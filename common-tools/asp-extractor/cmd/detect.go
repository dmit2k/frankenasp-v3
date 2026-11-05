package main

import (
	"archive/zip"
	"errors"
	"os"
)

func detectBackupPlatform(dir string) (string, error) {
	src, handles, err := openBackupImages(dir)
	if err != nil {
		return "", err
	}
	defer closeImages(handles)
	prop, err := readSystemBuildProp(src)
	if err != nil {
		return "", err
	}
	return buildPropCodename(prop)
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
	for _, file := range z.File {
		if file.Name != "META-INF/com/android/metadata" {
			continue
		}
		raw, err := readZip(file, 65536)
		if err != nil {
			return "", err
		}
		return sourceCodename(metadataValue(raw, "post-device"), metadataValue(raw, "post-build"))
	}
	return "", nil
}
