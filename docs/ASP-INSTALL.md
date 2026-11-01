# FrankenASP V3 — installation and maintenance

`asp-installer` is the native ARM32 Linux installer for FrankenASP V3 on supported TECHO5 Echo devices. It extracts the required **original factory ASP files** from an owner-provided Amazon OTA image, maintains a verified firmware cache, and manages the installed external ASP runtime.

**Supported devices:** Echo Show 8 (2nd generation, `athena`) and Echo Spot (1st generation, `rook`). Builds and firmware caches are platform-specific. No other Echo model is supported or implied.

The FrankenASP release package contains the installer, its Linux launcher, the Bionic helper, and platform configuration. **It does not include Amazon firmware, proprietary libraries, calibration, or factory runtime images.** You must provide a compatible OTA or use a previously verified extraction cache. The installed runtime and factory cache are separate from the TECHO5 host binary.

## Requirements and package layout

- A supported Echo running TECHO5 with root shell access and the compatible External ASP host integration.
- The matching versioned `frankenasp-v<version>-athena-arm32.tar.gz` or `frankenasp-v<version>-rook-arm32.tar.gz` package, built for the target platform.
- A compatible **full** Amazon OTA ZIP, unless the required extracted firmware cache already exists.
- Sufficient free storage for a new OTA download, extracted cache, and any requested runtime backup.

On the Echo, unpack the archive and place **the contents of its `frankenasp-athena/` or `frankenasp-rook/` directory** in:

```text
/data/techo5-linux/asp-installer-v3/
├── asp-installer
├── LICENSE
└── assets/
    ├── manifest.json
    ├── external-asp
    ├── asp1-child
    └── channel-map        # Athena only
```

When updating the **installer package**, completely replace the previous contents of `asp-installer-v3/`. Do **not** remove or overwrite `/data/techo5-linux/external-asp/` (the deployed runtime) or `/data/techo5-linux/asp-installer-cache/` (the verified factory cache) as part of package replacement.

All examples below are run as root from:

```sh
cd /data/techo5-linux/asp-installer-v3
```

## Quick start

**Before install, update, rollback, or uninstall, disable External ASP in TECHO5 settings.** The installer does not change the TECHO5 selector or persistence configuration. Re-enable External ASP manually after checking the installed runtime.

```sh
# Inspect an owner-provided local full OTA and populate/reuse the cache:
./asp-installer inspect --ota /data/techo5-linux/update.bin

# Alternatively use a user-selected OTA URL:
./asp-installer inspect --ota-url 'https://example.org/firmware/update.bin'

# Display installed runtime and all cached firmware/OTA files:
./asp-installer status

# Install from an existing, unique, verified cache (no OTA argument needed):
./asp-installer install --yes

# Or select a specific factory firmware build:
./asp-installer install --firmware PS75741-7080 --yes

# After installation:
./asp-installer status
./asp-installer check --firmware PS75741-7080
```

`install` requires `external-asp/` to be absent. If there is a recognized existing installation, use `update`, not `install`. Any mutating command requires **`--yes`**.

## Command reference

| Command | Purpose |
| --- | --- |
| `version` | Show the version of the **running installer**. |
| `status` | Show installed runtime metadata, saved generations, backup count, checked factory caches, and available OTA files. Does **not** establish that the helper is running or selected in TECHO5. |
| `inspect` | Validate/reuse or extract factory files into the cache, without changing the installed runtime. |
| `check` | Compare an existing installed FrankenASP runtime with the selected factory cache and release assets; report whether an update is required. |
| `check-vendor` | **Athena only:** compare required factory files against live `/vendor` and report whether a live-vendor runtime is usable. |
| `install --yes` | Install a new runtime, only if `external-asp/` does not exist. |
| `update --yes` | Compare, optionally back up, and atomically replace a recognized existing runtime, retaining its previous generation. |
| `rollback --generation NAME --yes` | Restore a listed saved runtime generation, retaining the replaced generation and creating a backup. |
| `uninstall --yes` | Remove the recognized FrankenASP runtime and installer-managed generations; preserve factory caches and, by default, backup archives. |

