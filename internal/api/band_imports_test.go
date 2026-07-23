package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers the per-band import split on the read endpoints: stored
// values for past days, a live integration for today, and the requirement
// that /day and /history report the identical numbers (AC 3.4).

// touPlan is the incoming time-of-use plan (Q3). Its rated segments are
// 00:00-01:00, 01:00-06:00, 06:00-10:00 and 15:00-24:00 — the free band and
// the default remainder either side of it.
func touPlan(id string) dynamo.PricingItem {
	return planRow(id, "2000-01-01", nil,
		freeBand("10:00", "15:00"),
		ratedBand("01:00", "06:00", 0.28))
}

func TestDay_PastDayServesStoredBandImports(t *testing.T) {
	// AC 3.5: the captured split outlives the readings TTL, so a past day is
	// served straight from storage.
	now := time.Date(2026, 4, 15, 10, 0, 0, 0, sydneyTZ)
	const date = "2026-04-10"
	stored := []dynamo.BandImportAttr{
		{Start: "00:00", End: "01:00", Kwh: 1.1},
		{Start: "01:00", End: "06:00", Kwh: 5.5},
		{Start: "06:00", End: "10:00", Kwh: 4.4},
		{Start: "15:00", End: "24:00", Kwh: 9.9},
	}
	mr := &mockReader{
		getDailyEnergyFn: func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{
				SysSn: serial, Date: d, EInput: 21,
				BandImports: stored, BandsComputedAt: "2026-04-11T00:05:00Z",
			}, nil
		},
	}
	h := handlerWithPlans(mr, touPlan("p"))
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var dr DayDetailResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &dr))
	require.NotNil(t, dr.Summary)
	require.Len(t, dr.Summary.BandImports, 4)
	for i, want := range stored {
		assert.Equal(t, want.Start, dr.Summary.BandImports[i].Start)
		assert.Equal(t, want.End, dr.Summary.BandImports[i].End)
		assert.InDelta(t, want.Kwh, dr.Summary.BandImports[i].Kwh, 1e-9)
	}
}

func TestDay_PastDayWithoutStoredSplitOmitsBandImports(t *testing.T) {
	// A pre-feature row has no split; the client falls back to the tier-2 or
	// tier-3 cost path rather than seeing an empty array it might read as
	// "zero import in every band".
	now := time.Date(2026, 4, 15, 10, 0, 0, 0, sydneyTZ)
	mr := &mockReader{
		getDailyEnergyFn: func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 21}, nil
		},
	}
	h := handlerWithPlans(mr, touPlan("p"))
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": "2026-04-10"}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	assert.NotContains(t, resp.Body, "bandImports", "absent, not an empty array")
}

func TestDay_TodayBandImportsIntegratedLive(t *testing.T) {
	// AC 3.4: today has no stored split yet, so it is integrated from
	// readings. A steady 1 kW import makes each band's expected value its
	// elapsed length in hours; the final band is clamped to now.
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	const date = "2026-04-15"
	mr := readerWithReadings(steadyImportReadings(now, 1000))
	mr.getDailyEnergyFn = func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
		return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 16}, nil
	}
	h := handlerWithPlans(mr, touPlan("p"))
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var dr DayDetailResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &dr))
	require.NotNil(t, dr.Summary)
	want := []BandImport{
		{Start: "00:00", End: "01:00", Kwh: 1},
		{Start: "01:00", End: "06:00", Kwh: 5},
		{Start: "06:00", End: "10:00", Kwh: 4},
		{Start: "15:00", End: "24:00", Kwh: 1}, // 15:00-16:00 elapsed
	}
	require.Len(t, dr.Summary.BandImports, len(want))
	for i, w := range want {
		assert.Equal(t, w.Start, dr.Summary.BandImports[i].Start)
		assert.Equal(t, w.End, dr.Summary.BandImports[i].End)
		assert.InDelta(t, w.Kwh, dr.Summary.BandImports[i].Kwh, 0.05)
	}
	// The free band is not in the list — the flux-offpeak row owns that kWh
	// exclusively (Q31).
	for _, b := range dr.Summary.BandImports {
		assert.NotEqual(t, "10:00", b.Start, "the free band must not appear in bandImports")
	}
}

