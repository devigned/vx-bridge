#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
: "${OUT:?Set OUT to a new absolute external directory}"
: "${REVISION:?Set REVISION to clean exact HEAD}"
[[ "$OUT" == /* ]]
parent="$(realpath -e "$(dirname "$OUT")")"
OUT="$parent/$(basename "$OUT")"
[[ "$OUT" == /* && "$OUT" != "$root" && "$OUT" != "$root/"* && ! -e "$OUT" ]]
[[ "$(git -C "$root" rev-parse HEAD)" == "$REVISION" && -z "$(git -C "$root" status --porcelain --untracked-files=all)" ]]
cc=/usr/bin/x86_64-w64-mingw32-gcc-posix
command -v "$cc" >/dev/null
for package in \
  'gcc-mingw-w64-x86-64-posix=13.2.0-6ubuntu1+26.1' \
  'binutils-mingw-w64-x86-64=2.41.90.20240122-1ubuntu1+11.4' \
  'mingw-w64-x86-64-dev=11.0.1-3build1'; do
  [[ "$(dpkg-query -W -f='${Version}' "${package%%=*}")" == "${package#*=}" ]]
done
mkdir "$OUT"
sources=("$root/installer/setup/common.c" "$root/installer/managed/state.c")
flags=(-std=c11 -O2 -Wall -Wextra -Werror -DUNICODE -D_UNICODE -D_WIN32_WINNT=0x0601 -D_WIN32_MSI=500
  -static-libgcc '-Wl,--nxcompat,--dynamicbase,--high-entropy-va,--no-insert-timestamp')
libraries=(-lmsi -ladvapi32 -lole32 -lshell32 -luuid -lbcrypt -lcomctl32 -luser32)
"$cc" "${flags[@]}" "-DVX_SOURCE_REVISION=\"$REVISION\"" -shared -o "$OUT/setup-actions.dll" \
  "$root/installer/setup/actions.c" "$root/installer/setup/remove.c" "${sources[@]}" "${libraries[@]}"
x86_64-w64-mingw32-windres -I "$root/installer/setup" "$root/installer/setup/setup.rc" "$OUT/setup-resource.o"
"$cc" "${flags[@]}" -municode -mwindows -o "$OUT/setup.exe" "$root/installer/setup/launcher.c" \
  "${sources[@]}" "$OUT/setup-resource.o" "${libraries[@]}"
"$cc" "${flags[@]}" "-DVX_SOURCE_REVISION=\"$REVISION\"" -ffunction-sections -fdata-sections \
  -Wl,--gc-sections -municode -mwindows -o "$OUT/bundle-stub.exe" "$root/installer/setup/bundle.c" \
  "${sources[@]}" "$OUT/setup-resource.o" "${libraries[@]}"
"$cc" "${flags[@]}" -municode -o "$OUT/private-root-tests.exe" "$root/installer/setup/test_root.c" \
  "${sources[@]}" "${libraries[@]}"
"$cc" "${flags[@]}" -municode -o "$OUT/private-remove-tests.exe" "$root/installer/setup/test_remove.c" \
  "${sources[@]}" "${libraries[@]}"
printf 'source_revision=%s\n' "$REVISION" > "$OUT/native-inputs.txt"
sha256sum "$root"/installer/setup/*.[ch] "$root/installer/setup/setup.rc" "$root/installer/setup/setup.manifest" \
  "$root/installer/managed/state.c" "$root/installer/managed/state.h" >> "$OUT/native-inputs.txt"
"$cc" --version >> "$OUT/native-inputs.txt"
(cd "$OUT" && sha256sum setup.exe setup-actions.dll bundle-stub.exe private-root-tests.exe private-remove-tests.exe > native-SHA256SUMS)
