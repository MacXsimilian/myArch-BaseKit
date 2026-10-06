package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// DesktopSession is the only DMS startup path. The advisory lock survives exec
// for the lifetime of DMS; it never starts or stops another desktop shell.
func DesktopSession(c *Context) error {
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" || os.Getenv("WAYLAND_DISPLAY") == "" {
		return fmt.Errorf("DMS startup requires the intended Hyprland session")
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return fmt.Errorf("XDG_RUNTIME_DIR is required")
	}
	fd, err := syscall.Open(filepath.Join(runtimeDir, "cachyos-dms-start.lock"), syscall.O_WRONLY|syscall.O_CREAT|syscall.O_APPEND|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		syscall.Close(fd)
		if err == syscall.EWOULDBLOCK {
			return nil
		}
		return err
	}
	if _, err = c.Run("systemctl", "--user", "import-environment", "WAYLAND_DISPLAY", "HYPRLAND_INSTANCE_SIGNATURE", "XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE"); err != nil {
		syscall.Close(fd)
		return err
	}
	if err = c.Err(); err == nil {
		err = syscall.Exec("/usr/bin/dms", []string{"dms", "run"}, os.Environ())
	}
	syscall.Close(fd)
	return err
}
func ScreenshotMain(c *Context, args []string) (result error) {
	if len(args) != 1 || (args[0] != "region" && args[0] != "focused") {
		return fmt.Errorf("usage: cachyos-screenshot region|focused")
	}
	command := []string{"grim", "-t", "png"}
	if args[0] == "region" {
		reply, err := c.Runner.Execute(Command{Args: []string{"slurp"}, Timeout: 300 * time.Second})
		if err != nil {
			return err
		}
		geometry := strings.TrimSpace(reply.Stdout)
		if reply.Code != 0 || geometry == "" {
			return nil
		}
		if !regexp.MustCompile(`^-?\d+,-?\d+ \d+x\d+$`).MatchString(geometry) {
			return fmt.Errorf("selected screenshot region was invalid")
		}
		command = append(command, "-g", geometry)
	} else {
		reply, err := c.Command(Command{Args: []string{"hyprctl", "-j", "monitors"}, Timeout: 10 * time.Second})
		if err != nil {
			return err
		}
		var monitors []map[string]any
		if err = json.Unmarshal([]byte(reply.Stdout), &monitors); err != nil {
			return err
		}
		focused := []string{}
		for _, monitor := range monitors {
			if monitor["focused"] == true && monitor["disabled"] != true {
				if name, ok := monitor["name"].(string); ok {
					focused = append(focused, name)
				}
			}
		}
		if len(focused) != 1 {
			return fmt.Errorf("could not identify one focused monitor")
		}
		command = append(command, "-o", focused[0])
	}
	folder := filepath.Join(c.Home, "Pictures/Screenshots")
	if err := os.MkdirAll(folder, 0700); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return err
	}
	home, err := filepath.EvalSymlinks(c.Home)
	if err != nil {
		return err
	}
	info, err := os.Stat(folder)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !inside(home, resolved) || stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("screenshot directory must be owned by you inside your home")
	}
	file, err := os.CreateTemp(folder, time.Now().Format("Screenshot-2006-01-02-150405-")+"*.png")
	if err != nil {
		return err
	}
	name := file.Name()
	if err = file.Close(); err != nil {
		os.Remove(name)
		return err
	}
	defer func() {
		if result != nil {
			os.Remove(name)
		}
	}()
	if _, err = c.Command(Command{Args: append(command, name), Timeout: 30 * time.Second}); err != nil {
		return err
	}
	image, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	if len(image) < 8 || !bytes.Equal(image[:8], []byte{'\x89', 'P', 'N', 'G', '\r', '\n', '\x1a', '\n'}) {
		return fmt.Errorf("screenshot tool did not produce a PNG")
	}
	if _, err = c.Command(Command{Args: []string{"wl-copy", "--type", "image/png"}, Input: image, Timeout: 15 * time.Second}); err != nil {
		fmt.Fprintf(os.Stderr, "Saved %s; copying to the clipboard failed\n", name)
		return nil
	}
	fmt.Println(name)
	return nil
}

const ClipboardInstructions = `History is initially disabled pending the Bitwarden dummy-item check.
1. Keep real credentials out of the clipboard during this check.
2. Run cachyos-clipboard enable, then create/copy a harmless dummy password in Bitwarden.
3. Open Super+V. Check whether that dummy password appears in history.
4. Wait for Bitwarden's clipboard clearing timeout and check history again.
5. If either check retains the dummy password, run cachyos-clipboard disable
   followed by cachyos-clipboard clear; ordinary copy/paste still works.
6. If both checks meet your expectations, leave history enabled. Its limit is
   100 ordinary entries; clearing happens on every DMS start and may use disk.
The DMS source honors a password-manager MIME hint. That does not prove that
your installed Bitwarden client supplies it for every copy operation.
`

func ClipboardMain(c *Context, args []string) error {
	action := "instructions"
	if len(args) > 1 {
		return fmt.Errorf("usage: cachyos-clipboard instructions|show|enable|disable|clear|status")
	}
	if len(args) == 1 {
		action = args[0]
	}
	command := []string{"dms"}
	switch action {
	case "instructions":
		fmt.Print(ClipboardInstructions)
		return nil
	case "show":
		command = append(command, "ipc", "call", "clipboardPlus", "togglePanel")
	case "disable":
		command = append(command, "cl", "config", "set", "--disable")
	case "enable":
		command = append(command, "cl", "config", "set", "--max-history", "100", "--auto-clear-days", "0", "--clear-at-startup", "--enable")
	case "clear":
		command = append(command, "cl", "clear")
	case "status":
		command = append(command, "cl", "config", "get")
	default:
		return fmt.Errorf("usage: cachyos-clipboard instructions|show|enable|disable|clear|status")
	}
	result, err := c.Run(command...)
	if result.Stdout != "" {
		fmt.Print(result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprint(os.Stderr, result.Stderr)
	}
	if err != nil {
		return err
	}
	if action == "enable" {
		fmt.Println("Clipboard history is enabled. It may use disk; entries are cleared when DMS starts.")
	}
	return nil
}