**Output format:** all commands print readable text by default. Use `--json` on the command to request a structured JSON result. The aliases `status-json`, `check-json`, `check-vendor-json`, and the corresponding `*-json` forms are also accepted. Progress messages, startup version and total elapsed time are printed to **stderr**; JSON results go to **stdout** only.

Each command prints the running installer's version in its startup banner (for example, `asp-installer v3.1`); `version` and `version --json` report it explicitly. The installed FrankenASP runtime revision and Amazon firmware build are separate values.

## Options

| Option | Applies to / behavior |
| --- | --- |
| `--ota FILE` | Select a local full OTA ZIP for `inspect`, `check`, `check-vendor`, `install`, or `update`. The original file is never deleted. |
| `--ota-url URL` | Download a selected full OTA ZIP instead of using a local file. Mutually exclusive with `--ota`. |
| `--firmware NAME` | Select a verified extracted cache by Amazon build, unambiguous Fire OS version, or exact cache directory name. Cannot be combined with `--ota` or `--ota-url`. |
| `--cache-dir ABSOLUTE_PATH` | Override the factory extraction/OTA cache root (default `/data/techo5-linux/asp-installer-cache`). Also accepted by `status`. |
| `--assets DIR` | Override the packaged asset directory (default `./assets`). |
| `--platform athena\|rook` | Optional explicit platform check; must match the packaged release manifest. |
| `--vendor-mode auto\|live\|bundled` | `auto` (default) selects live `/vendor` on Athena only when verified and read-only, otherwise a bundled private copy. Rook always uses `bundled`. |
| `--ota-search-dir DIR` | Optional directory to search for previously saved local OTA candidates (default: `<cache-dir>/ota-images`). This option does **not** control download or temporary file placement. |
| `--tmp-dir DIR` | Deprecated alias of `--ota-search-dir`, retained for compatibility. |
| `--refresh-cache` | Force a fresh download/extraction instead of cached reuse; requires an explicit `--ota` or `--ota-url`. |
| `--delete-downloaded-ota` | With `--ota-url`, delete the newly downloaded OTA after the command rather than retaining it. Does not delete previously saved/local OTA images. |
| `--force-no-backup` | `update` only: skip the archive backup; the previous runtime generation is still retained. |
| `--restart-techo5` | Mutations only: permit the guarded restart of a verified PID1-supervised TECHO5 daemon if required to release runtime references. |
| `--purge-backups` | `uninstall` only: also remove recognized installer-created backup archives. Does **not** remove the factory cache. |
| `--generation NAME` | Select a named generation from `status` for `rollback`. |
| `--yes` | Explicit confirmation required for `install`, `update`, `rollback`, and `uninstall`. |
| `--json` | Return machine-readable JSON on stdout for any command. |

`check-vendor` and `--vendor-mode live` are **not supported on Rook**. A cached firmware can be used without its complete OTA image as long as the extracted files verify and match the packaged helper; if several caches qualify, specify `--firmware` rather than allowing an ambiguous selection.

## Factory firmware cache and OTA retention

The persistent cache is independent of the installed runtime. Typical names are:

```text
/data/techo5-linux/asp-installer-cache/
├── athena-fireos-7574.1-build-PS75741-7080/
│   ├── manifest.json
│   ├── SHA256SUMS
│   └── files/
├── rook-fireos-5.5.6.9-build-690918120/
│   ├── manifest.json
│   ├── SHA256SUMS
│   └── files/
└── ota-images/
    ├── athena-fireos-7574.1-build-PS75741-7080.bin
    └── rook-fireos-5.5.6.9-build-690918120.bin
```

These names are examples, not bundled files. A distinct OTA or helper with the same build label gets a `-variant-N` cache directory. Cache reuse verifies the source/helper identity and every extracted file against SHA-256 records; cached `ready` means **verified files**, not that FrankenASP is active. `status` shows full paths, Fire OS version, Amazon build, file counts, and OTA integrity status. An original OTA file is **not required** once a compatible extraction is cached.

