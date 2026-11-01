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

Transfer the matching archive to the Echo and extract its platform-specific root folder into **`/data/techo5-linux/asp-installer-v3/`**. Each package update **replaces the entire previous installer package** in that directory; it does not touch installed `/data/techo5-linux/external-asp` or the separate `/data/techo5-linux/asp-installer-cache`.

Connect as root and run commands from the unpacked package:

```sh
cd /data/techo5-linux/asp-installer-v3
# Contains asp-installer and assets/ from the current release.

./asp-installer version
./asp-installer inspect --ota /data/techo5-linux/update.bin
./asp-installer status
./asp-installer status --json
./asp-installer check --ota /data/techo5-linux/update.bin
./asp-installer check-json --ota /data/techo5-linux/update.bin
./asp-installer check-vendor --ota /data/techo5-linux/update.bin
./asp-installer check-vendor-json --ota /data/techo5-linux/update.bin
```

Instead of `--ota FILE`, use `--ota-url 'https://user-selected-host/path/update.bin'` for a direct device download. The release manifest selects the platform automatically. A successfully downloaded OTA is retained **inside** `/data/techo5-linux/asp-installer-cache/ota-images/`, normally as a readable name such as `rook-fireos-5.5.6.9-build-690918120.bin` or `athena-fireos-7574.1-build-PS75741-7080.bin` (with `-variant-N` for different contents). The precise path is displayed in command output and `status`; the original OTA is not bundled into FrankenASP releases. Partial/failed downloads are cleaned up automatically. Add **`--delete-downloaded-ota`** only when you explicitly want the newly downloaded image discarded after the command instead of retaining it as a cache archive. Previously saved images and user-supplied local files are not removed by this flag.

**Every command prints human-readable output by default:** `version`, `status`, `inspect`, `check`, `check-vendor`, `install`, `update`, `uninstall` and `rollback`. Specify **`--json`** to obtain machine-readable JSON, for example `inspect --json`, `install --yes --json`, or `uninstall --yes --json`. The legacy `status-json`, `check-json` and `check-vendor-json` aliases remain supported; all commands also accept the corresponding `*-json` alias. Progress messages, warnings and elapsed time are written to stderr, so JSON stdout remains valid without filtering. All long operations print phase messages on stderr and a single-line activity spinner when attached to a terminal; structured results remain on stdout. Every command also prints **total elapsed wall-clock time** to stderr after success or failure, leaving JSON stdout unchanged. A progress *scan* is **not a single file**: it is another pass through a compressed `system` or `vendor` partition to collect missing ext4 blocks that may serve multiple libraries. Output includes the monotonically increasing overall scan number and a per-partition pass counter; on-screen spinner lines are erased before log messages and on exit.

## Reusable extracted-OTA cache

The costly selective extraction runs **once per OTA content and bundled helper version**. All OTA-aware commands (`inspect`, `check`, `check-json`, `check-vendor`, `check-vendor-json`, `install`, `update`) first look for a **fully verified** extracted runtime cache. The default private cache root on Echo is:

`/data/techo5-linux/asp-installer-cache/`

The extracted-library directory is **human-readable**, for example `athena-fireos-7574.1-build-PS75741-7080/` or `rook-fireos-5.5.6.9-build-690918120/`. If a distinct OTA or helper build has the same firmware release label, separate caches are preserved as `athena-fireos-7574.1-build-PS75741-7080-variant-2/`, `...-variant-3/`, etc. **No hash fragments appear in directory names**: full OTA and helper SHA-256 identifiers are kept in the manifest and checked before reuse. Amazon build is read from the OTA's `META-INF/com/android/metadata`; the human-facing **Fire OS version** comes from Amazon's `ro.build.mktg.fireos` / `ro.build.version.name` in the OTA's `system/build.prop` (or its Amazon target properties). It is not derived from the Android version or guessed from digits in the filename. For earlier already-verified caches without this metadata, only explicitly recognized build identities have a fallback; unrecognized versions display as unrecorded. Inside are `manifest.json` (firmware, platform, full source and helper hashes, OTA format, file count, dependency edges), `SHA256SUMS` (sorted checksums and **paths of every extracted factory artifact**) and `files/system/...`, `files/vendor/...` containing the minimal ELF dependency closure. There are no whole partition images. OTA images reside separately under `asp-installer-cache/ota-images/<same-readable-version-name>.bin` so cache validation does not mistake a complete image for an extra extracted artifact. Both extracted files and images are root-private on Echo. A locally provided `--ota FILE` is copied into this directory when free space permits; the original is never moved or removed. If the filesystem lacks space for a second image, the verified extracted library cache remains usable and a clear warning is printed.

