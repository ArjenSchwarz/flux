package dynamo

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplaceOpenEnded_SameDaySuccession covers AC 2.2 under exclusive end
// dates: the closing row's end date is the successor's start date, literally
// the same string, and the switch day is priced by the successor.
func TestReplaceOpenEnded_SameDaySuccession(t *testing.T) {
	t.Parallel()
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	closing := bandPricingItem("p-old", "2026-01-01", nil)
	require.NoError(t, store.PutPricing(context.Background(), closing, nil))

	successor := bandPricingItem("p-new", "2026-08-01", nil)
	successor.Windows = []PricingWindow{
		{Start: "10:00", End: "15:00", Free: true},
		{Start: "01:00", End: "06:00", Rate: floatPtr(0.28)},
	}
	require.NoError(t, store.ReplaceOpenEnded(context.Background(),
		"p-old", successor.StartDate, "2026-07-01T10:00:00Z", successor))

	rows, err := store.ListPricing(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.NotNil(t, rows[0].EndDate)
	assert.Equal(t, "2026-08-01", *rows[0].EndDate,
		"the closing row's exclusive end date is the successor's start date, with no ±1 arithmetic")

	plans := PlansFromItems(rows)
	assert.True(t, plans[0].Covers("2026-07-31"), "the predecessor's last priced day is the switch day eve")
	assert.False(t, plans[0].Covers("2026-08-01"))
	assert.True(t, plans[1].Covers("2026-08-01"), "AC 2.2: the switch day belongs to the successor")
}

// TestReplaceOpenEnded_RejectsLegacyClosingRow covers Q32. The closing write
// is a partial UpdateItem, so patching a not-yet-migrated row would leave it
// still legacy-detected but carrying an exclusive end date — which the read
// transform and then the migration would each shift by a day. Rejecting is
// the guard; the cutover order already runs the migration first.
func TestReplaceOpenEnded_RejectsLegacyClosingRow(t *testing.T) {
	t.Parallel()
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())
	api.items["p-legacy"] = legacyRow("p-legacy", "2026-01-01", nil)

	err := store.ReplaceOpenEnded(context.Background(),
		"p-legacy", "2026-08-01", "2026-07-01T10:00:00Z",
		bandPricingItem("p-new", "2026-08-01", nil))

	require.ErrorIs(t, err, ErrPricingLegacyShape)
	assert.True(t, IsLegacyPricingRow(api.items["p-legacy"]),
		"the legacy row must be left exactly as it was")
	assert.NotContains(t, api.items, "p-new", "the successor must not be written")
}

// TestReplaceOpenEnded_MissingClosingRow pins that a vanished closing row is
// an error rather than a silent half-succession.
func TestReplaceOpenEnded_MissingClosingRow(t *testing.T) {
	t.Parallel()
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	err := store.ReplaceOpenEnded(context.Background(),
		"p-missing", "2026-08-01", "2026-07-01T10:00:00Z",
		bandPricingItem("p-new", "2026-08-01", nil))

	require.ErrorIs(t, err, ErrPricingConcurrentWrite)
	assert.NotContains(t, api.items, "p-new")
}

// TestReplaceOpenEnded_WritesTheClosingEndDateVerbatim pins that the store
// stores what it is handed. Deriving the closing date is the caller's job
// under the switch-day semantics, so no date arithmetic belongs here.
func TestReplaceOpenEnded_WritesTheClosingEndDateVerbatim(t *testing.T) {
	t.Parallel()
	var captured *dynamodb.TransactWriteItemsInput
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(&recordingPricingAPI{inMemoryPricingAPI: api, onTransact: func(in *dynamodb.TransactWriteItemsInput) {
		captured = in
	}}, pricingTestTable())
	require.NoError(t, store.PutPricing(context.Background(), bandPricingItem("p-old", "2026-01-01", nil), nil))

	require.NoError(t, store.ReplaceOpenEnded(context.Background(),
		"p-old", "2026-08-01", "2026-07-01T10:00:00Z",
		bandPricingItem("p-new", "2026-08-01", nil)))

	require.NotNil(t, captured)
	require.Len(t, captured.TransactItems, 3, "sentinel, closing row, new row")
	closing := captured.TransactItems[1].Update
	require.NotNil(t, closing)
	end, isString := closing.ExpressionAttributeValues[":end"].(*types.AttributeValueMemberS)
	require.True(t, isString)
	assert.Equal(t, "2026-08-01", end.Value)
}

// recordingPricingAPI wraps the in-memory fake to capture the transaction
// input while still applying it, so a test can assert on the request shape
// and the resulting state in the same run.
type recordingPricingAPI struct {
	*inMemoryPricingAPI
	onTransact func(*dynamodb.TransactWriteItemsInput)
}

func (r *recordingPricingAPI) TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	if r.onTransact != nil {
		r.onTransact(params)
	}
	return r.inMemoryPricingAPI.TransactWriteItems(ctx, params, optFns...)
}