Incomplete OTA downloads are created as private temporary files inside `<cache-dir>/ota-images/` and removed after failed transfers; completed downloaded images are retained there under private, readable names. No temporary OTA files are created directly in `/data/techo5-linux/` by default. The OTA-search option only controls where previously saved local candidates are looked up.

OTA downloads are retained in `ota-images/` by default. A local OTA can be copied to that private archive when sufficient disk space exists; if not, the verified extraction remains usable and the installer reports a warning. `uninstall` does **not** remove `asp-installer-cache/` or any OTA images. The optional `--purge-backups` removes **backups**, not the factory cache.

Amazon build and Fire OS marketing version are separate values. The installer reads the marketing version from compatible OTA build properties, not from the Android OS version. Selecting `--firmware 5.5.6.9` works only if that version uniquely identifies a compatible cache; otherwise specify its exact Amazon build or `-variant-N` name.

## Update, rollback, and uninstall

```sh
# First disable External ASP in TECHO5 settings.
./asp-installer check
./asp-installer update --yes
./asp-installer status

# To return to a previously saved generation:
./asp-installer rollback --generation '.external-asp-generation-v3-YYYYMMDDTHHMMSSZ-XXXXXXXXXXXX' --yes

# To remove the installed helper and saved generations, retaining cache/backups:
./asp-installer uninstall --yes

# Only when backup archives should also be removed:
./asp-installer uninstall --yes --purge-backups
```

The installer validates its **own** runtime files, lock file, cache, staging trees, and backup content; it does not impose ownership or mode requirements on pre-existing Android/TECHO5 parent directories. Mutations verify recognized runtimes and active process references, fail closed on unexpected files or pinned resources, and use guarded rename operations. Update normally creates a private archive backup and retains the former runtime as a generation. Rollback saves the replaced generation. Uninstall preserves the factory cache. Interrupted operations may require manual inspection rather than automatic destructive cleanup.

## Compatible OTA images and disk space

Supported inputs are full Android block OTA ZIPs with known system/vendor ext4 layouts (including the Athena v4 and Rook v3 transfer-list formats), or supported full file-tree ZIPs. Incremental/patch-only OTA images, `payload.bin`, unsupported transfer operations and unknown layouts are rejected. The extractor reads only the required ELF dependency closure; full partition images are not reconstructed.

Disk preflight accounts for extraction cache, staged runtime, backup, OTA download, and a **64 MiB safety reserve per filesystem**. The 168 MiB extraction bound and separate 168 MiB staging bound are not estimates of the OTA size. For a URL download, the installer adds the actual HTTP Content-Length when available and checks free space during streaming.

**Source trust:** the OTA is owner-selected. Its SHA-256 protects cache integrity, **not vendor authenticity**. OTA signatures are not verified, and the download implementation accepts HTTPS certificates without normal certificate/hostname verification. Use only a source you trust and verify firmware provenance independently. Downloads have a 2 GiB size cap.

## Building the installer package

With Go **1.26.9** and platform-specific ARM32 Linux launcher/Bionic helper build outputs available, from `common-tools/asp-installer/`:

```sh
GO=/path/to/go1.26.9/bin/go ./build-release.sh \
  athena /path/to/launcher-arm32 /path/to/asp1-child \
  /path/to/athena-output /path/to/approved-channel-map

GO=/path/to/go1.26.9/bin/go ./build-release.sh \
  rook /path/to/launcher-arm32 /path/to/asp1-child \
  /path/to/rook-output
```

The script produces the matching `frankenasp-v<version>-<platform>-arm32.tar.gz` archive without proprietary Amazon files. Release versions are resolved automatically. Platform launcher and Bionic helper sources and build scripts are under `platforms/athena/` and `platforms/rook/`.

For source validation:

```sh
cd common-tools/asp-installer
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```
