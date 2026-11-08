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

	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

// The build script supplies the tagged release version or a timestamped build.
// An unpackaged source build advertises itself as development.
var revision = "3.dev"

func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func main() {
	started := time.Now()
	printRunBanner(os.Stderr, revision)
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "asp-installer:", err)
	}
	// Preserve elapsed wall-clock reporting, including failed commands.
	// Use stderr to keep machine-readable JSON on stdout unchanged.
	fmt.Fprintf(os.Stderr, "asp-installer: Total elapsed: %s\n", elapsedText(time.Since(started)))
	if err != nil {
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
		return errors.New("usage: asp-installer <inspect|check|check-vendor|install|update|uninstall|rollback|status|version> [flags] [--json]; *-json aliases remain available")
	}
	selected := os.Args[1]
	jsonOutput := strings.HasSuffix(selected, "-json")
	cmd := strings.TrimSuffix(selected, "-json")
	if cmd == "version" {
		vf := flag.NewFlagSet("version", flag.ContinueOnError)
		jsonFlag := vf.Bool("json", false, "emit machine-readable JSON instead of human output")
		if e := vf.Parse(os.Args[2:]); e != nil {
			return e
		}
		if vf.NArg() != 0 {
			return errors.New("version accepts no positional arguments")
		}
		return outputResult(os.Stdout, jsonOutput || *jsonFlag, map[string]string{"version": revision}, func() { printVersion(os.Stdout, revision) })
	}
	if cmd == "status" {
		sf := flag.NewFlagSet("status", flag.ContinueOnError)
		cacheRoot := sf.String("cache-dir", cacheDefaultPath(), "private OTA and extracted cache root")
		jsonFlag := sf.Bool("json", false, "emit machine-readable JSON instead of human output")
		if e := sf.Parse(os.Args[2:]); e != nil {
			return e
		}
		if sf.NArg() != 0 {
			return errors.New("status accepts no positional arguments")
		}
		if *cacheRoot == "" {
			return errors.New("--cache-dir must be a non-root directory")
		}
		absRoot, err := filepath.Abs(*cacheRoot)
		if err != nil {
			return fmt.Errorf("--cache-dir: %w", err)
		}
		if absRoot == "/" {
			return errors.New("unsafe cache directory")
		}
		result := statusData(absRoot)
		return outputResult(os.Stdout, jsonOutput || *jsonFlag, result, func() { printStatus(os.Stdout, result) })
	}
	switch cmd {
	case "inspect", "check", "check-vendor", "install", "update", "uninstall", "rollback":
	default:
		return errors.New("unknown installer command")
	}
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	jsonFlag := f.Bool("json", false, "emit machine-readable JSON instead of human output")
	platform := f.String("platform", "", "optional Echo codename (validated against source when known)")
	ota := f.String("ota", "", "local OTA ZIP on Echo")
	otaURL := f.String("ota-url", "", "owner-selected OTA HTTP(S) URL")
	firmware := f.String("firmware", "", "select already-extracted cached firmware version or variant by name; omit OTA for cached use")
	assets := f.String("assets", "./assets", "directory with release launcher/helper/config assets")
	otaSearchDir := f.String("ota-search-dir", "", "optional directory to search for previously saved local OTA candidates (default: <cache-dir>/ota-images)")
	legacyTmpDir := f.String("tmp-dir", "", "deprecated alias for --ota-search-dir; NOT used for OTA downloads")
	cachePath := f.String("cache-dir", cacheDefaultPath(), "persistent SHA-256 verified extracted OTA cache directory")
	refreshCache := f.Bool("refresh-cache", false, "redownload and re-extract selected OTA, atomically replacing an existing cache")
	vendorMode := f.String("vendor-mode", "auto", "auto|bundled|live; live requires explicit release capability")
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
	jsonOutput = jsonOutput || *jsonFlag
	if *removeOTA && *otaURL == "" {
		return errors.New("--delete-downloaded-ota requires --ota-url")
	}
	if *cachePath == "" {
		return errors.New("--cache-dir must be a non-root directory")
	}
	absCache, err := filepath.Abs(*cachePath)
	if err != nil {
		return fmt.Errorf("--cache-dir: %w", err)
	}
	if absCache == "/" {
		return errors.New("unsafe cache directory")
	}
	*cachePath = absCache
	for _, dir := range []*string{otaSearchDir, legacyTmpDir} {
		if *dir != "" {
			absolute, err := filepath.Abs(*dir)
			if err != nil {
				return fmt.Errorf("OTA search directory: %w", err)
			}
			*dir = absolute
		}
	}
	searchDirectory, e := resolveOTASearchDir(*cachePath, *otaSearchDir, *legacyTmpDir)
	if e != nil {
		return e
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
	normalizedPlatform, err := resolveCodename(*platform, "")
	if err != nil {
		return err
	}
	*platform = normalizedPlatform
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
		return outputResult(os.Stdout, jsonOutput, out, func() { printMutation(os.Stdout, cmd, out) })
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
	if *firmware != "" && (!asp.SafePath(*firmware) || strings.Contains(*firmware, "/")) {
		return errors.New("unsafe --firmware version or cache name")
	}
	var assetPlan asp.AssetPlan
	if cmd != "inspect" {
		assetsAbs, er := filepath.Abs(*assets)
		if er != nil {
			return er
		}
		assetPlan, err = asp.LoadAssets(assetsAbs)
		if err != nil {
			return err
		}
		if *platform == "" {
			*platform = assetPlan.Platform
		} else if *platform != assetPlan.Platform {
			return errors.New("release platform mismatch")
		}
	}
	if *vendorMode != "auto" && *vendorMode != "bundled" && *vendorMode != "live" {
		return errors.New("--vendor-mode must be auto|bundled|live")
	}
	var child []byte
	if cmd != "inspect" {
		child = assetPlan.Files["assets/asp1-child"]
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
	extraction, downloadedOTA, cleanup, e := loadExtracted(ctx, *cachePath, *platform, *ota, *otaURL, *firmware, searchDirectory, cmd, *forceNoBackup, *refreshCache, *removeOTA, meter)
	if cleanup != nil && *removeOTA {
		defer cleanup()
	}
	if e != nil {
		return e
	}
	collectedItems := extraction.Items
	if cmd != "inspect" {
		if e := asp.ValidateHelperRuntimeForPaths(child, collectedItems, assetPlan.Runtime.LibrarySearch); e != nil {
			return e
		}
	}
	summary := map[string]any{"platform": extraction.Metadata.Platform, "ota_format": extraction.Metadata.OTAFormat, "factory_files": extraction.Metadata.Files, "factory_bytes": extraction.Metadata.Bytes, "dependency_edges": extraction.Metadata.DependencyEdges, "firmware": extraction.Metadata.Firmware, "fire_os_version": asp.FireOSForCache(extraction.Metadata), "platform_label": displayPlatform(extraction.Metadata.Platform), "cache": map[string]any{"hit": extraction.Hit, "path": extraction.Path, "source_sha256": extraction.Metadata.SourceSHA256}}
	if *otaURL != "" && downloadedOTA != "" {
		summary["downloaded_ota"] = map[string]any{"path": downloadedOTA, "retained": !*removeOTA}
	}
	if cmd == "inspect" {
		meter.Stop()
		return outputResult(os.Stdout, jsonOutput, summary, func() { printInspection(os.Stdout, summary) })
	}
	if cmd == "check-vendor" {
		meter.Phase("Comparing selected factory vendor files against live /vendor")
		diff := compareVendor(collectedItems, "/vendor")
		if err := vendorReadOnly(); err != nil {
			diff.Matching = false
			diff.Warning = err.Error()
		}
		summary["vendor"] = diff
		summary["live_vendor_usable"] = diff.Matching && diff.Compared > 0 && assetPlan.AllowsVendor("live")
		summary["live_capability"] = assetPlan.AllowsVendor("live")
		if diff.Matching {
			summary["recommendation"] = "live vendor usable only when declared by the native launcher"
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
	mode, inspection, e := chooseVendorModeCapabilities(assetPlan.Runtime, *vendorMode, collectedItems)
	if e != nil {
		return e
	}
	summary["vendor_mode"] = mode
	if assetPlan.AllowsVendor("live") && *vendorMode == "auto" {
		summary["vendor_inspection"] = inspection
	}
	if cmd == "check" {
		meter.Phase("Comparing installed FrankenASP and factory files")
		// Current installed mode is authoritative unless explicitly overridden.
		if *vendorMode == "auto" {
			before := installedVersion()
			if m, ok := before["vendor_mode"].(string); ok {
				mode = m
			}
		}
		miccal, er := discoverMiccal()
		if er != nil {
			return er
		}
		diff, e := compareInstalledWithPlan(assetPlan, collectedItems, mode, destination, miccal)
		if e != nil {
			return e
		}
		summary["installed"] = installedVersion()
		summary["comparison"] = diff
		summary["update_required"] = !diff.Matching
		if mode == "live" {
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
		result, err := installOrUpdatePlan(assetPlan, collectedItems, extraction.Metadata.OTAFormat, mode, cmd == "update", *forceNoBackup, *restart, meter)
		if err != nil {
			return err
		}
		summary["result"] = result
		meter.Stop()
		return outputResult(os.Stdout, jsonOutput, summary, func() { printInstallResult(os.Stdout, summary) })
	}
	return fmt.Errorf("unsupported action: %s", strings.TrimSpace(cmd))
}
