# VX Bridge

**One ProTee VX connection. Your choice of golf simulator.**

For golfers using a ProTee VX, VX Bridge sends shots from **ProTee Labs** to
**Infinite Tees** or **GSPro OpenAPI** on the same PC. Choose the game from the
small icon near the Windows clock; your choice is remembered. Labs still manages
your launch monitor, and the game handles ball flight. You don't need to swap
connector files whenever you change games.

## Get started

You'll need **Windows x64**, an activated **ProTee VX with Labs already working**,
and a **licensed Infinite Tees or GSPro game on the same PC**. For GSPro, use its
public **OpenAPI / APIv1 Connect** application, not the native VX receiver.
VX Bridge does not include a game or replace your existing licenses.

**Public source; no public installer release yet.** An earlier installer candidate
passed hosted Windows checks for installation, damaged-file repair, one
controlled version upgrade, full uninstall and recovery by uninstall/reinstall.
That does not qualify this public bootstrap or a rebuilt binary. Interactive
setup, Windows sign-in and live Labs/game behavior for the current source,
including GSPro and game switching, still need testing. Earlier Infinite Tees
trials do not qualify it as a ready-to-play release.

For an approved pilot build:

1. **Choose the approved installer ZIP from the pilot organizer.** Use
   **Extract All**, then keep matching `setup.exe` and `bridge.msi` in the
   extracted folder with the accompanying files. Choose the installer ZIP,
   not the application ZIP or MSI alone; see
   [filenames and which files to use](docs/INSTALL.md#choose-the-right-files).
2. **Close any existing receiver safely.** In ProTee Labs, use **Game Options >
   Disconnect**, then close that receiver through its own controls. Never run
   two receivers or overwrite the vendor's `GSPconnect.exe`.
3. **Open `setup.exe` and choose Install.** Run it normally as your Windows user,
   not as administrator. Don't double-click `bridge.msi` to install it.
   **Auto-launch on Windows Startup** is optional and off on a fresh install:
   "Starts when you sign in to Windows." After successful setup, **Start bridge**
   opens it once; **Not now** lets you open **VX Bridge** from Start later.
4. **Open your game and choose it in VX Bridge.** Right-click the bridge icon
   near the clock (check hidden icons too), then choose **Use Infinite Tees** or
   **Use GSPro OpenAPI**. For GSPro, open its OpenAPI Connect application as well.
   **Standby (discard shots)** keeps Labs connected but sends shots to neither game.
5. **Connect in ProTee Labs and check the game.** Use Labs' normal Connect action
   if needed. The menu should show Labs connected and the selected game
   **Connected (transport only)**. Confirm the game is in a playable scene:
   a connection alone does **not** mean it is ready for your next shot.

Neither launch option opens Labs or the game, connects Labs, or changes your
saved game choice. For another day, open VX Bridge and your game, check the tray
choice, and connect Labs.

**When finished:** use **Game Options > Disconnect in ProTee Labs**, then
**Quit** in VX Bridge. Quit waits while Labs is still attached; don't force it
closed with Task Manager.

Need help? [Installation and repair](docs/INSTALL.md),
[everyday use](docs/USAGE.md) and the [troubleshooting FAQ](docs/FAQ.md) cover the
next steps. If you see **Repair required**, use VX Bridge Setup; a failed upgrade
may need a complete uninstall and fresh install rather than Repair alone.

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
distinguish tested behavior from work still needed before release.

## Open-source license

Original project work is available under the [MIT license](LICENSE), with its
notice and warranty disclaimer. [Third-party notices](THIRD_PARTY_NOTICES.txt)
cover dependencies and reference material; MIT does not license vendor software.
This is an unofficial project, not affiliated with or endorsed by ProTee,
Infinite Tees or GSPro.
