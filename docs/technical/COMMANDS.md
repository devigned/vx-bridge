# Command reference

[Technical manual](README.md) / Commands

The executable reference applies to an already reviewed Windows x64 build.
Project commands run from the repository root. A build, test, or example is
not permission to install software or operate real equipment.

## vxbridge(1)

### NAME

vxbridge - inspect build identity or control the current user's VX Bridge host

### SYNOPSIS

```text
vxbridge.exe --version
vxbridge.exe --status
vxbridge.exe --quit
GSPconnect.exe [--desktop[=true|false]] [--target-port N]
```

### DESCRIPTION

`GSPconnect.exe` is the application-owned host. It supplies the compatibility
identity, source listener, and optional native tray/local logs. The packaged
GUI host defaults to desktop mode. Its executable must remain in the reviewed
application directory, not replace the vendor connector.

`vxbridge.exe` is the distinct console/control build. Status and Quit connect
to the existing same-user host with a 3-second query deadline. They do not
start a second host and remain available when managed host admission refuses.
Quit requests source-first shutdown; a returned snapshot does not prove
process exit. Use normal ProTee Labs Disconnect first.

There is no command-line game selector, hostname option, readiness command,
force-close flag, or service mode. The tray selects the destination and the
saved preference supplies it on startup. `--target-port` configures Infinite
Tees only, not GSPro OpenAPI.

### OPTIONS

**`--version`** prints application/release/revision, Go, desktop default, and
available embedded build settings without starting a host. It can be inspected
before the Windows platform check.

**`--status`** prints the current engine/desktop snapshot as JSON.
Unreachable control is an error, not cached status.

**`--quit`** requests the same guarded Quit as the tray and prints its snapshot.
It never forces an attached or possibly attached source closed.

**`--desktop`** enables the native tray and bounded local logs in host mode.
`--desktop=false` selects console diagnostics; the GUI distribution normally
needs no explicit option.

**`--target-port N`** sets the Infinite Tees IPv4 loopback port. The default
is 999; valid values are 1 through 65535 except source port 12321.
GSPro OpenAPI remains at 921, and Standby does not dial.

Status and Quit are mutually exclusive; version cannot combine with either.
A nondefault target port is invalid with status/Quit. Positional arguments
are rejected. Control through the `GSPconnect.exe` basename and host startup
through the `vxbridge.exe` basename are rejected to preserve one identity.
The Go flag parser also supplies `-h`/`--help`; current usage handling exits 2.

### FILES

```text
%APPDATA%\VxInfiniteTeesBridge\destination.json
%LOCALAPPDATA%\VxInfiniteTeesBridge\logs\bridge.0.jsonl ... bridge.4.jsonl
%LOCALAPPDATA%\Programs\VxInfiniteTeesBridge\          managed payload
%LOCALAPPDATA%\VxInfiniteTeesBridge\installer\        passive maintenance state
HKCU\Software\VxInfiniteTeesBridge\Installer
HKCU\Software\Microsoft\Windows\CurrentVersion\Run\VxInfiniteTeesBridge
```

These names are compatibility identities, not renamed by the VX Bridge brand.
See [Desktop](DESKTOP.md) for preferences and
[Installation](INSTALLER.md) for receipt/cache authority. Do not edit receipt
or Pending values to bypass admission.

### EXIT STATUS

**0** means the requested version/control operation completed, or the host
finished its guarded lifecycle successfully. A Quit response can return 0
while the host still waits for Labs departure.

**1** means runtime, admission, control, output, persistence, or cleanup
failure. The operation must not be assumed complete from partial output.

**2** means invalid/conflicting usage, unsupported host platform/role, or help.
The runtime host requires Windows amd64.

### EXAMPLES

From the reviewed build's own directory in PowerShell:

```powershell
.\vxbridge.exe --version
.\vxbridge.exe --status
.\vxbridge.exe --quit
```

For an explicitly approved desktop host using the default Infinite Tees port:

```powershell
.\GSPconnect.exe --desktop --target-port 999
```

