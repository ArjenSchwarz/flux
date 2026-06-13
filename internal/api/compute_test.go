package api

import (
	"math"
	"testing"
	"testing/quick"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOffpeakDeltas(t *testing.T) {
	tests := map[string]struct {
		op       dynamo.OffpeakItem
		wantOK   bool
		wantVals offpeakDeltaValues
	}{
		"complete: pass through final deltas": {
			op: dynamo.OffpeakItem{
				Status:              dynamo.OffpeakStatusComplete,
				GridUsageKwh:        2.5,
				SolarKwh:            5.0,
				BatteryChargeKwh:    1.0,
				BatteryDischargeKwh: 0.5,
				GridExportKwh:       0.3,
			},
			wantOK: true,
			wantVals: offpeakDeltaValues{
				GridImport: 2.5, Solar: 5.0, BatteryCharge: 1.0,
				BatteryDischarge: 0.5, GridExport: 0.3,
			},
		},
		"pending: not handled here; caller live-integrates": {
			op:     dynamo.OffpeakItem{Status: dynamo.OffpeakStatusPending, StartEInput: 2.0},
			wantOK: false,
		},
		"unknown status: not computable": {
			op:     dynamo.OffpeakItem{Status: "future-status"},
			wantOK: false,
		},
		"empty status: not computable": {
			op:     dynamo.OffpeakItem{},
			wantOK: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := offpeakDeltas(tc.op)
			assert.Equal(t, tc.wantOK, ok)
			if !ok {
				return
			}
			assert.InDelta(t, tc.wantVals.GridImport, got.GridImport, 0.001)
			assert.InDelta(t, tc.wantVals.Solar, got.Solar, 0.001)
			assert.InDelta(t, tc.wantVals.BatteryCharge, got.BatteryCharge, 0.001)
			assert.InDelta(t, tc.wantVals.BatteryDischarge, got.BatteryDischarge, 0.001)
			assert.InDelta(t, tc.wantVals.GridExport, got.GridExport, 0.001)
		})
	}
}

func TestLiveOffpeakDeltas(t *testing.T) {
	// Sydney-local 2026-04-15. Window 11:00-14:00 (3h).
	loc := sydneyTZ
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, loc)
	const windowStart = "11:00"
	const windowEnd = "14:00"
	opStart := dayStart.Add(11 * time.Hour)
	opEnd := dayStart.Add(14 * time.Hour)

	// Build a uniform 10-second cadence reading stream covering the full
	// off-peak window plus a 60s shoulder either side. Constant pgrid = 3600 W
	// so each second contributes exactly 1 Wh; trivial to assert kWh totals.
	const pgridW = 3600.0
	readings := make([]dynamo.ReadingItem, 0)
	for ts := opStart.Add(-60 * time.Second).Unix(); ts <= opEnd.Add(60*time.Second).Unix(); ts += 10 {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: ts,
			Pgrid:     pgridW,
		})
	}

	tests := map[string]struct {
		now      time.Time
		wantOK   bool
		wantKwh  float64 // expected GridImport for the live window
		approxKw float64 // tolerance in kWh
	}{
		"before window returns false": {
			now:    opStart.Add(-time.Minute),
			wantOK: false,
		},
		"at window start has no usable interval (zero length)": {
			now:    opStart,
			wantOK: false,
		},
		"30 minutes into window integrates first 30 minutes": {
			now:    opStart.Add(30 * time.Minute),
			wantOK: true,
			// 3600 W * 1800 s = 6_480_000 J = 1.8 kWh
			wantKwh:  1.8,
			approxKw: 0.001,
		},
		"at window end integrates the full window": {
			now:    opEnd,
			wantOK: true,
			// 3600 W * 3 h = 10.8 kWh
			wantKwh:  10.8,
			approxKw: 0.001,
		},
		"after window end caps at window end (full window)": {
			now:      opEnd.Add(30 * time.Minute),
			wantOK:   true,
			wantKwh:  10.8,
			approxKw: 0.001,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := liveOffpeakDeltas(readings, tc.now, windowStart, windowEnd)
			assert.Equal(t, tc.wantOK, ok)
			if !ok {
				return
			}
			assert.InDelta(t, tc.wantKwh, got.GridImport, tc.approxKw)
			// All five deltas non-negative by construction (Decision 8).
			assert.GreaterOrEqual(t, got.GridImport, 0.0)
			assert.GreaterOrEqual(t, got.GridExport, 0.0)
			assert.GreaterOrEqual(t, got.BatteryCharge, 0.0)
			assert.GreaterOrEqual(t, got.BatteryDischarge, 0.0)
			assert.GreaterOrEqual(t, got.Solar, 0.0)
		})
	}
}

// TestLiveOffpeakDeltasDeterminism asserts that the same inputs always produce
// the same outputs — the basis for AC 4.4's monotonicity property test.
func TestLiveOffpeakDeltasDeterminism(t *testing.T) {
	loc := sydneyTZ
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, loc)
	const windowStart = "11:00"
	const windowEnd = "14:00"
	opStart := dayStart.Add(11 * time.Hour)

	readings := []dynamo.ReadingItem{
		{Timestamp: opStart.Unix(), Pgrid: 1500, Ppv: 200, Pbat: -800},
		{Timestamp: opStart.Add(10 * time.Second).Unix(), Pgrid: 1600, Ppv: 250, Pbat: -900},
		{Timestamp: opStart.Add(20 * time.Second).Unix(), Pgrid: 1700, Ppv: 300, Pbat: -1000},
		{Timestamp: opStart.Add(30 * time.Second).Unix(), Pgrid: 1800, Ppv: 350, Pbat: -1100},
	}

	now := opStart.Add(time.Hour)

	first, ok1 := liveOffpeakDeltas(readings, now, windowStart, windowEnd)
	second, ok2 := liveOffpeakDeltas(readings, now, windowStart, windowEnd)

	require.True(t, ok1)
	require.True(t, ok2)
	assert.Equal(t, first, second, "same inputs must produce identical outputs")
}

