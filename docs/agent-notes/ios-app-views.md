# iOS App — Views

## Dashboard Views (Dashboard/)

- **BatteryHeroView** — SOC percentage (large text), ProgressView, status line with cutoff time. Uses `BatteryColor.forSOC().color` for text and progress tint.
- **PowerTrioView** — Solar/Load/Grid columns. Solar green when ppv > 0, load red above threshold, grid colored via `GridColor.forGrid().color`. Accepts `nowProvider` for testable time.
- **SecondaryStatsView** — 24h low, off-peak grid/delta, 15-min avg load with cutoff time colored via `CutoffTimeColor.forCutoff().color`. Accepts `nowProvider`.
- **TodayEnergyView** — kWh totals for solar, grid in/out, charge/discharge.
- **DashboardView** — ScrollView + VStack with `.refreshable`. Two distinct error states: `initialLoadErrorCard` (when status is nil + error) and `stalenessBanner` (when status exists + error). Both have Retry and conditional Settings buttons. Sheet-based settings access.

## History Views (History/)

- **HistoryChartView** — Grouped BarMark with metrics, day range picker (7/14/30). Chart data comes pre-computed from HistoryViewModel.
- **HistoryView** — Chart + summary card, `ContentUnavailableView` when empty. Error card with retry/settings for empty-cache failures.

## Day Detail Views (DayDetail/)

- **SOCChartView** — AreaMark + LineMark for SOC over time. Dashed RuleMark at cutoff percent. PointMark for low annotation with timestamp.
- **PowerChartView** — AreaMark for solar (green), LineMark for load, colored areas for grid import/export.
- **BatteryPowerChartView** — LineMark with negated pbat, RuleMark at zero line.
- **DayChartDomain** — 14-line helper computing consistent 00:00–00:00 Sydney time domain for all charts. Prevents cross-chart domain inconsistencies.
- **DayDetailView** — Chevron navigation (previous/next day). `.task(id: viewModel.date)` triggers reload on date change. Power charts hidden when `!hasPowerData`. Auth/config errors show settings CTA.

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
