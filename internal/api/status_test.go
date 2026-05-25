package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedNow returns a deterministic "now" for status tests.
// 2026-04-15 10:00:00 AEST (UTC+10) = 2026-04-15 00:00:00 UTC.
func fixedNow() time.Time {
	return time.Date(2026, 4, 15, 10, 0, 0, 0, sydneyTZ)
}

// statusRequest builds an authenticated GET /status request.
func statusRequest() events.LambdaFunctionURLRequest {
	return makeRequest("GET", "/status", "Bearer "+testToken)
}

// parseStatusResponse unmarshals the response body into a StatusResponse.
func parseStatusResponse(t *testing.T, resp events.LambdaFunctionURLResponse) StatusResponse {
	t.Helper()
	var sr StatusResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &sr))
	return sr
}

func TestHandleStatusAllDataPresent(t *testing.T) {
	// Use an early-morning "now" (06:00 AEST) so the linear cutoff
	// extrapolation lands before the 11:00 off-peak window under the T-827
	// filter. At Pbat ~1.4 kW and SOC 48% against a 13.34 kWh capacity the
	// projected cutoff is ~3.6 h out → ~09:37, well before 11:00.
	now := time.Date(2026, 4, 15, 6, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, serial string, from, to int64) ([]dynamo.ReadingItem, error) {
			assert.Equal(t, testSerial, serial)
			// Sydney midnight today = nowUnix - 21600 (06:00 AEST → 00:00 AEST is 6h).
			// Pre-midnight readings must be excluded from low24h.
			return []dynamo.ReadingItem{
				// Yesterday morning (~24h ago) — pre-midnight, must be excluded.
				{Timestamp: nowUnix - 86000, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50, Soc: 15},
				// Yesterday evening (~14h ago) — pre-midnight, must be excluded.
				{Timestamp: nowUnix - 50000, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50, Soc: 20},
				// Today ~02:00 AEST (~4h ago) — in-window low for low24h.
				{Timestamp: nowUnix - 14400, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50, Soc: 25},
				// 15min ago.
				{Timestamp: nowUnix - 800, Ppv: 500, Pload: 1000, Pbat: 800, Pgrid: 100, Soc: 55},
				// Within 60s — 3 consecutive readings with pgrid > 500.
				{Timestamp: nowUnix - 30, Ppv: 1000, Pload: 1500, Pbat: 1200, Pgrid: 600, Soc: 50},
				{Timestamp: nowUnix - 20, Ppv: 1100, Pload: 1600, Pbat: 1300, Pgrid: 700, Soc: 49},
				{Timestamp: nowUnix - 10, Ppv: 1200, Pload: 1700, Pbat: 1400, Pgrid: 800, Soc: 48},
			}, nil
		},
		getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
		},
		getOffpeakFn: func(_ context.Context, serial, date string) (*dynamo.OffpeakItem, error) {
			return &dynamo.OffpeakItem{
				SysSn: serial, Date: date, Status: dynamo.OffpeakStatusComplete,
				GridUsageKwh: 2.5, SolarKwh: 5.0, BatteryChargeKwh: 1.0,
				BatteryDischargeKwh: 0.5, GridExportKwh: 0.3, BatteryDeltaPercent: 10.0,
			}, nil
		},
		getDailyEnergyFn: func(_ context.Context, serial, date string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{
				SysSn: serial, Date: date,
				Epv: 12.345, EInput: 3.456, EOutput: 1.234, ECharge: 5.678, EDischarge: 4.567,
			}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)

	// Live: last reading with rounding.
	require.NotNil(t, sr.Live)
	assert.Equal(t, roundPower(1200), sr.Live.Ppv)
	assert.Equal(t, roundPower(1700), sr.Live.Pload)
	assert.Equal(t, roundPower(1400), sr.Live.Pbat)
	assert.Equal(t, roundPower(800), sr.Live.Pgrid)
	assert.Equal(t, roundPower(48), sr.Live.Soc)
	assert.True(t, sr.Live.PgridSustained)

	// Battery.
	require.NotNil(t, sr.Battery)
	assert.Equal(t, 13.34, sr.Battery.CapacityKwh)
	assert.Equal(t, 5, sr.Battery.CutoffPercent)
	require.NotNil(t, sr.Battery.EstimatedCutoff, "should have cutoff since discharging")
	require.NotNil(t, sr.Battery.Low24h)
	assert.Equal(t, roundPower(25), sr.Battery.Low24h.Soc)

	// Rolling 15min: at least 2 readings in 15min window.
	require.NotNil(t, sr.Rolling15m)

	// Offpeak: complete, so delta fields populated.
	require.NotNil(t, sr.Offpeak)
	assert.Equal(t, "11:00", sr.Offpeak.WindowStart)
	assert.Equal(t, "14:00", sr.Offpeak.WindowEnd)
	assert.Equal(t, dynamo.OffpeakStatusComplete, sr.Offpeak.Status)
	require.NotNil(t, sr.Offpeak.GridUsageKwh)
	assert.Equal(t, 2.5, *sr.Offpeak.GridUsageKwh)

	// Today energy.
	require.NotNil(t, sr.TodayEnergy)
	assert.Equal(t, derivedstats.RoundEnergy(12.345), sr.TodayEnergy.Epv)
}

