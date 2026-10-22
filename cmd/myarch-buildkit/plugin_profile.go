package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var pluginBarWidgets = []string{"dockerManager", "kubernetes", "bongoCat", "clipboardPlus"}

func pluginObject(data []byte, label string) (map[string]json.RawMessage, error) {
	object := map[string]json.RawMessage{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &object); err != nil || object == nil {
			return nil, fmt.Errorf("%s must be a JSON object; existing file preserved", label)
		}
	}
	return object, nil
}

// Both DMS 1.6.2 and Dux 2 store per-plugin preferences directly under the ID.
// The upstream CLI installs code; this backed-up change enables it at next login.
func AddPluginProfile(home string, s Settings, changes []FileChange) ([]FileChange, error) {
	root := DMSConfigHome(home, s)
	path := filepath.Join(root, "plugin_settings.json")
	if err := safeHomePath(home, path); err != nil {
		return nil, err
	}
	_, statErr := os.Lstat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	original, err := profileRead(path)
	if err != nil {
		return nil, err
	}
	prefs, err := pluginObject(original, path)
	if err != nil {
		return nil, err
	}
	for _, plugin := range RequestedPlugins {
		entry, err := pluginObject(prefs[plugin.ID], "preferences for "+plugin.ID)
		if err != nil {
			return nil, err
		}
		entry["enabled"] = json.RawMessage("true")
		if plugin.ID == "dockerManager" {
			entry["dockerBinary"] = json.RawMessage(`"podman"`)
			entry["terminalApp"] = json.RawMessage(`"ghostty"`)
		}
		if plugin.ID == "clipboardPlus" {
			entry["useDmsClipboard"] = json.RawMessage("true")
		}
		prefs[plugin.ID], err = json.Marshal(entry)
		if err != nil {
			return nil, err
		}
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return nil, err
	}
	found := false
	for i := range changes {
		if changes[i].Path != filepath.Join(root, "settings.json") {
			continue
		}
		settings, err := pluginObject(changes[i].Data, "planned Dux settings")
		if err != nil {
			return nil, err
		}
		if err := addPluginWidgets(settings); err != nil {
			return nil, err
		}
		encoded, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return nil, err
		}
		changes[i].Data = append(encoded, '\n')
		found = true
	}
	if !found {
		return nil, fmt.Errorf("plugin activation requires planned Dux settings")
	}
	return append(changes, FileChange{Path: path, Data: append(data, '\n'), Mode: 0600, Original: original, Existed: statErr == nil, CheckOriginal: true}), nil
}

func addPluginWidgets(settings map[string]json.RawMessage) error {
	var bars []map[string]json.RawMessage
	if raw, exists := settings["barConfigs"]; exists {
		if err := json.Unmarshal(raw, &bars); err != nil || bars == nil {
			return fmt.Errorf("barConfigs must be an array of bar objects")
		}
	} else {
		// A new profile gets the ordinary built-in clock/calendar and control
		// center as well as the requested third-party widgets.
		if err := json.Unmarshal([]byte(`[{"id":"default","name":"Main Bar","enabled":true,"position":0,"screenPreferences":["all"],"showOnLastDisplay":true,"leftWidgets":["launcherButton","workspaceSwitcher","focusedWindow"],"centerWidgets":["clock"],"rightWidgets":["systemTray","notificationButton","battery","controlCenterButton"]}]`), &bars); err != nil {
			return err
		}
	}
	placed := map[string]bool{}
	first := -1
	for i, bar := range bars {
		if bar == nil {
			return fmt.Errorf("barConfigs contains a non-object bar")
		}
		eligible := true
		for _, key := range []string{"enabled", "visible", "island"} {
			if raw, exists := bar[key]; exists {
				var value bool
				if err := json.Unmarshal(raw, &value); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					return fmt.Errorf("bar %s must be boolean", key)
				}
				if key == "island" && value || key != "island" && !value {
					eligible = false
				}
			}
		}
		if !eligible {
			continue
		}
		if first < 0 {
			first = i
		}
		for _, section := range []string{"leftWidgets", "centerWidgets", "rightWidgets"} {
			raw, exists := bar[section]
			if !exists {
				continue
			}
			var entries []json.RawMessage
			if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
				return fmt.Errorf("bar %s must be a widget array", section)
			}
			for j, entry := range entries {
				var id string
				var object map[string]json.RawMessage
				if json.Unmarshal(entry, &id) != nil {
					var err error
					object, err = pluginObject(entry, "bar widget")
					if err != nil {
						return err
					}
					key := object["id"]
					if len(key) == 0 {
						key = object["widgetId"]
					}
					if err := json.Unmarshal(key, &id); err != nil || id == "" {
						return fmt.Errorf("bar widget needs a string id")
					}
				}
				if id == "" {
					return fmt.Errorf("bar widget needs a nonempty id")
				}
				for _, wanted := range pluginBarWidgets {
					if id != wanted {
						continue
					}
					placed[id] = true
					if object != nil {
						object["enabled"] = json.RawMessage("true")
						entries[j], _ = json.Marshal(object)
					}
				}
			}
			bar[section], _ = json.Marshal(entries)
		}
	}
	if first < 0 {
		return fmt.Errorf("plugin widgets need an enabled visible regular bar; enable a bar in Dux settings before applying")
	}
	right := []json.RawMessage{}
	if raw, exists := bars[first]["rightWidgets"]; exists {
		if err := json.Unmarshal(raw, &right); err != nil {
			return err
		}
	}
	for _, id := range pluginBarWidgets {
		if !placed[id] {
			entry, _ := json.Marshal(id)
			right = append(right, entry)
		}
	}
	bars[first]["rightWidgets"], _ = json.Marshal(right)
	encoded, err := json.Marshal(bars)
	if err != nil {
		return err
	}
	settings["barConfigs"] = encoded
	return nil
}
