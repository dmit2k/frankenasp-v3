package main

import "github.com/dmit2k/frankenasp-v3/internal/asp"

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
		if e = asp.RequireSpace(need, m); e != nil {
			return downloadBudget{}, e
		}
	}
	return budget, nil
}
