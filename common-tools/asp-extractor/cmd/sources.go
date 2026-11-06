package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func ensureDesktopCache(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return requireCacheDir(path)
}

// A selected cache is always revalidated by the embedded installer's
// unmodified manifest/inventory verifier.
func selectDesktopCache(o options) (cacheResult, error) {
	entries, err := os.ReadDir(o.cacheDir)
	if err != nil {
		return cacheResult{}, err
	}
	var matched []cacheResult
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Name() == otaImageDir {
			continue
		}
		path := filepath.Join(o.cacheDir, entry.Name())
		raw, err := os.ReadFile(filepath.Join(path, "manifest.json"))
		if err != nil {
			return cacheResult{}, fmt.Errorf("%s missing manifest: %w", path, err)
		}
		var meta cacheMetadata
		if err = json.Unmarshal(raw, &meta); err != nil {
			return cacheResult{}, err
		}
		if meta.Schema == "ASP1-EXTRACTED-CACHE-V1" {
			continue // Never use legacy helper-bound cache for a new bundle.
		}
		if o.platform != "" && meta.Platform != o.platform {
			continue
		}
		if o.firmware != "" && o.firmware != entry.Name() && o.firmware != meta.Firmware && o.firmware != fireOSForCache(meta) {
			continue
		}
		result, err := readCache(path)
		if err != nil {
			return cacheResult{}, fmt.Errorf("%s: %w", path, err)
		}
		matched = append(matched, result)
	}
	if len(matched) == 0 {
		return cacheResult{}, fmt.Errorf("no matching verified cache for %s firmware %q", o.platform, o.firmware)
	}
	if len(matched) > 1 {
		names := make([]string, 0, len(matched))
		for _, m := range matched {
			names = append(names, filepath.Base(m.Path))
		}
		return cacheResult{}, fmt.Errorf("ambiguous cache (%s); specify --firmware with exact cache directory", strings.Join(names, ", "))
	}
	return matched[0], nil
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
		result, err := readCache(path)
		if err != nil {
			if raw, readErr := os.ReadFile(filepath.Join(path, "manifest.json")); readErr == nil {
				var legacy cacheMetadata
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
		if fireos := fireOSForCache(m); fireos != "" {
			osLabel = " (Fire OS " + fireos + ")"
		}
		label := m.Platform
		if label == "" {
			label = "(unknown platform)"
		}
		fmt.Printf("  %s %s%s — %d files", label, m.Firmware, osLabel, m.Files)
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
	unlock, err := cacheLock(ctx, o.cacheDir)
	if err != nil {
		return err
	}
	defer unlock()
	meter := newMeter()
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
	searchDir, err := resolveOTASearchDir(o.cacheDir, "", "")
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

// Backup source partitions are selected by ext4 contents, not by Echo codenames
// or hardcoded partition numbers. The original image files are never changed.
func closeImages(handles []*os.File) {
	for _, f := range handles {
		_ = f.Close()
	}
}

func openBackupImages(dir string) (*otaSources, []*os.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	type candidate struct {
		name   string
		file   *os.File
		img    *image
		score  int
		prefix string
	}
	var candidates []candidate
	var handles []*os.File
	fail := func(err error) (*otaSources, []*os.File, error) {
		closeImages(handles)
		return nil, nil, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".img") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return fail(err)
		}
		if !info.Mode().IsRegular() || info.Size() < 4096 || info.Size() > 4<<30 {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return fail(err)
		}
		img := &image{src: f, size: info.Size()}
		if err := img.init(); err != nil {
			_ = f.Close()
			continue // Non-ext4 image: not a factory filesystem source.
		}
		handles = append(handles, f)
		c := candidate{name: entry.Name(), file: f, img: img}
		for _, probe := range []struct {
			path   string
			points int
			prefix string
		}{
			{"system/bin/linker", 4, "system"}, {"bin/linker", 4, ""},
			{"system/build.prop", 1, "system"}, {"build.prop", 1, ""},
		} {
			// A nested system root often has /bin as a symlink. Do not
			// traverse alternate paths once a higher-quality root is known.
			if c.score >= probe.points {
				continue
			}
			ok, err := img.Exists(probe.path)
			if err != nil {
				return fail(err)
			}
			if ok {
				c.score, c.prefix = probe.points, probe.prefix
			}
		}
		candidates = append(candidates, c)
	}
	var system *candidate
	for i := range candidates {
		c := &candidates[i]
		if c.score == 0 {
			continue
		}
		if system == nil || c.score > system.score {
			system = c
		} else if c.score == system.score {
			return fail(fmt.Errorf("ambiguous system partitions: %s and %s", system.name, c.name))
		}
	}
	if system == nil {
		return fail(errors.New("no supported raw ext4 system filesystem found"))
	}
	system.img.prefix = system.prefix
	src := &otaSources{kind: "BACKUP_RAW_EXT4", system: system.img}
	var vendor *candidate
	for i := range candidates {
		c := &candidates[i]
		if c.name == system.name {
			continue
		}
		for _, probe := range []string{"etc/audio-algorithms/AFE.cfg", "lib/libasp.so", "build.prop"} {
			ok, err := c.img.Exists(probe)
			if err != nil {
				return fail(err)
			}
			if ok {
				if vendor != nil {
					return fail(fmt.Errorf("ambiguous vendor partitions: %s and %s", vendor.name, c.name))
				}
				vendor = c
				break
			}
		}
	}
	if vendor != nil {
		src.vendor = vendor.img
	} else {
		src.vendor = prefixed{parent: src.system, prefix: "vendor"}
	}
	// Order preserves the original Athena (system, vendor) and Rook (system)
	// backup SHA-256 source identities, irrespective of directory listing order.
	ordered := []*os.File{system.file}
	if vendor != nil {
		ordered = append(ordered, vendor.file)
	}
	for _, f := range handles {
		if f == system.file || vendor != nil && f == vendor.file {
			continue
		}
		_ = f.Close()
	}
	return src, ordered, nil
}

