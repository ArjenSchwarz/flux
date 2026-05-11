# Design: Stat Comparisons

## Overview

Add an opt-in Compare control to Day Detail that fetches the resolved comparison day via the existing `/day` endpoint and renders a sub-line under each supported value showing the signed kWh delta. Backend is unchanged; the work is all client-side in `Flux/Flux/`.

## Architecture

### Data path

The comparison day for both periods (Yesterday, 7 days ago) is at most one calendar day before the selected day, so it is reachable via a second call to `apiClient.fetchDay(date:)`. Past-date `/day` reads from `flux-daily-energy` without a readings query (`internal/api/day.go:115-146`), so the request is cheap.

`/day` always returns HTTP 200 for a syntactically-valid date (`internal/api/day.go:148-157`); a date with no record yields a body whose `summary`, `dailyUsage`, and `peakPeriods` are all nil/empty. We use that shape — not an HTTP status — to detect "no comparison data".

We deliberately do not extend `/history` (anchored to `now`, no end-date parameter — `internal/api/history.go:18-32`) and do not add a `/day?compare=` server-side aggregation. Both would be backend wins for a UI feature whose data already exists in the response we'd otherwise duplicate. The inflection point that would justify a backend endpoint is "compare to N-day average" (logged in Decision 17), which v1 doesn't ship.

### Trigger matrix

| Event | Action |
|-------|--------|
| Compare toggle off → on | Cancel any in-flight comparison task; fetch comparison `/day` for resolved date |
| Period chip changes | Cancel; fetch comparison `/day` for newly resolved date |
| Day-navigation (prev / next) while Compare on | Cancel; fetch comparison `/day` for new selected day's offset |
| Compare on → off | Cancel; clear snapshot; `comparisonState = .off` |
| Selected day's `loadDay()` succeeds while Compare on | No automatic re-fetch (comparison is independent of the selected-day refresh) |
| Selected day is Today; Day Detail does not auto-refresh on a timer | n/a — confirmed by inspection of `DayDetailView` (no timer-driven `loadDay`) |

Comparison fetches are independent tasks owned by `DayDetailViewModel`; a comparison-day failure never blocks the primary day's render. Rapid period-chip / day-nav events rely on cancel-and-restart rather than debounce — past-date `/day` is cheap and `Task.isCancelled` checks (below) prevent stale writes.

### Module placement

| File | Purpose |
|------|---------|
| `Flux/Flux/DayDetail/Compare/ComparePeriod.swift` | Period enum + `dayOffset` + display name + `init?(rawValue:)` fallback |
| `Flux/Flux/DayDetail/Compare/ComparisonSnapshot.swift` | Value type holding the comparison day's energy fields, derived `houseUsed` / `peakGridImport`, and `dailyUsage` |
| `Flux/Flux/DayDetail/Compare/ComparisonState.swift` | Loading/ready/unavailable enum + `isUnavailable` |
| `Flux/Flux/DayDetail/Compare/CompareControl.swift` | Toggle + period chip + failure caption (caption is part of this view) |
| `Flux/Flux/DayDetail/Compare/DeltaFormatter.swift` | Pure functions producing the sub-line string |
| `Flux/Flux/DayDetail/Compare/ValueSubline.swift` | Shared sub-line view used by `FluxStatRow` and `DayInFiveBlocksPanel` |
| `Flux/Flux/DayDetail/Compare/SublineContent.swift` | The three-state enum used to drive the sub-line slot |

Everything Compare-specific lives under `DayDetail/Compare/`. `ValueSubline` is here even though `FluxStatRow` consumes it, so the dependency direction (FluxStatRow → Compare type) is visible from the file tree.

Modifications:

- `FluxStatRow` (`Flux/Flux/Helpers/FluxV5Components.swift`): add `valueSub: SublineContent = .hidden` and `accessibilityOverride: String? = nil` parameters.
- `SummaryBlock` (`Flux/Flux/Helpers/SummaryBlock.swift`): accept `compare: ComparisonState = .off`; feed each row's `valueSub` and `accessibilityOverride`.
- `DayInFiveBlocksPanel` (`Flux/Flux/DayDetail/DayInFiveBlocksPanel.swift`): accept `compare: ComparisonState = .off`; place sub-lines under the solar and total columns of each row.
- `DayDetailViewModel` (`Flux/Flux/DayDetail/DayDetailViewModel.swift`): add `comparisonState` and the comparison-fetch task.
- `DayDetailView` (`Flux/Flux/DayDetail/DayDetailView.swift`): host `CompareControl` directly above the existing `if let dailyUsage` block, declare two `@AppStorage` properties, wire three `.onChange` reactions.

