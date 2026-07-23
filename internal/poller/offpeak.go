package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
)

const (
	snapshotRetries   = 3
	defaultRetryDelay = 10 * time.Second

	// defaultEndWaitBudget is the maximum time handleEnd waits after
	// offpeak-end for a reading with timestamp >= offpeak-end to land
	// (specs/offpeak-from-readings AC 3.1). The 10 s live-poll cadence means
	// the first at-or-after-boundary reading typically arrives within 5–15 s.
	defaultEndWaitBudget = 30 * time.Second

	// defaultEndWaitPollInterval is the cadence at which handleEnd probes
	// the readings table while waiting for an at-or-after-boundary reading.
	// Two seconds keeps the probe lightweight against the live-poll cadence.
	defaultEndWaitPollInterval = 2 * time.Second

	// defaultPlanRetryInterval is how long the scheduler waits before
	// re-attempting a day whose plan could not be read. Retrying within the
	// day (rather than writing it off until midnight) is what lets a late
	// recovery still finalise the window from readings (Q36); the interval is
	// long enough that a sustained outage does not spin.
	defaultPlanRetryInterval = 15 * time.Minute
)

// windowPosition represents the poller's position relative to the off-peak window.
type windowPosition string

const (
	positionBefore windowPosition = "before"
	positionDuring windowPosition = "during"
	positionAfter  windowPosition = "after"
)

// offpeakWindow is one day's resolved free window: the absolute local
// boundaries the integration runs over, plus the HH:MM geometry snapshotted
// onto the row so a later plan edit shows up as a mismatch instead of
// silently repricing the day (Q23/Q31).
type offpeakWindow struct {
	Start, End time.Time
	StartHHMM  string
	EndHHMM    string
}

// OffpeakScheduler manages off-peak window state and snapshot capture.
type OffpeakScheduler struct {
	client APIClient
	store  dynamo.Store
	cfg    *config.Config
	plans  *PlanSource

	// In-memory state for current day's off-peak calculation.
	startSnapshot *alphaess.EnergyData
	socStart      float64

	// retryDelay between snapshot attempts (overridable for tests).
	retryDelay time.Duration

	// planRetryInterval is the wait before re-attempting a day whose plan
	// could not be read. Zero means defaultPlanRetryInterval.
	planRetryInterval time.Duration

	// endWaitBudget / endWaitPollInterval control the boundary wait in
	// handleEnd (specs/offpeak-from-readings AC 3.1). Zero means "use
	// default" (defaultEndWaitBudget / defaultEndWaitPollInterval). Tests
	// override these to keep wall time short.
	endWaitBudget       time.Duration
	endWaitPollInterval time.Duration

	// now returns the current time. Injectable for deterministic testing.
	now func() time.Time
}

// NewOffpeakScheduler creates an OffpeakScheduler with the given dependencies.
func NewOffpeakScheduler(client APIClient, store dynamo.Store, plans *PlanSource, cfg *config.Config) *OffpeakScheduler {
	return &OffpeakScheduler{
		client:            client,
		store:             store,
		cfg:               cfg,
		plans:             plans,
		retryDelay:        defaultRetryDelay,
		planRetryInterval: defaultPlanRetryInterval,
		now:               time.Now,
	}
}

// Run processes one free-window per local day, anchored to midnight (Q27):
// each cycle resolves the window from the plan pricing that day, handles
// whichever boundaries are still ahead, then sleeps to the next midnight.
//
// Anchoring to midnight is what makes plan succession automatic. Plans change
// behaviour only at midnight boundaries (AC 2.2), so refreshing once per day
// is exactly sufficient, and the switch day picks up the successor's window
// with no manual reconfiguration. A same-day edit to today's window is not
// picked up until tomorrow; the backfill CLI is the repair path for that.
func (o *OffpeakScheduler) Run(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		o.resetState()
		now := o.now().In(o.cfg.Location)
		date := now.Format(dateLayout)

		win, ok, err := o.resolveWindow(drainCtx, now, date)
		switch {
		case err != nil:
			// Plan data is unreadable, which is transient by definition
			// (AC 4.6). Retrying within the day rather than writing it off
			// means a load that recovers before midnight still finalises the
			// window from readings (Q36).
			slog.Error("offpeak: plan resolution failed; retrying later today",
				"date", date, "error", err)
			if !o.waitFor(loopCtx, o.planRetry()) {
				return
			}
			continue
		case !ok:
			// No plan prices the day, or its plan has no free band: there is
			// no window to process, and nothing is written (AC 4.4).
			slog.Info("offpeak: no free window for date, sleeping to next midnight", "date", date)
		default:
			if !o.runWindow(loopCtx, drainCtx, date, win) {
				return
			}
		}

		if !o.waitUntil(loopCtx, nextLocalMidnight(o.now().In(o.cfg.Location))) {
			return
		}
	}
}

