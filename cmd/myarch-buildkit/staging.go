package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

var disableUnits = []string{"dms.service", "cachyos-dms-direct.service", "noctalia.service", "noctalia-shell.service", "waybar.service", "mako.service", "dunst.service", "swaync.service", "hyprpolkitagent.service", "nm-applet.service", "cachyos-laptop-session.service", "cachyos-laptop-idle.service"}

type StageFile struct {
	Path            string         `json:"path"`
	Source          string         `json:"source"`
	Mode            uint32         `json:"mode"`
	SHA256          string         `json:"sha256"`
	BeforeSHA256    string         `json:"before_sha256"`
	OwnershipMarker string         `json:"ownership_marker,omitempty"`
	JSONUpdates     map[string]any `json:"json_updates"`
}
type Manifest struct {
	Version     int            `json:"version"`
	Status      string         `json:"status"`
	Phase       string         `json:"phase"`
	Report      string         `json:"report"`
	ReportScope string         `json:"report_scope,omitempty"`
	Files       []StageFile    `json:"files"`
	Validation  map[string]any `json:"validation"`
}
type QueueItem struct {
	Manifest    string `json:"manifest"`
	Phase       string `json:"phase"`
	Report      string `json:"report,omitempty"`
	ReportScope string `json:"report_scope,omitempty"`
}
type pendingQueue struct {
	Version int         `json:"version"`
	Items   []QueueItem `json:"items"`
}

// A missing bundle is still queued configuration. Checking it must not silently
// discard the entry or imply that the current desktop is configured correctly.
type PendingConfigurationError struct {
	Phase, Manifest, Report, Reason, RecoveryCommand string
	RecoveryCommands                                 []string
	Cause                                            error
}

func (e *PendingConfigurationError) Error() string {
	return fmt.Sprintf("pending %s manifest is missing: %s; the next-login queue still references this unavailable run", e.Phase, e.Manifest)
}
func (e *PendingConfigurationError) Unwrap() error { return e.Cause }

