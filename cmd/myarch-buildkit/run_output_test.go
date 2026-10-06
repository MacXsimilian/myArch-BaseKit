package main

import (
	"strings"
	"testing"
)

func TestRunResultsShowFailureReasonAndRepairInsteadOfRetry(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	command := "'/home/example user/.local/bin/myarch-buildkit' apply --stage laptop --stage desktop --settings '/home/example user/run/effective-settings.json'"
	text := runResultsText(&Context{RunDir: "/home/example user/run"}, map[string]any{
		"status": "partial",
		"stages": map[string]any{
			"preflight": map[string]any{"status": "completed"},
			"verify":    map[string]any{"status": "failed", "error": "Pending laptop configuration is missing.", "recovery_command": command, "retry": "wrong-verify-only-retry"},
			"cleanup":   map[string]any{"status": "blocked", "reason": "Verification must pass before cleanup."},
		},
		"pending_configuration": map[string]any{"status": "pending"},
	})
	for _, expected := range []string{"Setup results", "Setup needs attention", "Preflight", "Completed", "Verification", "Failed", "Cleanup", "Blocked", "Pending laptop configuration is missing.", "Verification must pass before cleanup.", "Repair", command, "/home/example user/run", "Resolve the failures"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q in:\n%s", expected, text)
		}
	}
	for _, unexpected := range []string{"wrong-verify-only-retry", "log out, and select", "pending_login", "\x1b[", "\"schema_version\"", "<nil>"} {
		if strings.Contains(text, unexpected) {
			t.Errorf("unexpected %q in:\n%s", unexpected, text)
		}
	}
	if !strings.Contains(text, "Repair            "+command+"\n") {
		t.Error("repair command was wrapped or changed")
	}
}

func TestRunResultsRetainRetryRestoreAndAllStatusesInStageOrder(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	summary := map[string]any{
		"status": "interrupted",
		"error":  "Setup stopped before every stage completed.",
		"stages": map[string]any{
			"cleanup":              map[string]any{"status": "blocked", "reason": "Desktop staging did not complete."},
			"verify":               map[string]any{"status": "skipped", "reason": "An earlier stage failed."},
			"desktop":              map[string]any{"status": "interrupted", "error": "Interrupted by user.", "retry": "myarch-buildkit apply --stage desktop", "restore": "myarch-buildkit --restore-stage desktop --stage-run-dir /run/desktop"},
			"laptop":               map[string]any{"status": "pending_login", "restore": "myarch-buildkit --restore-stage laptop --stage-run-dir /run/laptop"},
			"packages-development": map[string]any{"status": "completed"},
			"packages-core":        map[string]any{"status": "completed"},
		},
	}
	text := runResultsText(nil, summary)
	for _, expected := range []string{"Setup interrupted", "Core packages", "Development packages", "Laptop settings", "Pending login", "Interrupted", "Skipped", "Blocked", "Interrupted by user.", "An earlier stage failed.", "Desktop staging did not complete.", "Retry", "myarch-buildkit apply --stage desktop", "Restore/cancel", "myarch-buildkit --restore-stage laptop --stage-run-dir /run/laptop", "Setup stopped before every stage completed."} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q", expected)
		}
	}
	previous := -1
	for _, stage := range []string{"Core packages", "Development packages", "Laptop settings", "Desktop", "Verification", "Cleanup"} {
		position := strings.Index(text, stage)
		if position <= previous {
			t.Errorf("%s was not in stage order", stage)
		}
		previous = position
	}
	if strings.Contains(text, "pending_login") || strings.Contains(text, "<nil>") {
		t.Error("machine status or missing values leaked")
	}
}

