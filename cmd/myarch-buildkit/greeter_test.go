package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGreeterUsesPasswordLoginAndDocumentedCompositor(t *testing.T) {
	text, err := GreeterConfigText("/usr/bin/dms-greeter", "Hyprland", "/etc/greetd/hypr.lua")
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range []string{"[default_session]", "user = \"greeter\"", "--command", "hyprland", "-C"} {
		if !strings.Contains(text, need) {
			t.Fatalf("missing %s", need)
		}
	}
	if strings.Contains(text, "initial_session") || strings.Contains(text, "autologin") || strings.Contains(text, "dms run") {
		t.Fatalf("unexpected automatic session: %s", text)
	}
	if _, err = GreeterConfigText("/usr/bin/dms-greeter", "sway", "x"); err == nil {
		t.Fatal("unverified compositor accepted")
	}
}
func TestGreeterKeyboardRulesOnlyVerifiedInternalNames(t *testing.T) {
	roles := map[string][]string{"internal": {"at-translated-set-2-keyboard", "missing"}, "external": {"usb-keyboard"}}
	observed := map[string]bool{"at-translated-set-2-keyboard": true, "usb-keyboard": true}
	for _, kind := range []string{"lua", "hyprlang"} {
		text, err := GreeterKeyboardText(kind, roles, observed, "hu")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "at-translated-set-2-keyboard") || !strings.Contains(text, "kb_layout") || !strings.Contains(text, "DMS_RUN_GREETER") {
			t.Fatalf("missing per-device rule: %s", text)
		}
		if strings.Contains(text, "usb-keyboard") || strings.Contains(text, "missing") || strings.Contains(text, "theme") || strings.Contains(text, "input {") {
			t.Fatalf("unexpected global or appearance policy: %s", text)
		}
	}
	if _, err := GreeterKeyboardText("hyprlang", map[string][]string{"internal": {"bad\nname"}}, map[string]bool{"bad\nname": true}, "hu"); err == nil {
		t.Fatal("unsafe hyprlang device accepted")
	}
}
func TestGreetdExecutableVerification(t *testing.T) {
	for _, text := range []string{"{ path=/usr/bin/greetd ; argv[]=/usr/bin/greetd ; }", "{ path=greetd ; argv[]=greetd ; }"} {
		if !GreetdExecMatches(text) {
			t.Fatalf("packaged executable rejected: %s", text)
		}
	}
	for _, text := range []string{"{ path=/tmp/greetd ; }", "{ path=/usr/bin/greetd ; }; { path=/tmp/extra ; }", "/usr/bin/greetd"} {
		if GreetdExecMatches(text) {
			t.Fatalf("unverified executable accepted: %s", text)
		}
	}
}
func TestSystemAllowlistRemovesPowerAndThemePaths(t *testing.T) {
	for _, path := range []string{"/etc/greetd/config.toml", "/etc/greetd/hypr.lua", systemBinary, "/usr/share/wayland-sessions/myarch-buildkit.desktop"} {
		if !systemAllowed(path) {
			t.Errorf("supported path rejected: %s", path)
		}
	}
	for _, path := range []string{"/etc/passwd", "/etc/pam.d/greetd", "/etc/systemd/logind.conf.d/cachyos.conf", "/var/cache/dms-greeter/settings.json", "/tmp/myarch-buildkit", "/etc/greetd/../passwd"} {
		if systemAllowed(path) {
			t.Errorf("unrelated path accepted: %s", path)
		}
	}
	for _, id := range []string{"../escape", "/absolute", ".", "..", "a/b", ""} {
		if validRunID(id) {
			t.Errorf("unsafe run identity accepted: %q", id)
		}
	}
}
func requireRootFS(t *testing.T) string {
	t.Helper()
	if os.Getuid() != 0 {
		t.Skip("root ownership tests use an isolated temporary filesystem")
	}
	return t.TempDir()
}
func TestPrivilegedWritesJournalOriginalAndRestoreExplicitly(t *testing.T) {
	root := requireRootFS(t)
	path := "/etc/greetd/config.toml"
	destination := rooted(root, path)
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("original"), 0640); err != nil {
		t.Fatal(err)
	}
	p := systemPayload{Path: path, Data: []byte("managed"), Mode: 0644, RunID: "run-1"}
	journal, err := privilegedWrite(root, 1000, p)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.Existed || journal.Backup == "" || journal.SHA256 != systemDigest(p.Data) {
		t.Fatalf("incomplete original journal: %+v", journal)
	}
	p.Data = []byte("managed-again")
	if _, err = privilegedWrite(root, 1000, p); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(journal.Backup)
	if err != nil || string(old) != "original" {
		t.Fatalf("first original was overwritten: %q %v", old, err)
	}
	if err = privilegedRestore(root, 1000, p); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(destination)
	if err != nil || string(restored) != "original" {
		t.Fatalf("original not restored: %q %v", restored, err)
	}
	info, _ := os.Stat(destination)
	if info.Mode().Perm() != 0640 {
		t.Fatalf("original mode lost: %o", info.Mode().Perm())
	}
}
func TestPrivilegedRestoreProtectsLaterEdits(t *testing.T) {
	root := requireRootFS(t)
	p := systemPayload{Path: "/etc/greetd/config.toml", Data: []byte("managed"), Mode: 0644, RunID: "run-2"}
	if _, err := privilegedWrite(root, 1000, p); err != nil {
		t.Fatal(err)
	}
	destination := rooted(root, p.Path)
	if err := os.WriteFile(destination, []byte("personal change"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := privilegedRestore(root, 1000, p); err == nil {
		t.Fatal("later edits were overwritten")
	}
	data, _ := os.ReadFile(destination)
	if string(data) != "personal change" {
		t.Fatal("later edits not preserved")
	}
}
func TestPrivilegedWriterRejectsSymlinkAndWritableAncestor(t *testing.T) {
	root := requireRootFS(t)
	path := rooted(root, "/etc/greetd")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), path); err != nil {
		t.Fatal(err)
	}
	p := systemPayload{Path: "/etc/greetd/config.toml", Data: []byte("managed"), Mode: 0644, RunID: "run-3"}
	if _, err := privilegedWrite(root, 1000, p); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := privilegedWrite(root, 1000, p); err == nil {
		t.Fatal("writable ancestor accepted")
	}
	p.Mode = 0777
	if _, err := privilegedWrite(root, 1000, p); err == nil {
		t.Fatal("unsafe mode accepted")
	}
}
func TestSystemRestoreNeverTrustsCallerBackupPath(t *testing.T) {
	root := requireRootFS(t)
	p := systemPayload{Path: "/etc/greetd/config.toml", Data: []byte("managed"), Mode: 0644, RunID: "run-4"}
	if _, err := privilegedWrite(root, 1000, p); err != nil {
		t.Fatal(err)
	}
	if err := privilegedRestore(root, 1001, p); err == nil {
		t.Fatal("different sudo user journal accepted")
	}
	journalPath, _, _ := rootJournalPaths(root, 1000, p.RunID, p.Path)
	data, _ := os.ReadFile(journalPath)
	var journal systemJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	journal.Existed = true
	journal.Backup = "/etc/passwd"
	if err := saveSystemJournal(journalPath, journal); err != nil {
		t.Fatal(err)
	}
	if err := privilegedRestore(root, 1000, p); err == nil {
		t.Fatal("arbitrary backup source accepted")
	}
}

