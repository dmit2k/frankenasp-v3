package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Only scan the user-selected download directory. A filename match is a
// candidate, never proof: the complete SHA-256 must match a verified cache.
func localOTACandidateName(filename, expected string) bool {
	if filename == expected {
		return true
	}
	const prefix = "asp-installer-ota-"
	if !strings.HasPrefix(filename, prefix) {
		return false
	}
	remainder := strings.TrimPrefix(filename, prefix)
	if len(remainder) != 12+1+len(expected) || remainder[12] != '-' ||
		remainder[13:] != expected {
		return false
	}
	_, err := hex.DecodeString(remainder[:12])
	return err == nil
}

func hashLocalCandidate(ctx context.Context, filename string) (string, error) {
	fd, err := unix.Open(filename, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), filename)
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return "", err
	}
	raw, ok := stat.Sys().(*syscall.Stat_t)
	if !ok || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > 2<<30 ||
		raw.Uid != uint32(os.Geteuid()) || raw.Nlink != 1 {
		return "", errors.New("unsafe candidate OTA file")
	}
	hash := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		n, e := file.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}
	// Reject files modified or replaced while hashing. Untrusted local files
	// must never be treated as verified merely because their names match.
	after, e := file.Stat()
	if e != nil {
		return "", e
	}
	if !os.SameFile(stat, after) || stat.Size() != after.Size() ||
		!stat.ModTime().Equal(after.ModTime()) {
		return "", errors.New("OTA candidate changed during verification")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func cachedLocalOTAForURL(ctx context.Context, cacheDir, otaSearchDir, platform, helperHash, rawURL string, m *progressMeter) (cacheResult, string, error) {
	u, e := url.Parse(rawURL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return cacheResult{}, "", errors.New("OTA URL must be HTTP(S)")
	}
	expected := safeOTAName(path.Base(u.Path))
	// Generic or missing names cannot establish a useful filename association.
	if expected == "firmware.bin" {
		return cacheResult{}, "", os.ErrNotExist
	}
	entries, e := os.ReadDir(otaSearchDir)
	if e != nil {
		return cacheResult{}, "", e
	}
	checked := 0
	for _, entry := range entries {
		if !localOTACandidateName(entry.Name(), expected) {
			continue
		}
		if e = ctx.Err(); e != nil {
			return cacheResult{}, "", e
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			continue
		}
		filename := filepath.Join(otaSearchDir, entry.Name())
		checked++
		if checked > 64 {
			break
		}
		hash, err := hashLocalCandidate(ctx, filename)
		if err != nil {
			if ctx.Err() != nil {
				return cacheResult{}, "", ctx.Err()
			}
			m.Note(fmt.Sprintf("Skipping unsafe/unreadable local OTA candidate %s: %v", filename, err))
			continue
		}
		result, err := findCache(cacheDir, platform, helperHash, hash, "")
		if err == nil {
			m.Note("Reusing saved local OTA and its verified extracted cache; skipping network download")
			m.Note("Local OTA: " + filename)
			describeCache(m, result)
			return result, filename, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return cacheResult{}, "", err
		}
	}
	return cacheResult{}, "", os.ErrNotExist
}
