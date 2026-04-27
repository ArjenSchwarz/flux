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

// dayRequest builds an authenticated GET /day request with optional query params.
func dayRequest(params map[string]string) events.LambdaFunctionURLRequest {
	req := makeRequest("GET", "/day", "Bearer "+testToken)
	if params != nil {
		req.QueryStringParameters = params
	}
	return req
}

func parseDayResponse(t *testing.T, resp events.LambdaFunctionURLResponse) DayDetailResponse {
	t.Helper()
	var dr DayDetailResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &dr))
	return dr
}

func TestHandleDayNormalCase(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	date := "2026-04-14"

	// Create readings spanning the day at known times.
	readings := []dynamo.ReadingItem{
		{Timestamp: time.Date(2026, 4, 14, 8, 1, 0, 0, loc).Unix(), Ppv: 1000, Pload: 500, Pbat: 200, Pgrid: 100, Soc: 80},
		{Timestamp: time.Date(2026, 4, 14, 8, 3, 0, 0, loc).Unix(), Ppv: 1200, Pload: 600, Pbat: 300, Pgrid: 50, Soc: 78},
		{Timestamp: time.Date(2026, 4, 14, 12, 0, 0, 0, loc).Unix(), Ppv: 3000, Pload: 800, Pbat: -500, Pgrid: 0, Soc: 95},
		{Timestamp: time.Date(2026, 4, 14, 18, 0, 0, 0, loc).Unix(), Ppv: 0, Pload: 1200, Pbat: 1000, Pgrid: 200, Soc: 40},
	}

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, serial string, from, to int64) ([]dynamo.ReadingItem, error) {
			assert.Equal(t, testSerial, serial)
			return readings, nil
		},
		getDailyEnergyFn: func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
			assert.Equal(t, date, d)
			return &dynamo.DailyEnergyItem{
				Date: date, Epv: 15.5, EInput: 4.2, EOutput: 2.1, ECharge: 8.0, EDischarge: 6.5,
			}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	// Pin "now" to a date later than the requested one so the request is for a
	// past day (deterministic across test machines).
	h.nowFunc = func() time.Time { return fixedNow() }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	assert.Equal(t, date, dr.Date)
	assert.NotEmpty(t, dr.Readings, "should have downsampled readings")

	// Summary should have both energy and socLow.
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.Epv)
	assert.Equal(t, roundEnergy(15.5), *dr.Summary.Epv)
	// socLow should be from raw data (40 at 18:00).
	require.NotNil(t, dr.Summary.SocLow)
	assert.Equal(t, roundPower(40), *dr.Summary.SocLow)

	// peakPeriods should be present and non-null (at least empty slice).
	assert.NotNil(t, dr.PeakPeriods, "peakPeriods should never be null")

	// DailyUsage: past day with readings present should produce at least one
	// emitted block. Detailed kind/status/boundary coverage is in
	// TestFindDailyUsage; this assertion just pins the wiring.
	require.NotNil(t, dr.DailyUsage, "dailyUsage should be present when readings exist")
	assert.NotEmpty(t, dr.DailyUsage.Blocks, "dailyUsage should have at least one block")
}

