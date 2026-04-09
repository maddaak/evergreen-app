package patch

import (
	"testing"

	"github.com/evergreen-ci/evergreen/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindAllMergeQueueDepthSnapshotsReturnsAllUpsertedProjects(t *testing.T) {
	require.NoError(t, db.ClearCollections(MergeQueueDepthSnapshotCollection))

	snapshots, err := FindAllMergeQueueDepthSnapshots(t.Context())
	require.NoError(t, err)
	assert.Empty(t, snapshots)

	require.NoError(t, UpsertMergeQueueDepthSnapshot(t.Context(), "project-a", []string{"id1", "id2"}))
	require.NoError(t, UpsertMergeQueueDepthSnapshot(t.Context(), "project-b", []string{"id3"}))

	snapshots, err = FindAllMergeQueueDepthSnapshots(t.Context())
	require.NoError(t, err)
	require.Len(t, snapshots, 2)

	a := snapshots["project-a"]
	assert.ElementsMatch(t, []string{"id1", "id2"}, a.PatchIDs)
	assert.False(t, a.CapturedAt.IsZero())

	b := snapshots["project-b"]
	assert.ElementsMatch(t, []string{"id3"}, b.PatchIDs)
}

func TestUpsertMergeQueueDepthSnapshotOverwritesPreviousPatchIDs(t *testing.T) {
	require.NoError(t, db.ClearCollections(MergeQueueDepthSnapshotCollection))

	require.NoError(t, UpsertMergeQueueDepthSnapshot(t.Context(), "project-a", []string{"id1", "id2"}))
	require.NoError(t, UpsertMergeQueueDepthSnapshot(t.Context(), "project-a", []string{"id3"}))

	snapshots, err := FindAllMergeQueueDepthSnapshots(t.Context())
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	assert.ElementsMatch(t, []string{"id3"}, snapshots["project-a"].PatchIDs)
}
