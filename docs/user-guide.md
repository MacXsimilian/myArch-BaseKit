# Setup and operation

[Home](../README.md) · [Packages](packages.md) · [Troubleshooting](troubleshooting.md) · [Development](../CONTRIBUTING.md)

## Before you start

Use an installed CachyOS Linux x86-64 system and a regular user account with sudo access. `apply` refuses root and other operating systems. Have a working network connection and `paru` or `yay` available if selected packages need AUR installation. The tool does not bootstrap an AUR helper.

CachyOS's Niri preset can be your starting desktop; you do not need an installer option named Dux. This project supplies the Niri/Dux configuration afterward. Starting without a desktop is also possible, but you must already have networking, sudo and the package-installation prerequisites working from a console.

Build with `make build` using the Go version in [`.go-version`](../.go-version). A packaged executable needs no Go compiler on the target. Examples below use `./bin/myarch-buildkit`; substitute the path to your extracted executable when using an archive.

If migrating from an older BaseKit with pending-login stages, finish or restore that work with the matching old executable first. The new tool accepts a completed empty queue but refuses outstanding or unrecognized pending state. Old reports are not new settings files.

## Prepare your settings

Create a new file, then edit it:

```bash
./bin/myarch-buildkit --settings-template > workstation.json
```

| Setting | Default | Meaning |
| --- | --- | --- |
| `setup_greeter` | `true` | Run the upstream interactive Dux greeter installer |
| `dms_config_dir` | `"dms"` | Dux 2; use `"DankMaterialShell"` for DMS 1 |
| `groups` | `["core", "utilities", "development", "containers"]` | Package groups to install/check |
| `keyboard` | `"hu"` | Global keyboard layout |
| `scale` | `"4/3"` | Display scale, as a number string or ratio; range 0.25–8 |
| `monitor` | `"eDP-1"` | Output connector receiving the configured scale |
| `config_path` | omitted | Absolute `.kdl` file inside your home; defaults to `~/.config/niri/config.kdl` |

Record your display connector before logging out: `niri msg outputs` in Niri, or `hyprctl monitors` when preparing from Hyprland. The keyboard layout applies to all keyboards; there is no device discovery or separate per-device layout setting.

Missing settings retain defaults. In particular, omitting `setup_greeter` enables it. Unknown fields and null values are rejected. Keep the same settings file for plan, apply and check.

The profile uses `~/.config`; another `XDG_CONFIG_HOME` is rejected. If `NIRI_CONFIG` is set, it must agree with the selected config path. Dux session state respects an absolute `XDG_STATE_HOME` inside your home. BaseKit's own backups always use `~/.local/state/myarch-buildkit`.

Package groups control package selection, not every configuration feature. Desktop configuration and the five plugins are still managed if you omit `core`; the required desktop programs must then already be installed. Container configuration is generated when `containers` is selected; kind's provider is configured when both `containers` and `development` are selected.

## Preview, apply and check

```bash
./bin/myarch-buildkit plan --settings workstation.json
```

`plan` reads local configuration and shows package targets, plugin activation, files and greeter setup without changing them or accessing the network. With `--cleanup-apps`, it also queries the local package database. It does not resolve repository availability or prove that apply will succeed. No command defaults to `plan`.

Save work and log out of graphical sessions. Merely switching to a text console while leaving Niri/DMS running is insufficient. Sign into the console as the same regular user:

```bash
./bin/myarch-buildkit apply --settings workstation.json
```

Apply performs these operations in order:

1. Checks the platform, pending state, running desktop processes and setup lock; reports competing startup entries.
2. Validates the candidate Niri configuration if Niri is already available, then upgrades/install packages unless skipped.
3. Checks required programs and the installed DMS major version, rebuilds the proposed configuration, and validates it with Niri.
4. Installs missing plugins through `dms plugins install`; keeps existing plugin versions.
5. Backs up changed user files, then writes configuration including plugin activation.
6. Configures NetworkManager/Bluetooth for the core group, runs greeter setup if enabled, then runs requested cleanup.

For a first Niri installation, validation occurs after installing the validator and before writing configuration. A failure can leave earlier steps completed. The entire operation is not one atomic transaction.

After login, open a new terminal and run:

```bash
./bin/myarch-buildkit check --settings workstation.json
```

`check` checks packages, plugin manifests, managed file contents, terminal selection, selected container integration, relevant services and competing startup entries. When greeter setup is enabled, it also checks greetd enablement and upstream greeter status. It does not repair anything or prove that the graphical desktop, plugins and hardware behave correctly.

Test the bar, launcher, Ghostty, calendar, network controls, audio, lock/unlock, suspend and display hotplug in your session. `dms doctor` provides upstream feature diagnostics.