func backupIdentity(ctx context.Context, files []*os.File) (string, error) {
	digest := sha256.New()
	buf := make([]byte, 1<<20)
	for _, f := range files {
		_, _ = io.WriteString(digest, filepath.Base(f.Name())+"\x00")
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		for {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			n, err := f.Read(buf)
			if n > 0 {
				_, _ = digest.Write(buf[:n])
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func property(buildprop, key string) string {
	for _, line := range strings.Split(buildprop, "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimSpace(strings.TrimPrefix(line, key+"="))
		}
	}
	return ""
}
func extractBackup(ctx context.Context, o options, meter *progressMeter) (cacheResult, error) {
	info, err := os.Stat(o.backup)
	if err != nil {
		return cacheResult{}, err
	}
	if !info.IsDir() {
		return cacheResult{}, errors.New("--backup-dir must be a directory")
	}
	meter.Phase("Opening original ext4 backup images")
	src, files, err := openBackupImages(o.backup)
	if err != nil {
		return cacheResult{}, err
	}
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	bp, err := readSystemBuildProp(src)
	if err != nil {
		return cacheResult{}, fmt.Errorf("cannot read backup build.prop: %w", err)
	}
	text := string(bp)
	detected, err := buildPropCodename(bp)
	if err != nil {
		return cacheResult{}, err
	}
	platform, err := resolveCodename(o.platform, detected)
	if err != nil {
		return cacheResult{}, err
	}
	firmware := firmwareLabel(property(text, "ro.build.fingerprint"), property(text, "ro.build.version.incremental"))
	if o.firmware != "" {
		if firmware != "unknown" && firmware != o.firmware {
			return cacheResult{}, fmt.Errorf("backup firmware %s differs from requested %s", firmware, o.firmware)
		}
		firmware = o.firmware
	}
	// Firmware labels are best-effort provenance metadata, never a prerequisite
	// for extracting factory files from an owner-provided backup.
	meter.Phase("Hashing original backup partition pair for cache identity")
	sourceSHA, err := backupIdentity(ctx, files)
	if err != nil {
		return cacheResult{}, err
	}
	if firmware == "unknown" || firmware == "" {
		firmware = "unknown-" + sourceSHA[:12]
		meter.Note("Backup firmware version unavailable; using source digest as stable identity")
	}
	if !o.refresh {
		existing, err := findCache(o.cacheDir, platform, sourceSHA, "")
		if err == nil {
			meter.Note("Verified backup cache reused; no ext4 rescan")
			return existing, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return cacheResult{}, err
		}
	}
	meter.Phase("Collecting factory ELF dependency closure")
	collected := newCollector(src)
	if err = collected.closure(); err != nil {
		return cacheResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return cacheResult{}, err
	}
	fireos := knownFireOS(platform, firmware)
	meta := cacheMetadata{Platform: platform, Firmware: firmware, FireOS: fireos, SourceSHA256: sourceSHA, OTAFormat: src.kind, DependencyEdges: collected.edgeCount}
	return publishCache(o.cacheDir, meta, collected.items, o.refresh, meter)
}
func cacheFiles(result cacheResult) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, name := range []string{"manifest.json", "SHA256SUMS"} {
		b, err := os.ReadFile(filepath.Join(result.Path, name))
		if err != nil {
			return nil, err
		}
		out[name] = b
	}
	names := make([]string, 0, len(result.Items))
	for n := range result.Items {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		out["files/"+name] = result.Items[name]
	}
	return out, nil
}
