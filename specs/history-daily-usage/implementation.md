# Implementation: History Daily Usage

Explanations of the T-1022 implementation at three audience levels, followed by a per-AC completeness assessment.

## Beginner Level

### What Changed

The History screen on the iOS app now has a new card called "Daily usage" that shows, for each day in the selected range (7 / 14 / 30 days), a stacked bar telling you how much electricity you used in five different parts of the day. The five parts are Night, Morning Peak, Off-Peak, Afternoon Peak, and Evening — the same chunks you already see on the Day Detail screen for one day.

Each bar in the new card has up to five coloured segments, one per chunk. Indigo for Night, orange for Morning Peak, teal for Off-Peak, red for Afternoon Peak, purple for Evening. Today's bar is dimmed to 50% opacity because today isn't finished yet. Tapping a bar selects that day across the whole History screen, just like tapping any of the existing cards (Solar, Grid, Battery).

There is also a tiny piece of "behind the scenes" plumbing fixed in the same change: when the app caches a day's data on disk for offline reading, it now also remembers the per-chunk breakdown (and a few other related fields). Before this change those fields silently weren't being saved, which meant offline users couldn't see them.

### Why It Matters

Up to today the History screen could tell you *how much* you used per day, but not *when* during the day. The new card lets you spot patterns — if your Evening chunk is consistently the biggest, that's a hint your habit-shifting candidate is dinner-time loads, not the overnight baseline. Pinning the same five colours and labels across History and Day Detail also means your eyes don't have to retrain when you switch screens.

### Key Concepts

- **Block / chunk**: a fixed slice of the day. There are five — Night, Morning Peak, Off-Peak, Afternoon Peak, Evening. The boundaries are computed by the backend from sunrise / sunset and the off-peak window settings.
- **Stacked bar**: a bar where the total is split into pieces, with each piece coloured. The whole bar's height is the day's total; each segment's height is that block's contribution.
- **Cache**: the app's local copy of recent days, used so it can show something useful when there's no internet. Before this change the cache was missing four of the day's "derived" fields — including the per-block breakdown — even though the database column was already there.

---

## Intermediate Level

### Changes Overview

Three new Swift files in the iOS app, one Day Detail file refactored, the History view-model extended, and tests added across four files.

| File | Role |
|---|---|
| `Flux/Flux/History/HistoryDailyUsageCard.swift` | New SwiftUI Charts view. Mirrors `HistoryGridUsageCard`'s shape: `HistoryCardChrome` wrapper, KPI / subtitle, conditional placeholder vs. chart, `historySelectionOverlay` for column-tap selection. |
| `Flux/Flux/History/DailyUsageBlockKindStyling.swift` | Extension on `FluxCore.DailyUsageBlock.Kind` exposing `chronologicalOrder`, `chronologicalIndex`, `chartColor`, `displayLabel`. Single source of truth shared with Day Detail. |
| `Flux/Flux/History/HistoryCacheLog.swift` | Wraps `os.Logger(subsystem: "eu.arjen.flux", category: "history-cache")` and exposes a default warn closure for the view-model to call when the cache upsert clears a previously-non-nil derived field. |
| `Flux/Flux/History/HistoryDerivedState.swift` | Existing extension file that gains `DailyUsageEntry`, `DailyUsageEntryBlock`, an extended `DerivedState` and `PeriodSummary`, and a per-kind sum tracker on `Totals`. |
| `Flux/Flux/History/HistoryViewModel.swift` | Adds an injectable `warn` callback parameter; `cacheHistoricalDays` upserts the four derived fields with a `warnIfClearing` helper that fires once per (date, fieldName) on non-nil → nil transitions. |
| `Flux/Flux/History/HistoryView.swift` | Inserts `HistoryDailyUsageCard` between the Battery card and the per-day summary card. |
| `Flux/Flux/DayDetail/DailyUsageCard.swift` | Replaces a private `label(for:)` switch with `block.kind.displayLabel` so labels stay in lockstep across screens. |
| `Flux/Flux/Services/MockFluxAPIClient.swift` | `historyDays` factory now emits `dailyUsage` per row, reusing the existing `dayDailyUsage(for:)` helper, so the SwiftUI preview renders the new card. |

### Implementation Approach

**Series derivation**: `DerivedState.init(days:now:)` already iterated `days` once to build `solar` / `grid` / `battery` entries plus a `Totals` running aggregate. The change adds a fourth append (`dailyUsage`) and one more `Totals` mutator. The per-day filter rule is "include if `DayEnergy.dailyUsage` is non-nil and has a non-empty `blocks` array"; today's row is included in the series (with `isToday = true`) but excluded from the period aggregates so its partial total doesn't skew the daily average.

