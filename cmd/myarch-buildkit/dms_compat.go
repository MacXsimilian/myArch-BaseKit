package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const dmsUpstream = "https://github.com/duxlabs-dev/dux-shell"

var dmsVersionPattern = regexp.MustCompile(`\bv?([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)\b`)

func dmsVersion(output string) string {
	match := dmsVersionPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func dmsDirectory(c *Context) string {
	version, _ := c.Inventory["dms_version"].(string)
	major, _ := strconv.Atoi(strings.Split(version, ".")[0])
	if major >= 2 {
		return "dms"
	}
	return "DankMaterialShell"
}

func desktopSettings(c *Context) map[string]any {
	settings := desktopCloneObject(DMSSettings)
	if !boolValue(c.Inventory["dms_dock_configs"]) {
		return settings
	}
	// Dux moved docks into per-dock records and retired these global widget keys.
	for _, key := range []string{
		"showDock",
		"dockPosition",
		"dockAutoHide",
		"dockSmartAutoHide",
		"dockUseOverlayLayer",
		"dockGroupByApp",
		"dockOpenOnOverview",
		"showWorkspaceIndex",
		"showWorkspaceName",
		"workspaceFollowFocus",
		"controlCenterShowNetworkIcon",
		"controlCenterShowAudioIcon",
	} {
		delete(settings, key)
	}
	settings["dockConfigs"] = []map[string]any{{
		"id":                "dock",
		"name":              "Dock",
		"enabled":           true,
		"position":          1,
		"screenPreferences": []string{"all"},
		"showOnLastDisplay": true,
		"autoHide":          false,
		"smartAutoHide":     false,
		"useOverlayLayer":   false,
		"groupByApp":        true,
		"openOnOverview":    false,
	}}
	return settings
}

func dmsSchemaVersion(source, property string) int {
	match := regexp.MustCompile(`\b` + regexp.QuoteMeta(property) + `\s*:\s*([0-9]+)\b`).FindStringSubmatch(source)
	if len(match) != 2 {
		return 0
	}
	version, _ := strconv.Atoi(match[1])
	return version
}

func inspectDMSSchema(c *Context, root string) bool {
	settingsData, settingsDataErr := desktopSafeText(filepath.Join(root, "Common/SettingsData.qml"), false)
	sessionData, sessionDataErr := desktopSafeText(filepath.Join(root, "Common/SessionData.qml"), false)
	settingsSpec, settingsSpecErr := desktopSafeText(filepath.Join(root, "Common/settings/SettingsSpec.js"), false)
	sessionSpec, sessionSpecErr := desktopSafeText(filepath.Join(root, "Common/settings/SessionSpec.js"), false)
	settingsVersion := dmsSchemaVersion(settingsData, "settingsConfigVersion")
	sessionVersion := dmsSchemaVersion(sessionData, "sessionConfigVersion")
	if settingsDataErr != nil || sessionDataErr != nil || settingsSpecErr != nil || sessionSpecErr != nil || settingsVersion < 1 || sessionVersion < 1 {
		return false
	}
	// Distribution packages expose DCommon below the QML root; source checkouts
	// keep it in the pinned submodule beside quickshell.
	for _, path := range []string{
		filepath.Join(root, "DCommon/Common/settings/SharedSettingsSpec.js"),
		filepath.Join(filepath.Dir(root), "dux-qml-common/DCommon/Common/settings/SharedSettingsSpec.js"),
		filepath.Join(filepath.Dir(root), "dank-qml-common/DCommon/Common/settings/SharedSettingsSpec.js"),
	} {
		shared, err := desktopSafeText(path, false)
		if err == nil {
			settingsSpec += "\n" + shared
			sharedSession, err := desktopSafeText(filepath.Join(filepath.Dir(path), "SharedSessionSpec.js"), false)
			if err == nil {
				sessionSpec += "\n" + sharedSession
			}
		}
	}
	c.Inventory["dms_dock_configs"] = mapKeyPresent(settingsSpec, "dockConfigs")
	for key := range desktopSettings(c) {
		if !mapKeyPresent(settingsSpec, key) {
			return false
		}
	}
	for _, key := range []string{"barConfigs", "screenPreferences"} {
		if !mapKeyPresent(settingsSpec, key) {
			return false
		}
	}
	for key := range DMSSession {
		if !mapKeyPresent(sessionSpec, key) {
			return false
		}
	}
	pinKey := "pinnedApps"
	if boolValue(c.Inventory["dms_dock_configs"]) {
		pinKey = "dockPins"
	}
	for _, key := range []string{pinKey, "wallpaperPath"} {
		if !mapKeyPresent(sessionSpec, key) {
			return false
		}
	}
	c.Inventory["dms_settings_version"] = settingsVersion
	c.Inventory["dms_session_version"] = sessionVersion
	return true
}

// Embedded QML builds have no installed source to inspect. Known releases have
// verified schema adapters; other releases are inspected at their exact tag.
// This happens only while staging, never during plan, check, or login.
func fetchDMSSchema(c *Context, version string) (string, error) {
	if c.CheckOnly || c.Pending == nil || c.RunDir == "" {
		return "", fmt.Errorf("DMS schema is unverified; stage desktop to inspect the installed release")
	}
	destination := filepath.Join(c.RunDir, "dms-schema")
	if err := securePath(destination, true); err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"git", "init", destination},
		{"git", "-C", destination, "fetch", "--depth", "1", dmsUpstream, "refs/tags/v" + version},
		{"git", "-C", destination, "checkout", "--force", "--detach", "FETCH_HEAD"},
		{"git", "-C", destination, "clean", "-fdx"},
	} {
		if _, err := c.Run(args...); err != nil {
			return "", fmt.Errorf("DMS schema is unverified for %s: %w", version, err)
		}
	}
	for _, submodule := range []string{"dux-qml-common", "dank-qml-common"} {
		if exists(filepath.Join(destination, submodule)) {
			if _, err := c.Run("git", "-C", destination, "submodule", "update", "--init", "--depth", "1", "--", submodule); err != nil {
				return "", err
			}
		}
	}
	return filepath.Join(destination, "quickshell"), nil
}

