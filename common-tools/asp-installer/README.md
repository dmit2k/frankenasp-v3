# FrankenASP V3 — on-device asp-installer (experimental)

One ARM32 Linux executable on Echo. Selectively extract original factory libraries and dependencies directly from a user-selected OTA, build an isolated runtime, and install or update FrankenASP V3. There is no Python preparer, full partition image reconstruction, desktop-specific package, or intermediary TAR payload.

The public archive contains only the FrankenASP installer, native launcher, ARM32/Bionic helper, platform manifest, optional Athena channel map and license. Amazon `libasp.so`, Bionic `linker`, original configuration files, firmware and calibration are **never included** in public archives.

## Supported platforms and OTA formats

- Athena / Echo Show 8 Gen 2: full Android block OTA v4, `system.new.dat.br` and `vendor.new.dat.br`, with their transfer lists; `libasp.so` is under vendor/lib.
- Rook / Echo Spot Gen 1: full Android block OTA v3, `system.new.dat` with its transfer list; `libasp.so` is under system/lib and vendor is embedded inside system.
- Full file-tree ZIPs are supported if they contain the required runtime components.
- Incremental block updates, patch/move/stash commands, payload.bin and unknown filesystem layouts are rejected.

The extractor uses selective ext4 reads, at most 96 MiB retained blocks per partition, at most 64 streaming passes, and a bounded ELF dependency closure. No partition images are written. Factory DSP compatibility and acoustic results still require testing on actual hardware.

