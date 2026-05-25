# Design: Off-Peak From Readings

## Overview

Replace the `endE* − startE*` snapshot diff in `internal/poller/offpeak.go::computeOffpeakDeltas` and the snapshot-projection branch in `internal/api/compute.go::offpeakDeltas` with trapezoidal integration over the SSM window from the `flux-readings` table. The existing `derivedstats.integratePload` / `integratePpv` algorithm is the template — five new sibling integrals replace the snapshot math at the poller's window-end, the Lambda's live projection, and the new backfill CLI.

## Architecture

### Call graph after the change

```
poller.OffpeakScheduler.handleEnd        ─┐
api.buildOffpeak (live)                  ─┼─►  derivedstats.integrateOffpeakDeltas
cmd/backfill-offpeak.main                ─┘            │
                                                       ▼
                                          derivedstats.integrate (one helper,
                                                       5 channel selectors)
```

`integrateOffpeakDeltas(readings, startUnix, endUnix) → OffpeakDeltas` is the single new package-public entry point. It owns the five `integrate(...)` calls and the sample-count / skipped-pair tally for AC 5.4.

### Three call sites for the same primitive

| Call site | When | Window | What it produces |
|---|---|---|---|
| `OffpeakScheduler.handleEnd` (poller) | Once per day, ≤30 s after `offpeak-end` (AC 3.1) | `[offpeak-start, offpeak-end)` Sydney | Persists `complete` row with deltas + provenance |
| `api.buildOffpeak` (Lambda live path) | Per `/status` / `/day?date=today` request while window open | `[offpeak-start, min(now, offpeak-end))` | Transient response payload only |
| `cmd/backfill-offpeak.main` | One-time + on-demand | `[offpeak-start, offpeak-end)` Sydney per day | Overwrites historical rows (deltas + provenance) |

### Window-end finalisation state machine

The current scheduler fires `handleEnd` at exactly `wallClockTime(now, loc, OffpeakEnd)`. New flow inside `handleEnd`:

```
1. waitUntil(loopCtx, offpeak-end)         ── existing
2. waitForReadingAtOrAfter(drainCtx,        ── new (max 30 s)
       offpeak-end, pollInterval=2s)
3. queryReadings(drainCtx,
       offpeak-start.Unix(),
       offpeak-end.Unix())                  ── new
4. integrateOffpeakDeltas(readings, ...)    ── new
5. captureEndSnapshot (existing AlphaESS call) — kept per Decision 2
6. WriteOffpeak (conditional, see below)    ── modified
```

Step 2 polls the readings table every 2 s until either (a) a reading with `timestamp ≥ offpeak-end.Unix()` exists, or (b) the 30 s budget elapses. The existing 10-second poll cadence means (a) typically wins within 5–15 s. The `handleEnd` deadline of "≤5 min after window-close" in AC 3.2 is unaffected.

### Mid-window recovery and restart handling

`recoverMidWindow` currently rebuilds `o.startSnapshot` from the pending row's `StartE*` so that snapshot-diff can complete. With readings-integration, recovery does NOT need any in-memory state — the readings table holds the same data the original `handleStart` would have captured. The recovery method is simplified to a check that the pending row exists; the final deltas come from `QueryReadings` at the time `handleEnd` runs.

The start snapshot (per Decision 2) is still captured at window-open by `handleStart` and persisted on the pending row, but its presence is no longer load-bearing for the final deltas.

**Restart paths covered (AC 3.4):**

| Restart at | Behaviour |
|---|---|
| Before `offpeak-start` | `Run()` enters `positionBefore`; waits for start; normal flow. Unchanged. |
| During the window with pending row present | `Run()` enters `positionDuring`; calls `recoverMidWindow` (now a no-op state-wise, just confirms pending row exists); waits for `offpeak-end`; runs new `handleEnd`. Re-arm of the end-timer is the existing `waitUntil` in the `positionDuring` branch. |
| During the window with no pending row | `Run()` enters `positionDuring`; `recoverMidWindow` finds no row → logs "no pending record found, skipping today"; today is lost. Same as today. |
| Between `offpeak-end` and 24:00 with pending row | `Run()` enters `positionAfter`; new code: `GetOffpeak(today)` → if pending, run `handleEnd`'s integration path immediately (no waitForReading step, the boundary is already past). Writes `complete`. |
| Between `offpeak-end` and 24:00 with no pending row | `positionAfter` logs and skips — unchanged. |

