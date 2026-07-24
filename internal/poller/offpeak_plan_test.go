package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPlanItem builds a pricing row whose free band is the given window and
// whose remainder carries a single flat rate — the shape every migrated
// legacy plan has.
func testPlanItem(id, startDate, endDate, freeStart, freeEnd string) dynamo.PricingItem {
	savings := 0.35
	item := dynamo.PricingItem{
		PricingID:            id,
		StartDate:            startDate,
		DefaultRate:          0.35,
		Windows:              []dynamo.PricingWindow{{Start: freeStart, End: freeEnd, Free: true}},
		FeedInRate:           0.05,
		SavingsReferenceRate: &savings,
	}
	if endDate != "" {
		item.EndDate = &endDate
	}
	return item
}

// planSourceWith returns a PlanSource serving exactly these rows.
func planSourceWith(items ...dynamo.PricingItem) *PlanSource {
	return testPlanSource(&mockPlanLister{responses: []planListerResponse{{items: items}}})
}

// openEndedPlanLister serves one open-ended plan whose free band is the
// given window — the shape every pre-feature day was priced under.
func openEndedPlanLister(freeStart, freeEnd string) PlanLister {
	return &mockPlanLister{responses: []planListerResponse{
		{items: []dynamo.PricingItem{testPlanItem("plan", "2000-01-01", "", freeStart, freeEnd)}},
	}}
}

// failingPlanLister is permanently unreachable.
func failingPlanLister() PlanLister {
	return &mockPlanLister{responses: []planListerResponse{{err: errors.New("pricing table unreachable")}}}
}

// failingPlanSource returns a PlanSource whose store is permanently
// unreachable and which has never cached a good result.
func failingPlanSource() *PlanSource {
	return testPlanSource(failingPlanLister())
}

func testScheduler(client APIClient, store dynamo.Store, plans *PlanSource) *OffpeakScheduler {
	return &OffpeakScheduler{
		client: client, store: store, cfg: testOffpeakCfg(), plans: plans,
		retryDelay: time.Millisecond, now: time.Now,
	}
}

// --- Window resolution from the plan covering the date ---

func TestResolveWindow_FromPlanCoveringDate(t *testing.T) {
	cfg := testOffpeakCfg()
	o := testScheduler(&mockClient{}, &mockStore{}, planSourceWith(
		testPlanItem("a", "2026-01-01", "", "10:00", "15:00"),
	))
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)

	win, ok, err := o.resolveWindow(t.Context(), day, "2026-04-13")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "10:00", win.StartHHMM)
	assert.Equal(t, "15:00", win.EndHHMM)
	assert.Equal(t, time.Date(2026, 4, 13, 10, 0, 0, 0, cfg.Location), win.Start)
	assert.Equal(t, time.Date(2026, 4, 13, 15, 0, 0, 0, cfg.Location), win.End)
}

// AC 2.2 / AC 4.2: the switch day belongs to the successor, so the window
// changes at midnight without any manual reconfiguration.
func TestResolveWindow_SwitchDayUsesSuccessorWindow(t *testing.T) {
	cfg := testOffpeakCfg()
	src := planSourceWith(
		testPlanItem("old", "2026-01-01", "2026-08-01", "11:00", "14:00"),
		testPlanItem("new", "2026-08-01", "", "10:00", "15:00"),
	)
	o := testScheduler(&mockClient{}, &mockStore{}, src)

	eve := time.Date(2026, 7, 31, 0, 0, 0, 0, cfg.Location)
	win, ok, err := o.resolveWindow(t.Context(), eve, "2026-07-31")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "11:00", win.StartHHMM, "switch eve is still priced by the predecessor")

	switchDay := time.Date(2026, 8, 1, 0, 0, 0, 0, cfg.Location)
	win, ok, err = o.resolveWindow(t.Context(), switchDay, "2026-08-01")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "10:00", win.StartHHMM, "the switch day belongs to the successor")
	assert.Equal(t, "15:00", win.EndHHMM)
}

func TestResolveWindow_PlanWithoutFreeBand(t *testing.T) {
	cfg := testOffpeakCfg()
	rated := dynamo.PricingItem{
		PricingID: "rated", StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
	}
	o := testScheduler(&mockClient{}, &mockStore{}, planSourceWith(rated))
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)

	_, ok, err := o.resolveWindow(t.Context(), day, "2026-04-13")

	require.NoError(t, err)
	assert.False(t, ok, "no free band → no window to process")
}

func TestResolveWindow_NoPlanCoversDate(t *testing.T) {
	cfg := testOffpeakCfg()
	o := testScheduler(&mockClient{}, &mockStore{}, planSourceWith(
		testPlanItem("a", "2026-05-01", "", "10:00", "15:00"),
	))
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)

	_, ok, err := o.resolveWindow(t.Context(), day, "2026-04-13")

	require.NoError(t, err)
	assert.False(t, ok)
}

