# Occasional installer qualification (parked)

This folder contains the **parked, separately authorized** Windows lifecycle
worker and workflow template. It is not an active Actions workflow, ordinary
diagnostic, installer front end, or command for your PC.
Use [`tools/diagnostics`](../../diagnostics/README.md) for safe offline inspection.

The only execution mode is:

```text
lifecycle-worker.exe -config ABSOLUTE_GENERATED_CONFIGURATION
```

The retained [workflow template](workflow.yml) and adjacent input, author, run,
and policy scripts generate and bind that configuration to the exact source and
workflow SHA, first run attempt, disposable GitHub-hosted Windows context, three
actual A/B/B-fault media profiles, and independent non-admin primary-token user.
The worker verifies delivered full-byte/package/component identities before
product operations. Do not manufacture configuration or environment variables
to bypass these checks. Local source tests do not authorize a workflow dispatch.

## Temporary activation

Routine CI and tag release are the only files under `.github/workflows`.
GitHub cannot dispatch this template in its parked location. Its retained
`github.event.repository.private` gate intentionally fails closed in this
**public** repository, even if copied to `.github/workflows/setup-lifecycle.yml`.
Copying it to public `main` does not enable qualification. Do not remove that
gate, switch repository visibility, manufacture a private event, or dispatch
the template as a routine check.

Any future execution needs separate owner authorization and a separately
reviewed private qualification design, including repository/source identity,
private evidence transport and disposable-account handling. No such design
or execution is authorized here. Any future source changes, including
activation/removal, must use signed commits and pull requests under
[AGENTS.md](../../../AGENTS.md), not direct default-branch pushes.

Preserve original logs, immutable artifact identity and first outcomes.
Never retry an unchanged failed source to manufacture a green result.

**Source limitation:** activating and removing the copy each changes the commit
identity. The actual standard-account result qualifies only the activation
commit, not the earlier parked source or the later removal commit. Core guards
require source SHA to equal workflow SHA; do not relax them to claim that a
parked final commit was executed. Routine CI can still statically verify the
actual application and matching installer package for the parked source.

## Why it remains

The full `recovery-first-v2` qualification still checks fresh default-off startup,
repair of damaged owned files, normal upgrade with retained settings/logs/Run
value, clean uninstall, fresh reinstall, real post-InstallExecute mutation failure,
retained original receipt and typed Pending denial, exact-media supported repair,
and, when repair is incomplete, explicit original-product removal followed by a
fresh usable install. The final uninstall audits all app-owned paths, registrations
and component clients while preserving foreign sentinels and original media.
Timeouts preserve raw potentially changing logs; they do not trigger forced kill,
cleanup takeover, or a retry to manufacture success.

Old comparison-only, external/vendor, outer, elevated, and private ETL modes are
removed. They did not run in the accepted full lifecycle. Retired flags/config
fields are refused rather than silently interpreted as the full lifecycle.
The previous restoration-only catalog classifier is gone; current failure tests
exercise the actual retained recovery denial/before-after binding and wire ABI.
Historical failed runs and accepted lifecycle results are not relabelled.

## Source checks only

From the repository root, use the pinned Go toolchain. These checks do not run
the lifecycle worker or any MSI:

```sh
make help-qualification
make lint-workflows
GOTOOLCHAIN=local CGO_ENABLED=0 go test -mod=readonly -count=1 -timeout=90s \
  ./tools/msi/lifecycle ./tools/diagnostics/...
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go vet -mod=readonly ./tools/msi/lifecycle ./tools/diagnostics/...
```

`make lint-workflows` checks the parked template as well as routine workflows;
linting does not activate it. Exceptional Make entry points are
`build-lifecycle-input`, `author-lifecycle`, `test-installer-lifecycle` and
`restore-lifecycle-policy`. Their hosted authorization/source/account/policy
guards remain in force.

The current native build remains `installer/build-setup-native.sh`, with all eleven
exact inputs in `installer/setup/` and `installer/managed/state.[ch]`; those shared
state helpers are production dependencies, not obsolete fixtures.

Worker process exit zero plus its actual final audit is the lifecycle result;
an individual MSI log line is not qualification. This worker does not test
interactive client/sign-in behavior, real Labs/game compatibility, cross-native
core servicing, reboot, or hardware. No live execution is authorized here.
