package api

import (
	"context"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
)

// PricingStore is the api-package-local view of the pricing read+write
// surface. Lambda wiring constructs a *dynamo.DynamoPricingStore and
// passes it in directly; tests substitute an in-memory fake.
type PricingStore interface {
	ListPricing(ctx context.Context) ([]dynamo.PricingItem, error)
	GetPricing(ctx context.Context, id string) (*dynamo.PricingItem, error)
	GetSentinel(ctx context.Context) (*dynamo.PricingSentinel, error)
	PutPricing(ctx context.Context, item dynamo.PricingItem, prevOpenEndedID *string) error
	UpdatePricing(ctx context.Context, item dynamo.PricingItem, prevOpenEndedID *string) error
	DeletePricing(ctx context.Context, id string, prevOpenEndedID *string) error
	ReplaceOpenEnded(ctx context.Context, closingID, closingEndDate, updatedAt string, newItem dynamo.PricingItem) error
}

// Pricing error codes returned in the response envelope's "error" field.
// The single-plan band codes live in internal/plan, which owns the rules
// they report, and are re-exported here so every code the endpoint can
// emit is visible in one place.
const (
	pricingCodeInvertedDates      = plan.CodeInvertedDates
	pricingCodeBandWindowInvalid  = plan.CodeBandWindowInvalid
	pricingCodeBandOverlap        = plan.CodeBandOverlap
	pricingCodeMultipleFreeBands  = plan.CodeMultipleFreeBands
	pricingCodeSavingsRateMissing = plan.CodeSavingsRateMissing
	pricingCodeNoRatedBand        = plan.CodeNoRatedBand
	pricingCodeRatePrecision      = plan.CodeRatePrecision
	pricingCodeRateOutOfRange     = plan.CodeRateOutOfRange

	pricingCodeOverlap         = "overlap"
	pricingCodeSecondOpenEnded = "second_open_ended"
	pricingCodeConcurrentWrite = "concurrent_open_ended_write"
	pricingCodeLegacyShape     = "legacy_shape"
	pricingCodeNotFound        = "not_found"
	pricingCodeBadRequest      = "bad_request"
	pricingCodeInternal        = "internal_error"
)

// pricingMaxEndDate is the "open-ended" sentinel value used by the
// in-memory overlap check. Lexicographic compare on a YYYY-MM-DD string
// keeps the check trivial.
const pricingMaxEndDate = "9999-12-31"

// pricingBodyMaxBytes caps inbound JSON on every pricing mutation. The
// incoming time-of-use plan (three rates, two windows) fits in well under
// 512 bytes; the cap leaves generous room for a plan with many bands
// while still bounding a pathological payload.
const pricingBodyMaxBytes = 4096