func TestDayAndHistoryAgreeOnTodaysBandImports(t *testing.T) {
	// AC 3.4 and the project's data-consistency rule: both endpoints derive
	// today's split from the same helper, so the numbers are identical.
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	const date = "2026-04-15"
	readings := steadyImportReadings(now, 1000)
	mr := readerWithReadings(readings)
	mr.getDailyEnergyFn = func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
		return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 16}, nil
	}
	mr.queryDailyEnergyFn = func(_ context.Context, serial, _, _ string) ([]dynamo.DailyEnergyItem, error) {
		return []dynamo.DailyEnergyItem{{SysSn: serial, Date: date, EInput: 16}}, nil
	}
	h := handlerWithPlans(mr, touPlan("p"))
	h.nowFunc = func() time.Time { return now }

	dayResp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, dayResp.StatusCode)
	var dr DayDetailResponse
	require.NoError(t, json.Unmarshal([]byte(dayResp.Body), &dr))

	histReq := makeRequest("GET", "/history", "Bearer "+testToken)
	histReq.QueryStringParameters = map[string]string{"days": "1"}
	histResp, err := h.Handle(context.Background(), histReq)
	require.NoError(t, err)
	require.Equal(t, 200, histResp.StatusCode)
	var hr HistoryResponse
	require.NoError(t, json.Unmarshal([]byte(histResp.Body), &hr))

	require.NotNil(t, dr.Summary)
	require.Len(t, hr.Days, 1)
	require.NotEmpty(t, dr.Summary.BandImports)
	assert.Equal(t, dr.Summary.BandImports, hr.Days[0].BandImports,
		"/day and /history must show the identical split for the same day")
}

func TestHistory_PastRowServesStoredBandImports(t *testing.T) {
	now := time.Date(2026, 4, 15, 10, 0, 0, 0, sydneyTZ)
	stored := []dynamo.BandImportAttr{
		{Start: "00:00", End: "01:00", Kwh: 1.1},
		{Start: "01:00", End: "06:00", Kwh: 5.5},
		{Start: "06:00", End: "10:00", Kwh: 4.4},
		{Start: "15:00", End: "24:00", Kwh: 9.9},
	}
	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, serial, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{
				{SysSn: serial, Date: "2026-04-13", EInput: 21, BandImports: stored, BandsComputedAt: "x"},
				{SysSn: serial, Date: "2026-04-14", EInput: 20},
			}, nil
		},
	}
	h := handlerWithPlans(mr, touPlan("p"))
	h.nowFunc = func() time.Time { return now }

	req := makeRequest("GET", "/history", "Bearer "+testToken)
	req.QueryStringParameters = map[string]string{"days": "7"}
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var hr HistoryResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &hr))
	require.Len(t, hr.Days, 2)
	require.Len(t, hr.Days[0].BandImports, 4)
	assert.InDelta(t, 5.5, hr.Days[0].BandImports[1].Kwh, 1e-9)
	assert.Nil(t, hr.Days[1].BandImports, "a row without a captured split carries none")
}

func TestStatus_DoesNotCarryBandImports(t *testing.T) {
	// Q29: the Dashboard shows no costs, so /status has no use for the split.
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	mr := readerWithReadings(steadyImportReadings(now, 1000))
	mr.getDailyEnergyFn = func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
		return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 16}, nil
	}
	h := handlerWithPlans(mr, touPlan("p"))
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	assert.False(t, strings.Contains(resp.Body, "bandImports"),
		"/status must not carry the per-band split")
}

func TestDay_TodayBandImportsAbsentWhenNoPlan(t *testing.T) {
	// An unpriced day has no band geometry to report (AC 2.7).
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	mr := readerWithReadings(steadyImportReadings(now, 1000))
	mr.getDailyEnergyFn = func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
		return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 16}, nil
	}
	h := handlerWithPlans(mr)
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": "2026-04-15"}))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	assert.NotContains(t, resp.Body, "bandImports")
}

func TestLiveBandImports_UnavailableWhenAStartedBandCannotIntegrate(t *testing.T) {
	// AC 3.6: a partially known split counts as unavailable, so the client
	// falls back rather than pricing some bands at zero.
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	midnight := startOfDaySydney(now)
	// Readings only from 12:00 onwards: the 00:00-01:00 band has started but
	// has no samples at all.
	var readings []dynamo.ReadingItem
	for ts := midnight.Add(12 * time.Hour); !ts.After(now); ts = ts.Add(time.Minute) {
		readings = append(readings, dynamo.ReadingItem{Timestamp: ts.Unix(), Pgrid: 1000})
	}

	_, ok := liveBandImports(readings, now, touPlan("p").Plan())
	assert.False(t, ok, "an elapsed band with no usable samples makes the split unavailable")
}