**Block sort**: backend payload order isn't a contract (Decision 7). Each day's blocks are sorted into `Kind.chronologicalOrder` (`night → morningPeak → offPeak → afternoonPeak → evening`) inside the entry builder. AC 5.1 (b) covers a non-chronological payload fixture.

**Tie-break**: the period summary's "largest contributing block kind" uses a 0.01 kWh tolerance band (Decision 5 / AC 1.8): kinds whose summed `totalKwh` differ by less than that are treated as tied, then chronological order wins. Implemented by iterating `Kind.chronologicalOrder` and only swapping when the new sum is strictly greater than the current best by more than 0.01.

**Clamp**: `DailyUsageEntryBlock.totalKwh` is `max(0, payload.totalKwh)`, so negative values from the backend are pinned at zero for chart rendering, period sums, and accessibility labels in one place (AC 1.13).

**Card view**: SwiftUI Charts' `BarMark` auto-stacks when multiple marks share an `x` value and use `.foregroundStyle(by:)` keyed on a series identifier. The card emits one `BarMark` per block per entry, keyed by `displayLabel`, and pins the legend order with `chartForegroundStyleScale(domain:range:)` (the explicit-array overload — the `[String: Color]` dictionary literal does not preserve iteration order).

**X-axis density**: `.stride(by: .day, count: max(1, entries.count / 6))`. At 30 days that's stride 5, producing 6 marks ≤ AC 2.2's "≤ 7 labels" budget. The 7- and 14-day cases reduce stride to 1 or 2.

**Animation**: `.animation(.default, value: entries.count)` on the Chart, not on individual marks. Range changes (7 → 14 → 30) produce one relayout, not 150 simultaneous mark animations (AC 2.1).

**Accessibility**: `.accessibilityElement(children: .ignore)` plus `.accessibilityRepresentation { List { ForEach(entries) { Text($0.accessibilitySummary) } } }`. Replaces Charts' default per-mark a11y tree (which would expose 150 elements at 30 days × 5 segments) with one element per day. The summary string is `"{date}: {kwh}, {largestKindForThisDay} largest"` using the clamped values (AC 1.12 / 1.13).

**Selection**: reuses the existing `historySelectionOverlay` (`chartOverlay` + `DragGesture` + `proxy.value(atX:)`) for parity with the other three History cards. Decision 8 deferred a four-card migration to iOS 26's `chartXSelection(value:)` to a separate ticket.

**Cache upsert observability**: `HistoryViewModel.init` gains `warn: @escaping @Sendable (String) -> Void = HistoryCacheLog.defaultWarn`. The new `warnIfClearing(cached:day:)` checks each of the four derived fields for the non-nil → nil transition and calls `warn` once per (date, fieldName) pair before the assignment. Tests inject an `actor WarnSink` for Swift 6 strict-concurrency-safe capture.

**Test-shaped statics**: `HistoryDailyUsageCard.{kpi, subtitle, opacity, placeholderCopy, shouldShowPlaceholder}` are static so unit tests can assert the formatted strings without rendered-tree inspection (the project has none). Documented in `docs/agent-notes/ios-app-views.md`.

### Trade-offs

- **Add a card vs. retire / replace Grid card**: kept additive (Decision 4). Off-peak load (new) and off-peak grid import (existing) are different metrics over the same window; conflating them loses information.
- **Five blocks vs. collapsed three / four**: kept five (Decision 2) for vocabulary parity with Day Detail. A 30-day chart of five-stack bars is dense but readable.
- **Modern `chartXSelection` vs. legacy overlay**: reused legacy (Decision 8) for parity with the three existing cards. Migrating only this card would split selection mechanics across the same screen.
- **Cache nil-overwrite policy**: overwrite unconditionally with a warn-log on non-nil → nil (Decision 6). Backend is authoritative; the warning gives the operator a signal if a deploy mistakenly emits nil for past dates.
- **Block sort source-of-truth**: client-side (Decision 7). Three lines of negligible work decouples this UI from a backend implementation invariant.

---

## Expert Level

### Technical Deep Dive

**Series build is single-pass** — no second iteration over `days` to build the daily-usage series or the per-kind sums. `Totals` accumulates `[Kind: Double]` while iterating the same `days` slice; `largestDailyUsageKind` is computed once at snapshot time. Cost is O(n × k) where n ≤ 30 and k = 5, so ~150 dictionary updates worst-case.

