package asp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"time"
)

type ReleaseResource struct {
	Path      string // e.g. assets/channel-map, platform-owned opaque data
	InstallTo string // e.g. lib/rootfs/factory/channel-map
	Bytes     []byte
}
type V4Package struct {
	Platform  string
	Version   string
	Runtime   ReleaseRuntime
	Installer []byte
	License   []byte
	Launcher  []byte
	Child     []byte
	Data      []ReleaseResource
}

// PackV4 builds a release from explicit public/package-owned bytes only.
// Factory caches and OTA sources are never inspected or embedded.
func PackV4(req V4Package) (VerifiedRelease, error) {
	if !wirePlatformPattern.MatchString(req.Platform) || !releaseVersionPattern.MatchString(req.Version) {
		return VerifiedRelease{}, errors.New("invalid platform/version for release package")
	}
	if err := validateRuntime(req.Runtime); err != nil {
		return VerifiedRelease{}, err
	}
	files := map[string][]byte{
		"asp-installer":       req.Installer,
		"LICENSE":             req.License,
		"assets/external-asp": req.Launcher,
		"assets/asp1-child":   req.Child,
	}
	manifest := ReleaseManifest{
		Schema: ReleaseSchemaV4, Platform: req.Platform, Version: req.Version,
		CacheSchema: cacheSchema, Arch: "arm32", Runtime: req.Runtime,
	}
	roleFor := map[string]string{
		"asp-installer": "installer", "LICENSE": "license",
		"assets/external-asp": "launcher", "assets/asp1-child": "child",
	}
	installFor := map[string]string{
		"assets/external-asp": "bin/external-asp",
		"assets/asp1-child":   "lib/rootfs/factory/asp1-child",
	}
	for _, r := range req.Data {
		if _, exists := files[r.Path]; exists {
			return VerifiedRelease{}, fmt.Errorf("duplicate release resource %q", r.Path)
		}
		files[r.Path] = r.Bytes
		roleFor[r.Path] = "data"
		installFor[r.Path] = r.InstallTo
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		b := files[name]
		h := sha256.Sum256(b)
		manifest.Files = append(manifest.Files, ReleaseFile{
			Path: name, Role: roleFor[name], InstallTo: installFor[name],
			Size: int64(len(b)), SHA256: hex.EncodeToString(h[:]),
		})
	}
	if err := validateInventory(manifest.Files); err != nil {
		return VerifiedRelease{}, err
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		return VerifiedRelease{}, err
	}
	if len(rawManifest) > maxReleaseManifest {
		return VerifiedRelease{}, errors.New("manifest too large")
	}
	files["assets/manifest.json"] = rawManifest
	names = append(names, "assets/manifest.json")
	sort.Strings(names)
	root := "frankenasp-" + req.Platform
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	gz.Header.ModTime = time.Unix(0, 0)
	gz.Header.OS = 255
	tw := tar.NewWriter(gz)
	directories := map[string]bool{root: true}
	for _, name := range names {
		for p := path.Dir(root + "/" + name); p != "." && p != ""; p = path.Dir(p) {
			directories[p] = true
			if p == root {
				break
			}
		}
	}
	dirs := make([]string, 0, len(directories))
	for d := range directories {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if err = tw.WriteHeader(&tar.Header{
			Name: d + "/", Mode: 0700, Typeflag: tar.TypeDir, Format: tar.FormatUSTAR,
		}); err != nil {
			return VerifiedRelease{}, err
		}
	}
	for _, name := range names {
		b := files[name]
		mode := int64(0600)
		if name == "asp-installer" || name == "assets/external-asp" || name == "assets/asp1-child" {
			mode = 0500
		}
		if err = tw.WriteHeader(&tar.Header{
			Name: root + "/" + name, Mode: mode, Size: int64(len(b)),
			Typeflag: tar.TypeReg, Format: tar.FormatUSTAR,
		}); err != nil {
			return VerifiedRelease{}, err
		}
		if _, err = tw.Write(b); err != nil {
			return VerifiedRelease{}, err
		}
	}
	if err = tw.Close(); err != nil {
		return VerifiedRelease{}, err
	}
	if err = gz.Close(); err != nil {
		return VerifiedRelease{}, err
	}
	filename := fmt.Sprintf("frankenasp-v%s-%s-arm32.tar.gz", req.Version, req.Platform)
	return ParseReleaseBytes(compressed.Bytes(), filename, req.Platform)
}

func SaveRelease(dir string, release VerifiedRelease) (string, error) {
	if release.Schema != ReleaseSchemaV4 || len(release.Original) == 0 {
		return "", errors.New("not a verified V4 archive")
	}
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		return "", errors.New("release output directory must exist")
	}
	filepath := path.Join(dir, release.Name)
	f, err := os.OpenFile(filepath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, e := f.Write(release.Original)
	if e == nil {
		e = f.Sync()
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		_ = os.Remove(filepath)
		return "", e
	}
	return filepath, nil
}
