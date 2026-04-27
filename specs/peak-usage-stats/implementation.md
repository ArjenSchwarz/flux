# Implementation: Peak Usage Stats

This document explains the peak-usage-stats feature at three levels of expertise and validates completeness against the spec.

---

## Beginner Level

### What This Does

Flux's Day Detail screen used to show one card called "Evening / Night" that summarised how much electricity was used during two parts of the day (the dark hours when solar is asleep). This change replaces it with a single "Daily Usage" card that splits the day into up to five chronological slices:

1. **Night** — midnight to first solar
2. **Morning Peak** — first solar to off-peak start
3. **Off-Peak** — the cheap-electricity window from the energy provider (currently 11:00–14:00)
4. **Afternoon Peak** — off-peak end to last solar
5. **Evening** — last solar to next midnight

Each slice shows the total kWh used, the average kWh per hour, and what percentage of the day's total it represents.

### Why It Matters

Two reasons:

1. The old card only showed evening + night. The new card shows the whole day, so the user can see *where* their electricity bill comes from — morning, midday charging, afternoon, or evening — at a glance.
2. The "Off-Peak" slice surfaces how much the battery actually charges during the cheap-rate window. That was previously buried; now it's a row on the same card.

### Key Concepts

- **Backend** (Go on AWS Lambda): the iOS app asks the backend for a date's data; the backend computes the five-block split and sends it back as JSON.
- **kWh integration**: turning power readings (watts at 10-second intervals) into energy used (kilowatt-hours over a window). The existing helper `integratePload` handles this.
- **Boundary source**: each block's start/end can come from real readings ("readings") or from a fallback sunrise/sunset table ("estimated"). The card shows a "≈ sunrise" / "≈ sunset" hint next to estimated edges so the user knows it's an approximation.

---

## Intermediate Level

### Changes Overview

**Backend (Go, `internal/api/`):**
- `compute.go`: new `findDailyUsage` function (~200 lines), helper `buildDailyUsageBlock`, local `pendingBlock` struct, `recentSolarThreshold = 5 * time.Minute` constant. The previous `findEveningNight` and `buildEveningNightBlock` are deleted.
- `response.go`: new `DailyUsage` and `DailyUsageBlock` structs, plus status/boundary/kind string constants. `DayDetailResponse.EveningNight` removed; `DayDetailResponse.DailyUsage *DailyUsage` added with `omitempty`.
- `day.go`: handler wiring swapped (`findDailyUsage(...)` replaces `findEveningNight(...)` and now passes the SSM-configured off-peak window).
- `compute_test.go`: 18 fixtures driving `TestFindDailyUsage`, plus `TestFindDailyUsage_PercentOfDay`, `TestBuildDailyUsageBlock`, and `BenchmarkFindDailyUsage`.
- `day_test.go`: integration cases updated; `TestHandleDayDailyUsageOvercast` replaces the old per-block fallback test.

**iOS (Swift, `Flux/`):**
- `Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift`: new `DailyUsage`, `DailyUsageBlock` types with `Kind`/`Status`/`BoundarySource` enums. Old `EveningNight` types deleted. `DayDetailResponse` memberwise init signature updated.
- `Flux/DayDetail/DailyUsageCard.swift`: new SwiftUI view with one row per block, caption rendered positionally adjacent to the estimated edge.
- `Flux/DayDetail/DayDetailView.swift`: card slot now renders `DailyUsageCard` between `PeakUsageCard` and the summary.
- `Flux/DayDetail/DayDetailViewModel.swift`: `dailyUsage: DailyUsage?` property replaces `eveningNight`.
- `Services/MockFluxAPIClient.swift`: preview fixture rewritten to compute Sydney-local UTC timestamps so the SwiftUI preview renders all five rows correctly.
- Tests: `DayDetailViewModelTests.swift`, `APIModelsTests.swift`, `StatusTimelineLogicTests.swift` updated to the new field name and shape.

### Implementation Approach

**Backend pipeline (six steps, in order, in `findDailyUsage`):**

1. Resolve nominal block intervals from `dayStart`, `firstSolarTS`, `offpeakStartTime`, `offpeakEndTime`, `lastSolarTS`, `dayEnd`.
2. Solar-window guard: if the strict invariant `firstSolar < offpeakStart < offpeakEnd < lastSolar` fails, fall through to a two-block path emitting only `night` and `evening`. Same path on off-peak SSM misconfiguration.
3. Today-gate (decision 9): when "solar is still up" — `recentSolar` exists, or no qualifying Ppv readings yet and request is before sunset — omit `evening`, set `afternoonPeak.end = now`, mark `afternoonPeak.statusOverride = "in-progress"`.
4. Future-omit: drop blocks whose `start > now` on today.
5. In-progress clamp: blocks whose `end > now` get `end = now` and `status = "in-progress"`. Otherwise honour `statusOverride` or default to `"complete"`.
6. Degenerate-omit: drop blocks where `start >= end` after clamping.