The shown port is the default; replace it only with the known local target.
No example initiates Labs Connect or configures a game/license.

### DIAGNOSTICS

**Managed installation admission refused** requires verified Setup maintenance,
not an MSI dependency added to normal startup. **Local control failed** means
no current response was obtained. **Exclusive source bind failed** leaves the
other receiver untouched. **Native identity failed closed** means the required
interactive/window properties were not verified.

Preference and local-log failures are explicit; they do not select a new game,
create an alternative log path, or tear down attached Labs.
See [Lifecycle](LIFECYCLE.md) and [Security](SECURITY.md).

## Project checks

Use GNU make 3.81 or later, Bash, Git, basic Unix tools, and the exact Go
compiler in [.go-version](../../.go-version). The
[Makefile](../../Makefile) is authoritative. It exports `GOTOOLCHAIN=local`;
it does not substitute a downloaded compiler. `GO` and `GOFMT` may name
executable paths containing spaces, not command-and-flag strings.
On Windows use GNU make with Bash, not `nmake`.

| Target | Effect |
|---|---|
| `make` / `make help` | Print help only. |
| `make help-installer` | List repeatable static installer build/integrity entry points. |
| `make help-qualification` | Explain parked private-only qualification, unavailable in this public repository. |
| `make test` | Native readonly-module tests, count 1, 90-second package timeout; CGO defaults to 0. |
| `make test-race` | Native race tests with CGO1; requires a native C compiler. |
| `make vet` | Native vet; `GOOS`/`GOARCH` may select cross-vet. |
| `make fmt` | Format tracked Go files in place. |
| `make fmt-check` | Report unformatted tracked files without editing. |
| `make check` | Formatting check, tests, and vet; no package or publication. |
| `make ci` / `make ci-windows` | Shared portable/native-Windows workflow checks; `ci-windows` requires a new external `OUT` for the fake-tool regression. |
| `make check-commands` | Fake-tool command-contract tests, also included in full tests. |
| `make test-native-tools` | Windows fake-native-tool regression, also used by `ci-windows`; requires new external `OUT`, with no real installer execution. |
| `make lint-workflows` | Pinned actionlint; may download tool modules into the external Go cache. |
| `make benchmark` | Fixed informational processing benchmark, not a live timing gate. |

For Windows cross-vet without executing Windows code:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 make vet
```

For the native Windows checks, run from Bash with an existing external parent
directory and an output directory that does not yet exist:

```sh
OUT='C:/private-artifacts/new-native-tool-tests' make ci-windows
```

The same `OUT` requirement applies to standalone `make test-native-tools`.
The hosted workflow supplies a fresh runner-temporary path; the PowerShell
script refuses existing or in-checkout output instead of prompting or reusing it.

For fixed benchmark output without hiding failure:

```sh
set -o pipefail
make benchmark 2>&1 | tee '/absolute/path/to/existing-private-directory/bench.txt'
```

The benchmark uses readonly modules, CGO0, benchmem, six 300 ms samples,
one CPU, and a 5-minute package timeout in `internal/protocol` and
`internal/bridge`. Its selector is
`Benchmark(SourceFrameInspect|SplitMerge|ReceiptFrameResponse|ForwardReceiptPipe|IdleTick)$`.
See [Testing](TESTING.md#benchmarks) for what those cases measure.

## Build and verify a package

`make build` aliases `make package`, not a loose/native binary or `go run`.
Packaging requires a clean committed checkout, its exact revision, the pinned
Go compiler, and a **new absolute output directory outside the checkout**
whose parent exists. Dirty/untracked/ignored input, wrong HEAD/compiler,
in-tree output, or an existing output directory refuses the build.

```sh
OUT='/absolute/path/to/artifacts/new-build' make package
revision="$(git rev-parse HEAD)"
ARCHIVE="/absolute/path/to/artifacts/new-build/vx-bridge-windows-amd64-$revision.zip" \
  make verify-package
