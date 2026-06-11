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

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	assert.Len(t, hr.Days, 2)
	assert.Equal(t, "2026-04-10", hr.Days[0].Date)
	assert.Equal(t, derivedstats.RoundEnergy(10.123), hr.Days[0].Epv)
}

// TestHandleHistoryDaysValidation covers the widened days bounds (1-31). The
// to-date ranges resolve to any inclusive day-count from 1 through 31, so the
// backend accepts that whole range and rejects anything outside it (or
// non-numeric) with the bounds error message. An absent param defaults to 7.
func TestHandleHistoryDaysValidation(t *testing.T) {
	now := fixedNow()

	tests := map[string]struct {
		days       string // query value; empty string means the param is absent
		wantStatus int
		// wantDays is the expected inclusive window length when wantStatus is
		// 200; the startDate is now-(wantDays-1).
		wantDays int
	}{
		"absent defaults to 7": {days: "", wantStatus: 200, wantDays: 7},
		"one day":              {days: "1", wantStatus: 200, wantDays: 1},
		"seven days":           {days: "7", wantStatus: 200, wantDays: 7},
		"fourteen days":        {days: "14", wantStatus: 200, wantDays: 14},
		"thirty days":          {days: "30", wantStatus: 200, wantDays: 30},
		"thirty-one days":      {days: "31", wantStatus: 200, wantDays: 31},
		"zero rejected":        {days: "0", wantStatus: 400},
		"thirty-two rejected":  {days: "32", wantStatus: 400},
		"negative rejected":    {days: "-1", wantStatus: 400},
		"non-numeric rejected": {days: "x", wantStatus: 400},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mr := &mockReader{
				queryDailyEnergyFn: func(_ context.Context, _, start, end string) ([]dynamo.DailyEnergyItem, error) {
					expectedStart := now.AddDate(0, 0, -(tc.wantDays - 1)).Format("2006-01-02")
					assert.Equal(t, expectedStart, start, "window start should be now-(days-1)")
					assert.Equal(t, now.Format("2006-01-02"), end)
					return []dynamo.DailyEnergyItem{}, nil
				},
			}

			h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			var params map[string]string
			if tc.days != "" {
				params = map[string]string{"days": tc.days}
			}

			resp, err := h.Handle(context.Background(), historyRequest(params))
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, resp.StatusCode)

			if tc.wantStatus == 400 {
				var body map[string]string
				require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
				assert.Equal(t, "invalid days parameter, must be between 1 and 31", body["error"])
			}
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

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 1)
	assert.Equal(t, derivedstats.RoundEnergy(10.126), hr.Days[0].Epv)
	assert.Equal(t, derivedstats.RoundEnergy(3.455), hr.Days[0].EInput)
	assert.Equal(t, derivedstats.RoundEnergy(1.234), hr.Days[0].EOutput)
	assert.Equal(t, derivedstats.RoundEnergy(5.675), hr.Days[0].ECharge)
	assert.Equal(t, derivedstats.RoundEnergy(4.565), hr.Days[0].EDischarge)
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

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
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

