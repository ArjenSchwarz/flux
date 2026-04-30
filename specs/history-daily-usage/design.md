# Design: History Daily Usage

## Overview

Add a SwiftUI Charts stacked-bar card to the History screen that consumes the per-day `dailyUsage` already on every `DayEnergy` row. Extends `HistoryViewModel.DerivedState` with one series and a per-kind aggregate; fixes the cache upsert path so the four derived fields backfill correctly with observability on unexpected nil-overwrites.

## Architecture

### Files added

| Path | Role |
|---|---|
| `Flux/Flux/History/HistoryDailyUsageCard.swift` | The card view, mirroring `HistoryGridUsageCard` shape |
| `Flux/Flux/History/DailyUsageBlockKindStyling.swift` | Per-`Kind` extension exposing `chronologicalOrder`, `chartColor`, `displayLabel` — single source of truth for AC 1.3 / 1.4 / 1.5 |
| `Flux/Flux/History/HistoryCacheLog.swift` | Static `Logger(subsystem: "eu.arjen.flux", category: "history-cache")` with an injectable warn-callback shim for tests |

### Files modified

| Path | Change |
|---|---|
| `Flux/Flux/History/HistoryView.swift` | Insert `HistoryDailyUsageCard` after `HistoryBatteryCard` |
| `Flux/Flux/History/HistoryViewModel.swift` | Add `DailyUsageEntry`, extend `DerivedState`/`PeriodSummary`/`Totals`; fix `cacheHistoricalDays` upsert; inject warn-callback |
| `Flux/Flux/DayDetail/DailyUsageCard.swift` | Replace private `label(for:)` with `block.kind.displayLabel` so labels stay aligned (AC 1.5 mandates the same text — extracting to a shared extension prevents future drift) |
| `Flux/Flux/Services/MockFluxAPIClient.swift` | `historyDays` factory: emit `dailyUsage` on each row so previews render the card |
| `docs/agent-notes/ios-app-views.md` | Note the new card |
| `docs/agent-notes/ios-app-viewmodels.md` | Refresh stale `HistoryViewModel` entry to match the actual type shape |

### Pattern extension audit

The new card extends the "History card backed by a per-day series on `DerivedState`" pattern. Audit of existing call sites:

| Site | Needs equivalent | Notes |
|---|---|---|
| `HistoryView.swift` cards stack | yes | Insert between `HistoryBatteryCard` and the per-day summary |
| `DerivedState.init(days:now:)` | yes | Build `dailyUsage` series in same single-pass loop |
| `Totals` struct | yes | Track per-`Kind` sums + complete-day stack-total sum/count |
| `PeriodSummary` snapshot | yes | Surface average daily kWh, day count, largest kind |
| Convenience accessors (`solarSeries`, etc.) | optional | Add `dailyUsageSeries` for symmetry with the existing test/preview accessors |
| `cacheHistoricalDays` upsert | yes | The bug fix lives here — extends the four already-on-`CachedDayEnergy` fields |
| `loadCachedDays` fallback | no | Uses `CachedDayEnergy.asDayEnergy` which already round-trips `dailyUsage` |
| `MockFluxAPIClient.historyDays` | yes | Otherwise previews fall through to the empty placeholder |
| Test fixtures | yes | New table cases per AC 5.1 |

### `HistoryView` integration

Insert the new card between the existing `HistoryBatteryCard` and the per-day summary card, with the `derived` series wired through:

```swift
HistoryBatteryCard(entries: derived.battery, summary: derived.summary,
                   selectedDate: selectedDate, onSelect: selectDay)

HistoryDailyUsageCard(entries: derived.dailyUsage, summary: derived.summary,
                      selectedDate: selectedDate, onSelect: selectDay)

if let selectedDay = viewModel.selectedDay {
    summaryCard(for: selectedDay)
}
```

Same `selectedDate` and `selectDay(_:)` plumbing the other three cards already use — no `HistoryView` API additions.

### Selection API decision

