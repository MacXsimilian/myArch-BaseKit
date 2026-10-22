package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Plugin struct {
	ID     string
	Repo   string
	Subdir string
}

var RequestedPlugins = []Plugin{
	{ID: "dockerManager", Repo: "LuckShiba/DmsDockerManager", Subdir: ""},
	{ID: "kubernetes", Repo: "psyreactor/dms-kubernetes", Subdir: ""},
	{ID: "emojiLauncher", Repo: "devnullvoid/dms-emoji-launcher", Subdir: ""},
	{ID: "bongoCat", Repo: "hthienloc/dms-plugins", Subdir: "bongoCat"},
	{ID: "clipboardPlus", Repo: "Dadangdut33/dms-plugins", Subdir: "ClipboardPlus"},
}

// Fetch the current default branch and retain the resolved commit.
// Checkout data is never read from an existing (possibly edited) working tree.
func FetchPlugin(c *Context, p Plugin) (string, string, error) {
	destination := filepath.Join(c.RunDir, "plugin-downloads", p.ID)
	if err := securePath(destination, true); err != nil {
		return "", "", err
	}
	for _, args := range [][]string{
		{"git", "init", destination},
		{"git", "-C", destination, "fetch", "--depth", "1", "https://github.com/" + p.Repo, "HEAD"},
		{"git", "-C", destination, "checkout", "--detach", "--force", "FETCH_HEAD"},
		{"git", "-C", destination, "clean", "-fdx"},
	} {
		if _, err := c.Run(args...); err != nil {
			return "", "", err
		}
	}
	revision, err := c.Run("git", "-C", destination, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	commit := strings.TrimSpace(revision.Stdout)
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(commit) {
		return "", "", fmt.Errorf("invalid upstream plugin revision: %s", p.ID)
	}
	return filepath.Join(destination, p.Subdir), commit, nil
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

type pluginSnapshot struct {
	Destination string            `json:"destination"`
	Files       map[string]string `json:"files"`
}

func pluginSnapshotPath(c *Context, id string) string {
	return filepath.Join(stateRoot(c), "plugin-snapshots", id+".json")
}

// Only previously managed files can be removed. Unknown files may be plugin
// user data; a first upgrade must not assume every file is disposable code.
func stageObsoletePluginFiles(c *Context, plugin Plugin, destination string, files map[string]string) error {
	path := pluginSnapshotPath(c, plugin.ID)
	previous := pluginSnapshot{}
	if exists(path) {
		if err := ReadJSON(path, &previous); err != nil {
			return err
		}
	}
	if previous.Destination == destination {
		for relative, expected := range previous.Files {
			target := filepath.Join(destination, relative)
			if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == "." || !inside(destination, target) || strings.Split(relative, string(os.PathSeparator))[0] == ".git" {
				return fmt.Errorf("unsafe plugin snapshot path: %s", relative)
			}
			if _, present := files[relative]; present {
				continue
			}
			actual, err := FileHash(target)
			if err != nil {
				return err
			}
			if actual == "" {
				continue
			}
			if actual != expected {
				c.Warn("Preserved locally changed obsolete plugin file: " + target)
				continue
			}
			if err := c.stageDelete(target); err != nil {
				return err
			}
		}
	}
	data, err := jsonBytes(pluginSnapshot{Destination: destination, Files: files})
	if err != nil {
		return err
	}
	return c.Write(path, data, 0600)
}

// installedPluginDirectories prefers user copies over system-wide plugins.
func installedPluginDirectories(root string) (map[string]string, error) {
	existing := map[string]string{}
	for _, directory := range []string{root, "/etc/xdg/quickshell/dms-plugins"} {
		entries, err := os.ReadDir(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name(), "plugin.json")
			if !exists(path) {
				continue
			}
			raw, e := os.ReadFile(path)
			if e != nil {
				return nil, e
			}
			var manifest map[string]any
			if e = json.Unmarshal(raw, &manifest); e != nil {
				return nil, e
			}
			id, _ := manifest["id"].(string)
			if _, ok := existing[id]; !ok {
				existing[id] = filepath.Dir(path)
			}
		}
	}
	return existing, nil
}

// stagePluginSnapshot copies regular files and records hashes for future cleanup.
// Git metadata is excluded, and symlinks never enter the pending payload.
func stagePluginSnapshot(c *Context, source, destination string) (map[string]string, error) {
	newFiles := map[string]string{}
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
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
		newFiles[relative] = hashBytes(raw)
		return c.Write(filepath.Join(destination, relative), raw, info.Mode().Perm()&0755)
	})
	if err != nil {
		return nil, err
	}
	return newFiles, nil
}

// configurePluginPreferences retains personal values and stages only our overrides.
func configurePluginPreferences(c *Context) error {
	path := filepath.Join(c.ConfigHome, dmsDirectory(c), "plugin_settings.json")
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
	return nil
}

func ConfigurePlugins(c *Context) error {
	if c.CheckOnly || c.Pending == nil {
		return fmt.Errorf("plugin updates require staging")
	}
	root := filepath.Join(c.ConfigHome, dmsDirectory(c), "plugins")
	existing, err := installedPluginDirectories(root)
	if err != nil {
		return err
	}
	records := []map[string]any{}
	for _, plugin := range RequestedPlugins {
		destination := filepath.Join(root, plugin.ID)
		installed, updating := existing[plugin.ID]
		if updating {
			if _, err := PluginManifest(installed, plugin.ID); err != nil {
				return err
			}
			// User copies override system plugins; never modify package-owned files.
			if inside(root, installed) {
				destination = installed
			}
		}
		if !updating || destination != installed {
			if _, err := os.Lstat(destination); err == nil {
				return fmt.Errorf("unrecognized plugin directory preserved: %s", destination)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		source, commit, err := FetchPlugin(c, plugin)
		if err != nil {
			return err
		}
		manifest, err := PluginManifest(source, plugin.ID)
		if err != nil {
			return err
		}
		newFiles, err := stagePluginSnapshot(c, source, destination)
		if err != nil {
			return err
		}
		if err := stageObsoletePluginFiles(c, plugin, destination, newFiles); err != nil {
			return err
		}
		records = append(records, map[string]any{
			"id":          plugin.ID,
			"status":      "staged latest snapshot",
			"repo":        "https://github.com/" + plugin.Repo,
			"ref":         "HEAD",
			"commit":      commit,
			"version":     manifest["version"],
			"updated":     updating,
			"destination": destination,
		})
	}
	if err := configurePluginPreferences(c); err != nil {
		return err
	}
	c.Report["plugins"] = records
	c.Note("Five DMS plugins are staged for next login; latest upstream plugin code will replace existing copies with backups; preferences are preserved.")
	c.Note("Bongo Cat needs input-group membership to react to typing. See the guide for its explicit permission step.")
	return nil
}
