package dynamo

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DeviceItem represents a row in the flux-devices table.
type DeviceItem struct {
	DeviceID           string `dynamodbav:"deviceId"`
	Platform           string `dynamodbav:"platform"`            // "ios" | "macos"
	APNsToken          string `dynamodbav:"apnsToken,omitempty"` // lowercase hex; empty until granted
	APNsTokenUpdatedAt string `dynamodbav:"apnsTokenUpdatedAt,omitempty"`
	TZIdentifier       string `dynamodbav:"tzIdentifier"`     // IANA
	TZUpdatedAt        int64  `dynamodbav:"tzUpdatedAt"`      // unix seconds, monotonic per device
	LastRegisteredAt   string `dynamodbav:"lastRegisteredAt"` // RFC 3339 UTC
	TokenStatus        string `dynamodbav:"tokenStatus"`      // "active" | "stale"
	CreatedAt          string `dynamodbav:"createdAt"`
}

// DevicesAPI is the subset of the DynamoDB client used by DynamoDeviceWriter.
// PutItem upserts the device row; DeleteItem is used by the conditional orphan
// GC path; UpdateItem narrowly handles the stale-token mutation triggered by
// APNs feedback; GetItem is used by the Lambda to read existing values
// before overwriting (preserves token across token-less re-registrations).
type DevicesAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// DynamoDeviceWriter writes device rows to DynamoDB. The single live
// *dynamodb.Client satisfies DevicesAPI at compile time.
type DynamoDeviceWriter struct {
	client DevicesAPI
	table  string
}

// NewDynamoDeviceWriter returns a writer scoped to the given table name.
func NewDynamoDeviceWriter(client DevicesAPI, table string) *DynamoDeviceWriter {
	return &DynamoDeviceWriter{client: client, table: table}
}

// PutDeviceConditional upserts the row only when no existing row exists or
// the stored tzUpdatedAt is less than or equal to the incoming value (AC 4.5).
// Returns *types.ConditionalCheckFailedException when a newer TZ already
// won — the Lambda maps that to a 409 response.
//
// The condition is evaluated server-side, so two concurrent registrations
// with different TZUpdatedAt values cannot both clobber each other.
func (w *DynamoDeviceWriter) PutDeviceConditional(ctx context.Context, item DeviceItem, incomingTZUpdatedAt int64) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal device (deviceId=%s): %w", item.DeviceID, err)
	}
	cond := "attribute_not_exists(deviceId) OR tzUpdatedAt <= :incoming"
	_, err = w.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &w.table,
		Item:                av,
		ConditionExpression: &cond,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":incoming": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", incomingTZUpdatedAt)},
		},
	})
	if err != nil {
		return fmt.Errorf("put device conditional (table=%s, deviceId=%s): %w", w.table, item.DeviceID, err)
	}
	return nil
}

// PutDevice upserts a single device row. Last-write-wins; the Lambda guards
// the tzUpdatedAt-monotonic invariant before calling this.
func (w *DynamoDeviceWriter) PutDevice(ctx context.Context, item DeviceItem) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal device (deviceId=%s): %w", item.DeviceID, err)
	}
	_, err = w.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &w.table,
		Item:      av,
	})
	if err != nil {
		return fmt.Errorf("put device (table=%s, deviceId=%s): %w", w.table, item.DeviceID, err)
	}
	return nil
}

// DeleteDeviceConditional removes the device row only when the stored
// lastRegisteredAt equals the scanned value. Protects the midnight orphan GC
// against the race where a device re-registers between Scan and Delete:
// the returned error is a *types.ConditionalCheckFailedException so the
// caller can log flux_orphan_gc_skipped_reregistered.
func (w *DynamoDeviceWriter) DeleteDeviceConditional(ctx context.Context, deviceID, scannedLastRegisteredAt string) error {
	cond := "lastRegisteredAt = :scanned"
	_, err := w.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:           &w.table,
		Key:                 map[string]types.AttributeValue{"deviceId": &types.AttributeValueMemberS{Value: deviceID}},
		ConditionExpression: &cond,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":scanned": &types.AttributeValueMemberS{Value: scannedLastRegisteredAt},
		},
	})
	if err != nil {
		return fmt.Errorf("delete device (table=%s, deviceId=%s): %w", w.table, deviceID, err)
	}
	return nil
}

// ScanDevices reads every row from the devices table. Only used by the
// daily orphan GC and the rules-cache snapshot; both are low-frequency,
// so a Scan is acceptable at this feature's cardinality (≤20 devices).
func ScanDevices(ctx context.Context, client interface {
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
}, table string) ([]DeviceItem, error) {
	var items []DeviceItem
	input := &dynamodb.ScanInput{TableName: &table}
	for {
		out, err := client.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("scan devices (table=%s): %w", table, err)
		}
		page := make([]DeviceItem, len(out.Items))
		for i, av := range out.Items {
			if err := attributevalue.UnmarshalMap(av, &page[i]); err != nil {
				return nil, fmt.Errorf("unmarshal device (table=%s): %w", table, err)
			}
		}
		items = append(items, page...)
		if out.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = out.LastEvaluatedKey
	}
	return items, nil
}

// GetDevice returns the row for the given deviceId or nil when not present.
// Used by the Lambda's POST /devices handler to preserve fields the payload
// omits (most importantly the APNs token after a permission-grant flip).
func (w *DynamoDeviceWriter) GetDevice(ctx context.Context, deviceID string) (*DeviceItem, error) {
	return getItem[DeviceItem](ctx, w.client, w.table,
		map[string]types.AttributeValue{
			"deviceId": &types.AttributeValueMemberS{Value: deviceID},
		},
		fmt.Sprintf("device (table=%s, deviceId=%s)", w.table, deviceID),
	)
}

// MarkStale sets tokenStatus = "stale" for the given device. Called by the
// APNs worker when APNs reports 410 / BadDeviceToken.
func (w *DynamoDeviceWriter) MarkStale(ctx context.Context, deviceID string) error {
	expr := "SET tokenStatus = :stale"
	_, err := w.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &w.table,
		Key: map[string]types.AttributeValue{
			"deviceId": &types.AttributeValueMemberS{Value: deviceID},
		},
		UpdateExpression: &expr,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":stale": &types.AttributeValueMemberS{Value: "stale"},
		},
	})
	if err != nil {
		return fmt.Errorf("mark device stale (table=%s, deviceId=%s): %w", w.table, deviceID, err)
	}
	return nil
}
