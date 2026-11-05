#!/usr/bin/env bash
# Actual ARM32 Android 5 API22 / Bionic production child. Not an NDK mock.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")" && pwd)
: "${OUT_DIR:?private output directory outside repository}" "${BIONIC_CC:?ARM32 compiler}" "${BIONIC_LD:?ARM32 linker}" "${BIONIC_ROOT:?owner original Bionic runtime}"
OUT_DIR=$(realpath -m "$OUT_DIR")
case "$OUT_DIR/" in "$ROOT/"*) echo "build outside tracked Git tree" >&2; exit 2;; esac
mkdir -p "$OUT_DIR/objects"
BINUTILS_DIR=${BIONIC_BINUTILS:-$(dirname "$BIONIC_LD")}
[[ -x "$BINUTILS_DIR/arm-linux-gnueabihf-as" ]] || { echo "target ARM assembler missing" >&2; exit 2; }
flags=(-B "$BINUTILS_DIR/" -std=c11 -O2 -Wall -Wextra -Werror -Wpedantic -fno-builtin -fno-stack-protector
 -fno-strict-aliasing -fPIC -ffreestanding -march=armv7-a -marm -mfpu=neon -mfloat-abi=softfp
 -nostdinc -I"$ROOT/src" -DASP_BIONIC)
objects=()
for f in descriptor io runtime factory_adapter vendor_factory sha256 child bionic_entry; do
 "$BIONIC_CC" "${flags[@]}" -c "$ROOT/src/$f.c" -o "$OUT_DIR/objects/$f.o"
 objects+=("$OUT_DIR/objects/$f.o")
done
"$BIONIC_CC" "${flags[@]}" -c "$ROOT/src/start32.S" -o "$OUT_DIR/objects/start32.o"
objects+=("$OUT_DIR/objects/start32.o")
"$BIONIC_LD" -m armelf_linux_eabi -pie --no-undefined --hash-style=both --dynamic-linker=/system/bin/linker \
 -z now -z relro -z noexecstack -e _start -o "$OUT_DIR/asp1-child" "${objects[@]}" \
 -rpath-link "$BIONIC_ROOT/system/lib" -L"$BIONIC_ROOT/system/lib" -l:libdl.so -l:libc.so "$("$BIONIC_CC" -print-libgcc-file-name)"
readelf -h -l -d -A "$OUT_DIR/asp1-child" > "$OUT_DIR/asp1-child.elf.txt"
python3 - "$OUT_DIR/asp1-child" <<'CHECK'
import struct,sys
data=open(sys.argv[1],'rb').read()
assert data[:7]==b'\x7fELF\x01\x01\x01'
assert struct.unpack_from('<H',data,18)[0]==40
ph=struct.unpack_from('<I',data,28)[0]
width,count=struct.unpack_from('<HH',data,42)
paths=[]
for i in range(count):
 t,off,_,_,size,_,_,_=struct.unpack_from('<8I',data,ph+i*width)
 if t==3: paths.append(data[off:off+size])
assert paths==[b'/system/bin/linker\0'],paths
assert b'libasp.so' in data
assert b'ASP1_TEST_' not in data
CHECK
sha256sum "$OUT_DIR/asp1-child"
