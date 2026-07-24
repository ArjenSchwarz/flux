package plan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sydney is the timezone every plan date and band boundary is interpreted in
// (Q19). Loaded once so the DST cases below exercise the real tzdata rules.
var sydney = func() *time.Location {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		panic(err)
	}
	return loc
}()

// TestSegments pins the derived full-day segmentation. Segments must tile
// 00:00–24:00 exactly (AC 1.1) with the default rate filling everything the
// windows do not claim.
func TestSegments(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		plan Plan
		want []Segment
	}{
		"no windows is one default segment": {
			plan: Plan{DefaultRate: 0.35},
			want: []Segment{{Start: "00:00", End: "24:00", Rate: 0.35}},
		},
		"current plan": {
			plan: currentPlan(),
			want: []Segment{
				{Start: "00:00", End: "11:00", Rate: 0.35},
				{Start: "11:00", End: "14:00", Free: true},
				{Start: "14:00", End: "24:00", Rate: 0.35},
			},
		},
		"new plan": {
			plan: newPlan(),
			want: []Segment{
				{Start: "00:00", End: "01:00", Rate: 0.35},
				{Start: "01:00", End: "06:00", Rate: 0.28},
				{Start: "06:00", End: "10:00", Rate: 0.35},
				{Start: "10:00", End: "15:00", Free: true},
				{Start: "15:00", End: "24:00", Rate: 0.35},
			},
		},
		"window at start of day leaves no leading default": {
			plan: Plan{DefaultRate: 0.35, Windows: []Window{{Start: "00:00", End: "06:00", Rate: 0.28}}},
			want: []Segment{
				{Start: "00:00", End: "06:00", Rate: 0.28},
				{Start: "06:00", End: "24:00", Rate: 0.35},
			},
		},
		"window at end of day leaves no trailing default": {
			plan: Plan{DefaultRate: 0.35, Windows: []Window{{Start: "18:00", End: "24:00", Rate: 0.28}}},
			want: []Segment{
				{Start: "00:00", End: "18:00", Rate: 0.35},
				{Start: "18:00", End: "24:00", Rate: 0.28},
			},
		},
		"windows tiling the day leave no zero-width remainders": {
			plan: Plan{DefaultRate: 0.35, Windows: []Window{
				{Start: "00:00", End: "12:00", Rate: 0.28},
				{Start: "12:00", End: "24:00", Rate: 0.31},
			}},
			want: []Segment{
				{Start: "00:00", End: "12:00", Rate: 0.28},
				{Start: "12:00", End: "24:00", Rate: 0.31},
			},
		},
		"unsorted windows are ordered by start": {
			plan: Plan{DefaultRate: 0.35, Windows: []Window{
				{Start: "18:00", End: "20:00", Rate: 0.40},
				{Start: "01:00", End: "06:00", Rate: 0.28},
			}},
			want: []Segment{
				{Start: "00:00", End: "01:00", Rate: 0.35},
				{Start: "01:00", End: "06:00", Rate: 0.28},
				{Start: "06:00", End: "18:00", Rate: 0.35},
				{Start: "18:00", End: "20:00", Rate: 0.40},
				{Start: "20:00", End: "24:00", Rate: 0.35},
			},
		},
		// Q26: abutting same-rate segments are NOT merged. Stable geometry is
		// what makes the stored bandImports join deterministic.
		"abutting same-rate windows are not merged": {
			plan: Plan{DefaultRate: 0.35, Windows: []Window{
				{Start: "00:00", End: "06:00", Rate: 0.35},
				{Start: "06:00", End: "12:00", Rate: 0.35},
			}},
			want: []Segment{
				{Start: "00:00", End: "06:00", Rate: 0.35},
				{Start: "06:00", End: "12:00", Rate: 0.35},
				{Start: "12:00", End: "24:00", Rate: 0.35},
			},
		},
		"window rate equal to the default is still its own segment": {
			plan: Plan{DefaultRate: 0.35, Windows: []Window{{Start: "10:00", End: "12:00", Rate: 0.35}}},
			want: []Segment{
				{Start: "00:00", End: "10:00", Rate: 0.35},
				{Start: "10:00", End: "12:00", Rate: 0.35},
				{Start: "12:00", End: "24:00", Rate: 0.35},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Segments(tc.plan))
		})
	}
}

// TestRatedSegments pins the rated-only view stored in bandImports — the free
// band's import lives on the flux-offpeak row instead (Q31).
func TestRatedSegments(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []Segment{
		{Start: "00:00", End: "01:00", Rate: 0.35},
		{Start: "01:00", End: "06:00", Rate: 0.28},
		{Start: "06:00", End: "10:00", Rate: 0.35},
		{Start: "15:00", End: "24:00", Rate: 0.35},
	}, RatedSegments(newPlan()))
}

