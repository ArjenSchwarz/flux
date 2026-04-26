// Package api implements the Lambda API request handling and business logic.
package api

// StatusResponse is the JSON response for GET /status.
type StatusResponse struct {
	Live        *LiveData    `json:"live"`
	Battery     *BatteryInfo `json:"battery"`
	Rolling15m  *RollingAvg  `json:"rolling15min"`
	Offpeak     *OffpeakData `json:"offpeak"`
	TodayEnergy *TodayEnergy `json:"todayEnergy"`
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
type DayEnergy struct {
	Date                 string   `json:"date"`
	Epv                  float64  `json:"epv"`
	EInput               float64  `json:"eInput"`
	EOutput              float64  `json:"eOutput"`
	ECharge              float64  `json:"eCharge"`
	EDischarge           float64  `json:"eDischarge"`
	OffpeakGridImportKwh *float64 `json:"offpeakGridImportKwh,omitempty"`
	OffpeakGridExportKwh *float64 `json:"offpeakGridExportKwh,omitempty"`
}

// PeakPeriod represents a contiguous period of high household load.
type PeakPeriod struct {
	Start    string  `json:"start"`    // RFC 3339
	End      string  `json:"end"`      // RFC 3339
	AvgLoadW float64 `json:"avgLoadW"` // average Pload, rounded to 1 decimal
	EnergyWh float64 `json:"energyWh"` // total energy, rounded to whole number
}

// DayDetailResponse is the JSON response for GET /day.
type DayDetailResponse struct {
	Date         string            `json:"date"`
	Readings     []TimeSeriesPoint `json:"readings"`
	Summary      *DaySummary       `json:"summary"`
	PeakPeriods  []PeakPeriod      `json:"peakPeriods"`
	EveningNight *EveningNight     `json:"eveningNight,omitempty"`
}

// Status and boundary-source values for an EveningNightBlock. Defined as
// constants so producers, consumers, and tests share a single source of
// truth, mirroring the `OffpeakStatus*` convention in internal/dynamo.
const (
	EveningNightStatusComplete    = "complete"
	EveningNightStatusInProgress  = "in-progress"
	EveningNightBoundaryReadings  = "readings"
	EveningNightBoundaryEstimated = "estimated"
)

// EveningNight groups the evening (last solar → midnight) and night
// (midnight → first solar) no-solar usage blocks for a single calendar date.
// Either field may be nil when only one block applies.
type EveningNight struct {
	Evening *EveningNightBlock `json:"evening,omitempty"`
	Night   *EveningNightBlock `json:"night,omitempty"`
}

// EveningNightBlock describes a no-solar usage period.
//
// Start and End are RFC 3339 timestamps in UTC. AverageKwhPerHour is omitted
// when the elapsed duration is shorter than 60 seconds. Status is one of
// "complete" or "in-progress"; BoundarySource is "readings" when the boundary
// came from a Ppv>0 reading or "estimated" when it was filled from the
// Melbourne sunrise/sunset table.
type EveningNightBlock struct {
	Start             string   `json:"start"`
	End               string   `json:"end"`
	TotalKwh          float64  `json:"totalKwh"`
	AverageKwhPerHour *float64 `json:"averageKwhPerHour"`
	Status            string   `json:"status"`
	BoundarySource    string   `json:"boundarySource"`
}

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
