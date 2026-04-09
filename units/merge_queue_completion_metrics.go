package units

import (
	"context"
	"fmt"
	"time"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/patch"
	"github.com/evergreen-ci/evergreen/thirdparty"
	"github.com/evergreen-ci/utility"
	"github.com/mongodb/amboy"
	"github.com/mongodb/amboy/job"
	"github.com/mongodb/amboy/registry"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/message"
	"github.com/pkg/errors"
)

const mergeQueueCompletionMetricsFallbackJobName = "merge-queue-completion-metrics-fallback"

func init() {
	registry.AddJobType(mergeQueueCompletionMetricsFallbackJobName, NewMergeQueueCompletionMetricsFallbackJob)
}

type mergeQueueCompletionMetricsFallbackJob struct {
	job.Base `bson:"job_base" json:"job_base" yaml:"job_base"`
	env      evergreen.Environment
}

// NewMergeQueueCompletionMetricsFallbackJob creates a job that polls the GitHub PR API for merge
// queue patches that finished but never received a "destroyed" webhook, and emits the patch_completed
// span when the PR is confirmed merged.
func NewMergeQueueCompletionMetricsFallbackJob() amboy.Job {
	j := &mergeQueueCompletionMetricsFallbackJob{
		Base: job.Base{
			JobType: amboy.JobType{
				Name:    mergeQueueCompletionMetricsFallbackJobName,
				Version: 0,
			},
		},
	}
	j.SetID(fmt.Sprintf("%s.%s", mergeQueueCompletionMetricsFallbackJobName, utility.RoundPartOfHour(5).Format(TSFormat)))
	return j
}

// Run finds finalized merge queue patches that missed the GitHub webhook, polls the GitHub PR API,
// and emits completion metrics for any that are confirmed merged.
func (j *mergeQueueCompletionMetricsFallbackJob) Run(ctx context.Context) {
	defer j.MarkComplete()
	if j.env == nil {
		j.env = evergreen.GetEnvironment()
	}

	projectRefs, err := model.FindProjectRefsWithMergeQueueEnabled(ctx)
	if err != nil {
		grip.Error(ctx, message.WrapError(err, message.Fields{
			"message": "error finding projects with merge queue enabled",
			"job":     j.ID(),
		}))
		j.AddError(errors.Wrap(err, "finding projects with merge queue enabled"))
		return
	}

	for _, projectRef := range projectRefs {
		patches, err := patch.FindMergeQueuePatchesMissingCompletionMetrics(ctx, projectRef.Id)
		if err != nil {
			grip.Error(ctx, message.WrapError(err, message.Fields{
				"message":    "error querying merge queue patches missing completion metrics",
				"project_id": projectRef.Id,
				"job":        j.ID(),
			}))
			j.AddError(err)
			continue
		}
		for i := range patches {
			j.emitCompletionMetricsForPatch(ctx, &patches[i])
		}
	}
}

func (j *mergeQueueCompletionMetricsFallbackJob) emitCompletionMetricsForPatch(ctx context.Context, p *patch.Patch) {
	_, collectiveFinishTime, err := p.GetCollectiveTimes(ctx)
	if err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not get collective times for merge queue patch",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
		return
	}

	// Wait at least 5 minutes after Evergreen finishes before polling GitHub,
	// to give the webhook a chance to arrive first.
	if collectiveFinishTime.IsZero() || time.Since(collectiveFinishTime) < 5*time.Minute {
		return
	}

	prNum, err := p.GithubMergeData.PRNumber()
	if err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not parse PR number for merge queue patch",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
		return
	}

	pr, err := thirdparty.GetGithubPullRequest(ctx, p.GithubMergeData.Org, p.GithubMergeData.Repo, prNum)
	if err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not fetch GitHub PR for merge queue patch",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
		return
	}

	// Only emit if GitHub confirms the PR was merged — this is the source of truth for the end time.
	mergedAt := pr.GetMergedAt()
	if !pr.GetMerged() || mergedAt.IsZero() {
		return
	}

	// Re-fetch to check whether a webhook job emitted between our query and now.
	latest, err := patch.FindOneId(ctx, p.Id.Hex())
	if err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not re-fetch merge queue patch before emitting fallback metrics",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
		return
	}
	if latest == nil || latest.MergeQueueMetricsEmitted {
		return
	}

	v, err := model.VersionFindOneId(ctx, p.Version)
	if err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not find version for merge queue patch fallback completion metrics",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
		return
	}
	if v == nil {
		grip.Info(ctx, message.Fields{
			"message":  "no version found for merge queue patch fallback completion metrics",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		})
		return
	}

	endTime := mergedAt.Time
	if err := model.EmitMergeQueueCompletionMetrics(ctx, p, v, p.Status, endTime, patch.MergeQueueEndTimeSourceGitHubPRAPI); err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not emit fallback completion metrics for merge queue patch",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
		return
	}

	if err := patch.SetMergeQueueMetricsEmitted(ctx, p.Id); err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not mark merge queue metrics emitted for patch via fallback",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
	}
}

// PopulateMergeQueueCompletionMetricsFallbackJobs enqueues a job to emit completion metrics for
// merge queue patches that missed the GitHub webhook.
func PopulateMergeQueueCompletionMetricsFallbackJobs() amboy.QueueOperation {
	return func(ctx context.Context, queue amboy.Queue) error {
		flags, err := evergreen.GetServiceFlags(ctx)
		if err != nil {
			return errors.WithStack(err)
		}

		if flags.MonitorDisabled {
			return nil
		}

		j := NewMergeQueueCompletionMetricsFallbackJob()
		return queue.Put(ctx, j)
	}
}
