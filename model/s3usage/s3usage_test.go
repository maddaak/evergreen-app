package s3usage

import (
	"testing"

	"github.com/evergreen-ci/evergreen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bytesForFile returns the stored bytes for a specific file in a specific bucket, or 0 if not found.
func bytesForFile(metrics []BucketArtifactMetrics, bucket, fileKey string) int64 {
	for _, b := range metrics {
		if b.Bucket == bucket {
			for _, f := range b.Artifacts {
				if f.FileKey == fileKey {
					return f.Bytes
				}
			}
		}
	}
	return 0
}

// hasBucket returns true if the given bucket exists in the metrics slice.
func hasBucket(metrics []BucketArtifactMetrics, bucket string) bool {
	for _, b := range metrics {
		if b.Bucket == bucket {
			return true
		}
	}
	return false
}

func TestS3Usage(t *testing.T) {
	t.Run("IsZero", func(t *testing.T) {
		s3Usage := S3Usage{}
		assert.True(t, s3Usage.IsZero())

		s3Usage.Artifacts.PutRequests = 10
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.Artifacts.UploadBytes = 100
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.Artifacts.Count = 1
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.Logs.PutRequests = 5
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.Logs.UploadBytes = 100
		assert.False(t, s3Usage.IsZero())

	})

	t.Run("IncrementArtifacts", func(t *testing.T) {
		s3Usage := S3Usage{}
		assert.Equal(t, 0, s3Usage.Artifacts.PutRequests)
		assert.Equal(t, int64(0), s3Usage.Artifacts.UploadBytes)
		assert.Equal(t, 0, s3Usage.Artifacts.Count)
		assert.Equal(t, 0, s3Usage.Artifacts.ArtifactWithMaxPutRequests)
		assert.Equal(t, 0, s3Usage.Artifacts.ArtifactWithMinPutRequests)

		filesA := []FileMetrics{
			{RemotePath: "path/file1.txt", FileSizeBytes: 600},
			{RemotePath: "path/file2.txt", FileSizeBytes: 424},
		}
		s3Usage.IncrementArtifacts(ArtifactUpload{PutRequests: 5, UploadBytes: 1024, FileCount: 2, MaxPuts: 3, MinPuts: 2, Bucket: "bucket-a", Artifacts: filesA})
		assert.Equal(t, 5, s3Usage.Artifacts.PutRequests)
		assert.Equal(t, int64(1024), s3Usage.Artifacts.UploadBytes)
		assert.Equal(t, 2, s3Usage.Artifacts.Count)
		assert.Equal(t, 3, s3Usage.Artifacts.ArtifactWithMaxPutRequests)
		assert.Equal(t, 2, s3Usage.Artifacts.ArtifactWithMinPutRequests)
		require.NotEmpty(t, s3Usage.Artifacts.ArtifactsByBucket)
		require.True(t, hasBucket(s3Usage.Artifacts.ArtifactsByBucket, "bucket-a"))
		assert.Equal(t, int64(600), bytesForFile(s3Usage.Artifacts.ArtifactsByBucket, "bucket-a", "path/file1.txt"))
		assert.Equal(t, int64(424), bytesForFile(s3Usage.Artifacts.ArtifactsByBucket, "bucket-a", "path/file2.txt"))

		filesB := []FileMetrics{
			{RemotePath: "other/file3.txt", FileSizeBytes: 2048},
		}
		s3Usage.IncrementArtifacts(ArtifactUpload{PutRequests: 10, UploadBytes: 2048, FileCount: 3, MaxPuts: 8, MinPuts: 1, Bucket: "bucket-b", Artifacts: filesB})
		assert.Equal(t, 15, s3Usage.Artifacts.PutRequests)
		assert.Equal(t, int64(3072), s3Usage.Artifacts.UploadBytes)
		assert.Equal(t, 5, s3Usage.Artifacts.Count)
		assert.Equal(t, 8, s3Usage.Artifacts.ArtifactWithMaxPutRequests)
		assert.Equal(t, 1, s3Usage.Artifacts.ArtifactWithMinPutRequests)
		require.True(t, hasBucket(s3Usage.Artifacts.ArtifactsByBucket, "bucket-b"))
		assert.Equal(t, int64(600), bytesForFile(s3Usage.Artifacts.ArtifactsByBucket, "bucket-a", "path/file1.txt"), "bucket-a file bytes should be unchanged")
		assert.Equal(t, int64(2048), bytesForFile(s3Usage.Artifacts.ArtifactsByBucket, "bucket-b", "other/file3.txt"))

		filesA2 := []FileMetrics{
			{RemotePath: "path/file1.txt", FileSizeBytes: 512},
		}
		s3Usage.IncrementArtifacts(ArtifactUpload{PutRequests: 3, UploadBytes: 512, FileCount: 1, MaxPuts: 3, MinPuts: 3, Bucket: "bucket-a", Artifacts: filesA2})
		assert.Equal(t, int64(1112), bytesForFile(s3Usage.Artifacts.ArtifactsByBucket, "bucket-a", "path/file1.txt"), "bucket-a file bytes should accumulate across invocations")
	})

	t.Run("IncrementLogs", func(t *testing.T) {
		t.Run("AccumulatesPutRequestsAndBytes", func(t *testing.T) {
			s3Usage := S3Usage{}
			s3Usage.IncrementLogs(5, 1024, "", "")
			assert.Equal(t, 5, s3Usage.Logs.PutRequests)
			assert.Equal(t, int64(1024), s3Usage.Logs.UploadBytes)
			s3Usage.IncrementLogs(10, 2048, "", "")
			assert.Equal(t, 15, s3Usage.Logs.PutRequests)
			assert.Equal(t, int64(3072), s3Usage.Logs.UploadBytes)
		})
		t.Run("TracksTaskLogBytesAndKey", func(t *testing.T) {
			s3Usage := S3Usage{}
			s3Usage.IncrementLogs(5, 1024, LogTypeTask, "task_key")
			assert.Equal(t, int64(1024), s3Usage.Logs.Task.Bytes)
			assert.Equal(t, "task_key", s3Usage.Logs.Task.LogKey)
			assert.Equal(t, int64(0), s3Usage.Logs.Agent.Bytes)
			assert.Equal(t, int64(0), s3Usage.Logs.System.Bytes)
		})
		t.Run("TracksAgentLogBytesAndKey", func(t *testing.T) {
			s3Usage := S3Usage{}
			s3Usage.IncrementLogs(3, 512, LogTypeAgent, "agent_key")
			assert.Equal(t, int64(0), s3Usage.Logs.Task.Bytes)
			assert.Equal(t, int64(512), s3Usage.Logs.Agent.Bytes)
			assert.Equal(t, "agent_key", s3Usage.Logs.Agent.LogKey)
			assert.Equal(t, int64(0), s3Usage.Logs.System.Bytes)
		})
		t.Run("TracksSystemLogBytesAndKey", func(t *testing.T) {
			s3Usage := S3Usage{}
			s3Usage.IncrementLogs(2, 256, LogTypeSystem, "system_key")
			assert.Equal(t, int64(0), s3Usage.Logs.Task.Bytes)
			assert.Equal(t, int64(0), s3Usage.Logs.Agent.Bytes)
			assert.Equal(t, int64(256), s3Usage.Logs.System.Bytes)
			assert.Equal(t, "system_key", s3Usage.Logs.System.LogKey)
		})
		t.Run("UnknownLogTypeDoesNotUpdatePerTypeMetrics", func(t *testing.T) {
			s3Usage := S3Usage{}
			s3Usage.IncrementLogs(5, 1024, "unknown_type", "key")
			assert.Equal(t, 5, s3Usage.Logs.PutRequests)
			assert.Equal(t, int64(0), s3Usage.Logs.Task.Bytes)
			assert.Equal(t, int64(0), s3Usage.Logs.Agent.Bytes)
			assert.Equal(t, int64(0), s3Usage.Logs.System.Bytes)
		})
		t.Run("EmptyLogKeyDoesNotOverwriteExistingKey", func(t *testing.T) {
			s3Usage := S3Usage{}
			s3Usage.IncrementLogs(5, 1024, LogTypeTask, "task_key")
			s3Usage.IncrementLogs(5, 512, LogTypeTask, "")
			assert.Equal(t, "task_key", s3Usage.Logs.Task.LogKey)
			assert.Equal(t, int64(1536), s3Usage.Logs.Task.Bytes)
		})
	})

	t.Run("NilReceiverIsZero", func(t *testing.T) {
		var s3Usage *S3Usage
		assert.True(t, s3Usage.IsZero())
	})
}

func TestCalculatePutRequests(t *testing.T) {
	const MB = 1024 * 1024

	t.Run("ZeroOrNegativeSize", func(t *testing.T) {
		assert.Equal(t, 0, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 0))
		assert.Equal(t, 0, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 0))
		assert.Equal(t, 0, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, -100))
		assert.Equal(t, 0, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodWriter, -1*MB))
		assert.Equal(t, 0, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodCopy, -1))
	})

	t.Run("CopyMethod", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodCopy, 1))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodCopy, 1))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodCopy, 1*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodCopy, 100*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodCopy, 1000*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodCopy, 1000*MB))
	})

	t.Run("SmallBucketWriter", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodWriter, 1))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodWriter, 1*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodWriter, 4*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodWriter, 5*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodWriter, 10*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodWriter, 100*MB))
	})

	t.Run("LargeBucketWriter", func(t *testing.T) {
		assert.Equal(t, 3, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodWriter, 1))
		assert.Equal(t, 3, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodWriter, 1*MB))
		assert.Equal(t, 3, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodWriter, 5*MB))
		assert.Equal(t, 4, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodWriter, 10*MB))
		assert.Equal(t, 12, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodWriter, 50*MB))
		assert.Equal(t, 22, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodWriter, 100*MB))
	})

	t.Run("PutMethodSmallBucket", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 1))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 100*1024))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 1*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 4*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 5*MB-1))
		assert.Equal(t, 3, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 5*MB))
		assert.Equal(t, 4, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 5*MB+1))
		assert.Equal(t, 4, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 10*MB))
		assert.Equal(t, 22, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 100*MB))
	})

	t.Run("PutMethodLargeBucket", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 1))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 2*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 4*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 5*MB-1))
		assert.Equal(t, 3, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 5*MB))
		assert.Equal(t, 4, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 5*MB+1))
		assert.Equal(t, 5, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 15*MB))
		assert.Equal(t, 22, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 100*MB))
	})

	t.Run("RealWorldScenarios", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 2*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodWriter, 500*1024))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodCopy, 1000*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 300*1024))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 50*1024))
		assert.Equal(t, 6, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 20*MB))
	})

	t.Run("BoundaryConditions", func(t *testing.T) {
		assert.Equal(t, 3, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 5*MB))
		assert.Equal(t, 3, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 5*MB))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 5*MB-1))
		assert.Equal(t, 1, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 5*MB-1))
		assert.Equal(t, 4, CalculatePutRequests(S3BucketTypeSmall, S3UploadMethodPut, 5*MB+1))
		assert.Equal(t, 4, CalculatePutRequests(S3BucketTypeLarge, S3UploadMethodPut, 5*MB+1))
	})
}

