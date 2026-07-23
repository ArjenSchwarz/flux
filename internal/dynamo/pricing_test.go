package dynamo

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPricingItemJSONWireShape pins the on-the-wire JSON keys the Swift
// client decodes into PricingPeriod. PricingID must serialise as "id" so
// the Swift Identifiable conformance works.
func TestPricingItemJSONWireShape(t *testing.T) {
	end := "2027-01-01"
	item := bandPricingItem("pricing-1", "2026-01-01", &end)
	item.Windows = []PricingWindow{
		{Start: "10:00", End: "15:00", Free: true},
		{Start: "01:00", End: "06:00", Rate: floatPtr(0.28)},
	}
	encoded, err := json.Marshal(item)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(encoded, &raw))

	assert.NotContains(t, raw, "pricingId")
	assert.NotContains(t, raw, "PricingID")

	expected := map[string]any{
		"id":        "pricing-1",
		"startDate": "2026-01-01",
		// Exclusive switch date (Decision 5): this period's last priced day
		// is 2026-12-31.
		"endDate":     "2027-01-01",
		"defaultRate": 0.2873,
		"windows": []any{
			map[string]any{"start": "10:00", "end": "15:00", "free": true},
			map[string]any{"start": "01:00", "end": "06:00", "free": false, "rate": 0.28},
		},
		"feedInRate":           0.05,
		"savingsReferenceRate": 0.15,
		"createdAt":            "2026-05-23T10:00:00Z",
		"updatedAt":            "2026-05-23T10:00:00Z",
	}
	for key, want := range expected {
		assert.Equal(t, want, raw[key], "wire shape key %q", key)
	}
	assert.Len(t, raw, len(expected))
}

// TestPricingItemJSONOmitsAbsentOptionals verifies the open-ended period's
// nil end date and a free-band-less plan's absent savings reference rate are
// omitted on the wire, so the Swift decoder sees nil rather than a zero value.
func TestPricingItemJSONOmitsAbsentOptionals(t *testing.T) {
	item := bandPricingItem("pricing-1", "2026-01-01", nil)
	item.Windows = nil
	item.SavingsReferenceRate = nil

	encoded, err := json.Marshal(item)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(encoded, &raw))
	assert.NotContains(t, raw, "endDate")
	assert.NotContains(t, raw, "savingsReferenceRate")
}

// bandPricingItem is the shared fixture for a band-shape row: the migrated
// form of the plan that ran before the switch (free 11:00–14:00, one flat
// rate otherwise).
func bandPricingItem(id, startDate string, endDate *string) PricingItem {
	return PricingItem{
		PricingID:            id,
		StartDate:            startDate,
		EndDate:              endDate,
		DefaultRate:          0.2873,
		Windows:              []PricingWindow{{Start: "11:00", End: "14:00", Free: true}},
		FeedInRate:           0.0500,
		SavingsReferenceRate: floatPtr(0.1500),
		CreatedAt:            "2026-05-23T10:00:00Z",
		UpdatedAt:            "2026-05-23T10:00:00Z",
	}
}

func floatPtr(v float64) *float64 { return &v }
func strPtr(v string) *string     { return &v }

// inMemoryPricingAPI is a hand-rolled fake that satisfies every DynamoDB
// operation the pricing reader/writer needs: PutItem, GetItem, UpdateItem,
// DeleteItem, Scan, and TransactWriteItems. Backed by a single map keyed
// by pricingId so reads and writes stay coherent across all paths.
type inMemoryPricingAPI struct {
	items map[string]map[string]types.AttributeValue

	// failTransact toggles a forced TransactionCanceledException with the
	// configured Reasons slice. Used by the atomicity tests.
	failTransact bool
	transactErr  error
}

func newInMemoryPricingAPI() *inMemoryPricingAPI {
	return &inMemoryPricingAPI{items: make(map[string]map[string]types.AttributeValue)}
}

func (m *inMemoryPricingAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	id := params.Item["pricingId"].(*types.AttributeValueMemberS).Value
	m.items[id] = params.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (m *inMemoryPricingAPI) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	id := params.Key["pricingId"].(*types.AttributeValueMemberS).Value
	if av, ok := m.items[id]; ok {
		return &dynamodb.GetItemOutput{Item: av}, nil
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *inMemoryPricingAPI) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	id := params.Key["pricingId"].(*types.AttributeValueMemberS).Value
	row, ok := m.items[id]
	if !ok {
		// Apply the update by setting fields as if the row existed empty
		// (sufficient for sentinel lazy-create semantics in tests).
		row = map[string]types.AttributeValue{
			"pricingId": &types.AttributeValueMemberS{Value: id},
		}
	}
	applyUpdateExpression(row, params.UpdateExpression, params.ExpressionAttributeValues)
	m.items[id] = row
	return &dynamodb.UpdateItemOutput{}, nil
}