After the pipeline, a two-pass integration produces the per-block totals: pass 1 calls `integratePload` once per surviving block (reusing the existing helper, no re-implementation), sums into `unroundedSum`; pass 2 maps each `pendingBlock` through `buildDailyUsageBlock`, which formats start/end as RFC 3339 UTC, computes `averageKwhPerHour` from the unrounded total, and computes `percentOfDay = round(unroundedKwh / unroundedSum * 100)` with `0` when the sum is zero.

**Decision 12 (lastSolar override):** before step 2 evaluates the invariant, if `solarStillUp` fires, the function overrides `lastSolarTS` to the computed sunset and sets `lastSolarFromFallback = true` even when the `lastSolar` tracker is non-nil. Without this, today + recent-solar requests mid-day fail the invariant and lose the morningPeak/offPeak detail. The today-gate's clamp on `afternoonPeak.end = now` (with `endEstimated = false`) cancels any visible boundarySource side-effect.

**iOS view layout:**

`DailyUsageCard` renders a `VStack` of rows. Each row is itself a `VStack(alignment: .leading)` with three `HStack`s: the label and time-range on line 1, the totals line on line 2, the percentage and `(so far)` indicator on line 3. The tricky part is the caption position — `≈ sunrise` renders before the time when the start is estimated (morningPeak, evening), after the time when the end is estimated (night, afternoonPeak), and never for offPeak. A `@ViewBuilder` `timeRangeView` returns an `HStack(spacing: 4)` that places the caption on whichever side the `captionLeads(kind)` switch dictates. `Text + Text` concatenation can't be used here because the row's outer HStack mixes view types with a `Spacer`.

### Trade-offs

- **Replace, not add**: removed `eveningNight` from the response in lockstep with the iOS update (decision 5). Simpler payload, single source of truth, but requires a co-ordinated TestFlight + backend rollout.
- **Strict solar-window invariant**: when the invariant fails (broken-recorder days, unusual fixtures), the function falls back to two blocks rather than trying to clip overlapping blocks. Loses morningPeak/offPeak detail on those days but guarantees no overlapping blocks ever render (decision 11).
- **Today-gate uses recent-solar heuristic**, not just `now ≤ sunset` (decision 9). Cloudy late afternoons render evening as in-progress instead of being absorbed by afternoonPeak.
- **No new database schema** (req 1.12). Computation runs over the readings slice already loaded by `handleDay`. No new GSI, no new fetch.

---

## Expert Level

### Technical Deep Dive

**Single-pass reading scan** (`compute.go:684–697`): one loop over the readings slice tracks `firstSolar`, `lastSolar` (both filtered to the closed window `[sunrise − 30 min, sunset + 30 min]` per decisions 8 + 10), and (when `isToday`) `recentSolar` over `[now − 5 min, now]`. `hasQualifyingPpv` is derived from `firstSolar != nil` rather than a separate counter, which avoids double-counting when the qualifying filter passes.

**Symmetry of the blip filter** (decision 10): the post-sunset upper bound is new — the predecessor `findEveningNight` only filtered the lower bound. Without it, a 22:00 Ppv blip would push `lastSolar = 22:00` and swallow the evening into afternoonPeak. The filter applies to `lastSolar` discovery, *not* to `recentSolar`, because `recentSolar` is meant to detect "polling reported nonzero solar within the last 5 min" regardless of where in the day that lands.

**Pipeline ordering matters** (decision 6): the today-gate must fire before the future-omit and in-progress clamp because the gate rewrites `afternoonPeak.end = now` and that value subsequently feeds the clamp's `end > now` check (which now compares `now > now` → false, so no further clamp). Reversing the order would silently uncount the gap between `lastSolar` and `requestTime` on cloudy mid-afternoon requests.

**`pendingBlock.statusOverride` sentinel** (`compute.go:878`): a function-local field that distinguishes "today-gate set this block to in-progress and rewrote its end" from "in-progress clamp set this block to in-progress because end > now". Both produce `Status = "in-progress"` and `endEstimated = false`, but only the gate path skips the clamp's `end = now` rewrite (because the gate already set it). The sentinel is never serialised — `buildDailyUsageBlock` only reads `p.status`.

