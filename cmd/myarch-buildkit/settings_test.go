package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsPartialMergePreservesExplicitFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	data := `{"package_groups":{"utilities":false},"laptop":{"natural_scroll":false}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.PackageGroups["utilities"] || s.Laptop.NaturalScroll {
		t.Fatal("explicit false was replaced")
	}
	if !s.PackageGroups["development"] || !s.PackageGroups["core"] || s.Laptop.InternalKeyboard != "hu" || s.Laptop.InternalScale != "4/3" {
		t.Fatalf("defaults lost: %+v", s)
	}
}

func TestSettingsRejectInvalidDocuments(t *testing.T) {
	cases := []string{`null`, `[]`, `{"schema_version":1}`, `{"package_groups":{"core":false}}`, `{"package_groups":{"unknown":true}}`, `{"package_groups":{"utilities":null}}`, `{"package_groups":null}`, `{"laptop":null}`, `{"laptop":{"natural_scroll":null}}`, `{"laptop":{"natural_scroll":"true"}}`, `{"laptop":{"internal_scale":true}}`, `{"laptop":{"internal_scale":"4/0"}}`, `{"laptop":{"internal_keyboard":"hu,us"}}`, `{"laptop":{"lid_action":"suspend"}}`, `{"theme":"custom"}`, `{} {}`}
	for _, data := range cases {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadSettings(path); err == nil {
				t.Fatal("invalid settings accepted")
			}
		})
	}
}

func TestScaleRatioAndBounds(t *testing.T) {
	for _, value := range []any{"4/3", 1.3333333333333333} {
		got, err := parseScaleValue(value)
		if err != nil || math.Abs(got-4.0/3) > 1e-12 {
			t.Fatalf("%v: %v %v", value, got, err)
		}
	}
	for _, value := range []any{"0", ".5", "4/0", "1/-2", "1; rm", "NaN", 9, false, nil} {
		if _, err := parseScaleValue(value); err == nil {
			t.Fatalf("accepted %v", value)
		}
	}
}

func TestContextRejectsRelativeXDGDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if _, err := NewContext("", DefaultSettings(), Options{}, nil); err == nil {
		t.Fatal("relative XDG path accepted")
	}
}
