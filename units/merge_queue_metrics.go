package units

import (
	"context"
	"fmt"
	"time"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/patch"
	"github.com/evergreen-ci/evergreen/model/task"
	"github.com/evergreen-ci/utility"
	"github.com/mongodb/amboy"
	"github.com/mongodb/amboy/job"
	"github.com/mongodb/amboy/registry"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/message"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const mergeQueueMetricsJobName = "merge-queue-metrics"

func init() {
	registry.AddJobType(mergeQueueMetricsJobName, NewMergeQueueMetricsJob)
}

type mergeQueueMetricsJob struct {
	job.Base `bson:"job_base" json:"job_base" yaml:"job_base"`
	env      evergreen.Environment
}

// NewMergeQueueMetricsJob creates a job to emit merge queue depth metrics.
func NewMergeQueueMetricsJob() amboy.Job {
	j := &mergeQueueMetricsJob{
		Base: job.Base{
			JobType: amboy.JobType{
				Name:    mergeQueueMetricsJobName,
				Version: 0,
			},
		},
	}
	j.SetID(fmt.Sprintf("%s.%s", mergeQueueMetricsJobName, utility.RoundPartOfHour(5).Format(TSFormat)))
	return j
}

// Run emits merge queue depth and completion metrics for all projects with merge queue enabled.
func (j *mergeQueueMetricsJob) Run(ctx context.Context) {
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

	previousSnapshots, err := patch.FindAllMergeQueueDepthSnapshots(ctx)
	if err != nil {
		grip.Warning(ctx, message.WrapError(err, message.Fields{
			"message": "error loading merge queue depth snapshots",
			"job":     j.ID(),
		}))
		j.AddError(err)
		previousSnapshots = map[string]patch.MergeQueueDepthSnapshot{}
	}

	currentPatchIDsByProject := make(map[string][]string, len(projectRefs))

	for _, projectRef := range projectRefs {
		if err := j.emitMetricsForProject(ctx, &projectRef); err != nil {
			grip.Error(ctx, message.WrapError(err, message.Fields{
				"message":    "error emitting merge queue metrics for project",
				"project_id": projectRef.Id,
				"job":        j.ID(),
			}))
			j.AddError(err)
		}

		snapshotIDs, err := patch.FindMergeQueuePatchIDsForSnapshot(ctx, projectRef.Id)
		if err != nil {
			grip.Error(ctx, message.WrapError(err, message.Fields{
				"message":    "error querying merge queue patch IDs for snapshot",
				"project_id": projectRef.Id,
				"job":        j.ID(),
			}))
			j.AddError(err)
		}
		currentPatchIDsByProject[projectRef.Id] = snapshotIDs
	}

	// Emit before snapshot upsert so a crash here re-detects departed patches next run.
	if err := j.emitCompletionMetrics(ctx, previousSnapshots, currentPatchIDsByProject); err != nil {
		grip.Error(ctx, message.WrapError(err, message.Fields{
			"message": "error emitting merge queue patch completion metrics",
			"job":     j.ID(),
		}))
		j.AddError(err)
	}

	for projectID, patchIDs := range currentPatchIDsByProject {
		if err := patch.UpsertMergeQueueDepthSnapshot(ctx, projectID, patchIDs); err != nil {
			grip.Error(ctx, message.WrapError(err, message.Fields{
				"message":    "error upserting merge queue depth snapshot",
				"project_id": projectID,
				"job":        j.ID(),
			}))
			j.AddError(err)
		}
	}
}

// emitMetricsForProject emits depth metrics for a project.
func (j *mergeQueueMetricsJob) emitMetricsForProject(ctx context.Context, projectRef *model.ProjectRef) error {
	patches, err := patch.FindMergeQueuePatchesByProject(ctx, projectRef.Id)
	if err != nil {
		return errors.Wrapf(err, "querying merge queue patches for project '%s'", projectRef.Id)
	}

	if len(patches) == 0 {
		return nil
	}

	type queueKey struct {
		org        string
		repo       string
		baseBranch string
	}
	queuePatches := make(map[queueKey][]patch.Patch)
	for i := range patches {
		p := patches[i]
		if p.GithubMergeData.Org == "" || p.GithubMergeData.Repo == "" || p.GithubMergeData.BaseBranch == "" {
			continue
		}
		key := queueKey{
			org:        p.GithubMergeData.Org,
			repo:       p.GithubMergeData.Repo,
			baseBranch: p.GithubMergeData.BaseBranch,
		}
		queuePatches[key] = append(queuePatches[key], p)
	}

	for key, queuePatchList := range queuePatches {
		if err := j.emitMetricsForQueue(ctx, projectRef.Id, key.org, key.repo, key.baseBranch, queuePatchList); err != nil {
			grip.Error(ctx, message.WrapError(err, message.Fields{
				"message":     "error emitting metrics for queue",
				"project_id":  projectRef.Id,
				"org":         key.org,
				"repo":        key.repo,
				"base_branch": key.baseBranch,
			}))
			j.AddError(err)
		}
	}

	return nil
}

func (j *mergeQueueMetricsJob) emitMetricsForQueue(ctx context.Context, projectID, org, repo, baseBranch string, patches []patch.Patch) error {
	depth := int64(len(patches))
	pendingCount := int64(0)
	runningCount := int64(0)
	var oldestPatch *patch.Patch
	versionIDs := make([]string, 0, len(patches))

	for i := range patches {
		p := &patches[i]
		versionIDs = append(versionIDs, p.Id.Hex())

		if p.Status == evergreen.VersionCreated {
			pendingCount++
		} else if p.Status == evergreen.VersionStarted {
			runningCount++
		}

		// Track the patch with earliest queue entry time (oldest patch = top of queue).
		// Use HeadCommitDate when available since it reflects when the PR entered the queue;
		pQueueTime := p.GithubMergeData.HeadCommitDate
		if pQueueTime.IsZero() {
			pQueueTime = p.CreateTime
		}
		if oldestPatch == nil {
			oldestPatch = p
		} else {
			oldestQueueTime := oldestPatch.GithubMergeData.HeadCommitDate
			if oldestQueueTime.IsZero() {
				oldestQueueTime = oldestPatch.CreateTime
			}
			if pQueueTime.Before(oldestQueueTime) {
				oldestPatch = p
			}
		}
	}

	oldestPatchAgeMs := int64(0)
	queueEntrySource := "head_commit_date"
	if oldestPatch != nil {
		queueEntryTime := oldestPatch.GithubMergeData.HeadCommitDate
		if queueEntryTime.IsZero() {
			queueEntryTime = oldestPatch.CreateTime
			queueEntrySource = "create_time"
		}
		oldestPatchAgeMs = time.Since(queueEntryTime).Milliseconds()
	}

	runningTasksCount, err := task.CountRunningTasksForVersions(ctx, versionIDs)
	if err != nil {
		grip.Error(ctx, message.WrapError(err, message.Fields{
			"message":     "error counting running tasks in merge queue",
			"project_id":  projectID,
			"org":         org,
			"repo":        repo,
			"base_branch": baseBranch,
		}))
		runningTasksCount = 0
	}

	topOfQueuePatchID := ""
	topOfQueueStatus := ""
	topOfQueueSHA := ""
	if oldestPatch != nil {
		topOfQueuePatchID = oldestPatch.Id.Hex()
		topOfQueueStatus = oldestPatch.Status
		topOfQueueSHA = oldestPatch.GithubMergeData.HeadSHA
	}

	// Emit span using WithNewRoot to ignore sampling so this always gets exported.
	_, span := tracer.Start(ctx, "merge_queue.depth_sample",
		trace.WithNewRoot(),
		trace.WithAttributes(
			attribute.String("evergreen.merge_queue.project_id", projectID),
			attribute.String("evergreen.merge_queue.org", org),
			attribute.String("evergreen.merge_queue.repo", repo),
			attribute.String("evergreen.merge_queue.queue_name", baseBranch),
			attribute.String("evergreen.merge_queue.base_branch", baseBranch),
			attribute.Int64("evergreen.merge_queue.depth", depth),
			attribute.Int64("evergreen.merge_queue.pending_count", pendingCount),
			attribute.Int64("evergreen.merge_queue.running_count", runningCount),
			attribute.Int64("evergreen.merge_queue.running_tasks_count", int64(runningTasksCount)),
			attribute.Bool("evergreen.merge_queue.has_running_tasks", runningTasksCount > 0),
			attribute.Int64("evergreen.merge_queue.oldest_patch_age_ms", oldestPatchAgeMs),
			attribute.String(patch.MergeQueueAttrQueueEntrySource, queueEntrySource),
			attribute.String("evergreen.merge_queue.top_of_queue_patch_id", topOfQueuePatchID),
			attribute.String("evergreen.merge_queue.top_of_queue_status", topOfQueueStatus),
			attribute.String("evergreen.merge_queue.top_of_queue_sha", topOfQueueSHA),
		))
	span.End()

	return nil
}

// emitCompletionMetrics finds patches that departed the queue since the last snapshot and emits completion metrics for each.
func (j *mergeQueueMetricsJob) emitCompletionMetrics(ctx context.Context, previousSnapshots map[string]patch.MergeQueueDepthSnapshot, currentPatchIDsByProject map[string][]string) error {
	currentSets := make(map[string]map[string]bool, len(currentPatchIDsByProject))
	for projectID, ids := range currentPatchIDsByProject {
		set := make(map[string]bool, len(ids))
		for _, id := range ids {
			set[id] = true
		}
		currentSets[projectID] = set
	}

	// Diff: patches in N-1 that are absent from N have left the queue.
	var departedIDs []string
	for projectID, snapshot := range previousSnapshots {
		currentSet := currentSets[projectID]
		for _, id := range snapshot.PatchIDs {
			if !currentSet[id] {
				departedIDs = append(departedIDs, id)
			}
		}
	}

	if len(departedIDs) == 0 {
		return nil
	}

	patches, err := patch.Find(ctx, patch.ByStringIds(departedIDs))
	if err != nil {
		return errors.Wrap(err, "fetching departed merge queue patches")
	}

	for i := range patches {
		p := &patches[i]
		if p.MergeQueueMetricsEmitted {
			continue
		}
		j.emitCompletionMetricsForPatch(ctx, p)
	}
	return nil
}

func (j *mergeQueueMetricsJob) emitCompletionMetricsForPatch(ctx context.Context, p *patch.Patch) {
	var endTime time.Time
	var endTimeSource string

	if !p.GithubMergeData.RemovedFromQueueAt.IsZero() {
		// GitHub removal webhook arrived — use it as the most accurate end time.
		endTime = p.GithubMergeData.RemovedFromQueueAt
		endTimeSource = "github_webhook_destroyed"
	} else {
		// Webhook not yet received — fall back to when Evergreen tasks finished.
		_, collectiveFinishTime, err := p.GetCollectiveTimes(ctx)
		if err != nil {
			grip.Info(ctx, message.WrapError(err, message.Fields{
				"message":  "could not get collective times for merge queue patch",
				"patch_id": p.Id.Hex(),
				"job":      j.ID(),
			}))
			return
		}
		endTime = collectiveFinishTime
		endTimeSource = "evergreen_patch_finish_time"
	}

	v, err := model.VersionFindOneId(ctx, p.Version)
	if err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not find version for merge queue patch",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
		return
	}
	if v == nil {
		grip.Info(ctx, message.Fields{
			"message":  "no version found for merge queue patch",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		})
		return
	}

	if err := model.EmitMergeQueueCompletionMetrics(ctx, p, v, p.Status, endTime, endTimeSource); err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not emit completion metrics for merge queue patch",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
		return
	}

	if err := patch.SetMergeQueueMetricsEmitted(ctx, p.Id); err != nil {
		grip.Info(ctx, message.WrapError(err, message.Fields{
			"message":  "could not mark merge queue metrics emitted for patch",
			"patch_id": p.Id.Hex(),
			"job":      j.ID(),
		}))
	}
}

// PopulateMergeQueueMetricsJobs enqueues a job to emit merge queue depth metrics.
func PopulateMergeQueueMetricsJobs() amboy.QueueOperation {
	return func(ctx context.Context, queue amboy.Queue) error {
		flags, err := evergreen.GetServiceFlags(ctx)
		if err != nil {
			return errors.WithStack(err)
		}

		if flags.MonitorDisabled {
			return nil
		}

		j := NewMergeQueueMetricsJob()
		return queue.Put(ctx, j)
	}
}
