package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupPlatformDetectionDoesNotUseSerial(t *testing.T) {
	for _, tc := range []struct {
		name, image, want string
	}{
		{"G070RQ13843305B5", "system.img", "rook"},
		{"G091GM10221608N1", "p21-system.img", ""},
	} {
		dir := filepath.Join(t.TempDir(), tc.name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, tc.image), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := detectBackupPlatform(dir)
		if tc.want == "" && err == nil {
			t.Fatal("accepted incomplete backup")
		}
		if tc.want != "" && (err != nil || got != tc.want) {
			t.Fatalf("got %s, %v, want %s", got, err, tc.want)
		}
	}
	dir := t.TempDir()
	for _, name := range []string{"p21-system.img", "p12-vendor.img"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := detectBackupPlatform(dir)
	if err != nil || got != "athena" {
		t.Fatalf("Athena: %s %v", got, err)
	}
}

func TestOTAPlatformDetectionByContents(t *testing.T) {
	for _, tc := range []struct {
		name, fingerprint string
		vendor            bool
		want              string
	}{
		{"athena", "Amazon/athena/athena:9/PS75741.7080N/test", true, "athena"},
		{"rook", "Amazon/rook/rook:5.1.1/LVY48F/304.6.9.0_user_690918120", false, "rook"},
		{"contradictory", "Amazon/rook/rook:5.1.1/test", true, ""},
		{"unknown", "Amazon/unsupported/test:5.1/test", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "full-ota.zip")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			w, err := zw.Create("META-INF/com/android/metadata")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write([]byte("post-build=" + tc.fingerprint + "\n")); err != nil {
				t.Fatal(err)
			}
			if tc.vendor {
				_, err = zw.Create("vendor.transfer.list")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = zw.Close(); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := detectOTAPlatform(path)
			if tc.want == "" && err == nil {
				t.Fatalf("accepted invalid OTA %s", got)
			}
			if tc.want != "" && (err != nil || got != tc.want) {
				t.Fatalf("got %s %v, want %s", got, err, tc.want)
			}
		})
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
