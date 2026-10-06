package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
)

var DMSSettings = map[string]any{
	"frameEnabled": true, "frameMode": "connected", "frameScreenPreferences": []string{"all"},
	"showDock": true, "dockPosition": 1, "dockAutoHide": false, "dockSmartAutoHide": false, "dockUseOverlayLayer": false, "dockGroupByApp": true, "dockOpenOnOverview": false,
	"launcherStyle": "full", "rememberLastMode": false, "rememberLastQuery": false, "dankLauncherV2IncludeFilesInAll": false, "dankLauncherV2IncludeFoldersInAll": false,
	"clockFormat": "24h", "clockDateFormat": "ddd d MMM", "showSeconds": false, "showWorkspaceIndex": true, "showWorkspaceName": false, "workspaceFollowFocus": false,
	"notificationHistoryEnabled": true, "soundNewNotification": false, "weatherEnabled": false, "useAutoLocation": false, "controlCenterShowNetworkIcon": true, "controlCenterShowAudioIcon": true,
}
var DMSBar = map[string]any{
	"id": "default", "name": "Main Bar", "enabled": true, "position": 0, "screenPreferences": []string{"all"}, "showOnLastDisplay": true,
	"leftWidgets": []string{"workspaceSwitcher", "focusedWindow"}, "centerWidgets": []string{"clock"},
	"rightWidgets":       []string{"dockerManager", "kubernetes", "bongoCat", "clipboardPlus", "systemTray", "notificationButton", "battery", "controlCenterButton"},
	"attachToScreenEdge": true, "autoHide": false, "visible": true, "useOverlayLayer": false,
}
var DMSSession = map[string]any{"doNotDisturb": false, "doNotDisturbUntil": 0, "terminalOverride": "ghostty", "nightModeEnabled": false, "nightModeAutoEnabled": false, "launcherLastMode": "apps", "launcherLastQuery": "", "appDrawerLastMode": "apps"}
var DesktopCompetitors = []string{"noctalia", "noctalia-shell", "waybar", "mako", "dunst", "swaync", "hyprpolkitagent", "polkit-gnome-authentication-agent-1", "nm-applet"}
var CompetingDesktopUnits = []string{"noctalia.service", "noctalia-shell.service", "waybar.service", "mako.service", "dunst.service", "swaync.service", "hyprpolkitagent.service", "nm-applet.service"}
var DMSSources = []string{
	"https://github.com/AvengeMedia/DankMaterialShell/blob/v1.6.2/quickshell/Common/settings/SettingsSpec.js",
	"https://github.com/AvengeMedia/DankMaterialShell/blob/v1.6.2/quickshell/Common/settings/SessionSpec.js",
	"https://github.com/AvengeMedia/DankMaterialShell/blob/v1.6.2/core/internal/server/clipboard/types.go",
	"https://github.com/AvengeMedia/DankMaterialShell/blob/v1.6.2/assets/systemd/dms.service",
	"https://danklinux.com/docs/dankmaterialshell/keybinds-ipc",
	"https://wiki.hypr.land/configuring/core/dispatchers/",
	"https://github.com/hyprwm/Hyprland/blob/main/example/hyprland.lua",
	"https://wiki.hypr.land/0.54.0/Configuring/Keywords/",
}

