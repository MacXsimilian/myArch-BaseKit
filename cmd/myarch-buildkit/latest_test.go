package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatestPluginFetchUsesRemoteHEADAndRecordsCommit(t *testing.T) {
	c, r := desktopContext(t)
	if err := ConfigurePlugins(c); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, call := range r.calls {
		if len(call.Args) > 3 && call.Args[0] == "git" && call.Args[3] == "fetch" {
			count++
			if call.Args[len(call.Args)-1] != "HEAD" {
				t.Fatalf("fixed plugin revision: %v", call.Args)
			}
		}
	}
	if count != len(RequestedPlugins) {
		t.Fatalf("expected fresh upstream fetch for every plugin, got %d", count)
	}
	records := reportRecords(c.Report, "plugins")
	if len(records) != len(RequestedPlugins) {
		t.Fatal(records)
	}
	for _, record := range records {
		if record["commit"] != strings.Repeat("a", 40) || record["ref"] != "HEAD" {
			t.Fatal(record)
		}
	}
}

func TestLatestPluginFetchFailureDoesNotUseStaleCheckout(t *testing.T) {
	c, r := desktopContext(t)
	r.override = func(cmd Command) (CommandResult, bool) {
		if len(cmd.Args) > 3 && cmd.Args[0] == "git" && cmd.Args[3] == "fetch" {
			return CommandResult{Code: 1, Stderr: "offline"}, true
		}
		return CommandResult{}, false
	}
	storageRequireError(t, ConfigurePlugins(c), "offline")
	if len(c.Pending.Files) != 0 {
		t.Fatal("staged stale checkout after failed fetch")
	}
}

func TestPluginUpdatePreservesEditsAfterStaging(t *testing.T) {
	c, _ := desktopContext(t)
	root := filepath.Join(c.ConfigHome, "DankMaterialShell/plugins/dockerManager")
	storagePut(t, filepath.Join(root, "plugin.json"), `{"id":"dockerManager","component":"Widget.qml"}`)
	storagePut(t, filepath.Join(root, "Widget.qml"), "original")
	obsolete := filepath.Join(root, "obsolete.qml")
	storagePut(t, obsolete, "old")
	snapshot, _ := json.Marshal(pluginSnapshot{Destination: root, Files: map[string]string{"obsolete.qml": hashBytes([]byte("old"))}})
	storagePut(t, pluginSnapshotPath(c, "dockerManager"), string(snapshot))
	if err := ConfigurePlugins(c); err != nil {
		t.Fatal(err)
	}
	storagePublish(t, c)
	storagePut(t, obsolete, "edit after staging")
	storageRequireError(t, ApplyPending(c), "changed since staging")
	if storageRead(t, obsolete) != "edit after staging" || storageRead(t, filepath.Join(root, "Widget.qml")) != "original" {
		t.Fatal("partial plugin update changed live files")
	}
}

func TestStagedDeletionRequiresNewManifestVersion(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "plugin/old.qml")
	storagePut(t, path, "old")
	if err := c.StartStaging(); err != nil {
		t.Fatal(err)
	}
	if err := c.stageDelete(path); err != nil {
		t.Fatal(err)
	}
	if c.Pending.Version != 3 {
		t.Fatal("old executables could misread deletion")
	}
	c.Pending.Version = 1
	storagePublish(t, c)
	storageRequireError(t, ApplyPending(c), "invalid staged deletion")
	if storageRead(t, path) != "old" {
		t.Fatal("invalid deletion changed file")
	}
}

func TestDux2StagesModernPathsAndSchemaAndAppliesWithoutNetwork(t *testing.T) {
	c, r := desktopContext(t)
	r.dmsVersion, r.packageVersion = "2.0.0", "2.0.0-1"
	c.Inventory["hyprland_version"] = "Hyprland 0.99.0"
	main := filepath.Join(c.ConfigHome, "hypr/hyprland.lua")
	storagePut(t, main, "-- original")
	c.Inventory["active_config"] = main
	settingsPath := filepath.Join(c.ConfigHome, "dms/settings.json")
	sessionPath := filepath.Join(c.StateHome, "dms/session.json")
	storagePut(t, settingsPath, `{"configVersion":42,"currentThemeName":"personal"}`)
	storagePut(t, sessionPath, `{"configVersion":7,"personal":"preserved"}`)
	// The migration compatibility symlinks must not be used for staged writes.
	if err := os.Symlink("dms", filepath.Join(c.ConfigHome, "DankMaterialShell")); err != nil {
		t.Fatal(err)
	}
	desktopStage(t, c)
	settings := desktopObject(t, c.View(settingsPath))
	if settings["configVersion"] != float64(42) || settings["currentThemeName"] != "personal" {
		t.Fatal(settings)
	}
	docks, ok := settings["dockConfigs"].([]any)
	if !ok || len(docks) != 1 || object(docks[0])["enabled"] != true || object(docks[0])["position"] != float64(1) {
		t.Fatal(settings["dockConfigs"])
	}
	if _, old := settings["showDock"]; old {
		t.Fatal("obsolete dock setting written")
	}
	if len(stringList(object(desktopObject(t, c.View(sessionPath))["dockPins"])["dock"])) != 3 {
		t.Fatal("Dux dock pins missing")
	}
	if desktopObject(t, c.View(sessionPath))["configVersion"] != float64(7) {
		t.Fatal("old session schema written")
	}
	if err := ValidateDesktop(c); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish("desktop"); err != nil {
		t.Fatal(err)
	}
	storagePut(t, settingsPath, `{"configVersion":42,"currentThemeName":"changed while staged","runtimePreference":42}`)
	r.calls = nil
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	settings = desktopObject(t, settingsPath)
	if settings["currentThemeName"] != "changed while staged" || settings["runtimePreference"] != float64(42) {
		t.Fatal("late preferences lost")
	}
	if !exists(filepath.Join(c.ConfigHome, "dms/plugins/dockerManager/plugin.json")) {
		t.Fatal("plugin installed to wrong path")
	}
	for _, call := range r.calls {
		if call.Args[0] == "git" {
			t.Fatal("login attempted network access")
		}
	}
}

