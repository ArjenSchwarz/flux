package api

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestOffpeakDeltas(t *testing.T) {
	tests := map[string]struct {
		op       dynamo.OffpeakItem
		today    *TodayEnergy
		wantOK   bool
		wantVals offpeakDeltaValues
	}{
		"complete: pass through final deltas regardless of today": {
			op: dynamo.OffpeakItem{
				Status:              dynamo.OffpeakStatusComplete,
				GridUsageKwh:        2.5,
				SolarKwh:            5.0,
				BatteryChargeKwh:    1.0,
				BatteryDischargeKwh: 0.5,
				GridExportKwh:       0.3,
			},
			today:  nil,
			wantOK: true,
			wantVals: offpeakDeltaValues{
				GridImport: 2.5, Solar: 5.0, BatteryCharge: 1.0,
				BatteryDischarge: 0.5, GridExport: 0.3,
			},
		},
		"pending: project deltas from running totals": {
			op: dynamo.OffpeakItem{
				Status:          dynamo.OffpeakStatusPending,
				StartEpv:        10.0,
				StartEInput:     2.0,
				StartEOutput:    0.5,
				StartECharge:    1.0,
				StartEDischarge: 3.0,
			},
			today: &TodayEnergy{
				Epv: 12.5, EInput: 3.5, EOutput: 0.6,
				ECharge: 1.8, EDischarge: 3.4,
			},
			wantOK: true,
			wantVals: offpeakDeltaValues{
				GridImport: 1.5, Solar: 2.5, BatteryCharge: 0.8,
				BatteryDischarge: 0.4, GridExport: 0.1,
			},
		},
		"pending without today: not computable": {
			op: dynamo.OffpeakItem{
				Status: dynamo.OffpeakStatusPending, StartEInput: 2.0,
			},
			today:  nil,
			wantOK: false,
		},
		"pending with snapshot lag: clamp negatives to zero": {
			// A poller reconciliation can briefly reduce running totals
			// below the start snapshot. The clamp keeps "-0.1 kWh
			// imported" off the dashboard.
			op: dynamo.OffpeakItem{
				Status:          dynamo.OffpeakStatusPending,
				StartEpv:        10.0,
				StartEInput:     5.0,
				StartEOutput:    1.0,
				StartECharge:    2.0,
				StartEDischarge: 3.0,
			},
			today: &TodayEnergy{
				Epv: 9.5, EInput: 4.8, EOutput: 0.9,
				ECharge: 1.7, EDischarge: 2.9,
			},
			wantOK:   true,
			wantVals: offpeakDeltaValues{},
		},
		"unknown status: not computable": {
			op:     dynamo.OffpeakItem{Status: "future-status"},
			today:  &TodayEnergy{Epv: 10},
			wantOK: false,
		},
		"empty status: not computable": {
			op:     dynamo.OffpeakItem{},
			today:  nil,
			wantOK: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := offpeakDeltas(tc.op, tc.today)
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
			// remaining = (50-10)/100 * 10 = 4 kWh, rate = 1 kW, hours = 4
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
			// remaining = (80-10)/100 * 13.34 = 9.338 kWh
			// rate = 2000W = 2 kW, hours = 9.338/2 = 4.669
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
				// Allow 1ms tolerance for floating point duration conversion.
				assert.WithinDuration(t, *tc.want, *got, time.Millisecond)
			}
		})
	}
}

