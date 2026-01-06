package task

import (
	"context"
	"testing"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/db"
	"github.com/evergreen-ci/evergreen/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestS3Usage(t *testing.T) {
	t.Run("IsZero", func(t *testing.T) {
		s3Usage := S3Usage{}
		assert.True(t, s3Usage.IsZero())

		s3Usage.NumPutRequests = 10
		assert.False(t, s3Usage.IsZero())
	})

	t.Run("IncrementPutRequests", func(t *testing.T) {
		s3Usage := S3Usage{}
		assert.Equal(t, 0, s3Usage.NumPutRequests)

		s3Usage.IncrementPutRequests(5)
		assert.Equal(t, 5, s3Usage.NumPutRequests)

		s3Usage.IncrementPutRequests(10)
		assert.Equal(t, 15, s3Usage.NumPutRequests)
	})
}

func TestS3UsageCalculateCost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	env := &mock.Environment{}
	require.NoError(t, env.Configure(ctx))

	require.NoError(t, db.ClearCollections(evergreen.ConfigCollection))
	defer func() {
		assert.NoError(t, db.ClearCollections(evergreen.ConfigCollection))
	}()

	costConfig := evergreen.CostConfig{
		OnDemandDiscount: 0.2,
	}
	require.NoError(t, costConfig.Set(ctx))

	t.Run("WithZeroRequests", func(t *testing.T) {
		s3Usage := S3Usage{NumPutRequests: 0}
		cost, err := s3Usage.CalculateCost(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithConfiguredDiscount", func(t *testing.T) {
		s3Usage := S3Usage{NumPutRequests: 1000}
		cost, err := s3Usage.CalculateCost(ctx)
		require.NoError(t, err)
		// 1000 * $0.000005 * (1 - 0.2) = $0.004
		assert.InDelta(t, 0.004, cost, 0.000001)
	})

	t.Run("WithNoDiscount", func(t *testing.T) {
		noDiscountConfig := evergreen.CostConfig{
			OnDemandDiscount: 0.0,
		}
		require.NoError(t, noDiscountConfig.Set(ctx))

		s3Usage := S3Usage{NumPutRequests: 1000}
		cost, err := s3Usage.CalculateCost(ctx)
		require.NoError(t, err)
		// 1000 * $0.000005 = $0.005
		assert.Equal(t, 0.005, cost)
	})

	t.Run("WithNegativeDiscount", func(t *testing.T) {
		negativeConfig := evergreen.CostConfig{
			OnDemandDiscount: -0.5,
		}
		require.NoError(t, negativeConfig.Set(ctx))

		s3Usage := S3Usage{NumPutRequests: 1000}
		cost, err := s3Usage.CalculateCost(ctx)
		require.Error(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithConfigCleared", func(t *testing.T) {
		// Clear the config - GetConfig will return empty config with OnDemandDiscount = 0.0
		require.NoError(t, db.ClearCollections(evergreen.ConfigCollection))

		s3Usage := S3Usage{NumPutRequests: 1000}
		cost, err := s3Usage.CalculateCost(ctx)
		require.NoError(t, err)
		// OnDemandDiscount = 0.0 (no discount): 1000 * $0.000005 = $0.005
		assert.Equal(t, 0.005, cost)
	})
}

func TestCalculateS3CostForTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	env := &mock.Environment{}
	require.NoError(t, env.Configure(ctx))

	require.NoError(t, db.ClearCollections(evergreen.ConfigCollection))
	defer func() {
		assert.NoError(t, db.ClearCollections(evergreen.ConfigCollection))
	}()

	costConfig := evergreen.CostConfig{
		OnDemandDiscount: 0.2,
	}
	require.NoError(t, costConfig.Set(ctx))

	t.Run("CalculatesCostCorrectly", func(t *testing.T) {
		s3Usage := S3Usage{NumPutRequests: 500}
		cost, err := CalculateS3CostForTask(ctx, s3Usage)
		require.NoError(t, err)
		// 500 * $0.000005 * (1 - 0.2) = $0.002
		assert.InDelta(t, 0.002, cost, 0.0000001)
	})

	t.Run("ReturnsZeroForEmptyUsage", func(t *testing.T) {
		s3Usage := S3Usage{}
		cost, err := CalculateS3CostForTask(ctx, s3Usage)
		require.NoError(t, err)
		assert.Equal(t, 0.0, cost)
	})
}

func TestCalculateCostWithConfig(t *testing.T) {
	validConfig := &evergreen.CostConfig{OnDemandDiscount: 0.3}
	invalidConfig := &evergreen.CostConfig{OnDemandDiscount: 1.5}

	t.Run("WithValidConfig", func(t *testing.T) {
		s3Usage := S3Usage{NumPutRequests: 1000}
		cost, err := s3Usage.CalculateCostWithConfig(validConfig)
		require.NoError(t, err)
		// 1000 * $0.000005 * (1 - 0.3) = $0.0035
		assert.InDelta(t, 0.0035, cost, 0.000001)
	})

	t.Run("WithNilConfig", func(t *testing.T) {
		s3Usage := S3Usage{NumPutRequests: 1000}
		cost, err := s3Usage.CalculateCostWithConfig(nil)
		require.NoError(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithZeroRequests", func(t *testing.T) {
		s3Usage := S3Usage{NumPutRequests: 0}
		cost, err := s3Usage.CalculateCostWithConfig(validConfig)
		require.NoError(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithInvalidDiscount", func(t *testing.T) {
		s3Usage := S3Usage{NumPutRequests: 1000}
		cost, err := s3Usage.CalculateCostWithConfig(invalidConfig)
		require.Error(t, err)
		assert.Equal(t, 0.0, cost)
	})
}