### Pattern-extension audit

`FluxStatRow` is the row primitive for all stat panels. Only Day Detail's SummaryBlock and the (hand-rolled) DayInFiveBlocksPanel need sub-line wiring; every other callsite must continue to render exactly as today.

| Callsite | Needs sub-line wiring | Notes |
|----------|----------------------|-------|
| `BatteryBlock` rows | No | BatteryBlock is excluded from `SR` per requirements |
| `OffPeakBlock` rows | No | Off-peak panel is not in `SC` |
| Dashboard `SummaryBlock` ("Today so far") | No | Dashboard is not in `SC`; `compare` defaults to `.off` |
| Day Detail `SummaryBlock` ("Power") | Yes | Pass `compare` from view |
| `DayInFiveBlocksPanel` rows (hand-rolled) | Yes | Modify the row builder directly |
| `FluxV5Components` preview rows | No | Preview only |

The `compare = .off` default means the 19 existing `FluxStatRow` callsites and the Dashboard / OffPeakBlock / BatteryBlock callers are unchanged at the source level.

### Off-state byte-identity scope

Decision 16's "off-state byte-identical to pre-feature" claim applies to the contents of `SummaryBlock` and `DayInFiveBlocksPanel` (no reserved sub-line slot, no behaviour change). The Day Detail screen as a whole is *not* byte-identical to pre-feature — `CompareControl` is hosted unconditionally per AC 1.1 (the toggle is always visible; only the period chip and caption are gated on the toggle). Tests target the row/card identity, not the screen.

## Components and Interfaces

