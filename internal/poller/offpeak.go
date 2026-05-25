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
)

// windowPosition represents the poller's position relative to the off-peak window.
type windowPosition string

const (
	positionBefore windowPosition = "before"
	positionDuring windowPosition = "during"
	positionAfter  windowPosition = "after"
)

// OffpeakScheduler manages off-peak window state and snapshot capture.
type OffpeakScheduler struct {
	client APIClient
	store  dynamo.Store
	cfg    *config.Config

	// In-memory state for current day's off-peak calculation.
	startSnapshot *alphaess.EnergyData
	socStart      float64

	// retryDelay between snapshot attempts (overridable for tests).
	retryDelay time.Duration

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
func NewOffpeakScheduler(client APIClient, store dynamo.Store, cfg *config.Config) *OffpeakScheduler {
	return &OffpeakScheduler{
		client:     client,
		store:      store,
		cfg:        cfg,
		retryDelay: defaultRetryDelay,
		now:        time.Now,
	}
}

// Run determines the current position relative to the off-peak window and
// schedules snapshot captures accordingly. Loops daily.
func (o *OffpeakScheduler) Run(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	now := o.now().In(o.cfg.Location)
	date := now.Format(dateLayout)
	pos := timePosition(now, o.cfg.OffpeakStart, o.cfg.OffpeakEnd)

	slog.Debug("offpeak scheduler starting", "position", pos, "date", date)

	switch pos {
	case positionBefore:
		// Wait for start time, then handle start and end. handleStart
		// populates in-memory state, so handleEnd doesn't need the pending row.
		if !o.waitUntil(loopCtx, wallClockTime(now, o.cfg.Location, o.cfg.OffpeakStart)) {
			return
		}
		if err := o.handleStart(drainCtx, date); err != nil {
			slog.Error("offpeak start failed", "date", date, "error", err)
			// Skip to tomorrow.
			goto nextDay
		}
		if !o.waitUntil(loopCtx, wallClockTime(now, o.cfg.Location, o.cfg.OffpeakEnd)) {
			return
		}
		o.handleEndOrCleanup(drainCtx, date, nil)

	case positionDuring:
		// T-1341: recovery surfaces the pending row to the caller. In-memory
		// state isn't rebuilt — handleEnd reads readings directly and uses
		// the pending row's StartE* fields for diagnostic snapshot population.
		pending := o.recoverMidWindow(drainCtx, date)
		if pending != nil {
			if !o.waitUntil(loopCtx, wallClockTime(now, o.cfg.Location, o.cfg.OffpeakEnd)) {
				return
			}
			o.handleEndOrCleanup(drainCtx, date, pending)
		} else {
			slog.Info("offpeak: no pending record found, skipping today", "date", date)
		}

	case positionAfter:
		// T-1341 AC 3.4: restart between offpeak-end and 24:00. If a pending
		// row exists, finalise it now (skip boundary wait — the boundary is
		// in the past). Otherwise log+skip. recoverAfterWindow follows the
		// scheduler's log-and-continue convention; failures are logged inside.
		o.recoverAfterWindow(drainCtx, date)
	}

nextDay:
	// Daily loop: wait for tomorrow's start, then repeat.
	for {
		o.resetState()
		tomorrow := o.now().In(o.cfg.Location).AddDate(0, 0, 1)
		date = tomorrow.Format(dateLayout)
		startTime := wallClockTime(tomorrow, o.cfg.Location, o.cfg.OffpeakStart)

		if !o.waitUntil(loopCtx, startTime) {
			return
		}

		if err := o.handleStart(drainCtx, date); err != nil {
			slog.Error("offpeak start failed", "date", date, "error", err)
			continue
		}

		endTime := wallClockTime(tomorrow, o.cfg.Location, o.cfg.OffpeakEnd)
		if !o.waitUntil(loopCtx, endTime) {
			return
		}

		o.handleEndOrCleanup(drainCtx, date, nil)
	}
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

// handleEnd finalises the day's off-peak row by integrating the readings
// table over the SSM window (specs/offpeak-from-readings T-1341).
//
// When pending is non-nil (post-restart recovery path) its StartE* fields
// populate the diagnostic start snapshot; otherwise handleEnd uses the
// in-memory state captured by handleStart in this process. One of the two
// must be set — both nil indicates a programming error and returns immediately.
//
// The boundary-wait step is skipped automatically when offpeak-end is already
// in the past (positionAfter recovery): probing for an at-or-after-boundary
// reading would only burn the budget on a moot wait.
//
// Flow (matches design.md "Window-end finalisation state machine"):
//  1. Capture the AlphaESS end snapshot (Decision 2 — diagnostic only).
//  2. Wait up to endWaitBudget for a reading at-or-after offpeak-end
//     (AC 3.1), unless the boundary is already in the past.
//  3. Strongly-consistent query of readings over [offpeak-start, offpeak-end).
//  4. Integrate the five deltas via derivedstats.IntegrateOffpeakDeltas.
//  5. Conditional write with WriteOffpeakIfPendingOrAbsent — fails only when
//     a concurrent writer (backfill CLI) reached `complete` first; in that
//     case we log+skip and accept the other writer's value (AC 3.5).
func (o *OffpeakScheduler) handleEnd(ctx context.Context, date string, pending *dynamo.OffpeakItem) error {
	energy, soc, err := o.captureSnapshot(ctx, date)
	if err != nil {
		return fmt.Errorf("capture end snapshot: %w", err)
	}

	// Resolve the diagnostic start snapshot. The integration over readings
	// is the source of truth for the five deltas (Decision 2); the start
	// snapshot is retained only for drift logging and operator forensics.
	startSnap := o.startSnapshot
	startSoc := o.socStart
	if startSnap == nil {
		if pending == nil {
			// Unreachable in production: both Run() and recoverAfterWindow
			// only call handleEnd with either in-memory state (handleStart
			// ran in this process) or a non-nil pending row.
			return fmt.Errorf("handleEnd: no start snapshot available for %s (no in-memory state, no pending row)", date)
		}
		startSnap = &alphaess.EnergyData{
			Epv: pending.StartEpv, EInput: pending.StartEInput, EOutput: pending.StartEOutput,
			ECharge: pending.StartECharge, EDischarge: pending.StartEDischarge,
			EGridCharge: pending.StartEGridCharge,
		}
		startSoc = pending.SocStart
	}

	// Resolve boundary window from cfg in Sydney local time. The wall-clock
	// helper exists already; we re-use it so DST handling stays uniform.
	day, _ := time.ParseInLocation(dateLayout, date, o.cfg.Location)
	windowStart := wallClockTime(day, o.cfg.Location, o.cfg.OffpeakStart)
	windowEnd := wallClockTime(day, o.cfg.Location, o.cfg.OffpeakEnd)

	// Skip the boundary-wait when offpeak-end is already in the past: the
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
	item := buildOffpeakRow(o.cfg.Serial, date, startSnap, energy,
		startSoc, soc, deltas, o.now().UTC())

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
		"date", date, "socStart", startSoc, "socEnd", soc,
		"gridUsageKwh", item.GridUsageKwh, "solarKwh", item.SolarKwh,
		"sampleCount", item.IntegrationSampleCount,
		"skippedPairs", item.IntegrationSkippedPairs)
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
func buildOffpeakRow(
	serial, date string,
	start, end *alphaess.EnergyData,
	socStart, socEnd float64,
	deltas derivedstats.OffpeakDeltas,
	integratedAt time.Time,
) dynamo.OffpeakItem {
	return dynamo.OffpeakItem{
		SysSn: serial, Date: date, Status: dynamo.OffpeakStatusComplete,
		StartEpv:        start.Epv,
		StartEInput:     start.EInput,
		StartEOutput:    start.EOutput,
		StartECharge:    start.ECharge,
		StartEDischarge: start.EDischarge, StartEGridCharge: start.EGridCharge,
		SocStart: socStart,
		EndEpv:   end.Epv, EndEInput: end.EInput, EndEOutput: end.EOutput,
		EndECharge: end.ECharge, EndEDischarge: end.EDischarge, EndEGridCharge: end.EGridCharge,
		SocEnd:                  socEnd,
		GridUsageKwh:            derivedstats.RoundEnergy(deltas.GridImportKwh),
		SolarKwh:                derivedstats.RoundEnergy(deltas.SolarKwh),
		BatteryChargeKwh:        derivedstats.RoundEnergy(deltas.BatteryChargeKwh),
		BatteryDischargeKwh:     derivedstats.RoundEnergy(deltas.BatteryDischargeKwh),
		GridExportKwh:           derivedstats.RoundEnergy(deltas.GridExportKwh),
		BatteryDeltaPercent:     socEnd - socStart,
		IntegrationSampleCount:  deltas.SampleCount,
		IntegrationSkippedPairs: deltas.SkippedPairs,
		IntegratedAt:            integratedAt.Format(time.RFC3339),
	}
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

// recoverAfterWindow handles the positionAfter restart path (T-1341 AC 3.4):
// when the poller starts up between offpeak-end and 24:00 with a pending row,
// run the integration path immediately so the day is finalised without
// waiting another 24 hours for the next start tick. When no row exists or
// the row is already complete, log+skip — no work to do.
//
// handleEnd internally skips the boundary-wait when offpeak-end is already
// in the past (which it is by definition on this path), so there's no need
// for an explicit override.
func (o *OffpeakScheduler) recoverAfterWindow(ctx context.Context, date string) {
	item, err := o.store.GetOffpeak(ctx, o.cfg.Serial, date)
	if err != nil {
		slog.Warn("offpeak post-window recovery: store query failed", "date", date, "error", err)
		return
	}
	if item == nil {
		slog.Info("offpeak: past window with no pending row, skipping today", "date", date)
		return
	}
	if item.Status == dynamo.OffpeakStatusComplete {
		slog.Info("offpeak: past window with already-complete row, nothing to recover", "date", date)
		return
	}
	if err := o.handleEnd(ctx, date, item); err != nil {
		slog.Warn("offpeak post-window recovery: handleEnd failed", "date", date, "error", err)
	}
}

// handleEndOrCleanup attempts the end snapshot; on failure, deletes the
// pending record. The caller passes the pending row (from recoverMidWindow)
// when handleStart did not run in this process; otherwise nil and handleEnd
// uses in-memory state.
func (o *OffpeakScheduler) handleEndOrCleanup(ctx context.Context, date string, pending *dynamo.OffpeakItem) {
	if err := o.handleEnd(ctx, date, pending); err != nil {
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
	delay := time.Until(target)
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

// timePosition returns the current time's position relative to the off-peak window.
func timePosition(now time.Time, start, end time.Duration) windowPosition {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	elapsed := now.Sub(midnight)
	switch {
	case elapsed < start:
		return positionBefore
	case elapsed < end:
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
