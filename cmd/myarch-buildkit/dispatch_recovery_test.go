package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func dispatchFixtureEnvironment(t *testing.T, c *Context) {
	t.Helper()
	for key, value := range map[string]string{
		"HOME": c.Home, "XDG_CONFIG_HOME": c.ConfigHome,
		"XDG_STATE_HOME": c.StateHome, "XDG_DATA_HOME": c.DataHome,
		"XDG_CACHE_HOME": c.CacheHome,
	} {
		t.Setenv(key, value)
	}
}

func TestDispatchRestoreIgnoresBrokenCurrentSettings(t *testing.T) {
	for _, settings := range []string{"not JSON", `{"schema_version":1}`} {
		t.Run(settings, func(t *testing.T) {
			c, runner := storageContext(t)
			dispatchFixtureEnvironment(t, c)
			path := filepath.Join(c.Home, ".bashrc")
			storagePut(t, path, "original")
			if err := c.Write(path, []byte("managed"), 0600); err != nil {
				t.Fatal(err)
			}
			storagePut(t, filepath.Join(c.ConfigHome, "myarch-buildkit/settings.json"), settings)
			checked := false
			code, err := dispatchWithPlatform([]string{"--restore-stage", "shell", "--stage-run-dir", c.RunDir}, runner, func(*Context, bool) error {
				checked = true
				return nil
			})
			if code != 0 || err != nil || !checked || storageRead(t, path) != "original" {
				t.Fatalf("restore failed: code=%d error=%v platform=%v", code, err, checked)
			}
		})
	}
}

func TestDispatchCancelMissingPendingIgnoresBrokenSettings(t *testing.T) {
	c, runner := storageContext(t)
	dispatchFixtureEnvironment(t, c)
	storagePut(t, filepath.Join(c.ConfigHome, "myarch-buildkit/settings.json"), "broken")
	if err := writeQueue(c, []QueueItem{{Phase: "laptop", Manifest: manifestPath(c)}}); err != nil {
		t.Fatal(err)
	}
	code, err := dispatchWithPlatform([]string{"--cancel-pending", "laptop"}, runner, func(_ *Context, apply bool) error {
		if apply {
			t.Fatal("pending cancellation requested setup prerequisites")
		}
		return nil
	})
	if code != 0 || err != nil {
		t.Fatalf("cancel: %d %v", code, err)
	}
	items, err := PendingItems(c)
	if err != nil || len(items) != 0 {
		t.Fatalf("queue remains: %v %v", items, err)
	}
}

func TestDispatchRecoveryStillChecksPlatformAndJournalSafety(t *testing.T) {
	c, runner := storageContext(t)
	dispatchFixtureEnvironment(t, c)
	storagePut(t, filepath.Join(c.ConfigHome, "myarch-buildkit/settings.json"), "broken")
	refusal := errors.New("platform refused")
	args := []string{"--restore-stage", "shell", "--stage-run-dir", c.RunDir}
	if code, err := dispatchWithPlatform(args, runner, func(*Context, bool) error { return refusal }); code != 2 || !errors.Is(err, refusal) {
		t.Fatalf("platform protection bypassed: %d %v", code, err)
	}
	storagePut(t, c.ReportPath, `{"schema_version":999}`)
	if code, err := dispatchWithPlatform(args, runner, func(*Context, bool) error { return nil }); code != 2 || err == nil {
		t.Fatalf("invalid recovery journal accepted: %d %v", code, err)
	}
}

func TestDispatchJSONInitializationFailuresAreOneDocument(t *testing.T) {
	for _, kind := range []string{"missing", "malformed", "config-home", "state-home"} {
		t.Run(kind, func(t *testing.T) {
			c, runner := storageContext(t)
			dispatchFixtureEnvironment(t, c)
			args := []string{"check", "--json"}
			switch kind {
			case "missing":
				args = append(args, "--settings", filepath.Join(c.Home, "missing.json"))
			case "malformed":
				storagePut(t, filepath.Join(c.ConfigHome, "myarch-buildkit/settings.json"), "broken")
			case "config-home":
				t.Setenv("XDG_CONFIG_HOME", "relative")
			case "state-home":
				t.Setenv("XDG_STATE_HOME", "relative")
			}
			output := captureCLIOutput(t, func() {
				code, err := dispatchWithPlatform(args, runner, func(*Context, bool) error {
					t.Fatal("initialization failure reached platform checks")
					return nil
				})
				var shown *printedError
				if code != 2 || !errors.As(err, &shown) {
					t.Fatalf("failure lost: %d %v", code, err)
				}
			})
			var report map[string]any
			if err := json.Unmarshal([]byte(output), &report); err != nil {
				t.Fatalf("not exactly one JSON document: %v: %q", err, output)
			}
			if report["status"] != "incomplete" || report["check_error"] == "" || len(runner.calls) != 0 {
				t.Fatalf("bad diagnostics or external operation: %v %v", report, runner.calls)
			}
			if _, err := os.Stat(c.RunDir); !os.IsNotExist(err) {
				t.Fatalf("check created a run directory: %v", err)
			}
		})
	}
}