```swift
// Compare/ComparePeriod.swift
enum ComparePeriod: String, CaseIterable, Identifiable, Sendable {
    case yesterday
    case sevenDaysAgo

    var id: String { rawValue }
    var dayOffset: Int { self == .yesterday ? -1 : -7 }
    var displayName: String { self == .yesterday ? "Yesterday" : "7 days ago" }

    /// Used by `@AppStorage`'s rawValue codec when a future build introduced
    /// new cases that the current build doesn't know about. Returning the
    /// default keeps the toggle/chip operable instead of crashing.
    static func parseOrDefault(_ raw: String?) -> ComparePeriod {
        ComparePeriod(rawValue: raw ?? "") ?? .yesterday
    }
}

// Compare/ComparisonSnapshot.swift
struct ComparisonSnapshot: Sendable, Equatable {
    let date: String              // YYYY-MM-DD
    let solar: Double?            // epv
    let gridImport: Double?       // eInput
    let gridExport: Double?       // eOutput
    let batteryCharge: Double?    // eCharge
    let batteryDischarge: Double? // eDischarge
    let offpeakGridImport: Double?
    let dailyUsage: DailyUsage?

    var houseUsed: Double? { /* HouseholdLoad.kwh(...) */ }
    /// Production data always carries the off-peak split (Decision 10), but
    /// the wire type is optional, so the guard is defensive: when the field
    /// is unexpectedly nil this returns nil and the per-row fallback handles
    /// the Grid in (peak) row. Real production responses never hit that path.
    var peakGridImport: Double? { /* max(0, gridImport - offpeakGridImport); nil when either input is nil */ }

    /// Returns `nil` when the response carries neither a `summary` nor a
    /// `dailyUsage`. A response with a present-but-all-nil-`SR`-fields summary
    /// stays `.ready`; per-row fallback handles it via `DeltaFormatter`. A
    /// response with summary present but `dailyUsage` nil also stays `.ready`
    /// — `SummaryBlock` rows render their deltas while every `Five-Block` row
    /// independently resolves to `.reserved` via the per-block fallback in
    /// `solarValueSub` / `totalValueSub` (a "partial-availability" success).
    static func from(date: String, response: DayDetailResponse) -> ComparisonSnapshot?
}

// Compare/ComparisonState.swift
enum ComparisonState: Sendable, Equatable {
    case off
    case loading(date: String)
    case ready(ComparisonSnapshot, period: ComparePeriod)
    case unavailable(period: ComparePeriod)

    var isUnavailable: Bool { if case .unavailable = self { true } else { false } }
}

// The failure caption sources its period text from the live `@AppStorage`
// `comparePeriod` binding (via `CompareControl`'s `period` parameter), not
// from `ComparisonState`. This way the caption always reflects the chip's
// current value during rapid period switches, instead of trailing the last
// resolved fetch.

// Compare/SublineContent.swift
enum SublineContent: Equatable {
    case hidden                 // no slot rendered; row at pre-feature height
    case reserved               // slot rendered, no glyph (loading / fallback)
    case text(String)           // slot rendered with the formatted delta string
}

// Compare/DeltaFormatter.swift
enum DeltaFormatter {
    /// Returns `.text("▲ 1.2 kWh")` / `.text("▼ 0.4 kWh")` / `.text("— kWh")`
    /// when both inputs are present, `.reserved` when comparison is nil,
    /// or `.reserved` when current is nil. Callers translate `compare == .off`
    /// to `.hidden` themselves; this function never returns `.hidden`.
    static func sublineContent(current: Double?, comparison: Double?) -> SublineContent

    /// Composes "{rowLabel}{labelSubClause}: {primaryValue}, {dir} {abs} {unit} versus {period}".
    /// `labelSubClause` is `, paid` / `, free` / "" — used so existing FluxStatRow
    /// `sub` slot text survives the accessibility combine.
    static func voiceOverLabel(rowLabel: String,
                               labelSub: String?,
                               primaryValue: String,
                               current: Double?,
                               comparison: Double?,
                               period: ComparePeriod) -> String

    /// Used in `.reserved` and `.off` states; omits the comparison clause.
    static func voiceOverFallbackLabel(rowLabel: String,
                                       labelSub: String?,
                                       primaryValue: String) -> String
}

// Compare/ValueSubline.swift
struct ValueSubline: View {
    let content: SublineContent

    var body: some View {
        switch content {
        case .hidden:
            EmptyView()
        case .reserved:
            // U+00A0 (non-breaking space) keeps the line height while
            // rendering nothing visible.
            Text("\u{00A0}")
                .appFont(FluxTheme.Typography.touTime)
                .foregroundStyle(FluxTheme.Palette.tertiaryText)
                .accessibilityHidden(true)
        case .text(let s):
            Text(s)
                .appFont(FluxTheme.Typography.touTime)
                .foregroundStyle(FluxTheme.Palette.tertiaryText)
                .accessibilityHidden(true)
        }
    }
}

// Compare/CompareControl.swift
struct CompareControl: View {
    @Binding var enabled: Bool
    @Binding var period: ComparePeriod
    let unavailable: Bool        // drives the failure caption visibility

    // Layout: VStack(alignment: .leading, spacing: 4) {
    //     HStack { Toggle("Compare", isOn: $enabled); if enabled { PeriodChip }; Spacer() }
    //     if enabled && unavailable {
    //         Text("No comparison data available for \(period.displayName)")
    //             .appFont(FluxTheme.Typography.touTime)
    //             .foregroundStyle(FluxTheme.Palette.tertiaryText)
    //     }
    // }
    //
    // The caption is rendered inside CompareControl directly below the chip's
    // leading edge, satisfying AC 5.5 ("directly below the period chip,
    // leading-aligned to the chip"). No external sibling caption.
}
```

### `FluxStatRow` modifications

