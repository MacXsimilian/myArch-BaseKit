package main

import (
	"os"
	"os/exec"
	"testing"
)

type nativeNiriRunner struct{ binary string }

func (r nativeNiriRunner) Run(args ...string) error { return exec.Command(r.binary, args[1:]...).Run() }
func (r nativeNiriRunner) Output(args ...string) ([]byte, error) {
	return exec.Command(r.binary, args[1:]...).CombinedOutput()
}

// Set BASEKIT_NIRI_BINARY to a Niri executable for an actual parser check.
// This validates only; it never starts a compositor or changes user files.
func TestNativeNiriProfile(t *testing.T) {
	binary := os.Getenv("BASEKIT_NIRI_BINARY")
	if binary == "" {
		t.Skip("set BASEKIT_NIRI_BINARY to run the native Niri parser")
	}
	for _, scale := range []string{"4/3", "1", "3/2"} {
		s := defaultSettings()
		s.Scale = scale
		changes, err := BuildProfile(t.TempDir(), s)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateProfile(changes, nativeNiriRunner{binary}); err != nil {
			t.Fatalf("scale %s: %v", scale, err)
		}
		changes[0].Data = append(changes[0].Data, []byte("\nthis-is-not-a-niri-setting {}\n")...)
		if err := ValidateProfile(changes, nativeNiriRunner{binary}); err == nil {
			t.Fatal("native parser accepted an invalid config")
		}
	}
}
