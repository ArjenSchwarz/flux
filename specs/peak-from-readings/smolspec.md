# Peak From Readings

## Overview

The iOS app shows daily peak grid import as `eInput − offpeakGridImportKwh` (Flux/Flux/History/HistoryDerivedState.swift:230) — a subtractive residual of two values measured by different methods. For **past days**, `eInput` comes from AlphaESS's sub-second internal counter (matches the household meter to within ~7 Wh/day) while `offpeakGridImportKwh` is Flux's trapezoidal integration of 10-second `Pgrid` samples (loses ~1.5% on continuous high-current charging). The sampling loss therefore lands entirely on peak — verified ~43% relative error against an OVO Energy meter export on 23 days of May 2026, on peak totals averaging ~0.45 kWh/day.

This spec adds a server-computed `peakGridImportKwh` field for past days, derived by integrating `Pgrid` over the non-off-peak portion of the day using the same trapezoidal method as `derivedstats.IntegrateOffpeakDeltas`. Peak and off-peak then each carry their own ~1.5% noise floor proportional to their own kWh, instead of the noise stacking entirely on peak.

## Empirical baseline

24 days, May 2026 (verified comparator vs OVO meter CSV; tool kept in repo via this branch's history). Δ = Flux − OVO, kWh.

| Metric     | mean Δ  | mean\|Δ\| | stddev | max\|Δ\| |
|------------|---------|-----------|--------|----------|
| gridImport | +0.007  | 0.104     | 0.118  | 0.225    |
| gridExport | −0.420  | 0.420     | 0.058  | 0.563    |
| peak       | +0.196  | 0.196     | 0.049  | 0.323    |
| offpeak    | −0.182  | 0.182     | 0.111  | 0.414    |

Peak Δ ≈ −offpeak Δ with gridImport Δ ≈ 0 → bucketing artifact, not a measurement bug. Mean |Δ| on off-peak grows ~linearly with off-peak kWh, supporting the proportional-loss model.

## Requirements

- The system **MUST** produce `peakGridImportKwh` per local-Sydney day as the sum of `max(Pgrid, 0)` integrations over `[day-start, offpeak-start)` and `[offpeak-end, next-day-start)`.
- The system **MUST** produce `peakGridImportKwh + offpeakGridImportKwh` within 3% of `eInput` on any day where all three are populated, reflecting that both integrations use the same numerical method with the same ~1.5% per-window sampling artifact.
- The system **MUST** omit `peakGridImportKwh` from the DynamoDB row and from the API JSON on days where the integration's usability gate fails for either sub-window.
- The system **MUST** expose `peakGridImportKwh` at JSON key `peakGridImportKwh` (omitted when absent) on both `/day` and `/history` responses, alongside the existing `offpeakGridImportKwh`.
- The system **MUST** populate `peakGridImportKwh` on existing rows automatically via the next eligible hourly summarisation pass after deploy, with no operator-run backfill step. (See Decision 3.)
- The iOS app **SHOULD** prefer the server-provided `peakGridImportKwh` when non-nil and fall back to `max(0, day.eInput − offpeakImport)` otherwise.
- The system **MAY** emit a structured log entry per peak write comparing the integrated value against the `eInput − offpeakGridImportKwh` residual, analogous to the existing `offpeak drift` line (internal/dynamo/offpeak_drift.go), for ongoing drift visibility.

## Implementation Approach

### Compute & store (Go, server)

- Add helper `IntegratePeakGridImportKwh(readings []Reading, dayStartUnix, offpeakStartUnix, offpeakEndUnix, dayEndUnix int64) (kwh float64, sampleCount int, skippedPairs int, ok bool)` in `internal/derivedstats/integrate_offpeak.go`. Calls the existing single-window integrator twice (over `[dayStart, offpeakStart)` and `[offpeakEnd, dayEnd)`), each with selector `max(Pgrid, 0)`, and returns sum + combined provenance + true iff both sub-windows pass the usability gate.
- Window args are unix timestamps to match the existing `IntegrateOffpeakDeltas` signature; the caller converts the HH:MM SSM config exactly as `cmd/backfill-offpeak/main.go:offpeakBoundaries` already does.
- Add `PeakGridImportKwh *float64 dynamodbav:"peakGridImportKwh,omitempty"` to `dynamo.DailyEnergyItem` (internal/dynamo/models.go:37) and `PeakComputedAt string` (idempotency sentinel; see Decision 3).
- Add matching fields to `dynamo.DerivedStats`. Extend `UpdateDailyEnergyDerived` (internal/dynamo/dynamostore.go:92) to set both attributes when non-nil.
- In `poller.runSummarisationPass` (internal/poller/dailysummary.go:41), gate the existing derived-stats block on `item.DerivedStatsComputedAt == ""` (as today) and add a parallel block gated on `item.PeakComputedAt == ""` that calls the new helper and writes `PeakGridImportKwh` + `PeakComputedAt`. Skip-result returns only when **both** sentinels are set.

### API surface

- Add `PeakGridImportKwh *float64 json:"peakGridImportKwh,omitempty"` to the day-summary struct (internal/api/response.go around :107) and the history-day struct (around :176).
- Populate from `deItem.PeakGridImportKwh` in `internal/api/day.go` at day.go:198 (after the live/stored energy reconcile) and in `internal/api/history.go` near history.go:154. No real-time computation path for today; today's row uses the iOS fallback (see Decision 4).

### iOS consumption

- Add optional `peakGridImportKwh: Double?` to `DayEnergy` and `CachedDayEnergy` (Flux/Flux/Models/CachedDayEnergy.swift:12-14 alongside the off-peak fields).
- Update `HistoryDerivedState.gridEntry` (Flux/Flux/History/HistoryDerivedState.swift:228-239): when `day.peakGridImportKwh` is non-nil, use it directly; otherwise keep the existing `max(0, day.eInput − offpeakImport)`.

### Out of scope

- Only the `peakGridImportKwh` channel is added. Peak versions of solar / export / battery charge / battery discharge are deliberately not computed (see Decision 5).
- Off-peak computation and storage are unchanged; `flux-offpeak` row shape, the off-peak CLI, and the `flux-offpeak.GridUsageKwh` field name (see Decision 6) are all untouched.
- No real-time peak computation path for today; today's row uses the iOS fallback (see Decision 4).
- No persistence beyond the 30-day `flux-readings` TTL; rows older than 30 days at deploy never get peak populated (see Decision 4).
- No change to `derivedstats.PeakPeriods` (top-3 high-load clusters — unrelated to peak grid import).
- No change to AlphaESS polling cadence, API authentication, or response envelope shape.
- No display changes beyond switching `gridEntry`'s source field; chart axes, colours, and labels stay as they are.

## Risks and Assumptions

- **Risk: the morning sub-window `[00:00, off-peak-start)` may have sparse readings on days when the poller was offline overnight.** Mitigation: the helper requires both sub-windows to pass the usability gate; either failing produces `ok=false` and the field is omitted. The iOS fallback then applies.
- **Risk: DST day boundaries (23h / 25h days in Sydney).** Mitigation: pass already computes `dayStart`/`dayEnd` via `time.ParseInLocation(dateLayout, date, p.cfg.Location).AddDate(0,0,1)` (dailysummary.go:67-69); the new helper takes unix args so the call site is naturally DST-correct.
- **Risk: rows older than 30 days at deploy will never receive `peakGridImportKwh`** because `flux-readings` (the integration input) has a 30-day TTL. Mitigation: this is accepted — the iOS fallback applies indefinitely for those rows, displaying the noisy-residual value the app already shows today. The discontinuity in History rolls forward with the TTL window but the user never sees a worse number than the current production calculation. Decision 4 captures the trade-off.
- **Assumption: the residual sampling loss is roughly proportional to energy throughput per window** (validated by the table above — peak Δ tightly bounded at 0.196 ± 0.049 across 23 days). Success criterion for the post-deploy comparison is "peak Δ proportional to peak kWh, similar magnitude to off-peak Δ", not "peak Δ near zero".
