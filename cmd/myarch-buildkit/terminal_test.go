package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTerminalPreferencesPreserveFallbacksAndSession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	paths := map[string]string{
		".config/xdg-terminals.list":      "# Personal fallback\nkitty.desktop\n" + ghosttyDesktopID + "\n/execarg_compat\n",
		".config/niri-xdg-terminals.list": "foot.desktop\n-" + ghosttyDesktopID + "\n",
		".local/state/dms/session.json":   `{"terminalOverride":"kitty","wallpaperPath":"/my/photo.png","nested":{"keep":true}}`,
	}
	for relative, data := range paths {
		path := filepath.Join(home, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	changes, err := BuildTerminalProfile(home, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(changes[0].Data, []byte(terminalListHeader+"\n"+ghosttyDesktopID+"\n")) || !bytes.Contains(changes[0].Data, []byte("kitty.desktop\n")) || !bytes.Contains(changes[0].Data, []byte("/execarg_compat\n")) {
		t.Fatal("terminal fallbacks or preference order lost")
	}
	var session map[string]any
	if err := json.Unmarshal(changes[3].Data, &session); err != nil {
		t.Fatal(err)
	}
	if session["terminalOverride"] != "ghostty" || session["wallpaperPath"] != "/my/photo.png" || session["nested"].(map[string]any)["keep"] != true {
		t.Fatal("personal session settings lost")
	}
	for _, change := range changes {
		if !change.CheckOriginal {
			t.Fatal("missing concurrent edit protection")
		}
		if change.Existed {
			current, _ := os.ReadFile(change.Path)
			if !bytes.Equal(current, change.Original) {
				t.Fatal("preview modified a live file")
			}
		}
		if err := os.MkdirAll(filepath.Dir(change.Path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(change.Path, change.Data, change.Mode); err != nil {
			t.Fatal(err)
		}
	}
	again, err := BuildTerminalProfile(home, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range changes {
		if !bytes.Equal(changes[i].Data, again[i].Data) {
			t.Fatalf("not idempotent: %s", changes[i].Path)
		}
	}
}

func TestTerminalSessionRespectsSelectedDMSVersionAndStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	changes, err := BuildTerminalProfile(home, Settings{DMSConfigDir: "DankMaterialShell"})
	if err != nil {
		t.Fatal(err)
	}
	if changes[3].Path != filepath.Join(home, "state/DankMaterialShell/session.json") {
		t.Fatalf("wrong session: %s", changes[3].Path)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := BuildTerminalProfile(home, Settings{}); err == nil {
		t.Fatal("accepted state outside home")
	}
}

func TestTerminalMalformedSessionIsPreserved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	path := filepath.Join(home, ".local/state/dms/session.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"null", "broken JSON", "[]"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := BuildTerminalProfile(home, Settings{}); err == nil {
			t.Fatal("accepted malformed session")
		}
		actual, _ := os.ReadFile(path)
		if string(actual) != data {
			t.Fatal("modified invalid session")
		}
	}
}

type terminalCheckRunner struct {
	selected string
	args     []string
}

func (r *terminalCheckRunner) Run(...string) error { return errors.New("must not launch a terminal") }
func (r *terminalCheckRunner) Output(args ...string) ([]byte, error) {
	r.args = args
	return []byte(r.selected + "\n"), nil
}
func TestTerminalCheckOnlyPrintsSelection(t *testing.T) {
	r := &terminalCheckRunner{selected: ghosttyDesktopID}
	if err := CheckTerminalPreferences(r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.args, []string{"xdg-terminal-exec", "--print-id"}) {
		t.Fatalf("unsafe check: %v", r.args)
	}
	r.selected = "kitty.desktop"
	if err := CheckTerminalPreferences(r); err == nil {
		t.Fatal("missed terminal override")
	}
}

func TestShellKindProviderOnlyForSelectedContainerDevelopment(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		groups []string
		kind   bool
	}{
		{[]string{"core"}, false}, {[]string{"containers"}, false}, {[]string{"development"}, false}, {[]string{"containers", "development"}, true},
	} {
		s := profileSettings(home)
		s.Groups = tc.groups
		changes, err := BuildProfile(home, s)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, change := range changes {
			if filepath.Base(change.Path) != "shell.bash" {
				continue
			}
			found = true
			if bytes.Contains(change.Data, []byte("export KIND_EXPERIMENTAL_PROVIDER=podman")) != tc.kind {
				t.Fatalf("wrong kind setup for %v", tc.groups)
			}
			if !bytes.Contains(change.Data, []byte("export TERMINAL=ghostty")) {
				t.Fatal("missing shell terminal preference")
			}
		}
		if !found {
			t.Fatal("missing managed shell")
		}
	}
}
