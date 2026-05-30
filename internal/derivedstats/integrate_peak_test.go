package derivedstats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegratePeakGridImportKwh(t *testing.T) {
	const base int64 = 1_700_000_000

	t.Run("sums both sub-windows", func(t *testing.T) {
		// Morning window [base, base+40): 1000 W constant grid import over the
		// four in-window points (0,10,20,30) → 30s of 1000 W = 30000 Ws.
		// Evening window [base+1000, base+1040): same shape, another 30000 Ws.
		// The off-peak window between them carries no grid import. The windows
		// are spaced far apart (>60s) so no edge synthesis bleeds across them.
		readings := mkReadings(base,
			spec{0, 0, 1000, 0},
			spec{10, 0, 1000, 0},
			spec{20, 0, 1000, 0},
			spec{30, 0, 1000, 0},
			// evening peak window, well clear of the off-peak gap
			spec{1000, 0, 1000, 0},
			spec{1010, 0, 1000, 0},
			spec{1020, 0, 1000, 0},
			spec{1030, 0, 1000, 0},
		)
		kwh, samples, skipped, ok := IntegratePeakGridImportKwh(readings, base, base+40, base+960, base+1040)
		require.True(t, ok)
		want := (30_000.0 + 30_000.0) / 3_600_000.0
		assert.InDelta(t, want, kwh, 1e-9)
		assert.Equal(t, 8, samples, "four samples per peak sub-window")
		assert.Equal(t, 0, skipped)
	})

	t.Run("only positive grid clamped per sample", func(t *testing.T) {
		// Negative pgrid (export) contributes zero to grid import.
		readings := mkReadings(base,
			spec{0, 0, -2000, 0},
			spec{10, 0, -2000, 0},
			spec{20, 0, 1000, 0},
			spec{30, 0, 1000, 0},
			spec{100, 0, -500, 0},
			spec{110, 0, -500, 0},
			spec{120, 0, 0, 0},
			spec{130, 0, 0, 0},
		)
		kwh, _, _, ok := IntegratePeakGridImportKwh(readings, base, base+40, base+100, base+140)
		require.True(t, ok)
		assert.GreaterOrEqual(t, kwh, 0.0, "import is non-negative by construction")
	})

	t.Run("gate failure in morning sub-window yields not-usable", func(t *testing.T) {
		// Morning window has a single sample (sparse — overnight outage);
		// evening window is fine. The combined result must be not-usable.
		readings := mkReadings(base,
			spec{10, 0, 1000, 0}, // single sample in [base, base+40)
			spec{100, 0, 1000, 0},
			spec{110, 0, 1000, 0},
			spec{120, 0, 1000, 0},
			spec{130, 0, 1000, 0},
		)
		_, _, _, ok := IntegratePeakGridImportKwh(readings, base, base+40, base+100, base+140)
		assert.False(t, ok, "sparse morning sub-window must produce ok=false")
	})

	t.Run("gate failure in evening sub-window yields not-usable", func(t *testing.T) {
		readings := mkReadings(base,
			spec{0, 0, 1000, 0},
			spec{10, 0, 1000, 0},
			spec{20, 0, 1000, 0},
			spec{30, 0, 1000, 0},
			spec{110, 0, 1000, 0}, // single sample in [base+100, base+140)
		)
		_, _, _, ok := IntegratePeakGridImportKwh(readings, base, base+40, base+100, base+140)
		assert.False(t, ok, "sparse evening sub-window must produce ok=false")
	})

	t.Run("empty readings yields not-usable", func(t *testing.T) {
		_, _, _, ok := IntegratePeakGridImportKwh(nil, base, base+40, base+100, base+140)
		assert.False(t, ok)
	})

	t.Run("DST 25h day handled via unix window args", func(t *testing.T) {
		// Sydney 2026-04-05 is a 25-hour day (DST ends, clocks back at 03:00).
		// Off-peak 11:00-14:00. Build hourly readings with 1000 W import in
		// peak hours and 0 in off-peak, then confirm boundaries derived from
		// the local calendar integrate without error and exclude off-peak.
		loc, err := time.LoadLocation("Australia/Sydney")
		require.NoError(t, err)
		dayStart := time.Date(2026, 4, 5, 0, 0, 0, 0, loc)
		dayEnd := dayStart.AddDate(0, 0, 1)
		assert.Equal(t, 25*time.Hour, dayEnd.Sub(dayStart), "2026-04-05 must be a 25h Sydney day")

		offStart := time.Date(2026, 4, 5, 11, 0, 0, 0, loc)
		offEnd := time.Date(2026, 4, 5, 14, 0, 0, 0, loc)

		var readings []Reading
		for t := dayStart; t.Before(dayEnd); t = t.Add(time.Minute) {
			pgrid := 1000.0
			if !t.Before(offStart) && t.Before(offEnd) {
				pgrid = 0
			}
			readings = append(readings, Reading{Timestamp: t.Unix(), Pgrid: pgrid})
		}

		kwh, _, _, ok := IntegratePeakGridImportKwh(readings, dayStart.Unix(), offStart.Unix(), offEnd.Unix(), dayEnd.Unix())
		require.True(t, ok)
		// 25h day minus 3h off-peak = 22h of 1000 W ≈ 22 kWh.
		assert.InDelta(t, 22.0, kwh, 0.05)
	})

	t.Run("peak + offpeak within 3% of eInput on a representative day", func(t *testing.T) {
		// Full day with grid import all day: integrating the whole day (peak
		// sub-windows + off-peak window) must reconstruct the day's total grid
		// import — the eInput analogue — to within 3% (Requirement: same
		// numerical method, same per-window artifact).
		loc, err := time.LoadLocation("Australia/Sydney")
		require.NoError(t, err)
		dayStart := time.Date(2026, 5, 20, 0, 0, 0, 0, loc)
		dayEnd := dayStart.AddDate(0, 0, 1)
		offStart := time.Date(2026, 5, 20, 11, 0, 0, 0, loc)
		offEnd := time.Date(2026, 5, 20, 14, 0, 0, 0, loc)

		// 10s readings, varying but always-positive grid import.
		var readings []Reading
		var trueWattSeconds float64
		prev := -1.0
		for ts := dayStart.Unix(); ts < dayEnd.Unix(); ts += 10 {
			p := 400.0 + 300.0*float64((ts/10)%5) // 400..1600 W
			readings = append(readings, Reading{Timestamp: ts, Pgrid: p})
			if prev >= 0 {
				trueWattSeconds += (prev + p) / 2 * 10
			}
			prev = p
		}
		dayKwh := trueWattSeconds / 3_600_000

		peak, _, _, ok := IntegratePeakGridImportKwh(readings, dayStart.Unix(), offStart.Unix(), offEnd.Unix(), dayEnd.Unix())
		require.True(t, ok)
		off, offOK := IntegrateOffpeakDeltas(readings, offStart.Unix(), offEnd.Unix())
		require.True(t, offOK)

		total := peak + off.GridImportKwh
		assert.InDelta(t, dayKwh, total, dayKwh*0.03, "peak+offpeak must be within 3%% of the full-day integral")
	})
}
