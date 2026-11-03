#!/bin/sh
# FrankenASP V3: generic read-only ALSA and calibration-path probe.
# No audio capture, mixer changes, firmware reads or calibration values.
# Run on a TECHO5 device: sh probe-audio.sh
# Review the complete report before sharing it publicly.

if [ "$#" -ne 0 ]; then
    printf 'Usage: sh %s\n' "$0" >&2
    exit 2
fi

printf 'FrankenASP V3 audio probe (read-only)\n'
printf 'Kernel release: %s\n' "$(uname -r)"
printf 'CPU architecture: %s\n' "$(uname -m)"

printf '\nALSA cards:\n'
if [ -r /proc/asound/cards ]; then
    sed -n '1,40p' /proc/asound/cards
else
    printf 'not available\n'
fi

printf '\nALSA PCM devices:\n'
if [ -r /proc/asound/pcm ]; then
    sed -n '1,80p' /proc/asound/pcm
else
    printf 'not available\n'
fi

printf '\nCapture device nodes (presence only):\n'
count=0
for node in /dev/snd/pcmC[0-9]*D[0-9]*c; do
    [ -e "$node" ] || continue
    printf '  %s\n' "$node"
    count=$((count + 1))
done
if [ "$count" -eq 0 ]; then
    printf 'none visible\n'
fi

printf '\nCapture stream geometry (only currently opened streams report parameters):\n'
count=0
for path in /proc/asound/card[0-9]*/pcm[0-9]*c/sub[0-9]*/hw_params; do
    [ -r "$path" ] || continue
    count=$((count + 1))
    printf '%s:\n' "${path#/proc/asound/}"
    reported=no
    # Allowlisted ALSA geometry only, never stream contents or owner PIDs.
    while IFS= read -r line; do
        case "$line" in
            'access: '*|'format: '*|'channels: '*|'rate: '*|'period_size: '*|'buffer_size: '*)
                printf '  %s\n' "$line"
                reported=yes
                ;;
        esac
    done < "$path"
    if [ "$reported" = no ]; then
        printf '  closed or no active geometry\n'
    fi
done
if [ "$count" -eq 0 ]; then
    printf 'not available\n'
fi

printf '\nKnown IDME microphone calibration paths (presence only; contents NOT read):\n'
count=0
for base in /proc/idme /proc/device-tree/idme /sys/firmware/devicetree/base/idme; do
    for entry in "$base"/miccal.*; do
        [ -e "$entry" ] || [ -L "$entry" ] || continue
        name=${entry##*/}
        index=${name#miccal.}
        case "$index" in
            ''|*[!0-9]*) continue ;;
        esac
        # Some kernels expose a file or symlink, others a directory with /value.
        if [ -d "$entry" ]; then
            [ -e "$entry/value" ] || [ -L "$entry/value" ] || continue
        fi
        printf '  %s/miccal.%s: present\n' "$base" "$index"
        count=$((count + 1))
    done
done
if [ "$count" -eq 0 ]; then
    printf 'none visible in known IDME locations (not proof of absent calibration)\n'
fi

printf '\nNo capture devices were opened, and no files or mixer settings were changed.\n'
printf 'Review the report before posting. Never share calibration values or factory files.\n'
