# FrankenASP V3 — desktop extraction and delivery

`asp-extractor` prepares the original Amazon factory ASP dependencies **on a desktop** from an owner-provided full OTA or supported TECHO5 backup. It builds a verified local factory-file cache, then can transfer the selected cache and a FrankenASP installer release to an Echo. It does **not** install or activate the runtime on the Echo: that is the separate, manual `asp-installer` step.

**Supported host:** Linux x86_64 only. **Factory extraction** is independent of Echo codenames for supported source formats. **FrankenASP runtime packages and hardware validation** currently cover Echo Show 8 (2nd generation, `athena`) and Echo Spot (1st generation, `rook`). The V4 runtime archive parser supports any valid Echo codename without generic source changes, but a working launcher/child and hardware verification remain platform responsibilities; no additional model is claimed production-ready. The tool includes no Amazon firmware or proprietary factory files; private extracted caches and bundles must **not** be published to GitHub Releases.

Real full CrAU v2 Donut/Biscuit OTA sources identify themselves as `donut_puffin` and `biscuit_puffin` (not `donut` or `biscuit`). Use those exact values for `--platform` and any matching native release packaging identity. A shorter display label is not an extraction alias; existing caches are never silently renamed.

The desktop and embedded tools use compatible factory extraction and verified-cache formats. For on-device installation, maintenance and activation boundaries, see [ASP-INSTALL.md](ASP-INSTALL.md).

The `ASP libs` value in `asp-extractor list` and `asp-installer status` is the same first 16 hexadecimal characters of a deterministic SHA-256 fingerprint over selected extracted ELF libraries and linker components recorded in Cache V2 `SHA256SUMS`. Firmware metadata, configuration files and other non-library resources are not included in this informational fingerprint, but remain fully preserved and verified in the cache. **Matching fingerprints do not establish equivalent runtime configuration or behavior.**

## How it works

1. Accepts one **local full OTA ZIP**, an explicitly requested OTA URL, or a directory of supported raw TECHO5 backup images. When present, device identity is read from source metadata; the platform selector is optional, including for URL extraction.
2. Extracts the factory audio files required for processing, **not** complete partition images. Factory extraction does not require a matching FrankenASP runtime release. File hashes and cache integrity are verified.
3. Reuses a verified extraction for the same input when possible. Different OTA/backup sources with **identical factory files** share one cache directory regardless of optional platform, build or Fire OS labels; the original cache metadata is retained. Genuinely different factory contents receive a numbered `-variant-N` directory, including after an explicit refresh of an older, less-complete inventory from the **same source SHA**.
4. Optionally lists/verifies the factory cache, creates a **private** combined bundle, or transfers selected factory files and a matching installer release over authenticated SSH.
5. Leaves `external-asp/`, TECHO5 settings, `echod` and persistent startup untouched. `deploy-all` delivers files **only**. On Echo, the operator must run `asp-installer install` / `update` and explicitly enable External ASP in TECHO5.

A local OTA is **never copied or deleted** by `asp-extractor`; local backups are also read in place. Source paths are not retained in the cache. Only OTA files actually **downloaded by the tool** are stored under `ota-images/` by default.

## Universal runtime releases and factory-source boundary

`asp-extractor` and the ARMv7 `asp-installer` are built from a **single repository-root Go module**. Shared source readers, ELF closure, cache validation/selection and release parsing reside in `internal/asp`; desktop builds no longer copy installer lifecycle/process-management code. Both CLIs validate `ASP1-ASSETS-V4` archives for arbitrary canonical platform names. The historical Athena and Rook V3 archives retain their precise member and capability requirements.

A V4 archive declares every public FrankenASP-owned payload's role, size and SHA-256, plus the launcher's permitted bundled/live vendor modes and ordered factory library search paths. The same validated release plan supplies the original bytes to `create-bundle` and `deploy-all`. An optional channel map or other public data asset is delivered to its declared factory-root-relative destination as **opaque data**; the installer never rewrites channel geometry. Athena's own contract still requires its V1 map; Rook's native child does not yet apply one. Unlisted archives, unsupported V3 platforms, invalid manifests and mismatched source/target codenames are rejected before deployment.

