package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// checkText presents observations separately from checks that actually passed.
// The JSON report remains the source for complete diagnostic data.
func checkText(o Options, c *Context, checkErr error) string {
	v := &planView{}
	report := c.Report
	fmt.Fprintln(&v.Builder, "myarch-buildkit  /  System check")
	v.note(BuildID + " · Read-only")
	status := "Incomplete — resolve the items below"
	if checkErr == nil {
		if report["status"] == "verified" {
			status = "Passed"
		}
		if (report["status"] == "verified" || report["status"] == "pending_login") && object(report["pending_configuration"])["status"] == "pending" {
			status = "Pending login — staged files validated"
		}
	}
	v.field("Result", status)

	v.section("Checks")
	tooling := object(report["tooling_checks"])
	v.field("Packages & tools", checkState(report, "tooling", tooling["success"]))
	v.field("App defaults", checkState(report, "defaults", object(report["defaults_check_result"])["success"]))
	containers := object(report["containers"])
	if !c.Settings.PackageGroups["containers"] && len(containers) == 0 {
		v.field("Containers", "Not selected")
	} else {
		v.field("Containers", checkState(report, "containers", containers["ready"])+" · static prerequisites")
		v.note("Container execution and effective runtime provider were not tested.")
	}
	if o.Details {
		checkContainerDetails(v, containers)
	}

	defaults := checkDefaultsMap(report["defaults_check"])
	if len(defaults) > 0 {
		v.section("App defaults")
		for _, item := range [][2]string{{"Terminal", "terminal"}, {"Files", "inode/directory"}, {"Text files", "text/plain"}, {"Markdown", "text/markdown"}, {"PDF", "application/pdf"}, {"Audio", "audio/mpeg"}, {"Video", "video/mp4"}, {"Notes links", "x-scheme-handler/obsidian"}, {"Font", "font"}} {
			value, present := defaults[item[1]]
			if !present {
				continue
			}
			if item[1] == "font" {
				value = strings.Split(value, ",")[0]
			} else {
				value = checkAppName(value)
			}
			if value == "" {
				value = "Not set"
			}
			v.field(item[0], value)
			if o.Details && item[1] != "font" && defaults[item[1]] != "" {
				v.note(defaults[item[1]])
			}
		}
		if o.Details && defaults["terminal_command"] != "" {
			v.field("Terminal command", strings.ReplaceAll(defaults["terminal_command"], "\n", " "))
		}
	}

	pending := object(report["pending_configuration"])
	v.section("Pending setup")
	switch pending["status"] {
	case "none":
		v.field("Status", "No staged configuration")
	case "pending":
		v.field("Status", "Validated · applies at the next myarch-buildkit login")
	case "blocked":
		v.field("Status", "Blocked · staged configuration could not be validated")
	default:
		v.field("Status", "Not checked")
	}
	if o.Details {
		for _, item := range checkQueueItems(pending["items"]) {
			v.field(item.Phase, item.Manifest)
		}
		if manifest, ok := pending["manifest"].(string); ok && manifest != "" {
			v.field("Manifest", manifest)
		}
	}

	running := object(report["running_desktop"])
	v.section("Running desktop")
	observationOnly := running["status"] == "observed" || running["status"] == "not_observed"
	switch {
	case running["status"] == "not_observed":
		v.field("Hyprland + DMS", "Not observed")
		if reason, ok := running["reason"].(string); ok && reason != "" {
			v.note(reason)
		}
	case !observationOnly && running["ready"] == true:
		v.field("Hyprland + DMS", "Passed · running session verified")
	case !observationOnly && running["ready"] == false:
		v.field("Hyprland + DMS", "Needs attention")
	case running["dms_responding"] == true:
		v.field("DMS", "Responding · full runtime checks not completed")
	case running["dms_responding"] == false || running["dms_ipc_exit_code"] != nil:
		v.field("DMS", "Not responding · full runtime checks not completed")
	default:
		v.field("Hyprland + DMS", "Not checked")
	}
	if !observationOnly && running["ready"] != nil {
		laptop := object(report["laptop"])
		v.field("Laptop settings", checkObservedState(laptop["ready"]))
	} else if observationOnly {
		v.field("Laptop settings", "Not checked · observation only")
	}
	if o.Details {
		if method, ok := running["startup_method"].(string); ok && method != "" {
			v.field("DMS startup", method)
		}
		plugins := object(running["plugins"])
		for _, plugin := range RequestedPlugins {
			if value, ok := plugins[plugin.ID].(string); ok {
				v.field(checkPluginName(plugin.ID), value)
			}
		}
	}

	issues := checkIssues(report, checkErr)
	if len(issues) > 0 {
		v.section("Needs attention")
		for _, issue := range issues {
			v.note(issue)
		}
	}
	v.section("Next steps")
	if pending["status"] == "pending" {
		v.note("Save your work, log out, and select myarch-buildkit at login.")
		v.command("Then check", planFollowupCommand(o, false))
	} else if pending["status"] == "blocked" {
		v.note("Keep cleanup blocked until the staged configuration is repaired.")
		if commands := stringList(pending["recovery_commands"]); len(commands) > 0 {
			v.note("Restore missing files from the original setup run, or use the explicit recovery command below.")
			for _, command := range commands {
				v.command("Recovery", command)
			}
		} else {
			v.note("Use the reported paths to repair the staged files, or explicitly restore the original setup run.")
		}
		v.command("Then check", planFollowupCommand(o, false))
	} else if checkErr == nil && report["status"] == "verified" {
		v.note("Checks passed. Cleanup can now run if you want to remove duplicates.")
		v.command("Cleanup", planFollowupCommand(o, true))
	} else {
		v.note("Resolve the reported issues, then run the check again.")
		v.command("Check", planFollowupCommand(o, false))
	}
	if !o.Details {
		v.command("Details", checkOutputCommand(o, "--details"))
	}
	v.command("JSON report", checkOutputCommand(o, "--json"))
	return v.String()
}

