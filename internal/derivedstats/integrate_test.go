package derivedstats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntegratePload(t *testing.T) {
	// Helper that builds readings at fixed timestamps relative to a base
	// (any sufficiently large value that avoids signed-int corner cases).
	const base int64 = 1_700_000_000

	type spec struct {
		dt    int64
		pload float64
	}
	mkReadings := func(specs ...spec) []Reading {
		out := make([]Reading, len(specs))
		for i, s := range specs {
			out[i] = Reading{Timestamp: base + s.dt, Pload: s.pload}
		}
		return out
	}

	tests := map[string]struct {
		readings []Reading
		startDt  int64
		endDt    int64
		wantKwh  float64
		// tolerance for floating point comparison
		delta float64
	}{
		"design worked example: t=0,10,20,30 plouds 200,400,-100,600 over [15,25)": {
			readings: mkReadings(
				spec{0, 200},
				spec{10, 400},
				spec{20, -100},
				spec{30, 600},
			),
			startDt: 15,
			endDt:   25,
			// pts: {15,200}, {20,0}, {25,300}; trapezoids 500 + 750 = 1250 W·s
			wantKwh: 1250.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"start exactly at a reading: that reading is included as interior": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, 200},
				spec{20, 300},
				spec{30, 400},
			),
			startDt: 10,
			endDt:   30,
			wantKwh: 6000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"end exactly at a reading: that reading is excluded (half-open)": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, 200},
				spec{20, 300},
				spec{30, 400},
			),
			startDt: 10,
			endDt:   30,
			wantKwh: 6000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"60s pair-gap skip at left bracket: edge synthesis skipped": {
			readings: mkReadings(
				spec{0, 1000},
				spec{80, 100},
				spec{90, 200},
			),
			startDt: 50,
			endDt:   100,
			wantKwh: 1500.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"start before all readings: no left edge synthesis": {
			readings: mkReadings(
				spec{100, 100},
				spec{110, 200},
				spec{120, 300},
			),
			startDt: 50,
			endDt:   200,
			wantKwh: 4000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"end after all readings: no right edge synthesis": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, 200},
				spec{20, 300},
			),
			startDt: 0,
			endDt:   200,
			wantKwh: 4000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"single interior reading and no usable brackets: returns 0": {
			readings: mkReadings(
				spec{50, 1000},
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
				spec{0, 1000},
				spec{10, 1000},
			),
			startDt: 100,
			endDt:   200,
			wantKwh: 0,
			delta:   1e-12,
		},
		"negative pload clamped before interpolation at right edge": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, -200},
			),
			startDt: 0,
			endDt:   5,
			wantKwh: 375.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"60s pair-gap skip across adjacent pts pairs": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, 100},
				spec{20, 100},
				spec{90, 100},
			),
			startDt: 0,
			endDt:   100,
			wantKwh: 2000.0 / 3_600_000.0,
			delta:   1e-9,
		},
		"left edge synthesis exactly at startUnix when readings[iL+1].Timestamp == startUnix": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, 200},
				spec{20, 300},
			),
			startDt: 10,
			endDt:   20,
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
