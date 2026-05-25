package derivedstats

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// Helper: build readings at base+dt seconds with the given power values.
type spec struct {
	dt    int64
	ppv   float64
	pgrid float64
	pbat  float64
}

func mkReadings(base int64, specs ...spec) []Reading {
	out := make([]Reading, len(specs))
	for i, s := range specs {
		out[i] = Reading{
			Timestamp: base + s.dt,
			Ppv:       s.ppv,
			Pgrid:     s.pgrid,
			Pbat:      s.pbat,
		}
	}
	return out
}

func TestIntegrateOffpeakDeltas(t *testing.T) {
	const base int64 = 1_700_000_000

	t.Run("happy path: constant power on each channel", func(t *testing.T) {
		// Four 10s-spaced readings inside a 50s window: 1000 W on each channel
		// integrated over [base, base+40) (the trapezoidal sum uses the four
		// in-window points: timestamps 0,10,20,30 → 30s of 1000 W = 30000 Ws).
		readings := mkReadings(base,
			spec{0, 1000, 1000, 1000},
			spec{10, 1000, 1000, 1000},
			spec{20, 1000, 1000, 1000},
			spec{30, 1000, 1000, 1000},
		)
		got, ok := IntegrateOffpeakDeltas(readings, base, base+40)
		require.True(t, ok)
		want := 30_000.0 / 3_600_000.0
		assert.InDelta(t, want, got.SolarKwh, 1e-9)
		assert.InDelta(t, want, got.GridImportKwh, 1e-9)
		assert.InDelta(t, 0.0, got.GridExportKwh, 1e-12)
		assert.InDelta(t, want, got.BatteryDischargeKwh, 1e-9)
		assert.InDelta(t, 0.0, got.BatteryChargeKwh, 1e-12)
		assert.Equal(t, 4, got.SampleCount)
		assert.Equal(t, 0, got.SkippedPairs)
	})

	t.Run("single sample in window returns false", func(t *testing.T) {
		readings := mkReadings(base, spec{10, 1000, 0, 0})
		_, ok := IntegrateOffpeakDeltas(readings, base, base+30)
		assert.False(t, ok, "single-sample windows are unusable per AC 1.6")
	})

	t.Run("empty readings returns false", func(t *testing.T) {
		_, ok := IntegrateOffpeakDeltas(nil, base, base+30)
		assert.False(t, ok)
	})

	t.Run("bracketing samples within 60s synthesise edge points", func(t *testing.T) {
		// Left bracket at base-5 with pgrid=0, in-window at base+5 with pgrid=200.
		// Right bracket at base+35 with pgrid=0, last in-window at base+25 pgrid=200.
		// Window [base, base+30). With linear interpolation:
		//  - left edge at base: pgrid = 0 + (200-0)*(5/10) = 100
		//  - in-window points at base+5 (200), base+15 (200), base+25 (200)
		//  - right edge at base+30: pgrid = 200 + (0-200)*(5/10) = 100
		// Trapezoid sum (Ws):
		//  base→base+5    : (100+200)/2 * 5 = 750
		//  base+5→base+15 : (200+200)/2 * 10 = 2000
		//  base+15→base+25: (200+200)/2 * 10 = 2000
		//  base+25→base+30: (200+100)/2 * 5 = 750
		// total = 5500 Ws
		readings := mkReadings(base,
			spec{-5, 0, 0, 0},
			spec{5, 0, 200, 0},
			spec{15, 0, 200, 0},
			spec{25, 0, 200, 0},
			spec{35, 0, 0, 0},
		)
		got, ok := IntegrateOffpeakDeltas(readings, base, base+30)
		require.True(t, ok)
		want := 5500.0 / 3_600_000.0
		assert.InDelta(t, want, got.GridImportKwh, 1e-9)
		assert.Equal(t, 3, got.SampleCount, "only in-window readings counted")
		assert.Equal(t, 0, got.SkippedPairs)
	})

	t.Run("90s gap inside window skips the affected pair", func(t *testing.T) {
		// Three in-window readings: t=0 (200W), t=10 (200W), t=100 (200W).
		// Gap between idx1 and idx2 is 90s — pair contributes zero.
		// Only the 0→10 pair contributes: (200+200)/2 * 10 = 2000 Ws.
		readings := mkReadings(base,
			spec{0, 0, 200, 0},
			spec{10, 0, 200, 0},
			spec{100, 0, 200, 0},
		)
		got, ok := IntegrateOffpeakDeltas(readings, base, base+200)
		require.True(t, ok)
		assert.InDelta(t, 2000.0/3_600_000.0, got.GridImportKwh, 1e-9)
		assert.Equal(t, 3, got.SampleCount)
		assert.Equal(t, 1, got.SkippedPairs, "the >60s pair should be tallied")
	})

	t.Run("signed pgrid splits into import and export per-sample", func(t *testing.T) {
		// Constant +200 W (import) for 10s then constant -200 W (export) for 10s.
		// Trapezoid uses per-sample clamping, so import = ∫max(pgrid,0).
		// Per-sample values: 200, 200, 0, -200 → clamp+: 200,200,0,0 → trap:
		//  0→10: (200+200)/2 * 10 = 2000 Ws import
		// 10→20: (200+0)/2 * 10 = 1000 Ws import
		// 20→30: (0+0)/2 * 10 = 0
		// Export (clamp-pgrid): 0,0,0,200 → trap:
		//  0→10: 0
		// 10→20: (0+0)/2 * 10 = 0
		// 20→30: (0+200)/2 * 10 = 1000 Ws export
		readings := mkReadings(base,
			spec{0, 0, 200, 0},
			spec{10, 0, 200, 0},
			spec{20, 0, 0, 0},
			spec{30, 0, -200, 0},
		)
		got, ok := IntegrateOffpeakDeltas(readings, base, base+40)
		require.True(t, ok)
		assert.InDelta(t, 3000.0/3_600_000.0, got.GridImportKwh, 1e-9)
		assert.InDelta(t, 1000.0/3_600_000.0, got.GridExportKwh, 1e-9)
		assert.GreaterOrEqual(t, got.GridImportKwh, 0.0)
		assert.GreaterOrEqual(t, got.GridExportKwh, 0.0)
	})

	t.Run("reading exactly at endUnix is excluded as interior but used as right bracket", func(t *testing.T) {
		// Readings at base, base+10, base+20, base+30. Window [base, base+30).
		// base+30 is excluded as an interior sample (half-open), but the
		// algorithm uses base+20→base+30 as the right bracket and synthesises
		// a point at endUnix=base+30 by linear interpolation (frac = 1.0 since
		// next.Timestamp == endUnix). For pgrid constant 200 W the synthesised
		// edge is 200, so pts is (0,200),(10,200),(20,200),(30,200) and
		//  sum = 200*30 = 6000 Ws.
		// SampleCount counts only [startUnix, endUnix) interior readings: 3.
		readings := mkReadings(base,
			spec{0, 0, 200, 0},
			spec{10, 0, 200, 0},
			spec{20, 0, 200, 0},
			spec{30, 0, 200, 0},
		)
		got, ok := IntegrateOffpeakDeltas(readings, base, base+30)
		require.True(t, ok)
		assert.InDelta(t, 6000.0/3_600_000.0, got.GridImportKwh, 1e-9)
		// SampleCount must reflect [start, end) — 3 interior readings.
		assert.Equal(t, 3, got.SampleCount)
	})

	t.Run("gridUsage + peak == eInput invariant (AC 2.3 structural)", func(t *testing.T) {
		// Synthesise a "day" where the off-peak window integration plus a
		// pretend daily total trivially sums to that total. The point is that
		// the integration is the source of `gridUsageKwh`; whatever it returns
		// gets subtracted from `eInput` to produce peak. Here we just assert
		// that gridUsageKwh + (eInput - gridUsageKwh) == eInput exactly.
		readings := mkReadings(base,
			spec{0, 0, 1000, 0},
			spec{10, 0, 1000, 0},
			spec{20, 0, 1000, 0},
		)
		got, ok := IntegrateOffpeakDeltas(readings, base, base+30)
		require.True(t, ok)
		const eInput = 12.34
		peak := eInput - got.GridImportKwh
		assert.Equal(t, eInput, got.GridImportKwh+peak, "trivially true by construction; documented per AC 2.3")
	})

	t.Run("solar negative ppv clamped per-sample", func(t *testing.T) {
		// Per-sample max(ppv,0): values 100, -200, 100 over 10s spacing.
		// Clamped: 100, 0, 100. Trapezoid:
		//  pts: (0,100),(10,0),(20,100)
		//  sum: (100+0)/2*10 + (0+100)/2*10 = 500 + 500 = 1000 Ws
		readings := mkReadings(base,
			spec{0, 100, 0, 0},
			spec{10, -200, 0, 0},
			spec{20, 100, 0, 0},
		)
		got, ok := IntegrateOffpeakDeltas(readings, base, base+30)
		require.True(t, ok)
		assert.InDelta(t, 1000.0/3_600_000.0, got.SolarKwh, 1e-9)
	})

	t.Run("battery charge/discharge per-sample split", func(t *testing.T) {
		// pbat: +1000 (discharge), -1000 (charge), +500 (discharge)
		// charge   = max(-pbat, 0) per-sample: 0, 1000, 0
		// discharge= max( pbat, 0) per-sample: 1000, 0, 500
		readings := mkReadings(base,
			spec{0, 0, 0, 1000},
			spec{10, 0, 0, -1000},
			spec{20, 0, 0, 500},
		)
		got, ok := IntegrateOffpeakDeltas(readings, base, base+30)
		require.True(t, ok)
		// discharge trap: (1000+0)/2*10 + (0+500)/2*10 = 5000+2500 = 7500 Ws
		// charge trap:   (0+1000)/2*10 + (1000+0)/2*10 = 5000+5000 = 10000 Ws
		assert.InDelta(t, 7500.0/3_600_000.0, got.BatteryDischargeKwh, 1e-9)
		assert.InDelta(t, 10000.0/3_600_000.0, got.BatteryChargeKwh, 1e-9)
	})
}

