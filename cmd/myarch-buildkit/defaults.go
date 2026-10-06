package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const BuildkitFont = "JetBrainsMono Nerd Font Mono"
const MarkdownDesktop = "myarch-buildkit-obsidian.desktop"

var codeDesktopIDs = []string{"code.desktop", "visual-studio-code.desktop", "com.microsoft.VSCode.desktop"}
var codeMIMEs = []string{
	"text/plain", "text/x-python", "text/x-shellscript", "text/x-c", "text/x-c++", "text/x-csrc", "text/x-c++src", "text/x-chdr", "text/x-c++hdr", "text/x-go", "text/x-rust", "text/javascript", "application/javascript", "application/json", "application/yaml", "text/yaml", "application/toml", "text/x-makefile", "application/x-code-workspace",
}

type desktopEntry struct {
	ID    string   `json:"id"`
	Path  string   `json:"path"`
	MIMEs []string `json:"mime_types"`
}

func configRead(c *Context, path string) (string, error) {
	data, err := os.ReadFile(c.View(path))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("configuration is not valid UTF-8: %s", path)
	}
	return string(data), nil
}

func xdgDataDirs(c *Context) []string {
	dirs := []string{c.DataHome}
	other := os.Getenv("XDG_DATA_DIRS")
	if other == "" {
		other = "/usr/local/share:/usr/share"
	}
	for _, item := range strings.Split(other, ":") {
		if filepath.IsAbs(item) {
			found := false
			for _, dir := range dirs {
				found = found || item == dir
			}
			if !found {
				dirs = append(dirs, item)
			}
		}
	}
	return dirs
}

func desktopProperties(text string) (map[string]string, error) {
	properties := map[string]string{}
	section := ""
	found := false
	for _, line := range sourceLines(text) {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") || trim == "" {
			continue
		}
		if header, ok := mimeHeader(line); ok {
			section = header
			found = found || section == "Desktop Entry"
			continue
		}
		if section == "Desktop Entry" {
			at := strings.IndexByte(line, '=')
			if at < 0 {
				return nil, fmt.Errorf("invalid desktop-entry property")
			}
			properties[strings.TrimSpace(line[:at])] = strings.TrimSpace(line[at+1:])
		}
	}
	if !found {
		return nil, fmt.Errorf("missing Desktop Entry section")
	}
	return properties, nil
}

func discoverDesktop(c *Context, id string) (*desktopEntry, error) {
	for _, directory := range xdgDataDirs(c) {
		path := filepath.Join(directory, "applications", id)
		if !exists(c.View(path)) {
			continue
		}
		text, err := configRead(c, path)
		if err != nil {
			return nil, err
		}
		props, err := desktopProperties(text)
		if err != nil {
			return nil, fmt.Errorf("invalid desktop entry %s: %w", path, err)
		}
		if strings.EqualFold(props["Hidden"], "true") {
			return nil, fmt.Errorf("the user desktop entry hides %s: %s", id, path)
		}
		if typ := props["Type"]; typ != "" && typ != "Application" {
			return nil, fmt.Errorf("not an application desktop entry: %s", path)
		}
		return &desktopEntry{id, path, desktopIDs(props["MimeType"], "")}, nil
	}
	return nil, fmt.Errorf("installed desktop entry was not found: %s", id)
}

func discoverCode(c *Context) (*desktopEntry, error) {
	for _, id := range codeDesktopIDs {
		for _, directory := range xdgDataDirs(c) {
			if exists(c.View(filepath.Join(directory, "applications", id))) {
				return discoverDesktop(c, id)
			}
		}
	}
	return nil, fmt.Errorf("installed VS Code desktop entry was not found; checked %s", strings.Join(codeDesktopIDs, ", "))
}

func defaultContainsString(list []string, item string) bool {
	for _, value := range list {
		if value == item {
			return true
		}
	}
	return false
}

func managedBlock(text, begin, end, block string) (string, error) {
	if strings.Count(text, begin) != strings.Count(text, end) || strings.Count(text, begin) > 1 {
		return "", fmt.Errorf("unbalanced or duplicate managed markers; edit manually")
	}
	if !strings.Contains(text, begin) {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		return text + block, nil
	}
	pattern := regexp.MustCompile(`(?ms)^` + regexp.QuoteMeta(begin) + `\r?\n.*?^` + regexp.QuoteMeta(end) + `(?:\r?\n|$)`)
	matches := pattern.FindAllStringIndex(text, -1)
	if len(matches) != 1 {
		return "", fmt.Errorf("malformed or reversed managed markers; edit manually")
	}
	where := matches[0]
	return text[:where[0]] + block + text[where[1]:], nil
}

