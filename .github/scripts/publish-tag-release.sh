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
installer="vx-bridge-installer-windows-amd64-$RELEASE_VERSION.exe"
[[ -d "$directory" && ! -L "$directory" &&
  "$(find "$directory" -mindepth 1 -maxdepth 1 | wc -l)" -eq 2 ]] ||
  fail 'download must contain exactly the installer EXE and its checksum'
for file in "$installer"; do
  [[ -f "$directory/$file" && ! -L "$directory/$file" &&
    -f "$directory/$file.sha256" && ! -L "$directory/$file.sha256" ]] || fail 'unexpected asset/checksum file'
  line="$(cat "$directory/$file.sha256")"
  sum="${line%% *}"
  [[ "$sum" =~ ^[0-9a-f]{64}$ && "$line" == "$sum  $file" &&
    "$(wc -c < "$directory/$file.sha256")" -eq $((64 + 2 + ${#file} + 1)) ]] ||
    fail 'checksum must contain exactly one canonical line naming this asset'
  (cd "$directory" && sha256sum --strict --check "$file.sha256")
done
REVISION="$SOURCE_REVISION" INSTALLER_ARCHIVE="$directory/$installer" make verify-installer

notes="${RUNNER_TEMP:?}/tag-release-notes.txt"
cat > "$notes" <<EOF
$RELEASE_VERSION - unsigned Windows application and installer. Source: $SOURCE_REVISION
Tag-ref object: $TAG_OID
Download $installer and open it normally as the Windows user who will use the bridge, not as administrator.
This one EXE contains the matching native Setup, MSI and support files; no ZIP extraction or separate bridge download is needed.
Only the setup EXE and its .exe.sha256 checksum are uploaded. GitHub also supplies source archives.
Windows Installer still owns installation, repair, upgrades and uninstall. Normal application startup never invokes MSI.
Embedded metadata records the exact source, actual PackageCode, full MSI hash and native owner identity.
Toolchain/license notices and checksums are inside the EXE; /extract can save verified media to a new absolute folder for inspection or explicit recovery.
The installer and application are not Authenticode-signed. No automatic-update claim is made.
Publication does not qualify hardware, games, startup or lifecycle behavior on your machine.
Checksums detect corruption; they do not authenticate a publisher.
Build workflow: $GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID
EOF
# --verify-tag forbids the CLI's implicit missing-tag path. Draft creation
# fails on duplicate releases; no upload/clobber into an existing release.
gh release create "$RELEASE_VERSION" "$directory/$installer" "$directory/$installer.sha256" \
  --repo "$GITHUB_REPOSITORY" --verify-tag --target "$SOURCE_REVISION" --draft \
  --prerelease=false --latest=false --title "$RELEASE_VERSION" --notes-file "$notes"
bash "$scripts/check-tag-release.sh" verify
gh release edit "$RELEASE_VERSION" --repo "$GITHUB_REPOSITORY" \
  --draft=false --prerelease=false --latest=false