func TestHandleDayDailyUsageOvercast(t *testing.T) {
	// AC 4.1 "Overcast day, no qualifying Ppv": all five blocks emitted; sunrise/
	// sunset edges fall back to the Melbourne table, so night, morningPeak,
	// afternoonPeak, and evening have boundarySource = "estimated"; offPeak is
	// "readings".
	loc, _ := time.LoadLocation("Australia/Sydney")
	date := "2026-04-14"

	readings := []dynamo.ReadingItem{
		{Timestamp: time.Date(2026, 4, 14, 1, 0, 0, 0, loc).Unix(), Ppv: 0, Pload: 600, Soc: 80},
		{Timestamp: time.Date(2026, 4, 14, 8, 0, 0, 0, loc).Unix(), Ppv: 0, Pload: 600, Soc: 70},
		{Timestamp: time.Date(2026, 4, 14, 14, 0, 0, 0, loc).Unix(), Ppv: 0, Pload: 600, Soc: 60},
		{Timestamp: time.Date(2026, 4, 14, 20, 0, 0, 0, loc).Unix(), Ppv: 0, Pload: 600, Soc: 50},
	}

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return fixedNow() }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.DailyUsage, "dailyUsage should be present even on overcast days")
	require.Len(t, dr.DailyUsage.Blocks, 5, "all five blocks should emit on a complete overcast past day")

	byKind := make(map[string]DailyUsageBlock, 5)
	for _, b := range dr.DailyUsage.Blocks {
		byKind[b.Kind] = b
	}
	assert.Equal(t, DailyUsageBoundaryEstimated, byKind[DailyUsageKindNight].BoundarySource, "night: end = sunrise fallback")
	assert.Equal(t, DailyUsageBoundaryEstimated, byKind[DailyUsageKindMorningPeak].BoundarySource, "morningPeak: start = sunrise fallback")
	assert.Equal(t, DailyUsageBoundaryReadings, byKind[DailyUsageKindOffPeak].BoundarySource, "offPeak edges are SSM-derived")
	assert.Equal(t, DailyUsageBoundaryEstimated, byKind[DailyUsageKindAfternoonPeak].BoundarySource, "afternoonPeak: end = sunset fallback")
	assert.Equal(t, DailyUsageBoundaryEstimated, byKind[DailyUsageKindEvening].BoundarySource, "evening: start = sunset fallback")
	for _, b := range dr.DailyUsage.Blocks {
		assert.Equal(t, DailyUsageStatusComplete, b.Status, "kind=%s", b.Kind)
	}
}

func TestHandleDayFallbackToDailyPower(t *testing.T) {
	date := "2026-04-14"

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{}, nil // no flux-readings
		},
		queryDailyPowerFn: func(_ context.Context, serial, d string) ([]dynamo.DailyPowerItem, error) {
			assert.Equal(t, date, d)
			return []dynamo.DailyPowerItem{
				{UploadTime: "2026-04-14 08:00:00", Cbat: 85},
				{UploadTime: "2026-04-14 12:00:00", Cbat: 95},
				{UploadTime: "2026-04-14 18:00:00", Cbat: 45},
			}, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	assert.Equal(t, date, dr.Date)
	require.Len(t, dr.Readings, 3, "fallback data used directly, not downsampled")

	// Verify cbat mapped to soc, power fields are 0.
	for _, r := range dr.Readings {
		assert.Equal(t, float64(0), r.Ppv)
		assert.Equal(t, float64(0), r.Pload)
		assert.Equal(t, float64(0), r.Pbat)
		assert.Equal(t, float64(0), r.Pgrid)
	}
	assert.Equal(t, roundPower(85), dr.Readings[0].Soc)
	assert.Equal(t, roundPower(95), dr.Readings[1].Soc)
	assert.Equal(t, roundPower(45), dr.Readings[2].Soc)

	// Summary should have socLow from fallback data.
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.SocLow)
	assert.Equal(t, roundPower(45), *dr.Summary.SocLow)
	// Energy fields should be null since no daily energy.
	assert.Nil(t, dr.Summary.Epv)

	// peakPeriods should be empty array (no flux-readings to compute from).
	assert.NotNil(t, dr.PeakPeriods, "peakPeriods should never be null")
	assert.Empty(t, dr.PeakPeriods, "peakPeriods should be empty when using fallback data")

	// DailyUsage must be omitted entirely — the daily-power fallback lacks
	// the pload resolution required for accurate integration (req 1.10).
	assert.Nil(t, dr.DailyUsage, "dailyUsage must be omitted on the daily-power fallback path")
}

func TestHandleDayOnlyDailyEnergySocLowIsNull(t *testing.T) {
	// When daily energy exists but no readings/power data, SocLow and SocLowTime
	// should be null — not zero-valued — so the client can distinguish "absent" from "0%".
	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{}, nil
		},
		queryDailyPowerFn: func(_ context.Context, _, _ string) ([]dynamo.DailyPowerItem, error) {
			return []dynamo.DailyPowerItem{}, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{
				Date: "2026-04-14", Epv: 10.0, EInput: 2.0, EOutput: 1.0, ECharge: 5.0, EDischarge: 4.0,
			}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": "2026-04-14"}))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.Summary, "summary should exist when daily energy is present")
	assert.Nil(t, dr.Summary.SocLow, "socLow should be null when no readings exist")
	assert.Nil(t, dr.Summary.SocLowTime, "socLowTime should be null when no readings exist")
	require.NotNil(t, dr.Summary.Epv, "energy fields should be populated")
}