func configureGhostty(c *Context) (string, error) {
	directory := filepath.Join(c.ConfigHome, "ghostty")
	main := filepath.Join(directory, "config.ghostty")
	legacy := filepath.Join(directory, "config")
	if _, err := os.Lstat(c.View(legacy)); err == nil {
		main = legacy
	}
	child := filepath.Join(directory, "myarch-buildkit-font.conf")
	if strings.ContainsAny(child, "\r\n\"") {
		return "", fmt.Errorf("Ghostty config path contains unsupported line breaks or quotes")
	}
	if err := c.Write(child, []byte("# Managed by myarch-buildkit.\nfont-family = \"\"\nfont-family = \""+BuildkitFont+"\"\n"), 0600); err != nil {
		return "", err
	}
	text, err := configRead(c, main)
	if err != nil {
		return "", err
	}
	begin, end := "# BEGIN myarch-buildkit font include", "# END myarch-buildkit font include"
	updated, err := managedBlock(text, begin, end, begin+"\nconfig-file = \""+child+"\"\n"+end+"\n")
	if err != nil {
		return "", err
	}
	return main, c.Write(main, []byte(updated), 0600)
}

func configureTerminalPreferences(c *Context, id string) error {
	for _, name := range []string{"hyprland-xdg-terminals.list", "xdg-terminals.list"} {
		path := filepath.Join(c.ConfigHome, name)
		text, err := configRead(c, path)
		if err != nil {
			return err
		}
		kept := []string{}
		for _, line := range sourceLines(text) {
			if strings.TrimSpace(line) != id {
				kept = append(kept, line)
			}
		}
		if err = c.Write(path, []byte(id+"\n"+strings.Join(kept, "")), 0600); err != nil {
			return err
		}
	}
	return nil
}

func desktopExecArg(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	var quote strings.Builder
	quote.WriteByte('"')
	for _, r := range value {
		if strings.ContainsRune("\\`\"$", r) {
			quote.WriteByte('\\')
		}
		quote.WriteRune(r)
	}
	quote.WriteByte('"')
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(quote.String())
}

func configureObsidianHandler(c *Context) (string, error) {
	executable, err := exec.LookPath("obsidian")
	if err != nil {
		return "", fmt.Errorf("Obsidian executable was not found; Markdown handler was not changed")
	}
	binary := c.Binary
	if binary == "" || !filepath.IsAbs(binary) {
		return "", fmt.Errorf("the myarch-buildkit executable must be installed at an absolute path for the Markdown handler")
	}
	path := filepath.Join(c.DataHome, "applications", MarkdownDesktop)
	text := "[Desktop Entry]\nType=Application\nName=Obsidian Notes\nComment=Open Markdown notes in a registered Obsidian vault\n" +
		"Exec=" + desktopExecArg(binary) + " obsidian-open --executable " + desktopExecArg(executable) + " %F\n" +
		"Icon=obsidian\nTerminal=false\nNoDisplay=true\nCategories=Office;\nMimeType=text/markdown;text/x-markdown;\n"
	if err = c.Write(path, []byte(text), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// ObsidianOpen is the native registered-vault opener used by the MIME handler.
// It creates no notes, opens only existing local files, and keeps each URI as
// one argument. It may also be invoked without files to open Obsidian normally.
func ObsidianOpen(c *Context, args []string) error {
	executable := "obsidian"
	if len(args) >= 2 && args[0] == "--executable" {
		executable, args = args[1], args[2:]
		if !filepath.IsAbs(executable) {
			return fmt.Errorf("Obsidian executable must be an absolute path")
		}
	}
	if len(args) == 0 {
		_, err := c.Command(Command{Args: []string{executable}, Detached: true})
		return err
	}
	failures := []string{}
	for _, argument := range args {
		path, err := obsidianLocalPath(argument)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		uri := "obsidian://open?path=" + url.QueryEscape(path)
		// QueryEscape uses '+' for spaces; Obsidian expects URI percent encoding.
		uri = strings.ReplaceAll(uri, "+", "%20")
		if _, err = c.Command(Command{Args: []string{executable, uri}, Detached: true}); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("Obsidian notes: %s", strings.Join(failures, "; "))
	}
	return nil
}

func obsidianLocalPath(argument string) (string, error) {
	if strings.HasPrefix(argument, "file://") {
		parsed, err := url.Parse(argument)
		if err != nil {
			return "", err
		}
		if parsed.Host != "" && parsed.Host != "localhost" {
			return "", fmt.Errorf("only local file URLs are supported")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("local file URL must not have a query or fragment")
		}
		argument = parsed.Path
	}
	path, err := filepath.Abs(argument)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file does not exist: %s", path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", path)
	}
	return path, nil
}

