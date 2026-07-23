package dynamo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

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

// UpdateDailyEnergyDerived writes two independent attribute groups on a
// flux-daily-energy row via a single UpdateItem SET expression, each gated on
// its own sentinel being non-empty (peak-from-readings Decision 3):
//   - the derivedStats group (dailyUsage, socLow, peakPeriods,
//     derivedStatsComputedAt), gated on stats.DerivedStatsComputedAt;
//   - the peak group (peakGridImportKwh, peakComputedAt), gated on
//     stats.PeakComputedAt.
//
// Either group may be written without the other, so a row that already has
// derivedStats can have peak filled later (and vice versa) without clobbering.
// The energy attributes (epv, eInput, …) are left untouched; running this
// against a row that does not yet exist will create the row with only these
// derived attributes and no energy totals — callers must precheck via
// GetDailyEnergy when that is undesirable (the daily-derived-stats
// summarisation pass does so per AC 1.4, and cmd/backfill-grid does so for the
// peak group to avoid phantom rows).
//
// Invariant: this is the only write path for the dailyUsage attribute outside
// the cmd/backfill-solar CLI. The peak group is additionally written by
// cmd/backfill-grid (gated on peakComputedAt). New writers of the derivedStats
// group must not be added without revisiting the backfill's idempotency
// assumptions (specs/solar-by-block/design.md).
func (s *DynamoStore) UpdateDailyEnergyDerived(ctx context.Context, sysSn, date string, stats DerivedStats) error {
	tableName := s.tables.DailyEnergy

	// The derivedStats, peak, and band groups have independent lifecycles
	// (peak-from-readings Decision 3). Each is written only when its own sentinel is non-empty,
	// so the summarisation pass can fill peak on a row that already has derived
	// stats — and vice versa — without clobbering the other groups with zero
	// values. At least one group is always present in a real call; an empty
	// stats produces a no-op write guarded below.
	sets := make([]string, 0, 8)
	values := map[string]types.AttributeValue{}

	if stats.DerivedStatsComputedAt != "" {
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
		sets = append(sets, "dailyUsage = :du", "socLow = :sl", "peakPeriods = :pp", "derivedStatsComputedAt = :ts")
		values[":du"] = dailyUsageAV
		values[":sl"] = socLowAV
		values[":pp"] = peakPeriodsAV
		values[":ts"] = &types.AttributeValueMemberS{Value: stats.DerivedStatsComputedAt}
	}

	if stats.PeakComputedAt != "" {
		peakAV, err := attributevalue.Marshal(stats.PeakGridImportKwh)
		if err != nil {
			return fmt.Errorf("marshal peakGridImportKwh (sysSn=%s, date=%s): %w", sysSn, date, err)
		}
		sets = append(sets, "peakGridImportKwh = :pk", "peakComputedAt = :pkts")
		values[":pk"] = peakAV
		values[":pkts"] = &types.AttributeValueMemberS{Value: stats.PeakComputedAt}
	}

	if stats.BandsComputedAt != "" {
		bandsAV, err := attributevalue.Marshal(stats.BandImports)
		if err != nil {
			return fmt.Errorf("marshal bandImports (sysSn=%s, date=%s): %w", sysSn, date, err)
		}
		sets = append(sets, "bandImports = :bi", "bandsComputedAt = :bits")
		values[":bi"] = bandsAV
		values[":bits"] = &types.AttributeValueMemberS{Value: stats.BandsComputedAt}
	}

	if len(sets) == 0 {
		// Nothing to write — neither sentinel set. Treat as a no-op rather
		// than issuing an empty UpdateExpression (which DynamoDB rejects).
		return nil
	}

	updateExpr := "SET " + strings.Join(sets, ", ")
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
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
// Reads are eventually-consistent — the poller's off-peak window-end pass
// uses QueryReadingsConsistent instead to avoid missing the at-or-after-
// boundary reading it has just confirmed.
func (s *DynamoStore) QueryReadings(ctx context.Context, serial string, from, to int64) ([]ReadingItem, error) {
	return queryReadings(ctx, s.client, s.tables.Readings, serial, from, to, false)
}

// QueryReadingsConsistent is the strongly-consistent variant used by the
// off-peak window-end finalisation (AC 3.5). The poller has just observed
// that a reading at or after offpeak-end exists; a strongly-consistent read
// guarantees the integration query includes that reading.
func (s *DynamoStore) QueryReadingsConsistent(ctx context.Context, serial string, from, to int64) ([]ReadingItem, error) {
	return queryReadings(ctx, s.client, s.tables.Readings, serial, from, to, true)
}

// queryReadings is the shared body used by QueryReadings and
// QueryReadingsConsistent. The ScanIndexForward and pagination behaviour
// stay identical to queryAll's defaults; only ConsistentRead toggles.
func queryReadings(ctx context.Context, client interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}, table, serial string, from, to int64, consistent bool,
) ([]ReadingItem, error) {
	forward := true
	keyCond := "sysSn = :serial AND #ts BETWEEN :from AND :to"
	input := &dynamodb.QueryInput{
		TableName:                &table,
		KeyConditionExpression:   &keyCond,
		ExpressionAttributeNames: map[string]string{"#ts": "timestamp"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":serial": &types.AttributeValueMemberS{Value: serial},
			":from":   &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", from)},
			":to":     &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", to)},
		},
		ScanIndexForward: &forward,
	}
	if consistent {
		c := true
		input.ConsistentRead = &c
	}

	var items []ReadingItem
	for {
		out, err := client.Query(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("query readings (table=%s): %w", table, err)
		}
		page := make([]ReadingItem, len(out.Items))
		for i, av := range out.Items {
			if err := attributevalue.UnmarshalMap(av, &page[i]); err != nil {
				return nil, fmt.Errorf("unmarshal readings (table=%s): %w", table, err)
			}
		}
		items = append(items, page...)
		if out.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = out.LastEvaluatedKey
	}
	if items == nil {
		items = []ReadingItem{}
	}
	return items, nil
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
		end := min(i+batchWriteMax, len(items))

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

// WriteOffpeakIfPendingOrAbsent writes the row only when no row exists yet
// OR the existing row has status="pending". A row with status="complete"
// causes the put to fail with ErrOffpeakConditionFailed — the poller's
// callers log+skip per design.md "Concurrent writer guard". The pending-row
// write from handleStart continues to use unconditional WriteOffpeak.
func (s *DynamoStore) WriteOffpeakIfPendingOrAbsent(ctx context.Context, item OffpeakItem) error {
	return s.writeOffpeakConditional(ctx, item,
		"attribute_not_exists(#status) OR #status = :pending",
		map[string]types.AttributeValue{
			":pending": &types.AttributeValueMemberS{Value: OffpeakStatusPending},
		},
	)
}

// WriteOffpeakIfComplete writes the row only when the existing row has
// status="complete". A missing row OR status="pending" causes the put to
// fail with ErrOffpeakConditionFailed — protects the backfill CLI against
// overwriting a row mid-poll.
func (s *DynamoStore) WriteOffpeakIfComplete(ctx context.Context, item OffpeakItem) error {
	return s.writeOffpeakConditional(ctx, item,
		"#status = :complete",
		map[string]types.AttributeValue{
			":complete": &types.AttributeValueMemberS{Value: OffpeakStatusComplete},
		},
	)
}

// writeOffpeakConditional marshals + PutItem with the given condition,
// mapping ConditionalCheckFailedException to ErrOffpeakConditionFailed. The
// #status alias decouples the expression text from any future rename of the
// `status` attribute and avoids the DynamoDB reserved-word check.
func (s *DynamoStore) writeOffpeakConditional(
	ctx context.Context,
	item OffpeakItem,
	condition string,
	values map[string]types.AttributeValue,
) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal offpeak (sysSn=%s, date=%s): %w", item.SysSn, item.Date, err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                 &s.tables.Offpeak,
		Item:                      av,
		ConditionExpression:       &condition,
		ExpressionAttributeNames:  map[string]string{"#status": "status"},
		ExpressionAttributeValues: values,
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("offpeak (sysSn=%s, date=%s): %w", item.SysSn, item.Date, ErrOffpeakConditionFailed)
		}
		return fmt.Errorf("put conditional offpeak (table=%s, sysSn=%s, date=%s): %w", s.tables.Offpeak, item.SysSn, item.Date, err)
	}
	return nil
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
