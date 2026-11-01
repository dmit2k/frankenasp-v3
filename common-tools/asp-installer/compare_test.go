package main

import (
	"os"
	"path/filepath"
	"testing"
)

func fixtureFile(t *testing.T, root, path string, data []byte) {
	t.Helper()
	dest := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(dest, data, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestCheckVendorOnlyUsedFiles(t *testing.T) {
	root := t.TempDir()
	items := map[string][]byte{"vendor/lib/libasp.so": []byte("factory-A"), "vendor/etc/audio-algorithms/AFE.cfg": []byte("factory-config"), "system/lib/libc.so": []byte("ignore-system")}
	fixtureFile(t, root, "lib/libasp.so", items["vendor/lib/libasp.so"])
	fixtureFile(t, root, "etc/audio-algorithms/AFE.cfg", items["vendor/etc/audio-algorithms/AFE.cfg"])
	fixtureFile(t, root, "etc/audio-algorithms/UNRELATED.cfg", []byte("unchanged ignored"))
	got := compareVendor(items, root)
	if !got.Matching || got.Compared != 2 {
		t.Fatalf("expected matching vendor subset: %+v", got)
	}
	fixtureFile(t, root, "lib/libasp.so", []byte("factory-B"))
	got = compareVendor(items, root)
	if got.Matching || len(got.Changed) != 1 || got.Changed[0].Path != "lib/libasp.so" {
		t.Fatalf("wrong vendor diff: %+v", got)
	}
}
func TestCompareInstalledRuntime(t *testing.T) {
	root := t.TempDir()
	assets := t.TempDir()
	factory := map[string][]byte{"system/bin/linker": []byte("linker"), "system/lib/libasp.so": []byte("asp"), "vendor/etc/audio-algorithms/AFE.cfg": []byte("config")}
	for name, data := range factory {
		fixtureFile(t, root, "lib/rootfs/"+name, data)
	}
	fixtureFile(t, root, "lib/rootfs/factory/asp1-child", []byte("helper"))
	fixtureFile(t, assets, "asp1-child", []byte("helper"))
	fixtureFile(t, root, "bin/external-asp", []byte("launcher"))
	fixtureFile(t, assets, "external-asp", []byte("launcher"))
	fixtureFile(t, root, "etc/launcher.profile", []byte(vendorBundled))
	for name, target := range aliasTargets() {
		p := filepath.Join(root, "lib/rootfs/proc", name)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(target, p); e != nil {
			t.Fatal(e)
		}
	}
	got, e := compareInstalled("rook", assets, factory, "bundled", root)
	if e != nil || !got.Matching {
		t.Fatalf("expected same rootfs: %+v %v", got, e)
	}
	fixtureFile(t, root, "lib/rootfs/system/lib/libasp.so", []byte("new factory"))
	got, e = compareInstalled("rook", assets, factory, "bundled", root)
	if e != nil || got.Matching || len(got.Changed) != 1 {
		t.Fatalf("factory change not detected: %+v %v", got, e)
	}
	fixtureFile(t, root, "lib/rootfs/system/lib/extra.so", []byte("unexpected"))
	got, e = compareInstalled("rook", assets, factory, "bundled", root)
	if e != nil || len(got.Changed) != 2 {
		t.Fatalf("unexpected factory file undetected: %+v %v", got, e)
	}
}
func TestProfileModes(t *testing.T) {
	if profileForMode("live") == profileForMode("bundled") {
		t.Fatal("factory profiles must differ")
	}
	if profileForMode("bundled") != vendorBundled {
		t.Fatal("factory profile changed")
	}
	if !generationName.MatchString(".external-asp-generation-v3-20261101T153000Z-aabbccddeeff") {
		t.Fatal("generation name broken")
	}
	if generationName.MatchString("../external-asp") {
		t.Fatal("unsafe generation name")
	}
}
