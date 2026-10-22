# Package reference

[Home](../README.md) · [Setup guide](user-guide.md) · [Troubleshooting](troubleshooting.md)

This reference describes the code's package targets, not a live repository inventory. The source of truth is [packages.txt](../cmd/myarch-buildkit/packages.txt), with shell selection in [catalog.go](../cmd/myarch-buildkit/catalog.go) and the greeter target in [greeter.go](../cmd/myarch-buildkit/greeter.go).

## Selection and installation

Defaults select `core`, `utilities`, `development` and `containers`, plus greeter setup: **126 distinct package targets**, before dependencies, and **five separately installed Dux plugins**. Podman and kubectl occur in more than one group but are deduplicated when selected together. Removing a group from settings does not uninstall its existing packages.

The Dux 2 profile (`dms_config_dir: "dms"`) selects `dms-shell-git`. DMS 1 (`"DankMaterialShell"`) instead selects `dms-shell` from repositories. This is an explicit code policy; BaseKit does not automatically choose a shell major from repository version numbers.

For each target, apply tries repository candidates from left to right using `pacman -Si`. If none resolves and AUR candidates exist, it chooses the **first AUR candidate**; it does not probe subsequent AUR candidates. The catalogue may list additional AUR names, but they are not a retry chain in the current implementation.

Apply runs a full `pacman -Syu --needed` transaction for repository packages, then installs AUR targets with an available `paru` or `yay`. Already-installed packages can still be upgraded. Transitive dependencies, the greeter's own setup dependencies and source-build dependencies make the actual installed package count machine-dependent.

Use `plan --settings workstation.json` to see your selection. It does not contact repositories. `--skip-packages` skips OS package transactions, not missing-plugin downloads or enabled configuration/service/greeter steps.

## Core

| Repository candidates, in order | First AUR fallback |
| --- | --- |
| `podman` | — |
| `kubectl` | — |
| `evtest` | — |
| `libinput-tools` | — |
| `fastfetch` | — |
| `ghostty` | — |
| `nautilus` | — |
| `gvfs-smb` | — |
| `ghostty-nautilus` | — |
| `visual-studio-code-bin` | `visual-studio-code-bin` |
| `bitwarden` | — |
| `bitwarden-cli` | — |
| `obsidian` | — |
| `firefox` | — |
| `vlc` | — |
| `vlc-plugins-all` | — |
| `ttf-jetbrains-mono-nerd` | — |
| `git` | — |
| `direnv` | — |
| `fzf` | — |
| `zoxide` | — |
| `starship` | — |
| `mise` | — |
| `xdg-terminal-exec` | — |
| `xdg-utils` | — |
| `desktop-file-utils` | — |
| `fontconfig` | — |
| `base-devel` | — |
| `dms-shell-git` | `dms-shell-git` |
| `quickshell` | — |
| `wl-clipboard` | — |
| `libnotify` | — |
| `niri` | — |
| `xwayland-satellite` | — |
| `xdg-desktop-portal-gnome` | — |
| `gnome-keyring` | — |
| `xdg-desktop-portal-gtk` | — |
| `wireplumber` | — |
| `brightnessctl` | — |
| `cliphist` | — |
| `networkmanager` | — |
| `bluez` | — |
| `bluez-utils` | — |
| `pipewire` | — |
| `pipewire-pulse` | — |
| `polkit` | — |
| `upower` | — |
| `accountsservice` | — |
| `qt6-multimedia` | — |
| `qt6-imageformats` | — |
| `kimageformats` | — |
| `cava` | — |
| `matugen` | — |
| `adw-gtk-theme` | — |
| `dgop` | — |
| `wtype` | — |
| `dsearch`, `dsearch-bin` | `dsearch-bin` |
| `dcal`, `dankcalendar`, `dankcalendar-bin` | `dankcalendar-bin` |

## Utilities

| Repository candidates, in order | First AUR fallback |
| --- | --- |
| `btop` | — |
| `dust` | — |
| `peazip`, `peazip-qt-bin` | `peazip-qt-bin` |
| `snapshot` | — |
| `kooha` | — |
| `gst-plugins-ugly` | — |
| `localsend`, `localsend-bin` | `localsend-bin` |
| `libayatana-indicator` | — |

## Development

| Repository candidates, in order | First AUR fallback |
| --- | --- |
| `github-cli` | — |
| `git-delta` | — |
| `lazygit` | — |
| `pre-commit` | — |
| `actionlint` | — |
| `shellcheck` | — |
| `make` | — |
| `coreutils` | — |
| `wget` | — |
| `jq` | — |
| `go-yq` | — |
| `ripgrep` | — |
| `fd` | — |
| `bat` | — |
| `eza` | — |
| `tmux` | — |
| `tree` | — |
| `procps-ng` | — |
| `kubectl` | — |
| `kubectx` | — |
| `k9s` | — |
| `helm` | — |
| `helmfile` | — |
| `kustomize` | — |
| `kind` | — |
| `stern` | — |
| `kubecolor` | `kubecolor` |
| `kubeconform` | — |
| `terraform` | — |
| `terragrunt` | — |
| `tflint` | — |
| `terraform-docs` | `terraform-docs` |
| `infracost` | `infracost` |
| `packer` | — |
| `ansible` | — |
| `trivy` | — |
| `gitleaks` | — |
| `httpie` | — |
| `grpcurl`, `grpcurl-bin` | `grpcurl-bin` |
| `websocat` | — |
| `mtr` | — |
| `nmap` | — |
| `bind` | — |
| `iperf3` | — |
| `tcpdump` | — |
| `ethtool` | — |
| `wireshark-qt` | — |
| `k6`, `k6-bin` | `k6-bin` |
| `uv` | — |
| `go` | — |
| `golangci-lint` | — |
| `gopls` | — |
| `delve` | — |
| `goreleaser` | — |
| `neovim` | — |
| `yazi` | — |
| `mc` | — |

## Containers

| Repository candidates, in order | First AUR fallback |
| --- | --- |
| `podman` | — |
| `podman-compose` | — |
| `passt` | — |
| `shadow` | — |

## Greeter

With `setup_greeter: true` (the default), repository candidates are `greetd-dms-greeter`, `greetd-dms-greeter-bin`, then `greetd-dms-greeter-git`. The AUR fallback is `greetd-dms-greeter-git`.

The upstream interactive installer handles greetd and its system configuration. It can install additional prerequisites. Set `setup_greeter: false` to keep your login manager; the standard Noctalia cleanup path still requires a verified Dux greeter handoff.

## Dux plugins

These are installed by the DMS CLI rather than pacman:

| Plugin | Registry ID |
| --- | --- |
| Docker Manager | `dockerManager` |
| Kubernetes | `kubernetes` |
| Emoji & Unicode Launcher | `emojiLauncher` |
| Bongo Cat | `bongoCat` |
| ClipBoard+ | `clipboardPlus` |

Apply installs missing plugins, preserves existing versions, and merges activation settings. It does not download every plugin in the Dux registry. Calendar, networking and the other built-in shell controls are covered in the [feature guide](user-guide.md#desktop-features-and-plugins).

## Optional removal

Installation never implies automatic removal of alternative applications. `--cleanup-apps` selects a separate, constrained allowlist after checking replacements. See [cleanup behavior and exact candidates](user-guide.md#migrating-from-noctalia-and-cleaning-up-applications). No recursive dependency removal is used.
