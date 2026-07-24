package dynamo

import (
	"context"
	"testing"

	"github.com/ArjenSchwarz/flux/internal/plan"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyRow builds the raw attribute map of a pre-migration three-rate row.
// Built as a raw map rather than by marshalling a struct because that is
// exactly the shape the read path has to recognise.
func legacyRow(id, startDate string, endDate *string) map[string]types.AttributeValue {
	av := map[string]types.AttributeValue{
		"pricingId":          &types.AttributeValueMemberS{Value: id},
		"startDate":          &types.AttributeValueMemberS{Value: startDate},
		"peakRate":           &types.AttributeValueMemberN{Value: "0.2873"},
		"feedInRate":         &types.AttributeValueMemberN{Value: "0.05"},
		"offPeakSavingsRate": &types.AttributeValueMemberN{Value: "0.15"},
		"createdAt":          &types.AttributeValueMemberS{Value: "2026-05-23T10:00:00Z"},
		"updatedAt":          &types.AttributeValueMemberS{Value: "2026-05-23T10:00:00Z"},
	}
	if endDate != nil {
		av["endDate"] = &types.AttributeValueMemberS{Value: *endDate}
	}
	return av
}

// TestIsLegacyPricingRow pins detection to the raw attribute map. Detecting
// via a decoded struct is not an option: attributevalue silently drops
// unknown attributes, so a legacy row decodes into the band shape as a
// zero-rate plan with no windows rather than as something recognisably wrong.
func TestIsLegacyPricingRow(t *testing.T) {
	t.Parallel()
	bandRow, err := attributevalue.MarshalMap(bandPricingItem("p-1", "2026-01-01", nil))
	require.NoError(t, err)

	tests := map[string]struct {
		row  map[string]types.AttributeValue
		want bool
	}{
		"legacy row":     {row: legacyRow("p-1", "2026-01-01", nil), want: true},
		"band-shape row": {row: bandRow, want: false},
		"empty map":      {row: map[string]types.AttributeValue{}, want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsLegacyPricingRow(tc.row))
		})
	}
}

// TestTransformLegacyPricing covers AC 5.1: the legacy period maps to a free
// band matching the window its historical data was computed under, a default
// rate carrying the former flat rate, the unchanged feed-in rate, and a
// savings reference rate equal to the former off-peak savings rate.
func TestTransformLegacyPricing(t *testing.T) {
	t.Parallel()
	legacyEnd := "2026-07-31"
	got, err := TransformLegacyPricing(LegacyPricingItem{
		PricingID:          "p-1",
		StartDate:          "2026-01-01",
		EndDate:            &legacyEnd,
		PeakRate:           0.2873,
		FeedInRate:         0.05,
		OffPeakSavingsRate: 0.15,
		CreatedAt:          "2026-05-23T10:00:00Z",
		UpdatedAt:          "2026-05-23T10:00:00Z",
	})
	require.NoError(t, err)

	savings := 0.15
	// The legacy inclusive end 2026-07-31 becomes the exclusive 2026-08-01,
	// so the period still prices through 31 July and no day is gained or
	// lost (AC 5.2).
	wantEnd := "2026-08-01"
	assert.Equal(t, PricingItem{
		PricingID:            "p-1",
		StartDate:            "2026-01-01",
		EndDate:              &wantEnd,
		DefaultRate:          0.2873,
		Windows:              []PricingWindow{{Start: "11:00", End: "14:00", Free: true}},
		FeedInRate:           0.05,
		SavingsReferenceRate: &savings,
		CreatedAt:            "2026-05-23T10:00:00Z",
		UpdatedAt:            "2026-05-23T10:00:00Z",
	}, got)
}

