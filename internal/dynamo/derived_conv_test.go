package dynamo

import (
	"testing"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDailyUsageFromAttr_Nil(t *testing.T) {
	assert.Nil(t, DailyUsageFromAttr(nil))
}

func TestDailyUsageToAttr_Nil(t *testing.T) {
	assert.Nil(t, DailyUsageToAttr(nil))
}

func TestDailyUsageRoundTrip(t *testing.T) {
	avg := 1.5
	solar := 3.21
	d := &derivedstats.DailyUsage{
		Blocks: []derivedstats.DailyUsageBlock{
			{
				Kind:              derivedstats.DailyUsageKindNight,
				Start:             "2026-04-12T14:00:00Z",
				End:               "2026-04-12T20:30:00Z",
				TotalKwh:          1.8,
				AverageKwhPerHour: &avg,
				PercentOfDay:      12,
				Status:            derivedstats.DailyUsageStatusComplete,
				BoundarySource:    derivedstats.DailyUsageBoundaryReadings,
			},
			{
				Kind:           derivedstats.DailyUsageKindMorningPeak,
				Start:          "2026-04-12T20:30:00Z",
				End:            "2026-04-13T01:00:00Z",
				TotalKwh:       2.4,
				SolarKwh:       &solar,
				PercentOfDay:   18,
				Status:         derivedstats.DailyUsageStatusComplete,
				BoundarySource: derivedstats.DailyUsageBoundaryEstimated,
			},
		},
	}

	got := DailyUsageFromAttr(DailyUsageToAttr(d))
	require.NotNil(t, got)
	require.Len(t, got.Blocks, 2)
	for i, b := range got.Blocks {
		assert.Equal(t, d.Blocks[i].Kind, b.Kind)
		assert.Equal(t, d.Blocks[i].Start, b.Start)
		assert.Equal(t, d.Blocks[i].End, b.End)
		assert.InDelta(t, d.Blocks[i].TotalKwh, b.TotalKwh, 1e-9)
		assert.Equal(t, d.Blocks[i].PercentOfDay, b.PercentOfDay)
		assert.Equal(t, d.Blocks[i].Status, b.Status)
		assert.Equal(t, d.Blocks[i].BoundarySource, b.BoundarySource)
		if d.Blocks[i].AverageKwhPerHour == nil {
			assert.Nil(t, b.AverageKwhPerHour)
		} else {
			require.NotNil(t, b.AverageKwhPerHour)
			assert.InDelta(t, *d.Blocks[i].AverageKwhPerHour, *b.AverageKwhPerHour, 1e-9)
		}
		if d.Blocks[i].SolarKwh == nil {
			assert.Nil(t, b.SolarKwh)
		} else {
			require.NotNil(t, b.SolarKwh)
			assert.InDelta(t, *d.Blocks[i].SolarKwh, *b.SolarKwh, 1e-9)
		}
	}
}

func TestDailyUsageRoundTrip_SolarKwhZero(t *testing.T) {
	zero := 0.0
	d := &derivedstats.DailyUsage{
		Blocks: []derivedstats.DailyUsageBlock{
			{
				Kind:           derivedstats.DailyUsageKindOffPeak,
				Start:          "2026-04-12T01:00:00Z",
				End:            "2026-04-12T04:00:00Z",
				TotalKwh:       0.5,
				SolarKwh:       &zero,
				PercentOfDay:   12,
				Status:         derivedstats.DailyUsageStatusComplete,
				BoundarySource: derivedstats.DailyUsageBoundaryReadings,
			},
		},
	}

	got := DailyUsageFromAttr(DailyUsageToAttr(d))
	require.NotNil(t, got)
	require.Len(t, got.Blocks, 1)
	require.NotNil(t, got.Blocks[0].SolarKwh, "&0.0 must round-trip to non-nil &0.0, not nil")
	assert.InDelta(t, 0.0, *got.Blocks[0].SolarKwh, 1e-9)
}

func TestPeakPeriodsRoundTrip(t *testing.T) {
	periods := []derivedstats.PeakPeriod{
		{Start: "2026-04-12T22:00:00Z", End: "2026-04-12T22:30:00Z", AvgLoadW: 3500.5, EnergyWh: 1750},
		{Start: "2026-04-13T08:00:00Z", End: "2026-04-13T08:15:00Z", AvgLoadW: 4000, EnergyWh: 1000},
	}

	got := PeakPeriodsFromAttr(PeakPeriodsToAttr(periods))
	require.Len(t, got, len(periods))
	for i := range periods {
		assert.Equal(t, periods[i].Start, got[i].Start)
		assert.Equal(t, periods[i].End, got[i].End)
		assert.InDelta(t, periods[i].AvgLoadW, got[i].AvgLoadW, 1e-9)
		assert.InDelta(t, periods[i].EnergyWh, got[i].EnergyWh, 1e-9)
	}
}

func TestPeakPeriodsRoundTrip_EmptySlice(t *testing.T) {
	got := PeakPeriodsFromAttr(PeakPeriodsToAttr([]derivedstats.PeakPeriod{}))
	// Empty slice round-trips to empty slice (or nil — equivalent for this contract).
	assert.Empty(t, got)
}

func TestPeakPeriodsRoundTrip_Nil(t *testing.T) {
	assert.Empty(t, PeakPeriodsFromAttr(nil))
	assert.Empty(t, PeakPeriodsToAttr(nil))
}
