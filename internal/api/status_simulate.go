package api

import (
	"errors"
	"strconv"
	"strings"
)

// maxDischargeW is the inverter's sustained discharge ceiling in watts,
// derived from the single maxDischargeKW constant so the ceiling has one
// definition (Decision 14). Simulated battery discharge is capped here; the
// surplus spills to grid import.
const maxDischargeW = maxDischargeKW * 1000

// simLoadMaxWatts is the inclusive upper bound on the simulateLoadWatts
// parameter (20 kW). Values above it are rejected (Req 1.3 / 4.6); presets are
// validated to the same bound so a stored preset can never produce a rejected
// request.
const simLoadMaxWatts = 20000

// errInvalidSimLoad is returned by parseSimulateLoad when the parameter is
// present but unparseable or outside (0, simLoadMaxWatts].
var errInvalidSimLoad = errors.New("simulateLoadWatts must be an integer between 1 and 20000")

// parseSimulateLoad reads the simulateLoadWatts query parameter. An absent
// (empty) value returns (0, nil) — no simulation. A present value must be an
// integer strictly greater than zero and at most simLoadMaxWatts; anything
// else returns errInvalidSimLoad so the handler can 400 ([4.6]). The
// zero-load-equivalence invariant ([4.2]) is an internal compute-path
// property, deliberately NOT reachable via the wire (0 is rejected here).
func parseSimulateLoad(q map[string]string) (float64, error) {
	raw, ok := q["simulateLoadWatts"]
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	// Integer-only: a float or any non-numeric value is rejected.
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, errInvalidSimLoad
	}
	if v <= 0 || v > simLoadMaxWatts {
		return 0, errInvalidSimLoad
	}
	return float64(v), nil
}

// simAllocation holds the simulated live trio after the priority waterfall has
// allocated the added load. Sign convention: pbat > 0 discharge, pgrid > 0
// import (pgrid < 0 export).
type simAllocation struct {
	pload float64
	pbat  float64
	pgrid float64
}

// allocateSimLoad applies the priority waterfall (Decision 14) to the live
// trio: first reduce any grid export, then draw from the battery up to its
// remaining discharge headroom, then meet the remainder with grid import. The
// returned trio is energy-balanced (delta load == delta battery + delta grid)
// and never shows the battery discharging below its real value. At w == 0
// every step is a no-op, so the result equals the input exactly ([4.2]).
func allocateSimLoad(pload, pbat, pgrid, w float64) simAllocation {
	exportReduction := exportReductionFor(pgrid, w)
	wBattery := w - exportReduction
	absorbed := batteryAbsorbed(pbat, wBattery)
	overflow := wBattery - absorbed
	return simAllocation{
		pload: pload + w,
		pbat:  pbat + absorbed,
		pgrid: pgrid + exportReduction + overflow,
	}
}

// exportReductionFor returns the portion of the added load served by cutting
// current grid export toward zero. Only positive export (pgrid < 0) can be
// reduced; the result is never more than the added load.
func exportReductionFor(pgrid, w float64) float64 {
	export := -pgrid
	if export < 0 {
		export = 0
	}
	return minFloat(w, export)
}

// headroom returns the additional discharge the battery can take on before
// reaching the inverter ceiling. Zero when p is already at or above the
// ceiling — the key to the no-op property: capping only the *added* portion
// (via headroom) rather than min(p+w, ceiling) leaves a real reading already
// at/above the ceiling untouched at w == 0.
func headroom(p float64) float64 {
	h := maxDischargeW - p
	if h < 0 {
		return 0
	}
	return h
}

// batteryAbsorbed returns how much of wBattery the battery takes on, bounded by
// its remaining headroom.
func batteryAbsorbed(p, wBattery float64) float64 {
	return minFloat(wBattery, headroom(p))
}

// simDischarge returns the simulated discharge for a series power p given the
// watts reaching the battery. It caps only the added portion via headroom, so
// simDischarge(p, wBattery) >= p always (it never lowers a real reading) and
// simDischarge(p, 0) == p for every p, including p >= the ceiling ([3.4],
// [4.2]). Evaluated per series (live pbat, rolling avgPbat).
func simDischarge(p, wBattery float64) float64 {
	return p + batteryAbsorbed(p, wBattery)
}

// minFloat returns the smaller of two float64 values.
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
