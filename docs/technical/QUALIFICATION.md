# Qualification

[Technical manual](README.md) / Qualification

## Reading an evidence claim

Automated fixtures, static package inspection, disposable Windows servicing,
and real-equipment observations establish different things. A claim belongs
to its exact source, artifact, version, scenario, and observation method.
Passing CI does not turn an unsigned development package into a release or
qualify later binaries on a golf PC.

The current source implements remembered IT/GSPro OpenAPI/Standby selection
and recovery-first-v2 installation. The public v0.1.0 tag and release are now
published; their bounded verification is recorded below. There is no official
vendor endorsement or live GSPro/cross-game qualification.
[Testing](TESTING.md) describes software coverage;
the [research boundary](research/README.md) explains what is retained publicly.
This repository starts from a source-only bootstrap without the earlier private
history, run artifacts or investigation ledgers. The historical installer and
bridge observations below concern older, separately identified instruments and
candidates, not this bootstrap commit or binaries rebuilt from it.
Original private records remain unchanged;
no old hash, run or issue is relabeled as belonging to this repository.

## Public release publication

[v0.1.0](https://github.com/devigned/vx-bridge/releases/tag/v0.1.0) targets public
source commit `5401cf26d19238b04ba3dbb8ecfa60442616ca79`. Its annotated tag was
cryptographically signed with the owner's existing Git signing identity and
verified locally and by GitHub. The
[tag-triggered release run](https://github.com/devigned/vx-bridge/actions/runs/41832770003)
completed its build, static package verification and publication jobs.

The published application ZIP, installer ZIP, direct matching MSI and three
SHA-256 files were downloaded and verified. All six files matched that run's
exact immutable artifact bytes and GitHub asset digests. The application manifest
identified the tagged source and v0.1.0; the installer ZIP had the expected nine
flat entries, and its `bridge.msi` matched the direct MSI.

No product executable or installer was run for these checks. This establishes
publication and artifact integrity, not interactive installation, Windows sign-in,
installer servicing or real Labs/game compatibility for v0.1.0. The emitted
Windows programs and installer are not Authenticode-signed; signed source commits
and tags do not sign those files.

## Installer observations

A prior private candidate passed ordinary CI and a separately authorized
hosted lifecycle under **recovery-first-v2**. Later documentation/source checks
were not another installer execution. These are bounded prior observations,
not qualification of the renamed public source or a public release.

The lifecycle used genuine Go v0.0.1/v0.0.2 payloads with one native core,
distinct A/B ProductCodes, generated PackageCodes, and separately identified
fault-B media. It ran as a fresh independent standard user with a loaded
profile, unelevated default token, and no Administrators SID, including
deny-only membership. Only the approved temporary `DisableMSI=0` policy
prerequisite was adjusted; both AlwaysInstallElevated settings remained
effectively zero and the exact original policy state was restored.

Fresh A installed with one per-user unmanaged product, six matching files,
receipt/cache, no Pending, startup off, and fresh destination defaults.
Repair restored a missing control executable and damaged known LICENSE
while preserving exact configuration, log, and Run bytes. Normal A-to-B
upgrade used one Setup MSI call and standard old removal after successful
InstallFinalize, leaving only B's verified product/client state.
Full normal Uninstall proved owned file/cache/settings/log/shortcut/
registration absence while preserving foreign sentinels and original media.

The fault case failed **after actual file and registry mutation**, at
post-Execute Type19. MSI returned 1603; Setup reported nonsuccess/unusable,
retained Pending, and did not launch a bridge. Selected-B Repair returned
zero from MSI and repaired files but left A/B registrations. Native
postconditions correctly refused usable success.

Recovery then removed original A through standard MSI, preserving B's
shared files/settings/Run and denial; performed original-B full Uninstall;
installed selected B fresh; and completed a final full Uninstall.
Actual catalogs, component clients, payloads, receipts, passive cache,
preferences, and final absence were audited. No manual registry/client
purge or account/profile destruction supplied cleanup proof.

This qualifies normal servicing and that explicit recovery route on the
recorded same-source/native-core hosted candidate. It is not an
atomic-upgrade guarantee, a general rollback fix, or a claim that Repair
alone resolved the induced failure. Earlier strict failed runs retain their
original outcomes and raw diagnostics in the private record; they are not
superseded by this summary or republished in the source-only repository.

Interactive Install/Repair/Cancel, client Windows and Apps UI, Start now,
actual sign-in, attached-Labs maintenance, different native cores, earlier
prototype takeover, and arbitrary interruptions remain unqualified.
The [installer chapter](INSTALLER.md) separates implemented safeguards from
these observations.

## Bridge and desktop observations

An earlier console trial
forwarded one combined normal shot and one combined putt through a persistent
target, with exact object bytes plus LF and owner-confirmed single outcomes.
It did not exercise split merge; two timing samples were not a latency
distribution, and its saved capture omitted the shutdown tail.

The later desktop trial used its separately recorded GUI/control pair.
It observed a real split normal shot, a putt, tray/Open Logs, startup off,
and repeated attached/pending Quit with bounded survival. It had no payload
capture and did not establish putt shape, wire departure, or use of every
response code.

An earlier recovery trial
kept Labs across target startup, scene round trip, exit, and restart.
An outage shot had no send/ACK/replay; a fresh split shot later produced one
flight. Reset preceded the recorded Quit. Separate source-free Windows
observations exercised keyboard/current-scaling readability, Explorer icon
restoration with the same host, and sign-in registration enable/removal.
They did not include actual sign-out/sign-in policy execution.

These qualify only the recorded older instruments and bounded scenarios.
They do not reproduce the current multi-game/native-Setup candidate.
Later native trials recorded Labs 1.11.17.1; earlier October inventory recorded
1.11.16.0. E9 did not record its active Labs version, and update timing is
unknown. Firmware and exact in-app game/connector releases remain unresolved
where the original reports say so; executable resource metadata is not an
in-app release number.

## Safety incident

In an earlier private trial (E14), a research
receiver reached its two-shot goal and exited zero after receiver-first
teardown; Labs then crashed. That result is not safe compatibility, and
research v2 remains stopped. The cause was not isolated.

E15 used a different source-first probe. It observed a reset, bounded Labs
survival, and actual exit 9: clean EOF was false and teardown risk remained
true. It was not a clean-EOF success. The persistent bridge's reset handling
does not rewrite the probe's EOF-only contract or authorize another trial.
[Lifecycle](LIFECYCLE.md) explains the resulting conservative guard.

## Limits and future evidence

Readiness remains unknown. Target responses lack shot IDs, so delayed-200
ambiguity remains despite the one-receipt lane. No observation establishes
exactly-once delivery, arbitrary forced-teardown safety, or live GSPro
framing/fields/units, receipts, putting/handedness, and bidirectional switching.

Long sessions, partial/uncertain live in-flight recovery, all source-reconnect
modes, sleep/resume, broad/dynamic DPI, attached-source Explorer restoration,
general installer interruptions and Authenticode signing also remain outside
completed qualification. The publication check above does not close these gaps.

New real-equipment trials require separate owner approval for an exact
artifact, scope, risks, and cleanup. Record goal attainment, actual departure,
classification, local cleanup, final/nonfinal status, process outcome, visual
confirmation, and bounded peer health separately. Preserve first failures and
raw evidence; do not infer unobserved packet direction, response codes,
physical shots, or causal mechanisms. Private data remains outside Git.
