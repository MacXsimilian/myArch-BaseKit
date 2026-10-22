package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultFontsCheckEffectiveSelection(t *testing.T) {
	for _, target := range []string{"ready", "fontconfig", "ghostty", "editor", "terminal", "missing settings", "invalid settings"} {
		t.Run(target, func(t *testing.T) {
			c, r := defaultsContext(t)
			c.CheckOnly = true
			code := fmt.Sprintf(`{"editor.fontFamily":%q,"terminal.integrated.fontFamily":%q}`, BuildkitFont, BuildkitFont)
			if target == "editor" {
				code = fmt.Sprintf(`{"editor.fontFamily":"other","terminal.integrated.fontFamily":%q}`, BuildkitFont)
			} else if target == "terminal" {
				code = fmt.Sprintf(`{"editor.fontFamily":%q,"terminal.integrated.fontFamily":"other"}`, BuildkitFont)
			} else if target == "invalid settings" {
				code = `{"editor.fontFamily":"other","editor.fontFamily":"JetBrainsMono Nerd Font Mono"}`
			}
			if target != "missing settings" {
				storagePut(t, filepath.Join(c.ConfigHome, "Code/User/settings.json"), code)
			}
			r.reply = func(command Command) (CommandResult, error) {
				if command.Args[0] == "fc-match" {
					font := BuildkitFont
					if target == "fontconfig" && command.Args[len(command.Args)-1] == "monospace" {
						font = "DejaVu Sans Mono"
					}
					return CommandResult{Stdout: font}, nil
				}
				font := BuildkitFont
				if target == "ghostty" {
					font = "other"
				}
				return CommandResult{Stdout: "font-family = \"\"\nfont-family = " + font + "\nfont-family = fallback\n"}, nil
			}
			checks := map[string]string{}
			failures := checkDefaultFonts(c, checks)
			if (len(failures) == 0) != (target == "ready") {
				t.Fatalf("%v: %v", checks, failures)
			}
			if target != "ready" && checks["font_installed"] != BuildkitFont {
				t.Fatal("fixture must have the font installed while effective selection fails")
			}
			if len(reportRecords(c.Report, "file_changes")) != 0 || exists(c.ReportPath) {
				t.Fatal("font checks wrote configuration")
			}
		})
	}
}

func TestCodeFontsCommentsFallbacksAndInheritance(t *testing.T) {
	c, _ := defaultsContext(t)
	text := "{\n // user font selection\n \"editor.fontFamily\": \"'" + BuildkitFont + "', monospace\",\n \"terminal.integrated.fontFamily\": \"\",\n}\n"
	storagePut(t, filepath.Join(c.ConfigHome, "Code/User/settings.json"), text)
	editor, terminal, err := codeUserFonts(c)
	if err != nil || editor != BuildkitFont || terminal != BuildkitFont {
		t.Fatalf("%s %s %v", editor, terminal, err)
	}
	if ghosttyPrimaryFont("font-family = other\nfont-family = \"\"\nfont-family = "+BuildkitFont+"\n") != BuildkitFont {
		t.Fatal("Ghostty font reset was ignored")
	}
	if strings.Contains(storageRead(t, filepath.Join(c.ConfigHome, "Code/User/settings.json")), "null") {
		t.Fatal("read-only validation rewrote selected values")
	}
}
