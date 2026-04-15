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
	loc, _ := time.LoadLocation("Australia/Sydney")
	return time.Date(2026, 4, 15, 10, 0, 0, 0, loc)
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
	now := fixedNow()
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

func TestHandleStatusOffpeakPending(t *testing.T) {
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
	assert.Nil(t, sr.Offpeak.GridUsageKwh, "delta fields should be null when pending")
	assert.Nil(t, sr.Offpeak.SolarKwh)
	assert.Nil(t, sr.Offpeak.BatteryChargeKwh)
	assert.Nil(t, sr.Offpeak.BatteryDischargeKwh)
	assert.Nil(t, sr.Offpeak.GridExportKwh)
	assert.Nil(t, sr.Offpeak.BatteryDeltaPercent)
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
