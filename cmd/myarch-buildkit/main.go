package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const helpText = `myarch-buildkit — native Go installer

Usage:
  ./myarch-buildkit plan
  ./myarch-buildkit plan --details
  ./myarch-buildkit apply --all
  ./myarch-buildkit apply --stage desktop
  ./myarch-buildkit check
  ./myarch-buildkit check --details
  ./myarch-buildkit check --json
  ./myarch-buildkit --settings-template
  ./myarch-buildkit --restore-stage desktop --stage-run-dir /absolute/stage/folder

Options:
  --settings PATH          Settings JSON (schema 2)
  --stage NAME             Select a stage; repeat as needed
  --package-group NAME     Select core/utilities/development/containers
  --configure-only         Skip packages and cleanup
  --laptop-scale VALUE     Internal display scale; default 4/3
  --keep-terminal          Preserve Alacritty in cleanup
  --enable-podman-socket   Enable this user's local API socket
  --remove-notes NAME      Explicitly remove gnote/bijiben/knotes/xpad
  --restore-profile DIR    Restore a desktop stage (or resolve it in a run)
  --restore-stage NAME     Restore a stage using --stage-run-dir RUN_DIRECTORY
  --stage-run-dir DIR      Run folder containing report.json (older stage folders work)
  --cancel-pending NAME    Explicitly remove a missing laptop/desktop staging reference
  --dry-run                Same as plan; no changes or network
  --details                Show package options in plan or diagnostic details in check
  --json                   Print the complete check report as JSON
  --check                  Same as check
  --version                Build and executable hash
  --help                   This reference

Stages: preflight, packages, defaults, shell, containers, laptop,
desktop, greeter, verify, cleanup. Defaults enable all package groups.

Run as your regular user from a Hyprland terminal, without sudo in front.
Package review prompts remain interactive. Desktop/plugin/laptop files
are staged separately and applied before Hyprland and DMS at the next
myarch-buildkit login. Exit 3 means pending login; exit 2 means failure.
No theme setup, power/lid/idle policy, live handover or automatic rescue.
Only natural scrolling, internal Hungarian layout and 4/3 internal scale.
Plugins: Docker Manager, Kubernetes, Emoji & Unicode Launcher, Bongo Cat,
ClipBoard+. Bongo Cat input-group membership is an explicit manual step.
`

