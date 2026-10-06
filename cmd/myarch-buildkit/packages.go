package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

type Tool struct {
	Label                                   string
	Commands, RepoCandidates, AURCandidates []string
	Group                                   string
}

// Explicit package identities only. No search result is installed automatically.
const packageRows = `DMS Docker Manager backend|podman|podman|-
DMS Kubernetes backend|kubectl|kubectl|-
Bongo Cat keyboard events|evtest|evtest|-
Bongo Cat input tools|libinput|libinput-tools|-
Ghostty|ghostty|ghostty|-
GNOME Files|nautilus|nautilus|-
GNOME Files SMB support|-|gvfs-smb|-
GNOME Files Open in Ghostty|-|ghostty-nautilus|-
Microsoft VS Code|code|visual-studio-code-bin|visual-studio-code-bin
Bitwarden Desktop|bitwarden-desktop|bitwarden|-
Bitwarden CLI|bw|bitwarden-cli|-
Obsidian|obsidian|obsidian|-
Firefox PDF viewer|firefox|firefox|-
VLC media player|vlc|vlc|-
VLC format support|-|vlc-plugins-all|-
PeaZip archive manager|peazip|peazip peazip-qt-bin|peazip-qt-bin peazip
GNOME Camera (Snapshot)|snapshot|snapshot|-
Kooha screen recorder|kooha|kooha|-
Kooha H.264 encoding|-|gst-plugins-ugly|-
LocalSend|localsend|localsend localsend-bin|localsend-bin localsend
LocalSend compatibility library|-|libayatana-indicator|-
JetBrains Mono Nerd Font Mono|-|ttf-jetbrains-mono-nerd|-
Git|git|git|-
GitHub CLI|gh|github-cli|-
Git Delta|delta|git-delta|-
Lazygit|lazygit|lazygit|-
pre-commit|pre-commit|pre-commit|-
direnv|direnv|direnv|-
Make|make|make|-
GNU Coreutils|ls cp mv cat sort|coreutils|-
wget|wget|wget|-
jq|jq|jq|-
yq (Mike Farah)|yq|go-yq|-
fzf|fzf|fzf|-
ripgrep|rg|ripgrep|-
fd|fd|fd|-
bat|bat|bat|-
eza|eza|eza|-
zoxide|zoxide|zoxide|-
tmux|tmux|tmux|-
Starship|starship|starship|-
mise|mise|mise|-
tree|tree|tree|-
watch|watch|procps-ng|-
kubectl|kubectl|kubectl|-
kubectx + kubens|kubectx kubens|kubectx|-
K9s|k9s|k9s|-
Helm|helm|helm|-
Helmfile|helmfile|helmfile|-
Kustomize|kustomize|kustomize|-
kind|kind|kind|-
Stern|stern|stern|-
Kubecolor|kubecolor|kubecolor|kubecolor
Kubeconform|kubeconform|kubeconform|-
Terraform|terraform|terraform|-
Terragrunt|terragrunt|terragrunt|-
TFLint|tflint|tflint|-
Terraform Docs|terraform-docs|terraform-docs|terraform-docs
Infracost|infracost|infracost|infracost
Packer|packer|packer|-
Ansible|ansible ansible-playbook|ansible|-
Trivy|trivy|trivy|-
Gitleaks|gitleaks|gitleaks|-
HTTPie|http https|httpie|-
grpcurl|grpcurl|grpcurl grpcurl-bin|grpcurl-bin grpcurl
websocat|websocat|websocat|-
MTR|mtr|mtr|-
Nmap|nmap|nmap|-
DNS diagnostics|dig|bind|-
Network throughput testing|iperf3|iperf3|-
Packet capture|tcpdump|tcpdump|-
Ethernet diagnostics|ethtool|ethtool|-
Wireshark packet analyzer|wireshark|wireshark-qt|-
Podman|podman|podman|-
Podman Compose|podman-compose|podman-compose|-
Podman rootless networking|pasta|passt|-
Podman subordinate-ID helpers|newuidmap newgidmap getsubids|shadow|-
k6|k6|k6 k6-bin|k6-bin k6
uv|uv uvx|uv|-
Go (includes go install)|go gofmt|go|-
golangci-lint|golangci-lint|golangci-lint|-
gopls|gopls|gopls|-
Delve|dlv|delve|-
GoReleaser|goreleaser|goreleaser|-
Neovim|nvim|neovim|-
Yazi|yazi|yazi|-
Midnight Commander|mc|mc|-
XDG terminal selection|xdg-terminal-exec|xdg-terminal-exec|-
XDG MIME tools|xdg-mime xdg-open|xdg-utils|-
Desktop entry tools|update-desktop-database desktop-file-validate|desktop-file-utils|-
Font configuration|fc-cache fc-match|fontconfig|-
AUR build dependencies|-|base-devel|-
DankMaterialShell|dms|dms-shell|-
Quickshell|qs|quickshell|-
DankGreeter|dms-greeter|greetd-dms-greeter-bin|greetd-dms-greeter-bin
greetd login manager|greetd|greetd|-
Region screenshot capture|grim|grim|-
Region screenshot selection|slurp|slurp|-
Wayland clipboard tools|wl-copy wl-paste|wl-clipboard|-
Desktop notification tools|notify-send|libnotify|-
Hyprland desktop portal|-|xdg-desktop-portal-hyprland|-
GTK file-picker portal|-|xdg-desktop-portal-gtk|-`

