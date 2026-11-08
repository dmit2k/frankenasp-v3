package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

// Exercises real production mutation functions. Only device-dependent actions
// and the deployment root/UID are substituted; all filesystem mutations,
// staging, tar backup, atomic exchange, rollback and removal remain production.
func TestProductionLifecycleDisposable(t *testing.T) {
	parent := t.TempDir()
	calibration := filepath.Join(t.TempDir(), "idme")
	if err := os.MkdirAll(calibration, 0700); err != nil {
		t.Fatal(err)
	}
	for index, value := range map[string]string{"0": "secret-zero", "2": "", "7": "secret-seven"} {
		node := filepath.Join(calibration, "miccal."+index)
		if err := os.Mkdir(node, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(node, "value"), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	originalDestination, originalChown, originalLchown := destination, runtimeChown, runtimeLchown
	originalOwner, originalRoot := runtimeOwnerUID, runtimeIsRoot
	originalDiscovery, originalScan, originalGeneration := discoverMiccal, scanActivity, generationID
	destination = filepath.Join(parent, "external-asp")
	runtimeOwnerUID = uint32(os.Geteuid())
	runtimeIsRoot = func() bool { return true }
	runtimeChown = func(string, int, int) error { return nil }
	runtimeLchown = func(string, int, int) error { return nil }
	discoverMiccal = func() ([]int, error) { return discoverMiccalAt(calibration, uint32(os.Geteuid())) }
	scanActivity = func([]string) (activity, error) { return activity{}, nil }
	t.Cleanup(func() {
		destination, runtimeChown, runtimeLchown = originalDestination, originalChown, originalLchown
		runtimeOwnerUID, runtimeIsRoot = originalOwner, originalRoot
		discoverMiccal, scanActivity, generationID = originalDiscovery, originalScan, originalGeneration
	})
	m := newMeter()
	m.Out = io.Discard
	defer m.Close()

	createPlan := func(variant byte) asp.AssetPlan {
		t.Helper()
		launcher, child := integrationARM32(false), integrationARM32(true)
		launcher[230], child[230] = variant, variant
		p := asp.V4Package{
			Platform: "9_disposable", Version: "3.6.lifecycle",
			Runtime:   asp.ReleaseRuntime{VendorModes: []string{"bundled"}, LibrarySearch: []string{"system/lib", "vendor/lib"}},
			Installer: integrationARM32(false), License: []byte("test license"),
			Launcher: launcher, Child: child,
			Data: []asp.ReleaseResource{{Path: "assets/channel-map", InstallTo: "lib/rootfs/factory/channel-map", Bytes: []byte{'o', 'p', 'a', 'q', 'u', 'e', variant}}},
		}
		release, err := asp.PackV4(p)
		if err != nil {
			t.Fatal(err)
		}
		assets := filepath.Join(t.TempDir(), "assets")
		for name, data := range release.Files {
			if !strings.HasPrefix(name, "assets/") {
				continue
			}
			path := filepath.Join(assets, strings.TrimPrefix(name, "assets/"))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		plan, err := asp.LoadAssets(assets)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	first, changed := createPlan(1), createPlan(2)
	items := map[string][]byte{
		"system/bin/linker":                         integrationARM32(false),
		"system/lib/libasp.so":                      []byte("synthetic factory, not a public asset"),
		"vendor/etc/audio-algorithms/nested/eq.cfg": []byte("factory config"),
	}
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	if err := requireCacheDir(cacheRoot); err != nil {
		t.Fatal(err)
	}
	metadata := cacheMetadata{Platform: first.Platform, Firmware: "test-firmware", SourceSHA256: hashString("local source"), OTAFormat: "ZIP_FILE_TREE"}
	cached, err := publishCache(cacheRoot, metadata, items, false, m)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := selectFirmwareCache(cacheRoot, first.Platform, "")
	if err != nil || selected.Path != cached.Path {
		t.Fatalf("Cache V2 not reusable without OTA: %v", err)
	}
	items = selected.Items
	check := func(plan asp.AssetPlan, matching bool) {
		t.Helper()
		cmp, err := compareInstalledWithPlan(plan, items, "bundled", destination, []int{0, 7})
		if err != nil || cmp.Matching != matching {
			t.Fatalf("check matching=%v: %+v %v", matching, cmp, err)
		}
	}
	readFile := func(root, name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	run := func(plan asp.AssetPlan, update, skipBackup bool) (map[string]any, error) {
		return installOrUpdatePlan(plan, items, "cache-v2", "bundled", update, skipBackup, false, m)
	}
	firstResult, err := run(first, false, false)
	if err != nil || firstResult["changed"] != true {
		t.Fatalf("fresh install: %+v %v", firstResult, err)
	}
	if len(knownGenerations()) != 0 || len(backupArchives()) != 0 || installedVersion()["installed"] != true {
		t.Fatal("fresh install created wrong generation/status/backup")
	}
	check(first, true)
	for _, absent := range []string{"lib/rootfs/proc/idme/miccal.2", "lib/rootfs/proc/idme/miccal.1"} {
		if _, err := os.Lstat(filepath.Join(destination, absent)); !os.IsNotExist(err) {
			t.Fatalf("fabricated sparse alias %s", absent)
		}
	}
	receipt := readFile(destination, "etc/installer-receipt.json")
	if bytes.Contains(receipt, []byte("secret-zero")) || bytes.Contains(receipt, []byte("secret-seven")) {
		t.Fatal("calibration values were saved in receipt")
	}
	noop, err := run(first, true, false)
	if err != nil || noop["changed"] == true || len(knownGenerations()) != 0 || len(backupArchives()) != 0 {
		t.Fatalf("no-op mutated generations: %+v %v", noop, err)
	}

	// A pre-miccal-inventory V3 receipt remains valid; replacement retains it.
	var oldReceipt map[string]any
	if err := json.Unmarshal(receipt, &oldReceipt); err != nil {
		t.Fatal(err)
	}
	delete(oldReceipt, "miccal_aliases")
	delete(oldReceipt, "miccal_inventory_version")
	legacyReceipt, _ := json.Marshal(oldReceipt)
	if err := os.WriteFile(filepath.Join(destination, "etc/installer-receipt.json"), legacyReceipt, 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateManaged(destination); err != nil {
		t.Fatalf("legacy receipt rejected: %v", err)
	}
	updated, err := run(changed, true, false)
	if err != nil || updated["changed"] != true || updated["backup"] == nil {
		t.Fatalf("changed update: %+v %v", updated, err)
	}
	if len(knownGenerations()) != 1 || len(backupArchives()) != 1 {
		t.Fatal("missing retained previous generation or backup")
	}
	check(changed, true)
	status := statusData(cacheRoot)
	if status["current"].(map[string]any)["installed"] != true ||
		len(status["generations"].([]map[string]any)) != 1 ||
		len(status["backup_archives"].([]string)) != 1 {
		t.Fatalf("status after update: %+v", status)
	}
	check(first, false)
	if !bytes.Equal(readFile(knownGenerations()[0], "bin/external-asp"), first.Files["assets/external-asp"]) {
		t.Fatal("old native generation bytes not retained")
	}
	if bytes.Contains(readFile(destination, "etc/installer-receipt.json"), []byte("secret-seven")) {
		t.Fatal("calibration value entered new receipt")
	}

	// Check archive content, not merely that a tar filename exists.
	f, err := os.Open(filepath.Join(parent, backupFolder, backupArchives()[0]))
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	foundOld := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "external-asp/bin/external-asp" {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			foundOld = bytes.Equal(b, first.Files["assets/external-asp"])
		}
	}
	_ = gz.Close()
	_ = f.Close()
	if !foundOld {
		t.Fatal("archive lacks original native bytes")
	}
	rolled, err := rollbackGeneration(filepath.Base(knownGenerations()[0]), false, m)
	if err != nil || rolled["saved_replaced_generation"] == nil {
		t.Fatalf("rollback: %+v %v", rolled, err)
	}
	check(first, true)
	if len(knownGenerations()) != 1 || len(backupArchives()) != 2 {
		t.Fatal("rollback lost replaced generation or backup")
	}
	// Staging failure before exchange must not touch the installed runtime.
	broken := changed
	broken.Files = make(map[string][]byte, len(changed.Files))
	for k, v := range changed.Files {
		if k != "assets/channel-map" {
			broken.Files[k] = v
		}
	}
	if _, err := run(broken, true, false); err == nil {
		t.Fatal("failed staging did not return error")
	}
	check(first, true)
	if len(incompleteOperations()) != 0 || len(backupArchives()) != 2 {
		t.Fatal("prepublication failure left a stage or backup")
	}

	// Force the real post-exchange generation rename to collide. No exchange,
	// rename, file copy or backup function is replaced by a mock.
	collision := knownGenerations()[0]
	generationID = func() string { return strings.TrimPrefix(filepath.Base(collision), generationPrefix) }
	if _, err := run(changed, true, true); err == nil || !strings.Contains(err.Error(), "generation rename") {
		t.Fatalf("post-exchange interruption not recognized: %v", err)
	}
	check(changed, true)
	pending := incompleteOperations()
	if len(pending) != 1 || !strings.HasPrefix(pending[0], ".external-asp-stage-v3-") {
		t.Fatalf("missing recoverable old stage: %v", pending)
	}
	if _, err := run(changed, true, true); err == nil || !strings.Contains(err.Error(), "manual recovery required") {
		t.Fatalf("interrupted operation was not blocked: %v", err)
	}
	interruptedStatus := statusData(cacheRoot)
	if len(interruptedStatus["incomplete_operations"].([]string)) != 1 ||
		interruptedStatus["current"].(map[string]any)["installed"] != true {
		t.Fatalf("status lost interrupted operation: %+v", interruptedStatus)
	}
	oldStage := filepath.Join(parent, pending[0])
	if err := validateManaged(oldStage); err != nil {
		t.Fatalf("previous runtime not recoverable: %v", err)
	}
	recovered := filepath.Join(parent, generationPrefix+"20991231T235959Z-abcdef123456")
	if err := os.Rename(oldStage, recovered); err != nil {
		t.Fatal(err)
	}
	generationID = originalGeneration
	if len(incompleteOperations()) != 0 || len(knownGenerations()) != 2 {
		t.Fatal("manual recovery did not retain both prior runtimes")
	}
	afterRecovery, err := run(changed, true, true)
	if err != nil || afterRecovery["changed"] == true {
		t.Fatalf("normal operation after recovery failed: %+v %v", afterRecovery, err)
	}
	if len(backupArchives()) != 2 {
		t.Fatal("no-op after recovery created a backup")
	}

	removed, err := uninstallAll(false, false, m)
	if err != nil || removed["backups_kept"] != true || installedVersion()["installed"] != false || len(knownGenerations()) != 0 {
		t.Fatalf("uninstall: %+v %v", removed, err)
	}
	if len(backupArchives()) != 2 {
		t.Fatal("uninstall removed preserved backups")
	}
	removedStatus := statusData(cacheRoot)
	if removedStatus["current"].(map[string]any)["installed"] != false ||
		len(removedStatus["generations"].([]map[string]any)) != 0 ||
		len(removedStatus["backup_archives"].([]string)) != 2 {
		t.Fatalf("status after uninstall: %+v", removedStatus)
	}

	// The unchanged cache may be installed again without reopening an OTA.
	selected, err = selectFirmwareCache(cacheRoot, first.Platform, "")
	if err != nil || selected.Path != cached.Path {
		t.Fatalf("factory cache unavailable for reinstall: %v", err)
	}
	items = selected.Items
	if _, err := run(first, false, false); err != nil {
		t.Fatalf("reinstall from Cache V2: %v", err)
	}
	check(first, true)
	if _, err := uninstallAll(false, false, m); err != nil {
		t.Fatal(err)
	}
	purged, err := uninstallAll(true, false, m)
	if err != nil || purged["backups_kept"] != false || len(backupArchives()) != 0 {
		t.Fatalf("explicit backup purge failed: %+v %v", purged, err)
	}
}