func TestCalculatePutCost(t *testing.T) {
	validConfig := &evergreen.CostConfig{
		S3Cost: evergreen.S3CostConfig{
			Upload: evergreen.S3UploadCostConfig{
				UploadCostDiscount: 0.3,
			},
		},
	}

	t.Run("WithValidConfig", func(t *testing.T) {
		standard, adjusted := CalculatePutCost(1000, validConfig)
		assert.InDelta(t, 0.005, standard, 0.000001)
		assert.InDelta(t, 0.0035, adjusted, 0.000001)
		assert.Greater(t, standard, adjusted)
	})

	t.Run("WithNilConfig", func(t *testing.T) {
		standard, adjusted := CalculatePutCost(1000, nil)
		assert.InDelta(t, 0.005, standard, 0.000001)
		assert.Equal(t, 0.0, adjusted)
	})

	t.Run("WithZeroPutRequests", func(t *testing.T) {
		standard, adjusted := CalculatePutCost(0, validConfig)
		assert.Equal(t, 0.0, standard)
		assert.Equal(t, 0.0, adjusted)
	})

	t.Run("WithNegativePutRequests", func(t *testing.T) {
		standard, adjusted := CalculatePutCost(-5, validConfig)
		assert.Equal(t, 0.0, standard)
		assert.Equal(t, 0.0, adjusted)
	})

	t.Run("WithInvalidDiscount", func(t *testing.T) {
		invalidConfig := &evergreen.CostConfig{
			S3Cost: evergreen.S3CostConfig{
				Upload: evergreen.S3UploadCostConfig{
					UploadCostDiscount: 1.5,
				},
			},
		}
		standard, adjusted := CalculatePutCost(1000, invalidConfig)
		assert.InDelta(t, 0.005, standard, 0.000001)
		assert.Equal(t, 0.0, adjusted)
	})

}

