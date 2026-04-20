package cost

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCostTotalAdjusted(t *testing.T) {
	c := Cost{
		AdjustedEC2Cost:               1,
		AdjustedEBSThroughputCost:     2,
		AdjustedEBSStorageCost:        4,
		AdjustedS3ArtifactPutCost:     0.1,
		AdjustedS3LogPutCost:          0.2,
		AdjustedS3ArtifactStorageCost: 0.3,
		AdjustedS3LogStorageCost:      0.4,
		OnDemandEC2Cost:               100,
	}
	assert.InDelta(t, 8.0, c.TotalAdjusted(), 1e-9)
}

func TestRoundToSignificantFigures(t *testing.T) {
	t.Run("Zero", func(t *testing.T) {
		assert.Equal(t, 0.0, roundToSignificantFigures(0, 4))
	})
	t.Run("NoisySmallValue", func(t *testing.T) {
		// Floating-point noise after the 4th significant digit should be removed.
		assert.Equal(t, 0.0000003675, roundToSignificantFigures(0.00000036750000000000004, 4))
	})
	t.Run("LargerValue", func(t *testing.T) {
		assert.Equal(t, 0.008724, roundToSignificantFigures(0.008724004762715199, 4))
	})
	t.Run("RoundsUp", func(t *testing.T) {
		assert.Equal(t, 0.005820, roundToSignificantFigures(0.005819905908980206, 4))
	})
	t.Run("WholeNumber", func(t *testing.T) {
		assert.Equal(t, 1.235, roundToSignificantFigures(1.2345678, 4))
	})
}

func TestCostRound(t *testing.T) {
	c := Cost{
		OnDemandEC2Cost:           0.008724004762715199,
		AdjustedEC2Cost:           0.005819105908980206,
		AdjustedEBSThroughputCost: 0.00019962631363096064,
		AdjustedEBSStorageCost:    0.000009582063054286111,
		AdjustedS3ArtifactPutCost: 0.00000036750000000000004,
		AdjustedS3LogPutCost:      0.000044100000000000001,
	}
	rounded := c.Round()
	assert.Equal(t, 0.008724, rounded.OnDemandEC2Cost)
	assert.Equal(t, 0.005819, rounded.AdjustedEC2Cost)
	assert.Equal(t, 0.0001996, rounded.AdjustedEBSThroughputCost)
	assert.Equal(t, 0.000009582, rounded.AdjustedEBSStorageCost)
	assert.Equal(t, 0.0000003675, rounded.AdjustedS3ArtifactPutCost)
	assert.Equal(t, 0.00004410, rounded.AdjustedS3LogPutCost)
	// Total should be the rounded sum of all adjusted fields.
	assert.Equal(t, roundToSignificantFigures(c.TotalAdjusted(), 4), rounded.Total)
}

func TestCostIsZero(t *testing.T) {
	t.Run("ZeroValues", func(t *testing.T) {
		cost := Cost{}
		assert.True(t, cost.IsZero())
	})

	t.Run("NonZeroOnDemand", func(t *testing.T) {
		cost := Cost{OnDemandEC2Cost: 1.0}
		assert.False(t, cost.IsZero())
	})

	t.Run("NonZeroAdjusted", func(t *testing.T) {
		cost := Cost{AdjustedEC2Cost: 1.0}
		assert.False(t, cost.IsZero())
	})

	t.Run("NonZeroBoth", func(t *testing.T) {
		cost := Cost{OnDemandEC2Cost: 1.5, AdjustedEC2Cost: 1.2}
		assert.False(t, cost.IsZero())
	})

	t.Run("NonZeroOnDemandS3ArtifactPutCost", func(t *testing.T) {
		cost := Cost{OnDemandS3ArtifactPutCost: 0.00005}
		assert.False(t, cost.IsZero())
	})

	t.Run("NonZeroOnDemandS3LogPutCost", func(t *testing.T) {
		cost := Cost{OnDemandS3LogPutCost: 0.00003}
		assert.False(t, cost.IsZero())
	})
}

func TestCostJSONIncludesEBSThroughputFieldsWhenZero(t *testing.T) {
	// Adjusted EBS throughput and storage JSON keys omit `omitempty`, so zero values still serialize (for API stability).
	c := Cost{}
	data, err := json.Marshal(c)
	require.NoError(t, err)

	var unmarshaled map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))

	assert.NotContains(t, unmarshaled, "on_demand_ebs_throughput_cost")
	assert.NotContains(t, unmarshaled, "on_demand_ebs_storage_cost")

	assert.Contains(t, unmarshaled, "adjusted_ebs_throughput_cost")
	assert.Contains(t, unmarshaled, "adjusted_ebs_storage_cost")
	assert.Equal(t, 0.0, unmarshaled["adjusted_ebs_throughput_cost"])
	assert.Equal(t, 0.0, unmarshaled["adjusted_ebs_storage_cost"])
}

func TestCostJSONSerializesTotalWhenSet(t *testing.T) {
	c := Cost{OnDemandEC2Cost: 1, AdjustedEC2Cost: 0.5}
	c.Total = c.TotalAdjusted()
	data, err := json.Marshal(c)
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	assert.InDelta(t, 0.5, m["total"], 1e-9)
	assert.InDelta(t, 1.0, m["on_demand_ec2_cost"], 1e-9)
}
