package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func previewForTest(t *testing.T, o Options, s Settings) string {
	t.Helper()
	stages, err := selectedStages(o, s)
	if err != nil {
		t.Fatal(err)
	}
	return planText(o, s, stages, false)
}

func TestPlanRespectsSelectedStages(t *testing.T) {
	cases := []struct {
		stages          []string
		present, absent []string
	}{
		{[]string{"shell"}, []string{"Shell", "Starship"}, []string{"Packages\n", "At your next myarch-buildkit login", "Plugins", "Login screen", "Cleanup"}},
		{[]string{"packages"}, []string{"Packages", "selected requirements"}, []string{"Plugins", "Internal display", "Login screen", "App defaults", "sudo usermod", "--stage verify --stage cleanup"}},
		{[]string{"cleanup"}, []string{"Cleanup", "after checks pass"}, []string{"At your next myarch-buildkit login", "Plugins", "sudo usermod", "./myarch-buildkit check", "--stage verify --stage cleanup"}},
		{[]string{"laptop"}, []string{"Internal display", "first laptop setup", "./myarch-buildkit check"}, []string{"Plugins", "Docker Manager", "Login screen", "sudo usermod", "--stage verify --stage cleanup"}},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.stages, ","), func(t *testing.T) {
			text := previewForTest(t, Options{Command: "plan", Stages: c.stages}, DefaultSettings())
			for _, expected := range c.present {
				if !strings.Contains(text, expected) {
					t.Errorf("missing %q", expected)
				}
			}
			for _, unexpected := range c.absent {
				if strings.Contains(text, unexpected) {
					t.Errorf("unselected action %q", unexpected)
				}
			}
		})
	}
}

func TestPlanEffectivePreferencesAndExplicitOptions(t *testing.T) {
	s := DefaultSettings()
	s.Laptop = LaptopSettings{"3/2", "de", false}
	o := Options{Command: "plan", EnablePodmanSocket: true, KeepTerminal: true, RemoveNotes: []string{"gnote"}}
	text := previewForTest(t, o, s)
	for _, expected := range []string{"3/2 scale", "de; verified", "Off for pointers", "Enable your user socket", "Alacritty", "gnote"} {
		if !strings.Contains(text, expected) {
			t.Errorf("effective choice missing: %q", expected)
		}
	}
	if strings.Contains(text, "Hungarian") || strings.Contains(text, "4/3 scale") {
		t.Fatal("hardcoded default overrode effective settings")
	}
}

func TestPlanPackageTableReferencesPreserveGroupMembership(t *testing.T) {
	o := Options{Command: "plan", Details: true}
	s := DefaultSettings()
	wantReferences := map[string]string{"development": "kubectl", "containers": "Podman"}
	seenRoles := map[string]bool{}
	for _, group := range GroupOrder {
		rows := planTableRows(o, s, group)
		previous := ""
		references := 0
		for _, row := range rows {
			if row.Tool.Group != group {
				t.Errorf("%s tool was moved out of %s", row.Tool.Label, row.Tool.Group)
			}
			name := strings.ToLower(row.Name)
			if previous > name {
				t.Errorf("%s display names not A-Z: %q before %q", group, previous, name)
			}
			previous = name
			if row.Name != planToolName(row.Tool) {
				t.Errorf("%s was sorted/displayed by a different name", row.Tool.Label)
			}
			if seenRoles[row.Tool.Label] {
				t.Errorf("role represented more than once: %s", row.Tool.Label)
			}
			seenRoles[row.Tool.Label] = true
			if row.Reference == "" {
				if row.Pacman != strings.Join(row.Tool.RepoCandidates, ", ") {
					t.Errorf("candidate order changed for %s: %q", row.Name, row.Pacman)
				}
				if len(row.Tool.AURCandidates) > 0 && row.AUR != strings.Join(row.Tool.AURCandidates, ", ") {
					t.Errorf("fallback order changed for %s: %q", row.Name, row.AUR)
				}
			} else {
				references++
				if row.Reference != "Core" || row.Name != wantReferences[group] {
					t.Errorf("unexpected reference in %s: %+v", group, row)
				}
			}
		}
		wantReferenceCount := 0
		if wantReferences[group] != "" {
			wantReferenceCount = 1
		}
		if references != wantReferenceCount {
			t.Errorf("%s references: %d, want %d", group, references, wantReferenceCount)
		}
	}
	if len(seenRoles) != len(PackageManifest()) {
		t.Errorf("lost manifest roles: got %d, want %d", len(seenRoles), len(PackageManifest()))
	}
}

func TestPlanGroupOnlyOwnsItsSharedDependency(t *testing.T) {
	for _, c := range []struct{ group, name string }{{"development", "kubectl"}, {"containers", "Podman"}} {
		t.Run(c.group, func(t *testing.T) {
			o := Options{Command: "plan", Details: true, PackageGroup: c.group}
			rows := planTableRows(o, DefaultSettings(), c.group)
			found := false
			for _, row := range rows {
				if row.Reference != "" {
					t.Errorf("reference to an unselected group: %+v", row)
				}
				if row.Name == c.name {
					found = true
					if row.Pacman == "" || row.Pacman == "—" {
						t.Errorf("selected group's dependency has no package candidate: %+v", row)
					}
				}
			}
			if !found {
				t.Fatalf("missing %s", c.name)
			}
			for _, group := range GroupOrder {
				if group != c.group && len(planTableRows(o, DefaultSettings(), group)) != 0 {
					t.Errorf("unselected %s group still displayed", group)
				}
			}
		})
	}
}

