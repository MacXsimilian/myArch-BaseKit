package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type storageRunner struct {
	calls      []Command
	active     bool
	failNative bool
}

func (r *storageRunner) Execute(cmd Command) (CommandResult, error) {
	r.calls = append(r.calls, cmd)
	out := ""
	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "--help") {
		out = "--verify-config --config"
	}
	if strings.Contains(joined, "--property=ActiveState") {
		out = "inactive"
		if r.active {
			out = "active"
		}
	}
	if strings.Contains(joined, "--property=UnitFileState") {
		out = "disabled"
	}
	if len(cmd.Args) > 1 && cmd.Args[0] == "/usr/bin/dms" && cmd.Args[1] == "version" {
		out = "dms v1.6.2"
	}
	if r.failNative && strings.Contains(joined, "--verify-config") {
		return CommandResult{Code: 1, Stderr: "config error"}, nil
	}
	return CommandResult{Stdout: out}, nil
}
func storageContext(t *testing.T) (*Context, *storageRunner) {
	t.Helper()
	home := t.TempDir()
	runner := &storageRunner{}
	ctx := &Context{Home: home, ConfigHome: filepath.Join(home, ".config"), StateHome: filepath.Join(home, ".state"), DataHome: filepath.Join(home, ".data"), CacheHome: filepath.Join(home, ".cache"), BinDir: filepath.Join(home, ".local", "bin"), RunDir: filepath.Join(home, "run"), Runner: runner, Report: map[string]any{"schema_version": 1}, Inventory: map[string]any{"hyprland_binary": "/usr/bin/Hyprland"}}
	ctx.ReportPath = filepath.Join(ctx.RunDir, "profile-report.json")
	ctx.ensureReport()
	old := noDesktopProcRoot
	noDesktopProcRoot = filepath.Join(home, "proc")
	if err := os.MkdirAll(noDesktopProcRoot, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { noDesktopProcRoot = old })
	return ctx, runner
}
func storagePut(t *testing.T, path string, data string) {
	t.Helper()
	if err := AtomicWrite(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func storageStage(t *testing.T, c *Context, path, data string) {
	t.Helper()
	if c.Pending == nil {
		if err := c.StartStaging(); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Write(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func storagePublish(t *testing.T, c *Context) {
	t.Helper()
	if err := c.Publish("laptop"); err != nil {
		t.Fatal(err)
	}
}
func storageRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func storageRequireError(t *testing.T, err error, part string) {
	t.Helper()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(part)) {
		t.Fatalf("wanted error containing %q, got %v", part, err)
	}
}

func TestStorageStagingLeavesWatchedFilesUntouched(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.ConfigHome, "DankMaterialShell", "settings.json")
	storagePut(t, path, `{"existing":1}`)
	storageStage(t, c, path, `{"existing":1,"managed":2}`)
	if got := storageRead(t, path); got != `{"existing":1}` {
		t.Fatal(got)
	}
	if c.View(path) == path || len(reportRecords(c.Report, "file_changes")) != 0 {
		t.Fatal("staging wrote active files or created active backups")
	}
}
func TestStorageJSONPreservesUnrelatedRuntimeProperties(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.ConfigHome, "DankMaterialShell", "settings.json")
	storagePut(t, path, `{"managed":1,"runtime":1}`)
	storageStage(t, c, path, `{"managed":2,"runtime":1}`)
	storagePublish(t, c)
	storagePut(t, path, `{"managed":1,"runtime":9,"extra":true}`)
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	value := map[string]any{}
	if err := ReadJSON(path, &value); err != nil {
		t.Fatal(err)
	}
	if value["managed"] != float64(2) || value["runtime"] != float64(9) || value["extra"] != true {
		t.Fatal(value)
	}
}
func TestStorageUnchangedJSONStillAllowsRuntimeChanges(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.ConfigHome, "DankMaterialShell", "settings.json")
	storagePut(t, path, `{"runtime":1}`)
	storageStage(t, c, path, `{"runtime":1}`)
	storagePublish(t, c)
	storagePut(t, path, `{"runtime":9}`)
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(storageRead(t, path), "9") {
		t.Fatal("runtime property lost")
	}
}
func TestStorageExplicitEqualJSONSettingAndNestedMerge(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.ConfigHome, "DankMaterialShell", "settings.json")
	storagePut(t, path, `{"managed":true,"screenPreferences":{"custom":1}}`)
	storageStage(t, c, path, `{"managed":true,"screenPreferences":{"custom":1,"dock":["all"]}}`)
	if err := c.OverrideJSON(path, map[string]any{"managed": true, "screenPreferences": map[string]any{"dock": []string{"all"}}}); err != nil {
		t.Fatal(err)
	}
	storagePublish(t, c)
	storagePut(t, path, `{"managed":false,"screenPreferences":{"custom":9}}`)
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	value := map[string]any{}
	if err := ReadJSON(path, &value); err != nil {
		t.Fatal(err)
	}
	if value["managed"] != true || object(value["screenPreferences"])["custom"] != float64(9) {
		t.Fatal(value)
	}
}
func TestStorageChecksEveryBaselineBeforeWriting(t *testing.T) {
	c, _ := storageContext(t)
	a, b := filepath.Join(c.Home, "first"), filepath.Join(c.Home, "second")
	storagePut(t, a, "old a")
	storagePut(t, b, "old b")
	storageStage(t, c, a, "new a")
	storageStage(t, c, b, "new b")
	storagePublish(t, c)
	storagePut(t, b, "user edit")
	storageRequireError(t, ApplyPending(c), "changed since staging")
	if storageRead(t, a) != "old a" {
		t.Fatal("earlier file changed before later conflict check")
	}
}
func TestStorageRejectsPayloadTampering(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "file")
	storagePut(t, path, "old")
	storageStage(t, c, path, "new")
	storagePublish(t, c)
	storagePut(t, c.View(path), "tampered")
	storageRequireError(t, ApplyPending(c), "payload changed")
	if storageRead(t, path) != "old" {
		t.Fatal("active file changed")
	}
}
func TestStorageRejectsPendingSymlinkDestinationAndParent(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprint(parent), func(t *testing.T) {
			c, _ := storageContext(t)
			path := filepath.Join(c.Home, "real", "file")
			storagePut(t, path, "old")
			link := filepath.Join(c.Home, "link")
			target := path
			if parent {
				target = filepath.Dir(path)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if parent {
				link = filepath.Join(link, "new")
			}
			if err := c.StartStaging(); err != nil {
				t.Fatal(err)
			}
			storageRequireError(t, c.Write(link, []byte("new"), 0600), "symlink")
		})
	}
}
func TestStorageOrdinaryDefaultsPreserveSafeSymlinksAndMode(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "actual", "settings")
	storagePut(t, path, "old")
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(c.Home, "config-link")
	if err := os.Symlink(filepath.Dir(path), link); err != nil {
		t.Fatal(err)
	}
	requested := filepath.Join(link, "settings")
	if err := c.Write(requested, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("parent symlink replaced")
	}
	info, err = os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatal("existing file mode changed")
	}
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	if storageRead(t, path) != "old" {
		t.Fatal("safe symlink target not restored")
	}
}
func TestStorageCancellationLeavesDesktopUntouched(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "file")
	storagePut(t, path, "old")
	storageStage(t, c, path, "new")
	storagePublish(t, c)
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	items, err := PendingItems(c)
	if err != nil || len(items) != 0 {
		t.Fatalf("queue = %v %v", items, err)
	}
	if storageRead(t, path) != "old" {
		t.Fatal("cancel changed desktop")
	}
}
func TestStorageLoginBackupCapturesLatestJSON(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.ConfigHome, "DankMaterialShell", "settings.json")
	storagePut(t, path, `{"runtime":1}`)
	storageStage(t, c, path, `{"runtime":1,"managed":true}`)
	storagePublish(t, c)
	storagePut(t, path, `{"runtime":9}`)
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	report := map[string]any{}
	if err := ReadJSON(c.ReportPath, &report); err != nil {
		t.Fatal(err)
	}
	record := reportRecords(report, "file_changes")[0]
	if storageRead(t, str(record["backup"])) != `{"runtime":9}` {
		t.Fatal("backup is stale")
	}
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	if storageRead(t, path) != `{"runtime":9}` {
		t.Fatal("latest original not restored")
	}
}
func TestStorageExplicitRestorePreservesLaterEdits(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "file")
	storagePut(t, path, "old")
	storageStage(t, c, path, "new")
	storagePublish(t, c)
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	storagePut(t, path, "user edit")
	storageRequireError(t, RestoreContext(c), "manual follow-up")
	if storageRead(t, path) != "user edit" {
		t.Fatal("later edits overwritten")
	}
}
func TestStorageOwnershipBlocksEditedManagedFile(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.ConfigHome, "hypr", "desktop-managed.conf")
	storageStage(t, c, path, "generated")
	storagePublish(t, c)
	if err := ApplyPending(c); err != nil {
		t.Fatal(err)
	}
	if err := c.checkOwned(path); err != nil {
		t.Fatal(err)
	}
	storagePut(t, path, "user change")
	storageRequireError(t, c.checkOwned(path), "edited after setup")
}
func TestStorageActiveServiceBlocksEveryWrite(t *testing.T) {
	c, r := storageContext(t)
	path := filepath.Join(c.Home, "file")
	storagePut(t, path, "old")
	storageStage(t, c, path, "new")
	storagePublish(t, c)
	r.active = true
	storageRequireError(t, ApplyPending(c), "still running")
	if storageRead(t, path) != "old" {
		t.Fatal("active service allowed file replacement")
	}
}
func TestStorageConcurrentDesktopBlocksBeforeServiceCalls(t *testing.T) {
	c, r := storageContext(t)
	path := filepath.Join(c.Home, "file")
	storageStage(t, c, path, "new")
	storagePublish(t, c)
	proc := filepath.Join(noDesktopProcRoot, "123")
	if err := os.Mkdir(proc, 0700); err != nil {
		t.Fatal(err)
	}
	storagePut(t, filepath.Join(proc, "cmdline"), "/usr/bin/dms\x00run\x00")
	storageRequireError(t, ApplyPending(c), "log out")
	if len(r.calls) != 0 || exists(path) {
		t.Fatal("desktop guard ran too late")
	}
}
func TestStorageDuplicatePhaseIsNotReplaced(t *testing.T) {
	c, _ := storageContext(t)
	storageStage(t, c, filepath.Join(c.Home, "file"), "first")
	storagePublish(t, c)
	second := *c
	second.RunDir = filepath.Join(c.Home, "second-run")
	second.ReportPath = filepath.Join(second.RunDir, "profile-report.json")
	second.Report = map[string]any{}
	second.Pending = nil
	storageStage(t, &second, filepath.Join(c.Home, "second"), "second")
	storageRequireError(t, second.Publish("laptop"), "earlier pending")
	items, err := PendingItems(c)
	if err != nil || len(items) != 1 || items[0].Manifest != manifestPath(c) {
		t.Fatal(items, err)
	}
}
func storageDesktop(t *testing.T, c *Context) {
	t.Helper()
	main := filepath.Join(c.ConfigHome, "hypr", "hyprland.conf")
	managed := filepath.Join(c.ConfigHome, "hypr", "desktop-managed.conf")
	custom := filepath.Join(c.ConfigHome, "hypr", "desktop-custom.conf")
	fragment := filepath.Join(c.ConfigHome, "myarch-buildkit", "laptop.conf")
	c.Report["profile_config"] = map[string]any{"main_config": main, "managed_config": managed, "custom_config": custom, "laptop_fragment": fragment}
	storageStage(t, c, fragment, "input { natural_scroll = true }\n")
	storageStage(t, c, managed, "source = "+fragment+"\nexec-once = dms run\n")
	storageStage(t, c, custom, "# personal\n")
	storageStage(t, c, main, "source = "+managed+"\nsource = "+custom+"\n")
	if err := ValidateDesktop(c); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish("desktop"); err != nil {
		t.Fatal(err)
	}
}
func TestStorageNativeValidationUsesStagedDependencies(t *testing.T) {
	c, _ := storageContext(t)
	storageDesktop(t, c)
	candidate := str(c.Pending.Validation["candidate"])
	managedCandidate := filepath.Join(c.RunDir, "validate-managed.conf")
	if !strings.Contains(storageRead(t, candidate), managedCandidate) {
		t.Fatal("loader did not use managed snapshot")
	}
	fragment := str(object(c.Report["profile_config"])["laptop_fragment"])
	if !strings.Contains(storageRead(t, managedCandidate), c.View(fragment)) {
		t.Fatal("managed snapshot loads live fragment")
	}
	if err := ValidatePending(c); err != nil {
		t.Fatal(err)
	}
}
func TestStorageNativeDependencyTamperingBlocksAllWrites(t *testing.T) {
	c, _ := storageContext(t)
	storageDesktop(t, c)
	storagePut(t, filepath.Join(c.RunDir, "validate-managed.conf"), "tampered")
	storageRequireError(t, ApplyPending(c), "dependency changed")
	main := str(object(c.Report["profile_config"])["main_config"])
	if exists(main) {
		t.Fatal("validation tampering applied live")
	}
}
func TestStorageInvalidNativeConfigAndExtraStartupBlockStaging(t *testing.T) {
	c, r := storageContext(t)
	r.failNative = true
	main := filepath.Join(c.Home, "main.conf")
	managed := filepath.Join(c.Home, "managed.conf")
	custom := filepath.Join(c.Home, "custom.conf")
	c.Report["profile_config"] = map[string]any{"main_config": main, "managed_config": managed, "custom_config": custom}
	storageStage(t, c, main, "source = "+managed)
	storageStage(t, c, managed, "# managed")
	storageStage(t, c, custom, "# custom")
	storageRequireError(t, ValidateDesktop(c), "invalid")
	r.failNative = false
	storageStage(t, c, custom, "exec-once = dms run")
	storageRequireError(t, ValidateDesktop(c), "additional DMS")
}
func TestStorageLaptopDependencyRequiresDesktopStage(t *testing.T) {
	c, _ := storageContext(t)
	storageStage(t, c, filepath.Join(c.Home, "laptop.conf"), "settings")
	c.Pending.Validation["requires_desktop_stage"] = true
	storagePublish(t, c)
	storageRequireError(t, ApplyPending(c), "stage desktop")
}
func TestStorageInterruptedManifestIsNeverRetried(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "file")
	storagePut(t, path, "old")
	storageStage(t, c, path, "new")
	storagePublish(t, c)
	manifest := *c.Pending
	manifest.Status = "applying"
	if err := WriteJSON(manifestPath(c), manifest); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, ApplyPending(c), "not ready")
	if storageRead(t, path) != "old" {
		t.Fatal("interrupted bundle retried")
	}
}
func TestStorageLoginAppliesThenStartsOneDesktop(t *testing.T) {
	c, r := storageContext(t)
	storageDesktop(t, c)
	if err := LoginMain(c); err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, call := range r.calls {
		if call.Interactive {
			starts++
			if len(call.Args) != 3 || call.Args[0] != "/usr/bin/Hyprland" || call.Env["XDG_SESSION_TYPE"] != "wayland" {
				t.Fatal(call)
			}
		}
		for _, arg := range call.Args {
			if arg == "--now" {
				t.Fatal("live service mutation")
			}
		}
	}
	if starts != 1 {
		t.Fatal("desktop starts", starts)
	}
	status := map[string]any{}
	if err := ReadJSON(filepath.Join(stateRoot(c), "login-status.json"), &status); err != nil {
		t.Fatal(err)
	}
	if status["running_desktop"] != "not yet started" {
		t.Fatal(status)
	}
}
func TestStorageFailedLoginDoesNotStartDesktop(t *testing.T) {
	c, r := storageContext(t)
	storageDesktop(t, c)
	r.failNative = true
	storageRequireError(t, LoginMain(c), "blocked")
	for _, call := range r.calls {
		if call.Interactive {
			t.Fatal("desktop started after failed verification")
		}
	}
	status := map[string]any{}
	if err := ReadJSON(filepath.Join(stateRoot(c), "login-status.json"), &status); err != nil {
		t.Fatal(err)
	}
	if status["status"] != "blocked" {
		t.Fatal(status)
	}
}
func TestStorageRejectsMalformedJSONAndUnsafePermissions(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "bad.json")
	storagePut(t, path, "{} {}")
	var value any
	if err := ReadJSON(path, &value); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	storagePut(t, path, "{}")
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, ReadJSON(path, &value), "writable-by-others")
}
func TestStorageManifestJSONRemainsCompatible(t *testing.T) {
	manifest := Manifest{}
	if err := json.Unmarshal([]byte(`{"version":1,"status":"pending","phase":"laptop","report":"/home/u/run/profile-report.json","files":[{"path":"/home/u/file","source":"/home/u/run/payload","mode":384,"sha256":"x","before_sha256":null}],"validation":{}}`), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Files[0].BeforeSHA256 != "" {
		t.Fatal("null baseline did not represent absent file")
	}
}

func TestStorageLegacyDefaultsReportRestoresBackupAndCreatedFile(t *testing.T) {
	c, _ := storageContext(t)
	c.Options.RestoreStage = "defaults"
	old, newFile := filepath.Join(c.Home, "settings"), filepath.Join(c.Home, "new")
	backup := filepath.Join(c.RunDir, "backups", "original")
	storagePut(t, old, "generated")
	storagePut(t, newFile, "created")
	storagePut(t, backup, "original")
	reportPath := filepath.Join(c.RunDir, "desktop-report.json")
	if err := WriteJSON(reportPath, map[string]any{"schema_version": 1, "changed_paths": []string{old, newFile}, "backups": []map[string]any{{"path": old, "backup": backup, "mode": 0640}}, "written_sha256": map[string]any{old: hashBytes([]byte("generated")), newFile: hashBytes([]byte("created"))}}); err != nil {
		t.Fatal(err)
	}
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	if storageRead(t, old) != "original" || exists(newFile) {
		t.Fatal("legacy restore failed")
	}
	info, _ := os.Stat(old)
	if info.Mode().Perm() != 0640 {
		t.Fatal("legacy mode not restored")
	}
	if err := RestoreContext(c); err != nil {
		t.Fatal("idempotent legacy restore", err)
	}
}
func TestStorageLegacyShellReportPreservesLinkAndLaterEdits(t *testing.T) {
	c, _ := storageContext(t)
	c.Options.RestoreStage = "shell"
	actual := filepath.Join(c.Home, "actual")
	link := filepath.Join(c.Home, ".bashrc")
	storagePut(t, actual, "generated")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(c.RunDir, "backups", "shell")
	storagePut(t, backup, "old")
	reportPath := filepath.Join(c.RunDir, "shell-report.json")
	if err := WriteJSON(reportPath, map[string]any{"files": []map[string]any{{"path": link, "changed": true, "backup": backup, "sha256": hashBytes([]byte("generated")), "mode": 0600, "symlink_target": actual}}}); err != nil {
		t.Fatal(err)
	}
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(link)
	if info.Mode()&os.ModeSymlink == 0 || storageRead(t, actual) != "old" {
		t.Fatal("legacy link lost")
	}
}
func TestStorageLegacyReportRefusesChangedOutput(t *testing.T) {
	c, _ := storageContext(t)
	c.Options.RestoreStage = "containers"
	path := filepath.Join(c.Home, "containers.conf")
	backup := filepath.Join(c.RunDir, "backup")
	storagePut(t, path, "user edit")
	storagePut(t, backup, "original")
	if err := WriteJSON(filepath.Join(c.RunDir, "podman-report.json"), map[string]any{"files": []map[string]any{{"path": path, "changed": true, "backup": backup, "sha256": hashBytes([]byte("generated"))}}}); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, RestoreContext(c), "changed since setup")
	if storageRead(t, path) != "user edit" {
		t.Fatal("legacy restore overwrote user edit")
	}
}
func TestStorageLegacyReportRefusesChangedSymlinkIdentity(t *testing.T) {
	c, _ := storageContext(t)
	c.Options.RestoreStage = "shell"
	actual := filepath.Join(c.Home, "actual")
	other := filepath.Join(c.Home, "other")
	link := filepath.Join(c.Home, ".zshrc")
	backup := filepath.Join(c.RunDir, "backup")
	storagePut(t, actual, "generated")
	storagePut(t, other, "generated")
	storagePut(t, backup, "old")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(filepath.Join(c.RunDir, "shell-report.json"), map[string]any{"files": []map[string]any{{"path": link, "changed": true, "backup": backup, "sha256": hashBytes([]byte("generated")), "symlink_target": actual}}}); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, RestoreContext(c), "symlink identity")
	if storageRead(t, other) != "generated" {
		t.Fatal("changed link target overwritten")
	}
}
func TestStorageLegacyReportRejectsMissingOutputHash(t *testing.T) {
	c, _ := storageContext(t)
	c.Options.RestoreStage = "cleanup"
	path := filepath.Join(c.Home, "launcher.desktop")
	storagePut(t, path, "generated")
	if err := WriteJSON(filepath.Join(c.RunDir, "cleanup-report.json"), map[string]any{"changed_paths": []string{path}, "backups": []any{}, "written_sha256": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, RestoreContext(c), "no recorded output hash")
	if !exists(path) {
		t.Fatal("legacy file removed without hash")
	}
}

func TestStorageRestoreProfileRootSelectsDesktopStage(t *testing.T) {
	c, _ := storageContext(t)
	root := c.RunDir
	path := filepath.Join(c.Home, "desktop.conf")
	backup := filepath.Join(root, "stages", "desktop", "backups", "original")
	storagePut(t, path, "generated")
	storagePut(t, backup, "original")
	storagePut(t, filepath.Join(c.Home, "root-binary"), "root file")
	if err := WriteJSON(c.ReportPath, map[string]any{"schema_version": 1, "file_changes": []map[string]any{{"path": filepath.Join(c.Home, "root-binary"), "sha256": hashBytes([]byte("root file")), "write_journal_version": 2}}}); err != nil {
		t.Fatal(err)
	}
	desktopReport := filepath.Join(root, "stages", "desktop", "profile-report.json")
	if err := WriteJSON(desktopReport, map[string]any{"schema_version": 1, "completed_phases": []string{"desktop"}, "profile_config": map[string]any{"main_config": path}, "file_changes": []map[string]any{{"path": path, "backup": backup, "sha256": hashBytes([]byte("generated")), "write_journal_version": 2}}}); err != nil {
		t.Fatal(err)
	}
	c.Options.RestoreProfile = root
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	if storageRead(t, path) != "original" || !exists(filepath.Join(c.Home, "root-binary")) {
		t.Fatal("restore profile selected wrong report")
	}
	if c.ReportPath != desktopReport {
		t.Fatal(c.ReportPath)
	}
}
func TestStorageRestoreProfileAcceptsDesktopStageDirectory(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "desktop.conf")
	storagePut(t, path, "generated")
	if err := WriteJSON(c.ReportPath, map[string]any{"schema_version": 1, "completed_phases": []string{"desktop"}, "file_changes": []map[string]any{{"path": path, "sha256": hashBytes([]byte("generated")), "write_journal_version": 2}}}); err != nil {
		t.Fatal(err)
	}
	c.Options.RestoreProfile = c.RunDir
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	if exists(path) {
		t.Fatal("stage directory profile was not restored")
	}
}
func TestStorageRestoreProfileRefusesRootWithoutDesktop(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "root-binary")
	storagePut(t, path, "root file")
	if err := WriteJSON(c.ReportPath, map[string]any{"schema_version": 1, "file_changes": []map[string]any{{"path": path, "sha256": hashBytes([]byte("root file")), "write_journal_version": 2}}}); err != nil {
		t.Fatal(err)
	}
	c.Options.RestoreProfile = c.RunDir
	storageRequireError(t, RestoreContext(c), "no desktop profile")
	if !exists(path) {
		t.Fatal("root-only binary report restored as desktop")
	}
	if err := os.MkdirAll(filepath.Join(c.RunDir, "stages", "shell"), 0700); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, RestoreContext(c), "no desktop profile report")
}
func TestStorageNativeDefaultRestoreCanRunWithDesktopActive(t *testing.T) {
	c, _ := storageContext(t)
	c.Options.RestoreStage = "defaults"
	path := filepath.Join(c.Home, "settings")
	storagePut(t, path, "original")
	if err := c.Write(path, []byte("generated"), 0600); err != nil {
		t.Fatal(err)
	}
	proc := filepath.Join(noDesktopProcRoot, "456")
	if err := os.Mkdir(proc, 0700); err != nil {
		t.Fatal(err)
	}
	storagePut(t, filepath.Join(proc, "cmdline"), "/usr/bin/Hyprland\x00")
	if err := RestoreContext(c); err != nil {
		t.Fatal(err)
	}
	if storageRead(t, path) != "original" {
		t.Fatal("ordinary app default did not restore")
	}
}