func checkState(report map[string]any, name string, observed any) string {
	result := object(object(report["check_results"])[name])
	switch result["status"] {
	case "passed":
		return "Passed"
	case "failed":
		return "Needs attention"
	}
	return checkObservedState(observed)
}

func checkObservedState(observed any) string {
	if observed == true {
		return "Passed"
	}
	if observed == false {
		return "Needs attention"
	}
	return "Not checked"
}

func checkDefaultsMap(value any) map[string]string {
	if values, ok := value.(map[string]string); ok {
		return values
	}
	values := map[string]string{}
	for key, value := range object(value) {
		if text, ok := value.(string); ok {
			values[key] = text
		}
	}
	return values
}

func checkAppName(id string) string {
	if id == "" {
		return ""
	}
	names := map[string]string{
		"com.mitchellh.ghostty.desktop": "Ghostty", "org.gnome.Nautilus.desktop": "GNOME Files",
		"com.microsoft.VSCode.desktop": "VS Code", "code.desktop": "VS Code", "visual-studio-code.desktop": "VS Code",
		MarkdownDesktop: "Obsidian", "obsidian.desktop": "Obsidian", "firefox.desktop": "Firefox", "vlc.desktop": "VLC",
	}
	if name, ok := names[id]; ok {
		return name
	}
	return strings.TrimSuffix(filepath.Base(id), ".desktop")
}

func checkPluginName(id string) string {
	names := map[string]string{"dockerManager": "Docker Manager", "kubernetes": "Kubernetes", "emojiLauncher": "Emoji & Unicode", "bongoCat": "Bongo Cat", "clipboardPlus": "ClipBoard+"}
	if name, ok := names[id]; ok {
		return name
	}
	return id
}

func checkQueueItems(value any) []QueueItem {
	if items, ok := value.([]QueueItem); ok {
		return items
	}
	items := []QueueItem{}
	for _, item := range objects(value) {
		phase, _ := item["phase"].(string)
		manifest, _ := item["manifest"].(string)
		items = append(items, QueueItem{Phase: phase, Manifest: manifest})
	}
	return items
}

func checkIssues(report map[string]any, checkErr error) []string {
	issues := []string{}
	add := func(message string) {
		for _, line := range strings.Split(message, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !contains(issues, line) {
				issues = append(issues, line)
			}
		}
	}
	for _, name := range []string{"tooling", "defaults", "containers", "profile"} {
		if message, ok := object(object(report["check_results"])[name])["error"].(string); ok {
			add(message)
		}
	}
	for _, message := range stringList(object(report["tooling_checks"])["failures"]) {
		add(message)
	}
	for _, message := range stringList(object(report["running_desktop"])["failures"]) {
		add(message)
	}
	for _, key := range []string{"hyprland_error", "error"} {
		if message, ok := object(report["running_desktop"])[key].(string); ok {
			add(message)
		}
	}
	for _, message := range stringList(object(report["laptop"])["pending"]) {
		add(message)
	}
	for _, message := range stringList(report["warnings"]) {
		add(message)
	}
	if message, ok := object(report["pending_configuration"])["error"].(string); ok {
		add(message)
	}
	if checkErr != nil {
		add(checkErr.Error())
	}
	return issues
}

func checkContainerDetails(v *planView, containers map[string]any) {
	if len(containers) == 0 {
		return
	}
	if provider, ok := containers["compose_provider"].(string); ok {
		v.field("Compose provider", provider)
	}
	v.field("Provider setup", checkObservedState(containers["compose_provider_configured"]))
	v.field("Executable", checkObservedState(containers["compose_provider_executable"]))
	readiness := object(containers["readiness"])
	if len(readiness) > 0 {
		v.field("cgroup v2", checkObservedState(readiness["cgroup_v2"]))
		v.field("Subordinate UIDs", checkObservedState(object(readiness["subuids"])["ready"]))
		v.field("Subordinate GIDs", checkObservedState(object(readiness["subgids"])["ready"]))
	}
	for _, key := range []string{"configuration_overrides", "later_provider_overrides", "dropin_inspection_errors"} {
		values := append([]string{}, stringList(containers[key])...)
		sort.Strings(values)
		for _, value := range values {
			v.field("Provider override", value)
		}
	}
}

func checkOutputCommand(o Options, flag string) string {
	args := []string{"./myarch-buildkit", "check", flag}
	if o.SettingsPath != "" {
		args = append(args, "--settings", o.SettingsPath)
	}
	return planShellWords(args)
}