func stateRoot(c *Context) string    { return filepath.Join(c.StateHome, "myarch-buildkit") }
func queuePath(c *Context) string    { return filepath.Join(stateRoot(c), "pending-login.json") }
func manifestPath(c *Context) string { return filepath.Join(c.RunDir, "pending-profile.json") }
func payloadPath(manifest, destination string) string {
	return filepath.Join(filepath.Dir(manifest), "pending-files", pathID(destination), filepath.Base(destination))
}
func validateQueueItem(c *Context, item QueueItem) error {
	if !contains([]string{"laptop", "desktop"}, item.Phase) || !filepath.IsAbs(item.Manifest) || filepath.Clean(item.Manifest) != item.Manifest || !inside(c.Home, item.Manifest) || filepath.Base(item.Manifest) != "pending-profile.json" {
		return fmt.Errorf("unsupported pending login entry: %s", item.Manifest)
	}
	if err := securePath(item.Manifest, true); err != nil {
		return err
	}
	if item.Report != "" || item.ReportScope != "" {
		if item.ReportScope != item.Phase || !filepath.IsAbs(item.Report) || filepath.Clean(item.Report) != item.Report || !inside(c.Home, item.Report) || filepath.Base(item.Report) != "report.json" {
			return fmt.Errorf("unsupported pending report reference: %s", item.Report)
		}
		return securePath(item.Report, true)
	}
	return nil
}
func pendingReportReference(item QueueItem) (string, string, string) {
	if item.ReportScope != "" {
		return item.Report, item.ReportScope, filepath.Dir(item.Report)
	}
	return filepath.Join(filepath.Dir(item.Manifest), "profile-report.json"), "", filepath.Dir(item.Manifest)
}
func pendingRecoveryCommand(c *Context, item QueueItem) string {
	binary, err := currentExecutable()
	if err != nil || binary == "" {
		binary = c.Binary
	}
	if binary == "" {
		binary = "myarch-buildkit"
	}
	reportPath, scope, restoreDir := pendingReportReference(item)
	report := map[string]any{}
	if ReadProfileReport(reportPath, scope, &report) == nil && mapJSONNumber(report["schema_version"]) == 1 && object(report["pending_configuration"])["manifest"] == item.Manifest && contains(stringList(report["completed_phases"]), item.Phase) {
		return planShellWords([]string{binary, "--restore-stage", item.Phase, "--stage-run-dir", restoreDir})
	}
	if _, err = os.Lstat(item.Manifest); errors.Is(err, os.ErrNotExist) {
		return planShellWords([]string{binary, "--cancel-pending", item.Phase})
	}
	return planShellWords([]string{binary, "--restore-stage", item.Phase, "--stage-run-dir", restoreDir})
}
func missingPendingError(c *Context, item QueueItem, cause error) *PendingConfigurationError {
	reportPath, _, _ := pendingReportReference(item)
	e := &PendingConfigurationError{Phase: item.Phase, Manifest: item.Manifest, Report: reportPath, Reason: "The saved next-login queue still references this missing manifest.", Cause: cause}
	// Desktop validation can include the laptop payload. Remove that dependency
	// explicitly before dropping the laptop queue entry.
	if item.Phase == "laptop" {
		if items, err := PendingItems(c); err == nil {
			for _, dependency := range items {
				if dependency.Phase == "desktop" && validateQueueItem(c, dependency) == nil {
					e.RecoveryCommands = append(e.RecoveryCommands, pendingRecoveryCommand(c, dependency))
				}
			}
		}
	}
	e.RecoveryCommands = append(e.RecoveryCommands, pendingRecoveryCommand(c, item))
	e.RecoveryCommand = e.RecoveryCommands[0]
	return e
}
func readPendingManifest(c *Context, item QueueItem) (Manifest, error) {
	manifest := Manifest{}
	if err := validateQueueItem(c, item); err != nil {
		return manifest, err
	}
	if err := ReadJSON(item.Manifest, &manifest); err != nil {
		if _, statErr := os.Lstat(item.Manifest); errors.Is(statErr, os.ErrNotExist) {
			return manifest, missingPendingError(c, item, statErr)
		}
		return manifest, fmt.Errorf("cannot read pending %s manifest %s: %w", item.Phase, item.Manifest, err)
	}
	return manifest, nil
}
func PendingItems(c *Context) ([]QueueItem, error) {
	queue := pendingQueue{Version: 1, Items: []QueueItem{}}
	if err := securePath(queuePath(c), true); err != nil {
		return nil, err
	}
	if exists(queuePath(c)) {
		if err := ReadJSON(queuePath(c), &queue); err != nil {
			return nil, err
		}
	}
	if queue.Version != 1 || queue.Items == nil {
		return nil, fmt.Errorf("unsupported pending login queue")
	}
	return queue.Items, nil
}
func writeQueue(c *Context, items []QueueItem) error {
	if items == nil {
		items = []QueueItem{}
	}
	return WriteJSON(queuePath(c), pendingQueue{Version: 1, Items: items})
}
func (c *Context) StartStaging() error {
	if c.CheckOnly {
		return fmt.Errorf("a check cannot stage configuration")
	}
	version := 1
	if c.ReportScope != "" {
		version = 2
	}
	manifest := Manifest{Version: version, Status: "building", Report: c.ReportPath, ReportScope: c.ReportScope, Files: []StageFile{}, Validation: map[string]any{}}
	if exists(manifestPath(c)) {
		if err := ReadJSON(manifestPath(c), &manifest); err != nil {
			return err
		}
		if manifest.Version != version || manifest.Status != "building" && manifest.Status != "pending" {
			return fmt.Errorf("this stage was already applied; create a fresh run")
		}
		if manifest.Report != c.ReportPath || manifest.ReportScope != c.ReportScope {
			return fmt.Errorf("unexpected report in staging manifest")
		}
	}
	c.Pending = &manifest
	return nil
}
func (c *Context) View(path string) string {
	if c.Pending != nil {
		for _, record := range c.Pending.Files {
			if record.Path == path {
				return record.Source
			}
		}
	}
	items, err := PendingItems(c)
	if err != nil {
		return path
	}
	for i := len(items) - 1; i >= 0; i-- {
		manifest := Manifest{}
		if ReadJSON(items[i].Manifest, &manifest) != nil {
			continue
		}
		for _, record := range manifest.Files {
			if record.Path == path && record.Source == payloadPath(items[i].Manifest, path) {
				return record.Source
			}
		}
	}
	return path
}
func (c *Context) stageWrite(path string, data []byte, mode os.FileMode) error {
	if !inside(c.Home, path) {
		return fmt.Errorf("pending destination must be within the user home: %s", path)
	}
	if err := securePath(path, true); err != nil {
		return err
	}
	if err := c.checkOwned(path); err != nil {
		return err
	}
	before, err := FileHash(path)
	if err != nil {
		return err
	}
	record := StageFile{Path: path, Source: payloadPath(manifestPath(c), path), Mode: uint32(mode.Perm()), SHA256: hashBytes(data), BeforeSHA256: before, OwnershipMarker: c.ownershipPath(path)}
	index := -1
	for i, old := range c.Pending.Files {
		if old.Path == path {
			index = i
			record.BeforeSHA256 = old.BeforeSHA256
			break
		}
	}
	if isDMSJSON(path) {
		old := map[string]any{}
		if exists(path) {
			if err := ReadJSON(path, &old); err != nil || old == nil {
				return fmt.Errorf("expected DMS object %s: %w", path, err)
			}
		}
		after := map[string]any{}
		if err = json.Unmarshal(data, &after); err != nil || after == nil {
			return fmt.Errorf("expected DMS JSON object: %s", path)
		}
		record.JSONUpdates = map[string]any{}
		for key, value := range after {
			a, _ := json.Marshal(old[key])
			b, _ := json.Marshal(value)
			_, present := old[key]
			if !present || string(a) != string(b) {
				record.JSONUpdates[key] = value
			}
		}
	}
	if err = AtomicWrite(record.Source, data, mode); err != nil {
		return err
	}
	if index >= 0 {
		c.Pending.Files[index] = record
	} else {
		c.Pending.Files = append(c.Pending.Files, record)
	}
	return WriteJSON(manifestPath(c), c.Pending)
}
func isDMSJSON(path string) bool {
	if !contains([]string{"settings.json", "session.json", "clsettings.json", "plugin_settings.json"}, filepath.Base(path)) {
		return false
	}
	for _, part := range strings.Split(filepath.Clean(path), string(os.PathSeparator)) {
		if part == "DankMaterialShell" {
			return true
		}
	}
	return false
}
func (c *Context) OverrideJSON(path string, updates map[string]any) error {
	if c.Pending == nil {
		return fmt.Errorf("JSON overrides require staging")
	}
	for i := range c.Pending.Files {
		record := &c.Pending.Files[i]
		if record.Path != path {
			continue
		}
		if record.JSONUpdates == nil {
			return fmt.Errorf("not a staged DMS JSON file: %s", path)
		}
		for key, value := range updates {
			record.JSONUpdates[key] = value
		}
		return WriteJSON(manifestPath(c), c.Pending)
	}
	return fmt.Errorf("no staged JSON file: %s", path)
}
func (c *Context) Publish(phase string) error {
	if err := c.Err(); err != nil {
		return err
	}
	if c.Pending == nil {
		return fmt.Errorf("no configuration is being staged")
	}
	if c.ReportScope != "" && c.ReportScope != phase {
		return fmt.Errorf("pending report scope does not match stage: %s", phase)
	}
	items, err := PendingItems(c)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Manifest == manifestPath(c) {
			continue
		}
		old, readErr := readPendingManifest(c, item)
		if readErr != nil {
			return readErr
		}
		if old.Phase == phase {
			return fmt.Errorf("restore/cancel the earlier pending %s stage first: %s", phase, item.Manifest)
		}
	}
	c.Pending.Phase = phase
	c.Pending.Status = "pending"
	if err = WriteJSON(manifestPath(c), c.Pending); err != nil {
		return err
	}
	filtered := []QueueItem{}
	for _, item := range items {
		if item.Manifest != manifestPath(c) {
			filtered = append(filtered, item)
		}
	}
	item := QueueItem{Manifest: manifestPath(c), Phase: phase}
	if c.ReportScope != "" {
		item.Report, item.ReportScope = c.ReportPath, c.ReportScope
	}
	if err = validateQueueItem(c, item); err != nil {
		return err
	}
	filtered = append(filtered, item)
	if err = writeQueue(c, filtered); err != nil {
		return err
	}
	c.ensureReport()
	phases := stringList(c.Report["completed_phases"])
	if !contains(phases, phase) {
		phases = append(phases, phase)
	}
	c.Report["completed_phases"] = phases
	c.Report["pending_configuration"] = map[string]any{"status": "pending", "manifest": manifestPath(c), "activation": "Log out and select myarch-buildkit at the next login", "validation": c.Pending.Validation}
	c.Report["running_desktop"] = map[string]any{"changed_by_this_stage": false, "readiness": "not asserted"}
	c.Report["status"] = "pending_login"
	return c.Save()
}
func hyprlandBinary(c *Context) (string, error) {
	if binary, ok := c.Inventory["hyprland_binary"].(string); ok && binary != "" {
		return binary, nil
	}
	for _, path := range []string{"/usr/bin/Hyprland", "/usr/bin/hyprland"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("Hyprland is missing")
}

var configError = regexp.MustCompile(`(?i)config error|parse error`)
var extraDMS = regexp.MustCompile(`\bdms\s+run\b|cachyos-desktop-session|cachyos-dms-guard`)

func ValidateDesktop(c *Context) error {
	if c.Pending == nil {
		return fmt.Errorf("desktop validation requires staging")
	}
	record := object(c.Report["profile_config"])
	paths := map[string]string{}
	for _, key := range []string{"main_config", "managed_config", "custom_config"} {
		value, ok := record[key].(string)
		if !ok || value == "" {
			return fmt.Errorf("missing profile_config %s", key)
		}
		paths[key] = value
	}
	main, managed, custom := paths["main_config"], paths["managed_config"], paths["custom_config"]
	extension := ".conf"
	if filepath.Ext(main) == ".lua" {
		extension = ".lua"
	}
	dependencies := map[string]any{}
	managedSource := c.View(managed)
	data, err := os.ReadFile(managedSource)
	if err != nil {
		return err
	}
	hash, err := FileHash(managedSource)
	if err != nil {
		return err
	}
	dependencies[managedSource] = hash
	text := string(data)
	if fragment, ok := record["laptop_fragment"].(string); ok && fragment != "" {
		source := c.View(fragment)
		hash, err = FileHash(source)
		if err != nil {
			return err
		}
		if hash == "" {
			return fmt.Errorf("missing laptop fragment: %s", source)
		}
		dependencies[source] = hash
		text = strings.ReplaceAll(text, fragment, source)
	}
	managedCandidate := filepath.Join(c.RunDir, "validate-managed"+extension)
	if err = AtomicWrite(managedCandidate, []byte(text), 0600); err != nil {
		return err
	}
	hash, err = FileHash(managedCandidate)
	if err != nil {
		return err
	}
	dependencies[managedCandidate] = hash
	source := c.View(main)
	data, err = os.ReadFile(source)
	if err != nil {
		return err
	}
	hash, err = FileHash(source)
	if err != nil {
		return err
	}
	dependencies[source] = hash
	text = strings.ReplaceAll(string(data), managed, managedCandidate)
	customSource := c.View(custom)
	text = strings.ReplaceAll(text, custom, customSource)
	candidate := filepath.Join(c.RunDir, "validate"+extension)
	if err = AtomicWrite(candidate, []byte(text), 0600); err != nil {
		return err
	}
	binary, err := hyprlandBinary(c)
	if err != nil {
		return err
	}
	help := c.Try(binary, "--help")
	if !strings.Contains(help.Stdout+help.Stderr, "--verify-config") {
		return fmt.Errorf("Hyprland lacks native config verification")
	}
	result := c.Try(binary, "--verify-config", "--config", candidate)
	if result.Code != 0 || configError.MatchString(result.Stdout+result.Stderr) {
		return fmt.Errorf("pending Hyprland config is invalid: %s", result.Stdout+result.Stderr)
	}
	customData, err := os.ReadFile(customSource)
	if err != nil {
		return err
	}
	if extraDMS.Match(customData) {
		return fmt.Errorf("custom overrides contain an additional DMS startup; resolve it before staging")
	}
	customHash, err := FileHash(customSource)
	if err != nil {
		return err
	}
	candidateHash, err := FileHash(candidate)
	if err != nil {
		return err
	}
	c.Pending.Validation = map[string]any{"native_config_verified": true, "binary": binary, "candidate": candidate, "main_config": main, "managed_config": managed, "custom_config": custom, "candidate_dependencies": dependencies, "candidate_sha256": candidateHash, "custom_sha256": customHash, "custom_source": customSource, "startup_method": "Hyprland start hook -> dms run", "dms_schema_verified": true}
	return WriteJSON(manifestPath(c), c.Pending)
}
func mergeJSON(target, updates map[string]any) {
	for key, value := range updates {
		incoming, ok := value.(map[string]any)
		current, has := target[key].(map[string]any)
		if ok && has {
			mergeJSON(current, incoming)
		} else {
			target[key] = value
		}
	}
}
func plannedBytes(record StageFile) ([]byte, error) {
	if err := securePath(record.Path, true); err != nil {
		return nil, err
	}
	hash, err := FileHash(record.Source)
	if err != nil {
		return nil, err
	}
	if hash != record.SHA256 {
		return nil, fmt.Errorf("pending payload changed: %s", record.Source)
	}
	data, err := os.ReadFile(record.Source)
	if err != nil {
		return nil, err
	}
	if record.JSONUpdates != nil {
		current := map[string]any{}
		if exists(record.Path) {
			if err = ReadJSON(record.Path, &current); err != nil || current == nil {
				return nil, fmt.Errorf("DMS JSON is not an object: %s", record.Path)
			}
		}
		mergeJSON(current, record.JSONUpdates)
		return jsonBytes(current)
	}
	actual, err := FileHash(record.Path)
	if err != nil {
		return nil, err
	}
	if actual != record.BeforeSHA256 {
		return nil, fmt.Errorf("changed since staging; cancel and restage: %s", record.Path)
	}
	return data, nil
}

type filePlan struct {
	Record StageFile
	Data   []byte
}
type loginPlan struct {
	Path     string
	Manifest Manifest
	Report   map[string]any
	Files    []filePlan
}

func validatePlans(c *Context, items []QueueItem) ([]loginPlan, error) {
	plans := []loginPlan{}
	destinations := map[string]bool{}
	desktop := false
	for _, item := range items {
		desktop = desktop || item.Phase == "desktop"
	}
	for _, item := range items {
		manifest, err := readPendingManifest(c, item)
		if err != nil {
			return nil, err
		}
		if (manifest.Version != 1 && manifest.Version != 2) || manifest.Status != "pending" || manifest.Phase != item.Phase {
			return nil, fmt.Errorf("pending bundle is not ready: %s", item.Manifest)
		}
		if manifest.Version == 1 && (manifest.Report != filepath.Join(filepath.Dir(item.Manifest), "profile-report.json") || manifest.ReportScope != "" || item.Report != "" || item.ReportScope != "") {
			return nil, fmt.Errorf("unexpected manifest/report path")
		}
		if manifest.Version == 2 && (manifest.Report != item.Report || manifest.ReportScope != item.Phase || manifest.ReportScope != item.ReportScope) {
			return nil, fmt.Errorf("unexpected unified report reference")
		}
		plan := loginPlan{Path: item.Manifest, Manifest: manifest, Report: map[string]any{}, Files: []filePlan{}}
		for _, record := range manifest.Files {
			if !filepath.IsAbs(record.Path) || !inside(c.Home, record.Path) || destinations[record.Path] {
				return nil, fmt.Errorf("unsupported/overlapping destination: %s", record.Path)
			}
			if record.Source != payloadPath(item.Manifest, record.Path) {
				return nil, fmt.Errorf("unexpected payload path")
			}
			if record.OwnershipMarker != "" {
				expected := filepath.Join(stateRoot(c), "managed-files", pathID(record.Path)+".json")
				if record.OwnershipMarker != expected {
					return nil, fmt.Errorf("unexpected ownership marker")
				}
				if err := securePath(expected, true); err != nil {
					return nil, err
				}
			}
			data, err := plannedBytes(record)
			if err != nil {
				return nil, err
			}
			destinations[record.Path] = true
			plan.Files = append(plan.Files, filePlan{record, data})
		}
		validation := manifest.Validation
		if boolValue(validation["requires_desktop_stage"]) && !desktop {
			return nil, fmt.Errorf("stage desktop before applying laptop settings; installed profile does not load the static fragment")
		}
		if item.Phase == "desktop" || boolValue(validation["native_config_verified"]) {
			if !boolValue(validation["native_config_verified"]) {
				return nil, fmt.Errorf("pending desktop was not natively verified")
			}
			for _, pair := range [][2]string{{"candidate", "candidate_sha256"}, {"custom_source", "custom_sha256"}} {
				path, ok := validation[pair[0]].(string)
				expected, ok2 := validation[pair[1]].(string)
				if !ok || !ok2 {
					return nil, fmt.Errorf("incomplete native validation")
				}
				actual, err := FileHash(path)
				if err != nil {
					return nil, err
				}
				if actual != expected {
					return nil, fmt.Errorf("validation candidate or custom overrides changed; restage desktop")
				}
			}
			for path, expected := range object(validation["candidate_dependencies"]) {
				actual, err := FileHash(path)
				if err != nil {
					return nil, err
				}
				if actual != expected {
					return nil, fmt.Errorf("validated dependency changed: %s", path)
				}
			}
			binary, _ := validation["binary"].(string)
			if binary != "/usr/bin/Hyprland" && binary != "/usr/bin/hyprland" {
				if observed, ok := c.Inventory["hyprland_binary"].(string); !ok || observed != binary {
					return nil, fmt.Errorf("unsupported validation binary")
				}
			}
			result, err := c.Run(binary, "--verify-config", "--config", str(validation["candidate"]))
			if err != nil {
				return nil, err
			}
			if configError.MatchString(result.Stdout + result.Stderr) {
				return nil, fmt.Errorf("pending Hyprland configuration is invalid")
			}
			version, err := c.Run("/usr/bin/dms", "version")
			if err != nil {
				return nil, err
			}
			if !regexp.MustCompile(`\bv?1\.6\.2\b`).MatchString(version.Stdout) {
				return nil, fmt.Errorf("DMS version changed since staging; restage with a verified schema")
			}
		}
		if err := ReadProfileReport(manifest.Report, manifest.ReportScope, &plan.Report); err != nil {
			return nil, err
		}
		if mapJSONNumber(plan.Report["schema_version"]) != 1 {
			return nil, fmt.Errorf("unsupported report schema")
		}
		plans = append(plans, plan)
	}
	return plans, nil
}
func ValidatePending(c *Context) error {
	items, err := PendingItems(c)
	if err != nil {
		return err
	}
	_, err = validatePlans(c, items)
	return err
}

var noDesktopProcRoot = "/proc"

func NoDesktop(c *Context) error {
	entries, err := os.ReadDir(noDesktopProcRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		numeric := true
		for _, r := range entry.Name() {
			numeric = numeric && r >= '0' && r <= '9'
		}
		if !numeric {
			continue
		}
		path := filepath.Join(noDesktopProcRoot, entry.Name())
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != os.Getuid() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(path, "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(string(data), "\x00")
		if len(args) == 0 || args[0] == "" {
			continue
		}
		name := filepath.Base(args[0])
		if contains([]string{"Hyprland", "hyprland", "qs", "quickshell"}, name) || name == "dms" && len(args) > 1 && args[1] == "run" {
			return fmt.Errorf("log out of graphical sessions before applying/restoring desktop files: %s", name)
		}
	}
	return nil
}
func commitFile(c *Context, plan *loginPlan, path string, data []byte, mode os.FileMode) error {
	saved := filepath.Join(filepath.Dir(plan.Path), "backups", "profile", "login-files", pathID(path))
	oldHash, err := FileHash(path)
	if err != nil {
		return err
	}
	record := map[string]any{"path": path, "backup": nil, "mode": nil, "sha256": hashBytes(data), "previous_sha256": nullableHash(oldHash), "write_journal_version": 2}
	if oldHash != "" {
		oldData, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err = AtomicWrite(saved, oldData, info.Mode()); err != nil {
			return err
		}
		record["backup"] = saved
		record["mode"] = int(info.Mode().Perm())
	}
	appendRecord(plan.Report, "file_changes", record)
	if err = WriteProfileReport(plan.Manifest.Report, plan.Manifest.ReportScope, plan.Report); err != nil {
		return err
	}
	return AtomicWrite(path, data, mode)
}
func ApplyPending(c *Context) error {
	if err := NoDesktop(c); err != nil {
		return err
	}
	items, err := PendingItems(c)
	if err != nil {
		return err
	}
	plans, err := validatePlans(c, items)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		return nil
	}
	for _, unit := range disableUnits {
		result, err := c.Run("systemctl", "--user", "show", unit, "--property=ActiveState", "--value")
		if err != nil {
			return err
		}
		if contains([]string{"active", "activating", "deactivating"}, strings.TrimSpace(result.Stdout)) {
			return fmt.Errorf("a desktop service is still running; no files applied: %s", unit)
		}
	}
	for i := range plans {
		plan := &plans[i]
		pending := object(plan.Report["pending_configuration"])
		pending["status"] = "applying"
		plan.Report["pending_configuration"] = pending
		plan.Manifest.Status = "applying"
		if err = WriteJSON(plan.Path, plan.Manifest); err != nil {
			return err
		}
		if err = WriteProfileReport(plan.Manifest.Report, plan.Manifest.ReportScope, plan.Report); err != nil {
			return err
		}
		for _, file := range plan.Files {
			record := file.Record
			if err = commitFile(c, plan, record.Path, file.Data, os.FileMode(record.Mode)); err != nil {
				return err
			}
			if record.OwnershipMarker != "" {
				data, e := jsonBytes(map[string]any{"path": record.Path, "sha256": hashBytes(file.Data)})
				if e != nil {
					return e
				}
				if err = commitFile(c, plan, record.OwnershipMarker, data, 0600); err != nil {
					return err
				}
			}
		}
		if plan.Manifest.Phase == "desktop" {
			for _, unit := range disableUnits {
				result, err := c.Run("systemctl", "--user", "show", unit, "--property=UnitFileState", "--value")
				if err != nil {
					return err
				}
				old := strings.TrimSpace(result.Stdout)
				if contains([]string{"enabled", "enabled-runtime", "linked", "linked-runtime"}, old) {
					appendRecord(plan.Report, "login_service_changes", map[string]any{"unit": unit, "old": old})
					if err = WriteProfileReport(plan.Manifest.Report, plan.Manifest.ReportScope, plan.Report); err != nil {
						return err
					}
					if _, err = c.Run("systemctl", "--user", "disable", unit); err != nil {
						return err
					}
				}
			}
			data, e := jsonBytes(map[string]any{"report": plan.Manifest.Report, "report_scope": plan.Manifest.ReportScope, "main_config": plan.Manifest.Validation["main_config"], "startup_method": "Hyprland start hook -> dms run"})
			if e != nil {
				return e
			}
			if err = commitFile(c, plan, filepath.Join(stateRoot(c), "active-profile.json"), data, 0600); err != nil {
				return err
			}
		}
		pending["status"] = "applied"
		plan.Report["status"] = "applied_before_login"
		plan.Report["running_desktop"] = map[string]any{"changed_by_this_stage": false, "readiness": "not yet started"}
		if err = WriteProfileReport(plan.Manifest.Report, plan.Manifest.ReportScope, plan.Report); err != nil {
			return err
		}
		plan.Manifest.Status = "applied"
		if err = WriteJSON(plan.Path, plan.Manifest); err != nil {
			return err
		}
		remaining := []QueueItem{}
		for _, item := range items {
			if item.Manifest != plan.Path {
				remaining = append(remaining, item)
			}
		}
		items = remaining
		if err = writeQueue(c, items); err != nil {
			return err
		}
	}
	_, err = c.Run("systemctl", "--user", "daemon-reload")
	return err
}
func cancelPending(c *Context) error {
	items, err := PendingItems(c)
	if err != nil {
		return err
	}
	own := manifestPath(c)
	pending := object(c.Report["pending_configuration"])
	completed := stringList(c.Report["completed_phases"])
	for _, item := range items {
		if item.Manifest != own && item.Phase == "desktop" && pending["status"] == "pending" && len(completed) == 1 && completed[0] == "laptop" {
			return fmt.Errorf("cancel dependent pending desktop stage before cancelling laptop")
		}
	}
	remaining := []QueueItem{}
	for _, item := range items {
		if item.Manifest != own {
			remaining = append(remaining, item)
		}
	}
	if err = writeQueue(c, remaining); err != nil {
		return err
	}
	if exists(own) {
		manifest := Manifest{}
		if err = ReadJSON(own, &manifest); err != nil {
			return err
		}
		manifest.Status = "cancelled"
		if err = WriteJSON(own, manifest); err != nil {
			return err
		}
	}
	pending["status"] = "cancelled"
	c.Report["pending_configuration"] = pending
	return nil
}

// CancelMissingPending is an explicit escape hatch for lost staging data. It
// removes only missing manifests from the queue; it does not restore files,
// apply configuration, or assert that the active desktop is ready.
func CancelMissingPending(c *Context, phase string) error {
	if c.CheckOnly {
		return fmt.Errorf("a check cannot cancel pending configuration")
	}
	if !contains([]string{"laptop", "desktop"}, phase) {
		return fmt.Errorf("cancel-pending expects laptop or desktop")
	}
	if err := securePath(stateRoot(c), false); err != nil {
		return err
	}
	items, err := PendingItems(c)
	if err != nil {
		return err
	}
	remaining := []QueueItem{}
	removed := 0
	for _, item := range items {
		if err = validateQueueItem(c, item); err != nil {
			return err
		}
		if item.Phase != phase {
			remaining = append(remaining, item)
			continue
		}
		if _, err = os.Lstat(item.Manifest); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return fmt.Errorf("pending %s manifest is still present; restore/cancel it explicitly with:\n  %s", phase, pendingRecoveryCommand(c, item))
		}
		removed++
	}
	if removed == 0 {
		return fmt.Errorf("no missing pending %s configuration is queued", phase)
	}
	if phase == "laptop" {
		for _, item := range items {
			if item.Phase == "desktop" {
				commands := []string{pendingRecoveryCommand(c, item)}
				for _, laptop := range items {
					if laptop.Phase == "laptop" {
						commands = append(commands, pendingRecoveryCommand(c, laptop))
					}
				}
				return fmt.Errorf("cancel/restore the dependent pending desktop stage before cancelling laptop, in this order:\n  %s", strings.Join(commands, "\n  "))
			}
		}
	}
	return writeQueue(c, remaining)
}
func LoginMain(c *Context) error {
	root := stateRoot(c)
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	lockPath := filepath.Join(root, "setup.lock")
	if err := securePath(lockPath, true); err != nil {
		return err
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	fail := func(cause error) error {
		_ = WriteJSON(filepath.Join(root, "login-status.json"), map[string]any{"status": "blocked", "error": cause.Error(), "follow_up": "Use a TTY to inspect reports and explicitly restore/cancel the named stage."})
		return fmt.Errorf("desktop login blocked: %w", cause)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(fmt.Errorf("another setup or login operation is running"))
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err = ApplyPending(c); err != nil {
		return fail(err)
	}
	active := map[string]any{}
	if err = ReadJSON(filepath.Join(root, "active-profile.json"), &active); err != nil {
		return fail(err)
	}
	main, ok := active["main_config"].(string)
	if !ok || !inside(c.Home, main) {
		return fail(fmt.Errorf("unsupported active profile"))
	}
	if err = securePath(main, false); err != nil {
		return fail(err)
	}
	binary, err := hyprlandBinary(c)
	if err != nil {
		return fail(err)
	}
	result, err := c.Run(binary, "--verify-config", "--config", main)
	if err != nil {
		return fail(err)
	}
	if configError.MatchString(result.Stdout + result.Stderr) {
		return fail(fmt.Errorf("active Hyprland configuration is invalid"))
	}
	if err = WriteJSON(filepath.Join(root, "login-status.json"), map[string]any{"status": "configuration_applied", "running_desktop": "not yet started", "main_config": main}); err != nil {
		return err
	}
	// Exec the compositor for the real runner. The close-on-exec lock remains
	// held until the kernel has started Hyprland, avoiding a second login race.
	// Fake runners use the observable command path below for isolated tests.
	nativeRunner := false
	switch c.Runner.(type) {
	case ExecRunner, *ExecRunner:
		nativeRunner = true
	}
	if nativeRunner {
		if err = c.Err(); err != nil {
			return fail(err)
		}
		overrides := map[string]string{"XDG_CURRENT_DESKTOP": "Hyprland", "XDG_SESSION_DESKTOP": "Hyprland", "XDG_SESSION_TYPE": "wayland"}
		environment := []string{}
		for _, entry := range os.Environ() {
			key := strings.SplitN(entry, "=", 2)[0]
			if _, replace := overrides[key]; !replace {
				environment = append(environment, entry)
			}
		}
		for key, value := range overrides {
			environment = append(environment, key+"="+value)
		}
		return fail(syscall.Exec(binary, []string{binary, "--config", main}, environment))
	}
	// Release the setup lock before starting the long-lived desktop. No helper
	// remains running and DMS starts once through the verified compositor hook.
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		return err
	}
	_, err = c.Command(Command{Args: []string{binary, "--config", main}, Env: map[string]string{"XDG_CURRENT_DESKTOP": "Hyprland", "XDG_SESSION_DESKTOP": "Hyprland", "XDG_SESSION_TYPE": "wayland"}, Interactive: true})
	return err
}
