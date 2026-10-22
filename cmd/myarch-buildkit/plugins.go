package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Plugin struct{ ID, Name string }

var RequestedPlugins = []Plugin{
	{"dockerManager", "Docker Manager"}, {"kubernetes", "Kubernetes"},
	{"emojiLauncher", "Emoji & Unicode Launcher"}, {"bongoCat", "Bongo Cat"}, {"clipboardPlus", "Clipboard+"},
}

func ValidateDMSVersion(s Settings, r Runner) error {
	output, err := r.Output("dms", "version")
	if err != nil {
		return fmt.Errorf("read DMS version: %w", err)
	}
	match := regexp.MustCompile(`(?m)^dms(?: \(git\))? v?([12])\.[0-9]+\.[0-9]+(?:[-+][^\s]+)?\s*$`).FindStringSubmatch(strings.TrimSpace(string(output)))
	if len(match) == 0 {
		return fmt.Errorf("unsupported DMS version %q; use a released DMS 1.x or Dux 2.x build", strings.TrimSpace(string(output)))
	}
	expected := "dms"
	if match[1] == "1" {
		expected = "DankMaterialShell"
	}
	if s.DMSConfigDir != expected {
		return fmt.Errorf("installed %s requires dms_config_dir %q; update your settings before applying", strings.TrimSpace(string(output)), expected)
	}
	return nil
}

// Read manifests locally: the upstream list command refreshes its online registry.
// The CLI uses symlinks into plugins/.repos for multi-plugin repositories.
func installedPlugins(roots []string) (map[string]bool, error) {
	found := map[string]bool{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") || strings.HasSuffix(entry.Name(), ".meta") {
				continue
			}
			path := filepath.Join(root, entry.Name())
			requested := false
			for _, p := range RequestedPlugins {
				if p.ID == entry.Name() {
					requested = true
				}
			}
			if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				if requested {
					return nil, fmt.Errorf("plugin destination is not a directory: %s", path)
				}
				continue
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil, fmt.Errorf("plugin directory %s: %w", path, err)
			}
			rel, err := filepath.Rel(root, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("plugin symlink escapes plugin directory: %s", path)
			}
			manifest := filepath.Join(path, "plugin.json")
			info, err := os.Lstat(manifest)
			if err != nil {
				if os.IsNotExist(err) && !requested {
					continue
				}
				return nil, fmt.Errorf("plugin manifest %s: %w", manifest, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("plugin manifest is not a regular file: %s", manifest)
			}
			data, err := os.ReadFile(manifest)
			if err != nil {
				return nil, err
			}
			var value struct {
				ID        string `json:"id"`
				Component string `json:"component"`
			}
			if err := json.Unmarshal(data, &value); err != nil || value.ID == "" {
				return nil, fmt.Errorf("invalid plugin manifest: %s", manifest)
			}
			if requested && value.ID != entry.Name() {
				return nil, fmt.Errorf("plugin ID mismatch at %s; existing files preserved", path)
			}

			for _, plugin := range RequestedPlugins {
				if value.ID != plugin.ID {
					continue
				}
				component := filepath.Join(resolved, value.Component)
				target, err := filepath.EvalSymlinks(component)
				rel, relErr := filepath.Rel(resolved, target)
				if value.Component == "" || filepath.IsAbs(value.Component) || err != nil || relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return nil, fmt.Errorf("missing or unsafe plugin component in %s; repair this installation manually", manifest)
				}
				info, err := os.Stat(target)
				if err != nil || !info.Mode().IsRegular() {
					return nil, fmt.Errorf("plugin component is not a regular file: %s", component)
				}
			}
			found[value.ID] = true
		}
	}
	return found, nil
}
func pluginRoots(home string, s Settings) []string {
	return []string{filepath.Join(DMSConfigHome(home, s), "plugins"), "/etc/xdg/quickshell/dms-plugins"}
}
func CheckPlugins(home string, s Settings) error {
	found, err := installedPlugins(pluginRoots(home, s))
	if err != nil {
		return err
	}
	var missing []string
	for _, p := range RequestedPlugins {
		if !found[p.ID] {
			missing = append(missing, p.ID)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing plugins: %s", strings.Join(missing, ", "))
	}
	return nil
}
func InstallPlugins(home string, s Settings, r Runner, out io.Writer) error {
	if err := ValidateDMSVersion(s, r); err != nil {
		return err
	}
	roots := pluginRoots(home, s)
	for _, path := range []string{roots[0], filepath.Join(roots[0], ".repos"), filepath.Join(DMSConfigHome(home, s), "plugins.lock.json")} {
		if err := safeHomePath(home, path); err != nil {
			return err
		}
	}
	found, err := installedPlugins(roots)
	if err != nil {
		return err
	}
	for _, p := range RequestedPlugins {
		if found[p.ID] {
			fmt.Fprintf(out, "Plugin already installed: %s\n", p.Name)
			continue
		}
		if err := r.Run("dms", "plugins", "install", p.ID); err != nil {
			return fmt.Errorf("install plugin %s: %w", p.ID, err)
		}
		found, err = installedPlugins(roots)
		if err != nil {
			return err
		}
		if !found[p.ID] {
			return fmt.Errorf("plugin installer did not create %s under %s", p.ID, roots[0])
		}
		fmt.Fprintf(out, "Installed plugin: %s\n", p.Name)
	}
	return nil
}
