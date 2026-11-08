#!/usr/bin/env bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
go_cmd=${GO:-go}
revision=$("$repo/common-tools/asp-installer/resolve-version.sh" "$repo")
out=${1:-"$here/dist/asp-extractor-v${revision}-linux-amd64"}
mkdir -p "$(dirname "$out")"
(cd "$repo" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=local  "$go_cmd" build -trimpath -ldflags="-s -w -X main.extractorVersion=$revision -X main.revision=$revision" -o "$out" ./common-tools/asp-extractor/cmd)
echo "$out"