// TestTransformLegacyPricingOpenEnded pins that an open-ended legacy row stays
// open-ended — there is no end date to shift.
func TestTransformLegacyPricingOpenEnded(t *testing.T) {
	t.Parallel()
	got, err := TransformLegacyPricing(LegacyPricingItem{
		PricingID: "p-1", StartDate: "2026-01-01",
		PeakRate: 0.3, FeedInRate: 0.05, OffPeakSavingsRate: 0.1,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	})
	require.NoError(t, err)
	assert.Nil(t, got.EndDate)
}

// TestTransformLegacyPricingEndDateShift covers the inclusive → exclusive
// mapping across month, year, and leap-day boundaries.
func TestTransformLegacyPricingEndDateShift(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ legacy, want string }{
		"mid month":       {legacy: "2026-03-14", want: "2026-03-15"},
		"end of month":    {legacy: "2026-04-30", want: "2026-05-01"},
		"end of year":     {legacy: "2026-12-31", want: "2027-01-01"},
		"leap day":        {legacy: "2028-02-29", want: "2028-03-01"},
		"end of february": {legacy: "2026-02-28", want: "2026-03-01"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			end := tc.legacy
			got, err := TransformLegacyPricing(LegacyPricingItem{
				PricingID: "p-1", StartDate: "2020-01-01", EndDate: &end,
				PeakRate: 0.3, FeedInRate: 0.05, OffPeakSavingsRate: 0.1,
			})
			require.NoError(t, err)
			require.NotNil(t, got.EndDate)
			assert.Equal(t, tc.want, *got.EndDate)
		})
	}
}

// TestTransformLegacyPricingRejectsMalformedEndDate pins that an unparseable
// end date is an error rather than a silently dropped one — the migration
// must abort, not quietly open-end a closed period.
func TestTransformLegacyPricingRejectsMalformedEndDate(t *testing.T) {
	t.Parallel()
	end := "31-12-2026"
	_, err := TransformLegacyPricing(LegacyPricingItem{
		PricingID: "p-1", StartDate: "2026-01-01", EndDate: &end,
	})
	require.Error(t, err)
}

// TestTransformedLegacyPlanPreservesTheHistoricalWindow ties the transform
// back to the domain: the resulting plan validates, prices the same days, and
// exposes the 11:00–14:00 window historical off-peak values were computed
// under.
func TestTransformedLegacyPlanPreservesTheHistoricalWindow(t *testing.T) {
	t.Parallel()
	legacyEnd := "2026-07-31"
	item, err := TransformLegacyPricing(LegacyPricingItem{
		PricingID: "p-1", StartDate: "2026-01-01", EndDate: &legacyEnd,
		PeakRate: 0.2873, FeedInRate: 0.05, OffPeakSavingsRate: 0.15,
	})
	require.NoError(t, err)

	p := item.Plan()
	assert.Empty(t, p.Validate())
	assert.True(t, p.Covers("2026-07-31"), "the legacy inclusive end date must still be priced")
	assert.False(t, p.Covers("2026-08-01"), "the exclusive end date must not be priced")

	start, end, ok := p.FreeWindowMinutes()
	require.True(t, ok)
	assert.Equal(t, 11*60, start)
	assert.Equal(t, 14*60, end)
}

// TestListPricingTransformsLegacyRows covers Q28: until the migration runs,
// the read path converts legacy rows so a band-aware poller or Lambda
// deployed first still resolves windows and serves plans correctly.
func TestListPricingTransformsLegacyRows(t *testing.T) {
	t.Parallel()
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	legacyEnd := "2026-07-31"
	api.items["p-legacy"] = legacyRow("p-legacy", "2026-01-01", &legacyEnd)
	newItem := bandPricingItem("p-band", "2026-08-01", nil)
	require.NoError(t, store.PutPricing(context.Background(), newItem, nil))

	got, err := store.ListPricing(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "p-legacy", got[0].PricingID)
	assert.Equal(t, 0.2873, got[0].DefaultRate)
	assert.Equal(t, []PricingWindow{{Start: "11:00", End: "14:00", Free: true}}, got[0].Windows)
	require.NotNil(t, got[0].SavingsReferenceRate)
	assert.Equal(t, 0.15, *got[0].SavingsReferenceRate)
	require.NotNil(t, got[0].EndDate)
	assert.Equal(t, "2026-08-01", *got[0].EndDate)

	assert.Equal(t, newItem, got[1], "band-shape rows pass through unchanged")
}

