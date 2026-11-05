# Echo Spot (1st generation) — Rook

FrankenASP V3 implementation for **Echo Spot (1st generation)** (`rook`).

## Supported firmware

| Fire OS | Amazon build |
| --- | --- |
| **5.5.6.9** | **690918120** |

Only this factory firmware version is supported. Other versions have not been verified.

## Implementation status

The Rook implementation is included in **FrankenASP 3.4**. It provides the platform-specific components needed to use External ASP V3 with TECHO5. Compatible factory firmware must be supplied separately by the device owner; it is not distributed with FrankenASP.

## Testing status

- Host tests, ASAN and UBSAN: **passed**.
- Factory-runtime validation: **passed**.
- Physical Echo Spot: startup and live audio processing **verified**.
- Controlled acoustic evaluation: **pending**. Full equivalence to the original factory processing has not been established.

## Building and tests

Run from `platforms/rook/`:

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