**Tolerance-band tie-break**, not rounding-then-equality, was chosen so the contract is symmetric: two sums at 1.2049 and 1.2051 round to 1.20 (would be treated equal under naïve rounding) but actually differ by 0.0002, well below the 0.01 threshold. The 0.01 band matches AC 1.8's stated precision directly so the AC text and the code agree without translation. Implementation: iterate `chronologicalOrder` (which has indices 0…4 ascending), only swap when `(sum - current.sum) >= 0.01`. The first kind to achieve a strictly-greater-by-tolerance sum wins; later equal-or-tied kinds don't displace it. This is equivalent to "sort by sum descending, break ties by chronological index ascending" but avoids a sort.

**`max(by:)` at day-level is consistent with the period-level rule** by accident-but-not-coincidence: blocks are pre-sorted into chronological order, and Swift's `Sequence.max(by:)` returns the *first* element on ties (it only swaps when `<` is strictly true). So the day-level "largest kind" in `accessibilitySummary` falls into the same chronological tie-break as the period-level — without an explicit tolerance band, but at the per-day granularity ties of >0 kWh values are vanishingly unlikely against IEEE 754 representations of backend-summed quarter-hour readings.

**`@Sendable` warn callback** is required because `HistoryViewModel` is `@MainActor @Observable` but the closure escapes the init and may be invoked from any context. The default `HistoryCacheLog.defaultWarn` wraps `os.Logger.warning` which is itself thread-safe; the test sink is an `actor WarnSink` whose `record(_:)` is invoked via `Task { await sink.record(line) }`. The test helper `waitForLines(count:timeoutMillis:)` polls instead of relying on a specific scheduling window, since unstructured `Task` launches don't guarantee ordering.

**Idempotence of `warnIfClearing`**: a second `loadHistory` with the same nil response finds `cached.field` already nil (cleared by the first pass), so the precondition `cached.field != nil` is false and no warning is emitted. The warning fires only on the transition non-nil → nil, never on the steady state. Important for offline-mode users who may load the same range repeatedly.

**`chartForegroundStyleScale(domain:range:)` explicit-array overload, not `[String: Color]`** — the dictionary-literal overload's iteration order is documented as "implementation-defined" by Swift's `Dictionary`, which means the legend's left-to-right order is not deterministic. The two existing cards (`HistoryGridUsageCard`, `HistorySolarCard`) use the dictionary form; both have ≤3 series and rely on Charts' incidental ordering. The new card has 5 series and explicit ordering is load-bearing for AC 1.5, so the array overload is the right tool here. (Migrating the existing cards is out of scope.)

**SwiftUI Charts default a11y produces 1 element per `BarMark`** — at 30 days × 5 blocks that's 150 elements, which would make day-by-day VoiceOver navigation tedious. The fix is `accessibilityElement(children: .ignore)` (suppresses Charts' a11y tree) plus `accessibilityRepresentation { List }` (substitutes a 1-per-day list). This combination is the documented SwiftUI pattern for replacing one accessibility element with another. Verification on a real device (with VoiceOver enabled) is required because Charts' internal a11y behaviour is implementation-defined; AC 5.5 captures this in the manual pass.

**Animation on `entries.count`, not on the entry list itself** — `entries.count` is always 7 / 14 / 30, allocates nothing, and changes exactly when the range changes. Using `value: entries` would equate to "the chart animated whenever the data changed", which fires on every `loadHistory` refresh, not just range switches. The narrower trigger keeps the chart visually stable during `viewModel.loadHistory(days:)` reloads of the same range.

### Architecture Impact

The card extends an established pattern (per-day series on `DerivedState` driving a card with `historySelectionOverlay`) without introducing a new abstraction. The shared `Kind` extension keeps `Color` out of FluxCore (where it would force a SwiftUI dependency on a model package). The cache backfill change is a localised bug-fix in `cacheHistoricalDays` plus an injectable observability hook on `init` — no API surface change for callers (the parameter has a default).

The injectable `warn` parameter establishes the pattern for future cache observability without coupling the view-model to `os.Logger` at the API surface. If a second similar pattern shows up (e.g. dashboard cache), the same shape (`@escaping @Sendable (String) -> Void` + a `Logger`-wrapped default) generalises.

