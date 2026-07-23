package poller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
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

	// Three orthogonal sentinels gate three independent compute blocks
	// (peak-from-readings Decision 3). Skip the whole pass only when ALL are
	// set; otherwise compute whichever group is still missing. A row with
	// derived stats but no peak (e.g. pre-feature row picked up after deploy)
	// gets only peak written.
	needDerived := item.DerivedStatsComputedAt == ""
	needPeak := item.PeakComputedAt == ""
	needBands := item.BandsComputedAt == ""
	if !needDerived && !needPeak && !needBands {
		// AC 1.10 / daily-derived-stats Decision 8 — every sentinel present
		// means a prior pass computed everything.
		return PassResultSkippedAlreadyDone
	}

	// 2. Plan resolution (AC 4.1). The plan pricing the date is the source of
	// truth for the free window; the three outcomes below are gated
	// separately because they mean different things (Q33).
	plans, err := p.plans.Plans(ctx)
	if err != nil {
		// AC 4.6: an unreadable pricing table is transient, never "no plan".
		// Setting sentinels here would make the day terminal on the strength
		// of an infra blip, so nothing is written and the next tick retries.
		slog.Error("summary plan read failed", "date", date, "error", err)
		return PassResultError
	}
	datePlan, hasPlan := plan.PlanFor(plans, date)
	// hhmm bounds for the derivedstats helpers. Empty strings are how those
	// helpers already express "no off-peak window", and they degrade to their
	// window-free layouts rather than defaulting to a window that isn't there.
	var offpeakStart, offpeakEnd string
	if startMin, endMin, ok := datePlan.FreeWindowMinutes(); hasPlan && ok {
		offpeakStart = plan.FormatBandTime(startMin)
		offpeakEnd = plan.FormatBandTime(endMin)
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
	//
	// With no plan at all only socLow runs: the block layout and the peak
	// periods both partition the day around the free window, and guessing one
	// would bake a wrong layout into a sentinel-gated field. socLow needs no
	// window, and the design's rule is that a semantic absence never returns
	// before window-independent work has run.
	if needDerived {
		socLow, socLowTS, socFound := derivedstats.MinSOC(readings)
		if socFound {
			derived.SocLow = &dynamo.SocLowAttr{
				Soc:       socLow,
				Timestamp: time.Unix(socLowTS, 0).UTC().Format(time.RFC3339),
			}
		}
		if hasPlan {
			derived.DailyUsage = dynamo.DailyUsageToAttr(derivedstats.Blocks(readings, offpeakStart, offpeakEnd, date, date, now))
			derived.PeakPeriods = dynamo.PeakPeriodsToAttr(derivedstats.PeakPeriods(readings, offpeakStart, offpeakEnd))
		}
		derived.DerivedStatsComputedAt = now.UTC().Format(time.RFC3339)
	}

	// 4b. Rated-band block, shared by the peak and band groups. Both describe
	// the same physical quantity — grid import outside the free window — so
	// they come from one integration and cannot disagree (Data Consistency).
	// A plan without a free band leaves the whole day rated, which is exactly
	// what "whole-day-rated mode" means for both values.
	//
	// Without a plan neither is defined: "peak" means "outside the free
	// window", and no plan means no answer to what that window is. Both
	// sentinels stay unset so a backfill can still capture the split once a
	// plan exists — terminal only once the readings TTL prunes the day.
	if hasPlan && (needPeak || needBands) {
		bands, totalKwh, bandsOK := dynamo.IntegrateRatedBands(readings, datePlan, dayStart, p.cfg.Location)
		if needPeak {
			// The usability gate is shared too: a split missing any segment
			// cannot produce a trustworthy total either. The field stays
			// absent, but the sentinel is set so the row is not re-attempted
			// every hour.
			if bandsOK {
				derived.PeakGridImportKwh = &totalKwh
			}
			derived.PeakComputedAt = now.UTC().Format(time.RFC3339)
		}
		if needBands {
			if bandsOK {
				derived.BandImports = bands
			}
			derived.BandsComputedAt = now.UTC().Format(time.RFC3339)
		}
	}

	// 5. Write — UpdateDailyEnergyDerived writes each group only when its
	// sentinel is non-empty, so an already-computed group is never clobbered.
	if err := p.store.UpdateDailyEnergyDerived(ctx, p.cfg.Serial, date, derived); err != nil {
		slog.Error("summary write failed", "date", date, "error", err)
		return PassResultError
	}
	slog.Info("summary written", "date", date,
		"plan", planLabel(datePlan, hasPlan), "window", windowLabel(offpeakStart, offpeakEnd),
		"wroteDerived", derived.DerivedStatsComputedAt != "",
		"wrotePeak", derived.PeakComputedAt != "",
		"wroteBands", derived.BandsComputedAt != "")
	return PassResultSuccess
}

// planLabel renders the plan pricing the date for the pass's log line.
func planLabel(p plan.Plan, hasPlan bool) string {
	if !hasPlan {
		return "none"
	}
	return p.ID
}

// windowLabel renders the resolved free window, or "none" when the day's plan
// has no free band.
func windowLabel(start, end string) string {
	if start == "" || end == "" {
		return "none"
	}
	return start + "-" + end
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
