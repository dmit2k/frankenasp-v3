package main

import (
	"fmt"
	"strings"
)

// Conservative upper bounds, not the actual size of any OTA or extracted image.
const cacheCapacityBound = downloadStagingBound + stagingOverhead
const runtimeCapacityBound = downloadStagingBound + stagingOverhead

// The download budget represents bytes that must remain free on the download
// filesystem after the last OTA byte is written. Cache/runtime/backup are
// included only when they actually share that filesystem.
type downloadBudget struct {
	need diskNeed
}

type planVolume struct {
	need diskNeed
}

func addPlannedNeed(groups map[uint64]*planVolume, disk diskSnapshot, path, role string, cache, runtime, backup uint64) error {
	item, ok := groups[disk.Dev]
	if !ok {
		item = &planVolume{need: diskNeed{Role: role, Path: path, Reserve: spaceReserve}}
		groups[disk.Dev] = item
	}
	var e error
	item.need.Cache, e = sumSpace(item.need.Cache, cache)
	if e != nil {
		return e
	}
	item.need.Rootfs, e = sumSpace(item.need.Rootfs, runtime)
	if e != nil {
		return e
	}
	item.need.Backup, e = sumSpace(item.need.Backup, backup)
	return e
}

// composeSpacePlan groups roles by filesystem device instead of performing
// separate preflights that can each individually fit but collectively overfill
// one filesystem.
func composeSpacePlan(cacheDir, installDir, tmpDir string, cacheDisk, installDisk, tmpDisk diskSnapshot, runtime, backup uint64, downloading bool) ([]diskNeed, downloadBudget, error) {
	groups := map[uint64]*planVolume{}
	if e := addPlannedNeed(groups, cacheDisk, cacheDir, "extraction cache", cacheCapacityBound, 0, 0); e != nil {
		return nil, downloadBudget{}, e
	}
	if runtime != 0 || backup != 0 {
		if e := addPlannedNeed(groups, installDisk, installDir, "staged installation", 0, runtime, backup); e != nil {
			return nil, downloadBudget{}, e
		}
	}
	if downloading {
		if e := addPlannedNeed(groups, tmpDisk, tmpDir, "OTA download", 0, 0, 0); e != nil {
			return nil, downloadBudget{}, e
		}
	}
	// One reserve per real filesystem, even when cache, installation and OTA
	// download share the same device. Name each component explicitly.
	for id, v := range groups {
		parts := []string{}
		if v.need.Cache > 0 {
			parts = append(parts, "extraction cache")
		}
		if v.need.Rootfs > 0 {
			parts = append(parts, "staged runtime")
		}
		if v.need.Backup > 0 {
			parts = append(parts, "backup")
		}
		if downloading && id == tmpDisk.Dev {
			parts = append(parts, "OTA download (size pending)")
		}
		v.need.Role = strings.Join(parts, " + ")
	}
	var budget downloadBudget
	if downloading {
		volume, ok := groups[tmpDisk.Dev]
		if !ok {
			return nil, downloadBudget{}, fmt.Errorf("missing OTA download volume")
		}
		budget.need = volume.need
		budget.need.Role = "OTA download (HTTP Content-Length)"
		budget.need.Path = tmpDir
	}
	needs := []diskNeed{}
	for _, id := range []uint64{cacheDisk.Dev, installDisk.Dev, tmpDisk.Dev} {
		if v, ok := groups[id]; ok {
			needs = append(needs, v.need)
			delete(groups, id)
		}
	}
	return needs, budget, nil
}

func preflightCacheAndOTA(cacheDir, tmpDir, cmd string, downloadRequired, noBackup bool, m *progressMeter) (downloadBudget, error) {
	cacheDisk, e := diskAt(cacheDir)
	if e != nil {
		return downloadBudget{}, e
	}
	installDisk := cacheDisk
	installDir := cacheDir
	var runtime, backup uint64
	if cmd == "install" || cmd == "update" {
		installDir = parentPath()
		installDisk, e = diskAt(installDir)
		if e != nil {
			return downloadBudget{}, e
		}
		runtime = runtimeCapacityBound
		if cmd == "update" && !noBackup {
			backup, e = estimateTreeBackup(destination)
			if e != nil {
				return downloadBudget{}, e
			}
		}
	}
	tmpDisk := cacheDisk
	if downloadRequired {
		tmpDisk, e = diskAt(tmpDir)
		if e != nil {
			return downloadBudget{}, e
		}
	}
	needs, budget, e := composeSpacePlan(cacheDir, installDir, tmpDir, cacheDisk, installDisk, tmpDisk, runtime, backup, downloadRequired)
	if e != nil {
		return downloadBudget{}, e
	}
	if downloadRequired && m != nil {
		m.Note("OTA size unknown until HTTP Content-Length; initial totals exclude OTA download bytes")
	}
	for _, need := range needs {
		if e = requireSpace(need, m); e != nil {
			return downloadBudget{}, e
		}
	}
	return budget, nil
}
