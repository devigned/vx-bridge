# Install VX Bridge and make the first connection

[User guides](README.md) / Installation

VX Bridge connects ProTee Labs to your choice of Infinite Tees or GSPro OpenAPI.
Follow these steps only for an approved pilot build.

## Before you start

You need **Windows x64**, a working **activated ProTee VX and ProTee Labs**, and
a **licensed Infinite Tees or GSPro installation on the same PC**. Keep your
existing licenses and activation in place. VX Bridge does not supply a game,
replace Labs or unlock a vendor feature.

For GSPro, use its public **OpenAPI / APIv1 Connect** application, without
running the competing native VX receiver. Both the game and OpenAPI Connect
must be open when you play. The bridge itself needs no separate .NET or browser
runtime.

## Choose the right files

**No public installer or GitHub release is published yet.** Approved pilot
testers should get the reviewed installer files and compatibility notes from the
pilot organizer. The public
[Releases page](https://github.com/devigned/vx-bridge/releases)
has no published installer or public version to recommend. Source availability
does not make a development build a qualified playing release.

For an approved tagged build, **choose the installer ZIP**. The tag-delivery
contract uses these filenames; this does not mean a release is available now.
`vX.Y.Z` is the version and `<40sha>` identifies the source commit.

| Files | What they are for |
|---|---|
| `vx-bridge-installer-windows-amd64-vX.Y.Z-<40sha>.zip` | **Recommended install choice:** extract it and run `setup.exe` with matching `bridge.msi` beside it. |
| `vx-bridge-windows-amd64-vX.Y.Z-<40sha>.zip` | Application-only package for separately approved manual/development use. Its six files do not include Setup/MSI. |
| `vx-bridge-windows-amd64-vX.Y.Z-<40sha>.msi` | Windows Installer package, **not a standalone installer**. It still requires matching Setup; choose the installer ZIP instead. |
| Source checkout or source archive | Contributing to the project, not a ready-to-run Windows app. |

Each ZIP/MSI has a separate checksum file named `<filename>.sha256`.

**Do not install by double-clicking `bridge.msi`.** It requires the matching
native Setup program and refuses an unowned install. Do not mix files from
different builds or treat an Actions artifact marked **BUILD ONLY** as approval
to install it.

Right-click the installer ZIP, choose **Extract All**, and open the extracted
folder before running Setup. Its nine files have no subfolders. Keep them
together: `setup.exe`, `bridge.msi`, the information files `setup-input.json` and
`msi-inspection.json`, the notices/licenses `INSTALLER-NOTICE.txt`,
`MINGW-COPYRIGHT.txt`, `LICENSE` and `THIRD_PARTY_NOTICES.txt`, and `SHA256SUMS`.

Keep the download's separate checksum alongside the archive. There is no
signed-publisher claim today: a checksum checks file contents, not who published
them. Don't disable Windows security protections to run an unverified file.

Untagged developer application ZIPs use
`vx-bridge-windows-amd64-<revision>.zip`. See
[building without installing](../contributors.md#build-without-installing-or-running),
not these setup steps, for development packages.

## Install

1. If another receiver is running, use **Game Options > Disconnect in ProTee
   Labs**, then close that receiver through its own controls. For an existing
   VX Bridge, choose **Quit** after disconnecting Labs. Never run two receivers,
   force-close an attached one or overwrite the vendor's `GSPconnect.exe`.
2. Open the approved **`setup.exe`** normally as the Windows user who will use
   the bridge, **not as administrator**. Choose **Install** for a fresh install.
3. Leave **Auto-launch on Windows Startup** off unless you want the bridge to
   open when you sign in. Its helper says: **Starts when you sign in to Windows.**
   Fresh installs default off; maintenance preserves an existing verified
   preference unless you change it.
4. Wait for Setup to report success. At **Start the bridge now?**, choose
   **Start bridge** to open it once, or **Not now** to open **VX Bridge** from the
   Windows Start menu later.

Start bridge and auto-launch are independent. Neither opens Labs or the game,
connects Labs, or changes your saved game choice. If the one-time launch fails
after installation succeeded, resolve the reported runtime conflict and open
the installed **VX Bridge** shortcut; don't reinstall just to retry launching.

**Pilot status:** an earlier private installer candidate passed hosted Windows
installation, damaged-file repair, one controlled version-to-version upgrade,
full uninstall and explicit uninstall/reinstall recovery checks as a standard
user. This does not qualify the public bootstrap or rebuilt binaries, or verify
interactive setup, the Start bridge prompt, actual
Windows sign-in or live Labs/game use. GSPro and game switching still need live
testing for this candidate. The
[qualification summary](technical/QUALIFICATION.md) records the bounded prior
observations and limits.

## Make the first connection

1. Open **VX Bridge** on your signed-in Windows desktop. Find its icon near the
   clock; it may be in the hidden-icons menu.
2. Open Infinite Tees or GSPro and enter a playable practice scene. For GSPro,
   also open its **OpenAPI / APIv1 Connect** application, not the native VX receiver.
3. Right-click the bridge icon and select **Use Infinite Tees** or
   **Use GSPro OpenAPI**. **Standby (discard shots)** sends shots to neither game.
   Your selection is remembered; opening another game won't select it for you.
4. Use the normal connection action in **ProTee Labs** if needed. Opening the
   bridge does not guarantee that Labs connects automatically.
5. In the bridge menu, check that **ProTee Labs** is connected and your chosen
   game says **Connected (transport only)**. Check the game itself before swinging:
   the bridge cannot tell whether it is ready for the next shot.

Most users should leave connection settings unchanged. Infinite Tees normally
uses local port 999; GSPro OpenAPI uses 921, not the native GSPro receiver's
9050. If needed, use the [connection FAQ](FAQ.md#protee-labs-wont-connect) or
[alternate Infinite Tees port instructions](FAQ.md#my-infinite-tees-connection-uses-a-different-port).

To change games, stop swinging and let the visible result finish first. Then
choose the other game, wait for switching to finish and check it before playing.
Shots missed during switching, an outage or Standby are not saved or replayed.
See [everyday use](USAGE.md#choose-a-destination).

## Maintenance and Repair required

Opening VX Bridge from its executable, Start shortcut or Windows sign-in does
**not** run Windows Installer or repair the installation automatically.

For a damaged installation or **Repair required** message, open **VX Bridge
Setup** from Start and choose **Repair / Reinstall**. The bridge does not need to
work first. If Setup's saved installation source is missing, use the original
verified `setup.exe` and its matching `bridge.msi`. Windows' raw Modify/Repair
buttons are disabled; use Setup rather than trying to bypass its checks.

Before maintenance, disconnect in Labs normally and Quit. If Setup says
**Disconnect in ProTee Labs to continue**, do that and let it wait for the bridge
to close. Cancel does not restart a bridge that has already been asked to Quit.
Don't force-close it, reboot to bypass the wait or replace vendor files.

Windows Installer's automatic rollback is enabled, but **a failed upgrade is
not guaranteed to restore the previous version**. Keep the bridge stopped until
Setup verifies a consistent installation. Repair can restore damaged files, but
**may not resolve a failed upgrade with multiple registered versions**.

If Repair cannot complete, use **complete Uninstall followed by a fresh Install**.
If Windows lists more than one interrupted VX Bridge version, ask the pilot
organizer for the recorded-version recovery instructions. That path removes the
original recorded old version, fully uninstalls the current version, confirms
complete removal, then installs fresh. Removing just one version is not completed
recovery or permission to start the remaining bridge.

Normal uninstall uses Windows' registered package. If that cache is damaged,
recovery may need the verified original MSI for each recorded version with
`REMOVE=ALL`; this is an explicit uninstall route, not permission to install a
raw MSI. Don't guess product identities, delete Windows Installer records or
manually purge settings to make a failed operation look successful. See the
[recovery FAQ](FAQ.md#vx-bridge-reports-repair-required) and
[installer manual](technical/INSTALLER.md) for the supported boundaries.

Repair, in-place reinstall and supported upgrades preserve configuration, logs
and game choice by default. Arbitrary version changes, different native Setup
cores and older portable/pilot copies are not automatically adopted. Keep an old
copy's files and startup registration intact until you have approved migration
instructions.

## Uninstall

Disconnect in Labs and Quit normally, then use **Windows Settings > Apps >
Installed apps > VX Bridge > Uninstall** (called **Apps & features** on some
Windows versions). Normal uninstall does not need a working bridge or working
cached Setup executable, and normally needs no original download. Running Setup
with `/remove` only directs you to Windows Settings; it does not perform removal.

**Successful full Uninstall removes** VX Bridge's installed files, saved installer
files, installation records, settings, game choice, logs, shortcuts and owned
startup/product entries. A later fresh install uses fresh defaults. Vendor/game
data and independently saved downloads or exports are left alone.

Unknown files, ownership problems or incomplete cleanup cause an explicit
failure, not a successful partial uninstall. Don't delete broad folders or
registry entries to work around it; report the error.

## Finish safely

Use **Game Options > Disconnect in ProTee Labs**, then **Quit** in VX Bridge.
Quit waits while Labs is attached. Do not use Task Manager or sign out as a way
to force it closed. The [everyday guide](USAGE.md#quit-safely) and
[shutdown FAQ](FAQ.md#quit-keeps-waiting-for-labs) explain what to do if it waits.
