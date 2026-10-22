package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRecoveryPartialRestoreRetainsManagedOwnership(t *testing.T) {
	for _, prior := range []bool{false, true} {
		t.Run(fmt.Sprint("previous-marker-", prior), func(t *testing.T) {
			c, _ := storageContext(t)
			path := filepath.Join(c.ConfigHome, "hypr", "desktop-managed.conf")
			marker := c.ownershipPath(path)
			if prior {
				storagePut(t, path, "prior managed contents")
				if err := WriteJSON(marker, map[string]any{"path": path, "sha256": hashBytes([]byte("prior managed contents"))}); err != nil {
					t.Fatal(err)
				}
			}
			storageStage(t, c, path, "generated")
			storagePublish(t, c)
			if err := ApplyPending(c); err != nil {
				t.Fatal(err)
			}
			markerBefore := storageRead(t, marker)
			storagePut(t, path, "personal edit")
			storageRequireError(t, RestoreContext(c), "changed since setup")
			if storageRead(t, marker) != markerBefore || storageRead(t, path) != "personal edit" {
				t.Fatal("partial restore changed the target or its protecting marker")
			}
			fresh := *c
			fresh.RunDir = filepath.Join(c.Home, "fresh-run")
			fresh.ReportPath = filepath.Join(fresh.RunDir, "profile-report.json")
			fresh.Report = map[string]any{"schema_version": 1}
			fresh.Pending = nil
			if err := fresh.StartStaging(); err != nil {
				t.Fatal(err)
			}
			storageRequireError(t, fresh.Write(path, []byte("replacement"), 0600), "edited after setup")
			if storageRead(t, path) != "personal edit" {
				t.Fatal("restaging overwrote the preserved edit")
			}
			// Resolving the conflict permits both target and marker to restore.
			storagePut(t, path, "generated")
			if err := RestoreContext(c); err != nil {
				t.Fatal(err)
			}
			if prior {
				if storageRead(t, path) != "prior managed contents" || c.checkOwned(path) != nil {
					t.Fatal("previous file and marker were not restored together")
				}
			} else if exists(path) || exists(marker) {
				t.Fatal("created file or marker survived successful restore")
			}
		})
	}
}

func TestRecoveryRestartsAfterOriginalBytesBeforeCompletion(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy-journal-", legacy), func(t *testing.T) {
			c, _ := storageContext(t)
			path := filepath.Join(c.ConfigHome, "application.conf")
			storagePut(t, path, "original")
			if err := c.Write(path, []byte("managed"), 0600); err != nil {
				t.Fatal(err)
			}
			if legacy {
				delete(reportRecords(c.Report, "file_changes")[0], "original_sha256")
				if err := c.Save(); err != nil {
					t.Fatal(err)
				}
			}
			persisted := map[string]any{}
			if err := ReadJSON(c.ReportPath, &persisted); err != nil {
				t.Fatal(err)
			}
			// Stop at the real restore boundary: original bytes are durable, but
			// no completion record has been written to the on-disk journal.
			if err := restoreFile(reportRecords(persisted, "file_changes")[0]); err != nil {
				t.Fatal(err)
			}
			if err := RestoreContext(c); err != nil {
				t.Fatalf("restart rejected its own restored original: %v", err)
			}
			if storageRead(t, path) != "original" || !boolValue(reportRecords(c.Report, "file_changes")[0]["restored"]) {
				t.Fatal("restart failed to complete the recovery journal")
			}
		})
	}
}

func TestRecoveryRefusesCorruptedOriginalBackup(t *testing.T) {
	c, _ := storageContext(t)
	path := filepath.Join(c.Home, "application.conf")
	storagePut(t, path, "original")
	if err := c.Write(path, []byte("managed"), 0600); err != nil {
		t.Fatal(err)
	}
	record := reportRecords(c.Report, "file_changes")[0]
	storagePut(t, str(record["backup"]), "corrupted backup")
	storageRequireError(t, RestoreContext(c), "original backup changed")
	if storageRead(t, path) != "managed" {
		t.Fatal("corrupt backup was written to destination")
	}
}

func TestRecoveryValidatesRecordsBeforeCancellingPending(t *testing.T) {
	for name, invalid := range map[string]any{
		"bad-field":     []map[string]any{{"path": "/invalid.conf", "sha256": 42}},
		"bad-container": "corrupt",
		"bad-object":    map[string]any{"path": "/invalid.conf"},
		"bad-element":   []any{"corrupt"},
		"null-element":  []any{nil},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := storageContext(t)
			storageStage(t, c, filepath.Join(c.Home, "future.conf"), "pending")
			storagePublish(t, c)
			c.Report["file_changes"] = invalid
			if err := c.Save(); err != nil {
				t.Fatal(err)
			}
			storageRequireError(t, RestoreContext(c), "invalid file recovery")
			items, err := PendingItems(c)
			if err != nil || len(items) != 1 {
				t.Fatalf("invalid journal cancelled pending work: %#v %v", items, err)
			}
		})
	}
}

