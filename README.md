# FrankenASP V3

External factory audio DSP helper for TECHO5, built per device platform.

## Why this project?

The Echo Show 8 (2nd generation) and Echo Spot (1st generation) are both appealing local voice satellites, with four microphones and capable audio hardware. But wake-word detection with TECHO5's open-source audio frontend can be disappointing, especially when an Echo is playing music. On the Show 8, stereo music playback makes the problem considerably more apparent.

While researching Athena's original audio architecture, I designed and implemented **FrankenASP** specifically to compare TECHO5's open-source audio processing against Amazon's original factory Audio Signal Processor (ASP). FrankenASP runs the factory ASP in an external helper and feeds its processed audio into TECHO5's existing voice pipeline. It now enables controlled comparisons on either supported Echo model: **the same physical device, microphone array, microWakeWord model, room, speech recordings, playback conditions, and automated scoring — but different audio processing before the detector.**

The difference was striking. [Read about the testing process and results](docs/athena-acoustic-comparison-20261025.md).

## Platforms

- `platforms/athena/` — Echo Show 8 (2nd generation): ARM32 Linux launcher and ARM32/Bionic factory ASP helper, build scripts, offline tests and fixtures. **Tested on a physical Echo Show 8 (2nd gen).**
- `platforms/rook/` — Echo Spot (1st generation, 2017): ARM32 Linux launcher and ARM32/Bionic factory ASP helper, native tests and offline build scripts. **Tested on a physical Echo Spot (1st gen).**

Both implementations have been deployed and exercised with TECHO5 and the original factory DSP on real devices; validation is not limited to emulation or offline tests. These are the only Echo models currently available to the maintainer. Other models have **not** been adapted or tested because the maintainer does not have access to their hardware; support for other models must not be assumed. Adding a model requires device-specific adaptation and on-device verification. Have an unused Echo collecting dust? You're always welcome to donate it to the maintainer for testing and experiments. :)

## Acoustic comparison

[Echo Show 8 Gen 2 (Athena): controlled wake-word performance comparison](docs/athena-acoustic-comparison-20261025.md) — measured results for native TECHO5 versus Amazon factory ASP processing, including mono/stereo music playback, quiet and normal speech, spatial interference, and the test-room diagram.

## Build and test

```sh
cd platforms/athena
make test
make asan
make ubsan
```

Production ARM32 launcher and Bionic child are built separately using `platforms/athena/build_launcher.sh` and `platforms/athena/build_bionic.sh`, with externally supplied cross-compilers and owner-provided Bionic link inputs. Each script requires `OUT_DIR` outside the platform source directory and builds without invoking or installing the binaries on a device.

**Vendor library files, firmware, calibration data and private runtime images are not distributed with this repository.**

## Built on TECHO5 — with genuine gratitude

First and foremost, I want to recognize **[HuskerMinion](https://github.com/HuskerMinion) and the [TECHO5 contributors](https://github.com/HuskerMinion/techo5)**.

Making Linux work on ageing, tightly integrated Echo hardware is already impressive. Making displays, sound, hardware interfaces, Home Assistant integration, music playback, and daily usability work *together* across multiple generations is a remarkable engineering accomplishment. Just as impressive is the pace and persistence with which HuskerMinion continues to improve TECHO5.

For owners of Echo devices that might otherwise become obsolete or e-waste, **TECHO5 gives that hardware a useful second life**: local control, integration with our own home automation, and a reason to keep good equipment in service instead of discarding it.

A special thank-you also goes to **[@nuno407](https://github.com/nuno407)** for the [Athena kernel work exposing the two-channel `DL1_AWB_Record` hardware echo reference](https://github.com/HuskerMinion/techo5/commit/c94f013b8d6d30e49f3262649e0c243363e5b72c) (PR #214). By adding the missing DL1-to-UL2 routes, this contribution made the 96 kHz stereo playback reference available alongside Athena's four-microphone capture. Access to the real microphone and playback-reference signals is a crucial foundation for meaningful comparisons against the original factory processing. This is an improvement to TECHO5's own open hardware support, independently of FrankenASP; it does not imply that the two DSP implementations are equivalent.

**None of this project would exist without that work.** FrankenASP is not competing firmware, claims none of the credit for the underlying platform, and does not ask the TECHO5 maintainers to support an experiment involving proprietary Amazon components. My aim is narrower: to compare the open-source and factory audio-processing paths on supported Echo hardware and share useful findings. Whether any general-purpose improvements should be incorporated upstream is entirely the TECHO5 maintainers' decision.

**Full credit for the foundation belongs to TECHO5.** The scope and risks of FrankenASP belong to this project.

## How the projects fit together

| Codebase | Responsibility |
| --- | --- |
| **[FrankenASP V3](https://github.com/dmit2k/frankenasp-v3)** (this repository) | Device-specific ARM32 Linux launchers and ARM32/Bionic helpers for Athena and Rook; isolated, owner-provided factory runtime integration; microphone/reference adaptation, factory DSP processing, and mono PCM output; build scripts and tests. No proprietary Amazon binaries are distributed. |
| **[TECHO5 fork — External ASP V3](https://github.com/dmit2k/techo5/tree/experimental/external-asp-v3-dev-main)** | The platform-independent, optional host-side integration: native microphone/playback-reference capture, PCM-only helper interface, secure session lifecycle, selection and fallback between built-in and external processing, and delivery of processed PCM to the existing wake-word and voice pipeline. Factory-specific DSP knowledge stays out of TECHO5. |
| **[Upstream TECHO5](https://github.com/HuskerMinion/techo5)** | The underlying Linux-based Echo platform: device and hardware support, audio playback, UI, Home Assistant integration, and local voice-satellite functionality. |

The External ASP integration currently lives in the linked **experimental branch of my TECHO5 fork**, not in upstream TECHO5. TECHO5 owns the generic PCM capture and voice-pipeline integration; FrankenASP owns everything needed to run each device's factory DSP. The built-in TECHO5 processing remains available as the default and fallback. This separation keeps the host-side interface independent of Amazon's libraries and allows other audio-processing helpers to use the same interface without carrying factory-specific code into TECHO5.

## License

MIT (`LICENSE`). This implementation incorporates MIT-licensed concepts and source derived from earlier TECHO5 contributors; their copyright notice is preserved in `LICENSE`.
