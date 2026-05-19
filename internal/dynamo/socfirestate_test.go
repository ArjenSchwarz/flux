package dynamo

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inMemorySocFireStateAPI implements the subset of the DynamoDB client used by
// DynamoSocFireStateWriter (PutItem with ConditionExpression, DeleteItem,
// Query). PutItem honours the `attribute_not_exists(deviceRule)` condition
// expression so PutIfAbsent can be exercised.
type inMemorySocFireStateAPI struct {
	items map[string]map[string]types.AttributeValue
}

func newInMemorySocFireStateAPI() *inMemorySocFireStateAPI {
	return &inMemorySocFireStateAPI{items: make(map[string]map[string]types.AttributeValue)}
}

func fireKey(deviceRule, windowStartDate string) string {
	return deviceRule + "|" + windowStartDate
}

func (m *inMemorySocFireStateAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	dr := params.Item["deviceRule"].(*types.AttributeValueMemberS).Value
	ws := params.Item["windowStartDate"].(*types.AttributeValueMemberS).Value
	if params.ConditionExpression != nil {
		// Only conditional Put we use is attribute_not_exists(deviceRule);
		// fail when the row already exists.
		if _, exists := m.items[fireKey(dr, ws)]; exists {
			return nil, &types.ConditionalCheckFailedException{}
		}
	}
	m.items[fireKey(dr, ws)] = params.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (m *inMemorySocFireStateAPI) DeleteItem(_ context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	dr := params.Key["deviceRule"].(*types.AttributeValueMemberS).Value
	ws := params.Key["windowStartDate"].(*types.AttributeValueMemberS).Value
	delete(m.items, fireKey(dr, ws))
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *inMemorySocFireStateAPI) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	want := params.ExpressionAttributeValues[":deviceRule"].(*types.AttributeValueMemberS).Value
	var out []map[string]types.AttributeValue
	for _, av := range m.items {
		if av["deviceRule"].(*types.AttributeValueMemberS).Value == want {
			out = append(out, av)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["windowStartDate"].(*types.AttributeValueMemberS).Value <
			out[j]["windowStartDate"].(*types.AttributeValueMemberS).Value
	})
	return &dynamodb.QueryOutput{Items: out}, nil
}

func fireStateTestTable() string { return "test-soc-fire-state" }

func TestSoCFireStateItem_DeviceRuleComposition(t *testing.T) {
	item := NewSoCFireStateItem("device-1", "rule-A", "2026-05-19", 38.4, "collapseid-xxx",
		mustParseTime(t, "2026-05-19T18:30:00Z"))
	assert.Equal(t, "device-1#rule-A", item.DeviceRule,
		"deviceRule PK must compose deviceId + '#' + ruleId")
	assert.Equal(t, "2026-05-19", item.WindowStartDate)
	assert.Equal(t, "device-1", item.DeviceID)
	assert.Equal(t, "rule-A", item.RuleID)
	assert.InDelta(t, 38.4, item.ObservedSoc, 1e-9)
	assert.Equal(t, "collapseid-xxx", item.APNsCollapseID)
}

func TestSoCFireStateItem_TTLSevenDaysAfterFire(t *testing.T) {
	firedAt := mustParseTime(t, "2026-05-19T18:30:00Z")
	item := NewSoCFireStateItem("d", "r", "2026-05-19", 35, "x", firedAt)
	want := firedAt.Add(7 * 24 * time.Hour).Unix()
	assert.Equal(t, want, item.ExpiresAt, "TTL must be firedAt + 7d in unix seconds")
}

