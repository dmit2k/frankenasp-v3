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
	for _, choice := range []string{"", "PS75741-7080", "athena-PS75741-7080"} {
		selected, e := selectFirmwareCache(base, "athena", found.Metadata.HelperSHA256, choice)
		if e != nil || selected.Path != found.Path || !selected.Hit {
			t.Fatalf("selection %q: %+v %v", choice, selected, e)
		}
	}
	for _, choice := range []string{"PS75741-9999", "athena-PS75741-7080-variant-2"} {
		_, e := selectFirmwareCache(base, "athena", found.Metadata.HelperSHA256, choice)
		if e == nil {
			t.Fatalf("missing firmware %q selected", choice)
		}
	}
	_, e := selectFirmwareCache(base, "rook", found.Metadata.HelperSHA256, "")
	if e == nil {
		t.Fatal("wrong platform selected")
	}
	_, e = selectFirmwareCache(base, "athena", hashString("another helper"), "")
	if e == nil {
		t.Fatal("wrong helper selected")
	}
}
func TestFirmwareWithoutOTAAutoAndExplicit(t *testing.T) {
	base, found := fixtureCache(t)
	for _, firmware := range []string{"", "PS75741-7080"} {
		var out bytes.Buffer
		meter := &progressMeter{out: &out, done: make(chan struct{})}
		selected, path, cleanup, e := loadExtracted(context.Background(), base, "athena", "", "", firmware, t.TempDir(), "inspect", []byte("fake helper"), false, false, false, meter)
		meter.Close()
		if e != nil || selected.Path != found.Path || path != "" || cleanup != nil {
			t.Fatalf("firmware %q failed: %v", firmware, e)
		}
		if !strings.Contains(out.String(), "Using verified firmware cache") {
			t.Fatalf("missing cache-hit message: %s", out.String())
		}
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
	_, e = selectFirmwareCache(base, "athena", found.Metadata.HelperSHA256, "")
	if e == nil || !strings.Contains(e.Error(), "multiple firmware caches") {
		t.Fatalf("ambiguous auto selection accepted: %v", e)
	}
	_, e = selectFirmwareCache(base, "athena", found.Metadata.HelperSHA256, "PS75741-7080")
	if e == nil {
		t.Fatal("ambiguous firmware label accepted")
	}
	selected, e := selectFirmwareCache(base, "athena", found.Metadata.HelperSHA256, filepath.Base(second.Path))
	if e != nil || selected.Path != second.Path {
		t.Fatalf("variant not selected: %v", e)
	}
}
func TestOTACacheCopiesLocalSourceAndStatusInventory(t *testing.T) {
	base, found := fixtureCache(t)
	source := filepath.Join(t.TempDir(), "update.bin")
	if e := os.WriteFile(source, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	dest, e := saveOTAArchive(context.Background(), found, source, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	if want := filepath.Join(base, "ota-images", "athena-PS75741-7080.bin"); dest != want {
		t.Fatalf("unexpected OTA cache path %s", dest)
	}
	if raw, e := os.ReadFile(source); e != nil || string(raw) != "fake OTA contents" {
		t.Fatal("original OTA modified")
	}
	if _, e = archiveExisting(context.Background(), found); e != nil {
		t.Fatal(e)
	}
	extracted, images := cacheInventory(context.Background(), base)
	if len(extracted) != 1 || extracted[0].State != "ready" || extracted[0].OTAImage != dest {
		t.Fatalf("cache status incorrect: %+v", extracted)
	}
	if len(images) != 1 || images[0].State != "ready" || images[0].Path != dest {
		t.Fatalf("OTA status incorrect: %+v", images)
	}
}
func TestOTACacheRefusesModification(t *testing.T) {
	base, found := fixtureCache(t)
	source := filepath.Join(t.TempDir(), "update.bin")
	if e := os.WriteFile(source, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	dest, e := saveOTAArchive(context.Background(), found, source, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(dest, []byte("tampered!"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = archiveExisting(context.Background(), found); e == nil {
		t.Fatal("modified OTA accepted")
	}
	_, images := cacheInventory(context.Background(), base)
	if len(images) != 1 || images[0].State != "invalid" {
		t.Fatalf("tampered OTA status: %+v", images)
	}
	_, e = saveOTAArchive(context.Background(), found, source, false, nil)
	if e == nil {
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
	source := filepath.Join(t.TempDir(), "update-kindle-rook_user_690918120.bin")
	if e := os.WriteFile(source, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := saveOTAArchive(context.Background(), found, source, false, nil); e != nil {
		t.Fatal(e)
	}
	if e := attachOriginalOTAName(found, source); e != nil {
		t.Fatal(e)
	}
	fromURL, e := cacheForURLFilename(context.Background(), base, "athena", found.Metadata.HelperSHA256, "https://example.invalid/path/update-kindle-rook_user_690918120.bin")
	if e != nil || fromURL.Path != found.Path {
		t.Fatalf("URL should reuse cached archive: %v", e)
	}
	if _, e = cacheForURLFilename(context.Background(), base, "athena", found.Metadata.HelperSHA256, "https://example.invalid/path/other.bin"); !errors.Is(e, os.ErrNotExist) {
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
