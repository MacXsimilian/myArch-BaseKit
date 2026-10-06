# Contributing

## Development setup

Use Go 1.23 or newer. The project uses the Go standard library and has no external Go module dependencies. GNU Make is optional; the README includes direct Go commands.

```bash
make build
./bin/myarch-buildkit plan
make check
```

Build products belong in `bin/` and are ignored by Git. Run commands from the repository root unless a guide says otherwise.

## Code organization

All application code is in `cmd/myarch-buildkit/`:

| Files | Responsibility |
| --- | --- |
| `main.go`, `settings.go`, `types.go` | CLI dispatch, settings, shared types |
| `plan.go`, `plan_names.go`, `check_output.go`, `run_output.go` | Preview and terminal output |
| `packages.go`, `defaults.go`, `integrations.go`, `cleanup.go` | Packages, application defaults, shell/container integrations, cleanup |
| `desktop.go`, `laptop.go`, `greeter.go`, `plugins.go`, `launchers.go`, `helpers.go` | Desktop, laptop preferences, login, plugins, and helpers |
| `staging.go`, `storage.go`, `report_store.go`, `system.go` | Pending configuration, backups, reports, privileged operations |
| `runner.go`, `parser.go` | Command execution and configuration parsing |
| `*_test.go` | Tests next to the implementation |

Keep changes in the relevant files. Introduce a separate package when it has a clear responsibility and API; shared implementation details do not need to become public just to create more folders.

## Validation

Format changed Go files with `gofmt`, or use `make fmt` for the command package. Run `make test` and `make vet` before submitting changes. For a focused test:

```bash
go test ./cmd/myarch-buildkit -run '^TestDisabledContainersSkipOptionalChecksButKeepCorePodman$' -v
```

That test uses an isolated command lookup path to verify that disabling optional container checks preserves the required Core Podman version check. Prefer assertions about behavior, preserved user data, and failure handling over keyword lists or fixed package counts. Five privileged-operation tests require root and skip in ordinary non-root runs; they need an isolated privileged test environment.

Unit tests do not replace validation of the login session, DMS, input devices, display scaling, and suspend/resume on supported hardware. Follow the [user guide](docs/user-guide.md) for target-machine checks.

GitHub Actions runs the build and validation as independent jobs on pushes, pull requests, and manual requests. The build uses stable Go and `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 make checksum`, smoke-tests the CLI, and uploads a tar archive. The validation job runs `make vet` and `make test` without suppressing failures. An available artifact does not imply that the test job passed.

## Versioning

Use tags in the form `vMAJOR.MINOR.PATCH` (without a dot after `v`). Keep `BuildID` in `cmd/myarch-buildkit/version.go` and the user guide's documented version aligned with the intended release. Data-format schema versions are independent of the application version.

Build a specific version locally with:

```bash
make checksum VERSION=v0.1.1
./bin/myarch-buildkit --version
```

After committing the release changes, create and push its tag:

```bash
git tag v0.1.1
git push origin v0.1.1
```

The workflow validates the tag and embeds it through Go linker flags. It still uploads an Actions artifact; it does not publish a GitHub Release. Never reuse a published version for different source code.


## Documentation and generated files

Keep the root README focused on the project and quick start. Update `docs/user-guide.md` when commands, settings, paths, recovery behavior, or supported platforms change.

Use `make checksum` to build the executable and generate its matching checksum in `bin/`. Distribute these together; checksums for local builds do not belong in the source tree.

Before committing, inspect `git status` and the staged diff. Editor settings, AI assistant files, personal environment files, credentials, binaries, and installer reports are excluded by `.gitignore`.
