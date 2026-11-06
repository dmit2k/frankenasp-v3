package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A real ext4 image, embedded as a full block OTA v4, exercises the selective
// filesystem reader instead of a standalone ZIP build.prop fixture.
func syntheticBlockIdentityOTA(t *testing.T, zipDevice, fsDevice string) string {
	t.Helper()
	mke2fs, err := exec.LookPath("mke2fs")
	if err != nil {
		t.Skip("mke2fs is needed for real ext4 regression coverage")
	}
	dir := t.TempDir()
	tree := filepath.Join(dir, "tree")
	if err := os.MkdirAll(tree, 0700); err != nil {
		t.Fatal(err)
	}
	props := "ro.build.version.name=synthetic\n"
	if fsDevice != "" {
		props += "ro.product.device=" + fsDevice + "\n"
	}
	if err := os.WriteFile(filepath.Join(tree, "build.prop"), []byte(props), 0600); err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(dir, "system.img")
	if out, err := exec.Command(mke2fs, "-q", "-t", "ext4", "-F", "-b", "4096", "-d", tree, img, "2048").CombinedOutput(); err != nil {
		t.Fatalf("create ext4: %v: %s", err, out)
	}
	raw, err := os.ReadFile(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%blockSize != 0 {
		t.Fatalf("unaligned ext4 image: %d", len(raw))
	}
	blocks := len(raw) / blockSize
	source := filepath.Join(dir, "source.zip")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	members := map[string][]byte{
		"system.transfer.list": []byte(fmt.Sprintf("4\n%d\n0\n0\nnew 2,0,%d\n", blocks, blocks)),
		"system.new.dat":       raw,
	}
	if zipDevice != "" {
		members["META-INF/com/android/metadata"] = []byte("post-device=" + zipDevice + "\n")
	}
	for _, name := range []string{"META-INF/com/android/metadata", "system.transfer.list", "system.new.dat"} {
		data, exists := members[name]
		if !exists {
			continue
		}
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return source
}

func cachedBlockIdentityFixture(t *testing.T, source string, originalURL string) (string, cacheResult, []byte, []byte) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "cache")
	if err := requireCacheDir(root); err != nil {
		t.Fatal(err)
	}
	hash, err := hashOTA(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	meta := cacheMetadata{
		Platform: "athena", Firmware: "synthetic", SourceSHA256: hash,
		OTAFormat: "ZIP_FULL_BLOCK_v4",
	}
	if originalURL != "" {
		meta.URLSHA256 = hashString(originalURL)
		meta.OriginalOTAName = "factory.zip"
	}
	cached, err := publishCache(root, meta, map[string][]byte{"vendor/lib/libasp.so": []byte("fixture")}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(cached.Path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile(filepath.Join(cached.Path, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	return root, cached, manifest, sums
}

func assertBlockCacheUnchanged(t *testing.T, root string, cached cacheResult, manifest, sums []byte) {
	t.Helper()
	if dedupCacheCount(t, root) != 1 {
		t.Fatal("unexpected extracted cache directory")
	}
	afterManifest, err := os.ReadFile(filepath.Join(cached.Path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	afterSums, err := os.ReadFile(filepath.Join(cached.Path, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(manifest, afterManifest) || !bytes.Equal(sums, afterSums) {
		t.Fatal("existing manifest or SHA256SUMS modified")
	}
}

func expectBlockIdentityConflict(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "conflicts") ||
		!strings.Contains(err.Error(), "donut_puffin") {
		t.Fatalf("ZIP rook / ext4 donut_puffin must conflict: %v", err)
	}
}

func TestBlockCacheHitRejectsConflictingFilesystemIdentity(t *testing.T) {
	source := syntheticBlockIdentityOTA(t, "rook", "donut_puffin")
	t.Run("local_source_hash", func(t *testing.T) {
		root, cached, manifest, sums := cachedBlockIdentityFixture(t, source, "")
		meter := newMeter()
		defer meter.Close()
		_, _, _, err := loadExtracted(context.Background(), root, "rook", source, "", "", root, "inspect", false, false, false, meter)
		expectBlockIdentityConflict(t, err)
		cacheHitErr := err
		// Refresh reaches ordinary extraction and must reject the same mismatch.
		_, _, _, err = loadExtracted(context.Background(), root, "rook", source, "", "", root, "inspect", false, true, false, meter)
		expectBlockIdentityConflict(t, err)
		if err.Error() != cacheHitErr.Error() {
			t.Fatalf("cache-hit and refresh identity results differ: %v vs %v", cacheHitErr, err)
		}
		assertBlockCacheUnchanged(t, root, cached, manifest, sums)
	})
	t.Run("cached_url_archive", func(t *testing.T) {
		const url = "https://example.invalid/factory.zip"
		root, cached, manifest, sums := cachedBlockIdentityFixture(t, source, url)
		archiveDir, err := ensureOTADir(root)
		if err != nil {
			t.Fatal(err)
		}
		pending := filepath.Join(archiveDir, "pending.zip")
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pending, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := saveOTAArchive(context.Background(), cached, pending, true, nil); err != nil {
			t.Fatal(err)
		}
		meter := newMeter()
		defer meter.Close()
		// URL SHA lookup and archived-filename lookup both use the same archive.
		for _, candidate := range []string{url, "https://other.invalid/factory.zip"} {
			_, _, _, err := loadExtracted(context.Background(), root, "rook", "", candidate, "", t.TempDir(), "inspect", false, false, false, meter)
			expectBlockIdentityConflict(t, err)
		}
		assertBlockCacheUnchanged(t, root, cached, manifest, sums)
	})
	t.Run("saved_local_ota_for_url", func(t *testing.T) {
		root, cached, manifest, sums := cachedBlockIdentityFixture(t, source, "")
		localDir := t.TempDir()
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(localDir, "factory.zip"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		meter := newMeter()
		defer meter.Close()
		_, _, _, err = loadExtracted(context.Background(), root, "rook", "", "https://example.invalid/factory.zip", "", localDir, "inspect", false, false, false, meter)
		expectBlockIdentityConflict(t, err)
		assertBlockCacheUnchanged(t, root, cached, manifest, sums)
	})
	t.Run("post_download_source_hash", func(t *testing.T) {
		root, cached, manifest, sums := cachedBlockIdentityFixture(t, source, "")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f, err := os.Open(source)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer f.Close()
			_, _ = io.Copy(w, f)
		}))
		defer server.Close()
		meter := newMeter()
		defer meter.Close()
		_, _, cleanup, err := loadExtracted(context.Background(), root, "rook", "", server.URL+"/download.bin", "", t.TempDir(), "inspect", false, false, true, meter)
		if cleanup != nil {
			cleanup()
		}
		expectBlockIdentityConflict(t, err)
		assertBlockCacheUnchanged(t, root, cached, manifest, sums)
	})
}

func TestBlockCacheHitIdentityAvailability(t *testing.T) {
	for _, tc := range []struct {
		name, zipDevice, fsDevice, requested string
	}{
		{"matching_both", "rook", "rook", "rook"},
		{"zip_only", "rook", "", "rook"},
		{"filesystem_only", "", "rook", "rook"},
		{"neither", "", "", "rook"},
		{"platform_omitted", "rook", "donut_puffin", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := syntheticBlockIdentityOTA(t, tc.zipDevice, tc.fsDevice)
			root, cached, manifest, sums := cachedBlockIdentityFixture(t, source, "")
			meter := newMeter()
			defer meter.Close()
			result, _, _, err := loadExtracted(context.Background(), root, tc.requested, source, "", "", root, "inspect", false, false, false, meter)
			if err != nil || !result.Hit || result.Path != cached.Path {
				t.Fatalf("expected content-based early hit: %+v %v", result, err)
			}
			assertBlockCacheUnchanged(t, root, cached, manifest, sums)
		})
	}
}
