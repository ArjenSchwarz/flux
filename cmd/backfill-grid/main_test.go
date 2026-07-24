package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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
	testSerial           = "TEST123"
	testOffpeakTable     = "flux-offpeak-test"
	testReadingsTable    = "flux-readings-test"
	testDailyEnergyTable = "flux-daily-energy-test"
	testPricingTable     = "flux-pricing-test"
)

// fakeDynamo is a lightweight in-memory stand-in for the DynamoDB client.
// It returns canned offpeak and readings rows from Query, and records every
// PutItem call so tests can assert on the persisted payload and condition
// expression.
type fakeDynamo struct {
	offpeakRows       map[string][]dynamo.OffpeakItem   // keyed by "*"
	readingsByDate    map[string][]dynamo.ReadingItem   // keyed by Sydney YYYY-MM-DD
	dailyEnergyByDate map[string]dynamo.DailyEnergyItem // keyed by Sydney YYYY-MM-DD; absent = no row
	pricingRows       []dynamo.PricingItem              // nil ⇒ the default 11:00–14:00 open-ended plan
	location          *time.Location
	queries           []*dynamodb.QueryInput
	puts              []*dynamodb.PutItemInput
	updates           []*dynamodb.UpdateItemInput // records UpdateDailyEnergyDerived calls
	queryErrForTable  map[string]error
	putErr            error
	scanErr           error
}

// Scan serves the pricing read. A fixture that sets no pricingRows gets the
// pre-feature plan — free 11:00–14:00, one flat rate — which is the shape
// every date in these tests was originally priced under.
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
	case testOffpeakTable:
		return marshalQueryItems(f.offpeakRows["*"])
	case testReadingsTable:
		date := readingsQueryDate(params, f.location)
		return marshalQueryItems(f.readingsByDate[date])
	}
	return &dynamodb.QueryOutput{}, nil
}

func (f *fakeDynamo) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.puts = append(f.puts, params)
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &dynamodb.PutItemOutput{}, nil
}

// The CLI now constructs a *dynamo.DynamoStore (Fix 6 of the pre-push review)
// so the conditional-write expression lives in one place. The store's
// interface (dynamo.DynamoAPI) requires Delete/Get/Update/BatchWrite — the CLI
// itself never calls them, so the fake returns benign zero values.

func (f *fakeDynamo) DeleteItem(_ context.Context, _ *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	return &dynamodb.DeleteItemOutput{}, nil
}

// GetItem serves the daily-energy GET the peak path issues before writing.
// The date is read from the key; a missing fixture entry returns an empty
// Item (DynamoDB's "not found"), which GetDailyEnergy maps to nil.
func (f *fakeDynamo) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	date := ""
	if d, ok := params.Key["date"].(*types.AttributeValueMemberS); ok {
		date = d.Value
	}
	row, ok := f.dailyEnergyByDate[date]
	if !ok {
		return &dynamodb.GetItemOutput{}, nil
	}
	av, err := attributevalue.MarshalMap(row)
	if err != nil {
		return nil, err
	}
	return &dynamodb.GetItemOutput{Item: av}, nil
}

func (f *fakeDynamo) UpdateItem(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.updates = append(f.updates, params)
	return &dynamodb.UpdateItemOutput{}, nil
}

func (f *fakeDynamo) BatchWriteItem(_ context.Context, _ *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	return &dynamodb.BatchWriteItemOutput{}, nil
}

// readingsQueryDate inverts the :from timestamp on a readings Query back to
// the Sydney YYYY-MM-DD date the fixture is keyed on. The CLI queries
// readings with [windowStart, windowEnd-1] in Sydney TZ, so the from value
// lands inside the off-peak window of that date.
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

