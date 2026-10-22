package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerProfilePreservesPersonalConfig(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config/containers/containers.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("[engine]\nevents_logger = \"file\"\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s := Settings{Groups: []string{"containers", "development"}}
	changes, err := BuildContainerProfile(home, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatal(changes)
	}
	if _, err := ApplyFiles(home, changes); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(original) {
		t.Fatal("personal TOML overwritten")
	}
	for _, c := range changes {
		if !c.CheckOriginal {
			t.Fatal("missing snapshot")
		}
	}
}
func TestContainerProfileSelectionAndSymlinks(t *testing.T) {
	home := t.TempDir()
	changes, err := BuildContainerProfile(home, Settings{Groups: []string{"development"}})
	if err != nil || len(changes) != 0 {
		t.Fatal(changes, err)
	}
	path := filepath.Join(home, ".config/containers")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildContainerProfile(home, Settings{Groups: []string{"containers"}}); err == nil {
		t.Fatal("symlink accepted")
	}
}

type containerRunner struct {
	rootless, fail bool
	mappings       string
	calls          int
}

func (r *containerRunner) Run(args ...string) error { return errors.New("mutation forbidden") }
func (r *containerRunner) Output(args ...string) ([]byte, error) {
	r.calls++
	if r.fail {
		return nil, errors.New("unavailable")
	}
	switch strings.Join(args, " ") {
	case "podman info --format json":
		if r.rootless {
			mappings := r.mappings
			if mappings == "" {
				mappings = `{"uidmap":[{"size":1},{"size":65536}],"gidmap":[{"size":1},{"size":65536}]}`
			}
			return []byte(`{"host":{"security":{"rootless":true},"cgroupVersion":"v2","idMappings":` + mappings + `}}`), nil
		}
		return []byte(`{"host":{"security":{"rootless":false}}}`), nil
	case "podman compose version":
		return []byte("podman-compose version 1.5.0"), nil
	case "kind version":
		return []byte("kind v0.30.0"), nil
	default:
		return nil, errors.New("unexpected command")
	}
}
func TestContainerChecks(t *testing.T) {
	t.Setenv("CONTAINERS_CONF", "")
	t.Setenv("CONTAINERS_CONF_OVERRIDE", "")
	t.Setenv("KIND_EXPERIMENTAL_PROVIDER", "podman")
	s := Settings{Groups: []string{"containers", "development"}}
	if err := CheckContainers(s, &containerRunner{rootless: true}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*containerRunner{{}, {fail: true}} {
		if err := CheckContainers(s, r); err == nil {
			t.Fatal("bad Podman accepted")
		}
		if r.calls != 1 {
			t.Fatal("continued after failure")
		}
	}
	t.Setenv("KIND_EXPERIMENTAL_PROVIDER", "docker")
	if err := CheckContainers(s, &containerRunner{rootless: true}); err == nil {
		t.Fatal("wrong kind provider accepted")
	}
}

func TestContainerMissingSubordinateMappings(t *testing.T) {
	t.Setenv("CONTAINERS_CONF", "")
	t.Setenv("CONTAINERS_CONF_OVERRIDE", "")
	for _, mappings := range []string{`{}`, `{"uidmap":[{"size":1}],"gidmap":[{"size":65536}]}`, `{"uidmap":[{"size":65536}],"gidmap":[{"size":1}]}`} {
		r := &containerRunner{rootless: true, mappings: mappings}
		err := CheckContainers(Settings{Groups: []string{"containers"}}, r)
		if err == nil || !strings.Contains(err.Error(), "/etc/subuid") {
			t.Fatalf("missing mapping error: %v", err)
		}
		if r.calls != 1 {
			t.Fatal("continued after insufficient mappings")
		}
	}
}
