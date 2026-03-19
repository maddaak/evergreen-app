package units

import (
	"context"
	"fmt"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/db"
	"github.com/evergreen-ci/evergreen/model/event"
	"github.com/evergreen-ci/utility"

	"github.com/mongodb/amboy"
	"github.com/mongodb/amboy/job"
	"github.com/mongodb/amboy/registry"
	adb "github.com/mongodb/anser/db"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/message"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
)

const (
	webhookSecretMigrationJobName   = "webhook-secret-migration"
	webhookSecretMigrationBatchSize = 50

	webhookSecretCleanupJobName   = "webhook-secret-cleanup"
	webhookSecretCleanupBatchSize = 50
)

func init() {
	registry.AddJobType(webhookSecretMigrationJobName,
		func() amboy.Job { return makeWebhookSecretMigrationJob() })
	registry.AddJobType(webhookSecretCleanupJobName,
		func() amboy.Job { return makeWebhookSecretCleanupJob() })
}

type webhookSecretMigrationJob struct {
	job.Base       `bson:"job_base" json:"job_base" yaml:"job_base"`
	SubscriptionID string `bson:"subscription_id" json:"subscription_id" yaml:"subscription_id"`

	env evergreen.Environment
}

func makeWebhookSecretMigrationJob() *webhookSecretMigrationJob {
	j := &webhookSecretMigrationJob{
		Base: job.Base{
			JobType: amboy.JobType{
				Name:    webhookSecretMigrationJobName,
				Version: 0,
			},
		},
	}
	return j
}

// NewWebhookSecretMigrationJob creates a job to migrate a single webhook
// subscription's secret from MongoDB to Parameter Store.
func NewWebhookSecretMigrationJob(subscriptionID, ts string) amboy.Job {
	j := makeWebhookSecretMigrationJob()
	j.SubscriptionID = subscriptionID
	j.SetID(fmt.Sprintf("%s.%s.%s", webhookSecretMigrationJobName, subscriptionID, ts))
	j.SetScopes([]string{
		fmt.Sprintf("%s.%s", webhookSecretMigrationJobName, subscriptionID),
	})
	j.SetEnqueueAllScopes(true)
	return j
}

func (j *webhookSecretMigrationJob) Run(ctx context.Context) {
	defer j.MarkComplete()

	if j.env == nil {
		j.env = evergreen.GetEnvironment()
	}

	sub := &event.Subscription{}
	if err := db.FindOneQ(ctx, event.SubscriptionsCollection, db.Query(bson.M{"_id": j.SubscriptionID}), sub); err != nil {
		if adb.ResultsNotFound(err) {
			grip.Info(message.Fields{
				"message":         "subscription not found, skipping migration",
				"subscription_id": j.SubscriptionID,
				"job_id":          j.ID(),
				"source":          "webhook-secret-migration",
			})
			return
		}
		j.AddError(errors.Wrapf(err, "finding subscription '%s'", j.SubscriptionID))
		return
	}

	if sub.Subscriber.Type != event.EvergreenWebhookSubscriberType {
		return
	}

	webhookSub, ok := sub.Subscriber.Target.(*event.WebhookSubscriber)
	if !ok {
		return
	}

	// Already migrated.
	if webhookSub.SecretParameter != "" {
		return
	}

	// Nothing to migrate.
	if len(webhookSub.Secret) == 0 {
		return
	}

	paramMgr := j.env.ParameterManager()
	paramPath := event.GetWebhookSecretParameterPath(j.SubscriptionID)

	param, err := paramMgr.Put(ctx, paramPath, string(webhookSub.Secret))
	if err != nil {
		j.AddError(errors.Wrapf(err, "saving webhook secret to Parameter Store for subscription '%s'", j.SubscriptionID))
		return
	}

	if err := db.Update(ctx, event.SubscriptionsCollection,
		bson.M{"_id": j.SubscriptionID},
		bson.M{
			"$set": bson.M{"subscriber.target.secret_parameter": param.Name},
		},
	); err != nil {
		j.AddError(errors.Wrapf(err, "updating subscription '%s' after migrating secret", j.SubscriptionID))
		return
	}

	grip.Info(message.Fields{
		"message":         "successfully migrated webhook secret to Parameter Store",
		"subscription_id": j.SubscriptionID,
		"job_id":          j.ID(),
		"source":          "webhook-secret-migration",
	})
}

// findUnmigratedWebhookSubscriptionIDs returns IDs of webhook subscriptions
// that still have secrets stored in MongoDB (not yet migrated to Parameter Store).
func findUnmigratedWebhookSubscriptionIDs(ctx context.Context, limit int) ([]string, error) {
	subscriptions := []event.Subscription{}
	query := db.Query(bson.M{
		"subscriber.type":                    event.EvergreenWebhookSubscriberType,
		"subscriber.target.secret":           bson.M{"$exists": true, "$ne": nil},
		"subscriber.target.secret_parameter": bson.M{"$exists": false},
	}).Limit(limit)

	if err := db.FindAllQ(ctx, event.SubscriptionsCollection, query, &subscriptions); err != nil {
		return nil, errors.Wrap(err, "finding unmigrated webhook subscriptions")
	}

	ids := make([]string, 0, len(subscriptions))
	for _, sub := range subscriptions {
		ids = append(ids, sub.ID)
	}
	return ids, nil
}

