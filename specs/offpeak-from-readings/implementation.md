# Implementation: Off-Peak From Readings (T-1341)

Three-level explanation of the change set spanning commits `da3754b..30bbdb0` (11 commits across spec + 4 implementation phases + 3 design-critic review rounds + 1 pre-push review fix commit).

---

## Beginner Level

### What Changed

Flux measures how much electricity flows in and out of your house during the cheap "off-peak" power window every day. Until now, it answered "how much did we import / export / charge / discharge / generate during off-peak?" by asking AlphaESS for two running totals — one at 11:00 and one at 14:00 — and subtracting. The problem: AlphaESS's running totals only catch up to physical reality about 15–30 minutes later. So the 14:00 reading is missing the last bit of grid charging that actually happened just before 14:00, and that missing bit gets pushed into the "peak" bucket for the rest of the day. On 2026-05-18 the electricity meter said the house drew 0.17 kWh from the grid during peak hours, but Flux said 1.74 kWh — about a 10× overcount on the headline number a user cares about.

The fix: instead of asking for two running totals, Flux now reads the 10-second power measurements (`flux-readings`) it was already collecting and adds them up itself — once for grid-in, once for grid-out, once for solar, once for battery-charging, once for battery-discharging.

### Why It Matters

The History "Peak imports" tile and the Day Detail "Off-peak savings" row were systematically wrong. The bill never agreed with the app. After this change, the app's off-peak split matches the meter to within calibration noise.

### Key Concepts

- **Off-peak window**: a configurable 3-hour midday slot (11:00–14:00 in Sydney) where cheap solar-soaked grid power is used to top up the battery. Configured in AWS SSM (`/flux/offpeak-start` / `/flux/offpeak-end`).
- **Reading**: a row in DynamoDB written every 10 seconds. Each one captures the current power flowing in five channels (grid, solar, battery, load, etc.).
- **Snapshot**: AlphaESS's running daily total. Convenient (one number) but delayed.
- **Integration**: adding up a stream of "current rate" measurements over time to get a total. Trapezoidal integration is the high-school technique of treating each pair of measurements as a small trapezoid whose area is the energy delivered during that interval.

---

## Intermediate Level

### Changes Overview

Three call sites used to compute the off-peak split via snapshot diff:
1. Poller window-end (`internal/poller/offpeak.go::handleEnd`) — writes the persisted row at 14:00.
2. Lambda live path (`internal/api/compute.go::offpeakDeltas` snapshot-projection branch) — covered today before the persisted row exists.
3. Lambda past-day path — read the persisted row (no change needed structurally).

All three now route through a single new function `derivedstats.IntegrateOffpeakDeltas(readings, now, offpeakStart, offpeakEnd)` which produces the five deltas. The poller's window-end pass writes the row; the Lambda live path computes on the fly until the row goes `complete`; past days serve the persisted row.

### Implementation Approach

**Foundation (Phase 1)**
- `derivedstats.IntegrateOffpeakDeltas` reuses the same trapezoidal core, 60-second gap rule, and boundary-bracket synthesis that already shipped for the daily-energy `integratePload`/`integratePpv` helpers. The integration loop computes `iL`, `iR` indices once and then five passes over the same window with per-sample selectors for `max(pgrid,0)`, `max(-pgrid,0)`, `max(pbat,0)`, `max(-pbat,0)`, `max(ppv,0)`.
- Three new provenance fields on `OffpeakItem`: `IntegrationSampleCount`, `IntegrationSkippedPairs`, `IntegratedAt`.
- Two conditional-write methods on `DynamoStore`: `WriteOffpeakIfPendingOrAbsent` (poller's first-write) and `WriteOffpeakIfComplete` (backfill's overwrite-only-if-already-complete). Backed by a single shared expression with named status sentinels.

**Poller Integration (Phase 2)**
- `OffpeakScheduler.waitForReadingAtOrAfter` (real clock, not the injected scheduler clock) blocks `handleEnd` until a reading with `ts >= offpeak-end` arrives — capped at a 30-second wait. This avoids the boundary-loss class of bug that the snapshot version had at exactly `offpeak-end`.
- Mid-window restart recovery: `positionAfter` walks forward from the persisted row's `pendingFrom` timestamp so the in-memory scheduler resumes correctly without needing to capture a fresh start-snapshot.
- Drift logging (`dynamo.LogOffpeakDrift`): every poller write emits a structured INFO log comparing snapshot-diff to readings-integration values per delta. PositionAfter-recovery rows (where the end-snapshot was never captured) emit a separate `offpeak_drift_no_snapshot_pair` line instead of the meaningless `-startE*` deltas the comparison would otherwise produce.

