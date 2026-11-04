# Build and release automation

[Technical manual](README.md) / Automation

## Responsibilities

The [Makefile](../../Makefile) and checked scripts own reusable build,
verification, test, and publication commands. Workflow YAML supplies events,
runner/tool setup, selected refs, permissions, concurrency, timeouts, and
artifact transport. A local and hosted build should use the same command
contract rather than separate copies of linker or installer logic.

[CI](../../.github/workflows/ci.yml) checks pull requests and main pushes.
CI and tag release are the only active workflow files. Occasional installer
qualification is kept beside its tooling, not in the routine Actions list.
Hosted CI containers use `vx-bridge-ci-input-` and `vx-bridge-ci-authoring-`
prefixes; release containers use `vx-bridge-tag-input-`, `vx-bridge-tag-assets-`
and `vx-bridge-tag-authoring-`, each with the exact source/run or version.
Portable tests, Linux race instrumentation, and native Windows fixtures are
distinct checks. PR CI tests the merge checkout, not an asserted reviewed
head. Ordinary artifacts use development identity; CI is not release
promotion and does not run the product EXEs or operate a simulator.
Its build-only jobs produce the application archive, matching native Setup
inputs, and statically verified installer artifacts through shared commands.

The [version-tag workflow](../../.github/workflows/release.yml) builds an exact
canonical tagged commit already in main, then publishes only that run's
verified artifacts. The
[parked qualification template](../../tools/msi/lifecycle/workflow.yml)
is different: its retained private-repository gate prevents execution in this
public repository, even if copied into the active workflow directory. A future
separately reviewed private qualification design and owner authorization would
be required to execute Setup/MSI under an independent standard account.
It is not part of ordinary CI or tag publication.
[Command reference](COMMANDS.md) documents the actual entry points.
The [lifecycle worker reference](../../tools/msi/lifecycle/README.md)
owns its guarded configuration interface; it is not a general local MSI CLI.
Its activation/removal commits have different source identities. Only the
activation commit can receive actual standard-account qualification; the
parked final source must not be described as having executed that workflow.

Build tools use the pinned [.go-version](../../.go-version) and
[installer toolchain](../../installer/toolchain.json). Hosted provisioning is
runner setup, not a requirement to change a simulator PC.
Native Go distribution is Windows amd64/v1 with CGO disabled. Race testing
uses CGO on a native C-capable runner, not those distribution flags.

## Distribution identity

Application packaging starts from a clean committed checkout and a new
external output directory. The authoritative distribution builder owns
readonly module compilation, trimpath/VCS identity, linker flags, PE/Go
inspection, and ZIP/checksum generation. It rechecks cleanliness and rejects
linked module replacements or wrong source/compiler input.

The application ZIP has exactly six entries:

```text
GSPconnect.exe
vxbridge.exe
LICENSE
THIRD_PARTY_NOTICES.txt
build-manifest.json
SHA256SUMS
```

The emitted host must have GUI subsystem 2 and actual `defaultDesktop=true`;
control must have console subsystem 3 and `defaultDesktop=false`. Inspection
checks actual architecture, no CLR directory/import, compiler, CGO0, linked
dependencies/checksums, and embedded revision/release data. Requested flags
or a matching filename are not proof.

The schema-2 manifest binds source, release, toolchain, binary sizes/hashes,
modes, and embedded build metadata. `SHA256SUMS` covers the five payload files,
not itself. Reopened ZIP entries must meet the exact allowlist, size/type
bounds, manifest, and hash checks. An external `.zip.sha256` binds the whole
archive.

Archive entry ordering/modes are fixed and timestamps derive from the source
commit. Comparing two independent clean builds with the same toolchain can
test reproducibility; one successful package does not prove universal
cross-platform reproducibility. Historical artifacts retain their own
source, helper, schema, and instrument identities.

