package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type Settings struct {
	SetupGreeter bool     `json:"setup_greeter"`
	DMSConfigDir string   `json:"dms_config_dir"`
	Groups       []string `json:"groups"`
	Keyboard     string   `json:"keyboard"`
	Scale        string   `json:"scale"`
	Monitor      string   `json:"monitor"`
	ConfigPath   string   `json:"config_path,omitempty"`
}

func defaultSettings() Settings {
	return Settings{SetupGreeter: true, DMSConfigDir: "dms", Groups: []string{"core", "utilities", "development", "containers"}, Keyboard: "hu", Scale: "4/3", Monitor: "eDP-1"}
}
func readSettings(path string) (Settings, error) {
	s := defaultSettings()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return s, err
		}
		if strings.TrimSpace(string(data)) == "null" {
			return s, errors.New("settings must be an object")
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return s, err
		}
		for key, value := range raw {
			if string(value) == "null" {
				return s, fmt.Errorf("%s cannot be null", key)
			}
		}
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if err := d.Decode(&s); err != nil {
			return s, fmt.Errorf("settings: %w (old settings are not supported; use --settings-template)", err)
		}
		var extra any
		if err := d.Decode(&extra); err != io.EOF {
			return s, errors.New("settings must contain one JSON object")
		}
	}
	if s.DMSConfigDir != "dms" && s.DMSConfigDir != "DankMaterialShell" {
		return s, errors.New("dms_config_dir must be dms (Dux 2) or DankMaterialShell (DMS 1)")
	}
	if _, err := SelectedPackages(s.Groups); err != nil {
		return s, err
	}
	identifier := regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	if !identifier.MatchString(s.Keyboard) || !identifier.MatchString(s.Monitor) {
		return s, errors.New("keyboard and monitor must be simple device/layout identifiers")
	}
	parts := strings.Split(s.Scale, "/")
	if len(parts) > 2 {
		return s, errors.New("scale must be a number or ratio")
	}
	n, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return s, errors.New("invalid scale")
	}
	if len(parts) == 2 {
		d, e := strconv.ParseFloat(parts[1], 64)
		if e != nil || d <= 0 {
			return s, errors.New("invalid scale denominator")
		}
		n /= d
	}
	if !(n >= .25 && n <= 8) {
		return s, errors.New("scale must be between 0.25 and 8")
	}
	if s.ConfigPath != "" && (!filepath.IsAbs(s.ConfigPath) || filepath.Ext(s.ConfigPath) != ".kdl") {
		return s, errors.New("config_path must be an absolute Niri .kdl path")
	}
	return s, nil
}

const usage = `myarch-buildkit — personal CachyOS workstation setup

  myarch-buildkit plan [--settings FILE] [--cleanup-apps] [--setup-greeter=true|false]
  myarch-buildkit apply [--settings FILE] [--skip-packages] [--cleanup-apps] [--setup-greeter=true|false]
  myarch-buildkit check [--settings FILE]
  myarch-buildkit --settings-template
  myarch-buildkit --version

No command defaults to plan. Review settings before applying.
Apply from a text console after logging out of graphical sessions and DMS.
Greeter setup and plugin activation are enabled by default.
Existing files are backed up before configuration changes.
`

