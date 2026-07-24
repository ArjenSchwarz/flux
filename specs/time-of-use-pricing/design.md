# Design: Time-of-Use Pricing

## Overview

Pricing plans become "default rate + exception windows" (stored as entered, full-day segmentation derived), with exclusive end dates for same-day succession, the plan replacing SSM as the source of the free window, and per-band import energy persisted at day close so banded costs survive the 30-day readings TTL. Existing rows are migrated once by a CLI tool with golden-value verification.

## Architecture

### New package: `internal/plan`

Leaf package (no imports from `dynamo`/`api`, mirroring `derivedstats` layering) holding the plan domain: types, validation, segmentation, per-date plan selection, free-window resolution. Consumed by the Lambda API, the poller, the backfill CLIs, and the migration tool. `dynamo.PricingItem` ⇄ `plan.Plan` conversion lives in `internal/dynamo/pricing.go`.

### Storage shape: what the user entered, not derived segments

A plan stores `defaultRate` + `windows` (the exceptions). Segmentation into contiguous bands is derived on demand by `plan.Segments`. Rationale: round-trips the editor exactly, makes gaps/coverage violations unrepresentable (a gap is just the default rate), and leaves only overlap/range checks to validate. Requirements 1.1/1.7's gap and coverage error codes are satisfied by construction.

### End-date semantics change: exclusive

New-shape `endDate` is the switch date (exclusive): `covers(d) = startDate ≤ d < endDate`. "Old plan ends Aug 1, successor starts Aug 1" is stored literally, satisfying AC 2.2 with no ±1 arithmetic. `ReplaceOpenEnded` sets `closing.endDate = successor.startDate` (the `previousDate` helper and the inclusive-overlap check are deleted). Because its closing write is a partial `UpdateItem` (every other write path is a full-item `Put`), running it against a not-yet-migrated legacy row would produce a row that legacy-detects as inclusive while carrying an exclusive end date — the read transform and the migration would then each add +1 day. Guard: `replace-open-ended` rejects with `legacyShape` when the closing row still carries `peakRate` ("run migration first"); rejected over rewrite-in-transaction because `ReplaceOpenEnded` carries no predecessor state (a rewrite needs an extra read and can clobber a concurrent edit), and the cutover order already sequences migration before succession. Overlap validation becomes half-open interval intersection, and `Validate` rejects `endDate ≤ startDate` (exclusive ends make `endDate == startDate` a zero-day plan). Migration maps legacy inclusive ends to `legacyEnd + 1 day` (AC 5.2: no day gained or lost). Swift `covers(date:)` changes from `<=` to `<`.

### Free window from plans: consumer audit

Every consumer of the SSM window (`cfg.OffpeakStart/End`, `OFFPEAK_START/END`, handler `offpeakStart/End` fields). "Plan-derived" means: resolve the plan covering the relevant date, take its free window (`plan.FreeWindow(plans, date)`). The absent-window outcomes are NOT the old unparseable-window no-op — they are decomposed per consumer (see the summarisation table and the nullable `/status.offpeak` below), because that no-op path returns before window-independent work runs and sets no sentinels.

