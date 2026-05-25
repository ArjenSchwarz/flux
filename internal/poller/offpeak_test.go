package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testOffpeakCfg() *config.Config {
	return &config.Config{
		Serial:       "TEST123",
		Location:     time.FixedZone("AEST", 10*60*60),
		OffpeakStart: 1 * time.Hour, // 01:00
		OffpeakEnd:   6 * time.Hour, // 06:00
	}
}

// --- Tests for time position detection ---

func TestTimePosition(t *testing.T) {
	cfg := testOffpeakCfg()

	tests := map[string]struct {
		now  time.Time
		want windowPosition
	}{
		"before window": {
			now:  time.Date(2026, 4, 13, 0, 30, 0, 0, cfg.Location),
			want: positionBefore,
		},
		"exactly at start": {
			now:  time.Date(2026, 4, 13, 1, 0, 0, 0, cfg.Location),
			want: positionDuring,
		},
		"during window": {
			now:  time.Date(2026, 4, 13, 3, 0, 0, 0, cfg.Location),
			want: positionDuring,
		},
		"exactly at end": {
			now:  time.Date(2026, 4, 13, 6, 0, 0, 0, cfg.Location),
			want: positionAfter,
		},
		"after window": {
			now:  time.Date(2026, 4, 13, 12, 0, 0, 0, cfg.Location),
			want: positionAfter,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := timePosition(tc.now, cfg.OffpeakStart, cfg.OffpeakEnd)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- Tests for captureSnapshot retry ---

func TestCaptureSnapshot_SuccessOnFirstAttempt(t *testing.T) {
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{Epv: 10.0},
		lastPowerData: &alphaess.PowerData{Soc: 50.0},
	}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: mc, store: &mockStore{}, cfg: cfg, now: time.Now}

	energy, soc, err := o.captureSnapshot(context.Background(), "2026-04-13")
	require.NoError(t, err)
	assert.Equal(t, 10.0, energy.Epv)
	assert.Equal(t, 50.0, soc)
	assert.Equal(t, 1, mc.oneDateEnergyCalls)
	assert.Equal(t, 1, mc.lastPowerCalls)
}

func TestCaptureSnapshot_RetryThenSucceed(t *testing.T) {
	mc := &mockClient{
		lastPowerData: &alphaess.PowerData{Soc: 50.0},
	}
	// Override GetOneDateEnergy to fail twice then succeed.
	energyCallCount := 0
	origClient := &retryMockClient{
		mockClient: mc,
		energyFunc: func() (*alphaess.EnergyData, error) {
			energyCallCount++
			if energyCallCount <= 2 {
				return nil, errors.New("transient error")
			}
			return &alphaess.EnergyData{Epv: 10.0}, nil
		},
	}

	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: origClient, store: &mockStore{}, cfg: cfg, retryDelay: 1 * time.Millisecond, now: time.Now}

	energy, soc, err := o.captureSnapshot(context.Background(), "2026-04-13")
	require.NoError(t, err)
	assert.Equal(t, 10.0, energy.Epv)
	assert.Equal(t, 50.0, soc)
	assert.Equal(t, 3, energyCallCount)
}

func TestCaptureSnapshot_AllRetriesFail(t *testing.T) {
	mc := &mockClient{
		oneDateEnergyErr: errors.New("persistent error"),
	}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: mc, store: &mockStore{}, cfg: cfg, retryDelay: 1 * time.Millisecond, now: time.Now}

	_, _, err := o.captureSnapshot(context.Background(), "2026-04-13")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "3 attempts")
	assert.Equal(t, 3, mc.oneDateEnergyCalls)
}

// --- Tests for start/end flow ---

func TestOffpeak_StartSucceeds_EndFails_DeletesPending(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	ms := &mockStore{}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{Epv: 10.0},
		lastPowerData: &alphaess.PowerData{Soc: 50.0},
	}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: mc, store: ms, cfg: cfg, retryDelay: 1 * time.Millisecond, now: time.Now}

	// Simulate start capture.
	err := o.handleStart(context.Background(), "2026-04-13")
	require.NoError(t, err)

	// Now make API fail for end capture.
	mc.oneDateEnergyErr = errors.New("end snapshot fail")
	err = o.handleEnd(context.Background(), "2026-04-13", nil)
	require.Error(t, err)

	assert.True(t, logContains(buf, "end snapshot fail") || logContains(buf, "3 attempts"))
}