// TestCovers pins the exclusive end-date semantics of Decision 5.
func TestCovers(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		plan Plan
		date string
		want bool
	}{
		"open-ended covers its start date":  {plan: Plan{StartDate: "2026-01-01"}, date: "2026-01-01", want: true},
		"open-ended covers the far future":  {plan: Plan{StartDate: "2026-01-01"}, date: "2099-12-31", want: true},
		"open-ended excludes earlier dates": {plan: Plan{StartDate: "2026-01-01"}, date: "2025-12-31", want: false},
		"closed covers its start date":      {plan: Plan{StartDate: "2026-01-01", EndDate: "2026-08-01"}, date: "2026-01-01", want: true},
		"closed covers the day before its end": {plan: Plan{StartDate: "2026-01-01", EndDate: "2026-08-01"},
			date: "2026-07-31", want: true},
		"closed excludes its end date": {plan: Plan{StartDate: "2026-01-01", EndDate: "2026-08-01"},
			date: "2026-08-01", want: false},
		"closed excludes later dates": {plan: Plan{StartDate: "2026-01-01", EndDate: "2026-08-01"},
			date: "2026-08-02", want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.plan.Covers(tc.date))
		})
	}
}

// switchDayPlans is the succession pair from AC 2.2: the predecessor's
// exclusive end date is the successor's start date, both literally 2026-08-01.
func switchDayPlans() []Plan {
	pred := currentPlan()
	pred.EndDate = "2026-08-01"
	return []Plan{pred, newPlan()}
}

// TestPlanFor covers AC 2.1/2.2 — every day is priced by at most one plan and
// the switch day belongs to the successor.
func TestPlanFor(t *testing.T) {
	t.Parallel()
	plans := switchDayPlans()
	tests := map[string]struct {
		date   string
		wantID string // empty => no plan covers the date
	}{
		"well inside the predecessor": {date: "2026-05-01", wantID: "current"},
		"switch day eve":              {date: "2026-07-31", wantID: "current"},
		"switch day":                  {date: "2026-08-01", wantID: "successor"},
		"day after the switch":        {date: "2026-08-02", wantID: "successor"},
		"before any plan":             {date: "2025-12-31"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := PlanFor(plans, tc.date)
			if tc.wantID == "" {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			assert.Equal(t, tc.wantID, got.ID)
		})
	}
}

// TestPlanForGapBetweenPlans covers AC 2.4/2.7: ending the open-ended plan
// without a successor leaves the days after it unpriced.
func TestPlanForGapBetweenPlans(t *testing.T) {
	t.Parallel()
	closed := currentPlan()
	closed.EndDate = "2026-08-01"
	_, ok := PlanFor([]Plan{closed}, "2026-08-01")
	assert.False(t, ok)
}

// TestFreeWindow covers AC 4.1/4.2 — the free window comes from the plan
// pricing the day in question, switching automatically on the switch date.
func TestFreeWindow(t *testing.T) {
	t.Parallel()
	plans := switchDayPlans()
	noFree := Plan{ID: "flat", StartDate: "2026-01-01", DefaultRate: 0.35}

	tests := map[string]struct {
		plans     []Plan
		date      string
		wantStart int
		wantEnd   int
		wantOK    bool
	}{
		"predecessor window":     {plans: plans, date: "2026-07-31", wantStart: 660, wantEnd: 840, wantOK: true},
		"successor window":       {plans: plans, date: "2026-08-01", wantStart: 600, wantEnd: 900, wantOK: true},
		"no plan covers the day": {plans: plans, date: "2025-01-01"},
		"plan has no free band":  {plans: []Plan{noFree}, date: "2026-05-01"},
		"empty plan set":         {plans: nil, date: "2026-05-01"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start, end, ok := FreeWindow(tc.plans, tc.date)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantStart, start)
				assert.Equal(t, tc.wantEnd, end)
			}
		})
	}
}

// TestFreeWindowMinutes pins the per-plan accessor the package-level
// FreeWindow builds on.
func TestFreeWindowMinutes(t *testing.T) {
	t.Parallel()
	start, end, ok := currentPlan().FreeWindowMinutes()
	require.True(t, ok)
	assert.Equal(t, 660, start)
	assert.Equal(t, 840, end)

	_, _, ok = Plan{DefaultRate: 0.35}.FreeWindowMinutes()
	assert.False(t, ok)
}

