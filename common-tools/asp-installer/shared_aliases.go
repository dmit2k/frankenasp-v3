package main

import (
	"context"
	"github.com/dmit2k/frankenasp-v3/internal/asp"
	"github.com/dmit2k/frankenasp-v3/platforms"
)

var archiveExisting = asp.ArchiveExisting
var attachOriginalOTAName = asp.AttachOriginalOTAName

const blockSize = asp.BlockSize

var cacheFilename = asp.CacheFilename
var cacheForURLFilename = asp.CacheForURLFilename
var cacheInventory = asp.CacheInventory
var cacheLock = asp.CacheLock
var cacheMatchesName = asp.CacheMatchesName

type cacheMetadata = asp.CacheMetadata

type cacheResult = asp.CacheResult

var cachedLocalOTAForURL = asp.CachedLocalOTAForURL

var composeSpacePlan = asp.ComposeSpacePlan
var diskAt = asp.DiskAt

type diskNeed = asp.DiskNeed
type diskSnapshot = asp.DiskSnapshot

var displayPlatform = asp.DisplayPlatform
var download = asp.Download

type downloadBudget = asp.DownloadBudget

var downloadWithBudget = asp.DownloadWithBudget
var downloadWithReserve = asp.DownloadWithReserve
var ensureOTADir = asp.EnsureOTADir
var evaluateSpace = asp.EvaluateSpace

type extractionStatus = asp.ExtractionStatus
type files = asp.Files

var findCache = asp.FindCache
var fireOSFromOTA = asp.FireOSFromOTA
var firmwareLabel = asp.FirmwareLabel
var hashOTA = asp.HashOTA
var hashString = asp.HashString

type image = asp.Image
type imageStatus = asp.ImageStatus

var invalid = asp.Invalid
var localOTACandidateName = asp.LocalOTACandidateName

var migrateCacheNames = asp.MigrateCacheNames
var newMeter = asp.NewMeter
var newZipTree = asp.NewZipTree

const otaImageDir = asp.OtaImageDir

var parseFireOSBuildProp = asp.ParseFireOSBuildProp
var pendingOTAForURL = asp.PendingOTAForURL

type progressMeter = asp.ProgressMeter

var publishCache = asp.PublishCache
var readCache = asp.ReadCache
var requireCacheDir = asp.RequireCacheDir
var resolveCodename = asp.ResolveCodename
var resolveOTASearchDir = asp.ResolveOTASearchDir

const runtimeCapacityBound = asp.RuntimeCapacityBound

var safeOTAName = asp.SafeOTAName
var saveOTAArchive = asp.SaveOTAArchive
var selectFirmwareCache = asp.SelectFirmwareCache

type selective = asp.Selective

const spaceReserve = asp.SpaceReserve

var sumSpace = asp.SumSpace

func loadExtracted(ctx context.Context, cacheDir, platform, sourcePath, sourceURL, firmware, otaSearchDir, cmd string, noBackup, refresh, deleteOTA bool, m *progressMeter) (cacheResult, string, func(), error) {
	return asp.LoadExtracted(ctx, cacheDir, platform, sourcePath, sourceURL, firmware, otaSearchDir, cmd, noBackup, refresh, deleteOTA, m, preflightCacheAndOTA)
}

var validateChannelMap = platforms.ValidateChannelMap
