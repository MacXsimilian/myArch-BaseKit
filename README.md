# myArch-BaseKit

A personal setup tool for **CachyOS, Niri and Dux Shell**. It installs the workstation packages, sets Ghostty as the preferred terminal, configures Podman and kind, activates five Dux plugins, and runs the Dux greeter installer.

Use it after installing CachyOS. It configures an existing system; it does not install the operating system, manage kernels or change the bootloader.

## Start here

1. Build the executable, or use a packaged executable you have already obtained.
2. Generate settings and edit the monitor, keyboard and scale.
3. Preview the changes.
4. Log out of the graphical desktop and apply from a text console.
5. Log into Niri and check the result.

From the repository root, with the compiler specified in [`.go-version`](.go-version) and GNU Make installed:

```bash
make build
./bin/myarch-buildkit --settings-template > workstation.json
```

Use a new filename: shell redirection overwrites an existing settings file. Edit `workstation.json` before continuing. The defaults are Hungarian keyboard layout, `eDP-1`, scale `4/3`, all four package groups, Dux 2 and greeter setup enabled.

```bash
./bin/myarch-buildkit plan --settings workstation.json
```

Save your work and **log out**, then switch to a text console such as `Ctrl+Alt+F3`. Sign in as your regular user and return to the repository directory:

```bash
./bin/myarch-buildkit apply --settings workstation.json
```

Review package prompts and choose **Niri** in the interactive greeter installer. After logging into Niri, open a new terminal:

```bash
./bin/myarch-buildkit check --settings workstation.json
```

**Apply replaces the selected Niri configuration after backing it up and performs a full package upgrade.** It may need sudo, network access and an installed `paru` or `yay` for AUR packages. The Dux 2 profile selects `dms-shell-git`, which can require compilation. Read the [setup guide](docs/user-guide.md) before applying to your main machine.

## What you get

| Area | Managed by this project |
| --- | --- |
| Desktop | Niri configuration, Dux startup, frame/dock/clock preferences |
| Terminal | Ghostty preference in Niri, Dux, terminal selectors and managed Bash/Zsh shells |
| Plugins | Docker Manager, Kubernetes, Emoji Launcher, Bongo Cat and ClipBoard+, activated with bar widgets where applicable |
| Shell features | Calendar, search, monitoring, media and theme dependencies; NetworkManager and Bluetooth service setup |
| Development | CLI tools, Kubernetes/IaC tooling, Podman Compose and kind's Podman provider |
| Login | Upstream Dux greeter setup, enabled by default |
| Cleanup | Optional removal of selected duplicate apps and Noctalia after replacement checks |

Calendar accounts, VPN profiles, Kubernetes credentials and Bongo Cat input permissions remain personal setup steps. Weather is disabled by the managed profile. Installing dependencies does not verify every feature on your hardware.

## Moving from Noctalia

You can start with CachyOS's Niri/Noctalia desktop. Apply this profile first, review competing startup entries, and verify Dux and its greeter before requesting cleanup. Cleanup never runs by default.

```bash
./bin/myarch-buildkit plan --settings workstation.json --cleanup-apps
# Log out again before applying:
./bin/myarch-buildkit apply --settings workstation.json --cleanup-apps
```

Noctalia cleanup requires the Dux greeter handoff, even if you chose to keep another login manager. It retains Niri and Quickshell, leaves personal Noctalia files in place, and uses pacman's normal confirmation and dependency checks. See [migration and cleanup](docs/user-guide.md#migrating-from-noctalia-and-cleaning-up-applications).

## Documentation

- [Setup and operation](docs/user-guide.md): prerequisites, settings, features, cleanup and backups.
- [Package reference](docs/packages.md): complete catalogue, groups and fallback packages.
- [Troubleshooting](docs/troubleshooting.md): failed setup, version mismatches, startup conflicts and recovery.
- [Contributing](CONTRIBUTING.md): implementation map, tests, manual acceptance and packaging.

Configuration backups are ordinary files with a manifest. There is no automatic rollback, and backups do not undo packages, services or greeter system changes. Tests cover the implementation and optional native Niri parsing; a real graphical login and hardware checks are still required.

[MIT license](LICENSE).
