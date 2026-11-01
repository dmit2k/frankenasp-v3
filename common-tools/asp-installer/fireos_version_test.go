package main

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarketingFireOSProperty(t *testing.T) {
	for _, tc := range []struct{ raw, version string }{
		{"ro.build.mktg.fireos=Fire OS 5.5.6.9 (690918120)\nro.build.version.release=5.1.1\n", "5.5.6.9"},
		{"ro.build.mktg.fireos=Fire OS 7574.1\nro.build.version.fireos=7.0\n", "7574.1"},
		{"ro.build.version.name=Fire OS 7575.1 (PS75751/7806)\n", "7575.1"},
		{"ro.build.version.release=9\nro.build.version.fireos=7.0\n", ""},
	} {
		if got := parseFireOSBuildProp([]byte(tc.raw)); got != tc.version {
			t.Errorf("%q -> %s, want %s", tc.raw, got, tc.version)
		}
	}
}

func TestFireOSReadFromOTAContents(t *testing.T) {
	for _, tc := range []struct{ archiveEntry, version string }{
		{"system/build.prop", "7574.1"},
		{"META-INF/com/amazon/android/target.build.prop", "5.5.6.9"},
	} {
		name := filepath.Join(t.TempDir(), "firmware.zip")
		f, e := os.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		z := zip.NewWriter(f)
		w, e := z.Create(tc.archiveEntry)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte("ro.build.mktg.fireos=Fire OS " + tc.version + "\n")); e != nil {
			t.Fatal(e)
		}
		if e = z.Close(); e != nil {
			t.Fatal(e)
		}
		if e = f.Close(); e != nil {
			t.Fatal(e)
		}
		result, e := fireOSFromOTA(name)
		if e != nil || result != tc.version {
			t.Fatalf("OTA property: %q %v", result, e)
		}
	}
}

func TestLegacyCacheMigrationPreservesArchives(t *testing.T) {
	base, original := fixtureCache(t)
	newPath := original.Path
	oldPath := filepath.Join(base, "athena-PS75741-7080")
	if e := os.Rename(newPath, oldPath); e != nil {
		t.Fatal(e)
	}
	if _, e := readCache(oldPath); e != nil {
		t.Fatalf("legacy cache should be valid: %v", e)
	}
	archiveDir, e := ensureOTADir(base)
	if e != nil {
		t.Fatal(e)
	}
	oldOTA := filepath.Join(archiveDir, "athena-PS75741-7080.bin")
	if e = os.WriteFile(oldOTA, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = migrateCacheNames(base, nil); e != nil {
		t.Fatal(e)
	}
	if e = migrateCacheNames(base, nil); e != nil {
		t.Fatal("non-idempotent migration: ", e)
	}
	got, e := readCache(newPath)
	if e != nil || got.Metadata.FireOS != "7574.1" {
		t.Fatalf("migrated cache invalid: %+v %v", got.Metadata, e)
	}
	if _, e = os.Stat(oldPath); !os.IsNotExist(e) {
		t.Fatal("legacy path was not moved")
	}
	archive, e := archiveExisting(context.Background(), got)
	if e != nil || archive != filepath.Join(archiveDir, filepath.Base(newPath)+".bin") {
		t.Fatalf("OTA archive migration: %q %v", archive, e)
	}
}
func TestInvalidLegacyCacheNeverRenamed(t *testing.T) {
	base, original := fixtureCache(t)
	oldPath := filepath.Join(base, "athena-PS75741-7080")
	if e := os.Rename(original.Path, oldPath); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(oldPath, "files/vendor/lib/libasp.so"), []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	e := migrateCacheNames(base, nil)
	if e == nil || !strings.Contains(e.Error(), "invalid cache") {
		t.Fatalf("unsafe cache migration accepted: %v", e)
	}
	if _, e = os.Stat(oldPath); e != nil {
		t.Fatal("corrupted cache deleted")
	}
}