```swift
struct FluxStatRow: View {
    let label: String
    let value: String
    var sub: String? = nil                          // existing label-side sub ("paid"/"free")
    var accent: Color? = nil
    var last: Bool = false
    var valueSub: SublineContent = .hidden          // NEW
    var accessibilityOverride: String? = nil        // NEW

    var body: some View {
        VStack(spacing: 0) {
            VStack(alignment: .trailing, spacing: 2) {
                HStack(alignment: .firstTextBaseline) {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(label)
                            .appFont(FluxTheme.Typography.statRowLabel)
                            .foregroundStyle(FluxTheme.Palette.secondaryText)
                        if let sub {
                            Text(sub)
                                .appFont(FluxTheme.Typography.statRowSub)
                                .foregroundStyle(FluxTheme.Palette.tertiaryText)
                        }
                    }
                    Spacer()
                    Text(value)
                        .appFont(FluxTheme.Typography.statRowValue)
                        .foregroundStyle(accent ?? FluxTheme.Palette.primaryText)
                }
                ValueSubline(content: valueSub)
            }
            .padding(.vertical, FluxTheme.Metrics.statRowVerticalPadding)
            .modifier(RowAccessibilityModifier(label: accessibilityOverride))

            if !last {
                Rectangle()
                    .fill(FluxTheme.Palette.border)
                    .frame(height: FluxTheme.Metrics.hairline)
            }
        }
    }
}

private struct RowAccessibilityModifier: ViewModifier {
    let label: String?
    func body(content: Content) -> some View {
        if let label {
            content
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(label)
        } else {
            content
        }
    }
}
```

Contract notes that aren't obvious from the signature:

- `valueSub == .hidden` produces `EmptyView()` from `ValueSubline`, so the inner `VStack(spacing: 2)` has only one child and the row's measured height equals the pre-feature row body wrapped in a 1-child VStack with 2pt spacing. The test (below) asserts byte-identity by comparing `compare = .off` against pre-feature bytes; the 2pt VStack overhead with one child is structurally identical to the original `HStack` because SwiftUI collapses unused stack spacing.
- `accessibilityOverride` is the *only* way the row participates in a combined VoiceOver label. When nil, the row keeps the pre-feature behaviour (label / sub / value read separately by VoiceOver). When non-nil, the entire row reads as one element with the override string. Sub-line `Text` views are always `.accessibilityHidden(true)` so the chevron glyph never leaks into VoiceOver.

### `DayInFiveBlocksPanel` modifications

```swift
struct DayInFiveBlocksPanel: View {
    let dailyUsage: DailyUsage
    var compare: ComparisonState = .off          // NEW

    @ViewBuilder
    private func row(_ block: DailyUsageBlock, isLast: Bool) -> some View {
        // ... existing layout unchanged through the HStack with two value columns ...
        VStack(alignment: .trailing, spacing: 2) {
            // Solar value column — only on daylight rows, only when block.solarKwh != nil
            if isDaylight(block.kind), let solar = block.solarKwh {
                Text(EnergyFormatting.format(solar))
                    .appFont(FluxTheme.Typography.touValue)
                    .foregroundStyle(FluxTheme.Palette.amber)
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
                    .frame(width: 76, alignment: .trailing)
                ValueSubline(content: solarValueSub(for: block))
                    .frame(width: 76, alignment: .trailing)
            }
        }
        // and a parallel VStack(alignment: .trailing, spacing: 2) for the total-kWh column.
    }

    private func solarValueSub(for block: DailyUsageBlock) -> SublineContent {
        switch compare {
        case .off:
            return .hidden
        case .loading, .unavailable:
            return .reserved
        case .ready(let snapshot, _):
            let comparisonBlock = snapshot.dailyUsage?.blocks.first { $0.kind == block.kind }
            return DeltaFormatter.sublineContent(current: block.solarKwh,
                                                  comparison: comparisonBlock?.solarKwh)
        }
    }

    private func totalValueSub(for block: DailyUsageBlock) -> SublineContent {
        switch compare {
        case .off: return .hidden
        case .loading, .unavailable: return .reserved
        case .ready(let snapshot, _):
            let comparisonBlock = snapshot.dailyUsage?.blocks.first { $0.kind == block.kind }
            return DeltaFormatter.sublineContent(current: block.totalKwh,
                                                  comparison: comparisonBlock?.totalKwh)
        }
    }
}
```

The existing `HStack` already has two right-anchored value columns (76pt wide). Wrapping each in a `VStack(alignment: .trailing, spacing: 2)` with `ValueSubline` beneath preserves the column geometry. AC 4.1's "two independent sub-lines" is satisfied because each `ValueSubline` resolves its content from the snapshot independently (one can be `.text(...)`, the other `.reserved`).

