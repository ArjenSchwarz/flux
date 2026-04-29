package derivedstats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readingPpv builds a Reading at the given Sydney local time with the
// specified Ppv and Pload values for daily-usage tests.
func readingPpv(date string, hour, minute, second int, ppv, pload float64) Reading {
	d, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)
	t := time.Date(d.Year(), d.Month(), d.Day(), hour, minute, second, 0, sydneyTZ)
	return Reading{Timestamp: t.Unix(), Ppv: ppv, Pload: pload}
}

// dailyUsageBlocksByKind indexes the emitted blocks of a DailyUsage result
// by Kind so assertions can pick out the block they care about.
func dailyUsageBlocksByKind(du *DailyUsage) map[string]DailyUsageBlock {
	if du == nil {
		return nil
	}
	out := make(map[string]DailyUsageBlock, len(du.Blocks))
	for _, b := range du.Blocks {
		out[b.Kind] = b
	}
	return out
}

func TestBlocks(t *testing.T) {
	const offpeakStart = "11:00"
	const offpeakEnd = "14:00"

	const pastDate = "2026-04-12"
	const todayDate = "2026-04-15"

	pastDayReadings := func() []Reading {
		var out []Reading
		for h := range 7 {
			for m := 0; m < 60; m += 5 {
				if h == 6 && m >= 30 {
					break
				}
				out = append(out, readingPpv(pastDate, h, m, 0, 0, 1000))
			}
		}
		for h := 6; h < 18; h++ {
			startMin := 0
			if h == 6 {
				startMin = 30
			}
			for m := startMin; m < 60; m += 5 {
				out = append(out, readingPpv(pastDate, h, m, 0, 1000, 1000))
			}
		}
		for h := 18; h < 24; h++ {
			for m := 0; m < 60; m += 5 {
				out = append(out, readingPpv(pastDate, h, m, 0, 0, 1000))
			}
		}
		return out
	}()

	todayReadingsUpTo := func(stopHour, stopMinute int) []Reading {
		var out []Reading
		for h := 0; h <= stopHour; h++ {
			for m := 0; m < 60; m += 5 {
				if h == stopHour && m > stopMinute {
					break
				}
				ppv := 0.0
				solarStart := h*60+m >= 6*60+30
				solarEnd := h*60+m < 18*60
				if solarStart && solarEnd {
					ppv = 1000
				}
				out = append(out, readingPpv(todayDate, h, m, 0, ppv, 1000))
			}
		}
		return out
	}

	overcastReadings := []Reading{
		readingPpv(pastDate, 0, 30, 0, 0, 800),
		readingPpv(pastDate, 6, 0, 0, 0, 800),
		readingPpv(pastDate, 12, 0, 0, 0, 800),
		readingPpv(pastDate, 14, 30, 0, 0, 800),
		readingPpv(pastDate, 18, 0, 0, 0, 800),
		readingPpv(pastDate, 23, 0, 0, 0, 800),
	}

	tests := map[string]struct {
		readings     []Reading
		offpeakStart string
		offpeakEnd   string
		date         string
		today        string
		now          time.Time
		check        func(t *testing.T, got *DailyUsage)
	}{
		"typical past day, all five blocks complete from readings": {
			readings:     pastDayReadings,
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         pastDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				blocks := dailyUsageBlocksByKind(got)
				require.Len(t, got.Blocks, 5)
				for _, kind := range []string{
					DailyUsageKindNight, DailyUsageKindMorningPeak,
					DailyUsageKindOffPeak, DailyUsageKindAfternoonPeak,
					DailyUsageKindEvening,
				} {
					b, ok := blocks[kind]
					require.True(t, ok, "missing block %s", kind)
					assert.Equal(t, DailyUsageStatusComplete, b.Status, "kind=%s", kind)
					assert.Equal(t, DailyUsageBoundaryReadings, b.BoundarySource, "kind=%s", kind)
				}
				assert.Equal(t, DailyUsageKindNight, got.Blocks[0].Kind)
				assert.Equal(t, DailyUsageKindMorningPeak, got.Blocks[1].Kind)
				assert.Equal(t, DailyUsageKindOffPeak, got.Blocks[2].Kind)
				assert.Equal(t, DailyUsageKindAfternoonPeak, got.Blocks[3].Kind)
				assert.Equal(t, DailyUsageKindEvening, got.Blocks[4].Kind)
			},
		},
		"today before sunrise: only night, in-progress, readings-derived end": {
			readings: []Reading{
				readingPpv(todayDate, 1, 0, 0, 0, 1000),
				readingPpv(todayDate, 2, 0, 0, 0, 1000),
				readingPpv(todayDate, 3, 0, 0, 0, 1000),
				readingPpv(todayDate, 4, 30, 0, 0, 1000),
			},
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         todayDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 4, 30, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 1)
				night := got.Blocks[0]
				assert.Equal(t, DailyUsageKindNight, night.Kind)
				assert.Equal(t, DailyUsageStatusInProgress, night.Status)
				assert.Equal(t, DailyUsageBoundaryReadings, night.BoundarySource)
				wantEnd := time.Date(2026, 4, 15, 4, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEnd, night.End)
			},
		},
		"today mid-morning-peak: night complete, morningPeak in-progress": {
			readings:     todayReadingsUpTo(9, 30),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         todayDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 9, 30, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				blocks := dailyUsageBlocksByKind(got)
				require.Len(t, got.Blocks, 2)
				night, ok := blocks[DailyUsageKindNight]
				require.True(t, ok)
				assert.Equal(t, DailyUsageStatusComplete, night.Status)
				assert.Equal(t, DailyUsageBoundaryReadings, night.BoundarySource)
				mp, ok := blocks[DailyUsageKindMorningPeak]
				require.True(t, ok)
				assert.Equal(t, DailyUsageStatusInProgress, mp.Status)
				assert.Equal(t, DailyUsageBoundaryReadings, mp.BoundarySource)
				wantEnd := time.Date(2026, 4, 15, 9, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEnd, mp.End)
			},
		},
		"today during off-peak: night/morningPeak complete, offPeak in-progress": {
			readings:     todayReadingsUpTo(12, 30),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         todayDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 30, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				blocks := dailyUsageBlocksByKind(got)
				require.Len(t, got.Blocks, 3)
				assert.Equal(t, DailyUsageStatusComplete, blocks[DailyUsageKindNight].Status)
				assert.Equal(t, DailyUsageStatusComplete, blocks[DailyUsageKindMorningPeak].Status)
				op := blocks[DailyUsageKindOffPeak]
				assert.Equal(t, DailyUsageStatusInProgress, op.Status)
				assert.Equal(t, DailyUsageBoundaryReadings, op.BoundarySource)
				wantEnd := time.Date(2026, 4, 15, 12, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEnd, op.End)
			},
		},
		"today mid-afternoon-peak with sun still up: today-gate fires": {
			readings: func() []Reading {
				return todayReadingsUpTo(15, 30)
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         todayDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 15, 30, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				blocks := dailyUsageBlocksByKind(got)
				require.Len(t, got.Blocks, 4, "evening should be omitted by today-gate")
				_, hasEvening := blocks[DailyUsageKindEvening]
				assert.False(t, hasEvening)
				ap := blocks[DailyUsageKindAfternoonPeak]
				assert.Equal(t, DailyUsageStatusInProgress, ap.Status)
				wantEnd := time.Date(2026, 4, 15, 15, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEnd, ap.End)
				assert.Equal(t, DailyUsageBoundaryReadings, ap.BoundarySource)
			},
		},
		"today late afternoon, cloudy, solar stopped 90 min ago: gate does NOT fire": {
			readings: func() []Reading {
				var out []Reading
				for h := 0; h <= 14; h++ {
					for m := 0; m < 60; m += 5 {
						if h == 14 && m > 30 {
							break
						}
						ppv := 0.0
						if h*60+m >= 6*60+30 && h*60+m <= 14*60+30 {
							ppv = 1000
						}
						out = append(out, readingPpv(todayDate, h, m, 0, ppv, 1000))
					}
				}
				for h := 14; h <= 16; h++ {
					startM := 35
					if h == 15 {
						startM = 0
					}
					if h == 16 {
						startM = 0
					}
					for m := startM; m < 60; m += 5 {
						if h == 16 && m > 0 {
							break
						}
						out = append(out, readingPpv(todayDate, h, m, 0, 0, 1000))
					}
				}
				return out
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         todayDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				blocks := dailyUsageBlocksByKind(got)
				require.Len(t, got.Blocks, 5)
				ap := blocks[DailyUsageKindAfternoonPeak]
				assert.Equal(t, DailyUsageStatusComplete, ap.Status)
				assert.Equal(t, DailyUsageBoundaryReadings, ap.BoundarySource)
				wantApEnd := time.Date(2026, 4, 15, 14, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantApEnd, ap.End)
				ev := blocks[DailyUsageKindEvening]
				assert.Equal(t, DailyUsageStatusInProgress, ev.Status)
				assert.Equal(t, DailyUsageBoundaryReadings, ev.BoundarySource)
				wantEvStart := time.Date(2026, 4, 15, 14, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEvStart, ev.Start)
			},
		},
		"today after sunset: all five emitted, evening in-progress": {
			readings: func() []Reading {
				var out []Reading
				for h := 0; h <= 22; h++ {
					for m := 0; m < 60; m += 5 {
						if h == 22 && m > 0 {
							break
						}
						ppv := 0.0
						mod := h*60 + m
						if mod >= 6*60+30 && mod < 18*60 {
							ppv = 1000
						}
						out = append(out, readingPpv(todayDate, h, m, 0, ppv, 1000))
					}
				}
				return out
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         todayDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 22, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 5)
				blocks := dailyUsageBlocksByKind(got)
				assert.Equal(t, DailyUsageStatusInProgress, blocks[DailyUsageKindEvening].Status)
				wantEnd := time.Date(2026, 4, 15, 22, 0, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEnd, blocks[DailyUsageKindEvening].End)
			},
		},
		"overcast complete day: all five emitted, sunrise/sunset edges estimated": {
			readings:     overcastReadings,
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         pastDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 5)
				blocks := dailyUsageBlocksByKind(got)
				assert.Equal(t, DailyUsageBoundaryEstimated, blocks[DailyUsageKindNight].BoundarySource)
				assert.Equal(t, DailyUsageBoundaryEstimated, blocks[DailyUsageKindMorningPeak].BoundarySource)
				assert.Equal(t, DailyUsageBoundaryReadings, blocks[DailyUsageKindOffPeak].BoundarySource)
				assert.Equal(t, DailyUsageBoundaryEstimated, blocks[DailyUsageKindAfternoonPeak].BoundarySource)
				assert.Equal(t, DailyUsageBoundaryEstimated, blocks[DailyUsageKindEvening].BoundarySource)
				for _, b := range got.Blocks {
					assert.Equal(t, DailyUsageStatusComplete, b.Status, "kind=%s", b.Kind)
				}
			},
		},
		"partial-data after-offpeak: solar-window invariant holds, five-block path": {
			readings: func() []Reading {
				var out []Reading
				for h := 0; h < 16; h++ {
					for m := 0; m < 60; m += 5 {
						if h == 15 && m > 30 {
							break
						}
						ppv := 0.0
						mod := h*60 + m
						if mod >= 6*60+30 && mod <= 15*60+25 {
							ppv = 1000
						}
						out = append(out, readingPpv(pastDate, h, m, 0, ppv, 1000))
					}
				}
				return out
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         pastDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 5)
				blocks := dailyUsageBlocksByKind(got)
				ap := blocks[DailyUsageKindAfternoonPeak]
				wantApEnd := time.Date(2026, 4, 12, 15, 25, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantApEnd, ap.End)
				ev := blocks[DailyUsageKindEvening]
				assert.Equal(t, DailyUsageBoundaryReadings, ev.BoundarySource)
				wantEvStart := time.Date(2026, 4, 12, 15, 25, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEvStart, ev.Start)
				assert.InDelta(t, 0, ev.TotalKwh, 1e-9)
			},
		},
		"partial-data during-offpeak: solar-window invariant fails, two-block path": {
			readings: func() []Reading {
				var out []Reading
				for h := 0; h < 13; h++ {
					for m := 0; m < 60; m += 5 {
						if h == 12 && m > 30 {
							break
						}
						ppv := 0.0
						mod := h*60 + m
						if mod >= 6*60+30 && mod <= 12*60+25 {
							ppv = 1000
						}
						out = append(out, readingPpv(pastDate, h, m, 0, ppv, 1000))
					}
				}
				return out
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         pastDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 2)
				blocks := dailyUsageBlocksByKind(got)
				_, hasNight := blocks[DailyUsageKindNight]
				_, hasEv := blocks[DailyUsageKindEvening]
				assert.True(t, hasNight)
				assert.True(t, hasEv)
				ev := blocks[DailyUsageKindEvening]
				assert.Equal(t, DailyUsageBoundaryReadings, ev.BoundarySource)
				wantEvStart := time.Date(2026, 4, 12, 12, 25, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEvStart, ev.Start)
				assert.InDelta(t, 0, ev.TotalKwh, 1e-9)
			},
		},
		"off-peak SSM misconfigured: only night and evening emitted": {
			readings:     pastDayReadings,
			offpeakStart: "",
			offpeakEnd:   "",
			date:         pastDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 2)
				blocks := dailyUsageBlocksByKind(got)
				_, hasNight := blocks[DailyUsageKindNight]
				_, hasEv := blocks[DailyUsageKindEvening]
				assert.True(t, hasNight)
				assert.True(t, hasEv)
			},
		},
		"off-peak start >= end: only night and evening emitted": {
			readings:     pastDayReadings,
			offpeakStart: "14:00",
			offpeakEnd:   "11:00",
			date:         pastDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 2)
			},
		},
		"single-solar reading firstSolar==lastSolar: invariant violated, two-block path": {
			readings: []Reading{
				readingPpv(pastDate, 0, 30, 0, 0, 1000),
				readingPpv(pastDate, 12, 0, 0, 1000, 1000),
				readingPpv(pastDate, 18, 0, 0, 0, 1000),
				readingPpv(pastDate, 23, 0, 0, 0, 1000),
			},
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         pastDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 2)
			},
		},
		"DST spring-forward day (2025-10-05, 23h day): five blocks": {
			readings: func() []Reading {
				const date = "2025-10-05"
				var out []Reading
				for h := range 24 {
					for m := 0; m < 60; m += 30 {
						ppv := 0.0
						mod := h*60 + m
						if mod >= 6*60+55 && mod <= 19*60 {
							ppv = 1000
						}
						out = append(out, readingPpv(date, h, m, 0, ppv, 1000))
					}
				}
				return out
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         "2025-10-05",
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 5)
				dayStart, _ := time.ParseInLocation("2006-01-02", "2025-10-05", sydneyTZ)
				dayEnd := dayStart.AddDate(0, 0, 1)
				assert.InDelta(t, float64(23*3600), float64(dayEnd.Unix()-dayStart.Unix()), 0.5,
					"DST spring-forward day should be 23h long")
			},
		},
		"DST fall-back day (2026-04-05, 25h day): five blocks": {
			readings: func() []Reading {
				const date = "2026-04-05"
				var out []Reading
				for h := range 24 {
					for m := 0; m < 60; m += 30 {
						ppv := 0.0
						mod := h*60 + m
						if mod >= 7*60+45 && mod <= 19*60 {
							ppv = 1000
						}
						out = append(out, readingPpv(date, h, m, 0, ppv, 1000))
					}
				}
				return out
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         "2026-04-05",
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 5)
				dayStart, _ := time.ParseInLocation("2006-01-02", "2026-04-05", sydneyTZ)
				dayEnd := dayStart.AddDate(0, 0, 1)
				assert.InDelta(t, float64(25*3600), float64(dayEnd.Unix()-dayStart.Unix()), 0.5,
					"DST fall-back day should be 25h long")
			},
		},
		"pre-sunrise 01:30 Ppv blip is filtered": {
			readings: func() []Reading {
				out := []Reading{readingPpv(pastDate, 1, 30, 0, 50, 1000)}
				for h := 7; h < 18; h++ {
					for m := 0; m < 60; m += 30 {
						out = append(out, readingPpv(pastDate, h, m, 0, 1000, 1000))
					}
				}
				return out
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         pastDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				blocks := dailyUsageBlocksByKind(got)
				night := blocks[DailyUsageKindNight]
				expected := time.Date(2026, 4, 12, 7, 0, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, expected, night.End, "night should not end at the 01:30 blip")
			},
		},
		"post-sunset 22:00 Ppv blip is filtered": {
			readings: func() []Reading {
				var out []Reading
				for h := range 18 {
					for m := 0; m < 60; m += 30 {
						ppv := 0.0
						mod := h*60 + m
						if mod >= 6*60+45 && mod <= 17*60+30 {
							ppv = 1000
						}
						out = append(out, readingPpv(pastDate, h, m, 0, ppv, 1000))
					}
				}
				out = append(out, readingPpv(pastDate, 22, 0, 0, 50, 1000))
				out = append(out, readingPpv(pastDate, 23, 30, 0, 0, 1000))
				return out
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         pastDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				blocks := dailyUsageBlocksByKind(got)
				ap := blocks[DailyUsageKindAfternoonPeak]
				wantApEnd := time.Date(2026, 4, 12, 17, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantApEnd, ap.End, "afternoonPeak must not absorb post-sunset hours")
			},
		},
		"future-dated request, no readings: five complete zero-energy blocks": {
			readings:     nil,
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         "2099-01-01",
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				require.Len(t, got.Blocks, 5)
				wantKinds := []string{
					DailyUsageKindNight,
					DailyUsageKindMorningPeak,
					DailyUsageKindOffPeak,
					DailyUsageKindAfternoonPeak,
					DailyUsageKindEvening,
				}
				for i, b := range got.Blocks {
					assert.Equal(t, wantKinds[i], b.Kind, "block %d kind", i)
					assert.Equal(t, DailyUsageStatusComplete, b.Status, "block %d status", i)
					assert.InDelta(t, 0.0, b.TotalKwh, 0.0001, "block %d totalKwh", i)
				}
			},
		},
		"today overcast mid-morning, no qualifying Ppv yet: morningPeak in-progress estimated": {
			readings: func() []Reading {
				var out []Reading
				for h := 0; h < 9; h++ {
					for m := 0; m < 60; m += 30 {
						out = append(out, readingPpv(todayDate, h, m, 0, 0, 1000))
					}
				}
				out = append(out, readingPpv(todayDate, 9, 0, 0, 0, 1000))
				out = append(out, readingPpv(todayDate, 9, 30, 0, 0, 1000))
				return out
			}(),
			offpeakStart: offpeakStart,
			offpeakEnd:   offpeakEnd,
			date:         todayDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 9, 30, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				blocks := dailyUsageBlocksByKind(got)
				require.Len(t, got.Blocks, 2, "expected night + morningPeak only")
				night, ok := blocks[DailyUsageKindNight]
				require.True(t, ok)
				assert.Equal(t, DailyUsageStatusComplete, night.Status)
				assert.Equal(t, DailyUsageBoundaryEstimated, night.BoundarySource)
				mp, ok := blocks[DailyUsageKindMorningPeak]
				require.True(t, ok)
				assert.Equal(t, DailyUsageStatusInProgress, mp.Status)
				assert.Equal(t, DailyUsageBoundaryEstimated, mp.BoundarySource)
				wantEnd := time.Date(2026, 4, 15, 9, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEnd, mp.End)
			},
		},
		"today + off-peak misconfigured: today-gate still applies on 2-block path": {
			readings: func() []Reading {
				var out []Reading
				for h := range 14 {
					for m := 0; m < 60; m += 30 {
						ppv := 0.0
						mod := h*60 + m
						if mod >= 6*60+45 && mod < 14*60 {
							ppv = 1000
						}
						out = append(out, readingPpv(todayDate, h, m, 0, ppv, 1000))
					}
				}
				out = append(out, readingPpv(todayDate, 14, 0, 0, 1000, 1000))
				out = append(out, readingPpv(todayDate, 14, 30, 0, 0, 1000))
				out = append(out, readingPpv(todayDate, 15, 0, 0, 0, 1000))
				out = append(out, readingPpv(todayDate, 15, 30, 0, 0, 1000))
				return out
			}(),
			offpeakStart: "",
			offpeakEnd:   "",
			date:         todayDate,
			today:        todayDate,
			now:          time.Date(2026, 4, 15, 15, 30, 0, 0, sydneyTZ),
			check: func(t *testing.T, got *DailyUsage) {
				require.NotNil(t, got)
				blocks := dailyUsageBlocksByKind(got)
				require.Len(t, got.Blocks, 2)
				ev, ok := blocks[DailyUsageKindEvening]
				require.True(t, ok)
				assert.Equal(t, DailyUsageStatusInProgress, ev.Status)
				wantEvEnd := time.Date(2026, 4, 15, 15, 30, 0, 0, sydneyTZ).UTC().Format(time.RFC3339)
				assert.Equal(t, wantEvEnd, ev.End)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := Blocks(tc.readings, tc.offpeakStart, tc.offpeakEnd, tc.date, tc.today, tc.now)
			tc.check(t, got)
		})
	}
}

