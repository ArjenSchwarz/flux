package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trackingReader wraps mockReader and counts QueryReadings calls so the
// AC 3.5 regression test can assert past-date /day issues no readings query.
type trackingReader struct {
	*mockReader
	queryReadingsCalls atomic.Int32
}

func (t *trackingReader) QueryReadings(ctx context.Context, serial string, from, to int64) ([]dynamo.ReadingItem, error) {
	t.queryReadingsCalls.Add(1)
	return t.mockReader.QueryReadings(ctx, serial, from, to)
}

func makePastDateRow(date string) *dynamo.DailyEnergyItem {
	avg := 1.5
	morningSolar := 1.2
	offPeakSolar := 4.8
	afternoonSolar := 2.6
	return &dynamo.DailyEnergyItem{
		SysSn: testSerial, Date: date,
		Epv: 15.5, EInput: 4.2, EOutput: 2.1, ECharge: 8.0, EDischarge: 6.5,
		DailyUsage: &dynamo.DailyUsageAttr{
			Blocks: []dynamo.DailyUsageBlockAttr{
				{
					Kind:              derivedstats.DailyUsageKindNight,
					Start:             "2026-04-13T14:00:00Z",
					End:               "2026-04-13T20:30:00Z",
					TotalKwh:          1.8,
					AverageKwhPerHour: &avg,
					PercentOfDay:      12,
					Status:            derivedstats.DailyUsageStatusComplete,
					BoundarySource:    derivedstats.DailyUsageBoundaryReadings,
				},
				{
					Kind:              derivedstats.DailyUsageKindMorningPeak,
					Start:             "2026-04-13T20:30:00Z",
					End:               "2026-04-14T01:00:00Z",
					TotalKwh:          2.4,
					SolarKwh:          &morningSolar,
					AverageKwhPerHour: &avg,
					PercentOfDay:      18,
					Status:            derivedstats.DailyUsageStatusComplete,
					BoundarySource:    derivedstats.DailyUsageBoundaryReadings,
				},
				{
					Kind:              derivedstats.DailyUsageKindOffPeak,
					Start:             "2026-04-14T01:00:00Z",
					End:               "2026-04-14T04:00:00Z",
					TotalKwh:          1.5,
					SolarKwh:          &offPeakSolar,
					AverageKwhPerHour: &avg,
					PercentOfDay:      14,
					Status:            derivedstats.DailyUsageStatusComplete,
					BoundarySource:    derivedstats.DailyUsageBoundaryReadings,
				},
				{
					Kind:              derivedstats.DailyUsageKindAfternoonPeak,
					Start:             "2026-04-14T04:00:00Z",
					End:               "2026-04-14T07:30:00Z",
					TotalKwh:          3.1,
					SolarKwh:          &afternoonSolar,
					AverageKwhPerHour: &avg,
					PercentOfDay:      24,
					Status:            derivedstats.DailyUsageStatusComplete,
					BoundarySource:    derivedstats.DailyUsageBoundaryReadings,
				},
				{
					Kind:              derivedstats.DailyUsageKindEvening,
					Start:             "2026-04-14T07:30:00Z",
					End:               "2026-04-14T14:00:00Z",
					TotalKwh:          2.8,
					AverageKwhPerHour: &avg,
					PercentOfDay:      32,
					Status:            derivedstats.DailyUsageStatusComplete,
					BoundarySource:    derivedstats.DailyUsageBoundaryReadings,
				},
			},
		},
		SocLow:                 &dynamo.SocLowAttr{Soc: 22, Timestamp: "2026-04-14T19:45:00Z"},
		PeakPeriods:            []dynamo.PeakPeriodAttr{{Start: "2026-04-14T22:00:00Z", End: "2026-04-14T22:30:00Z", AvgLoadW: 3500, EnergyWh: 1750}},
		DerivedStatsComputedAt: "2026-04-15T00:30:00Z",
	}
}

