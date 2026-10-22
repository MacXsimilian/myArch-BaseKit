package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type ExecRunner struct{ Base context.Context }

func (r ExecRunner) Err() error {
	if r.Base != nil {
		return r.Base.Err()
	}
	return nil
}

func (r ExecRunner) Execute(command Command) (CommandResult, error) {
	if len(command.Args) == 0 {
		return CommandResult{}, errors.New("empty command")
	}
	base := r.Base
	if base == nil {
		base = context.Background()
	}
	if err := base.Err(); err != nil {
		return CommandResult{Code: 130}, err
	}
	limit := command.Timeout
	if limit == 0 && !command.Interactive {
		limit = 120 * time.Second
	}
	var cancel context.CancelFunc
	if limit > 0 {
		base, cancel = context.WithTimeout(base, limit)
		defer cancel()
	}
	process := exec.CommandContext(base, command.Args[0], command.Args[1:]...)
	// A descendant can inherit our output pipes after its parent exits. Bound
	// that wait as well as the command itself.
	process.WaitDelay = time.Second
	if command.Detached {
		process = exec.Command(command.Args[0], command.Args[1:]...)
		process.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	} else if !command.Interactive {
		process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		process.Cancel = func() error {
			if filepath.Base(command.Args[0]) == "sudo" {
				// sudo must forward termination to its privileged child. Killing
				// the unprivileged wrapper first would prevent that forwarding.
				return process.Process.Signal(syscall.SIGTERM)
			}
			// Noninteractive processes have their own group: cancellation must
			// also stop descendants that could keep writing after the deadline.
			err := syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
	} else {
		// Keep the foreground terminal group intact for package review prompts.
		// sudo and other interactive wrappers get a chance to stop their work;
		// WaitDelay bounds a wrapper that ignores termination.
		process.Cancel = func() error { return process.Process.Signal(syscall.SIGTERM) }
	}
	process.Dir = command.Dir
	process.Env = commandEnvironment(command)
	var stdout, stderr bytes.Buffer
	if command.Detached {
		if err := process.Start(); err != nil {
			return CommandResult{Code: 127}, err
		}
		return CommandResult{}, process.Process.Release()
	}
	if command.Interactive {
		process.Stdin = os.Stdin
		process.Stdout = os.Stdout
		process.Stderr = os.Stderr
	} else {
		process.Stdout = &stdout
		process.Stderr = &stderr
		if command.Input != nil {
			process.Stdin = bytes.NewReader(command.Input)
		}
	}
	err := process.Run()
	if err != nil && process.Process != nil && !command.Interactive && filepath.Base(command.Args[0]) != "sudo" {
		// An unsuccessful parent must not leave writers running. A nonzero
		// exit can mask ErrWaitDelay when descendants kept output pipes open.
		// Successful commands such as wl-copy may intentionally daemonize.
		_ = syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
	}
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if base.Err() != nil {
		result.Code = 130
		return result, base.Err()
	}
	if err == nil {
		return result, nil
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		result.Code = 124
		return result, err
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.Code = exit.ExitCode()
		if result.Code < 0 {
			result.Code = 130
		}
		return result, nil
	}
	result.Code = 127
	return result, err
}

// commandEnvironment keeps noninteractive probes deterministic while allowing
// explicitly supplied variables to override the default locale.
func commandEnvironment(command Command) []string {
	environmentEntries := os.Environ()
	if command.Env != nil || (!command.Interactive && !command.Detached) {
		environment := map[string]string{}
		for _, entry := range environmentEntries {
			key, value, _ := strings.Cut(entry, "=")
			environment[key] = value
		}
		if !command.Interactive && !command.Detached {
			environment["LC_ALL"] = "C"
		}
		for key, value := range command.Env {
			environment[key] = value
		}
		keys := make([]string, 0, len(environment))
		for key := range environment {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		environmentEntries = nil
		for _, key := range keys {
			environmentEntries = append(environmentEntries, key+"="+environment[key])
		}
	}
	return environmentEntries
}
