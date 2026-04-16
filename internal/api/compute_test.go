package api

import (
	"math"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
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
