package api

import (
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file pins the API's live window path to wall-clock time across Sydney's
// two DST transitions.
//
// It exists because the original implementation resolved the window as
// "midnight plus N minutes", which is an hour off on those two days, while
// liveBandImports in the same response used plan.SegmentBounds and was correct.
// Nothing caught it: the DST coverage elsewhere asserted only the UTC offset,
// which is right either way. These tests assert the wall-clock hour instead.

// sydneyDSTDays are the two days a year on which elapsed-minute arithmetic and
// wall-clock resolution disagree.
var sydneyDSTDays = map[string]string{
	"spring forward (23h day, 02:00 -> 03:00)": "2026-10-04",
	"fall back (25h day, 03:00 -> 02:00)":      "2026-04-05",
}

func TestOffpeakWindowBounds_FollowsWallClockAcrossDST(t *testing.T) {
	// AC 3.8: band membership follows local wall-clock time. A window declared
	// as 11:00-14:00 must start at 11:00 and end at 14:00 on every day of the
	// year, including the two whose midnight-to-window elapsed time is not
	// 11 hours.
	for name, date := range sydneyDSTDays {
		t.Run(name, func(t *testing.T) {
			day, err := time.ParseInLocation("2006-01-02", date, sydneyTZ)
			require.NoError(t, err)

			start, end := win("11:00", "14:00").bounds(day)

			assert.Equal(t, 11, start.In(sydneyTZ).Hour(), "window start wall-clock hour")
			assert.Equal(t, 0, start.In(sydneyTZ).Minute())
			assert.Equal(t, 14, end.In(sydneyTZ).Hour(), "window end wall-clock hour")
			assert.Equal(t, 0, end.In(sydneyTZ).Minute())
			assert.Equal(t, date, start.In(sydneyTZ).Format("2006-01-02"))
		})
	}
}

func TestOffpeakWindowBounds_AgreeWithRatedSegmentBounds(t *testing.T) {
	// Data Consistency: /day and /history compute today's peak import from
	// offpeakWindow.bounds and today's band split from plan.SegmentBounds, and
	// serve both in one payload. If the two resolve the same boundary to
	// different instants, the peak figure stops being the sum of the rated
	// bands. The free window is exactly the gap between the rated segments, so
	// the boundaries must coincide instant for instant.
	p := plan.Plan{
		ID:          "p",
		StartDate:   "2026-01-01",
		DefaultRate: 0.30,
		Windows:     []plan.Window{{Start: "11:00", End: "14:00", Free: true}},
	}
	rated := plan.RatedSegments(p)
	require.Len(t, rated, 2, "a single midday free band leaves a rated segment either side")

	for name, date := range sydneyDSTDays {
		t.Run(name, func(t *testing.T) {
			day, err := time.ParseInLocation("2006-01-02", date, sydneyTZ)
			require.NoError(t, err)

			windowStart, windowEnd := win("11:00", "14:00").bounds(day)
			_, morningEnd := plan.SegmentBounds(rated[0], day, sydneyTZ)
			eveningStart, _ := plan.SegmentBounds(rated[1], day, sydneyTZ)

			assert.Equal(t, morningEnd, windowStart.Unix(),
				"free-window start must be the instant the morning rated segment ends")
			assert.Equal(t, eveningStart, windowEnd.Unix(),
				"free-window end must be the instant the evening rated segment starts")
		})
	}
}
