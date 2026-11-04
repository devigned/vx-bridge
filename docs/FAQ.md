# VX Bridge: frequently asked questions

[User guides](README.md) / FAQ

## Where is the installer?

[Open the latest release](https://github.com/devigned/vx-bridge/releases/latest).
Download `vx-bridge-installer-windows-amd64-vX.Y.Z.exe` and open it normally,
not as administrator. It contains Setup, the matching MSI and support files.
The `.exe.sha256` file is a checksum, not a second installer.

Older releases, including [v0.1.0](releases/v0.1.0.md), retain their original
ZIP downloads and extraction instructions. Source archives and **BUILD ONLY**
artifacts are not the installer. See [file types](INSTALL.md#choose-the-right-files) and
[tested scope](#which-versions-and-features-have-been-checked).

## Windows blocks Setup

Stop and [report the warning](https://github.com/devigned/vx-bridge/issues).
Do not disable security protections or bypass it. The Windows programs and
installer are not publisher-signed (Authenticode). Signed Git commits and tags
do not sign those files.

## ProTee Labs won't connect

First confirm Labs can see your VX and that your existing connection access is
active. Open VX Bridge on your signed-in Windows desktop. Check the bridge
icon's **ProTee Labs** connection text.

If the real GSPro connector is running, disconnect in Labs normally and close
that connector through its own controls before trying this bridge. Both receivers
can't own the same source connection. Don't replace the vendor executable or
force-close it while Labs is attached.

Labs may need its normal Connect action; automatic bridge startup doesn't
guarantee Labs auto-connect. If it still won't attach, report your Labs version
and the visible error. Don't reset activation or change vendor files to work
around an unexplained connection failure.

## Infinite Tees stays unavailable

Open Infinite Tees on the same PC and enter a playable scene. Check that its
shot-input connection is available and that its configured port matches the
bridge. The bridge retries without needing another swing.

If you recently changed the port, see the
[alternate-port instructions](#my-infinite-tees-connection-uses-a-different-port).
Until the target reconnects, shots are lost rather than saved for later.

## GSPro stays unavailable

Select **Use GSPro OpenAPI** in VX Bridge and run both GSPro and its public
**OpenAPI / APIv1 Connect** application on the same PC. VX Bridge uses local
port **921**, not the native ProTee VX/VGPconnect integration on **9050**.
It does not open either receiver for you. Keep licenses and activation in
place and do not run the competing Labs-facing native VX receiver.

Missing or unsupported shot data is reported as missed, not filled in. This
path still needs live testing for the current candidate; an OpenAPI application
remaining open does not prove that the game behind it is healthy or ready.
The [protocol guide](technical/PROTOCOL.md) records the supported data profile.

## Which game receives shots if both are open?

Only the selected game. VX Bridge remembers your tray choice; it does not
detect the active game, follow the foreground window, mirror shots or fail over
to another game. If the selected game is unavailable, wait for
recovery before swinging. **Standby** deliberately sends to neither game.

## Can I change games without disconnecting Labs?

Stop swinging, finish the visible result, select the other game and wait for
switching to finish. VX Bridge keeps Labs connected while the current shot
response and pending replies finish or time out. New shots during switching
are discarded, not saved for later.

If switching is blocked by a Labs connection fault, use normal Labs Disconnect; repeated
choices cannot force it. Live Infinite Tees/GSPro switching still needs testing.
See [game selection](USAGE.md#choose-a-destination) for the limits.

## Both connections look good. Can I swing?

Not from that indication alone. **Connected (transport only)** means the data
connection is open, not that the selected game is ready for the next shot.
Check the game, confirm you are in the intended scene and wait for the previous flight to finish.
The bridge deliberately says **Game readiness: not reported**.

## A shot was missed or delivery is uncertain

Read **Last error** and check the game before swinging again. An unavailable
game, a previous shot response still pending or a changed connection can cause a
missed shot. The bridge keeps Labs attached, records the loss and never replays it.

An uncertain delivery means the bridge couldn't establish the outcome; the game
may already have received it. Don't assume it was definitely absent or will
appear after reconnecting. Resume with a fresh shot only after checking the game.
The [protocol reference](technical/PROTOCOL.md)
explains why the transport can't guarantee exactly-once delivery.

## The last error remains after a successful reconnect

**Last error** retains the most recent failure even after recovery. Look at the
current connection text, the timestamp and **Last** to distinguish that earlier
event from a new problem. For example, a recorded outage loss isn't an extra
lost shot just because the message remains visible.

## Quit keeps waiting for Labs

Use **Game Options > Disconnect in ProTee Labs**. Quit waits for the actual
connection to close, not just for a click or a changed label.

If Labs still owns the connection, stop playing and use Labs' normal Disconnect
or Exit controls. Don't force the bridge closed or keep clicking Quit to override
the guard. If it continues waiting after normal departure, record the tray/status
error for a report.

The guard exists because forcing a receiver closed was followed by a Labs crash.
Maintainers can read the [lifecycle explanation](technical/LIFECYCLE.md)
and the [safety summary](technical/QUALIFICATION.md#safety-incident).

## The tray icon is missing, or another copy is running

Check hidden notification-area icons first. Opening the bridge a second time
doesn't replace the running host; a duplicate notice leaves it alone.

Don't restart Explorer during an attached session just to try to restore an
icon. If the menu remains inaccessible, the separate `vxbridge.exe` control
tool can check status or request normal guarded Quit. Ask a project maintainer
for help or use the [control reference](technical/DESKTOP.md).
Don't launch an extra `GSPconnect.exe` to perform a control command.

## VX Bridge reports Repair required

Ordinary startup does not run Windows Installer or attempt automatic repair.
Open **VX Bridge Setup** from Start and choose **Repair / Reinstall**.
The bridge need not work first. If Setup cannot find its saved source, use the
original verified installer EXE for that build. Older ZIP releases need their
original matching extracted Setup/MSI pair.

**Repair does not always fix a failed upgrade.** It can restore files while
leaving more than one registered version, so Setup must still refuse to start
the bridge. Windows' automatic rollback is enabled, but a failed operation does
not guarantee that the previous version returned.

Keep the bridge stopped. If Repair cannot complete, use **complete Uninstall,
then a fresh Install**. If multiple versions remain, get a project maintainer's
recorded-version recovery instructions: remove the original recorded old
version, fully uninstall the current version, confirm complete removal, then
install fresh. Removing only one version is not completed recovery.

Normal removal uses **Windows Settings > Apps > Installed apps > VX Bridge >
Uninstall**. A damaged Windows Installer cache may require each recorded
version's verified original MSI with `REMOVE=ALL`. A maintainer can use the
original EXE's `/extract` option to save that media without installing it.
That explicit recovery route is not a raw-MSI install instruction.
Don't guess identities, delete installer
records or manually purge folders to make the remaining copy appear healthy.
Remaining registrations or bridge-owned files are not successful recovery,
even if one command reported success.

Repair preserves settings; full Uninstall removes VX Bridge-owned settings and
logs, not vendor data or independently saved files. Use normal Labs Disconnect
and Quit if a copy is still attached. See the
[maintenance steps](INSTALL.md#maintenance-and-repair-required) and
[recorded test limits](technical/QUALIFICATION.md).

## Open Logs fails or local logs are unavailable

The folder may be missing or unwritable, or Windows may be unable to open it.
Check the fixed path in the [log guide](USAGE.md#open-local-logs) and the visible
error. Logs rotate through a fixed set of files; they are not a complete audit
and their names don't give chronological order.

Logging failure doesn't deliberately close Labs. Use the available menu or local
control status to report the build version and sanitized error. Don't substitute
full vendor logs or a raw capture: those can contain private data.

## How do I report a bug if the bridge will not start?

Open **VX Bridge Diagnostics** from Start to save a local ZIP. It does not
start or repair the host. Missing status or logs are marked unavailable, not
healthy. Review the ZIP, then attach it to the
[bug form](https://github.com/devigned/vx-bridge/issues/new?template=bug.yml)
if you choose. Nothing is sent automatically.

If export fails or an older build lacks that shortcut, submit without an
attachment. Include the visible error, selected game, versions you know,
approximate time/time zone, and what you expected and observed. Do not attach
raw vendor/MSI logs or private data. See [Reporting](REPORTING.md).

For a new capability rather than a failure, use the
[feature form](https://github.com/devigned/vx-bridge/issues/new?template=feature.yml).
Describe your task, workaround and desired outcome; an implementation is optional.

<a id="start-with-windows-is-checked-but-the-bridge-doesnt-start"></a>

## Auto-launch on Windows Startup is checked, but the bridge doesn't start

The checkmark reports registration, not Windows' decision to run it. Review your
Windows Startup apps settings or applicable policy. The bridge doesn't override
Windows disabling an entry.

The helper says **Starts when you sign in to Windows.** It does not start before
sign-in, open Labs or the game, connect Labs, or select a different game.
Setup's separate **Start bridge** option opens the bridge once after verified
successful setup; it does not turn on sign-in launch.

If another copy or different arguments are registered, use the owning copy to
remove its registration normally. The bridge won't silently replace it. For a
portable copy, unregister before moving the executable. See
[startup ownership](technical/DESKTOP.md) for details.

## My Infinite Tees connection uses a different port

The default is local port 999. There is no settings dialog or automatic port
discovery. For an approved build, disconnect Labs and quit the bridge normally
before relaunching its own host with your confirmed port, for example:

```powershell
.\GSPconnect.exe --desktop --target-port 1000
```

This example runs from the build's own directory, not a vendor installation.
Use your actual configured port, not 1000 unless that is your setup. Valid ports
are 1 through 65535, excluding the source port 12321. Both applications must
remain on the same PC; network/LAN targets aren't supported.

## Which versions and features have been checked?

v0.1.0 passed automated build and static file checks. Those checks did not run
the installer or product executables. See the [release notes](releases/v0.1.0.md)
and [publication record](technical/QUALIFICATION.md#public-release-publication).

An earlier private installer candidate passed hosted Windows checks for standard-user
installation, damaged-file repair, one controlled version-to-version upgrade,
full removal and explicit uninstall/reinstall recovery after an induced failure.
The failed-upgrade Repair attempt did **not** complete recovery; the full
uninstall/reinstall route did. That result does not qualify this public
bootstrap or any rebuilt binaries.

Earlier Infinite Tees trials with Labs **1.11.17.1** observed shots, putts,
combined separate ball/club messages, reconnection without closing Labs, and
one outage discard with no replay followed by a fresh flight. Separate idle
checks covered keyboard use, icon restoration and reversible startup entries.
Those observations apply to their recorded builds, not automatically to the
current candidate.

Interactive setup and Start bridge, actual Windows sign-in, live Labs/game use,
GSPro, switching between games and putting/player context for this candidate
still need testing. Other Labs versions, long sessions/sleep and uncertain
in-flight failures need further evidence. The
[qualification guide](technical/QUALIFICATION.md) summarizes prior observations
and limits without importing the private ledgers.

## Will the installer upgrade an older portable or pilot copy?

No. Setup does not automatically adopt or remove old portable/pilot folders.
The tested upgrade used compatible versions built with the same native Setup
core and source; it is not a promise to upgrade arbitrary older builds.

Start with normal Labs Disconnect and Quit the old copy. Preserve its files and
startup registration until you have separately approved migration instructions.
There is no verified portable-migration procedure. Publication of v0.1.0 does
not authorize adopting or deleting an old copy.

## Does uninstall erase my game choice or all installer files?

Yes. **Successful full Uninstall removes** VX Bridge's settings, game choice,
logs, installed files, saved installer files and installation records, shortcuts
and owned startup/product entries. A later fresh install uses fresh defaults.
Repair, in-place reinstall and supported upgrades preserve configuration and
game choice by default.

Use Windows' **VX Bridge > Uninstall** entry, not Setup's `/remove` flag.
Normal removal does not need an original download, a working bridge or a working
cached Setup executable. Repair is a separate **VX Bridge Setup** action.

Full cleanup passed for an earlier private hosted candidate, not this public
bootstrap, the interactive
Windows Settings UI or every possible failure. Unknown contents or incomplete
cleanup fail explicitly. Independently saved downloads/exports, development
archives, vendor/game data and foreign paths are left alone. See
[uninstall instructions](INSTALL.md#uninstall).

## Does it replace Labs, alter vendor files or upload my shots?

No. Labs manages the hardware. The bridge forwards supplied data locally and
doesn't overwrite the GSPro connector, modify activation, calculate flight or
upload telemetry. There is no service, updater or shipped .NET/browser runtime.
Local logs are redacted; [privacy and source-use details](technical/SECURITY.md)
explain their bounds and the dependency notices.
