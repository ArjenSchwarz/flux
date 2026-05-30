package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uniformTodayReadings builds a constant-pgrid reading stream from Sydney
// midnight to end (inclusive) at 30s cadence — used to assert live peak wiring
// with an easily integrable signal.
func uniformTodayReadings(dayStart, end time.Time, pgridW float64) []dynamo.ReadingItem {
	out := []dynamo.ReadingItem{}
	for ts := dayStart.Unix(); ts <= end.Unix(); ts += 30 {
		out = append(out, dynamo.ReadingItem{Timestamp: ts, Pgrid: pgridW, Soc: 50})
	}
	return out
}

// TestHandleDayPeakGridImport covers the /day peakGridImportKwh field: present
// with the stored value when the daily-energy row carries it, and absent from
// the JSON when the stored value is nil (Decision 4 — no real-time path).
func TestHandleDayPeakGridImport(t *testing.T) {
	// A past date so the stored value is authoritative (today would have no
	// stored peak and fall through to the iOS fallback).
	date := "2026-04-10"

	t.Run("present uses stored value", func(t *testing.T) {
		peak := 0.42
		mr := &mockReader{
			getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
				return &dynamo.DailyEnergyItem{
					Date: date, EInput: 4.2, PeakGridImportKwh: &peak,
				}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = fixedNow

		resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		dr := parseDayResponse(t, resp)
		require.NotNil(t, dr.Summary)
		require.NotNil(t, dr.Summary.PeakGridImportKwh)
		assert.InDelta(t, 0.42, *dr.Summary.PeakGridImportKwh, 1e-9)
		assert.Contains(t, resp.Body, "peakGridImportKwh")
	})

	t.Run("absent omits the key", func(t *testing.T) {
		mr := &mockReader{
			getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
				return &dynamo.DailyEnergyItem{Date: date, EInput: 4.2}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = fixedNow

		resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		dr := parseDayResponse(t, resp)
		require.NotNil(t, dr.Summary)
		assert.Nil(t, dr.Summary.PeakGridImportKwh)
		assert.NotContains(t, resp.Body, "peakGridImportKwh", "omitempty must drop the key when nil")
	})
}

// TestHandleHistoryPeakGridImport covers the /history peakGridImportKwh field
// across rows: one carries the stored value, one does not.
func TestHandleHistoryPeakGridImport(t *testing.T) {
	now := fixedNow()
	peak := 0.37

	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{
				{Date: "2026-04-10", EInput: 2.3, PeakGridImportKwh: &peak},
				{Date: "2026-04-11", EInput: 3.0}, // no peak
			}, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(map[string]string{"days": "30"}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 2)

	byDate := map[string]DayEnergy{}
	for _, d := range hr.Days {
		byDate[d.Date] = d
	}

	withPeak := byDate["2026-04-10"]
	require.NotNil(t, withPeak.PeakGridImportKwh)
	assert.InDelta(t, 0.37, *withPeak.PeakGridImportKwh, 1e-9)

	without := byDate["2026-04-11"]
	assert.Nil(t, without.PeakGridImportKwh, "row without stored peak must omit the field")

	// The key appears (for the populated row) but exactly once.
	assert.Equal(t, 1, strings.Count(resp.Body, "peakGridImportKwh"),
		"only the populated row should carry peakGridImportKwh")
}

// TestHandleDayTodayLivePeakGridImport pins the T-1420 fix on /day: today's
// peak is integrated live from readings (not the stored value, not the off-peak
// residual) and is present even before the off-peak window opens — i.e. with no
// off-peak row at all (fixedNow is 10:00, before the 11:00 window).
func TestHandleDayTodayLivePeakGridImport(t *testing.T) {
	now := fixedNow() // 2026-04-15 10:00 AEST — before the 11:00 off-peak window.
	date := now.Format("2006-01-02")
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, sydneyTZ)
	readings := uniformTodayReadings(dayStart, now, 3600) // [00:00, 10:00].

	// Expected = the same live integration the handler performs, rounded.
	rawPeak, ok := livePeakGridImport(readings, now, "11:00", "14:00")
	require.True(t, ok)
	wantPeak := derivedstats.RoundEnergy(rawPeak)
	require.Greater(t, wantPeak, 0.0)

	const storedPeak = 99.0 // deliberately wrong — today must ignore stored.
	sp := storedPeak
	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{Date: date, EInput: 4.2, PeakGridImportKwh: &sp}, nil
		},
		// No off-peak row: proves peak renders independently of the off-peak split.
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return nil, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.PeakGridImportKwh, "today peak must be present before the off-peak window opens")
	assert.InDelta(t, wantPeak, *dr.Summary.PeakGridImportKwh, 1e-9, "today uses live integration")
	assert.NotEqual(t, storedPeak, *dr.Summary.PeakGridImportKwh, "today must ignore the stored value")
	assert.Nil(t, dr.Summary.OffpeakGridImportKwh, "no off-peak split before the window — peak still renders")
}

