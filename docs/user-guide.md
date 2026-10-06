# myArch-BaseKit: User Guide

Practical setup and daily-use reference\
Documented build: **v0.1.0**\
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

Package sources follow this order: keep a supported installed variant; otherwise try the listed repository package options from left to right, then the listed AUR fallbacks. Pacman uses the repositories enabled in `/etc/pacman.conf` in their configured priority order. The Repository packages column lists package names; exact repository names and availability are determined during apply.

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

The installer keeps your existing login shell. It does not switch you to Zsh or Fish. It adds supported Starship, mise, zoxide, fzf and direnv integrations to Bash and applicable existing Zsh/Fish configurations. Existing prompt frameworks are preserved and may need manual adjustment if they conflict. Editor-related shell defaults use `code --wait`.

Sign into Bitwarden, GitHub and other services yourself. Choose your Obsidian vault. Configure project runtimes and dependencies as needed. SMB support does not create NAS mounts, store credentials or install a Samba server. The manifest does not add VPN plugins or VPN profiles.

DMS clipboard history starts disabled. ClipBoard+ uses that same backend; `Super+V` opens its panel. Use `cachyos-clipboard instructions` for the clipboard check before enabling history.

### Included DMS plugins

The desktop stage downloads pinned upstream copies of these five plugins into its separate staging directory. Their files and enablement settings are applied before DMS starts at the next myarch-buildkit login. Existing installations, including plugins installed under another directory name, are preserved. The desktop report records the installed or staged version and the commit used for each new snapshot.

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

Installed supported alternatives such as `k6` or `k6-bin` are retained rather than replaced by an overlapping variant. The plan lists package options; the package profiles in `report.json` identify the exact choices. An unresolved update source is reported.

Cleanup removes Dolphin when Files is available; Alacritty when Ghostty and Hyprland checks pass, unless kept; and known duplicate GUI editors when VS Code is available. Dedicated notes apps are removed only when explicitly named. Normal package dependency checks can prevent removal.

Selected camera diagnostics, Avahi browsers, hardware-topology and Qt/GTK settings launchers are hidden using reversible per-user overrides. Their packages remain installed. Cleanup does not remove every unfamiliar app, purge orphan packages, or uninstall Noctalia itself.

<!-- pagebreak -->

## 6. Pending desktop configuration and reports

### Separate files and one startup method

The bundle lives in the internal run directory under `~/.local/state/myarch-buildkit/runs/RUN_ID/STAGE/`, or the corresponding directory under `XDG_STATE_HOME`, away from active DMS files and their file watchers. Nothing stops DMS or reloads Hyprland during staging. The login launcher checks every pending payload and non-JSON baseline before writing any configuration. It also reruns native Hyprland validation and checks the supported DMS version. DMS settings, session, clipboard and plugin settings JSON merge the intended properties into the latest existing objects; unrelated runtime properties are retained.

Backups capture the files immediately before they are replaced at login. Each write is journalled first. An error or interruption leaves the bundle blocked for explicit cancellation/restoration; the launcher does not start the compositor after a failed apply, retry the operation automatically, or launch a rescue shell.

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

This guide describes build `v0.1.0`. The latest non-root validation reports **191 passing top-level tests and 5 skipped privileged-operation tests**; `go vet` passes. Statement coverage is 71.6%. Privileged system operations and greeter setup remain lightly covered in this run. Tests cover CLI/settings validation, package selection and interactive review, readable check/apply output, JSON error reporting, unified reports and settings snapshots, separate staging, login ordering, payload/baseline conflicts, interrupted writes, DMS property merging, missing pending references, cleanup blocking, explicit and legacy restoration, source-preserving JSONC/MIME edits, personal appearance and symlink preservation, internal-only keyboard targeting, the three static laptop settings, staged fragment dependencies, plugin snapshots/preferences/loading, shell and container integrations, clipboard checks and screenshot handling. The [project README](../README.md) lists the Go test, vet and executable-build commands.

A target-machine run has applied the staged configuration before Hyprland startup and loaded all five DMS plugins. Natural-scrolling verification and suspend/resume issues remain unresolved; this is not a complete hardware validation. Test the myarch-buildkit session, DMS, its five plugins, built-in display scaling, keyboard layout, natural scrolling and suspend/resume on the target machine before treating setup as complete. Native verification requires supported installed Hyprland (hyprlang 0.54 or Lua 0.55/0.56) and the verified DMS schema (1.6.2, SettingsSpec 18 / SessionSpec 4).

### Maintaining the native Go version

Keep the compiled executable, this guide and the matching Go source project together with a known-working version and its run reports. The [project README](../README.md) documents the standard Go build and test commands. Build with Go 1.23 or newer; the compiled installer needs no Go or Python runtime on the target. Do not regenerate this Go version with the earlier Bash/Python build scripts.

Use `--help`, `--settings-template` and `plan` for the current command reference. `--version` prints the build identifier and executable SHA256. The run's `report.json` records the build, executed binary and its hash.

The single startup method follows [DMS's documented compositor-managed Hyprland launch](https://danklinux.com/docs/dankmaterialshell/managing#manual-launch). The installer still verifies the installed schema and configuration format rather than assuming arbitrary future versions are compatible.

The requested plugin IDs and repositories follow the [DMS registry](https://github.com/AvengeMedia/dms-plugin-registry). Plugins use DMS default styling; this build adds no theme setup.

From the repository root, build from source with Go 1.23 or newer:

```bash
go test ./...
go vet ./...
go build -o bin/myarch-buildkit ./cmd/myarch-buildkit
./bin/myarch-buildkit --version
```
