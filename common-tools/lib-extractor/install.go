package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const destination = "/data/techo5-linux/external-asp"
const vendorBundled = "ASP1-LAUNCHER-V1\nvendor_mode=OWNER_BUNDLED_MATCHED\nsource_pair=OWNER_VERIFIED\n"

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
	if e := os.Chown(path, 0, 0); e != nil {
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
		return os.Chown(p, 0, 0)
	})
}
func fsyncDir(p string) error {
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func writeRoot(stage string, platform string, items map[string][]byte, assets string, kind string) error {
	root := filepath.Join(stage, "lib", "rootfs")
	directories := []string{"bin", "etc", "lib/rootfs/factory", "lib/rootfs/system/bin", "lib/rootfs/system/lib", "lib/rootfs/vendor/etc/audio-algorithms", "lib/rootfs/proc/idme", "lib/rootfs/sys", "lib/rootfs/.realproc", "lib/rootfs/dev", "lib/rootfs/data"}
	for _, n := range directories {
		if e := os.MkdirAll(filepath.Join(stage, n), 0700); e != nil {
			return e
		}
	}
	for p, raw := range items {
		if !safePath(p) || !strings.HasPrefix(p, "system/") && !strings.HasPrefix(p, "vendor/") {
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
	for _, asset := range []struct {
		src, dst string
		mode     os.FileMode
	}{
		{"external-asp", "bin/external-asp", 0500}, {"asp1-child", "lib/rootfs/factory/asp1-child", 0500},
	} {
		raw, e := os.ReadFile(filepath.Join(assets, asset.src))
		if e != nil {
			return e
		}
		if e = ownedFile(filepath.Join(stage, asset.dst), raw, asset.mode); e != nil {
			return e
		}
	}
	if platform == "athena" {
		raw, e := os.ReadFile(filepath.Join(assets, "channel-map"))
		if e != nil {
			return e
		}
		if e = validateChannelMap(raw); e != nil {
			return e
		}
		if e = ownedFile(filepath.Join(root, "factory/channel-map"), raw, 0600); e != nil {
			return e
		}
	}
	if e := ownedFile(filepath.Join(stage, "etc/launcher.profile"), []byte(vendorBundled), 0600); e != nil {
		return e
	}
	aliases := map[string]string{"self": "/.realproc/self", "sys": "/.realproc/sys", "cpuinfo": "/.realproc/cpuinfo", "device-tree": "/sys/firmware/devicetree/base"}
	for i := 0; i < 4; i++ {
		aliases[fmt.Sprintf("idme/miccal.%d", i)] = fmt.Sprintf("/proc/device-tree/idme/miccal.%d/value", i)
	}
	for name, target := range aliases {
		p := filepath.Join(root, "proc", name)
		if e := os.Symlink(target, p); e != nil {
			return e
		}
		if e := os.Lchown(p, 0, 0); e != nil {
			return e
		}
	}
	receipt := map[string]any{"schema": "ASP1-INSTALLER-V3", "platform": platform, "runtime_source": kind, "proprietary_binaries_distributed": false, "installed_at": time.Now().UTC().Format(time.RFC3339)}
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
			return fsyncDir(p)
		}
		return nil
	})
}
func install(platform, assets string, items map[string][]byte, source string, output string) error {
	if os.Geteuid() != 0 {
		return errors.New("installation requires root")
	}
	if output != destination {
		return errors.New("production target is fixed; use --output only in unprivileged inspect")
	}
	parent := filepath.Dir(output)
	info, e := os.Lstat(parent)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("deployment parent missing or not a real directory")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 {
		return errors.New("insecure deployment parent ownership or mode")
	}
	if _, e = os.Lstat(output); e == nil {
		return errors.New("destination exists: no automatic replacement of active or unknown installation")
	}
	if !os.IsNotExist(e) {
		return e
	}
	var bytes uint64
	for _, raw := range items {
		bytes += uint64(len(raw))
	}
	if e = enoughSpace(parent, bytes+64<<20); e != nil {
		return e
	}
	stage, e := os.MkdirTemp(parent, ".external-asp-stage-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if e = writeRoot(stage, platform, items, assets, source); e != nil {
		return e
	}
	if e = fsyncDir(stage); e != nil {
		return e
	}
	if e = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, output, unix.RENAME_NOREPLACE); e != nil {
		return e
	}
	return fsyncDir(parent)
}