func TestHandleDay_PastDate_AllDerivedFieldsPresent(t *testing.T) {
	const date = "2026-04-14"
	row := makePastDateRow(date)
	tr := &trackingReader{mockReader: &mockReader{
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return row, nil
		},
	}}

	h := newTestHandlerFor(tr, nil, testSerial, testToken)
	h.nowFunc = func() time.Time { return fixedNow() }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.DailyUsage)
	require.Len(t, dr.DailyUsage.Blocks, 5)
	assert.Equal(t, derivedstats.DailyUsageKindNight, dr.DailyUsage.Blocks[0].Kind)
	require.NotNil(t, dr.PeakPeriods)
	require.NotEmpty(t, dr.PeakPeriods)

	// Daylight blocks (morningPeak, offPeak, afternoonPeak) carry solarKwh;
	// night and evening blocks must not.
	byKind := map[string]derivedstats.DailyUsageBlock{}
	for _, b := range dr.DailyUsage.Blocks {
		byKind[b.Kind] = b
	}
	require.NotNil(t, byKind[derivedstats.DailyUsageKindMorningPeak].SolarKwh)
	assert.Equal(t, 1.2, *byKind[derivedstats.DailyUsageKindMorningPeak].SolarKwh)
	require.NotNil(t, byKind[derivedstats.DailyUsageKindOffPeak].SolarKwh)
	assert.Equal(t, 4.8, *byKind[derivedstats.DailyUsageKindOffPeak].SolarKwh)
	require.NotNil(t, byKind[derivedstats.DailyUsageKindAfternoonPeak].SolarKwh)
	assert.Equal(t, 2.6, *byKind[derivedstats.DailyUsageKindAfternoonPeak].SolarKwh)
	assert.Nil(t, byKind[derivedstats.DailyUsageKindNight].SolarKwh, "night block must not carry solarKwh")
	assert.Nil(t, byKind[derivedstats.DailyUsageKindEvening].SolarKwh, "evening block must not carry solarKwh")

	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.SocLow)
	assert.Equal(t, 22.0, *dr.Summary.SocLow)

	// AC 3.5 regression: past dates issue no QueryReadings.
	assert.Zero(t, tr.queryReadingsCalls.Load(), "past-date /day must not issue QueryReadings")
}

func TestHandleDay_PastDate_OneFieldAbsent(t *testing.T) {
	const date = "2026-04-14"
	row := makePastDateRow(date)
	row.SocLow = nil // simulate one field missing

	tr := &trackingReader{mockReader: &mockReader{
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return row, nil
		},
	}}
	h := newTestHandlerFor(tr, nil, testSerial, testToken)
	h.nowFunc = func() time.Time { return fixedNow() }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.DailyUsage, "dailyUsage still present")
	require.NotNil(t, dr.PeakPeriods)
	require.NotNil(t, dr.Summary)
	assert.Nil(t, dr.Summary.SocLow, "missing SocLow attribute → omitted from summary")
	assert.Nil(t, dr.Summary.SocLowTime)
}

func TestHandleDay_PastDate_AllDerivedFieldsAbsent(t *testing.T) {
	// Pre-feature row: only the AlphaESS energy fields are present.
	const date = "2026-04-14"
	row := &dynamo.DailyEnergyItem{
		SysSn: testSerial, Date: date,
		Epv: 15.5, EInput: 4.2, EOutput: 2.1, ECharge: 8.0, EDischarge: 6.5,
	}

	tr := &trackingReader{mockReader: &mockReader{
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return row, nil
		},
	}}
	h := newTestHandlerFor(tr, nil, testSerial, testToken)
	h.nowFunc = func() time.Time { return fixedNow() }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	assert.Nil(t, dr.DailyUsage, "dailyUsage section omitted")
	assert.Empty(t, dr.PeakPeriods, "peakPeriods empty")
	require.NotNil(t, dr.Summary)
	assert.Nil(t, dr.Summary.SocLow)
	assert.Nil(t, dr.Summary.SocLowTime)
	// Energy fields still come through.
	require.NotNil(t, dr.Summary.Epv)
	assert.Equal(t, derivedstats.RoundEnergy(15.5), *dr.Summary.Epv)
}

func TestHandleDay_PastDate_NoDerivedStats_FallsBackToDailyPower(t *testing.T) {
	// AC 3.5 regression: past dates with no readings AND no derivedStats
	// must continue rendering via the flux-daily-power fallback.
	const date = "2026-04-14"

	tr := &trackingReader{mockReader: &mockReader{
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil // no row
		},
		queryDailyPowerFn: func(_ context.Context, _, _ string) ([]dynamo.DailyPowerItem, error) {
			return []dynamo.DailyPowerItem{
				{UploadTime: "2026-04-14 08:00:00", Cbat: 85},
				{UploadTime: "2026-04-14 12:00:00", Cbat: 95},
				{UploadTime: "2026-04-14 18:00:00", Cbat: 45},
			}, nil
		},
	}}
	h := newTestHandlerFor(tr, nil, testSerial, testToken)
	h.nowFunc = func() time.Time { return fixedNow() }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotEmpty(t, dr.Readings, "daily-power chart still renders")
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.SocLow, "socLow available from daily-power fallback")
	assert.Equal(t, roundPower(45), *dr.Summary.SocLow)

	// AC 3.5: must NOT issue a QueryReadings on past dates.
	assert.Zero(t, tr.queryReadingsCalls.Load())
}