// --- Tests for mid-window startup recovery ---
//
// After T-1341 (Off-Peak From Readings), recoverMidWindow no longer
// rebuilds in-memory state from the pending row — handleEnd integrates the
// readings table at window-close regardless. The recovery method now only
// confirms a pending row exists so the daily flow knows to fire handleEnd
// rather than emit a "no pending row" log+skip.

func TestOffpeak_MidWindowRecovery_PendingRecordExists(t *testing.T) {
	pending := &dynamo.OffpeakItem{
		SysSn: "TEST123", Date: "2026-04-13", Status: dynamo.OffpeakStatusPending,
		StartEpv: 1.0, StartEInput: 2.0, StartEOutput: 0.5,
		StartECharge: 3.0, StartEDischarge: 1.0, StartEGridCharge: 0.5,
		SocStart: 20.0,
	}
	ms := &mockStore{getOffpeakResult: pending}
	mc := &mockClient{}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: mc, store: ms, cfg: cfg, now: time.Now}

	got := o.recoverMidWindow(context.Background(), "2026-04-13")
	require.NotNil(t, got, "pending row present → recovery returns it for handleEnd")
	assert.Equal(t, dynamo.OffpeakStatusPending, got.Status)
	// startSnapshot/socStart are no longer rebuilt from the row —
	// handleEnd reads readings, not in-memory snapshots.
	assert.Nil(t, o.startSnapshot, "in-memory state must not be rebuilt from pending row")
}

func TestOffpeak_MidWindowRecovery_NoRecord(t *testing.T) {
	ms := &mockStore{getOffpeakResult: nil}
	mc := &mockClient{}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: mc, store: ms, cfg: cfg, now: time.Now}

	got := o.recoverMidWindow(context.Background(), "2026-04-13")
	assert.Nil(t, got, "no row → caller logs+skips")
}

func TestOffpeak_MidWindowRecovery_CompleteRow(t *testing.T) {
	// A row already finalised before the restart — recovery treats it as
	// "no pending row" (nothing to do).
	ms := &mockStore{
		getOffpeakResult: &dynamo.OffpeakItem{
			SysSn: "TEST123", Date: "2026-04-13", Status: dynamo.OffpeakStatusComplete,
		},
	}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{store: ms, cfg: cfg, now: time.Now}

	got := o.recoverMidWindow(context.Background(), "2026-04-13")
	assert.Nil(t, got, "complete row → recovery returns nil (no work)")
}

func TestOffpeak_MidWindowRecovery_StoreError(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	ms := &mockStore{getOffpeakErr: errors.New("dynamo query fail")}
	mc := &mockClient{}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: mc, store: ms, cfg: cfg, now: time.Now}

	got := o.recoverMidWindow(context.Background(), "2026-04-13")
	assert.Nil(t, got)
	assert.True(t, logContains(buf, "dynamo query fail"))
}

// --- Tests for positionAfter recovery (T-1341 AC 3.4) ---
//
// New behaviour: when the poller restarts between offpeak-end and 24:00 with
// a pending row, it runs handleEnd's integration path immediately (no
// boundary wait) instead of skipping the day.

func TestPositionAfterRecovery_PendingRow_RunsHandleEndImmediately(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	windowStart := day.Add(cfg.OffpeakStart)
	windowEnd := day.Add(cfg.OffpeakEnd)
	readings := fixtureReadings(windowStart, windowEnd, 0, 2000, -1500)

	pending := &dynamo.OffpeakItem{
		SysSn: "TEST123", Date: "2026-04-13", Status: dynamo.OffpeakStatusPending,
		StartEInput: 2.0, StartECharge: 1.0,
	}
	var captured dynamo.OffpeakItem
	writes := 0
	ms := &mockStore{
		getOffpeakResult: pending,
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
		oneDateEnergy: &alphaess.EnergyData{EInput: 12.0, ECharge: 8.0},
		lastPowerData: &alphaess.PowerData{Soc: 95.0},
	}
	o := &OffpeakScheduler{
		client: mc, store: ms, cfg: cfg, retryDelay: 1 * time.Millisecond,
		// Clock past offpeak-end so the recovery path's "skip wait" branch
		// actually skips the wait — the boundary is in the past.
		now: func() time.Time { return day.Add(cfg.OffpeakEnd + 30*time.Minute) },
	}

	o.recoverAfterWindow(context.Background(), "2026-04-13")
	assert.Equal(t, 1, writes, "handleEnd-style integration must finalise the row")
	assert.Equal(t, dynamo.OffpeakStatusComplete, captured.Status)
	assert.InDelta(t, 10.0, captured.GridUsageKwh, 0.05)
	assert.Greater(t, captured.IntegrationSampleCount, 0)
}

