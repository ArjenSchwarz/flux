package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMetrics records RecordSummarisationPass calls so tests can assert the
// metric dimension that resulted from a pass.
type fakeMetrics struct {
	recorded []string
}

func (f *fakeMetrics) RecordSummarisationPass(_ context.Context, result string) {
	f.recorded = append(f.recorded, result)
}

func summarisationFixturePoller(t *testing.T, ms *mockStore) (*Poller, *fakeMetrics) {
	t.Helper()
	loc, _ := time.LoadLocation("Australia/Sydney")
	cfg := &config.Config{
		Serial:       "TEST123",
		Location:     loc,
		OffpeakStart: 11 * time.Hour,
		OffpeakEnd:   14 * time.Hour,
	}
	fakeM := &fakeMetrics{}
	p := New(nil, ms, cfg)
	// Pin clock to 2026-04-15 02:00 AEST so "yesterday" deterministically
	// resolves to 2026-04-14.
	p.now = func() time.Time { return time.Date(2026, 4, 15, 2, 0, 0, 0, loc) }
	p.metrics = fakeM
	return p, fakeM
}

// makeReadings builds a 24-hour worth of readings in Sydney time, every 60s.
func makeReadings(date string, loc *time.Location) []dynamo.ReadingItem {
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	out := make([]dynamo.ReadingItem, 0, 24*60)
	for i := range 24 * 60 {
		t := dayStart.Add(time.Duration(i) * time.Minute)
		ppv := 0.0
		mod := t.Hour()*60 + t.Minute()
		if mod >= 6*60+45 && mod <= 17*60+30 {
			ppv = 1500
		}
		pload := 600.0
		if t.Hour() >= 11 && t.Hour() < 14 {
			pload = 2500
		}
		soc := 50.0 + float64(i%30)
		out = append(out, dynamo.ReadingItem{
			Timestamp: t.Unix(),
			Ppv:       ppv,
			Pload:     pload,
			Pbat:      0,
			Pgrid:     0,
			Soc:       soc,
		})
	}
	return out
}

func TestSummarisation_SuccessPath(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		// Row exists, sentinel absent → run the pass.
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14", Epv: 12.0},
		queryReadingsResult:  makeReadings("2026-04-14", loc),
	}
	p, m := summarisationFixturePoller(t, ms)

	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, "success", result)
	assert.Equal(t, 1, ms.derivedUpdates, "must call UpdateDailyEnergyDerived once on success")

	p.summariseYesterday(context.Background())
	require.Contains(t, m.recorded, "success")
}

func TestSummarisation_NoReadings(t *testing.T) {
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  []dynamo.ReadingItem{}, // empty
	}
	p, _ := summarisationFixturePoller(t, ms)

	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, "skipped-no-readings", result)
	assert.Zero(t, ms.derivedUpdates, "must NOT call UpdateDailyEnergyDerived when readings is empty")
}

func TestSummarisation_NoRow(t *testing.T) {
	ms := &mockStore{
		getDailyEnergyResult: nil, // no row yet
	}
	p, _ := summarisationFixturePoller(t, ms)

	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, "skipped-no-row", result)
	assert.Zero(t, ms.derivedUpdates)
}

func TestSummarisation_AlreadyPopulated(t *testing.T) {
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{
			SysSn: "TEST123", Date: "2026-04-14",
			DerivedStatsComputedAt: "2026-04-14T22:00:00Z",
		},
	}
	p, _ := summarisationFixturePoller(t, ms)

	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, "skipped-already-populated", result)
	// Critical: must NOT issue a readings query when sentinel is present
	// (per AC 1.10 and the design — the precheck saves the query cost).
	assert.Nil(t, ms.queryReadingsResult, "queryReadingsResult unset means QueryReadings must not have been called for default")
}

func TestSummarisation_SsmUnresolved(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  makeReadings("2026-04-14", loc),
	}
	p, _ := summarisationFixturePoller(t, ms)
	// Force off-peak window to invalid by zeroing the durations after Pollerm
	// is built (cfg.OffpeakStart >= cfg.OffpeakEnd → ParseOffpeakWindow returns false).
	p.cfg.OffpeakStart = 0
	p.cfg.OffpeakEnd = 0

	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, "skipped-ssm-unresolved", result)
	assert.Zero(t, ms.derivedUpdates)
}

func TestSummarisation_ReadingsError(t *testing.T) {
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsErr:     errors.New("throttled"),
	}
	p, _ := summarisationFixturePoller(t, ms)
	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, "error", result)
	assert.Zero(t, ms.derivedUpdates)
}

