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

func TestEmitCompletionMetricsForPatchNoWebhookUsesPatchFinishTime(t *testing.T) {
	require.NoError(t, db.ClearCollections(patch.Collection))

	p := patch.Patch{
		Id:      mgobson.NewObjectId(),
		Version: mgobson.NewObjectId().Hex(),
	}
	require.NoError(t, db.Insert(t.Context(), patch.Collection, p))

	j := &mergeQueueMetricsJob{}
	j.emitCompletionMetricsForPatch(t.Context(), &p)

	updated, err := patch.FindOneId(t.Context(), p.Id.Hex())
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.False(t, updated.MergeQueueMetricsEmitted)
}

func TestEmitCompletionMetricsForPatchUsesRemovedFromQueueAtAsEndTime(t *testing.T) {
	p := patch.Patch{
		Id: mgobson.NewObjectId(),
		GithubMergeData: thirdparty.GithubMergeGroup{
			RemovedFromQueueAt: time.Now().Add(-3 * time.Minute),
		},
	}

	j := &mergeQueueMetricsJob{}
	j.emitCompletionMetricsForPatch(t.Context(), &p)
}

func TestEmitCompletionMetricsOnlyEmitsForDepartedPatches(t *testing.T) {
	require.NoError(t, db.ClearCollections(patch.Collection, patch.MergeQueueDepthSnapshotCollection))

	departedID := mgobson.NewObjectId()
	stillActiveID := mgobson.NewObjectId()

	previousSnapshots := map[string]patch.MergeQueueDepthSnapshot{
		"my-project": {
			ProjectID: "my-project",
			PatchIDs:  []string{departedID.Hex(), stillActiveID.Hex()},
		},
	}
	currentPatchIDsByProject := map[string][]string{
		"my-project": {stillActiveID.Hex()},
	}

	departed := patch.Patch{Id: departedID}
	require.NoError(t, db.Insert(t.Context(), patch.Collection, departed))

	j := &mergeQueueMetricsJob{}
	err := j.emitCompletionMetrics(t.Context(), previousSnapshots, currentPatchIDsByProject)
	assert.NoError(t, err)
}
