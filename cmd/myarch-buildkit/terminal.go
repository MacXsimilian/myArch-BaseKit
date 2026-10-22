package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ghosttyDesktopID = "com.mitchellh.ghostty.desktop"
const terminalListHeader = "# myarch-buildkit preferred terminal"

// Keep fallback entries and directives, placing Ghostty before them. The
// desktop-specific list wins over the generic list in a Niri session.
func preferGhostty(data []byte) []byte {
	lines := []string{terminalListHeader, ghosttyDesktopID}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == terminalListHeader || trimmed == ghosttyDesktopID || trimmed == "+"+ghosttyDesktopID || trimmed == "-"+ghosttyDesktopID {
			continue
		}
		if len(data) == 0 {
			break
		}
		lines = append(lines, line)
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// BuildTerminalProfile configures terminal selection for desktop launchers,
// Dux, and newly started user services. It only reads; apply backs up and writes.
func BuildTerminalProfile(home string, s Settings) ([]FileChange, error) {
	if s.DMSConfigDir != "" && s.DMSConfigDir != "dms" && s.DMSConfigDir != "DankMaterialShell" {
		return nil, fmt.Errorf("unsupported dms_config_dir")
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local/state")
	}
	if !filepath.IsAbs(state) {
		return nil, fmt.Errorf("XDG_STATE_HOME must be absolute")
	}
	directory := filepath.Base(DMSConfigHome(home, s))
	sessionPath := filepath.Join(state, directory, "session.json")
	paths := []string{
		filepath.Join(home, ".config/xdg-terminals.list"),
		filepath.Join(home, ".config/niri-xdg-terminals.list"),
		filepath.Join(home, ".config/environment.d/90-myarch-buildkit.conf"),
		sessionPath,
	}
	changes := make([]FileChange, 0, len(paths))
	for i, path := range paths {
		if err := safeHomePath(home, path); err != nil {
			return nil, err
		}
		_, statErr := os.Lstat(path)
		if statErr != nil && !os.IsNotExist(statErr) {
			return nil, statErr
		}
		original, err := profileRead(path)
		if err != nil {
			return nil, err
		}
		var data []byte
		switch i {
		case 0, 1:
			data = preferGhostty(original)
		case 2:
			data = []byte("# Managed by myarch-buildkit; loaded by the user service manager.\nTERMINAL=ghostty\nEDITOR=code --wait\nVISUAL=code --wait\nGIT_EDITOR=code --wait\nGH_EDITOR=code --wait\n")
		case 3:
			session := map[string]json.RawMessage{}
			if len(original) > 0 {
				if err := json.Unmarshal(original, &session); err != nil || session == nil {
					return nil, fmt.Errorf("Dux session must be a JSON object: %s", path)
				}
			}
			session["terminalOverride"] = json.RawMessage(`"ghostty"`)
			data, err = json.MarshalIndent(session, "", "  ")
			if err != nil {
				return nil, err
			}
			data = append(data, '\n')
		}
		changes = append(changes, FileChange{Path: path, Data: data, Mode: 0600, Original: original, Existed: statErr == nil, CheckOriginal: true})
	}
	return changes, nil
}

// --print-id is explicitly nonlaunching in xdg-terminal-exec's CLI contract.
func CheckTerminalPreferences(runner Runner) error {
	output, err := runner.Output("xdg-terminal-exec", "--print-id")
	if err != nil {
		return fmt.Errorf("resolve preferred terminal: %w", err)
	}
	if selected := strings.TrimSpace(string(output)); selected != ghosttyDesktopID {
		return fmt.Errorf("xdg-terminal-exec selected %q; expected %s (check Ghostty installation and terminal preference overrides)", selected, ghosttyDesktopID)
	}
	return nil
}
