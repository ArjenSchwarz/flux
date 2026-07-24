package api

import (
	"context"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCrossHandlerEquivalence_PastDateDerivedStats covers AC 6.6: for a
// completed date whose row carries derivedStats, /day and /history must
// return field-equivalent derivedStats payloads (same field set, same
// values).
//
// /day publishes socLow/socLowTime under DaySummary; /history publishes them
// flat on each DayEnergy row (per the wire-shape note in design.md).
// Equivalence is asserted on the underlying values, not on the JSON path.
func TestCrossHandlerEquivalence_PastDateDerivedStats(t *testing.T) {
	const date = "2026-04-14"
	row := makePastDateRow(date)

	mr := &mockReader{
		// /day path
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return row, nil
		},
		// /history path
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{*row}, nil
		},
	}

	now := fixedNow()
	h := newTestHandlerFor(mr, nil, testSerial, testToken)
	h.nowFunc = func() time.Time { return now }

	dayResp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, dayResp.StatusCode)
	dr := parseDayResponse(t, dayResp)

	historyResp, err := h.Handle(context.Background(), historyRequest(map[string]string{"days": "7"}))
	require.NoError(t, err)
	require.Equal(t, 200, historyResp.StatusCode)
	hr := parseHistoryResponse(t, historyResp)

	// Find the matching day in the history response.
	var hday *DayEnergy
	for i := range hr.Days {
		if hr.Days[i].Date == date {
			hday = &hr.Days[i]
			break
		}
	}
	require.NotNil(t, hday, "history response must include the date")

	// DailyUsage equivalence — both sides go through the same converter.
	require.NotNil(t, dr.DailyUsage)
	require.NotNil(t, hday.DailyUsage)
	require.Equal(t, len(dr.DailyUsage.Blocks), len(hday.DailyUsage.Blocks))
	for i := range dr.DailyUsage.Blocks {
		a := dr.DailyUsage.Blocks[i]
		b := hday.DailyUsage.Blocks[i]
		assert.Equal(t, a.Kind, b.Kind)
		assert.Equal(t, a.Start, b.Start)
		assert.Equal(t, a.End, b.End)
		assert.InDelta(t, a.TotalKwh, b.TotalKwh, 1e-9)
		assert.Equal(t, a.PercentOfDay, b.PercentOfDay)
		assert.Equal(t, a.Status, b.Status)
		assert.Equal(t, a.BoundarySource, b.BoundarySource)
	}

	// PeakPeriods equivalence.
	require.Equal(t, len(dr.PeakPeriods), len(hday.PeakPeriods))
	for i := range dr.PeakPeriods {
		a := dr.PeakPeriods[i]
		b := hday.PeakPeriods[i]
		assert.Equal(t, a.Start, b.Start)
		assert.Equal(t, a.End, b.End)
		assert.InDelta(t, a.AvgLoadW, b.AvgLoadW, 1e-9)
		assert.InDelta(t, a.EnergyWh, b.EnergyWh, 1e-9)
	}

	// SocLow equivalence — wire-shape differs (DaySummary vs flat) but
	// values must match.
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.SocLow)
	require.NotNil(t, hday.SocLow)
	assert.InDelta(t, *dr.Summary.SocLow, *hday.SocLow, 1e-9)
	require.NotNil(t, dr.Summary.SocLowTime)
	require.NotNil(t, hday.SocLowTime)
	assert.Equal(t, *dr.Summary.SocLowTime, *hday.SocLowTime)
}

// TestCrossHandlerEquivalence_OldDateSummaryFields covers req 5.7 for the
// fields the derivedStats test above does not assert: energy totals, peak
// grid import, and the off-peak split. The date is more than 30 days before
// fixedNow, so its flux-readings rows would be TTL-expired — both handlers
// must serve every value from the same non-TTL flux-daily-energy and
// flux-offpeak rows, never recomputing from readings. /history is queried via
// the new range form, the path the History screen uses for past periods.
func TestCrossHandlerEquivalence_OldDateSummaryFields(t *testing.T) {
	// fixedNow is 2026-04-15; 2026-03-01 is 45 days earlier — readings gone.
	const date = "2026-03-01"

	peak := 2.75
	row := makePastDateRow(date)
	row.PeakGridImportKwh = &peak

	opRow := dynamo.OffpeakItem{
		SysSn: testSerial, Date: date, Status: dynamo.OffpeakStatusComplete,
		GridUsageKwh: 3.1, GridExportKwh: 0.9,
	}

	mr := &mockReader{
		// /day path
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return row, nil
		},
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &opRow, nil
		},
		// /history path
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{*row}, nil
		},
		queryOffpeakFn: func(_ context.Context, _, _, _ string) ([]dynamo.OffpeakItem, error) {
			return []dynamo.OffpeakItem{opRow}, nil
		},
	}

	now := fixedNow()
	h := newTestHandlerFor(mr, nil, testSerial, testToken)
	h.nowFunc = func() time.Time { return now }

	dayResp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	require.Equal(t, 200, dayResp.StatusCode)
	dr := parseDayResponse(t, dayResp)

	historyResp, err := h.Handle(context.Background(), historyRequest(map[string]string{
		"start": date, "end": date,
	}))
	require.NoError(t, err)
	require.Equal(t, 200, historyResp.StatusCode)
	hr := parseHistoryResponse(t, historyResp)

	require.Len(t, hr.Days, 1)
	hday := hr.Days[0]
	require.Equal(t, date, hday.Date)

	// Energy totals — /day publishes them as Summary pointers, /history flat.
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.Epv)
	assert.InDelta(t, *dr.Summary.Epv, hday.Epv, 1e-9)
	require.NotNil(t, dr.Summary.EInput)
	assert.InDelta(t, *dr.Summary.EInput, hday.EInput, 1e-9)
	require.NotNil(t, dr.Summary.EOutput)
	assert.InDelta(t, *dr.Summary.EOutput, hday.EOutput, 1e-9)
	require.NotNil(t, dr.Summary.ECharge)
	assert.InDelta(t, *dr.Summary.ECharge, hday.ECharge, 1e-9)
	require.NotNil(t, dr.Summary.EDischarge)
	assert.InDelta(t, *dr.Summary.EDischarge, hday.EDischarge, 1e-9)

	// Peak grid import — stored server-computed value on both paths.
	require.NotNil(t, dr.Summary.PeakGridImportKwh)
	require.NotNil(t, hday.PeakGridImportKwh)
	assert.InDelta(t, *dr.Summary.PeakGridImportKwh, *hday.PeakGridImportKwh, 1e-9)

	// Off-peak split — final deltas from the same complete flux-offpeak row.
	require.NotNil(t, dr.Summary.OffpeakGridImportKwh)
	require.NotNil(t, hday.OffpeakGridImportKwh)
	assert.InDelta(t, *dr.Summary.OffpeakGridImportKwh, *hday.OffpeakGridImportKwh, 1e-9)
	require.NotNil(t, dr.Summary.OffpeakGridExportKwh)
	require.NotNil(t, hday.OffpeakGridExportKwh)
	assert.InDelta(t, *dr.Summary.OffpeakGridExportKwh, *hday.OffpeakGridExportKwh, 1e-9)
}
