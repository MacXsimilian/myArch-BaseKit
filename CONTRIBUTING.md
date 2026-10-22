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

GitHub Actions builds the Linux x86-64 archive with stable Go independently of validation. Tests and vet run on both Go 1.23 (the minimum in `go.mod`) and stable Go, with the race detector enabled. Stable Go also checks formatting and runs the five privileged-operation tests as root on an ephemeral runner, using temporary directories. Workflow and shell scripts are checked with actionlint and ShellCheck. An available development artifact does not imply that all checks passed.

Use `make package` to produce the same archive locally. Release builds use `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`. Generated files go in `bin/` and `dist/`; the archive and its external checksum are in `dist/`.

## Versioning

Use stable tags in the form `vMAJOR.MINOR.PATCH` (without leading zeroes). Tags must exactly match `BuildID` in `cmd/myarch-buildkit/version.go`; keep README and user guide versions aligned too. Schema versions are independent of application versions. Prerelease tags are not currently supported by the publishing workflow.

## Release procedure

1. Prepare the intended version and documentation on a branch, review the changes, and merge to `master`. Wait for all checks on that commit to pass. Run target-machine validation described in the user guide; CI does not exercise a real desktop session.
2. From a clean checkout of that exact commit, validate and build the release (replace `v0.2.0` with the intended version):

   ```bash
   make check
   make release-check VERSION=v0.2.0
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 make package VERSION=v0.2.0
   ./bin/myarch-buildkit --version
   (cd dist && sha256sum -c SHA256SUMS)
   ```

3. Create an annotated tag and push that tag only:

   ```bash
   git tag -a v0.2.0 -m 'Release v0.2.0'
   git push origin v0.2.0
   ```

4. Watch the tag's **Build and test** run. The build validates the version, embeds the tag, extracts and smoke-tests the packaged CLI, and uploads the archive and checksums. Publication waits for every build, test, and lint job to pass. The release job downloads that run's artifact, verifies its checksum, creates a draft with generated notes and both assets, then publishes it. Only this job receives `contents: write`; it uses the built-in `GITHUB_TOKEN`, with no personal token or repository secret required.
5. Check the published release notes and download both assets from GitHub Releases. Verify and extract them using the README commands, then confirm `--version` matches the tag.

A failed validation job prevents publication. Fix the source and use a new version/tag if the tagged source needs to change; never move a published tag. For a transient runner failure, rerun failed jobs. If publication created a draft before failing, inspect that draft and its assets: either finish publishing it manually or delete only the unpublished draft (keep the tag) before rerunning the release job. The workflow deliberately refuses to overwrite an existing release. A rerun after successful publication will fail at release creation without replacing assets.

Manual workflow runs produce development artifacts, not releases, even when a tag is selected. Tags pushed by another workflow using `GITHUB_TOKEN` do not trigger this workflow; push release tags with your own authenticated Git client. No release is created until a matching version tag is pushed after this workflow is committed.

Repository maintainers should require the build, both Go test jobs, and automation lint checks before merging into `master`. Branch/tag protection and repository Actions settings must be configured in GitHub; they are not applied by these files. Dependabot checks GitHub Actions versions weekly. Update the pinned actionlint version deliberately when a new release is available.

## Documentation and generated files

Keep the root README focused on the project and quick start. Update `docs/user-guide.md` when commands, settings, paths, recovery behavior, or supported platforms change.

Use `make checksum` to build the executable and generate its matching checksum in `bin/`. Distribute these together; checksums for local builds do not belong in the source tree.

Before committing, inspect `git status` and the staged diff. Editor settings, AI assistant files, personal environment files, credentials, binaries, and installer reports are excluded by `.gitignore`.