Five-block rows do not consume `accessibilityOverride` because `DayInFiveBlocksPanel` is hand-rolled — its rows already render four `Text` views per row (label, time-range, solar, total). VoiceOver grouping mirrors `FluxStatRow`: when `compare ≠ .off`, the row applies `.accessibilityElement(children: .ignore)` and an explicit `.accessibilityLabel` composed by `DeltaFormatter.voiceOverLabel` (or `voiceOverFallbackLabel` for `.reserved` slots). When `compare = .off`, the row keeps its existing per-`Text` VoiceOver behaviour — no modifier chain change.

### `SummaryBlock` per-row mapping

The override mapping is sketched here so the implementation surface for each `SR` row is unambiguous:

```swift
private func valueSub(current: Double?, comparison: Double?) -> SublineContent {
    switch compare {
    case .off:                       return .hidden
    case .loading, .unavailable:     return .reserved
    case .ready(let snapshot, _):    return DeltaFormatter.sublineContent(current: current,
                                                                          comparison: snapshot.value(for: row))
    }
}

private func accessibilityOverride(rowLabel: String,
                                   labelSub: String?,
                                   primary: String,
                                   current: Double?,
                                   comparison: Double?) -> String? {
    switch compare {
    case .off:                       return nil
    case .loading, .unavailable:     return DeltaFormatter.voiceOverFallbackLabel(rowLabel: rowLabel,
                                                                                  labelSub: labelSub,
                                                                                  primaryValue: primary)
    case .ready(_, let period):      return DeltaFormatter.voiceOverLabel(rowLabel: rowLabel,
                                                                          labelSub: labelSub,
                                                                          primaryValue: primary,
                                                                          current: current,
                                                                          comparison: comparison,
                                                                          period: period)
    }
}
```

Each `FluxStatRow` callsite in `SummaryBlock` passes `valueSub:` and `accessibilityOverride:` produced by these helpers. The `labelSub` argument is the existing `sub:` parameter on Grid In rows (`"paid"` / `"free"`) so the combined VoiceOver label preserves the qualifier — e.g. "Grid in (peak), paid: 4.2 kilowatt-hours, up 0.6 kilowatt-hours versus yesterday".

### `DayDetailViewModel` additions

```swift
@MainActor @Observable
final class DayDetailViewModel {
    private(set) var comparisonState: ComparisonState = .off
    private var comparisonTask: Task<Void, Never>?

    // `date`, `apiClient`, and the existing `loadDay()` machinery are all
    // pre-feature view-model state; only the comparison fields below are new.
    func updateCompare(enabled: Bool, period: ComparePeriod) {
        comparisonTask?.cancel()
        guard enabled else {
            comparisonState = .off
            return
        }
        guard let target = resolveCompareDate(period: period) else {
            comparisonState = .unavailable(period: period)
            return
        }
        comparisonState = .loading(date: target)
        comparisonTask = Task { [weak self, target, period, apiClient] in
            guard let self else { return }
            let result: ComparisonState
            do {
                let response = try await apiClient.fetchDay(date: target)
                if Task.isCancelled { return }
                if let snapshot = ComparisonSnapshot.from(date: target, response: response) {
                    result = .ready(snapshot, period: period)
                } else {
                    result = .unavailable(period: period)
                }
            } catch {
                if Task.isCancelled { return }
                result = .unavailable(period: period)
            }
            if Task.isCancelled { return }
            self.comparisonState = result
        }
    }

    private func resolveCompareDate(period: ComparePeriod) -> String? {
        guard let parsed = DateFormatting.parseDayDate(date),
              let target = DateFormatting.sydneyCalendar
                  .date(byAdding: .day, value: period.dayOffset, to: parsed)
        else { return nil }
        return DateFormatting.dayDateString(from: target)
    }
}
```

Each `Task.isCancelled` guard is non-negotiable: cancellation is cooperative, so an awaited `fetchDay` whose body has already produced bytes will resume even after the task is cancelled. Without these guards, a stale `.ready` could overwrite a newer `.loading`.

