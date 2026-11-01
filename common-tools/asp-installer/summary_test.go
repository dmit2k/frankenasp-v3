package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestStatusHumanSummary(t *testing.T) {
	data := map[string]any{
		"current":               map[string]any{"installed": true, "path": "/data/techo5-linux/external-asp", "release_revision": "abc123", "vendor_mode": "bundled", "receipt": map[string]any{"platform": "rook"}},
		"generations":           []map[string]any{{"generation": "old", "valid": true}},
		"backup_archives":       []string{"backup.tar.gz"},
		"incomplete_operations": []string{},
	}
	var out bytes.Buffer
	printStatus(&out, data)
	for _, fragment := range []string{"Files: present (running/active state NOT checked)", "Installed target: Echo Spot (1st Gen, 2017) [rook]", "FrankenASP revision: abc123", "Vendor source: bundled", "Saved generations: 1", "Backup archives: 1", "status-json"} {
		if !strings.Contains(out.String(), fragment) {
			t.Errorf("missing %q in %s", fragment, out.String())
		}
	}
	if strings.Contains(out.String(), "{") {
		t.Fatal("human status emitted JSON")
	}
}
func TestStatusWarnsOnInvalidRuntime(t *testing.T) {
	data := map[string]any{
		"current":               map[string]any{"installed": false, "error": "unknown launcher profile"},
		"generations":           []map[string]any{},
		"backup_archives":       []string{},
		"incomplete_operations": []string{"unfinished-stage"},
	}
	var out bytes.Buffer
	printStatus(&out, data)
	if !strings.Contains(out.String(), "INVALID") || !strings.Contains(out.String(), "unfinished-stage") {
		t.Fatal(out.String())
	}
}
func TestCheckHumanSummaries(t *testing.T) {
	diff := compareResult{Compared: 12, Changed: []difference{{Path: "vendor/lib/libasp.so", Change: "different-content"}}, Matching: false}
	data := map[string]any{"platform": "athena", "vendor": diff, "recommendation": "use matched bundled vendor rootfs", "live_vendor_usable": false}
	var out bytes.Buffer
	printVendorCheck(&out, data)
	if !strings.Contains(out.String(), "DIFFERENCES FOUND") || !strings.Contains(out.String(), "vendor/lib/libasp.so") || !strings.Contains(out.String(), "check-vendor-json") {
		t.Fatal(out.String())
	}
	data = map[string]any{"platform": "athena", "vendor_mode": "live", "comparison": compareResult{Compared: 3, Matching: true}, "live_vendor_comparison": diff, "update_required": true}
	out.Reset()
	printRuntimeCheck(&out, data)
	if !strings.Contains(out.String(), "Update: required") || !strings.Contains(out.String(), "Live /vendor:") || !strings.Contains(out.String(), "check-json") {
		t.Fatal(out.String())
	}
}
func TestProgressErasesSpinnerBeforeLogAndClose(t *testing.T) {
	var out bytes.Buffer
	m := &progressMeter{tty: true, drawn: false, active: true, out: &out, done: make(chan struct{})}
	m.Phase("Reading OTA")
	m.mu.Lock()
	m.drawn = true
	m.mu.Unlock()
	m.Note("system partition: scan 1")
	m.mu.Lock()
	m.drawn = true
	m.mu.Unlock()
	m.Close()
	data := out.String()
	if strings.Count(data, "\r\x1b[2K") != 2 {
		t.Fatalf("spinner row not cleared before message/exit: %q", data)
	}
	if !strings.Contains(data, "asp-installer: system partition: scan 1\n") {
		t.Fatal(data)
	}
}

func TestSharedScanNumberAcrossPartitions(t *testing.T) {
	meter := &progressMeter{out: &bytes.Buffer{}, done: make(chan struct{})}
	if meter.NextScan() != 1 || meter.NextScan() != 2 || meter.NextScan() != 3 {
		t.Fatal("global scan numbering is not sequential")
	}
	meter.Close()
}

func TestStatusSeparatesLegacyInstalledRuntimeFromCachedFirmware(t *testing.T) {
	data := map[string]any{
		"current":          map[string]any{"installed": true, "path": "/data/techo5-linux/external-asp", "release_revision": "legacy-or-unrecorded", "vendor_mode": "bundled"},
		"cache_root":       "/data/techo5-linux/asp-installer-cache",
		"extracted_caches": []extractionStatus{{Platform: "rook", PlatformLabel: displayPlatform("rook"), Firmware: "690918120", FireOS: "5.5.6.9", State: "ready", FactoryFiles: 55, Path: "/data/techo5-linux/asp-installer-cache/rook-fireos-5.5.6.9-build-690918120"}},
		"ota_images":       []imageStatus{},
		"generations":      []map[string]any{}, "backup_archives": []string{}, "incomplete_operations": []string{},
	}
	var out bytes.Buffer
	printStatus(&out, data)
	for _, fragment := range []string{"Installed target: not recorded (manual/legacy deployment)", "FrankenASP revision: not recorded (manual/legacy deployment)", "Factory firmware cache (not necessarily installed on this device)", "Fire OS: 5.5.6.9", "Amazon firmware build: 690918120", "Device/target: Echo Spot (1st Gen, 2017) [rook]"} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("missing %q in %s", fragment, out.String())
		}
	}
}
