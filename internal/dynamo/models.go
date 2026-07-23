package dynamo

import (
	"fmt"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
)

const ttl30Days = 30 * 24 * time.Hour

// Off-peak record status values.
const (
	OffpeakStatusPending  = "pending"
	OffpeakStatusComplete = "complete"
)

// ReadingItem represents a row in the flux-readings table.
type ReadingItem struct {
	SysSn     string  `dynamodbav:"sysSn"`
	Timestamp int64   `dynamodbav:"timestamp"`
	Ppv       float64 `dynamodbav:"ppv"`
	Pload     float64 `dynamodbav:"pload"`
	Pbat      float64 `dynamodbav:"pbat"`
	Pgrid     float64 `dynamodbav:"pgrid"`
	Soc       float64 `dynamodbav:"soc"`
	TTL       int64   `dynamodbav:"ttl"`
}

// DailyEnergyItem represents a row in the flux-daily-energy table.
//
// The DailyUsage / SocLow / PeakPeriods / DerivedStatsComputedAt fields are
// populated by the poller's hourly summarisation pass (daily-derived-stats
// spec). They are optional: pre-feature rows and rows whose pass has not
// yet completed deserialise with these fields set to their zero value
// (nil for pointers/slices, empty string for the sentinel).
type DailyEnergyItem struct {
	SysSn       string  `dynamodbav:"sysSn"`
	Date        string  `dynamodbav:"date"`
	Epv         float64 `dynamodbav:"epv"`
	EInput      float64 `dynamodbav:"eInput"`
	EOutput     float64 `dynamodbav:"eOutput"`
	ECharge     float64 `dynamodbav:"eCharge"`
	EDischarge  float64 `dynamodbav:"eDischarge"`
	EGridCharge float64 `dynamodbav:"eGridCharge"`

	DailyUsage             *DailyUsageAttr  `dynamodbav:"dailyUsage,omitempty"`
	SocLow                 *SocLowAttr      `dynamodbav:"socLow,omitempty"`
	PeakPeriods            []PeakPeriodAttr `dynamodbav:"peakPeriods,omitempty"`
	DerivedStatsComputedAt string           `dynamodbav:"derivedStatsComputedAt,omitempty"`

	// PeakGridImportKwh is the trapezoidal integration of max(pgrid,0) over the
	// two windows bracketing off-peak (peak-from-readings spec). It is computed
	// independently of the derivedStats block above and gated on its own
	// PeakComputedAt sentinel (Decision 3): the hourly pass forward-fills it
	// onto each day's row as that day becomes "yesterday", and the
	// cmd/backfill-grid CLI fills pre-deploy historical rows within the readings
	// TTL (Decision 7) — neither redoes the other derived stats. Absent when the
	// integration's usability gate fails for either sub-window.
	//
	// Storage naming note (Decision 6): the off-peak counterpart is stored as
	// OffpeakItem.GridUsageKwh; this field uses "Import" to match the API key
	// peakGridImportKwh. The divergence is intentional and not worth a rename.
	PeakGridImportKwh *float64 `dynamodbav:"peakGridImportKwh,omitempty"`
	PeakComputedAt    string   `dynamodbav:"peakComputedAt,omitempty"`

	// BandImports is the day's per-band grid import, captured at day close so
	// banded costs survive the 30-day readings TTL (Q13). It holds the RATED
	// segments only — free-window import lives on the flux-offpeak row, which
	// owns that quantity exclusively (Q31). Each entry snapshots the geometry
	// it was captured under (Q23), so a later window edit shows up as a
	// mismatch instead of silently mispricing the day.
	//
	// Gated on its own BandsComputedAt sentinel, third in the group set
	// UpdateDailyEnergyDerived writes independently. Absent when the
	// integrator's usability gate fails for any rated segment.
	BandImports     []BandImportAttr `dynamodbav:"bandImports,omitempty"`
	BandsComputedAt string           `dynamodbav:"bandsComputedAt,omitempty"`
}

// BandImportAttr is the storage shape for one rated band's import energy.
type BandImportAttr struct {
	Start string  `dynamodbav:"start"` // HH:MM, Sydney local
	End   string  `dynamodbav:"end"`   // HH:MM, may be "24:00"
	Kwh   float64 `dynamodbav:"kwh"`
}

