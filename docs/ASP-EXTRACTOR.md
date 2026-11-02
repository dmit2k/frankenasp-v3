# FrankenASP V3 — desktop extraction and delivery

`asp-extractor` prepares the original Amazon factory ASP dependencies **on a desktop** from an owner-provided full OTA or supported TECHO5 backup. It builds a verified local factory-file cache, then can transfer the selected cache and a FrankenASP installer release to an Echo. It does **not** install or activate the runtime on the Echo: that is the separate, manual `asp-installer` step.

**Supported host:** Linux x86_64 only. **Supported targets:** Echo Show 8 (2nd generation, `athena`) and Echo Spot (1st generation, `rook`). Other desktop OS implementations and Echo models have not been implemented or tested. The tool includes no Amazon firmware or proprietary factory files; private extracted caches and bundles must **not** be published to GitHub Releases.

The desktop and embedded tools reuse the same OTA/ext4 reader, ELF dependency-closure extractor and SHA-256 cache code. For on-device installation, maintenance and activation boundaries, see [ASP-INSTALL.md](ASP-INSTALL.md).

## How it works

1. Accepts one **local full OTA ZIP**, an explicitly requested OTA URL, or a directory of supported raw TECHO5 backup images. Local OTA/backup inputs are auto-detected as Athena/Rook; URL extraction requires a platform selector before download.
2. Extracts only the factory files and dependencies needed by the supported helper, **not** complete partition images. Validates file hashes and manifest/inventory integrity.
3. Reuses a verified extraction for the same input when possible. Different OTA/backup sources with **identical factory files** share one cache directory (platform, build and Fire OS version must agree). Genuinely different factory contents receive a numbered `-variant-N` directory.
4. Optionally lists/verifies the factory cache, creates a **private** combined bundle, or transfers selected factory files and a matching installer release over authenticated SSH.
5. Leaves `external-asp/`, TECHO5 settings, `echod` and persistent startup untouched. `deploy-all` delivers files **only**. On Echo, the operator must run `asp-installer install` / `update` and explicitly enable External ASP in TECHO5.

A local OTA is **never copied or deleted** by `asp-extractor`; a local backup is also read in place and forgotten. There is no backup source-path index: old `asp-extractor-backups.json` files from earlier experimental builds are ignored. Only OTA files actually **downloaded by the tool** are stored under `ota-images/` by default.

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

For Athena, use the matching Athena release archive. The desktop cache location is optional; these commands use its default. If you extracted using `--cache-dir /YOUR/PRIVATE/CACHE`, pass **that same** `--cache-dir` to `list`, `verify` and `deploy-all` as applicable.

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

`--backup-dir` takes a directory containing **raw ext4 partition images** (not a tarball). Supported layouts:

```text
# Athena (both in the selected directory):
BACKUP_DIR/p21-system.img
BACKUP_DIR/p12-vendor.img

# Rook:
BACKUP_DIR/system.img
```

```sh
"$EX" extract --backup-dir /path/to/BACKUP_DIR
"$EX" list
```

The utility detects the model from the layout and checks device identity against the image metadata. It reads ext4 directly in Go, without mounting the images. It saves **only extracted factory files**, not the backup images or their paths. Whole-device eMMC dumps, arbitrary image tarballs and unknown layouts are not currently accepted by this interface.

### Instead of a local file: explicitly download a full OTA

```sh
# Set OTA_URL to a full, trusted OTA URL yourself before running this.
"$EX" extract --platform rook --ota-url "$OTA_URL"
```

For `--ota-url`, `--platform` is mandatory because the URL contents are not yet available for auto-detection. A successfully downloaded OTA is normally retained in `<cache-dir>/ota-images/`; for `--ota FILE`, the original local OTA is **never copied there**. The inherited downloader does not perform normal HTTPS certificate/hostname verification. Independently authenticate the firmware's provenance; SHA-256 protects cache integrity, **not Amazon authenticity**.

## Command reference