func ParseOptions(args []string) (Options, error) {
	o := Options{Command: "apply"}
	commandSet := false
	value := func(index *int, flag string) (string, error) {
		*index++
		if *index >= len(args) || strings.HasPrefix(args[*index], "--") {
			return "", fmt.Errorf("%s needs a value", flag)
		}
		return args[*index], nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "plan", "apply", "check":
			if commandSet {
				return o, errors.New("only one command may be selected")
			}
			o.Command = arg
			commandSet = true
		case "--help", "-h":
			o.Help = true
		case "--version":
			o.Version = true
		case "--settings-template":
			o.SettingsTemplate = true
		case "--all":
			o.All = true
		case "--dry-run":
			o.DryRun = true
		case "--details":
			o.Details = true
		case "--json":
			o.JSON = true
		case "--check":
			o.Check = true
		case "--configure-only":
			o.ConfigureOnly = true
		case "--keep-terminal":
			o.KeepTerminal = true
		case "--enable-podman-socket":
			o.EnablePodmanSocket = true
		case "--allow-live-desktop":
			return o, errors.New("--allow-live-desktop was removed; use staged next-login application")
		case "--settings", "--stage", "--package-group", "--restore-stage", "--stage-run-dir", "--restore-profile", "--laptop-scale", "--remove-notes", "--cancel-pending":
			v, err := value(&i, arg)
			if err != nil {
				return o, err
			}
			switch arg {
			case "--settings":
				o.SettingsPath = v
			case "--stage":
				o.Stages = append(o.Stages, v)
			case "--package-group":
				o.PackageGroup = v
			case "--restore-stage":
				o.RestoreStage = v
			case "--stage-run-dir":
				o.StageRunDir = v
			case "--restore-profile":
				o.RestoreProfile = v
			case "--laptop-scale":
				o.LaptopScale = v
			case "--remove-notes":
				o.RemoveNotes = append(o.RemoveNotes, v)
			case "--cancel-pending":
				o.CancelPending = v
			}
		default:
			return o, fmt.Errorf("unknown option: %s (see --help)", arg)
		}
	}
	if o.DryRun {
		o.Command = "plan"
	}
	if o.Check {
		o.Command = "check"
	}
	if o.Details && o.Command != "plan" && o.Command != "check" {
		return o, errors.New("--details is available with plan, --dry-run or check")
	}
	if o.JSON && (o.Command != "check" || o.Details) {
		return o, errors.New("--json is available with check and cannot be combined with --details")
	}
	for _, stage := range o.Stages {
		if !contains(StageOrder, stage) {
			return o, fmt.Errorf("unknown stage: %s", stage)
		}
	}
	if o.All && len(o.Stages) > 0 {
		return o, errors.New("--all cannot be combined with --stage")
	}
	if o.PackageGroup != "" && !contains(GroupOrder, o.PackageGroup) {
		return o, fmt.Errorf("unknown package group: %s", o.PackageGroup)
	}
	if o.LaptopScale != "" {
		if _, err := parseScaleValue(o.LaptopScale); err != nil {
			return o, err
		}
	}
	for _, note := range o.RemoveNotes {
		if !contains([]string{"gnote", "bijiben", "knotes", "xpad"}, note) {
			return o, fmt.Errorf("unsupported notes package: %s", note)
		}
	}
	restoring := o.RestoreStage != "" || o.RestoreProfile != ""
	if o.RestoreStage != "" && (!contains([]string{"defaults", "shell", "containers", "laptop", "desktop", "greeter", "cleanup"}, o.RestoreStage) || !filepath.IsAbs(o.StageRunDir)) {
		return o, errors.New("restore needs a supported stage and an absolute --stage-run-dir")
	}
	if o.RestoreProfile != "" && !filepath.IsAbs(o.RestoreProfile) {
		return o, errors.New("--restore-profile needs an absolute directory")
	}
	if restoring && (o.Command != "apply" || commandSet || len(o.Stages) > 0 || o.All || o.ConfigureOnly || o.PackageGroup != "" || o.LaptopScale != "" || o.KeepTerminal || o.EnablePodmanSocket || len(o.RemoveNotes) > 0) {
		return o, errors.New("restore must be used on its own")
	}
	if o.RestoreStage != "" && o.RestoreProfile != "" {
		return o, errors.New("select one restore operation")
	}
	if o.CancelPending != "" {
		if !contains([]string{"laptop", "desktop"}, o.CancelPending) {
			return o, errors.New("--cancel-pending supports laptop or desktop")
		}
		if o.Command != "apply" || commandSet || restoring || o.StageRunDir != "" || len(o.Stages) > 0 || o.All || o.ConfigureOnly || o.PackageGroup != "" || o.LaptopScale != "" || o.KeepTerminal || o.EnablePodmanSocket || len(o.RemoveNotes) > 0 {
			return o, errors.New("--cancel-pending must be used on its own")
		}
	}
	if o.StageRunDir != "" && o.RestoreStage == "" {
		return o, errors.New("--stage-run-dir requires --restore-stage")
	}
	if o.Command == "check" && (len(o.Stages) > 0 || o.All || o.ConfigureOnly || o.PackageGroup != "" || o.LaptopScale != "" || o.KeepTerminal || o.EnablePodmanSocket || len(o.RemoveNotes) > 0) {
		return o, errors.New("check cannot be combined with execution or apply-only options")
	}
	if o.ConfigureOnly && o.PackageGroup != "" {
		return o, errors.New("--package-group cannot be combined with --configure-only")
	}
	return o, nil
}

func selectedStages(o Options, s Settings) ([]string, error) {
	requested := o.Stages
	if len(requested) == 0 {
		requested = StageOrder
	}
	selected := map[string]bool{"preflight": true}
	for _, stage := range requested {
		selected[stage] = true
	}
	if o.ConfigureOnly {
		delete(selected, "packages")
		delete(selected, "cleanup")
	}
	if !s.PackageGroups["containers"] {
		delete(selected, "containers")
	}
	if o.PackageGroup != "" {
		if !selected["packages"] {
			return nil, errors.New("a package group requires the packages stage")
		}
		if !s.PackageGroups[o.PackageGroup] {
			return nil, errors.New("selected package group is disabled")
		}
	}
	result := []string{}
	for _, stage := range StageOrder {
		if selected[stage] {
			result = append(result, stage)
		}
	}
	return result, nil
}

