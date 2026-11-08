package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dmit2k/frankenasp-v3/internal/asp"
	"os"
	"path/filepath"
	"strings"
)

func ensureDesktopCache(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return asp.RequireCacheDir(path)
}

// Desktop and device share the same verified cache selection logic.
func selectDesktopCache(o options) (cacheResult, error) {
	return asp.SelectFirmwareCache(o.cacheDir, o.platform, o.firmware)
}
func listLocal(o options) error {
	entries, err := os.ReadDir(o.cacheDir)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("No extracted caches yet:", o.cacheDir)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Println("Extracted factory ASP sets:")
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "ota-images" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		path := filepath.Join(o.cacheDir, entry.Name())
		result, err := asp.ReadCache(path)
		if err != nil {
			if raw, readErr := os.ReadFile(filepath.Join(path, "manifest.json")); readErr == nil {
				var legacy asp.CacheMetadata
				if json.Unmarshal(raw, &legacy) == nil && legacy.Schema == "ASP1-EXTRACTED-CACHE-V1" {
					fmt.Printf("LEGACY %s: V1 helper-bound cache (preserved, re-extract for V2)\n", entry.Name())
					continue
				}
			}
			fmt.Printf("INVALID %s: %v\n", entry.Name(), err)
			continue
		}
		m := result.Metadata
		if o.platform != "" && o.platform != m.Platform {
			continue
		}
		osLabel := ""
		if fireos := asp.FireOSForCache(m); fireos != "" {
			osLabel = " (Fire OS " + fireos + ")"
		}
		label := m.Platform
		if label == "" {
			label = "(unknown platform)"
		}
		fmt.Printf("  %s %s%s — %d files", label, m.Firmware, osLabel, m.Files)
		if fingerprint, err := asp.LibraryFingerprint(path); err == nil {
			fmt.Printf(" — ASP libs: %s", fingerprint[:16])
		} else {
			fmt.Print(" — ASP libs: unavailable")
		}
		if strings.Contains(entry.Name(), "-variant-") {
			fmt.Printf(" [--firmware %s]", entry.Name())
		}
		fmt.Println()
		count++
	}
	if count == 0 {
		fmt.Println("  None")
	}
	otaDir := filepath.Join(o.cacheDir, "ota-images")
	if sources, err := os.ReadDir(otaDir); err == nil {
		printed := false
		for _, s := range sources {
			if !s.Type().IsRegular() || strings.HasPrefix(s.Name(), ".") {
				continue
			}
			if !printed {
				fmt.Println("Saved OTA downloads:")
				printed = true
			}
			fmt.Printf("  %s\n", s.Name())
		}
	}
	return nil
}
func extractSource(ctx context.Context, o options) error {
	if err := ensureDesktopCache(o.cacheDir); err != nil {
		return err
	}
	unlock, err := asp.CacheLock(ctx, o.cacheDir)
	if err != nil {
		return err
	}
	defer unlock()
	meter := asp.NewMeter()
	defer meter.Close()
	if o.backup != "" {
		result, err := extractBackup(ctx, o, meter)
		if err != nil {
			return err
		}
		meter.Stop()
		printExtractionSummary(result)
		return nil
	}
	searchDir, err := asp.ResolveOTASearchDir(o.cacheDir, "", "")
	if err != nil {
		return err
	}
	result, _, cleanup, err := loadExtracted(ctx, o.cacheDir, o.platform, o.ota, o.otaURL, "", searchDir, "inspect", false, o.refresh, false, meter)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return err
	}
	// The extractor never requests --delete-downloaded-ota. A completed
	// download belongs to the owner and must survive this command.

	meter.Stop()
	printExtractionSummary(result)
	return nil
}
func printExtractionSummary(result cacheResult) {
	status := "created"
	if result.Hit {
		status = "reused (no duplicate stored)"
	}
	label := result.Metadata.Platform
	if label == "" {
		label = "(unidentified)"
	}
	fmt.Printf("Factory ASP %s %s: %d files, %s\n", label, result.Metadata.Firmware, result.Metadata.Files, status)
}

func extractBackup(ctx context.Context, o options, meter *asp.ProgressMeter) (cacheResult, error) {
	return asp.ExtractBackup(ctx, o.cacheDir, o.platform, o.firmware, o.backup, o.refresh, meter)
}
