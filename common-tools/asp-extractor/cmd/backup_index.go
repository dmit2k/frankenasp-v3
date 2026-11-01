package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Backup links are desktop-only bookkeeping outside asp-installer-cache;
// nothing is added to or changed inside the embedded cache schema.
type backupLink struct {
	Platform     string `json:"platform"`
	Firmware     string `json:"firmware"`
	CacheName    string `json:"cache_name"`
	Path         string `json:"path"`
	SourceSHA256 string `json:"source_sha256"`
}

func backupIndexPath(cacheDir string) string {
	return filepath.Join(filepath.Dir(cacheDir), "asp-extractor-backups.json")
}
func readBackupLinks(cacheDir string) ([]backupLink, error) {
	p := backupIndexPath(cacheDir)
	st, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > 1<<20 || st.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("unsafe backup source index: %s", p)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var v []backupLink
	if err = json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("invalid backup source index: %w", err)
	}
	if len(v) > 1000 {
		return nil, errors.New("too many backup source links")
	}
	return v, nil
}
func recordBackupLink(o options, c cacheResult) error {
	sourcePath, err := filepath.Abs(o.backup)
	if err != nil {
		return err
	}
	links, err := readBackupLinks(o.cacheDir)
	if err != nil {
		return err
	}
	newItem := backupLink{Platform: o.platform, Firmware: c.Metadata.Firmware, CacheName: filepath.Base(c.Path), Path: sourcePath, SourceSHA256: c.Metadata.SourceSHA256}
	kept := make([]backupLink, 0, len(links)+1)
	for _, b := range links {
		if b.Platform == newItem.Platform && b.CacheName == newItem.CacheName {
			continue
		}
		kept = append(kept, b)
	}
	kept = append(kept, newItem)
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].Platform != kept[j].Platform {
			return kept[i].Platform < kept[j].Platform
		}
		return kept[i].CacheName < kept[j].CacheName
	})
	raw, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(o.cacheDir)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".asp-backup-index-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), backupIndexPath(o.cacheDir)); err != nil {
		return err
	}
	return nil
}
func printBackupLinks(o options) error {
	links, err := readBackupLinks(o.cacheDir)
	if err != nil {
		return err
	}
	for _, link := range links {
		if o.platform != "" && o.platform != link.Platform {
			continue
		}
		info, err := os.Stat(link.Path)
		availability := "unavailable"
		if err == nil && info.IsDir() {
			availability = "present"
		}
		fmt.Printf("BACKUP %s firmware=%s cache=%s source=%s (%s)\n", link.Platform, link.Firmware, link.CacheName, link.Path, availability)
	}
	return nil
}