func TestRunResultsPendingLoginInstructionsKeepSettingsCommandIntact(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	c := &Context{Binary: "/home/example user/.local/bin/myarch-buildkit", RunDir: "/home/example user/run"}
	summary := map[string]any{
		"status":   "pending_login",
		"settings": "/home/example user/run/effective-settings.json",
		"stages": map[string]any{
			"desktop": map[string]any{"status": "pending_login"},
			"cleanup": map[string]any{"status": "blocked", "reason": "Awaiting myarch-buildkit login."},
		},
	}
	text := runResultsText(c, summary)
	command := "'/home/example user/.local/bin/myarch-buildkit' check --settings '/home/example user/run/effective-settings.json'"
	for _, expected := range []string{"Configuration awaits your next myarch-buildkit login", "Pending login", "Save your work, log out, and select myarch-buildkit", "After login", command, "Report"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q", expected)
		}
	}
	if !strings.Contains(text, command+"\n") {
		t.Error("check command was wrapped or changed")
	}
}

func TestRunResultsUseConsolidatedReportForFollowupAndReportPath(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	c := &Context{Binary: "/home/example user/.local/bin/myarch-buildkit", RunDir: "/home/example user/run", ReportPath: "/home/example user/run/report.json"}
	text := runResultsText(c, map[string]any{
		"status":        "pending_login",
		"settings_file": c.ReportPath,
		"settings":      "/legacy/effective-settings.json",
		"stages":        map[string]any{"desktop": map[string]any{"status": "pending_login"}},
	})
	command := "'/home/example user/.local/bin/myarch-buildkit' check --settings '/home/example user/run/report.json'"
	if !strings.Contains(text, command+"\n") {
		t.Error("followup did not use the consolidated report path intact")
	}
	if !strings.Contains(text, "Report\n  "+c.ReportPath+"\n") {
		t.Error("report path did not name the report file")
	}
	if strings.Contains(text, "/legacy/effective-settings.json") || strings.Contains(text, "Reports and backups") {
		t.Error("obsolete settings or directory label leaked")
	}
}

func TestRunResultsCompletedRunOmitsUnrelatedActions(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	text := runResultsText(&Context{RunDir: "/run/setup"}, map[string]any{
		"status": "completed_with_manual_checks",
		"stages": map[string]any{"shell": map[string]any{"status": "completed", "retry": "unused-retry", "restore": "unused-restore"}},
	})
	for _, expected := range []string{"Selected stages completed", "Shell", "Completed", "/run/setup"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q", expected)
		}
	}
	for _, unexpected := range []string{"Next steps", "Details and actions", "unused-retry", "unused-restore", "<nil>", "completed_with_manual_checks"} {
		if strings.Contains(text, unexpected) {
			t.Errorf("unrelated action %q", unexpected)
		}
	}
}

func TestRunResultsRecoveryCommandsAreOrderedAndTargetOriginalProfile(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	first := "myarch-buildkit --restore-stage desktop --stage-run-dir '/home/example user/original/desktop'"
	second := "myarch-buildkit --cancel-pending laptop"
	text := runResultsText(nil, map[string]any{
		"status":                "partial",
		"error":                 "Pending configuration could not be inspected.",
		"pending_configuration": map[string]any{"status": "blocked"},
		"stages": map[string]any{
			"desktop": map[string]any{
				"status":            "failed",
				"error":             "The original laptop staging manifest is missing.",
				"recovery_command":  "superseded-singular-command",
				"recovery_commands": []string{first, second},
				"retry":             "misleading-desktop-retry",
				"restore":           "wrong-new-empty-stage-restore",
			},
			"verify": map[string]any{"status": "failed", "restore": "wrong-verify-stage-restore"},
		},
	})
	if strings.Index(text, first) < 0 || strings.Index(text, second) <= strings.Index(text, first) {
		t.Error("recovery commands were missing or out of order")
	}
	for _, command := range []string{first, second} {
		if !strings.Contains(text, command+"\n") {
			t.Errorf("command %q was wrapped or changed", command)
		}
	}
	for _, unexpected := range []string{"superseded-singular-command", "misleading-desktop-retry", "wrong-new-empty-stage-restore", "wrong-verify-stage-restore", "log out, and select"} {
		if strings.Contains(text, unexpected) {
			t.Errorf("misleading action %q", unexpected)
		}
	}
	for _, expected := range []string{"Pending configuration could not be inspected.", "Pending configuration is blocked.", "Complete the repair steps"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q", expected)
		}
	}
}
