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

// historyRequest builds an authenticated GET /history request with optional query params.
func historyRequest(params map[string]string) events.LambdaFunctionURLRequest {
	req := makeRequest("GET", "/history", "Bearer "+testToken)
	if params != nil {
		req.QueryStringParameters = params
	}
	return req
}

func parseHistoryResponse(t *testing.T, resp events.LambdaFunctionURLResponse) HistoryResponse {
	t.Helper()
	var hr HistoryResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &hr))
	return hr
}

func TestHandleHistoryDefaultDays(t *testing.T) {
	now := fixedNow()

	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, serial, start, end string) ([]dynamo.DailyEnergyItem, error) {
			assert.Equal(t, testSerial, serial)
			// Default 7 days: start should be 6 days before today.
			expectedStart := now.AddDate(0, 0, -6).Format("2006-01-02")
			assert.Equal(t, expectedStart, start)
			assert.Equal(t, now.Format("2006-01-02"), end)

			return []dynamo.DailyEnergyItem{
				{Date: "2026-04-10", Epv: 10.123, EInput: 2.345, EOutput: 1.234, ECharge: 5.678, EDischarge: 4.567},
				{Date: "2026-04-11", Epv: 11.0, EInput: 3.0, EOutput: 2.0, ECharge: 6.0, EDischarge: 5.0},
			}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	assert.Len(t, hr.Days, 2)
	assert.Equal(t, "2026-04-10", hr.Days[0].Date)
	assert.Equal(t, roundEnergy(10.123), hr.Days[0].Epv)
}

func TestHandleHistoryExplicitDays(t *testing.T) {
	now := fixedNow()

	tests := map[string]struct {
		days         string
		expectedDays int
	}{
		"14 days": {days: "14", expectedDays: 13},
		"30 days": {days: "30", expectedDays: 29},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mr := &mockReader{
				queryDailyEnergyFn: func(_ context.Context, _, start, end string) ([]dynamo.DailyEnergyItem, error) {
					expectedStart := now.AddDate(0, 0, -tc.expectedDays).Format("2006-01-02")
					assert.Equal(t, expectedStart, start)
					assert.Equal(t, now.Format("2006-01-02"), end)
					return []dynamo.DailyEnergyItem{}, nil
				},
			}

			h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), historyRequest(map[string]string{"days": tc.days}))
			require.NoError(t, err)
			assert.Equal(t, 200, resp.StatusCode)
		})
	}
}

func TestHandleHistoryInvalidDays(t *testing.T) {
	now := fixedNow()

	tests := map[string]struct {
		days string
	}{
		"invalid number": {days: "5"},
		"zero":           {days: "0"},
		"negative":       {days: "-1"},
		"non-numeric":    {days: "abc"},
		"too large":      {days: "60"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := NewHandler(&mockReader{}, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), historyRequest(map[string]string{"days": tc.days}))
			require.NoError(t, err)
			assert.Equal(t, 400, resp.StatusCode)

			var body map[string]string
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
			assert.Equal(t, "invalid days parameter, must be 7, 14, or 30", body["error"])
		})
	}
}

func TestHandleHistoryNoData(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	assert.Empty(t, hr.Days)
}

func TestHandleHistoryAscendingOrder(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			// Return in ascending order (as DynamoDB does with ScanIndexForward: true).
			return []dynamo.DailyEnergyItem{
				{Date: "2026-04-09"},
				{Date: "2026-04-10"},
				{Date: "2026-04-11"},
			}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 3)
	assert.Equal(t, "2026-04-09", hr.Days[0].Date)
	assert.Equal(t, "2026-04-10", hr.Days[1].Date)
	assert.Equal(t, "2026-04-11", hr.Days[2].Date)
}

func TestHandleHistoryEnergyRounding(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{
				{Date: "2026-04-15", Epv: 10.126, EInput: 3.455, EOutput: 1.234, ECharge: 5.675, EDischarge: 4.565},
			}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 1)
	assert.Equal(t, roundEnergy(10.126), hr.Days[0].Epv)
	assert.Equal(t, roundEnergy(3.455), hr.Days[0].EInput)
	assert.Equal(t, roundEnergy(1.234), hr.Days[0].EOutput)
	assert.Equal(t, roundEnergy(5.675), hr.Days[0].ECharge)
	assert.Equal(t, roundEnergy(4.565), hr.Days[0].EDischarge)
}

// TestHandleHistoryReconcilesTodaysRow reproduces T-828: when today is part of
// the requested range, today's row must reconcile stored daily energy with
// values integrated from live readings — matching the dashboard's /status
// values and the day-detail summary. Rows for past days are untouched.
func TestHandleHistoryReconcilesTodaysRow(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	now := fixedNow()
	today := now.In(loc).Format("2006-01-02")

	t1 := time.Date(2026, 4, 15, 9, 0, 0, 0, loc).Unix()
	t2 := t1 + 60

	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{
				{Date: "2026-04-14", Epv: 20, EInput: 4, EOutput: 1, ECharge: 10, EDischarge: 8},
				{Date: today, Epv: 0.05, EInput: 0.05, EOutput: 0, ECharge: 0, EDischarge: 0.05},
			}, nil
		},
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				{Timestamp: t1, Ppv: 6000, Pload: 500, Pbat: 6000, Pgrid: 6000, Soc: 80},
				{Timestamp: t2, Ppv: 6000, Pload: 500, Pbat: 6000, Pgrid: 6000, Soc: 79},
			}, nil
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 2)

	// Yesterday: stored values pass through unchanged.
	assert.Equal(t, "2026-04-14", hr.Days[0].Date)
	assert.Equal(t, 20.0, hr.Days[0].Epv)
	assert.Equal(t, 4.0, hr.Days[0].EInput)
	assert.Equal(t, 8.0, hr.Days[0].EDischarge)

	// Today: reconciled against live readings.
	assert.Equal(t, today, hr.Days[1].Date)
	assert.InDelta(t, 0.1, hr.Days[1].Epv, 0.001, "today's epv should be reconciled")
	assert.InDelta(t, 0.1, hr.Days[1].EInput, 0.001, "today's eInput should be reconciled")
	assert.InDelta(t, 0.1, hr.Days[1].EDischarge, 0.001, "today's eDischarge should be reconciled")
}

func TestHandleHistoryDynamoDBError(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return nil, errors.New("throttled")
		},
	}

	h := NewHandler(mr, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
	assert.Equal(t, "internal error", body["error"])
}