func TestPositionAfterRecovery_NoRow_LogsAndSkips(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	cfg := testOffpeakCfg()
	ms := &mockStore{getOffpeakResult: nil}
	o := &OffpeakScheduler{store: ms, cfg: cfg, now: time.Now}

	o.recoverAfterWindow(context.Background(), "2026-04-13")
	assert.True(t, logContains(buf, "past window") || logContains(buf, "no pending"),
		"absent row → log and skip")
}

func TestPositionAfterRecovery_CompleteRow_LogsAndSkips(t *testing.T) {
	cfg := testOffpeakCfg()
	completeWrites := 0
	ms := &mockStore{
		getOffpeakResult: &dynamo.OffpeakItem{
			SysSn: "TEST123", Date: "2026-04-13", Status: dynamo.OffpeakStatusComplete,
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, _ dynamo.OffpeakItem) error {
			completeWrites++
			return nil
		},
	}
	o := &OffpeakScheduler{store: ms, cfg: cfg, now: time.Now}

	o.recoverAfterWindow(context.Background(), "2026-04-13")
	assert.Equal(t, 0, completeWrites, "already-complete row must not be re-written")
}

// --- Tests for DST-safe wall-clock scheduling ---

func TestWallClockTime_DST(t *testing.T) {
	sydney, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)

	cfg := &config.Config{
		Serial:       "TEST123",
		Location:     sydney,
		OffpeakStart: 1 * time.Hour,
		OffpeakEnd:   6 * time.Hour,
	}

	// During AEDT (UTC+11), 01:00 local = 14:00 UTC previous day.
	aedt := time.Date(2026, 1, 15, 1, 0, 0, 0, sydney)
	pos := timePosition(aedt, cfg.OffpeakStart, cfg.OffpeakEnd)
	assert.Equal(t, positionDuring, pos)

	// During AEST (UTC+10), 01:00 local = 15:00 UTC previous day.
	aest := time.Date(2026, 7, 15, 1, 0, 0, 0, sydney)
	pos = timePosition(aest, cfg.OffpeakStart, cfg.OffpeakEnd)
	assert.Equal(t, positionDuring, pos)
}

// --- retryMockClient wraps mockClient with custom energy function ---

type retryMockClient struct {
	*mockClient
	energyFunc func() (*alphaess.EnergyData, error)
}

func (r *retryMockClient) GetOneDateEnergy(_ context.Context, _ string, _ string) (*alphaess.EnergyData, error) {
	return r.energyFunc()
}

// --- Tests for waitForReadingAtOrAfter (T-1341, AC 3.1) ---

func TestWaitForReadingAtOrAfter_AlreadyPresent(t *testing.T) {
	target := time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)
	ms := &mockStore{
		queryReadingsResult: []dynamo.ReadingItem{
			{Timestamp: target.Unix() + 3},
		},
	}
	o := &OffpeakScheduler{store: ms, cfg: testOffpeakCfg(), now: time.Now}

	start := time.Now()
	found, err := o.waitForReadingAtOrAfter(context.Background(), target, 30*time.Second, 10*time.Millisecond)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, found, "reading already present must return true on first probe")
	assert.Less(t, elapsed, 100*time.Millisecond, "should return immediately, not sleep")
}

func TestWaitForReadingAtOrAfter_ArrivesAfterPolls(t *testing.T) {
	target := time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)
	calls := 0
	ms := &mockStore{
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			calls++
			if calls < 3 {
				// First two polls: only old readings.
				return []dynamo.ReadingItem{{Timestamp: target.Unix() - 5}}, nil
			}
			// Third poll: a reading lands at or after the target.
			return []dynamo.ReadingItem{
				{Timestamp: target.Unix() - 5},
				{Timestamp: target.Unix() + 2},
			}, nil
		},
	}
	o := &OffpeakScheduler{store: ms, cfg: testOffpeakCfg(), now: time.Now}

	found, err := o.waitForReadingAtOrAfter(context.Background(), target, 1*time.Second, 5*time.Millisecond)
	require.NoError(t, err)
	assert.True(t, found, "reading must be found by the third probe")
	assert.GreaterOrEqual(t, calls, 3, "must poll multiple times before finding")
}

