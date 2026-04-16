package api

import (
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

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
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
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