// DailyUsageAttr is the storage shape for derivedstats.DailyUsage.
type DailyUsageAttr struct {
	Blocks []DailyUsageBlockAttr `dynamodbav:"blocks"`
}

// DailyUsageBlockAttr is the storage shape for derivedstats.DailyUsageBlock.
type DailyUsageBlockAttr struct {
	Kind              string   `dynamodbav:"kind"`
	Start             string   `dynamodbav:"start"`
	End               string   `dynamodbav:"end"`
	TotalKwh          float64  `dynamodbav:"totalKwh"`
	SolarKwh          *float64 `dynamodbav:"solarKwh,omitempty"`
	AverageKwhPerHour *float64 `dynamodbav:"averageKwhPerHour,omitempty"`
	PercentOfDay      int      `dynamodbav:"percentOfDay"`
	Status            string   `dynamodbav:"status"`
	BoundarySource    string   `dynamodbav:"boundarySource"`
}

// SocLowAttr is the storage shape for the day's lowest SOC reading. The
// timestamp is RFC3339 UTC at write time (one int64 → string conversion in
// the poller; readers re-publish the string as-is).
type SocLowAttr struct {
	Soc       float64 `dynamodbav:"soc"`
	Timestamp string  `dynamodbav:"timestamp"`
}

// PeakPeriodAttr is the storage shape for derivedstats.PeakPeriod.
type PeakPeriodAttr struct {
	Start    string  `dynamodbav:"start"`
	End      string  `dynamodbav:"end"`
	AvgLoadW float64 `dynamodbav:"avgLoadW"`
	EnergyWh float64 `dynamodbav:"energyWh"`
}

// DerivedStats bundles the four attributes the summarisation pass writes in a
// single UpdateItem call. Lives in the dynamo package (not poller) per
// Decision 9 — it is a storage-write argument, not a poller-only concept.
type DerivedStats struct {
	DailyUsage             *DailyUsageAttr
	SocLow                 *SocLowAttr
	PeakPeriods            []PeakPeriodAttr
	DerivedStatsComputedAt string

	// PeakGridImportKwh / PeakComputedAt carry the peak-from-readings result.
	// They have an independent lifecycle from the four fields above: the
	// summarisation pass may set only these (on a row that already has derived
	// stats) or only the four above (on a row whose readings fail the peak
	// usability gate). UpdateDailyEnergyDerived writes each group only when its
	// sentinel is non-empty.
	PeakGridImportKwh *float64
	PeakComputedAt    string

	// BandImports / BandsComputedAt carry the per-band split. Third group with
	// its own lifecycle, same contract as the peak pair above: written only
	// when BandsComputedAt is non-empty, and a nil BandImports with the
	// sentinel set records "attempted, unavailable" so the row is not retried
	// every hour.
	BandImports     []BandImportAttr
	BandsComputedAt string
}

// DailyPowerItem represents a row in the flux-daily-power table.
type DailyPowerItem struct {
	SysSn      string  `dynamodbav:"sysSn"`
	UploadTime string  `dynamodbav:"uploadTime"`
	Cbat       float64 `dynamodbav:"cbat"`
	Ppv        float64 `dynamodbav:"ppv"`
	Load       float64 `dynamodbav:"load"`
	FeedIn     float64 `dynamodbav:"feedIn"`
	GridCharge float64 `dynamodbav:"gridCharge"`
}

// SystemItem represents a row in the flux-system table.
type SystemItem struct {
	SysSn       string  `dynamodbav:"sysSn"`
	Cobat       float64 `dynamodbav:"cobat"`
	Mbat        string  `dynamodbav:"mbat"`
	Minv        string  `dynamodbav:"minv"`
	Popv        float64 `dynamodbav:"popv"`
	Poinv       float64 `dynamodbav:"poinv"`
	EmsStatus   string  `dynamodbav:"emsStatus"`
	LastUpdated string  `dynamodbav:"lastUpdated"`
}

