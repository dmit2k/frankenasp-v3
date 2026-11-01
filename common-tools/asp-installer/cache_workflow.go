package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
)

func loadExtracted(ctx context.Context, cacheDir, platform, sourcePath, sourceURL, firmware, otaSearchDir, cmd string, child []byte, noBackup, refresh, deleteOTA bool, m *progressMeter) (cacheResult, string, func(), error) {
	if sourcePath == "" && sourceURL == "" {
		result, e := selectFirmwareCache(cacheDir, platform, hashText(child), firmware)
		if e != nil {
			return cacheResult{}, "", nil, e
		}
		m.Note("Using verified firmware cache: " + result.Metadata.Firmware)
		describeCache(m, result)
		if _, er := archiveExisting(ctx, result); er != nil {
			m.Note("OTA archive not available or invalid; extracted cache is verified and usable: " + er.Error())
		}
		return result, "", nil, nil
	}
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
			if sourcePath != "" {
				archiveLocalIfPossible(ctx, cached, sourcePath, m)
			}
			return cached, "", nil, nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return cacheResult{}, "", nil, e
		}
	}
	if sourceURL != "" && !refresh {
		fromArchive, err := cacheForURLFilename(ctx, cacheDir, platform, helperHash, sourceURL)
		if err == nil {
			m.Note("Reusing archived OTA with matching source filename and verified SHA-256; no network request")
			describeCache(m, fromArchive)
			return fromArchive, "", nil, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return cacheResult{}, "", nil, err
		}
	}
	if sourceURL != "" && !refresh {
		m.Phase("Looking for already-downloaded OTA matching the extracted cache")
		cached, localPath, err := cachedLocalOTAForURL(ctx, cacheDir, otaSearchDir, platform, helperHash, sourceURL, m)
		if err == nil {
			archiveLocalIfPossible(ctx, cached, localPath, m)
			return cached, "", nil, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return cacheResult{}, "", nil, err
		}
	}
	if e := ctx.Err(); e != nil {
		return cacheResult{}, "", nil, e
	}
	// Preflight uses the existing private cache root for local OTA sources.
	// For URL downloads the actual temporary file is created only inside the
	// private cache's ota-images directory, never the OTA search directory.
	downloadDir := cacheDir
	if sourceURL != "" {
		var err error
		downloadDir, err = ensureOTADir(cacheDir)
		if err != nil {
			return cacheResult{}, "", nil, err
		}
	}
	pendingPath := ""
	if sourceURL != "" && !refresh {
		existing, digest, er := pendingOTAForURL(ctx, cacheDir, sourceURL)
		if er == nil {
			pendingPath = existing
			sourceHash = digest
			m.Note("Reusing previously downloaded, not-yet-extracted OTA: " + pendingPath)
		} else if !errors.Is(er, os.ErrNotExist) {
			return cacheResult{}, "", nil, er
		}
	}
	downloadBudget, e := preflightCacheAndOTA(cacheDir, downloadDir, cmd, sourceURL != "" && pendingPath == "", noBackup, m)
	if e != nil {
		return cacheResult{}, "", nil, e
	}
	source := sourcePath
	if pendingPath != "" {
		source = pendingPath
	}
	var cleanup func()
	if sourceURL != "" && pendingPath == "" {
		m.Phase("Downloading OTA: no validated cache matches the requested URL")
		source, cleanup, e = downloadWithBudget(ctx, sourceURL, downloadDir, downloadBudget, m)
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
				if deleteOTA {
					return found, source, cleanup, nil
				}
				archived, arerr := saveOTAArchive(ctx, found, source, true, m)
				if arerr != nil {
					return cacheResult{}, source, cleanup, arerr
				}
				return found, archived, cleanup, nil
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
	originalName := filepath.Base(source)
	if sourceURL != "" {
		parsed, err := url.Parse(sourceURL)
		if err == nil {
			originalName = path.Base(parsed.Path)
		}
	}
	originalName = safeOTAName(originalName)
	version, versionErr := fireOSFromOTA(source)
	if versionErr != nil {
		return cacheResult{}, source, cleanup, fmt.Errorf("read Fire OS version from OTA: %w", versionErr)
	}
	if version == "" {
		version = knownFireOS(platform, src.firmware)
	}
	meta := cacheMetadata{Platform: platform, Firmware: src.firmware, FireOS: version, SourceSHA256: sourceHash, HelperSHA256: helperHash, URLSHA256: urlHash, OriginalOTAName: originalName, OTAFormat: src.kind, DependencyEdges: collected.edgeCount}
	m.Phase("Writing extracted artifact cache with SHA-256 inventory")
	saved, e := publishCache(cacheDir, meta, collected.items, refresh, m)
	if e != nil {
		// In case a concurrent independently-created cache is already present,
		// use it only after full hash verification.
		if errors.Is(e, os.ErrExist) && !refresh {
			existing, er := findCache(cacheDir, platform, helperHash, sourceHash, "")
			if er == nil {
				if sourceURL == "" {
					archiveLocalIfPossible(ctx, existing, source, m)
					return existing, "", cleanup, nil
				}
				if deleteOTA {
					return existing, source, cleanup, nil
				}
				archived, saveErr := saveOTAArchive(ctx, existing, source, true, m)
				if saveErr != nil {
					return cacheResult{}, source, cleanup, saveErr
				}
				return existing, archived, cleanup, nil
			}
		}
		return cacheResult{}, source, cleanup, e
	}
	m.Note("Cache created: " + saved.Path)
	if sourceURL != "" {
		if deleteOTA {
			return saved, source, cleanup, nil
		}
		archived, err := saveOTAArchive(ctx, saved, source, true, m)
		if err != nil {
			return cacheResult{}, source, cleanup, err
		}
		return saved, archived, cleanup, nil
	}
	archiveLocalIfPossible(ctx, saved, source, m)
	return saved, "", cleanup, nil
}
func cacheDefaultPath() string { return filepath.Join(parentPath(), cacheDirName) }

func archiveLocalIfPossible(ctx context.Context, result cacheResult, localPath string, m *progressMeter) {
	if localPath == "" {
		return
	}
	if _, err := saveOTAArchive(ctx, result, localPath, false, m); err != nil {
		m.Note("WARNING: could not store a private copy of local OTA in cache (extracted files remain usable): " + err.Error())
	} else if err = attachOriginalOTAName(result, localPath); err != nil {
		m.Note("WARNING: OTA image saved, but source filename could not be indexed: " + err.Error())
	}
}
