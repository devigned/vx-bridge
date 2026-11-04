SHELL := bash
.DEFAULT_GOAL := help

GO ?= go
GOFMT ?= gofmt
POWERSHELL ?= pwsh
CGO_ENABLED ?= 0
RELEASE_VERSION ?= development
SETUP_BUILD_PROFILE ?= normal
override GOTOOLCHAIN := local
export GO GOFMT POWERSHELL CGO_ENABLED GOTOOLCHAIN REVISION RELEASE_VERSION OUT ARCHIVE NATIVE_DLL
export NATIVE_DIR SETUP_STAGING WIX_ARCHIVE TOOL_LOG SETUP_BUILD_PROFILE
export WORKFLOW_REVISION BUILD_INPUT_DIR EVIDENCE_DIR NATIVE_ARTIFACT_ID NATIVE_ARTIFACT_DIGEST
export AUTHORED_SETUP MINGW_COPYRIGHT BUNDLE_STUB INSTALLER_ARCHIVE LIFECYCLE_EVIDENCE

.PHONY: help help-installer help-qualification build package verify-package install-setup-toolchain build-setup-input build-setup-artifacts package-installer verify-installer build-setup-native prepare-setup build-setup-msi build-lifecycle-input author-lifecycle test-installer-lifecycle restore-lifecycle-policy test-native-tools test test-race vet fmt fmt-check check ci ci-windows benchmark lint-workflows check-commands release-resolve release-verify release-publish

help:
	@printf '%s\n' \
	  'Project commands (GNU make 3.81+, Bash, pinned Go from .go-version):' \
	  '  make help             Show this help (also the default)' \
	  '  make check            Formatting, unit tests and vet; no product execution' \
	  '  make ci               check, race tests and pinned workflow/template lint' \
	  '  make ci-windows       Windows tests/vet/fake tools; requires new external OUT' \
	  '  make build            Alias for package (not a loose/native executable)' \
	  '  make package          Statically verified Windows amd64 CGO0 ZIP; requires OUT' \
	  '  make verify-package   Verify an existing ZIP; requires ARCHIVE' \
	  '  make test             Native unit/regression tests, CGO0 by default' \
	  '  make test-race        Native race tests, CGO1; requires a C compiler' \
	  '  make vet              Vet native packages (GOOS/GOARCH may select cross-vet)' \
	  '  make fmt              Format tracked Go files in place' \
	  '  make fmt-check        Fail on unformatted tracked Go files; no edits' \
	  '  make lint-workflows   Lint routine workflows plus the inactive qualification template' \
	  '  make check-commands   Run small fake-tool Makefile command checks' \
	  '  make help-installer   Repeatable Linux/Windows installer build and integrity commands' \
	  '  make help-qualification  Exceptional disposable-host testing; parked, not routine CI' \
	  '' \
	  'OUT must be a new absolute directory outside the checkout; its parent must exist.' \
	  'REVISION defaults to exact HEAD at invocation. Packaging requires a clean commit.' \
	  'RELEASE_VERSION defaults to development; tagged packages require an explicit vX.Y.Z.' \
	  'ARCHIVE is the existing ZIP path. Paths with spaces are supported.' \
	  'These routine commands do not start the bridge, install an MSI or publish a release.' \
	  'See docs/technical/COMMANDS.md for prerequisites, identity, outputs and workflow use.'

help-installer:
	@printf '%s\n' \
	  'Installer builds (static authoring/integrity only; no product execution):' \
	  '  make install-setup-toolchain  Explicit pinned MinGW provisioning on the approved Linux builder' \
	  '  make build-setup-native      Pinned C Setup/actions/bundle stub cross-build; REVISION/OUT' \
	  '  make prepare-setup           Verify and stage application/native inputs; ARCHIVE/NATIVE_DIR/OUT' \
	  '  make build-setup-msi          Windows WiX authoring, tables/CAB inspection; SETUP_STAGING/WIX_ARCHIVE/TOOL_LOG/OUT' \
	  '  make package-installer       Self-contained EXE + checksum; AUTHORED_SETUP/ARCHIVE/MINGW_COPYRIGHT/BUNDLE_STUB/OUT' \
	  '  make verify-installer        Check EXE, embedded media and app ZIP; INSTALLER_ARCHIVE' \
	  '  make build-setup-input       Shared exact-source Linux input job; source/run/version identity' \
	  '  make build-setup-artifacts   Shared Windows author/inspect/package job; exact input artifact identity' \
	  '  make test-native-tools       Fake-tool logging/exit regression; new external OUT' \
	  '' \
	  'Tag publication alone uses guarded release-resolve/release-verify/release-publish.' \
	  'No local build command creates a tag or release.' \
	  'See docs/technical/COMMANDS.md and docs/technical/AUTOMATION.md.'