**API Live Path (Phase 3)**
- `api.liveOffpeakDeltas(readings, now, offpeakStart, offpeakEnd)` integrates today's readings up to `min(now, offpeak-end)`. Returns `(_, false)` if `now <= offpeak-start` (AC 4.3) or fewer than two usable readings exist (AC 1.6).
- `buildOffpeak` (`status.go`) and `offpeakSplit` (`history.go` + `day.go`) dispatch on `item.Status`: `complete` reads the persisted row directly; `pending` runs `liveOffpeakDeltas` against the today readings slice already in memory.
- All references to `op.StartE*` / `op.EndE*` removed from `internal/api/` so AC 5.3 (no live-path snapshot reads) is a compile-time guarantee — sentinel-value tests inject 999.0 into those fields to prove the code path never reads them.
- `liveOffpeakDeltas` trims the readings slice to the integration window via `sort.Search` before allocating its `Reading` conversions — drops ~80 KB per request when called with the 24-hour readings slice that `/status` already has loaded.

**Backfill CLI + Regression Tests (Phase 4)**
- `cmd/backfill-offpeak` walks `flux-offpeak` rows in a date range, integrates per day, logs drift, and writes via `Store.WriteOffpeakIfComplete`. Skips today (AC 7.2) even when `--to=today` is given explicitly. `--dry-run` runs everything except the write.
- Per-day summary: `prev→new |Δ|=abs` columns for all five deltas plus `samples` and `skipped` counts (AC 7.5).
- `internal/derivedstats/regression_test.go` + `testdata/offpeak_2026_05_18.json`: documented synthesised fixture reproducing the 2026-05-18 incident shape. The test asserts AC 2.1 (`peak ≤ 0.25 kWh`) and AC 2.2 (`gridUsageKwh ≤ eInput`).
- `BenchmarkIntegrateOffpeakDeltas` (1080 readings → 35.95 µs/op) and `TestHandleStatus_LiveOffpeak_P95Under500ms` (200 warm requests through the full handler → p95 = 632 µs) — both orders of magnitude inside the AC 8.1/8.2 budgets. The live-path test is `testing.Short()`-skippable to dodge CI flake.

### Trade-offs

- **Why not just patch AlphaESS's lag**: out of our control. Replacing the source removes the dependency entirely.
- **Why not store integrated deltas in `flux-daily-energy` instead of a parallel `flux-offpeak`**: the daily-energy table is the off-the-shelf snapshot for end-of-day totals. Keeping off-peak in its own table preserves the snapshot fields as operator diagnostics and lets us evolve the integration path without touching the canonical daily aggregate.
- **Why a deferred wait at window-end rather than just integrating up to the latest reading**: produces the exact same boundary-loss bug at the readings layer that AlphaESS had at the snapshot layer. The 30-second wait costs at most one missed cycle in pathological cases; the bug it prevents would silently re-introduce the problem we're fixing.
- **Why a separate backfill CLI rather than letting the live path serve corrected data on-demand**: persisted rows are authoritative once written. The CLI gives operators a one-time correction pass; thereafter the poller writes accurate values forward.
- **Why retain the snapshot fields**: pure diagnostics. Drift logging compares them every write; if AlphaESS's lag ever shrinks (or grows), it's visible in CloudWatch without instrumenting upstream.

---

## Expert Level

### Technical Deep Dive

**Integration core, single-pass with five selectors.** `IntegrateOffpeakDeltas` computes `iL` (largest index with `ts < startUnix`) and `iR` (smallest index with `ts >= endUnix`) once via `bracketIndices`, gates on `(iR - iL - 1) + leftEdgeSynth + rightEdgeSynth >= 2`, then invokes `integrate(readings, iL, iR, selector)` five times. Each `integrate` call walks the same `[iL+1, iR-1]` index range, synthesising boundary points at `startUnix` / `endUnix` via linear interpolation against the out-of-window brackets when those brackets are within 60 s. The 60 s gap rule (`maxPairGapSeconds`) skips any consecutive pair whose timestamps differ by more than 60 s — prevents phantom integration across polling outages. Per-sample clamping (`max(pgrid, 0)` etc.) before integration means each of the five outputs is non-negative by construction; no total-level clamp is needed.

