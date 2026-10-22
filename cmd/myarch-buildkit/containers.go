package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func containerGroup(s Settings, group string) bool {
	for _, g := range s.Groups {
		if g == group {
			return true
		}
	}
	return false
}

// A dedicated drop-in preserves existing containers.conf and storage settings.
func BuildContainerProfile(home string, s Settings) ([]FileChange, error) {
	if !containerGroup(s, "containers") {
		return nil, nil
	}
	changes := []FileChange{{Path: filepath.Join(home, ".config/containers/containers.conf.d/90-myarch-buildkit.conf"), Data: []byte("# Managed by myarch-buildkit\n[engine]\ncompose_providers = [\"podman-compose\"]\n"), Mode: 0600}}
	if containerGroup(s, "development") {
		changes = append(changes, FileChange{Path: filepath.Join(home, ".config/environment.d/60-myarch-buildkit-containers.conf"), Data: []byte("# Managed by myarch-buildkit\nKIND_EXPERIMENTAL_PROVIDER=podman\n"), Mode: 0600})
	}
	for i := range changes {
		c := &changes[i]
		if err := safeHomePath(home, c.Path); err != nil {
			return nil, err
		}
		_, err := os.Lstat(c.Path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		c.Existed = err == nil
		c.Original, err = profileRead(c.Path)
		if err != nil {
			return nil, err
		}
		c.CheckOriginal = true
	}
	return changes, nil
}

// These probes never create containers or change system configuration.
func CheckContainers(s Settings, r Runner) error {
	if !containerGroup(s, "containers") {
		return nil
	}
	for _, key := range []string{"CONTAINERS_CONF", "CONTAINERS_CONF_OVERRIDE"} {
		if os.Getenv(key) != "" {
			return fmt.Errorf("%s overrides managed Podman settings; unset it before checking", key)
		}
	}
	output, err := r.Output("podman", "info", "--format", "json")
	if err != nil {
		return fmt.Errorf("rootless Podman unavailable: %w; inspect podman info and your subordinate UID/GID mappings", err)
	}
	type idRange struct {
		Size int64 `json:"size"`
	}
	var info struct {
		Host struct {
			IDMappings struct {
				UIDMap []idRange `json:"uidmap"`
				GIDMap []idRange `json:"gidmap"`
			} `json:"idMappings"`
			Security struct {
				Rootless bool `json:"rootless"`
			} `json:"security"`
			CgroupVersion string `json:"cgroupVersion"`
		} `json:"host"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return fmt.Errorf("read Podman info: %w", err)
	}
	if !info.Host.Security.Rootless {
		return fmt.Errorf("Podman must run rootless as your regular user")
	}

	for name, mappings := range map[string][]idRange{"UID": info.Host.IDMappings.UIDMap, "GID": info.Host.IDMappings.GIDMap} {
		var size int64
		for _, mapping := range mappings {
			if mapping.Size > 0 {
				size += mapping.Size
			}
		}
		if size < 65536 {
			return fmt.Errorf("rootless Podman has only %d mapped %ss; arrange at least 65536 subordinate IDs in /etc/subuid and /etc/subgid, then refresh Podman's user namespace before retrying", size, name)
		}
	}
	output, err = r.Output("podman", "compose", "version")
	if err != nil {
		return fmt.Errorf("Podman Compose unavailable: %w", err)
	}
	if !strings.Contains(strings.ToLower(string(output)), "podman-compose") {
		return fmt.Errorf("podman compose is not using podman-compose; inspect PODMAN_COMPOSE_PROVIDER and containers.conf overrides")
	}
	if containerGroup(s, "development") {
		if info.Host.CgroupVersion != "v2" {
			return fmt.Errorf("rootless kind requires cgroup v2; Podman reports %q", info.Host.CgroupVersion)
		}
		if _, err := r.Output("kind", "version"); err != nil {
			return fmt.Errorf("kind unavailable: %w", err)
		}
		if os.Getenv("KIND_EXPERIMENTAL_PROVIDER") != "podman" {
			return fmt.Errorf("kind provider is not podman; open a new terminal after login or export KIND_EXPERIMENTAL_PROVIDER=podman")
		}
	}
	return nil
}
