# FrankenASP V3 — native on-device OTA installer (development)

This is a device-side, standalone Go installer for **owner-selected** Echo firmware. The release archive contains only FrankenASP code and independently built platform-specific binaries, plus the owner's approved Athena channel map. No proprietary Amazon linker, ASP library, firmware or calibration material is distributed.

## Supported source formats

- Athena: full Android **v4** block OTA with `system.transfer.list`, `vendor.transfer.list`, and `*.new.dat.br`. The Bionic linker and system dependencies are taken from the OTA; vendor libraries and configuration come from the **same** OTA.
- Rook: full Android **v3** block OTA with `system.transfer.list` and `system.new.dat`. Original `libasp.so` is under `/system/lib`, and vendor files are in the **same system image** under `/vendor`.
- A full OTA ZIP containing regular `system/...` and `vendor/...` entries is also supported where the required files exist.
- Incremental OTA transfers, patch/move/stash commands, `payload.bin`, sparse/EROFS images and unknown layouts are deliberately rejected. The installer does not execute OTA updater scripts.

The block extractor uses a small bounded in-memory cache of selected 4-KiB blocks. It reads ext4 metadata and requested file extents without materializing complete system/vendor partition images. Brotli decoding is built into the Go executable (no Echo Python, Brotli CLI, mount, loop device, `debugfs`, or shell pipeline).

Dependencies are resolved transitively from the relevant ELF32 ARM/Bionic objects using the factory loader's directory priority. A bounded closure is enforced (256 ELF nodes, 128 MiB of runtime bytes; a maximum of 64 complete selective passes, 96 MiB retained blocks per partition, 64 MiB per source file). These syntactic checks do **not** prove factory ABI compatibility, acoustic performance or security of the selected firmware.

## Build a platform-specific release on a development Linux PC

Build the appropriate V3 native Linux launcher and ARM32 Bionic child using that platform's existing verified build scripts and externally supplied toolchains. The built helper is a FrankenASP binary, **not** the original factory `libasp.so`.

```sh
cd common-tools/lib-extractor
GO=/path/to/go1.26.9/bin/go ./build-release.sh \
  athena /path/to/asp1-launcher-arm32 /path/to/asp1-child \
  /private/new-athena-release /path/to/owner-approved-channel-map

GO=/path/to/go1.26.9/bin/go ./build-release.sh \
  rook /path/to/asp1-launcher-arm32 /path/to/asp1-child \
  /private/new-rook-release
```

The output archive is approximately 3 MiB and includes:

```text
frankenasp-athena/
  LICENSE
  asp-installer                  # standalone static Linux ARM32 Go binary
  assets/
    manifest.json
    external-asp                 # platform-specific native ARM32 launcher
    asp1-child                   # platform-specific ARM32/Bionic helper
    channel-map                  # Athena only, owner-approved
```

No Python tool is required for end users on Windows, macOS or Linux: a released archive is simply copied to the Echo. Development builds still require the appropriate cross-compilers.

## On-device workflow

Copy the appropriate **already-built** release TAR to your Echo, for example using `scp`. Work as root on the device. The example below does not assume any fixed SSH IP or firmware download URL.

```sh
cd /data/techo5-linux
tar -xzf frankenasp-v3-athena-arm32.tar.gz
cd frankenasp-athena

./asp-installer version
./asp-installer inspect --ota /data/techo5-linux/update.bin
./asp-installer install --ota /data/techo5-linux/update.bin
./asp-installer status
```

Alternatively download the OTA using the installer itself:

```sh
./asp-installer inspect --ota-url 'https://owner-selected-host/path/update.bin'
./asp-installer install --ota-url 'https://owner-selected-host/path/update.bin'
```

The release manifest selects Athena or Rook automatically. An explicit `--platform` is optional and rejects mismatches. Use the Rook archive for Spot. `--assets` defaults to `./assets`. Local `--ota` never downloads. When `--ota-url` is used, the OTA is written once to a private temporary file (maximum 2 GiB) and removed after the command completes. No full partition images or intermediate TAR payloads are written. The owner's local `--ota` file is never removed.

**Trust policy:** consistent with this project's deliberately owner-controlled firmware policy, there are no mandatory SHA256 hashes, manufacturer signature checks, certificate chain validation or hostname validation. HTTPS verification is intentionally disabled for owner-selected OTA URLs. This makes network interception possible. The user explicitly accepts source authenticity risk, while malformed file formats, unsafe paths, ELF incompatibility and incomplete transfers are still checked.

## Install contract and limitations

The installed tree has the **exact** V3 layout:

```text
/data/techo5-linux/external-asp/
  bin/external-asp                 # root 0500
  etc/launcher.profile            # exact ASP1-LAUNCHER-V1 contents
  etc/installer-receipt.json      # local receipt, no vendor hashes
  lib/rootfs/
    factory/asp1-child            # root 0500
    factory/channel-map           # Athena only
    system/bin/linker             # extracted factory linker, root 0500
    system/lib/...                # selected factory ELF closure
    vendor/lib/...                # Athena when required
    vendor/etc/audio-algorithms/  # original configs from selected OTA
    proc/ sys/ .realproc/ dev/ data/
```

Directories are root-owned, mode 0700. Regular data files are mode 0600. Required rootfs /proc symlinks refer to real device calibration sources and are never populated with copied calibration values.

**The initial implementation is install-only.** It refuses to replace an existing `external-asp` directory. A private sibling stage is populated, files/directories are fsynced, then a kernel `RENAME_NOREPLACE` publishes it atomically. Existing sessions, TECHO5 settings, selector, boot persistence, partitions and stock processes are untouched. Device upgrades, generation cleanup, rollback, actual on-Echo extraction performance and verified active-helper shutdown require separate implementation and hardware qualification. Do not mistake the presence of a valid source runtime for a guarantee that it will execute on an arbitrary firmware revision.

## Tests

```sh
cd common-tools/lib-extractor
go test -count=1 ./...
go test -race -count=1 ./...
```

The Go unit tests cover transfer validation, rejecting base-dependent ranges, selective block mapping and path validation. End-to-end source reads must be tested separately against **real locally supplied** Athena and Rook OTA files. Manufacturer libraries and full firmware files must never enter this repository or its release assets.
