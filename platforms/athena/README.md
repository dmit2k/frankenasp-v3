# Echo Show 8 (2nd generation) — Athena

FrankenASP V3 implementation for **Echo Show 8 (2nd generation)** (`athena`).

## Supported firmware

| Fire OS | Amazon build |
| --- | --- |
| **7574.1** | **PS75741/7080** |
| **7574.1** | **PS75741/7081** |
| **7574.1** | **PS75741/7082** |

These factory firmware versions have been verified for this implementation. Other versions have not been verified.

## Implementation status

The Athena implementation is included in **FrankenASP 3.4**. It provides the platform-specific components needed to use External ASP V3 with TECHO5. Compatible factory firmware must be supplied separately by the device owner; it is not distributed with FrankenASP.

## Testing status

- Host tests, ASAN and UBSAN: **passed**.
- Physical Echo Show 8: startup and live audio processing **verified** with **PS75741/7080**.
- Controlled acoustic comparison: **completed** on the tested device; see the [acoustic comparison report](../../docs/athena-acoustic-comparison-20261025.md).
- Builds **PS75741/7081** and **PS75741/7082**: verified from factory firmware files but not separately tested on physical devices.

## Building and tests

Run from `platforms/athena/`:

```sh
make test
make asan
make ubsan
```

For ARM32 target builds, provide the appropriate toolchains and an owner-supplied compatible factory runtime. Keep both output directories outside the source checkout:

```sh
OUT_DIR=/path/to/child-build \
BIONIC_CC=/path/to/arm32-bionic-compiler \
BIONIC_LD=/path/to/arm32-linker \
BIONIC_ROOT=/path/to/factory-runtime ./build_bionic.sh

OUT_DIR=/path/to/launcher-build \
ARM32_CC=/path/to/arm32-linux-compiler \
ARM32_SYSROOT=/path/to/arm32-linux-sysroot \
ARM32_BINUTILS=/path/to/arm32-binutils ./build_launcher.sh
```

Build success alone does not establish on-device or acoustic compatibility.

For deployment, see the [installation guide](../../docs/ASP-INSTALL.md).
