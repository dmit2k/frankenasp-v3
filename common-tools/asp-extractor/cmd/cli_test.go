package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleCache(t *testing.T, root, platform, firmware, helperHash, source string) cacheResult {
	t.Helper()
	if err := ensureDesktopCache(root); err != nil {
		t.Fatal(err)
	}
	meta := cacheMetadata{Platform: platform, Firmware: firmware, SourceSHA256: source, HelperSHA256: helperHash, OTAFormat: "ZIP_FULL_BLOCK_v4", FireOS: "7574.1"}
	m, err := publishCache(root, meta, map[string][]byte{"system/bin/linker": []byte("test linker data")}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestCacheAmbiguityAndTamper(t *testing.T) {
	root := filepath.Join(t.TempDir(), "asp-installer-cache")
	helper := strings.Repeat("1", 64)
	a := sampleCache(t, root, "athena", "PS75741-7080", helper, strings.Repeat("a", 64))
	_ = sampleCache(t, root, "athena", "PS75741-7080", helper, strings.Repeat("b", 64))
	opts := options{cacheDir: root, platform: "athena"}
	if _, err := selectDesktopCache(opts, helper); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity: %v", err)
	}
	opts.firmware = filepath.Base(a.Path)
	selected, err := selectDesktopCache(opts, helper)
	if err != nil || selected.Path != a.Path {
		t.Fatalf("specific cache %v %v", selected.Path, err)
	}
	if _, err = selectDesktopCache(opts, strings.Repeat("3", 64)); err == nil {
		t.Fatal("helper mismatch accepted")
	}
	if err = os.WriteFile(filepath.Join(a.Path, "files/system/bin/linker"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = selectDesktopCache(opts, helper); err == nil {
		t.Fatal("tampered cache accepted")
	}
}
func TestRemotePathGuard(t *testing.T) {
	ok := []string{echoInstaller, echoCache + "/athena-fireos-7574.1-build-PS75741-7080/files/system/bin/linker"}
	for _, p := range ok {
		if err := remoteSafePath(p); err != nil {
			t.Errorf("valid path %s rejected: %v", p, err)
		}
	}
	bad := []string{"/", "/etc/passwd", echoRoot, echoRoot + "/../etc/shadow", echoRoot + "/installer/../../etc", echoRoot + "/foo\nbar"}
	for _, p := range bad {
		if err := remoteSafePath(p); err == nil {
			t.Errorf("unsafe path accepted: %q", p)
		}
	}
	quoted := shQuote("a'b;\necho x")
	if !strings.Contains(quoted, "'\\''") {
		t.Fatalf("bad shell quoting %q", quoted)
	}
}
func TestCLIValidation(t *testing.T) {
	cases := [][]string{
		{"extract", "--platform", "athena", "--ota", "/tmp/fake"},
		{"extract", "--platform", "athena", "--release", "x"},
		{"verify", "--platform", "wrong"},
		{"deploy-asp", "--platform", "athena", "--host", "root@x"},
		{"deploy-cache", "--platform", "athena"},
		{"create-bundle", "--platform", "athena"},
		{"verify", "--platform", "athena", "--firmware", "../../bad"},
	}
	for _, c := range cases {
		if err := runCLI(c); err == nil {
			t.Errorf("accepted invalid input %v", c)
		}
	}
}
func TestReleaseRejectsSymlinks(t *testing.T) {
	name := filepath.Join(t.TempDir(), "frankenasp-v3.2-athena-arm32.tar.gz")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	g := gzip.NewWriter(f)
	w := tar.NewWriter(g)
	if err = w.WriteHeader(&tar.Header{Name: "frankenasp-athena/assets/asp1-child", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	_ = g.Close()
	_ = f.Close()
	if _, err = loadRelease(name, "athena"); err == nil {
		t.Fatal("release symlink accepted")
	}
}
func TestBundlePreservesOriginalRelease(t *testing.T) {
	releasePath := os.Getenv("FRANKENASP_TEST_ATHENA_RELEASE")
	if releasePath == "" {
		t.Skip("set FRANKENASP_TEST_ATHENA_RELEASE for real release bundle integration")
	}
	rel, err := loadRelease(releasePath, "athena")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "asp-installer-cache")
	cached := sampleCache(t, root, "athena", "PS75741-7080", hashText(rel.Files["assets/asp1-child"]), strings.Repeat("a", 64))
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	opt := options{platform: "athena", cacheDir: root, firmware: filepath.Base(cached.Path), release: releasePath, output: out}
	if err = createBundle(opt); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gr)
	foundRelease := false
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(tr)
		if e != nil {
			t.Fatal(e)
		}
		if h.Name == "release/"+rel.Name {
			foundRelease = true
			ha := sha256.Sum256(body)
			hb := sha256.Sum256(rel.Original)
			if hex.EncodeToString(ha[:]) != hex.EncodeToString(hb[:]) {
				t.Fatal("release byte changes")
			}
		}
		if strings.Contains(h.Name, "ota-images") || strings.Contains(h.Name, ".bin") {
			t.Fatalf("raw OTA included: %s", h.Name)
		}
	}
	if !foundRelease {
		t.Fatal("release missing from bundle")
	}
	if err = createBundle(opt); err == nil {
		t.Fatal("existing bundle output overwritten")
	}
}
func TestReleaseIsNotImportedFromForeignPlatform(t *testing.T) {
	p := os.Getenv("FRANKENASP_TEST_ATHENA_RELEASE")
	if p == "" {
		t.Skip("release integration optional")
	}
	if _, err := loadRelease(p, "rook"); err == nil {
		t.Fatal("wrong target release accepted")
	}
	if _, err := loadRelease(p, "athena"); err != nil {
		t.Fatal(err)
	}
}

var _ = bytes.Equal

func TestBackupSourceIndex(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(home, "source")
	if err := os.Mkdir(src, 0700); err != nil {
		t.Fatal(err)
	}
	o := options{platform: "rook", cacheDir: filepath.Join(home, "asp-installer-cache"), backup: src}
	if err := ensureDesktopCache(o.cacheDir); err != nil {
		t.Fatal(err)
	}
	sample := cacheResult{Metadata: cacheMetadata{Firmware: "690918120", SourceSHA256: strings.Repeat("b", 64)}, Path: filepath.Join(o.cacheDir, "rook-fireos-5.5.6.9-build-690918120")}
	if err := recordBackupLink(o, sample); err != nil {
		t.Fatal(err)
	}
	if err := recordBackupLink(o, sample); err != nil {
		t.Fatal(err)
	}
	all, err := readBackupLinks(o.cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Path != src || all[0].Firmware != sample.Metadata.Firmware {
		t.Fatalf("index missing, duplicated, or wrong: %+v", all)
	}
	st, err := os.Stat(backupIndexPath(o.cacheDir))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("unsafe backup-index mode: %v %v", st, err)
	}
}
