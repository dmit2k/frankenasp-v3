#!/usr/bin/env bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
go_cmd=${GO:-go}
(cd "$repo" && GOFLAGS=-p=2 GOMAXPROCS=2 GOTOOLCHAIN=local "$go_cmd" test -race -count=1 ./... &&
 GOFLAGS=-p=2 GOMAXPROCS=2 GOTOOLCHAIN=local "$go_cmd" vet ./...)
