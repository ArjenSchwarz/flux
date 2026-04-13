package poller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

const (
	snapshotRetries   = 3
	defaultRetryDelay = 10 * time.Second
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
}

// NewOffpeakScheduler creates an OffpeakScheduler with the given dependencies.
func NewOffpeakScheduler(client APIClient, store dynamo.Store, cfg *config.Config) *OffpeakScheduler {
	return &OffpeakScheduler{
		client:     client,
		store:      store,
		cfg:        cfg,
		retryDelay: defaultRetryDelay,
	}
}

// Run determines the current position relative to the off-peak window and
// schedules snapshot captures accordingly. Loops daily.
func (o *OffpeakScheduler) Run(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	now := time.Now().In(o.cfg.Location)
	date := now.Format("2006-01-02")
	pos := timePosition(now, o.cfg.OffpeakStart, o.cfg.OffpeakEnd)

	slog.Debug("offpeak scheduler starting", "position", pos, "date", date)

	switch pos {
	case "before":
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
		if err := o.handleEnd(drainCtx, date); err != nil {
			slog.Warn("offpeak end failed, deleting pending record", "date", date, "error", err)
			if delErr := o.store.DeleteOffpeak(drainCtx, o.cfg.Serial, date); delErr != nil {
				slog.Error("delete pending offpeak failed", "date", date, "error", delErr)
			}
		}

	case "during":
		// Try to recover from existing pending record.
		if err := o.recoverMidWindow(drainCtx, date); err != nil {
			slog.Error("offpeak mid-window recovery failed", "date", date, "error", err)
		}
		if o.hasStart {
			if !o.waitUntil(loopCtx, wallClockTime(now, o.cfg.Location, o.cfg.OffpeakEnd)) {
				return
			}
			if err := o.handleEnd(drainCtx, date); err != nil {
				slog.Warn("offpeak end failed, deleting pending record", "date", date, "error", err)
				if delErr := o.store.DeleteOffpeak(drainCtx, o.cfg.Serial, date); delErr != nil {
					slog.Error("delete pending offpeak failed", "date", date, "error", delErr)
				}
			}
		} else {
			slog.Info("offpeak: no pending record found, skipping today", "date", date)
		}

	case "after":
		slog.Info("offpeak: past window, skipping today", "date", date)
	}

nextDay:
	// Daily loop: wait for tomorrow's start, then repeat.
	for {
		o.resetState()
		tomorrow := time.Now().In(o.cfg.Location).AddDate(0, 0, 1)
		date = tomorrow.Format("2006-01-02")
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

		if err := o.handleEnd(drainCtx, date); err != nil {
			slog.Warn("offpeak end failed, deleting pending record", "date", date, "error", err)
			if delErr := o.store.DeleteOffpeak(drainCtx, o.cfg.Serial, date); delErr != nil {
				slog.Error("delete pending offpeak failed", "date", date, "error", delErr)
			}
		}
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
		SysSn: o.cfg.Serial, Date: date, Status: "pending",
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

// handleEnd captures the end snapshot, computes deltas, and writes the complete record.
func (o *OffpeakScheduler) handleEnd(ctx context.Context, date string) error {
	energy, soc, err := o.captureSnapshot(ctx, date)
	if err != nil {
		return fmt.Errorf("capture end snapshot: %w", err)
	}

	item := computeOffpeakDeltas(o.cfg.Serial, date, o.startSnapshot, energy, o.socStart, soc)
	if err := o.store.WriteOffpeak(ctx, item); err != nil {
		return fmt.Errorf("write complete offpeak: %w", err)
	}

	slog.Info("offpeak end captured", "date", date, "socStart", o.socStart, "socEnd", soc)
	return nil
}

// captureSnapshot calls GetOneDateEnergy + GetLastPowerData with retry.
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

		energy, err := o.client.GetOneDateEnergy(ctx, o.cfg.Serial, date)
		if err != nil {
			lastErr = err
			slog.Warn("offpeak snapshot energy attempt failed", "attempt", attempt+1, "error", err)
			continue
		}

		power, err := o.client.GetLastPowerData(ctx, o.cfg.Serial)
		if err != nil {
			lastErr = err
			slog.Warn("offpeak snapshot power attempt failed", "attempt", attempt+1, "error", err)
			continue
		}

		return energy, power.Soc, nil
	}
	return nil, 0, fmt.Errorf("off-peak snapshot failed after %d attempts: %w", snapshotRetries, lastErr)
}

// recoverMidWindow checks for an existing pending record and recovers state.
func (o *OffpeakScheduler) recoverMidWindow(ctx context.Context, date string) error {
	item, err := o.store.GetOffpeak(ctx, o.cfg.Serial, date)
	if err != nil {
		slog.Warn("offpeak mid-window recovery: store query failed", "date", date, "error", err)
		return nil // Log and skip, don't propagate.
	}

	if item == nil || item.Status != "pending" {
		return nil
	}

	o.startSnapshot = &alphaess.EnergyData{
		Epv: item.StartEpv, EInput: item.StartEInput, EOutput: item.StartEOutput,
		ECharge: item.StartECharge, EDischarge: item.StartEDischarge, EGridCharge: item.StartEGridCharge,
	}
	o.socStart = item.SocStart
	o.hasStart = true

	slog.Info("offpeak: recovered pending record", "date", date, "socStart", o.socStart)
	return nil
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

// timePosition returns "before", "during", or "after" based on the current
// time's position relative to the off-peak window.
func timePosition(now time.Time, start, end time.Duration) string {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	elapsed := now.Sub(midnight)
	switch {
	case elapsed < start:
		return "before"
	case elapsed < end:
		return "during"
	default:
		return "after"
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

// computeOffpeakDeltas computes all energy deltas between start and end snapshots.
func computeOffpeakDeltas(serial, date string, start, end *alphaess.EnergyData, socStart, socEnd float64) dynamo.OffpeakItem {
	return dynamo.OffpeakItem{
		SysSn: serial, Date: date, Status: "complete",
		StartEpv: start.Epv, StartEInput: start.EInput, StartEOutput: start.EOutput,
		StartECharge: start.ECharge, StartEDischarge: start.EDischarge, StartEGridCharge: start.EGridCharge,
		SocStart: socStart,
		EndEpv:   end.Epv, EndEInput: end.EInput, EndEOutput: end.EOutput,
		EndECharge: end.ECharge, EndEDischarge: end.EDischarge, EndEGridCharge: end.EGridCharge,
		SocEnd:              socEnd,
		GridUsageKwh:        end.EInput - start.EInput,
		SolarKwh:            end.Epv - start.Epv,
		BatteryChargeKwh:    end.ECharge - start.ECharge,
		BatteryDischargeKwh: end.EDischarge - start.EDischarge,
		GridExportKwh:       end.EOutput - start.EOutput,
		BatteryDeltaPercent: socEnd - socStart,
	}
}
