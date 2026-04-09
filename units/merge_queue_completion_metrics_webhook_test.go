package units

import (
	"testing"
	"time"

	"github.com/evergreen-ci/evergreen/db"
	mgobson "github.com/evergreen-ci/evergreen/db/mgo/bson"
	"github.com/evergreen-ci/evergreen/model/patch"
	"github.com/evergreen-ci/evergreen/thirdparty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeQueueCompletionMetricsWebhookJobSkipsIfAlreadyEmitted(t *testing.T) {
	require.NoError(t, db.ClearCollections(patch.Collection))

	p := patch.Patch{
		Id:                       mgobson.NewObjectId(),
		MergeQueueMetricsEmitted: true,
		GithubMergeData: thirdparty.GithubMergeGroup{
			RemovedFromQueueAt: time.Now().Add(-2 * time.Minute),
		},
	}
	require.NoError(t, db.Insert(t.Context(), patch.Collection, p))

	j := &mergeQueueCompletionMetricsWebhookJob{PatchID: p.Id.Hex()}
	j.Run(t.Context())

	// Patch should remain emitted — job skipped without error.
	assert.NoError(t, j.Error())
	updated, err := patch.FindOneId(t.Context(), p.Id.Hex())
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.True(t, updated.MergeQueueMetricsEmitted)
}

func TestMergeQueueCompletionMetricsWebhookJobSkipsIfNoRemovedFromQueueAt(t *testing.T) {
	require.NoError(t, db.ClearCollections(patch.Collection))

	p := patch.Patch{
		Id: mgobson.NewObjectId(),
	}
	require.NoError(t, db.Insert(t.Context(), patch.Collection, p))

	j := &mergeQueueCompletionMetricsWebhookJob{PatchID: p.Id.Hex()}
	j.Run(t.Context())

	assert.NoError(t, j.Error())
	updated, err := patch.FindOneId(t.Context(), p.Id.Hex())
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, updated.MergeQueueMetricsEmitted)
}