| Command | Exact behavior |
| --- | --- |
| `help`, `--help`, `-h` | Print CLI help. |
| `version`, `--version` | Print the **desktop tool** version, not a firmware build or the installed FrankenASP runtime revision. |
| `extract` | Requires **exactly one** of `--ota`, `--ota-url`, `--backup-dir`. Reads/extracts factory files into the desktop cache; it does **not** use a FrankenASP release. Rejects `--asp-release`. |
| `list` | Lists verified extracted factory sets and retained downloaded OTA images, with optional platform filtering for factory sets. Invalid or legacy cache entries are marked. Does **not** list backup source paths. |
| `verify` | Fully validates one cached factory set (metadata, inventory, per-file SHA-256 and safe layout). Requires `--platform`; choose `--firmware` if multiple candidates exist. |
| `create-bundle` | Creates an owner-private `.tar.gz` containing exactly one verified extraction and the **unchanged** matching versioned FrankenASP release, plus SHA-256 manifest. Requires `--asp-release FILE` and `--output FILE`. It does **not** deploy. |
| `deploy-cache` | Transfers one verified factory extraction to `/data/techo5-linux/asp-installer-cache/` on Echo. Requires `--host` and `--platform`; does **not** transfer the installer. |
| `deploy-asp` | Transfers/replaces the versioned **installer package only** under `/data/techo5-linux/asp-installer-v3/`. Requires `--host` and `--asp-release`; rejects `--firmware`. Does **not** install the runtime. |
| `deploy-all` | Transfers a selected factory cache **and** matching installer package. Requires `--host` and `--asp-release`; optional `--firmware` selects among cached builds. Does **not** install or activate the runtime. |

`deploy-app` is **not** a command. `--asp-release` takes a local path to an already built, versioned `.tar.gz` archive, **not** a version number; there is no `--release` lookup feature. The desktop parser accepts the command before or after flags; the examples place it first.

### Every option and default

| Option | Default | Accepted use and constraints |
| --- | --- | --- |
| `--platform CODENAME` | **Unset** | `athena` or `rook`. Auto-detected from local `extract --ota` or `--backup-dir`. **Required** for `extract --ota-url`, `verify`, and `deploy-cache`. In `create-bundle`, `deploy-asp`, `deploy-all`, inferred from release archive; any explicit value must agree. Filters factory sets with `list`. |
| `--firmware NAME` | **Unset** | Select existing extraction by Amazon build, uniquely matching Fire OS version, or **exact cache folder name** to disambiguate `-variant-N`. Omit if the relevant cache selection is unique. For `extract --backup-dir`, optional explicit build must agree with detected metadata; for `extract --ota` or `--ota-url`, the OTA supplies the build and the option is rejected. `deploy-asp` rejects it. |
| `--cache-dir DIR` | `$XDG_CACHE_HOME/frankenasp/asp-extractor/asp-installer-cache`, or `$HOME/.cache/frankenasp/asp-extractor/asp-installer-cache` when `XDG_CACHE_HOME` is unset | Desktop cache root. Must be an **absolute path**, not `/`. Nonabsolute `XDG_CACHE_HOME` is an error. The flag applies to all operations that use the local cache; it never changes the **fixed** Echo target paths. |
| `--ota FILE` | **Unset** | Local **full** OTA ZIP for `extract`; read in place, never copied or deleted. Exclusive with `--ota-url` and `--backup-dir`. |
| `--ota-url URL` | **Unset** | Owner-specified full OTA HTTP(S) URL for `extract`; requires `--platform`. Download is retained by default. Exclusive with other sources. |
| `--backup-dir DIR` | **Unset** | Directory holding supported raw TECHO5 backup images for `extract`; original images and source path are not stored. Exclusive with OTA sources. |
| `--refresh-cache` | `false` | Reprocess explicit source on `extract` rather than using early source-identity reuse. If extracted file contents already match, they are still deduplicated; different contents can receive another variant. |
| `--asp-release FILE` | **Unset** | Required by `create-bundle`, `deploy-asp`, `deploy-all`. Must point to a local matching `frankenasp-vVERSION-PLATFORM-arm32.tar.gz`; validates platform, required assets and internal cache schema. Not used by `extract`, `verify`, `list` or `deploy-cache`. |
| `--output FILE` | **Unset** | Required by `create-bundle`. Must end in `.tar.gz`; output **cannot overwrite** an existing file. |
| `--host root@HOST` | **Unset** | Required by `deploy-cache`, `deploy-asp` and `deploy-all`. Only `root` is accepted as SSH username. |
| `--port NUMBER` | `22` | SSH port (1–65535). |
| `--identity FILE` | First existing `~/.ssh/id_ed25519`, then `id_rsa`, then `id_ecdsa` | Local SSH private key; must be readable/parseable without an interactive passphrase prompt. |
| `--known-hosts FILE` | `~/.ssh/known_hosts` | Host-key verification file. Host keys must already be pinned; there is **no unsafe bypass**. |

