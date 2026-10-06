package main

// Display names identify the tool before its purpose. Package identities and
// installation roles remain in the package manifest and resolver.
var planToolNames = map[string]string{
	"AUR build dependencies":          "Base build tools",
	"Bongo Cat input tools":           "libinput (Bongo Cat)",
	"Bongo Cat keyboard events":       "evtest (Bongo Cat)",
	"Desktop entry tools":             "Desktop file utilities",
	"Desktop notification tools":      "libnotify (notifications)",
	"DMS Docker Manager backend":      "Podman",
	"DMS Kubernetes backend":          "kubectl",
	"DNS diagnostics":                 "bind (DNS tools)",
	"Ethernet diagnostics":            "ethtool (Ethernet tools)",
	"Firefox PDF viewer":              "Firefox",
	"Font configuration":              "Fontconfig",
	"GNOME Camera (Snapshot)":         "Snapshot (GNOME Camera)",
	"GNOME Files Open in Ghostty":     "Ghostty Files integration",
	"GNOME Files SMB support":         "GVfs SMB support",
	"Go (includes go install)":        "Go",
	"greetd login manager":            "greetd",
	"GTK file-picker portal":          "GTK portal",
	"Hyprland desktop portal":         "Hyprland portal",
	"JetBrains Mono Nerd Font Mono":   "JetBrains Mono Nerd Font",
	"Kooha H.264 encoding":            "GStreamer H.264 codecs",
	"Kooha screen recorder":           "Kooha",
	"LocalSend compatibility library": "Ayatana indicator library",
	"Microsoft VS Code":               "VS Code",
	"Network throughput testing":      "iperf3 (network testing)",
	"Packet capture":                  "tcpdump (packet capture)",
	"PeaZip archive manager":          "PeaZip",
	"Podman rootless networking":      "passt (rootless networking)",
	"Podman subordinate-ID helpers":   "shadow (UID helpers)",
	"Region screenshot capture":       "grim (screenshots)",
	"Region screenshot selection":     "slurp (region selection)",
	"VLC format support":              "VLC codecs",
	"VLC media player":                "VLC",
	"Wayland clipboard tools":         "wl-clipboard",
	"Wireshark packet analyzer":       "Wireshark",
	"XDG MIME tools":                  "XDG MIME utilities",
	"XDG terminal selection":          "XDG terminal launcher",
}

func planToolName(tool Tool) string {
	if name, ok := planToolNames[tool.Label]; ok {
		return name
	}
	return tool.Label
}
