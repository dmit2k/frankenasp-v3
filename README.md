# FrankenASP V3

External factory audio DSP helper for TECHO5, built per device platform.

## Platforms

- `platforms/athena/` — Echo Show 8 (2nd generation): ARM32 launcher and factory ASP helper source, build scripts, offline tests and fixtures.
- `platforms/rook/` — Echo Spot (1st generation): pinned ARM32/Bionic factory helper, secure launcher, native tests and offline build scripts. Hardware acceptance is pending.

Both implementations remain platform-specific. The rook build reuses the existing PCM-only descriptor and I/O source without changing athena or introducing a shared factory backend.

## Build and test

```sh
cd platforms/athena
make test
make asan
make ubsan
```

Production ARM32 launcher and Bionic child are built separately using `platforms/athena/build_launcher.sh` and `platforms/athena/build_bionic.sh`, with externally supplied cross-compilers and owner-provided Bionic link inputs. Each script requires `OUT_DIR` outside the platform source directory and builds without invoking or installing the binaries on a device.

`common-tools/lib-extractor/` is reserved for future extraction tools; the rook implementation uses an independently verified owner-extracted matched runtime. `docs/` is intentionally empty. Development-stage specifications, reports and handoffs are not part of this repository layout.

**Vendor library files, firmware, calibration data and private runtime images are not distributed with this repository.**

## License

MIT (`LICENSE`). This implementation incorporates MIT-licensed concepts and source derived from earlier TECHO5 contributors; their copyright notice is preserved in `LICENSE`.
