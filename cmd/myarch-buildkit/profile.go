package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

//go:embed profiles/*
var profileAssets embed.FS

var profileIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func profileRead(path string) ([]byte, error) {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("configuration path is a symlink: %s", current)
		}
		if err == nil && current == path && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("configuration path is not a regular file: %s", path)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func profileScale(value string) (string, error) {
	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return "", fmt.Errorf("invalid scale %q", value)
	}
	n, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return "", fmt.Errorf("invalid scale %q", value)
	}
	if len(parts) == 2 {
		d, e := strconv.ParseFloat(parts[1], 64)
		if e != nil || d == 0 {
			return "", fmt.Errorf("invalid scale %q", value)
		}
		n /= d
	}
	if !(n >= 0.25 && n <= 8) {
		return "", fmt.Errorf("scale must be between 0.25 and 8")
	}
	return strconv.FormatFloat(n, 'f', -1, 64), nil
}

// BuildProfile only reads files. The caller validates and backs up every change
// before committing the returned batch.
func BuildProfile(home string, s Settings) ([]FileChange, error) {
	if s.DMSConfigDir != "" && s.DMSConfigDir != "dms" && s.DMSConfigDir != "DankMaterialShell" {
		return nil, fmt.Errorf("unsupported dms_config_dir")
	}
	originals := map[string][]byte{}
	existed := map[string]bool{}
	read := func(path string) ([]byte, error) {
		_, statErr := os.Lstat(path)
		if statErr != nil && !os.IsNotExist(statErr) {
			return nil, statErr
		}
		data, err := profileRead(path)
		if err != nil {
			return nil, err
		}
		originals[path] = data
		existed[path] = statErr == nil
		return data, nil
	}
	if !filepath.IsAbs(home) {
		return nil, fmt.Errorf("home must be absolute")
	}
	if !profileIdentifier.MatchString(s.Monitor) || !profileIdentifier.MatchString(s.Keyboard) {
		return nil, fmt.Errorf("monitor and keyboard layout must be explicit identifiers")
	}
	scale, err := profileScale(s.Scale)
	if err != nil {
		return nil, err
	}
	s.Scale = scale
	main := s.ConfigPath
	if strings.HasPrefix(main, "~/") {
		main = filepath.Join(home, main[2:])
	}
	if main == "" {
		main = filepath.Join(home, ".config/niri/config.kdl")
	}
	main = filepath.Clean(main)
	rel, err := filepath.Rel(home, main)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !filepath.IsAbs(main) {
		return nil, fmt.Errorf("config_path must be inside your home")
	}
	if filepath.Ext(main) != ".kdl" {
		return nil, fmt.Errorf("config_path must be a Niri .kdl file")
	}
	if _, err := read(main); err != nil {
		return nil, err
	}
	raw, err := profileAssets.ReadFile("profiles/niri.kdl.tmpl")
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("niri").Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var rendered bytes.Buffer
	if err = tmpl.Execute(&rendered, s); err != nil {
		return nil, err
	}
	settingsPath := filepath.Join(DMSConfigHome(home, s), "settings.json")
	settingsBytes, err := read(settingsPath)
	if err != nil {
		return nil, err
	}
	preferences := map[string]any{}
	if len(settingsBytes) > 0 {
		if err = json.Unmarshal(settingsBytes, &preferences); err != nil || preferences == nil {
			return nil, fmt.Errorf("DMS settings must be a JSON object: %s", settingsPath)
		}
	}
	defaults, err := profileAssets.ReadFile("profiles/dms-settings.json")
	if err != nil {
		return nil, err
	}
	var desired map[string]any
	if err = json.Unmarshal(defaults, &desired); err != nil {
		return nil, err
	}
	for key, value := range desired {
		preferences[key] = value
	}
	merged, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return nil, err
	}
	changes := []FileChange{{Path: main, Data: rendered.Bytes(), Mode: 0600}, {Path: settingsPath, Data: append(merged, '\n'), Mode: 0600}}
	shellTemplate, err := template.ParseFS(profileAssets, "profiles/shell.tmpl")
	if err != nil {
		return nil, err
	}
	for _, shell := range []string{"bash", "zsh"} {
		rc := filepath.Join(home, "."+shell+"rc")
		if _, err := os.Lstat(rc); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		previous, err := read(rc)
		if err != nil {
			return nil, err
		}
		script := filepath.Join(home, ".config/myarch-buildkit/shell."+shell)
		if _, err := read(script); err != nil {
			return nil, err
		}
		var content bytes.Buffer
		hasDevelopment, hasContainers := false, false
		for _, group := range s.Groups {
			hasDevelopment = hasDevelopment || group == "development"
			hasContainers = hasContainers || group == "containers"
		}
		if err := shellTemplate.Execute(&content, struct {
			Shell      string
			KindPodman bool
		}{shell, hasDevelopment && hasContainers}); err != nil {
			return nil, err
		}
		quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
		line := "[ ! -r " + quote(script) + " ] || . " + quote(script)
		present := false
		for _, existing := range strings.Split(string(previous), "\n") {
			if strings.TrimSpace(existing) == line {
				present = true
			}
		}
		rcData := append([]byte(nil), previous...)
		if !present {
			if len(rcData) > 0 && rcData[len(rcData)-1] != '\n' {
				rcData = append(rcData, '\n')
			}
			rcData = append(rcData, []byte("\n# myarch-buildkit shell tools\n"+line+"\n")...)
		}
		changes = append(changes, FileChange{Path: script, Data: content.Bytes(), Mode: 0600}, FileChange{Path: rc, Data: rcData, Mode: 0600})
	}
	for i := range changes {
		changes[i].Original = originals[changes[i].Path]
		changes[i].Existed = existed[changes[i].Path]
		changes[i].CheckOriginal = true
	}
	terminalChanges, err := BuildTerminalProfile(home, s)
	if err != nil {
		return nil, err
	}
	changes = append(changes, terminalChanges...)
	containerChanges, err := BuildContainerProfile(home, s)
	if err != nil {
		return nil, err
	}
	return AddPluginProfile(home, s, append(changes, containerChanges...))
}

// ValidateProfile checks the complete proposed Niri configuration in a private
// temporary file. No live configuration is written during validation.
func ValidateProfile(changes []FileChange, runner Runner) error {
	var config *FileChange
	for i := range changes {
		if filepath.Ext(changes[i].Path) == ".kdl" {
			if config != nil {
				return fmt.Errorf("profile contains multiple Niri configs")
			}
			config = &changes[i]
		}
	}
	if config == nil {
		return fmt.Errorf("profile lacks its Niri config")
	}
	directory, err := os.MkdirTemp("", "myarch-profile-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	candidate := filepath.Join(directory, "config.kdl")
	if err = os.WriteFile(candidate, config.Data, 0600); err != nil {
		return err
	}
	output, err := runner.Output("niri", "validate", "--config", candidate)
	if err != nil {
		return fmt.Errorf("Niri configuration validation failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// Both releases retain the dms command but use different configuration roots.
func DMSConfigHome(home string, s Settings) string {
	directory := s.DMSConfigDir
	if directory == "" {
		directory = "dms"
	}
	return filepath.Join(home, ".config", directory)
}
