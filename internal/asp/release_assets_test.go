package asp

import (
	"os"
	"path/filepath"
	"testing"
)

func writeReleasePlanAssets(t *testing.T, base string, v VerifiedRelease) {
	t.Helper()
	for name, b := range v.Files {
		if len(name) < 7 || name[:7] != "assets/" {
			continue
		}
		dest := filepath.Join(base, name[7:])
		if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(dest, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}

func TestLoadAssetsSingleVerifiedSnapshotAndData(t *testing.T) {
	p := syntheticPackage("9_test-model")
	p.Data = []ReleaseResource{{Path: "assets/nested/channel-map", InstallTo: "lib/rootfs/factory/custom-channel-map", Bytes: []byte("non-Athena opaque data")}}
	v, e := PackV4(p)
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	writeReleasePlanAssets(t, root, v)
	plan, e := LoadAssets(root)
	if e != nil {
		t.Fatal(e)
	}
	if plan.Platform != p.Platform || len(plan.Installed) != 3 || plan.AllowsVendor("live") {
		t.Fatal("wrong runtime asset plan")
	}
	file := filepath.Join(root, "nested", "channel-map")
	if e = os.WriteFile(file, []byte("modified after verification"), 0600); e != nil {
		t.Fatal(e)
	}
	if string(plan.Files["assets/nested/channel-map"]) != "non-Athena opaque data" {
		t.Fatal("verified bytes changed after asset mutation")
	}
	if _, e = LoadAssets(root); e == nil {
		t.Fatal("modified resource accepted")
	}
}

func TestLoadAssetsRejectsSymlinkComponents(t *testing.T) {
	p := syntheticPackage("9_test-model")
	p.Data = []ReleaseResource{{Path: "assets/nested/resource", InstallTo: "lib/rootfs/factory/resource", Bytes: []byte("not private")}}
	v, e := PackV4(p)
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	writeReleasePlanAssets(t, root, v)
	original := filepath.Join(root, "nested", "resource")
	if e := os.Remove(original); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("/etc/passwd", original); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadAssets(root); e == nil {
		t.Fatal("symlink final resource accepted")
	}
	if e := os.Remove(original); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(filepath.Join(root, "nested")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(t.TempDir(), filepath.Join(root, "nested")); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadAssets(root); e == nil {
		t.Fatal("symlink intermediate resource directory accepted")
	}
	if e := os.Remove(filepath.Join(root, "nested")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(t.TempDir(), root+"-symlink"); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadAssets(root + "-symlink"); e == nil {
		t.Fatal("symlink assets directory accepted")
	}
}
