package dynamo

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// batchWriteMax is the DynamoDB BatchWriteItem limit per request.
const batchWriteMax = 25

// DynamoAPI is the subset of the DynamoDB client used by DynamoStore.
// Defined as an interface to enable testing without a real DynamoDB connection.
type DynamoAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
}

// DynamoStore writes poller data to DynamoDB.
type DynamoStore struct {
	client DynamoAPI
	tables TableNames
}

// NewDynamoStore creates a DynamoStore with the given client and table names.
func NewDynamoStore(client DynamoAPI, tables TableNames) *DynamoStore {
	return &DynamoStore{client: client, tables: tables}
}

func (s *DynamoStore) WriteReading(ctx context.Context, item ReadingItem) error {
	return s.putItem(ctx, s.tables.Readings, item, fmt.Sprintf("reading (sysSn=%s)", item.SysSn))
}

func (s *DynamoStore) WriteDailyEnergy(ctx context.Context, item DailyEnergyItem) error {
	return s.putItem(ctx, s.tables.DailyEnergy, item, fmt.Sprintf("daily energy (sysSn=%s, date=%s)", item.SysSn, item.Date))
}

func (s *DynamoStore) WriteDailyPower(ctx context.Context, items []DailyPowerItem) error {
	if len(items) == 0 {
		return nil
	}

	for i := 0; i < len(items); i += batchWriteMax {
		end := i + batchWriteMax
		if end > len(items) {
			end = len(items)
		}

		requests := make([]types.WriteRequest, 0, end-i)
		for _, item := range items[i:end] {
			av, err := attributevalue.MarshalMap(item)
			if err != nil {
				return fmt.Errorf("marshal daily power (sysSn=%s, uploadTime=%s): %w", item.SysSn, item.UploadTime, err)
			}
			requests = append(requests, types.WriteRequest{
				PutRequest: &types.PutRequest{Item: av},
			})
		}

		out, err := s.client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				s.tables.DailyPower: requests,
			},
		})
		if err != nil {
			return fmt.Errorf("batch write daily power (table=%s, chunk %d-%d): %w", s.tables.DailyPower, i, end, err)
		}

		// One retry for unprocessed items.
		if len(out.UnprocessedItems) > 0 {
			slog.Warn("retrying unprocessed items",
				"table", s.tables.DailyPower,
				"count", len(out.UnprocessedItems[s.tables.DailyPower]),
			)
			out, err = s.client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
				RequestItems: out.UnprocessedItems,
			})
			if err != nil {
				return fmt.Errorf("retry batch write daily power (table=%s): %w", s.tables.DailyPower, err)
			}
			if len(out.UnprocessedItems) > 0 {
				count := len(out.UnprocessedItems[s.tables.DailyPower])
				return fmt.Errorf("batch write daily power (table=%s): %d items still unprocessed after retry", s.tables.DailyPower, count)
			}
		}
	}
	return nil
}

func (s *DynamoStore) WriteSystem(ctx context.Context, item SystemItem) error {
	return s.putItem(ctx, s.tables.System, item, fmt.Sprintf("system (sysSn=%s)", item.SysSn))
}

func (s *DynamoStore) WriteOffpeak(ctx context.Context, item OffpeakItem) error {
	return s.putItem(ctx, s.tables.Offpeak, item, fmt.Sprintf("offpeak (sysSn=%s, date=%s)", item.SysSn, item.Date))
}

func (s *DynamoStore) DeleteOffpeak(ctx context.Context, serial, date string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.tables.Offpeak,
		Key: map[string]types.AttributeValue{
			"sysSn": &types.AttributeValueMemberS{Value: serial},
			"date":  &types.AttributeValueMemberS{Value: date},
		},
	})
	if err != nil {
		return fmt.Errorf("delete offpeak (table=%s, sysSn=%s, date=%s): %w", s.tables.Offpeak, serial, date, err)
	}
	return nil
}

func (s *DynamoStore) GetOffpeak(ctx context.Context, serial, date string) (*OffpeakItem, error) {
	return getItem[OffpeakItem](ctx, s.client, s.tables.Offpeak,
		offpeakKey(serial, date),
		fmt.Sprintf("offpeak (table=%s, sysSn=%s, date=%s)", s.tables.Offpeak, serial, date),
	)
}

// putItem marshals the item and writes it to the given table. The key string
// is used for error context (e.g., "reading (sysSn=X)").
func (s *DynamoStore) putItem(ctx context.Context, table string, item any, key string) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", key, err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &table,
		Item:      av,
	})
	if err != nil {
		return fmt.Errorf("put %s (table=%s): %w", key, table, err)
	}
	return nil
}
