#!/usr/bin/env bash
set -euo pipefail
resolver=$(realpath "$(dirname "$0")/resolve-version.sh")
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
git init -q -b main
git config user.name "Version Test"
git config user.email "version-test@example.invalid"
printf '3.1\n' > VERSION
printf 'initial\n' > tracked.txt
git add VERSION tracked.txt
git commit -qm base

export SOURCE_DATE_EPOCH=1793550000
expect_stamp=$(date -u -d "@$SOURCE_DATE_EPOCH" +%y%m%d%H%M%S)

check() {
  got=$("$resolver" "$tmp")
  [[ "$got" == "$1" ]] || { echo "version expected $1, got $got" >&2; exit 1; }
  echo "PASS: $2 -> $got"
}

check "3.1.$expect_stamp" "untagged main"
git tag v3.1
check "3.1.$expect_stamp" "lightweight tag is not a release"
git tag -d v3.1 >/dev/null
git tag -am "Release 3.1" v3.1
check 3.1 "annotated release tag, clean tree"
printf 'dirty\n' >> tracked.txt
check "3.1.$expect_stamp" "modified release working tree"
git checkout -- tracked.txt
check 3.1 "clean exact release"
printf 'post-release\n' >> tracked.txt
git add tracked.txt
git commit -qm next
check "3.1.$expect_stamp" "later main commit"
printf '3.2\n' > VERSION
git add VERSION
git commit -qm bump
check "3.2.$expect_stamp" "new release base before tagging"
git tag -am "Release 3.2" v3.2
check 3.2 "next tagged release"
printf 'future\n' >> tracked.txt
git add tracked.txt
git commit -qm after-v3.2
check "3.2.$expect_stamp" "post-v3.2 main"

if [[ "$("$resolver" "$tmp")" == 3.1.* ]]; then
 echo "UNEXPECTED OLD VERSION" >&2
 exit 1
fi
echo "PASS: version progression 3.1 < 3.1.timestamp < 3.2 < 3.2.timestamp"