**User-controlled source policy:** vendor signatures, manufacturer-provided SHA attestations and TLS certificate/hostname checks are not required. The installer independently computes SHA-256 of a local OTA for cache identity; this is not manufacturer authenticity verification. Self-signed/untrusted HTTPS sources are accepted by design, so network tampering is possible. URL downloads are limited to 2 GiB and use a private temporary file until fully verified for transport completeness. Completed OTA downloads are **retained by default** as private files with unique readable names; local `--ota` paths are never removed.

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
./asp-installer status
./asp-installer status-json
./asp-installer check --ota /data/techo5-linux/update.bin
./asp-installer check-json --ota /data/techo5-linux/update.bin
./asp-installer check-vendor --ota /data/techo5-linux/update.bin
./asp-installer check-vendor-json --ota /data/techo5-linux/update.bin
```

Instead of `--ota FILE`, use `--ota-url 'https://user-selected-host/path/update.bin'` for a direct device download. The release manifest selects the platform automatically. A successfully downloaded OTA is **not deleted**. Its exact location is printed, for example `/data/techo5-linux/asp-installer-ota-<random>-update.bin`. Reuse this file in subsequent operations with `--ota /data/techo5-linux/asp-installer-ota-...-update.bin` to avoid downloading again. Partial/failed downloads are cleaned up automatically. Add **`--delete-downloaded-ota`** to a command using `--ota-url` only when you explicitly want the successfully downloaded file deleted at the end of that operation (including on later extraction failure).

`status`, `check`, and `check-vendor` print human-readable summaries. Use `status-json`, `check-json`, or `check-vendor-json` for full machine-readable JSON with the same respective flags. `inspect` and mutation commands continue returning JSON. All long operations print phase messages on stderr and a single-line activity spinner when attached to a terminal; structured results remain on stdout. A progress *scan* is **not a single file**: it is another pass through a compressed `system` or `vendor` partition to collect missing ext4 blocks that may serve multiple libraries. Output includes the monotonically increasing overall scan number and a per-partition pass counter; on-screen spinner lines are erased before log messages and on exit.

## Reusable extracted-OTA cache

The costly selective extraction runs **once per OTA content and bundled helper version**. All OTA-aware commands (`inspect`, `check`, `check-json`, `check-vendor`, `check-vendor-json`, `install`, `update`) first look for a **fully verified** extracted runtime cache. The default private cache root on Echo is:

`/data/techo5-linux/asp-installer-cache/`

The per-firmware directory is **human-readable**, for example `athena-PS75741-7080/` or `rook-690918120/`. If a distinct OTA or helper build has the same firmware release label, separate caches are preserved as `athena-PS75741-7080-variant-2/`, `...-variant-3/`, etc. **No hash fragments appear in directory names**: full OTA and helper SHA-256 identifiers are kept in the manifest and checked before reuse. Firmware version is read from the OTA's `META-INF/com/android/metadata` (not guessed from the file name). Inside are `manifest.json` (firmware, platform, full source and helper hashes, OTA format, file count, dependency edges), `SHA256SUMS` (sorted checksums and **paths of every extracted factory artifact**) and `files/system/...`, `files/vendor/...` containing the minimal ELF dependency closure. There are no whole partition images. The root and entries are restricted to the invoking owner, normally root on Echo.

- **Local `--ota FILE`:** compute the SHA-256 of the supplied OTA, locate the matching cached extraction, then check **every cached file** against `SHA256SUMS`, including missing, unexpected, unsafe or modified entries. No partition scans occur on a valid cache hit. Hashing the original OTA still requires one sequential file read, not full decompression.
- **Remote `--ota-url URL`:** first check any fully verified cache already linked to the same URL and helper. If none exists, scan the top level of `--tmp-dir` (default `/data/techo5-linux`) for an existing OTA whose basename matches the URL filename, including the installer's retained `asp-installer-ota-<random>-<filename>` archives. **Only** accept it when the local file's full SHA-256 equals an existing fully verified cache for the platform and helper. In that case, **reuse the cache without any network request or partition scan**, even when the cache was originally created with `--ota`. If no trustworthy local match exists, download the OTA and compare its full hash against existing caches before extraction. Invalid candidate filenames, symlinks, hardlinks and non-owner files are ignored; unrelated directories are not searched recursively.
- **Changed firmware or helper:** a different cache key requires a new extraction; previous revisions remain available. Partially written caches are not published: extraction is staged and published by an atomic rename, protected by a cross-process cache lock.
- **Invalid cache:** the command refuses the unsafe cache and reports the problem rather than trusting its contents. Use `--refresh-cache` explicitly to re-extract the selected OTA and atomically replace that cache, or to fetch changed content from a URL.

**Freshness limitation:** a URL-based cache hit (including one backed by a locally saved file) does not contact the remote server. If its contents have changed behind the same URL, the installer cannot know without `--refresh-cache` or providing a local OTA to hash. Checksums protect reuse of locally extracted bytes, **not** the identity or authenticity of the OTA publisher. If a saved OTA lives outside `--tmp-dir`, pass its parent directory as `--tmp-dir PATH` or select it explicitly with `--ota PATH`. `--refresh-cache` intentionally bypasses both existing cached extractions and local-OTA reuse so the URL is fetched again. `--delete-downloaded-ota` never deletes an already-existing local file that was reused. The cache persists independently of downloaded OTA archives and deployed `external-asp` generations; uninstalling the runtime does not silently delete these potentially expensive-to-recreate factory artifacts. Treat them as owner-private data.

Example:

```sh
./asp-installer inspect --ota-url 'https://your-ota-source/update.bin'
# Subsequent operations reuse the same cached extraction.
./asp-installer check-vendor --ota-url 'https://your-ota-source/update.bin'
./asp-installer check-vendor-json --ota-url 'https://your-ota-source/update.bin'
# Explicitly rebuild, for example when a URL's firmware content has changed:
./asp-installer inspect --ota-url 'https://your-ota-source/update.bin' --refresh-cache
```

When extraction is required, the free-space preflight also reserves room for the private cache in addition to download, staged rootfs, backup and the existing 64 MiB safety margin. `--cache-dir ABSOLUTE_PATH` overrides its location if you intentionally need a different filesystem. Cache validation happens before any installation mutation.

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
./asp-installer check-json --ota /data/techo5-linux/update.bin
./asp-installer check-vendor --ota /data/techo5-linux/update.bin
./asp-installer check-vendor-json --ota /data/techo5-linux/update.bin
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
