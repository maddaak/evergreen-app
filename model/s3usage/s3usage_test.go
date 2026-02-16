package s3usage

import (
	"testing"

	"github.com/evergreen-ci/evergreen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestS3Usage(t *testing.T) {
	t.Run("IsZero", func(t *testing.T) {
		s3Usage := S3Usage{}
		assert.True(t, s3Usage.IsZero())

		s3Usage.UserFiles.PutRequests = 10
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.UserFiles.UploadBytes = 100
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.UserFiles.FileCount = 1
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.LogFiles.PutRequests = 5
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.LogFiles.UploadBytesUncompressed = 1024
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.LogFiles.LogChunksUploaded = 3
		assert.False(t, s3Usage.IsZero())

		s3Usage = S3Usage{}
		s3Usage.UserFiles.PutCost = 0.005
		assert.False(t, s3Usage.IsZero())
	})

	t.Run("IncrementUserFiles", func(t *testing.T) {
		s3Usage := S3Usage{}
		assert.Equal(t, 0, s3Usage.UserFiles.PutRequests)
		assert.Equal(t, int64(0), s3Usage.UserFiles.UploadBytes)
		assert.Equal(t, 0, s3Usage.UserFiles.FileCount)

		s3Usage.IncrementUserFiles(5, 1024, 2)
		assert.Equal(t, 5, s3Usage.UserFiles.PutRequests)
		assert.Equal(t, int64(1024), s3Usage.UserFiles.UploadBytes)
		assert.Equal(t, 2, s3Usage.UserFiles.FileCount)

		s3Usage.IncrementUserFiles(10, 2048, 3)
		assert.Equal(t, 15, s3Usage.UserFiles.PutRequests)
		assert.Equal(t, int64(3072), s3Usage.UserFiles.UploadBytes)
		assert.Equal(t, 5, s3Usage.UserFiles.FileCount)
	})

	t.Run("IncrementLogFiles", func(t *testing.T) {
		s3Usage := S3Usage{}
		assert.Equal(t, 0, s3Usage.LogFiles.PutRequests)
		assert.Equal(t, int64(0), s3Usage.LogFiles.UploadBytesUncompressed)
		assert.Equal(t, 0, s3Usage.LogFiles.LogChunksUploaded)

		s3Usage.IncrementLogFiles(1, 5000, 1)
		assert.Equal(t, 1, s3Usage.LogFiles.PutRequests)
		assert.Equal(t, int64(5000), s3Usage.LogFiles.UploadBytesUncompressed)
		assert.Equal(t, 1, s3Usage.LogFiles.LogChunksUploaded)

		s3Usage.IncrementLogFiles(1, 3000, 1)
		assert.Equal(t, 2, s3Usage.LogFiles.PutRequests)
		assert.Equal(t, int64(8000), s3Usage.LogFiles.UploadBytesUncompressed)
		assert.Equal(t, 2, s3Usage.LogFiles.LogChunksUploaded)
	})
}

