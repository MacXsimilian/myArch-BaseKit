package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnerTimeoutStopsDescendantWrites(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "late-write")
	started := time.Now()
	result, err := (ExecRunner{}).Execute(Command{
		Args:    []string{"sh", "-c", `(sleep 0.3; printf changed > "$1") & wait`, "sh", marker},
		Timeout: 40 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) || result.Code != 130 {
		t.Fatalf("timeout result: %+v %v", result, err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("descendant held the command open beyond its deadline")
	}
	time.Sleep(350 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("descendant wrote after cancellation: %v", err)
	}
}

func TestRunnerContextCancellationStopsDescendants(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	marker := filepath.Join(filepath.Dir(ready), "late-write")
	done := make(chan error, 1)
	go func() {
		_, err := (ExecRunner{Base: base}).Execute(Command{
			Args: []string{"sh", "-c", `printf ready > "$1"; (sleep 0.3; printf changed > "$2") & wait`, "sh", ready, marker},
		})
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !exists(ready) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !exists(ready) {
		cancel()
		<-done
		t.Fatal("child never started")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	time.Sleep(350 * time.Millisecond)
	if exists(marker) {
		t.Fatal("descendant survived base-context cancellation")
	}
}

func TestRunnerBoundsPipesAfterParentExit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "late-write")
	started := time.Now()
	result, err := (ExecRunner{}).Execute(Command{Args: []string{"sh", "-c", `(sleep 1.5; printf changed > "$1") &`, "sh", marker}})
	if !errors.Is(err, exec.ErrWaitDelay) || result.Code != 124 {
		t.Fatalf("inherited-pipe result: %+v %v", result, err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("pipe wait was not bounded")
	}
	time.Sleep(600 * time.Millisecond)
	if exists(marker) {
		t.Fatal("orphaned writer survived bounded pipe cleanup")
	}
}

func TestRunnerFailedParentDoesNotLeaveInheritedPipeWriter(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "late-write")
	result, err := (ExecRunner{}).Execute(Command{
		Args: []string{"sh", "-c", `(sleep 1.5; printf changed > "$1") & exit 7`, "sh", marker},
	})
	// WaitDelay also expires here, but os/exec prioritizes the parent's exit
	// status over ErrWaitDelay. Cleanup must not depend on that sentinel alone.
	if err != nil || result.Code != 7 {
		t.Fatalf("failed-parent result: %+v %v", result, err)
	}
	time.Sleep(600 * time.Millisecond)
	if exists(marker) {
		t.Fatal("failed parent's descendant survived inherited-pipe cleanup")
	}
}

func TestRunnerDetachedCommandSurvivesCallerCancellation(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := filepath.Join(t.TempDir(), "detached")
	_, err := (ExecRunner{Base: base}).Execute(Command{
		Args: []string{"sh", "-c", `sleep 0.05; printf detached > "$1"`, "sh", marker}, Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for !exists(marker) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !exists(marker) {
		t.Fatal("intentional detached command was cancelled")
	}
}

func TestRunnerInteractivePreservesForegroundTerminal(t *testing.T) {
	if os.Getenv("MYARCH_RUNNER_PTY_CHILD") == "1" {
		result, err := (ExecRunner{}).Execute(Command{Args: []string{"sh", "-c", `test -t 0 && stty -a >/dev/null && printf terminal-ok`}, Interactive: true})
		if err != nil || result.Code != 0 {
			t.Fatalf("interactive: %+v %v", result, err)
		}
		return
	}
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("util-linux script is required for the terminal regression")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(base, script, "-q", "-e", "-c", shellJoin([]string{binary, "-test.run=^TestRunnerInteractivePreservesForegroundTerminal$"}), "/dev/null")
	command.Env = append(os.Environ(), "MYARCH_RUNNER_PTY_CHILD=1")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "terminal-ok") {
		t.Fatalf("foreground terminal failed: %v\n%s", err, output)
	}
}