func TestHandleDayNoDataFromEitherSource(t *testing.T) {
	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{}, nil
		},
		queryDailyPowerFn: func(_ context.Context, _, _ string) ([]dynamo.DailyPowerItem, error) {
			return []dynamo.DailyPowerItem{}, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": "2026-04-14"}))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	assert.Empty(t, dr.Readings)
	assert.Nil(t, dr.Summary, "summary should be null when no data exists")

	// peakPeriods should be empty array even with no data.
	assert.NotNil(t, dr.PeakPeriods, "peakPeriods should never be null")
	assert.Empty(t, dr.PeakPeriods)
}

func TestHandleDayReadingsButNoDailyEnergy(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	date := "2026-04-14"

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				{Timestamp: time.Date(2026, 4, 14, 10, 0, 0, 0, loc).Unix(), Soc: 70, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50},
				{Timestamp: time.Date(2026, 4, 14, 14, 0, 0, 0, loc).Unix(), Soc: 35, Ppv: 50, Pload: 300, Pbat: 400, Pgrid: 100},
			}, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.SocLow)
	assert.Equal(t, roundPower(35), *dr.Summary.SocLow)
	assert.Nil(t, dr.Summary.Epv, "energy fields should be null")
	assert.Nil(t, dr.Summary.EInput)
}

func TestHandleDayPeakPeriods(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	date := "2026-04-14"

	// Build readings: a sustained high-load period at 8:00-8:05 (outside off-peak 11:00-14:00)
	// and low-load readings elsewhere. Readings every 10s for 3+ minutes to exceed minPeriodSeconds.
	base := time.Date(2026, 4, 14, 8, 0, 0, 0, loc)
	var readings []dynamo.ReadingItem
	// 20 readings at 10s intervals = ~3.3 minutes of high load at 8:00
	for i := range 20 {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: base.Add(time.Duration(i*10) * time.Second).Unix(),
			Ppv:       100, Pload: 3000, Pbat: 0, Pgrid: 0, Soc: 50,
		})
	}
	// A few low-load readings at 16:00 to bring the mean down
	afternoon := time.Date(2026, 4, 14, 16, 0, 0, 0, loc)
	for i := range 20 {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: afternoon.Add(time.Duration(i*10) * time.Second).Unix(),
			Ppv:       0, Pload: 200, Pbat: 0, Pgrid: 0, Soc: 50,
		})
	}

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.PeakPeriods)
	require.NotEmpty(t, dr.PeakPeriods, "should detect the high-load period as a peak")
	assert.Equal(t, roundPower(3000), dr.PeakPeriods[0].AvgLoadW)
	assert.True(t, dr.PeakPeriods[0].EnergyWh > 0, "energy should be positive")
}

func TestHandleDayDateValidation(t *testing.T) {
	tests := map[string]struct {
		params map[string]string
	}{
		"missing date":   {params: nil},
		"empty date":     {params: map[string]string{"date": ""}},
		"invalid format": {params: map[string]string{"date": "15-04-2026"}},
		"partial date":   {params: map[string]string{"date": "2026-04"}},
		"garbage":        {params: map[string]string{"date": "not-a-date"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := NewHandler(&mockReader{}, testSerial, testToken, "11:00", "14:00")

			resp, err := h.Handle(context.Background(), dayRequest(tc.params))
			require.NoError(t, err)
			assert.Equal(t, 400, resp.StatusCode)

			var body map[string]string
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
			assert.Equal(t, "invalid or missing date parameter", body["error"])
		})
	}
}

