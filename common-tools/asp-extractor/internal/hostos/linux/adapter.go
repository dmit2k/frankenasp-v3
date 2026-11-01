package linux

import (
	"errors"
	"os"
	"path/filepath"
)

type Adapter struct{}

func (Adapter) DefaultStore() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		base = filepath.Join(home, ".cache")
	}
	if !filepath.IsAbs(base) {
		return "", errors.New("XDG_CACHE_HOME must be absolute")
	}
	return filepath.Join(base, "frankenasp", "asp-extractor"), nil
}
