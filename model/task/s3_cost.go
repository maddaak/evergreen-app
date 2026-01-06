package task

import (
	"context"

	"github.com/evergreen-ci/evergreen"
	"github.com/mongodb/grip"
	"github.com/pkg/errors"
)

// S3Usage tracks S3 API usage for cost calculation
type S3Usage struct {
	// NumPutRequests is the number of S3 PutObject API requests made
	NumPutRequests int `bson:"num_put_requests,omitempty" json:"num_put_requests,omitempty"`
}

// IsZero implements bsoncodec.Zeroer for BSON marshalling.
func (s S3Usage) IsZero() bool {
	return s.NumPutRequests == 0
}

// IncrementPutRequests increments the PUT request counter
func (s *S3Usage) IncrementPutRequests(count int) {
	s.NumPutRequests += count
}

// CalculateCost calculates the total S3 cost based on usage.
// Returns 0 if config cannot be retrieved.
func (s *S3Usage) CalculateCost(ctx context.Context) (float64, error) {
	if s.NumPutRequests <= 0 {
		return 0.0, nil
	}

	settings, err := evergreen.GetConfig(ctx)
	if err != nil || settings == nil {
		grip.Error(errors.Wrap(err, "getting config for S3 cost calculation"))
		return 0.0, nil
	}

	return s.CalculateCostWithConfig(&settings.Cost)
}

// CalculateCostWithConfig calculates the total S3 cost based on usage using a pre-fetched config.
// This is useful when calculating costs for multiple files to avoid fetching config repeatedly.
func (s *S3Usage) CalculateCostWithConfig(costConfig *evergreen.CostConfig) (float64, error) {
	if s.NumPutRequests <= 0 {
		return 0.0, nil
	}

	if costConfig == nil {
		return 0.0, nil
	}

	cost, err := costConfig.CalculateS3Cost(s.NumPutRequests)
	if err != nil {
		return 0.0, errors.Wrap(err, "calculating S3 cost")
	}

	return cost, nil
}

// CalculateS3CostForTask function that calculates S3 cost for a task's usage
func CalculateS3CostForTask(ctx context.Context, s3Usage S3Usage) (float64, error) {
	return s3Usage.CalculateCost(ctx)
}
