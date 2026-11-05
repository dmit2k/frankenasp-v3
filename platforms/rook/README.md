# Rook — Native Factory ASP V3

Platform: **Echo Spot Gen 1 (rook)**, original Fire OS **5.5.6.9**, build **690918120**, ARM32 / Bionic API22.

This is a standalone External ASP V3 implementation based on the Athena PCM-only bridge and the original Rook factory `libasp.so`. It replaces the earlier experimental GenericAFE frontend. It is not an acoustic-equivalence claim: native startup and live processing have been exercised on a physical Rook, but controlled acoustic qualification is still in progress.

## Processing

- Input: one 92-byte ASP1 descriptor and one 5760-byte PCM buffer per exchange, 320 frames, six **interleaved S24_3LE** channels at 16 kHz.
- Capture slots: MIC0–MIC3 followed by REF0–REF1, with offsets 0/3/6/9/12/15. All other geometries are rejected before READY.
- Accumulate unmodified raw frames into consecutive 128-frame (8 ms) factory quanta.
- Factory codec (private ABI of the pinned library) converts the original REF 4–5 into stereo PCM16; the registered native RefProvider writer receives 128 frames before the corresponding public ASR call.
- `asp_process` receives the **original six-channel packed PCM24**, without helper-side MIC calibration, channel permutation, FIR filtering, resampling or gain changes.
- The original library owns MIC calibration, AEC, beamforming and the REF 16→48 kHz resampler. The launcher only exposes the four authentic readonly `/proc/idme/miccal.*` paths.
- Public PipelineAsr returns 128 mono PCM16 samples per quantum. A continuous FIFO re-packetizes them as 320 PCM16 samples (640 bytes) per exchange, with one initial 64-sample zero pad. There is no recurring pad or tail packet.
- REF push and processing use the same monotonically increasing 8 ms timestamp. Factory output is returned with its original LSB.

The pinned private ABI is supported only for SHA-256 `3258e6555b460a7b6a13e470a7f68b8359ceadf66fb4930f331c89d3bd33fb59` of `/system/lib/libasp.so`. Other libraries are rejected before READY; requalification is required for different firmware.

## Runtime and trust boundary

The independent Linux ARM32 launcher retains Athena's authenticated FD3/FD4 deployment boundary, fixed private root, readonly stock `/system` and matched `/vendor`, IDME aliases, and no-fork execution. Rook uses `/system/bin/linker`, `/system/lib/libasp.so`, `/vendor/etc/audio-algorithms/AFE.cfg` and the original coefficient files. On kernel 4.9 it uses the checked `getrlimit`/`fcntl` CLOEXEC fallback rather than assuming `close_range`.

TECHO5 owns ASP selection, supervision, SoftwareCut and fallback; the child never re-mutes an in-flight frame or mixes factory and built-in outputs. Factory failures are terminal to the child.

## Building and tests

From this directory:

```sh
make test
make asan
make ubsan
```

For target binaries, provide a matching owner-selected stock Rook runtime and an ARM32 toolchain. Both outputs must be outside the source checkout:

```sh
OUT_DIR=/private/rook-child \
BIONIC_CC=/path/to/arm-linux-gnueabihf-gcc \
BIONIC_LD=/path/to/arm-linux-gnueabihf-ld \
BIONIC_ROOT=/path/to/rook-factory-runtime ./build_bionic.sh

OUT_DIR=/private/rook-launcher \
ARM32_CC=/path/to/arm-linux-gnueabihf-gcc \
ARM32_SYSROOT=/path/to/arm32-glibc-sysroot \
ARM32_BINUTILS=/path/to/binutils-wrapper-dir ./build_launcher.sh
```

The Bionic child requires the original `/system/bin/linker` and factory dependency closure. Target build success alone does not prove that factory DSP is usable; verify against the original factory runtime in an isolated ARM32 full-system VM.

## Qualification status

Laboratory evaluation uses the original pinned library and matched factory runtime in an isolated VM. Physical Rook qualification remains separate: real individual IDME use, MIC/REF mapping, playback timing, acoustic AEC/double-talk, latency/CPU/RSS, MWW/STT and long-term stability. Physical device startup and live-PCM stability have been checked; complete acoustic equivalence has not been established.
