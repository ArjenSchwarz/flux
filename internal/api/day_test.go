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
