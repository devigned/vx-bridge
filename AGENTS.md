# Repository rules

## Signed changes through pull requests

The owner authorized one initial source-only bootstrap push to `main`.
That exception is not reusable. After that bootstrap, all code changes must
use a branch and a pull request, including fixes to CI, documentation, release
scripts, and these rules. Never push directly to `main`, force-push it, bypass
its protections, or amend or rewrite published history.

Every commit you create must be cryptographically signed with the owner's
existing configured Git signing identity. Verify the signature before pushing.
Author text and a `Co-authored-by` trailer are not signatures. Never disable
signing, fall back to unsigned commits, generate or substitute a signing key,
or expose signing credentials. Stop and report unavailable signing capability.

## Preserve compatibility and safety

Read [contributors.md](contributors.md) and the relevant
[technical manual](docs/technical/README.md) before changing behavior.
Keep `GSPconnect.exe`, the `ProTee United VX` compatibility window,
`vxbridge.exe`, existing Windows storage/registry/pipe/product-family identities,
and source-first shutdown protections intact. The repository/module/artifact
name is `vx-bridge`; it does not migrate persisted Windows identities.

Ordinary application startup must not call MSI or attempt automatic repair.
Do not weaken same-user admission, uninstall ownership, exact-source/package
checks, or recovery-first non-atomic servicing.

Only routine CI and version-tag release belong under `.github/workflows`.
Installer qualification is parked, separately authorized, and private-host-only;
do not activate or dispatch it as a migration or routine check. The Labs
research probe remains STOP / DO NOT RERUN. No build or test authorizes real
simulator, hardware, installer servicing, vendor configuration, or account work.

Use the pinned toolchain and `make ci` for a broad source change. Packaging
requires an exact clean signed commit and a new output directory outside the
checkout. Keep raw captures, private evidence, machine-specific data, binaries,
caches, and credentials out of Git. Preserve LICENSE and third-party notices.
