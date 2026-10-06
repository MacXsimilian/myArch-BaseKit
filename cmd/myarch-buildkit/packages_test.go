package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type packageTestRunner struct {
	calls []Command
	reply func(Command) (CommandResult, error)
}

func (r *packageTestRunner) Execute(command Command) (CommandResult, error) {
	r.calls = append(r.calls, command)
	if r.reply != nil {
		return r.reply(command)
	}
	return CommandResult{Code: 1}, nil
}
func packageTestContext(t *testing.T, r Runner) *Context {
	t.Helper()
	root := t.TempDir()
	return &Context{Home: root, ConfigHome: filepath.Join(root, "config"), StateHome: filepath.Join(root, "state"), DataHome: filepath.Join(root, "data"), CacheHome: filepath.Join(root, "cache"), RunDir: filepath.Join(root, "run"), ReportPath: filepath.Join(root, "run", "report.json"), Report: map[string]any{}, Inventory: map[string]any{}, Runner: r, Settings: Settings{PackageGroups: map[string]bool{"core": true, "utilities": true, "development": true, "containers": true}}}
}
func packageCmd(command Command) string { return strings.Join(command.Args, " ") }
func packageReplyAllRepo(command Command) (CommandResult, error) {
	args := command.Args
	if len(args) > 1 && args[0] == "pacman" {
		if args[1] == "-Si" {
			return CommandResult{Stdout: "Name : " + args[len(args)-1] + "\n"}, nil
		}
		if args[1] == "-Q" {
			if len(args) > 4 {
				return CommandResult{Stdout: "installed versions\n"}, nil
			}
			return CommandResult{Code: 1}, nil
		}
	}
	return CommandResult{}, nil
}

