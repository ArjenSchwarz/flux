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
	case item.DerivedStatsComputedAt != "":
		// AC 1.10 / Decision 8 — sentinel present means a prior pass succeeded.
		return PassResultSkippedAlreadyDone
	}

	// 2. Off-peak window resolution (AC 1.6 / 1.14).
	offpeakStart := config.FormatHHMM(p.cfg.OffpeakStart)
	offpeakEnd := config.FormatHHMM(p.cfg.OffpeakEnd)
	if _, _, ok := derivedstats.ParseOffpeakWindow(offpeakStart, offpeakEnd); !ok {
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

	// 4. Compute derivedStats. Pass `today=date` so the today-gate cannot
	// fire on a completed date (AC 1.2 + the "today" parameter contract on
	// derivedstats.Blocks).
	now := p.now()
	socLow, socLowTS, socFound := derivedstats.MinSOC(readings)
	derived := dynamo.DerivedStats{
		DailyUsage:             dynamo.DailyUsageToAttr(derivedstats.Blocks(readings, offpeakStart, offpeakEnd, date, date, now)),
		PeakPeriods:            dynamo.PeakPeriodsToAttr(derivedstats.PeakPeriods(readings, offpeakStart, offpeakEnd)),
		DerivedStatsComputedAt: now.UTC().Format(time.RFC3339),
	}
	if socFound {
		derived.SocLow = &dynamo.SocLowAttr{
			Soc:       socLow,
			Timestamp: time.Unix(socLowTS, 0).UTC().Format(time.RFC3339),
		}
	}

	// 5. Write — single SET expression covers all four attributes atomically.
	if err := p.store.UpdateDailyEnergyDerived(ctx, p.cfg.Serial, date, derived); err != nil {
		slog.Error("summary write failed", "date", date, "error", err)
		return PassResultError
	}
	slog.Info("summary written", "date", date)
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
