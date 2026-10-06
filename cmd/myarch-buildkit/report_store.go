package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
)

const RunReportFormat = "myarch-buildkit-run"
const RunReportSchemaVersion = 3

// Setup and login already hold the state lock. This also keeps independent
// report updates in the same process from replacing another scope's journal.
var runReportMutex sync.Mutex

func ValidateReportScope(scope string) error {
	if scope == "_run" || contains(StageOrder, scope) {
		return nil
	}
	for _, group := range GroupOrder {
		if scope == "packages-"+group {
			return nil
		}
	}
	return fmt.Errorf("unsupported report scope: %s", scope)
}

func readRunReport(path string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	document := map[string]json.RawMessage{}
	if err := ReadJSON(path, &document); err != nil {
		return nil, nil, err
	}
	var format string
	var version int
	if json.Unmarshal(document["format"], &format) != nil || format != RunReportFormat || json.Unmarshal(document["schema_version"], &version) != nil || version != RunReportSchemaVersion {
		return nil, nil, fmt.Errorf("unsupported run report format: %s", path)
	}
	profiles := map[string]json.RawMessage{}
	if json.Unmarshal(document["profiles"], &profiles) != nil || profiles == nil {
		return nil, nil, fmt.Errorf("invalid profile journals in run report: %s", path)
	}
	return document, profiles, nil
}

func validateProfileJSON(data []byte) error {
	profile := map[string]json.RawMessage{}
	var version int
	if json.Unmarshal(data, &profile) != nil || profile == nil || json.Unmarshal(profile["schema_version"], &version) != nil || version != 1 {
		return fmt.Errorf("unsupported report schema")
	}
	return nil
}

// Empty scope retains the standalone report format used by earlier releases.
func ReadProfileReport(path, scope string, out any) error {
	if scope == "" {
		return ReadJSON(path, out)
	}
	if err := ValidateReportScope(scope); err != nil {
		return err
	}
	_, profiles, err := readRunReport(path)
	if err != nil {
		return err
	}
	data, found := profiles[scope]
	if !found {
		return fmt.Errorf("no %s profile in %s", scope, path)
	}
	if err := validateProfileJSON(data); err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func WriteProfileReport(path, scope string, value any) error {
	if scope == "" {
		return WriteJSON(path, value)
	}
	if err := ValidateReportScope(scope); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := validateProfileJSON(data); err != nil {
		return err
	}
	runReportMutex.Lock()
	defer runReportMutex.Unlock()
	document, profiles, err := readRunReport(path)
	if err != nil {
		return err
	}
	profiles[scope] = data
	document["profiles"], err = json.Marshal(profiles)
	if err != nil {
		return err
	}
	if err := updateRunProfileStatus(document, profiles, scope, data); err != nil {
		return err
	}
	return WriteJSON(path, document)
}

// Login and explicit restoration update journals after the setup command has
// finished. Reflect those terminal changes without erasing the setup history.
func updateRunProfileStatus(document, profiles map[string]json.RawMessage, scope string, data []byte) error {
	profile := map[string]any{}
	if err := json.Unmarshal(data, &profile); err != nil {
		return err
	}
	status, _ := profile["status"].(string)
	if scope == "_run" || !contains([]string{"applied_before_login", "restored", "restore_partial"}, status) {
		return nil
	}
	stages := map[string]map[string]any{}
	if json.Unmarshal(document["stages"], &stages) != nil || stages == nil || stages[scope] == nil {
		return nil
	}
	stages[scope]["status"] = status
	var err error
	document["stages"], err = json.Marshal(stages)
	if err != nil {
		return err
	}
	pending, allTerminal, anyRestored, anyPartial, anyStaged := false, true, false, false, false
	for _, phase := range []string{"laptop", "desktop"} {
		stage, selected := stages[phase]
		if !selected {
			continue
		}
		anyStaged = true
		journal := map[string]any{}
		if raw, found := profiles[phase]; found {
			if err := json.Unmarshal(raw, &journal); err != nil {
				return err
			}
		}
		phaseStatus, _ := journal["status"].(string)
		pendingStatus, _ := object(journal["pending_configuration"])["status"].(string)
		pending = pending || stage["status"] == "pending_login" || phaseStatus == "pending_login" || contains([]string{"pending", "applying"}, pendingStatus)
		allTerminal = allTerminal && contains([]string{"applied_before_login", "restored"}, phaseStatus)
		anyRestored = anyRestored || phaseStatus == "restored"
		anyPartial = anyPartial || phaseStatus == "restore_partial"
	}
	if anyStaged && !pending {
		document["pending_configuration"], err = json.Marshal(map[string]any{"status": "none"})
		if err != nil {
			return err
		}
		var overall string
		if json.Unmarshal(document["status"], &overall) == nil && overall == "pending_login" {
			if anyPartial {
				overall = "partial"
			} else if allTerminal {
				overall = "completed_with_manual_checks"
				if anyRestored {
					overall = "configuration_restored"
				}
			}
			document["status"], err = json.Marshal(overall)
		}
	}
	return err
}

// Fresh-read merging preserves profile journals written between summary saves.
func WriteRunSummary(path string, summary map[string]any) error {
	runReportMutex.Lock()
	defer runReportMutex.Unlock()
	document, _, err := readRunReport(path)
	if err != nil {
		return err
	}
	for key, value := range summary {
		if key == "profiles" {
			continue
		}
		data, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return marshalErr
		}
		if key == "format" && value != RunReportFormat || key == "schema_version" && mapJSONNumber(value) != RunReportSchemaVersion {
			return fmt.Errorf("cannot change run report format")
		}
		document[key] = data
	}
	return WriteJSON(path, document)
}