// T-1274 regression: when AlphaESS stops returning fresh data overnight, the
// poller stops writing new readings and the most recent reading ages. /status
// must not surface that aged reading as if it were live — the iOS Dashboard
// has no other way to know the data is stale and would render an hours-old
// snapshot as current. Threshold tracks `liveDataStalenessThreshold`.
func TestHandleStatusStaleLatestReading_OmitsLive(t *testing.T) {
	now := fixedNow()
	nowUnix := now.Unix()
	// Latest reading is 2 hours old — well past any sane "live" threshold.
	staleUnix := nowUnix - 2*3600
	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				{Timestamp: staleUnix, Ppv: 0, Pload: 200, Pbat: 250, Pgrid: 150, Soc: 65},
			}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	assert.Nil(t, sr.Live, "live should be omitted when latest reading is stale")
	require.NotNil(t, sr.Battery)
	assert.Nil(t, sr.Battery.EstimatedCutoff, "cutoff should be omitted when based on a stale reading")
}

// Boundary: a reading exactly at the staleness threshold is still treated as
// fresh; one second past it is not.
func TestHandleStatusStalenessBoundary(t *testing.T) {
	now := fixedNow()
	nowUnix := now.Unix()

	cases := map[string]struct {
		ageSec   int64
		wantLive bool
	}{
		"fresh":              {ageSec: 10, wantLive: true},
		"at threshold":       {ageSec: 90, wantLive: true},
		"one past threshold": {ageSec: 91, wantLive: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mr := &mockReader{
				queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
					return []dynamo.ReadingItem{
						{Timestamp: nowUnix - tc.ageSec, Ppv: 0, Pload: 200, Pbat: 50, Pgrid: 150, Soc: 75},
					}, nil
				},
			}

			h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), statusRequest())
			require.NoError(t, err)
			sr := parseStatusResponse(t, resp)

			if tc.wantLive {
				assert.NotNil(t, sr.Live, "expected Live populated at age=%ds", tc.ageSec)
			} else {
				assert.Nil(t, sr.Live, "expected Live omitted at age=%ds", tc.ageSec)
			}
		})
	}
}

func TestHandleStatusNoReadings(t *testing.T) {
	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return fixedNow() }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	assert.Nil(t, sr.Live, "live should be null when no readings")
	assert.Nil(t, sr.Rolling15m, "rolling15min should be null when no readings")
	require.NotNil(t, sr.Battery)
	assert.Nil(t, sr.Battery.Low24h, "low24h should be null when no readings")
}

// TestHandleStatusLow24hNoReadingsToday verifies that low24h is nil when
// readings exist but all of them predate Sydney midnight on now's date — the
// brief case just after midnight before the first reading of the new day
// arrives.
func TestHandleStatusLow24hNoReadingsToday(t *testing.T) {
	// now = 00:00:30 AEST on 2026-04-15. Sydney midnight today is 30 seconds
	// before. All readings are placed before that boundary.
	now := time.Date(2026, 4, 15, 0, 0, 30, 0, sydneyTZ)
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				// Yesterday afternoon — pre-midnight today.
				{Timestamp: nowUnix - 50000, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50, Soc: 42},
				// Yesterday a minute before midnight — still pre-midnight today.
				{Timestamp: nowUnix - 90, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50, Soc: 38},
			}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.Battery)
	assert.Nil(t, sr.Battery.Low24h, "low24h should be null when no readings exist since Sydney midnight")
}

