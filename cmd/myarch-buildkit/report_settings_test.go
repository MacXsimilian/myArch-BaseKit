package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func saveSettingsDocument(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func settingsReportEnvelope(settings any) map[string]any {
	return map[string]any{"format": RunReportFormat, "schema_version": RunReportSchemaVersion, "settings": settings, "stages": map[string]any{"defaults": map[string]any{"status": "completed"}}, "status": "partial"}
}

func settingsSnapshotObject(t *testing.T) map[string]any {
	t.Helper()
	data, err := json.Marshal(DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]any{}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSettingsFromRunReportPreservesEffectiveSettings(t *testing.T) {
	want := DefaultSettings()
	want.PackageGroups["utilities"] = false
	want.PackageGroups["development"] = false
	want.Laptop.InternalScale = "3/2"
	want.Laptop.InternalKeyboard = "de"
	want.Laptop.NaturalScroll = false
	report := settingsReportEnvelope(want)
	report["file_changes"] = []any{map[string]any{"path": "/unused", "sha256": "unused"}}
	path := saveSettingsDocument(t, report)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot changed: got %+v, want %+v, error %v", got, want, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("reading report settings modified the report")
	}
}

func TestSettingsRejectUnknownRunReportEnvelope(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(r map[string]any) { r["format"] = "another-program" },
		func(r map[string]any) { r["format"] = nil },
		func(r map[string]any) { r["schema_version"] = RunReportSchemaVersion + 1 },
		func(r map[string]any) { r["schema_version"] = "3" },
		func(r map[string]any) { delete(r, "schema_version") },
		func(r map[string]any) { delete(r, "settings") },
		func(r map[string]any) { r["settings"] = nil },
		func(r map[string]any) { r["settings"] = []any{} },
		func(r map[string]any) { r["settings"] = "/home/user/effective-settings.json" },
	} {
		report := settingsReportEnvelope(DefaultSettings())
		mutate(report)
		if _, err := LoadSettings(saveSettingsDocument(t, report)); err == nil {
			t.Fatalf("unsupported report accepted: %+v", report)
		}
	}
}

func TestSettingsRejectIncompleteEffectiveSnapshot(t *testing.T) {
	for _, path := range [][]string{{"schema_version"}, {"package_groups"}, {"laptop"}, {"package_groups", "core"}, {"package_groups", "utilities"}, {"package_groups", "development"}, {"package_groups", "containers"}, {"laptop", "internal_scale"}, {"laptop", "internal_keyboard"}, {"laptop", "natural_scroll"}} {
		snapshot := settingsSnapshotObject(t)
		if len(path) == 1 {
			delete(snapshot, path[0])
		} else {
			delete(snapshot[path[0]].(map[string]any), path[1])
		}
		if _, err := LoadSettings(saveSettingsDocument(t, settingsReportEnvelope(snapshot))); err == nil {
			t.Errorf("snapshot missing %v inherited defaults", path)
		}
	}
}

func TestSettingsRunReportUsesStrictSettingsValidation(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(s map[string]any) { s["theme"] = "custom" },
		func(s map[string]any) { s["schema_version"] = 1 },
		func(s map[string]any) { s["package_groups"] = nil },
		func(s map[string]any) { s["laptop"] = nil },
		func(s map[string]any) { s["package_groups"].(map[string]any)["other"] = true },
		func(s map[string]any) { s["package_groups"].(map[string]any)["core"] = false },
		func(s map[string]any) { s["package_groups"].(map[string]any)["utilities"] = "false" },
		func(s map[string]any) { s["laptop"].(map[string]any)["natural_scroll"] = nil },
		func(s map[string]any) { s["laptop"].(map[string]any)["lid_action"] = "suspend" },
		func(s map[string]any) { s["laptop"].(map[string]any)["internal_scale"] = "4/0" },
		func(s map[string]any) { s["laptop"].(map[string]any)["internal_keyboard"] = "hu,us" },
	} {
		snapshot := settingsSnapshotObject(t)
		mutate(snapshot)
		if _, err := LoadSettings(saveSettingsDocument(t, settingsReportEnvelope(snapshot))); err == nil {
			t.Fatalf("invalid embedded settings accepted: %+v", snapshot)
		}
	}
}

func TestSettingsLegacyFilesKeepPartialMerging(t *testing.T) {
	for _, name := range []string{"settings.json", "effective-settings.json"} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(`{"package_groups":{"utilities":false},"laptop":{"natural_scroll":false}}`), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadSettings(path)
		if err != nil || got.PackageGroups["utilities"] || got.Laptop.NaturalScroll || !got.PackageGroups["development"] || got.Laptop.InternalScale != "4/3" {
			t.Fatalf("legacy partial settings changed: %+v, error %v", got, err)
		}
	}
	if _, err := LoadSettings(saveSettingsDocument(t, map[string]any{"settings": DefaultSettings(), "schema_version": RunReportSchemaVersion})); err == nil {
		t.Fatal("unmarked document was treated as a run report")
	}
}
