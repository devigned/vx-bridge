#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'tag publication: %s\n' "$*" >&2; exit 1; }
scripts="$(cd "$(dirname "$0")" && pwd)"
bash "$scripts/check-tag-release.sh" verify
[[ "${ARTIFACT_ID:-}" =~ ^[1-9][0-9]*$ ]] || fail 'missing exact same-run build artifact ID'
[[ "${ARTIFACT_DIGEST:-}" =~ ^[0-9a-f]{64}$ ]] || fail 'missing exact same-run artifact digest'
gh api "repos/$GITHUB_REPOSITORY/actions/artifacts/$ARTIFACT_ID" |
  jq -e --arg source "$SOURCE_REVISION" --arg run "$GITHUB_RUN_ID" \
    --arg name "vx-bridge-tag-assets-$RELEASE_VERSION-$SOURCE_REVISION" --arg digest "sha256:$ARTIFACT_DIGEST" '
    .expired == false and .name == $name and .digest == $digest and
    (.workflow_run.id | tostring) == $run and .workflow_run.head_sha == $source
  ' >/dev/null || fail 'artifact is not the exact immutable output of this source/run'
directory="${RELEASE_DIRECTORY:?}"
archive="vx-bridge-windows-amd64-$RELEASE_VERSION-$SOURCE_REVISION.zip"
installer="vx-bridge-installer-windows-amd64-$RELEASE_VERSION-$SOURCE_REVISION.zip"
msi="vx-bridge-windows-amd64-$RELEASE_VERSION-$SOURCE_REVISION.msi"
[[ -d "$directory" && ! -L "$directory" &&
  "$(find "$directory" -mindepth 1 -maxdepth 1 | wc -l)" -eq 6 ]] ||
  fail 'download must contain exactly app ZIP, installer ZIP, direct MSI and their checksums'
for file in "$archive" "$installer" "$msi"; do
  [[ -f "$directory/$file" && ! -L "$directory/$file" &&
    -f "$directory/$file.sha256" && ! -L "$directory/$file.sha256" ]] || fail 'unexpected asset/checksum file'
  line="$(cat "$directory/$file.sha256")"
  sum="${line%% *}"
  [[ "$sum" =~ ^[0-9a-f]{64}$ && "$line" == "$sum  $file" ]] || fail 'checksum must name only this exact asset'
  (cd "$directory" && sha256sum --strict --check "$file.sha256")
done
REVISION="$SOURCE_REVISION" ARCHIVE="$directory/$archive" INSTALLER_ARCHIVE="$directory/$installer" make verify-installer
embedded="$(unzip -p "$directory/$installer" bridge.msi | sha256sum | cut -d' ' -f1)"
[[ "$embedded" == "$(sha256sum "$directory/$msi" | cut -d' ' -f1)" ]] ||
  fail 'direct MSI differs from the installer bundle matching Setup/MSI'

notes="${RUNNER_TEMP:?}/tag-release-notes.txt"
cat > "$notes" <<EOF
$RELEASE_VERSION - unsigned Windows application and installer. Source: $SOURCE_REVISION
Tag-ref object: $TAG_OID
Download the installer ZIP, extract it to one folder, and run setup.exe.
Keep its matching bridge.msi beside setup.exe; the direct MSI asset is not a standalone installer.
The separate application ZIP contains the six-file portable application distribution.
Installer metadata records the actual emitted PackageCode, full MSI hash and native owner identity.
Toolchain/license notices and checksums are included. No signing or automatic-update claim is made.
Publication does not qualify hardware, games, startup or lifecycle behavior on your machine.
Checksums detect corruption; they do not authenticate a publisher.
Build workflow: $GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID
EOF
# --verify-tag forbids the CLI's implicit missing-tag path. Draft creation
# fails on duplicate releases; no upload/clobber into an existing release.
gh release create "$RELEASE_VERSION" "$directory/$archive" "$directory/$archive.sha256" \
  "$directory/$installer" "$directory/$installer.sha256" "$directory/$msi" "$directory/$msi.sha256" \
  --repo "$GITHUB_REPOSITORY" --verify-tag --target "$SOURCE_REVISION" --draft \
  --prerelease=false --latest=false --title "$RELEASE_VERSION" --notes-file "$notes"
bash "$scripts/check-tag-release.sh" verify
gh release edit "$RELEASE_VERSION" --repo "$GITHUB_REPOSITORY" \
  --draft=false --prerelease=false --latest=false
