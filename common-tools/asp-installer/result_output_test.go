package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAllCommandHumanResultsAreNotJSON(t *testing.T) {
	firmware := map[string]any{"platform": "athena", "fire_os_version": "7574.1", "firmware": "PS75741-7080", "factory_files": 83, "factory_bytes": 12824378, "ota_format": "ZIP_FULL_BLOCK_v4", "cache": map[string]any{"hit": true, "path": "/data/techo5-linux/asp-installer-cache/athena-fireos-7574.1-build-PS75741-7080"}}
	current := map[string]any{"installed": true, "path": "/data/techo5-linux/external-asp", "release_revision": "abc123", "vendor_mode": "live"}
	installed := map[string]any{}
	for k, v := range firmware {
		installed[k] = v
	}
	installed["vendor_mode"] = "live"
	installed["result"] = map[string]any{"action": "install", "changed": true, "current": current, "backup": nil, "notice": "Enable external ASP manually"}
	updated := map[string]any{}
	for k, v := range installed {
		updated[k] = v
	}
	updated["result"] = map[string]any{"action": "update", "changed": true, "current": current, "backup": "/data/backup.tar.gz", "previous_generation": "saved-v3"}
	tables := []struct {
		name     string
		print    func(w *bytes.Buffer)
		expected []string
	}{
		{"version", func(w *bytes.Buffer) { printVersion(w, "3.0.261101202533") }, []string{"version: 3.0.261101202533"}},
		{"status", func(w *bytes.Buffer) { printStatus(w, map[string]any{"current": current, "cache_root": "/cache"}) }, []string{"Installed FrankenASP runtime"}},
		{"inspect", func(w *bytes.Buffer) { printInspection(w, firmware) }, []string{"Fire OS 7574.1", "Amazon firmware build: PS75741-7080", "Cache directory:"}},
		{"check", func(w *bytes.Buffer) {
			printRuntimeCheck(w, map[string]any{"platform": "athena", "comparison": compareResult{Matching: true}, "vendor_mode": "live"})
		}, []string{"runtime comparison", "Update: not required"}},
		{"check-vendor", func(w *bytes.Buffer) {
			printVendorCheck(w, map[string]any{"platform": "athena", "vendor": compareResult{Matching: true}})
		}, []string{"factory vendor comparison"}},
		{"install", func(w *bytes.Buffer) { printInstallResult(w, installed) }, []string{"Install complete", "Fire OS 7574.1", "Amazon firmware build: PS75741-7080", "Vendor source: live", "FrankenASP revision: abc123", "Backup archive: none"}},
		{"update", func(w *bytes.Buffer) { printInstallResult(w, updated) }, []string{"Update complete", "Backup archive: /data/backup.tar.gz", "Previous runtime generation: saved-v3"}},
		{"uninstall", func(w *bytes.Buffer) {
			printMutation(w, "uninstall", map[string]any{"removed": []string{"external-asp"}, "backups_kept": true})
		}, []string{"Uninstall complete", "Removed managed runtime: external-asp", "Factory extraction cache and saved OTA images: preserved"}},
		{"rollback", func(w *bytes.Buffer) {
			printMutation(w, "rollback", map[string]any{"current": current, "backup": "/backup.tar.gz", "saved_replaced_generation": "generation2"})
		}, []string{"Rollback complete", "Active FrankenASP revision: abc123", "Backup archive: /backup.tar.gz"}},
	}
	for _, tc := range tables {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			tc.print(&out)
			data := out.String()
			if json.Valid(out.Bytes()) || strings.HasPrefix(strings.TrimSpace(data), "{") {
				t.Fatalf("ordinary output is JSON: %s", data)
			}
			for _, part := range tc.expected {
				if !strings.Contains(data, part) {
					t.Errorf("missing %q in %s", part, data)
				}
			}
		})
	}
}

func TestExplicitJSONOutputAndHumanDefault(t *testing.T) {
	sample := map[string]any{"action": "inspect", "factory_files": 83}
	var out bytes.Buffer
	calls := 0
	human := func() { calls++; fmt.Fprintln(&out, "Factory inspection complete") }
	if err := outputResult(&out, false, sample, human); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || json.Valid(out.Bytes()) {
		t.Fatalf("human default invalid: %q", out.String())
	}
	out.Reset()
	if err := outputResult(&out, true, sample, human); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !json.Valid(out.Bytes()) {
		t.Fatalf("JSON mode invalid: %q", out.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil || parsed["action"] != "inspect" {
		t.Fatalf("unexpected JSON payload: %v %v", parsed, err)
	}
}

func TestVersionBannerAlwaysHumanAndNotJSON(t *testing.T) {
	for _, version := range []string{"3.0.261101202533", "3.0.dev"} {
		var out bytes.Buffer
		printRunBanner(&out, version)
		if got := out.String(); got != "asp-installer v"+version+"\n" {
			t.Fatalf("unexpected startup banner: %q", got)
		}
		if json.Valid(out.Bytes()) {
			t.Fatal("banner unexpectedly parsed as JSON")
		}
	}
}
