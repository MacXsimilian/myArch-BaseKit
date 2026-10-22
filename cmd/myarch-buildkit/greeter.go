package main

import (
	"fmt"
	"io"
)

// The upstream installer owns privileged configuration, permissions, backups,
// and display-manager switching. Do not duplicate that machinery here.
func GreeterPackage() Package {
	return Package{Name: "greetd-dms-greeter", Group: "greeter", RepoCandidates: []string{"greetd-dms-greeter", "greetd-dms-greeter-bin", "greetd-dms-greeter-git"}, AURCandidates: []string{"greetd-dms-greeter-git"}}
}

func PreviewGreeter(out io.Writer) {
	fmt.Fprintln(out, "Greeter setup (enabled in settings): install greetd-dms-greeter (AUR fallback: greetd-dms-greeter-git), then run dms-greeter install interactively.")
	fmt.Fprintln(out, "Choose Niri when prompted. Upstream setup configures greetd, permissions, theme/authentication, graphical target and display-manager enablement.")
	fmt.Fprintln(out, "Review its prompts; system files and upstream backups are separate from this tool's user-file backups. Run dms-greeter status afterwards.")
}

func PreflightGreeter(r Runner) error {
	if _, err := r.Output("dms-greeter", "install", "--help"); err != nil {
		return fmt.Errorf("greeter installer unavailable; install greetd-dms-greeter-git or a compatible distribution package: %w", err)
	}
	return nil
}

func SetupGreeter(r Runner, out io.Writer) error {
	PreviewGreeter(out)
	if err := r.Run("dms-greeter", "install"); err != nil {
		return fmt.Errorf("upstream greeter setup: %w; inspect dms-greeter status before retrying", err)
	}
	fmt.Fprintln(out, "Greeter installer returned. Review its result (including any skipped steps) and run dms-greeter status before rebooting.")
	return nil
}

func CheckGreeter(r Runner) error {
	if _, err := r.Output("systemctl", "is-enabled", "greetd.service"); err != nil {
		return fmt.Errorf("greetd is not enabled: %w", err)
	}
	if _, err := r.Output("dms-greeter", "status"); err != nil {
		return fmt.Errorf("greeter status: %w", err)
	}
	return nil
}
