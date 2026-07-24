package plan

// This file holds the three-tier day-cost resolution (Decision 6). It is the
// Go side of a formula that also lives in FluxCore; the two are pinned to each
// other by the shared vectors in internal/api/testdata/pricing_costs.json.
//
// Tier 2 is the pre-band DayCosts formula verbatim — server-peak preference,
// zero clamp, nil-off-peak path. That exactness is what makes AC 5.2 hold: the
// migration tool computes its goldens with this same code, so a migrated
// single-rate plan reprices every historical day to the identical number.

// legacyFreeWindowStart / legacyFreeWindowEnd is the window every pre-feature
// off-peak row was integrated under. Rows predating the geometry snapshot
// carry no windowStart/windowEnd, and this is the only window they can have
// had.
const (
	legacyFreeWindowStart = "11:00"
	legacyFreeWindowEnd   = "14:00"
)

// BandImport is one stored per-band import figure. Each entry snapshots the
// geometry it was captured under (Q23) so a later window edit is detectable as
// a mismatch rather than silently mispricing the day.
type BandImport struct {
	Start string
	End   string
	Kwh   float64
}

// OffpeakRow is the flux-offpeak row's contribution to costing. That row
// exclusively owns free-window import (Q31), so it is the only source of the
// free band's kWh.
type OffpeakRow struct {
	GridImportKwh float64
	// WindowStart / WindowEnd is the geometry the row was integrated under.
	// Empty means a pre-feature row, which can only be 11:00–14:00.
	WindowStart string
	WindowEnd   string
	// IntegratedAt / SampleCount are the integration provenance. A row with
	// IntegratedAt set but no samples is a zero-delta artifact, not a measured
	// zero, so it cannot be used to price a free band.
	IntegratedAt string
	SampleCount  int
}

// Usable reports whether the row's free-window import is a real measurement.
func (r OffpeakRow) Usable() bool {
	return r.IntegratedAt == "" || r.SampleCount > 0
}

// Geometry returns the window the row was integrated under, substituting the
// pre-feature default when the row carries no snapshot.
func (r OffpeakRow) Geometry() (start, end string) {
	if r.WindowStart == "" || r.WindowEnd == "" {
		return legacyFreeWindowStart, legacyFreeWindowEnd
	}
	return r.WindowStart, r.WindowEnd
}

// DayEnergy is one day's stored energy, as cost resolution sees it. Pointer
// fields distinguish "never recorded" from a measured zero — the distinction
// tier 2's formula table turns on.
type DayEnergy struct {
	EInput            *float64
	EOutput           *float64
	Offpeak           *OffpeakRow
	PeakGridImportKwh *float64
	BandImports       []BandImport
}

// Costs is the four figures every screen shows for a day or a period.
type Costs struct {
	ImportCost   float64
	FeedInIncome float64
	Net          float64
	Savings      float64
}

// Tier identifies which resolution path produced a Costs value. Exposed so
// tests and the migration tool can assert on the path, not just the number.
type Tier int

const (
	// TierBanded prices each rated band at its own rate from the stored split.
	TierBanded Tier = 1
	// TierSingleRate is the pre-band formula, applicable whenever the plan's
	// rated segments share one rate — which every migrated legacy plan does.
	TierSingleRate Tier = 2
	// TierFallback prices all import at the plan's highest rate with no
	// savings (AC 3.6). Reachable only for multi-rate plans.
	TierFallback Tier = 3
)

