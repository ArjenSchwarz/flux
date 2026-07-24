package derivedstats

import "time"

// ParseOffpeakWindow parses "HH:MM" strings into minute-of-day values.
// Returns (start, end, true) on success, or (0, 0, false) if parsing fails
// or start >= end. Exported so the poller can pre-gate the summarisation
// pass per requirement 1.6.
//
// "24:00" is accepted as end-of-day (1440). A plan's free band may legitimately
// run to midnight, and this window is handed straight over from
// plan.Segments — rejecting it here would silently drop Blocks and PeakPeriods
// into whole-day-rated mode on a day that does have a free band. isOffpeak
// compares a 0–1439 minute-of-day with `< end`, so 1440 needs no special case
// downstream.
func ParseOffpeakWindow(startStr, endStr string) (int, int, bool) {
	parse := func(s string) (int, bool) {
		if len(s) != 5 || s[2] != ':' {
			return 0, false
		}
		h := int(s[0]-'0')*10 + int(s[1]-'0')
		m := int(s[3]-'0')*10 + int(s[4]-'0')
		if h > 24 || m > 59 || (h == 24 && m != 0) {
			return 0, false
		}
		return h*60 + m, true
	}
	startMin, ok1 := parse(startStr)
	endMin, ok2 := parse(endStr)
	if !ok1 || !ok2 || startMin >= endMin {
		return 0, 0, false
	}
	return startMin, endMin, true
}

// isOffpeak checks whether a Unix timestamp falls within the off-peak window
// (>= start AND < end) in Sydney local time.
func isOffpeak(ts int64, offpeakStartMin, offpeakEndMin int) bool {
	t := time.Unix(ts, 0).In(sydneyTZ)
	minuteOfDay := t.Hour()*60 + t.Minute()
	return minuteOfDay >= offpeakStartMin && minuteOfDay < offpeakEndMin
}
