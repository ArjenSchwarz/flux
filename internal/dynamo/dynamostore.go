package dynamo

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

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
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
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

// WriteDailyEnergy upserts the energy fields on a flux-daily-energy row via
// UpdateItem with a field-level SET expression (Decision 3). The summarisation
// pass writes derivedStats attributes via UpdateDailyEnergyDerived; the two
// writers never touch each other's attributes, so concurrent updates do not
// race.
//
// SET clause covers exactly the AlphaESS-sourced energy attributes; if a new
// energy field is added to DailyEnergyItem without updating this method, the
// regression test in TestWriteDailyEnergy_StructTagCoverage will fail at
// build time.
func (s *DynamoStore) WriteDailyEnergy(ctx context.Context, item DailyEnergyItem) error {
	tableName := s.tables.DailyEnergy
	updateExpr := "SET epv = :epv, eInput = :eInput, eOutput = :eOutput, " +
		"eCharge = :eCharge, eDischarge = :eDischarge, eGridCharge = :eGridCharge"
	values := map[string]types.AttributeValue{
		":epv":         &types.AttributeValueMemberN{Value: formatFloat(item.Epv)},
		":eInput":      &types.AttributeValueMemberN{Value: formatFloat(item.EInput)},
		":eOutput":     &types.AttributeValueMemberN{Value: formatFloat(item.EOutput)},
		":eCharge":     &types.AttributeValueMemberN{Value: formatFloat(item.ECharge)},
		":eDischarge":  &types.AttributeValueMemberN{Value: formatFloat(item.EDischarge)},
		":eGridCharge": &types.AttributeValueMemberN{Value: formatFloat(item.EGridCharge)},
	}
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &tableName,
		Key: map[string]types.AttributeValue{
			"sysSn": &types.AttributeValueMemberS{Value: item.SysSn},
			"date":  &types.AttributeValueMemberS{Value: item.Date},
		},
		UpdateExpression:          &updateExpr,
		ExpressionAttributeValues: values,
	})
	if err != nil {
		return fmt.Errorf("update daily energy (sysSn=%s, date=%s) (table=%s): %w", item.SysSn, item.Date, tableName, err)
	}
	return nil
}

// UpdateDailyEnergyDerived sets the four derivedStats attributes
// (dailyUsage, socLow, peakPeriods, derivedStatsComputedAt) on a
// flux-daily-energy row in a single UpdateItem SET expression. The energy
// attributes (epv, eInput, …) are left untouched; running this against a row
// that does not yet exist will create the row with only derivedStats and no
// energy totals — callers must precheck via GetDailyEnergy when that is
// undesirable (the daily-derived-stats summarisation pass does so per AC 1.4).
func (s *DynamoStore) UpdateDailyEnergyDerived(ctx context.Context, sysSn, date string, stats DerivedStats) error {
	tableName := s.tables.DailyEnergy

	dailyUsageAV, err := attributevalue.Marshal(stats.DailyUsage)
	if err != nil {
		return fmt.Errorf("marshal dailyUsage (sysSn=%s, date=%s): %w", sysSn, date, err)
	}
	socLowAV, err := attributevalue.Marshal(stats.SocLow)
	if err != nil {
		return fmt.Errorf("marshal socLow (sysSn=%s, date=%s): %w", sysSn, date, err)
	}
	peakPeriodsAV, err := attributevalue.Marshal(stats.PeakPeriods)
	if err != nil {
		return fmt.Errorf("marshal peakPeriods (sysSn=%s, date=%s): %w", sysSn, date, err)
	}

	updateExpr := "SET dailyUsage = :du, socLow = :sl, peakPeriods = :pp, derivedStatsComputedAt = :ts"
	values := map[string]types.AttributeValue{
		":du": dailyUsageAV,
		":sl": socLowAV,
		":pp": peakPeriodsAV,
		":ts": &types.AttributeValueMemberS{Value: stats.DerivedStatsComputedAt},
	}
	_, err = s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &tableName,
		Key: map[string]types.AttributeValue{
			"sysSn": &types.AttributeValueMemberS{Value: sysSn},
			"date":  &types.AttributeValueMemberS{Value: date},
		},
		UpdateExpression:          &updateExpr,
		ExpressionAttributeValues: values,
	})
	if err != nil {
		return fmt.Errorf("update daily energy derived (sysSn=%s, date=%s) (table=%s): %w", sysSn, date, tableName, err)
	}
	return nil
}

// GetDailyEnergy returns the row for (sysSn, date) or nil when not found.
// Mirrors the existing GetOffpeak pattern; used by the summarisation
// pass for the AC 1.10 precheck and (in future) by Lambda /day for past-date
// reads.
func (s *DynamoStore) GetDailyEnergy(ctx context.Context, sysSn, date string) (*DailyEnergyItem, error) {
	return getItem[DailyEnergyItem](ctx, s.client, s.tables.DailyEnergy,
		map[string]types.AttributeValue{
			"sysSn": &types.AttributeValueMemberS{Value: sysSn},
			"date":  &types.AttributeValueMemberS{Value: date},
		},
		fmt.Sprintf("daily energy (table=%s, sysSn=%s, date=%s)", s.tables.DailyEnergy, sysSn, date),
	)
}

// QueryReadings paginates the flux-readings table for the given serial and
// timestamp range. Used by the daily-derived-stats summarisation pass.
func (s *DynamoStore) QueryReadings(ctx context.Context, serial string, from, to int64) ([]ReadingItem, error) {
	return queryAll[ReadingItem](ctx, s.client, s.tables.Readings, "readings",
		"sysSn = :serial AND #ts BETWEEN :from AND :to",
		map[string]string{"#ts": "timestamp"},
		map[string]types.AttributeValue{
			":serial": &types.AttributeValueMemberS{Value: serial},
			":from":   &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", from)},
			":to":     &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", to)},
		},
	)
}

// formatFloat formats a float64 for a DynamoDB N attribute value.
// Uses 'g' (shortest unique) formatting to keep the wire compact while
// preserving precision; matches the encoding attributevalue.MarshalMap
// uses internally.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
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