// DayCosts resolves one day's costs under the plan pricing that day.
// Resolution order is tier 1 → 2 → 3; the tier that produced the result is
// returned alongside it.
func DayCosts(p Plan, e DayEnergy) (Costs, Tier) {
	feedIn := deref(e.EOutput) * p.FeedInRate
	finish := func(importCost, savings float64, tier Tier) (Costs, Tier) {
		return Costs{
			ImportCost:   importCost,
			FeedInIncome: feedIn,
			Net:          importCost - feedIn,
			Savings:      savings,
		}, tier
	}

	rated := RatedSegments(p)

	if importCost, savings, ok := bandedCosts(p, rated, e); ok {
		return finish(importCost, savings, TierBanded)
	}
	if rate, ok := singleRate(rated); ok {
		importCost, savings := singleRateCosts(p, e, rate)
		return finish(importCost, savings, TierSingleRate)
	}
	// AC 3.6: an unresolvable split prices everything at the highest rate and
	// shows no savings — the conservative overestimate every screen must agree
	// on.
	return finish(deref(e.EInput)*maxRate(rated), 0, TierFallback)
}

// bandedCosts prices the day from the stored split. It applies only when the
// split's geometry exactly matches the plan's rated segments AND the free
// band's import is resolvable — a partially known split is unavailable
// (AC 3.6), not partially used.
func bandedCosts(p Plan, rated []Segment, e DayEnergy) (importCost, savings float64, ok bool) {
	if len(e.BandImports) != len(rated) || len(rated) == 0 {
		return 0, 0, false
	}
	for i, seg := range rated {
		if e.BandImports[i].Start != seg.Start || e.BandImports[i].End != seg.End {
			return 0, 0, false
		}
		importCost += e.BandImports[i].Kwh * seg.Rate
	}

	freeStart, freeEnd, hasFree := p.freeWindowStrings()
	if !hasFree {
		// No free band: the rated segments are the whole day and there is
		// nothing to value as savings.
		return importCost, 0, true
	}
	if e.Offpeak == nil || !e.Offpeak.Usable() {
		return 0, 0, false
	}
	if rowStart, rowEnd := e.Offpeak.Geometry(); rowStart != freeStart || rowEnd != freeEnd {
		return 0, 0, false
	}
	if p.SavingsRefRate == nil {
		return importCost, 0, true
	}
	return importCost, e.Offpeak.GridImportKwh * *p.SavingsRefRate, true
}

// singleRateCosts is the pre-band formula, unchanged. Peak kWh prefers the
// server-computed value over the eInput − off-peak residual: the two differ by
// ~1.5% by design (a shared sampling artifact), and pricing the measured value
// is what keeps migrated history identical (Q30).
func singleRateCosts(p Plan, e DayEnergy, rate float64) (importCost, savings float64) {
	total := deref(e.EInput)
	if e.Offpeak == nil {
		if e.PeakGridImportKwh != nil {
			return *e.PeakGridImportKwh * rate, 0
		}
		return total * rate, 0
	}

	off := e.Offpeak.GridImportKwh
	peak := max(0, total-off)
	if e.PeakGridImportKwh != nil {
		peak = *e.PeakGridImportKwh
	}
	if p.SavingsRefRate != nil {
		savings = off * *p.SavingsRefRate
	}
	return peak * rate, savings
}

// freeWindowStrings returns the plan's free band boundaries as they appear in
// the segmentation, for geometry comparison against a stored row.
func (p Plan) freeWindowStrings() (start, end string, ok bool) {
	for _, seg := range Segments(p) {
		if seg.Free {
			return seg.Start, seg.End, true
		}
	}
	return "", "", false
}

// singleRate returns the rate shared by every rated segment. ok is false when
// the segments carry more than one rate — the only case that can reach the
// fallback tier.
func singleRate(rated []Segment) (float64, bool) {
	if len(rated) == 0 {
		return 0, false
	}
	rate := rated[0].Rate
	for _, seg := range rated[1:] {
		if seg.Rate != rate {
			return 0, false
		}
	}
	return rate, true
}

// maxRate returns the highest rate among the rated segments — the rate the
// fallback tier prices the whole day at.
func maxRate(rated []Segment) float64 {
	var highest float64
	for _, seg := range rated {
		highest = max(highest, seg.Rate)
	}
	return highest
}

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
