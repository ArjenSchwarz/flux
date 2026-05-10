# iOS App — ViewModels

All ViewModels follow `@MainActor @Observable final class` pattern with `private(set)` on published state.

## DashboardViewModel (Dashboard/DashboardViewModel.swift)

- **Dependencies:** `apiClient: any FluxAPIClient`, injectable `nowProvider`, `sleep`, `refreshInterval` (all for testable auto-refresh timing).
- **State:** `status: StatusResponse?`, `lastSuccessfulFetch: Date?`, `error: FluxAPIError?`, `isLoading: Bool`.
- `refresh()` guards on `isLoading` to prevent concurrent fetches. Preserves previous `status` on failure (stale data is better than no data).
- `startAutoRefresh()` cancels existing task before creating new 10s loop. Idempotent — safe to call multiple times.
- Uses `weak self` in task closure to avoid retain cycles in long-lived refresh tasks.

## HistoryViewModel (History/HistoryViewModel.swift)

- **Dependencies:** `apiClient`, `modelContext` (SwiftData), `nowProvider`, injectable `warn: @Sendable (String) -> Void` (defaults to `HistoryCacheLog.defaultWarn`, which logs to `Logger(subsystem: "eu.arjen.flux", category: "history-cache")`).
- **State:** `days`, `selectedDay`, `isLoading`, `error`. Range (7/14/30) is owned by `HistoryView`, not the ViewModel.
- **Derived series:** `derived` computed property returns `DerivedState` (`solar`, `grid`, `battery`, `dailyUsage` series + `summary`). Built in a single pass in `DerivedState.init(days:now:)`. Convenience accessors (`solarSeries`, `gridSeries`, `batterySeries`, `dailyUsageSeries`, `summary`) for tests/previews.
- **PeriodSummary:** Aggregates per-day totals across complete days (today excluded except for grid off-peak counts, which include today). Daily-usage fields: `dailyUsageTotalKwh`, `dailyUsageDayCount`, `dailyUsageLargestKind` (with 0.01 kWh tolerance-band tie-break by chronological order), `dailyUsageLargestKindTotalKwh`, plus `dailyUsageAvgKwh` / `dailyUsageLargestKindAvgKwh` accessors. Overview-card fields: `nightTotalKwh`, `nightBlockDayCount` (denominator for `nightAvgKwh`, presence-based — only days that contributed a `night` block count), `mostUsageDay` / `mostSolarDay` / `lowestSocDay` (record types — see below). `lowestSocDay` includes today; the rest follow the existing `!isToday` / off-peak-presence rules. Tie-breaks on all three day-records pick the most recent date.
- **DayKwhRecord** / **LowestSocRecord:** Equatable nested types in `HistoryDerivedState.swift`. Both carry `dayID`, `date`, and the comparable value (`kwh` or raw `Double` `soc`). `LowestSocRecord` additionally carries `socLowTime: String?` (full ISO timestamp from the wire). The raw-`Double` `soc` is load-bearing for the tie-break (see `HistoryStatsFormatters.socPercent` rounding); a fileprivate `DayRecordValue` protocol makes the generic `consider` comparator in `Totals` work over both types.
- **DailyUsageEntry:** Per-day struct with `blocks` sorted into chronological order, clamped `totalKwh` ≥ 0 per block, `stackedTotalKwh`, `isToday`. `accessibilitySummary` formats `{date}: {kwh}, {largestKind} largest` for VoiceOver.
- Upsert-based caching: `cacheHistoricalDays()` updates existing `CachedDayEnergy` records, including the four derived fields (`dailyUsage`, `socLow`, `socLowTime`, `peakPeriods`). `warnIfClearing(cached:day:)` fires the injected `warn` callback once per (date, fieldName) pair when a non-nil cached value is overwritten with nil — observability for unexpected backend nil-emit (Decision 6).
- Falls back to SwiftData cache on network failure via `loadCachedDays()`.
- `selectDefaultDayIfNeeded()` preserves selection across reloads.
- Concurrent load guard prevents duplicate requests.

## DayDetailViewModel (DayDetail/DayDetailViewModel.swift)