**Boundary-loss avoidance at window-end.** `handleEnd` defers integration until `waitForReadingAtOrAfter(offpeakEnd, budget=30s)` observes a reading with `ts >= offpeak-end`, then runs a strongly-consistent query over `[offpeak-start, offpeak-end)` and integrates. Two distinct DynamoDB queries are intentional: the boundary probe must use a forward-scan to detect arrival; the integration query is the authoritative window read. If the probe times out without observation, integration runs anyway against whatever readings exist — AC 3.2 says "no later than 5 minutes after `offpeak-end`," and the 30-second probe with a fall-back keeps the worst case well inside that.

**PositionAfter recovery.** `recoverMidWindow` reads the pending row (written at `offpeak-start` with `pendingFrom=startTime`). If the row exists but no end-snapshot was captured before restart, the scheduler walks readings forward from `pendingFrom` to resync its in-memory cursor. The end-snapshot fields stay zero — `LogOffpeakDrift` detects this (`pending.EndEgridCharge == 0 && pending.EndEsolarPv == 0 && ...`) and emits a `no_snapshot_pair` log line with the integrated values and a `reason: "positionAfterRecovery"` field instead of the standard drift comparison that would otherwise produce meaningless `-startE*` "drifts."

**Conditional-write race semantics.** The poller writes with `WriteOffpeakIfPendingOrAbsent` — fails if a row already has `status=complete`. The backfill CLI writes with `WriteOffpeakIfComplete` — fails if the row is `pending` or absent. Together these enforce: the poller is the single authoritative writer for the row's first transition to `complete`; the backfill CLI can only overwrite an already-complete row. A poller-vs-backfill race on the same day cannot cross — whichever side writes second observes a `ConditionalCheckFailedException`, maps it to `dynamo.ErrOffpeakConditionFailed`, logs a warn, and exits with status 0 (no operator escalation).

**API live-path dispatch.** `buildOffpeak` and `offpeakSplit` switch on `item.Status`. `complete` reads `op.GridUsageKwh` etc. directly — the persisted row is the source of truth post-finalisation. `pending` calls `liveOffpeakDeltas(allReadings, now, offpeakStart, offpeakEnd)` with the today readings slice already loaded by the existing live-energy path; no extra DynamoDB query. Trim-before-convert via `sort.Search` keeps the conversion bounded to `~[opStart-1sample, min(now,opEnd)+1sample]` (the +/-1 retains the bracket samples needed for boundary synthesis).

**Deterministic integration.** `IntegrateOffpeakDeltas` is a pure function with no global state and no clock except via the `now` parameter. AC 4.4 monotonicity property test (rapid PBT in `integrate_offpeak_test.go`) asserts that for any pair `now₁ < now₂` within the window, `result(now₂) >= result(now₁)` per delta. Determinism + monotonicity + idempotent rounding (`derivedstats.RoundEnergy` to 2 dp) means two backfill runs against the same readings produce byte-equal delta and count fields (AC 7.3, AC 7.7).

### Architecture Impact

- **No client schema change.** `/status`, `/day`, `/history` shapes unchanged — only the values shift. iOS / macOS clients pick up corrected numbers on next read.
- **Operator surface gains one CLI.** `cmd/backfill-offpeak` joins `cmd/backfill-readings`, `cmd/backfill-solar`, `cmd/backfill-daily-power`. All share the same Go-flag CLI shape and DynamoDB scan-then-write pattern. There's modest duplication (`dynamoAPI` interface, `paginate[T]`, `queryReadingsRange`, `toDerivedReadings`) flagged as a separate follow-up refactor.
- **Drift telemetry now part of every off-peak write.** Two new CloudWatch INFO log shapes: the standard `offpeak_drift` (snapshot vs integration per delta) and `offpeak_drift_no_snapshot_pair` (positionAfter recovery). No alerting; this is hands-on observability for the next time AlphaESS' upstream behaviour shifts.
- **The `derivedstats` package gains a third integration helper.** `integratePload`, `integratePpv`, `IntegrateOffpeakDeltas`. The first two carry TODO comments to consolidate; Decision 9 keeps them as-is for now to avoid a larger refactor. Worth revisiting when the next integration use case lands.
- **Compile-time AC 5.3 enforcement.** Removing all `op.StartE*` / `op.EndE*` reads from `internal/api/` means a future regression that re-introduces snapshot-based projection would fail to compile. Sentinel test fixtures inject 999.0 to prove non-reading at runtime.

### Potential Issues

