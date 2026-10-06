package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMissingPendingManifestRetainsQueueAndReportsRecovery(t *testing.T) {
	c, runner := storageContext(t)
	target := filepath.Join(c.ConfigHome, "myarch-buildkit", "laptop.conf")
	storagePut(t, target, "current desktop")
	storageStage(t, c, target, "next login")
	storagePublish(t, c)
	before := storageRead(t, queuePath(c))
	if err := os.Remove(manifestPath(c)); err != nil {
		t.Fatal(err)
	}
	err := ValidatePending(c)
	var pending *PendingConfigurationError
	if !errors.As(err, &pending) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing bundle must retain a typed diagnostic: %v", err)
	}
	if pending.Phase != "laptop" || pending.Manifest != manifestPath(c) || pending.Report != c.ReportPath || !strings.Contains(pending.RecoveryCommand, "--restore-stage laptop") {
		t.Fatalf("incorrect recovery diagnostic: %#v", pending)
	}
	if strings.Contains(err.Error(), "--restore-stage") || strings.Contains(err.Error(), "--cancel-pending") {
		t.Fatal("structured recovery commands were embedded in prose")
	}
	if got := storageRead(t, queuePath(c)); got != before {
		t.Fatal("read-only validation discarded an unavailable bundle")
	}
	if storageRead(t, target) != "current desktop" || len(runner.calls) != 0 {
		t.Fatal("missing manifest validation affected the desktop")
	}
	// With the report retained, the existing explicit restoration operation can
	// safely cancel the dangling queue without needing the lost manifest.
	c.Pending = nil
	c.Options.RestoreStage = "laptop"
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	items, err := PendingItems(c)
	if err != nil || len(items) != 0 || storageRead(t, target) != "current desktop" {
		t.Fatalf("explicit restore did not cancel only the queued stage: %v %v", items, err)
	}
}

func TestMissingWholeRunCanBeExplicitlyCancelled(t *testing.T) {
	c, runner := storageContext(t)
	missing := QueueItem{Manifest: manifestPath(c), Phase: "laptop"}
	if err := writeQueue(c, []QueueItem{missing}); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(stateRoot(c), "active-profile.json")
	storagePut(t, active, `{"report":"existing desktop report"}`)
	var pending *PendingConfigurationError
	if err := ValidatePending(c); !errors.As(err, &pending) || !strings.Contains(pending.RecoveryCommand, "--cancel-pending laptop") {
		t.Fatalf("lost run recovery is unavailable: %v", err)
	}
	if err := CancelMissingPending(c, "laptop"); err != nil {
		t.Fatal(err)
	}
	items, err := PendingItems(c)
	if err != nil || len(items) != 0 {
		t.Fatalf("missing entry still queued: %v %v", items, err)
	}
	if storageRead(t, active) != `{"report":"existing desktop report"}` || len(runner.calls) != 0 || exists(c.RunDir) {
		t.Fatal("queue cancellation changed active files or recreated lost run data")
	}
}

func TestCancelMissingPendingRejectsPresentAndUnsafeManifests(t *testing.T) {
	for _, kind := range []string{"valid", "malformed", "symlink", "writable-parent"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := storageContext(t)
			item := QueueItem{Manifest: manifestPath(c), Phase: "laptop"}
			if err := writeQueue(c, []QueueItem{item}); err != nil {
				t.Fatal(err)
			}
			before := storageRead(t, queuePath(c))
			switch kind {
			case "valid":
				if err := WriteJSON(item.Manifest, Manifest{Version: 1, Status: "pending", Phase: "laptop"}); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				storagePut(t, item.Manifest, "not JSON")
			case "symlink":
				if err := os.MkdirAll(c.RunDir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(c.Home, "absent-target"), item.Manifest); err != nil {
					t.Fatal(err)
				}
			case "writable-parent":
				if err := os.MkdirAll(c.RunDir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(c.RunDir, 0777); err != nil {
					t.Fatal(err)
				}
			}
			if err := CancelMissingPending(c, "laptop"); err == nil {
				t.Fatal("present/unsafe manifest was treated as missing")
			}
			if got := storageRead(t, queuePath(c)); got != before {
				t.Fatal("rejected cancellation changed the queue")
			}
		})
	}
}

