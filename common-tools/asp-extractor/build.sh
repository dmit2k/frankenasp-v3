#!/usr/bin/env bash
set -euo pipefail
# Compile existing installer extraction/cache source verbatim with the desktop
# entrypoint. No duplicate OTA parser or changes to the embedded installer.
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
src="$repo/common-tools/asp-installer"
go_cmd=${GO:-go}
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
cp "$here/go.mod" "$tmp/go.mod"
cp "$here/go.sum" "$tmp/go.sum"
for f in "$src"/*.go; do
  case "$(basename "$f")" in main.go|*_test.go) continue;; esac
  cp "$f" "$tmp/"
done
cp "$here"/cmd/*.go "$tmp/"
mkdir -p "$tmp/internal"
cp -R "$here/internal/hostos" "$tmp/internal/"
revision=$("$src/resolve-version.sh" "$repo")
out=${1:-"$here/dist/asp-extractor-v${revision}-linux-amd64"}
mkdir -p "$(dirname "$out")"
(cd "$tmp" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=local \
 "$go_cmd" build -trimpath -ldflags="-s -w -X main.extractorVersion=$revision -X main.revision=$revision" -o "$out" .)
echo "$out"
