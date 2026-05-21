package dynamo

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// SoCRuleItem represents a row in the flux-soc-rules table. RuleID is
// serialised as "id" so the Swift client decodes it into Identifiable.id.
type SoCRuleItem struct {
	DeviceID         string `dynamodbav:"deviceId" json:"deviceId"`
	RuleID           string `dynamodbav:"ruleId" json:"id"`                         // UUID, server-assigned
	ThresholdPercent int    `dynamodbav:"thresholdPercent" json:"thresholdPercent"` // 1..99
	WindowStart      string `dynamodbav:"windowStart" json:"windowStart"`           // HH:MM
	WindowEnd        string `dynamodbav:"windowEnd" json:"windowEnd"`               // HH:MM
	Enabled          bool   `dynamodbav:"enabled" json:"enabled"`
	Label            string `dynamodbav:"label,omitempty" json:"label,omitempty"` // <=40 chars
	CreatedAt        string `dynamodbav:"createdAt" json:"createdAt"`
	UpdatedAt        string `dynamodbav:"updatedAt" json:"updatedAt"` // bumped by every PUT
}

// SoCRulesWriteAPI is the subset of the DynamoDB client used by the rule
// writer. Distinct from SoCRulesReadAPI so the IAM split is enforced at
// compile time (the evaluator path is read-only).
type SoCRulesWriteAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
}

// SoCRulesReadAPI is the subset of the DynamoDB client used by the rule
// reader. The poller-side RulesCache and the Lambda's GET handler both go
// through Query.
type SoCRulesReadAPI interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// DynamoSocRuleWriter writes SoC alert rules to DynamoDB.
type DynamoSocRuleWriter struct {
	client SoCRulesWriteAPI
	table  string
}

// NewDynamoSocRuleWriter returns a writer scoped to the given table name.
func NewDynamoSocRuleWriter(client SoCRulesWriteAPI, table string) *DynamoSocRuleWriter {
	return &DynamoSocRuleWriter{client: client, table: table}
}

// PutRule upserts a rule row. The Lambda is the only caller and supplies
// server-assigned RuleID / CreatedAt / UpdatedAt.
func (w *DynamoSocRuleWriter) PutRule(ctx context.Context, item SoCRuleItem) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal soc rule (deviceId=%s, ruleId=%s): %w", item.DeviceID, item.RuleID, err)
	}
	_, err = w.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &w.table,
		Item:      av,
	})
	if err != nil {
		return fmt.Errorf("put soc rule (table=%s, deviceId=%s, ruleId=%s): %w", w.table, item.DeviceID, item.RuleID, err)
	}
	return nil
}

// DeleteRule removes a rule by composite key (deviceId, ruleId).
func (w *DynamoSocRuleWriter) DeleteRule(ctx context.Context, deviceID, ruleID string) error {
	_, err := w.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &w.table,
		Key: map[string]types.AttributeValue{
			"deviceId": &types.AttributeValueMemberS{Value: deviceID},
			"ruleId":   &types.AttributeValueMemberS{Value: ruleID},
		},
	})
	if err != nil {
		return fmt.Errorf("delete soc rule (table=%s, deviceId=%s, ruleId=%s): %w", w.table, deviceID, ruleID, err)
	}
	return nil
}

// DynamoSocRuleReader reads SoC alert rules from DynamoDB.
type DynamoSocRuleReader struct {
	client SoCRulesReadAPI
	table  string
}

// NewDynamoSocRuleReader returns a reader scoped to the given table name.
func NewDynamoSocRuleReader(client SoCRulesReadAPI, table string) *DynamoSocRuleReader {
	return &DynamoSocRuleReader{client: client, table: table}
}

// Table returns the underlying table name. Used by callers that need to
// drive Scan/Query operations the reader doesn't directly expose (e.g., the
// orphan GC, which needs to issue conditional deletes by raw key).
func (r *DynamoSocRuleReader) Table() string { return r.table }

// ListRulesByDevice returns every rule for the given device. Sorting by
// createdAt (AC 1.6) is the caller's responsibility — DynamoDB sorts by SK
// (ruleId UUID), not createdAt.
func (r *DynamoSocRuleReader) ListRulesByDevice(ctx context.Context, deviceID string) ([]SoCRuleItem, error) {
	return queryAll[SoCRuleItem](ctx, r.client, r.table, "soc rules",
		"deviceId = :deviceId",
		nil,
		map[string]types.AttributeValue{
			":deviceId": &types.AttributeValueMemberS{Value: deviceID},
		},
	)
}
