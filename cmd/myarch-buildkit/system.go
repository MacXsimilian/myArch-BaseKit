package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const systemBinary = "/usr/local/lib/myarch-buildkit/myarch-buildkit"
const systemBackupRoot = "/var/backups/myarch-buildkit"

type systemPayload struct {
	Path         string         `json:"path"`
	Data         []byte         `json:"data,omitempty"`
	Mode         uint32         `json:"mode,omitempty"`
	RunID        string         `json:"run_id"`
	SHA256       string         `json:"sha256,omitempty"`
	NewInstall   bool           `json:"new_install,omitempty"`
	Greeter      map[string]any `json:"greeter,omitempty"`
	LegacyRecord map[string]any `json:"legacy_record,omitempty"`
	LegacyRunDir string         `json:"legacy_run_dir,omitempty"`
}
type systemJournal struct {
	Path           string         `json:"path"`
	Backup         string         `json:"backup,omitempty"`
	Previous       map[string]any `json:"previous,omitempty"`
	SHA256         string         `json:"sha256"`
	PendingSHA256  string         `json:"pending_sha256,omitempty"`
	PreviousSHA256 string         `json:"previous_sha256,omitempty"`
	RunID          string         `json:"run_id"`
	Existed        bool           `json:"existed"`
	Restored       bool           `json:"restored,omitempty"`
	UID            int            `json:"requester_uid"`
}

func systemDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func systemAllowed(path string) bool {
	switch path {
	case "/etc/greetd/config.toml", "/etc/greetd/hypr.lua", "/etc/greetd/hyprland.conf", systemBinary, "/usr/share/wayland-sessions/myarch-buildkit.desktop":
		return true
	}
	return false
}
func validRunID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return id != "." && id != ".."
}
func safeRootPath(root, path string, missing bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("unsafe absolute system path: %s", path)
	}
	current := root
	if current == "" {
		current = string(os.PathSeparator)
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && missing {
			continue
		}
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if info.Mode()&os.ModeSymlink != 0 || !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("system path is linked, unowned, or writable by others: %s", current)
		}
	}
	return nil
}
func rooted(root, path string) string {
	if root == "" {
		return path
	}
	return filepath.Join(root, strings.TrimPrefix(path, "/"))
}
func rootAtomic(path string, data []byte, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".myarch-buildkit-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Chown(name, uid, gid); err != nil {
		return err
	}
	if err = os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func rootJournalPaths(root string, uid int, runID, path string) (string, string, error) {
	if !validRunID(runID) {
		return "", "", fmt.Errorf("invalid system write run identity")
	}
	dir := fmt.Sprintf("%s/%d/%s", systemBackupRoot, uid, runID)
	if err := safeRootPath(root, dir, true); err != nil {
		return "", "", err
	}
	real := rooted(root, dir)
	if err := os.MkdirAll(real, 0700); err != nil {
		return "", "", err
	}
	// Journals and original files are readable only by root.
	if err := os.Chmod(real, 0700); err != nil {
		return "", "", err
	}
	digest := systemDigest([]byte(path))
	return filepath.Join(real, digest+".json"), filepath.Join(real, digest+".original"), nil
}
func saveSystemJournal(path string, journal systemJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return rootAtomic(path, append(data, '\n'), 0600, 0, 0)
}
func privilegedWrite(root string, uid int, p systemPayload) (systemJournal, error) {
	var journal systemJournal
	if !systemAllowed(p.Path) || len(p.Data) > 64<<20 {
		return journal, fmt.Errorf("system write is outside the supported file paths or size limit")
	}
	wantMode := uint32(0644)
	if p.Path == systemBinary {
		wantMode = 0755
	}
	if p.Mode != wantMode {
		return journal, fmt.Errorf("unsupported permissions for %s", p.Path)
	}
	if err := safeRootPath(root, p.Path, true); err != nil {
		return journal, err
	}
	destination := rooted(root, p.Path)
	journalPath, backup, err := rootJournalPaths(root, uid, p.RunID, p.Path)
	if err != nil {
		return journal, err
	}
	if err = safeRootPath(root, strings.TrimPrefix(journalPath, root), true); err != nil {
		return journal, err
	}
	if existing, err := os.ReadFile(journalPath); err == nil {
		if err = json.Unmarshal(existing, &journal); err != nil {
			return journal, err
		}
		if journal.UID != uid || journal.Path != p.Path || journal.RunID != p.RunID || journal.Restored {
			return journal, fmt.Errorf("system journal identity does not match")
		}
		if current, readErr := os.ReadFile(destination); readErr == nil && systemDigest(current) != journal.SHA256 && systemDigest(current) != journal.PendingSHA256 {
			return journal, fmt.Errorf("system file changed since setup: %s", p.Path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return journal, err
	} else {
		journal = systemJournal{Path: p.Path, RunID: p.RunID, UID: uid}
		info, statErr := os.Lstat(destination)
		if statErr == nil {
			if !info.Mode().IsRegular() {
				return journal, fmt.Errorf("refusing non-regular system file")
			}
			old, err := os.ReadFile(destination)
			if err != nil {
				return journal, err
			}
			stat := info.Sys().(*syscall.Stat_t)
			journal.Existed = true
			journal.Backup = backup
			journal.Previous = map[string]any{"mode": uint32(info.Mode().Perm()), "uid": stat.Uid, "gid": stat.Gid}
			if p.NewInstall {
				if p.Path != systemBinary || !bytes.Equal(old, p.Data) {
					return journal, fmt.Errorf("invalid initial binary installation")
				}
				journal.Existed = false
				journal.Backup = ""
				journal.Previous = nil
			} else if err = rootAtomic(backup, old, 0600, 0, 0); err != nil {
				return journal, err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return journal, statErr
		}
	}
	journal.PendingSHA256 = systemDigest(p.Data)
	if old, err := os.ReadFile(destination); err == nil {
		journal.PreviousSHA256 = systemDigest(old)
	}
	if err = saveSystemJournal(journalPath, journal); err != nil {
		return journal, err
	}
	if err = rootAtomic(destination, p.Data, os.FileMode(p.Mode), 0, 0); err != nil {
		return journal, err
	}
	journal.SHA256 = journal.PendingSHA256
	journal.PendingSHA256 = ""
	journal.PreviousSHA256 = ""
	return journal, saveSystemJournal(journalPath, journal)
}
func privilegedRestore(root string, uid int, p systemPayload) error {
	if !systemAllowed(p.Path) {
		return fmt.Errorf("unsupported system restoration path")
	}
	if err := safeRootPath(root, p.Path, true); err != nil {
		return err
	}
	journalPath, _, err := rootJournalPaths(root, uid, p.RunID, p.Path)
	if err != nil {
		return err
	}
	if err = safeRootPath(root, strings.TrimPrefix(journalPath, root), false); err != nil {
		return err
	}
	data, err := os.ReadFile(journalPath)
	if err != nil {
		return err
	}
	var j systemJournal
	if err = json.Unmarshal(data, &j); err != nil {
		return err
	}
	if j.UID != uid || j.Path != p.Path || j.RunID != p.RunID {
		return fmt.Errorf("system restoration journal identity does not match")
	}
	if j.Restored {
		return nil
	}
	destination := rooted(root, p.Path)
	if current, readErr := os.ReadFile(destination); readErr == nil {
		digest := systemDigest(current)
		if digest != j.SHA256 && digest != j.PendingSHA256 && digest != j.PreviousSHA256 {
			return fmt.Errorf("file changed since setup; restore manually: %s", p.Path)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if j.Existed {
		_, expectedBackup, err := rootJournalPaths(root, uid, p.RunID, p.Path)
		if err != nil {
			return err
		}
		if j.Backup != expectedBackup {
			return fmt.Errorf("unsupported original backup path")
		}
		if err = safeRootPath(root, strings.TrimPrefix(j.Backup, root), false); err != nil {
			return err
		}
		old, err := os.ReadFile(j.Backup)
		if err != nil {
			return err
		}
		mode := os.FileMode(mapJSONNumber(j.Previous["mode"]))
		owner := int(mapJSONNumber(j.Previous["uid"]))
		group := int(mapJSONNumber(j.Previous["gid"]))
		if err = rootAtomic(destination, old, mode, owner, group); err != nil {
			return err
		}
	} else if err = os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	j.Restored = true
	return saveSystemJournal(journalPath, j)
}
func requesterUID() (int, error) {
	if os.Geteuid() != 0 {
		return 0, fmt.Errorf("internal system command requires sudo")
	}
	uid, err := strconv.Atoi(os.Getenv("SUDO_UID"))
	if err != nil || uid <= 0 {
		return 0, fmt.Errorf("internal system command requires an original non-root sudo user")
	}
	return uid, nil
}

// SystemCommand uses stdin only, and independently validates every privileged path.
func SystemCommand(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("unexpected internal command arguments")
	}
	uid, err := requesterUID()
	if err != nil {
		return err
	}
	input, err := io.ReadAll(io.LimitReader(os.Stdin, 90<<20))
	if err != nil {
		return err
	}
	var p systemPayload
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&p); err != nil {
		return err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("internal command expects one JSON payload")
	}
	switch args[0] {
	case "internal-write":
		j, err := privilegedWrite("", uid, p)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(j)
	case "internal-restore":
		return privilegedRestore("", uid, p)
	case "internal-restore-legacy":
		return privilegedLegacyRestore("", uid, p)
	case "internal-greeter-boot":
		record, err := privilegedGreeterBoot("", uid, p)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(record)
	case "internal-greeter-restore":
		return privilegedGreeterRestore("", uid, p)
	default:
		return fmt.Errorf("unknown internal command")
	}
}
func rootOperation(c *Context, command string, p systemPayload) (CommandResult, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return CommandResult{}, err
	}
	return c.Command(Command{Args: []string{"sudo", "-n", systemBinary, command}, Input: data})
}
func (c *Context) systemRunID() string { return "go-" + systemDigest([]byte(c.RunDir))[:24] }
func appendSystemRecord(c *Context, record map[string]any) {
	list := []any{}
	switch records := c.Report["system_changes"].(type) {
	case []any:
		list = records
	case []map[string]any:
		for _, r := range records {
			list = append(list, r)
		}
	}
	for _, item := range list {
		r := object(item)
		if r["path"] == record["path"] && r["run_id"] == record["run_id"] {
			for k, v := range record {
				r[k] = v
			}
			if record["sha256"] != nil {
				delete(r, "pending_sha256")
				delete(r, "previous_sha256")
			}
			c.Report["system_changes"] = list
			return
		}
	}
	c.Report["system_changes"] = append(list, record)
}
func (c *Context) SystemWrite(path string, data []byte, mode os.FileMode) error {
	if err := c.Err(); err != nil {
		return err
	}
	if c.CheckOnly {
		return fmt.Errorf("a check cannot modify system files")
	}
	if !systemAllowed(path) {
		return fmt.Errorf("unsupported system write: %s", path)
	}
	if err := bootstrapRootBinary(c); err != nil {
		return err
	}
	return systemWriteReady(c, path, data, mode, false)
}
func systemWriteReady(c *Context, path string, data []byte, mode os.FileMode, newInstall bool) error {
	record := map[string]any{"path": path, "run_id": c.systemRunID(), "pending_sha256": systemDigest(data), "write_journal_version": 3}
	appendSystemRecord(c, record)
	if err := c.Save(); err != nil {
		return err
	}
	result, err := rootOperation(c, "internal-write", systemPayload{Path: path, Data: data, Mode: uint32(mode.Perm()), RunID: c.systemRunID(), NewInstall: newInstall})
	if err != nil {
		return err
	}
	var details map[string]any
	if err = json.Unmarshal([]byte(result.Stdout), &details); err != nil {
		return err
	}
	details["write_journal_version"] = 3
	appendSystemRecord(c, details)
	return c.Save()
}
func RestoreSystem(c *Context, record map[string]any) error {
	path, _ := record["path"].(string)
	runID, _ := record["run_id"].(string)
	if runID == "" {
		if err := bootstrapRootBinary(c); err != nil {
			return err
		}
		_, err := rootOperation(c, "internal-restore-legacy", systemPayload{LegacyRecord: record, LegacyRunDir: c.RunDir})
		return err
	}
	_, err := rootOperation(c, "internal-restore", systemPayload{Path: path, RunID: runID})
	return err
}
func bootstrapRootBinary(c *Context) error {
	if c.rootBinaryReady {
		return verifyInstalledRootBinary(c)
	}
	source, err := currentExecutable()
	if err != nil {
		return err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("cannot identify the compiled myarch-buildkit executable")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	current := c.Try("sudo", "-n", "/usr/bin/sha256sum", systemBinary)
	if current.Code == 0 {
		if err := verifyInstalledRootBinary(c); err != nil {
			return err
		}
		fields := strings.Fields(current.Stdout)
		if len(fields) == 0 {
			return fmt.Errorf("could not read installed root binary checksum")
		}
		for _, record := range reportRecords(c.Report, "system_changes") {
			if record["path"] == systemBinary && record["run_id"] == c.systemRunID() && record["initial_install"] == true && record["sha256"] == nil && record["pending_sha256"] == fields[0] {
				installed, readErr := os.ReadFile(systemBinary)
				if readErr != nil {
					return readErr
				}
				if err = systemWriteReady(c, systemBinary, installed, 0755, true); err != nil {
					return err
				}
			}
		}
		if fields[0] != systemDigest(data) {
			if err = systemWriteReady(c, systemBinary, data, 0755, false); err != nil {
				return err
			}
		}
		c.rootBinaryReady = true
		return nil
	}
	// Bootstrap through packaged utilities. Never run a user-owned binary as root.
	for _, path := range []string{"/usr", "/usr/local", "/usr/local/lib", "/usr/local/lib/myarch-buildkit", systemBinary} {
		result := c.Try("sudo", "-n", "/usr/bin/test", "-L", path)
		if result.Code == 0 {
			return fmt.Errorf("root binary installation path is linked: %s", path)
		}
		stat := c.Try("sudo", "-n", "/usr/bin/stat", "-c", "%u:%a:%F", "--", path)
		if stat.Code == 0 {
			if err = validateRootStat(path, stat.Stdout); err != nil {
				return err
			}
		} else if path != filepath.Dir(systemBinary) && path != systemBinary {
			return fmt.Errorf("cannot verify root binary parent directory: %s", path)
		}
	}
	appendSystemRecord(c, map[string]any{"path": systemBinary, "run_id": c.systemRunID(), "pending_sha256": systemDigest(data), "write_journal_version": 3, "initial_install": true})
	if err = c.Save(); err != nil {
		return err
	}
	if _, err = c.Run("sudo", "-n", "/usr/bin/install", "-d", "-m", "0755", "-o", "root", "-g", "root", filepath.Dir(systemBinary)); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(systemBinary), fmt.Sprintf(".myarch-buildkit-%d-%d", os.Getuid(), os.Getpid()))
	if _, err = c.Run("sudo", "-n", "/usr/bin/install", "-m", "0755", "-o", "root", "-g", "root", "--", source, temporary); err != nil {
		return err
	}
	digest, err := c.Run("sudo", "-n", "/usr/bin/sha256sum", temporary)
	if err != nil {
		return err
	}
	fields := strings.Fields(digest.Stdout)
	if len(fields) < 1 || fields[0] != systemDigest(data) {
		return fmt.Errorf("compiled myarch-buildkit binary changed during installation")
	}
	if _, err = c.Run("sudo", "-n", "/usr/bin/mv", "-T", "--", temporary, systemBinary); err != nil {
		return err
	}
	if err = verifyInstalledRootBinary(c); err != nil {
		return err
	}
	if err = systemWriteReady(c, systemBinary, data, 0755, true); err != nil {
		return err
	}
	c.rootBinaryReady = true
	return nil
}
func verifyInstalledRootBinary(c *Context) error {
	for _, path := range []string{"/usr", "/usr/local", "/usr/local/lib", filepath.Dir(systemBinary), systemBinary} {
		result, err := c.Run("sudo", "-n", "/usr/bin/stat", "-c", "%u:%a:%F", "--", path)
		if err != nil {
			return err
		}
		if err = validateRootStat(path, result.Stdout); err != nil {
			return err
		}
	}
	return nil
}
func validateRootStat(path, text string) error {
	fields := strings.Split(strings.TrimSpace(text), ":")
	if len(fields) != 3 || fields[0] != "0" {
		return fmt.Errorf("system binary path is not root-owned: %s", path)
	}
	mode, err := strconv.ParseUint(fields[1], 8, 32)
	if err != nil || mode&0022 != 0 || strings.Contains(fields[2], "symbolic link") || !(strings.Contains(fields[2], "directory") || strings.Contains(fields[2], "regular file")) {
		return fmt.Errorf("system binary path has unsafe ownership or permissions: %s", path)
	}
	return nil
}
func InstallLogin(c *Context) error {
	if err := bootstrapRootBinary(c); err != nil {
		return err
	}
	entry := "[Desktop Entry]\nName=myarch-buildkit\nComment=Apply pending configuration before Hyprland starts\nExec=" + systemBinary + " login\nType=Application\nDesktopNames=Hyprland\n"
	if err := c.SystemWrite("/usr/share/wayland-sessions/myarch-buildkit.desktop", []byte(entry), 0644); err != nil {
		return err
	}
	c.Note("At the next login select myarch-buildkit to apply the pending bundle before Hyprland starts.")
	return nil
}
func executeRoot(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s: %s", shellJoin(args), strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// Removed configuration is restore-only: new runs cannot write these paths.
func legacySystemAllowed(path string) bool {
	if systemAllowed(path) {
		return true
	}
	switch path {
	case "/usr/local/lib/myarch-buildkit/login.py", "/etc/systemd/logind.conf.d/60-myarch-buildkit-lid.conf", "/var/cache/dms-greeter/settings.json", "/var/cache/dms-greeter/session.json", "/var/cache/dms-greeter/custom-theme.json":
		return true
	}
	return false
}
func secureLegacyBackup(root, path string, uid int) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("invalid legacy backup path")
	}
	current := root
	if current == "" {
		current = "/"
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || info.Mode()&os.ModeSymlink != 0 || (stat.Uid != 0 && stat.Uid != uint32(uid)) || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("legacy backup has unsafe ownership, permissions, or symlink ancestry")
		}
	}
	info, err := os.Lstat(rooted(root, path))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Uid != uint32(uid) || info.Size() > 64<<20 {
		return fmt.Errorf("legacy original is not an owned regular backup")
	}
	return nil
}
func privilegedLegacyRestore(root string, uid int, p systemPayload) error {
	record := p.LegacyRecord
	path, _ := record["path"].(string)
	if !legacySystemAllowed(path) {
		return fmt.Errorf("unsupported legacy restoration destination")
	}
	if err := safeLegacyDestination(root, path); err != nil {
		return err
	}
	destination := rooted(root, path)
	if current, err := os.ReadFile(destination); err == nil {
		digest, _ := record["sha256"].(string)
		if len(digest) != 64 || systemDigest(current) != digest {
			return fmt.Errorf("file changed since setup; restore manually: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	backup, _ := record["backup"].(string)
	if backup == "" {
		if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if !filepath.IsAbs(p.LegacyRunDir) || filepath.Clean(p.LegacyRunDir) != p.LegacyRunDir {
		return fmt.Errorf("invalid legacy run directory")
	}
	expected := filepath.Join(p.LegacyRunDir, "backups", "profile", "system-files", systemDigest([]byte(path)))
	if backup != expected {
		return fmt.Errorf("legacy original is outside the recorded backup tree")
	}
	if err := secureLegacyBackup(root, backup, uid); err != nil {
		return err
	}
	data, err := os.ReadFile(rooted(root, backup))
	if err != nil {
		return err
	}
	old := object(record["previous"])
	mode := os.FileMode(mapJSONNumber(old["mode"]))
	owner := int(mapJSONNumber(old["uid"]))
	group := int(mapJSONNumber(old["gid"]))
	if mode == 0 {
		mode = 0644
	}
	if mode.Perm()&0022 != 0 || mode&07000 != 0 {
		return fmt.Errorf("legacy original has unsafe system file permissions")
	}
	return rootAtomic(destination, data, mode, owner, group)
}

func safeLegacyDestination(root, path string) error {
	if !strings.HasPrefix(path, "/var/cache/dms-greeter/") {
		return safeRootPath(root, path, true)
	}
	if err := safeRootPath(root, "/var/cache", false); err != nil {
		return err
	}
	account, err := user.Lookup("greeter")
	if err != nil {
		return fmt.Errorf("packaged greeter account is missing")
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil || uid <= 0 {
		return fmt.Errorf("invalid greeter account")
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil || gid <= 0 {
		return fmt.Errorf("invalid greeter account group")
	}
	for _, item := range []string{"/var/cache/dms-greeter", path} {
		info, err := os.Lstat(rooted(root, item))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || info.Mode()&os.ModeSymlink != 0 || (stat.Uid != 0 && stat.Uid != uint32(uid)) || info.Mode().Perm()&0002 != 0 || (info.Mode().Perm()&0020 != 0 && stat.Gid != uint32(gid)) {
			return fmt.Errorf("legacy greeter cache path is unsafe")
		}
	}
	return nil
}