func runApp(args []string, home string, r Runner, out io.Writer) error {
	if len(args) == 1 {
		switch args[0] {
		case "--help", "-h":
			fmt.Fprint(out, usage)
			return nil
		case "--version":
			fmt.Fprintln(out, BuildID)
			return nil
		case "--settings-template":
			b, _ := json.MarshalIndent(defaultSettings(), "", "  ")
			fmt.Fprintln(out, string(b))
			return nil
		}
	}
	command := "plan"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	if command != "plan" && command != "apply" && command != "check" {
		return fmt.Errorf("unknown command %q; see --help", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(out)
	path := flags.String("settings", "", "settings JSON")
	cleanup := flags.Bool("cleanup-apps", false, "preview/remove known duplicate apps with installed replacements")
	greeter := flags.Bool("setup-greeter", false, "override setup_greeter setting (default true)")
	skip := flags.Bool("skip-packages", false, "configure without installing packages")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *skip && command != "apply" {
		return errors.New("--skip-packages is only for apply")
	}
	if command == "check" && (*cleanup || *greeter) {
		return errors.New("--cleanup-apps and --setup-greeter are only for plan/apply")
	}
	s, err := readSettings(*path)
	if err != nil {
		return err
	}
	greeterOverride := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "setup-greeter" {
			greeterOverride = true
		}
	})
	if greeterOverride {
		s.SetupGreeter = *greeter
	}
	if configured := os.Getenv("XDG_CONFIG_HOME"); configured != "" && filepath.Clean(configured) != filepath.Join(home, ".config") {
		return errors.New("this personal profile uses ~/.config; nondefault XDG_CONFIG_HOME is unsupported")
	}
	target := s.ConfigPath
	if target == "" {
		target = filepath.Join(home, ".config/niri/config.kdl")
	}
	if configured := os.Getenv("NIRI_CONFIG"); configured != "" && filepath.Clean(configured) != filepath.Clean(target) {
		return errors.New("NIRI_CONFIG differs from config_path; select the same Niri configuration in settings")
	}
	packages, err := SelectedProfilePackages(s)
	if err != nil {
		return err
	}
	if s.SetupGreeter {
		packages = append(packages, GreeterPackage())
	}
	changes, err := BuildProfile(home, s)
	if err != nil {
		return err
	}
	for _, c := range changes {
		if err := safeHomePath(home, c.Path); err != nil {
			return err
		}
	}
	if command == "plan" {
		fmt.Fprintln(out, "Packages (full system upgrade, then selected packages):")
		for _, p := range packages {
			fmt.Fprintf(out, "  %-12s %s\n", p.Group, p.Name)
		}
		fmt.Fprintln(out, "Dux plugins (install missing, activate all five, add widgets; keep existing versions):")
		for _, plugin := range RequestedPlugins {
			fmt.Fprintf(out, "  %s (%s)\n", plugin.Name, plugin.ID)
		}
		fmt.Fprintln(out, "Shell configuration:", DMSConfigHome(home, s))
		fmt.Fprintln(out, "Niri config.kdl is replaced with the managed profile; existing contents are backed up.")
		fmt.Fprintln(out, "Configuration files (backed up when changed):")
		for _, c := range changes {
			fmt.Fprintln(out, " ", c.Path)
		}
		if *cleanup {
			if _, err := previewCleanup(r, out); err != nil {
				return err
			}
		}
		if s.SetupGreeter {
			PreviewGreeter(out)
		}
		fmt.Fprintf(out, "Monitor: %s; scale: %s; keyboard: %s\n", s.Monitor, s.Scale, s.Keyboard)
		fmt.Fprintln(out, "Review settings and existing desktop startup entries. Log out, then run apply from a text console. No changes made.")
		return nil
	}
	if command == "check" {
		var problems []error
		if containerGroup(s, "core") {
			if err := CheckShellServices(r); err != nil {
				problems = append(problems, err)
			}
		}
		if s.SetupGreeter {
			if err := CheckGreeter(r); err != nil {
				problems = append(problems, err)
			}
		}
		for _, err := range []error{CheckTerminalPreferences(r), CheckContainers(s, r)} {
			if err != nil {
				problems = append(problems, err)
			}
		}
		findings, inspectionErr := reportCompetingDesktop(home, r, out)
		if inspectionErr != nil {
			problems = append(problems, inspectionErr)
		}
		if len(findings) > 0 {
			problems = append(problems, errors.New("review competing desktop startup entries listed above"))
		}
		if err := ValidateDMSVersion(s, r); err != nil {
			problems = append(problems, err)
		}
		if err := CheckPlugins(home, s); err != nil {
			problems = append(problems, err)
		}
		if err := CheckPackages(packages, r); err != nil {
			problems = append(problems, err)
		}
		for _, c := range changes {
			data, e := os.ReadFile(c.Path)
			if e != nil || !bytes.Equal(data, c.Data) {
				problems = append(problems, fmt.Errorf("configuration differs or missing: %s", c.Path))
			}
		}
		if len(problems) > 0 {
			return errors.Join(problems...)
		}
		fmt.Fprintln(out, "Packages and configuration match. Check display, keyboard, audio and suspend in your normal Niri session.")
		return nil
	}
	if err := checkApplyPlatform(); err != nil {
		return err
	}
	if err := legacyPending(home); err != nil {
		return err
	}
	if err := noDesktop("/proc", os.Getuid()); err != nil {
		return err
	}
	unlock, err := setupLock(home)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := reportCompetingDesktop(home, r, out); err != nil {
		fmt.Fprintln(out, "Warning: desktop startup inspection incomplete:", err)
	}
	if s.SetupGreeter {
		PreviewGreeter(out)
	}
	// A first migration can install Niri before native validation is available.
	_, niriErr := exec.LookPath("niri")
	if err := preparePackages(packages, changes, r, *skip, niriErr == nil); err != nil {
		return err
	}
	if err := noDesktop("/proc", os.Getuid()); err != nil {
		return err
	}
	for _, tool := range []string{"niri", "dms", "git", "ghostty", "code", "nautilus"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required desktop command %s is missing; install the core group before applying configuration", tool)
		}
	}
	if err := ValidateDMSVersion(s, r); err != nil {
		return err
	}
	if s.SetupGreeter {
		if err := PreflightGreeter(r); err != nil {
			return err
		}
	}
	changes, err = BuildProfile(home, s)
	if err != nil {
		return err
	}
	if err := ValidateProfile(changes, r); err != nil {
		return fmt.Errorf("configuration validation after upgrade: %w", err)
	}
	if err := InstallPlugins(home, s, r, out); err != nil {
		return err
	}
	if err := noDesktop("/proc", os.Getuid()); err != nil {
		return err
	}
	backup, err := ApplyFiles(home, changes)
	if backup != "" {
		fmt.Fprintln(out, "Backups:", backup)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Plugins installed and activated. Managed widgets appear on the first enabled bar after login.")
	fmt.Fprintln(out, "Bongo Cat typing animation needs input-device permission; see the user guide for the manual step.")
	if containerGroup(s, "core") {
		if err := SetupShellServices(r, out); err != nil {
			return err
		}
	}
	if s.SetupGreeter {
		if err := SetupGreeter(r, out); err != nil {
			return err
		}
	}
	if *cleanup {
		names, err := previewCleanup(r, out)
		if err != nil {
			return err
		}
		if err := ApplyCleanup(names, r); err != nil {
			return fmt.Errorf("application cleanup: %w", err)
		}
	}
	fmt.Fprintln(out, "Setup complete. Log into your normal Niri session, then run check with the same settings file.")
	return nil
}

