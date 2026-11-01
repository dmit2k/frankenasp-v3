# FrankenASP V3 — on-device asp-installer (experimental)

One ARM32 Linux executable on Echo. Selectively extract original factory libraries and dependencies directly from a user-selected OTA, build an isolated runtime, and install or update FrankenASP V3. There is no Python preparer, full partition image reconstruction, desktop-specific package, or intermediary TAR payload.

The public archive contains only the FrankenASP installer, native launcher, ARM32/Bionic helper, platform manifest, optional Athena channel map and license. Amazon `libasp.so`, Bionic `linker`, original configuration files, firmware and calibration are **never included** in public archives.

## Supported platforms and OTA formats

- Athena / Echo Show 8 Gen 2: full Android block OTA v4, `system.new.dat.br` and `vendor.new.dat.br`, with their transfer lists; `libasp.so` is under vendor/lib.
- Rook / Echo Spot Gen 1: full Android block OTA v3, `system.new.dat` with its transfer list; `libasp.so` is under system/lib and vendor is embedded inside system.
- Full file-tree ZIPs are supported if they contain the required runtime components.
- Incremental block updates, patch/move/stash commands, payload.bin and unknown filesystem layouts are rejected.

The extractor uses selective ext4 reads, at most 96 MiB retained blocks per partition, at most 64 streaming passes, and a bounded ELF dependency closure. No partition images are written. Factory DSP compatibility and acoustic results still require testing on actual hardware.

**User-controlled source policy:** OTA signatures, full-image SHA and TLS certificate/hostname checks are not required. Self-signed/untrusted HTTPS sources are accepted by design, so network tampering is possible. URL downloads are limited to 2 GiB and use a private temporary file; local --ota paths are never removed.

## Build ARM32 release archives

Use the device's own scripts to build the appropriate launcher and ARM32/Bionic helper. The Athena channel map is selected and approved by the owner.

```sh
cd common-tools/asp-installer
GO=/path/to/go1.26.9/bin/go ./build-release.sh \
  athena /private/asp1-launcher-arm32 /private/asp1-child \
  /private/new-athena-release /private/approved-channel-map

GO=/path/to/go1.26.9/bin/go ./build-release.sh \
  rook /private/asp1-launcher-arm32 /private/asp1-child \
  /private/new-rook-release
```

Output: `frankenasp-v3-athena-arm32.tar.gz` and `frankenasp-v3-rook-arm32.tar.gz`. Both contain a standalone ARM32 `asp-installer` and `assets/` with the appropriate binaries and manifest. No firmware components are in the release.

## Basic Echo commands

Transfer the matching archive to `/data/techo5-linux` and connect as root:

```sh
cd /data/techo5-linux
tar -xzf frankenasp-v3-athena-arm32.tar.gz
cd frankenasp-athena

./asp-installer version
./asp-installer inspect --ota /data/techo5-linux/update.bin
./asp-installer check --ota /data/techo5-linux/update.bin
./asp-installer check-vendor --ota /data/techo5-linux/update.bin
```

Instead of `--ota FILE`, use `--ota-url 'https://user-selected-host/path/update.bin'` for a direct device download. The release manifest selects the platform automatically. All long operations print phase messages on stderr and an activity spinner when attached to a terminal; JSON results remain on stdout.

## Install and update

**First disable External ASP manually in TECHO5 settings.** The installer cannot change the TECHO5 selector and cannot guarantee that an enabled helper will not respawn.

```sh
./asp-installer install --ota /data/techo5-linux/update.bin --yes
./asp-installer update --ota /data/techo5-linux/update.bin --yes
./asp-installer status
```

Install requires an empty destination. Update recognizes an existing V3 rootfs (including an older manual installation), compares the current files with proposed ones, and normally creates a private `tar.gz` archive under `/data/techo5-linux/asp-installer-backups/` **before replacing the current deployment**. `--force-no-backup` disables only the archival backup. The old installed runtime remains as a generation after an update.

Installation builds a new private immutable tree, fsyncs its content, and publishes it through kernel `renameat2` without overwriting unknown directories. Update atomically exchanges new and existing trees with `RENAME_EXCHANGE` and preserves the old tree under `.external-asp-generation-v3-...`. Interruption between the exchange and generation naming preserves the old runtime in the staged directory for owner recovery.

The installer scans process executable identities, open file descriptors, memory mappings and mount references. It sends SIGTERM only to positively verified FrankenASP launcher/helper processes; unrelated processes block modification. If TECHO5 itself retains the tree, optional `--restart-techo5` permits a controlled restart of exactly one verified PID1-supervised Linux TECHO5 daemon. Unknown supervisor, executable identity or runtime activity causes refusal instead of a broad process-name-based restart. Do not enable External ASP during maintenance.

