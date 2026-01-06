package evergreen

import (
	"context"

	"github.com/mongodb/anser/bsonutil"
	"github.com/mongodb/grip"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
)

const (
	S3PutRequestCost = 0.000005        // AWS pricing: $0.005 per 1,000 PUT requests
	S3PartSize       = 5 * 1024 * 1024 // 5MB (AWS minimum multipart part size)
)

// CostConfig represents the admin config section for finance-related settings.
type CostConfig struct {
	// FinanceFormula determines the weighting/percentage of the two parts of total cost: savingsPlanComponent and onDemandComponent.
	FinanceFormula float64 `bson:"finance_formula" json:"finance_formula" yaml:"finance_formula"`
	// SavingsPlanDiscount is the discount rate (0.0-1.0) applied to savings plan pricing
	SavingsPlanDiscount float64 `bson:"savings_plan_discount" json:"savings_plan_discount" yaml:"savings_plan_discount"`
	// OnDemandDiscount is the discount rate (0.0-1.0) applied to on-demand pricing
	OnDemandDiscount float64 `bson:"on_demand_discount" json:"on_demand_discount" yaml:"on_demand_discount"`
}

var (
	financeConfigFormulaKey             = bsonutil.MustHaveTag(CostConfig{}, "FinanceFormula")
	financeConfigSavingsPlanDiscountKey = bsonutil.MustHaveTag(CostConfig{}, "SavingsPlanDiscount")
	financeConfigOnDemandDiscountKey    = bsonutil.MustHaveTag(CostConfig{}, "OnDemandDiscount")
)

func (*CostConfig) SectionId() string { return "cost" }

func (c *CostConfig) Get(ctx context.Context) error {
	return getConfigSection(ctx, c)
}

func (c *CostConfig) Set(ctx context.Context) error {
	return errors.Wrapf(setConfigSection(ctx, c.SectionId(), bson.M{
		"$set": bson.M{
			financeConfigFormulaKey:             c.FinanceFormula,
			financeConfigSavingsPlanDiscountKey: c.SavingsPlanDiscount,
			financeConfigOnDemandDiscountKey:    c.OnDemandDiscount,
		}}), "updating config section '%s'", c.SectionId(),
	)
}

func (c *CostConfig) ValidateAndDefault() error {
	catcher := grip.NewBasicCatcher()

	if c.FinanceFormula < 0.0 || c.FinanceFormula > 1.0 {
		catcher.Add(errors.New("finance formula must be between 0.0 and 1.0"))
	}
	if c.SavingsPlanDiscount < 0.0 || c.SavingsPlanDiscount > 1.0 {
		catcher.Add(errors.New("savings plan discount must be between 0.0 and 1.0"))
	}
	if c.OnDemandDiscount < 0.0 || c.OnDemandDiscount > 1.0 {
		catcher.Add(errors.New("on demand discount must be between 0.0 and 1.0"))
	}

	return catcher.Resolve()
}

// IsConfigured returns true if any finance config field is set.
func (c *CostConfig) IsConfigured() bool {
	return c.FinanceFormula != 0 || c.SavingsPlanDiscount != 0 || c.OnDemandDiscount != 0
}

// CalculateS3Cost returns the cost of S3 PUT requests with OnDemandDiscount applied.
// Returns an error if OnDemandDiscount is invalid (< 0 or > 1).
func (c *CostConfig) CalculateS3Cost(numPutRequests int) (float64, error) {
	if numPutRequests <= 0 {
		return 0.0, nil
	}

	if c.OnDemandDiscount < 0.0 || c.OnDemandDiscount > 1.0 {
		return 0.0, errors.Errorf("invalid OnDemandDiscount: %f (must be between 0.0 and 1.0)", c.OnDemandDiscount)
	}

	grip.Infof("calculating S3 cost using OnDemandDiscount: %f", c.OnDemandDiscount)

	return float64(numPutRequests) * S3PutRequestCost * (1 - c.OnDemandDiscount), nil
}

// CalculatePutRequests returns the number of S3 PUT API calls needed to upload a file.
// App uses multipart upload: CreateMultipartUpload + UploadPart(s) + CompleteMultipartUpload.
func CalculatePutRequests(fileSize int64) int {
	if fileSize <= 0 {
		return 0
	}

	if fileSize <= S3PartSize {
		return 3 // Create + 1 Part + Complete
	}

	numParts := int((fileSize + S3PartSize - 1) / S3PartSize) // Ceiling division
	return 1 + numParts + 1
}
