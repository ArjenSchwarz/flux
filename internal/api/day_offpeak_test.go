package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleDaySummaryOffpeakSplit covers the new /day off-peak split fields
// added on DaySummary. Three paths are exercised:
//   - opItem with status=complete → split fields populated from the stored
//     deltas (today's date, so the live energy reconciliation also runs).
//   - opItem with status=pending  → split fields populated by projecting the
//     start snapshot against the running energy totals (today only).
//   - GetOffpeak returns a hard error → handler logs and continues; both
//     fields stay nil but the rest of the summary is unaffected.
func TestHandleDaySummaryOffpeakSplit(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	now := fixedNow()
	date := now.In(loc).Format("2006-01-02")

	readings := []dynamo.ReadingItem{
		{Timestamp: time.Date(2026, 4, 15, 8, 1, 0, 0, loc).Unix(), Ppv: 1000, Pload: 500, Pbat: 200, Pgrid: 100, Soc: 80},
		{Timestamp: time.Date(2026, 4, 15, 9, 0, 0, 0, loc).Unix(), Ppv: 3000, Pload: 800, Pbat: -500, Pgrid: 0, Soc: 95},
	}

	tests := map[string]struct {
		offpeakFn  func(ctx context.Context, serial, date string) (*dynamo.OffpeakItem, error)
		wantImport *float64
		wantExport *float64
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
		},
		"pending record projects against running totals": {
			offpeakFn: func(_ context.Context, _, d string) (*dynamo.OffpeakItem, error) {
				assert.Equal(t, date, d)
				return &dynamo.OffpeakItem{
					Date:         date,
					Status:       dynamo.OffpeakStatusPending,
					StartEInput:  1.0,
					StartEOutput: 0.5,
				}, nil
			},
			// Projected delta = max(0, current - start). Stored row sets
			// eInput=4.2 / eOutput=2.1; reconciliation keeps both, so
			// import = 4.2 - 1.0 = 3.2 and export = 2.1 - 0.5 = 1.6.
			wantImport: floatPtr(3.2),
			wantExport: floatPtr(1.6),
		},
		"offpeak query failure leaves fields nil and does not abort": {
			offpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
				return nil, errors.New("dynamodb throttled")
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
			assert.Equal(t, roundEnergy(4.2), *dr.Summary.EInput)

			if tc.wantImport == nil {
				assert.Nil(t, dr.Summary.OffpeakGridImportKwh, "import should be nil when split is missing")
			} else {
				require.NotNil(t, dr.Summary.OffpeakGridImportKwh, "import should be populated for "+name)
				assert.InDelta(t, *tc.wantImport, *dr.Summary.OffpeakGridImportKwh, 0.001)
			}
			if tc.wantExport == nil {
				assert.Nil(t, dr.Summary.OffpeakGridExportKwh, "export should be nil when split is missing")
			} else {
				require.NotNil(t, dr.Summary.OffpeakGridExportKwh, "export should be populated for "+name)
				assert.InDelta(t, *tc.wantExport, *dr.Summary.OffpeakGridExportKwh, 0.001)
			}
		})
	}
}

// TestHandleDaySummaryOffpeakSplitNilEnergy guards against a regression where
// `offpeakSplit(*opItem, energy, isToday)` could panic if `energy` is nil.
//
// `reconcileEnergy(nil, nil)` returns nil, so a today path with neither stored
// energy nor enough readings to live-compute lands in `offpeakSplit` with
// `energy=nil`. Today the safety lives in `offpeakDeltas`: the pending branch
// guards `if current == nil { return …, false }` and the complete branch reads
// from the OffpeakItem only. Pin both behaviours so a future refactor can't
// quietly drop the guard.
func TestHandleDaySummaryOffpeakSplitNilEnergy(t *testing.T) {
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
		"complete OffpeakItem still applies split when energy is nil": {
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
		"pending OffpeakItem yields no split when energy is nil": {
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
