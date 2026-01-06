package evergreen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateS3Cost(t *testing.T) {
	config := &CostConfig{OnDemandDiscount: 0.2}
	negativeDiscountConfig := &CostConfig{OnDemandDiscount: -0.5}
	tooHighDiscountConfig := &CostConfig{OnDemandDiscount: 1.5}
	noDiscountConfig := &CostConfig{OnDemandDiscount: 0.0}

	t.Run("WithZeroRequests", func(t *testing.T) {
		cost, err := config.CalculateS3Cost(0)
		assert.NoError(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithNegativeRequests", func(t *testing.T) {
		// ensure invalid input returns 0 instead of negative cost
		cost, err := config.CalculateS3Cost(-10)
		assert.NoError(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithNegativeDiscount", func(t *testing.T) {
		// ensure invalid discount returns error
		cost, err := negativeDiscountConfig.CalculateS3Cost(1000)
		assert.Error(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithDiscountGreaterThanOne", func(t *testing.T) {
		// ensure invalid discount returns error
		cost, err := tooHighDiscountConfig.CalculateS3Cost(1000)
		assert.Error(t, err)
		assert.Equal(t, 0.0, cost)
	})

	t.Run("WithNoDiscount", func(t *testing.T) {
		cost, err := noDiscountConfig.CalculateS3Cost(1000)
		assert.NoError(t, err)
		assert.Equal(t, 0.005, cost)
	})

	t.Run("WithStandardDiscount", func(t *testing.T) {
		cost, err := config.CalculateS3Cost(1000)
		assert.NoError(t, err)
		// 1000 * $0.000005 * (1 - 0.2) = $0.004
		assert.InDelta(t, 0.004, cost, 0.000001)
	})

	t.Run("WithSingleRequest", func(t *testing.T) {
		cost, err := config.CalculateS3Cost(1)
		assert.NoError(t, err)
		// 1 * $0.000005 * (1 - 0.2) = $0.000004
		assert.InDelta(t, 0.000004, cost, 0.000000001)
	})

	t.Run("WithLargeNumberOfRequests", func(t *testing.T) {
		cost, err := config.CalculateS3Cost(1000000)
		assert.NoError(t, err)
		// 1000000 * $0.000005 * (1 - 0.2) = $4.00
		assert.InDelta(t, 4.0, cost, 0.001)
	})
}

func TestS3PutRequestCost(t *testing.T) {
	assert.Equal(t, 0.000005, S3PutRequestCost)
}

func TestCalculatePutRequests(t *testing.T) {
	const MB = 1024 * 1024

	t.Run("ZeroOrNegativeSize", func(t *testing.T) {
		assert.Equal(t, 0, CalculatePutRequests(0))
		assert.Equal(t, 0, CalculatePutRequests(-100))
		assert.Equal(t, 0, CalculatePutRequests(-1*MB))
	})

	t.Run("SmallFiles", func(t *testing.T) {
		// Files ≤ 5MB = 3 requests
		assert.Equal(t, 3, CalculatePutRequests(1))
		assert.Equal(t, 3, CalculatePutRequests(1*MB))
		assert.Equal(t, 3, CalculatePutRequests(3*MB))
		assert.Equal(t, 3, CalculatePutRequests(4*MB))
		assert.Equal(t, 3, CalculatePutRequests(5*MB-1))
	})

	t.Run("ExactlyAtThreshold", func(t *testing.T) {
		// 5MB = 3 requests
		assert.Equal(t, 3, CalculatePutRequests(5*MB))
	})

	t.Run("JustOverThreshold", func(t *testing.T) {
		// 5MB+1 byte = 4 requests (ceiling division)
		assert.Equal(t, 4, CalculatePutRequests(5*MB+1))
	})

	t.Run("MultipartFiles", func(t *testing.T) {
		// 10MB = 2 parts: Create + 2 parts + Complete = 4
		assert.Equal(t, 4, CalculatePutRequests(10*MB))

		// 15MB = 3 parts: Create + 3 parts + Complete = 5
		assert.Equal(t, 5, CalculatePutRequests(15*MB))

		// 50MB = 10 parts: Create + 10 parts + Complete = 12
		assert.Equal(t, 12, CalculatePutRequests(50*MB))

		// 100MB = 20 parts: Create + 20 parts + Complete = 22
		assert.Equal(t, 22, CalculatePutRequests(100*MB))
	})

	t.Run("NonEvenMultiples", func(t *testing.T) {
		// 10.5MB = 3 parts (ceiling): Create + 3 parts + Complete = 5
		assert.Equal(t, 5, CalculatePutRequests(10*MB+512*1024))

		// 25.1MB = 6 parts (ceiling): Create + 6 parts + Complete = 8
		assert.Equal(t, 8, CalculatePutRequests(25*MB+100*1024))
	})
}
