package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestKernelImagesCanBeReadAsNormalUserWithoutWeakeningUserGuard(t *testing.T) {
	// A packaged, root-owned binary exercises the same ownership contract as
	// kernel images without creating or changing any host boot files.
	path := "/usr/bin/true"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if owner, ok := info.Sys().(*syscall.Stat_t); !ok || owner.Uid != 0 {
		if os.Getenv("MYARCH_REQUIRE_ROOT_OWNED_FIXTURES") == "1" {
			t.Fatal("required root-owned system fixture is unavailable; system ownership is remapped")
		}
		t.Skip("requires ordinary root-owned system files; this environment remaps system ownership")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := trustedKernelImageHash(path)
	if err != nil || digest != hashBytes(data) {
		t.Fatalf("root-owned image read failed: digest=%q error=%v", digest, err)
	}
	if os.Getuid() != 0 {
		if _, err := FileHash(path); err == nil {
			t.Fatal("user configuration hashing unexpectedly permits root-owned files")
		}
	}
}

func TestKernelImagesRejectUntrustedFilesAndNonregularPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image")
	if err := os.WriteFile(path, []byte("untrusted"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, "/usr", "relative-image"} {
		if _, err := trustedKernelImageHash(candidate); err == nil {
			t.Fatalf("accepted untrusted or nonregular image %s", candidate)
		}
	}
}

func TestKernelVerificationDefaultsToTrustedSystemReader(t *testing.T) {
	f := newKernelFixture(t)
	f.manager.imageHash = nil
	_, err := f.manager.verifyKernel("linux-cachyos-lts", f.entries[0], nil)
	if err == nil || !strings.Contains(err.Error(), "system path") {
		t.Fatalf("production verifier accepted user-owned fixtures: %v", err)
	}
}
