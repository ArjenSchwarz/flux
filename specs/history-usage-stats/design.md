# Design: History Usage Stats

## Overview

A new `HistoryStatsOverviewCard` rendering eight stat tiles for the active 7 / 14 / 30-day range, inserted above the four existing chart cards on the History screen. Four tile values reuse existing `PeriodSummary` aggregates; four new aggregates are computed in the same `Totals` single pass that already builds the solar / grid / battery / daily-usage series.

## Architecture

### Files added

| Path | Role |
|---|---|
| `Flux/Flux/History/HistoryStatsOverviewCard.swift` | The card view + private tile sub-views |
| `Flux/Flux/History/HistoryStatsFormatters.swift` | Tile-specific formatters: SoC percent, "MMM d", "Apr 26 at 06:14", inclusive date range covered |

### Files modified

| Path | Change |
|---|---|
| `Flux/Flux/History/HistoryView.swift` | Insert `HistoryStatsOverviewCard` between `NoteRowView` and `HistorySolarCard` (above the existing four chart cards) |
| `Flux/Flux/History/HistoryDerivedState.swift` | Extend `PeriodSummary` with four new aggregates; extend `Totals` accumulator |
| `Flux/Flux/History/HistoryCardChrome.swift` | Make `kpi` parameter optional (drop the always-rendered KPI label when nil) — see Decision 14 |
| `Flux/Flux/Services/MockFluxAPIClient.swift` | Extend `historyDays` so each fixture row carries `offpeakGridImportKwh`, `offpeakGridExportKwh`, `socLow`, and `socLowTime` (existing rows already carry `dailyUsage`). At least one preview fixture must populate every tile (no all-em-dash preview). |
| `docs/agent-notes/ios-app-views.md` | Note the new card |
| `docs/agent-notes/ios-app-viewmodels.md` | Refresh the documented `PeriodSummary` shape with the four new fields |

### Pattern extension audit

| Site | Needs equivalent | Notes |
|---|---|---|
| `HistoryView.swift` cards stack | yes | Insert above `HistorySolarCard` |
| `DerivedState.init(days:now:)` | yes | New aggregates computed in the same pass |
| `Totals` struct | yes | Track running max for usage / solar, running min for SoC, sum + count for night blocks |
| `PeriodSummary.empty` | yes | Defaults for the four new fields |
| `Totals.snapshot` | yes | Wire the four new fields into `PeriodSummary` |
| `HistoryCardChrome` | yes | Optional KPI — call sites of the existing four cards continue to pass a string |
| `MockFluxAPIClient.historyDays` | yes | Today's mock fixtures don't carry `offpeakGridImportKwh`, `socLow`, or `socLowTime` on every row. Add them so previews populate Peak imports / Exported / Lowest SoC tiles; otherwise the preview demonstrates only em-dash rendering and the tap-to-select preview path can't fire. |
| Test fixtures | yes | Per AC 7.1 cases (a)–(n) |

### Existing four cards

`HistorySolarCard`, `HistoryGridUsageCard`, `HistoryBatteryCard`, `HistoryDailyUsageCard` are not modified except where the chrome-KPI optionality forces a constructor adjustment. They continue to pass their current KPI strings; the chrome change is backward-compatible.

## Components and Interfaces

### `HistoryViewModel.PeriodSummary` additions

The existing four fields (`solarTotalKwh`, `exportTotalKwh`, `peakImportTotalKwh`, `dailyUsageTotalKwh`) cover tiles 2.1 / 2.2 / 2.3 / 2.4. Four new fields cover tiles 2.5 – 2.8:

