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

	"github.com/dmit2k/frankenasp-v3/internal/asp"
	"github.com/dmit2k/frankenasp-v3/platforms"
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
func chooseVendorMode(platform, requested string, items map[string][]byte) (string, compareResult, error) {
	c, ok, err := platforms.LegacyContract(platform)
	if err != nil {
		return "", compareResult{}, err
	}
	if !ok {
		return "", compareResult{}, errors.New("unknown legacy vendor mode contract")
	}
	return chooseVendorModeCapabilities(asp.ReleaseRuntime{VendorModes: c.Runtime.VendorModes, LibrarySearch: c.Runtime.LibrarySearch}, requested, items)
}
func chooseVendorModeCapabilities(runtime asp.ReleaseRuntime, requested string, items map[string][]byte) (string, compareResult, error) {
	return chooseVendorModeForRoot(runtime, requested, items, "/vendor", vendorReadOnly)
}
func chooseVendorModeForRoot(runtime asp.ReleaseRuntime, requested string, items map[string][]byte, root string, readOnly func() error) (string, compareResult, error) {
	var inspection compareResult
	if requested == "" {
		requested = "bundled"
	}
	if requested != "bundled" && requested != "live" && requested != "auto" {
		return "", inspection, errors.New("--vendor-mode must be bundled, live or auto")
	}
	allowed := false
	for _, v := range runtime.VendorModes {
		if v == "live" {
			allowed = true
		}
	}
	if requested == "live" && !allowed {
		return "", inspection, errors.New("launcher does not support live vendor")
	}
	if requested == "bundled" || !allowed {
		return "bundled", inspection, nil
	}
	inspection = compareVendor(items, root)
	valid := inspection.Matching && inspection.Compared > 0 && readOnly() == nil
	if valid {
		return "live", inspection, nil
	}
	if requested == "live" {
		return "", inspection, errors.New("live vendor mismatch or not mounted read-only")
	}
	return "bundled", inspection, nil
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
