#!/usr/bin/env bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
src="$here/../asp-installer"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
cp "$here/go.mod" "$here/go.sum" "$tmp/"
for f in "$src"/*.go; do
 case "$(basename "$f")" in main.go|*_test.go) continue;; esac
 cp "$f" "$tmp/"
done
cp "$here"/cmd/*.go "$tmp/"
mkdir -p "$tmp/internal"
cp -R "$here/internal/hostos" "$tmp/internal/"
go_cmd=${GO:-go}
(cd "$tmp" && GOFLAGS=-p=2 GOMAXPROCS=2 GOOS=linux GOARCH=amd64 CGO_ENABLED=1 GOTOOLCHAIN=local "$go_cmd" test -race -count=1 ./... && GOFLAGS=-p=2 GOMAXPROCS=2 GOTOOLCHAIN=local "$go_cmd" vet ./...)
