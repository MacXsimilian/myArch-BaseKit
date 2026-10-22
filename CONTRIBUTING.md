# Contributing

[Home](README.md) · [User guide](docs/user-guide.md) · [Packages](docs/packages.md)

BaseKit is a personal CachyOS workstation recipe. Keep each operation explicit and reviewable: select packages, render preferences, validate, back up, apply and check. Prefer the upstream Dux CLI for plugin installation and greeter setup. Avoid adding staging queues, automatic rollback or general-purpose desktop-management frameworks.

## Build and checks

Use GNU Make and the Go version in [`.go-version`](.go-version). The program uses the Go standard library. Race tests require a working C toolchain.

```bash
make build
./bin/myarch-buildkit plan
make check
make test-race
```

| Target | Purpose |
| --- | --- |
| `build` | Build `bin/myarch-buildkit`; also the default target |
| `fmt` | Format Go files |
| `fmt-check` | Fail if Go files need formatting |
| `test` | Run Go tests |
| `vet` | Run `go vet` |
| `check` | Formatting check, tests and vet |
| `test-race` | Tests with the race detector |
| `package` | Build a Linux x86-64 archive and checksum locally |

Override the compiler with `GO=/absolute/path/to/go`; `GOFMT` can also be overridden. CI runs check, race tests and build with the pinned compiler. It does not apply desktop settings or publish releases.

When Niri is available, exercise the actual parser:

```bash
BASEKIT_NIRI_BINARY=/usr/bin/niri go test ./cmd/myarch-buildkit -run TestNativeNiriProfile -v
```

This validates temporary files, including rejection of invalid configuration; it does not launch a compositor. The test skips when the environment variable is unset.

## Source map

All application code lives in `cmd/myarch-buildkit`.

| Files | Responsibility |
| --- | --- |
| `app.go`, `executor.go` | CLI, settings, orchestration, platform/session checks and command execution |
| `catalog.go`, `packages.txt` | Offline selection, DMS major-specific package choice, repository/AUR resolution |
| `profile.go`, `profiles/` | Niri rendering, Dux preferences and shell scripts |
| `terminal.go`, `containers.go` | Terminal integration and container configuration/checks |
| `plugins.go`, `plugin_profile.go` | Install/inspect plugins and merge activation/widget placement |
| `shell_services.go`, `desktop.go` | Service readiness and competing-startup detection |
| `greeter.go`, `cleanup.go` | Upstream greeter delegation and constrained package cleanup |
| `backup.go` | Original-content snapshots, backup manifest and file writes |
| `version.go` | Executable version |

## Change rules

- Keep plan free of mutations and network requests. Cleanup preview may read the local package database.
- Use `Runner` fakes for command behavior; ordinary tests must not install/remove packages, enable services or edit the developer's desktop.
- Keep original snapshots when merging files. Reject malformed input, unsafe paths and concurrent edits instead of guessing.
- Preserve unrelated JSON preferences and existing widget placements. Match upstream schemas before introducing new keys.
- Use exact package allowlists for removal, recheck replacements, and preserve pacman's dependency checks and interactive confirmation.
- Keep privileged system ownership with the upstream greeter installer; user-file backups must not be described as system rollback.
- Update the package reference when the catalogue changes, and document changed settings/defaults in the setup guide.

Test observable behavior: preservation of personal settings, failed commands stopping later work, missing replacements blocking removal, repeatable merges and recoverable backups. Avoid tests that merely reproduce implementation details.

## Manual acceptance

Use a disposable CachyOS VM or an explicitly prepared test machine. Take a snapshot before package and greeter changes.

1. Build/package from the intended checkout. Preview settings for the actual display and keyboard.
2. Apply from a logged-out text console as a regular user. Review package and greeter prompts.
3. Verify backup originals, manifest paths and records for newly created files.
4. Log into Niri; check Ghostty selection, Dux startup, plugin activation, bar placement and keyboard/display settings.
5. Run BaseKit check and upstream Dux diagnostics. Exercise calendar, VPN controls, Bluetooth/audio and rootless containers as available.
6. Verify greeter login before testing Noctalia cleanup. Confirm dependency refusal and preservation of Niri/Quickshell.
7. Rerun apply and inspect preservation of unrelated configuration. Remember that package upgrades and greeter prompts can recur.
8. Restore selected user files from the manifest while logged out. Test system-level recovery separately.

Report exactly what was exercised. Unit tests and native parsing do not establish that a real login, hardware, current AUR build or upstream greeter succeeds.

## Packaging

```bash
make package
(cd dist && sha256sum -c SHA256SUMS)
tar -tzf dist/myarch-buildkit-linux-x86_64.tar.gz
```

The archive includes the Linux x86-64 executable built with CGO disabled, README, contributing guide, all user documentation and license. Source-file links in the docs refer to the repository checkout; executable users can follow the included operational guides without a Go installation.

Packaging does not tag, upload or publish. Before a requested release, update the version, validate the intended revision, inspect the archive and publish the archive/checksum through the approved workflow. Checksums establish integrity, not publisher identity.
