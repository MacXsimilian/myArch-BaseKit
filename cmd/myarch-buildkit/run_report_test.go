package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunCreatesOneVisibleReportWithSettingsResultsAndBackups(t *testing.T) {
	c := mainStageContext(t)
	paths := map[string]string{}
	mainStageSeam(t, func(child *Context, stage, _ string, _ bool, _ []string) error {
		if stage == "defaults" || stage == "shell" {
			target := filepath.Join(child.Home, stage+".conf")
			paths[stage] = target
			storagePut(t, target, "before "+stage)
			return child.Write(target, []byte("after "+stage), 0600)
		}
		child.Report["settings_validation"] = "passed"
		return nil
	})
	if code, err := RunStages(c, []string{"preflight", "defaults", "shell"}); code != 0 || err != nil {
		t.Fatalf("%d %v", code, err)
	}
	entries, err := os.ReadDir(c.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "report.json" || entries[0].IsDir() {
		t.Fatalf("run output still scattered: %v", entries)
	}
	settings, err := LoadSettings(c.ReportPath)
	if err != nil || !reflect.DeepEqual(settings, c.Settings) {
		t.Fatalf("retry settings snapshot changed: %+v %v", settings, err)
	}
	document := mainStageSummary(t, c)
	if document["format"] != RunReportFormat || len(object(document["stages"])) != 3 || len(object(document["profiles"])) != 4 {
		t.Fatalf("incomplete consolidated report: %#v", document)
	}
	for _, stage := range []string{"defaults", "shell"} {
		profile := map[string]any{}
		if err := ReadProfileReport(c.ReportPath, stage, &profile); err != nil {
			t.Fatal(err)
		}
		changes := reportRecords(profile, "file_changes")
		if len(changes) != 1 || changes[0]["path"] != paths[stage] {
			t.Fatalf("stage journal lost: %s %#v", stage, profile)
		}
		backup, _ := changes[0]["backup"].(string)
		if !inside(filepath.Join(stateRoot(c), "runs"), backup) || storageRead(t, backup) != "before "+stage {
			t.Fatalf("backup not retained in internal state: %s", backup)
		}
	}
}

func TestFailedRunKeepsReportAndRetryUsesItsSettingsSnapshot(t *testing.T) {
	c := mainStageContext(t)
	mainStageSeam(t, func(child *Context, stage, _ string, _ bool, _ []string) error {
		if stage == "shell" {
			return os.ErrPermission
		}
		return nil
	})
	if code, err := RunStages(c, []string{"preflight", "shell", "verify", "cleanup"}); code != 2 || err == nil {
		t.Fatalf("%d %v", code, err)
	}
	document := mainStageSummary(t, c)
	stage := object(object(document["stages"])["shell"])
	if document["status"] != "partial" || object(object(document["profiles"])["shell"])["status"] != "failed" || !strings.Contains(str(stage["retry"]), "--settings "+c.ReportPath) || !strings.Contains(str(stage["restore"]), "--stage-run-dir "+c.RunDir) {
		t.Fatalf("failure/recovery metadata wrong: %#v", document)
	}
	if _, err := LoadSettings(c.ReportPath); err != nil {
		t.Fatalf("failed-run retry settings not readable: %v", err)
	}
}