func desktopCloneObject(source map[string]any) map[string]any {
	copy := map[string]any{}
	for k, v := range source {
		copy[k] = v
	}
	return copy
}
func desktopSafeText(path string, owned bool) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing a symlinked configuration file: %s", path)
	}
	if !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
		return "", fmt.Errorf("expected a small regular configuration file: %s", path)
	}
	if owned {
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Getuid()) {
			return "", fmt.Errorf("refusing a configuration file owned by another user: %s", path)
		}
	}
	raw, err := os.ReadFile(path)
	return string(raw), err
}
func desktopReadObject(path string, version *int) (map[string]any, error) {
	text, err := desktopSafeText(path, true)
	if err != nil {
		return nil, err
	}
	value := map[string]any{}
	if strings.TrimSpace(text) != "" {
		if err = json.Unmarshal([]byte(text), &value); err != nil {
			return nil, err
		}
		if value == nil {
			return nil, fmt.Errorf("expected a JSON object: %s", path)
		}
	}
	if version != nil {
		if found, ok := value["configVersion"]; ok && found != nil && mapJSONNumber(found) != float64(*version) {
			return nil, fmt.Errorf("unverified DMS configVersion %v in %s; open it with the installed DMS first", found, path)
		}
	}
	return value, nil
}
func desktopWriteObject(c *Context, path string, value map[string]any, mode os.FileMode) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return c.Write(path, append(raw, '\n'), mode)
}
func mapKeyPresent(source, key string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `\s*:`).MatchString(source)
}
func VerifyDMSSchema(c *Context) (string, error) {
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
				if !desktopContainsString(roots, root) {
					roots = append(roots, root)
				}
			}
		}
	}
	for _, root := range roots {
		data, e := desktopSafeText(filepath.Join(root, "Common/SettingsData.qml"), false)
		if e != nil {
			continue
		}
		settings, e := desktopSafeText(filepath.Join(root, "Common/settings/SettingsSpec.js"), false)
		if e != nil {
			continue
		}
		session, e := desktopSafeText(filepath.Join(root, "Common/SessionData.qml"), false)
		if e != nil {
			continue
		}
		sessionSpec, e := desktopSafeText(filepath.Join(root, "Common/settings/SessionSpec.js"), false)
		if e != nil {
			continue
		}
		if !regexp.MustCompile(`settingsConfigVersion\s*:\s*18\b`).MatchString(data) || !regexp.MustCompile(`sessionConfigVersion\s*:\s*4\b`).MatchString(session) {
			continue
		}
		valid := true
		for key := range DMSSettings {
			valid = valid && mapKeyPresent(settings, key)
		}
		for _, key := range []string{"barConfigs", "screenPreferences"} {
			valid = valid && mapKeyPresent(settings, key)
		}
		for key := range DMSSession {
			valid = valid && mapKeyPresent(sessionSpec, key)
		}
		for _, key := range []string{"pinnedApps", "wallpaperPath"} {
			valid = valid && mapKeyPresent(sessionSpec, key)
		}
		if valid {
			return root, nil
		}
	}
	if len(roots) == 0 {
		pkg := c.Try("pacman", "-Q", "dms-shell")
		version := c.Try("dms", "version")
		c.Report["dms_version_detection"] = map[string]any{"command": []string{"dms", "version"}, "package": strings.TrimSpace(pkg.Stdout), "package_exit_code": pkg.Code, "version_stdout": strings.TrimSpace(version.Stdout), "version_stderr": strings.TrimSpace(version.Stderr), "version_exit_code": version.Code}
		if pkg.Code == 0 && version.Code == 0 && regexp.MustCompile(`^dms-shell\s+(?:\d+:)?1\.6\.2-[A-Za-z0-9.]+\s*$`).MatchString(pkg.Stdout) && regexp.MustCompile(`\bv?1\.6\.2\b`).MatchString(version.Stdout) {
			return "", nil
		}
	}
	return "", fmt.Errorf("DMS's installed settings schema is unverified; this profile supports SettingsSpec 18 / SessionSpec 4 (DMS 1.6.2); existing desktop files were preserved")
}
func DesktopFormat(c *Context) (string, string, error) {
	main, ok := c.Inventory["active_config"].(string)
	if !ok || main == "" {
		return "", "", fmt.Errorf("the active Hyprland configuration could not be identified; no higher-priority config will be created")
	}
	if !filepath.IsAbs(main) || !inside(c.Home, main) {
		return "", "", fmt.Errorf("active Hyprland configuration is outside the user's home; automatic replacement is unsupported")
	}
	if _, err := desktopSafeText(main, true); err != nil {
		return "", "", err
	}
	match := regexp.MustCompile(`\b(?:v)?(\d+)\.(\d+)\.(\d+)\b`).FindStringSubmatch(str(c.Inventory["hyprland_version"]))
	if len(match) > 0 && match[1] == "0" {
		if filepath.Ext(main) == ".lua" && (match[2] == "55" || match[2] == "56") {
			return main, "lua", nil
		}
		if filepath.Ext(main) == ".conf" && match[2] == "54" {
			return main, "hyprlang", nil
		}
	}
	return "", "", fmt.Errorf("unverified Hyprland version/config format; supported: Lua 0.55/0.56 or hyprland.conf 0.54; existing configuration was preserved")
}
func DesktopPins(c *Context) ([]string, error) {
	roots := []string{filepath.Join(c.DataHome, "applications")}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	for _, directory := range strings.Split(dirs, ":") {
		if directory != "" {
			roots = append(roots, filepath.Join(directory, "applications"))
		}
	}
	result := []string{}
	for _, choices := range [][]string{{"com.mitchellh.ghostty", "ghostty"}, {"org.gnome.Nautilus"}, {"code", "visual-studio-code", "com.microsoft.VSCode"}} {
		found := ""
		for _, choice := range choices {
			for _, root := range roots {
				if exists(filepath.Join(root, choice+".desktop")) {
					found = choice
					break
				}
			}
			if found != "" {
				break
			}
		}
		if found == "" {
			return nil, fmt.Errorf("could not find the installed application launcher for %s; dock pins were not guessed", choices[0])
		}
		result = append(result, found)
	}
	return result, nil
}