func TestHandleStatusOffpeakPendingBeforeWindowNoSplit(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &dynamo.OffpeakItem{Status: dynamo.OffpeakStatusPending}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)

	require.NotNil(t, sr.Offpeak)
	assert.Equal(t, "11:00", sr.Offpeak.WindowStart)
	assert.Equal(t, "14:00", sr.Offpeak.WindowEnd)
	assert.Empty(t, sr.Offpeak.Status, "no status when pending row is before window opens (AC 4.3)")
	assert.Nil(t, sr.Offpeak.GridUsageKwh, "delta fields should be null before window opens (AC 4.3)")
	assert.Nil(t, sr.Offpeak.SolarKwh)
	assert.Nil(t, sr.Offpeak.BatteryChargeKwh)
	assert.Nil(t, sr.Offpeak.BatteryDischargeKwh)
	assert.Nil(t, sr.Offpeak.GridExportKwh)
	assert.Nil(t, sr.Offpeak.BatteryDeltaPercent)
}

func TestHandleStatusOffpeakInProgress(t *testing.T) {
	// 11:30 AEST — 30 minutes into the 11:00-14:00 window. liveOffpeakDeltas
	// integrates the readings over [11:00, 11:30). Constant 3600 W grid
	// import → 1.8 kWh.
	loc := sydneyTZ
	now := time.Date(2026, 4, 15, 11, 30, 0, 0, loc)
	opStart := time.Date(2026, 4, 15, 11, 0, 0, 0, loc)

	var readings []dynamo.ReadingItem
	for ts := opStart.Unix(); ts <= now.Unix(); ts += 10 {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: ts,
			Pgrid:     3600,
		})
	}

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			// StartE* / EndE* values must NOT be read by the live path (AC 5.3).
			return &dynamo.OffpeakItem{
				Status:          dynamo.OffpeakStatusPending,
				StartEpv:        999.0,
				StartEInput:     999.0,
				StartEOutput:    999.0,
				StartECharge:    999.0,
				StartEDischarge: 999.0,
			}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)

	require.NotNil(t, sr.Offpeak)
	assert.Equal(t, dynamo.OffpeakStatusPending, sr.Offpeak.Status)
	require.NotNil(t, sr.Offpeak.GridUsageKwh)
	assert.InDelta(t, 1.8, *sr.Offpeak.GridUsageKwh, 0.01)
	require.NotNil(t, sr.Offpeak.SolarKwh)
	assert.InDelta(t, 0, *sr.Offpeak.SolarKwh, 0.01)
	require.NotNil(t, sr.Offpeak.BatteryChargeKwh)
	assert.InDelta(t, 0, *sr.Offpeak.BatteryChargeKwh, 0.01)
	require.NotNil(t, sr.Offpeak.BatteryDischargeKwh)
	assert.InDelta(t, 0, *sr.Offpeak.BatteryDischargeKwh, 0.01)
	require.NotNil(t, sr.Offpeak.GridExportKwh)
	assert.InDelta(t, 0, *sr.Offpeak.GridExportKwh, 0.01)
	assert.Nil(t, sr.Offpeak.BatteryDeltaPercent, "battery delta percent unavailable mid-window")
}

func TestHandleStatusOffpeakComplete(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &dynamo.OffpeakItem{
				Status:       dynamo.OffpeakStatusComplete,
				GridUsageKwh: 3.0, SolarKwh: 6.0, BatteryChargeKwh: 2.0,
				BatteryDischargeKwh: 1.0, GridExportKwh: 0.5, BatteryDeltaPercent: 15.0,
			}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)

	require.NotNil(t, sr.Offpeak)
	assert.Equal(t, dynamo.OffpeakStatusComplete, sr.Offpeak.Status)
	require.NotNil(t, sr.Offpeak.GridUsageKwh)
	assert.Equal(t, 3.0, *sr.Offpeak.GridUsageKwh)
	require.NotNil(t, sr.Offpeak.SolarKwh)
	assert.Equal(t, 6.0, *sr.Offpeak.SolarKwh)
	require.NotNil(t, sr.Offpeak.BatteryChargeKwh)
	assert.Equal(t, 2.0, *sr.Offpeak.BatteryChargeKwh)
	require.NotNil(t, sr.Offpeak.BatteryDischargeKwh)
	assert.Equal(t, 1.0, *sr.Offpeak.BatteryDischargeKwh)
	require.NotNil(t, sr.Offpeak.GridExportKwh)
	assert.Equal(t, 0.5, *sr.Offpeak.GridExportKwh)
	require.NotNil(t, sr.Offpeak.BatteryDeltaPercent)
	assert.Equal(t, 15.0, *sr.Offpeak.BatteryDeltaPercent)
}

func TestHandleStatusNoTodayEnergy(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil // not found
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)

	assert.Nil(t, sr.TodayEnergy, "todayEnergy should be null when no record")
}