help-qualification:
	@printf '%s\n' \
	  'Exceptional installer qualification (not active under .github/workflows):' \
	  '  make build-lifecycle-input   Private exact-source A/B/worker inputs; manual hosted scope only' \
	  '  make author-lifecycle        Author and inspect increasing versions/private fault media' \
	  '  make test-installer-lifecycle Real MSI recovery/full cleanup under an independent standard account' \
	  '  make restore-lifecycle-policy Restore the exact original temporary disposable-VM MSI policy' \
	  '' \
	  'The parked template is tools/msi/lifecycle/workflow.yml.' \
	  'Its private-repository gate intentionally prevents execution in this public home.' \
	  'Do not manufacture hosted environment/configuration or run this on your PC.' \
	  'A separately reviewed private qualification design is required; see tools/msi/lifecycle/README.md.'

build: package

package:
	@set -euo pipefail; \
	  : "$${OUT:?Set OUT to a new absolute external directory with an existing parent}"; \
	  revision="$${REVISION:-$$(git rev-parse HEAD)}"; \
	  "$$GO" mod download; \
	  "$$GO" mod verify; \
	  "$$GO" run -mod=readonly ./tools/dist -revision "$$revision" -version "$$RELEASE_VERSION" -out "$$OUT"

verify-package:
	@set -euo pipefail; \
	  : "$${ARCHIVE:?Set ARCHIVE to the existing distribution ZIP}"; \
	  revision="$${REVISION:-$$(git rev-parse HEAD)}"; \
	  "$$GO" run -mod=readonly ./tools/dist -revision "$$revision" -version "$$RELEASE_VERSION" -verify "$$ARCHIVE"

install-setup-toolchain:
	@bash .github/scripts/install-setup-toolchain.sh

build-setup-input:
	@bash .github/scripts/setup-build-input.sh

build-setup-artifacts:
	@"$$POWERSHELL" -NoLogo -NoProfile -File .github/scripts/setup-build-only.ps1

package-installer:
	@set -euo pipefail; \
	  : "$${AUTHORED_SETUP:?Set AUTHORED_SETUP to the inspected Windows authoring output}"; \
	  : "$${ARCHIVE:?Set ARCHIVE to the exact verified application ZIP}"; \
	  : "$${MINGW_COPYRIGHT:?Set MINGW_COPYRIGHT to the pinned compiler copyright file}"; \
	  : "$${BUNDLE_STUB:?Set BUNDLE_STUB to the matching native bundle-stub.exe}"; \
	  : "$${OUT:?Set OUT to a new external release artifact directory}"; \
	  "$$GO" run -mod=readonly ./tools/msi/setup -revision "$${REVISION:-$$(git rev-parse HEAD)}" \
	    -version "$$RELEASE_VERSION" -archive "$$ARCHIVE" -installer "$$AUTHORED_SETUP" \
	    -copyright "$$MINGW_COPYRIGHT" -bundle-stub "$$BUNDLE_STUB" -out "$$OUT"

verify-installer:
	@set -euo pipefail; \
	  : "$${INSTALLER_ARCHIVE:?Set INSTALLER_ARCHIVE to the self-contained installer EXE}"; \
	  "$$GO" run -mod=readonly ./tools/msi/setup -revision "$${REVISION:-$$(git rev-parse HEAD)}" \
	    -version "$$RELEASE_VERSION" -verify-installer "$$INSTALLER_ARCHIVE"

build-setup-native:
	@REVISION="$${REVISION:-$$(git rev-parse HEAD)}" bash installer/build-setup-native.sh