// chargeReadings builds dense (10s) readings across the 11:00-14:00 Sydney
// window with a constant 4 kW grid import — exercises a heavy-charge day so
// the integration produces a non-trivial gridUsageKwh.
func chargeReadings(t *testing.T, date string, loc *time.Location) []dynamo.ReadingItem {
	t.Helper()
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	require.NoError(t, err)
	start := day.Add(11 * time.Hour)
	end := day.Add(14 * time.Hour)
	out := make([]dynamo.ReadingItem, 0, 3*60*60/10+1)
	for ts := start.Unix(); ts <= end.Unix(); ts += 10 {
		out = append(out, dynamo.ReadingItem{
			SysSn:     testSerial,
			Timestamp: ts,
			Pgrid:     4000,
			Pbat:      -3800,
			Ppv:       0,
			Soc:       30,
		})
	}
	return out
}

// fullDayReadings builds dense (10s) readings across the entire Sydney-local
// day [00:00, 24:00) with a constant 1 kW grid import. Both bracketing peak
// sub-windows ([00:00, 11:00) and [14:00, 24:00)) are densely covered so the
// peak usability gate passes. Peak energy = 1 kW over 11h + 10h = 21 kWh.
func fullDayReadings(t *testing.T, date string, loc *time.Location) []dynamo.ReadingItem {
	t.Helper()
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	require.NoError(t, err)
	end := day.AddDate(0, 0, 1)
	out := make([]dynamo.ReadingItem, 0, 24*60*60/10)
	for ts := day.Unix(); ts < end.Unix(); ts += 10 {
		out = append(out, dynamo.ReadingItem{
			SysSn:     testSerial,
			Timestamp: ts,
			Pgrid:     1000,
			Pbat:      0,
			Ppv:       0,
			Soc:       50,
		})
	}
	return out
}

// existingDailyEnergyRow returns a populated flux-daily-energy row for date,
// with no peak yet (PeakGridImportKwh nil). The peak backfill writes peak onto
// this row.
func existingDailyEnergyRow(date string) dynamo.DailyEnergyItem {
	return dynamo.DailyEnergyItem{
		SysSn:                  testSerial,
		Date:                   date,
		EInput:                 20.0,
		DerivedStatsComputedAt: "2026-05-19T03:00:00Z",
	}
}

// existingPendingRow returns a row in pending state — must NOT be overwritten
// by the CLI per AC 7.8.
func existingPendingRow(date string) dynamo.OffpeakItem {
	return dynamo.OffpeakItem{
		SysSn:        testSerial,
		Date:         date,
		Status:       dynamo.OffpeakStatusPending,
		StartEInput:  10.0,
		StartEpv:     0,
		StartECharge: 5.0,
	}
}

// existingCompleteRow returns a row in complete state with the lagged
// snapshot-diff values from the bug. The CLI overwrites this with
// readings-integrated values.
func existingCompleteRow(date string) dynamo.OffpeakItem {
	return dynamo.OffpeakItem{
		SysSn:               testSerial,
		Date:                date,
		Status:              dynamo.OffpeakStatusComplete,
		StartEInput:         10.0,
		EndEInput:           28.95,
		StartEpv:            0,
		EndEpv:              0,
		StartECharge:        5.0,
		EndECharge:          18.0,
		StartEDischarge:     2.0,
		EndEDischarge:       2.5,
		StartEOutput:        1.0,
		EndEOutput:          1.5,
		GridUsageKwh:        18.95, // lagged snapshot-diff
		SolarKwh:            0,
		BatteryChargeKwh:    13.0,
		BatteryDischargeKwh: 0.5,
		GridExportKwh:       0.5,
		BatteryDeltaPercent: 60,
	}
}

func sydney(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)
	return loc
}

func backfillOptsForTest(loc *time.Location, dates ...string) backfillOpts {
	from := dates[0]
	to := dates[0]
	if len(dates) > 1 {
		to = dates[len(dates)-1]
	}
	// Pin clock so the today-skip gate (AC 7.2) is stable. "Today" is
	// 2026-05-20 here so the historical dates 2026-05-18/19 are past.
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, loc)
	return backfillOpts{
		serial:           testSerial,
		tableOffpeak:     testOffpeakTable,
		tableReadings:    testReadingsTable,
		tableDailyEnergy: testDailyEnergyTable,
		tablePricing:     testPricingTable,
		from:             from,
		to:               to,
		location:         loc,
		now:              func() time.Time { return now },
	}
}