func TestHandleStatusComputedEnergyNoDaily(t *testing.T) {
	now := fixedNow()
	midnightUnix := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydneyTZ).Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				// Before midnight — excluded by computeTodayEnergy.
				{Timestamp: midnightUnix - 100, Ppv: 9999, Pload: 200, Pbat: 0, Pgrid: 0, Soc: 50},
				// After midnight: 3600W solar, 1800W grid import, 3600W battery charging.
				{Timestamp: midnightUnix + 100, Ppv: 3600, Pload: 2000, Pbat: -3600, Pgrid: 1800, Soc: 50},
				{Timestamp: midnightUnix + 110, Ppv: 3600, Pload: 2000, Pbat: -3600, Pgrid: 1800, Soc: 50},
				{Timestamp: midnightUnix + 120, Ppv: 3600, Pload: 2000, Pbat: -3600, Pgrid: 1800, Soc: 50},
			}, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.TodayEnergy, "should have computed energy from readings")
	// 2 pairs × (3600W × 10s / 3600s) / 1000 = 0.02 kWh
	assert.Equal(t, 0.02, sr.TodayEnergy.Epv)
	// 2 pairs × (1800W × 10s / 3600s) / 1000 = 0.01 kWh
	assert.Equal(t, 0.01, sr.TodayEnergy.EInput)
	assert.Equal(t, 0.0, sr.TodayEnergy.EOutput)
	// 2 pairs × (3600W × 10s / 3600s) / 1000 = 0.02 kWh
	assert.Equal(t, 0.02, sr.TodayEnergy.ECharge)
	assert.Equal(t, 0.0, sr.TodayEnergy.EDischarge)
}

func TestHandleStatusReconciledEnergy(t *testing.T) {
	now := fixedNow()
	midnightUnix := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydneyTZ).Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				// 3 readings → computed: epv=0.02, eInput=0.01, eOutput=0.00, eCharge=0.02, eDischarge=0.00
				{Timestamp: midnightUnix + 100, Ppv: 3600, Pload: 2000, Pbat: -3600, Pgrid: 1800, Soc: 50},
				{Timestamp: midnightUnix + 110, Ppv: 3600, Pload: 2000, Pbat: -3600, Pgrid: 1800, Soc: 50},
				{Timestamp: midnightUnix + 120, Ppv: 3600, Pload: 2000, Pbat: -3600, Pgrid: 1800, Soc: 50},
			}, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			// Stored: some fields lower than computed, some higher.
			return &dynamo.DailyEnergyItem{
				Epv: 0.01, EInput: 0.05, EOutput: 0.03, ECharge: 0.01, EDischarge: 0.04,
			}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.TodayEnergy, "should have reconciled energy")
	// Per-field max of computed vs stored:
	assert.Equal(t, 0.02, sr.TodayEnergy.Epv, "computed (0.02) > stored (0.01)")
	assert.Equal(t, 0.05, sr.TodayEnergy.EInput, "stored (0.05) > computed (0.01)")
	assert.Equal(t, 0.03, sr.TodayEnergy.EOutput, "stored (0.03) > computed (0.00)")
	assert.Equal(t, 0.02, sr.TodayEnergy.ECharge, "computed (0.02) > stored (0.01)")
	assert.Equal(t, 0.04, sr.TodayEnergy.EDischarge, "stored (0.04) > computed (0.00)")
}

func TestHandleStatusSingleReadingWithDaily(t *testing.T) {
	now := fixedNow()
	midnightUnix := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydneyTZ).Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				// Single reading after midnight → computeTodayEnergy returns nil.
				{Timestamp: midnightUnix + 100, Ppv: 5000, Pload: 2000, Pbat: -1000, Pgrid: 500, Soc: 80},
			}, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{
				Epv: 10.5, EInput: 2.3, EOutput: 1.1, ECharge: 4.0, EDischarge: 3.5,
			}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.TodayEnergy, "should use DailyEnergyItem when < 2 readings")
	assert.Equal(t, derivedstats.RoundEnergy(10.5), sr.TodayEnergy.Epv)
	assert.Equal(t, derivedstats.RoundEnergy(2.3), sr.TodayEnergy.EInput)
	assert.Equal(t, derivedstats.RoundEnergy(1.1), sr.TodayEnergy.EOutput)
	assert.Equal(t, derivedstats.RoundEnergy(4.0), sr.TodayEnergy.ECharge)
	assert.Equal(t, derivedstats.RoundEnergy(3.5), sr.TodayEnergy.EDischarge)
}