func TestRecoveryManifestStatusValidationKeepsWireFormat(t *testing.T) {
	for _, state := range []manifestStatus{manifestBuilding, manifestPending, manifestApplying, manifestApplied, manifestCancelled} {
		data, err := json.Marshal(Manifest{Status: state})
		if err != nil {
			t.Fatal(err)
		}
		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil || manifest.Status != state {
			t.Fatalf("status changed on disk: %s %v", data, err)
		}
	}
	for _, value := range []string{`{"status":"ready-ish"}`, `{"status":42}`} {
		var manifest Manifest
		if json.Unmarshal([]byte(value), &manifest) == nil {
			t.Fatalf("accepted invalid recovery state: %s", value)
		}
	}
}

func TestRecoveryPathRejectsReplaceableGrandparent(t *testing.T) {
	for _, sticky := range []bool{false, true} {
		t.Run(fmt.Sprint("sticky-", sticky), func(t *testing.T) {
			c, _ := storageContext(t)
			shared := filepath.Join(c.Home, "shared")
			path := filepath.Join(shared, "private", "config")
			storagePut(t, path, "data")
			mode := os.FileMode(0777)
			if sticky {
				mode |= os.ModeSticky
			}
			if err := os.Chmod(shared, mode); err != nil {
				t.Fatal(err)
			}
			for _, candidate := range []string{path, filepath.Join(filepath.Dir(path), "missing", "config")} {
				err := securePath(candidate, true)
				if sticky && err != nil {
					t.Fatal(err)
				}
				if !sticky && err == nil {
					t.Fatal("accepted replaceable ancestry")
				}
			}
		})
	}
}

type recoveryAncestorInfo struct {
	os.FileInfo
	owner uint32
}

func (i recoveryAncestorInfo) Sys() any { return &syscall.Stat_t{Uid: i.owner} }

func TestRecoveryPathTrustsRootButNotForeignAncestors(t *testing.T) {
	dir := t.TempDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := secureAncestor(dir, recoveryAncestorInfo{info, 0}); err != nil {
		t.Fatalf("root-owned private ancestor rejected: %v", err)
	}
	if err := secureAncestor(dir, recoveryAncestorInfo{info, uint32(os.Getuid() + 10000)}); err == nil {
		t.Fatal("foreign-controlled ancestor accepted")
	}
}

// This runner simulates only packaged utility/helper operations; it never
// writes system paths. A missing helper exercises the actual bootstrap flow.
type recoveryHelperRunner struct {
	installed      bool
	capabilityMode string
	unsafe         bool
	temporary      map[string]string
	calls          []Command
}

func (r *recoveryHelperRunner) Execute(cmd Command) (CommandResult, error) {
	r.calls = append(r.calls, cmd)
	args := cmd.Args
	if len(args) < 3 || args[0] != "sudo" || args[1] != "-n" {
		return CommandResult{}, nil
	}
	switch args[2] {
	case "/usr/bin/test":
		if args[3] == "-L" || args[3] == "-e" && !r.installed {
			return CommandResult{Code: 1}, nil
		}
	case "/usr/bin/stat":
		path := args[len(args)-1]
		if path == systemBinary {
			if !r.installed {
				return CommandResult{Code: 1}, nil
			}
			if r.unsafe {
				return CommandResult{Stdout: "0:777:regular file\n"}, nil
			}
			return CommandResult{Stdout: "0:755:regular file\n"}, nil
		}
		return CommandResult{Stdout: "0:755:directory\n"}, nil
	case "/usr/bin/sha256sum":
		path := args[len(args)-1]
		if path == systemBinary && !r.installed {
			return CommandResult{Code: 1}, nil
		}
		digest := r.temporary[path]
		if digest == "" && path == systemBinary {
			digest = hashBytes([]byte("previous helper build"))
		}
		return CommandResult{Stdout: digest + "  " + path + "\n"}, nil
	case "/usr/bin/install":
		if args[3] != "-d" {
			data, err := os.ReadFile(args[len(args)-2])
			if err != nil {
				return CommandResult{}, err
			}
			r.temporary[args[len(args)-1]] = hashBytes(data)
		}
	case "/usr/bin/mv":
		r.installed = true
	case systemBinary:
		if !r.installed {
			return CommandResult{Code: 127, Stderr: "helper missing"}, nil
		}
		if args[3] == "internal-capabilities" {
			switch r.capabilityMode {
			case "legacy":
				return CommandResult{Code: 2, Stderr: "ERROR: unknown internal command\n"}, nil
			case "unsupported":
				return CommandResult{Stdout: `{"protocol_version":1,"features":[]}`}, nil
			case "malformed":
				return CommandResult{Stdout: `{"protocol_version":1}`}, nil
			case "broken":
				return CommandResult{Code: 2, Stderr: "cannot read capabilities"}, nil
			case "newer":
				return CommandResult{Stdout: `{"protocol_version":9,"features":["future-feature","idempotent-file-restore"]}`}, nil
			default:
				data, err := json.Marshal(currentRecoveryCapabilities())
				return CommandResult{Stdout: string(data)}, err
			}
		}
		var payload systemPayload
		if err := json.Unmarshal(cmd.Input, &payload); err != nil {
			return CommandResult{}, err
		}
		if args[3] == "internal-write" {
			if payload.Path == systemBinary {
				r.capabilityMode = ""
				r.temporary[systemBinary] = systemDigest(payload.Data)
			}
			data, err := json.Marshal(systemJournal{Path: payload.Path, RunID: payload.RunID, SHA256: systemDigest(payload.Data)})
			return CommandResult{Stdout: string(data)}, err
		}
		if args[3] == "internal-restore" && payload.Path == systemBinary {
			r.installed = false
		}
	}
	return CommandResult{}, nil
}