**Concurrent `handleEnd` invocation (out of scope but contracted):** production runs `desiredCount=1` so no two pollers should run simultaneously. If they did, the second instance's `WriteOffpeakIfPendingOrAbsent` would fail (the first wrote `complete`) and that instance would log + skip. Two integrations would be computed and two drift log lines emitted; both consume the same readings and produce the same numbers, so the only cost is duplicated work and a duplicate log entry — acceptable.

### Concurrent writer guard

`WriteOffpeak` currently uses an unconditional `PutItem`. The CLI (AC 7.8) and the poller (AC 3.5) both need to avoid stomping each other. New write path:

| Writer | Condition | Behaviour on condition failure |
|---|---|---|
| Poller `handleEnd` | `attribute_not_exists(status) OR status = "pending"` | Log + skip (the CLI got there first or the row is already final from another instance — accept the existing value) |
| Backfill CLI | `status = "complete"` | Log + skip (don't overwrite a row mid-poll or pending) |

`DynamoStore.WriteOffpeak` keeps its current signature (used by `handleStart` for the pending-row write). Two new methods `WriteOffpeakIfPendingOrAbsent` and `WriteOffpeakIfComplete` are added on the `Store` interface for the two finalisation paths — full signatures in the next section.

**Race scenarios covered:**

- **Poller writes first, then CLI**: poller writes `complete`. CLI's `status = "complete"` condition succeeds; the CLI overwrites with the same integration-sourced value. AC 7.3 idempotence handles the byte-for-byte equality (subject to AC 7.7 rounding).
- **CLI writes first (e.g. backfill running just as the window closes today)**: CLI writes `complete`. Poller's `attribute_not_exists OR pending` condition FAILS (the row is `complete`). Poller logs and skips. The CLI's value stands.
- **Two poller instances**: production runs `desiredCount=1`, so the spec doesn't have to handle this. If it did, the second instance's condition fails (the row is `complete`) and it logs + skips.
- **Poller restart between window-end and the CLI's read**: covered by the post-`handleEnd` recovery path described above; the row reaches `complete` before any CLI run.

### Pattern Extension Audit

Each existing snapshot-diff consumer needs an explicit decision:

| Site | Existing behaviour | Needs change? | Rationale |
|---|---|---|---|
| `poller.computeOffpeakDeltas` | Returns `OffpeakItem` from `end − start` | **Replaced** | The five delta fields are now integration-sourced |
| `poller.handleEnd` | Calls `computeOffpeakDeltas` | **Replaced** | New flow per above |
| `poller.recoverMidWindow` | Loads `StartE*` into `o.startSnapshot` | **Simplified** | No longer load-bearing |
| `poller.handleStart` | Captures + persists start snapshot | **Unchanged** | Decision 2 keeps diagnostic snapshot |
| `api.offpeakDeltas` (complete branch) | Reads `op.GridUsageKwh` etc. | **Unchanged** | Reads the new integration-sourced values transparently |
| `api.offpeakDeltas` (pending branch) | Projects `current.EInput − op.StartEInput` | **Replaced** | New live-integration path in `api.buildOffpeak`. All references to `op.StartE*` / `op.EndE*` are removed from `compute.go`, enforcing AC 5.3 at compile-time (consumer code has no remaining reader). |
| `api.buildOffpeak` | Calls `offpeakDeltas(item, today)` | **Modified** | Calls `integrateOffpeakDeltas` for live path; reads row for finalised path |
| `api.history.go::offpeakSplit` | Calls `offpeakDeltas(op, energy, isItemToday)` | **Modified** | Same dispatch as `buildOffpeak` |
| `api.day.go::handleDay` | Calls `offpeakSplit` | **Unchanged** | Transitively uses new path via `offpeakSplit` |
| `derivedstats.integratePload` | Standalone utility | **Unchanged** | Stays as-is; design choice not to fold into the new generic (see Decision 9) |
| `derivedstats.integratePpv` | Standalone utility | **Unchanged** | Same as above |

## Components and Interfaces

### `internal/derivedstats/integrate_offpeak.go` (new)

```go
// OffpeakDeltas is the five integrated kWh values plus integration provenance.
type OffpeakDeltas struct {
    GridImportKwh       float64  // ∫ max(pgrid, 0)
    GridExportKwh       float64  // ∫ max(-pgrid, 0)
    BatteryChargeKwh    float64  // ∫ max(-pbat, 0)
    BatteryDischargeKwh float64  // ∫ max(pbat, 0)
    SolarKwh            float64  // ∫ max(ppv, 0)
    SampleCount         int      // readings with timestamp in [start, end)
    SkippedPairs        int      // consecutive pairs >60s apart
}

// IntegrateOffpeakDeltas returns the five integrated kWh values over
// [startUnix, endUnix) plus provenance counts. Returns (zero, false) when
// fewer than two usable samples (in-window or bracketing) exist — caller
// treats this as "missing record" per AC 1.6. Readings must be sorted by
// Timestamp ascending.
func IntegrateOffpeakDeltas(readings []Reading, startUnix, endUnix int64) (OffpeakDeltas, bool)
```

Implementation uses one internal helper:

```go
// integrate is the shared algorithm — same control flow as integratePpv in
// integrate_ppv.go but with two differences: (a) sample-count tallying is
// removed (callers handle it once at IntegrateOffpeakDeltas level, not five
// times); (b) selector returns the per-sample power value to integrate and is
// responsible for any clamping (e.g. max(r.Pgrid, 0)). Returns kWh
// (watt-seconds / 3,600,000). The `len(pts) >= 2` usability gate runs AFTER
// point construction, so a window with zero in-window readings but two
// bracketing readings inside 60 s on both sides produces a valid integral.
func integrate(readings []Reading, startUnix, endUnix int64,
    selector func(Reading) float64) float64
```

Boundary clipping (AC 1.5): if a reading at index `iL` exists outside the window with `readings[iL+1].Timestamp > startUnix` and `readings[iL+1].Timestamp - readings[iL].Timestamp ≤ 60s`, a synthetic point at `startUnix` is interpolated linearly from `selector(readings[iL])` and `selector(readings[iL+1])`. Same construction at `endUnix`. This is the existing `integratePload`/`integratePpv` algorithm — see `integrate.go` lines 66–116 for the canonical implementation.

**Usability gate** (AC 1.6): `IntegrateOffpeakDeltas` returns `(_, false)` when `integrate`'s internal `len(pts) < 2`. This correctly handles three cases together: (a) zero in-window readings and no usable bracketing pairs, (b) one in-window reading with no usable bracket, and (c) bracket-only intervals where the bracketing pairs themselves don't satisfy the 60 s rule. Bracket-only intervals with valid 60 s bracketing on both sides DO produce a valid integral, matching AC 1.6's "inside or bracketing".

`IntegrateOffpeakDeltas` calls `integrate` five times with selectors:

| Field | Selector |
|---|---|
| `GridImportKwh` | `func(r) float64 { return max(r.Pgrid, 0) }` |
| `GridExportKwh` | `func(r) float64 { return max(-r.Pgrid, 0) }` |
| `BatteryChargeKwh` | `func(r) float64 { return max(-r.Pbat, 0) }` |
| `BatteryDischargeKwh` | `func(r) float64 { return max(r.Pbat, 0) }` |
| `SolarKwh` | `func(r) float64 { return max(r.Ppv, 0) }` |

`SampleCount` counts readings with `Timestamp ∈ [startUnix, endUnix)`. `SkippedPairs` counts adjacent pairs within that same range separated by >60 s. Both are computed in a single linear pass over the in-window readings before the five `integrate` calls; the five calls do not contribute to the counts (synthesised edge points aren't real samples and skipped pairs are identical across all five selectors since the gap rule looks only at timestamps). Total work is `O(n) + 5·O(n)` = `O(n)` for the typical 1080-reading window.

### `internal/dynamo/store.go` (modified)

Two named methods rather than a single conditional with an enum — each call site reads as its intent, no `iota` default hazard, and the conditional-expression strings differ enough that the two writers exercise genuinely different DynamoDB code paths:

```go
type Store interface {
    // existing methods...

    // WriteOffpeakIfPendingOrAbsent is the poller's window-end write. Fails
    // with ErrOffpeakConditionFailed if the row's status is "complete"
    // (someone else finalised first).
    WriteOffpeakIfPendingOrAbsent(ctx context.Context, item OffpeakItem) error

    // WriteOffpeakIfComplete is the backfill CLI's write. Fails with
    // ErrOffpeakConditionFailed if the row's status is anything other than
    // "complete" — protects rows mid-poll (no status / pending).
    WriteOffpeakIfComplete(ctx context.Context, item OffpeakItem) error
}
```

DynamoDB conditional expressions:
- `WriteOffpeakIfPendingOrAbsent`: `attribute_not_exists(#status) OR #status = :pending`
- `WriteOffpeakIfComplete`: `#status = :complete`

Both map `ConditionalCheckFailedException` to a sentinel `ErrOffpeakConditionFailed` that callers log-and-skip. The existing unconditional `WriteOffpeak` stays — used by `handleStart` for the pending-row write at window-open (which must succeed unconditionally).

### `internal/dynamo/models.go` (modified)

```go
type OffpeakItem struct {
    // ... existing fields unchanged ...

    // Provenance (new, AC 5.4)
    IntegrationSampleCount  int    `dynamodbav:"integrationSampleCount,omitempty"`
    IntegrationSkippedPairs int    `dynamodbav:"integrationSkippedPairs,omitempty"`
    IntegratedAt            string `dynamodbav:"integratedAt,omitempty"` // RFC3339 UTC
}
```

`omitempty` so rows backfilled before this change (or rows where readings weren't usable) don't carry zero-valued fields that mislead.

### `internal/poller/offpeak.go` (modified)

```go
// handleEnd replaces the existing implementation.
// The start snapshot is still captured at window-open (Decision 2) but the
// final deltas come from integrating readings, not from end − start.
func (o *OffpeakScheduler) handleEnd(ctx context.Context, date string) error
```

New private helper:

```go
// waitForReadingAtOrAfter polls the readings table every pollInterval until
// either a reading at-or-after target exists or the budget expires. Returns
// (true, _) on found, (false, nil) on timeout, (false, err) on store error.
func (o *OffpeakScheduler) waitForReadingAtOrAfter(
    ctx context.Context,
    target time.Time,
    budget time.Duration,
    pollInterval time.Duration,
) (found bool, err error)
```

### `internal/api/compute.go::offpeakDeltas` (modified)

The function signature stays the same so existing call sites in `api/history.go::offpeakSplit` and `api/status.go::buildOffpeak` are unchanged. The implementation drops the `OffpeakStatusPending` branch's snapshot projection — that path now signals "live-integrate at the caller" by returning `(_, false)`:

```go
// offpeakDeltas resolves deltas for a stored record (status == complete).
// For pending records, returns (_, false). Callers needing today's
// in-window value live-integrate via api.liveOffpeakDeltas.
func offpeakDeltas(op dynamo.OffpeakItem) (offpeakDeltaValues, bool)

// liveOffpeakDeltas integrates readings over [offpeakStart, min(now, offpeakEnd))
// for today. Used by buildOffpeak and offpeakSplit when the row is pending.
func liveOffpeakDeltas(readings []dynamo.ReadingItem, now time.Time,
    windowStart, windowEnd time.Duration) (offpeakDeltaValues, bool)
```

**Readings source for the live path.** `liveOffpeakDeltas` receives the readings slice from its caller (`buildOffpeak` / `offpeakSplit`), which obtains them from the existing today-readings query that already runs for every `/status` and `/day?date=today` request (`history.go` lines 73-77, `status.go` and `day.go` similarly). No additional DynamoDB query is added — the integration consumes a slice that's already in memory for the existing live-compute path. This is what keeps AC 8.2's 500 ms p95 budget achievable: the extra cost is a single linear pass over ≤1080 readings, not a new round-trip.

**SSM sampling per request.** `h.offpeakStart` / `h.offpeakEnd` are populated in `main.go` at handler construction from SSM. A warm Lambda container reuses the parsed values for the life of the container; a cold container re-fetches. A mid-day SSM change is therefore picked up at the next cold start, NOT at the next request on a warm container. AC 4.4's monotonicity-modulo-SSM-change consequence covers this. (Tightening this to per-request SSM reads would add a per-request SSM call to every Lambda invocation — out of scope; deferred.)

**Determinism contract for `liveOffpeakDeltas`.** Given the same readings slice, same `now`, same window, the function returns the same value. No internal state, no clock dependency beyond the explicit `now` parameter. This is the basis for AC 4.4's monotonicity property test (PBT).

### `cmd/backfill-offpeak/main.go` (new)

Modelled on `cmd/backfill-solar/main.go`:

```
--serial            (or env SYSTEM_SERIAL)
--table-offpeak     (or env TABLE_OFFPEAK)
--table-readings    (or env TABLE_READINGS)
--from              (default: today - 30d, Sydney)
--to                (default: yesterday, Sydney)
--offpeak-start     (or env OFFPEAK_START)
--offpeak-end       (or env OFFPEAK_END)
--dry-run
```

`--to` defaults to yesterday, enforcing AC 7.2 (skip today). The CLI calls `DynamoReader.QueryOffpeak` then `DynamoReader.QueryReadings` per day, runs `IntegrateOffpeakDeltas`, and writes via `WriteOffpeakIfComplete`. The per-day summary line is:

```
2026-05-18  prev: peak=1.74 off=18.95   new: peak=0.27 off=20.42  Δpeak=-1.47  samples=1068  skipped=0
```

Rows with `len(readings) < 2` in the window emit `SKIPPED (sparse readings)` and leave the row untouched.

### Drift logging (Requirement 6)

A shared logger function `LogOffpeakDrift(date string, item dynamo.OffpeakItem)` lives in `internal/dynamo/offpeak_drift.go` and is called from both `handleEnd` and the backfill CLI immediately before the write. It lives in the `dynamo` package so the CLI doesn't have to import `internal/poller` to call it — `OffpeakItem` is already defined there.

The logical schema of one entry is:

```
date=<YYYY-MM-DD>
snapshotGrid=<endE_input - startE_input>     integratedGrid=<gridUsageKwh>     driftGrid=|integrated - snapshot|
snapshotSolar=<endEpv - startEpv>            integratedSolar=<solarKwh>        driftSolar=|...|
snapshotCharge=<endECharge - startECharge>   integratedCharge=<chargeKwh>      driftCharge=|...|
snapshotDischarge=...                        integratedDischarge=...           driftDischarge=...
snapshotExport=<endEOutput - startEOutput>   integratedExport=<exportKwh>      driftExport=|...|
```

The actual emission goes through `slog.Info` and inherits the handler configured by the binary (JSON for the poller via `cmd/poller/logging.go`; Text for the backfill CLI per the pattern of the sibling CLIs in `cmd/backfill-*`). CloudWatch Logs Insights parses both forms via `fields date, driftGrid, …`. No metric/alert is in scope (Decision 6).

## Data Models

Three new fields on `OffpeakItem` per AC 5.4 — `IntegrationSampleCount`, `IntegrationSkippedPairs`, `IntegratedAt`. All three are `omitempty` so old rows survive a marshalling round-trip without spurious zero values. No DynamoDB schema migration is required (DynamoDB is schemaless at the attribute level; `omitempty` plus tolerant unmarshal handles absent attributes).

## Error Handling

| Failure | Behaviour |
|---|---|
| `QueryReadings` returns error inside `handleEnd` | Existing path: delete the pending row + log error. Status quo for poller. |
| `QueryReadings` returns paginated results spanning >1 page | The existing `queryAll[ReadingItem]` helper (`internal/dynamo/reader.go`) already paginates exhaustively via `LastEvaluatedKey`; the design relies on that — no new pagination logic needed. A 24h fault-injection scenario that exceeds 1 MB falls under the existing primitive. |
| `handleEnd`'s `QueryReadings` racing the live readings write | `handleEnd` issues the query AFTER the wait-for-reading step has observed the at-or-after-boundary reading. The integration query SHALL be strongly consistent so the observed reading is guaranteed to be included. Implementation: extend the `Store.QueryReadings` signature (the poller's interface, distinct from the API Lambda's `Reader.QueryReadings`) to accept an opts struct with a `ConsistentRead` flag, or add a sibling `QueryReadingsConsistent` — the existing API Lambda paths stay on eventually-consistent reads (their existing behaviour). |
| `IntegrateOffpeakDeltas` returns `(_, false)` (sparse) | Persist a `complete` row with zero deltas and the three provenance fields. Downstream API layer treats `SampleCount == 0` the same as "no row" per AC 1.6. Backfill CLI: log SKIPPED, leave row untouched. |
| `WriteOffpeakIfPendingOrAbsent` returns `ErrOffpeakConditionFailed` | Log `warn` with the condition + writer identity; no error propagated. The other writer's value is accepted. |
| SSM parsing failure | Existing behaviour: skip the day's snapshot + log. New code emits a warning ("offpeak window unset, integration skipped"). |
| Readings table is empty for the window | `(zero, false)` → same as sparse. Row is written with zero deltas and `SampleCount == 0`. |

## Testing Strategy

### Unit tests

| Test | Asserts |
|---|---|
| `derivedstats.IntegrateOffpeakDeltas` — happy path | All five deltas non-negative; `Grid* + Grid* == ∫ |pgrid|`; samples = inputs in window |
| `IntegrateOffpeakDeltas` — single sample in window | Returns `(_, false)` (AC 1.6) |
| `IntegrateOffpeakDeltas` — bracketing sample at window edge | Linear interpolation contributes as per `integratePload` |
| `IntegrateOffpeakDeltas` — 90s gap inside window | Pair-gap rule triggers; `SkippedPairs == 1`; missed energy excluded |
| `IntegrateOffpeakDeltas` — signed pgrid (import + export) | Per-sample split: positive integrates as import, negative as export; both non-negative |
| `IntegrateOffpeakDeltas` — reading exactly at `endUnix` | Excluded as interior sample; still usable as right-edge bracket per `[start, end)` semantics |
| `IntegrateOffpeakDeltas` — `gridUsageKwh + peak == eInput` invariant | Asserts AC 2.3 structurally by construction: when integrated over the full day's window, the off-peak split + peak sum to `eInput` (within rounding tolerance) on a fixture |
| `poller.handleEnd` — happy path | `WriteOffpeakIfPendingOrAbsent` called with complete row + provenance |
| `poller.handleEnd` — no reading ≥ offpeak-end after 30 s budget | Falls through; integrates what exists; row written with `IntegratedAt` set |
| `poller.handleEnd` — `WriteOffpeakIfPendingOrAbsent` returns `ErrOffpeakConditionFailed` | Logged at warn, no error returned |
| `poller.Run` — restart in `positionAfter` with pending row | New recovery path runs `handleEnd`-from-readings immediately (no waitForReading); writes `complete` |
| `poller.Run` — restart in `positionAfter` with no row | Existing log+skip behaviour preserved |
| Concurrent-write race — poller and CLI both target the same date | Poller wins (CLI condition fails) or CLI wins first (poller condition fails); both produce identical delta values per AC 7.3, so the surviving row is correct either way |
| `api.liveOffpeakDeltas` — now < offpeak-start | Returns `(_, false)` |
| `api.liveOffpeakDeltas` — now == offpeak-start + 30 min | Integrates first 30 minutes |
| `api.liveOffpeakDeltas` — now ≥ offpeak-end | Integrates full window |
| `api.buildOffpeak` — pending row + readings | Calls `liveOffpeakDeltas`, ignores `op.StartE*` |
| `api.buildOffpeak` — complete row | Returns `op.GridUsageKwh` etc. directly |
| `cmd/backfill-offpeak` — dry-run | No `UpdateItem` calls; summary printed |
| `cmd/backfill-offpeak` — `WriteOffpeakIfComplete` returns `ErrOffpeakConditionFailed` | Row skipped + reported in summary; CLI exits 0 |
| `cmd/backfill-offpeak` — sparse readings | SKIPPED in summary, no write |

### Property-based tests (PBT)

`IntegrateOffpeakDeltas` has natural universal properties; use `pgregory.net/rapid` for these (already in the repo per the language rules):

| Property | Generator |
|---|---|
| **Closure under window**: `GridImport + GridExport ≤ ∫ |pgrid|` for any subwindow ⊂ full window | random reading list + random sub-interval |
| **Monotonicity over window growth**: for `now₂ > now₁` within `[offpeak-start, offpeak-end)`, every delta from the longer integration ≥ the shorter (AC 4.4, also a structural invariant) | random reading list + two `now` values |
| **Per-sample clamping consistency**: replacing one reading's `pgrid = v` with `pgrid = -v` SHALL swap the import/export contribution for that pair (modulo bracketing) | random reading list |
| **Round-trip**: integrating the same readings twice yields identical results (idempotence, AC 7.3) | random reading list |
| **Empty/single-sample**: `len(readings) < 2` always returns `(_, false)` | constrained generator |

### Integration / regression tests

- **2026-05-18 fixture test**: deterministic test fixture with the actual 13:50-14:10 readings from the bug report. Asserts AC 2.1: post-integration peak ≤ 0.25 kWh on this day.
- **Structural bound test**: across a generator of 30 random days, `gridUsageKwh ≤ eInput` for every row (AC 2.2).
- **Backfill idempotence**: run the CLI twice against the same fixtures; second run produces identical writes (AC 7.3 + AC 7.7 rounding consistency).
- **Drift log assertion**: parse the emitted log line; verify all five drift values are present and numerically correct against the fixture.

### Performance

The 2 s budget of AC 8.1 is verified by a benchmark in `internal/derivedstats` over 1080 synthetic readings. The 500 ms p95 budget of AC 8.2 is asserted by an existing `integration` test pattern (the repo has a Lambda integration test scaffold per CLAUDE.md); a new case integrates a full window's readings end-to-end and records the measured p95 in the test output for `design.md` to reference.