func requireCommands(names ...string) error {
	for _, name := range names {
		if !hasCommand(name) {
			return fmt.Errorf("stage requires %s; run apply --stage packages first", name)
		}
	}
	return nil
}
func observeProfile(c *Context) error {
	items, err := PendingItems(c)
	if err != nil {
		c.Report["pending_configuration"] = pendingFailure(err)
		observeRunningWhilePending(c)
		return err
	}
	if len(items) > 0 {
		c.Report["pending_configuration"] = map[string]any{"status": "pending", "items": items}
		if err = ValidatePending(c); err != nil {
			record := pendingFailure(err)
			record["items"] = items
			c.Report["pending_configuration"] = record
			observeRunningWhilePending(c)
			return err
		}
		observeRunningWhilePending(c)
		c.Note("Pending configuration validated; awaiting myarch-buildkit login. Running desktop is reported separately.")
		return nil
	}
	c.Report["pending_configuration"] = map[string]any{"status": "none"}
	if err = InspectSession(c); err != nil {
		c.Report["running_desktop"] = map[string]any{"ready": false, "error": err.Error()}
		return err
	}
	err = errors.Join(checkHyprlandErrors(c), CheckDesktop(c))
	record := object(c.Report["running_desktop"])
	record["ready"] = err == nil
	if err != nil {
		record["error"] = err.Error()
	}
	c.Report["running_desktop"] = record
	return err
}

func pendingFailure(err error) map[string]any {
	record := map[string]any{"status": "blocked", "error": err.Error()}
	var problem *PendingConfigurationError
	if errors.As(err, &problem) {
		record["phase"] = problem.Phase
		record["manifest"] = problem.Manifest
		record["report"] = problem.Report
		record["recovery_command"] = problem.RecoveryCommand
		record["recovery_commands"] = problem.RecoveryCommands
	}
	return record
}

// A staged profile is checked independently of the desktop still running now.
func observeRunningWhilePending(c *Context) {
	record := map[string]any{"verification_scope": "Observation only; staged configuration has not been applied."}
	if os.Getenv("WAYLAND_DISPLAY") == "" || os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" {
		record["status"] = "not_observed"
		record["reason"] = "Open a terminal in your Hyprland session to observe the running desktop."
	} else {
		record["status"] = "observed"
		reply := desktopIPCProbe(c, "theme", "getMode")
		mode := strings.Trim(strings.TrimSpace(reply.Stdout), "\"")
		record["dms_ipc_exit_code"] = reply.Code
		record["dms_ipc_mode"] = mode
		record["dms_responding"] = reply.Code == 0 && (mode == "dark" || mode == "light")
		if err := checkHyprlandErrors(c); err != nil {
			record["hyprland_error"] = err.Error()
		}
	}
	c.Report["running_desktop"] = record
}
func checkHyprlandErrors(c *Context) error {
	reply, err := c.Command(Command{Args: []string{"hyprctl", "-j", "configerrors"}, Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("cannot verify running Hyprland configuration: %w", err)
	}
	var messages []string
	if json.Unmarshal([]byte(reply.Stdout), &messages) != nil || messages == nil {
		return errors.New("unrecognized Hyprland configuration-error response")
	}
	c.Report["hyprland_configerrors"] = messages
	for _, message := range messages {
		if strings.TrimSpace(message) != "" {
			return fmt.Errorf("Hyprland configuration errors: %s", strings.Join(messages, "\n"))
		}
	}
	return nil
}
func checkAll(c *Context) error {
	quiet := c.Quiet
	c.Quiet = true
	defer func() { c.Quiet = quiet }()
	failures := []error{}
	type namedCheck struct {
		name  string
		check func(*Context) error
	}
	checks := []namedCheck{{"tooling", CheckTooling}, {"defaults", CheckDefaults}}
	if c.Settings.PackageGroups["containers"] {
		checks = append(checks, namedCheck{"containers", CheckContainers})
	}
	checks = append(checks, namedCheck{"profile", observeProfile})
	results := map[string]any{}
	c.Report["check_results"] = results
	for _, check := range checks {
		result := map[string]any{"status": "passed"}
		if err := check.check(c); err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
			failures = append(failures, err)
		}
		results[check.name] = result
	}
	c.Report["status"] = "verified"
	if len(failures) > 0 {
		c.Report["status"] = "incomplete"
		c.Report["check_error"] = errors.Join(failures...).Error()
	} else if object(c.Report["pending_configuration"])["status"] == "pending" {
		c.Report["status"] = "pending_login"
	}
	return errors.Join(failures...)
}

