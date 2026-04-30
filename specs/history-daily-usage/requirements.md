# Requirements: History Daily Usage

## Introduction

The History screen visualises solar, grid, and battery totals across 7 / 14 / 30-day ranges but offers no view of how each day's load was distributed across the same five chronological blocks (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening) that Day Detail shows. The `daily-derived-stats` spec already added `dailyUsage` to every `DayEnergy` row in the `/history` response and the on-device cache, so this feature adds the consuming UI: a new History card rendering one stacked bar per day with the per-block load totals, plus the small ViewModel and cache-upsert changes needed to wire the existing data through the card.

## Non-Goals

- Any backend, FluxCore model, or `daily-derived-stats` change — `dailyUsage`, `socLow`, `socLowTime`, and `peakPeriods` are already on the wire and on the cache row, so no SwiftData migration is required.
- Showing the multi-day per-block view on Dashboard, Day Detail, or widgets.
- Replacing or restyling the existing Solar / Grid / Battery cards.
- Computing block totals for ranges greater than 30 days.
- Adding a new range selector.
- Surfacing per-source breakdowns inside any block (solar vs grid vs battery share within Evening, etc.).
- Surfacing `socLow` / `socLowTime` / `peakPeriods` on the History screen — the card is load-only; those derived fields are persisted on the cache for future use but not rendered here.

## Definitions

- **block**: one of the five chronological no-overlap intervals on a single calendar date — `night`, `morningPeak`, `offPeak`, `afternoonPeak`, `evening` — together with its `totalKwh`, `status`, and `boundarySource`. Block kinds, intervals, and computation rules are owned by `daily-derived-stats`.
- **day-with-blocks**: a calendar date in the requested range whose `DayEnergy.dailyUsage` is present and has at least one block.
- **stacked total**: the sum of `totalKwh` across every block emitted for one day.
- **complete day**: a day-with-blocks whose date is strictly before today (resolved through `DateFormatting.isToday(_:now:)`, which the rest of `HistoryViewModel` already uses for the same boundary). Complete days drive the card's averages so that today's in-progress total never skews them.

## 1. iOS: Daily Usage Card

**User Story:** As a Flux user, I want a History card that shows where each day's load went across the same five blocks I see on Day Detail, so that I can spot patterns in my evening / night / peak / off-peak consumption over the past week or month.

**Acceptance Criteria:**

