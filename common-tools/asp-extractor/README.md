# asp-extractor — Linux x86_64

Desktop companion to the embedded FrankenASP V3 `asp-installer`. It prepares
**owner-local** factory ASP caches using the **same** selective OTA reader,
ext4 reader, ARM ELF closure and SHA-256 cache code as the embedded installer,
without copying full OTA images to an Echo.

**Supported host:** Linux x86_64 only. **Targets:** Athena (Echo Show 8 Gen 2)
and Rook (Echo Spot 2017). No incremental OTA support.

## Build and tests

Requires Go 1.26.9 at build time (and a C toolchain for race tests).

```sh
cd common-tools/asp-extractor
GO=/path/to/go1.26.9/bin/go ./build.sh
GO=/path/to/go1.26.9/bin/go ./test.sh
# Package only this project's own executable and text (no Amazon files):
./package.sh dist/asp-extractor-v<VERSION>-linux-amd64
```

The build emits `dist/asp-extractor-v<VERSION>-linux-amd64`, with the
repository's existing release-policy resolver. The binary is statically
linked (`CGO_ENABLED=0`); no Go, glibc, SSH/SCP, mount or image utilities
are needed at runtime. Untagged builds include the UTC suffix defined in
`docs/RELEASING.md`. **No new GitHub Release is implied by building.**

The original `asp-installer` is a `package main`. To reuse its source rather
than duplicate the parsers, the build script copies its Go source files *verbatim* (excluding its CLI entrypoint and tests)
into a disposable build directory, together with the desktop CLI and OS
adapter. No parser source is duplicated in Git; there is one compiled copy
per binary. The embedded installer remains independently buildable using
its original command. This composition is deliberate and can later be
replaced with a narrow shared package if that becomes justified.

## Local storage

Default: `$XDG_CACHE_HOME/frankenasp/asp-extractor/asp-installer-cache/`,
or `$HOME/.cache/frankenasp/asp-extractor/asp-installer-cache/`.
Override with `--cache-dir ABSOLUTE_PATH` on any command.

The cache has the embedded installer's exact structure:
`<platform>-<firmware>/manifest.json`, `SHA256SUMS`, `files/`,
plus optional archived original OTA files in `ota-images/`. It supports
multiple platform/firmware variants without replacing unrelated caches.
Re-extraction of a cache matching the supplied OTA is
skipped; all reused cache files are SHA-256 checked. Owner-selected backup
directory paths are indexed separately in the desktop-only
`asp-extractor-backups.json` next to the cache root, never inside a firmware
cache; `list` displays the links and reports whether the source remains
present. This index stores only local paths and references, not backup bytes.

## Examples

```sh
E=./dist/asp-extractor-v<version>-linux-amd64
ATHENA=/path/to/frankenasp-v<VERSION>-athena-arm32.tar.gz
ROOK=/path/to/frankenasp-v<VERSION>-rook-arm32.tar.gz

# Extract factory ASP from a full local OTA; platform detected automatically.
"$E" extract --ota update-kindle-athena-7080.bin
"$E" extract --ota update-kindle-rook.bin

# Or from TECHO5 raw ext4 backup partition images, even in a serial-number folder.
"$E" extract --backup-dir /backups/G070RQ13843305B5
"$E" extract --backup-dir /backup/athena/partitions

"$E" list
"$E" --platform athena verify --firmware PS75741-7080

"$E" create-bundle \
  --firmware PS75741-7080 \
  --asp-release "$ATHENA" --output ./athena-private-bundle.tar.gz

# SSH host keys MUST already be pinned in ~/.ssh/known_hosts.
"$E" --platform athena deploy-cache --host root@10.0.0.2 --firmware PS75741-7080
"$E" deploy-asp --host root@10.0.0.2 --asp-release "$ATHENA"
"$E" deploy-all --host root@10.0.0.2 \
  --firmware PS75741-7080 --asp-release "$ATHENA"
```

Flags for SSH: `--identity PATH` (default: an unencrypted user SSH private
key), `--known-hosts PATH` (default: `~/.ssh/known_hosts`), `--port PORT`
(default 22). No insecure host-key fallback is provided.

