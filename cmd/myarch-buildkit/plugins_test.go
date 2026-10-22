package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type pluginRunner struct {
	root, version    string
	calls            int
	fail, noManifest bool
}

func (r *pluginRunner) Output(args ...string) ([]byte, error) {
	if !reflect.DeepEqual(args, []string{"dms", "version"}) {
		return nil, errors.New("unexpected output command")
	}
	return []byte(r.version), nil
}
func (r *pluginRunner) Run(args ...string) error {
	if len(args) != 4 || !reflect.DeepEqual(args[:3], []string{"dms", "plugins", "install"}) {
		return errors.New("unexpected install command")
	}
	r.calls++
	if r.fail {
		return errors.New("offline")
	}
	if r.noManifest {
		return nil
	}
	path := filepath.Join(r.root, args[3])
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(path, "Main.qml"), []byte("Item {}"), 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(path, "plugin.json"), []byte(`{"id":"`+args[3]+`","component":"Main.qml"}`), 0600)
}
func TestPluginsInstallOnce(t *testing.T) {
	home := t.TempDir()
	s := Settings{DMSConfigDir: "dms"}
	r := &pluginRunner{root: filepath.Join(DMSConfigHome(home, s), "plugins"), version: "dms v2.0.0"}
	for i := 0; i < 2; i++ {
		if err := InstallPlugins(home, s, r, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if r.calls != len(RequestedPlugins) {
		t.Fatalf("ran %d installs", r.calls)
	}
	if err := CheckPlugins(home, s); err != nil {
		t.Fatal(err)
	}
}
func TestPluginFailuresStop(t *testing.T) {
	for _, noManifest := range []bool{false, true} {
		home := t.TempDir()
		s := Settings{DMSConfigDir: "dms"}
		r := &pluginRunner{root: filepath.Join(DMSConfigHome(home, s), "plugins"), version: "dms v2.0.0", fail: !noManifest, noManifest: noManifest}
		if err := InstallPlugins(home, s, r, io.Discard); err == nil {
			t.Fatal("failure accepted")
		}
		if r.calls != 1 {
			t.Fatal("continued after failure")
		}
	}
}
func TestPluginMalformedDestinationPreserved(t *testing.T) {
	home := t.TempDir()
	s := Settings{DMSConfigDir: "dms"}
	root := filepath.Join(DMSConfigHome(home, s), "plugins")
	path := filepath.Join(root, "dockerManager")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(path, "plugin.json")
	if err := os.WriteFile(manifest, []byte(`{"id":"different"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := &pluginRunner{root: root, version: "dms v2.0.0"}
	if err := InstallPlugins(home, s, r, io.Discard); err == nil {
		t.Fatal("mismatch accepted")
	}
	if r.calls != 0 {
		t.Fatal("modified mismatched destination")
	}
	data, _ := os.ReadFile(manifest)
	if string(data) != `{"id":"different"}` {
		t.Fatal("existing content changed")
	}
}
func TestPluginMonorepoLink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".repos", "bundle", "bongoCat")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "plugin.json"), []byte(`{"id":"bongoCat","component":"Main.qml"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "Main.qml"), []byte("Item {}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(root, "bongoCat")); err != nil {
		t.Fatal(err)
	}
	found, err := installedPlugins([]string{root})
	if err != nil || !found["bongoCat"] {
		t.Fatalf("%v %v", found, err)
	}
}
func TestDMSVersionConfigMatch(t *testing.T) {
	for _, test := range []struct {
		version, dir string
		valid        bool
	}{
		{"dms v1.6.3", "DankMaterialShell", true}, {"dms (git) v2.0.0-123", "dms", true},
		{"dms v1.6.3", "dms", false}, {"dms v2.0.0", "DankMaterialShell", false}, {"dms dev", "dms", false},
	} {
		if err := ValidateDMSVersion(Settings{DMSConfigDir: test.dir}, &pluginRunner{version: test.version}); (err == nil) != test.valid {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}

func TestPluginUnsafeSupportPaths(t *testing.T) {
	for _, name := range []string{"plugins", "plugins/.repos", "plugins.lock.json"} {
		home := t.TempDir()
		s := Settings{DMSConfigDir: "dms"}
		root := DMSConfigHome(home, s)
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), path); err != nil {
			t.Fatal(err)
		}
		r := &pluginRunner{version: "dms v2.0.0"}
		if err := InstallPlugins(home, s, r, io.Discard); err == nil {
			t.Fatalf("accepted redirected %s", name)
		}
		if r.calls != 0 {
			t.Fatal("installer ran")
		}
	}
}
func TestPluginIncompleteInstall(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dockerManager")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "plugin.json"), []byte(`{"id":"dockerManager","component":"Main.qml"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := installedPlugins([]string{root}); err == nil {
		t.Fatal("missing component accepted")
	}
}