## Check and check-vendor

```sh
./asp-installer check --ota /data/techo5-linux/update.bin
./asp-installer check-vendor --ota /data/techo5-linux/update.bin
```

`check` compares extracted OTA files, current release launcher/helper, Athena channel map and launcher profile to the installed runtime. It reports missing, different and extra installed runtime files; generated receipt timestamps are excluded. For a live-vendor deployment, it additionally compares the actual vendor files.

`check-vendor` is **Athena-only** and compares precisely the vendor libraries and configuration files used by the proposed dependency closure against the device's existing `/vendor`. A match requires identical contents and a read-only mounted vendor filesystem. Matching files may be used through the V3 launcher's live-vendor mode; otherwise a matched bundled rootfs must be used. Rook always bundles its vendor files.

`--vendor-mode auto` (default) uses live vendor on Athena only on a successful match and a read-only mount, otherwise bundles vendor from the selected OTA. `--vendor-mode bundled` forces full private vendor copies; `--vendor-mode live` refuses if incompatible.

## Disk-space preflight

All mutation commands check the filesystem free-space budget *before* writing a new runtime or archival backup. A fixed 64 MiB reserve protects the normal device filesystem from being filled by installer operations. Before an OTA URL download for install/update, the installer reserves a conservative 168 MiB capacity for a future private runtime plus the estimated uncompressed upper bound of the backup, if enabled. After the OTA has been selectively extracted into memory, it recalculates the stage budget from the actual dependency closure, bundled assets, per-file filesystem allocation, directory metadata and backup estimate.

- If the OTA URL supplies an HTTP `Content-Length`, the exact transfer size plus reserved future runtime, backup and safety margin must fit **before downloading the body**.
- Without a `Content-Length`, available space is checked while downloading; the OTA download is aborted before it consumes space reserved for installation.
- The OTA temporary directory and the target rootfs are checked separately if located on **different filesystems**, using filesystem device identifiers. A local `--ota` already occupies its disk space and is not counted twice.
- `--force-no-backup` excludes the optional TAR archive but **not** the new staged rootfs, the old retained generation, or the 64 MiB reserve. `rollback` separately reserves space for its mandatory backup.
- Each budget prints the free/required disk space and the OTA, runtime, backup and safety-margin components to stderr. The installer rejects an insufficient budget before modifying the installed tree.

This is an advance capacity estimate, not disk-space allocation: another process writing to the same filesystem can still cause `ENOSPC` between checks. Normal filesystem errors remain fatal and do not authorize overwriting a known generation. The old installation is retained through the update transaction.

## Rollback and uninstall

```sh
./asp-installer status
./asp-installer rollback --generation '.external-asp-generation-v3-YYYYMMDDTHHMMSSZ-RANDOM' --yes
./asp-installer uninstall --yes
```

Rollback verifies the saved generation and all active references, archives the current installation, atomically exchanges the selected generation with current, and retains the replaced generation.

Uninstall stops only recognized FrankenASP processes, checks for other runtime references, then detaches and removes the installed external-asp directory and recognized V3 generations. It fails closed on unknown files, foreign ownership or unexpected process references. By default, it keeps previously archived TAR backups. `--purge-backups` additionally removes only recognized installer backups. After uninstall **disable External ASP yourself in TECHO5 settings**; TECHO5 configuration is not modified by this utility.

## Production layout and qualification

The destination remains `/data/techo5-linux/external-asp/{bin,etc,lib}`. The launcher expects an exact `ASP1-LAUNCHER-V1` profile; the helper is `lib/rootfs/factory/asp1-child`, and factory `/system/bin/linker` is extracted locally. All installer-owned directories are root-only 0700; executable files 0500; runtime data 0600. Genuine device calibration is exposed through validated /proc symlink aliases, not copied or recorded.

```sh
cd common-tools/asp-installer
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```

Selective extraction was tested against real Athena and Rook OTA images in an isolated Linux test environment. Unit tests cover transfer-range failures, path safety, download truncation, ELF dependency closure, vendor comparisons and installed file comparisons.

**Experimental:** the new privileged install/update/rollback/uninstall logic has not yet undergone full on-device hardware qualification. Running helpers, private mount namespaces, namespace-descriptor keepers, filesystem crash recovery and daemon supervision need physical Echo acceptance before a production release. No code here updates the TECHO5 binary, firmware, boot slot or persistence configuration.
