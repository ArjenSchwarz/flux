package derivedstats

import "time"

// sydneyTZ is the Australia/Sydney timezone used for all date-based
// operations. Loaded once at package init to avoid repeated lookups and
// silent error discarding. The Go runtime embeds time/tzdata since 1.21
// and both binaries are built with CGO_DISABLED, so the panic is a
// documented assertion rather than a realistic runtime path.
var sydneyTZ = func() *time.Location {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		panic("failed to load Australia/Sydney timezone: " + err.Error())
	}
	return loc
}()

// melbourneSunriseSunset returns the UTC instant of Melbourne sunrise (when
// isSunrise=true) or sunset (false) for the given calendar date in
// "YYYY-MM-DD" format. The result is truncated to whole seconds and is
// always in UTC.
//
// The implementation looks up the date's MM-DD in melbourneSunLocal (an
// embedded static table; see melbourne_sun_table.go). The table value is a
// wall-clock "HH:MM" string in Sydney-local time. Combining it with the
// requested calendar date via time.ParseInLocation in sydneyTZ yields the
// correct UTC instant for any year — Go's IANA database resolves AEDT vs
// AEST automatically.
//
// Feb 29 is intentionally absent from the table; the lookup falls back to
// Feb 28's values (well within the ±2 minute tolerance of req 1.12).
func melbourneSunriseSunset(date string, isSunrise bool) time.Time {
	dayStart, err := time.ParseInLocation("2006-01-02", date, sydneyTZ)
	if err != nil {
		// Defensive fallback. Caller validates date format before getting
		// here; if we somehow get a malformed date, returning the zero
		// time lets the buildDailyUsageBlock degenerate-omit guard drop
		// the block.
		return time.Time{}
	}
	key := date[5:10] // MM-DD; ParseInLocation guarantees len(date) == 10
	entry, ok := melbourneSunLocal[key]
	if !ok {
		// Feb 29 is the only intentional miss; reuse Feb 28's values.
		entry = melbourneSunLocal["02-28"]
	}
	hhmm := entry.setLocal
	if isSunrise {
		hhmm = entry.riseLocal
	}
	if len(hhmm) != 5 || hhmm[2] != ':' {
		// Corrupt static table entry: return the zero time so the
		// buildDailyUsageBlock degenerate-omit guard drops the block,
		// matching the parse-error path above.
		return time.Time{}
	}
	h := int(hhmm[0]-'0')*10 + int(hhmm[1]-'0')
	m := int(hhmm[3]-'0')*10 + int(hhmm[4]-'0')
	return dayStart.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute).UTC().Truncate(time.Second)
}
