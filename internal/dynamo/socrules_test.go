package dynamo

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSoCRuleItemJSONWireShape pins the on-the-wire JSON keys the Swift
// client decodes into SoCAlertRule. A Go-only round-trip can't catch a
// cross-language casing regression — both encode and decode use the same
// Go field, so the wire shape drifts silently. This test asserts the keys
// explicitly.
func TestSoCRuleItemJSONWireShape(t *testing.T) {
	item := SoCRuleItem{
		DeviceID:         "dev-1",
		RuleID:           "rule-uuid-1",
		ThresholdPercent: 40,
		WindowStart:      "17:00",
		WindowEnd:        "23:59",
		Enabled:          true,
		Label:            "Evening cooking",
		CreatedAt:        "2026-05-20T10:00:00Z",
		UpdatedAt:        "2026-05-20T10:00:00Z",
	}
	encoded, err := json.Marshal(item)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(encoded, &raw))

	// RuleID must serialise as "id" — the Swift SoCAlertRule reads it
	// through Identifiable.id, not a separate ruleId field.
	assert.Equal(t, "rule-uuid-1", raw["id"])
	assert.NotContains(t, raw, "ruleId")
	assert.NotContains(t, raw, "RuleID")

	// Everything else stays camelCase.
	expected := map[string]any{
		"deviceId":         "dev-1",
		"thresholdPercent": float64(40), // JSON numbers decode as float64
		"windowStart":      "17:00",
		"windowEnd":        "23:59",
		"enabled":          true,
		"label":            "Evening cooking",
		"createdAt":        "2026-05-20T10:00:00Z",
		"updatedAt":        "2026-05-20T10:00:00Z",
	}
	for key, want := range expected {
		assert.Equal(t, want, raw[key], "wire shape key %q", key)
	}

	// Pin the absence of accidental PascalCase leakage from Go's default
	// json.Marshal field-name behaviour.
	for _, leaked := range []string{
		"DeviceID", "ThresholdPercent", "WindowStart",
		"WindowEnd", "Enabled", "Label", "CreatedAt", "UpdatedAt",
	} {
		assert.NotContains(t, raw, leaked, "PascalCase field leaked")
	}
}

func TestSoCRuleItemJSONOmitsEmptyLabel(t *testing.T) {
	item := SoCRuleItem{
		DeviceID:         "dev-1",
		RuleID:           "rule-uuid-1",
		ThresholdPercent: 40,
		WindowStart:      "17:00",
		WindowEnd:        "23:59",
		Enabled:          true,
		CreatedAt:        "2026-05-20T10:00:00Z",
		UpdatedAt:        "2026-05-20T10:00:00Z",
	}
	encoded, err := json.Marshal(item)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(encoded, &raw))
	assert.NotContains(t, raw, "label", "empty label must be omitted, not serialised as \"\"")
}

// inMemorySocRulesAPI implements the subset of the DynamoDB client used by
// DynamoSocRuleWriter (PutItem, DeleteItem, Query). The map is keyed by
// deviceId + "|" + ruleId for simple lookup; Query iterates and filters.
type inMemorySocRulesAPI struct {
	items map[string]map[string]types.AttributeValue
}

func newInMemorySocRulesAPI() *inMemorySocRulesAPI {
	return &inMemorySocRulesAPI{items: make(map[string]map[string]types.AttributeValue)}
}

func ruleKey(deviceID, ruleID string) string { return deviceID + "|" + ruleID }

func (m *inMemorySocRulesAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	d := params.Item["deviceId"].(*types.AttributeValueMemberS).Value
	r := params.Item["ruleId"].(*types.AttributeValueMemberS).Value
	m.items[ruleKey(d, r)] = params.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (m *inMemorySocRulesAPI) DeleteItem(_ context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	d := params.Key["deviceId"].(*types.AttributeValueMemberS).Value
	r := params.Key["ruleId"].(*types.AttributeValueMemberS).Value
	delete(m.items, ruleKey(d, r))
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *inMemorySocRulesAPI) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	want := params.ExpressionAttributeValues[":deviceId"].(*types.AttributeValueMemberS).Value
	var out []map[string]types.AttributeValue
	for _, av := range m.items {
		if av["deviceId"].(*types.AttributeValueMemberS).Value == want {
			out = append(out, av)
		}
	}
	// Stable order by ruleId so tests can assert content deterministically.
	sort.Slice(out, func(i, j int) bool {
		return out[i]["ruleId"].(*types.AttributeValueMemberS).Value <
			out[j]["ruleId"].(*types.AttributeValueMemberS).Value
	})
	return &dynamodb.QueryOutput{Items: out}, nil
}