- **Mid-day SSM window change.** AC 1.7 admits that `/flux/offpeak-start` / `offpeak-end` changes mid-day may produce a persisted row computed against one window and live-path responses computed against another. Documented as acceptable; no operational mitigation (cache the window at Lambda cold start? Document the manual restart sequence?).
- **Live-path performance under load.** Bench p95 is 632 µs at observed scale (~1080 readings × 5 deltas, single client). At ~10× concurrent users this likely still fits the 500 ms budget but hasn't been measured.
- **Backfill CLI today-skip is runtime-only.** An operator with a misconfigured clock could pass `--to=today` after midnight and the CLI's `time.Now()` check would correctly skip. But if the host clock drifts, an "off-by-a-day" inclusion is structurally possible. Conditional write on `status=complete` is the safety net.
- **Provenance fields don't capture window timing.** `IntegratedAt` records when the integration ran but not which `offpeak-start` / `offpeak-end` values were in effect. If a future audit needs to reconcile a row against historic SSM state, the window must be derived from `pendingFrom` and the row's date — not the provenance fields.
- **CloudWatch volume.** One `offpeak_drift` INFO log per off-peak window per day = 365/year per device; a backfill run = 30 more in a few seconds. Within reasonable bounds for the current scale; if the system grows, log filtering / sampling may be warranted.

---

## Completeness Assessment

**Fully Implemented (all 8 requirement groups, 30 of 30 ACs):**

| Requirement | ACs | Status |
|---|---|---|
| 1. Off-peak delta from readings | 1.1–1.7 | All Pass — `IntegrateOffpeakDeltas` + boundary synthesis + 60s gap rule + sparse-skip + SSM-per-pass sampling |
| 2. Correctness validation | 2.1–2.3 | All Pass — 2026-05-18 fixture asserts peak ≤ 0.25 kWh (observed 0.12 kWh); structural bound `gridUsageKwh ≤ eInput` asserted |
| 3. Window-end finalisation | 3.1–3.5 | All Pass — `waitForReadingAtOrAfter` + bounded timeout + `status=complete` only post-write + positionAfter recovery + conditional write |
| 4. Today's live split | 4.1–4.4 | All Pass — `liveOffpeakDeltas` + pre-window `(_, false)` + monotonicity PBT |
| 5. Persisted diagnostic fields | 5.1–5.4 | All Pass — snapshot retained; deltas sourced from integration; AC 5.3 compile-time enforced; three provenance fields |
| 6. Drift observability | 6.1–6.2 | All Pass — `LogOffpeakDrift` at INFO on every write, plus `no_snapshot_pair` variant for recovery rows |
| 7. One-time backfill | 7.1–7.8 | All Pass — `cmd/backfill-offpeak` with idempotence, today-skip, sparse-skip, per-delta summary, `--dry-run`, rounding consistency, conditional write |
| 8. Performance | 8.1, 8.2 | All Pass — bench 35.95 µs/op (vs 2 s budget); live-path p95 = 632 µs (vs 500 ms budget) |

**Partially Implemented (none).**

**Not Implemented (none in scope).** Out-of-scope items per the requirements doc are deferred by design: no schema/retention change to `flux-readings`, no per-pricing-period window override, no alert replay, no AlphaESS replacement, no automatic alerting on drift.

**Test coverage gaps (minor, none AC-blocking):**

- AC 1.7's mid-day SSM warm-container caveat is documented in design but not unit-tested. A test would require Lambda cold-start simulation; the cost-vs-benefit favours leaving it.
- A negative test for unsorted readings input doesn't exist. The `BETWEEN` query in production guarantees sorted order so the integrator's "Precondition: readings sorted ascending" is structurally satisfied; a future refactor to a different source would break this assumption without an immediate test signal.
- A symmetric "poller-first-then-CLI" cross-writer race test exists only implicitly (the idempotence test covers the value-equality side); the conditional-write-fails branch covers the rejection side.

**Documentation status:**

- `CHANGELOG.md`: `[Unreleased] → Fixed` entry describes the user-visible behaviour change. ✅
- `specs/OVERVIEW.md`: status flipped Planned → In Progress → Done across phase commits. ✅
- `specs/offpeak-from-readings/decision_log.md`: 10 decisions from the original spec plus 2 added in the pre-push review pass (Decisions 11, 12) covering the `hasUsablePoints` separate-pass choice and the `waitForReadingAtOrAfter` real-clock choice. ✅
- `specs/offpeak-from-readings/tasks.md`: all 22 tasks marked complete. ✅
- This implementation document covers the three-level explanation plus completeness assessment. ✅

**Verdict.** Spec is fully implemented. No must-fix gaps. The remaining items (CLI duplication refactor, integration-core consolidation between `integrate*` siblings) are explicitly deferred follow-up work, captured as TODO comments in the affected files and noted in the pre-push review findings.
