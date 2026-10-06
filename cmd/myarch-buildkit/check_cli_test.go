package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureCLIOutput(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = previous; reader.Close(); writer.Close() }()
	output := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(reader); output <- data }()
	fn()
	writer.Close()
	os.Stdout = previous
	return string(<-output)
}

func TestCheckOutputOptionsAndCancellationStayScoped(t *testing.T) {
	for _, args := range [][]string{{"check", "--json"}, {"--check", "--json"}, {"check", "--details"}, {"--cancel-pending", "laptop"}, {"--cancel-pending", "desktop"}} {
		if _, err := ParseOptions(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{{"plan", "--json"}, {"apply", "--json"}, {"check", "--json", "--details"}, {"--cancel-pending", "shell"}, {"apply", "--cancel-pending", "laptop"}, {"--cancel-pending", "laptop", "--stage", "verify"}, {"--cancel-pending", "desktop", "--restore-profile", "/tmp/run"}, {"check", "--cancel-pending", "laptop"}} {
		if _, err := ParseOptions(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestJSONCheckIsOneDocumentAndContainsFailure(t *testing.T) {
	c := checkOutputContext()
	c.CheckOnly = true
	c.Report["status"] = "incomplete"
	c.Report["check_error"] = "lost staging data"
	output := captureCLIOutput(t, func() {
		c.Note("recorded note")
		c.Warn("recorded warning")
		code, err := printCheck(c, Options{Command: "check", JSON: true}, errors.New("lost staging data"))
		var shown *printedError
		if code != 2 || !errors.As(err, &shown) {
			t.Fatalf("failure lost: %d %v", code, err)
		}
	})
	report := map[string]any{}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("JSON contains extra console output: %v\n%s", err, output)
	}
	if report["check_error"] != "lost staging data" || len(stringList(report["notes"])) != 1 || len(stringList(report["warnings"])) != 1 {
		t.Fatalf("incomplete diagnostic report: %#v", report)
	}
}

func TestValidatedPendingCheckUsesExitThree(t *testing.T) {
	c := checkOutputContext()
	c.Report["status"] = "pending_login"
	c.Report["pending_configuration"] = map[string]any{"status": "pending"}
	output := captureCLIOutput(t, func() {
		if code, err := printCheck(c, Options{Command: "check"}, nil); code != 3 || err != nil {
			t.Fatalf("%d %v", code, err)
		}
	})
	if !strings.Contains(output, "Pending login") || strings.Contains(output, "Incomplete") || strings.HasPrefix(output, "{") {
		t.Fatal(output)
	}
}

func TestCheckRetainsMissingProfileDiagnosticWithoutMutation(t *testing.T) {
	c, runner := storageContext(t)
	c.Settings = DefaultSettings()
	c.Settings.PackageGroups["containers"] = false
	c.CheckOnly = true
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if err := writeQueue(c, []QueueItem{{Phase: "laptop", Manifest: manifestPath(c)}}); err != nil {
		t.Fatal(err)
	}
	before := storageRead(t, queuePath(c))
	var pending *PendingConfigurationError
	if err := checkAll(c); !errors.As(err, &pending) {
		t.Fatalf("missing structured failure: %v", err)
	}
	for _, section := range []string{"tooling", "defaults", "profile"} {
		if object(object(c.Report["check_results"])[section])["status"] == nil {
			t.Errorf("check result missing: %s", section)
		}
	}
	if object(c.Report["pending_configuration"])["status"] != "blocked" || object(c.Report["running_desktop"])["status"] != "not_observed" || c.Report["check_error"] == nil {
		t.Fatalf("failure not retained in report: %#v", c.Report)
	}
	if storageRead(t, queuePath(c)) != before || exists(c.RunDir) {
		t.Fatal("read-only check changed staging data")
	}
	for _, cmd := range runner.calls {
		if len(cmd.Args) > 0 && cmd.Args[0] == "sudo" {
			t.Fatal("read-only check requested privileges")
		}
	}
}

func TestVerifyOnlyPreservesInstalledExecutable(t *testing.T) {
	c := mainStageContext(t)
	c.Binary = filepath.Join(c.Home, ".local", "bin", "myarch-buildkit")
	storagePut(t, c.Binary, "existing executable")
	mainStageSeam(t, func(*Context, string, string, bool, []string) error { return nil })
	code, err := RunStages(c, []string{"preflight", "verify"})
	if code != 0 || err != nil {
		t.Fatalf("%d %v", code, err)
	}
	if storageRead(t, c.Binary) != "existing executable" {
		t.Fatal("verification replaced installed executable")
	}
}

func TestBrokenPendingQueueStillFinalizesRunResults(t *testing.T) {
	c := mainStageContext(t)
	storagePut(t, queuePath(c), "broken queue")
	mainStageSeam(t, func(*Context, string, string, bool, []string) error { return nil })
	code, err := RunStages(c, []string{"preflight", "verify", "cleanup"})
	if code != 2 || err == nil {
		t.Fatalf("%d %v", code, err)
	}
	summary := mainStageSummary(t, c)
	if summary["status"] != "partial" || mainStageStatus(summary, "cleanup") != "blocked" || object(summary["pending_configuration"])["status"] != "blocked" {
		t.Fatalf("unfinished diagnostic summary: %#v", summary)
	}
}
