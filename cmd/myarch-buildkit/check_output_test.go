package main

import (
	"errors"
	"strings"
	"testing"
)

func checkOutputContext() *Context {
	return &Context{Settings: DefaultSettings(), Report: map[string]any{
		"status": "verified",
		"check_results": map[string]any{
			"tooling": map[string]any{"status": "passed"}, "defaults": map[string]any{"status": "passed"},
			"containers": map[string]any{"status": "passed"}, "profile": map[string]any{"status": "passed"},
		},
		"tooling_checks":        map[string]any{"success": true, "failures": []string{}},
		"defaults_check":        map[string]string{"terminal": "com.mitchellh.ghostty.desktop", "inode/directory": "org.gnome.Nautilus.desktop", "text/plain": "com.microsoft.VSCode.desktop", "text/markdown": MarkdownDesktop, "application/pdf": "firefox.desktop", "audio/mpeg": "vlc.desktop", "video/mp4": "vlc.desktop", "font": "JetBrainsMono Nerd Font Mono,JetBrainsMono NFM"},
		"containers":            map[string]any{"ready": true, "compose_provider": "/usr/bin/podman-compose", "compose_provider_configured": true, "compose_provider_executable": true, "readiness": map[string]any{"cgroup_v2": true, "subuids": map[string]any{"ready": true}, "subgids": map[string]any{"ready": true}}},
		"pending_configuration": map[string]any{"status": "none"},
		"running_desktop":       map[string]any{"ready": true, "startup_method": "Hyprland start hook -> dms run", "plugins": map[string]any{"dockerManager": "loaded", "bongoCat": "loaded"}},
		"laptop":                map[string]any{"ready": true},
	}}
}

func TestCheckOutputUsesHumanNamesAndStaticScope(t *testing.T) {
	output := checkText(Options{}, checkOutputContext(), nil)
	for _, text := range []string{"myarch-buildkit  /  System check", "Result            Passed", "Ghostty", "GNOME Files", "VS Code", "Obsidian", "Firefox", "VLC", "static prerequisites", "Container execution and effective runtime provider were not tested.", "No staged configuration", "running session verified"} {
		if !strings.Contains(output, text) {
			t.Errorf("missing %q in:\n%s", text, output)
		}
	}
	for _, text := range []string{".desktop", "\"schema_version\"", "100000", "58370"} {
		if strings.Contains(output, text) {
			t.Errorf("technical detail %q leaked into default output", text)
		}
	}
}

func TestCheckOutputAbsentSectionsAreNotPassed(t *testing.T) {
	c := &Context{Settings: DefaultSettings(), Report: map[string]any{"status": "incomplete"}}
	output := checkText(Options{}, c, errors.New("missing path: /home/user/run/stages/laptop/pending-profile.json"))
	if strings.Contains(output, "Passed") || strings.Contains(output, "Cleanup can now run") {
		t.Fatalf("absent observations were presented as successful:\n%s", output)
	}
	for _, text := range []string{"Not checked", "Needs attention", "missing path: /home/user/run/stages/laptop/pending-profile.json"} {
		if !strings.Contains(output, text) {
			t.Errorf("missing %q:\n%s", text, output)
		}
	}
}

func TestCheckOutputPendingAndRunningRemainSeparate(t *testing.T) {
	c := checkOutputContext()
	c.Report["status"] = "pending_login"
	c.Report["pending_configuration"] = map[string]any{"status": "pending", "items": []QueueItem{{Phase: "laptop", Manifest: "/home/u/run/laptop/pending-profile.json"}}}
	c.Report["running_desktop"] = map[string]any{"status": "observed", "dms_ipc_exit_code": 0, "dms_responding": true}
	output := checkText(Options{}, c, nil)
	for _, text := range []string{"Pending login", "Pending setup", "Validated", "Running desktop", "Responding · full runtime checks not completed", "Laptop settings   Not checked · observation only", "select myarch-buildkit at login"} {
		if !strings.Contains(output, text) {
			t.Errorf("missing %q:\n%s", text, output)
		}
	}
	if strings.Contains(output, "running session verified") || strings.Contains(output, "Cleanup can now run") || strings.Contains(output, "Incomplete") {
		t.Fatal("pending profile was presented as running")
	}
}

