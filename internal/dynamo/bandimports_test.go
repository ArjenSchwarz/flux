package dynamo

import (
	"context"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/plan"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleBandImports is the rated-segment split of the incoming plan. The free
// band is deliberately absent: its import lives on the flux-offpeak row, which
// owns that quantity exclusively (Q31).
func sampleBandImports() []BandImportAttr {
	return []BandImportAttr{
		{Start: "00:00", End: "01:00", Kwh: 1.2},
		{Start: "01:00", End: "06:00", Kwh: 4.4},
		{Start: "06:00", End: "10:00", Kwh: 2.1},
		{Start: "15:00", End: "24:00", Kwh: 8.3},
	}
}

// TestDailyEnergyItem_BandImportsRoundTrip pins the storage shape: present
// when captured, omitted entirely on rows whose split was never computed.
func TestDailyEnergyItem_BandImportsRoundTrip(t *testing.T) {
	t.Parallel()
	t.Run("captured split round-trips", func(t *testing.T) {
		t.Parallel()
		in := DailyEnergyItem{
			SysSn: "AB1234", Date: "2026-08-02", EInput: 16.0,
			BandImports:     sampleBandImports(),
			BandsComputedAt: "2026-08-03T00:30:00Z",
		}
		av, err := attributevalue.MarshalMap(in)
		require.NoError(t, err)
		assert.Contains(t, av, "bandImports")
		assert.Contains(t, av, "bandsComputedAt")

		var out DailyEnergyItem
		require.NoError(t, attributevalue.UnmarshalMap(av, &out))
		assert.Equal(t, in.BandImports, out.BandImports)
		assert.Equal(t, in.BandsComputedAt, out.BandsComputedAt)
	})

	t.Run("pre-feature row carries neither attribute", func(t *testing.T) {
		t.Parallel()
		av, err := attributevalue.MarshalMap(DailyEnergyItem{SysSn: "AB1234", Date: "2026-04-12"})
		require.NoError(t, err)
		assert.NotContains(t, av, "bandImports")
		assert.NotContains(t, av, "bandsComputedAt")

		var out DailyEnergyItem
		require.NoError(t, attributevalue.UnmarshalMap(av, &out))
		assert.Nil(t, out.BandImports)
		assert.Empty(t, out.BandsComputedAt)
	})
}

// TestUpdateDailyEnergyDerived_BandGroup covers the third sentinel-gated
// group. It follows the peak group's contract (peak-from-readings Decision 3):
// each group is written only when its own sentinel is set, so a pass filling
// one never clobbers another.
func TestUpdateDailyEnergyDerived_BandGroup(t *testing.T) {
	t.Parallel()
	capture := func(t *testing.T, stats DerivedStats) *dynamodb.UpdateItemInput {
		t.Helper()
		var got *dynamodb.UpdateItemInput
		mock := &fakeDynamoAPIv2{
			updateItemFn: func(_ context.Context, params *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
				got = params
				return &dynamodb.UpdateItemOutput{}, nil
			},
		}
		store := NewDynamoStore(mock, testTables())
		require.NoError(t, store.UpdateDailyEnergyDerived(context.Background(), "AB1234", "2026-08-02", stats))
		return got
	}

	t.Run("bands only — other groups absent", func(t *testing.T) {
		t.Parallel()
		got := capture(t, DerivedStats{
			BandImports:     sampleBandImports(),
			BandsComputedAt: "2026-08-03T00:30:00Z",
		})
		require.NotNil(t, got)
		expr := *got.UpdateExpression
		assert.Contains(t, expr, "bandImports")
		assert.Contains(t, expr, "bandsComputedAt")
		for _, name := range []string{"dailyUsage", "socLow", "peakPeriods", "derivedStatsComputedAt", "peakGridImportKwh", "peakComputedAt"} {
			assert.NotContains(t, expr, name, "%s must be absent when its sentinel is unset", name)
		}
		_, isList := got.ExpressionAttributeValues[":bi"].(*types.AttributeValueMemberL)
		assert.True(t, isList, "a captured split must marshal as a list")
	})

	t.Run("other groups only — band attributes absent", func(t *testing.T) {
		t.Parallel()
		got := capture(t, DerivedStats{DerivedStatsComputedAt: "2026-08-03T00:30:00Z"})
		require.NotNil(t, got)
		expr := *got.UpdateExpression
		assert.NotContains(t, expr, "bandImports")
		assert.NotContains(t, expr, "bandsComputedAt")
	})

	// Usability gate mirror of PeakGridImportKwh: when the integrator cannot
	// produce every rated segment the value stays absent, but the sentinel is
	// still set so the row is not re-attempted every hour.
	t.Run("nil split with sentinel set marshals as NULL", func(t *testing.T) {
		t.Parallel()
		got := capture(t, DerivedStats{BandsComputedAt: "2026-08-03T00:30:00Z"})
		require.NotNil(t, got)
		expr := *got.UpdateExpression
		assert.Contains(t, expr, "bandsComputedAt")
		_, isNull := got.ExpressionAttributeValues[":bi"].(*types.AttributeValueMemberNULL)
		assert.True(t, isNull, "an unavailable split must marshal as NULL")
	})

	t.Run("all three groups in one call", func(t *testing.T) {
		t.Parallel()
		peak := 12.5
		got := capture(t, DerivedStats{
			DerivedStatsComputedAt: "2026-08-03T00:30:00Z",
			PeakGridImportKwh:      &peak,
			PeakComputedAt:         "2026-08-03T00:30:00Z",
			BandImports:            sampleBandImports(),
			BandsComputedAt:        "2026-08-03T00:30:00Z",
		})
		require.NotNil(t, got)
		expr := *got.UpdateExpression
		for _, name := range []string{"dailyUsage", "socLow", "peakPeriods", "derivedStatsComputedAt", "peakGridImportKwh", "peakComputedAt", "bandImports", "bandsComputedAt"} {
			assert.Contains(t, expr, name)
		}
	})
}

// TestOffpeakItem_WindowGeometryRoundTrip pins the geometry snapshot that
// makes a later free-window edit detectable as a mismatch instead of silently
// mispricing the day.
func TestOffpeakItem_WindowGeometryRoundTrip(t *testing.T) {
	t.Parallel()
	t.Run("snapshot round-trips", func(t *testing.T) {
		t.Parallel()
		in := OffpeakItem{
			SysSn: "AB1234", Date: "2026-08-02", Status: "complete",
			GridUsageKwh: 3.2,
			WindowStart:  "10:00",
			WindowEnd:    "15:00",
		}
		av, err := attributevalue.MarshalMap(in)
		require.NoError(t, err)
		assert.Contains(t, av, "windowStart")
		assert.Contains(t, av, "windowEnd")

		var out OffpeakItem
		require.NoError(t, attributevalue.UnmarshalMap(av, &out))
		assert.Equal(t, "10:00", out.WindowStart)
		assert.Equal(t, "15:00", out.WindowEnd)
	})

	t.Run("pre-feature row carries no geometry", func(t *testing.T) {
		t.Parallel()
		av, err := attributevalue.MarshalMap(OffpeakItem{SysSn: "AB1234", Date: "2026-04-12", Status: "complete"})
		require.NoError(t, err)
		assert.NotContains(t, av, "windowStart")
		assert.NotContains(t, av, "windowEnd")
	})
}

// TestOffpeakItemGeometry covers the pre-feature default: a row with no
// snapshot can only have been computed under 11:00–14:00, the window that was
// configured for its whole lifetime.
func TestOffpeakItemGeometry(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		item      OffpeakItem
		wantStart string
		wantEnd   string
	}{
		"snapshotted":                 {item: OffpeakItem{WindowStart: "10:00", WindowEnd: "15:00"}, wantStart: "10:00", wantEnd: "15:00"},
		"pre-feature row":             {item: OffpeakItem{}, wantStart: "11:00", wantEnd: "14:00"},
		"half-written snapshot":       {item: OffpeakItem{WindowStart: "10:00"}, wantStart: "11:00", wantEnd: "14:00"},
		"half-written snapshot (end)": {item: OffpeakItem{WindowEnd: "15:00"}, wantStart: "11:00", wantEnd: "14:00"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start, end := tc.item.Geometry()
			assert.Equal(t, tc.wantStart, start)
			assert.Equal(t, tc.wantEnd, end)
		})
	}
}