DST: `Calendar.date(byAdding: .day, ...)` operates on calendar days, so the −1 / −7 day offsets are stable across DST transitions. The `dayDateString` formatter writes wall-clock `YYYY-MM-DD` in Sydney's timezone via `sydneyCalendar`, matching how the rest of the app addresses days.

### `DayDetailView` wiring

```swift
struct DayDetailView: View {
    @State private var viewModel: DayDetailViewModel
    // ...

    @AppStorage(UserDefaults.compareEnabledKey, store: UserDefaults.fluxAppGroup)
    private var compareEnabled: Bool = false

    @AppStorage(UserDefaults.comparePeriodKey, store: UserDefaults.fluxAppGroup)
    private var comparePeriodRaw: String = ComparePeriod.yesterday.rawValue

    private var comparePeriod: Binding<ComparePeriod> {
        Binding(get: { ComparePeriod.parseOrDefault(comparePeriodRaw) },
                set: { comparePeriodRaw = $0.rawValue })
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FluxTheme.Metrics.panelGap) {
                // ... existing header / DayNavigationHeader / DayDetailNoteSection ...

                CompareControl(
                    enabled: $compareEnabled,
                    period: comparePeriod,
                    unavailable: viewModel.comparisonState.isUnavailable
                )

                if let dailyUsage = viewModel.dailyUsage, !dailyUsage.blocks.isEmpty {
                    DayInFiveBlocksPanel(dailyUsage: dailyUsage,
                                         compare: viewModel.comparisonState)
                }

                contentSection

                SummaryBlock(
                    title: "Power",
                    trailing: trailingSummaryDate,
                    summary: viewModel.summary,
                    offpeakGridImport: viewModel.summary?.offpeakGridImportKwh ?? viewModel.offpeakStats.gridImportKwh,
                    showsBatteryCycle: false,
                    compare: viewModel.comparisonState
                )

                BatteryBlock(/* unchanged */)
            }
        }
        .task(id: viewModel.date) { await viewModel.loadDay() }
        .onChange(of: compareEnabled, initial: true) { _, _ in
            viewModel.updateCompare(enabled: compareEnabled, period: comparePeriod.wrappedValue)
        }
        .onChange(of: comparePeriodRaw) { _, _ in
            viewModel.updateCompare(enabled: compareEnabled, period: comparePeriod.wrappedValue)
        }
        .onChange(of: viewModel.date) { _, _ in
            viewModel.updateCompare(enabled: compareEnabled, period: comparePeriod.wrappedValue)
        }
        // ... rest unchanged
    }
}
```

The day-navigation re-fetch uses `.onChange(of: viewModel.date)` rather than `.task(id:)` so the comparison's `.loading` state is set in the same pass that mutates `viewModel.date`. This avoids a frame in which the rows render at pre-Compare height before the new `.loading` arrives.

The `initial: true` flag on the `compareEnabled` `.onChange` triggers the comparison fetch on first appearance, when `compareEnabled` was already `true` from a previous launch. `viewModel.date` is non-empty when `DayDetailView` is constructed (`init(date:apiClient:...)`), so this fire never sees an empty selected date.

### Persistence keys

```swift
extension UserDefaults {
    static let compareEnabledKey = "compareEnabled"
    static let comparePeriodKey  = "comparePeriod"   // stores ComparePeriod.rawValue
}
```

Stored in `UserDefaults.fluxAppGroup` (matching the existing `themeIdentifier` / `appFontFamily` pattern). Per-device by definition; no iCloud sync (AC 6.2). Using the app-group store leaks two strings into the widget sandbox; benign — widgets ignore them.

`comparePeriodRaw`'s decoding goes through `ComparePeriod.parseOrDefault`, which falls back to `.yesterday` when a future build's value (e.g. `"lastMonth"`) is read by the current build.

## Data Models

No new data types cross the API boundary. `ComparisonSnapshot` is a client-only projection of the existing `DayDetailResponse`.

## Error Handling

