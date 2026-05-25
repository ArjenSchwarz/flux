package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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
	testSerial        = "TEST123"
	testOffpeakTable  = "flux-offpeak-test"
	testReadingsTable = "flux-readings-test"
)

// fakeDynamo is a lightweight in-memory stand-in for the DynamoDB client.
// It returns canned offpeak and readings rows from Query, and records every
// PutItem call so tests can assert on the persisted payload and condition
// expression.
type fakeDynamo struct {
	offpeakRows      map[string][]dynamo.OffpeakItem // keyed by "*"
	readingsByDate   map[string][]dynamo.ReadingItem // keyed by Sydney YYYY-MM-DD
	location         *time.Location
	queries          []*dynamodb.QueryInput
	puts             []*dynamodb.PutItemInput
	queryErrForTable map[string]error
	putErr           error
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
		serial:        testSerial,
		tableOffpeak:  testOffpeakTable,
		tableReadings: testReadingsTable,
		from:          from,
		to:            to,
		offpeakStart:  "11:00",
		offpeakEnd:    "14:00",
		location:      loc,
		now:           func() time.Time { return now },
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

func TestValidateOpts_RejectsReversedDateRange(t *testing.T) {
	loc := sydney(t)
	opts := backfillOptsForTest(loc, "2026-05-18")
	opts.from = "2026-04-20"
	opts.to = "2026-04-10"

	err := validateOpts(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after")
}

func TestValidateOpts_RejectsMissingWindow(t *testing.T) {
	loc := sydney(t)
	opts := backfillOptsForTest(loc, "2026-05-18")
	opts.offpeakStart = ""

	err := validateOpts(opts)
	require.Error(t, err)
}

func decodeOffpeakItem(t *testing.T, av map[string]types.AttributeValue) dynamo.OffpeakItem {
	t.Helper()
	var got dynamo.OffpeakItem
	require.NoError(t, attributevalue.UnmarshalMap(av, &got))
	return got
}