// TestLivePeakGridImport covers the "peak so far today" integration: the two
// windows bracketing off-peak, each clamped to now, gated on the morning
// window only (T-1420 / T-1421). Window 11:00-14:00, so peak windows are
// [00:00, 11:00) and [14:00, 24:00).
func TestLivePeakGridImport(t *testing.T) {
	loc := sydneyTZ
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, loc)
	const windowStart = "11:00"
	const windowEnd = "14:00"
	opStart := dayStart.Add(11 * time.Hour)
	opEnd := dayStart.Add(14 * time.Hour)

	// Uniform 10-second cadence covering the whole day plus a 60s shoulder
	// before midnight (for left-edge synthesis). Constant pgrid = 3600 W so
	// each second contributes exactly 1 Wh: kWh == hours integrated.
	const pgridW = 3600.0
	readings := make([]dynamo.ReadingItem, 0)
	for ts := dayStart.Add(-60 * time.Second).Unix(); ts <= dayStart.Add(24*time.Hour).Unix(); ts += 10 {
		readings = append(readings, dynamo.ReadingItem{Timestamp: ts, Pgrid: pgridW})
	}

	tests := map[string]struct {
		now     time.Time
		wantOK  bool
		wantKwh float64 // expected peak grid import so far
	}{
		"before off-peak: whole day so far is peak": {
			// 05:00 -> [00:00, 05:00) = 5 h.
			now: dayStart.Add(5 * time.Hour), wantOK: true, wantKwh: 18.0,
		},
		"at off-peak start: full morning, no evening": {
			// [00:00, 11:00) = 11 h.
			now: opStart, wantOK: true, wantKwh: 39.6,
		},
		"during off-peak: morning only, evening still zero": {
			// 12:30 -> morning 11 h, evening not yet open.
			now: dayStart.Add(12*time.Hour + 30*time.Minute), wantOK: true, wantKwh: 39.6,
		},
		"at off-peak end: morning only (evening zero-length)": {
			now: opEnd, wantOK: true, wantKwh: 39.6,
		},
		"after off-peak end: morning plus partial evening": {
			// 14:30 -> morning 11 h + evening 0.5 h = 11.5 h.
			now: opEnd.Add(30 * time.Minute), wantOK: true, wantKwh: 41.4,
		},
		"late evening: morning plus most of evening": {
			// 23:00 -> morning 11 h + evening [14:00, 23:00) = 9 h = 20 h total.
			now: dayStart.Add(23 * time.Hour), wantOK: true, wantKwh: 72.0,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := livePeakGridImport(readings, tc.now, windowStart, windowEnd)
			assert.Equal(t, tc.wantOK, ok)
			if !ok {
				return
			}
			assert.InDelta(t, tc.wantKwh, got, 0.01)
			assert.GreaterOrEqual(t, got, 0.0)
		})
	}
}

// TestLivePeakGridImportGates covers the not-usable paths: an unparseable
// window and a morning window with too few samples to integrate.
func TestLivePeakGridImportGates(t *testing.T) {
	loc := sydneyTZ
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, loc)

	t.Run("unparseable window returns false", func(t *testing.T) {
		readings := []dynamo.ReadingItem{
			{Timestamp: dayStart.Add(time.Hour).Unix(), Pgrid: 1000},
			{Timestamp: dayStart.Add(time.Hour + 10*time.Second).Unix(), Pgrid: 1000},
		}
		_, ok := livePeakGridImport(readings, dayStart.Add(2*time.Hour), "nope", "14:00")
		assert.False(t, ok)
	})

	t.Run("single morning sample is not integrable", func(t *testing.T) {
		readings := []dynamo.ReadingItem{
			{Timestamp: dayStart.Add(time.Hour).Unix(), Pgrid: 1000},
		}
		_, ok := livePeakGridImport(readings, dayStart.Add(2*time.Hour), "11:00", "14:00")
		assert.False(t, ok)
	})
}

// TestBuildOffpeakDispatch covers the live-vs-stored dispatch in buildOffpeak.
// Pending rows live-integrate from the readings slice; complete rows
// pass through op.GridUsageKwh etc. The op.StartE* fields are never read.
func TestBuildOffpeakDispatch(t *testing.T) {
	loc := sydneyTZ
	// In-window now: 11:30 AEST so 30 minutes of the 11:00-14:00 window have
	// elapsed. liveOffpeakDeltas integrates over [11:00, 11:30).
	now := time.Date(2026, 4, 15, 11, 30, 0, 0, loc)
	opStart := time.Date(2026, 4, 15, 11, 0, 0, 0, loc)

	// 30 minutes of constant 3600W grid import at 10s cadence.
	// 3600 W * 1800 s = 1.8 kWh.
	readings := make([]dynamo.ReadingItem, 0)
	for ts := opStart.Unix(); ts <= now.Unix(); ts += 10 {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: ts,
			Pgrid:     3600,
		})
	}

	t.Run("pending row dispatches to live integration", func(t *testing.T) {
		op := &dynamo.OffpeakItem{
			Status: dynamo.OffpeakStatusPending,
			// StartE* values must NOT be read — if they were, the test would
			// produce values reflecting these obviously-wrong baselines.
			StartEpv:        999.0,
			StartEInput:     999.0,
			StartEOutput:    999.0,
			StartECharge:    999.0,
			StartEDischarge: 999.0,
		}
		got := buildOffpeak(op, readings, now, "11:00", "14:00")
		require.NotNil(t, got)
		assert.Equal(t, "11:00", got.WindowStart)
		assert.Equal(t, "14:00", got.WindowEnd)
		require.NotNil(t, got.GridUsageKwh)
		assert.InDelta(t, 1.8, *got.GridUsageKwh, 0.01)
		// BatteryDeltaPercent is only set on complete rows.
		assert.Nil(t, got.BatteryDeltaPercent)
	})

	t.Run("complete row passes through stored deltas", func(t *testing.T) {
		op := &dynamo.OffpeakItem{
			Status:              dynamo.OffpeakStatusComplete,
			GridUsageKwh:        2.5,
			SolarKwh:            5.0,
			BatteryChargeKwh:    1.0,
			BatteryDischargeKwh: 0.5,
			GridExportKwh:       0.3,
			BatteryDeltaPercent: 12.0,
		}
		// Readings would integrate to 1.8 if used — assert we see 2.5 instead,
		// proving the complete branch reads from the row.
		got := buildOffpeak(op, readings, now, "11:00", "14:00")
		require.NotNil(t, got)
		require.NotNil(t, got.GridUsageKwh)
		assert.Equal(t, 2.5, *got.GridUsageKwh)
		require.NotNil(t, got.BatteryDeltaPercent)
		assert.Equal(t, 12.0, *got.BatteryDeltaPercent)
	})

	t.Run("nil item returns window only", func(t *testing.T) {
		got := buildOffpeak(nil, readings, now, "11:00", "14:00")
		require.NotNil(t, got)
		assert.Equal(t, "11:00", got.WindowStart)
		assert.Equal(t, "14:00", got.WindowEnd)
		assert.Nil(t, got.GridUsageKwh)
	})

	t.Run("pending row before window returns no deltas (AC 4.3)", func(t *testing.T) {
		beforeWindow := time.Date(2026, 4, 15, 10, 0, 0, 0, loc)
		op := &dynamo.OffpeakItem{Status: dynamo.OffpeakStatusPending}
		got := buildOffpeak(op, readings, beforeWindow, "11:00", "14:00")
		require.NotNil(t, got)
		assert.Equal(t, "11:00", got.WindowStart)
		assert.Empty(t, got.Status, "no status before window opens")
		assert.Nil(t, got.GridUsageKwh)
	})
}

