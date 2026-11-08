package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

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

// Production rootfs budgeting reads only the verified asset inventory.
func predictedRootfsBytesPlan(items map[string][]byte, plan asp.AssetPlan, mode string, block uint64) (uint64, error) {
	if block == 0 {
		return 0, errors.New("zero filesystem block size")
	}
	var size, count uint64
	for name, data := range items {
		if mode == "live" && len(name) >= 7 && name[:7] == "vendor/" {
			continue
		}
		count++
		var err error
		size, err = sumSpace(size, asp.RoundAlloc(uint64(len(data)), block))
		if err != nil {
			return 0, err
		}
	}
	for _, asset := range plan.Installed {
		data, ok := plan.Files[asset.Path]
		if !ok {
			return 0, fmt.Errorf("verified release resource unavailable %s", asset.Path)
		}
		count++
		var err error
		size, err = sumSpace(size, asp.RoundAlloc(uint64(len(data)), block))
		if err != nil {
			return 0, err
		}
	}
	// Existing layout/profile/receipt and alias metadata overhead.
	return sumSpace(size, (count+64)*asp.RoundAlloc(4096, block), asp.StagingOverhead)
}
func preflightBuildPlan(cmd, mode string, plan asp.AssetPlan, items map[string][]byte, noBackup bool, m *progressMeter) error {
	if cmd != "install" && cmd != "update" {
		return nil
	}
	device, e := diskAt(parentPath())
	if e != nil {
		return e
	}
	runtime, e := predictedRootfsBytesPlan(items, plan, mode, device.BlockSize)
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
	return asp.RequireSpace(diskNeed{Role: "installation filesystem (before staging)", Path: parentPath(), Rootfs: runtime, Backup: backup, Reserve: spaceReserve}, m)
}
