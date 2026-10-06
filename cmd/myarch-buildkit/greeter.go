package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

var greeterVersionPattern = regexp.MustCompile(`\bv?(\d+)\.(\d+)\.(\d+)\b`)
var greeterDevicePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)
var greeterLayoutPattern = regexp.MustCompile(`^[A-Za-z0-9_,-]+$`)
var displayManagerPattern = regexp.MustCompile(`^[A-Za-z0-9_.@:-]+\.service$`)
var greeterErrorPattern = regexp.MustCompile(`(?i)config error|parse error`)

func GreeterConfigText(binary, compositor, config string) (string, error) {
	if compositor != "Hyprland" && compositor != "hyprland" {
		return "", fmt.Errorf("DankGreeter requires the installed Hyprland compositor")
	}
	command := shellJoin([]string{binary, "--command", "hyprland", "-C", config})
	encoded, _ := json.Marshal(command)
	return "# Managed by myarch-buildkit. Password login; no automatic login.\n[terminal]\nvt = 1\n\n[default_session]\nuser = \"greeter\"\ncommand = " + string(encoded) + "\n", nil
}
func GreeterKeyboardText(kind string, roles map[string][]string, observed map[string]bool, layout string) (string, error) {
	if !greeterLayoutPattern.MatchString(layout) {
		return "", fmt.Errorf("unsupported internal keyboard layout")
	}
	names := []string{}
	for _, name := range roles["internal"] {
		if observed[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	lines := []string{}
	switch kind {
	case "lua":
		lines = append(lines, "-- Managed greeter internal keyboard layout.", `hl.env("DMS_RUN_GREETER", "1")`)
		for _, name := range names {
			key, _ := json.Marshal(name)
			value, _ := json.Marshal(layout)
			lines = append(lines, "hl.device({name="+string(key)+",kb_layout="+string(value)+"})")
		}
	case "hyprlang":
		lines = append(lines, "# Managed greeter internal keyboard layout.", "env = DMS_RUN_GREETER,1")
		for _, name := range names {
			if !greeterDevicePattern.MatchString(name) {
				return "", fmt.Errorf("unsupported internal keyboard name")
			}
			lines = append(lines, "device {", "  name = "+name, "  kb_layout = "+layout, "}")
		}
	default:
		return "", fmt.Errorf("unsupported greeter compositor format")
	}
	return strings.Join(lines, "\n") + "\n", nil
}
func GreetdExecMatches(text string) bool {
	pattern := regexp.MustCompile(`(?:\{|;)\s*path=([^ ;}]+)`)
	matches := pattern.FindAllStringSubmatch(text, -1)
	return len(matches) == 1 && (matches[0][1] == "/usr/bin/greetd" || matches[0][1] == "greetd")
}
func greeterStageKeyboard(c *Context, staging, binary string, record map[string]any) (string, string, error) {
	version := greeterVersionPattern.FindStringSubmatch(str(c.Inventory["hyprland_version"]))
	main := str(c.Inventory["active_config"])
	kind, destination := "", ""
	if len(version) == 4 && version[1] == "0" && (version[2] == "55" || version[2] == "56") && filepath.Ext(main) == ".lua" {
		kind = "lua"
		destination = "/etc/greetd/hypr.lua"
	} else if len(version) == 4 && version[1] == "0" && version[2] == "54" && filepath.Ext(main) == ".conf" {
		kind = "hyprlang"
		destination = "/etc/greetd/hyprland.conf"
	} else {
		return "", "", fmt.Errorf("Hyprland version/config format is unverified for the greeter")
	}
	roles, err := KeyboardRoles(c)
	if err != nil {
		return "", "", err
	}
	observed := map[string]bool{}
	switch keyboards := object(c.Inventory["devices"])["keyboards"].(type) {
	case []any:
		for _, item := range keyboards {
			name, _ := object(item)["name"].(string)
			observed[name] = true
		}
	case []map[string]any:
		for _, item := range keyboards {
			name, _ := item["name"].(string)
			observed[name] = true
		}
	}
	layout := c.Settings.Laptop.InternalKeyboard
	if layout == "" {
		layout = "hu"
	}
	text, err := GreeterKeyboardText(kind, roles, observed, layout)
	if err != nil {
		return "", "", err
	}
	staged := filepath.Join(staging, filepath.Base(destination))
	if err = c.Write(staged, []byte(text), 0600); err != nil {
		return "", "", err
	}
	help := c.Try(binary, "--help")
	if help.Code != 0 || !strings.Contains(help.Stdout+help.Stderr, "--verify-config") || !strings.Contains(help.Stdout+help.Stderr, "--config") {
		return "", "", fmt.Errorf("Hyprland lacks native greeter config verification")
	}
	result := c.Try(binary, "--verify-config", "--config", c.View(staged))
	if result.Code != 0 || greeterErrorPattern.MatchString(result.Stdout+result.Stderr) {
		return "", "", fmt.Errorf("greeter keyboard configuration failed Hyprland validation: %s", strings.TrimSpace(result.Stdout+result.Stderr))
	}
	record["keyboard"] = map[string]any{"config": destination, "format": kind, "default": layout, "external_overrides": false, "native_config_verified": true}
	return staged, destination, nil
}

// ConfigureGreeter prepares password login for the next boot and never restarts a session.
func ConfigureGreeter(c *Context) (err error) {
	record := map[string]any{"status": "checking", "autologin": false, "host_compositor": "Hyprland", "current_session_restarted": false, "appearance": "Packaged defaults; no appearance snapshot copied.", "verification": "Static preflight only; next-boot password login must be tested."}
	c.Report["greeter"] = record
	defer func() {
		if err != nil {
			record["status"] = "needs_attention"
			record["error"] = err.Error()
			record["automatic_restore"] = false
			c.Save()
			c.Warn("DankGreeter needs attention: " + err.Error())
		}
	}()
	for _, name := range []string{"/usr/bin/dms-greeter", "/usr/bin/greetd", "/usr/bin/qs"} {
		if info, e := os.Stat(name); e != nil || info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("required greeter runtime is missing: %s", name)
		}
	}
	binary := "/usr/bin/Hyprland"
	if !exists(binary) {
		binary = "/usr/bin/hyprland"
	}
	if !exists(binary) {
		return fmt.Errorf("installed Hyprland compositor is missing")
	}
	help := c.Try("/usr/bin/dms-greeter", "--help")
	text := help.Stdout + help.Stderr
	if help.Code != 0 || !strings.Contains(text, "--command") || !regexp.MustCompile(`(?:^|\s)-C(?:,|\s)`).MatchString(text) {
		return fmt.Errorf("installed dms-greeter does not advertise --command and -C")
	}
	record["greeter_help_checked"] = true
	unit, err := c.Run("systemctl", "show", "greetd.service", "--property=FragmentPath", "--value")
	if err != nil {
		return err
	}
	unitPath := strings.TrimSpace(unit.Stdout)
	if !filepath.IsAbs(unitPath) || !exists(unitPath) {
		return fmt.Errorf("installed greetd.service could not be located")
	}
	effective, err := c.Run("systemctl", "show", "greetd.service", "--property=ExecStart", "--value")
	if err != nil {
		return err
	}
	record["effective_exec_start"] = strings.TrimSpace(effective.Stdout + effective.Stderr)
	if !GreetdExecMatches(str(record["effective_exec_start"])) {
		return fmt.Errorf("greetd.service has an unexpected executable")
	}
	if _, err = c.Run("systemd-analyze", "verify", unitPath); err != nil {
		return err
	}
	if !exists("/etc/pam.d/greetd") {
		return fmt.Errorf("packaged greetd PAM policy is missing; authentication files are left unchanged")
	}
	sessions, _ := filepath.Glob("/usr/share/wayland-sessions/*hypr*.desktop")
	if len(sessions) == 0 {
		return fmt.Errorf("no packaged Hyprland login session was found")
	}
	record["wayland_sessions"] = sessions
	account := c.Try("getent", "passwd", "greeter")
	if account.Code != 0 {
		declaration := "/usr/lib/sysusers.d/dms-greeter.conf"
		if !exists(declaration) {
			return fmt.Errorf("packaged greeter account/declaration is missing")
		}
		if _, err = c.Run("sudo", "-n", "systemd-sysusers", declaration); err != nil {
			return err
		}
		account, err = c.Run("getent", "passwd", "greeter")
		if err != nil {
			return err
		}
	}
	fields := strings.Split(strings.TrimSpace(account.Stdout), ":")
	if len(fields) != 7 || fields[0] != "greeter" {
		return fmt.Errorf("unable to identify packaged greeter account")
	}
	accountUID, uidErr := strconv.Atoi(fields[2])
	accountGID, gidErr := strconv.Atoi(fields[3])
	if uidErr != nil || gidErr != nil || accountUID <= 0 || accountGID <= 0 {
		return fmt.Errorf("packaged greeter account has invalid numeric identity")
	}
	group, err := c.Run("getent", "group", fields[3])
	if err != nil {
		return err
	}
	groupName := strings.SplitN(strings.TrimSpace(group.Stdout), ":", 2)[0]
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`).MatchString(groupName) {
		return fmt.Errorf("unable to identify packaged greeter primary group")
	}
	record["greeter_group"] = groupName
	if !exists(fields[5]) {
		declaration := "/usr/lib/tmpfiles.d/dms-greeter.conf"
		if !exists(declaration) {
			return fmt.Errorf("packaged greeter home/tmpfiles declaration is missing")
		}
		if _, err = c.Run("sudo", "-n", "systemd-tmpfiles", "--create", declaration); err != nil {
			return err
		}
	}
	staging := filepath.Join(c.RunDir, "greeter-staging")
	staged, destination, err := greeterStageKeyboard(c, staging, binary, record)
	if err != nil {
		return err
	}
	config, err := GreeterConfigText("/usr/bin/dms-greeter", filepath.Base(binary), destination)
	if err != nil {
		return err
	}
	for _, path := range []string{"/var", "/var/cache", "/var/cache/dms-greeter"} {
		if c.Try("sudo", "-n", "/usr/bin/test", "-L", path).Code == 0 {
			return fmt.Errorf("greeter cache directory is a symlink")
		}
		if path != "/var/cache/dms-greeter" {
			stat, err := c.Run("sudo", "-n", "/usr/bin/stat", "-c", "%u:%a:%F", "--", path)
			if err != nil {
				return err
			}
			if err = validateRootStat(path, stat.Stdout); err != nil {
				return err
			}
		}
	}
	if _, err = c.Run("sudo", "-n", "/usr/bin/install", "-d", "-m", "2770", "-o", "greeter", "-g", groupName, "/var/cache/dms-greeter"); err != nil {
		return err
	}
	stagedConfig := filepath.Join(staging, "config.toml")
	if err = c.Write(stagedConfig, []byte(config), 0600); err != nil {
		return err
	}
	data, err := os.ReadFile(c.View(staged))
	if err != nil {
		return err
	}
	if err = c.SystemWrite(destination, data, 0644); err != nil {
		return err
	}
	if err = c.SystemWrite("/etc/greetd/config.toml", []byte(config), 0644); err != nil {
		return err
	}
	record["installed_paths"] = []string{destination, "/etc/greetd/config.toml"}
	record["root_boot_journal"] = true
	record["run_id"] = c.systemRunID()
	record["boot_transition_started"] = true
	if err = c.Save(); err != nil {
		return err
	}
	result, err := rootOperation(c, "internal-greeter-boot", systemPayload{RunID: c.systemRunID()})
	if err != nil {
		return err
	}
	var boot map[string]any
	if err = json.Unmarshal([]byte(result.Stdout), &boot); err != nil {
		return err
	}
	for key, value := range boot {
		record[key] = value
	}
	record["status"] = "enabled_for_next_boot"
	record["restore_instructions"] = shellJoin([]string{c.Binary, "--restore-stage", "greeter", "--stage-run-dir", c.RunDir}) + "; restoration never restarts the current login."
	if err = c.Save(); err != nil {
		return err
	}
	c.Note("DankGreeter is configured for password login at the next boot. The current display manager remains running.")
	return nil
}

type greeterBootJournal struct {
	UID                    int    `json:"requester_uid"`
	RunID                  string `json:"run_id"`
	PreviousDisplayManager string `json:"previous_display_manager"`
	PreviousEnabled        bool   `json:"previous_display_manager_enabled"`
	PreviousActive         bool   `json:"previous_display_manager_active"`
	AliasExists            bool   `json:"alias_exists"`
	AliasTarget            string `json:"alias_target"`
	NewAliasTarget         string `json:"new_alias_target,omitempty"`
	Started                bool   `json:"boot_transition_started"`
	Completed              bool   `json:"boot_transition_completed"`
	Restored               bool   `json:"boot_transition_restored,omitempty"`
}

func greeterBootPath(root string, uid int, runID string) (string, error) {
	path, _, err := rootJournalPaths(root, uid, runID, "greeter-boot")
	return path, err
}
func saveGreeterBoot(path string, j greeterBootJournal) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return rootAtomic(path, append(data, '\n'), 0600, 0, 0)
}
func privilegedGreeterBoot(root string, uid int, p systemPayload) (greeterBootJournal, error) {
	var j greeterBootJournal
	if root != "" {
		return j, fmt.Errorf("boot transitions require the actual system")
	}
	path, err := greeterBootPath(root, uid, p.RunID)
	if err != nil {
		return j, err
	}
	if data, e := os.ReadFile(path); e == nil {
		if err = json.Unmarshal(data, &j); err != nil {
			return j, err
		}
		if j.UID != uid || j.RunID != p.RunID || j.Restored {
			return j, fmt.Errorf("boot journal identity does not match")
		}
		if j.Completed {
			return j, nil
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return j, e
	} else {
		j = greeterBootJournal{UID: uid, RunID: p.RunID}
		if err = safeRootPath("", "/etc/systemd/system", false); err != nil {
			return j, err
		}
		alias := "/etc/systemd/system/display-manager.service"
		info, e := os.Lstat(alias)
		if e == nil {
			if info.Mode()&os.ModeSymlink == 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
				return j, fmt.Errorf("display-manager alias is not a symlink")
			}
			j.AliasExists = true
			j.AliasTarget, e = os.Readlink(alias)
			if e != nil {
				return j, e
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return j, e
		}
		old, e := executeRoot("/usr/bin/systemctl", "show", "display-manager.service", "--property=Id", "--value")
		if e == nil {
			j.PreviousDisplayManager = strings.TrimSpace(old)
		}
		if j.PreviousDisplayManager == "display-manager.service" {
			j.PreviousDisplayManager = filepath.Base(j.AliasTarget)
		}
		if j.PreviousDisplayManager != "" && !displayManagerPattern.MatchString(j.PreviousDisplayManager) {
			return j, fmt.Errorf("unsupported previous display manager")
		}
		if j.PreviousDisplayManager != "" {
			_, e = executeRoot("/usr/bin/systemctl", "is-enabled", "--quiet", j.PreviousDisplayManager)
			j.PreviousEnabled = e == nil
			_, e = executeRoot("/usr/bin/systemctl", "is-active", "--quiet", j.PreviousDisplayManager)
			j.PreviousActive = e == nil
		}
	}
	j.Started = true
	if err = saveGreeterBoot(path, j); err != nil {
		return j, err
	}
	if j.PreviousDisplayManager != "" && j.PreviousDisplayManager != "greetd.service" && j.PreviousEnabled {
		if _, err = executeRoot("/usr/bin/systemctl", "disable", j.PreviousDisplayManager); err != nil {
			return j, err
		}
	}
	if _, err = executeRoot("/usr/bin/systemctl", "enable", "--force", "greetd.service"); err != nil {
		return j, err
	}
	if _, err = executeRoot("/usr/bin/systemctl", "is-enabled", "--quiet", "greetd.service"); err != nil {
		return j, err
	}
	j.NewAliasTarget, err = os.Readlink("/etc/systemd/system/display-manager.service")
	if err != nil || filepath.Base(j.NewAliasTarget) != "greetd.service" {
		return j, fmt.Errorf("boot alias does not select greetd")
	}
	if j.PreviousDisplayManager != "" && j.PreviousDisplayManager != "greetd.service" && j.PreviousActive {
		if _, err = executeRoot("/usr/bin/systemctl", "is-active", "--quiet", j.PreviousDisplayManager); err != nil {
			return j, fmt.Errorf("previously active display manager is no longer active")
		}
	}
	j.Completed = true
	return j, saveGreeterBoot(path, j)
}
func privilegedGreeterRestore(root string, uid int, p systemPayload) error {
	if root != "" {
		return fmt.Errorf("boot transitions require the actual system")
	}
	path, err := greeterBootPath(root, uid, p.RunID)
	if err != nil {
		return err
	}
	if err = safeRootPath("", path, false); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var j greeterBootJournal
	if err = json.Unmarshal(data, &j); err != nil {
		return err
	}
	if j.UID != uid || j.RunID != p.RunID {
		return fmt.Errorf("boot restoration journal identity does not match")
	}
	if j.Restored {
		return nil
	}
	if j.PreviousDisplayManager != "" && !displayManagerPattern.MatchString(j.PreviousDisplayManager) {
		return fmt.Errorf("invalid previous display manager journal")
	}
	if err = safeRootPath("", "/etc/systemd/system", false); err != nil {
		return err
	}
	alias := "/etc/systemd/system/display-manager.service"
	current, e := os.Readlink(alias)
	if e == nil && current != j.AliasTarget && current != j.NewAliasTarget && !(j.NewAliasTarget == "" && filepath.Base(current) == "greetd.service") {
		return fmt.Errorf("boot alias changed since setup; restore manually")
	}
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if j.PreviousDisplayManager != "greetd.service" || !j.PreviousEnabled {
		if _, err = executeRoot("/usr/bin/systemctl", "disable", "greetd.service"); err != nil {
			return err
		}
	}
	if j.PreviousEnabled && j.PreviousDisplayManager != "" {
		if _, err = executeRoot("/usr/bin/systemctl", "enable", "--force", j.PreviousDisplayManager); err != nil {
			return err
		}
	}
	if err = os.Remove(alias); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if j.AliasExists {
		if err = os.Symlink(j.AliasTarget, alias); err != nil {
			return err
		}
	}
	if _, err = executeRoot("/usr/bin/systemctl", "daemon-reload"); err != nil {
		return err
	}
	j.Restored = true
	return saveGreeterBoot(path, j)
}
func RestoreGreeter(c *Context) error {
	record := object(c.Report["greeter"])
	if record["root_boot_journal"] != true || record["boot_transition_restored"] == true {
		return nil
	}
	runID, _ := record["run_id"].(string)
	if _, err := rootOperation(c, "internal-greeter-restore", systemPayload{RunID: runID}); err != nil {
		return err
	}
	record["boot_transition_restored"] = true
	return c.Save()
}
