# Diagnostics

Small, read-only tools for existing local logs and packages. They do not install,
repair, uninstall, run the bridge, bind its source port, create an identity window,
connect to a simulator, change registry state, or grant permission for a live test.
Run source commands from the repository root with the Go version in
[`.go-version`](../../.go-version), `GOTOOLCHAIN=local`, and `CGO_ENABLED=0`.

Only the log inspector lives here. Package verification reuses the authoritative
distribution tool rather than maintaining another verifier; status reuses the
installed control client rather than introducing another host.

## msi-log

### NAME

`msi-log` - summarize an existing Windows Installer verbose log offline.

### SYNOPSIS

```text
go run -mod=readonly ./tools/diagnostics/msi-log [-require-policy] LOG
```

### DESCRIPTION

Useful when Setup fails, an upgrade leaves recovery required, or rollback reports
skipped errors. Emits one JSON object containing observed file/registry operation
counts, private fault markers, skipped rollback errors, completion observations
for a fixed list of relevant actions, and whether the log records all three engine
policies as zero. It shares the existing lifecycle's bounded text decoder, action
matcher, and policy verifier in `msilog/`; it does not read MSI databases or current
installed state.

### OPTIONS

`-require-policy` also exits nonzero if any `DisableMsi` or machine/user
`AlwaysInstallElevated` proof is missing or nonzero. All observed occurrences must
be zero. `-h` prints help. One log is required; options precede its path.

### EXAMPLES

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 go run -mod=readonly \
  ./tools/diagnostics/msi-log "/absolute/path/to/setup-msi.log"
GOTOOLCHAIN=local CGO_ENABLED=0 go run -mod=readonly \
  ./tools/diagnostics/msi-log -require-policy "/absolute/path/to/07-failed-upgrade-setup-msi.log"
```

### FILES

Reads only the named regular file, at most 32 MiB; no symlinks or directories.
UTF-16LE with a BOM is decoded, including native MSI logs; other bytes are read
unchanged. Prints fixed fields/counts to stdout and errors to stderr; does not
export raw log text, SIDs, product identifiers or input paths in the JSON.
Original bytes are not rewritten. Shell redirection creates only the file you
explicitly name.

### EXIT STATUS

`0`: summary emitted (or help printed), **not an installer pass**.
`1`: input/output failure, malformed UTF-16, exceeded bound, or required policy
proof missing/nonzero. Policy failure still emits its summary.
`2`: invalid arguments. `go run` can wrap the command's nonzero exit status;
build the command if a script needs the exact numeric status.

### WARNINGS

Counts and action observations span the whole supplied log. Appended operations
can contain earlier completions; these are not evidence of the latest outcome.
Logs may be partial or still changing. A completed action is not a successful
install, restored old version, usable repair, clean uninstall, or live bridge
qualification. English engine-policy phrases are required by `-require-policy`;
localized or incomplete logs cannot provide that proof. Keep the raw log locally
for diagnosis; it can contain private account/path data. Never clear Pending,
force components, weaken ownership, or change privileges/policies to obtain a
clean-looking result.

## verify-package

### NAME

`verify-package` - statically check a previously built distribution ZIP.

### SYNOPSIS

```text
make verify-package ARCHIVE=ZIP REVISION=FULL_SHA RELEASE_VERSION=VERSION
go run -mod=readonly ./tools/dist -verify ZIP -revision FULL_SHA -version VERSION
```

### DESCRIPTION

Useful for detecting a corrupt, mismatched or incorrectly packaged archive before
any Windows execution. Reuses [`tools/dist`](../dist/main.go) for the exact
inventory, hashes, build manifest, native PE/build identity and notice checks.
Neither executable in the archive is loaded.

### OPTIONS

`ARCHIVE` / `-verify`: existing ZIP. `REVISION` / `-revision`: its expected full
40-character source SHA. `RELEASE_VERSION` / `-version`: its expected `vX.Y.Z`,
or `development` for a development archive. Supply the archive's actual identity,
not the current checkout's identity when they differ. Do not use `-out`, which
selects a build instead of inspection.

### EXAMPLES

```sh
archive="/absolute/path/to/bridge.zip"
revision=0123456789012345678901234567890123456789 # replace with the archive's real SHA
version=v0.0.2 # replace with its real version
make verify-package ARCHIVE="$archive" REVISION="$revision" RELEASE_VERSION="$version"
```

### FILES

Reads the named ZIP and its contained manifest, checksums, executables and notices.
Does not extract or execute the application, modify the archive, or publish it.

### EXIT STATUS

Zero means static package checks passed. Nonzero means invalid input or failed
verification. `make` / `go run` may wrap the underlying exit status.

### WARNINGS

This checks bytes and packaging only, not signatures, interactive Windows startup,
sign-in, native installer execution, real Labs, or simulator compatibility.
Preserve `LICENSE` and `THIRD_PARTY_NOTICES.txt`; an intact archive is not permission
to run it on hardware.

## vxbridge --status (Windows; not offline)

### NAME

`vxbridge --status` - query the already running same-user bridge without starting it.

### SYNOPSIS

```text
vxbridge.exe --status
```

### DESCRIPTION

Useful when logs or tray status are unavailable. The existing control client reads
one bounded JSON snapshot over the private same-user pipe. It does not create a
second `GSPconnect.exe` identity, source listener, game connection, or host.

### OPTIONS

`--status` requests status only. Do not substitute `--quit`: that requests a
guarded state change, not a read-only diagnostic. Do not run without `--status`.

### EXAMPLES

```sh
# On Windows, from the application's own directory as the owning normal user:
./vxbridge.exe --status
```

### FILES

Uses the existing per-user control pipe. No arbitrary pipe/path override and no
settings, startup, installation, or vendor-state writes.

### EXIT STATUS

`0`: snapshot printed. `1`: control/output failed (including no running host).
`2`: unsupported platform or invalid/conflicting invocation.

### WARNINGS

Run only when a same-user status query is authorized. Use distinct `vxbridge.exe`,
not `GSPconnect.exe`; elevation, SSH/Session 0, or a different account is not a
substitute for the owning user. Transport status and recorded target acceptance
do not prove game readiness, peer consumption, or end-to-end compatibility.

## Deliberately separate

[`tools/msi/lifecycle`](../msi/lifecycle/README.md) retains the full **manual,
separately authorized disposable-host** install, repair, upgrade, deliberate
failure, supported recovery, explicit uninstall/reinstall, and final cleanup
qualification. It is not a diagnostic command for a developer's PC. The old
wixl/state-first, native fixture, outer/vendor/elevated comparison, and ETL
workflow-bound spike commands have been retired, not promoted to debugging tools.
Their original private Git history and recorded outcomes are unchanged and
are not imported into this public source-only repository.

[`tools/research/labs-probe`](../research/labs-probe/README.md) remains **STOP / DO NOT RERUN** research,
outside this folder. Its parser/window/source-first fixture tests are retained,
not offered as a live debugging command. E14's receiver-teardown crash and E15's
separately approved inconclusive reset/exit 9 remain historical safety facts;
see [the safety summary](../../docs/technical/QUALIFICATION.md#safety-incident).
Do not run that executable, bind source 12321, or exercise teardown on Labs as a
development check. No source socket/window/session or new live run is authorized
by this README.
