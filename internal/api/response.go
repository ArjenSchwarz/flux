// Package api implements the Lambda API request handling and business logic.
package api

import (
	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// StatusResponse is the JSON response for GET /status.
type StatusResponse struct {
	Live        *LiveData    `json:"live"`
	Battery     *BatteryInfo `json:"battery"`
	Rolling15m  *RollingAvg  `json:"rolling15min"`
	Offpeak     *OffpeakData `json:"offpeak"`
	TodayEnergy *TodayEnergy `json:"todayEnergy"`
	// PeakGridImportKwh is today's peak (non-off-peak) grid import so far,
	// integrated directly from Pgrid over the windows bracketing off-peak
	// (T-1421). Absent when the morning window is not yet integrable; the iOS
	// Dashboard then falls back to the eInput − offpeak residual.
	PeakGridImportKwh *float64 `json:"peakGridImportKwh,omitempty"`
	Note              *string  `json:"note"`
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

// BatteryInfo contains battery capacity, cutoff estimates, and the lowest
// SOC since 00:00 Sydney local on the current day.
//
// CantEmptyBeforeOffpeak is the T-1327 "won't drain in time" indicator. It is
// a pointer with no omitempty so the wire emits `null` when the condition
// does not hold and `true` when it does — `false` is never serialised
// (mirrors EstimatedCutoff). See AC 2.2.
type BatteryInfo struct {
	CapacityKwh            float64 `json:"capacityKwh"`
	CutoffPercent          int     `json:"cutoffPercent"`
	EstimatedCutoff        *string `json:"estimatedCutoffTime"`
	CantEmptyBeforeOffpeak *bool   `json:"cantEmptyBeforeOffpeak"`
	Low24h                 *Low24h `json:"low24h"`
}

// Low24h contains the lowest SOC reading since 00:00 Sydney local on the
// current day. The JSON field name is preserved for backwards compatibility
// with existing clients; the underlying window is no longer 24 hours and
// no longer tied to off-peak (see specs/low-since-offpeak/decision_log.md
// Decision 4). Nil when no readings have arrived since Sydney midnight.
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
//
// ProjectedEndSoc is the idealised SoC (percent) the battery will reach by
// WindowEnd if charging continues at full capability. It is a pointer with no
// omitempty so absence serialises as explicit JSON null (mirrors
// EstimatedCutoff): null means "no projection — outside the window or no fresh
// live data", which clients must distinguish from a real value. See AC 3.2.
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
	ProjectedEndSoc     *float64 `json:"projectedEndSoc"`
}

// BandImport is one rated band's grid import for a day.
//
// Start and End are the band's "HH:MM" boundaries in Sydney local time (End
// may be "24:00"), snapshotted alongside the value so a later plan-window
// edit shows up to the client as a geometry mismatch instead of silently
// mispricing the day (Q23).
//
// Only rated bands appear: the free band's import lives in
// offpeakGridImportKwh, which owns that quantity exclusively (Q31).
type BandImport struct {
	Start string  `json:"start"`
	End   string  `json:"end"`
	Kwh   float64 `json:"kwh"`
}

// OffpeakSource describes the flux-offpeak row that OffpeakGridImportKwh came
// from. Clients need it to decide whether that value can price a plan's free
// band: the geometry so a plan-window edit shows up as a mismatch rather than
// silently mispricing the day (Q23/Q16), and the integration provenance so a
// sparse-complete row (integrated, no samples) is recognised as a zero-delta
// artifact rather than a measured zero.
//
// The fields are flattened into DaySummary and DayEnergy, alongside the
// off-peak values they describe. All are absent when the day has no off-peak
// split; the window is additionally absent on pre-feature rows, which can only
// have been integrated under 11:00–14:00.
type OffpeakSource struct {
	OffpeakWindowStart  string `json:"offpeakWindowStart,omitempty"`
	OffpeakWindowEnd    string `json:"offpeakWindowEnd,omitempty"`
	OffpeakIntegratedAt string `json:"offpeakIntegratedAt,omitempty"`
	OffpeakSampleCount  *int   `json:"offpeakSampleCount,omitempty"`
}

// offpeakSourceFrom describes the row a day's off-peak split came from.
//
// Today's split is live-integrated from readings over the resolved window
// rather than read off the row, so it carries that window's geometry and no
// row provenance — it is a measurement by construction.
func offpeakSourceFrom(op dynamo.OffpeakItem, isToday bool, window *offpeakWindow) OffpeakSource {
	if isToday && op.Status != dynamo.OffpeakStatusComplete {
		start, end := hhmmBounds(window)
		return OffpeakSource{OffpeakWindowStart: start, OffpeakWindowEnd: end}
	}
	start, end := op.Geometry()
	count := op.IntegrationSampleCount
	return OffpeakSource{
		OffpeakWindowStart:  start,
		OffpeakWindowEnd:    end,
		OffpeakIntegratedAt: op.IntegratedAt,
		OffpeakSampleCount:  &count,
	}
}

// bandImportsFromAttr converts the stored per-band split to the wire shape.
// Returns nil for an absent split so the field is omitted rather than sent as
// an empty array, which a client could misread as "zero import in every band".
func bandImportsFromAttr(stored []dynamo.BandImportAttr) []BandImport {
	if len(stored) == 0 {
		return nil
	}
	out := make([]BandImport, len(stored))
	for i, b := range stored {
		out[i] = BandImport{Start: b.Start, End: b.End, Kwh: b.Kwh}
	}
	return out
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
	PeakGridImportKwh    *float64                  `json:"peakGridImportKwh,omitempty"`
	BandImports          []BandImport              `json:"bandImports,omitempty"`
	OffpeakSource                                  // flattened: offpeakWindowStart/End, offpeakIntegratedAt, offpeakSampleCount
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
//
// OffpeakGridImportKwh and OffpeakGridExportKwh are populated when an
// off-peak record exists for the date, so clients can split EInput into
// peak vs off-peak imports without re-deriving the window from readings.
type DaySummary struct {
	Epv                  *float64 `json:"epv"`
	EInput               *float64 `json:"eInput"`
	EOutput              *float64 `json:"eOutput"`
	ECharge              *float64 `json:"eCharge"`
	EDischarge           *float64 `json:"eDischarge"`
	SocLow               *float64 `json:"socLow"`
	SocLowTime           *string  `json:"socLowTime"`
	OffpeakGridImportKwh *float64 `json:"offpeakGridImportKwh,omitempty"`
	OffpeakGridExportKwh *float64 `json:"offpeakGridExportKwh,omitempty"`
	PeakGridImportKwh    *float64 `json:"peakGridImportKwh,omitempty"`

	// BandImports is the day's rated-band import split, absent when the day
	// is unpriced or its split is unavailable (AC 3.6).
	BandImports []BandImport `json:"bandImports,omitempty"`

	// OffpeakSource describes the row OffpeakGridImportKwh came from, so the
	// client can tell whether it can price this plan's free band.
	OffpeakSource
}