func TestMissingLaptopCancellationRequiresDesktopFirst(t *testing.T) {
	c, _ := storageContext(t)
	laptop := QueueItem{Manifest: manifestPath(c), Phase: "laptop"}
	desktop := QueueItem{Manifest: filepath.Join(c.Home, "desktop-run", "pending-profile.json"), Phase: "desktop"}
	items := []QueueItem{laptop, desktop}
	if err := writeQueue(c, items); err != nil {
		t.Fatal(err)
	}
	var pending *PendingConfigurationError
	if err := ValidatePending(c); !errors.As(err, &pending) || len(pending.RecoveryCommands) != 2 || !strings.Contains(pending.RecoveryCommands[0], "--cancel-pending desktop") || !strings.Contains(pending.RecoveryCommands[1], "--cancel-pending laptop") {
		t.Fatalf("dependent cancellation order is unclear: %v %#v", err, pending)
	}
	storageRequireError(t, CancelMissingPending(c, "laptop"), "--cancel-pending desktop")
	got, err := PendingItems(c)
	if err != nil || !reflect.DeepEqual(got, items) {
		t.Fatal("failed dependency guard modified the queue")
	}
	if err := CancelMissingPending(c, "desktop"); err != nil {
		t.Fatal(err)
	}
	got, err = PendingItems(c)
	if err != nil || !reflect.DeepEqual(got, []QueueItem{laptop}) {
		t.Fatal("desktop cancellation discarded a different missing phase")
	}
	if err := CancelMissingPending(c, "laptop"); err != nil {
		t.Fatal(err)
	}
}

func TestPublishingDoesNotReplaceDanglingQueueImplicitly(t *testing.T) {
	c, _ := storageContext(t)
	old := QueueItem{Manifest: filepath.Join(c.Home, "lost-run", "pending-profile.json"), Phase: "laptop"}
	if err := writeQueue(c, []QueueItem{old}); err != nil {
		t.Fatal(err)
	}
	storageStage(t, c, filepath.Join(c.ConfigHome, "laptop.conf"), "replacement")
	var pending *PendingConfigurationError
	if err := c.Publish("laptop"); !errors.As(err, &pending) {
		t.Fatalf("new staging hid the missing old bundle: %v", err)
	}
	items, err := PendingItems(c)
	if err != nil || !reflect.DeepEqual(items, []QueueItem{old}) {
		t.Fatal("publishing silently replaced the missing bundle")
	}
}

func TestCancelMissingPendingRejectsForeignPathsAndReadOnlyContext(t *testing.T) {
	c, _ := storageContext(t)
	foreign := QueueItem{Manifest: filepath.Join(filepath.Dir(c.Home), "foreign-run", "pending-profile.json"), Phase: "laptop"}
	if err := writeQueue(c, []QueueItem{foreign}); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, CancelMissingPending(c, "laptop"), "unsupported pending")
	if err := writeQueue(c, []QueueItem{{Manifest: manifestPath(c), Phase: "laptop"}}); err != nil {
		t.Fatal(err)
	}
	c.CheckOnly = true
	storageRequireError(t, CancelMissingPending(c, "laptop"), "a check cannot cancel")
	items, err := PendingItems(c)
	if err != nil || len(items) != 1 {
		t.Fatal("read-only cancellation altered the queue")
	}
}

func TestPendingQueueSymlinkIsNotTreatedAsEmpty(t *testing.T) {
	c, _ := storageContext(t)
	if err := os.MkdirAll(stateRoot(c), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(c.Home, "missing-queue-target"), queuePath(c)); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, ValidatePending(c), "unsafe symlink")
	storageRequireError(t, CancelMissingPending(c, "laptop"), "unsafe symlink")
	if info, err := os.Lstat(queuePath(c)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("invalid queue was changed")
	}
}

