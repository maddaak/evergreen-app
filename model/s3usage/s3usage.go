package s3usage

import (
	"context"
	"math"
	"os"

	"github.com/evergreen-ci/evergreen"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/message"
)

const (
	LogTypeTask   = "task_log"
	LogTypeAgent  = "agent_log"
	LogTypeSystem = "system_log"

	S3PutRequestCost = 0.000005
	S3PartSize       = 5 * 1024 * 1024 // S3 multipart upload threshold

	S3BucketTypeSmall S3BucketType = "small"
	S3BucketTypeLarge S3BucketType = "large"

	S3UploadMethodWriter S3UploadMethod = "writer"
	S3UploadMethodPut    S3UploadMethod = "put"
	S3UploadMethodCopy   S3UploadMethod = "copy"

	// S3 Intelligent Tiering pricing constants and tier transition thresholds.
	// Transition days (30, 90) are defined by AWS S3 Intelligent Tiering:
	// https://aws.amazon.com/s3/storage-classes/intelligent-tiering/
	S3StandardPricePerGBMonth = 0.023
	S3IAPricePerGBMonth       = 0.0125
	S3ArchivePricePerGBMonth  = 0.004
	S3BytesPerGB              = 1024 * 1024 * 1024
	S3DaysPerMonth            = 30.0

	// S3ITDefaultTransitionToIADays and S3ITDefaultTransitionToGlacierDays are the implicit
	// tier transition thresholds for S3 Intelligent Tiering. All Evergreen S3 uploads use
	// the IT storage class, so these defaults always apply regardless of lifecycle rule configuration.
	S3ITDefaultTransitionToIADays      = 30
	S3ITDefaultTransitionToGlacierDays = 90
)

// BucketArtifact is a flat representation of a single artifact with its bucket.
type BucketArtifact struct {
	Bucket  string
	FileKey string
	Bytes   int64
}

// ArtifactUpload holds the metrics for a single artifact upload event.
type ArtifactUpload struct {
	PutRequests int
	UploadBytes int64
	FileCount   int
	MaxPuts     int
	MinPuts     int
	Bucket      string
	Artifacts   []FileMetrics
}

// FileExpirationInfo holds the S3 lifecycle expiration configuration for a bucket rule.
type FileExpirationInfo struct {
	ExpirationDays int
}

// S3Costs holds the calculated S3 cost breakdown for a task.
type S3Costs struct {
	OnDemandArtifactPutCost        float64
	AdjustedArtifactPutCost        float64
	OnDemandLogPutCost             float64
	AdjustedLogPutCost             float64
	OnDemandArtifactStorageCost    float64
	AdjustedArtifactStorageCost    float64
	OnDemandLogStorageCost         float64
	AdjustedLogStorageCost         float64
	OnDemandAvgArtifactPutCost     float64
	AdjustedAvgArtifactPutCost     float64
	OnDemandMaxArtifactPutCost     float64
	AdjustedMaxArtifactPutCost     float64
	OnDemandMinArtifactPutCost     float64
	AdjustedMinArtifactPutCost     float64
	OnDemandAvgArtifactStorageCost float64
	OnDemandMinArtifactStorageCost float64
	OnDemandMaxArtifactStorageCost float64
	AdjustedAvgArtifactStorageCost float64
	AdjustedMinArtifactStorageCost float64
	AdjustedMaxArtifactStorageCost float64
	OnDemandAvgLogStorageCost      float64
	AdjustedAvgLogStorageCost      float64
	OnDemandMinLogStorageCost      float64
	AdjustedMinLogStorageCost      float64
	OnDemandMaxLogStorageCost      float64
	AdjustedMaxLogStorageCost      float64
}

// LogTypeMetrics holds the S3 key and byte count for a single log type.
type LogTypeMetrics struct {
	LogKey string `bson:"log_key,omitempty" json:"log_key,omitempty"`
	Bytes  int64  `bson:"bytes,omitempty" json:"bytes,omitempty"`
}

