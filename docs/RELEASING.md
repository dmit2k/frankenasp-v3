# FrankenASP release maintenance

This document is for maintainers. Installation and owner-private factory extraction are documented in [ASP-INSTALL.md](ASP-INSTALL.md) and [ASP-EXTRACTOR.md](ASP-EXTRACTOR.md).

## Version policy

The repository-root `VERSION` contains the major/minor release baseline, such as `3.6`.

- A clean checkout at an **annotated** matching `vVERSION` release tag builds as `VERSION`.
- An untagged or dirty checkout builds as `VERSION.YYMMDDHHMMSS` (UTC).
- The complete version appears in all archive filenames; the factory Amazon build is unrelated.
- `common-tools/asp-installer/resolve-version.sh` and `version_test.sh` implement and verify this. Use `SOURCE_DATE_EPOCH` where identical build stamps are required across platforms.

## One Go module and universal V4 packaging

From the **repository root**, run the common verification suite and cross-build the two application targets with the supported Go toolchain (validated on Go 1.27.3, root `go.mod` minimum retained):

```sh
GOTOOLCHAIN=local GOFLAGS=-p=2 GOMAXPROCS=2 go test -race -count=1 ./...
GOTOOLCHAIN=local GOFLAGS=-p=2 GOMAXPROCS=2 go vet ./...
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 GOTOOLCHAIN=local \
  go build -trimpath -o ./asp-installer-armv7 ./common-tools/asp-installer
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=local \
  go build -trimpath -o ./asp-extractor-amd64 ./common-tools/asp-extractor/cmd
GOTOOLCHAIN=local go build -trimpath -o ./asp-release ./common-tools/asp-release/cmd
```

The root Go module and `internal/asp` implement shared factory-source readers, ELF parsing, verified cache selection and release validation. Desktop builds must never copy installer `*.go` files or link installer lifecycle/process mutation code.

The universal packager accepts **one platform codename at a time** plus an already built ARM32 launcher, Bionic child and optional FrankenASP-owned data resources. A new platform needs its native launcher/child and its own release contract; it must **not** require a new platform branch in generic installer/extractor/publisher processing.

```sh
./asp-release \
  --platform CODENAME \
  --version VERSION \
  --contract ./platforms/CODENAME/release-contract.json \
  --installer ./asp-installer-armv7 \
  --license ./LICENSE \
  --launcher ./PUBLIC-LAUNCHER-ARM32 \
  --child ./PUBLIC-CHILD-BIONIC \
  --out-dir ./RELEASE-OUTPUT \
  --data assets/RESOURCE=lib/rootfs/factory/RESOURCE:/path/to/public/resource

./asp-release --validate ./RELEASE-OUTPUT/frankenasp-vVERSION-CODENAME-arm32.tar.gz
```

`--data` is repeatable. Omit it when no optional resource is required. `--contract` supplies the platform's `runtime.vendor_modes` and ordered `runtime.library_search`; for the two historical codenames, the embedded descriptors also supply their release defaults. The compatibility wrapper `common-tools/asp-installer/build-release.sh PLATFORM LAUNCHER CHILD OUT_DIR [CHANNEL_MAP]` accepts the previous four/five positional arguments and packages **V4**. The existing Athena V1 channel map remains mandatory through Athena's descriptor. The current Rook helper does not consume a map, even if a V4 resource is declared.

The V4 schema is `ASP1-ASSETS-V4`, with exact `schema`, `platform`, `version`, `cache_schema` (`ASP1-EXTRACTED-CACHE-V2`), `arch` (`arm32`), `runtime.vendor_modes`, `runtime.library_search` and full `files` inventory. Each file has a role, canonical source path, positive byte size and SHA-256; runtime files also have a fixed/allowed installation destination. The entire release archive, including each file, filename, root, manifest and compressed tar framing, is revalidated by the **same parser** used by installer/extractor.

**Legacy V3 compatibility is mandatory:** preserve existing Athena/Rook `ASP1-ASSETS-V3` archives and their exact file sets, mandatory Athena V1 channel map, bundled-only Rook, original vendor/library policy and previously validated factory caches. Unknown V3 codenames lack a complete self-describing contract and are rejected; valid V4 codenames are not restricted by that legacy inventory. Invalid V4 must not fall back to V3.

## Publishing a tagged release

1. Update `VERSION`, release notes at `docs/releases/vMAJOR.MINOR.md`, and the platform-owned release definitions required for this release. Test native launcher/child builds in their own platform pipelines and run the full Go, cross-build and negative/security regression suite. Confirm the intended source commit and a clean Git tree.
2. Create an **annotated** `vMAJOR.MINOR` tag on that exact source commit. Do not rewrite an existing tag.
3. Produce a validated `frankenasp-vMAJOR.MINOR-CODENAME-arm32.tar.gz` for **each** supported release platform, using the reviewed native launcher and child. Produce the mandatory `asp-extractor-vMAJOR.MINOR-linux-amd64.tar.gz` using `common-tools/asp-extractor/build.sh` and `package.sh`. The desktop archive must contain `asp-extractor`, `LICENSE`, `README.md` and `ASP-EXTRACTOR.md`. The platform archives contain only explicitly approved FrankenASP-owned assets — **never** original Amazon firmware, `libasp.so`, OTA/backup images, private calibration or extracted factory caches.
4. Stage these archives in an independent `release-assets/vMAJOR.MINOR` branch with a newline-terminated `SHA256SUMS` enumerating the **exact** basename and SHA-256 of each archive, and the `COMMIT` and `TAG` metadata files containing the exact source commit and `vMAJOR.MINOR`. There must be **one desktop archive plus one or more platform archives**. No specific platform count is embedded in the publisher.
5. Locally verify the staged assets with the offline validator; supply the matching source tag and the built `asp-release` binary:
   ```sh
   python3 scripts/verify-release-inventory.py \
     --dir release-assets --tag vMAJOR.MINOR \
     --validator ./asp-release --output ./verified-release-assets.txt
   ```
   This checks the listed archives' exact presence/no extras, safe filenames and types, duplicate names, every SHA-256, each platform archive's V3/V4 contract, the mandatory desktop archive and its internal files. No wildcard upload of unverified files is allowed.
6. Push the staged assets branch, followed by the release source commit to `main`, and **only then** push its annotated tag. Pushes to `main` alone do not publish. The tag-triggered GitHub Actions workflow verifies `VERSION`, `TAG`, `COMMIT`, exact archive SHA/inventory and each archive again, then constructs the GitHub Release from **only** the validated filenames and `SHA256SUMS`.
7. Verify successful GitHub Actions completion, download/published hashes and release attachments (all platform archives, one desktop extractor, one `SHA256SUMS`). Delete the temporary release-assets branch after acceptance. Preserve the immutable source tag.

The publisher does not build native Echo launchers/children on GitHub runners. Their platform-specific source and hardware validation remain the maintainer's responsibility. No release may be considered hardware-supported merely because its V4 package passes generic validation.

The factory extraction cache remains `ASP1-EXTRACTED-CACHE-V2`. The new recursive audio-resource inventory does not invalidate honest historical cache inventories; an explicit refresh yielding different factory files preserves the old verified cache as a separate variant. The release manifest and helper hashes are **never** included in factory cache identity.

**Corrections:** do not move/rewrite an existing source tag. Changing previously published GitHub assets requires a separate authenticated, independently reviewed release-maintenance action and new checksum verification; it is not part of an automatic code build or this refactor.
