package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// currentPlan mirrors the plan in production today: free 11:00–14:00 with a
// single flat rate covering the rest of the day (requirement 1.8).
func currentPlan() Plan {
	savings := 0.35
	return Plan{
		ID:             "current",
		StartDate:      "2026-01-01",
		DefaultRate:    0.35,
		Windows:        []Window{{Start: "11:00", End: "14:00", Free: true}},
		FeedInRate:     0.05,
		SavingsRefRate: &savings,
	}
}

// newPlan mirrors the incoming plan: free 10:00–15:00, a cheaper 01:00–06:00
// band, and the standard rate everywhere else (requirement 1.8 / Q3).
func newPlan() Plan {
	savings := 0.35
	return Plan{
		ID:          "successor",
		StartDate:   "2026-08-01",
		DefaultRate: 0.35,
		Windows: []Window{
			{Start: "10:00", End: "15:00", Free: true},
			{Start: "01:00", End: "06:00", Rate: 0.28},
		},
		FeedInRate:     0.05,
		SavingsRefRate: &savings,
	}
}

// codes flattens a validation result to its codes so tests assert on the rule
// that fired rather than on message wording.
func codes(errs []ValidationError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Code
	}
	return out
}

// TestParseBandTime pins the band-boundary parser. It is deliberately NOT
// derivedstats.ParseOffpeakWindow: that parser rejects h > 23 and would reject
// the 24:00 end-of-day boundary every plan's last segment carries (Q34).
func TestParseBandTime(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in     string
		want   int
		wantOK bool
	}{
		"midnight":            {in: "00:00", want: 0, wantOK: true},
		"morning boundary":    {in: "01:00", want: 60, wantOK: true},
		"free window start":   {in: "11:00", want: 660, wantOK: true},
		"last minute of day":  {in: "23:59", want: 1439, wantOK: true},
		"end of day":          {in: "24:00", want: 1440, wantOK: true},
		"past end of day":     {in: "24:01", wantOK: false},
		"hour out of range":   {in: "25:00", wantOK: false},
		"minute out of range": {in: "10:60", wantOK: false},
		"unpadded hour":       {in: "9:00", wantOK: false},
		"missing colon":       {in: "1000", wantOK: false},
		"non-numeric":         {in: "aa:bb", wantOK: false},
		"empty":               {in: "", wantOK: false},
		"too long":            {in: "10:00:00", wantOK: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseBandTime(tc.in)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

// TestFormatBandTime pins the inverse of ParseBandTime, including the 24:00
// end-of-day representation.
func TestFormatBandTime(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   int
		want string
	}{
		"midnight":   {in: 0, want: "00:00"},
		"one am":     {in: 60, want: "01:00"},
		"half past":  {in: 690, want: "11:30"},
		"end of day": {in: 1440, want: "24:00"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, FormatBandTime(tc.in))
		})
	}
}

// TestValidateAcceptsRealPlans covers requirement 1.8: the model must express
// both the current and the incoming plan without a validation failure.
func TestValidateAcceptsRealPlans(t *testing.T) {
	t.Parallel()
	tests := map[string]Plan{
		"current plan": currentPlan(),
		"new plan":     newPlan(),
		"no free band": {StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05},
		"open ended":   currentPlan(),
		"rated windows only": {
			StartDate:   "2026-01-01",
			DefaultRate: 0.35,
			Windows:     []Window{{Start: "01:00", End: "06:00", Rate: 0.28}},
			FeedInRate:  0.05,
		},
	}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, p.Validate())
		})
	}
}

// TestValidateZeroWidthDefaultRemainder covers the noRatedBand carve-out: a
// free window abutting rated windows that tile the rest of the day leaves a
// zero-width default remainder, which is valid — the plan still has rated
// bands (requirement 1.3).
func TestValidateZeroWidthDefaultRemainder(t *testing.T) {
	t.Parallel()
	savings := 0.35
	p := Plan{
		StartDate:   "2026-01-01",
		DefaultRate: 0.35,
		Windows: []Window{
			{Start: "00:00", End: "10:00", Rate: 0.28},
			{Start: "10:00", End: "15:00", Free: true},
			{Start: "15:00", End: "24:00", Rate: 0.30},
		},
		FeedInRate:     0.05,
		SavingsRefRate: &savings,
	}
	assert.Empty(t, p.Validate())
}

// TestValidateBandRules covers each band-specific validation code from the
// design's error table (AC 1.7 / 7.2).
func TestValidateBandRules(t *testing.T) {
	t.Parallel()
	savings := 0.35
	tests := map[string]struct {
		plan     Plan
		wantCode string
	}{
		"unparseable window start": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				Windows: []Window{{Start: "9:00", End: "14:00", Rate: 0.2}}},
			wantCode: CodeBandWindowInvalid,
		},
		"unparseable window end": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				Windows: []Window{{Start: "11:00", End: "24:30", Rate: 0.2}}},
			wantCode: CodeBandWindowInvalid,
		},
		"inverted window": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				Windows: []Window{{Start: "14:00", End: "11:00", Rate: 0.2}}},
			wantCode: CodeBandWindowInvalid,
		},
		"zero width window": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				Windows: []Window{{Start: "11:00", End: "11:00", Rate: 0.2}}},
			wantCode: CodeBandWindowInvalid,
		},
		"overlapping windows": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				Windows: []Window{
					{Start: "10:00", End: "14:00", Rate: 0.2},
					{Start: "13:00", End: "16:00", Rate: 0.3},
				}},
			wantCode: CodeBandOverlap,
		},
		"overlapping windows out of order": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				Windows: []Window{
					{Start: "13:00", End: "16:00", Rate: 0.3},
					{Start: "10:00", End: "14:00", Rate: 0.2},
				}},
			wantCode: CodeBandOverlap,
		},
		"two free bands": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				SavingsRefRate: &savings,
				Windows: []Window{
					{Start: "10:00", End: "12:00", Free: true},
					{Start: "13:00", End: "15:00", Free: true},
				}},
			wantCode: CodeMultipleFreeBands,
		},
		"free band without savings rate": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				Windows: []Window{{Start: "11:00", End: "14:00", Free: true}}},
			wantCode: CodeSavingsRateMissing,
		},
		"free band spans whole day": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				SavingsRefRate: &savings,
				Windows:        []Window{{Start: "00:00", End: "24:00", Free: true}}},
			wantCode: CodeNoRatedBand,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, codes(tc.plan.Validate()), tc.wantCode)
		})
	}
}

