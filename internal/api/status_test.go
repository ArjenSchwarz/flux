package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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
			// Return readings spanning 24h with a few in the last 15min and 60s.
			return []dynamo.ReadingItem{
				// Old reading (24h ago) — lowest SOC.
				{Timestamp: nowUnix - 86000, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50, Soc: 20},
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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
	assert.Equal(t, 10, sr.Battery.CutoffPercent)
	require.NotNil(t, sr.Battery.EstimatedCutoff, "should have cutoff since discharging")
	require.NotNil(t, sr.Battery.Low24h)
	assert.Equal(t, roundPower(20), sr.Battery.Low24h.Soc)

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
	assert.Equal(t, roundEnergy(12.345), sr.TodayEnergy.Epv)
}

func TestHandleStatusNoReadings(t *testing.T) {
	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

func TestHandleStatusOffpeakPendingNoDailyEnergy(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &dynamo.OffpeakItem{Status: dynamo.OffpeakStatusPending}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)

	require.NotNil(t, sr.Offpeak)
	assert.Equal(t, "11:00", sr.Offpeak.WindowStart)
	assert.Equal(t, "14:00", sr.Offpeak.WindowEnd)
	assert.Empty(t, sr.Offpeak.Status, "no status when deltas cannot be computed")
	assert.Nil(t, sr.Offpeak.GridUsageKwh, "delta fields should be null without daily energy")
	assert.Nil(t, sr.Offpeak.SolarKwh)
	assert.Nil(t, sr.Offpeak.BatteryChargeKwh)
	assert.Nil(t, sr.Offpeak.BatteryDischargeKwh)
	assert.Nil(t, sr.Offpeak.GridExportKwh)
	assert.Nil(t, sr.Offpeak.BatteryDeltaPercent)
}

func TestHandleStatusOffpeakInProgress(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &dynamo.OffpeakItem{
				Status:          dynamo.OffpeakStatusPending,
				StartEpv:        10.0,
				StartEInput:     2.0,
				StartEOutput:    0.5,
				StartECharge:    1.0,
				StartEDischarge: 3.0,
			}, nil
		},
		getDailyEnergyFn: func(_ context.Context, serial, date string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{
				SysSn: serial, Date: date,
				Epv: 12.5, EInput: 3.5, EOutput: 0.6, ECharge: 1.8, EDischarge: 3.4,
			}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)

	require.NotNil(t, sr.Offpeak)
	assert.Equal(t, dynamo.OffpeakStatusPending, sr.Offpeak.Status)
	require.NotNil(t, sr.Offpeak.GridUsageKwh)
	assert.InDelta(t, 1.5, *sr.Offpeak.GridUsageKwh, 0.001)
	require.NotNil(t, sr.Offpeak.SolarKwh)
	assert.InDelta(t, 2.5, *sr.Offpeak.SolarKwh, 0.001)
	require.NotNil(t, sr.Offpeak.BatteryChargeKwh)
	assert.InDelta(t, 0.8, *sr.Offpeak.BatteryChargeKwh, 0.001)
	require.NotNil(t, sr.Offpeak.BatteryDischargeKwh)
	assert.InDelta(t, 0.4, *sr.Offpeak.BatteryDischargeKwh, 0.001)
	require.NotNil(t, sr.Offpeak.GridExportKwh)
	assert.InDelta(t, 0.1, *sr.Offpeak.GridExportKwh, 0.001)
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.TodayEnergy, "should use DailyEnergyItem when < 2 readings")
	assert.Equal(t, roundEnergy(10.5), sr.TodayEnergy.Epv)
	assert.Equal(t, roundEnergy(2.3), sr.TodayEnergy.EInput)
	assert.Equal(t, roundEnergy(1.1), sr.TodayEnergy.EOutput)
	assert.Equal(t, roundEnergy(4.0), sr.TodayEnergy.ECharge)
	assert.Equal(t, roundEnergy(3.5), sr.TodayEnergy.EDischarge)
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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
			h := NewHandler(tc.mock, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, testSerial, testToken, "bad", "also-bad")
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

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
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

func TestHandleStatusSingleNowCapture(t *testing.T) {
	// Verify that the handler captures "now" once and uses it consistently.
	// The mock clock should be called exactly once via nowFunc.
	callCount := 0
	now := fixedNow()

	mr := &mockReader{}
	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time {
		callCount++
		return now
	}

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, callCount, "nowFunc should be called exactly once for time consistency")
}
