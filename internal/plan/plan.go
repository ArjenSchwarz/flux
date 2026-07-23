// Package plan holds the electricity-plan domain: the band model, its
// validation rules, the derived full-day segmentation, and per-date plan
// selection. It is a leaf package — it imports nothing from other Flux
// packages — so the Lambda API, the poller, the backfill CLIs, and the
// migration tool can all depend on it without forming a cycle (mirroring
// the derivedstats layering).
//
// A plan is stored as entered: a default rate plus the exception windows
// that deviate from it (Decision 4). The contiguous full-day band list the
// requirements describe is derived on demand by Segments, which makes gaps
// and partial coverage unrepresentable — uncovered time simply carries the
// default rate.
package plan

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Validation codes. These are the wire values the API returns in the
// pricing error envelope's "error" field and the Swift client mirrors as
// PricingValidationReason cases, so they follow the snake_case convention
// the existing pricing codes established.
const (
	CodeInvertedDates      = "inverted_dates"
	CodeBandWindowInvalid  = "band_window_invalid"
	CodeBandOverlap        = "band_overlap"
	CodeMultipleFreeBands  = "multiple_free_bands"
	CodeSavingsRateMissing = "savings_rate_missing"
	CodeNoRatedBand        = "no_rated_band"
	CodeRatePrecision      = "rate_precision"
	CodeRateOutOfRange     = "rate_out_of_range"
)

// RateCap is the per-rate upper bound carried over from the flat-rate model
// (daily-costs Decision 12) — 10× the highest plausible AU retail tariff,
// which catches order-of-magnitude typos without constraining real use.
const RateCap = 10.0

// minutesPerDay is the exclusive upper bound of a band boundary. Boundaries
// are minute-of-day values in [0, 1440]; 1440 ("24:00") is end-of-day and is
// only ever a window/segment End.
const minutesPerDay = 24 * 60

// dateLayout is the calendar-date format used for plan start/end dates,
// interpreted in Australia/Sydney local time (Q19).
const dateLayout = "2006-01-02"

// Window is one exception to a plan's default rate: a half-open [Start, End)
// slice of the day that is either free or carries its own rate. Boundaries
// are "HH:MM" in Sydney local time; End may be "24:00". Rate is ignored when
// Free is set.
type Window struct {
	Start string
	End   string
	Free  bool
	Rate  float64
}

// Plan is one pricing plan: a date range, a default import rate, the
// exception windows, a flat feed-in rate, and — when the plan has a free
// window — the rate free-window energy is valued at.
//
// EndDate is exclusive (Decision 5): the plan prices days in
// [StartDate, EndDate), so a plan ending on the same date its successor
// starts hands the switch day to the successor with no ±1 arithmetic. An
// empty EndDate means open-ended.
//
// SavingsRefRate is a pointer so "no savings reference rate was supplied" is
// distinguishable from a supplied $0.00 — the difference the
// savings_rate_missing rule turns on.
type Plan struct {
	ID             string
	StartDate      string
	EndDate        string
	DefaultRate    float64
	Windows        []Window
	FeedInRate     float64
	SavingsRefRate *float64
}

// ValidationError is one violated rule. Code is the machine-readable value
// the API and the Swift client switch on; Message is the human-readable
// description the editor surfaces.
type ValidationError struct {
	Code    string
	Message string
}

// ParseBandTime converts an "HH:MM" band boundary to minutes since midnight.
// Unlike derivedstats.ParseOffpeakWindow it accepts "24:00" (1440) as the
// end-of-day boundary every plan's last segment carries; reusing that parser
// here would reject every plan (Q34).
func ParseBandTime(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	for _, i := range [4]int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if m > 59 {
		return 0, false
	}
	total := h*60 + m
	if total > minutesPerDay {
		return 0, false
	}
	return total, true
}

