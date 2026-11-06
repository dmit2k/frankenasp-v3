package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
)

// Read the available OTA identity before a hash-based early cache hit.
// File-tree build properties can be checked without decompressing a block OTA.
func otaSourceCodename(path string) (string, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer z.Close()
	var otaMeta, buildProp []byte
	for _, member := range z.File {
		switch member.Name {
		case "META-INF/com/android/metadata":
			otaMeta, err = readZip(member, 65536)
		case "system/build.prop", "system/system/build.prop":
			buildProp, err = readZip(member, 128<<10)
		default:
			continue
		}
		if err != nil {
			return "", err
		}
	}
	fromOTA, err := sourceCodename(metadataValue(otaMeta, "post-device"), metadataValue(otaMeta, "post-build"))
	if err != nil {
		return "", err
	}
	fromSystem, err := buildPropCodename(buildProp)
	if err != nil {
		return "", err
	}
	if fromOTA != "" && fromSystem != "" && fromOTA != fromSystem {
		return "", fmt.Errorf("OTA source platform %s conflicts with system build platform %s", fromOTA, fromSystem)
	}
	if fromSystem != "" {
		return fromSystem, nil
	}
	return fromOTA, nil
}

// Check inexpensive ZIP-level identity first. A match here is not proof that
// the internal filesystem agrees; all early hits use checkAvailableOTAIdentity.
func checkZIPOTAIdentity(path, requested string) error {
	detected, err := otaSourceCodename(path)
	if errors.Is(err, zip.ErrFormat) {
		// Historical cache fixtures may be opaque source bytes; attempt the
		// SHA-256 lookup, and let the normal reader reject an uncached input.
		return nil
	}
	if err != nil {
		return err
	}
	_, err = resolveCodename(requested, detected)
	return err
}

// Check available ZIP and filesystem identities before reusing a matching
// extracted cache with an explicit --platform. This reads build.prop through
// the existing selective block reader without re-extracting factory files.
// Legacy opaque cached source fixtures have no inspectable OTA identity.
func checkAvailableOTAIdentity(path, requested string) error {
	if requested == "" {
		return nil
	}
	src, handle, err := openOTA(path, requested)
	if errors.Is(err, zip.ErrFormat) {
		return nil
	}
	if err != nil {
		return err
	}
	defer handle.Close()
	// File-tree identities are already directly available from ZIP members.
	// Use their bounded reader, which also accepts historical empty fixtures.
	if src.kind == "ZIP_FILE_TREE" {
		return checkZIPOTAIdentity(path, requested)
	}
	props, err := readSystemBuildProp(src)
	if err != nil {
		return err
	}
	detected, err := buildPropCodename(props)
	if err != nil {
		return err
	}
	_, err = resolveCodename(requested, detected)
	return err
}

