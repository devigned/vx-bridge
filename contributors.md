# Contributing

Help make VX Bridge easier to use and maintain. Documentation, useful bug
reports, regression tests and small fixes are all welcome. You don't need a
launch monitor to improve the guides or run the automated tests.

## Pick a useful first contribution

For a confusing setup step, suggest the wording you wish you'd seen. For a bug,
describe what you did, what you expected, what happened, and the bridge/Labs/game
versions involved. Include the visible tray error and selected game when relevant.

Check the [FAQ](docs/FAQ.md) and
[existing issues](https://github.com/devigned/vx-bridge/issues)
before opening a report. This is the public source repository; there is no
published playing release yet. Please don't attach raw captures, real shot data,
device identifiers, account details or full vendor logs.

For code changes, choose a small issue or discuss the intended behavior first.
Changes to delivery, game readiness, shutdown or installation need particular
care. Read the relevant [technical guide](docs/technical/README.md) first:
sending data successfully does not prove the game displayed a shot.

## Find your way through the source

Start with the runtime, not installer qualification or historical probes:

1. Read [`cmd/vxbridge/main.go`](cmd/vxbridge/main.go): `run` validates the entry
   mode and admits the host; `host` wires listeners, native identity, desktop
   workers and guarded cleanup. [`window.go`](cmd/vxbridge/window.go) keeps the
   native window lifecycle on one OS thread.
2. Follow [`internal/bridge/bridge.go`](internal/bridge/bridge.go): `Run` owns
   state, `handle` applies worker events, and `offer`/`forward`/`acceptReceipt`
   separate shot eligibility, actual writes and target acceptance. Read
   [`io.go`](internal/bridge/io.go) for arrival tickets, frame deadlines and
   upstream write admission, then nearby regression tests for the edge cases.
3. Read [`internal/protocol`](internal/protocol/) for framing, envelope checks
   and byte-preserving club merging, then [`internal/destination`](internal/destination/)
   for explicit game profiles and saved selection. Raw JSON/numeric spelling is
   retained; a successful transport or target 200 is not game readiness.
4. Follow [`internal/desktop`](internal/desktop/) and
   [`internal/control`](internal/control/) back into `bridge.Controller`.
   [`internal/identity`](internal/identity/) owns native window/listener details,
   [`internal/managed`](internal/managed/) checks installed-copy admission,
   [`internal/startup`](internal/startup/) owns sign-in registration, and
   [`internal/diagnostics`](internal/diagnostics/) owns bounded sanitized logs.
   These responsibilities do not authorize forced source teardown or automatic repair.

| Area | Where to look |
| --- | --- |
| Application and offline regression tests | `cmd/`, `internal/`; tests live beside the code they exercise |
| Ordinary package building and offline log inspection | [`tools/dist`](tools/dist/), [`tools/diagnostics`](tools/diagnostics/); use the [command guide](docs/technical/COMMANDS.md) |
| Installer construction and exceptional servicing qualification | [`installer`](installer/), [`tools/msi/setup`](tools/msi/setup/), [`tools/msi/lifecycle`](tools/msi/lifecycle/); read the [installer manual](docs/technical/INSTALLER.md) before using these tools |
| Frozen Labs research, not a routine diagnostic | [`tools/research/labs-probe`](tools/research/labs-probe/) and the [research archive](docs/technical/research/README.md); keep the no-rerun boundary intact |

Package introductions and API contracts are available without running the app:

```sh
go doc ./internal/bridge
go doc ./internal/protocol Stream.Next
GOOS=windows GOARCH=amd64 go doc -all ./internal/identity
```

Read platform-specific implementations as well as the portable files. `_windows.go`
and `!windows` files are alternatives, not duplicate active implementations;
non-Windows window/control stubs do not qualify Windows behavior. The icon
generator is separately build-excluded source, not another runtime command.
Follow [Go's doc-comment guidance](https://go.dev/doc/comment) when changing an
API: document packages once and explain exported symbols, fields, ownership,
errors and meaningful defaults rather than merely repeating their names.

## Set up a development checkout

Use Git, Bash, GNU make 3.81 or newer, and the exact Go version in
[.go-version](.go-version). A native C compiler is needed for race tests, not the
Windows distribution. Windows contributors need GNU make and Bash, not `nmake`.

From the repository root:

```sh
make help
make check
```

`make check` checks formatting, runs tests and runs Go's static checks (`vet`).
It does not build a package, start the bridge, enable startup or publish anything.
Tests use simulated connections and private fixtures, not your simulator. See the
[command guide](docs/technical/COMMANDS.md) for platform prerequisites and the
[testing guide](docs/technical/TESTING.md) for targeted checks and race coverage.

## Build without installing or running

Commit your changes first. Packaging requires a clean checkout and a new absolute
output directory outside it; the output directory's parent must already exist.
Replace the example path with your own:

```sh
OUT='/absolute/path/to/artifacts/new-build' make package
revision="$(git rev-parse HEAD)"
ARCHIVE="/absolute/path/to/artifacts/new-build/vx-bridge-windows-amd64-$revision.zip" \
  make verify-package
```

This creates and inspects a Windows x64 application ZIP without running either
executable. It is a development package, not an installer or a tested playing
release. It does not include the native Setup/MSI pair. The
[build and package reference](docs/technical/COMMANDS.md#build-and-verify-a-package)
explains the two executable names, metadata and output rules.

Installer builds use a separate native/Windows toolchain and explicit commands
in the [installer manual](docs/technical/INSTALLER.md). An MSI alone is not the
user install route: the current package requires its matching `setup.exe`.

Don't copy a build over vendor files or onto a simulator just to see whether it
works. Real-equipment trials require separate owner approval, a reviewed artifact
and a shutdown plan. The [qualification guide](docs/technical/QUALIFICATION.md)
separates automated checks from actual compatibility evidence.

## Submit a focused change

All changes after the authorized initial bootstrap must go through a branch
and pull request. Never push directly to `main`, force-push it, bypass its
protections, or rewrite published history, even to repair CI.
Every commit you create must be cryptographically signed using your own
existing configured Git signing identity, never a shared repository-owner key.
Verify that signature before pushing;
author text or a co-author trailer is not enough. If signing is unavailable,
stop rather than disable it or substitute a key. See [AGENTS.md](AGENTS.md).

Keep changes small enough to review, add a regression test for changed behavior,
and update the guide affected by that behavior. Run the smallest relevant test
selection before the broader checks needed for your change. Documentation-only
changes don't need a full Go build; do check relative links, anchors and filenames.

User instructions belong in [docs](docs/README.md). Architecture, test methods,
release policy and implementation constraints belong in
[docs/technical](docs/technical/README.md). Reviewed qualification summaries
belong in the [qualification guide](docs/technical/QUALIFICATION.md), not the
user quickstart. Raw evidence stays outside Git.

User guides should explain the symptom, the action and the consequence in plain
language. Keep build history and implementation details out of the quick start.
Preserve historical observations, compatibility identities and negative results
in the technical evidence rather than rewriting them to match later successes.

Original contributions use the project's [MIT license](LICENSE). Review
[third-party notices](THIRD_PARTY_NOTICES.txt) before adding or reusing code.
Public source can inform an original implementation; it isn't permission to
copy vendor software or omit another project's attribution.

## Compatibility and release boundaries

**VX Bridge** is the display name. Preserve the Labs-facing `GSPconnect.exe` and
`ProTee United VX` window, distinct `vxbridge.exe` control tool, and existing
control-pipe, Windows startup and storage identities. Game selection stays
separate from startup registration. Ordinary application starts must not depend
on Windows Installer or attempt automatic repair.

Repair/reinstall and supported upgrades preserve user settings by default.
Successful full Uninstall removes the bridge's files, installer cache, settings,
logs and owned registrations, while leaving vendor data and independently saved
downloads alone. Failed maintenance must not start an unverified bridge or
promise that Windows restored the previous version. See the
[installer contract](docs/technical/INSTALLER.md) and
[recorded checks](docs/technical/QUALIFICATION.md) before changing these paths.

This public repository begins with a source-only bootstrap, without the prior
private Git history or investigation ledgers. Existing licenses, upstream
provenance and safety limits remain applicable. New source or artifacts need
their own compatibility evidence; earlier trials do not qualify them
automatically. No public binary release is implied by publishing source.
