package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func loadExtracted(ctx context.Context, cacheDir, platform, sourcePath, sourceURL, tmpDir, cmd string, child []byte, noBackup, refresh, deleteOTA bool, m *progressMeter) (cacheResult, string, func(), error) {
	urlHash := ""
	sourceHash := ""
	helperHash := hashText(child)
	if sourceURL != "" {
		urlHash = hashString(sourceURL)
	} else {
		m.Phase("Hashing local OTA for cache identity")
		digest, e := hashOTA(ctx, sourcePath)
		if e != nil {
			return cacheResult{}, "", nil, e
		}
		sourceHash = digest
	}
	if !refresh {
		cached, e := findCache(cacheDir, platform, helperHash, sourceHash, urlHash)
		if e == nil {
			m.Note("Verified extracted OTA cache reused; no download or partition scan necessary")
			describeCache(m, cached)
			return cached, "", nil, nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return cacheResult{}, "", nil, e
		}
	}
	if sourceURL != "" && !refresh {
		m.Phase("Looking for already-downloaded OTA matching the extracted cache")
		cached, localPath, err := cachedLocalOTAForURL(ctx, cacheDir, tmpDir, platform, helperHash, sourceURL, m)
		if err == nil {
			m.Note("Previously downloaded OTA remains on disk: " + localPath)
			return cached, "", nil, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return cacheResult{}, "", nil, err
		}
	}
	if e := ctx.Err(); e != nil {
		return cacheResult{}, "", nil, e
	}
	downloadBudget, e := preflightCacheAndOTA(cacheDir, tmpDir, cmd, sourceURL != "", noBackup, m)
	if e != nil {
		return cacheResult{}, "", nil, e
	}
	source := sourcePath
	var cleanup func()
	if sourceURL != "" {
		m.Phase("Downloading OTA: no validated cache matches the requested URL")
		source, cleanup, e = downloadWithBudget(ctx, sourceURL, tmpDir, downloadBudget, m)
		if e != nil {
			return cacheResult{}, "", nil, e
		}
		if deleteOTA {
			m.Note("OTA will be deleted after operation: " + source)
		} else {
			m.Note("OTA saved for reuse: " + source)
		}
		sourceHash, e = hashOTA(ctx, source)
		if e != nil {
			return cacheResult{}, source, cleanup, e
		}
		// A different URL can deliver bytes already present in another verified cache.
		if !refresh {
			found, er := findCache(cacheDir, platform, helperHash, sourceHash, "")
			if er == nil {
				m.Note("Downloaded OTA content matches an existing verified cache")
				describeCache(m, found)
				return found, source, cleanup, nil
			}
			if !errors.Is(er, os.ErrNotExist) {
				return cacheResult{}, source, cleanup, er
			}
		}
	}
	m.Phase("Reading OTA metadata and partition layout")
	src, handle, e := openOTA(source, platform)
	if e != nil {
		return cacheResult{}, source, cleanup, e
	}
	defer handle.Close()
	for _, reader := range src.selective {
		reader.ctx = ctx
		reader.meter = m
	}
	m.Phase("Extracting factory libraries and dependencies (first use only)")
	if len(src.selective) > 0 {
		m.Note("Scans fetch multiple files; cache will skip all scans on later commands")
	}
	collected := newCollector(src)
	if e = collected.closure(platform, child); e != nil {
		return cacheResult{}, source, cleanup, fmt.Errorf("factory dependency closure: %w", e)
	}
	if e = ctx.Err(); e != nil {
		return cacheResult{}, source, cleanup, e
	}
	meta := cacheMetadata{Platform: platform, Firmware: src.firmware, SourceSHA256: sourceHash, HelperSHA256: helperHash, URLSHA256: urlHash, OTAFormat: src.kind, DependencyEdges: collected.edgeCount}
	m.Phase("Writing extracted artifact cache with SHA-256 inventory")
	saved, e := publishCache(cacheDir, meta, collected.items, refresh, m)
	if e != nil {
		// In case a concurrent independently-created cache is already present,
		// use it only after full hash verification.
		if errors.Is(e, os.ErrExist) && !refresh {
			existing, er := findCache(cacheDir, platform, helperHash, sourceHash, "")
			if er == nil {
				return existing, source, cleanup, nil
			}
		}
		return cacheResult{}, source, cleanup, e
	}
	m.Note("Cache created: " + saved.Path)
	return saved, source, cleanup, nil
}
func cacheDefaultPath() string { return filepath.Join(parentPath(), cacheDirName) }
