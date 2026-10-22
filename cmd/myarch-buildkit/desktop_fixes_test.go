package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaptopPreservesValidatedModeAndPlacement(t *testing.T) {
	c, _ := desktopContext(t)
	c.Inventory["monitors"] = []map[string]any{{"name": "eDP-1", "width": 1280, "height": 720, "refreshRate": 59.9401, "x": -960, "y": 1080, "transform": 1, "availableModes": []string{"1366x768@60.00Hz", "1280x720@59.94Hz"}}}
	if err := PreflightLaptop(c); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"lua", "hyprlang"} {
		text, err := LaptopConfigText(format, object(c.Report["laptop_scale_selection"]), "hu", true)
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"1280x720@59.9401", "-960x1080", "transform", "1.3333333333333333"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("%s lost %s: %s", format, expected, text)
			}
		}
		if strings.Contains(text, "preferred") || strings.Contains(text, "auto") || strings.Contains(text, "1366") {
			t.Fatalf("unverified mode selected: %s", text)
		}
	}
}

func TestLaptopIncompleteOrDisabledModePreservesFiles(t *testing.T) {
	for _, field := range []string{"width", "height", "refreshRate", "x", "y", "disabled"} {
		t.Run(field, func(t *testing.T) {
			c, _ := desktopContext(t)
			monitor := objects(c.Inventory["monitors"])[0]
			if field == "disabled" {
				monitor[field] = true
			} else {
				delete(monitor, field)
			}
			if err := ConfigureLaptop(c); err == nil || len(c.Pending.Files) != 0 {
				t.Fatalf("incomplete hardware information was applied: %v", err)
			}
		})
	}
}

func TestLaptopMalformedObservedNumbersPreserveFiles(t *testing.T) {
	for _, field := range []string{"width", "height", "refreshRate", "x", "y", "transform"} {
		for _, invalid := range []any{"0", false, nil, math.NaN(), math.Inf(1)} {
			t.Run(fmt.Sprintf("%s/%v", field, invalid), func(t *testing.T) {
				c, _ := desktopContext(t)
				objects(c.Inventory["monitors"])[0][field] = invalid
				if err := ConfigureLaptop(c); err == nil || len(c.Pending.Files) != 0 {
					t.Fatalf("malformed monitor number was applied: %v", err)
				}
			})
		}
	}
	for _, field := range []string{"width", "height", "x", "y", "transform"} {
		c, _ := desktopContext(t)
		objects(c.Inventory["monitors"])[0][field] = 1.5
		if err := ConfigureLaptop(c); err == nil {
			t.Fatalf("fractional %s accepted", field)
		}
	}
}

func TestDesktopAutostartUsesXDGPriority(t *testing.T) {
	for _, test := range []struct {
		high, low, user string
		want            int
	}{
		{"Exec=waybar", "Hidden=true", "", 1},
		{"Hidden=true", "Exec=waybar", "", 0},
		{"Exec=waybar", "Exec=waybar", "Hidden=true", 0},
		{"Hidden=true", "Hidden=true", "Exec=waybar", 1},
	} {
		c, _ := desktopContext(t)
		high, low := filepath.Join(c.Home, "high"), filepath.Join(c.Home, "low")
		storagePut(t, filepath.Join(high, "autostart/shell.desktop"), "[Desktop Entry]\n"+test.high+"\n")
		storagePut(t, filepath.Join(low, "autostart/shell.desktop"), "[Desktop Entry]\n"+test.low+"\n")
		if test.user != "" {
			storagePut(t, filepath.Join(c.ConfigHome, "autostart/shell.desktop"), "[Desktop Entry]\n"+test.user+"\n")
		}
		t.Setenv("XDG_CONFIG_DIRS", high+":"+low)
		overrides, err := DesktopAutostarts(c)
		if err != nil || len(overrides) != test.want {
			t.Fatalf("%+v: %v %v", test, overrides, err)
		}
	}
}

func TestDesktopPinsRespectTombstonesAndAllowNoDisplay(t *testing.T) {
	c, _ := desktopContext(t)
	path := filepath.Join(c.DataHome, "applications/com.mitchellh.ghostty.desktop")
	storagePut(t, path, "[Desktop Entry]\nType=Application\nHidden=true\n")
	storagePut(t, filepath.Join(os.Getenv("XDG_DATA_DIRS"), "applications/com.mitchellh.ghostty.desktop"), "[Desktop Entry]\nType=Application\nExec=ghostty\n")
	if _, err := DesktopPins(c); err == nil {
		t.Fatal("user tombstone was bypassed")
	}
	storagePut(t, path, "[Desktop Entry]\nType=Application\nNoDisplay=true\nExec=ghostty\n")
	if pins, err := DesktopPins(c); err != nil || pins[0] != "com.mitchellh.ghostty" {
		t.Fatalf("explicit NoDisplay pin rejected: %v %v", pins, err)
	}
	storagePut(t, path, "[Desktop Entry]\nType=Link\nURL=https://example.com\n")
	if _, err := DesktopPins(c); err == nil {
		t.Fatal("non-application pin accepted")
	}
	storagePut(t, filepath.Join(c.DataHome, "applications/ghostty.desktop"), "[Desktop Entry]\nType=Application\nExec=ghostty\n")
	if pins, err := DesktopPins(c); err != nil || pins[0] != "ghostty" {
		t.Fatalf("valid alternative pin not selected: %v %v", pins, err)
	}
}

func TestPluginCommitReplayVerifiesResolvedRevision(t *testing.T) {
	c, r := desktopContext(t)
	commit := strings.Repeat("a", 40)
	c.Settings.PluginCommits = map[string]string{"dockerManager": commit}
	if err := ConfigurePlugins(c); err != nil {
		t.Fatal(err)
	}
	for _, record := range reportRecords(c.Report, "plugins") {
		if record["id"] == "dockerManager" && (record["policy"] != "pinned" || record["ref"] != commit || record["commit"] != commit) {
			t.Fatal(record)
		}
	}
	found := false
	for _, command := range r.calls {
		if len(command.Args) > 3 && command.Args[3] == "fetch" && strings.Contains(strings.Join(command.Args, " "), "DmsDockerManager") {
			found = command.Args[len(command.Args)-1] == commit
		}
	}
	if !found {
		t.Fatal("pinned fetch did not use the exact requested commit")
	}
	c.Settings.PluginCommits["dockerManager"] = strings.Repeat("b", 40)
	if _, _, err := FetchPlugin(c, RequestedPlugins[0]); err == nil {
		t.Fatal("mismatching resolved commit accepted")
	}
}
