package dynamo

import (
	"fmt"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func genDailyUsage(t *rapid.T) *derivedstats.DailyUsage {
	if rapid.Bool().Draw(t, "nilUsage") {
		return nil
	}
	n := rapid.IntRange(0, 5).Draw(t, "blockCount")
	blocks := make([]derivedstats.DailyUsageBlock, n)
	for i := range n {
		var avg *float64
		if rapid.Bool().Draw(t, fmt.Sprintf("hasAvg%d", i)) {
			a := rapid.Float64Range(0.0, 100.0).Draw(t, fmt.Sprintf("avg%d", i))
			avg = &a
		}
		blocks[i] = derivedstats.DailyUsageBlock{
			Kind:              rapid.SampledFrom([]string{"night", "morningPeak", "offPeak", "afternoonPeak", "evening"}).Draw(t, fmt.Sprintf("kind%d", i)),
			Start:             time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
			End:               time.Date(2026, 4, 12, 6, 0, 0, 0, time.UTC).Format(time.RFC3339),
			TotalKwh:          rapid.Float64Range(0, 50).Draw(t, fmt.Sprintf("kwh%d", i)),
			AverageKwhPerHour: avg,
			PercentOfDay:      rapid.IntRange(0, 100).Draw(t, fmt.Sprintf("pct%d", i)),
			Status:            rapid.SampledFrom([]string{"complete", "in-progress"}).Draw(t, fmt.Sprintf("status%d", i)),
			BoundarySource:    rapid.SampledFrom([]string{"readings", "estimated"}).Draw(t, fmt.Sprintf("bs%d", i)),
		}
	}
	return &derivedstats.DailyUsage{Blocks: blocks}
}

func genPeakPeriods(t *rapid.T) []derivedstats.PeakPeriod {
	n := rapid.IntRange(0, 3).Draw(t, "peakCount")
	if n == 0 {
		return nil
	}
	out := make([]derivedstats.PeakPeriod, n)
	for i := range n {
		out[i] = derivedstats.PeakPeriod{
			Start:    time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
			End:      time.Date(2026, 4, 12, 1, 0, 0, 0, time.UTC).Format(time.RFC3339),
			AvgLoadW: rapid.Float64Range(0, 10000).Draw(t, fmt.Sprintf("avg%d", i)),
			EnergyWh: rapid.Float64Range(0, 5000).Draw(t, fmt.Sprintf("wh%d", i)),
		}
	}
	return out
}

func TestPropertyDailyUsageRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := genDailyUsage(t)
		out := DailyUsageFromAttr(DailyUsageToAttr(d))
		if d == nil {
			assert.Nil(t, out)
			return
		}
		assert.Equal(t, len(d.Blocks), len(out.Blocks))
		for i := range d.Blocks {
			assert.Equal(t, d.Blocks[i].Kind, out.Blocks[i].Kind)
			assert.InDelta(t, d.Blocks[i].TotalKwh, out.Blocks[i].TotalKwh, 1e-9)
			assert.Equal(t, d.Blocks[i].PercentOfDay, out.Blocks[i].PercentOfDay)
			assert.Equal(t, d.Blocks[i].Status, out.Blocks[i].Status)
			assert.Equal(t, d.Blocks[i].BoundarySource, out.Blocks[i].BoundarySource)
		}
	})
}

func TestPropertyPeakPeriodsRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ps := genPeakPeriods(t)
		out := PeakPeriodsFromAttr(PeakPeriodsToAttr(ps))
		assert.Equal(t, len(ps), len(out))
		for i := range ps {
			assert.Equal(t, ps[i].Start, out[i].Start)
			assert.Equal(t, ps[i].End, out[i].End)
			assert.InDelta(t, ps[i].AvgLoadW, out[i].AvgLoadW, 1e-9)
			assert.InDelta(t, ps[i].EnergyWh, out[i].EnergyWh, 1e-9)
		}
	})
}
