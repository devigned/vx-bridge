# Security and data ownership

[Technical manual](README.md) / Security

## Local trust boundary

Source and target sockets use IPv4 loopback only. The bridge has no LAN
endpoint, HTTP server, telemetry, automatic upload, or firewall modification.
The exclusive source bind refuses to take over another receiver.

Local control uses first-instance named-pipe exclusivity, a protected DACL
for the current user and LocalSystem, and remote-client rejection. The
protocol contains only bounded status and guarded Quit requests, not
arbitrary execution. Control failure leaves an existing host untouched;
it does not establish ownership by inference.

Loopback is a locality constraint, not vendor authentication or proof that
another local process is honest. Strict framing, bounded resources, and
generation/eligibility checks constrain malformed or stale traffic. Actual
target responses still cannot establish exactly-once delivery or readiness.
See [Protocol](PROTOCOL.md).

The application's `GSPconnect.exe` basename is a compatibility identity,
not permission to overwrite the vendor connector. Do not patch vendor
executables, reset activation, disable security protections, or terminate
connectors by process name. Neither a successful build nor an earlier trial
grants permission to deploy or operate real equipment.

## Managed files and preferences

Managed-copy startup checks the fixed installation root, receipt, absence of
Pending maintenance, and exact six-file payload. It retains verified object
handles and a process-lifetime lease. Explicit Setup additionally verifies
MSI package, source, catalog, and current-user ownership before servicing.
Ordinary startup does not query MSI or repair itself.

Repair and compatible upgrades preserve owned preferences, logs, and
sign-in state. Full Uninstall must remove them, along with payload, passive
cache, shortcuts, receipt, Pending, and owned product/startup entries.
Unknown names, reparse points, hardlinks, or foreign state cause refusal;
they are not recursively deleted. Original user downloads, vendor files,
game data, and independently saved exports are outside cleanup authority.
[Installation](INSTALLER.md#full-uninstall) defines the exact boundary.

## Bounded diagnostics

Desktop logs use five fixed circular slots:

```text
%LOCALAPPDATA%\VxInfiniteTeesBridge\logs\bridge.0.jsonl
...
%LOCALAPPDATA%\VxInfiniteTeesBridge\logs\bridge.4.jsonl
```

Each slot is limited to 2 MiB, for 10 MiB total. UTC event timestamps determine
chronology; file-number order does not. Records contain sanitized reasons,
optional bounded local shot aliases, and compiled application/build/Go
identity. Unknown reasons become stable fault hashes.

Records exclude measurements, DeviceID, real shot IDs, player values, raw
objects, window handles, and private-path dumps. Metadata comes from the
application build, not arbitrary payload fields. There is no raw capture or
export menu.

Unexpected file types, oversized slots, permission/lock errors, short writes,
disk-full, rotation, and close failures stop persistence explicitly.
The logger does not invent an alternative directory, remove unrelated files,
or tear down attached Labs. Its independent sink has a 32-record queue and
64-event sanitized fallback; the engine separately retains 64 events and a
32-event diagnostic queue. Loss and overload are visible in status and the
tray.

Logs are best effort, not a durable audit. OS termination can lose queued
records, and final persistence failure can produce a nonzero exit after
guarded shutdown. Bounded worker waits do not imply durable delivery of every
diagnostic.

## Research data

Raw captures and private evidence remain local and outside Git. Do not place
PCAPs, vendor logs, real measurements, device/account/license identifiers,
or credentials in fixtures, issues, or documentation. Invented fixtures must
be identified as invented; capture-derived fixtures require provenance and
privacy review.

Real-equipment access, capture, and diagnostic execution require separate
owner approval for the exact artifact, scope, risk, and cleanup. Historical
research procedures are records, not standing permission.
The [research boundary](research/README.md) explains why those private records
are not included in this public source tree.

## Licenses and publisher identity

Original work is [MIT-licensed](../../LICENSE), including its notice and
warranty disclaimer. That license does not license vendor software or
third-party source. [THIRD_PARTY_NOTICES.txt](../../THIRD_PARTY_NOTICES.txt)
preserves the Go, x/sys, go-winio, and evaluated-reference terms.

VX Connector v1.5.6 was a behavioral and architectural reference, not a
mechanically translated distribution. Its README declared MIT while the
inspected tag lacked a standalone license; that provenance caveat must remain
before source reuse. No vendor binary or upstream source is shipped. Evaluated
systray and lumberjack packages were not linked; the native tray, fixed-slot
logger, and geometric icons are original work.

The native build uses the selected WiX 3.14.1 toolchain under MS-RL; its
ended community security support is documented in
[Installer build tooling](INSTALLER.md#build-tooling-and-static-verification).
WiX's build-time Framework requirement is not an installed bridge
requirement. Preserve [installer notices](../../installer/NOTICE.txt) and
third-party acquisition records.

Checksums, manifests, and static inspection establish internal consistency,
not publisher authentication, vendor endorsement, or hardware compatibility.
Binary signing and cryptographic artifact attestation are not configured, and
CI does not supply either claim. Signed repository commits are required by
[AGENTS.md](../../AGENTS.md); a Git signature does not sign emitted binaries.
