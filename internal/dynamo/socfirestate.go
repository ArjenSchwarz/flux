package dynamo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// fireStateTTL is the per-row lifetime applied via DynamoDB TTL. Seven days
// is well past the daily reset cadence; combined with the (deviceRule,
// windowStartDate) key it guards against unbounded growth without ever
// expiring a row that is still relevant to today's dedup.
const fireStateTTL = 7 * 24 * time.Hour

// SoCFireStateItem represents a row in the flux-soc-fire-state table.
// The PK deviceRule composes deviceId + "#" + ruleId so a single Query
// scoped to a (device, rule) pair returns every fire-state for that rule.
type SoCFireStateItem struct {
	DeviceRule      string  `dynamodbav:"deviceRule"`      // PK: deviceId + "#" + ruleId
	WindowStartDate string  `dynamodbav:"windowStartDate"` // SK: YYYY-MM-DD in device TZ
	DeviceID        string  `dynamodbav:"deviceId"`        // duplicated for debug Queries
	RuleID          string  `dynamodbav:"ruleId"`          // duplicated for debug Queries
	FiredAt         string  `dynamodbav:"firedAt"`         // RFC 3339 UTC
	ObservedSoc     float64 `dynamodbav:"observedSoc"`
	APNsCollapseID  string  `dynamodbav:"apnsCollapseId"`
	ExpiresAt       int64   `dynamodbav:"expiresAt"` // TTL, firedAt + 7 days
}

// NewSoCFireStateItem builds a fire-state item with the canonical PK
// composition, RFC 3339 firedAt, and TTL = firedAt + 7d.
func NewSoCFireStateItem(deviceID, ruleID, windowStartDate string, observedSoc float64, collapseID string, firedAt time.Time) SoCFireStateItem {
	return SoCFireStateItem{
		DeviceRule:      deviceID + "#" + ruleID,
		WindowStartDate: windowStartDate,
		DeviceID:        deviceID,
		RuleID:          ruleID,
		FiredAt:         firedAt.UTC().Format(time.RFC3339),
		ObservedSoc:     observedSoc,
		APNsCollapseID:  collapseID,
		ExpiresAt:       firedAt.Add(fireStateTTL).Unix(),
	}
}

// SoCFireStateAPI is the subset of the DynamoDB client used by the fire-state
// store. PutItem with ConditionExpression handles PutIfAbsent; Query +
// DeleteItem handles the Lambda's cleanup-on-rule-mutation path.
type SoCFireStateAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// DynamoSocFireStateWriter writes fire-state rows to DynamoDB.
type DynamoSocFireStateWriter struct {
	client SoCFireStateAPI
	table  string
}

// NewDynamoSocFireStateWriter returns a writer scoped to the given table name.
func NewDynamoSocFireStateWriter(client SoCFireStateAPI, table string) *DynamoSocFireStateWriter {
	return &DynamoSocFireStateWriter{client: client, table: table}
}

// Table returns the underlying table name. Used by the orphan-GC adapter
// which needs to issue raw queries against the same table.
func (w *DynamoSocFireStateWriter) Table() string { return w.table }

// QueryFireStateByDeviceRule lists every fire-state row for the given
// (device, rule). Exposed as a package-level helper so the orphan GC can
// reuse it without depending on a writer instance.
func QueryFireStateByDeviceRule(ctx context.Context, client interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}, table, deviceID, ruleID string) ([]SoCFireStateItem, error) {
	return queryAll[SoCFireStateItem](ctx, client, table, "soc fire state",
		"deviceRule = :deviceRule",
		nil,
		map[string]types.AttributeValue{
			":deviceRule": &types.AttributeValueMemberS{Value: deviceID + "#" + ruleID},
		},
	)
}

// DeleteFireStateRow deletes a single fire-state row by composite key.
// Exposed as a package-level helper for the orphan GC's cascade.
func DeleteFireStateRow(ctx context.Context, client interface {
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
}, table, deviceRule, windowStartDate string) error {
	_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &table,
		Key: map[string]types.AttributeValue{
			"deviceRule":      &types.AttributeValueMemberS{Value: deviceRule},
			"windowStartDate": &types.AttributeValueMemberS{Value: windowStartDate},
		},
	})
	if err != nil {
		return fmt.Errorf("delete soc fire state row (table=%s, deviceRule=%s, windowStartDate=%s): %w", table, deviceRule, windowStartDate, err)
	}
	return nil
}

// PutIfAbsent writes the fire-state row only when no row exists for the same
// (deviceRule, windowStartDate). Returns (true, nil) on a newly-written row,
// (false, nil) when a row already exists (the rule already fired today), and
// (false, err) for any other failure.
func (w *DynamoSocFireStateWriter) PutIfAbsent(ctx context.Context, item SoCFireStateItem) (bool, error) {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return false, fmt.Errorf("marshal soc fire state (deviceRule=%s, windowStartDate=%s): %w", item.DeviceRule, item.WindowStartDate, err)
	}
	cond := "attribute_not_exists(deviceRule)"
	_, err = w.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &w.table,
		Item:                av,
		ConditionExpression: &cond,
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return false, nil
		}
		return false, fmt.Errorf("put soc fire state (table=%s, deviceRule=%s, windowStartDate=%s): %w", w.table, item.DeviceRule, item.WindowStartDate, err)
	}
	return true, nil
}

// DeleteByDeviceRule removes every row matching the given (deviceId, ruleId).
// Used by the Lambda after a rule edit / delete (AC 5.3 / 5.4). Returns the
// number of rows deleted so the caller can emit observability.
func (w *DynamoSocFireStateWriter) DeleteByDeviceRule(ctx context.Context, deviceID, ruleID string) (int, error) {
	deviceRule := deviceID + "#" + ruleID
	rows, err := queryAll[SoCFireStateItem](ctx, w.client, w.table, "soc fire state",
		"deviceRule = :deviceRule",
		nil,
		map[string]types.AttributeValue{
			":deviceRule": &types.AttributeValueMemberS{Value: deviceRule},
		},
	)
	if err != nil {
		return 0, fmt.Errorf("query soc fire state for cleanup (deviceRule=%s): %w", deviceRule, err)
	}
	deleted := 0
	for _, row := range rows {
		_, err := w.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: &w.table,
			Key: map[string]types.AttributeValue{
				"deviceRule":      &types.AttributeValueMemberS{Value: row.DeviceRule},
				"windowStartDate": &types.AttributeValueMemberS{Value: row.WindowStartDate},
			},
		})
		if err != nil {
			return deleted, fmt.Errorf("delete soc fire state row (deviceRule=%s, windowStartDate=%s): %w", row.DeviceRule, row.WindowStartDate, err)
		}
		deleted++
	}
	return deleted, nil
}