- **Dependencies:** `apiClient`, `nowProvider`.
- **State:** `date` (private(set)), `readings`, `summary`, `isLoading`, `error`, `hasPowerData`, `note`, `comparisonState`.
- Uses centralised `DateFormatting.parseDayDate` and `dayDateString` (cached formatters, not created per call).
- `navigatePrevious()`/`navigateNext()` methods with `navigateNext` blocking advancement past today.
- Load guard prevents duplicate requests.
- `isFallbackData()` checks if readings are synthetic (backend returns synthetic data for days without real readings).
- `saveNote(_:)` applies client-side `NoteText.normalised + graphemeCount` cap (throws `FluxAPIError.badRequest` if over 200) before calling `apiClient.saveNote`. On success: sets `note = response.text` (or nil if empty — server confirms delete by returning empty text).

### Compare lifecycle

- `comparisonState: ComparisonState` is `.off | .loading(date:) | .ready(snapshot, period:) | .unavailable(period:)`. Drives the Day Detail Compare feature (T-1161). The view subscribes to it through `SummaryBlock(compare:)` and `DayInFiveBlocksPanel(compare:)`.
- `updateCompare(enabled:period:)` is the single entry point. It cancels any in-flight `comparisonTask`, then either resets to `.off`, short-circuits to synchronous `.unavailable` on date-resolution failure, or kicks off a fetch whose outcome maps to `.ready` / `.unavailable`.
- **Three `Task.isCancelled` guards inside the spawned task are load-bearing.** Cancellation is cooperative — an awaited `fetchDay` whose body has already produced bytes will resume even after the task is cancelled. Without the guards a slow Task A whose cancellation completes after Task B has already written `.ready` could overwrite Task B's outcome.
- The Task captures `[apiClient]` only (no `[weak self]`) and writes `self.comparisonState = result` at the end. The strong `self` capture is intentional so the result lands; lifetime is bounded by `deinit { comparisonTask?.cancel() }`.
- `resolveCompareDate(period:)` uses `DateFormatting.parseDayDate` + `sydneyCalendar.date(byAdding: .day, value: period.dayOffset)` so the −1 / −7 day offsets are stable across DST transitions in Sydney.
- `DayDetailView` triggers `updateCompare` from three `.onChange` reactions — `compareEnabled` (with `initial: true` so a previously-on toggle re-fires the fetch on first appearance), `comparePeriodRaw`, and `viewModel.date`. The `viewModel.date` reaction sits next to `.task(id: viewModel.date)` so the primary `loadDay` and the comparison fetch run in parallel after day-navigation.
- Compare preferences live in `UserDefaults.fluxAppGroup` under keys `compare.enabled` and `compare.period` (per device, no iCloud sync). `ComparePeriod.parseOrDefault` falls back to `.yesterday` for unknown raw values so a future build's enum case never crashes the current build.

## NoteEditorViewModel (DayDetail/NoteEditorViewModel.swift)

- Owns `draft`, `isSaving`, `error: FluxAPIError?`.
- `canSave` = `!isSaving && characterCount <= NoteText.maxGraphemes` — disables both during in-flight saves (no double-tap) and over the cap (no client/server disagreement).
- `save()` returns `Bool`: true on success (caller dismisses sheet), false when call was suppressed or backend rejected; on throw it sets `error` and leaves `draft` intact for retry.
- Calls `parent.saveNote(draft)`; saves go through `DayDetailViewModel` so the parent's `note` updates atomically with the API response.

## SettingsViewModel (Settings/SettingsViewModel.swift)

- **Dependencies:** `keychainService`, `userDefaults`, `apiClientFactory` (closure for testable client creation).
- **Editable state:** `apiURL`, `apiToken`, `loadAlertThreshold`.
- **Validation state:** `isValidating`, `validationError`, `shouldDismiss`.
- `save()` captures form values at save-start with explicit local variables to prevent race conditions from user edits during async validation. Guards against concurrent saves via `isValidating` check. Trims whitespace from URL.
- Custom error messages per `FluxAPIError` case via `message(for:)`.
- `loadExisting()` populates form from Keychain + UserDefaults on view appearance.

## UserDefaults+Settings (Settings/UserDefaults+Settings.swift)

- Extension on `UserDefaults` with private `Keys` enum for type-safe key constants.
- Properties: `apiURL: String?`, `loadAlertThreshold: Double` (defaults to 3000).

## Testing Patterns

- Each test file creates its own focused mock (not sharing `MockFluxAPIClient`).
- Actor-based `MockSettingsAPIClient` with configurable `fetchDelay` for race-condition testing.
- `CaptureBox` actor for cross-actor state capture in settings tests.
- In-memory `ModelContainer` for SwiftData tests.