AC 1.10 requires column-level selection with the same affordance the other three cards use. Those use `historySelectionOverlay` (`chartOverlay` + `DragGesture` + `proxy.value(atX:)`). The iOS 26 alternative `chartXSelection(value:)` would be cleaner but applying it only to the new card breaks parity. **Reuse `historySelectionOverlay` unchanged.** A future migration would cover all four cards together; that work is out of scope for T-1022.

## Components and Interfaces

### `DailyUsageBlock.Kind` extension (Flux app target)

```swift
extension DailyUsageBlock.Kind {
    static let chronologicalOrder: [DailyUsageBlock.Kind] =
        [.night, .morningPeak, .offPeak, .afternoonPeak, .evening]

    var chronologicalIndex: Int   // 0…4, drives sort + tie-break
    var chartColor: Color          // pinned per Decision 5
    var displayLabel: String       // shared with Day Detail
}
```

Lives in the iOS app target (not FluxCore) because `Color` is a SwiftUI symbol.

### `HistoryViewModel.DailyUsageEntry`

```swift
struct DailyUsageEntry: Identifiable, Equatable {
    let date: Date
    let dayID: String
    let blocks: [Block]              // sorted into chronologicalOrder
    let stackedTotalKwh: Double      // sum of clamped block kWh
    let isToday: Bool
    var id: String { dayID }

    struct Block: Equatable {
        let kind: DailyUsageBlock.Kind
        let totalKwh: Double          // clamped to ≥ 0 per AC 1.13
    }
}
```

`Block` is intentionally narrower than `FluxCore.DailyUsageBlock` — the card never needs `start` / `end` / `status` / `boundarySource`, so the entry structure stays focused on what the chart renders.

### `HistoryViewModel.DerivedState` additions

```swift
struct DerivedState {
    let solar: [SolarEntry]
    let grid: [GridEntry]
    let battery: [BatteryEntry]
    let dailyUsage: [DailyUsageEntry]   // new
    let summary: PeriodSummary
}
```

Built inside the existing `init(days:now:)` single-pass loop. Each iteration appends to whichever series the day qualifies for. `dailyUsage` is appended only when the day is a day-with-blocks (filtering per AC 3.2).

### `HistoryViewModel.PeriodSummary` additions

```swift
struct PeriodSummary: Equatable {
    // ...existing fields...
    let dailyUsageTotalKwh: Double          // sum of stacked totals across complete days
    let dailyUsageDayCount: Int             // complete days-with-blocks
    let dailyUsageLargestKind: DailyUsageBlock.Kind?  // nil when count == 0
}

extension PeriodSummary {
    var dailyUsageAvgKwh: Double? {
        dailyUsageDayCount > 0 ? dailyUsageTotalKwh / Double(dailyUsageDayCount) : nil
    }
}
```

`Totals` extension tracks `[Kind: Double]` per-kind sums; `largestKind` is determined at snapshot time using `Kind.chronologicalIndex` for tie-break (AC 1.8 / 5.1.j). The production tie-break treats per-kind sums whose absolute difference is below 0.01 kWh as ties, then breaks by ascending `chronologicalIndex`:

```swift
if (sumA - sumB).magnitude < 0.01 {
    // tie at the AC's stated 0.01 kWh precision — chronological order wins
} else if sumA > sumB {
    // strict winner
}
```

Tolerance-band rather than rounding-then-equality gives a symmetric contract and avoids mid-bucket surprises (one sum at 1.2049, another at 1.2051 — both round to 1.20 but compare unequal under naive rounding). The 0.01 band matches AC 1.8's stated precision directly, so the AC and the code agree without arithmetic translation.

### `HistoryDailyUsageCard`

```swift
struct HistoryDailyUsageCard: View {
    let entries: [HistoryViewModel.DailyUsageEntry]
    let summary: HistoryViewModel.PeriodSummary
    let selectedDate: Date?
    let onSelect: (String) -> Void
}
```

Body shape mirrors `HistoryGridUsageCard`:

