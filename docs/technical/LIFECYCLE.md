# Lifecycle and shutdown

[Technical manual](README.md) / Lifecycle

## Source-first lifetime

Quit is a request to finish safely, not cancellation of the host's root
context. While Labs is attached, or may still be attached, VX Bridge retains
the source socket, compatibility window, and host process. The user must
disconnect normally in ProTee Labs before local teardown can complete.
Repeated Quit, Ctrl+C, and `WM_CLOSE` do not add a Force Close route.

This guard exists because an earlier receiver-first research teardown was
followed by a Labs crash. The trial did not isolate whether closing the socket,
removing the identity, or another mechanism caused the crash. Preserving the
guard is therefore a safety requirement, not a claim that the cause is known
or that every shutdown is safe.

Power loss, logoff, OS termination, console-window close, Task Manager, and
actual process crashes remain outside the guard. Installer maintenance uses
the same normal-departure procedure and never controls Labs on the user's
behalf. The [safety summary](QUALIFICATION.md#safety-incident) preserves the
distinct crash and later reset/exit-9 outcomes without republishing private data.

## Normal operation and target recovery

Startup claims private control, binds exclusively, verifies the identity, and
listens for Labs. A source attachment creates an epoch and permits target
dialing for the selected destination. A second source is rejected; the
existing source is not displaced.

The host outlives target unavailability. Invalid responses, write failures,
and receipt expiry retire target work while leaving Labs and the identity
alive. Retry runs independently of new swings. Shots arriving while
unavailable are discarded without success or replay; after recovery only
fresh eligible arrivals can forward.

A destination change uses a target-only drain, not a source reconnect.
It discards held work, settles or expires the existing receipt, and resolves
finite upstream work before applying the next profile. Standby keeps Labs
attached but does not dial. See
[Protocol](PROTOCOL.md#changing-destination) for the admission barrier.

## Departure and quarantine

An actual zero-byte EOF or specifically identified peer reset ends the source
epoch. On Windows the reset classification is `WSAECONNRESET`. The host
retires session-bound held/reply work, closes the target, clears the duplicate
ledger, and returns to listening unless Quit is pending.

A reset is not clean EOF, deliberate user intent, valid completion of a
partial JSON frame, or proof that Labs survived. Incomplete-frame
classification and socket errno remain separate from host state.

Parsing, source-response write, and unexplained platform/I/O faults take a
different route: quarantine. Forwarding and success replies stop, the target
is invalidated, but the source remains because attachment may be unresolved.
After framing failure, bounded payload-free draining can still observe actual
peer departure. A deadline or local close is not substituted for that
observation.

Unknown I/O conservatively means "may still be attached." Quit may consequently
remain pending indefinitely. A message-loop or listener failure is a guarded
fatal request, not permission to destroy the identity beneath Labs. Disk,
console, logging, or shell failure likewise does not authorize source teardown.

## Quit and the accept race

An ordinary pending Quit can continue normal forwarding until Labs departs.
Quarantine instead prohibits forwarding while retaining the same departure
guard. Destination requests after Quit or quarantine fail explicitly; they
cannot be used to escape the guard.

With no current source, Quit closes the listener and waits for accept-loop
admission to finish. A connection already accepted in that race must still be
accounted for and retained until actual departure. A source-free snapshot
alone is not proof that this barrier has completed.

A successful control response means the request was accepted and a snapshot
was returned. It does not mean the process exited. Maintenance must wait for
the authenticated host instance and lifetime lease, not a missing icon,
detached-looking status, or a stopped control pipe.

## Completion and diagnostics

Only guarded engine completion permits desktop-worker shutdown and
owner-thread identity/tray cleanup. The runtime then closes control and
reports its process result. The engine diagnostic worker receives up to
100 ms to finish; desktop and log workers have bounded 500 ms shutdown waits.
A persistence or cleanup failure remains explicit and can produce a nonzero
exit after source-first completion.

When reviewing evidence, distinguish requested Quit, observed departure,
departure classification, final versus nonfinal snapshot, process exit,
owner-confirmed game outcome, and bounded peer-health checks. Exit 0 is not a
wire capture of FIN/RST. A last attached/pending snapshot is not a final
state. [Qualification](QUALIFICATION.md) records which of these observations
exist and which interruption cases remain untested.
