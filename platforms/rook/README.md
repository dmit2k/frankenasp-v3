# Echo Spot 1st generation (`rook`)

This platform targets the original factory ASP from rook firmware build
`304.6.9.0_user_690918120` (Fire OS 5.5.6.9). The DSP adapter is **not**
compatible with arbitrary `libasp.so` builds. The 55-entry SHA-256 manifest
pins its complete runtime and configuration closure; missing or mismatched
owner-supplied resources fail closed before READY.

## PCM / TECHO5 contract

- Input: ASP1 PCM-only descriptor, one interleaved buffer per exchange,
  320 frames at 16 kHz, six packed signed 24-bit channels (5,760 bytes).
- Channels: four physical MIC channels (slots 0–3) and two hardware REF
  channels (slots 4–5). Physical channel polarity, order and reference
  timing require acoustic qualification on the target device.
- Output: one A5 READY byte followed by 640 bytes of mono PCM16 per input
  block. The first 20-ms block includes an initial 64-sample FIFO pad.
- Factory processing: 128-frame quanta, direct pinned `GenericAFE` path,
  stereo REF upsampling from 16 to 48 kHz, and factory mode-7 transport
  metadata stripped from the output PCM LSBs.
- Calibration: the helper reads authenticated device-specific `miccal.0..3`
  through `/proc/idme/`, applies the validated Q30 gains to captured
  microphones exactly once, and never stores or logs those values. Missing
  calibration prevents startup.

The helper has no mute detection, PCM-output censorship or privacy policy.
TECHO5 applies SoftwareCut at the capture boundary. Already-captured PCM,
including DSP-delayed frames, remains valid output after a mute transition.

The existing TECHO5 native capture and ASP1 wire protocol are used unchanged.
No platform-specific changes to TECHO5 are required for the qualified capture
geometry. The factory route differs from athena; the public rook `asp_process`
path did not receive an effective playback reference in offline experiments.

## Original runtime and build

Prepare a **private**, matched Bionic API-22 runtime extracted from your own
rook firmware or backup. Do not place factory libraries, firmware, calibration
or private runtime material in Git. The helper uses original
`/system/bin/linker`, `/system/lib/libasp.so` and its complete dependencies,
and `/vendor/etc/audio-algorithms/` including `AFE.cfg` and coefficient files.

Compile from this platform directory with an ARM32 cross-toolchain. Set
`OUT_DIR` outside the checkout, `BIONIC_CC`, `BIONIC_LD`, `BIONIC_ROOT` and
optionally `BIONIC_BINUTILS` for the helper; use `ARM32_CC`,
`ARM32_SYSROOT` and optionally `ARM32_BINUTILS` for the static Linux launcher.

```sh
bash build_bionic.sh
bash build_launcher.sh
make test
make sanitize
```

The build links against original *user-supplied* Bionic API-22 objects; it
never embeds proprietary files. The launcher requires the established FD3/FD4
TECHO5 session entry, a root-owned private installation tree and a bundled,
owner-verified system/vendor source pair. It does not use the device's older
live vendor configuration. Build and native tests do not mount or modify a
running device.

## Acceptance limitations

The original factory DSP has run in a full-system offline VM with matched
runtime assets and synthetic PCM. This is not evidence of real-time behavior,
valid physical MIC/REF mapping, calibration accuracy, false wake prevention,
or voice quality on a real Spot. Native Linux 4.9 mount namespace, chroot,
capabilities and launcher process timing still require separately authorized
hardware tests. A change to an active TECHO5 service is not part of this stage.