func PackageManifest() []Tool {
	result := []Tool{}
	for _, row := range strings.Split(packageRows, "\n") {
		fields := strings.Split(row, "|")
		group := "development"
		switch fields[0] {
		case "AUR build dependencies", "Bitwarden CLI", "Bitwarden Desktop", "Bongo Cat input tools", "Bongo Cat keyboard events", "DMS Docker Manager backend", "DMS Kubernetes backend", "DankGreeter", "DankMaterialShell", "Desktop entry tools", "Desktop notification tools", "Firefox PDF viewer", "Font configuration", "GNOME Files", "GNOME Files Open in Ghostty", "GNOME Files SMB support", "GTK file-picker portal", "Ghostty", "Git", "Hyprland desktop portal", "JetBrains Mono Nerd Font Mono", "Microsoft VS Code", "Obsidian", "Quickshell", "Region screenshot capture", "Region screenshot selection", "Starship", "VLC format support", "VLC media player", "Wayland clipboard tools", "XDG MIME tools", "XDG terminal selection", "direnv", "fzf", "greetd login manager", "mise", "zoxide":
			group = "core"
		case "GNOME Camera (Snapshot)", "Kooha H.264 encoding", "Kooha screen recorder", "LocalSend", "LocalSend compatibility library", "PeaZip archive manager":
			group = "utilities"
		case "Podman", "Podman Compose", "Podman rootless networking", "Podman subordinate-ID helpers":
			group = "containers"
		}
		split := func(s string) []string {
			if s == "-" {
				return nil
			}
			return strings.Fields(s)
		}
		result = append(result, Tool{Label: fields[0], Commands: split(fields[1]), RepoCandidates: split(fields[2]), AURCandidates: split(fields[3]), Group: group})
	}
	return result
}

func selectedTools(c *Context, group string) []Tool {
	result := []Tool{}
	for _, tool := range PackageManifest() {
		enabled := true
		if flag, ok := c.Settings.PackageGroups[tool.Group]; ok {
			enabled = flag
		}
		if enabled && (group == "" || group == tool.Group) {
			result = append(result, tool)
		}
	}
	return result
}

type packageSelection struct {
	Label   string `json:"label"`
	Source  string `json:"source"`
	Package string `json:"package"`
}
type packageResolver struct {
	context              *Context
	installed, repo, aur map[string]bool
	helper               string
}

