package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dailyEnergyWithDerived builds a DailyEnergyItem for the given past date
// carrying all three derivedStats fields.
func dailyEnergyWithDerived(date string, percent int) dynamo.DailyEnergyItem {
	avg := 1.0
	return dynamo.DailyEnergyItem{
		SysSn: testSerial, Date: date,
		Epv: 12.0, EInput: 3.0, EOutput: 1.5, ECharge: 7.0, EDischarge: 5.0,
		DailyUsage: &dynamo.DailyUsageAttr{
			Blocks: []dynamo.DailyUsageBlockAttr{
				{
					Kind:              derivedstats.DailyUsageKindNight,
					Start:             date + "T14:00:00Z",
					End:               date + "T20:30:00Z",
					TotalKwh:          1.5,
					AverageKwhPerHour: &avg,
					PercentOfDay:      percent,
					Status:            derivedstats.DailyUsageStatusComplete,
					BoundarySource:    derivedstats.DailyUsageBoundaryReadings,
				},
			},
		},
		SocLow:                 &dynamo.SocLowAttr{Soc: 25, Timestamp: date + "T19:45:00Z"},
		PeakPeriods:            []dynamo.PeakPeriodAttr{{Start: date + "T22:00:00Z", End: date + "T22:30:00Z", AvgLoadW: 3000, EnergyWh: 1500}},
		DerivedStatsComputedAt: date + "T22:30:00Z",
	}
}

func TestHandleHistory_AllPastRowsHaveDerivedStats(t *testing.T) {
	now := fixedNow()
	loc, _ := time.LoadLocation("Australia/Sydney")
	today := now.In(loc).Format("2006-01-02")
	_ = today

	// Build 7-day window of past rows (excluding today since fixedNow pins
	// today as 2026-04-15).
	dates := []string{"2026-04-09", "2026-04-10", "2026-04-11", "2026-04-12", "2026-04-13", "2026-04-14"}
	rows := make([]dynamo.DailyEnergyItem, 0, len(dates))
	for i, d := range dates {
		rows = append(rows, dailyEnergyWithDerived(d, 10+i))
	}

	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return rows, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(map[string]string{"days": "7"}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, len(dates))
	for i, day := range hr.Days {
		require.NotNil(t, day.DailyUsage, "day %s should have DailyUsage", day.Date)
		assert.Equal(t, 10+i, day.DailyUsage.Blocks[0].PercentOfDay)
		require.NotNil(t, day.SocLow)
		assert.Equal(t, 25.0, *day.SocLow)
		require.NotNil(t, day.SocLowTime)
		require.NotEmpty(t, day.PeakPeriods)
	}
}

func TestHandleHistory_OldestDayLacksDerivedFields(t *testing.T) {
	now := fixedNow()
	rowOld := dynamo.DailyEnergyItem{ // pre-feature row
		SysSn: testSerial, Date: "2026-04-09",
		Epv: 10, EInput: 2, EOutput: 1, ECharge: 5, EDischarge: 4,
	}
	rowMid := dailyEnergyWithDerived("2026-04-12", 14)
	rowRecent := dailyEnergyWithDerived("2026-04-14", 18)

	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{rowOld, rowMid, rowRecent}, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(map[string]string{"days": "7"}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 3)
	// Old day: derivedStats absent.
	assert.Nil(t, hr.Days[0].DailyUsage)
	assert.Nil(t, hr.Days[0].SocLow)
	assert.Nil(t, hr.Days[0].SocLowTime)
	assert.Empty(t, hr.Days[0].PeakPeriods)
	// Mid + recent: present.
	require.NotNil(t, hr.Days[1].DailyUsage)
	require.NotNil(t, hr.Days[2].DailyUsage)
}

func TestHandleHistory_TodayLiveCompute(t *testing.T) {
	now := fixedNow()
	loc, _ := time.LoadLocation("Australia/Sydney")
	today := now.In(loc).Format("2006-01-02")

	// Today's stored row: no derivedStats yet (poller only summarises
	// yesterday). The handler must live-compute via allReadings.
	todayRow := dynamo.DailyEnergyItem{
		SysSn: testSerial, Date: today,
		Epv: 0.5, EInput: 0, EOutput: 0, ECharge: 0, EDischarge: 0.1,
	}

	// Past row with derivedStats from storage.
	pastRow := dailyEnergyWithDerived("2026-04-14", 22)

	// Today readings — three readings spaced within 60s so live integration
	// produces non-zero energy.
	todayReadings := []dynamo.ReadingItem{
		{Timestamp: time.Date(2026, 4, 15, 8, 0, 0, 0, loc).Unix(), Ppv: 2000, Pload: 600, Pbat: 0, Pgrid: -1400, Soc: 80},
		{Timestamp: time.Date(2026, 4, 15, 8, 0, 30, 0, loc).Unix(), Ppv: 2200, Pload: 700, Pbat: -200, Pgrid: -1300, Soc: 81},
		{Timestamp: time.Date(2026, 4, 15, 9, 0, 0, 0, loc).Unix(), Ppv: 3000, Pload: 800, Pbat: -500, Pgrid: 0, Soc: 60},
		{Timestamp: time.Date(2026, 4, 15, 9, 0, 30, 0, loc).Unix(), Ppv: 3100, Pload: 900, Pbat: -600, Pgrid: 0, Soc: 59},
	}

	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{pastRow, todayRow}, nil
		},
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return todayReadings, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(map[string]string{"days": "7"}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 2)

	// First (past) row: derivedStats from storage.
	require.Equal(t, pastRow.Date, hr.Days[0].Date)
	require.NotNil(t, hr.Days[0].DailyUsage)
	require.NotNil(t, hr.Days[0].SocLow)

	// Today row: derivedStats from live compute.
	require.Equal(t, today, hr.Days[1].Date)
	// SocLow live-computed from readings.
	require.NotNil(t, hr.Days[1].SocLow, "today row should carry live-computed SocLow")
	assert.Equal(t, 59.0, *hr.Days[1].SocLow)
}

func TestHandleHistory_TodayReadingsQueryFailure_AC4_9(t *testing.T) {
	// AC 4.9: today readings query failure → today row served with energy
	// only, derivedStats omitted, rest of range unaffected.
	now := fixedNow()
	loc, _ := time.LoadLocation("Australia/Sydney")
	today := now.In(loc).Format("2006-01-02")

	pastRow := dailyEnergyWithDerived("2026-04-14", 22)
	todayRow := dynamo.DailyEnergyItem{
		SysSn: testSerial, Date: today,
		Epv: 0.5, EInput: 0, EOutput: 0, ECharge: 0, EDischarge: 0.1,
	}

	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{pastRow, todayRow}, nil
		},
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return nil, errors.New("throttled")
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), historyRequest(map[string]string{"days": "7"}))
	require.NoError(t, err)
	// AC 4.9: response is NOT an error.
	require.Equal(t, 200, resp.StatusCode)

	hr := parseHistoryResponse(t, resp)
	require.Len(t, hr.Days, 2)

	// Past row unaffected.
	assert.NotNil(t, hr.Days[0].DailyUsage)
	assert.NotNil(t, hr.Days[0].SocLow)

	// Today row: energy still served, derivedStats omitted.
	assert.Equal(t, roundEnergy(0.5), hr.Days[1].Epv, "energy still served on today row")
	assert.Nil(t, hr.Days[1].DailyUsage, "derivedStats omitted when readings query fails")
	assert.Nil(t, hr.Days[1].SocLow)
	assert.Nil(t, hr.Days[1].SocLowTime)
	assert.Empty(t, hr.Days[1].PeakPeriods)
}