// TestHandleHistoryOffpeakSplit verifies the per-day off-peak grid split:
// complete records pass through final deltas, today's pending record is
// live-integrated from readings, and days without an off-peak record (or
// with a stale pending record from a poller failure) report no split.
func TestHandleHistoryOffpeakSplit(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	// 11:30 AEST — 30 minutes into the 11:00-14:00 off-peak window so
	// today's pending row live-integrates over [11:00, 11:30).
	now := time.Date(2026, 4, 15, 11, 30, 0, 0, loc)
	today := now.In(loc).Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).In(loc).Format("2006-01-02")
	twoDaysAgo := now.AddDate(0, 0, -2).In(loc).Format("2006-01-02")

	// Synthetic readings: constant 3600 W grid import across [11:00, 11:30)
	// → 1.8 kWh integrated import; zero export. Cadence 10s.
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
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{
				{Date: twoDaysAgo, Epv: 15, EInput: 4, EOutput: 1, ECharge: 8, EDischarge: 7},
				{Date: yesterday, Epv: 16, EInput: 5, EOutput: 2, ECharge: 9, EDischarge: 8},
				{Date: today, Epv: 12, EInput: 3.2, EOutput: 0.7, ECharge: 4, EDischarge: 3},
			}, nil
		},
		queryOffpeakFn: func(_ context.Context, _, _, _ string) ([]dynamo.OffpeakItem, error) {
			return []dynamo.OffpeakItem{
				// Complete record for yesterday — final deltas.
				{
					Date: yesterday, Status: dynamo.OffpeakStatusComplete,
					GridUsageKwh: 2.4, GridExportKwh: 0.6,
				},
				// Stale pending record from two days ago — poller failed mid-window.
				{
					Date: twoDaysAgo, Status: dynamo.OffpeakStatusPending,
					StartEInput: 1.0, StartEOutput: 0.5,
				},
				// Pending today — live-integrated from the readings slice;
				// StartE* values must NOT be read (AC 5.3).
				{
					Date: today, Status: dynamo.OffpeakStatusPending,
					StartEInput: 999.0, StartEOutput: 999.0,
				},
			}, nil
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 3)

	// twoDaysAgo: stale pending record — split should be missing.
	assert.Equal(t, twoDaysAgo, hr.Days[0].Date)
	assert.Nil(t, hr.Days[0].OffpeakGridImportKwh)
	assert.Nil(t, hr.Days[0].OffpeakGridExportKwh)

	// yesterday: complete record — final deltas surface.
	assert.Equal(t, yesterday, hr.Days[1].Date)
	require.NotNil(t, hr.Days[1].OffpeakGridImportKwh)
	assert.InDelta(t, 2.4, *hr.Days[1].OffpeakGridImportKwh, 0.001)
	require.NotNil(t, hr.Days[1].OffpeakGridExportKwh)
	assert.InDelta(t, 0.6, *hr.Days[1].OffpeakGridExportKwh, 0.001)

	// today: pending record live-integrated from readings → 1.8 kWh import.
	assert.Equal(t, today, hr.Days[2].Date)
	require.NotNil(t, hr.Days[2].OffpeakGridImportKwh)
	assert.InDelta(t, 1.8, *hr.Days[2].OffpeakGridImportKwh, 0.01)
	require.NotNil(t, hr.Days[2].OffpeakGridExportKwh)
	assert.InDelta(t, 0.0, *hr.Days[2].OffpeakGridExportKwh, 0.01)
}

func TestHandleHistoryDynamoDBError(t *testing.T) {
	// Daily energy errors are still gating: no fallback. Readings errors
	// are tolerated per AC 4.9 — that case is exercised in
	// TestHandleHistory_TodayReadingsQueryFailure_AC4_9.
	mock := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return nil, errors.New("throttled")
		},
	}
	now := fixedNow()
	h := NewHandler(mock, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
	assert.Equal(t, "internal error", body["error"])
}