```swift
struct PeriodSummary: Equatable {
    // ...existing fields...

    /// Sum of `night`-block kWh across complete days that contributed a night
    /// block (post-clamp, never negative). Divisor is `nightBlockDayCount`.
    /// Both fields are zero when the cohort is empty; the tile branches on
    /// `nightAvgKwh` (computed) returning nil.
    let nightTotalKwh: Double
    let nightBlockDayCount: Int

    /// Best day-with-blocks (max stacked kWh, ties broken by most recent).
    /// nil when no complete day-with-blocks exists.
    let mostUsageDay: DayKwhRecord?

    /// Best complete day by `epv`. nil when no complete day exists.
    let mostSolarDay: DayKwhRecord?

    /// Day-with-low whose raw `socLow` is the minimum; ties broken by most
    /// recent. nil when no day-with-low exists in the range. Today is
    /// included.
    let lowestSocDay: LowestSocRecord?

    var nightAvgKwh: Double? {
        nightBlockDayCount > 0 ? nightTotalKwh / Double(nightBlockDayCount) : nil
    }
}

struct DayKwhRecord: Equatable {
    let dayID: String        // "YYYY-MM-DD"
    let date: Date           // parsed Sydney midnight, used by chart selection plumbing
    let kwh: Double
}

struct LowestSocRecord: Equatable {
    let dayID: String
    let date: Date
    let soc: Double          // raw Double; rounding lives at render time
    let socLowTime: String?  // "HH:mm:ss" Sydney; nil when payload had no time
}
```

Two record types instead of one because `LowestSocRecord` carries `socLowTime`. Keeping them separate avoids a semi-optional `time: String?` field on the kWh records that is meaningless 2/3 of the time.

The decision to keep the raw `Double` on `LowestSocRecord` (rather than pre-rounding) is load-bearing: AC 2.8's tie-break compares the raw value, so the record type holds the raw value and the formatter rounds for display only.

### `Totals` accumulator additions

```swift
fileprivate struct Totals {
    // ...existing fields...

    var nightTotal: Double = 0
    var nightBlockDayCount: Int = 0

    var mostUsage: DayKwhRecord?
    var mostSolar: DayKwhRecord?
    var lowestSoc: LowestSocRecord?
}
```

#### Tie-break rule (single form, used by all three comparators)

```
beats(candidate, current) ⇔
    (candidate.value strictly beats current.value)
  OR
    (candidate.value == current.value && candidate.date > current.date)
```

Where "strictly beats" is `>` for max-style records (`mostUsage`, `mostSolar`) and `<` for the min (`lowestSoc`). `==` is exact `Double` equality on the raw `socLow` for SoC (per Decision 8) and exact `Double` equality on stacked-kWh / epv for the others. The "later date wins" rule is enforced even though the API returns chronologically ascending `days` — so the comparator is symmetric and tests can reorder fixtures without changing outcomes.

#### `addCompleteDay` extension

The existing `init(days:now:)` already builds a `DailyUsageEntry` (with its own pre-clamped `stackedTotalKwh`) earlier in the same iteration, before the day flows into `addCompleteDay`. The `mostUsage` comparator reads that entry's `stackedTotalKwh` directly — no second clamp pass.

```swift
mutating func addCompleteDay(_ day: DayEnergy, parsedDate: Date,
                             dailyUsageEntry: DailyUsageEntry?) {
    // ...existing mutations (solarTotal, chargeTotal, dischargeTotal,
    // completeDayCount)...

    // Most-solar — every complete day participates (epv is non-optional).
    consider(&mostSolar,
             candidate: DayKwhRecord(dayID: day.date, date: parsedDate, kwh: day.epv),
             prefersLarger: true)

    // Most-usage — only when day-with-blocks. Reuses the entry already built
    // upstream; no re-summation, no re-clamp.
    if let entry = dailyUsageEntry {
        consider(&mostUsage,
                 candidate: DayKwhRecord(dayID: day.date, date: parsedDate,
                                         kwh: entry.stackedTotalKwh),
                 prefersLarger: true)

        // Night — read the post-clamp block straight from the entry.
        if let night = entry.blocks.first(where: { $0.kind == .night }) {
            nightTotal += night.totalKwh   // already clamped at entry construction
            nightBlockDayCount += 1
        }
    }
}

/// Lowest SoC includes today, so it has its own entry point called
/// from the main loop unconditionally (the only aggregate that does).
mutating func considerSocLow(day: DayEnergy, parsedDate: Date) {
    guard let soc = day.socLow, soc.isFinite else { return }
    let candidate = LowestSocRecord(
        dayID: day.date, date: parsedDate, soc: soc, socLowTime: day.socLowTime
    )
    consider(&lowestSoc, candidate: candidate, prefersLarger: false)
}

private mutating func consider<T: DayRecordValue>(
    _ current: inout T?, candidate: T, prefersLarger: Bool
) {
    guard let existing = current else { current = candidate; return }
    let strictlyBeats = prefersLarger
        ? candidate.comparableValue > existing.comparableValue
        : candidate.comparableValue < existing.comparableValue
    let equal = candidate.comparableValue == existing.comparableValue
    if strictlyBeats || (equal && candidate.date > existing.date) {
        current = candidate
    }
}
```

