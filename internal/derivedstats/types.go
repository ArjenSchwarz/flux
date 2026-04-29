// Package derivedstats holds the reading-derived per-day statistics shared
// by the Lambda API and the poller summarisation pass. It is a leaf package:
// it imports nothing from other Flux packages so that both consumers can
// import it without forming a cycle (Decision 9).
package derivedstats

// Reading mirrors the subset of the storage ReadingItem fields the helpers
// consume. Defined here (instead of importing the dynamo package) to keep
// this package leaf-pure. Call sites convert []dynamo.ReadingItem to
// []derivedstats.Reading via a one-line slice mapping.
type Reading struct {
	Timestamp int64
	Ppv       float64
	Pload     float64
	Soc       float64
	Pbat      float64
	Pgrid     float64
}

// Status, boundary-source, and kind values for a DailyUsageBlock. Defined as
// constants so producers, consumers, and tests share a single source of
// truth.
const (
	DailyUsageStatusComplete    = "complete"
	DailyUsageStatusInProgress  = "in-progress"
	DailyUsageBoundaryReadings  = "readings"
	DailyUsageBoundaryEstimated = "estimated"

	DailyUsageKindNight         = "night"
	DailyUsageKindMorningPeak   = "morningPeak"
	DailyUsageKindOffPeak       = "offPeak"
	DailyUsageKindAfternoonPeak = "afternoonPeak"
	DailyUsageKindEvening       = "evening"
)

// DailyUsage groups the chronological no-overlap usage blocks (night,
// morningPeak, offPeak, afternoonPeak, evening) for a single calendar date.
// Blocks is ordered chronologically by Start and contains at most five
// entries; consumers identify each block by its Kind.
type DailyUsage struct {
	Blocks []DailyUsageBlock `json:"blocks"`
}

// DailyUsageBlock describes one chronological slice of a calendar day.
//
// Start and End are RFC 3339 timestamps in UTC. AverageKwhPerHour is omitted
// when the elapsed duration is shorter than 60 seconds. Status is one of
// "complete" or "in-progress"; BoundarySource is "readings" when the
// emitted boundaries came from real data (readings, SSM-configured off-peak
// times, calendar midnight, or in-progress request-time clamping) or
// "estimated" when at least one emitted boundary was filled from the
// Melbourne sunrise/sunset table.
type DailyUsageBlock struct {
	Kind              string   `json:"kind"`
	Start             string   `json:"start"`
	End               string   `json:"end"`
	TotalKwh          float64  `json:"totalKwh"`
	AverageKwhPerHour *float64 `json:"averageKwhPerHour,omitempty"`
	PercentOfDay      int      `json:"percentOfDay"`
	Status            string   `json:"status"`
	BoundarySource    string   `json:"boundarySource"`
}

// PeakPeriod represents a contiguous period of high household load.
type PeakPeriod struct {
	Start    string  `json:"start"`    // RFC 3339
	End      string  `json:"end"`      // RFC 3339
	AvgLoadW float64 `json:"avgLoadW"` // average Pload, rounded to 1 decimal
	EnergyWh float64 `json:"energyWh"` // total energy, rounded to whole number
}
