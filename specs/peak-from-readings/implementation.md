# Implementation: Peak From Readings

Explanation of the implementation that shipped on branch `peak-from-readings`
(commits `531032b`..`HEAD`), written at three expertise levels, followed by a
Completeness Assessment mapping each smolspec requirement to code and tests.

---

## Beginner Level

### What This Does

The app's History screen shows how much grid electricity you imported during
"peak" hours each day. Until now the app worked that number out by subtraction:
it took the whole day's grid import (a very accurate number from the battery
inverter) and subtracted the off-peak portion (a less accurate number the
backend calculates). Subtracting a slightly-wrong number from an accurate one
dumps *all* of the error onto what's left — peak. On small peak days that error
was around 43% of the displayed value.

This change makes the backend calculate peak grid import **directly** instead of
by subtraction. It adds up the grid power readings (taken every 10 seconds)
during the two stretches of the day that sit *outside* the off-peak window —
before it and after it. Off-peak and peak are now measured the same way, so each
carries only its own small (~1.5%) measurement noise instead of one carrying
both.

### Why It Matters

The peak number on the History card is what you compare against your electricity
bill. When it's visibly wrong, it erodes trust in everything else on the
dashboard. After this change the peak figure lines up with reality to within a
few percent.

### Key Concepts

- **Grid import**: electricity you pull *from* the grid (as opposed to feeding
  solar back into it).
- **Off-peak window**: the cheap-tariff hours (configured as 11:00–14:00) when
  the battery is allowed to charge from the grid.
- **Peak**: everything outside the off-peak window — split into a morning piece
  (midnight → off-peak start) and an evening piece (off-peak end → next
  midnight).
- **Integration**: adding up many small instantaneous power readings to get
  total energy (kWh), like summing the area under a graph.
- **Sentinel**: a small "done" marker stored next to a row so a repeating job
  knows it already did the work and can skip it.
- **Fallback**: if the backend hasn't produced a peak value for a day (today, or
  very old days), the app quietly uses the old subtraction method so the screen
  is never blank.

---

## Intermediate Level

### Changes Overview

Three layers, one new field (`peakGridImportKwh`).

**Go backend — compute & store**
- `internal/derivedstats/integrate_offpeak.go`: new
  `IntegratePeakGridImportKwh(readings, dayStart, offpeakStart, offpeakEnd,
  dayEnd)` — calls the existing single-window integrator `IntegrateOffpeakDeltas`
  twice (over `[dayStart, offpeakStart)` and `[offpeakEnd, dayEnd)`), sums the
  `GridImportKwh` of each, and returns `ok=true` only when *both* sub-windows
  pass the usability gate.
- `internal/dynamo/models.go`: `DailyEnergyItem` and `DerivedStats` gain an
  optional `PeakGridImportKwh *float64` and a `PeakComputedAt` sentinel string.
- `internal/dynamo/dynamostore.go`: `UpdateDailyEnergyDerived` now builds its
  `SET` expression dynamically and writes the *derivedStats group* and the
  *peak group* independently, each gated on its own sentinel being non-empty.
- `internal/poller/dailysummary.go`: the hourly summarisation pass keeps the
  existing derivedStats block (gated on `DerivedStatsComputedAt`) and adds a
  parallel peak block (gated on `PeakComputedAt`). The pass skips a row only
  when *both* sentinels are set.

**Go backend — API surface**
- `internal/api/response.go`, `day.go`, `history.go`: `DaySummary` and
  `DayEnergy` expose `peakGridImportKwh` (JSON `omitempty`), populated from the
  stored value. No real-time computation for today.

**Go backend — historical backfill**
- `cmd/backfill-offpeak` → `cmd/backfill-grid`: the existing off-peak backfill
  CLI is renamed and extended. Per date it still recomputes the `flux-offpeak`
  row unchanged, and *additionally* computes peak over the full day and writes
  `peakGridImportKwh` + `peakComputedAt` to the matching `flux-daily-energy`
  row — after a `GetDailyEnergy` check so it never creates a phantom row. New
  `--table-daily-energy` flag.

**iOS**
- `FluxCore/Models/APIModels.swift`, `Flux/Models/CachedDayEnergy.swift`,
  `HistoryViewModel.swift`: `DayEnergy` and its cached form decode and persist
  the optional field.
- `Flux/History/HistoryDerivedState.swift`: `gridEntry` uses
  `day.peakGridImportKwh ?? max(0, day.eInput - offpeakImport)`.

### Implementation Approach

The guiding idea is **symmetry**: peak and off-peak are computed by the same
trapezoidal integrator over complementary windows, so their errors are
proportional to their own throughput rather than concentrated on a residual.
Reusing `IntegrateOffpeakDeltas` (rather than writing a fresh single-channel
integrator) guarantees the peak usability gate is byte-for-byte identical to the
off-peak one.

