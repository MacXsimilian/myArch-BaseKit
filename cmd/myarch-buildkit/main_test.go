package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseOptionsValidCommands(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		command string
		check   func(t *testing.T, o Options)
	}{
		{"default", nil, "apply", nil},
		{"plan", []string{"plan"}, "plan", nil},
		{"dry run", []string{"apply", "--dry-run"}, "plan", nil},
		{"check flag", []string{"--check"}, "check", nil},
		{"explicit settings", []string{"check", "--settings", "/tmp/settings.json"}, "check", func(t *testing.T, o Options) {
			if o.SettingsPath != "/tmp/settings.json" {
				t.Fatal("lost settings path")
			}
		}},
		{"selected stages", []string{"apply", "--stage", "desktop", "--stage", "laptop"}, "apply", func(t *testing.T, o Options) {
			if !reflect.DeepEqual(o.Stages, []string{"desktop", "laptop"}) {
				t.Fatal("lost repeated stage choices")
			}
		}},
		{"package group", []string{"apply", "--stage", "packages", "--package-group", "containers"}, "apply", func(t *testing.T, o Options) {
			if o.PackageGroup != "containers" {
				t.Fatal("lost selected group")
			}
		}},
		{"configure only", []string{"--all", "--configure-only", "--keep-terminal"}, "apply", func(t *testing.T, o Options) {
			if !o.All || !o.ConfigureOnly || !o.KeepTerminal {
				t.Fatal("lost apply flags")
			}
		}},
		{"ratio override", []string{"--laptop-scale", "4/3"}, "apply", func(t *testing.T, o Options) {
			if o.LaptopScale != "4/3" {
				t.Fatal("lost requested exact scale")
			}
		}},
		{"notes opt in", []string{"--remove-notes", "gnote", "--remove-notes", "xpad"}, "apply", func(t *testing.T, o Options) {
			if !reflect.DeepEqual(o.RemoveNotes, []string{"gnote", "xpad"}) {
				t.Fatal("lost explicit notes removal")
			}
		}},
		{"stage restore", []string{"--restore-stage", "desktop", "--stage-run-dir", "/tmp/run/stages/desktop"}, "apply", func(t *testing.T, o Options) {
			if o.RestoreStage != "desktop" || o.StageRunDir != "/tmp/run/stages/desktop" {
				t.Fatal("lost explicit restore target")
			}
		}},
		{"profile restore", []string{"--restore-profile", "/tmp/run/stages/desktop"}, "apply", nil},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			o, err := ParseOptions(item.args)
			if err != nil {
				t.Fatal(err)
			}
			if o.Command != item.command {
				t.Fatalf("command %q, want %q", o.Command, item.command)
			}
			if item.check != nil {
				item.check(t, o)
			}
		})
	}
}
func TestParseOptionsRejectsAmbiguousOrUnsupportedExecution(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unknown option", []string{"--unknown"}},
		{"two commands", []string{"plan", "apply"}},
		{"missing value", []string{"--settings"}},
		{"flag as value", []string{"--settings", "--help"}},
		{"unknown stage", []string{"--stage", "theme"}},
		{"unknown group", []string{"--package-group", "themes"}},
		{"all and stage", []string{"--all", "--stage", "shell"}},
		{"invalid scale", []string{"--laptop-scale", "0"}},
		{"arbitrary notes", []string{"--remove-notes", "neovim"}},
		{"live desktop removed", []string{"--allow-live-desktop"}},
		{"check with execution", []string{"check", "--stage", "cleanup"}},
		{"check with package group", []string{"check", "--package-group", "core"}},
		{"check with notes", []string{"check", "--remove-notes", "gnote"}},
		{"configure only group", []string{"--configure-only", "--package-group", "core"}},
		{"restore without folder", []string{"--restore-stage", "desktop"}},
		{"restore relative folder", []string{"--restore-stage", "desktop", "--stage-run-dir", "run/stages/desktop"}},
		{"restore packages unsupported", []string{"--restore-stage", "packages", "--stage-run-dir", "/tmp/run"}},
		{"restore on explicit apply", []string{"apply", "--restore-profile", "/tmp/run"}},
		{"restore with apply flags", []string{"--restore-profile", "/tmp/run", "--keep-terminal"}},
		{"restore combined", []string{"--restore-profile", "/tmp/run", "--restore-stage", "desktop", "--stage-run-dir", "/tmp/run"}},
		{"folder without restore", []string{"--stage-run-dir", "/tmp/run"}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if _, err := ParseOptions(item.args); err == nil {
				t.Fatalf("accepted invalid execution: %v", item.args)
			}
		})
	}
}
func TestSelectedStagesFollowDependenciesAndDisabledGroups(t *testing.T) {
	withoutPackages := []string{"preflight", "defaults", "shell", "containers", "laptop", "desktop", "greeter", "verify"}
	cases := []struct {
		name     string
		opts     Options
		disabled []string
		want     []string
		fail     bool
	}{
		{name: "all", want: StageOrder},
		{name: "shell retry", opts: Options{Stages: []string{"shell"}}, want: []string{"preflight", "shell"}},
		{name: "canonical ordering", opts: Options{Stages: []string{"desktop", "laptop", "desktop"}}, want: []string{"preflight", "laptop", "desktop"}},
		{name: "configure only", opts: Options{ConfigureOnly: true}, want: withoutPackages},
		{name: "disabled container stage", opts: Options{Stages: []string{"containers"}}, disabled: []string{"containers"}, want: []string{"preflight"}},
		{name: "package group needs packages", opts: Options{Stages: []string{"verify"}, PackageGroup: "development"}, fail: true},
		{name: "disabled group rejected", opts: Options{Stages: []string{"packages"}, PackageGroup: "utilities"}, disabled: []string{"utilities"}, fail: true},
		{name: "explicit group accepted", opts: Options{Stages: []string{"packages"}, PackageGroup: "development"}, want: []string{"preflight", "packages"}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			settings := DefaultSettings()
			for _, group := range item.disabled {
				settings.PackageGroups[group] = false
			}
			actual, err := selectedStages(item.opts, settings)
			if item.fail {
				if err == nil {
					t.Fatal("invalid stage/group selection was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, item.want) {
				t.Fatalf("stages %v, want %v", actual, item.want)
			}
		})
	}
}

func mainStageContext(t *testing.T) *Context {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, filepath.Join(home, strings.ToLower(name)))
	}
	c, err := NewContext(filepath.Join(home, "run"), DefaultSettings(), Options{Command: "apply"}, &packageTestRunner{reply: func(cmd Command) (CommandResult, error) {
		return CommandResult{}, errors.New("unexpected host command: " + packageCmd(cmd))
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Reuse the test executable for orchestration tests, avoiding binary replacement.
	c.Binary, err = currentExecutable()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func mainStageSeam(t *testing.T, fn func(*Context, string, string, bool, []string) error) {
	t.Helper()
	previous := executeStageFunc
	executeStageFunc = fn
	t.Cleanup(func() { executeStageFunc = previous })
}
func mainStageSummary(t *testing.T, c *Context) map[string]any {
	t.Helper()
	report := map[string]any{}
	if err := ReadJSON(c.ReportPath, &report); err != nil {
		t.Fatal(err)
	}
	return report
}
func mainStageStatus(report map[string]any, name string) string {
	return str(object(object(report["stages"])[name])["status"])
}

func TestRunStagesPackageGroupingAndBootstrap(t *testing.T) {
	cases := []struct {
		name          string
		disabled      []string
		selectedGroup string
		want          []string
	}{
		{"all groups", nil, "", []string{"core", "utilities", "development", "containers"}},
		{"disabled optional groups", []string{"utilities", "containers"}, "", []string{"core", "development"}},
		{"single optional group", nil, "containers", []string{"containers"}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			c := mainStageContext(t)
			for _, group := range item.disabled {
				c.Settings.PackageGroups[group] = false
			}
			c.Options.PackageGroup = item.selectedGroup
			groups := []string{}
			bootstrap := []bool{}
			mainStageSeam(t, func(child *Context, stage, group string, bootstrapped bool, selected []string) error {
				if stage == "packages" {
					groups = append(groups, group)
					bootstrap = append(bootstrap, bootstrapped)
					if child.Options.PackageGroup != group {
						t.Fatal("tooling check lost stage group scope")
					}
				}
				return nil
			})
			code, err := RunStages(c, []string{"preflight", "packages"})
			if err != nil || code != 0 {
				t.Fatalf("unexpected result %d %v", code, err)
			}
			if !reflect.DeepEqual(groups, item.want) {
				t.Fatalf("groups %v, want %v", groups, item.want)
			}
			for i, done := range bootstrap {
				if done != (i > 0) {
					t.Fatalf("bootstrap state %v; system refresh should happen once", bootstrap)
				}
			}
		})
	}
}
func TestRunStagesCoreFailureDefersOptionalPackagesButKeepsIndependentStages(t *testing.T) {
	c := mainStageContext(t)
	executed := []string{}
	mainStageSeam(t, func(child *Context, stage, group string, _ bool, _ []string) error {
		executed = append(executed, stage+":"+group)
		if stage == "packages" && group == "core" {
			return errors.New("bootstrap failed")
		}
		return nil
	})
	code, err := RunStages(c, []string{"preflight", "packages", "shell", "cleanup"})
	if code != 2 || err == nil {
		t.Fatalf("expected partial failure, got %d %v", code, err)
	}
	if !reflect.DeepEqual(executed, []string{"preflight:", "packages:core", "shell:"}) {
		t.Fatalf("wrong independent/dependent stages: %v", executed)
	}
	summary := mainStageSummary(t, c)
	if mainStageStatus(summary, "packages-optional") != "skipped" || mainStageStatus(summary, "cleanup") != "skipped" {
		t.Fatal("failed bootstrap did not defer optional packages/cleanup")
	}
}
func TestRunStagesOptionalPackageFailureKeepsOtherOptionalGroups(t *testing.T) {
	c := mainStageContext(t)
	groups := []string{}
	mainStageSeam(t, func(_ *Context, stage, group string, _ bool, _ []string) error {
		if stage == "packages" {
			groups = append(groups, group)
			if group == "utilities" {
				return errors.New("optional recipe unavailable")
			}
		}
		return nil
	})
	code, err := RunStages(c, []string{"preflight", "packages"})
	if code != 2 || err == nil {
		t.Fatalf("expected partial failure: %d %v", code, err)
	}
	if !reflect.DeepEqual(groups, GroupOrder) {
		t.Fatalf("independent optional groups skipped after failure: %v", groups)
	}
}
func TestRunStagesLaptopFailureBlocksDesktop(t *testing.T) {
	c := mainStageContext(t)
	executed := []string{}
	mainStageSeam(t, func(_ *Context, stage, _ string, _ bool, _ []string) error {
		executed = append(executed, stage)
		if stage == "laptop" {
			return errors.New("internal keyboard not verified")
		}
		return nil
	})
	code, err := RunStages(c, []string{"preflight", "laptop", "desktop", "greeter", "cleanup"})
	if code != 2 || err == nil {
		t.Fatalf("expected failure: %d %v", code, err)
	}
	if !reflect.DeepEqual(executed, []string{"preflight", "laptop", "greeter"}) {
		t.Fatalf("wrong stage dependency behavior: %v", executed)
	}
	if mainStageStatus(mainStageSummary(t, c), "desktop") != "skipped" {
		t.Fatal("desktop not marked skipped after laptop failure")
	}
}
func TestRunStagesDesktopFailureBlocksDependentLaterStages(t *testing.T) {
	c := mainStageContext(t)
	executed := []string{}
	mainStageSeam(t, func(_ *Context, stage, _ string, _ bool, _ []string) error {
		executed = append(executed, stage)
		if stage == "desktop" {
			return errors.New("native validation failed")
		}
		return nil
	})
	code, err := RunStages(c, []string{"preflight", "desktop", "greeter", "verify", "cleanup"})
	if code != 2 || err == nil {
		t.Fatalf("expected failure: %d %v", code, err)
	}
	if !reflect.DeepEqual(executed, []string{"preflight", "desktop"}) {
		t.Fatalf("dependent stages executed after desktop failure: %v", executed)
	}
	summary := mainStageSummary(t, c)
	for _, name := range []string{"greeter", "verify", "cleanup"} {
		if mainStageStatus(summary, name) != "blocked" {
			t.Fatalf("dependent %s not blocked", name)
		}
	}
}
func TestRunStagesPendingLoginReturnsThreeAndBlocksCleanup(t *testing.T) {
	c := mainStageContext(t)
	executed := []string{}
	mainStageSeam(t, func(child *Context, stage, _ string, _ bool, _ []string) error {
		executed = append(executed, stage)
		if stage == "desktop" {
			return writeQueue(child, []QueueItem{{Manifest: filepath.Join(child.RunDir, "pending-profile.json"), Phase: "desktop"}})
		}
		return nil
	})
	code, err := RunStages(c, []string{"preflight", "desktop", "verify", "cleanup"})
	if code != 3 || err != nil {
		t.Fatalf("pending login should be successful exit 3: %d %v", code, err)
	}
	if !reflect.DeepEqual(executed, []string{"preflight", "desktop", "verify"}) {
		t.Fatalf("pending desktop allowed cleanup: %v", executed)
	}
	summary := mainStageSummary(t, c)
	if summary["status"] != "pending_login" || mainStageStatus(summary, "cleanup") != "blocked" {
		t.Fatal("pending state not reported separately")
	}
	if object(summary["running_desktop"])["changed_by_desktop_stage"] != false {
		t.Fatal("staging incorrectly reported a live desktop change")
	}
}
func TestRunStagesErrorTakesPriorityOverPendingExitCode(t *testing.T) {
	c := mainStageContext(t)
	mainStageSeam(t, func(child *Context, stage, _ string, _ bool, _ []string) error {
		if stage == "desktop" {
			return writeQueue(child, []QueueItem{{Manifest: filepath.Join(child.RunDir, "pending-profile.json"), Phase: "desktop"}})
		}
		if stage == "verify" {
			return errors.New("tooling incomplete")
		}
		return nil
	})
	code, err := RunStages(c, []string{"preflight", "desktop", "verify"})
	if code != 2 || err == nil {
		t.Fatalf("pending state hid actual failed stage: %d %v", code, err)
	}
	if mainStageSummary(t, c)["status"] != "partial" {
		t.Fatal("partial failure not reported")
	}
}
func TestRunStagesInterruptStopsSubsequentConfiguration(t *testing.T) {
	c := mainStageContext(t)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Runner = ExecRunner{Base: base}
	executed := []string{}
	mainStageSeam(t, func(_ *Context, stage, _ string, _ bool, _ []string) error {
		executed = append(executed, stage)
		if stage == "packages" {
			cancel()
			return context.Canceled
		}
		return nil
	})
	code, err := RunStages(c, []string{"preflight", "packages", "defaults", "shell", "cleanup"})
	if code != 130 || err == nil {
		t.Fatalf("expected interrupted exit130, got %d %v", code, err)
	}
	if !reflect.DeepEqual(executed, []string{"preflight", "packages"}) {
		t.Fatalf("configuration continued after interruption: %v", executed)
	}
	summary := mainStageSummary(t, c)
	if summary["status"] != "interrupted" || mainStageStatus(summary, "packages-core") != "interrupted" {
		t.Fatal("interruption not journalled")
	}
}
func TestFailedPreflightPreservesInstalledBinary(t *testing.T) {
	c := mainStageContext(t)
	c.Binary = filepath.Join(c.Home, ".local/bin/myarch-buildkit")
	if err := os.MkdirAll(filepath.Dir(c.Binary), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("previous verified binary")
	if err := os.WriteFile(c.Binary, original, 0755); err != nil {
		t.Fatal(err)
	}
	mainStageSeam(t, func(_ *Context, stage, _ string, _ bool, _ []string) error {
		if stage == "preflight" {
			return errors.New("requested scale incompatible")
		}
		return nil
	})
	code, err := RunStages(c, []string{"preflight", "laptop", "desktop"})
	if code != 2 || err == nil {
		t.Fatalf("expected failed preflight: %d %v", code, err)
	}
	actual, err := os.ReadFile(c.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(original) {
		t.Fatal("preflight failure overwrote installed executable")
	}
}

func TestStageJournalFailureStopsLaterActions(t *testing.T) {
	c := mainStageContext(t)
	executed := []string{}
	mainStageSeam(t, func(_ *Context, stage, _ string, _ bool, _ []string) error {
		executed = append(executed, stage)
		if stage == "preflight" {
			path := c.ReportPath
			if err := os.Remove(path); err != nil {
				return err
			}
			if err := os.Mkdir(path, 0700); err != nil {
				return err
			}
		}
		return nil
	})
	code, err := RunStages(c, []string{"preflight", "packages", "defaults"})
	if code != 2 || err == nil {
		t.Fatalf("journal failure not reported: %d %v", code, err)
	}
	if !reflect.DeepEqual(executed, []string{"preflight"}) {
		t.Fatalf("actions continued without a writable stage journal: %v", executed)
	}
}
func TestReadOnlyCLIPathsCreateNoFilesOrCommands(t *testing.T) {
	for _, args := range [][]string{{"plan"}, {"plan", "--details"}, {"--dry-run"}, {"--settings-template"}, {"--version"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			c := mainStageContext(t)
			runner := &packageTestRunner{reply: func(command Command) (CommandResult, error) {
				return CommandResult{}, errors.New("read-only CLI attempted " + packageCmd(command))
			}}
			code, err := dispatch(args, runner)
			if code != 0 || err != nil {
				t.Fatalf("unexpected result: %d %v", code, err)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("read-only CLI queried or mutated host services: %v", runner.calls)
			}
			entries, err := os.ReadDir(c.Home)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("read-only CLI created files: %v", entries)
			}
		})
	}
}