**`boundarySource` is decided once, not derived from string comparison**: each `pendingBlock` carries `(startEstimated, endEstimated)` booleans set when the block is constructed; `buildDailyUsageBlock` computes `BoundarySource = "estimated" iff p.startEstimated || p.endEstimated`. Crucially, both the today-gate clamp (`compute.go:816`) and the in-progress clamp (`compute.go:834`) reset `endEstimated = false` after rewriting `end`, so a clamped block that originally had a sunset-fallback end correctly reports `boundarySource = "readings"`. This drops out the AC 3.4 invariant "no caption on in-progress night" without a special-case rule.

**Decision 12 cancellation**: the `lastSolar` override pushes `lastSolarTS = sunset` and `lastSolarFromFallback = true` when `solarStillUp` fires. The reason it has no visible effect on `boundarySource`:

- `afternoonPeak.endEstimated = lastSolarFromFallback = true` initially, but the today-gate (when fires) clamps `afternoonPeak.end = now` and resets `endEstimated = false`.
- `evening.startEstimated = lastSolarFromFallback = true` initially, but the today-gate (when fires) omits `evening` entirely.

If `solarStillUp` fires *and* the today-gate clamp doesn't apply for some reason (e.g. invariant failure dropping us to the two-block path), the override propagates to `evening.startEstimated = true`. The two-block path test fixtures cover this.

**Two-pass percentOfDay** (`compute.go:855–865`): pass 1 stores unrounded `integratePload` results in each surviving `pendingBlock.unroundedKwh` and accumulates `unroundedSum`; pass 2 calls `buildDailyUsageBlock(p, unroundedSum)`. The helper rounds `totalKwh` and `averageKwhPerHour` to two decimals via `roundEnergy` only at the end, so cross-block percentages are computed against unrounded values per req 1.7's "rounding at serialization only" precedent. The integer percent rounding can produce ±3 drift across five blocks; tests assert `sum ∈ [97, 103]` rather than exact 100.

**DST correctness**: block durations are computed as `endUnix - startUnix` in seconds, which is naturally correct on 23h and 25h days. The DST fixtures in `compute_test.go` (2025-10-05 spring forward, 2026-04-05 fall back) explicitly assert `dayEnd.Unix() - dayStart.Unix() ≈ 23×3600 / 25×3600` and that all five blocks emit cleanly.

**Caller contract**: `today` must be the calendar date `now` falls on in `Australia/Sydney`. The handler at `day.go` already satisfies this. Passing UTC-formatted `today` would silently miss the today-gate at the midnight transition. `readings` must be sorted ascending by `Timestamp` — DynamoDB's `ScanIndexForward: true` guarantees this in production.

**iOS `timeRangeView` view-builder**: SwiftUI's `Text + Text` operator only works on `Text` operands. The row's outer `HStack` mixes a `Text` label, `Spacer`, and the time-range view, so concatenation doesn't compose. The `@ViewBuilder` HStack with `spacing: 4` is the simplest correct solution.

### Architecture Impact

**No new infrastructure**. The function consumes `[]dynamo.ReadingItem` already in scope and reuses `integratePload`, `melbourneSunriseSunset`, `parseOffpeakWindow`, `preSunriseBlipBuffer`, and `roundEnergy`. Per-request cost is one extra single-pass scan + five extra `integratePload` calls (each linear in readings) — `BenchmarkFindDailyUsage` runs in the same order of magnitude as `BenchmarkFindEveningNight` over an 8640-reading day.

**Breaking API change** (decision 5): `eveningNight` is removed from `/day` payload, not deprecated. Two-user app, no version pinning. iOS update must reach both devices via TestFlight before the backend deploys, otherwise the Day Detail card silently disappears for unupdated installs.

**Constants placement**: `recentSolarThreshold = 5 * time.Minute` is defined alongside `preSunriseBlipBuffer` in `compute.go`. Status/boundary/kind constants live in `response.go` next to the type definition; this matches the `OffpeakStatus*` precedent in `internal/dynamo`.

### Potential Issues

- **Caller correctness around `today`**: future refactors of `day.go` must keep `today` as a Sydney-local date string. A unit-tested wrapper or named type would harden this contract.
- **Five rows on small-screen iPhones**: the card is taller than the predecessor. Spec design.md acknowledges this; if a regression is observed on iPhone SE-class devices, the row layout can compress to two visual lines (label/time on one, totals/percent on the other).
- **TestFlight install skew**: install-time mismatch between iOS and backend hides the card silently. There is no telemetry for this; mitigation is procedural (iOS first, wait for both devices, then backend).

---

## Validation: Requirements Coverage

