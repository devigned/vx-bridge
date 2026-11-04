# VX Bridge

VX Bridge forwards shot data from ProTee Labs to Infinite Tees or GSPro OpenAPI
on the same Windows PC. Select one game from the icon near the clock. Labs
controls the launch monitor; the game calculates ball flight.

## Requirements

- Windows x64.
- An activated ProTee VX with ProTee Labs already working.
- A licensed Infinite Tees or GSPro installation on the same PC.

For GSPro, use its public **OpenAPI / APIv1 Connect** application, not the native
VX receiver. VX Bridge does not supply a game or replace an existing license.
v0.1.0 is an early release; read [Limits](#limits) before installing.

## Install

**[Download the v0.1.0 installer ZIP](https://github.com/devigned/vx-bridge/releases/download/v0.1.0/vx-bridge-installer-windows-amd64-v0.1.0-5401cf26d19238b04ba3dbb8ecfa60442616ca79.zip).**
Use this ZIP, not the application-only ZIP, separate MSI or **Source code**
downloads. See the [release notes](docs/releases/v0.1.0.md) for the initial
release's features, contributors and files.

1. In ProTee Labs, select **Game Options > Disconnect**. Close any existing
   receiver through its own controls. Do not run two receivers or replace the
   vendor's `GSPconnect.exe`.
2. Right-click the downloaded ZIP and select **Extract All**. Open the extracted
   folder. Keep all nine files together, including `setup.exe` and `bridge.msi`.
   Do not run Setup from inside the ZIP or mix files from different builds.
3. Open `setup.exe` as the Windows user who will use the bridge, not as
   administrator. Select **Install**. Do not double-click `bridge.msi`.
4. Leave **Auto-launch on Windows Startup** off unless the bridge should open
   when you sign in. It is off on a fresh install.
5. Wait for Setup to report success. At **Start the bridge now?**, select
   **Start bridge** to open it once, or **Not now** to open **VX Bridge** from
   Start later. Starting it once does not enable auto-launch.

If Windows blocks Setup, stop and [report the warning](https://github.com/devigned/vx-bridge/issues).
Do not disable security protections or bypass the warning.

## First use

1. Open the game and enter a playable practice scene. For GSPro, also open
   **OpenAPI / APIv1 Connect**.
2. Open **VX Bridge** from Start if it is not running. Find its icon near the
   Windows clock; check the hidden-icons arrow if necessary.
3. Right-click the icon. Select **Use Infinite Tees** or **Use GSPro OpenAPI**.
   A checkmark shows the applied choice. The bridge remembers it for next time.
4. Use ProTee Labs' normal Connect action if needed. The bridge does not open
   Labs or the game, or connect Labs for you.
5. Check the bridge menu. Labs should be connected and the selected game should
   show **Connected (transport only)**. Then check the game itself before
   swinging. A data connection does not establish that the game is ready.

For later sessions, open the game and bridge, check the selected game, then
connect Labs. Wait for each shot's visible result before the next swing.

## End a session

In ProTee Labs, select **Game Options > Disconnect**. Then select **Quit** in
VX Bridge. Quit waits while Labs is attached. Do not force the bridge closed
with Task Manager.

## Troubleshooting

| Symptom | Action |
|---|---|
| No bridge icon | Check hidden icons near the clock. Open **VX Bridge** from Start, not another downloaded copy. |
| No connection | Run one receiver. Check the selected game and open it. For GSPro, open **OpenAPI / APIv1 Connect** too. See [connection help](docs/FAQ.md#protee-labs-wont-connect). |
| **Repair required** | Open **VX Bridge Setup** from Start and select **Repair / Reinstall**. If it fails, follow the [recovery guide](docs/INSTALL.md#maintenance-and-repair-required). Do not replace files manually. |

Do not hit shots to trigger reconnection. If delivery is uncertain, check the
game before swinging again: it may have received the shot.

## Change games without changing the connection

Stop swinging and let the visible result finish. Select the other game and wait
for switching to finish. Check that game before resuming.

Only the selected game receives shots. Opening a game does not select it.
**Standby (discard shots)** keeps Labs connected but sends to neither game.
Shots missed during switching, an outage or Standby are lost, not saved or
replayed.

## Limits

v0.1.0 passed automated build and file checks. Interactive installation, Windows
sign-in, installer servicing and live Labs/game use, including GSPro and game
switching, remain unqualified for these binaries. Earlier observations used
different builds; see [qualification](docs/technical/QUALIFICATION.md).

The Windows programs and installer are not publisher-signed (Authenticode).
Signed Git commits and tags do not sign those files.

Local logs omit shot measurements and device/player identifiers. Nothing is
uploaded automatically.

## Documentation and contributions

Use the [installation guide](docs/INSTALL.md), [everyday guide](docs/USAGE.md) or
[FAQ](docs/FAQ.md) for more detail. The [technical manual](docs/technical/README.md)
covers implementation and testing. Read [Contributing](contributors.md) before
reporting a bug or submitting a change.

Original work uses the [MIT license](LICENSE).
[Third-party notices](THIRD_PARTY_NOTICES.txt) cover dependencies and reference
material, not a license to vendor software. VX Bridge is unofficial and is not
endorsed by ProTee, Infinite Tees or GSPro.
