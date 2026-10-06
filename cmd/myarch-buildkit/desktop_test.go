package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type desktopRunner struct {
	calls                                                        []Command
	dmsVersion, packageVersion, sourceRoot, pluginStates, groups string
	slurpReply                                                   string
	slurpCode                                                    int
	corruptPNG, copyFailure                                      bool
	override                                                     func(Command) (CommandResult, bool)
}

func (r *desktopRunner) Execute(cmd Command) (CommandResult, error) {
	r.calls = append(r.calls, cmd)
	if r.override != nil {
		if result, ok := r.override(cmd); ok {
			return result, nil
		}
	}
	joined := strings.Join(cmd.Args, " ")
	result := CommandResult{}
	switch {
	case strings.HasPrefix(joined, "udevadm info"):
		result.Stdout = "ID_INPUT_KEYBOARD=1\n"
	case joined == "pacman -Ql dms-shell":
		if r.sourceRoot != "" {
			result.Stdout = "dms-shell " + filepath.Join(r.sourceRoot, "Common/SettingsData.qml") + "\n"
		}
	case joined == "pacman -Q dms-shell":
		result.Stdout = "dms-shell " + r.packageVersion + "\n"
	case joined == "dms version":
		result.Stdout = "dms v" + r.dmsVersion + "\n"
	case len(cmd.Args) > 3 && cmd.Args[0] == "git" && cmd.Args[len(cmd.Args)-1] == "HEAD":
		for _, plugin := range RequestedPlugins {
			if filepath.Base(cmd.Args[2]) == plugin.ID {
				result.Stdout = plugin.Commit + "\n"
			}
		}
	case strings.Contains(joined, "theme getMode"):
		result.Stdout = "dark"
	case strings.Contains(joined, "plugin-scan list"):
		result.Stdout = r.pluginStates
	case joined == "id -nG":
		result.Stdout = r.groups
	case strings.Contains(joined, "is-active"):
		result.Code = 3
	case cmd.Args[0] == "slurp":
		result.Stdout = r.slurpReply
		result.Code = r.slurpCode
	case cmd.Args[0] == "grim":
		raw := []byte("\x89PNG\r\n\x1a\ndata")
		if r.corruptPNG {
			raw = []byte("not a PNG")
		}
		if err := os.WriteFile(cmd.Args[len(cmd.Args)-1], raw, 0600); err != nil {
			return result, err
		}
	case cmd.Args[0] == "wl-copy":
		if r.copyFailure {
			result.Code = 1
			result.Stderr = "copy failed"
		}
	case strings.HasPrefix(joined, "hyprctl -j monitors"):
		result.Stdout = `[{"name":"eDP-1","scale":1.3333333333333333,"focused":true}]`
	case joined == "hyprctl -j devices":
		result.Stdout = `{"keyboards":[{"name":"internal-keyboard","active_keymap":"Hungarian"}]}`
	case strings.HasPrefix(joined, "hyprctl -j getoption"):
		result.Stdout = `{"int":1}`
	case strings.Contains(joined, "--help"):
		result.Stdout = "--verify-config --config"
	case strings.Contains(joined, "--property=ActiveState"):
		result.Stdout = "inactive"
	case strings.Contains(joined, "--property=UnitFileState"):
		result.Stdout = "disabled"
	}
	return result, nil
}
func desktopContext(t *testing.T) (*Context, *desktopRunner) {
	t.Helper()
	c, _ := storageContext(t)
	r := &desktopRunner{dmsVersion: "1.6.2", packageVersion: "1.6.2-1", groups: "users input", slurpReply: "0,0 200x100"}
	c.Runner = r
	c.Binary = filepath.Join(c.BinDir, "myarch-buildkit")
	c.Settings = Settings{Laptop: LaptopSettings{InternalScale: "4/3", InternalKeyboard: "hu", NaturalScroll: true}}
	main := filepath.Join(c.ConfigHome, "hypr/hyprland.conf")
	storagePut(t, main, "# original desktop\n")
	inputRoot := filepath.Join(c.Home, "inputs")
	for index, keyboard := range []struct{ name, bus string }{{"Internal Keyboard", "0011"}, {"USB Keyboard", "0003"}, {"Bluetooth Keyboard", "0005"}} {
		root := filepath.Join(inputRoot, fmt.Sprintf("event%d/device", index))
		storagePut(t, filepath.Join(root, "name"), keyboard.name)
		storagePut(t, filepath.Join(root, "id/bustype"), keyboard.bus)
	}
	c.Inventory = map[string]any{"active_config": main, "hyprland_version": "Hyprland 0.54.3", "hyprland_binary": "/usr/bin/Hyprland", "input_root": inputRoot, "monitors": []map[string]any{{"name": "eDP-1", "width": 2880, "height": 1800}, {"name": "DP-1", "width": 2560, "height": 1440}}, "devices": map[string]any{"keyboards": []map[string]any{{"name": "internal-keyboard"}, {"name": "usb-keyboard"}, {"name": "bluetooth-keyboard"}}}}
	for _, name := range []string{"com.mitchellh.ghostty", "org.gnome.Nautilus", "code"} {
		storagePut(t, filepath.Join(c.DataHome, "applications", name+".desktop"), "[Desktop Entry]\n")
	}
	for _, plugin := range RequestedPlugins {
		source := filepath.Join(c.RunDir, "plugin-downloads", plugin.ID, plugin.Subdir)
		raw, _ := json.Marshal(map[string]any{"id": plugin.ID, "component": "Widget.qml", "version": "test"})
		storagePut(t, filepath.Join(source, "plugin.json"), string(raw))
		storagePut(t, filepath.Join(source, "Widget.qml"), "import QtQuick\nItem {}\n")
		r.pluginStates += plugin.ID + "\tloaded\twidget\n"
	}
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(c.Home, "system-config"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(c.Home, "system-data"))
	if err := c.StartStaging(); err != nil {
		t.Fatal(err)
	}
	return c, r
}
func desktopStage(t *testing.T, c *Context) {
	t.Helper()
	if err := ConfigureLaptop(c); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureDesktop(c); err != nil {
		t.Fatal(err)
	}
}
func desktopObject(t *testing.T, path string) map[string]any {
	t.Helper()
	value, err := desktopReadObject(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestLaptopExactScaleAndNoSubstitution(t *testing.T) {
	dimensions, err := CompatibleScale(2880, 1800, 4.0/3)
	if err != nil || dimensions[0] != 2160 || dimensions[1] != 1350 {
		t.Fatalf("%v %v", dimensions, err)
	}
	_, err = CompatibleScale(1366, 768, 4.0/3)
	storageRequireError(t, err, "no alternative")
	for _, value := range []any{"1/0", "0.2", "9", "1; echo unsafe", "NaN"} {
		if _, err := ParseLaptopScale(value); err == nil {
			t.Fatalf("accepted %v", value)
		}
	}
}
func TestLaptopInternalKeyboardVerificationExcludesExternal(t *testing.T) {
	c, _ := desktopContext(t)
	roles, err := KeyboardRoles(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles["internal"]) != 1 || roles["internal"][0] != "internal-keyboard" || len(roles["unmatched"]) != 2 {
		t.Fatal(roles)
	}
}
func TestLaptopUnverifiedKeyboardBlocksBeforeWrites(t *testing.T) {
	c, r := desktopContext(t)
	r.override = func(cmd Command) (CommandResult, bool) {
		if cmd.Args[0] == "udevadm" {
			return CommandResult{Stdout: "ID_INPUT_KEYBOARD=0"}, true
		}
		return CommandResult{}, false
	}
	storageRequireError(t, ConfigureLaptop(c), "built-in keyboard")
	if len(c.Pending.Files) != 0 {
		t.Fatal("unverified hardware changed configuration")
	}
}
func TestLaptopStaticPreferencesOnlyForBothFormats(t *testing.T) {
	c, r := desktopContext(t)
	if err := ConfigureLaptop(c); err != nil {
		t.Fatal(err)
	}
	selection := object(c.Report["laptop_scale_selection"])
	for _, format := range []string{"lua", "hyprlang"} {
		text, err := LaptopConfigText(format, selection, "hu", true)
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"natural_scroll", "internal-keyboard", "hu", "eDP-1", "1.3333333333333333"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("missing %s: %s", expected, text)
			}
		}
		for _, removed := range []string{"usb-keyboard", "monitor = DP-1,", "refresh", "suspend", "lid", "idle", "power", "follow_mouse"} {
			if strings.Contains(text, removed) {
				t.Fatalf("unexpected %s: %s", removed, text)
			}
		}
	}
	if len(c.Pending.Files) != 2 || exists(LaptopFragment(c)) {
		t.Fatal("laptop configuration was not kept in staging")
	}
	for _, call := range r.calls {
		joined := strings.Join(call.Args, " ")
		if strings.Contains(joined, "start") || strings.Contains(joined, "sudo") {
			t.Fatal("laptop configuration touched services")
		}
	}
}
func TestDesktopFreshPreservesDefaultAppearanceAndStagesPlugins(t *testing.T) {
	c, r := desktopContext(t)
	desktopStage(t, c)
	settings := desktopObject(t, c.View(filepath.Join(c.ConfigHome, "DankMaterialShell/settings.json")))
	session := desktopObject(t, c.View(filepath.Join(c.StateHome, "DankMaterialShell/session.json")))
	for _, key := range []string{"currentThemeName", "customThemeFile", "runDmsMatugenTemplates", "acProfileName", "lockBeforeSuspend"} {
		if _, ok := settings[key]; ok {
			t.Fatal("unexpected managed appearance/power setting", key)
		}
	}
	if _, ok := session["isLightMode"]; ok {
		t.Fatal("theme override")
	}
	if settings["frameEnabled"] != true || settings["showDock"] != true {
		t.Fatal(settings)
	}
	if settings["configVersion"] != float64(18) || session["configVersion"] != float64(4) {
		t.Fatal("schema versions")
	}
	for _, plugin := range RequestedPlugins {
		target := filepath.Join(c.ConfigHome, "DankMaterialShell/plugins", plugin.ID, "plugin.json")
		if exists(target) || !exists(c.View(target)) {
			t.Fatal("plugin not isolated from running shell", plugin.ID)
		}
	}
	main := str(c.Inventory["active_config"])
	if storageRead(t, main) != "# original desktop\n" {
		t.Fatal("active desktop overwritten")
	}
	for _, call := range r.calls {
		if desktopContainsString(call.Args, "start") || desktopContainsString(call.Args, "stop") {
			t.Fatal("live startup mutation")
		}
	}
	wrapper := storageRead(t, c.View(filepath.Join(c.BinDir, "cachyos-desktop-session")))
	if !strings.Contains(wrapper, "exec "+shellQuote(c.Binary)+" desktop-session") || strings.Contains(wrapper, "python") {
		t.Fatal(wrapper)
	}
}
func TestDesktopExistingPersonalSettingsAndCustomFileSurvive(t *testing.T) {
	c, _ := desktopContext(t)
	settingsPath := filepath.Join(c.ConfigHome, "DankMaterialShell/settings.json")
	sessionPath := filepath.Join(c.StateHome, "DankMaterialShell/session.json")
	clipboardPath := filepath.Join(c.ConfigHome, "DankMaterialShell/clsettings.json")
	storagePut(t, settingsPath, `{"configVersion":18,"currentThemeName":"personal","runDmsMatugenTemplates":false,"showOccupiedWorkspacesOnly":false,"screenPreferences":{"clock":["DP-1"]}}`)
	storagePut(t, sessionPath, `{"configVersion":4,"isLightMode":true,"themeModeAutoEnabled":true}`)
	storagePut(t, clipboardPath, `{"disabled":false}`)
	custom := filepath.Join(c.ConfigHome, "hypr/desktop-custom.conf")
	storagePut(t, custom, "# personal custom\n")
	desktopStage(t, c)
	settings := desktopObject(t, c.View(settingsPath))
	session := desktopObject(t, c.View(sessionPath))
	if settings["currentThemeName"] != "personal" || settings["runDmsMatugenTemplates"] != false || settings["showOccupiedWorkspacesOnly"] != false || session["isLightMode"] != true || session["themeModeAutoEnabled"] != true {
		t.Fatal("personal preferences overwritten")
	}
	if desktopObject(t, c.View(clipboardPath))["disabled"] != false || storageRead(t, c.View(custom)) != "# personal custom\n" {
		t.Fatal("explicit clipboard or custom preferences overwritten")
	}
	if len(stringList(object(settings["screenPreferences"])["clock"])) != 1 {
		t.Fatal("existing screen preferences lost")
	}
}
func TestDesktopUnsupportedSchemaStopsBeforeDesktopWrites(t *testing.T) {
	c, r := desktopContext(t)
	r.dmsVersion = "1.6.3"
	r.packageVersion = "1.6.3-1"
	storageRequireError(t, ConfigureDesktop(c), "unverified")
	if len(c.Pending.Files) != 0 {
		t.Fatal("unsupported schema staged writes")
	}
}
func TestDesktopInstalledSourceRequiresCompleteVerifiedSchema(t *testing.T) {
	c, r := desktopContext(t)
	root := filepath.Join(c.Home, "qml")
	r.sourceRoot = root
	storagePut(t, filepath.Join(root, "Common/SettingsData.qml"), "property int settingsConfigVersion: 18\n")
	storagePut(t, filepath.Join(root, "Common/SessionData.qml"), "property int sessionConfigVersion: 4\n")
	storagePut(t, filepath.Join(root, "Common/settings/SettingsSpec.js"), "barConfigs: [],screenPreferences: {}\n")
	storagePut(t, filepath.Join(root, "Common/settings/SessionSpec.js"), "pinnedApps: [],wallpaperPath: ''\n")
	_, err := VerifyDMSSchema(c)
	storageRequireError(t, err, "unverified")
	settings := "barConfigs: [],screenPreferences: {}\n"
	for key := range DMSSettings {
		settings += key + ": null,\n"
	}
	session := "pinnedApps: [],wallpaperPath: ''\n"
	for key := range DMSSession {
		session += key + ": null,\n"
	}
	storagePut(t, filepath.Join(root, "Common/settings/SettingsSpec.js"), settings)
	storagePut(t, filepath.Join(root, "Common/settings/SessionSpec.js"), session)
	verified, err := VerifyDMSSchema(c)
	if err != nil || verified != root {
		t.Fatalf("%s %v", verified, err)
	}
}
func TestDesktopUnknownConfigVersionIsPreserved(t *testing.T) {
	c, _ := desktopContext(t)
	path := filepath.Join(c.ConfigHome, "DankMaterialShell/settings.json")
	storagePut(t, path, `{"configVersion":19,"personal":true}`)
	storageRequireError(t, ConfigureDesktop(c), "configVersion")
	if len(c.Pending.Files) != 0 || storageRead(t, path) != `{"configVersion":19,"personal":true}` {
		t.Fatal("unknown config changed")
	}
}
func TestDesktopBothVerifiedFormatsKeepShortcutBehavior(t *testing.T) {
	for _, format := range []string{"lua", "hyprlang"} {
		text, err := DesktopConfigText(format, "/home/test/startup", "/home/test/shot", "/home/test/clip", "/home/test/laptop")
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"ghostty", "nautilus", "code", "toggleWith apps", "clip", "region", "focused", "audio increment 5", "brightness decrement 5", "laptop", "mouse:272", "mouse:273"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("missing %s", expected)
			}
		}
		for _, removed := range []string{"rounding", "gaps_in", "rgba(", "border_size", "active_opacity", "kb_layout", "monitor ="} {
			if strings.Contains(text, removed) {
				t.Fatal("removed appearance/global input/display override", removed)
			}
		}
	}
	c, _ := desktopContext(t)
	c.Inventory["hyprland_version"] = "Hyprland 0.55.0"
	storageRequireError(t, ConfigureDesktop(c), "unverified Hyprland")
}
func TestDesktopKnownAutostartsDisabledByOverridesOnly(t *testing.T) {
	c, _ := desktopContext(t)
	path := filepath.Join(c.ConfigHome, "autostart/waybar.desktop")
	storagePut(t, path, "[Desktop Entry]\nExec=env FOO=1 /usr/bin/waybar\n")
	overrides, err := DesktopAutostarts(c)
	if err != nil || !strings.Contains(overrides[path], "Hidden=true") {
		t.Fatalf("%v %v", overrides, err)
	}
	if !strings.Contains(storageRead(t, path), "Exec=") {
		t.Fatal("autostart was edited live")
	}
	storagePut(t, path, "[Desktop Entry]\nExec=sh -c 'waybar; echo wrapper'\n")
	_, err = DesktopAutostarts(c)
	storageRequireError(t, err, "unrecognized wrapper")
}
func TestPluginExistingInstallationAndPreferencesArePreserved(t *testing.T) {
	c, _ := desktopContext(t)
	root := filepath.Join(c.ConfigHome, "DankMaterialShell/plugins/personal-docker")
	storagePut(t, filepath.Join(root, "plugin.json"), `{"id":"dockerManager","component":"Personal.qml"}`)
	storagePut(t, filepath.Join(root, "Personal.qml"), "personal content")
	settingsPath := filepath.Join(c.ConfigHome, "DankMaterialShell/plugin_settings.json")
	storagePut(t, settingsPath, `{"dockerManager":{"filter":"mine","enabled":false},"other":{"enabled":true}}`)
	if err := ConfigurePlugins(c); err != nil {
		t.Fatal(err)
	}
	settings := desktopObject(t, c.View(settingsPath))
	if object(settings["dockerManager"])["filter"] != "mine" || object(settings["dockerManager"])["dockerBinary"] != "podman" || object(settings["other"])["enabled"] != true {
		t.Fatal(settings)
	}
	if storageRead(t, filepath.Join(root, "Personal.qml")) != "personal content" || exists(c.View(filepath.Join(c.ConfigHome, "DankMaterialShell/plugins/dockerManager/plugin.json"))) {
		t.Fatal("existing plugin was replaced")
	}
}
func TestPluginSnapshotRejectsWrongManifestSymlinksAndEscapes(t *testing.T) {
	c, _ := desktopContext(t)
	plugin := RequestedPlugins[0]
	source := filepath.Join(c.RunDir, "plugin-downloads", plugin.ID)
	storagePut(t, filepath.Join(source, "plugin.json"), `{"id":"other","component":"Widget.qml"}`)
	storageRequireError(t, ConfigurePlugins(c), "unexpected plugin ID")
	storagePut(t, filepath.Join(source, "plugin.json"), `{"id":"dockerManager","component":"Widget.qml"}`)
	if err := os.Symlink("Widget.qml", filepath.Join(source, "link.qml")); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, ConfigurePlugins(c), "symlink")
	outside := filepath.Join(c.Home, "outside.qml")
	storagePut(t, outside, "outside")
	storagePut(t, filepath.Join(source, "plugin.json"), `{"id":"dockerManager","component":"../../../outside.qml"}`)
	_, err := PluginManifest(source, "dockerManager")
	storageRequireError(t, err, "unsafe")
}
func TestPluginLoginMergeRetainsLateRuntimePreferences(t *testing.T) {
	c, _ := desktopContext(t)
	path := filepath.Join(c.ConfigHome, "DankMaterialShell/plugin_settings.json")
	storagePut(t, path, `{"dockerManager":{"filter":"before","enabled":false}}`)
	if err := ConfigurePlugins(c); err != nil {
		t.Fatal(err)
	}
	storagePublish(t, c)
	storagePut(t, path, `{"dockerManager":{"filter":"after","enabled":false,"newPreference":42}}`)
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	plugin := object(desktopObject(t, path)["dockerManager"])
	if plugin["filter"] != "after" || plugin["newPreference"] != float64(42) || plugin["enabled"] != true || plugin["dockerBinary"] != "podman" {
		t.Fatal(plugin)
	}
}
func TestDesktopRuntimeChecksReportPluginAndInputAccess(t *testing.T) {
	c, r := desktopContext(t)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "test")
	policy := map[string]any{"internal_outputs": []string{"eDP-1"}, "internal_keyboards": []string{"internal-keyboard"}, "internal_scale": 4.0 / 3, "internal_keyboard": "hu", "natural_scroll": true}
	raw, _ := json.Marshal(policy)
	storagePut(t, filepath.Join(c.ConfigHome, "myarch-buildkit/laptop.json"), string(raw))
	if err := CheckDesktop(c); err != nil {
		t.Fatal(err)
	}
	r.pluginStates = strings.ReplaceAll(r.pluginStates, "clipboardPlus\tloaded", "clipboardPlus\tunloaded")
	r.groups = "users"
	if err := CheckDesktop(c); err == nil {
		t.Fatal("missing plugin and input membership passed")
	}
	failures := strings.Join(stringList(object(c.Report["running_desktop"])["failures"]), " ")
	if !strings.Contains(failures, "clipboardPlus") || !strings.Contains(failures, "input-group") {
		t.Fatal(failures)
	}
}
func TestWallpaperExistingAndExplicitTOMLImageIsPreserved(t *testing.T) {
	c, _ := desktopContext(t)
	session := map[string]any{"wallpaperPath": "/old/preference.png"}
	record := PreserveWallpaper(c, session)
	if record["source"] != "existing DMS session" {
		t.Fatal(record)
	}
	image := filepath.Join(c.Home, "wallpaper.png")
	storagePut(t, image, "image")
	storagePut(t, filepath.Join(c.ConfigHome, "noctalia/config.toml"), "[wallpaper]\nimage = "+fmt.Sprintf("%q", image)+"\n")
	session = map[string]any{}
	record = PreserveWallpaper(c, session)
	if record["status"] != "preserved" || session["wallpaperPath"] != image {
		t.Fatalf("%v %v", record, session)
	}
}
func TestScreenshotCancellationLeavesNoFile(t *testing.T) {
	c, r := desktopContext(t)
	r.slurpCode = 1
	if err := ScreenshotMain(c, []string{"region"}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(c.Home, "Pictures/Screenshots")) {
		t.Fatal("cancellation created a file")
	}
}
func TestScreenshotCorruptPNGRemovedAndCopyFailureKeepsImage(t *testing.T) {
	c, r := desktopContext(t)
	r.corruptPNG = true
	storageRequireError(t, ScreenshotMain(c, []string{"region"}), "PNG")
	entries, _ := os.ReadDir(filepath.Join(c.Home, "Pictures/Screenshots"))
	if len(entries) != 0 {
		t.Fatal("invalid image retained")
	}
	r.corruptPNG = false
	r.copyFailure = true
	if err := ScreenshotMain(c, []string{"focused"}); err != nil {
		t.Fatal(err)
	}
	entries, _ = os.ReadDir(filepath.Join(c.Home, "Pictures/Screenshots"))
	if len(entries) != 1 {
		t.Fatal("valid saved image removed after copy failure")
	}
}
func TestClipboardCommandsKeepExplicitEnablementAndLimit(t *testing.T) {
	c, r := desktopContext(t)
	if err := ClipboardMain(c, []string{"enable"}); err != nil {
		t.Fatal(err)
	}
	last := strings.Join(r.calls[len(r.calls)-1].Args, " ")
	if last != "dms cl config set --max-history 100 --auto-clear-days 0 --clear-at-startup --enable" {
		t.Fatal(last)
	}
	if err := ClipboardMain(c, []string{"show"}); err != nil {
		t.Fatal(err)
	}
	last = strings.Join(r.calls[len(r.calls)-1].Args, " ")
	if last != "dms ipc call clipboardPlus togglePanel" {
		t.Fatal(last)
	}
	storageRequireError(t, ClipboardMain(c, []string{"unknown"}), "usage")
}
func TestDesktopSessionRequiresIntendedSession(t *testing.T) {
	c, r := desktopContext(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	storageRequireError(t, DesktopSession(c), "intended Hyprland")
	if len(r.calls) != 0 {
		t.Fatal("startup attempted outside session")
	}
}
