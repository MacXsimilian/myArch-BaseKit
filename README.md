# myArch-BaseKit

An opinionated workstation setup tool for **CachyOS + Hyprland**. The `myarch-buildkit` command installs development tools, configures everyday applications, and stages a Hyprland desktop with Dux Shell (DMS) for your next login.

Preview the changes, choose your settings, apply them, and keep a report with configuration backups and recovery commands.

[Get started](#quick-start) · [User guide](docs/user-guide.md) · [Package reference](docs/user-guide.md#package-reference) · [Recovery](docs/user-guide.md#troubleshooting-retry-and-restore) · [Contributing](CONTRIBUTING.md)

## What it sets up

| Area | Included |
| --- | --- |
| Desktop | Hyprland profile, DMS Frame Mode, top bar, bottom dock, and a dedicated login session |
| Plugins | Docker Manager, Kubernetes, Emoji & Unicode Launcher, Bongo Cat, and ClipBoard+ |
| Applications | Ghostty, GNOME Files, VS Code, Bitwarden, Obsidian, Firefox, and VLC |
| Development | Git tools, Go, uv, mise, Kubernetes, infrastructure tools, and modern CLI utilities |
| Containers | Podman, Compose, kind integration, and rootless prerequisite checks |
| Laptop | Internal keyboard layout, internal display scale, and natural scrolling |
| Recovery | Per-run reports, configuration backups, conflict-aware restore, and explicit retry commands |

Application defaults, packages, and shell integrations take effect during setup. Desktop, plugin, and laptop files are staged separately and applied before Hyprland starts at the next **myarch-buildkit** login. DankGreeter changes take effect at the next boot.

## Requirements and project status

- **Native CachyOS, Linux x86-64, and an already installed, working Hyprland session.** This project configures an existing system; it does not install the OS.
- An ordinary desktop user with `sudo` access, internet access, and working package repositories. Run setup from an interactive terminal inside Hyprland, without `sudo` before the command.
- Review the defaults: **Hungarian (`hu`) internal keyboard**, **4/3 internal display scale**, and **natural scrolling enabled**. All four package groups are enabled initially.

The project is in **0.x development**. Automated checks cover setup and recovery logic; this build still needs real CachyOS login, reboot, and hardware validation. Configuration restore retains package upgrades and is not a complete system rollback. Read the [support limits](docs/user-guide.md#support-and-validation-limits) before applying it to a workstation you depend on.

## Download a release

Download `myarch-buildkit-linux-x86_64.tar.gz` and `SHA256SUMS` from [GitHub Releases](https://github.com/MacXsimilian/myArch-BaseKit/releases). From the directory containing both downloads:

```bash
sha256sum -c SHA256SUMS
mkdir myarch-buildkit-release
tar -xzf myarch-buildkit-linux-x86_64.tar.gz -C myarch-buildkit-release
cd myarch-buildkit-release
sha256sum -c SHA256SUMS
./myarch-buildkit --version
```

The outer checksum verifies the archive; the inner checksums verify the executable, documentation, license, and `BUILD-INFO` metadata. Release builds use the compiler pinned in `.go-version` and need no Go, Python, or host libc runtime. Checksum verification checks file integrity; it is not a publisher signature.

### Build from source

Use Go 1.23 or newer and GNU Make. From a checkout:

```bash
make build
./bin/myarch-buildkit --version
cd bin
```

Without Make, use `go build -trimpath -o bin/myarch-buildkit ./cmd/myarch-buildkit`. The remaining examples assume your working directory contains the executable. See [Contributing](CONTRIBUTING.md) for tests, packaging, and release builds.

## Quick start

### 1. Review your settings and plan

Generate a settings file using a new filename, then edit the keyboard, scale, and package groups for your machine:

```bash
./myarch-buildkit --settings-template > myarch-buildkit-settings.json
./myarch-buildkit plan --settings myarch-buildkit-settings.json
```

`plan` makes no changes and performs no network requests. Add `--details` to inspect package alternatives. Keep this settings file for later checks and updates; [settings and optional flags](docs/user-guide.md#settings-and-optional-flags) explains every choice.

### 2. Apply, then log in

```bash
./myarch-buildkit apply --all --settings myarch-buildkit-settings.json
```

Review the interactive package prompts. **Exit code 3 means configuration is ready for login**, not a failed installation. Save your work, log out, and select **myarch-buildkit** in the login session chooser. An ordinary Hyprland login leaves the pending bundle untouched.

### 3. Check and finish

From a terminal in the new session, return to the executable's directory:

```bash
./myarch-buildkit check --settings myarch-buildkit-settings.json
```

After checks pass, finish cleanup:

```bash
./myarch-buildkit apply --stage verify --stage cleanup \
  --settings myarch-buildkit-settings.json
```

Bongo Cat typing animation requires a separate [input permission step](docs/user-guide.md#included-dms-plugins). Open a new terminal for shell changes. Save work before rebooting to test the greeter, then check the display, keyboard, audio, networking, and suspend/resume on your hardware.

Each apply run prints the path to `myarch-buildkit-runs/RUN_ID/report.json` under your working directory. Keep that report and the internal state/backups it references. If a step fails, use its printed retry or restore command; see [Troubleshooting, retry and restore](docs/user-guide.md#troubleshooting-retry-and-restore).

## Updates and reproducibility

The packages stage performs a full upgrade against enabled repositories and updates selected AUR packages. Desktop runs fetch the five plugins from their upstream default branches and record exact commits. Optional [`plugin_commits`](docs/user-guide.md#included-dms-plugins) settings pin individual plugins to those commits; they do not pin OS packages.

Kernel selection is opt-in: the default `kernel_channel: "unchanged"` keeps normal package upgrade behavior. The optional stable/RC workflow requires a distinct, verified stable/LTS fallback and a supported systemd-boot layout, with explicit trial boot and promotion. Read the [kernel guide](docs/user-guide.md#optional-cachyos-kernel-selection) before enabling it.

DMS compatibility is checked against installed or exact release schemas, and Hyprland configuration must pass native validation. A successful older setup does not establish compatibility with every future upstream version.

## Development builds and contributing

The [Build and test workflow](https://github.com/MacXsimilian/myArch-BaseKit/actions/workflows/build.yml) produces the **myarch-buildkit-linux-x86_64** artifact for branch pushes, pull requests, and manual runs. Unzip it and follow the release verification commands above. Artifacts expire after 30 days. Read the entire workflow result: an available development artifact can coexist with a failed test job.

To contribute, start with the [development workflow](CONTRIBUTING.md#development-workflow). Include a focused change, relevant validation, and documentation for any changed user behavior. For a bug report, include `--version`, the failing command, and relevant redacted report fields; never post credentials or private configuration.

The source version is defined in [`version.go`](cmd/myarch-buildkit/version.go). Make-built development binaries include the Git revision and a dirty marker when applicable; release binaries use the exact `vMAJOR.MINOR.PATCH` tag. Compatibility is still evolving during the 0.x series. Maintainers should follow the [release procedure](CONTRIBUTING.md#release-procedure).

## License

[MIT](LICENSE). Source fixtures retain their upstream license notices.
