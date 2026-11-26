# Echo Show 8 (1st generation) — Crown

FrankenASP V3 implementation for **Echo Show 8 (1st generation)** (`crown`, model C7H6N3, MT8163).

## Supported firmware

| Fire OS | Amazon build | `libasp.so` SHA-256 |
| --- | --- | --- |
| **6.5.7.3** (NS6573/7595) | **0013054618500** | `37c9248416873022ad210c50638525edb8f0a77ee942ced30f957bd74b881d18` |

Only this factory firmware version has been verified. The factory files come from the owner's own backup (for example a TWRP backup of the original `system` and `vendor` partitions taken before installing TECHO5). They are not distributed with FrankenASP.

## How it works

Crown uses the ASP V2 public API of `libasp.so`, without binder, the DSP or IPC:

- `asp_parameterized_init({0, 0, "/vendor/etc/audio-algorithms/asp.cfg"})`.
- A Speaker pipeline (`asp_create_pipeline(3, pcm16, 2, 48000, pcm16, 2, 48000)` and `asp_set_device(2)`), as the Fire OS audio HAL creates for playback. It fills the factory echo-reference buffer.
- An ASR pipeline (`asp_create_pipeline(0, pcm24 packed, 6, 16000, pcm16, 1, 16000)`) fed with TECHO5's native capture: 6 × S24_3LE at 16 kHz, microphones on channels 0–3 and the playback loopback on channels 4–5.
- `asp_set_ext_ref_sync_status(asr, 2)`.

The factory ASR aligns its echo reference by searching for an exact match between the loopback channels and the 48 kHz reference decimated by 3. The helper therefore:

- upsamples channels 4–5 by 3 (linear, with every third sample equal to the original) and feeds them to the Speaker pipeline;
- delays the capture going to the ASR by 2 quanta (16 ms, `CROWN_ASP_LEAD`), so that the reference always leads; with no lead the ASR logs "Out of sync" and runs without echo cancellation;
- requires `"Passthrough": true` for the Speaker pipeline in `asp.cfg`, otherwise the speaker EQ/limiter changes the reference and no match is found. `tools/make_bundle.sh` patches the bundled copy.

TECHO5 hands the External ASP output to the voice pipeline after its own leveler, so the factory output reached STT about 20–30 dB quieter than the built-in path. The helper applies a fixed +28 dB gain with a peak limiter (`CROWN_ASP_GAIN_DB` overrides it in offline tests).

## Testing status

- Offline (qemu-arm with the extracted factory runtime): with the reference synchronized, the AEC_V2 stage runs; white-noise echo drops 34 dB, versus 27 dB with no reference.
- Simulated wake-word test (14 real "Gina" recordings over music, echo path measured on the device, microWakeWord model): factory ASP 14/14 down to −25 dB speech-to-echo; WebRTC AEC3 and the raw microphone 0–4/14 from −10 dB down.
- Physical Echo Show 8 (1st gen), TECHO5 v1.6.54, music at volume 0.43:
  - built-in processing: 0/6 wake words;
  - External ASP with the stock hardware loopback: 6/6, scores 0.60–1.00;
  - External ASP with a software playback reference: 6/6, scores 0.88–1.00.
- CPU: about 38% of one core at 1.3 GHz.

The hardware loopback on Crown is the 48 kHz playback with every third sample kept and no filtering, so content above 8 kHz aliases into the reference. It works with the factory ASP, but a software reference gives somewhat higher scores.

## Building

Run from `platforms/crown/`, with output directories outside the source checkout:

```sh
OUT_DIR=/path/to/child-build BIONIC_ROOT=/path/to/factory-runtime ./build_bionic.sh
OUT_DIR=/path/to/launcher-build ./build_launcher.sh
```

`build_bionic.sh` defaults to `arm-linux-gnueabihf-gcc`/`ld` and links against the owner-supplied `libc.so` and `libdl.so`. `BIONIC_ROOT` is the extracted factory runtime with `system/` and `vendor/`.

To build the `external-asp` tree for `/data/techo5-linux/external-asp`:

```sh
FW=/path/to/factory-runtime CHILD=/path/to/child-build/asp1-child \
LAUNCHER=/path/to/launcher-build/asp1-launcher-arm32 \
LIBS=/path/to/used-libs.txt OUT=/path/to/external-asp-crown.tgz tools/make_bundle.sh
```

`LIBS` lists the `/system/lib` and `/vendor/lib` files the factory ASP opens; it can be collected with `QEMU_STRACE=1` while running the child under qemu-arm. `tools/logshim.c` is an optional `LD_PRELOAD` shim that prints the factory Android log to stderr for debugging.