func TestHandleStatusSystemMissingFallbackCapacity(t *testing.T) {
	now := fixedNow()
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				{Timestamp: nowUnix - 10, Ppv: 100, Pload: 200, Pbat: 1000, Pgrid: 50, Soc: 50},
			}, nil
		},
		getSystemFn: func(_ context.Context, _ string) (*dynamo.SystemItem, error) {
			return nil, nil // not found
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)

	require.NotNil(t, sr.Battery)
	assert.Equal(t, 13.34, sr.Battery.CapacityKwh, "should use fallback capacity")
}

func TestHandleStatusSystemZeroCobatFallback(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		getSystemFn: func(_ context.Context, _ string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{Cobat: 0}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)

	require.NotNil(t, sr.Battery)
	assert.Equal(t, 13.34, sr.Battery.CapacityKwh, "should use fallback when cobat is 0")
}

func TestHandleStatusDynamoDBError(t *testing.T) {
	now := fixedNow()
	dbErr := errors.New("connection refused")

	tests := map[string]struct {
		mock *mockReader
	}{
		"readings error": {
			mock: &mockReader{
				queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
					return nil, dbErr
				},
			},
		},
		"system error": {
			mock: &mockReader{
				getSystemFn: func(_ context.Context, _ string) (*dynamo.SystemItem, error) {
					return nil, dbErr
				},
			},
		},
		"offpeak error": {
			mock: &mockReader{
				getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
					return nil, dbErr
				},
			},
		},
		"daily energy error": {
			mock: &mockReader{
				getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
					return nil, dbErr
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := NewHandler(tc.mock, nil, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), statusRequest())
			require.NoError(t, err)
			assert.Equal(t, 500, resp.StatusCode)

			var body map[string]string
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
			assert.Equal(t, "internal error", body["error"])
		})
	}
}

func TestHandleStatusOffpeakNotFound(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return nil, nil // no offpeak record exists
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)

	// No offpeak record — window times present, delta fields null.
	require.NotNil(t, sr.Offpeak)
	assert.Equal(t, "11:00", sr.Offpeak.WindowStart)
	assert.Equal(t, "14:00", sr.Offpeak.WindowEnd)
	assert.Nil(t, sr.Offpeak.GridUsageKwh)
	assert.Nil(t, sr.Offpeak.SolarKwh)
	assert.Nil(t, sr.Offpeak.BatteryChargeKwh)
	assert.Nil(t, sr.Offpeak.BatteryDischargeKwh)
	assert.Nil(t, sr.Offpeak.GridExportKwh)
	assert.Nil(t, sr.Offpeak.BatteryDeltaPercent)
}

func TestHandleStatusRollingAvgFewerThan2Readings(t *testing.T) {
	now := fixedNow()
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			// Only one reading in the 24h window (also within 15min).
			return []dynamo.ReadingItem{
				{Timestamp: nowUnix - 60, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50, Soc: 50},
			}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)

	// Only 1 reading in 15min window → rolling15min is null.
	assert.Nil(t, sr.Rolling15m, "rolling15min should be null with fewer than 2 readings")
	// But live should still be present.
	require.NotNil(t, sr.Live)
}

// TestHandleStatusCutoffSuppressedWhenAfterOffpeak verifies T-827:
// the estimated cutoff must be suppressed (nil) when it would fall at or after
// the next off-peak window, because the battery will be charged during that
// window. This applies to both battery.estimatedCutoffTime and
// rolling15min.estimatedCutoffTime.
func TestHandleStatusCutoffSuppressedWhenAfterOffpeak(t *testing.T) {
	// now = 07:00 Sydney on 2026-04-15. Off-peak window: 11:00-14:00.
	// Discharge rate is very low so the linear extrapolation lands well
	// inside (or after) the off-peak window.
	now := time.Date(2026, 4, 15, 7, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			// Two discharging readings within the 15min window.
			// pbat = 100W, soc = 50%, capacity = 13.34 kWh, cutoff = 10%
			// remaining = (50-10)/100 * 13.34 = 5.336 kWh
			// hours = 5.336 / 0.1 = 53.36 h → cutoff far after off-peak (tomorrow+).
			return []dynamo.ReadingItem{
				{Timestamp: nowUnix - 60, Ppv: 0, Pload: 100, Pbat: 100, Pgrid: 0, Soc: 50},
				{Timestamp: nowUnix - 10, Ppv: 0, Pload: 100, Pbat: 100, Pgrid: 0, Soc: 50},
			}, nil
		},
		getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.Battery)
	assert.Nil(t, sr.Battery.EstimatedCutoff,
		"battery.estimatedCutoffTime should be nil when cutoff falls after next off-peak window")
	require.NotNil(t, sr.Rolling15m)
	assert.Nil(t, sr.Rolling15m.EstimatedCutoff,
		"rolling15min.estimatedCutoffTime should be nil when cutoff falls after next off-peak window")
}