// runWindow processes the boundaries of one day's free window that are still
// ahead of the clock. Returns false when the loop context was cancelled.
func (o *OffpeakScheduler) runWindow(loopCtx, drainCtx context.Context, date string, win offpeakWindow) bool {
	pos := positionFor(o.now().In(o.cfg.Location), win)
	slog.Debug("offpeak day cycle", "date", date, "position", pos,
		"window", win.StartHHMM+"-"+win.EndHHMM)

	switch pos {
	case positionBefore:
		if !o.waitUntil(loopCtx, win.Start) {
			return false
		}
		if err := o.handleStart(drainCtx, date); err != nil {
			// The start snapshot is diagnostics-only since T-1341, so losing
			// it costs forensics, not the day — handleEnd integrates the
			// readings regardless (Q36).
			slog.Error("offpeak start failed; will still finalise from readings at window end",
				"date", date, "error", err)
		}
		if !o.waitUntil(loopCtx, win.End) {
			return false
		}
		o.handleEndOrCleanup(drainCtx, date, nil, win)

	case positionDuring:
		// recoverMidWindow surfaces the pending row when one exists so its
		// StartE* fields populate the diagnostic snapshot. Its absence no
		// longer forfeits the day (Q36); WriteOffpeakIfPendingOrAbsent is
		// what guards against overwriting a row a peer already finalised.
		pending := o.recoverMidWindow(drainCtx, date)
		if !o.waitUntil(loopCtx, win.End) {
			return false
		}
		o.handleEndOrCleanup(drainCtx, date, pending, win)

	case positionAfter:
		// Restart between window-end and midnight: finalise now rather than
		// waiting a day. recoverAfterWindow follows the scheduler's
		// log-and-continue convention; failures are logged inside.
		o.recoverAfterWindow(drainCtx, date, win)
	}
	return true
}

// resolveWindow resolves the free window of the plan pricing date (AC 4.1)
// into absolute local boundaries on the given day.
//
// The three outcomes are deliberately distinct: (window, true, nil) when a
// plan with a free band covers the date; (_, false, nil) when no plan covers
// it or its plan has no free band — both semantic absences the caller treats
// as "nothing to process"; and a non-nil error when the plan data could not
// be read, which AC 4.6 forbids collapsing into "no plan".
func (o *OffpeakScheduler) resolveWindow(ctx context.Context, day time.Time, date string) (offpeakWindow, bool, error) {
	plans, err := o.plans.Plans(ctx)
	if err != nil {
		return offpeakWindow{}, false, fmt.Errorf("resolve free window for %s: %w", date, err)
	}
	startMin, endMin, ok := plan.FreeWindow(plans, date)
	if !ok {
		return offpeakWindow{}, false, nil
	}
	return offpeakWindow{
		Start:     wallClockTime(day, o.cfg.Location, time.Duration(startMin)*time.Minute),
		End:       wallClockTime(day, o.cfg.Location, time.Duration(endMin)*time.Minute),
		StartHHMM: plan.FormatBandTime(startMin),
		EndHHMM:   plan.FormatBandTime(endMin),
	}, true, nil
}

// planRetry returns the configured plan-retry wait, substituting the default
// for the zero value so a hand-constructed scheduler cannot busy-loop.
func (o *OffpeakScheduler) planRetry() time.Duration {
	if o.planRetryInterval <= 0 {
		return defaultPlanRetryInterval
	}
	return o.planRetryInterval
}

// handleStart captures the start snapshot and writes a pending record.
func (o *OffpeakScheduler) handleStart(ctx context.Context, date string) error {
	energy, soc, err := o.captureSnapshot(ctx, date)
	if err != nil {
		return fmt.Errorf("capture start snapshot: %w", err)
	}

	o.startSnapshot = energy
	o.socStart = soc

	item := dynamo.OffpeakItem{
		SysSn: o.cfg.Serial, Date: date, Status: dynamo.OffpeakStatusPending,
		StartEpv: energy.Epv, StartEInput: energy.EInput, StartEOutput: energy.EOutput,
		StartECharge: energy.ECharge, StartEDischarge: energy.EDischarge, StartEGridCharge: energy.EGridCharge,
		SocStart: soc,
	}
	if err := o.store.WriteOffpeak(ctx, item); err != nil {
		return fmt.Errorf("write pending offpeak: %w", err)
	}

	slog.Info("offpeak start captured", "date", date, "soc", soc)
	return nil
}