// TestListPricingLeavesTheSentinelAlone pins that the sentinel row is never
// mistaken for a pricing row by either the legacy detector or the transform.
func TestListPricingLeavesTheSentinelAlone(t *testing.T) {
	t.Parallel()
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	openID := "p-legacy"
	sentinel := PricingSentinel{PricingID: PricingSentinelID, OpenEndedID: &openID, UpdatedAt: "2026-05-23T10:00:00Z"}
	av, err := attributevalue.MarshalMap(sentinel)
	require.NoError(t, err)
	api.items[PricingSentinelID] = av
	api.items["p-legacy"] = legacyRow("p-legacy", "2026-01-01", nil)

	got, err := store.ListPricing(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "p-legacy", got[0].PricingID)

	gotSentinel, err := store.GetSentinel(context.Background())
	require.NoError(t, err)
	require.NotNil(t, gotSentinel)
	assert.Equal(t, sentinel, *gotSentinel)
}

// TestGetPricingTransformsLegacyRow covers the single-row read path, which
// needs the same conversion as the list path.
func TestGetPricingTransformsLegacyRow(t *testing.T) {
	t.Parallel()
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())
	api.items["p-legacy"] = legacyRow("p-legacy", "2026-01-01", nil)

	got, err := store.GetPricing(context.Background(), "p-legacy")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 0.2873, got.DefaultRate)
	assert.Equal(t, []PricingWindow{{Start: "11:00", End: "14:00", Free: true}}, got.Windows)
	assert.Nil(t, got.EndDate)
}

// TestPlansFromItems pins the conversion the poller and Lambda consume:
// storage rows in, domain plans out, ready for PlanFor / FreeWindow.
func TestPlansFromItems(t *testing.T) {
	t.Parallel()
	closing := bandPricingItem("p-old", "2026-01-01", strPtr("2026-08-01"))
	successor := bandPricingItem("p-new", "2026-08-01", nil)
	successor.Windows = []PricingWindow{
		{Start: "10:00", End: "15:00", Free: true},
		{Start: "01:00", End: "06:00", Rate: floatPtr(0.28)},
	}

	plans := PlansFromItems([]PricingItem{closing, successor})
	require.Len(t, plans, 2)

	got, ok := plan.PlanFor(plans, "2026-08-01")
	require.True(t, ok)
	assert.Equal(t, "p-new", got.ID, "AC 2.2: the switch day belongs to the successor")

	start, end, ok := plan.FreeWindow(plans, "2026-08-01")
	require.True(t, ok)
	assert.Equal(t, 10*60, start)
	assert.Equal(t, 15*60, end)

	start, end, ok = plan.FreeWindow(plans, "2026-07-31")
	require.True(t, ok)
	assert.Equal(t, 11*60, start)
	assert.Equal(t, 14*60, end)
}

// TestPricingItemPlanCarriesWindowRates pins that a rated window's rate
// survives the storage → domain conversion, and that a free window's absent
// rate becomes the zero the domain ignores.
func TestPricingItemPlanCarriesWindowRates(t *testing.T) {
	t.Parallel()
	item := bandPricingItem("p-1", "2026-01-01", nil)
	item.Windows = []PricingWindow{
		{Start: "10:00", End: "15:00", Free: true},
		{Start: "01:00", End: "06:00", Rate: floatPtr(0.28)},
	}
	assert.Equal(t, []plan.Window{
		{Start: "10:00", End: "15:00", Free: true},
		{Start: "01:00", End: "06:00", Rate: 0.28},
	}, item.Plan().Windows)
}
