package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

var runStageNames = map[string]string{
	"preflight":            "Preflight",
	"packages-core":        "Core packages",
	"packages-utilities":   "Utilities packages",
	"packages-development": "Development packages",
	"packages-containers":  "Container packages",
	"packages-optional":    "Optional packages",
	"defaults":             "App defaults",
	"shell":                "Shell",
	"containers":           "Containers",
	"laptop":               "Laptop settings",
	"desktop":              "Desktop",
	"greeter":              "Login screen",
	"verify":               "Verification",
	"cleanup":              "Cleanup",
}

func runStageLabel(name string) string {
	if label, ok := runStageNames[name]; ok {
		return label
	}
	label := strings.ReplaceAll(strings.ReplaceAll(name, "_", " "), "-", " ")
	if label == "" {
		return "Stage"
	}
	return strings.ToUpper(label[:1]) + label[1:]
}

func runStageStatus(status string) string {
	switch status {
	case "completed":
		return "Completed"
	case "failed":
		return "Failed"
	case "pending_login":
		return "Pending login"
	case "blocked":
		return "Blocked"
	case "skipped":
		return "Skipped"
	case "interrupted":
		return "Interrupted"
	case "running":
		return "Incomplete"
	case "applied_before_login":
		return "Applied before login"
	case "restored":
		return "Restored"
	case "restore_partial":
		return "Restoration incomplete"
	default:
		return "Unknown"
	}
}

func runResultStages(records map[string]any) []string {
	stages := []string{}
	seen := map[string]bool{}
	for _, stage := range StageOrder {
		names := []string{stage}
		if stage == "packages" {
			names = []string{"packages-core", "packages-utilities", "packages-development", "packages-containers", "packages-optional"}
		}
		for _, name := range names {
			if _, ok := records[name]; ok {
				stages = append(stages, name)
				seen[name] = true
			}
		}
	}
	// Retain unexpected stage results rather than silently omitting diagnostics.
	extra := []string{}
	for name := range records {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return append(stages, extra...)
}

func runResultValue(record map[string]any, key string) string {
	value, _ := record[key].(string)
	return strings.TrimSpace(value)
}

func runRecoveryCommands(record map[string]any) []string {
	commands := []string{}
	for _, command := range stringList(record["recovery_commands"]) {
		if command = strings.TrimSpace(command); command != "" {
			commands = append(commands, command)
		}
	}
	if len(commands) == 0 {
		if command := runResultValue(record, "recovery_command"); command != "" {
			commands = append(commands, command)
		}
	}
	return commands
}

// runResultsText renders saved machine results without changing their statuses.
// Commands and paths stay on one line so they can be copied exactly.
func runResultsText(c *Context, summary map[string]any) string {
	v := &planView{color: planHasColor()}
	fmt.Fprintln(&v.Builder, v.styled("myarch-buildkit", "1;36")+"  /  Setup results")
	message := "Selected stages completed"
	switch runResultValue(summary, "status") {
	case "pending_login":
		message = "Configuration awaits your next myarch-buildkit login"
	case "partial":
		message = "Setup needs attention"
	case "interrupted":
		message = "Setup interrupted"
	case "configuration_restored":
		message = "Configuration restored"
	}
	v.note(BuildID + " · " + message)
	records := object(summary["stages"])
	stages := runResultStages(records)
	v.section("Stages")
	fmt.Fprintf(&v.Builder, "  %-22s%s\n", "Stage", "Result")
	pending, failed := false, false
	for _, stage := range stages {
		record := object(records[stage])
		status := runResultValue(record, "status")
		pending = pending || status == "pending_login"
		failed = failed || status == "failed" || status == "interrupted" || status == "running"
		fmt.Fprintf(&v.Builder, "  %-22s%s\n", runStageLabel(stage), runStageStatus(status))
	}
	if len(stages) == 0 {
		v.note("No stage results were recorded.")
	}

	details := false
	for _, stage := range stages {
		record := object(records[stage])
		status := runResultValue(record, "status")
		errorText, reason := runResultValue(record, "error"), runResultValue(record, "reason")
		recovery := runRecoveryCommands(record)
		retry, restore := "", ""
		if status == "failed" || status == "interrupted" {
			retry = runResultValue(record, "retry")
		}
		if stage != "verify" && (status == "failed" || status == "pending_login" || status == "interrupted") {
			restore = runResultValue(record, "restore")
		}
		// Recovery refers to the original queued profile. A failed new stage's
		// restore command would instead target its empty run folder.
		if status == "failed" && len(recovery) > 0 {
			restore = ""
		}
		if errorText == "" && reason == "" && len(recovery) == 0 && retry == "" && restore == "" {
			continue
		}
		if !details {
			v.section("Details and actions")
			details = true
		}
		if errorText != "" {
			v.field(runStageLabel(stage), errorText)
		} else if reason != "" {
			v.field(runStageLabel(stage), reason)
		} else {
			v.field(runStageLabel(stage), runStageStatus(status))
		}
		if errorText != "" && reason != "" && reason != errorText {
			v.field("Reason", reason)
		}
		if len(recovery) > 0 {
			for index, command := range recovery {
				label := "Repair"
				if index > 0 {
					label = "Then"
				}
				v.command(label, command)
			}
		} else if retry != "" {
			v.command("Retry", retry)
		}
		if restore != "" {
			v.command("Restore/cancel", restore)
		}
	}
	if issue := runResultValue(summary, "error"); issue != "" {
		if !details {
			v.section("Details and actions")
		}
		v.field("Setup", issue)
	}

	pendingStatus := runResultValue(object(summary["pending_configuration"]), "status")
	pending = pending || pendingStatus == "pending" || pendingStatus == "blocked"
	if pending {
		v.section("Next steps")
		if pendingStatus == "blocked" {
			v.note("Pending configuration is blocked. Complete the repair steps before your next myarch-buildkit login.")
		} else if failed || runResultValue(summary, "status") == "partial" || runResultValue(summary, "status") == "interrupted" {
			v.note("Configuration is still pending. Resolve the failures before your next myarch-buildkit login.")
		} else {
			v.note("Save your work, log out, and select myarch-buildkit on the login screen.")
			if c != nil {
				args := []string{c.Binary, "check"}
				settings := runResultValue(summary, "settings_file")
				if settings == "" {
					settings = runResultValue(summary, "settings")
				}
				if settings != "" {
					args = append(args, "--settings", settings)
				}
				v.command("After login", planShellWords(args))
			}
		}
	}
	if c != nil && (c.ReportPath != "" || c.RunDir != "") {
		reportPath := c.ReportPath
		if reportPath == "" {
			reportPath = filepath.Join(c.RunDir, "report.json")
		}
		v.section("Report")
		fmt.Fprintln(&v.Builder, "  "+reportPath)
	}
	return v.String()
}
