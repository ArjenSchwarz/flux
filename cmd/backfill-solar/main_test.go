package main

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

const (
	testSerial       = "TEST123"
	testEnergyTable  = "flux-daily-energy-test"
	testReadingTable = "flux-readings-test"
	testPricingTable = "flux-pricing-test"
)

// fakeDynamo is a lightweight in-memory stand-in for the DynamoDB client.
// It returns canned daily-energy and readings rows from Query, and records
// every UpdateItem call so tests can assert on the patched dailyUsage payload.
type fakeDynamo struct {
	dailyEnergyRows  map[string][]dynamo.DailyEnergyItem // keyed by date or "*" for all
	readingsByDate   map[string][]dynamo.ReadingItem     // keyed by date "YYYY-MM-DD" Sydney
	pricingRows      []dynamo.PricingItem                // nil ⇒ the default 11:00–14:00 open-ended plan
	location         *time.Location
	queries          []*dynamodb.QueryInput
	updates          []*dynamodb.UpdateItemInput
	queryErrForTable map[string]error
	updateErr        error
	scanErr          error
}

// Scan serves the pricing read. A fixture that sets no pricingRows gets the
// pre-feature plan — free 11:00–14:00, one flat rate — which is the window
// every date in these tests was originally computed under.
func (f *fakeDynamo) Scan(_ context.Context, _ *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	rows := f.pricingRows
	if rows == nil {
		rows = []dynamo.PricingItem{testPricingRow("legacy-equivalent", "2000-01-01", "", "11:00", "14:00")}
	}
	avs := make([]map[string]types.AttributeValue, 0, len(rows))
	for i := range rows {
		av, err := attributevalue.MarshalMap(rows[i])
		if err != nil {
			return nil, err
		}
		avs = append(avs, av)
	}
	return &dynamodb.ScanOutput{Items: avs}, nil
}

// testPricingRow builds a band-shape plan whose free window is the given
// range and whose remainder carries a single flat rate.
func testPricingRow(id, startDate, endDate, freeStart, freeEnd string) dynamo.PricingItem {
	savings := 0.35
	item := dynamo.PricingItem{
		PricingID: id, StartDate: startDate, DefaultRate: 0.35, FeedInRate: 0.05,
		Windows:              []dynamo.PricingWindow{{Start: freeStart, End: freeEnd, Free: true}},
		SavingsReferenceRate: &savings,
	}
	if endDate != "" {
		item.EndDate = &endDate
	}
	return item
}

func (f *fakeDynamo) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.queries = append(f.queries, params)
	if err, ok := f.queryErrForTable[*params.TableName]; ok && err != nil {
		return nil, err
	}
	switch *params.TableName {
	case testEnergyTable:
		return marshalQueryItems(f.dailyEnergyRows["*"])
	case testReadingTable:
		date := readingsQueryDate(params, f.location)
		return marshalQueryItems(f.readingsByDate[date])
	}
	return &dynamodb.QueryOutput{}, nil
}

func (f *fakeDynamo) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.updates = append(f.updates, params)
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return &dynamodb.UpdateItemOutput{}, nil
}

// readingsQueryDate inverts the from/to seconds in a readings Query back to
// the YYYY-MM-DD Sydney date the test fixture is keyed on. The CLI queries
// readings with [dayStart, dayEnd-1] in Sydney TZ, so the from value is the
// midnight Unix timestamp.
func readingsQueryDate(params *dynamodb.QueryInput, loc *time.Location) string {
	from := params.ExpressionAttributeValues[":from"].(*types.AttributeValueMemberN).Value
	ts, err := strconv.ParseInt(from, 10, 64)
	if err != nil {
		panic("readingsQueryDate: malformed :from value " + from + ": " + err.Error())
	}
	return time.Unix(ts, 0).In(loc).Format("2006-01-02")
}

func marshalQueryItems[T any](items []T) (*dynamodb.QueryOutput, error) {
	avs := make([]map[string]types.AttributeValue, 0, len(items))
	for i := range items {
		av, err := attributevalue.MarshalMap(items[i])
		if err != nil {
			return nil, err
		}
		avs = append(avs, av)
	}
	return &dynamodb.QueryOutput{Items: avs}, nil
}

