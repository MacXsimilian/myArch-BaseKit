package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type integrationRunner struct {
	commands      []Command
	rejectSession bool
}

func (r *integrationRunner) Execute(command Command) (CommandResult, error) {
	r.commands = append(r.commands, command)
	if command.Args[0] == "getsubids" {
		return CommandResult{Stdout: "0: user 100000 65536\n"}, nil
	}
	if r.rejectSession && command.Args[0] == "sh" {
		return CommandResult{Code: 2, Stderr: "syntax error"}, nil
	}
	return CommandResult{}, nil
}
func integrationContext(t *testing.T, runner Runner) *Context {
	t.Helper()
	home := t.TempDir()
	c := &Context{Home: home, ConfigHome: filepath.Join(home, ".config"), StateHome: filepath.Join(home, ".local/state"), DataHome: filepath.Join(home, ".local/share"), CacheHome: filepath.Join(home, ".cache"), RunDir: filepath.Join(home, "run"), Report: map[string]any{}, Runner: runner, Inventory: map[string]any{}}
	c.BinDir = filepath.Join(home, ".local/bin")
	c.ReportPath = filepath.Join(c.RunDir, "profile-report.json")
	t.Setenv("ZDOTDIR", home)
	return c
}
func integrationParserFiles(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"sh", "bash"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("parser placeholder\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
}

func TestManagedShellMarkersPreserveBytes(t *testing.T) {
	user := "# user\r\nexport SOMETHING='a b'\r\n"
	input := user + shellBlockBegin + "\r\nold content\r\n" + shellBlockEnd + "\r\n" + "# tail\n"
	result, err := withoutShellBlock(input, "rc")
	if err != nil {
		t.Fatal(err)
	}
	if result != user+"# tail\n" {
		t.Fatalf("lost user bytes: %q", result)
	}
	for _, bad := range []string{shellBlockEnd + "\n", shellBlockBegin + "\n", shellBlockBegin + "\n" + shellBlockBegin + "\n" + shellBlockEnd + "\n"} {
		if _, err := withoutShellBlock(bad, "rc"); err == nil {
			t.Fatalf("accepted broken marker: %q", bad)
		}
	}
	result = withShellBlock(result, "new config")
	again, err := withoutShellBlock(result, "rc")
	if err != nil {
		t.Fatal(err)
	}
	if withShellBlock(again, "new config") != result {
		t.Fatal("managed block not idempotent")
	}
}
func TestShellInspectionNeverExpandsExecutableSources(t *testing.T) {
	input := `source "$HOME/.tool config"
. ${XDG_CONFIG_HOME}/extra
source ~/literal
source "$(touch /tmp/nope)"
source "$OTHER_HOME/file"
source relative
source /tmp/*.sh
# source /ignored
`
	got := literalShellSources(input, "/home/user", "/home/user/.config", "/home/user")
	want := []string{"/home/user/.tool config", "/home/user/.config/extra", "/home/user/literal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
func TestShellSourceIntrospectionPreservesToolAndPrompt(t *testing.T) {
	home := t.TempDir()
	child := filepath.Join(home, "child.zsh")
	if err := os.WriteFile(child, []byte("eval \"$(mise activate zsh)\"\nsource ~/.p10k.zsh\nplugins=(\n zoxide\n direnv\n)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("# starship init zsh\nprintf 'starship init zsh'\nsource %s\n", shellQuote(child))
	recognized, prompts := inspectShellSources("zsh", filepath.Join(home, ".zshrc"), base, home, filepath.Join(home, ".config"), home)
	if len(recognized["mise"]) == 0 || len(recognized["zoxide"]) == 0 || len(recognized["direnv"]) == 0 {
		t.Fatalf("missed sourced integration: %v", recognized)
	}
	if len(recognized["starship"]) > 0 {
		t.Fatalf("recognized printed/commented setup as active: %v", recognized)
	}
	if len(prompts) == 0 {
		t.Fatal("missed sourced prompt framework")
	}
	recognized["starship"] = []string{"preserved prompt"}
	body := posixShellBody("zsh", recognized)
	for _, absent := range []string{"mise activate zsh", "zoxide init zsh", "direnv hook zsh", "starship init zsh"} {
		if strings.Contains(body, absent) {
			t.Fatalf("duplicated %s", absent)
		}
	}
}
func TestGeneratedShellIntegrationOrderAndCollisionRules(t *testing.T) {
	recognized := map[string][]string{}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		body := posixShellBody(shell, recognized)
		if shell == "fish" {
			body = fishShellBody(recognized)
		}
		for _, required := range []string{"code --wait", "KIND_EXPERIMENTAL_PROVIDER", "GOTOOLCHAIN=local", "--no-cmd", "__cachyos_tooling_mise_initialized"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s missing %s", shell, required)
			}
		}
		if strings.Index(body, "mise activate") > strings.Index(body, "zoxide init") || strings.Index(body, "starship init") > strings.Index(body, "direnv hook") {
			t.Fatalf("bad integration order in %s", shell)
		}
		if strings.Contains(body, "export GOPATH=") || strings.Contains(body, "set -gx GOPATH") {
			t.Fatal("overwrote Go environment")
		}
	}
}
func TestShellValidationFailureWritesNothing(t *testing.T) {
	integrationParserFiles(t)
	runner := &integrationRunner{rejectSession: true}
	c := integrationContext(t, runner)
	rc := filepath.Join(c.Home, ".bashrc")
	if err := os.WriteFile(rc, []byte("# personal configuration\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureShell(c); err == nil {
		t.Fatal("accepted rejected session environment")
	}
	data, _ := os.ReadFile(rc)
	if string(data) != "# personal configuration\n" {
		t.Fatal("wrote rc before all syntax checks")
	}
	if exists(filepath.Join(c.ConfigHome, "environment.d/90-cachyos-tooling.conf")) {
		t.Fatal("wrote session defaults before parser checks")
	}
}
func TestShellConfigurationKeepsExistingDefaultsAndPrompt(t *testing.T) {
	integrationParserFiles(t)
	runner := &integrationRunner{}
	c := integrationContext(t, runner)
	rc := filepath.Join(c.Home, ".bashrc")
	initial := "export PERSONAL=value\neval \"$(oh-my-posh init bash)\"\n"
	if err := os.WriteFile(rc, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(c.ConfigHome, "uwsm/env")
	if err := os.MkdirAll(filepath.Dir(env), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env, []byte("export PERSONAL_GUI=present\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureShell(c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(rc)
	if !strings.HasPrefix(string(data), initial) {
		t.Fatal("changed user rc")
	}
	if strings.Contains(string(data), "starship init bash") {
		t.Fatal("overrode existing prompt")
	}
	session, _ := os.ReadFile(env)
	if !strings.Contains(string(session), "export PERSONAL_GUI=present") {
		t.Fatal("lost unrelated UWSM configuration")
	}
	for _, command := range runner.commands {
		if command.Input != nil && (command.Env["BASH_ENV"] != "" || command.Env["ENV"] != "") {
			t.Fatal("validation could execute injected environment script")
		}
	}
	before := string(data)
	if err := ConfigureShell(c); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(rc)
	if string(data) != before {
		t.Fatal("shell config changed on repeated invocation")
	}
}
func TestComposeProviderEditOnlyChangesProvider(t *testing.T) {
	cases := []struct{ input, unchanged string }{
		{"# personal\n[engine]\nevents_logger = \"file\"\ncompose_providers = [\n \"/custom/compose\", # old\n] # selection\n[network]\ndefault_network=\"custom\"\n", "[network]\ndefault_network=\"custom\"\n"},
		{"[engine]\nevents_logger=\"file\"\n", "events_logger=\"file\"\n"},
		{"[network]\nnetwork_backend=\"netavark\"\n", "[network]\nnetwork_backend=\"netavark\"\n"},
		{"[engine]\r\ncompose_providers=['docker-compose']\r\nfoo=true\r\n", "foo=true\r\n"},
		{"[[additional]]\nname='first'\n[[additional]]\nname='second'\n", "[[additional]]\nname='first'\n[[additional]]\nname='second'\n"},
	}
	for _, tc := range cases {
		result, err := selectComposeProvider(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result, tc.unchanged) {
			t.Fatalf("lost user setting: %q", result)
		}
		selected, err := scanComposeConfig(result)
		if err != nil || !composeConfigured(selected, false) {
			t.Fatalf("provider not selected: %s: %v", result, err)
		}
		again, err := selectComposeProvider(result)
		if err != nil || again != result {
			t.Fatal("provider edit not idempotent")
		}
	}
}
func TestComposeRejectsUnsafeOrAmbiguousTOML(t *testing.T) {
	for _, input := range []string{
		"[engine]\ncompose_providers=[\"one\"]\ncompose_providers=[\"two\"]\n",
		"engine.compose_providers=[\"one\"]\n",
		"engine={compose_providers=[\"one\"]}\n",
		"[[engine]]\ncompose_providers=[\"one\"]\n",
		"[engine]\ncompose_providers=[\"one\"\n",
		"[engine]\ncompose_providers=[\"one\",{append=broken}]\n",
		"[engine]\ncompose_providers=[\"one\"]\nfoo=\"\"\"multiline\"\"\"\n",
		"[engine]\nfoo=true\n[engine]\nbar=false\n",
		"[engine]\nfoo=2026-99-99\n",
		"[engine]\nfoo=\"literal\x00byte\"\n",
		"[engine]\nfoo=\"invalid\xffbyte\"\n",
	} {
		if _, err := selectComposeProvider(input); err == nil {
			t.Fatalf("accepted unsafe TOML: %s", input)
		}
	}
}
func TestComposeKeepsArrayAppendModeExplicitlyDisabled(t *testing.T) {
	single, err := scanComposeConfig("[engine]\ncompose_providers=[\"" + composeProvider + "\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if composeConfigured(single, false) {
		t.Fatal("single provider did not reset prior append mode")
	}
	if !composeConfigured(single, true) {
		t.Fatal("later sole-provider dropin treated as conflict")
	}
	configured, err := selectComposeProvider("[engine]\ncompose_providers=[\"" + composeProvider + "\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(configured, "{append=false}") {
		t.Fatal("persistent append mode not reset")
	}
}
func TestSubordinateMappingsRequireContiguousNonoverlappingRange(t *testing.T) {
	for _, tc := range []struct {
		text           string
		ready, invalid bool
	}{
		{"0: user 100000 65536\n", true, false},
		{"0: user 100000 32768\n1: user 132768 32768\n", false, false},
		{"0: user 100000 65536\n1: user 110000 65536\n", false, true},
		{"0: user 0 65536\n", false, true},
		{"0: user 4294967290 65536\n", false, true},
		{"wrong output\n", false, true},
	} {
		_, ready, err := parseSubordinateRanges(tc.text)
		if ready != tc.ready || (err != nil) != tc.invalid {
			t.Fatalf("mapping %q: ready=%v error=%v", tc.text, ready, err)
		}
	}
}
func TestContainerSetupNeverStartsWorkloadsAndSocketIsOptIn(t *testing.T) {
	runner := &integrationRunner{}
	c := integrationContext(t, runner)
	if err := ConfigureContainers(c); err != nil {
		t.Fatal(err)
	}
	for _, command := range runner.commands {
		if command.Args[0] != "getsubids" {
			t.Fatalf("unexpected runtime command %v", command.Args)
		}
	}
	report := object(c.Report["containers"])
	if report["compose_provider_configured"] != true {
		t.Fatal("provider not reported configured")
	}
	c.Options.EnablePodmanSocket = true
	if err := ConfigureContainers(c); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, command := range runner.commands {
		if reflect.DeepEqual(command.Args, []string{"systemctl", "--user", "enable", "--now", "podman.socket"}) {
			found = true
		}
	}
	if !found {
		t.Fatal("explicit socket opt-in not applied")
	}
}
func TestContainerCheckDoesNotWriteConfigurationOrReport(t *testing.T) {
	runner := &integrationRunner{}
	c := integrationContext(t, runner)
	// CheckContainers is read-only even if the caller has not set CheckOnly.
	_ = CheckContainers(c)
	if exists(filepath.Join(c.ConfigHome, "containers/containers.conf.d/90-myarch-buildkit.conf")) || exists(c.ReportPath) {
		t.Fatal("read-only check wrote files")
	}
	for _, command := range runner.commands {
		if command.Args[0] != "getsubids" {
			t.Fatalf("check changed runtime: %v", command.Args)
		}
	}
}
func TestLaterComposeOverridesAndParseErrorsAreReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "90-myarch-buildkit.conf")
	files := map[string]string{"80-early.conf": "[engine]\ncompose_providers=['docker-compose']\n", "91-custom.conf": "[engine]\ncompose_providers=['custom']\n", "92-same.conf": "[engine]\ncompose_providers=['" + composeProvider + "']\n", "99-broken.conf": "[engine]\ncompose_providers=[\n"}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	conflicts, errors := laterComposeOverrides(path)
	if len(conflicts) != 1 || !strings.HasSuffix(conflicts[0], "91-custom.conf") || len(errors) != 1 || !strings.Contains(errors[0], "99-broken.conf") {
		t.Fatalf("conflicts=%v errors=%v", conflicts, errors)
	}
}
