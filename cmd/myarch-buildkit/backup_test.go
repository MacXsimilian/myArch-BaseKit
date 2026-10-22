package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupsAndRepeatApply(t *testing.T) {
	home := t.TempDir()
	a := filepath.Join(home, "existing")
	b := filepath.Join(home, "new")
	os.WriteFile(a, []byte("personal"), 0640)
	changes := []FileChange{{Path: a, Data: []byte("configured")}, {Path: b, Data: []byte("created")}}
	backup, err := ApplyFiles(home, changes)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(backup, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []BackupEntry
	if err = json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].Existed || entries[1].Existed {
		t.Fatalf("manifest: %+v", entries)
	}
	saved, _ := os.ReadFile(filepath.Join(backup, entries[0].Backup))
	if string(saved) != "personal" {
		t.Fatalf("original lost: %s", saved)
	}
	info, _ := os.Stat(a)
	if info.Mode().Perm() != 0640 {
		t.Fatal("permissions changed")
	}
	again, err := ApplyFiles(home, changes)
	if err != nil || again != "" {
		t.Fatalf("repeat not a no-op: %q %v", again, err)
	}
}
func TestUnsafeBatchDoesNotPartiallyWrite(t *testing.T) {
	home := t.TempDir()
	a := filepath.Join(home, "first")
	outside := filepath.Join(t.TempDir(), "outside")
	link := filepath.Join(home, "linked")
	os.WriteFile(a, []byte("original"), 0600)
	os.WriteFile(outside, []byte("outside"), 0600)
	os.Symlink(outside, link)
	if _, err := ApplyFiles(home, []FileChange{{Path: a, Data: []byte("new")}, {Path: link, Data: []byte("bad")}}); err == nil {
		t.Fatal("accepted symlink")
	}
	data, _ := os.ReadFile(a)
	if string(data) != "original" {
		t.Fatal("partial mutation before validation")
	}
	if _, err := ApplyFiles(home, []FileChange{{Path: outside, Data: []byte("bad")}}); err == nil {
		t.Fatal("escaped home")
	}
}
func TestStaleProfileRefusesOverwrite(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config")
	os.WriteFile(path, []byte("new personal edit"), 0600)
	_, err := ApplyFiles(home, []FileChange{{Path: path, Data: []byte("managed"), Original: []byte("old"), Existed: true, CheckOriginal: true}})
	if err == nil {
		t.Fatal("accepted stale render")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new personal edit" {
		t.Fatal("personal edit overwritten")
	}
}
func TestLegacyPendingBlocksNewApply(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".local/state/myarch-buildkit/pending-login.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte(`[{"phase":"desktop"}]`), 0600)
	if legacyPending(home) == nil {
		t.Fatal("legacy pending ignored")
	}
	os.WriteFile(p, []byte(`[]`), 0600)
	if err := legacyPending(home); err != nil {
		t.Fatal(err)
	}
}
func TestDesktopProcessBlocksApply(t *testing.T) {
	proc := t.TempDir()
	p := filepath.Join(proc, "123")
	os.Mkdir(p, 0700)
	os.WriteFile(filepath.Join(p, "cmdline"), []byte("niri\x00--session\x00"), 0600)
	if noDesktop(proc, os.Getuid()) == nil {
		t.Fatal("running desktop ignored")
	}
}
