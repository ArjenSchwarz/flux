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
	// Negative Pload readings (corrupted data or net-export accounting) are
	// clamped to 0 to stay consistent with the energy integration in step 5.
	var sum float64
	var count int
	for i, r := range readings {
		if offpeakMask[i] {
			continue
		}
		sum += max(r.Pload, 0)
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
		// Filter on the rounded value: a period that displays as "0 Wh" is
		// noise, not a peak.
		rounded := math.Round(energyWh)
		if rounded == 0 {
			continue
		}

		results = append(results, ranked{
			period: PeakPeriod{
				Start:    time.Unix(readings[c.startIdx].Timestamp, 0).UTC().Format(time.RFC3339),
				End:      time.Unix(readings[c.endIdx].Timestamp, 0).UTC().Format(time.RFC3339),
				AvgLoadW: roundPower(c.sum / float64(c.count)),
				EnergyWh: rounded,
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

// nextOffpeakStart returns the absolute Sydney-local time of the next
// off-peak window start, used to suppress cutoff predictions that land at or
// after the next scheduled charging window. Today's start is returned
// whenever now is before today's end (including inside the window — during
// which any future cutoff is also >= start, so it is suppressed); tomorrow's
// start is returned once now has passed today's end. Returns (_, false) for
// an unparseable off-peak configuration.
func nextOffpeakStart(now time.Time, offpeakStart, offpeakEnd string) (time.Time, bool) {
	startMin, endMin, ok := parseOffpeakWindow(offpeakStart, offpeakEnd)
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
		if h > 23 || m > 59 {
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

// melbourneSunriseSunset returns the UTC instant of Melbourne sunrise (when
// isSunrise=true) or sunset (false) for the given calendar date in
// "YYYY-MM-DD" format. The result is truncated to whole seconds and is
// always in UTC.
//
// The implementation looks up the date's MM-DD in melbourneSunLocal (an
// embedded static table; see melbourne_sun_table.go). The table value is a
// wall-clock "HH:MM" string in Sydney-local time. Combining it with the
// requested calendar date via time.ParseInLocation in sydneyTZ yields the
// correct UTC instant for any year — Go's IANA database resolves AEDT vs
// AEST automatically.
//
// Feb 29 is intentionally absent from the table; the lookup falls back to
// Feb 28's values (well within the ±2 minute tolerance of req 1.12).
func melbourneSunriseSunset(date string, isSunrise bool) time.Time {
	dayStart, err := time.ParseInLocation("2006-01-02", date, sydneyTZ)
	if err != nil {
		// Defensive fallback. Caller validates date format before getting
		// here; if we somehow get a malformed date, returning the zero
		// time lets the buildDailyUsageBlock degenerate-omit guard drop
		// the block.
		return time.Time{}
	}
	key := date[5:10] // MM-DD; ParseInLocation guarantees len(date) == 10
	entry, ok := melbourneSunLocal[key]
	if !ok {
		// Feb 29 is the only intentional miss; reuse Feb 28's values.
		entry = melbourneSunLocal["02-28"]
	}
	hhmm := entry.setLocal
	if isSunrise {
		hhmm = entry.riseLocal
	}
	if len(hhmm) != 5 || hhmm[2] != ':' {
		// Corrupt static table entry: return the zero time so the
		// buildDailyUsageBlock degenerate-omit guard drops the block,
		// matching the parse-error path above.
		return time.Time{}
	}
	h := int(hhmm[0]-'0')*10 + int(hhmm[1]-'0')
	m := int(hhmm[3]-'0')*10 + int(hhmm[4]-'0')
	return dayStart.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute).UTC().Truncate(time.Second)
}

// integratePload returns the trapezoidal integral of max(pload, 0) over the
// half-open interval [startUnix, endUnix), expressed in kWh.
//
// Algorithm (full specification in specs/evening-night-stats/design.md):
//  1. Build a working point sequence pts. Synthesize a left/right edge by
//     linearly interpolating pload between the readings that bracket the
//     period boundary, with negative pload values clamped to zero before
//     interpolation. Skip edge synthesis when the bracketing pair has a gap
//     greater than 60 seconds.
//  2. Append every reading in [startUnix, endUnix) as an interior point
//     (clamped pload). A reading exactly at startUnix is interior; a reading
//     exactly at endUnix is excluded (half-open).
//  3. Sum trapezoidal areas across adjacent pairs in pts, applying the same
//     >60s skip used in computeTodayEnergy. Return watt-seconds / 3,600,000.
//
// The function does no rounding — callers round at serialization time.
//
// Precondition: readings must be sorted by Timestamp ascending. The bracket
// searches use first-match early-break and produce silently-wrong results on
// unsorted input. DynamoDB queries on the sort key satisfy this in production.
func integratePload(readings []dynamo.ReadingItem, startUnix, endUnix int64) float64 {
	if startUnix >= endUnix || len(readings) == 0 {
		return 0
	}

	// Find left bracket index: largest i with readings[i].Timestamp < startUnix.
	iL := -1
	for i, r := range readings {
		if r.Timestamp < startUnix {
			iL = i
		} else {
			break
		}
	}
	// Find right bracket index: smallest i > iL with readings[i].Timestamp >= endUnix.
	// Starting from iL+1 skips the prefix we already know is below startUnix.
	iR := len(readings)
	for i := iL + 1; i < len(readings); i++ {
		if readings[i].Timestamp >= endUnix {
			iR = i
			break
		}
	}

	type pt struct {
		ts    int64
		pload float64
	}
	pts := make([]pt, 0, (iR-iL-1)+2)

	// Left edge synthesis.
	if iL >= 0 && iL+1 < len(readings) {
		next := readings[iL+1]
		if next.Timestamp > startUnix {
			gap := next.Timestamp - readings[iL].Timestamp
			if gap <= maxPairGapSeconds {
				prev := readings[iL]
				p0 := max(prev.Pload, 0)
				p1 := max(next.Pload, 0)
				frac := float64(startUnix-prev.Timestamp) / float64(next.Timestamp-prev.Timestamp)
				pts = append(pts, pt{
					ts:    startUnix,
					pload: p0 + (p1-p0)*frac,
				})
			}
		}
		// next.Timestamp == startUnix → skip; the interior loop will pick up that reading.
	}

	// Interior readings.
	for i := iL + 1; i < iR; i++ {
		r := readings[i]
		if r.Timestamp < startUnix || r.Timestamp >= endUnix {
			continue
		}
		pts = append(pts, pt{ts: r.Timestamp, pload: max(r.Pload, 0)})
	}

	// Right edge synthesis. iR is the first index with Timestamp >= endUnix,
	// so readings[iR-1].Timestamp < endUnix is guaranteed.
	//
	// When iR-1 == iL (no interior readings), prev is the left-bracket reading
	// and gap spans the entire pre-period region. The 60s gap check then
	// conservatively skips synthesis even if the bracket pair around endUnix
	// is itself tight. Safe — energy is underestimated rather than fabricated —
	// and the all-readings-outside-period case is already covered upstream by
	// the len(pts) < 2 guard.
	if iR > 0 && iR < len(readings) {
		prev := readings[iR-1]
		next := readings[iR]
		gap := next.Timestamp - prev.Timestamp
		if gap <= maxPairGapSeconds {
			p0 := max(prev.Pload, 0)
			p1 := max(next.Pload, 0)
			frac := float64(endUnix-prev.Timestamp) / float64(next.Timestamp-prev.Timestamp)
			pts = append(pts, pt{
				ts:    endUnix,
				pload: p0 + (p1-p0)*frac,
			})
		}
	}

	if len(pts) < 2 {
		return 0
	}

	var wattSeconds float64
	for i := 1; i < len(pts); i++ {
		a := pts[i-1]
		b := pts[i]
		dt := b.ts - a.ts
		if dt <= 0 || dt > maxPairGapSeconds {
			continue
		}
		wattSeconds += (a.pload + b.pload) / 2 * float64(dt)
	}
	return wattSeconds / 3_600_000
}

// preSunriseBlipBuffer is the slack applied when filtering Ppv > 0 readings
// that could plausibly mark the start of solar production. Readings before
// sunrise - preSunriseBlipBuffer (or after sunset + preSunriseBlipBuffer) are
// treated as sensor noise — e.g. a stray Ppv > 0 reading at 01:30 should not
// end the night block at 01:30, and a post-sunset blip should not push the
// afternoonPeak into the evening hours. 30 minutes is generous enough to
// admit early-morning and late-evening production in twilight while rejecting
// middle-of-night blips by hours.
const preSunriseBlipBuffer = 30 * time.Minute

// recentSolarThreshold is the lookback window used by the today-gate to
// detect "solar still active". A reading inside [now - threshold, now] with
// Ppv > 0 keeps the gate firing and prevents the lastSolar boundary from
// flickering during the live afternoon window.
const recentSolarThreshold = 5 * time.Minute

// findDailyUsage breaks the requested calendar date into up to five
// chronological no-overlap blocks (night, morningPeak, offPeak,
// afternoonPeak, evening) and returns their totals. The off-peak window
// boundaries come from SSM; firstSolar and lastSolar boundaries come from
// readings (with Melbourne sunrise/sunset fallbacks).
//
// today is the caller's "today" string in sydneyTZ (date == today switches
// on the today-gate, future-omit, and in-progress clamping). now is the
// request-scoped clock. When the off-peak window is unparseable or the
// solar-window invariant fails, only night and evening are emitted. Returns
// nil when no blocks survive the pipeline.
func findDailyUsage(
	readings []dynamo.ReadingItem,
	offpeakStart, offpeakEnd string,
	date, today string,
	now time.Time,
) *DailyUsage {
	dayStart, err := time.ParseInLocation("2006-01-02", date, sydneyTZ)
	if err != nil {
		return nil
	}
	dayEnd := dayStart.AddDate(0, 0, 1)
	isToday := date == today

	computedSunrise := melbourneSunriseSunset(date, true)
	computedSunset := melbourneSunriseSunset(date, false)

	// Single pass over readings: track firstSolar/lastSolar inside the closed
	// window [computedSunrise - 30 min, computedSunset + 30 min] (decision 8
	// + decision 10), and (when isToday) recentSolar across [now - 5 min, now].
	lowerCutoff := computedSunrise.Add(-preSunriseBlipBuffer).Unix()
	upperCutoff := computedSunset.Add(preSunriseBlipBuffer).Unix()
	recentLower := now.Add(-recentSolarThreshold).Unix()
	recentUpper := now.Unix()
	var firstSolar, lastSolar *dynamo.ReadingItem
	recentSolar := false
	for i := range readings {
		r := readings[i]
		if r.Ppv > 0 && r.Timestamp >= lowerCutoff && r.Timestamp <= upperCutoff {
			if firstSolar == nil {
				rr := r
				firstSolar = &rr
			}
			rr := r
			lastSolar = &rr
		}
		if isToday && r.Ppv > 0 && r.Timestamp >= recentLower && r.Timestamp <= recentUpper {
			recentSolar = true
		}
	}
	hasQualifyingPpv := firstSolar != nil

	// solarStillUp drives both the today-gate (decision 9) and an early
	// override of lastSolar (decision 12): when solar is still active or
	// expected later today, the latest qualifying reading is not the day's
	// true "last solar" — it's just where we are now. Using the sunset
	// fallback for lastSolar in that case keeps the five-block layout
	// viable through step 4's strict invariant so the today-gate's
	// afternoonPeak/evening overrides have somewhere to land.
	solarStillUp := isToday && (recentSolar || (!hasQualifyingPpv && !now.After(computedSunset)))

	var firstSolarTS, lastSolarTS int64
	firstSolarFromFallback := false
	lastSolarFromFallback := false
	if firstSolar != nil {
		firstSolarTS = firstSolar.Timestamp
	} else {
		firstSolarTS = computedSunrise.Unix()
		firstSolarFromFallback = true
	}
	if lastSolar != nil && !solarStillUp {
		lastSolarTS = lastSolar.Timestamp
	} else {
		lastSolarTS = computedSunset.Unix()
		lastSolarFromFallback = true
	}

	// Solar-window guard. Parse off-peak first; failure → two-block path.
	offpeakStartMin, offpeakEndMin, offpeakOK := parseOffpeakWindow(offpeakStart, offpeakEnd)
	var offpeakStartTime, offpeakEndTime time.Time
	useFiveBlock := false
	if offpeakOK {
		offpeakStartTime = dayStart.Add(time.Duration(offpeakStartMin) * time.Minute)
		offpeakEndTime = dayStart.Add(time.Duration(offpeakEndMin) * time.Minute)
		offpeakStartTS := offpeakStartTime.Unix()
		offpeakEndTS := offpeakEndTime.Unix()
		// Strict invariant: firstSolarTS < offpeakStartTS < offpeakEndTS < lastSolarTS
		// (decision 7 + decision 11). offpeakStartTS < offpeakEndTS is guaranteed
		// by parseOffpeakWindow.
		if firstSolarTS < offpeakStartTS && offpeakEndTS < lastSolarTS {
			useFiveBlock = true
		}
	}

	// Build pendingBlocks per the resolved layout.
	var pending []pendingBlock
	if useFiveBlock {
		pending = []pendingBlock{
			{
				kind:           DailyUsageKindNight,
				start:          dayStart,
				end:            time.Unix(firstSolarTS, 0),
				startEstimated: false,
				endEstimated:   firstSolarFromFallback,
			},
			{
				kind:           DailyUsageKindMorningPeak,
				start:          time.Unix(firstSolarTS, 0),
				end:            offpeakStartTime,
				startEstimated: firstSolarFromFallback,
				endEstimated:   false,
			},
			{
				kind:           DailyUsageKindOffPeak,
				start:          offpeakStartTime,
				end:            offpeakEndTime,
				startEstimated: false,
				endEstimated:   false,
			},
			{
				kind:           DailyUsageKindAfternoonPeak,
				start:          offpeakEndTime,
				end:            time.Unix(lastSolarTS, 0),
				startEstimated: false,
				endEstimated:   lastSolarFromFallback,
			},
			{
				kind:           DailyUsageKindEvening,
				start:          time.Unix(lastSolarTS, 0),
				end:            dayEnd,
				startEstimated: lastSolarFromFallback,
				endEstimated:   false,
			},
		}
	} else {
		pending = []pendingBlock{
			{
				kind:           DailyUsageKindNight,
				start:          dayStart,
				end:            time.Unix(firstSolarTS, 0),
				startEstimated: false,
				endEstimated:   firstSolarFromFallback,
			},
			{
				kind:           DailyUsageKindEvening,
				start:          time.Unix(lastSolarTS, 0),
				end:            dayEnd,
				startEstimated: lastSolarFromFallback,
				endEstimated:   false,
			},
		}
	}

	// Today-gate (decision 9). solarStillUp computed above with the lastSolar
	// override. When fired: omit evening; afternoonPeak.end = now,
	// statusOverride = in-progress. On the two-block path the gate's
	// afternoonPeak override is a no-op (no such block exists); evening
	// omission still applies.
	if isToday {
		if solarStillUp {
			filtered := pending[:0]
			for _, p := range pending {
				if p.kind == DailyUsageKindEvening {
					continue
				}
				if p.kind == DailyUsageKindAfternoonPeak {
					p.end = now
					p.statusOverride = DailyUsageStatusInProgress
					// Today-gate clamp produces a requestTime end ⇒ readings.
					p.endEstimated = false
				}
				filtered = append(filtered, p)
			}
			pending = filtered
		}
	}

	// Future-omit + in-progress clamp.
	survivors := make([]pendingBlock, 0, len(pending))
	for _, p := range pending {
		if isToday && p.start.After(now) {
			continue
		}
		// Two paths set status because the today-gate (above) already clamped
		// afternoonPeak.end to now, so the generic p.end.After(now) branch
		// would not fire for that block — statusOverride carries the
		// in-progress signal across the gate.
		if isToday && p.end.After(now) && p.statusOverride == "" {
			p.end = now
			p.status = DailyUsageStatusInProgress
			// In-progress clamp produces a requestTime end ⇒ readings.
			p.endEstimated = false
		} else if p.statusOverride != "" {
			p.status = p.statusOverride
		} else {
			p.status = DailyUsageStatusComplete
		}
		survivors = append(survivors, p)
	}

	// Degenerate-omit.
	withDuration := make([]pendingBlock, 0, len(survivors))
	for _, p := range survivors {
		if p.start.Before(p.end) {
			withDuration = append(withDuration, p)
		}
	}

	if len(withDuration) == 0 {
		return nil
	}

	// Two-pass integration: per-block integratePload, then sum, then per-block
	// percentOfDay.
	var unroundedSum float64
	for i := range withDuration {
		withDuration[i].unroundedKwh = integratePload(readings, withDuration[i].start.Unix(), withDuration[i].end.Unix())
		unroundedSum += withDuration[i].unroundedKwh
	}

	blocks := make([]DailyUsageBlock, 0, len(withDuration))
	for _, p := range withDuration {
		blocks = append(blocks, buildDailyUsageBlock(p, unroundedSum))
	}
	return &DailyUsage{Blocks: blocks}
}

// pendingBlock carries the cross-pass state of one block between the
// pipeline steps and the two-pass integration. It is a local sentinel struct
// for findDailyUsage and never escapes the function.
type pendingBlock struct {
	kind           string
	start, end     time.Time
	startEstimated bool
	endEstimated   bool
	statusOverride string
	status         string
	unroundedKwh   float64
}

// buildDailyUsageBlock is a pure formatter: it computes boundarySource,
// formats start/end as RFC 3339 UTC, and computes averageKwhPerHour and
// percentOfDay from p.unroundedKwh and unroundedSum. It does not access the
// readings slice.
func buildDailyUsageBlock(p pendingBlock, unroundedSum float64) DailyUsageBlock {
	startUnix := p.start.Unix()
	endUnix := p.end.Unix()
	elapsed := endUnix - startUnix

	boundarySource := DailyUsageBoundaryReadings
	if p.startEstimated || p.endEstimated {
		boundarySource = DailyUsageBoundaryEstimated
	}

	block := DailyUsageBlock{
		Kind:           p.kind,
		Start:          p.start.UTC().Format(time.RFC3339),
		End:            p.end.UTC().Format(time.RFC3339),
		TotalKwh:       roundEnergy(p.unroundedKwh),
		Status:         p.status,
		BoundarySource: boundarySource,
	}
	if elapsed >= 60 {
		avg := roundEnergy(p.unroundedKwh / (float64(elapsed) / 3600.0))
		block.AverageKwhPerHour = &avg
	}
	if unroundedSum > 0 {
		block.PercentOfDay = int(math.Round(p.unroundedKwh / unroundedSum * 100))
	}
	return block
}

// roundEnergy rounds a kWh value to 2 decimal places.
func roundEnergy(v float64) float64 {
	return math.Round(v*100) / 100
}

// roundPower rounds a watts or SOC value to 1 decimal place.
func roundPower(v float64) float64 {
	return math.Round(v*10) / 10
}