// LogMetrics tracks log upload metrics broken down by log type.
type LogMetrics struct {
	S3UploadMetrics `bson:",inline"`
	Task            LogTypeMetrics `bson:"task_log,omitempty" json:"task_log,omitempty"`
	Agent           LogTypeMetrics `bson:"agent_log,omitempty" json:"agent_log,omitempty"`
	System          LogTypeMetrics `bson:"system_log,omitempty" json:"system_log,omitempty"`
}

// S3Usage tracks S3 API usage for cost calculation.
type S3Usage struct {
	Artifacts ArtifactMetrics `bson:"artifacts,omitempty" json:"artifacts,omitempty"`
	Logs      LogMetrics      `bson:"logs,omitempty" json:"logs,omitempty"`
}

// S3UploadMetrics tracks common S3 upload metrics shared across upload types.
type S3UploadMetrics struct {
	PutRequests int   `bson:"put_requests,omitempty" json:"put_requests,omitempty"`
	UploadBytes int64 `bson:"upload_bytes,omitempty" json:"upload_bytes,omitempty"`
}

// BucketArtifactMetrics groups per-artifact byte metrics for a single S3 bucket.
type BucketArtifactMetrics struct {
	Bucket    string          `bson:"bucket" json:"bucket"`
	Artifacts []ArtifactBytes `bson:"artifacts" json:"artifacts"`
}

// ArtifactBytes tracks bytes uploaded for a single S3 artifact key.
type ArtifactBytes struct {
	FileKey string `bson:"file_key" json:"file_key"`
	Bytes   int64  `bson:"bytes" json:"bytes"`
}

// ArtifactMetrics tracks artifact upload metrics with an additional file count.
type ArtifactMetrics struct {
	S3UploadMetrics `bson:",inline"`
	// Count is the total number of artifacts uploaded per task.
	Count int `bson:"count,omitempty" json:"count,omitempty"`
	// ArtifactWithMaxPutRequests is the highest PUT request count for a single artifact across all s3.put invocations per task.
	ArtifactWithMaxPutRequests int `bson:"max_put_requests_per_file,omitempty" json:"max_put_requests_per_file,omitempty"`
	// ArtifactWithMinPutRequests is the lowest PUT request count for a single artifact across all s3.put invocations per task.
	ArtifactWithMinPutRequests int `bson:"min_put_requests_per_file,omitempty" json:"min_put_requests_per_file,omitempty"`
	// ArtifactsByBucket groups per-artifact byte metrics by S3 bucket.
	ArtifactsByBucket []BucketArtifactMetrics `bson:"artifacts_by_bucket,omitempty" json:"artifacts_by_bucket,omitempty"`
}

// FileMetrics contains metrics for a single uploaded file.
type FileMetrics struct {
	LocalPath     string
	RemotePath    string
	FileSizeBytes int64
	PutRequests   int
}

type S3BucketType string
type S3UploadMethod string

// CalculateUploadMetrics populates file size and PUT requests for each uploaded file.
// Returns the populated metrics plus aggregate totals.
// If any file stat fails, logs a warning and uses zero values for that file.
func CalculateUploadMetrics(
	logger grip.Journaler,
	files []FileMetrics,
	bucketType S3BucketType,
	method S3UploadMethod,
) (populatedFiles []FileMetrics, totalSize int64, totalPuts int) {
	populatedFiles = make([]FileMetrics, len(files))

	for i, file := range files {
		fileInfo, err := os.Stat(file.LocalPath)
		if err != nil {
			logger.Warningf(context.Background(), "Unable to calculate file size and PUT requests for '%s' after successful upload: %s. Using zero values for metadata.", file.LocalPath, err)
			populatedFiles[i] = FileMetrics{
				LocalPath:     file.LocalPath,
				RemotePath:    file.RemotePath,
				FileSizeBytes: 0,
				PutRequests:   0,
			}
			continue
		}

		fileSize := fileInfo.Size()
		putRequests := CalculatePutRequests(bucketType, method, fileSize)

		populatedFiles[i] = FileMetrics{
			LocalPath:     file.LocalPath,
			RemotePath:    file.RemotePath,
			FileSizeBytes: fileSize,
			PutRequests:   putRequests,
		}

		totalSize += fileSize
		totalPuts += putRequests
	}

	return populatedFiles, totalSize, totalPuts
}

