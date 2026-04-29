package derivedstats

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// genReadings generates a slice of monotonically-increasing readings within
// a single Sydney calendar day. Length 0–500 (kept modest to keep PBT fast).
func genReadings(t *rapid.T, date string) []Reading {
	dayStart, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)
	n := rapid.IntRange(0, 500).Draw(t, "n")
	readings := make([]Reading, n)
	ts := dayStart.Unix()
	for i := range n {
		gap := rapid.IntRange(8, 60).Draw(t, fmt.Sprintf("gap%d", i))
		ts += int64(gap)
		readings[i] = Reading{
			Timestamp: ts,
			Pload:     rapid.Float64Range(0, 10000).Draw(t, fmt.Sprintf("pload%d", i)),
			Ppv:       rapid.Float64Range(0, 5000).Draw(t, fmt.Sprintf("ppv%d", i)),
			Soc:       rapid.Float64Range(0, 100).Draw(t, fmt.Sprintf("soc%d", i)),
		}
	}
	return readings
}

// TestPropertyBlocksDeterminism asserts that two consecutive Blocks calls
// against the same readings + off-peak window produce field-equivalent
// outputs (per AC 1.8).
func TestPropertyBlocksDeterminism(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const date = "2026-04-12"
		readings := genReadings(t, date)
		startH := rapid.IntRange(0, 22).Draw(t, "startH")
		startM := rapid.IntRange(0, 59).Draw(t, "startM")
		endH := rapid.IntRange(startH+1, min(startH+6, 23)).Draw(t, "endH")
		endM := rapid.IntRange(0, 59).Draw(t, "endM")
		offpeakStart := fmt.Sprintf("%02d:%02d", startH, startM)
		offpeakEnd := fmt.Sprintf("%02d:%02d", endH, endM)
		now := time.Date(2026, 4, 13, 12, 0, 0, 0, sydneyTZ)

		a := Blocks(readings, offpeakStart, offpeakEnd, date, "2026-04-13", now)
		b := Blocks(readings, offpeakStart, offpeakEnd, date, "2026-04-13", now)

		if a == nil {
			assert.Nil(t, b)
			return
		}
		assert.Equal(t, len(a.Blocks), len(b.Blocks))
		for i := range a.Blocks {
			assert.Equal(t, a.Blocks[i].Kind, b.Blocks[i].Kind)
			assert.Equal(t, a.Blocks[i].Start, b.Blocks[i].Start)
			assert.Equal(t, a.Blocks[i].End, b.Blocks[i].End)
			assert.InDelta(t, a.Blocks[i].TotalKwh, b.Blocks[i].TotalKwh, 1e-9)
			assert.Equal(t, a.Blocks[i].Status, b.Blocks[i].Status)
		}
	})
}

// TestPropertyMinSOCDeterminism: same readings → same answer.
func TestPropertyMinSOCDeterminism(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const date = "2026-04-12"
		readings := genReadings(t, date)
		soc1, ts1, found1 := MinSOC(readings)
		soc2, ts2, found2 := MinSOC(readings)
		assert.Equal(t, found1, found2)
		if found1 {
			assert.InDelta(t, soc1, soc2, 1e-9)
			assert.Equal(t, ts1, ts2)
		}
	})
}