// TestOffpeakSplitDispatch covers the live-vs-stored dispatch in offpeakSplit.
// The today + pending branch live-integrates; the complete branch (any date)
// reads from the row. Past-date pending rows still return hasSplit=false.
func TestOffpeakSplitDispatch(t *testing.T) {
	loc := sydneyTZ
	now := time.Date(2026, 4, 15, 11, 30, 0, 0, loc)
	opStart := time.Date(2026, 4, 15, 11, 0, 0, 0, loc)

	readings := make([]dynamo.ReadingItem, 0)
	for ts := opStart.Unix(); ts <= now.Unix(); ts += 10 {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: ts,
			Pgrid:     3600,
		})
	}

	t.Run("pending today row dispatches to live integration", func(t *testing.T) {
		op := dynamo.OffpeakItem{
			Status:      dynamo.OffpeakStatusPending,
			StartEInput: 999.0, // must not be read
		}
		imp, exp, hasSplit := offpeakSplit(op, readings, now, true, "11:00", "14:00")
		require.True(t, hasSplit)
		assert.InDelta(t, 1.8, imp, 0.01)
		assert.InDelta(t, 0, exp, 0.01)
	})

	t.Run("complete row passes through deltas regardless of date", func(t *testing.T) {
		op := dynamo.OffpeakItem{
			Status:        dynamo.OffpeakStatusComplete,
			GridUsageKwh:  2.5,
			GridExportKwh: 0.3,
		}
		// Past day (isToday=false) — still a pass-through.
		imp, exp, hasSplit := offpeakSplit(op, nil, now, false, "11:00", "14:00")
		require.True(t, hasSplit)
		assert.Equal(t, 2.5, imp)
		assert.Equal(t, 0.3, exp)
	})

	t.Run("pending past-date row has no split", func(t *testing.T) {
		op := dynamo.OffpeakItem{Status: dynamo.OffpeakStatusPending}
		_, _, hasSplit := offpeakSplit(op, readings, now, false, "11:00", "14:00")
		assert.False(t, hasSplit, "pending past-date row indicates poller failure, no split")
	})

	t.Run("pending today row before offpeak-start returns no split (AC 4.3)", func(t *testing.T) {
		beforeWindow := time.Date(2026, 4, 15, 10, 0, 0, 0, loc)
		op := dynamo.OffpeakItem{Status: dynamo.OffpeakStatusPending}
		_, _, hasSplit := offpeakSplit(op, readings, beforeWindow, true, "11:00", "14:00")
		assert.False(t, hasSplit)
	})
}

func TestComputeCutoffTime(t *testing.T) {
	now := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		soc           float64
		pbat          float64
		capacityKwh   float64
		cutoffPercent float64
		want          *time.Time
	}{
		"discharging normal": {
			soc: 50, pbat: 1000, capacityKwh: 10, cutoffPercent: 10,
			want: timePtr(now.Add(4 * time.Hour)),
		},
		"charging returns nil": {
			soc: 50, pbat: -500, capacityKwh: 10, cutoffPercent: 10,
			want: nil,
		},
		"idle returns nil": {
			soc: 50, pbat: 0, capacityKwh: 10, cutoffPercent: 10,
			want: nil,
		},
		"soc at cutoff returns nil": {
			soc: 10, pbat: 1000, capacityKwh: 10, cutoffPercent: 10,
			want: nil,
		},
		"soc below cutoff returns nil": {
			soc: 5, pbat: 1000, capacityKwh: 10, cutoffPercent: 10,
			want: nil,
		},
		"zero capacity returns nil": {
			soc: 50, pbat: 1000, capacityKwh: 0, cutoffPercent: 10,
			want: nil,
		},
		"negative capacity returns nil": {
			soc: 50, pbat: 1000, capacityKwh: -5, cutoffPercent: 10,
			want: nil,
		},
		"calculation verification": {
			soc: 80, pbat: 2000, capacityKwh: 13.34, cutoffPercent: 10,
			want: func() *time.Time {
				hours := 9.338 / 2.0
				t := now.Add(time.Duration(hours * float64(time.Hour)))
				return &t
			}(),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := computeCutoffTime(tc.soc, tc.pbat, tc.capacityKwh, tc.cutoffPercent, now)
			if tc.want == nil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.WithinDuration(t, *tc.want, *got, time.Millisecond)
			}
		})
	}
}

