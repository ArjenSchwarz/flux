package dynamo

import (
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/plan"
)

// IntegrateRatedBands computes one day's rated-band grid import from readings.
//
// It lives here, between the two leaf packages, because three writers need the
// identical number: the poller's day-close capture, the backfill CLI's repair,
// and — through the values they persist — every screen that prices the day.
// A second implementation anywhere would be a second answer (Data Consistency).
//
// Only the rated segments are integrated. Free-window import belongs to the
// flux-offpeak row, which owns that quantity exclusively (Q31).
//
// Segment boundaries come from plan.SegmentBounds, which resolves them on the
// day's wall clock. Deriving them by adding elapsed minutes to midnight is an
// hour off on a DST day, which is exactly the latent bug the peak block used
// to carry — routing both through this helper retires it.
//
// totalKwh is the sum of the raw per-segment integrals rounded once, not the
// sum of the rounded entries: rounding first would let per-band error
// accumulate into a total that disagrees with the sum of what is displayed.
//
// ok is false when the plan has no rated segments, or when any rated segment
// fails the integrator's usability gate. A partially known split counts as
// unavailable (AC 3.6), so callers must not persist a partial result.
func IntegrateRatedBands(
	readings []derivedstats.Reading,
	p plan.Plan,
	day time.Time,
	loc *time.Location,
) (bands []BandImportAttr, totalKwh float64, ok bool) {
	rated := plan.RatedSegments(p)
	if len(rated) == 0 {
		return nil, 0, false
	}

	out := make([]BandImportAttr, 0, len(rated))
	var rawTotal float64
	for _, seg := range rated {
		startUnix, endUnix := plan.SegmentBounds(seg, day, loc)
		deltas, segOK := derivedstats.IntegrateOffpeakDeltas(readings, startUnix, endUnix)
		if !segOK {
			return nil, 0, false
		}
		rawTotal += deltas.GridImportKwh
		out = append(out, BandImportAttr{
			Start: seg.Start,
			End:   seg.End,
			Kwh:   derivedstats.RoundEnergy(deltas.GridImportKwh),
		})
	}
	return out, derivedstats.RoundEnergy(rawTotal), true
}