func TestCalculateStorageCost(t *testing.T) {
	validConfig := &evergreen.CostConfig{
		S3Cost: evergreen.S3CostConfig{
			Storage: evergreen.S3StorageCostConfig{
				StandardStorageCostDiscount: 0.37,
				IAStorageCostDiscount:       0.312,
				ArchiveStorageCostDiscount:  0.265,
			},
		},
	}

	const GB = 1024 * 1024 * 1024

	t.Run("DefaultArtifacts365Days", func(t *testing.T) {
		// ExpirationDays=365: Standard=30, IA=60, Archive=275
		standard, adjusted := CalculateStorageCost(t.Context(), GB, 365, validConfig)
		assert.Greater(t, standard, 0.0)
		assert.Greater(t, adjusted, 0.0)
		assert.Greater(t, standard, adjusted)
		stdTier := 30.0 * (0.023 / float64(GB) / 30.0) * (1 - 0.37)
		iaTier := 60.0 * (0.0125 / float64(GB) / 30.0) * (1 - 0.312)
		archiveTier := 275.0 * (0.004 / float64(GB) / 30.0) * (1 - 0.265)
		expectedAdj := float64(GB) * (stdTier + iaTier + archiveTier)
		assert.InDelta(t, expectedAdj, adjusted, 0.000001)
	})

	t.Run("MongoDBMongoArtifacts90Days", func(t *testing.T) {
		// ExpirationDays=90: Standard=30, IA=60, Archive=0
		standard, adjusted := CalculateStorageCost(t.Context(), GB, 90, validConfig)
		assert.Greater(t, standard, adjusted)
		stdTier := 30.0 * (0.023 / float64(GB) / 30.0) * (1 - 0.37)
		iaTier := 60.0 * (0.0125 / float64(GB) / 30.0) * (1 - 0.312)
		expectedAdj := float64(GB) * (stdTier + iaTier)
		assert.InDelta(t, expectedAdj, adjusted, 0.000001)
	})

	t.Run("MongoSyncArtifacts180Days", func(t *testing.T) {
		// ExpirationDays=180: Standard=30, IA=60, Archive=90
		standard, adjusted := CalculateStorageCost(t.Context(), GB, 180, validConfig)
		assert.Greater(t, standard, adjusted)
		stdTier := 30.0 * (0.023 / float64(GB) / 30.0) * (1 - 0.37)
		iaTier := 60.0 * (0.0125 / float64(GB) / 30.0) * (1 - 0.312)
		archiveTier := 90.0 * (0.004 / float64(GB) / 30.0) * (1 - 0.265)
		expectedAdj := float64(GB) * (stdTier + iaTier + archiveTier)
		assert.InDelta(t, expectedAdj, adjusted, 0.000001)
	})

	t.Run("DefaultLog60Days", func(t *testing.T) {
		// ExpirationDays=60: Standard=30, IA=30, Archive=0
		standard, adjusted := CalculateStorageCost(t.Context(), GB, 60, validConfig)
		assert.Greater(t, standard, adjusted)
		stdTier := 30.0 * (0.023 / float64(GB) / 30.0) * (1 - 0.37)
		iaTier := 30.0 * (0.0125 / float64(GB) / 30.0) * (1 - 0.312)
		expectedAdj := float64(GB) * (stdTier + iaTier)
		assert.InDelta(t, expectedAdj, adjusted, 0.000001)
	})

	t.Run("ZeroBytes", func(t *testing.T) {
		standard, adjusted := CalculateStorageCost(t.Context(), 0, 365, validConfig)
		assert.Equal(t, 0.0, standard)
		assert.Equal(t, 0.0, adjusted)
	})

	t.Run("ZeroExpirationDays", func(t *testing.T) {
		standard, adjusted := CalculateStorageCost(t.Context(), GB, 0, validConfig)
		assert.Equal(t, 0.0, standard)
		assert.Equal(t, 0.0, adjusted)
	})

	t.Run("NilConfig", func(t *testing.T) {
		standard, adjusted := CalculateStorageCost(t.Context(), GB, 365, nil)
		assert.Greater(t, standard, 0.0)
		assert.Equal(t, 0.0, adjusted)
	})
}

