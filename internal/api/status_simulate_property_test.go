package api

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// maxDischargeWatts restates the inverter ceiling in watts for the property
// generators below; the production code derives the same value from
// maxDischargeKW.
const maxDischargeWatts = maxDischargeKW * 1000

// drawPower generates a battery/grid power value spanning charging through
// discharge and deliberately past the inverter ceiling, so the headroom form
// is exercised above maxDischargeW (where the naive min would clamp).
func drawPower(t *rapid.T, label string) float64 {
	return rapid.Float64Range(-8000, 12000).Draw(t, label)
}

// drawWatts generates an added-load value across the accepted range, including
// 0 (the zero-load-equivalence boundary) up to the 20 kW cap.
func drawWatts(t *rapid.T) float64 {
	return rapid.Float64Range(0, 20000).Draw(t, "watts")
}

// TestPropertySimDischargeNeverBelowReal asserts [3.4]: for any series power p
// and any added load w >= 0, simDischarge(p, wBattery) is never below the real
// p (adding load never lowers the shown discharge), and never exceeds the
// inverter ceiling unless the real p already did.
func TestPropertySimDischargeNeverBelowReal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := drawPower(t, "pbat")
		w := drawWatts(t)
		got := simDischarge(p, w)
		if got < p {
			t.Fatalf("simDischarge(%v, %v) = %v < real pbat %v", p, w, got, p)
		}
		// The added portion never pushes discharge above the ceiling; if p
		// already exceeds the ceiling, got must equal p (headroom 0).
		ceiling := float64(maxDischargeWatts)
		if p >= ceiling && got != p {
			t.Fatalf("p already >= ceiling: simDischarge(%v, %v) = %v, want %v", p, w, got, p)
		}
		if p < ceiling && got > ceiling+1e-9 {
			t.Fatalf("simDischarge(%v, %v) = %v exceeds ceiling %v", p, w, got, ceiling)
		}
	})
}

// TestPropertySimDischargeZeroLoadNoOp asserts the headroom form is a true
// no-op at w = 0 for every p — including p at or above the ceiling, where the
// naive min(p+w, ceiling) form would wrongly clamp p down.
func TestPropertySimDischargeZeroLoadNoOp(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := drawPower(t, "pbat")
		if got := simDischarge(p, 0); got != p {
			t.Fatalf("simDischarge(%v, 0) = %v, want %v (zero-load must be a no-op)", p, got, p)
		}
	})
}

// TestPropertyAllocateZeroLoadEquivalence asserts the compute-layer
// zero-load-equivalence ([4.2]): allocateSimLoad at w = 0 returns the input
// pload/pbat/pgrid unchanged, field by field, for any starting state
// (charging, idle, discharging, exporting, importing, above the ceiling).
func TestPropertyAllocateZeroLoadEquivalence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pload := rapid.Float64Range(0, 15000).Draw(t, "pload")
		pbat := drawPower(t, "pbat")
		pgrid := rapid.Float64Range(-10000, 10000).Draw(t, "pgrid")

		alloc := allocateSimLoad(pload, pbat, pgrid, 0)
		if alloc.pload != pload {
			t.Fatalf("pload changed at w=0: got %v want %v", alloc.pload, pload)
		}
		if alloc.pbat != pbat {
			t.Fatalf("pbat changed at w=0: got %v want %v", alloc.pbat, pbat)
		}
		if alloc.pgrid != pgrid {
			t.Fatalf("pgrid changed at w=0: got %v want %v", alloc.pgrid, pgrid)
		}
	})
}

// TestPropertyAllocateEnergyBalance asserts [3.2]/[3.4]: in every state the
// trio stays energy-balanced -- delta load == delta battery + delta grid -- and
// delta load equals the added watts.
func TestPropertyAllocateEnergyBalance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pload := rapid.Float64Range(0, 15000).Draw(t, "pload")
		pbat := drawPower(t, "pbat")
		pgrid := rapid.Float64Range(-10000, 10000).Draw(t, "pgrid")
		w := drawWatts(t)

		alloc := allocateSimLoad(pload, pbat, pgrid, w)
		dLoad := alloc.pload - pload
		dBat := alloc.pbat - pbat
		dGrid := alloc.pgrid - pgrid
		if abs(dLoad-(dBat+dGrid)) > 1e-6 {
			t.Fatalf("energy imbalance: dLoad=%v dBat=%v dGrid=%v", dLoad, dBat, dGrid)
		}
		if abs(dLoad-w) > 1e-6 {
			t.Fatalf("dLoad %v != added watts %v", dLoad, w)
		}
		// Battery never shown discharging below its real value.
		if alloc.pbat < pbat {
			t.Fatalf("simulated pbat %v below real %v", alloc.pbat, pbat)
		}
	})
}

// TestPropertyCutoffMonotonicity asserts [Property B]: a larger added load
// never pushes the simulated cutoff later. The cutoff is computed from
// simDischarge(avgPbat, w); once the battery saturates at the ceiling the
// cutoff plateaus (extra w then moves grid, not battery), so "no later" holds
// with equality at the plateau.
func TestPropertyCutoffMonotonicity(t *testing.T) {
	now := time.Date(2026, 4, 15, 18, 0, 0, 0, time.UTC)
	rapid.Check(t, func(t *rapid.T) {
		soc := rapid.Float64Range(0, 100).Draw(t, "soc")
		avgPbat := drawPower(t, "avgPbat")
		capacity := rapid.Float64Range(1, 20).Draw(t, "capacity")

		w1 := drawWatts(t)
		w2 := drawWatts(t)
		if w2 < w1 {
			w1, w2 = w2, w1
		}

		// wBattery equals w when not exporting; the cutoff series uses the
		// per-series headroom against avgPbat, so feed w directly.
		ct1 := computeCutoffTime(soc, simDischarge(avgPbat, w1), capacity, cutoffPercent, now)
		ct2 := computeCutoffTime(soc, simDischarge(avgPbat, w2), capacity, cutoffPercent, now)

		// More load can only turn "no cutoff" into "a cutoff" or move it
		// earlier; it can never move it later, and never erase an existing one.
		if ct1 != nil && ct2 == nil {
			t.Fatalf("larger w erased a cutoff: w1=%v ct1=%v, w2=%v ct2=nil", w1, *ct1, w2)
		}
		if ct1 != nil && ct2 != nil && ct2.After(*ct1) {
			t.Fatalf("larger w moved cutoff later: w1=%v ct1=%v, w2=%v ct2=%v", w1, *ct1, w2, *ct2)
		}
	})
}

// abs is a tiny float helper for the property assertions.
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