// TestHandleStatusCutoffShownWhenBeforeOffpeak verifies that a cutoff that
// lands strictly before the next off-peak window start is still shown.
func TestHandleStatusCutoffShownWhenBeforeOffpeak(t *testing.T) {
	// now = 07:00 Sydney on 2026-04-15. Off-peak window: 11:00-14:00.
	// Heavy discharge so cutoff is ~1 hour away, well before 11:00.
	now := time.Date(2026, 4, 15, 7, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			// pbat = 5336W, soc = 50%, capacity = 13.34 kWh, cutoff = 10%
			// remaining = 5.336 kWh, hours = 1.0 → cutoff at 08:00 (before 11:00).
			return []dynamo.ReadingItem{
				{Timestamp: nowUnix - 60, Ppv: 0, Pload: 5400, Pbat: 5336, Pgrid: 0, Soc: 50},
				{Timestamp: nowUnix - 10, Ppv: 0, Pload: 5400, Pbat: 5336, Pgrid: 0, Soc: 50},
			}, nil
		},
		getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.Battery)
	require.NotNil(t, sr.Battery.EstimatedCutoff,
		"battery.estimatedCutoffTime should be present when cutoff is before next off-peak")
	require.NotNil(t, sr.Rolling15m)
	require.NotNil(t, sr.Rolling15m.EstimatedCutoff,
		"rolling15min.estimatedCutoffTime should be present when cutoff is before next off-peak")
}

// TestHandleStatusCutoffShownWithInvalidOffpeakConfig verifies that when the
// off-peak window is misconfigured (unparseable), the cutoff filter falls
// through as a no-op — a computed cutoff is still returned as-is rather than
// silently suppressed.
func TestHandleStatusCutoffShownWithInvalidOffpeakConfig(t *testing.T) {
	now := time.Date(2026, 4, 15, 7, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			// Light discharge that would otherwise land inside the off-peak
			// window if off-peak were configured.
			return []dynamo.ReadingItem{
				{Timestamp: nowUnix - 60, Ppv: 0, Pload: 100, Pbat: 100, Pgrid: 0, Soc: 50},
				{Timestamp: nowUnix - 10, Ppv: 0, Pload: 100, Pbat: 100, Pgrid: 0, Soc: 50},
			}, nil
		},
		getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "bad", "also-bad")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.Battery)
	require.NotNil(t, sr.Battery.EstimatedCutoff,
		"cutoff should be returned when off-peak config is invalid (no suppression)")
	require.NotNil(t, sr.Rolling15m)
	require.NotNil(t, sr.Rolling15m.EstimatedCutoff,
		"rolling cutoff should be returned when off-peak config is invalid")
}

// TestHandleStatusCutoffSuppressedDuringOffpeak verifies that when "now" is
// already inside the off-peak window, any future cutoff is suppressed — the
// battery is being charged, so a projected cutoff during the same window is
// misleading.
func TestHandleStatusCutoffSuppressedDuringOffpeak(t *testing.T) {
	// now = 12:00 Sydney, inside off-peak window 11:00-14:00.
	// Note: this edge case (discharging Pbat during off-peak) is not fully
	// redundant with computeCutoffTime's Pbat<=0 guard — during real off-peak
	// the battery charges so Pbat<=0 and the helper returns nil, but data
	// glitches or throttled charging can produce discharge readings mid-window
	// which would otherwise surface a misleading cutoff.
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			// Battery still showing a discharge reading (unusual during off-peak,
			// but possible if charging hasn't started yet or is throttled).
			return []dynamo.ReadingItem{
				{Timestamp: nowUnix - 60, Ppv: 0, Pload: 5400, Pbat: 5336, Pgrid: 0, Soc: 50},
				{Timestamp: nowUnix - 10, Ppv: 0, Pload: 5400, Pbat: 5336, Pgrid: 0, Soc: 50},
			}, nil
		},
		getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.Battery)
	assert.Nil(t, sr.Battery.EstimatedCutoff,
		"battery.estimatedCutoffTime should be nil while now is inside the off-peak window")
	require.NotNil(t, sr.Rolling15m)
	assert.Nil(t, sr.Rolling15m.EstimatedCutoff,
		"rolling15min.estimatedCutoffTime should be nil while now is inside the off-peak window")
}