// applyUpdateExpression applies a "REMOVE attr, … SET attr = :ph, …"
// expression to a row. Every update expression the pricing store emits is of
// that shape.
//
// The attribute name has to come from the expression, not from the
// placeholder: the store writes `SET endDate = :end`, so mapping `:end` to an
// attribute called "end" would leave the closing row's end date unset and
// silently pass a succession test that should fail.
func applyUpdateExpression(row map[string]types.AttributeValue, expr *string, values map[string]types.AttributeValue) {
	if expr == nil {
		return
	}
	remove, set := "", *expr
	if before, after, found := strings.Cut(set, " SET "); found && strings.HasPrefix(set, "REMOVE ") {
		remove, set = strings.TrimPrefix(before, "REMOVE "), after
	} else {
		set = strings.TrimPrefix(set, "SET ")
	}

	for attr := range strings.SplitSeq(remove, ",") {
		if attr = strings.TrimSpace(attr); attr != "" {
			delete(row, attr)
		}
	}
	for assignment := range strings.SplitSeq(set, ",") {
		attr, placeholder, found := strings.Cut(assignment, "=")
		if !found {
			continue
		}
		if v, ok := values[strings.TrimSpace(placeholder)]; ok {
			row[strings.TrimSpace(attr)] = v
		}
	}
}

func (m *inMemoryPricingAPI) DeleteItem(_ context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	id := params.Key["pricingId"].(*types.AttributeValueMemberS).Value
	delete(m.items, id)
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *inMemoryPricingAPI) Scan(_ context.Context, params *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	out := make([]map[string]types.AttributeValue, 0, len(m.items))
	for _, av := range m.items {
		out = append(out, av)
	}
	// Stable order so tests can assert deterministically (the production
	// reader sorts by StartDate regardless).
	sort.Slice(out, func(i, j int) bool {
		return out[i]["pricingId"].(*types.AttributeValueMemberS).Value <
			out[j]["pricingId"].(*types.AttributeValueMemberS).Value
	})
	return &dynamodb.ScanOutput{Items: out}, nil
}

func (m *inMemoryPricingAPI) TransactWriteItems(_ context.Context, params *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	if m.failTransact {
		if m.transactErr != nil {
			return nil, m.transactErr
		}
		return nil, errors.New("forced transact failure")
	}
	// Apply each item in order. Simple semantics: ConditionExpression is
	// not actually evaluated — the atomicity tests inject failures via
	// failTransact + transactErr to exercise the real handler mapping.
	for _, item := range params.TransactItems {
		switch {
		case item.Put != nil:
			id := item.Put.Item["pricingId"].(*types.AttributeValueMemberS).Value
			m.items[id] = item.Put.Item
		case item.Update != nil:
			id := item.Update.Key["pricingId"].(*types.AttributeValueMemberS).Value
			row, ok := m.items[id]
			if !ok {
				row = map[string]types.AttributeValue{
					"pricingId": &types.AttributeValueMemberS{Value: id},
				}
			}
			applyUpdateExpression(row, item.Update.UpdateExpression, item.Update.ExpressionAttributeValues)
			m.items[id] = row
		case item.Delete != nil:
			id := item.Delete.Key["pricingId"].(*types.AttributeValueMemberS).Value
			delete(m.items, id)
		}
	}
	return &dynamodb.TransactWriteItemsOutput{}, nil
}

func pricingTestTable() string { return "test-pricing" }

func readPricing(t *testing.T, api *inMemoryPricingAPI, id string) *PricingItem {
	t.Helper()
	av, ok := api.items[id]
	if !ok {
		return nil
	}
	var item PricingItem
	require.NoError(t, attributevalue.UnmarshalMap(av, &item))
	return &item
}