// offpeakStart is the diagnostic start-of-window snapshot. It is a pointer
// at every call site because it can legitimately be absent: a plan-read
// failure at window start means handleStart never ran, and a restart past
// the boundary may find no pending row. The readings integration never
// needed it (offpeak-from-readings Decision 2), so its absence costs
// forensics, not the day (Q36).
type offpeakStart struct {
	energy alphaess.EnergyData
	soc    float64
}

// handleEnd finalises the day's off-peak row by integrating the readings
// table over the plan's free window for that date.
//
// When pending is non-nil (post-restart recovery path) its StartE* fields
// populate the diagnostic start snapshot; otherwise handleEnd uses the
// in-memory state captured by handleStart in this process. With neither, the
// row is finalised from readings alone and the snapshot fields stay zero.
//
// The boundary-wait step is skipped automatically when window-end is already
// in the past (positionAfter recovery): probing for an at-or-after-boundary
// reading would only burn the budget on a moot wait.
//
// Flow (matches design.md "Window-end finalisation state machine"):
//  1. Capture the AlphaESS end snapshot (Decision 2 — diagnostic only).
//  2. Wait up to endWaitBudget for a reading at-or-after window-end
//     (AC 3.1), unless the boundary is already in the past.
//  3. Strongly-consistent query of readings over [windowStart, windowEnd).
//  4. Integrate the five deltas via derivedstats.IntegrateOffpeakDeltas.
//  5. Conditional write with WriteOffpeakIfPendingOrAbsent — fails only when
//     a concurrent writer (backfill CLI) reached `complete` first; in that
//     case we log+skip and accept the other writer's value (AC 3.5).
func (o *OffpeakScheduler) handleEnd(ctx context.Context, date string, pending *dynamo.OffpeakItem, win offpeakWindow) error {
	energy, soc, err := o.captureSnapshot(ctx, date)
	if err != nil {
		return fmt.Errorf("capture end snapshot: %w", err)
	}

	// Resolve the diagnostic start snapshot. The integration over readings
	// is the source of truth for the five deltas (Decision 2); the start
	// snapshot is retained only for drift logging and operator forensics.
	start := o.resolveStartSnapshot(date, pending)
	windowStart, windowEnd := win.Start, win.End

	// Skip the boundary-wait when window-end is already in the past: the
	// at-or-after-boundary reading either already exists or never will, and
	// the wait would just burn its budget on a moot probe. This subsumes the
	// former skipBoundaryWait parameter — the positionAfter recovery path
	// always satisfies this condition (Run() only enters it when now is past
	// windowEnd) and the normal positionBefore/positionDuring paths fire
	// handleEnd within a second of windowEnd so the wait runs.
	if !o.now().After(windowEnd) {
		budget := o.endWaitBudget
		if budget == 0 {
			budget = defaultEndWaitBudget
		}
		pollInterval := o.endWaitPollInterval
		if pollInterval == 0 {
			pollInterval = defaultEndWaitPollInterval
		}
		found, waitErr := o.waitForReadingAtOrAfter(ctx, windowEnd, budget, pollInterval)
		if waitErr != nil {
			// Wait surfaced a store error — log and continue; the query
			// below will either succeed and integrate what it sees, or
			// fail and we propagate that error. This matches "best-effort
			// within the 5-min deadline" (AC 3.2).
			slog.Warn("offpeak boundary wait failed; proceeding with integration",
				"date", date, "error", waitErr)
		} else if !found {
			slog.Warn("offpeak boundary wait timed out; integrating with available readings",
				"date", date, "budget", budget)
		}
	}

	readings, err := o.store.QueryReadingsConsistent(ctx, o.cfg.Serial,
		windowStart.Unix(), windowEnd.Unix())
	if err != nil {
		return fmt.Errorf("query readings for offpeak integration: %w", err)
	}

	deltas := integrateReadings(readings, windowStart, windowEnd)
	// Sparse readings (<2 usable samples) yield SampleCount == 0 and zero-valued
	// deltas. The poller writes that as a complete row anyway (AC 1.6 + AC 3.2)
	// because there's no upstream that can defer the write — unlike the backfill
	// CLI, which can SKIP and leave the row unchanged. A future backfill run
	// with denser readings will overwrite via WriteOffpeakIfComplete.
	if deltas.SampleCount == 0 {
		slog.Warn("offpeak integration produced zero usable samples; writing zero-delta row",
			"date", date, "readingsCount", len(readings))
	}
	item := buildOffpeakRow(o.cfg.Serial, date, start, energy, soc, deltas, o.now().UTC(), win)

	dynamo.LogOffpeakDrift(date, item)

	if err := o.store.WriteOffpeakIfPendingOrAbsent(ctx, item); err != nil {
		if errors.Is(err, dynamo.ErrOffpeakConditionFailed) {
			slog.Warn("offpeak conditional write rejected (peer writer finalised first); skipping",
				"date", date)
			return nil
		}
		return fmt.Errorf("write complete offpeak: %w", err)
	}

	slog.Info("offpeak end captured",
		"date", date, "window", win.StartHHMM+"-"+win.EndHHMM,
		"socStart", item.SocStart, "socEnd", soc,
		"gridUsageKwh", item.GridUsageKwh, "solarKwh", item.SolarKwh,
		"sampleCount", item.IntegrationSampleCount,
		"skippedPairs", item.IntegrationSkippedPairs)
	return nil
}

