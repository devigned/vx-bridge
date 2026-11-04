# VX Bridge technical manual

[User documentation](../README.md) / Technical manual

This manual describes the implementation and its operating constraints. For
installation and everyday use, consult the [user guides](../README.md).
For checkout prerequisites and contribution procedure, see
[contributors.md](../../contributors.md).

VX Bridge is a native Windows x64 application running in the signed-in user's
desktop session. It receives the ProTee Labs feed through an application-owned
`GSPconnect.exe` and forwards eligible shots to one selected destination:
Infinite Tees, GSPro OpenAPI, or no destination in Standby. It retains the
source connection and compatibility window during target faults. It does not
replay shots, calculate flight, or infer game readiness.

The executable names, per-user directories, registry names, and protocol
identity are compatibility interfaces. The VX Bridge name does not rename them.
No vendor executable is replaced, and neither vendor endorsement nor live
GSPro qualification is implied.

## Reading the manual

Read [Architecture](ARCHITECTURE.md) for process and package ownership, then
[Protocol and delivery](PROTOCOL.md) for framing, shot eligibility, and
acknowledgments. [Lifecycle and shutdown](LIFECYCLE.md) explains why source
departure, rather than a Quit request, controls teardown.

[Desktop integration](DESKTOP.md) covers the tray, local control, and sign-in
registration. [Installation and recovery](INSTALLER.md) describes managed-copy
admission, conventional MSI servicing, and the full-Uninstall contract.
[Security and data ownership](SECURITY.md) defines local storage, trust, and
license boundaries.

Commands are collected in [Command reference](COMMANDS.md), not interleaved
with the design explanation. [Build and release automation](AUTOMATION.md)
describes artifact identity and publication. [Testing](TESTING.md) explains
what the automated checks establish; [Qualification](QUALIFICATION.md)
separates those checks from observed Windows and equipment behavior.

## Status and scope

The public [v0.1.0 release](../releases/v0.1.0.md) passed build and static artifact
checks. Interactive installation, Windows sign-in, client-PC servicing and live
Labs/game behavior remain unqualified for these binaries.

An earlier private installer candidate passed a same-source, same-native-core
lifecycle on a disposable Windows host, including recovery after an induced
failure. That result does not qualify v0.1.0 or a rebuilt artifact.
[Qualification](QUALIFICATION.md) separates the publication checks from earlier
installer and equipment observations.

Ordinary startup has no Windows Installer service dependency. There is no
Session 0 service, automatic updater, hardware-direct interface, vendor
configuration writer, network discovery, or shipped browser/.NET runtime.

## Historical references

The [research boundary](research/README.md) explains the source-only public
curation. Private captures, dated investigation ledgers and obsolete proposals
are not published here. The [qualification summary](QUALIFICATION.md) retains
bounded prior observations, negative results and safety limits; those are not
current procedures or permission to repeat a trial. Upstream provenance and
license obligations remain in the project and installer notices.
