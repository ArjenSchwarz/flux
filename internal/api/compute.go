package api

import (
	"math"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// sydneyTZ is the Australia/Sydney timezone used for all date-based operations.
// Loaded once at package init to avoid repeated lookups and silent error discarding.
var sydneyTZ = func() *time.Location {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		panic("failed to load Australia/Sydney timezone: " + err.Error())
	}
	return loc
}()

// offpeakDeltas resolves the energy deltas for an off-peak record.
//
// A complete record carries final deltas from the poller. A pending record
// requires a current snapshot (the running totals for the same day as op)
// to project against the start snapshot; without one the deltas are
// unknown. Returns ok=false when the data is not usable.
func offpeakDeltas(op dynamo.OffpeakItem, current *TodayEnergy) (deltas offpeakDeltaValues, ok bool) {
	switch op.Status {
	case dynamo.OffpeakStatusComplete:
		return offpeakDeltaValues{
			GridImport:       op.GridUsageKwh,
			Solar:            op.SolarKwh,
			BatteryCharge:    op.BatteryChargeKwh,
			BatteryDischarge: op.BatteryDischargeKwh,
			GridExport:       op.GridExportKwh,
		}, true
	case dynamo.OffpeakStatusPending:
		if current == nil {
			return offpeakDeltaValues{}, false
		}
		// Energy counters are monotonically non-decreasing, so deltas
		// should never be negative. They can briefly appear negative if
		// the running snapshot lags the start snapshot (poller writes the
		// start record, then a later reconciliation reduces the running
		// total). Clamp to zero so the dashboard never shows nonsense
		// like "-0.1 kWh imported".
		return offpeakDeltaValues{
			GridImport:       max(0, current.EInput-op.StartEInput),
			Solar:            max(0, current.Epv-op.StartEpv),
			BatteryCharge:    max(0, current.ECharge-op.StartECharge),
			BatteryDischarge: max(0, current.EDischarge-op.StartEDischarge),
			GridExport:       max(0, current.EOutput-op.StartEOutput),
		}, true
	}
	return offpeakDeltaValues{}, false
}

// offpeakDeltaValues holds the five energy deltas derived from an off-peak record.
type offpeakDeltaValues struct {
	GridImport       float64
	Solar            float64
	BatteryCharge    float64
	BatteryDischarge float64
	GridExport       float64
}

// computeCutoffTime estimates when the battery will reach the cutoff percentage
// using linear extrapolation. Returns nil if the battery is not discharging or
// SOC is already at/below cutoff.
func computeCutoffTime(soc, pbat, capacityKwh, cutoffPercent float64, now time.Time) *time.Time {
	if pbat <= 0 || soc <= cutoffPercent || capacityKwh <= 0 {
		return nil
	}
	remainingKwh := (soc - cutoffPercent) / 100 * capacityKwh
	hoursRemaining := remainingKwh / (pbat / 1000)
	if hoursRemaining <= 0 || math.IsNaN(hoursRemaining) || math.IsInf(hoursRemaining, 0) {
		return nil
	}
	t := now.Add(time.Duration(hoursRemaining * float64(time.Hour)))
	return &t
}

// computeRollingAverages returns the mean pload and pbat over the given readings.
// Returns (0, 0) for an empty slice.
func computeRollingAverages(readings []dynamo.ReadingItem) (avgLoad, avgPbat float64) {
	if len(readings) == 0 {
		return 0, 0
	}
	var sumLoad, sumPbat float64
	for _, r := range readings {
		sumLoad += r.Pload
		sumPbat += r.Pbat
	}
	n := float64(len(readings))
	return sumLoad / n, sumPbat / n
}

// computePgridSustained checks whether grid import is currently sustained.
// It iterates backwards from the most recent reading and counts consecutive
// readings where pgrid > 500 with each pair no more than 30 seconds apart.
// Returns true if 3+ consecutive qualifying readings are found.
// The function expects readings in ascending timestamp order.
func computePgridSustained(readings []dynamo.ReadingItem) bool {
	if len(readings) < 3 {
		return false
	}

	consecutive := 1
	for i := len(readings) - 1; i > 0; i-- {
		curr := readings[i]
		prev := readings[i-1]

		if curr.Pgrid <= 500 {
			break
		}
		if prev.Pgrid <= 500 {
			break
		}
		if curr.Timestamp-prev.Timestamp > 30 {
			break
		}
		consecutive++
		if consecutive >= 3 {
			return true
		}
	}
	return false
}

// bucketsPerDay is the number of 5-minute buckets in a day (288).
const bucketsPerDay = 288