func TestPricingStore_PutAndGetClosedPeriodRoundTrip(t *testing.T) {
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	want := bandPricingItem("pricing-1", "2026-01-01", strPtr("2027-01-01"))
	require.NoError(t, store.PutPricing(context.Background(), want, nil))

	got, err := store.GetPricing(context.Background(), want.PricingID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestPricingStore_DeleteClosedPeriod(t *testing.T) {
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	item := bandPricingItem("pricing-1", "2026-01-01", strPtr("2027-01-01"))
	require.NoError(t, store.PutPricing(context.Background(), item, nil))
	require.NoError(t, store.DeletePricing(context.Background(), item.PricingID, nil))

	got, err := store.GetPricing(context.Background(), item.PricingID)
	require.NoError(t, err)
	assert.Nil(t, got, "delete should clear the row")
}

func TestPricingStore_ListPricingOrdersByStartDateAscending(t *testing.T) {
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	rows := []PricingItem{
		// Insert deliberately out of order.
		{PricingID: "p-b", StartDate: "2026-01-01", EndDate: strPtr("2026-07-01"), DefaultRate: 0.3, FeedInRate: 0.05, CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z"},
		{PricingID: "p-a", StartDate: "2025-01-01", EndDate: strPtr("2026-01-01"), DefaultRate: 0.25, FeedInRate: 0.04, CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z"},
		{PricingID: "p-c", StartDate: "2026-07-01", DefaultRate: 0.32, FeedInRate: 0.05, CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z"},
	}
	for _, r := range rows {
		require.NoError(t, store.PutPricing(context.Background(), r, nil))
	}

	got, err := store.ListPricing(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 3, "every closed and open-ended period must appear in the list")
	assert.Equal(t, []string{"p-a", "p-b", "p-c"},
		[]string{got[0].PricingID, got[1].PricingID, got[2].PricingID},
		"AC 2.5: ListPricing must sort by startDate ascending")
}

func TestPricingStore_ListPricingExcludesSentinelRow(t *testing.T) {
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	// Seed both a real pricing row and the singleton sentinel directly.
	openID := "pricing-1"
	sentinel := PricingSentinel{
		PricingID:   pricingSentinelID,
		OpenEndedID: &openID,
		UpdatedAt:   "2026-05-23T10:00:00Z",
	}
	av, err := attributevalue.MarshalMap(sentinel)
	require.NoError(t, err)
	api.items[pricingSentinelID] = av

	item := bandPricingItem(openID, "2026-01-01", nil)
	require.NoError(t, store.PutPricing(context.Background(), item, nil))

	got, err := store.ListPricing(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1, "the sentinel row must not appear in ListPricing output")
	assert.Equal(t, openID, got[0].PricingID)
}

func TestPricingStore_GetSentinelReturnsNilWhenAbsent(t *testing.T) {
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	got, err := store.GetSentinel(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got, "GetSentinel must return nil when the sentinel row has not been provisioned yet")
}

func TestPricingStore_GetSentinelRoundTrip(t *testing.T) {
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	openID := "pricing-open"
	want := PricingSentinel{
		PricingID:   pricingSentinelID,
		OpenEndedID: &openID,
		UpdatedAt:   "2026-05-23T10:00:00Z",
	}
	av, err := attributevalue.MarshalMap(want)
	require.NoError(t, err)
	api.items[pricingSentinelID] = av

	got, err := store.GetSentinel(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestPricingStore_UpdateClosedRateOnly(t *testing.T) {
	api := newInMemoryPricingAPI()
	store := NewDynamoPricingStore(api, pricingTestTable())

	original := bandPricingItem("p-1", "2026-01-01", strPtr("2026-07-01"))
	require.NoError(t, store.PutPricing(context.Background(), original, nil))

	// Edit rates only — both before and after are closed periods, so the
	// open-ended-id snapshot stays nil and no transaction is needed.
	updated := original
	updated.DefaultRate = 0.3000
	updated.FeedInRate = 0.0500
	updated.UpdatedAt = "2026-05-24T10:00:00Z"
	require.NoError(t, store.UpdatePricing(context.Background(), updated, nil))

	got := readPricing(t, api, original.PricingID)
	require.NotNil(t, got)
	assert.Equal(t, 0.3000, got.DefaultRate)
	assert.Equal(t, "2026-05-24T10:00:00Z", got.UpdatedAt)
	assert.Equal(t, original.CreatedAt, got.CreatedAt, "createdAt must not change on update")
}

func TestPricingStore_PutPricingWrapsError(t *testing.T) {
	mock := &mockPricingAPI{
		putItemFn: func(_ context.Context, _ *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			return nil, errors.New("throttled")
		},
	}
	store := NewDynamoPricingStore(mock, pricingTestTable())

	end := "2026-12-31"
	err := store.PutPricing(context.Background(), PricingItem{PricingID: "p-1", StartDate: "2026-01-01", EndDate: &end}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "put pricing")
	assert.Contains(t, err.Error(), "test-pricing")
}

// mockPricingAPI is a function-based double covering every pricing-store
// operation. Fields are typed to match the production interface so error
// paths and per-call behaviour can be stubbed independently.
type mockPricingAPI struct {
	putItemFn       func(ctx context.Context, params *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error)
	getItemFn       func(ctx context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
	updateItemFn    func(ctx context.Context, params *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error)
	deleteItemFn    func(ctx context.Context, params *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error)
	scanFn          func(ctx context.Context, params *dynamodb.ScanInput) (*dynamodb.ScanOutput, error)
	transactWriteFn func(ctx context.Context, params *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error)
}

func (m *mockPricingAPI) PutItem(ctx context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if m.putItemFn != nil {
		return m.putItemFn(ctx, params)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockPricingAPI) GetItem(ctx context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if m.getItemFn != nil {
		return m.getItemFn(ctx, params)
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *mockPricingAPI) UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if m.updateItemFn != nil {
		return m.updateItemFn(ctx, params)
	}
	return &dynamodb.UpdateItemOutput{}, nil
}

func (m *mockPricingAPI) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if m.deleteItemFn != nil {
		return m.deleteItemFn(ctx, params)
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *mockPricingAPI) Scan(ctx context.Context, params *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	if m.scanFn != nil {
		return m.scanFn(ctx, params)
	}
	return &dynamodb.ScanOutput{}, nil
}

func (m *mockPricingAPI) TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	if m.transactWriteFn != nil {
		return m.transactWriteFn(ctx, params)
	}
	return &dynamodb.TransactWriteItemsOutput{}, nil
}
