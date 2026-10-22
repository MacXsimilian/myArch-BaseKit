package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Explicit application alternatives. Noctalia has a separate allowlist and
// replacement checks below; notes apps are never cleanup candidates.
var cleanupAlternatives = []struct {
	name         string
	replacements []string
}{
	{"alacritty", []string{"ghostty"}},
	{"dolphin", []string{"nautilus"}},
	{"gnome-text-editor", []string{"visual-studio-code-bin", "code"}},
	{"gedit", []string{"visual-studio-code-bin", "code"}},
	{"mousepad", []string{"visual-studio-code-bin", "code"}},
	{"xed", []string{"visual-studio-code-bin", "code"}},
	{"kate", []string{"visual-studio-code-bin", "code"}},
	{"kwrite", []string{"visual-studio-code-bin", "code"}},
	{"leafpad", []string{"visual-studio-code-bin", "code"}},
	{"pluma", []string{"visual-studio-code-bin", "code"}},
}

var noctaliaPackages = []string{"cachyos-niri-noctalia", "noctalia", "noctalia-git", "noctalia-shell", "noctalia-shell-git", "noctalia-greeter", "noctalia-greeter-git"}

func isNoctaliaPackage(name string) bool {
	for _, candidate := range noctaliaPackages {
		if name == candidate {
			return true
		}
	}
	return false
}

func installedDMS(installed map[string]bool) string {
	for _, name := range []string{"dms-shell-git", "dms-shell", "dms-shell-bin"} {
		if installed[name] {
			return name
		}
	}
	return ""
}

func installedPackageSet(r Runner) (map[string]bool, error) {
	output, err := r.Output("pacman", "-Qq")
	if err != nil {
		return nil, fmt.Errorf("read installed packages: %w", err)
	}
	result := map[string]bool{}
	for _, name := range strings.Fields(string(output)) {
		result[name] = true
	}
	return result, nil
}
func PlanCleanup(r Runner) ([]string, error) {
	installed, err := installedPackageSet(r)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, candidate := range cleanupAlternatives {
		if !installed[candidate.name] {
			continue
		}
		for _, replacement := range candidate.replacements {
			if installed[replacement] {
				names = append(names, candidate.name)
				break
			}
		}
	}
	if installedDMS(installed) != "" && installed["niri"] && installed["quickshell"] {
		for _, name := range noctaliaPackages {
			if installed[name] {
				names = append(names, name)
			}
		}
	}
	return names, nil
}
func ApplyCleanup(names []string, r Runner) error {
	if len(names) == 0 {
		return nil
	}
	allowed, err := PlanCleanup(r)
	if err != nil {
		return err
	}
	safe := map[string]bool{}
	for _, name := range allowed {
		safe[name] = true
	}
	seen := map[string]bool{}
	var selected []string
	for _, name := range names {
		if !safe[name] {
			return fmt.Errorf("cleanup refused %s: package or installed replacement changed; preview again", name)
		}
		if !seen[name] {
			selected = append(selected, name)
			seen[name] = true
		}
	}
	removesNoctalia := false
	for _, name := range selected {
		removesNoctalia = removesNoctalia || isNoctaliaPackage(name)
	}
	if removesNoctalia {
		if err := checkNoctaliaReplacement(r); err != nil {
			return err
		}
		// Keep the compositor and shared shell runtime installed even if removing
		// the CachyOS preset leaves them classified as orphan dependencies.
		if err := r.Run("sudo", "pacman", "-D", "--asexplicit", "--", "niri", "quickshell"); err != nil {
			return err
		}
	}
	// No recursive dependency removal, cascade, or unattended confirmation.
	return r.Run(append([]string{"sudo", "pacman", "-R", "--"}, selected...)...)
}

func checkNoctaliaReplacement(r Runner) error {
	version, err := r.Output("dms", "version")
	if err != nil || !regexp.MustCompile(`(?m)^dms(?: \(git\))? v?[12]\.[0-9]+\.[0-9]+`).Match(version) {
		return fmt.Errorf("Noctalia cleanup requires a working Dux/DMS CLI; run setup and verify the Niri session first")
	}
	if err := CheckGreeter(r); err != nil {
		return fmt.Errorf("Noctalia cleanup requires the replacement Dux greeter: %w", err)
	}
	config, err := r.Output("cat", "/etc/greetd/config.toml")
	if err != nil {
		return fmt.Errorf("cannot verify greeter handoff; Noctalia retained: %w", err)
	}
	// Accept only the direct upstream greeter command in default_session. A
	// wrapper or custom config needs manual review rather than guessed safety.
	section, found := "", false
	command := regexp.MustCompile(`^command\s*=\s*["'](?:/usr/bin/)?dms-greeter(?:\s+[^"']*)?["']\s*(?:#.*)?$`)
	for _, line := range strings.Split(string(config), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(strings.ToLower(line), "noctalia") {
			return fmt.Errorf("greetd still references Noctalia; complete greeter setup before cleanup")
		}
		if strings.HasPrefix(line, "[") {
			section = line
		}
		if section == "[default_session]" && command.MatchString(line) {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("greetd default_session is not a direct dms-greeter command; Noctalia retained")
	}
	return nil
}