func TestWaitForReadingAtOrAfter_BudgetExpires(t *testing.T) {
	target := time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)
	ms := &mockStore{
		queryReadingsResult: []dynamo.ReadingItem{
			{Timestamp: target.Unix() - 30},
			{Timestamp: target.Unix() - 20},
		},
	}
	o := &OffpeakScheduler{store: ms, cfg: testOffpeakCfg(), now: time.Now}

	start := time.Now()
	found, err := o.waitForReadingAtOrAfter(context.Background(), target, 50*time.Millisecond, 10*time.Millisecond)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.False(t, found, "must return false when budget expires before any at-or-after reading lands")
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond, "must respect the budget")
}

func TestWaitForReadingAtOrAfter_StoreErrorPropagates(t *testing.T) {
	target := time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)
	ms := &mockStore{
		queryReadingsErr: errors.New("dynamo down"),
	}
	o := &OffpeakScheduler{store: ms, cfg: testOffpeakCfg(), now: time.Now}

	found, err := o.waitForReadingAtOrAfter(context.Background(), target, 30*time.Second, 10*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dynamo down")
	assert.False(t, found)
}

// --- Drift logging integration (T-1341 AC 6.1) ---
//
// LogOffpeakDrift itself is unit-tested in internal/dynamo/offpeak_drift_test.go.
// Here we only verify handleEnd wires it in: the "offpeak drift" line must be
// emitted, and it must fire before the conditional write so the log is
// visible even when the write is rejected.

func TestHandleEnd_CallsLogOffpeakDrift(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	windowStart := day.Add(cfg.OffpeakStart)
	windowEnd := day.Add(cfg.OffpeakEnd)
	readings := fixtureReadings(windowStart, windowEnd, 0, 1000, 0)
	readings = append(readings, dynamo.ReadingItem{Timestamp: windowEnd.Unix() + 3})

	writes := 0
	driftSeenBeforeWrite := false
	ms := &mockStore{
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, _ dynamo.OffpeakItem) error {
			driftSeenBeforeWrite = logContains(buf, "offpeak drift")
			writes++
			return nil
		},
	}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{EInput: 5.0},
		lastPowerData: &alphaess.PowerData{Soc: 80.0},
	}
	o := &OffpeakScheduler{
		client: mc, store: ms, cfg: cfg, retryDelay: 1 * time.Millisecond,
		now: func() time.Time { return windowEnd.Add(1 * time.Second) },
	}
	o.startSnapshot = &alphaess.EnergyData{EInput: 1.0}
	o.socStart = 20.0

	require.NoError(t, o.handleEnd(context.Background(), "2026-04-13", nil))
	assert.Equal(t, 1, writes)
	assert.True(t, logContains(buf, "offpeak drift"), "handleEnd must emit a drift line")
	assert.True(t, driftSeenBeforeWrite, "drift log must be emitted before the conditional write fires")
}

// --- Tests for handleEnd readings-integration path (T-1341 tasks 9/10) ---

// fixtureReadings synthesises a constant-power off-peak window's worth of
// readings between [start, end) at 10s cadence with the given pgrid/pbat/ppv
// values (watts). Used by the handleEnd tests below.
func fixtureReadings(start, end time.Time, ppv, pgrid, pbat float64) []dynamo.ReadingItem {
	out := make([]dynamo.ReadingItem, 0)
	for t := start; t.Before(end); t = t.Add(10 * time.Second) {
		out = append(out, dynamo.ReadingItem{
			SysSn: "TEST123", Timestamp: t.Unix(),
			Ppv: ppv, Pgrid: pgrid, Pbat: pbat,
		})
	}
	return out
}