// CalculatePutRequests returns the number of S3 PUT API calls
// needed to upload a file based on bucket type, upload method, and file size.
func CalculatePutRequests(bucketType S3BucketType, method S3UploadMethod, fileSize int64) int {
	if fileSize <= 0 {
		return 0
	}

	switch method {
	case S3UploadMethodCopy:
		return 1

	case S3UploadMethodWriter:
		if bucketType == S3BucketTypeSmall {
			return 1
		}
		// Large bucket Writer uses multipart for all sizes, <= 5MB is simple multipart (3 PUTs)
		if fileSize <= S3PartSize {
			return 3
		}
		numParts := int((fileSize + S3PartSize - 1) / S3PartSize)
		return 1 + numParts + 1

	case S3UploadMethodPut:
		// AWS SDK uses single PUT for < 5MB, multipart for >= 5MB
		if fileSize < S3PartSize {
			return 1
		}
		numParts := int((fileSize + S3PartSize - 1) / S3PartSize)
		return 1 + numParts + 1

	default:
		return 0
	}
}

// CalculateAllCosts calculates all S3 PUT and storage costs for the given usage.
func CalculateAllCosts(ctx context.Context, s3usage S3Usage, fileExpiration map[string]FileExpirationInfo, costConfig *evergreen.CostConfig) S3Costs {
	var s3costs S3Costs
	setArtifactPutCosts(&s3costs, s3usage, costConfig)
	setLogPutCosts(&s3costs, s3usage, costConfig)
	setArtifactStorageCosts(ctx, &s3costs, s3usage, fileExpiration, costConfig)
	setLogStorageCosts(ctx, &s3costs, s3usage, fileExpiration, costConfig)
	return s3costs
}

func setArtifactPutCosts(s3costs *S3Costs, s3usage S3Usage, costConfig *evergreen.CostConfig) {
	s3costs.OnDemandArtifactPutCost, s3costs.AdjustedArtifactPutCost = CalculatePutCost(s3usage.Artifacts.PutRequests, costConfig)
	if s3usage.Artifacts.Count > 0 && s3usage.Artifacts.PutRequests > 0 {
		onDemandCostPerPut := s3costs.OnDemandArtifactPutCost / float64(s3usage.Artifacts.PutRequests)
		adjustedCostPerPut := s3costs.AdjustedArtifactPutCost / float64(s3usage.Artifacts.PutRequests)
		s3costs.OnDemandAvgArtifactPutCost = s3costs.OnDemandArtifactPutCost / float64(s3usage.Artifacts.Count)
		s3costs.AdjustedAvgArtifactPutCost = s3costs.AdjustedArtifactPutCost / float64(s3usage.Artifacts.Count)
		s3costs.OnDemandMaxArtifactPutCost = onDemandCostPerPut * float64(s3usage.Artifacts.ArtifactWithMaxPutRequests)
		s3costs.AdjustedMaxArtifactPutCost = adjustedCostPerPut * float64(s3usage.Artifacts.ArtifactWithMaxPutRequests)
		s3costs.OnDemandMinArtifactPutCost = onDemandCostPerPut * float64(s3usage.Artifacts.ArtifactWithMinPutRequests)
		s3costs.AdjustedMinArtifactPutCost = adjustedCostPerPut * float64(s3usage.Artifacts.ArtifactWithMinPutRequests)
	}
}

func setLogPutCosts(s3costs *S3Costs, s3usage S3Usage, costConfig *evergreen.CostConfig) {
	s3costs.OnDemandLogPutCost, s3costs.AdjustedLogPutCost = CalculatePutCost(s3usage.Logs.PutRequests, costConfig)
}