| Consumer | Site | Change |
|---|---|---|
| Poller off-peak scheduler | `internal/poller/offpeak.go` `Run()` | Per-day window from `PlanSource` each daily cycle; no free band that day → sleep to next midnight |
| Poller summarisation pass | `internal/poller/dailysummary.go` step 2 | Window + segments from plan covering `date`; also gains the band-split block (below) |
| Lambda `/status` | `internal/api/status.go` (`buildOffpeak`, cutoff suppression) | Plans fetched in the existing errgroup; window for today from today's plan |
| Cutoff suppression next-window | `internal/api/compute.go` `nextOffpeakStart` | Uses the plan covering the day the window falls on (today vs tomorrow) — AC 4.2/Q11 |
| Lambda `/day` | `internal/api/day.go` | Per-request plan fetch; window for the requested date's plan |
| Lambda `/history` | `internal/api/history.go` | One plan fetch; per-day window when computing today's blocks/split |
| `derivedstats` (Blocks, PeakPeriods, integrators) | `internal/derivedstats/*` | Unchanged — they already take window params; callers change |
| Lambda env/config | `cmd/api/main.go`, `internal/api/handler.go` | Drop `OFFPEAK_START/END` env + handler fields; handler already holds the pricing store |
| Poller config | `internal/config/config.go`, `cmd/poller/logging.go` | Drop `OffpeakStart/End` fields + validation + log line |
| Infra | `infrastructure/template.yaml` | Drop `OffpeakStartParameter/OffpeakEndParameter` + both containers' env vars; add `TABLE_PRICING` env + read-only IAM (`dynamodb:Scan`, `GetItem`, `Query` — `ListPricing` is a Scan) on `PricingTable` to the ECS TaskRole (Lambda keeps sole write access) |
| Backfill CLIs | `cmd/backfill-grid`, `cmd/backfill-solar` | Replace `--offpeak-start/end` flags with per-day plan resolution from the pricing table (a backfill spanning the switch date needs per-day windows; static flags would silently misattribute). `backfill-grid` additionally rewrites the day's rated `bandImports` and the offpeak row's window geometry whenever it repairs that day (writes across the two items are separate calls — non-atomic but idempotent and re-runnable; known limitation) |
| SoC alert evaluation | `internal/poller/eval`, `apns` | No window usage (verified) — untouched |
| Day Detail chart shading | `Flux/Flux/DayDetail/DayChartDomain.swift` `offpeakRange` | Hardcodes 11:00–14:00 (`+11h`/`+14h`) — must take the day's window from the API response; no window → no shading |
| Widget/`OffpeakData` defaults | FluxCore `APIModels.swift` (`defaultWindowStart/End`) | `offpeak: null` must render as "no window", never substitute the default window constants |

### Poller: PlanSource

`internal/poller/plansource.go`: loads all plans at startup, caches last-good (Q14/AC 4.6). Read failure → warn + serve cache, never "no plan". Cold start with unreachable table → retry with backoff; until the first successful load the scheduler defers window processing (the existing `positionAfter` recovery + backfill CLI are the repair paths, so a deferred day is recoverable within the 30-day TTL).

The scheduler's daily cycle is re-anchored to midnight: wake at local midnight, refresh plans, resolve that day's window, then sleep to its start (today it sleeps directly to the next window start using static config). Since plans only change behaviour at midnight boundaries (AC 2.2), one refresh per day is exactly sufficient; a same-day edit to today's free window takes effect only for windows after the edit is picked up, with the backfill CLI as the repair path. The summarisation pass and Lambda always read plans per invocation, so they see edits immediately.

### Band-split capture at day close

Ownership rule (one writer per physical quantity, per Q13): **the flux-offpeak row exclusively owns free-window import** (`GridUsageKwh`, written by the scheduler at window end and repaired by `backfill-grid`); **`bandImports` stores rated segments only**. The offpeak row gains `windowStart`/`windowEnd` geometry attributes snapshotted at capture so a later free-window edit is detectable as a mismatch; rows without geometry (pre-feature) are treated as 11:00–14:00, the only window they can have been computed under. A sparse-complete offpeak row (`integratedAt` set, `integrationSampleCount == 0`) counts as *unavailable* for costing — a zero-delta artifact, not a measured zero.

`runSummarisationPass` gains a third sentinel-gated block (pattern: peak-from-readings Decision 3): `bandsComputedAt` empty → derive the rated segments of `Segments(plan-of-date)`, integrate `max(pgrid,0)` per rated segment (reusing `IntegrateOffpeakDeltas` per segment window, boundaries from wall-clock `SegmentBounds` — do NOT copy the existing peak block's `dayStart.Add(elapsed)` arithmetic, which is an hour off on DST days; deriving the peak block's boundaries from `SegmentBounds` fixes that latent bug in passing), sum raw integrals before per-entry rounding, write via the extended `UpdateDailyEnergyDerived`. Usability gate: all rated segments must pass the integrator's gate, else the field stays absent and the sentinel is still set (mirrors `PeakGridImportKwh`).

The single early-return the pass uses today for an unresolved window is replaced with typed per-outcome gating:

| Outcome | Blocks run | Sentinels | Result |
|---|---|---|---|
| Plan read failed (no cache) | none | none | `PassResultError` — retried next tick (Q14/AC 4.6) |
| Plan with free band | all (windows from plan) | set | normal |
| Plan without free band | socLow + derived + peak in whole-day-rated mode; off-peak-split values absent (AC 4.4) | set | rated `bandImports` cover the whole day |
| No plan covers the date | window-independent stats only (socLow) | band sentinel left unset | terminal until an explicit backfill within the TTL repairs it |