```

`OUT` is required for package; `ARCHIVE` is required for verification.
Quoted environment assignments support paths with spaces. `REVISION`
defaults to HEAD, but an explicit full SHA must match the clean checkout.
`RELEASE_VERSION` defaults to `development` even if HEAD has a tag.
Explicit versions use canonical `vMAJOR.MINOR.PATCH`, at most 64 characters,
without leading-zero components, prerelease, or build suffixes.

The clean-HEAD requirement applies to building, not inspection of an existing
archive. For verification, supply the archive's actual revision/version when
they differ from the checkout defaults, and use its recorded Go compiler.

The authoritative distribution builder owns the Windows amd64/v1 CGO0
linker/PE/build-metadata checks and six-entry ZIP. It runs neither executable.
Output is the ZIP and external `.zip.sha256`; partial output is left for
inspection, not silently cleaned or reused. Versioned archive names include
both version and exact SHA. Setting version metadata creates no tag or release.
See [Automation](AUTOMATION.md#distribution-identity).

## Installer builds

These commands produce native inputs and static authoring evidence only.
They do not install a product, load a callback DLL, start the bridge, or
qualify a user machine.

**`make build-setup-native`** cross-builds native Setup/actions on the selected
Linux-amd64 toolchain. It requires clean exact `REVISION`, a new external
`OUT`, and the [pinned native prerequisites](INSTALLER.md#build-tooling-and-static-verification).

**`make prepare-setup`** consumes the authoritative Go `ARCHIVE` and matching
`NATIVE_DIR`, with a new external `OUT`. It verifies exact source/version
and stages MSI input; it does not create the Go archive.

**`make build-setup-msi`** authors and statically inspects on Windows. It
requires `SETUP_STAGING`, pinned `WIX_ARCHIVE`, explicit `TOOL_LOG`, and new
external `OUT`. `POWERSHELL` defaults to `pwsh`. Normal
`SETUP_BUILD_PROFILE` is `normal`; private fault media is restricted to the
separate exact-source hosted lifecycle context.

After creating the application package from a clean reviewed commit:

```sh
REVISION="$(git rev-parse HEAD)" OUT='/external/new-native' make build-setup-native
revision="$(git rev-parse HEAD)"
ARCHIVE="/absolute/path/to/artifacts/new-build/vx-bridge-windows-amd64-$revision.zip" \
  NATIVE_DIR='/external/new-native' OUT='/external/new-staging' \
  RELEASE_VERSION=development make prepare-setup