The **two-sentinel** design lets the peak field be filled onto rows that already
have derivedStats without recomputing or clobbering them. The same write path
(`UpdateDailyEnergyDerived`) serves both the hourly forward-fill and the
historical CLI.

Backfill is split by reach: the hourly pass only ever processes "yesterday", so
it *forward-fills* each new day; the renamed CLI handles the pre-deploy 30-day
history in one operator run, matching the established `backfill-offpeak` /
`backfill-solar` pattern.

### Trade-offs

- **Direct integration vs. calibration multiplier vs. snapshot scaling**
  (Decision 1): direct integration keeps provenance honest — the stored value is
  a clean integration of what was measured, not a fudged residual.
- **Store on `flux-daily-energy` vs. `flux-offpeak` vs. new table** (Decision 2):
  reuses the existing per-day summary row and its hourly write path.
- **Two sentinels vs. clearing the existing sentinel / versioning**
  (Decision 3): orthogonal lifecycles, no coordinated deploy, no unnecessary
  recompute of other stats.
- **CLI backfill vs. per-tick TTL scan vs. accepting reduced scope**
  (Decision 7): the CLI matches repo convention and keeps the hourly pass a
  single-row operation. This corrected an error in the original Decision 3,
  which had assumed the hourly pass would backfill history (it cannot — it only
  visits yesterday).
- **Grid-import only vs. all five channels** (Decision 5): YAGNI — no UI consumes
  peak solar/export/battery.

---

## Expert Level

### Technical Deep Dive

- **Gate composition.** `IntegratePeakGridImportKwh` returns early with
  `ok=false` if either sub-window's `IntegrateOffpeakDeltas` is unusable. The
  morning sub-window `[00:00, offpeakStart)` is the fragile one (overnight poller
  outages → sparse readings); a single sparse sub-window omits the whole day's
  peak and the iOS fallback applies. There is no partial-day peak.
- **DST correctness.** The window bounds are absolute unix timestamps derived
  from `time.ParseInLocation(dateLayout, date, Location)` + minute offsets, so
  23h/25h Sydney days integrate over the correct real-time span. The integrator
  never sees wall-clock; it only sees timestamps.
- **Write-path independence.** `UpdateDailyEnergyDerived` accumulates `sets`/
  `values` per group and no-ops when neither sentinel is set (DynamoDB rejects an
  empty `UpdateExpression`). This is what lets a pre-feature row — which already
  has `derivedStatsComputedAt` — receive only the peak group on a later pass.
- **Gate-failure still stamps the sentinel.** On a usability-gate miss the pass
  sets `PeakComputedAt` but leaves `PeakGridImportKwh` nil, so a permanently
  sparse past day is not retried every hour. Past-day readings are immutable once
  the day closes, so this is safe.