Factory extraction itself never accepts a list of required files from the helper/release. It follows the original `libasp.so` and transitive `DT_NEEDED` and recursively includes all regular audio-algorithm resources under the original system/vendor audio-algorithms trees (nested CFG/JSON/TFLite/coefficients included). It does not parse arbitrary config strings as dependencies, scan unrelated directories or claim to catch dynamic `dlopen()` references. Static helper compatibility is checked afterward with the release's ordered library paths; native startup remains the final compatibility check.

The physical Echo's calibration is **not** part of OTA extraction, the public release or the desktop cache. The on-device installer discovers authentic populated `miccal.N` properties under kernel IDME and creates only safe aliases; factory DSP reads the kernel properties itself. No calibration gains, values or hashes are transferred.

**Build and verify** (from the repository root, with the supported Go toolchain):

```sh
GOTOOLCHAIN=local GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
  go build -trimpath -o ./asp-installer-armv7 ./common-tools/asp-installer
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -o ./asp-extractor-amd64 ./common-tools/asp-extractor/cmd
GOTOOLCHAIN=local go build -trimpath -o ./asp-release ./common-tools/asp-release/cmd

./asp-release --validate ./frankenasp-vVERSION-CODENAME-arm32.tar.gz
```

See [RELEASING.md](RELEASING.md) for the platform-neutral packager's contract, CLI arguments and exact release-inventory checks. No original Amazon binaries, extracted caches or private bundles belong in public release archives.

## Minimal workflow: prepare and deliver a local OTA

These commands run on the **Linux PC**. Substitute the actual release archive, device hostname, and Amazon firmware build; the placeholders below are **not** real firmware identifiers or URLs. Preconfigure root public-key SSH access and a verified host key in `~/.ssh/known_hosts`.

```sh
EX=./asp-extractor
RELEASE=./frankenasp-vVERSION-rook-arm32.tar.gz
OTA=./full-ota.zip

# The local OTA determines the target platform automatically.
"$EX" extract --ota "$OTA"
"$EX" list

# Transfer the matching installer package plus one factory cache.
"$EX" deploy-all --host root@ECHO_HOST \
  --asp-release "$RELEASE" --firmware YOUR_AMAZON_BUILD
```

For Athena, use the matching Athena release archive. The desktop cache location is optional; these commands use its default. If you extracted using `--cache-dir ./asp-installer-cache/`, run subsequent `list`, `verify` and `deploy-all` commands from the same working directory with that cache path (or specify its absolute equivalent).

**Then on the Echo**, with External ASP disabled in TECHO5 settings:

```sh
cd /data/techo5-linux/asp-installer-v3
./asp-installer status
./asp-installer install --firmware YOUR_AMAZON_BUILD --yes
./asp-installer status
```

The human-readable `status` shows generation/backup counts; use `./asp-installer status --json` to obtain the exact names required for rollback.

If `external-asp/` already exists, use `update --firmware YOUR_AMAZON_BUILD --yes` instead of `install`. The `--firmware` selector is optional if **only one** compatible cache exists for this model, but is recommended when several builds are present. If multiple **different** sets share one build, pass their exact cache directory name instead. No complete OTA is transferred to the Echo by `deploy-all`.

### Instead of an OTA: read a TECHO5 backup

`--backup-dir` takes a directory containing **raw ext4 partition images** (not a tarball). Place the system image and, if available, the separate vendor image in the same directory. Files must have the `.img` extension; the tool identifies their roles from filesystem contents rather than device codenames or fixed filenames.

```sh
"$EX" extract --backup-dir /path/to/BACKUP_DIR
"$EX" list
```

The utility identifies the available supported filesystems. When source metadata identifies the device, an explicit `--platform` must agree. Images are read without mounting. Only extracted factory files are cached; original images and their paths are not stored. Whole-device dumps and image tarballs are not accepted.

### Instead of a local file: explicitly download a full OTA