A semantic absence never returns before window-independent work runs; a read failure never sets sentinels.

Window-start plan failure: if the plan is unknown at window start, `handleStart` cannot run; `handleEnd`/recovery are relaxed to permit readings-only finalisation without a pending row or start snapshot (the integration never needed them — the snapshot is diagnostics-only per offpeak-from-readings Decision 2), so a plan load that succeeds later in the day still finalises the window. Only a day-long outage needs the backfill CLI.

Today's (live) split is computed on demand by `/day` and `/history` from readings, same as `computeTodayEnergy` — one shared helper so both agree (AC 3.4).

### Migration: `cmd/migrate-pricing`

Follows the `cmd/backfill-*` pattern (local CLI, direct DynamoDB), except that reporting is the default and `--apply` is what writes — there is no `--dry-run` flag to forget. Steps: (1) read all pricing rows + all retained daily-energy rows; (2) compute every priced day's costs under the **exact legacy `DayCosts` formula** (the tier-2 table above — server-peak preference, clamp, nil-offpeak path; a "three multiplications" simplification would make the golden check vacuous) and record as goldens; days priced by rows that are already new-shape (edited via the band-aware Lambda pre-migration) are verified band-formula-vs-band-formula and logged as such; (3) transform rows: `defaultRate ← peakRate`, `windows ← [{11:00–14:00 free}]` (AC 5.1), `savingsReferenceRate ← offPeakSavingsRate`, `endDate ← legacyEnd + 1`; (4) recompute all goldens under the band formula and diff — any mismatch aborts before writing (AC 5.2/5.3); (5) `--apply` writes new-shape rows in place (same `pricingId`s, sentinel untouched). Idempotent: already-migrated rows (no `peakRate` attribute) are skipped.

To decouple deploy order from the migration run, the dynamo read path converts legacy rows to `plan.Plan` on read using the same transform function the migration tool uses (defined once, in `internal/dynamo/pricing.go`). A band-aware poller or Lambda deployed before migration therefore still resolves windows and serves plans correctly; writes of the legacy shape are rejected immediately. The read-side conversion becomes dead code once migration has run and is deleted in the cleanup task, keeping Decision 3's no-dual-paths end state.

Cutover order (AC 5.4): deploy band-aware poller + Lambda + apps → run migration → enter the new plan → switch date arrives. Legacy app builds against a migrated API fail decoding `/pricing` and `PricingService` publishes no periods → cost cards hide (existing nil-costs path); no crash, no writes.

## Components and Interfaces

```go
// internal/plan
type Window struct{ Start, End string; Free bool; Rate float64 } // HH:MM; Rate ignored when Free
type Plan struct {
    ID, StartDate string
    EndDate       string  // exclusive; "" = open-ended
    DefaultRate   float64
    Windows       []Window
    FeedInRate    float64
    SavingsRefRate float64 // required iff a free window exists
}
type Segment struct{ Start, End string; Free bool; Rate float64 }

func (p Plan) Covers(date string) bool          // startDate <= date < endDate (lexicographic)
func (p Plan) Validate() []ValidationError      // overlap, bounds, precision, free count, savings rate, windows-consume-whole-day
func Segments(p Plan) []Segment                 // deterministic; contiguous; [0] starts 00:00, last ends 24:00
func PlanFor(plans []Plan, date string) (Plan, bool)
func FreeWindow(plans []Plan, date string) (startMin, endMin int, ok bool)
func SegmentBounds(seg Segment, day time.Time, loc *time.Location) (startUnix, endUnix int64) // wallClockTime-based; DST per AC 3.8
```

