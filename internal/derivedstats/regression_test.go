package derivedstats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegression_20260518_PeakDropsBelow025Kwh is the bug-incident assertion
// for T-1341 (AC 2.1). On 2026-05-18 the snapshot-diff method recorded
// 1.74 kWh peak / 18.95 kWh off-peak against the meter's 0.17 / 20.75 kWh.
// After re-integrating readings over [11:00, 14:00) Sydney, the peak
// (= eInput − gridUsageKwh) should drop to ≤ 0.25 kWh — within sensor
// calibration of the meter's 0.17 reading.
//
// Fixture provenance: see testdata/offpeak_2026_05_18.json. The actual
// 10-second readings from the incident were not captured before the 30-day
// TTL pruned them, so the fixture is synthesised to reproduce the bug
// shape — a heavy 6.9 kW grid-charging plateau through Phase A and a soft
// rampdown to 5.4 kW over the final 10 minutes before the inverter
// physically stopped at 14:00:07. The boundary-bracket reading at +7s
// exercises the right-edge synthesis (AC 1.5).
func TestRegression_20260518_PeakDropsBelow025Kwh(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)

	readings, windowStart, windowEnd := readings20260518(loc)
	got, ok := IntegrateOffpeakDeltas(readings, windowStart.Unix(), windowEnd.Unix())
	require.True(t, ok, "fixture must produce a usable integration")
	t.Logf("2026-05-18 fixture: gridUsageKwh=%.4f sampleCount=%d skippedPairs=%d",
		got.GridImportKwh, got.SampleCount, got.SkippedPairs)

	// AC 2.1: peak (= eInput − gridUsageKwh) ≤ 0.25 kWh.
	// eInput is the AlphaESS daily total — 20.69 on 2026-05-18 per the
	// poller's getOneDateEnergyBySn snapshot at 14:00.
	const eInputKwh = 20.69
	peak := eInputKwh - got.GridImportKwh
	assert.LessOrEqual(t, peak, 0.25,
		"AC 2.1: peak (= eInput − gridUsageKwh) must be ≤ 0.25 kWh after re-integration; got %.3f (gridUsageKwh = %.3f)",
		peak, got.GridImportKwh)
	// AC 2.2: gridUsageKwh ≤ eInput is the structural sanity bound — off-peak
	// grid import is a subset of total grid import for the day.
	assert.LessOrEqual(t, got.GridImportKwh, eInputKwh,
		"AC 2.2: gridUsageKwh (%.3f) must be ≤ eInput (%.3f)", got.GridImportKwh, eInputKwh)
	// Sanity: the bug shape — readings-integration should differ substantially
	// from the snapshot-diff value (1.74 kWh peak under the old method). If
	// this assertion ever fails, the fixture has been weakened to the point
	// where it no longer reproduces the original incident.
	assert.Greater(t, got.GridImportKwh, 19.0,
		"fixture should reproduce a heavy-charge day (>19 kWh integrated); got %.3f", got.GridImportKwh)
}

// readings20260518 builds the synthetic 2026-05-18 reading series described
// in testdata/offpeak_2026_05_18.json. Two phases (constant plateau then
// linear rampdown) plus a single bracketing reading 7 s past the window end
// that mirrors the actual inverter-stop event recorded in the incident.
func readings20260518(loc *time.Location) ([]Reading, time.Time, time.Time) {
	day := time.Date(2026, 5, 18, 0, 0, 0, 0, loc)
	windowStart := day.Add(11 * time.Hour)               // 11:00:00 Sydney
	phaseATail := day.Add(13*time.Hour + 50*time.Minute) // 13:50:00
	windowEnd := day.Add(14 * time.Hour)                 // 14:00:00 Sydney (exclusive)
	rightBracket := windowEnd.Add(7 * time.Second)       // 14:00:07 — inverter stops

	const (
		phaseAPgrid      = 6900.0
		phaseAPbat       = -6600.0
		phaseBStartPgrid = 6900.0
		phaseBEndPgrid   = 5400.0
		phaseBStartPbat  = -6600.0
		phaseBEndPbat    = -5100.0
		intervalSeconds  = 10
	)

	out := make([]Reading, 0, 1083)

	// Phase A: 11:00:00 → 13:50:00 (inclusive end), 10s cadence, constant.
	for ts := windowStart; !ts.After(phaseATail); ts = ts.Add(intervalSeconds * time.Second) {
		out = append(out, Reading{
			Timestamp: ts.Unix(),
			Pgrid:     phaseAPgrid,
			Pbat:      phaseAPbat,
		})
	}
	// Phase B: 13:50:10 → 13:59:50, linear ramp on pgrid + pbat.
	phaseBDuration := windowEnd.Sub(phaseATail) // 10 minutes
	for ts := phaseATail.Add(intervalSeconds * time.Second); ts.Before(windowEnd); ts = ts.Add(intervalSeconds * time.Second) {
		frac := float64(ts.Sub(phaseATail)) / float64(phaseBDuration)
		out = append(out, Reading{
			Timestamp: ts.Unix(),
			Pgrid:     phaseBStartPgrid + (phaseBEndPgrid-phaseBStartPgrid)*frac,
			Pbat:      phaseBStartPbat + (phaseBEndPbat-phaseBStartPbat)*frac,
		})
	}
	// Right boundary bracket: the inverter physically stopped at 14:00:07.
	out = append(out, Reading{
		Timestamp: rightBracket.Unix(),
		Pgrid:     0,
		Pbat:      0,
	})
	return out, windowStart, windowEnd
}