- `HistoryCardChrome(title: "Daily usage", kpi: kpi, subtitle: subtitle) { … }`
- Branch condition: `summary.dailyUsageDayCount == 0` returns the placeholder per AC 1.9 (same treatment as `HistoryGridUsageCard.placeholder`). This catches both the all-nil case and the today-only case in one check, since today is excluded from the count.
- Otherwise the chart: `Chart { … }.frame(minHeight: 180)`.
- Selection highlight: `if let selectedDate { RuleMark(x: .value("Day", selectedDate)).foregroundStyle(.gray.opacity(0.18)).lineStyle(StrokeStyle(lineWidth: 12)) }` — copied from `HistoryGridUsageCard`. The y=0 baseline rule the Grid card uses is omitted here because daily-usage values are non-negative (clamped per AC 1.13), so a zero baseline adds visual noise without information.
- For each entry, emit one `BarMark` per block in `chronologicalOrder`, stacked at the same `x` (a `Date`). SwiftUI Charts auto-stacks `BarMark`s sharing an x value when `.foregroundStyle(by:)` is keyed.
- `chartForegroundStyleScale(domain:range:)` (the explicit array overload, NOT the `[key: value]` dictionary literal) is used so legend order is deterministic — domain is `Kind.chronologicalOrder.map(\.displayLabel)`, range is `Kind.chronologicalOrder.map(\.chartColor)`. The dictionary-literal overload elsewhere in the codebase relies on Charts' incidental ordering and is not guaranteed.
- Today bar opacity via `.opacity(entry.isToday ? 0.5 : 1.0)` per existing pattern.
- `.historySelectionOverlay(entries: entries.map { ($0.dayID, $0.date) }, onSelect: onSelect)` — same overlay file, no changes needed. The overlay's `proxy.value(atX:)` returns `Date?` because the chart's x is a `Date` plottable; stacking-by-kind on the y-axis does not change that.

### KPI / subtitle copy

- `kpi`: `summary.dailyUsageAvgKwh.map { HistoryFormatters.kwh($0) } ?? "—"`.
- `subtitle`: `summary.dailyUsageLargestKind` formatted as `"\(kind.displayLabel) largest at \(kwh) kWh/day average"` with kWh from `HistoryFormatters.kwh` of `(largest-kind sum) / dailyUsageDayCount`. Plain text — no quotes, no backticks (AC 1.8).
- Placeholder branch hides KPI/subtitle (AC 1.9): the card chrome's KPI parameter is fixed-required, so pass `"—"` for KPI and `nil` for subtitle, matching `HistoryGridUsageCard` placeholder behaviour.

### Animation

`HistoryView` doesn't pass the selected range down explicitly; the chart re-renders because `viewModel.days` changes. Primary approach:

```swift
.animation(.default, value: entries.count)
```

on the `Chart` (not on individual marks). `entries.count` is always 7/14/30 — it changes exactly when the range changes, allocates nothing, and triggers one relayout. No `.animation` modifier is applied to the marks themselves.