// TestOffpeakItemUsable pins the sparse-complete rule: a row integrated
// without any samples is a zero-delta artifact, not a measured zero, so it
// cannot be used to price a free band.
func TestOffpeakItemUsable(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		item OffpeakItem
		want bool
	}{
		"integrated with samples":      {item: OffpeakItem{IntegratedAt: "2026-08-02T05:00:00Z", IntegrationSampleCount: 900}, want: true},
		"integrated without samples":   {item: OffpeakItem{IntegratedAt: "2026-08-02T05:00:00Z"}, want: false},
		"pre-integration snapshot row": {item: OffpeakItem{GridUsageKwh: 3.0}, want: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.item.Usable())
		})
	}
}

// --- IntegrateRatedBands ---

// bandFixtureReadings synthesises a constant-power day at 60 s cadence over
// [dayStart, dayStart+24h] in loc, importing `pgrid` watts from the grid the
// whole time. The closing sample sits exactly on the next midnight so the
// integrator has a right bracket for the last band and the expected energies
// are whole numbers.
func bandFixtureReadings(dayStart time.Time, pgrid float64) []derivedstats.Reading {
	dayEnd := dayStart.AddDate(0, 0, 1)
	out := make([]derivedstats.Reading, 0, 24*60+1)
	for ts := dayStart; !ts.After(dayEnd); ts = ts.Add(time.Minute) {
		out = append(out, derivedstats.Reading{Timestamp: ts.Unix(), Pgrid: pgrid})
	}
	return out
}

