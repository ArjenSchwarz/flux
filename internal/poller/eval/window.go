// Package eval implements the per-cycle SoC threshold evaluator that runs
// inside the poller. Helpers here are independent of the rule store so they
// can be exercised by property-based tests in isolation.
package eval

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseHHMM converts "HH:MM" into a minute-of-day in [0, 1440). Returns an
// error for any string that doesn't parse cleanly; the Lambda validates the
// input on POST, so failures here indicate a corrupt row rather than user
// input.
func parseHHMM(s string) (int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("HH:MM: missing colon in %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("HH:MM: hour out of range in %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("HH:MM: minute out of range in %q", s)
	}
	return h*60 + m, nil
}

// inWindow reports whether the wall-clock instant `at` (interpreted in the
// supplied location) falls inside the rule's [start, end) window, and the
// windowStartDate (YYYY-MM-DD in the device TZ) that opens the active window
// at that instant.
//
// Windows are interpreted in the device's local time zone. A start strictly
// greater than the end crosses midnight: [start, 24:00) on day D ∪
// [00:00, end) on day D+1. End-of-day is expressed as "00:00" (Decision 7),
// which is the cross-midnight model where end is taken on the next day.
//
// When `at` is outside the window the returned date is the empty string and
// the caller MUST ignore it; it is only meaningful inside the window.
func inWindow(at time.Time, startStr, endStr string, loc *time.Location) (bool, string) {
	startMin, err := parseHHMM(startStr)
	if err != nil {
		return false, ""
	}
	endMin, err := parseHHMM(endStr)
	if err != nil {
		return false, ""
	}
	if startMin == endMin {
		return false, ""
	}
	local := at.In(loc)
	nowMin := local.Hour()*60 + local.Minute()
	dateToday := localDate(local)

	if startMin < endMin {
		// Non-cross-midnight: window lives entirely within one local day.
		if nowMin >= startMin && nowMin < endMin {
			return true, dateToday
		}
		return false, ""
	}

	// Cross-midnight: [startMin, 1440) on day D, [0, endMin) on day D+1.
	if nowMin >= startMin {
		return true, dateToday
	}
	if nowMin < endMin {
		// We're in the [00:00, end) tail. The opening day is yesterday.
		yesterday := local.AddDate(0, 0, -1)
		return true, localDate(yesterday)
	}
	return false, ""
}

// localDate returns YYYY-MM-DD for the calendar date of `at` in its own
// location.
func localDate(at time.Time) string {
	y, m, d := at.Date()
	return fmt.Sprintf("%04d-%02d-%02d", y, int(m), d)
}