func TestHandleHistoryBundlesNotes(t *testing.T) {
	now := fixedNow()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	twoDaysAgo := now.AddDate(0, 0, -2).Format("2006-01-02")
	rangeStart := now.AddDate(0, 0, -6).Format("2006-01-02")

	dailyEnergy := []dynamo.DailyEnergyItem{
		{Date: twoDaysAgo, Epv: 10},
		{Date: yesterday, Epv: 11},
		{Date: today, Epv: 12},
	}

	t.Run("notes joined onto correct day, others null", func(t *testing.T) {
		mr := &mockReader{
			queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
				return dailyEnergy, nil
			},
			queryNotesFn: func(_ context.Context, _, start, end string) ([]dynamo.NoteItem, error) {
				assert.Equal(t, rangeStart, start, "history queries notes from start of requested window")
				assert.Equal(t, today, end, "history queries notes up to today")
				return []dynamo.NoteItem{
					{Date: yesterday, Text: "Quiet"},
					{Date: today, Text: "Busy"},
				}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), historyRequest(nil))
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		hr := parseHistoryResponse(t, resp)
		require.Len(t, hr.Days, 3)

		assert.Nil(t, hr.Days[0].Note, "two days ago has no note")
		require.NotNil(t, hr.Days[1].Note)
		assert.Equal(t, "Quiet", *hr.Days[1].Note)
		require.NotNil(t, hr.Days[2].Note)
		assert.Equal(t, "Busy", *hr.Days[2].Note)
	})

	t.Run("absent notes serialise as null", func(t *testing.T) {
		mr := &mockReader{
			queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
				return dailyEnergy, nil
			},
			queryNotesFn: func(_ context.Context, _, _, _ string) ([]dynamo.NoteItem, error) {
				return []dynamo.NoteItem{}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), historyRequest(nil))
		require.NoError(t, err)

		var raw struct {
			Days []map[string]json.RawMessage `json:"days"`
		}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &raw))
		require.Len(t, raw.Days, 3)
		for i, day := range raw.Days {
			require.Contains(t, day, "note", "day %d must always serialise note field", i)
			assert.Equal(t, "null", string(day["note"]), "day %d note should be null", i)
		}
	})

	t.Run("notes read failure leaves all notes nil and request 200", func(t *testing.T) {
		mr := &mockReader{
			queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
				return dailyEnergy, nil
			},
			queryNotesFn: func(_ context.Context, _, _, _ string) ([]dynamo.NoteItem, error) {
				return nil, errors.New("throttled")
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), historyRequest(nil))
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode, "/history must not 500 when only the notes read fails")

		hr := parseHistoryResponse(t, resp)
		require.Len(t, hr.Days, 3)
		for i, day := range hr.Days {
			assert.Nil(t, day.Note, "day %d note should be nil after failure", i)
		}
	})
}

// TestHandleHistoryRangeParamMatrix covers the two request forms of /history
// (req 5.1, 5.4, 5.6): the existing days=N form (including the no-params
// default of 7), the new explicit start/end range form, and every rejected
// combination. The range form is past-only per Decision 15: end must be
// strictly before the current Sydney date. Spans are inclusive on both ends
// and capped at 31 days; the cap must be calendar-aware so a window crossing
// the April DST fallback (Sydney, 2026-04-05) is not off by one.
func TestHandleHistoryRangeParamMatrix(t *testing.T) {
	now := fixedNow() // 2026-04-15 10:00 AEST; today = "2026-04-15"

	tests := map[string]struct {
		params     map[string]string
		wantStatus int
		// wantErr is the exact error body message when wantStatus is 400.
		wantErr string
		// wantStart/wantEnd are the inclusive bounds QueryDailyEnergy must
		// receive when wantStatus is 200.
		wantStart string
		wantEnd   string
	}{
		"no params defaults to days=7": {
			params: nil, wantStatus: 200,
			wantStart: "2026-04-09", wantEnd: "2026-04-15",
		},
		"days only unchanged": {
			params: map[string]string{"days": "14"}, wantStatus: 200,
			wantStart: "2026-04-02", wantEnd: "2026-04-15",
		},
		"days with start and end rejected": {
			params:     map[string]string{"days": "7", "start": "2026-04-01", "end": "2026-04-07"},
			wantStatus: 400, wantErr: "cannot combine days with start and end parameters",
		},
		"days with lone start rejected": {
			params:     map[string]string{"days": "7", "start": "2026-04-01"},
			wantStatus: 400, wantErr: "cannot combine days with start and end parameters",
		},
		"lone start rejected": {
			params:     map[string]string{"start": "2026-04-01"},
			wantStatus: 400, wantErr: "start and end must be supplied together",
		},
		"lone end rejected": {
			params:     map[string]string{"end": "2026-04-07"},
			wantStatus: 400, wantErr: "start and end must be supplied together",
		},
		"unparseable start rejected": {
			params:     map[string]string{"start": "01-04-2026", "end": "2026-04-07"},
			wantStatus: 400, wantErr: "invalid start or end parameter, must be YYYY-MM-DD",
		},
		"impossible end date rejected": {
			params:     map[string]string{"start": "2026-04-01", "end": "2026-04-31"},
			wantStatus: 400, wantErr: "invalid start or end parameter, must be YYYY-MM-DD",
		},
		"end before start rejected": {
			params:     map[string]string{"start": "2026-04-07", "end": "2026-04-01"},
			wantStatus: 400, wantErr: "end must not be before start",
		},
		"end equals today rejected": {
			params:     map[string]string{"start": "2026-04-09", "end": "2026-04-15"},
			wantStatus: 400, wantErr: "end must be before the current date",
		},
		"end after today rejected": {
			params:     map[string]string{"start": "2026-04-09", "end": "2026-04-20"},
			wantStatus: 400, wantErr: "end must be before the current date",
		},
		"single-day range ok": {
			params:     map[string]string{"start": "2026-04-10", "end": "2026-04-10"},
			wantStatus: 200, wantStart: "2026-04-10", wantEnd: "2026-04-10",
		},
		"31-day inclusive span ok": {
			params:     map[string]string{"start": "2026-03-01", "end": "2026-03-31"},
			wantStatus: 200, wantStart: "2026-03-01", wantEnd: "2026-03-31",
		},
		"32-day span rejected": {
			params:     map[string]string{"start": "2026-03-01", "end": "2026-04-01"},
			wantStatus: 400, wantErr: "date range must not exceed 31 days",
		},
		"31-day span crossing April DST fallback ok": {
			params:     map[string]string{"start": "2026-03-15", "end": "2026-04-14"},
			wantStatus: 200, wantStart: "2026-03-15", wantEnd: "2026-04-14",
		},
		"32-day span crossing April DST fallback rejected": {
			params:     map[string]string{"start": "2026-03-14", "end": "2026-04-14"},
			wantStatus: 400, wantErr: "date range must not exceed 31 days",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotStart, gotEnd string
			mr := &mockReader{
				queryDailyEnergyFn: func(_ context.Context, _, start, end string) ([]dynamo.DailyEnergyItem, error) {
					gotStart, gotEnd = start, end
					return []dynamo.DailyEnergyItem{}, nil
				},
			}

			h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), historyRequest(tc.params))
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, resp.StatusCode)

			if tc.wantStatus == 400 {
				var body map[string]string
				require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
				assert.Equal(t, tc.wantErr, body["error"])
				return
			}
			assert.Equal(t, tc.wantStart, gotStart, "QueryDailyEnergy inclusive start bound")
			assert.Equal(t, tc.wantEnd, gotEnd, "QueryDailyEnergy inclusive end bound")
		})
	}
}

