package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// These tests cover the switch from --offpeak-start/end flags to per-day plan
// resolution (Q24). A backfill spanning a plan switch needs a different window
// on either side of it; static flags would silently misattribute every day on
// the wrong side, which is the failure this replacement exists to prevent.

// gridReadingsForDay builds dense (10 s) readings across the whole Sydney-local
// day at a constant grid import, closing on the next local midnight so every
// band has a right bracket.
func gridReadingsForDay(t *testing.T, date string, loc *time.Location, watts float64) []dynamo.ReadingItem {
	t.Helper()
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	require.NoError(t, err)
	end := day.AddDate(0, 0, 1)
	out := make([]dynamo.ReadingItem, 0, 24*60*60/10+1)
	for ts := day.Unix(); ts <= end.Unix(); ts += 10 {
		out = append(out, dynamo.ReadingItem{
			SysSn: testSerial, Timestamp: ts, Pgrid: watts, Soc: 50,
		})
	}
	return out
}

// decodeUpdatedBands pulls the bandImports payload back out of the captured
// UpdateItem so assertions run against what would actually be persisted.
func decodeUpdatedBands(t *testing.T, f *fakeDynamo) []dynamo.BandImportAttr {
	t.Helper()
	require.Len(t, f.updates, 1)
	av, ok := f.updates[0].ExpressionAttributeValues[":bi"]
	require.True(t, ok, "the band group must set :bi")
	var bands []dynamo.BandImportAttr
	require.NoError(t, attributevalue.Unmarshal(av, &bands))
	return bands
}

// A range spanning the switch date must repair each day under its own plan's
// window — the whole reason the flags were removed.
func TestBackfill_ResolvesWindowPerDayAcrossAPlanSwitch(t *testing.T) {
	loc := sydney(t)
	const before, after = "2026-07-31", "2026-08-01"
	f := &fakeDynamo{
		location: loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {
			existingCompleteRow(before), existingCompleteRow(after),
		}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			before: gridReadingsForDay(t, before, loc, 1000),
			after:  gridReadingsForDay(t, after, loc, 1000),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{
			before: existingDailyEnergyRow(before),
			after:  existingDailyEnergyRow(after),
		},
		pricingRows: []dynamo.PricingItem{
			testPricingRow("old", "2000-01-01", after, "11:00", "14:00"),
			testPricingRow("new", after, "", "10:00", "15:00"),
		},
	}
	opts := backfillOptsForTest(loc, before, after)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Equal(t, 2, res.RowsWritten)

	geometry := map[string][2]string{}
	for _, put := range f.puts {
		row := decodeOffpeakItem(t, put.Item)
		geometry[row.Date] = [2]string{row.WindowStart, row.WindowEnd}
	}
	assert.Equal(t, [2]string{"11:00", "14:00"}, geometry[before],
		"switch eve is still priced by the predecessor")
	assert.Equal(t, [2]string{"10:00", "15:00"}, geometry[after],
		"the switch day belongs to the successor")
}

// The repaired row re-records the window it was integrated under, so a later
// plan edit shows up as a mismatch instead of silently repricing the day (Q23).
func TestBackfill_RewritesOffpeakWindowGeometry(t *testing.T) {
	loc := sydney(t)
	const date = "2026-08-03"
	// The stored row still carries the pre-switch geometry.
	stale := existingCompleteRow(date)
	stale.WindowStart = "11:00"
	stale.WindowEnd = "14:00"

	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {stale}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: gridReadingsForDay(t, date, loc, 1000),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{date: existingDailyEnergyRow(date)},
		pricingRows: []dynamo.PricingItem{
			testPricingRow("new", "2026-08-01", "", "10:00", "15:00"),
		},
	}
	opts := backfillOptsForTest(loc, date)

	_, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Len(t, f.puts, 1)

	row := decodeOffpeakItem(t, f.puts[0].Item)
	assert.Equal(t, "10:00", row.WindowStart)
	assert.Equal(t, "15:00", row.WindowEnd)
	// 1 kW across the 5-hour free window.
	assert.InDelta(t, 5.0, row.GridUsageKwh, 0.05)
}

// The band split and its total land in the same write, and the total equals
// the sum of the bands — the two describe one physical quantity, so a screen
// showing either must show the same number.
func TestBackfill_WritesBandImportsWithPeakTotal(t *testing.T) {
	loc := sydney(t)
	const date = "2026-08-03"
	rate := 0.28
	savings := 0.35
	tou := dynamo.PricingItem{
		PricingID: "tou", StartDate: "2026-08-01", DefaultRate: 0.35, FeedInRate: 0.05,
		Windows: []dynamo.PricingWindow{
			{Start: "10:00", End: "15:00", Free: true},
			{Start: "01:00", End: "06:00", Rate: &rate},
		},
		SavingsReferenceRate: &savings,
	}
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: gridReadingsForDay(t, date, loc, 1000),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{date: existingDailyEnergyRow(date)},
		pricingRows:       []dynamo.PricingItem{tou},
	}
	opts := backfillOptsForTest(loc, date)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, res.PeakWritten)

	expr := *f.updates[0].UpdateExpression
	assert.Contains(t, expr, "bandImports")
	assert.Contains(t, expr, "bandsComputedAt")
	assert.Contains(t, expr, "peakGridImportKwh")
	assert.NotContains(t, expr, "derivedStatsComputedAt", "the derived-stats group stays untouched")

	assert.Equal(t, []dynamo.BandImportAttr{
		{Start: "00:00", End: "01:00", Kwh: 1.0},
		{Start: "01:00", End: "06:00", Kwh: 5.0},
		{Start: "06:00", End: "10:00", Kwh: 4.0},
		{Start: "15:00", End: "24:00", Kwh: 9.0},
	}, decodeUpdatedBands(t, f))

	var peak float64
	require.NoError(t, attributevalue.Unmarshal(f.updates[0].ExpressionAttributeValues[":pk"], &peak))
	assert.InDelta(t, 19.0, peak, 0.05, "24 h at 1 kW less the 5 h free window")
}

