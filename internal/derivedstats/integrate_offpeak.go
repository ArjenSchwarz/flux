package derivedstats

// OffpeakDeltas is the five integrated kWh values for an off-peak window
// plus provenance counts the writer persists per AC 5.4. All five kWh fields
// are non-negative by construction (per-sample clamping per Decision 8).
type OffpeakDeltas struct {
	GridImportKwh       float64 // ∫ max(pgrid, 0)
	GridExportKwh       float64 // ∫ max(-pgrid, 0)
	BatteryChargeKwh    float64 // ∫ max(-pbat, 0)
	BatteryDischargeKwh float64 // ∫ max(pbat, 0)
	SolarKwh            float64 // ∫ max(ppv, 0)

	// SampleCount counts readings with Timestamp in [startUnix, endUnix).
	// Synthesised edge points do not count.
	SampleCount int
	// SkippedPairs counts adjacent in-window reading pairs separated by more
	// than maxPairGapSeconds. Identical across the five selectors because the
	// gap rule looks only at timestamps.
	SkippedPairs int
}

// IntegrateOffpeakDeltas returns the five integrated kWh values over the
// half-open interval [startUnix, endUnix) plus provenance counts (AC 5.4).
// Returns (zero, false) when fewer than two usable points (in-window or
// bracketing within 60 s of the boundary) exist — caller treats the row as
// "missing record" per AC 1.6.
//
// Per-sample clamping (Decision 8): signed power is split per-sample before
// integration, so import and export over the same window can both be
// non-zero on a day where the sign of pgrid flipped.
//
// 60-second pair-gap rule (AC 1.3 / Decision 4): any consecutive reading pair
// more than 60 s apart contributes zero energy to the integral. Edge
// synthesis is skipped when the bracketing pair fails the same rule.
//
// Precondition: readings must be sorted by Timestamp ascending. DynamoDB
// queries on the sort key satisfy this in production.
func IntegrateOffpeakDeltas(readings []Reading, startUnix, endUnix int64) (OffpeakDeltas, bool) {
	if startUnix >= endUnix {
		return OffpeakDeltas{}, false
	}

	// Single pass over in-window readings for sample-count and skipped-pair
	// tallies (AC 5.4). The five integrate calls below do not recompute these
	// counts — the gap rule looks only at timestamps, so it is identical
	// across all five selectors.
	var samples, skipped int
	var prevIdx = -1
	for i, r := range readings {
		if r.Timestamp < startUnix || r.Timestamp >= endUnix {
			continue
		}
		samples++
		if prevIdx >= 0 {
			gap := r.Timestamp - readings[prevIdx].Timestamp
			if gap > maxPairGapSeconds {
				skipped++
			}
		}
		prevIdx = i
	}

	// Run the five selector integrals. The usability gate (AC 1.6) lives
	// inside integrate(): it returns 0 with sentinel below when fewer than
	// two construction points exist. We probe with one channel and re-check
	// the construction count to decide ok; since integrate() returns 0 for
	// both "no usable points" and "all zero", we run a separate gate.
	if !hasUsablePoints(readings, startUnix, endUnix) {
		return OffpeakDeltas{}, false
	}

	out := OffpeakDeltas{
		GridImportKwh:       integrate(readings, startUnix, endUnix, func(r Reading) float64 { return max(r.Pgrid, 0) }),
		GridExportKwh:       integrate(readings, startUnix, endUnix, func(r Reading) float64 { return max(-r.Pgrid, 0) }),
		BatteryChargeKwh:    integrate(readings, startUnix, endUnix, func(r Reading) float64 { return max(-r.Pbat, 0) }),
		BatteryDischargeKwh: integrate(readings, startUnix, endUnix, func(r Reading) float64 { return max(r.Pbat, 0) }),
		SolarKwh:            integrate(readings, startUnix, endUnix, func(r Reading) float64 { return max(r.Ppv, 0) }),
		SampleCount:         samples,
		SkippedPairs:        skipped,
	}
	return out, true
}

// hasUsablePoints mirrors the point-construction predicate inside integrate():
// returns true when at least two points (in-window or bracket-synthesised
// within 60 s) would be built. This decouples the (_, false) decision from
// the numeric result so a window that legitimately integrates to 0 W·s is
// still reported as usable.
func hasUsablePoints(readings []Reading, startUnix, endUnix int64) bool {
	if len(readings) == 0 {
		return false
	}
	iL, iR := bracketIndices(readings, startUnix, endUnix)

	points := 0

	// Left bracket synthesis: requires a reading before startUnix AND its
	// successor strictly after startUnix AND the bracket pair within 60 s.
	if iL >= 0 && iL+1 < len(readings) {
		next := readings[iL+1]
		if next.Timestamp > startUnix &&
			next.Timestamp-readings[iL].Timestamp <= maxPairGapSeconds {
			points++
		}
	}
	// Interior points.
	for i := iL + 1; i < iR; i++ {
		t := readings[i].Timestamp
		if t >= startUnix && t < endUnix {
			points++
			if points >= 2 {
				return true
			}
		}
	}
	// Right bracket synthesis.
	if iR > 0 && iR < len(readings) {
		prev := readings[iR-1]
		next := readings[iR]
		if next.Timestamp-prev.Timestamp <= maxPairGapSeconds {
			points++
		}
	}
	return points >= 2
}

