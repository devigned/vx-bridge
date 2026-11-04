# Everyday use with VX Bridge

[User guides](README.md) / Everyday use

Open VX Bridge, choose today's game, connect Labs and check the game before
swinging. [v0.1.0 is published](https://github.com/devigned/vx-bridge/releases/tag/v0.1.0),
but is not yet a qualified ready-to-play release. Live Labs/game use, GSPro and
game switching for these binaries still need testing.

## Start a session

Open **VX Bridge** from Start on the same signed-in Windows desktop as Labs and
your game. For a separately approved portable build, use its supplied launch
instructions instead. Open the game, enter a playable scene and connect in
ProTee Labs if needed.

Find the bridge icon near the Windows clock, including the hidden-icons menu.
The desktop build has no permanent console window. Right-click the icon to open
its menu. Keyboard users can press **Win+B**, use the arrows to find the icon,
and open its menu with **Enter** or **Shift+F10**.

## Choose a destination

Select **Use Infinite Tees**, **Use GSPro OpenAPI** or **Standby (discard shots)**.
A checkmark shows the applied choice. Only one game receives shots; opening
another game does not select it, send to both games or switch automatically if
the selected game becomes unavailable.

Infinite Tees is the first-launch default when no choice has been saved. For
GSPro, run both the licensed game and its public **OpenAPI / APIv1 Connect**
application. Don't start the competing native VX receiver or change vendor
files or activation. For connection settings, see the
[Infinite Tees port FAQ](FAQ.md#my-infinite-tees-connection-uses-a-different-port)
or [GSPro connection FAQ](FAQ.md#gspro-stays-unavailable).

**Before changing games, stop swinging and let the visible result finish.**
Choose the other game and wait for **Switching to...** to finish. The bridge
waits for the current shot response and pending replies to finish or time out;
new shots during switching are discarded without a success reply.
It keeps the Labs connection in place rather than restarting the bridge.
If a Labs connection fault blocks switching, use normal Labs Disconnect; repeated choices
cannot force it.

**Standby** keeps Labs connected but closes the game connection. Shots are
discarded, not saved for later. After selecting a game again, check the game
before swinging. A connected OpenAPI application may not mean the game behind
it is healthy or ready.

Your choice is saved separately in
`%APPDATA%\VxInfiniteTeesBridge\destination.json`. If the saved choice is invalid
or unreadable, a new bridge stops with an error instead of guessing a game.
If the menu says **Game choice not saved**, the selected game still applies for
this session; select it again after switching finishes to retry saving.
Changing games never changes Windows startup registration.

## Read the menu

| Menu text | What it means |
|---|---|
| **ProTee Labs** | Whether Labs is connected or waiting. A fault can mean it is still attached; disconnect normally. |
| **Infinite Tees / GSPro OpenAPI / Standby** | The applied choice and connection status. A pending switch is shown separately. |
| **Connected (transport only)** | The data connection is open, not proof that the game is ready. |
| **Game readiness: not reported** | Check the game yourself and let the previous flight finish before swinging. |
| **Forwarded (target 200)** | The game returned a success response, not a guarantee of a visible flight or a uniquely identified shot. |
| **Missed/discarded / Uncertain** | Lost shots and deliveries the bridge couldn't confirm. Uncertain is included in missed, not an extra total to add. |
| **Last / Last error** | The latest event and most recent error. An earlier error may stay visible after the connection recovers. |

Counts cover the whole bridge session across game changes, not individual
games' visible shot totals. Use the text and the game, not icon color alone:
no icon color means "ready to swing."

## Play and recover

VX Bridge preserves supplied measurements and combines matching separate ball
and club messages. It does not invent missing measurements or calculate flight.
Missing or unsupported GSPro data is reported as a missed shot, not estimated
or silently converted.

If the selected game closes or loses its connection, the bridge keeps Labs
connected and retries only that game. **Don't hit a shot to trigger reconnection.**
Shots arriving while the game is unavailable or still responding to the previous
shot are lost; they won't appear later when the connection returns.

After recovery, check the game and resume with a fresh shot. For **Uncertain**
delivery, inspect the game before deciding to swing again: it may already have
received the shot, and the bridge won't resend it. See the
[missed-shot FAQ](FAQ.md#a-shot-was-missed-or-delivery-is-uncertain).

Only run one bridge. Opening another copy does not replace the active one.
Selecting GSPro here means **OpenAPI**, not the native VX connector. To return
to the vendor's native integration instead, disconnect normally in Labs and
Quit VX Bridge before opening that receiver.

## Open local logs

Choose **Open Logs** in the tray menu. The folder is:

```text
%LOCALAPPDATA%\VxInfiniteTeesBridge\logs\
```

Logs use five rotating files, up to 2 MiB each. Use event timestamps for order,
not filenames. Logs contain connection/error events and build versions, not
shot measurements, device identifiers, actual shot IDs or player data.
Nothing is uploaded automatically.

A log-folder or storage failure appears in the menu. Some recent status remains
available in memory; logging trouble does not deliberately disconnect Labs.
See the [logs FAQ](FAQ.md#open-logs-fails-or-local-logs-are-unavailable) before
collecting information for a bug report.

## Auto-launch on Windows Startup

The menu label is **Auto-launch on Windows Startup**, with the helper
**Starts when you sign in to Windows.** It is off on a fresh install.
Choose it to register this copy for your Windows user; choose it again to turn
that registration off.

A checkmark means the entry is registered, not that Windows will allow it to
run. Windows settings or policy may disable it. It does not sign you in, open
Labs or the game, connect Labs, or choose a different game.
If another copy is registered, VX Bridge won't overwrite it; see the
[startup FAQ](FAQ.md#auto-launch-on-windows-startup-is-checked-but-the-bridge-doesnt-start).

Setup's separate **Start bridge** choice opens the installed bridge once, only
after Setup has verified success and finished maintenance, using the saved game
choice. It does not enable sign-in launch. Ordinary launches don't run Windows
Installer or automatically repair the bridge.

For a portable development copy, turn off its own startup registration before
moving it. For managed installation or repair, follow the
[installation guide](INSTALL.md#maintenance-and-repair-required).

## Quit safely

Use **Game Options > Disconnect in ProTee Labs**, then choose **Quit** in VX
Bridge. If you choose Quit first, it shows **Waiting for Labs to disconnect**.
Repeated clicks don't force it closed, and it may keep forwarding while it waits.

Don't use Task Manager, close a console by its X button or deliberately sign
out while Labs is attached. A forced receiver shutdown was followed by a Labs
crash in earlier research. Disconnecting Labs first is the supported procedure,
not a guarantee against power loss or process failure.

If Quit keeps waiting, follow the [shutdown FAQ](FAQ.md#quit-keeps-waiting-for-labs).