// bandTestPlan is the incoming time-of-use plan: free 10:00–15:00, a cheaper
// band 01:00–06:00, the default rate otherwise.
func bandTestPlan() plan.Plan {
	savings := 0.35
	return plan.Plan{
		ID: "tou", StartDate: "2026-01-01", DefaultRate: 0.35,
		Windows: []plan.Window{
			{Start: "10:00", End: "15:00", Free: true},
			{Start: "01:00", End: "06:00", Rate: 0.28},
		},
		FeedInRate: 0.05, SavingsRefRate: &savings,
	}
}

func TestIntegrateRatedBands_GeometryMatchesRatedSegments(t *testing.T) {
	t.Parallel()
	sydney, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)
	day := time.Date(2026, 8, 12, 0, 0, 0, 0, sydney)

	// 1000 W of constant import: each band's kWh is exactly its hour count.
	bands, total, ok := IntegrateRatedBands(bandFixtureReadings(day, 1000), bandTestPlan(), day, sydney)

	require.True(t, ok)
	require.Len(t, bands, 4, "the free band splits the day into four rated segments")
	assert.Equal(t, []BandImportAttr{
		{Start: "00:00", End: "01:00", Kwh: 1.0},
		{Start: "01:00", End: "06:00", Kwh: 5.0},
		{Start: "06:00", End: "10:00", Kwh: 4.0},
		{Start: "15:00", End: "24:00", Kwh: 9.0},
	}, bands)
	// 24 h day minus the 5 h free window = 19 kWh at 1 kW.
	assert.InDelta(t, 19.0, total, 0.01)
}

// The free band's import is deliberately excluded: the flux-offpeak row owns
// that quantity (Q31), and capturing it twice is what backfill repairs would
// desynchronise.
func TestIntegrateRatedBands_ExcludesFreeWindow(t *testing.T) {
	t.Parallel()
	sydney, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)
	day := time.Date(2026, 8, 12, 0, 0, 0, 0, sydney)

	bands, _, ok := IntegrateRatedBands(bandFixtureReadings(day, 1000), bandTestPlan(), day, sydney)

	require.True(t, ok)
	for _, b := range bands {
		assert.False(t, b.Start == "10:00" && b.End == "15:00", "the free band must not be stored")
	}
}

// A plan with no free band leaves the whole day rated, so the bands tile
// 00:00–24:00 and the total is the day's entire grid import.
func TestIntegrateRatedBands_WholeDayWhenNoFreeBand(t *testing.T) {
	t.Parallel()
	sydney, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)
	day := time.Date(2026, 8, 12, 0, 0, 0, 0, sydney)
	rated := plan.Plan{ID: "flat", StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05}

	bands, total, ok := IntegrateRatedBands(bandFixtureReadings(day, 1000), rated, day, sydney)

	require.True(t, ok)
	require.Len(t, bands, 1)
	assert.Equal(t, "00:00", bands[0].Start)
	assert.Equal(t, "24:00", bands[0].End)
	assert.InDelta(t, 24.0, total, 0.01)
}

