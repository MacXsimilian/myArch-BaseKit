package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type kernelFixture struct {
	manager   kernelManager
	runner    *packageTestRunner
	packages  map[string]string
	entries   []kernelEntry
	versions  map[string]string
	releases  map[string]string
	upgraded  bool
	fail      string
	secure    bool
	space     string
	downgrade bool
	stale     bool
}

func newKernelFixture(t *testing.T) *kernelFixture {
	t.Helper()
	f := &kernelFixture{
		packages: map[string]string{"linux-cachyos-lts": "6.12.1-1", "linux-cachyos-lts-headers": "6.12.1-1"},
		versions: map[string]string{"linux-cachyos-rc": "7.3.rc6-2", "linux-cachyos-rc-headers": "7.3.rc6-2"},
		releases: map[string]string{"linux-cachyos-lts": "6.12.1-cachyos-lts", "linux-cachyos-rc": "7.3.0-rc6-cachyos-rc"},
		space:    "2147483648",
	}
	f.runner = &packageTestRunner{}
	c := packageTestContext(t, f.runner)
	c.Settings = DefaultSettings()
	c.Settings.KernelChannel = "rc"
	c.Quiet = true
	f.manager = kernelManager{c, t.TempDir()}
	for _, pkg := range []string{"linux-cachyos-lts", "linux-cachyos-rc"} {
		entry := kernelEntry{ID: pkg + ".conf", Type: "type1", Root: "/boot", Linux: "/vmlinuz-" + pkg, Initrd: []string{"/initramfs-" + pkg + ".img"}, Default: pkg == "linux-cachyos-lts"}
		f.entries = append(f.entries, entry)
		storagePut(t, f.manager.path(filepath.Join("/boot", entry.Linux)), pkg)
		storagePut(t, f.manager.path(filepath.Join("/boot", entry.Initrd[0])), "initramfs")
		storagePut(t, f.manager.path("/usr/lib/modules/"+f.releases[pkg]+"/vmlinuz"), pkg)
	}
	f.runner.reply = func(cmd Command) (CommandResult, error) {
		text := packageCmd(cmd)
		if f.fail != "" && strings.Contains(text, f.fail) {
			return CommandResult{Code: 1, Stderr: "fixture failure"}, nil
		}
		ok := func(text string) (CommandResult, error) { return CommandResult{Stdout: text}, nil }
		switch {
		case text == "bootctl status --no-pager":
			state := "disabled"
			if f.secure {
				state = "enabled"
			}
			return ok("System:\n Secure Boot: " + state + "\n\nCurrent Boot Loader:\n Product: systemd-boot 259\n\nAvailable Boot Loaders:\n")
		case text == "pacman -Q":
			rows := []string{}
			for p, v := range f.packages {
				rows = append(rows, p+" "+v)
			}
			return ok(strings.Join(rows, "\n"))
		case text == "bootctl list --json=short --no-pager":
			entries := f.entries
			if !f.upgraded && f.packages["linux-cachyos-rc"] == "" {
				entries = entries[:1]
			}
			data, _ := json.Marshal(entries)
			return ok(string(data))
		case strings.HasPrefix(text, "findmnt --noheadings"):
			return ok("rw,relatime")
		case strings.HasPrefix(text, "findmnt --mountpoint"):
			return ok("/boot")
		case strings.HasPrefix(text, "df "):
			return ok("Avail\n" + f.space)
		case strings.HasPrefix(text, "pacman -Si "):
			p := cmd.Args[len(cmd.Args)-1]
			v := f.versions[p]
			if v == "" {
				return CommandResult{Code: 1}, nil
			}
			return ok("Repository : cachyos-v3\nName : " + p + "\nVersion : " + v + "\n")
		case strings.HasPrefix(text, "vercmp "):
			if f.downgrade {
				return ok("-1")
			}
			return ok("0")
		case strings.HasPrefix(text, "pacman -Ql "):
			p := cmd.Args[len(cmd.Args)-1]
			return ok(p + " /usr/lib/modules/" + f.releases[p] + "/vmlinuz\n")
		case strings.HasPrefix(text, "lsinitcpio "):
			for pkg, release := range f.releases {
				if strings.Contains(text, "initramfs-"+pkg+".img") {
					if f.stale && f.upgraded && pkg == "linux-cachyos-rc" {
						return ok("usr/lib/modules/old/kernel/")
					}
					return ok("usr/lib/modules/" + release + "/kernel/")
				}
			}
		case strings.HasPrefix(text, "modinfo "):
			return ok(cmd.Args[2] + " SMP preempt")
		case text == "sudo pacman -Syu --downloadonly":
			return ok("")
		case strings.HasPrefix(text, "sudo pacman -Su --downloadonly"):
			return ok("")
		case strings.HasPrefix(text, "sudo pacman -Su --needed"):
			f.upgraded = true
			for p, v := range f.versions {
				f.packages[p] = v
			}
			return ok("")
		case strings.HasPrefix(text, "sudo bootctl "):
			return ok("")
		case text == "uname -r":
			for _, entry := range f.entries {
				if entry.Selected && strings.Contains(entry.ID, "rc") {
					return ok(f.releases["linux-cachyos-rc"])
				}
			}
			return ok(f.releases["linux-cachyos-lts"])
		}
		return CommandResult{Code: 1, Stderr: "unexpected command: " + text}, nil
	}
	return f
}

