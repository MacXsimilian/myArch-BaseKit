# myArch-BaseKit: User Guide

Practical setup and daily-use reference\
Documented build: **v0.3.0**\
Documentation date: **6 October 2026**

**Current required base: CachyOS on x86_64 with Hyprland already installed and running.** Regular Arch Linux, EndeavourOS and other distributions are not supported by this release. The installer currently checks for CachyOS explicitly.

The project is now named **myArch-BaseKit**. The executable is named `myarch-buildkit`, the login-session chooser displays **myarch-buildkit**, and configuration/report paths use the `myarch-buildkit` name. Use the commands and session label shown in this guide.

## 1. Purpose and scope

myArch-BaseKit is a minimal, opinionated workstation builder for development, DevOps, SRE, cloud and platform engineering. It turns the currently required CachyOS + Hyprland base into a consistent work environment, replacing a long personal setup checklist with one repeatable process: install the chosen tools, set sensible defaults, configure the desktop and three laptop preferences, verify the result, and keep reports for troubleshooting.

The goal is a dependable starting point for building and operating software and infrastructure. The included profile has opinionated defaults, such as Hungarian layout for the built-in keyboard and 4/3 scale for the internal display. Review those choices before sharing it with colleagues or using it on another laptop.

### What you get

| Area | Main result |
| --- | --- |
| Desktop | Hyprland with Dank Material Shell (DMS), Frame Mode, top bar and bottom dock, with application-default appearance |
| DMS plugins | Docker Manager, Kubernetes, Emoji & Unicode Launcher, Bongo Cat and ClipBoard+ |
| Everyday apps | Ghostty, GNOME Files, VS Code, Bitwarden, Obsidian, Firefox for PDFs, VLC for media |
| Utilities | PeaZip, GNOME Camera/Snapshot, Kooha, LocalSend and SMB browsing in Files |
| Development | Git tools, Go tools, uv, mise, modern command-line utilities, Kubernetes and infrastructure tools |
| Containers and networking | Podman/Compose, kind integration, and network inspection tools |
| Laptop | Natural scrolling, Hungarian layout for the built-in keyboard, and 4/3 built-in display scale |

Use `plan` to see the complete selected tool list. Additional dependency packages can also be installed.

### Before you start

- Use **native CachyOS on x86_64 with an existing, working Hyprland session**. This installer does not install the OS or set up a headless machine from scratch.
- Run it in an interactive terminal inside Hyprland, as your regular desktop user. Ghostty is suitable. Use a text TTY for recovery, not ordinary setup.
- Have an internet connection, a working CachyOS package configuration and sudo access. Package and AUR prompts require your review.
- Keep the executable in a writable working directory, such as Downloads. Reports are created in the directory you run it from.
- Save open work before logging out to apply the staged desktop. The installer backs up configuration, but those backups are not a complete system backup.

You need only the Linux x86_64 executable `myarch-buildkit` to run the installer. It is compiled Go and uses the Go standard library; you do not need Go or Python installed to run it. The source project is supplied separately for maintenance. Bash/Zsh/Fish configuration and small command wrappers remain shell files. Do not put `sudo` before the whole command.

<!-- pagebreak -->

## 2. First setup

### Inspect the installer's identity and plan

Save the executable as `myarch-buildkit`, then run:

```bash
cd ~/Downloads
chmod +x myarch-buildkit
./myarch-buildkit --version
./myarch-buildkit plan
```

`plan` shows a compact setup preview: package groups, changes during setup, changes at your next myarch-buildkit login, and next-boot greeter changes. Only selected stages appear. `plan --details` shows Package options tables with Tool, Repository packages and AUR fallback columns. Names lead with the recognizable tool and are sorted A–Z, ignoring capitalization. Podman and kubectl remain visible in Containers/Development as references to Core. The total counts unique selected requirements; actual installs are determined during apply. Neither mode checks live package availability or changes anything. `--dry-run` is an equivalent preview.

The output uses restrained color in a real terminal and plain text when redirected. Set `NO_COLOR=1` to disable color explicitly. The preview ends with an apply command that retains your settings and selected options.

