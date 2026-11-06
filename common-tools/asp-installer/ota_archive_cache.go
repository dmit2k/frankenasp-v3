package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// OTA archives are separate from extracted files and SHA256SUMS. Keeping them
// under a dedicated cache-root subdirectory permits a readable firmware name
// without requiring the whole 1GiB+ image for each extracted-cache validation.
const otaImageDir = "ota-images"

func otaArchiveDir(base string) string { return filepath.Join(base, otaImageDir) }
func archiveForExtraction(result cacheResult) string {
	return filepath.Join(filepath.Dir(result.Path), otaImageDir, filepath.Base(result.Path)+".bin")
}
func ensureOTADir(base string) (string, error) {
	dir := otaArchiveDir(base)
	if e := requireCacheDir(dir); e != nil {
		return "", e
	}
	return dir, nil
}
func checkArchive(ctx context.Context, path, expectedHash string) (int64, error) {
	if e := checkCacheNode(path, true); e != nil {
		return 0, e
	}
	hash, e := hashLocalCandidate(ctx, path)
	if e != nil {
		return 0, e
	}
	if hash != expectedHash {
		return 0, fmt.Errorf("cached OTA SHA-256 mismatch: %s", path)
	}
	st, e := os.Stat(path)
	if e != nil {
		return 0, e
	}
	return st.Size(), nil
}

// archiveExisting returns the matching, fully verified OTA image if present.
// It does not trust an arbitrary filename or a previously cached hash alone.
func archiveExisting(ctx context.Context, result cacheResult) (string, error) {
	path := archiveForExtraction(result)
	_, e := checkArchive(ctx, path, result.Metadata.SourceSHA256)
	if e != nil {
		return "", e
	}
	return path, nil
}

// saveOTAArchive publishes a complete OTA under the readable extraction-cache
// identity. A downloaded temporary file can be renamed without a second copy.
// User-supplied images are never removed or renamed.
// Only program-downloaded OTA archives may be moved into ota-images.
// A user-supplied local OTA must remain at its original path and is never
// copied or moved into the private cache.
func saveOTAArchive(ctx context.Context, result cacheResult, src string, downloaded bool, m *progressMeter) (string, error) {
	if !downloaded {
		return "", errors.New("local OTA images are not archived; only files downloaded by the installer/extractor may be saved")
	}
	if src == "" {
		return "", errors.New("downloaded OTA source missing")
	}
	base := filepath.Dir(result.Path)
	dir, e := ensureOTADir(base)
	if e != nil {
		return "", e
	}
	dest := archiveForExtraction(result)
	if p, err := archiveExisting(ctx, result); err == nil {
		if filepath.Clean(src) != filepath.Clean(p) {
			_ = os.Remove(src)
		}
		return p, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("existing cached OTA cannot be replaced implicitly: %w", err)
	}
	if filepath.Clean(src) == filepath.Clean(dest) {
		_, e = checkArchive(ctx, dest, result.Metadata.SourceSHA256)
		return dest, e
	}
	if filepath.Clean(filepath.Dir(src)) != filepath.Clean(dir) {
		return "", errors.New("download outside OTA archive directory")
	}
	if _, e = checkArchive(ctx, src, result.Metadata.SourceSHA256); e != nil {
		return "", e
	}
	if e = unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dest, unix.RENAME_NOREPLACE); e != nil {
		return "", e
	}
	if e = fsyncDir(dir); e != nil {
		return "", e
	}
	if _, e = checkArchive(ctx, dest, result.Metadata.SourceSHA256); e != nil {
		return "", e
	}
	if m != nil {
		m.Note("Downloaded OTA retained: " + dest)
	}
	return dest, nil
}