func TestKernelSettingsRemainOptIn(t *testing.T) {
	for _, channel := range []string{"", "unchanged", "stable", "rc", "invalid"} {
		data := `{"schema_version":2}`
		if channel != "" {
			data = fmt.Sprintf(`{"schema_version":2,"kernel_channel":%q}`, channel)
		}
		path := filepath.Join(t.TempDir(), "settings.json")
		storagePut(t, path, data)
		settings, err := LoadSettings(path)
		if channel == "invalid" {
			if err == nil {
				t.Fatal("invalid channel accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if channel == "" && settings.KernelChannel != "unchanged" {
			t.Fatal(settings)
		}
	}
}

func TestKernelUpgradePreservesFallbackAndDoesNotActivate(t *testing.T) {
	f := newKernelFixture(t)
	if err := f.manager.upgrade(); err != nil {
		t.Fatal(err)
	}
	if !f.upgraded || f.packages["linux-cachyos-lts"] == "" {
		t.Fatal("missing target/fallback")
	}
	calls := []string{}
	for _, cmd := range f.runner.calls {
		calls = append(calls, packageCmd(cmd))
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, "set-default linux-cachyos-rc") || strings.Contains(joined, "set-oneshot linux-cachyos-rc") || strings.Contains(joined, "reboot") {
		t.Fatal(joined)
	}
	download := strings.Index(joined, "sudo pacman -Su --downloadonly")
	install := strings.Index(joined, "sudo pacman -Su --needed")
	pin := strings.Index(joined, "sudo bootctl set-default linux-cachyos-lts.conf")
	if download < 0 || pin < download || install < pin {
		t.Fatal(joined)
	}
	// A subsequent run updates the same explicitly chosen channel, retaining fallback.
	if err := f.manager.upgrade(); err != nil {
		t.Fatal(err)
	}
	if f.manager.c.Report["kernel"].(map[string]any)["status"] != "installed_not_activated" {
		t.Fatal(f.manager.c.Report)
	}
}

func TestKernelPreflightFailuresDoNotInstall(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*kernelFixture)
	}{
		{"secure boot", func(f *kernelFixture) { f.secure = true }},
		{"disk space", func(f *kernelFixture) { f.space = "1" }},
		{"unmounted boot", func(f *kernelFixture) { f.fail = "findmnt --mountpoint" }},
		{"missing fallback", func(f *kernelFixture) { delete(f.packages, "linux-cachyos-lts") }},
		{"dkms", func(f *kernelFixture) { f.packages["nvidia-open-dkms"] = "1" }},
		{"missing candidate", func(f *kernelFixture) { delete(f.versions, "linux-cachyos-rc") }},
		{"headers mismatch", func(f *kernelFixture) { f.versions["linux-cachyos-rc-headers"] = "7.2-1" }},
		{"dependency solve", func(f *kernelFixture) { f.fail = "sudo pacman -Su --downloadonly" }},
		{"downgrade", func(f *kernelFixture) { f.packages["linux-cachyos-rc"] = "99-1"; f.downgrade = true }},
		{"unavailable driver", func(f *kernelFixture) { f.packages["linux-cachyos-lts-nvidia-open"] = "1" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newKernelFixture(t)
			test.change(f)
			if err := f.manager.upgrade(); err == nil {
				t.Fatal("unsafe selection accepted")
			}
			if f.upgraded {
				t.Fatal("installed after preflight failure")
			}
		})
	}
}

