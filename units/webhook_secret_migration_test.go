package units

import (
	"testing"

	"github.com/evergreen-ci/evergreen/cloud/parameterstore/fakeparameter"
	"github.com/evergreen-ci/evergreen/db"
	"github.com/evergreen-ci/evergreen/mock"
	"github.com/evergreen-ci/evergreen/model/event"
	"github.com/evergreen-ci/evergreen/util"
	"github.com/mongodb/amboy/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

func setupWebhookMigrationTest(t *testing.T) *mock.Environment {
	env := &mock.Environment{}
	require.NoError(t, env.Configure(t.Context()))
	require.NoError(t, db.ClearCollections(event.SubscriptionsCollection, fakeparameter.Collection))
	t.Cleanup(func() {
		require.NoError(t, db.ClearCollections(event.SubscriptionsCollection, fakeparameter.Collection))
	})
	return env
}

// insertUnmigratedWebhookSubscription inserts a subscription document directly
// into MongoDB with a plaintext secret and no secret_parameter, simulating
// a pre-migration state.
func insertUnmigratedWebhookSubscription(t *testing.T, id string, secret string) {
	t.Helper()
	doc := bson.M{
		"_id":           id,
		"resource_type": "PATCH",
		"trigger":       "outcome",
		"selectors":     bson.A{bson.M{"type": "id", "data": "test"}},
		"subscriber": bson.M{
			"type": event.EvergreenWebhookSubscriberType,
			"target": bson.M{
				"url":    "https://example.com/webhook",
				"secret": []byte(secret),
			},
		},
		"owner":      "test-owner",
		"owner_type": "person",
	}
	require.NoError(t, db.Insert(t.Context(), event.SubscriptionsCollection, doc))
}

func TestWebhookSecretMigrationJobFactory(t *testing.T) {
	factory, err := registry.GetJobFactory(webhookSecretMigrationJobName)
	require.NoError(t, err)
	require.NotNil(t, factory)
	j := factory()
	require.NotNil(t, j)
	assert.Equal(t, webhookSecretMigrationJobName, j.Type().Name)
}

func TestWebhookSecretMigrationJobConstructor(t *testing.T) {
	j := NewWebhookSecretMigrationJob("sub-123", "2025-01-01")
	require.NotNil(t, j)
	assert.Equal(t, "webhook-secret-migration.sub-123.2025-01-01", j.ID())
	assert.Equal(t, webhookSecretMigrationJobName, j.Type().Name)
}

func TestWebhookSecretMigrationJobRun(t *testing.T) {
	t.Run("MigratesSecretToParameterStore", func(t *testing.T) {
		env := setupWebhookMigrationTest(t)
		insertUnmigratedWebhookSubscription(t, "sub-migrate", "my-secret")

		j := makeWebhookSecretMigrationJob()
		j.env = env
		j.SubscriptionID = "sub-migrate"
		j.SetID("test-migrate")

		j.Run(t.Context())
		require.True(t, j.Status().Completed)
		require.False(t, j.HasErrors())

		// Verify the DB document was updated.
		raw := bson.M{}
		require.NoError(t, db.FindOneQ(t.Context(), event.SubscriptionsCollection, db.Query(bson.M{"_id": "sub-migrate"}), &raw))
		subscriberRaw, ok := raw["subscriber"].(bson.M)
		require.True(t, ok)
		targetRaw, ok := subscriberRaw["target"].(bson.M)
		require.True(t, ok)
		assert.NotNil(t, targetRaw["secret"], "secret should be kept in MongoDB for phase 2 cleanup")
		assert.NotEmpty(t, targetRaw["secret_parameter"], "secret_parameter should be set")

		// Verify the secret is in the fake Parameter Store.
		paramName, ok := targetRaw["secret_parameter"].(string)
		require.True(t, ok)
		require.NotEmpty(t, paramName)
		fakeParams, err := fakeparameter.FindByIDs(t.Context(), paramName)
		require.NoError(t, err)
		require.Len(t, fakeParams, 1)
		assert.Equal(t, "my-secret", fakeParams[0].Value)
	})

	t.Run("SkipsAlreadyMigratedSubscription", func(t *testing.T) {
		env := setupWebhookMigrationTest(t)

		// Insert a subscription that already has secret_parameter set.
		doc := bson.M{
			"_id":           "sub-already-migrated",
			"resource_type": "PATCH",
			"trigger":       "outcome",
			"selectors":     bson.A{bson.M{"type": "id", "data": "test"}},
			"subscriber": bson.M{
				"type": event.EvergreenWebhookSubscriberType,
				"target": bson.M{
					"url":              "https://example.com/webhook",
					"secret_parameter": "/some/existing/param",
				},
			},
			"owner":      "test-owner",
			"owner_type": "person",
		}
		require.NoError(t, db.Insert(t.Context(), event.SubscriptionsCollection, doc))

		j := makeWebhookSecretMigrationJob()
		j.env = env
		j.SubscriptionID = "sub-already-migrated"
		j.SetID("test-already-migrated")

		j.Run(t.Context())
		require.True(t, j.Status().Completed)
		require.False(t, j.HasErrors())

		// No new parameters should have been created.
		allParams, err := fakeparameter.FindByIDs(t.Context())
		require.NoError(t, err)
		assert.Empty(t, allParams)
	})

	t.Run("SkipsSubscriptionNotFound", func(t *testing.T) {
		env := setupWebhookMigrationTest(t)

		j := makeWebhookSecretMigrationJob()
		j.env = env
		j.SubscriptionID = "nonexistent"
		j.SetID("test-not-found")

		j.Run(t.Context())
		require.True(t, j.Status().Completed)
		require.False(t, j.HasErrors())
	})

	t.Run("SkipsEmptySecret", func(t *testing.T) {
		env := setupWebhookMigrationTest(t)

		// Insert a webhook subscription with no secret.
		doc := bson.M{
			"_id":           "sub-no-secret",
			"resource_type": "PATCH",
			"trigger":       "outcome",
			"selectors":     bson.A{bson.M{"type": "id", "data": "test"}},
			"subscriber": bson.M{
				"type": event.EvergreenWebhookSubscriberType,
				"target": bson.M{
					"url": "https://example.com/webhook",
				},
			},
			"owner":      "test-owner",
			"owner_type": "person",
		}
		require.NoError(t, db.Insert(t.Context(), event.SubscriptionsCollection, doc))

		j := makeWebhookSecretMigrationJob()
		j.env = env
		j.SubscriptionID = "sub-no-secret"
		j.SetID("test-no-secret")

		j.Run(t.Context())
		require.True(t, j.Status().Completed)
		require.False(t, j.HasErrors())

		// No parameters should have been created.
		allParams, err := fakeparameter.FindByIDs(t.Context())
		require.NoError(t, err)
		assert.Empty(t, allParams)
	})
}

func TestFindUnmigratedWebhookSubscriptionIDs(t *testing.T) {
	setupWebhookMigrationTest(t)

	// Insert 3 unmigrated and 1 already-migrated subscription.
	insertUnmigratedWebhookSubscription(t, "unmigrated-1", "secret-1")
	insertUnmigratedWebhookSubscription(t, "unmigrated-2", "secret-2")
	insertUnmigratedWebhookSubscription(t, "unmigrated-3", "secret-3")

	migratedDoc := bson.M{
		"_id":           "already-migrated",
		"resource_type": "PATCH",
		"trigger":       "outcome",
		"selectors":     bson.A{bson.M{"type": "id", "data": "test"}},
		"subscriber": bson.M{
			"type": event.EvergreenWebhookSubscriberType,
			"target": bson.M{
				"url":              "https://example.com/webhook",
				"secret_parameter": "/some/param",
			},
		},
		"owner":      "test-owner",
		"owner_type": "person",
	}
	require.NoError(t, db.Insert(t.Context(), event.SubscriptionsCollection, migratedDoc))

	t.Run("ReturnsOnlyUnmigrated", func(t *testing.T) {
		ids, err := findUnmigratedWebhookSubscriptionIDs(t.Context(), 100)
		require.NoError(t, err)
		assert.Len(t, ids, 3)
		assert.NotContains(t, ids, "already-migrated")
	})

	t.Run("RespectsLimit", func(t *testing.T) {
		ids, err := findUnmigratedWebhookSubscriptionIDs(t.Context(), 2)
		require.NoError(t, err)
		assert.Len(t, ids, 2)
	})
}

func TestWebhookSecretMigrationParameterPath(t *testing.T) {
	subscriptionID := "test-sub-id"
	expected := event.GetWebhookSecretParameterPath(subscriptionID)
	actual := "webhooks/" + util.GetSHA256Hash(subscriptionID) + "/secret"
	assert.Equal(t, expected, actual)
}

// insertMigratedWebhookSubscription inserts a subscription that has both a
// secret and a secret_parameter, simulating a post-migration state.
func insertMigratedWebhookSubscription(t *testing.T, id, secret, paramPath string) {
	t.Helper()
	doc := bson.M{
		"_id":           id,
		"resource_type": "PATCH",
		"trigger":       "outcome",
		"selectors":     bson.A{bson.M{"type": "id", "data": "test"}},
		"subscriber": bson.M{
			"type": event.EvergreenWebhookSubscriberType,
			"target": bson.M{
				"url":              "https://example.com/webhook",
				"secret":           []byte(secret),
				"secret_parameter": paramPath,
			},
		},
		"owner":      "test-owner",
		"owner_type": "person",
	}
	require.NoError(t, db.Insert(t.Context(), event.SubscriptionsCollection, doc))
}

func TestWebhookSecretCleanupJobFactory(t *testing.T) {
	factory, err := registry.GetJobFactory(webhookSecretCleanupJobName)
	require.NoError(t, err)
	require.NotNil(t, factory)
	j := factory()
	require.NotNil(t, j)
	assert.Equal(t, webhookSecretCleanupJobName, j.Type().Name)
}

func TestWebhookSecretCleanupJobConstructor(t *testing.T) {
	j := NewWebhookSecretCleanupJob("sub-456", "2025-01-01")
	require.NotNil(t, j)
	assert.Equal(t, "webhook-secret-cleanup.sub-456.2025-01-01", j.ID())
	assert.Equal(t, webhookSecretCleanupJobName, j.Type().Name)
}

func TestWebhookSecretCleanupJobRun(t *testing.T) {
	t.Run("RemovesSecretFromMongoDB", func(t *testing.T) {
		env := setupWebhookMigrationTest(t)
		insertMigratedWebhookSubscription(t, "sub-cleanup", "old-secret", "/some/param/path")

		j := makeWebhookSecretCleanupJob()
		j.env = env
		j.SubscriptionID = "sub-cleanup"
		j.SetID("test-cleanup")

		j.Run(t.Context())
		require.True(t, j.Status().Completed)
		require.False(t, j.HasErrors())

		raw := bson.M{}
		require.NoError(t, db.FindOneQ(t.Context(), event.SubscriptionsCollection, db.Query(bson.M{"_id": "sub-cleanup"}), &raw))
		subscriberRaw, ok := raw["subscriber"].(bson.M)
		require.True(t, ok)
		targetRaw, ok := subscriberRaw["target"].(bson.M)
		require.True(t, ok)
		assert.Nil(t, targetRaw["secret"], "secret should be removed from MongoDB")
		assert.Equal(t, "/some/param/path", targetRaw["secret_parameter"], "secret_parameter should remain")
	})

	t.Run("SkipsNotMigratedSubscription", func(t *testing.T) {
		env := setupWebhookMigrationTest(t)
		insertUnmigratedWebhookSubscription(t, "sub-not-migrated", "still-in-mongo")

		j := makeWebhookSecretCleanupJob()
		j.env = env
		j.SubscriptionID = "sub-not-migrated"
		j.SetID("test-not-migrated")

		j.Run(t.Context())
		require.True(t, j.Status().Completed)
		require.False(t, j.HasErrors())

		// Secret should still be in MongoDB since it was not migrated.
		raw := bson.M{}
		require.NoError(t, db.FindOneQ(t.Context(), event.SubscriptionsCollection, db.Query(bson.M{"_id": "sub-not-migrated"}), &raw))
		subscriberRaw, ok := raw["subscriber"].(bson.M)
		require.True(t, ok)
		targetRaw, ok := subscriberRaw["target"].(bson.M)
		require.True(t, ok)
		assert.NotNil(t, targetRaw["secret"], "secret should remain since subscription is not migrated")
	})

	t.Run("SkipsSubscriptionNotFound", func(t *testing.T) {
		env := setupWebhookMigrationTest(t)

		j := makeWebhookSecretCleanupJob()
		j.env = env
		j.SubscriptionID = "nonexistent"
		j.SetID("test-cleanup-not-found")

		j.Run(t.Context())
		require.True(t, j.Status().Completed)
		require.False(t, j.HasErrors())
	})
}

func TestFindMigratedWebhookSubscriptionIDs(t *testing.T) {
	setupWebhookMigrationTest(t)

	insertMigratedWebhookSubscription(t, "migrated-1", "secret-1", "/param/1")
	insertMigratedWebhookSubscription(t, "migrated-2", "secret-2", "/param/2")
	insertUnmigratedWebhookSubscription(t, "unmigrated-1", "secret-3")

	t.Run("ReturnsOnlyMigrated", func(t *testing.T) {
		ids, err := findMigratedWebhookSubscriptionIDs(t.Context(), 100)
		require.NoError(t, err)
		assert.Len(t, ids, 2)
		assert.NotContains(t, ids, "unmigrated-1")
	})

	t.Run("RespectsLimit", func(t *testing.T) {
		ids, err := findMigratedWebhookSubscriptionIDs(t.Context(), 1)
		require.NoError(t, err)
		assert.Len(t, ids, 1)
	})
}