func newPackageResolver(c *Context) *packageResolver {
	return &packageResolver{context: c, installed: map[string]bool{}, repo: map[string]bool{}, aur: map[string]bool{}}
}
func (p *packageResolver) query(args ...string) (CommandResult, error) {
	return p.context.Command(Command{Args: args, Env: map[string]string{"LC_ALL": "C"}})
}
func (p *packageResolver) isInstalled(name string) bool {
	if found, ok := p.installed[name]; ok {
		return found
	}
	_, err := p.query("pacman", "-Q", "--", name)
	p.installed[name] = err == nil
	return err == nil
}
func exactPackageInfo(info, name string) bool {
	for _, line := range strings.Split(info, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "Name" && fields[1] == ":" && fields[2] == name {
			return true
		}
	}
	return false
}
func (p *packageResolver) repoHas(name string) bool {
	if found, ok := p.repo[name]; ok {
		return found
	}
	result, err := p.query("pacman", "-Si", "--color", "never", "--", name)
	found := err == nil && exactPackageInfo(result.Stdout, name)
	p.repo[name] = found
	return found
}
func (p *packageResolver) aurHas(name string) bool {
	if found, ok := p.aur[name]; ok {
		return found
	}
	result, err := p.query(p.helper, "-Si", "--aur", "--color", "never", "--", name)
	found := err == nil && exactPackageInfo(result.Stdout, name)
	p.aur[name] = found
	return found
}
func (p *packageResolver) findHelper() error {
	for _, helper := range []string{"paru", "yay"} {
		if _, err := p.query(helper, "--version"); err == nil {
			p.helper = helper
			return nil
		}
	}
	for _, candidate := range []string{"paru", "paru-bin"} {
		if !p.repoHas(candidate) {
			continue
		}
		p.context.Note("Installing or repairing repository AUR helper: " + candidate)
		// No --needed here: existing current-version helper files may need repair.
		if _, err := p.context.Command(Command{Args: []string{"sudo", "pacman", "-S", "--", candidate}, Interactive: true}); err != nil {
			return fmt.Errorf("AUR-helper repair was cancelled or failed; no AUR operation attempted: %w", err)
		}
		if _, err := p.query("paru", "--version"); err == nil {
			p.helper = "paru"
			return nil
		}
		return errors.New("repository paru was installed but cannot run; repair its loader or PATH before retrying")
	}
	return errors.New("no working paru/yay or repository paru; restore the CachyOS repository or repair an AUR helper")
}
func (p *packageResolver) resolve(tools []Tool) ([]packageSelection, error) {
	result := []packageSelection{}
	missing := []string{}
	for _, tool := range tools {
		chosen := packageSelection{Label: tool.Label}
		candidates := append(append([]string{}, tool.RepoCandidates...), tool.AURCandidates...)
		for _, candidate := range candidates {
			if !p.isInstalled(candidate) {
				continue
			}
			chosen.Package = candidate
			chosen.Source = "installed"
			if p.repoHas(candidate) {
				chosen.Source = "repo"
			} else if containsPackage(tool.AURCandidates, candidate) {
				if p.helper == "" {
					if err := p.findHelper(); err != nil {
						p.context.Warn("Retaining installed " + candidate + ": " + err.Error())
						break
					}
				}
				if p.aurHas(candidate) {
					chosen.Source = "aur"
				}
			}
			if chosen.Source == "installed" {
				p.context.Warn("Retaining installed " + candidate + " for " + tool.Label + "; its exact update source was not verified. Updates remain pending.")
			}
			break
		}
		if chosen.Package == "" {
			for _, candidate := range tool.RepoCandidates {
				if p.repoHas(candidate) {
					chosen.Package = candidate
					chosen.Source = "repo"
					break
				}
			}
		}
		if chosen.Package == "" && len(tool.AURCandidates) > 0 {
			if p.helper == "" {
				if err := p.findHelper(); err != nil {
					return result, err
				}
			}
			for _, candidate := range tool.AURCandidates {
				if p.aurHas(candidate) {
					chosen.Package = candidate
					chosen.Source = "aur"
					break
				}
			}
		}
		if chosen.Package == "" {
			missing = append(missing, fmt.Sprintf("%s (repository: %s; AUR: %s)", tool.Label, strings.Join(tool.RepoCandidates, " "), strings.Join(tool.AURCandidates, " ")))
			continue
		}
		result = append(result, chosen)
	}
	if len(missing) > 0 {
		return result, fmt.Errorf("unavailable exact package candidates:\n%s\nDefaults and removals have not run", strings.Join(missing, "\n"))
	}
	return result, nil
}
func containsPackage(list []string, item string) bool {
	for _, value := range list {
		if value == item {
			return true
		}
	}
	return false
}

func InstallPackages(c *Context, group string, bootstrapped bool) error {
	if c.CheckOnly || c.Options.Check || c.Options.DryRun {
		return errors.New("package installation is unavailable in read-only check mode")
	}
	if group != "" && !containsPackage([]string{"core", "utilities", "development", "containers"}, group) {
		return fmt.Errorf("unknown package group: %s", group)
	}
	tools := selectedTools(c, group)
	if len(tools) == 0 {
		return errors.New("selected package manifest is empty or disabled")
	}
	// Refresh and upgrade once, before resolving packages against the new databases.
	if !bootstrapped {
		if _, err := c.Command(Command{Args: []string{"sudo", "pacman", "-Syu", "--needed", "--", "base-devel", "git"}, Interactive: true}); err != nil {
			return err
		}
	}
	resolver := newPackageResolver(c)
	selected, err := resolver.resolve(tools)
	c.Report["resolved_packages"] = selected
	if err != nil {
		return err
	}
	repo, aur, all := []string{}, []string{}, []string{}
	seen := map[string]bool{}
	for _, item := range selected {
		if seen[item.Package] {
			continue
		}
		seen[item.Package] = true
		all = append(all, item.Package)
		if item.Source == "repo" {
			repo = append(repo, item.Package)
		} else if item.Source == "aur" {
			aur = append(aur, item.Package)
		}
	}
	if seen["go-yq"] && resolver.isInstalled("yq") && !resolver.isInstalled("go-yq") {
		return errors.New("Python yq is installed, but Mike Farah go-yq owns the same path. Review/remove yq with normal pacman dependency checks, then retry; no files are force-overwritten")
	}
	if c.RunDir != "" {
		if err := c.Save(); err != nil {
			return err
		}
	}
	if len(repo) > 0 {
		args := append([]string{"sudo", "pacman", "-Su", "--needed", "--"}, repo...)
		if _, err := c.Command(Command{Args: args, Interactive: true}); err != nil {
			return err
		}
	}
	if len(aur) > 0 {
		c.Note("Review the AUR recipes and any replacement prompts.")
		args := append([]string{resolver.helper, "-S", "--aur", "--needed", "--"}, aur...)
		if _, err := c.Command(Command{Args: args, Interactive: true}); err != nil {
			return err
		}
	}
	versions, err := c.Run(append([]string{"pacman", "-Q", "--"}, all...)...)
	if err != nil {
		return err
	}
	c.Report["installed_tool_versions"] = versions.Stdout
	if c.RunDir != "" {
		return c.Save()
	}
	return nil
}