func recoveryHelperContexts(t *testing.T, installed bool) (*Context, *Context, *recoveryHelperRunner) {
	t.Helper()
	desktop, _ := storageContext(t)
	runner := &recoveryHelperRunner{installed: installed, temporary: map[string]string{}}
	desktop.Runner = runner
	desktop.Report["system_changes"] = []map[string]any{
		{"path": systemBinary, "run_id": "desktop-run", "initial_install": true},
		{"path": "/usr/share/wayland-sessions/myarch-buildkit.desktop", "run_id": "desktop-run"},
	}
	if err := desktop.Save(); err != nil {
		t.Fatal(err)
	}
	greeter := *desktop
	greeter.RunDir = filepath.Join(desktop.Home, "greeter-run")
	greeter.ReportPath = filepath.Join(greeter.RunDir, "profile-report.json")
	greeter.Report = map[string]any{
		"schema_version": 1,
		"greeter":        map[string]any{"root_boot_journal": true, "run_id": "greeter-run"},
		"system_changes": []map[string]any{{"path": "/etc/greetd/config.toml", "run_id": "greeter-run"}},
	}
	if err := greeter.Save(); err != nil {
		t.Fatal(err)
	}
	return desktop, &greeter, runner
}

func TestRecoveryStageOrdersRetainSharedHelper(t *testing.T) {
	for _, desktopFirst := range []bool{true, false} {
		t.Run(fmt.Sprint("desktop-first-", desktopFirst), func(t *testing.T) {
			desktop, greeter, runner := recoveryHelperContexts(t, true)
			order := []*Context{desktop, greeter}
			if !desktopFirst {
				order = []*Context{greeter, desktop}
			}
			for _, context := range order {
				if err := RestoreContext(context); err != nil {
					t.Fatal(err)
				}
			}
			if !runner.installed || object(greeter.Report["greeter"])["boot_transition_restored"] != true {
				t.Fatal("restoring one stage broke recovery of another")
			}
			record := reportRecords(desktop.Report, "system_changes")[0]
			if record["retained_dependency"] != "shared privileged recovery helper" {
				t.Fatal("helper retention was not recorded")
			}
			for _, cmd := range runner.calls {
				if strings.Contains(strings.Join(cmd.Args, " "), "/usr/bin/install") {
					t.Fatal("existing trusted helper was unexpectedly replaced")
				}
			}
		})
	}
}