// captureSlog swaps slog.Default for a JSON-buffered logger so tests can
// assert log lines without coupling to the binary's output handler.
func captureSlog() (*bytes.Buffer, func()) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	old := slog.Default()
	slog.SetDefault(logger)
	return &buf, func() { slog.SetDefault(old) }
}

func TestBackfill_DryRun_NoPutItemCalled(t *testing.T) {
	loc := sydney(t)
	date := "2026-05-18"
	row := existingCompleteRow(date)
	f := &fakeDynamo{
		location:       loc,
		offpeakRows:    map[string][]dynamo.OffpeakItem{"*": {row}},
		readingsByDate: map[string][]dynamo.ReadingItem{date: chargeReadings(t, date, loc)},
	}
	opts := backfillOptsForTest(loc, date)
	opts.dryRun = true

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	assert.Empty(t, f.puts, "dry-run must not invoke PutItem")
	assert.Equal(t, 1, res.RowsScanned)
	assert.Equal(t, 1, res.RowsDryRun)
	assert.Zero(t, res.RowsWritten)
	assert.NotEmpty(t, res.Summary, "dry-run should emit at least one summary line")
}

func TestBackfill_LiveRun_WritesRecomputedDeltas(t *testing.T) {
	loc := sydney(t)
	date := "2026-05-18"
	row := existingCompleteRow(date)
	readings := chargeReadings(t, date, loc)
	f := &fakeDynamo{
		location:       loc,
		offpeakRows:    map[string][]dynamo.OffpeakItem{"*": {row}},
		readingsByDate: map[string][]dynamo.ReadingItem{date: readings},
	}
	opts := backfillOptsForTest(loc, date)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Len(t, f.puts, 1, "live run must call PutItem exactly once")
	assert.Equal(t, 1, res.RowsWritten)

	put := f.puts[0]
	require.NotNil(t, put.ConditionExpression)
	assert.Contains(t, *put.ConditionExpression, "#status = :complete",
		"PutItem must use WriteOffpeakIfComplete's condition (AC 7.8)")

	persisted := decodeOffpeakItem(t, put.Item)
	assert.Equal(t, dynamo.OffpeakStatusComplete, persisted.Status)
	// 4000W × 10800s = 43_200_000Ws = 12.0 kWh exactly.
	assert.InDelta(t, 12.0, persisted.GridUsageKwh, 0.01)
	assert.InDelta(t, 11.4, persisted.BatteryChargeKwh, 0.01)
	// Half-open [start, end): the reading at exactly endUnix is not counted as
	// an interior sample. chargeReadings emits 1081 timestamps from start to
	// end inclusive; SampleCount reflects the 1080 inside the window.
	assert.Equal(t, 1080, persisted.IntegrationSampleCount)
	assert.Equal(t, 0, persisted.IntegrationSkippedPairs)
	assert.NotEmpty(t, persisted.IntegratedAt)
	// Diagnostic snapshot preserved verbatim from the existing row (Decision 2).
	assert.Equal(t, row.StartEInput, persisted.StartEInput)
	assert.Equal(t, row.EndEInput, persisted.EndEInput)
}

