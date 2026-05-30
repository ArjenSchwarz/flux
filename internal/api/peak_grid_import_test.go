package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