// TestHandleHistoryTodayLivePeakGridImport pins the same on /history: the today
// row carries a live-integrated peak even though it has no stored peak and no
// off-peak row.
func TestHandleHistoryTodayLivePeakGridImport(t *testing.T) {
	now := fixedNow()
	today := now.Format("2006-01-02")
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, sydneyTZ)
	readings := uniformTodayReadings(dayStart, now, 3600)

	rawPeak, ok := livePeakGridImport(readings, now, "11:00", "14:00")
	require.True(t, ok)
	wantPeak := derivedstats.RoundEnergy(rawPeak)

	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{{Date: today, EInput: 4.2}}, nil // today, no stored peak
		},
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		// No off-peak rows for today.
		queryOffpeakFn: func(_ context.Context, _, _, _ string) ([]dynamo.OffpeakItem, error) {
			return nil, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(map[string]string{"days": "7"}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 1)
	require.NotNil(t, hr.Days[0].PeakGridImportKwh, "today row must carry a live peak even without a stored value")
	assert.InDelta(t, wantPeak, *hr.Days[0].PeakGridImportKwh, 1e-9)
	assert.Nil(t, hr.Days[0].OffpeakGridImportKwh, "no off-peak split — peak still present")
}

// TestTodayPeakGridImportConsistentAcrossEndpoints is the core Data Consistency
// guarantee of Decision 9: /status, /day, and /history must return the SAME
// today peak for the same readings and now. The readings deliberately start
// before Sydney midnight so the three callers feed livePeakGridImport slices of
// different lengths (/status reads a rolling 24h, /day and /history are today-
// only) — the internal day-trim is what makes the three agree, and this test
// would fail without it. now is 16:00 so both peak windows contribute.
func TestTodayPeakGridImportConsistentAcrossEndpoints(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	today := now.Format("2006-01-02")

	// 30s cadence from 22:00 yesterday (pre-midnight) to 16:00 today, constant
	// 3600 W. Peak so far = morning [00:00, 11:00) + evening [14:00, 16:00) =
	// 13 h = 46.8 kWh; the pre-midnight tail must NOT leak in.
	start := time.Date(2026, 4, 14, 22, 0, 0, 0, sydneyTZ)
	readings := []dynamo.ReadingItem{}
	for ts := start.Unix(); ts <= now.Unix(); ts += 30 {
		readings = append(readings, dynamo.ReadingItem{Timestamp: ts, Pgrid: 3600, Soc: 50})
	}

	newHandler := func() *Handler {
		mr := &mockReader{
			queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
				return readings, nil
			},
			getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
				return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
			},
			getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) { return nil, nil },
			queryOffpeakFn: func(_ context.Context, _, _, _ string) ([]dynamo.OffpeakItem, error) {
				return nil, nil
			},
			getDailyEnergyFn: func(_ context.Context, _, date string) (*dynamo.DailyEnergyItem, error) {
				return &dynamo.DailyEnergyItem{Date: date, EInput: 3.0}, nil
			},
			queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
				return []dynamo.DailyEnergyItem{{Date: today, EInput: 3.0}}, nil
			},
		}
		h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
		h.nowFunc = func() time.Time { return now }
		return h
	}

	// /status
	statusResp, err := newHandler().Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	sr := parseStatusResponse(t, statusResp)
	require.NotNil(t, sr.PeakGridImportKwh)

	// /day (today)
	dayResp, err := newHandler().Handle(context.Background(), dayRequest(map[string]string{"date": today}))
	require.NoError(t, err)
	dr := parseDayResponse(t, dayResp)
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.PeakGridImportKwh)

	// /history (today row)
	histResp, err := newHandler().Handle(context.Background(), historyRequest(map[string]string{"days": "7"}))
	require.NoError(t, err)
	hr := parseHistoryResponse(t, histResp)
	var todayPeak *float64
	for _, d := range hr.Days {
		if d.Date == today {
			todayPeak = d.PeakGridImportKwh
		}
	}
	require.NotNil(t, todayPeak)

	// All three identical, and equal the expected 46.8 kWh (pre-midnight excluded).
	assert.InDelta(t, 46.8, *sr.PeakGridImportKwh, 0.01)
	assert.InDelta(t, *sr.PeakGridImportKwh, *dr.Summary.PeakGridImportKwh, 1e-9, "/status and /day must agree")
	assert.InDelta(t, *sr.PeakGridImportKwh, *todayPeak, 1e-9, "/status and /history must agree")
}

// TestHandleStatusLivePeakGridImport pins the new /status field (T-1421).
func TestHandleStatusLivePeakGridImport(t *testing.T) {
	now := fixedNow()
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, sydneyTZ)
	readings := uniformTodayReadings(dayStart, now, 3600)

	rawPeak, ok := livePeakGridImport(readings, now, "11:00", "14:00")
	require.True(t, ok)
	wantPeak := derivedstats.RoundEnergy(rawPeak)

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
		},
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return nil, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, date string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{Date: date, EInput: 3.0}, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.PeakGridImportKwh, "/status must expose today's live peak")
	assert.InDelta(t, wantPeak, *sr.PeakGridImportKwh, 1e-9)
}
