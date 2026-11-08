package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

func integrationARM32(interpreter bool) []byte {
	b := make([]byte, 256)
	copy(b[:4], "\x7fELF")
	b[4], b[5], b[6] = 1, 1, 1
	binary.LittleEndian.PutUint16(b[16:], 2)
	binary.LittleEndian.PutUint16(b[18:], 40)
	binary.LittleEndian.PutUint32(b[20:], 1)
	binary.LittleEndian.PutUint32(b[28:], 52)
	binary.LittleEndian.PutUint32(b[36:], 0x05000000)
	binary.LittleEndian.PutUint16(b[42:], 32)
	ph := b[52:]
	binary.LittleEndian.PutUint32(ph[0:], 1)
	binary.LittleEndian.PutUint32(ph[16:], uint32(len(b)))
	if interpreter {
		binary.LittleEndian.PutUint16(b[44:], 2)
		linker := "/system/bin/linker\x00"
		copy(b[180:], linker)
		ph = b[84:]
		binary.LittleEndian.PutUint32(ph[0:], 3)
		binary.LittleEndian.PutUint32(ph[4:], 180)
		binary.LittleEndian.PutUint32(ph[16:], uint32(len(linker)))
	} else {
		binary.LittleEndian.PutUint16(b[44:], 1)
	}
	return b
}

func TestThirdPlatformReleaseToRootfsAndComparison(t *testing.T) {
	p := asp.V4Package{
		Platform: "9_test-model", Version: "3.6.test",
		Runtime:   asp.ReleaseRuntime{VendorModes: []string{"bundled"}, LibrarySearch: []string{"system/lib", "vendor/lib"}},
		Installer: integrationARM32(false), License: []byte("synthetic public license"),
		Launcher: integrationARM32(false), Child: integrationARM32(true),
		Data: []asp.ReleaseResource{
			{Path: "assets/channel-map", InstallTo: "lib/rootfs/factory/channel-map", Bytes: []byte("platform-owned, opaque channel map")},
			{Path: "assets/resources/coefficients.test", InstallTo: "lib/rootfs/factory/data/coefficients.test", Bytes: []byte("synthetic resource, not factory calibration")},
		},
	}
	release, err := asp.PackV4(p)
	if err != nil {
		t.Fatal(err)
	}
	assetsDir := filepath.Join(t.TempDir(), "assets")
	if err = os.MkdirAll(assetsDir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range release.Files {
		if !strings.HasPrefix(name, "assets/") {
			continue
		}
		dest := filepath.Join(assetsDir, strings.TrimPrefix(name, "assets/"))
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(dest, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := asp.LoadAssets(assetsDir)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Platform != p.Platform || len(plan.Installed) != 4 {
		t.Fatal("missing optional resources in V4 plan")
	}
	items := map[string][]byte{
		"system/bin/linker":                          func() []byte { v := integrationARM32(false); binary.LittleEndian.PutUint16(v[16:], 3); return v }(),
		"system/lib/libasp.so":                       []byte("synthetic factory placeholder"),
		"vendor/etc/audio-algorithms/nested/afe.cfg": []byte("synthetic nested config"),
	}
	if err = asp.ValidateHelperRuntimeForPaths(plan.Files["assets/asp1-child"], items, plan.Runtime.LibrarySearch); err != nil {
		t.Fatal(err)
	}
	cacheRoot := filepath.Join(t.TempDir(), "asp-installer-cache")
	if err = requireCacheDir(cacheRoot); err != nil {
		t.Fatal(err)
	}
	meta := cacheMetadata{Platform: p.Platform, Firmware: "third-platform-fixture", SourceSHA256: hashString("independent source"), OTAFormat: "ZIP_FILE_TREE"}
	cache, err := publishCache(cacheRoot, meta, items, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cache.Metadata.Platform != p.Platform {
		t.Fatal("unknown platform factory cache cannot be selected")
	}
	selected, err := selectFirmwareCache(cacheRoot, p.Platform, "")
	if err != nil || selected.Path != cache.Path {
		t.Fatalf("unknown platform cache selection: %v", err)
	}
	// Isolated unprivileged staging uses only test-local no-op ownership calls.
	// Production always executes real os.Chown/os.Lchown before publication.
	originalChown, originalLchown := runtimeChown, runtimeLchown
	runtimeChown = func(string, int, int) error { return nil }
	runtimeLchown = func(string, int, int) error { return nil }
	defer func() { runtimeChown, runtimeLchown = originalChown, originalLchown }()
	stage := filepath.Join(t.TempDir(), "external-asp")
	if err = os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	miccal := []int{0, 7}
	if err = writeRootWithPlan(stage, selected.Items, plan, "ZIP_FILE_TREE", "bundled", miccal); err != nil {
		t.Fatal(err)
	}
	cmp, err := compareInstalledWithPlan(plan, selected.Items, "bundled", stage, miccal)
	if err != nil || !cmp.Matching {
		t.Fatalf("staged rootfs differs: %+v %v", cmp, err)
	}
	sz, err := predictedRootfsBytesPlan(selected.Items, plan, "bundled", 4096)
	if err != nil || sz == 0 {
		t.Fatalf("invalid rootfs estimate %v %v", sz, err)
	}
	receipt, err := os.ReadFile(filepath.Join(stage, "etc/installer-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var details struct {
		MiccalAliases []int  `json:"miccal_aliases"`
		Platform      string `json:"platform"`
	}
	if err = json.Unmarshal(receipt, &details); err != nil {
		t.Fatal(err)
	}
	if details.Platform != p.Platform || len(details.MiccalAliases) != 2 || details.MiccalAliases[1] != 7 {
		t.Fatal("incorrect receipt alias inventory")
	}
	if bytes.Contains(receipt, []byte("private-calibration")) {
		t.Fatal("calibration leaked into receipt")
	}
	if _, err = os.Lstat(filepath.Join(stage, "lib/rootfs/proc/idme/miccal.2")); !os.IsNotExist(err) {
		t.Fatal("fabricated miccal.2")
	}
	// The installed receipt must match the dynamically discovered inventory.
	var tampered map[string]any
	if err = json.Unmarshal(receipt, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered["miccal_aliases"] = []int{0, 1, 7}
	changed, _ := json.Marshal(tampered)
	if err = os.WriteFile(filepath.Join(stage, "etc/installer-receipt.json"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	cmp, err = compareInstalledWithPlan(plan, selected.Items, "bundled", stage, miccal)
	if err != nil || cmp.Matching {
		t.Fatalf("tampered receipt alias inventory undetected: %+v %v", cmp, err)
	}
	if err = os.WriteFile(filepath.Join(stage, "etc/installer-receipt.json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
	// Old installed generations with no explicit alias inventory stay recognized.
	delete(tampered, "miccal_aliases")
	legacy, _ := json.Marshal(tampered)
	if err = os.WriteFile(filepath.Join(stage, "etc/installer-receipt.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	cmp, err = compareInstalledWithPlan(plan, selected.Items, "bundled", stage, miccal)
	if err != nil || !cmp.Matching {
		t.Fatalf("legacy receipt must stay compatible: %+v %v", cmp, err)
	}
	resource := filepath.Join(stage, "lib/rootfs/factory/channel-map")
	if err = os.WriteFile(resource, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	cmp, err = compareInstalledWithPlan(plan, selected.Items, "bundled", stage, miccal)
	if err != nil || cmp.Matching {
		t.Fatalf("tampered resource not reported: %+v %v", cmp, err)
	}
}