`DayRecordValue` is a small private protocol the two record types adopt to expose `comparableValue: Double` and `date: Date`. Keeps the comparator generic over both `DayKwhRecord` and `LowestSocRecord` without duplicating logic.

The `soc.isFinite` guard is load-bearing: the wire model declares `socLow` as `Double?` and a NaN value would trap on the eventual `Int(_:)` conversion at render time. Guarding at the aggregate boundary means the formatter can assume finiteness.

The `considerSocLow` call is placed in the existing main loop unconditionally (alongside the existing `addCompleteDay` / `addGrid` / `addDailyUsage` calls), since it's the only aggregate that includes today.

### `HistoryViewModel.DerivedState` integration

The existing `init(days:now:)` loop calls `totals.addCompleteDay(day)` (non-today) and `totals.addGrid(entry)` (when grid entry exists) and `totals.addDailyUsage(entry)` (when today is excluded). The new wiring:

1. Build the `DailyUsageEntry` (existing code) **before** the `addCompleteDay` call so the entry's pre-clamped `stackedTotalKwh` and night block are available.
2. Pass the (optional) entry into `addCompleteDay(_:parsedDate:dailyUsageEntry:)`. The signature change is contained to `Totals` — `DerivedState.init` is the only caller.
3. Call `totals.considerSocLow(day:parsedDate:)` once per day (today-included).

`Totals.snapshot` then constructs `PeriodSummary` with the four new fields. `PeriodSummary.empty` gets `nightTotalKwh: 0`, `nightBlockDayCount: 0`, `mostUsageDay: nil`, `mostSolarDay: nil`, `lowestSocDay: nil`.

### `HistoryCardChrome` change

```swift
struct HistoryCardChrome<Content: View>: View {
    let title: String
    let kpi: String?            // was `String`
    let subtitle: String?
    @ViewBuilder let content: () -> Content

    init(title: String, kpi: String? = nil, subtitle: String? = nil,
         @ViewBuilder content: @escaping () -> Content) { ... }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(title).appFont(.subheadline).foregroundStyle(.secondary)
                Spacer()
                if let kpi {
                    Text(kpi).appFont(.headline).monospacedDigit()
                }
            }
            // ...subtitle, content unchanged
        }
        // ...
    }
}
```

