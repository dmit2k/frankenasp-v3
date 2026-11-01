#!/usr/bin/env bash
# Build the native ARM32 Bionic API-22 helper with owner-extracted libraries.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")" && pwd)
ATHENA=$(cd "$ROOT/../athena" && pwd)
: "${OUT_DIR:?private output outside repository}" "${BIONIC_CC:?ARM32 compiler}" "${BIONIC_LD:?ARM32 linker}" "${BIONIC_ROOT:?matched factory runtime}"
OUT_DIR=$(realpath -m "$OUT_DIR")
case "$OUT_DIR/" in "$ROOT/"*|"$ATHENA/"*) echo "build output inside source tree" >&2; exit 2;; esac
mkdir -p "$OUT_DIR/objects"
BINUTILS_DIR=${BIONIC_BINUTILS:-$(dirname "$BIONIC_LD")}
flags=(-B "$BINUTILS_DIR/" -std=c11 -O2 -Wall -Wextra -Werror
 -fno-builtin -fno-stack-protector -fno-strict-aliasing -fPIC -ffreestanding
 -march=armv7-a -marm -mfpu=neon -mfloat-abi=softfp -nostdinc
 -ffile-prefix-map="$ROOT"=. -ffile-prefix-map="$ATHENA"=.
 -I"$ROOT/src" -I"$ATHENA/src" -DASP_BIONIC)
objects=()
for name in src/front.c src/vendor.c src/child.c \
 "$ATHENA/src/descriptor.c" "$ATHENA/src/io.c" "$ATHENA/src/bionic_entry.c"; do
 key=$(basename "${name%.c}")
 source="$name"; [[ "$source" = /* ]] || source="$ROOT/$name"
 "$BIONIC_CC" "${flags[@]}" -c "$source" -o "$OUT_DIR/objects/$key.o"
 objects+=("$OUT_DIR/objects/$key.o")
done
"$BIONIC_CC" -B "$BINUTILS_DIR/" -fPIE -march=armv7-a -marm -mfpu=neon -mfloat-abi=softfp \
 -c "$ROOT/src/start32.S" -o "$OUT_DIR/objects/start32.o"
objects+=("$OUT_DIR/objects/start32.o")
"$BIONIC_LD" -pie -m armelf_linux_eabi -e _start --hash-style=both \
 --dynamic-linker=/system/bin/linker -z now -z relro -z noexecstack \
 -o "$OUT_DIR/asp1-child" "${objects[@]}" \
 -rpath-link "$BIONIC_ROOT/system/lib" -L"$BIONIC_ROOT/system/lib" \
 -l:libdl.so -l:libc.so -l:libm.so
readelf -h -l -d -A "$OUT_DIR/asp1-child" > "$OUT_DIR/asp1-child.elf.txt"
grep -q 'Machine:.*ARM' "$OUT_DIR/asp1-child.elf.txt"
grep -q '/system/bin/linker' "$OUT_DIR/asp1-child.elf.txt"
if grep -Eq 'MOCK_ASP|ROOK_TEST_ONLY' "$OUT_DIR/asp1-child.elf.txt"; then
 echo "Unexpected test or environment markers in ELF inspection" >&2; exit 2
fi
sha256sum "$OUT_DIR/asp1-child"
