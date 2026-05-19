package dynamo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inMemoryDevicesAPI implements the union of WriteAPI + GetItem + UpdateItem
// used by DynamoDeviceWriter. The single shared map keeps writes and reads
// consistent without a real DynamoDB client.
type inMemoryDevicesAPI struct {
	items map[string]map[string]types.AttributeValue
}

func newInMemoryDevicesAPI() *inMemoryDevicesAPI {
	return &inMemoryDevicesAPI{items: make(map[string]map[string]types.AttributeValue)}
}

func (m *inMemoryDevicesAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	id := params.Item["deviceId"].(*types.AttributeValueMemberS).Value
	m.items[id] = params.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (m *inMemoryDevicesAPI) DeleteItem(_ context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	id := params.Key["deviceId"].(*types.AttributeValueMemberS).Value
	if params.ConditionExpression != nil {
		// Emulate `lastRegisteredAt = :scanned`: if the scanned value differs
		// from the current row's value, the delete must fail.
		existing, ok := m.items[id]
		if !ok {
			return nil, &types.ConditionalCheckFailedException{}
		}
		want := params.ExpressionAttributeValues[":scanned"].(*types.AttributeValueMemberS).Value
		got := existing["lastRegisteredAt"].(*types.AttributeValueMemberS).Value
		if want != got {
			return nil, &types.ConditionalCheckFailedException{}
		}
	}
	delete(m.items, id)
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *inMemoryDevicesAPI) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	id := params.Key["deviceId"].(*types.AttributeValueMemberS).Value
	existing, ok := m.items[id]
	if !ok {
		existing = map[string]types.AttributeValue{
			"deviceId": &types.AttributeValueMemberS{Value: id},
		}
	}
	// Parse the limited "SET attr1 = :v1, attr2 = :v2" grammar the
	// production code actually uses. Anything else is a test bug.
	expr := strings.TrimSpace(*params.UpdateExpression)
	expr = strings.TrimPrefix(expr, "SET ")
	for assign := range strings.SplitSeq(expr, ",") {
		parts := strings.SplitN(strings.TrimSpace(assign), "=", 2)
		attr := strings.TrimSpace(parts[0])
		valKey := strings.TrimSpace(parts[1])
		existing[attr] = params.ExpressionAttributeValues[valKey]
	}
	m.items[id] = existing
	return &dynamodb.UpdateItemOutput{Attributes: existing}, nil
}

func (m *inMemoryDevicesAPI) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	id := params.Key["deviceId"].(*types.AttributeValueMemberS).Value
	if av, ok := m.items[id]; ok {
		return &dynamodb.GetItemOutput{Item: av}, nil
	}
	return &dynamodb.GetItemOutput{}, nil
}

func devicesTestTable() string { return "test-devices" }

func readDevice(t *testing.T, api *inMemoryDevicesAPI, deviceID string) *DeviceItem {
	t.Helper()
	out, err := api.GetItem(context.Background(), &dynamodb.GetItemInput{
		Key: map[string]types.AttributeValue{
			"deviceId": &types.AttributeValueMemberS{Value: deviceID},
		},
	})
	require.NoError(t, err)
	if out.Item == nil {
		return nil
	}
	var item DeviceItem
	require.NoError(t, attributevalue.UnmarshalMap(out.Item, &item))
	return &item
}

func TestDynamoDeviceWriter_PutDeviceRoundTrip(t *testing.T) {
	api := newInMemoryDevicesAPI()
	writer := NewDynamoDeviceWriter(api, devicesTestTable())

	want := DeviceItem{
		DeviceID:           "device-uuid-abc",
		Platform:           "ios",
		APNsToken:          "deadbeef",
		APNsTokenUpdatedAt: "2026-05-19T10:00:00Z",
		TZIdentifier:       "Australia/Sydney",
		TZUpdatedAt:        1714838400,
		LastRegisteredAt:   "2026-05-19T10:00:00Z",
		TokenStatus:        "active",
		CreatedAt:          "2026-05-19T10:00:00Z",
	}
	require.NoError(t, writer.PutDevice(context.Background(), want))

	got := readDevice(t, api, want.DeviceID)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestDynamoDeviceWriter_DeleteDeviceConditional_MatchDeletes(t *testing.T) {
	api := newInMemoryDevicesAPI()
	writer := NewDynamoDeviceWriter(api, devicesTestTable())

	item := DeviceItem{
		DeviceID:         "device-uuid-abc",
		LastRegisteredAt: "2026-04-19T00:00:00Z",
	}
	require.NoError(t, writer.PutDevice(context.Background(), item))

	require.NoError(t, writer.DeleteDeviceConditional(
		context.Background(), item.DeviceID, item.LastRegisteredAt,
	))
	assert.Nil(t, readDevice(t, api, item.DeviceID), "matching condition deletes the row")
}

func TestDynamoDeviceWriter_DeleteDeviceConditional_MismatchKeepsRow(t *testing.T) {
	api := newInMemoryDevicesAPI()
	writer := NewDynamoDeviceWriter(api, devicesTestTable())

	item := DeviceItem{
		DeviceID:         "device-uuid-abc",
		LastRegisteredAt: "2026-05-19T01:00:00Z", // updated since the scan
	}
	require.NoError(t, writer.PutDevice(context.Background(), item))

	err := writer.DeleteDeviceConditional(
		context.Background(), item.DeviceID, "2026-04-19T00:00:00Z", // stale scanned value
	)
	require.Error(t, err, "stale scanned timestamp must fail the condition")
	var ccf *types.ConditionalCheckFailedException
	assert.True(t, errors.As(err, &ccf), "error must surface ConditionalCheckFailedException for the caller to detect re-registration")
	assert.NotNil(t, readDevice(t, api, item.DeviceID), "row must remain after a failed conditional delete")
}

func TestDynamoDeviceWriter_MarkStaleSetsTokenStatus(t *testing.T) {
	api := newInMemoryDevicesAPI()
	writer := NewDynamoDeviceWriter(api, devicesTestTable())

	require.NoError(t, writer.PutDevice(context.Background(), DeviceItem{
		DeviceID:    "device-uuid-abc",
		TokenStatus: "active",
	}))

	require.NoError(t, writer.MarkStale(context.Background(), "device-uuid-abc"))

	got := readDevice(t, api, "device-uuid-abc")
	require.NotNil(t, got)
	assert.Equal(t, "stale", got.TokenStatus)
}

func TestDynamoDeviceWriter_PutDeviceWrapsError(t *testing.T) {
	mock := &mockDevicesAPI{
		putItemFn: func(_ context.Context, _ *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
			return nil, errors.New("throttled")
		},
	}
	writer := NewDynamoDeviceWriter(mock, devicesTestTable())

	err := writer.PutDevice(context.Background(), DeviceItem{DeviceID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "put device")
	assert.Contains(t, err.Error(), "test-devices")
}

// mockDevicesAPI is a function-based double for error-path tests.
type mockDevicesAPI struct {
	putItemFn    func(ctx context.Context, params *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error)
	deleteItemFn func(ctx context.Context, params *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error)
	updateItemFn func(ctx context.Context, params *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error)
}

func (m *mockDevicesAPI) PutItem(ctx context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if m.putItemFn != nil {
		return m.putItemFn(ctx, params)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockDevicesAPI) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if m.deleteItemFn != nil {
		return m.deleteItemFn(ctx, params)
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *mockDevicesAPI) UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if m.updateItemFn != nil {
		return m.updateItemFn(ctx, params)
	}
	return &dynamodb.UpdateItemOutput{}, nil
}