func TestNextOffpeakStart(t *testing.T) {
	// Off-peak window: 11:00 - 14:00 Sydney.
	const opStart = "11:00"
	const opEnd = "14:00"

	// syd builds a Sydney-local time at the given hour/minute on 2026-04-15.
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
	base := int64(1713168000) // arbitrary fixed timestamp

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
				// 31s gap breaks consecutiveness
				{Timestamp: base + 41, Pgrid: 800},
				{Timestamp: base + 51, Pgrid: 900},
			},
			want: false,
		},
		"below threshold interspersed": {
			readings: []dynamo.ReadingItem{
				{Timestamp: base, Pgrid: 600},
				{Timestamp: base + 10, Pgrid: 700},
				{Timestamp: base + 20, Pgrid: 400}, // below threshold
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
			want: false, // threshold is > 500, not >= 500
		},
		"sustained in middle but not at end": {
			// Only evaluates the current run from the end.
			readings: []dynamo.ReadingItem{
				{Timestamp: base, Pgrid: 600},
				{Timestamp: base + 10, Pgrid: 700},
				{Timestamp: base + 20, Pgrid: 800},
				{Timestamp: base + 30, Pgrid: 100}, // breaks at end
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
	// Use a fixed date for bucket calculation.
	date := "2026-04-15"

	// Helper to create a reading at a specific hour:minute.
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
		// Optional: check specific bucket values.
		checkFn func(t *testing.T, points []TimeSeriesPoint)
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
				reading(10, 6, 2000, 700, 400, 300, 70), // next bucket
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
				// First point should be the earlier one.
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

func TestFindMinSOC(t *testing.T) {
	tests := map[string]struct {
		readings  []dynamo.ReadingItem
		wantSoc   float64
		wantTS    int64
		wantFound bool
	}{
		"empty input": {
			readings:  nil,
			wantFound: false,
		},
		"single reading": {
			readings: []dynamo.ReadingItem{
				{Soc: 50, Timestamp: 1000},
			},
			wantSoc: 50, wantTS: 1000, wantFound: true,
		},
		"multiple readings": {
			readings: []dynamo.ReadingItem{
				{Soc: 80, Timestamp: 1000},
				{Soc: 30, Timestamp: 2000},
				{Soc: 60, Timestamp: 3000},
			},
			wantSoc: 30, wantTS: 2000, wantFound: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			soc, ts, found := findMinSOC(tc.readings)
			assert.Equal(t, tc.wantFound, found)
			if found {
				assert.InDelta(t, tc.wantSoc, soc, 0.001)
				assert.Equal(t, tc.wantTS, ts)
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
			got := roundEnergy(tc.input)
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
	// roundEnergy: 2 decimal places → multiplier 100
	assert.InDelta(t, 0.01, 1.0/math.Round(1.0/roundEnergy(0.01)), 1e-9)
	// roundPower: 1 decimal place → multiplier 10
	assert.InDelta(t, 0.1, 1.0/math.Round(1.0/roundPower(0.1)), 1e-9)
}

func TestComputeTodayEnergy(t *testing.T) {
	midnight := int64(1713139200) // arbitrary midnight boundary

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
			// dt = 10s, trapezoid average = same values
			// epv: (1000+1000)/2 * 10/3600 = 2.7778 Wh = 0.0028 kWh → roundEnergy → 0.0
			// eInput: max(500,0)=500, (500+500)/2 * 10/3600 = 1.3889 Wh = 0.0014 kWh → 0.0
			// eOutput: max(-500,0)=0 → 0
			// eCharge: max(-(-300),0)=max(300,0)=300, (300+300)/2 * 10/3600 = 0.8333 Wh = 0.0008 kWh → 0.0
			// eDischarge: max(-300,0)=0 → 0
			want: &TodayEnergy{
				Epv:        roundEnergy(1000.0 * 10.0 / 3600.0 / 1000.0),
				EInput:     roundEnergy(500.0 * 10.0 / 3600.0 / 1000.0),
				EOutput:    0,
				ECharge:    roundEnergy(300.0 * 10.0 / 3600.0 / 1000.0),
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
				Epv:        roundEnergy(2000.0 * 10.0 / 3600.0 / 1000.0),
				EInput:     roundEnergy(1000.0 * 10.0 / 3600.0 / 1000.0),
				EOutput:    0,
				ECharge:    0,
				EDischarge: roundEnergy(500.0 * 10.0 / 3600.0 / 1000.0),
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
			// Only pairs (10,20) and (81,91) count; pair (20,81) has 61s gap → skipped
			want: &TodayEnergy{
				Epv:        roundEnergy((1000.0*10.0/3600.0 + 3000.0*10.0/3600.0) / 1000.0),
				EInput:     roundEnergy((500.0*10.0/3600.0 + 1500.0*10.0/3600.0) / 1000.0),
				EOutput:    0,
				ECharge:    0,
				EDischarge: roundEnergy((200.0*10.0/3600.0 + 600.0*10.0/3600.0) / 1000.0),
			},
		},
		"mixed sign pgrid and pbat maps to correct fields": {
			readings: []dynamo.ReadingItem{
				{Timestamp: midnight + 100, Ppv: 500, Pgrid: -800, Pbat: -400},
				{Timestamp: midnight + 110, Ppv: 500, Pgrid: -800, Pbat: -400},
			},
			midnightUnix: midnight,
			// pgrid=-800: eInput=max(-800,0)=0, eOutput=max(800,0)=800
			// pbat=-400: eDischarge=max(-400,0)=0, eCharge=max(400,0)=400
			want: &TodayEnergy{
				Epv:        roundEnergy(500.0 * 10.0 / 3600.0 / 1000.0),
				EInput:     0,
				EOutput:    roundEnergy(800.0 * 10.0 / 3600.0 / 1000.0),
				ECharge:    roundEnergy(400.0 * 10.0 / 3600.0 / 1000.0),
				EDischarge: 0,
			},
		},
		"rounding matches roundEnergy output": {
			readings: []dynamo.ReadingItem{
				{Timestamp: midnight + 10, Ppv: 3600, Pgrid: 1800, Pbat: 900},
				{Timestamp: midnight + 20, Ppv: 3600, Pgrid: 1800, Pbat: 900},
			},
			midnightUnix: midnight,
			// dt = 10s, constant power
			// epv: 3600 * 10 / 3600 / 1000 = 0.01
			// eInput: 1800 * 10 / 3600 / 1000 = 0.005 → roundEnergy → 0.01
			// eDischarge: 900 * 10 / 3600 / 1000 = 0.0025 → roundEnergy → 0.0
			want: &TodayEnergy{
				Epv:        roundEnergy(3600.0 * 10.0 / 3600.0 / 1000.0),
				EInput:     roundEnergy(1800.0 * 10.0 / 3600.0 / 1000.0),
				EOutput:    0,
				ECharge:    0,
				EDischarge: roundEnergy(900.0 * 10.0 / 3600.0 / 1000.0),
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

// sydneyReading creates a ReadingItem at the given Sydney local time with the specified Pload.
// Other fields default to zero.
func sydneyReading(hour, minute, second int, pload float64) dynamo.ReadingItem {
	t := time.Date(2026, 4, 15, hour, minute, second, 0, sydneyTZ)
	return dynamo.ReadingItem{Timestamp: t.Unix(), Pload: pload}
}

// sydneyReadings creates a sequence of readings at 10-second intervals starting
// at the given Sydney local time, one per Pload value.
func sydneyReadings(hour, minute, second int, ploads ...float64) []dynamo.ReadingItem {
	start := time.Date(2026, 4, 15, hour, minute, second, 0, sydneyTZ)
	out := make([]dynamo.ReadingItem, len(ploads))
	for i, p := range ploads {
		out[i] = dynamo.ReadingItem{
			Timestamp: start.Add(time.Duration(i*10) * time.Second).Unix(),
			Pload:     p,
		}
	}
	return out
}

func TestFindPeakPeriods(t *testing.T) {
	// Standard off-peak window: 11:00 - 14:00
	const opStart = "11:00"
	const opEnd = "14:00"

	tests := map[string]struct {
		readings     []dynamo.ReadingItem
		offpeakStart string
		offpeakEnd   string
		wantCount    int
		check        func(t *testing.T, got []PeakPeriod)
	}{
		"empty readings": {
			readings:     nil,
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"all readings in off-peak": {
			// All readings between 11:00 and 14:00
			readings:     sydneyReadings(12, 0, 0, 5000, 6000, 7000, 8000, 9000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"uniform load": {
			// All readings have the same Pload = mean, so none are strictly above threshold
			readings:     sydneyReadings(8, 0, 0, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"single peak above mean": {
			// Mean of non-off-peak = (100*6 + 5000*13) / 19 ≈ 3441
			// The 5000W readings at 08:01:00 through 08:03:00 form a cluster > 2 min
			readings: append(
				sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100),
				sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...,
			),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				assert.True(t, got[0].EnergyWh > 0)
				assert.True(t, got[0].AvgLoadW > 0)
			},
		},
		"two clusters within 5min merge": {
			// Two above-threshold bursts separated by < 5 min of below-threshold
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Low baseline: 08:00:00 - 08:00:50 (6 readings)
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// High burst 1: 08:01:00 - 08:03:00 (13 readings)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				// Brief dip: 08:03:10 - 08:03:40 (4 readings, below threshold)
				r = append(r, sydneyReadings(8, 3, 10, 100, 100, 100, 100)...)
				// High burst 2: 08:03:50 - 08:05:50 (13 readings)
				r = append(r, sydneyReadings(8, 3, 50, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1, // merged into one
		},
		"two clusters >5min separate": {
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Low baseline
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// High burst 1: 08:01:00 - 08:03:00
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				// Long gap of low readings: 08:03:10 - 08:09:00 (>5 min)
				for i := range 36 {
					ts := time.Date(2026, 4, 15, 8, 3, 10+i*10, 0, sydneyTZ)
					r = append(r, dynamo.ReadingItem{Timestamp: ts.Unix(), Pload: 100})
				}
				// High burst 2: 08:09:10 - 08:11:10
				r = append(r, sydneyReadings(8, 9, 10, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 2,
		},
		"period under 2min discarded": {
			// Short burst of 11 readings (100s) above threshold, below 2 min
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// 11 readings = 100s duration (< 120s)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"more than 3 returns top 3": {
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Low baseline
				r = append(r, sydneyReadings(6, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// 4 separate high bursts, each > 2 min, separated by > 5 min of low readings
				starts := []struct{ h, m int }{{7, 0}, {7, 15}, {7, 30}, {7, 45}}
				for i, s := range starts {
					// 13 readings = 120s at 10s intervals
					for j := range 13 {
						ts := time.Date(2026, 4, 15, s.h, s.m, j*10, 0, sydneyTZ)
						r = append(r, dynamo.ReadingItem{Timestamp: ts.Unix(), Pload: 5000 + float64(i*100)})
					}
					// Low readings to create > 5 min gap (fill until next burst)
					if i < len(starts)-1 {
						endSec := s.m*60 + 120 // burst ends 120s after start
						nextStartSec := starts[i+1].m * 60
						for sec := endSec + 10; sec < nextStartSec; sec += 10 {
							ts := time.Date(2026, 4, 15, s.h, 0, sec, 0, sydneyTZ)
							r = append(r, dynamo.ReadingItem{Timestamp: ts.Unix(), Pload: 100})
						}
					}
				}
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 3,
			check: func(t *testing.T, got []PeakPeriod) {
				// Descending energy order
				for i := 1; i < len(got); i++ {
					assert.True(t, got[i-1].EnergyWh >= got[i].EnergyWh, "periods should be in descending energy order")
				}
			},
		},
		"gap >60s skips energy pair": {
			// Readings with a 61s gap in the middle — that pair should be skipped in energy calc
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Low baseline
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// High readings with a gap
				r = append(r, sydneyReading(8, 1, 0, 5000))
				r = append(r, sydneyReading(8, 1, 10, 5000))
				r = append(r, sydneyReading(8, 1, 20, 5000))
				// 61s gap
				r = append(r, sydneyReading(8, 2, 21, 5000))
				r = append(r, sydneyReading(8, 2, 31, 5000))
				r = append(r, sydneyReading(8, 2, 41, 5000))
				r = append(r, sydneyReading(8, 2, 51, 5000))
				r = append(r, sydneyReading(8, 3, 1, 5000))
				r = append(r, sydneyReading(8, 3, 11, 5000))
				r = append(r, sydneyReading(8, 3, 21, 5000))
				r = append(r, sydneyReading(8, 3, 31, 5000))
				r = append(r, sydneyReading(8, 3, 41, 5000))
				r = append(r, sydneyReading(8, 3, 51, 5000))
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				// Energy should be less than if the gap pair were included
				// Full energy without gap: 5000 * 170s / 3600 ≈ 236 Wh
				// With gap skipped: less than that
				assert.True(t, got[0].EnergyWh > 0)
				assert.True(t, got[0].EnergyWh < 236)
			},
		},
		"off-peak boundary": {
			// Reading at exactly 11:00 is off-peak (>= start), reading at 14:00 is NOT off-peak (< end is false)
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Low baseline before off-peak
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// Reading at exactly 11:00 — should be excluded (off-peak)
				r = append(r, sydneyReading(11, 0, 0, 9000))
				// Readings at 14:00 and after — should be included (not off-peak)
				r = append(r, sydneyReadings(14, 0, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				// The period should start at 14:00, not 11:00
				assert.Contains(t, got[0].Start, "T04:00:00Z") // 14:00 AEST = 04:00 UTC
			},
		},
		"off-peak boundary clustering": {
			// Above-threshold readings at 10:59 and 14:01 must NOT cluster together
			// Off-peak readings between them break the cluster
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Low baseline
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// Burst ending at 10:59 (13 readings = 120s)
				r = append(r, sydneyReadings(10, 57, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				// Off-peak readings (these break the cluster)
				r = append(r, sydneyReadings(11, 0, 0, 5000, 5000, 5000)...)
				// Burst starting at 14:01 (13 readings = 120s)
				r = append(r, sydneyReadings(14, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 2, // must be separate
		},
		"transitive merge": {
			// Three clusters: A-B within 5min, B-C within 5min, but A-C > 5min
			// Should all merge transitively into one period
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Low baseline
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// Cluster A: 08:01:00 - 08:03:00 (13 readings)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				// Gap ~4 min of low readings
				for i := range 24 {
					ts := time.Date(2026, 4, 15, 8, 3, 10+i*10, 0, sydneyTZ)
					r = append(r, dynamo.ReadingItem{Timestamp: ts.Unix(), Pload: 100})
				}
				// Cluster B: 08:07:10 - 08:09:10 (13 readings)
				r = append(r, sydneyReadings(8, 7, 10, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				// Gap ~4 min of low readings
				for i := range 24 {
					ts := time.Date(2026, 4, 15, 8, 9, 20+i*10, 0, sydneyTZ)
					r = append(r, dynamo.ReadingItem{Timestamp: ts.Unix(), Pload: 100})
				}
				// Cluster C: 08:13:20 - 08:15:20 (13 readings)
				r = append(r, sydneyReadings(8, 13, 20, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1, // all three merge transitively
		},
		"zero-energy sparse period discarded": {
			// All reading pairs within the cluster have > 60s gaps, so energy = 0
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Low baseline
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// Sparse high readings: each 61s apart, spanning > 2 min
				r = append(r, sydneyReading(8, 1, 0, 5000))
				r = append(r, sydneyReading(8, 2, 1, 5000))  // 61s gap
				r = append(r, sydneyReading(8, 3, 2, 5000))  // 61s gap
				r = append(r, sydneyReading(8, 4, 3, 5000))  // 61s gap
				r = append(r, sydneyReading(8, 5, 4, 5000))  // 61s gap
				r = append(r, sydneyReading(8, 6, 5, 5000))  // 61s gap
				r = append(r, sydneyReading(8, 7, 6, 5000))  // 61s gap
				r = append(r, sydneyReading(8, 8, 7, 5000))  // 61s gap
				r = append(r, sydneyReading(8, 9, 8, 5000))  // 61s gap
				r = append(r, sydneyReading(8, 10, 9, 5000)) // 61s gap
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0, // zero energy → discarded
		},
		"invalid off-peak parse failure": {
			// Invalid off-peak strings → treat as no off-peak window
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: "invalid", offpeakEnd: "also-invalid",
			wantCount: 1,
		},
		"negative Pload clamped": {
			// Negative Pload values should be clamped to 0 in energy calculation
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Mix of negative and positive — mean computed from raw values,
				// but energy uses max(Pload, 0)
				r = append(r, sydneyReadings(8, 0, 0, -500, -500, -500, -500, -500, -500)...)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				assert.True(t, got[0].EnergyWh > 0)
			},
		},
		"single reading": {
			// A single reading cannot form a period: step 3 requires Pload > mean,
			// but the reading's own Pload equals the mean, so it is filtered out.
			readings:     []dynamo.ReadingItem{sydneyReading(8, 0, 0, 5000)},
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"DST transition day (AEDT→AEST)": {
			// Sydney DST ends first Sunday of April. In 2026 that's April 5:
			// 03:00 AEDT → 02:00 AEST. Verify off-peak filtering still works on
			// this day for a window well clear of the transition hour.
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				dst := func(h, m, s int, pload float64) dynamo.ReadingItem {
					ts := time.Date(2026, 4, 5, h, m, s, 0, sydneyTZ)
					return dynamo.ReadingItem{Timestamp: ts.Unix(), Pload: pload}
				}
				// Low baseline in the morning.
				for i := range 6 {
					r = append(r, dst(8, 0, i*10, 100))
				}
				// Readings inside the 11:00–14:00 off-peak window — must be ignored.
				for i := range 6 {
					r = append(r, dst(12, 0, i*10, 9000))
				}
				// Afternoon burst (13 readings = 120s) that should form one period.
				for i := range 13 {
					r = append(r, dst(15, 0, i*10, 5000))
				}
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				// Period must be at 15:00 Sydney (post-DST = AEST, UTC+10 = 05:00 UTC).
				assert.Contains(t, got[0].Start, "T05:00:00Z")
			},
		},
		"two periods with same rounded energy ranked by unrounded": {
			// Two periods that round to the same energy but differ in unrounded value
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				// Low baseline
				r = append(r, sydneyReadings(6, 0, 0, 100, 100, 100, 100, 100, 100)...)
				// Period 1: slightly higher energy (13 readings at 5001W)
				r = append(r, sydneyReadings(7, 0, 0, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001)...)
				// Gap > 5 min
				for i := range 36 {
					ts := time.Date(2026, 4, 15, 7, 2, 10+i*10, 0, sydneyTZ)
					r = append(r, dynamo.ReadingItem{Timestamp: ts.Unix(), Pload: 100})
				}
				// Period 2: slightly lower energy (13 readings at 4999W)
				r = append(r, sydneyReadings(7, 8, 10, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 2,
			check: func(t *testing.T, got []PeakPeriod) {
				// First period should have higher or equal energy
				assert.True(t, got[0].EnergyWh >= got[1].EnergyWh)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := findPeakPeriods(tc.readings, tc.offpeakStart, tc.offpeakEnd)
			assert.Len(t, got, tc.wantCount)
			if tc.check != nil && len(got) > 0 {
				tc.check(t, got)
			}
		})
	}
}

func TestFindPeakPeriodsProperties(t *testing.T) {
	type pbtInput struct {
		readings     []dynamo.ReadingItem
		offpeakStart string
		offpeakEnd   string
	}

	gen := rapid.Custom(func(t *rapid.T) pbtInput {
		// Generate off-peak window with start < end
		startH := rapid.IntRange(0, 22).Draw(t, "offpeakStartH")
		startM := rapid.IntRange(0, 59).Draw(t, "offpeakStartM")
		endH := rapid.IntRange(startH+1, min(startH+6, 23)).Draw(t, "offpeakEndH")
		endM := rapid.IntRange(0, 59).Draw(t, "offpeakEndM")
		if endH*60+endM <= startH*60+startM {
			endM = startM + 1
			if endM > 59 {
				endH++
				endM = 0
			}
		}

		// Generate readings spanning a day at ~10s intervals
		n := rapid.IntRange(0, 500).Draw(t, "numReadings")
		dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, sydneyTZ)
		readings := make([]dynamo.ReadingItem, n)
		ts := dayStart.Unix()
		for i := range n {
			gap := rapid.IntRange(8, 15).Draw(t, fmt.Sprintf("gap%d", i))
			ts += int64(gap)
			readings[i] = dynamo.ReadingItem{
				Timestamp: ts,
				Pload:     rapid.Float64Range(0, 10000).Draw(t, fmt.Sprintf("pload%d", i)),
			}
		}
		return pbtInput{
			readings:     readings,
			offpeakStart: fmt.Sprintf("%02d:%02d", startH, startM),
			offpeakEnd:   fmt.Sprintf("%02d:%02d", endH, endM),
		}
	})

	parseMinOfDay := func(s string) int {
		h := int(s[0]-'0')*10 + int(s[1]-'0')
		m := int(s[3]-'0')*10 + int(s[4]-'0')
		return h*60 + m
	}

	t.Run("result count <= 3", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := findPeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			assert.LessOrEqual(t, len(got), 3)
		})
	})

	t.Run("all periods outside off-peak", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := findPeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			opStartM := parseMinOfDay(in.offpeakStart)
			opEndM := parseMinOfDay(in.offpeakEnd)
			for _, p := range got {
				startT, _ := time.Parse(time.RFC3339, p.Start)
				endT, _ := time.Parse(time.RFC3339, p.End)
				startMOD := startT.In(sydneyTZ).Hour()*60 + startT.In(sydneyTZ).Minute()
				endMOD := endT.In(sydneyTZ).Hour()*60 + endT.In(sydneyTZ).Minute()
				assert.False(t, startMOD >= opStartM && startMOD < opEndM, "period start %s is in off-peak", p.Start)
				assert.False(t, endMOD >= opStartM && endMOD < opEndM, "period end %s is in off-peak", p.End)
			}
		})
	})

	t.Run("non-overlapping", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := findPeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			if len(got) < 2 {
				return
			}
			sorted := make([]PeakPeriod, len(got))
			copy(sorted, got)
			slices.SortFunc(sorted, func(a, b PeakPeriod) int { return cmp.Compare(a.Start, b.Start) })
			for i := 1; i < len(sorted); i++ {
				assert.LessOrEqual(t, sorted[i-1].End, sorted[i].Start,
					"periods overlap: %s-%s and %s-%s", sorted[i-1].Start, sorted[i-1].End, sorted[i].Start, sorted[i].End)
			}
		})
	})

	t.Run("energy positive", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := findPeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			for _, p := range got {
				assert.True(t, p.EnergyWh > 0, "energy should be positive, got %f", p.EnergyWh)
			}
		})
	})

	t.Run("descending energy order", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := findPeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			for i := 1; i < len(got); i++ {
				assert.True(t, got[i-1].EnergyWh >= got[i].EnergyWh,
					"not descending: %f < %f", got[i-1].EnergyWh, got[i].EnergyWh)
			}
		})
	})

	t.Run("duration >= 2 minutes", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := findPeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			for _, p := range got {
				startT, _ := time.Parse(time.RFC3339, p.Start)
				endT, _ := time.Parse(time.RFC3339, p.End)
				assert.True(t, endT.Sub(startT) >= 2*time.Minute,
					"duration %s < 2 minutes for period %s-%s", endT.Sub(startT), p.Start, p.End)
			}
		})
	})
}

func TestMelbourneSunriseSunset(t *testing.T) {
	tests := map[string]struct {
		date      string
		isSunrise bool
		// Min/max UTC instants for the result (inclusive). The table is in
		// HH:MM precision, so we assert plausible ranges rather than exact
		// equality.
		check func(t *testing.T, got time.Time)
	}{
		"winter solstice sunset between 16:30 and 17:30 AEST": {
			date:      "2026-06-21",
			isSunrise: false,
			check: func(t *testing.T, got time.Time) {
				local := got.In(sydneyTZ)
				assert.Equal(t, 2026, local.Year())
				assert.Equal(t, time.June, local.Month())
				assert.Equal(t, 21, local.Day())
				mins := local.Hour()*60 + local.Minute()
				assert.GreaterOrEqual(t, mins, 16*60+30, "expected sunset >= 16:30 AEST")
				assert.LessOrEqual(t, mins, 17*60+30, "expected sunset <= 17:30 AEST")
			},
		},
		"summer solstice sunset between 20:00 and 21:00 AEDT": {
			date:      "2026-12-22",
			isSunrise: false,
			check: func(t *testing.T, got time.Time) {
				local := got.In(sydneyTZ)
				assert.Equal(t, 2026, local.Year())
				assert.Equal(t, time.December, local.Month())
				assert.Equal(t, 22, local.Day())
				mins := local.Hour()*60 + local.Minute()
				assert.GreaterOrEqual(t, mins, 20*60, "expected sunset >= 20:00 AEDT")
				assert.LessOrEqual(t, mins, 21*60, "expected sunset <= 21:00 AEDT")
			},
		},
		"leap year Feb 29 falls back to Feb 28 values": {
			date:      "2028-02-29",
			isSunrise: false,
			check: func(t *testing.T, got time.Time) {
				// Compare to Feb 28 of the same year, parsed via the same
				// helper. The wall-clock minute-of-day must match because the
				// table reuses Feb 28's value for Feb 29.
				feb28 := melbourneSunriseSunset("2028-02-28", false)
				want := feb28.In(sydneyTZ)
				gotLocal := got.In(sydneyTZ)
				// Sanity: the result should be on the requested date.
				assert.Equal(t, time.February, gotLocal.Month())
				assert.Equal(t, 29, gotLocal.Day())
				// The wall-clock HH:MM should match Feb 28's value.
				assert.Equal(t, want.Hour(), gotLocal.Hour(), "Feb 29 should reuse Feb 28's HH")
				assert.Equal(t, want.Minute(), gotLocal.Minute(), "Feb 29 should reuse Feb 28's MM")
			},
		},
		"AEDT-end transition day resolves to a UTC instant on the right local date": {
			date:      "2026-04-05",
			isSunrise: true,
			check: func(t *testing.T, got time.Time) {
				// On 2026-04-05 AEDT ends at 03:00. The result must still
				// land on the local date 2026-04-05 in sydneyTZ.
				local := got.In(sydneyTZ)
				assert.Equal(t, 2026, local.Year())
				assert.Equal(t, time.April, local.Month())
				assert.Equal(t, 5, local.Day())
				// Sunrise on Apr 5 in Melbourne is roughly 06:30-07:40 local
				// (depending on which side of DST).
				mins := local.Hour()*60 + local.Minute()
				assert.GreaterOrEqual(t, mins, 5*60, "expected sunrise after 05:00")
				assert.LessOrEqual(t, mins, 9*60, "expected sunrise before 09:00")
			},
		},
		"return value is in UTC": {
			date:      "2026-06-21",
			isSunrise: true,
			check: func(t *testing.T, got time.Time) {
				assert.Equal(t, time.UTC, got.Location())
			},
		},
		"truncated to whole seconds": {
			date:      "2026-12-22",
			isSunrise: true,
			check: func(t *testing.T, got time.Time) {
				assert.Zero(t, got.Nanosecond())
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := melbourneSunriseSunset(tc.date, tc.isSunrise)
			tc.check(t, got)
		})
	}
}

func TestIntegratePload(t *testing.T) {
	// Helper that builds readings at fixed timestamps relative to a base
	// (any sufficiently large value that avoids signed-int corner cases).
	const base int64 = 1_700_000_000

	mkReadings := func(specs ...struct {
		dt    int64
		pload float64
	}) []dynamo.ReadingItem {
		out := make([]dynamo.ReadingItem, len(specs))
		for i, s := range specs {
			out[i] = dynamo.ReadingItem{Timestamp: base + s.dt, Pload: s.pload}
		}
		return out
	}

	tests := map[string]struct {
		readings []dynamo.ReadingItem
		startDt  int64
		endDt    int64
		wantKwh  float64
		// tolerance for floating point comparison
		delta float64
	}{
		"design worked example: t=0,10,20,30 plouds 200,400,-100,600 over [15,25)": {
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{0, 200},
				struct {
					dt    int64
					pload float64
				}{10, 400},
				struct {
					dt    int64
					pload float64
				}{20, -100},
				struct {
					dt    int64
					pload float64
				}{30, 600},
			),
			startDt: 15,
			endDt:   25,
			// pts: {15,200}, {20,0}, {25,300}; trapezoids 500 + 750 = 1250 W·s
			wantKwh: 1250.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"start exactly at a reading: that reading is included as interior": {
			// Period [10, 30); reading at t=10 must count as interior, not
			// be reproduced via left-edge synthesis.
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{0, 100},
				struct {
					dt    int64
					pload float64
				}{10, 200},
				struct {
					dt    int64
					pload float64
				}{20, 300},
				struct {
					dt    int64
					pload float64
				}{30, 400},
			),
			startDt: 10,
			endDt:   30,
			// pts: {10,200}, {20,300} (interior), and right-edge synth
			// pair-gap = 10s ≤ 60, interpolate at 30 between {20,300} and {30,400}: result 400
			// pts = {10,200}, {20,300}, {30,400}.
			// Trapezoids: ((200+300)/2)*10 + ((300+400)/2)*10 = 2500 + 3500 = 6000 W·s
			wantKwh: 6000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"end exactly at a reading: that reading is excluded (half-open)": {
			// Period [10, 30) over readings at 0,10,20,30. Reading at t=30 excluded.
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{0, 100},
				struct {
					dt    int64
					pload float64
				}{10, 200},
				struct {
					dt    int64
					pload float64
				}{20, 300},
				struct {
					dt    int64
					pload float64
				}{30, 400},
			),
			startDt: 10,
			endDt:   30,
			// The t=30 reading is excluded from the interior set (half-open), but
			// the right-edge synthesis still places a point at endUnix=30 by
			// interpolating between readings[iR-1] (t=20) and readings[iR] (t=30).
			// Because the bracket is exact, the synthesised value equals the
			// t=30 Pload. See the edge-case table in design.md.
			wantKwh: 6000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"60s pair-gap skip at left bracket: edge synthesis skipped": {
			// Period [50, 100). Left bracket gap = 80s > 60s — skip left edge synthesis.
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{0, 1000},
				struct {
					dt    int64
					pload float64
				}{80, 100},
				struct {
					dt    int64
					pload float64
				}{90, 200},
			),
			startDt: 50,
			endDt:   100,
			// Left bracket: readings[0] at t=0, readings[1] at t=80, gap=80>60: skip.
			// Interior: {80,100}, {90,200}. Right bracket: iR=3 (none); skip.
			// pts = {80,100},{90,200}; trapezoid = ((100+200)/2)*10 = 1500 W·s
			wantKwh: 1500.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"start before all readings: no left edge synthesis": {
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{100, 100},
				struct {
					dt    int64
					pload float64
				}{110, 200},
				struct {
					dt    int64
					pload float64
				}{120, 300},
			),
			startDt: 50,
			endDt:   200,
			// No left bracket. Interior: all three readings. No right bracket either.
			// pts: {100,100},{110,200},{120,300}.
			// Trapezoids: ((100+200)/2)*10 + ((200+300)/2)*10 = 1500 + 2500 = 4000 W·s
			wantKwh: 4000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"end after all readings: no right edge synthesis": {
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{0, 100},
				struct {
					dt    int64
					pload float64
				}{10, 200},
				struct {
					dt    int64
					pload float64
				}{20, 300},
			),
			startDt: 0,
			endDt:   200,
			// No right bracket. Interior: all three readings. No left bracket either.
			// pts: {0,100},{10,200},{20,300}.
			// Trapezoids: 1500 + 2500 = 4000 W·s
			wantKwh: 4000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"single interior reading and no usable brackets: returns 0": {
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{50, 1000},
			),
			startDt: 0,
			endDt:   200,
			wantKwh: 0,
			delta:   1e-12,
		},
		"empty readings returns 0": {
			readings: nil,
			startDt:  0,
			endDt:    200,
			wantKwh:  0,
			delta:    1e-12,
		},
		"all readings outside period: returns 0": {
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{0, 1000},
				struct {
					dt    int64
					pload float64
				}{10, 1000},
			),
			startDt: 100,
			endDt:   200,
			// iL = 1 (readings[1].ts=10 < 100). iL+1 = 2 == len(readings). No left edge.
			// iR = 2 (out of range). iR-1 = 1 < 200, but iR == len. No right edge.
			// No interior. pts is empty.
			wantKwh: 0,
			delta:   1e-12,
		},
		"negative pload clamped before interpolation at right edge": {
			// readings t=0:100 t=10:-200. Period [0,5).
			// Right bracket: gap = 10 ≤ 60. Clamped values: 100 and 0.
			// Interpolate at 5: 100 + (0-100)*(5-0)/(10-0) = 50.
			// Interior: t=0 (clamped to 100). pts = {0,100},{5,50}.
			// Trapezoid = ((100+50)/2)*5 = 375 W·s.
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{0, 100},
				struct {
					dt    int64
					pload float64
				}{10, -200},
			),
			startDt: 0,
			endDt:   5,
			wantKwh: 375.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"60s pair-gap skip across adjacent pts pairs": {
			// readings every 10s except a 70s gap between t=20 and t=90.
			// Period [0, 100): all four readings interior. Adjacent pair (20,90) has dt=70>60: skip.
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{0, 100},
				struct {
					dt    int64
					pload float64
				}{10, 100},
				struct {
					dt    int64
					pload float64
				}{20, 100},
				struct {
					dt    int64
					pload float64
				}{90, 100},
			),
			startDt: 0,
			endDt:   100,
			// Trapezoids: (100+100)/2 * 10 = 1000, (100+100)/2 * 10 = 1000, skip (20→90), no right edge synth (last reading at 90; iR=4=len, no right edge).
			// Total = 2000 W·s.
			wantKwh: 2000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"left edge synthesis exactly at startUnix when readings[iL+1].Timestamp == startUnix": {
			// Period [10, 20). readings[iL+1].Timestamp == 10 == startUnix.
			// Per design: skip left edge to avoid duplicating the interior reading.
			readings: mkReadings(
				struct {
					dt    int64
					pload float64
				}{0, 100},
				struct {
					dt    int64
					pload float64
				}{10, 200},
				struct {
					dt    int64
					pload float64
				}{20, 300},
			),
			startDt: 10,
			endDt:   20,
			// Interior: {10,200}. Right edge synth: t=20 reading, gap 10s ≤ 60.
			// Interpolate: at endUnix=20 between {10,200} and {20,300}: 300.
			// pts = {10,200}, {20,300}; trapezoid ((200+300)/2)*10 = 2500 W·s.
			wantKwh: 2500.0 / 3_600_000.0,
			delta:   1e-9,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := integratePload(tc.readings, base+tc.startDt, base+tc.endDt)
			assert.InDelta(t, tc.wantKwh, got, tc.delta)
		})
	}
}

// readingPpv builds a ReadingItem at the given Sydney local time with the
// specified Ppv and Pload values for findEveningNight tests.
func readingPpv(date string, hour, minute, second int, ppv, pload float64) dynamo.ReadingItem {
	d, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)
	t := time.Date(d.Year(), d.Month(), d.Day(), hour, minute, second, 0, sydneyTZ)
	return dynamo.ReadingItem{Timestamp: t.Unix(), Ppv: ppv, Pload: pload}
}

func TestFindEveningNight(t *testing.T) {
	const date = "2026-04-15"
	dayStart, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)

	// "Past day" date so isToday == false.
	const pastDate = "2026-03-10"

	// Reusable: a day where Ppv>0 from 06:30 to 18:00 (10s cadence), constant
	// Pload=1000. We don't need every second; coarse readings every 5 min are
	// enough to exercise the boundaries.
	pastDayReadings := func() []dynamo.ReadingItem {
		var out []dynamo.ReadingItem
		// Pre-dawn: Pload only, no solar. 00:00 → 06:25 every 5 min.
		for h := 0; h < 7; h++ {
			for m := 0; m < 60; m += 5 {
				if h == 6 && m >= 30 {
					break
				}
				out = append(out, readingPpv(pastDate, h, m, 0, 0, 1000))
			}
		}
		// Solar window 06:30 → 17:55 every 5 min.
		for h := 6; h < 18; h++ {
			startMin := 0
			if h == 6 {
				startMin = 30
			}
			for m := startMin; m < 60; m += 5 {
				out = append(out, readingPpv(pastDate, h, m, 0, 1000, 1000))
			}
		}
		// Post-sunset 18:00 → 23:55 every 5 min.
		for h := 18; h < 24; h++ {
			for m := 0; m < 60; m += 5 {
				out = append(out, readingPpv(pastDate, h, m, 0, 0, 1000))
			}
		}
		return out
	}()

	tests := map[string]struct {
		readings []dynamo.ReadingItem
		date     string
		today    string
		now      time.Time
		check    func(t *testing.T, got *EveningNight)
	}{
		"typical past day, both periods complete from readings": {
			readings: pastDayReadings,
			date:     pastDate,
			today:    date, // today != pastDate
			now:      time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *EveningNight) {
				require.NotNil(t, got)
				require.NotNil(t, got.Night)
				require.NotNil(t, got.Evening)
				assert.Equal(t, EveningNightStatusComplete, got.Night.Status)
				assert.Equal(t, EveningNightStatusComplete, got.Evening.Status)
				assert.Equal(t, EveningNightBoundaryReadings, got.Night.BoundarySource)
				assert.Equal(t, EveningNightBoundaryReadings, got.Evening.BoundarySource)
			},
		},
		"today before sunrise: only night, in-progress, end clamped to now": {
			readings: []dynamo.ReadingItem{
				// A few overnight readings, no Ppv>0 yet.
				readingPpv(date, 1, 0, 0, 0, 1000),
				readingPpv(date, 2, 0, 0, 0, 1000),
				readingPpv(date, 3, 0, 0, 0, 1000),
				readingPpv(date, 4, 30, 0, 0, 1000),
			},
			date:  date,
			today: date,
			now:   time.Date(2026, 4, 15, 4, 30, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *EveningNight) {
				require.NotNil(t, got)
				require.NotNil(t, got.Night)
				assert.Nil(t, got.Evening, "evening must be omitted when sun has not set")
				assert.Equal(t, EveningNightStatusInProgress, got.Night.Status)
				assert.Equal(t, EveningNightBoundaryEstimated, got.Night.BoundarySource)
				// End == now (clamped).
				wantEnd := time.Date(2026, 4, 15, 4, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEnd, got.Night.End)
				wantStart := dayStart.UTC().Format(time.RFC3339)
				assert.Equal(t, wantStart, got.Night.Start)
			},
		},
		"today after sunset: both blocks; evening in-progress, night complete": {
			readings: func() []dynamo.ReadingItem {
				// Solar 07:00 → 17:30; now=22:00. lastPpv at 17:30, firstPpv at 07:00.
				var r []dynamo.ReadingItem
				// pre-dawn
				r = append(r, readingPpv(date, 0, 30, 0, 0, 1000))
				// first solar
				r = append(r, readingPpv(date, 7, 0, 0, 500, 1000))
				// last solar
				r = append(r, readingPpv(date, 17, 30, 0, 100, 1000))
				// after sunset
				r = append(r, readingPpv(date, 19, 0, 0, 0, 1000))
				r = append(r, readingPpv(date, 21, 0, 0, 0, 1000))
				return r
			}(),
			date:  date,
			today: date,
			now:   time.Date(2026, 4, 15, 22, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *EveningNight) {
				require.NotNil(t, got)
				require.NotNil(t, got.Night)
				require.NotNil(t, got.Evening)
				assert.Equal(t, EveningNightStatusComplete, got.Night.Status)
				assert.Equal(t, EveningNightStatusInProgress, got.Evening.Status)
				assert.Equal(t, EveningNightBoundaryReadings, got.Night.BoundarySource)
				assert.Equal(t, EveningNightBoundaryReadings, got.Evening.BoundarySource)
				// Evening end clamped to now.
				wantEnd := time.Date(2026, 4, 15, 22, 0, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEnd, got.Evening.End)
			},
		},
		"today midday: only night; evening omitted by today gate": {
			readings: func() []dynamo.ReadingItem {
				var r []dynamo.ReadingItem
				r = append(r, readingPpv(date, 0, 30, 0, 0, 1000))
				r = append(r, readingPpv(date, 7, 0, 0, 500, 1000))
				r = append(r, readingPpv(date, 11, 0, 0, 800, 1000))
				r = append(r, readingPpv(date, 13, 0, 0, 900, 1000))
				return r
			}(),
			date:  date,
			today: date,
			now:   time.Date(2026, 4, 15, 13, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *EveningNight) {
				require.NotNil(t, got)
				require.NotNil(t, got.Night)
				assert.Nil(t, got.Evening, "evening must be omitted by today gate (sun still up)")
				assert.Equal(t, EveningNightStatusComplete, got.Night.Status)
				assert.Equal(t, EveningNightBoundaryReadings, got.Night.BoundarySource)
			},
		},
		"fully overcast past day: both blocks estimated, complete": {
			readings: []dynamo.ReadingItem{
				readingPpv(pastDate, 0, 30, 0, 0, 500),
				readingPpv(pastDate, 6, 0, 0, 0, 500),
				readingPpv(pastDate, 12, 0, 0, 0, 500),
				readingPpv(pastDate, 18, 0, 0, 0, 500),
				readingPpv(pastDate, 23, 0, 0, 0, 500),
			},
			date:  pastDate,
			today: date,
			now:   time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *EveningNight) {
				require.NotNil(t, got)
				require.NotNil(t, got.Night)
				require.NotNil(t, got.Evening)
				assert.Equal(t, EveningNightStatusComplete, got.Night.Status)
				assert.Equal(t, EveningNightStatusComplete, got.Evening.Status)
				assert.Equal(t, EveningNightBoundaryEstimated, got.Night.BoundarySource)
				assert.Equal(t, EveningNightBoundaryEstimated, got.Evening.BoundarySource)
			},
		},
		"morning solar but no afternoon (recorder dies at noon): both readings-derived (per spec)": {
			readings: []dynamo.ReadingItem{
				readingPpv(pastDate, 0, 30, 0, 0, 500),
				readingPpv(pastDate, 7, 0, 0, 500, 500),
				readingPpv(pastDate, 11, 0, 0, 800, 500),
				readingPpv(pastDate, 12, 55, 0, 700, 500), // last Ppv>0
			},
			date:  pastDate,
			today: date,
			now:   time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *EveningNight) {
				require.NotNil(t, got)
				require.NotNil(t, got.Night)
				require.NotNil(t, got.Evening)
				assert.Equal(t, EveningNightBoundaryReadings, got.Night.BoundarySource)
				// Per spec (Decision 2 / design.md): for past days, lastPpvPositive
				// is used directly even if it's mid-day. Evening start = 12:55.
				assert.Equal(t, EveningNightBoundaryReadings, got.Evening.BoundarySource)
				wantStart := time.Date(2026, 3, 10, 12, 55, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantStart, got.Evening.Start)
			},
		},
		"zero readings inside period emits block with totalKwh=0": {
			// Past day with no readings between estimated sunrise and sunset.
			readings: []dynamo.ReadingItem{
				// Just one reading well outside both periods so they exist.
				readingPpv(pastDate, 12, 0, 0, 100, 0),
			},
			date:  pastDate,
			today: date,
			now:   time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *EveningNight) {
				require.NotNil(t, got)
				// Both blocks expected (one Ppv>0 reading at noon → both blocks readings).
				require.NotNil(t, got.Night)
				require.NotNil(t, got.Evening)
				// Pload=0 → totalKwh=0.
				assert.InDelta(t, 0, got.Night.TotalKwh, 1e-9)
				assert.InDelta(t, 0, got.Evening.TotalKwh, 1e-9)
				// AverageKwhPerHour should be non-nil (elapsed >> 60s).
				require.NotNil(t, got.Night.AverageKwhPerHour)
				require.NotNil(t, got.Evening.AverageKwhPerHour)
				assert.InDelta(t, 0, *got.Night.AverageKwhPerHour, 1e-9)
				assert.InDelta(t, 0, *got.Evening.AverageKwhPerHour, 1e-9)
			},
		},
		"in-progress evening with elapsed < 60s: averageKwhPerHour omitted": {
			readings: []dynamo.ReadingItem{
				readingPpv(date, 0, 0, 30, 0, 1000),
				readingPpv(date, 8, 0, 0, 500, 1000),
				readingPpv(date, 18, 0, 0, 100, 1000), // last Ppv>0
				readingPpv(date, 18, 0, 30, 0, 1000),
			},
			date:  date,
			today: date,
			// 30s after the lastPpvPositive at 18:00:00.
			now: time.Date(2026, 4, 15, 18, 0, 30, 0, sydneyTZ),
			check: func(t *testing.T, got *EveningNight) {
				require.NotNil(t, got)
				require.NotNil(t, got.Evening)
				assert.Equal(t, EveningNightStatusInProgress, got.Evening.Status)
				assert.Nil(t, got.Evening.AverageKwhPerHour, "elapsed<60s must omit average")
			},
		},
		"empty readings on past date emits both blocks as estimated": {
			// Caller gates findEveningNight on len(readings)>0, so an empty
			// slice is the canonical "skip" signal in production. This test
			// pins the documented behaviour when the gate is bypassed: with
			// no readings on a past date, both blocks fall back to the sun
			// table because dayStart < sunrise and sunset < dayEnd.
			readings: nil,
			date:     "2026-04-14",
			today:    date,
			now:      time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *EveningNight) {
				require.NotNil(t, got)
				require.NotNil(t, got.Night)
				require.NotNil(t, got.Evening)
				assert.Equal(t, EveningNightBoundaryEstimated, got.Night.BoundarySource)
				assert.Equal(t, EveningNightBoundaryEstimated, got.Evening.BoundarySource)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := findEveningNight(tc.readings, tc.date, tc.today, tc.now)
			tc.check(t, got)
		})
	}
}

