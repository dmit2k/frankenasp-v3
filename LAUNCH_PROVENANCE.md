# Stage04 — exact factory launch, ABI, and code provenance

## Authority and scope

Only historical reference `dmit2k/FrankenASP`, commit
`52d51de33e3105501b298c1c214db55f05a2eb82`. Historical remote:
`https://github.com/dmit2k/FrankenASP.git`. This reference was available from
a separate checkout of the referenced historical repository; it is not a V3
baseline and no old commit was merged/cherry-picked. Its MIT attribution is
preserved in `THIRD_PARTY_NOTICES.md`.

| Old path under `tools/external-asp/` | Exact Git blob SHA | New files and retained purpose |
|---|---|---|
| `launcher/launcher.c` | `74543f20b01208bcd312161e7f06e34082dcf4d8` | `launcher/launcher.c`: root-owned component/ELF/trust checks, FD3 self-identity, FD4 private assets, mount namespace/RO binds, sysfs/proc & authentic miccal alias checks, static ARM32 exec, child environment; profile replaced with owner-qualified fixed ASP1 values, channel-map check added, old child name removed |
| `factory_adapter.h` | `2a52de9b17a992b15f5afb6bfb0272c4dc3754ec` | `src/factory_adapter.h`: six-function ABI with timespec32 by value; no calibration gain input |
| `factory_adapter.c` | `891838997840f1d14e771d030c0e8170f6af1950` | `src/factory_adapter.c`: init/create/ref/process/destroy/deinit lifecycle, canary/size/error checks |
| `factory.c` | `7604d8752729c37ee76ae73f91464c0c1d98b293` | `src/vendor_factory.c`: fixed `/vendor/lib/libasp.so` and exact symbol resolution; no ABI mock in production |
| `factory_child.c` | `e32c8456f8c8822df34166e91adeb2887452bfa2` | `src/child.c`: only the wire FD preserved before stdout→stderr, terminal cleanup; all CFG2/legacy handshakes discarded |
| `core.c` | `08401ce3af71546a9470d4505b493e89fe5a6d73` | `src/runtime.c`: MIC signed24 low/high adaptation, 2 independent S16 REF slots, 768→128 DSP quantum; rewritten PCM-only admission/64-output-FIFO |
| `core.h` | `374d99264466c9b59f545553169472663994327b` | `src/runtime.h`: qualified factory geometry and FIFO state; no old profile/sequence/origin |
| `io.c` | `c47c75e44285881c71c5c263b991bf7730fda7d6` | `src/io.c`: full EINTR/short read/write; replaced old metadata parsing and secure open with trusted map-only reader |
| `bionic_entry.c` | `fa3fe28163caad6b5f8d379f26c6f682c9cf35dc` | `src/bionic_entry.c`: existing Android9 `__libc_init` startup seam |
| `start32.S` | `0f0ca4964caf022bf73d0e076036ecf19a4397f4` | `src/start32.S`: ARM32 ELF entry |
| `build_bionic.sh` | `98718efe8297a848b9e8b19c0af9deeb792a7ea8` | `build_bionic.sh`: freestanding ARM32 Bionic link recipe, rebuilt for PCM-only child |
| `BIONIC_FACTORY.md` | `644f648b87da3ce243613088691fa947446d7055` | crossbuild requirements only, no runtime bytes |
| `platform.h` | `3f6a762d932c926421c8f43f60e36b1f3d42853a` | `src/bionic_compat.h`: small ARM32 Bionic ABI declarations without glibc headers |

New `launcher/alias_policy.[ch]` is deliberately narrow and test-linked
into the *production launcher*. It supplies precisely four private factory
aliases, not a generic host/platform metadata API. The launcher verifies
exact symlink paths and root ownership, binds authenticated vendor assets,
mounts sysfs and proc only inside a private namespace, and verifies the
authentic miccal sysfs files by open/fstat (never read). Missing required
paths/configs terminate launcher before child exec and therefore before READY.

Owner-provided trusted runtime, not bundled here: private root-owned
`/data/techo5-linux/external-asp` exposed through FD4; root-owned, immutable
`lib/rootfs` tree with `/system/bin/linker`, `/factory/asp1-child`,
`/factory/channel-map`, `/proc` verified relative symlinks, directory
mountpoints; vendor `lib/libasp.so`, `etc/audio-algorithms/asp.cfg`,
`AFE.cfg`; four genuine device tree sources; selected owner-attested
`etc/launcher.profile`. Child environment is fixed, no CLI bypass,
no installer/OTA/discovery/supervisor, no profile values sent over PCM IPC.

The child expects the exact map grammar:
```
ASP1-CHANNEL-MAP-V1
mic=0,1,2,3
ref=0,1
```
Orders may be explicit permutations. It is captured-slot routing,
not a calibration file, gain multiplier, inferred physical association,
platform whitelist or host identity.
The launcher profile, as *owner-verified* fixed file, is one of:
```
ASP1-LAUNCHER-V1
vendor_mode=LIVE_VENDOR_MATCHED
source_pair=OWNER_VERIFIED
```
or `OWNER_BUNDLED_MATCHED` instead of `LIVE_VENDOR_MATCHED`.
It describes an already-prepared install, never obtains or installs assets.
For live mode the existing host vendor filesystem must be read-only.

## Limit of proof

Native alias tests and static launch security review run on an offline validation environment without
privilege. ARM32 crossbuild checks ELF/ABI but does **not** execute launcher,
mount, chroot, Bionic child, vendor Calibrator, or Echo hardware. Only a real
owner-authorized device qualification can confirm firmware-specific paths,
permissions, actual vendor init, calibrator reading values and acoustic DSP.
The native mock does not assert hardware success; its calibration test resource
is merely an independent init precondition and never contributes gain.