func TestDux2RefusesToBypassPendingUserDataMigration(t *testing.T) {
	c, r := desktopContext(t)
	r.dmsVersion, r.packageVersion = "2.0.0", "2.0.0-1"
	path := filepath.Join(c.ConfigHome, "DankMaterialShell/settings.json")
	original := `{"configVersion":18,"personal":true}`
	storagePut(t, path, original)
	storageRequireError(t, ConfigureDesktop(c), "migration is pending")
	if len(c.Pending.Files) != 0 || storageRead(t, path) != original {
		t.Fatal("unmigrated preferences changed")
	}
}

func TestDesktopLoginRejectsShellUpgradeSinceStaging(t *testing.T) {
	c, r := desktopContext(t)
	r.dmsVersion, r.packageVersion = "2.0.0", "2.0.0-1"
	desktopStage(t, c)
	if err := ValidateDesktop(c); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish("desktop"); err != nil {
		t.Fatal(err)
	}
	r.dmsVersion = "2.0.1"
	storageRequireError(t, ApplyPending(c), "version changed since staging")
	if exists(filepath.Join(c.ConfigHome, "dms/settings.json")) {
		t.Fatal("unvalidated shell update applied configuration")
	}
}

func TestNewHyprlandStillRequiresNativeValidation(t *testing.T) {
	c, r := desktopContext(t)
	c.Inventory["hyprland_version"] = "Hyprland 1.99.0"
	desktopStage(t, c)
	r.override = func(cmd Command) (CommandResult, bool) {
		if strings.Contains(strings.Join(cmd.Args, " "), "--verify-config") {
			return CommandResult{Code: 1, Stderr: "unsupported syntax"}, true
		}
		return CommandResult{}, false
	}
	storageRequireError(t, ValidateDesktop(c), "pending Hyprland config is invalid")
	if boolValue(c.Pending.Validation["native_config_verified"]) {
		t.Fatal("failed native validation accepted")
	}
}

func TestFutureDMSReadsInstalledSchemaVersions(t *testing.T) {
	c, r := desktopContext(t)
	r.dmsVersion, r.packageVersion = "2.1.0", "2.1.0-1"
	root := filepath.Join(c.Home, "qml")
	r.sourceRoot = root
	installDMSSchemaFixture(t, root, "2.0.0")
	storagePut(t, filepath.Join(root, "Common/SettingsData.qml"), "readonly property int settingsConfigVersion: 43")
	storagePut(t, filepath.Join(root, "Common/SessionData.qml"), "readonly property int sessionConfigVersion: 8")
	desktopStage(t, c)
	if desktopObject(t, c.View(filepath.Join(c.ConfigHome, "dms/settings.json")))["configVersion"] != float64(43) {
		t.Fatal("future settings version replaced with old schema")
	}
	if desktopObject(t, c.View(filepath.Join(c.StateHome, "dms/session.json")))["configVersion"] != float64(8) {
		t.Fatal("future session version replaced with old schema")
	}
	for _, call := range r.calls {
		if len(call.Args) > 2 && strings.Contains(strings.Join(call.Args, " "), "dms-schema") {
			t.Fatal("installed schema unexpectedly required download")
		}
	}
}

// Public schema excerpts verified against duxlabs-dev/dux-shell v2.0.0 and
// dux-qml-common ebf5e6c002da1bdaa4aaa2f319fea7b8507abc42. Shared imports matter:
// looking only in SettingsSpec.js/SessionSpec.js misses clock and wallpaper keys.
func TestDux2SchemaIncludesSharedDefinitions(t *testing.T) {
	c, r := desktopContext(t)
	r.dmsVersion, r.packageVersion = "2.0.0", "2.0.0-1"
	root := filepath.Join(c.Home, "qml")
	r.sourceRoot = root
	installDMSSchemaFixture(t, root, "2.0.0")
	sharedPath := filepath.Join(root, "DCommon/Common/settings/SharedSessionSpec.js")
	shared := storageRead(t, sharedPath)
	if err := os.Remove(sharedPath); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, func() error {
		_, err := VerifyDMSSchema(c)
		return err
	}(), "unverified")
	storagePut(t, sharedPath, shared)
	if _, err := VerifyDMSSchema(c); err != nil {
		t.Fatal(err)
	}
}