func TestSetOnDemandArtifactPutCosts(t *testing.T) {
	validConfig := &evergreen.CostConfig{
		S3Cost: evergreen.S3CostConfig{
			Upload: evergreen.S3UploadCostConfig{
				UploadCostDiscount: 0.0,
			},
		},
	}

	t.Run("ZeroArtifactsShouldReturnZero", func(t *testing.T) {
		var costs S3Costs
		setArtifactPutCosts(&costs, S3Usage{}, validConfig)
		assert.Equal(t, 0.0, costs.OnDemandArtifactPutCost)
		assert.Equal(t, 0.0, costs.AdjustedArtifactPutCost)
		assert.Equal(t, 0.0, costs.OnDemandAvgArtifactPutCost)
		assert.Equal(t, 0.0, costs.AdjustedAvgArtifactPutCost)
		assert.Equal(t, 0.0, costs.OnDemandMaxArtifactPutCost)
		assert.Equal(t, 0.0, costs.AdjustedMaxArtifactPutCost)
		assert.Equal(t, 0.0, costs.OnDemandMinArtifactPutCost)
		assert.Equal(t, 0.0, costs.AdjustedMinArtifactPutCost)
	})

	t.Run("SingleFileShouldHaveEqualAvgMinMax", func(t *testing.T) {
		var costs S3Costs
		usage := S3Usage{
			Artifacts: ArtifactMetrics{
				S3UploadMetrics:            S3UploadMetrics{PutRequests: 3},
				Count:                      1,
				ArtifactWithMaxPutRequests: 3,
				ArtifactWithMinPutRequests: 3,
			},
		}
		setArtifactPutCosts(&costs, usage, validConfig)
		assert.Greater(t, costs.OnDemandArtifactPutCost, 0.0)
		assert.Equal(t, costs.OnDemandAvgArtifactPutCost, costs.OnDemandMaxArtifactPutCost)
		assert.Equal(t, costs.OnDemandAvgArtifactPutCost, costs.OnDemandMinArtifactPutCost)
		assert.Equal(t, costs.AdjustedAvgArtifactPutCost, costs.AdjustedMaxArtifactPutCost)
		assert.Equal(t, costs.AdjustedAvgArtifactPutCost, costs.AdjustedMinArtifactPutCost)
	})

	t.Run("MultipleFilesWithDifferentPutsShouldComputeExtremes", func(t *testing.T) {
		var costs S3Costs
		usage := S3Usage{
			Artifacts: ArtifactMetrics{
				S3UploadMetrics:            S3UploadMetrics{PutRequests: 10},
				Count:                      3,
				ArtifactWithMaxPutRequests: 6,
				ArtifactWithMinPutRequests: 1,
			},
		}
		setArtifactPutCosts(&costs, usage, validConfig)
		expectedPutCost := float64(10) * S3PutRequestCost
		assert.InDelta(t, expectedPutCost, costs.OnDemandArtifactPutCost, 0.000001)
		// discount is 0.0, so OnDemand == Adjusted
		assert.InDelta(t, expectedPutCost/3, costs.OnDemandAvgArtifactPutCost, 0.000001)
		assert.InDelta(t, expectedPutCost/3, costs.AdjustedAvgArtifactPutCost, 0.000001)
		assert.Greater(t, costs.OnDemandMaxArtifactPutCost, costs.OnDemandMinArtifactPutCost)
		assert.Greater(t, costs.AdjustedMaxArtifactPutCost, costs.AdjustedMinArtifactPutCost)
		costPerPut := expectedPutCost / 10
		assert.InDelta(t, costPerPut*6, costs.OnDemandMaxArtifactPutCost, 0.000001)
		assert.InDelta(t, costPerPut*1, costs.OnDemandMinArtifactPutCost, 0.000001)
		assert.InDelta(t, costPerPut*6, costs.AdjustedMaxArtifactPutCost, 0.000001)
		assert.InDelta(t, costPerPut*1, costs.AdjustedMinArtifactPutCost, 0.000001)
	})

	t.Run("NilConfigShouldReturnZeroAdjustedCost", func(t *testing.T) {
		var costs S3Costs
		usage := S3Usage{
			Artifacts: ArtifactMetrics{
				S3UploadMetrics: S3UploadMetrics{PutRequests: 10},
				Count:           3,
			},
		}
		setArtifactPutCosts(&costs, usage, nil)
		assert.Greater(t, costs.OnDemandArtifactPutCost, 0.0)
		assert.Equal(t, 0.0, costs.AdjustedArtifactPutCost)
		assert.Greater(t, costs.OnDemandAvgArtifactPutCost, 0.0)
		assert.Equal(t, 0.0, costs.AdjustedAvgArtifactPutCost)
		// ArtifactWithMaxPutRequests and ArtifactWithMinPutRequests are 0 in this case
		assert.Equal(t, 0.0, costs.OnDemandMaxArtifactPutCost)
		assert.Equal(t, 0.0, costs.AdjustedMaxArtifactPutCost)
		assert.Equal(t, 0.0, costs.OnDemandMinArtifactPutCost)
		assert.Equal(t, 0.0, costs.AdjustedMinArtifactPutCost)
	})
}