func selectedMIMEAssociations(entries map[string]*desktopEntry) (map[string]string, []string) {
	intended := map[string]string{}
	warnings := []string{}
	for _, mime := range codeMIMEs {
		intended[mime] = entries["code"].ID
	}
	if !defaultContainsString(entries["obsidian"].MIMEs, "x-scheme-handler/obsidian") {
		warnings = append(warnings, "obsidian.desktop does not advertise its URI scheme; URI default was not changed")
	} else {
		intended["x-scheme-handler/obsidian"] = entries["obsidian"].ID
	}
	if !defaultContainsString(entries["firefox"].MIMEs, "application/pdf") {
		warnings = append(warnings, "firefox.desktop does not advertise application/pdf; PDF default was not changed")
	} else {
		intended["application/pdf"] = entries["firefox"].ID
	}
	vlc := entries["vlc"]
	mediaReady := defaultContainsString(vlc.MIMEs, "audio/mpeg") && defaultContainsString(vlc.MIMEs, "video/mp4")
	if !mediaReady {
		warnings = append(warnings, "vlc.desktop does not advertise the required MP3/MP4 types; media defaults were not changed")
	}
	intended["text/markdown"], intended["text/x-markdown"] = MarkdownDesktop, MarkdownDesktop
	intended["inode/directory"] = entries["nautilus"].ID
	for _, mime := range vlc.MIMEs {
		if mediaReady && (strings.HasPrefix(mime, "audio/") || strings.HasPrefix(mime, "video/") || defaultContainsString([]string{"application/ogg", "application/x-ogg", "application/xspf+xml", "application/vnd.apple.mpegurl", "application/x-mpegurl"}, mime)) {
			intended[mime] = vlc.ID
		}
	}
	return intended, warnings
}

func configureMIME(c *Context, intended map[string]string, codeID string) error {
	path := filepath.Join(c.ConfigHome, "mimeapps.list")
	text, err := configRead(c, path)
	if err != nil {
		return err
	}
	code := map[string]string{}
	for _, mime := range codeMIMEs {
		code[mime] = codeID
	}
	updated := EditMIMEDefaults(EditMIMEAssociations(text, code), intended)
	if err = c.Write(path, []byte(updated), 0600); err != nil {
		return err
	}
	active := os.Getenv("XDG_CURRENT_DESKTOP")
	if active == "" {
		active = "Hyprland"
	}
	valid := regexp.MustCompile(`^[a-z0-9_-]+$`)
	seen := map[string]bool{}
	for _, name := range strings.Split(strings.ToLower(active), ":") {
		if !valid.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		for _, directory := range []string{c.ConfigHome, filepath.Join(c.DataHome, "applications")} {
			path = filepath.Join(directory, name+"-mimeapps.list")
			if !exists(c.View(path)) {
				continue
			}
			text, err = configRead(c, path)
			if err != nil {
				return err
			}
			if err = c.Write(path, []byte(EditMIMEDefaults(text, intended)), 0600); err != nil {
				return err
			}
		}
	}
	return nil
}

