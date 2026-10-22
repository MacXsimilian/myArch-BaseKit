package main

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type shellServiceRunner struct {
	nm, bluetooth, competing string
	probeError               bool
	failRun                  int
	runs                     [][]string
}

func (r *shellServiceRunner) Output(args ...string) ([]byte, error) {
	if r.probeError {
		return nil, errors.New("system bus unavailable")
	}
	if reflect.DeepEqual(args, []string{"systemctl", "show", "--property=ActiveState", "--value", "--", "NetworkManager.service"}) {
		return []byte(r.nm), nil
	}
	if reflect.DeepEqual(args, []string{"systemctl", "show", "--property=ActiveState", "--value", "--", "bluetooth.service"}) {
		return []byte(r.bluetooth), nil
	}
	if len(args) > 2 && args[0] == "systemctl" && args[1] == "list-units" {
		if !strings.Contains(strings.Join(args, " "), "iwd.service") {
			return nil, errors.New("standalone iwd not checked")
		}
		return []byte(r.competing), nil
	}
	return nil, errors.New("unexpected probe")
}
func (r *shellServiceRunner) Run(args ...string) error {
	r.runs = append(r.runs, args)
	if r.failRun == len(r.runs) {
		return errors.New("enable failed")
	}
	return nil
}
func TestSetupShellServices(t *testing.T) {
	for _, state := range []string{"active", "inactive"} {
		r := &shellServiceRunner{nm: state}
		if err := SetupShellServices(r, io.Discard); err != nil {
			t.Fatal(err)
		}
		expected := [][]string{{"sudo", "systemctl", "enable", "--now", "NetworkManager.service"}, {"sudo", "systemctl", "enable", "--now", "bluetooth.service"}}
		if !reflect.DeepEqual(r.runs, expected) {
			t.Fatal(r.runs)
		}
	}
}
func TestShellServicesPreserveOtherNetworking(t *testing.T) {
	for _, other := range []string{"systemd-networkd.service", "connman.service", "iwd.service", "dhcpcd@eth0.service"} {
		r := &shellServiceRunner{nm: "inactive", competing: other + " loaded active running"}
		if err := SetupShellServices(r, io.Discard); err == nil || !strings.Contains(err.Error(), "migrate") {
			t.Fatal(err)
		}
		if len(r.runs) != 0 {
			t.Fatal("changed services before conflict resolution")
		}
	}
}
func TestShellServicesFailuresStop(t *testing.T) {
	for _, r := range []*shellServiceRunner{{nm: "activating"}, {probeError: true}, {nm: "active", failRun: 1}} {
		if err := SetupShellServices(r, io.Discard); err == nil {
			t.Fatal("failure accepted")
		}
		if len(r.runs) > 1 {
			t.Fatal("continued after failure")
		}
	}
}
func TestCheckShellServicesHardwareAware(t *testing.T) {
	r := &shellServiceRunner{nm: "active", bluetooth: "inactive"}
	if err := checkShellServices(r, false); err != nil {
		t.Fatal(err)
	}
	if err := checkShellServices(r, true); err == nil {
		t.Fatal("inactive Bluetooth accepted with hardware")
	}
	r.bluetooth = "active"
	if err := checkShellServices(r, true); err != nil {
		t.Fatal(err)
	}
	r.nm = "inactive"
	if err := checkShellServices(r, false); err == nil {
		t.Fatal("inactive networking accepted")
	}
	if len(r.runs) != 0 {
		t.Fatal("check mutated services")
	}
}

func TestShellServicesKeepNetworkManagerIWDBackend(t *testing.T) {
	r := &shellServiceRunner{nm: "active", competing: "iwd.service loaded active running"}
	if err := SetupShellServices(r, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, args := range r.runs {
		if strings.Contains(strings.Join(args, " "), "iwd") {
			t.Fatal("modified existing iwd backend")
		}
	}
}
