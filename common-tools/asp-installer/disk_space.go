package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const (
	spaceReserve         uint64 = 64 << 20
	maxRuntimeSource     uint64 = 128 << 20
	stagingOverhead      uint64 = 8 << 20
	downloadStagingBound uint64 = maxRuntimeSource + 32<<20
)

type diskSnapshot struct {
	Dev       uint64
	Available uint64
	BlockSize uint64
}

func diskAt(dir string) (diskSnapshot, error) {
	var stat unix.Statfs_t
	if e := unix.Statfs(dir, &stat); e != nil {
		return diskSnapshot{}, e
	}
	var st unix.Stat_t
	if e := unix.Stat(dir, &st); e != nil {
		return diskSnapshot{}, e
	}
	if stat.Bsize <= 0 {
		return diskSnapshot{}, errors.New("invalid filesystem block size")
	}
	block := uint64(stat.Bsize)
	if stat.Bavail > math.MaxUint64/block {
		return diskSnapshot{}, errors.New("filesystem free space overflow")
	}
	return diskSnapshot{Dev: uint64(st.Dev), Available: stat.Bavail * block, BlockSize: block}, nil
}
func sumSpace(parts ...uint64) (uint64, error) {
	var total uint64
	for _, p := range parts {
		if p > math.MaxUint64-total {
			return 0, errors.New("disk space size overflow")
		}
		total += p
	}
	return total, nil
}
func roundAlloc(size, block uint64) uint64 {
	if size == 0 {
		return block
	}
	if size > math.MaxUint64-(block-1) {
		return math.MaxUint64
	}
	return ((size + block - 1) / block) * block
}

type diskNeed struct {
	Role    string
	Path    string
	OTA     uint64
	Rootfs  uint64
	Backup  uint64
	Reserve uint64
}
type diskReport struct {
	Role         string `json:"role"`
	AvailableMiB uint64 `json:"available_mib"`
	RequiredMiB  uint64 `json:"required_mib"`
	ReserveMiB   uint64 `json:"reserve_mib"`
	OTABytes     uint64 `json:"ota_bytes"`
	RootfsBytes  uint64 `json:"rootfs_bytes"`
	BackupBytes  uint64 `json:"backup_bytes"`
}

