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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const cacheSchema = "ASP1-EXTRACTED-CACHE-V1"
const cacheDirName = "asp-installer-cache"
const cacheFileLimit = 512
const cacheTotalLimit = 128 << 20

type cacheMetadata struct {
	Schema          string `json:"schema"`
	Platform        string `json:"platform"`
	Firmware        string `json:"firmware"`
	SourceSHA256    string `json:"source_sha256"`
	HelperSHA256    string `json:"helper_sha256"`
	URLSHA256       string `json:"url_sha256,omitempty"`
	OTAFormat       string `json:"ota_format"`
	Files           int    `json:"files"`
	Bytes           int    `json:"bytes"`
	DependencyEdges int    `json:"dependency_edges"`
	Created         string `json:"created"`
}
type cacheResult struct {
	Items    map[string][]byte
	Metadata cacheMetadata
	Path     string
	Hit      bool
}

var releasePattern = regexp.MustCompile(`(?i)(PS[0-9]{4,})[._/-]([0-9]{4,})`)
var rookBuildPattern = regexp.MustCompile(`_user_([0-9]{6,})`)
var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var firmwarePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,48}$`)

func hashText(data []byte) string   { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func hashString(data string) string { return hashText([]byte(data)) }
func hashOTA(ctx context.Context, path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return "", e
	}
	if !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > 2<<30 {
		return "", errors.New("OTA must be a regular nonempty file <=2GiB")
	}
	h := sha256.New()
	b := make([]byte, 1<<20)
	for {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		n, er := f.Read(b)
		if n > 0 {
			_, _ = h.Write(b[:n])
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			return "", er
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func firmwareLabel(postBuild, incremental string) string {
	if m := releasePattern.FindStringSubmatch(postBuild); len(m) == 3 {
		return strings.ToUpper(m[1]) + "-" + m[2]
	}
	if m := rookBuildPattern.FindStringSubmatch(postBuild); len(m) == 2 {
		return m[1]
	}
	var b strings.Builder
	for _, r := range incremental {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		if b.Len() == 36 {
			break
		}
	}
	if b.Len() > 0 {
		return "build-" + b.String()
	}
	return "unknown"
}

// Keep directory names human-readable; the full cryptographic identities live
// only in manifest.json and SHA256SUMS.
func cacheName(meta cacheMetadata) string {
	return meta.Platform + "-" + meta.Firmware
}
func cacheMatchesName(folder string, meta cacheMetadata) bool {
	base := cacheName(meta)
	if folder == base {
		return true
	}
	prefix := base + "-variant-"
	if !strings.HasPrefix(folder, prefix) {
		return false
	}
	num, err := strconv.Atoi(strings.TrimPrefix(folder, prefix))
	return err == nil && num >= 2 && num <= 999 && strconv.Itoa(num) == strings.TrimPrefix(folder, prefix)
}

// Distinct contents or helper builds of the same firmware receive numbered
// variants instead of opaque hash suffixes. Existing variants are never lost.
func selectCacheDestination(base string, meta cacheMetadata) (string, bool, error) {
	for variant := 1; variant <= 999; variant++ {
		name := cacheName(meta)
		if variant > 1 {
			name += fmt.Sprintf("-variant-%d", variant)
		}
		candidate := filepath.Join(base, name)
		st, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			return candidate, false, nil
		}
		if err != nil {
			return "", false, err
		}
		if !st.IsDir() {
			return "", false, fmt.Errorf("unsafe cache destination: %s", candidate)
		}
		raw, err := os.ReadFile(filepath.Join(candidate, "manifest.json"))
		if err != nil {
			return "", false, fmt.Errorf("existing firmware cache missing manifest: %w", err)
		}
		var prior cacheMetadata
		if err = json.Unmarshal(raw, &prior); err != nil {
			return "", false, fmt.Errorf("existing firmware cache corrupt: %w", err)
		}
		if prior.Schema != cacheSchema || prior.Platform != meta.Platform || prior.Firmware != meta.Firmware || !cacheMatchesName(name, prior) {
			return "", false, fmt.Errorf("existing firmware cache identity mismatch: %s", candidate)
		}
		if prior.SourceSHA256 == meta.SourceSHA256 && prior.HelperSHA256 == meta.HelperSHA256 {
			return candidate, true, nil
		}
	}
	return "", false, errors.New("too many firmware cache variants")
}
func requireCacheDir(base string) error {
	st, e := os.Lstat(base)
	if os.IsNotExist(e) {
		if e = os.Mkdir(base, 0700); e != nil {
			return e
		}
		st, e = os.Lstat(base)
	}
	if e != nil {
		return e
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode().Perm() != 0700 || sys.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("unsafe cache directory: %s (owner must match and mode must be 0700)", base)
	}
	return nil
}
func checkCacheNode(path string, regular bool) error {
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || sys.Uid != uint32(os.Geteuid()) || st.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("unsafe cached file ownership or permissions: %s", path)
	}
	if regular {
		if !st.Mode().IsRegular() || sys.Nlink != 1 {
			return fmt.Errorf("unsafe cached regular file: %s", path)
		}
	} else if !st.IsDir() {
		return fmt.Errorf("unsafe cached directory: %s", path)
	}
	return nil
}
func cacheLock(ctx context.Context, base string) (func(), error) {
	if e := requireCacheDir(base); e != nil {
		return nil, e
	}
	lockFile := filepath.Join(base, ".cache.lock")
	fd, e := unix.Open(lockFile, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	if e = checkCacheNode(lockFile, true); e != nil {
		unix.Close(fd)
		return nil, e
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			break
		}
		if e != unix.EWOULDBLOCK && e != unix.EAGAIN {
			_ = unix.Close(fd)
			return nil, e
		}
		select {
		case <-ctx.Done():
			_ = unix.Close(fd)
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = unix.Close(fd) }, nil
}
func cacheKeyLines(items map[string][]byte) ([]string, int, error) {
	if len(items) == 0 || len(items) > cacheFileLimit {
		return nil, 0, errors.New("empty or oversized extracted cache inventory")
	}
	names := make([]string, 0, len(items))
	var total int
	for name, data := range items {
		if !safePath(name) || !strings.HasPrefix(name, "system/") && !strings.HasPrefix(name, "vendor/") {
			return nil, 0, fmt.Errorf("unsafe cache member %q", name)
		}
		if len(data) > 64<<20 {
			return nil, 0, fmt.Errorf("oversized cache member %s", name)
		}
		total += len(data)
		if total > cacheTotalLimit {
			return nil, 0, errors.New("extracted cache exceeds 128MiB")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, hashText(items[name])+"  "+name)
	}
	return lines, total, nil
}
func readCache(path string) (cacheResult, error) {
	if e := checkCacheNode(path, false); e != nil {
		return cacheResult{}, e
	}
	metaPath := filepath.Join(path, "manifest.json")
	sumsPath := filepath.Join(path, "SHA256SUMS")
	filesDir := filepath.Join(path, "files")
	for _, p := range []string{metaPath, sumsPath} {
		if e := checkCacheNode(p, true); e != nil {
			return cacheResult{}, e
		}
	}
	if e := checkCacheNode(filesDir, false); e != nil {
		return cacheResult{}, e
	}
	data, e := os.ReadFile(metaPath)
	if e != nil {
		return cacheResult{}, e
	}
	if len(data) > 16<<10 {
		return cacheResult{}, errors.New("cache manifest oversized")
	}
	var meta cacheMetadata
	if e = json.Unmarshal(data, &meta); e != nil {
		return cacheResult{}, e
	}
	if meta.Schema != cacheSchema || (meta.Platform != "athena" && meta.Platform != "rook") || !firmwarePattern.MatchString(meta.Firmware) || !hexDigest.MatchString(meta.SourceSHA256) || !hexDigest.MatchString(meta.HelperSHA256) || (meta.URLSHA256 != "" && !hexDigest.MatchString(meta.URLSHA256)) || meta.Files < 1 || meta.Files > cacheFileLimit || meta.Bytes < 1 || meta.Bytes > cacheTotalLimit || meta.DependencyEdges < 0 || meta.DependencyEdges > 16384 {
		return cacheResult{}, errors.New("invalid cache manifest")
	}
	if !cacheMatchesName(filepath.Base(path), meta) {
		return cacheResult{}, errors.New("cache directory identity does not match manifest")
	}
	sumData, e := os.ReadFile(sumsPath)
	if e != nil {
		return cacheResult{}, e
	}
	if len(sumData) > cacheFileLimit*180 {
		return cacheResult{}, errors.New("cache checksum inventory oversized")
	}
	lines := strings.Split(strings.TrimSuffix(string(sumData), "\n"), "\n")
	if len(lines) != meta.Files {
		return cacheResult{}, errors.New("cache checksum inventory incomplete")
	}
	files := map[string][]byte{}
	for _, line := range lines {
		if len(line) < 67 || line[64:66] != "  " || !hexDigest.MatchString(line[:64]) {
			return cacheResult{}, errors.New("malformed cache SHA256SUMS")
		}
		name := line[66:]
		if !safePath(name) || !strings.HasPrefix(name, "system/") && !strings.HasPrefix(name, "vendor/") {
			return cacheResult{}, errors.New("unsafe cache checksum path")
		}
		if _, ok := files[name]; ok {
			return cacheResult{}, errors.New("duplicate cache checksum path")
		}
		file := filepath.Join(filesDir, filepath.FromSlash(name))
		if e = checkCacheNode(file, true); e != nil {
			return cacheResult{}, e
		}
		blob, er := os.ReadFile(file)
		if er != nil {
			return cacheResult{}, er
		}
		if hashText(blob) != line[:64] {
			return cacheResult{}, fmt.Errorf("cache file hash mismatch: %s", name)
		}
		files[name] = blob
	}
	expectedLines, total, e := cacheKeyLines(files)
	if e != nil {
		return cacheResult{}, e
	}
	if total != meta.Bytes || len(files) != meta.Files || strings.Join(expectedLines, "\n")+"\n" != string(sumData) {
		return cacheResult{}, errors.New("cache inventory mismatch")
	}
	// Walk without following links: exclude unexpected artifacts and directory entry attacks.
	seen := 0
	e = filepath.Walk(path, func(name string, st os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if st.IsDir() {
			if er := checkCacheNode(name, false); er != nil {
				return er
			}
			return nil
		}
		if er := checkCacheNode(name, true); er != nil {
			return er
		}
		if name == metaPath || name == sumsPath {
			return nil
		}
		relative, er := filepath.Rel(filesDir, name)
		if er != nil {
			return er
		}
		if _, ok := files[filepath.ToSlash(relative)]; !ok {
			return fmt.Errorf("extra cached artifact: %s", name)
		}
		seen++
		return nil
	})
	if e != nil {
		return cacheResult{}, e
	}
	if seen != meta.Files {
		return cacheResult{}, errors.New("cached artifact count mismatch")
	}
	return cacheResult{Items: files, Metadata: meta, Path: path, Hit: true}, nil
}
func findCache(base, platform, helperHash, sourceHash, urlHash string) (cacheResult, error) {
	entries, e := os.ReadDir(base)
	if e != nil {
		return cacheResult{}, e
	}
	var candidates []cacheResult
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), platform+"-") {
			continue
		}
		path := filepath.Join(base, entry.Name())
		raw, er := os.ReadFile(filepath.Join(path, "manifest.json"))
		if er != nil {
			continue
		}
		var meta cacheMetadata
		if json.Unmarshal(raw, &meta) != nil {
			continue
		}
		if meta.Platform != platform || meta.HelperSHA256 != helperHash {
			continue
		}
		if sourceHash != "" && meta.SourceSHA256 != sourceHash {
			continue
		}
		if urlHash != "" && meta.URLSHA256 != urlHash {
			continue
		}
		found, er := readCache(path)
		if er != nil {
			return cacheResult{}, fmt.Errorf("cached extraction %s is invalid: %w; use --refresh-cache", entry.Name(), er)
		}
		candidates = append(candidates, found)
	}
	if len(candidates) == 0 {
		return cacheResult{}, os.ErrNotExist
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Metadata.Created > candidates[j].Metadata.Created })
	return candidates[0], nil
}
func publishCache(base string, meta cacheMetadata, items map[string][]byte, refresh bool, m *progressMeter) (cacheResult, error) {
	lines, total, e := cacheKeyLines(items)
	if e != nil {
		return cacheResult{}, e
	}
	meta.Files = len(items)
	meta.Bytes = total
	meta.Schema = cacheSchema
	meta.Created = time.Now().UTC().Format(time.RFC3339Nano)
	destination, exists, e := selectCacheDestination(base, meta)
	if e != nil {
		return cacheResult{}, e
	}
	if exists && !refresh {
		return readCache(destination)
	}
	// Extracted size plus per-file metadata, with a safety reserve; no full images written.
	device, e := diskAt(base)
	if e != nil {
		return cacheResult{}, e
	}
	required, e := sumSpace(uint64(total)+(uint64(len(items))+16)*roundAlloc(4096, device.BlockSize), 8<<20)
	if e != nil {
		return cacheResult{}, e
	}
	if e = requireSpace(diskNeed{Role: "extracted OTA cache", Path: base, Rootfs: required, Reserve: spaceReserve}, m); e != nil {
		return cacheResult{}, e
	}
	stage, e := os.MkdirTemp(base, ".cache-building-")
	if e != nil {
		return cacheResult{}, e
	}
	defer os.RemoveAll(stage)
	if e = os.Chmod(stage, 0700); e != nil {
		return cacheResult{}, e
	}
	for name, blob := range items {
		p := filepath.Join(stage, "files", filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return cacheResult{}, e
		}
		if e = writeCacheFile(p, blob); e != nil {
			return cacheResult{}, e
		}
	}
	manifest, e := json.MarshalIndent(meta, "", "  ")
	if e != nil {
		return cacheResult{}, e
	}
	if e = writeCacheFile(filepath.Join(stage, "manifest.json"), append(manifest, '\n')); e != nil {
		return cacheResult{}, e
	}
	if e = writeCacheFile(filepath.Join(stage, "SHA256SUMS"), []byte(strings.Join(lines, "\n")+"\n")); e != nil {
		return cacheResult{}, e
	}
	e = filepath.Walk(stage, func(path string, st os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if st.IsDir() {
			return fsyncDir(path)
		}
		return nil
	})
	if e != nil {
		return cacheResult{}, e
	}
	if e = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE); errors.Is(e, unix.EEXIST) && refresh {
		if e = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, destination, unix.RENAME_EXCHANGE); e == nil {
			// Previous cache is now under the stage name; current command holds the cache lock.
			if er := os.RemoveAll(stage); er != nil {
				return cacheResult{}, er
			}
		}
	}
	if e != nil {
		return cacheResult{}, e
	}
	if e = fsyncDir(base); e != nil {
		return cacheResult{}, e
	}
	result, e := readCache(destination)
	if e != nil {
		return cacheResult{}, fmt.Errorf("published cache failed validation: %w", e)
	}
	result.Hit = false
	return result, nil
}
func describeCache(m *progressMeter, result cacheResult) {
	m.Note(fmt.Sprintf("Cache: %s (%s, %d artifacts, %d bytes)", map[bool]string{true: "verified hit", false: "created"}[result.Hit], result.Metadata.Firmware, result.Metadata.Files, result.Metadata.Bytes))
	m.Note("Cache path: " + result.Path)
}

func writeCacheFile(path string, data []byte) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	n, e := f.Write(data)
	if e == nil && n != len(data) {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}