1. <a name="1.1"></a>The History screen SHALL render a "Daily usage" card after the existing Battery card and before the per-day Summary card.  
2. <a name="1.2"></a>The card SHALL render one stacked bar per day-with-blocks in chronological order, oldest at the leading edge, with each block contributing one segment whose height is proportional to its `totalKwh`.  
3. <a name="1.3"></a>Within each day's stack, segments SHALL be ordered bottom-to-top in the chronological block sequence Night → Morning Peak → Off-Peak → Afternoon Peak → Evening, regardless of the order the API payload happens to list them in.  
4. <a name="1.4"></a>Each block kind SHALL render with a fixed colour bound by kind: Night `Color.indigo`, Morning Peak `Color.orange`, Off-Peak `Color.teal` (the same teal `HistoryGridUsageCard` already uses for its off-peak segment), Afternoon Peak `Color.red`, Evening `Color.purple`. The mapping is a single source of truth that the legend, segments, and tests SHALL all read from.  
5. <a name="1.5"></a>The card SHALL include a chart legend identifying each block kind with the same labels Day Detail uses ("Night", "Morning Peak", "Off-Peak", "Afternoon Peak", "Evening"), in the same chronological order as [1.3](#1.3).  
6. <a name="1.6"></a>The bar for any entry whose `isToday` flag is true SHALL render at 50% opacity; all other bars at full opacity. The opacity is keyed on the `isToday` semantic, not on a literal date comparison inside the view.  
7. <a name="1.7"></a>For days the ViewModel filters out per [3.2](#3.2) (no `dailyUsage`, or empty `blocks` array), the rendered chart SHALL show an x-axis gap at that day's position rather than a zero-value bar or placeholder label.  
8. <a name="1.8"></a>The card's KPI line SHALL show the average daily total kWh across complete days-with-blocks in the range, formatted via the existing `HistoryFormatters.kwh` helper. The subtitle SHALL identify the largest contributing block kind over the same window using the literal-text format `"{blockLabel} largest at {kwh} kWh/day average"` — for example, `Evening largest at 3.4 kWh/day average` (no quotes, no backticks in the rendered string). When two block kinds' largest-sum totals differ by less than 0.01 kWh, the comparison SHALL be treated as a tie and the earlier kind in the chronological sequence from [1.3](#1.3) SHALL win.  
9. <a name="1.9"></a>WHEN no day in the response is a day-with-blocks, OR when the only day-with-blocks is today (so no complete day exists for the average), the card SHALL render the same placeholder treatment `HistoryGridUsageCard` uses when its split is unavailable, with text "No load breakdown available for this range." The KPI value and subtitle SHALL be hidden in this state.  
10. <a name="1.10"></a>Tapping anywhere within a day's column SHALL select that whole day across all History cards (column-level selection, not per-segment) using the same gesture and selection-highlight affordance the Solar / Grid / Battery cards already use, so that the same `(dayID, date)` selection flow round-trips through `HistoryViewModel.selectDay(_:)`.  
11. <a name="1.11"></a>The card chrome SHALL be the existing `HistoryCardChrome` container; the chart content area SHALL match the minimum height the other History chart cards use (`.frame(minHeight: 180)`).  
12. <a name="1.12"></a>The chart's accessibility tree SHALL expose one element per day-with-blocks in the range, whose label summarises that day's date, stacked total kWh (one decimal), and the largest block kind for the day. Per-segment elements SHALL NOT be exposed individually, so VoiceOver navigation steps day-by-day rather than segment-by-segment. Accessibility labels SHALL use the clamped (post-[1.13](#1.13)) values, matching what the user sees rendered.  
13. <a name="1.13"></a>Block `totalKwh` values SHALL be rendered in the chart's accessibility / tooltip labels rounded to one decimal, matching the rounding the Day Detail Daily Usage card uses. Blocks whose payload `totalKwh` is zero SHALL render a zero-height segment (legend entry kept, segment invisible); blocks whose payload `totalKwh` is negative SHALL be clamped to zero for chart rendering, aggregation, and accessibility labels.  

## 2. iOS: Chart Density at 30-Day Range

**User Story:** As a Flux user looking at a full month of stacked bars, I want the chart to remain readable, so that I do not need to squint to tell one day's stack from the next.

**Acceptance Criteria:**

1. <a name="2.1"></a>The y-axis SHALL auto-scale to the largest stacked total in the visible range, with no fixed upper bound. Switching the range selector SHALL produce a single animated relayout of the y-axis domain — no per-mark animation SHALL occur during the range change, so a 7→14→30 transition does not produce 150 simultaneous mark animations.  
2. <a name="2.2"></a>The x-axis SHALL render at most one date label per N days (N chosen so the 30-day view shows ≤ 7 labels), with the first and last dates always labelled. The chosen labels SHALL come from the existing `Date` domain — no manual date arithmetic in the view.  
3. <a name="2.3"></a>At the smallest current-iPhone content width (iPhone SE 3rd generation, 375 pt screen width minus the standard `HistoryCardChrome` horizontal padding), a 30-day range SHALL render bar columns at least 6 pt wide. When the available width drops below this floor on a smaller hypothetical surface, the chart SHALL favour reducing inter-bar padding before reducing bar width.  
4. <a name="2.4"></a>The chart SHALL not crash, freeze, or visibly stutter when the user toggles 7 → 30 → 7 ranges five times in succession on a real device. (Tested manually before merge; see [5.5](#5.5).)  

## 3. iOS: ViewModel Series Derivation

**User Story:** As the History view, I want the ViewModel to expose a per-day daily-usage series and a period-summary aggregate, so that the card renders without doing data work in its body.

**Acceptance Criteria:**

1. <a name="3.1"></a>`HistoryViewModel.DerivedState` SHALL expose a `dailyUsage` series whose entries each carry: the parsed `Date`, the day identifier, the day's blocks sorted into the chronological order from [1.3](#1.3), the day's stacked total (sum of clamped block kWh per [1.13](#1.13)), and an `isToday` flag.  
2. <a name="3.2"></a>The series SHALL omit days whose `DayEnergy.dailyUsage` is nil or whose `blocks` array is empty, so the card never sees a "no-blocks" entry for which it would have to decide whether to render a gap.  
3. <a name="3.3"></a>`HistoryViewModel.PeriodSummary` SHALL expose: the sum of clamped `totalKwh` across complete days-with-blocks, the count of complete days-with-blocks, and the kind whose summed clamped `totalKwh` across complete days-with-blocks is largest (with ties broken per [1.8](#1.8)). The largest-block field SHALL be `nil` when no complete day-with-blocks exists in the range.  
4. <a name="3.4"></a>The aggregates in [3.3](#3.3) SHALL exclude today's bar from both the numerator and the denominator.  
5. <a name="3.5"></a>The series and summary SHALL be (re)computed once per `days` update, in a single pass alongside the existing solar / grid / battery series.  

## 4. iOS: Cache Upsert Backfill

**User Story:** As a Flux user with cached history rows from before this feature shipped, I want subsequent successful `/history` calls to backfill the per-day breakdown into the cache, so that the offline fallback can render the card for days I had cached before.

**Acceptance Criteria:**

1. <a name="4.1"></a>The `HistoryViewModel.cacheHistoricalDays` upsert path SHALL update `dailyUsage`, `socLow`, `socLowTime`, and `peakPeriods` on already-cached `CachedDayEnergy` rows on every successful response, in addition to the existing energy fields and note. The update SHALL apply whether the new value is non-nil (overwriting an older nil or stale value) or nil (clearing a previously-cached value the backend no longer returns); the backend is the authoritative source.  
2. <a name="4.2"></a>WHEN [4.1](#4.1)'s upsert overwrites a previously non-nil cached value with nil for any of the four derived fields, the ViewModel SHALL emit one warning-level log line per cleared (date, field) pair via `os.Logger(subsystem: "eu.arjen.flux", category: "history-cache")`, with the date string and the field name in the message. The fixed subsystem/category gives test code a defined capture point and lets the operator filter for these lines via Console.app while a debugger is attached.  
3. <a name="4.3"></a>WHEN the API call fails and the view falls back to cached days, days whose cached `dailyUsage` is non-nil SHALL render their bars; days whose cached `dailyUsage` is nil SHALL render no bar (per [1.7](#1.7)).  
4. <a name="4.4"></a>Today's row SHALL NOT be persisted to the cache, matching the existing rule (`HistoryViewModel.swift` filters today out before caching).  

## 5. Testing

**User Story:** As the project maintainer, I want enough coverage on the new ViewModel derivation, the cache upsert change, and the rendered card to trust that the multi-day view stays consistent with Day Detail and survives offline fallback.

**Acceptance Criteria:**

1. <a name="5.1"></a>`HistoryViewModel` tests SHALL cover, as separate fixtures: (a) every day carries `dailyUsage` with five blocks in chronological order; (b) every day carries `dailyUsage` but blocks arrive in a non-chronological order in the payload (asserts client-side sort per [1.3](#1.3)); (c) one day in the range emits only `night` and `evening` blocks (the off-peak-unresolved shape from `daily-derived-stats`); (d) mixed presence — some days nil; (e) all days nil (empty series, placeholder per [1.9](#1.9)); (f) the only day-with-blocks is today (placeholder per [1.9](#1.9)); (g) one day's `blocks` array is empty (treated as nil per [3.2](#3.2)); (h) today is mid-window with `status == inProgress` on at least one block (asserts `isToday` flag and that the partial today is excluded from the average per [3.4](#3.4)); (i) one day contains a zero-`totalKwh` block and one day contains a negative-`totalKwh` block (asserts clamp per [1.13](#1.13)); (j) two block kinds tie for largest sum across the range, constructed using integer-half kWh values (e.g. 0.5, 1.0, 1.5) so the sums are exactly representable in IEEE 754 and the tie is deterministic (asserts tie-break per [1.8](#1.8)).  
2. <a name="5.2"></a>`HistoryViewModel` tests SHALL assert that `cacheHistoricalDays` upserts overwrite `dailyUsage`, `socLow`, `socLowTime`, and `peakPeriods` on a row that was previously cached with those fields nil (no log emitted), AND clear them on a row whose previously-cached values are now nil in the response (warning log emitted, content asserted to include the date and field name per [4.2](#4.2)).  
3. <a name="5.3"></a>`HistoryViewModel` tests SHALL assert that the offline fallback path returns rows whose `dailyUsage` round-trips through `CachedDayEnergy.asDayEnergy` with field equality on the blocks list.  
4. <a name="5.4"></a>`HistoryDailyUsageCard` view tests (snapshot or rendered-tree assertions, per project convention) SHALL cover: legend ordering matches [1.5](#1.5); the colour mapping in [1.4](#1.4) is exercised against a fixed palette source; today's bar renders at 50% opacity per [1.6](#1.6); placeholder copy per [1.9](#1.9) when no day-with-blocks is present and when the only day-with-blocks is today; the largest-block subtitle line matches [1.8](#1.8) for a known fixture; the accessibility tree exposes one element per day per [1.12](#1.12).  
5. <a name="5.5"></a>The 30-day chart density behaviours in [2.2](#2.2), [2.3](#2.3), and [2.4](#2.4) SHALL be exercised manually on a real iPhone before merge: toggle 7 → 30 → 7 ranges five times, confirm no stutter or crash, confirm bar widths and x-axis label thinning. The PR description SHALL include a one-line note recording the device model and iOS version the toggle test ran on.  
