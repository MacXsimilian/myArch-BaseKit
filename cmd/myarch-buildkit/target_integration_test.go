package main

import (
	"encoding/json"
	"os"
	"testing"
)

// Explicitly enabled, read-only acceptance checks for a disposable CachyOS VM
// or a target machine. Installation, logout and reboots are separate deliberate
// steps; this test never drives a package transaction or switches boot entries.
func TestTargetSessionAndBootContracts(t *testing.T) {
	if os.Getenv("MYARCH_TARGET_TEST") != "1" {
		t.Skip("set MYARCH_TARGET_TEST=1 on the target after the documented setup/login steps")
	}
	phase := os.Getenv("MYARCH_TARGET_PHASE")
	if phase != "staged" && phase != "active" {
		t.Fatal("MYARCH_TARGET_PHASE must explicitly select staged or active")
	}
	settingsPath := os.Getenv("MYARCH_TARGET_SETTINGS")
	if settingsPath == "" {
		t.Fatal("MYARCH_TARGET_SETTINGS must identify the settings file or saved run report used for setup")
	}
	settings, err := LoadSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewContext("", settings, Options{Command: "check", SettingsPath: settingsPath}, ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	c.CheckOnly, c.Quiet = true, true
	if err = CheckPlatform(c, false); err != nil {
		t.Fatal(err)
	}
	err = checkAll(c)
	data, marshalErr := json.MarshalIndent(c.Report, "", "  ")
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	t.Log(string(data))
	if err != nil {
		t.Fatal(err)
	}
	want := "verified"
	if phase == "staged" {
		want = "pending_login"
	}
	if c.Report["status"] != want {
		t.Fatalf("phase %s: wanted %s, got %v", phase, want, c.Report["status"])
	}

	boot := os.Getenv("MYARCH_TARGET_BOOT")
	if boot == "" {
		return
	}
	if !contains([]string{"fallback", "trial", "promoted"}, boot) {
		t.Fatal("MYARCH_TARGET_BOOT must be fallback, trial or promoted")
	}
	if kernelChannel(settings) == "unchanged" {
		t.Fatal("boot acceptance requires an opted-in kernel channel")
	}
	record := object(c.Report["kernel"])
	if record["fallback"] == record["target"] || record["entry"] == record["fallback_entry"] {
		t.Fatal("target and fallback are not independent")
	}
	wantRelease := record["installed_release"]
	if boot == "fallback" {
		fallback, ok := record["fallback"].(string)
		if !ok {
			t.Fatal("missing fallback identity")
		}
		wantRelease, err = (kernelManager{c: c, root: "/"}).moduleRelease(fallback)
		if err != nil {
			t.Fatal(err)
		}
	}
	if record["running"] != wantRelease {
		t.Fatalf("boot %s: running %v, want %v", boot, record["running"], wantRelease)
	}
	wantDefault := boot == "promoted"
	if record["default"] != wantDefault {
		t.Fatalf("boot %s: target default=%v, want %v", boot, record["default"], wantDefault)
	}
}
