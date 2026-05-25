package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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
	hasStart      bool

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
		// Wait for start time, then handle start and end.
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
		o.handleEndOrCleanup(drainCtx, date)

	case positionDuring:
		// T-1341: recovery now only confirms whether the pending row exists.
		// In-memory state isn't rebuilt — handleEnd reads readings and the
		// persisted start snapshot directly.
		hasPending, _ := o.recoverMidWindow(drainCtx, date)
		if hasPending {
			if !o.waitUntil(loopCtx, wallClockTime(now, o.cfg.Location, o.cfg.OffpeakEnd)) {
				return
			}
			o.handleEndOrCleanup(drainCtx, date)
		} else {
			slog.Info("offpeak: no pending record found, skipping today", "date", date)
		}

	case positionAfter:
		// T-1341 AC 3.4: restart between offpeak-end and 24:00. If a pending
		// row exists, finalise it now (skip boundary wait — the boundary is
		// in the past). Otherwise log+skip.
		if err := o.recoverAfterWindow(drainCtx, date); err != nil {
			slog.Error("offpeak post-window recovery failed", "date", date, "error", err)
		}
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

		o.handleEndOrCleanup(drainCtx, date)
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
	o.hasStart = true

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
// Flow (matches design.md "Window-end finalisation state machine"):
//  1. Capture the AlphaESS end snapshot (Decision 2 — diagnostic only).
//  2. Wait up to endWaitBudget for a reading at-or-after offpeak-end
//     (AC 3.1) to land in the readings table.
//  3. Strongly-consistent query of readings over [offpeak-start, offpeak-end).
//  4. Integrate the five deltas via derivedstats.IntegrateOffpeakDeltas.
//  5. Conditional write with WriteOffpeakIfPendingOrAbsent — fails only when
//     a concurrent writer (backfill CLI) reached `complete` first; in that
//     case we log+skip and accept the other writer's value (AC 3.5).
func (o *OffpeakScheduler) handleEnd(ctx context.Context, date string) error {
	energy, soc, err := o.captureSnapshot(ctx, date)
	if err != nil {
		return fmt.Errorf("capture end snapshot: %w", err)
	}

	// Resolve start snapshot: prefer in-memory state (handleStart already
	// ran this process), fall back to the persisted pending row (post-
	// restart recovery path). Either way the value is diagnostic only
	// (Decision 2) — the integration over readings is the source of truth.
	startSnap := o.startSnapshot
	startSoc := o.socStart
	if startSnap == nil {
		if pending, getErr := o.store.GetOffpeak(ctx, o.cfg.Serial, date); getErr == nil && pending != nil {
			startSnap = &alphaess.EnergyData{
				Epv: pending.StartEpv, EInput: pending.StartEInput, EOutput: pending.StartEOutput,
				ECharge: pending.StartECharge, EDischarge: pending.StartEDischarge,
				EGridCharge: pending.StartEGridCharge,
			}
			startSoc = pending.SocStart
		} else {
			// No pending row and no in-memory state: persist zero start
			// snapshot. Drift logging will still compare integratedGrid
			// against (endE − 0), making the recovery case visible.
			startSnap = &alphaess.EnergyData{}
		}
	}

	// Resolve boundary window from cfg in Sydney local time. The wall-clock
	// helper exists already; we re-use it so DST handling stays uniform.
	day, _ := time.ParseInLocation(dateLayout, date, o.cfg.Location)
	windowStart := wallClockTime(day, o.cfg.Location, o.cfg.OffpeakStart)
	windowEnd := wallClockTime(day, o.cfg.Location, o.cfg.OffpeakEnd)

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
		// Wait surfaced a store error — log and continue; the query below
		// will either succeed and integrate what it sees, or fail and we
		// propagate that error. This matches "best-effort within the 5-min
		// deadline" (AC 3.2).
		slog.Warn("offpeak boundary wait failed; proceeding with integration",
			"date", date, "error", waitErr)
	} else if !found {
		slog.Warn("offpeak boundary wait timed out; integrating with available readings",
			"date", date, "budget", budget)
	}

	readings, err := o.store.QueryReadingsConsistent(ctx, o.cfg.Serial,
		windowStart.Unix(), windowEnd.Unix())
	if err != nil {
		return fmt.Errorf("query readings for offpeak integration: %w", err)
	}

	deltas := integrateReadings(readings, windowStart, windowEnd)
	item := buildOffpeakRow(o.cfg.Serial, date, startSnap, energy,
		startSoc, soc, deltas, o.now().UTC())

	LogOffpeakDrift(date, item)

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
		GridUsageKwh:            roundEnergy(deltas.GridImportKwh),
		SolarKwh:                roundEnergy(deltas.SolarKwh),
		BatteryChargeKwh:        roundEnergy(deltas.BatteryChargeKwh),
		BatteryDischargeKwh:     roundEnergy(deltas.BatteryDischargeKwh),
		GridExportKwh:           roundEnergy(deltas.GridExportKwh),
		BatteryDeltaPercent:     socEnd - socStart,
		IntegrationSampleCount:  deltas.SampleCount,
		IntegrationSkippedPairs: deltas.SkippedPairs,
		IntegratedAt:            integratedAt.Format(time.RFC3339),
	}
}