func TestPlanNarrowTableKeepsPackageIdentifiersIntact(t *testing.T) {
	o := Options{Command: "plan", Details: true}
	s := DefaultSettings()
	stages, err := selectedStages(o, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{80, 100} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			text := planTextWidth(o, s, stages, false, width)
			for _, tool := range PackageManifest() {
				for _, candidate := range append(append([]string{}, tool.RepoCandidates...), tool.AURCandidates...) {
					if !strings.Contains(text, candidate) {
						t.Errorf("width %d split or lost package identity %q", width, candidate)
					}
				}
			}
			if strings.Contains(text, "\x1b") {
				t.Fatal("plain table has terminal escapes")
			}
			_, details, ok := strings.Cut(text, "Package options\n")
			if !ok {
				t.Fatal("package table heading missing")
			}
			details = strings.SplitN(details, "\nNext step", 2)[0]
			for _, line := range strings.Split(details, "\n") {
				if got := utf8.RuneCountInString(line); got > width {
					t.Errorf("package table overflows %d-column terminal (%d): %q", width, got, line)
				}
			}
		})
	}
}

func TestPlanDisabledGroupsAndConfigurationOnly(t *testing.T) {
	s := DefaultSettings()
	s.PackageGroups["utilities"] = false
	s.PackageGroups["containers"] = false
	text := previewForTest(t, Options{Command: "plan", Details: true}, s)
	if strings.Contains(text, "Utilities") || strings.Contains(text, "Podman Compose") {
		t.Fatal("disabled optional group still planned")
	}
	if !strings.Contains(text, "Podman") || !strings.Contains(text, "podman") {
		t.Fatal("core plugin backend removed with optional group")
	}
	text = previewForTest(t, Options{Command: "plan", ConfigureOnly: true}, s)
	if strings.Contains(text, "selected requirements") || strings.Contains(text, "Cleanup") {
		t.Fatal("configuration-only installs or cleans up")
	}
}

func TestPlanFollowupCommandsRetainOnlyRelevantFlags(t *testing.T) {
	o := Options{Command: "plan", All: true, Details: true, Stages: []string{"desktop"}, ConfigureOnly: true, SettingsPath: "/home/user/my settings.json", PackageGroup: "development", LaptopScale: "3/2", EnablePodmanSocket: true, KeepTerminal: true, RemoveNotes: []string{"gnote", "xpad"}}
	if got, want := planFollowupCommand(o, false), "./myarch-buildkit check --settings '/home/user/my settings.json'"; got != want {
		t.Errorf("check command\n got: %s\nwant: %s", got, want)
	}
	if got, want := planFollowupCommand(o, true), "./myarch-buildkit apply --stage verify --stage cleanup --settings '/home/user/my settings.json' --keep-terminal --remove-notes gnote --remove-notes xpad"; got != want {
		t.Errorf("cleanup command\n got: %s\nwant: %s", got, want)
	}
	if _, err := ParseOptions([]string{"check", "--settings", o.SettingsPath}); err != nil {
		t.Fatalf("check follow-up cannot run: %v", err)
	}
	if _, err := ParseOptions([]string{"apply", "--stage", "verify", "--stage", "cleanup", "--settings", o.SettingsPath, "--keep-terminal", "--remove-notes", "gnote", "--remove-notes", "xpad"}); err != nil {
		t.Fatalf("cleanup follow-up cannot run: %v", err)
	}
}

func TestPlanPackageGroupDoesNotHideConfigurationStages(t *testing.T) {
	text := previewForTest(t, Options{Command: "plan", PackageGroup: "development"}, DefaultSettings())
	if !strings.Contains(text, "Only the development package group") || !strings.Contains(text, "At your next myarch-buildkit login") {
		t.Fatal("package group was presented as whole-run scope")
	}
	text = previewForTest(t, Options{Command: "plan", Stages: []string{"packages"}, PackageGroup: "development"}, DefaultSettings())
	if strings.Contains(text, "At your next myarch-buildkit login") {
		t.Fatal("package-only run includes configuration")
	}
}

func TestPlanCommandRetainsFlagsAndQuotedPaths(t *testing.T) {
	o := Options{Command: "plan", Stages: []string{"desktop"}, SettingsPath: "/home/user/my settings.json", LaptopScale: "3/2"}
	command := planCommand(o, "apply", false)
	for _, value := range []string{"--stage desktop", "--settings '/home/user/my settings.json'", "--laptop-scale 3/2"} {
		if !strings.Contains(command, value) {
			t.Errorf("command lost %q", value)
		}
	}
	v := &planView{}
	v.command("Apply", command+" --keep-terminal --enable-podman-socket")
	if strings.Count(v.String(), "\n") != 1 {
		t.Fatal("shell command split into invalid lines")
	}
}

func TestDetailsFlagIsRestrictedToPreviewAndCheck(t *testing.T) {
	for _, args := range [][]string{{"plan", "--details"}, {"--dry-run", "--details"}, {"check", "--details"}} {
		if o, err := ParseOptions(args); err != nil || !o.Details {
			t.Fatalf("%v: %+v %v", args, o, err)
		}
	}
	for _, args := range [][]string{{"apply", "--details"}, {"--details"}} {
		if _, err := ParseOptions(args); err == nil {
			t.Fatalf("execution accepted preview option %v", args)
		}
	}
}

func TestPlanColorCanBeDisabledAndNeverLeaksIntoPlainOutput(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "1")
	if planHasColor() {
		t.Fatal("NO_COLOR ignored")
	}
	text := previewForTest(t, Options{Command: "plan"}, DefaultSettings())
	if strings.Contains(text, "\x1b") {
		t.Fatal("plain output contains terminal escapes")
	}
}
