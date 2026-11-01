#!/usr/bin/env bash
set -euo pipefail

# Manual release: an annotated tag vMAJOR.MINOR on a clean exact commit.
# Non-tagged commits (and locally modified trees) use build-time UTC suffix.
# SOURCE_DATE_EPOCH allows two platform archives to share a build timestamp.
repo=${1:?usage: resolve-version.sh REPO_ROOT}
[[ -f "$repo/VERSION" ]] || { echo "missing project VERSION" >&2; exit 2; }
base=$(tr -d '\r\n' < "$repo/VERSION")
[[ "$base" =~ ^3\.[0-9]+$ ]] || { echo "invalid base version: $base" >&2; exit 2; }

official=false
if git -C "$repo" rev-parse --verify HEAD >/dev/null 2>&1 &&
   [[ "$(git -C "$repo" cat-file -t "refs/tags/v$base" 2>/dev/null || true)" == tag ]] &&
   [[ "$(git -C "$repo" rev-parse HEAD)" == "$(git -C "$repo" rev-parse "refs/tags/v$base^{commit}")" ]] &&
   [[ -z "$(git -C "$repo" status --porcelain --untracked-files=normal)" ]]; then
  official=true
fi

if [[ "$official" == true ]]; then
  printf '%s\n' "$base"
  exit 0
fi

if [[ -n "${SOURCE_DATE_EPOCH:-}" ]]; then
  [[ "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]] || { echo "invalid SOURCE_DATE_EPOCH" >&2; exit 2; }
  stamp=$(date -u -d "@$SOURCE_DATE_EPOCH" +%y%m%d%H%M%S)
else
  stamp=$(date -u +%y%m%d%H%M%S)
fi
printf '%s.%s\n' "$base" "$stamp"
