package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectFirmwareFromVerifiedCache(t *testing.T) {
	base, found := fixtureCache(t)
	for _, choice := range []string{"", "PS75741-7080", "7574.1", "athena-fireos-7574.1-build-PS75741-7080"} {
		selected, e := selectFirmwareCache(base, "athena", choice)
		if e != nil || selected.Path != found.Path || !selected.Hit {
			t.Fatalf("selection %q: %+v %v", choice, selected, e)
		}
	}
	for _, choice := range []string{"PS75741-9999", "athena-fireos-7574.1-build-PS75741-7080-variant-2"} {
		_, e := selectFirmwareCache(base, "athena", choice)
		if e == nil {
			t.Fatalf("missing firmware %q selected", choice)
		}
	}
	_, e := selectFirmwareCache(base, "rook", "")
	if e == nil {
		t.Fatal("wrong platform selected")
	}
}
func TestFirmwareWithoutOTAAutoAndExplicit(t *testing.T) {
	base, found := fixtureCache(t)
	for _, firmware := range []string{"", "PS75741-7080"} {
		var out bytes.Buffer
		meter := &progressMeter{out: &out, done: make(chan struct{})}
		selected, path, cleanup, e := loadExtracted(context.Background(), base, "athena", "", "", firmware, t.TempDir(), "inspect", false, false, false, meter)
		meter.Close()
		if e != nil || selected.Path != found.Path || path != "" || cleanup != nil {
			t.Fatalf("firmware %q failed: %v", firmware, e)
		}
		if !strings.Contains(out.String(), "Verified factory files") {
			t.Fatalf("missing cache-hit message: %s", out.String())
		}
	}
}
func TestCachedFirmwareRequiresNoOTAArchiveInEveryMode(t *testing.T) {
	base, saved := fixtureCache(t)
	for _, mode := range []string{"inspect", "install", "update", "check", "check-vendor"} {
		t.Run(mode, func(t *testing.T) {
			var output bytes.Buffer
			meter := &progressMeter{out: &output, done: make(chan struct{})}
			selected, otaPath, cleanup, err := loadExtracted(context.Background(), base, "athena", "", "", saved.Metadata.Firmware, t.TempDir(), mode, false, false, false, meter)
			meter.Close()
			if err != nil || selected.Path != saved.Path || otaPath != "" || cleanup != nil {
				t.Fatalf("cached firmware selection requires archive in %s: %+v %s %v", mode, selected, otaPath, err)
			}
			for _, unwanted := range []string{"OTA archive not available", "lstat", "ota-images/", "Factory extraction: "} {
				if strings.Contains(output.String(), unwanted) {
					t.Fatalf("irrelevant %s message %q: %s", mode, unwanted, output.String())
				}
			}
		})
	}
}

func TestFirmwareVariantsMustBeSelectedExplicitly(t *testing.T) {
	base, found := fixtureCache(t)
	alt := found.Metadata
	alt.SourceSHA256 = hashString("different OTA same firmware")
	alt.URLSHA256 = hashString("another URL")
	second, e := publishCache(base, alt, map[string][]byte{"vendor/lib/libasp.so": []byte("other")}, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	_, e = selectFirmwareCache(base, "athena", "")
	if e == nil || !strings.Contains(e.Error(), "multiple firmware caches") {
		t.Fatalf("ambiguous auto selection accepted: %v", e)
	}
	_, e = selectFirmwareCache(base, "athena", "PS75741-7080")
	if e == nil {
		t.Fatal("ambiguous firmware label accepted")
	}
	selected, e := selectFirmwareCache(base, "athena", filepath.Base(second.Path))
	if e != nil || selected.Path != second.Path {
		t.Fatalf("variant not selected: %v", e)
	}
}
func TestLocalOTAIsNeverCopiedIntoCache(t *testing.T) {
	base, found := fixtureCache(t)
	source := filepath.Join(t.TempDir(), "update.bin")
	if e := os.WriteFile(source, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := saveOTAArchive(context.Background(), found, source, false, nil); e == nil {
		t.Fatal("accepted archiving user-supplied local OTA")
	}
	if raw, e := os.ReadFile(source); e != nil || string(raw) != "fake OTA contents" {
		t.Fatal("original OTA changed")
	}
	if _, e := os.Stat(filepath.Join(base, "ota-images")); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("local OTA created an archive directory: %v", e)
	}
	var output bytes.Buffer
	meter := &progressMeter{out: &output, done: make(chan struct{})}
	got, _, _, err := loadExtracted(context.Background(), base, "athena", source, "", "", t.TempDir(), "inspect", false, false, false, meter)
	meter.Close()
	if err != nil || got.Path != found.Path {
		t.Fatalf("local OTA cache lookup: %v", err)
	}
	if _, e := os.Stat(filepath.Join(base, "ota-images")); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("cached local OTA created an archive: %v", e)
	}
	if _, images := cacheInventory(context.Background(), base); len(images) != 0 {
		t.Fatalf("unexpected retained OTA images: %+v", images)
	}
}

