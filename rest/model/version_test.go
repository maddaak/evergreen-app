package model

import (
	"testing"
	"time"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/db"
	mgobson "github.com/evergreen-ci/evergreen/db/mgo/bson"
	"github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/cost"
	"github.com/evergreen-ci/evergreen/model/patch"
	"github.com/evergreen-ci/evergreen/model/s3usage"
	"github.com/evergreen-ci/evergreen/model/task"
	"github.com/evergreen-ci/utility"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVersionBuildFromService tests that BuildFromService function completes
// correctly and without error.
func TestVersionBuildFromService(t *testing.T) {
	assert := assert.New(t)

	ts := time.Now()
	versionId := "versionId"
	revision := "revision"
	author := "author"
	authorEmail := "author_email"
	msg := "message"
	status := "status"
	repo := "repo"
	branch := "branch"
	errors := []string{"made a mistake"}

	bv1 := "buildvariant1"
	bv2 := "buildvariant2"
	bi1 := "buildId1"
	bi2 := "buildId2"

	buildVariants := []model.VersionBuildStatus{
		{
			BuildVariant: bv1,
			BuildId:      bi1,
		},
		{
			BuildVariant: bv2,
			BuildId:      bi2,
		},
	}
	gitTags := []model.GitTag{
		{
			Tag:    "tag",
			Pusher: "pusher",
		},
	}
	triggeredGitTag := model.GitTag{
		Tag:    "my-triggered-tag",
		Pusher: "pusher",
	}
	ingestTs := ts.Add(time.Minute)
	v := model.Version{
		Id:                versionId,
		CreateTime:        ts,
		IngestTime:        ingestTs,
		StartTime:         ts,
		FinishTime:        ts,
		Revision:          revision,
		Author:            author,
		AuthorEmail:       authorEmail,
		Message:           msg,
		Status:            status,
		Repo:              repo,
		Branch:            branch,
		BuildVariants:     buildVariants,
		Errors:            errors,
		GitTags:           gitTags,
		TriggeredByGitTag: triggeredGitTag,
	}

	apiVersion := &APIVersion{}
	// BuildFromService should complete without error
	apiVersion.BuildFromService(t.Context(), v)
	// Each field should be as expected
	assert.Equal(apiVersion.Id, utility.ToStringPtr(versionId))
	assert.Equal(*apiVersion.CreateTime, ts)
	require.NotNil(t, apiVersion.IngestTime)
	assert.Equal(*apiVersion.IngestTime, ingestTs)
	assert.Equal(*apiVersion.StartTime, ts)
	assert.Equal(*apiVersion.FinishTime, ts)
	assert.Equal(apiVersion.Revision, utility.ToStringPtr(revision))
	assert.Equal(apiVersion.Author, utility.ToStringPtr(author))
	assert.Equal(apiVersion.AuthorEmail, utility.ToStringPtr(authorEmail))
	assert.Equal(apiVersion.Message, utility.ToStringPtr(msg))
	assert.Equal(apiVersion.Status, utility.ToStringPtr(status))
	assert.Equal(apiVersion.Repo, utility.ToStringPtr(repo))
	assert.Equal(apiVersion.Branch, utility.ToStringPtr(branch))
	assert.Equal(apiVersion.Errors, utility.ToStringPtrSlice(errors))

	bvs := apiVersion.BuildVariantStatus
	assert.Equal(bvs[0].BuildVariant, utility.ToStringPtr(bv1))
	assert.Equal(bvs[0].BuildId, utility.ToStringPtr(bi1))
	assert.Equal(bvs[1].BuildVariant, utility.ToStringPtr(bv2))
	assert.Equal(bvs[1].BuildId, utility.ToStringPtr(bi2))

	gts := apiVersion.GitTags
	require.Len(t, gts, 1)
	assert.Equal(gts[0].Pusher, utility.ToStringPtr("pusher"))
	assert.Equal(gts[0].Tag, utility.ToStringPtr("tag"))

	require.NotNil(t, apiVersion.TriggeredGitTag)
	assert.Equal(apiVersion.TriggeredGitTag.Tag, utility.ToStringPtr("my-triggered-tag"))
}

func TestVersionBuildFromServiceCost(t *testing.T) {
	t.Run("PopulatedCost", func(t *testing.T) {
		v := model.Version{
			Id: "v-with-costs",
			Cost: cost.Cost{
				OnDemandEC2Cost:           15.0,
				AdjustedEC2Cost:           12.0,
				AdjustedS3ArtifactPutCost: 0.08,
				AdjustedS3LogPutCost:      0.03,
			},
			PredictedCost: cost.Cost{
				OnDemandEC2Cost: 5.0,
				AdjustedEC2Cost: 4.0,
			},
		}

		apiVersion := &APIVersion{}
		apiVersion.BuildFromService(t.Context(), v)

		require.NotNil(t, apiVersion.Cost)
		assert.InDelta(t, 15.0, apiVersion.Cost.OnDemandEC2Cost, 0.01)
		assert.InDelta(t, 12.0, apiVersion.Cost.AdjustedEC2Cost, 0.01)
		assert.InDelta(t, 0.08, apiVersion.Cost.AdjustedS3ArtifactPutCost, 0.001)
		assert.InDelta(t, 0.03, apiVersion.Cost.AdjustedS3LogPutCost, 0.001)
		assert.InDelta(t, 12.0+0.08+0.03, apiVersion.Cost.Total, 0.001)

		require.NotNil(t, apiVersion.PredictedCost)
		assert.InDelta(t, 5.0, apiVersion.PredictedCost.OnDemandEC2Cost, 0.01)
		assert.InDelta(t, 4.0, apiVersion.PredictedCost.AdjustedEC2Cost, 0.01)
		assert.InDelta(t, 4.0, apiVersion.PredictedCost.Total, 0.01)
	})

	t.Run("ZeroCostIsNil", func(t *testing.T) {
		v := model.Version{
			Id: "v-no-costs",
		}

		apiVersion := &APIVersion{}
		apiVersion.BuildFromService(t.Context(), v)

		assert.Nil(t, apiVersion.Cost)
		assert.Nil(t, apiVersion.PredictedCost)
	})
}

func TestVersionBuildFromServiceS3Usage(t *testing.T) {
	t.Run("PopulatedS3UsageIsExposed", func(t *testing.T) {
		v := model.Version{
			Id: "v-with-s3-usage",
			S3Usage: s3usage.S3Usage{
				Artifacts: s3usage.ArtifactMetrics{
					S3UploadMetrics: s3usage.S3UploadMetrics{
						PutRequests: 100,
						UploadBytes: 1024 * 1024,
					},
					Count: 5,
				},
				Logs: s3usage.LogMetrics{
					S3UploadMetrics: s3usage.S3UploadMetrics{
						PutRequests: 50,
						UploadBytes: 512 * 1024,
					},
				},
			},
		}

		apiVersion := &APIVersion{}
		apiVersion.BuildFromService(t.Context(), v)

		require.NotNil(t, apiVersion.S3Usage)
		assert.Equal(t, 100, apiVersion.S3Usage.Artifacts.PutRequests)
		assert.Equal(t, int64(1024*1024), apiVersion.S3Usage.Artifacts.UploadBytes)
		assert.Equal(t, 5, apiVersion.S3Usage.Artifacts.Count)
		assert.Equal(t, 50, apiVersion.S3Usage.Logs.PutRequests)
		assert.Equal(t, int64(512*1024), apiVersion.S3Usage.Logs.UploadBytes)
	})

	t.Run("ZeroS3UsageIsNil", func(t *testing.T) {
		v := model.Version{
			Id: "v-no-s3-usage",
		}

		apiVersion := &APIVersion{}
		apiVersion.BuildFromService(t.Context(), v)

		assert.Nil(t, apiVersion.S3Usage)
	})
}

func TestVersionBuildFromServiceChildPatchCosts(t *testing.T) {
	require.NoError(t, db.ClearCollections(model.VersionCollection, patch.Collection))
	t.Cleanup(func() { db.ClearCollections(model.VersionCollection, patch.Collection) }) //nolint:errcheck

	childPatchID := mgobson.NewObjectId()
	childVersionID := "child-version-1"

	childVersion := model.Version{
		Id: childVersionID,
		Cost: cost.Cost{
			AdjustedEC2Cost:      3.0,
			AdjustedS3LogPutCost: 0.5,
		},
		PredictedCost: cost.Cost{
			AdjustedEC2Cost: 2.0,
		},
	}
	require.NoError(t, childVersion.Insert(t.Context()))

	childPatchDoc := patch.Patch{
		Id:      childPatchID,
		Version: childVersionID,
		Status:  evergreen.VersionSucceeded,
	}
	require.NoError(t, childPatchDoc.Insert(t.Context()))

	// patch.FindOneId requires a valid ObjectId hex, so version ID must equal the patch ObjectId hex.
	parentPatchID := mgobson.NewObjectId()
	parentVersionID := parentPatchID.Hex()
	parentPatchDoc := patch.Patch{
		Id:      parentPatchID,
		Version: parentVersionID,
		Status:  evergreen.VersionSucceeded,
		Triggers: patch.TriggerInfo{
			ChildPatches: []string{childPatchID.Hex()},
		},
	}
	require.NoError(t, parentPatchDoc.Insert(t.Context()))

	parentVersion := model.Version{
		Id:        parentVersionID,
		Requester: evergreen.PatchVersionRequester,
		Status:    evergreen.VersionSucceeded,
		Cost: cost.Cost{
			AdjustedEC2Cost: 10.0,
		},
		PredictedCost: cost.Cost{
			AdjustedEC2Cost: 8.0,
		},
	}

	apiVersion := &APIVersion{}
	apiVersion.BuildFromService(t.Context(), parentVersion)

	require.NotNil(t, apiVersion.Cost)
	// Child adjusted total = 3.0 + 0.5 = 3.5
	assert.InDelta(t, 3.5, apiVersion.Cost.ChildPatchesTotalCost, 0.001)
	// Total = parent adjusted total + child patches total = 10.0 + 3.5 = 13.5
	assert.InDelta(t, 13.5, apiVersion.Cost.Total, 0.001)

	require.NotNil(t, apiVersion.PredictedCost)
	// Child predicted adjusted total = 2.0
	assert.InDelta(t, 2.0, apiVersion.PredictedCost.ChildPatchesTotalCost, 0.001)
	// Total = parent predicted adjusted total + child patches total = 8.0 + 2.0 = 10.0
	assert.InDelta(t, 10.0, apiVersion.PredictedCost.Total, 0.001)
}

func TestVersionBuildFromServiceChildPatchCostsSkippedWhenNotFinished(t *testing.T) {
	require.NoError(t, db.ClearCollections(model.VersionCollection, patch.Collection))
	t.Cleanup(func() { db.ClearCollections(model.VersionCollection, patch.Collection) }) //nolint:errcheck

	childPatchID := mgobson.NewObjectId()
	childVersionID := "child-version-not-finished"

	childPatchDoc := patch.Patch{
		Id:      childPatchID,
		Version: childVersionID,
		Status:  evergreen.VersionSucceeded,
	}
	require.NoError(t, childPatchDoc.Insert(t.Context()))

	parentPatchID := mgobson.NewObjectId()
	parentVersionID := parentPatchID.Hex()
	parentPatchDoc := patch.Patch{
		Id:      parentPatchID,
		Version: parentVersionID,
		Status:  evergreen.VersionStarted,
		Triggers: patch.TriggerInfo{
			ChildPatches: []string{childPatchID.Hex()},
		},
	}
	require.NoError(t, parentPatchDoc.Insert(t.Context()))

	parentVersion := model.Version{
		Id:        parentVersionID,
		Requester: evergreen.PatchVersionRequester,
		Status:    evergreen.VersionStarted,
		Cost: cost.Cost{
			AdjustedEC2Cost: 10.0,
		},
	}

	apiVersion := &APIVersion{}
	apiVersion.BuildFromService(t.Context(), parentVersion)

	require.NotNil(t, apiVersion.Cost)
	assert.Zero(t, apiVersion.Cost.ChildPatchesTotalCost)
	// Total reflects only the parent's own cost — child costs not loaded
	assert.InDelta(t, 10.0, apiVersion.Cost.Total, 0.001)
}

func TestVersionBuildFromServiceChildPatchCostSkipsInvalidChildren(t *testing.T) {
	require.NoError(t, db.ClearCollections(model.VersionCollection, patch.Collection))
	t.Cleanup(func() { db.ClearCollections(model.VersionCollection, patch.Collection) }) //nolint:errcheck

	// Child 1: no version yet (not started)
	noVersionPatchID := mgobson.NewObjectId()
	noVersionPatch := patch.Patch{
		Id:     noVersionPatchID,
		Status: evergreen.VersionCreated,
	}
	require.NoError(t, noVersionPatch.Insert(t.Context()))

	// Child 2: version ID set but no version doc in DB
	missingVersionPatchID := mgobson.NewObjectId()
	missingVersionPatch := patch.Patch{
		Id:      missingVersionPatchID,
		Version: "version-does-not-exist",
		Status:  evergreen.VersionStarted,
	}
	require.NoError(t, missingVersionPatch.Insert(t.Context()))

	// Child 3: valid version with real cost — only this one should contribute
	validChildPatchID := mgobson.NewObjectId()
	validChildVersion := model.Version{
		Id: "valid-child-version",
		Cost: cost.Cost{
			AdjustedEC2Cost: 4.0,
		},
	}
	require.NoError(t, validChildVersion.Insert(t.Context()))
	validChildPatch := patch.Patch{
		Id:      validChildPatchID,
		Version: validChildVersion.Id,
		Status:  evergreen.VersionSucceeded,
	}
	require.NoError(t, validChildPatch.Insert(t.Context()))

	parentPatchID := mgobson.NewObjectId()
	parentVersionID := parentPatchID.Hex()
	parentPatchDoc := patch.Patch{
		Id:      parentPatchID,
		Version: parentVersionID,
		Status:  evergreen.VersionSucceeded,
		Triggers: patch.TriggerInfo{
			ChildPatches: []string{noVersionPatchID.Hex(), missingVersionPatchID.Hex(), validChildPatchID.Hex()},
		},
	}
	require.NoError(t, parentPatchDoc.Insert(t.Context()))

	parentVersion := model.Version{
		Id:        parentVersionID,
		Requester: evergreen.PatchVersionRequester,
		Status:    evergreen.VersionSucceeded,
		Cost: cost.Cost{
			AdjustedEC2Cost: 10.0,
		},
	}

	apiVersion := &APIVersion{}
	apiVersion.BuildFromService(t.Context(), parentVersion)

	require.NotNil(t, apiVersion.Cost)
	assert.InDelta(t, 4.0, apiVersion.Cost.ChildPatchesTotalCost, 0.001)
	// Total = parent 10.0 + valid child 4.0 = 14.0
	assert.InDelta(t, 14.0, apiVersion.Cost.Total, 0.001)
}

func TestVersionBuildFromServiceChildPatchCostAllocatesWhenParentHasNoCost(t *testing.T) {
	require.NoError(t, db.ClearCollections(model.VersionCollection, patch.Collection))
	t.Cleanup(func() { db.ClearCollections(model.VersionCollection, patch.Collection) }) //nolint:errcheck

	childPatchID := mgobson.NewObjectId()
	childVersion := model.Version{
		Id: "child-version-no-parent-cost",
		Cost: cost.Cost{
			AdjustedEC2Cost: 6.0,
		},
	}
	require.NoError(t, childVersion.Insert(t.Context()))
	childPatchDoc := patch.Patch{
		Id:      childPatchID,
		Version: childVersion.Id,
		Status:  evergreen.VersionSucceeded,
	}
	require.NoError(t, childPatchDoc.Insert(t.Context()))

	parentPatchID := mgobson.NewObjectId()
	parentVersionID := parentPatchID.Hex()
	parentPatchDoc := patch.Patch{
		Id:      parentPatchID,
		Version: parentVersionID,
		Status:  evergreen.VersionSucceeded,
		Triggers: patch.TriggerInfo{
			ChildPatches: []string{childPatchID.Hex()},
		},
	}
	require.NoError(t, parentPatchDoc.Insert(t.Context()))

	// Parent has no own cost — Cost field is zero
	parentVersion := model.Version{
		Id:        parentVersionID,
		Requester: evergreen.PatchVersionRequester,
		Status:    evergreen.VersionSucceeded,
	}

	apiVersion := &APIVersion{}
	apiVersion.BuildFromService(t.Context(), parentVersion)

	require.NotNil(t, apiVersion.Cost)
	assert.InDelta(t, 6.0, apiVersion.Cost.ChildPatchesTotalCost, 0.001)
	assert.InDelta(t, 6.0, apiVersion.Cost.Total, 0.001)
}

func TestAPITaskBuildFromServiceSetsCostTotals(t *testing.T) {
	tsk := &task.Task{
		TaskCost: cost.Cost{
			AdjustedEC2Cost:           10.0,
			AdjustedS3ArtifactPutCost: 0.05,
		},
		PredictedTaskCost: cost.Cost{
			AdjustedEC2Cost: 3.0,
		},
	}
	var api APITask
	require.NoError(t, api.BuildFromService(t.Context(), tsk, nil))
	require.NotNil(t, api.TaskCost)
	require.NotNil(t, api.PredictedTaskCost)
	assert.InDelta(t, 10.05, api.TaskCost.Total, 0.0001)
	assert.InDelta(t, 3.0, api.PredictedTaskCost.Total, 0.0001)
}
