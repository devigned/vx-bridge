# Testing

[Technical manual](README.md) / Testing

## Test environment

Automated Go tests use invented peers, `net.Pipe`, ephemeral loopback ports,
and private temporary native fixtures. They do not launch the product EXEs,
bind runtime source/game ports or the production control pipe, change actual
startup policy, read vendor data, or operate simulators.

Run the smallest selection covering the changed invariant. A protocol,
destination, or bridge change can start with:

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 go test -mod=readonly -count=1 -timeout=90s \
  ./internal/protocol ./internal/destination ./internal/bridge
```

Include `./internal/desktop` when selection/status behavior changes.
Preference tests use temporary files; a blocked or failing save must leave
status and guarded Quit responsive. The bridge's switching fixtures use
private pipe barriers, not live API ports.

For broader changes use `make check`, then `make test-race` with a native C
compiler. Native tests must run for the host platform; selecting a foreign
`GOOS` is not a way to execute them. Windows cross-vet checks code selection,
not native window/menu behavior. The
[command reference](COMMANDS.md#project-checks) owns shared target semantics.

## Protocol and delivery assertions

Framing tests must cover fragmentation, coalescing, UTF-8, duplicate keys,
size/depth/time limits, and exact raw representation. Merge assertions must
retain unknown, null, false, and missing optional values while changing only
the two club members. Same-ID, duplicate, late, and unmatched inputs must
not create additional shots.

Receipt tests distinguish a full write, applicable observed 200, local ACK
completion, and peer consumption. Include early reply/write races, delayed
status, a persistent target carrying multiple fresh shots, first-read and
buffered origin, and eligibility changes on the same socket. A restored
connection must not retag old frames or replay a backlog.

Recovery tests exercise partial/stalled/zero-progress writes, invalid replies,
receipt expiry, canceled late dials, and stale callbacks. Return-path tests
must include pacing admission, inclusive count/byte limits, abandoned accepted
ACKs, blocked consumers, and writes that complete after retirement.
Prefer exact cause, counter, ownership, and completion assertions over
arbitrary sleeps or global goroutine counts.

## Lifecycle and native integration

Source tests distinguish actual EOF/reset from deadlines, local close, and
unknown I/O. Quarantine must retain possible attachment while stopping
forwarding. Repeated Quit, second-source rejection, accepted-source races,
destination drain, and faults during that drain must not bypass the guard.

Desktop, logs, and startup fixtures check honest status, owner-thread dispatch,
visible shell/storage failures, redaction/bounds, quoting, fresh-off behavior,
and exact-owner mutation. Native Windows fixtures use temporary HKCU keys,
private protected pipes, exclusive binds, and test windows/icons/menus.
They do not qualify the host in Session 0: interactive visibility verification
deliberately fails closed when the required window is not visible.

Managed/installer tests cover local admission, source/receipt/payload binding,
Pending denial, root and removal policy, and emitted-package invariants.
Cross-compiling native C fixtures or checking their source is not executing
them on the Windows kernel. Ordinary Go tests and static builds are distinct
from the explicit disposable-account lifecycle described in
[Qualification](QUALIFICATION.md#installer-observations).

For the retained lifecycle worker and offline diagnostics, use their package
fixtures without executing a product:

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 go test -mod=readonly -count=1 -timeout=90s \
  ./tools/msi/lifecycle ./tools/diagnostics/...
```

The [worker reference](../../tools/msi/lifecycle/README.md) and
[diagnostic reference](../../tools/diagnostics/README.md) own those tools'
contracts. Log fixtures must preserve byte/UTF-16 decoding, bounds, action
counts, and explicit failures without turning analysis into an install verdict.

Command/workflow tests use fake tools, small PE fixtures, disposable Git
repositories, and stubbed GitHub calls. They must preserve argv quoting,
environment selection, exact source identity, and failures without real
publication. A local tag fixture does not prove the hosted release write path.

Installer bundle tests check the nine-entry allowlist, companion Setup/MSI
identity, final metadata, compiler/license records, per-entry hashes, and
agreement with the direct named MSI. Those static postconditions remain
separate from `test-installer-lifecycle`, which executes full normal servicing,
actual-mutation failure, supported recovery, and final owned cleanup under
the guarded disposable-account harness.

## Timing fixtures and negative evidence

A deadline test must reach its intended phase before triggering the cause.
Report setup or window creation can consume a short deadline before an
interrupt case starts; expiry must then remain expiry, not be relabeled as
cancellation. A post-goal fault test must observe its actual ACK/goal barrier
before applying timeout, cancellation, or window failure.

Likewise, a receiver rejecting complete invalid/over-budget TCP input may
close before the peer's later half-close. Test rejection directly with the
source write-open; retain successful half-close requirements in genuinely
EOF-driven cases. Do not ignore `ENOTCONN` or turn receiver rejection into
clean EOF.

These were distinct historical fixture failures, not blanket data-race or
runtime defects. Their original failed runs, test-only corrections, and
subsequent exact-source checks remain in the private investigation record,
which is not imported into this source-only repository.
Corrections do not erase prior failures, weaken production deadlines, or
prove every timing case flake-free.

## Benchmarks

`make benchmark` uses invented processing inputs and fixed sample conditions.
Retain source, compiler/platform/CPU, exact command, and raw output. Compare
clean base/head checkouts on the same runner with identical flags; unrelated
hosted timings are not a regression threshold.

`SourceFrameInspect` measures strict source framing/inspection.
`SplitMerge` measures already-framed processing, not the intentional 600 ms
hold. `ReceiptFrameResponse` measures response validation, not peer receipt
semantics. `ForwardReceiptPipe` measures owner forwarding/admission and
invented pipe scheduling, not real ingress, pacing, retry, or flight.
`IdleTick` measures owner deadline checks, not OS wakeups, idle CPU, or power.

An earlier private six-sample comparison established a bounded split-processing
optimization, not a general live or native-Windows latency improvement.
Benchmarks are informational; there is no timing gate or live target.

## Documentation and equipment

Documentation changes require relative-link, anchor, case, and example
checks against the actual CLI/Make/script contract. They do not require a
full Go/race build when executable behavior is unchanged.

Software checks do not authorize live equipment, diagnostic execution,
activation changes, or capture. Consult [Qualification](QUALIFICATION.md)
before proposing a real trial, and retain exact artifact identity, negative
outcomes, and observation limits in dated evidence.