func TestRecoveryMissingHelperUsesIndependentSupportJournal(t *testing.T) {
	desktop, greeter, runner := recoveryHelperContexts(t, false)
	originalRecords, _ := json.Marshal(greeter.Report["system_changes"])
	if err := RestoreContext(greeter); err != nil {
		t.Fatal(err)
	}
	if !runner.installed {
		t.Fatal("missing helper was not repaired")
	}
	supportPath := filepath.Join(stateRoot(desktop), "support", "privileged-helper", "profile-report.json")
	support := map[string]any{}
	if err := ReadJSON(supportPath, &support); err != nil {
		t.Fatal(err)
	}
	changes := reportRecords(support, "system_changes")
	if len(changes) != 1 || changes[0]["path"] != systemBinary || changes[0]["run_id"] == "greeter-run" {
		t.Fatalf("helper repair reused stage journal identity: %#v", changes)
	}
	for _, record := range reportRecords(greeter.Report, "system_changes") {
		delete(record, "restored")
	}
	afterRecords, _ := json.Marshal(greeter.Report["system_changes"])
	if string(afterRecords) != string(originalRecords) {
		t.Fatal("repair changed the original stage's backup records")
	}
	if err := RestoreContext(desktop); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryHelperCapabilitiesControlReplacement(t *testing.T) {
	for _, mode := range []string{"capable", "newer", "legacy", "unsupported", "malformed", "broken", "unsafe"} {
		t.Run(mode, func(t *testing.T) {
			_, greeter, runner := recoveryHelperContexts(t, true)
			runner.capabilityMode = mode
			runner.unsafe = mode == "unsafe"
			err := ensureRecoveryHelper(greeter)
			wantError := mode == "malformed" || mode == "broken" || mode == "unsafe"
			if (err != nil) != wantError {
				t.Fatalf("unexpected capability result for %s: %v", mode, err)
			}
			upgraded, probed := false, false
			for _, cmd := range runner.calls {
				if len(cmd.Args) > 3 && cmd.Args[2] == systemBinary {
					upgraded = upgraded || cmd.Args[3] == "internal-write"
					probed = probed || cmd.Args[3] == "internal-capabilities"
				}
			}
			wantUpgrade := mode == "legacy" || mode == "unsupported"
			if upgraded != wantUpgrade {
				t.Fatalf("unexpected helper replacement for %s: %v", mode, upgraded)
			}
			if mode == "unsafe" && probed {
				t.Fatal("unsafe installed binary was executed before ownership verification")
			}
			supportPath := filepath.Join(stateRoot(greeter), "support", "privileged-helper", "profile-report.json")
			if exists(supportPath) != wantUpgrade {
				t.Fatal("capability handling created or omitted an unexpected support journal")
			}
			if wantUpgrade {
				support := map[string]any{}
				if err := ReadJSON(supportPath, &support); err != nil {
					t.Fatal(err)
				}
				for _, record := range reportRecords(support, "system_changes") {
					if record["run_id"] == "greeter-run" {
						t.Fatal("helper upgrade reused the stage's privileged journal identity")
					}
				}
			}
		})
	}
}

func TestPrivilegedRecoveryRestartsAfterOriginalBytesBeforeCompletion(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy-journal-", legacy), func(t *testing.T) {
			root := requireRootFS(t)
			path := "/etc/greetd/config.toml"
			destination := rooted(root, path)
			if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, []byte("original"), 0640); err != nil {
				t.Fatal(err)
			}
			payload := systemPayload{Path: path, Data: []byte("managed"), Mode: 0644, RunID: "restore-restart"}
			journal, err := privilegedWrite(root, 1000, payload)
			if err != nil {
				t.Fatal(err)
			}
			if journal.OriginalSHA256 != systemDigest([]byte("original")) {
				t.Fatal("immutable original checksum was not recorded")
			}
			if legacy {
				journal.OriginalSHA256 = ""
				journalPath, _, err := rootJournalPaths(root, 1000, payload.RunID, path)
				if err != nil {
					t.Fatal(err)
				}
				if err := saveSystemJournal(journalPath, journal); err != nil {
					t.Fatal(err)
				}
			}
			// Inject the crash state at the privileged write/completion boundary:
			// original bytes are durable but the root journal is still pending.
			if err := rootAtomic(destination, []byte("original"), 0640, 0, 0); err != nil {
				t.Fatal(err)
			}
			if err := privilegedRestore(root, 1000, payload); err != nil {
				t.Fatalf("restart rejected its own original: %v", err)
			}
			if err := privilegedRestore(root, 1000, payload); err != nil {
				t.Fatal("completed restore is not idempotent", err)
			}
		})
	}
}

func TestPrivilegedRecoveryRefusesCorruptedOriginal(t *testing.T) {
	root := requireRootFS(t)
	path := "/etc/greetd/config.toml"
	destination := rooted(root, path)
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	payload := systemPayload{Path: path, Data: []byte("managed"), Mode: 0644, RunID: "restore-corrupt"}
	journal, err := privilegedWrite(root, 1000, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal.Backup, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	storageRequireError(t, privilegedRestore(root, 1000, payload), "original system backup changed")
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "managed" {
		t.Fatal("corrupt original replaced destination")
	}
}

func TestRecoveryRootAtomicReportsDirectorySyncFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory read permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0300); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	path := filepath.Join(dir, "journal")
	if err := rootAtomic(path, []byte("original"), 0600, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("write reported success without being able to sync the renamed journal's directory")
	}
}
