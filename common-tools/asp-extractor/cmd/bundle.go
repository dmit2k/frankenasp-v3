package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"strings"
	"time"
)

type releaseArchive struct {
	Name, Version, Platform string
	Original                []byte
	Files                   map[string][]byte
}

var releaseNamePattern = regexp.MustCompile(`^frankenasp-v([0-9][A-Za-z0-9._-]*)-(athena|rook)-arm32\.tar\.gz$`)

func loadRelease(path, platform string) (releaseArchive, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return releaseArchive{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return releaseArchive{}, errors.New("release must be a regular .tar.gz <=64MiB")
	}
	basename := filepath.Base(path)
	matched := releaseNamePattern.FindStringSubmatch(basename)
	if len(matched) != 3 || matched[2] != platform {
		return releaseArchive{}, errors.New("release filename does not match versioned target platform")
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return releaseArchive{}, err
	}
	z, err := gzip.NewReader(bytes.NewReader(original))
	if err != nil {
		return releaseArchive{}, fmt.Errorf("release gzip: %w", err)
	}
	defer z.Close()
	r := tar.NewReader(z)
	prefix := "frankenasp-" + platform + "/"
	allowed := map[string]bool{"asp-installer": true, "LICENSE": true, "assets/manifest.json": true, "assets/asp1-child": true, "assets/external-asp": true}
	if platform == "athena" {
		allowed["assets/channel-map"] = true
	}
	files := map[string][]byte{}
	total := int64(0)
	for {
		h, e := r.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return releaseArchive{}, e
		}
		if h.Typeflag == tar.TypeDir {
			name := strings.TrimSuffix(h.Name, "/")
			if name == "frankenasp-"+platform || name == "frankenasp-"+platform+"/assets" {
				continue
			}
			return releaseArchive{}, fmt.Errorf("unexpected release directory %q", h.Name)
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return releaseArchive{}, fmt.Errorf("unsupported release entry %q", h.Name)
		}
		if !strings.HasPrefix(h.Name, prefix) {
			return releaseArchive{}, fmt.Errorf("wrong release root %q", h.Name)
		}
		name := strings.TrimPrefix(h.Name, prefix)
		if !allowed[name] || files[name] != nil {
			return releaseArchive{}, fmt.Errorf("unexpected or repeated release entry %q", name)
		}
		if h.Size <= 0 || h.Size > 32<<20 {
			return releaseArchive{}, fmt.Errorf("invalid release file size %q", name)
		}
		total += h.Size
		if total > 64<<20 {
			return releaseArchive{}, errors.New("oversized release contents")
		}
		blob, e := io.ReadAll(io.LimitReader(r, h.Size+1))
		if e != nil || int64(len(blob)) != h.Size {
			return releaseArchive{}, fmt.Errorf("truncated release member %s", name)
		}
		files[name] = blob
	}
	if len(files) != len(allowed) {
		return releaseArchive{}, errors.New("release incomplete")
	}
	var manifest struct {
		Schema   string `json:"schema"`
		Platform string `json:"platform"`
	}
	if err = json.Unmarshal(files["assets/manifest.json"], &manifest); err != nil {
		return releaseArchive{}, err
	}
	if manifest.Schema != "ASP1-ASSETS-V3" || manifest.Platform != platform {
		return releaseArchive{}, errors.New("release platform or schema mismatch")
	}
	if _, err = elfNeeded(files["assets/asp1-child"], false, "/system/bin/linker"); err != nil {
		return releaseArchive{}, fmt.Errorf("invalid helper ELF: %w", err)
	}
	if _, err = elfNeeded(files["assets/external-asp"], false, ""); err != nil {
		return releaseArchive{}, fmt.Errorf("invalid launcher ELF: %w", err)
	}
	if platform == "athena" {
		if err = validateChannelMap(files["assets/channel-map"]); err != nil {
			return releaseArchive{}, err
		}
	}
	return releaseArchive{Name: basename, Version: matched[1], Platform: platform, Original: original, Files: files}, nil
}

type bundleManifest struct {
	Schema            string            `json:"schema"`
	Platform          string            `json:"platform"`
	Firmware          string            `json:"firmware"`
	FireOS            string            `json:"fire_os_version"`
	CacheDir          string            `json:"cache_dir"`
	CacheSourceSHA256 string            `json:"cache_source_sha256"`
	CacheHelperSHA256 string            `json:"cache_helper_sha256"`
	Release           string            `json:"release"`
	ReleaseVersion    string            `json:"release_version"`
	ReleaseSHA256     string            `json:"release_sha256"`
	FilesSHA256       map[string]string `json:"files_sha256"`
}

func archiveMember(w *tar.Writer, name string, b []byte) error {
	if !safePath(name) {
		return fmt.Errorf("unsafe bundle entry %q", name)
	}
	h := &tar.Header{Name: name, Mode: 0600, Size: int64(len(b)), ModTime: time.Unix(0, 0).UTC(), Typeflag: tar.TypeReg}
	if err := w.WriteHeader(h); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}
func createBundle(o options) error {
	rel, err := loadRelease(o.release, o.platform)
	if err != nil {
		return err
	}
	cache, err := selectDesktopCache(o, hashText(rel.Files["assets/asp1-child"]))
	if err != nil {
		return err
	}
	members, err := cacheFiles(cache)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(rel.Original)
	mf := bundleManifest{Schema: "FRANKENASP-EXTRACTOR-BUNDLE-V1", Platform: o.platform, Firmware: cache.Metadata.Firmware, FireOS: fireOSForCache(cache.Metadata), CacheDir: filepath.Base(cache.Path), CacheSourceSHA256: cache.Metadata.SourceSHA256, CacheHelperSHA256: cache.Metadata.HelperSHA256, Release: rel.Name, ReleaseVersion: rel.Version, ReleaseSHA256: hex.EncodeToString(digest[:]), FilesSHA256: map[string]string{}}
	prefix := "asp-installer-cache/" + filepath.Base(cache.Path) + "/"
	archiveFiles := map[string][]byte{"release/" + rel.Name: rel.Original}
	for name, data := range members {
		archiveFiles[prefix+name] = data
	}
	for name, data := range archiveFiles {
		h := sha256.Sum256(data)
		mf.FilesSHA256[name] = hex.EncodeToString(h[:])
	}
	raw, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	archiveFiles["manifest.json"] = append(raw, '\n')
	if filepath.Ext(o.output) != ".gz" || !strings.HasSuffix(o.output, ".tar.gz") {
		return errors.New("--output must end in .tar.gz")
	}
	dir := filepath.Dir(o.output)
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".asp-extractor-bundle-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	gz := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(archiveFiles))
	for n := range archiveFiles {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if err = archiveMember(tw, name, archiveFiles[name]); err != nil {
			break
		}
	}
	if e := tw.Close(); err == nil {
		err = e
	}
	if e := gz.Close(); err == nil {
		err = e
	}
	if e := tmp.Sync(); err == nil {
		err = e
	}
	if e := tmp.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	// Hardlink enforces no-clobber even if another process creates output meanwhile.
	if err = os.Link(tmpPath, o.output); err != nil {
		return err
	}
	fmt.Printf("BUNDLE %s: %s %s, release %s, %d cached files\n", o.output, o.platform, mf.Firmware, rel.Version, cache.Metadata.Files)
	return nil
}
