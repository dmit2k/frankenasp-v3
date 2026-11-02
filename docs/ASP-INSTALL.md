# FrankenASP V3 — on-device installation and maintenance

`asp-installer` installs and maintains the FrankenASP V3 **factory audio DSP runtime** on a supported Echo running TECHO5. It is a standalone ARM32 Linux executable, distinct from both the desktop [`asp-extractor`](ASP-EXTRACTOR.md) and the TECHO5 `echod` host. It can extract Amazon's original factory DSP dependencies from an owner-supplied **full OTA ZIP directly on the Echo**, or use a previously verified extraction prepared on the device or transferred from a desktop.

**Tested target models:** Echo Show 8 (2nd generation, `athena`) and Echo Spot (1st generation, `rook`) only. Use the release built for the actual model; support for other Echo devices is not implied. The installer package contains only FrankenASP-owned code and configuration: **no Amazon OTA, proprietary ASP binaries, calibration, or extracted factory files**. Those must come from the owner's firmware or backup.

**Choose a workflow:** If the OTA is already on the Echo or can be downloaded to it, use [direct installation](#first-installation-from-an-ota-without-asp-extractor). If extraction is done on a Linux PC, use the [desktop delivery workflow](#first-installation-after-desktop-extraction) and [ASP-EXTRACTOR.md](ASP-EXTRACTOR.md). Both lead to the same on-device `install` / `update` operations.

## Prerequisites and installation boundary

- Root shell on one of the supported Echo devices, running a TECHO5 build with [External ASP V3 support](https://github.com/dmit2k/techo5/tree/experimental/external-asp-v3-pr-cleanup). The integration is proposed upstream in [PR #407](https://github.com/HuskerMinion/techo5/pull/407) and is not included in standard upstream releases while that PR remains unmerged.
- The **matching** `frankenasp-vVERSION-PLATFORM-arm32.tar.gz` release archive (`PLATFORM` is `athena` or `rook`). The filename's release version is **not** an Amazon firmware build.
- Either a complete compatible Amazon OTA ZIP selected by the owner or a verified, matching factory-extraction cache. Incremental/patch-only OTA packages are not supported.
- Enough writable storage for extraction, an optional downloaded OTA and the required runtime staging/backup. See [Disk space and security](#disk-space-integrity-and-security).

**Before `install`, `update`, `rollback`, or `uninstall`, select Built-in / disable External ASP in TECHO5.** The installer does not change the TECHO5 selector, its persistent configuration or the running `echod` service by default. Verify the operation, then re-enable External ASP yourself. The existence of `external-asp/` does **not** mean it is currently selected or active; use TECHO5's own ASP status for that.

The release archive contains a single `frankenasp-PLATFORM/` directory. On the Echo, its **contents** must reside at:

```text
/data/techo5-linux/asp-installer-v3/
├── asp-installer             # ARM32 Linux executable
├── LICENSE
└── assets/
    ├── manifest.json        # release platform and cache schema
    ├── external-asp          # FrankenASP Linux launcher
    ├── asp1-child            # Bionic factory DSP helper
    └── channel-map           # athena only; absent on rook
```

The command examples use `cd /data/techo5-linux/asp-installer-v3` because `--assets` defaults to the **relative** directory `./assets` (relative to the shell's current working directory). Replacing only the installer package must not delete `external-asp/` or `asp-installer-cache/`.

## First installation from an OTA without asp-extractor

These are the minimal commands for a **new** installation, assuming `asp-installer-v3/` and `external-asp/` are not present, a full OTA is available on the PC, and TECHO5 is already installed. Substitute the target's actual package filename and SSH address; `VERSION` is a placeholder, not a literal release number.

**On the PC**, copy the matching release and the owner-provided OTA to the Echo:

```sh
scp frankenasp-vVERSION-rook-arm32.tar.gz full-ota.zip \
  root@ECHO_HOST:/data/techo5-linux/
```

**On the Echo** (`root` shell; external ASP disabled):

```sh
cd /data/techo5-linux

tar -xzf frankenasp-vVERSION-rook-arm32.tar.gz
mv frankenasp-rook asp-installer-v3
cd asp-installer-v3

./asp-installer install --ota /data/techo5-linux/full-ota.zip --yes
./asp-installer status
```

For Athena, substitute the Athena release filename and extracted `frankenasp-athena` folder. The `install --ota` command **extracts and installs in one invocation**; a preliminary `inspect` is optional, not required. The full local OTA is read in place, not copied to `asp-installer-cache/`, and is not deleted by the installer. The operator manages that original file independently.

**Alternative when the OTA is not on the Echo:** instead of `--ota`, provide your own approved download URL as `--ota-url "$OTA_URL"` in `install`; you must set `OTA_URL` to the actual complete firmware URL beforehand. In this case the installer downloads the OTA and, by default, retains it under its private cache; `--delete-downloaded-ota` opts out of retention for the new download. The installer does not authenticate the Amazon provenance of an arbitrary URL. If the cache already contains suitable files, no OTA is needed at all.

`install` is strictly **first-install only**: it refuses to run if `/data/techo5-linux/external-asp/` exists. For an already installed runtime use `update`, never a second `install`.

## First installation after desktop extraction

When `asp-extractor` has already transferred the installer **and** a verified factory cache with `deploy-all`, no original OTA is needed on the Echo. The desktop transfer does not activate anything. In a root shell on the Echo, with External ASP disabled:

```sh
cd /data/techo5-linux/asp-installer-v3
./asp-installer status
./asp-installer install --firmware YOUR_AMAZON_BUILD --yes
./asp-installer status
```

`YOUR_AMAZON_BUILD` is a placeholder for the **actual Amazon firmware build ID** used during desktop extraction. If there is exactly one verified cache for the release platform, `--firmware` can be omitted. If two genuinely different sets share a build, use the exact cache directory name reported by the tools (including `-variant-N`). If a runtime is already present, use `update --firmware ... --yes` instead. See [ASP-EXTRACTOR.md](ASP-EXTRACTOR.md#prepare-and-deliver-an-ota-using-linux).

## Command reference

Run all commands from the installation directory as described above. `install`, `update`, `rollback`, and `uninstall` require `--yes`; the first two also require root and correct release assets. Other commands do not mutate the installed runtime.

| Command | What it actually does |
| --- | --- |
| `version` | Prints the executing installer version; this is distinct from the installed FrankenASP revision and Amazon firmware build. |
| `status` | Reports installed runtime files/revision/vendor mode, **counts** of saved generations and backups, incomplete operations, verified extraction sets and saved OTA images. Use `status --json` to obtain exact saved generation names for rollback. **Does not confirm that the helper is running or selected in TECHO5.** |
| `inspect` | Checks an existing firmware cache or reads/extracts a full OTA into it. Does not install or change the runtime. |
| `check` | Compares a recognized installed runtime against selected release assets and extracted factory dependencies, reporting whether an update is needed. |
| `check-vendor` | Athena only: compares required factory vendor files with live `/vendor` and assesses whether `live` vendor mode is usable. |
| `install --yes` | Creates a runtime only if the normal `external-asp/` installation is absent. |
| `update --yes` | Validates the installed runtime; compares contents; when different, creates an archive backup by default, atomically exchanges the runtime and retains the former one as a generation. An identical runtime is left unchanged. |
| `rollback --generation NAME --yes` | Restores one saved generation, archives the current runtime, and retains the replaced runtime as another generation. |
| `uninstall --yes` | Removes recognized installed runtime and managed generations; preserves extracted firmware cache and, by default, installer backup archives. |

### Complete option reference

The underlying CLI uses standard Go command-specific flags: put options **after the subcommand**, e.g. `./asp-installer inspect --ota FILE`. Options with no specified value below use the indicated defaults.

| Option | Allowed context | Default / meaning |
| --- | --- | --- |
| `--ota FILE` | `inspect`, `check`, `check-vendor`, `install`, `update` | **Unset.** Path to an owner-provided local **full OTA ZIP** on the Echo. Read in place; never copied to the OTA archive or deleted. Exclusive with `--ota-url` and `--firmware`. |
| `--ota-url URL` | Same five firmware-using commands | **Unset.** Download a full OTA from an explicitly supplied HTTP(S) URL. Exclusive with `--ota` and `--firmware`. Download retained by default. |
| `--firmware NAME` | Same five firmware-using commands | **Unset.** Selects an existing extraction by Amazon build, uniquely matching Fire OS version, or exact directory name. With no OTA/source option, omission is allowed **only if precisely one compatible cache** exists for the release platform. Full OTA not needed. |
| `--platform athena\|rook` | Commands using release assets | **Unset; inferred from `./assets/manifest.json`.** If explicitly set, must agree with the release. `check-vendor` works only on Athena. |
| `--assets DIR` | Firmware-using commands | `./assets` (relative to current working directory). Must contain the correct release manifest and launcher/helper/channel map as appropriate. |
| `--cache-dir DIR` | `status` and firmware-using commands | `/data/techo5-linux/asp-installer-cache` on Echo (derived from the fixed runtime deployment parent). Must be an absolute path, not `/`. |
| `--ota-search-dir DIR` | Firmware-using commands | `<cache-dir>/ota-images`. **Only** a scan location for previously saved local OTA candidates when resolving a URL; not a download or temp directory. Requires an absolute, non-root path. |
| `--tmp-dir DIR` | Firmware-using commands | **Unset.** Deprecated alias for `--ota-search-dir`, not a temp path. Cannot be combined with `--ota-search-dir`. |
| `--refresh-cache` | Firmware-using commands with `--ota` or `--ota-url` | `false`. Reprocesses the explicit source rather than accepting an early cache hit. Identical extracted factory files are still deduplicated; a different set is kept as a variant. Not permitted with cache-only selection. |
| `--delete-downloaded-ota` | Commands with `--ota-url` | `false`. Requests cleanup of the newly downloaded OTA after the operation. Does not remove an earlier archived OTA or a user-supplied local file. Requires `--ota-url`. |
| `--vendor-mode auto\|bundled\|live` | Firmware-using commands | `auto`. On Athena, `auto` evaluates the live read-only `/vendor` and falls back to bundled factory vendor data as appropriate. Rook requires `bundled`; explicit `live` is rejected. `check` uses installed mode when set to `auto`. |
| `--force-no-backup` | `update` only | `false`. Skip creation of the compressed archive backup; the previous runtime generation is still retained after an actual replacement. |
| `--restart-techo5` | `install`, `update`, `rollback`, `uninstall` | `false`. Permits a **guarded** restart of a verified PID1-supervised TECHO5 daemon if necessary to release pinned runtime references. Not a general service restart switch. |
| `--purge-backups` | `uninstall` only | `false`. Also remove installer-created backup archives; never purge factory extraction caches. |
| `--generation NAME` | `rollback` only | **Required** for rollback. Exact saved generation name from the status information; not a file path. |
| `--yes` | `install`, `update`, `rollback`, `uninstall` | `false`. Mandatory explicit authorization for each mutating command. Does not bypass safety checks. |
| `--json` | Every command, including `status`, `version` | `false`. Structured JSON on stdout rather than human summary. The corresponding `COMMAND-json` alias is also recognized. |

`status` accepts only `--cache-dir` and `--json`; `version` accepts only `--json`. `uninstall` and `rollback` also do not take OTA sources. In particular, `--ota`, `--ota-url`, and `--firmware` are mutually exclusive ways of selecting a firmware source for the five firmware-using commands. `--refresh-cache` requires an explicit OTA.

Every invocation prints `asp-installer v...` and `asp-installer: Total elapsed: ...` to **stderr**, including on error. Normal results appear as readable text on stdout; with `--json`, stdout contains structured JSON (progress and elapsed time stay on stderr). `install/update` summaries report the deployment, rather than internal OTA format or cache-reuse diagnostics. No operation is automatically activated in TECHO5.

## What is stored, and where

```text
/data/techo5-linux/
├── asp-installer-v3/              # extracted FrankenASP release: installer and assets
├── asp-installer-cache/          # verified factory artifact sets (separate from runtime)
│   ├── PLATFORM-fireos-FIRE_OS_VERSION-build-AMAZON_BUILD/
│   │   ├── manifest.json         # metadata, internal schema, source identity
│   │   ├── SHA256SUMS            # per-file hashes
│   │   └── files/               # selected factory files in system/ and vendor/ paths
│   ├── PLATFORM-AMAZON_BUILD/    # possible when Fire OS marketing version unavailable
│   ├── ...-variant-N/            # only if factory file contents genuinely differ
│   ├── .cache.lock               # private cache lock
│   └── ota-images/              # downloaded OTA archives (optional; not local input copies)
├── external-asp/                 # installed runtime used by TECHO5
├── .external-asp-generation-v3-.../  # previous managed runtime generation(s)
├── asp-installer-backups/       # compressed installer backup archives (after updates/rollbacks)
└── .asp-installer-v3.lock       # private mutation lock
```

- The **release package** (`asp-installer-v3`) contains the installer/launcher/helper, but not factory Amazon files. Replacing that package does not itself install or update `external-asp/`.
- The **factory cache** is persistent and independent of the installed runtime. The internal manifest schema name is not part of the directory name. Reusing an extraction verifies `manifest.json`, `SHA256SUMS`, file contents and safe ownership/layout. A `ready` cache means its **files** verify, not that the audio helper is active.
- The cache is **content-deduplicated** within each platform, Amazon build and Fire OS version: different source images, including OTA and backup, can yield the same factory file inventory and then use a single cache directory. If files differ, a numbered `-variant-N` is kept rather than silently overwriting distinct factory content. `--firmware` build or Fire OS values must resolve uniquely.
- A **local OTA** passed with `--ota` stays at its original path and is not copied into `ota-images/`. When the installer **downloads** an OTA using `--ota-url`, completed archives are retained by default and temporary download files live under `ota-images/`. With `--delete-downloaded-ota`, that newly downloaded file is removed after use. Original OTA images are not needed once the extracted factory files are cached.
- The runtime and backup directories are **installer-managed**. On update, the previous installed tree becomes a saved generation; a compressed backup archive is created unless expressly skipped. Rollback archives the installed runtime and retains the replaced generation. `uninstall` removes managed runtime trees but leaves the cache and, unless `--purge-backups` is requested, backup archives.
- Interrupted operations may leave explicitly named staging or uninstall directories under `/data/techo5-linux/`; `status` reports incomplete operations and the installer refuses unsafe automatic cleanup.

The desktop extractor stores its **own** cache by default on the PC. `deploy-cache` / `deploy-all` transfer one chosen verified factory set into the Echo's **fixed** `asp-installer-cache/`, without copying a full OTA or automatically installing the runtime.

## Maintenance examples

From the Echo's installer directory, after disabling External ASP in TECHO5:

```sh
# Inspect current state and compare against a selected factory build:
./asp-installer status
./asp-installer check --firmware YOUR_AMAZON_BUILD

# Update only if needed; installing again is not valid:
./asp-installer update --firmware YOUR_AMAZON_BUILD --yes
./asp-installer status

# Read exact saved generation names from the JSON generations array:
./asp-installer status --json
./asp-installer rollback --generation YOUR_SAVED_GENERATION_NAME --yes

# Remove all managed runtime generations, but retain factory files/backups:
./asp-installer uninstall --yes

# Alternative to the uninstall command above (not an additional step):
# also purge installer-created backup archives:
./asp-installer uninstall --yes --purge-backups
```

An `update` that finds no differences does not replace the runtime. A rollback requires a previously retained generation and a recognized current installation. `uninstall` cannot be used to remove arbitrary unmanaged directories.

## Disk space, integrity and security

Supported OTA inputs: known **full** Android block OTA ZIPs (including the supported block-transfer-list families for these models), or compatible full file-tree ZIPs. Unsupported layouts, incremental OTAs, unsupported operations and `payload.bin`-only packages are rejected. Extraction reads the required factory ELF/dependency closure; it does not reconstruct complete partition images for runtime installation.

The installer reserves capacity for cache extraction, a staged runtime, backups and (if requested) downloads, with a **64 MiB filesystem safety reserve**. Extraction/runtime planning bounds are separate from the actual OTA transfer size; for network downloads HTTP `Content-Length`, when provided, is considered and free space is checked while downloading. OTA input is limited to **2 GiB**. All source inputs must be trusted by the owner; SHA-256 verifies local integrity but **does not authenticate Amazon provenance**. OTA signature authenticity is not verified. The current inherited HTTP downloader does **not validate HTTPS certificates/hostnames normally**, so independently verify the download source (or use a verified local OTA).

Mutating operations require root, recognized installer-owned runtime files, safe private locks/staging, and checks for processes using the active helper. They fail on unexpected pinned resources rather than blindly removing them. `--restart-techo5` is opt-in and guarded. The installer does not impose its own ownership/mode policy on the Android/TECHO5-owned parent `/data/techo5-linux`.

## Building and testing the installer release

With **Go 1.26.9** and separately built, approved platform-specific launcher/Bionic helper binaries, from `common-tools/asp-installer/`:

```sh
GO=/path/to/go1.26.9/bin/go ./build-release.sh \
  athena /path/to/launcher-arm32 /path/to/asp1-child \
  /path/to/athena-output /path/to/approved-channel-map

GO=/path/to/go1.26.9/bin/go ./build-release.sh \
  rook /path/to/launcher-arm32 /path/to/asp1-child \
  /path/to/rook-output
```

`build-release.sh` produces a platform/version-named archive with no Amazon proprietary files. It does not deploy to an Echo. Tests:

```sh
cd common-tools/asp-installer
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```

Maintainer release policy: [RELEASING.md](RELEASING.md). Desktop preparation and delivery: [ASP-EXTRACTOR.md](ASP-EXTRACTOR.md).