// minuteReadingsWithSolar builds 60s-spaced readings for the given Sydney
// date with Ppv=1500W from 06:00 to 18:00 Sydney. This guarantees every
// daylight block contains many samples and produces a non-trivial solar
// integral. Pload is a constant 600W so totalKwh is also computed but
// irrelevant to the test assertions.
func minuteReadingsWithSolar(t *testing.T, date string, loc *time.Location) []dynamo.ReadingItem {
	t.Helper()
	dayStart, err := time.ParseInLocation("2006-01-02", date, loc)
	require.NoError(t, err)
	out := make([]dynamo.ReadingItem, 0, 24*60)
	for i := range 24 * 60 {
		ts := dayStart.Add(time.Duration(i) * time.Minute)
		ppv := 0.0
		if ts.Hour() >= 6 && ts.Hour() < 18 {
			ppv = 1500
		}
		out = append(out, dynamo.ReadingItem{
			SysSn:     testSerial,
			Timestamp: ts.Unix(),
			Ppv:       ppv,
			Pload:     600,
			Soc:       50,
		})
	}
	return out
}

// storedRowAllDaylightMissingSolar builds a daily-energy row whose dailyUsage
// already has the five-block layout but no solarKwh on any daylight block.
// totalKwh, percentOfDay, boundarySource, status, start, end carry deliberately
// distinctive values so tests can assert byte-for-byte preservation.
func storedRowAllDaylightMissingSolar(date string) dynamo.DailyEnergyItem {
	avgNight := 0.5
	return dynamo.DailyEnergyItem{
		SysSn: testSerial,
		Date:  date,
		Epv:   12.0,
		DailyUsage: &dynamo.DailyUsageAttr{
			Blocks: []dynamo.DailyUsageBlockAttr{
				{
					Kind:              derivedstats.DailyUsageKindNight,
					Start:             "2026-04-13T14:00:00Z",
					End:               "2026-04-13T20:00:00Z",
					TotalKwh:          2.01,
					AverageKwhPerHour: &avgNight,
					PercentOfDay:      11,
					Status:            derivedstats.DailyUsageStatusComplete,
					BoundarySource:    derivedstats.DailyUsageBoundaryReadings,
				},
				{
					Kind:           derivedstats.DailyUsageKindMorningPeak,
					Start:          "2026-04-13T20:00:00Z",
					End:            "2026-04-14T01:00:00Z",
					TotalKwh:       3.45,
					PercentOfDay:   22,
					Status:         derivedstats.DailyUsageStatusComplete,
					BoundarySource: derivedstats.DailyUsageBoundaryEstimated,
				},
				{
					Kind:           derivedstats.DailyUsageKindOffPeak,
					Start:          "2026-04-14T01:00:00Z",
					End:            "2026-04-14T04:00:00Z",
					TotalKwh:       1.55,
					PercentOfDay:   12,
					Status:         derivedstats.DailyUsageStatusComplete,
					BoundarySource: derivedstats.DailyUsageBoundaryReadings,
				},
				{
					Kind:           derivedstats.DailyUsageKindAfternoonPeak,
					Start:          "2026-04-14T04:00:00Z",
					End:            "2026-04-14T08:00:00Z",
					TotalKwh:       4.12,
					PercentOfDay:   28,
					Status:         derivedstats.DailyUsageStatusComplete,
					BoundarySource: derivedstats.DailyUsageBoundaryReadings,
				},
				{
					Kind:           derivedstats.DailyUsageKindEvening,
					Start:          "2026-04-14T08:00:00Z",
					End:            "2026-04-14T14:00:00Z",
					TotalKwh:       3.07,
					PercentOfDay:   27,
					Status:         derivedstats.DailyUsageStatusComplete,
					BoundarySource: derivedstats.DailyUsageBoundaryReadings,
				},
			},
		},
		DerivedStatsComputedAt: "2026-04-15T01:00:00Z",
	}
}

func sydney(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)
	return loc
}

func backfillOptsForTest(loc *time.Location) backfillOpts {
	// Pin clock so derivedstats.Blocks treats every test date as historical
	// (date != today, today-gate inert) and returns deterministic values.
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, loc)
	return backfillOpts{
		serial:           testSerial,
		tableDailyEnergy: testEnergyTable,
		tableReadings:    testReadingTable,
		tablePricing:     testPricingTable,
		from:             "2026-04-14",
		to:               "2026-04-14",
		location:         loc,
		now:              func() time.Time { return now },
	}
}

func TestBackfill_DryRun_NoUpdateItemCalled(t *testing.T) {
	loc := sydney(t)
	row := storedRowAllDaylightMissingSolar("2026-04-14")
	f := &fakeDynamo{
		location:        loc,
		dailyEnergyRows: map[string][]dynamo.DailyEnergyItem{"*": {row}},
		readingsByDate:  map[string][]dynamo.ReadingItem{"2026-04-14": minuteReadingsWithSolar(t, "2026-04-14", loc)},
	}
	opts := backfillOptsForTest(loc)
	opts.dryRun = true

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	assert.Empty(t, f.updates, "dry-run must not invoke UpdateItem")
	assert.Equal(t, 1, res.RowsScanned)
	assert.Equal(t, 1, res.RowsDryRun)
	assert.Zero(t, res.RowsWritten)
	assert.NotEmpty(t, res.IntentLog, "dry-run should emit at least one intended-write log entry")
}