// OffpeakItem represents a row in the flux-offpeak table.
type OffpeakItem struct {
	SysSn  string `dynamodbav:"sysSn"`
	Date   string `dynamodbav:"date"`
	Status string `dynamodbav:"status"` // "pending" or "complete"

	// Start snapshot
	StartEpv         float64 `dynamodbav:"startEpv"`
	StartEInput      float64 `dynamodbav:"startEInput"`
	StartEOutput     float64 `dynamodbav:"startEOutput"`
	StartECharge     float64 `dynamodbav:"startECharge"`
	StartEDischarge  float64 `dynamodbav:"startEDischarge"`
	StartEGridCharge float64 `dynamodbav:"startEGridCharge"`
	SocStart         float64 `dynamodbav:"socStart"`

	// End snapshot
	EndEpv         float64 `dynamodbav:"endEpv"`
	EndEInput      float64 `dynamodbav:"endEInput"`
	EndEOutput     float64 `dynamodbav:"endEOutput"`
	EndECharge     float64 `dynamodbav:"endECharge"`
	EndEDischarge  float64 `dynamodbav:"endEDischarge"`
	EndEGridCharge float64 `dynamodbav:"endEGridCharge"`
	SocEnd         float64 `dynamodbav:"socEnd"`

	// Computed deltas
	GridUsageKwh        float64 `dynamodbav:"gridUsageKwh"`
	SolarKwh            float64 `dynamodbav:"solarKwh"`
	BatteryChargeKwh    float64 `dynamodbav:"batteryChargeKwh"`
	BatteryDischargeKwh float64 `dynamodbav:"batteryDischargeKwh"`
	GridExportKwh       float64 `dynamodbav:"gridExportKwh"`
	BatteryDeltaPercent float64 `dynamodbav:"batteryDeltaPercent"`

	// Integration provenance (AC 5.4). Populated when the five deltas are
	// computed via derivedstats.IntegrateOffpeakDeltas (poller window-end
	// and backfill CLI). `omitempty` keeps pre-feature rows clean of zero-
	// valued fields and lets the marshalling round-trip past rows safely.
	// No API consumer reads these fields; they exist for operator diagnostics.
	IntegrationSampleCount  int    `dynamodbav:"integrationSampleCount,omitempty"`
	IntegrationSkippedPairs int    `dynamodbav:"integrationSkippedPairs,omitempty"`
	IntegratedAt            string `dynamodbav:"integratedAt,omitempty"` // RFC3339 UTC

	// WindowStart / WindowEnd snapshot the free window this row was
	// integrated under, so a later plan edit that moves the window is
	// detectable as a mismatch rather than silently repricing the day. Absent
	// on pre-feature rows — see Geometry.
	WindowStart string `dynamodbav:"windowStart,omitempty"` // HH:MM, Sydney local
	WindowEnd   string `dynamodbav:"windowEnd,omitempty"`
}

// offpeakLegacyWindowStart / offpeakLegacyWindowEnd is the window every row
// written before the geometry snapshot existed was computed under. It was the
// only configured window for that whole period, so substituting it for a
// missing snapshot is exact, not a guess.
const (
	offpeakLegacyWindowStart = "11:00"
	offpeakLegacyWindowEnd   = "14:00"
)

// Geometry returns the free window the row's deltas were computed over,
// substituting the pre-feature window when the row carries no snapshot.
func (o OffpeakItem) Geometry() (start, end string) {
	if o.WindowStart == "" || o.WindowEnd == "" {
		return offpeakLegacyWindowStart, offpeakLegacyWindowEnd
	}
	return o.WindowStart, o.WindowEnd
}

// Usable reports whether the row's deltas are a real measurement. A row
// integrated from readings but with no samples in the window is a zero-delta
// artifact, not a measured zero, so it cannot price a free band. Rows
// predating the integration path (no IntegratedAt) are snapshot deltas and
// remain usable.
func (o OffpeakItem) Usable() bool {
	return o.IntegratedAt == "" || o.IntegrationSampleCount > 0
}

// NewReadingItem transforms AlphaESS power data into a DynamoDB reading item.
func NewReadingItem(serial string, data *alphaess.PowerData, now time.Time) ReadingItem {
	return ReadingItem{
		SysSn:     serial,
		Timestamp: now.Unix(),
		Ppv:       data.Ppv,
		Pload:     data.Pload,
		Pbat:      data.Pbat,
		Pgrid:     data.Pgrid,
		Soc:       data.Soc,
		TTL:       now.Add(ttl30Days).Unix(),
	}
}

