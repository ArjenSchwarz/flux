package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The client's banded cost tier needs to know which window a day's off-peak
// import was integrated under and whether that integration was a real
// measurement — otherwise a day priced by the new plan can never resolve past
// the fallback tier. These tests pin that provenance onto the /day and
// /history wire.

func TestDay_ServesTheOffpeakRowsGeometryAndProvenance(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, sydneyTZ)
	const date = "2026-08-15"
	opRow := dynamo.OffpeakItem{
		SysSn: testSerial, Date: date, Status: dynamo.OffpeakStatusComplete,
		GridUsageKwh: 3.1, GridExportKwh: 0.9,
		WindowStart: "10:00", WindowEnd: "15:00",
		IntegratedAt: "2026-08-16T05:00:00Z", IntegrationSampleCount: 1500,
	}
	mr := &mockReader{
		getDailyEnergyFn: func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 21}, nil
		},
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &opRow, nil
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
	assert.Equal(t, "10:00", dr.Summary.OffpeakWindowStart)
	assert.Equal(t, "15:00", dr.Summary.OffpeakWindowEnd)
	assert.Equal(t, "2026-08-16T05:00:00Z", dr.Summary.OffpeakIntegratedAt)
	require.NotNil(t, dr.Summary.OffpeakSampleCount)
	assert.Equal(t, 1500, *dr.Summary.OffpeakSampleCount)
}

func TestDay_PreFeatureRowReportsTheOnlyWindowItCanHaveHad(t *testing.T) {
	// A row written before the geometry snapshot existed carries no window,
	// and 11:00-14:00 is the only one it can have been integrated under — so
	// the wire states it rather than leaving the client to guess.
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, sydneyTZ)
	const date = "2026-03-01"
	opRow := dynamo.OffpeakItem{
		SysSn: testSerial, Date: date, Status: dynamo.OffpeakStatusComplete,
		GridUsageKwh: 3.1, GridExportKwh: 0.9,
	}
	mr := &mockReader{
		getDailyEnergyFn: func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 21}, nil
		},
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &opRow, nil
		},
	}
	h := handlerWithPlans(mr, planRow("p", "2000-01-01", nil, freeBand("11:00", "14:00")))
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)

	var dr DayDetailResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &dr))
	require.NotNil(t, dr.Summary)
	assert.Equal(t, "11:00", dr.Summary.OffpeakWindowStart)
	assert.Equal(t, "14:00", dr.Summary.OffpeakWindowEnd)
	assert.Empty(t, dr.Summary.OffpeakIntegratedAt)
}

func TestDay_ASparseCompleteRowIsReportedAsSuch(t *testing.T) {
	// integratedAt set with no samples is a zero-delta artifact; the client
	// must be able to tell it apart from a measured zero.
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, sydneyTZ)
	const date = "2026-08-15"
	opRow := dynamo.OffpeakItem{
		SysSn: testSerial, Date: date, Status: dynamo.OffpeakStatusComplete,
		WindowStart: "10:00", WindowEnd: "15:00",
		IntegratedAt: "2026-08-16T05:00:00Z", IntegrationSampleCount: 0,
	}
	mr := &mockReader{
		getDailyEnergyFn: func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 21}, nil
		},
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &opRow, nil
		},
	}
	h := handlerWithPlans(mr, touPlan("p"))
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)

	var dr DayDetailResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &dr))
	require.NotNil(t, dr.Summary)
	require.NotNil(t, dr.Summary.OffpeakSampleCount)
	assert.Equal(t, 0, *dr.Summary.OffpeakSampleCount)
	assert.Equal(t, "2026-08-16T05:00:00Z", dr.Summary.OffpeakIntegratedAt)
}

func TestDayAndHistoryAgreeOnTheOffpeakSource(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, sydneyTZ)
	const date = "2026-08-15"
	row := &dynamo.DailyEnergyItem{SysSn: testSerial, Date: date, EInput: 21}
	opRow := dynamo.OffpeakItem{
		SysSn: testSerial, Date: date, Status: dynamo.OffpeakStatusComplete,
		GridUsageKwh: 3.1, GridExportKwh: 0.9,
		WindowStart: "10:00", WindowEnd: "15:00",
		IntegratedAt: "2026-08-16T05:00:00Z", IntegrationSampleCount: 1500,
	}
	mr := &mockReader{
		getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
			return row, nil
		},
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &opRow, nil
		},
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return []dynamo.DailyEnergyItem{*row}, nil
		},
		queryOffpeakFn: func(_ context.Context, _, _, _ string) ([]dynamo.OffpeakItem, error) {
			return []dynamo.OffpeakItem{opRow}, nil
		},
	}
	h := handlerWithPlans(mr, touPlan("p"))
	h.nowFunc = func() time.Time { return now }

	dayResp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
	require.NoError(t, err)
	var dr DayDetailResponse
	require.NoError(t, json.Unmarshal([]byte(dayResp.Body), &dr))

	historyResp, err := h.Handle(context.Background(), historyRequest(map[string]string{
		"start": date, "end": date,
	}))
	require.NoError(t, err)
	var hr HistoryResponse
	require.NoError(t, json.Unmarshal([]byte(historyResp.Body), &hr))

	require.NotNil(t, dr.Summary)
	require.Len(t, hr.Days, 1)
	assert.Equal(t, dr.Summary.OffpeakSource, hr.Days[0].OffpeakSource,
		"a value on two screens must come from one source")
}

func TestDay_NoOffpeakSplitCarriesNoSource(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, sydneyTZ)
	mr := &mockReader{
		getDailyEnergyFn: func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
			return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 21}, nil
		},
	}
	h := handlerWithPlans(mr, touPlan("p"))
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": "2026-08-15"}))
	require.NoError(t, err)
	assert.NotContains(t, resp.Body, "offpeakWindowStart", "absent, not an empty string")
	assert.NotContains(t, resp.Body, "offpeakSampleCount")
}
