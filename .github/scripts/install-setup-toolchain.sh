#!/usr/bin/env bash
set -euo pipefail
file=installer/toolchain.json
image="$(jq -er '.cross_build_image' "$file")"
cc="$(jq -er '.cross_compiler_package' "$file")"
bin="$(jq -er '.cross_binutils_package' "$file")"
headers="$(jq -er '.cross_headers_package' "$file")"
[[ "$image" == 'ubuntu@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852' &&
  "$cc" == 'gcc-mingw-w64-x86-64-posix=13.2.0-6ubuntu1+26.1' &&
  "$bin" == 'binutils-mingw-w64-x86-64=2.41.90.20240122-1ubuntu1+11.4' &&
  "$headers" == 'mingw-w64-x86-64-dev=11.0.1-3build1' ]] ||
  { echo 'Unapproved native compiler/container pins' >&2; exit 1; }
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "$cc" "$bin" "$headers"