`extract` automatically determines the model from raw backup partition layout
or a local full OTA's build fingerprint and partition structure; `--platform`
is optional and must agree with the detected model. For `--ota-url`, specify
`--platform` (the remote file cannot be examined before download).
`verify` and `deploy-cache` require `--platform`. For operations using a
FrankenASP release, supply `--asp-release FILE`, which is a path to a
versioned local `.tar.gz` archive, **not** a version number; its embedded
manifest determines the platform. `--firmware` is optional when the selected
cache is unambiguous; otherwise use the **exact cache directory name**.
Factory caches use the internal `ASP1-EXTRACTED-CACHE-V2` manifest schema,
independent of the helper; schema versions are **not** part of directory names.
Existing V1 caches are not selected as V2 caches. Obsolete caches must be
re-extracted or explicitly replaced; automatic deletion is not performed.

### Backup layouts

Supported **directory** layouts, using native Go ext4 parsing (no mount):

- Athena: `p21-system.img` and `p12-vendor.img` in the selected directory.
- Rook: `system.img` in the selected directory (vendor tree under system).

These are the actual TECHO5 backup layouts verified for this release.
Individual ext4 images inside arbitrary tarballs, whole-device eMMC dumps,
unknown layouts and incremental OTA are not claimed as supported.
The backup's actual build properties determine its firmware identity.
The large source images are only read on PC, never included in bundle or
sent to Echo. Firmware provenance is owner-controlled: SHA-256 verifies cache
integrity but does not authenticate Amazon firmware. The inherited embedded
OTA HTTP downloader does **not** validate HTTPS certificates or hostnames;
for network downloads, independently verify the supplied image provenance,
or prefer a locally authenticated OTA.

### Bundle

A private bundle requires a FrankenASP release declaring the new cache format
`ASP1-EXTRACTED-CACHE-V2` in its assets manifest. Existing v3.2 releases
do not qualify. `deploy-asp` and `deploy-all` enforce the same requirement.

A private bundle (`FRANKENASP-EXTRACTOR-BUNDLE-V2`) contains one verified cache, a JSON manifest with platform,
firmware, release version and SHA-256 inventory, and the **unchanged original**
versioned FrankenASP release archive. It contains no full OTA by default.
The output path is no-clobber. **Bundles contain proprietary Amazon
components and must not be uploaded to public GitHub Releases.**

### Deployment boundaries

The only permanent target directories are:

- `/data/techo5-linux/asp-installer-cache/` — one selected verified cache.
- `/data/techo5-linux/asp-installer-v3/` — the embedded installer package.

Transfers stream and re-read/check SHA-256 using Go SSH and portable remote
`sh/cat/mkdir/chmod/mv/rm` operations. Staging is private and temporary.
Existing cache directories are verified, never overwritten. The
installer's previous package is discarded only after the replacement package
has been completely transferred and verified; it is not backed up.

**No `deploy-*` command runs the embedded install/update command, touches
`external-asp/`, changes TECHO5 settings, restarts `echod` or persistence.**
After delivery the CLI prints the current installer's **manual** commands:
`./asp-installer status`, then `install --yes` for a new runtime or
`update --yes` for an existing one, after disabling External ASP manually.
For `deploy-cache` and `deploy-all` the suggested `--firmware` value is the
original Amazon firmware build (such as `690918120`), **not** the verbose
cache directory name. If multiple verified cache variants have the same build,
the embedded installer requires the exact cache directory name as an explicit
selection; the delivery output also shows that name.

## Desktop OS extension

Host-specific operations belong to `internal/hostos/<os>/` and a
`select_<os>.go` file with Go build tags. Only the Linux adapter exists;
it implements Linux/XDG cache-root resolution. The shared CLI, archive,
OTA/ext4/ELF, cache and Go SSH logic are intentionally OS-independent at
the API level. Before macOS or Windows builds, isolate the embedded
Linux-only cache lock, filesystem-stat and secure publish primitives
behind the corresponding OS adapters; implement only the needed OS
behavior and test on each target. No platform compatibility is implied yet.