func stageRun(c *Context, name string, group string, bootstrapped bool, selected []string) error {
	c.StageName = name
	if name == "desktop" || name == "greeter" {
		if _, err := c.Command(Command{Args: []string{"sudo", "-v"}, Interactive: true}); err != nil {
			return err
		}
	}
	switch name {
	case "preflight":
		c.Report["settings_validation"] = "passed"
		if contains(selected, "laptop") || contains(selected, "desktop") {
			if err := InspectSession(c); err != nil {
				return err
			}
			return PreflightLaptop(c)
		}
		return nil
	case "packages":
		if err := InstallPackages(c, group, bootstrapped); err != nil {
			return err
		}
		return CheckTooling(c)
	case "defaults":
		if err := requireCommands("xdg-mime", "code", "obsidian", "firefox", "vlc", "ghostty", "xdg-terminal-exec"); err != nil {
			return err
		}
		return ConfigureDefaults(c)
	case "shell":
		if err := requireCommands("starship", "fzf", "zoxide", "mise", "direnv"); err != nil {
			return err
		}
		return ConfigureShell(c)
	case "containers":
		if err := requireCommands("podman", "podman-compose"); err != nil {
			return err
		}
		return ConfigureContainers(c)
	case "laptop", "desktop":
		if err := InspectSession(c); err != nil {
			return err
		}
		if err := c.StartStaging(); err != nil {
			return err
		}
		if name == "laptop" {
			if err := ConfigureLaptop(c); err != nil {
				return err
			}
			c.Pending.Validation = map[string]any{"scale_preflight_passed": true, "requires_desktop_stage": true}
			main := str(c.Inventory["active_config"])
			ext := filepath.Ext(main)
			managed := filepath.Join(filepath.Dir(main), "desktop-managed"+ext)
			custom := filepath.Join(filepath.Dir(main), "desktop-custom"+ext)
			fragment := str(object(c.Report["laptop"])["fragment"])
			data, _ := os.ReadFile(managed)
			mainData, _ := os.ReadFile(main)
			if exists(custom) && strings.Contains(string(data), fragment) && strings.Contains(string(mainData), managed) {
				c.Report["profile_config"] = map[string]any{"main_config": main, "managed_config": managed, "custom_config": custom, "laptop_fragment": fragment}
				if err := ValidateDesktop(c); err != nil {
					return err
				}
			}
		} else {
			if err := requireCommands("dms", "hyprctl", "git", "podman", "kubectl", "evtest", "libinput", "ghostty", "code", "nautilus"); err != nil {
				return err
			}
			if err := ConfigureDesktop(c); err != nil {
				return err
			}
			if err := ValidateDesktop(c); err != nil {
				return err
			}
			if err := InstallLogin(c); err != nil {
				return err
			}
		}
		return c.Publish(name)
	case "greeter":
		if err := requireCommands("hyprctl", "dms-greeter", "greetd"); err != nil {
			return err
		}
		if err := InspectSession(c); err != nil {
			return err
		}
		return ConfigureGreeter(c)
	case "verify":
		return checkAll(c)
	case "cleanup":
		return ConfigureCleanup(c)
	}
	return fmt.Errorf("unknown stage: %s", name)
}

func acquireLock(c *Context) (func(), error) {
	directory := filepath.Join(c.StateHome, "myarch-buildkit")
	if err := securePath(directory, true); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, "setup.lock")
	if err := securePath(path, true); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another setup, login application or restore is running")
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}

func ensureUserBinary(c *Context) error {
	current, err := currentExecutable()
	if err != nil {
		return err
	}
	current, err = filepath.EvalSymlinks(current)
	if err != nil {
		return err
	}
	if current == c.Binary {
		return nil
	}
	data, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	writer := *c
	if c.ReportScope == "_run" {
		writer.RunDir = filepath.Join(stateRoot(c), "runs", filepath.Base(filepath.Dir(c.ReportPath)), "_run")
	}
	return writer.Write(c.Binary, data, 0755)
}

var executeStageFunc = stageRun

