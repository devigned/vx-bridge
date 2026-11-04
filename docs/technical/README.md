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

An earlier private installer candidate passed a recorded same-source,
same-native-core lifecycle on a disposable hosted Windows machine, including
explicit recovery after an induced failure. That does not qualify the public
bootstrap or a rebuilt artifact, and is not client-PC, interactive
installer, sign-in, attached-Labs, or cross-game qualification.
The public [v0.1.0 release](https://github.com/devigned/vx-bridge/releases/tag/v0.1.0)
has passed build and static artifact verification, not those runtime or servicing
checks. See [Qualification](QUALIFICATION.md#public-release-publication).

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