// TestHandleHistoryRangeSkipsLiveCompute verifies the range form is
// stored-values-only (req 5.3, Decision 15): no readings query is issued, no
// energy reconciliation or live off-peak integration happens, and the
// off-peak and notes queries are bounded by the requested range rather than
// today.
func TestHandleHistoryRangeSkipsLiveCompute(t *testing.T) {
	now := fixedNow()
	const rangeStart = "2026-04-10"
	const rangeEnd = "2026-04-12"

	peak := 3.4
	tr := &trackingReader{mockReader: &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, start, end string) ([]dynamo.DailyEnergyItem, error) {
			assert.Equal(t, rangeStart, start)
			assert.Equal(t, rangeEnd, end)
			return []dynamo.DailyEnergyItem{
				{Date: "2026-04-10", Epv: 15, EInput: 4, EOutput: 1, ECharge: 8, EDischarge: 7},
				{Date: "2026-04-11", Epv: 16, EInput: 5, EOutput: 2, ECharge: 9, EDischarge: 8, PeakGridImportKwh: &peak},
				{Date: "2026-04-12", Epv: 17, EInput: 6, EOutput: 3, ECharge: 10, EDischarge: 9},
			}, nil
		},
		queryOffpeakFn: func(_ context.Context, _, start, end string) ([]dynamo.OffpeakItem, error) {
			assert.Equal(t, rangeStart, start, "offpeak query bounded by range start, not today's window")
			assert.Equal(t, rangeEnd, end, "offpeak query bounded by range end, not today")
			return []dynamo.OffpeakItem{
				// Complete record — final deltas pass through.
				{Date: "2026-04-10", Status: dynamo.OffpeakStatusComplete, GridUsageKwh: 2.4, GridExportKwh: 0.6},
				// Pending record on a past date: must NOT be live-integrated
				// (no readings exist on the range path); split stays absent.
				{Date: "2026-04-12", Status: dynamo.OffpeakStatusPending, StartEInput: 999, StartEOutput: 999},
			}, nil
		},
		queryNotesFn: func(_ context.Context, _, start, end string) ([]dynamo.NoteItem, error) {
			assert.Equal(t, rangeStart, start, "notes query bounded by range start")
			assert.Equal(t, rangeEnd, end, "notes query bounded by range end, not today")
			return []dynamo.NoteItem{{Date: "2026-04-11", Text: "Past note"}}, nil
		},
	}}

	h := NewHandler(tr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(map[string]string{
		"start": rangeStart, "end": rangeEnd,
	}))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	assert.Equal(t, int32(0), tr.queryReadingsCalls.Load(),
		"range form must not issue a readings query (Decision 15)")

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 3)

	// Stored energy totals pass through unreconciled.
	assert.Equal(t, "2026-04-10", hr.Days[0].Date)
	assert.Equal(t, 15.0, hr.Days[0].Epv)
	assert.Equal(t, 4.0, hr.Days[0].EInput)

	// Complete off-peak record surfaces its final deltas.
	require.NotNil(t, hr.Days[0].OffpeakGridImportKwh)
	assert.InDelta(t, 2.4, *hr.Days[0].OffpeakGridImportKwh, 0.001)
	require.NotNil(t, hr.Days[0].OffpeakGridExportKwh)
	assert.InDelta(t, 0.6, *hr.Days[0].OffpeakGridExportKwh, 0.001)

	// Stored peak grid import passes through; note joined onto its day.
	require.NotNil(t, hr.Days[1].PeakGridImportKwh)
	assert.InDelta(t, peak, *hr.Days[1].PeakGridImportKwh, 0.001)
	require.NotNil(t, hr.Days[1].Note)
	assert.Equal(t, "Past note", *hr.Days[1].Note)

	// Pending record on a past date: no live integration, split absent.
	assert.Nil(t, hr.Days[2].OffpeakGridImportKwh)
	assert.Nil(t, hr.Days[2].OffpeakGridExportKwh)
}