func TestBlocks_PercentOfDay(t *testing.T) {
	const offpeakStart = "11:00"
	const offpeakEnd = "14:00"
	const pastDate = "2026-04-12"
	const todayDate = "2026-04-15"
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, sydneyTZ)

	t.Run("typical day percentages sum within 100±3", func(t *testing.T) {
		var readings []Reading
		dayStart, _ := time.ParseInLocation("2006-01-02", pastDate, sydneyTZ)
		for s := int64(0); s < 24*3600; s += 30 {
			ts := dayStart.Unix() + s
			tt := time.Unix(ts, 0).In(sydneyTZ)
			h := tt.Hour()
			pload := 500.0
			if h >= 11 && h < 14 {
				pload = 2500
			}
			if h >= 18 {
				pload = 1500
			}
			ppv := 0.0
			mod := h*60 + tt.Minute()
			if mod >= 6*60+45 && mod <= 17*60+30 {
				ppv = 1500
			}
			readings = append(readings, Reading{Timestamp: ts, Ppv: ppv, Pload: pload})
		}
		got := Blocks(readings, offpeakStart, offpeakEnd, pastDate, todayDate, now)
		require.NotNil(t, got)
		require.Len(t, got.Blocks, 5)
		sum := 0
		for _, b := range got.Blocks {
			sum += b.PercentOfDay
		}
		assert.True(t, sum >= 97 && sum <= 103, "percentOfDay sum must be 100±3, got %d", sum)
	})

	t.Run("zero-load day: every emitted block has percentOfDay = 0", func(t *testing.T) {
		var readings []Reading
		dayStart, _ := time.ParseInLocation("2006-01-02", pastDate, sydneyTZ)
		for s := int64(0); s < 24*3600; s += 30 {
			ts := dayStart.Unix() + s
			tt := time.Unix(ts, 0).In(sydneyTZ)
			h := tt.Hour()
			mod := h*60 + tt.Minute()
			ppv := 0.0
			if mod >= 6*60+45 && mod <= 17*60+30 {
				ppv = 1000
			}
			readings = append(readings, Reading{Timestamp: ts, Ppv: ppv, Pload: 0})
		}
		got := Blocks(readings, offpeakStart, offpeakEnd, pastDate, todayDate, now)
		require.NotNil(t, got)
		for _, b := range got.Blocks {
			assert.Equal(t, 0, b.PercentOfDay, "kind=%s", b.Kind)
		}
	})
}

