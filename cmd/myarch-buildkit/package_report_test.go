package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageResultsAreSavedInReportWithoutExtraArtifacts(t *testing.T) {
	versions := "podman 5.0.0\nquickshell 0.2.0\n"
	runner := &packageTestRunner{reply: func(command Command) (CommandResult, error) {
		if len(command.Args) > 4 && command.Args[0] == "pacman" && command.Args[1] == "-Q" {
			return CommandResult{Stdout: versions}, nil
		}
		return packageReplyAllRepo(command)
	}}
	c := packageTestContext(t, runner)
	if err := InstallPackages(c, "core", true); err != nil {
		t.Fatal(err)
	}
	report := map[string]any{}
	if err := ReadJSON(c.ReportPath, &report); err != nil {
		t.Fatal(err)
	}
	if report["installed_tool_versions"] != versions {
		t.Fatal("installed version output changed or was omitted")
	}
	rows := reportRecords(report, "resolved_packages")
	tools := selectedTools(c, "core")
	if len(rows) != len(tools) {
		t.Fatalf("resolved tool rows %d, want %d", len(rows), len(tools))
	}
	for index, row := range rows {
		if row["label"] != tools[index].Label || row["source"] != "repo" || row["package"] != tools[index].RepoCandidates[0] {
			t.Fatalf("wrong selected package row: %v", row)
		}
	}
	if len(reportRecords(report, "file_changes")) != 0 {
		t.Fatal("report artifacts were recorded as configuration changes")
	}
	for _, name := range []string{"resolved-packages.tsv", "installed-tool-versions.txt", "backups"} {
		if _, err := os.Stat(filepath.Join(c.RunDir, name)); !os.IsNotExist(err) {
			t.Errorf("unexpected reporting artifact %s: %v", name, err)
		}
	}
}
