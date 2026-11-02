#!/usr/bin/env bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../.." && pwd)
if [[ $# -ne 1 || ! -f "$1" ]]; then
  echo "usage: package.sh dist/asp-extractor-vVERSION-linux-amd64" >&2
  exit 2
fi
bin=$(realpath "$1")
base=$(basename "$bin")
[[ "$base" =~ ^asp-extractor-v[0-9][A-Za-z0-9._-]*-linux-amd64$ ]] || {
  echo "invalid versioned binary filename" >&2; exit 2;
}
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
mkdir -p "$tmp/$base"
cp "$bin" "$tmp/$base/asp-extractor"
cp "$root/LICENSE" "$tmp/$base/LICENSE"
cp "$here/README.md" "$tmp/$base/README.md"
cp "$root/docs/ASP-EXTRACTOR.md" "$tmp/$base/ASP-EXTRACTOR.md"
chmod 0500 "$tmp/$base/asp-extractor"
stamp=${SOURCE_DATE_EPOCH:-$(git -C "$root" log -1 --format=%ct)}
tar --sort=name --owner=0 --group=0 --numeric-owner --format=ustar \
    --mtime="@$stamp" -C "$tmp" -cf - "$base" | gzip -n > "$bin.tar.gz"
sha256sum "$bin" "$bin.tar.gz"