func resolveUnifiedRestore(c *Context) (bool, error) {
	if c.Options.RestoreProfile == "" && c.Options.RestoreStage == "" {
		return c.ReportScope != "", nil
	}
	root := c.RunDir
	if c.Options.StageRunDir != "" {
		root = c.Options.StageRunDir
	}
	if c.Options.RestoreProfile != "" {
		root = c.Options.RestoreProfile
	}
	path := filepath.Join(root, "report.json")
	if err := securePath(path, true); err != nil {
		return true, err
	}
	if !exists(path) {
		return false, nil
	}
	document, profiles, err := readRunReport(path)
	if err != nil {
		return true, err
	}
	scope := c.Options.RestoreStage
	if c.Options.RestoreProfile != "" {
		scope = "desktop"
	}
	if !contains([]string{"defaults", "shell", "containers", "laptop", "desktop", "greeter", "cleanup"}, scope) {
		return true, fmt.Errorf("unsupported restore stage: %s", scope)
	}
	if _, found := profiles[scope]; !found {
		return true, fmt.Errorf("no %s profile in %s; restore a specific completed stage with --restore-stage and --stage-run-dir", scope, root)
	}
	stages := map[string]map[string]json.RawMessage{}
	if json.Unmarshal(document["stages"], &stages) != nil || stages == nil {
		return true, fmt.Errorf("invalid stages in run report: %s", path)
	}
	var artifacts string
	if json.Unmarshal(stages[scope]["artifacts"], &artifacts) != nil || artifacts == "" {
		return true, fmt.Errorf("no %s restoration artifacts in %s", scope, path)
	}
	expected := filepath.Join(stateRoot(c), "runs", filepath.Base(root), scope)
	if !filepath.IsAbs(artifacts) || filepath.Clean(artifacts) != artifacts || artifacts != expected {
		return true, fmt.Errorf("unsafe restoration artifacts: %s", artifacts)
	}
	if err := securePath(artifacts, true); err != nil {
		return true, err
	}
	c.RunDir, c.ReportPath, c.ReportScope = artifacts, path, scope
	return true, nil
}