func TestKernelHooksMustProduceMatchingImages(t *testing.T) {
	for _, kind := range []string{"transaction", "stale initramfs", "wrong image", "missing driver"} {
		t.Run(kind, func(t *testing.T) {
			f := newKernelFixture(t)
			switch kind {
			case "transaction":
				f.fail = "sudo pacman -Su --needed"
			case "stale initramfs":
				f.stale = true
			case "wrong image":
				storagePut(t, f.manager.path("/boot/vmlinuz-linux-cachyos-rc"), "old kernel")
			case "missing driver":
				f.packages["linux-cachyos-lts-zfs"] = "1"
				f.versions["linux-cachyos-rc-zfs"] = "1"
				f.fail = "modinfo -k " + f.releases["linux-cachyos-rc"]
			}
			if err := f.manager.upgrade(); err == nil {
				t.Fatal("broken kernel accepted")
			}
			for _, cmd := range f.runner.calls {
				if strings.Contains(packageCmd(cmd), "set-oneshot linux-cachyos-rc") {
					t.Fatal("activated failed kernel")
				}
			}
		})
	}
}

func TestKernelBootTrialAndPromotion(t *testing.T) {
	f := newKernelFixture(t)
	if err := f.manager.upgrade(); err != nil {
		t.Fatal(err)
	}
	if err := f.manager.boot("promote"); err == nil {
		t.Fatal("promoted before trial boot")
	}
	if err := f.manager.boot("once"); err != nil {
		t.Fatal(err)
	}
	last := packageCmd(f.runner.calls[len(f.runner.calls)-1])
	if last != "sudo bootctl set-oneshot linux-cachyos-rc.conf" {
		t.Fatal(last)
	}
	f.entries[1].Selected = true
	if err := f.manager.boot("promote"); err != nil {
		t.Fatal(err)
	}
	last = packageCmd(f.runner.calls[len(f.runner.calls)-1])
	if last != "sudo bootctl set-default linux-cachyos-rc.conf" {
		t.Fatal(last)
	}
}

func TestKernelReadOnlyAndCLIIsolation(t *testing.T) {
	f := newKernelFixture(t)
	f.manager.c.CheckOnly = true
	if f.manager.upgrade() == nil || f.manager.boot("once") == nil {
		t.Fatal("read-only mutation accepted")
	}
	if len(f.runner.calls) != 0 {
		t.Fatal(f.runner.calls)
	}
	for _, args := range [][]string{{"--kernel-boot", "once", "--dry-run"}, {"check", "--kernel-boot", "once"}, {"--kernel-boot", "promote", "--stage", "packages"}, {"--kernel-boot", "bad"}} {
		if _, err := ParseOptions(args); err == nil {
			t.Fatal(args)
		}
	}
	if _, err := ParseOptions([]string{"--kernel-boot", "once", "--settings", "settings.json"}); err != nil {
		t.Fatal(err)
	}
}

func TestKernelBootRejectsMissingOrAmbiguousImages(t *testing.T) {
	f := newKernelFixture(t)
	entries := append(f.entries, f.entries[0])
	if _, err := entryFor(entries, "linux-cachyos-lts"); err == nil {
		t.Fatal("ambiguous boot entry accepted")
	}
	if err := os.Remove(f.manager.path("/boot/initramfs-linux-cachyos-lts.img")); err != nil {
		t.Fatal(err)
	}
	if f.manager.upgrade() == nil {
		t.Fatal("missing fallback initramfs accepted")
	}
}

func TestKernelCheckReportsRebootWithoutChangingBootSelection(t *testing.T) {
	f := newKernelFixture(t)
	if err := f.manager.upgrade(); err != nil {
		t.Fatal(err)
	}
	f.runner.calls = nil
	if err := f.manager.check(); err != nil {
		t.Fatal(err)
	}
	record := object(f.manager.c.Report["kernel"])
	if record["reboot_required"] != true || record["fallback"] != "linux-cachyos-lts" || record["installed_version"] != "7.3.rc6-2" {
		t.Fatal(record)
	}
	f.entries[1].Selected = true
	if err := f.manager.check(); err != nil {
		t.Fatal(err)
	}
	if object(f.manager.c.Report["kernel"])["reboot_required"] != false {
		t.Fatal(f.manager.c.Report)
	}
	for _, cmd := range f.runner.calls {
		if cmd.Args[0] == "sudo" {
			t.Fatal("check requested privileges", cmd)
		}
	}
}

