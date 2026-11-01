package main

import (
	"errors"
	"path/filepath"
)

// OTA downloads and their transient files are always written under the
// installer's private cache/ota-images. This option ONLY changes which
// directory is scanned for previously saved local OTA candidates.
func resolveOTASearchDir(cacheRoot, chosen, legacy string) (string, error) {
	if chosen != "" && legacy != "" {
		return "", errors.New("--ota-search-dir and --tmp-dir cannot be combined")
	}
	if chosen == "" {
		chosen = legacy
	}
	if chosen == "" {
		chosen = otaArchiveDir(cacheRoot)
	}
	if !filepath.IsAbs(chosen) || filepath.Clean(chosen) == "/" {
		return "", errors.New("--ota-search-dir must be an absolute directory other than /")
	}
	return filepath.Clean(chosen), nil
}
