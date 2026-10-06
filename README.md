# myArch-BaseKit

An opinionated workstation setup tool for development, DevOps, SRE, and platform engineering. The `myarch-buildkit` command configures packages, application defaults, shell integrations, containers, and a Hyprland desktop with Dank Material Shell.

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

## Download an automatic build

The [Build and test workflow](https://github.com/MacXsimilian/myArch-BaseKit/actions/workflows/build.yml) builds a Linux x86-64 executable on every push and pull request. You can also start it from **Actions → Build and test → Run workflow** once the workflow is on the default branch.

Open a workflow run and download **myarch-buildkit-linux-x86_64** from its **Artifacts** section. Unzip the download, then extract and verify the bundle:

```bash
tar -xzf myarch-buildkit-linux-x86_64.tar.gz
sha256sum -c SHA256SUMS
./myarch-buildkit --version
./myarch-buildkit plan
```

The archive preserves executable permissions and includes the checksum, license, and user guide. Artifacts are retained for 30 days. Builds use the current stable Go release with CGO disabled to avoid a host libc dependency.

Tests and static analysis run in a separate job. A failed test marks the workflow unsuccessful, but the download remains available if the build job succeeds. Test failures are not suppressed. This workflow creates build artifacts, not GitHub Releases.

## Versioning

The program uses [semantic versioning](https://semver.org/), displayed as `vMAJOR.MINOR.PATCH`, starting at **v0.1.0**. Use `v0.1.1` for a compatible bug fix and `v0.2.0` for the next feature release. The `0.x` series is initial development; stable compatibility starts at `v1.0.0`.

The default version is defined in `cmd/myarch-buildkit/version.go`. A GitHub Actions build triggered by a tag such as `v0.1.1` embeds that tag in the executable and its run reports. Branch, pull-request, and ordinary local builds use the source version. Version numbers are chosen deliberately, not incremented on every build.

## Documentation

- [User guide](docs/user-guide.md): setup, settings, commands, verification, and recovery.
- [Contributing](CONTRIBUTING.md): development workflow, code organization, and validation.
- [License](LICENSE): MIT.

## Repository layout

```text
.
├── .github/
│   └── workflows/build.yml # Automatic builds and validation
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

Equivalent Make targets are `make test`, `make vet`, and `make check` (both checks).

The latest non-root validation has 191 passing top-level tests and 5 skipped privileged-operation tests. Statement coverage is 71.6%; this does not establish target-machine readiness. See the user guide for hardware validation limits.