func TestSoCFireStateItem_RoundTrip(t *testing.T) {
	api := newInMemorySocFireStateAPI()
	writer := NewDynamoSocFireStateWriter(api, fireStateTestTable())

	firedAt := mustParseTime(t, "2026-05-19T18:30:00Z")
	want := NewSoCFireStateItem("device-1", "rule-A", "2026-05-19", 38.4, "collapseid-xxx", firedAt)

	wrote, err := writer.PutIfAbsent(context.Background(), want)
	require.NoError(t, err)
	assert.True(t, wrote, "first write must report wrote=true")

	av := api.items[fireKey(want.DeviceRule, want.WindowStartDate)]
	var got SoCFireStateItem
	require.NoError(t, attributevalue.UnmarshalMap(av, &got))
	assert.Equal(t, want, got)
}

func TestDynamoSocFireStateWriter_PutIfAbsentSecondReturnsFalse(t *testing.T) {
	api := newInMemorySocFireStateAPI()
	writer := NewDynamoSocFireStateWriter(api, fireStateTestTable())

	item := NewSoCFireStateItem("d", "r", "2026-05-19", 35, "cx",
		mustParseTime(t, "2026-05-19T18:30:00Z"))

	wrote1, err := writer.PutIfAbsent(context.Background(), item)
	require.NoError(t, err)
	assert.True(t, wrote1, "first PutIfAbsent must report wrote=true")

	wrote2, err := writer.PutIfAbsent(context.Background(), item)
	require.NoError(t, err,
		"PutIfAbsent must not propagate ConditionalCheckFailedException as an error")
	assert.False(t, wrote2, "second PutIfAbsent for same key must report wrote=false")
}

func TestDynamoSocFireStateWriter_PutIfAbsentPropagatesOtherErrors(t *testing.T) {
	mock := &mockSocFireStateAPI{
		putItemFn: func(_ context.Context, _ *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			return nil, errors.New("throttled")
		},
	}
	writer := NewDynamoSocFireStateWriter(mock, fireStateTestTable())

	wrote, err := writer.PutIfAbsent(context.Background(), NewSoCFireStateItem("d", "r", "w", 0, "", time.Now()))
	require.Error(t, err)
	assert.False(t, wrote)
	assert.Contains(t, err.Error(), "put soc fire state")
}

func TestDynamoSocFireStateWriter_DeleteByDeviceRule(t *testing.T) {
	api := newInMemorySocFireStateAPI()
	writer := NewDynamoSocFireStateWriter(api, fireStateTestTable())

	dates := []string{"2026-05-18", "2026-05-19", "2026-05-20"}
	for _, d := range dates {
		_, err := writer.PutIfAbsent(context.Background(),
			NewSoCFireStateItem("device-1", "rule-A", d, 35, "x", mustParseTime(t, d+"T18:30:00Z")))
		require.NoError(t, err)
	}
	// Another device-rule should not be affected.
	_, err := writer.PutIfAbsent(context.Background(),
		NewSoCFireStateItem("device-1", "rule-B", "2026-05-19", 35, "x", mustParseTime(t, "2026-05-19T18:30:00Z")))
	require.NoError(t, err)

	deleted, err := writer.DeleteByDeviceRule(context.Background(), "device-1", "rule-A")
	require.NoError(t, err)
	assert.Equal(t, 3, deleted, "all rows for the targeted deviceRule must be deleted")

	// Sibling rule survives.
	siblingKey := fireKey("device-1#rule-B", "2026-05-19")
	_, ok := api.items[siblingKey]
	assert.True(t, ok, "rows for other rules must not be affected")
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return v
}

// mockSocFireStateAPI is a function-based double for error-path tests.
type mockSocFireStateAPI struct {
	putItemFn    func(ctx context.Context, params *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error)
	deleteItemFn func(ctx context.Context, params *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error)
	queryFn      func(ctx context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
}

func (m *mockSocFireStateAPI) PutItem(ctx context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if m.putItemFn != nil {
		return m.putItemFn(ctx, params)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockSocFireStateAPI) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if m.deleteItemFn != nil {
		return m.deleteItemFn(ctx, params)
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *mockSocFireStateAPI) Query(ctx context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, params)
	}
	return &dynamodb.QueryOutput{}, nil
}
