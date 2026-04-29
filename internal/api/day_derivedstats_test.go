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

	h := NewHandler(tr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return fixedNow() }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	dr := parseDayResponse(t, resp)
	require.NotNil(t, dr.DailyUsage)
	require.Len(t, dr.DailyUsage.Blocks, 1)
	assert.Equal(t, derivedstats.DailyUsageKindNight, dr.DailyUsage.Blocks[0].Kind)
	require.NotNil(t, dr.PeakPeriods)
	require.NotEmpty(t, dr.PeakPeriods)

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
	h := NewHandler(tr, nil, testSerial, testToken, "11:00", "14:00")
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
	h := NewHandler(tr, nil, testSerial, testToken, "11:00", "14:00")
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
	assert.Equal(t, roundEnergy(15.5), *dr.Summary.Epv)
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
	h := NewHandler(tr, nil, testSerial, testToken, "11:00", "14:00")
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

	h := NewHandler(tr, nil, testSerial, testToken, "11:00", "14:00")
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