`kpi` becomes optional; when nil, only the title renders on the header row. Existing call sites pass a non-nil string and continue to compile (Swift defaulted parameters are caller-side, so call sites that pass `kpi:` positionally don't break). The four existing cards always pass `kpi`, so they continue to render their KPIs.

### `HistoryStatsOverviewCard`

```swift
struct HistoryStatsOverviewCard: View {
    let summary: HistoryViewModel.PeriodSummary
    let entries: [HistoryViewModel.SolarEntry]   // for inclusive date range
    let onSelect: (String) -> Void

    var body: some View {
        HistoryCardChrome(
            title: "Period overview",
            kpi: HistoryStatsFormatters.dateRange(entries: entries),
            subtitle: nil
        ) {
            grid
        }
    }

    private var grid: some View { ... }   // see Layout below
}
```

The card binds the eight tiles in a fixed reading order (2.1, 2.2, …, 2.8). Tile views are private nested types or `@ViewBuilder` factories — the eight tiles are uniform enough that one `StatTile` view + a record-tile variant covers the surface.

#### Tile types

Two private view types:

```swift
private struct StatTile: View {
    let label: String
    let value: String          // either a formatted kWh string or "—"
    var accessibilityValueOverride: String? = nil   // for "kilowatt hours" expansion
}

private struct DayRecordTile: View {
    let label: String
    let value: String          // either a formatted value or "—"
    let dateLine: String?      // "Apr 28" or "Apr 26 at 06:14" — nil when em-dash
    let dayID: String?         // nil when em-dash → non-tappable
    let onSelect: ((String) -> Void)?
    var accessibilityValueOverride: String? = nil
}
```

`DayRecordTile` wraps its content in a `Button` with `.buttonStyle(.plain)` only when `dayID != nil && onSelect != nil`. When em-dash, it renders the same content without a button wrapper, so VoiceOver does not expose the `.isButton` trait per AC 6.3 / 3.2.

#### Tile content layout

Each tile renders:

```
{label}                         ← FluxTheme.Typography.statRowLabel (15 pt regular, .monospacedDigit), .secondaryText
{value}                         ← FluxTheme.Typography.statRowValue (15 pt regular, .monospacedDigit), .primaryText
{dateLine?}                     ← FluxTheme.Typography.statRowSub (10 pt monospaced), .tertiaryText
```

Vertical alignment is top-leading; rows in the grid are forced to uniform height by setting `.frame(maxHeight: .infinity, alignment: .topLeading)` on the tile body and letting the parent grid stretch all rows to the tallest tile in that row (per AC 5.2).

`StatTile` does not render a `dateLine` slot at all; `DayRecordTile` always reserves the slot (using a non-rendering placeholder when the tile is em-dash) so a row mixing a record tile and a stat tile does not have a height mismatch. The placeholder approach uses `Color.clear.frame(height: …)` with a height equal to `statRowSub`'s line metric — implementer to confirm via `.frame(minHeight:)` or a measured value.

### Grid layout

```swift
@Environment(\.horizontalSizeClass) private var hSizeClass

private var columnCount: Int {
    #if os(macOS)
    return 4
    #else
    return hSizeClass == .regular ? 4 : 2
    #endif
}

private var grid: some View {
    LazyVGrid(
        columns: Array(repeating: GridItem(.flexible(), spacing: 12, alignment: .topLeading),
                       count: columnCount),
        alignment: .leading,
        spacing: 12
    ) {
        // 8 tiles, in the order defined by §2.1 – §2.8
    }
    .animation(.default, value: columnCount)   // smooth column reflow on size-class change
}
```

`LazyVGrid` over `Grid`: the eight tiles are not row-aligned across columns the way `Grid` would force, and `LazyVGrid` flows naturally into 4×2 or 2×4 with no per-row markup. `GridItem(.flexible())` lets each column take equal width.

Decision 13 covers the column-count call. **Known limitation** — iPad in 1/3 split view reports `horizontalSizeClass == .regular` while the History detail column is ~320 pt wide. Four columns at that width yields ~68 pt per tile after padding; labels truncate. Accepted as a documented edge case (Flux is not optimised for iPad-narrow split). If a future iteration reveals real users hitting this, the fallback is `ViewThatFits` between a 2-column and 4-column variant — implemented in a follow-up, not this feature.

### KPI line content

Per AC 1.2 the KPI must not duplicate the picker. The card renders the inclusive date range covered (oldest entry → newest entry) as the KPI:

```swift
enum HistoryStatsFormatters {
    static func dateRange(entries: [HistoryViewModel.SolarEntry]) -> String? {
        // Use min/max rather than first/last so a defensively-reversed
        // response (the API contract is ascending, but the view model
        // does not assert this on the wire) still produces the right
        // chronological extremes.
        let dates = entries.map(\.date)
        guard let earliest = dates.min(), let latest = dates.max() else { return nil }
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateFormat = "MMM d"
        let lhs = formatter.string(from: earliest)
        let rhs = formatter.string(from: latest)
        return lhs == rhs ? lhs : "\(lhs) – \(rhs)"
    }
}
```

The Solar series is used because `SolarEntry` is built for every day in the response (no filtering, unlike grid/dailyUsage). When the response is empty the chrome renders without a KPI line (the screen-level empty state hides the whole card per AC 1.4 anyway).

### Tile binding and ordering

Order is fixed (AC 1.3 / 5.1): 2.1 Total usage, 2.2 Total solar, 2.3 Exported, 2.4 Peak imports, 2.5 Avg night, 2.6 Most usage, 2.7 Most solar, 2.8 Lowest SoC.

| # | Tile | Source | Em-dash trigger |
|---|---|---|---|
| 2.1 | Total usage | `HistoryFormatters.kwh(summary.dailyUsageTotalKwh)` | `summary.dailyUsageDayCount == 0` |
| 2.2 | Total solar | `HistoryFormatters.kwh(summary.solarTotalKwh)` | `summary.solarDayCount == 0` |
| 2.3 | Exported | `HistoryFormatters.kwh(summary.exportTotalKwh)` | `summary.gridDayCount == 0` |
| 2.4 | Peak imports | `HistoryFormatters.kwh(summary.peakImportTotalKwh)` | `summary.gridDayCount == 0` |
| 2.5 | Avg night | `HistoryFormatters.kwh(summary.nightAvgKwh!)` | `summary.nightAvgKwh == nil` |
| 2.6 | Most usage | `HistoryFormatters.kwh(record.kwh)` | `summary.mostUsageDay == nil` |
| 2.7 | Most solar | `HistoryFormatters.kwh(record.kwh)` | `summary.mostSolarDay == nil` |
| 2.8 | Lowest SoC | `HistoryStatsFormatters.socPercent(record.soc)` | `summary.lowestSocDay == nil` |

Tile 2.1 / 2.2 / 2.3 / 2.4 use the existing aggregates; the em-dash trigger for 2.4 explicitly checks `gridDayCount == 0` (not `peakImportTotalKwh == 0`) so a day-with-offpeak whose peak imports happen to be zero renders as `0.0 kWh`, not em-dash (AC 2.4 / 7.1.l).

### Formatters

```swift
enum HistoryStatsFormatters {
    /// Half-up rounded to nearest whole percent. NumberFormatter defaults to
    /// banker's rounding on Apple platforms, so this uses a direct rounded()
    /// call which is half-away-from-zero — equivalent to half-up for the
    /// non-negative SoC range (0…100). Caller has already filtered NaN/Inf
    /// at `considerSocLow`, but this guard is a belt-and-braces precondition
    /// so the formatter can be unit-tested in isolation without crashing on
    /// a hand-constructed pathological input.
    static func socPercent(_ soc: Double) -> String {
        guard soc.isFinite else { return "—" }
        return "\(Int(soc.rounded(.toNearestOrAwayFromZero)))%"
    }

    /// "Apr 28" Sydney time.
    static func shortDate(from date: Date) -> String { ... }

    /// "Apr 26 at 06:14" Sydney time. `socLowTime` is a "HH:mm:ss" wire string;
    /// parse the time and re-render at "HH:mm" Sydney.
    static func dateWithTime(from date: Date, time: String?) -> String { ... }

    /// Inclusive date range covered (see KPI section).
    static func dateRange(entries: [HistoryViewModel.SolarEntry]) -> String? { ... }

    /// "kilowatt hours" expansion of a kWh string, used by accessibility labels.
    static func accessibleKwh(_ kwh: Double) -> String { ... }

    /// "12 percent" expansion for Lowest SoC accessibility label.
    static func accessibleSocPercent(_ soc: Double) -> String { ... }
}
```

The two `accessible*` formatters provide the unit-spelled-out form referenced in AC 6.1; tile views compose them into the full label string `"{name}, {accessible value}, {date / time line}"` (§6 below).

### Accessibility label format

Per AC 6.1 / 6.2 / 6.3 — single accessibility element per tile, label combining name + value + date/time line, units spelled out:

| Case | Label |
|---|---|
| Total usage = 98.4 | `"Total usage, 98.4 kilowatt hours"` |
| Most usage = 18.7 on 28 Apr | `"Most usage, 18.7 kilowatt hours, April 28"` |
| Lowest SoC = 12% on 26 Apr at 06:14 | `"Lowest SoC, 12 percent, April 26 at 06:14"` |
| Peak imports — em-dash | `"Peak imports, no data"` |

`accessibilityHint` on tappable record tiles is the literal `"Selects this day in the charts below."` (AC 6.3). `accessibilityAddTraits(.isButton)` is added when the tile is tappable.

The `dateLine` for accessibility uses the long month name (`"April 28"`) even though the visible tile uses `"MMM d"` (`"Apr 28"`) — VoiceOver reads dates better when the month is fully spelled. Consistent with AppKit / UIKit defaults.

Card chrome accessibility (the `"Period overview"` title and the date-range KPI) is delegated to `HistoryCardChrome`'s default `Text` accessibility — no explicit chrome-level a11y modifiers in this card. The `accessibilityLabel(tile:summary:)` static helper is tile-scoped only.

### Tap target plumbing

The card receives `onSelect: (String) -> Void` from `HistoryView`, identical to the four chart cards. Inside the card, only `DayRecordTile` invokes it. The new `HistoryView` integration:

```swift
HistoryStatsOverviewCard(
    summary: derived.summary,
    entries: derived.solar,
    onSelect: selectDay
)

HistorySolarCard(...)   // unchanged
// ...other three cards unchanged...
```

### `HistoryView` placement

Insert above `HistorySolarCard`. The card sits below the existing `NoteRowView` because the note row carries selected-day context that the user wants visible above the period view (per AC 1.1).

```swift
NoteRowView(text: viewModel.selectedDay?.note)

if !viewModel.days.isEmpty {           // existing gate already wraps the cards
    HistoryStatsOverviewCard(...)      // new
    HistorySolarCard(...)
    HistoryGridUsageCard(...)
    HistoryDailyUsageCard(...)
    if let selectedDay = viewModel.selectedDay { summaryCard(for: selectedDay) }
}
```

The existing `viewModel.days.isEmpty` branch (which renders `errorState` / `emptyState`) already gates all four chart cards. The new card joins that branch verbatim — AC 1.4 falls out of the existing structure with no new branch.

## Data Models

No new persistent or wire-format models. `PeriodSummary` is an in-memory derived type; the two new nested record structs (`DayKwhRecord`, `LowestSocRecord`) are also in-memory only.

## Error Handling

No new failure modes. Em-dash placeholder is the response for empty cohorts; cohort emptiness is captured in the `PeriodSummary` field shape (nil-bearing optionals or zero-bearing counts) and never raises an error.

## Testing Strategy

### Unit tests (Swift Testing)

Per AC 7.1, all new fixtures live in `HistoryViewModelTests` (or a focused `HistoryViewModelOverviewTests`).

| Fixture | Asserts |
|---|---|
| (a) empty days array | every new field is nil / zero |
| (b) only-today | complete-day fields nil; `lowestSocDay` populated iff today has `socLow` |
| (b′) today + complete day, both with off-peak records | `peakImportTotalKwh` and `exportTotalKwh` include today's contribution (locks the asymmetry contract from Decision 5) |
| (c) all complete days, full data | every tile populated with hand-computed expectations; assert `summary.mostUsageDay == DayKwhRecord(...)` exactly using the Equatable conformance |
| (d) one complete day-with-blocks, only `dailyUsage` populated | Total usage / Most usage / Avg night non-nil |
| (e) two days tie for `mostUsageDay` | later date wins |
| (f) two days tie for `mostSolarDay` | later date wins |
| (g) two days tie on raw `socLow` Double | later date wins |
| (h) ≥1 complete day-with-blocks lacks night | excluded from `nightBlockDayCount` |
| (h′) one complete day-with-blocks where `night.totalKwh == 0` | day counts toward `nightBlockDayCount` (real zero, not absence); `nightAvgKwh` non-nil and equals 0 in the single-day case |
| (i) negative `night.totalKwh` | clamped to zero in `nightTotal` |
| (j) negative `peakGridImportKwh` payload | clamped to zero (existing rule, asserted for regression) |
| (k) every day lacks `offpeakGridImportKwh` | `gridDayCount == 0`; tiles 2.3 / 2.4 em-dash via the chart cards' existing semantics |
| (l) at least one day-with-offpeak with all peak imports zero | `peakImportTotalKwh == 0`, `gridDayCount > 0` |
| (m) one day has `dailyUsage` but no `offpeakGridImportKwh` | contributes to Total usage / Avg night / Most usage but not Peak imports / Exported |
| (o) `socLow = Double.nan` | `lowestSocDay == nil` (the `isFinite` guard kicks in) |

Fixture (l) explicitly catches the regression where a tile renders em-dash when the right answer is `0.0 kWh`. Fixture (b′) locks the today-asymmetry contract — without it, a future regression that adds `!isToday` to `addGrid` would silently pass tests.

#### Formatter tests (`HistoryStatsFormattersTests`)

Separate test target from the ViewModel tests so `socPercent` is exercised directly without driving a `loadHistory` cycle.

| Fixture | Asserts |
|---|---|
| (n) `socPercent(11.5)` → `"12%"`; `socPercent(11.4)` → `"11%"`; `socPercent(0.5)` → `"1%"`; `socPercent(99.5)` → `"100%"` | half-up rounding boundary |
| (n′) `socPercent(.nan)` → `"—"`; `socPercent(.infinity)` → `"—"` | finiteness guard |
| `dateRange` with chronologically-reversed input | min/max produce the same string as forward input |
| `dateRange` with single-day range | returns just the one date (no en-dash) |

### Tile rendering tests (`HistoryStatsOverviewCardTests`)

Project convention is rendered-tree assertions over snapshots. The card exposes static helpers paralleling `HistoryDailyUsageCard.kpi(for:)` / `subtitle(for:)`:

```swift
extension HistoryStatsOverviewCard {
    static func valueText(for tile: TileKey, summary: PeriodSummary) -> String  // returns "—" or formatted
    static func dateLineText(for tile: TileKey, summary: PeriodSummary) -> String?
    static func isTappable(tile: TileKey, summary: PeriodSummary) -> Bool
    static func accessibilityLabel(tile: TileKey, summary: PeriodSummary) -> String
}
```

Unit tests assert these helpers against fixtures, so per AC 7.2 we cover labels, em-dash treatment, date-line format, tap-target invariant, and accessibility labels without running a SwiftUI layout pass.

### Tap-action test

Per AC 7.3, a focused test invokes the action closure passed to `DayRecordTile` directly (not via a synthesised gesture) and asserts that the `onSelect` callback fires with the expected `dayID`. The test set includes a fixture where the Lowest SoC record is today, asserting today-tap behaviour described in AC 3.1.

### Preview

Per AC 7.4, `#Preview { ... }` block with a fixture mixing populated and em-dash tiles, gated under `#if DEBUG`. Two preview entry points: one for iPhone (`.previewLayout(.fixed(width: 375, height: 600))`) and one for Mac wide window (`.previewLayout(.fixed(width: 900, height: 600))`).

### Property-based testing

Skipped. The new logic is straightforward aggregation with discrete tie-break and clamp rules, both of which are covered by named example fixtures (e–g for ties, i / j for clamps). The input space is small and the invariants are stated as ACs that translate 1:1 to fixtures.

### Manual verification

Run the app on iPhone and Mac with mocked data covering the eight tiles. Confirm:
- Tile order matches the requirements
- VoiceOver reads each tile as one element with the expected label
- Tapping a record tile selects the day in the four chart cards (highlight rectangle moves)
- Switching the range picker produces stable layout (no tile reorder, em-dash tiles stay in place)

No PR-description device-and-iOS-version note is required (the underlying SwiftUI behaviour is well-trodden — the `HistoryDailyUsageCard` PR introduced the 30-day stress test that this card does not need).
