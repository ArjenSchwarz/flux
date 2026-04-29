package api

import (
	"context"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// BenchmarkHandleHistory_30Days exercises a 30-day /history request against a
// fixture mock reader that returns rows with all three derivedStats sections.
// Used to capture the post-feature p95 latency baseline (AC 4.7) and the
// serialised payload size (AC 4.8).
//
// The pre-feature baseline must be captured against the parent commit by
// running the same benchmark there; this file does not encode the baseline
// number itself because it would drift with unrelated changes to the handler.
func BenchmarkHandleHistory_30Days(b *testing.B) {
	loc, _ := time.LoadLocation("Australia/Sydney")
	now := time.Date(2026, 4, 15, 10, 0, 0, 0, loc)

	// Build 30 days of fixture rows, every row carrying derivedStats.
	rows := make([]dynamo.DailyEnergyItem, 30)
	avg := 1.2
	for i := range 30 {
		date := now.AddDate(0, 0, -29+i).Format("2006-01-02")
		rows[i] = dynamo.DailyEnergyItem{
			SysSn: "TEST", Date: date,
			Epv: float64(20 + i%10), EInput: 5, EOutput: 2, ECharge: 8, EDischarge: 6,
			DailyUsage: &dynamo.DailyUsageAttr{
				Blocks: []dynamo.DailyUsageBlockAttr{
					{Kind: derivedstats.DailyUsageKindNight, Start: date + "T14:00:00Z", End: date + "T20:30:00Z", TotalKwh: 1.8, AverageKwhPerHour: &avg, PercentOfDay: 12, Status: derivedstats.DailyUsageStatusComplete, BoundarySource: derivedstats.DailyUsageBoundaryReadings},
					{Kind: derivedstats.DailyUsageKindMorningPeak, Start: date + "T20:30:00Z", End: date + "T01:00:00Z", TotalKwh: 2.4, AverageKwhPerHour: &avg, PercentOfDay: 16, Status: derivedstats.DailyUsageStatusComplete, BoundarySource: derivedstats.DailyUsageBoundaryReadings},
				},
			},
			SocLow:                 &dynamo.SocLowAttr{Soc: float64(18 + i), Timestamp: date + "T19:45:00Z"},
			PeakPeriods:            []dynamo.PeakPeriodAttr{{Start: date + "T22:00:00Z", End: date + "T22:30:00Z", AvgLoadW: 3500, EnergyWh: 1750}},
			DerivedStatsComputedAt: date + "T22:30:00Z",
		}
	}

	mr := &mockReader{
		queryDailyEnergyFn: func(_ context.Context, _, _, _ string) ([]dynamo.DailyEnergyItem, error) {
			return rows, nil
		},
	}
	h := NewHandler(mr, nil, "TEST", "tok", "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }
	req := historyRequest(map[string]string{"days": "30"})
	req.Headers["authorization"] = "Bearer tok"

	for b.Loop() {
		_, _ = h.Handle(context.Background(), req)
	}
}
