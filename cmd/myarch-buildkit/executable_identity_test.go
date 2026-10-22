package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func executableBytesForTest(t *testing.T) []byte {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRunningExecutableHashMatchesImageBytes(t *testing.T) {
	want := fmt.Sprintf("%x", sha256.Sum256(executableBytesForTest(t)))
	got, err := runningExecutableHash()
	if err != nil || got != want {
		t.Fatalf("running executable SHA256 = %q, %v; want %q", got, err, want)
	}
}

func TestVersionReportsRunningExecutableHash(t *testing.T) {
	want := fmt.Sprintf("%x", sha256.Sum256(executableBytesForTest(t)))
	assertVersionExecutableHash(t, want)
}

func assertVersionExecutableHash(t *testing.T, want string) {
	t.Helper()
	output := captureCLIOutput(t, func() {
		code, err := dispatch([]string{"--version"}, &storageRunner{})
		if code != 0 || err != nil {
			t.Fatalf("--version failed: %d, %v", code, err)
		}
	})
	if expected := "myarch-buildkit " + BuildID + "\nExecutable SHA256: " + want + "\n"; output != expected {
		t.Fatalf("--version output = %q; want %q", output, expected)
	}
}

func TestRunningExecutableHashUsesLoadedImageAfterReplacement(t *testing.T) {
	const childVariable = "MYARCH_EXECUTABLE_HASH_TEST_CHILD"
	if want := os.Getenv(childVariable); want != "" {
		path, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := FileHash(path); err == nil {
			t.Fatal("configuration hashing accepted the writable executable ancestor")
		}
		if err := os.Rename(path, path+".started"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("replacement bytes, not the running image\n"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path + ".started"); err != nil {
			t.Fatal(err)
		}
		got, err := runningExecutableHash()
		if err != nil || got != want {
			t.Fatalf("hash changed after replacement: %q, %v; want %q", got, err, want)
		}
		assertVersionExecutableHash(t, want)
		return
	}
	data := executableBytesForTest(t)
	want := fmt.Sprintf("%x", sha256.Sum256(data))
	directory := filepath.Join(t.TempDir(), "group-writable")
	if err := os.Mkdir(directory, 0770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0770); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "image.test")
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(path, "-test.run=^TestRunningExecutableHashUsesLoadedImageAfterReplacement$")
	command.Env = append(os.Environ(), childVariable+"="+want)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("executable replacement regression: %v\n%s", err, output)
	}
}

func TestRunStagesRecordsRunningExecutableHash(t *testing.T) {
	c := mainStageContext(t)
	want := fmt.Sprintf("%x", sha256.Sum256(executableBytesForTest(t)))
	captureCLIOutput(t, func() {
		code, err := RunStages(c, nil)
		if code != 0 || err != nil {
			t.Fatalf("empty stage run: %d, %v", code, err)
		}
	})
	provenance := object(mainStageSummary(t, c)["provenance"])
	if str(provenance["sha256"]) != want {
		t.Fatalf("incorrect executable provenance: %v", provenance)
	}
	if _, exists := provenance["hash_error"]; exists {
		t.Fatalf("unexpected executable hash error: %v", provenance)
	}
}
