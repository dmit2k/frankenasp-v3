package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const backupFolder = "asp-installer-backups"
const generationPrefix = ".external-asp-generation-v3-"

var generationName = regexp.MustCompile("^\\.external-asp-generation-v3-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}$")

func nonce() string {
	b := make([]byte, 6)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func generationID() string { return time.Now().UTC().Format("20060102T150405Z") + "-" + nonce() }
func parentPath() string   { return filepath.Dir(destination) }
func knownGenerations() []string {
	items, e := os.ReadDir(parentPath())
	if e != nil {
		return nil
	}
	result := []string{}
	for _, v := range items {
		if generationName.MatchString(v.Name()) {
			result = append(result, filepath.Join(parentPath(), v.Name()))
		}
	}
	sort.Strings(result)
	return result
}
func incompleteOperations() []string {
	entries, e := os.ReadDir(parentPath())
	if e != nil {
		return []string{"cannot read deployment parent: " + e.Error()}
	}
	out := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".external-asp-stage-v3-") || strings.HasPrefix(name, ".external-asp-uninstall-v3-") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
func backupArchives() []string {
	dir := filepath.Join(parentPath(), backupFolder)
	entries, e := os.ReadDir(dir)
	if e != nil {
		return nil
	}
	out := []string{}
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), "external-asp-") && strings.HasSuffix(entry.Name(), ".tar.gz") {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

func withLock(fn func() error) error {
	if os.Geteuid() != 0 {
		return errors.New("mutation requires root")
	}
	if incomplete := incompleteOperations(); len(incomplete) > 0 {
		return fmt.Errorf("incomplete previous operation: %s; manual recovery required", strings.Join(incomplete, ", "))
	}
	return withInstallerLockAt(parentPath(), fn)
}

// The deployment parent is a location, not an installer-owned resource.
// Do not inspect its owner or permission bits: TECHO5/Android owns this policy.
// Only the lock file itself must belong to the invoking user (root on Echo).
func withInstallerLockAt(parent string, fn func() error) error {
	name := filepath.Join(parent, ".asp-installer-v3.lock")
	fd, e := unix.Open(name, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if e = unix.Fstat(fd, &st); e != nil {
		return e
	}
	if st.Uid != uint32(os.Geteuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Nlink != 1 {
		return errors.New("untrusted installer lock file")
	}
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return fmt.Errorf("installer already running: %w", e)
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return fn()
}
func validateManaged(root string) error {
	st, e := os.Lstat(root)
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed directory unsafe")
	}
	for _, required := range []string{"bin/external-asp", "lib/rootfs/factory/asp1-child", "lib/rootfs/system/bin/linker", "etc/launcher.profile"} {
		if st, e := os.Lstat(filepath.Join(root, required)); e != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("not a recognized FrankenASP V3 runtime: %s", required)
		}
	}
	raw, e := os.ReadFile(filepath.Join(root, "etc/launcher.profile"))
	if e != nil {
		return e
	}
	if string(raw) != profileForMode("live") && string(raw) != profileForMode("bundled") {
		return errors.New("unknown launcher profile")
	}
	nodes := 0
	return filepath.Walk(root, func(path string, st os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		nodes++
		if nodes > 8192 {
			return errors.New("managed tree node count too high")
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok || sys.Uid != 0 {
			return fmt.Errorf("foreign-owned managed node: %s", path)
		}
		if st.Mode()&os.ModeSymlink == 0 && st.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("writable managed node: %s", path)
		}
		switch {
		case st.IsDir():
			return nil
		case st.Mode().IsRegular():
			if sys.Nlink != 1 {
				return fmt.Errorf("hard-linked runtime file: %s", path)
			}
			if st.Size() > 64<<20 {
				return fmt.Errorf("oversized runtime file: %s", path)
			}
			return nil
		case st.Mode()&os.ModeSymlink != 0:
			return nil
		default:
			return fmt.Errorf("special device/socket inside managed runtime: %s", path)
		}
	})
}
func archiveBackup(root string, m *progressMeter) (string, error) {
	if e := validateManaged(root); e != nil {
		return "", e
	}
	archiveDir := filepath.Join(parentPath(), backupFolder)
	if e := os.Mkdir(archiveDir, 0700); e != nil && !os.IsExist(e) {
		return "", e
	}
	st, e := os.Lstat(archiveDir)
	if e != nil {
		return "", e
	}
	ss, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode().Perm() != 0700 || ss.Uid != 0 {
		return "", errors.New("untrusted backup folder")
	}
	estimated, e := estimateTreeBackup(root)
	if e != nil {
		return "", e
	}
	if e = requireSpace(diskNeed{Role: "backup filesystem", Path: parentPath(), Backup: estimated, Reserve: spaceReserve}, m); e != nil {
		return "", e
	}
	m.Phase("Archiving installed external-asp before update")
	f, e := os.OpenFile(filepath.Join(archiveDir, ".partial-"+nonce()), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return "", e
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	var total int64
	var writeErr error
	err := filepath.Walk(root, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		relative, e := filepath.Rel(filepath.Dir(root), path)
		if e != nil {
			return e
		}
		if !safePath(filepath.ToSlash(relative)) {
			return errors.New("invalid backup member path")
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, e = os.Readlink(path)
			if e != nil {
				return e
			}
		}
		header, e := tar.FileInfoHeader(info, link)
		if e != nil {
			return e
		}
		header.Name = filepath.ToSlash(relative)
		header.Uid = 0
		header.Gid = 0
		header.Uname = "root"
		header.Gname = "root"
		if e = tw.WriteHeader(header); e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		total += info.Size()
		if total > 256<<20 {
			return errors.New("archive runtime exceeds 256MiB")
		}
		source, e := os.Open(path)
		if e != nil {
			return e
		}
		n, e := io.Copy(tw, source)
		closeErr := source.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if n != info.Size() {
			return errors.New("backup source truncated")
		}
		return nil
	})
	if err != nil {
		writeErr = err
	}
	if e = tw.Close(); writeErr == nil {
		writeErr = e
	}
	if e = gz.Close(); writeErr == nil {
		writeErr = e
	}
	if e = f.Sync(); writeErr == nil {
		writeErr = e
	}
	if e = f.Close(); writeErr == nil {
		writeErr = e
	}
	if writeErr != nil {
		return "", writeErr
	}
	final := filepath.Join(archiveDir, fmt.Sprintf("external-asp-%s.tar.gz", generationID()))
	if e = unix.Renameat2(unix.AT_FDCWD, temporary, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE); e != nil {
		return "", e
	}
	if e = fsyncDir(archiveDir); e != nil {
		return final, e
	}
	m.Note(fmt.Sprintf("Backup saved: %s", final))
	return final, nil
}
func removeManaged(root string) error {
	if e := validateManaged(root); e != nil {
		return e
	}
	if e := os.RemoveAll(root); e != nil {
		return e
	}
	return fsyncDir(filepath.Dir(root))
}
func generationInfo() []map[string]any {
	out := []map[string]any{}
	for _, g := range knownGenerations() {
		info := map[string]any{"generation": filepath.Base(g), "path": g}
		if e := validateManaged(g); e != nil {
			info["validation_error"] = e.Error()
		} else {
			info["valid"] = true
		}
		out = append(out, info)
	}
	return out
}
func installedVersion() map[string]any {
	res := map[string]any{"installed": false, "path": destination}
	if _, e := os.Lstat(destination); os.IsNotExist(e) {
		return res
	}
	if e := validateManaged(destination); e != nil {
		res["error"] = e.Error()
		return res
	}
	res["installed"] = true
	for _, relative := range []string{"bin/external-asp", "lib/rootfs/factory/asp1-child"} {
		if raw, e := os.ReadFile(filepath.Join(destination, relative)); e == nil {
			sum := sha256.Sum256(raw)
			res[relative+"_sha256_prefix"] = fmt.Sprintf("%x", sum[:8])
		}
	}
	res["release_revision"] = "legacy-or-unrecorded"
	if raw, e := os.ReadFile(filepath.Join(destination, "etc/installer-receipt.json")); e == nil {
		var receipt map[string]any
		if jsonErr := json.Unmarshal(raw, &receipt); jsonErr == nil {
			res["receipt"] = receipt
			if ver, ok := receipt["release_revision"].(string); ok {
				res["release_revision"] = ver
			}
		}
	}
	if raw, e := os.ReadFile(filepath.Join(destination, "etc/launcher.profile")); e == nil {
		if string(raw) == profileForMode("live") {
			res["vendor_mode"] = "live"
		} else {
			res["vendor_mode"] = "bundled"
		}
	}
	return res
}
func safeGeneration(root string) bool {
	return generationName.MatchString(filepath.Base(root)) && filepath.Dir(root) == parentPath()
}
func installOrUpdate(platform, assets string, items map[string][]byte, source, mode string, update, skipBackup, restart bool, m *progressMeter) (map[string]any, error) {
	result := map[string]any{"action": "install", "backup": nil}
	if update {
		result["action"] = "update"
	}
	e := withLock(func() error {
		exists := false
		if _, e := os.Lstat(destination); e == nil {
			exists = true
		} else if !os.IsNotExist(e) {
			return e
		}
		if update != exists {
			return errors.New("install needs absent external-asp; update needs existing managed external-asp")
		}
		if exists {
			if e := validateManaged(destination); e != nil {
				return e
			}
		}
		if mode == "live" && vendorReadOnly() != nil {
			return errors.New("live vendor must remain read-only")
		}
		before := installedVersion()
		result["previous"] = before
		var sourceID fileID
		if exists {
			sourceID, _ = idOf(destination)
		}
		if err := preflightBuild(result["action"].(string), platform, mode, assets, items, skipBackup, m); err != nil {
			return err
		}
		m.Phase("Preparing separate immutable runtime generation")
		stage, e := os.MkdirTemp(parentPath(), ".external-asp-stage-v3-")
		if e != nil {
			return e
		}
		cleanup := true
		defer func() {
			if cleanup {
				_ = os.RemoveAll(stage)
			}
		}()
		prepared := make(map[string][]byte, len(items))
		for p, v := range items {
			if mode == "live" && strings.HasPrefix(p, "vendor/") {
				continue
			}
			prepared[p] = v
		}
		if e = writeRootWithMode(stage, platform, prepared, assets, source, mode); e != nil {
			return e
		}
		if e = validateManaged(stage); e != nil {
			return e
		}
		if e = fsyncDir(stage); e != nil {
			return e
		}
		if exists {
			roots, e := activeRoots()
			if e != nil {
				return e
			}
			m.Phase("Checking live helper sessions and pinned resources")
			if e = requireInactive(roots, m, restart); e != nil {
				return e
			}
			now, e := idOf(destination)
			if e != nil || now != sourceID {
				return errors.New("installed runtime identity changed during update")
			}
			diff, e := compareInstalled(platform, assets, items, mode, destination)
			if e != nil {
				return e
			}
			result["difference"] = diff
			if diff.Matching {
				m.Note("All managed files already match; no exchange required")
				return nil
			}
			// Archive before any atomic swap, unless the user explicitly opted out.
			if !skipBackup {
				backup, e := archiveBackup(destination, m)
				if e != nil {
					return e
				}
				result["backup"] = backup
			}
			m.Phase("Atomically exchanging new and previous runtime")
			if e = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, destination, unix.RENAME_EXCHANGE); e != nil {
				return e
			}
			cleanup = false // stage is now the old runtime; never delete after an exchange
			if e = fsyncDir(parentPath()); e != nil {
				return e
			}
			gen := filepath.Join(parentPath(), generationPrefix+generationID())
			if e = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, gen, unix.RENAME_NOREPLACE); e != nil {
				return fmt.Errorf("new runtime installed, previous retained at %s; generation rename: %w", stage, e)
			}
			result["previous_generation"] = filepath.Base(gen)
			result["changed"] = true
		} else {
			roots, e := activeRoots()
			if e != nil {
				return e
			}
			if len(roots) > 0 {
				return errors.New("unexpected active generation without current installation")
			}
			m.Phase("Atomically publishing first installation")
			if e = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE); e != nil {
				return e
			}
			cleanup = false
			result["changed"] = true
		}
		return fsyncDir(parentPath())
	})
	if e != nil {
		return result, e
	}
	result["current"] = installedVersion()
	return result, nil
}
