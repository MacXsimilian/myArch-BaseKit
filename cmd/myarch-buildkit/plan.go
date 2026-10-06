package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"
)

type planRequirement struct {
	Tool   Tool
	Groups []string
	Labels []string
	Roles  []Tool
}

type planPackageRow struct {
	Tool                         Tool
	Name, Pacman, AUR, Reference string
}

func planTableRows(o Options, s Settings, group string) []planPackageRow {
	rows := []planPackageRow{}
	for _, requirement := range planRequirements(o, s) {
		if !contains(requirement.Groups, group) {
			continue
		}
		tool := requirement.Tool
		for _, role := range requirement.Roles {
			if role.Group == group {
				tool = role
				break
			}
		}
		row := planPackageRow{Tool: tool, Name: planToolName(tool), Pacman: strings.Join(tool.RepoCandidates, ", "), AUR: strings.Join(tool.AURCandidates, ", ")}
		if requirement.Groups[0] != group {
			row.Reference = planGroupNames[requirement.Groups[0]]
			row.Pacman, row.AUR = "included under "+row.Reference, ""
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	return rows
}

// Shared dependencies occupy one row, retaining every role and group in details.
func planRequirements(o Options, s Settings) []planRequirement {
	rows := []planRequirement{}
	positions := map[string]int{}
	for _, group := range GroupOrder {
		for _, tool := range PackageManifest() {
			if tool.Group != group || !s.PackageGroups[group] || o.PackageGroup != "" && o.PackageGroup != group {
				continue
			}
			key := strings.Join(tool.RepoCandidates, "\x00") + "\x01" + strings.Join(tool.AURCandidates, "\x00")
			if index, ok := positions[key]; ok {
				row := &rows[index]
				if !contains(row.Groups, group) {
					row.Groups = append(row.Groups, group)
				}
				row.Labels = append(row.Labels, tool.Label)
				row.Roles = append(row.Roles, tool)
				continue
			}
			positions[key] = len(rows)
			rows = append(rows, planRequirement{Tool: tool, Groups: []string{group}, Labels: []string{tool.Label}, Roles: []Tool{tool}})
		}
	}
	return rows
}

type planView struct {
	strings.Builder
	color bool
}

func (v *planView) styled(text, code string) string {
	if v.color {
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	return text
}

func (v *planView) section(title string) {
	fmt.Fprintln(&v.Builder, "\n"+v.styled(title, "1;36"))
}

func (v *planView) field(label, value string) {
	const labelWidth = 18
	prefix := fmt.Sprintf("  %-*s", labelWidth, label)
	lines := planWrap(value, 76-utf8.RuneCountInString(prefix))
	for index, line := range lines {
		if index == 0 {
			fmt.Fprintln(&v.Builder, prefix+line)
		} else {
			fmt.Fprintln(&v.Builder, strings.Repeat(" ", utf8.RuneCountInString(prefix))+line)
		}
	}
}

func (v *planView) note(value string) {
	for _, line := range planWrap(value, 72) {
		fmt.Fprintln(&v.Builder, "  "+v.styled(line, "2"))
	}
}

func (v *planView) command(label, command string) {
	// Keep the shell command intact; inserted newlines could split quoted paths.
	fmt.Fprintf(&v.Builder, "  %-18s%s\n", label, command)
}

func planWrap(value string, width int) []string {
	lines := []string{}
	current := ""
	for _, word := range strings.Fields(value) {
		if current != "" && utf8.RuneCountInString(current)+1+utf8.RuneCountInString(word) > width {
			lines = append(lines, current)
			current = ""
		}
		if current != "" {
			current += " "
		}
		current += word
	}
	return append(lines, current)
}

var planGroupNames = map[string]string{"core": "Core", "utilities": "Utilities", "development": "Development", "containers": "Containers"}

func planText(o Options, s Settings, stages []string, color bool) string {
	return planTextWidth(o, s, stages, color, 100)
}

func planTextWidth(o Options, s Settings, stages []string, color bool, width int) string {
	v := &planView{color: color}
	fmt.Fprintln(&v.Builder, v.styled("myarch-buildkit", "1;36")+"  /  Setup preview")
	v.note(BuildID + " · No changes made")
	if o.SettingsPath != "" {
		v.field("Settings", o.SettingsPath)
	}
	selected := func(stage string) bool { return contains(stages, stage) }
	if selected("packages") {
		rows := planRequirements(o, s)
		counts := map[string]int{}
		for _, row := range rows {
			counts[row.Groups[0]]++
		}
		v.section("Packages")
		fmt.Fprintf(&v.Builder, "  %-15s%13s  %s\n", "Group", "Requirements", "Included tools")
		descriptions := map[string]string{"core": "Apps, DMS and shell tools", "utilities": "Archives, camera, recording and sharing", "development": "Git, Go, Kubernetes and infrastructure", "containers": "Podman Compose and networking"}
		for _, group := range GroupOrder {
			if counts[group] > 0 {
				count := fmt.Sprint(counts[group])
				shared := len(planTableRows(o, s, group)) - counts[group]
				if shared > 0 {
					count += fmt.Sprintf(" + %d shared", shared)
				}
				fmt.Fprintf(&v.Builder, "  %-15s%13s  %s\n", planGroupNames[group], count, descriptions[group])
			}
		}
		v.note(fmt.Sprintf("%d selected requirements; actual installs determined during apply.", len(rows)))
		v.note("Shared entries count once in the total.")
		v.note("Full system upgrade, then selected tools with review prompts.")
		v.note("Keep a supported installed variant; otherwise pacman first, then AUR.")
		v.note("Pacman follows your enabled repository order in /etc/pacman.conf.")
		v.note("Candidates are tried left to right; actual sources are unresolved.")
		if o.PackageGroup != "" {
			message := "Only the " + o.PackageGroup + " package group is selected."
			if len(stages) > 2 {
				message += " Other selected stages below still run."
			}
			v.note(message)
		}
	}
	if selected("defaults") || selected("shell") || selected("containers") || selected("preflight") {
		v.section("During setup")
		v.field("Preflight", "Settings and prerequisites for selected stages")
		if selected("defaults") {
			v.field("App defaults", "Ghostty, Files, VS Code, Obsidian, Firefox/VLC; fonts")
		}
		if selected("shell") {
			v.field("Shell", "Keep login shell; Starship, mise, zoxide, fzf and direnv")
		}
		if selected("containers") {
			v.field("Containers", "Podman Compose; static rootless checks")
			if o.EnablePodmanSocket {
				v.field("Podman API", "Enable your user socket")
			}
		}
	}
	if selected("laptop") || selected("desktop") {
		v.section("At your next myarch-buildkit login")
		if selected("desktop") {
			v.field("Desktop", "Hyprland + DMS; Frame Mode, top bar and bottom dock")
		}
		v.field("Internal display", fmt.Sprint(s.Laptop.InternalScale)+" scale")
		layout := s.Laptop.InternalKeyboard
		if layout == "hu" {
			layout = "Hungarian (hu)"
		}
		v.field("Internal keyboard", layout+"; verified built-in devices only")
		scroll := "Off"
		if s.Laptop.NaturalScroll {
			scroll = "On"
		}
		v.field("Natural scrolling", scroll+" for pointers and touchpads")
		if selected("desktop") {
			v.field("Plugins", "Docker Manager · Kubernetes · Emoji & Unicode Launcher · Bongo Cat · ClipBoard+")
		}
		v.note("Files are staged separately and applied before Hyprland and DMS start.")
		if !selected("desktop") {
			v.note("A first laptop setup also needs the desktop stage to complete the login profile.")
		}
	}
	if selected("defaults") || selected("desktop") {
		v.field("Theme setup", "None; existing personal themes are preserved")
	}
	if selected("greeter") {
		v.section("At the next boot")
		v.field("Login screen", "Password login with DankGreeter")
	}
	if selected("verify") || selected("cleanup") {
		v.section("Verification and cleanup")
		if selected("verify") {
			v.field("Verification", "Pending files and running desktop reported separately")
		}
		if selected("cleanup") {
			v.field("Cleanup", "Approved duplicates; hide launchers after checks pass")
			if selected("desktop") || selected("laptop") {
				v.note("Cleanup waits for login. Rerun verify/cleanup once checks pass.")
			}
			if o.KeepTerminal {
				v.field("Keep", "Alacritty")
			}
			if len(o.RemoveNotes) > 0 {
				v.field("Remove notes apps", strings.Join(o.RemoveNotes, ", "))
			}
		}
	}
	if o.Details && selected("packages") {
		v.section("Package options")
		v.note("Package availability and source choices are checked during apply.")
		for _, group := range GroupOrder {
			rows := planTableRows(o, s, group)
			if len(rows) == 0 {
				continue
			}
			fmt.Fprintln(&v.Builder, "\n  "+v.styled(planGroupNames[group], "1"))
			v.packageTable(rows, width)
			if group == "core" {
				v.note("Podman and kubectl also provide the Docker Manager and Kubernetes plugin backends.")
			}
		}
	}
	if o.Details && !selected("packages") {
		v.note("Package installation is not selected in this plan.")
	}
	v.section("Next steps")
	v.command("Apply", planCommand(o, "apply", false))
	if !o.Details && selected("packages") {
		v.command("Package details", planCommand(o, "plan", true))
	}
	if selected("desktop") {
		v.note("Optional Bongo Cat typing: keyboard/input access, active after logout.")
		v.command("Typing animation", `sudo usermod -aG input "$USER"`)
	}
	if selected("desktop") || selected("laptop") {
		v.note("After setup: save work, log out and select myarch-buildkit.")
		v.command("After login", planFollowupCommand(o, false))
		if selected("cleanup") {
			v.note("After the check passes:")
			v.command("Cleanup", planFollowupCommand(o, true))
		}
	}
	if selected("greeter") {
		v.note("Save work and reboot after successful setup to activate DankGreeter.")
	}
	return v.String()
}

func (v *planView) packageTable(rows []planPackageRow, width int) {
	headings := [3]string{"Tool", "Repository packages", "AUR fallback"}
	cells := [][3]string{}
	widths := [3]int{}
	minimum := [3]int{12, 17, 12}
	for _, row := range rows {
		aur := row.AUR
		if aur == "" {
			aur = "—"
		}
		cells = append(cells, [3]string{row.Name, row.Pacman, aur})
	}
	for _, cell := range append([][3]string{headings}, cells...) {
		for column, value := range cell {
			widths[column] = max(widths[column], utf8.RuneCountInString(value))
			for _, word := range strings.Fields(value) {
				minimum[column] = max(minimum[column], utf8.RuneCountInString(word))
			}
		}
	}
	// Wrap cells only between words; exact package identifiers stay intact.
	for widths[0]+widths[1]+widths[2]+6 > width {
		column, room := -1, 0
		for i := range widths {
			if extra := widths[i] - minimum[i]; extra > room {
				column, room = i, extra
			}
		}
		if column < 0 {
			break
		}
		widths[column]--
	}
	printRow := func(cell [3]string, header bool) {
		lines := [3][]string{}
		height := 1
		for i, value := range cell {
			lines[i] = planWrap(value, widths[i])
			height = max(height, len(lines[i]))
		}
		for line := 0; line < height; line++ {
			parts := [3]string{}
			for i := range parts {
				if line < len(lines[i]) {
					parts[i] = lines[i][line]
				}
				if i < 2 {
					parts[i] += strings.Repeat(" ", widths[i]-utf8.RuneCountInString(parts[i]))
				}
			}
			text := "  " + parts[0] + "  " + parts[1] + "  " + parts[2]
			if header {
				text = v.styled(text, "1")
			}
			fmt.Fprintln(&v.Builder, strings.TrimRight(text, " "))
		}
	}
	printRow(headings, true)
	for _, cell := range cells {
		printRow(cell, false)
	}
}

func planFollowupCommand(o Options, cleanup bool) string {
	args := []string{"./myarch-buildkit", "check"}
	if cleanup {
		args = []string{"./myarch-buildkit", "apply", "--stage", "verify", "--stage", "cleanup"}
	}
	if o.SettingsPath != "" {
		args = append(args, "--settings", o.SettingsPath)
	}
	if cleanup {
		if o.KeepTerminal {
			args = append(args, "--keep-terminal")
		}
		for _, name := range o.RemoveNotes {
			args = append(args, "--remove-notes", name)
		}
	}
	return planShellWords(args)
}

func planCommand(o Options, command string, details bool) string {
	args := []string{"./myarch-buildkit", command}
	if o.All || len(o.Stages) == 0 && !o.ConfigureOnly {
		args = append(args, "--all")
	}
	for _, stage := range o.Stages {
		args = append(args, "--stage", stage)
	}
	for _, option := range [][2]string{{"--settings", o.SettingsPath}, {"--package-group", o.PackageGroup}, {"--laptop-scale", o.LaptopScale}} {
		if option[1] != "" {
			args = append(args, option[:]...)
		}
	}
	for _, option := range []struct {
		enabled bool
		flag    string
	}{{o.ConfigureOnly, "--configure-only"}, {o.KeepTerminal, "--keep-terminal"}, {o.EnablePodmanSocket, "--enable-podman-socket"}, {details, "--details"}} {
		if option.enabled {
			args = append(args, option.flag)
		}
	}
	for _, note := range o.RemoveNotes {
		args = append(args, "--remove-notes", note)
	}
	return planShellWords(args)
}

func planShellWords(args []string) string {
	for index, arg := range args {
		if strings.IndexFunc(arg, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._/:", r))
		}) >= 0 {
			args[index] = shellQuote(arg)
		}
	}
	return strings.Join(args, " ")
}

func planTerminalWidth() int {
	var size struct{ Rows, Cols, X, Y uint16 }
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&size)))
	if err == 0 && size.Cols > 0 {
		return int(size.Cols)
	}
	return 100
}

func planHasColor() bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" || os.Getenv("TERM") == "" {
		return false
	}
	var state syscall.Termios
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&state)))
	return err == 0
}

func PrintPlan(o Options, s Settings) error {
	stages, err := selectedStages(o, s)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, planTextWidth(o, s, stages, planHasColor(), planTerminalWidth()))
	return err
}
