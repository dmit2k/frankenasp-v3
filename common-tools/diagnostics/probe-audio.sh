#!/bin/sh
# FrankenASP V3: read-only capture and calibration-path availability probe.
# Does not read calibration values, factory files, or user/device identifiers.
# Run locally on a TECHO5 device: sh probe-audio.sh donut|cronos
# Review all output before posting it publicly.

case "${1:-}" in
    donut|cronos) platform=$1 ;;
    *) printf 'Usage: sh %s donut|cronos\n' "$0" >&2; exit 2 ;;
esac

presence() {
    if [ -e "$2" ]; then
        printf '%s: present\n' "$1"
    else
        printf '%s: not visible\n' "$1"
    fi
}

printf 'FrankenASP V3 audio probe (read-only)\n'
printf 'Platform (operator-supplied): %s\n' "$platform"
printf 'Kernel release: %s\n' "$(uname -r)"
printf 'CPU architecture: %s\n' "$(uname -m)"

printf '\nALSA cards:\n'
if [ -r /proc/asound/cards ]; then
    sed -n '1,25p' /proc/asound/cards
else
    printf 'not available\n'
fi
printf '\nALSA PCM devices:\n'
if [ -r /proc/asound/pcm ]; then
    sed -n '1,50p' /proc/asound/pcm
else
    printf 'not available\n'
fi

printf '\nExpected PCM device nodes (presence only):\n'
if [ "$platform" = donut ]; then
    presence 'MIC hw:0,1 capture' /dev/snd/pcmC0D1c
    presence 'REF hw:0,7 capture' /dev/snd/pcmC0D7c
    devices='1 7'
else
    presence 'MIC/REF hw:0,22 capture' /dev/snd/pcmC0D22c
    devices='22'
fi

printf '\nActive ALSA capture parameters (no audio is recorded):\n'
for device in $devices; do
    path="/proc/asound/card0/pcm${device}c/sub0/hw_params"
    printf 'hw:0,%s: ' "$device"
    if [ ! -r "$path" ]; then
        printf 'not available\n'
        continue
    fi
    # Only allowlisted PCM geometry fields; no arbitrary proc contents.
    found=no
    while IFS= read -r line; do
        case "$line" in
            'access: '*|'format: '*|'channels: '*|'rate: '*|'period_size: '*|'buffer_size: '*)
                if [ "$found" = no ]; then
                    printf '\n'
                    found=yes
                fi
                printf '  %s\n' "$line"
                ;;
        esac
    done < "$path"
    if [ "$found" = no ]; then
        printf 'closed or no geometry reported\n'
    fi
done

printf '\nFactory ASP visibility (presence only; unmounted partitions are not checked):\n'
presence 'system libasp.so' /system/lib/libasp.so
presence 'vendor libasp.so' /vendor/lib/libasp.so

printf '\nMicrophone calibration paths (presence only; contents NEVER read):\n'
for index in 0 1 2 3; do
    found=no
    for base in /proc/idme /proc/device-tree/idme /sys/firmware/devicetree/base/idme; do
        if [ -e "$base/miccal.$index" ] || [ -e "$base/miccal.$index/value" ]; then
            found=yes
        fi
    done
    printf 'miccal.%s: %s\n' "$index" "$found"
done

printf '\nNo files, partitions, or mixer settings were modified.\n'
printf 'Inspect this report before sharing; NEVER post calibration values or factory binaries.\n'
