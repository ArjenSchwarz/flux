package derivedstats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntegratePpv(t *testing.T) {
	const base int64 = 1_700_000_000

	type spec struct {
		dt  int64
		ppv float64
	}
	mkReadings := func(specs ...spec) []Reading {
		out := make([]Reading, len(specs))
		for i, s := range specs {
			out[i] = Reading{Timestamp: base + s.dt, Ppv: s.ppv}
		}
		return out
	}

	tests := map[string]struct {
		readings    []Reading
		startDt     int64
		endDt       int64
		wantKwh     float64
		wantSamples int
		delta       float64
	}{
		"empty readings": {
			readings:    nil,
			startDt:     0,
			endDt:       200,
			wantKwh:     0,
			wantSamples: 0,
			delta:       1e-12,
		},
		"single reading inside window: integration zero, sampleCount one": {
			readings: mkReadings(
				spec{50, 1000},
			),
			startDt:     0,
			endDt:       200,
			wantKwh:     0,
			wantSamples: 1,
			delta:       1e-12,
		},
		"two readings inside window with no brackets": {
			readings: mkReadings(
				spec{100, 1000},
				spec{110, 2000},
				spec{120, 3000},
			),
			startDt:     50,
			endDt:       200,
			wantKwh:     40000.0 / 3_600_000.0,
			wantSamples: 3,
			delta:       1e-9,
		},
		"design worked example mirrored: t=0,10,20,30 ppv 200,400,-100,600 over [15,25)": {
			readings: mkReadings(
				spec{0, 200},
				spec{10, 400},
				spec{20, -100},
				spec{30, 600},
			),
			startDt:     15,
			endDt:       25,
			wantKwh:     1250.0 / 3_600_000.0,
			wantSamples: 1,
			delta:       1e-9,
		},
		"60s pair-gap skip across adjacent pts pairs": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, 100},
				spec{20, 100},
				spec{90, 100},
			),
			startDt:     0,
			endDt:       100,
			wantKwh:     2000.0 / 3_600_000.0,
			wantSamples: 4,
			delta:       1e-9,
		},
		"left edge synthesis at startUnix when bracket pair under 60s": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, 200},
				spec{20, 300},
				spec{30, 400},
			),
			startDt:     5,
			endDt:       30,
			wantKwh:     6875.0 / 3_600_000.0,
			wantSamples: 2,
			delta:       1e-9,
		},
		"right edge synthesis at endUnix when bracket pair under 60s": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, 200},
				spec{20, 300},
				spec{30, 400},
			),
			startDt:     0,
			endDt:       25,
			wantKwh:     5625.0 / 3_600_000.0,
			wantSamples: 3,
			delta:       1e-9,
		},
		"60s pair-gap skip at left bracket: edge synthesis skipped": {
			readings: mkReadings(
				spec{0, 1000},
				spec{80, 100},
				spec{90, 200},
			),
			startDt:     50,
			endDt:       100,
			wantKwh:     1500.0 / 3_600_000.0,
			wantSamples: 2,
			delta:       1e-9,
		},
		"negative ppv clamped before interpolation at right edge": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, -200},
			),
			startDt:     0,
			endDt:       5,
			wantKwh:     375.0 / 3_600_000.0,
			wantSamples: 1,
			delta:       1e-9,
		},
		"all readings outside period: returns 0 with zero samples": {
			readings: mkReadings(
				spec{0, 1000},
				spec{10, 1000},
			),
			startDt:     100,
			endDt:       200,
			wantKwh:     0,
			wantSamples: 0,
			delta:       1e-12,
		},
		"reading exactly at endUnix is excluded (half-open)": {
			readings: mkReadings(
				spec{0, 100},
				spec{10, 200},
				spec{20, 300},
				spec{30, 400},
			),
			startDt:     10,
			endDt:       30,
			wantKwh:     6000.0 / 3_600_000.0,
			wantSamples: 2,
			delta:       1e-9,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotKwh, gotSamples := integratePpv(tc.readings, base+tc.startDt, base+tc.endDt)
			assert.InDelta(t, tc.wantKwh, gotKwh, tc.delta)
			assert.Equal(t, tc.wantSamples, gotSamples)
		})
	}
}
