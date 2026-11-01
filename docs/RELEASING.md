# FrankenASP release maintenance

This document is for project maintainers. For installation on Echo devices, see [ASP-INSTALL.md](ASP-INSTALL.md).

## Version policy

The repository-root `VERSION` file defines the next/current release baseline, e.g. `3.1`.

- A clean checkout of an **annotated** matching release tag (`v3.1`) builds as `3.1`.
- Builds from untagged commits or a modified working tree use `3.1.YYMMDDHHMMSS`, where the suffix is the UTC build timestamp.
- A new release bumps the baseline, e.g. `3.2`. Published tags must not be moved.
- Build archive names include the complete version: `frankenasp-v3.1-athena-arm32.tar.gz`, `frankenasp-v3.1-rook-arm32.tar.gz`; timestamped builds use the full timestamped version.

`common-tools/asp-installer/resolve-version.sh` implements this policy and `version_test.sh` tests tagged/untagged scenarios. Optionally set `SOURCE_DATE_EPOCH` to share the same UTC timestamp across both platform build invocations.

## Manual release

1. Update `VERSION` to the selected major/minor. Commit code, docs and `VERSION`; run platform tests plus `go test -race -count=1 ./...` and `go vet ./...` in `common-tools/asp-installer`. Confirm clean Git state and the intended commit.
2. Create an **annotated** `vMAJOR.MINOR` tag on that exact commit. Do not retag older releases.
3. Build both ARM32 packages **from that clean tagged checkout** with `common-tools/asp-installer/build-release.sh`. Check that both archive names contain the same release version, and inspect each TAR's contents and platform manifest. Only FrankenASP-owned assets are permitted; factory vendor binaries must not be distributed.
4. Create an independent temporary Git branch `release-assets/vMAJOR.MINOR` containing exactly:
   - `frankenasp-vMAJOR.MINOR-athena-arm32.tar.gz`
   - `frankenasp-vMAJOR.MINOR-rook-arm32.tar.gz`
   - `SHA256SUMS` listing those two archive filenames and hashes
   - `COMMIT` with the full source commit hash, newline-terminated
   - `TAG` with the exact `vMAJOR.MINOR` tag, newline-terminated
5. Push the assets branch, then push the release commit to `main`. **Only after** the matching branch and `main` are published, push the annotated release tag.
6. The GitHub Actions workflow `.github/workflows/publish-release.yml` responds to the tag push, verifies `VERSION`, `COMMIT`, `TAG`, SHA-256 checksums and platform archives, and uses GitHub's job token to create a GitHub Release with both assets and `SHA256SUMS`.
7. Verify the Actions run completed successfully, that the Release exists and includes all three files, and that the published asset hashes match the source archives. After acceptance, delete the temporary release-assets branch. Retain the immutable release tag and Release attachments.

Pushes to `main` alone do **not** create releases. The publisher intentionally does not compile device launchers/Bionic binaries on GitHub runners; those platform-specific binaries are built and reviewed by the maintainer before publication.

**Correcting release assets:** Do not amend/move an existing annotated release tag. If archive filenames or other release metadata must be corrected without changing their contents, verify SHA-256 and update Release assets separately using an authenticated GitHub release-maintenance action. Such changes do not alter the tagged source snapshot.