func TestBackfill_Idempotent_DeltaFieldsBitEqualAcrossRuns(t *testing.T) {
	// AC 7.3 + AC 7.7: re-running the CLI against the same readings produces
	// identical values for the five delta fields and the two count fields.
	// integratedAt MAY differ — Decision 10 — so we pin two distinct clocks
	// for the two runs and assert the deltas match exactly while integratedAt
	// reflects each run's time.
	loc := sydney(t)
	date := "2026-05-18"
	row := existingCompleteRow(date)
	readings := chargeReadings(t, date, loc)

	clock1 := time.Date(2026, 5, 20, 12, 0, 0, 0, loc)
	clock2 := clock1.Add(time.Hour)

	run := func(now time.Time) dynamo.OffpeakItem {
		f := &fakeDynamo{
			location:       loc,
			offpeakRows:    map[string][]dynamo.OffpeakItem{"*": {row}},
			readingsByDate: map[string][]dynamo.ReadingItem{date: readings},
		}
		opts := backfillOptsForTest(loc, date)
		opts.now = func() time.Time { return now }
		_, err := runBackfill(context.Background(), f, opts)
		require.NoError(t, err)
		require.Len(t, f.puts, 1)
		return decodeOffpeakItem(t, f.puts[0].Item)
	}

	first := run(clock1)
	second := run(clock2)

	assert.Equal(t, first.GridUsageKwh, second.GridUsageKwh)
	assert.Equal(t, first.SolarKwh, second.SolarKwh)
	assert.Equal(t, first.BatteryChargeKwh, second.BatteryChargeKwh)
	assert.Equal(t, first.BatteryDischargeKwh, second.BatteryDischargeKwh)
	assert.Equal(t, first.GridExportKwh, second.GridExportKwh)
	assert.Equal(t, first.IntegrationSampleCount, second.IntegrationSampleCount)
	assert.Equal(t, first.IntegrationSkippedPairs, second.IntegrationSkippedPairs)
	// integratedAt is excluded from the idempotence guarantee per Decision 10.
	assert.NotEqual(t, first.IntegratedAt, second.IntegratedAt,
		"integratedAt is the time of integration; the two runs were pinned to different clocks")
}

func TestBackfill_RoundingConsistency_PersistedFieldsRoundedTwoDecimals(t *testing.T) {
	// AC 7.7: the persisted delta values are rounded to two decimal places so
	// the poller and the backfill CLI produce byte-equal values for the same
	// readings. A reading series with sub-millikWh precision would otherwise
	// drift across runs once the values are compared via marshalled rows.
	loc := sydney(t)
	date := "2026-05-18"
	row := existingCompleteRow(date)
	readings := chargeReadings(t, date, loc)
	f := &fakeDynamo{
		location:       loc,
		offpeakRows:    map[string][]dynamo.OffpeakItem{"*": {row}},
		readingsByDate: map[string][]dynamo.ReadingItem{date: readings},
	}
	opts := backfillOptsForTest(loc, date)
	_, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Len(t, f.puts, 1)
	persisted := decodeOffpeakItem(t, f.puts[0].Item)
	for _, v := range []float64{
		persisted.GridUsageKwh,
		persisted.SolarKwh,
		persisted.BatteryChargeKwh,
		persisted.BatteryDischargeKwh,
		persisted.GridExportKwh,
	} {
		assert.Equal(t, derivedstats.RoundEnergy(v), v,
			"persisted delta %v must already be rounded to 2 decimal places", v)
	}
}

func TestBackfill_DefaultTo_SkipsToday(t *testing.T) {
	// AC 7.2: --to defaults to yesterday; today's row must never be processed
	// even when the query result includes a row dated today (e.g. the poller
	// pre-staged a pending row at offpeak-start).
	loc := sydney(t)
	today := time.Date(2026, 5, 20, 12, 0, 0, 0, loc).Format("2006-01-02")
	yesterday := time.Date(2026, 5, 19, 0, 0, 0, 0, loc).Format("2006-01-02")
	yesterdayRow := existingCompleteRow(yesterday)
	todayRow := existingPendingRow(today)
	f := &fakeDynamo{
		location: loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {
			yesterdayRow, todayRow,
		}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			yesterday: chargeReadings(t, yesterday, loc),
			today:     chargeReadings(t, today, loc),
		},
	}
	opts := backfillOptsForTest(loc, yesterday)
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Len(t, f.puts, 1, "only yesterday's row must be written")
	persisted := decodeOffpeakItem(t, f.puts[0].Item)
	assert.Equal(t, yesterday, persisted.Date)
	assert.Equal(t, 1, res.RowsWritten)
	assert.Equal(t, 1, res.RowsSkipped, "today's row must be skipped")
}

