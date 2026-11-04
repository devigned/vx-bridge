#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'tag release gate: %s\n' "$*" >&2; exit 1; }

mode="${1:-resolve}"
[[ "$mode" == resolve || "$mode" == verify ]] || fail 'use resolve or verify'
[[ "${GITHUB_EVENT_NAME:-}" == push ]] || fail 'version-tag push required'
event="${GITHUB_EVENT_PATH:?}"
jq -e '.created == true and .deleted == false and .forced == false and
  .before == "0000000000000000000000000000000000000000"' "$event" >/dev/null ||
  fail 'only new tags may release; deleted or moved refs are rejected'
ref="$(jq -er .ref "$event")"
oid="$(jq -er .after "$event")"
[[ "$ref" == "${GITHUB_REF:-}" && "$ref" == refs/tags/* ]] || fail 'event and tag ref mismatch'
version="${ref#refs/tags/}"
[[ ${#version} -le 64 && "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail 'only canonical vMAJOR.MINOR.PATCH tags (at most 64 characters) are supported'
[[ "$oid" =~ ^[0-9a-f]{40}$ && "$oid" != 0000000000000000000000000000000000000000 ]] ||
  fail 'event must pin a full tag-ref object ID'
[[ "${GITHUB_WORKFLOW_REF:-}" == "${GITHUB_REPOSITORY:?}/.github/workflows/release.yml@$ref" ]] ||
  fail 'only the version-tag release workflow may publish'
[[ "$(git rev-parse --verify "$ref")" == "$oid" ]] || fail 'local tag moved or differs from event object ID'
revision="$(git rev-parse --verify "$ref^{commit}")"
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || fail 'tag does not peel to a commit'
[[ "$(git rev-parse HEAD)" == "$revision" ]] || fail 'checkout must be the tagged commit, never current main'
[[ "$(gh api "repos/$GITHUB_REPOSITORY/git/ref/tags/$version" --jq .object.sha)" == "$oid" ]] ||
  fail 'remote tag is missing or changed since the push'
main="$(gh api "repos/$GITHUB_REPOSITORY/git/ref/heads/main" --jq .object.sha)"
[[ "$main" =~ ^[0-9a-f]{40}$ ]] || fail 'main must resolve to a commit'
status="$(gh api "repos/$GITHUB_REPOSITORY/compare/$revision...$main" --jq .status)"
[[ "$status" == identical || "$status" == ahead ]] || fail 'tagged commit must already be in main history'

if [[ "$mode" == verify ]]; then
  [[ "$revision" == "${SOURCE_REVISION:-}" && "$version" == "${RELEASE_VERSION:-}" && "$oid" == "${TAG_OID:-}" ]] ||
    fail 'resolved source/version/tag identity differs from this run initial authorization'
else
  printf 'revision=%s\nversion=%s\ntag-oid=%s\n' "$revision" "$version" "$oid" >> "${GITHUB_OUTPUT:?}"
fi
printf 'Tag release source: %s commit=%s tag-object=%s main=%s\n' "$version" "$revision" "$oid" "$main"