func TestNextOffpeakStart(t *testing.T) {
	const opStart = "11:00"
	const opEnd = "14:00"

	syd := func(h, m int) time.Time {
		return time.Date(2026, 4, 15, h, m, 0, 0, sydneyTZ)
	}

	tests := map[string]struct {
		now          time.Time
		offpeakStart string
		offpeakEnd   string
		wantValid    bool
		wantStart    time.Time
	}{
		"morning before window": {
			now:          syd(9, 0),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantValid: true,
			wantStart: syd(11, 0),
		},
		"exactly at window start": {
			now:          syd(11, 0),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantValid: true,
			wantStart: syd(11, 0),
		},
		"inside window": {
			now:          syd(12, 30),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantValid: true,
			wantStart: syd(11, 0),
		},
		"exactly at window end rolls to tomorrow": {
			now:          syd(14, 0),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantValid: true,
			wantStart: syd(11, 0).AddDate(0, 0, 1),
		},
		"after window same day": {
			now:          syd(18, 0),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantValid: true,
			wantStart: syd(11, 0).AddDate(0, 0, 1),
		},
		"invalid window returns false": {
			now:          syd(9, 0),
			offpeakStart: "bad", offpeakEnd: "also-bad",
			wantValid: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := nextOffpeakStart(tc.now, tc.offpeakStart, tc.offpeakEnd)
			assert.Equal(t, tc.wantValid, ok)
			if tc.wantValid {
				assert.True(t, got.Equal(tc.wantStart),
					"nextOffpeakStart(%s, %s, %s) = %s, want %s",
					tc.now, tc.offpeakStart, tc.offpeakEnd, got, tc.wantStart)
			}
		})
	}
}

func TestStartOfDaySydney(t *testing.T) {
	syd := func(y, m, d, h, mi int) time.Time {
		return time.Date(y, time.Month(m), d, h, mi, 0, 0, sydneyTZ)
	}

	tests := map[string]struct {
		now  time.Time
		want time.Time
	}{
		"morning weekday returns midnight same date": {
			now:  syd(2026, 4, 15, 6, 0),
			want: syd(2026, 4, 15, 0, 0),
		},
		"just past midnight returns midnight same date": {
			now:  syd(2026, 4, 15, 0, 0).Add(time.Second),
			want: syd(2026, 4, 15, 0, 0),
		},
		"near end of day returns midnight same date": {
			now:  syd(2026, 4, 15, 23, 59),
			want: syd(2026, 4, 15, 0, 0),
		},
		"utc instant on next sydney day returns sydney tomorrow midnight": {
			// 2026-04-14 23:00 UTC = 2026-04-15 09:00 AEST → midnight same Sydney date.
			now:  time.Date(2026, 4, 14, 23, 0, 0, 0, time.UTC),
			want: syd(2026, 4, 15, 0, 0),
		},
		"utc instant late on previous sydney day returns sydney same midnight": {
			// 2026-04-15 13:30 UTC = 2026-04-15 23:30 AEST → midnight same Sydney date.
			now:  time.Date(2026, 4, 15, 13, 30, 0, 0, time.UTC),
			want: syd(2026, 4, 15, 0, 0),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := startOfDaySydney(tc.now)
			assert.True(t, got.Equal(tc.want),
				"startOfDaySydney(%s) = %s, want %s", tc.now, got, tc.want)
		})
	}
}

func TestComputeRollingAverages(t *testing.T) {
	tests := map[string]struct {
		readings []dynamo.ReadingItem
		wantLoad float64
		wantPbat float64
	}{
		"empty slice": {
			readings: nil,
			wantLoad: 0, wantPbat: 0,
		},
		"single reading": {
			readings: []dynamo.ReadingItem{
				{Pload: 1500, Pbat: 800},
			},
			wantLoad: 1500, wantPbat: 800,
		},
		"multiple readings": {
			readings: []dynamo.ReadingItem{
				{Pload: 1000, Pbat: 500},
				{Pload: 2000, Pbat: 1500},
				{Pload: 3000, Pbat: 1000},
			},
			wantLoad: 2000, wantPbat: 1000,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotLoad, gotPbat := computeRollingAverages(tc.readings)
			assert.InDelta(t, tc.wantLoad, gotLoad, 0.001)
			assert.InDelta(t, tc.wantPbat, gotPbat, 0.001)
		})
	}
}

