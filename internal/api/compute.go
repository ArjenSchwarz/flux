package api

import (
	"math"
	"sort"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
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

// offpeakWindow is the free window of the plan pricing one date, expressed as
// Sydney-local minutes of day.
//
// Callers hold it as a *offpeakWindow, where nil means the date has no free
// window — either its plan carries no free band or no plan prices it at all.
// Every window-dependent value is then absent rather than defaulted (AC 4.4);
// the pointer is what makes that state impossible to confuse with a real
// window.
type offpeakWindow struct {
	startMin, endMin int
}

// startHHMM / endHHMM render the window in the "HH:MM" form the derivedstats
// helpers still parse.
func (w offpeakWindow) startHHMM() string { return plan.FormatBandTime(w.startMin) }
func (w offpeakWindow) endHHMM() string   { return plan.FormatBandTime(w.endMin) }

// resolveOffpeakWindow returns the free window of the plan pricing date
// (AC 4.1), or nil when there is none.
func resolveOffpeakWindow(plans []plan.Plan, date string) *offpeakWindow {
	startMin, endMin, ok := plan.FreeWindow(plans, date)
	if !ok {
		return nil
	}
	return &offpeakWindow{startMin: startMin, endMin: endMin}
}

// hhmmBounds renders a window for the derivedstats helpers that take "HH:MM"
// strings. A nil window yields two empty strings, which those helpers already
// treat as "no off-peak window" and degrade accordingly.
func hhmmBounds(w *offpeakWindow) (start, end string) {
	if w == nil {
		return "", ""
	}
	return w.startHHMM(), w.endHHMM()
}

// bounds resolves the window to absolute Sydney-local instants on the day
// containing local.
//
// Boundaries come from plan.SegmentBounds, the same wall-clock resolution the
// poller's capture and liveBandImports use. Adding elapsed minutes to midnight
// instead would put the window an hour off on the two DST-transition days a
// year, so the free-window edge and the band edges beside it would disagree
// (Data Consistency).
func (w offpeakWindow) bounds(local time.Time) (start, end time.Time) {
	seg := plan.Segment{Start: w.startHHMM(), End: w.endHHMM()}
	startUnix, endUnix := plan.SegmentBounds(seg, local, sydneyTZ)
	return time.Unix(startUnix, 0).In(sydneyTZ), time.Unix(endUnix, 0).In(sydneyTZ)
}

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

// liveOffpeakDeltas integrates readings over [window start, min(now, window
// end)) for today's date in Sydney local time and returns the five energy
// deltas.
//
// Returns (_, false) when the day has no free window, when now is at or before
// the window start (AC 4.3 — pre-window behaviour), or when the readings slice
// does not contain enough usable samples to integrate (AC 1.6).
//
// Pure function: no state and no clock except the explicit now parameter. This
// is the determinism contract that backs AC 4.4's monotonicity guarantee.
func liveOffpeakDeltas(readings []dynamo.ReadingItem, now time.Time,
	window *offpeakWindow,
) (offpeakDeltaValues, bool) {
	if window == nil {
		return offpeakDeltaValues{}, false
	}
	local := now.In(sydneyTZ)
	opStart, opEnd := window.bounds(local)

	if !local.After(opStart) {
		return offpeakDeltaValues{}, false
	}
	endTime := opEnd
	if local.Before(opEnd) {
		endTime = local
	}

	// Trim the readings slice to just the bracketed window before the
	// derivedstats conversion. A /status request hands in ~8640 readings of
	// which only ~1080 sit inside the off-peak window; converting the rest
	// to []derivedstats.Reading is wasted work. The window slice keeps one
	// reading before opStart (for left-edge synthesis) and one reading at or
	// after endTime (for right-edge synthesis), so bracket integration in
	// IntegrateOffpeakDeltas observes the same boundary samples it would on
	// the full slice.
	windowed := sliceWindow(readings, opStart.Unix(), endTime.Unix())

	deltas, ok := derivedstats.IntegrateOffpeakDeltas(
		toDerivedReadings(windowed),
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

// livePeakGridImport integrates max(pgrid, 0) over today's two peak windows —
// the spans bracketing the off-peak window — each clamped to now:
//
//	morning = [00:00, min(now, opStart))
//	evening = [opEnd,  min(now, dayEnd))   (empty until now passes opEnd)
//
// It is the "peak so far today" complement of liveOffpeakDeltas, computed
// directly from readings (independent of reconcileEnergy) so the off-peak
// sampling artifact is never dumped onto the peak residual (T-1421).
//
// Returns (_, false) when the day has no free window or the morning window has
// too few usable samples to integrate — the same <2-point usability gate
// liveOffpeakDeltas uses. The evening window is additive-when-usable: before
// now passes opEnd, or when it is too sparse to integrate on its own, it
// contributes zero rather than failing the whole result. This is why
// derivedstats.IntegratePeakGridImportKwh cannot be reused here — it requires
// BOTH windows to pass the gate, which is wrong for the partial "today so far"
// case (the evening window is empty before opEnd).
//
// Pure function: no state and no clock except the explicit now parameter.
func livePeakGridImport(readings []dynamo.ReadingItem, now time.Time,
	window *offpeakWindow,
) (float64, bool) {
	if window == nil {
		return 0, false
	}
	local := now.In(sydneyTZ)
	dayStart := startOfDaySydney(local)
	opStart, opEnd := window.bounds(local)
	dayEnd := dayStart.AddDate(0, 0, 1)

	// Trim to today's readings so the result is identical regardless of whether
	// the caller's slice carries pre-midnight samples (/status reads a rolling
	// 24h window; /day and /history read today only). Without this the morning
	// window would synthesise a pre-midnight left edge for /status but not the
	// others, so the same instant could read differently across screens. This
	// matches computeTodayEnergy, which integrates from the first post-midnight
	// sample. Readings are sorted ascending (the DynamoDB sort-key guarantee).
	if i := sort.Search(len(readings), func(i int) bool {
		return readings[i].Timestamp >= dayStart.Unix()
	}); i > 0 {
		readings = readings[i:]
	}

	// Morning window [00:00, min(now, opStart)). The usability gate hangs off
	// this window: before there are two usable post-midnight samples there is
	// no peak to report.
	mornEnd := opStart
	if local.Before(opStart) {
		mornEnd = local
	}
	mornWin := sliceWindow(readings, dayStart.Unix(), mornEnd.Unix())
	morning, ok := derivedstats.IntegrateOffpeakDeltas(
		toDerivedReadings(mornWin), dayStart.Unix(), mornEnd.Unix(),
	)
	if !ok {
		return 0, false
	}
	peak := morning.GridImportKwh

	// Evening window [opEnd, min(now, dayEnd)). Only contributes once now is
	// past the off-peak window end; additive-when-usable so a brief sparse
	// patch right after opEnd does not discard the morning value.
	if local.After(opEnd) {
		evenEnd := dayEnd
		if local.Before(dayEnd) {
			evenEnd = local
		}
		evenWin := sliceWindow(readings, opEnd.Unix(), evenEnd.Unix())
		if evening, evOK := derivedstats.IntegrateOffpeakDeltas(
			toDerivedReadings(evenWin), opEnd.Unix(), evenEnd.Unix(),
		); evOK {
			peak += evening.GridImportKwh
		}
	}
	return peak, true
}

// liveBandImports integrates max(pgrid, 0) over each rated segment of the plan
// pricing today, each clamped to now. It is the live counterpart of the
// poller's day-close band capture, and the single source both /day and
// /history use — the two screens cannot disagree about today's split because
// they compute it exactly once, here (AC 3.4).
//
// A segment the clock has not reached yet contributes 0: the day is not over,
// and that band has genuinely imported nothing so far. A segment that HAS
// started but cannot be integrated makes the whole split unavailable rather
// than partially known, because a partially known split is what AC 3.6 defines
// as unavailable — pricing the unknown bands at zero would understate the day.
//
// Boundaries come from plan.SegmentBounds, so band membership follows local
// wall-clock time across a DST transition (AC 3.8).
//
// Returns (_, false) when the plan has no rated segments or any started
// segment fails the integrator's usability gate.
func liveBandImports(readings []dynamo.ReadingItem, now time.Time, p plan.Plan) ([]BandImport, bool) {
	rated := plan.RatedSegments(p)
	if len(rated) == 0 {
		return nil, false
	}
	local := now.In(sydneyTZ)

	// Trim to today's readings so the result does not depend on whether the
	// caller's slice carries pre-midnight samples — the same cross-screen
	// identity argument livePeakGridImport makes. Readings are sorted
	// ascending (the DynamoDB sort-key guarantee).
	dayStartUnix := startOfDaySydney(local).Unix()
	if i := sort.Search(len(readings), func(i int) bool {
		return readings[i].Timestamp >= dayStartUnix
	}); i > 0 {
		readings = readings[i:]
	}

	nowUnix := local.Unix()
	out := make([]BandImport, 0, len(rated))
	for _, seg := range rated {
		startUnix, endUnix := plan.SegmentBounds(seg, local, sydneyTZ)
		entry := BandImport{Start: seg.Start, End: seg.End}
		if nowUnix > startUnix {
			end := min(endUnix, nowUnix)
			windowed := sliceWindow(readings, startUnix, end)
			deltas, ok := derivedstats.IntegrateOffpeakDeltas(toDerivedReadings(windowed), startUnix, end)
			if !ok {
				return nil, false
			}
			entry.Kwh = derivedstats.RoundEnergy(deltas.GridImportKwh)
		}
		out = append(out, entry)
	}
	return out, true
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
	// Guard the int64 nanosecond conversion: a near-zero discharge rate yields
	// an astronomically large hoursRemaining whose conversion to time.Duration
	// would overflow and wrap to a garbage past time. Such a battery effectively
	// never empties, so report no cutoff.
	nanos := hoursRemaining * float64(time.Hour)
	if nanos >= float64(math.MaxInt64) {
		return nil
	}
	t := now.Add(time.Duration(nanos))
	return &t
}

// Off-peak charge-curve constants (Decision 3). Hardcoded like maxDischargeKW
// because Flux monitors a single, fixed battery system — these change only if
// the hardware changes. The tie-break at fastChargeMaxSoc is `>=` → trickle
// rate (Decision 7).
const (
	offpeakChargeRateKW  = 4.5  // grid charge rate while SoC < fastChargeMaxSoc
	offpeakTrickleRateKW = 0.5  // 500 W from fastChargeMaxSoc to 100%
	fastChargeMaxSoc     = 95.0 // percent
)

// projectOffpeakEndSoc projects the battery SoC (percent) at the off-peak
// window end using the idealised two-rate charge curve: offpeakChargeRateKW
// while SoC < fastChargeMaxSoc, then offpeakTrickleRateKW up to 100%.
//
// Returns nil when the day has no free window, now is outside [start, end), or
// capacity is non-positive. The result is clamped to [soc, 100] and rounded to
// 1 dp. The projection is a best-case figure independent of the live charge
// power: it never reads Pbat or the simulated load, so AC 1.9 and AC 2.4 hold
// by construction (Decision 4, Decision 6).
func projectOffpeakEndSoc(soc, capacityKwh float64, now time.Time, window *offpeakWindow) *float64 {
	if capacityKwh <= 0 || !withinOffpeakWindow(now, window) {
		return nil
	}

	// withinOffpeakWindow gates on minute-of-day, so now is always before the
	// window end here; h is a positive, seconds-precise duration absorbed by
	// the [soc, 100] clamp.
	_, windowEnd := window.bounds(now)
	h := windowEnd.Sub(now).Hours()

	// r converts a charge power (kW) to a SoC rate (percent per hour).
	r := func(kw float64) float64 { return kw / capacityKwh * 100 }

	var projected float64
	switch {
	case soc >= 100:
		projected = 100
	case soc >= fastChargeMaxSoc:
		projected = soc + r(offpeakTrickleRateKW)*h
	default:
		hoursTo95 := (fastChargeMaxSoc - soc) / r(offpeakChargeRateKW)
		if hoursTo95 >= h {
			projected = soc + r(offpeakChargeRateKW)*h
		} else {
			projected = fastChargeMaxSoc + r(offpeakTrickleRateKW)*(h-hoursTo95)
		}
	}

	projected = max(soc, min(100, projected))
	projected = roundPower(projected)
	return &projected
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
// handler's invariant) falls inside the free window [start, end). A day with
// no free window is never "within" one.
func withinOffpeakWindow(now time.Time, window *offpeakWindow) bool {
	if window == nil {
		return false
	}
	minuteOfDay := now.Hour()*60 + now.Minute()
	return minuteOfDay >= window.startMin && minuteOfDay < window.endMin
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
		Epv:        derivedstats.RoundEnergy(epvWh / 1000),
		EInput:     derivedstats.RoundEnergy(eInputWh / 1000),
		EOutput:    derivedstats.RoundEnergy(eOutputWh / 1000),
		ECharge:    derivedstats.RoundEnergy(eChargeWh / 1000),
		EDischarge: derivedstats.RoundEnergy(eDischargeWh / 1000),
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

// nextOffpeakStart returns the absolute Sydney-local time of the next free
// window start, used to suppress cutoff predictions that land at or after the
// next scheduled charging window. Today's start is returned whenever now is
// before today's end (including inside the window — during which any future
// cutoff is also >= start, so it is suppressed); tomorrow's start is returned
// once now has passed today's end.
//
// Each candidate window comes from the plan pricing the day that window falls
// on (Q11/AC 4.2): on the eve of a plan switch the successor's window is the
// one the battery will actually charge in, so anchoring to today's plan would
// suppress against a boundary that no longer exists. Returns (_, false) when
// neither day has a free window.
func nextOffpeakStart(now time.Time, plans []plan.Plan) (time.Time, bool) {
	local := now.In(sydneyTZ)
	if window := resolveOffpeakWindow(plans, local.Format("2006-01-02")); window != nil {
		todayStart, todayEnd := window.bounds(local)
		if local.Before(todayEnd) {
			return todayStart, true
		}
	}
	tomorrow := local.AddDate(0, 0, 1)
	window := resolveOffpeakWindow(plans, tomorrow.Format("2006-01-02"))
	if window == nil {
		return time.Time{}, false
	}
	start, _ := window.bounds(tomorrow)
	return start, true
}

// startOfDaySydney returns 00:00 on now's Sydney-local date, used as the
// lower bound for the "lowest SOC today" stat (see
// specs/low-since-offpeak/decision_log.md Decision 4).
func startOfDaySydney(now time.Time) time.Time {
	local := now.In(sydneyTZ)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, sydneyTZ)
}

// roundPower rounds a watts or SOC value to 1 decimal place.
func roundPower(v float64) float64 {
	return math.Round(v*10) / 10
}

// sliceWindow returns the contiguous sub-slice of readings spanning the
// off-peak window, plus one bracketing reading on each side when present.
// Readings must be sorted by Timestamp ascending (the DynamoDB sort-key
// guarantee for /status's today query). The returned slice aliases the input
// — callers must not mutate it.
//
// The conservative bracket extension (one reading before startUnix, one at or
// after endUnix) preserves derivedstats.IntegrateOffpeakDeltas's left and
// right edge-synthesis behaviour: it needs a neighbour outside the window to
// interpolate the boundary points.
func sliceWindow(readings []dynamo.ReadingItem, startUnix, endUnix int64) []dynamo.ReadingItem {
	if len(readings) == 0 {
		return readings
	}
	// First index with Timestamp >= startUnix.
	leftIn := sort.Search(len(readings), func(i int) bool {
		return readings[i].Timestamp >= startUnix
	})
	// First index with Timestamp > endUnix — anything strictly greater can
	// only contribute as a right-edge bracket of [start, end), so we keep
	// exactly one (the search returns the cut point; one slot beyond is
	// outside the half-open window and any further reading is irrelevant).
	rightOut := sort.Search(len(readings), func(i int) bool {
		return readings[i].Timestamp > endUnix
	})

	// Extend one reading to the left for left-edge bracket synthesis.
	if leftIn > 0 {
		leftIn--
	}
	// Extend one reading to the right for right-edge bracket synthesis.
	if rightOut < len(readings) {
		rightOut++
	}
	return readings[leftIn:rightOut]
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
