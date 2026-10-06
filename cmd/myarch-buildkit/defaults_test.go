package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type defaultsRunner struct {
	calls []Command
	reply func(Command) (CommandResult, error)
}

func (r *defaultsRunner) Execute(command Command) (CommandResult, error) {
	r.calls = append(r.calls, command)
	if r.reply != nil {
		return r.reply(command)
	}
	return CommandResult{}, nil
}

func defaultsContext(t *testing.T) (*Context, *defaultsRunner) {
	t.Helper()
	home := t.TempDir()
	runner := &defaultsRunner{}
	c := &Context{Home: home, ConfigHome: filepath.Join(home, "config"), DataHome: filepath.Join(home, "data"), StateHome: filepath.Join(home, "state"), CacheHome: filepath.Join(home, "cache"), BinDir: filepath.Join(home, "bin"), RunDir: filepath.Join(home, "run"), Report: map[string]any{}, Inventory: map[string]any{}, Runner: runner, Binary: filepath.Join(home, "bin", "myarch-buildkit")}
	c.ReportPath = filepath.Join(c.RunDir, "report.json")
	return c, runner
}
func defaultsPut(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func defaultsRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func decodedJSONC(t *testing.T, text string) map[string]any {
	t.Helper()
	clean, err := stripJSONC(text)
	if err != nil {
		t.Fatal(err)
	}
	chars := []byte(clean)
	for i := 0; i < len(chars); {
		if chars[i] == '"' {
			_, end, err := jsonValueAt(clean, i)
			if err != nil {
				t.Fatal(err)
			}
			i = end
		} else if chars[i] == ',' {
			next := jsonSkip(clean, i+1)
			if next < len(chars) && (chars[next] == '}' || chars[next] == ']') {
				chars[i] = ' '
			}
			i++
		} else {
			i++
		}
	}
	var value map[string]any
	if err = json.Unmarshal(chars, &value); err != nil {
		t.Fatalf("invalid edited JSONC: %v\n%s", err, text)
	}
	return value
}

func TestDefaultsJSONCPreservesCommentsAndNestedSettings(t *testing.T) {
	before := "\xef\xbb\xbf{\r\n\t// Keep this explanatory comment\r\n\t\"url\": \"https://host/a//b/*c*/\",\r\n\t\"日本語\": {\"editor.fontFamily\": \"nested\",},\r\n\t\"editor.fontFamily\": /* retain */ \"old\",\r\n}\r\n"
	after, err := EditJSONC(before, map[string]any{"editor.fontFamily": "new", "terminal.integrated.fontFamily": "new"})
	if err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{"// Keep this explanatory comment", "/* retain */", "\"日本語\": {\"editor.fontFamily\": \"nested\",}", "https://host/a//b/*c*/", "\xef\xbb\xbf"} {
		if !strings.Contains(after, kept) {
			t.Fatalf("lost unrelated source %q: %s", kept, after)
		}
	}
	value := decodedJSONC(t, after)
	if value["editor.fontFamily"] != "new" || value["terminal.integrated.fontFamily"] != "new" {
		t.Fatal(value)
	}
	if strings.Contains(strings.ReplaceAll(after, "\r\n", ""), "\n") {
		t.Fatal("did not preserve CRLF")
	}
	again, err := EditJSONC(after, map[string]any{"editor.fontFamily": "new", "terminal.integrated.fontFamily": "new"})
	if err != nil || again != after {
		t.Fatalf("not idempotent: %v\n%s", err, again)
	}
}

func TestDefaultsJSONCInsertsAroundTrailingComment(t *testing.T) {
	for _, text := range []string{`{}`, `{ "keep": true }`, "{\n  \"keep\": true // comment\n}\n", `{"keep":[1,2,],}`} {
		updated, err := EditJSONC(text, map[string]any{"new": true})
		if err != nil {
			t.Fatal(err)
		}
		value := decodedJSONC(t, updated)
		if value["new"] != true {
			t.Fatal(updated)
		}
		if strings.Contains(text, "// comment") && !strings.Contains(updated, "// comment") {
			t.Fatal("comment removed")
		}
	}
}

func TestDefaultsJSONCRejectsAmbiguousOrMalformedInput(t *testing.T) {
	for _, text := range []string{`{"font":1,"font":2}`, `[]`, `null`, `{} {}`, `{"font":2 /*oops}`, `{"font":}`} {
		if _, err := EditJSONC(text, map[string]any{"font": "new"}); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
	if _, err := EditJSONC(`{"unrelated":1,"unrelated":2}`, map[string]any{"font": "new"}); err != nil {
		t.Fatal("unrelated duplicate need not block selected settings:", err)
	}
}

func TestDefaultsMIMEPreservesFallbacksAndUnrelatedSections(t *testing.T) {
	before := "# My defaults\r\n[Default Applications]\r\n text/plain = old.desktop;code.desktop;fallback.desktop;\r\nimage/png=viewer.desktop;\r\n[Added Associations]\r\ntext/plain=old.desktop;code.desktop;\r\n[Removed Associations]\r\ntext/plain=code.desktop;blocked.desktop;\r\n[Other]\r\nkeep=yes\r\n"
	selected := map[string]string{"text/plain": "code.desktop", "text/x-go": "code.desktop"}
	updated := EditMIMEDefaults(EditMIMEAssociations(before, selected), selected)
	for _, part := range []string{"# My defaults\r\n", " text/plain = code.desktop;old.desktop;fallback.desktop;\r\n", "image/png=viewer.desktop;\r\n", "text/plain=blocked.desktop;\r\n", "[Other]\r\nkeep=yes\r\n"} {
		if !strings.Contains(updated, part) {
			t.Fatalf("missing %q in %s", part, updated)
		}
	}
	if strings.Count(updated, "text/x-go=code.desktop;") != 2 {
		t.Fatal(updated)
	}
	if got := EditMIMEDefaults(EditMIMEAssociations(updated, selected), selected); got != updated {
		t.Fatal("MIME edit is not idempotent")
	}
}

func TestDefaultsMIMEAddsOnlySelectedAssociation(t *testing.T) {
	before := "[Removed Associations]\ntext/plain=code.desktop;\ntext/x-go=other.desktop;\n"
	updated := EditMIMEAssociations(before, map[string]string{"text/plain": "code.desktop"})
	if !strings.Contains(updated, "text/plain=\n") || !strings.Contains(updated, "text/x-go=other.desktop;\n") || !strings.Contains(updated, "[Added Associations]\ntext/plain=code.desktop;\n") {
		t.Fatal(updated)
	}
}

func TestDefaultsLauncherRecognizesOnlySharedOptions(t *testing.T) {
	for _, item := range []struct{ before, after string }{{"kitty", "ghostty"}, {" uwsm app -- /usr/bin/kitty -e bash -lc 'echo hi'", " uwsm app -- /usr/bin/ghostty -e bash -lc 'echo hi'"}, {"gedit --wait", "code --wait"}} {
		got := launcherCommand(item.before)
		if got == nil || !got.Safe || got.Replacement != item.after {
			t.Fatalf("%q: %#v", item.before, got)
		}
	}
	for _, command := range []string{"kitty --class mine", "kitty --hold", "kitty; rm -rf /", "kitty $(touch file)", "kitty -e 'broken", "code --profile mine"} {
		got := launcherCommand(command)
		if got == nil || got.Safe {
			t.Fatalf("unsafe command recognized: %q %#v", command, got)
		}
	}
	for _, command := range []string{"sh -c kitty", "env TERM=x kitty", "~/bin/kitty", "/custom/kitty", "kitty.sh", "launch-terminal"} {
		if got := launcherCommand(command); got != nil {
			t.Fatalf("guessed wrapper or custom command: %q %#v", command, got)
		}
	}
}

func TestDefaultsLuaTokenizerPreservesOffsetsAndSkipsComments(t *testing.T) {
	text := "--[=[ ignore require('secret') ]=]\nlocal terminal = 'kitty' -- comment\nhl.bind('SUPER', 'Return', hl.dsp.exec_cmd(terminal))\n"
	tokens, clean, err := tokenizeLua(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(clean) != len(text) {
		t.Fatal("source offsets changed")
	}
	for _, token := range tokens {
		if token.Value == "require" || token.Value == "secret" {
			t.Fatal("tokenized comment")
		}
		if text[token.Start:token.End] == "'kitty'" && !token.Known {
			t.Fatal("plain literal unknown")
		}
	}
	if strings.Count(clean, "\n") != strings.Count(text, "\n") {
		t.Fatal("line offsets changed")
	}
	for _, invalid := range []string{"--[=[oops", "'oops", "[[oops"} {
		if _, _, err = tokenizeLua(invalid); err == nil {
			t.Fatal("accepted incomplete Lua")
		}
	}
}

func TestDefaultsLuaEditsVerifiedVariablesAndLiteralBindings(t *testing.T) {
	c, _ := defaultsContext(t)
	path := filepath.Join(c.ConfigHome, "hypr/hyprland.lua")
	before := "-- personal comment\nlocal terminal = 'uwsm app -- kitty -e bash'\nlocal editor = 'gedit --wait'\nhl.bind('SUPER', 'Return', hl.dsp.exec_cmd(terminal))\nhl.bind('SUPER', 'T', hl.dsp.exec_cmd(editor))\n"
	defaultsPut(t, path, before)
	details, err := EditHyprlandLaunchers(c, path, true)
	if err != nil {
		t.Fatal(err)
	}
	if details["launcher_verified"] != true || details["editor_verified"] != true {
		t.Fatal(details)
	}
	after := defaultsRead(t, path)
	for _, part := range []string{"-- personal comment", "uwsm app -- ghostty -e bash", "code --wait"} {
		if !strings.Contains(after, part) {
			t.Fatal(after)
		}
	}
	if !reflect.DeepEqual(stringList(details["terminal_candidates"]), []string{"kitty"}) {
		t.Fatal(details)
	}
}

func TestDefaultsLuaRefusesDynamicIncludeAndShadowing(t *testing.T) {
	for _, text := range []string{
		"local terminal = 'kitty'\nrequire(config_path)\nhl.bind('SUPER', 'Return', hl.dsp.exec_cmd(terminal))\n",
		"local terminal = 'kitty'\nhl.bind('SUPER', 'Return', function() local terminal = 'foot'; hl.exec_cmd(terminal) end)\n",
		"local terminal = 'kitty'\nif true then terminal = 'foot' end\nhl.bind('SUPER', 'Return', hl.dsp.exec_cmd(terminal))\n",
	} {
		c, _ := defaultsContext(t)
		path := filepath.Join(c.ConfigHome, "hypr/hyprland.lua")
		defaultsPut(t, path, text)
		details, err := EditHyprlandLaunchers(c, path, true)
		if err != nil {
			t.Fatal(err)
		}
		if details["launcher_verified"] != false || defaultsRead(t, path) != text {
			t.Fatalf("unsafe config changed: %#v", details)
		}
	}
}

func TestDefaultsLuaStaticIncludeTracksSourceOrder(t *testing.T) {
	c, _ := defaultsContext(t)
	main := filepath.Join(c.ConfigHome, "hypr/hyprland.lua")
	child := filepath.Join(c.ConfigHome, "hypr/apps.lua")
	defaultsPut(t, main, "require('apps')\nhl.bind('SUPER','Return',hl.dsp.exec_cmd(terminal))\n")
	defaultsPut(t, child, "terminal = 'alacritty'\n")
	details, err := EditHyprlandLaunchers(c, main, true)
	if err != nil {
		t.Fatal(err)
	}
	if details["launcher_verified"] != true || !strings.Contains(defaultsRead(t, child), "ghostty") {
		t.Fatal(details)
	}
}

func TestDefaultsLegacyEditsOnlySafeLaunchers(t *testing.T) {
	c, _ := defaultsContext(t)
	path := filepath.Join(c.ConfigHome, "hypr/hyprland.conf")
	before := "# preserve\n$terminal = uwsm app -- kitty\n$editor = gedit --wait\nbind = SUPER, Return, exec, $terminal\nbindd = SUPER, T, Editor, exec, $editor\nbind = SUPER, F, exec, custom-wrapper\n"
	defaultsPut(t, path, before)
	details, err := EditHyprlandLaunchers(c, path, true)
	if err != nil {
		t.Fatal(err)
	}
	if details["launcher_verified"] != true || details["editor_verified"] != true {
		t.Fatal(details)
	}
	after := defaultsRead(t, path)
	if !strings.Contains(after, "$terminal = uwsm app -- ghostty") || !strings.Contains(after, "$editor = code --wait") || !strings.Contains(after, "custom-wrapper") {
		t.Fatal(after)
	}
}

func TestDefaultsIncludeEscapingOrRepeatedLegacyPreservesGraph(t *testing.T) {
	c, _ := defaultsContext(t)
	path := filepath.Join(c.ConfigHome, "hypr/hyprland.conf")
	child := filepath.Join(c.ConfigHome, "hypr/apps.conf")
	defaultsPut(t, child, "$terminal=kitty\n")
	before := "source = " + child + "\nsource = " + child + "\nbind = SUPER, Return, exec, $terminal\n"
	defaultsPut(t, path, before)
	details, err := EditHyprlandLaunchers(c, path, true)
	if err != nil {
		t.Fatal(err)
	}
	if details["launcher_verified"] != false || defaultsRead(t, child) != "$terminal=kitty\n" {
		t.Fatal("repeated source graph modified")
	}
	outside := t.TempDir()
	defaultsPut(t, filepath.Join(outside, "apps.lua"), "terminal='kitty'\n")
	lua := filepath.Join(c.ConfigHome, "hypr/hyprland.lua")
	text := "require('" + filepath.Join(outside, "apps.lua") + "')\nhl.bind('SUPER','Return',hl.dsp.exec_cmd('kitty'))\n"
	defaultsPut(t, lua, text)
	details, err = EditHyprlandLaunchers(c, lua, true)
	if err != nil {
		t.Fatal(err)
	}
	if details["launcher_verified"] != false || defaultsRead(t, lua) != text {
		t.Fatal("outside include graph modified")
	}
}

func defaultsDesktopFixtures(t *testing.T, c *Context) {
	t.Helper()
	t.Setenv("XDG_DATA_DIRS", filepath.Join(c.Home, "system-data"))
	t.Setenv("XDG_CURRENT_DESKTOP", "Hyprland")
	for _, item := range [][2]string{{"com.mitchellh.ghostty.desktop", ""}, {"code.desktop", "application/x-code-workspace;"}, {"obsidian.desktop", "x-scheme-handler/obsidian;"}, {"firefox.desktop", "application/pdf;"}, {"org.gnome.Nautilus.desktop", "inode/directory;"}, {"vlc.desktop", "audio/mpeg;video/mp4;image/png;x-scheme-handler/http;application/ogg;"}} {
		defaultsPut(t, filepath.Join(c.DataHome, "applications", item[0]), "[Desktop Entry]\nType=Application\nMimeType="+item[1]+"\n")
	}
	executable := filepath.Join(c.BinDir, "obsidian")
	defaultsPut(t, executable, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(executable, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", c.BinDir)
}

func TestDefaultsConfigurationPreservesPersonalSettingsAndSymlinks(t *testing.T) {
	c, _ := defaultsContext(t)
	defaultsDesktopFixtures(t, c)
	settings := filepath.Join(c.ConfigHome, "Code/User/settings.json")
	target := filepath.Join(c.Home, "personal-code.json")
	defaultsPut(t, target, "{\n  // personal\n  \"workbench.colorTheme\": \"My existing theme\",\n  \"other\": true\n}\n")
	if err := os.MkdirAll(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, settings); err != nil {
		t.Fatal(err)
	}
	defaultsPut(t, filepath.Join(c.ConfigHome, "ghostty/config"), "theme = my-existing-theme\nfont-size = 14\n")
	mime := filepath.Join(c.ConfigHome, "mimeapps.list")
	mimeTarget := filepath.Join(c.Home, "my-mimeapps.list")
	defaultsPut(t, mimeTarget, "[Default Applications]\nimage/png=viewer.desktop;\ntext/plain=old.desktop;\n")
	if err := os.Symlink(mimeTarget, mime); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureDefaults(c); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(settings); err != nil || link != target {
		t.Fatal("Code symlink was replaced", err)
	}
	if link, err := os.Readlink(mime); err != nil || link != mimeTarget {
		t.Fatal("MIME symlink was replaced", err)
	}
	if text := defaultsRead(t, target); !strings.Contains(text, "// personal") || !strings.Contains(text, "My existing theme") || !strings.Contains(text, BuildkitFont) {
		t.Fatal(text)
	}
	if text := defaultsRead(t, mimeTarget); !strings.Contains(text, "image/png=viewer.desktop;") || !strings.Contains(text, "text/plain=code.desktop;old.desktop;") || strings.Contains(text, "x-scheme-handler/http=vlc.desktop") {
		t.Fatal(text)
	}
	if text := defaultsRead(t, filepath.Join(c.ConfigHome, "ghostty/config")); !strings.Contains(text, "theme = my-existing-theme") || !strings.Contains(text, "config-file =") {
		t.Fatal(text)
	}
	desktop := defaultsRead(t, filepath.Join(c.DataHome, "applications", MarkdownDesktop))
	if strings.Contains(desktop, "python") || !strings.Contains(desktop, " obsidian-open --executable ") {
		t.Fatal(desktop)
	}
}

func TestDefaultsHiddenDesktopOverrideBlocksConfiguration(t *testing.T) {
	c, _ := defaultsContext(t)
	defaultsDesktopFixtures(t, c)
	path := filepath.Join(c.DataHome, "applications/code.desktop")
	defaultsPut(t, path, "[Desktop Entry]\nHidden=true\n")
	if err := ConfigureDefaults(c); err == nil {
		t.Fatal("accepted hidden selected application")
	}
	if exists(filepath.Join(c.ConfigHome, "ghostty/config.ghostty")) {
		t.Fatal("wrote defaults after discovery failure")
	}
}

func TestDefaultsUnsupportedMIMEsPreserveThoseDefaultsAndConfigureOthers(t *testing.T) {
	c, _ := defaultsContext(t)
	defaultsDesktopFixtures(t, c)
	defaultsPut(t, filepath.Join(c.DataHome, "applications/firefox.desktop"), "[Desktop Entry]\nType=Application\nMimeType=text/html;\n")
	defaultsPut(t, filepath.Join(c.DataHome, "applications/vlc.desktop"), "[Desktop Entry]\nType=Application\nMimeType=audio/mpeg;\n")
	mime := filepath.Join(c.ConfigHome, "mimeapps.list")
	defaultsPut(t, mime, "[Default Applications]\napplication/pdf=my-pdf.desktop;\nvideo/mp4=my-player.desktop;\n")
	if err := ConfigureDefaults(c); err == nil {
		t.Fatal("unsupported advertised types were not reported")
	}
	text := defaultsRead(t, mime)
	for _, part := range []string{"application/pdf=my-pdf.desktop;", "video/mp4=my-player.desktop;", "text/plain=code.desktop;", "text/markdown=" + MarkdownDesktop + ";"} {
		if !strings.Contains(text, part) {
			t.Fatal(text)
		}
	}
	if strings.Contains(text, "audio/mpeg=vlc.desktop;") {
		t.Fatal("configured partially advertised media types")
	}
}

func TestDefaultsLuaMultilineUnknownLiteralIsPreserved(t *testing.T) {
	c, _ := defaultsContext(t)
	path := filepath.Join(c.ConfigHome, "hypr/hyprland.lua")
	before := "local terminal = \"kitty\\\nargument\"\nhl.bind('SUPER','Return',hl.dsp.exec_cmd(terminal))\n"
	defaultsPut(t, path, before)
	details, err := EditHyprlandLaunchers(c, path, true)
	if err != nil {
		t.Fatal(err)
	}
	if details["launcher_verified"] != false || defaultsRead(t, path) != before {
		t.Fatal("unknown multiline command was changed")
	}
}

func TestDefaultsChecksAreReadonlyAndVerifyActualHandlers(t *testing.T) {
	c, r := defaultsContext(t)
	c.CheckOnly = true
	expected := map[string]string{"text/plain": "code.desktop", "x-scheme-handler/obsidian": "obsidian.desktop", "text/markdown": MarkdownDesktop, "inode/directory": "org.gnome.Nautilus.desktop", "application/pdf": "firefox.desktop", "audio/mpeg": "vlc.desktop", "video/mp4": "vlc.desktop"}
	r.reply = func(command Command) (CommandResult, error) {
		a := command.Args
		out := ""
		if a[0] == "xdg-mime" {
			out = expected[a[3]]
		} else if a[0] == "fc-match" {
			out = BuildkitFont
		} else if a[1] == "--print-id" {
			out = "com.mitchellh.ghostty.desktop"
		} else {
			out = "ghostty"
		}
		return CommandResult{Stdout: out}, nil
	}
	if err := CheckDefaults(c); err != nil {
		t.Fatal(err)
	}
	if exists(c.ReportPath) || len(reportRecords(c.Report, "file_changes")) != 0 {
		t.Fatal("read-only check wrote state")
	}
	expected["video/mp4"] = "other.desktop"
	if err := CheckDefaults(c); err == nil || !strings.Contains(err.Error(), "video/mp4") {
		t.Fatal("failed default was accepted")
	}
}

func TestDefaultsHyprlandConfigArgumentVariants(t *testing.T) {
	for _, argv := range [][]string{{"Hyprland", "--config", "custom.lua"}, {"Hyprland", "--config=custom.lua"}, {"Hyprland", "-ccustom.lua"}} {
		path, err := hyprConfigArgument(argv)
		if err != nil || path != "custom.lua" {
			t.Fatalf("%v: %s %v", argv, path, err)
		}
	}
	for _, argv := range [][]string{{"Hyprland", "-c"}, {"Hyprland", "--config="}, {"Hyprland", "-ca.lua", "--config=b.lua"}} {
		if _, err := hyprConfigArgument(argv); err == nil {
			t.Fatal("accepted ambiguous arguments", argv)
		}
	}
}

func TestDefaultsInspectSessionUsesVerifiedActiveCustomConfig(t *testing.T) {
	c, r := defaultsContext(t)
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "selected")
	original := hyprProcRoot
	hyprProcRoot = filepath.Join(c.Home, "proc")
	t.Cleanup(func() { hyprProcRoot = original })
	process := filepath.Join(hyprProcRoot, "123")
	defaultsPut(t, filepath.Join(process, "cmdline"), "/usr/bin/Hyprland\x00--config\x00custom.lua\x00")
	directory := filepath.Join(c.Home, "custom")
	defaultsPut(t, filepath.Join(directory, "custom.lua"), "-- personal\n")
	if err := os.Symlink(directory, filepath.Join(process, "cwd")); err != nil {
		t.Fatal(err)
	}
	r.reply = func(command Command) (CommandResult, error) {
		switch strings.Join(command.Args, " ") {
		case "hyprctl -j instances":
			return CommandResult{Stdout: `[{"instance":"selected","pid":123}]`}, nil
		case "hyprctl version":
			return CommandResult{Stdout: "Hyprland 0.55.0"}, nil
		case "hyprctl -j monitors all":
			return CommandResult{Stdout: `[{"name":"eDP-1"}]`}, nil
		case "hyprctl -j devices":
			return CommandResult{Stdout: `{"keyboards":[]}`}, nil
		}
		return CommandResult{}, nil
	}
	if err := InspectSession(c); err != nil {
		t.Fatal(err)
	}
	if c.Inventory["active_config"] != filepath.Join(directory, "custom.lua") {
		t.Fatal(c.Inventory)
	}
	defaultsPut(t, filepath.Join(process, "cmdline"), "/usr/bin/sh\x00--config\x00custom.lua\x00")
	if err := InspectSession(c); err == nil {
		t.Fatal("accepted unrelated process")
	}
}

func TestDefaultsObsidianOpenerUsesExistingLocalFilesOnly(t *testing.T) {
	c, r := defaultsContext(t)
	path := filepath.Join(c.Home, "vault/note with spaces # ü.md")
	defaultsPut(t, path, "# Existing note\n")
	if err := ObsidianOpen(c, []string{"--executable", "/usr/bin/obsidian", path}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || len(r.calls[0].Args) != 2 || !strings.Contains(r.calls[0].Args[1], "%20") || !strings.Contains(r.calls[0].Args[1], "%23") {
		t.Fatal(r.calls)
	}
	if !r.calls[0].Detached {
		t.Fatal("Markdown opener must detach the GUI from installer command timeouts")
	}
	if err := ObsidianOpen(c, []string{"file://remote/vault/note.md", filepath.Join(c.Home, "missing.md")}); err == nil {
		t.Fatal("accepted unsupported path")
	}
	if len(r.calls) != 1 {
		t.Fatal("launched unsupported files")
	}
}
