package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func detectBackupPlatform(dir string) (string, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", errors.New("--backup-dir must name a directory")
	}
	regular := func(name string) (bool, error) {
		st, err := os.Lstat(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !st.Mode().IsRegular() {
			return false, fmt.Errorf("backup image %s is not a regular file", name)
		}
		return true, nil
	}
	system, err := regular("system.img")
	if err != nil {
		return "", err
	}
	p21, err := regular("p21-system.img")
	if err != nil {
		return "", err
	}
	p12, err := regular("p12-vendor.img")
	if err != nil {
		return "", err
	}
	if system && !p21 && !p12 {
		return "rook", nil
	}
	if !system && p21 && p12 {
		return "athena", nil
	}
	return "", errors.New("unknown or ambiguous backup layout: expected system.img (rook), or p21-system.img and p12-vendor.img (athena)")
}

func detectOTAPlatform(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > 2<<30 {
		return "", errors.New("OTA must be a regular file <=2GiB")
	}
	z, err := zip.NewReader(f, st.Size())
	if err != nil {
		return "", err
	}
	if len(z.File) > 100000 {
		return "", errors.New("OTA has too many files")
	}
	var meta *zip.File
	hasVendorPartition := false
	for _, file := range z.File {
		if file.Name == "META-INF/com/android/metadata" {
			meta = file
		}
		if file.Name == "vendor.transfer.list" || strings.HasPrefix(file.Name, "vendor/") {
			hasVendorPartition = true
		}
	}
	if meta == nil || meta.UncompressedSize64 > 65536 {
		return "", errors.New("OTA metadata missing or oversized")
	}
	raw, err := readZip(meta, 65536)
	if err != nil {
		return "", err
	}
	fingerprint := metadataValue(raw, "post-build")
	platform := ""
	if strings.Contains(fingerprint, "/athena/") {
		platform = "athena"
	}
	if strings.Contains(fingerprint, "/rook/") {
		platform = "rook"
	}
	if platform == "" {
		return "", fmt.Errorf("unrecognized OTA platform in post-build fingerprint %q; supply a supported full OTA", fingerprint)
	}
	if (platform == "athena") != hasVendorPartition {
		return "", fmt.Errorf("OTA partition layout conflicts with fingerprint platform %s", platform)
	}
	return platform, nil
}
