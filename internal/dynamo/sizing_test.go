package dynamo

import (
	"encoding/json"
	"testing"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSizing_DailyEnergyRow_BelowReadUnitBoundary covers AC 2.5: a typical
// post-feature DailyEnergyItem (with all three derivedStats sections) must
// stay below 4 KB so a GetItem keeps consuming one RCU.
//
// The 4 KB DynamoDB read-unit boundary is measured against the item's
// serialised AttributeValue map. We marshal once via attributevalue.MarshalMap
// and re-encode to JSON — JSON length is a tight upper bound on the on-disk
// item size in DynamoDB's internal format (DynamoDB strips the structural
// overhead JSON adds).
func TestSizing_DailyEnergyRow_BelowReadUnitBoundary(t *testing.T) {
	avg := 1.2
	row := DailyEnergyItem{
		SysSn:       "ALPHATEST123456789012345",
		Date:        "2026-04-14",
		Epv:         32.4567,
		EInput:      8.2345,
		EOutput:     2.1234,
		ECharge:     12.5678,
		EDischarge:  9.8765,
		EGridCharge: 1.0001,
		DailyUsage: &DailyUsageAttr{
			Blocks: []DailyUsageBlockAttr{
				{Kind: derivedstats.DailyUsageKindNight, Start: "2026-04-13T14:00:00Z", End: "2026-04-13T20:30:00Z", TotalKwh: 1.812345, AverageKwhPerHour: &avg, PercentOfDay: 12, Status: derivedstats.DailyUsageStatusComplete, BoundarySource: derivedstats.DailyUsageBoundaryReadings},
				{Kind: derivedstats.DailyUsageKindMorningPeak, Start: "2026-04-13T20:30:00Z", End: "2026-04-14T01:00:00Z", TotalKwh: 2.412345, AverageKwhPerHour: &avg, PercentOfDay: 16, Status: derivedstats.DailyUsageStatusComplete, BoundarySource: derivedstats.DailyUsageBoundaryEstimated},
				{Kind: derivedstats.DailyUsageKindOffPeak, Start: "2026-04-14T01:00:00Z", End: "2026-04-14T04:00:00Z", TotalKwh: 4.567891, AverageKwhPerHour: &avg, PercentOfDay: 31, Status: derivedstats.DailyUsageStatusComplete, BoundarySource: derivedstats.DailyUsageBoundaryReadings},
				{Kind: derivedstats.DailyUsageKindAfternoonPeak, Start: "2026-04-14T04:00:00Z", End: "2026-04-14T08:00:00Z", TotalKwh: 3.123456, AverageKwhPerHour: &avg, PercentOfDay: 22, Status: derivedstats.DailyUsageStatusComplete, BoundarySource: derivedstats.DailyUsageBoundaryReadings},
				{Kind: derivedstats.DailyUsageKindEvening, Start: "2026-04-14T08:00:00Z", End: "2026-04-14T14:00:00Z", TotalKwh: 2.789012, AverageKwhPerHour: &avg, PercentOfDay: 19, Status: derivedstats.DailyUsageStatusComplete, BoundarySource: derivedstats.DailyUsageBoundaryReadings},
			},
		},
		SocLow: &SocLowAttr{Soc: 18.5, Timestamp: "2026-04-14T19:45:00Z"},
		PeakPeriods: []PeakPeriodAttr{
			{Start: "2026-04-13T22:00:00Z", End: "2026-04-13T22:30:00Z", AvgLoadW: 3500.5, EnergyWh: 1750},
			{Start: "2026-04-14T08:00:00Z", End: "2026-04-14T08:15:00Z", AvgLoadW: 4200, EnergyWh: 1050},
			{Start: "2026-04-14T11:30:00Z", End: "2026-04-14T11:45:00Z", AvgLoadW: 3800, EnergyWh: 950},
		},
		DerivedStatsComputedAt: "2026-04-15T00:30:00Z",
	}

	av, err := attributevalue.MarshalMap(row)
	require.NoError(t, err)

	// JSON-encoded size as the upper-bound proxy for the wire/storage size.
	encoded, err := json.Marshal(av)
	require.NoError(t, err)

	const fourKB = 4 * 1024
	t.Logf("post-feature DailyEnergyItem JSON-encoded AttributeValue size: %d bytes", len(encoded))
	assert.Less(t, len(encoded), fourKB,
		"DailyEnergyItem post-feature size must stay below 4 KB read-unit boundary (AC 2.5)")
}