prepare-setup:
	@set -euo pipefail; \
	  : "$${NATIVE_DIR:?Set NATIVE_DIR to exact native build output}"; \
	  : "$${ARCHIVE:?Set ARCHIVE to the checked application ZIP}"; \
	  : "$${OUT:?Set OUT to a new external staging directory}"; \
	  "$$GO" run -mod=readonly ./tools/msi/setup -revision "$${REVISION:-$$(git rev-parse HEAD)}" \
	    -version "$$RELEASE_VERSION" -archive "$$ARCHIVE" -native "$$NATIVE_DIR" -out "$$OUT" -profile "$$SETUP_BUILD_PROFILE"

build-setup-msi:
	@set -euo pipefail; \
	  : "$${SETUP_STAGING:?Set SETUP_STAGING to prepared verified input}"; \
	  : "$${WIX_ARCHIVE:?Set WIX_ARCHIVE to the exact approved WiX ZIP}"; \
	  : "$${TOOL_LOG:?Set TOOL_LOG to an explicit native log file}"; \
	  : "$${OUT:?Set OUT to a new external output directory}"; \
	  "$$POWERSHELL" -NoLogo -NoProfile -File installer/build-setup.ps1 \
	    -Profile "$$SETUP_BUILD_PROFILE" \
	    -Staging "$$SETUP_STAGING" -Out "$$OUT" -WixArchive "$$WIX_ARCHIVE" -ToolLog "$$TOOL_LOG"

build-lifecycle-input:
	@bash tools/msi/lifecycle/setup-lifecycle-input.sh

author-lifecycle:
	@"$$POWERSHELL" -NoLogo -NoProfile -File tools/msi/lifecycle/check-lifecycle-scripts.ps1
	@"$$POWERSHELL" -NoLogo -NoProfile -File tools/msi/lifecycle/setup-lifecycle-author.ps1

test-installer-lifecycle:
	@"$$POWERSHELL" -NoLogo -NoProfile -File tools/msi/lifecycle/check-lifecycle-scripts.ps1
	@"$$POWERSHELL" -NoLogo -NoProfile -File tools/msi/lifecycle/setup-lifecycle-run.ps1

restore-lifecycle-policy:
	@"$$POWERSHELL" -NoLogo -NoProfile -File tools/msi/lifecycle/setup-lifecycle-policy.ps1 -Mode Restore

test-native-tools:
	@set -euo pipefail; \
	  : "$${OUT:?Set OUT to a new absolute external native-tool test directory}"; \
	  "$$POWERSHELL" -NoLogo -NoProfile -File installer/test-native-tools.ps1 -Out "$$OUT"

test:
	@"$$GO" test -mod=readonly -count=1 -timeout=90s ./...

test-race:
	@CGO_ENABLED=1 "$$GO" test -mod=readonly -race -count=1 -timeout=90s ./...

vet:
	@"$$GO" vet -mod=readonly ./...

fmt:
	@set -euo pipefail; git ls-files -z '*.go' | xargs -0 "$$GOFMT" -w

fmt-check:
	@set -euo pipefail; \
	  unformatted="$$(git ls-files -z '*.go' | xargs -0 "$$GOFMT" -l)"; \
	  if [[ -n "$$unformatted" ]]; then \
	    printf 'Run make fmt on:\n%s\n' "$$unformatted" >&2; exit 1; \
	  fi

check: fmt-check test vet

ci: check test-race lint-workflows

ci-windows: test vet test-native-tools

benchmark:
	@CGO_ENABLED=0 "$$GO" test -mod=readonly -run='^$$' \
	  -bench='Benchmark(SourceFrameInspect|SplitMerge|ReceiptFrameResponse|ForwardReceiptPipe|IdleTick)$$' \
	  -benchmem -benchtime=300ms -count=6 -cpu=1 -timeout=5m \
	  ./internal/protocol ./internal/bridge

lint-workflows:
	@"$$GO" run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -color \
	  .github/workflows/ci.yml .github/workflows/release.yml tools/msi/lifecycle/workflow.yml

check-commands:
	@"$$GO" test -mod=readonly -count=1 -timeout=90s ./tools/commands

release-resolve:
	@bash .github/scripts/check-tag-release.sh resolve

release-verify:
	@bash .github/scripts/check-tag-release.sh verify

release-publish:
	@bash .github/scripts/publish-tag-release.sh