// CalculatePutCost calculates the S3 PUT request cost, returning both the standard
// (non-discounted) and adjusted (discounted) values. If config is nil or the discount is invalid,
// adjusted is returned as 0.
func CalculatePutCost(putRequests int, costConfig *evergreen.CostConfig) (standard, adjusted float64) {
	if putRequests <= 0 {
		return 0.0, 0.0
	}

	standard = float64(putRequests) * S3PutRequestCost

	if costConfig == nil {
		grip.Warning(context.Background(), message.Fields{
			"message": "cost config is not available to calculate S3 PUT cost",
		})
		return standard, 0.0
	}

	discount := costConfig.S3Cost.Upload.UploadCostDiscount
	if discount < 0.0 || discount > 1.0 {
		grip.Warning(context.Background(), message.Fields{
			"message":  "invalid S3 upload cost discount",
			"discount": discount,
		})
		return standard, 0.0
	}

	adjusted = standard * (1 - discount)
	return standard, adjusted
}

func setArtifactStorageCosts(ctx context.Context, s3costs *S3Costs, s3usage S3Usage, fileExpiration map[string]FileExpirationInfo, costConfig *evergreen.CostConfig) {
	s3costs.OnDemandMinArtifactStorageCost = math.MaxFloat64
	s3costs.OnDemandMaxArtifactStorageCost = -math.MaxFloat64
	s3costs.AdjustedMinArtifactStorageCost = math.MaxFloat64
	s3costs.AdjustedMaxArtifactStorageCost = -math.MaxFloat64
	for _, file := range s3usage.Artifacts.AllArtifacts() {
		standardCost, adjustedCost := CalculateStorageCost(ctx, file.Bytes, fileExpiration[file.FileKey].ExpirationDays, costConfig)
		s3costs.OnDemandArtifactStorageCost += standardCost
		s3costs.AdjustedArtifactStorageCost += adjustedCost
		if standardCost < s3costs.OnDemandMinArtifactStorageCost {
			s3costs.OnDemandMinArtifactStorageCost = standardCost
		}
		if standardCost > s3costs.OnDemandMaxArtifactStorageCost {
			s3costs.OnDemandMaxArtifactStorageCost = standardCost
		}
		if adjustedCost < s3costs.AdjustedMinArtifactStorageCost {
			s3costs.AdjustedMinArtifactStorageCost = adjustedCost
		}
		if adjustedCost > s3costs.AdjustedMaxArtifactStorageCost {
			s3costs.AdjustedMaxArtifactStorageCost = adjustedCost
		}
	}
	if s3usage.Artifacts.Count == 0 {
		s3costs.OnDemandMinArtifactStorageCost = 0
		s3costs.OnDemandMaxArtifactStorageCost = 0
		s3costs.AdjustedMinArtifactStorageCost = 0
		s3costs.AdjustedMaxArtifactStorageCost = 0
	} else {
		s3costs.OnDemandAvgArtifactStorageCost = s3costs.OnDemandArtifactStorageCost / float64(s3usage.Artifacts.Count)
		s3costs.AdjustedAvgArtifactStorageCost = s3costs.AdjustedArtifactStorageCost / float64(s3usage.Artifacts.Count)
	}
}

