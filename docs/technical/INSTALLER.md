# Installation and recovery

[Technical manual](README.md) / Installation

## Scope

The native Setup candidate installs a per-user Windows x64 bridge. It runs
normally as the installing user, not as administrator. The application uses
local admission checks; Windows Installer is involved only in explicit
installation and maintenance, never in ordinary direct, shortcut, or sign-in
startup.

The selected contract is **recovery-first-v2**. An earlier private candidate
passed conventional MSI install, repair, upgrade and full Uninstall checks on
a disposable host with one recorded source/native-core pair. That result does not
qualify the public [v0.1.0 installer](../releases/v0.1.0.md) or rebuilt binaries.
Interactive/client-PC operation, actual sign-in, Apps UI, Start bridge and
attached-Labs maintenance remain unqualified. See
[Qualification](QUALIFICATION.md#installer-observations) for the recorded scope.

## Self-contained media

Future releases upload one version-named setup EXE and its `.exe.sha256`;
GitHub supplies source archives separately. The existing v0.1.0 downloads
remain in their original ZIP/MSI format.

A thin native outer EXE carries the unchanged verified `setup.exe`/`bridge.msi`
pair, authoring metadata, notices, checksums and the original application
archive. Inner Setup remains hash-bound in the MSI. The outer EXE neither
implements a second MSI engine nor changes the persisted compatibility
identities.

The outer launcher refuses elevation. It validates its exact-source overlay
and all full-byte hashes before creating a fresh GUID-named staging directory
under the current user's canonical LocalAppData. Ancestors are pinned;
directories/files reject reparse points, files reject hardlinks, and new
files use exclusive creation with an installing-user/System-only ACL.
Reopened file identity and SHA-256 must agree, and read pins deny writes and
deletion through the child process's entire lifetime. MSI receipt/source
and inner Setup-owner hash are checked before dispatch.

Each invocation has its own staging directory. Inner Setup's existing owner
mutex and admission rules still serialize actual servicing. The outer launcher
passes the embedded MSI explicitly, not a possibly older cached selection.
Cancellation and guarded Quit remain inner Setup's responsibility. The
launcher waits for the child, never terminates it, and returns its result.
Verified cleanup removes only the objects created by that invocation, checking
file identity again under pinned ancestors. Unexpected contents or cleanup
failure reports nonsuccess; it does not claim rollback. An unknown child
lifetime preserves its media. A forcibly terminated outer process can leave
an orphan staging folder; no later invocation scans or recursively purges it.

Successful installation caches the inner Setup and exact MSI in the existing
passive maintenance root. Repair/Uninstall use those owned records, not the
download location or another automatic extraction. The outer download is
neither adopted into the cache nor removed by Uninstall. Ordinary application
startup is unchanged and never invokes the bundle or MSI.

For inspection or exceptional original-media recovery, `/extract` writes
the verified internal files to an explicitly selected new absolute local
directory without running Setup. It never overwrites an existing folder.
See [Setup commands](COMMANDS.md#setup1). Keep that original media together;
an MSI extracted this way is not a standalone-install interface. Installer
and compiler notices remain available inside the EXE and through extraction;
the application license/notices also remain in the six-file installed payload.

## Managed-copy admission

The application root is fixed:

```text
%LOCALAPPDATA%\Programs\VxInfiniteTeesBridge\
```

It contains exactly `GSPconnect.exe`, `vxbridge.exe`, `LICENSE`,
`THIRD_PARTY_NOTICES.txt`, `build-manifest.json`, and `SHA256SUMS`.
The passive maintenance directory is separate:

```text
%LOCALAPPDATA%\VxInfiniteTeesBridge\installer\
HKCU\Software\VxInfiniteTeesBridge\Installer
```

The registry receipt binds product, package, numeric version, native-core
revision, and six payload hashes. Any Pending value, unreadable or corrupt
receipt, inconsistent build, or unexpected payload refuses a managed start
before control, source, or window resources are created.

Admission uses local state and exact object checks, not MSI APIs. It retains
payload pins and a no-sharing host lease through process exit, including the
interval after Go cleanup. Missing or interrupted state produces **Repair
required**, not automatic repair or a usable-looking fallback. A bounded
read-only control query can identify a healthy existing host when the lease
is already held; duplicate startup is not by itself evidence of corruption.
The separate status/Quit roles remain available.

A portable copy outside the fixed managed root is neither adopted nor fenced
by this receipt. Migration must first use normal Labs Disconnect and guarded
Quit, then deal explicitly with that copy and its Run entry. Setup does not
overwrite an unmanaged folder or claim that an old portable host honors the
new admission contract.

## Maintenance and guarded Quit

Setup verifies the selected MSI, recorded source, current-user unmanaged
registration, package/version, fixed paths, and owned state before requesting
Quit or changing payloads. A downgrade, changed same-version normal media,
different native core, foreign startup value, or unknown ownership refuses
preflight rather than stopping a working host first.

Setup publishes durable Pending denial before waiting for quiescence. New
managed launches then refuse while an existing host continues its source-first
lifetime. If the owned host is running, Setup authenticates its control
connection against the expected process instance, user, image, source, and
receipt, and requests only guarded Quit. It waits for actual clean process
exit and exclusive lifetime access, not a snapshot or disappearance of the
tray.

When Labs is attached or attachment is uncertain, the user sees **Disconnect
in ProTee Labs to continue**. Quiet operation must refuse within its bounded
wait rather than force teardown or wait for unattended user action indefinitely.
An unowned/portable host is not sent Quit. There is no Force Close, automatic
Labs/game control, termination by name, app restart, or reboot route.

Cancellation requests normal installer handling; it does not promise to undo
an already accepted host Quit. Failed or unverified mutation retains Pending
for explicit recovery. The [lifecycle guard](LIFECYCLE.md) remains authoritative
even when an installer wants to proceed.

## Install, repair, and compatible upgrade

Setup makes a single conventional `MsiInstallProductW` call for the selected
package. It has no custom outer Begin/End transaction, manual old-remove/
new-install sequence, or compensating advertised-old-package reconstruction.
Built-in MSI rollback remains enabled on a best-effort basis.

Same-native-core versions use stable features/components, increasing versions,
new ProductCodes, and generated PackageCodes. Standard
`RemoveExistingProducts` runs **after successful InstallFinalize**. The new
package is committed before old removal; a removal failure may leave it
committed and require recovery. This is deliberately non-atomic across
versions.

The old-removal child validates the actual Pending parent binding and
parent-owned quiescence. It preserves shared settings, cache, and startup
instead of invoking full-Uninstall cleanup or releasing the parent's denial.
`RemoveExistingProducts` is a first-install upgrade action, not a remedy run
by selected-version maintenance.

Same-package Repair uses `REINSTALL=ALL REINSTALLMODE=amus` for installed
features, or `ADDLOCAL=Bridge REINSTALLMODE=amus` for absent/advertised features.
It does not force recaching changed MSI bytes. Repair and compatible upgrades
preserve owned configuration, logs, game choice, and sign-in state.

Success requires a consistent selected catalog, exact payload and receipt,
no extra known product/client registrations, and no Pending. A successful
MSI API return is necessary but insufficient. Remaining catalogs, unknown
cleanup, incomplete publication, or result-recording failure are not usable
completion and do not permit implicit launch.

## Recovery after failure

A failed operation is not guaranteed to restore the old version. Pending
keeps ordinary managed startup denied until supported maintenance proves a
consistent selected version or complete removal.

Repair is appropriate only if it can restore the selected files, receipt,
and catalog with no Pending or extra registrations while preserving settings.
After an interrupted upgrade, selected-B Repair may restore B's files but
leave both A and B registered. That result is still **Repair required**,
even if MSI returns zero.

The supported alternative is complete Uninstall followed by fresh Install.
Where known A/B remnants exist, use their verified original media: remove the
fully installed old client through standard MSI, preserving the new shared
resources and denial, then perform full current-product Uninstall.
Only complete owned catalog/data absence permits fresh installation.
An intermediate old-product removal is not usable recovery.

The recorded fault trial followed that explicit route: original A removal,
original B full Uninstall, fresh selected B, and final full Uninstall.
No component action state, registry client record, account, or profile was
manually purged to obtain a pass. Unknown media, ownership, remaining clients,
or partial cleanup still refuses. The
[qualification summary](QUALIFICATION.md#installer-observations) distinguishes
the failed Repair attempt from completed recovery.

## Full Uninstall

Windows Installer owns conventional `REMOVE=ALL`, standard sequencing,
rollback, cached-product servicing, and temporary action resources.
The standard Installed apps entry owns Uninstall; raw ARP Modify/Repair is
disabled, and **VX Bridge Setup** is the explicit Start-menu repair action.
Cached Setup `/remove` gives directions to Installed apps rather than trying
to delete its own running executable. Original downloads are not required
for normal Uninstall.

Uninstall verifies actual-session current-user registration, cached package
identity, feature removal state, canonical Known Folder paths, and bounded
file/registry inventory. It schedules literal-name removal rows, not wildcard
or recursive deletion. Known damaged filenames are replaceable/removable only
within verified owned authority. Unknown/excess names, reparse points,
hardlinks, and foreign Run/cache ownership refuse rather than truncate cleanup.

Complete removal includes the six application files, passive Setup and MSI
cache, maintenance logs/state, destination/settings and recognized partial
preference files, the five bridge log slots, all three owned shortcuts, owned Run
registration, receipt/Pending, and known product/component clients.
Validated empty application directories and registry keys are removed too.
The shared `LocalAppData\Programs` parent is not owned or removed.
Original user downloads, independent exports/development archives, vendor/
game files, and foreign state remain untouched.

The nonadvertised shortcuts are **VX Bridge**, **VX Bridge Setup**, and
**VX Bridge Diagnostics**. The support shortcut targets the existing
`vxbridge.exe --diagnostics-dialog`; it adds no seventh payload file. Its
exact target/arguments are part of shortcut ownership and uninstall inventory.
Install/Repair/compatible-upgrade authoring and static MSI inspection require
it, and full removal checks its absence. Reporting does not require successful
host admission or invoke Windows Installer.

Removal checks use callback-scoped handles. Setup retains relevant ancestor,
source, owner, and host protections but hands off the app-root handle for
standard engine work, then revalidates identity. This is not a continuous
custom filesystem pin or an all-fault restoration guarantee. Rollback does
not recreate a missing directory merely to manufacture continuity.
Reboot requests, leftover owned state, or unverified absence prevent a
full-cleanup success claim.

## Startup and post-install launch

**Auto-launch on Windows Startup** has the exact help **Starts when you sign
in to Windows.** It is off on a fresh install, opt-in, and may preserve a
verified owned maintenance preference. Setup journals the exact Run state
and changes/restores it under the startup mutex without taking over foreign
values or modifying `StartupApproved`.

The separate post-success choice is **Start bridge** or **Not now**, defaulting
to Not now. It occurs only after verified usable completion and maintenance
lock release. Launch uses the normal user context outside a Setup job and the
ordinary desktop entry point; it neither changes sign-in consent nor forces
Infinite Tees over the saved destination. Quiet or failed Setup does not
launch the application. A launch failure is reported separately from a
completed installation.

## Build tooling and static verification

The selected pins are in [installer/toolchain.json](../../installer/toolchain.json):
Go from [.go-version](../../.go-version), the reviewed MinGW-w64 compiler/
binutils, and official WiX 3.14.1 under MS-RL. WiX's public community security
support ended on 2025-02-06; this is not a maintained-toolchain or private
extended-support claim.

Native outer bundle, Setup and actions are C code; Setup/actions use the actual
MSI handle. WiX
candle/light need a compatible .NET Framework on the **build machine**; the build checks for
it and does not install a runtime. The emitted native code imports Windows
system DLLs, not a separately shipped CLR or compiler runtime.

The build consumes the authoritative six-entry Go ZIP rather than relinking
the application or harvesting arbitrary files. Prepared metadata has no
guessed PackageCode: WiX generates it, and actual MSI Summary Information
property 9 is bound to the delivered MSI hash in final metadata.
The bundle adds no hash cycle: the native outer stub is built separately,
then combined with the already authored, hash-bound inner Setup/MSI pair.
An independently rebuilt same-source/same-version MSI can have a different
PackageCode and is not equivalent Repair media. Its full-media hash is also
not a hash of Windows Installer's cached `LocalPackage`.

Authoring retains warnings-as-errors with narrowly checked exceptions:
LGHT1008 only for the exact omitted advertise actions `PublishFeatures`,
`PublishProduct`, and `CreateShortcuts`; ICE91 only for the checked per-user
limited x64 profile; ICE96 only for that intentionally nonadvertisable profile.
Source-policy and complete emitted-table/stream/CAB checks enforce the
exceptions. All other warning/ICE validation remains enabled. This is not
exception-free validation or proof that every raw advertisement request has
no effects.

Use [Command reference](COMMANDS.md#installer-builds) for actual build inputs,
and [Automation](AUTOMATION.md) for workflow/artifact identity.
Compilation and static inspection do not execute callbacks, install a
product, exercise UI, or qualify Labs/game behavior.

## Investigation history

The [research boundary](research/README.md#installer-and-automation-records)
explains why original transaction prototypes, independent rollback controls,
failed lifecycle runs and raw evidence remain in the private record. They retain
their original criteria and outcomes. Recovery-first-v2 supersedes the old atomic-restoration
requirement; it does not retroactively repair failed tests or prove the
Windows rollback mechanism reliable in every environment.

Preserve [MIT](../../LICENSE), [third-party notices](../../THIRD_PARTY_NOTICES.txt),
and [installer notices](../../installer/NOTICE.txt). Historical source/licensing
investigations do not change the selected build stack or grant publication,
installer execution, or equipment access.
