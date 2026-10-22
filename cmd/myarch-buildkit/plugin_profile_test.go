package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginProfileEnablesPreservesAndReruns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	for _, directory := range []string{"dms", "DankMaterialShell"} {
		t.Run(directory, func(t *testing.T) {
			home := t.TempDir()
			s := profileSettings(home)
			s.DMSConfigDir = directory
			root := DMSConfigHome(home, s)
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			prefs := []byte(`{"dockerManager":{"enabled":false,"pollingInterval":9000},"otherPlugin":{"custom":42},"clipboardPlus":{"pinned":"keep"}}`)
			if err := os.WriteFile(filepath.Join(root, "plugin_settings.json"), prefs, 0600); err != nil {
				t.Fatal(err)
			}
			settings := []byte(`{"personal":123,"barConfigs":[{"id":"main","enabled":true,"position":2,"custom":true,"leftWidgets":[{"id":"bongoCat","enabled":false,"size":99}],"centerWidgets":["clock"],"rightWidgets":["systemTray"]},{"id":"other","enabled":true,"leftWidgets":[],"rightWidgets":["kubernetes"]}]}`)
			if err := os.WriteFile(filepath.Join(root, "settings.json"), settings, 0600); err != nil {
				t.Fatal(err)
			}
			changes, err := BuildProfile(home, s)
			if err != nil {
				t.Fatal(err)
			}
			var activated map[string]map[string]any
			var planned map[string]any
			for _, change := range changes {
				if filepath.Base(change.Path) == "plugin_settings.json" {
					if !bytes.Equal(change.Original, prefs) || !change.Existed || !change.CheckOriginal {
						t.Fatal("missing original preference snapshot")
					}
					if err := json.Unmarshal(change.Data, &activated); err != nil {
						t.Fatal(err)
					}
				}
				if change.Path == filepath.Join(root, "settings.json") {
					if err := json.Unmarshal(change.Data, &planned); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.MkdirAll(filepath.Dir(change.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(change.Path, change.Data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, plugin := range RequestedPlugins {
				if activated[plugin.ID]["enabled"] != true {
					t.Fatalf("not enabled: %s", plugin.ID)
				}
			}
			if activated["dockerManager"]["dockerBinary"] != "podman" || activated["dockerManager"]["terminalApp"] != "ghostty" || activated["dockerManager"]["pollingInterval"] != float64(9000) || activated["otherPlugin"]["custom"] != float64(42) {
				t.Fatal("plugin preferences lost")
			}
			bars := planned["barConfigs"].([]any)
			first := bars[0].(map[string]any)
			if first["position"] != float64(2) || first["custom"] != true {
				t.Fatal("bar customization lost")
			}
			bongo := first["leftWidgets"].([]any)[0].(map[string]any)
			if bongo["enabled"] != true || bongo["size"] != float64(99) {
				t.Fatal("existing plugin widget options lost")
			}
			for _, value := range first["rightWidgets"].([]any) {
				if value == "kubernetes" || value == "bongoCat" || value == "emojiLauncher" {
					t.Fatal("duplicated existing placement or inserted launcher as widget")
				}
			}
			again, err := BuildProfile(home, s)
			if err != nil {
				t.Fatal(err)
			}
			for i := range changes {
				if !bytes.Equal(changes[i].Data, again[i].Data) {
					t.Fatalf("rerun drift: %s", changes[i].Path)
				}
			}
		})
	}
}

func TestPluginProfileRejectsInvalidExistingBarSchemas(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[null]`, `[]`, `[{"enabled":false}]`, `[{"rightWidgets":[null]}]`, `[{"rightWidgets":[{"enabled":true}]}]`, `[{"rightWidgets":"clock"}]`} {
		settings := map[string]json.RawMessage{"barConfigs": json.RawMessage(raw)}
		if err := addPluginWidgets(settings); err == nil {
			t.Fatalf("accepted unsupported bars: %s", raw)
		}
	}
}

func TestPluginProfileFreshBarIncludesCalendarAndControlCenter(t *testing.T) {
	settings := map[string]json.RawMessage{}
	if err := addPluginWidgets(settings); err != nil {
		t.Fatal(err)
	}
	for _, id := range append([]string{"clock", "controlCenterButton"}, pluginBarWidgets...) {
		if !bytes.Contains(settings["barConfigs"], []byte(`"`+id+`"`)) {
			t.Fatalf("missing %s", id)
		}
	}
}
