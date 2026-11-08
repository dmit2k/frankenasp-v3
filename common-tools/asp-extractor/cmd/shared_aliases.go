package main

import (
	"context"
	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

var cacheName = asp.CacheName

type cacheResult = asp.CacheResult
type cacheMetadata = asp.CacheMetadata

type files = asp.Files

var invalid = asp.Invalid

var publishCache = asp.PublishCache
var resolveCodename = asp.ResolveCodename

var sourceCodename = asp.SourceCodename

var validateHelperRuntime = asp.ValidateHelperRuntime

func loadExtracted(ctx context.Context, cacheDir, platform, sourcePath, sourceURL, firmware, otaSearchDir, cmd string, noBackup, refresh, deleteOTA bool, m *asp.ProgressMeter) (cacheResult, string, func(), error) {
	return asp.LoadExtracted(ctx, cacheDir, platform, sourcePath, sourceURL, firmware, otaSearchDir, cmd, noBackup, refresh, deleteOTA, m, asp.PreflightGenericDesktop)
}