// ConfigureDefaults preserves personal application settings and adds only the
// requested defaults. All edits use the context's backup/staging writer.
func ConfigureDefaults(c *Context) error {
	if c.CheckOnly {
		return fmt.Errorf("a check cannot modify application configuration")
	}
	if c.Report == nil {
		c.Report = map[string]any{}
	}
	configured := map[string]any{}
	entries := map[string]*desktopEntry{}
	discoveries := map[string]any{}
	incomplete := []string{}
	report := map[string]any{"schema_version": 1, "font": BuildkitFont, "status": "complete", "configured": configured, "desktop_entries": discoveries,
		"manual_checks": []string{"Test the existing terminal/editor shortcuts after the staged desktop profile is applied at login.", "Open an existing notes vault in Obsidian once. Markdown associations require the file to belong to a vault already registered in Obsidian."}}
	c.Report["defaults"] = report
	action := func(name string, fn func() error) {
		if err := fn(); err != nil {
			message := name + ": " + err.Error()
			incomplete = append(incomplete, message)
			c.Warn(message)
		}
	}
	for _, app := range [][2]string{{"ghostty", "com.mitchellh.ghostty.desktop"}, {"obsidian", "obsidian.desktop"}, {"firefox", "firefox.desktop"}, {"vlc", "vlc.desktop"}, {"nautilus", "org.gnome.Nautilus.desktop"}} {
		app := app
		action("Discover "+app[0], func() error {
			entry, err := discoverDesktop(c, app[1])
			if err == nil {
				entries[app[0]] = entry
				discoveries[entry.ID] = entry
			}
			return err
		})
	}
	action("Discover code", func() error {
		entry, err := discoverCode(c)
		if err == nil {
			entries["code"] = entry
			discoveries[entry.ID] = entry
		}
		return err
	})
	if len(incomplete) == 0 {
		oldMarkdown := map[string]string{}
		for _, mime := range []string{"text/markdown", "text/x-markdown"} {
			result := c.Try("xdg-mime", "query", "default", mime)
			if result.Code == 0 {
				oldMarkdown[mime] = strings.TrimSpace(result.Stdout)
			} else {
				oldMarkdown[mime] = ""
			}
		}
		report["old_markdown_desktop"] = oldMarkdown
		action("Ghostty font", func() error {
			path, err := configureGhostty(c)
			if err == nil {
				configured["ghostty_font"] = path
			}
			return err
		})
		action("Fontconfig", func() error {
			path := filepath.Join(c.ConfigHome, "fontconfig/conf.d/99-myarch-buildkit-monospace.conf")
			text := "<?xml version=\"1.0\"?>\n<!DOCTYPE fontconfig SYSTEM \"fonts.dtd\">\n<fontconfig>\n  <alias binding=\"strong\">\n    <family>monospace</family>\n    <prefer><family>" + BuildkitFont + "</family></prefer>\n  </alias>\n</fontconfig>\n"
			if err := c.Write(path, []byte(text), 0600); err != nil {
				return err
			}
			configured["fontconfig"] = path
			return nil
		})
		action("Terminal preferences", func() error {
			err := configureTerminalPreferences(c, entries["ghostty"].ID)
			if err == nil {
				configured["terminal_desktop"] = entries["ghostty"].ID
			}
			return err
		})
		configured["hyprland_launchers"] = "Managed by the separately staged desktop profile"
		fonts := map[string]any{"editor.fontFamily": "'" + BuildkitFont + "', monospace", "terminal.integrated.fontFamily": "'" + BuildkitFont + "', monospace"}
		report["vscode_font_settings"] = fonts
		action("VS Code fonts", func() error {
			path := filepath.Join(c.ConfigHome, "Code/User/settings.json")
			text, err := configRead(c, path)
			if err != nil {
				return err
			}
			updated, err := EditJSONC(text, fonts)
			if err != nil {
				return fmt.Errorf("VS Code settings preserved: %s: %w; set the two font settings manually", path, err)
			}
			if err = c.Write(path, []byte(updated), 0600); err == nil {
				configured["vscode_fonts"] = path
			}
			return err
		})
		openerReady := false
		action("Obsidian Markdown opener", func() error {
			path, err := configureObsidianHandler(c)
			if err == nil {
				openerReady = true
				configured["obsidian_markdown_opener"] = c.Binary
				configured["obsidian_markdown_desktop"] = path
			}
			return err
		})
		action("MIME defaults", func() error {
			intended, warnings := selectedMIMEAssociations(entries)
			for _, warning := range warnings {
				incomplete = append(incomplete, warning)
				c.Warn(warning)
			}
			if !openerReady {
				delete(intended, "text/markdown")
				delete(intended, "text/x-markdown")
			}
			err := configureMIME(c, intended, entries["code"].ID)
			if err == nil {
				configured["mime_defaults"] = intended
				configured["code_mime_types_explicitly_associated"] = codeMIMEs
				report["code_mime_types_advertised"] = entries["code"].MIMEs
			}
			return err
		})
		if c.Pending == nil {
			if result := c.Try("update-desktop-database", filepath.Join(c.DataHome, "applications")); result.Code != 0 {
				c.Warn("update-desktop-database: " + strings.TrimSpace(result.Stderr))
			}
		}
	}
	if len(incomplete) > 0 {
		report["status"] = "partial"
		report["incomplete_config"] = incomplete
	}
	if err := c.Save(); err != nil {
		return err
	}
	if len(incomplete) > 0 {
		return fmt.Errorf("application configuration incomplete: %s", strings.Join(incomplete, "; "))
	}
	return nil
}

