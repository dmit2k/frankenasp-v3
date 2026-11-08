package asp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
func extractBackup(ctx context.Context, cacheDir, platformReq, firmwareReq, backupDir string, refresh bool, meter *progressMeter) (cacheResult, error) {
	info, err := os.Stat(backupDir)
	if err != nil {
		return cacheResult{}, err
	}
	if !info.IsDir() {
		return cacheResult{}, errors.New("--backup-dir must be a directory")
	}
	meter.Phase("Opening original ext4 backup images")
	src, files, err := openBackupImages(backupDir)
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
	platform, err := resolveCodename(platformReq, detected)
	if err != nil {
		return cacheResult{}, err
	}
	firmware := firmwareLabel(property(text, "ro.build.fingerprint"), property(text, "ro.build.version.incremental"))
	if firmwareReq != "" {
		if firmware != "unknown" && firmware != firmwareReq {
			return cacheResult{}, fmt.Errorf("backup firmware %s differs from requested %s", firmware, firmwareReq)
		}
		firmware = firmwareReq
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
	if !refresh {
		existing, err := findCache(cacheDir, platform, sourceSHA, "")
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
	return publishCache(cacheDir, meta, collected.items, refresh, meter)
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

var OpenBackupImages = openBackupImages
var CloseImages = closeImages
var BackupIdentity = backupIdentity
var ExtractBackup = extractBackup
var CacheFiles = cacheFiles
