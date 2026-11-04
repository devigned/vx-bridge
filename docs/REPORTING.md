# Report a bug or request a feature

[User guides](README.md) / Reporting

## Submit a bug

Check the [FAQ](FAQ.md) and
[existing issues](https://github.com/devigned/vx-bridge/issues) first. Then
choose **Report a bug** in the bridge menu, or open the
[bug form](https://github.com/devigned/vx-bridge/issues/new?template=bug.yml).
A GitHub account is needed to submit it.

Describe the problem, steps, expected result and observed result. Include the
visible error and selected game. If you know them, add:

- Approximate problem time and time zone.
- Bridge release, Windows version/build, ProTee Labs version, and game/OpenAPI version.
- Whether this followed a fresh install, upgrade, startup, game change or Quit.
- How often it happens, the last working bridge version, and its effect on play.

Write **unknown** for versions you cannot find. Find Labs and game versions
in their own About screens; the bridge does not search their files.
Do not hit extra shots, force-close an attached receiver or change vendor
settings just to reproduce a fault.

## Save an optional diagnostic report

1. Choose **Save diagnostic report...** from the bridge menu. If the bridge
   will not start, its icon is missing or it reports **Repair required**,
   open **VX Bridge Diagnostics** from Windows Start instead. No terminal
   command is needed.
2. Choose a **new filename in a local folder**. Existing files are not
   overwritten. Cancel closes the save dialog without collecting or saving.
3. Wait for the saved, partial, or failure notice. **Nothing was sent.**
4. Open the ZIP and read `summary.txt`. Review its contents before dragging
   it into the bug form. You choose whether to attach it.

The report contains bridge versions, numeric Windows build facts, selected
game/local endpoints, bounded status and sanitized events, log-loss counters,
an installer-result summary and installed-file checks. `manifest.json`
identifies sources that were included, unavailable, skipped or truncated.
`report.json` and `events.jsonl` provide structured details.

A partial report is useful. An unreachable host is marked unavailable; export
does not start or repair it, disconnect Labs, change settings, or call Windows
Installer. Missing or corrupt sources do not prevent other sources from being
saved. Reports are best-effort observations, not proof of game readiness,
complete shot history, or installation health.

If saving fails, try a new filename in a writable local folder. An incomplete
output file may remain; do not attach it. Submit the bug **without a ZIP** and
describe the save error. If your build has no reporting menu or shortcut, it
predates this workflow; use the web form without a ZIP. Do not repair, upgrade
or restart solely to make a diagnostic report.

## Keep private information out

Issues and attachments are public. Do not attach raw vendor or verbose MSI
logs, packet captures, crash dumps, shot measurements, player data, device or
hardware identifiers, license/account details, usernames, private paths or
credentials. Crop or redact screenshots before attaching them.

The exporter reads only fixed bridge-owned sources and allowlists exported
fields. It excludes vendor data, raw payloads and private-path/error dumps.
It does not upload, clear, rotate or delete the original logs. Independently
saved reports remain yours; bridge Uninstall does not remove them. See the
[privacy contract](technical/DIAGNOSTICS.md) for precise limits.

## Request a feature

Use the
[feature form](https://github.com/devigned/vx-bridge/issues/new?template=feature.yml).
Describe the task or problem, your current workaround and the outcome that
would help. An implementation suggestion is optional. No diagnostic report
is needed for an ordinary feature request.
