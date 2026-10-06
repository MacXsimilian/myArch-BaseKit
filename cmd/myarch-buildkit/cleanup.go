package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
)

var hiddenUtilityPackages = []string{"v4l-utils", "avahi", "hwloc", "qt6ct", "nwg-look", "lxappearance"}
var duplicateEditorPackages = []string{"gnome-text-editor", "gedit", "mousepad", "xed", "kate", "kwrite", "leafpad", "pluma"}

func ConfigureCleanup(c *Context) error {
	if c.Options.ConfigureOnly {
		return nil
	}
	if c.CheckOnly || c.Options.Check || c.Options.DryRun {
		return errors.New("cleanup is unavailable in read-only check mode")
	}
	for _, name := range c.Options.RemoveNotes {
		if !ValidNotesPackage(name) {
			return fmt.Errorf("not an approved dedicated notes package: %s", name)
		}
	}
	pending, err := PendingItems(c)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return errors.New("cleanup blocked: configuration is pending for the next myarch-buildkit login")
	}
	if err := CheckDefaults(c); err != nil {
		return fmt.Errorf("cleanup blocked: application defaults have not passed verification: %w", err)
	}
	if err := CheckDesktop(c); err != nil {
		return fmt.Errorf("cleanup blocked: running desktop has not passed verification: %w", err)
	}
	if err := checkCleanupHyprland(c); err != nil {
		return err
	}
	resolver := newPackageResolver(c)
	removals := []string{}
	if hasCommand("nautilus") && resolver.isInstalled("nautilus") && resolver.isInstalled("dolphin") {
		removals = append(removals, "dolphin")
	}
	codeInstalled := false
	for _, name := range []string{"visual-studio-code-bin"} {
		if resolver.isInstalled(name) {
			codeInstalled = true
		}
	}
	if hasCommand("code") && codeInstalled {
		for _, name := range duplicateEditorPackages {
			if resolver.isInstalled(name) {
				removals = append(removals, name)
			}
		}
	}
	if !c.Options.KeepTerminal && hasCommand("ghostty") && resolver.isInstalled("ghostty") && resolver.isInstalled("alacritty") {
		removals = append(removals, "alacritty")
	}
	for _, name := range c.Options.RemoveNotes {
		if resolver.isInstalled(name) {
			removals = append(removals, name)
		}
	}
	var results strings.Builder
	failures := []error{}
	seen := map[string]bool{}
	for _, name := range removals {
		if seen[name] {
			continue
		}
		seen[name] = true
		// Keep normal dependency checks and review prompts. Never cascade or recurse.
		_, err := c.Command(Command{Args: []string{"sudo", "pacman", "-R", "--", name}, Interactive: true})
		status := "removed"
		if err != nil {
			status = "retained"
			c.Warn(name + " retained: removal failed or was cancelled")
			failures = append(failures, err)
		}
		fmt.Fprintf(&results, "%s\t%s\n", name, status)
	}
	c.Report["package_cleanup"] = results.String()
	if c.RunDir != "" {
		if err := c.Save(); err != nil {
			return err
		}
	}
	sources := map[string]bool{}
	for _, name := range hiddenUtilityPackages {
		if !resolver.isInstalled(name) {
			continue
		}
		files, err := c.Run("pacman", "-Qlq", "--", name)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		for _, path := range strings.Split(files.Stdout, "\n") {
			path = strings.TrimSpace(path)
			if filepath.Dir(path) == "/usr/share/applications" && filepath.Ext(path) == ".desktop" {
				sources[path] = true
			}
		}
	}
	sorted := []string{}
	for path := range sources {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)
	if err := hideLauncherSources(c, sorted); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func hiddenEntry(text string) (string, error) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "[Desktop Entry]" {
			start = i
			break
		}
	}
	if start < 0 {
		return "", errors.New("missing Desktop Entry section")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), "[") {
			end = i
			break
		}
	}
	if !strings.HasSuffix(lines[start], "\n") {
		lines[start] += "\n"
	}
	section := []string{}
	for _, line := range lines[start+1 : end] {
		if !strings.HasPrefix(strings.TrimLeft(line, " \t"), "NoDisplay=") {
			section = append(section, line)
		}
	}
	if len(section) > 0 && !strings.HasSuffix(section[len(section)-1], "\n") {
		section[len(section)-1] += "\n"
	}
	output := append([]string{}, lines[:start+1]...)
	output = append(output, section...)
	output = append(output, "NoDisplay=true\n")
	output = append(output, lines[end:]...)
	return strings.Join(output, ""), nil
}

func ordinaryOwnedOverride(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("existing override is a symlink or non-file")
	}
	if owner, ok := info.Sys().(*syscall.Stat_t); !ok || int(owner.Uid) != os.Getuid() {
		return errors.New("existing override belongs to another user")
	}
	return nil
}

func hideLauncherSources(c *Context, sources []string) error {
	hidden := []string{}
	failures := []error{}
	targetDir := filepath.Join(c.DataHome, "applications")
	for _, source := range sources {
		target := filepath.Join(targetDir, filepath.Base(source))
		if err := ordinaryOwnedOverride(target); err != nil {
			c.Warn("Launcher preserved: " + target + ": " + err.Error())
			failures = append(failures, err)
			continue
		}
		bytes, err := os.ReadFile(target)
		if os.IsNotExist(err) {
			bytes, err = os.ReadFile(source)
		}
		if err != nil {
			c.Warn("Launcher preserved: " + target + ": " + err.Error())
			failures = append(failures, err)
			continue
		}
		if !utf8.Valid(bytes) {
			err = errors.New("launcher is not valid UTF-8")
			c.Warn("Launcher preserved: " + target + ": " + err.Error())
			failures = append(failures, err)
			continue
		}
		text, err := hiddenEntry(string(bytes))
		if err == nil {
			err = c.Write(target, []byte(text), 0644)
		}
		if err != nil {
			c.Warn("Launcher preserved: " + target + ": " + err.Error())
			failures = append(failures, err)
			continue
		}
		hidden = append(hidden, filepath.Base(source))
	}
	c.Report["hidden_launchers"] = hidden
	return errors.Join(failures...)
}

func checkCleanupHyprland(c *Context) error {
	result, err := c.Run("hyprctl", "-j", "configerrors")
	if err != nil {
		return fmt.Errorf("cleanup blocked: cannot verify running Hyprland configuration: %w", err)
	}
	var reported []string
	if err = json.Unmarshal([]byte(result.Stdout), &reported); err != nil || reported == nil {
		return errors.New("cleanup blocked: unrecognized Hyprland configuration-error response")
	}
	c.Report["hyprland_configerrors"] = reported
	for _, message := range reported {
		if strings.TrimSpace(message) != "" {
			return fmt.Errorf("cleanup blocked: Hyprland configuration errors: %s", strings.Join(reported, "\n"))
		}
	}
	return nil
}
