package main

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dedupCacheCount(t *testing.T, root string) int {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && entry.Name() != otaImageDir {
			count++
		}
	}
	return count
}

func TestCacheContentIdentityIndependentOfLabels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*cacheMetadata)
	}{
		{"same_source_different_platform", func(m *cacheMetadata) { m.Platform = "donut_puffin" }},
		{"same_inventory_different_source", func(m *cacheMetadata) { m.SourceSHA256 = hashString("second source") }},
		{"same_inventory_different_versions", func(m *cacheMetadata) {
			m.Platform = "donut_puffin"
			m.SourceSHA256 = hashString("new backup")
			m.Firmware = "other-build"
			m.FireOS = "6.5.7.5"
		}},
		{"same_inventory_absent_platform", func(m *cacheMetadata) {
			m.Platform = ""
			m.SourceSHA256 = hashString("anonymous backup")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, first := fixtureCache(t)
			originalManifest, err := os.ReadFile(filepath.Join(first.Path, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			originalSums, err := os.ReadFile(filepath.Join(first.Path, "SHA256SUMS"))
			if err != nil {
				t.Fatal(err)
			}
			different := first.Metadata
			tc.change(&different)
			second, err := publishCache(root, different, first.Items, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !second.Hit || second.Path != first.Path || dedupCacheCount(t, root) != 1 {
				t.Fatalf("duplicate cache created: first=%s, second=%s, hit=%v", first.Path, second.Path, second.Hit)
			}
			afterManifest, err := os.ReadFile(filepath.Join(first.Path, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			afterSums, err := os.ReadFile(filepath.Join(first.Path, "SHA256SUMS"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(originalManifest, afterManifest) || !bytes.Equal(originalSums, afterSums) {
				t.Fatal("existing cache metadata or SHA inventory modified")
			}
			if result, err := readCache(first.Path); err != nil || result.Metadata.Platform != first.Metadata.Platform {
				t.Fatalf("original cache corrupted: %v", err)
			}
		})
	}
}

func TestCacheSourceHashReuseAcrossLabels(t *testing.T) {
	root, first := fixtureCache(t)
	for _, platform := range []string{"donut_puffin", "", "unknown_echo"} {
		found, err := findCache(root, platform, first.Metadata.SourceSHA256, "")
		if err != nil || found.Path != first.Path {
			t.Fatalf("source digest must not depend on platform %q: %v", platform, err)
		}
		byURL, err := findCache(root, platform, "", first.Metadata.URLSHA256)
		if err != nil || byURL.Path != first.Path {
			t.Fatalf("URL identity must not depend on platform %q: %v", platform, err)
		}
	}
	if selected, err := selectFirmwareCache(root, "athena", first.Metadata.Firmware); err != nil || selected.Path != first.Path {
		t.Fatalf("explicit selection changed: %v", err)
	}
	if _, err := selectFirmwareCache(root, "unknown_echo", first.Metadata.Firmware); err == nil {
		t.Fatal("explicit platform cache selection must remain filtered")
	}
}

func TestCacheDifferentFactoryInventoriesRetainedAcrossPlatforms(t *testing.T) {
	root, first := fixtureCache(t)
	modified := first.Metadata
	modified.Platform = "other_echo"
	modified.Firmware = "other-build"
	modified.FireOS = "9.9.9"
	modified.SourceSHA256 = hashString("second distinct OTA")
	copyItems := make(map[string][]byte, len(first.Items))
	for k, v := range first.Items {
		copyItems[k] = v
	}
	copyItems["vendor/lib/libasp.so"] = []byte("different-content")
	second, err := publishCache(root, modified, copyItems, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Hit || second.Path == first.Path || dedupCacheCount(t, root) != 2 {
		t.Fatalf("distinct content wrongly reused: %s, %s", first.Path, second.Path)
	}
	for _, item := range []struct{ platform, name, want string }{
		{"athena", filepath.Base(first.Path), first.Path},
		{"other_echo", filepath.Base(second.Path), second.Path},
	} {
		got, err := selectFirmwareCache(root, item.platform, item.name)
		if err != nil || got.Path != item.want {
			t.Fatalf("exact cache selection: %v", err)
		}
	}
	if _, err := selectFirmwareCache(root, "", ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("unfiltered selection must remain ambiguous: %v", err)
	}
}

func syntheticOTAIdentity(t *testing.T, metadata, buildprop string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "factory.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(f)
	for name, content := range map[string]string{
		"META-INF/com/android/metadata": metadata,
		"system/build.prop":             buildprop,
	} {
		w, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCachedOTASourceStillRejectsExplicitIdentityMismatch(t *testing.T) {
	for _, tc := range []struct{ name, metadata, buildprop string }{
		{"ota_metadata", "post-build=Amazon/donut/donut_puffin:6.0/TEST/123\n", ""},
		{"build_properties", "", "ro.product.device=donut_puffin\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "cache")
			if err := requireCacheDir(root); err != nil {
				t.Fatal(err)
			}
			path := syntheticOTAIdentity(t, tc.metadata, tc.buildprop)
			digest, err := hashOTA(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			cached, err := publishCache(root, cacheMetadata{
				Platform: "rook", Firmware: "from-cache", SourceSHA256: digest,
				OTAFormat: "ZIP_FILE_TREE",
			}, map[string][]byte{"system/lib/libasp.so": []byte("test-data")}, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			meter := newMeter()
			defer meter.Close()
			_, _, _, err = loadExtracted(context.Background(), root, "rook", path, "", "", root, "inspect", false, false, false, meter)
			if err == nil || !strings.Contains(err.Error(), "conflicts") {
				t.Fatalf("early hit bypassed source identity: %v", err)
			}
			if dedupCacheCount(t, root) != 1 {
				t.Fatal("unexpected cache write")
			}
			if _, err := readCache(cached.Path); err != nil {
				t.Fatal(err)
			}
			_, _, _, err = loadExtracted(context.Background(), root, "donut_puffin", path, "", "", root, "inspect", false, false, false, meter)
			if err != nil {
				t.Fatalf("same source must be reusable despite old platform label: %v", err)
			}
			if dedupCacheCount(t, root) != 1 {
				t.Fatal("duplicate on repeated source")
			}
		})
	}
}

func TestCachedURLReuseAcrossSourceLabelsRejectsKnownMismatch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	if err := requireCacheDir(root); err != nil {
		t.Fatal(err)
	}
	source := syntheticOTAIdentity(t, "post-build=Amazon/donut/donut_puffin:6.0/TEST/123\\n", "")
	digest, err := hashOTA(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	const originalURL = "https://example.invalid/factory-source.zip"
	first, err := publishCache(root, cacheMetadata{
		Platform: "rook", Firmware: "old-label", SourceSHA256: digest,
		URLSHA256: hashString(originalURL), OriginalOTAName: "factory-source.zip",
		OTAFormat: "ZIP_FILE_TREE",
	}, map[string][]byte{"system/lib/libasp.so": []byte("test-data")}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	archiveDir, err := ensureOTADir(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	downloaded := filepath.Join(archiveDir, "downloaded-test-ota.zip")
	if err := os.WriteFile(downloaded, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := saveOTAArchive(context.Background(), first, downloaded, true, nil); err != nil {
		t.Fatal(err)
	}
	meter := newMeter()
	defer meter.Close()
	for _, url := range []string{originalURL, "https://different.invalid/factory-source.zip"} {
		t.Run(url, func(t *testing.T) {
			reused, _, _, err := loadExtracted(context.Background(), root, "donut_puffin", "", url, "", root, "inspect", false, false, false, meter)
			if err != nil || !reused.Hit || reused.Path != first.Path {
				t.Fatalf("URL source with different descriptive platform not reused: %+v, %v", reused, err)
			}
			_, _, _, err = loadExtracted(context.Background(), root, "rook", "", url, "", root, "inspect", false, false, false, meter)
			if err == nil || !strings.Contains(err.Error(), "conflicts") {
				t.Fatalf("cached URL bypassed known OTA platform mismatch: %v", err)
			}
			if dedupCacheCount(t, root) != 1 {
				t.Fatal("URL lookup created a duplicate cache")
			}
		})
	}
}

func TestPrivateFactoryInventoryDedupAcrossSourceMetadata(t *testing.T) {
	originalPath := os.Getenv("ASP_TEST_FACTORY_CACHE")
	if originalPath == "" {
		t.Skip("owner-provided V2 factory cache required")
	}
	original, err := readCache(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "cache")
	if err := requireCacheDir(root); err != nil {
		t.Fatal(err)
	}
	first, err := publishCache(root, original.Metadata, original.Items, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	originalManifest, err := os.ReadFile(filepath.Join(first.Path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	originalSums, err := os.ReadFile(filepath.Join(first.Path, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	for i, useSameSource := range []bool{true, false} {
		different := original.Metadata
		different.Platform = "unlisted_echo_model"
		different.FireOS = "9.9.9"
		different.Firmware = "distinct-build"
		if !useSameSource {
			different.SourceSHA256 = hashString("alternate-source-identity")
		}
		reused, err := publishCache(root, different, original.Items, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reused.Hit || reused.Path != first.Path || dedupCacheCount(t, root) != 1 {
			t.Fatalf("owner-supplied factory inventory duplicated on pass %d", i)
		}
	}
	afterManifest, err := os.ReadFile(filepath.Join(first.Path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	afterSums, err := os.ReadFile(filepath.Join(first.Path, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(originalManifest, afterManifest) || !bytes.Equal(originalSums, afterSums) {
		t.Fatal("owner-supplied factory cache was rewritten")
	}
	t.Logf("reused %d real factory artifacts (unchanged SHA256SUMS)", len(original.Items))
}

func TestCachedLocalOTAKnownIdentityAcrossLabels(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	if err := requireCacheDir(root); err != nil {
		t.Fatal(err)
	}
	path := syntheticOTAIdentity(t, "post-build=Amazon/donut/donut_puffin:6.0/TEST/123\\n", "")
	digest, err := hashOTA(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := publishCache(root, cacheMetadata{
		Platform: "rook", Firmware: "original-label", SourceSHA256: digest, OTAFormat: "ZIP_FILE_TREE",
	}, map[string][]byte{"system/lib/libasp.so": []byte("test-data")}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(dir, "factory.zip")
	if err := os.WriteFile(local, raw, 0600); err != nil {
		t.Fatal(err)
	}
	meter := newMeter()
	defer meter.Close()
	const url = "https://example.invalid/factory.zip"
	found, _, err := cachedLocalOTAForURL(context.Background(), root, dir, "donut_puffin", url, meter)
	if err != nil || !found.Hit || found.Path != initial.Path {
		t.Fatalf("local candidate rejected because of cached platform label: %v", err)
	}
	_, _, err = cachedLocalOTAForURL(context.Background(), root, dir, "rook", url, meter)
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("local candidate bypassed known source platform: %v", err)
	}
	if dedupCacheCount(t, root) != 1 {
		t.Fatal("local candidate generated duplicate cache")
	}
}

func TestUnidentifiedOTACacheHitAcrossOptionalLabels(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	if err := requireCacheDir(root); err != nil {
		t.Fatal(err)
	}
	source := syntheticOTAIdentity(t, "", "ro.build.version.name=Test\n")
	sourceHash, err := hashOTA(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	original, err := publishCache(root, cacheMetadata{
		Platform: "unlisted_original", Firmware: "test-build", SourceSHA256: sourceHash,
		OTAFormat: "ZIP_FILE_TREE",
	}, map[string][]byte{"system/lib/libasp.so": []byte("factory")}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	meter := newMeter()
	defer meter.Close()
	reused, _, _, err := loadExtracted(context.Background(), root, "different_optional_label", source, "", "", root, "inspect", false, false, false, meter)
	if err != nil || !reused.Hit || reused.Path != original.Path {
		t.Fatalf("same source hash was not reused when codename unavailable: %v", err)
	}
	if dedupCacheCount(t, root) != 1 {
		t.Fatal("repeated extraction created another directory")
	}
}
