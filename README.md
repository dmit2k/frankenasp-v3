# FrankenASP V3

External factory audio DSP helper for TECHO5, built per device platform.

## Platforms

- `platforms/athena/` — Echo Show 8 (2nd generation): ARM32 Linux launcher and ARM32/Bionic factory ASP helper, build scripts, offline tests and fixtures. **Tested on a physical Echo Show 8 (2nd gen).**
- `platforms/rook/` — Echo Spot (1st generation, 2017): ARM32 Linux launcher and ARM32/Bionic factory ASP helper, native tests and offline build scripts. **Tested on a physical Echo Spot (1st gen).**

Both implementations have been deployed and exercised with TECHO5 and the original factory DSP on real devices; validation is not limited to emulation or offline tests. These are the only Echo models currently available to the maintainer. Other models have **not** been adapted or tested because the maintainer does not have access to their hardware; support for other models must not be assumed. Adding a model requires device-specific adaptation and on-device verification.

## Build and test

```sh
cd platforms/athena
make test
make asan
make ubsan
```

Production ARM32 launcher and Bionic child are built separately using `platforms/athena/build_launcher.sh` and `platforms/athena/build_bionic.sh`, with externally supplied cross-compilers and owner-provided Bionic link inputs. Each script requires `OUT_DIR` outside the platform source directory and builds without invoking or installing the binaries on a device.

**Vendor library files, firmware, calibration data and private runtime images are not distributed with this repository.**

## License

MIT (`LICENSE`). This implementation incorporates MIT-licensed concepts and source derived from earlier TECHO5 contributors; their copyright notice is preserved in `LICENSE`.