// CheckDefaults reads the running account's selected handlers and font only.
func CheckDefaults(c *Context) error {
	checks := map[string]string{}
	failures := []string{}
	terminal, err := c.Command(Command{Args: []string{"xdg-terminal-exec", "--print-id"}, Env: map[string]string{"XDG_CURRENT_DESKTOP": "Hyprland"}, Timeout: 15 * time.Second})
	checks["terminal"] = strings.TrimSpace(terminal.Stdout)
	if err != nil || checks["terminal"] != "com.mitchellh.ghostty.desktop" {
		failures = append(failures, "Ghostty terminal default")
	}
	for _, item := range [][2]string{{"text/plain", ""}, {"x-scheme-handler/obsidian", "obsidian.desktop"}, {"text/markdown", MarkdownDesktop}, {"inode/directory", "org.gnome.Nautilus.desktop"}, {"application/pdf", "firefox.desktop"}, {"audio/mpeg", "vlc.desktop"}, {"video/mp4", "vlc.desktop"}} {
		result := c.Try("xdg-mime", "query", "default", item[0])
		actual := strings.TrimSpace(result.Stdout)
		checks[item[0]] = actual
		good := result.Code == 0 && actual == item[1]
		if item[0] == "text/plain" {
			good = result.Code == 0 && defaultContainsString(codeDesktopIDs, actual)
		}
		if !good {
			failures = append(failures, item[0]+" default")
		}
	}
	font := c.Try("fc-match", "-f", "%{family}\n", BuildkitFont)
	checks["font"] = strings.TrimSpace(font.Stdout)
	if font.Code != 0 || !strings.Contains(checks["font"], BuildkitFont) {
		failures = append(failures, "monospace font")
	}
	command := c.Try("xdg-terminal-exec", "--print-cmd")
	checks["terminal_command"] = strings.TrimSpace(command.Stdout)
	if c.Report == nil {
		c.Report = map[string]any{}
	}
	c.Report["defaults_check"] = checks
	if len(failures) > 0 {
		return fmt.Errorf("default/font checks failed: %s", strings.Join(failures, ", "))
	}
	return nil
}

var hyprProcRoot = "/proc"

func effectiveHyprlandConfig(c *Context) (string, error) {
	signature := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if signature != "" {
		result, err := c.Run("hyprctl", "-j", "instances")
		if err != nil {
			return "", fmt.Errorf("cannot identify the active Hyprland process: %w", err)
		}
		var instances []map[string]any
		decoder := json.NewDecoder(strings.NewReader(result.Stdout))
		decoder.UseNumber()
		if err = decoder.Decode(&instances); err != nil {
			return "", err
		}
		matches := []map[string]any{}
		for _, item := range instances {
			if item["instance"] == signature {
				matches = append(matches, item)
			}
		}
		if len(matches) != 1 {
			return "", fmt.Errorf("could not identify the active Hyprland process; its custom --config may differ from the default")
		}
		pid, ok := matches[0]["pid"].(json.Number)
		if !ok {
			return "", fmt.Errorf("Hyprland returned an invalid process ID")
		}
		number, err := strconv.Atoi(string(pid))
		if err != nil || number <= 1 {
			return "", fmt.Errorf("Hyprland returned an invalid process ID")
		}
		process := filepath.Join(hyprProcRoot, strconv.Itoa(number))
		info, err := os.Stat(process)
		if err != nil {
			return "", err
		}
		if owner, ok := info.Sys().(*syscall.Stat_t); !ok || int(owner.Uid) != os.Getuid() {
			return "", fmt.Errorf("selected Hyprland process belongs to another user")
		}
		cmdline, err := os.ReadFile(filepath.Join(process, "cmdline"))
		if err != nil {
			return "", err
		}
		argv := []string{}
		for _, item := range strings.Split(string(cmdline), "\x00") {
			if item != "" {
				argv = append(argv, item)
			}
		}
		if len(argv) == 0 || !strings.EqualFold(filepath.Base(argv[0]), "Hyprland") {
			return "", fmt.Errorf("could not validate the selected Hyprland process command")
		}
		path, err := hyprConfigArgument(argv)
		if err != nil {
			return "", err
		}
		if path != "" {
			if !filepath.IsAbs(path) {
				cwd, err := filepath.EvalSymlinks(filepath.Join(process, "cwd"))
				if err != nil {
					return "", err
				}
				path = filepath.Join(cwd, path)
			}
			if ext := filepath.Ext(path); ext != ".lua" && ext != ".conf" {
				return "", fmt.Errorf("cannot infer syntax of custom config %s; adjust launchers manually", path)
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				return "", fmt.Errorf("active Hyprland config does not exist: %s", path)
			}
			return filepath.Clean(path), nil
		}
	}
	directory := filepath.Join(c.ConfigHome, "hypr")
	lua, conf := filepath.Join(directory, "hyprland.lua"), filepath.Join(directory, "hyprland.conf")
	if exists(lua) && exists(conf) {
		return "", fmt.Errorf("both hyprland.lua and hyprland.conf exist; active --config selection is ambiguous")
	}
	for _, path := range []string{lua, conf} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("no existing hyprland.lua or hyprland.conf in %s; no replacement main config was created", directory)
}

