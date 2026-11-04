# Desktop integration

[Technical manual](README.md) / Desktop

The [user guide](../USAGE.md) describes everyday menu operation. This chapter
explains native ownership, status meaning, and persistent preferences.

## Window and worker ownership

The tray attaches to the compatibility window's locked-OS-thread Win32 message
loop. A hidden owned window supplies popup ownership. Fallback notices use a
separate UI thread and are owned by the canonical identity; they do not create
another unowned exact-title main window.

Callbacks dispatch work rather than performing controller queries, modal
dialogs, registry changes, or file I/O. Workers poll snapshots with finite
deadlines and send updates to the owner thread. A blocked shell or disk must
not prevent status, selection admission, or guarded Quit.

On `TaskbarCreated`, the tray re-adds the current icon and v4 registration
without restarting the source or target. Windows supplies menu fonts,
keyboard navigation, and scaling. The original geometric icons contain
16/20/24/32/40/48/64-pixel sizes; `go generate ./internal/desktop` regenerates
those marks, not vendor artwork.

Initial icon, update, popup, and restoration failures become explicit desktop
status and a one-time owned fallback notice. They do not close attached Labs.
An open popup is dismissed during owner-thread cleanup only after the
[guarded engine completion](LIFECYCLE.md#completion-and-diagnostics).

## Status is an observation

Source and target transport are displayed separately from **Game readiness:
not reported**. A linked blue icon means transport is connected, not that the
game is ready to swing. The latest event and error are sanitized and shown in
local time. Unknown event reasons receive a short stable hash rather than an
arbitrary payload dump.

Forwarded counts derive from `TargetAccept`; missed counts derive from
`Dropped`, with uncertain delivery shown as its subset. These are aggregate
engine observations, not per-shot receipt correlation or flight guarantees.

IT, GSPro OpenAPI, and Standby are explicit checked choices. Status shows
applied and requested destination separately while a change drains.
Selection is disabled during switching, Quit, quarantine, or unavailable
status, while guarded Quit remains available. The GSPro entry discloses its
unqualified live behavior. No menu action discovers the running game.

**Open Logs** invokes native ShellExecute only on the fixed application log
directory. It accepts no arbitrary path or command. Failure is visible through
the menu and `DesktopAction`.

**Save diagnostic report...** runs the shared exporter behind a native save
dialog on a separate serialized support worker. **Report a bug** opens the
empty GitHub form, with no report data in its URL. Cancel saves nothing;
completion distinguishes saved, partial and failed results. Native Windows
controls provide keyboard/screen-reader labels; nothing is uploaded. Support
work cannot block the engine, native owner, ordinary desktop action worker or
status/Quit poller. See [Diagnostic reports](DIAGNOSTICS.md).

## Remembered destination

The destination choice is stored separately from startup registration at:

```text
%APPDATA%\VxInfiniteTeesBridge\destination.json
```

The file contains one `destination` value: `infinite_tees`, `gspro`, or
`standby`. Missing preferences select Infinite Tees. Unknown fields, invalid
values, unreadable data, or data larger than 1 KiB refuse host startup rather
than silently reverting to a different game.

The action worker saves an accepted selection through a temporary file, sync,
and rename. A blocked save cannot block the engine or identity loop.
Failure remains visible even if the destination change itself applies; it
must not be described as a remembered success. Normal startup reads this file,
so post-install **Start bridge** does not override the user's game choice.

## Sign-in registration

The exact label **Auto-launch on Windows Startup** means sign-in, not boot
before login. Its help text is **Starts when you sign in to Windows.**
Fresh installation defaults to off. Enabling it requires explicit consent
through the tray or Setup; maintenance may preserve a verified owned choice.

```text
HKCU\Software\Microsoft\Windows\CurrentVersion\Run
VxInfiniteTeesBridge (REG_SZ)
"<absolute own GSPconnect.exe path>" --desktop --target-port <validated port>
```

The executable path is absolute and quoted; basename, arguments, port, and
Windows Run length are validated. Mutation rechecks complete exact-command
ownership under a protected same-user mutex. A different copy, unfamiliar
registry type, different arguments, or lock/read/write error is not silently
overwritten or removed.

The checkmark reports registration, not execution by Windows policy.
The bridge does not modify undocumented `StartupApproved` values, override
disabled startup, reassert a preference, auto-login, start Labs or a game,
or initiate Connect. No service, scheduled task, or elevation is involved.
Unregister a portable copy before moving it; it is not automatically adopted
by the [managed installer](INSTALLER.md#managed-copy-admission).

## Control and failure reporting

The separate `vxbridge.exe` supplies `--version`, `--status`, `--quit`,
`--diagnostics` and `--diagnostics-dialog`;
see [Command reference](COMMANDS.md#vxbridge1). Status and Quit use bounded
same-user control, independently of managed host admission. They neither
start a host nor bypass source-first shutdown.

The **VX Bridge Diagnostics** Start shortcut uses `--diagnostics-dialog`,
independently of normal host admission. A missing/refused/unstarted host yields
an honestly partial report; it never triggers repair. The shortcut is a
nonadvertised owned installer resource, not another runtime or service.

`Desktop`, `LocalLogs`, `LocalLogLost`, `Startup`, and `DesktopAction`
decorate engine snapshots with health and registration state, not executable
paths or raw registry contents. An unreachable pipe is an explicit failure,
not cached status. Remote control access is rejected, and a remote shell
cannot make the host satisfy the interactive window requirement.

Recorded source-free trials exercised current-scaling keyboard use, icon
restoration, and registration enable/removal. They did not establish actual
sign-in policy execution or broad DPI behavior. The
[qualification summary](QUALIFICATION.md#bridge-and-desktop-observations)
keeps those distinctions.
