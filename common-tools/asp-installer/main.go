package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var revision = "development"

func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func main() {
	started := time.Now()
	e := run()
	if e != nil {
		fmt.Fprintln(os.Stderr, "asp-installer:", e)
	}
	// Always show overall wall-clock time, including failed commands.
	// stderr leaves stdout as a clean JSON document for machine consumers.
	fmt.Fprintf(os.Stderr, "asp-installer: Total elapsed: %s\n", elapsedText(time.Since(started)))
	if e != nil {
		os.Exit(2)
	}
}
func elapsedText(duration time.Duration) string {
	if duration > 0 && duration < time.Millisecond {
		return "<1ms"
	}
	if duration < time.Second {
		return duration.Round(time.Millisecond).String()
	}
	return duration.Round(100 * time.Millisecond).String()
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: asp-installer <inspect|check|check-json|check-vendor|check-vendor-json|install|update|uninstall|rollback|status|status-json|version> [flags]")
	}
	selected := os.Args[1]
	jsonOutput := selected == "status-json" || selected == "check-vendor-json" || selected == "check-json"
	cmd := selected
	if jsonOutput {
		cmd = strings.TrimSuffix(selected, "-json")
	}
	if cmd == "version" {
		printJSON(map[string]string{"version": revision})
		return nil
	}
	if cmd == "status" {
		sf := flag.NewFlagSet("status", flag.ContinueOnError)
		cacheRoot := sf.String("cache-dir", cacheDefaultPath(), "private OTA and extracted cache root")
		if e := sf.Parse(os.Args[2:]); e != nil {
			return e
		}
		if sf.NArg() != 0 || !filepath.IsAbs(*cacheRoot) || *cacheRoot == "/" {
			return errors.New("status accepts only --cache-dir ABSOLUTE_PATH")
		}
		result := statusData(*cacheRoot)
		if jsonOutput {
			printJSON(result)
		} else {
			printStatus(os.Stdout, result)
		}
		return nil
	}
	switch cmd {
	case "inspect", "check", "check-vendor", "install", "update", "uninstall", "rollback":
	default:
		return errors.New("unknown installer command")
	}
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	platform := f.String("platform", "", "athena|rook; normally detected from release manifest")
	ota := f.String("ota", "", "local OTA ZIP on Echo")
	otaURL := f.String("ota-url", "", "owner-selected OTA HTTP(S) URL")
	firmware := f.String("firmware", "", "select already-extracted cached firmware version or variant by name; omit OTA for cached use")
	assets := f.String("assets", "./assets", "directory with release launcher/helper/config assets")
	tmp := f.String("tmp-dir", "/data/techo5-linux", "temporary OTA download directory")
	cachePath := f.String("cache-dir", cacheDefaultPath(), "persistent SHA-256 verified extracted OTA cache directory")
	refreshCache := f.Bool("refresh-cache", false, "redownload and re-extract selected OTA, atomically replacing an existing cache")
	vendorMode := f.String("vendor-mode", "auto", "auto|bundled|live; Rook always bundled")
	forceNoBackup := f.Bool("force-no-backup", false, "update only: skip archive backup (previous generation still retained)")
	restart := f.Bool("restart-techo5", false, "mutations only: allow verified PID1-supervised Linux TECHO5 daemon restart if required")
	purge := f.Bool("purge-backups", false, "uninstall only: also remove installer-created backups")
	confirm := f.Bool("yes", false, "explicitly authorize mutation")
	removeOTA := f.Bool("delete-downloaded-ota", false, "delete a successfully downloaded OTA after this command (default: retain)")
	generation := f.String("generation", "", "rollback generation name from status")
	if e := f.Parse(os.Args[2:]); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *removeOTA && *otaURL == "" {
		return errors.New("--delete-downloaded-ota requires --ota-url")
	}
	if *cachePath == "" || !filepath.IsAbs(*cachePath) {
		return errors.New("--cache-dir must be absolute")
	}
	if *cachePath == "/" {
		return errors.New("unsafe cache directory")
	}

	mutating := cmd == "install" || cmd == "update" || cmd == "uninstall" || cmd == "rollback"
	if mutating && !*confirm {
		return errors.New("destructive operation requires explicit --yes; turn External ASP off in TECHO5 settings before maintenance")
	}
	if *forceNoBackup && cmd != "update" {
		return errors.New("--force-no-backup applies only to update")
	}
	if *purge && cmd != "uninstall" {
		return errors.New("--purge-backups applies only to uninstall")
	}
	if *restart && !mutating {
		return errors.New("--restart-techo5 applies only to mutations")
	}
	if *platform != "" && *platform != "athena" && *platform != "rook" {
		return errors.New("unknown --platform")
	}
	if cmd == "uninstall" || cmd == "rollback" {
		if *ota != "" || *otaURL != "" {
			return errors.New("OTA source not required for uninstall/rollback")
		}
		meter := newMeter()
		defer meter.Close()
		var out map[string]any
		var e error
		if cmd == "uninstall" {
			meter.Note("Disable External ASP yourself in TECHO5 settings before uninstall")
			out, e = uninstallAll(*purge, *restart, meter)
		} else {
			if *generation == "" {
				return errors.New("rollback requires --generation")
			}
			out, e = rollbackGeneration(*generation, *restart, meter)
		}
		if e != nil {
			return e
		}
		meter.Stop()
		printJSON(out)
		return nil
	}
	if *ota != "" && *otaURL != "" {
		return errors.New("select only one --ota or --ota-url")
	}
	if *firmware != "" && (*ota != "" || *otaURL != "") {
		return errors.New("--firmware selects a cached extraction; omit --ota and --ota-url")
	}
	if *refreshCache && *ota == "" && *otaURL == "" {
		return errors.New("--refresh-cache requires --ota or --ota-url")
	}
	if *firmware != "" && (!safePath(*firmware) || strings.Contains(*firmware, "/")) {
		return errors.New("unsafe --firmware version or cache name")
	}
	manifestRaw, e := os.ReadFile(filepath.Join(*assets, "manifest.json"))
	if e != nil {
		return fmt.Errorf("release manifest unavailable: %w", e)
	}
	var manifest struct {
		Schema   string `json:"schema"`
		Platform string `json:"platform"`
	}
	if e = json.Unmarshal(manifestRaw, &manifest); e != nil {
		return e
	}
	if manifest.Schema != "ASP1-ASSETS-V3" || (manifest.Platform != "athena" && manifest.Platform != "rook") {
		return errors.New("invalid release manifest")
	}
	if *platform == "" {
		*platform = manifest.Platform
	} else if *platform != manifest.Platform {
		return errors.New("release platform mismatch")
	}
	if *vendorMode != "auto" && *vendorMode != "bundled" && *vendorMode != "live" {
		return errors.New("--vendor-mode must be auto|bundled|live")
	}
	if *platform == "rook" && *vendorMode == "live" {
		return errors.New("rook requires bundled vendor; check-vendor is Athena-only")
	}
	if cmd == "check-vendor" && *platform != "athena" {
		return errors.New("check-vendor is supported only on Athena")
	}
	child, e := os.ReadFile(filepath.Join(*assets, "asp1-child"))
	if e != nil {
		return e
	}
	if _, e = elfNeeded(child, false, "/system/bin/linker"); e != nil {
		return fmt.Errorf("invalid bundled helper ELF: %w", e)
	}
	launcher, e := os.ReadFile(filepath.Join(*assets, "external-asp"))
	if e != nil {
		return e
	}
	if _, e = elfNeeded(launcher, false, ""); e != nil {
		return fmt.Errorf("invalid bundled launcher ELF: %w", e)
	}
	if *platform == "athena" {
		mapData, err := os.ReadFile(filepath.Join(*assets, "channel-map"))
		if err != nil {
			return err
		}
		if err = validateChannelMap(mapData); err != nil {
			return err
		}
	}
	if mutating && os.Geteuid() != 0 {
		return errors.New("install/update require root")
	}
	if cmd == "install" {
		if _, err := os.Lstat(destination); err == nil {
			return errors.New("external-asp already exists; use update (no download performed)")
		}
	} else if cmd == "update" || cmd == "check" {
		if e = validateManaged(destination); e != nil {
			return fmt.Errorf("current installation unavailable or unsafe: %w", e)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	meter := newMeter()
	defer meter.Close()
	if cmd == "update" {
		meter.Note("Disable External ASP manually in TECHO5 settings to prevent helper respawn")
	}
	unlock, e := cacheLock(ctx, *cachePath)
	if e != nil {
		return e
	}
	defer unlock()
	if e = migrateCacheNames(*cachePath, meter); e != nil {
		return e
	}
	extraction, downloadedOTA, cleanup, e := loadExtracted(ctx, *cachePath, *platform, *ota, *otaURL, *firmware, *tmp, cmd, child, *forceNoBackup, *refreshCache, *removeOTA, meter)
	if cleanup != nil && *removeOTA {
		defer cleanup()
	}
	if e != nil {
		return e
	}
	collectedItems := extraction.Items
	summary := map[string]any{"platform": *platform, "ota_format": extraction.Metadata.OTAFormat, "factory_files": extraction.Metadata.Files, "factory_bytes": extraction.Metadata.Bytes, "dependency_edges": extraction.Metadata.DependencyEdges, "firmware": extraction.Metadata.Firmware, "cache": map[string]any{"hit": extraction.Hit, "path": extraction.Path, "source_sha256": extraction.Metadata.SourceSHA256}}
	if *otaURL != "" && downloadedOTA != "" {
		summary["downloaded_ota"] = map[string]any{"path": downloadedOTA, "retained": !*removeOTA}
	}
	if cmd == "inspect" {
		meter.Stop()
		printJSON(summary)
		return nil
	}
	if cmd == "check-vendor" {
		meter.Phase("Comparing selected OTA vendor files against live /vendor")
		diff := compareVendor(collectedItems, "/vendor")
		if err := vendorReadOnly(); err != nil {
			diff.Matching = false
			diff.Warning = err.Error()
		}
		summary["vendor"] = diff
		summary["live_vendor_usable"] = diff.Matching && diff.Compared > 0
		if diff.Matching {
			summary["recommendation"] = "live vendor may be used on Athena"
		} else {
			summary["recommendation"] = "use matched bundled vendor rootfs"
		}
		meter.Stop()
		if jsonOutput {
			printJSON(summary)
		} else {
			printVendorCheck(os.Stdout, summary)
		}
		return nil
	}
	mode, inspection, e := chooseVendorMode(*platform, *vendorMode, collectedItems)
	if e != nil {
		return e
	}
	summary["vendor_mode"] = mode
	if *platform == "athena" && *vendorMode == "auto" {
		summary["vendor_inspection"] = inspection
	}
	if cmd == "check" {
		meter.Phase("Comparing installed FrankenASP and extracted OTA closure")
		// Current installed mode is authoritative unless explicitly overridden.
		if *vendorMode == "auto" {
			before := installedVersion()
			if m, ok := before["vendor_mode"].(string); ok {
				mode = m
			}
		}
		diff, e := compareInstalled(*platform, *assets, collectedItems, mode, destination)
		if e != nil {
			return e
		}
		summary["installed"] = installedVersion()
		summary["comparison"] = diff
		summary["update_required"] = !diff.Matching
		if mode == "live" && *platform == "athena" {
			vendorDiff := compareVendor(collectedItems, "/vendor")
			if err := vendorReadOnly(); err != nil {
				vendorDiff.Matching = false
				vendorDiff.Warning = err.Error()
			}
			summary["live_vendor_comparison"] = vendorDiff
			if !vendorDiff.Matching {
				summary["update_required"] = true
			}
		}
		meter.Stop()
		if jsonOutput {
			printJSON(summary)
		} else {
			printRuntimeCheck(os.Stdout, summary)
		}
		return nil
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if cmd == "install" || cmd == "update" {
		result, err := installOrUpdate(*platform, *assets, collectedItems, extraction.Metadata.OTAFormat, mode, cmd == "update", *forceNoBackup, *restart, meter)
		if err != nil {
			return err
		}
		summary["result"] = result
		meter.Stop()
		printJSON(summary)
		return nil
	}
	return fmt.Errorf("unsupported action: %s", strings.TrimSpace(cmd))
}
