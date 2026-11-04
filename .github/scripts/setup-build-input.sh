#!/usr/bin/env bash
set -euo pipefail
: "${REVISION:?Exact source required}"
: "${WORKFLOW_REVISION:?Exact workflow required}"
: "${RUNNER_TEMP:?Runner output required}"
: "${RELEASE_VERSION:?Explicit package version required}"
head="$(git rev-parse HEAD)"
state="$(git status --porcelain --untracked-files=all --ignored=matching)"
[[ "$REVISION" =~ ^[0-9a-f]{40}$ && "$WORKFLOW_REVISION" =~ ^[0-9a-f]{40}$ &&
  "$head" == "$REVISION" && -z "$state" && "${SETUP_BUILD_PROFILE:-normal}" == normal &&
  "$(uname -m)" == x86_64 ]] ||
  { echo 'Exact clean build source and normal installer profile required' >&2; exit 1; }

native="$RUNNER_TEMP/setup-native"
application="$RUNNER_TEMP/setup-application"
staging="$RUNNER_TEMP/setup-linux-staging"
bundle="$RUNNER_TEMP/setup-build-input"
[[ ! -e "$bundle" ]]
OUT="$native" make build-setup-native
OUT="$application" make package
identity="$REVISION"
if [[ "$RELEASE_VERSION" != development ]]; then identity="$RELEASE_VERSION-$REVISION"; fi
archiveName="vx-bridge-windows-amd64-$identity.zip"
archive="$application/$archiveName"
ARCHIVE="$archive" NATIVE_DIR="$native" OUT="$staging" make prepare-setup
mkdir "$bundle"
cp "$native/setup.exe" "$native/setup-actions.dll" "$native/native-inputs.txt" \
  "$native/native-SHA256SUMS" "$archive" "$archive.sha256" "$bundle/"
cp "$staging/payload.wxs" "$bundle/linux-payload.wxs"
cp "$staging/setup-input.json" "$bundle/linux-setup-input.json"
/usr/bin/x86_64-w64-mingw32-gcc-posix --version > "$bundle/compiler.txt"
dpkg-query -W -f='${Package}=${Version}\n' gcc-mingw-w64-x86-64-posix \
  binutils-mingw-w64-x86-64 mingw-w64-x86-64-dev > "$bundle/packages.txt"
cat /usr/share/doc/gcc-mingw-w64-x86-64-posix/copyright \
  /usr/share/doc/mingw-w64-x86-64-dev/copyright > "$bundle/mingw-copyright.txt"

count=0
while IFS= read -r line; do
  if [[ "$line" =~ ^([0-9a-f]{64})\ \ (.+)$ ]]; then
    expected="${BASH_REMATCH[1]}"
    path="${BASH_REMATCH[2]}"
    [[ "$path" == "$PWD/"* ]]
    relative="${path#"$PWD/"}"
    [[ "$relative" == installer/setup/* || "$relative" == installer/managed/state.[ch] ]]
    [[ "$(sha256sum "$path" | cut -d' ' -f1)" == "$expected" &&
      "$(git show "$REVISION:$relative" | sha256sum | cut -d' ' -f1)" == "$expected" ]]
    count=$((count+1))
  fi
done < "$native/native-inputs.txt"
[[ "$count" == 11 ]]
(
  cd "$bundle"
  sha256sum setup.exe setup-actions.dll native-inputs.txt native-SHA256SUMS \
    "$archiveName" "$archiveName.sha256" \
    linux-payload.wxs linux-setup-input.json compiler.txt packages.txt mingw-copyright.txt > INPUT-SHA256SUMS
)
jq -n --arg source "$REVISION" --arg workflow "$WORKFLOW_REVISION" \
  --arg version "$RELEASE_VERSION" \
  --arg run "$GITHUB_RUN_ID" --arg attempt "$GITHUB_RUN_ATTEMPT" \
  --arg image 'ubuntu@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852' \
  --slurpfile prepared "$staging/setup-input.json" \
  '{schema:1,source_revision:$source,workflow_revision:$workflow,release_version:$version,run_id:$run,run_attempt:$attempt,
    image:$image,platform:"linux/amd64",warning_exceptions:$prepared[0].warning_exceptions,
    ice_exceptions:$prepared[0].ice_exceptions,scope:"BUILD ONLY; no native executable or MSI execution"}' \
  > "$bundle/provenance.json"
head="$(git rev-parse HEAD)"
state="$(git status --porcelain --untracked-files=all --ignored=matching)"
[[ "$head" == "$REVISION" && -z "$state" ]]
