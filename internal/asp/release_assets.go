package asp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dmit2k/frankenasp-v3/platforms"
	"golang.org/x/sys/unix"
)

// AssetPlan is a verified, single-read snapshot of runtime assets. It is
// consumed by rootfs preparation, comparison and disk-space estimates without
// reopening paths supplied by a release.
type AssetPlan struct {
	Schema    string
	Platform  string
	Runtime   ReleaseRuntime
	Files     map[string][]byte
	Installed []ReleaseFile
}

// Open the asset directory by pinned directory handles, rejecting symlinks
// in *every* component, not only the final file.
func pinnedDirectory(absolute string) (int, error) {
	if !strings.HasPrefix(absolute, "/") {
		return -1, errors.New("assets directory must be absolute")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	for _, p := range strings.Split(strings.TrimPrefix(absolute, "/"), "/") {
		if p == "" {
			continue
		}
		if p == "." || p == ".." {
			unix.Close(fd)
			return -1, errors.New("unsafe asset directory component")
		}
		next, e := unix.Openat(fd, p, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return -1, fmt.Errorf("asset directory component %s: %w", p, e)
		}
		fd = next
	}
	// Do not impose ownership policy on /data or other Android parent
	// directories. Protect only the selected installer assets root.
	var trusted unix.Stat_t
	if err = unix.Fstat(fd, &trusted); err != nil {
		unix.Close(fd)
		return -1, err
	}
	if trusted.Mode&unix.S_IFMT != unix.S_IFDIR || trusted.Mode&0022 != 0 || (os.Geteuid() == 0 && trusted.Uid != 0) {
		unix.Close(fd)
		return -1, errors.New("untrusted installer assets directory")
	}
	return fd, nil
}

func pinnedAssetRead(dirfd int, name string, max int64) ([]byte, error) {
	if !validReleasePath(name, 160, 8) {
		return nil, fmt.Errorf("unsafe release resource path %q", name)
	}
	current, e := unix.Dup(dirfd)
	if e != nil {
		return nil, e
	}
	// Close the final current descriptor rather than the initial duplicated
	// descriptor, which is closed as traversal advances.
	defer func() { _ = unix.Close(current) }()
	parts := strings.Split(name, "/")
	for _, p := range parts[:len(parts)-1] {
		fd, err := unix.Openat(current, p, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, fmt.Errorf("release asset parent %s: %w", p, err)
		}
		var ds unix.Stat_t
		if err := unix.Fstat(fd, &ds); err != nil || ds.Mode&0022 != 0 || (os.Geteuid() == 0 && ds.Uid != 0) {
			unix.Close(fd)
			return nil, errors.New("unsafe release asset parent permissions")
		}
		unix.Close(current)
		current = fd
	}
	fd, e := unix.Openat(current, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	var stat unix.Stat_t
	if e = unix.Fstat(fd, &stat); e != nil {
		return nil, e
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size <= 0 || stat.Size > max || stat.Mode&07000 != 0 || stat.Mode&0022 != 0 || (os.Geteuid() == 0 && stat.Uid != 0) {
		return nil, fmt.Errorf("untrusted release asset %q", name)
	}
	b, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil {
		return nil, e
	}
	if len(b) == 0 || int64(len(b)) != stat.Size {
		return nil, fmt.Errorf("release asset %q changed during read", name)
	}
	return b, nil
}

// LoadAssets verifies all runtime assets without reading installer/license.
// An archive validator must verify those two public packaging files before
// distribution; the embedded device consumes only assets/*.
func LoadAssets(assetDir string) (AssetPlan, error) {
	dir, err := pinnedDirectory(assetDir)
	if err != nil {
		return AssetPlan{}, err
	}
	defer unix.Close(dir)
	manifestRaw, err := pinnedAssetRead(dir, "manifest.json", maxReleaseManifest)
	if err != nil {
		return AssetPlan{}, fmt.Errorf("read release manifest: %w", err)
	}
	m, err := parseReleaseManifest(manifestRaw)
	if err != nil {
		return AssetPlan{}, err
	}
	plan := AssetPlan{Schema: m.Schema, Platform: m.Platform, Files: map[string][]byte{"assets/manifest.json": manifestRaw}}
	normalized, err := normalizeRuntimeRelease(m)
	if err != nil {
		return plan, err
	}
	plan.Runtime = normalized.runtime
	source := normalized.assets
	for _, item := range source {
		if !strings.HasPrefix(item.Path, "assets/") {
			return plan, errors.New("invalid runtime source path")
		}
		b, e := pinnedAssetRead(dir, strings.TrimPrefix(item.Path, "assets/"), maxReleaseFile)
		if e != nil {
			return plan, fmt.Errorf("read release runtime %s: %w", item.Path, e)
		}
		if m.Schema == ReleaseSchemaV4 {
			if int64(len(b)) != item.Size {
				return plan, fmt.Errorf("release runtime %s size mismatch", item.Path)
			}
			digest := sha256.Sum256(b)
			if hex.EncodeToString(digest[:]) != item.SHA256 {
				return plan, fmt.Errorf("release runtime %s sha256 mismatch", item.Path)
			}
		} else if normalized.legacyChannelMap && item.Path == "assets/channel-map" {
			if e = platforms.ValidateChannelMap(b); e != nil {
				return plan, e
			}
		}
		plan.Files[item.Path] = b
		plan.Installed = append(plan.Installed, item)
	}
	if _, e := elfNeeded(plan.Files["assets/asp1-child"], false, "/system/bin/linker"); e != nil {
		return plan, fmt.Errorf("invalid helper ELF: %w", e)
	}
	if _, e := elfNeeded(plan.Files["assets/external-asp"], false, ""); e != nil {
		return plan, fmt.Errorf("invalid launcher ELF: %w", e)
	}
	return plan, nil
}

func (p AssetPlan) AllowsVendor(mode string) bool {
	for _, candidate := range p.Runtime.VendorModes {
		if candidate == mode {
			return true
		}
	}
	return false
}
