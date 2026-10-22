package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func firstFontFamily(value string) string {
	first, _, _ := strings.Cut(value, ",")
	return strings.Trim(strings.TrimSpace(first), "\"'")
}

// Read Ghostty's resolved configuration so later includes and resets are
// respected. This is the default for new windows; per-launch flags are external.
func ghosttyPrimaryFont(text string) string {
	fonts := []string{}
	for _, line := range sourceLines(text) {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found || strings.TrimSpace(key) != "font-family" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"")
		if value == "" {
			fonts = nil
		} else {
			fonts = append(fonts, value)
		}
	}
	if len(fonts) == 0 {
		return ""
	}
	return fonts[0]
}

func codeUserFonts(c *Context) (string, string, error) {
	path := filepath.Join(c.ConfigHome, "Code/User/settings.json")
	text, err := configRead(c, path)
	if err != nil {
		return "", "", err
	}
	// Reuse the source-preserving parser's duplicate-key and document checks.
	if _, err = EditJSONC(text, map[string]any{"editor.fontFamily": nil, "terminal.integrated.fontFamily": nil}); err != nil {
		return "", "", fmt.Errorf("VS Code user settings: %w", err)
	}
	clean, err := stripJSONC(text)
	if err != nil {
		return "", "", err
	}
	clean, err = withoutTrailingJSONCommas(clean)
	if err != nil {
		return "", "", err
	}
	values := map[string]any{}
	if err = json.Unmarshal([]byte(clean), &values); err != nil {
		return "", "", err
	}
	editor, _ := values["editor.fontFamily"].(string)
	terminal, _ := values["terminal.integrated.fontFamily"].(string)
	if strings.TrimSpace(terminal) == "" {
		terminal = editor
	}
	return firstFontFamily(editor), firstFontFamily(terminal), nil
}

func checkDefaultFonts(c *Context, checks map[string]string) []string {
	failures := []string{}
	for _, item := range []struct{ key, query, failure string }{
		{"font_installed", BuildkitFont, "installed monospace font"},
		{"font", "monospace", "effective Fontconfig monospace default"},
	} {
		reply := c.Try("fc-match", "-f", "%{family}\n", item.query)
		checks[item.key] = strings.TrimSpace(reply.Stdout)
		if reply.Code != 0 || firstFontFamily(checks[item.key]) != BuildkitFont {
			failures = append(failures, item.failure)
		}
	}
	ghostty, err := c.Command(Command{Args: []string{"ghostty", "+show-config"}, Timeout: 15 * time.Second})
	checks["ghostty_font"] = ghosttyPrimaryFont(ghostty.Stdout)
	if err != nil || checks["ghostty_font"] != BuildkitFont {
		failures = append(failures, "effective Ghostty font")
	}
	editor, terminal, err := codeUserFonts(c)
	checks["vscode_editor_font"], checks["vscode_terminal_font"] = editor, terminal
	checks["vscode_font_scope"] = "default user settings; workspace and profile overrides require an editor check"
	if err != nil {
		checks["vscode_font_error"] = err.Error()
	}
	if err != nil || editor != BuildkitFont || terminal != BuildkitFont {
		failures = append(failures, "VS Code user font defaults")
	}
	return failures
}