func unifiedProfileFixture(t *testing.T) (*Context, *Context, string) {
	t.Helper()
	laptop, _ := storageContext(t)
	root := filepath.Join(laptop.Home, "visible-run")
	reportPath := filepath.Join(root, "report.json")
	laptop.RunDir = filepath.Join(stateRoot(laptop), "runs", filepath.Base(root), "laptop")
	laptop.ReportPath, laptop.ReportScope = reportPath, "laptop"
	laptop.Report = map[string]any{"schema_version": 1}
	laptop.ensureReport()
	desktop := *laptop
	desktop.RunDir = filepath.Join(stateRoot(laptop), "runs", filepath.Base(root), "desktop")
	desktop.ReportScope = "desktop"
	desktop.Report = map[string]any{"schema_version": 1}
	desktop.ensureReport()
	if err := WriteJSON(reportPath, map[string]any{
		"format": RunReportFormat, "schema_version": RunReportSchemaVersion,
		"status": "pending_login", "profiles": map[string]any{},
		"stages": map[string]any{
			"laptop":  map[string]any{"status": "pending_login", "artifacts": laptop.RunDir},
			"desktop": map[string]any{"status": "pending_login", "artifacts": desktop.RunDir},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return laptop, &desktop, root
}

func TestUnifiedLaptopAndDesktopLoginPreservesBothJournals(t *testing.T) {
	laptop, desktop, root := unifiedProfileFixture(t)
	fragment := filepath.Join(laptop.ConfigHome, "myarch-buildkit", "laptop.conf")
	storagePut(t, fragment, "original laptop settings\n")
	storageStage(t, laptop, fragment, "input { natural_scroll = true }\n")
	laptop.Pending.Validation["requires_desktop_stage"] = true
	storagePublish(t, laptop)
	main := filepath.Join(desktop.ConfigHome, "hypr", "hyprland.conf")
	managed := filepath.Join(desktop.ConfigHome, "hypr", "desktop-managed.conf")
	custom := filepath.Join(desktop.ConfigHome, "hypr", "desktop-custom.conf")
	desktop.Report["profile_config"] = map[string]any{"main_config": main, "managed_config": managed, "custom_config": custom, "laptop_fragment": fragment}
	storageStage(t, desktop, managed, "source = "+fragment+"\nexec-once = dms run\n")
	storageStage(t, desktop, custom, "# personal\n")
	storageStage(t, desktop, main, "source = "+managed+"\nsource = "+custom+"\n")
	if err := ValidateDesktop(desktop); err != nil {
		t.Fatal(err)
	}
	if err := desktop.Publish("desktop"); err != nil {
		t.Fatal(err)
	}
	items, err := PendingItems(laptop)
	if err != nil || len(items) != 2 {
		t.Fatalf("expected both scoped profiles in queue: %v %v", items, err)
	}
	for _, item := range items {
		manifest := Manifest{}
		if err := ReadJSON(item.Manifest, &manifest); err != nil || manifest.Version != 2 || manifest.Report != laptop.ReportPath || manifest.ReportScope != item.Phase || item.Report != manifest.Report || item.ReportScope != manifest.ReportScope {
			t.Fatalf("incomplete unified queue/report reference: %#v %#v %v", item, manifest, err)
		}
	}
	if err := ApplyPending(laptop); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"laptop", "desktop"} {
		profile := map[string]any{}
		if err := ReadProfileReport(laptop.ReportPath, scope, &profile); err != nil {
			t.Fatal(err)
		}
		if profile["status"] != "applied_before_login" || object(profile["pending_configuration"])["status"] != "applied" || len(reportRecords(profile, "file_changes")) == 0 {
			t.Fatalf("%s journal lost during other stage's writes: %#v", scope, profile)
		}
	}
	marker := map[string]any{}
	if err := ReadJSON(filepath.Join(stateRoot(laptop), "active-profile.json"), &marker); err != nil || marker["report"] != laptop.ReportPath || marker["report_scope"] != "desktop" {
		t.Fatalf("active desktop lacks exact report scope: %#v %v", marker, err)
	}
	document := map[string]any{}
	if err := ReadJSON(laptop.ReportPath, &document); err != nil {
		t.Fatal(err)
	}
	if str(object(object(document["stages"])["desktop"])["artifacts"]) != desktop.RunDir || len(object(document["profiles"])) != 2 {
		t.Fatal("login journal writes erased summary or another profile")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "report.json" {
		t.Fatalf("visible run produced extra reports: %v %v", entries, err)
	}
	// Restoration must retain the independently owned active desktop marker even
	// though both profiles now use the same physical report file.
	laptop.Pending = nil
	laptop.Options.RestoreStage = "laptop"
	if err := RestoreContext(laptop); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(stateRoot(laptop), "active-profile.json")) || storageRead(t, fragment) != "original laptop settings\n" {
		t.Fatal("restoring laptop removed independently scoped desktop ownership")
	}
	desktopProfile := map[string]any{}
	if err := ReadProfileReport(desktop.ReportPath, "desktop", &desktopProfile); err != nil || desktopProfile["status"] != "applied_before_login" {
		t.Fatalf("laptop restore changed desktop journal: %v %#v", err, desktopProfile)
	}
}

func TestMissingUnifiedArtifactsRecoverFromSingleRunReport(t *testing.T) {
	laptop, _, root := unifiedProfileFixture(t)
	storageStage(t, laptop, filepath.Join(laptop.ConfigHome, "laptop.conf"), "next login")
	storagePublish(t, laptop)
	if err := os.RemoveAll(laptop.RunDir); err != nil {
		t.Fatal(err)
	}
	var pending *PendingConfigurationError
	if err := ValidatePending(laptop); !errors.As(err, &pending) || pending.Report != laptop.ReportPath || !strings.Contains(pending.RecoveryCommand, "--restore-stage laptop --stage-run-dir "+root) {
		t.Fatalf("lost internal artifacts hid retained root report: %v %#v", err, pending)
	}
	// Exercise the root-directory command printed above, rather than depending
	// on an already configured in-memory stage context.
	restore := *laptop
	restore.RunDir, restore.ReportPath, restore.ReportScope = root, filepath.Join(root, "profile-report.json"), ""
	restore.Pending = nil
	restore.Options.RestoreStage, restore.Options.StageRunDir = "laptop", root
	if err := RestoreContext(&restore); err != nil {
		t.Fatal(err)
	}
	items, err := PendingItems(laptop)
	if err != nil || len(items) != 0 {
		t.Fatalf("root report did not permit explicit cancellation of lost artifacts: %v %v", items, err)
	}
	profile := map[string]any{}
	if err := ReadProfileReport(laptop.ReportPath, "laptop", &profile); err != nil || profile["status"] != "restored" || object(profile["pending_configuration"])["status"] != "cancelled" {
		t.Fatalf("explicit cancellation did not persist in single report: %v %#v", err, profile)
	}
}

func TestUnifiedPendingRejectsMismatchedQueueAndManifestScope(t *testing.T) {
	laptop, _, _ := unifiedProfileFixture(t)
	storageStage(t, laptop, filepath.Join(laptop.ConfigHome, "laptop.conf"), "next login")
	storagePublish(t, laptop)
	manifest := Manifest{}
	if err := ReadJSON(manifestPath(laptop), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ReportScope = "desktop"
	if err := WriteJSON(manifestPath(laptop), manifest); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, ValidatePending(laptop), "unexpected unified report")
	items, err := PendingItems(laptop)
	if err != nil || len(items) != 1 {
		t.Fatal("invalid scope caused automatic cancellation")
	}
}