func TestHandleStatusBundlesNote(t *testing.T) {
	now := fixedNow()
	today := now.Format("2006-01-02")

	t.Run("populated note", func(t *testing.T) {
		mr := &mockReader{
			getNoteFn: func(_ context.Context, _, date string) (*dynamo.NoteItem, error) {
				assert.Equal(t, today, date, "/status reads today's note")
				return &dynamo.NoteItem{Date: date, Text: "Away in Bali", UpdatedAt: "2026-04-15T01:00:00Z"}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		sr := parseStatusResponse(t, resp)
		require.NotNil(t, sr.Note)
		assert.Equal(t, "Away in Bali", *sr.Note)
	})

	t.Run("absent note serialises as null", func(t *testing.T) {
		mr := &mockReader{
			getNoteFn: func(_ context.Context, _, _ string) (*dynamo.NoteItem, error) { return nil, nil },
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)

		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &raw))
		require.Contains(t, raw, "note", "note field must always be serialised")
		assert.Equal(t, "null", string(raw["note"]))
	})

	t.Run("note read failure leaves field nil and request 200", func(t *testing.T) {
		mr := &mockReader{
			getNoteFn: func(_ context.Context, _, _ string) (*dynamo.NoteItem, error) {
				return nil, errors.New("throttled")
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode, "/status must not 500 when only the note read fails")

		sr := parseStatusResponse(t, resp)
		assert.Nil(t, sr.Note)
	})
}

// TestHandleStatusCantEmptyBeforeOffpeak covers the wire-level cases for the
// T-1327 "battery can't empty before off-peak" indicator: liveFresh on/off,
// flag on/off, off-peak config presence, DST transition, and fallback
// capacity. Each case asserts the marshalled JSON shape ("cantEmptyBeforeOffpeak":
// true | null — false is never emitted) per AC 2.2.
func TestHandleStatusCantEmptyBeforeOffpeak(t *testing.T) {
	t.Run("a) liveFresh and condition true emits true", func(t *testing.T) {
		// now = 10:50 Sydney, off-peak 11:00-14:00 → 10 min to window.
		// Soc 60 with 13.34 kWh cap at 5 kW max → requires ~88 min to reach 5%
		// → cannot empty in 10 min → flag &true.
		now := time.Date(2026, 4, 15, 10, 50, 0, 0, sydneyTZ)
		nowUnix := now.Unix()
		mr := &mockReader{
			queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
				return []dynamo.ReadingItem{
					{Timestamp: nowUnix - 10, Ppv: 0, Pload: 200, Pbat: 100, Pgrid: 50, Soc: 60},
				}, nil
			},
			getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
				return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		sr := parseStatusResponse(t, resp)
		require.NotNil(t, sr.Battery)
		require.NotNil(t, sr.Battery.CantEmptyBeforeOffpeak, "flag must be set when condition holds")
		assert.True(t, *sr.Battery.CantEmptyBeforeOffpeak)

		// Verify JSON shape: field present and explicitly true.
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &raw))
		var battery map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw["battery"], &battery))
		require.Contains(t, battery, "cantEmptyBeforeOffpeak")
		assert.Equal(t, "true", string(battery["cantEmptyBeforeOffpeak"]))
	})

	t.Run("b) liveFresh and condition false emits null", func(t *testing.T) {
		// now = 10:00, off-peak 11:00-14:00 → 1 hour to window.
		// Soc 6 with 13.34 kWh → requires ~1.6 min, can empty in 1h → flag nil.
		now := time.Date(2026, 4, 15, 10, 0, 0, 0, sydneyTZ)
		nowUnix := now.Unix()
		mr := &mockReader{
			queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
				return []dynamo.ReadingItem{
					{Timestamp: nowUnix - 10, Ppv: 0, Pload: 200, Pbat: 100, Pgrid: 50, Soc: 6},
				}, nil
			},
			getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
				return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		sr := parseStatusResponse(t, resp)
		require.NotNil(t, sr.Battery)
		assert.Nil(t, sr.Battery.CantEmptyBeforeOffpeak, "flag must be nil when battery can empty in time")

		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &raw))
		var battery map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw["battery"], &battery))
		require.Contains(t, battery, "cantEmptyBeforeOffpeak", "field must always be serialised (no omitempty)")
		assert.Equal(t, "null", string(battery["cantEmptyBeforeOffpeak"]))
	})

	t.Run("c) not liveFresh emits null regardless", func(t *testing.T) {
		// Stale reading (>90s old) → liveFresh false → flag must not be computed.
		now := time.Date(2026, 4, 15, 10, 50, 0, 0, sydneyTZ)
		nowUnix := now.Unix()
		mr := &mockReader{
			queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
				return []dynamo.ReadingItem{
					{Timestamp: nowUnix - 3600, Ppv: 0, Pload: 200, Pbat: 100, Pgrid: 50, Soc: 60},
				}, nil
			},
			getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
				return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		sr := parseStatusResponse(t, resp)
		assert.Nil(t, sr.Live, "precondition: live must be omitted when stale")
		require.NotNil(t, sr.Battery)
		assert.Nil(t, sr.Battery.CantEmptyBeforeOffpeak, "flag must be nil when !liveFresh")
	})

	t.Run("d) empty off-peak config emits null", func(t *testing.T) {
		now := time.Date(2026, 4, 15, 10, 50, 0, 0, sydneyTZ)
		nowUnix := now.Unix()
		mr := &mockReader{
			queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
				return []dynamo.ReadingItem{
					{Timestamp: nowUnix - 10, Ppv: 0, Pload: 200, Pbat: 100, Pgrid: 50, Soc: 60},
				}, nil
			},
			getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
				return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
			},
		}
		// Empty off-peak config → ParseOffpeakWindow returns ok=false → no boundary.
		h := NewHandler(mr, nil, testSerial, testToken, "", "")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		sr := parseStatusResponse(t, resp)
		require.NotNil(t, sr.Battery)
		assert.Nil(t, sr.Battery.CantEmptyBeforeOffpeak, "flag must be nil when off-peak config is missing")
	})

	t.Run("e) Sydney DST transition day", func(t *testing.T) {
		// 2026-10-04 is DST start in Sydney — clocks jump 02:00 AEST → 03:00 AEDT.
		// Pin now to 01:30 AEST (= 15:30 UTC on 2026-10-03). Off-peak start
		// 11:00 AEDT (= 00:00 UTC on 2026-10-04) → real elapsed ≈ 8.5 h.
		// Soc 60 with 13.34 kWh at 5 kW max → required ≈ 1.47 h < 8.5 h → flag nil.
		// The point is to verify nextOffpeakStart's DST math feeds the helper
		// without producing a spurious flag flip on the gap day.
		loc, err := time.LoadLocation("Australia/Sydney")
		require.NoError(t, err)
		now := time.Date(2026, 10, 4, 1, 30, 0, 0, loc)
		nowUnix := now.Unix()
		mr := &mockReader{
			queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
				return []dynamo.ReadingItem{
					{Timestamp: nowUnix - 10, Ppv: 0, Pload: 200, Pbat: 100, Pgrid: 50, Soc: 60},
				}, nil
			},
			getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
				return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		sr := parseStatusResponse(t, resp)
		require.NotNil(t, sr.Battery)
		assert.Nil(t, sr.Battery.CantEmptyBeforeOffpeak,
			"DST day: required hours (~1.47h) far less than time-to-window (~8.5h) → flag nil")

		// Cross-check the boundary the integration uses: nextOpStart on
		// the DST day must be 11:00 AEDT (UTC+11), not 11:00 AEST.
		nextOp, ok := nextOffpeakStart(now.In(sydneyTZ), "11:00", "14:00")
		require.True(t, ok)
		_, offsetSec := nextOp.Zone()
		assert.Equal(t, 11*3600, offsetSec, "off-peak start sits in AEDT after the DST gap")
	})

	t.Run("f) fallback capacity when system record missing", func(t *testing.T) {
		// System record nil → handler uses fallbackCapacityKwh = 13.34.
		// Same conditions as case (a): flag should fire.
		now := time.Date(2026, 4, 15, 10, 50, 0, 0, sydneyTZ)
		nowUnix := now.Unix()
		mr := &mockReader{
			queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
				return []dynamo.ReadingItem{
					{Timestamp: nowUnix - 10, Ppv: 0, Pload: 200, Pbat: 100, Pgrid: 50, Soc: 60},
				}, nil
			},
			getSystemFn: func(_ context.Context, _ string) (*dynamo.SystemItem, error) {
				return nil, nil // not found → fallback capacity used
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		sr := parseStatusResponse(t, resp)
		require.NotNil(t, sr.Battery)
		assert.Equal(t, 13.34, sr.Battery.CapacityKwh, "precondition: fallback capacity in use")
		require.NotNil(t, sr.Battery.CantEmptyBeforeOffpeak, "flag must fire even on fallback capacity")
		assert.True(t, *sr.Battery.CantEmptyBeforeOffpeak)
	})
}

func TestHandleStatusSingleNowCapture(t *testing.T) {
	// Verify that the handler captures "now" once and uses it consistently.
	// The mock clock should be called exactly once via nowFunc.
	callCount := 0
	now := fixedNow()

	mr := &mockReader{}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time {
		callCount++
		return now
	}

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, callCount, "nowFunc should be called exactly once for time consistency")
}