// TestHandleDay_Today_SolarKwh_OnDaylightBlocks exercises the live-compute path
// for today and asserts the daylight blocks carry solarKwh while night/evening
// do not. Uses tight reading pairs so integratePpv has gap-eligible samples in
// each daylight window.
func TestHandleDay_Today_SolarKwh_OnDaylightBlocks(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	now := time.Date(2026, 4, 15, 12, 30, 0, 0, loc) // mid off-peak window
	date := now.Format("2006-01-02")

	readings := []dynamo.ReadingItem{
		// Sunrise pair → seeds firstSolar inside the morning peak boundary.
		{Timestamp: time.Date(2026, 4, 15, 7, 30, 0, 0, loc).Unix(), Ppv: 800, Pload: 500, Soc: 90},
		{Timestamp: time.Date(2026, 4, 15, 7, 30, 30, 0, loc).Unix(), Ppv: 1000, Pload: 500, Soc: 89},
		// Morning peak pair — produces a non-zero solarKwh on that block.
		{Timestamp: time.Date(2026, 4, 15, 9, 0, 0, 0, loc).Unix(), Ppv: 2000, Pload: 600, Soc: 80},
		{Timestamp: time.Date(2026, 4, 15, 9, 0, 30, 0, loc).Unix(), Ppv: 2100, Pload: 600, Soc: 81},
		// Off-peak pair early in the window.
		{Timestamp: time.Date(2026, 4, 15, 12, 0, 0, 0, loc).Unix(), Ppv: 3000, Pload: 800, Soc: 95},
		{Timestamp: time.Date(2026, 4, 15, 12, 0, 30, 0, loc).Unix(), Ppv: 3100, Pload: 800, Soc: 95},
		// Recent pair within [now-5m, now] so the today-gate fires (solarStillUp=true).
		{Timestamp: time.Date(2026, 4, 15, 12, 29, 0, 0, loc).Unix(), Ppv: 2800, Pload: 700, Soc: 70},
		{Timestamp: time.Date(2026, 4, 15, 12, 29, 30, 0, loc).Unix(), Ppv: 2800, Pload: 700, Soc: 70},
	}

	tr := &trackingReader{mockReader: &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil
		},
	}}

	h := newTestHandlerFor(tr, nil, testSerial, testToken)
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.DailyUsage)
	byKind := map[string]derivedstats.DailyUsageBlock{}
	for _, b := range dr.DailyUsage.Blocks {
		byKind[b.Kind] = b
	}

	// Daylight blocks present in this in-progress layout (morning peak +
	// off-peak; afternoon peak starts at 14:00 which is after now=12:30, so
	// future-omit drops it). The today-gate also drops evening.
	mp, ok := byKind[derivedstats.DailyUsageKindMorningPeak]
	require.True(t, ok, "morning peak block must be present")
	require.NotNil(t, mp.SolarKwh, "morningPeak must carry solarKwh from live readings")
	assert.Greater(t, *mp.SolarKwh, 0.0)

	op, ok := byKind[derivedstats.DailyUsageKindOffPeak]
	require.True(t, ok, "off-peak block must be present")
	require.NotNil(t, op.SolarKwh, "offPeak must carry solarKwh from live readings")
	assert.Greater(t, *op.SolarKwh, 0.0)

	// Night block has no solarKwh by kind regardless of readings.
	if night, ok := byKind[derivedstats.DailyUsageKindNight]; ok {
		assert.Nil(t, night.SolarKwh, "night block must not carry solarKwh")
	}
	// Evening is omitted by the today-gate; afternoon peak is omitted by
	// future-omit. If either survived we'd want to assert their solar field.
	if ap, ok := byKind[derivedstats.DailyUsageKindAfternoonPeak]; ok {
		require.NotNil(t, ap.SolarKwh, "afternoonPeak when present must carry solarKwh")
	}
	if ev, ok := byKind[derivedstats.DailyUsageKindEvening]; ok {
		assert.Nil(t, ev.SolarKwh, "evening block must not carry solarKwh")
	}
}

func TestHandleDay_Today_LiveCompute_Unchanged(t *testing.T) {
	// Today path stays live-compute; readings query is issued.
	loc, _ := time.LoadLocation("Australia/Sydney")
	now := time.Date(2026, 4, 15, 18, 0, 0, 0, loc)
	date := now.Format("2006-01-02")

	readings := []dynamo.ReadingItem{
		{Timestamp: time.Date(2026, 4, 15, 8, 0, 0, 0, loc).Unix(), Ppv: 1000, Pload: 500, Soc: 80},
		{Timestamp: time.Date(2026, 4, 15, 12, 0, 0, 0, loc).Unix(), Ppv: 3000, Pload: 800, Soc: 95},
		{Timestamp: time.Date(2026, 4, 15, 17, 0, 0, 0, loc).Unix(), Ppv: 0, Pload: 1200, Soc: 40},
	}

	tr := &trackingReader{mockReader: &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return nil, nil
		},
	}}

	h := newTestHandlerFor(tr, nil, testSerial, testToken)
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.SocLow)
	assert.Equal(t, roundPower(40), *dr.Summary.SocLow)

	// Today: QueryReadings IS called.
	assert.Equal(t, int32(1), tr.queryReadingsCalls.Load())
}
