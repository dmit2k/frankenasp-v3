#!/usr/bin/env bash
# Build the static native Linux ARM32 launcher for rook.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")" && pwd)
ATHENA=$(cd "$ROOT/../athena" && pwd)
: "${OUT_DIR:?private output outside repository}" "${ARM32_CC:?Linux ARM32 C compiler}"
OUT_DIR=$(realpath -m "$OUT_DIR")
case "$OUT_DIR/" in "$ROOT/"*|"$ATHENA/"*) echo "build output inside source tree" >&2; exit 2;; esac
mkdir -p "$OUT_DIR"
flags=(-std=c11 -Os -Wall -Wextra -Werror -Wpedantic -fno-strict-aliasing
 -march=armv7-a -marm -mfpu=vfpv3-d16 -mfloat-abi=hard -static -no-pie
 -fstack-protector-strong -D_FORTIFY_SOURCE=2
 -ffile-prefix-map="$ROOT"=. -ffile-prefix-map="$ATHENA"=.
 -I"$ATHENA/launcher" -Wl,-z,relro,-z,now,-z,noexecstack)
if [[ -n ${ARM32_SYSROOT:-} ]]; then
 flags+=(--sysroot="$ARM32_SYSROOT"
 -isystem "$ARM32_SYSROOT/usr/arm-linux-gnueabihf/include"
 -B "$ARM32_SYSROOT/usr/arm-linux-gnueabihf/lib/"
 -L "$ARM32_SYSROOT/usr/arm-linux-gnueabihf/lib")
fi
if [[ -n ${ARM32_BINUTILS:-} ]]; then flags+=(-B "$ARM32_BINUTILS/"); fi
"$ARM32_CC" "${flags[@]}" "$ROOT/launcher/launcher.c" \
 "$ATHENA/launcher/alias_policy.c" -o "$OUT_DIR/asp1-launcher-arm32"
readelf -h -l -d -A "$OUT_DIR/asp1-launcher-arm32" > "$OUT_DIR/asp1-launcher.elf.txt"
grep -q 'Class:.*ELF32' "$OUT_DIR/asp1-launcher.elf.txt"
grep -q 'Machine:.*ARM' "$OUT_DIR/asp1-launcher.elf.txt"
if grep -Eq 'INTERP|NEEDED' "$OUT_DIR/asp1-launcher.elf.txt"; then
 echo "launcher must be static" >&2; exit 3
fi
sha256sum "$OUT_DIR/asp1-launcher-arm32"
