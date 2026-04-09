package units

import (
	"testing"
	"time"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/db"
	mgobson "github.com/evergreen-ci/evergreen/db/mgo/bson"
	"github.com/evergreen-ci/evergreen/model/patch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeQueueCompletionMetricsFallbackJobSkipsPatchFinishedLessThan5MinAgo(t *testing.T) {
	require.NoError(t, db.ClearCollections(patch.Collection))

	p := patch.Patch{
		Id:         mgobson.NewObjectId(),
		Project:    "my-project",
		Alias:      evergreen.CommitQueueAlias,
		Status:     evergreen.VersionSucceeded,
		CreateTime: time.Now(),
		FinishTime: time.Now().Add(-2 * time.Minute),
	}
	require.NoError(t, db.Insert(t.Context(), patch.Collection, p))

	j := &mergeQueueCompletionMetricsFallbackJob{}
	j.emitCompletionMetricsForPatch(t.Context(), &p)

	updated, err := patch.FindOneId(t.Context(), p.Id.Hex())
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, updated.MergeQueueMetricsEmitted)
}

func TestMergeQueueCompletionMetricsFallbackJobSkipsUnparsablePRNumber(t *testing.T) {
	require.NoError(t, db.ClearCollections(patch.Collection))

	p := patch.Patch{
		Id:         mgobson.NewObjectId(),
		Project:    "my-project",
		Alias:      evergreen.CommitQueueAlias,
		Status:     evergreen.VersionSucceeded,
		CreateTime: time.Now(),
		FinishTime: time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, db.Insert(t.Context(), patch.Collection, p))

	j := &mergeQueueCompletionMetricsFallbackJob{}
	j.emitCompletionMetricsForPatch(t.Context(), &p)

	updated, err := patch.FindOneId(t.Context(), p.Id.Hex())
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, updated.MergeQueueMetricsEmitted)
}
