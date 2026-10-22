# Troubleshooting and recovery

[Home](../README.md) · [Setup guide](user-guide.md) · [Packages](packages.md)

Read the final error and any printed backup location before rerunning apply. Package installation, user-file changes, services, greeter setup and cleanup happen sequentially. A failure in a later step does not undo earlier steps.

## Build or package installation

| Symptom | What to check |
| --- | --- |
| `go` is unavailable or too old | Use the compiler specified in `.go-version`, or an already-built executable. `make build GO=/absolute/path/to/go` selects a compiler. |
| AUR helper required | Install/configure `paru` or `yay` before apply. BaseKit does not install the helper itself. |
| Repository package unavailable | Check configured repositories and the failed package target. The package reference lists coded fallback names; a fallback is not a guarantee of current availability. |
| Package upgrade/build failed | Resolve the package-manager error before retrying. Do not bypass dependency checks. Configuration has not been written if failure occurred during the package step. |
| DMS version does not match settings | Compare `dms version` with `dms_config_dir`: `dms` for 2.x, `DankMaterialShell` for 1.x. Re-preview after correcting the setting. |

Dux 2 selects the upstream git shell package; its dependency or build requirements can change. This tool checks the installed major version before applying configuration. It does not promise that every future git revision will work with the managed profile.

## Apply refuses to continue

| Message or condition | Action |
| --- | --- |
| Run as a regular user | Invoke BaseKit without sudo. Individual privileged commands request it when needed. |
| Graphical desktop is running | Log out fully. Switching virtual terminals alone leaves the old session running. |
| Another setup process is running | Let that process finish; do not run two applies concurrently. |
| Older pending state detected | Use the matching old executable to complete/restore it. Do not delete the queue merely to silence the check. |
| Niri validation failed | Inspect the validation output and generated template; no user configuration is written before validation succeeds. |
| File changed since preview/during setup | Stop the program writing that file, inspect any backups, and rerun. Do not force an overwrite. |
| Symlink requires manual configuration | Decide how the file should be managed in your dotfile workflow. BaseKit refuses symlinked user configuration rather than replacing its target. |
| Invalid JSON or bar layout | Correct the existing file. Unknown preferences are preserved, but malformed objects cannot be merged safely. |
| No enabled visible regular bar | Enable a regular bar in Dux settings, then log out and rerun. Plugin widgets are not inserted into hidden/island-only layouts. |
| Nondefault XDG configuration/Niri path | Match the path rules in the settings guide; the tool does not silently target a different profile. |

## Dux, plugins and startup

If two bars, launchers or notification services appear, inspect the competing-startup report. BaseKit scans relevant user/system autostarts and enabled user services, respecting disabled entries and Niri desktop restrictions. It reports known competitors and duplicate DMS launchers. Custom scripts can still start additional processes outside that scan.

The managed Niri file already starts `dms run`. Do not independently enable a second DMS startup method. Noctalia package cleanup leaves personal configuration and independent startup entries in place; review those separately.

For plugins, use `check` to verify manifests and managed activation files, then inspect the running shell with `dms doctor`. Existing plugin code is preserved rather than updated by apply. A passing file check is not a plugin runtime test. Bongo Cat needs input permission for typing animation, and Kubernetes needs a valid kubeconfig.

If changes disappear after a rerun, check whether they are managed preferences. Niri is a complete replacement profile; selected Dux/plugin values are reapplied. Change the source profile and rebuild for persistent customization.

## Network, Bluetooth and containers

VPN controls need NetworkManager. If NM is inactive while networkd, ConnMan, standalone iwd, dhcpcd or netctl is active, service setup stops. Plan a deliberate network migration; BaseKit does not stop the existing backend. An already-active NetworkManager/iwd setup is retained. VPN profiles and protocol-specific plugins are separate from basic controls.

Bluetooth checks require an active service when an adapter exists. Pairing, radio blocks and device firmware remain hardware troubleshooting tasks.

For containers, inspect:

```bash
podman info
podman compose version
kind version
```

The profile expects rootless Podman with sufficient UID/GID mappings and Podman Compose as the provider. Configure subordinate IDs through your system administration workflow if missing; BaseKit does not allocate them. Container config/environment overrides can supersede the managed drop-in. For kind, use cgroup v2 and open a fresh terminal after login so `KIND_EXPERIMENTAL_PROVIDER=podman` is loaded.

## Greeter and Noctalia cleanup

Run `dms-greeter status` and inspect `/etc/greetd/config.toml`. The upstream installer can exit after a declined prompt, so a successful apply process alone is not proof that the greeter was switched.

Noctalia cleanup requires the direct Dux greeter configuration and enabled greetd. It intentionally refuses other working display managers or custom wrappers. Resolve the handoff, or keep Noctalia packages and handle removal manually after reviewing the dependencies.

Pacman may refuse removal because another desktop preset depends on Noctalia. BaseKit does not remove that preset automatically and never uses cascading removal. Noctalia candidates may be absent from an early preview until Dux, Niri and Quickshell have been installed.

If login breaks, return to a text console and inspect upstream greeter status/configuration before rebooting repeatedly. Upstream `dms-greeter uninstall` provides greeter removal and display-manager restoration prompts; review them for your machine. Its system backups are separate from BaseKit's [user-file backups](user-guide.md#backups-and-restoration).

## What successful checks establish

`make check`, race tests and optional native Niri validation exercise the code and configuration parser. `myarch-buildkit check` compares the installed system with the intended profile. Neither replaces testing an actual login, lock/unlock, suspend, audio, monitor hotplug, VPN connection or calendar account.

For an issue report, include the BaseKit version, selected settings with personal paths redacted if needed, the command that failed, relevant error output, and whether packages or configuration had already changed. Do not include credentials, VPN secrets or account tokens.
