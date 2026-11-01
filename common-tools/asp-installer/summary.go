package main

import (
	"context"
	"fmt"
	"io"
)

func textField(m map[string]any, key string) string {
	value, ok := m[key]
	if !ok || value == nil {
		return "unknown"
	}
	return fmt.Sprint(value)
}
func mapField(v any) map[string]any {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return map[string]any{}
}
func stringField(m map[string]any, key string) string {
	s, ok := m[key].(string)
	if !ok || s == "" {
		return "unknown"
	}
	return s
}
func boolField(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}
func statusData(cacheRoot string) map[string]any {
	extracted, images := cacheInventory(context.Background(), cacheRoot)
	return map[string]any{"current": installedVersion(), "generations": generationInfo(), "backup_archives": backupArchives(), "incomplete_operations": incompleteOperations(), "cache_root": cacheRoot, "extracted_caches": extracted, "ota_images": images}
}
func printStatus(w io.Writer, v map[string]any) {
	current := mapField(v["current"])
	fmt.Fprintln(w, "FrankenASP V3 — status")
	fmt.Fprintln(w, "Installed FrankenASP runtime:")
	if err, ok := current["error"].(string); ok {
		fmt.Fprintln(w, "  Files: INVALID / UNRECOGNIZED — "+err)
	} else if !boolField(current, "installed") {
		fmt.Fprintln(w, "  Files: not installed")
	} else {
		fmt.Fprintln(w, "  Files: present (running/active state NOT checked)")
		receipt := mapField(current["receipt"])
		platform := stringField(receipt, "platform")
		if platform == "unknown" {
			fmt.Fprintln(w, "  Installed target: not recorded (manual/legacy deployment)")
		} else {
			fmt.Fprintln(w, "  Installed target: "+displayPlatform(platform))
		}
		rev := stringField(current, "release_revision")
		if rev == "legacy-or-unrecorded" {
			fmt.Fprintln(w, "  FrankenASP revision: not recorded (manual/legacy deployment)")
		} else {
			fmt.Fprintln(w, "  FrankenASP revision: "+rev)
		}
		fmt.Fprintln(w, "  Vendor source: "+stringField(current, "vendor_mode"))
		fmt.Fprintln(w, "  Directory: "+stringField(current, "path"))
	}
	generations, _ := v["generations"].([]map[string]any)
	backups, _ := v["backup_archives"].([]string)
	problems, _ := v["incomplete_operations"].([]string)
	fmt.Fprintf(w, "Saved generations: %d\nBackup archives: %d\n", len(generations), len(backups))
	for _, g := range generations {
		if err, ok := g["validation_error"]; ok {
			fmt.Fprintf(w, "WARNING: invalid generation %s: %v\n", textField(g, "generation"), err)
		}
	}
	if len(problems) > 0 {
		fmt.Fprintln(w, "WARNING: incomplete operations require manual recovery:")
		for _, p := range problems {
			fmt.Fprintln(w, "  - "+p)
		}
	} else {
		fmt.Fprintln(w, "Incomplete operations: none")
	}
	extracted, _ := v["extracted_caches"].([]extractionStatus)
	images, _ := v["ota_images"].([]imageStatus)
	fmt.Fprintf(w, "Factory firmware cache (not necessarily installed on this device):\n  Root: %s\n  Cached extractions: %d\n", textField(v, "cache_root"), len(extracted))
	for _, c := range extracted {
		if c.State != "ready" {
			fmt.Fprintf(w, "  Cache: %s (INVALID)\n    WARNING: %s\n", c.Path, c.Error)
			continue
		}
		fmt.Fprintf(w, "  Device/target: %s\n", c.PlatformLabel)
		if c.FireOS != "" {
			fmt.Fprintf(w, "  Fire OS: %s\n", c.FireOS)
		} else {
			fmt.Fprintln(w, "  Fire OS: not recorded")
		}
		fmt.Fprintf(w, "  Amazon firmware build: %s\n  Extraction: %s (%d verified files)\n  Cache path: %s\n", c.Firmware, c.State, c.FactoryFiles, c.Path)
		if c.OTAImage != "" {
			fmt.Fprintf(w, "  Downloaded OTA: %s\n", c.OTAImage)
		}
	}
	fmt.Fprintf(w, "Saved downloaded OTA images: %d\n", len(images))
	for _, img := range images {
		if img.PlatformLabel != "" {
			fmt.Fprintf(w, "  Target: %s\n", img.PlatformLabel)
		}
		if img.FireOS != "" {
			fmt.Fprintf(w, "  Fire OS: %s\n", img.FireOS)
		}
		if img.Firmware != "" {
			fmt.Fprintf(w, "  Amazon build: %s\n", img.Firmware)
		}
		fmt.Fprintf(w, "  OTA: %s (%s, %d MiB)\n", img.Path, img.State, mbCeil(uint64(img.SizeBytes)))
		if img.Error != "" {
			fmt.Fprintf(w, "  WARNING: %s\n", img.Error)
		}
	}
}
func displayDifferences(w io.Writer, result compareResult) {
	if result.Matching && result.Warning == "" {
		fmt.Fprintln(w, "Result: MATCH — compared files are identical")
	}
	if !result.Matching || result.Warning != "" {
		fmt.Fprintln(w, "Result: DIFFERENCES FOUND")
	}
	fmt.Fprintf(w, "Files compared: %d\nDifferences: %d\n", result.Compared, len(result.Changed))
	if result.Warning != "" {
		fmt.Fprintln(w, "WARNING: "+result.Warning)
	}
	for i, d := range result.Changed {
		if i >= 10 {
			fmt.Fprintf(w, "... and %d more differences (see -json).\n", len(result.Changed)-i)
			break
		}
		fmt.Fprintf(w, "  - %s (%s)\n", d.Path, d.Change)
	}
}
func printVendorCheck(w io.Writer, v map[string]any) {
	fmt.Fprintln(w, "FrankenASP V3 — factory vendor comparison")
	fmt.Fprintln(w, "Platform: "+textField(v, "platform"))
	diff, _ := v["vendor"].(compareResult)
	displayDifferences(w, diff)
	if usable, ok := v["live_vendor_usable"].(bool); ok && usable {
		fmt.Fprintln(w, "Live /vendor: usable for this firmware")
	} else {
		fmt.Fprintln(w, "Live /vendor: not verified for this firmware")
	}
	fmt.Fprintln(w, "Recommendation: "+textField(v, "recommendation"))
}
func printRuntimeCheck(w io.Writer, v map[string]any) {
	fmt.Fprintln(w, "FrankenASP V3 — installed runtime comparison")
	fmt.Fprintln(w, "Platform: "+textField(v, "platform"))
	fmt.Fprintln(w, "Vendor mode: "+textField(v, "vendor_mode"))
	diff, _ := v["comparison"].(compareResult)
	displayDifferences(w, diff)
	if vd, ok := v["live_vendor_comparison"].(compareResult); ok {
		fmt.Fprintln(w, "Live /vendor:")
		displayDifferences(w, vd)
	}
	if boolField(v, "update_required") {
		fmt.Fprintln(w, "Update: required (differences detected)")
	} else {
		fmt.Fprintln(w, "Update: not required (files match)")
	}
}
