#!/usr/bin/env python3
"""Offline regression tests for the exact release-publishing inventory.

The validator stub isolates staging/selection policy. Go release tests cover
the actual V3/V4 archive parser used by the publishing workflow.
"""
import hashlib
import importlib.util
import io
from pathlib import Path
import stat
import tarfile
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parent / "verify-release-inventory.py"
spec = importlib.util.spec_from_file_location("release_inventory", SCRIPT)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def make_desktop(path: Path, tag: str):
    root = f"asp-extractor-{tag}-linux-amd64"
    with tarfile.open(path, "w:gz") as tw:
        entry = tarfile.TarInfo(root)
        entry.type = tarfile.DIRTYPE
        entry.mode = 0o700
        tw.addfile(entry)
        for name in ("asp-extractor", "ASP-EXTRACTOR.md", "README.md", "LICENSE"):
            data = name.encode("ascii")
            entry = tarfile.TarInfo(root + "/" + name)
            entry.size = len(data)
            entry.mode = 0o500 if name == "asp-extractor" else 0o600
            tw.addfile(entry, io.BytesIO(data))


class ReleaseInventoryTests(unittest.TestCase):
    tag = "v3.6"

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.stage = self.root / "release-assets"
        self.stage.mkdir()
        self.validator = self.root / "validator"
        self.validator.write_text(
            '#!/bin/sh\n'
            'if [ "$1" = --validate ] && [ "$3" = --json ]; then '
            'name="$(basename "$2")"; '
            'platform="${name#frankenasp-v3.6-}"; platform="${platform%-arm32.tar.gz}"; '
            'printf \'{"name":"%s","schema":"ASP1-ASSETS-V4","version":"3.6","platform":"%s"}\\n\' "$name" "$platform"; '
            'else exit 2; fi\n'
        )
        self.validator.chmod(0o700)

    def package(self, count):
        for name in ("atlas", "moon", "9_test-model")[:count]:
            (self.stage / f"frankenasp-{self.tag}-{name}-arm32.tar.gz").write_bytes(b"fake-public-release-for-set-tests")
        make_desktop(self.stage / f"asp-extractor-{self.tag}-linux-amd64.tar.gz", self.tag)
        self.regenerate_sums()

    def regenerate_sums(self):
        lines = []
        for p in sorted(self.stage.glob("*.tar.gz")):
            lines.append(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}")
        (self.stage / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="ascii")

    def verify(self):
        return module.verify(self.stage, self.tag, self.validator)

    def test_one_two_three_platforms(self):
        for count in (1, 2, 3):
            with self.subTest(platforms=count):
                for p in self.stage.glob("*.tar.gz"):
                    p.unlink()
                self.package(count)
                items = self.verify()
                self.assertEqual(len(items), count + 1)
                self.assertTrue(all(p.startswith("release-assets/") for p in items))

    def test_platform_sets_without_desktop_are_rejected(self):
        for count in (2, 3):
            with self.subTest(platforms=count):
                for old in self.stage.glob("*.tar.gz"):
                    old.unlink()
                self.package(count)
                (self.stage / f"asp-extractor-{self.tag}-linux-amd64.tar.gz").unlink()
                self.regenerate_sums()
                with self.assertRaisesRegex(ValueError, "mandatory desktop extractor missing"):
                    self.verify()

    def test_desktop_only_rejected(self):
        self.package(1)
        next(self.stage.glob("frankenasp-*.tar.gz")).unlink()
        self.regenerate_sums()
        with self.assertRaisesRegex(ValueError, "at least one platform"):
            self.verify()

    def test_wrong_desktop_version_and_extra_desktop_rejected(self):
        self.package(1)
        desktop = self.stage / f"asp-extractor-{self.tag}-linux-amd64.tar.gz"
        renamed = self.stage / "asp-extractor-v3.7-linux-amd64.tar.gz"
        desktop.rename(renamed)
        self.regenerate_sums()
        with self.assertRaisesRegex(ValueError, "unexpected release archive name|mandatory desktop extractor"):
            self.verify()
        renamed.rename(desktop)
        extra = self.stage / "asp-extractor-v3.7-linux-amd64.tar.gz"
        extra.write_bytes(b"duplicate-unexpected-desktop")
        self.regenerate_sums()
        with self.assertRaisesRegex(ValueError, "unexpected release archive name"):
            self.verify()

    def test_extra_archive_rejected(self):
        self.package(1)
        (self.stage / "unlisted-release.tar.gz").write_bytes(b"extra")
        with self.assertRaisesRegex(ValueError, "inventory mismatch"):
            self.verify()

    def test_missing_archive_rejected(self):
        self.package(1)
        next(p for p in self.stage.glob("frankenasp*.tar.gz")).unlink()
        with self.assertRaisesRegex(ValueError, "inventory mismatch"):
            self.verify()

    def test_duplicate_checksum_rejected(self):
        self.package(1)
        path = self.stage / "SHA256SUMS"
        path.write_text(path.read_text() + path.read_text().splitlines()[0] + "\n")
        with self.assertRaisesRegex(ValueError, "duplicate"):
            self.verify()

    def test_bad_hash_rejected(self):
        self.package(1)
        archive = next(self.stage.glob("frankenasp*.tar.gz"))
        archive.write_bytes(b"mutated")
        with self.assertRaisesRegex(ValueError, "SHA256SUMS mismatch"):
            self.verify()

    def test_wrong_version_rejected(self):
        self.package(1)
        p = next(self.stage.glob("frankenasp*.tar.gz"))
        p.rename(self.stage / p.name.replace("v3.6", "v3.7"))
        self.regenerate_sums()
        with self.assertRaisesRegex(ValueError, "release archive identity mismatch"):
            self.verify()

    def test_manifest_version_mismatch_rejected(self):
        # Reproduces the independent review's 3.5-extra under v3.5 tag.
        self.package(1)
        source = self.validator.read_text()
        altered = source.replace('"version":"3.6"', '"version":"3.6-extra"')
        self.assertNotEqual(altered, source)
        self.validator.write_text(altered)
        with self.assertRaisesRegex(ValueError, "manifest version mismatch"):
            self.verify()

    def test_wrong_manifest_platform_rejected(self):
        self.package(1)
        source = self.validator.read_text()
        original = 'platform="' + chr(36) + '{name#frankenasp-v3.6-}"'
        altered = source.replace(original, 'platform="another-model"; other="' + chr(36) + '{name#frankenasp-v3.6-}"')
        self.assertNotEqual(altered, source)
        self.validator.write_text(altered)
        with self.assertRaisesRegex(ValueError, "archive identity mismatch"):
            self.verify()

    def test_symlink_release_rejected(self):
        self.package(1)
        p = next(self.stage.glob("frankenasp*.tar.gz"))
        target = self.root / "target"
        p.rename(target)
        p.symlink_to(target)
        with self.assertRaisesRegex(ValueError, "unsafe release archive"):
            self.verify()

    def test_desktop_extra_file_rejected(self):
        self.package(1)
        archive = self.stage / f"asp-extractor-{self.tag}-linux-amd64.tar.gz"
        root = f"asp-extractor-{self.tag}-linux-amd64"
        with tarfile.open(archive, "w:gz") as tw:
            item = tarfile.TarInfo(root + "/unexpected")
            data = b"x"
            item.size = len(data)
            tw.addfile(item, io.BytesIO(data))
        self.regenerate_sums()
        with self.assertRaisesRegex(ValueError, "unexpected desktop archive file"):
            self.verify()


if __name__ == "__main__":
    unittest.main()