// Without a plan there is no window to integrate over and no segmentation to
// capture, so repairing the day would mean inventing its geometry.
func TestBackfill_NoPlanForDate_SkipsRowEntirely(t *testing.T) {
	loc := sydney(t)
	const date = "2026-05-18"
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: gridReadingsForDay(t, date, loc, 1000),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{date: existingDailyEnergyRow(date)},
		// The only plan starts well after the date being repaired.
		pricingRows: []dynamo.PricingItem{testPricingRow("later", "2026-08-01", "", "10:00", "15:00")},
	}
	opts := backfillOptsForTest(loc, date)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	assert.Equal(t, 1, res.RowsNoPlan)
	assert.Zero(t, res.RowsWritten)
	assert.Empty(t, f.puts, "no off-peak write without a window")
	assert.Empty(t, f.updates, "no band write without a segmentation")
}

// A plan with no free band has no off-peak window to recompute, but its rated
// bands still cover the whole day — so the band side must still run.
func TestBackfill_NoFreeBand_SkipsOffpeakButStillWritesBands(t *testing.T) {
	loc := sydney(t)
	const date = "2026-08-03"
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: gridReadingsForDay(t, date, loc, 1000),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{date: existingDailyEnergyRow(date)},
		pricingRows: []dynamo.PricingItem{{
			PricingID: "flat", StartDate: "2026-08-01", DefaultRate: 0.35, FeedInRate: 0.05,
		}},
	}
	opts := backfillOptsForTest(loc, date)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	assert.Equal(t, 1, res.RowsNoFreeBand)
	assert.Zero(t, res.RowsWritten)
	assert.Empty(t, f.puts, "no free window means no off-peak row to repair")

	assert.Equal(t, 1, res.PeakWritten)
	bands := decodeUpdatedBands(t, f)
	require.Len(t, bands, 1)
	assert.Equal(t, "00:00", bands[0].Start)
	assert.Equal(t, "24:00", bands[0].End)
	assert.InDelta(t, 24.0, bands[0].Kwh, 0.05)
}

// An unreadable pricing table is fatal rather than a per-row skip: continuing
// would repair every day in the range under no window at all.
func TestBackfill_PricingReadError_PropagatedAsFatal(t *testing.T) {
	loc := sydney(t)
	f := &fakeDynamo{
		location: loc,
		scanErr:  errors.New("pricing table unreachable"),
	}
	opts := backfillOptsForTest(loc, "2026-05-18")

	_, err := runBackfill(context.Background(), f, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list pricing")
}

// AC 3.8: on a DST day the bands keep their wall-clock boundaries while the
// energies follow real elapsed time. Deriving boundaries by adding elapsed
// minutes to midnight would shift every band after the transition by an hour.
func TestBackfill_DSTDayBandsFollowWallClock(t *testing.T) {
	loc := sydney(t)
	const date = "2026-10-04" // DST start: 02:00 → 03:00, a 23-hour day
	f := &fakeDynamo{
		location:    loc,
		offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			date: gridReadingsForDay(t, date, loc, 1000),
		},
		dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{date: existingDailyEnergyRow(date)},
		pricingRows: []dynamo.PricingItem{
			testPricingRow("new", "2026-01-01", "", "10:00", "15:00"),
		},
	}
	opts := backfillOptsForTest(loc, date)
	opts.now = func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, loc) }

	_, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	bands := decodeUpdatedBands(t, f)
	require.Len(t, bands, 2)
	assert.Equal(t, "00:00", bands[0].Start)
	assert.Equal(t, "10:00", bands[0].End)
	// 00:00–10:00 local spans only nine real hours on this day.
	assert.InDelta(t, 9.0, bands[0].Kwh, 0.05)
	assert.InDelta(t, 9.0, bands[1].Kwh, 0.05, "15:00–24:00 is unaffected by the transition")
}

// The off-peak and band writes go to different tables in separate calls, so a
// re-run after an interruption must converge rather than double-apply.
func TestBackfill_RerunIsIdempotentAcrossBothWrites(t *testing.T) {
	loc := sydney(t)
	const date = "2026-08-03"
	newFake := func() *fakeDynamo {
		return &fakeDynamo{
			location:    loc,
			offpeakRows: map[string][]dynamo.OffpeakItem{"*": {existingCompleteRow(date)}},
			readingsByDate: map[string][]dynamo.ReadingItem{
				date: gridReadingsForDay(t, date, loc, 1000),
			},
			dailyEnergyByDate: map[string]dynamo.DailyEnergyItem{date: existingDailyEnergyRow(date)},
			pricingRows: []dynamo.PricingItem{
				testPricingRow("new", "2026-08-01", "", "10:00", "15:00"),
			},
		}
	}
	opts := backfillOptsForTest(loc, date)

	first := newFake()
	_, err := runBackfill(context.Background(), first, opts)
	require.NoError(t, err)

	second := newFake()
	_, err = runBackfill(context.Background(), second, opts)
	require.NoError(t, err)

	assert.Equal(t, decodeOffpeakItem(t, first.puts[0].Item), decodeOffpeakItem(t, second.puts[0].Item))
	assert.Equal(t, decodeUpdatedBands(t, first), decodeUpdatedBands(t, second))
}