For an explanation of every package and its purpose, see the [package reference](#9-package-reference).

Package sources follow this order: keep a supported installed variant when its update source is available; otherwise try the listed repository package options from left to right, then the listed AUR fallbacks. Pacman uses the repositories enabled in `/etc/pacman.conf` in their configured priority order. The Repository packages column lists package names; exact repository names and availability are determined during apply.

The preview ends with the apply command and relevant follow-up commands. Bongo typing permission is an explicit optional step granting input-event access after logout. Choose myarch-buildkit at login, run the displayed check command, then run the displayed verify/cleanup command after checks pass. Those commands retain only settings and options relevant to each action.

Before the first full run, review the defaults in section 4. They include a **Hungarian internal keyboard layout**, **4/3 internal scale**.

### Stage, then log in

```bash
./myarch-buildkit apply --all
```

The laptop and desktop stages now create separate pending files. The installer leaves the running DMS settings, session state and Hyprland configuration untouched. Application defaults, shell setup, packages, and next-boot system configuration can still change during this run.

**Exit status 3 means validated configuration is staged for login.** Look for Waiting for login in the terminal output, or `pending_login` in the run's `report.json`. Cleanup is blocked while a bundle is pending. `verify` validates the pending bundle and reports the running desktop separately, without requiring the new profile to be running yet. Missing or invalid staging data produces an error and a repair command.

Save your work, log out, and select **myarch-buildkit** in the login screen's session chooser. This dedicated session applies the pending files before starting Hyprland; Hyprland then starts DMS through one `dms run` invocation. Selecting the ordinary Hyprland session leaves the bundle pending. If DankGreeter was newly enabled, it takes effect after reboot; your existing display manager may offer the myarch-buildkit session immediately.

### Confirm completion

In the new graphical session:

```bash
./myarch-buildkit check
./myarch-buildkit apply --stage verify --stage cleanup
```

`check` prints a readable summary of **pending configuration**, **running desktop**, app defaults and container readiness. Use `check --details` for extended findings or `check --json` for the complete JSON report, including errors. JSON output remains one valid document even when a check fails. These checks create no run folder. A validated pending bundle returns exit status 3; failed or incomplete checks return 2.

After application, the desktop journal in the original run's `report.json` changes from `pending_login` to `applied_before_login`. That confirms the files were applied before login; check DMS IPC, plugin loading, the three laptop preferences and the actual display before cleanup. The run's stage summary retains its original staging outcome, while the profile journal and internal login outcome record later application.

Apply prints readable stage results, the reason for blocked/skipped work and relevant retry or recovery commands. Verification requests no sudo up front. Cleanup requests sudo only when it performs a privileged action.

Open a new terminal for shell changes. The next-boot greeter takes effect after reboot. Test the login screen after saving work.

If this machine is already set up and checks pass, there is no need to repeat the full installation just to use the tools.

<!-- pagebreak -->

## 3. Commands for an existing installation

All examples assume the terminal is in the directory containing the executable. Stages always run in the installer's fixed order, regardless of the order of your flags. Preflight is automatically included in an apply run.

| Stage | What it does |
| --- | --- |
| `preflight` | Checks selected settings and desktop/laptop prerequisites |
| `packages` | Upgrades repository packages and installs selected tools |
| `defaults` | Applies app associations, terminal/editor preferences and fonts |
| `shell` | Adds shell integrations while preserving the login shell |
| `containers` | Configures Podman Compose and checks static rootless prerequisites |
| `laptop` | Stages a static fragment for natural scrolling, built-in keyboard layout and built-in display scale |
| `desktop` | Stages five plugins and natively validates the DMS/Hyprland profile; installs the myarch-buildkit login session |
| `greeter` | Configures password-based DankGreeter for the next boot |
| `verify` | Checks tools/defaults, validates pending files, and separately observes the running desktop |
| `cleanup` | Removes approved duplicate apps and hides selected launchers |

### Useful recipes

**Check the current setup without changing managed configuration:**

```bash
./myarch-buildkit check
./myarch-buildkit check --details
./myarch-buildkit check --json
```

**Install/update tools only:**

```bash
./myarch-buildkit apply --stage packages
```

**Retry one package group:**

```bash
./myarch-buildkit apply \
  --stage packages --package-group development
```

Groups are `core`, `utilities`, `development` and `containers`. A selected group must be enabled in settings. Include `--stage packages` to avoid selecting the other default stages.

**Reapply app defaults, or verify with a saved run report:**

```bash
./myarch-buildkit apply --stage defaults
./myarch-buildkit apply --stage verify
```

To reuse a specific run's settings, add `--settings` with its actual report path, for example:

```bash
./myarch-buildkit apply --stage verify \
  --settings "$HOME/Downloads/myarch-buildkit-runs/RUN_ID/report.json"
```

**Stage a new desktop when the static laptop fragment is installed or already pending:**

```bash
./myarch-buildkit apply \
  --stage desktop --stage verify
```

**Reapply configuration without package installation or cleanup:**

```bash
./myarch-buildkit apply --all \
  --configure-only
```

`--configure-only` stages desktop/laptop user files for the next myarch-buildkit login, while other selected configuration stages apply their changes during setup. It skips packages **and all cleanup**, including launcher hiding. It is not a preview. Configuration stages require their tools to be installed already.

<!-- pagebreak -->

## 4. Settings and optional flags

### Create a settings file

Use a new filename, so redirecting the template does not overwrite settings you already edited:

```bash
./myarch-buildkit --settings-template > myarch-buildkit-settings.json
```

Edit that JSON in VS Code, preview it, then use it for the actual run:

```bash
./myarch-buildkit plan --settings myarch-buildkit-settings.json
./myarch-buildkit apply --all \
  --settings myarch-buildkit-settings.json
```

The installer also automatically reads `~/.config/myarch-buildkit/settings.json` if it exists, or the equivalent under `XDG_CONFIG_HOME`. An explicit `--settings` path takes precedence. It does not automatically save your complete settings file there. Every apply run saves its resolved choices in the `settings` field of `report.json`; pass that report to `--settings` to reuse the snapshot.

A laptop-only stage needs either an existing myarch-buildkit desktop profile that loads the static laptop fragment or a subsequent desktop stage. Older profiles must be updated by staging desktop too. Cancel an earlier pending stage before staging another run of the same stage.

### Current defaults

| Setting under `laptop` | Fresh default | Meaning |
| --- | --- | --- |
| `internal_scale` | `"4/3"` | Internal display scaling; validated against the panel |
| `internal_keyboard` | `"hu"` | Hungarian internal keyboard layout |
| `natural_scroll` | `true` | Natural scrolling for pointer/touchpad policy |

The top-level `kernel_channel` defaults to `"unchanged"`; opt into `"stable"` or `"rc"` as described in [kernel selection](#latest-cachyos-release-candidate-kernel).

The template uses `schema_version: 2`. All four package groups are enabled by default; `core` must remain enabled. Partial settings are merged with defaults; unknown keys are rejected. There are no arbitrary per-application switches. Disabling a group does not uninstall its existing packages.

The default scale is 4/3. A settings-file scale or `--laptop-scale` explicitly overrides it. Unsupported scales are reported rather than silently substituted. Settings from older builds must drop the removed refresh, external-display, power, lid and idle keys; unknown keys are rejected.

### Optional flags

| Flag | When to use it |
| --- | --- |
| `--laptop-scale 4/3` | Explicitly choose the internal scale for a laptop/desktop run |
| `--keep-terminal` | Keep Alacritty when cleanup runs |
| `--enable-podman-socket` | Enable the user API socket for clients that require it; include the containers stage |
| `--remove-notes gnote` | Remove a chosen dedicated notes app during cleanup; allowed names: `gnote`, `bijiben`, `knotes`, `xpad` |
| `--help` | Show the installed installer's command reference |

`check` can use `--settings PATH`, including a saved run's `report.json`. It accepts `--details` or `--json`, separately, and cannot be combined with stage execution or apply-only options. `--cancel-pending laptop` and `--cancel-pending desktop` are standalone recovery actions for a missing staging reference; see section 7.

<!-- pagebreak -->

## 5. What changes after setup

### Apps, shell and everyday use

| Task | Default or shortcut |
| --- | --- |
| Open terminal | Ghostty: `Super+Enter` |
| Open file manager | GNOME Files: `Super+E` |
| Open editor | VS Code: `Super+C` |
| Open app launcher | `Super+Space` |
| Open PDF / audio / video | Firefox / VLC / VLC |
| Work with Markdown notes | Obsidian; register/open the containing folder as a vault |
| Browse a Synology SMB share | Files with SMB support; connect using the server address and your credentials |

Fastfetch uses its built-in Arch logo through `~/.config/fastfetch/config.jsonc` (or the corresponding `XDG_CONFIG_HOME` path). The defaults stage preserves existing modules, display settings, and other logo options, and backs up the original configuration. Only the logo changes; OS information still identifies CachyOS. An explicit `--logo` argument in a custom shell startup command overrides the config setting.

The installer keeps your existing login shell. It does not switch you to Zsh or Fish. It adds supported Starship, mise, zoxide, fzf and direnv integrations to Bash and applicable existing Zsh/Fish configurations. Existing prompt frameworks are preserved and may need manual adjustment if they conflict. Editor-related shell defaults use `code --wait`.

Sign into Bitwarden, GitHub and other services yourself. Choose your Obsidian vault. Configure project runtimes and dependencies as needed. SMB support does not create NAS mounts, store credentials or install a Samba server. The manifest does not add VPN plugins or VPN profiles.

DMS clipboard history starts disabled. ClipBoard+ uses that same backend; `Super+V` opens its panel. Use `cachyos-clipboard instructions` for the clipboard check before enabling history.

### Included DMS plugins

Each desktop run fetches the current default-branch HEAD of these five upstream repositories into its separate staging directory. These snapshots can include unreleased changes. A failed fetch stops staging instead of silently using an older checkout. The report records each manifest version, resolved commit, and destination.

Existing user plugin copies, including installations under another directory name, are updated at the next myarch-buildkit login. Replaced files are backed up. A snapshot inventory tracks managed files and hashes; unchanged tracked files removed upstream are backed up and removed at the login boundary. Untracked files and locally modified obsolete files are preserved because they may contain user data. On the first upgrade of an older/unmanaged installation, extra files are preserved until there is a managed baseline. Plugin preferences in `plugin_settings.json` are preserved except for the documented enablement/backend choices. Keep personal source changes separately: plugin code is replaced by upstream code. A user copy overrides an existing system plugin without modifying package-owned files. Edits made after staging block activation until you cancel/restage.

Bundles that remove obsolete files use pending-manifest version 3. Use this build's installed login helper to activate them; older helpers refuse these bundles. Explicit desktop restoration restores the backed-up files.

| Plugin | Registry ID | Setup / use |
| --- | --- | --- |
| Docker Manager | `dockerManager` | Bar widget, configured to use `podman` and Ghostty |
| Kubernetes | `kubernetes` | Bar widget; uses your existing `kubectl` context and kubeconfig |
| Emoji & Unicode Launcher | `emojiLauncher` | Open the launcher with `Super+Space`, then type `:e` and a search |
| Bongo Cat | `bongoCat` | Bar widget; reacts to typing after its input permission step below |
| ClipBoard+ | `clipboardPlus` | Bar widget and `Super+V`; uses the built-in DMS clipboard backend |

`podman`, `kubectl`, `evtest` and `libinput-tools` are included as core plugin dependencies even if optional package groups are disabled. The installer does not create a cluster or configure cluster credentials. ClipBoard+ needs no additional `cliphist` recorder with the selected DMS backend. Its notes, todos and pinned content remain user data, separate from ordinary clipboard history.

Bongo Cat requires membership in the `input` group. This grants access to keyboard and other input-device events. The installer does not change group membership. To enable its typing animation, run this explicit step as your regular user, then log out and log back in:

```bash
sudo usermod -aG input "$USER"
```

Check the current session with `id -nG`. Without the permission, Bongo Cat shows a warning and the running-desktop check reports the missing setup. Its idle animation is part of the widget; it does not change system idle or power policy.

The new plugin copies are manual snapshots, with no automatic updates or extra startup services. To change a myarch-buildkit-installed snapshot, cancel it before login or restore its desktop stage from a TTY after logout, then restage with an updated installer. If you prefer DMS-managed plugin updates, remove the restored snapshot's empty folder and install the corresponding registry ID through DMS Settings → Plugins; subsequent myarch-buildkit runs preserve that installation. Existing DMS-managed installations keep their usual update mechanism.

### Appearance defaults

The installer does not download themes, install a VS Code theme extension, edit Obsidian appearance, generate GTK colors, copy a desktop theme into DankGreeter, or add custom Hyprland colors, gaps, rounding or opacity. Fresh configurations use the applications' default appearance. The Frame Mode, top bar and bottom dock behavior remains configured.

Existing personal appearance preferences and previously installed themes are left intact; removing theme setup from this installer does not reset a previously themed desktop. Fonts, app associations and shell integrations remain part of the defaults and shell stages. The `dms ipc call theme getMode` diagnostic is only a read-only readiness check.

### Laptop behavior

The laptop stage configures only natural scrolling for pointers/touchpads, Hungarian layout for keyboards verified as built-in by sysfs/udev, and 4/3 scale for the built-in display. It generates a static Hyprland fragment and a small metadata file. An unverified built-in keyboard or display stops setup instead of applying a global keyboard layout or guessing an output.

External keyboard layouts and display rules are not configured. No refresh rate, display switching, power profile, lid action, idle timeout or automatic lock policy is imposed. No laptop background helper is installed. Existing system and DMS preferences govern those behaviors.

When upgrading from an older myarch-buildkit build, its two laptop helper units are disabled at the next desktop login boundary. Previously applied power, logind or idle configuration is not automatically deleted. Use the earlier build's stage report and explicit restoration to undo those earlier changes; packages remain installed.

### Updates and cleanup

The packages stage runs a full upgrade against the **enabled repositories**, then installs/updates the selected tools. It updates the manifest-selected AUR packages through a working `paru` or `yay`. It does not perform an update of every unrelated AUR package, Flatpak, pip/npm installation or project runtime. It does not schedule automatic updates.

Installed supported alternatives such as `k6` or `k6-bin` are retained when their repository/AUR update source can be verified. If that source is unavailable, setup searches the listed alternatives with an available source; package replacement remains subject to normal interactive package-manager prompts. If none is available, the package stage fails instead of reporting an old package as updated. The plan lists package options; the package profiles in `report.json` identify exact choices and installed versions.

“Latest” means the versions offered by your enabled repositories/AUR for the selected package variants, and current upstream default branches for plugins. Setup does not enable testing repositories or replace every package with a development `-git` variant. Hyprland is already installed and receives normal repository upgrades during the full system upgrade.

Dux Shell (DMS) 2.0 keeps the `dms` command and currently uses the `dms-shell` package. It moves config/state/cache from `DankMaterialShell/` to `dms/`, leaving compatibility symlinks. The installer writes directly to the new paths, uses settings schema 42/session schema 7, and configures the new per-dock settings and pins. If old user directories have not migrated yet, start the installed DMS once to let it migrate preferences, then rerun desktop setup. Existing JSON with an older schema must likewise be migrated by DMS first; setup does not relabel unmigrated data.

For newer DMS releases, setup inspects installed schemas. When QML is embedded, it uses a verified release adapter or fetches the installed version's exact upstream tag and pinned shared-schema submodule during staging. Missing required fields or unavailable release sources stop configuration with an error. The installed DMS version/build output is recorded and must still match at login; upgrades after staging require restaging. Plan, check, and login do not fetch plugin or schema sources.

Hyprland `.lua` and `.conf` configurations are selected by the active file's format and verified with the installed compositor's `--verify-config`. A newer version number alone does not block setup. This checks syntax/API compatibility, not all runtime desktop behavior.

Cleanup removes Dolphin when Files is available; Alacritty when Ghostty and Hyprland checks pass, unless kept; and known duplicate GUI editors when VS Code is available. Dedicated notes apps are removed only when explicitly named. Normal package dependency checks can prevent removal.

Selected camera diagnostics, Avahi browsers, hardware-topology and Qt/GTK settings launchers are hidden using reversible per-user overrides. Their packages remain installed. Cleanup does not remove every unfamiliar app, purge orphan packages, or uninstall Noctalia itself.

### Latest CachyOS release-candidate kernel

Kernel selection is opt-in. Add the top-level setting `"kernel_channel": "rc"` to your settings JSON. Allowed values are `unchanged` (the default, preserving the existing full-upgrade behavior), `stable` (select `linux-cachyos`), and `rc` (select `linux-cachyos-rc`). Old settings and saved reports without this field retain `unchanged` behavior. The selected channel is retained in your settings file and saved run snapshots; it does not schedule updates.

“Latest” means the version available from the enabled CachyOS repositories when apply refreshes package databases. The RC package follows mainline development and may contain a release candidate or a release-transition build; the installer does not hardcode a kernel version or promise an upstream release that CachyOS has not packaged. See [CachyOS kernel variants and module support](https://wiki.cachyos.org/features/kernel/).

#### Supported automatic path

This implementation requires the **active systemd-boot loader**, Secure Boot disabled, and standard Type 1 entries referencing `vmlinuz-PACKAGE` and `initramfs-PACKAGE.img`. The boot partition must be mounted at `/boot`, `/efi`, or `/boot/efi`, with readable images and at least 1 GiB available on both that partition and the filesystem containing `/boot`.

Before opting in, install and boot a stable/LTS fallback using the CachyOS kernel manager. The installer requires an existing, verifiable `linux-cachyos-lts`, `linux-lts`, `linux-cachyos`, or `linux` entry; it never removes that package. An initramfs fallback image of the RC kernel itself is not a separate stable kernel.

GRUB, Limine, rEFInd, unified kernel images, custom entry layouts, enabled/unknown Secure Boot, and DKMS drivers require manual kernel/boot validation. Automatic selection stops before changing installed packages when these conditions cannot be verified. Use the [CachyOS kernel manager](https://wiki.cachyos.org/features/kernel_manager/) and [boot-manager instructions](https://wiki.cachyos.org/configuration/boot_manager_configuration/) for those systems; retain a stable/LTS entry, select the new kernel manually from the boot menu, and test it before changing the default. Do not disable Secure Boot simply to bypass this check.

NVIDIA and ZFS installations require the corresponding prebuilt target-kernel modules from CachyOS. The installed NVIDIA open/closed driver family is retained. Package dependency resolution must succeed, and module vermagic must match the installed kernel before boot activation. DKMS builds and ambiguous/legacy NVIDIA providers are deliberately not inferred to be compatible.

#### Install, trial boot, then promote

After editing your settings file:

```bash
./myarch-buildkit plan --stage packages --settings myarch-buildkit-settings.json
./myarch-buildkit apply --stage packages --settings myarch-buildkit-settings.json
./myarch-buildkit check --settings myarch-buildkit-settings.json
```

The packages stage performs kernel selection once per run, including when a single package group is selected. `--configure-only` skips kernel installation. Preview reads local inventory only; cached repository versions are provisional. Check reports the running release, installed target release/version, fallback entry, and whether a reboot is required.

Apply first validates the current fallback, boot environment, space, and driver requirements. It refreshes/downloads a full system upgrade, resolves the exact kernel, matching headers and prebuilt driver packages, then downloads the selected transaction before installing it with a full upgrade. No AUR kernel or version downgrade is selected. If resolution fails after refresh, complete a full `sudo pacman -Syu` before other package installations; do not perform a partial upgrade against refreshed databases.

The validated fallback is made the default and any old one-time selection is cleared before installation. Package hooks generate boot images/entries. Afterward, both target and fallback images are checked against the installed kernel files, initramfs contents must contain the matching kernel release, required modules must match, and the fallback entry must still be the default. A successful pacman exit alone is insufficient. If verification fails, repair the reported problem before rebooting.

Request a trial boot explicitly:

```bash
./myarch-buildkit --kernel-boot once --settings myarch-buildkit-settings.json
```

This revalidates the installed images and modules, keeps the fallback as default, and selects the target for **one boot**. Save work and reboot manually. Confirm `uname -r`, then test graphics, networking, storage, suspend/resume, and your Hyprland session. When satisfied:

```bash
./myarch-buildkit --kernel-boot promote --settings myarch-buildkit-settings.json
```

Promotion requires that systemd-boot reports the selected entry as the current boot and `uname -r` matches the currently installed target kernel. It changes the default without rebooting. The command is an explicit user decision after testing; it does not claim to automatically validate hardware. See [bootctl's boot selection commands](https://www.freedesktop.org/software/systemd/man/latest/bootctl.html).

Subsequent package runs with this channel update the kernel from the repositories and return the default to the fallback for a fresh trial/promotion cycle. `stable` follows the same process with `linux-cachyos`; `unchanged` stops selecting a channel but does not uninstall the RC package or prevent normal pacman upgrades to it.

#### Recovery and validation limits

If a trial fails, reboot and select the stable/LTS entry. A one-time boot does not make the RC entry the persistent default, but it is not a watchdog and cannot recover a hung machine by itself. After promotion, use the boot menu to select the fallback if needed. Keep that entry and package installed. Configuration restore does **not** roll back kernel packages, repository upgrades, or bootctl default/one-time selections.

Automated tests cover settings compatibility, version resolution, downgrade refusal, missing drivers, boot-space/mount checks, stale images, failed transactions, repeated upgrades, fallback retention, and promotion guards using an isolated filesystem and simulated commands. Installation and reboot still require validation in a CachyOS VM and on target hardware; this development build is not claimed to have passed those checks.

<!-- pagebreak -->

## 6. Pending desktop configuration and reports

### Separate files and one startup method

The bundle lives in the internal run directory under `~/.local/state/myarch-buildkit/runs/RUN_ID/STAGE/`, or the corresponding directory under `XDG_STATE_HOME`, away from active DMS files and their file watchers. Nothing stops DMS or reloads Hyprland during staging. The login launcher checks every pending payload and non-JSON baseline before writing any configuration. It also reruns native Hyprland validation and checks that DMS still matches the version/build validated during staging. DMS settings, session, clipboard and plugin settings JSON merge the intended properties into the latest existing objects; unrelated runtime properties are retained.

Backups capture the files immediately before they are replaced or removed at login. Each write is journalled first. An error or interruption leaves the bundle blocked for explicit cancellation/restoration; the launcher does not start the compositor after a failed apply, retry the operation automatically, or launch a rescue shell.

The managed startup is **Hyprland start hook → `dms run`**. At the login boundary, the launcher disables recorded alternate DMS and recognized competing service autostarts without stopping a running service. If a graphical desktop or relevant service is still running, it refuses to apply files. Recognized competing XDG autostarts are overridden in the pending bundle. Review personal `desktop-custom.lua` / `.conf` overrides for other shell launchers.

The original packaged Hyprland login choices remain available. Use a text TTY if the myarch-buildkit login reports a problem. `--allow-live-desktop` has been removed and produces an error; it does not enable a live handover.

### Where to look

Each new apply run creates **one user-facing report file**: `~/Downloads/myarch-buildkit-runs/RUN_ID/report.json` when run from Downloads. If you run the installer using an absolute path from another directory, the report is created in that working directory instead. `RUN_ID` is the timestamped directory printed by the installer.

This report combines resolved settings, executable identity, stage outcomes, package choices, verification results and restoration journals. New runs do not create separate `effective-settings.json`, `stages.json`, per-stage report files, settings-validation files or TSV version reports in the report folder. Old runs keep their original layout and remain readable/restorable.

| Field in `report.json` | Purpose |
| --- | --- |
| `format` / `schema_version` | Identifies `myarch-buildkit-run`, report schema 3 |
| `settings` | Resolved settings snapshot; reusable with `--settings report.json` |
| `provenance` | Build, executed binary and executable SHA256 |
| `stages` | Overall stage outcomes, reasons and concrete retry/restore commands |
| `profiles` | Stage findings and schema-1 backup/restoration journals keyed by stage, including package-group profiles |
| `profiles._run` | Journal for the installed per-user executable |
| `pending_configuration` / `running_desktop` | Pending work and observed running state kept separate |

Backups, staged payloads, manifests and other working files live internally under `~/.local/state/myarch-buildkit/runs/RUN_ID/STAGE/`, or the corresponding directory under `XDG_STATE_HOME`. These are actual file contents needed for later application or restoration. A report records their locations; it does not replace the backups. Keep the report and internal run data while a bundle is pending or you may want to restore it.

The queue is `~/.local/state/myarch-buildkit/pending-login.json`; the latest login outcome is `login-status.json` beside it (or under `XDG_STATE_HOME`). Queue entries refer to internal manifests and the unified report, so do not move or delete pending run data or its report.

Standalone `check` prints findings without creating a run. Use `apply --stage verify` when you want verification saved in a new `report.json`.

### Reading outcomes

- **completed:** the stage passed its relevant checks.
- **pending_login:** valid configuration is staged; the running desktop has not been replaced.
- **applied_before_login:** files were applied before the compositor started; confirm live readiness with `check`.
- **blocked / skipped:** read the stated reason; cleanup waits for pending work, and desktop depends on laptop files.
- **failed / partial:** inspect the report and use explicit restoration as needed. No automatic desktop rescue runs.
- **restored / restore_partial:** explicit restore completed, or preserved conflicts need manual review.

There is no `status` subcommand. Use `check` for a readable current summary, `check --details` for extended findings or the saved `report.json` for a run's history.

<!-- pagebreak -->

## 7. Troubleshooting, retry and restore

### Start with the reported failure

| Message or symptom | Next action |
| --- | --- |
| A package is unavailable, or a transaction fails | Read the exact package error. Correct its repository, dependency or file-conflict issue before retrying packages. Do not force file overwrites. |
| Exit status 3 / pending login | Save work, log out and select myarch-buildkit. An ordinary Hyprland login does not apply the bundle. |
| myarch-buildkit login blocked | Inspect the run's `report.json` and internal `login-status.json` from a TTY. Resolve the conflict or explicitly restore/cancel, then stage a fresh bundle. |
| Pending laptop/desktop manifest is missing | Use the exact recovery command printed by `check`. Remove a missing reference with `--cancel-pending`, cancelling/restoring queued desktop before laptop, then stage a replacement. |
| Earlier pending stage exists | Cancel/restore the named pending stage before retrying; cancel its dependent desktop stage before its laptop stage. |
| Existing shell theme warning | Open a new terminal and inspect the prompt. Existing theme configuration was preserved. |
| Desktop readiness failed | Keep the reports. Use the IPC and Hyprland output below; avoid repeated full runs. |
| Changed since setup / restore manually | Compare the saved backup with the current file. The installer preserved a later change rather than overwriting it. |

From a terminal in the graphical session, these commands inspect the current state:

```bash
dms ipc call theme getMode
dms ipc call plugin-scan list
id -nG
hyprctl monitors
hyprctl devices
hyprctl getoption input:natural_scroll
hyprctl getoption input:touchpad:natural_scroll
hyprctl layers
```

The myarch-buildkit profile launches DMS directly through Hyprland; do not enable either old DMS service or add another `dms run` autostart. Do not restart the login manager to fix a missing dock. If you cannot open a graphical terminal, a text TTY such as `Ctrl+Alt+F3` can provide access for service/log diagnostics. GUI IPC checks require the graphical session environment.

### Retry only what needs work

Prefer the **Retry** or **Repair** command printed by the failed run. New-run retries use that original run's `report.json` as the settings snapshot, so you do not need a separate settings file. Completed package installations are retained. Desktop requires the static laptop settings fragment; retry laptop first if its selected stage failed. A failed desktop staging step blocks dependent later stages. Earlier successful pending stages remain queued; cancel them explicitly if you want a fresh run.

### Repair a missing pending reference

If staging files have been deleted or moved, `check` names the missing laptop/desktop manifest and prints a recovery command. Use ordinary stage restoration when its journal and backups are still available. When the manifest itself is missing, these standalone commands explicitly remove its queue reference:

```bash
./myarch-buildkit --cancel-pending desktop
./myarch-buildkit --cancel-pending laptop
```

Run only the commands for missing queued stages. A queued desktop depends on laptop, so cancel/restore desktop first; the installer refuses to cancel laptop while a dependent desktop remains queued. `--cancel-pending` refuses a stage whose manifest still exists; restore that stage instead. It removes only the missing state reference and does not change or restore desktop files. Then run `check`, stage the needed replacement and log in through myarch-buildkit. Cleanup remains blocked until pending work is resolved and verification passes.

### Restore a stage's configuration

Use the exact restore command printed for the original run or recorded in its `report.json`. For example, after replacing `RUN_ID` with a real new-format run directory:

```bash
./myarch-buildkit --restore-stage defaults \
  --stage-run-dir "$HOME/Downloads/myarch-buildkit-runs/RUN_ID"
```

Supported restore stages are `defaults`, `shell`, `containers`, `laptop`, `desktop`, `greeter` and `cleanup`. For new runs, supply the absolute path to the **top-level run folder containing `report.json`**; the installer selects the named profile and its internal backups. Do not add `apply`, `--configure-only` or `--allow-live-desktop` to a restore command. Restoring an unapplied stage cancels its queue entry and restores any setup-time system changes. Cancel the dependent desktop stage before its pending laptop stage. To restore files already applied at login, first log out of graphical sessions and run the restore command from a TTY; the installer refuses to overwrite a running desktop. Ordinary defaults, shell, containers and cleanup restoration can run independently; applied desktop/laptop/greeter changes require logout when their report includes desktop or login changes.

This rename changes configuration/state paths, the login-session entry, system backup paths, and the unified report format identifier. Existing installations and pending runs are not migrated automatically. Use the original executable and session to complete or restore runs created before the rename; keep their reports and backup files. Do not mix pre-rename pending runs with a new installation. `--restore-stage` also understands the earlier `desktop-report.json`, `shell-report.json`, `podman-report.json` and `cleanup-report.json` formats when those named files identify the selected stage. The earlier profile report format remains supported for desktop/laptop/greeter restoration. Keep the full original run and backup files.

`--restore-profile /absolute/run/folder` resolves a top-level run to its **desktop** profile, including the unified format. It does not restore every stage or the root executable backup. For shell, defaults, containers, laptop, greeter or cleanup, use `--restore-stage` with the new run root or the older run's exact stage folder. Stages can be restored independently; an unapplied desktop still depends on its pending laptop stage, so cancel desktop first.

Restore is not a system rollback: package upgrades stay installed; removed packages are not reinstalled; cleanup restore restores launcher/configuration changes only. Restoring Podman configuration does not automatically disable an API socket you opted into. Files modified after setup are preserved and reported as conflicts. Restoration of an applied desktop preserves later edits and can be partial. When restoring reports from an older themed build, a theme referenced by subsequently edited DMS settings is retained. Desktop/greeter restores can change the next login; review their reports before using them.

<!-- pagebreak -->

## 8. Limits and a short maintenance note

### Practical limits

- **Setup starts from an installed CachyOS + Hyprland base.** This release requires native CachyOS x86_64 and supported Hyprland/DMS versions. Regular Arch Linux, EndeavourOS, WSL and other desktop environments are not supported. myArch-BaseKit does not install the operating system or build an ISO.
- **It is not a frozen system image.** Repository and AUR versions change. An old ISO receives repository updates, but package availability and future desktop APIs can still break assumptions. Review prompts and warnings.
- **A successful check is not a complete hardware test.** Test login after reboot, monitor plug/unplug, lid close/open, suspend/resume, Wi-Fi, keyboard/touchpad, camera, audio and recording on the target machine. Rootless container checks are static prerequisites; they do not run every workload.
- **It does not configure company access.** Accounts, cloud credentials, VPNs, remote clusters, project secrets and NAS credentials remain separate setup work.
- **Rerunning can reapply managed preferences.** Keep custom Hyprland changes in the generated custom configuration file. Do not assume every manually edited managed setting will survive a future apply. Review the settings and report first.
- **Recovery is limited.** Backups and explicit restoration cannot guarantee recovery from every hardware or compositor failure. No live handover, detached recovery worker or automatic rescue is included.

### Current validation baseline

This guide describes build `v0.3.0`. The non-root suite skips five privileged-operation tests; run `go test ./...` and `go vet ./...` for current results. Tests also cover latest-plugin refresh, obsolete-file restoration, Dux 2.0 paths/dock pins, future schema versions, and version changes between staging and login. CI also runs the five privileged-operation tests as root against temporary directories on an ephemeral runner. Real system operations and greeter setup still need target-machine testing. Tests cover CLI/settings validation, package selection and interactive review, readable check/apply output, JSON error reporting, unified reports and settings snapshots, separate staging, login ordering, payload/baseline conflicts, interrupted writes, DMS property merging, missing pending references, cleanup blocking, explicit and legacy restoration, source-preserving JSONC/MIME edits, personal appearance and symlink preservation, internal-only keyboard targeting, the three static laptop settings, staged fragment dependencies, plugin snapshots/preferences/loading, shell and container integrations, clipboard checks and screenshot handling. The [project README](../README.md) lists the Go test, vet and executable-build commands.

A prior build was exercised on a target machine, applying staged configuration before Hyprland startup and loading all five DMS plugins. This build’s Dux 2.0 and latest-plugin changes have automated coverage and upstream-source checks, but still need a target-machine login test. Natural-scrolling verification and suspend/resume issues remain unresolved; this is not a complete hardware validation. Test the myarch-buildkit session, DMS, its five plugins, built-in display scaling, keyboard layout, natural scrolling and suspend/resume on the target machine before treating setup as complete. Native verification requires Hyprland config-validation support and an inspected DMS schema. Verified embedded-QML adapters cover DMS 1.6.2 (18/4) and Dux 2.0.0 (42/7); other versions require inspectable release or installed source.

### Maintaining the native Go version

Keep the compiled executable, this guide and the matching Go source project together with a known-working version and its run reports. The [project README](../README.md) documents the standard Go build and test commands. Build with Go 1.23 or newer; the compiled installer needs no Go or Python runtime on the target. Do not regenerate this Go version with the earlier Bash/Python build scripts.

Use `--help`, `--settings-template` and `plan` for the current command reference. `--version` prints the build identifier and executable SHA256. The run's `report.json` records the build, executed binary and its hash.

The single startup method follows [DMS's documented compositor-managed Hyprland launch](https://danklinux.com/docs/dankmaterialshell/managing#manual-launch). The installer still verifies the installed schema and configuration format rather than assuming arbitrary future versions are compatible.

The requested plugin IDs and repositories follow the [DMS registry](https://github.com/AvengeMedia/dms-plugin-registry). Plugins use DMS default styling; this build adds no theme setup.

Published Linux x86-64 downloads and checksum verification are described in the [README](../README.md#download-a-release). Maintainers should follow the [release procedure](../CONTRIBUTING.md#release-procedure).

From the repository root, build from source with Go 1.23 or newer:

```bash
go test ./...
go vet ./...
go build -o bin/myarch-buildkit ./cmd/myarch-buildkit
./bin/myarch-buildkit --version
```

<!-- pagebreak -->

## 9. Package reference

This reference follows `PackageManifest()` in `cmd/myarch-buildkit/packages.go`. All four groups are enabled by default. Core must remain enabled; utilities, development, and containers can be disabled through the package-group settings described in section 4. These are direct package requirements, not a list of every transitive dependency that the package manager may install.

Repository candidates are tried in the listed order unless an installed supported variant has a verified update source. AUR candidates are fallbacks, also in order; alternatives are not all installed together. Availability depends on the enabled repositories at apply time. An em dash means no candidate or command is declared in that column. Commands identify the tools checked by the installer, not every executable included in a package.

Podman and kubectl appear in both their optional groups and core because the DMS plugins require them. Disabling containers or development therefore does not remove those core requirements. Installing tools does not configure accounts, clusters, cloud credentials, or project environments.

### Core packages

| Tool | Repository candidates | AUR fallback | Commands checked | Purpose |
| --- | --- | --- | --- | --- |
| DMS Docker Manager backend | `podman` | — | `podman` | Runs containers for the DMS Docker Manager plugin using Podman. |
| DMS Kubernetes backend | `kubectl` | — | `kubectl` | Lets the DMS Kubernetes plugin query Kubernetes clusters. |
| Bongo Cat keyboard events | `evtest` | — | `evtest` | Reads Linux input events for the Bongo Cat typing animation; access requires the optional input-permission setup. |
| Bongo Cat input tools | `libinput-tools` | — | `libinput` | Provides input-device inspection tools used by the Bongo Cat integration. |
| Fastfetch | `fastfetch` | — | `fastfetch` | Displays system information in the terminal; the defaults stage selects the Arch logo. |
| Ghostty | `ghostty` | — | `ghostty` | Provides the default terminal emulator. |
| GNOME Files | `nautilus` | — | `nautilus` | Provides the graphical file manager, also called Nautilus. |
| GNOME Files SMB support | `gvfs-smb` | — | — | Lets Files browse and access SMB network shares; does not configure mounts or credentials. |
| GNOME Files Open in Ghostty | `ghostty-nautilus` | — | — | Adds an Open in Ghostty action to Files. |
| Microsoft VS Code | `visual-studio-code-bin` | `visual-studio-code-bin` | `code` | Provides Microsoft's Visual Studio Code editor distribution. |
| Bitwarden Desktop | `bitwarden` | — | `bitwarden-desktop` | Provides the graphical Bitwarden password manager. |
| Bitwarden CLI | `bitwarden-cli` | — | `bw` | Provides terminal access to Bitwarden through the bw command. |
| Obsidian | `obsidian` | — | `obsidian` | Provides a Markdown-based notes and knowledge-management app. |
| Firefox PDF viewer | `firefox` | — | `firefox` | Provides a web browser and the configured PDF viewer. |
| VLC media player | `vlc` | — | `vlc` | Plays video and audio files. |
| VLC format support | `vlc-plugins-all` | — | — | Adds VLC's full plugin set for broader media-format support. |
| JetBrains Mono Nerd Font Mono | `ttf-jetbrains-mono-nerd` | — | — | Provides a monospaced programming font with Nerd Font symbols for terminals and prompts. |
| Git | `git` | — | `git` | Provides version control and repository cloning, including installer source downloads. |
| direnv | `direnv` | — | `direnv` | Loads and unloads directory-specific environment variables after authorization. |
| fzf | `fzf` | — | `fzf` | Provides interactive fuzzy selection for shell workflows. |
| zoxide | `zoxide` | — | `zoxide` | Tracks frequently used directories for quick navigation. |
| Starship | `starship` | — | `starship` | Provides the configured cross-shell prompt. |
| mise | `mise` | — | `mise` | Manages project tool versions and development environments; project runtimes are selected separately. |
| XDG terminal selection | `xdg-terminal-exec` | — | `xdg-terminal-exec` | Launches the selected terminal for desktop applications that request one. |
| XDG MIME tools | `xdg-utils` | — | `xdg-mime`, `xdg-open` | Provides xdg-open and xdg-mime for opening files and managing default applications. |
| Desktop entry tools | `desktop-file-utils` | — | `update-desktop-database`, `desktop-file-validate` | Validates desktop entries and updates the desktop application database. |
| Font configuration | `fontconfig` | — | `fc-cache`, `fc-match` | Manages font discovery, matching, and caches. |
| AUR build dependencies | `base-devel` | — | — | Provides the Arch build-tool meta-package needed to build AUR packages. |
| DankMaterialShell | `dms-shell` | — | `dms` | Provides Dux Shell, formerly Dank Material Shell, including the dms command and desktop shell UI. |
| Quickshell | `quickshell` | — | `qs` | Provides the Qt/QML runtime used by the desktop shell. |
| DankGreeter | `greetd-dms-greeter-bin` | `greetd-dms-greeter-bin` | `dms-greeter` | Provides the DMS-based graphical login greeter. |
| greetd login manager | `greetd` | — | `greetd` | Provides the login service used to launch the configured greeter and session. |
| Region screenshot capture | `grim` | — | `grim` | Captures Wayland screenshots; paired with slurp for region selection. |
| Region screenshot selection | `slurp` | — | `slurp` | Selects a screen region for screenshot capture. |
| Wayland clipboard tools | `wl-clipboard` | — | `wl-copy`, `wl-paste` | Provides wl-copy and wl-paste for the Wayland clipboard. |
| Desktop notification tools | `libnotify` | — | `notify-send` | Provides notify-send for sending desktop notifications. |
| Hyprland desktop portal | `xdg-desktop-portal-hyprland` | — | — | Provides Hyprland's desktop portal integration, including screen-sharing support. |
| GTK file-picker portal | `xdg-desktop-portal-gtk` | — | — | Provides GTK-based file-picker portal dialogs for applications. |

### Utilities packages

| Tool | Repository candidates | AUR fallback | Commands checked | Purpose |
| --- | --- | --- | --- | --- |
| btop | `btop` | — | `btop` | Monitors CPU, memory, disks, network traffic, and processes in an interactive terminal view. |
| dust | `dust` | — | `dust` | Displays directory disk usage as a readable tree to help locate large files and folders. |
| PeaZip archive manager | `peazip`, `peazip-qt-bin` | `peazip-qt-bin`, `peazip` | `peazip` | Provides a graphical archive manager for creating and extracting archives. |
| GNOME Camera (Snapshot) | `snapshot` | — | `snapshot` | Provides a graphical camera app for photos and video. |
| Kooha screen recorder | `kooha` | — | `kooha` | Provides a graphical screen recorder. |
| Kooha H.264 encoding | `gst-plugins-ugly` | — | — | Adds GStreamer plugins used for Kooha's H.264 encoding support. |
| LocalSend | `localsend`, `localsend-bin` | `localsend-bin`, `localsend` | `localsend` | Transfers files between devices on the local network. |
| LocalSend compatibility library | `libayatana-indicator` | — | — | Provides indicator-library compatibility required by the LocalSend integration. |

### Development packages

| Tool | Repository candidates | AUR fallback | Commands checked | Purpose |
| --- | --- | --- | --- | --- |
| GitHub CLI | `github-cli` | — | `gh` | Works with GitHub repositories, pull requests, issues, and authentication from the terminal. |
| Git Delta | `git-delta` | — | `delta` | Displays Git diffs with syntax highlighting and improved formatting. |
| Lazygit | `lazygit` | — | `lazygit` | Provides an interactive terminal interface for Git. |
| pre-commit | `pre-commit` | — | `pre-commit` | Runs configured checks through Git hooks before commits. |
| actionlint | `actionlint` | — | `actionlint` | Checks GitHub Actions workflow syntax, expressions, and common configuration errors. |
| ShellCheck | `shellcheck` | — | `shellcheck` | Analyzes shell scripts for quoting, variable-expansion, and other common mistakes. |
| Make | `make` | — | `make` | Runs build recipes defined in Makefiles. |
| GNU Coreutils | `coreutils` | — | `ls`, `cp`, `mv`, `cat`, `sort` | Provides standard file and text utilities such as ls, cp, mv, cat, and sort. |
| wget | `wget` | — | `wget` | Downloads files over HTTP, HTTPS, and FTP. |
| jq | `jq` | — | `jq` | Queries and transforms JSON from the command line. |
| yq (Mike Farah) | `go-yq` | — | `yq` | Queries and edits YAML using Mike Farah's Go implementation of yq. |
| ripgrep | `ripgrep` | — | `rg` | Searches file contents quickly with the rg command. |
| fd | `fd` | — | `fd` | Finds files and directories by name and other filters. |
| bat | `bat` | — | `bat` | Displays file contents with syntax highlighting. |
| eza | `eza` | — | `eza` | Lists directory contents with additional formatting and metadata. |
| tmux | `tmux` | — | `tmux` | Keeps terminal sessions running and organizes them into windows and panes. |
| tree | `tree` | — | `tree` | Displays directory contents as a tree. |
| watch | `procps-ng` | — | `watch` | Provides watch for repeating a command and displaying its output; procps-ng also contains process utilities. |
| kubectl | `kubectl` | — | `kubectl` | Manages Kubernetes resources and clusters; also required by the core DMS plugin. |
| kubectx + kubens | `kubectx` | — | `kubectx`, `kubens` | Switches Kubernetes contexts and namespaces using kubectx and kubens. |
| K9s | `k9s` | — | `k9s` | Provides an interactive terminal interface for Kubernetes. |
| Helm | `helm` | — | `helm` | Installs and manages applications packaged as Kubernetes Helm charts. |
| Helmfile | `helmfile` | — | `helmfile` | Declares and coordinates sets of Helm releases. |
| Kustomize | `kustomize` | — | `kustomize` | Customizes Kubernetes YAML using reusable bases and overlays. |
| kind | `kind` | — | `kind` | Runs local Kubernetes clusters in containers; installation alone does not create a cluster. |
| Stern | `stern` | — | `stern` | Streams logs from multiple Kubernetes pods. |
| Kubecolor | `kubecolor` | `kubecolor` | `kubecolor` | Adds color to kubectl output. |
| Kubeconform | `kubeconform` | — | `kubeconform` | Validates Kubernetes manifests against schemas. |
| Terraform | `terraform` | — | `terraform` | Defines and provisions infrastructure as code. |
| Terragrunt | `terragrunt` | — | `terragrunt` | Coordinates Terraform configurations and reduces repeated infrastructure configuration. |
| TFLint | `tflint` | — | `tflint` | Lints Terraform configuration for errors and best-practice issues. |
| Terraform Docs | `terraform-docs` | `terraform-docs` | `terraform-docs` | Generates documentation from Terraform modules. |
| Infracost | `infracost` | `infracost` | `infracost` | Estimates cloud costs from infrastructure definitions; service setup may be needed. |
| Packer | `packer` | — | `packer` | Builds machine images from templates. |
| Ansible | `ansible` | — | `ansible`, `ansible-playbook` | Automates machine configuration and deployment with inventories and playbooks. |
| Trivy | `trivy` | — | `trivy` | Scans container images, filesystems, and infrastructure configuration for security issues. |
| Gitleaks | `gitleaks` | — | `gitleaks` | Scans source code and Git history for exposed secrets. |
| HTTPie | `httpie` | — | `http`, `https` | Provides readable command-line HTTP requests through http and https. |
| grpcurl | `grpcurl`, `grpcurl-bin` | `grpcurl-bin`, `grpcurl` | `grpcurl` | Calls and inspects gRPC services from the terminal. |
| websocat | `websocat` | — | `websocat` | Connects WebSockets to command-line streams for testing and debugging. |
| MTR | `mtr` | — | `mtr` | Combines traceroute and ping-style measurements to diagnose network paths. |
| Nmap | `nmap` | — | `nmap` | Discovers hosts and scans network ports and services. |
| DNS diagnostics | `bind` | — | `dig` | Provides dig and other DNS utilities; installing it does not configure a DNS server. |
| Network throughput testing | `iperf3` | — | `iperf3` | Measures network throughput between test endpoints. |
| Packet capture | `tcpdump` | — | `tcpdump` | Captures and inspects network packets from the terminal. |
| Ethernet diagnostics | `ethtool` | — | `ethtool` | Inspects and configures Ethernet device and link settings. |
| Wireshark packet analyzer | `wireshark-qt` | — | `wireshark` | Provides a graphical packet analyzer; capture access depends on system permissions. |
| k6 | `k6`, `k6-bin` | `k6-bin`, `k6` | `k6` | Runs scripted load and performance tests. |
| uv | `uv` | — | `uv`, `uvx` | Manages Python environments, dependencies, and command-line tools through uv and uvx. |
| Go (includes go install) | `go` | — | `go`, `gofmt` | Provides the Go compiler, standard tooling, gofmt, and go install. |
| golangci-lint | `golangci-lint` | — | `golangci-lint` | Runs a collection of Go linters. |
| gopls | `gopls` | — | `gopls` | Provides the Go language server for editor integration. |
| Delve | `delve` | — | `dlv` | Debugs Go programs through the dlv command. |
| GoReleaser | `goreleaser` | — | `goreleaser` | Builds and packages Go project releases. |
| Neovim | `neovim` | — | `nvim` | Provides an extensible terminal text editor. |
| Yazi | `yazi` | — | `yazi` | Provides a terminal file manager with previews and navigation. |
| Midnight Commander | `mc` | — | `mc` | Provides a dual-pane terminal file manager. |

### Containers packages

| Tool | Repository candidates | AUR fallback | Commands checked | Purpose |
| --- | --- | --- | --- | --- |
| Podman | `podman` | — | `podman` | Runs and manages containers; also required by the core DMS Docker Manager plugin. |
| Podman Compose | `podman-compose` | — | `podman-compose` | Runs Compose-defined multi-container applications with Podman. |
| Podman rootless networking | `passt` | — | `pasta` | Provides pasta for rootless container networking. |
| Podman subordinate-ID helpers | `shadow` | — | `newuidmap`, `newgidmap`, `getsubids` | Provides subordinate user/group ID helpers used by rootless containers. |

### AUR helper and existing-system prerequisites

AUR operations use a working `paru` or `yay`. If neither works, the installer attempts to install or repair repository `paru`, then `paru-bin` as the next candidate. `paru-bin` is a prebuilt alternative providing the same helper command. These helpers resolve and build selected AUR packages; they are conditional infrastructure outside the manifest above. The initial package upgrade also ensures `base-devel` and `git` are present.

CachyOS and an already installed, running Hyprland are prerequisites. Hyprland receives normal repository updates during the full-system upgrade but is not a manifest-selected installation target. Optional kernel selection is separate from the application manifest; see [RC kernel support](#latest-cachyos-release-candidate-kernel).
