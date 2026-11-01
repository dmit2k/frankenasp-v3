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
)

var revision = "development"

func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "asp-installer:", e)
		os.Exit(2)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: asp-installer <inspect|check|check-vendor|install|update|uninstall|rollback|status|version> [flags]")
	}
	cmd := os.Args[1]
	if cmd == "version" {
		printJSON(map[string]string{"version": revision})
		return nil
	}
	if cmd == "status" {
		if len(os.Args) != 2 {
			return errors.New("status takes no arguments")
		}
		printJSON(map[string]any{"current": installedVersion(), "generations": generationInfo(), "backup_archives": backupArchives(), "incomplete_operations": incompleteOperations()})
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
	assets := f.String("assets", "./assets", "directory with release launcher/helper/config assets")
	tmp := f.String("tmp-dir", "/data/techo5-linux", "temporary OTA download directory")
	vendorMode := f.String("vendor-mode", "auto", "auto|bundled|live; Rook always bundled")
	forceNoBackup := f.Bool("force-no-backup", false, "update only: skip archive backup (previous generation still retained)")
	restart := f.Bool("restart-techo5", false, "mutations only: allow verified PID1-supervised Linux TECHO5 daemon restart if required")
	purge := f.Bool("purge-backups", false, "uninstall only: also remove installer-created backups")
	confirm := f.Bool("yes", false, "explicitly authorize mutation")
	generation := f.String("generation", "", "rollback generation name from status")
	if e := f.Parse(os.Args[2:]); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
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
		printJSON(out)
		return nil
	}
	if (*ota == "") == (*otaURL == "") {
		return errors.New("select exactly one --ota or --ota-url")
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
	extraDownloadReserve, e := preflightBeforeOTA(cmd, *otaURL, *tmp, *forceNoBackup, meter)
	if e != nil {
		return e
	}
	source := *ota
	cleanup := func() {}
	if *otaURL != "" {
		meter.Phase("Downloading user-selected OTA (without manufacturer SHA or TLS certificate validation)")
		source, cleanup, e = downloadWithReserve(ctx, *otaURL, *tmp, extraDownloadReserve)
		if e != nil {
			return e
		}
		defer cleanup()
	}
	meter.Phase("Opening OTA and recognizing partition format")
	src, handle, e := openOTA(source, *platform)
	if e != nil {
		return e
	}
	defer handle.Close()
	for _, reader := range src.selective {
		reader.ctx = ctx
	}
	meter.Phase("Selectively extracting factory libraries and transitive ELF dependencies")
	collected := newCollector(src)
	if e = collected.closure(*platform, child); e != nil {
		return fmt.Errorf("factory dependency closure: %w", e)
	}
	var total int
	for _, raw := range collected.items {
		total += len(raw)
	}
	summary := map[string]any{"platform": *platform, "ota_format": src.kind, "factory_files": len(collected.items), "factory_bytes": total, "dependency_edges": collected.edgeCount}
	if cmd == "inspect" {
		printJSON(summary)
		return nil
	}
	if cmd == "check-vendor" {
		meter.Phase("Comparing selected OTA vendor files against live /vendor")
		diff := compareVendor(collected.items, "/vendor")
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
		printJSON(summary)
		return nil
	}
	mode, inspection, e := chooseVendorMode(*platform, *vendorMode, collected.items)
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
		diff, e := compareInstalled(*platform, *assets, collected.items, mode, destination)
		if e != nil {
			return e
		}
		summary["installed"] = installedVersion()
		summary["comparison"] = diff
		summary["update_required"] = !diff.Matching
		if mode == "live" && *platform == "athena" {
			vendorDiff := compareVendor(collected.items, "/vendor")
			if err := vendorReadOnly(); err != nil {
				vendorDiff.Matching = false
				vendorDiff.Warning = err.Error()
			}
			summary["live_vendor_comparison"] = vendorDiff
			if !vendorDiff.Matching {
				summary["update_required"] = true
			}
		}
		printJSON(summary)
		return nil
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if cmd == "install" || cmd == "update" {
		result, err := installOrUpdate(*platform, *assets, collected.items, src.kind, mode, cmd == "update", *forceNoBackup, *restart, meter)
		if err != nil {
			return err
		}
		summary["result"] = result
		printJSON(summary)
		return nil
	}
	return fmt.Errorf("unsupported action: %s", strings.TrimSpace(cmd))
}