### Command options

| Option | Commands | Effect |
| --- | --- | --- |
| `--settings FILE` | plan, apply, check | Load settings over defaults |
| `--skip-packages` | apply | Skip OS packages; still installs missing plugins, writes configuration and runs enabled service/greeter steps |
| `--cleanup-apps` | plan, apply | Preview/remove eligible alternatives |
| `--setup-greeter=true` or `--setup-greeter=false` | plan, apply | Override the setting for that invocation |
| `--settings-template` | standalone | Print default JSON |
| `--version`, `--help` | standalone | Print version or usage |

A one-run greeter override is not saved. Set `setup_greeter` in your settings file if you want later checks to follow the same choice. `--skip-packages` requires all relevant software, including the greeter when enabled, to be installed already.

## Configuration ownership

| Files | What happens |
| --- | --- |
| Selected Niri `.kdl` file | Replaced completely with the managed profile; existing content backed up |
| Dux `settings.json` | Managed desktop preferences merged; plugin widgets added without replacing unrelated bar entries |
| Dux `plugin_settings.json` | Five plugins enabled; unrelated plugin preferences preserved |
| Dux state `session.json` | Ghostty terminal override merged; other state preserved |
| `~/.config/xdg-terminals.list`, `niri-xdg-terminals.list` | Ghostty placed first; other terminal entries retained |
| `~/.config/environment.d/90-myarch-buildkit.conf` | Managed terminal/editor variables |
| Existing `~/.bashrc`, `~/.zshrc` | One source line added for a managed script in `~/.config/myarch-buildkit/` |
| Container drop-in and environment file | Managed Compose provider and optional kind provider |

Dux files live in `~/.config/dms` or `~/.config/DankMaterialShell`, according to settings. Managed user paths must be beneath your home; symlinked configuration paths are refused rather than followed. Plugin repository symlinks are handled separately by the plugin installer checks.

Rerunning apply reapplies managed choices. For example, manual changes to the generated Niri configuration or managed weather preference are overwritten. Put lasting changes in the profile templates/source before rebuilding. Unrelated merged preferences are retained. Equal file contents are not rewritten or backed up again, but packages, services and the greeter can still be revisited on reruns.

## Desktop features and plugins

Ghostty is preferred by Niri bindings, Dux, xdg-terminal-exec, user services and managed interactive Bash/Zsh scripts. Editor variables use `code --wait`. Apps with independent terminal settings can still need their own preference. The shell scripts also initialize mise, zoxide, fzf, Starship and direnv when available. Existing shell hooks are not removed; review duplicate initialization. Other shells are not configured.

| Feature | Included behavior | Your remaining setup |
| --- | --- | --- |
| Calendar | Dux Calendar package and built-in clock/calendar access on a new bar | Connect calendar accounts in `dcal` |
| VPN | NetworkManager backend for Dux controls | Import VPN profiles; install protocol-specific plugins when needed |
| Bluetooth | BlueZ packages and service setup | Pair devices |
| Media | PipeWire/WirePlumber, Qt multimedia and Cava dependencies | Select devices and test playback |
| Search and monitoring | dsearch and dgop packages | Check search/indexing preferences and runtime behavior |
| Themes | Matugen, image formats and GTK theme dependency | Choose wallpaper and appearance |
| Weather | Shipped in the shell, disabled by this profile | Change managed defaults if you want it enabled persistently |
| Power profiles | Existing compatible power manager can serve Dux | Configure a provider if absent; BaseKit does not replace it |

NetworkManager is started only after checking for another active network manager when NM is inactive. A conflicting stack requires manual migration; setup does not stop it. Existing NetworkManager with an iwd backend is retained. Bluetooth readiness is required by check only when an adapter is detected.

The launcher, notifications, dock, clipboard, lock screen and clock are shell components rather than separate third-party plugins. Dux Files is a separate application; this profile keeps Nautilus. Fingerprint and external-monitor brightness permissions are not provisioned.

| Plugin | Registry ID | Managed preferences |
| --- | --- | --- |
| Docker Manager | `dockerManager` | Enabled; Podman backend, Ghostty terminal |
| Kubernetes | `kubernetes` | Enabled; uses your existing kubeconfig |
| Emoji & Unicode Launcher | `emojiLauncher` | Enabled launcher extension |
| Bongo Cat | `bongoCat` | Enabled bar widget; input permissions unchanged |
| ClipBoard+ | `clipboardPlus` | Enabled; built-in Dux clipboard backend |

The four bar widgets are added only when missing from eligible visible bars. Existing placements/options are preserved and disabled matching widget entries are enabled. A new bar includes clock/calendar access and the control center. Existing custom bars are not forced to gain those built-ins. If no visible, enabled regular bar exists, setup stops with an explanation.

