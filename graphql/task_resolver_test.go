package graphql

import (
	"context"
	"testing"

	"github.com/evergreen-ci/evergreen/model/cost"
	restModel "github.com/evergreen-ci/evergreen/rest/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCostResolverTotal(t *testing.T) {
	r := &costResolver{}
	ctx := context.Background()

	t.Run("NilObjReturnsNil", func(t *testing.T) {
		result, err := r.Total(ctx, nil)
		assert.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("ReturnsSumOfAdjustedFieldsRounded", func(t *testing.T) {
		c := &cost.Cost{
			AdjustedEC2Cost:          0.012915498438270698,
			AdjustedEBSStorageCost:   0.004573629508333333,
			AdjustedS3LogPutCost:     0.000055,
			AdjustedS3LogStorageCost: 5.182527626554171e-7,
		}
		result, err := r.Total(ctx, c)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.InDelta(t, cost.RoundCost(c.TotalAdjusted()), *result, 1e-10)
	})
}

func TestTaskResolverTaskCost(t *testing.T) {
	r := &taskResolver{}
	ctx := context.Background()

	t.Run("NilTaskCostReturnsNil", func(t *testing.T) {
		obj := &restModel.APITask{TaskCost: nil}
		result, err := r.TaskCost(ctx, obj)
		assert.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("AdjustedFieldsAreRounded", func(t *testing.T) {
		obj := &restModel.APITask{
			TaskCost: &cost.Cost{
				OnDemandEC2Cost:               0.019362917886479997,
				AdjustedEC2Cost:               0.012915498438270698,
				AdjustedEBSThroughputCost:     0,
				AdjustedEBSStorageCost:        0.004573629508333333,
				AdjustedS3ArtifactPutCost:     0,
				AdjustedS3LogPutCost:          0.000055,
				AdjustedS3ArtifactStorageCost: 0,
				AdjustedS3LogStorageCost:      5.182527626554171e-7,
			},
		}
		result, err := r.TaskCost(ctx, obj)
		require.NoError(t, err)
		require.NotNil(t, result)

		// On-demand field is passed through unchanged.
		assert.Equal(t, obj.TaskCost.OnDemandEC2Cost, result.OnDemandEC2Cost)

		// Adjusted fields are rounded to 4 significant figures.
		assert.Equal(t, cost.RoundCost(obj.TaskCost.AdjustedEC2Cost), result.AdjustedEC2Cost)
		assert.Equal(t, cost.RoundCost(obj.TaskCost.AdjustedEBSThroughputCost), result.AdjustedEBSThroughputCost)
		assert.Equal(t, cost.RoundCost(obj.TaskCost.AdjustedEBSStorageCost), result.AdjustedEBSStorageCost)
		assert.Equal(t, cost.RoundCost(obj.TaskCost.AdjustedS3ArtifactPutCost), result.AdjustedS3ArtifactPutCost)
		assert.Equal(t, cost.RoundCost(obj.TaskCost.AdjustedS3LogPutCost), result.AdjustedS3LogPutCost)
		assert.Equal(t, cost.RoundCost(obj.TaskCost.AdjustedS3ArtifactStorageCost), result.AdjustedS3ArtifactStorageCost)
		assert.Equal(t, cost.RoundCost(obj.TaskCost.AdjustedS3LogStorageCost), result.AdjustedS3LogStorageCost)
	})

	t.Run("FloatingPointNoiseIsRemoved", func(t *testing.T) {
		obj := &restModel.APITask{
			TaskCost: &cost.Cost{
				AdjustedS3LogStorageCost: 5.182527626554171e-7,
			},
		}
		result, err := r.TaskCost(ctx, obj)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, 5.183e-7, result.AdjustedS3LogStorageCost)
	})
}
