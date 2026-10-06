package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func hashBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func pathID(path string) string    { return hashBytes([]byte(path)) }

// securePath refuses symlinks and files another user can replace. Missing files
// are accepted only when their nearest existing parent belongs to this user.
func securePath(path string, missing bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("unsafe path: %s", path)
	}
	for item := path; ; item = filepath.Dir(item) {
		info, err := os.Lstat(item)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe symlink: %s", item)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if item == filepath.Dir(item) {
			break
		}
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if !missing {
			return fmt.Errorf("missing path: %s", path)
		}
		parent := filepath.Dir(path)
		for {
			info, err = os.Stat(parent)
			if err == nil {
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			next := filepath.Dir(parent)
			if next == parent {
				return err
			}
			parent = next
		}
		if !info.IsDir() {
			return fmt.Errorf("parent is not a directory: %s", parent)
		}
		return ownedInfo(parent, info)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("unsupported file type: %s", path)
	}
	if err = ownedInfo(path, info); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return err
	}
	return ownedInfo(parent, parentInfo)
}
func ownedInfo(path string, info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("unowned or writable-by-others path: %s", path)
	}
	return nil
}

func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := securePath(path, true); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".myarch-buildkit-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Chmod(mode.Perm()); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err == nil {
		defer dir.Close()
		err = dir.Sync()
	}
	return err
}
func jsonBytes(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
func WriteJSON(path string, value any) error {
	data, err := jsonBytes(value)
	if err != nil {
		return err
	}
	return AtomicWrite(path, data, 0600)
}
func ReadJSON(path string, out any) error {
	if err := securePath(path, false); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 32*1024*1024 {
		return fmt.Errorf("expected a small JSON file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	if err = decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing JSON data: %s", path)
	}
	return nil
}
func FileHash(path string) (string, error) {
	if err := securePath(path, true); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return hashBytes(data), nil
}

func (c *Context) ensureReport() {
	if c.Report == nil {
		c.Report = map[string]any{}
	}
	for _, key := range []string{"file_changes", "system_changes", "warnings", "notes", "completed_phases"} {
		if c.Report[key] == nil {
			c.Report[key] = []any{}
		}
	}
	if c.Report["schema_version"] == nil {
		c.Report["schema_version"] = 1
	}
}
func (c *Context) Save() error {
	if c.CheckOnly {
		return nil
	}
	c.ensureReport()
	return WriteProfileReport(c.ReportPath, c.ReportScope, c.Report)
}
func reportRecords(report map[string]any, key string) []map[string]any {
	result := []map[string]any{}
	switch values := report[key].(type) {
	case []any:
		for _, value := range values {
			if item, ok := value.(map[string]any); ok {
				result = append(result, item)
			}
		}
	case []map[string]any:
		return values
	}
	return result
}
func appendRecord(report map[string]any, key string, record map[string]any) {
	records := reportRecords(report, key)
	records = append(records, record)
	report[key] = records
}
func (c *Context) ownershipPath(path string) string {
	root := filepath.Join(c.StateHome, "myarch-buildkit", "managed-files")
	if inside(root, path) {
		return ""
	}
	name := filepath.Base(path)
	protected := strings.Contains(name, "desktop-managed") || strings.HasPrefix(name, "cachyos-") && strings.HasSuffix(name, ".service")
	for _, value := range []string{"cachyos-laptop-session", "cachyos-desktop-session", "cachyos-dms-guard", "cachyos-screenshot", "cachyos-clipboard", "laptop.json", "laptop.lua", "laptop.conf"} {
		protected = protected || name == value
	}
	if active, ok := c.Inventory["active_config"].(string); ok {
		protected = protected || active == path
	}
	protected = protected || object(c.Report["profile_config"])["main_config"] == path
	if !protected {
		return ""
	}
	return filepath.Join(root, pathID(path)+".json")
}
func (c *Context) checkOwned(path string) error {
	marker := c.ownershipPath(path)
	if marker == "" || !exists(marker) {
		return nil
	}
	old := map[string]any{}
	if err := ReadJSON(marker, &old); err != nil {
		return err
	}
	if old["path"] != path {
		return fmt.Errorf("invalid managed ownership record: %s", marker)
	}
	actual, err := FileHash(path)
	if err != nil {
		return err
	}
	if actual == "" || actual != old["sha256"] {
		return fmt.Errorf("managed file was edited after setup; preserved: %s. Move custom settings into desktop-custom.lua/.conf before retrying", path)
	}
	return nil
}
func (c *Context) backup(path string) (map[string]any, error) {
	c.ensureReport()
	for _, record := range reportRecords(c.Report, "file_changes") {
		if record["path"] == path {
			return record, nil
		}
	}
	if err := securePath(path, true); err != nil {
		return nil, err
	}
	hash, err := FileHash(path)
	if err != nil {
		return nil, err
	}
	record := map[string]any{"path": path, "requested_path": path, "backup": nil, "mode": nil, "sha256": nil, "write_journal_version": 2}
	if hash != "" {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		saved := filepath.Join(c.RunDir, "backups", "profile", "user-files", pathID(path))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err = AtomicWrite(saved, data, info.Mode()); err != nil {
			return nil, err
		}
		record["backup"] = saved
		record["mode"] = int(info.Mode().Perm())
		record["sha256"] = hash
	}
	appendRecord(c.Report, "file_changes", record)
	return record, c.Save()
}

// Ordinary app defaults preserve safe symlinked configuration. Login payloads
// use stageWrite instead, which always rejects symlinks.
func resolvedUserPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("unsafe path: %s", path)
	}
	current := path
	suffix := []string{}
	for {
		_, err := os.Lstat(current)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("missing path parent: %s", path)
		}
		current = parent
	}
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	return resolved, nil
}