func TestBackfill_LiveRun_PatchesSolarKwhAndWritesOnce(t *testing.T) {
	loc := sydney(t)
	row := storedRowAllDaylightMissingSolar("2026-04-14")
	f := &fakeDynamo{
		location:        loc,
		dailyEnergyRows: map[string][]dynamo.DailyEnergyItem{"*": {row}},
		readingsByDate:  map[string][]dynamo.ReadingItem{"2026-04-14": minuteReadingsWithSolar(t, "2026-04-14", loc)},
	}
	opts := backfillOptsForTest(loc)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Len(t, f.updates, 1, "live run must call UpdateItem exactly once for one needing-update row")
	assert.Equal(t, 1, res.RowsWritten)

	// The UpdateItem expression sets dailyUsage. Decode the patched payload
	// and confirm every daylight block carries a non-nil SolarKwh.
	patched := decodeDailyUsageFromUpdate(t, f.updates[0])
	require.NotNil(t, patched)
	byKind := map[string]dynamo.DailyUsageBlockAttr{}
	for _, b := range patched.Blocks {
		byKind[b.Kind] = b
	}
	for _, kind := range []string{
		derivedstats.DailyUsageKindMorningPeak,
		derivedstats.DailyUsageKindOffPeak,
		derivedstats.DailyUsageKindAfternoonPeak,
	} {
		got, ok := byKind[kind]
		require.True(t, ok)
		require.NotNil(t, got.SolarKwh, "%s SolarKwh must be set after backfill", kind)
		assert.Greater(t, *got.SolarKwh, 0.0, "%s SolarKwh must be positive given continuous Ppv readings", kind)
	}
	for _, kind := range []string{
		derivedstats.DailyUsageKindNight,
		derivedstats.DailyUsageKindEvening,
	} {
		got, ok := byKind[kind]
		require.True(t, ok)
		assert.Nil(t, got.SolarKwh, "%s must remain without SolarKwh", kind)
	}
}

func TestBackfill_Idempotent_SkipsRowsAlreadyPopulated(t *testing.T) {
	loc := sydney(t)
	row := storedRowAllDaylightMissingSolar("2026-04-14")
	one := 1.23
	for i := range row.DailyUsage.Blocks {
		switch row.DailyUsage.Blocks[i].Kind {
		case derivedstats.DailyUsageKindMorningPeak,
			derivedstats.DailyUsageKindOffPeak,
			derivedstats.DailyUsageKindAfternoonPeak:
			v := one
			row.DailyUsage.Blocks[i].SolarKwh = &v
		}
	}
	f := &fakeDynamo{
		location:        loc,
		dailyEnergyRows: map[string][]dynamo.DailyEnergyItem{"*": {row}},
		readingsByDate:  map[string][]dynamo.ReadingItem{"2026-04-14": minuteReadingsWithSolar(t, "2026-04-14", loc)},
	}
	opts := backfillOptsForTest(loc)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	assert.Empty(t, f.updates, "rows already populated must not be written again")
	assert.Equal(t, 1, res.RowsSkipped)
	assert.Zero(t, res.RowsWritten)
}

func TestBackfill_PreservesNonSolarFieldsByteForByte(t *testing.T) {
	// Even when recomputed Blocks() would yield slightly different totalKwh,
	// percentOfDay or boundary timestamps (e.g. because readings have been
	// partially TTL-pruned), the patch must preserve every stored field
	// except SolarKwh. Decision 7.
	loc := sydney(t)
	row := storedRowAllDaylightMissingSolar("2026-04-14")
	f := &fakeDynamo{
		location:        loc,
		dailyEnergyRows: map[string][]dynamo.DailyEnergyItem{"*": {row}},
		readingsByDate:  map[string][]dynamo.ReadingItem{"2026-04-14": minuteReadingsWithSolar(t, "2026-04-14", loc)},
	}
	opts := backfillOptsForTest(loc)

	_, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Len(t, f.updates, 1)
	patched := decodeDailyUsageFromUpdate(t, f.updates[0])
	require.NotNil(t, patched)
	require.Len(t, patched.Blocks, len(row.DailyUsage.Blocks))

	for i, want := range row.DailyUsage.Blocks {
		got := patched.Blocks[i]
		assert.Equal(t, want.Kind, got.Kind, "kind preserved")
		assert.Equal(t, want.Start, got.Start, "start preserved for kind=%s", want.Kind)
		assert.Equal(t, want.End, got.End, "end preserved for kind=%s", want.Kind)
		assert.Equal(t, want.TotalKwh, got.TotalKwh, "totalKwh preserved for kind=%s", want.Kind)
		assert.Equal(t, want.PercentOfDay, got.PercentOfDay, "percentOfDay preserved for kind=%s", want.Kind)
		assert.Equal(t, want.Status, got.Status, "status preserved for kind=%s", want.Kind)
		assert.Equal(t, want.BoundarySource, got.BoundarySource, "boundarySource preserved for kind=%s", want.Kind)
		if want.AverageKwhPerHour == nil {
			assert.Nil(t, got.AverageKwhPerHour, "averageKwhPerHour preserved nil for kind=%s", want.Kind)
		} else {
			require.NotNil(t, got.AverageKwhPerHour, "averageKwhPerHour preserved non-nil for kind=%s", want.Kind)
			assert.Equal(t, *want.AverageKwhPerHour, *got.AverageKwhPerHour, "averageKwhPerHour preserved for kind=%s", want.Kind)
		}
	}
}