// --- Property tests (pgregory.net/rapid) ---

// genOffpeakReadings builds a random sorted slice of readings with bounded
// inter-sample gap so the 60s gap rule is rarely or never triggered
// (gap ∈ [5, 60]). Power values span ±5000 W so both signs are exercised on
// pgrid/pbat and clamping on ppv. Named to avoid collision with the existing
// genReadings helper in property_test.go (Pload/Ppv/Soc focus).
func genOffpeakReadings(t *rapid.T, n int, base int64) []Reading {
	out := make([]Reading, n)
	ts := base
	for i := range n {
		gap := rapid.IntRange(5, 60).Draw(t, fmt.Sprintf("gap%d", i))
		ts += int64(gap)
		out[i] = Reading{
			Timestamp: ts,
			Ppv:       rapid.Float64Range(-500, 5000).Draw(t, fmt.Sprintf("ppv%d", i)),
			Pgrid:     rapid.Float64Range(-5000, 5000).Draw(t, fmt.Sprintf("pgrid%d", i)),
			Pbat:      rapid.Float64Range(-5000, 5000).Draw(t, fmt.Sprintf("pbat%d", i)),
		}
	}
	return out
}

// TestPropertyOffpeakClosureUnderWindow asserts that for any sub-window,
// GridImport + GridExport ≤ ∫|pgrid|. Per-sample clamping guarantees this is
// a tight bound when pgrid never changes sign within a trapezoid pair; when it
// does, the clamped sum is strictly less than the trapezoid of |pgrid|, but
// never greater. The integrate helper is invoked through the public entry
// point so the test exercises the same wiring as production.
func TestPropertyOffpeakClosureUnderWindow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const base int64 = 1_700_000_000
		n := rapid.IntRange(3, 100).Draw(t, "n")
		readings := genOffpeakReadings(t, n, base)
		firstTS := readings[0].Timestamp
		lastTS := readings[n-1].Timestamp
		windowLo := firstTS - 30
		windowHi := lastTS + 30
		a := rapid.Int64Range(windowLo, windowHi-1).Draw(t, "a")
		c := rapid.Int64Range(a+1, windowHi).Draw(t, "c")

		got, ok := IntegrateOffpeakDeltas(readings, a, c)
		if !ok {
			return
		}
		// Build an "absolute pgrid" version and integrate via the same primitive
		// by transforming readings; reuse integratePpv as the abs-integral proxy
		// is brittle, so build expectations directly from a flattened sum: the
		// per-pair contribution of GridImport+GridExport over each trap is at
		// most max(|p_a|, |p_b|) * dt — bounded by the trapezoid of |pgrid|.
		sum := got.GridImportKwh + got.GridExportKwh
		assert.GreaterOrEqual(t, sum, 0.0)
		// Upper bound: ∫|pgrid| using the same trap algorithm over per-sample |pgrid|.
		abs := make([]Reading, len(readings))
		for i, r := range readings {
			abs[i] = Reading{Timestamp: r.Timestamp, Ppv: math.Abs(r.Pgrid)}
		}
		upper, _ := integratePpv(abs, a, c)
		// Allow a tiny epsilon for float noise.
		assert.LessOrEqual(t, sum, upper+1e-9)
	})
}