func TestComputePgridSustained(t *testing.T) {
	base := int64(1713168000)

	tests := map[string]struct {
		readings []dynamo.ReadingItem
		want     bool
	}{
		"empty readings": {
			readings: nil,
			want:     false,
		},
		"3 consecutive above threshold": {
			readings: []dynamo.ReadingItem{
				{Timestamp: base, Pgrid: 600},
				{Timestamp: base + 10, Pgrid: 700},
				{Timestamp: base + 20, Pgrid: 800},
			},
			want: true,
		},
		"2 consecutive not enough": {
			readings: []dynamo.ReadingItem{
				{Timestamp: base, Pgrid: 600},
				{Timestamp: base + 10, Pgrid: 700},
			},
			want: false,
		},
		"gap over 30s breaks chain": {
			readings: []dynamo.ReadingItem{
				{Timestamp: base, Pgrid: 600},
				{Timestamp: base + 10, Pgrid: 700},
				{Timestamp: base + 41, Pgrid: 800},
				{Timestamp: base + 51, Pgrid: 900},
			},
			want: false,
		},
		"below threshold interspersed": {
			readings: []dynamo.ReadingItem{
				{Timestamp: base, Pgrid: 600},
				{Timestamp: base + 10, Pgrid: 700},
				{Timestamp: base + 20, Pgrid: 400},
				{Timestamp: base + 30, Pgrid: 800},
			},
			want: false,
		},
		"exactly 500 not sustained": {
			readings: []dynamo.ReadingItem{
				{Timestamp: base, Pgrid: 500},
				{Timestamp: base + 10, Pgrid: 500},
				{Timestamp: base + 20, Pgrid: 500},
			},
			want: false,
		},
		"sustained in middle but not at end": {
			readings: []dynamo.ReadingItem{
				{Timestamp: base, Pgrid: 600},
				{Timestamp: base + 10, Pgrid: 700},
				{Timestamp: base + 20, Pgrid: 800},
				{Timestamp: base + 30, Pgrid: 100},
			},
			want: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := computePgridSustained(tc.readings)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDownsample(t *testing.T) {
	date := "2026-04-15"

	reading := func(h, m int, ppv, pload, pbat, pgrid, soc float64) dynamo.ReadingItem {
		loc, _ := time.LoadLocation("Australia/Sydney")
		ts := time.Date(2026, 4, 15, h, m, 0, 0, loc)
		return dynamo.ReadingItem{
			Timestamp: ts.Unix(),
			Ppv:       ppv,
			Pload:     pload,
			Pbat:      pbat,
			Pgrid:     pgrid,
			Soc:       soc,
		}
	}

	tests := map[string]struct {
		readings []dynamo.ReadingItem
		wantLen  int
		checkFn  func(t *testing.T, points []TimeSeriesPoint)
	}{
		"empty input": {
			readings: nil,
			wantLen:  0,
		},
		"single reading": {
			readings: []dynamo.ReadingItem{
				reading(10, 2, 1000, 500, 200, 100, 80),
			},
			wantLen: 1,
		},
		"two readings in same bucket averaged": {
			readings: []dynamo.ReadingItem{
				reading(10, 1, 1000, 500, 200, 100, 80),
				reading(10, 3, 2000, 700, 400, 300, 70),
			},
			wantLen: 1,
			checkFn: func(t *testing.T, points []TimeSeriesPoint) {
				assert.InDelta(t, 1500, points[0].Ppv, 0.001)
				assert.InDelta(t, 600, points[0].Pload, 0.001)
				assert.InDelta(t, 300, points[0].Pbat, 0.001)
				assert.InDelta(t, 200, points[0].Pgrid, 0.001)
				assert.InDelta(t, 75, points[0].Soc, 0.001)
			},
		},
		"readings in different buckets": {
			readings: []dynamo.ReadingItem{
				reading(10, 1, 1000, 500, 200, 100, 80),
				reading(10, 6, 2000, 700, 400, 300, 70),
			},
			wantLen: 2,
		},
		"sorted ascending": {
			readings: []dynamo.ReadingItem{
				reading(14, 0, 500, 500, 200, 100, 60),
				reading(10, 0, 1000, 500, 200, 100, 80),
			},
			wantLen: 2,
			checkFn: func(t *testing.T, points []TimeSeriesPoint) {
				assert.InDelta(t, 1000, points[0].Ppv, 0.001)
				assert.InDelta(t, 500, points[1].Ppv, 0.001)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := downsample(tc.readings, date)
			assert.Len(t, got, tc.wantLen)
			if tc.checkFn != nil {
				tc.checkFn(t, got)
			}
		})
	}
}

func TestRoundEnergy(t *testing.T) {
	tests := map[string]struct {
		input float64
		want  float64
	}{
		"two decimal places": {input: 5.936, want: 5.94},
		"rounds down":        {input: 1.234, want: 1.23},
		"rounds up":          {input: 1.235, want: 1.24},
		"already two places": {input: 3.14, want: 3.14},
		"zero":               {input: 0, want: 0},
		"negative":           {input: -1.236, want: -1.24},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := derivedstats.RoundEnergy(tc.input)
			assert.InDelta(t, tc.want, got, 1e-9)
		})
	}
}

func TestRoundPower(t *testing.T) {
	tests := map[string]struct {
		input float64
		want  float64
	}{
		"one decimal place": {input: 207.06, want: 207.1},
		"rounds down":       {input: 41.24, want: 41.2},
		"rounds up":         {input: 41.25, want: 41.3},
		"already one place": {input: 50.0, want: 50.0},
		"zero":              {input: 0, want: 0},
		"negative":          {input: -3.15, want: -3.2},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := roundPower(tc.input)
			assert.InDelta(t, tc.want, got, 1e-9)
		})
	}
}

func BenchmarkDownsample(b *testing.B) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	dayStart := time.Date(2026, 4, 10, 0, 0, 0, 0, loc)
	readings := make([]dynamo.ReadingItem, 0, 8640)
	for i := range 8640 {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: dayStart.Unix() + int64(i*10),
			Ppv:       float64(i % 500),
			Pload:     float64(i % 350),
			Pbat:      float64(i % 200),
			Pgrid:     float64(i % 150),
			Soc:       20 + float64(i%80),
		})
	}

	for b.Loop() {
		_ = downsample(readings, "2026-04-10")
	}
}

func BenchmarkComputePgridSustained(b *testing.B) {
	readings := make([]dynamo.ReadingItem, 0, 360)
	base := int64(1000)
	for i := range 360 {
		pgrid := 100.0
		if i > 350 {
			pgrid = 600
		}
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: base + int64(i*10),
			Pgrid:     pgrid,
		})
	}

	for b.Loop() {
		_ = computePgridSustained(readings)
	}
}

// timePtr returns a pointer to the given time.
func timePtr(t time.Time) *time.Time {
	return &t
}

// Verify roundEnergy and roundPower use the correct multipliers.
func TestRoundingMultipliers(t *testing.T) {
	assert.InDelta(t, 0.01, 1.0/math.Round(1.0/derivedstats.RoundEnergy(0.01)), 1e-9)
	assert.InDelta(t, 0.1, 1.0/math.Round(1.0/roundPower(0.1)), 1e-9)
}