If manual verification per [AC 5.5](../requirements.md#5.5) shows visible per-mark stutter (BarMarks fading in/out individually rather than the chart relaying out as one), the fallback is to suppress the data-change transaction's animation and add an explicit single transition:

```swift
.transaction(value: entries.count) { transaction in
    transaction.animation = .default
}
```

Either form satisfies AC 2.1's "no per-mark animation occurs during range change" — the choice depends on which one looks right on device, not which compiles.

### x-axis label thinning

```swift
.chartXAxis {
    AxisMarks(values: .stride(by: .day, count: max(1, entries.count / 6)))
}
```

For a 30-day range that produces 5–6 stride marks; for 7-day it stays at 1. Always shows first/last via `AxisMarks`'s automatic edge handling (AC 2.2).

### Bar width

Default `BarMark` width is automatic and respects the chart's plot width. At iPhone SE 3rd gen (375 pt − ~32 pt chrome inset = 343 pt plot width), 30 columns yield ~11 pt per column before any inter-bar padding. AC 2.3's 6 pt floor is satisfied by default behaviour; no explicit `width:` parameter is needed.

### Accessibility

Default Charts accessibility exposes one element per `BarMark` — at 30 days × 5 segments that's 150 elements. AC 1.12 requires one element per day. Primary pattern:

```swift
.accessibilityElement(children: .ignore)
.accessibilityRepresentation {
    List {
        ForEach(entries) { entry in
            Text(entry.accessibilitySummary)
        }
    }
}
```

`accessibilitySummary` is a derived property on `DailyUsageEntry` returning `"{date}: {stackedTotal} kWh, {largestKindForThisDay} largest"` using `HistoryFormatters.kwh` (AC 1.12 / 1.13). The day-level "largest" reported here is the largest kind *for that day*, not the period-level largest in the subtitle — VoiceOver users navigating day-by-day want each day's own breakdown.

The combination of `accessibilityElement(children: .ignore)` (suppresses the chart's own a11y tree) plus `accessibilityRepresentation { List }` (provides a replacement) is the documented SwiftUI pattern for replacing one accessibility element with another. **It must be verified on a real device with VoiceOver enabled before merge** that the replacement happens (one element per day) rather than being merged with the per-mark elements (151 elements). Verification rolls into AC 5.5's pre-merge manual pass.

If the verification fails (Charts' internal a11y tree is not suppressed by the modifier), the fallback is to wrap the chart and a parallel-laid-out hidden `List` in a `ZStack`:

```swift
ZStack {
    Chart { … }.accessibilityHidden(true)
    accessibilityList(for: entries).hidden()  // visually hidden, a11y visible
}
```

Where `accessibilityList(for:)` returns a `List` of one element per day. This pattern is bulletproof because `accessibilityHidden(true)` deterministically removes the chart from the a11y tree, but it adds a non-rendering sibling. The primary pattern above is preferred when it works; the fallback exists because Charts' internal a11y behaviour is implementation-defined and may differ across iOS minor versions.

### `cacheHistoricalDays` fix

```swift
for day in dayEnergies where !DateFormatting.isToday(day.date, now: now) {
    if let cached = cachedByDate[day.date] {
        // existing energy-field updates...
        warnIfClearing(cached: cached, day: day)   // new
        cached.dailyUsage = day.dailyUsage         // new
        cached.socLow = day.socLow                 // new
        cached.socLowTime = day.socLowTime         // new
        cached.peakPeriods = day.peakPeriods       // new
    } else {
        // CachedDayEnergy(from:) already covers all four fields
    }
}
```

`warnIfClearing` is a private method that, for each of the four fields, checks `cached.field != nil && day.field == nil` and calls the injected `warn` callback once per (date, fieldName) pair (AC 4.2).

Idempotence: a second `loadHistory` call with the same nil response will find the cached field already nil (cleared by the first pass), so the precondition `cached.field != nil` is false and no warning is emitted. The warn fires only on the transition non-nil → nil, never on the steady state nil → nil.

### `HistoryViewModel` constructor extension

```swift
init(
    apiClient: any FluxAPIClient,
    modelContext: ModelContext,
    nowProvider: @escaping @Sendable () -> Date = { .now },
    warn: @escaping @Sendable (String) -> Void = HistoryCacheLog.defaultWarn
)
```

`HistoryCacheLog.defaultWarn` wraps `Logger(subsystem: "eu.arjen.flux", category: "history-cache").warning("\($0, privacy: .public)")`. Tests inject a closure that appends to an `[String]` for assertion.

## Data Models

`CachedDayEnergy` already declares `dailyUsage`, `socLow`, `socLowTime`, `peakPeriods` (added by `daily-derived-stats`). No SwiftData migration is required.

`HistoryViewModel.DailyUsageEntry` is a new struct (sketch above). `DailyUsageEntry.Block` is an internal nested type.

## Error Handling

The only new failure surface is the cache warn-log path. Behaviour:

| Condition | Action |
|---|---|
| `cached.field` was nil, `day.field` is nil | No write, no log. |
| `cached.field` was nil, `day.field` is non-nil | Write, no log (normal backfill). |
| `cached.field` was non-nil, `day.field` is non-nil | Write, no log (normal refresh). |
| `cached.field` was non-nil, `day.field` is nil | Warn (one line per cleared field, identifying date and field), then write. |
| Logger initialisation fails | Cannot — `os.Logger` doesn't throw. |
| `warn` callback throws | Callback is non-throwing by signature. |

The warning is informational only — the upsert proceeds either way (per AC 4.1 the backend is the authoritative source).

## Testing Strategy

### Unit tests (Swift Testing, `@MainActor` where the type requires)

- **`DailyUsageBlockKindStylingTests`** — fixed-table assertion that `chronologicalOrder` has length 5, `chronologicalIndex` is 0…4 in order, `chartColor` matches the pinned palette per Kind, `displayLabel` matches Day Detail's text.
- **`HistoryViewModelTests` extensions** — fixtures (a) through (j) per AC 5.1. Each fixture constructs a `HistoryResponse`, calls `viewModel.loadHistory`, then asserts `viewModel.derived.dailyUsage` and `viewModel.derived.summary` match an expected snapshot. Fixture (j) uses integer-half kWh values so the tie is exact in IEEE 754 (AC 5.1.j).
- **Cache upsert tests** — preload a `CachedDayEnergy` with non-nil derived fields, run `loadHistory` with a response that returns nil for those fields, assert (1) the cached row is cleared, (2) the injected `warn` sink received one line per cleared field with the date and field name. The `warn` sink is an `actor WarnSink { var lines: [String] }` exposing an async `record(_:)` and a snapshot `lines()` accessor; the closure passed to `HistoryViewModel.init` calls `Task { await sink.record($0) }`. This pattern compiles under Swift 6 strict concurrency without `nonisolated(unsafe)` on the test side.
- **Cache backfill test** — preload a `CachedDayEnergy` with all derived fields nil, run `loadHistory` returning non-nil, assert the cached row picks up the values and the `warn` sink remained empty.
- **Round-trip test** — assert that a `DayEnergy` decoded from a `/history` JSON fixture, persisted via `cacheHistoricalDays`, then read back via `loadCachedDays` produces a `dailyUsage` with field equality on the blocks list (AC 5.3).

### View tests (`HistoryDailyUsageCardTests`)

Project convention is rendered-tree assertions over snapshot tests (none of the existing History cards has snapshot tests). For this card:

- Legend ordering: there is no public API to inspect a `Chart`'s foreground-style scale at runtime, so the test asserts at the data-model level instead — the `domain` and `range` arrays passed to `chartForegroundStyleScale(domain:range:)` are produced by `Kind.chronologicalOrder.map(\.displayLabel)` and `.map(\.chartColor)`, so a unit test on `Kind.chronologicalOrder` (length, order, per-Kind palette and label) is the falsifiable evidence of AC 1.5 / 1.4 compliance. Visual confirmation rolls into the AC 5.5 manual pass.
- Today opacity: render with one entry where `isToday == true`, assert via the body's emitted `BarMark.opacity(_:)` modifier value (verified by hosting the view and inspecting the rendered tree).
- Placeholder copy: render with `entries: []` and assert the placeholder text matches the constant.
- Today-only placeholder: render with `entries: [todayOnly]` and `summary.dailyUsageDayCount == 0`, assert the same placeholder.
- Subtitle format: render with a fixture whose largest kind is `.evening` and per-day average is 3.4, assert the rendered subtitle string equals `"Evening largest at 3.4 kWh/day average"`.
- Accessibility tree: assert the card's `accessibilityElements` exposes one element per entry (count == entries.count), not 5× that.

### Manual verification (AC 5.5)

PR description must include a one-liner naming device + iOS version that ran the 7→30→7 toggle test five times.

### Property-based testing

Not appropriate for this feature. The new logic is per-day data shaping with discrete enum cases; the input space is small and fully covered by the table cases enumerated in AC 5.1. The clamp invariant (negative kWh → 0) and the sort invariant (any payload order → chronological output) are stated as ACs and exercised by named fixtures (5.1.b, 5.1.i) — generative testing would not raise coverage meaningfully against a 5-element ordered enum.
