package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootctlJSONContractThroughRealProcess(t *testing.T) {
	// Literal wire fields follow systemd v259 boot_entry_to_json, independently
	// of kernelEntry's JSON tags. This is a representative contract fixture, not
	// a claim that a host boot was exercised.
	// https://github.com/systemd/systemd/blob/v259/src/shared/bootspec.c#L1784-L1837
	const output = `[
 {"type":"type1","source":"esp","id":"linux.conf","path":"/boot/loader/entries/linux.conf","root":"/boot","title":"Arch Linux","linux":"/vmlinuz-linux","initrd":["/intel-ucode.img","/initramfs-linux.img"],"options":"root=UUID=fixture rw","isReported":true,"isDefault":true,"isSelected":false},
 {"type":"type1","source":"esp","id":"linux-cachyos.conf","root":"/boot","linux":"/vmlinuz-linux-cachyos","initrd":["/initramfs-linux-cachyos.img"],"isDefault":false,"isSelected":true}
]`
	bin := t.TempDir()
	script := "#!/bin/sh\n[ \"$*\" = 'list --json=short --no-pager' ] || exit 91\n[ \"$LC_ALL\" = C ] || exit 92\n[ \"$SYSTEMD_COLORS\" = 0 ] || exit 93\ncat <<'BOOTCTL_JSON'\n" + output + "\nBOOTCTL_JSON\n"
	// Use an absolute cat so PATH can contain only the fake bootctl.
	script = strings.Replace(script, "cat <<", "/usr/bin/cat <<", 1)
	if err := os.WriteFile(filepath.Join(bin, "bootctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	c := packageTestContext(t, ExecRunner{})
	entries, err := (kernelManager{c: c, root: "/"}).entries()
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := entryFor(entries, "linux")
	if err != nil || fallback.Root != "/boot" || !fallback.Default || fallback.Selected || len(fallback.Initrd) != 2 {
		t.Fatalf("fallback contract decoded incorrectly: %+v %v", fallback, err)
	}
	target, err := entryFor(entries, "linux-cachyos")
	if err != nil || target.Default || !target.Selected {
		t.Fatalf("selected-entry contract decoded incorrectly: %+v %v", target, err)
	}
}

func TestPackageStageRunsTransactionsAndChecksThroughRealProcesses(t *testing.T) {
	for _, missingCommand := range []bool{false, true} {
		name := "complete"
		if missingCommand {
			name = "installed-package-with-missing-command"
		}
		t.Run(name, func(t *testing.T) {
			bin := t.TempDir()
			log := filepath.Join(t.TempDir(), "commands.log")
			// Every command is replaced by a local harmless executable. The real
			// stageRun, resolver, command runner and tooling verification still run.
			const script = `#!/bin/sh
name=${0##*/}
printf '%s %s\n' "$name" "$*" >> "$MYARCH_BOUNDARY_LOG"
case "$name" in
 sudo) exec "$@" ;;
 pacman)
  operation=$1
  for package do :; done
  case "$operation" in
   -Si) printf 'Name : %s\n' "$package" ;;
   -Q) printf '%s 1.0-1\n' "$package" ;;
   -Syu|-Su) : ;;
   *) exit 94 ;;
  esac ;;
 podman|podman-compose) printf 'version 1.0\n' ;;
 *) exit 95 ;;
esac
`
			for _, command := range []string{"sudo", "pacman", "podman", "podman-compose", "pasta", "newuidmap", "newgidmap", "getsubids"} {
				if missingCommand && command == "podman-compose" {
					continue
				}
				if err := os.WriteFile(filepath.Join(bin, command), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin)
			t.Setenv("MYARCH_BOUNDARY_LOG", log)
			c := packageTestContext(t, ExecRunner{})
			c.Options.PackageGroup = "containers"
			c.Quiet = true
			err := stageRun(c, "packages", "containers", false, []string{"packages"})
			if missingCommand {
				if err == nil || !strings.Contains(err.Error(), "MISSING COMMAND: podman-compose") {
					t.Fatalf("stage did not propagate runtime verification failure: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			calls := string(data)
			for _, want := range []string{"sudo pacman -Syu --needed -- base-devel git\n", "sudo pacman -Su --needed -- podman podman-compose passt shadow\n", "podman --version\n"} {
				if strings.Count(calls, want) != 1 {
					t.Fatalf("expected one %q in subprocess calls:\n%s", want, calls)
				}
			}
			if !missingCommand && !strings.Contains(calls, "podman-compose version\n") {
				t.Fatalf("stage skipped installed-tool verification:\n%s", calls)
			}
		})
	}
}
