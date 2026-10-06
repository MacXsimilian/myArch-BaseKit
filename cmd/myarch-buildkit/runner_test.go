package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type replyRunner struct {
	reply CommandResult
	calls []Command
}

func (r *replyRunner) Execute(c Command) (CommandResult, error) {
	r.calls = append(r.calls, c)
	return r.reply, nil
}

func TestRunningHyprlandErrorsRequireValidEmptyList(t *testing.T) {
	for _, response := range []string{`[]`, `[""]`, `["invalid bind"]`, `null`, `{}`, `invalid`} {
		r := &replyRunner{reply: CommandResult{Stdout: response}}
		c := &Context{Runner: r, Report: map[string]any{}}
		err := checkHyprlandErrors(c)
		if (err == nil) != (response == `[]` || response == `[""]`) {
			t.Fatalf("%s: %v", response, err)
		}
	}
}

func TestDisabledContainersSkipOptionalChecksButKeepCorePodman(t *testing.T) {
	c, _ := storageContext(t)
	c.Settings = DefaultSettings()
	c.Settings.PackageGroups["containers"] = false
	c.CheckOnly = true
	// Command discovery must not depend on the developer's installed tools.
	bin := t.TempDir()
	for _, name := range []string{"podman", "podman-compose", "getsubids"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	r := &replyRunner{reply: CommandResult{Code: 1}}
	c.Runner = r
	// Missing unrelated packages are expected in this isolated environment.
	_ = checkAll(c)
	if _, checked := object(c.Report["check_results"])["containers"]; checked {
		t.Fatal("disabled container checks ran")
	}
	podmanVersions := 0
	for _, call := range r.calls {
		if strings.Contains(strings.Join(call.Args, " "), "getsubids") || call.Args[0] == "podman-compose" {
			t.Fatalf("disabled optional container tool was probed: %v", call.Args)
		}
		if call.Args[0] == "podman" {
			if len(call.Args) != 2 || call.Args[1] != "--version" {
				t.Fatalf("unexpected Podman operation: %v", call.Args)
			}
			podmanVersions++
		}
	}
	if podmanVersions != 1 {
		t.Fatalf("Core Podman backend must be checked once, got %d checks", podmanVersions)
	}
}

func TestRunnerPreservesArgumentsInputAndEnvironment(t *testing.T) {
	result, err := (ExecRunner{}).Execute(Command{Args: []string{"sh", "-c", `printf '%s:%s:' "$1" "$MYARCH_BUILDKIT_RUNNER_TEST"; cat`, "sh", "literal $(danger)"}, Env: map[string]string{"MYARCH_BUILDKIT_RUNNER_TEST": "custom value"}, Input: []byte("input")})
	if err != nil || result.Code != 0 || result.Stdout != "literal $(danger):custom value:input" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestRunnerDistinguishesExitAndInvocationFailure(t *testing.T) {
	result, err := (ExecRunner{}).Execute(Command{Args: []string{"sh", "-c", "exit 7"}})
	if err != nil || result.Code != 7 {
		t.Fatalf("%+v %v", result, err)
	}
	result, err = (ExecRunner{}).Execute(Command{Args: []string{filepath.Join(t.TempDir(), "missing")}})
	if err == nil || result.Code != 127 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestCancellationPreventsWritesAndCommands(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Context{Runner: ExecRunner{Base: base}, Report: map[string]any{}}
	path := filepath.Join(t.TempDir(), "must-not-exist")
	if err := c.Write(path, []byte("configuration"), 0600); !errors.Is(err, context.Canceled) {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("write happened: %v", err)
	}
	if result, err := c.Run("sh", "-c", "exit 0"); !errors.Is(err, context.Canceled) || result.Code != 130 {
		t.Fatalf("command: %+v %v", result, err)
	}
}

func TestDetachedLaunchFailsForMissingExecutable(t *testing.T) {
	result, err := (ExecRunner{}).Execute(Command{Args: []string{filepath.Join(t.TempDir(), "missing")}, Detached: true})
	if err == nil || result.Code != 127 {
		t.Fatalf("%+v %v", result, err)
	}
}