func hyprConfigArgument(argv []string) (string, error) {
	paths := []string{}
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "-c" || arg == "--config":
			i++
			if i >= len(argv) {
				return "", fmt.Errorf("Hyprland config argument has no path")
			}
			paths = append(paths, argv[i])
		case strings.HasPrefix(arg, "--config="):
			paths = append(paths, strings.TrimPrefix(arg, "--config="))
		case strings.HasPrefix(arg, "-c") && !strings.HasPrefix(arg, "--") && len(arg) > 2:
			paths = append(paths, arg[2:])
		}
	}
	if len(paths) > 1 {
		return "", fmt.Errorf("multiple Hyprland config arguments are ambiguous")
	}
	if len(paths) == 0 {
		return "", nil
	}
	if paths[0] == "" {
		return "", fmt.Errorf("empty Hyprland config path")
	}
	return paths[0], nil
}

func InspectSession(c *Context) error {
	if c.Inventory == nil {
		c.Inventory = map[string]any{}
	}
	if c.Report == nil {
		c.Report = map[string]any{}
	}
	active, err := effectiveHyprlandConfig(c)
	if err != nil {
		return err
	}
	c.Inventory["active_config"] = active
	version, err := c.Run("hyprctl", "version")
	if err != nil {
		return err
	}
	c.Inventory["hyprland_version"] = strings.TrimSpace(version.Stdout)
	pattern := regexp.MustCompile(`\b(\d+)\.(\d+)\.\d+\b`)
	for _, binary := range []string{"/usr/bin/Hyprland", "/usr/bin/hyprland"} {
		if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			continue
		}
		result := c.Try(binary, "--version")
		c.Inventory["hyprland_binary"] = binary
		installed := strings.TrimSpace(result.Stdout + result.Stderr)
		c.Inventory["installed_hyprland_version"] = installed
		running, available := pattern.FindStringSubmatch(version.Stdout), pattern.FindStringSubmatch(installed)
		if result.Code == 0 && len(running) > 0 && len(available) > 0 && (running[1] != available[1] || running[2] != available[2]) {
			return fmt.Errorf("system upgrade changed Hyprland's configuration generation while the old compositor is still running; start a fresh session and rerun configuration")
		}
		break
	}
	for _, item := range []struct {
		key  string
		args []string
	}{{"monitors", []string{"hyprctl", "-j", "monitors", "all"}}, {"devices", []string{"hyprctl", "-j", "devices"}}} {
		result, err := c.Run(item.args...)
		if err != nil {
			return err
		}
		var value any
		if err = json.Unmarshal([]byte(result.Stdout), &value); err != nil {
			return fmt.Errorf("invalid Hyprland %s response: %w", item.key, err)
		}
		if item.key == "monitors" {
			if _, ok := value.([]any); !ok {
				return fmt.Errorf("Hyprland monitors response must be an array")
			}
		} else if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("Hyprland devices response must be an object")
		}
		c.Inventory[item.key] = value
	}
	c.Report["inventory"] = c.Inventory
	return c.Save()
}
