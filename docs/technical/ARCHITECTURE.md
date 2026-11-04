# Architecture

[Technical manual](README.md) / Architecture

## Process model

VX Bridge runs in the interactive Windows x64 user session. ProTee Labs remains
responsible for the launch monitor; the selected game remains responsible for
flight and display. The bridge translates neither measurements nor units.

```text
ProTee VX -> ProTee Labs -> GSPconnect.exe -> Infinite Tees 127.0.0.1:999
                           127.0.0.1:12321   or GSPro OpenAPI 127.0.0.1:921
                           native tray     or Standby (no target)

vxbridge.exe --status / --quit -> private same-user control -> GSPconnect.exe
```

The application-owned `GSPconnect.exe` hosts the source listener, compatibility
window, and tray in one process. It is installed or staged in its own directory,
never over the vendor connector. A distinct `vxbridge.exe` provides short-lived
console/control commands without creating another compatibility identity.

Both executables are built from `cmd/vxbridge`. The distribution builder selects
the GUI subsystem and `defaultDesktop=true` for the host, and the console
subsystem and `defaultDesktop=false` for control. The linked revision, release
metadata, and compiler are checked in the emitted binaries. A filename or
requested linker flag is not sufficient evidence of build identity.

## Startup and ownership

A managed host first verifies its local installation receipt and payload.
It then claims first-instance private control, binds exclusively to
`127.0.0.1:12321`, and creates the verified Win32 identity. This order avoids
advertising a connector before its listener exists. Duplicate startup reports
an existing host or an unresolved control conflict; it does not replace or
terminate another process.

The identity is an unowned, shown, transparent, nonactivating window titled
`ProTee United VX`, positioned offscreen. Being shown is a compatibility
requirement, not an invitation to display application content in that window.
The tray uses the same locked-OS-thread message loop. An additional hidden,
owned window supplies native popup ownership without becoming a second
compatibility main window.

The first accepted Labs connection establishes a source epoch. A second source
is rejected without replacing the first. Actual source EOF or an identified
peer reset ends that epoch and returns the host to listening. Target failure
does not end it. [Lifecycle](LIFECYCLE.md) describes the more conservative
handling of faults that leave attachment uncertain.

## State and I/O

`internal/bridge` has one mutable state owner. Readers, serialized writers,
dialers, and diagnostic workers exchange typed events with that owner.
Blocking socket, filesystem, registry, and shell work does not run in the
state owner or identity callbacks. Snapshots are observations of that state,
not another authority allowed to mutate it.

The engine maintains one source, one destination connection or dial attempt,
one held split-shot candidate, and one pending target write/receipt.
`internal/protocol` owns strict framing and raw-preserving merge;
`internal/destination` owns built-in target profiles and the saved choice.
Profiles validate shot bytes and classify responses; they do not select games,
manage source lifetime, or decide which arrivals are eligible.

Source epochs, target generations, and eligibility tickets serve different
purposes. An epoch identifies a Labs session. A generation identifies a target
attempt/connection. A ticket identifies an opportunity to forward fresh work,
including successive opportunities on the same connection. Keeping these
identities separate prevents stale callbacks and buffered frames from becoming
current merely because a socket remains open.

## Destination selection

The initial destination is Infinite Tees at `127.0.0.1:999`. The host's
`--target-port` option changes only that profile's port; GSPro OpenAPI remains
at `127.0.0.1:921`. Standby neither connects nor retries. All endpoints are
IPv4 loopback. There is no hostname, LAN, IPv6, vendor-settings discovery,
active-game detection, or fan-out.

The tray selects and remembers one destination. Selection closes new-shot
admission, discards an unsent held candidate, and drains the existing receipt
and finite upstream work before retiring the old target. It preserves the
source epoch, socket, window, and duplicate ledger. Reopening admission
creates a fresh ticket rather than retagging old read-ahead. See
[Protocol and delivery](PROTOCOL.md#changing-destination) for the switch barrier.

A healthy target carries successive newly eligible shots. A failed target is
retired and retried independently of swings. Recovery restores the opportunity
to send future arrivals, not a queue of shots missed during the outage.
Connected transport still leaves game readiness unknown.

## Supporting packages

`internal/identity` owns the exclusive bind and native window.
`internal/control` owns bounded same-user status and Quit.
`internal/desktop` owns the tray model, owner-thread dispatch, and action workers.
`internal/diagnostics` owns bounded redacted logs and the shared read-only local
ZIP exporter; `internal/startup` owns
exact-copy, opt-in sign-in registration.

`internal/managed` verifies the fixed managed-copy receipt, local Pending
denial, and six payload files, retaining a process-lifetime exclusion handle.
It does not call MSI during ordinary startup. Explicit native Setup owns
maintenance; it is not a permanent broker, service, or game selector.
Control remains available even when managed host admission is refused.
Diagnostic modes also run before admission, without starting a host or touching
its lease. The tray and installed support shortcut share that exporter; no
native duplicate collector or extra payload executable is installed.

The shipped Go graph uses the standard library, pinned `golang.org/x/sys`,
and `github.com/Microsoft/go-winio`. It is built with CGO disabled and requires
no separately installed .NET or browser runtime. Build identity and native
installer tooling are described in [Automation](AUTOMATION.md) and
[Installation](INSTALLER.md).

VX Connector v1.5.6 informed the original implementation, but its ready timers,
uncertain-write resend, status-triggered merge release, and paired Stop were
not adopted. [Third-party notices](../../THIRD_PARTY_NOTICES.txt) preserve the
reference and license record; the [research boundary](research/README.md)
explains the omission of private ledgers and superseded proposals.