func RunStages(root *Context, selected []string) (int, error) {
	if err := os.MkdirAll(root.RunDir, 0700); err != nil {
		return 2, err
	}
	root.ReportPath = filepath.Join(root.RunDir, "report.json")
	root.ReportScope = "_run"
	effective := root.ReportPath
	executable, _ := currentExecutable()
	hash, _ := FileHash(executable)
	root.Report["provenance"] = map[string]any{"build": BuildID, "executable": executable, "sha256": hash}
	summary := map[string]any{"format": RunReportFormat, "schema_version": RunReportSchemaVersion, "build": BuildID, "settings": root.Settings, "settings_file": effective, "provenance": root.Report["provenance"], "status": "running", "stages": map[string]any{}, "profiles": map[string]any{}, "rollback_limits": "Package upgrades are retained; configuration restore is per stage.", "manual_checks": []string{"Login and greeter", "Internal scale, Hungarian layout and natural scrolling", "Five DMS plugins; Bongo Cat input-group permission", "Camera, recording and audio"}}
	summaryPath := root.ReportPath
	records := object(summary["stages"])
	if err := WriteJSON(summaryPath, summary); err != nil {
		return 2, err
	}
	if err := root.Save(); err != nil {
		return 2, err
	}
	failed := false
	bootstrapped := false
	var fatal error
	execute := func(name, stage, group string) bool {
		if err := root.Err(); err != nil {
			fatal = err
			return false
		}
		folder := filepath.Join(stateRoot(root), "runs", filepath.Base(root.RunDir), name)
		opts := root.Options
		opts.PackageGroup = group
		c, err := NewContext(folder, root.Settings, opts, root.Runner)
		if err != nil {
			records[name] = map[string]any{"status": "failed", "error": err.Error()}
			failed = true
			return false
		}
		c.ReportPath = root.ReportPath
		c.ReportScope = name
		if err := securePath(folder, true); err != nil {
			fatal = err
			return false
		}
		if err := os.MkdirAll(folder, 0700); err != nil {
			fatal = err
			return false
		}
		record := map[string]any{"status": "running", "artifacts": folder, "started": time.Now().UTC().Format(time.RFC3339)}
		records[name] = record
		if err := WriteRunSummary(summaryPath, summary); err != nil {
			fatal = err
			return false
		}
		fmt.Printf("\n==> %s\n", runStageLabel(name))
		err = executeStageFunc(c, stage, group, bootstrapped, selected)
		status := "completed"
		code := 0
		if err != nil {
			status = "failed"
			code = 2
			c.Report["warnings"] = append(stringList(c.Report["warnings"]), err.Error())
			failed = true
		} else if stage == "laptop" || stage == "desktop" {
			status = "pending_login"
		}
		if root.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			fatal = errors.Join(err, root.Err())
			status, code = "interrupted", 130
		}
		c.Report["status"] = status
		if saveErr := c.Save(); saveErr != nil {
			failed = true
			status = "failed"
			code = 2
			err = errors.Join(err, saveErr)
		}
		record["status"] = status
		record["exit_code"] = code
		commandBinary := executable
		if commandBinary == "" {
			commandBinary = root.Binary
		}
		retry := []string{commandBinary, "apply", "--stage", stage, "--settings", effective}
		if group != "" {
			retry = append(retry, "--package-group", group)
		}
		if root.Options.KeepTerminal {
			retry = append(retry, "--keep-terminal")
		}
		if root.Options.EnablePodmanSocket {
			retry = append(retry, "--enable-podman-socket")
		}
		for _, note := range root.Options.RemoveNotes {
			retry = append(retry, "--remove-notes", note)
		}
		record["retry"] = planShellWords(retry)
		if stage != "packages" && stage != "preflight" && stage != "verify" {
			record["restore"] = planShellWords([]string{commandBinary, "--restore-stage", stage, "--stage-run-dir", root.RunDir})
		}
		if err != nil {
			record["error"] = err.Error()
			var problem *PendingConfigurationError
			if errors.As(err, &problem) {
				record["recovery_command"] = problem.RecoveryCommand
				record["recovery_commands"] = problem.RecoveryCommands
			}
		}
		if saveErr := WriteRunSummary(summaryPath, summary); saveErr != nil {
			fatal = errors.Join(fatal, saveErr)
		}
		return err == nil
	}
	for _, stage := range selected {
		if fatal != nil || root.Err() != nil {
			fatal = errors.Join(fatal, root.Err())
			break
		}
		if stage == "cleanup" {
			if failed {
				records[stage] = map[string]any{"status": "skipped", "reason": "An earlier selected stage failed; cleanup requires passing verification"}
				continue
			}
			items, err := PendingItems(root)
			if err != nil {
				records[stage] = map[string]any{"status": "blocked", "reason": "The saved pending configuration cannot be read", "error": err.Error()}
				fatal = errors.Join(fatal, err)
				continue
			}
			if len(items) > 0 {
				records[stage] = map[string]any{"status": "blocked", "reason": "Configuration pending; check after the next myarch-buildkit login before cleanup"}
				continue
			}
		}
		if stage == "packages" {
			if root.Options.PackageGroup != "" {
				ok := execute("packages-"+root.Options.PackageGroup, stage, root.Options.PackageGroup)
				bootstrapped = ok
				continue
			}
			if !execute("packages-core", stage, "core") {
				records["packages-optional"] = map[string]any{"status": "skipped", "reason": "Core bootstrap failed"}
				continue
			}
			bootstrapped = true
			for _, group := range GroupOrder[1:] {
				if fatal != nil || root.Err() != nil {
					break
				}
				if root.Settings.PackageGroups[group] {
					execute("packages-"+group, stage, group)
				}
			}
			continue
		}
		if stage == "desktop" && contains(selected, "laptop") && str(object(records["laptop"])["status"]) != "pending_login" {
			records[stage] = map[string]any{"status": "skipped", "reason": "Selected laptop stage failed"}
			continue
		}
		if !execute(stage, stage, "") {
			if stage == "preflight" {
				break
			}
			if stage == "desktop" {
				for _, later := range selected {
					if contains([]string{"greeter", "verify", "cleanup"}, later) {
						records[later] = map[string]any{"status": "blocked", "reason": "Desktop staging failed"}
					}
				}
				break
			}
		}
		if stage == "preflight" && fatal == nil && needsInstalledBinary(selected) {
			if err := ensureUserBinary(root); err != nil {
				fatal = err
				break
			}
			if err := root.Save(); err != nil {
				fatal = err
				break
			}
		}
	}
	items, err := PendingItems(root)
	if err != nil {
		fatal = errors.Join(fatal, err)
	}
	summary["status"] = "completed_with_manual_checks"
	if len(items) > 0 {
		summary["status"] = "pending_login"
	}
	if failed {
		summary["status"] = "partial"
	}
	if fatal != nil {
		summary["status"] = "partial"
		summary["error"] = fatal.Error()
		if root.Err() != nil || errors.Is(fatal, context.Canceled) || errors.Is(fatal, context.DeadlineExceeded) {
			summary["status"] = "interrupted"
		}
	}
	summary["pending_configuration"] = map[string]any{"status": map[bool]string{true: "pending", false: "none"}[len(items) > 0]}
	if err != nil {
		summary["pending_configuration"] = pendingFailure(err)
	}
	summary["running_desktop"] = map[string]any{"changed_by_desktop_stage": false, "readiness": "reported by check"}
	if err = WriteRunSummary(summaryPath, summary); err != nil {
		return 2, err
	}
	fmt.Print(runResultsText(root, summary))
	if fatal != nil {
		if summary["status"] == "interrupted" {
			return 130, fatal
		}
		return 2, fatal
	}
	if failed {
		return 2, errors.New("setup is incomplete; see report.json and the printed recovery commands")
	}
	if len(items) > 0 {
		return 3, nil
	}
	return 0, nil
}

