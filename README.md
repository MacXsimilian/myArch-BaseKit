# myArch-BaseKit

An opinionated workstation setup tool for development, DevOps, SRE, and platform engineering. The `myarch-buildkit` command configures packages, application defaults, shell integrations, containers, and a Hyprland desktop with Dux Shell (formerly Dank Material Shell).

**Supported base:** native CachyOS on Linux x86-64, with Hyprland already installed and running. This tool configures an existing system; it does not install the operating system.

## Build and preview

Install Go 1.23 or newer, then run from the repository root:

```bash
go build -o bin/myarch-buildkit ./cmd/myarch-buildkit
./bin/myarch-buildkit --version
./bin/myarch-buildkit plan
```

The preview does not change the system. Read the [user guide](docs/user-guide.md) before applying the plan, especially the default Hungarian internal keyboard layout, 4/3 display scaling, and next-login desktop staging.

With GNU Make installed, `make build` builds the executable and `make checksum` also generates `bin/SHA256SUMS`. Verify it with `cd bin && sha256sum -c SHA256SUMS`.

## Version policy

Package setup upgrades the enabled repositories and selected AUR packages to their available versions. Desktop setup refreshes all five plugins from their upstream default branches, records exact commits, and stages updates with backups for the next login. Plugin preferences are preserved.

Dux Shell 2.0 is supported, including its `dms/` data directories and new dock settings. Newer shell releases are checked against their installed schemas or exact upstream release sources. Hyprland configuration must pass native validation; there is no fixed release-number ceiling. See the [update guide](docs/user-guide.md#updates-and-cleanup) for migration and recovery details.

Kernel selection is opt-in through `kernel_channel: "rc"` or `"stable"` in settings. It retains a validated stable/LTS fallback and supports explicit trial boot and promotion on systemd-boot. Other boot layouts, Secure Boot, and DKMS need manual validation. See the [kernel guide](docs/user-guide.md#latest-cachyos-release-candidate-kernel) for requirements and commands. VM and hardware boot testing remain outstanding.

## Download a release

Download `myarch-buildkit-linux-x86_64.tar.gz` and `SHA256SUMS` from [GitHub Releases](https://github.com/MacXsimilian/myArch-BaseKit/releases). In the download directory, verify the archive before extracting it into a separate directory:

```bash
sha256sum -c SHA256SUMS
mkdir -p myarch-buildkit-release
tar -xzf myarch-buildkit-linux-x86_64.tar.gz -C myarch-buildkit-release
cd myarch-buildkit-release
sha256sum -c SHA256SUMS
./myarch-buildkit --version
./myarch-buildkit plan
```

The outer checksum verifies the archive; the checksum inside verifies the executable. The archive includes the license, README, contributing guide, and user guide. Builds target Linux x86-64 with CGO disabled to avoid a host libc dependency.

Stable version tags publish a release only after the build, tests on minimum and stable Go, privileged-operation tests, and automation lint checks pass. See [the release procedure](CONTRIBUTING.md#release-procedure).

## Download a development build

The [Build and test workflow](https://github.com/MacXsimilian/myArch-BaseKit/actions/workflows/build.yml) also runs on branch pushes, pull requests, and manual requests. Open a run and download **myarch-buildkit-linux-x86_64** from **Artifacts**. Unzip it, then use the verification and extraction commands above. Artifacts expire after 30 days.

Builds run independently of tests, so a development artifact can be available even when another check failed. Check the entire run before using it. Branch and manual runs do not publish releases.

## Versioning

The program uses [semantic versioning](https://semver.org/), displayed as `vMAJOR.MINOR.PATCH`, currently **v0.3.0**. Use `v0.3.1` for a compatible bug fix and `v0.4.0` for the next feature release. The `0.x` series is initial development; stable compatibility starts at `v1.0.0`.

The default version is defined in `cmd/myarch-buildkit/version.go`. A GitHub Actions build triggered by a tag such as `v0.3.0` embeds that tag in the executable and its run reports. Branch, pull-request, and ordinary local builds use the source version. Version numbers are chosen deliberately, not incremented on every build.

## Documentation

- [User guide](docs/user-guide.md): setup, settings, commands, verification, and recovery.
- [Package reference](docs/user-guide.md#9-package-reference): every package, its purpose, commands, optional group, and repository/AUR alternatives.
- [Contributing](CONTRIBUTING.md): development workflow, code organization, and validation.
- [License](LICENSE): MIT.

## Repository layout

```text
.
├── .github/
│   └── workflows/build.yml # Builds, validation, and tagged releases
├── cmd/
│   └── myarch-buildkit/   # Go command and adjacent unit tests
├── docs/
│   └── user-guide.md     # Full setup and operation guide
├── bin/                 # Generated executable and checksum; ignored by Git
├── .editorconfig        # Shared formatting conventions
├── .gitignore           # Local settings, secrets, and generated output
├── CONTRIBUTING.md
├── LICENSE
├── Makefile             # Build, test, vet, format, and checksum targets
├── README.md
└── go.mod
```

Tests live beside the code they exercise, following Go conventions. The command remains one package because its setup stages share internal types, state, and recovery logic.

## Validation

```bash
go test ./...
go vet ./...
```

Equivalent Make targets are `make test`, `make vet`, and `make check` (formatting, tests, and vet).

The non-root suite skips five privileged-operation tests; CI runs those separately as root in temporary directories. Automated checks do not establish target-machine readiness; see the user guide for hardware validation limits.
