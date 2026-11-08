#!/usr/bin/env python3
"""Verify an exact release artifact set before publishing any GitHub assets.

The SHA256SUMS document is the staging inventory. Every listed file must
exist, every archive must be individually validated and no additional release
archive may be present. The output contains exact, shell-safe relative paths.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tarfile

HASH_LINE = re.compile(r"([0-9a-f]{64})  ([A-Za-z0-9._-]+\.tar\.gz)")
VALID_TAG = re.compile(r"v[0-9][A-Za-z0-9._-]{0,63}")

def regular(path: Path, bound: int) -> bool:
    info = path.lstat()
    return stat.S_ISREG(info.st_mode) and 0 < info.st_size <= bound and info.st_nlink == 1

def sha256(path: Path) -> str:
    d = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b""):
            d.update(chunk)
    return d.hexdigest()

def verify_desktop(archive: Path, tag: str) -> None:
    root = f"asp-extractor-{tag}-linux-amd64"
    required = {"asp-extractor", "ASP-EXTRACTOR.md", "README.md", "LICENSE"}
    found = set()
    seen = set()
    total = 0
    with tarfile.open(archive, "r:gz") as stream:
        members = stream.getmembers()
        if len(members) > 16:
            raise ValueError("desktop archive member count exceeded")
        for item in members:
            name = item.name.rstrip("/")
            if name in seen:
                raise ValueError("duplicate desktop archive member")
            seen.add(name)
            if item.isdir():
                if name != root:
                    raise ValueError(f"unexpected desktop archive directory {name!r}")
                continue
            if not item.isfile():
                raise ValueError("unsafe desktop archive member type")
            prefix = root + "/"
            if not name.startswith(prefix):
                raise ValueError("wrong desktop archive root")
            relative = name[len(prefix):]
            if relative not in required:
                raise ValueError(f"unexpected desktop archive file {relative!r}")
            if item.size <= 0 or item.size > (32 << 20) or (item.mode & 0o7000):
                raise ValueError("invalid desktop archive file metadata")
            total += item.size
            if total > (48 << 20):
                raise ValueError("desktop archive expanded size exceeded")
            found.add(relative)
    if found != required:
        raise ValueError(f"desktop archive missing files: {required - found}")

def verify(directory: Path, tag: str, validator: Path) -> list[str]:
    if not VALID_TAG.fullmatch(tag):
        raise ValueError("invalid release tag")
    inventory = directory / "SHA256SUMS"
    if not regular(inventory, 32 << 10):
        raise ValueError("missing or unsafe SHA256SUMS")
    raw = inventory.read_text(encoding="ascii")
    if not raw.endswith("\n"):
        raise ValueError("SHA256SUMS must end in newline")
    records = {}
    for line in raw.splitlines():
        match = HASH_LINE.fullmatch(line)
        if not match:
            raise ValueError(f"invalid SHA256SUMS entry {line!r}")
        digest, name = match.groups()
        if name in records:
            raise ValueError(f"duplicate SHA256SUMS entry {name}")
        records[name] = digest
    if not records:
        raise ValueError("empty release inventory")
    desktop = f"asp-extractor-{tag}-linux-amd64.tar.gz"
    # Both version and platform may contain dashes. Derive the identity
    # from validated manifest fields, never from a guessed split.
    platform_pattern = re.compile(r"frankenasp-v[0-9][A-Za-z0-9._-]*-arm32\.tar\.gz")
    found_platforms = set()
    for name in records:
        if name == desktop:
            continue
        if not platform_pattern.fullmatch(name):
            raise ValueError(f"unexpected release archive name {name}")
    # Desktop is mandatory independently of how many platform archives exist.
    if desktop not in records:
        raise ValueError("mandatory desktop extractor missing from inventory")
    if len(records) <= 1:
        raise ValueError("at least one platform release is required")
    actual = {p.name for p in directory.iterdir() if p.name.endswith(".tar.gz")}
    if actual != records.keys():
        raise ValueError(f"archive inventory mismatch: missing={sorted(records.keys()-actual)}, extra={sorted(actual-records.keys())}")
    for name, digest in records.items():
        archive = directory / name
        if not regular(archive, 64 << 20):
            raise ValueError(f"missing or unsafe release archive {name}")
        if sha256(archive) != digest:
            raise ValueError(f"SHA256SUMS mismatch for {name}")
        if name == desktop:
            verify_desktop(archive, tag)
            continue
        result = subprocess.run(
            [str(validator), "--validate", str(archive), "--json"],
            capture_output=True, text=True, timeout=120, check=False,
        )
        if result.returncode != 0:
            raise ValueError(f"release archive validation failed for {name}: {result.stderr}")
        try:
            verified = json.loads(result.stdout)
        except (ValueError, TypeError) as error:
            raise ValueError(f"validator did not return verified JSON for {name}") from error
        if not isinstance(verified, dict) or set(verified) != {"name", "schema", "platform", "version"}:
            raise ValueError(f"unexpected verified release fields for {name}")
        platform = verified["platform"]
        version = verified["version"]
        if not isinstance(platform, str) or not re.fullmatch(r"[a-z0-9][a-z0-9._-]{0,47}", platform):
            raise ValueError(f"invalid verified release platform for {name}")
        if version != tag[1:]:
            raise ValueError(f"release manifest version mismatch: {version!r} != {tag[1:]!r}")
        if verified["schema"] not in {"ASP1-ASSETS-V3", "ASP1-ASSETS-V4"}:
            raise ValueError(f"unsupported release schema for {name}")
        canonical = f"frankenasp-{tag}-{platform}-arm32.tar.gz"
        if verified["name"] != name or canonical != name:
            raise ValueError(f"release archive identity mismatch for {name}")
        if platform in found_platforms:
            raise ValueError(f"duplicate platform archive {platform}")
        found_platforms.add(platform)
    return [str(Path(directory.name) / name) for name in sorted(records)]

def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dir", required=True, type=Path)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--validator", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    try:
        verified = verify(args.dir.resolve(), args.tag, args.validator.resolve())
        args.output.write_text("\n".join(verified) + "\n", encoding="ascii")
        print(f"VERIFIED release inventory: {len(verified)-1} platform(s) + desktop")
    except (ValueError, OSError, subprocess.TimeoutExpired, tarfile.TarError) as error:
        print(f"REJECTED release inventory: {error}", file=sys.stderr)
        return 1
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
