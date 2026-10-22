package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func shellServiceActive(r Runner, unit string) (bool, error) {
	output, err := r.Output("systemctl", "show", "--property=ActiveState", "--value", "--", unit)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", unit, err)
	}
	switch strings.TrimSpace(string(output)) {
	case "active", "reloading":
		return true, nil
	case "inactive", "failed":
		return false, nil
	default:
		return false, fmt.Errorf("%s has unexpected or transitioning state %q; retry after it settles", unit, strings.TrimSpace(string(output)))
	}
}

// Keep existing networking intact. An active alternative requires an explicit
// manual migration. With NetworkManager already active, its iwd backend is left alone.
func SetupShellServices(r Runner, out io.Writer) error {
	active, err := shellServiceActive(r, "NetworkManager.service")
	if err != nil {
		return err
	}
	if !active {
		competing, err := r.Output("systemctl", "list-units", "--type=service", "--state=active,activating,reloading", "--no-legend", "--plain", "--no-pager", "--", "systemd-networkd.service", "connman.service", "iwd.service", "dhcpcd.service", "dhcpcd@*.service", "netctl@*.service")
		if err != nil {
			return fmt.Errorf("inspect active network services: %w", err)
		}
		if strings.TrimSpace(string(competing)) != "" {
			return fmt.Errorf("NetworkManager is required for VPN controls, but another network manager is active; migrate networking manually before retrying (nothing was stopped): %s", strings.TrimSpace(string(competing)))
		}
	}
	if err := r.Run("sudo", "systemctl", "enable", "--now", "NetworkManager.service"); err != nil {
		return fmt.Errorf("enable NetworkManager for network/VPN controls: %w", err)
	}
	if err := r.Run("sudo", "systemctl", "enable", "--now", "bluetooth.service"); err != nil {
		return fmt.Errorf("enable Bluetooth controls: %w", err)
	}
	fmt.Fprintln(out, "NetworkManager and Bluetooth services enabled. Import your own VPN profile to use VPN controls.")
	return nil
}

func CheckShellServices(r Runner) error {
	entries, err := os.ReadDir("/sys/class/bluetooth")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	adapters := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "hci") {
			adapters = true
			break
		}
	}
	return checkShellServices(r, adapters)
}
func checkShellServices(r Runner, adapters bool) error {
	active, err := shellServiceActive(r, "NetworkManager.service")
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("NetworkManager is inactive; network/VPN controls need NetworkManager (run apply or migrate your existing network manager manually)")
	}
	if !adapters {
		return nil
	}
	active, err = shellServiceActive(r, "bluetooth.service")
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("Bluetooth adapter detected but bluetooth.service is inactive; enable it to use Bluetooth controls")
	}
	return nil
}