Contracts:
- Band time parsing is a new parser accepting `"24:00"` as end-of-day (internally minutes 0–1440). `derivedstats.ParseOffpeakWindow` rejects `h > 23` and must NOT be reused for bands — it would reject every plan.
- `Segments` output tiles the day exactly; adjacent same-rate segments produced by abutting windows are NOT merged (stable geometry for the split join).
- `SegmentBounds` derives boundaries via `time.Date` wall-clock (existing `wallClockTime` semantics), so per-segment integrals over a DST day sum to the whole-day integral (shared boundaries cancel). A boundary inside the repeated/skipped DST hour resolves to whichever occurrence `time.Date` picks — deterministic is enough (AC 3.8's sum invariant holds either way; no real plan has a boundary in 02:00–03:00).
- Cost join rule (FluxCore), in order:
  1. **Banded**: `bandImports` present and its geometry exactly equals the *rated* segments of `Segments(plan-of-day)`, AND the free import is resolvable (plan has no free band, or a usable offpeak row whose geometry matches the plan's free window — sparse-complete rows are unusable) → `importCost = Σ ratedKwh×rate`, `savings = offpeakRowKwh × savingsRefRate`.
  2. **Single-rate legacy formula** — applies when the plan's rated segments share one rate `R`. This is the existing `DayCosts` formula **verbatim** (server-peak preference, zero clamp, nil-offpeak path — NOT the naive residual; the stored `peakGridImportKwh` differs ~1.5% from `eInput − offpeak` by design, and legacy costing prefers it). With `E = eInput ?? 0`, `O = offpeakGridImportKwh`, `P = peakGridImportKwh`, `S = savingsRefRate`:

     | `O` | `P` | importCost | savings |
     |---|---|---|---|
     | present | present | `P × R` | `O × S` |
     | present | absent | `max(0, E − O) × R` | `O × S` |
     | absent | present | `P × R` | `$0.00` |
     | absent | absent | `E × R` | `$0.00` |

     For a single-rate plan this tier always resolves, so tier 3 is reachable only for multi-rate plans. This tier prices all pre-feature history and is what makes AC 5.2 hold without backfill.
  3. **Fallback** (multi-rate plans only): all `eInput` × max segment rate, savings $0.00 (AC 3.5/3.6).

  `feedInIncome = eOutput × feedInRate` and `net = importCost − feedInIncome` in every tier. Energy is frozen at capture — meaning unchanged by plan/rate edits, though an explicit backfill may rewrite it; rates are applied at display, so rate edits reprice history but window edits degrade affected multi-rate days to the fallback (visible and consistent; re-capture within the TTL via backfill if wanted).

```swift
// FluxCore replaces PricingPeriod/PricingPeriodDraft
struct PricingPlan: Codable { id, startDate, endDate?, defaultRate, windows: [PlanWindow], feedInRate, savingsReferenceRate?, createdAt, updatedAt }
struct PlanWindow: Codable { start, end: String; free: Bool; rate: Double? }
// PricingPlanDraft mirrors plan.Validate; segmentation helper mirrors plan.Segments,
// pinned to Go via shared vectors (internal/api/testdata/pricing_segments.json — note_lengths.json pattern)
```

Wire shape (`/pricing` CRUD + replace-open-ended, same routes):

```json
{"id":"…","startDate":"2026-08-01","endDate":null,
 "defaultRate":0.35,
 "windows":[{"start":"10:00","end":"15:00","free":true},
            {"start":"01:00","end":"06:00","free":false,"rate":0.28}],
 "feedInRate":0.05,"savingsReferenceRate":0.35,
 "createdAt":"…","updatedAt":"…"}
```

Read-endpoint additions: `DayEnergy` (history) and `DaySummary` (day) gain nullable `bandImports: [{start,end,kwh}]` (rated segments only). `/status` does not — the Dashboard shows no costs, and Day Detail/History are served by the other two endpoints. `/status.offpeak` becomes a nullable object: `null` on a no-free-band or no-plan day (FluxCore's `OffpeakData.windowStart/windowEnd` are currently non-optional and `buildOffpeak` always emits them — both change; clients render nil as "no window", never the default-window constants). Existing `offpeakGridImportKwh`/`offpeakGridExportKwh` fields stay (off-peak card unchanged). Absent serialises as `null` (project convention).

### UI

`PricingEditor` keeps its sheet structure (dates, open-ended toggle, delete, overlap remediation): the three rate fields become Default rate / Feed-in / Savings reference, plus a Windows section (per row: start/end pickers, Free toggle, rate field when not free; add/remove). `PricingPeriodsView.rateSummary` renders "Free 10:00–15:00 · $0.2800 01:00–06:00 · $0.3500 default". Overlap remediation copy changes from "day before this start date" to the switch-date phrasing. `CostsCard`/`HistoryPeriodCostsCard` are unchanged in layout (user decision); only `DayCosts`/`PeriodCosts` inputs change.

## Data Models

`flux-pricing` new-shape item (same table, same key, sentinel row untouched):

| Attribute | Type | Notes |
|---|---|---|
| `pricingId` | S | PK, unchanged |
| `startDate` | S | inclusive, unchanged |
| `endDate` | S? | **exclusive** switch date; absent = open-ended |
| `defaultRate` | N | 4 dp |
| `windows` | L of M | `{start S, end S, free BOOL, rate N?}` |
| `feedInRate` | N | unchanged semantics |
| `savingsReferenceRate` | N? | present iff a free window exists |
| `createdAt`/`updatedAt` | S | unchanged |

Legacy detection: `peakRate` attribute present.

`flux-daily-energy` additions (written via `UpdateDailyEnergyDerived`, third group):

| Attribute | Type | Notes |
|---|---|---|
| `bandImports` | L of M | `{start S, end S, kwh N}` — **rated segments only** (free import lives on the offpeak row), geometry snapshotted at capture |
| `bandsComputedAt` | S | sentinel, same contract as `peakComputedAt` |

`flux-offpeak` additions:

| Attribute | Type | Notes |
|---|---|---|
| `windowStart`/`windowEnd` | S | HH:MM geometry the row was integrated under, snapshotted at capture; absent = 11:00–14:00 (pre-feature rows) |

## Error Handling

New `PricingValidationReason` codes (server + Swift mirror): `bandWindowInvalid` (bad HH:MM, start ≥ end, out of day), `bandOverlap` (windows intersect), `multipleFreeBands`, `savingsRateMissing`, `noRatedBand` (the free window spans the entire day — violates AC 1.3; a zero-width default remainder is otherwise fine when rated windows tile the rest), `legacyShape` (three-rate payload post-migration, AC 7.3; also returned by `replace-open-ended` when the closing row is still legacy-shape). Legacy-shape detection cannot rely on plain `json.Unmarshal` — it silently drops unknown fields, so a legacy POST would decode as a malformed band plan; detect via the raw JSON/attribute map (`peakRate` key present) on both the write path and row reads. Existing `ratePrecision`/`rateRange`/`overlap`/`secondOpenEnded` codes carry over (`overlap` now half-open; `invertedDates` extends to reject `endDate == startDate`).

Plan-data failures: poller → last-good cache + warn (never "no plan", AC 4.6); Lambda read endpoints → 500 like any other store failure (Q14 — never fabricate an unpriced day); `PricingService` decode failure on legacy builds → empty periods, cost cards hide.

## Testing Strategy

- **`internal/plan`**: table-driven tests for `Validate` (each code), `Segments` (new plan, old plan, no windows, abutting windows), `Covers`/`PlanFor` boundary dates (switch day D, D−1). Property-based (`pgregory.net/rapid`, existing project pattern): generated window sets → segments always tile 00:00–24:00 with no overlap and preserve window rates; and for generated readings + windows, Σ per-segment `IntegrateOffpeakDeltas` grid import = whole-day integral (±ε) including DST-length days.
- **Cross-language vectors**: `internal/api/testdata/pricing_segments.json` + `pricing_costs.json` (inputs → segments; day energy + plan → 4 cost figures) consumed by both Go tests and FluxCore tests, pinning AC 3.1–3.6 to identical numbers on both sides. The cost vectors MUST cover all four tier-2 input combinations (offpeak ±, server peak ±), the zero clamp, a sparse-complete offpeak row (unavailable), a geometry-mismatch day, and the multi-rate fallback — the tier-2 rows are also the migration tool's golden formula, so these vectors are the AC 5.2 proof.
- **Poller**: scheduler tests with a mock PlanSource — window from plan, switch-day change (predecessor window D−1, successor D), no-free-band day, unreachable-then-recovered source; summarisation tests for the band block (sentinel gating, geometry snapshot, usability-gate absence).
- **API**: handler tests for new payload validation codes, legacy-shape rejection, half-open overlap, replace-open-ended same-day semantics, `bandImports` in all three read endpoints, next-window suppression across the switch boundary (AC 4.2).
- **Migration**: golden test with legacy fixture rows + daily-energy fixtures asserting old-formula == new-formula for every day (AC 5.2), idempotence, and dry-run write-nothing.
- **Swift**: `DayCosts`/`PeriodCosts` against the shared vectors; `PricingPlanDraft` validation mirror; editor/view-model tests per existing Settings patterns.