`--platform` and `--firmware` are selectors rather than instructions to extract unsupported firmware automatically. `--refresh-cache` with a backup explicitly rescans the original images; with a local OTA it rereads the local ZIP. The CLI does not have a separate general-purpose temporary-directory option. Download working files are created within the private cache's `ota-images/` only for network downloads.

## Files and retention

**Desktop default** (when `XDG_CACHE_HOME` is unset):

```text
$HOME/.cache/frankenasp/asp-extractor/
└── asp-installer-cache/
    ├── PLATFORM-fireos-FIRE_OS_VERSION-build-AMAZON_BUILD/
    │   ├── manifest.json     # firmware/build/source details and internal schema
    │   ├── SHA256SUMS        # cryptographic inventory of factory files
    │   └── files/           # required system/ and vendor/ dependency closure
    ├── PLATFORM-AMAZON_BUILD/  # possible if Fire OS version is unavailable
    ├── ...-variant-N/        # same build, different factory file contents
    ├── .cache.lock          # private concurrent extraction lock
    └── ota-images/          # only OTA downloads performed by the tool
```

With `XDG_CACHE_HOME` set to an absolute directory, the root is instead `$XDG_CACHE_HOME/frankenasp/asp-extractor/asp-installer-cache/`. Use `--cache-dir` to override either default. Each extraction records an internal schema marker in `manifest.json`, **not** in the directory name. File content and checksum inventory, not simply a different full-OTA/backup SHA-256, determine duplicates for the same platform/build/Fire OS. The same source can be reused without another partition scan; requesting a different source may cause a temporary extraction and comparison, but only genuinely different factory content is retained as another set.

A local OTA stays wherever the user placed it. Backup ext4 images likewise remain untouched and are not copied to cache; the tool no longer writes a source-path index. Completed **tool-downloaded** OTA images are retained in `ota-images/` by default. `list` shows factory sets and available downloads, not a second entry for each earlier input source. The actual installed runtime is **not** stored in the desktop cache.

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

Go **1.26.9** is required for the project build. From the repository root:

```sh
cd common-tools/asp-extractor
GO=/path/to/go1.26.9/bin/go ./build.sh
GO=/path/to/go1.26.9/bin/go ./test.sh

# Substitute the actual versioned filename returned by build.sh.
./package.sh dist/asp-extractor-vVERSION-linux-amd64
```

`build.sh` emits `dist/asp-extractor-vVERSION-linux-amd64`; `package.sh` creates the versioned `.tar.gz` containing the executable, LICENSE, short README and this full guide. The build uses the existing `common-tools/asp-installer` firmware/parser/cache source files in a **temporary** build directory instead of maintaining duplicate parsing code. The produced Linux x86_64 binary is statically linked (`CGO_ENABLED=0`) and needs no Go installation, system SSH/SCP utilities, mounts or native ext4 drivers at runtime. Race tests use a C toolchain. Building/packaging does not publish a release or deploy anything.

## Input compatibility and safety

Supported OTA formats are compatible **full** Android block OTA ZIPs using supported transfer-list operations and partition layouts, plus supported full file-tree ZIPs. Incremental/delta OTA images, unknown partitions and `payload.bin`-only packages are rejected. Local ZIPs are limited to **2 GiB**. Backup extraction supports the raw ext4 directory layouts above, with no host filesystem mount required.

The cache validates owner/modes, absence of unexpected files and symlinks, manifest and **all** artifact hashes before reuse. SHA-256 checks do not establish official Amazon firmware authenticity; you must verify the original source independently. The inherited HTTP downloader does **not** perform normal HTTPS certificate/hostname checks. SSH delivery requires a pinned host key and verified transfers; there is no bypass for SSH host-key verification.

## Desktop OS extension

Linux x86_64 is the current reference host. Host-specific implementation is isolated under `common-tools/asp-extractor/internal/hostos/` with the existing `linux/` adapter and `select_linux.go` build selector. The shared CLI, parser, ELF closure, cache semantics and SSH protocol can be reused by new platforms, but secure Linux filesystem locking/publication operations need corresponding OS adapters. **macOS and Windows implementations do not exist yet** and must be implemented and tested independently; the current binary cannot be claimed to support them.

For Echo runtime installation, updates, rollback, maintenance and storage layout, read [ASP-INSTALL.md](ASP-INSTALL.md). For release versioning/publishing policy, read [RELEASING.md](RELEASING.md).
