package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Fire OS is the marketing version from Amazon's own build.prop, not the
// Android version nor an inferred translation of the numeric OTA build ID.
var fireOSPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,4}$`)

func parseFireOSBuildProp(content []byte) string {
	if len(content) > 128<<10 {
		return ""
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ro.build.mktg.fireos=") && !strings.HasPrefix(line, "ro.build.version.name=") {
			continue
		}
		value := strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
		if !strings.HasPrefix(value, "Fire OS ") {
			continue
		}
		value = strings.TrimPrefix(value, "Fire OS ")
		if k := strings.IndexByte(value, ' '); k >= 0 {
			value = value[:k]
		}
		if fireOSPattern.MatchString(value) {
			return value
		}
	}
	return ""
}

func fireOSFromOTA(source string) (string, error) {
	z, e := zip.OpenReader(source)
	if e != nil {
		return "", e
	}
	defer z.Close()
	for _, name := range []string{"system/build.prop", "META-INF/com/amazon/android/target.build.prop"} {
		for _, entry := range z.File {
			if entry.Name != name {
				continue
			}
			if entry.UncompressedSize64 > 128<<10 {
				return "", fmt.Errorf("oversized OTA build.prop: %s", name)
			}
			r, err := entry.Open()
			if err != nil {
				return "", err
			}
			raw, err := io.ReadAll(io.LimitReader(r, 128<<10+1))
			if cerr := r.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return "", err
			}
			if result := parseFireOSBuildProp(raw); result != "" {
				return result, nil
			}
		}
	}
	return "", nil
}

// Legacy extracted caches from older installer versions did not store the
// readable OS version. Only *verified exact known builds* have an offline
// fallback; unknown builds remain explicitly unidentified.
func knownFireOS(platform, build string) string {
	switch {
	case platform == "rook" && build == "690918120":
		return "5.5.6.9"
	case platform == "athena" && (build == "PS75741-7080" || build == "PS75741-7081" || build == "PS75741-7082"):
		return "7574.1"
	}
	return ""
}
func fireOSForCache(meta cacheMetadata) string {
	if fireOSPattern.MatchString(meta.FireOS) {
		return meta.FireOS
	}
	return knownFireOS(meta.Platform, meta.Firmware)
}
func displayPlatform(platform string) string {
	switch platform {
	case "rook":
		return "Echo Spot (1st Gen, 2017) [rook]"
	case "athena":
		return "Echo Show 8 (2nd Gen, 2021) [athena]"
	default:
		return "unidentified"
	}
}

func cacheFilename(meta cacheMetadata) string {
	if osVersion := fireOSForCache(meta); osVersion != "" {
		return meta.Platform + "-fireos-" + osVersion + "-build-" + meta.Firmware + "-cache-v2"
	}
	return meta.Platform + "-" + meta.Firmware + "-cache-v2"
}

// Called with the existing exclusive cache lock. Legacy directories are
// verified before any changes; no extraction is repeated and no OTA is deleted.
func migrateCacheNames(base string, m *progressMeter) error {
	entries, e := os.ReadDir(base)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == otaImageDir || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		oldPath := filepath.Join(base, entry.Name())
		manifestBytes, er := os.ReadFile(filepath.Join(oldPath, "manifest.json"))
		if er != nil {
			return er
		}
		var manifest cacheMetadata
		if er = json.Unmarshal(manifestBytes, &manifest); er != nil {
			return er
		}
		if manifest.Schema == "ASP1-EXTRACTED-CACHE-V1" {
			continue // Legacy cache is preserved but not eligible for v2 runtime.
		}
		cache, e := readCache(oldPath)
		if e != nil {
			return fmt.Errorf("cannot migrate invalid cache %s: %w", entry.Name(), e)
		}
		meta := cache.Metadata
		version := fireOSForCache(meta)
		if version == "" {
			continue
		}
		desired := cacheFilename(meta)
		// Preserve numbered variants, including old legacy names.
		suffix := cacheVariantSuffix(entry.Name(), meta)
		// readCache already validated the old or new directory identity.
		newName := desired + suffix
		if meta.FireOS == version && entry.Name() == newName {
			continue
		}
		newPath := filepath.Join(base, newName)
		if newPath != oldPath {
			if _, statErr := os.Lstat(newPath); statErr == nil {
				return fmt.Errorf("cache rename collision: %s", newPath)
			} else if !os.IsNotExist(statErr) {
				return statErr
			}
		}
		originalOTA := filepath.Join(otaArchiveDir(base), entry.Name()+".bin")
		newOTA := filepath.Join(otaArchiveDir(base), newName+".bin")
		hasOTA := false
		if _, e := os.Lstat(originalOTA); e == nil {
			hasOTA = true
			if _, e := checkArchiveFileForMigration(originalOTA, meta.SourceSHA256); e != nil {
				return e
			}
			if newOTA != originalOTA {
				if _, statErr := os.Lstat(newOTA); statErr == nil {
					return fmt.Errorf("OTA rename collision: %s", newOTA)
				} else if !os.IsNotExist(statErr) {
					return statErr
				}
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if meta.FireOS != version {
			meta.FireOS = version
			raw, e := json.MarshalIndent(meta, "", "  ")
			if e != nil {
				return e
			}
			// Atomic metadata replacement, same owner-only protections as cache writer.
			temp, e := os.CreateTemp(oldPath, ".manifest-upgrade-")
			if e != nil {
				return e
			}
			tempName := temp.Name()
			if e = temp.Chmod(0600); e != nil {
				temp.Close()
				os.Remove(tempName)
				return e
			}
			if _, e = temp.Write(append(raw, '\n')); e == nil {
				e = temp.Sync()
			}
			if closeErr := temp.Close(); e == nil {
				e = closeErr
			}
			if e != nil {
				os.Remove(tempName)
				return e
			}
			if e = os.Rename(tempName, filepath.Join(oldPath, "manifest.json")); e != nil {
				os.Remove(tempName)
				return e
			}
			if e = fsyncDir(oldPath); e != nil {
				return e
			}
		}
		if newPath != oldPath {
			if e = unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE); e != nil {
				return e
			}
			if e = fsyncDir(base); e != nil {
				return e
			}
		}
		if hasOTA && originalOTA != newOTA {
			if e = unix.Renameat2(unix.AT_FDCWD, originalOTA, unix.AT_FDCWD, newOTA, unix.RENAME_NOREPLACE); e != nil {
				return e
			}
			if e = fsyncDir(otaArchiveDir(base)); e != nil {
				return e
			}
		}
		if m != nil && oldPath != newPath {
			m.Note("Renamed verified firmware cache: " + entry.Name() + " -> " + newName)
		}
	}
	return nil
}
func cacheVariantSuffix(name string, meta cacheMetadata) string {
	candidates := []string{meta.Platform + "-" + meta.Firmware, cacheFilename(meta)}
	for _, base := range candidates {
		if name == base {
			return ""
		}
		if strings.HasPrefix(name, base+"-variant-") {
			suffix := strings.TrimPrefix(name, base)
			num := strings.TrimPrefix(suffix, "-variant-")
			value, err := strconv.Atoi(num)
			if err == nil && value >= 2 && value <= 999 && strconv.Itoa(value) == num {
				return suffix
			}
		}
	}
	return ""
}
func checkArchiveFileForMigration(file, hash string) (int64, error) {
	return checkArchive(context.Background(), file, hash)
}