Installer authoring consumes that verified app ZIP and matching native inputs.
Actual MSI Summary Information, tables, native stream, and extracted CAB
bytes must agree with final metadata and checksums. The generated PackageCode
identifies delivered media; rebuilt same-version bytes are not silently
equivalent Repair media. See
[Installation](INSTALLER.md#build-tooling-and-static-verification).

## Installer artifacts

The installer ZIP contains nine flat entries:

```text
setup.exe
bridge.msi
setup-input.json
msi-inspection.json
INSTALLER-NOTICE.txt
MINGW-COPYRIGHT.txt
LICENSE
THIRD_PARTY_NOTICES.txt
SHA256SUMS
```

`setup.exe` and `bridge.msi` must be the matching authored pair.
Metadata, inspection, compiler copyright, and license notices are retained
with the actual bytes. The direct named MSI is identical to `bridge.msi`
inside the bundle; it is not a separate standalone-install interface.
Use verified Setup for installation/Repair and conventional registered-product
Uninstall as described in [Installation](INSTALLER.md).

For a canonical versioned build, filenames use the public repository name
(persisted Windows compatibility identities are unchanged):

```text
vx-bridge-windows-amd64-vX.Y.Z-<40sha>.zip
vx-bridge-installer-windows-amd64-vX.Y.Z-<40sha>.zip
vx-bridge-windows-amd64-vX.Y.Z-<40sha>.msi
```

Each file has its own appended `.sha256`. These are output naming rules,
not existing release URLs. The release publishes the verified application
ZIP, installer ZIP, direct matching MSI, and all three checksums.
`package-installer` and `verify-installer` own bundle assembly/integrity;
native compilation and WiX authoring remain separate stages.

## Artifact transport

A hosted artifact is not a GitHub release. Artifact names and metadata carry
the exact source/version; download the selected run's original artifact rather
than substituting another successful run or rebuilding it locally.
Authenticated read-only inspection can start with:

```sh
gh run list --workflow ci.yml --limit 5
gh run download RUN_ID --name ARTIFACT_NAME --dir /new/private/download
```

Replace the placeholders with that run's actual ID/name and a new private
directory. Verify the original container digest, external checksum, inner
manifest, and exact-source package. Static verification does not require
executing an EXE, installing an MSI, or acquiring vendor/capture data.

Within workflows, consumers use the producer's nonempty immutable artifact ID
and digest, not a mutable name/latest search. Missing files, digest mismatch,
mixed source/version, or unexpected package contents fail explicitly.
`NATIVE_ARTIFACT_ID`/`NATIVE_ARTIFACT_DIGEST` bind source inputs;
publication's `ARTIFACT_ID`/`ARTIFACT_DIGEST` bind the final same-run tag assets.
Retention is finite; an expired artifact does not justify replacing its
evidence with newly built bytes.

Saved MSI logs can also be inspected through the
[offline diagnostic tools](../../tools/diagnostics/README.md). Such inspection
requires neither GitHub context nor installer execution and supplies no
qualification verdict.

## Version-tag release policy

A creation push for `vMAJOR.MINOR.PATCH` selects the exact tagged commit.
The version is canonical, at most 64 characters, with no leading-zero
components except zero and no prerelease/build suffix. Deleted, moved,
forced, or noncreation tag events refuse before build.

The event's raw ref OID can identify an annotated-tag object rather than a
commit. Gates therefore pin raw tag OID, peeled source commit, and version
separately. Event/local/live ref identity, checkout HEAD, current remote main,
and GitHub ancestry comparison must agree. Later main is never substituted
as build source.

Read-only build/verification jobs create artifacts. The write-token
publication job rechecks tag/source identity and main ancestry, downloads
only that run's exact artifact, and validates its contents and hashes before
creating a draft with verified-tag/exact-target semantics.
Existing releases/assets are not reused or overwritten.

After upload and a further identity check, publication keeps
`prerelease=false` and `latest=false`. A late failure can leave a draft;
inspect it rather than moving a tag, replacing assets, or turning an
uncertain result into success. Local `RELEASE_VERSION` metadata alone never
creates a tag or release.

There is no branch/PR artifact promotion, schedule, `workflow_run` release
handoff, or extra manual enable gate. GitHub loads the tag workflow from
the tagged revision; an old commit lacking that workflow does not inherit
today's YAML.

## Trust and qualification

Default workflow permissions are read-only. Only guarded publication gets
contents write; checkout credentials are not persisted. Actions and tools
use checked pins. Release serialization does not make ref checks atomic
with GitHub writes: authorized workflow/tag editors can still select code,
so review and ref protections remain part of the trust boundary.
The scripts do not configure rulesets, secrets, signing, or approval
environments as a side effect.

Binary signing and cryptographic artifact attestation are not configured.
Repository commits must instead be signed under [AGENTS.md](../../AGENTS.md);
that does not sign the emitted EXEs/MSI. Checksums detect
corruption and bind inspected bytes; they do not authenticate a publisher,
prove hardware compatibility, or imply vendor endorsement.
The tag-release write path has local negative-fixture coverage, not an
actual published-release result.

The only active workflows are CI and version-tag release. The private-only
manual Setup lifecycle remains parked. Independent comparison/rollback-control
and OS-diagnostic branches are retired investigation, not extra release gates.

[Qualification](QUALIFICATION.md) records current hosted CI/installer scope
and older bridge observations. The
[research boundary](research/README.md#installer-and-automation-records) explains
why private run/hash ledgers and failed attempts are not imported into this
source-only public repository.
