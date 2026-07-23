package plan

import (
	"sort"
	"time"
)

// Segment is one band of the derived full-day segmentation: a half-open
// [Start, End) slice of the day carrying either a rate or the free marker.
// The segments of a plan tile 00:00–24:00 exactly (AC 1.1).
type Segment struct {
	Start string
	End   string
	Free  bool
	Rate  float64
}

// Segments derives the plan's contiguous full-day band list from its default
// rate and exception windows. The result always starts at "00:00", ends at
// "24:00", and contains no gaps, overlaps, or zero-width entries.
//
// Abutting segments carrying the same rate are deliberately not merged
// (Q26): the stored per-band split joins to this geometry, so it has to stay
// stable against a merge that would depend on rate equality.
//
// Windows that fail to parse are skipped. Validate is the place that reports
// them; producing a partial segmentation here keeps callers from having to
// handle an error on what is otherwise a total function.
func Segments(p Plan) []Segment {
	type band struct {
		start, end int
		free       bool
		rate       float64
	}
	bands := make([]band, 0, len(p.Windows))
	for _, w := range p.Windows {
		start, okStart := ParseBandTime(w.Start)
		end, okEnd := ParseBandTime(w.End)
		if !okStart || !okEnd || start >= end {
			continue
		}
		bands = append(bands, band{start: start, end: end, free: w.Free, rate: w.Rate})
	}
	sort.SliceStable(bands, func(i, j int) bool { return bands[i].start < bands[j].start })

	segments := make([]Segment, 0, len(bands)*2+1)
	appendDefault := func(from, to int) {
		if from >= to {
			return
		}
		segments = append(segments, Segment{
			Start: FormatBandTime(from),
			End:   FormatBandTime(to),
			Rate:  p.DefaultRate,
		})
	}

	cursor := 0
	for _, b := range bands {
		// A window overlapping its predecessor is invalid (Validate reports
		// band_overlap); clamping keeps the tiling invariant intact rather
		// than emitting an inverted segment.
		if b.start < cursor {
			continue
		}
		appendDefault(cursor, b.start)
		seg := Segment{Start: FormatBandTime(b.start), End: FormatBandTime(b.end), Free: b.free}
		if !b.free {
			seg.Rate = b.rate
		}
		segments = append(segments, seg)
		cursor = b.end
	}
	appendDefault(cursor, minutesPerDay)
	return segments
}

// RatedSegments is Segments filtered to the non-free bands. This is the
// geometry stored in a day's bandImports — free-window import lives on the
// flux-offpeak row, which owns it exclusively (Q31).
func RatedSegments(p Plan) []Segment {
	all := Segments(p)
	rated := make([]Segment, 0, len(all))
	for _, seg := range all {
		if !seg.Free {
			rated = append(rated, seg)
		}
	}
	return rated
}

// Covers reports whether the plan prices the given YYYY-MM-DD date. The end
// date is exclusive (Decision 5), so a plan ending on the date its successor
// starts hands that day to the successor. Lexicographic comparison is correct
// because YYYY-MM-DD sorts chronologically.
func (p Plan) Covers(date string) bool {
	if date < p.StartDate {
		return false
	}
	return p.EndDate == "" || date < p.EndDate
}

// PlanFor returns the plan pricing the given date. At most one plan covers
// any date (AC 2.1) — the validation rules make overlapping ranges
// unstorable — so the first match is the only match.
func PlanFor(plans []Plan, date string) (Plan, bool) {
	for _, p := range plans {
		if p.Covers(date) {
			return p, true
		}
	}
	return Plan{}, false
}

// FreeWindowMinutes returns the plan's free band as minute-of-day bounds.
// ok is false when the plan has no free window — the caller then behaves as
// it does when no off-peak data exists (AC 4.4), never substituting a default
// window.
func (p Plan) FreeWindowMinutes() (startMin, endMin int, ok bool) {
	for _, w := range p.Windows {
		if !w.Free {
			continue
		}
		start, okStart := ParseBandTime(w.Start)
		end, okEnd := ParseBandTime(w.End)
		if !okStart || !okEnd || start >= end {
			return 0, 0, false
		}
		return start, end, true
	}
	return 0, 0, false
}

// FreeWindow returns the free window of the plan pricing the given date
// (AC 4.1). ok is false when no plan covers the date or the covering plan has
// no free band — the two "no window" outcomes callers treat alike.
func FreeWindow(plans []Plan, date string) (startMin, endMin int, ok bool) {
	p, found := PlanFor(plans, date)
	if !found {
		return 0, 0, false
	}
	return p.FreeWindowMinutes()
}

// SegmentBounds resolves a segment to absolute Unix bounds on the given local
// calendar day. Boundaries come from time.Date on the day's wall clock, so
// band membership follows local time across DST transitions (AC 3.8) and
// per-segment integrals over a 23- or 25-hour day still sum to the whole-day
// integral — adjacent segments share a boundary, so the shared points cancel.
//
// Deriving boundaries by adding elapsed minutes to midnight would be an hour
// off on DST days; this helper exists so no caller has to remember that.
//
// A boundary landing inside the skipped or repeated DST hour resolves to
// whichever occurrence time.Date picks. That is deterministic, which is all
// the sum invariant needs, and no real plan has a boundary in 02:00–03:00.
func SegmentBounds(seg Segment, day time.Time, loc *time.Location) (startUnix, endUnix int64) {
	local := day.In(loc)
	at := func(hhmm string) int64 {
		minutes, ok := ParseBandTime(hhmm)
		if !ok {
			minutes = 0
		}
		// time.Date normalises out-of-range fields, so 24:00 becomes the next
		// day's midnight — DST-correctly, because the normalisation happens
		// in the location.
		return time.Date(local.Year(), local.Month(), local.Day(), minutes/60, minutes%60, 0, 0, loc).Unix()
	}
	return at(seg.Start), at(seg.End)
}