// AC 4.6: an unreadable pricing table is transient, not "no plan". It must
// surface as an error the caller retries, never as an absent window.
func TestResolveWindow_PlanReadFailureIsAnError(t *testing.T) {
	cfg := testOffpeakCfg()
	o := testScheduler(&mockClient{}, &mockStore{}, failingPlanSource())
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)

	_, ok, err := o.resolveWindow(t.Context(), day, "2026-04-13")

	require.Error(t, err)
	assert.False(t, ok)
}

// The window can only be resolved on a DST day by wall clock; adding elapsed
// minutes to midnight would be an hour off on the 23-hour day.
func TestResolveWindow_DSTDayUsesWallClock(t *testing.T) {
	sydney, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)

	o := testScheduler(&mockClient{}, &mockStore{}, planSourceWith(
		testPlanItem("a", "2026-01-01", "", "10:00", "15:00"),
	))
	o.cfg.Location = sydney

	// 2026-10-04 is the Sydney DST start (02:00 → 03:00), a 23-hour day.
	day := time.Date(2026, 10, 4, 0, 0, 0, 0, sydney)
	win, ok, err := o.resolveWindow(t.Context(), day, "2026-10-04")

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 10, win.Start.Hour(), "window start stays at local 10:00")
	assert.Equal(t, 15, win.End.Hour(), "window end stays at local 15:00")
}

// --- Position relative to the resolved window ---

func TestPositionFor(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	win := offpeakWindow{
		Start:     time.Date(2026, 4, 13, 1, 0, 0, 0, cfg.Location),
		End:       time.Date(2026, 4, 13, 6, 0, 0, 0, cfg.Location),
		StartHHMM: "01:00", EndHHMM: "06:00",
	}

	tests := map[string]struct {
		now  time.Time
		want windowPosition
	}{
		"before window":    {now: day.Add(30 * time.Minute), want: positionBefore},
		"exactly at start": {now: win.Start, want: positionDuring},
		"during window":    {now: day.Add(3 * time.Hour), want: positionDuring},
		"exactly at end":   {now: win.End, want: positionAfter},
		"after window":     {now: day.Add(12 * time.Hour), want: positionAfter},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, positionFor(tc.now, win))
		})
	}
}

// --- Window geometry snapshotted onto the row ---

// testWindow is the 01:00–06:00 window the offpeak fixtures use, as an
// already-resolved offpeakWindow for the given day.
func testWindow(day time.Time) offpeakWindow {
	return offpeakWindow{
		Start:     time.Date(day.Year(), day.Month(), day.Day(), 1, 0, 0, 0, day.Location()),
		End:       time.Date(day.Year(), day.Month(), day.Day(), 6, 0, 0, 0, day.Location()),
		StartHHMM: "01:00",
		EndHHMM:   "06:00",
	}
}

func TestHandleEnd_SnapshotsWindowGeometry(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	win := offpeakWindow{
		Start:     time.Date(2026, 4, 13, 10, 0, 0, 0, cfg.Location),
		End:       time.Date(2026, 4, 13, 15, 0, 0, 0, cfg.Location),
		StartHHMM: "10:00", EndHHMM: "15:00",
	}
	readings := fixtureReadings(win.Start, win.End, 0, 1000, -1000)

	var captured dynamo.OffpeakItem
	ms := &mockStore{
		queryReadingsConsistentFunc: func(_ context.Context, _ string, from, to int64) ([]dynamo.ReadingItem, error) {
			assert.Equal(t, win.Start.Unix(), from, "integration must run over the plan's window")
			assert.Equal(t, win.End.Unix(), to)
			return readings, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, item dynamo.OffpeakItem) error {
			captured = item
			return nil
		},
	}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{EInput: 8.0},
		lastPowerData: &alphaess.PowerData{Soc: 90.0},
	}
	o := testScheduler(mc, ms, planSourceWith())
	o.now = func() time.Time { return win.End.Add(time.Second) }
	o.startSnapshot = &alphaess.EnergyData{EInput: 2.0}
	o.socStart = 20.0
	_ = day

	require.NoError(t, o.handleEnd(t.Context(), "2026-04-13", nil, win))

	assert.Equal(t, "10:00", captured.WindowStart)
	assert.Equal(t, "15:00", captured.WindowEnd)
}

// --- Q36: readings-only finalisation ---