func setLogStorageCosts(ctx context.Context, s3costs *S3Costs, s3usage S3Usage, fileExpiration map[string]FileExpirationInfo, costConfig *evergreen.CostConfig) {
	s3costs.OnDemandMinLogStorageCost = math.MaxFloat64
	s3costs.OnDemandMaxLogStorageCost = -math.MaxFloat64
	s3costs.AdjustedMinLogStorageCost = math.MaxFloat64
	s3costs.AdjustedMaxLogStorageCost = -math.MaxFloat64
	count := 0
	for _, logMetrics := range []LogTypeMetrics{s3usage.Logs.Task, s3usage.Logs.Agent, s3usage.Logs.System} {
		if logMetrics.LogKey == "" {
			continue
		}
		standardCost, adjustedCost := CalculateStorageCost(ctx, logMetrics.Bytes, fileExpiration[logMetrics.LogKey].ExpirationDays, costConfig)
		s3costs.OnDemandLogStorageCost += standardCost
		s3costs.AdjustedLogStorageCost += adjustedCost
		if standardCost < s3costs.OnDemandMinLogStorageCost {
			s3costs.OnDemandMinLogStorageCost = standardCost
		}
		if standardCost > s3costs.OnDemandMaxLogStorageCost {
			s3costs.OnDemandMaxLogStorageCost = standardCost
		}
		if adjustedCost < s3costs.AdjustedMinLogStorageCost {
			s3costs.AdjustedMinLogStorageCost = adjustedCost
		}
		if adjustedCost > s3costs.AdjustedMaxLogStorageCost {
			s3costs.AdjustedMaxLogStorageCost = adjustedCost
		}
		count++
	}
	if count == 0 {
		s3costs.OnDemandMinLogStorageCost = 0
		s3costs.OnDemandMaxLogStorageCost = 0
		s3costs.AdjustedMinLogStorageCost = 0
		s3costs.AdjustedMaxLogStorageCost = 0
	} else {
		s3costs.OnDemandAvgLogStorageCost = s3costs.OnDemandLogStorageCost / float64(count)
		s3costs.AdjustedAvgLogStorageCost = s3costs.AdjustedLogStorageCost / float64(count)
	}
}

// CalculateStorageCost calculates the S3 storage cost for uploadBytes using the S3 Intelligent
// Tiering transition schedule. Returns 0 if expirationDays is non-positive — buckets without a
// lifecycle expiration policy have no defined retention period. If costConfig is nil, adjusted is 0.
func CalculateStorageCost(ctx context.Context, uploadBytes int64, expirationDays int, costConfig *evergreen.CostConfig) (standard, adjusted float64) {
	if uploadBytes <= 0 || expirationDays <= 0 {
		return 0.0, 0.0
	}

	daysInStandard, daysInIA, daysInArchive := storageTierDays(expirationDays)

	pricePerBytePerDay := func(pricePerGBMonth float64) float64 {
		return pricePerGBMonth / S3BytesPerGB / S3DaysPerMonth
	}

	standardTierCost := float64(daysInStandard) * pricePerBytePerDay(S3StandardPricePerGBMonth)
	iaTierCost := float64(daysInIA) * pricePerBytePerDay(S3IAPricePerGBMonth)
	archiveTierCost := float64(daysInArchive) * pricePerBytePerDay(S3ArchivePricePerGBMonth)
	standardCostPerByte := standardTierCost + iaTierCost + archiveTierCost
	standard = float64(uploadBytes) * standardCostPerByte

	if costConfig == nil {
		grip.Warning(ctx, message.Fields{
			"message": "cost config is not available to calculate S3 storage cost",
		})
		return standard, 0.0
	}

	standardDiscount := costConfig.S3Cost.Storage.StandardStorageCostDiscount
	iaDiscount := costConfig.S3Cost.Storage.IAStorageCostDiscount
	archiveDiscount := costConfig.S3Cost.Storage.ArchiveStorageCostDiscount

	adjustedStandardTierCost := standardTierCost * (1 - standardDiscount)
	adjustedIATierCost := iaTierCost * (1 - iaDiscount)
	adjustedArchiveTierCost := archiveTierCost * (1 - archiveDiscount)
	adjustedCostPerByte := adjustedStandardTierCost + adjustedIATierCost + adjustedArchiveTierCost
	adjusted = float64(uploadBytes) * adjustedCostPerByte

	return standard, adjusted
}

// storageTierDays returns how many days an object spends in each S3 storage tier
// based on the S3 Intelligent Tiering transition schedule.
func storageTierDays(expirationDays int) (daysInStandard, daysInIA, daysInArchive int) {
	daysInStandard = min(expirationDays, S3ITDefaultTransitionToIADays)
	daysInIA = max(0, min(expirationDays, S3ITDefaultTransitionToGlacierDays)-S3ITDefaultTransitionToIADays)
	daysInArchive = max(0, expirationDays-S3ITDefaultTransitionToGlacierDays)
	return daysInStandard, daysInIA, daysInArchive
}

