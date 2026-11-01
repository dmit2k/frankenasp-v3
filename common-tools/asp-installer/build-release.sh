#!/usr/bin/env bash
set -euo pipefail
if [[ $# -lt 4 || $# -gt 5 ]]; then
  echo "usage: $0 athena|rook LAUNCHER_ARM32 CHILD_BIONIC OUT_DIR [ATHENA_CHANNEL_MAP]" >&2
  exit 2
fi
platform=$1
launcher=$(realpath "$2")
child=$(realpath "$3")
out=$(realpath -m "$4")
map=${5:-}
case "$platform" in
  athena) [[ -n "$map" ]] || { echo "Athena requires a qualified channel-map" >&2; exit 2; }; map=$(realpath "$map");;
  rook) [[ -z "$map" ]] || { echo "Rook has no channel-map asset" >&2; exit 2; };;
  *) echo "unsupported platform $platform" >&2; exit 2;;
esac
[[ -f "$launcher" && -f "$child" && ! -e "$out" ]] || { echo "missing binary or output already exists" >&2; exit 2; }
git_root=$(git rev-parse --show-toplevel)
# Resolve the official release tag or a post-release timestamp automatically.
revision=$("$git_root/common-tools/asp-installer/resolve-version.sh" "$git_root")
go_cmd=${GO:-go}
mkdir -p "$out"
pkg="$out/frankenasp-$platform"
mkdir -p "$pkg/assets"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 GOTOOLCHAIN=local \
  "$go_cmd" build -trimpath -ldflags="-s -w -X main.revision=$revision" -o "$pkg/asp-installer" .
cp "$launcher" "$pkg/assets/external-asp"
cp "$child" "$pkg/assets/asp1-child"
cp "$git_root/LICENSE" "$pkg/LICENSE"
if [[ "$platform" == athena ]]; then cp "$map" "$pkg/assets/channel-map"; fi
printf '{"schema":"ASP1-ASSETS-V3","platform":"%s","cache_schema":"ASP1-EXTRACTED-CACHE-V2"}\n' "$platform" > "$pkg/assets/manifest.json"
chmod 0500 "$pkg/asp-installer" "$pkg/assets/external-asp" "$pkg/assets/asp1-child"
chmod 0600 "$pkg/assets/manifest.json"
if [[ "$platform" == athena ]]; then chmod 0600 "$pkg/assets/channel-map"; fi
(cd "$out" && tar --owner=0 --group=0 --numeric-owner -czf "frankenasp-v${revision}-$platform-arm32.tar.gz" "frankenasp-$platform")
echo "$out/frankenasp-v${revision}-$platform-arm32.tar.gz"