| Failure mode | Detection | Result |
|--------------|-----------|--------|
| `fetchDay` throws | `do { try await ... } catch` | `.unavailable(period:)` |
| Response has neither `summary` nor `dailyUsage` | `ComparisonSnapshot.from` returns nil | `.unavailable(period:)` |
| Resolved date predates earliest record | Same as above (server returns 200 + empty fields) | `.unavailable(period:)` |
| Per-row field nil within a successful snapshot | `DeltaFormatter.sublineContent` returns `.reserved` | Slot reserved, no text rendered |
| Task cancelled (toggle off, day-nav, period change) | Three `Task.isCancelled` guards in `loadComparison` | No state mutation; the new fetch's outcome wins |
| `parseDayDate` / `sydneyCalendar.date(byAdding:)` returns nil | Pre-task date-resolution guard | Synchronous `.unavailable(period:)`; no fetch issued |

The failure caption is bound to `comparisonState.isUnavailable` only. `.loading` neither shows a caption nor a spinner — sub-line slots stay reserved with empty content. AC 5.6's "no row height changes while the fetch is in flight" is satisfied because `.loading` and `.ready` both produce reserved-or-text sub-line slots of equal height.

## Testing Strategy

Unit tests (Swift Testing, alongside existing `FluxTests/`):

- `ComparePeriodTests` — `dayOffset` and `displayName`; `parseOrDefault` falls back to `.yesterday` for nil, empty, and unknown rawValues.
- `ComparisonSnapshotTests` — `from(response:)` mapping, `houseUsed` and `peakGridImport` derivations, the "no summary AND no dailyUsage → nil" case, the "summary present with all-`SR`-fields-nil → non-nil snapshot, per-row fallback handles it" case, and the "partial availability: summary present, dailyUsage nil → non-nil snapshot, SummaryBlock renders deltas while Five-Block rows individually fall back" case.
- `DeltaFormatterTests`:
  - `sublineContent` returns `.reserved` when comparison is nil OR current is nil.
  - Rounding boundary: `current=10.04, comparison=10.0` → `.text("— kWh")`; `current=10.05, comparison=10.0` → `.text("▲ 0.1 kWh")`; `current=10.0, comparison=10.05` → `.text("▼ 0.1 kWh")`. Pinned by example tests; `String(format: "%.1f", ...)` is the formatter under test.
  - Sign formatting: positive → `▲`, negative → `▼`, post-rounding zero → `—`.
  - VoiceOver label composition for `.ready` (with and without `labelSub`) and for the fallback path.
- `DayDetailViewModelTests` (extends existing) — comparison-fetch lifecycle:
  - Toggle on triggers fetch; success → `.ready`.
  - Backend error → `.unavailable`.
  - Empty response → `.unavailable`.
  - Period change cancels in-flight task and starts a new one (assert via a slow-resolving mock that the early Task's outcome does not overwrite the late Task's).
  - Day-navigation reissues the fetch with the new resolved date.
  - Toggle off cancels in-flight task and resets to `.off`.
  - Date-resolution failure (corrupt `viewModel.date`) → synchronous `.unavailable`, no fetch.

View-tree tests (Swift Testing only — no third-party snapshot library):

- `SummaryBlock` and `DayInFiveBlocksPanel` rendered-height assertions for `compare = .off`, `compare = .loading`, `compare = .ready(snapshot)` (per-row delta and per-row fallback paths), and `compare = .unavailable`. The `.off` height is captured against a fixture and asserted byte-equal to `.loading` / `.ready` `... .row.height − sublineSlotHeight`. This avoids the trap of "no `ValueSubline` in tree" — `ValueSubline(content: .hidden)` *is* in the tree but renders `EmptyView()` and contributes zero frame, so a tree-shape check would falsely fail.
- `ValueSubline` line-height: render `.reserved` and `.text("▲ 1.2 kWh")` and assert measured heights are equal at the default Dynamic Type size.
- AC 5.6 height stability is verified at the default Dynamic Type size only. Dynamic Type variance is deferred per Decision 14; non-default sizes are not tested in v1 and any divergence at AX1+ is a known acceptable risk.

Manual smoke (post-implementation):

- Compare on → toggle off-on jitter check during day-nav (per Decision 16).
- VoiceOver pass: row reads as one element with the composed label; fallback label omits the comparison clause; failure caption is spoken.
- macOS parity pass.

Property-based testing is not appropriate for this feature — there is no algorithm with universal invariants worth fuzzing. The delta formatter's rounding boundary is exhaustively covered by example tests above.