type imageStatus struct {
	Firmware      string `json:"firmware,omitempty"`
	FireOS        string `json:"fire_os_version,omitempty"`
	PlatformLabel string `json:"platform_label,omitempty"`
	Platform      string `json:"platform,omitempty"`
	Path          string `json:"path"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	State         string `json:"state"`
	Error         string `json:"error,omitempty"`
}
type extractionStatus struct {
	Firmware      string `json:"firmware"`
	FireOS        string `json:"fire_os_version,omitempty"`
	PlatformLabel string `json:"platform_label,omitempty"`
	Platform      string `json:"platform"`
	Path          string `json:"path"`
	FactoryFiles  int    `json:"factory_files,omitempty"`
	OTAImage      string `json:"ota_image,omitempty"`
	State         string `json:"state"`
	Error         string `json:"error,omitempty"`
}

func cacheInventory(ctx context.Context, base string) ([]extractionStatus, []imageStatus) {
	extra := []extractionStatus{}
	images := []imageStatus{}
	entries, e := os.ReadDir(base)
	if os.IsNotExist(e) {
		return extra, images
	}
	if e != nil {
		return []extractionStatus{{State: "error", Path: base, Error: e.Error()}}, images
	}
	known := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == otaImageDir || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		p := filepath.Join(base, entry.Name())
		state := extractionStatus{Path: p, State: "invalid"}
		found, err := readCache(p)
		if err != nil {
			state.Error = err.Error()
			extra = append(extra, state)
			continue
		}
		state.State = "ready"
		state.Firmware = found.Metadata.Firmware
		state.FireOS = fireOSForCache(found.Metadata)
		state.PlatformLabel = displayPlatform(found.Metadata.Platform)
		state.Platform = found.Metadata.Platform
		state.FactoryFiles = found.Metadata.Files
		imagePath := archiveForExtraction(found)
		known[imagePath] = true
		if _, err = archiveExisting(ctx, found); err == nil {
			state.OTAImage = imagePath
			imageStat, _ := os.Stat(imagePath)
			images = append(images, imageStatus{Firmware: state.Firmware, FireOS: state.FireOS, PlatformLabel: state.PlatformLabel, Platform: state.Platform, Path: imagePath, SizeBytes: imageStat.Size(), SHA256: found.Metadata.SourceSHA256, State: "ready"})
		} else if !os.IsNotExist(err) {
			images = append(images, imageStatus{Firmware: state.Firmware, FireOS: state.FireOS, PlatformLabel: state.PlatformLabel, Platform: state.Platform, Path: imagePath, State: "invalid", Error: err.Error()})
		}
		extra = append(extra, state)
	}
	imageDir := otaArchiveDir(base)
	loose, err := os.ReadDir(imageDir)
	if err == nil {
		for _, entry := range loose {
			path := filepath.Join(imageDir, entry.Name())
			if known[path] || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			item := imageStatus{Path: path, State: "unassociated"}
			if digest, e := hashLocalCandidate(ctx, path); e != nil {
				item.State = "invalid"
				item.Error = e.Error()
			} else {
				item.SHA256 = digest
				if st, er := os.Stat(path); er == nil {
					item.SizeBytes = st.Size()
				}
			}
			images = append(images, item)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Path < extra[j].Path })
	sort.Slice(images, func(i, j int) bool { return images[i].Path < images[j].Path })
	return extra, images
}

func cacheForURLFilename(ctx context.Context, base, _ string, rawURL string) (cacheResult, error) {
	parsed, e := url.Parse(rawURL)
	if e != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return cacheResult{}, errors.New("invalid OTA URL")
	}
	filename := safeOTAName(path.Base(parsed.Path))
	if filename == "firmware.bin" {
		return cacheResult{}, os.ErrNotExist
	}
	entries, e := os.ReadDir(base)
	if e != nil {
		return cacheResult{}, e
	}
	candidates := []cacheResult{}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Name() == otaImageDir {
			continue
		}
		p := filepath.Join(base, entry.Name())
		metaRaw, err := os.ReadFile(filepath.Join(p, "manifest.json"))
		if err != nil {
			continue
		}
		var meta cacheMetadata
		if json.Unmarshal(metaRaw, &meta) != nil {
			continue
		}
		if meta.Schema != cacheSchema || meta.OriginalOTAName != filename {
			continue
		}
		found, err := readCache(p)
		if err != nil {
			return cacheResult{}, fmt.Errorf("invalid matching cache %s: %w", p, err)
		}
		if _, err = archiveExisting(ctx, found); err == nil {
			candidates = append(candidates, found)
		} else if !os.IsNotExist(err) {
			return cacheResult{}, fmt.Errorf("invalid OTA archive %s: %w", p, err)
		}
	}
	if len(candidates) == 0 {
		return cacheResult{}, os.ErrNotExist
	}
	if len(candidates) != 1 {
		return cacheResult{}, fmt.Errorf("multiple caches match OTA basename %q; specify --firmware or --ota explicitly", filename)
	}
	return candidates[0], nil
}

func pendingOTAForURL(ctx context.Context, base, rawURL string) (string, string, error) {
	parsed, e := url.Parse(rawURL)
	if e != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", "", errors.New("invalid OTA URL")
	}
	filename := safeOTAName(path.Base(parsed.Path))
	if filename == "firmware.bin" {
		return "", "", os.ErrNotExist
	}
	entries, e := os.ReadDir(otaArchiveDir(base))
	if os.IsNotExist(e) {
		return "", "", os.ErrNotExist
	}
	if e != nil {
		return "", "", e
	}
	var candidate, hash string
	for _, entry := range entries {
		if !localOTACandidateName(entry.Name(), filename) || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		p := filepath.Join(otaArchiveDir(base), entry.Name())
		digest, err := hashLocalCandidate(ctx, p)
		if err != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			continue
		}
		if candidate != "" {
			return "", "", fmt.Errorf("several saved OTA archives match %s; choose --ota explicitly", filename)
		}
		candidate, hash = p, digest
	}
	if candidate == "" {
		return "", "", os.ErrNotExist
	}
	return candidate, hash, nil
}

func attachOriginalOTAName(result cacheResult, filename string) error {
	if result.Metadata.OriginalOTAName != "" {
		return nil
	}
	filename = safeOTAName(filepath.Base(filename))
	if strings.HasPrefix(filename, "asp-installer-ota-") {
		raw := strings.TrimPrefix(filename, "asp-installer-ota-")
		if len(raw) > 13 && raw[12] == '-' {
			if _, er := hex.DecodeString(raw[:12]); er == nil {
				filename = raw[13:]
			}
		}
	}
	if filename == "firmware.bin" || filename == filepath.Base(result.Path)+".bin" {
		return nil
	}
	meta := result.Metadata
	meta.OriginalOTAName = filename
	raw, e := json.MarshalIndent(meta, "", "  ")
	if e != nil {
		return e
	}
	root := filepath.Dir(result.Path)
	tmp, e := os.CreateTemp(root, ".cache-manifest-")
	if e != nil {
		return e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if e = tmp.Chmod(0600); e != nil {
		tmp.Close()
		return e
	}
	if _, e = tmp.Write(append(raw, '\n')); e != nil {
		tmp.Close()
		return e
	}
	if e = tmp.Sync(); e != nil {
		tmp.Close()
		return e
	}
	if e = tmp.Close(); e != nil {
		return e
	}
	if e = os.Rename(name, filepath.Join(result.Path, "manifest.json")); e != nil {
		return e
	}
	return fsyncDir(result.Path)
}