func splitDesktopCommand(text string) ([]string, error) {
	words := []string{}
	var word strings.Builder
	quote := rune(0)
	escape, started := false, false
	for _, r := range text {
		if escape {
			word.WriteRune(r)
			escape = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escape = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if unicode.IsSpace(r) {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		word.WriteRune(r)
		started = true
	}
	if escape || quote != 0 {
		return nil, fmt.Errorf("unterminated desktop command")
	}
	if started {
		words = append(words, word.String())
	}
	return words, nil
}
func knownDesktopCompetitor(command string) string {
	for _, unsafe := range []string{";", "&&", "||", "`", "$(", "\n"} {
		if strings.Contains(command, unsafe) {
			return ""
		}
	}
	args, err := splitDesktopCommand(command)
	if err != nil || len(args) == 0 {
		return ""
	}
	if filepath.Base(args[0]) == "env" {
		args = args[1:]
		for len(args) > 0 && regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`).MatchString(args[0]) {
			args = args[1:]
		}
	}
	if len(args) == 0 {
		return ""
	}
	base := filepath.Base(args[0])
	if desktopContainsString(DesktopCompetitors, base) {
		return base
	}
	if (base == "qs" || base == "quickshell") && len(args) >= 3 && (args[1] == "-c" || args[1] == "--config") && (args[2] == "noctalia" || args[2] == "noctalia-shell") {
		return "noctalia-shell"
	}
	return ""
}
func DesktopAutostarts(c *Context) (map[string]string, error) {
	dirs := os.Getenv("XDG_CONFIG_DIRS")
	if dirs == "" {
		dirs = "/etc/xdg"
	}
	roots := []string{}
	for _, directory := range strings.Split(dirs, ":") {
		if directory != "" {
			roots = append(roots, filepath.Join(directory, "autostart"))
		}
	}
	roots = append(roots, filepath.Join(c.ConfigHome, "autostart"))
	paths := map[string]string{}
	for _, root := range roots {
		entries, err := filepath.Glob(filepath.Join(root, "*.desktop"))
		if err != nil {
			return nil, err
		}
		for _, path := range entries {
			paths[filepath.Base(path)] = path
		}
	}
	result := map[string]string{}
	for name, path := range paths {
		text, err := desktopSafeText(path, inside(c.ConfigHome, path))
		if err != nil {
			return nil, err
		}
		section := ""
		entry := map[string]string{}
		scanner := bufio.NewScanner(strings.NewReader(text))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.Trim(line, "[]")
				continue
			}
			if section == "Desktop Entry" {
				pair := strings.SplitN(line, "=", 2)
				if len(pair) == 2 {
					entry[strings.ToLower(strings.TrimSpace(pair[0]))] = strings.TrimSpace(pair[1])
				}
			}
		}
		if strings.EqualFold(entry["hidden"], "true") {
			continue
		}
		command := entry["exec"]
		args, _ := splitDesktopCommand(command)
		dms := len(args) >= 2 && filepath.Base(args[0]) == "dms" && args[1] == "run" && (len(args) == 2 || (len(args) == 3 && args[2] == "--session"))
		if knownDesktopCompetitor(command) != "" || dms {
			result[filepath.Join(c.ConfigHome, "autostart", name)] = "[Desktop Entry]\nType=Application\nName=Superseded by the DMS desktop profile\nHidden=true\n"
			continue
		}
		for _, competitor := range append(append([]string{}, DesktopCompetitors...), "dms") {
			pattern := `(?:^|[^\w-])` + regexp.QuoteMeta(competitor) + `(?:$|[^\w-])`
			if competitor == "dms" {
				pattern = `\bdms\s+run\b`
			}
			if regexp.MustCompile(pattern).MatchString(command) {
				return nil, fmt.Errorf("an unrecognized wrapper starts a competing desktop shell in %s; resolve its launch before DMS activation", path)
			}
		}
	}
	return result, nil
}
func existingImage(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	extension := strings.ToLower(filepath.Ext(path))
	if !desktopContainsString([]string{".png", ".jpg", ".jpeg", ".webp", ".avif", ".bmp", ".gif", ".jxl"}, extension) || !filepath.IsAbs(path) {
		return ""
	}
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() {
		return ""
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return ""
	}
	return resolved
}
func wallpaperImages(value any, images map[string]bool) {
	switch v := value.(type) {
	case string:
		if image := existingImage(v); image != "" {
			images[image] = true
		}
	case map[string]any:
		for _, child := range v {
			wallpaperImages(child, images)
		}
	case []any:
		for _, child := range v {
			wallpaperImages(child, images)
		}
	}
}
func PreserveWallpaper(c *Context, session map[string]any) map[string]any {
	for _, key := range []string{"wallpaperPath", "wallpaperPathLight", "wallpaperPathDark", "monitorWallpapers", "monitorWallpapersLight", "monitorWallpapersDark"} {
		if value, ok := session[key]; ok && value != nil && value != "" {
			switch v := value.(type) {
			case map[string]any:
				if len(v) == 0 {
					continue
				}
			case []any:
				if len(v) == 0 {
					continue
				}
			}
			return map[string]any{"status": "preserved", "source": "existing DMS session"}
		}
	}
	candidate, _ := c.Inventory["wallpaper_path"].(string)
	source := "session inventory"
	if candidate == "" {
		query := c.Try("swww", "query")
		images := map[string]bool{}
		if query.Code == 0 {
			for _, line := range strings.Split(query.Stdout, "\n") {
				if match := regexp.MustCompile(`\bimage:\s*(.+)$`).FindStringSubmatch(line); len(match) > 0 {
					images[strings.TrimSpace(match[1])] = true
				}
			}
		}
		if len(images) == 1 {
			for image := range images {
				candidate = image
			}
			source = "running swww"
		}
	}
	if candidate == "" {
		images := map[string]bool{}
		for _, path := range []string{filepath.Join(c.ConfigHome, "noctalia/settings.json"), filepath.Join(c.ConfigHome, "noctalia/config.toml"), filepath.Join(c.StateHome, "noctalia/settings.toml")} {
			text, err := desktopSafeText(path, true)
			if err != nil {
				continue
			}
			found := map[string]bool{}
			if filepath.Ext(path) == ".json" {
				var value map[string]any
				if json.Unmarshal([]byte(text), &value) != nil {
					continue
				}
				wallpaperImages(value["wallpaper"], found)
			} else {
				section := ""
				scanner := bufio.NewScanner(strings.NewReader(text))
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if strings.HasPrefix(line, "[") {
						section = strings.Trim(line, "[] ")
						continue
					}
					if section != "wallpaper" && !strings.HasPrefix(section, "wallpaper.") {
						continue
					}
					for _, match := range regexp.MustCompile(`"((?:\\.|[^"\\])*)"|'([^']*)'`).FindAllStringSubmatch(line, -1) {
						value := match[2]
						if match[1] != "" {
							var decoded string
							if json.Unmarshal([]byte("\""+match[1]+"\""), &decoded) != nil {
								continue
							}
							value = decoded
						}
						if image := existingImage(value); image != "" {
							found[image] = true
						}
					}
				}
			}
			if len(found) > 0 {
				images = found
			}
		}
		if len(images) == 1 {
			for image := range images {
				candidate = image
			}
			source = "explicit Noctalia wallpaper image"
		}
	}
	if image := existingImage(candidate); image != "" {
		session["wallpaperPath"] = image
		session["perMonitorWallpaper"] = false
		session["perModeWallpaper"] = false
		return map[string]any{"status": "preserved", "source": source}
	}
	c.Warn("The existing wallpaper could not be identified automatically. Its source files are preserved; select the same image in DMS after login. Wallpaper preservation is not yet verified.")
	return map[string]any{"status": "needs_selection", "source": nil}
}

