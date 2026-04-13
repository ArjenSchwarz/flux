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

// --- Tests for computeOffpeakDeltas ---

func TestComputeOffpeakDeltas(t *testing.T) {
	tests := map[string]struct {
		start    *alphaess.EnergyData
		end      *alphaess.EnergyData
		socStart float64
		socEnd   float64
		want     dynamo.OffpeakItem
	}{
		"normal deltas": {
			start: &alphaess.EnergyData{
				Epv: 1.0, EInput: 2.0, EOutput: 0.5,
				ECharge: 3.0, EDischarge: 1.0, EGridCharge: 0.5,
			},
			end: &alphaess.EnergyData{
				Epv: 1.5, EInput: 5.0, EOutput: 1.0,
				ECharge: 6.0, EDischarge: 2.0, EGridCharge: 1.5,
			},
			socStart: 20.0,
			socEnd:   80.0,
			want: dynamo.OffpeakItem{
				SysSn: "TEST123", Date: "2026-04-13", Status: dynamo.OffpeakStatusComplete,
				StartEpv: 1.0, StartEInput: 2.0, StartEOutput: 0.5,
				StartECharge: 3.0, StartEDischarge: 1.0, StartEGridCharge: 0.5,
				SocStart: 20.0,
				EndEpv:   1.5, EndEInput: 5.0, EndEOutput: 1.0,
				EndECharge: 6.0, EndEDischarge: 2.0, EndEGridCharge: 1.5,
				SocEnd:              80.0,
				SolarKwh:            0.5,
				GridUsageKwh:        3.0,
				GridExportKwh:       0.5,
				BatteryChargeKwh:    3.0,
				BatteryDischargeKwh: 1.0,
				BatteryDeltaPercent: 60.0,
			},
		},
		"zero deltas": {
			start:    &alphaess.EnergyData{Epv: 5.0, EInput: 5.0, EOutput: 5.0, ECharge: 5.0, EDischarge: 5.0, EGridCharge: 5.0},
			end:      &alphaess.EnergyData{Epv: 5.0, EInput: 5.0, EOutput: 5.0, ECharge: 5.0, EDischarge: 5.0, EGridCharge: 5.0},
			socStart: 50.0,
			socEnd:   50.0,
			want: dynamo.OffpeakItem{
				SysSn: "TEST123", Date: "2026-04-13", Status: dynamo.OffpeakStatusComplete,
				StartEpv: 5.0, StartEInput: 5.0, StartEOutput: 5.0,
				StartECharge: 5.0, StartEDischarge: 5.0, StartEGridCharge: 5.0,
				SocStart: 50.0,
				EndEpv:   5.0, EndEInput: 5.0, EndEOutput: 5.0,
				EndECharge: 5.0, EndEDischarge: 5.0, EndEGridCharge: 5.0,
				SocEnd: 50.0,
			},
		},
		"negative battery delta (discharge)": {
			start:    &alphaess.EnergyData{},
			end:      &alphaess.EnergyData{},
			socStart: 80.0,
			socEnd:   30.0,
			want: dynamo.OffpeakItem{
				SysSn: "TEST123", Date: "2026-04-13", Status: dynamo.OffpeakStatusComplete,
				SocStart: 80.0, SocEnd: 30.0,
				BatteryDeltaPercent: -50.0,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := computeOffpeakDeltas("TEST123", "2026-04-13", tc.start, tc.end, tc.socStart, tc.socEnd)
			assert.Equal(t, tc.want, got)
		})
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
	callCount := 0
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
	_ = callCount

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
	assert.True(t, o.hasStart)

	// Now make API fail for end capture.
	mc.oneDateEnergyErr = errors.New("end snapshot fail")
	err = o.handleEnd(context.Background(), "2026-04-13")
	require.Error(t, err)

	assert.True(t, logContains(buf, "end snapshot fail") || logContains(buf, "3 attempts"))
}

// --- Tests for mid-window startup recovery ---

func TestOffpeak_MidWindowRecovery_PendingRecordExists(t *testing.T) {
	ms := &mockStore{
		getOffpeakResult: &dynamo.OffpeakItem{
			SysSn: "TEST123", Date: "2026-04-13", Status: dynamo.OffpeakStatusPending,
			StartEpv: 1.0, StartEInput: 2.0, StartEOutput: 0.5,
			StartECharge: 3.0, StartEDischarge: 1.0, StartEGridCharge: 0.5,
			SocStart: 20.0,
		},
	}
	mc := &mockClient{}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: mc, store: ms, cfg: cfg, now: time.Now}

	err := o.recoverMidWindow(context.Background(), "2026-04-13")
	require.NoError(t, err)
	assert.True(t, o.hasStart)
	assert.Equal(t, 20.0, o.socStart)
	assert.Equal(t, 1.0, o.startSnapshot.Epv)
}

func TestOffpeak_MidWindowRecovery_NoRecord(t *testing.T) {
	ms := &mockStore{getOffpeakResult: nil}
	mc := &mockClient{}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: mc, store: ms, cfg: cfg, now: time.Now}

	err := o.recoverMidWindow(context.Background(), "2026-04-13")
	require.NoError(t, err)
	assert.False(t, o.hasStart)
}

func TestOffpeak_MidWindowRecovery_StoreError(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	ms := &mockStore{getOffpeakErr: errors.New("dynamo query fail")}
	mc := &mockClient{}
	cfg := testOffpeakCfg()
	o := &OffpeakScheduler{client: mc, store: ms, cfg: cfg, now: time.Now}

	err := o.recoverMidWindow(context.Background(), "2026-04-13")
	require.NoError(t, err) // Should not return error, just log and skip.
	assert.False(t, o.hasStart)
	assert.True(t, logContains(buf, "dynamo query fail"))
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
