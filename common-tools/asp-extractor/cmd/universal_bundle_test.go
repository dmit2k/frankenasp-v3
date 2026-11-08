package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

func syntheticBundleELF(elfInterp bool) []byte {
	b := make([]byte, 256)
	copy(b[:4], "\x7fELF")
	b[4], b[5], b[6] = 1, 1, 1
	binary.LittleEndian.PutUint16(b[16:], 2)
	binary.LittleEndian.PutUint16(b[18:], 40)
	binary.LittleEndian.PutUint32(b[20:], 1)
	binary.LittleEndian.PutUint32(b[28:], 52)
	binary.LittleEndian.PutUint32(b[36:], 0x05000000)
	binary.LittleEndian.PutUint16(b[42:], 32)
	binary.LittleEndian.PutUint32(b[52:], 1)
	binary.LittleEndian.PutUint32(b[52+16:], 256)
	count := uint16(1)
	if elfInterp {
		count = 2
		target := "/system/bin/linker\x00"
		copy(b[180:], target)
		binary.LittleEndian.PutUint32(b[84:], 3)
		binary.LittleEndian.PutUint32(b[84+4:], 180)
		binary.LittleEndian.PutUint32(b[84+16:], uint32(len(target)))
	}
	binary.LittleEndian.PutUint16(b[44:], count)
	return b
}

func TestUnknownPlatformV4BundlePreservesOriginalArchive(t *testing.T) {
	platform := "9_test-model"
	pkg, err := asp.PackV4(asp.V4Package{
		Platform: platform, Version: "3.6.test",
		Runtime:   asp.ReleaseRuntime{VendorModes: []string{"bundled"}, LibrarySearch: []string{"system/lib", "vendor/lib"}},
		Installer: syntheticBundleELF(false), License: []byte("public test license"),
		Launcher: syntheticBundleELF(false), Child: syntheticBundleELF(true),
		Data: []asp.ReleaseResource{{Path: "assets/resources/layout.dat", InstallTo: "lib/rootfs/factory/resources/layout.dat", Bytes: []byte("synthetic opaque layout")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	releasePath, err := asp.SaveRelease(dir, pkg)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "asp-installer-cache")
	if err = ensureDesktopCache(root); err != nil {
		t.Fatal(err)
	}
	source := strings.Repeat("a", 64)
	cache, err := publishCache(root, cacheMetadata{
		Platform: platform, Firmware: "test-firmware", OTAFormat: "ZIP_FILE_TREE", SourceSHA256: source,
	}, map[string][]byte{
		"system/bin/linker":    func() []byte { v := syntheticBundleELF(false); binary.LittleEndian.PutUint16(v[16:], 3); return v }(),
		"system/lib/libasp.so": []byte("synthetic factory file"),
	}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "private-bundle.tar.gz")
	o := options{platform: platform, firmware: filepath.Base(cache.Path), cacheDir: root, release: releasePath, output: output, command: "create-bundle"}
	if err = createBundle(o); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	seen := map[string][]byte{}
	for {
		h, e := tarReader.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(tarReader)
		if e != nil {
			t.Fatal(e)
		}
		seen[h.Name] = b
	}
	if !bytes.Equal(seen["release/"+pkg.Name], pkg.Original) {
		t.Fatal("bundle altered original V4 release bytes")
	}
	var manifest bundleManifest
	if err = json.Unmarshal(seen["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(pkg.Original)
	if manifest.Platform != platform || manifest.CacheSourceSHA256 != source || manifest.ReleaseSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("bundle metadata differs from verified source: %+v", manifest)
	}
	if len(seen) < 4 {
		t.Fatalf("bundle missing cache and release: %v", seen)
	}
	// The bundling process doesn't execute or transform optional data assets.
	archived, err := asp.ParseReleaseBytes(seen["release/"+pkg.Name], pkg.Name, platform)
	if err != nil || string(archived.Files["assets/resources/layout.dat"]) != "synthetic opaque layout" {
		t.Fatalf("bundle optional resource changed: %v", err)
	}
	o.platform = "wrong-platform"
	o.output = filepath.Join(t.TempDir(), "unexpected-bundle.tar.gz")
	if err = createBundle(o); err == nil {
		t.Fatal("mismatched platform bundle accepted")
	}
}