// resolveStartSnapshot picks the diagnostic start snapshot from in-memory
// state (handleStart ran in this process) or the pending row (post-restart
// recovery), and returns nil when neither exists.
func (o *OffpeakScheduler) resolveStartSnapshot(date string, pending *dynamo.OffpeakItem) *offpeakStart {
	if o.startSnapshot != nil {
		return &offpeakStart{energy: *o.startSnapshot, soc: o.socStart}
	}
	if pending != nil {
		return &offpeakStart{
			energy: alphaess.EnergyData{
				Epv: pending.StartEpv, EInput: pending.StartEInput, EOutput: pending.StartEOutput,
				ECharge: pending.StartECharge, EDischarge: pending.StartEDischarge,
				EGridCharge: pending.StartEGridCharge,
			},
			soc: pending.SocStart,
		}
	}
	slog.Info("offpeak: finalising from readings alone (no start snapshot, no pending row)", "date", date)
	return nil
}

// integrateReadings converts a []dynamo.ReadingItem to []derivedstats.Reading
// and runs the off-peak integration over [windowStart, windowEnd). Empty or
// sparse readings produce zero-valued OffpeakDeltas with SampleCount == 0
// (the (_, false) case from IntegrateOffpeakDeltas) — see handleEnd which
// persists that as a `complete` row per AC 1.6 + AC 3.2.
func integrateReadings(readings []dynamo.ReadingItem, windowStart, windowEnd time.Time) derivedstats.OffpeakDeltas {
	pts := make([]derivedstats.Reading, len(readings))
	for i, r := range readings {
		pts[i] = derivedstats.Reading{
			Timestamp: r.Timestamp,
			Ppv:       r.Ppv,
			Pload:     r.Pload,
			Soc:       r.Soc,
			Pbat:      r.Pbat,
			Pgrid:     r.Pgrid,
		}
	}
	d, _ := derivedstats.IntegrateOffpeakDeltas(pts, windowStart.Unix(), windowEnd.Unix())
	return d
}

