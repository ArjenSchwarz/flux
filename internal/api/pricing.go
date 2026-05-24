package api

import (
	"context"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
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

// Pricing error codes documented in AC 2.3 and the design's
// TransactionCanceledException → HTTP mapping table.
const (
	pricingCodeInvertedDates   = "inverted_dates"
	pricingCodeOverlap         = "overlap"
	pricingCodeRatePrecision   = "rate_precision"
	pricingCodeRateOutOfRange  = "rate_out_of_range"
	pricingCodeSecondOpenEnded = "second_open_ended"
	pricingCodeConcurrentWrite = "concurrent_open_ended_write"
	pricingCodeNotFound        = "not_found"
	pricingCodeBadRequest      = "bad_request"
	pricingCodeInternal        = "internal_error"
)

// pricingRateCap is the per-rate upper bound from Decision 12 — 10×
// the highest plausible AU retail tariff, catching order-of-magnitude
// typos without constraining legitimate use.
const pricingRateCap = 10.0

// pricingMaxEndDate is the "open-ended" sentinel value used by the
// in-memory overlap check. Lexicographic compare on a YYYY-MM-DD string
// keeps the check trivial.
const pricingMaxEndDate = "9999-12-31"

// pricingBodyMaxBytes caps inbound JSON on every pricing mutation. A
// maxed-out payload fits comfortably under 512 bytes; the cap leaves
// generous room for whitespace.
const pricingBodyMaxBytes = 4096
