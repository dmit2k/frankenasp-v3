package asp

import (
	"errors"
	"fmt"
	"math"

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
	Cache   uint64
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
	CacheBytes   uint64 `json:"cache_bytes"`
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
	required, e := sumSpace(need.OTA, need.Cache, need.Rootfs, need.Backup, need.Reserve)
	if e != nil {
		return diskReport{}, e
	}
	result := diskReport{Role: need.Role, AvailableMiB: available >> 20, RequiredMiB: mbCeil(required), ReserveMiB: mbCeil(need.Reserve), OTABytes: need.OTA, CacheBytes: need.Cache, RootfsBytes: need.Rootfs, BackupBytes: need.Backup}
	if available < required {
		return result, fmt.Errorf("insufficient disk space on %s: %d MiB available, %d MiB required (OTA %d MiB + extraction cache %d MiB + staged runtime %d MiB + backup %d MiB + safety reserve %d MiB)", need.Role, available>>20, mbCeil(required), mbCeil(need.OTA), mbCeil(need.Cache), mbCeil(need.Rootfs), mbCeil(need.Backup), mbCeil(need.Reserve))
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
		m.Note(fmt.Sprintf("Disk capacity [%s on %s]: %d MiB free; %d MiB reserved/required (OTA %d, extraction cache %d, staged runtime %d, backup %d, safety reserve %d MiB)", report.Role, need.Path, report.AvailableMiB, report.RequiredMiB, mbCeil(need.OTA), mbCeil(need.Cache), mbCeil(need.Rootfs), mbCeil(need.Backup), report.ReserveMiB))
	}
	return e
}
