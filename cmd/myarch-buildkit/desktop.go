package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var competingDesktopNames = []string{"waybar", "mako", "dunst", "swaync", "noctalia", "hyprpolkitagent", "nm-applet"}

func competingName(value string) bool {
	lower := strings.ToLower(value)
	for _, name := range competingDesktopNames {
		if strings.Contains(lower, name) {
			return true
		}
	}
	return false
}

// DetectCompetingDesktop reports candidates for manual review; it never disables
// services or modifies startup files. Session-specific entries can be intentional.
func DetectCompetingDesktop(home string, r Runner) ([]string, error) {
	return detectCompetingDesktop(home, "/etc/xdg/autostart", r)
}
func detectCompetingDesktop(home, systemDirectory string, r Runner) ([]string, error) {
	var findings []string
	// The user's basename overrides its system counterpart, including Hidden=true.
	paths := map[string]string{}
	for _, directory := range []string{systemDirectory, filepath.Join(home, ".config/autostart")} {
		entries, err := os.ReadDir(directory)
		if err != nil && !os.IsNotExist(err) {
			return findings, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".desktop") {
				paths[entry.Name()] = filepath.Join(directory, entry.Name())
			}
		}
	}
	for name, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return findings, err
		}
		fields := map[string]string{}
		active := false
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") {
				active = line == "[Desktop Entry]"
				continue
			}
			if !active || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if ok {
				fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
		if strings.EqualFold(fields["Hidden"], "true") || strings.EqualFold(fields["X-GNOME-Autostart-enabled"], "false") {
			continue
		}
		if only := fields["OnlyShowIn"]; only != "" && !includesNiri(only) {
			continue
		}
		if includesNiri(fields["NotShowIn"]) {
			continue
		}
		command := fields["Exec"]
		if command != "" && (competingName(command) || competingName(name) || duplicateShellCommand(command)) {
			findings = append(findings, "Autostart: "+path+" ("+command+")")
		}
	}
	output, err := r.Output("systemctl", "--user", "list-unit-files", "--type=service", "--state=enabled,enabled-runtime", "--no-legend", "--no-pager")
	if err != nil {
		return findings, fmt.Errorf("inspect enabled desktop services: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[1] == "enabled" || fields[1] == "enabled-runtime") && (competingName(fields[0]) || duplicateShellService(fields[0])) {
			findings = append(findings, "Enabled user service: "+fields[0])
		}
	}
	sort.Strings(findings)
	return findings, nil
}

func includesNiri(value string) bool {
	for _, desktop := range strings.Split(value, ";") {
		if strings.EqualFold(strings.TrimSpace(desktop), "niri") {
			return true
		}
	}
	return false
}

// Explicit shell launchers only: dms-greeter and other DMS helper commands are
// not a second desktop shell. This is detection, never shell evaluation.
func duplicateShellService(name string) bool {
	switch strings.ToLower(name) {
	case "dms.service", "cachyos-dms-direct.service", "dux.service", "dux-shell.service", "dankmaterialshell.service":
		return true
	}
	return false
}
func duplicateShellCommand(command string) bool {
	fields := strings.Fields(command)
	for i, field := range fields {
		executable := strings.ToLower(filepath.Base(strings.Trim(field, "\"'")))
		if (executable == "dms" || executable == "dux") && i+1 < len(fields) && strings.Trim(fields[i+1], "\"'") == "run" {
			return true
		}
		if executable != "qs" && executable != "quickshell" {
			continue
		}
		for _, argument := range fields[i+1:] {
			value := strings.ToLower(strings.Trim(argument, "\"'"))
			if value == "dms" || value == "dux" || strings.Contains(value, "/dms/") || strings.Contains(value, "/dankmaterialshell/") || strings.Contains(value, "/dux/") || strings.HasSuffix(value, "/dms") || strings.HasSuffix(value, "/dankmaterialshell") || strings.HasSuffix(value, "/dux") {
				return true
			}
		}
	}
	return false
}