// FormatBandTime is the inverse of ParseBandTime, rendering 1440 as "24:00".
func FormatBandTime(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// Validate returns every violated rule, in a deterministic order. An empty
// result means the plan is acceptable. Cross-plan rules (date-range overlap
// between plans, the single-open-ended rule) are the caller's job — they need
// the whole plan set, which this method does not see.
func (p Plan) Validate() []ValidationError {
	var errs []ValidationError
	add := func(code, format string, args ...any) {
		errs = append(errs, ValidationError{Code: code, Message: fmt.Sprintf(format, args...)})
	}

	errs = append(errs, p.validateDates()...)

	// Band geometry. Windows that fail to parse are excluded from the
	// overlap and coverage checks below — reporting "overlap" on top of an
	// unparseable boundary would be noise, not a second problem.
	parsed := make([]parsedWindow, 0, len(p.Windows))
	for i, w := range p.Windows {
		start, okStart := ParseBandTime(w.Start)
		end, okEnd := ParseBandTime(w.End)
		switch {
		case !okStart:
			add(CodeBandWindowInvalid, "window %d: start %q must be HH:MM between 00:00 and 24:00", i+1, w.Start)
		case !okEnd:
			add(CodeBandWindowInvalid, "window %d: end %q must be HH:MM between 00:00 and 24:00", i+1, w.End)
		case start >= end:
			add(CodeBandWindowInvalid, "window %d: start %s must precede end %s", i+1, w.Start, w.End)
		default:
			parsed = append(parsed, parsedWindow{start: start, end: end, free: w.Free})
		}
	}

	sorted := append([]parsedWindow(nil), parsed...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].start < sorted[i-1].end {
			add(CodeBandOverlap, "windows %s-%s and %s-%s overlap",
				FormatBandTime(sorted[i-1].start), FormatBandTime(sorted[i-1].end),
				FormatBandTime(sorted[i].start), FormatBandTime(sorted[i].end))
			break
		}
	}

	freeCount := 0
	freeMinutes := 0
	for _, w := range parsed {
		if w.free {
			freeCount++
			freeMinutes += w.end - w.start
		}
	}
	if freeCount > 1 {
		add(CodeMultipleFreeBands, "a plan may have at most one free window, found %d", freeCount)
	}
	// AC 1.3: at least one rated band. The only way to have none is a free
	// window covering the whole day — a zero-width default remainder left by
	// rated windows tiling the rest is fine.
	if freeCount > 0 && freeMinutes >= minutesPerDay {
		add(CodeNoRatedBand, "the free window covers the whole day, leaving no rated band")
	}
	if freeCount > 0 && p.SavingsRefRate == nil {
		add(CodeSavingsRateMissing, "a plan with a free window requires a savings reference rate")
	}

	errs = append(errs, p.validateRates()...)
	return errs
}

// parsedWindow is a window whose boundaries have been resolved to minutes.
type parsedWindow struct {
	start, end int
	free       bool
}

// validateDates enforces the exclusive-end date rules: both dates must be
// real calendar dates and EndDate must be strictly after StartDate, since
// under exclusive ends EndDate == StartDate is a plan that prices no days.
func (p Plan) validateDates() []ValidationError {
	invalid := func(format string, args ...any) []ValidationError {
		return []ValidationError{{Code: CodeInvertedDates, Message: fmt.Sprintf(format, args...)}}
	}
	if !validDate(p.StartDate) {
		return invalid("startDate %q must be YYYY-MM-DD", p.StartDate)
	}
	if p.EndDate == "" {
		return nil
	}
	if !validDate(p.EndDate) {
		return invalid("endDate %q must be YYYY-MM-DD", p.EndDate)
	}
	if p.EndDate <= p.StartDate {
		return invalid("endDate %s must be after startDate %s", p.EndDate, p.StartDate)
	}
	return nil
}

// validateRates applies the shared bounds and precision rules (requirement
// 1.6) to every rate the plan carries. A free window's Rate is skipped — it
// carries no rate by contract.
func (p Plan) validateRates() []ValidationError {
	rates := []struct {
		label string
		value float64
	}{
		{"default rate", p.DefaultRate},
		{"feed-in rate", p.FeedInRate},
	}
	if p.SavingsRefRate != nil {
		rates = append(rates, struct {
			label string
			value float64
		}{"savings reference rate", *p.SavingsRefRate})
	}
	for i, w := range p.Windows {
		if w.Free {
			continue
		}
		rates = append(rates, struct {
			label string
			value float64
		}{fmt.Sprintf("window %d rate", i+1), w.Rate})
	}

	var errs []ValidationError
	for _, r := range rates {
		// 4 dp is exactly representable enough in float64 that scaling and
		// comparing against the nearest integer is safe (daily-costs
		// Decision 20).
		scaled := r.value * 10000
		if math.Abs(scaled-math.Round(scaled)) > 1e-6 {
			errs = append(errs, ValidationError{
				Code:    CodeRatePrecision,
				Message: fmt.Sprintf("%s must have at most 4 decimal places", r.label),
			})
		}
		if r.value < 0 || r.value > RateCap {
			errs = append(errs, ValidationError{
				Code:    CodeRateOutOfRange,
				Message: fmt.Sprintf("%s must be between 0 and %.2f AUD per kWh", r.label, RateCap),
			})
		}
	}
	return errs
}

// validDate reports whether s is a real calendar date in YYYY-MM-DD form.
// The structural check runs first so common malformed inputs fail without
// reaching the parser, and time.Parse rejects impossible dates like 02-30.
func validDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	_, err := time.Parse(dateLayout, s)
	return err == nil
}
