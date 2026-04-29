// Package api implements the Lambda API request handling and business logic.
package api

import "github.com/ArjenSchwarz/flux/internal/derivedstats"

// StatusResponse is the JSON response for GET /status.
type StatusResponse struct {
	Live        *LiveData    `json:"live"`
	Battery     *BatteryInfo `json:"battery"`
	Rolling15m  *RollingAvg  `json:"rolling15min"`
	Offpeak     *OffpeakData `json:"offpeak"`
	TodayEnergy *TodayEnergy `json:"todayEnergy"`
	Note        *string      `json:"note"`
}

// LiveData contains the most recent power readings.
type LiveData struct {
	Ppv            float64 `json:"ppv"`
	Pload          float64 `json:"pload"`
	Pbat           float64 `json:"pbat"`
	Pgrid          float64 `json:"pgrid"`
	PgridSustained bool    `json:"pgridSustained"`
	Soc            float64 `json:"soc"`
	Timestamp      string  `json:"timestamp"`
}

// BatteryInfo contains battery capacity, cutoff estimates, and 24h low.
type BatteryInfo struct {
	CapacityKwh     float64 `json:"capacityKwh"`
	CutoffPercent   int     `json:"cutoffPercent"`
	EstimatedCutoff *string `json:"estimatedCutoffTime"`
	Low24h          *Low24h `json:"low24h"`
}

// Low24h contains the lowest SOC reading in the last 24 hours.
type Low24h struct {
	Soc       float64 `json:"soc"`
	Timestamp string  `json:"timestamp"`
}

// RollingAvg contains 15-minute rolling averages and a smoothed cutoff estimate.
type RollingAvg struct {
	AvgLoad         float64 `json:"avgLoad"`
	AvgPbat         float64 `json:"avgPbat"`
	EstimatedCutoff *string `json:"estimatedCutoffTime"`
}

// OffpeakData contains off-peak window times and energy deltas.
//
// Status is "complete" once the window has closed and final deltas are
// written, or "pending" while the window is open and deltas are derived
// from the current daily-energy snapshot. Empty when no record exists or
// when deltas cannot be computed.
type OffpeakData struct {
	WindowStart         string   `json:"windowStart"`
	WindowEnd           string   `json:"windowEnd"`
	Status              string   `json:"status,omitempty"`
	GridUsageKwh        *float64 `json:"gridUsageKwh"`
	SolarKwh            *float64 `json:"solarKwh"`
	BatteryChargeKwh    *float64 `json:"batteryChargeKwh"`
	BatteryDischargeKwh *float64 `json:"batteryDischargeKwh"`
	GridExportKwh       *float64 `json:"gridExportKwh"`
	BatteryDeltaPercent *float64 `json:"batteryDeltaPercent"`
}

// TodayEnergy contains cumulative energy totals for the current day.
type TodayEnergy struct {
	Epv        float64 `json:"epv"`
	EInput     float64 `json:"eInput"`
	EOutput    float64 `json:"eOutput"`
	ECharge    float64 `json:"eCharge"`
	EDischarge float64 `json:"eDischarge"`
}

// HistoryResponse is the JSON response for GET /history.
type HistoryResponse struct {
	Days []DayEnergy `json:"days"`
}

// DayEnergy contains daily energy totals for a single date.
//
// OffpeakGridImportKwh and OffpeakGridExportKwh are populated when an
// off-peak record exists for the date; they let clients separate intentional
// off-peak imports from peak imports.
//
// DailyUsage, SocLow, SocLowTime and PeakPeriods are the per-day derived
// stats added by the daily-derived-stats spec; absent when not yet computed
// (pre-feature row, summarisation pending, or today readings query failed).
type DayEnergy struct {
	Date                 string                    `json:"date"`
	Epv                  float64                   `json:"epv"`
	EInput               float64                   `json:"eInput"`
	EOutput              float64                   `json:"eOutput"`
	ECharge              float64                   `json:"eCharge"`
	EDischarge           float64                   `json:"eDischarge"`
	OffpeakGridImportKwh *float64                  `json:"offpeakGridImportKwh,omitempty"`
	OffpeakGridExportKwh *float64                  `json:"offpeakGridExportKwh,omitempty"`
	DailyUsage           *derivedstats.DailyUsage  `json:"dailyUsage,omitempty"`
	SocLow               *float64                  `json:"socLow,omitempty"`
	SocLowTime           *string                   `json:"socLowTime,omitempty"`
	PeakPeriods          []derivedstats.PeakPeriod `json:"peakPeriods,omitempty"`
	Note                 *string                   `json:"note"`
}

// PeakPeriod is the wire-side alias for derivedstats.PeakPeriod, kept for
// backwards source compatibility with consumers that import the type by its
// historical name.
type PeakPeriod = derivedstats.PeakPeriod

// DailyUsage is the wire-side alias for derivedstats.DailyUsage.
type DailyUsage = derivedstats.DailyUsage

// DailyUsageBlock is the wire-side alias for derivedstats.DailyUsageBlock.
type DailyUsageBlock = derivedstats.DailyUsageBlock

// DayDetailResponse is the JSON response for GET /day.
type DayDetailResponse struct {
	Date        string                    `json:"date"`
	Readings    []TimeSeriesPoint         `json:"readings"`
	Summary     *DaySummary               `json:"summary"`
	PeakPeriods []derivedstats.PeakPeriod `json:"peakPeriods"`
	DailyUsage  *derivedstats.DailyUsage  `json:"dailyUsage,omitempty"`
	Note        *string                   `json:"note"`
}

// Status, boundary-source, and kind values for a DailyUsageBlock. Aliased to
// the canonical constants in derivedstats so producers, consumers, and tests
// share a single source of truth.
const (
	DailyUsageStatusComplete    = derivedstats.DailyUsageStatusComplete
	DailyUsageStatusInProgress  = derivedstats.DailyUsageStatusInProgress
	DailyUsageBoundaryReadings  = derivedstats.DailyUsageBoundaryReadings
	DailyUsageBoundaryEstimated = derivedstats.DailyUsageBoundaryEstimated

	DailyUsageKindNight         = derivedstats.DailyUsageKindNight
	DailyUsageKindMorningPeak   = derivedstats.DailyUsageKindMorningPeak
	DailyUsageKindOffPeak       = derivedstats.DailyUsageKindOffPeak
	DailyUsageKindAfternoonPeak = derivedstats.DailyUsageKindAfternoonPeak
	DailyUsageKindEvening       = derivedstats.DailyUsageKindEvening
)

// TimeSeriesPoint is a single downsampled reading in the day detail response.
type TimeSeriesPoint struct {
	Timestamp string  `json:"timestamp"`
	Ppv       float64 `json:"ppv"`
	Pload     float64 `json:"pload"`
	Pbat      float64 `json:"pbat"`
	Pgrid     float64 `json:"pgrid"`
	Soc       float64 `json:"soc"`
}

// DaySummary contains energy totals and the SOC low for a day.
type DaySummary struct {
	Epv        *float64 `json:"epv"`
	EInput     *float64 `json:"eInput"`
	EOutput    *float64 `json:"eOutput"`
	ECharge    *float64 `json:"eCharge"`
	EDischarge *float64 `json:"eDischarge"`
	SocLow     *float64 `json:"socLow"`
	SocLowTime *string  `json:"socLowTime"`
}