func TestCheckOutputMalformedIPCAndObservationErrorsAreVisible(t *testing.T) {
	c := checkOutputContext()
	c.Report["status"] = "pending_login"
	c.Report["pending_configuration"] = map[string]any{"status": "pending"}
	c.Report["running_desktop"] = map[string]any{"status": "observed", "dms_ipc_exit_code": 0, "dms_ipc_mode": "malformed", "dms_responding": false, "hyprland_error": "unrecognized Hyprland configuration-error response"}
	output := checkText(Options{}, c, nil)
	if strings.Contains(output, "DMS               Responding") || strings.Contains(output, "running session verified") {
		t.Fatalf("malformed successful IPC response was treated as working:\n%s", output)
	}
	for _, text := range []string{"DMS               Not responding", "unrecognized Hyprland configuration-error response", "Laptop settings   Not checked"} {
		if !strings.Contains(output, text) {
			t.Errorf("missing %q:\n%s", text, output)
		}
	}
	c.Report["running_desktop"] = map[string]any{"status": "not_observed", "reason": "Open a terminal in your Hyprland session."}
	output = checkText(Options{}, c, nil)
	if !strings.Contains(output, "Not observed") || !strings.Contains(output, "Open a terminal in your Hyprland session.") {
		t.Fatalf("unobserved reason omitted:\n%s", output)
	}
	c.Report["running_desktop"] = map[string]any{"ready": false, "error": "active Hyprland config does not exist"}
	output = checkText(Options{}, c, nil)
	if !strings.Contains(output, "active Hyprland config does not exist") {
		t.Fatalf("session inspection error omitted:\n%s", output)
	}
}

func TestCheckOutputBlockedProfileShowsRecoveryAndErrors(t *testing.T) {
	c := checkOutputContext()
	c.Report["status"] = "incomplete"
	message := "missing path: /home/u/run/stages/laptop/pending-profile.json"
	c.Report["pending_configuration"] = map[string]any{"status": "blocked", "error": message, "recovery_commands": []string{"./myarch-buildkit --cancel-pending laptop"}}
	c.Report["check_results"] = map[string]any{"profile": map[string]any{"status": "failed", "error": message}}
	delete(c.Report, "running_desktop")
	output := checkText(Options{}, c, errors.New(message))
	for _, text := range []string{"Blocked", "Keep cleanup blocked", "Restore missing files", "./myarch-buildkit --cancel-pending laptop", message} {
		if !strings.Contains(output, text) {
			t.Errorf("missing %q:\n%s", text, output)
		}
	}
	if strings.Count(output, message) != 1 {
		t.Errorf("same diagnostic repeated:\n%s", output)
	}
}

func TestCheckOutputDetailsExposeObservationsAndCommandsPreserveSettings(t *testing.T) {
	c := checkOutputContext()
	o := Options{Details: true, SettingsPath: "/home/user/My Settings/setup.json", Stages: []string{"cleanup"}, KeepTerminal: true}
	output := checkText(o, c, nil)
	for _, text := range []string{"com.mitchellh.ghostty.desktop", "/usr/bin/podman-compose", "Subordinate UIDs", "Hyprland start hook -> dms run", "Docker Manager", "Bongo Cat", "--settings '/home/user/My Settings/setup.json'"} {
		if !strings.Contains(output, text) {
			t.Errorf("missing %q:\n%s", text, output)
		}
	}
	command := checkOutputCommand(o, "--json")
	if strings.Contains(command, "--keep-terminal") || strings.Contains(command, "--stage") {
		t.Fatalf("apply flags leaked into check command: %s", command)
	}
}

func TestCheckOutputEmptyDefaultAndFailedProbeRemainVisible(t *testing.T) {
	c := checkOutputContext()
	c.Report["defaults_check"] = map[string]any{"terminal": "", "text/plain": "other-editor.desktop"}
	c.Report["check_results"] = map[string]any{"defaults": map[string]any{"status": "failed", "error": "Ghostty terminal default"}}
	c.Report["status"] = "incomplete"
	output := checkText(Options{}, c, errors.New("Ghostty terminal default"))
	for _, text := range []string{"App defaults      Needs attention", "Terminal          Not set", "other-editor", "Ghostty terminal default"} {
		if !strings.Contains(output, text) {
			t.Errorf("missing %q:\n%s", text, output)
		}
	}
}
