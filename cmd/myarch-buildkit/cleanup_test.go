package main

import (
	"errors"
	"reflect"
	"testing"
)

type cleanupRunner struct {
	installed string
	runs      [][]string
	err       error
}

func (r *cleanupRunner) Output(...string) ([]byte, error) { return []byte(r.installed), r.err }
func (r *cleanupRunner) Run(args ...string) error         { r.runs = append(r.runs, args); return r.err }
func TestCleanupRequiresInstalledReplacement(t *testing.T) {
	r := &cleanupRunner{installed: "alacritty kitty nautilus dolphin thunar gnote"}
	names, err := PlanCleanup(r)
	if err != nil || !reflect.DeepEqual(names, []string{"dolphin"}) {
		t.Fatalf("%v %v", names, err)
	}
	if len(r.runs) != 0 {
		t.Fatal("preview mutated system")
	}
}
func TestCleanupRechecksAndNeverCascades(t *testing.T) {
	r := &cleanupRunner{installed: "ghostty alacritty"}
	if err := ApplyCleanup([]string{"alacritty", "alacritty"}, r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.runs, [][]string{{"sudo", "pacman", "-R", "--", "alacritty"}}) {
		t.Fatal(r.runs)
	}
	r.installed = "alacritty"
	r.runs = nil
	if err := ApplyCleanup([]string{"alacritty"}, r); err == nil {
		t.Fatal("removed without replacement")
	}
	if len(r.runs) != 0 {
		t.Fatal("unsafe command ran")
	}
}
func TestCleanupRejectsUnlistedAndInventoryFailure(t *testing.T) {
	r := &cleanupRunner{installed: "ghostty gnote"}
	if err := ApplyCleanup([]string{"gnote"}, r); err == nil {
		t.Fatal("notes removal accepted")
	}
	r.err = errors.New("database unreadable")
	if _, err := PlanCleanup(r); err == nil {
		t.Fatal("inventory failure ignored")
	}
}

func TestCleanupEditorAlternativesKeepCode(t *testing.T) {
	r := &cleanupRunner{installed: "code gedit kate pluma kitty foot thunar nemo"}
	names, err := PlanCleanup(r)
	if err != nil || !reflect.DeepEqual(names, []string{"gedit", "kate", "pluma"}) {
		t.Fatalf("%v %v", names, err)
	}
}

type noctaliaCleanupRunner struct {
	cleanupRunner
	config        string
	greeterFailed bool
}

func (r *noctaliaCleanupRunner) Output(args ...string) ([]byte, error) {
	switch args[0] {
	case "pacman":
		return []byte(r.installed), nil
	case "dms":
		return []byte("dms v2.0.0"), nil
	case "systemctl", "dms-greeter":
		if r.greeterFailed {
			return nil, errors.New("not configured")
		}
		return []byte("enabled"), nil
	case "cat":
		return []byte(r.config), nil
	}
	return nil, errors.New("unexpected query")
}
func TestNoctaliaCleanupRetainsSharedPackages(t *testing.T) {
	r := &noctaliaCleanupRunner{cleanupRunner: cleanupRunner{installed: "niri quickshell dms-shell-git cachyos-niri-noctalia noctalia-shell noctalia-greeter"}, config: "[default_session]\ncommand = \"/usr/bin/dms-greeter --command niri\"\n"}
	names, err := PlanCleanup(r)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"cachyos-niri-noctalia", "noctalia-shell", "noctalia-greeter"}) {
		t.Fatal(names)
	}
	if err := ApplyCleanup(names, r); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"sudo", "pacman", "-D", "--asexplicit", "--", "niri", "quickshell"}, {"sudo", "pacman", "-R", "--", "cachyos-niri-noctalia", "noctalia-shell", "noctalia-greeter"}}
	if !reflect.DeepEqual(r.runs, want) {
		t.Fatal(r.runs)
	}
}
func TestNoctaliaCleanupRequiresGreeterHandoff(t *testing.T) {
	for _, config := range []string{"", "[default_session]\ncommand = \"noctalia-greeter-session\"", "[initial_session]\ncommand = \"dms-greeter --command niri\"", "[default_session]\ncommand = \"custom-wrapper\""} {
		r := &noctaliaCleanupRunner{cleanupRunner: cleanupRunner{installed: "niri quickshell dms-shell noctalia"}, config: config}
		if err := ApplyCleanup([]string{"noctalia"}, r); err == nil {
			t.Fatal("accepted unsafe greeter", config)
		}
		if len(r.runs) != 0 {
			t.Fatal("changed packages before handoff")
		}
	}
	r := &noctaliaCleanupRunner{cleanupRunner: cleanupRunner{installed: "niri quickshell dms-shell noctalia"}, config: "[default_session]\ncommand = \"dms-greeter --command niri\"", greeterFailed: true}
	if err := ApplyCleanup([]string{"noctalia"}, r); err == nil || len(r.runs) != 0 {
		t.Fatal("ignored failed greeter", err)
	}
}
func TestNoctaliaCleanupNeedsInstalledDuxAndNiri(t *testing.T) {
	for _, installed := range []string{"noctalia niri quickshell", "noctalia dms-shell quickshell", "noctalia dms-shell niri"} {
		r := &cleanupRunner{installed: installed}
		names, err := PlanCleanup(r)
		if err != nil || len(names) != 0 {
			t.Fatal(names, err)
		}
		if err := ApplyCleanup([]string{"noctalia"}, r); err == nil {
			t.Fatal("accepted missing replacement")
		}
	}
}
