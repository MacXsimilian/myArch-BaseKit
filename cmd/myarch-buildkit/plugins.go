package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Plugin struct{ ID, Repo, Commit, Subdir string }

var RequestedPlugins = []Plugin{
	{"dockerManager", "LuckShiba/DmsDockerManager", "f6f7d94c84510da098980a2bb733137e90f98fd8", ""},
	{"kubernetes", "psyreactor/dms-kubernetes", "0c39d9a28bb2ea592f7a306021dffaabbf2723ca", ""},
	{"emojiLauncher", "devnullvoid/dms-emoji-launcher", "8ff394e3ddfcb2fd755ed2e7b4c6f01f3e26e596", ""},
	{"bongoCat", "hthienloc/dms-plugins", "30841c817c152bda1c969e0b91d034ccc9a75b86", "bongoCat"},
	{"clipboardPlus", "Dadangdut33/dms-plugins", "63fe6b87c497f1f7c2ea61432716817db1c5c3a4", "ClipboardPlus"},
}

func FetchPlugin(c *Context, p Plugin) (string, error) {
	destination := filepath.Join(c.RunDir, "plugin-downloads", p.ID)
	if !exists(destination) {
		for _, args := range [][]string{{"git", "init", destination}, {"git", "-C", destination, "fetch", "--depth", "1", "https://github.com/" + p.Repo, p.Commit}, {"git", "-C", destination, "checkout", "--detach", "FETCH_HEAD"}} {
			if _, err := c.Run(args...); err != nil {
				return "", err
			}
		}
	}
	revision, err := c.Run("git", "-C", destination, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(revision.Stdout) != p.Commit {
		return "", fmt.Errorf("unexpected plugin revision: %s", p.ID)
	}
	return filepath.Join(destination, p.Subdir), nil
}
func PluginManifest(root, expectedID string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(root, "plugin.json"))
	if err != nil {
		return nil, err
	}
	var manifest map[string]any
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	if manifest["id"] != expectedID {
		return nil, fmt.Errorf("unexpected plugin ID: %s", expectedID)
	}
	component, ok := manifest["component"].(string)
	if !ok || component == "" {
		return nil, fmt.Errorf("missing/unsafe plugin component: %s", expectedID)
	}
	full := filepath.Join(root, component)
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return nil, fmt.Errorf("missing/unsafe plugin component: %s", expectedID)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil || !inside(resolvedRoot, resolved) || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("missing/unsafe plugin component: %s", expectedID)
	}
	return manifest, nil
}
func ConfigurePlugins(c *Context) error {
	root := filepath.Join(c.ConfigHome, "DankMaterialShell/plugins")
	existing := map[string]string{}
	for _, directory := range []string{root, "/etc/xdg/quickshell/dms-plugins"} {
		entries, err := os.ReadDir(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name(), "plugin.json")
			if !exists(path) {
				continue
			}
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			var manifest map[string]any
			if e = json.Unmarshal(raw, &manifest); e != nil {
				return e
			}
			id, _ := manifest["id"].(string)
			if _, ok := existing[id]; !ok {
				existing[id] = filepath.Dir(path)
			}
		}
	}
	records := []map[string]any{}
	for _, plugin := range RequestedPlugins {
		if installed, ok := existing[plugin.ID]; ok {
			manifest, err := PluginManifest(installed, plugin.ID)
			if err != nil {
				return err
			}
			records = append(records, map[string]any{"id": plugin.ID, "status": "existing installation preserved", "version": manifest["version"]})
			continue
		}
		destination := filepath.Join(root, plugin.ID)
		if _, err := os.Lstat(destination); err == nil {
			return fmt.Errorf("unrecognized plugin directory preserved: %s", destination)
		} else if !os.IsNotExist(err) {
			return err
		}
		source, err := FetchPlugin(c, plugin)
		if err != nil {
			return err
		}
		manifest, err := PluginManifest(source, plugin.ID)
		if err != nil {
			return err
		}
		err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, e := filepath.Rel(source, path)
			if e != nil {
				return e
			}
			for _, part := range strings.Split(relative, string(os.PathSeparator)) {
				if part == ".git" {
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("plugin snapshot contains a symlink: %s", relative)
			}
			if entry.IsDir() {
				return nil
			}
			info, e := entry.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("plugin snapshot contains a nonregular file: %s", relative)
			}
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			return c.Write(filepath.Join(destination, relative), raw, info.Mode().Perm()&0755)
		})
		if err != nil {
			return err
		}
		records = append(records, map[string]any{"id": plugin.ID, "status": "staged snapshot", "repo": "https://github.com/" + plugin.Repo, "commit": plugin.Commit, "version": manifest["version"]})
	}
	path := filepath.Join(c.ConfigHome, "DankMaterialShell/plugin_settings.json")
	settings, err := desktopReadObject(c.View(path), nil)
	if err != nil {
		return err
	}
	updates := map[string]any{}
	for _, plugin := range RequestedPlugins {
		previous := map[string]any{}
		if v, ok := settings[plugin.ID]; ok {
			var valid bool
			previous, valid = v.(map[string]any)
			if !valid {
				return fmt.Errorf("invalid plugin preferences: %s", plugin.ID)
			}
		}
		changes := map[string]any{"enabled": true}
		if plugin.ID == "dockerManager" {
			changes["dockerBinary"] = "podman"
			changes["terminalApp"] = "ghostty"
		}
		if plugin.ID == "clipboardPlus" {
			changes["useDmsClipboard"] = true
		}
		merged := desktopCloneObject(previous)
		for k, v := range changes {
			merged[k] = v
		}
		settings[plugin.ID] = merged
		updates[plugin.ID] = changes
	}
	if err = desktopWriteObject(c, path, settings, 0600); err != nil {
		return err
	}
	if err = c.OverrideJSON(path, updates); err != nil {
		return err
	}
	c.Report["plugins"] = records
	c.Note("Five DMS plugins are staged for next login; existing plugin installations are preserved.")
	c.Note("Bongo Cat needs input-group membership to react to typing. See the guide for its explicit permission step.")
	return nil
}
