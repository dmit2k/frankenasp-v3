package main

import (
	"github.com/dmit2k/frankenasp-v3/internal/asp"
	"path/filepath"
)

func cacheDefaultPath() string { return filepath.Join(parentPath(), asp.CacheDirName) }
