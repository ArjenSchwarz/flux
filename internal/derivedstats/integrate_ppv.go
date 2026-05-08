package derivedstats

// integratePpv returns the trapezoidal integral of max(ppv, 0) over the
// half-open interval [startUnix, endUnix), expressed in kWh, plus the count
// of readings whose Timestamp is in [startUnix, endUnix). Synthesised edge
// points do not count. The count lets callers distinguish "no samples"
// (emit nil) from "samples summed to zero" (emit 0.0).
//
// Algorithm mirrors integratePload exactly: same 60s pair-gap rule, same
// edge synthesis with linear interpolation, same half-open [start, end),
// same max(ppv, 0) clamping. See integrate.go for the algorithmic
// commentary.
//
// Precondition: readings must be sorted by Timestamp ascending. The bracket
// searches use first-match early-break and produce silently-wrong results on
// unsorted input. DynamoDB queries on the sort key satisfy this in production.
func integratePpv(readings []Reading, startUnix, endUnix int64) (kwh float64, sampleCount int) {
	if startUnix >= endUnix || len(readings) == 0 {
		return 0, 0
	}

	iL := -1
	for i, r := range readings {
		if r.Timestamp < startUnix {
			iL = i
		} else {
			break
		}
	}
	iR := len(readings)
	for i := iL + 1; i < len(readings); i++ {
		if readings[i].Timestamp >= endUnix {
			iR = i
			break
		}
	}

	type pt struct {
		ts  int64
		ppv float64
	}
	pts := make([]pt, 0, (iR-iL-1)+2)

	if iL >= 0 && iL+1 < len(readings) {
		next := readings[iL+1]
		if next.Timestamp > startUnix {
			gap := next.Timestamp - readings[iL].Timestamp
			if gap <= maxPairGapSeconds {
				prev := readings[iL]
				p0 := max(prev.Ppv, 0)
				p1 := max(next.Ppv, 0)
				frac := float64(startUnix-prev.Timestamp) / float64(next.Timestamp-prev.Timestamp)
				pts = append(pts, pt{
					ts:  startUnix,
					ppv: p0 + (p1-p0)*frac,
				})
			}
		}
	}

	for i := iL + 1; i < iR; i++ {
		r := readings[i]
		if r.Timestamp < startUnix || r.Timestamp >= endUnix {
			continue
		}
		pts = append(pts, pt{ts: r.Timestamp, ppv: max(r.Ppv, 0)})
		sampleCount++
	}

	if iR > 0 && iR < len(readings) {
		prev := readings[iR-1]
		next := readings[iR]
		gap := next.Timestamp - prev.Timestamp
		if gap <= maxPairGapSeconds {
			p0 := max(prev.Ppv, 0)
			p1 := max(next.Ppv, 0)
			frac := float64(endUnix-prev.Timestamp) / float64(next.Timestamp-prev.Timestamp)
			pts = append(pts, pt{
				ts:  endUnix,
				ppv: p0 + (p1-p0)*frac,
			})
		}
	}

	if len(pts) < 2 {
		return 0, sampleCount
	}

	var wattSeconds float64
	for i := 1; i < len(pts); i++ {
		a := pts[i-1]
		b := pts[i]
		dt := b.ts - a.ts
		if dt <= 0 || dt > maxPairGapSeconds {
			continue
		}
		wattSeconds += (a.ppv + b.ppv) / 2 * float64(dt)
	}
	return wattSeconds / 3_600_000, sampleCount
}
