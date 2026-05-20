package eval

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// testZones is the set of time zones the property tests exercise. We
// deliberately include one that observes southern-hemisphere DST
// (Australia/Sydney), one with no DST (UTC), and one with northern-hemisphere
// DST (America/New_York) so the spring-forward gap test covers the relevant
// branches of time.Date's "is this wall time valid?" handling.
var testZones = []string{"Australia/Sydney", "UTC", "America/New_York"}

// preloadedTZ keeps the parsed locations so rapid.Check inner closures don't
// repeatedly hit time.LoadLocation and don't need require to surface errors.
var preloadedTZ = func() map[string]*time.Location {
	m := make(map[string]*time.Location, len(testZones))
	for _, name := range testZones {
		loc, err := time.LoadLocation(name)
		if err != nil {
			panic("test setup: time.LoadLocation(" + name + "): " + err.Error())
		}
		m[name] = loc
	}
	return m
}()

func mustLoadLocation(t testing.TB, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}

// genHHMM generates a random HH:MM string in 00:00..23:59.
func genHHMM(t *rapid.T, label string) (string, int) {
	hh := rapid.IntRange(0, 23).Draw(t, label+"H")
	mm := rapid.IntRange(0, 59).Draw(t, label+"M")
	return fmt.Sprintf("%02d:%02d", hh, mm), hh*60 + mm
}

// genDistinctWindow generates a (start, end) pair where start != end.
func genDistinctWindow(t *rapid.T) (string, string, int, int) {
	startStr, start := genHHMM(t, "start")
	for {
		endStr, end := genHHMM(t, "end")
		if start != end {
			return startStr, endStr, start, end
		}
	}
}

// TestInWindow_NonCrossMidnightMembership verifies a window that doesn't
// cross midnight covers exactly the [start, end) minute range.
func TestInWindow_NonCrossMidnightMembership(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		startStr, endStr, start, end := genDistinctWindow(rt)
		if start >= end {
			// Force non-cross-midnight by swapping.
			startStr, endStr = endStr, startStr
			start, end = end, start
		}
		tzName := testZones[rapid.IntRange(0, len(testZones)-1).Draw(rt, "tz")]
		loc := preloadedTZ[tzName]

		// Pick a calendar date well clear of DST transitions in any of the
		// listed zones (any random date inside 2026-04 — Sydney's DST ends in
		// early April but mid-month days are well past it).
		day := time.Date(2026, 4, 20, 0, 0, 0, 0, loc)
		minute := rapid.IntRange(0, 1439).Draw(rt, "minute")
		at := day.Add(time.Duration(minute) * time.Minute)
		got, _ := inWindow(at, startStr, endStr, loc)
		want := minute >= start && minute < end
		if got != want {
			rt.Fatalf("minute=%d start=%d end=%d at=%s want=%v got=%v",
				minute, start, end, at, want, got)
		}
	})
}

// TestInWindow_CrossMidnightMembership verifies the cross-midnight model:
// for start > end, the window covers [start, 24:00) ∪ [00:00, end).
func TestInWindow_CrossMidnightMembership(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		startStr, endStr, start, end := genDistinctWindow(rt)
		if start <= end {
			startStr, endStr = endStr, startStr
			start, end = end, start
		}
		tzName := testZones[rapid.IntRange(0, len(testZones)-1).Draw(rt, "tz")]
		loc := preloadedTZ[tzName]

		day := time.Date(2026, 4, 20, 0, 0, 0, 0, loc)
		minute := rapid.IntRange(0, 1439).Draw(rt, "minute")
		at := day.Add(time.Duration(minute) * time.Minute)
		got, _ := inWindow(at, startStr, endStr, loc)
		want := minute >= start || minute < end
		if got != want {
			rt.Fatalf("minute=%d start=%d end=%d at=%s want=%v got=%v",
				minute, start, end, at, want, got)
		}
	})
}

// TestInWindow_TwentyFourHourPeriodicity verifies that membership at time t
// equals membership at t + 24h (in stable (non-DST-transition) regions).
func TestInWindow_TwentyFourHourPeriodicity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		startStr, endStr, _, _ := genDistinctWindow(rt)
		loc := preloadedTZ["UTC"]
		base := time.Date(2026, 6, 15, 12, 0, 0, 0, loc)
		minute := rapid.IntRange(0, 1439).Draw(rt, "minute")
		at1 := base.Add(time.Duration(minute) * time.Minute)
		at2 := at1.Add(24 * time.Hour)
		got1, _ := inWindow(at1, startStr, endStr, loc)
		got2, _ := inWindow(at2, startStr, endStr, loc)
		if got1 != got2 {
			rt.Fatalf("24-hour periodicity broken: at1=%s (in=%v) at2=%s (in=%v)",
				at1, got1, at2, got2)
		}
	})
}