- **Local `--ota FILE`:** compute the SHA-256 of the supplied OTA, locate the matching cached extraction, then check **every cached file** against `SHA256SUMS`, including missing, unexpected, unsafe or modified entries. No partition scans occur on a valid cache hit. Hashing the original OTA still requires one sequential file read, not full decompression.
- **Remote `--ota-url URL`:** downloads are stored in the private `ota-images` directory. A complete download that could not yet be extracted is still visible in `status` and is eligible for reuse on a later attempt. First check any fully verified cache already linked to the same URL and helper. If none exists, scan the top level of `--tmp-dir` (default `/data/techo5-linux`) for an existing OTA whose basename matches the URL filename, including the installer's retained `asp-installer-ota-<random>-<filename>` archives. **Only** accept it when the local file's full SHA-256 equals an existing fully verified cache for the platform and helper. In that case, **reuse the cache without any network request or partition scan**, even when the cache was originally created with `--ota`. If no trustworthy local match exists, download the OTA and compare its full hash against existing caches before extraction. Invalid candidate filenames, symlinks, hardlinks and non-owner files are ignored; unrelated directories are not searched recursively.
- **Changed firmware or helper:** a different cache key requires a new extraction; previous revisions remain available. Partially written caches are not published: extraction is staged and published by an atomic rename, protected by a cross-process cache lock.
- **Invalid cache:** the command refuses the unsafe cache and reports the problem rather than trusting its contents. Use `--refresh-cache` explicitly to re-extract the selected OTA and atomically replace that cache, or to fetch changed content from a URL.

**Freshness limitation:** a URL-based cache hit (including one backed by a locally saved file) does not contact the remote server. If its contents have changed behind the same URL, the installer cannot know without `--refresh-cache` or providing a local OTA to hash. Checksums protect reuse of locally extracted bytes, **not** the identity or authenticity of the OTA publisher. If a saved OTA lives outside `--tmp-dir`, pass its parent directory as `--tmp-dir PATH` or select it explicitly with `--ota PATH`. `--refresh-cache` intentionally bypasses both existing cached extractions and local-OTA reuse so the URL is fetched again. `--delete-downloaded-ota` never deletes an already-existing local file that was reused. The cache persists independently of deployed `external-asp` generations; uninstalling the runtime does not silently delete these potentially expensive-to-recreate factory artifacts. Treat them as owner-private data.

Example:

```sh
./asp-installer inspect --ota-url 'https://your-ota-source/update.bin'
# Subsequent operations reuse the same cached extraction.
./asp-installer check-vendor --ota-url 'https://your-ota-source/update.bin'
./asp-installer check-vendor-json --ota-url 'https://your-ota-source/update.bin'
# Explicitly rebuild, for example when a URL's firmware content has changed:
./asp-installer inspect --ota-url 'https://your-ota-source/update.bin' --refresh-cache
```

When extraction is required, free-space preflight separately labels the private **extraction cache**, optional **staged runtime**, backup, downloaded OTA and 64 MiB safety margin. Components on the **same** filesystem are added together; different filesystems each receive a single 64 MiB safety margin. For `--ota-url`, a preliminary capacity report explicitly says that OTA size is **not yet known**; after reading the HTTP `Content-Length`, the installer prints the **combined total including OTA bytes** before accepting the response body. If the server omits `Content-Length`, the installer retains the runtime/cache/backup reserve and checks free space while streaming the OTA. A valid cached extraction skips the pre-extraction reserve entirely. `--cache-dir ABSOLUTE_PATH` overrides its location if you intentionally need a different filesystem. Cache validation happens before any installation mutation.

## Selecting a cached firmware without OTA arguments

Once a factory extraction has been cached, **`--ota` and `--ota-url` are optional** for `inspect`, `check`, `check-json`, `check-vendor`, `check-vendor-json`, `install` and `update`:

```sh
./asp-installer status
./asp-installer status-json
./asp-installer inspect                         # Use the single compatible cache
./asp-installer inspect --firmware PS75741-7080  # Choose a cached firmware version
./asp-installer install --firmware PS75741-7080 --yes
./asp-installer inspect --firmware 5.5.6.9   # Fire OS version, when unambiguous
# If multiple variants share the same release label, select the exact cache name:
./asp-installer inspect --firmware athena-fireos-7574.1-build-PS75741-7080-variant-2
```

The release manifest determines platform and the bundled helper SHA-256 must match. Every selected cache is verified against its full artifact SHA-256 inventory. No OTA image is needed for already-extracted files. If more than one compatible cache exists, the installer **refuses ambiguous automatic selection** and displays available directory names; selecting a shared firmware label is likewise ambiguous until the exact variant name is supplied. `--firmware` accepts the Amazon build, the readable Fire OS version (when unambiguous) or the exact cache directory name. It cannot be combined with `--ota` or `--ota-url`, and `--refresh-cache` always requires a source. When an OTA command acquires the exclusive cache lock, any valid old `rook-690918120` / `athena-PS75741-7080` directories are upgraded in-place by atomic rename (along with their verified OTA images, when present) to the new readable names; `status` itself remains read-only.

