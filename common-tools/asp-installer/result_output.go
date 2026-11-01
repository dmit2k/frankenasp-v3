package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Human-readable output is the default for every command. Structured results
// are emitted only for an explicit --json flag or a *-json command alias.
// All progress messages and elapsed-time diagnostics remain on stderr.
func outputResult(w io.Writer, asJSON bool, result any, human func()) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	human()
	return nil
}

func printVersion(w io.Writer, rev string) {
	fmt.Fprintf(w, "FrankenASP V3 installer revision: %s\n", rev)
}

func printFactorySummary(w io.Writer, result map[string]any) {
	fmt.Fprintln(w, "Device / target: "+displayPlatform(stringField(result, "platform")))
	if osVersion := stringField(result, "fire_os_version"); osVersion != "unknown" {
		fmt.Fprintln(w, "Factory firmware: Fire OS "+osVersion)
	} else {
		fmt.Fprintln(w, "Factory firmware: Fire OS version not recorded")
	}
	fmt.Fprintln(w, "Amazon firmware build: "+textField(result, "firmware"))
	fmt.Fprintf(w, "Extracted factory files: %v (%v bytes)\n", textField(result, "factory_files"), textField(result, "factory_bytes"))
	fmt.Fprintln(w, "OTA format: "+textField(result, "ota_format"))
	cache := mapField(result["cache"])
	if boolField(cache, "hit") {
		fmt.Fprintln(w, "Factory extraction: existing verified cache reused")
	} else {
		fmt.Fprintln(w, "Factory extraction: verified cache created or refreshed")
	}
	fmt.Fprintln(w, "Cache directory: "+textField(cache, "path"))
	if downloaded, ok := result["downloaded_ota"].(map[string]any); ok {
		fmt.Fprintln(w, "OTA image: "+textField(downloaded, "path"))
		if retained, ok := downloaded["retained"].(bool); ok && !retained {
			fmt.Fprintln(w, "OTA retention: downloaded image scheduled for deletion")
		}
	}
}

func printInspection(w io.Writer, result map[string]any) {
	fmt.Fprintln(w, "FrankenASP V3 — factory firmware inspection complete")
	printFactorySummary(w, result)
	fmt.Fprintln(w, "No installed runtime was changed.")
	fmt.Fprintln(w, "JSON details: asp-installer inspect --json [same options]")
}

func printInstallResult(w io.Writer, result map[string]any) {
	action := stringField(mapField(result["result"]), "action")
	if action == "unknown" {
		action = "install"
	}
	fmt.Fprintf(w, "FrankenASP V3 — %s complete\n", strings.ToUpper(action[:1])+action[1:])
	printFactorySummary(w, result)
	details := mapField(result["result"])
	current := mapField(details["current"])
	if boolField(details, "changed") {
		fmt.Fprintln(w, "Installed runtime: published successfully")
	} else {
		fmt.Fprintln(w, "Installed runtime: no changes required")
	}
	fmt.Fprintln(w, "Runtime directory: "+textField(current, "path"))
	fmt.Fprintln(w, "FrankenASP revision: "+textField(current, "release_revision"))
	fmt.Fprintln(w, "Vendor source: "+textField(result, "vendor_mode"))
	if backup := details["backup"]; backup != nil {
		fmt.Fprintln(w, "Backup archive: "+fmt.Sprint(backup))
	} else {
		fmt.Fprintln(w, "Backup archive: none")
	}
	if generation, ok := details["previous_generation"].(string); ok && generation != "" {
		fmt.Fprintln(w, "Previous runtime generation: "+generation)
	}
	if hint, ok := details["notice"].(string); ok && hint != "" {
		fmt.Fprintln(w, "Next step: "+hint)
	}
	fmt.Fprintf(w, "JSON details: asp-installer %s --json [same options]\n", action)
}

func printMutation(w io.Writer, action string, result map[string]any) {
	fmt.Fprintf(w, "FrankenASP V3 — %s complete\n", strings.ToUpper(action[:1])+action[1:])
	switch action {
	case "uninstall":
		count := 0
		if removed, ok := result["removed"].([]string); ok {
			count = len(removed)
			for _, name := range removed {
				fmt.Fprintln(w, "Removed managed runtime: "+name)
			}
		}
		fmt.Fprintf(w, "Managed installations/generations removed: %d\n", count)
		if boolField(result, "backups_kept") {
			fmt.Fprintln(w, "Backup archives: preserved")
		} else {
			fmt.Fprintln(w, "Backup archives: purged by explicit request")
		}
		fmt.Fprintln(w, "Factory extraction cache and saved OTA images: preserved")
	case "rollback":
		fmt.Fprintln(w, "Runtime directory: "+textField(mapField(result["current"]), "path"))
		fmt.Fprintln(w, "Active FrankenASP revision: "+textField(mapField(result["current"]), "release_revision"))
		if name, ok := result["saved_replaced_generation"].(string); ok {
			fmt.Fprintln(w, "Replaced generation saved as: "+name)
		}
		if backup, ok := result["backup"].(string); ok {
			fmt.Fprintln(w, "Backup archive: "+backup)
		}
	}
	if hint, ok := result["notice"].(string); ok && hint != "" {
		fmt.Fprintln(w, "Next step: "+hint)
	}
	fmt.Fprintf(w, "JSON details: asp-installer %s --json [same options]\n", action)
}
