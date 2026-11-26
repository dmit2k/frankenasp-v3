#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")" && pwd)
: "${OUT_DIR:?outside repo}"
ARM32_CC=${ARM32_CC:-arm-linux-gnueabihf-gcc}
OUT_DIR=$(realpath -m "$OUT_DIR")
case "$OUT_DIR/" in "$ROOT/"*) echo "OUT_DIR inside repository forbidden" >&2; exit 2;; esac
mkdir -p "$OUT_DIR"
flags=(-std=c11 -Os -Wall -Wextra -Werror -Wpedantic -fno-strict-aliasing
 -march=armv7-a -marm -mfpu=vfpv3-d16 -mfloat-abi=hard -static -no-pie
 -fstack-protector-strong -D_FORTIFY_SOURCE=2 -ffile-prefix-map="$ROOT"=.
 -Wl,-z,relro,-z,now,-z,noexecstack)
"$ARM32_CC" "${flags[@]}" "$ROOT/launcher/launcher.c" "$ROOT/launcher/alias_policy.c" -o "$OUT_DIR/asp1-launcher-arm32"
arm-linux-gnueabihf-readelf -h -l -d "$OUT_DIR/asp1-launcher-arm32" > "$OUT_DIR/asp1-launcher.elf.txt"
if grep -Eq 'INTERP|NEEDED' "$OUT_DIR/asp1-launcher.elf.txt"; then
 echo "launcher must be static ARM32 without interpreter or needed libs" >&2; exit 3
fi
sha256sum "$OUT_DIR/asp1-launcher-arm32"
