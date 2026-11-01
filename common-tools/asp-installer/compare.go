package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type difference struct {
	Path   string `json:"path"`
	Change string `json:"change"`
}
type compareResult struct {
	Matching bool         `json:"matching"`
	Compared int          `json:"compared"`
	Changed  []difference `json:"changed"`
	Mode     string       `json:"mode,omitempty"`
	Warning  string       `json:"warning,omitempty"`
}

func (r *compareResult) check(path string, expected []byte, root string) {
	r.Compared++
	p := filepath.Join(root, filepath.FromSlash(path))
	st, e := os.Lstat(p)
	if errors.Is(e, os.ErrNotExist) {
		r.Changed = append(r.Changed, difference{path, "missing"})
		return
	}
	if e != nil || !st.Mode().IsRegular() {
		r.Changed = append(r.Changed, difference{path, "unreadable-or-not-regular"})
		return
	}
	if st.Size() != int64(len(expected)) {
		r.Changed = append(r.Changed, difference{path, "different-size"})
		return
	}
	raw, e := os.ReadFile(p)
	if e != nil || !bytes.Equal(raw, expected) {
		r.Changed = append(r.Changed, difference{path, "different-content"})
	}
}
func (r *compareResult) finish() {
	r.Matching = len(r.Changed) == 0
	sort.Slice(r.Changed, func(i, j int) bool { return r.Changed[i].Path < r.Changed[j].Path })
}
func compareVendor(items map[string][]byte, root string) compareResult {
	result := compareResult{Changed: []difference{}}
	for p, raw := range items {
		if strings.HasPrefix(p, "vendor/") {
			result.check(strings.TrimPrefix(p, "vendor/"), raw, root)
		}
	}
	result.finish()
	return result
}
func vendorReadOnly() error {
	var stat unix.Statfs_t
	if e := unix.Statfs("/vendor", &stat); e != nil {
		return e
	}
	if stat.Flags&unix.ST_RDONLY == 0 {
		return errors.New("live /vendor is not a read-only filesystem")
	}
	return nil
}
func chooseVendorMode(platform, requested string, items map[string][]byte) (mode string, inspection compareResult, e error) {
	if requested == "" {
		requested = "bundled"
	}
	if platform == "rook" {
		if requested != "bundled" && requested != "auto" {
			return "", inspection, errors.New("Rook requires bundled vendor; live-vendor mode is unsupported")
		}
		return "bundled", inspection, nil
	}
	if requested != "bundled" && requested != "live" && requested != "auto" {
		return "", inspection, errors.New("--vendor-mode must be bundled, live or auto")
	}
	if requested == "bundled" {
		return "bundled", inspection, nil
	}
	inspection = compareVendor(items, "/vendor")
	valid := inspection.Matching && vendorReadOnly() == nil && inspection.Compared > 0
	if valid {
		return "live", inspection, nil
	}
	if requested == "live" {
		return "", inspection, fmt.Errorf("live vendor differs from OTA, is unavailable, or not mounted read-only")
	}
	return "bundled", inspection, nil
}
func aliasTargets() map[string]string {
	aliases := map[string]string{"self": "/.realproc/self", "sys": "/.realproc/sys", "cpuinfo": "/.realproc/cpuinfo", "device-tree": "/sys/firmware/devicetree/base"}
	for i := 0; i < 4; i++ {
		aliases[fmt.Sprintf("idme/miccal.%d", i)] = fmt.Sprintf("/proc/device-tree/idme/miccal.%d/value", i)
	}
	return aliases
}
func compareInstalled(platform, assets string, items map[string][]byte, mode string, installed string) (compareResult, error) {
	result := compareResult{Changed: []difference{}, Mode: mode}
	if st, e := os.Lstat(installed); e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("external-asp directory not installed or unsafe")
	}
	expected := map[string][]byte{}
	for p, raw := range items {
		if mode == "live" && strings.HasPrefix(p, "vendor/") {
			continue
		}
		expected["lib/rootfs/"+p] = raw
	}
	for _, pair := range [][2]string{{"external-asp", "bin/external-asp"}, {"asp1-child", "lib/rootfs/factory/asp1-child"}} {
		raw, e := os.ReadFile(filepath.Join(assets, pair[0]))
		if e != nil {
			return result, e
		}
		expected[pair[1]] = raw
	}
	if platform == "athena" {
		raw, e := os.ReadFile(filepath.Join(assets, "channel-map"))
		if e != nil {
			return result, e
		}
		expected["lib/rootfs/factory/channel-map"] = raw
	}
	expected["etc/launcher.profile"] = []byte(profileForMode(mode))
	for p, raw := range expected {
		result.check(p, raw, installed)
	}
	// Verify the required calibration aliases without resolving them on the host.
	expectedLinks := map[string]string{}
	for name, target := range aliasTargets() {
		path := "lib/rootfs/proc/" + name
		expectedLinks[path] = target
		result.Compared++
		actual, e := os.Readlink(filepath.Join(installed, filepath.FromSlash(path)))
		if e != nil || actual != target {
			result.Changed = append(result.Changed, difference{path, "invalid-or-missing-symlink"})
		}
	}
	// Detect unexpected runtime regular files and symlinks without following them.
	root := filepath.Join(installed, "lib/rootfs")
	e := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.Type().IsRegular() && d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		rel, e := filepath.Rel(installed, path)
		if e != nil {
			return e
		}
		name := filepath.ToSlash(rel)
		if d.Type().IsRegular() {
			if _, ok := expected[name]; !ok {
				result.Changed = append(result.Changed, difference{name, "extra-installed"})
			}
		} else if d.Type()&os.ModeSymlink != 0 {
			if _, ok := expectedLinks[name]; !ok {
				result.Changed = append(result.Changed, difference{name, "extra-symlink"})
			}
		}
		return nil
	})
	if e != nil {
		return result, e
	}
	result.finish()
	return result, nil
}
func profileForMode(mode string) string {
	if mode == "live" {
		return "ASP1-LAUNCHER-V1\nvendor_mode=LIVE_VENDOR_MATCHED\nsource_pair=OWNER_VERIFIED\n"
	}
	return vendorBundled
}
func individualFingerprint(raw []byte) string {
	h := sha256.Sum256(raw)
	return fmt.Sprintf("%x", h[:8])
}
