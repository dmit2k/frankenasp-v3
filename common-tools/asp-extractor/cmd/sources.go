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
func selectDesktopCache(o options, helperSHA string) (cacheResult, error) {
	entries, err := os.ReadDir(o.cacheDir)
	if err != nil {
		return cacheResult{}, err
	}
	var matched []cacheResult
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), o.platform+"-") {
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
		if meta.Platform != o.platform {
			return cacheResult{}, fmt.Errorf("manifest platform mismatch: %s", path)
		}
		if o.firmware != "" && o.firmware != entry.Name() && o.firmware != meta.Firmware && o.firmware != fireOSForCache(meta) {
			continue
		}
		if helperSHA != "" && meta.HelperSHA256 != helperSHA {
			continue // Another installer-helper revision is not a candidate.
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
	fmt.Println("Cache root:", o.cacheDir)
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "ota-images" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if o.platform != "" && !strings.HasPrefix(entry.Name(), o.platform+"-") {
			continue
		}
		path := filepath.Join(o.cacheDir, entry.Name())
		result, err := readCache(path)
		if err != nil {
			fmt.Printf("INVALID %s: %v\n", entry.Name(), err)
			continue
		}
		m := result.Metadata
		fmt.Printf("CACHE %s: platform=%s firmware=%s fireos=%s files=%d bytes=%d helper=%s\n", entry.Name(), m.Platform, m.Firmware, fireOSForCache(m), m.Files, m.Bytes, m.HelperSHA256[:12])
		count++
	}
	if count == 0 {
		fmt.Println("No verified caches")
	}
	otaDir := filepath.Join(o.cacheDir, "ota-images")
	if sources, err := os.ReadDir(otaDir); err == nil {
		for _, s := range sources {
			if s.Type().IsRegular() {
				fmt.Printf("OTA %s\n", s.Name())
			}
		}
	}
	return printBackupLinks(o)
}
func extractSource(ctx context.Context, o options) error {
	if err := ensureDesktopCache(o.cacheDir); err != nil {
		return err
	}
	release, err := loadRelease(o.release, o.platform)
	if err != nil {
		return err
	}
	child := release.Files["assets/asp1-child"]
	meter := newMeter()
	defer meter.Close()
	if o.backup != "" {
		result, err := extractBackup(ctx, o, child, meter)
		if err != nil {
			return err
		}
		if err = recordBackupLink(o, result); err != nil {
			return fmt.Errorf("backup cache created, but source index not saved: %w", err)
		}
		meter.Stop()
		fmt.Printf("CACHE %s: firmware=%s fireos=%s files=%d\n", result.Path, result.Metadata.Firmware, fireOSForCache(result.Metadata), result.Metadata.Files)
		return nil
	}
	searchDir, err := resolveOTASearchDir(o.cacheDir, "", "")
	if err != nil {
		return err
	}
	result, downloaded, cleanup, err := loadExtracted(ctx, o.cacheDir, o.platform, o.ota, o.otaURL, "", searchDir, "inspect", child, false, o.refresh, false, meter)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	meter.Stop()
	fmt.Printf("CACHE %s: firmware=%s fireos=%s files=%d source=%s\n", result.Path, result.Metadata.Firmware, fireOSForCache(result.Metadata), result.Metadata.Files, downloaded)
	return nil
}
func openBackupImages(dir, platform string) (*otaSources, []*os.File, error) {
	names := []string{"system.img"}
	if platform == "athena" {
		names = []string{"p21-system.img", "p12-vendor.img"}
	}
	handles := make([]*os.File, 0, len(names))
	closeAll := func() {
		for _, f := range handles {
			_ = f.Close()
		}
	}
	imgs := make([]*image, 0, len(names))
	for i, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		if !info.Mode().IsRegular() || info.Size() < 4096 || info.Size() > 4<<30 {
			closeAll()
			return nil, nil, fmt.Errorf("invalid raw backup image: %s", path)
		}
		f, err := os.Open(path)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		handles = append(handles, f)
		prefix := ""
		if platform == "athena" && i == 0 {
			prefix = "system"
		}
		imgs = append(imgs, &image{src: f, size: info.Size(), prefix: prefix})
	}
	src := &otaSources{platform: platform, kind: "BACKUP_RAW_EXT4", system: imgs[0]}
	if platform == "athena" {
		src.vendor = imgs[1]
	} else {
		src.vendor = prefixed{parent: imgs[0], prefix: "vendor"}
	}
	return src, handles, nil
}
func backupIdentity(ctx context.Context, dir, platform string) (string, error) {
	names := []string{"system.img"}
	if platform == "athena" {
		names = []string{"p21-system.img", "p12-vendor.img"}
	}
	digest := sha256.New()
	buf := make([]byte, 1<<20)
	for _, name := range names {
		_, _ = io.WriteString(digest, name+"\x00")
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		for {
			if err = ctx.Err(); err != nil {
				f.Close()
				return "", err
			}
			n, e := f.Read(buf)
			if n > 0 {
				_, _ = digest.Write(buf[:n])
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				f.Close()
				return "", e
			}
		}
		if err = f.Close(); err != nil {
			return "", err
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
func extractBackup(ctx context.Context, o options, child []byte, meter *progressMeter) (cacheResult, error) {
	info, err := os.Stat(o.backup)
	if err != nil {
		return cacheResult{}, err
	}
	if !info.IsDir() {
		return cacheResult{}, errors.New("--backup-dir must be a directory")
	}
	meter.Phase("Opening original ext4 backup images")
	src, files, err := openBackupImages(o.backup, o.platform)
	if err != nil {
		return cacheResult{}, err
	}
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	bp, err := src.system.Read("build.prop")
	if err != nil {
		return cacheResult{}, fmt.Errorf("cannot read backup build.prop: %w", err)
	}
	text := string(bp)
	firmware := firmwareLabel(property(text, "ro.build.fingerprint"), property(text, "ro.build.version.incremental"))
	if o.firmware != "" {
		if firmware != "unknown" && firmware != o.firmware {
			return cacheResult{}, fmt.Errorf("backup firmware %s differs from requested %s", firmware, o.firmware)
		}
		firmware = o.firmware
	}
	if firmware == "unknown" || firmware == "" {
		return cacheResult{}, errors.New("backup firmware not identified; supply --firmware")
	}
	meter.Phase("Hashing original backup partition pair for cache identity")
	sourceSHA, err := backupIdentity(ctx, o.backup, o.platform)
	if err != nil {
		return cacheResult{}, err
	}
	helperSHA := hashText(child)
	if !o.refresh {
		existing, err := findCache(o.cacheDir, o.platform, helperSHA, sourceSHA, "")
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
	if err = collected.closure(o.platform, child); err != nil {
		return cacheResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return cacheResult{}, err
	}
	fireos := knownFireOS(o.platform, firmware)
	meta := cacheMetadata{Platform: o.platform, Firmware: firmware, FireOS: fireos, SourceSHA256: sourceSHA, HelperSHA256: helperSHA, OTAFormat: src.kind, DependencyEdges: collected.edgeCount}
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