func TestReadOnlyPluginRefreshDoesNotFetch(t *testing.T) {
	c, r := desktopContext(t)
	c.CheckOnly = true
	storageRequireError(t, ConfigurePlugins(c), "require staging")
	if len(r.calls) != 0 {
		t.Fatal("check fetched plugin source")
	}
}

func TestFutureEmbeddedSchemaFetchUsesExactInstalledTag(t *testing.T) {
	c, r := desktopContext(t)
	r.dmsVersion, r.packageVersion = "2.1.0", "2.1.0-1"
	// No schema fixture: downloading without valid source must remain a failure.
	_, err := VerifyDMSSchema(c)
	storageRequireError(t, err, "unverified")
	fetched := false
	for _, call := range r.calls {
		if len(call.Args) > 3 && call.Args[0] == "git" && call.Args[3] == "fetch" {
			fetched = true
			if call.Args[len(call.Args)-1] != "refs/tags/v2.1.0" {
				t.Fatalf("schema fetched from wrong release: %v", call.Args)
			}
		}
	}
	if !fetched {
		t.Fatal("new embedded release was blocked solely by version number")
	}
	c.CheckOnly = true
	r.calls = nil
	_, err = VerifyDMSSchema(c)
	if err == nil {
		t.Fatal("unverified source accepted")
	}
	for _, call := range r.calls {
		if call.Args[0] == "git" {
			t.Fatal("check fetched schema source")
		}
	}
}

func TestNewHyprlandGreeterUsesNativeValidation(t *testing.T) {
	c, r := desktopContext(t)
	c.Inventory["hyprland_version"] = "Hyprland 0.99.0"
	c.Inventory["active_config"] = filepath.Join(c.ConfigHome, "hypr/hyprland.lua")
	record := map[string]any{}
	_, destination, err := greeterStageKeyboard(c, filepath.Join(c.RunDir, "greeter"), "/usr/bin/Hyprland", record)
	if err != nil || destination != "/etc/greetd/hypr.lua" {
		t.Fatalf("%s %v", destination, err)
	}
	r.override = func(cmd Command) (CommandResult, bool) {
		if strings.Contains(strings.Join(cmd.Args, " "), "--verify-config") {
			return CommandResult{Code: 1, Stderr: "parse error"}, true
		}
		return CommandResult{}, false
	}
	_, _, err = greeterStageKeyboard(c, filepath.Join(c.RunDir, "greeter"), "/usr/bin/Hyprland", record)
	storageRequireError(t, err, "failed Hyprland validation")
}

func TestPluginUpdatePreservesUntrackedDataAndModifiedObsoleteFiles(t *testing.T) {
	c, _ := desktopContext(t)
	root := filepath.Join(c.ConfigHome, "DankMaterialShell/plugins/dockerManager")
	storagePut(t, filepath.Join(root, "plugin.json"), `{"id":"dockerManager","component":"Widget.qml"}`)
	storagePut(t, filepath.Join(root, "Widget.qml"), "old widget")
	storagePut(t, filepath.Join(root, "notes.json"), `{"personal":"notes"}`)
	storagePut(t, filepath.Join(root, "removed.qml"), "local edits")
	snapshot, _ := jsonBytes(pluginSnapshot{Destination: root, Files: map[string]string{"removed.qml": hashBytes([]byte("old upstream"))}})
	storagePut(t, pluginSnapshotPath(c, "dockerManager"), string(snapshot))
	if err := ConfigurePlugins(c); err != nil {
		t.Fatal(err)
	}
	storagePublish(t, c)
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	if storageRead(t, filepath.Join(root, "notes.json")) != `{"personal":"notes"}` || storageRead(t, filepath.Join(root, "removed.qml")) != "local edits" {
		t.Fatal("user plugin data lost during update")
	}
	if storageRead(t, filepath.Join(root, "Widget.qml")) == "old widget" {
		t.Fatal("upstream plugin code was not updated")
	}
}

func TestPluginSettingsFileUsesExactSnapshotInsteadOfRuntimeMerge(t *testing.T) {
	c, _ := desktopContext(t)
	source := filepath.Join(c.RunDir, "plugin-downloads/dockerManager/settings.json")
	storagePut(t, source, `{"new":true}`)
	root := filepath.Join(c.ConfigHome, "DankMaterialShell/plugins/dockerManager")
	storagePut(t, filepath.Join(root, "plugin.json"), `{"id":"dockerManager","component":"Widget.qml"}`)
	storagePut(t, filepath.Join(root, "Widget.qml"), "old")
	path := filepath.Join(root, "settings.json")
	storagePut(t, path, `{"obsolete":true}`)
	if err := ConfigurePlugins(c); err != nil {
		t.Fatal(err)
	}
	storagePublish(t, c)
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	if storageRead(t, path) != `{"new":true}` {
		t.Fatal("plugin source settings were merged with obsolete content")
	}
}
