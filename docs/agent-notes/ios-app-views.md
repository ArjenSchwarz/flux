# iOS App — Views

## Dashboard Views (Dashboard/)

- **BatteryHeroView** — SOC percentage (large text), ProgressView, status line with cutoff time. Uses `BatteryColor.forSOC().color` for text and progress tint.
- **PowerTrioView** — Solar/Load/Grid columns. Solar green when ppv > 0, load red above threshold, grid colored via `GridColor.forGrid().color`. Accepts `nowProvider` for testable time.
- **SecondaryStatsView** — "Lowest" (lowest SoC since the most recent off-peak window end; reads `battery.low24h` — wire field name retained), off-peak grid/delta, 15-min avg load with cutoff time colored via `CutoffTimeColor.forCutoff().color`. Accepts `nowProvider`.
- **TodayEnergyView** — kWh totals for solar, grid in/out, charge/discharge.
- **DashboardView** — ScrollView + VStack with `.refreshable`. Two distinct error states: `initialLoadErrorCard` (when status is nil + error) and `stalenessBanner` (when status exists + error). Both have Retry and conditional Settings buttons. Sheet-based settings access.

## History Views (History/)

- **HistoryView** — Multi-card layout. Range picker (7/14/30/Wk/Mo), the period overview card, three mounted chart cards (solar, grid usage, daily usage), then a per-day summary card and `View day detail` link. (`HistoryBatteryCard` still exists but is not currently mounted in this layout.) The chart-card helpers pass `viewModel.chartDomain` for the to-date x-axis reservation. Empty/error states replace the cards when there is no data.
- **HistoryStatsOverviewCard** — Sits above the chart cards. Eight tiles in a `LazyVGrid` (2 columns on iOS compact, 4 on regular and macOS) covering Total usage, Total solar, Exported, Peak imports, Avg night, Most usage, Most solar, Lowest SoC. Per `history-usage-stats` Decision 15, the totals (Total usage, Total solar) and the day-records (Most usage, Most solar) include today — only Avg night (and the chart cards' per-day averages) excludes it. Total-usage/Total-solar em-dash gates use the today-inclusive display counts (`dailyUsageDisplayDayCount`, `dayCount`), so a today-only range shows a real number. Card chrome KPI is the inclusive Sydney date range covered (`HistoryCardChrome.kpi` is optional). The three day-record tiles (Most usage, Most solar, Lowest SoC) are tappable when populated and call `HistoryViewModel.selectDay` via the existing `(String) -> Void` plumbing — em-dash tiles are non-tappable and don't expose `.isButton`. Static helpers (`label(for:)`, `valueText(for:summary:)`, `dateLineText(for:summary:)`, `isTappable(tile:summary:)`, `tapAction(for:summary:onSelect:)`, `accessibilityLabel(tile:summary:)`) keyed by `TileKey` make the rendering testable without a SwiftUI layout pass.
- **HistoryStatsFormatters** — Tile-specific formatters: `socPercent` (half-up, `—` for non-finite), `shortDate` (`MMM d` Sydney), `dateWithTime` (parses an ISO timestamp and renders `MMM d at HH:mm`), `dateRange` (min/max so reversed input still works), and the `accessibleKwh` / `accessibleSocPercent` spell-out variants for VoiceOver.
- **HistorySolarCard** — Green daily bars with a dashed average rule. Today's bar dimmed.
- **HistoryGridUsageCard** — Diverging stacked bars: peak import (red) on top of off-peak import (teal), exports (blue) below the zero line. Header KPI leads with peak imports because that's the actionable number for an off-peak charging strategy. Days without an off-peak record are hidden from this card; if no day has a split the card shows a placeholder.
- **HistoryBatteryCard** — Charge (orange, above zero) and discharge (purple, below zero) per day.
- **HistoryDailyUsageCard** — Stacked bars per day with five segments in chronological order (Night → Morning Peak → Off-Peak → Afternoon Peak → Evening). Colour palette pinned per Decision 5: indigo / orange / teal / red / purple. Today's bar at 50% opacity. Placeholder copy `No load breakdown available for this range.` when `summary.dailyUsageDisplayDayCount == 0` (no day-with-blocks at all); a today-only range now shows today's bar rather than the placeholder, with the KPI/subtitle staying em-dash until a complete day exists. Subtitle is `"{kind} largest at {kwh} kWh/day average"`. Static helpers (`kpi(for:)`, `subtitle(for:)`, `opacity(for:)`, `placeholderCopy`, `shouldShowPlaceholder(summary:)`) are exposed for unit tests since the project has no rendered-tree inspection library.
- **DailyUsageBlockKindStyling** — Extension on `FluxCore.DailyUsageBlock.Kind` exposing `chronologicalOrder`, `chronologicalIndex`, `chartColor`, `displayLabel`. Lives in the iOS app target (not FluxCore) because `Color` is SwiftUI; shared with Day Detail's `DailyUsageCard` so labels stay aligned across screens.
- **HistoryChartDomain** — Full-period x-axis reservation for the to-date ranges (`history-month-week-to-date` Decision 7). `HistoryViewModel.chartDomain` builds it from the resolved range, Sydney `now`, and locale first weekday: Wk → a full 7-day week, Mo → the full calendar month (from the month interval), fixed `.days` → `nil` (auto-fit). Charts apply it with the `historyChartXScale(_:)` View modifier and render invisible zero-height `scaffold` `BarMark`s at every slot date so Swift Charts infers a consistent one-day bar width even when only one real day has data. Scoped to the inline cards — the expanded-chart host passes `nil` because its `ChartScope` carries only a day count, not the range type.
- **HistoryCardChrome** — Shared rounded-rectangle container, header (title + KPI) and optional subtitle.
- **ChartHighlightOverlay** — `historySelectionOverlay` view extension. Shared drag-to-select gesture that maps the touch x-position to the nearest entry's date and reports the day ID; a single `selectedDay` on the view model drives the highlight rectangle in every chart.
- **HistoryFormatters** — `kwh` helper picks 1 decimal under 100 kWh, 0 above.

## Day Detail Views (DayDetail/)

- **SOCChartView** — AreaMark + LineMark for SOC over time. Dashed RuleMark at cutoff percent. PointMark for low annotation with timestamp.
- **PowerChartView** — AreaMark for solar (green), LineMark for load, colored areas for grid import/export.
- **BatteryPowerChartView** — LineMark with negated pbat, RuleMark at zero line.
- **DayChartDomain** — 14-line helper computing consistent 00:00–00:00 Sydney time domain for all charts. Prevents cross-chart domain inconsistencies.
- **DayDetailView** — Chevron navigation (previous/next day). `.task(id: viewModel.date)` triggers reload on date change. Power charts hidden when `!hasPowerData`. Auth/config errors show settings CTA. Note row sits at top of VStack: tappable when note exists, "Add note" button when nil and date ≤ today Sydney, EmptyView for future dates.
- **NoteRowView** — Read-only note row shared across Dashboard, History, Day Detail. Returns `EmptyView` when `text` is nil/empty so callers place it unconditionally.
- **NoteEditorSheet** — Sheet-presented `NavigationStack` with `TextEditor`, remaining-character counter, Cancel/Save toolbar. Save dismisses on success; failure keeps sheet open with error message.

## Settings Views (Settings/)

- **SettingsView** — Form with Backend (URL, token) and Display (load alert threshold) sections. Save button disabled during validation. Error display with user-friendly messages.

## Navigation (Navigation/)

- **Screen** — Enum: `.dashboard`, `.history`, `.settings` with `title` and `systemImage`.
- **AppNavigationView** — Root view using `NavigationSplitView`. Creates `URLSessionAPIClient` from UserDefaults URL + Keychain token. `effectiveScreen` computed property redirects unconfigured state to settings. `scenePhase` handling reloads dependencies when app becomes active. `onSaved` callback from SettingsView triggers dependency rebuild.

## Patterns

- All `#Preview` blocks wrapped in `#if DEBUG` (MockFluxAPIClient is debug-only).
- Views use `ColorTier.color` for semantic color access (logic is testable without SwiftUI).
- Charts parse timestamps inline via `DateFormatting` static formatters.
- `historyFactory` closure pattern in DashboardView for navigation to History.
- `@Bindable` used for view model bindings in forms.