// buildOffpeakRow composes the OffpeakItem persisted at window-end. The five
// kWh deltas come from the readings integration (T-1341 AC 5.2); the
// startE*/endE* snapshot fields are retained as diagnostics only (Decision 2).
// Rounded to two decimal places (AC 7.7) so the poller and the backfill CLI
// produce byte-equal values for the same readings.
//
// The row snapshots the window geometry it was integrated under, so a later
// plan edit that moves the free window is detectable as a mismatch rather
// than silently repricing the day (Q23/Q31).
//
// A nil start leaves the StartE*, SocStart, and BatteryDeltaPercent fields
// zero: with no start-of-window reference the SoC delta is unknown, and
// reporting socEnd − 0 would read as a full-battery swing that never happened.
func buildOffpeakRow(
	serial, date string,
	start *offpeakStart,
	end *alphaess.EnergyData,
	socEnd float64,
	deltas derivedstats.OffpeakDeltas,
	integratedAt time.Time,
	win offpeakWindow,
) dynamo.OffpeakItem {
	item := dynamo.OffpeakItem{
		SysSn: serial, Date: date, Status: dynamo.OffpeakStatusComplete,
		EndEpv: end.Epv, EndEInput: end.EInput, EndEOutput: end.EOutput,
		EndECharge: end.ECharge, EndEDischarge: end.EDischarge, EndEGridCharge: end.EGridCharge,
		SocEnd:                  socEnd,
		GridUsageKwh:            derivedstats.RoundEnergy(deltas.GridImportKwh),
		SolarKwh:                derivedstats.RoundEnergy(deltas.SolarKwh),
		BatteryChargeKwh:        derivedstats.RoundEnergy(deltas.BatteryChargeKwh),
		BatteryDischargeKwh:     derivedstats.RoundEnergy(deltas.BatteryDischargeKwh),
		GridExportKwh:           derivedstats.RoundEnergy(deltas.GridExportKwh),
		IntegrationSampleCount:  deltas.SampleCount,
		IntegrationSkippedPairs: deltas.SkippedPairs,
		IntegratedAt:            integratedAt.Format(time.RFC3339),
		WindowStart:             win.StartHHMM,
		WindowEnd:               win.EndHHMM,
	}
	if start != nil {
		item.StartEpv = start.energy.Epv
		item.StartEInput = start.energy.EInput
		item.StartEOutput = start.energy.EOutput
		item.StartECharge = start.energy.ECharge
		item.StartEDischarge = start.energy.EDischarge
		item.StartEGridCharge = start.energy.EGridCharge
		item.SocStart = start.soc
		item.BatteryDeltaPercent = socEnd - start.soc
	}
	return item
}

// captureSnapshot calls GetOneDateEnergy + GetLastPowerData in parallel with retry.
// Both API calls are independent and run concurrently within each attempt.
func (o *OffpeakScheduler) captureSnapshot(ctx context.Context, date string) (*alphaess.EnergyData, float64, error) {
	delay := o.retryDelay
	var lastErr error
	for attempt := range snapshotRetries {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(delay):
			}
		}

		var (
			energy              *alphaess.EnergyData
			power               *alphaess.PowerData
			energyErr, powerErr error
			wg                  sync.WaitGroup
		)

		wg.Add(2)
		go func() {
			defer wg.Done()
			energy, energyErr = o.client.GetOneDateEnergy(ctx, o.cfg.Serial, date)
		}()
		go func() {
			defer wg.Done()
			power, powerErr = o.client.GetLastPowerData(ctx, o.cfg.Serial)
		}()
		wg.Wait()

		if energyErr != nil {
			lastErr = energyErr
			slog.Warn("offpeak snapshot energy attempt failed", "attempt", attempt+1, "error", energyErr)
			continue
		}
		if powerErr != nil {
			lastErr = powerErr
			slog.Warn("offpeak snapshot power attempt failed", "attempt", attempt+1, "error", powerErr)
			continue
		}

		return energy, power.Soc, nil
	}
	return nil, 0, fmt.Errorf("off-peak snapshot failed after %d attempts: %w", snapshotRetries, lastErr)
}

// recoverMidWindow surfaces the pending row to the caller so handleEnd can
// use its StartE* fields for diagnostic snapshot population. Post T-1341 the
// method no longer rebuilds in-memory snapshot/SoC state — handleEnd
// integrates the readings table directly (Decision 2).
//
// Returns the pending row when one exists with status="pending"; nil when
// the row is absent, already complete, or the store query fails. Store
// failures are logged at Error (the day's off-peak row will be skipped
// downstream) but not propagated — the scheduler keeps running for the
// next day.
func (o *OffpeakScheduler) recoverMidWindow(ctx context.Context, date string) *dynamo.OffpeakItem {
	item, err := o.store.GetOffpeak(ctx, o.cfg.Serial, date)
	if err != nil {
		slog.Error("offpeak mid-window recovery: store query failed", "date", date, "error", err)
		return nil
	}
	if item == nil || item.Status != dynamo.OffpeakStatusPending {
		return nil
	}
	slog.Info("offpeak: pending record confirmed for mid-window recovery", "date", date)
	return item
}

// recoverAfterWindow handles the positionAfter path: the process reached this
// day with window-end already behind it, either because it restarted between
// window-end and midnight (T-1341 AC 3.4) or because plan data only became
// readable late in the day (Q36). Either way the day is finalised now rather
// than left for a backfill run.
//
// An absent row is no longer a reason to skip: the integration reads the
// readings table, which still holds the window, and the conditional write
// accepts an absent row. Only an already-complete row is left alone.
//
// handleEnd internally skips the boundary-wait when window-end is already
// in the past (which it is by definition on this path), so there's no need
// for an explicit override.
func (o *OffpeakScheduler) recoverAfterWindow(ctx context.Context, date string, win offpeakWindow) {
	item, err := o.store.GetOffpeak(ctx, o.cfg.Serial, date)
	if err != nil {
		slog.Warn("offpeak post-window recovery: store query failed", "date", date, "error", err)
		return
	}
	if item != nil && item.Status == dynamo.OffpeakStatusComplete {
		slog.Info("offpeak: past window with already-complete row, nothing to recover", "date", date)
		return
	}
	if err := o.handleEnd(ctx, date, item, win); err != nil {
		slog.Warn("offpeak post-window recovery: handleEnd failed", "date", date, "error", err)
	}
}

