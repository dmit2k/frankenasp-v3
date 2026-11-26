#!/usr/bin/env bash
set -euo pipefail
: "${FW:?extracted crown system and vendor}" "${CHILD:?asp1-child}" "${LAUNCHER:?asp1-launcher-arm32}" "${LIBS:?list of used /system and /vendor libraries}" "${OUT:?output tar}"
W=$(mktemp -d)
D=$W/external-asp
R=$D/lib/rootfs
install -d -m 0700 "$D" "$D/bin" "$D/lib" "$D/etc" "$R"
install -d -m 0755 "$R/system" "$R/system/bin" "$R/system/lib" "$R/vendor" "$R/vendor/lib" "$R/vendor/etc" \
 "$R/factory" "$R/proc" "$R/proc/idme" "$R/sys" "$R/.realproc" "$R/dev" "$R/data"
install -m 0700 "$LAUNCHER" "$D/bin/external-asp"
printf 'ASP1-LAUNCHER-V1\nvendor_mode=OWNER_BUNDLED_MATCHED\nsource_pair=OWNER_VERIFIED\n' > "$D/etc/launcher.profile"
chmod 0600 "$D/etc/launcher.profile"
install -m 0755 "$FW/system/bin/linker" "$R/system/bin/linker"
install -m 0755 "$CHILD" "$R/factory/asp1-child"
while read -r p; do
 case "$p" in
  /system/lib/*|/vendor/lib/*) install -m 0644 "$FW$p" "$R$p" ;;
 esac
done < "$LIBS"
for l in libdl.so libc.so libm.so liblog.so; do install -m 0644 "$FW/system/lib/$l" "$R/system/lib/$l"; done
cp -r "$FW/vendor/etc/audio-algorithms" "$R/vendor/etc/"
rm -f "$R/vendor/etc/audio-algorithms/asp.cfg.orig"
python3 - "$R/vendor/etc/audio-algorithms/asp.cfg" <<'EOF'
import sys
p=sys.argv[1]
t=open(p).read()
i=t.index('"Speaker"')
j=t.index('"Passthrough"',i)
k=t.index(',',j)
t=t[:j]+'"Passthrough": true'+t[k:]
open(p,'w').write(t)
EOF
find "$R/vendor/etc" -type d -exec chmod 0755 {} +
find "$R/vendor/etc" -type f -exec chmod 0644 {} +
ln -s /.realproc/self "$R/proc/self"
ln -s /.realproc/sys "$R/proc/sys"
ln -s /.realproc/cpuinfo "$R/proc/cpuinfo"
ln -s /sys/firmware/devicetree/base "$R/proc/device-tree"
for i in 0 1 2 3; do ln -s "/proc/device-tree/idme/miccal.$i/value" "$R/proc/idme/miccal.$i"; done
tar -C "$W" --numeric-owner --owner=0 --group=0 -czf "$OUT" external-asp
rm -rf "$W"
ls -la "$OUT"