// bracketIndices returns:
//   - iL: largest index with readings[iL].Timestamp < startUnix, or -1 when none.
//   - iR: smallest index > iL with readings[iR].Timestamp >= endUnix, or len(readings).
//
// Matches the bracket discovery used in integratePpv / integratePload.
func bracketIndices(readings []Reading, startUnix, endUnix int64) (iL, iR int) {
	iL = -1
	for i, r := range readings {
		if r.Timestamp < startUnix {
			iL = i
		} else {
			break
		}
	}
	iR = len(readings)
	for i := iL + 1; i < len(readings); i++ {
		if readings[i].Timestamp >= endUnix {
			iR = i
			break
		}
	}
	return iL, iR
}

// integrate is the shared trapezoidal integration over [startUnix, endUnix)
// per Decision 9. Control flow mirrors integratePpv / integratePload (see
// integrate.go); two intentional differences:
//
//  1. Sample-count tallying is removed. IntegrateOffpeakDeltas already
//     computes SampleCount and SkippedPairs in a single pass before calling
//     this helper five times — duplicating that work per call would be five
//     extra linear passes for no extra information.
//
//  2. The clamping (e.g. max(r.Pgrid, 0)) is the caller's responsibility via
//     the selector. The helper's job is the algorithm; the per-channel sign
//     policy lives at the call site.
//
// Returns kWh (watt-seconds / 3,600,000). When fewer than two construction
// points exist, returns 0 — the caller distinguishes "unusable" from "zero"
// via hasUsablePoints (the IntegrateOffpeakDeltas (_, false) gate above).
//
// Precondition: readings must be sorted by Timestamp ascending. The bracket
// searches use first-match early-break and produce silently-wrong results on
// unsorted input.
func integrate(readings []Reading, startUnix, endUnix int64,
	selector func(Reading) float64,
) float64 {
	if startUnix >= endUnix || len(readings) == 0 {
		return 0
	}

	iL, iR := bracketIndices(readings, startUnix, endUnix)

	type pt struct {
		ts int64
		p  float64
	}
	pts := make([]pt, 0, (iR-iL-1)+2)

	// Left edge synthesis.
	if iL >= 0 && iL+1 < len(readings) {
		next := readings[iL+1]
		if next.Timestamp > startUnix {
			gap := next.Timestamp - readings[iL].Timestamp
			if gap <= maxPairGapSeconds {
				prev := readings[iL]
				p0 := selector(prev)
				p1 := selector(next)
				frac := float64(startUnix-prev.Timestamp) / float64(next.Timestamp-prev.Timestamp)
				pts = append(pts, pt{
					ts: startUnix,
					p:  p0 + (p1-p0)*frac,
				})
			}
		}
		// next.Timestamp == startUnix → skip; the interior loop picks up the reading.
	}

	// Interior readings.
	for i := iL + 1; i < iR; i++ {
		r := readings[i]
		if r.Timestamp < startUnix || r.Timestamp >= endUnix {
			continue
		}
		pts = append(pts, pt{ts: r.Timestamp, p: selector(r)})
	}

	// Right edge synthesis. iR is the first index with Timestamp >= endUnix,
	// so readings[iR-1].Timestamp < endUnix is guaranteed when iR > 0.
	if iR > 0 && iR < len(readings) {
		prev := readings[iR-1]
		next := readings[iR]
		gap := next.Timestamp - prev.Timestamp
		if gap <= maxPairGapSeconds {
			p0 := selector(prev)
			p1 := selector(next)
			frac := float64(endUnix-prev.Timestamp) / float64(next.Timestamp-prev.Timestamp)
			pts = append(pts, pt{
				ts: endUnix,
				p:  p0 + (p1-p0)*frac,
			})
		}
	}

	if len(pts) < 2 {
		return 0
	}

	var wattSeconds float64
	for i := 1; i < len(pts); i++ {
		a := pts[i-1]
		b := pts[i]
		dt := b.ts - a.ts
		if dt <= 0 || dt > maxPairGapSeconds {
			continue
		}
		wattSeconds += (a.p + b.p) / 2 * float64(dt)
	}
	return wattSeconds / 3_600_000
}
