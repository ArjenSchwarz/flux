# Design: Solar by Block

## Overview

Add a `solarKwh` value per daylight `DailyUsageBlock` (`morningPeak`, `offPeak`, `afternoonPeak`), populated by a trapezoidal integration of `ppv` over the block's `[start, end)` window. Persist via the existing poller summarisation pass; backfill historical rows with a one-shot CLI; render inline on the iOS Day Detail five-block panel.

## Architecture

### Persistence model — verified, not assumed

The Lambda `/day` handler does **not** write back to DynamoDB. The only write path for `DailyUsageBlock` is the poller's `runSummarisationPass` (see `internal/poller/dailysummary.go:41`), which runs once per day for "yesterday", guarded by the `DerivedStatsComputedAt` sentinel. This invalidates the reviewer concern about ~360 writes/hour from the live API; there is no live write path.

Implication: requirement 2.2 (persist when samples present) and 2.3 (skip when no samples) are satisfied without changing the API. They translate to:

| Path | Behaviour |
|---|---|
| Poller summarisation (new days post-deploy) | `Blocks()` already produces solar values; `DailyUsageToAttr` carries them into the existing atomic `UpdateDailyEnergyDerived` write. No change to the poller. |
| Live `/day` for today | `Blocks()` returns solar values in the response; nothing is written. |
| Past dates (read) | Existing read path through `DailyUsageFromAttr` carries the new field unchanged once the attribute exists. |
| Days summarised before deploy | Sentinel is set, so the poller will not recompute. The backfill CLI is the only path to populate solar on these rows. |

### Integration function

Add `integratePpv(readings, startUnix, endUnix)` mirroring `integratePload` (`internal/derivedstats/integrate.go`): same trapezoidal algorithm, same 60s gap rule, same edge synthesis, same half-open `[start, end)`. Negative `ppv` (sensor noise / inverter idle) is clamped to zero before integration, matching `integratePload`'s `max(p.Pload, 0)` treatment.

The two functions share enough that the implementation may be a single helper that takes a field selector closure, but readability outweighs the ~50 lines saved and the two call sites are stable. Implement as a sibling function.

### `Blocks()` change