func TestBackfill_TodayExplicitlyRequested_StillSkipped(t *testing.T) {
	// AC 7.2 also covers the case where the operator passes --to=today
	// explicitly. The CLI's runtime gate skips today regardless of the flag.
	loc := sydney(t)
	today := time.Date(2026, 5, 20, 12, 0, 0, 0, loc).Format("2006-01-02")
	todayRow := existingPendingRow(today)
	f := &fakeDynamo{
		location:       loc,
		offpeakRows:    map[string][]dynamo.OffpeakItem{"*": {todayRow}},
		readingsByDate: map[string][]dynamo.ReadingItem{today: chargeReadings(t, today, loc)},
	}
	opts := backfillOptsForTest(loc, today)
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	assert.Empty(t, f.puts, "today's row must not be written even when --to=today")
	assert.Equal(t, 1, res.RowsSkipped)
}

func TestBackfill_SparseReadings_SkippedNoWrite(t *testing.T) {
	// AC 7.4: a day with fewer than two usable readings in the window is
	// reported SKIPPED and the row is left unchanged.
	loc := sydney(t)
	date := "2026-05-18"
	row := existingCompleteRow(date)
	day, _ := time.ParseInLocation("2006-01-02", date, loc)
	sparseTs := day.Add(12 * time.Hour).Unix()
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {row}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: {{SysSn: testSerial, Timestamp: sparseTs, Pgrid: 1000}},
		},
	}
	opts := backfillOptsForTest(loc, date)
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	assert.Empty(t, f.puts, "sparse-readings day must not be written")
	assert.Equal(t, 1, res.RowsSparseSkipped)
	require.Len(t, res.Summary, 1)
	assert.Contains(t, res.Summary[0], "SKIPPED",
		"sparse-readings days must emit a SKIPPED summary line")
}

func TestBackfill_ConditionalWriteFailure_RowReportedExitsZero(t *testing.T) {
	// A row that has transitioned to pending between Query and PutItem (or a
	// row absent at the moment of the put) trips the WriteOffpeakIfComplete
	// condition. The CLI logs and continues — it does NOT exit non-zero.
	loc := sydney(t)
	date := "2026-05-18"
	row := existingCompleteRow(date)
	f := &fakeDynamo{
		location:       loc,
		offpeakRows:    map[string][]dynamo.OffpeakItem{"*": {row}},
		readingsByDate: map[string][]dynamo.ReadingItem{date: chargeReadings(t, date, loc)},
		putErr:         &types.ConditionalCheckFailedException{},
	}
	opts := backfillOptsForTest(loc, date)
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err, "conditional-write failure must not propagate as a fatal error")
	assert.Equal(t, 1, res.RowsConditionFailed)
	assert.Zero(t, res.RowsWritten)
}

func TestBackfill_DriftLogEmittedPerRow(t *testing.T) {
	// AC 6.1: the writer emits a structured INFO log entry per row, with the
	// five drift values. The backfill CLI shares the LogOffpeakDrift function
	// with the poller (Decision 6).
	buf, restore := captureSlog()
	defer restore()

	loc := sydney(t)
	date := "2026-05-18"
	row := existingCompleteRow(date)
	f := &fakeDynamo{
		location:       loc,
		offpeakRows:    map[string][]dynamo.OffpeakItem{"*": {row}},
		readingsByDate: map[string][]dynamo.ReadingItem{date: chargeReadings(t, date, loc)},
	}
	opts := backfillOptsForTest(loc, date)
	_, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	out := buf.String()
	for _, key := range []string{
		"driftGrid", "driftSolar", "driftCharge", "driftDischarge", "driftExport",
		"snapshotGrid", "integratedGrid",
	} {
		assert.True(t, bytes.Contains([]byte(out), []byte(key)),
			"drift log must include %q. got: %s", key, out)
	}
	assert.Contains(t, out, "offpeak drift")
}

func TestBackfill_QueryError_PropagatedAsFatal(t *testing.T) {
	loc := sydney(t)
	f := &fakeDynamo{
		location:         loc,
		queryErrForTable: map[string]error{testOffpeakTable: errors.New("throttled")},
	}
	opts := backfillOptsForTest(loc, "2026-05-18")
	_, err := runBackfill(context.Background(), f, opts)
	require.Error(t, err)
	assert.Empty(t, f.puts)
}

