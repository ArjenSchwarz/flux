package derivedstats

import "time"

// ParseOffpeakWindow parses "HH:MM" strings into minute-of-day values.
// Returns (start, end, true) on success, or (0, 0, false) if parsing fails
// or start >= end. Exported so the poller can pre-gate the summarisation
// pass per requirement 1.6.
func ParseOffpeakWindow(startStr, endStr string) (int, int, bool) {
	parse := func(s string) (int, bool) {
		if len(s) != 5 || s[2] != ':' {
			return 0, false
		}
		h := int(s[0]-'0')*10 + int(s[1]-'0')
		m := int(s[3]-'0')*10 + int(s[4]-'0')
		if h > 23 || m > 59 {
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
