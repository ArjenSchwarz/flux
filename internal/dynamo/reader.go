package dynamo

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Reader defines read operations for the API Lambda.
type Reader interface {
	QueryReadings(ctx context.Context, serial string, from, to int64) ([]ReadingItem, error)
	GetSystem(ctx context.Context, serial string) (*SystemItem, error)
	GetOffpeak(ctx context.Context, serial, date string) (*OffpeakItem, error)
	GetDailyEnergy(ctx context.Context, serial, date string) (*DailyEnergyItem, error)
	QueryDailyEnergy(ctx context.Context, serial, startDate, endDate string) ([]DailyEnergyItem, error)
	QueryDailyPower(ctx context.Context, serial, date string) ([]DailyPowerItem, error)
}

// ReadAPI is the subset of the DynamoDB client used by DynamoReader.
// Separate from DynamoAPI to avoid forcing poller mocks to implement Query.
type ReadAPI interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// DynamoReader reads poller data from DynamoDB for the API Lambda.
type DynamoReader struct {
	client ReadAPI
	tables TableNames
}

// NewDynamoReader creates a DynamoReader with the given client and table names.
func NewDynamoReader(client ReadAPI, tables TableNames) *DynamoReader {
	return &DynamoReader{client: client, tables: tables}
}

// getItem is a shared helper for GetItem calls that return nil for not-found items.
// Used by both DynamoReader and DynamoStore to avoid implementation divergence.
func getItem[T any](ctx context.Context, client interface {
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}, table string, key map[string]types.AttributeValue, desc string) (*T, error) {
	out, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &table,
		Key:       key,
	})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", desc, err)
	}
	if out.Item == nil {
		return nil, nil
	}
	var item T
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", desc, err)
	}
	return &item, nil
}

// offpeakKey returns the DynamoDB key for an offpeak item.
func offpeakKey(serial, date string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"sysSn": &types.AttributeValueMemberS{Value: serial},
		"date":  &types.AttributeValueMemberS{Value: date},
	}
}

func (r *DynamoReader) QueryReadings(ctx context.Context, serial string, from, to int64) ([]ReadingItem, error) {
	return queryAll[ReadingItem](ctx, r.client, r.tables.Readings, "readings",
		"sysSn = :serial AND #ts BETWEEN :from AND :to",
		map[string]string{"#ts": "timestamp"},
		map[string]types.AttributeValue{
			":serial": &types.AttributeValueMemberS{Value: serial},
			":from":   &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", from)},
			":to":     &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", to)},
		},
	)
}

func (r *DynamoReader) GetSystem(ctx context.Context, serial string) (*SystemItem, error) {
	return getItem[SystemItem](ctx, r.client, r.tables.System,
		map[string]types.AttributeValue{
			"sysSn": &types.AttributeValueMemberS{Value: serial},
		},
		fmt.Sprintf("system (table=%s, sysSn=%s)", r.tables.System, serial),
	)
}

func (r *DynamoReader) GetOffpeak(ctx context.Context, serial, date string) (*OffpeakItem, error) {
	return getItem[OffpeakItem](ctx, r.client, r.tables.Offpeak,
		offpeakKey(serial, date),
		fmt.Sprintf("offpeak (table=%s, sysSn=%s, date=%s)", r.tables.Offpeak, serial, date),
	)
}

func (r *DynamoReader) GetDailyEnergy(ctx context.Context, serial, date string) (*DailyEnergyItem, error) {
	return getItem[DailyEnergyItem](ctx, r.client, r.tables.DailyEnergy,
		map[string]types.AttributeValue{
			"sysSn": &types.AttributeValueMemberS{Value: serial},
			"date":  &types.AttributeValueMemberS{Value: date},
		},
		fmt.Sprintf("daily energy (table=%s, sysSn=%s, date=%s)", r.tables.DailyEnergy, serial, date),
	)
}

func (r *DynamoReader) QueryDailyEnergy(ctx context.Context, serial, startDate, endDate string) ([]DailyEnergyItem, error) {
	return queryAll[DailyEnergyItem](ctx, r.client, r.tables.DailyEnergy, "daily energy",
		"sysSn = :serial AND #d BETWEEN :start AND :end",
		map[string]string{"#d": "date"},
		map[string]types.AttributeValue{
			":serial": &types.AttributeValueMemberS{Value: serial},
			":start":  &types.AttributeValueMemberS{Value: startDate},
			":end":    &types.AttributeValueMemberS{Value: endDate},
		},
	)
}

func (r *DynamoReader) QueryDailyPower(ctx context.Context, serial, date string) ([]DailyPowerItem, error) {
	return queryAll[DailyPowerItem](ctx, r.client, r.tables.DailyPower, "daily power",
		"sysSn = :serial AND begins_with(uploadTime, :date)",
		nil,
		map[string]types.AttributeValue{
			":serial": &types.AttributeValueMemberS{Value: serial},
			":date":   &types.AttributeValueMemberS{Value: date},
		},
	)
}

// queryAll executes a paginated DynamoDB Query and collects all results.
// All queries use ScanIndexForward: true (ascending sort key order).
func queryAll[T any](
	ctx context.Context,
	client ReadAPI,
	table, desc string,
	keyCondition string,
	exprNames map[string]string,
	exprValues map[string]types.AttributeValue,
) ([]T, error) {
	forward := true
	input := &dynamodb.QueryInput{
		TableName:                 &table,
		KeyConditionExpression:    &keyCondition,
		ExpressionAttributeValues: exprValues,
		ScanIndexForward:          &forward,
	}
	if len(exprNames) > 0 {
		input.ExpressionAttributeNames = exprNames
	}

	var items []T
	for {
		out, err := client.Query(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("query %s (table=%s): %w", desc, table, err)
		}

		page := make([]T, len(out.Items))
		for i, av := range out.Items {
			if err := attributevalue.UnmarshalMap(av, &page[i]); err != nil {
				return nil, fmt.Errorf("unmarshal %s (table=%s): %w", desc, table, err)
			}
		}
		items = append(items, page...)

		if out.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = out.LastEvaluatedKey
	}

	if items == nil {
		items = []T{}
	}
	return items, nil
}
