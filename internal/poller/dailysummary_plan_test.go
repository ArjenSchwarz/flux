package poller

import (
	"context"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cover the per-outcome gating table in design.md: what runs, what
// sentinels are set, and what the pass reports, for each of the four ways plan
// resolution can turn out. The old single early-return on an unresolved window
// would have starved socLow, dailyUsage, peak, and bands forever on any day
// without one (Q33).

// summarisationPollerWith builds the fixture poller against a specific plan
// source, so each outcome in the table can be exercised in isolation.
func summarisationPollerWith(t *testing.T, ms *mockStore, lister PlanLister) *Poller {
	t.Helper()
	loc, _ := time.LoadLocation("Australia/Sydney")
	p := New(nil, ms, lister, &config.Config{Serial: "TEST123", Location: loc})
	p.plans.retryDelay = time.Microsecond
	p.now = func() time.Time { return time.Date(2026, 4, 15, 2, 0, 0, 0, loc) }
	p.metrics = &fakeMetrics{}
	return p
}

// gridReadings builds a day of constant grid import at 60 s cadence, closing
// on the next local midnight so every band has a right bracket.
func gridReadings(date string, loc *time.Location, watts float64) []dynamo.ReadingItem {
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	out := make([]dynamo.ReadingItem, 0, 24*60+1)
	for ts := dayStart; !ts.After(dayEnd); ts = ts.Add(time.Minute) {
		out = append(out, dynamo.ReadingItem{
			SysSn: "TEST123", Timestamp: ts.Unix(), Pgrid: watts, Soc: 50,
		})
	}
	return out
}

func touPlanLister() PlanLister {
	savings := 0.35
	rate := 0.28
	return &mockPlanLister{responses: []planListerResponse{{items: []dynamo.PricingItem{{
		PricingID: "tou", StartDate: "2000-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
		Windows: []dynamo.PricingWindow{
			{Start: "10:00", End: "15:00", Free: true},
			{Start: "01:00", End: "06:00", Rate: &rate},
		},
		SavingsReferenceRate: &savings,
	}}}}}
}

func noFreeBandPlanLister() PlanLister {
	return &mockPlanLister{responses: []planListerResponse{{items: []dynamo.PricingItem{{
		PricingID: "flat", StartDate: "2000-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
	}}}}}
}

// --- Outcome 1: plan read failed with no cache ---

// AC 4.6 / Q14: an unreadable pricing table is transient. Setting sentinels on
// this path would make the day terminal on the strength of an infra blip, so
// nothing is written and the pass reports an error for the next tick to retry.
func TestSummarisation_PlanReadFailure_WritesNothingAndRetries(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  gridReadings("2026-04-14", loc, 1000),
	}
	p := summarisationPollerWith(t, ms, failingPlanLister())

	result := p.runSummarisationPass(context.Background(), "2026-04-14")

	assert.Equal(t, PassResultError, result)
	assert.Zero(t, ms.derivedUpdates, "a plan read failure must not set any sentinel")
}

// --- Outcome 2: plan with a free band ---

func TestSummarisation_PlanWithFreeBand_RunsEveryBlock(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  gridReadings("2026-04-14", loc, 1000),
	}
	p := summarisationPollerWith(t, ms, touPlanLister())

	require.Equal(t, PassResultSuccess, p.runSummarisationPass(context.Background(), "2026-04-14"))
	require.NotNil(t, ms.lastDerived)

	assert.NotEmpty(t, ms.lastDerived.DerivedStatsComputedAt)
	assert.NotEmpty(t, ms.lastDerived.PeakComputedAt)
	assert.NotEmpty(t, ms.lastDerived.BandsComputedAt)
	assert.NotNil(t, ms.lastDerived.SocLow)
	assert.NotNil(t, ms.lastDerived.DailyUsage)

	// The split's geometry is the plan's rated segments — the free band is
	// absent because the flux-offpeak row owns that import (Q31).
	assert.Equal(t, []dynamo.BandImportAttr{
		{Start: "00:00", End: "01:00", Kwh: 1.0},
		{Start: "01:00", End: "06:00", Kwh: 5.0},
		{Start: "06:00", End: "10:00", Kwh: 4.0},
		{Start: "15:00", End: "24:00", Kwh: 9.0},
	}, ms.lastDerived.BandImports)
}