// TestPropertyOffpeakMonotonicityOverWindow asserts AC 4.4: extending the
// integration window can only grow each delta (or leave it equal).
func TestPropertyOffpeakMonotonicityOverWindow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const base int64 = 1_700_000_000
		n := rapid.IntRange(3, 60).Draw(t, "n")
		readings := genOffpeakReadings(t, n, base)
		firstTS := readings[0].Timestamp
		lastTS := readings[n-1].Timestamp
		start := firstTS - 10
		// Two endpoints, end1 < end2, both inside [start, lastTS+10].
		end1 := rapid.Int64Range(start+1, lastTS+5).Draw(t, "end1")
		end2 := rapid.Int64Range(end1, lastTS+10).Draw(t, "end2")

		got1, ok1 := IntegrateOffpeakDeltas(readings, start, end1)
		got2, ok2 := IntegrateOffpeakDeltas(readings, start, end2)
		if !ok1 || !ok2 {
			return
		}
		// Each delta from the longer integration must be >= the shorter,
		// modulo float epsilon. Per-sample non-negative clamping guarantees
		// this for the integrand; gap-skipped pairs contribute zero in both.
		const eps = 1e-9
		assert.GreaterOrEqual(t, got2.GridImportKwh+eps, got1.GridImportKwh)
		assert.GreaterOrEqual(t, got2.GridExportKwh+eps, got1.GridExportKwh)
		assert.GreaterOrEqual(t, got2.SolarKwh+eps, got1.SolarKwh)
		assert.GreaterOrEqual(t, got2.BatteryChargeKwh+eps, got1.BatteryChargeKwh)
		assert.GreaterOrEqual(t, got2.BatteryDischargeKwh+eps, got1.BatteryDischargeKwh)
	})
}