func CheckTooling(c *Context) error {
	resolver := newPackageResolver(c)
	failures := []string{}
	versionChecked := map[string]bool{}
	for _, tool := range selectedTools(c, c.Options.PackageGroup) {
		selected := ""
		for _, candidate := range append(append([]string{}, tool.RepoCandidates...), tool.AURCandidates...) {
			if resolver.isInstalled(candidate) {
				selected = candidate
				break
			}
		}
		if selected == "" {
			failures = append(failures, "MISSING PACKAGE: "+tool.Label)
		}
		for _, command := range tool.Commands {
			path, err := exec.LookPath(command)
			if err != nil {
				failures = append(failures, "MISSING COMMAND: "+command+" ("+tool.Label+")")
				continue
			}
			if !strings.HasPrefix(path, "/usr/bin/") && !strings.HasPrefix(path, "/bin/") {
				c.Warn(command + " resolves to " + path + "; a user installation may shadow the packaged version")
			}
			if command == "code" || command == "yq" {
				resolved, err := filepath.EvalSymlinks(path)
				if err != nil {
					failures = append(failures, "cannot resolve active "+command+": "+err.Error())
					continue
				}
				owner := c.Try("pacman", "-Qqo", "--", resolved)
				if owner.Code != 0 || strings.TrimSpace(owner.Stdout) != selected {
					failures = append(failures, fmt.Sprintf("WRONG ACTIVE COMMAND: %s is %s (owner: %s; expected: %s)", command, path, strings.TrimSpace(owner.Stdout), selected))
				}
			}
			if (command == "podman" || command == "podman-compose") && !versionChecked[command] {
				versionChecked[command] = true
				flag := "--version"
				if command == "podman-compose" {
					flag = "version"
				}
				if _, err := c.Run(command, flag); err != nil {
					failures = append(failures, "FAILED COMMAND: "+command+" "+flag)
				}
			}
		}
	}
	c.Report["tooling_checks"] = map[string]any{"failures": failures, "success": len(failures) == 0}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

func osReleaseID(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "ID=")), "\"'")
		}
	}
	return ""
}
func inputIsTerminal() bool {
	var info syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&info)))
	return errno == 0
}
func CheckPlatform(c *Context, forApply bool) error {
	if os.Geteuid() == 0 {
		return errors.New("run as your regular desktop user without sudo in front of the installer")
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return err
	}
	if id := osReleaseID(data); id != "cachyos" {
		return fmt.Errorf("native CachyOS is required; detected ID=%s", id)
	}
	if runtime.GOARCH != "amd64" || runtime.GOOS != "linux" {
		return errors.New("this manifest targets CachyOS x86_64 Linux")
	}
	release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	if os.Getenv("WSL_INTEROP") != "" || os.Getenv("WSL_DISTRO_NAME") != "" || strings.Contains(strings.ToLower(string(release)), "microsoft") {
		return errors.New("the native CachyOS Hyprland desktop is required; WSL is unsupported")
	}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "ZDOTDIR"} {
		if value := os.Getenv(name); value != "" && !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be an absolute path", name)
		}
	}
	if !hasCommand("pacman") {
		return errors.New("pacman is required")
	}
	if !forApply {
		return nil
	}
	if !hasCommand("sudo") {
		return errors.New("sudo is required")
	}
	if !inputIsTerminal() {
		return errors.New("run interactively in a terminal so package/AUR prompts can be reviewed")
	}
	if c.Options.RestoreProfile != "" || c.Options.RestoreStage != "" {
		return nil
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" || !strings.Contains(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), "hyprland") {
		return errors.New("run from a terminal inside your Hyprland desktop session")
	}
	if !hasCommand("hyprctl") {
		return errors.New("hyprctl is required for the active desktop session")
	}
	return nil
}

// Kept with the package selector so CLI --remove-notes cannot target arbitrary packages.
var notesPackageName = regexp.MustCompile(`^(gnote|bijiben|knotes|xpad)$`)

func ValidNotesPackage(name string) bool { return notesPackageName.MatchString(name) }