func TestCalculateAllCosts(t *testing.T) {
	validConfig := &evergreen.CostConfig{
		S3Cost: evergreen.S3CostConfig{
			Upload: evergreen.S3UploadCostConfig{UploadCostDiscount: 0.0},
			Storage: evergreen.S3StorageCostConfig{
				DefaultMaxArtifactExpirationDays: 365,
			},
		},
	}
	const MB = 1024 * 1024

	t.Run("PutCostsCalculatedIndependentlyFromStorageCosts", func(t *testing.T) {
		usage := S3Usage{
			Artifacts: ArtifactMetrics{
				S3UploadMetrics:            S3UploadMetrics{PutRequests: 10, UploadBytes: int64(5 * MB)},
				Count:                      2,
				ArtifactWithMaxPutRequests: 7,
				ArtifactWithMinPutRequests: 3,
			},
			Logs: LogMetrics{S3UploadMetrics: S3UploadMetrics{PutRequests: 5}},
		}
		costs := CalculateAllCosts(t.Context(), usage, nil, validConfig)
		assert.Greater(t, costs.OnDemandArtifactPutCost, 0.0)
		assert.Greater(t, costs.OnDemandLogPutCost, 0.0)
		assert.Greater(t, costs.OnDemandMaxArtifactPutCost, costs.OnDemandMinArtifactPutCost)
		assert.Equal(t, 0.0, costs.OnDemandArtifactStorageCost)
		assert.Equal(t, 0.0, costs.OnDemandLogStorageCost)
	})

	t.Run("StorageCostsCalculatedWithTierInfo", func(t *testing.T) {
		fileKey := "project/task1/0/artifacts/binary.tar.gz"
		usage := S3Usage{
			Artifacts: ArtifactMetrics{
				S3UploadMetrics: S3UploadMetrics{PutRequests: 1},
				Count:           1,
				ArtifactsByBucket: []BucketArtifactMetrics{
					{Bucket: "mciuploads", Artifacts: []ArtifactBytes{{FileKey: fileKey, Bytes: int64(5 * MB)}}},
				},
				ArtifactWithMaxPutRequests: 1,
				ArtifactWithMinPutRequests: 1,
			},
		}
		fileExpiration := map[string]FileExpirationInfo{
			fileKey: {ExpirationDays: 90},
		}
		costs := CalculateAllCosts(t.Context(), usage, fileExpiration, validConfig)
		assert.Greater(t, costs.OnDemandArtifactPutCost, 0.0)
		assert.Greater(t, costs.OnDemandArtifactStorageCost, 0.0)
		assert.Greater(t, costs.AdjustedArtifactStorageCost, 0.0)
	})

	t.Run("NilConfigShouldReturnZeroAdjustedCosts", func(t *testing.T) {
		usage := S3Usage{
			Artifacts: ArtifactMetrics{
				S3UploadMetrics: S3UploadMetrics{PutRequests: 10},
				Count:           1,
			},
		}
		costs := CalculateAllCosts(t.Context(), usage, nil, nil)
		assert.Equal(t, 0.0, costs.AdjustedArtifactPutCost)
		assert.Equal(t, 0.0, costs.AdjustedLogPutCost)
		assert.Equal(t, 0.0, costs.AdjustedArtifactStorageCost)
		assert.Equal(t, 0.0, costs.AdjustedLogStorageCost)
	})
}
