package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
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
	if command.Detached {
		process = exec.Command(command.Args[0], command.Args[1:]...)
		process.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	process.Dir = command.Dir
	process.Env = os.Environ()
	if command.Env != nil || (!command.Interactive && !command.Detached) {
		environment := map[string]string{}
		for _, entry := range process.Env {
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
		process.Env = nil
		for _, key := range keys {
			process.Env = append(process.Env, key+"="+environment[key])
		}
	}
	var stdout, stderr bytes.Buffer
	if command.Detached {
		if err := process.Start(); err != nil {
			return CommandResult{Code: 127}, err
		}
		return CommandResult{}, process.Process.Release()
	} else if command.Interactive {
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
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if base.Err() != nil {
		result.Code = 130
		return result, base.Err()
	}
	if err == nil {
		return result, nil
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
