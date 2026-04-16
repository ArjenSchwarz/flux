package api

import (
	"cmp"
	"math"
	"slices"
	"time"

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

const (
	mergeGapSeconds   = 300 // max gap between clusters to merge (5 minutes)
	minPeriodSeconds  = 120 // minimum period duration (2 minutes)
	maxPairGapSeconds = 60  // max gap between reading pairs for energy integration
	maxPeakPeriods    = 3   // maximum number of peak periods to return
)

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

// findMinSOC scans readings for the minimum SOC value.
// Returns (soc, timestamp, found). found is false if readings is empty.
func findMinSOC(readings []dynamo.ReadingItem) (soc float64, timestamp int64, found bool) {
	if len(readings) == 0 {
		return 0, 0, false
	}
	minSoc := readings[0].Soc
	minTS := readings[0].Timestamp
	for _, r := range readings[1:] {
		if r.Soc < minSoc {
			minSoc = r.Soc
			minTS = r.Timestamp
		}
	}
	return minSoc, minTS, true
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

// findPeakPeriods identifies the top peak usage periods from raw readings.
// It returns up to maxPeakPeriods periods ranked by energy consumed, excluding
// readings within the off-peak window. Always returns a non-nil slice.
func findPeakPeriods(readings []dynamo.ReadingItem, offpeakStart, offpeakEnd string) []PeakPeriod {
	out := []PeakPeriod{}
	if len(readings) == 0 {
		return out
	}

	// Step 1: Parse off-peak window and precompute a mask so each reading only
	// incurs one timezone conversion (used in steps 2 and 3).
	offpeakStartMin, offpeakEndMin, hasOffpeak := parseOffpeakWindow(offpeakStart, offpeakEnd)
	offpeakMask := make([]bool, len(readings))
	if hasOffpeak {
		for i, r := range readings {
			offpeakMask[i] = isOffpeak(r.Timestamp, offpeakStartMin, offpeakEndMin)
		}
	}

	// Step 2: Compute mean Pload threshold from non-off-peak readings.
	var sum float64
	var count int
	for i, r := range readings {
		if offpeakMask[i] {
			continue
		}
		sum += r.Pload
		count++
	}
	if count == 0 {
		return out
	}
	threshold := sum / float64(count)

	// Step 3: Build initial clusters from above-threshold, non-off-peak readings.
	type cluster struct {
		startIdx, endIdx int
		sum              float64
		count            int
	}
	clusters := make([]cluster, 0, 16)
	var cur *cluster

	for i, r := range readings {
		if offpeakMask[i] || r.Pload <= threshold {
			if cur != nil {
				clusters = append(clusters, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			cur = &cluster{startIdx: i, endIdx: i, sum: r.Pload, count: 1}
		} else {
			cur.endIdx = i
			cur.sum += r.Pload
			cur.count++
		}
	}
	if cur != nil {
		clusters = append(clusters, *cur)
	}
	if len(clusters) == 0 {
		return out
	}

	// Step 4: Merge clusters within mergeGapSeconds, then discard short periods.
	merged := make([]cluster, 0, len(clusters))
	merged = append(merged, clusters[0])
	for _, c := range clusters[1:] {
		last := &merged[len(merged)-1]
		gap := readings[c.startIdx].Timestamp - readings[last.endIdx].Timestamp
		if gap <= mergeGapSeconds {
			last.endIdx = c.endIdx
			last.sum += c.sum
			last.count += c.count
		} else {
			merged = append(merged, c)
		}
	}

	// Filter by minimum duration.
	filtered := make([]cluster, 0, len(merged))
	for _, c := range merged {
		duration := readings[c.endIdx].Timestamp - readings[c.startIdx].Timestamp
		if duration >= minPeriodSeconds {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return out
	}

	// Step 5: Compute energy via trapezoidal integration, rank, and return top N.
	type ranked struct {
		period   PeakPeriod
		energyWh float64 // unrounded for sorting
	}
	results := make([]ranked, 0, len(filtered))

	for _, c := range filtered {
		var energyWh float64
		for j := c.startIdx + 1; j <= c.endIdx; j++ {
			prev := readings[j-1]
			curr := readings[j]
			dt := float64(curr.Timestamp - prev.Timestamp)
			if dt > maxPairGapSeconds {
				continue
			}
			energyWh += (max(prev.Pload, 0) + max(curr.Pload, 0)) / 2 * dt / 3600
		}
		if energyWh == 0 {
			continue
		}

		results = append(results, ranked{
			period: PeakPeriod{
				Start:    time.Unix(readings[c.startIdx].Timestamp, 0).UTC().Format(time.RFC3339),
				End:      time.Unix(readings[c.endIdx].Timestamp, 0).UTC().Format(time.RFC3339),
				AvgLoadW: roundPower(c.sum / float64(c.count)),
				EnergyWh: math.Round(energyWh),
			},
			energyWh: energyWh,
		})
	}

	slices.SortFunc(results, func(a, b ranked) int {
		return cmp.Compare(b.energyWh, a.energyWh)
	})

	n := min(len(results), maxPeakPeriods)
	out = make([]PeakPeriod, n)
	for i := range n {
		out[i] = results[i].period
	}
	return out
}

// parseOffpeakWindow parses "HH:MM" strings into minute-of-day values.
// Returns (start, end, true) on success, or (0, 0, false) if parsing fails
// or start >= end.
func parseOffpeakWindow(startStr, endStr string) (int, int, bool) {
	parse := func(s string) (int, bool) {
		if len(s) != 5 || s[2] != ':' {
			return 0, false
		}
		h := int(s[0]-'0')*10 + int(s[1]-'0')
		m := int(s[3]-'0')*10 + int(s[4]-'0')
		if h < 0 || h > 23 || m < 0 || m > 59 {
			return 0, false
		}
		return h*60 + m, true
	}
	startMin, ok1 := parse(startStr)
	endMin, ok2 := parse(endStr)
	if !ok1 || !ok2 || startMin >= endMin {
		return 0, 0, false
	}
	return startMin, endMin, true
}

// isOffpeak checks whether a Unix timestamp falls within the off-peak window
// (>= start AND < end) in Sydney local time.
func isOffpeak(ts int64, offpeakStartMin, offpeakEndMin int) bool {
	t := time.Unix(ts, 0).In(sydneyTZ)
	minuteOfDay := t.Hour()*60 + t.Minute()
	return minuteOfDay >= offpeakStartMin && minuteOfDay < offpeakEndMin
}

// roundEnergy rounds a kWh value to 2 decimal places.
func roundEnergy(v float64) float64 {
	return math.Round(v*100) / 100
}

// roundPower rounds a watts or SOC value to 1 decimal place.
func roundPower(v float64) float64 {
	return math.Round(v*10) / 10
}
