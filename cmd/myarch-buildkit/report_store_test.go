package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func unifiedReportFixture(t *testing.T, c *Context, scopes ...string) string {
	t.Helper()
	root := c.RunDir
	path := filepath.Join(root, "report.json")
	stages := map[string]any{}
	for _, scope := range scopes {
		stages[scope] = map[string]any{"status": "completed", "artifacts": filepath.Join(stateRoot(c), "runs", filepath.Base(root), scope)}
	}
	if err := WriteJSON(path, map[string]any{"format": RunReportFormat, "schema_version": RunReportSchemaVersion, "settings": DefaultSettings(), "stages": stages, "profiles": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUnifiedReportInterleavedWritesKeepSummaryAndEveryProfile(t *testing.T) {
	c, _ := storageContext(t)
	path := unifiedReportFixture(t, c, "laptop", "desktop")
	summary := map[string]any{"status": "running", "stages": map[string]any{"laptop": map[string]any{"status": "pending_login"}}, "profiles": map[string]any{}}
	if err := WriteProfileReport(path, "laptop", map[string]any{"schema_version": 1, "status": "pending_login", "note": "original laptop"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteRunSummary(path, summary); err != nil {
		t.Fatal(err)
	}
	if err := WriteProfileReport(path, "desktop", map[string]any{"schema_version": 1, "status": "applied"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteProfileReport(path, "laptop", map[string]any{"schema_version": 1, "status": "applied", "note": "login update"}); err != nil {
		t.Fatal(err)
	}
	summary["status"] = "pending_login"
	if err := WriteRunSummary(path, summary); err != nil {
		t.Fatal(err)
	}
	document := map[string]any{}
	if err := ReadJSON(path, &document); err != nil {
		t.Fatal(err)
	}
	if document["status"] != "pending_login" || object(object(document["profiles"])["laptop"])["note"] != "login update" || object(object(document["profiles"])["desktop"])["status"] != "applied" {
		t.Fatalf("summary save replaced another journal: %#v", document)
	}
	if document["format"] != RunReportFormat || mapJSONNumber(document["schema_version"]) != RunReportSchemaVersion || object(document["settings"])["schema_version"] == nil {
		t.Fatal("document metadata was lost")
	}
}

func TestUnifiedReportConcurrentScopesDoNotReplaceEachOther(t *testing.T) {
	c, _ := storageContext(t)
	path := unifiedReportFixture(t, c)
	scopes := []string{"defaults", "shell", "containers", "laptop", "desktop", "greeter"}
	var workers sync.WaitGroup
	errors := make(chan error, len(scopes)+1)
	for _, scope := range scopes {
		workers.Add(1)
		go func(scope string) {
			defer workers.Done()
			errors <- WriteProfileReport(path, scope, map[string]any{"schema_version": 1, "scope": scope})
		}(scope)
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		errors <- WriteRunSummary(path, map[string]any{"status": "running", "profiles": map[string]any{}})
	}()
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range scopes {
		profile := map[string]any{}
		if err := ReadProfileReport(path, scope, &profile); err != nil || profile["scope"] != scope {
			t.Fatalf("scope %s was overwritten: %#v %v", scope, profile, err)
		}
	}
}

func TestUnifiedReportLoginUpdatesPendingSummaryAfterEveryStageFinishes(t *testing.T) {
	c, _ := storageContext(t)
	path := unifiedReportFixture(t, c, "laptop", "desktop")
	stages := map[string]any{}
	for _, scope := range []string{"laptop", "desktop"} {
		stages[scope] = map[string]any{"status": "pending_login", "artifacts": filepath.Join(stateRoot(c), "runs", filepath.Base(c.RunDir), scope), "retry": "saved retry"}
		if err := WriteProfileReport(path, scope, map[string]any{"schema_version": 1, "status": "pending_login", "pending_configuration": map[string]any{"status": "pending"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteRunSummary(path, map[string]any{"status": "pending_login", "stages": stages, "pending_configuration": map[string]any{"status": "pending"}}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"laptop", "desktop"} {
		if err := WriteProfileReport(path, scope, map[string]any{"schema_version": 1, "status": "applied_before_login", "pending_configuration": map[string]any{"status": "applied"}}); err != nil {
			t.Fatal(err)
		}
		document := map[string]any{}
		if err := ReadJSON(path, &document); err != nil {
			t.Fatal(err)
		}
		stage := object(object(document["stages"])[scope])
		if stage["status"] != "applied_before_login" || stage["artifacts"] != object(stages[scope])["artifacts"] || stage["retry"] != "saved retry" {
			t.Fatal("login update lost stage metadata")
		}
		if scope == "laptop" {
			if document["status"] != "pending_login" || object(document["pending_configuration"])["status"] != "pending" {
				t.Fatal("desktop is still pending but overall report is complete")
			}
		} else if document["status"] != "completed_with_manual_checks" || object(document["pending_configuration"])["status"] != "none" {
			t.Fatal("last login journal did not finish the pending summary")
		}
	}
}

func TestUnifiedReportTerminalUpdatesPreserveFailureHistory(t *testing.T) {
	for _, overall := range []string{"partial", "interrupted", "pending_login"} {
		t.Run(overall, func(t *testing.T) {
			c, _ := storageContext(t)
			path := unifiedReportFixture(t, c, "desktop")
			if err := WriteRunSummary(path, map[string]any{"status": overall, "error": "original setup error", "stages": map[string]any{"desktop": map[string]any{"status": "running", "error": "original stage error"}}, "pending_configuration": map[string]any{"status": "pending"}}); err != nil {
				t.Fatal(err)
			}
			if err := WriteProfileReport(path, "desktop", map[string]any{"schema_version": 1, "status": "pending"}); err != nil {
				t.Fatal(err)
			}
			document := map[string]any{}
			if err := ReadJSON(path, &document); err != nil || object(object(document["stages"])["desktop"])["status"] != "running" {
				t.Fatalf("initial journal replaced running stage status: %v", err)
			}
			if err := WriteProfileReport(path, "desktop", map[string]any{"schema_version": 1, "status": "restored", "pending_configuration": map[string]any{"status": "cancelled"}}); err != nil {
				t.Fatal(err)
			}
			if err := ReadJSON(path, &document); err != nil {
				t.Fatal(err)
			}
			want := overall
			if overall == "pending_login" {
				want = "configuration_restored"
			}
			if document["status"] != want || document["error"] != "original setup error" || object(object(document["stages"])["desktop"])["error"] != "original stage error" || object(document["pending_configuration"])["status"] != "none" {
				t.Fatal("restoration lost failure history or left a stale pending summary")
			}
		})
	}
}

func TestUnifiedReportPartialRestoreClearsPendingAndKeepsManualFollowup(t *testing.T) {
	c, _ := storageContext(t)
	path := unifiedReportFixture(t, c, "desktop")
	if err := WriteRunSummary(path, map[string]any{"status": "pending_login", "stages": map[string]any{"desktop": map[string]any{"status": "pending_login"}}, "pending_configuration": map[string]any{"status": "pending"}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteProfileReport(path, "desktop", map[string]any{"schema_version": 1, "status": "restore_partial", "pending_configuration": map[string]any{"status": "cancelled"}, "warnings": []string{"file changed; restore manually"}}); err != nil {
		t.Fatal(err)
	}
	document := map[string]any{}
	if err := ReadJSON(path, &document); err != nil {
		t.Fatal(err)
	}
	if document["status"] != "partial" || object(document["pending_configuration"])["status"] != "none" || object(object(document["stages"])["desktop"])["status"] != "restore_partial" {
		t.Fatal("partial restore reported as still pending or complete")
	}
	profile := object(object(document["profiles"])["desktop"])
	if !reflect.DeepEqual(stringList(profile["warnings"]), []string{"file changed; restore manually"}) {
		t.Fatal("manual restore warning was lost")
	}
}

func TestUnifiedReportRefusesMissingCorruptAndUnsupportedDocuments(t *testing.T) {
	for _, content := range []string{"", "not JSON", `{}`, `{"format":"myarch-buildkit-run","schema_version":2,"profiles":{}}`, `{"format":"myarch-buildkit-run","schema_version":3,"profiles":null}`} {
		t.Run(content, func(t *testing.T) {
			c, _ := storageContext(t)
			path := filepath.Join(c.RunDir, "report.json")
			if content != "" {
				storagePut(t, path, content)
			}
			profile := map[string]any{"schema_version": 1, "status": "applied"}
			if err := WriteProfileReport(path, "laptop", profile); err == nil {
				t.Fatal("invalid document replaced by profile write")
			}
			if err := WriteRunSummary(path, map[string]any{"status": "completed"}); err == nil {
				t.Fatal("invalid document replaced by summary write")
			}
			if content == "" {
				if exists(path) {
					t.Fatal("missing report recreated")
				}
			} else if storageRead(t, path) != content {
				t.Fatal("invalid original report was modified")
			}
		})
	}
}

func TestUnifiedReportRejectsUnknownScopesAndInvalidSchemaWithoutWriting(t *testing.T) {
	c, _ := storageContext(t)
	path := unifiedReportFixture(t, c)
	before := storageRead(t, path)
	for _, scope := range []string{"../desktop", "Desktop", "", "packages-other"} {
		if scope == "" {
			continue // Empty scope deliberately selects the legacy adapter.
		}
		if err := WriteProfileReport(path, scope, map[string]any{"schema_version": 1}); err == nil {
			t.Fatalf("unsupported scope accepted: %s", scope)
		}
		var profile map[string]any
		if err := ReadProfileReport(path, scope, &profile); err == nil {
			t.Fatalf("unsupported scope read: %s", scope)
		}
	}
	if err := WriteProfileReport(path, "desktop", map[string]any{"schema_version": 3}); err == nil {
		t.Fatal("envelope accepted as a profile")
	}
	for _, summary := range []map[string]any{{"format": "different"}, {"schema_version": 2}} {
		if err := WriteRunSummary(path, summary); err == nil {
			t.Fatal("summary changed document format")
		}
	}
	if storageRead(t, path) != before {
		t.Fatal("validation failure modified the report")
	}
}

func TestUnifiedReportRestoresOnlyRequestedScopeAndKeepsOtherActiveMarker(t *testing.T) {
	c, _ := storageContext(t)
	root := c.RunDir
	path := unifiedReportFixture(t, c, "defaults", "shell")
	paths := map[string]string{}
	for _, scope := range []string{"defaults", "shell"} {
		stage := *c
		stage.RunDir = filepath.Join(stateRoot(c), "runs", filepath.Base(root), scope)
		stage.ReportPath, stage.ReportScope = path, scope
		stage.Report = map[string]any{"schema_version": 1}
		paths[scope] = filepath.Join(c.Home, scope+".conf")
		storagePut(t, paths[scope], "original "+scope)
		if err := stage.Write(paths[scope], []byte("generated "+scope), 0600); err != nil {
			t.Fatal(err)
		}
	}
	shellBefore := map[string]any{}
	if err := ReadProfileReport(path, "shell", &shellBefore); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(stateRoot(c), "active-profile.json")
	if err := WriteJSON(active, map[string]any{"report": path, "report_scope": "desktop"}); err != nil {
		t.Fatal(err)
	}
	c.Options.RestoreStage, c.Options.StageRunDir = "defaults", root
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	if storageRead(t, paths["defaults"]) != "original defaults" || storageRead(t, paths["shell"]) != "generated shell" || !exists(active) {
		t.Fatal("restore affected another scope or its active marker")
	}
	shellAfter := map[string]any{}
	defaults := map[string]any{}
	if err := ReadProfileReport(path, "shell", &shellAfter); err != nil {
		t.Fatal(err)
	}
	if err := ReadProfileReport(path, "defaults", &defaults); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(shellBefore, shellAfter) || defaults["status"] != "restored" || c.ReportScope != "defaults" {
		t.Fatal("restoration journal did not remain scoped")
	}
}

func TestUnifiedRestoreProfileUsesDesktopAndRefusesUnsafeArtifacts(t *testing.T) {
	for _, kind := range []string{"desktop", "outside", "other-run", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := storageContext(t)
			root := c.RunDir
			path := unifiedReportFixture(t, c, "desktop")
			artifacts := filepath.Join(stateRoot(c), "runs", filepath.Base(root), "desktop")
			stage := *c
			stage.RunDir, stage.ReportPath, stage.ReportScope = artifacts, path, "desktop"
			stage.Report = map[string]any{"schema_version": 1, "completed_phases": []string{"desktop"}}
			target := filepath.Join(c.Home, "desktop.conf")
			if err := stage.Write(target, []byte("generated"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "outside" || kind == "other-run" {
				bad := filepath.Join(c.Home, "outside")
				if kind == "other-run" {
					bad = filepath.Join(stateRoot(c), "runs", "another-run", "desktop")
				}
				if err := WriteRunSummary(path, map[string]any{"stages": map[string]any{"desktop": map[string]any{"artifacts": bad}}}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink" {
				if err := os.MkdirAll(filepath.Dir(artifacts), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(c.Home, "outside"), artifacts); err != nil {
					t.Fatal(err)
				}
			}
			c.Options.RestoreProfile = root
			err := RestoreContext(c)
			if kind == "desktop" {
				if err != nil || exists(target) || c.ReportScope != "desktop" {
					t.Fatalf("desktop profile restoration failed: %v", err)
				}
			} else if err == nil || !exists(target) {
				t.Fatalf("unsafe artifacts accepted: %s, %v", kind, err)
			}
		})
	}
}

func TestProfileAdapterRetainsLegacyStandaloneRestore(t *testing.T) {
	c, _ := storageContext(t)
	c.Options.RestoreStage = "defaults"
	target := filepath.Join(c.Home, "legacy.conf")
	storagePut(t, target, "original")
	if err := c.Write(target, []byte("generated"), 0600); err != nil {
		t.Fatal(err)
	}
	profile := map[string]any{}
	if err := ReadProfileReport(c.ReportPath, "", &profile); err != nil || mapJSONNumber(profile["schema_version"]) != 1 {
		t.Fatalf("legacy read failed: %v", err)
	}
	if err := RestoreContext(c); err != nil || storageRead(t, target) != "original" {
		t.Fatalf("legacy restore failed: %v", err)
	}
}
