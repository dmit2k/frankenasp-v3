package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

type releaseArchive = asp.VerifiedRelease

func loadRelease(path, platform string) (releaseArchive, error) {
	return asp.LoadRelease(path, platform)
}

type bundleManifest struct {
	Schema            string            `json:"schema"`
	Platform          string            `json:"platform"`
	Firmware          string            `json:"firmware"`
	FireOS            string            `json:"fire_os_version"`
	CacheDir          string            `json:"cache_dir"`
	CacheSourceSHA256 string            `json:"cache_source_sha256"`
	Release           string            `json:"release"`
	ReleaseVersion    string            `json:"release_version"`
	ReleaseSHA256     string            `json:"release_sha256"`
	FilesSHA256       map[string]string `json:"files_sha256"`
}

func archiveMember(w *tar.Writer, name string, b []byte) error {
	if !asp.SafePath(name) {
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
	if rel.CacheSchema != asp.CacheSchema {
		return fmt.Errorf("--asp-release archive supports cache schema %q; this extractor requires %s (rebuild the FrankenASP release with the updated asp-installer)", rel.CacheSchema, asp.CacheSchema)
	}
	cache, err := selectDesktopCache(o)
	if err != nil {
		return err
	}
	if err := asp.ValidateHelperRuntimeForPaths(rel.Files["assets/asp1-child"], cache.Items, rel.Runtime.LibrarySearch); err != nil {
		return err
	}
	members, err := asp.CacheFiles(cache)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(rel.Original)
	mf := bundleManifest{Schema: "FRANKENASP-EXTRACTOR-BUNDLE-V2", Platform: o.platform, Firmware: cache.Metadata.Firmware, FireOS: asp.FireOSForCache(cache.Metadata), CacheDir: filepath.Base(cache.Path), CacheSourceSHA256: cache.Metadata.SourceSHA256, Release: rel.Name, ReleaseVersion: rel.Version, ReleaseSHA256: hex.EncodeToString(digest[:]), FilesSHA256: map[string]string{}}
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