func (c *Context) Write(path string, data []byte, mode os.FileMode) error {
	if err := c.Err(); err != nil {
		return err
	}
	if c.CheckOnly {
		return fmt.Errorf("a check cannot modify configuration")
	}
	path = filepath.Clean(path)
	if c.Pending != nil && !inside(c.RunDir, path) {
		return c.stageWrite(path, data, mode)
	}
	requested := path
	path, err := resolvedUserPath(path)
	if err != nil {
		return err
	}
	if err := securePath(path, true); err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err == nil && string(old) == string(data) {
		info, _ := os.Stat(path)
		if mode&0111 == 0 || info.Mode()&0111 != 0 {
			return nil
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = c.checkOwned(path); err != nil {
		return err
	}
	record, err := c.backup(path)
	if err != nil {
		return err
	}
	record["requested_path"] = requested
	desired := mode
	if info, err := os.Stat(path); err == nil && mode&0111 == 0 {
		desired = info.Mode().Perm()
	}
	before, err := FileHash(path)
	if err != nil {
		return err
	}
	record["pending_sha256"] = hashBytes(data)
	record["previous_sha256"] = nullableHash(before)
	if err = c.Save(); err != nil {
		return err
	}
	if err = AtomicWrite(path, data, desired); err != nil {
		return err
	}
	record["sha256"] = hashBytes(data)
	delete(record, "pending_sha256")
	delete(record, "previous_sha256")
	if marker := c.ownershipPath(path); marker != "" {
		bytes, err := jsonBytes(map[string]any{"path": path, "sha256": record["sha256"]})
		if err != nil {
			return err
		}
		if err = c.Write(marker, bytes, 0600); err != nil {
			return err
		}
	}
	return c.Save()
}
func nullableHash(hash string) any {
	if hash == "" {
		return nil
	}
	return hash
}
func boolValue(v any) bool { value, _ := v.(bool); return value }
func modeValue(value any, fallback os.FileMode) os.FileMode {
	switch v := value.(type) {
	case float64:
		return os.FileMode(v)
	case int:
		return os.FileMode(v)
	case uint32:
		return os.FileMode(v)
	case json.Number:
		n, e := v.Int64()
		if e == nil {
			return os.FileMode(n)
		}
	}
	return fallback
}

func RestoreContext(c *Context) error {
	if c.CheckOnly {
		return fmt.Errorf("a check cannot restore configuration")
	}
	unified, err := resolveUnifiedRestore(c)
	if err != nil {
		return err
	}
	if c.Options.RestoreProfile != "" && !unified {
		desktopRun := filepath.Join(c.RunDir, "stages", "desktop")
		if exists(filepath.Join(c.RunDir, "stages")) {
			desktopReport := filepath.Join(desktopRun, "profile-report.json")
			if !exists(desktopReport) {
				return fmt.Errorf("no desktop profile report in %s; restore a specific completed stage with --restore-stage and --stage-run-dir", c.RunDir)
			}
			c.RunDir = desktopRun
			c.ReportPath = desktopReport
		}
	}
	report := map[string]any{}
	if !exists(c.ReportPath) {
		names := map[string]string{"defaults": "desktop-report.json", "shell": "shell-report.json", "containers": "podman-report.json", "cleanup": "cleanup-report.json"}
		if name := names[c.Options.RestoreStage]; name != "" {
			return restoreLegacyUserReport(c, filepath.Join(c.RunDir, name))
		}
	}
	if err := ReadProfileReport(c.ReportPath, c.ReportScope, &report); err != nil {
		return err
	}
	if c.Options.RestoreProfile != "" {
		configuration := object(report["profile_config"])
		if _, ok := configuration["main_config"].(string); !ok && !contains(stringList(report["completed_phases"]), "desktop") {
			return fmt.Errorf("no desktop profile in %s; restore a specific completed stage with --restore-stage and --stage-run-dir", c.RunDir)
		}
	}
	if report["file_changes"] == nil && (report["files"] != nil || report["changed_paths"] != nil) {
		return restoreLegacyUserReport(c, c.ReportPath)
	}
	if mapJSONNumber(report["schema_version"]) != 1 {
		return fmt.Errorf("unsupported report schema")
	}
	c.Report = report
	c.ensureReport()
	records := reportRecords(report, "file_changes")
	needLogout := len(reportRecords(report, "login_service_changes")) > 0
	ordinaryStage := contains([]string{"defaults", "shell", "containers", "cleanup"}, c.Options.RestoreStage)
	if !ordinaryStage {
		for _, record := range records {
			needLogout = needLogout || !inside(c.RunDir, str(record["path"]))
		}
	}
	if needLogout {
		if err := NoDesktop(c); err != nil {
			return err
		}
	}
	if err := cancelPending(c); err != nil {
		return err
	}
	failures := []string{}
	// Old reports can contain theme dependencies. Preserve those if changed DMS
	// settings still reference them; new installers never create theme assets.
	retained := map[string]bool{}
	settings := filepath.Join(c.ConfigHome, "DankMaterialShell", "settings.json")
	for _, record := range records {
		if record["path"] != settings || boolValue(record["restored"]) {
			continue
		}
		actual, err := FileHash(settings)
		if err != nil || actual == record["sha256"] {
			continue
		}
		value := map[string]any{}
		if ReadJSON(settings, &value) != nil {
			continue
		}
		reference, ok := value["customThemeFile"].(string)
		if !ok {
			continue
		}
		if strings.HasPrefix(reference, "~/") {
			reference = filepath.Join(c.Home, reference[2:])
		}
		for _, entry := range records {
			if entry["path"] == reference && !boolValue(entry["restored"]) {
				retained[reference] = true
			}
		}
	}
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		if boolValue(record["restored"]) {
			continue
		}
		path := str(record["path"])
		delete(record, "retained_dependency")
		if retained[path] {
			record["retained_dependency"] = settings
			failures = append(failures, "retained theme referenced by changed DMS settings: "+path)
			continue
		}
		if err := restoreFile(record); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		record["restored"] = true
	}
	if err := RestoreGreeter(c); err != nil {
		failures = append(failures, err.Error())
	}
	for _, record := range reversedRecords(reportRecords(report, "system_changes")) {
		if record["path"] == systemBinary {
			continue
		}
		if boolValue(record["restored"]) {
			continue
		}
		if err := RestoreSystem(c, record); err != nil {
			failures = append(failures, err.Error())
		} else {
			record["restored"] = true
		}
	}
	restoreExternal(c, &failures)
	// Legacy restores can bootstrap this helper while processing old records.
	// Restore it last, after every operation that still needs root access.
	if len(failures) == 0 {
		for _, record := range reversedRecords(reportRecords(report, "system_changes")) {
			if record["path"] != systemBinary || boolValue(record["restored"]) {
				continue
			}
			if err := RestoreSystem(c, record); err != nil {
				failures = append(failures, err.Error())
			} else {
				record["restored"] = true
			}
		}
	}
	active := filepath.Join(c.StateHome, "myarch-buildkit", "active-profile.json")
	if len(failures) == 0 && exists(active) {
		value := map[string]any{}
		if ReadJSON(active, &value) == nil && value["report"] == c.ReportPath {
			activeScope, _ := value["report_scope"].(string)
			if activeScope == c.ReportScope {
				if err := os.Remove(active); err != nil {
					failures = append(failures, err.Error())
				}
			}
		}
	}
	report["status"] = "restored"
	if len(failures) > 0 {
		report["status"] = "restore_partial"
		for _, failure := range failures {
			c.Warn(failure)
		}
	}
	if err := c.Save(); err != nil {
		return err
	}
	if len(failures) > 0 {
		return fmt.Errorf("restoration needs manual follow-up: %s", strings.Join(failures, "; "))
	}
	return nil
}
func reversedRecords(records []map[string]any) []map[string]any {
	out := make([]map[string]any, len(records))
	for i, r := range records {
		out[len(records)-i-1] = r
	}
	return out
}
func restoreFile(record map[string]any) error {
	path := str(record["path"])
	if err := securePath(path, true); err != nil {
		return err
	}
	actual, err := FileHash(path)
	if err != nil {
		return err
	}
	if actual != "" {
		allowed := map[string]bool{}
		for _, key := range []string{"sha256", "pending_sha256", "previous_sha256"} {
			if value, ok := record[key].(string); ok && value != "" {
				allowed[value] = true
			}
		}
		if len(allowed) > 0 && !allowed[actual] || len(allowed) == 0 && mapJSONNumber(record["write_journal_version"]) == 2 {
			return fmt.Errorf("changed since setup; restore manually: %s", path)
		}
	}
	if backup, ok := record["backup"].(string); ok && backup != "" {
		if err := securePath(backup, false); err != nil {
			return err
		}
		data, err := os.ReadFile(backup)
		if err != nil {
			return err
		}
		info, err := os.Stat(backup)
		if err != nil {
			return err
		}
		return AtomicWrite(path, data, modeValue(record["mode"], info.Mode().Perm()))
	}
	if actual != "" {
		return os.Remove(path)
	}
	return nil
}
func restoreExternal(c *Context, failures *[]string) {
	try := func(message string, args ...string) bool {
		result := c.Try(args...)
		if result.Code != 0 {
			*failures = append(*failures, message+": "+strings.TrimSpace(result.Stderr))
			return false
		}
		return true
	}
	for _, change := range reversedRecords(reportRecords(c.Report, "gsettings_changes")) {
		if boolValue(change["restored"]) {
			continue
		}
		schema, key := str(change["schema"]), str(change["key"])
		current := c.Try("gsettings", "get", schema, key)
		if current.Code != 0 || strings.TrimSpace(current.Stdout) != strings.TrimSpace(str(change["new"])) {
			*failures = append(*failures, "changed since setup; restore GSettings manually: "+schema+" "+key)
			continue
		}
		if try("unable to restore GSettings "+key, "gsettings", "set", schema, key, str(change["old"])) {
			change["restored"] = true
		}
	}
	for _, change := range reversedRecords(reportRecords(c.Report, "login_service_changes")) {
		if boolValue(change["restored"]) {
			continue
		}
		unit, old := str(change["unit"]), str(change["old"])
		if !contains(disableUnits, unit) || old != "enabled" && old != "enabled-runtime" {
			*failures = append(*failures, "restore previous linked/unsupported service manually: "+unit)
			continue
		}
		args := []string{"systemctl", "--user", "enable"}
		if old == "enabled-runtime" {
			args = append(args, "--runtime")
		}
		args = append(args, unit)
		if try("service enablement restore failed "+unit, args...) {
			change["restored"] = true
		}
	}
	greeter := object(c.Report["greeter"])
	if !boolValue(greeter["root_boot_journal"]) && !boolValue(greeter["boot_transition_restored"]) && (greeter["status"] == "enabled_for_next_boot" || boolValue(greeter["boot_transition_started"])) {
		ok := true
		previous := str(greeter["previous_display_manager"])
		if previous != "greetd.service" || !boolValue(greeter["previous_display_manager_enabled"]) {
			ok = try("restore prior boot login choice", "sudo", "systemctl", "disable", "greetd.service")
		}
		if boolValue(greeter["previous_display_manager_enabled"]) {
			if safeUnit(previous) {
				ok = try("restore former display manager", "sudo", "systemctl", "enable", "--force", previous) && ok
			} else {
				*failures = append(*failures, "unsupported former display manager: "+previous)
				ok = false
			}
		}
		if ok {
			greeter["boot_transition_restored"] = true
		}
	}
	// Compatibility with backups made before power policy was removed.
	for _, change := range reversedRecords(reportRecords(object(c.Report["power_backend"]), "service_changes")) {
		if boolValue(change["restored"]) {
			continue
		}
		unit := str(change["unit"])
		if !contains([]string{"power-profiles-daemon.service", "tuned-ppd.service", "tlp-pd.service", "tuned.service", "tlp.service"}, unit) {
			*failures = append(*failures, "unsupported power-service restore: "+unit)
			continue
		}
		ok := true
		active := change["active_state"] == "active" || change["active_state"] == "activating"
		if !active {
			ok = try("power-service restore failed", "sudo", "systemctl", "stop", unit)
		}
		switch change["unit_file_state"] {
		case "disabled":
			ok = try("power-service restore failed", "sudo", "systemctl", "disable", unit) && ok
		case "enabled-runtime":
			ok = try("power-service restore failed", "sudo", "systemctl", "disable", unit) && ok
			ok = try("power-service restore failed", "sudo", "systemctl", "enable", "--runtime", unit) && ok
		case "enabled":
			ok = try("power-service restore failed", "sudo", "systemctl", "enable", unit) && ok
		}
		if active {
			ok = try("power-service restore failed", "sudo", "systemctl", "start", unit) && ok
		}
		if ok {
			change["restored"] = true
		}
	}
}
func safeUnit(unit string) bool {
	if !strings.HasSuffix(unit, ".service") {
		return false
	}
	for _, r := range unit {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("_.@:-", r)) {
			return false
		}
	}
	return true
}

