package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var StageOrder = []string{"preflight", "packages", "defaults", "shell", "containers", "laptop", "desktop", "greeter", "verify", "cleanup"}
var GroupOrder = []string{"core", "utilities", "development", "containers"}

func DefaultSettings() Settings {
	return Settings{2, map[string]bool{"core": true, "utilities": true, "development": true, "containers": true}, LaptopSettings{"4/3", "hu", true}}
}

func parseScaleValue(value any) (float64, error) {
	text := fmt.Sprint(value)
	if !regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)?|[0-9]+/[0-9]+)$`).MatchString(text) {
		return 0, errors.New("internal_scale must be a decimal or ratio, such as 4/3")
	}
	numbers := strings.Split(text, "/")
	n, err := strconv.ParseFloat(numbers[0], 64)
	if len(numbers) == 2 {
		denom, e := strconv.ParseFloat(numbers[1], 64)
		if e != nil || denom == 0 {
			return 0, errors.New("invalid scale denominator")
		}
		n /= denom
	}
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) || n < .25 || n > 8 {
		return 0, errors.New("internal_scale must be between 0.25 and 8")
	}
	return n, nil
}
func LoadSettings(path string) (Settings, error) {
	settings := DefaultSettings()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return settings, err
		}
		var raw map[string]json.RawMessage
		if err = json.Unmarshal(data, &raw); err != nil || raw == nil {
			return settings, errors.New("settings must be a JSON object")
		}
		if _, isReport := raw["format"]; isReport {
			data, raw, err = settingsFromRunReport(raw)
			if err != nil {
				return settings, err
			}
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&settings); err != nil {
			return settings, err
		}
		for key, value := range raw {
			if string(value) == "null" {
				return settings, fmt.Errorf("%s cannot be null", key)
			}
		}
		var typed struct {
			Groups map[string]json.RawMessage `json:"package_groups"`
			Laptop map[string]json.RawMessage `json:"laptop"`
		}
		_ = json.Unmarshal(data, &typed)
		for key, value := range typed.Groups {
			if string(value) != "true" && string(value) != "false" {
				return settings, fmt.Errorf("package group %s must be a boolean", key)
			}
		}
		for key, value := range typed.Laptop {
			if string(value) == "null" {
				return settings, fmt.Errorf("laptop.%s cannot be null", key)
			}
		}
	}
	if settings.SchemaVersion != 2 {
		return settings, errors.New("settings schema_version must be 2")
	}
	if !settings.PackageGroups["core"] {
		return settings, errors.New("core package group must remain enabled")
	}
	for key := range settings.PackageGroups {
		if !contains(GroupOrder, key) {
			return settings, fmt.Errorf("unknown package group: %s", key)
		}
	}
	if _, err := parseScaleValue(settings.Laptop.InternalScale); err != nil {
		return settings, err
	}
	if !regexp.MustCompile(`^[a-z]{2,3}$`).MatchString(settings.Laptop.InternalKeyboard) {
		return settings, errors.New("internal_keyboard must be one XKB layout identifier, such as hu")
	}
	return settings, nil
}

// Saved run settings are complete snapshots. Ordinary settings files may still
// omit fields and inherit defaults, but a damaged snapshot must not do so.
func settingsFromRunReport(report map[string]json.RawMessage) ([]byte, map[string]json.RawMessage, error) {
	var format string
	if json.Unmarshal(report["format"], &format) != nil || format != RunReportFormat {
		return nil, nil, errors.New("unsupported run report format")
	}
	var version int
	if json.Unmarshal(report["schema_version"], &version) != nil || version != RunReportSchemaVersion {
		return nil, nil, fmt.Errorf("run report schema_version must be %d", RunReportSchemaVersion)
	}
	data := report["settings"]
	var snapshot map[string]json.RawMessage
	if json.Unmarshal(data, &snapshot) != nil || snapshot == nil {
		return nil, nil, errors.New("run report settings must be a JSON object")
	}
	for _, key := range []string{"schema_version", "package_groups", "laptop"} {
		if _, present := snapshot[key]; !present {
			return nil, nil, fmt.Errorf("run report settings snapshot is missing %s", key)
		}
	}
	for _, required := range []struct {
		key    string
		fields []string
	}{{"package_groups", GroupOrder}, {"laptop", []string{"internal_scale", "internal_keyboard", "natural_scroll"}}} {
		var fields map[string]json.RawMessage
		if json.Unmarshal(snapshot[required.key], &fields) != nil || fields == nil {
			return nil, nil, fmt.Errorf("run report settings.%s must be a JSON object", required.key)
		}
		for _, field := range required.fields {
			if _, present := fields[field]; !present {
				return nil, nil, fmt.Errorf("run report settings snapshot is missing %s.%s", required.key, field)
			}
		}
	}
	return data, snapshot, nil
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func xdgPath(name, fallback string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		value = fallback
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("%s must be absolute", name)
	}
	return filepath.Clean(value), nil
}

func NewContext(runDir string, settings Settings, opts Options, runner Runner) (*Context, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(home) {
		return nil, errors.New("HOME must be absolute")
	}
	c := &Context{Home: filepath.Clean(home), RunDir: runDir, Settings: settings, Options: opts, Runner: runner, Inventory: map[string]any{}, Report: map[string]any{"schema_version": 1, "status": "pending", "warnings": []string{}, "notes": []string{}, "file_changes": []any{}, "system_changes": []any{}}}
	for _, row := range []struct {
		name, fallback string
		target         *string
	}{{"XDG_CONFIG_HOME", filepath.Join(home, ".config"), &c.ConfigHome}, {"XDG_STATE_HOME", filepath.Join(home, ".local/state"), &c.StateHome}, {"XDG_DATA_HOME", filepath.Join(home, ".local/share"), &c.DataHome}, {"XDG_CACHE_HOME", filepath.Join(home, ".cache"), &c.CacheHome}} {
		*row.target, err = xdgPath(row.name, row.fallback)
		if err != nil {
			return nil, err
		}
	}
	c.BinDir = filepath.Join(home, ".local/bin")
	c.Binary = filepath.Join(c.BinDir, "myarch-buildkit")
	if c.RunDir == "" {
		c.RunDir = filepath.Join(c.StateHome, "myarch-buildkit")
	}
	if !filepath.IsAbs(c.RunDir) {
		return nil, errors.New("run directory must be absolute")
	}
	c.ReportPath = filepath.Join(c.RunDir, "profile-report.json")
	if c.Runner == nil {
		c.Runner = ExecRunner{}
	}
	return c, nil
}
