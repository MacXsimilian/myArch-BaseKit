# Contributing

Changes to myArch-BaseKit should make workstation setup easier to understand, verify, and recover. Start with the [user guide](docs/user-guide.md) for the current behavior and support boundary, then keep your change focused on one problem.

[Development workflow](#development-workflow) · [Validation](#validation) · [Code organization](#code-organization) · [Target acceptance](#target-acceptance-tests) · [Releases](#release-procedure)

## Development workflow

Use Go 1.23 or newer for development. The application uses only the Go standard library. Build scripts use Bash, GNU Make, Git, and standard GNU command-line utilities. Race tests need a working C toolchain. Release builds use the exact Go version recorded in [`.go-version`](.go-version).

From the repository root:

```bash
make help
make build
./bin/myarch-buildkit --version
./bin/myarch-buildkit plan
make check
```

`plan`, `--help`, `--version`, and `--settings-template` can be exercised without applying workstation changes. Ordinary tests use isolated fixtures. Run actual setup only in a disposable supported VM or on an explicitly prepared target.

For a contribution:

1. Create a branch and reproduce the problem or describe the intended behavior.
2. Change the smallest relevant area. Preserve user data, explicit package review, and recovery behavior.
3. Add a behavioral regression test when the change warrants one, then run the relevant checks below.
4. Update the user guide for changed commands, settings, paths, or supported behavior.
5. Inspect `git diff --check`, `git status`, and the staged diff before opening a pull request.

A useful pull request explains the problem, resulting behavior, validation performed, and any remaining target-machine checks. For bug reports, include the build from `--version`, the exact command, expected/observed behavior, and relevant redacted report fields. Avoid attaching credentials, kubeconfigs, private paths, or complete personal configuration.

Generated executables, archives, local settings, and run reports belong outside the source diff. `bin/` and `dist/` are ignored. Use `GO=/absolute/path/to/go` to select a compiler; `gofmt` follows that compiler unless `GOFMT` is overridden.

## Validation

| Command | Checks |
| --- | --- |
| `make fmt` | Formats application Go sources in place |
| `make check` | Formatting, ordinary Go tests, and `go vet` |
| `make test-race` | Go tests with the race detector |
| `make lint` | GitHub workflows with actionlint, and shell scripts with ShellCheck |
| `make automation-test` | Offline packaging/release regression tests; requires Python 3.9+ |
| `make package` | Builds the Linux x86-64 distribution archive |
| `make package-check` | Verifies and smoke-tests the existing archive; requires Python 3 |

Install actionlint and ShellCheck separately before `make lint`; the [workflow](.github/workflows/build.yml) records the actionlint version used by CI. Use Python 3.9 or newer for automation tests and package verification; it is not an application runtime dependency.

Run a focused application test while developing, then the checks appropriate to the final change:

```bash
go test ./cmd/myarch-buildkit \
  -run '^TestDisabledContainersSkipOptionalChecksButKeepCorePodman$' -v
```

Prefer assertions about behavior, preserved user data, and failure handling over keyword lists or fixed package counts. Run tests with fixtures instead of invoking package managers or modifying host services. Privileged-operation tests skip in ordinary runs; CI runs them separately as root against temporary directories on an ephemeral runner.

Noninteractive commands run in separate process groups so cancellation can stop descendants. Interactive commands retain the foreground terminal group for prompts and shell job control; programmatic cancellation signals the wrapper and relies on it to forward termination. Detached helpers intentionally outlive their caller. Preserve these different contracts when changing the runner.

### CI gates

The [Build and test workflow](.github/workflows/build.yml) builds with the pinned release compiler, tests the minimum Go version and current stable Go, runs race/static/format checks, exercises privileged recovery in temporary directories, lints automation, and verifies the packaged CLI. Third-party actions are pinned to commit hashes and tracked by Dependabot.

Build and test jobs run independently. A development artifact can exist while another job has failed; review the whole run. Publication requires all release gates. CI does not exercise a real graphical login, reboot, package transaction, or hardware lifecycle.

## Code organization

Application code and its adjacent tests live in `cmd/myarch-buildkit/`:

| Files | Responsibility |
| --- | --- |
| `main.go`, `settings.go`, `types.go`, `version.go`, `executable_identity.go` | CLI dispatch, settings, shared types, version, and executable identity |
| `plan.go`, `plan_names.go`, `check_output.go`, `run_output.go` | Preview and terminal output |
| `packages.go`, `kernel.go`, `kernel_images.go` | Package selection, kernel lifecycle, and system image verification |
| `defaults.go`, `default_fonts.go`, `integrations.go`, `cleanup.go` | Application defaults, shell/container integrations, and cleanup |
| `desktop.go`, `laptop.go`, `greeter.go`, `plugins.go`, `helpers.go` | Desktop profile, laptop settings, login, plugins, and user helpers |
| `dms_compat.go`, `dms_schema.go`, `testdata/` | DMS compatibility inspection and source-pinned fixtures |
| `staging.go`, `storage.go`, `recovery_records.go`, `report_store.go`, `system.go` | Pending configuration, backups, recovery, reports, and privileged operations |
| `runner.go`, `parser.go` | Subprocess execution and configuration parsing |
| `*_test.go` | Unit, subprocess, privileged, and opt-in target acceptance tests |

Build and publication scripts live in `scripts/`, their offline tests in `scripts/tests/`, and CI in `.github/workflows/`. Keep the README focused on orientation and first use; the user guide owns operational detail.

Introduce a separate Go package when it has a clear responsibility and API. Shared implementation details do not need to become public solely to add another directory. Preserve fixture origin and license metadata when changing upstream snapshots.

## Target acceptance tests

The normal suite includes real subprocess tests for package-stage execution, cancellation and a foreground terminal, plus source-pinned DMS schema fixtures. Kernel lifecycle tests use simulated transactions; a separate production-reader test exercises protected root-owned files as a regular user. That reader test skips explicitly when a container maps system ownership away from root. Privileged recovery tests use temporary filesystems and run separately in CI. These checks do not establish that a real login or reboot succeeds.

Use a disposable CachyOS x86-64 VM with UEFI/systemd-boot, an existing Hyprland session, and a snapshot before setup. Boot a known working stable/LTS fallback first. The selected stable/RC target must be a different kernel package. Run setup as the ordinary desktop user and retain its run report. Then execute the read-only acceptance gate from the repository:

```bash
MYARCH_TARGET_TEST=1 MYARCH_TARGET_PHASE=staged \
  MYARCH_TARGET_SETTINGS=/absolute/path/to/run/report.json \
  go test ./cmd/myarch-buildkit -run '^TestTargetSessionAndBootContracts$' -count=1 -v
```

Save work, log out, choose the myarch-buildkit session, and rerun with `MYARCH_TARGET_PHASE=active`. The gate checks actual tools/defaults, pending files or the live desktop, DMS/plugins, laptop settings, and configured kernel images. It never installs packages, switches boot entries, restores files or reboots. An incomplete result fails the test; resolve it before advancing.

For kernel acceptance, keep the opted-in settings/report and run the gate after each deliberate boot step:

| Step in the disposable VM | Gate environment | Required result |
| --- | --- | --- |
| Apply the packages stage and reboot the fallback | `MYARCH_TARGET_BOOT=fallback` | Running fallback, distinct target, target not the default |
| Request `--kernel-boot once`, then reboot | `MYARCH_TARGET_BOOT=trial` | Running target, fallback still the default |
| Test the session, request `--kernel-boot promote`, then reboot | `MYARCH_TARGET_BOOT=promoted` | Running target and target is the default |

Use `MYARCH_TARGET_PHASE=active` for these boot checks. Capture `bootctl status`, `bootctl list --json=short`, `uname -r`, the JSON check report and the test output after every reboot. On a separate snapshot with only the target kernel installed, opted-in package/trial operations must refuse before mutation. Exercise restore desktop→greeter and greeter→desktop on separate snapshots; repeat a restore interrupted between file replacement and journal persistence, and verify manual edits remain protected after partial restore. Revert snapshots between scenarios. VM testing does not replace target-hardware graphics, input, display hotplug, suspend/resume or storage checks.

## Build artifacts and reproducibility

```bash
make package
make package-check
```

Packaging always builds **Linux amd64 with CGO disabled**, regardless of host `GOOS`, `GOARCH`, or `CGO_ENABLED` values. It writes:

| File | Purpose |
| --- | --- |
| `dist/myarch-buildkit-linux-x86_64.tar.gz` | Executable, license, documentation, inner checksums, and build metadata |
| `dist/SHA256SUMS` | SHA256 of the distribution archive |

Inside the archive, `BUILD-INFO` records the version, revision, dirty state, compiler, platform, and source epoch. The inner `SHA256SUMS` covers the executable, documentation, license, and metadata. Packaging builds into a temporary directory and leaves `bin/` unchanged. `make build` and `make checksum` separately produce the local executable and its checksum in `bin/`.

Archive entry order and metadata are normalized. `SOURCE_DATE_EPOCH` defaults to the Git commit timestamp and can be supplied explicitly. Reproduction requires the same source contents, full Git revision, dirty state, build version, compiler, epoch, and GNU tar/gzip versions; compression-tool versions can affect bytes. Archive normalization does not make workstation package installation reproducible.

`make package-check` verifies the archive and inner checksums and smoke-tests the extracted command. It does not apply any setup stages. On a host other than Linux x86-64 it verifies structure and checksums, then explicitly skips execution; complete the smoke test on Linux x86-64 before release.

## Versioning

The source version is `BuildID` in [`version.go`](cmd/myarch-buildkit/version.go). Application versions and settings/report schema versions are independent.

| Build | Version identity |
| --- | --- |
| Direct `go build` | Source `BuildID` |
| `make build` or `make package`, without `VERSION` | Development identity derived from the source version and short Git revision, plus a dirty marker when applicable |
| Release with `VERSION=vMAJOR.MINOR.PATCH` | Exact requested version, validated against source `BuildID` |

Stable release tags use `vMAJOR.MINOR.PATCH`, without leading zeroes. The publishing workflow does not publish prerelease tags. The 0.x series is initial development; describe compatibility changes in release notes instead of implying a stable 1.x contract.

Update `.go-version` deliberately when changing the release compiler. Development and minimum-version testing remain distinct from that release pin.

## Release procedure

Publication is triggered by a matching stable version-tag push. Local Make targets never create tags, push commits, or publish releases.

1. Update `BuildID` and user-facing documentation on a branch. Review and merge the intended release commit into `master`; wait for all checks. Complete the [target acceptance](#target-acceptance-tests) work needed for changed desktop, kernel, or recovery behavior, and record remaining limits in release notes.
2. Use a clean checkout of that commit and the exact compiler in `.go-version`. Install the lint/test prerequisites, then validate and package it. Replace the example version with the intended release:

   ```bash
   make release VERSION=v0.3.0
   (cd dist && sha256sum -c SHA256SUMS)
   tar -xOf dist/myarch-buildkit-linux-x86_64.tar.gz BUILD-INFO
   ```

   `make release` runs formatting and vet checks, the race-enabled test suite, lint, automation tests, packaging, and package verification. `make release-check VERSION=v0.3.0` checks version syntax and its match with the source separately. No tag needs to exist yet. Local packaging permits a dirty checkout for testing and records it in `BUILD-INFO`; maintainers must use a clean checkout for publication.

3. Create an annotated tag on the validated commit and push that tag:

   ```bash
   git tag -a v0.3.0 -m 'Release v0.3.0'
   git push origin v0.3.0
   ```

4. Watch the tag's **Build and test** run. Publication waits for every required build, test, and lint job. The publishing job verifies that run's artifact, creates or resumes a draft with the expected assets, then publishes it. Only that job receives `contents: write`; it uses the built-in `GITHUB_TOKEN`.
5. Review the published notes. Download both release assets, verify their checksums, and confirm the executable version and `BUILD-INFO` match the tag and intended commit.

### Failed or repeated publication

A failed validation gate prevents publication. For a transient runner failure, rerun the failed jobs. Publication can resume a matching draft and upload missing assets. A published release whose expected assets already match is a successful no-op. Any conflicting existing content is refused: the workflow does not overwrite or delete release assets.

Investigate a conflict before taking further action. If the tagged source must change, prepare a new version and tag; do not move a published tag. Matching names alone are not sufficient to reuse an asset—its content must match.

Manual workflow runs produce unpublished workflow artifacts. Selecting a stable tag retains that exact tag version in the artifact, but does not publish a release. Tags pushed by another workflow using `GITHUB_TOKEN` do not trigger this workflow; push release tags through your authenticated Git client.

### Repository settings

Maintainers should require the build, both Go test jobs, and automation lint checks before merging into `master`. Configure branch/tag protection and Actions permissions in GitHub; repository files do not apply those settings. Dependabot tracks pinned action updates. Review changes to the release compiler, action commits, and actionlint version deliberately.