- **Phantom-row guard.** `UpdateItem` upserts, so `cmd/backfill-grid` issues a
  `GetDailyEnergy` first and skips peak when the row is absent. The GET's return
  value is also *used* — the prior peak prints in the `prev→new` operator summary
  line — so a `ConditionExpression(attribute_exists)` would be strictly worse
  here (it wouldn't surface the old value), and there is no concurrent writer
  (the CLI skips today, the poller's domain).
- **API has no today path.** For today `day.eInput` is already `max(stored,
  computed)`; the iOS fallback's error is bounded at no-worse-than-current. Peak
  is intentionally absent on today's row (Decision 4).

### Architecture Impact

- One more optional field and one more sentinel on `DailyEnergyItem`; readers
  must treat each as independently absent. The two-sentinel pattern generalises
  to future per-field derived stats with independent lifecycles.
- The renamed CLI now reads two tables (`flux-offpeak`, `flux-daily-energy`) and
  the readings table, and writes two tables. Off-peak recompute is byte-for-byte
  unchanged; peak runs as an independent per-date step.
- Storage-layer naming divergence is accepted (Decision 6): off-peak stores
  `OffpeakItem.GridUsageKwh`, peak stores `DailyEnergyItem.PeakGridImportKwh`;
  both surface as `*GridImportKwh` at the API boundary.

### Potential Issues / To Monitor

- **Rolling discontinuity** in History at deploy-time-minus-30-days until the
  backfill CLI is run: recent days use the integrated field, older days the iOS
  residual. Accepted (Decision 4); never worse than current production.
- **`peakGridImportKwh + offpeakGridImportKwh ≠ eInput` exactly** — they differ
  by ~1.5% of eInput (the shared sampling artifact). The 3% tolerance bound is
  asserted in tests; no UI shows all three side-by-side, so this is operator-
  visible only.
- **Today is still the residual everywhere, and the breakdown is gated on
  off-peak**: per Decision 4 there is no server peak for today, so the Dashboard,
  Day Detail, and History all fall back to the `eInput − offpeak` residual for
  today — and before the off-peak window opens (no off-peak split yet) today
  shows no peak/off-peak breakdown at all. This violates the `CLAUDE.md` Data
  Consistency rule for *today's* values. Tracked as follow-ups: T-1420 (live
  data into Day Detail/History today) and T-1421 (real-time peak on the
  Dashboard). Past-day Day Detail (summary row, Compare overlay, peak cost) was
  brought onto the server value in this spec via Decision 8.
- **Redundant channel work**: `IntegratePeakGridImportKwh` computes all five
  energy channels twice and uses only grid import. Bounded daily volume makes it
  negligible; it is the first target if this ever moves off the per-day cadence.

---

## Completeness Assessment

### Fully implemented (code + tests)

| Smolspec requirement | Implementation | Tests |
|---|---|---|
| MUST: peak = Σ `max(Pgrid,0)` over the two bracketing windows | `IntegratePeakGridImportKwh` (`integrate_offpeak.go`) | `integrate_peak_test.go` — both windows summed, per-sample clamp |
| MUST: peak + offpeak within 3% of `eInput` | same numerical method both windows | `integrate_peak_test.go` — `InDelta(dayKwh, total, dayKwh*0.03)` |
| MUST: omit field from row & JSON when the gate fails for either sub-window | nil `PeakGridImportKwh` + `omitempty` (dynamo + JSON tags) | store round-trip + API `NotContains` test; pass gate-failure test |
| MUST: expose at JSON key `peakGridImportKwh` on `/day` and `/history` | `response.go`, `day.go`, `history.go` | `peak_grid_import_test.go` — present + absent on both endpoints |
| MUST: forward-fill via hourly pass on independent sentinel + one-shot CLI backfill | `dailysummary.go` two-block pass; `cmd/backfill-grid` | `dailysummary_peak_test.go`; `cmd/backfill-grid/main_test.go` |
| SHOULD: iOS prefers server value, residual fallback | `HistoryDerivedState.gridEntry` | `HistoryViewModelOverviewTests`; `DayEnergyDecodingTests`; `CachedDayEnergyTests` |

### Intentionally not implemented (in scope decisions)

- **MAY: per-write peak drift log** — not implemented; MAY-level. The CLI still
  emits a per-day peak summary line, partially covering the intent.
- **Peak solar / export / battery channels** — excluded (Decision 5).
- **Real-time today peak path in the API** — excluded (Decision 4); iOS fallback.
- **Cross-TTL persistence for pre-30-day rows** — excluded (Decision 4).
- **`DayCosts` / Day-Detail `DaySummary` consuming the new field** — outside the
  spec's iOS scope (History only); flagged above as a future follow-up.

### Verification status

- Go: `make check` (fmt, vet, lint, full test suite) passes.
- iOS/macOS: `make ios-test` / `make macos-test` pass; SwiftLint clean on touched
  files (pre-existing baseline violations elsewhere unchanged).

### Operational follow-up (not code)

After deploying the new Lambda + poller image, run `cmd/backfill-grid` once
(with `--table-daily-energy`) to populate peak on the existing 30-day history.
Until then those days display the iOS residual fallback.

## Addendum: live peak for today across all three screens (T-1420 / T-1421, Decision 9)

Decision 4a deferred a real-time peak path for today and Decision 8 deferred the
Dashboard; both are superseded by **Decision 9**. The follow-up work adds a single
server-side `api.livePeakGridImport(readings, now, offpeakStart, offpeakEnd)` that
integrates `max(pgrid,0)` directly over the two windows bracketing off-peak —
morning `[00:00, min(now, opStart))` and evening `[opEnd, min(now, dayEnd))` —
clamped to `now`, **gated on the morning window only** (the evening window is empty
before 14:00, so `IntegratePeakGridImportKwh`'s both-windows gate can't serve the
partial day). It trims to the Sydney day internally so `/status` (rolling 24h) and
`/day` / `/history` (today-only) return an identical value, independent of
`reconcileEnergy`.

`peakGridImportKwh` is now exposed for **today** on `/status` (new `StatusResponse`
field), `/day`, and `/history`; past days keep the stored value. On iOS, the
History `gridEntry` and `SummaryBlock.gridInRows` render the peak/off-peak split
whenever a peak value is present (off-peak shown as `0` before the window opens),
falling back to the residual and collapsing to a combined row only when neither a
server peak nor an off-peak value is known (pre-30-day rows, **Decision 4b
retained**). Cross-screen equality is locked in by
`TestTodayPeakGridImportConsistentAcrossEndpoints`. Full detail in
`specs/bugfixes/today-peak-grid-import/report.md`.