func TestDownloadedOTACacheRefusesModification(t *testing.T) {
	base, found := fixtureCache(t)
	dir, e := ensureOTADir(base)
	if e != nil {
		t.Fatal(e)
	}
	src := filepath.Join(dir, "asp-installer-ota-111111111111-update.bin")
	if e := os.WriteFile(src, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	dest, e := saveOTAArchive(context.Background(), found, src, true, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(dest, []byte("tampered!"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = archiveExisting(context.Background(), found); e == nil {
		t.Fatal("modified download accepted")
	}
	_, images := cacheInventory(context.Background(), base)
	if len(images) != 1 || images[0].State != "invalid" {
		t.Fatalf("tampered archive status: %+v", images)
	}
	src2 := filepath.Join(dir, "asp-installer-ota-222222222222-update.bin")
	if e := os.WriteFile(src2, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = saveOTAArchive(context.Background(), found, src2, true, nil); e == nil {
		t.Fatal("invalid cached OTA overwritten")
	}
}
func TestOTACacheDownloadedRenameNoCopies(t *testing.T) {
	base, found := fixtureCache(t)
	dir, e := ensureOTADir(base)
	if e != nil {
		t.Fatal(e)
	}
	src := filepath.Join(dir, "asp-installer-ota-123456789abc-update.bin")
	if e = os.WriteFile(src, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	dest, e := saveOTAArchive(context.Background(), found, src, true, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(src); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("download source should be atomically renamed")
	}
	if _, e = archiveExisting(context.Background(), found); e != nil {
		t.Fatalf("renamed image invalid %s %v", dest, e)
	}
}
func TestUnassociatedOTAVisible(t *testing.T) {
	base, _ := fixtureCache(t)
	dir, e := ensureOTADir(base)
	if e != nil {
		t.Fatal(e)
	}
	name := filepath.Join(dir, "asp-installer-ota-012345abcdef-update.bin")
	if e = os.WriteFile(name, []byte("incomplete extraction"), 0600); e != nil {
		t.Fatal(e)
	}
	_, images := cacheInventory(context.Background(), base)
	if len(images) != 1 || images[0].Path != name || images[0].State != "unassociated" {
		t.Fatalf("unassociated downloaded OTA hidden: %+v", images)
	}
}

func TestOriginalOTAFilenameIndexEnablesURLReuse(t *testing.T) {
	base, found := fixtureCache(t)
	dir, e := ensureOTADir(base)
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(dir, "asp-installer-ota-123456abcdef-update-kindle-rook_user_690918120.bin")
	if e := os.WriteFile(source, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := saveOTAArchive(context.Background(), found, source, true, nil); e != nil {
		t.Fatal(e)
	}
	if e := attachOriginalOTAName(found, source); e != nil {
		t.Fatal(e)
	}
	fromURL, e := cacheForURLFilename(context.Background(), base, "athena", "https://example.invalid/path/update-kindle-rook_user_690918120.bin")
	if e != nil || fromURL.Path != found.Path {
		t.Fatalf("URL should reuse cached archive: %v", e)
	}
	if _, e = cacheForURLFilename(context.Background(), base, "athena", "https://example.invalid/path/other.bin"); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("wrong source filename matched: %v", e)
	}
}
func TestPendingDownloadedOTARemainsInCache(t *testing.T) {
	base, _ := fixtureCache(t)
	imageDir, e := ensureOTADir(base)
	if e != nil {
		t.Fatal(e)
	}
	pending := filepath.Join(imageDir, "asp-installer-ota-123456abcdef-update.bin")
	if e = os.WriteFile(pending, []byte("fully transferred but not extracted"), 0600); e != nil {
		t.Fatal(e)
	}
	selected, hash, e := pendingOTAForURL(context.Background(), base, "https://example.invalid/update.bin")
	if e != nil || selected != pending || hash != hashString("fully transferred but not extracted") {
		t.Fatalf("pending archive not reused: %s %v", selected, e)
	}
	_, images := cacheInventory(context.Background(), base)
	if len(images) != 1 || images[0].Path != pending || images[0].State != "unassociated" {
		t.Fatalf("pending OTA absent from status: %+v", images)
	}
}
