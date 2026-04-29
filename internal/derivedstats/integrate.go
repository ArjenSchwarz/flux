package derivedstats

const (
	// maxPairGapSeconds is the maximum gap between reading pairs for energy
	// integration; larger gaps are skipped to prevent phantom energy
	// accumulation during polling outages.
	maxPairGapSeconds = 60
)

// integratePload returns the trapezoidal integral of max(pload, 0) over the
// half-open interval [startUnix, endUnix), expressed in kWh.
//
// Algorithm (full specification in specs/evening-night-stats/design.md):
//  1. Build a working point sequence pts. Synthesize a left/right edge by
//     linearly interpolating pload between the readings that bracket the
//     period boundary, with negative pload values clamped to zero before
//     interpolation. Skip edge synthesis when the bracketing pair has a gap
//     greater than 60 seconds.
//  2. Append every reading in [startUnix, endUnix) as an interior point
//     (clamped pload). A reading exactly at startUnix is interior; a reading
//     exactly at endUnix is excluded (half-open).
//  3. Sum trapezoidal areas across adjacent pairs in pts, applying the same
//     >60s skip used in computeTodayEnergy. Return watt-seconds / 3,600,000.
//
// The function does no rounding — callers round at serialization time.
//
// Precondition: readings must be sorted by Timestamp ascending. The bracket
// searches use first-match early-break and produce silently-wrong results on
// unsorted input. DynamoDB queries on the sort key satisfy this in production.
func integratePload(readings []Reading, startUnix, endUnix int64) float64 {
	if startUnix >= endUnix || len(readings) == 0 {
		return 0
	}

	// Find left bracket index: largest i with readings[i].Timestamp < startUnix.
	iL := -1
	for i, r := range readings {
		if r.Timestamp < startUnix {
			iL = i
		} else {
			break
		}
	}
	// Find right bracket index: smallest i > iL with readings[i].Timestamp >= endUnix.
	// Starting from iL+1 skips the prefix we already know is below startUnix.
	iR := len(readings)
	for i := iL + 1; i < len(readings); i++ {
		if readings[i].Timestamp >= endUnix {
			iR = i
			break
		}
	}

	type pt struct {
		ts    int64
		pload float64
	}
	pts := make([]pt, 0, (iR-iL-1)+2)

	// Left edge synthesis.
	if iL >= 0 && iL+1 < len(readings) {
		next := readings[iL+1]
		if next.Timestamp > startUnix {
			gap := next.Timestamp - readings[iL].Timestamp
			if gap <= maxPairGapSeconds {
				prev := readings[iL]
				p0 := max(prev.Pload, 0)
				p1 := max(next.Pload, 0)
				frac := float64(startUnix-prev.Timestamp) / float64(next.Timestamp-prev.Timestamp)
				pts = append(pts, pt{
					ts:    startUnix,
					pload: p0 + (p1-p0)*frac,
				})
			}
		}
		// next.Timestamp == startUnix → skip; the interior loop will pick up that reading.
	}

	// Interior readings.
	for i := iL + 1; i < iR; i++ {
		r := readings[i]
		if r.Timestamp < startUnix || r.Timestamp >= endUnix {
			continue
		}
		pts = append(pts, pt{ts: r.Timestamp, pload: max(r.Pload, 0)})
	}

	// Right edge synthesis. iR is the first index with Timestamp >= endUnix,
	// so readings[iR-1].Timestamp < endUnix is guaranteed.
	//
	// When iR-1 == iL (no interior readings), prev is the left-bracket reading
	// and gap spans the entire pre-period region. The 60s gap check then
	// conservatively skips synthesis even if the bracket pair around endUnix
	// is itself tight. Safe — energy is underestimated rather than fabricated —
	// and the all-readings-outside-period case is already covered upstream by
	// the len(pts) < 2 guard.
	if iR > 0 && iR < len(readings) {
		prev := readings[iR-1]
		next := readings[iR]
		gap := next.Timestamp - prev.Timestamp
		if gap <= maxPairGapSeconds {
			p0 := max(prev.Pload, 0)
			p1 := max(next.Pload, 0)
			frac := float64(endUnix-prev.Timestamp) / float64(next.Timestamp-prev.Timestamp)
			pts = append(pts, pt{
				ts:    endUnix,
				pload: p0 + (p1-p0)*frac,
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
		wattSeconds += (a.pload + b.pload) / 2 * float64(dt)
	}
	return wattSeconds / 3_600_000
}
