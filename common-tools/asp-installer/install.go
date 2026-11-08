package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

var destination = "/data/techo5-linux/external-asp"

const vendorBundled = "ASP1-LAUNCHER-V1\nvendor_mode=OWNER_BUNDLED_MATCHED\nsource_pair=OWNER_VERIFIED\n"

// Narrow local test seams only. Production always uses the real system chown.
var runtimeChown = os.Chown
var runtimeLchown = os.Lchown

// Production-managed nodes are root-owned; isolated mutation tests substitute
// the current test UID without bypassing the filesystem walk.
var runtimeOwnerUID uint32 = 0

func ownedFile(path string, data []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	if e := os.WriteFile(path, data, mode); e != nil {
		return e
	}
	if e := os.Chmod(path, mode); e != nil {
		return e
	}
	if e := runtimeChown(path, 0, 0); e != nil {
		return e
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func ensureOwnedDirs(root string) error {
	return filepath.Walk(root, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if !info.IsDir() {
			return nil
		}
		if e := os.Chmod(p, 0700); e != nil {
			return e
		}
		return runtimeChown(p, 0, 0)
	})
}
func writeRootWithPlan(stage string, items map[string][]byte, plan asp.AssetPlan, kind string, mode string, miccal []int) error {
	platform := plan.Platform
	root := filepath.Join(stage, "lib", "rootfs")
	directories := []string{"bin", "etc", "lib/rootfs/factory", "lib/rootfs/system/bin", "lib/rootfs/system/lib", "lib/rootfs/vendor/etc/audio-algorithms", "lib/rootfs/proc/idme", "lib/rootfs/sys", "lib/rootfs/.realproc", "lib/rootfs/dev", "lib/rootfs/data"}
	for _, n := range directories {
		if e := os.MkdirAll(filepath.Join(stage, n), 0700); e != nil {
			return e
		}
	}
	for p, raw := range items {
		if mode == "live" && strings.HasPrefix(p, "vendor/") {
			continue
		}
		if !asp.SafePath(p) || !strings.HasPrefix(p, "system/") && !strings.HasPrefix(p, "vendor/") {
			return fmt.Errorf("invalid runtime filename: %s", p)
		}
		mode := os.FileMode(0600)
		if p == "system/bin/linker" {
			mode = 0500
		}
		if e := ownedFile(filepath.Join(root, filepath.FromSlash(p)), raw, mode); e != nil {
			return e
		}
	}
	for _, asset := range plan.Installed {
		raw, ok := plan.Files[asset.Path]
		if !ok {
			return fmt.Errorf("verified asset snapshot missing %s", asset.Path)
		}
		mode := os.FileMode(0600)
		if asset.Role == "launcher" || asset.Role == "child" {
			mode = 0500
		}
		if e := ownedFile(filepath.Join(stage, filepath.FromSlash(asset.InstallTo)), raw, mode); e != nil {
			return e
		}
	}
	if e := ownedFile(filepath.Join(stage, "etc/launcher.profile"), []byte(profileForMode(mode)), 0600); e != nil {
		return e
	}
	for name, target := range aliasTargetsForMiccal(miccal) {
		p := filepath.Join(root, "proc", name)
		if e := os.Symlink(target, p); e != nil {
			return e
		}
		if e := runtimeLchown(p, 0, 0); e != nil {
			return e
		}
	}
	receipt := map[string]any{"schema": "ASP1-INSTALLER-V3", "platform": platform, "runtime_source": kind, "proprietary_binaries_distributed": false, "installed_at": time.Now().UTC().Format(time.RFC3339), "release_revision": revision, "vendor_mode": mode, "miccal_aliases": miccal, "miccal_inventory_version": 1}
	blob, _ := json.MarshalIndent(receipt, "", "  ")
	if e := ownedFile(filepath.Join(stage, "etc/installer-receipt.json"), append(blob, '\n'), 0600); e != nil {
		return e
	}
	if e := ensureOwnedDirs(stage); e != nil {
		return e
	}
	return filepath.Walk(stage, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return asp.FsyncDir(p)
		}
		return nil
	})
}