```sh
# Set OTA_URL to a full, trusted OTA URL yourself before running this.
"$EX" extract --platform rook --ota-url "$OTA_URL"
```

For `--ota-url`, `--platform` is optional. When supplied, it is checked against available OTA device identity, including on cache reuse. A successfully downloaded OTA is normally retained in `<cache-dir>/ota-images/`; for `--ota FILE`, the local OTA is **never copied there**. HTTPS certificate and hostname validation are **not** provided by the OTA downloader. Independently verify the firmware source; SHA-256 protects cache integrity, **not Amazon authenticity**.

## Command reference

| Command | Exact behavior |
| --- | --- |
| `help`, `--help`, `-h` | Print CLI help. |
| `version`, `--version` | Print the **desktop tool** version, not a firmware build or the installed FrankenASP runtime revision. |
| `extract` | Requires **exactly one** of `--ota`, `--ota-url`, `--backup-dir`. Reads/extracts factory files into the desktop cache; it does **not** use a FrankenASP release. Rejects `--asp-release`. |
| `list` | Lists verified extracted factory sets and retained downloaded OTA images, with optional platform filtering. Each verified set shows a 16-character informational `ASP libs` fingerprint. Invalid or legacy cache entries are marked. Does **not** list backup source paths. |
| `verify` | Fully validates one cached factory set (metadata, inventory, per-file SHA-256 and safe layout). Use `--platform` to filter, or `--firmware` with the exact cache directory name to resolve ambiguity. |
| `create-bundle` | Creates an owner-private `.tar.gz` containing exactly one verified extraction and the **unchanged** matching versioned FrankenASP release, plus SHA-256 manifest. Requires `--asp-release FILE` and `--output FILE`. It does **not** deploy. |
| `deploy-cache` | Transfers one verified factory extraction to `/data/techo5-linux/asp-installer-cache/` on Echo. Requires `--host`; `--platform` is optional for factory-cache selection; does **not** transfer the installer. |
| `deploy-asp` | Transfers/replaces the versioned **installer package only** under `/data/techo5-linux/asp-installer-v3/`. Requires `--host` and `--asp-release`; rejects `--firmware`. Does **not** install the runtime. |
| `deploy-all` | Transfers a selected factory cache **and** matching installer package. Requires `--host` and `--asp-release`; optional `--firmware` selects among cached builds. Does **not** install or activate the runtime. |

`deploy-app` is **not** a command. `--asp-release` takes a local path to an already built, versioned `.tar.gz` archive, **not** a version number; there is no `--release` lookup feature. The desktop parser accepts the command before or after flags; the examples place it first.

### Every option and default