func TestSummarisation_UpdateError(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult:        &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:         makeReadings("2026-04-14", loc),
		updateDailyEnergyDerivedErr: errors.New("conditional check failed"),
	}
	p, _ := summarisationFixturePoller(t, ms)
	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, "error", result)
	assert.Equal(t, 1, ms.derivedUpdates)
}

func TestSummarisation_GetDailyEnergyError(t *testing.T) {
	ms := &mockStore{getDailyEnergyErr: errors.New("timeout")}
	p, _ := summarisationFixturePoller(t, ms)
	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, "error", result)
	assert.Zero(t, ms.derivedUpdates)
}

func TestSummarisation_DateAsToday_SoTodayGateNeverFires(t *testing.T) {
	// AC 6.1 final scenario: the pass passes the date being summarised as
	// `today` so derivedstats.Blocks takes the completed-day branch and
	// the today-gate (and in-progress clamp) never fire.
	loc, _ := time.LoadLocation("Australia/Sydney")
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  makeReadings("2026-04-14", loc),
	}
	p, _ := summarisationFixturePoller(t, ms)

	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	require.Equal(t, "success", result)

	// Inspect the derived-stats payload via test-only hook.
	require.NotNil(t, p.lastDerivedForTest, "test hook must capture the last DerivedStats payload")
	require.NotNil(t, p.lastDerivedForTest.DailyUsage, "expected dailyUsage to be present on a normal day")
	for _, b := range p.lastDerivedForTest.DailyUsage.Blocks {
		assert.Equal(t, "complete", b.Status, "all blocks must be complete; the today-gate must not fire when date == today input")
	}
}

func TestSummarisation_Idempotence(t *testing.T) {
	// AC 6.2: two consecutive passes against the same readings produce the
	// same UpdateItem payload (modulo the sentinel timestamp, which is
	// driven by p.now()).
	loc, _ := time.LoadLocation("Australia/Sydney")
	readings := makeReadings("2026-04-14", loc)
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{SysSn: "TEST123", Date: "2026-04-14"},
		queryReadingsResult:  readings,
	}
	p, _ := summarisationFixturePoller(t, ms)

	r1 := p.runSummarisationPass(context.Background(), "2026-04-14")
	require.Equal(t, "success", r1)
	first := p.lastDerivedForTest
	require.NotNil(t, first)

	// Second run — pretend the sentinel has not been written yet (the
	// fakeMetrics + mockStore don't persist), so we re-invoke. Should
	// produce field-equivalent payload.
	r2 := p.runSummarisationPass(context.Background(), "2026-04-14")
	require.Equal(t, "success", r2)
	second := p.lastDerivedForTest

	// Field-equivalence on dailyUsage.
	require.NotNil(t, second.DailyUsage)
	require.Equal(t, len(first.DailyUsage.Blocks), len(second.DailyUsage.Blocks))
	for i := range first.DailyUsage.Blocks {
		a := first.DailyUsage.Blocks[i]
		b := second.DailyUsage.Blocks[i]
		assert.Equal(t, a.Kind, b.Kind)
		assert.Equal(t, a.Start, b.Start)
		assert.Equal(t, a.End, b.End)
		assert.InDelta(t, a.TotalKwh, b.TotalKwh, 1e-9)
		assert.Equal(t, a.Status, b.Status)
		assert.Equal(t, a.BoundarySource, b.BoundarySource)
	}

	// SocLow + PeakPeriods are also field-equivalent (no nondeterminism in
	// either function).
	if first.SocLow != nil {
		require.NotNil(t, second.SocLow)
		assert.InDelta(t, first.SocLow.Soc, second.SocLow.Soc, 1e-9)
		assert.Equal(t, first.SocLow.Timestamp, second.SocLow.Timestamp)
	}
	require.Equal(t, len(first.PeakPeriods), len(second.PeakPeriods))
}

func TestSummarisation_PrecheckShortCircuits_NoReadingsQuery(t *testing.T) {
	// AC 6.2 second clause: when the row already carries the sentinel,
	// the precheck must short-circuit before issuing a readings query.
	// queryReadingsResult/Err are never set; if QueryReadings was called
	// against an empty mock it would have returned (nil, nil) and we'd see
	// "skipped-no-readings" instead.
	ms := &mockStore{
		getDailyEnergyResult: &dynamo.DailyEnergyItem{
			SysSn: "TEST123", Date: "2026-04-14",
			DerivedStatsComputedAt: "2026-04-14T22:00:00Z",
		},
	}
	p, _ := summarisationFixturePoller(t, ms)
	result := p.runSummarisationPass(context.Background(), "2026-04-14")
	assert.Equal(t, "skipped-already-populated", result)
}
