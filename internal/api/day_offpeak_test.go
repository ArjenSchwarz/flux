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

// TestHandleDaySummaryOffpeakSplit covers the /day off-peak split fields on
// DaySummary. Three paths are exercised:
//   - opItem with status=complete → split fields populated from the stored
//     deltas (date is today; readings drive the live energy reconciliation
//     but the off-peak split bypasses them).
//   - opItem with status=pending  → split fields populated by live-integrating
//     readings over [offpeak-start, min(now, offpeak-end)).
//   - GetOffpeak returns a hard error → handler logs and continues; both
//     fields stay nil but the rest of the summary is unaffected.
func TestHandleDaySummaryOffpeakSplit(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	// 11:30 AEST so a 30-minute live integration is well-defined for the
	// pending-row case (constant 3600 W → 1.8 kWh).
	now := time.Date(2026, 4, 15, 11, 30, 0, 0, loc)
	date := now.In(loc).Format("2006-01-02")
	opStart := time.Date(2026, 4, 15, 11, 0, 0, 0, loc)

	var readings []dynamo.ReadingItem
	for ts := opStart.Unix(); ts <= now.Unix(); ts += 10 {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: ts,
			Pgrid:     3600,
		})
	}

	tests := map[string]struct {
		offpeakFn  func(ctx context.Context, serial, date string) (*dynamo.OffpeakItem, error)
		wantImport *float64
		wantExport *float64
		delta      float64
	}{
		"complete record applies stored deltas": {
			offpeakFn: func(_ context.Context, _, d string) (*dynamo.OffpeakItem, error) {
				assert.Equal(t, date, d)
				return &dynamo.OffpeakItem{
					Date:          date,
					Status:        dynamo.OffpeakStatusComplete,
					GridUsageKwh:  3.42,
					GridExportKwh: 0.71,
				}, nil
			},
			wantImport: floatPtr(3.42),
			wantExport: floatPtr(0.71),
			delta:      0.001,
		},
		"pending record live-integrates from readings": {
			offpeakFn: func(_ context.Context, _, d string) (*dynamo.OffpeakItem, error) {
				assert.Equal(t, date, d)
				return &dynamo.OffpeakItem{
					Date:   date,
					Status: dynamo.OffpeakStatusPending,
					// StartE* must NOT be read by the live path (AC 5.3).
					StartEInput:  999.0,
					StartEOutput: 999.0,
				}, nil
			},
			// 30 minutes × 3600 W = 1.8 kWh; export remains 0.
			wantImport: floatPtr(1.8),
			wantExport: floatPtr(0.0),
			delta:      0.01,
		},
		"offpeak query failure leaves fields nil and does not abort": {
			offpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
				return nil, errors.New("dynamodb throttled")
			},
			wantImport: nil,
			wantExport: nil,
			delta:      0.001,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mr := &mockReader{
				queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
					return readings, nil
				},
				getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
					return &dynamo.DailyEnergyItem{
						Date: date, Epv: 15.5, EInput: 4.2, EOutput: 2.1, ECharge: 8.0, EDischarge: 6.5,
					}, nil
				},
				getOffpeakFn: tc.offpeakFn,
			}

			h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
			require.NoError(t, err, "handler must not error on offpeak query failure")
			assert.Equal(t, 200, resp.StatusCode, "/day must succeed even when offpeak query fails")

			dr := parseDayResponse(t, resp)
			require.NotNil(t, dr.Summary, "summary should always be present when daily energy exists")

			// Energy totals stay populated regardless of off-peak split.
			require.NotNil(t, dr.Summary.EInput, "eInput should be populated independently of offpeak")
			assert.Equal(t, derivedstats.RoundEnergy(4.2), *dr.Summary.EInput)

			if tc.wantImport == nil {
				assert.Nil(t, dr.Summary.OffpeakGridImportKwh, "import should be nil when split is missing")
			} else {
				require.NotNil(t, dr.Summary.OffpeakGridImportKwh, "import should be populated for "+name)
				assert.InDelta(t, *tc.wantImport, *dr.Summary.OffpeakGridImportKwh, tc.delta)
			}
			if tc.wantExport == nil {
				assert.Nil(t, dr.Summary.OffpeakGridExportKwh, "export should be nil when split is missing")
			} else {
				require.NotNil(t, dr.Summary.OffpeakGridExportKwh, "export should be populated for "+name)
				assert.InDelta(t, *tc.wantExport, *dr.Summary.OffpeakGridExportKwh, tc.delta)
			}
		})
	}
}

