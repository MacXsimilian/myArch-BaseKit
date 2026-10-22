package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopDetectionPreservesDisabledAutostarts(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config/autostart")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{
		"waybar.desktop": "[Desktop Entry]\nExec=waybar\nHidden=true\n",
		"mako.desktop":   "[Desktop Entry]\nExec=mako\nX-GNOME-Autostart-enabled=false\n",
		"dunst.desktop":  "[Desktop Entry]\nExec=dunst\n",
		"app.desktop":    "[Desktop Entry]\nExec=ghostty\n[Desktop Action Other]\nExec=waybar\n",
	}
	for name, data := range fixtures {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := &cleanupRunner{installed: "noctalia.service enabled enabled\nwaybar.service disabled enabled\nother.service enabled enabled\n"}
	findings, err := detectCompetingDesktop(home, t.TempDir(), r)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(findings, "\n")
	if len(findings) != 2 || !strings.Contains(joined, "dunst.desktop") || !strings.Contains(joined, "noctalia.service") {
		t.Fatal(findings)
	}
	if len(r.runs) != 0 {
		t.Fatal("detection mutated services")
	}
	for name, data := range fixtures {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != data {
			t.Fatal("startup file changed")
		}
	}
}
func TestDesktopDetectionReportsUnavailableServiceManager(t *testing.T) {
	r := &cleanupRunner{err: errors.New("no user bus")}
	if _, err := detectCompetingDesktop(t.TempDir(), t.TempDir(), r); err == nil {
		t.Fatal("inspection failure hidden")
	}
}

func TestDesktopSystemAutostartsRespectUserAndSession(t *testing.T) {
	home := t.TempDir()
	system := t.TempDir()
	user := filepath.Join(home, ".config/autostart")
	if err := os.MkdirAll(user, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"waybar.desktop":    "[Desktop Entry]\nExec=waybar\n",
		"dunst.desktop":     "[Desktop Entry]\nExec=dunst\nOnlyShowIn=GNOME;\n",
		"mako.desktop":      "[Desktop Entry]\nExec=mako\nNotShowIn=niri;\n",
		"nm-applet.desktop": "[Desktop Entry]\nExec=nm-applet\nOnlyShowIn=niri;\n",
	} {
		if err := os.WriteFile(filepath.Join(system, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(user, "waybar.desktop"), []byte("[Desktop Entry]\nHidden=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	findings, err := detectCompetingDesktop(home, system, &cleanupRunner{})
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0], "nm-applet.desktop") {
		t.Fatalf("%v %v", findings, err)
	}
}

func TestDesktopDuplicateShellLaunchers(t *testing.T) {
	for _, command := range []string{"dms run", "/usr/bin/dms run --session", "env FOO=bar dms run", "sh -c 'dms run'", "qs -c dms", "quickshell -p /usr/share/quickshell/dms/shell.qml"} {
		if !duplicateShellCommand(command) {
			t.Errorf("missed %q", command)
		}
	}
	for _, command := range []string{"dms-greeter --command niri", "dms ipc call spotlight toggle", "qs -c unrelated", "quickshell -p /usr/share/dms-greeter/shell.qml"} {
		if duplicateShellCommand(command) {
			t.Errorf("helper reported %q", command)
		}
	}
	home := t.TempDir()
	system := t.TempDir()
	if err := os.WriteFile(filepath.Join(system, "shell.desktop"), []byte("[Desktop Entry]\nExec=dms run\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &cleanupRunner{installed: "dms.service enabled enabled\ncachyos-dms-direct.service enabled enabled\ndux.service enabled enabled\ndms-greeter.service enabled enabled\n"}
	findings, err := detectCompetingDesktop(home, system, r)
	if err != nil || len(findings) != 4 {
		t.Fatalf("%v %v", findings, err)
	}
}
