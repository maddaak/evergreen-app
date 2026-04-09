package patch

import (
	"context"
	"time"

	"github.com/evergreen-ci/evergreen/db"
	"github.com/mongodb/anser/bsonutil"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
)

const MergeQueueDepthSnapshotCollection = "merge_queue_depth_snapshots"

// MergeQueueDepthSnapshot stores the active merge queue patch IDs per project at a point in time,
// used to detect which patches have left the queue since the last snapshot was taken.
type MergeQueueDepthSnapshot struct {
	// ProjectID is the document key so there is always exactly one snapshot per project.
	ProjectID  string    `bson:"_id"`
	PatchIDs   []string  `bson:"patch_ids"`
	CapturedAt time.Time `bson:"captured_at"`
}

var (
	depthSnapshotProjectIDKey  = bsonutil.MustHaveTag(MergeQueueDepthSnapshot{}, "ProjectID")
	depthSnapshotPatchIDsKey   = bsonutil.MustHaveTag(MergeQueueDepthSnapshot{}, "PatchIDs")
	depthSnapshotCapturedAtKey = bsonutil.MustHaveTag(MergeQueueDepthSnapshot{}, "CapturedAt")
)

// FindAllMergeQueueDepthSnapshots returns the most recent snapshot for every project, keyed by project ID.
func FindAllMergeQueueDepthSnapshots(ctx context.Context) (map[string]MergeQueueDepthSnapshot, error) {
	var snapshots []MergeQueueDepthSnapshot
	if err := db.FindAllQ(ctx, MergeQueueDepthSnapshotCollection, db.Query(bson.M{}), &snapshots); err != nil {
		return nil, errors.Wrap(err, "loading merge queue depth snapshots")
	}

	result := make(map[string]MergeQueueDepthSnapshot, len(snapshots))
	for _, s := range snapshots {
		result[s.ProjectID] = s
	}
	return result, nil
}

// UpsertMergeQueueDepthSnapshot saves the current active patch IDs as the new baseline for the next diff.
func UpsertMergeQueueDepthSnapshot(ctx context.Context, projectID string, patchIDs []string) error {
	_, err := db.Upsert(
		ctx,
		MergeQueueDepthSnapshotCollection,
		bson.M{depthSnapshotProjectIDKey: projectID},
		bson.M{"$set": bson.M{
			depthSnapshotPatchIDsKey:   patchIDs,
			depthSnapshotCapturedAtKey: time.Now(),
		}},
	)
	return errors.Wrapf(err, "upserting merge queue depth snapshot for project '%s'", projectID)
}