// Restore reports from earlier installer builds without requiring Python. Their
// symlink identities and output hashes remain authoritative conflict checks.
func restoreLegacyUserReport(c *Context, reportPath string) error {
	report := map[string]any{}
	if err := ReadJSON(reportPath, &report); err != nil {
		return err
	}
	if _, present := report["files"]; present {
		backups := []map[string]any{}
		changed := []string{}
		hashes := map[string]any{}
		for _, file := range reportRecords(report, "files") {
			if !boolValue(file["changed"]) {
				continue
			}
			path := str(file["path"])
			changed = append(changed, path)
			hashes[path] = file["sha256"]
			if backup, ok := file["backup"].(string); ok && backup != "" {
				backups = append(backups, file)
			}
		}
		report["backups"] = backups
		report["changed_paths"] = changed
		report["written_sha256"] = hashes
	}
	changed := stringList(report["changed_paths"])
	set := map[string]bool{}
	for _, path := range changed {
		set[path] = true
	}
	backed := map[string]bool{}
	completed := map[string]bool{}
	for _, path := range stringList(report["restored_paths"]) {
		completed[path] = true
	}
	conflicts := []string{}
	hashes, links := object(report["written_sha256"]), object(report["written_links"])
	journal := func(path string) error {
		completed[path] = true
		paths := []string{}
		for _, raw := range changed {
			if completed[raw] {
				paths = append(paths, raw)
			}
		}
		report["restored_paths"] = paths
		return WriteJSON(reportPath, report)
	}
	verify := func(path string, record map[string]any) (string, error) {
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("unsafe restore path: %s", path)
		}
		expected := links[path]
		if value, present := record["symlink_target"]; present {
			expected = value
		}
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		actual := ""
		if info.Mode()&os.ModeSymlink != 0 {
			actual, err = os.Readlink(path)
			if err != nil {
				return "", err
			}
		}
		expectedText, _ := expected.(string)
		if actual != expectedText {
			return "", fmt.Errorf("symlink identity changed; preserved: %s", path)
		}
		destination, err := resolvedUserPath(path)
		if err != nil {
			return "", err
		}
		hash, err := FileHash(destination)
		if err != nil {
			return "", err
		}
		output, ok := hashes[path].(string)
		if !ok || output == "" {
			return "", fmt.Errorf("no recorded output hash; restore manually: %s", path)
		}
		if hash != output {
			return "", fmt.Errorf("changed since setup; preserved: %s", path)
		}
		return destination, nil
	}
	records := reportRecords(report, "backups")
	for _, record := range reversedRecords(records) {
		path := str(record["path"])
		if !set[path] {
			continue
		}
		backed[path] = true
		if completed[path] {
			continue
		}
		destination, err := verify(path, record)
		if err != nil {
			conflicts = append(conflicts, err.Error())
			continue
		}
		backup, ok := record["backup"].(string)
		if !ok || backup == "" {
			return fmt.Errorf("missing original backup: %s", path)
		}
		if err = securePath(backup, false); err != nil {
			return err
		}
		data, err := os.ReadFile(backup)
		if err != nil {
			return err
		}
		info, err := os.Stat(backup)
		if err != nil {
			return err
		}
		if err = AtomicWrite(destination, data, modeValue(record["mode"], info.Mode().Perm())); err != nil {
			return err
		}
		if err = journal(path); err != nil {
			return err
		}
	}
	for _, path := range changed {
		if backed[path] || completed[path] {
			continue
		}
		destination, err := verify(path, map[string]any{})
		if err != nil {
			conflicts = append(conflicts, err.Error())
			continue
		}
		if err = os.Remove(destination); err != nil {
			return err
		}
		if err = journal(path); err != nil {
			return err
		}
	}
	report["restore_conflicts"] = conflicts
	if len(conflicts) == 0 {
		report["status"] = "restored"
	} else {
		report["status"] = "restore_partial"
	}
	if err := WriteJSON(reportPath, report); err != nil {
		return err
	}
	c.Report = report
	c.ReportPath = reportPath
	if len(conflicts) > 0 {
		return fmt.Errorf("restoration needs manual follow-up: %s", strings.Join(conflicts, "; "))
	}
	return nil
}
