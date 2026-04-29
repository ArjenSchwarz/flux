# iOS App — Views

## Dashboard Views (Dashboard/)

- **BatteryHeroView** — SOC percentage (large text), ProgressView, status line with cutoff time. Uses `BatteryColor.forSOC().color` for text and progress tint.
- **PowerTrioView** — Solar/Load/Grid columns. Solar green when ppv > 0, load red above threshold, grid colored via `GridColor.forGrid().color`. Accepts `nowProvider` for testable time.
- **SecondaryStatsView** — 24h low, off-peak grid/delta, 15-min avg load with cutoff time colored via `CutoffTimeColor.forCutoff().color`. Accepts `nowProvider`.
- **TodayEnergyView** — kWh totals for solar, grid in/out, charge/discharge.
- **DashboardView** — ScrollView + VStack with `.refreshable`. Two distinct error states: `initialLoadErrorCard` (when status is nil + error) and `stalenessBanner` (when status exists + error). Both have Retry and conditional Settings buttons. Sheet-based settings access.

## History Views (History/)

- **HistoryView** — Multi-card layout. Range picker (7/14/30), three chart cards (solar, grid usage, battery), then a per-day summary card and `View day detail` link. Empty/error states replace the cards when there is no data.
- **HistorySolarCard** — Green daily bars with a dashed average rule. Today's bar dimmed.
- **HistoryGridUsageCard** — Diverging stacked bars: peak import (red) on top of off-peak import (teal), exports (blue) below the zero line. Header KPI leads with peak imports because that's the actionable number for an off-peak charging strategy. Days without an off-peak record are hidden from this card; if no day has a split the card shows a placeholder.
- **HistoryBatteryCard** — Charge (orange, above zero) and discharge (purple, below zero) per day.
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