// handleEndOrCleanup attempts the end snapshot; on failure, deletes the
// pending record. The caller passes the pending row (from recoverMidWindow)
// when handleStart did not run in this process; otherwise nil and handleEnd
// uses in-memory state.
func (o *OffpeakScheduler) handleEndOrCleanup(ctx context.Context, date string, pending *dynamo.OffpeakItem, win offpeakWindow) {
	if err := o.handleEnd(ctx, date, pending, win); err != nil {
		slog.Warn("offpeak end failed, deleting pending record", "date", date, "error", err)
		if delErr := o.store.DeleteOffpeak(ctx, o.cfg.Serial, date); delErr != nil {
			slog.Error("delete pending offpeak failed", "date", date, "error", delErr)
		}
	}
}

// resetState clears in-memory off-peak state for a new day.
func (o *OffpeakScheduler) resetState() {
	o.startSnapshot = nil
	o.socStart = 0
}

// waitUntil blocks until the target time or context cancellation.
// Returns false if context was cancelled.
func (o *OffpeakScheduler) waitUntil(ctx context.Context, target time.Time) bool {
	return o.waitFor(ctx, time.Until(target))
}

// waitFor blocks for the given duration or until context cancellation.
// Returns false if the context was cancelled.
func (o *OffpeakScheduler) waitFor(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

// waitForReadingAtOrAfter polls the readings table every pollInterval until
// either a reading with Timestamp >= target.Unix() exists or the budget
// expires (specs/offpeak-from-readings AC 3.1). Uses the strongly-consistent
// query so the observed reading is guaranteed to be visible to the
// subsequent integration query in handleEnd.
//
// Returns (true, nil) when a qualifying reading lands, (false, nil) on
// timeout, (false, err) on store error. Context cancellation returns
// (false, ctx.Err()).
//
// The helper uses real wall time (time.Now / time.After) intentionally — it
// polls live I/O, not orchestration state. The scheduler's injectable o.now
// governs "what date is it / when should I wake up to do the next thing";
// this helper's clock governs "have I waited long enough on the DB." Tests
// that freeze o.now to skip waitUntil still need the wait-helper deadline
// to advance, so the two clocks are kept distinct on purpose.
//
// The probe's upper bound slides forward each iteration to current real time
// so late-arriving readings beyond the originally-expected window are still
// caught. The lower bound is fixed at the target — anything older is
// irrelevant to the at-or-after check.
func (o *OffpeakScheduler) waitForReadingAtOrAfter(
	ctx context.Context,
	target time.Time,
	budget time.Duration,
	pollInterval time.Duration,
) (bool, error) {
	deadline := time.Now().Add(budget)
	targetUnix := target.Unix()
	for {
		from := targetUnix - 1
		to := time.Now().Unix() + 1
		if to < from {
			to = from
		}
		readings, err := o.store.QueryReadingsConsistent(ctx, o.cfg.Serial, from, to)
		if err != nil {
			return false, fmt.Errorf("query readings for boundary wait: %w", err)
		}
		for _, r := range readings {
			if r.Timestamp >= targetUnix {
				return true, nil
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		sleep := pollInterval
		if remaining < sleep {
			sleep = remaining
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(sleep):
		}
	}
}

// positionFor returns now's position relative to the day's resolved free
// window. It compares absolute instants rather than elapsed-since-midnight
// durations, so a DST day's 23 or 25 hours need no special handling.
func positionFor(now time.Time, win offpeakWindow) windowPosition {
	switch {
	case now.Before(win.Start):
		return positionBefore
	case now.Before(win.End):
		return positionDuring
	default:
		return positionAfter
	}
}

// wallClockTime returns the wall-clock time for a given date and duration from
// midnight. Uses time.Date for DST safety.
func wallClockTime(day time.Time, loc *time.Location, d time.Duration) time.Time {
	local := day.In(loc)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, loc)
}