func TestHandleEnd_HappyPath_IntegratesAndWrites(t *testing.T) {
	cfg := testOffpeakCfg()
	// Window: 01:00 → 06:00 on 2026-04-13, AEST.
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	windowStart := day.Add(cfg.OffpeakStart)
	windowEnd := day.Add(cfg.OffpeakEnd)
	// Charge at 2000 W on the grid (heavy off-peak charging), 1500 W to the
	// battery, no solar (off-peak window is overnight).
	readings := fixtureReadings(windowStart, windowEnd, 0, 2000, -1500)
	// Add one at-or-after-boundary reading so waitForReadingAtOrAfter sees it.
	readings = append(readings, dynamo.ReadingItem{
		SysSn: "TEST123", Timestamp: windowEnd.Unix() + 3, Pgrid: 0, Pbat: 0, Ppv: 0,
	})

	var captured dynamo.OffpeakItem
	writeCalled := 0
	ms := &mockStore{
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, item dynamo.OffpeakItem) error {
			writeCalled++
			captured = item
			return nil
		},
	}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{
			Epv: 0, EInput: 12.0, EOutput: 0,
			ECharge: 8.0, EDischarge: 1.0, EGridCharge: 7.5,
		},
		lastPowerData: &alphaess.PowerData{Soc: 95.0},
	}

	// Force the scheduler clock so the wait-for-boundary returns immediately.
	o := &OffpeakScheduler{
		client:     mc,
		store:      ms,
		cfg:        cfg,
		retryDelay: 1 * time.Millisecond,
		now:        func() time.Time { return windowEnd.Add(1 * time.Second) },
	}
	// Pre-populate the start snapshot as if handleStart already ran.
	o.startSnapshot = &alphaess.EnergyData{
		Epv: 0, EInput: 2.0, EOutput: 0,
		ECharge: 1.0, EDischarge: 0.5, EGridCharge: 1.0,
	}
	o.socStart = 20.0

	err := o.handleEnd(context.Background(), "2026-04-13", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, writeCalled, "WriteOffpeakIfPendingOrAbsent must be called once")
	assert.Equal(t, dynamo.OffpeakStatusComplete, captured.Status)
	assert.Equal(t, "2026-04-13", captured.Date)
	// 2000 W × 5 hours = 10 kWh grid import.
	assert.InDelta(t, 10.0, captured.GridUsageKwh, 0.05, "grid import ~ 10 kWh over a 5h window at 2 kW")
	// 1500 W × 5 hours = 7.5 kWh battery charge (pbat < 0).
	assert.InDelta(t, 7.5, captured.BatteryChargeKwh, 0.05)
	assert.Equal(t, 0.0, captured.SolarKwh, "no solar in this fixture")
	assert.Equal(t, 0.0, captured.GridExportKwh, "no export")
	assert.Equal(t, 0.0, captured.BatteryDischargeKwh, "no discharge")
	// End snapshot retained for diagnostics (Decision 2 / AC 5.1).
	assert.Equal(t, 12.0, captured.EndEInput, "end snapshot retained as diagnostic")
	assert.Equal(t, 2.0, captured.StartEInput, "start snapshot retained from handleStart")
	// Provenance populated (AC 5.4).
	assert.Greater(t, captured.IntegrationSampleCount, 0, "sample count must be populated")
	assert.Equal(t, 0, captured.IntegrationSkippedPairs, "no gaps in this fixture")
	assert.NotEmpty(t, captured.IntegratedAt, "IntegratedAt must be set")
}

func TestHandleEnd_BoundaryWaitTimeout_StillWritesRow(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	windowStart := day.Add(cfg.OffpeakStart)
	windowEnd := day.Add(cfg.OffpeakEnd)
	// Build readings strictly before windowEnd — wait-for-boundary will time out.
	readings := fixtureReadings(windowStart, windowEnd.Add(-10*time.Second), 0, 1000, -1000)

	var captured dynamo.OffpeakItem
	writeCalled := 0
	ms := &mockStore{
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, item dynamo.OffpeakItem) error {
			writeCalled++
			captured = item
			return nil
		},
	}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{EInput: 5.0, ECharge: 4.0, EGridCharge: 3.0},
		lastPowerData: &alphaess.PowerData{Soc: 90.0},
	}
	o := &OffpeakScheduler{
		client:     mc,
		store:      ms,
		cfg:        cfg,
		retryDelay: 1 * time.Millisecond,
		// Clock pinned BEFORE windowEnd so handleEnd runs the boundary-wait
		// step (which then times out because the fixture has no
		// at-or-after-boundary readings).
		now: func() time.Time { return windowEnd.Add(-1 * time.Second) },
	}
	o.startSnapshot = &alphaess.EnergyData{EInput: 1.0}
	o.socStart = 20.0

	// Use tiny override budget so the test runs fast.
	o.endWaitBudget = 30 * time.Millisecond
	o.endWaitPollInterval = 10 * time.Millisecond

	err := o.handleEnd(context.Background(), "2026-04-13", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, writeCalled, "row must be written even when the boundary wait times out")
	assert.Equal(t, dynamo.OffpeakStatusComplete, captured.Status)
	assert.NotEmpty(t, captured.IntegratedAt)
	assert.Greater(t, captured.GridUsageKwh, 0.0, "integration runs on whatever readings exist")
}