func BenchmarkFindEveningNight(b *testing.B) {
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, sydneyTZ)
	readings := make([]dynamo.ReadingItem, 0, 8640)
	// 10s cadence over a full day.
	for i := range 8640 {
		// Ppv positive between buckets ~6:30-18:00 to exercise both branches.
		t := dayStart.Add(time.Duration(i*10) * time.Second)
		secOfDay := t.Hour()*3600 + t.Minute()*60 + t.Second()
		var ppv float64
		if secOfDay >= 6*3600+30*60 && secOfDay <= 18*3600 {
			ppv = float64(500 + i%3000)
		}
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: t.Unix(),
			Ppv:       ppv,
			Pload:     float64(500 + i%4500),
		})
	}

	for b.Loop() {
		_ = findEveningNight(readings, "2026-04-15", "2026-04-16",
			time.Date(2026, 4, 16, 12, 0, 0, 0, sydneyTZ))
	}
}

func BenchmarkFindPeakPeriods(b *testing.B) {
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, sydneyTZ)
	readings := make([]dynamo.ReadingItem, 0, 8640)
	for i := range 8640 {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: dayStart.Unix() + int64(i*10),
			Pload:     float64(500 + i%4500), // varied load 500-5000W
		})
	}

	for b.Loop() {
		_ = findPeakPeriods(readings, "11:00", "14:00")
	}
}