// TestHandleHistoryRangePredatesData verifies req 5.5: a range older than (or
// partially older than) stored data returns whichever days exist — possibly
// none — without error.
func TestHandleHistoryRangePredatesData(t *testing.T) {
	now := fixedNow()

	t.Run("range entirely before stored data returns empty without error", func(t *testing.T) {
		mr := &mockReader{
			queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
				return []dynamo.DailyEnergyItem{}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), historyRequest(map[string]string{
			"start": "2020-01-01", "end": "2020-01-31",
		}))
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Empty(t, parseHistoryResponse(t, resp).Days)
	})

	t.Run("range partially before stored data returns the existing subset", func(t *testing.T) {
		mr := &mockReader{
			queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
				// Storage only has the last two days of the requested month.
				return []dynamo.DailyEnergyItem{
					{Date: "2026-03-30", Epv: 12},
					{Date: "2026-03-31", Epv: 13},
				}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), historyRequest(map[string]string{
			"start": "2026-03-01", "end": "2026-03-31",
		}))
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		hr := parseHistoryResponse(t, resp)
		require.Len(t, hr.Days, 2)
		assert.Equal(t, "2026-03-30", hr.Days[0].Date)
		assert.Equal(t, "2026-03-31", hr.Days[1].Date)
	})
}

// TestHandleHistoryOffpeakSoftFailure verifies that an off-peak query
// failure does not take down the entire history response. The iOS grid
// card already degrades gracefully when the split is missing, so a
// throttle on the off-peak table should yield a 200 with the daily
// energy rows intact and no off-peak split fields populated.
func TestHandleHistoryOffpeakSoftFailure(t *testing.T) {
	now := fixedNow()
	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{
				{Date: "2026-04-14", Epv: 18, EInput: 4.5, EOutput: 1.2, ECharge: 9, EDischarge: 8},
			}, nil
		},
		queryOffpeakFn: func(_ context.Context, _, _, _ string) ([]dynamo.OffpeakItem, error) {
			return nil, errors.New("throttled")
		},
	}

	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 1)
	assert.Equal(t, "2026-04-14", hr.Days[0].Date)
	assert.Nil(t, hr.Days[0].OffpeakGridImportKwh)
	assert.Nil(t, hr.Days[0].OffpeakGridExportKwh)
}
