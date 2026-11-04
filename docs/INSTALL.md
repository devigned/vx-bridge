# Install VX Bridge and make the first connection

[User guides](README.md) / Installation

VX Bridge connects ProTee Labs to your choice of Infinite Tees or GSPro OpenAPI.
This guide covers the self-contained Windows installer.
Read [Limits](#limits) before installing.

## Before you start

Use Windows x64, an activated ProTee VX with ProTee Labs already working, and a
licensed Infinite Tees or GSPro installation on the same PC. VX Bridge does not
supply a game, replace Labs or change licenses and activation.

For GSPro, run both the game and its public **OpenAPI / APIv1 Connect**
application. Do not run the native VX receiver at the same time.
The bridge needs no separate .NET or browser runtime.

## Choose the right files

[Open the latest release](https://github.com/devigned/vx-bridge/releases/latest).
Under **Assets**, download `vx-bridge-installer-windows-amd64-vX.Y.Z.exe`,
where `vX.Y.Z` is the release's version. Open that one file. No ZIP extraction,
adjacent MSI or separate bridge download is needed.

| Files | What they are for |
|---|---|
| `vx-bridge-installer-windows-amd64-vX.Y.Z.exe` | Normal installation. Contains the matching Setup, MSI and support files. |
| `vx-bridge-installer-windows-amd64-vX.Y.Z.exe.sha256` | Optional integrity check for that EXE, not an installer. |
| Source checkout or source archive | Contributing to the project, not a ready-to-run Windows app. |

These are the only two uploaded assets. GitHub also supplies source ZIP/tar.gz
archives. A checksum checks file contents, not who published them.
Do not use an Actions artifact marked **BUILD ONLY** as an approved installer.

The existing [v0.1.0 release](releases/v0.1.0.md) predates this format. Its
installer ZIP, separate MSI and application ZIP remain unchanged; use that
release's recorded extraction steps if it is still the latest release. This
change does not replace historical downloads or qualify new binaries.

Untagged developer application ZIPs use
`vx-bridge-windows-amd64-<revision>.zip`. See
[building without installing](../contributors.md#build-without-installing-or-running),
not these setup steps, for development packages.

## Install

1. In ProTee Labs, select **Game Options > Disconnect**. Close any existing
   receiver through its own controls. For VX Bridge, select **Quit** after Labs
   disconnects. Do not run two receivers or overwrite the vendor's `GSPconnect.exe`.
2. Open the downloaded installer EXE as the Windows user who will use the
   bridge, not as administrator. Select **Install** for a fresh install.
3. Leave **Auto-launch on Windows Startup** off unless the bridge should open
   when you sign in. Its helper says **Starts when you sign in to Windows.**
   It is off on a fresh install; maintenance preserves a verified preference
   unless you change it.
4. Wait for Setup to report success. At **Start the bridge now?**, select
   **Start bridge** to open it once, or **Not now** to open **VX Bridge** from the
   Windows Start menu later.

Start bridge and auto-launch are independent. Neither opens Labs or the game,
connects Labs or changes the saved game choice. If installation succeeds but
the one-time launch fails, resolve the reported conflict and open the installed
**VX Bridge** shortcut. Do not reinstall merely to retry a launch.

If Windows blocks Setup, stop and [report the warning](https://github.com/devigned/vx-bridge/issues).
Do not disable security protections or bypass the warning.

## Make the first connection

1. Open Infinite Tees or GSPro and enter a playable practice scene. For GSPro,
   also open **OpenAPI / APIv1 Connect**, not the native VX receiver.
2. Open **VX Bridge** from Start if it is not running. Find its icon near the
   clock; check the hidden-icons arrow if necessary.
3. Right-click the bridge icon and select **Use Infinite Tees** or
   **Use GSPro OpenAPI**. **Standby (discard shots)** sends shots to neither game.
   The bridge remembers the selection. Opening a game does not select it.
4. Use ProTee Labs' normal Connect action if needed. The bridge does not open
   Labs or the game, or connect Labs for you.
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

## Limits

v0.1.0 passed automated build and static file checks. The self-contained
installer is a later source change, not a replacement or new qualification of
that release. Interactive installation, Windows sign-in, installer servicing
and live Labs/game use, including GSPro and game switching, remain unqualified
for rebuilt binaries. Earlier installer and equipment observations used
different builds; see
[qualification](technical/QUALIFICATION.md).

The Windows programs and installer are not publisher-signed (Authenticode).
Signed Git commits and tags do not sign those files.

## Maintenance and Repair required

Opening VX Bridge from its executable, Start shortcut or Windows sign-in does
**not** run Windows Installer or repair the installation automatically.

For a damaged installation or **Repair required** message, open **VX Bridge
Setup** from Start and choose **Repair / Reinstall**. The bridge does not need to
work first. If Setup's saved installation source is missing, open the original
verified installer EXE for that installed build. Keep it for exceptional recovery;
normal repair uses the saved Setup/MSI and does not need the download.
For older ZIP releases, retain the original matching extracted pair.
Windows' raw Modify/Repair
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
If Windows lists more than one interrupted VX Bridge version, ask a project
maintainer for the recorded-version recovery instructions. That path removes the
original recorded old version, fully uninstalls the current version, confirms
complete removal, then installs fresh. Removing just one version is not completed
recovery or permission to start the remaining bridge.

Normal uninstall uses Windows' registered package. If that cache is damaged,
recovery may need the verified original MSI for each recorded version with
`REMOVE=ALL`. A maintainer can extract matching media from the original EXE
without installing it; see the [Setup command reference](technical/COMMANDS.md#setup1).
This is an explicit uninstall route, not permission to install a raw MSI.
Don't guess product identities, delete Windows Installer records or
manually purge settings to make a failed operation look successful. See the
[recovery FAQ](FAQ.md#vx-bridge-reports-repair-required) and
[installer manual](technical/INSTALLER.md) for the supported boundaries.

Repair, in-place reinstall and supported upgrades preserve configuration, logs
and game choice by default. Arbitrary version changes, different native Setup
cores and older portable/pilot copies are not automatically adopted. Keep an old
copy's files and startup registration intact until you have approved migration
instructions.

## Uninstall

Before removing logs needed for a bug report, save an optional report with
**VX Bridge Diagnostics** in Start. This does not start the host or call MSI.
See [Reporting](REPORTING.md); independently saved reports are not uninstalled.

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
