package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// These tests cover the switch from --offpeak-start/end flags to per-day plan
// resolution (Q24). The block layout is partitioned around the day's free
// window, so recomputing a day under the wrong window produces a different set
// of blocks than the stored row has and the per-kind patch lands nowhere.

// A range spanning a plan switch must recompute each day under its own
// window — the whole reason the flags were removed.
func TestBackfill_ResolvesWindowPerDayAcrossAPlanSwitch(t *testing.T) {
	loc := sydney(t)
	const before, after = "2026-07-31", "2026-08-01"
	f := &fakeDynamo{
		location: loc,
		dailyEnergyRows: map[string][]dynamo.DailyEnergyItem{"*": {
			storedRowAllDaylightMissingSolar(before),
			storedRowAllDaylightMissingSolar(after),
		}},
		readingsByDate: map[string][]dynamo.ReadingItem{
			before: minuteReadingsWithSolar(t, before, loc),
			after:  minuteReadingsWithSolar(t, after, loc),
		},
		pricingRows: []dynamo.PricingItem{
			testPricingRow("old", "2000-01-01", after, "11:00", "14:00"),
			testPricingRow("new", after, "", "10:00", "15:00"),
		},
	}
	opts := backfillOptsForTest(loc)
	opts.from, opts.to = before, after
	// Both fixture dates must be in the past for the in-progress clamp in
	// derivedstats.Blocks to stay inert.
	opts.now = func() time.Time { return time.Date(2026, 8, 5, 12, 0, 0, 0, loc) }

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)
	require.Equal(t, 2, res.RowsWritten)
	require.Len(t, f.updates, 2)

	// The patched row keeps the stored block boundaries verbatim (Decision 7),
	// so the window each day was recomputed under shows up in the energy: the
	// off-peak block's solar is the integral over that day's free window, at a
	// constant 1.5 kW of generation.
	offpeakSolar := func(up int) float64 {
		patched := decodeDailyUsageFromUpdate(t, f.updates[up])
		require.NotNil(t, patched)
		for _, b := range patched.Blocks {
			if b.Kind == derivedstats.DailyUsageKindOffPeak {
				require.NotNil(t, b.SolarKwh)
				return *b.SolarKwh
			}
		}
		t.Fatalf("update %d has no off-peak block", up)
		return 0
	}

	assert.InDelta(t, 4.5, offpeakSolar(0), 0.05,
		"switch eve is still priced by the predecessor's 3-hour window")
	assert.InDelta(t, 7.5, offpeakSolar(1), 0.05,
		"the switch day belongs to the successor's 5-hour window")
}

// Without a plan the day's block boundaries are unknowable, and recomputing
// under a guessed window would patch the wrong blocks.
func TestBackfill_NoPlanForDate_SkipsRow(t *testing.T) {
	loc := sydney(t)
	f := &fakeDynamo{
		location:        loc,
		dailyEnergyRows: map[string][]dynamo.DailyEnergyItem{"*": {storedRowAllDaylightMissingSolar("2026-04-14")}},
		readingsByDate:  map[string][]dynamo.ReadingItem{"2026-04-14": minuteReadingsWithSolar(t, "2026-04-14", loc)},
		// The only plan starts well after the date being repaired.
		pricingRows: []dynamo.PricingItem{testPricingRow("later", "2026-08-01", "", "10:00", "15:00")},
	}
	opts := backfillOptsForTest(loc)

	res, err := runBackfill(context.Background(), f, opts)
	require.NoError(t, err)

	assert.Equal(t, 1, res.RowsNoPlan)
	assert.Zero(t, res.RowsWritten)
	assert.Empty(t, f.updates)
}

// An unreadable pricing table is fatal rather than a per-row skip: continuing
// would recompute every day in the range under no window at all.
func TestBackfill_PricingReadError_PropagatedAsFatal(t *testing.T) {
	loc := sydney(t)
	f := &fakeDynamo{location: loc, scanErr: errors.New("pricing table unreachable")}

	_, err := runBackfill(context.Background(), f, backfillOptsForTest(loc))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list pricing")
}

// A plan with no free band carves out no off-peak block, which is the layout
// such a day's row was stored under — so the recompute must use empty bounds
// rather than substituting a window that isn't in the plan (AC 4.4).
func TestFreeWindowHHMM_NoFreeBandYieldsEmptyBounds(t *testing.T) {
	t.Parallel()
	flat := dynamo.PricingItem{
		PricingID: "flat", StartDate: "2026-08-01", DefaultRate: 0.35, FeedInRate: 0.05,
	}

	start, end := freeWindowHHMM(flat.Plan())

	assert.Empty(t, start)
	assert.Empty(t, end)
}

func TestFreeWindowHHMM_RendersThePlansFreeBand(t *testing.T) {
	t.Parallel()

	start, end := freeWindowHHMM(testPricingRow("p", "2026-08-01", "", "10:00", "15:00").Plan())

	assert.Equal(t, "10:00", start)
	assert.Equal(t, "15:00", end)
}

func TestValidateOpts_RejectsMissingPricingTable(t *testing.T) {
	loc := sydney(t)
	opts := backfillOptsForTest(loc)
	opts.tablePricing = ""

	err := validateOpts(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "table-pricing")
}
