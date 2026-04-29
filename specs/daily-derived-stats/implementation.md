# Implementation: Daily Derived Stats

This document explains the daily-derived-stats implementation at three expertise levels and validates it against the spec at `specs/daily-derived-stats/`.

## Beginner Level

### What Changed

For each day, the Flux app shows three numbers per battery system: a five-block usage breakdown (`dailyUsage`), the lowest battery percentage hit during the day (`socLow` + `socLowTime`), and the worst peak-load periods (`peakPeriods`). Until this change, every time the iOS app asked the Lambda API for one of those stats, the Lambda recomputed them from the raw 10-second readings stored in DynamoDB.

After this change, the **poller** (the long-running container that talks to the AlphaESS API) computes those three stats once an hour for *yesterday* and writes them onto the existing daily-energy row in DynamoDB. The Lambda then **reads** the pre-computed numbers for any past day, and only **computes live** for today.

### Why It Matters

The raw readings table (`flux-readings`) keeps data for 30 days then deletes it. So in two weeks, when a future feature wants to show "average evening peak over the last 30 days", that feature would be unable to recompute the data — the readings would already be gone. By baking the per-day stats into the never-expiring daily-energy row, that feature (and the one already planned, T-1022) can simply read N rows without re-fetching ~258k readings every call.

### Key Concepts

- **Poller**: a service that runs continuously, polling AlphaESS on schedules (every 10 seconds for live data, hourly for daily totals). Now it also runs an hourly summarisation pass.
- **Lambda**: the on-demand HTTP API the iOS app talks to. Reads from DynamoDB, computes derived stats for "today only", returns JSON.
- **DynamoDB UpdateItem**: a database operation that changes specific fields on a row without touching the others. Critical here because the poller's "energy totals" writer and the new "derived stats" writer must not clobber each other.
- **Sentinel**: a marker field (`derivedStatsComputedAt`) whose presence means "this row has been summarised already". Lets the poller skip work cheaply on re-runs.

---

## Intermediate Level

### Changes Overview

#### New package: `internal/derivedstats`
A leaf package with **zero Flux-internal imports**. Houses the three pure functions (`Blocks`, `MinSOC`, `PeakPeriods`), the Melbourne sunrise/sunset table, the off-peak window parser (`ParseOffpeakWindow`), the integration helper (`integratePload`), and a local `Reading` shadow struct that mirrors the dynamo `ReadingItem` fields the helpers consume. Both `internal/api` and `internal/poller` import this package; neither needs to import the other.

#### Storage extension: `internal/dynamo/models.go` + `derived_conv.go`
- `DailyEnergyItem` gains four optional attributes: `DailyUsage *DailyUsageAttr`, `SocLow *SocLowAttr`, `PeakPeriods []PeakPeriodAttr`, `DerivedStatsComputedAt string` (RFC 3339).
- A new `DerivedStats` bundle struct groups the four attributes for the writer's argument signature.
- `derived_conv.go` provides four conversion functions (`DailyUsageFromAttr`/`ToAttr`, `PeakPeriodsFromAttr`/`ToAttr`); `SocLowAttr` is read directly by call sites (no helper, since the storage and in-process shapes are identical).

#### Storage writers: `internal/dynamo/dynamostore.go`
- `WriteDailyEnergy` migrated from `PutItem` to `UpdateItem` with a SET expression covering exactly the six energy fields (Decision 3). A `reflect`-based regression test in `dynamostore_test.go` walks `DailyEnergyItem` and asserts every non-derivedStats, non-key field appears in the SET expression, so a future-added energy field cannot silently get dropped.
- New `UpdateDailyEnergyDerived` does a single SET on the four derived attributes atomically.
- New `GetDailyEnergy` reuses the existing `getItem` generic helper.

#### Poller summarisation: `internal/poller/dailysummary.go`
The pass runs once per `dailySummaryInterval` (1 hour) via the existing `pollLoop` helper, and on the first tick on container startup. Each tick:

1. Resolves "yesterday" in `cfg.Location` (`Australia/Sydney`).
2. **Precheck** (`GetDailyEnergy`): skip if no row, skip if `DerivedStatsComputedAt` sentinel is present.
3. Validates the off-peak window (`ParseOffpeakWindow`); skip if unresolved.
4. Queries readings for the day; skip if empty.
5. Calls `Blocks`, `MinSOC`, `PeakPeriods` (passing `today=date` so the today-gate inside `Blocks` doesn't fire on a completed day).
6. Single `UpdateItem` SET on the four attributes (atomic per-row).

Each pass returns one of six `PassResult*` constants (`success`, `skipped-no-row`, `skipped-already-populated`, `skipped-ssm-unresolved`, `skipped-no-readings`, `error`) which is emitted as a CloudWatch metric dimension on the `Flux/Poller` namespace.

#### Lambda reads: `internal/api/day.go` + `history.go`
- `/day` branches on `date == today` at request entry (single `nowFunc()` read per AC 3.7). Past dates skip the readings query entirely and read derivedStats from the `DailyEnergyItem`. The existing `flux-daily-power` fallback for old dates is preserved untouched.
- `/history` runs the today-readings query on a sibling goroutine **outside** the errgroup (per AC 4.9), so its failure logs and serves the today row with energy-only without failing the whole response. Past rows in the loop populate from storage; the today row live-computes against the readings already loaded for energy reconciliation.

#### iOS: `Flux/Packages/FluxCore/.../APIModels.swift` + `Flux/Flux/Models/CachedDayEnergy.swift`
- `DayEnergy` gains four optional properties (`dailyUsage`, `socLow`, `socLowTime`, `peakPeriods`) with `nil` defaults so existing callsites compile unchanged.
- `CachedDayEnergy` (SwiftData `@Model`) gains the same four optional properties; `init(from:)` and `asDayEnergy` round-trip them. No new `@Relationship`, so SwiftData lightweight migration is a no-op.

#### Infrastructure: `infrastructure/template.yaml`
Adds `DerivedStatsWritePolicy` to the poller's task role: `dynamodb:UpdateItem` on `DailyEnergyTable.Arn` and `cloudwatch:PutMetricData` scoped to `Flux/Poller` namespace via a condition key.

#### Tests + tooling
- `internal/derivedstats/*_test.go`: the helpers' tests moved here from `internal/api/compute_test.go`, plus `property_test.go` (rapid-based determinism property).
- `internal/dynamo/derived_conv_test.go` + `derived_conv_property_test.go`: round-trip tests for the attribute converters.
- `internal/poller/dailysummary_test.go`: all 8 AC 6.1 scenarios plus AC 6.2 idempotence.
- `internal/api/day_derivedstats_test.go`, `history_derivedstats_test.go`, `cross_handler_test.go`: read-side and equivalence coverage.
- `internal/integration/derivedstats_e2e_test.go`: AC 6.7 round-trip, gated on `INTEGRATION=1`. Stages readings, drives the real `SummariseYesterday` against DynamoDB Local, then invokes `/day` and `/history` via `Handle()` and asserts the responses carry the derivedStats.
- `Makefile` gains `make integration` which starts/stops `amazon/dynamodb-local`.

### Implementation Approach

**Layering invariant (Decision 9)**: `derivedstats` imports nothing from Flux. `dynamo` imports `derivedstats` (for the conversion helpers). `api` and `poller` import both. The `Reading` shadow struct in `derivedstats` and the per-call-site `toDerivedReadings` helpers (one in `internal/api/compute.go`, one in `internal/poller/dailysummary.go` named `summaryToDerivedReadings`) are the explicit cost of breaking the would-be cycle between `dynamo` and `derivedstats`.

**Atomic deploy (Section 7)**: the poller's UpdateItem-based writer migration and the new summarisation pass ship in the same image; the IAM grant ships in the same CFN deploy. The Lambda read path is independently safe to deploy at any time because it tolerates absent derivedStats per AC 3.3 / 4.4.

**Sentinel-based precheck (Decision 8)**: presence of `DerivedStatsComputedAt` is the only signal — inspecting the three derived attributes individually doesn't work because both `Blocks` (returns nil for empty pipelines) and `PeakPeriods` (returns empty slice on cloudy days) have legitimate empty results.

### Trade-offs

- Three near-identical reading conversions exist (Decision 9): we accepted ~12 lines of boilerplate per consuming package to keep `derivedstats` a leaf. Sharing a helper in `dynamo` would have made the conversion an API surface of the storage package.
- Storage shape is intentionally separated from in-process shape (`*Attr` types vs `derivedstats.*` types), mirroring how `DailyPowerItem` / `TimeSeriesPoint` are kept apart. The conversion functions are mechanical but they keep DynamoDB tags out of `derivedstats`.
- Backfilling pre-existing dates was rejected as out-of-scope (Non-Goals). Pre-feature rows simply lack the new attributes; the Lambda omits the corresponding sections per AC 3.3 / 4.4.

---

## Expert Level

### Technical Deep Dive

**Today-gate handling in `derivedstats.Blocks`**: the function takes `(date, today, now)` so a single call site can be both the today-live-compute and the past-storage-write path. The poller passes `today=date` (the date being summarised) so the in-progress clamp and future-omit logic cannot fire on a completed day. AC 6.1's "DateAsToday" scenario asserts this in `dailysummary_test.go` by reading the captured `mockStore.lastDerived` payload and verifying every block has `Status == "complete"`.

**Idempotence vs precheck**: AC 1.8 (deterministic re-runs) and AC 1.10 (precheck short-circuit) overlap but solve different things. Idempotence guarantees safety if the precheck were ever bypassed (e.g. someone clears the sentinel manually); the precheck guarantees the pass is cheap on the steady state. The idempotence test (`TestSummarisation_Idempotence`) re-runs the pass twice with a mock that doesn't persist the sentinel and asserts field-equivalence on the captured payload.

**RFC 3339 timestamp pipeline**: `derivedstats.MinSOC` returns a unix int64; `dailysummary.go` formats it once into the `SocLowAttr.Timestamp` RFC 3339 string at write time. Lambda `/day` re-parses it with `time.Parse(time.RFC3339, ...)` to populate `summary.socLowTime` (existing wire shape), while Lambda `/history` republishes the string as-is on `DayEnergy.SocLowTime`. The single int64→RFC3339 conversion happens once at write time.

**Cross-handler equivalence (AC 6.6 / 4.10)**: `cross_handler_test.go` fixes a clock and asserts that `/day` and `/history` for the same past date produce field-equivalent derivedStats payloads. The known acceptable gap is the off-peak SSM in-flight window (Decision documented in AC 4.10): if the operator changes off-peak times between the poller's read and the Lambda's read, the two surfaces may temporarily disagree. This closes within one summarisation tick after the redeploy.

**Concurrency on `/history`**:
- `errgroup`: `QueryDailyEnergy` + `QueryOffpeak` (offpeak failure logs and proceeds without split — supplementary).
- Sibling goroutine for today-readings (`QueryReadings`), drained via a buffered channel after `g.Wait()`. AC 4.9 mandates this isolation: a today-readings failure must not 500 the whole `/history` request.
- `fetchNotesAsync` runs in parallel using the parent `ctx` (not `gctx`) to avoid the cancel-on-Wait race.

**Lambda IAM (AC 7.2)**: confirmed during implementation that the existing Lambda role already grants `dynamodb:GetItem` and `dynamodb:Query` on `DailyEnergyTable` (no attribute-condition narrowing). Stanza in `infrastructure/template.yaml` was not modified.

**Sizing budget (AC 2.5)**: `internal/dynamo/sizing_test.go` marshals a representative `DailyEnergyItem` with all three derivedStats sections via `attributevalue.MarshalMap` and asserts the result is under 4 KB so a `GetItem` continues to consume one RCU. Measured size in the test is ~2.35 KB.

### Architecture Impact

**Unblocks T-1022 (history-daily-usage)**: the consuming feature now reads N rows from `flux-daily-energy` instead of querying `flux-readings` (which would re-fetch ~258k items per 30-day rollup against the 30-day TTL).

**No table changes**: no GSI, no LSI, no TTL change, no billing-mode change. All four new attributes are optional native types; pre-existing rows decode with the fields nil.

**Operator alarm surface**: the `SummarisationPassResult` metric with `Result` dimension lets ops alarm on "no `success` data point in 25 hours" (one diurnal cycle plus an hour buffer). The alarm itself is intentionally not part of this spec — it's an operator-side declaration that depends on SNS topic wiring.

**Future-proofing the energy SET expression**: the reflect-based struct-tag-coverage regression test in `dynamostore_test.go` is the explicit guard against the regression `PutItem` did not have — namely, adding a new energy field to `DailyEnergyItem` without updating `WriteDailyEnergy`'s SET expression.

### Potential Issues

1. **Container restart in the post-midnight gap**: a fresh container starting after midnight Sydney has to wait for `pollDailyEnergy` to finalise yesterday's row before `pollDailySummary` can write derivedStats. AlphaESS's finalisation tail can extend ~90 minutes past midnight, so the realistic worst-case time-to-populate for a fresh-after-midnight container is 2–3 hours. Well within the 30-day TTL and the proposed 24-hour CloudWatch alarm window (covered by Decision and design.md).

2. **Concurrent passes**: the precheck is a TOCTOU window — two summarisation passes firing simultaneously could both observe "no sentinel" and both write. The design accepts this because (a) `desiredCount=1` on the ECS service makes concurrent passes impossible in production, (b) the writes are idempotent so the last-write-wins outcome is field-equivalent. A `ConditionExpression` on the UpdateItem would close it tighter; not added because the precondition is already enforced by the deployment topology.

3. **Off-peak SSM in-flight redeploy gap (AC 4.10)**: a Lambda redeployed with a new off-peak window before the next poller summarisation tick will return live-computed `/history` today values that differ from the stored `/day` values for past dates. Acknowledged as acceptable; closes on the next successful summarisation pass.

4. **Today-readings query failure on `/history`**: the today row serves with energy totals only (no `dailyUsage` / `socLow` / `peakPeriods`). The iOS client must tolerate the absence of those sections on the today row specifically — the existing absence-handling for past rows (pre-feature rows) covers this case mechanically.

5. **iOS SwiftData migration**: per AC 5.5 the implementation claims a no-op migration. The lightweight test (`CachedDayEnergyTests.swift`) verifies the in-memory round-trip and a pre-feature-shape row in an in-memory store. **The full empirical cache-file test (load a `.store` file written by a pre-feature build on a simulator) is documented as a developer-side check before merge** — see Validation Findings below.

---

## Validation Findings

### Spec Coverage

All 22 implementation tasks in `specs/daily-derived-stats/tasks.md` are checked off, and the implementation matches the design on every load-bearing acceptance criterion. The cross-cutting changes (writer migration, precheck sentinel, cross-handler equivalence) are wired correctly. Detailed AC-by-AC verification was performed by the pre-push review subagent and is summarised below.

### Documented Divergences

None. Items in the design that were updated during implementation (the `summaryToDerivedReadings` function name, the in-flight test capture mechanism via mockStore vs the design's `lastDerivedForTest` field, the e2e test driving the actual pass via the new `SummariseYesterday`/`SetNow` test seams) are mechanical refinements and have been folded into the implementation without a decision-log entry — the design's guarantees still hold.

### Gaps Identified

1. **AC 5.5 empirical cache-file migration check is a pre-merge developer task, not a CI test.** `CachedDayEnergyTests.swift:158-194` exercises the in-memory schema migration path but does not load a SwiftData `.store` file written by the pre-feature build. The test's own comment acknowledges this. **Action**: a developer should run the simulator with a pre-feature build, then upgrade to this build, and verify that existing `CachedDayEnergy` rows load with the new properties as `nil` and no `ModelContainer` initialisation error. If the empirical check fails, the AC mandates adding a `VersionedSchema` lightweight migration.

2. **Performance baselines (AC 4.7 / 4.8)** are infrastructure-ready (`history_bench_test.go` exists, `sizing_test.go` asserts the 4 KB budget) but the actual pre-vs-post-feature p95 latency comparison is a PR-time activity per the AC's escape clause. Recommendation: capture the numbers via `go test -bench=.` against the pre-feature commit and the post-feature commit, and include in the PR description.

3. **AC 4.10 in-flight SSM gap**: deliberately not asserted by the cross-handler equivalence test (the test reads from a single shared row, so the assertion would be trivially true). A one-line comment in `cross_handler_test.go` would close the loop, but the AC is explicit that this is an acceptable known gap.

### Logic Issues

None identified. The data flow is consistent across all three streams (Backend Go, iOS, Infrastructure) and the layering invariant is preserved.

### Recommendations

1. Run the AC 5.5 simulator empirical check before merge; document the result in the PR description.
2. Capture pre/post `/history?days=30` p95 latency and payload-size baselines before merge per AC 4.7 / 4.8.
3. After deploy, declare a CloudWatch alarm on `SummarisationPassResult{Result=success}` absent for 25h (operator-side, intentionally out of spec scope).

---

## Completeness Assessment

### Fully Implemented

- Section 1 (Poller Hourly Summarisation Pass) — all 14 ACs
- Section 2 (DynamoDB Schema Extension) — all 5 ACs including the 4 KB sizing assertion
- Section 3 (Lambda `/day`) — all 7 ACs including the single-clock-read at request entry and the `flux-daily-power` fallback preservation
- Section 4 (Lambda `/history`) — ACs 4.1–4.6 and 4.9 covered; AC 4.10 acknowledged
- Section 5 (iOS Model Decoding) — ACs 5.1–5.4, 5.6 covered; AC 5.5 partially covered (see Partially below)
- Section 6 (Testing) — all 8 ACs covered, including the integration test that now actually drives the real summarisation pass and Lambda handlers against DynamoDB Local
- Section 7 (Deployment) — all 3 ACs; CFN grants land atomically with the binary

### Partially Implemented

- **AC 5.5 (iOS empirical migration check)**: the schema-roundtrip aspect is tested in-memory, but the pre-feature `.store` file load is documented as a manual pre-merge developer task. The AC's fail-criterion (add a `VersionedSchema` lightweight migration) is in scope but not exercised because the schema is additive-optional and SwiftData's documented behaviour for new optional properties is a no-op.
- **AC 4.7 / 4.8 (performance budgets)**: the measurement infrastructure is in place; the actual pre/post numbers are PR-time and have not yet been captured.

### Missing

None.