// TestBackfill_SummaryLine_ShowsAbsDifferencePerDelta covers AC 7.5: the
// per-day summary line includes a |Δ|=X.XX column for each of the five
// deltas, holding the absolute difference between the prior stored value
// and the newly-integrated value rounded to two decimal places.
func TestBackfill_SummaryLine_ShowsAbsDifferencePerDelta(t *testing.T) {
	loc := sydney(t)
	date := "2026-05-18"
	row := existingCompleteRow(date)
	readings := chargeReadings(t, date, loc)
	f := &fakeDynamo{
		location:       loc,
		offpeakRows:    map[string][]dynamo.OffpeakItem{"*": {row}},
		readingsByDate: map[string][]dynamo.ReadingItem{date: readings},
	}
	opts := backfillOptsForTest(loc, date)
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Len(t, res.Summary, 1, "expect one summary line for one row")
	line := res.Summary[0]

	// Decode the persisted row to read the new values exactly as the writer
	// rounded them, then compute the expected |Δ| against the stored prior.
	require.Len(t, f.puts, 1)
	persisted := decodeOffpeakItem(t, f.puts[0].Item)

	for _, c := range []struct {
		label string
		prev  float64
		next  float64
	}{
		{"grid", row.GridUsageKwh, persisted.GridUsageKwh},
		{"solar", row.SolarKwh, persisted.SolarKwh},
		{"chg", row.BatteryChargeKwh, persisted.BatteryChargeKwh},
		{"dis", row.BatteryDischargeKwh, persisted.BatteryDischargeKwh},
		{"exp", row.GridExportKwh, persisted.GridExportKwh},
	} {
		want := math.Abs(c.next - c.prev)
		// Format matches summaryLine: "label prev→next |Δ|=X.XX".
		fragment := fmt.Sprintf("%s %.2f→%.2f |Δ|=%.2f", c.label, c.prev, c.next, want)
		assert.Contains(t, line, fragment,
			"summary line must contain %q. got: %s", fragment, line)
	}
}

func TestBackfill_Peak_PresentDailyEnergyRow_WritesPeak(t *testing.T) {
	// Decision 7: a date whose flux-daily-energy row exists gets
	// peakGridImportKwh written via UpdateDailyEnergyDerived (peak group only).
	loc := sydney(t)
	date := "2026-05-18"
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: fullDayReadings(t, date, loc),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{
			date: existingDailyEnergyRow(date),
		},
	}
	opts := backfillOptsForTest(loc, date)
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	assert.Equal(t, 1, res.PeakWritten)
	assert.Zero(t, res.PeakSkippedAbsent)
	assert.Zero(t, res.PeakSkippedSparse)
	require.Len(t, f.updates, 1, "peak must be written via exactly one UpdateItem")

	up := f.updates[0]
	require.NotNil(t, up.UpdateExpression)
	// Peak group only: the derived-stats group must NOT be touched.
	assert.Contains(t, *up.UpdateExpression, "peakGridImportKwh")
	assert.Contains(t, *up.UpdateExpression, "peakComputedAt")
	assert.NotContains(t, *up.UpdateExpression, "derivedStatsComputedAt")
	assert.NotContains(t, *up.UpdateExpression, "dailyUsage")

	// 1 kW over the 21h of peak window (24h minus the 3h off-peak) = 21.0 kWh.
	peakAV, ok := up.ExpressionAttributeValues[":pk"]
	require.True(t, ok, "peak update must set :pk")
	var peak float64
	require.NoError(t, attributevalue.Unmarshal(peakAV, &peak))
	assert.InDelta(t, 21.0, peak, 0.01)
}

func TestBackfill_Peak_AbsentDailyEnergyRow_SkippedNoWrite(t *testing.T) {
	// Decision 7: an absent flux-daily-energy row must be skipped for peak with
	// no write — no phantom-row creation.
	loc := sydney(t)
	date := "2026-05-18"
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: fullDayReadings(t, date, loc),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{}, // no row for date
	}
	opts := backfillOptsForTest(loc, date)
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	assert.Equal(t, 1, res.PeakSkippedAbsent)
	assert.Zero(t, res.PeakWritten)
	assert.Empty(t, f.updates, "absent daily-energy row must not produce an UpdateItem (no phantom row)")
}

