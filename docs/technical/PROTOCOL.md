# Protocol and delivery

[Technical manual](README.md) / Protocol

## Framing and native data

TCP supplies a byte stream, not JSON message boundaries. Each reader therefore
frames objects incrementally across fragmentation, coalescing, whitespace,
escapes, and split UTF-8. Source objects and relayed target responses are bare
JSON. Target shot writes append one LF; target responses need not be
newline-delimited.

Frames must be valid UTF-8 and unambiguous JSON objects. Duplicate keys,
including escaped spellings and keys in otherwise ignored data, are rejected.
The per-frame limit is 1 MiB and the nesting limit is 64. Once an object starts,
it must complete within 5 seconds; an idle connection has no inbound timeout.
These are framing limits, not the research probe's total-run receive budget.

The native source envelope contains `DeviceID`, `Units`, `ShotNumber`,
`APIversion`, `ClubIndex`, `BallData`, `ClubData`, and `ShotDataOptions`.
Device, unit, and API values are strings. Shot and club indices are exact
integer JSON literals, not numeric strings or floating-point approximations.
The five native option flags must be booleans. A data member marked present
must be an object; an absent ball or club may be represented by null.
A heartbeat cannot simultaneously declare ball or club data.

Validation does not reserialize the envelope. Supplied numeric text, unknown
fields, units, absent optional values, nulls, and false post-impact flags remain
unchanged. The bridge does not fill missing measurements, compute spin or
flight, or convert units. The only merge edits are described below.

## Destination profiles

Infinite Tees accepts the existing native envelope and forwards its bytes plus
LF. Its response profile recognizes integer `Code` values 200, 201, 202, 203,
500, and 501. A response also requires a string `Message`; an optional `Player`
must be an object or null. Player values are not interpreted as readiness.

GSPro OpenAPI is the public
[Open Connect v1 interface](https://gsprogolf.com/GSProConnectV1.html) at port
921, not the native vendor integration historically observed at port 9050.
The implemented profile requires `"APIversion":"1"`, `"Units":"Yards"`,
and numeric `Speed`, `SpinAxis`, `HLA`, and `VLA` in `BallData`. It requires
numeric `TotalSpin` if that member is supplied, otherwise both numeric
`BackSpin` and `SideSpin`. Unsupported data is discarded explicitly, without
zeros, conversion, or a success ACK.

GSPro shot bytes are also sent unchanged plus LF. Its response profile admits
200, 201, and terminal failures 500 through 599. Other codes, including 202
and 203, invalidate the destination instead of creating a ready state.
Acceptance of these fields, framing, and responses by an installed GSPro
build remains unqualified.

## Split shots and duplicate suppression

A combined ball/club object requires no merge hold. A ball-only candidate waits
up to 600 ms for a club-only object with the same exact shot ID in the same
source epoch. Merge replaces only `ClubData` and `ContainsClubData` in the
original ball bytes. All other bytes retain their original representation.

A newer ball releases the older candidate in order. Status and heartbeat
traffic is counted locally but neither forwarded nor acknowledged; it does
not release or extend the hold. At timeout the original ball-only object can
forward only if its original eligibility still holds. Late or unmatched club
data is omitted visibly and cannot create a second shot.

A 256-entry recent-ID ledger uses SHA-256 keys to suppress repeated ball IDs
within the current Labs session. Target recovery and destination changes
retain this ledger; actual source departure clears it. The ledger does not
compare measurements or provide unlimited historical deduplication. Different
valid IDs remain different shots even if their measurements look identical.

## Eligibility and observation

The receipt lane admits only newly eligible work. Each opportunity receives a
monotonically new ticket, separate from the target generation. No ticket is
available while the target is down, switching, or already waiting on a shot.

A frame records the ticket at its first read observation, including buffered
read-ahead. Subsequent reads can invalidate that origin, but cannot assign a
new one. A held split candidate keeps the same ticket. Thus eligible A,
then busy, then eligible B on one healthy socket cannot revive a partial or
buffered ball from opportunity A. A busy, unavailable, or stale arrival is
discarded with a diagnostic and no success acknowledgment.

The ticket records when bytes were observed, not the physical strike time.
The bridge cannot date bytes it has not read or infer that Labs consumed an
upstream reply simply because a local write completed.

## Receipts and acknowledgments

The owner registers the pending receipt before starting the target writer.
Acceptance requires both a complete successful shot write and an applicable
actual target Code 200 observed after that generation's writer is armed.
An early applicable 200 waits for write completion. Neither a write alone,
player information, nor an elapsed timer settles the receipt.

After acceptance, the same healthy connection can admit the next fresh shot.
There is no per-shot reconnect, resend, post-shot ready timer, or required
201 transition. Real applicable status/player/error responses are relayed
unchanged in order; a terminal failure retires the target without sending a
fabricated 200.

Target responses contain no shot ID. A delayed or unsolicited 200 first
observed with a fresh armed write may be indistinguishable from that write's
receipt. One pending shot and observation/generation guards reduce ambiguity
but cannot prove per-ID correlation, exactly-once delivery, peer consumption,
or visible flight. Game readiness remains unknown.

Writes have a 2-second deadline and receipts a 5-second deadline.
A failed or partial write, invalid response, or expired receipt retires the
target and records uncertain delivery where applicable. It never resends the
uncertain shot. Retry uses a 2-second dial timeout and 2/4/8/10-second capped
backoff, independently of swings. Idle unsolicited 200s, known stale prefixes,
and retired-generation callbacks are rejected conservatively.

## Changing destination

A destination request closes admission immediately and discards any held,
unsent shot. The old pending write/receipt keeps its existing deadlines.
Queued and in-flight upstream writes must complete or be retired, including a
writer awaiting admission after pacing. New non-200 old-game responses are
omitted during this drain so that status traffic cannot extend it indefinitely.

Only after this barrier does the old target retire and the selected profile
apply. Labs, the identity window, source epoch, and duplicate ledger remain
intact. The next profile gets a new generation and eligibility opportunity;
old read-ahead and player information are not replayed into it.
Standby keeps the source but does not dial or accumulate shots.

Stop swinging during a change. The barrier cannot prove that Labs consumed a
completed old reply, and first-putt/handedness context after a real cross-game
switch still requires qualification. A source fault during the drain can
block switching until actual source departure; selection is not a teardown
override.

## Return-path bounds and accounting

Upstream replies carry their source epoch and target generation. After pacing,
the state owner admits only current work. An admitted write cannot safely be
retracted mid-JSON if its target retires; an actual completion after retirement
is recorded separately.

`TargetAccept` counts applicable observed target receipts. `SourceACK` counts
complete local upstream 200 writes, not peer consumption. If an accepted 200
is abandoned before writing, `AcceptedACKAbandoned` and `ReplyDiscarded`
record the loss without erasing acceptance or incrementing `SourceACK`.
`Uncertain` is a subset of `Dropped`, not an additional total.

The reply queue allows at most 16 objects and 2 MiB, including in-flight work.
Overflow quarantines the source visibly rather than silently evicting replies.
Other bounds are one held candidate, one pending receipt, 256 recent IDs,
64 recent events, and a 32-event diagnostic queue. Readers retain one framed
event and 4 KiB read-ahead.

Upstream writes are spaced by at least 60 ms after the preceding write;
TCP NoDelay is also used. These empirical compatibility aids do not guarantee
TCP packet or message boundaries. See [Lifecycle](LIFECYCLE.md) for quarantine
and [Qualification](QUALIFICATION.md) for the limits of real-equipment evidence.