The 5-block × 30-day chart is the densest visualisation in the app. If a sixth series were ever added (the colour palette is near-exhausted at 5 well-spaced semantic hues, per Decision 5's "Negative consequence"), legibility would degrade fast. A future change would need a different layout (grouped bars or per-block small multiples), which would not be backwards-compatible with this card's selection contract.

### Potential Issues

- **Backend nil-emit regression**: a deploy that sets `dailyUsage` to nil for past dates triggers the warn-log per affected date. No surfacing in-app (release builds without log inspection see a silent wipe). Acceptable for a two-user personal app; not at scale.
- **Charts a11y replacement**: `accessibilityElement(children: .ignore)` may not deterministically suppress Charts' internal tree across iOS minor versions. The design.md documents a fallback (`ZStack { Chart.accessibilityHidden(true); hiddenList }`) if the primary form regresses on a future iOS update.
- **Stride collision at 12 days**: `.stride(by: .day, count: max(1, 12/6))` = 2, producing 6 marks for a 12-day range (not a supported range, but if added would still satisfy ≤ 7).
- **Predicate fan-out on `cacheHistoricalDays`**: SwiftData's `datesToCache.contains(cached.date)` builds an IN-list against the SQLite store. At 30 dates this is fine; at 365+ the predicate would balloon. The existing range cap is 30, so no current concern.
- **Decision 5's red overlap**: Afternoon Peak (`Color.red`) and Grid card's "Peak import" (`Color.red`) both render red on the same screen but mean different things. UX review is the gate before merge per Decision 5.

---

## Completeness Assessment

Per-AC verdict against requirements.md. Sources: `Flux/Flux/History/{HistoryDailyUsageCard,HistoryDerivedState,DailyUsageBlockKindStyling,HistoryCacheLog,HistoryViewModel,HistoryView}.swift`, `Flux/FluxTests/{DailyUsageBlockKindStylingTests,HistoryDailyUsageCardTests,HistoryViewModelDailyUsageTests,HistoryViewModelCacheUpsertTests}.swift`.

| AC | Status | Evidence |
|---|---|---|
| 1.1 | Fully implemented | `HistoryView.swift:71` inserts the card after `HistoryBatteryCard` and before the per-day summary. |
| 1.2 | Fully implemented | `HistoryDailyUsageCard.swift` chart emits one `BarMark` per block per entry, stacked at the same `x = entry.date`. |
| 1.3 | Fully implemented | `HistoryDerivedState.swift` sorts blocks by `chronologicalIndex`. Tested by `dailyUsageSeriesSortsBlocksWhenPayloadOrderIsMixed`. |
| 1.4 | Fully implemented | `DailyUsageBlockKindStyling.swift` `chartColor` returns `.indigo / .orange / .teal / .red / .purple`. Asserted by `chartColorMapsPerDecisionFive`. |
| 1.5 | Fully implemented | `chartForegroundStyleScale(domain:range:)` uses `chronologicalOrder.map(\.displayLabel)` / `.chartColor`. Day Detail uses the same `displayLabel`. Asserted by `displayLabelMatchesDayDetailStrings`. |
| 1.6 | Fully implemented | `HistoryDailyUsageCard.opacity(for:)` returns 0.5 / 1.0; `.opacity(Self.opacity(for: entry))` applied per BarMark. |
| 1.7 | Fully implemented | Days filtered out via `dailyUsageEntry` (returns `nil`); `ForEach(entries)` skips them, producing an x-axis gap. |
| 1.8 | Fully implemented | Subtitle: `"\(largest.displayLabel) largest at \(HistoryFormatters.kwh(avg))/day average"` — `HistoryFormatters.kwh(3.4)` returns `"3.4 kWh"`, so the rendered string is `"Evening largest at 3.4 kWh/day average"`. Asserted by `subtitleFormatsLargestKindAndAverage`. Tolerance-band tie-break in `Totals.largestDailyUsageKind`; asserted by `dailyUsageLargestKindBreaksTiesByChronologicalOrder`. |
| 1.9 | Fully implemented | `shouldShowPlaceholder` checks `summary.dailyUsageDayCount == 0`, which is 0 in both the no-blocks and today-only cases. KPI returns "—", subtitle returns nil, body renders `placeholder` view. |
| 1.10 | Fully implemented | `.historySelectionOverlay(entries:onSelect:)` reused unchanged. `onSelect` is `selectDay` plumbed through `HistoryView`. |
| 1.11 | Fully implemented | Wrapped in `HistoryCardChrome`. Chart container uses `.frame(minHeight: 180)`. |
| 1.12 | Fully implemented | `accessibilityElement(children: .ignore) + accessibilityRepresentation { List { ForEach(entries) { Text($0.accessibilitySummary) } } }`. `accessibilitySummary` summarises date + total + largest-kind-for-day using clamped values. |
| 1.13 | Fully implemented | Clamp at `dailyUsageEntry`'s `max(0, $0.totalKwh)` flows into chart rendering, period sums, and accessibility labels (all read clamped fields). Asserted by `dailyUsageClampsZeroAndNegativeBlocks`. |
| 2.1 | Fully implemented | `.animation(.default, value: entries.count)` on the chart. No per-mark `.animation` modifier. |
| 2.2 | Fully implemented | `.chartXAxis { AxisMarks(values: .stride(by: .day, count: max(1, entries.count / 6))) }`. 30 days → stride 5 → 6 marks (≤ 7). |
| 2.3 | Fully implemented | Default `BarMark` width respects plot width; at iPhone SE 343 pt plot ÷ 30 columns ≈ 11 pt before inter-bar padding. No explicit `width:` cap. (Manual gate AC 5.5.) |
| 2.4 | Pending manual verification | AC 5.5 gates this — the PR description must record the device + iOS version of the 7→30→7 toggle test. |
| 3.1 | Fully implemented | `DailyUsageEntry` has `date`, `dayID`, `blocks` (sorted), `stackedTotalKwh`, `isToday`. |
| 3.2 | Fully implemented | `dailyUsageEntry` returns nil for `dailyUsage == nil` or empty `blocks`. Asserted by `dailyUsageSeriesTreatsEmptyBlocksArrayAsMissing` and `dailyUsageSeriesOmitsDaysWithoutDailyUsage`. |
| 3.3 | Fully implemented | `PeriodSummary.{dailyUsageTotalKwh, dailyUsageDayCount, dailyUsageLargestKind, dailyUsageLargestKindTotalKwh}` plus `dailyUsageAvgKwh` / `dailyUsageLargestKindAvgKwh` accessors. |
| 3.4 | Fully implemented | `addDailyUsage` only called when `!isToday`; today's bar appears in the series with `isToday = true` but doesn't contribute to aggregates. Asserted by `dailyUsageSummaryExcludesTodayMidWindow`. |
| 3.5 | Fully implemented | Single-pass `for day in days` in `DerivedState.init(days:now:)`. |
| 4.1 | Fully implemented | `cacheHistoricalDays` existing-row branch assigns all four derived fields unconditionally. Asserted by `cacheBackfillsDerivedFieldsWithoutWarnings` and `cacheClearsDerivedFieldsAndEmitsWarnings`. |
| 4.2 | Fully implemented | `warnIfClearing` emits one line per (date, field) pair via the injected `warn`; default uses `HistoryCacheLog.defaultWarn` → `Logger(subsystem: "eu.arjen.flux", category: "history-cache")`. Each line includes both the date string and the field name. Asserted by `cacheClearsDerivedFieldsAndEmitsWarnings`. |
| 4.3 | Fully implemented | `loadCachedDays` returns `cached.asDayEnergy` whose `dailyUsage` round-trips through `CachedDayEnergy`. Asserted by `cacheRoundTripPreservesDailyUsageBlocks`. |
| 4.4 | Fully implemented | `cacheHistoricalDays` filters with `!DateFormatting.isToday($0.date, now: now)` before fetching cache rows or writing. |
| 5.1 a–j | Fully implemented | All ten fixtures present in `HistoryViewModelDailyUsageTests.swift`: chronological / mixed-payload / off-peak-unresolved / mixed-presence / all-nil / today-only / empty-array / today-mid-window / clamp / tie-break. |
| 5.2 | Fully implemented | `HistoryViewModelCacheUpsertTests.swift` covers backfill (no warn) and clear (one warn per cleared field). |
| 5.3 | Fully implemented | `cacheRoundTripPreservesDailyUsageBlocks` asserts field equality on the blocks list across the round-trip. |
| 5.4 | Fully implemented | `HistoryDailyUsageCardTests.swift` covers opacity, placeholder copy, KPI / subtitle formatting, accessibility-tree count == entries.count. Legend ordering is asserted at the `chronologicalOrder` data-model level (`DailyUsageBlockKindStylingTests`). |
| 5.5 | Pending manual verification | PR description must include the device model + iOS version. |

**Summary**: 30 / 32 ACs are fully implemented in code and tests. The two remaining (2.4 and 5.5) are manual verification gates that must be exercised on a real device before merge per the spec; both are recorded in the PR description rather than in source.

No requirements are partially implemented or missing. No design divergences identified. The decision log is consistent with the implementation.