func TestHandleDaySocLowFromRawNotDownsampled(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	date := "2026-04-14"

	// Two readings in the same 5-min bucket: SOC 80 and SOC 20.
	// Downsampled average would be 50, but socLow should be 20 (from raw).
	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				{Timestamp: time.Date(2026, 4, 14, 10, 1, 0, 0, loc).Unix(), Soc: 80, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50},
				{Timestamp: time.Date(2026, 4, 14, 10, 3, 0, 0, loc).Unix(), Soc: 20, Ppv: 100, Pload: 200, Pbat: 300, Pgrid: 50},
			}, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.SocLow)
	assert.Equal(t, roundPower(20), *dr.Summary.SocLow, "socLow should come from raw data, not downsampled")
}

// TestHandleDayTodayReconcilesEnergy reproduces T-828: the Day Detail summary
// for today must reconcile stored daily energy with values integrated from live
// readings — so it matches the dashboard's /status view. When the stored
// DailyEnergyItem lags behind (it is refreshed hourly from AlphaESS), the
// response should still reflect the larger, integration-based figures.
func TestHandleDayTodayReconcilesEnergy(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	now := fixedNow() // 2026-04-15 10:00 AEST
	date := now.In(loc).Format("2006-01-02")

	// Two readings 60s apart today: each directional field integrates to
	// exactly 100 Wh (0.1 kWh after rounding).
	t1 := time.Date(2026, 4, 15, 9, 0, 0, 0, loc).Unix()
	t2 := t1 + 60
	readings := []dynamo.ReadingItem{
		{Timestamp: t1, Ppv: 6000, Pload: 500, Pbat: 6000, Pgrid: 6000, Soc: 80},
		{Timestamp: t2, Ppv: 6000, Pload: 500, Pbat: 6000, Pgrid: 6000, Soc: 79},
	}

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			// Stored values lag behind live readings (hourly refresh).
			return &dynamo.DailyEnergyItem{
				Date: date, Epv: 0.05, EInput: 0.05, EOutput: 0, ECharge: 0, EDischarge: 0.05,
			}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.Epv)
	require.NotNil(t, dr.Summary.EInput)
	require.NotNil(t, dr.Summary.EDischarge)

	assert.InDelta(t, 0.1, *dr.Summary.Epv, 0.001, "epv should be reconciled (max of computed and stored)")
	assert.InDelta(t, 0.1, *dr.Summary.EInput, 0.001, "eInput (grid import) should be reconciled")
	assert.InDelta(t, 0.1, *dr.Summary.EDischarge, 0.001, "eDischarge (battery) should be reconciled")
}

// TestHandleDayPastDateDoesNotReconcile locks in the scope of the T-828 fix:
// reconciliation is for today only. Past-date requests continue to return the
// authoritative stored values because finalized totals are written at midnight.
func TestHandleDayPastDateDoesNotReconcile(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	now := fixedNow()
	pastDate := "2026-04-14"

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			// Readings exist for the past date (within the 30d TTL window).
			t1 := time.Date(2026, 4, 14, 9, 0, 0, 0, loc).Unix()
			return []dynamo.ReadingItem{
				{Timestamp: t1, Ppv: 6000, Pload: 500, Pbat: 6000, Pgrid: 6000, Soc: 80},
				{Timestamp: t1 + 60, Ppv: 6000, Pload: 500, Pbat: 6000, Pgrid: 6000, Soc: 79},
			}, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{
				Date: pastDate, Epv: 0.05, EInput: 0.05, EOutput: 0, ECharge: 0, EDischarge: 0.05,
			}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": pastDate}))
	require.NoError(t, err)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.Epv)
	assert.Equal(t, 0.05, *dr.Summary.Epv, "past dates use stored totals directly")
	assert.Equal(t, 0.05, *dr.Summary.EInput)
	assert.Equal(t, 0.05, *dr.Summary.EDischarge)
}

func TestHandleDayDynamoDBError(t *testing.T) {
	dbErr := errors.New("timeout")

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

			resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": "2026-04-14"}))
			require.NoError(t, err)
			assert.Equal(t, 500, resp.StatusCode)

			var body map[string]string
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
			assert.Equal(t, "internal error", body["error"])
		})
	}
}
