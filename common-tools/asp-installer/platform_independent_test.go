package main

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFactoryCacheWithAbsentAndUnlistedPlatform(t *testing.T) {
	base := filepath.Join(t.TempDir(), "asp-installer-cache")
	if err := requireCacheDir(base); err != nil {
		t.Fatal(err)
	}
	items := map[string][]byte{"system/lib/libasp.so": []byte("factory-only-fixture")}
	unknown := cacheMetadata{
		Platform: "", Firmware: "generic-test", SourceSHA256: hashString("unidentified-source"),
		OTAFormat: "ZIP_FILE_TREE",
	}
	neutral, err := publishCache(base, unknown, items, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(neutral.Path), "factory-sha256-"+unknown.SourceSHA256[:12]) {
		t.Fatalf("non-deterministic neutral cache filename: %s", neutral.Path)
	}
	if verified, err := readCache(neutral.Path); err != nil || verified.Metadata.Platform != "" {
		t.Fatalf("empty platform metadata rejected: %v", err)
	}
	unlisted := unknown
	unlisted.Platform = "new_echo_model"
	unlisted.SourceSHA256 = hashString("new-model")
	novel, err := publishCache(base, unlisted, items, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := selectFirmwareCache(base, "", filepath.Base(neutral.Path)); err != nil || selected.Path != neutral.Path {
		t.Fatalf("select unnamed platform by exact cache name: %v", err)
	}
	if selected, err := selectFirmwareCache(base, "new_echo_model", ""); err != nil || selected.Path != novel.Path {
		t.Fatalf("select arbitrary platform by metadata: %v", err)
	}
	if _, err := selectFirmwareCache(base, "", ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous unfiltered cache selection accepted: %v", err)
	}
	if _, err := selectFirmwareCache(base, "", "generic-test"); err == nil {
		t.Fatal("ambiguous shared firmware label accepted")
	}
	if selected, err := findCache(base, "", unknown.SourceSHA256, ""); err != nil || selected.Path != neutral.Path {
		t.Fatalf("digest selection without platform: %v", err)
	}
	if _, err := findCache(base, "other", unknown.SourceSHA256, ""); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit platform filter ignored: %v", err)
	}
}

func TestUnknownCodenameNormalization(t *testing.T) {
	codename, err := resolveCodename("9_Unknown-Model", "9_unknown-model")
	if err != nil || codename != "9_unknown-model" {
		t.Fatalf("generic codename normalization: %q, %v", codename, err)
	}
	if displayPlatform(codename) != codename {
		t.Fatal("unknown detected codename hidden from inspection")
	}
}

func TestZipTreeDetectsImplicitAudioConfigDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "factory.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(f)
	w, err := writer.Create("vendor/etc/audio-algorithms/AFE.cfg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("config")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	reader := newZipTree(&archive.Reader, "vendor")
	present, err := reader.Exists("etc/audio-algorithms")
	if err != nil || !present {
		t.Fatalf("implicit config directory not discovered: %v", err)
	}
	names, err := reader.Files("etc/audio-algorithms")
	if err != nil || len(names) != 1 || names[0] != "etc/audio-algorithms/AFE.cfg" {
		t.Fatalf("wrong config inventory: %v %v", names, err)
	}
}