// PopulateWebhookSecretMigrationJobs returns a QueueOperation that enqueues
// jobs to migrate webhook secrets from MongoDB to Parameter Store.
func PopulateWebhookSecretMigrationJobs() amboy.QueueOperation {
	return func(ctx context.Context, queue amboy.Queue) error {
		ids, err := findUnmigratedWebhookSubscriptionIDs(ctx, webhookSecretMigrationBatchSize)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		grip.Info(message.Fields{
			"message": "enqueuing webhook secret migration jobs",
			"count":   len(ids),
			"source":  "webhook-secret-migration",
		})

		ts := utility.RoundPartOfHour(5).Format(TSFormat)
		catcher := grip.NewBasicCatcher()
		for _, id := range ids {
			catcher.Wrapf(amboy.EnqueueUniqueJob(ctx, queue, NewWebhookSecretMigrationJob(id, ts)),
				"enqueueing migration job for subscription '%s'", id)
		}
		return catcher.Resolve()
	}
}

// Phase 2: Cleanup — remove secrets from MongoDB after migration is verified.

type webhookSecretCleanupJob struct {
	job.Base       `bson:"job_base" json:"job_base" yaml:"job_base"`
	SubscriptionID string `bson:"subscription_id" json:"subscription_id" yaml:"subscription_id"`

	env evergreen.Environment
}

func makeWebhookSecretCleanupJob() *webhookSecretCleanupJob {
	j := &webhookSecretCleanupJob{
		Base: job.Base{
			JobType: amboy.JobType{
				Name:    webhookSecretCleanupJobName,
				Version: 0,
			},
		},
	}
	return j
}

// NewWebhookSecretCleanupJob creates a job to remove the secret field from MongoDB for a migrated subscription.
func NewWebhookSecretCleanupJob(subscriptionID, ts string) amboy.Job {
	j := makeWebhookSecretCleanupJob()
	j.SubscriptionID = subscriptionID
	j.SetID(fmt.Sprintf("%s.%s.%s", webhookSecretCleanupJobName, subscriptionID, ts))
	j.SetScopes([]string{
		fmt.Sprintf("%s.%s", webhookSecretCleanupJobName, subscriptionID),
	})
	j.SetEnqueueAllScopes(true)
	return j
}

func (j *webhookSecretCleanupJob) Run(ctx context.Context) {
	defer j.MarkComplete()

	if j.env == nil {
		j.env = evergreen.GetEnvironment()
	}

	sub := &event.Subscription{}
	if err := db.FindOneQ(ctx, event.SubscriptionsCollection, db.Query(bson.M{"_id": j.SubscriptionID}), sub); err != nil {
		if adb.ResultsNotFound(err) {
			return
		}
		j.AddError(errors.Wrapf(err, "finding subscription '%s'", j.SubscriptionID))
		return
	}

	if sub.Subscriber.Type != event.EvergreenWebhookSubscriberType {
		return
	}

	webhookSub, ok := sub.Subscriber.Target.(*event.WebhookSubscriber)
	if !ok {
		return
	}

	// Only clean up if already migrated and secret still exists in MongoDB.
	if webhookSub.SecretParameter == "" || len(webhookSub.Secret) == 0 {
		return
	}

	if err := db.Update(ctx, event.SubscriptionsCollection,
		bson.M{"_id": j.SubscriptionID},
		bson.M{
			"$unset": bson.M{"subscriber.target.secret": ""},
		},
	); err != nil {
		j.AddError(errors.Wrapf(err, "removing secret from MongoDB for subscription '%s'", j.SubscriptionID))
		return
	}

	grip.Info(message.Fields{
		"message":         "removed webhook secret from MongoDB",
		"subscription_id": j.SubscriptionID,
		"job_id":          j.ID(),
		"source":          "webhook-secret-cleanup",
	})
}

// findMigratedWebhookSubscriptionIDs returns IDs of webhook subscriptions that have been migrated but still have secrets in MongoDB.
func findMigratedWebhookSubscriptionIDs(ctx context.Context, limit int) ([]string, error) {
	subscriptions := []event.Subscription{}
	query := db.Query(bson.M{
		"subscriber.type":                    event.EvergreenWebhookSubscriberType,
		"subscriber.target.secret":           bson.M{"$exists": true, "$ne": nil},
		"subscriber.target.secret_parameter": bson.M{"$exists": true, "$ne": ""},
	}).Limit(limit)

	if err := db.FindAllQ(ctx, event.SubscriptionsCollection, query, &subscriptions); err != nil {
		return nil, errors.Wrap(err, "finding migrated webhook subscriptions with secrets")
	}

	ids := make([]string, 0, len(subscriptions))
	for _, sub := range subscriptions {
		ids = append(ids, sub.ID)
	}
	return ids, nil
}

// PopulateWebhookSecretCleanupJobs returns a QueueOperation that enqueues jobs to remove secrets from MongoDB for migrated subscriptions.
// NOT activated — add to crons_remote_five_minute.go ops list when ready.
func PopulateWebhookSecretCleanupJobs() amboy.QueueOperation {
	return func(ctx context.Context, queue amboy.Queue) error {
		ids, err := findMigratedWebhookSubscriptionIDs(ctx, webhookSecretCleanupBatchSize)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		grip.Info(message.Fields{
			"message": "enqueuing webhook secret cleanup jobs",
			"count":   len(ids),
			"source":  "webhook-secret-cleanup",
		})

		ts := utility.RoundPartOfHour(5).Format(TSFormat)
		catcher := grip.NewBasicCatcher()
		for _, id := range ids {
			catcher.Wrapf(amboy.EnqueueUniqueJob(ctx, queue, NewWebhookSecretCleanupJob(id, ts)),
				"enqueueing cleanup job for subscription '%s'", id)
		}
		return catcher.Resolve()
	}
}
