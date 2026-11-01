# Echo Spot 1st generation (`rook`)

This implementation uses an owner-provided ARM32 Bionic factory runtime.
It does not pin OTA versions, manufacturer library checksums or exact DSP
function addresses. The runtime must still contain a compatible internal DSP
interface: changed private vtable layout or buffer ABI may require an adapter
update. Incompatible runtime resources fail before READY where detectable.

## PCM / TECHO5 contract

- Input: ASP1 PCM-only descriptor, one interleaved buffer per exchange,
  320 frames at 16 kHz, six packed signed 24-bit channels (5,760 bytes).
- Channels: four physical MIC channels (slots 0–3) and two hardware REF
  channels (slots 4–5). Physical channel polarity, order and reference
  timing require acoustic qualification on the target device.
- Output: one A5 READY byte followed by 640 bytes of mono PCM16 per input
  block. The first 20-ms block includes an initial 64-sample FIFO pad.
- Factory processing: 128-frame quanta, direct internal `GenericAFE` path,
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

Prepare a **private**, internally compatible ARM32 Bionic runtime extracted from your own
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
owner-selected system/vendor source pair. It does not use the device's older
live vendor configuration. Build and native tests do not mount or modify a
running device.

## Hardware validation and limitations

The rook helper has been built, deployed and tested on a **physical Echo Spot
(1st generation, 2017)** with TECHO5 External ASP and the original factory DSP. On-device tests have exercised the secure launcher, isolated Bionic
runtime and PCM processing; the External ASP path has also run in a live
TECHO5 session. Native tests and a full-system offline VM were used in
addition to, not instead of, device testing.

This confirms hardware operation on the tested Spot, not exhaustive acoustic
qualification (for example, false-wake rates or voice quality in all
conditions). The maintainer does not have other Echo models available for
adaptation or hardware validation. Other models are neither supported nor
claimed compatible without their own device-specific work and testing.