// A plan-read failure at window start means handleStart never ran, so there
// is neither in-memory state nor a pending row. The integration never needed
// either (offpeak-from-readings Decision 2), so a plan load that succeeds
// later in the day must still finalise the window.
func TestHandleEnd_FinalisesFromReadingsWithoutStartSnapshot(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	win := testWindow(day)
	readings := fixtureReadings(win.Start, win.End, 0, 2000, -1500)

	var captured dynamo.OffpeakItem
	writes := 0
	ms := &mockStore{
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, item dynamo.OffpeakItem) error {
			writes++
			captured = item
			return nil
		},
	}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{EInput: 12.0},
		lastPowerData: &alphaess.PowerData{Soc: 95.0},
	}
	o := testScheduler(mc, ms, planSourceWith())
	o.now = func() time.Time { return win.End.Add(time.Second) }

	require.NoError(t, o.handleEnd(t.Context(), "2026-04-13", nil, win))

	assert.Equal(t, 1, writes, "no pending row and no snapshot must not forfeit the day")
	assert.Equal(t, dynamo.OffpeakStatusComplete, captured.Status)
	assert.InDelta(t, 10.0, captured.GridUsageKwh, 0.05)
	assert.Zero(t, captured.StartEInput, "no start snapshot to record")
	assert.Zero(t, captured.BatteryDeltaPercent, "SoC delta is unknown without a start snapshot")
}

func TestRecoverAfterWindow_NoRow_FinalisesFromReadings(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	win := testWindow(day)
	readings := fixtureReadings(win.Start, win.End, 0, 2000, -1500)

	writes := 0
	ms := &mockStore{
		getOffpeakResult: nil,
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, _ dynamo.OffpeakItem) error {
			writes++
			return nil
		},
	}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{EInput: 12.0},
		lastPowerData: &alphaess.PowerData{Soc: 95.0},
	}
	o := testScheduler(mc, ms, planSourceWith())
	o.now = func() time.Time { return win.End.Add(30 * time.Minute) }

	o.recoverAfterWindow(t.Context(), "2026-04-13", win)

	assert.Equal(t, 1, writes, "an absent row is repaired from readings, not skipped (Q36)")
}

func TestRecoverAfterWindow_CompleteRow_StillSkips(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	win := testWindow(day)

	writes := 0
	ms := &mockStore{
		getOffpeakResult: &dynamo.OffpeakItem{
			SysSn: "TEST123", Date: "2026-04-13", Status: dynamo.OffpeakStatusComplete,
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, _ dynamo.OffpeakItem) error {
			writes++
			return nil
		},
	}
	o := testScheduler(&mockClient{}, ms, planSourceWith())
	o.now = func() time.Time { return win.End.Add(30 * time.Minute) }

	o.recoverAfterWindow(t.Context(), "2026-04-13", win)

	assert.Equal(t, 0, writes, "an already-finalised row must not be re-written")
}

// --- Day cycle ---

// A mid-window restart with no pending row still finalises: the pending row
// carries diagnostics only, so its absence is not a reason to drop the day.
func TestRunWindow_MidWindowWithoutPendingRow_StillFinalises(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	win := testWindow(day)
	readings := fixtureReadings(win.Start, win.End, 0, 2000, -1500)
	readings = append(readings, dynamo.ReadingItem{SysSn: "TEST123", Timestamp: win.End.Unix()})

	writes := 0
	ms := &mockStore{
		getOffpeakResult: nil,
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, _ dynamo.OffpeakItem) error {
			writes++
			return nil
		},
	}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{EInput: 12.0},
		lastPowerData: &alphaess.PowerData{Soc: 95.0},
	}
	o := testScheduler(mc, ms, planSourceWith())
	o.now = func() time.Time { return win.Start.Add(2 * time.Hour) }

	require.True(t, o.runWindow(t.Context(), t.Context(), "2026-04-13", win))
	assert.Equal(t, 1, writes)
}

// A failed start snapshot no longer skips the day: the window still closes
// from readings (Q36).
func TestRunWindow_StartSnapshotFailure_StillFinalisesAtEnd(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	win := testWindow(day)
	readings := fixtureReadings(win.Start, win.End, 0, 2000, -1500)
	readings = append(readings, dynamo.ReadingItem{SysSn: "TEST123", Timestamp: win.End.Unix()})

	writes := 0
	ms := &mockStore{
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, _ dynamo.OffpeakItem) error {
			writes++
			return nil
		},
	}
	// First snapshot call (handleStart) fails, later ones succeed.
	calls := 0
	mc := &retryMockClient{
		mockClient: &mockClient{lastPowerData: &alphaess.PowerData{Soc: 95.0}},
		energyFunc: func() (*alphaess.EnergyData, error) {
			calls++
			if calls <= snapshotRetries {
				return nil, errors.New("alphaess unavailable")
			}
			return &alphaess.EnergyData{EInput: 12.0}, nil
		},
	}
	o := testScheduler(mc, ms, planSourceWith())
	o.now = func() time.Time { return day }

	require.True(t, o.runWindow(t.Context(), t.Context(), "2026-04-13", win))
	assert.Equal(t, 1, writes, "window end must still finalise from readings")
}