// NewReadingItemFromSnapshot derives a ReadingItem from a 5-minute AlphaESS
// PowerSnapshot (`getOneDayPowerBySn`). Used by the backfill tool to fill in
// synthetic readings when the live poll path lost data — e.g. the T-1274
// overnight outage where `getLastPowerData` returned null and the poller
// either skipped writes or stored zero rows.
//
// Power and grid fields are reconstructed using the same mapping
// `mapDailyPowerToPoints` uses for the past-date Day Detail fallback:
//   - soc = cbat
//   - pgrid = gridCharge - feedIn (positive = importing)
//   - pbat  = load - ppv - pgrid (positive = discharging)
//
// `now` is used for the 30-day TTL so backfilled rows get a fresh lease on
// life instead of inheriting the snapshot's age.
func NewReadingItemFromSnapshot(serial string, snap alphaess.PowerSnapshot, loc *time.Location, now time.Time) (ReadingItem, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", snap.UploadTime, loc)
	if err != nil {
		return ReadingItem{}, fmt.Errorf("parse uploadTime %q: %w", snap.UploadTime, err)
	}
	pgrid, pbat := alphaess.DerivePower(snap.Load, snap.Ppv, snap.GridCharge, snap.FeedIn)
	return ReadingItem{
		SysSn:     serial,
		Timestamp: t.Unix(),
		Ppv:       snap.Ppv,
		Pload:     snap.Load,
		Pbat:      pbat,
		Pgrid:     pgrid,
		Soc:       snap.Cbat,
		TTL:       now.Add(ttl30Days).Unix(),
	}, nil
}

// IsAllZeroReading reports whether every power/SoC field on a stored
// ReadingItem is exactly zero — the bogus pattern T-1274 introduced when
// AlphaESS returned null `getLastPowerData` payloads overnight. Used by the
// backfill tool to identify rows safe to delete.
//
// The "safe to delete" claim depends on the AlphaESS inverter being configured
// with a non-zero minimum-discharge floor (currently 5%, see `cutoffPercent`
// in internal/api/status.go and FluxCore/BatteryEnergy.swift). If that floor
// is ever lowered to 0%, a row taken at the moment the battery legitimately
// sat at SoC=0% AND every power channel happened to be 0 W would be deleted
// here. The decision_log of the late-night-current-data-gap bugfix captures
// the trade-off.
func IsAllZeroReading(r ReadingItem) bool {
	return r.Ppv == 0 && r.Pload == 0 && r.Pbat == 0 && r.Pgrid == 0 && r.Soc == 0
}

// NewDailyEnergyItem transforms AlphaESS energy data into a DynamoDB daily energy item.
func NewDailyEnergyItem(serial, date string, data *alphaess.EnergyData) DailyEnergyItem {
	return DailyEnergyItem{
		SysSn:       serial,
		Date:        date,
		Epv:         data.Epv,
		EInput:      data.EInput,
		EOutput:     data.EOutput,
		ECharge:     data.ECharge,
		EDischarge:  data.EDischarge,
		EGridCharge: data.EGridCharge,
	}
}

// NewDailyPowerItems transforms AlphaESS power snapshots into DynamoDB daily power items.
func NewDailyPowerItems(serial string, snapshots []alphaess.PowerSnapshot) []DailyPowerItem {
	items := make([]DailyPowerItem, len(snapshots))
	for i, s := range snapshots {
		items[i] = DailyPowerItem{
			SysSn:      serial,
			UploadTime: s.UploadTime,
			Cbat:       s.Cbat,
			Ppv:        s.Ppv,
			Load:       s.Load,
			FeedIn:     s.FeedIn,
			GridCharge: s.GridCharge,
		}
	}
	return items
}

// NewSystemItem transforms AlphaESS system info into a DynamoDB system item.
func NewSystemItem(info *alphaess.SystemInfo, now time.Time) SystemItem {
	return SystemItem{
		SysSn:       info.SysSn,
		Cobat:       info.Cobat,
		Mbat:        info.Mbat,
		Minv:        info.Minv,
		Popv:        info.Popv,
		Poinv:       info.Poinv,
		EmsStatus:   info.EmsStatus,
		LastUpdated: now.UTC().Format(time.RFC3339),
	}
}
