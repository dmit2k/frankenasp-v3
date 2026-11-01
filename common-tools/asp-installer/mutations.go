package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func uninstallAll(purgeBackups, restart bool, m *progressMeter) (map[string]any, error) {
	result := map[string]any{"action": "uninstall", "removed": []string{}, "backups_kept": !purgeBackups}
	e := withLock(func() error {
		roots, e := activeRoots()
		if e != nil {
			return e
		}
		if len(roots) == 0 && !purgeBackups {
			return errors.New("no installed FrankenASP runtime or generations")
		}
		for _, r := range roots {
			if e = validateManaged(r); e != nil {
				return fmt.Errorf("refusing to remove unrecognized runtime %s: %w", r, e)
			}
		}
		m.Phase("Checking pinned processes before uninstall")
		if e = requireInactive(roots, m, restart); e != nil {
			return e
		}
		// Detach current installation before deleting any bytes. If deletion fails,
		// the renamed tree remains recoverable under a visible .uninstall-v3 name.
		removed := []string{}
		for _, r := range roots {
			if _, e = os.Lstat(r); os.IsNotExist(e) {
				continue
			}
			if r != destination && !safeGeneration(r) {
				return errors.New("unsafe generation path")
			}
			trash := filepath.Join(parentPath(), ".external-asp-uninstall-v3-"+generationID())
			if e = unix.Renameat2(unix.AT_FDCWD, r, unix.AT_FDCWD, trash, unix.RENAME_NOREPLACE); e != nil {
				return e
			}
			if e = fsyncDir(parentPath()); e != nil {
				return fmt.Errorf("runtime detached at %s: %w", trash, e)
			}
			// One more scan catches a helper which spawned between preflight and rename.
			latest, e := scanActivity([]string{trash})
			if e != nil || len(latest.helpers) > 0 || len(latest.blockers) > 0 {
				return fmt.Errorf("detached runtime at %s may still be pinned; preserve it, inspect device: %v", trash, e)
			}
			m.Phase("Removing confirmed FrankenASP tree " + filepath.Base(r))
			if e = removeManaged(trash); e != nil {
				return fmt.Errorf("partially removed tree %s: %w", trash, e)
			}
			removed = append(removed, filepath.Base(r))
		}
		result["removed"] = removed
		if purgeBackups {
			archiveDir := filepath.Join(parentPath(), backupFolder)
			if st, e := os.Lstat(archiveDir); e == nil {
				if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 {
					return errors.New("unsafe backup directory")
				}
				children, e := os.ReadDir(archiveDir)
				if e != nil {
					return e
				}
				for _, item := range children {
					name := item.Name()
					if !strings.HasPrefix(name, "external-asp-") || !strings.HasSuffix(name, ".tar.gz") || !item.Type().IsRegular() {
						return fmt.Errorf("unexpected file in backup folder: %s", name)
					}
				}
				m.Phase("Removing archived FrankenASP backups by explicit request")
				if e = os.RemoveAll(archiveDir); e != nil {
					return e
				}
			} else if e != nil && !os.IsNotExist(e) {
				return e
			}
			result["backups_kept"] = false
		}
		return fsyncDir(parentPath())
	})
	if e != nil {
		return result, e
	}
	if !purgeBackups {
		result["backup_directory"] = filepath.Join(parentPath(), backupFolder)
	}
	return result, nil
}
func rollbackGeneration(generation string, restart bool, m *progressMeter) (map[string]any, error) {
	result := map[string]any{"action": "rollback"}
	if !generationName.MatchString(generation) {
		return result, errors.New("invalid generation name")
	}
	e := withLock(func() error {
		gen := filepath.Join(parentPath(), generation)
		for _, root := range []string{destination, gen} {
			if e := validateManaged(root); e != nil {
				return fmt.Errorf("invalid current/rollback generation %s: %w", root, e)
			}
		}
		roots, e := activeRoots()
		if e != nil {
			return e
		}
		if e = requireInactive(roots, m, restart); e != nil {
			return e
		}
		// Make an archive before replacing the installed version, even on rollback.
		backup, e := archiveBackup(destination, m)
		if e != nil {
			return e
		}
		result["backup"] = backup
		m.Phase("Atomically exchanging installed runtime with saved generation")
		if e = unix.Renameat2(unix.AT_FDCWD, gen, unix.AT_FDCWD, destination, unix.RENAME_EXCHANGE); e != nil {
			return e
		}
		if e = fsyncDir(parentPath()); e != nil {
			return e
		}
		newGen := filepath.Join(parentPath(), generationPrefix+generationID())
		if e = unix.Renameat2(unix.AT_FDCWD, gen, unix.AT_FDCWD, newGen, unix.RENAME_NOREPLACE); e != nil {
			return fmt.Errorf("rollback exchanged, replaced runtime left at %s: %w", gen, e)
		}
		result["saved_replaced_generation"] = filepath.Base(newGen)
		return fsyncDir(parentPath())
	})
	if e != nil {
		return result, e
	}
	result["current"] = installedVersion()
	return result, nil
}