func TestComputeTodayEnergy(t *testing.T) {
	midnight := int64(1713139200)

	tests := map[string]struct {
		readings     []dynamo.ReadingItem
		midnightUnix int64
		want         *TodayEnergy
	}{
		"empty readings returns nil": {
			readings:     nil,
			midnightUnix: midnight,
			want:         nil,
		},
		"single reading returns nil": {
			readings: []dynamo.ReadingItem{
				{Timestamp: midnight + 100, Ppv: 1000, Pgrid: 500, Pbat: 200},
			},
			midnightUnix: midnight,
			want:         nil,
		},
		"two readings after midnight computes correct energy": {
			readings: []dynamo.ReadingItem{
				{Timestamp: midnight + 10, Ppv: 1000, Pgrid: 500, Pbat: -300},
				{Timestamp: midnight + 20, Ppv: 1000, Pgrid: 500, Pbat: -300},
			},
			midnightUnix: midnight,
			want: &TodayEnergy{
				Epv:        derivedstats.RoundEnergy(1000.0 * 10.0 / 3600.0 / 1000.0),
				EInput:     derivedstats.RoundEnergy(500.0 * 10.0 / 3600.0 / 1000.0),
				EOutput:    0,
				ECharge:    derivedstats.RoundEnergy(300.0 * 10.0 / 3600.0 / 1000.0),
				EDischarge: 0,
			},
		},
		"readings spanning midnight only counts post-midnight pairs": {
			readings: []dynamo.ReadingItem{
				{Timestamp: midnight - 20, Ppv: 9999, Pgrid: 9999, Pbat: 9999},
				{Timestamp: midnight - 10, Ppv: 9999, Pgrid: 9999, Pbat: 9999},
				{Timestamp: midnight + 10, Ppv: 2000, Pgrid: 1000, Pbat: 500},
				{Timestamp: midnight + 20, Ppv: 2000, Pgrid: 1000, Pbat: 500},
			},
			midnightUnix: midnight,
			want: &TodayEnergy{
				Epv:        derivedstats.RoundEnergy(2000.0 * 10.0 / 3600.0 / 1000.0),
				EInput:     derivedstats.RoundEnergy(1000.0 * 10.0 / 3600.0 / 1000.0),
				EOutput:    0,
				ECharge:    0,
				EDischarge: derivedstats.RoundEnergy(500.0 * 10.0 / 3600.0 / 1000.0),
			},
		},
		"gap over 60s between readings skips that pair": {
			readings: []dynamo.ReadingItem{
				{Timestamp: midnight + 10, Ppv: 1000, Pgrid: 500, Pbat: 200},
				{Timestamp: midnight + 20, Ppv: 1000, Pgrid: 500, Pbat: 200},
				{Timestamp: midnight + 81, Ppv: 3000, Pgrid: 1500, Pbat: 600},
				{Timestamp: midnight + 91, Ppv: 3000, Pgrid: 1500, Pbat: 600},
			},
			midnightUnix: midnight,
			want: &TodayEnergy{
				Epv:        derivedstats.RoundEnergy((1000.0*10.0/3600.0 + 3000.0*10.0/3600.0) / 1000.0),
				EInput:     derivedstats.RoundEnergy((500.0*10.0/3600.0 + 1500.0*10.0/3600.0) / 1000.0),
				EOutput:    0,
				ECharge:    0,
				EDischarge: derivedstats.RoundEnergy((200.0*10.0/3600.0 + 600.0*10.0/3600.0) / 1000.0),
			},
		},
		"mixed sign pgrid and pbat maps to correct fields": {
			readings: []dynamo.ReadingItem{
				{Timestamp: midnight + 100, Ppv: 500, Pgrid: -800, Pbat: -400},
				{Timestamp: midnight + 110, Ppv: 500, Pgrid: -800, Pbat: -400},
			},
			midnightUnix: midnight,
			want: &TodayEnergy{
				Epv:        derivedstats.RoundEnergy(500.0 * 10.0 / 3600.0 / 1000.0),
				EInput:     0,
				EOutput:    derivedstats.RoundEnergy(800.0 * 10.0 / 3600.0 / 1000.0),
				ECharge:    derivedstats.RoundEnergy(400.0 * 10.0 / 3600.0 / 1000.0),
				EDischarge: 0,
			},
		},
		"rounding matches roundEnergy output": {
			readings: []dynamo.ReadingItem{
				{Timestamp: midnight + 10, Ppv: 3600, Pgrid: 1800, Pbat: 900},
				{Timestamp: midnight + 20, Ppv: 3600, Pgrid: 1800, Pbat: 900},
			},
			midnightUnix: midnight,
			want: &TodayEnergy{
				Epv:        derivedstats.RoundEnergy(3600.0 * 10.0 / 3600.0 / 1000.0),
				EInput:     derivedstats.RoundEnergy(1800.0 * 10.0 / 3600.0 / 1000.0),
				EOutput:    0,
				ECharge:    0,
				EDischarge: derivedstats.RoundEnergy(900.0 * 10.0 / 3600.0 / 1000.0),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := computeTodayEnergy(tc.readings, tc.midnightUnix)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.NotNil(t, got)
			assert.InDelta(t, tc.want.Epv, got.Epv, 1e-9)
			assert.InDelta(t, tc.want.EInput, got.EInput, 1e-9)
			assert.InDelta(t, tc.want.EOutput, got.EOutput, 1e-9)
			assert.InDelta(t, tc.want.ECharge, got.ECharge, 1e-9)
			assert.InDelta(t, tc.want.EDischarge, got.EDischarge, 1e-9)
		})
	}
}

