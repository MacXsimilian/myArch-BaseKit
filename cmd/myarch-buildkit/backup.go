package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type FileChange struct {
	Path          string
	Data          []byte
	Mode          os.FileMode
	Original      []byte
	Existed       bool
	CheckOriginal bool
}
type BackupEntry struct {
	Path    string      `json:"path"`
	Backup  string      `json:"backup,omitempty"`
	Existed bool        `json:"existed"`
	Mode    os.FileMode `json:"mode"`
}

// Only regular files beneath the user's home are managed. Symlinks are left alone.
func safeHomePath(home, path string) error {
	home, err := filepath.Abs(home)
	if err != nil {
		return err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == "." || rel == ".." || len(rel) > 3 && rel[:3] == "../" {
		return fmt.Errorf("path must be beneath home: %s", path)
	}
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink requires manual configuration: %s", p)
		}
		if err == nil && p == path && !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular file: %s", p)
		}
		if p == home {
			break
		}
	}
	return nil
}
func atomicFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".basekit-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

// Save every original before changing any destination. A failed apply leaves
// ordinary backup files and a manifest; recovery never depends on this binary.
func ApplyFiles(home string, changes []FileChange) (string, error) {
	pending := []FileChange{}
	originals := map[string][]byte{}
	entries := []BackupEntry{}
	seen := map[string]bool{}
	for _, c := range changes {
		if err := safeHomePath(home, c.Path); err != nil {
			return "", err
		}
		path := filepath.Clean(c.Path)
		if seen[path] {
			return "", fmt.Errorf("duplicate destination: %s", path)
		}
		seen[path] = true
		old, err := os.ReadFile(path)
		existed := err == nil
		if c.CheckOriginal && (existed != c.Existed || existed && !bytes.Equal(old, c.Original)) {
			return "", fmt.Errorf("file changed since preview: %s; rerun setup", path)
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if existed && bytes.Equal(old, c.Data) {
			continue
		}
		entry := BackupEntry{Path: path, Existed: existed}
		if existed {
			info, e := os.Stat(path)
			if e != nil {
				return "", e
			}
			entry.Mode = info.Mode().Perm()
			entry.Backup = fmt.Sprintf("%03d.original", len(entries))
			originals[path] = old
		}
		entries = append(entries, entry)
		pending = append(pending, c)
	}
	if len(pending) == 0 {
		return "", nil
	}
	base := filepath.Join(home, ".local/state/myarch-buildkit/backups")
	if err := safeHomePath(home, base); err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(base, time.Now().Format("20060102-150405-"))
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Existed {
			if err := atomicFile(filepath.Join(dir, e.Backup), originals[e.Path], 0600); err != nil {
				return dir, err
			}
		}
	}
	manifest, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return dir, err
	}
	if err := atomicFile(filepath.Join(dir, "manifest.json"), append(manifest, '\n'), 0600); err != nil {
		return dir, err
	}
	for _, c := range pending {
		if err := safeHomePath(home, c.Path); err != nil {
			return dir, err
		}
		current, err := os.ReadFile(c.Path)
		old, existed := originals[filepath.Clean(c.Path)]
		if existed && (err != nil || !bytes.Equal(current, old)) || !existed && !os.IsNotExist(err) {
			return dir, fmt.Errorf("file changed during setup: %s; backups: %s", c.Path, dir)
		}
		mode := c.Mode
		if mode == 0 {
			mode = 0600
		}
		if existed {
			for _, e := range entries {
				if e.Path == filepath.Clean(c.Path) {
					mode = e.Mode
				}
			}
		}
		if err := atomicFile(c.Path, c.Data, mode); err != nil {
			return dir, fmt.Errorf("apply %s: %w; backups: %s", c.Path, err, dir)
		}
	}
	return dir, nil
}
