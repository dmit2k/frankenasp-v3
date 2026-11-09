# Cronos factory microphone processing

Experimental ARM32/Bionic ASR-only helper for Echo Show 5 (2nd generation,
Cronos). It runs the original factory microphone processing library and returns
mono audio through TECHO5 External ASP.

## Validated factory runtime

| Fire OS | Amazon build | Factory library SHA-256 |
|---|---|---|
| 6574.1 | 13222532996 | d8ea580a54fe75a524d50dfec7b8e778c66cef0385a9f0142b84bc58319dbff1 |

Validation covers offline execution with the matching original runtime.
Physical-device operation, microphone calibration effectiveness, CPU usage and
recognition quality require hardware testing. Other firmware combinations need
separate runtime validation.

## Audio contract

Input is one interleaved buffer: 16 kHz, four-channel packed signed PCM24
(S24_3LE), 320 frames, 12 bytes per frame. MIC0/MIC1 and REF0/REF1 retain their
original order and values. Other descriptor geometries fail before READY.

The factory processes 128-frame quanta and returns mono PCM16 at 16 kHz.
The output FIFO starts with exactly 64 zero samples once per session, matching
the existing platform packetization policy. Every 320-frame input period
receives 320 output samples. Pending input and output remain owned by the
session; EOF does not manufacture a partial factory quantum or an extra wire
response. There is no special reset or padding during microphone mute.

ASR-only does not provide a factory Speaker playback/reference producer or
promise factory AEC parity. Capture REF channels are preserved even when
nonzero. Wake-word and speech recognition remain in TECHO5 and its configured
services. The helper does not apply its own calibration, gain, mixing,
resampling, beamforming or echo cancellation.

The launcher uses a matching bundled factory runtime. It validates installed
native calibration aliases and their readable, nonempty kernel sources
without reading coefficients. It accepts the discovered inventory rather than
a fixed microphone count. Missing or invalid sources fail startup.

## Build

Run from the repository root with an existing ARM32 Linux compiler, binutils,
Linux static-library sysroot, and matching original Bionic runtime. Factory
files remain private.

Set these toolchain variables for your environment:

- BIONIC_CC, BIONIC_LD and BIONIC_BINUTILS;
- BIONIC_ROOT, pointing to the extracted factory cache's files directory;
- ARM32_CC and, if required, ARM32_SYSROOT and ARM32_BINUTILS;
- GO, pointing to the supported Go toolchain.

Build outputs must be outside the repository:

~~~sh
export CRONOS_OUT=/absolute/path/private-output
OUT_DIR="$CRONOS_OUT/native" ./platforms/cronos/build_bionic.sh
OUT_DIR="$CRONOS_OUT/native" ./platforms/cronos/build_launcher.sh

revision=$(./common-tools/asp-installer/resolve-version.sh "$PWD")
mkdir -p "$CRONOS_OUT/release"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 GOTOOLCHAIN=local \
  "$GO" build -trimpath -ldflags="-s -w -X main.revision=$revision" \
  -o "$CRONOS_OUT/asp-installer" ./common-tools/asp-installer

GOTOOLCHAIN=local "$GO" run ./common-tools/asp-release/cmd \
  --platform cronos --version "$revision" \
  --contract platforms/cronos/release-contract.json \
  --installer "$CRONOS_OUT/asp-installer" --license LICENSE \
  --launcher "$CRONOS_OUT/native/asp1-launcher-arm32" \
  --child "$CRONOS_OUT/native/asp1-child" \
  --out-dir "$CRONOS_OUT/release"
~~~

The V4 release contains only the installer, launcher, child and license.
Original factory libraries and configuration are obtained through the shared
installer/extractor workflow. No factory binaries or calibration data belong in
a public release.

See [installation](../../docs/ASP-INSTALL.md) and
[desktop extraction](../../docs/ASP-EXTRACTOR.md) for the common workflow.
Use a controlled hardware-test procedure for this experimental platform.

## Source checks

~~~sh
make -C platforms/cronos test
make -C platforms/cronos sanitize
~~~

Host tests use an explicitly separate factory mock. The decoder conformance
tests reuse the unchanged generic vectors in platforms/rook/testdata.
A successful host test or compilation does not replace genuine factory-runtime
execution.
