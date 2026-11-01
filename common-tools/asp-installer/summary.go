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
	fmt.Fprintln(w, "FrankenASP V3 — installation status")
	if err, ok := current["error"].(string); ok {
		fmt.Fprintln(w, "State: INVALID / UNRECOGNIZED — "+err)
	} else if !boolField(current, "installed") {
		fmt.Fprintln(w, "State: not installed")
	} else {
		fmt.Fprintln(w, "State: installed (runtime files present; process activity not checked)")
		receipt := mapField(current["receipt"])
		fmt.Fprintln(w, "Platform: "+stringField(receipt, "platform"))
		fmt.Fprintln(w, "Revision: "+stringField(current, "release_revision"))
		fmt.Fprintln(w, "Vendor: "+stringField(current, "vendor_mode"))
		fmt.Fprintln(w, "Location: "+stringField(current, "path"))
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
	fmt.Fprintf(w, "Cache root: %s\nExtracted caches: %d\n", textField(v, "cache_root"), len(extracted))
	for _, c := range extracted {
		fmt.Fprintf(w, "  %s [%s, %d factory files]\n    %s\n", c.Firmware, c.State, c.FactoryFiles, c.Path)
		if c.OTAImage != "" {
			fmt.Fprintf(w, "    OTA: %s\n", c.OTAImage)
		}
		if c.Error != "" {
			fmt.Fprintf(w, "    WARNING: %s\n", c.Error)
		}
	}
	fmt.Fprintf(w, "OTA images: %d\n", len(images))
	for _, img := range images {
		fmt.Fprintf(w, "  %s [%s, %d MiB]\n    %s\n", img.Firmware, img.State, mbCeil(uint64(img.SizeBytes)), img.Path)
		if img.Error != "" {
			fmt.Fprintf(w, "    WARNING: %s\n", img.Error)
		}
	}
	fmt.Fprintln(w, "More details: asp-installer status-json")
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
	fmt.Fprintf(w, "OTA format: %s; factory files extracted: %v\n", textField(v, "ota_format"), textField(v, "factory_files"))
	diff, _ := v["vendor"].(compareResult)
	displayDifferences(w, diff)
	if usable, ok := v["live_vendor_usable"].(bool); ok && usable {
		fmt.Fprintln(w, "Live /vendor: usable for this OTA")
	} else {
		fmt.Fprintln(w, "Live /vendor: not verified for this OTA")
	}
	fmt.Fprintln(w, "Recommendation: "+textField(v, "recommendation"))
	fmt.Fprintln(w, "Details: asp-installer check-vendor-json [same options]")
}
func printRuntimeCheck(w io.Writer, v map[string]any) {
	fmt.Fprintln(w, "FrankenASP V3 — installed runtime comparison")
	fmt.Fprintln(w, "Platform: "+textField(v, "platform"))
	fmt.Fprintf(w, "OTA format: %s; factory files extracted: %v\n", textField(v, "ota_format"), textField(v, "factory_files"))
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
	fmt.Fprintln(w, "Details: asp-installer check-json [same options]")
}