`status` now **separates installed FrankenASP runtime metadata** (which may be unrecorded for earlier manual installations) **from cached factory firmware metadata**. It reports each verified cache's target model/codename, Fire OS version, Amazon firmware build, full paths and file count. Installed-runtime presence is not a running/active ASP check. It also lists saved generations and every valid/invalid extracted cache and archived OTA image with its path, size and integrity status. A matching image is marked `ready` only after comparing its SHA-256 to the cache manifest; damaged images are marked `invalid`. Downloaded images without a successful extraction are listed as `unassociated` and not mistaken for usable extracted libraries. `status-json` contains these as `cache_root`, `extracted_caches`, and `ota_images`. Both commands optionally accept `--cache-dir ABSOLUTE_PATH`.

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

The installer preserves a fixed **64 MiB safety margin per filesystem** and checks cumulative requirements before modifying an installed runtime. The conservative **168 MiB extraction-cache bound** is distinct from the optional **168 MiB staged-runtime bound**; neither represents the OTA size. Actual extracted factory files are normally substantially smaller, and their precise disk allocation is checked before cache publication or runtime staging.

- **Valid cache hit:** no new extraction or download, so no pre-extraction/cache-write reserve is applied. Installation still checks its actual staged runtime and backup requirements before writing.
- **Local OTA cache miss:** the existing OTA is not counted as another download. A separate retained copy of a local image requires additional free space; if a safe copy is impossible, an explicit warning is printed without destroying the usable extracted files. Cache capacity, optional runtime stage, backup and the safety margin are summed by filesystem.
- **Remote OTA cache miss:** the preliminary report states *OTA size pending HTTP Content-Length*. When the server provides a length, the complete **OTA + extraction cache + staged runtime + backup + 64 MiB reserve** is reported and checked on any shared filesystem **before streaming the body**. On separate filesystems, only the applicable roles are charged to each one. Without a length, the download remains capped at 2 GiB and capacity is checked during streaming while preserving all remaining-stage reserves.
- **Live download monitoring:** already-written OTA bytes are reflected in filesystem free space and are **not counted again** by the streaming guard. `--force-no-backup` excludes only the optional update archive, not its staged runtime or the safety margin.

For example, an `install` with new extraction on a single filesystem has a conservative **400 MiB preliminary reserve** (168 MiB cache + 168 MiB staged runtime + 64 MiB safety). The OTA size is unknown at this point: it is **not** a 400 MiB firmware estimate. If the HTTP response subsequently reports a 1,100 MiB OTA, the full required free space becomes **1,500 MiB** before download. The output labels each component instead of showing `runtime 168` for the cache.

All capacity checks are estimates: concurrent writes can still cause an `ENOSPC` error. The installer never treats an unknown OTA size as zero for its final streaming guard.

## Rollback and uninstall

```sh
./asp-installer status
./asp-installer rollback --generation '.external-asp-generation-v3-YYYYMMDDTHHMMSSZ-RANDOM' --yes
./asp-installer uninstall --yes
```

Rollback verifies the saved generation and all active references, archives the current installation, atomically exchanges the selected generation with current, and retains the replaced generation.

Uninstall stops only recognized FrankenASP processes, checks for other runtime references, then detaches and removes the installed external-asp directory and recognized V3 generations. It fails closed on unknown files, foreign ownership or unexpected process references. By default, it keeps previously archived TAR backups. `--purge-backups` additionally removes only recognized installer backups. After uninstall **disable External ASP yourself in TECHO5 settings**; TECHO5 configuration is not modified by this utility.

## Production layout and qualification

The installer deliberately does **not** validate the owner, mode, or specific UID/GID of `/data`, `/data/techo5-linux`, or any other pre-existing parent directory: these paths are managed by Android/TECHO5 and may vary across devices and releases. It validates **only its own objects** (the `external-asp` runtime tree, its private cache and backups, its lock file, and its installer-managed staging/generation trees), including required ownership, file types, modes, and integrity. Parent-directory permissions are enforced by the operating system when accessing files; the installer does not change them.

The destination remains `/data/techo5-linux/external-asp/{bin,etc,lib}`. The launcher expects an exact `ASP1-LAUNCHER-V1` profile; the helper is `lib/rootfs/factory/asp1-child`, and factory `/system/bin/linker` is extracted locally. All installer-owned directories are root-only 0700; executable files 0500; runtime data 0600. Genuine device calibration is exposed through validated /proc symlink aliases, not copied or recorded.

```sh
cd common-tools/asp-installer
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```

Selective extraction was tested against real Athena and Rook OTA images in an isolated Linux test environment. Unit tests cover transfer-range failures, path safety, download truncation, ELF dependency closure, vendor comparisons and installed file comparisons.

**Experimental:** the new privileged install/update/rollback/uninstall logic has not yet undergone full on-device hardware qualification. Running helpers, private mount namespaces, namespace-descriptor keepers, filesystem crash recovery and daemon supervision need physical Echo acceptance before a production release. No code here updates the TECHO5 binary, firmware, boot slot or persistence configuration.