func TestKernelPreviewUsesOnlyLocalInventory(t *testing.T) {
	f := newKernelFixture(t)
	text := kernelPlanObservation(f.manager.c)
	for _, want := range []string{"6.12.1-cachyos-lts", "7.3.rc6-2", "provisional"} {
		if !strings.Contains(text, want) {
			t.Fatal(text)
		}
	}
	for _, cmd := range f.runner.calls {
		if !contains([]string{"uname", "pacman", "vercmp"}, cmd.Args[0]) || contains(cmd.Args, "-Sy") || contains(cmd.Args, "-Syu") {
			t.Fatal(cmd)
		}
	}
	if exists(f.manager.c.ReportPath) {
		t.Fatal("preview created a report")
	}
}

func TestKernelUnverifiedRepositoryAndBootloaderAreRejected(t *testing.T) {
	for _, kind := range []string{"repository", "bootloader", "missing fallback default", "fallback removed"} {
		t.Run(kind, func(t *testing.T) {
			f := newKernelFixture(t)
			reply := f.runner.reply
			f.runner.reply = func(cmd Command) (CommandResult, error) {
				result, err := reply(cmd)
				if kind == "repository" {
					result.Stdout = strings.ReplaceAll(result.Stdout, "Repository : cachyos-v3", "Repository : third-party")
				}
				if kind == "bootloader" {
					result.Stdout = strings.ReplaceAll(result.Stdout, "Product: systemd-boot", "Product: Limine")
				}
				if f.upgraded && kind == "missing fallback default" && cmd.Args[0] == "bootctl" && cmd.Args[1] == "list" {
					result.Stdout = strings.ReplaceAll(result.Stdout, `"isDefault":true`, `"isDefault":false`)
				}
				if f.upgraded && kind == "fallback removed" && packageCmd(cmd) == "pacman -Q" {
					result.Stdout = "linux-cachyos-rc 7.3.rc6-2\nlinux-cachyos-rc-headers 7.3.rc6-2\n"
				}
				return result, err
			}
			if err := f.manager.upgrade(); err == nil {
				t.Fatal("unsafe transition accepted")
			}
		})
	}
}

func TestKernelStableChannelAndLegacySnapshot(t *testing.T) {
	settings := DefaultSettings()
	settings.KernelChannel = "stable"
	if kernelPackage(settings) != "linux-cachyos" {
		t.Fatal(kernelPackage(settings))
	}
	// Existing reports predate kernel_channel and must retain unchanged behavior.
	path := filepath.Join(t.TempDir(), "report.json")
	data := map[string]any{"format": RunReportFormat, "schema_version": RunReportSchemaVersion, "settings": map[string]any{"schema_version": 2, "package_groups": map[string]bool{"core": true, "utilities": true, "development": true, "containers": true}, "laptop": map[string]any{"internal_scale": "4/3", "internal_keyboard": "hu", "natural_scroll": true}}}
	encoded, _ := json.Marshal(data)
	storagePut(t, path, string(encoded))
	loaded, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if kernelChannel(loaded) != "unchanged" {
		t.Fatal(loaded)
	}
}

func TestKernelDriverFamilyIgnoresUserspaceLibraries(t *testing.T) {
	modules, err := kernelModuleSuffixes(map[string]string{"linux-cachyos-nvidia-open": "1", "nvidia-utils": "1", "opencl-nvidia": "1", "lib32-opencl-nvidia": "1"})
	if err != nil || len(modules) != 1 || modules[0] != "nvidia-open" {
		t.Fatalf("modules=%v error=%v", modules, err)
	}
}

func TestKernelRegisteredDKMSBlocksBootSelection(t *testing.T) {
	f := newKernelFixture(t)
	if err := f.manager.upgrade(); err != nil {
		t.Fatal(err)
	}
	f.packages["dkms"] = "1"
	original := f.runner.reply
	f.runner.reply = func(cmd Command) (CommandResult, error) {
		if packageCmd(cmd) == "dkms status" {
			return CommandResult{Stdout: "custom/1, kernel, x86_64: installed"}, nil
		}
		return original(cmd)
	}
	f.runner.calls = nil
	if f.manager.boot("once") == nil {
		t.Fatal("custom DKMS accepted")
	}
	for _, cmd := range f.runner.calls {
		if cmd.Args[0] == "sudo" {
			t.Fatal("boot selection changed", cmd)
		}
	}
}
