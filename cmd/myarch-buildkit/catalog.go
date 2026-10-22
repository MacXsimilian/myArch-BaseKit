package main

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed packages.txt
var packageCatalog string

type Package struct {
	Name           string
	Group          string
	AUR            bool
	RepoCandidates []string
	AURCandidates  []string
}

// SelectedPackages is an offline preview. Apply resolves the listed alternatives.
func SelectedPackages(groups []string) ([]Package, error) {
	selected := map[string]bool{}
	for _, group := range groups {
		switch group {
		case "core", "utilities", "development", "containers":
		default:
			return nil, fmt.Errorf("unknown package group: %s", group)
		}
		selected[group] = true
	}
	var packages []Package
	seen := map[string]bool{}
	for _, row := range strings.Split(strings.TrimSpace(packageCatalog), "\n") {
		fields := strings.Split(row, "|")
		if !selected[fields[0]] {
			continue
		}
		repo, aur := candidates(fields[1]), candidates(fields[2])
		name := ""
		if len(repo) > 0 {
			name = repo[0]
		} else if len(aur) > 0 {
			name = aur[0]
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		packages = append(packages, Package{Name: name, Group: fields[0], AUR: len(repo) == 0, RepoCandidates: repo, AURCandidates: aur})
	}
	return packages, nil
}
func candidates(value string) []string {
	if value == "-" {
		return nil
	}
	return strings.Fields(value)
}

func resolvePackage(p Package, r Runner) (string, bool, error) {
	repo, aur := p.RepoCandidates, p.AURCandidates
	if len(repo) == 0 && len(aur) == 0 {
		if p.AUR {
			aur = []string{p.Name}
		} else {
			repo = []string{p.Name}
		}
	}
	for _, name := range repo {
		if _, err := r.Output("pacman", "-Si", "--", name); err == nil {
			return name, false, nil
		}
	}
	if len(aur) > 0 {
		return aur[0], true, nil
	}
	return "", false, fmt.Errorf("repository package unavailable: %s", p.Name)
}

func InstallPackages(packages []Package, r Runner) error {
	if len(packages) == 0 {
		return nil
	}
	var repo, aur []string
	seen := map[string]bool{}
	for _, p := range packages {
		name, isAUR, err := resolvePackage(p, r)
		if err != nil {
			return err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if isAUR {
			aur = append(aur, name)
		} else {
			repo = append(repo, name)
		}
	}
	helper := ""
	if len(aur) > 0 {
		for _, candidate := range []string{"paru", "yay"} {
			if _, err := r.Output(candidate, "--version"); err == nil {
				helper = candidate
				break
			}
		}
		if helper == "" {
			return fmt.Errorf("AUR packages require an installed paru or yay helper: %s", strings.Join(aur, ", "))
		}
	}
	args := append([]string{"sudo", "pacman", "-Syu", "--needed", "--"}, repo...)
	if err := r.Run(args...); err != nil {
		return fmt.Errorf("system upgrade: %w", err)
	}
	if len(aur) > 0 {
		if err := r.Run(append([]string{helper, "-S", "--needed", "--"}, aur...)...); err != nil {
			return fmt.Errorf("AUR installation: %w", err)
		}
	}
	return nil
}

func CheckPackages(packages []Package, r Runner) error {
	var missing []string
	for _, p := range packages {
		names := append(append([]string{}, p.RepoCandidates...), p.AURCandidates...)
		if len(names) == 0 {
			names = []string{p.Name}
		}
		found := false
		for _, name := range names {
			if _, err := r.Output("pacman", "-Q", "--", name); err == nil {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, p.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing packages: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Dux 2 is newer than the stable Arch shell package. Select the maintained
// upstream git package explicitly instead of installing a mismatched major.
func SelectedProfilePackages(s Settings) ([]Package, error) {
	packages, err := SelectedPackages(s.Groups)
	if err != nil {
		return nil, err
	}
	if s.DMSConfigDir == "dms" {
		for i, p := range packages {
			if p.Name == "dms-shell" {
				packages[i] = Package{Name: "dms-shell-git", Group: p.Group, RepoCandidates: []string{"dms-shell-git"}, AURCandidates: []string{"dms-shell-git"}}
			}
		}
	}
	return packages, nil
}