func socRulesTestTable() string { return "test-soc-rules" }

func readRule(t *testing.T, api *inMemorySocRulesAPI, deviceID, ruleID string) *SoCRuleItem {
	t.Helper()
	av, ok := api.items[ruleKey(deviceID, ruleID)]
	if !ok {
		return nil
	}
	var item SoCRuleItem
	require.NoError(t, attributevalue.UnmarshalMap(av, &item))
	return &item
}

func TestDynamoSocRuleWriter_PutRuleRoundTrip(t *testing.T) {
	api := newInMemorySocRulesAPI()
	writer := NewDynamoSocRuleWriter(api, socRulesTestTable())

	want := SoCRuleItem{
		DeviceID:         "device-1",
		RuleID:           "rule-uuid-1",
		ThresholdPercent: 40,
		WindowStart:      "17:00",
		WindowEnd:        "00:00",
		Enabled:          true,
		Label:            "Evening cooking",
		CreatedAt:        "2026-05-19T10:00:00Z",
		UpdatedAt:        "2026-05-19T10:00:00Z",
	}
	require.NoError(t, writer.PutRule(context.Background(), want))

	got := readRule(t, api, want.DeviceID, want.RuleID)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestDynamoSocRuleWriter_DeleteRuleClearsRow(t *testing.T) {
	api := newInMemorySocRulesAPI()
	writer := NewDynamoSocRuleWriter(api, socRulesTestTable())

	item := SoCRuleItem{DeviceID: "device-1", RuleID: "rule-uuid-1", ThresholdPercent: 30}
	require.NoError(t, writer.PutRule(context.Background(), item))
	require.NoError(t, writer.DeleteRule(context.Background(), item.DeviceID, item.RuleID))

	assert.Nil(t, readRule(t, api, item.DeviceID, item.RuleID))
}

func TestDynamoSocRuleReader_ListRulesByDevice(t *testing.T) {
	api := newInMemorySocRulesAPI()
	writer := NewDynamoSocRuleWriter(api, socRulesTestTable())
	reader := NewDynamoSocRuleReader(api, socRulesTestTable())

	rules := []SoCRuleItem{
		{DeviceID: "device-1", RuleID: "rule-a", ThresholdPercent: 30, CreatedAt: "2026-05-01T00:00:00Z"},
		{DeviceID: "device-1", RuleID: "rule-b", ThresholdPercent: 40, CreatedAt: "2026-05-02T00:00:00Z"},
		{DeviceID: "device-2", RuleID: "rule-c", ThresholdPercent: 50, CreatedAt: "2026-05-03T00:00:00Z"},
	}
	for _, r := range rules {
		require.NoError(t, writer.PutRule(context.Background(), r))
	}

	got, err := reader.ListRulesByDevice(context.Background(), "device-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	ids := []string{got[0].RuleID, got[1].RuleID}
	sort.Strings(ids)
	assert.Equal(t, []string{"rule-a", "rule-b"}, ids)
}

func TestDynamoSocRuleWriter_PutRuleWrapsError(t *testing.T) {
	mock := &mockSocRulesAPI{
		putItemFn: func(_ context.Context, _ *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			return nil, errors.New("throttled")
		},
	}
	writer := NewDynamoSocRuleWriter(mock, socRulesTestTable())

	err := writer.PutRule(context.Background(), SoCRuleItem{DeviceID: "d", RuleID: "r"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "put soc rule")
	assert.Contains(t, err.Error(), "test-soc-rules")
}

// mockSocRulesAPI is a function-based double for error-path tests.
type mockSocRulesAPI struct {
	putItemFn    func(ctx context.Context, params *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error)
	deleteItemFn func(ctx context.Context, params *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error)
	queryFn      func(ctx context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
}

func (m *mockSocRulesAPI) PutItem(ctx context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if m.putItemFn != nil {
		return m.putItemFn(ctx, params)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockSocRulesAPI) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if m.deleteItemFn != nil {
		return m.deleteItemFn(ctx, params)
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *mockSocRulesAPI) Query(ctx context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, params)
	}
	return &dynamodb.QueryOutput{}, nil
}