func TestCalculateS3PutCostWithConfig(t *testing.T) {
	validConfig := &evergreen.CostConfig{
		S3Cost: evergreen.S3CostConfig{
			Upload: evergreen.S3UploadCostConfig{
				UploadCostDiscount: 0.3,
			},
		},
	}

	invalidHighDiscountConfig := &evergreen.CostConfig{
		S3Cost: evergreen.S3CostConfig{
			Upload: evergreen.S3UploadCostConfig{
				UploadCostDiscount: 1.5,
			},
		},
	}

	invalidNegativeDiscountConfig := &evergreen.CostConfig{
		S3Cost: evergreen.S3CostConfig{
			Upload: evergreen.S3UploadCostConfig{
				UploadCostDiscount: -0.5,
			},
		},
	}

	noDiscountConfig := &evergreen.CostConfig{}

	t.Run("WithValidConfig", func(t *testing.T) {
		cost, err := CalculateS3PutCostWithConfig(1000, validConfig)
		require.NoError(t, err)
		assert.InDelta(t, 0.0035, cost, 0.000001)
	})

	t.Run("WithNilConfig", func(t *testing.T) {
		cost, err := CalculateS3PutCostWithConfig(1000, nil)
		require.NoError(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithZeroRequests", func(t *testing.T) {
		cost, err := CalculateS3PutCostWithConfig(0, validConfig)
		require.NoError(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithNegativeRequests", func(t *testing.T) {
		cost, err := CalculateS3PutCostWithConfig(-10, validConfig)
		require.NoError(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithDiscountGreaterThanOne", func(t *testing.T) {
		cost, err := CalculateS3PutCostWithConfig(1000, invalidHighDiscountConfig)
		require.Error(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithNegativeDiscount", func(t *testing.T) {
		cost, err := CalculateS3PutCostWithConfig(1000, invalidNegativeDiscountConfig)
		require.Error(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithNoDiscount", func(t *testing.T) {
		cost, err := CalculateS3PutCostWithConfig(1000, noDiscountConfig)
		require.NoError(t, err)
		assert.Equal(t, 0.005, cost)
	})

	t.Run("WithSingleRequest", func(t *testing.T) {
		cost, err := CalculateS3PutCostWithConfig(1, validConfig)
		require.NoError(t, err)
		assert.InDelta(t, 0.0000035, cost, 0.000000001)
	})

	t.Run("WithLargeNumberOfRequests", func(t *testing.T) {
		cost, err := CalculateS3PutCostWithConfig(1000000, validConfig)
		require.NoError(t, err)
		assert.InDelta(t, 3.5, cost, 0.001)
	})

	t.Run("SeparateCostForUserFilesAndLogs", func(t *testing.T) {
		userFilesCost, err := CalculateS3PutCostWithConfig(1000, validConfig)
		require.NoError(t, err)

		logsCost, err := CalculateS3PutCostWithConfig(500, validConfig)
		require.NoError(t, err)

		// User files: 1000 * 0.000005 * 0.7 = 0.0035
		assert.InDelta(t, 0.0035, userFilesCost, 0.000001)
		// Logs: 500 * 0.000005 * 0.7 = 0.00175
		assert.InDelta(t, 0.00175, logsCost, 0.000001)
		// Total would be: 0.00525
		assert.InDelta(t, 0.00525, userFilesCost+logsCost, 0.000001)
	})
}

func TestS3PutRequestCost(t *testing.T) {
	assert.Equal(t, 0.000005, S3PutRequestCost)
}

func TestCalculatePutRequestsWithContext(t *testing.T) {
	const MB = 1024 * 1024

	t.Run("ZeroOrNegativeSize", func(t *testing.T) {
		assert.Equal(t, 0, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 0))
		assert.Equal(t, 0, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, -100))
		assert.Equal(t, 0, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodWriter, -1*MB))
		assert.Equal(t, 0, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodWriter, 0))
	})

	t.Run("CopyMethod", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodCopy, 1))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodCopy, 1))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodCopy, 1*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodCopy, 100*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodCopy, 1000*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodCopy, 1000*MB))
	})

	t.Run("SmallBucketWriter", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodWriter, 1))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodWriter, 1*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodWriter, 4*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodWriter, 5*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodWriter, 10*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodWriter, 100*MB))
	})

	t.Run("LargeBucketWriter", func(t *testing.T) {
		assert.Equal(t, 3, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodWriter, 1))
		assert.Equal(t, 3, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodWriter, 1*MB))
		assert.Equal(t, 3, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodWriter, 5*MB))
		assert.Equal(t, 4, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodWriter, 10*MB))
		assert.Equal(t, 12, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodWriter, 50*MB))
		assert.Equal(t, 22, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodWriter, 100*MB))
	})

	t.Run("PutMethodSmallBucket", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 1))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 100*1024))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 1*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 4*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 5*MB-1))
		assert.Equal(t, 3, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 5*MB))
		assert.Equal(t, 4, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 5*MB+1))
		assert.Equal(t, 4, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 10*MB))
		assert.Equal(t, 22, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 100*MB))
	})

	t.Run("PutMethodLargeBucket", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 1))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 2*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 4*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 5*MB-1))
		assert.Equal(t, 3, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 5*MB))
		assert.Equal(t, 4, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 5*MB+1))
		assert.Equal(t, 5, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 15*MB))
		assert.Equal(t, 22, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 100*MB))
	})

	t.Run("RealWorldScenarios", func(t *testing.T) {
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 2*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodWriter, 500*1024))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodCopy, 1000*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 300*1024))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 50*1024))
		assert.Equal(t, 6, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 20*MB))
	})

	t.Run("BoundaryConditions", func(t *testing.T) {
		assert.Equal(t, 3, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 5*MB))
		assert.Equal(t, 3, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 5*MB))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 5*MB-1))
		assert.Equal(t, 1, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 5*MB-1))
		assert.Equal(t, 4, CalculatePutRequestsWithContext(S3BucketTypeSmall, S3UploadMethodPut, 5*MB+1))
		assert.Equal(t, 4, CalculatePutRequestsWithContext(S3BucketTypeLarge, S3UploadMethodPut, 5*MB+1))
	})
}