// Data Consistency: peakGridImportKwh is import outside the free window, which
// is exactly the sum of the rated bands. Deriving both from one integration is
// what makes the two agree by construction rather than by coincidence.
func TestSummarisation_PeakEqualsSumOfRatedBands(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  gridReadings("2026-04-14", loc, 1000),
	}
	p := summarisationPollerWith(t, ms, touPlanLister())

	require.Equal(t, PassResultSuccess, p.runSummarisationPass(context.Background(), "2026-04-14"))
	require.NotNil(t, ms.lastDerived.PeakGridImportKwh)

	var sum float64
	for _, b := range ms.lastDerived.BandImports {
		sum += b.Kwh
	}
	assert.InDelta(t, sum, *ms.lastDerived.PeakGridImportKwh, 0.01)
	// 24 h at 1 kW less the 5 h free window.
	assert.InDelta(t, 19.0, *ms.lastDerived.PeakGridImportKwh, 0.01)
}

// --- Outcome 3: plan without a free band ---

// The window-dependent values are absent rather than zero (AC 4.4), but every
// window-independent block still runs and the whole day is rated.
func TestSummarisation_PlanWithoutFreeBand_WholeDayRated(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  gridReadings("2026-04-14", loc, 1000),
	}
	p := summarisationPollerWith(t, ms, noFreeBandPlanLister())

	require.Equal(t, PassResultSuccess, p.runSummarisationPass(context.Background(), "2026-04-14"))
	require.NotNil(t, ms.lastDerived)

	assert.NotEmpty(t, ms.lastDerived.DerivedStatsComputedAt)
	assert.NotEmpty(t, ms.lastDerived.PeakComputedAt)
	assert.NotEmpty(t, ms.lastDerived.BandsComputedAt)
	assert.NotNil(t, ms.lastDerived.SocLow)

	require.Len(t, ms.lastDerived.BandImports, 1)
	assert.Equal(t, "00:00", ms.lastDerived.BandImports[0].Start)
	assert.Equal(t, "24:00", ms.lastDerived.BandImports[0].End)
	require.NotNil(t, ms.lastDerived.PeakGridImportKwh)
	assert.InDelta(t, 24.0, *ms.lastDerived.PeakGridImportKwh, 0.01)

	// No off-peak block: there is no free window to carve one out of.
	require.NotNil(t, ms.lastDerived.DailyUsage)
	for _, b := range ms.lastDerived.DailyUsage.Blocks {
		assert.NotEqual(t, derivedstats.DailyUsageKindOffPeak, b.Kind)
	}
}

// --- Outcome 4: no plan covers the date ---

// Only window-independent work runs. The band sentinel stays unset so an
// explicit backfill can still capture the split once a plan exists — within
// the readings TTL, after which the day is terminal.
func TestSummarisation_NoPlan_RunsWindowIndependentStatsOnly(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  gridReadings("2026-04-14", loc, 1000),
	}
	p := summarisationPollerWith(t, ms, &mockPlanLister{responses: []planListerResponse{{items: nil}}})

	require.Equal(t, PassResultSuccess, p.runSummarisationPass(context.Background(), "2026-04-14"))
	require.NotNil(t, ms.lastDerived)

	assert.NotNil(t, ms.lastDerived.SocLow, "socLow needs no window")
	assert.Nil(t, ms.lastDerived.DailyUsage, "block layout needs a window")
	assert.Empty(t, ms.lastDerived.PeakPeriods, "peak periods need a window")
	assert.NotEmpty(t, ms.lastDerived.DerivedStatsComputedAt, "socLow must be persisted")
	assert.Empty(t, ms.lastDerived.PeakComputedAt, "peak is undefined without a plan")
	assert.Empty(t, ms.lastDerived.BandsComputedAt, "the band sentinel stays unset for backfill")
	assert.Nil(t, ms.lastDerived.BandImports)
}

