package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type catalogRunner struct {
	runs      [][]string
	available map[string]bool
	failRun   bool
}

func (r *catalogRunner) Run(args ...string) error {
	r.runs = append(r.runs, append([]string{}, args...))
	if r.failRun {
		return errors.New("transaction failed")
	}
	return nil
}
func (r *catalogRunner) Output(args ...string) ([]byte, error) {
	if r.available[strings.Join(args, " ")] {
		return []byte("available"), nil
	}
	return nil, errors.New("not found")
}
func TestCatalogSelection(t *testing.T) {
	if _, err := SelectedPackages([]string{"typo"}); err == nil {
		t.Fatal("unknown group accepted")
	}
	packages, err := SelectedPackages([]string{"core", "containers", "core"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range packages {
		if seen[p.Name] {
			t.Fatalf("duplicate %s", p.Name)
		}
		seen[p.Name] = true
		if p.Group != "core" && p.Group != "containers" {
			t.Fatal(p)
		}
	}
	if !seen["podman"] || !seen["ghostty"] {
		t.Fatal("curated core missing")
	}
}
func TestCatalogSingleUpgrade(t *testing.T) {
	r := &catalogRunner{available: map[string]bool{"pacman -Si -- git": true, "pacman -Si -- go": true}}
	if err := InstallPackages([]Package{{Name: "git"}, {Name: "go"}}, r); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"sudo", "pacman", "-Syu", "--needed", "--", "git", "go"}}
	if !reflect.DeepEqual(r.runs, want) {
		t.Fatalf("got %v", r.runs)
	}
}
func TestCatalogRequiresHelperBeforeUpgrade(t *testing.T) {
	r := &catalogRunner{}
	if err := InstallPackages([]Package{{Name: "custom-bin", AUR: true}}, r); err == nil {
		t.Fatal("missing helper accepted")
	}
	if len(r.runs) != 0 {
		t.Fatal("mutated before helper check")
	}
}
func TestCatalogUpgradeFailureStopsAUR(t *testing.T) {
	r := &catalogRunner{available: map[string]bool{"paru --version": true}, failRun: true}
	if err := InstallPackages([]Package{{Name: "custom-bin", AUR: true}}, r); err == nil {
		t.Fatal("transaction failure lost")
	}
	if len(r.runs) != 1 || r.runs[0][0] != "sudo" {
		t.Fatalf("unexpected commands %v", r.runs)
	}
}
func TestCatalogFallbackAndCheck(t *testing.T) {
	p := Package{Name: "preferred", RepoCandidates: []string{"preferred", "alternative"}, AURCandidates: []string{"fallback-bin"}}
	r := &catalogRunner{available: map[string]bool{"pacman -Si -- alternative": true, "pacman -Q -- alternative": true}}
	if err := InstallPackages([]Package{p}, r); err != nil {
		t.Fatal(err)
	}
	if got := r.runs[0]; got[len(got)-1] != "alternative" {
		t.Fatal(got)
	}
	if err := CheckPackages([]Package{p}, r); err != nil {
		t.Fatal(err)
	}
	if err := CheckPackages([]Package{{Name: "missing"}}, r); err == nil {
		t.Fatal("missing package accepted")
	}
}

func TestCatalogNiriDesktop(t *testing.T) {
	packages, err := SelectedPackages([]string{"core"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range packages {
		seen[p.Name] = true
	}
	for _, name := range []string{"niri", "xwayland-satellite", "xdg-desktop-portal-gnome", "xdg-desktop-portal-gtk", "gnome-keyring", "nautilus"} {
		if !seen[name] {
			t.Fatalf("missing Niri desktop dependency %s", name)
		}
	}
	if seen["xdg-desktop-portal-hyprland"] || seen["grim"] || seen["slurp"] {
		t.Fatal("obsolete Hyprland screen capture packages retained")
	}
}

func TestProfilePackagesSelectMatchingShellMajor(t *testing.T) {
	for _, directory := range []string{"dms", "DankMaterialShell"} {
		s := defaultSettings()
		s.DMSConfigDir = directory
		packages, err := SelectedProfilePackages(s)
		if err != nil {
			t.Fatal(err)
		}
		want := "dms-shell"
		if directory == "dms" {
			want = "dms-shell-git"
		}
		found := false
		for _, p := range packages {
			if strings.HasPrefix(p.Name, "dms-shell") {
				if p.Name != want {
					t.Fatalf("%s got %s", directory, p.Name)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("shell missing")
		}
	}
}
func TestCoreShellBackendsAndCalendarResolution(t *testing.T) {
	packages, err := SelectedProfilePackages(defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]Package{}
	for _, p := range packages {
		seen[p.Name] = p
	}
	for _, name := range []string{"networkmanager", "bluez", "pipewire", "wireplumber", "polkit", "upower", "dgop", "dsearch", "dcal"} {
		if _, ok := seen[name]; !ok {
			t.Errorf("missing shell backend %s", name)
		}
	}
	r := &catalogRunner{}
	name, aur, err := resolvePackage(seen["dcal"], r)
	if err != nil || !aur || name != "dankcalendar-bin" {
		t.Fatalf("calendar fallback: %s %v %v", name, aur, err)
	}
	if _, ok := seen["power-profiles-daemon"]; ok {
		t.Fatal("must preserve existing power manager")
	}
	for _, p := range packages {
		if strings.HasPrefix(p.Name, "networkmanager-") {
			t.Fatalf("provider-specific VPN forced: %s", p.Name)
		}
	}
}