func VerifyDMSSchema(c *Context) (string, error) {
	if c.Inventory == nil {
		c.Inventory = map[string]any{}
	}
	result, err := c.Run("dms", "version")
	if err != nil {
		return "", err
	}
	version := dmsVersion(result.Stdout)
	if version == "" {
		return "", fmt.Errorf("DMS installed version is unverified: %s", strings.TrimSpace(result.Stdout))
	}
	c.Inventory["dms_version"] = version
	c.Inventory["dms_version_output"] = strings.TrimSpace(result.Stdout)
	roots := []string{}
	if root, ok := c.Inventory["dms_qml_root"].(string); ok && root != "" {
		roots = append(roots, root)
	}
	listed := c.Try("pacman", "-Ql", "dms-shell")
	if listed.Code == 0 {
		for _, line := range strings.Split(listed.Stdout, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && strings.HasSuffix(fields[1], "/Common/SettingsData.qml") {
				root := filepath.Dir(filepath.Dir(fields[1]))
				if !contains(roots, root) {
					roots = append(roots, root)
				}
			}
		}
	}
	for _, root := range roots {
		if inspectDMSSchema(c, root) {
			return root, nil
		}
	}
	if len(roots) > 0 {
		return "", fmt.Errorf("DMS's installed settings schema is unverified; required configuration fields are missing")
	}
	// Verified upstream release schemas, for distro builds with embedded QML.
	known := map[string]struct {
		settings int
		session  int
	}{
		"1.6.2": {settings: 18, session: 4},
		"2.0.0": {settings: 42, session: 7},
	}
	if schema, ok := known[version]; ok {
		pkg := c.Try("pacman", "-Q", "dms-shell")
		pattern := `^dms-shell\s+(?:[0-9]+:)?` + regexp.QuoteMeta(version) + `-[A-Za-z0-9.]+\s*$`
		if pkg.Code == 0 && regexp.MustCompile(pattern).MatchString(pkg.Stdout) {
			c.Inventory["dms_settings_version"], c.Inventory["dms_session_version"] = schema.settings, schema.session
			c.Inventory["dms_dock_configs"] = version == "2.0.0"
			return "", nil
		}
	}
	root, err := fetchDMSSchema(c, version)
	if err != nil {
		return "", err
	}
	if !inspectDMSSchema(c, root) {
		return "", fmt.Errorf("DMS schema is unverified for %s; required configuration fields are missing", version)
	}
	return root, nil
}

func requireDMSMigration(c *Context) error {
	if dmsDirectory(c) != "dms" {
		return nil
	}
	for _, base := range []string{c.ConfigHome, c.StateHome} {
		old := filepath.Join(base, "DankMaterialShell")
		if info, err := os.Lstat(old); err == nil && info.IsDir() && !exists(filepath.Join(base, "dms")) {
			return fmt.Errorf("DMS 2.x data migration is pending in %s; start the installed DMS once to migrate your existing preferences, then rerun desktop setup", base)
		}
	}
	return nil
}

// Native verification is the compatibility gate; release-number ceilings would
// reject newer compositors even when their configuration APIs still work.
func hyprlandConfigFormat(path string) (string, error) {
	switch filepath.Ext(path) {
	case ".lua":
		return "lua", nil
	case ".conf":
		return "hyprlang", nil
	default:
		return "", fmt.Errorf("unsupported Hyprland configuration format: %s", path)
	}
}