func TestHandleEnd_ConditionalWriteFails_LogsWarn_NoError(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	windowStart := day.Add(cfg.OffpeakStart)
	windowEnd := day.Add(cfg.OffpeakEnd)
	readings := fixtureReadings(windowStart, windowEnd, 0, 1000, 0)
	readings = append(readings, dynamo.ReadingItem{Timestamp: windowEnd.Unix() + 3})

	ms := &mockStore{
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, _ dynamo.OffpeakItem) error {
			return dynamo.ErrOffpeakConditionFailed
		},
	}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{EInput: 5.0},
		lastPowerData: &alphaess.PowerData{Soc: 80.0},
	}
	o := &OffpeakScheduler{
		client: mc, store: ms, cfg: cfg, retryDelay: 1 * time.Millisecond,
		now: func() time.Time { return windowEnd.Add(1 * time.Second) },
	}
	o.startSnapshot = &alphaess.EnergyData{}

	err := o.handleEnd(context.Background(), "2026-04-13", nil)
	require.NoError(t, err, "conditional-failure must be logged, not returned as error")
	assert.True(t, logContains(buf, "conditional"), "warn log should mention the conditional failure")
}

func TestHandleEnd_EmptyReadings_WritesRowWithZeroDeltas(t *testing.T) {
	cfg := testOffpeakCfg()
	day := time.Date(2026, 4, 13, 0, 0, 0, 0, cfg.Location)
	windowEnd := day.Add(cfg.OffpeakEnd)

	var captured dynamo.OffpeakItem
	writeCalled := 0
	ms := &mockStore{
		queryReadingsConsistentFunc: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{}, nil
		},
		writeOffpeakIfPendingOrAbsentFunc: func(_ context.Context, item dynamo.OffpeakItem) error {
			writeCalled++
			captured = item
			return nil
		},
	}
	mc := &mockClient{
		oneDateEnergy: &alphaess.EnergyData{EInput: 5.0},
		lastPowerData: &alphaess.PowerData{Soc: 80.0},
	}
	o := &OffpeakScheduler{
		client: mc, store: ms, cfg: cfg, retryDelay: 1 * time.Millisecond,
		now:                 func() time.Time { return windowEnd.Add(1 * time.Second) },
		endWaitBudget:       30 * time.Millisecond,
		endWaitPollInterval: 10 * time.Millisecond,
	}
	o.startSnapshot = &alphaess.EnergyData{}

	err := o.handleEnd(context.Background(), "2026-04-13", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, writeCalled, "row must still be written when readings are empty")
	assert.Equal(t, 0.0, captured.GridUsageKwh)
	assert.Equal(t, 0.0, captured.SolarKwh)
	assert.Equal(t, 0, captured.IntegrationSampleCount, "empty readings → sample count 0")
	assert.NotEmpty(t, captured.IntegratedAt)
}

func TestWaitForReadingAtOrAfter_ContextCancelAborts(t *testing.T) {
	target := time.Date(2026, 5, 18, 14, 0, 0, 0, time.UTC)
	ms := &mockStore{
		queryReadingsResult: []dynamo.ReadingItem{{Timestamp: target.Unix() - 5}},
	}
	o := &OffpeakScheduler{store: ms, cfg: testOffpeakCfg(), now: time.Now}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay so the first probe completes but the sleep doesn't.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	found, err := o.waitForReadingAtOrAfter(ctx, target, 5*time.Second, 50*time.Millisecond)
	assert.False(t, found, "cancellation must abort with found=false")
	// Either ctx.Err() returned or no error and timeout — accept context error.
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}