func TestWithinOffpeakWindow(t *testing.T) {
	syd := func(h, m int) time.Time {
		return time.Date(2026, 4, 15, h, m, 0, 0, sydneyTZ)
	}

	tests := map[string]struct {
		now          time.Time
		offpeakStart string
		offpeakEnd   string
		want         bool
	}{
		"before window": {
			now: syd(10, 59), offpeakStart: "11:00", offpeakEnd: "14:00",
			want: false,
		},
		"at start": {
			now: syd(11, 0), offpeakStart: "11:00", offpeakEnd: "14:00",
			want: true,
		},
		"mid-window": {
			now: syd(12, 30), offpeakStart: "11:00", offpeakEnd: "14:00",
			want: true,
		},
		"at end (exclusive)": {
			now: syd(14, 0), offpeakStart: "11:00", offpeakEnd: "14:00",
			want: false,
		},
		"after window": {
			now: syd(14, 30), offpeakStart: "11:00", offpeakEnd: "14:00",
			want: false,
		},
		"unparseable strings": {
			now: syd(12, 0), offpeakStart: "x", offpeakEnd: "y",
			want: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := withinOffpeakWindow(tc.now, tc.offpeakStart, tc.offpeakEnd)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestComputeCantEmptyBeforeOffpeak(t *testing.T) {
	now := time.Date(2026, 4, 15, 10, 0, 0, 0, sydneyTZ)

	// Boundary equality: requiredHours = (Soc - cutoffPercent)/100 * CapacityKwh / maxDischargeKW
	// Pick FP-exact inputs so requiredHours == 1h without relying on IEEE 754 rounding:
	//   (55 - 5)/100 * 10.0 / 5.0 = 0.5 * 10.0 / 5.0 = 1.0 exactly.
	const boundarySoc, boundaryCapacityKwh = 55.0, 10.0

	tests := map[string]struct {
		in   cantEmptyInput
		want *bool
	}{
		"soc just above cutoff, short window": {
			// Soc 6, cap 13.34, cutoff 5 → remaining 0.1334 kWh.
			// At 5 kW the battery drains in ~1.6 min; a 1-minute window
			// therefore opens before drain completes → flag &true.
			in: cantEmptyInput{
				Soc: 6, CapacityKwh: 13.34,
				Now: now, NextOpStart: now.Add(1 * time.Minute),
				HasBoundary: true, WithinOffpeakWindow: false,
			},
			want: boolPtr(true),
		},
		"soc just above cutoff, long window": {
			in: cantEmptyInput{
				Soc: 6, CapacityKwh: 13.34,
				Now: now, NextOpStart: now.Add(24 * time.Hour),
				HasBoundary: true, WithinOffpeakWindow: false,
			},
			want: nil,
		},
		"soc well above cutoff, short window": {
			in: cantEmptyInput{
				Soc: 90, CapacityKwh: 13.34,
				Now: now, NextOpStart: now.Add(30 * time.Minute),
				HasBoundary: true, WithinOffpeakWindow: false,
			},
			want: boolPtr(true),
		},
		"soc well above cutoff, long window": {
			in: cantEmptyInput{
				Soc: 90, CapacityKwh: 13.34,
				Now: now, NextOpStart: now.Add(48 * time.Hour),
				HasBoundary: true, WithinOffpeakWindow: false,
			},
			want: nil,
		},
		"soc exactly at cutoff": {
			in: cantEmptyInput{
				Soc: 5, CapacityKwh: 13.34,
				Now: now, NextOpStart: now.Add(1 * time.Hour),
				HasBoundary: true, WithinOffpeakWindow: false,
			},
			want: nil,
		},
		"soc below cutoff": {
			in: cantEmptyInput{
				Soc: 3, CapacityKwh: 13.34,
				Now: now, NextOpStart: now.Add(1 * time.Hour),
				HasBoundary: true, WithinOffpeakWindow: false,
			},
			want: nil,
		},
		"window currently active": {
			in: cantEmptyInput{
				Soc: 80, CapacityKwh: 13.34,
				Now: now, NextOpStart: now.Add(1 * time.Hour),
				HasBoundary: true, WithinOffpeakWindow: true,
			},
			want: nil,
		},
		"no boundary": {
			in: cantEmptyInput{
				Soc: 80, CapacityKwh: 13.34,
				Now: now, NextOpStart: time.Time{},
				HasBoundary: false, WithinOffpeakWindow: false,
			},
			want: nil,
		},
		"zero capacity": {
			in: cantEmptyInput{
				Soc: 80, CapacityKwh: 0,
				Now: now, NextOpStart: now.Add(1 * time.Hour),
				HasBoundary: true, WithinOffpeakWindow: false,
			},
			want: nil,
		},
		"boundary equality": {
			in: cantEmptyInput{
				Soc: boundarySoc, CapacityKwh: boundaryCapacityKwh,
				Now: now, NextOpStart: now.Add(1 * time.Hour),
				HasBoundary: true, WithinOffpeakWindow: false,
			},
			want: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := computeCantEmptyBeforeOffpeak(tc.in)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
		})
	}
}

// boolPtr returns a pointer to the given bool.
func boolPtr(b bool) *bool {
	return &b
}

func TestReconcileEnergy(t *testing.T) {
	tests := map[string]struct {
		computed *TodayEnergy
		stored   *TodayEnergy
		want     *TodayEnergy
	}{
		"both nil returns nil": {
			computed: nil,
			stored:   nil,
			want:     nil,
		},
		"only computed returns computed": {
			computed: &TodayEnergy{Epv: 1.5, EInput: 0.8, EOutput: 0.3, ECharge: 0.5, EDischarge: 0.2},
			stored:   nil,
			want:     &TodayEnergy{Epv: 1.5, EInput: 0.8, EOutput: 0.3, ECharge: 0.5, EDischarge: 0.2},
		},
		"only stored returns stored": {
			computed: nil,
			stored:   &TodayEnergy{Epv: 2.0, EInput: 1.0, EOutput: 0.5, ECharge: 0.7, EDischarge: 0.3},
			want:     &TodayEnergy{Epv: 2.0, EInput: 1.0, EOutput: 0.5, ECharge: 0.7, EDischarge: 0.3},
		},
		"both present returns per-field max": {
			computed: &TodayEnergy{Epv: 3.0, EInput: 1.0, EOutput: 2.0, ECharge: 0.5, EDischarge: 1.5},
			stored:   &TodayEnergy{Epv: 2.5, EInput: 1.5, EOutput: 1.0, ECharge: 1.0, EDischarge: 0.5},
			want:     &TodayEnergy{Epv: 3.0, EInput: 1.5, EOutput: 2.0, ECharge: 1.0, EDischarge: 1.5},
		},
		"mixed values where some fields higher in computed and some in stored": {
			computed: &TodayEnergy{Epv: 0.1, EInput: 5.0, EOutput: 0.0, ECharge: 3.0, EDischarge: 0.0},
			stored:   &TodayEnergy{Epv: 4.0, EInput: 0.0, EOutput: 2.5, ECharge: 0.0, EDischarge: 7.0},
			want:     &TodayEnergy{Epv: 4.0, EInput: 5.0, EOutput: 2.5, ECharge: 3.0, EDischarge: 7.0},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := reconcileEnergy(tc.computed, tc.stored)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.NotNil(t, got)
			assert.InDelta(t, tc.want.Epv, got.Epv, 1e-9)
			assert.InDelta(t, tc.want.EInput, got.EInput, 1e-9)
			assert.InDelta(t, tc.want.EOutput, got.EOutput, 1e-9)
			assert.InDelta(t, tc.want.ECharge, got.ECharge, 1e-9)
			assert.InDelta(t, tc.want.EDischarge, got.EDischarge, 1e-9)
		})
	}
}

func TestProjectOffpeakEndSoc(t *testing.T) {
	// Window 11:00-14:00 in Sydney local time; capacity 13.34 kWh. These are
	// the design.md Testing Strategy fixtures, which double as the deferred
	// worked example from decision_log Decision 7.
	const (
		start = "11:00"
		end   = "14:00"
		cap   = 13.34
	)
	at := func(h, m int) time.Time {
		return time.Date(2026, 4, 15, h, m, 0, 0, sydneyTZ)
	}

	tests := map[string]struct {
		now  time.Time
		soc  float64
		cap  float64
		want *float64
	}{
		"crosses 95 boundary": {
			now: at(12, 0), soc: 50, cap: cap, want: floatPtr(97.5),
		},
		"fast rate only never reaches 95": {
			now: at(13, 30), soc: 40, cap: cap, want: floatPtr(56.9),
		},
		"already in trickle band clamps to 100": {
			now: at(13, 0), soc: 97, cap: cap, want: floatPtr(100.0),
		},
		"crosses 95 mid window": {
			now: at(13, 0), soc: 90, cap: cap, want: floatPtr(98.2),
		},
		"already full": {
			now: at(12, 0), soc: 100, cap: cap, want: floatPtr(100.0),
		},
		"before window returns nil": {
			now: at(10, 0), soc: 50, cap: cap, want: nil,
		},
		"after window returns nil": {
			now: at(14, 30), soc: 50, cap: cap, want: nil,
		},
		"zero capacity returns nil": {
			now: at(12, 0), soc: 50, cap: 0, want: nil,
		},
		"negative capacity returns nil": {
			now: at(12, 0), soc: 50, cap: -5, want: nil,
		},
		// 95% tie-break: SoC exactly 95 charges at the 500 W trickle rate. Over
		// the 2h from 12:00 the trickle gain is 0.5/13.34*100*2 = 7.5, so
		// 95 -> 102.5, clamped to 100.
		"soc exactly 95 uses trickle rate": {
			now: at(12, 0), soc: 95, cap: cap, want: floatPtr(100.0),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := projectOffpeakEndSoc(tc.soc, tc.cap, tc.now, start, end)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.InDelta(t, *tc.want, *got, 1e-9)
		})
	}
}