In `internal/derivedstats/blocks.go`, the existing two-pass loop already iterates `withDuration`. Extend it: for blocks of kind `morningPeak`/`offPeak`/`afternoonPeak`, scan readings once for the block window — count samples and accumulate the trapezoidal integral via `integratePpv`. Set `pendingBlock.unroundedSolarKwh` to the integral and `pendingBlock.solarSampled = true` when at least one reading falls inside `[start, end)`. (Folding the count into the existing first-pass scan at lines 60–73 isn't possible — that scan runs before block boundaries are known. The added cost is one O(n) walk per daylight block, negligible against the existing two-pass integration.)

`Blocks()` makes the kind decision: it only invokes the solar pass for `morningPeak`/`offPeak`/`afternoonPeak`. `buildDailyUsageBlock` stays a pure formatter — when `pendingBlock.solarSampled` is true it emits a rounded `*float64` (using the existing `roundEnergy` helper), otherwise it emits `nil`. A daylight block whose readings all sum to zero (heavy fog, single reading at zero) emits `&0.0`. A block with zero readings emits `nil`.

### Live-path null semantics

For today's in-progress day, a daylight block whose `[start, end)` is entirely in the future contains zero `ppv` samples → `solarSampled = false` → `solarKwh = nil`. This matches AC 1.4 (no samples → null). When the block is partially elapsed and has at least one sample, the integral over the elapsed portion is reported (AC 1.5).

### Wire format and decoder contract

| Layer | Field | Encoding |
|---|---|---|
| Go `derivedstats.DailyUsageBlock` | `SolarKwh *float64` | `json:"solarKwh,omitempty"` |
| Go `dynamo.DailyUsageBlockAttr` | `SolarKwh *float64` | `dynamodbav:"solarKwh,omitempty"` |
| iOS `DailyUsageBlock` | `solarKwh: Double?` | `Decodable` (null/absent both decode to nil) |

`omitempty` on the Go side means a `nil` value is encoded as a missing JSON field, not an explicit `null`. Swift `Decodable` treats both forms as `nil` for an `Optional<Double>`, so the contract is unambiguous: either side may emit either form, and both decode the same. iOS decoder tests cover both.

### Backfill CLI

Location: `cmd/backfill-solar/main.go`. Standalone `main` package, ARM64 Linux build target matches the existing `cmd/api` and `cmd/poller`. Run locally with the operator's AWS credentials.

The CLI **patches `solarKwh` field-by-field on existing blocks** rather than rewriting the whole `dailyUsage` map. This preserves the historically-stored `totalKwh`, `start`/`end`, `boundarySource`, `percentOfDay`, and `status` values exactly — which is essential because readings have a 30-day TTL, and re-running `Blocks()` against partially-pruned readings could shift first/last-solar boundaries and change `totalKwh` away from the originally summarised values.

Algorithm:

1. Read `--dry-run` flag and `--from` / `--to` date bounds (default: from 30 days before today, to yesterday — the practical readings TTL window). Date strings parsed as `time.ParseInLocation("2006-01-02", date, sydneyTZ)` to match the poller and `Blocks()`.
2. Query `flux-daily-energy` rows for the configured `sysSn` and date range.
3. For each row whose `dailyUsage` exists and at least one daylight block is missing `solarKwh`:
   a. Query `flux-readings` for that date.
   b. If readings empty, skip (log).
   c. Run `derivedstats.Blocks(readings, offpeakStart, offpeakEnd, date, date, time.Now().In(sydneyTZ))`.
   d. Take the existing `dailyUsage` from the row (deep copy or rebuild). For each daylight block in the recomputed result whose `SolarKwh` is non-nil, find the existing block with the same `Kind` in the stored row and patch its `SolarKwh`. Discard everything else from the recomputed result.
   e. Emit a DynamoDB `UpdateItem` with `SET dailyUsage = :du` carrying the patched attribute. (Alternative deeply-nested per-block updates are messier and offer no real benefit since the attribute is small.)
4. Honour `--dry-run`: log the intended writes (date, block kind, kWh) without calling `UpdateItem`.

Why `time.Now()` is safe for the today-gate: passing `today=date` plus a real wall-clock `now` makes `isToday=true` inside `Blocks()`. The today-gate's `recentSolar` flag is set only when a reading lands in `[now-5min, now]`. For any date `< today`, the readings carry that date's timestamps and cannot fall inside the live 5-minute window, so `recentSolar=false` and `solarStillUp=false`. The today-gate stays inert. (`Blocks()` then proceeds with `lastSolar` from the readings rather than the sunset fallback, which is what we want.)

Why this doesn't race the poller: the poller writes `yesterday` once per day, gated by `derivedStatsComputedAt`. The backfill's default `--to` is `yesterday`. If both run concurrently on the same `yesterday` row, last-writer-wins; both produce the same `solarKwh` per block (deterministic algorithm + same readings + same offpeak window), so the resulting state is field-equivalent. The patched `dailyUsage` has identical other fields by construction.

Concurrency / idempotency:

- Backfill operates on dates `< today`. The poller writes only `yesterday` per tick; overlap is at most one date per run.
- Re-running with no new readings finds every block already has `solarKwh` and skips (AC 2.5).
- Throttling: serialise writes; ≤30 rows × on-demand table is well under any limit.

### Pattern extension audit

`DailyUsageBlock` is read or transformed in the following sites; the table records whether each needs a parallel change for `SolarKwh`.

| Site | Path | Needs change |
|---|---|---|
| Backend struct | `internal/derivedstats/types.go` `DailyUsageBlock` | **yes** — add `SolarKwh *float64` |
| Backend formatter | `internal/derivedstats/blocks.go` `buildDailyUsageBlock` | **yes** — populate field per `pendingBlock.solarSampled` |
| Pending block | `internal/derivedstats/blocks.go` `pendingBlock` | **yes** — add `unroundedSolarKwh float64`, `solarSampled bool` |
| Block builder loop | `internal/derivedstats/blocks.go` `Blocks()` | **yes** — call `integratePpv` for daylight blocks |
| New helper | `internal/derivedstats/integrate.go` (or new file) | **yes** — add `integratePpv` |
| DynamoDB attr struct | `internal/dynamo/models.go` `DailyUsageBlockAttr` | **yes** — add `SolarKwh *float64 dynamodbav:"solarKwh,omitempty"` |
| Storage→runtime conv | `internal/dynamo/derived_conv.go` `DailyUsageFromAttr` | **yes** — explicit `SolarKwh: b.SolarKwh,` line in loop body |
| Runtime→storage conv | `internal/dynamo/derived_conv.go` `DailyUsageToAttr` | **yes** — explicit `SolarKwh: b.SolarKwh,` line in loop body |
| Conv unit tests | `internal/dynamo/derived_conv_test.go` | **yes** — round-trip with present + nil |
| Conv property test | `internal/dynamo/derived_conv_property_test.go` | **yes** — generator emits `SolarKwh` as `Maybe[float64]` |
| Sizing test fixture | `internal/dynamo/sizing_test.go` | **yes** — populate `SolarKwh` on the three daylight blocks; reassert <4 KB |
| E2E integration test | `internal/integration/derivedstats_e2e_test.go` | **yes** — assert `SolarKwh` survives the real DynamoDB Local round-trip |
| API alias | `internal/api/response.go` `DailyUsageBlock = derivedstats.DailyUsageBlock` | **no** — alias picks up new field |
| Live `/day` handler | `internal/api/day.go` | **no** — calls `Blocks()` and `DailyUsageFromAttr` already |
| `/day` derived-stats test | `internal/api/day_derivedstats_test.go` | **yes** — add expectation that daylight blocks carry `solarKwh` |
| `/history` handler | `internal/api/history.go:165` | **no** — passes attr through; field flows into response transparently (Non-Goal: not surfaced on History UI yet) |
| `/history` test fixtures | `internal/api/history_bench_test.go`, `internal/api/history_derivedstats_test.go` | **yes** — populate `SolarKwh` to keep coverage current |
| Poller summarisation | `internal/poller/dailysummary.go` | **no** — calls `DailyUsageToAttr(Blocks(...))` already |
| Poller summarisation test | `internal/poller/dailysummary_test.go` | **yes** — assert `SolarKwh` is written for daylight blocks |
| Backfill CLI | `cmd/backfill-solar/main.go` (new) | **yes** — implement per algorithm above |
| Backfill CLI test | `cmd/backfill-solar/main_test.go` (new) | **yes** — dry-run, write, idempotency, in-place patch preservation |
| iOS `DailyUsageBlock` model | `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift:292` | **yes** — add `solarKwh: Double?` and extend `init` with `solarKwh: Double? = nil` default to keep existing call sites compiling |
| iOS five-block panel | `Flux/Flux/DayDetail/DayInFiveBlocksPanel.swift:46` | **yes** — render solar inline on daylight rows |
| iOS DailyUsageCard | `Flux/Flux/DayDetail/DailyUsageCard.swift` | **review** — separate panel that renders `DailyUsageBlock`; current scope leaves its display unchanged. Still needs preview fixtures updated to construct blocks with the new field (default `nil` keeps preview output identical). |
| iOS panel preview fixtures | `DayInFiveBlocksPanel.swift` `#Preview`, `DailyUsageCard.swift` `#Preview` | **yes** — at minimum the five-block preview demonstrates the new field; card preview can keep nil |
| iOS decoder tests | `Flux/Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift` | **yes** — fixtures exercise `"solarKwh": 0.0`, `"solarKwh": null`, and absent-key cases |
| iOS view-model tests | `DayDetailViewModelTests.swift`, `HistoryViewModelDailyUsageTests.swift`, `HistoryViewModelOverviewTests.swift`, `HistoryViewModelCacheUpsertTests.swift`, `CachedDayEnergyTests.swift`, `MockFluxAPIClient.swift` | **no** — default `solarKwh: nil` keeps positional/named initialisers source-compatible |
| iOS SwiftData cache | `Flux/Flux/Models/CachedDayEnergy.swift` | **no** — stores `DailyUsage?` as a Codable nested struct; SwiftData persists the JSON encoding so adding an optional field is non-breaking. (Verify by exercising the existing cache decoding test with an old fixture.) |
| macOS app | shares `FluxCore` and `DayInFiveBlocksPanel` via `Flux/Flux/Mac/...` setup files | **review** — same panel renders on macOS; visually verify the row layout doesn't pinch in narrow window mode. No code change expected. |

`/history` returns `DailyUsage` already (`internal/api/response.go:103`), so the new field rides along on that endpoint at zero design cost. Surfacing it on History remains out of scope per requirements.

## Components and Interfaces

```go
// internal/derivedstats/integrate.go (or sibling integrate_ppv.go)

// integratePpv returns the trapezoidal integral of max(ppv, 0) over the
// half-open interval [startUnix, endUnix), expressed in kWh, plus the count
// of readings whose timestamp falls inside that window. The count lets
// callers distinguish "no samples" (emit nil) from "samples summed to zero"
// (emit 0.0). Same trapezoidal algorithm and 60s pair-gap rule as
// integratePload; negative ppv values are clamped to zero.
func integratePpv(readings []Reading, startUnix, endUnix int64) (kwh float64, sampleCount int)
```

```go
// internal/derivedstats/types.go

type DailyUsageBlock struct {
    Kind              string   `json:"kind"`
    Start             string   `json:"start"`
    End               string   `json:"end"`
    TotalKwh          float64  `json:"totalKwh"`
    SolarKwh          *float64 `json:"solarKwh,omitempty"` // NEW
    AverageKwhPerHour *float64 `json:"averageKwhPerHour,omitempty"`
    PercentOfDay      int      `json:"percentOfDay"`
    Status            string   `json:"status"`
    BoundarySource    string   `json:"boundarySource"`
}
```

Behavioural contract for `SolarKwh`:

- `nil` for any `kind` ∈ {`night`, `evening`}.
- `nil` for daylight blocks whose `[start, end)` interval contains zero readings.
- A rounded non-negative `float64` otherwise (rounded with the existing `roundEnergy` helper to keep precision parity with `TotalKwh`).

## Data Models

Only the three `DailyUsageBlock` representations change (Go struct, DynamoDB attr struct, iOS struct). All three add a single optional float field; all use the project's existing optional-with-omit convention. No new tables, no new top-level wire fields.

## Error Handling

No new failure modes. The integration function returns `0` for impossible inputs (already covered in `integratePload`); the persistence path is the existing `UpdateDailyEnergyDerived` with no condition expression; the backfill CLI logs and continues on per-row read errors.

## Testing Strategy

### Backend unit tests

Add to `internal/derivedstats/blocks_test.go` and a new `integrate_ppv_test.go`:

- `integratePpv` mirrors `integratePload`'s test cases: empty readings, single reading, gap-skip, edge synthesis, negative-ppv clamping. Each case asserts both the `kwh` and `sampleCount` returns.
- `Blocks()` test cases (per AC 4.1 and the in-progress refinement):
  1. **Sunny day, full readings**: each daylight block has a non-nil `SolarKwh` proportional to its window; sum is sensible.
  2. **Winter low-solar day**: daylight blocks emit small but non-nil values.
  3. **Reading gap inside daylight block**: integral skips the gap (per `maxPairGapSeconds`); block still emits a non-nil value (because `sampleCount > 0`).
  4. **Morning peak collapsed to zero duration** (sunrise after off-peak start → block omitted by existing degenerate-omit): no entry produced; nothing to assert about `SolarKwh` for that kind.
  5. **In-progress today, daylight block straddling `now`**: block whose `start ≤ now < end` is clamped to `[start, now)` and `SolarKwh` reflects the elapsed portion. Block whose `start > now` is omitted by future-omit (no entry).
  6. **Daylight block with no readings at all** (e.g., poller outage covering the whole window): `SolarKwh` is nil (`sampleCount == 0`).
  7. **Daylight block with readings whose ppv is all zero** (heavy fog): `SolarKwh` is `&0.0` (sampled but integrates to zero).
  8. **Daylight block with a single reading** (only one timestamp inside the window, no bracket on either side): integration returns 0 because `len(pts) < 2`; `sampleCount = 1` so the block emits `&0.0`. Documented as expected behaviour.
  9. **Night and evening blocks**: `SolarKwh` is always nil regardless of input.

### Round-trip tests

Add to `internal/dynamo/derived_conv_test.go` and `derived_conv_property_test.go`: an existing block with `SolarKwh = nil` and one with `SolarKwh = &x` both round-trip through `ToAttr` → `FromAttr` unchanged. Property test extends to generate `SolarKwh` as `Maybe[float64]`.

### Poller summarisation test

Extend `internal/poller/dailysummary_test.go`: a fixture day with synthetic `ppv` readings produces a `DailyUsageAttr` whose daylight blocks carry the expected `SolarKwh` values, written via the existing `UpdateDailyEnergyDerived` path.

### Backfill CLI test

Add `cmd/backfill-solar/main_test.go`:
- Dry-run mode prints intended writes without invoking `UpdateItem`.
- Live mode calls `UpdateItem` once per row that needs an update; rows already up to date are skipped.
- **In-place patch preservation**: when an existing block has `totalKwh = 4.5`, `boundarySource = readings`, etc., and the recomputed `Blocks()` would yield a slightly different `totalKwh` (because some readings have been pruned by TTL), the CLI writes back the existing block with only `solarKwh` patched — the original `totalKwh` and other fields survive byte-for-byte.

### iOS decoder tests

Extend the existing `DailyUsageBlock` decoding tests with two fixtures: a JSON payload with `"solarKwh": 0.0` decodes to `solarKwh == 0.0`; a payload omitting the key decodes to `solarKwh == nil`. Property — JSON containing `"solarKwh": null` also decodes to nil.

### iOS layout test

Manual / preview verification against the smallest iPhone target (iPhone SE 3rd gen, default Dynamic Type) per Decision 3 negative consequence — confirm the row does not truncate. No automated UI test added.

### Property-based testing candidate

`integratePpv` is monotonic-in-window: integrating `[a, c)` should equal integrating `[a, b) + [b, c)` for any `b ∈ [a, c]` when no >60s gap straddles `b`. A `pgregory.net/rapid` test is appropriate. Generators: random sorted readings with a configured max gap; random split point.
