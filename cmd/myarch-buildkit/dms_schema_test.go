package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installDMSSchemaFixture(t *testing.T, root, version string) {
	t.Helper()
	source := filepath.Join("testdata", "dms", version)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".js" {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		storagePut(t, filepath.Join(root, relative), string(raw))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only the declarations are needed from QML; the exact upstream source
	// hashes and declaration text are retained in versions.json.
	var versions struct{ Settings, Session int }
	metadata, err := os.ReadFile(filepath.Join(source, "versions.json"))
	if err != nil || json.Unmarshal(metadata, &versions) != nil || versions.Settings == 0 || versions.Session == 0 {
		t.Fatal("invalid fixture schema versions")
	}
	storagePut(t, filepath.Join(root, "Common/SettingsData.qml"), fmt.Sprintf("property int settingsConfigVersion: %d\n", versions.Settings))
	storagePut(t, filepath.Join(root, "Common/SessionData.qml"), fmt.Sprintf("property int sessionConfigVersion: %d\n", versions.Session))
}

func TestDMSSchemaPinnedSourceFixtures(t *testing.T) {
	for _, version := range []string{"1.6.2", "2.0.0"} {
		t.Run(version, func(t *testing.T) {
			c, _ := desktopContext(t)
			root := filepath.Join(c.Home, "qml")
			installDMSSchemaFixture(t, root, version)
			if !inspectDMSSchema(c, root) {
				t.Fatal("pinned upstream schema did not validate")
			}
			var sources []struct{ Path, SHA256 string }
			raw, err := os.ReadFile(filepath.Join("testdata", "dms", version, "sources.json"))
			if err != nil || json.Unmarshal(raw, &sources) != nil {
				t.Fatal("invalid fixture source manifest")
			}
			for _, source := range sources {
				raw, err := os.ReadFile(filepath.Join("testdata", "dms", version, source.Path))
				if err != nil || hashBytes(raw) != source.SHA256 {
					t.Fatalf("upstream fixture changed without source update: %s", source.Path)
				}
			}
		})
	}
}

func TestDMSSchemaRejectsChangedTypesAndCommentedFields(t *testing.T) {
	for _, test := range []struct{ name, path, before, after string }{
		{"setting type", "Common/settings/SettingsSpec.js", "frameEnabled: {\n        def: false", "frameEnabled: {\n        def: \"false\""},
		{"commented setting", "Common/settings/SettingsSpec.js", "frameEnabled: {\n        def: false\n    },", "/* frameEnabled: { def: false }, */"},
		{"dock shape", "Common/settings/DockConfig.js", "groupByApp: false", "groupByApp: \"false\""},
		{"bar shape", "Common/settings/SettingsSpec.js", "leftWidgets: [\"launcherButton\", \"workspaceSwitcher\", \"focusedWindow\"]", "leftWidgets: {}"},
		{"session type", "Common/settings/SessionSpec.js", "dockPins: {\n        def: {}", "dockPins: {\n        def: []"},
		{"string expression", "Common/settings/SettingsSpec.js", "frameMode: {\n        def: \"connected\"", "frameMode: {\n        def: \"connected\" + unknown()"},
		{"array expression", "Common/settings/SettingsSpec.js", "frameScreenPreferences: {\n        def: [\"all\"]", "frameScreenPreferences: {\n        def: [\"all\"] + unknown()"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := desktopContext(t)
			root := filepath.Join(c.Home, "qml")
			installDMSSchemaFixture(t, root, "2.0.0")
			path := filepath.Join(root, test.path)
			raw := storageRead(t, path)
			if !strings.Contains(raw, test.before) {
				t.Fatal("fixture replacement did not match")
			}
			storagePut(t, path, strings.Replace(raw, test.before, test.after, 1))
			if inspectDMSSchema(c, root) {
				t.Fatal("incompatible schema accepted")
			}
		})
	}
}

func TestDMSSchemaDefaultTypesRequireCompleteLiteral(t *testing.T) {
	for _, expression := range []string{`'text' + unknown()`, `"text" + unknown()`, `[1] + [2]`, `{} || {}`, `true && unknown()`, `[}`, `"unterminated`} {
		if kind := dmsValueType(expression); kind != "" {
			t.Fatalf("expression inferred as %s: %s", kind, expression)
		}
	}
}
