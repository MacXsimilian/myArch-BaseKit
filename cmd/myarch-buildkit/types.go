package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Settings struct {
	SchemaVersion int             `json:"schema_version"`
	PackageGroups map[string]bool `json:"package_groups"`
	Laptop        LaptopSettings  `json:"laptop"`
}
type LaptopSettings struct {
	InternalScale    any    `json:"internal_scale"`
	InternalKeyboard string `json:"internal_keyboard"`
	NaturalScroll    bool   `json:"natural_scroll"`
}
type Options struct {
	Command, SettingsPath, PackageGroup, RestoreStage, StageRunDir, RestoreProfile, LaptopScale, CancelPending string
	Stages                                                                                                     []string
	All, ConfigureOnly, KeepTerminal, EnablePodmanSocket, DryRun, Check, Help, Version, SettingsTemplate       bool
	RemoveNotes                                                                                                []string
	Details                                                                                                    bool
	JSON                                                                                                       bool
}
type Command struct {
	Args        []string
	Dir         string
	Env         map[string]string
	Input       []byte
	Timeout     time.Duration
	Interactive bool
	Detached    bool
}
type CommandResult struct {
	Code           int
	Stdout, Stderr string
}
type Runner interface {
	Execute(Command) (CommandResult, error)
}
type Context struct {
	Home, ConfigHome, StateHome, DataHome, CacheHome, BinDir, RunDir, ReportPath, Binary, StageName string
	ReportScope                                                                                     string
	Settings                                                                                        Settings
	Options                                                                                         Options
	Inventory                                                                                       map[string]any
	Report                                                                                          map[string]any
	Runner                                                                                          Runner
	Pending                                                                                         *Manifest
	CheckOnly                                                                                       bool
	Quiet                                                                                           bool
	rootBinaryReady                                                                                 bool
}

func (c *Context) Run(args ...string) (CommandResult, error) {
	return c.Command(Command{Args: args})
}
func (c *Context) Command(cmd Command) (CommandResult, error) {
	if err := c.Err(); err != nil {
		return CommandResult{Code: 130}, err
	}
	result, err := c.Runner.Execute(cmd)
	if err != nil {
		return result, err
	}
	if result.Code != 0 {
		return result, fmt.Errorf("%s failed (%d): %s", shellJoin(cmd.Args), result.Code, strings.TrimSpace(result.Stderr))
	}
	return result, nil
}
func (c *Context) Err() error {
	if runner, ok := c.Runner.(interface{ Err() error }); ok {
		return runner.Err()
	}
	return nil
}
func (c *Context) Try(args ...string) CommandResult {
	result, err := c.Runner.Execute(Command{Args: args})
	if err != nil {
		result.Code = 127
		result.Stderr = err.Error()
	}
	return result
}
func (c *Context) Warn(message string) {
	c.Report["warnings"] = append(stringList(c.Report["warnings"]), message)
	if !c.CheckOnly && !c.Quiet {
		fmt.Fprintln(os.Stderr, "WARNING:", message)
	}
}
func (c *Context) Note(message string) {
	c.Report["notes"] = append(stringList(c.Report["notes"]), message)
	if !c.CheckOnly && !c.Quiet {
		fmt.Println(message)
	}
}
func stringList(v any) []string {
	if list, ok := v.([]string); ok {
		return list
	}
	result := []string{}
	if list, ok := v.([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
	}
	return result
}
func object(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func exists(path string) bool     { _, err := os.Stat(path); return err == nil }
func hasCommand(name string) bool { _, err := exec.LookPath(name); return err == nil }

func currentExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		path, err = exec.LookPath(os.Args[0])
	}
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func shellJoin(args []string) string {
	parts := make([]string, len(args))
	for i, s := range args {
		parts[i] = shellQuote(s)
	}
	return strings.Join(parts, " ")
}
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
func mapJSONNumber(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return 0
}