// Existing Niri validates before any upgrade; a first install validates afterwards
// in runApp, before any user configuration is written.
func preparePackages(packages []Package, changes []FileChange, r Runner, skip, niriAvailable bool) error {
	if niriAvailable || skip {
		if err := ValidateProfile(changes, r); err != nil {
			return fmt.Errorf("configuration validation: %w", err)
		}
	}
	if !skip {
		if err := InstallPackages(packages, r); err != nil {
			return fmt.Errorf("packages: %w; configuration has not been changed", err)
		}
	}
	return nil
}
func checkApplyPlatform() error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return errors.New("apply requires CachyOS Linux x86-64")
	}
	if os.Getuid() == 0 {
		return errors.New("run as your regular user, without sudo")
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "ID=cachyos" || strings.TrimSpace(line) == `ID="cachyos"` {
			return nil
		}
	}
	return errors.New("apply requires CachyOS")
}
func legacyPending(home string) error {
	paths := []string{filepath.Join(home, ".local/state/myarch-buildkit/pending-login.json")}
	if state := os.Getenv("XDG_STATE_HOME"); state != "" {
		paths = append(paths, filepath.Join(state, "myarch-buildkit/pending-login.json"))
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var queue struct {
			Version int               `json:"version"`
			Items   []json.RawMessage `json:"items"`
		}
		empty := strings.TrimSpace(string(data)) == "[]"
		if json.Unmarshal(data, &queue) == nil && queue.Version == 1 && queue.Items != nil && len(queue.Items) == 0 {
			empty = true
		}
		if !empty {
			return fmt.Errorf("older setup has pending state at %s; finish or restore it using the old version before applying this version", p)
		}
	}
	return nil
}
func noDesktop(root string, uid int) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		path := filepath.Join(root, e.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(st.Uid) != uid {
			continue
		}
		data, err := os.ReadFile(filepath.Join(path, "cmdline"))
		if err != nil {
			continue
		}
		parts := strings.Split(string(data), "\x00")
		name := filepath.Base(parts[0])
		if name == "niri" || name == "Hyprland" || name == "hyprland" || name == "qs" || name == "quickshell" || name == "dms" && len(parts) > 1 && parts[1] == "run" {
			return fmt.Errorf("log out of Niri/Hyprland/DMS before apply (%s is running)", name)
		}
	}
	return nil
}
func setupLock(home string) (func(), error) {
	path := filepath.Join(home, ".local/state/myarch-buildkit/setup.lock")
	if err := safeHomePath(home, path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another setup process is running")
	}
	return func() { f.Close() }, nil
}
func main() {
	home, err := os.UserHomeDir()
	if err == nil {
		err = runApp(os.Args[1:], home, RealRunner{}, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func previewCleanup(r Runner, out io.Writer) ([]string, error) {
	names, err := PlanCleanup(r)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(out, "Application cleanup (installed alternatives with installed replacements):")
	if len(names) == 0 {
		fmt.Fprintln(out, "  No eligible packages currently installed.")
	}
	for _, name := range names {
		fmt.Fprintln(out, " ", name)
		if isNoctaliaPackage(name) {
			fmt.Fprintln(out, "    Requires Dux greeter handoff; Niri and Quickshell are kept explicitly installed.")
		}
	}
	fmt.Fprintln(out, "Apply rechecks after installation and uses pacman -R with its normal confirmation; dependencies are not cascaded.")
	return names, nil
}
func reportCompetingDesktop(home string, r Runner, out io.Writer) ([]string, error) {
	findings, err := DetectCompetingDesktop(home, r)
	if len(findings) > 0 {
		fmt.Fprintln(out, "Review competing desktop startup entries before login (no services disabled):")
		for _, finding := range findings {
			fmt.Fprintln(out, " ", finding)
		}
	}
	return findings, err
}