// roundEnergy rounds a kWh value to two decimal places (AC 7.7). Duplicated
// from derivedstats (unexported there) and internal/api/compute.go; consolidating
// is out of scope for T-1341 and would expand the blast radius.
func roundEnergy(v float64) float64 {
	return math.Round(v*100) / 100
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

// recoverMidWindow confirms a pending row exists for the given date so the
// caller knows whether to wait for offpeak-end and run handleEnd, or to
// log+skip the day. Post T-1341 the method no longer rebuilds in-memory
// snapshot/SoC state — handleEnd integrates the readings table directly and
// re-reads the persisted start snapshot only as a diagnostic (Decision 2).
//
// Returns (true, nil) when a row with status="pending" exists, (false, nil)
// when the row is absent or already complete, and (false, nil) on store
// error (logged at warn, not propagated — same defensive policy as before).
func (o *OffpeakScheduler) recoverMidWindow(ctx context.Context, date string) (bool, error) {
	item, err := o.store.GetOffpeak(ctx, o.cfg.Serial, date)
	if err != nil {
		slog.Warn("offpeak mid-window recovery: store query failed", "date", date, "error", err)
		return false, nil
	}
	if item == nil || item.Status != dynamo.OffpeakStatusPending {
		return false, nil
	}
	slog.Info("offpeak: pending record confirmed for mid-window recovery", "date", date)
	return true, nil
}

// recoverAfterWindow handles the positionAfter restart path (T-1341 AC 3.4):
// when the poller starts up between offpeak-end and 24:00 with a pending row,
// run the integration path immediately so the day is finalised without
// waiting another 24 hours for the next start tick. When no row exists or
// the row is already complete, log+skip — no work to do.
//
// The boundary wait is intentionally skipped here: the SSM offpeak-end is
// already in the past so the at-or-after-boundary reading either exists or
// never will. handleEnd's wait-loop would burn its budget on a moot probe.
func (o *OffpeakScheduler) recoverAfterWindow(ctx context.Context, date string) error {
	item, err := o.store.GetOffpeak(ctx, o.cfg.Serial, date)
	if err != nil {
		slog.Warn("offpeak post-window recovery: store query failed", "date", date, "error", err)
		return nil
	}
	if item == nil {
		slog.Info("offpeak: past window with no pending row, skipping today", "date", date)
		return nil
	}
	if item.Status == dynamo.OffpeakStatusComplete {
		slog.Info("offpeak: past window with already-complete row, nothing to recover", "date", date)
		return nil
	}
	// Pending row: re-run the finalisation logic, but skip the boundary wait
	// since offpeak-end is already in the past. Set the wait budget to zero
	// so handleEnd's wait helper short-circuits immediately to the integration.
	prevBudget := o.endWaitBudget
	o.endWaitBudget = 1 * time.Nanosecond
	defer func() { o.endWaitBudget = prevBudget }()
	if err := o.handleEnd(ctx, date); err != nil {
		slog.Warn("offpeak post-window recovery: handleEnd failed", "date", date, "error", err)
	}
	return nil
}

// handleEndOrCleanup attempts the end snapshot; on failure, deletes the pending record.
func (o *OffpeakScheduler) handleEndOrCleanup(ctx context.Context, date string) {
	if err := o.handleEnd(ctx, date); err != nil {
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
	o.hasStart = false
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
// The probe queries a narrow window around target (target-1s to
// target+pollInterval+1s) — wide enough to catch the next reading, narrow
// enough to keep DynamoDB cost negligible across the budget's ~15 probes.
func (o *OffpeakScheduler) waitForReadingAtOrAfter(
	ctx context.Context,
	target time.Time,
	budget time.Duration,
	pollInterval time.Duration,
) (bool, error) {
	// Use a real-time deadline so the wait honours the budget even when the
	// scheduler's injectable clock is frozen (tests fix o.now to a moment
	// past the SSM offpeak-end to skip the waitUntil pre-step). time.After
	// drives real wall time regardless.
	deadline := time.Now().Add(budget)
	targetUnix := target.Unix()
	for {
		// Query a narrow window around the target: from just before the
		// boundary to budget seconds past it. Ascending order means the
		// first reading with ts >= targetUnix indicates the boundary has
		// been crossed.
		from := targetUnix - 1
		to := target.Add(budget).Unix() + 1
		readings, err := o.store.QueryReadingsConsistent(ctx, o.cfg.Serial, from, to)
		if err != nil {
			return false, fmt.Errorf("query readings for boundary wait: %w", err)
		}
		for _, r := range readings {
			if r.Timestamp >= targetUnix {
				return true, nil
			}
		}
		// No qualifying reading yet. Sleep until the next probe or the
		// deadline, whichever is sooner.
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

// LogOffpeakDrift emits a structured INFO log entry comparing the snapshot-
// diff value (endE* − startE*) with the readings-integrated value for each
// of the five deltas, plus the absolute difference (T-1341 AC 6.1 / 6.2).
//
// Output is key=value formatted (slog default for slog.Info) so CloudWatch
// Logs Insights can parse it with `fields date, driftGrid, …`. No metric or
// alarm is emitted — alerting is deferred per Decision 6.
//
// Called by handleEnd immediately before the conditional write, and by the
// backfill CLI per row.
func LogOffpeakDrift(date string, item dynamo.OffpeakItem) {
	snapGrid := item.EndEInput - item.StartEInput
	snapSolar := item.EndEpv - item.StartEpv
	snapCharge := item.EndECharge - item.StartECharge
	snapDischarge := item.EndEDischarge - item.StartEDischarge
	snapExport := item.EndEOutput - item.StartEOutput

	slog.Info("offpeak drift",
		"date", date,
		"snapshotGrid", snapGrid,
		"integratedGrid", item.GridUsageKwh,
		"driftGrid", math.Abs(item.GridUsageKwh-snapGrid),
		"snapshotSolar", snapSolar,
		"integratedSolar", item.SolarKwh,
		"driftSolar", math.Abs(item.SolarKwh-snapSolar),
		"snapshotCharge", snapCharge,
		"integratedCharge", item.BatteryChargeKwh,
		"driftCharge", math.Abs(item.BatteryChargeKwh-snapCharge),
		"snapshotDischarge", snapDischarge,
		"integratedDischarge", item.BatteryDischargeKwh,
		"driftDischarge", math.Abs(item.BatteryDischargeKwh-snapDischarge),
		"snapshotExport", snapExport,
		"integratedExport", item.GridExportKwh,
		"driftExport", math.Abs(item.GridExportKwh-snapExport),
	)
}