// TestValidateRateBounds covers requirement 1.6 — bounds and precision apply
// to every rate the plan carries, including per-window rates.
func TestValidateRateBounds(t *testing.T) {
	t.Parallel()
	overCap := 10.0001
	tests := map[string]struct {
		plan     Plan
		wantCode string
	}{
		"default rate negative": {
			plan:     Plan{StartDate: "2026-01-01", DefaultRate: -0.01, FeedInRate: 0.05},
			wantCode: CodeRateOutOfRange,
		},
		"default rate over cap": {
			plan:     Plan{StartDate: "2026-01-01", DefaultRate: 10.0001, FeedInRate: 0.05},
			wantCode: CodeRateOutOfRange,
		},
		"feed-in rate over cap": {
			plan:     Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 10.5},
			wantCode: CodeRateOutOfRange,
		},
		"window rate over cap": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				Windows: []Window{{Start: "01:00", End: "06:00", Rate: 11}}},
			wantCode: CodeRateOutOfRange,
		},
		"savings rate over cap": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				SavingsRefRate: &overCap,
				Windows:        []Window{{Start: "11:00", End: "14:00", Free: true}}},
			wantCode: CodeRateOutOfRange,
		},
		"default rate too precise": {
			plan:     Plan{StartDate: "2026-01-01", DefaultRate: 0.12345, FeedInRate: 0.05},
			wantCode: CodeRatePrecision,
		},
		"feed-in rate too precise": {
			plan:     Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.054321},
			wantCode: CodeRatePrecision,
		},
		"window rate too precise": {
			plan: Plan{StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
				Windows: []Window{{Start: "01:00", End: "06:00", Rate: 0.123456}}},
			wantCode: CodeRatePrecision,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, codes(tc.plan.Validate()), tc.wantCode)
		})
	}
}

// TestValidateFreeWindowRateIgnored pins that a free window's Rate field is
// not rate-checked — it carries no rate by contract (design: "Rate ignored
// when Free"), so a stray value must not fail validation.
func TestValidateFreeWindowRateIgnored(t *testing.T) {
	t.Parallel()
	savings := 0.35
	p := Plan{
		StartDate: "2026-01-01", DefaultRate: 0.35, FeedInRate: 0.05,
		SavingsRefRate: &savings,
		Windows:        []Window{{Start: "11:00", End: "14:00", Free: true, Rate: 99.999999}},
	}
	assert.Empty(t, p.Validate())
}

// TestValidateDates covers the exclusive-end date rules (Decision 5): a
// zero-day plan (endDate == startDate) is rejected alongside inverted ranges.
func TestValidateDates(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		startDate string
		endDate   string
		wantCode  string // empty => valid
	}{
		"open ended":            {startDate: "2026-01-01"},
		"closed range":          {startDate: "2026-01-01", endDate: "2026-08-01"},
		"one day plan":          {startDate: "2026-01-01", endDate: "2026-01-02"},
		"zero day plan":         {startDate: "2026-01-01", endDate: "2026-01-01", wantCode: CodeInvertedDates},
		"inverted range":        {startDate: "2026-08-01", endDate: "2026-01-01", wantCode: CodeInvertedDates},
		"malformed start date":  {startDate: "2026-1-1", wantCode: CodeInvertedDates},
		"empty start date":      {startDate: "", wantCode: CodeInvertedDates},
		"malformed end date":    {startDate: "2026-01-01", endDate: "01-08-2026", wantCode: CodeInvertedDates},
		"impossible start date": {startDate: "2026-02-30", wantCode: CodeInvertedDates},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := Plan{StartDate: tc.startDate, EndDate: tc.endDate, DefaultRate: 0.35, FeedInRate: 0.05}
			got := codes(p.Validate())
			if tc.wantCode == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.wantCode)
		})
	}
}

// TestValidationErrorCarriesMessage pins that every emitted error has both a
// machine-readable code and a human-readable message — the API surfaces the
// code, the editor surfaces the message.
func TestValidationErrorCarriesMessage(t *testing.T) {
	t.Parallel()
	p := Plan{StartDate: "2026-01-01", DefaultRate: -1, FeedInRate: 0.05}
	errs := p.Validate()
	require.NotEmpty(t, errs)
	for _, e := range errs {
		assert.NotEmpty(t, e.Code)
		assert.NotEmpty(t, e.Message)
	}
}