Bongo Cat typing animation needs access to input events. If you choose to grant access through the input group, run this yourself and log out/back in:

```bash
sudo usermod -aG input "$USER"
```

That group exposes keyboard and other input-device events. Without permission, leave typing animation off or follow the installed plugin's permission instructions. Plugin activation alone does not grant access. Use Dux's plugin management to update existing plugins later.

## Containers

Selecting `containers` writes `~/.config/containers/containers.conf.d/90-myarch-buildkit.conf` with `compose_providers = ["podman-compose"]`. Personal `containers.conf` and storage settings are preserved.

With `development` also selected, `KIND_EXPERIMENTAL_PROVIDER=podman` is written to managed shells and `~/.config/environment.d/60-myarch-buildkit-containers.conf`. Log in again and open a new terminal before checking it.

Checks require rootless Podman with at least 65,536 mapped UIDs and GIDs, working Podman Compose, and, when selected, kind with cgroup v2 and the Podman provider. Setup does not allocate subordinate IDs, enable a Docker API socket, create containers/clusters or supply credentials.

## Greeter

Greeter setup is enabled by default. Apply installs the selected greeter package and runs `dms-greeter install` interactively after writing user configuration. Choose Niri when offered. Upstream owns its privileged greetd files, permissions, theme/authentication sync and display-manager enablement; BaseKit does not maintain a second greeter implementation.

The installer can prompt or skip work. A successful exit alone does not prove that a login screen works. Review its output and run `dms-greeter status` before rebooting. Set `setup_greeter: false` to retain your existing login manager. Greeter system backups are separate from BaseKit's user-file backups.

## Migrating from Noctalia and cleaning up applications

First apply without cleanup. Review the competing-startup report and disable unwanted entries deliberately before entering the new desktop. The generated Niri file starts DMS, replacing autostarts inside the old selected file; independent autostart files and enabled services are reported, not disabled automatically.

Verify Dux and its greeter on the machine. Then preview cleanup, log out again, and apply:

```bash
./bin/myarch-buildkit plan --settings workstation.json --cleanup-apps
./bin/myarch-buildkit apply --settings workstation.json --cleanup-apps
```

| Candidates | Required installed replacement |
| --- | --- |
| Alacritty | Ghostty |
| Dolphin | Nautilus |
| gnome-text-editor, gedit, mousepad, xed, kate, kwrite, leafpad, pluma | `code` or `visual-studio-code-bin` |
| cachyos-niri-noctalia, noctalia, noctalia-git, noctalia-shell, noctalia-shell-git, noctalia-greeter, noctalia-greeter-git | DMS shell package, Niri and Quickshell, plus greeter checks below |

Noctalia removal also requires a working DMS CLI, enabled greetd, successful upstream greeter status and a readable `/etc/greetd/config.toml` directly selecting `dms-greeter` without Noctalia references. A different login manager or custom wrapper fails this check even if it works for you. Cleanup does not automatically recognize that alternative.

The preview is based on currently installed replacements. Apply recomputes eligibility after installation, so its list can grow. It rechecks replacements before removal and uses interactive `pacman -R`, without recursive dependency removal or cascade. Other packages that depend on Noctalia cause refusal rather than automatic removal.

Before removing Noctalia, Niri and Quickshell are marked explicitly installed to protect them from later orphan cleanup. Those markings can remain if you decline removal. Notes apps, personal Noctalia files and independent startup entries are left alone. The program does not track whether you actually tested a graphical login; that acceptance step is yours.

## Backups and restoration

Changed user files are backed up under `~/.local/state/myarch-buildkit/backups/<id>/`. Apply prints the location. Keep the entire directory, including `manifest.json` and numbered `.original` files.

Each manifest entry records the destination `path`, whether it `existed`, the backup filename when applicable, and its original file mode. All originals are saved before the first user-file replacement. Individual writes use temporary files and rename; a later failure can still leave some destinations changed.

To restore:

1. Log out of graphical sessions and stop configuration writers.
2. Read the manifest and compare current files with their saved originals.
3. Preserve any newer personal edits elsewhere.
4. For entries with `existed: true`, copy the named backup to its recorded destination and restore the recorded permissions.
5. For entries with `existed: false`, remove the created file only after confirming it contains no later work.
6. Review the resulting Niri configuration before logging in.

There is no automatic restore command. Backups do not undo package upgrades/removals, downloaded plugins, service enablement or greeter system changes. Use the package manager for packages and upstream greeter recovery for its system changes. See [troubleshooting](troubleshooting.md) before retrying a partial failure.