```

Use actual newly produced paths whose parents exist. Windows authoring uses
that verified staging and the approved WiX archive, not a different source's
native DLL. Old private/outer/wixl helpers are
[investigation history](research/README.md#installer-and-automation-records),
not alternate production build recipes.

**`make package-installer`** assembles verified authored Setup output with its
matching application archive. It requires `AUTHORED_SETUP`, `ARCHIVE`,
`MINGW_COPYRIGHT`, and new external `OUT`. The pinned compiler copyright
record is required package provenance, not optional decoration.

**`make verify-installer`** checks an existing installer bundle against
`ARCHIVE`, `INSTALLER_ARCHIVE`, exact `REVISION`, and `RELEASE_VERSION`.
It is static integrity verification, not installation or a qualification
verdict. Bundle entries and release filenames are described in
[Automation](AUTOMATION.md#installer-artifacts).

**`make install-setup-toolchain`** explicitly provisions the pinned compiler
on the configured Linux builder. Unlike static verification, this changes
the build environment. It is not a default check or instruction to provision
a simulator PC.

The workflow wrappers avoid duplicating these commands in YAML.
`make build-setup-input` requires matching `REVISION`/`WORKFLOW_REVISION`,
`RELEASE_VERSION`, `RUNNER_TEMP`, and actual `GITHUB_RUN_ID`/
`GITHUB_RUN_ATTEMPT`; it writes `RUNNER_TEMP/setup-build-input`.
`make build-setup-artifacts` requires `BUILD_INPUT_DIR`, `EVIDENCE_DIR`,
`NATIVE_ARTIFACT_ID`/`NATIVE_ARTIFACT_DIGEST`, and the same source/run/version
identity. It authors and verifies on Windows, writing
`RUNNER_TEMP/release-artifacts` and evidence. These are explicit workflow
handoffs, not source/latest fallback interfaces.

## setup(1)

### NAME

setup - install or maintain a verified per-user VX Bridge package

### SYNOPSIS

```text
setup.exe [/quiet] [/maintain] [/package PATH]
setup.exe [/quiet] /remove
```

### DESCRIPTION

Native Setup is explicit maintenance, not an ordinary-start broker.
Run it normally as the installing user. With no explicit package, it uses
the verified recorded/cached source or adjacent `bridge.msi`. `/package`
selects source media for verification, not an ownership bypass.
`/maintain` is the cached maintenance entry, not a different transaction
policy. `/quiet` suppresses choices and never launches the bridge.

`/remove` only directs the user to Windows Installed apps or verified
`msiexec /x ProductCode`; it does not perform removal or self-delete.
Normal Uninstall is Windows Installer-owned.

### EXIT STATUS

**0** means verified usable installation/maintenance completion.
**1** means refusal or incomplete maintenance.
**2** means invalid arguments or canceled initial choice.
**120** (`ERROR_CALL_NOT_IMPLEMENTED`) is the directions-only `/remove` result.
MSI engine status is recorded separately; engine zero alone is not usable
completion. See [Installation](INSTALLER.md#recovery-after-failure).

## Workflow-only operations

`release-resolve` and `release-verify` are exact tag-event identity gates.
`release-publish` can write to GitHub with the required event, token, and
same-run verified artifact. They are not local test commands or shortcuts
for publishing a branch package.
Publication uses `ARTIFACT_ID` and `ARTIFACT_DIGEST` for the final same-run
tag assets, separately from `NATIVE_ARTIFACT_ID`/`NATIVE_ARTIFACT_DIGEST`
for source inputs. The input digest is 64 lowercase hexadecimal characters;
neither identity can be replaced by an artifact-name/latest lookup.

The manual hosted lifecycle is parked at
[`tools/msi/lifecycle/workflow.yml`](../../tools/msi/lifecycle/workflow.yml).
Its retained private-repository gate prevents execution from this public
repository, including after copying it to the active directory. The
[qualification boundary](../../tools/msi/lifecycle/README.md#temporary-activation)
requires a separately reviewed private design and owner authorization before
any future native Setup/MSI execution under an independent standard account.
Only the activation commit receives that result; source/workflow identity
guards must not be relaxed to claim qualification of a parked final commit.
Its worker configuration is not a general local installer interface.
`build-lifecycle-input`, `author-lifecycle`, `test-installer-lifecycle`, and
`restore-lifecycle-policy` are its shared Make entry points. They retain the
named hosted workflow, exact source/run, `BUILD_INPUT_DIR`,
`LIFECYCLE_EVIDENCE`, and native-artifact identity guards.
The test target executes the full failure/recovery/cleanup lane; it is
**not** a build-only or ordinary-CI target. Policy restoration does not
substitute for audited product cleanup.
Use [Automation](AUTOMATION.md) for those workflow responsibilities and
[Qualification](QUALIFICATION.md) for actual results.

## Diagnostic tools

The [diagnostic-tool reference](../../tools/diagnostics/README.md) owns offline
MSI-log inspection, package verification, and the distinction from live
same-user status. For a local saved log:

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 go run -mod=readonly ./tools/diagnostics/msi-log \
  /absolute/path/setup-msi.log
```

The inspector reads one bounded regular file and reports JSON counts and
fixed action observations, not an installation or qualification verdict.
It has no GitHub/source gate, socket, registry, MSI API, or host operation.
Its optional policy check and exit semantics are documented at that entry
point rather than duplicated here.

The [full lifecycle worker](../../tools/msi/lifecycle/README.md) remains a
separately guarded hosted execution tool, not a substitute for offline
analysis. The stopped [Labs probe](../../tools/research/labs-probe/README.md) remains
a research instrument outside the diagnostic-tool collection.