func DesktopConfigText(format, startup, screenshot, clipboard, fragment string) (string, error) {
	apps := [][2]string{{"SUPER + Return", "ghostty"}, {"SUPER + E", "nautilus"}, {"SUPER + C", "code"}, {"SUPER + space", "dms ipc call spotlight toggleWith apps"}, {"SUPER + L", "dms ipc call lock lock"}, {"SUPER + V", shellQuote(clipboard) + " show"}, {"Print", shellQuote(screenshot) + " region"}, {"SHIFT + Print", shellQuote(screenshot) + " focused"}}
	media := [][2]string{{"XF86AudioRaiseVolume", "dms ipc call audio increment 5"}, {"XF86AudioLowerVolume", "dms ipc call audio decrement 5"}, {"XF86AudioMute", "dms ipc call audio mute"}, {"XF86AudioMicMute", "dms ipc call audio micmute"}, {"XF86MonBrightnessUp", "dms ipc call brightness increment 5 ''"}, {"XF86MonBrightnessDown", "dms ipc call brightness decrement 5 ''"}}
	lines := []string{}
	for _, path := range []string{startup, screenshot, clipboard, fragment} {
		for _, r := range path {
			if r < 32 {
				return "", fmt.Errorf("control characters unsupported in Hyprland paths")
			}
		}
	}
	if format == "lua" {
		lines = append(lines, "-- Managed CachyOS desktop profile. Previous main config is backed up.", "-- Three laptop preferences live in a separate static fragment.", `hl.env("TERMINAL", "ghostty")`, `hl.env("EDITOR", "code --wait")`, `hl.env("VISUAL", "code --wait")`, `hl.config({ general = { layout = "dwindle" }, dwindle = { preserve_split = true } })`, "")
		if fragment != "" {
			lines = append(lines, "require("+luaString(fragment)+")")
		}
		for _, app := range apps {
			lines = append(lines, "hl.bind("+luaString(app[0])+", hl.dsp.exec_cmd("+luaString(app[1])+"))")
		}
		lines = append(lines, `hl.bind("SUPER + Q", hl.dsp.window.close())`, `hl.bind("SUPER + F", hl.dsp.window.fullscreen({ action = "toggle", mode = "fullscreen" }))`, `hl.bind("SUPER + SHIFT + space", hl.dsp.window.float({ action = "toggle" }))`)
		for _, direction := range []string{"left", "right", "up", "down"} {
			lines = append(lines, fmt.Sprintf(`hl.bind("SUPER + %s", hl.dsp.focus({ direction = "%s" }))`, direction, direction))
		}
		for i := 1; i <= 9; i++ {
			lines = append(lines, fmt.Sprintf(`hl.workspace_rule({ workspace = "%d", persistent = true })`, i), fmt.Sprintf(`hl.bind("SUPER + %d", hl.dsp.focus({ workspace = %d }))`, i, i), fmt.Sprintf(`hl.bind("SUPER + SHIFT + %d", hl.dsp.window.move({ workspace = %d }))`, i, i))
		}
		lines = append(lines, `hl.bind("SUPER + mouse:272", hl.dsp.window.drag(), { mouse = true })`, `hl.bind("SUPER + mouse:273", hl.dsp.window.resize(), { mouse = true })`)
		for _, binding := range media {
			lines = append(lines, "hl.bind("+luaString(binding[0])+", hl.dsp.exec_cmd("+luaString(binding[1])+"), { locked = true, repeating = true })")
		}
		lines = append(lines, "", `hl.on("hyprland.start", function()`, "  hl.exec_cmd("+luaString(shellQuote(startup))+")", "end)")
	} else {
		for _, path := range []string{startup, screenshot, clipboard} {
			if strings.ContainsAny(path, "\n\r,$#") {
				return "", fmt.Errorf("path cannot be represented safely in legacy Hyprland config")
			}
		}
		if strings.ContainsAny(fragment, "\n\r$#") {
			return "", fmt.Errorf("cannot represent laptop fragment path safely")
		}
		lines = append(lines, "# Managed CachyOS desktop profile. Previous main config is backed up.", "env = TERMINAL,ghostty", "env = EDITOR,code --wait", "env = VISUAL,code --wait", "general {", "  layout = dwindle", "}", "dwindle {", "  preserve_split = true", "}", "")
		if fragment != "" {
			lines = append(lines, "source = "+fragment)
		}
		for _, app := range apps {
			keys := strings.Split(app[0], " + ")
			lines = append(lines, "bind = "+strings.Join(keys[:len(keys)-1], " ")+", "+keys[len(keys)-1]+", exec, "+app[1])
		}
		lines = append(lines, "bind = SUPER, Q, killactive,", "bind = SUPER, F, fullscreen, 0", "bind = SUPER SHIFT, space, togglefloating,")
		for _, direction := range []string{"left", "right", "up", "down"} {
			lines = append(lines, "bind = SUPER, "+direction+", movefocus, "+direction[:1])
		}
		for i := 1; i <= 9; i++ {
			lines = append(lines, fmt.Sprintf("workspace = %d, persistent:true", i), fmt.Sprintf("bind = SUPER, %d, workspace, %d", i, i), fmt.Sprintf("bind = SUPER SHIFT, %d, movetoworkspace, %d", i, i))
		}
		lines = append(lines, "bindm = SUPER, mouse:272, movewindow", "bindm = SUPER, mouse:273, resizewindow")
		for _, binding := range media {
			lines = append(lines, "bindel = , "+binding[0]+", exec, "+binding[1])
		}
		lines = append(lines, "exec-once = "+shellQuote(startup))
	}
	return strings.Join(lines, "\n") + "\n", nil
}
func helperWrapper(c *Context, command string) string {
	return "#!/bin/sh\nexec " + shellQuote(c.Binary) + " " + command + " \"$@\"\n"
}
func ConfigureDesktop(c *Context) (result error) {
	report := map[string]any{"status": "checking", "configured": false, "activated": false, "sources": DMSSources}
	c.Report["profile_config"] = report
	defer func() {
		if result != nil {
			report["status"] = "blocked"
			report["error"] = result.Error()
			c.Warn(result.Error())
		}
	}()
	main, format, err := DesktopFormat(c)
	if err != nil {
		return err
	}
	root, err := VerifyDMSSchema(c)
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(c.ConfigHome, "DankMaterialShell/settings.json")
	sessionPath := filepath.Join(c.StateHome, "DankMaterialShell/session.json")
	clipboardPath := filepath.Join(c.ConfigHome, "DankMaterialShell/clsettings.json")
	settingsVersion, sessionVersion := 18, 4
	settings, err := desktopReadObject(c.View(settingsPath), &settingsVersion)
	if err != nil {
		return err
	}
	session, err := desktopReadObject(c.View(sessionPath), &sessionVersion)
	if err != nil {
		return err
	}
	clipboard, err := desktopReadObject(c.View(clipboardPath), nil)
	if err != nil {
		return err
	}
	pins, err := DesktopPins(c)
	if err != nil {
		return err
	}
	autostarts, err := DesktopAutostarts(c)
	if err != nil {
		return err
	}
	wallpaper := PreserveWallpaper(c, session)
	for key, value := range DMSSettings {
		settings[key] = value
	}
	if _, ok := settings["showOccupiedWorkspacesOnly"]; !ok {
		settings["showOccupiedWorkspacesOnly"] = true
	}
	settings["configVersion"] = 18
	settings["barConfigs"] = []map[string]any{desktopCloneObject(DMSBar)}
	screenPrefs := map[string]any{}
	if previous, ok := settings["screenPreferences"]; ok {
		var valid bool
		screenPrefs, valid = previous.(map[string]any)
		if !valid {
			return fmt.Errorf("DMS screenPreferences has an unsupported type")
		}
	}
	screenPrefs = desktopCloneObject(screenPrefs)
	screenPrefs["dock"] = []string{"all"}
	settings["screenPreferences"] = screenPrefs
	for key, value := range DMSSession {
		session[key] = value
	}
	session["configVersion"] = 4
	session["pinnedApps"] = pins
	enabled := clipboard["disabled"] == false
	clipboardUpdates := map[string]any{"maxHistory": 100, "autoClearDays": 0, "clearAtStartup": true, "disabled": !enabled}
	for key, value := range clipboardUpdates {
		clipboard[key] = value
	}
	screenshot := filepath.Join(c.BinDir, "cachyos-screenshot")
	clipboardHelper := filepath.Join(c.BinDir, "cachyos-clipboard")
	startup := filepath.Join(c.BinDir, "cachyos-desktop-session")
	fragment, _ := object(c.Report["laptop"])["fragment"].(string)
	if fragment == "" {
		fragment = LaptopFragment(c)
		if !exists(c.View(fragment)) {
			return fmt.Errorf("stage the three laptop preferences before configuring the desktop")
		}
	}
	mainText, err := DesktopConfigText(format, startup, screenshot, clipboardHelper, fragment)
	if err != nil {
		return err
	}
	extension := ".conf"
	comment := "#"
	if format == "lua" {
		extension = ".lua"
		comment = "--"
	}
	managed := filepath.Join(filepath.Dir(main), "desktop-managed"+extension)
	custom := filepath.Join(filepath.Dir(main), "desktop-custom"+extension)
	if !exists(c.View(custom)) {
		if err = c.Write(custom, []byte(comment+" Your personal overrides. The installer preserves this file.\n"), 0600); err != nil {
			return err
		}
	}
	loader := "# Managed CachyOS desktop profile loader.\nsource = " + managed + "\nsource = " + custom + "\n"
	if format == "lua" {
		loader = "-- Managed CachyOS desktop profile loader.\nrequire(" + luaString(managed) + ")\nrequire(" + luaString(custom) + ")\n"
	} else if strings.ContainsAny(managed+custom, "\n\r$#") {
		return fmt.Errorf("cannot safely represent the managed/custom source path")
	}
	for _, entry := range []struct {
		path  string
		value map[string]any
	}{{settingsPath, settings}, {sessionPath, session}, {clipboardPath, clipboard}} {
		if err = desktopWriteObject(c, entry.path, entry.value, 0600); err != nil {
			return err
		}
	}
	for _, entry := range []struct{ path, command string }{{screenshot, "screenshot"}, {clipboardHelper, "clipboard"}, {startup, "desktop-session"}} {
		if err = c.Write(entry.path, []byte(helperWrapper(c, entry.command)), 0700); err != nil {
			return err
		}
	}
	names := []string{}
	for path := range autostarts {
		names = append(names, path)
	}
	sort.Strings(names)
	for _, path := range names {
		if err = c.Write(path, []byte(autostarts[path]), 0600); err != nil {
			return err
		}
	}
	if err = ConfigurePlugins(c); err != nil {
		return err
	}
	if err = c.Write(managed, []byte(mainText), 0600); err != nil {
		return err
	}
	if err = c.Write(main, []byte(loader), 0600); err != nil {
		return err
	}
	updates := desktopCloneObject(DMSSettings)
	updates["configVersion"] = 18
	updates["barConfigs"] = []map[string]any{desktopCloneObject(DMSBar)}
	updates["screenPreferences"] = map[string]any{"dock": []string{"all"}}
	if err = c.OverrideJSON(settingsPath, updates); err != nil {
		return err
	}
	sessionUpdates := desktopCloneObject(DMSSession)
	sessionUpdates["configVersion"] = 4
	sessionUpdates["pinnedApps"] = pins
	if err = c.OverrideJSON(sessionPath, sessionUpdates); err != nil {
		return err
	}
	if err = c.OverrideJSON(clipboardPath, clipboardUpdates); err != nil {
		return err
	}
	report["status"] = "staged"
	report["configured"] = true
	report["appearance"] = "application defaults; existing theme preferences preserved"
	report["main_config"] = main
	report["format"] = format
	report["managed_config"] = managed
	report["custom_config"] = custom
	report["settings"] = settingsPath
	report["session"] = sessionPath
	report["source_root"] = root
	report["schema_verification"] = "installed source"
	if root == "" {
		report["schema_verification"] = "embedded QML, verified upstream dms-shell 1.6.2"
	}
	report["startup_script"] = startup
	report["startup_command"] = []string{startup}
	report["laptop_fragment"] = fragment
	report["startup_method"] = "Hyprland start hook -> dms run"
	report["competing_units"] = CompetingDesktopUnits
	report["disabled_autostarts"] = names
	report["wallpaper"] = wallpaper
	report["pinned_apps"] = pins
	report["launchers"] = map[string]any{"terminal": "ghostty", "files": "nautilus", "editor": "code"}
	report["screenshot"] = map[string]any{"helper": screenshot, "commands": []string{"region", "focused"}, "directory": filepath.Join(c.Home, "Pictures/Screenshots")}
	report["clipboard"] = map[string]any{"enabled": enabled, "max_history": 100, "clear_at_startup": true, "bitwarden_check": "manual_dummy_item_required", "may_use_disk": true, "instructions": []string{clipboardHelper, "instructions"}, "enable": []string{clipboardHelper, "enable"}, "disable": []string{clipboardHelper, "disable"}}
	c.Note("DMS Frame Mode, top bar, visible bottom dock and the accepted Hyprland keymap are staged for native validation.")
	if !enabled {
		c.Note("Clipboard history is staged at 100 items and clear-on-start, initially disabled. Run cachyos-clipboard instructions for the Bitwarden dummy-item check.")
	}
	return nil
}
func CheckDesktop(c *Context) error {
	record := object(c.Report["running_desktop"])
	failures := []string{}
	if os.Getenv("WAYLAND_DISPLAY") == "" || os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" {
		failures = append(failures, "Runtime checks require the intended Hyprland session")
	} else {
		reply := desktopIPCProbe(c, "theme", "getMode")
		mode := strings.Trim(strings.TrimSpace(reply.Stdout), "\"")
		record["ipc_mode"] = mode
		if reply.Code != 0 || (mode != "dark" && mode != "light") {
			failures = append(failures, "DMS did not answer its theme IPC probe")
		}
		reply = desktopIPCProbe(c, "plugin-scan", "list")
		states := map[string]string{}
		for _, line := range strings.Split(reply.Stdout, "\n") {
			columns := strings.Split(line, "\t")
			if len(columns) >= 2 && !strings.HasPrefix(line, "#") {
				states[columns[0]] = columns[1]
			}
		}
		record["plugins"] = states
		for _, plugin := range RequestedPlugins {
			if reply.Code != 0 || states[plugin.ID] != "loaded" {
				failures = append(failures, "DMS plugin is not loaded: "+plugin.ID)
			}
		}
		if !desktopContainsString(strings.Fields(c.Try("id", "-nG").Stdout), "input") {
			failures = append(failures, "Bongo Cat needs input-group membership and a new login to monitor typing; see the guide")
		}
		for _, unit := range []string{"dms.service", "cachyos-dms-direct.service"} {
			if c.Try("systemctl", "--user", "is-active", "--quiet", unit).Code == 0 {
				failures = append(failures, "Alternate DMS startup is active: "+unit)
			}
		}
	}
	record["ready"] = len(failures) == 0
	record["failures"] = failures
	record["startup_method"] = "Hyprland start hook -> dms run"
	c.Report["running_desktop"] = record
	for _, failure := range failures {
		c.Warn(failure)
	}
	if len(failures) > 0 {
		return fmt.Errorf("desktop runtime verification pending (%d DMS checks)", len(failures))
	}
	return CheckLaptop(c)
}

func desktopIPCProbe(c *Context, args ...string) CommandResult {
	reply, err := c.Runner.Execute(Command{Args: append([]string{"dms", "ipc", "call"}, args...), Timeout: 5 * time.Second})
	if err != nil {
		reply.Code = 127
		reply.Stderr = err.Error()
	}
	return reply
}