// TestPropertyProjectOffpeakEndSoc exercises the closed-form curve's
// invariants over random inputs: the result is always within [soc, 100], and
// it is monotonic non-decreasing in both the hours remaining and the starting
// SoC. testing/quick generates inputs independently, so each monotonicity
// property constructs a second input that differs from the generated base in
// exactly one dimension.
func TestPropertyProjectOffpeakEndSoc(t *testing.T) {
	const (
		start = "11:00"
		end   = "14:00"
	)
	// nowFor maps a fraction f in [0,1] onto an instant inside the window
	// [11:00, 14:00). f=0 is 11:00 (3h remaining); f→1 approaches 14:00
	// (0h remaining). A larger f means fewer hours remaining.
	nowFor := func(f float64) time.Time {
		f = math.Mod(math.Abs(f), 1)
		// Cap below 1 so we never land exactly on (or past) the window end,
		// where the minute-of-day gate flips to false.
		secs := int(f * (3*3600 - 1))
		return time.Date(2026, 4, 15, 11, 0, 0, 0, sydneyTZ).Add(time.Duration(secs) * time.Second)
	}
	// normSoc maps an arbitrary float onto [0, 100].
	normSoc := func(x float64) float64 {
		return math.Mod(math.Abs(x), 100.0000001)
	}
	// normCap maps an arbitrary float onto a positive capacity in (0, ~100].
	normCap := func(x float64) float64 {
		return math.Mod(math.Abs(x), 100) + 1
	}

	t.Run("result within [soc, 100]", func(t *testing.T) {
		f := func(socRaw, capRaw, fRaw float64) bool {
			soc := normSoc(socRaw)
			capKwh := normCap(capRaw)
			got := projectOffpeakEndSoc(soc, capKwh, nowFor(fRaw), start, end)
			if got == nil {
				return false // always in-window with positive capacity
			}
			return *got >= soc-1e-9 && *got <= 100+1e-9
		}
		require.NoError(t, quick.Check(f, nil))
	})

	t.Run("monotonic non-decreasing in hours remaining", func(t *testing.T) {
		f := func(socRaw, capRaw, fRaw float64) bool {
			soc := normSoc(socRaw)
			capKwh := normCap(capRaw)
			f0 := math.Mod(math.Abs(fRaw), 1)
			// Earlier now (more hours remaining) vs the same instant moved
			// later (fewer hours). More hours must not yield a lower SoC.
			fLate := f0 + (1-f0)/2 // strictly later than f0, still < 1
			early := projectOffpeakEndSoc(soc, capKwh, nowFor(f0), start, end)
			late := projectOffpeakEndSoc(soc, capKwh, nowFor(fLate), start, end)
			if early == nil || late == nil {
				return false
			}
			return *early >= *late-1e-9
		}
		require.NoError(t, quick.Check(f, nil))
	})

	t.Run("monotonic non-decreasing in soc", func(t *testing.T) {
		f := func(socRaw, capRaw, fRaw float64) bool {
			capKwh := normCap(capRaw)
			now := nowFor(fRaw)
			lowSoc := math.Mod(math.Abs(socRaw), 100)
			// Same now and capacity; a higher starting SoC must not project a
			// lower end SoC.
			highSoc := lowSoc + (100-lowSoc)/2 // strictly greater, <= 100
			low := projectOffpeakEndSoc(lowSoc, capKwh, now, start, end)
			high := projectOffpeakEndSoc(highSoc, capKwh, now, start, end)
			if low == nil || high == nil {
				return false
			}
			return *high >= *low-1e-9
		}
		require.NoError(t, quick.Check(f, nil))
	})
}