// --- Sentinel gating across the three groups ---

func TestSummarisation_BandGroupGatedOnItsOwnSentinel(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{
			SysSn: "TEST123", Date: "2026-04-14",
			DerivedStatsComputedAt: "2026-04-15T00:30:00Z",
			PeakComputedAt:         "2026-04-15T00:30:00Z",
		},
		queryReadingsResult: gridReadings("2026-04-14", loc, 1000),
	}
	p := summarisationPollerWith(t, ms, touPlanLister())

	require.Equal(t, PassResultSuccess, p.runSummarisationPass(context.Background(), "2026-04-14"))
	require.NotNil(t, ms.lastDerived)

	assert.NotEmpty(t, ms.lastDerived.BandsComputedAt, "the missing group is computed")
	assert.Empty(t, ms.lastDerived.DerivedStatsComputedAt, "an already-computed group is not rewritten")
	assert.Empty(t, ms.lastDerived.PeakComputedAt)
}

func TestSummarisation_AllThreeSentinelsPresent_SkipsEntirely(t *testing.T) {
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{
			SysSn: "TEST123", Date: "2026-04-14",
			DerivedStatsComputedAt: "2026-04-15T00:30:00Z",
			PeakComputedAt:         "2026-04-15T00:30:00Z",
			BandsComputedAt:        "2026-04-15T00:30:00Z",
		},
	}
	p := summarisationPollerWith(t, ms, touPlanLister())

	assert.Equal(t, PassResultSkippedAlreadyDone, p.runSummarisationPass(context.Background(), "2026-04-14"))
	assert.Zero(t, ms.derivedUpdates)
}

// Mirrors PeakGridImportKwh's contract: a split the integrator cannot produce
// leaves the value absent but still sets the sentinel, so the row is not
// re-attempted every hour for the rest of the day.
func TestSummarisation_BandUsabilityGateLeavesSplitAbsent(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	// Readings start at 06:00, so the 00:00–01:00 and 01:00–06:00 rated
	// segments have nothing to integrate.
	readings := gridReadings("2026-04-14", loc, 1000)[6*60:]
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  readings,
	}
	p := summarisationPollerWith(t, ms, touPlanLister())

	require.Equal(t, PassResultSuccess, p.runSummarisationPass(context.Background(), "2026-04-14"))
	require.NotNil(t, ms.lastDerived)

	assert.Nil(t, ms.lastDerived.BandImports, "a partially known split is unavailable, not partial")
	assert.NotEmpty(t, ms.lastDerived.BandsComputedAt, "the sentinel is still set so the row is not retried")
	assert.Nil(t, ms.lastDerived.PeakGridImportKwh, "peak shares the split's usability gate")
	assert.NotEmpty(t, ms.lastDerived.PeakComputedAt)
}

// AC 3.8: band membership follows wall-clock time, so on the 23-hour DST day
// the bands keep their local boundaries and the energies follow real elapsed
// time. Computing boundaries as midnight-plus-elapsed would shift them an hour.
func TestSummarisation_DSTDayBandsFollowWallClock(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	const date = "2026-10-04" // DST start: 02:00 → 03:00
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: date},
		queryReadingsResult:  gridReadings(date, loc, 1000),
	}
	p := summarisationPollerWith(t, ms, touPlanLister())

	require.Equal(t, PassResultSuccess, p.runSummarisationPass(context.Background(), date))
	require.NotNil(t, ms.lastDerived)
	require.Len(t, ms.lastDerived.BandImports, 4)

	assert.Equal(t, "01:00", ms.lastDerived.BandImports[1].Start)
	assert.Equal(t, "06:00", ms.lastDerived.BandImports[1].End)
	// 01:00–06:00 local spans only four real hours on this day.
	assert.InDelta(t, 4.0, ms.lastDerived.BandImports[1].Kwh, 0.01)
	// 23-hour day less the 5-hour free window.
	require.NotNil(t, ms.lastDerived.PeakGridImportKwh)
	assert.InDelta(t, 18.0, *ms.lastDerived.PeakGridImportKwh, 0.05)
}
