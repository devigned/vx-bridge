# Diagnostic reports

[Technical manual](README.md) / Diagnostics

The [reporting guide](../REPORTING.md) owns submission instructions.
`internal/diagnostics.Collector` is shared by the tray and the distinct
`vxbridge.exe --diagnostics` / `--diagnostics-dialog` modes. The installed
**VX Bridge Diagnostics** shortcut selects the latter. These modes precede
managed host admission and never acquire its lifetime lease or open a source
listener. Collection does not invoke MSI, Setup, repair, Quit, game selection,
startup changes, vendor discovery or network upload.

## Format and limits

Schema 1 ZIPs contain exactly four entries:

| Entry | Contents |
|---|---|
| `summary.txt` | Readable identity, host/source availability and privacy/interpretation notes |
| `manifest.json` | Schema, reporter build, UTC collection time, partial flag and per-source results |
| `report.json` | Typed Windows, host, settings, receipt/payload and installer-result observations |
| `events.jsonl` | Reserialized, sanitized records from existing bridge log slots |

Reporter build, running-host build and installed receipt identity are separate.
The structured release is distinct from the retained prototype/protocol label.
Older hosts/logs without a structured release report it as unavailable; the
reporter must not supply its own release as their identity.

Collection waits at most eight seconds, with a one-second status-only query.
An earlier caller cancellation is an export failure, not a successful partial
save. Deadline-limited source availability produces an explicitly partial
report. There is one source worker per process; an OS call that outlives the
deadline keeps later collections busy until it returns. A context cannot
interrupt all kernel filesystem operations. Collection and native save I/O
never run on the engine or tray owner thread. A separate support worker keeps
ordinary desktop actions and status/Quit polling independent of modal dialogs.

Each of the five log slots is read at most 2 MiB; lines are limited to 2 KiB.
At most 2,048 newest timestamped log records survive the global event budget.
The host retains at most 64 recent events. Invalid/incomplete records are
omitted and counted. Concurrent append/rotation can give a mixed point-in-time
view, not an atomic history. UTC timestamps, not slot names, establish order.
The exporter never flushes or rotates the live logger.

Settings are limited to 1 KiB, the existing result log to 256 KiB and each
fixed payload file to 32 MiB for local hash comparison. The finished ZIP is
limited to 2 MiB. Sources report included/unavailable/skipped states and stable
failure codes; omitted records and truncation remain visible. Missing logs,
settings or a refused control query do not mean healthy or fresh installation.

## Source and privacy boundary

Native known folders select only fixed current-user bridge paths. The exporter
reuses managed receipt decoding and exact Windows file/ancestor pin checks.
Reads reject reparse points, hardlinks, redirected final paths and nonregular
files; they permit concurrent log writes but not deletion while pinned.
Unsafe or unavailable inputs become source-level codes, never copied OS error
text. No environment, registry/process dump, username, SID, device ID, raw
shot/player data, private path, license, credential, vendor log or capture is
collected.

Records are decoded and constructed again from allowlisted fields, not copied
as raw log bytes. Build text has strict known-value/version/revision shapes.
Unknown reasons use the existing redacted event hash. Local shot aliases are
omitted. Only explicit IPv4 loopback endpoint/port text survives. Windows facts
are numeric major/minor/build values from the native version API, without a
machine name or hardware inventory.

Installer output uses selected operation/boolean/numeric/source-revision fields from
`setup-result.log`; it excludes the verbose MSI log and debug/authentication
text, including hexadecimal UI records. No MSI API is called. A result recorded
as usable describes that operation, not current installation health.
The read-only installed receipt observation includes Pending presence and
six fixed file hash outcomes. Matches are against the local receipt, not
publisher authentication, current MSI catalog consistency or a passed admission.
The diagnostic tool does not acquire or create `host.lock`.

The native save dialog and automation mode require a new absolute file on a
local fixed/removable drive with existing, verified ancestors. They reject
UNC/network, reparse and existing-file outputs. Files are created exclusively;
there is no silent replacement or fallback directory. Write/sync/close errors
are failures. An incomplete file may remain for explicit user cleanup rather
than deleting a possibly changed pathname. Nothing is sent; review/attachment
is manual. Reports are independent exports, outside Uninstall authority.

## Maintainer triage

Read the problem and impact first. Identify the area: installation/admission,
desktop/startup, Labs source, selected target/switching, delivery or diagnostics.
Use existing `bug`, `enhancement`, `documentation`, `question` or `accessibility`
labels where appropriate; no special triage label is required.

Check for duplicates and confirm the exact release, selected game, timing and
expected/observed result. Request only missing facts that could change the next
step. A ZIP is optional, including when export itself fails. Never request raw
vendor/MSI logs or captures in a public issue.

Compare reporter/host/receipt identities before drawing conclusions. Read source
availability and log-loss/truncation before interpreting absent events.
Correlate the approximate user time and zone with UTC events. Distinguish target
acceptance, local response writes and actual game behavior. Reproduce with
synthetic fixtures first; an issue or successful source check does not authorize
real simulator, vendor, account or installer qualification work.

## Validation boundary

Portable tests cover report shape, size, source failures, privacy allowlists,
malformed records, rotation, cancellation, busy workers and output refusal.
Windows fixtures check native ABI and owned-file pin semantics. Static installer
checks cover the support shortcut and uninstall inventory. None qualifies the
interactive save dialog, accessibility/DPI, physical Labs/game use or real
installer servicing; those require separate approval and Windows observation.