func needsInstalledBinary(stages []string) bool {
	for _, stage := range stages {
		if !contains([]string{"preflight", "verify", "cleanup"}, stage) {
			return true
		}
	}
	return false
}

type printedError struct{ error }

func (e *printedError) Unwrap() error { return e.error }

func dispatch(args []string, runner Runner) (int, error) {
	if len(args) > 0 && strings.HasPrefix(args[0], "internal-") {
		return 0, SystemCommand(args)
	}
	if len(args) > 0 && contains([]string{"login", "desktop-session", "screenshot", "clipboard", "obsidian-open"}, args[0]) {
		c, err := NewContext("", DefaultSettings(), Options{}, runner)
		if err != nil {
			return 2, err
		}
		switch args[0] {
		case "login":
			err = LoginMain(c)
		case "desktop-session":
			err = DesktopSession(c)
		case "screenshot":
			err = ScreenshotMain(c, args[1:])
		case "clipboard":
			err = ClipboardMain(c, args[1:])
		case "obsidian-open":
			err = ObsidianOpen(c, args[1:])
		}
		if err != nil {
			return 2, err
		}
		return 0, nil
	}
	o, err := ParseOptions(args)
	if err != nil {
		return 2, err
	}
	if o.Help {
		fmt.Print(helpText)
		return 0, nil
	}
	if o.Version {
		path, _ := currentExecutable()
		hash, _ := FileHash(path)
		fmt.Println("myarch-buildkit", BuildID)
		fmt.Println("Executable SHA256:", hash)
		return 0, nil
	}
	if o.SettingsTemplate {
		data, _ := json.MarshalIndent(DefaultSettings(), "", "  ")
		fmt.Println(string(data))
		return 0, nil
	}
	settingsPath := o.SettingsPath
	if settingsPath == "" {
		home, _ := os.UserHomeDir()
		config, _ := xdgPath("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		candidate := filepath.Join(config, "myarch-buildkit/settings.json")
		if exists(candidate) {
			settingsPath = candidate
		}
	}
	settings, err := LoadSettings(settingsPath)
	if err != nil {
		return 2, err
	}
	if o.LaptopScale != "" {
		settings.Laptop.InternalScale = o.LaptopScale
	}
	if o.Command == "plan" {
		o.SettingsPath = settingsPath
		return 0, PrintPlan(o, settings)
	}
	selected, err := selectedStages(o, settings)
	if err != nil {
		return 2, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 2, err
	}
	runDir := filepath.Join(cwd, "myarch-buildkit-runs", time.Now().UTC().Format("20060102-150405")+fmt.Sprintf("-%d", os.Getpid()))
	if o.Command == "check" {
		runDir = ""
	}
	if o.RestoreStage != "" {
		runDir = o.StageRunDir
	}
	if o.RestoreProfile != "" {
		runDir = o.RestoreProfile
	}
	c, err := NewContext(runDir, settings, o, runner)
	if err != nil {
		return 2, err
	}
	err = CheckPlatform(c, o.Command != "check" && o.CancelPending == "")
	if o.Command == "check" {
		c.CheckOnly = true
		if err == nil {
			err = checkAll(c)
		} else {
			c.Report["status"] = "incomplete"
			c.Report["check_error"] = err.Error()
		}
		return printCheck(c, o, err)
	}
	if err != nil {
		return 2, err
	}
	unlock, err := acquireLock(c)
	if err != nil {
		return 2, err
	}
	defer unlock()
	if o.CancelPending != "" {
		if err := CancelMissingPending(c, o.CancelPending); err != nil {
			return 2, err
		}
		fmt.Printf("Cancelled the missing %s staging reference. Current desktop files were left as they are.\n", o.CancelPending)
		fmt.Println("Run ./myarch-buildkit check to inspect the remaining state before staging a replacement.")
		return 0, nil
	}
	if o.RestoreStage != "" || o.RestoreProfile != "" {
		if _, err = c.Command(Command{Args: []string{"sudo", "-v"}, Interactive: true}); err != nil {
			return 2, err
		}
		err = RestoreContext(c)
		if err != nil {
			return 2, err
		}
		fmt.Println("Restored configuration; installed packages were retained.")
		return 0, nil
	}
	return RunStages(c, selected)
}

func printCheck(c *Context, o Options, err error) (int, error) {
	if o.JSON {
		data, marshalErr := json.MarshalIndent(c.Report, "", "  ")
		if marshalErr != nil {
			return 2, marshalErr
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(checkText(o, c, err))
	}
	if err != nil {
		return 2, &printedError{err}
	}
	if c.Report["status"] == "pending_login" {
		return 3, nil
	}
	return 0, nil
}

func main() {
	base, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	code, err := dispatch(os.Args[1:], ExecRunner{Base: base})
	if base.Err() != nil {
		code = 130
	}
	if err != nil {
		var shown *printedError
		if !errors.As(err, &shown) {
			fmt.Fprintln(os.Stderr, "ERROR:", err)
		}
		if code == 0 {
			code = 2
		}
	}
	os.Exit(code)
}
