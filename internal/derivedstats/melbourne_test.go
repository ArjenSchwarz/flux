package derivedstats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMelbourneSunriseSunset(t *testing.T) {
	tests := map[string]struct {
		date      string
		isSunrise bool
		// Min/max UTC instants for the result (inclusive). The table is in
		// HH:MM precision, so we assert plausible ranges rather than exact
		// equality.
		check func(t *testing.T, got time.Time)
	}{
		"winter solstice sunset between 16:30 and 17:30 AEST": {
			date:      "2026-06-21",
			isSunrise: false,
			check: func(t *testing.T, got time.Time) {
				local := got.In(sydneyTZ)
				assert.Equal(t, 2026, local.Year())
				assert.Equal(t, time.June, local.Month())
				assert.Equal(t, 21, local.Day())
				mins := local.Hour()*60 + local.Minute()
				assert.GreaterOrEqual(t, mins, 16*60+30, "expected sunset >= 16:30 AEST")
				assert.LessOrEqual(t, mins, 17*60+30, "expected sunset <= 17:30 AEST")
			},
		},
		"summer solstice sunset between 20:00 and 21:00 AEDT": {
			date:      "2026-12-22",
			isSunrise: false,
			check: func(t *testing.T, got time.Time) {
				local := got.In(sydneyTZ)
				assert.Equal(t, 2026, local.Year())
				assert.Equal(t, time.December, local.Month())
				assert.Equal(t, 22, local.Day())
				mins := local.Hour()*60 + local.Minute()
				assert.GreaterOrEqual(t, mins, 20*60, "expected sunset >= 20:00 AEDT")
				assert.LessOrEqual(t, mins, 21*60, "expected sunset <= 21:00 AEDT")
			},
		},
		"leap year Feb 29 falls back to Feb 28 values": {
			date:      "2028-02-29",
			isSunrise: false,
			check: func(t *testing.T, got time.Time) {
				feb28 := melbourneSunriseSunset("2028-02-28", false)
				want := feb28.In(sydneyTZ)
				gotLocal := got.In(sydneyTZ)
				assert.Equal(t, time.February, gotLocal.Month())
				assert.Equal(t, 29, gotLocal.Day())
				assert.Equal(t, want.Hour(), gotLocal.Hour(), "Feb 29 should reuse Feb 28's HH")
				assert.Equal(t, want.Minute(), gotLocal.Minute(), "Feb 29 should reuse Feb 28's MM")
			},
		},
		"AEDT-end transition day resolves to a UTC instant on the right local date": {
			date:      "2026-04-05",
			isSunrise: true,
			check: func(t *testing.T, got time.Time) {
				local := got.In(sydneyTZ)
				assert.Equal(t, 2026, local.Year())
				assert.Equal(t, time.April, local.Month())
				assert.Equal(t, 5, local.Day())
				mins := local.Hour()*60 + local.Minute()
				assert.GreaterOrEqual(t, mins, 5*60, "expected sunrise after 05:00")
				assert.LessOrEqual(t, mins, 9*60, "expected sunrise before 09:00")
			},
		},
		"return value is in UTC": {
			date:      "2026-06-21",
			isSunrise: true,
			check: func(t *testing.T, got time.Time) {
				assert.Equal(t, time.UTC, got.Location())
			},
		},
		"truncated to whole seconds": {
			date:      "2026-12-22",
			isSunrise: true,
			check: func(t *testing.T, got time.Time) {
				assert.Zero(t, got.Nanosecond())
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := melbourneSunriseSunset(tc.date, tc.isSunrise)
			tc.check(t, got)
		})
	}
}