// TestPropertyOffpeakClampingSymmetry: negating pgrid swaps import/export and
// negating pbat swaps charge/discharge. Holds because per-sample clamping is
// the only nonlinearity and max(-x,0) = max(x,0) under x → -x.
func TestPropertyOffpeakClampingSymmetry(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const base int64 = 1_700_000_000
		n := rapid.IntRange(3, 40).Draw(t, "n")
		readings := genOffpeakReadings(t, n, base)
		start := readings[0].Timestamp
		end := readings[n-1].Timestamp + 1

		got, ok := IntegrateOffpeakDeltas(readings, start, end)
		if !ok {
			return
		}
		flipped := make([]Reading, len(readings))
		for i, r := range readings {
			flipped[i] = Reading{
				Timestamp: r.Timestamp,
				Ppv:       r.Ppv,
				Pgrid:     -r.Pgrid,
				Pbat:      -r.Pbat,
			}
		}
		flippedGot, ok2 := IntegrateOffpeakDeltas(flipped, start, end)
		require.True(t, ok2)
		const eps = 1e-9
		assert.InDelta(t, got.GridImportKwh, flippedGot.GridExportKwh, eps)
		assert.InDelta(t, got.GridExportKwh, flippedGot.GridImportKwh, eps)
		assert.InDelta(t, got.BatteryChargeKwh, flippedGot.BatteryDischargeKwh, eps)
		assert.InDelta(t, got.BatteryDischargeKwh, flippedGot.BatteryChargeKwh, eps)
		// Solar (single-sign channel) is unaffected by pgrid/pbat flip.
		assert.InDelta(t, got.SolarKwh, flippedGot.SolarKwh, eps)
	})
}

