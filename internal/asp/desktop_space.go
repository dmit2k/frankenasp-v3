package asp

// PreflightGenericDesktop reserves the cache and download space. Desktop
// extraction never stages or modifies an installed runtime.
func PreflightGenericDesktop(cacheDir, tmpDir, _ string, downloading, _ bool, meter *progressMeter) (downloadBudget, error) {
	cacheDisk, err := diskAt(cacheDir)
	if err != nil {
		return downloadBudget{}, err
	}
	tmpDisk := cacheDisk
	if downloading {
		tmpDisk, err = diskAt(tmpDir)
		if err != nil {
			return downloadBudget{}, err
		}
	}
	needs, budget, err := composeSpacePlan(cacheDir, cacheDir, tmpDir, cacheDisk, cacheDisk, tmpDisk, 0, 0, downloading)
	if err != nil {
		return downloadBudget{}, err
	}
	for _, n := range needs {
		if err = requireSpace(n, meter); err != nil {
			return downloadBudget{}, err
		}
	}
	return budget, nil
}