// TestSegmentBounds pins wall-clock boundary resolution (AC 3.8). Boundaries
// must come from time.Date on the local calendar day — NOT from
// dayStart.Add(elapsed), which is an hour off on DST days.
func TestSegmentBounds(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		date     string
		segment  Segment
		wantFrom string // RFC3339 in Sydney local time
		wantTo   string
	}{
		"ordinary day": {
			date:     "2026-04-12",
			segment:  Segment{Start: "11:00", End: "14:00"},
			wantFrom: "2026-04-12T11:00:00+10:00",
			wantTo:   "2026-04-12T14:00:00+10:00",
		},
		"whole ordinary day": {
			date:     "2026-04-12",
			segment:  Segment{Start: "00:00", End: "24:00"},
			wantFrom: "2026-04-12T00:00:00+10:00",
			wantTo:   "2026-04-13T00:00:00+10:00",
		},
		// 2026-04-05 is Sydney's 25-hour day (clocks go back 03:00 → 02:00).
		"25-hour day spans midnight to midnight": {
			date:     "2026-04-05",
			segment:  Segment{Start: "00:00", End: "24:00"},
			wantFrom: "2026-04-05T00:00:00+11:00",
			wantTo:   "2026-04-06T00:00:00+10:00",
		},
		"25-hour day afternoon segment is unaffected": {
			date:     "2026-04-05",
			segment:  Segment{Start: "11:00", End: "14:00"},
			wantFrom: "2026-04-05T11:00:00+10:00",
			wantTo:   "2026-04-05T14:00:00+10:00",
		},
		// 2026-10-04 is Sydney's 23-hour day (clocks go forward 02:00 → 03:00).
		"23-hour day spans midnight to midnight": {
			date:     "2026-10-04",
			segment:  Segment{Start: "00:00", End: "24:00"},
			wantFrom: "2026-10-04T00:00:00+10:00",
			wantTo:   "2026-10-05T00:00:00+11:00",
		},
		"23-hour day afternoon segment is unaffected": {
			date:     "2026-10-04",
			segment:  Segment{Start: "10:00", End: "15:00"},
			wantFrom: "2026-10-04T10:00:00+11:00",
			wantTo:   "2026-10-04T15:00:00+11:00",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			day, err := time.ParseInLocation(dateLayout, tc.date, sydney)
			require.NoError(t, err)
			from, to := SegmentBounds(tc.segment, day, sydney)
			assert.Equal(t, tc.wantFrom, time.Unix(from, 0).In(sydney).Format(time.RFC3339))
			assert.Equal(t, tc.wantTo, time.Unix(to, 0).In(sydney).Format(time.RFC3339))
		})
	}
}

// TestSegmentBoundsDSTGapIsDeterministic pins the design's position on a
// boundary landing inside the skipped or repeated DST hour: whichever
// occurrence time.Date picks is fine, as long as it is stable. The sum
// invariant of AC 3.8 holds either way because adjacent segments share the
// boundary.
func TestSegmentBoundsDSTGapIsDeterministic(t *testing.T) {
	t.Parallel()
	for _, date := range []string{"2026-10-04", "2026-04-05"} {
		t.Run(date, func(t *testing.T) {
			t.Parallel()
			day, err := time.ParseInLocation(dateLayout, date, sydney)
			require.NoError(t, err)
			seg := Segment{Start: "02:30", End: "04:00"}
			from1, to1 := SegmentBounds(seg, day, sydney)
			from2, to2 := SegmentBounds(seg, day, sydney)
			assert.Equal(t, from1, from2)
			assert.Equal(t, to1, to2)
			assert.Less(t, from1, to1)
		})
	}
}

// TestSegmentBoundsTilesTheDay is the invariant the per-segment integrals
// depend on: consecutive segment bounds abut exactly and the first/last reach
// local midnight, on ordinary and DST-length days alike.
func TestSegmentBoundsTilesTheDay(t *testing.T) {
	t.Parallel()
	for _, date := range []string{"2026-04-12", "2026-04-05", "2026-10-04"} {
		t.Run(date, func(t *testing.T) {
			t.Parallel()
			day, err := time.ParseInLocation(dateLayout, date, sydney)
			require.NoError(t, err)
			segs := Segments(newPlan())

			var prevEnd int64
			for i, seg := range segs {
				from, to := SegmentBounds(seg, day, sydney)
				if i == 0 {
					assert.Equal(t, day.Unix(), from, "first segment starts at local midnight")
				} else {
					assert.Equal(t, prevEnd, from, "segment %d abuts its predecessor", i)
				}
				prevEnd = to
			}
			nextMidnight := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, sydney)
			assert.Equal(t, nextMidnight.Unix(), prevEnd, "last segment ends at the next local midnight")
		})
	}
}