// AC 3.8: on a DST day the bands follow local wall-clock time and still sum to
// the day's total — which only holds because the boundaries come from
// plan.SegmentBounds rather than midnight-plus-elapsed arithmetic.
func TestIntegrateRatedBands_DSTDaySumsToWholeDay(t *testing.T) {
	t.Parallel()
	sydney, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)

	tests := map[string]struct {
		day     time.Time
		wantKwh float64 // hours of rated time at 1 kW
	}{
		// 2026-10-04: DST start, 02:00 → 03:00 skipped. A 23-hour day, of
		// which the 10:00–15:00 free window still removes 5 wall-clock hours,
		// but 01:00–06:00 spans only 4 real hours.
		"dst start (23h day)": {day: time.Date(2026, 10, 4, 0, 0, 0, 0, sydney), wantKwh: 18.0},
		// 2026-04-05: DST end, 03:00 repeated. A 25-hour day.
		"dst end (25h day)": {day: time.Date(2026, 4, 5, 0, 0, 0, 0, sydney), wantKwh: 20.0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			readings := bandFixtureReadings(tc.day, 1000)
			bands, total, ok := IntegrateRatedBands(readings, bandTestPlan(), tc.day, sydney)
			require.True(t, ok)

			// The bands' wall-clock geometry is the same on any day...
			assert.Equal(t, "00:00", bands[0].Start)
			assert.Equal(t, "24:00", bands[len(bands)-1].End)
			// ...but the energy follows real elapsed time.
			assert.InDelta(t, tc.wantKwh, total, 0.05)

			// The whole-day integral over the same readings, less the free
			// window, must equal the band total — the invariant that breaks
			// if any boundary is computed by elapsed-minute arithmetic.
			dayStart := tc.day
			dayEnd := tc.day.AddDate(0, 0, 1)
			whole, wholeOK := derivedstats.IntegrateOffpeakDeltas(readings, dayStart.Unix(), dayEnd.Unix())
			require.True(t, wholeOK)
			freeStart := time.Date(tc.day.Year(), tc.day.Month(), tc.day.Day(), 10, 0, 0, 0, sydney)
			freeEnd := time.Date(tc.day.Year(), tc.day.Month(), tc.day.Day(), 15, 0, 0, 0, sydney)
			free, freeOK := derivedstats.IntegrateOffpeakDeltas(readings, freeStart.Unix(), freeEnd.Unix())
			require.True(t, freeOK)
			assert.InDelta(t, whole.GridImportKwh-free.GridImportKwh, total, 0.05)
		})
	}
}

// A partially known split is unavailable, not partially usable (AC 3.6): one
// unusable segment discards the whole result so no caller can persist half a
// day's bands.
func TestIntegrateRatedBands_UnusableSegmentDiscardsSplit(t *testing.T) {
	t.Parallel()
	sydney, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)
	day := time.Date(2026, 8, 12, 0, 0, 0, 0, sydney)

	// Readings only from 06:00 onward: the 00:00–01:00 and 01:00–06:00
	// segments have nothing to integrate.
	full := bandFixtureReadings(day, 1000)
	late := full[6*60:]

	_, _, ok := IntegrateRatedBands(late, bandTestPlan(), day, sydney)
	assert.False(t, ok, "a segment with no readings makes the whole split unavailable")
}

func TestIntegrateRatedBands_NoRatedSegments(t *testing.T) {
	t.Parallel()
	sydney, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)
	day := time.Date(2026, 8, 12, 0, 0, 0, 0, sydney)
	savings := 0.35
	allFree := plan.Plan{
		ID: "all-free", StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
		Windows:        []plan.Window{{Start: "00:00", End: "24:00", Free: true}},
		SavingsRefRate: &savings,
	}

	_, _, ok := IntegrateRatedBands(bandFixtureReadings(day, 1000), allFree, day, sydney)
	assert.False(t, ok, "a plan with no rated band has no split to capture")
}