func TestBackfill_SkipsRowWithoutDailyUsage(t *testing.T) {
	loc := sydney(t)
	row := dynamo.DailyEnergyItem{SysSn: testSerial, Date: "2026-04-14", Epv: 7.0}
	f := &fakeDynamo{
		location:        loc,
		dailyEnergyRows: map[string][]dynamo.DailyEnergyItem{"*": {row}},
	}
	opts := backfillOptsForTest(loc)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	assert.Empty(t, f.updates)
	assert.Equal(t, 1, res.RowsSkipped)
}

func TestBackfill_SkipsRowWithoutReadings(t *testing.T) {
	loc := sydney(t)
	row := storedRowAllDaylightMissingSolar("2026-04-14")
	f := &fakeDynamo{
		location:        loc,
		dailyEnergyRows: map[string][]dynamo.DailyEnergyItem{"*": {row}},
		readingsByDate:  map[string][]dynamo.ReadingItem{},
	}
	opts := backfillOptsForTest(loc)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	assert.Empty(t, f.updates, "missing readings must not produce an UpdateItem")
	assert.Equal(t, 1, res.RowsSkipped)
}

func TestValidateOpts_RejectsReversedDateRange(t *testing.T) {
	loc := sydney(t)
	opts := backfillOptsForTest(loc)
	opts.from = "2026-04-20"
	opts.to = "2026-04-10"

	err := validateOpts(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after")
}

func TestBackfill_DailyEnergyQueryError_PropagatedAsFatal(t *testing.T) {
	loc := sydney(t)
	f := &fakeDynamo{
		location:         loc,
		queryErrForTable: map[string]error{testEnergyTable: errors.New("throttled")},
	}
	opts := backfillOptsForTest(loc)

	_, err := runBackfill(context.Background(), f, opts)
	require.Error(t, err)
	assert.Empty(t, f.updates)
}

// TestBackfill_LiveRun_AppliesAttributeExistsCondition pins the
// ConditionExpression on writePatchedDailyUsage. Without it, an UpdateItem
// against a vanished or wrong-table key would silently create a fresh row
// containing only `dailyUsage` and no energy totals — a corrupted item that
// no other write path produces. The condition turns that into a clean error.
func TestBackfill_LiveRun_AppliesAttributeExistsCondition(t *testing.T) {
	loc := sydney(t)
	row := storedRowAllDaylightMissingSolar("2026-04-14")
	f := &fakeDynamo{
		location:        loc,
		dailyEnergyRows: map[string][]dynamo.DailyEnergyItem{"*": {row}},
		readingsByDate:  map[string][]dynamo.ReadingItem{"2026-04-14": minuteReadingsWithSolar(t, "2026-04-14", loc)},
	}
	opts := backfillOptsForTest(loc)

	_, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Len(t, f.updates, 1)
	cond := f.updates[0].ConditionExpression
	require.NotNil(t, cond, "writePatchedDailyUsage must guard with a ConditionExpression")
	assert.Contains(t, *cond, "attribute_exists(sysSn)")
}

func decodeDailyUsageFromUpdate(t *testing.T, up *dynamodb.UpdateItemInput) *dynamo.DailyUsageAttr {
	t.Helper()
	require.Contains(t, *up.UpdateExpression, "dailyUsage")
	av, ok := up.ExpressionAttributeValues[":du"]
	require.True(t, ok, "UpdateItem must bind :du")
	var got dynamo.DailyUsageAttr
	require.NoError(t, attributevalue.Unmarshal(av, &got))
	return &got
}
