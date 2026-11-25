#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")" && pwd)
: "${OUT_DIR:?private output directory outside repository}" "${BIONIC_ROOT:?owner original Bionic runtime}"
BIONIC_CC=${BIONIC_CC:-arm-linux-gnueabihf-gcc}
BIONIC_LD=${BIONIC_LD:-arm-linux-gnueabihf-ld}
OUT_DIR=$(realpath -m "$OUT_DIR")
case "$OUT_DIR/" in "$ROOT/"*) echo "build outside tracked Git tree" >&2; exit 2;; esac
mkdir -p "$OUT_DIR/objects"
flags=(-std=c11 -O2 -Wall -Wextra -Werror -Wpedantic -fno-builtin -fno-stack-protector
 -fno-strict-aliasing -fPIC -ffreestanding -march=armv7-a -marm -mfpu=neon -mfloat-abi=softfp
 -nostdinc -I"$ROOT/src" -DASP_BIONIC)
objects=()
for f in descriptor io runtime factory child bionic_entry; do
 "$BIONIC_CC" "${flags[@]}" -c "$ROOT/src/$f.c" -o "$OUT_DIR/objects/$f.o"
 objects+=("$OUT_DIR/objects/$f.o")
done
"$BIONIC_CC" "${flags[@]}" -c "$ROOT/src/start32.S" -o "$OUT_DIR/objects/start32.o"
objects+=("$OUT_DIR/objects/start32.o")
LIBGCC=$("$BIONIC_CC" -march=armv7-a -marm -mfloat-abi=softfp -print-libgcc-file-name)
"$BIONIC_LD" -m armelf_linux_eabi -pie --no-undefined --hash-style=both --dynamic-linker=/system/bin/linker \
 -z now -z relro -z noexecstack -e _start -o "$OUT_DIR/asp1-child" "${objects[@]}" \
 -rpath-link "$BIONIC_ROOT/system/lib" -L"$BIONIC_ROOT/system/lib" -l:libdl.so -l:libc.so "$LIBGCC"
arm-linux-gnueabihf-readelf -h -l -d "$OUT_DIR/asp1-child" > "$OUT_DIR/asp1-child.elf.txt"
grep -q '/system/bin/linker' "$OUT_DIR/asp1-child.elf.txt"
sha256sum "$OUT_DIR/asp1-child"
