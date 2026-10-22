package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type forbiddenRunner struct{ t *testing.T }

func (r forbiddenRunner) Run(args ...string) error {
	r.t.Errorf("unexpected execution: %v", args)
	return fmt.Errorf("unexpected")
}
func (r forbiddenRunner) Output(args ...string) ([]byte, error) {
	r.t.Errorf("unexpected query: %v", args)
	return nil, fmt.Errorf("unexpected")
}
func TestPlanIsOfflineAndDoesNotWrite(t *testing.T) {
	t.Setenv("NIRI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	var out bytes.Buffer
	if err := runApp(nil, home, forbiddenRunner{t}, &out); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(home)
	if len(files) != 0 {
		t.Fatal("plan wrote files")
	}
	if !bytes.Contains(out.Bytes(), []byte("No changes made")) {
		t.Fatal(out.String())
	}
}
func TestRejectInvalidSettings(t *testing.T) {
	for _, raw := range []string{`null`, `{"keyboard":null}`, `{"monitor":"eDP-1\nexec=bad"}`, `{"scale":"NaN"}`, `{"scale":"1/0"}`, `{"groups":["unknown"]}`, `{"kernel_channel":"stable"}`, `{} {}`} {
		p := filepath.Join(t.TempDir(), "settings.json")
		os.WriteFile(p, []byte(raw), 0600)
		if _, err := readSettings(p); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
func TestRetiredCommandsCannotApply(t *testing.T) {
	for _, args := range [][]string{{"apply-pending"}, {"--restore-stage", "desktop"}, {"apply", "--all"}, {"check", "--skip-packages"}} {
		if err := runApp(args, t.TempDir(), forbiddenRunner{t}, &bytes.Buffer{}); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

type migrationRunner struct {
	events  []string
	invalid bool
}

func (r *migrationRunner) Run(args ...string) error { r.events = append(r.events, args[0]); return nil }
func (r *migrationRunner) Output(args ...string) ([]byte, error) {
	r.events = append(r.events, args[0])
	if args[0] == "niri" && r.invalid {
		return nil, fmt.Errorf("invalid KDL")
	}
	return nil, nil
}
func TestFirstNiriInstallDoesNotRequireExistingValidator(t *testing.T) {
	changes, err := BuildProfile(t.TempDir(), defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	r := &migrationRunner{}
	if err := preparePackages([]Package{{Name: "niri"}}, changes, r, false, false); err != nil {
		t.Fatal(err)
	}
	for _, event := range r.events {
		if event == "niri" {
			t.Fatal("attempted validation before first Niri installation")
		}
	}
	if len(r.events) < 2 || r.events[len(r.events)-1] != "sudo" {
		t.Fatalf("missing installation: %v", r.events)
	}
	r = &migrationRunner{invalid: true}
	if err := preparePackages([]Package{{Name: "niri"}}, changes, r, false, true); err == nil {
		t.Fatal("invalid config passed existing validator")
	}
	if len(r.events) != 1 || r.events[0] != "niri" {
		t.Fatalf("packages changed despite validation failure: %v", r.events)
	}
	r = &migrationRunner{invalid: true}
	if err := preparePackages(nil, changes, r, true, false); err == nil {
		t.Fatal("skip-packages ignored unavailable/failed validator")
	}
}
func TestNiriConfigEnvironmentMustMatch(t *testing.T) {
	t.Setenv("NIRI_CONFIG", "/elsewhere/config.kdl")
	if err := runApp([]string{"plan"}, t.TempDir(), forbiddenRunner{t}, &bytes.Buffer{}); err == nil {
		t.Fatal("ignored NIRI_CONFIG mismatch")
	}
}

func TestOptionalGreeterPlanDoesNotExecuteInstaller(t *testing.T) {
	t.Setenv("NIRI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	var out bytes.Buffer
	if err := runApp([]string{"plan", "--setup-greeter"}, home, forbiddenRunner{t}, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("dms-greeter install interactively")) {
		t.Fatal(out.String())
	}
	files, _ := os.ReadDir(home)
	if len(files) != 0 {
		t.Fatal("preview wrote files")
	}
}

func TestCheckRejectsMutationOptions(t *testing.T) {
	for _, option := range []string{"--setup-greeter", "--cleanup-apps"} {
		if err := runApp([]string{"check", option}, t.TempDir(), forbiddenRunner{t}, &bytes.Buffer{}); err == nil {
			t.Fatalf("check accepted %s", option)
		}
	}
}

func TestLegacyQueueRecognizesCompletedInstall(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	for _, tc := range []struct {
		data  string
		empty bool
	}{
		{`[]`, true}, {`{"version":1,"items":[]}`, true},
		{`{"version":1,"items":[{}]}`, false}, {`{"version":1,"items":null}`, false},
		{`{"version":2,"items":[]}`, false}, {`{}`, false}, {`broken`, false},
	} {
		home := t.TempDir()
		path := filepath.Join(home, ".local/state/myarch-buildkit/pending-login.json")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := legacyPending(home); (err == nil) != tc.empty {
			t.Fatalf("queue %s: %v", tc.data, err)
		}
	}
}

func TestGreeterDefaultAndExplicitOptOut(t *testing.T) {
	t.Setenv("NIRI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"setup_greeter":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args    []string
		enabled bool
	}{
		{[]string{"plan"}, true},
		{[]string{"plan", "--setup-greeter=false"}, false},
		{[]string{"plan", "--settings", settingsPath}, false},
		{[]string{"plan", "--settings", settingsPath, "--setup-greeter"}, true},
	} {
		var out bytes.Buffer
		if err := runApp(tc.args, home, forbiddenRunner{t}, &out); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(out.Bytes(), []byte("dms-greeter install interactively")) != tc.enabled {
			t.Fatalf("wrong greeter plan for %v", tc.args)
		}
	}
}