// IncrementArtifacts updates aggregate artifact upload metrics after a s3.put command.
func (s *S3Usage) IncrementArtifacts(upload ArtifactUpload) {
	s.Artifacts.PutRequests += upload.PutRequests
	s.Artifacts.UploadBytes += upload.UploadBytes
	s.Artifacts.Count += upload.FileCount

	if upload.MaxPuts > s.Artifacts.ArtifactWithMaxPutRequests {
		s.Artifacts.ArtifactWithMaxPutRequests = upload.MaxPuts
	}
	if s.Artifacts.ArtifactWithMinPutRequests == 0 || upload.MinPuts < s.Artifacts.ArtifactWithMinPutRequests {
		s.Artifacts.ArtifactWithMinPutRequests = upload.MinPuts
	}

	for _, f := range upload.Artifacts {
		s.Artifacts.trackFileUploadMetrics(upload.Bucket, f.RemotePath, f.FileSizeBytes)
	}
}

// trackFileUploadMetrics records the bytes uploaded for a single artifact, organized by bucket and file path.
func (a *ArtifactMetrics) trackFileUploadMetrics(bucket, filePath string, bytes int64) {
	var bucketEntry *BucketArtifactMetrics
	for i := range a.ArtifactsByBucket {
		if a.ArtifactsByBucket[i].Bucket == bucket {
			bucketEntry = &a.ArtifactsByBucket[i]
			break
		}
	}
	if bucketEntry == nil {
		a.ArtifactsByBucket = append(a.ArtifactsByBucket, BucketArtifactMetrics{Bucket: bucket})
		bucketEntry = &a.ArtifactsByBucket[len(a.ArtifactsByBucket)-1]
	}

	for j := range bucketEntry.Artifacts {
		if bucketEntry.Artifacts[j].FileKey == filePath {
			bucketEntry.Artifacts[j].Bytes += bytes
			return
		}
	}
	bucketEntry.Artifacts = append(bucketEntry.Artifacts, ArtifactBytes{FileKey: filePath, Bytes: bytes})
}

// IncrementLogs increments log upload metrics and accumulates per-type bytes for storage cost tracking.
func (s *S3Usage) IncrementLogs(putRequests int, uploadBytes int64, logType, logKey string) {
	s.Logs.PutRequests += putRequests
	s.Logs.UploadBytes += uploadBytes

	var ltm *LogTypeMetrics
	switch logType {
	case LogTypeTask:
		ltm = &s.Logs.Task
	case LogTypeAgent:
		ltm = &s.Logs.Agent
	case LogTypeSystem:
		ltm = &s.Logs.System
	}
	if ltm != nil {
		ltm.Bytes += uploadBytes
		if logKey != "" {
			ltm.LogKey = logKey
		}
	}
}

// AllArtifacts returns a flat list of all artifacts across all buckets.
func (a *ArtifactMetrics) AllArtifacts() []BucketArtifact {
	var artifacts []BucketArtifact
	for _, b := range a.ArtifactsByBucket {
		for _, f := range b.Artifacts {
			artifacts = append(artifacts, BucketArtifact{Bucket: b.Bucket, FileKey: f.FileKey, Bytes: f.Bytes})
		}
	}
	return artifacts
}

// IsZero implements bsoncodec.Zeroer for BSON marshalling.
func (s *S3Usage) IsZero() bool {
	if s == nil {
		return true
	}
	return s.Artifacts.PutRequests == 0 && s.Artifacts.UploadBytes == 0 && s.Artifacts.Count == 0 &&
		s.Artifacts.ArtifactWithMaxPutRequests == 0 && s.Artifacts.ArtifactWithMinPutRequests == 0 &&
		s.Logs.PutRequests == 0 && s.Logs.UploadBytes == 0
}