func TestCalculateAndSetUserFilesCost(t *testing.T) {
	validConfig := &evergreen.CostConfig{
		S3Cost: evergreen.S3CostConfig{
			Upload: evergreen.S3UploadCostConfig{
				UploadCostDiscount: 0.3,
			},
		},
	}

	t.Run("WithValidConfig", func(t *testing.T) {
		s3Usage := S3Usage{}
		s3Usage.UserFiles.PutRequests = 1000
		require.NoError(t, s3Usage.CalculateAndSetUserFilesCost(validConfig))
		// 1000 * 0.000005 * 0.7 = 0.0035
		assert.InDelta(t, 0.0035, s3Usage.UserFiles.PutCost, 0.000001)
	})

	t.Run("WithNilConfig", func(t *testing.T) {
		s3Usage := S3Usage{}
		s3Usage.UserFiles.PutRequests = 1000
		require.NoError(t, s3Usage.CalculateAndSetUserFilesCost(nil))
		assert.Equal(t, 0.0, s3Usage.UserFiles.PutCost)
	})

	t.Run("WithZeroPutRequests", func(t *testing.T) {
		s3Usage := S3Usage{}
		require.NoError(t, s3Usage.CalculateAndSetUserFilesCost(validConfig))
		assert.Equal(t, 0.0, s3Usage.UserFiles.PutCost)
	})

	t.Run("WithInvalidDiscount", func(t *testing.T) {
		invalidConfig := &evergreen.CostConfig{
			S3Cost: evergreen.S3CostConfig{
				Upload: evergreen.S3UploadCostConfig{
					UploadCostDiscount: 1.5,
				},
			},
		}
		s3Usage := S3Usage{}
		s3Usage.UserFiles.PutRequests = 1000
		require.Error(t, s3Usage.CalculateAndSetUserFilesCost(invalidConfig))
		assert.Equal(t, 0.0, s3Usage.UserFiles.PutCost)
	})

	t.Run("DoesNotAffectLogFiles", func(t *testing.T) {
		s3Usage := S3Usage{}
		s3Usage.UserFiles.PutRequests = 1000
		s3Usage.LogFiles.PutRequests = 500
		s3Usage.LogFiles.UploadBytesUncompressed = 10000
		s3Usage.LogFiles.LogChunksUploaded = 500
		require.NoError(t, s3Usage.CalculateAndSetUserFilesCost(validConfig))
		assert.InDelta(t, 0.0035, s3Usage.UserFiles.PutCost, 0.000001)
		assert.Equal(t, 500, s3Usage.LogFiles.PutRequests)
		assert.Equal(t, int64(10000), s3Usage.LogFiles.UploadBytesUncompressed)
		assert.Equal(t, 500, s3Usage.LogFiles.LogChunksUploaded)
	})
}
