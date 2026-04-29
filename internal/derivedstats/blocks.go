package derivedstats

import (
	"math"
	"time"
)

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

// Blocks breaks the requested calendar date into up to five chronological
// no-overlap blocks (night, morningPeak, offPeak, afternoonPeak, evening) and
// returns their totals. The off-peak window boundaries come from SSM;
// firstSolar and lastSolar boundaries come from readings (with Melbourne
// sunrise/sunset fallbacks).
//
// today is the caller's "today" string in sydneyTZ (date == today switches
// on the today-gate, future-omit, and in-progress clamping). now is the
// request-scoped clock. When the off-peak window is unparseable or the
// solar-window invariant fails, only night and evening are emitted. Returns
// nil when no blocks survive the pipeline.
func Blocks(
	readings []Reading,
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
	var firstSolar, lastSolar *Reading
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
	offpeakStartMin, offpeakEndMin, offpeakOK := ParseOffpeakWindow(offpeakStart, offpeakEnd)
	var offpeakStartTime, offpeakEndTime time.Time
	useFiveBlock := false
	if offpeakOK {
		offpeakStartTime = dayStart.Add(time.Duration(offpeakStartMin) * time.Minute)
		offpeakEndTime = dayStart.Add(time.Duration(offpeakEndMin) * time.Minute)
		offpeakStartTS := offpeakStartTime.Unix()
		offpeakEndTS := offpeakEndTime.Unix()
		// Strict invariant: firstSolarTS < offpeakStartTS < offpeakEndTS < lastSolarTS
		// (decision 7 + decision 11). offpeakStartTS < offpeakEndTS is guaranteed
		// by ParseOffpeakWindow.
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
// for Blocks and never escapes the function.
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