func TestBuildDailyUsageBlock(t *testing.T) {
	const date = "2026-04-12"
	dayStart, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)

	tests := map[string]struct {
		p                pendingBlock
		unroundedSum     float64
		wantBoundary     string
		wantAverage      *float64
		wantPercentOfDay int
	}{
		"both readings": {
			p: pendingBlock{
				kind:         DailyUsageKindOffPeak,
				start:        dayStart.Add(11 * time.Hour),
				end:          dayStart.Add(14 * time.Hour),
				status:       DailyUsageStatusComplete,
				unroundedKwh: 6.0,
			},
			unroundedSum:     30.0,
			wantBoundary:     DailyUsageBoundaryReadings,
			wantAverage:      floatPtrTest(2.0),
			wantPercentOfDay: 20,
		},
		"start estimated only": {
			p: pendingBlock{
				kind:           DailyUsageKindMorningPeak,
				start:          dayStart.Add(6 * time.Hour),
				end:            dayStart.Add(11 * time.Hour),
				startEstimated: true,
				status:         DailyUsageStatusComplete,
				unroundedKwh:   2.5,
			},
			unroundedSum:     10.0,
			wantBoundary:     DailyUsageBoundaryEstimated,
			wantAverage:      floatPtrTest(0.5),
			wantPercentOfDay: 25,
		},
		"end estimated only": {
			p: pendingBlock{
				kind:         DailyUsageKindNight,
				start:        dayStart,
				end:          dayStart.Add(6 * time.Hour),
				endEstimated: true,
				status:       DailyUsageStatusComplete,
				unroundedKwh: 1.5,
			},
			unroundedSum:     10.0,
			wantBoundary:     DailyUsageBoundaryEstimated,
			wantAverage:      floatPtrTest(0.25),
			wantPercentOfDay: 15,
		},
		"both estimated": {
			p: pendingBlock{
				kind:           DailyUsageKindEvening,
				start:          dayStart.Add(18 * time.Hour),
				end:            dayStart.Add(24 * time.Hour),
				startEstimated: true,
				endEstimated:   true,
				status:         DailyUsageStatusComplete,
				unroundedKwh:   3.0,
			},
			unroundedSum:     10.0,
			wantBoundary:     DailyUsageBoundaryEstimated,
			wantAverage:      floatPtrTest(0.5),
			wantPercentOfDay: 30,
		},
		"elapsed below 60s drops average": {
			p: pendingBlock{
				kind:         DailyUsageKindEvening,
				start:        dayStart.Add(23*time.Hour + 59*time.Minute + 30*time.Second),
				end:          dayStart.Add(24 * time.Hour),
				status:       DailyUsageStatusInProgress,
				unroundedKwh: 0.001,
			},
			unroundedSum:     1.0,
			wantBoundary:     DailyUsageBoundaryReadings,
			wantAverage:      nil,
			wantPercentOfDay: 0,
		},
		"zero unroundedSum yields percentOfDay 0": {
			p: pendingBlock{
				kind:         DailyUsageKindOffPeak,
				start:        dayStart.Add(11 * time.Hour),
				end:          dayStart.Add(14 * time.Hour),
				status:       DailyUsageStatusComplete,
				unroundedKwh: 0,
			},
			unroundedSum:     0,
			wantBoundary:     DailyUsageBoundaryReadings,
			wantAverage:      floatPtrTest(0),
			wantPercentOfDay: 0,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildDailyUsageBlock(tc.p, tc.unroundedSum)
			assert.Equal(t, tc.wantBoundary, got.BoundarySource)
			assert.Equal(t, tc.wantPercentOfDay, got.PercentOfDay)
			if tc.wantAverage == nil {
				assert.Nil(t, got.AverageKwhPerHour, "AverageKwhPerHour should be omitted")
			} else {
				require.NotNil(t, got.AverageKwhPerHour)
				assert.InDelta(t, *tc.wantAverage, *got.AverageKwhPerHour, 1e-9)
			}
		})
	}
}

func floatPtrTest(v float64) *float64 { return &v }

func BenchmarkBlocks(b *testing.B) {
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, sydneyTZ)
	readings := make([]Reading, 0, 8640)
	for i := range 8640 {
		t := dayStart.Add(time.Duration(i*10) * time.Second)
		secOfDay := t.Hour()*3600 + t.Minute()*60 + t.Second()
		var ppv float64
		if secOfDay >= 6*3600+30*60 && secOfDay <= 18*3600 {
			ppv = float64(500 + i%3000)
		}
		readings = append(readings, Reading{
			Timestamp: t.Unix(),
			Ppv:       ppv,
			Pload:     float64(500 + i%4500),
		})
	}

	for b.Loop() {
		_ = Blocks(readings, "11:00", "14:00", "2026-04-15", "2026-04-16",
			time.Date(2026, 4, 16, 12, 0, 0, 0, sydneyTZ))
	}
}
