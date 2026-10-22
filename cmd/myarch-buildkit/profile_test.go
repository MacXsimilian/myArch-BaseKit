package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfilePreservesUnknownDMSPreferences(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config/dms/settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"personalTheme":"blue","nested":{"value":true},"showSeconds":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	changes, err := BuildProfile(home, profileSettings(home))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(changes[1].Data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["personalTheme"] != "blue" || settings["showSeconds"] != false || settings["nested"].(map[string]any)["value"] != true {
		t.Fatalf("unexpected settings: %v", settings)
	}
}

func TestProfileRejectsSymlinkedConfigurationDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildProfile(home, profileSettings(home)); err == nil {
		t.Fatal("followed configuration directory symlink")
	}
}

func TestProfileDefaultsToNiriAndLeavesHyprlandAlone(t *testing.T) {
	home := t.TempDir()
	hyprland := filepath.Join(home, ".config/hypr/hyprland.conf")
	if err := os.MkdirAll(filepath.Dir(hyprland), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("# existing Hyprland config\n")
	if err := os.WriteFile(hyprland, original, 0600); err != nil {
		t.Fatal(err)
	}
	s := profileSettings(home)
	s.ConfigPath = ""
	changes, err := BuildProfile(home, s)
	if err != nil {
		t.Fatal(err)
	}
	if changes[0].Path != filepath.Join(home, ".config/niri/config.kdl") {
		t.Fatal("wrong default configuration path")
	}
	for _, change := range changes {
		if strings.Contains(change.Path, "/hypr/") {
			t.Fatal("planned a Hyprland write")
		}
	}
	data, err := os.ReadFile(hyprland)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("changed existing Hyprland configuration")
	}
}

func TestProfileReplacesNiriConfigWithOriginalForBackup(t *testing.T) {
	home := t.TempDir()
	s := profileSettings(home)
	if err := os.MkdirAll(filepath.Dir(s.ConfigPath), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("// Personal Niri configuration\n")
	if err := os.WriteFile(s.ConfigPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	changes, err := BuildProfile(home, s)
	if err != nil {
		t.Fatal(err)
	}
	if !changes[0].CheckOriginal || !changes[0].Existed || !bytes.Equal(changes[0].Original, original) {
		t.Fatal("replacement lost original for backup")
	}
	if bytes.Equal(changes[0].Data, original) {
		t.Fatal("did not render a managed profile")
	}
	runner := &profileValidationRunner{}
	if err := ValidateProfile(changes, runner); err != nil {
		t.Fatal(err)
	}
	if !runner.sawCandidate {
		t.Fatal("did not validate scratch config")
	}
}

func profileSettings(home string) Settings {
	return Settings{Keyboard: "hu", Scale: "4/3", Monitor: "eDP-1", ConfigPath: filepath.Join(home, ".config/niri/config.kdl")}
}

func TestProfilePreservesShellFilesAndReruns(t *testing.T) {
	home := t.TempDir()
	s := profileSettings(home)
	if err := os.MkdirAll(filepath.Dir(s.ConfigPath), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("// Personal Niri config\n")
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("alias ll='ls -l'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ConfigPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	changes, err := BuildProfile(home, s)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(s.ConfigPath)
	if !bytes.Equal(current, original) {
		t.Fatal("preview changed live config")
	}
	for _, change := range changes {
		if err = os.MkdirAll(filepath.Dir(change.Path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(change.Path, change.Data, change.Mode); err != nil {
			t.Fatal(err)
		}
	}
	again, err := BuildProfile(home, s)
	if err != nil {
		t.Fatal(err)
	}
	for i := range changes {
		if !bytes.Equal(changes[i].Data, again[i].Data) {
			t.Fatalf("rerun changed %s", changes[i].Path)
		}
	}
	if !bytes.Equal(changes[0].Original, original) {
		t.Fatal("original config lost from backup input")
	}
	rc, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if !bytes.Contains(rc, []byte("alias ll='ls -l'")) || bytes.Count(rc, []byte("# myarch-buildkit shell tools")) != 1 {
		t.Fatal("shell config was not preserved idempotently")
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatal("created unused shell config")
	}
}

func TestProfileRejectsUnsafeOrUnsupportedInputs(t *testing.T) {
	home := t.TempDir()
	for _, mutate := range []func(*Settings){func(s *Settings) { s.Monitor = "eDP-1\nexec=bad" }, func(s *Settings) { s.Scale = "NaN" }, func(s *Settings) { s.Scale = "1/0" }, func(s *Settings) { s.ConfigPath = "/tmp/other.kdl" }, func(s *Settings) { s.ConfigPath = filepath.Join(home, "config.conf") }} {
		s := profileSettings(home)
		mutate(&s)
		if _, err := BuildProfile(home, s); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
}

type profileValidationRunner struct {
	fail         bool
	sawCandidate bool
}

func (r *profileValidationRunner) Run(args ...string) error { return errors.New("unexpected command") }
func (r *profileValidationRunner) Output(args ...string) ([]byte, error) {
	if len(args) != 4 || args[0] != "niri" || args[1] != "validate" || args[2] != "--config" {
		return nil, errors.New("unexpected command")
	}
	data, err := os.ReadFile(args[3])
	if err != nil {
		return nil, err
	}
	r.sawCandidate = strings.Contains(string(data), "spawn-at-startup")
	if r.fail {
		return []byte("invalid KDL"), errors.New("exit status 1")
	}
	return nil, nil
}

func TestProfileValidationNeverWritesLiveConfig(t *testing.T) {
	home := t.TempDir()
	s := profileSettings(home)
	changes, err := BuildProfile(home, s)
	if err != nil {
		t.Fatal(err)
	}
	runner := &profileValidationRunner{fail: true}
	if err = ValidateProfile(changes, runner); err == nil {
		t.Fatal("accepted native error")
	}
	if !runner.sawCandidate {
		t.Fatal("missing scratch candidate")
	}
	for _, change := range changes {
		if _, err = os.Stat(change.Path); !os.IsNotExist(err) {
			t.Fatalf("live path touched: %s", change.Path)
		}
	}
}

func TestProfileTargetsSelectedDMSDirectory(t *testing.T) {
	home := t.TempDir()
	for _, directory := range []string{"dms", "DankMaterialShell"} {
		s := defaultSettings()
		s.DMSConfigDir = directory
		changes, err := BuildProfile(home, s)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range changes {
			if c.Path == filepath.Join(home, ".config", directory, "settings.json") {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing settings for %s", directory)
		}
	}
}