// TestInWindow_DSTSpringForward_SydneyGapFalse: Sydney's DST starts at
// 02:00 local on the first Sunday of October — at 02:00 the clock jumps
// straight to 03:00. The minute 02:00–02:59 local does not exist on that
// day; a window fully inside the skipped hour must return false.
func TestInWindow_DSTSpringForward_SydneyGapFalse(t *testing.T) {
	loc := mustLoadLocation(t, "Australia/Sydney")
	// 2026-10-04 is the first Sunday of October 2026.
	// 02:00–03:00 Sydney does not exist.
	// time.Date(_, _, _, 2, 30, _, _, loc) is normalised by Go into 03:30 (the
	// nearest valid wall time); we verify that a window 02:00–03:00 reports
	// "outside" for any candidate clock-reading at that nominal time.
	at := time.Date(2026, 10, 4, 1, 30, 0, 0, loc) // before the gap, in window 01:00-02:00
	got, _ := inWindow(at, "01:00", "02:00", loc)
	assert.True(t, got, "pre-gap minute should be inside its window")

	// 02:30 nominally is in the gap; Go normalises forward to 03:30.
	// A window 02:00–03:00 must not contain it.
	atGap := time.Date(2026, 10, 4, 2, 30, 0, 0, loc)
	gotGap, _ := inWindow(atGap, "02:00", "03:00", loc)
	assert.False(t, gotGap, "skipped-hour time must not be in a 02:00-03:00 window")
}

// TestWindowStartDate_IncrementsAtOpeningMinute: for any window,
// windowStartDate(open, ...) > windowStartDate(open-1m, ...) when the
// surrounding minute is in the prior day.
func TestWindowStartDate_IncrementsAtOpeningMinute(t *testing.T) {
	loc := mustLoadLocation(t, "Australia/Sydney")
	// Window 17:00-00:00 (cross-midnight), opens at 17:00 each day.
	day := time.Date(2026, 6, 15, 17, 0, 0, 0, loc)
	atOpen := day
	atBeforeOpen := day.Add(-1 * time.Minute) // 16:59 — not in window
	_, openDate := inWindow(atOpen, "17:00", "00:00", loc)
	inAt, _ := inWindow(atBeforeOpen, "17:00", "00:00", loc)
	assert.False(t, inAt, "16:59 must not be in 17:00-00:00")
	assert.Equal(t, "2026-06-15", openDate)
}

// TestWindowStartDate_PersistsAcrossLocalMidnight: a cross-midnight window
// (22:00–06:00) opens at 22:00 on day D. At 02:00 on day D+1, the
// windowStartDate must still report D.
func TestWindowStartDate_PersistsAcrossLocalMidnight(t *testing.T) {
	loc := mustLoadLocation(t, "Australia/Sydney")
	openDay := time.Date(2026, 6, 15, 22, 30, 0, 0, loc) // 22:30 day D
	pastMidnight := time.Date(2026, 6, 16, 2, 0, 0, 0, loc)
	_, dateAtOpen := inWindow(openDay, "22:00", "06:00", loc)
	_, dateAfterMidnight := inWindow(pastMidnight, "22:00", "06:00", loc)
	assert.Equal(t, "2026-06-15", dateAtOpen)
	assert.Equal(t, "2026-06-15", dateAfterMidnight,
		"cross-midnight window date must reflect the opening day, not the post-midnight calendar day")
}

// TestWindowStartDate_EndOfDayMidnight: window 17:00–00:00 ends at 00:00
// of the day after the open. At 23:59 on day D the window is still active
// (date D); at 00:00 on day D+1 the window has closed.
func TestWindowStartDate_EndOfDayMidnight(t *testing.T) {
	loc := mustLoadLocation(t, "Australia/Sydney")
	day := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)

	inLate, dateLate := inWindow(day.Add(23*time.Hour+59*time.Minute), "17:00", "00:00", loc)
	assert.True(t, inLate, "23:59 must still be in a 17:00-00:00 window")
	assert.Equal(t, "2026-06-15", dateLate)

	inAfter, _ := inWindow(day.Add(24*time.Hour), "17:00", "00:00", loc)
	assert.False(t, inAfter, "00:00 next day must be outside the closing 17:00-00:00 window")
}