func TestRootStatRejectsWritableLinkedAndUnownedPaths(t *testing.T) {
	for _, text := range []string{"1000:755:directory", "0:777:directory", "0:755:symbolic link", "0:755:socket", "invalid"} {
		if validateRootStat("/x", text) == nil {
			t.Fatalf("unsafe ownership accepted: %s", text)
		}
	}
	for _, text := range []string{"0:755:directory", "0:755:regular file", "0:644:regular file"} {
		if err := validateRootStat("/x", text); err != nil {
			t.Fatal(err)
		}
	}
}
func TestLegacySystemRestoreKeepsRemovedSettingsRestoreOnly(t *testing.T) {
	for _, path := range []string{"/usr/local/lib/myarch-buildkit/login.py", "/etc/systemd/logind.conf.d/60-myarch-buildkit-lid.conf", "/var/cache/dms-greeter/settings.json"} {
		if !legacySystemAllowed(path) || systemAllowed(path) {
			t.Errorf("legacy path should be restore-only: %s", path)
		}
	}
	if legacySystemAllowed("/etc/systemd/logind.conf.d/unrelated.conf") {
		t.Fatal("unrelated root configuration accepted")
	}
	root := requireRootFS(t)
	destination := "/etc/systemd/logind.conf.d/60-myarch-buildkit-lid.conf"
	actual := rooted(root, destination)
	if err := os.MkdirAll(filepath.Dir(actual), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actual, []byte("managed"), 0644); err != nil {
		t.Fatal(err)
	}
	run := "/home/tomi/run"
	backup := filepath.Join(run, "backups/profile/system-files", systemDigest([]byte(destination)))
	actualBackup := rooted(root, backup)
	if err := os.MkdirAll(filepath.Dir(actualBackup), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actualBackup, []byte("old policy"), 0600); err != nil {
		t.Fatal(err)
	}
	record := map[string]any{"path": destination, "backup": backup, "sha256": systemDigest([]byte("managed")), "previous": map[string]any{"mode": float64(0644), "uid": float64(0), "gid": float64(0)}}
	p := systemPayload{LegacyRecord: record, LegacyRunDir: run}
	if err := privilegedLegacyRestore(root, int(os.Getuid()), p); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(actual)
	if string(data) != "old policy" {
		t.Fatalf("legacy original not restored: %q", data)
	}
	record["backup"] = "/etc/passwd"
	if err := os.WriteFile(actual, []byte("managed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := privilegedLegacyRestore(root, int(os.Getuid()), p); err == nil {
		t.Fatal("unrelated original backup accepted")
	}
}
