package asp

// Exported compatibility API for the application boundary.
var ArchiveExisting = archiveExisting
var AttachOriginalOTAName = attachOriginalOTAName

const BlockSize = blockSize

var BuildPropCodename = buildPropCodename

const CacheDirName = cacheDirName

var CacheFilename = cacheFilename
var CacheForURLFilename = cacheForURLFilename
var CacheInventory = cacheInventory
var CacheLock = cacheLock
var CacheMatchesName = cacheMatchesName

type CacheMetadata = cacheMetadata

var CacheName = cacheName

type CacheResult = cacheResult

const CacheSchema = cacheSchema

var CachedLocalOTAForURL = cachedLocalOTAForURL

var ComposeSpacePlan = composeSpacePlan
var DiskAt = diskAt

type DiskNeed = diskNeed
type DiskSnapshot = diskSnapshot

var DisplayPlatform = displayPlatform
var Download = download

type DownloadBudget = downloadBudget

var DownloadWithBudget = downloadWithBudget
var DownloadWithReserve = downloadWithReserve
var EnsureOTADir = ensureOTADir
var EvaluateSpace = evaluateSpace

type ExtractionStatus = extractionStatus
type Files = files

var FindCache = findCache
var FireOSForCache = fireOSForCache
var FireOSFromOTA = fireOSFromOTA
var FirmwareLabel = firmwareLabel
var HashOTA = hashOTA
var HashString = hashString

type Image = image
type ImageStatus = imageStatus

var Invalid = invalid
var LoadExtracted = loadExtracted
var LocalOTACandidateName = localOTACandidateName

var MbCeil = mbCeil
var MetadataValue = metadataValue
var MigrateCacheNames = migrateCacheNames
var NewMeter = newMeter
var NewZipTree = newZipTree

const OtaImageDir = otaImageDir

var ParseFireOSBuildProp = parseFireOSBuildProp
var PendingOTAForURL = pendingOTAForURL

type ProgressMeter = progressMeter

var PublishCache = publishCache
var ReadCache = readCache
var ReadSystemBuildProp = readSystemBuildProp
var ReadZip = readZip
var RequireCacheDir = requireCacheDir
var RequireSpace = requireSpace
var ResolveCodename = resolveCodename
var ResolveOTASearchDir = resolveOTASearchDir
var RoundAlloc = roundAlloc

const RuntimeCapacityBound = runtimeCapacityBound

var SafeOTAName = safeOTAName
var SafePath = safePath
var SaveOTAArchive = saveOTAArchive

type Selective = selective

var SourceCodename = sourceCodename

const SpaceReserve = spaceReserve
const StagingOverhead = stagingOverhead

var SumSpace = sumSpace

var ValidateHelperRuntime = validateHelperRuntime

var SelectFirmwareCache = selectFirmwareCache
