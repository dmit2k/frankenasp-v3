# FrankenASP V3 — Stage04 PCM-only factory helper

This isolated C component targets accepted Stage03-R Go
`b0c0d5fa96576eff8f7497d48e39fa30a5348e76` and accepted C codec
`77149e6494ac9784d98464ff984c84887a28ced9`. Stage04 is not a paired
Go/child launch integration or Echo runtime acceptance.

**Wire:** fixed little-endian `ASP1` descriptor (16+28×buffers+8×channels),
readiness `0xA5` only after successful vendor initialization, one immutable
20-ms input block per exchange, one fixed 640-byte mono S16LE response.
No frame envelopes, status, IDME, Q30 or raw calibration in descriptor/PCM.
Supported vendor geometry is exactly two physical buffers: MIC4 S32LE
24 valid bits (shift 0/8) and independent REF stereo S16LE, both 96000 Hz,
1920 frames per 20 ms. Valid other geometries fail before READY. The adapter
calls genuine vendor ABI for every 768 input frames and collects 128 mono
samples per quantum, with 64 one-time *output* FIFO zeros.

**Native tests:** `make test`, `make asan`, `make ubsan`.
`build/runtime_mock` links the same descriptor/IO/runtime/child/ABI adapter
source units with `tests/mock_vendor.c` *only in the test target*. The mock
requires `ASP1_TEST_MAP` and `ASP1_TEST_RESOURCE` in offline test sessions.
It does not load vendor libraries and cannot be used as production.

**Production builds:** outside Git:
`OUT_DIR=/private/evidence/bionic BIONIC_CC=/path/to/arm32-gcc
BIONIC_LD=/path/to/arm32-ld BIONIC_ROOT=/path/to/verified-root
bash build_bionic.sh`;
and separately
`OUT_DIR=/private/evidence/launcher ARM32_CC=/path/to/arm32-gcc
ARM32_BINUTILS=/path/to/arm32-binutils ARM32_SYSROOT=/path/to/arm32-linux-sysroot
bash build_launcher.sh`. Neither script installs or executes a binary.
The child is an ARM32 Bionic PIE with `/system/bin/linker`, linked to
owner-private original `libc.so`/`libdl.so`; vendor `libasp.so` resolved
at runtime. The launcher is static Linux ARM32. Runtime private bytes are
never stored in this repository.

**For Stage05:** host pins the native launcher executable in FD3 and trusted
private deployment directory in FD4, passes stdin/stdout PCM pipes, no CLI
parameters, owns the single child process through its lifetime. Launcher
verifies FD identity, owner/modes and assets and prepares RO factory aliases
inside private mount namespace before same-process `execveat`. Missing
necessary backend resources => stderr + exit before READY; host retains
Built-in. No Echo, mount, chroot, installer or user-device action is authorized
by this build. See `LAUNCH_PROVENANCE.md`.