func TestPackageManifestPreservesApplicationsWithoutInstallerRuntime(t *testing.T) {
	seen := map[string]Tool{}
	for _, tool := range PackageManifest() {
		if _, duplicate := seen[tool.Label]; duplicate {
			t.Fatalf("duplicate label: %s", tool.Label)
		}
		seen[tool.Label] = tool
		if tool.Label == "Configuration helper runtime" {
			t.Fatal("installer should not require Python")
		}
	}
	for _, label := range []string{"DMS Docker Manager backend", "DMS Kubernetes backend", "Bongo Cat keyboard events", "Bongo Cat input tools", "DankMaterialShell", "DankGreeter", "Microsoft VS Code", "Go (includes go install)", "Podman Compose", "Terraform", "Ansible", "Bitwarden CLI", "Obsidian", "PeaZip archive manager"} {
		if seen[label].Label == "" {
			t.Errorf("missing requested application %s", label)
		}
	}
	if seen["Go (includes go install)"].Group != "development" {
		t.Fatal("Go is a development app, not installer runtime")
	}
	if seen["DMS Docker Manager backend"].Group != "core" {
		t.Fatal("plugin backend must remain core")
	}
}
func TestExactPackageMetadataRejectsAliases(t *testing.T) {
	if exactPackageInfo("Name : different-package\n", "wanted") {
		t.Fatal("accepted a provider or alias as exact candidate")
	}
	if !exactPackageInfo("Repository : core\nName : wanted\n", "wanted") {
		t.Fatal("rejected exact Name field")
	}
}
func TestPackageResolutionPrefersRepository(t *testing.T) {
	runner := &packageTestRunner{reply: packageReplyAllRepo}
	c := packageTestContext(t, runner)
	tools := []Tool{{Label: "App", RepoCandidates: []string{"repo-app"}, AURCandidates: []string{"aur-app"}}}
	result, err := newPackageResolver(c).resolve(tools)
	if err != nil {
		t.Fatal(err)
	}
	if result[0].Package != "repo-app" || result[0].Source != "repo" {
		t.Fatalf("wrong resolution: %#v", result)
	}
	for _, call := range runner.calls {
		if call.Args[0] == "paru" || call.Args[0] == "yay" {
			t.Fatalf("unnecessary AUR query: %v", call.Args)
		}
	}
}
func TestPackageResolutionRetainsInstalledAlternativeWhenUpdateSourceMissing(t *testing.T) {
	runner := &packageTestRunner{reply: func(command Command) (CommandResult, error) {
		switch packageCmd(command) {
		case "pacman -Q -- alternative":
			return CommandResult{}, nil
		case "pacman -Si --color never -- preferred":
			return CommandResult{Stdout: "Name : preferred\n"}, nil
		}
		return CommandResult{Code: 1}, nil
	}}
	c := packageTestContext(t, runner)
	result, err := newPackageResolver(c).resolve([]Tool{{Label: "App", RepoCandidates: []string{"preferred", "alternative"}}})
	if err != nil {
		t.Fatal(err)
	}
	if result[0].Package != "alternative" || result[0].Source != "installed" {
		t.Fatalf("switched installed variant: %#v", result)
	}
}
func TestAURHelperRepairUsesRepositoryAndNormalPrompts(t *testing.T) {
	repaired := false
	runner := &packageTestRunner{reply: func(command Command) (CommandResult, error) {
		switch packageCmd(command) {
		case "paru --version":
			if repaired {
				return CommandResult{}, nil
			}
		case "pacman -Si --color never -- paru":
			return CommandResult{Stdout: "Name : paru\n"}, nil
		case "sudo pacman -S -- paru":
			if !command.Interactive {
				t.Fatal("helper repair did not preserve review prompts")
			}
			repaired = true
			return CommandResult{}, nil
		}
		return CommandResult{Code: 1}, nil
	}}
	resolver := newPackageResolver(packageTestContext(t, runner))
	if err := resolver.findHelper(); err != nil {
		t.Fatal(err)
	}
	if resolver.helper != "paru" {
		t.Fatal("repaired helper not selected")
	}
	for _, call := range runner.calls {
		if containsPackage(call.Args, "--needed") || containsPackage(call.Args, "--noconfirm") || containsPackage(call.Args, "--overwrite") {
			t.Fatal("helper repair has unsafe/unhelpful flag")
		}
	}
}
func TestPackageQueriesCachedWithinStage(t *testing.T) {
	runner := &packageTestRunner{reply: packageReplyAllRepo}
	resolver := newPackageResolver(packageTestContext(t, runner))
	resolver.repoHas("shared")
	resolver.repoHas("shared")
	resolver.isInstalled("shared")
	resolver.isInstalled("shared")
	if len(runner.calls) != 2 {
		t.Fatalf("metadata query not cached: %v", runner.calls)
	}
	for _, command := range runner.calls {
		if command.Env["LC_ALL"] != "C" {
			t.Fatal("metadata field parsing must use C locale")
		}
	}
}
func TestPackageInstallBootstrapOnceAndKeepReviewPrompts(t *testing.T) {
	for _, bootstrapped := range []bool{false, true} {
		runner := &packageTestRunner{reply: packageReplyAllRepo}
		c := packageTestContext(t, runner)
		c.RunDir = ""
		if err := InstallPackages(c, "core", bootstrapped); err != nil {
			t.Fatal(err)
		}
		upgrade := 0
		for _, command := range runner.calls {
			if strings.HasPrefix(packageCmd(command), "sudo pacman -Syu ") {
				upgrade++
				if !command.Interactive {
					t.Fatal("bootstrap prompts unavailable")
				}
				if containsPackage(command.Args, "python") {
					t.Fatal("Go installer should not install Python runtime")
				}
			}
			if command.Args[0] == "sudo" || command.Args[0] == "paru" || command.Args[0] == "yay" {
				if !command.Interactive {
					t.Fatal("transaction prompts unavailable")
				}
			}
			for _, flag := range []string{"--noconfirm", "--overwrite", "-Rdd", "--nodeps"} {
				if containsPackage(command.Args, flag) {
					t.Fatalf("unsafe package flag: %s", flag)
				}
			}
		}
		expected := 1
		if bootstrapped {
			expected = 0
		}
		if upgrade != expected {
			t.Fatalf("bootstrap count %d, want %d", upgrade, expected)
		}
	}
}
func TestPythonYQCollisionStopsBeforeToolTransaction(t *testing.T) {
	runner := &packageTestRunner{reply: func(command Command) (CommandResult, error) {
		if packageCmd(command) == "pacman -Q -- yq" {
			return CommandResult{}, nil
		}
		return packageReplyAllRepo(command)
	}}
	c := packageTestContext(t, runner)
	c.RunDir = ""
	err := InstallPackages(c, "development", true)
	if err == nil || !strings.Contains(err.Error(), "same path") {
		t.Fatalf("expected yq collision: %v", err)
	}
	for _, command := range runner.calls {
		if command.Interactive {
			t.Fatalf("transaction happened after collision: %v", command.Args)
		}
	}
}
func TestReadOnlyPackageInstallationRefusesTransactions(t *testing.T) {
	runner := &packageTestRunner{}
	c := packageTestContext(t, runner)
	c.CheckOnly = true
	if err := InstallPackages(c, "core", false); err == nil {
		t.Fatal("installation accepted in check mode")
	}
	if len(runner.calls) != 0 {
		t.Fatal("read-only mode issued package operation")
	}
}
func TestUnknownNotesPackageRejectedBeforeCleanup(t *testing.T) {
	runner := &packageTestRunner{}
	c := packageTestContext(t, runner)
	c.Options.RemoveNotes = []string{"neovim"}
	if err := ConfigureCleanup(c); err == nil {
		t.Fatal("arbitrary notes package was accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatal("cleanup performed operations before validation")
	}
}
func TestPendingDesktopBlocksAllCleanup(t *testing.T) {
	runner := &packageTestRunner{}
	c := packageTestContext(t, runner)
	if err := writeQueue(c, []QueueItem{{Manifest: "pending.json", Phase: "desktop"}}); err != nil {
		t.Fatal(err)
	}
	err := ConfigureCleanup(c)
	if err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("expected cleanup block: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("pending configuration triggered cleanup operation")
	}
}
func TestConfigureOnlySkipsCleanup(t *testing.T) {
	runner := &packageTestRunner{}
	c := packageTestContext(t, runner)
	c.Options.ConfigureOnly = true
	if err := ConfigureCleanup(c); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("configure-only cleanup called package manager")
	}
}
func TestLauncherHidingPreservesCommandsAssociationsActions(t *testing.T) {
	text := "# comment\n[Desktop Entry]\nName=Utility\nExec=utility %U\nMimeType=text/plain;\nNoDisplay=false\nActions=Open;\n\n[Desktop Action Open]\nExec=utility --open\n"
	hidden, err := hiddenEntry(text)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"# comment\n", "Exec=utility %U\n", "MimeType=text/plain;\n", "Actions=Open;\n", "[Desktop Action Open]\nExec=utility --open\n"} {
		if !strings.Contains(hidden, line) {
			t.Fatalf("lost launcher data %q", line)
		}
	}
	if strings.Count(hidden, "NoDisplay=") != 1 || !strings.Contains(hidden, "NoDisplay=true\n") {
		t.Fatal("launcher hidden field not uniquely set")
	}
	again, err := hiddenEntry(hidden)
	if err != nil || again != hidden {
		t.Fatal("launcher hiding not idempotent")
	}
}
func TestLauncherHidingPreservesExistingUserOverrideAndBacksUp(t *testing.T) {
	c := packageTestContext(t, &packageTestRunner{})
	source := filepath.Join(c.Home, "approved.desktop")
	target := filepath.Join(c.DataHome, "applications", "approved.desktop")
	if err := os.WriteFile(source, []byte("[Desktop Entry]\nExec=system-command\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	original := "[Desktop Entry]\nExec=personal-command\n"
	if err := os.WriteFile(target, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := hideLauncherSources(c, []string{source}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Exec=personal-command") || strings.Contains(string(data), "system-command") {
		t.Fatal("existing personal override lost")
	}
	records := reportRecords(c.Report, "file_changes")
	if len(records) < 1 || records[0]["backup"] == nil {
		t.Fatal("launcher override was not backed up")
	}
	backup, err := os.ReadFile(records[0]["backup"].(string))
	if err != nil || string(backup) != original {
		t.Fatal("backup does not preserve original override")
	}
}
func TestLauncherSymlinkPreserved(t *testing.T) {
	c := packageTestContext(t, &packageTestRunner{})
	source := filepath.Join(c.Home, "approved.desktop")
	if err := os.WriteFile(source, []byte("[Desktop Entry]\nExec=utility\n"), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(c.DataHome, "applications", "approved.desktop")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}
	if err := hideLauncherSources(c, []string{source}); err == nil {
		t.Fatal("symlink launcher override was replaced")
	}
	if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was not preserved")
	}
}
func TestOSReleaseIDParsing(t *testing.T) {
	for _, text := range []string{"NAME=CachyOS\nID=cachyos\n", "ID=\"cachyos\"\n", "ID='cachyos'\n"} {
		if osReleaseID([]byte(text)) != "cachyos" {
			t.Fatalf("incorrect OS identity: %q", text)
		}
	}
	if osReleaseID([]byte("ID_LIKE=cachyos\nID=arch\n")) != "arch" {
		t.Fatal("treated ID_LIKE as native CachyOS")
	}
}

func TestHyprlandParseErrorsPreventCleanup(t *testing.T) {
	for _, text := range []string{`["broken bind"]`, `{"error":"unavailable"}`, `null`, `[17]`} {
		c := packageTestContext(t, &packageTestRunner{reply: func(Command) (CommandResult, error) { return CommandResult{Stdout: text}, nil }})
		if err := checkCleanupHyprland(c); err == nil {
			t.Fatalf("accepted invalid running configuration response: %s", text)
		}
	}
	for _, text := range []string{`[]`, `["","   "]`} {
		c := packageTestContext(t, &packageTestRunner{reply: func(Command) (CommandResult, error) { return CommandResult{Stdout: text}, nil }})
		if err := checkCleanupHyprland(c); err != nil {
			t.Fatal(err)
		}
	}
}
func TestDependencyRefusalRetainsDuplicateWithoutCascade(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "test-session")
	c := packageTestContext(t, nil)
	commands := filepath.Join(c.Home, "commands")
	if err := os.Mkdir(commands, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nautilus", "code", "ghostty"} {
		if err := os.WriteFile(filepath.Join(commands, name), []byte("placeholder\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", commands)
	policy := filepath.Join(c.ConfigHome, "myarch-buildkit", "laptop.json")
	if err := os.MkdirAll(filepath.Dir(policy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte(`{"internal_outputs":[],"internal_keyboards":[],"internal_scale":1.3333333333333333,"natural_scroll":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	installed := map[string]bool{"nautilus": true, "dolphin": true, "visual-studio-code-bin": true, "kate": true, "ghostty": true, "alacritty": true, "gnote": true}
	c.Options.KeepTerminal = true
	runner := &packageTestRunner{reply: func(command Command) (CommandResult, error) {
		args := command.Args
		line := packageCmd(command)
		switch line {
		case "xdg-terminal-exec --print-id":
			return CommandResult{Stdout: "com.mitchellh.ghostty.desktop\n"}, nil
		case "xdg-terminal-exec --print-cmd":
			return CommandResult{Stdout: "ghostty\n"}, nil
		case "dms ipc call theme getMode":
			return CommandResult{Stdout: "dark\n"}, nil
		case "dms ipc call plugin-scan list":
			lines := []string{}
			for _, plugin := range RequestedPlugins {
				lines = append(lines, plugin.ID+"\tloaded")
			}
			return CommandResult{Stdout: strings.Join(lines, "\n")}, nil
		case "id -nG":
			return CommandResult{Stdout: "users input\n"}, nil
		case "hyprctl -j monitors all":
			return CommandResult{Stdout: "[]"}, nil
		case "hyprctl -j devices":
			return CommandResult{Stdout: "{\"keyboards\":[]}"}, nil
		case "hyprctl -j configerrors":
			return CommandResult{Stdout: "[]"}, nil
		case "sudo pacman -R -- kate":
			return CommandResult{Code: 1, Stderr: "required by another package"}, nil
		}
		if len(args) > 1 && args[0] == "xdg-mime" {
			values := map[string]string{"text/plain": "code.desktop", "x-scheme-handler/obsidian": "obsidian.desktop", "text/markdown": MarkdownDesktop, "inode/directory": "org.gnome.Nautilus.desktop", "application/pdf": "firefox.desktop", "audio/mpeg": "vlc.desktop", "video/mp4": "vlc.desktop"}
			return CommandResult{Stdout: values[args[len(args)-1]]}, nil
		}
		if args[0] == "fc-match" {
			return CommandResult{Stdout: BuildkitFont}, nil
		}
		if args[0] == "systemctl" {
			return CommandResult{Code: 3}, nil
		}
		if strings.HasPrefix(line, "hyprctl -j getoption ") {
			return CommandResult{Stdout: `{"int":1}`}, nil
		}
		if len(args) > 1 && args[0] == "pacman" && args[1] == "-Q" {
			if installed[args[len(args)-1]] {
				return CommandResult{}, nil
			}
			return CommandResult{Code: 1}, nil
		}
		return CommandResult{}, nil
	}}
	c.Runner = runner
	if err := ConfigureCleanup(c); err == nil {
		t.Fatal("dependency refusal should be reported")
	}
	sawDolphin, sawKate := false, false
	for _, command := range runner.calls {
		if strings.HasPrefix(packageCmd(command), "sudo pacman -R ") {
			if !command.Interactive {
				t.Fatal("removal prompts missing")
			}
			if containsPackage(command.Args, "alacritty") {
				t.Fatal("--keep-terminal ignored")
			}
			if containsPackage(command.Args, "gnote") {
				t.Fatal("notes removed without explicit selection")
			}
			if packageCmd(command) == "sudo pacman -R -- dolphin" {
				sawDolphin = true
			}
			if packageCmd(command) == "sudo pacman -R -- kate" {
				sawKate = true
			}
			if containsPackage(command.Args, "--nodeps") || containsPackage(command.Args, "-Rdd") || containsPackage(command.Args, "-Rns") {
				t.Fatal("bypassed package dependency gate")
			}
		}
	}
	if !sawDolphin || !sawKate {
		t.Fatal("approved duplicates not selected")
	}
	if !strings.Contains(str(c.Report["package_cleanup"]), "kate\tretained") {
		t.Fatal("dependency refusal not retained in cleanup report")
	}
	report := map[string]any{}
	if err := ReadJSON(c.ReportPath, &report); err != nil {
		t.Fatal(err)
	}
	if report["package_cleanup"] != c.Report["package_cleanup"] {
		t.Fatal("cleanup results were not saved in the report")
	}
	if _, err := os.Stat(filepath.Join(c.RunDir, "package-cleanup.tsv")); !os.IsNotExist(err) {
		t.Fatalf("unexpected separate cleanup report: %v", err)
	}
	if len(reportRecords(report, "file_changes")) != 0 {
		t.Fatal("cleanup report artifacts were recorded as configuration changes")
	}
}
