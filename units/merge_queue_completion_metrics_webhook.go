package units

import (
	"context"
	"fmt"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/patch"
	"github.com/mongodb/amboy"
	"github.com/mongodb/amboy/job"
	"github.com/mongodb/amboy/registry"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/message"
)

const mergeQueueCompletionMetricsWebhookJobName = "merge-queue-completion-metrics-webhook"

func init() {
	registry.AddJobType(mergeQueueCompletionMetricsWebhookJobName, func() amboy.Job {
		return &mergeQueueCompletionMetricsWebhookJob{}
	})
}

type mergeQueueCompletionMetricsWebhookJob struct {
	job.Base `bson:"job_base" json:"job_base" yaml:"job_base"`
	PatchID  string `bson:"patch_id" json:"patch_id" yaml:"patch_id"`
	env      evergreen.Environment
}

// NewMergeQueueCompletionMetricsWebhookJob creates a job that emits the patch_completed span
// for a merge queue patch using the GitHub removal time from the "destroyed" webhook.
func NewMergeQueueCompletionMetricsWebhookJob(patchID string) amboy.Job {
	j := &mergeQueueCompletionMetricsWebhookJob{
		PatchID: patchID,
		Base: job.Base{
			JobType: amboy.JobType{
				Name:    mergeQueueCompletionMetricsWebhookJobName,
				Version: 0,
			},
		},
	}
	j.SetID(fmt.Sprintf("%s.%s", mergeQueueCompletionMetricsWebhookJobName, patchID))
	return j
}

// Run emits completion metrics for a merge queue patch using the GitHub webhook removal time as the end time.
func (j *mergeQueueCompletionMetricsWebhookJob) Run(ctx context.Context) {
	defer j.MarkComplete()
	if j.env == nil {
		j.env = evergreen.GetEnvironment()
	}

	p, err := patch.FindOneId(ctx, j.PatchID)
	if err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not find merge queue patch for webhook completion metrics",
			"patch_id": j.PatchID,
			"job":      j.ID(),
		}))
		return
	}
	if p == nil {
		grip.Info(ctx, message.Fields{
			"message":  "no patch found for merge queue webhook completion metrics",
			"patch_id": j.PatchID,
			"job":      j.ID(),
		})
		return
	}

	if p.MergeQueueMetricsEmitted {
		return
	}

	endTime := p.GithubMergeData.RemovedFromQueueAt
	if endTime.IsZero() {
		grip.Info(ctx, message.Fields{
			"message":  "merge queue patch has no RemovedFromQueueAt time",
			"patch_id": j.PatchID,
			"job":      j.ID(),
		})
		return
	}

	v, err := model.VersionFindOneId(ctx, p.Version)
	if err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not find version for merge queue patch webhook completion metrics",
			"patch_id": j.PatchID,
			"job":      j.ID(),
		}))
		return
	}
	if v == nil {
		grip.Info(ctx, message.Fields{
			"message":  "no version found for merge queue patch webhook completion metrics",
			"patch_id": j.PatchID,
			"job":      j.ID(),
		})
		return
	}

	if err := model.EmitMergeQueueCompletionMetrics(ctx, p, v, p.Status, endTime, "github_webhook_destroyed"); err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not emit completion metrics for merge queue patch via webhook",
			"patch_id": j.PatchID,
			"job":      j.ID(),
		}))
		return
	}

	if err := patch.SetMergeQueueMetricsEmitted(ctx, p.Id); err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not mark merge queue metrics emitted for patch via webhook",
			"patch_id": j.PatchID,
			"job":      j.ID(),
		}))
	}
}
