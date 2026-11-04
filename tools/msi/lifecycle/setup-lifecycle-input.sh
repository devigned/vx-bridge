#!/usr/bin/env bash
set -euo pipefail
: "${REVISION:?Exact source required}"
: "${WORKFLOW_REVISION:?Exact workflow required}"
: "${RUNNER_TEMP:?Hosted output required}"
head="$(git rev-parse HEAD)"
state="$(git status --porcelain --untracked-files=all --ignored=matching)"
[[ "$GITHUB_ACTIONS" == true && "$RUNNER_ENVIRONMENT" == github-hosted &&
  "$GITHUB_EVENT_NAME" == workflow_dispatch && "$GITHUB_WORKFLOW" == 'VX Bridge disposable lifecycle' &&
  "$GITHUB_REPOSITORY" == devigned/vx-bridge &&
  "$GITHUB_RUN_ATTEMPT" == 1 && "$REVISION" =~ ^[0-9a-f]{40}$ &&
  "$REVISION" == "$WORKFLOW_REVISION" && "$head" == "$REVISION" && -z "$state" && "$(uname -m)" == x86_64 ]]

bundle="$RUNNER_TEMP/lifecycle-build-input"
[[ ! -e "$bundle" ]]
mkdir "$bundle"
native="$RUNNER_TEMP/lifecycle-native"
OUT="$native" make build-setup-native
mkdir "$bundle/native"
cp "$native/setup.exe" "$native/setup-actions.dll" "$native/native-inputs.txt" "$native/native-SHA256SUMS" "$bundle/native/"
mkdir -p "$bundle/versions/a" "$bundle/versions/b"
for pair in 'a v0.0.1' 'b v0.0.2'; do
  read -r role version <<< "$pair"
  application="$RUNNER_TEMP/lifecycle-application-$role"
  RELEASE_VERSION="$version" OUT="$application" make package
  archive="vx-bridge-windows-amd64-$version-$REVISION.zip"
  cp "$application/$archive" "$application/$archive.sha256" "$bundle/versions/$role/"
done
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.sourceRevision=$REVISION" -o "$bundle/lifecycle-worker.exe" ./tools/msi/lifecycle
go version -m "$bundle/lifecycle-worker.exe" > "$bundle/worker-build-info.txt"
grep -aFq "$REVISION" "$bundle/lifecycle-worker.exe"
/usr/bin/x86_64-w64-mingw32-gcc-posix --version > "$bundle/compiler.txt"
dpkg-query -W -f='${Package}=${Version}\n' gcc-mingw-w64-x86-64-posix \
  binutils-mingw-w64-x86-64 mingw-w64-x86-64-dev > "$bundle/packages.txt"
cat /usr/share/doc/gcc-mingw-w64-x86-64-posix/copyright \
  /usr/share/doc/mingw-w64-x86-64-dev/copyright > "$bundle/mingw-copyright.txt"
count=0
while IFS= read -r line; do
  if [[ "$line" =~ ^([0-9a-f]{64})\ \ (.+)$ ]]; then
    expected="${BASH_REMATCH[1]}"; path="${BASH_REMATCH[2]}"
    [[ "$path" == "$PWD/"* ]]
    relative="${path#"$PWD/"}"
    [[ "$relative" == installer/setup/* || "$relative" == installer/managed/state.[ch] ]]
    actual="$(git show "$REVISION:$relative" | sha256sum | cut -d' ' -f1)"
    [[ "$actual" == "$expected" ]]
    count=$((count+1))
  fi
done < "$bundle/native/native-inputs.txt"
[[ "$count" == 13 ]]
(
  cd "$bundle"
  sha256sum native/setup.exe native/setup-actions.dll native/native-inputs.txt native/native-SHA256SUMS \
    "versions/a/vx-bridge-windows-amd64-v0.0.1-$REVISION.zip" \
    "versions/a/vx-bridge-windows-amd64-v0.0.1-$REVISION.zip.sha256" \
    "versions/b/vx-bridge-windows-amd64-v0.0.2-$REVISION.zip" \
    "versions/b/vx-bridge-windows-amd64-v0.0.2-$REVISION.zip.sha256" \
    lifecycle-worker.exe worker-build-info.txt compiler.txt packages.txt mingw-copyright.txt > INPUT-SHA256SUMS
)
jq -n --arg source "$REVISION" --arg workflow "$WORKFLOW_REVISION" \
  --arg run "$GITHUB_RUN_ID" --arg attempt "$GITHUB_RUN_ATTEMPT" \
  '{schema:1,source_revision:$source,workflow_revision:$workflow,run_id:$run,run_attempt:$attempt,
    image:"ubuntu@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852",
    platform:"linux/amd64",versions:["v0.0.1","v0.0.2"],
    scope:"PRIVATE DISPOSABLE LIFECYCLE INPUT; no product execution in Linux; no release/tags"}' > "$bundle/provenance.json"
head="$(git rev-parse HEAD)"
state="$(git status --porcelain --untracked-files=all --ignored=matching)"
[[ "$head" == "$REVISION" && -z "$state" ]]
