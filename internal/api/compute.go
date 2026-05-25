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

// offpeakDeltas resolves the energy deltas for a complete off-peak record.
// Pending records return (_, false); callers needing today's in-window value
// live-integrate via liveOffpeakDeltas.
//
// AC 5.3: this function does not read op.StartE* / op.EndE* — those fields
// exist on the row for operator diagnostics only.
func offpeakDeltas(op dynamo.OffpeakItem) (offpeakDeltaValues, bool) {
	if op.Status != dynamo.OffpeakStatusComplete {
		return offpeakDeltaValues{}, false
	}
	return offpeakDeltaValues{
		GridImport:       op.GridUsageKwh,
		Solar:            op.SolarKwh,
		BatteryCharge:    op.BatteryChargeKwh,
		BatteryDischarge: op.BatteryDischargeKwh,
		GridExport:       op.GridExportKwh,
	}, true
}

// offpeakDeltaValues holds the five energy deltas derived from an off-peak record.
type offpeakDeltaValues struct {
	GridImport       float64
	Solar            float64
	BatteryCharge    float64
	BatteryDischarge float64
	GridExport       float64
}

// liveOffpeakDeltas integrates readings over [offpeakStart, min(now, offpeakEnd))
// for today's date in Sydney local time and returns the five energy deltas.
//
// offpeakStart and offpeakEnd are the raw "HH:MM" config values. Returns
// (_, false) when the window is unparseable, when now is at or before the
// window start (AC 4.3 — pre-window behaviour), or when the readings slice
// does not contain enough usable samples to integrate (AC 1.6).
//
// Pure function: no state and no clock except the explicit now parameter. This
// is the determinism contract that backs AC 4.4's monotonicity guarantee.
func liveOffpeakDeltas(readings []dynamo.ReadingItem, now time.Time,
	offpeakStart, offpeakEnd string,
) (offpeakDeltaValues, bool) {
	startMin, endMin, parsed := derivedstats.ParseOffpeakWindow(offpeakStart, offpeakEnd)
	if !parsed {
		return offpeakDeltaValues{}, false
	}
	local := now.In(sydneyTZ)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, sydneyTZ)
	opStart := dayStart.Add(time.Duration(startMin) * time.Minute)
	opEnd := dayStart.Add(time.Duration(endMin) * time.Minute)

	if !local.After(opStart) {
		return offpeakDeltaValues{}, false
	}
	endTime := opEnd
	if local.Before(opEnd) {
		endTime = local
	}

	deltas, ok := derivedstats.IntegrateOffpeakDeltas(
		toDerivedReadings(readings),
		opStart.Unix(),
		endTime.Unix(),
	)
	if !ok {
		return offpeakDeltaValues{}, false
	}
	return offpeakDeltaValues{
		GridImport:       deltas.GridImportKwh,
		Solar:            deltas.SolarKwh,
		BatteryCharge:    deltas.BatteryChargeKwh,
		BatteryDischarge: deltas.BatteryDischargeKwh,
		GridExport:       deltas.GridExportKwh,
	}, true
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

// cantEmptyInput bundles the inputs to computeCantEmptyBeforeOffpeak.
//
// Soc is the latest battery SOC (percent). CapacityKwh is the configured
// battery capacity. Now and NextOpStart are absolute Sydney-local times.
// HasBoundary mirrors nextOffpeakStart's ok return — false when the off-peak
// window is unparseable. WithinOffpeakWindow is true when Now falls inside
// the configured window (any future cutoff during a charging window is
// meaningless).
type cantEmptyInput struct {
	Soc, CapacityKwh    float64
	Now, NextOpStart    time.Time
	HasBoundary         bool
	WithinOffpeakWindow bool
}

// computeCantEmptyBeforeOffpeak returns &true when, at the constant
// maxDischargeKW ceiling, the battery cannot reach cutoffPercent before
// NextOpStart. Returns nil when the question is meaningless (no off-peak
// boundary, window currently active, SOC already at/below cutoff, or
// non-positive capacity) or when the battery can in fact empty in time.
// The comparison is strict (After), so Now+requiredHours == NextOpStart
// returns nil — see requirements AC 2.4 (boundary equality).
func computeCantEmptyBeforeOffpeak(in cantEmptyInput) *bool {
	if !in.HasBoundary || in.WithinOffpeakWindow || in.Soc <= cutoffPercent || in.CapacityKwh <= 0 {
		return nil
	}
	remainingKwh := (in.Soc - cutoffPercent) / 100 * in.CapacityKwh
	requiredHours := remainingKwh / maxDischargeKW
	if in.Now.Add(time.Duration(requiredHours * float64(time.Hour))).After(in.NextOpStart) {
		t := true
		return &t
	}
	return nil
}

// withinOffpeakWindow reports whether now (in Sydney local time per the
// handler's invariant) falls inside the off-peak window [start, end).
// Parsing is delegated to derivedstats.ParseOffpeakWindow so this stays a
// single source of truth — unparseable inputs return false rather than
// raising an error (consistent with how cutoff-time suppression degrades).
func withinOffpeakWindow(now time.Time, offpeakStart, offpeakEnd string) bool {
	startMin, endMin, ok := derivedstats.ParseOffpeakWindow(offpeakStart, offpeakEnd)
	if !ok {
		return false
	}
	minuteOfDay := now.Hour()*60 + now.Minute()
	return minuteOfDay >= startMin && minuteOfDay < endMin
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

// startOfDaySydney returns 00:00 on now's Sydney-local date, used as the
// lower bound for the "lowest SOC today" stat (see
// specs/low-since-offpeak/decision_log.md Decision 4).
func startOfDaySydney(now time.Time) time.Time {
	local := now.In(sydneyTZ)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, sydneyTZ)
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