func TestBackfill_Peak_SparseBracketingWindow_SkippedNoWrite(t *testing.T) {
	// When a bracketing sub-window fails the usability gate the date is skipped
	// for peak (keeps the iOS fallback). chargeReadings covers only 11:00-14:00,
	// so both peak sub-windows are empty.
	loc := sydney(t)
	date := "2026-05-18"
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: chargeReadings(t, date, loc),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{
			date: existingDailyEnergyRow(date),
		},
	}
	opts := backfillOptsForTest(loc, date)
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	assert.Equal(t, 1, res.PeakSkippedSparse)
	assert.Zero(t, res.PeakWritten)
	assert.Empty(t, f.updates, "sparse bracketing window must not write peak")
}

func TestBackfill_Peak_DryRun_NoWriteToEitherTable(t *testing.T) {
	// --dry-run must issue no PutItem (off-peak) and no UpdateItem (peak), while
	// still counting and summarising the intended peak write.
	loc := sydney(t)
	date := "2026-05-18"
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: fullDayReadings(t, date, loc),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{
			date: existingDailyEnergyRow(date),
		},
	}
	opts := backfillOptsForTest(loc, date)
	opts.dryRun = true
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	assert.Empty(t, f.puts, "dry-run must not write off-peak")
	assert.Empty(t, f.updates, "dry-run must not write peak")
	assert.Equal(t, 1, res.PeakWritten, "dry-run still counts the intended peak write")
	hasPeakLine := false
	for _, line := range res.Summary {
		if bytes.Contains([]byte(line), []byte("peak")) {
			hasPeakLine = true
		}
	}
	assert.True(t, hasPeakLine, "dry-run summary should include a peak line. got: %v", res.Summary)
}

func TestBackfill_Peak_OffpeakRecomputeStillWorks(t *testing.T) {
	// Adding the peak side must not change off-peak behaviour: the off-peak row
	// is still written with the recomputed deltas alongside the peak write.
	loc := sydney(t)
	date := "2026-05-18"
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: fullDayReadings(t, date, loc),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{
			date: existingDailyEnergyRow(date),
		},
	}
	opts := backfillOptsForTest(loc, date)
	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	require.Len(t, f.puts, 1, "off-peak row must still be written")
	assert.Equal(t, 1, res.RowsWritten)
	persisted := decodeOffpeakItem(t, f.puts[0].Item)
	// 1 kW over the 3h off-peak window (11:00-14:00) = 3.0 kWh.
	assert.InDelta(t, 3.0, persisted.GridUsageKwh, 0.01)
	// And peak was written independently.
	assert.Equal(t, 1, res.PeakWritten)
	require.Len(t, f.updates, 1)
}

func TestValidateOpts_RejectsMissingDailyEnergyTable(t *testing.T) {
	loc := sydney(t)
	opts := backfillOptsForTest(loc, "2026-05-18")
	opts.tableDailyEnergy = ""

	err := validateOpts(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "table-daily-energy")
}

func TestValidateOpts_RejectsReversedDateRange(t *testing.T) {
	loc := sydney(t)
	opts := backfillOptsForTest(loc, "2026-05-18")
	opts.from = "2026-04-20"
	opts.to = "2026-04-10"

	err := validateOpts(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after")
}

// The window flags are gone — the plan supplies each day's window — so the
// pricing table takes their place as a required option.
func TestValidateOpts_RejectsMissingPricingTable(t *testing.T) {
	loc := sydney(t)
	opts := backfillOptsForTest(loc, "2026-05-18")
	opts.tablePricing = ""

	err := validateOpts(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "table-pricing")
}

func decodeOffpeakItem(t *testing.T, av map[string]types.AttributeValue) dynamo.OffpeakItem {
	t.Helper()
	var got dynamo.OffpeakItem
	require.NoError(t, attributevalue.UnmarshalMap(av, &got))
	return got
}
