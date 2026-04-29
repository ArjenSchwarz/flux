package dynamo

import (
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
	TTL        int64   `dynamodbav:"ttl"`
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
func NewDailyPowerItems(serial string, snapshots []alphaess.PowerSnapshot, now time.Time) []DailyPowerItem {
	items := make([]DailyPowerItem, len(snapshots))
	ttl := now.Add(ttl30Days).Unix()
	for i, s := range snapshots {
		items[i] = DailyPowerItem{
			SysSn:      serial,
			UploadTime: s.UploadTime,
			Cbat:       s.Cbat,
			Ppv:        s.Ppv,
			Load:       s.Load,
			FeedIn:     s.FeedIn,
			GridCharge: s.GridCharge,
			TTL:        ttl,
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