| Option | Default | Accepted use and constraints |
| --- | --- | --- |
| `--platform CODENAME` | **Unset** | Optional device codename. Detected from source metadata when present; unknown and absent names are accepted for factory-only operations. An explicit value must agree with detected identity. Not required for `extract`, `list`, `verify`, or `deploy-cache`. In `create-bundle`, `deploy-asp`, `deploy-all`, inferred from release archive; any explicit value must agree. Filters factory sets with `list`. |
| `--firmware NAME` | **Unset** | Select existing extraction by Amazon build, uniquely matching Fire OS version, or **exact cache folder name** to disambiguate `-variant-N`. Omit if the relevant cache selection is unique. For `extract --backup-dir`, optional explicit build must agree with detected metadata; for `extract --ota` or `--ota-url`, the OTA supplies the build and the option is rejected. `deploy-asp` rejects it. |
| `--cache-dir DIR` | `$XDG_CACHE_HOME/frankenasp/asp-extractor/asp-installer-cache`, or `$HOME/.cache/frankenasp/asp-extractor/asp-installer-cache` when `XDG_CACHE_HOME` is unset | Desktop cache root. Accepts absolute or relative paths (relative to the current working directory); normalized internally to an absolute path. The filesystem root `/` is forbidden, and cache ownership, permission and integrity checks are unchanged. Nonabsolute `XDG_CACHE_HOME` is still an error. The flag never changes the **fixed** Echo target paths. |
| `--ota FILE` | **Unset** | Local **full** OTA ZIP for `extract`; read in place, never copied or deleted. Exclusive with `--ota-url` and `--backup-dir`. |
| `--ota-url URL` | **Unset** | Owner-specified full OTA HTTP(S) URL for `extract`; `--platform` optional. Download is retained by default. Exclusive with other sources. |
| `--backup-dir DIR` | **Unset** | Directory holding supported raw TECHO5 backup images for `extract`; original images and source path are not stored. Exclusive with OTA sources. |
| `--refresh-cache` | `false` | Reprocess explicit source on `extract` rather than using early source-identity reuse. If extracted file contents already match, they are still deduplicated; different contents can receive another variant. |
| `--asp-release FILE` | **Unset** | Required by `create-bundle`, `deploy-asp`, `deploy-all`. Must point to a local matching `frankenasp-vVERSION-PLATFORM-arm32.tar.gz`; validates release platform, assets and cache compatibility. Not used by `extract`, `verify`, `list` or `deploy-cache`. |
| `--output FILE` | **Unset** | Required by `create-bundle`. Must end in `.tar.gz`; output **cannot overwrite** an existing file. |
| `--host root@HOST` | **Unset** | Required by `deploy-cache`, `deploy-asp` and `deploy-all`. Only `root` is accepted as SSH username. |
| `--port NUMBER` | `22` | SSH port (1–65535). |
| `--identity FILE` | First existing `~/.ssh/id_ed25519`, then `id_rsa`, then `id_ecdsa` | Local SSH private key; must be readable/parseable without an interactive passphrase prompt. |
| `--known-hosts FILE` | `~/.ssh/known_hosts` | Host-key verification file. Host keys must already be pinned; there is **no unsafe bypass**. |

`--platform` and `--firmware` are metadata filters/selectors, not extraction processing parameters. If a selection matches multiple caches, specify the **exact cache directory name** with `--firmware` and omit `--ota` / `--ota-url`; do not guess a variant. `--refresh-cache` with a backup explicitly rescans the original images; with a local OTA it rereads the local ZIP. The CLI does not have a separate general-purpose temporary-directory option. Download working files are created within the private cache's `ota-images/` only for network downloads.

## Files and retention

**Desktop default** (when `XDG_CACHE_HOME` is unset):

```text
$HOME/.cache/frankenasp/asp-extractor/
└── asp-installer-cache/
    ├── PLATFORM-fireos-FIRE_OS_VERSION-build-AMAZON_BUILD/
    │   ├── manifest.json     # firmware/build/source details and internal schema
    │   ├── SHA256SUMS        # cryptographic inventory of factory files
    │   └── files/           # verified extracted factory files
    ├── PLATFORM-AMAZON_BUILD/  # possible if Fire OS version is unavailable
    ├── factory-sha256-SOURCE_HASH_PREFIX-.../ # codename unavailable
    ├── ...-variant-N/        # same build, different factory file contents
    ├── .cache.lock          # private concurrent extraction lock
    └── ota-images/          # only OTA downloads performed by the tool
```

With `XDG_CACHE_HOME` set to an absolute directory, the root is instead `$XDG_CACHE_HOME/frankenasp/asp-extractor/asp-installer-cache/`. Use `--cache-dir` to override either default. Each extraction records its cache format and source metadata in `manifest.json`. Verified factory file content and the complete SHA-256 inventory determine duplicates independently of descriptive platform, build and Fire OS labels. The same source can be reused without another partition scan; requesting a different source may cause a temporary extraction and comparison, but only genuinely different factory content is retained as another set.

A local OTA stays wherever the user placed it. Backup ext4 images likewise remain untouched and are not copied to cache. Completed **tool-downloaded** OTA images are retained in `ota-images/` by default. `list` shows factory sets and available downloads without duplicating entries for their input sources. The actual installed runtime is **not** stored in the desktop cache.

**Remote destination** (fixed irrespective of local `--cache-dir`):

