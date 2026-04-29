package derivedstats

import (
	"cmp"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// sydneyReading creates a Reading at the given Sydney local time with the specified Pload.
// Other fields default to zero.
func sydneyReading(hour, minute, second int, pload float64) Reading {
	t := time.Date(2026, 4, 15, hour, minute, second, 0, sydneyTZ)
	return Reading{Timestamp: t.Unix(), Pload: pload}
}

// sydneyReadings creates a sequence of readings at 10-second intervals starting
// at the given Sydney local time, one per Pload value.
func sydneyReadings(hour, minute, second int, ploads ...float64) []Reading {
	start := time.Date(2026, 4, 15, hour, minute, second, 0, sydneyTZ)
	out := make([]Reading, len(ploads))
	for i, p := range ploads {
		out[i] = Reading{
			Timestamp: start.Add(time.Duration(i*10) * time.Second).Unix(),
			Pload:     p,
		}
	}
	return out
}

func TestPeakPeriods(t *testing.T) {
	const opStart = "11:00"
	const opEnd = "14:00"

	tests := map[string]struct {
		readings     []Reading
		offpeakStart string
		offpeakEnd   string
		wantCount    int
		check        func(t *testing.T, got []PeakPeriod)
	}{
		"empty readings": {
			readings:     nil,
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"all readings in off-peak": {
			readings:     sydneyReadings(12, 0, 0, 5000, 6000, 7000, 8000, 9000, 10000, 10000, 10000, 10000, 10000, 10000, 10000, 10000),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"uniform load": {
			readings:     sydneyReadings(8, 0, 0, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"single peak above mean": {
			readings: append(
				sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100),
				sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...,
			),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				assert.True(t, got[0].EnergyWh > 0)
				assert.True(t, got[0].AvgLoadW > 0)
			},
		},
		"two clusters within 5min merge": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				r = append(r, sydneyReadings(8, 3, 10, 100, 100, 100, 100)...)
				r = append(r, sydneyReadings(8, 3, 50, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
		},
		"two clusters >5min separate": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				for i := range 36 {
					ts := time.Date(2026, 4, 15, 8, 3, 10+i*10, 0, sydneyTZ)
					r = append(r, Reading{Timestamp: ts.Unix(), Pload: 100})
				}
				r = append(r, sydneyReadings(8, 9, 10, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 2,
		},
		"period under 2min discarded": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"more than 3 returns top 3": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(6, 0, 0, 100, 100, 100, 100, 100, 100)...)
				starts := []struct{ h, m int }{{7, 0}, {7, 15}, {7, 30}, {7, 45}}
				for i, s := range starts {
					for j := range 13 {
						ts := time.Date(2026, 4, 15, s.h, s.m, j*10, 0, sydneyTZ)
						r = append(r, Reading{Timestamp: ts.Unix(), Pload: 5000 + float64(i*100)})
					}
					if i < len(starts)-1 {
						endSec := s.m*60 + 120
						nextStartSec := starts[i+1].m * 60
						for sec := endSec + 10; sec < nextStartSec; sec += 10 {
							ts := time.Date(2026, 4, 15, s.h, 0, sec, 0, sydneyTZ)
							r = append(r, Reading{Timestamp: ts.Unix(), Pload: 100})
						}
					}
				}
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 3,
			check: func(t *testing.T, got []PeakPeriod) {
				for i := 1; i < len(got); i++ {
					assert.True(t, got[i-1].EnergyWh >= got[i].EnergyWh, "periods should be in descending energy order")
				}
			},
		},
		"gap >60s skips energy pair": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReading(8, 1, 0, 5000))
				r = append(r, sydneyReading(8, 1, 10, 5000))
				r = append(r, sydneyReading(8, 1, 20, 5000))
				r = append(r, sydneyReading(8, 2, 21, 5000))
				r = append(r, sydneyReading(8, 2, 31, 5000))
				r = append(r, sydneyReading(8, 2, 41, 5000))
				r = append(r, sydneyReading(8, 2, 51, 5000))
				r = append(r, sydneyReading(8, 3, 1, 5000))
				r = append(r, sydneyReading(8, 3, 11, 5000))
				r = append(r, sydneyReading(8, 3, 21, 5000))
				r = append(r, sydneyReading(8, 3, 31, 5000))
				r = append(r, sydneyReading(8, 3, 41, 5000))
				r = append(r, sydneyReading(8, 3, 51, 5000))
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				assert.True(t, got[0].EnergyWh > 0)
				assert.True(t, got[0].EnergyWh < 236)
			},
		},
		"off-peak boundary": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReading(11, 0, 0, 9000))
				r = append(r, sydneyReadings(14, 0, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				assert.Contains(t, got[0].Start, "T04:00:00Z")
			},
		},
		"off-peak boundary clustering": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReadings(10, 57, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				r = append(r, sydneyReadings(11, 0, 0, 5000, 5000, 5000)...)
				r = append(r, sydneyReadings(14, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 2,
		},
		"transitive merge": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				for i := range 24 {
					ts := time.Date(2026, 4, 15, 8, 3, 10+i*10, 0, sydneyTZ)
					r = append(r, Reading{Timestamp: ts.Unix(), Pload: 100})
				}
				r = append(r, sydneyReadings(8, 7, 10, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				for i := range 24 {
					ts := time.Date(2026, 4, 15, 8, 9, 20+i*10, 0, sydneyTZ)
					r = append(r, Reading{Timestamp: ts.Unix(), Pload: 100})
				}
				r = append(r, sydneyReadings(8, 13, 20, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
		},
		"zero-energy sparse period discarded": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReading(8, 1, 0, 5000))
				r = append(r, sydneyReading(8, 2, 1, 5000))
				r = append(r, sydneyReading(8, 3, 2, 5000))
				r = append(r, sydneyReading(8, 4, 3, 5000))
				r = append(r, sydneyReading(8, 5, 4, 5000))
				r = append(r, sydneyReading(8, 6, 5, 5000))
				r = append(r, sydneyReading(8, 7, 6, 5000))
				r = append(r, sydneyReading(8, 8, 7, 5000))
				r = append(r, sydneyReading(8, 9, 8, 5000))
				r = append(r, sydneyReading(8, 10, 9, 5000))
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"invalid off-peak parse failure": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: "invalid", offpeakEnd: "also-invalid",
			wantCount: 1,
		},
		"negative Pload clamped": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(8, 0, 0, -500, -500, -500, -500, -500, -500)...)
				r = append(r, sydneyReadings(8, 1, 0, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				assert.True(t, got[0].EnergyWh > 0)
			},
		},
		"single reading": {
			readings:     []Reading{sydneyReading(8, 0, 0, 5000)},
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 0,
		},
		"DST transition day (AEDT→AEST)": {
			readings: func() []Reading {
				var r []Reading
				dst := func(h, m, s int, pload float64) Reading {
					ts := time.Date(2026, 4, 5, h, m, s, 0, sydneyTZ)
					return Reading{Timestamp: ts.Unix(), Pload: pload}
				}
				for i := range 6 {
					r = append(r, dst(8, 0, i*10, 100))
				}
				for i := range 6 {
					r = append(r, dst(12, 0, i*10, 9000))
				}
				for i := range 13 {
					r = append(r, dst(15, 0, i*10, 5000))
				}
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 1,
			check: func(t *testing.T, got []PeakPeriod) {
				assert.Contains(t, got[0].Start, "T05:00:00Z")
			},
		},
		"two periods with same rounded energy ranked by unrounded": {
			readings: func() []Reading {
				var r []Reading
				r = append(r, sydneyReadings(6, 0, 0, 100, 100, 100, 100, 100, 100)...)
				r = append(r, sydneyReadings(7, 0, 0, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001, 5001)...)
				for i := range 36 {
					ts := time.Date(2026, 4, 15, 7, 2, 10+i*10, 0, sydneyTZ)
					r = append(r, Reading{Timestamp: ts.Unix(), Pload: 100})
				}
				r = append(r, sydneyReadings(7, 8, 10, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999, 4999)...)
				return r
			}(),
			offpeakStart: opStart, offpeakEnd: opEnd,
			wantCount: 2,
			check: func(t *testing.T, got []PeakPeriod) {
				assert.True(t, got[0].EnergyWh >= got[1].EnergyWh)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := PeakPeriods(tc.readings, tc.offpeakStart, tc.offpeakEnd)
			assert.Len(t, got, tc.wantCount)
			if tc.check != nil && len(got) > 0 {
				tc.check(t, got)
			}
		})
	}
}

func TestPeakPeriodsProperties(t *testing.T) {
	type pbtInput struct {
		readings     []Reading
		offpeakStart string
		offpeakEnd   string
	}

	gen := rapid.Custom(func(t *rapid.T) pbtInput {
		startH := rapid.IntRange(0, 22).Draw(t, "offpeakStartH")
		startM := rapid.IntRange(0, 59).Draw(t, "offpeakStartM")
		endH := rapid.IntRange(startH+1, min(startH+6, 23)).Draw(t, "offpeakEndH")
		endM := rapid.IntRange(0, 59).Draw(t, "offpeakEndM")
		if endH*60+endM <= startH*60+startM {
			endM = startM + 1
			if endM > 59 {
				endH++
				endM = 0
			}
		}

		n := rapid.IntRange(0, 500).Draw(t, "numReadings")
		dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, sydneyTZ)
		readings := make([]Reading, n)
		ts := dayStart.Unix()
		for i := range n {
			gap := rapid.IntRange(8, 15).Draw(t, fmt.Sprintf("gap%d", i))
			ts += int64(gap)
			readings[i] = Reading{
				Timestamp: ts,
				Pload:     rapid.Float64Range(0, 10000).Draw(t, fmt.Sprintf("pload%d", i)),
			}
		}
		return pbtInput{
			readings:     readings,
			offpeakStart: fmt.Sprintf("%02d:%02d", startH, startM),
			offpeakEnd:   fmt.Sprintf("%02d:%02d", endH, endM),
		}
	})

	parseMinOfDay := func(s string) int {
		h := int(s[0]-'0')*10 + int(s[1]-'0')
		m := int(s[3]-'0')*10 + int(s[4]-'0')
		return h*60 + m
	}

	t.Run("result count <= 3", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := PeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			assert.LessOrEqual(t, len(got), 3)
		})
	})

	t.Run("all periods outside off-peak", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := PeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			opStartM := parseMinOfDay(in.offpeakStart)
			opEndM := parseMinOfDay(in.offpeakEnd)
			for _, p := range got {
				startT, _ := time.Parse(time.RFC3339, p.Start)
				endT, _ := time.Parse(time.RFC3339, p.End)
				startMOD := startT.In(sydneyTZ).Hour()*60 + startT.In(sydneyTZ).Minute()
				endMOD := endT.In(sydneyTZ).Hour()*60 + endT.In(sydneyTZ).Minute()
				assert.False(t, startMOD >= opStartM && startMOD < opEndM, "period start %s is in off-peak", p.Start)
				assert.False(t, endMOD >= opStartM && endMOD < opEndM, "period end %s is in off-peak", p.End)
			}
		})
	})

	t.Run("non-overlapping", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := PeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			if len(got) < 2 {
				return
			}
			sorted := make([]PeakPeriod, len(got))
			copy(sorted, got)
			slices.SortFunc(sorted, func(a, b PeakPeriod) int { return cmp.Compare(a.Start, b.Start) })
			for i := 1; i < len(sorted); i++ {
				assert.LessOrEqual(t, sorted[i-1].End, sorted[i].Start,
					"periods overlap: %s-%s and %s-%s", sorted[i-1].Start, sorted[i-1].End, sorted[i].Start, sorted[i].End)
			}
		})
	})

	t.Run("energy positive", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := PeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			for _, p := range got {
				assert.True(t, p.EnergyWh > 0, "energy should be positive, got %f", p.EnergyWh)
			}
		})
	})

	t.Run("descending energy order", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := PeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			for i := 1; i < len(got); i++ {
				assert.True(t, got[i-1].EnergyWh >= got[i].EnergyWh,
					"not descending: %f < %f", got[i-1].EnergyWh, got[i].EnergyWh)
			}
		})
	})

	t.Run("duration >= 2 minutes", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			in := gen.Draw(t, "input")
			got := PeakPeriods(in.readings, in.offpeakStart, in.offpeakEnd)
			for _, p := range got {
				startT, _ := time.Parse(time.RFC3339, p.Start)
				endT, _ := time.Parse(time.RFC3339, p.End)
				assert.True(t, endT.Sub(startT) >= 2*time.Minute,
					"duration %s < 2 minutes for period %s-%s", endT.Sub(startT), p.Start, p.End)
			}
		})
	})
}

func BenchmarkPeakPeriods(b *testing.B) {
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, sydneyTZ)
	readings := make([]Reading, 0, 8640)
	for i := range 8640 {
		readings = append(readings, Reading{
			Timestamp: dayStart.Unix() + int64(i*10),
			Pload:     float64(500 + i%4500),
		})
	}

	for b.Loop() {
		_ = PeakPeriods(readings, "11:00", "14:00")
	}
}