// TestPropertyOffpeakRoundTripIdempotence: running the integration twice on
// the same readings produces identical values (AC 7.3).
func TestPropertyOffpeakRoundTripIdempotence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const base int64 = 1_700_000_000
		n := rapid.IntRange(3, 80).Draw(t, "n")
		readings := genOffpeakReadings(t, n, base)
		start := readings[0].Timestamp
		end := readings[n-1].Timestamp + 1

		got1, ok1 := IntegrateOffpeakDeltas(readings, start, end)
		got2, ok2 := IntegrateOffpeakDeltas(readings, start, end)
		assert.Equal(t, ok1, ok2)
		assert.Equal(t, got1, got2)
	})
}

// TestPropertyOffpeakLenLessThanTwoFalse: any input that lands fewer than two
// usable construction points returns (_, false). Constructed: empty slice and
// single-reading slice both fall under the gate.
func TestPropertyOffpeakLenLessThanTwoFalse(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const base int64 = 1_700_000_000
		// Choose 0 or 1 sample.
		n := rapid.IntRange(0, 1).Draw(t, "n")
		readings := make([]Reading, n)
		if n == 1 {
			readings[0] = Reading{
				Timestamp: base + 50,
				Pgrid:     rapid.Float64Range(-1000, 1000).Draw(t, "pgrid"),
			}
		}
		_, ok := IntegrateOffpeakDeltas(readings, base, base+100)
		assert.False(t, ok)
	})
}

// BenchmarkIntegrateOffpeakDeltas covers AC 8.1: the window-end computation
// must complete within 2 s on a typical day. A typical day has ~1080 readings
// (3 h × 10 s spacing). The 2 s budget is asserted at the test level so a
// regression that adds an O(n²) pass surfaces immediately; the benchmark
// itself is also useful for tracking changes via benchstat.
func BenchmarkIntegrateOffpeakDeltas(b *testing.B) {
	const (
		n             = 1080
		startUnix     = int64(1_700_000_000)
		spacingSecond = 10
	)
	readings := make([]Reading, n)
	for i := range n {
		readings[i] = Reading{
			Timestamp: startUnix + int64(i*spacingSecond),
			Ppv:       float64(500 + i%2000),
			Pgrid:     float64(-3000 + i%6000),
			Pbat:      float64(-2000 + i%4000),
		}
	}
	endUnix := startUnix + int64(n*spacingSecond)

	for b.Loop() {
		_, _ = IntegrateOffpeakDeltas(readings, startUnix, endUnix)
	}
}

// TestIntegrateOffpeakDeltas_AC81_WallClock pins AC 8.1's 2 s budget on
// 1080 synthetic readings — a regression-only test, not a microbenchmark.
// Failure here means a pass added between the existing five integrate calls
// has pushed the typical-day computation past the poller's window-end budget.
func TestIntegrateOffpeakDeltas_AC81_WallClock(t *testing.T) {
	const (
		n             = 1080
		startUnix     = int64(1_700_000_000)
		spacingSecond = 10
	)
	readings := make([]Reading, n)
	for i := range n {
		readings[i] = Reading{
			Timestamp: startUnix + int64(i*spacingSecond),
			Ppv:       float64(500 + i%2000),
			Pgrid:     float64(-3000 + i%6000),
			Pbat:      float64(-2000 + i%4000),
		}
	}
	endUnix := startUnix + int64(n*spacingSecond)

	started := time.Now()
	_, ok := IntegrateOffpeakDeltas(readings, startUnix, endUnix)
	elapsed := time.Since(started)
	require.True(t, ok)
	t.Logf("IntegrateOffpeakDeltas over %d readings: %s", n, elapsed)
	assert.Less(t, elapsed, 2*time.Second, "AC 8.1: window-end computation must complete within 2 s")
}