```text
/data/techo5-linux/
├── asp-installer-v3/      # FrankenASP-owned installer executable and assets
└── asp-installer-cache/   # one selected verified factory set transferred
```

`deploy-cache` checks for an existing destination: if present, it compares metadata and every transferred file, keeps an identical copy untouched and refuses to overwrite a conflicting one. `deploy-asp` stages and verifies the complete replacement installer and replaces **only** `asp-installer-v3/` (without backing up the old installer package). Deployment uses temporary private staging and verified Go SSH transfers; it does not require system `scp` or `ssh` executables. Neither operation touches `/data/techo5-linux/external-asp/`, TECHO5 settings, persistence or `echod`.

## Optional operations

Verify the cache without transferring anything:

```sh
"$EX" verify --platform rook --firmware YOUR_AMAZON_BUILD
```

Transfer only the factory cache when a compatible installer is already present on Echo:

```sh
"$EX" deploy-cache --host root@ECHO_HOST \
  --platform rook --firmware YOUR_AMAZON_BUILD
```

Transfer only a newer installer package when the correct factory files are already on Echo:

```sh
"$EX" deploy-asp --host root@ECHO_HOST --asp-release "$RELEASE"
```

Create a **private** package for archival/transfer by some other mechanism:

```sh
"$EX" create-bundle --asp-release "$RELEASE" \
  --firmware YOUR_AMAZON_BUILD --output ./private-factory-bundle.tar.gz
```

The bundle contains one verified factory cache, the original release archive and a bundle manifest with component hashes. The bundle is **not** a valid `asp-installer --assets` directory and there is no `deploy-bundle` command. It is not published by the build. Its contents may include Amazon-owned binaries; keep it private. Bundle creation does not modify the source cache or any Echo.

## Build, package and tests

Use Go **1.26.9** or a compatible newer version (validated with **1.27.3**). From the repository root:

```sh
cd common-tools/asp-extractor
GO=/path/to/go/bin/go ./build.sh
GO=/path/to/go/bin/go ./test.sh

# Substitute the actual versioned filename returned by build.sh.
./package.sh dist/asp-extractor-vVERSION-linux-amd64
```

`build.sh` emits `dist/asp-extractor-vVERSION-linux-amd64`; `package.sh` creates the versioned `.tar.gz` containing the executable, LICENSE, short README and this full guide. The produced Linux x86_64 binary is statically linked (`CGO_ENABLED=0`) and needs no Go installation, system SSH/SCP utilities, mounts or native ext4 drivers at runtime. Race tests use a C toolchain. Building/packaging does not publish a release or deploy anything.

## Input compatibility and safety

Supported inputs are compatible **full** Android OTA ZIPs and directories containing supported raw ext4 partition images. Full Android A/B CrAU v2 payloads (ZIP-embedded or standalone) are supported when every needed partition operation is source-independent (REPLACE, REPLACE_BZ, REPLACE_XZ, ZERO, DISCARD). Incremental/delta OTAs needing source partitions and unsupported image layouts are rejected. Local OTA ZIPs are limited to **2 GiB**. ZIP-contained payloads are checked against ZIP CRC, and accessed operations against their available SHA-256 hashes; when operation hashes are absent, the whole selected partition is checked against its declared digest before cache publication. Internal checks do not prove vendor authenticity. Backup extraction requires no host filesystem mount.

The cache validates owner/modes, absence of unexpected files and symlinks, manifest and **all** artifact hashes before reuse. SHA-256 checks do not establish official Amazon firmware authenticity; you must verify the original source independently. The OTA downloader does **not** validate HTTPS certificates or hostnames. SSH delivery requires a pinned host key and verified transfers; there is no bypass for SSH host-key verification.

## Host compatibility

Only **Linux x86_64** is currently supported. The available executable does not run on macOS or Windows.

For Echo runtime installation, updates, rollback, maintenance and storage layout, read [ASP-INSTALL.md](ASP-INSTALL.md). For release versioning/publishing policy, read [RELEASING.md](RELEASING.md).
