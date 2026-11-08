#!/usr/bin/env bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
go_cmd=${GO:-go}

# Generic CLI accepts explicit release contract and any declared data assets.
# Preserve the historical four/five-position argument build wrapper.
if [[ ${1:-} == --* ]]; then
  cd "$repo"
  exec "$go_cmd" run ./common-tools/asp-release/cmd "$@"
fi
if [[ $# -lt 4 || $# -gt 5 ]]; then
  echo "usage: $0 PLATFORM LAUNCHER_ARM32 CHILD_BIONIC OUT_DIR [CHANNEL_MAP]" >&2
  echo "or: $0 --platform NAME --version VERSION --contract RELEASE-CONTRACT.json ..." >&2
  exit 2
fi
platform=$1
launcher=$(realpath "$2")
child=$(realpath "$3")
out=$(realpath -m "$4")
map=${5:-}
if [[ -n "$map" ]]; then map=$(realpath "$map"); fi
[[ -f "$launcher" && -f "$child" && ! -e "$out" ]] || {
  echo "missing binary or output already exists" >&2
  exit 2
}
revision=$("$here/resolve-version.sh" "$repo")
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
cd "$repo"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 GOTOOLCHAIN=local \
  "$go_cmd" build -trimpath -ldflags="-s -w -X main.revision=$revision" \
  -o "$tmp/asp-installer" ./common-tools/asp-installer
mkdir -p "$out"
args=(--platform "$platform" --version "$revision"
      --installer "$tmp/asp-installer" --license "$repo/LICENSE"
      --launcher "$launcher" --child "$child" --out-dir "$out")
if [[ -n "$map" ]]; then
  args+=(--data "assets/channel-map=lib/rootfs/factory/channel-map:$map")
fi
"$go_cmd" run ./common-tools/asp-release/cmd "${args[@]}"
