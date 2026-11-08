package main

import (
	"archive/zip"
	"errors"
	"github.com/dmit2k/frankenasp-v3/internal/asp"
	"os"
)

func detectBackupPlatform(dir string) (string, error) {
	src, handles, err := asp.OpenBackupImages(dir)
	if err != nil {
		return "", err
	}
	defer asp.CloseImages(handles)
	prop, err := asp.ReadSystemBuildProp(src)
	if err != nil {
		return "", err
	}
	return asp.BuildPropCodename(prop)
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
	if errors.Is(err, zip.ErrFormat) {
		// Standalone full CrAU payloads expose their identity in system/build.prop.
		var magic [4]byte
		if _, readErr := f.ReadAt(magic[:], 0); readErr == nil && string(magic[:]) == "CrAU" {
			return "", nil
		}
	}
	if err != nil {
		return "", err
	}
	if len(z.File) > 100000 {
		return "", errors.New("OTA has too many files")
	}
	for _, file := range z.File {
		if file.Name != "META-INF/com/android/metadata" {
			continue
		}
		raw, err := asp.ReadZip(file, 65536)
		if err != nil {
			return "", err
		}
		return sourceCodename(asp.MetadataValue(raw, "post-device"), asp.MetadataValue(raw, "post-build"))
	}
	return "", nil
}