func mbCeil(n uint64) uint64 {
	whole := n >> 20
	if n&((1<<20)-1) != 0 {
		whole++
	}
	return whole
}
func evaluateSpace(available uint64, need diskNeed) (diskReport, error) {
	required, e := sumSpace(need.OTA, need.Rootfs, need.Backup, need.Reserve)
	if e != nil {
		return diskReport{}, e
	}
	result := diskReport{Role: need.Role, AvailableMiB: available >> 20, RequiredMiB: mbCeil(required), ReserveMiB: mbCeil(need.Reserve), OTABytes: need.OTA, RootfsBytes: need.Rootfs, BackupBytes: need.Backup}
	if available < required {
		return result, fmt.Errorf("insufficient disk space on %s: %d MiB available, %d MiB required (OTA %d MiB + staged rootfs %d MiB + backup %d MiB + reserve %d MiB)", need.Role, available>>20, mbCeil(required), mbCeil(need.OTA), mbCeil(need.Rootfs), mbCeil(need.Backup), mbCeil(need.Reserve))
	}
	return result, nil
}
func requireSpace(need diskNeed, m *progressMeter) error {
	stat, e := diskAt(need.Path)
	if e != nil {
		return e
	}
	report, e := evaluateSpace(stat.Available, need)
	if m != nil {
		m.Note(fmt.Sprintf("Disk preflight [%s]: %d MiB free; %d MiB required (OTA %d, runtime %d, backup %d, safety reserve %d MiB)", report.Role, report.AvailableMiB, report.RequiredMiB, mbCeil(need.OTA), mbCeil(need.Rootfs), mbCeil(need.Backup), report.ReserveMiB))
	}
	return e
}
func estimateTreeBackup(root string) (uint64, error) {
	if e := validateManaged(root); e != nil {
		return 0, e
	}
	var content uint64
	var entries uint64
	e := filepath.Walk(root, func(path string, info os.FileInfo, walkError error) error {
		if walkError != nil {
			return walkError
		}
		entries++
		if info.Mode().IsRegular() {
			if info.Size() < 0 {
				return errors.New("invalid runtime file size")
			}
			var er error
			content, er = sumSpace(content, uint64(info.Size()))
			if er != nil {
				return er
			}
		}
		return nil
	})
	if e != nil {
		return 0, e
	}
	if content > 256<<20 {
		return 0, errors.New("runtime exceeds maximum archive size of 256MiB")
	}
	// Conservative tar framing (up to 2 KiB/member), gzip overhead and metadata.
	overhead, e := sumSpace(2<<20, entries*2048, content/20)
	if e != nil {
		return 0, e
	}
	return sumSpace(content, overhead)
}
func predictedRootfsBytes(items map[string][]byte, assets string, platform, mode string, block uint64) (uint64, error) {
	if block == 0 {
		return 0, errors.New("zero filesystem block size")
	}
	var size uint64
	count := uint64(0)
	for name, data := range items {
		if mode == "live" && len(name) >= 7 && name[:7] == "vendor/" {
			continue
		}
		count++
		var e error
		size, e = sumSpace(size, roundAlloc(uint64(len(data)), block))
		if e != nil {
			return 0, e
		}
	}
	needed := []string{"asp1-child", "external-asp", "manifest.json"}
	if platform == "athena" {
		needed = append(needed, "channel-map")
	}
	for _, name := range needed {
		info, e := os.Stat(filepath.Join(assets, name))
		if e != nil {
			return 0, e
		}
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("invalid asset %s", name)
		}
		count++
		size, e = sumSpace(size, roundAlloc(uint64(info.Size()), block))
		if e != nil {
			return 0, e
		}
	}
	// Directories, profile, receipt, calibration links and filesystem metadata.
	size, e := sumSpace(size, (count+64)*roundAlloc(4096, block), stagingOverhead)
	if e != nil {
		return 0, e
	}
	return size, nil
}
func preflightBeforeOTA(cmd, otaURL, tmpDir string, noBackup bool, m *progressMeter) (uint64, error) {
	if cmd != "install" && cmd != "update" {
		return 0, nil
	}
	target, e := diskAt(parentPath())
	if e != nil {
		return 0, e
	}
	var backup uint64
	if cmd == "update" && !noBackup {
		backup, e = estimateTreeBackup(destination)
		if e != nil {
			return 0, e
		}
	}
	// Before extraction, reserve room for the full bounded runtime rather than
	// guessing from a particular firmware image. Exact bytes are checked later.
	stage := downloadStagingBound + stagingOverhead
	if e = requireSpace(diskNeed{Role: "installation filesystem (pre-download)", Path: parentPath(), Rootfs: stage, Backup: backup, Reserve: spaceReserve}, m); e != nil {
		return 0, e
	}
	if otaURL == "" {
		return 0, nil
	}
	cache, e := diskAt(tmpDir)
	if e != nil {
		return 0, e
	}
	if cache.Dev == target.Dev {
		return stage + backup, nil
	}
	if e = requireSpace(diskNeed{Role: "OTA temporary filesystem", Path: tmpDir, Reserve: spaceReserve}, m); e != nil {
		return 0, e
	}
	return 0, nil
}
func preflightBuild(cmd, platform, mode, assets string, items map[string][]byte, noBackup bool, m *progressMeter) error {
	if cmd != "install" && cmd != "update" {
		return nil
	}
	device, e := diskAt(parentPath())
	if e != nil {
		return e
	}
	rootfs, e := predictedRootfsBytes(items, assets, platform, mode, device.BlockSize)
	if e != nil {
		return e
	}
	var backup uint64
	if cmd == "update" && !noBackup {
		backup, e = estimateTreeBackup(destination)
		if e != nil {
			return e
		}
	}
	return requireSpace(diskNeed{Role: "installation filesystem (before staging)", Path: parentPath(), Rootfs: rootfs, Backup: backup, Reserve: spaceReserve}, m)
}
