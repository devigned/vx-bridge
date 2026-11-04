# VX Bridge

**One ProTee VX connection. Your choice of golf simulator.**

For golfers using a ProTee VX, VX Bridge sends shots from **ProTee Labs** to
**Infinite Tees** or **GSPro OpenAPI** on the same PC. Choose the game from the
small icon near the Windows clock; your choice is remembered. Labs still manages
your launch monitor, and the game handles ball flight. You don't need to swap
connector files whenever you change games.

## Download and install

**[Download VX Bridge v0.1.0 for Windows x64 (installer ZIP)](https://github.com/devigned/vx-bridge/releases/download/v0.1.0/vx-bridge-installer-windows-amd64-v0.1.0-5401cf26d19238b04ba3dbb8ecfa60442616ca79.zip)**

This is the installer ZIP from the [v0.1.0 release](https://github.com/devigned/vx-bridge/releases/tag/v0.1.0).
You do **not** need the application-only ZIP, separate MSI, `.sha256` files or
**Source code** downloads to install.

**Early release:** automated build and file checks passed, but these binaries
have not been qualified for interactive installation, Windows sign-in or live
Labs/game use, including GSPro and game switching. Earlier trials used different
builds; they do not prove v0.1.0 is ready to play. See the
[tested scope and remaining gaps](docs/technical/QUALIFICATION.md).

You'll need **Windows x64**, an activated **ProTee VX with Labs already working**,
and a **licensed Infinite Tees or GSPro game on the same PC**. VX Bridge does not
include a game or replace your existing licenses.

1. **Close any existing receiver safely.** In ProTee Labs, choose **Game Options >
   Disconnect**, then close the old receiver through its own controls. Never run
   two receivers or replace the vendor's `GSPconnect.exe`.
2. **Extract the whole download.** Right-click the downloaded installer ZIP,
   choose **Extract All**, then open the extracted folder. Keep all nine files
   together, especially `setup.exe` and `bridge.msi`. Don't run Setup from inside
   the ZIP or mix files from different downloads.
3. **Open `setup.exe` and choose Install.** Open it normally as the Windows user
   who will play, **not as administrator**. Don't double-click `bridge.msi`.
   Leave **Auto-launch on Windows Startup** off unless you want VX Bridge to
   open when you sign in; it is off on a fresh install.
4. **Wait for Setup to report success.** At **Start the bridge now?**, choose
   **Start bridge** to open it once, or **Not now** to open **VX Bridge** from
   the Windows Start menu later. Starting it once does not enable auto-launch.

**If Windows blocks Setup:** the Windows programs and installer are not
publisher-signed (Authenticode). The signed Git tag does not sign them.
Don't disable Windows security protections or bypass a warning to continue;
[report the warning](https://github.com/devigned/vx-bridge/issues).

## First use

1. **Open your game** and enter a playable practice scene. For GSPro, also open
   its public **OpenAPI / APIv1 Connect** application, not the native VX receiver.
2. **Open VX Bridge** from Start if it isn't already running. Find its small
   icon near the Windows clock; check the hidden-icons arrow if needed.
3. **Right-click the icon and choose your game:** **Use Infinite Tees** or
   **Use GSPro OpenAPI**. A checkmark shows the applied choice, which is remembered.
   **Standby (discard shots)** sends shots to neither game.
4. **Connect in ProTee Labs** using its normal Connect action if needed. VX
   Bridge does not open Labs or the game, or connect Labs for you.
5. **Check both the menu and the game.** The menu should show Labs connected
   and your chosen game **Connected (transport only)**. That confirms a data
   connection, not readiness to play. Check the game itself before swinging and
   confirm each shot's visible result before the next swing.

**Next time:** open your game and VX Bridge, check the selected game, then
connect Labs. Only your selected game receives shots; opening a game does not
select it automatically.

**When finished:** use **Game Options > Disconnect in ProTee Labs**, then
**Quit** in VX Bridge. Quit waits while Labs is still attached; don't force it
closed with Task Manager.

## If something isn't working

| What you see | What to do |
|---|---|
| No bridge icon | Check hidden icons near the clock. Open **VX Bridge** from Start, not another downloaded copy. |
| Labs won't connect, or the game stays unavailable | Check that only one receiver is running, the game is open and the correct game is selected. For GSPro, open **OpenAPI / APIv1 Connect** too. [Connection help](docs/FAQ.md#protee-labs-wont-connect). |
| **Repair required** | Open **VX Bridge Setup** from Start and choose **Repair / Reinstall**. Don't replace files manually. A failed upgrade may need complete uninstall/reinstall; [follow the recovery guide](docs/INSTALL.md#maintenance-and-repair-required). |

Don't hit extra shots to fix a connection. Check the game first if a shot is
missing or uncertain: it may already have arrived. See
[everyday use](docs/USAGE.md) and the [FAQ](docs/FAQ.md) for more help.

## Change games without changing the connection

Only your selected game receives shots. Starting another game does not select it
automatically or send shots to both games. Stop swinging, let the visible result
finish, choose the other game, and wait for switching to finish before checking
that game.

Shots missed during an outage, switching or Standby are **lost, not saved for
later or replayed**. If delivery is uncertain, the game may already have received
the shot; check before swinging again. VX Bridge doesn't invent missing
measurements, success replies or game readiness.

Local logs leave out shot measurements and device/player identifiers. Nothing
is uploaded automatically.

## Help make VX Bridge better

You don't need a launch monitor to improve the instructions, report a confusing
step or work on automated tests. Start with the [contribution guide](contributors.md)
for reporting a bug, setting up a checkout and submitting a focused change.

For code work, use the [build and command guide](docs/technical/COMMANDS.md),
[testing guide](docs/technical/TESTING.md) and
[technical manual](docs/technical/README.md). The
[compatibility evidence and remaining gaps](docs/technical/QUALIFICATION.md)
distinguish automated release checks from live compatibility still needing testing.

## Open-source license

Original project work is available under the [MIT license](LICENSE), with its
notice and warranty disclaimer. [Third-party notices](THIRD_PARTY_NOTICES.txt)
cover dependencies and reference material; MIT does not license vendor software.
This is an unofficial project, not affiliated with or endorsed by ProTee,
Infinite Tees or GSPro.