// TestHandleDaySummaryOffpeakSplitDispatch pins the off-peak split when the
// live-compute path has no usable inputs:
//   - complete row: passes through stored deltas regardless of readings/energy.
//   - pending row before the off-peak window opens (AC 4.3): no split, no panic.
//
// fixedNow is 10:00 AEST — before the 11:00 window starts — and the single
// reading is insufficient for liveOffpeakDeltas in any case.
func TestHandleDaySummaryOffpeakSplitDispatch(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	now := fixedNow()
	date := now.In(loc).Format("2006-01-02")

	// Single reading produces a SocLow (so Summary is built) but is
	// insufficient for `computeTodayEnergy` (needs >= 2). With deItem also
	// nil, `energy = reconcileEnergy(nil, nil) = nil`.
	readings := []dynamo.ReadingItem{
		{Timestamp: time.Date(2026, 4, 15, 9, 0, 0, 0, loc).Unix(), Ppv: 0, Pload: 0, Pbat: 0, Pgrid: 0, Soc: 42},
	}

	tests := map[string]struct {
		offpeakFn  func(ctx context.Context, serial, date string) (*dynamo.OffpeakItem, error)
		wantImport *float64
		wantExport *float64
	}{
		"complete OffpeakItem applies split regardless of inputs": {
			offpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
				return &dynamo.OffpeakItem{
					Date:          date,
					Status:        dynamo.OffpeakStatusComplete,
					GridUsageKwh:  2.0,
					GridExportKwh: 0.4,
				}, nil
			},
			wantImport: floatPtr(2.0),
			wantExport: floatPtr(0.4),
		},
		"pending OffpeakItem before window returns no split (AC 4.3)": {
			offpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
				return &dynamo.OffpeakItem{
					Date:        date,
					Status:      dynamo.OffpeakStatusPending,
					StartEInput: 1.0,
				}, nil
			},
			wantImport: nil,
			wantExport: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mr := &mockReader{
				queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
					return readings, nil
				},
				getDailyEnergyFn: func(_ context.Context, _, _ string) (*dynamo.DailyEnergyItem, error) {
					return nil, nil
				},
				getOffpeakFn: tc.offpeakFn,
			}

			h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), dayRequest(map[string]string{"date": date}))
			require.NoError(t, err)
			assert.Equal(t, 200, resp.StatusCode)

			dr := parseDayResponse(t, resp)
			require.NotNil(t, dr.Summary, "summary built because socLow is present")
			assert.Nil(t, dr.Summary.EInput, "no daily energy → eInput nil when energy=nil")

			if tc.wantImport == nil {
				assert.Nil(t, dr.Summary.OffpeakGridImportKwh)
			} else {
				require.NotNil(t, dr.Summary.OffpeakGridImportKwh)
				assert.InDelta(t, *tc.wantImport, *dr.Summary.OffpeakGridImportKwh, 0.001)
			}
			if tc.wantExport == nil {
				assert.Nil(t, dr.Summary.OffpeakGridExportKwh)
			} else {
				require.NotNil(t, dr.Summary.OffpeakGridExportKwh)
				assert.InDelta(t, *tc.wantExport, *dr.Summary.OffpeakGridExportKwh, 0.001)
			}
		})
	}
}
