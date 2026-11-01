package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Select only a verified platform-specific factory cache (helper-independent).
// An unspecified version is safe only when exactly one candidate exists.
// --firmware accepts either firmware label or a specific cache directory name,
// allowing a user to disambiguate numbered firmware variants.
func selectFirmwareCache(base, platform, requested string) (cacheResult, error) {
	entries, e := os.ReadDir(base)
	if e != nil {
		return cacheResult{}, e
	}
	candidates := []cacheResult{}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), platform+"-") {
			continue
		}
		name := entry.Name()
		path := filepath.Join(base, name)
		raw, err := os.ReadFile(filepath.Join(path, "manifest.json"))
		if err != nil {
			continue
		}
		var meta cacheMetadata
		if json.Unmarshal(raw, &meta) != nil || meta.Schema != cacheSchema {
			continue
		}
		if meta.Platform != platform {
			continue
		}
		if requested != "" && requested != meta.Firmware && requested != fireOSForCache(meta) && requested != name {
			continue
		}
		result, err := readCache(path)
		if err != nil {
			return cacheResult{}, fmt.Errorf("selected cache %s invalid: %w", name, err)
		}
		candidates = append(candidates, result)
	}
	if len(candidates) == 0 {
		if requested != "" {
			return cacheResult{}, fmt.Errorf("no verified cache for firmware %q on %s; use inspect --ota or --ota-url", requested, platform)
		}
		return cacheResult{}, errors.New("no verified extracted cache for this platform; supply --ota or --ota-url")
	}
	if len(candidates) > 1 {
		names := make([]string, len(candidates))
		for i, c := range candidates {
			names[i] = filepath.Base(c.Path)
		}
		sort.Strings(names)
		return cacheResult{}, fmt.Errorf("multiple firmware caches available (%s); choose --firmware NAME", strings.Join(names, ", "))
	}
	return candidates[0], nil
}
