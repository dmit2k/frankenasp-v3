package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupDetectionRejectsNonExt4WithoutCodenameGuess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "unknown-model")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "system.img"), []byte("not ext4"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := detectBackupPlatform(dir); err == nil {
		t.Fatal("accepted unsupported filesystem")
	}
}

func TestGenericIdentityAndExplicitMismatch(t *testing.T) {
	for _, tc := range []struct{ device, fingerprint, want string }{
		{"", "Amazon/athena/athena:9/PS75741.7080N/test", "athena"},
		{"", "Amazon/rook/rook:5.1/test", "rook"},
		{"", "Amazon/test-board/previously_unknown_echo:9/example", "previously_unknown_echo"},
		{"newboard", "", "newboard"},
		{"", "", ""},
	} {
		got, err := sourceCodename(tc.device, tc.fingerprint)
		if err != nil || got != tc.want {
			t.Fatalf("%q %q: %q, %v", tc.device, tc.fingerprint, got, err)
		}
	}
	if _, err := sourceCodename("device-a", "Amazon/a/device-b:9/x"); err == nil {
		t.Fatal("conflicting source identity accepted")
	}
	if _, err := resolveCodename("another", "previously_unknown_echo"); err == nil {
		t.Fatal("explicit mismatch accepted")
	}
	if got, err := resolveCodename("", "previously_unknown_echo"); err != nil || got != "previously_unknown_echo" {
		t.Fatalf("automatic codename: %q, %v", got, err)
	}
}

func TestOTAPlatformDetectionIndependentOfPartitionLayout(t *testing.T) {
	for _, tc := range []struct {
		fingerprint string
		vendor      bool
		want        string
	}{
		{"Amazon/athena/athena:9/PS75741.7080N/test", true, "athena"},
		{"Amazon/rook/rook:5.1/test", false, "rook"},
		{"Amazon/rook/rook:5.1/test", true, "rook"},
		{"Amazon/new/new_model:9/test", false, "new_model"},
		{"Amazon/new/9_model:9/test", false, "9_model"},
		{"", false, ""},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "ota.zip")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		w, err := zw.Create("META-INF/com/android/metadata")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("post-build=" + tc.fingerprint + "\n")); err != nil {
			t.Fatal(err)
		}
		if tc.vendor {
			if _, err := zw.Create("vendor.transfer.list"); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := detectOTAPlatform(path)
		if err != nil || got != tc.want {
			t.Fatalf("want %q got %q error %v", tc.want, got, err)
		}
	}
}

func TestCacheSchemaExcludesHelperFingerprint(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	s := sampleCache(t, root, "rook", "690918120", strings.Repeat("a", 64))
	if s.Metadata.Schema != "ASP1-EXTRACTED-CACHE-V2" {
		t.Fatal(s.Metadata.Schema)
	}
	manifest, err := os.ReadFile(filepath.Join(s.Path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "helper_sha256") {
		t.Fatal("cache still bound to helper")
	}
}
