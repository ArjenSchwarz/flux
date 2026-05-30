package poller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// pollDailySummary runs the daily-derived-stats summarisation pass for
// "yesterday" once per dailySummaryInterval. The first tick fires immediately
// on poller startup so a container restarted inside the post-midnight gap
// fills in yesterday's row on its first iteration.
func (p *Poller) pollDailySummary(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	pollLoop(loopCtx, drainCtx, wg, dailySummaryInterval, p.summariseYesterday)
}

// summariseYesterday is the per-tick body. It resolves "yesterday" against
// p.cfg.Location, runs the pass, and emits the resulting metric dimension.
func (p *Poller) summariseYesterday(ctx context.Context) {
	yesterday := p.now().In(p.cfg.Location).AddDate(0, 0, -1).Format(dateLayout)
	result := p.runSummarisationPass(ctx, yesterday)
	p.metrics.RecordSummarisationPass(ctx, result)
}

// runSummarisationPass performs the full pass for one (sysSn, date) pair and
// returns the metric dimension value. Side-effects only: logs at info / warn
// / error and writes derivedStats via Store.UpdateDailyEnergyDerived. Never
// panics.
//
// The precheck (step 1) and write (step 5) form a read-then-write with no
// DynamoDB ConditionExpression. Decision 8 accepts the TOCTOU window because
// the ECS service runs at desiredCount=1 (concurrent passes cannot occur in
// production) and the writes are idempotent — a duplicate pass would write
// field-equivalent data. If the deployment topology ever scales out, add an
// `attribute_not_exists(derivedStatsComputedAt)` condition to the write.
func (p *Poller) runSummarisationPass(ctx context.Context, date string) string {
	// 1. Precheck (AC 1.10) — sentinel attribute presence is the only signal.
	item, err := p.store.GetDailyEnergy(ctx, p.cfg.Serial, date)
	switch {
	case err != nil:
		slog.Error("summary precheck failed", "date", date, "error", err)
		return PassResultError
	case item == nil:
		// AC 1.4: skip when row does not yet exist; let the next AlphaESS
		// energy poll create the row.
		slog.Info("summary skipped: no daily-energy row yet", "date", date)
		return PassResultSkippedNoRow
	}

	// Two orthogonal sentinels gate two independent compute blocks
	// (peak-from-readings Decision 3). Skip the whole pass only when BOTH are
	// set; otherwise compute whichever group is still missing. A row with
	// derived stats but no peak (e.g. pre-feature row picked up after deploy)
	// gets only peak written.
	needDerived := item.DerivedStatsComputedAt == ""
	needPeak := item.PeakComputedAt == ""
	if !needDerived && !needPeak {
		// AC 1.10 / daily-derived-stats Decision 8 — both sentinels present
		// means a prior pass computed everything.
		return PassResultSkippedAlreadyDone
	}

	// 2. Off-peak window resolution (AC 1.6 / 1.14). Needed by both blocks.
	offpeakStart := config.FormatHHMM(p.cfg.OffpeakStart)
	offpeakEnd := config.FormatHHMM(p.cfg.OffpeakEnd)
	startMin, endMin, ok := derivedstats.ParseOffpeakWindow(offpeakStart, offpeakEnd)
	if !ok {
		slog.Warn("summary skipped: off-peak window unresolved", "date", date)
		return PassResultSkippedSSMUnresolved
	}

	// 3. Fetch the day's readings.
	dayStart, _ := time.ParseInLocation(dateLayout, date, p.cfg.Location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	rawReadings, err := p.store.QueryReadings(ctx, p.cfg.Serial, dayStart.Unix(), dayEnd.Unix()-1)
	if err != nil {
		slog.Error("summary readings query failed", "date", date, "error", err)
		return PassResultError
	}
	if len(rawReadings) == 0 {
		slog.Info("summary skipped: no readings for date", "date", date)
		return PassResultSkippedNoReadings
	}
	readings := summaryToDerivedReadings(rawReadings)
	now := p.now()

	var derived dynamo.DerivedStats

	// 4a. derivedStats block — gated on its own sentinel. Pass `today=date` so
	// the today-gate cannot fire on a completed date (AC 1.2 + the "today"
	// parameter contract on derivedstats.Blocks).
	if needDerived {
		socLow, socLowTS, socFound := derivedstats.MinSOC(readings)
		derived.DailyUsage = dynamo.DailyUsageToAttr(derivedstats.Blocks(readings, offpeakStart, offpeakEnd, date, date, now))
		derived.PeakPeriods = dynamo.PeakPeriodsToAttr(derivedstats.PeakPeriods(readings, offpeakStart, offpeakEnd))
		derived.DerivedStatsComputedAt = now.UTC().Format(time.RFC3339)
		if socFound {
			derived.SocLow = &dynamo.SocLowAttr{
				Soc:       socLow,
				Timestamp: time.Unix(socLowTS, 0).UTC().Format(time.RFC3339),
			}
		}
	}

	// 4b. Peak grid import block — gated on its own sentinel. The off-peak
	// window bounds the two peak sub-windows. Boundaries are derived from the
	// DST-correct dayStart so 23h/25h Sydney days integrate correctly. When
	// the usability gate fails for either sub-window the field is left absent
	// (PeakGridImportKwh stays nil), but the sentinel is still set so the row
	// is not re-attempted every hour.
	if needPeak {
		offpeakStartUnix := dayStart.Add(time.Duration(startMin) * time.Minute).Unix()
		offpeakEndUnix := dayStart.Add(time.Duration(endMin) * time.Minute).Unix()
		kwh, _, _, peakOK := derivedstats.IntegratePeakGridImportKwh(
			readings, dayStart.Unix(), offpeakStartUnix, offpeakEndUnix, dayEnd.Unix())
		if peakOK {
			rounded := derivedstats.RoundEnergy(kwh)
			derived.PeakGridImportKwh = &rounded
		}
		derived.PeakComputedAt = now.UTC().Format(time.RFC3339)
	}

	// 5. Write — UpdateDailyEnergyDerived writes each group only when its
	// sentinel is non-empty, so an already-computed group is never clobbered.
	if err := p.store.UpdateDailyEnergyDerived(ctx, p.cfg.Serial, date, derived); err != nil {
		slog.Error("summary write failed", "date", date, "error", err)
		return PassResultError
	}
	slog.Info("summary written", "date", date, "wroteDerived", needDerived, "wrotePeak", needPeak)
	return PassResultSuccess
}

// summaryToDerivedReadings converts the storage-level []dynamo.ReadingItem
// to the leaf-package []derivedstats.Reading. Per Decision 9 this conversion
// is duplicated at each call site (api/day.go, api/history.go, here) rather
// than shared via a helper, to keep the derivedstats package free of any
// upward import into dynamo.
func summaryToDerivedReadings(in []dynamo.ReadingItem) []derivedstats.Reading {
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
