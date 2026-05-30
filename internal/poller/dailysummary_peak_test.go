package poller

import (
	"context"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makePeakReadings builds 10s-spaced readings across the whole day with a
// constant positive grid import, so both peak sub-windows pass the usability
// gate and the integrated peak value is non-zero.
func makePeakReadings(date string, loc *time.Location, pgrid float64) []dynamo.ReadingItem {
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	var out []dynamo.ReadingItem
	for t := dayStart; t.Before(dayEnd); t = t.Add(10 * time.Second) {
		out = append(out, dynamo.ReadingItem{
			Timestamp: t.Unix(),
			Pgrid:     pgrid,
			Soc:       50,
		})
	}
	return out
}

// TestSummarisation_PeakWrittenWhenDerivedAlreadyDone covers the Decision 3
// backfill path: a row that already has derivedStats but no peak gets peak
// written on the next tick, and the derivedStats group is left untouched.
func TestSummarisation_PeakWrittenWhenDerivedAlreadyDone(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{
			SysSn: "TEST123", Date: "2026-04-14",
			DerivedStatsComputedAt: "2026-04-14T22:00:00Z",
			// PeakComputedAt absent → peak still needed.
		},
		queryReadingsResult: makePeakReadings("2026-04-14", loc, 1000),
	}
	p, _ := summarisationFixturePoller(t, ms)

	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, PassResultSuccess, result)
	require.Equal(t, 1, ms.derivedUpdates)
	require.NotNil(t, ms.lastDerived)

	// Only the peak group was written; the derivedStats group is left empty so
	// UpdateDailyEnergyDerived does not re-touch it.
	assert.Empty(t, ms.lastDerived.DerivedStatsComputedAt, "derivedStats group must not be rewritten")
	assert.Nil(t, ms.lastDerived.DailyUsage, "derivedStats group must not be rewritten")
	assert.NotEmpty(t, ms.lastDerived.PeakComputedAt, "peak sentinel must be set")
	require.NotNil(t, ms.lastDerived.PeakGridImportKwh)
	assert.Greater(t, *ms.lastDerived.PeakGridImportKwh, 0.0, "constant 1000W import yields positive peak kWh")
}

// TestSummarisation_PeakSkippedWhenBothSentinelsSet confirms the pass skips
// (and does not query readings) only when BOTH sentinels are present.
func TestSummarisation_PeakSkippedWhenBothSentinelsSet(t *testing.T) {
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{
			SysSn: "TEST123", Date: "2026-04-14",
			DerivedStatsComputedAt: "2026-04-14T22:00:00Z",
			PeakComputedAt:         "2026-04-14T22:00:00Z",
		},
	}
	p, _ := summarisationFixturePoller(t, ms)

	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, PassResultSkippedAlreadyDone, result)
	assert.Zero(t, ms.derivedUpdates)
	assert.Nil(t, ms.queryReadingsResult, "no readings query when both sentinels set")
}

// TestSummarisation_PeakGateFailureLeavesFieldUnwritten verifies that when a
// peak sub-window fails the usability gate, the peak value is left absent but
// the sentinel is still set (so the row is not retried every hour).
func TestSummarisation_PeakGateFailureLeavesFieldUnwritten(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	// Build readings only in the off-peak and evening windows, leaving the
	// morning window [00:00, 11:00) with no usable readings → gate fails.
	dayStart, _ := time.ParseInLocation("2006-01-02", "2026-04-14", loc)
	var readings []dynamo.ReadingItem
	for h := 11; h < 24; h++ {
		for m := range 60 {
			t := dayStart.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
			readings = append(readings, dynamo.ReadingItem{Timestamp: t.Unix(), Pgrid: 500, Soc: 50})
		}
	}
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{
			SysSn: "TEST123", Date: "2026-04-14",
			DerivedStatsComputedAt: "2026-04-14T22:00:00Z",
		},
		queryReadingsResult: readings,
	}
	p, _ := summarisationFixturePoller(t, ms)

	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, PassResultSuccess, result)
	require.NotNil(t, ms.lastDerived)
	assert.NotEmpty(t, ms.lastDerived.PeakComputedAt, "sentinel set even on gate failure to avoid hourly retries")
	assert.Nil(t, ms.lastDerived.PeakGridImportKwh, "gate failure leaves the peak value absent")
}