// downsample divides a day into 5-minute buckets and averages all readings
// within each bucket. Empty buckets are omitted. The date parameter is in
// YYYY-MM-DD format and is interpreted in Australia/Sydney timezone.
func downsample(readings []dynamo.ReadingItem, date string) []TimeSeriesPoint {
	if len(readings) == 0 {
		return nil
	}

	dayStart, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)

	type bucket struct {
		ppv, pload, pbat, pgrid, soc float64
		count                        int
	}
	buckets := make([]bucket, bucketsPerDay)

	for _, r := range readings {
		t := time.Unix(r.Timestamp, 0).In(sydneyTZ)
		minuteOfDay := t.Hour()*60 + t.Minute()
		idx := minuteOfDay / 5
		if idx >= bucketsPerDay {
			idx = bucketsPerDay - 1
		}
		b := &buckets[idx]
		b.ppv += r.Ppv
		b.pload += r.Pload
		b.pbat += r.Pbat
		b.pgrid += r.Pgrid
		b.soc += r.Soc
		b.count++
	}

	var points []TimeSeriesPoint
	// Buckets are iterated 0..287, so points are already in chronological order.
	for i, b := range buckets {
		if b.count == 0 {
			continue
		}
		n := float64(b.count)
		bucketTime := dayStart.Add(time.Duration(i*5) * time.Minute)
		points = append(points, TimeSeriesPoint{
			Timestamp: bucketTime.UTC().Format(time.RFC3339),
			Ppv:       b.ppv / n,
			Pload:     b.pload / n,
			Pbat:      b.pbat / n,
			Pgrid:     b.pgrid / n,
			Soc:       b.soc / n,
		})
	}

	return points
}

func computeTodayEnergy(readings []dynamo.ReadingItem, midnightUnix int64) *TodayEnergy {
	var filtered []dynamo.ReadingItem
	for _, r := range readings {
		if r.Timestamp >= midnightUnix {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) < 2 {
		return nil
	}

	var epvWh, eInputWh, eOutputWh, eChargeWh, eDischargeWh float64

	for i := 1; i < len(filtered); i++ {
		prev := filtered[i-1]
		curr := filtered[i]

		dt := float64(curr.Timestamp - prev.Timestamp)
		// Skip pairs with gaps longer than ~6 poll intervals (10s each).
		// This prevents phantom energy accumulation during polling outages.
		if dt > 60 {
			continue
		}

		epvWh += ((max(prev.Ppv, 0) + max(curr.Ppv, 0)) / 2) * dt / 3600
		eInputWh += ((max(prev.Pgrid, 0) + max(curr.Pgrid, 0)) / 2) * dt / 3600
		eOutputWh += ((max(-prev.Pgrid, 0) + max(-curr.Pgrid, 0)) / 2) * dt / 3600
		eChargeWh += ((max(-prev.Pbat, 0) + max(-curr.Pbat, 0)) / 2) * dt / 3600
		eDischargeWh += ((max(prev.Pbat, 0) + max(curr.Pbat, 0)) / 2) * dt / 3600
	}

	return &TodayEnergy{
		Epv:        roundEnergy(epvWh / 1000),
		EInput:     roundEnergy(eInputWh / 1000),
		EOutput:    roundEnergy(eOutputWh / 1000),
		ECharge:    roundEnergy(eChargeWh / 1000),
		EDischarge: roundEnergy(eDischargeWh / 1000),
	}
}

func reconcileEnergy(computed *TodayEnergy, stored *TodayEnergy) *TodayEnergy {
	if computed == nil && stored == nil {
		return nil
	}
	// When one side is nil, return the other pointer directly. This aliases
	// the caller's input, which is safe because the result is only serialised
	// to JSON and never mutated after assignment.
	if computed == nil {
		return stored
	}
	if stored == nil {
		return computed
	}
	return &TodayEnergy{
		Epv:        max(computed.Epv, stored.Epv),
		EInput:     max(computed.EInput, stored.EInput),
		EOutput:    max(computed.EOutput, stored.EOutput),
		ECharge:    max(computed.ECharge, stored.ECharge),
		EDischarge: max(computed.EDischarge, stored.EDischarge),
	}
}

// nextOffpeakStart returns the absolute Sydney-local time of the next
// off-peak window start, used to suppress cutoff predictions that land at or
// after the next scheduled charging window. Today's start is returned
// whenever now is before today's end (including inside the window — during
// which any future cutoff is also >= start, so it is suppressed); tomorrow's
// start is returned once now has passed today's end. Returns (_, false) for
// an unparseable off-peak configuration.
func nextOffpeakStart(now time.Time, offpeakStart, offpeakEnd string) (time.Time, bool) {
	startMin, endMin, ok := derivedstats.ParseOffpeakWindow(offpeakStart, offpeakEnd)
	if !ok {
		return time.Time{}, false
	}
	local := now.In(sydneyTZ)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, sydneyTZ)
	todayStart := dayStart.Add(time.Duration(startMin) * time.Minute)
	todayEnd := dayStart.Add(time.Duration(endMin) * time.Minute)
	if !local.Before(todayEnd) {
		return todayStart.AddDate(0, 0, 1), true
	}
	return todayStart, true
}

// roundEnergy rounds a kWh value to 2 decimal places.
func roundEnergy(v float64) float64 {
	return math.Round(v*100) / 100
}

// roundPower rounds a watts or SOC value to 1 decimal place.
func roundPower(v float64) float64 {
	return math.Round(v*10) / 10
}

// toDerivedReadings converts a slice of dynamo.ReadingItem to the leaf
// []derivedstats.Reading. Per Decision 9 this conversion is duplicated at
// each call site (api, poller) to keep the derivedstats package free of
// upward imports into dynamo.
func toDerivedReadings(in []dynamo.ReadingItem) []derivedstats.Reading {
	out := make([]derivedstats.Reading, len(in))
	for i, r := range in {
		out[i] = derivedstats.Reading{
			Timestamp: r.Timestamp,
			Ppv:       r.Ppv,
			Pload:     r.Pload,
			Soc:       r.Soc,
			Pbat:      r.Pbat,
			Pgrid:     r.Pgrid,
		}
	}
	return out
}