| AC | Status | Implementation site |
|----|--------|---------------------|
| 1.1 | ✅ Implemented | `compute.go:867` returns `&DailyUsage{...}` only when blocks survive; `response.go:107` `omitempty`; caller-side gate at `day.go` skips when readings empty (req 1.10) |
| 1.2 | ✅ Implemented | `compute.go:743–798` builds five intervals from `dayStart`, `firstSolarTS`, `offpeakStartTime`, `offpeakEndTime`, `lastSolarTS`, `dayEnd` |
| 1.3 | ✅ Implemented | `response.go:143–152` carries all 8 fields; `compute.go:887–913` populates them |
| 1.4 | ✅ Implemented | `compute.go:859` calls `integratePload` once per surviving block; no re-implementation |
| 1.5 | ✅ Implemented | `compute.go:743–797` resolves per-edge fallback flags; clamps reset `endEstimated = false` (`compute.go:816, 834`); `buildDailyUsageBlock:893–895` derives `boundarySource` |
| 1.6 | ✅ Implemented | `compute.go:905–908` omits `AverageKwhPerHour` when `elapsed < 60`; `TestBuildDailyUsageBlock` covers this branch |
| 1.7 | ✅ Implemented | `compute.go:855–865` two-pass integration, unrounded sum, `compute.go:909–911` rounds at serialization |
| 1.8 | ✅ Implemented | All six pipeline steps present in documented order; Decision 12 documents the lastSolar override |
| 1.9 | ✅ Implemented | Surviving blocks remain in build order; "typical past day" test asserts kind sequence |
| 1.10 | ✅ Implemented | `day.go` skips `findDailyUsage` when readings empty or fallback path; `omitempty` drops field |
| 1.11 | ✅ Implemented | `compute.go:725–739` triggers two-block path on parse failure or `start ≥ end` |
| 1.12 | ✅ Implemented | No new tables/queries; reuses readings slice from `handleDay` |
| 1.13 | ✅ Implemented | Same two-block code path as 1.11 (reserved alias) |
| 2.1 | ✅ Implemented | `response.go:107` `dailyUsage *DailyUsage `json:"dailyUsage,omitempty"`` |
| 2.2 | ✅ Implemented | `EveningNight` types deleted from production code; only spec/CHANGELOG retain references |
| 2.3 | ✅ Implemented | `DailyUsage.Blocks` ordered by build, ≤5; Swift `Identifiable` keyed by `kind` |
| 2.4 | ✅ Implemented | `DayDetailResponse` retains date/readings/summary/peakPeriods |
| 3.1 | ✅ Implemented | `DayDetailView.swift:31–39` renders `DailyUsageCard` between `PeakUsageCard` and summary |
| 3.2 | ✅ Implemented | `DailyUsageCard.swift:21–47, 99–113` — rows with label, time range, totals, percentage |
| 3.3 | ✅ Implemented | `DailyUsageCard.swift:36–40` `(so far)` indicator |
| 3.4 | ✅ Implemented | `DailyUsageCard.swift:71–97` label/caption/captionLeads matches spec table |
| 3.5 | ✅ Implemented | `DailyUsageCard.swift:7–19` `.thinMaterial`, `RoundedRectangle(cornerRadius: 16, style: .continuous)`, `.headline` |
| 3.6 | ✅ Implemented | `DayDetailView.swift:35–37` guard `hasPowerData` + non-nil + non-empty blocks |
| 3.7 | ✅ Implemented | `DailyUsageCard.swift:108–113` omits `· kWh/h` when `averageKwhPerHour == nil` |
| 4.1 | ✅ Implemented | `TestFindDailyUsage` covers 18 fixtures including the `today + overcast mid-morning` fixture |
| 4.2 | ✅ Implemented | `TestFindDailyUsage_PercentOfDay` asserts sum ∈ [97, 103] and zero-load = 0 |
| 4.3 | ✅ Implemented | `DayDetailViewModelTests.swift:124–290` covers all five required cases |

### Completeness Assessment

**Fully implemented:** every acceptance criterion has a code site and a test. Backend covers 18 fixtures of `TestFindDailyUsage` plus dedicated tests for `buildDailyUsageBlock` boundarySource matrix and the elapsed-below-60s branch. iOS covers all five `DayDetailViewModelTests` cases plus four `APIModelsTests` decode cases (all five blocks, dailyUsage absent, two-block-only, null average).

**Partially implemented:** none.

**Missing:** none — the AC 4.3 caveat ("caption rendered when boundarySource = estimated" exercised at view-model construction time only, not view layer) is documented in the spec design and matches `EveningNightCard` precedent. Adding view-layer testing (ViewInspector / snapshot) is out of scope per the design.

**Documented divergences:**
- Decision 12 (lastSolar override on `solarStillUp`): added during pre-push review to formalise an undocumented behaviour the implementation needed for correctness on today + recent-solar mid-day requests.
