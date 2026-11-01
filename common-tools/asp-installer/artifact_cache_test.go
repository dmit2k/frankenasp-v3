package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureCache(t *testing.T) (string, cacheResult) {
	t.Helper()
	base := filepath.Join(t.TempDir(), "cache")
	if e := requireCacheDir(base); e != nil {
		t.Fatal(e)
	}
	h := hashString("fake OTA contents")
	helper := hashString("fake helper")
	meta := cacheMetadata{Platform: "athena", Firmware: "PS75741-7080", SourceSHA256: h, HelperSHA256: helper, URLSHA256: hashString("https://example.invalid/ota.bin"), OTAFormat: "ZIP_FULL_BLOCK_v4", DependencyEdges: 27}
	items := map[string][]byte{"system/bin/linker": []byte("fake-linker"), "vendor/lib/libasp.so": []byte("fake-asp"), "system/build.prop": []byte("fake-build")}
	saved, e := publishCache(base, meta, items, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	return base, saved
}
func TestCachePublishAndVerifiedHits(t *testing.T) {
	base, saved := fixtureCache(t)
	if saved.Hit {
		t.Fatal("cache just published must not be a hit")
	}
	if !strings.Contains(filepath.Base(saved.Path), "PS75741-7080") {
		t.Fatal(saved.Path)
	}
	for _, name := range []string{"manifest.json", "SHA256SUMS", "files/system/bin/linker", "files/vendor/lib/libasp.so"} {
		if _, e := os.Stat(filepath.Join(saved.Path, name)); e != nil {
			t.Fatal(e)
		}
	}
	byOTA, e := findCache(base, "athena", saved.Metadata.HelperSHA256, saved.Metadata.SourceSHA256, "")
	if e != nil || !byOTA.Hit || len(byOTA.Items) != 3 {
		t.Fatalf("local OTA lookup: %+v %v", byOTA.Metadata, e)
	}
	byURL, e := findCache(base, "athena", saved.Metadata.HelperSHA256, "", saved.Metadata.URLSHA256)
	if e != nil || !byURL.Hit {
		t.Fatalf("URL lookup: %v", e)
	}
	if _, e = findCache(base, "rook", saved.Metadata.HelperSHA256, "", saved.Metadata.URLSHA256); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("wrong platform matched: %v", e)
	}
	if _, e = findCache(base, "athena", hashString("different helper"), "", ""); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("wrong helper matched: %v", e)
	}
}
func TestCacheDetectsModifiedMissingAndExtraFiles(t *testing.T) {
	for _, change := range []string{"modify", "missing", "extra", "symlink", "bad-manifest"} {
		t.Run(change, func(t *testing.T) {
			base, saved := fixtureCache(t)
			p := filepath.Join(saved.Path, "files", "vendor", "lib", "libasp.so")
			switch change {
			case "modify":
				if e := os.WriteFile(p, []byte("changed"), 0600); e != nil {
					t.Fatal(e)
				}
			case "missing":
				if e := os.Remove(p); e != nil {
					t.Fatal(e)
				}
			case "extra":
				if e := os.WriteFile(filepath.Join(saved.Path, "files", "extra-file"), []byte("unexpected"), 0600); e != nil {
					t.Fatal(e)
				}
			case "symlink":
				if e := os.Remove(p); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink("/etc/passwd", p); e != nil {
					t.Fatal(e)
				}
			case "bad-manifest":
				if e := os.WriteFile(filepath.Join(saved.Path, "manifest.json"), []byte("{}"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			_, e := findCache(base, "athena", saved.Metadata.HelperSHA256, saved.Metadata.SourceSHA256, "")
			if change == "bad-manifest" {
				// Malformed metadata cannot masquerade as a matching cache.
				if e == nil {
					t.Fatal("invalid manifest accepted")
				}
			} else if e == nil || !strings.Contains(e.Error(), "invalid") {
				t.Fatalf("tampered cache accepted or error unclear: %v", e)
			}
		})
	}
}
func TestCacheRefreshAtomicExchange(t *testing.T) {
	base, saved := fixtureCache(t)
	newItems := map[string][]byte{"system/bin/linker": []byte("new-version"), "vendor/lib/libasp.so": []byte("new-asp")}
	result, e := publishCache(base, saved.Metadata, newItems, true, nil)
	if e != nil {
		t.Fatal(e)
	}
	if result.Path != saved.Path || result.Hit {
		t.Fatal("refresh did not replace same identity")
	}
	if string(result.Items["system/bin/linker"]) != "new-version" {
		t.Fatal("old cache survived refresh")
	}
	entries, e := os.ReadDir(base)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range entries {
		if strings.HasPrefix(v.Name(), ".cache-building-") {
			t.Fatal("old cache stage remained")
		}
	}
}
func TestCachedSourceSkipsExtractionAndDownload(t *testing.T) {
	base, saved := fixtureCache(t)
	var output bytes.Buffer
	meter := &progressMeter{out: &output, done: make(chan struct{})}
	result, downloaded, cleanup, e := loadExtracted(context.Background(), base, "athena", "", "https://example.invalid/ota.bin", "", t.TempDir(), "check-vendor", []byte("fake helper"), false, false, false, meter)
	meter.Close()
	if e != nil || !result.Hit || downloaded != "" || cleanup != nil {
		t.Fatalf("cache failed to skip network: %v %v", result.Hit, e)
	}
	if result.Path != saved.Path {
		t.Fatal("wrong cached version")
	}
	if !strings.Contains(output.String(), "no download or partition scan") {
		t.Fatal(output.String())
	}
}
func TestFirmwareLabelFromVerifiedMetadata(t *testing.T) {
	if got := firmwareLabel("Amazon/athena/athena:9/PS75741.7080N/0036274153472:user/amz", "0036274153604"); got != "PS75741-7080" {
		t.Fatal(got)
	}
	if got := firmwareLabel("", "0036274153604"); got != "build-0036274153604" {
		t.Fatal(got)
	}
}

func TestCacheLockHonorsCancellation(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	first, e := cacheLock(context.Background(), base)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, e = cacheLock(ctx, base)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("concurrent lock should be cancelable: %v", e)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("canceled cache lock took too long")
	}
	first()
	second, e := cacheLock(context.Background(), base)
	if e != nil {
		t.Fatal(e)
	}
	second()
}

func TestReadableFirmwareCacheNamesAndNumberedVariants(t *testing.T) {
	base, first := fixtureCache(t)
	if actual := filepath.Base(first.Path); actual != "athena-fireos-7574.1-build-PS75741-7080" {
		t.Fatalf("unreadable cache directory: %q", actual)
	}
	alt := first.Metadata
	alt.SourceSHA256 = hashString("a distinct OTA for same firmware")
	alt.URLSHA256 = hashString("https://example.invalid/other.bin")
	second, e := publishCache(base, alt, map[string][]byte{"vendor/lib/libasp.so": []byte("other factory binary")}, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	if actual := filepath.Base(second.Path); actual != "athena-fireos-7574.1-build-PS75741-7080-variant-2" {
		t.Fatalf("unexpected variant name: %q", actual)
	}
	if _, e := os.Stat(first.Path); e != nil {
		t.Fatalf("original variant lost: %v", e)
	}
	chosen, e := findCache(base, "athena", alt.HelperSHA256, alt.SourceSHA256, "")
	if e != nil || chosen.Path != second.Path {
		t.Fatalf("variant lookup failed: %+v %v", chosen, e)
	}
	if !cacheMatchesName("athena-PS75741-7080-variant-2", alt) {
		t.Fatal("valid readable variant name rejected")
	}
	for _, bad := range []string{"athena-PS75741-7080-variant-1", "athena-PS75741-7080-variant-02", "athena-PS75741-7080-variant-x", "athena-PS75741-7080-deadbeef"} {
		if cacheMatchesName(bad, alt) {
			t.Errorf("invalid variant name accepted: %s", bad)
		}
	}
}
func TestRookFirmwareReadableLabel(t *testing.T) {
	got := firmwareLabel("Amazon/rook/rook:5.1.1/LVY48F/304.6.9.0_user_690918120:user/release-keys", "")
	if got != "690918120" {
		t.Fatalf("incorrect rook release label: %s", got)
	}
}