func loadExtracted(ctx context.Context, cacheDir, platform, sourcePath, sourceURL, firmware, otaSearchDir, cmd string, noBackup, refresh, deleteOTA bool, m *progressMeter) (cacheResult, string, func(), error) {
	if sourcePath == "" && sourceURL == "" {
		result, e := selectFirmwareCache(cacheDir, platform, firmware)
		if e != nil {
			return cacheResult{}, "", nil, e
		}
		// A verified extracted cache is sufficient for installation and checks.
		// Full OTA archives are optional and should not be accessed or diagnosed
		// when the user explicitly selected an existing factory extraction.
		if cmd == "inspect" {
			m.Note("Verified factory files: " + result.Metadata.Firmware)
		}
		return result, "", nil, nil
	}
	urlHash := ""
	sourceHash := ""
	if sourcePath != "" {
		if err := checkZIPOTAIdentity(sourcePath, platform); err != nil {
			return cacheResult{}, "", nil, err
		}
	}
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
		cached, e := findCache(cacheDir, platform, sourceHash, urlHash)
		if e == nil {
			if sourcePath != "" {
				if err := checkAvailableOTAIdentity(sourcePath, platform); err != nil {
					return cacheResult{}, "", nil, err
				}
			}
			if sourceURL != "" {
				if archive, err := archiveExisting(ctx, cached); err == nil {
					if err := checkAvailableOTAIdentity(archive, platform); err != nil {
						return cacheResult{}, "", nil, err
					}
				}
			}
			m.Note("Verified extracted OTA cache reused; no download or partition scan necessary")
			describeCache(m, cached)
			return cached, "", nil, nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return cacheResult{}, "", nil, e
		}
	}
	if sourceURL != "" && !refresh {
		fromArchive, err := cacheForURLFilename(ctx, cacheDir, platform, sourceURL)
		if err == nil {
			if err := checkAvailableOTAIdentity(archiveForExtraction(fromArchive), platform); err != nil {
				return cacheResult{}, "", nil, err
			}
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
		cached, _, err := cachedLocalOTAForURL(ctx, cacheDir, otaSearchDir, platform, sourceURL, m)
		if err == nil {
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
		// A downloaded source must be checked before a hash-only cache hit.
		if err := checkZIPOTAIdentity(source, platform); err != nil {
			return cacheResult{}, source, cleanup, err
		}
		if !refresh {
			found, er := findCache(cacheDir, platform, sourceHash, "")
			if er == nil {
				if err := checkAvailableOTAIdentity(source, platform); err != nil {
					return cacheResult{}, source, cleanup, err
				}
				m.Note("Downloaded OTA content matches an existing verified cache")
				describeCache(m, found)
				if deleteOTA {
					return found, source, cleanup, nil
				}
				if found.Metadata.SourceSHA256 != sourceHash {
					m.Note("Downloaded OTA retained at " + source + "; identical factory files are already cached")
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
	// Cross-check filesystem metadata independently of OTA ZIP metadata.
	buildProp, err := readSystemBuildProp(src)
	if err != nil {
		return cacheResult{}, source, cleanup, err
	}
	detected, err := buildPropCodename(buildProp)
	if err != nil {
		return cacheResult{}, source, cleanup, err
	}
	platform, err = resolveCodename(src.platform, detected)
	if err != nil {
		return cacheResult{}, source, cleanup, err
	}
	src.platform = platform
	if src.firmware == "unknown" || src.firmware == "" {
		src.firmware = firmwareLabel(sourceProperty(buildProp, "ro.build.fingerprint"), sourceProperty(buildProp, "ro.build.version.incremental"))
	}
	if src.firmware == "unknown" || src.firmware == "" {
		src.firmware = "unknown-" + sourceHash[:12]
	}
	m.Phase("Extracting factory libraries and dependencies (first use only)")
	if len(src.selective) > 0 {
		m.Note("Scans fetch multiple files; cache will skip all scans on later commands")
	}
	collected := newCollector(src)
	if e = collected.closure(); e != nil {
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
		version = parseFireOSBuildProp(buildProp)
	}
	if version == "" {
		version = knownFireOS(platform, src.firmware)
	}
	meta := cacheMetadata{Platform: platform, Firmware: src.firmware, FireOS: version, SourceSHA256: sourceHash, URLSHA256: urlHash, OriginalOTAName: originalName, OTAFormat: src.kind, DependencyEdges: collected.edgeCount}
	m.Phase("Writing extracted artifact cache with SHA-256 inventory")
	saved, e := publishCache(cacheDir, meta, collected.items, refresh, m)
	if e != nil {
		// In case a concurrent independently-created cache is already present,
		// use it only after full hash verification.
		if errors.Is(e, os.ErrExist) && !refresh {
			existing, er := findCache(cacheDir, platform, sourceHash, "")
			if er == nil {
				if sourceURL == "" {
					return existing, "", cleanup, nil
				}
				if deleteOTA {
					return existing, source, cleanup, nil
				}
				if existing.Metadata.SourceSHA256 != sourceHash {
					m.Note("Downloaded OTA retained at " + source + "; identical factory files are already cached")
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
	if !saved.Hit {
		m.Note("Cache created: " + saved.Path)
	}
	if sourceURL != "" {
		if deleteOTA {
			return saved, source, cleanup, nil
		}
		if saved.Metadata.SourceSHA256 != sourceHash {
			m.Note("Downloaded OTA retained at " + source + "; identical factory files are already cached")
			return saved, source, cleanup, nil
		}
		archived, err := saveOTAArchive(ctx, saved, source, true, m)
		if err != nil {
			return cacheResult{}, source, cleanup, err
		}
		return saved, archived, cleanup, nil
	}
	return saved, "", cleanup, nil
}
func cacheDefaultPath() string { return filepath.Join(parentPath(), cacheDirName) }
