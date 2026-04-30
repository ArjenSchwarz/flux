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
- **PeriodSummary:** Aggregates per-day totals across complete days (today excluded except for grid off-peak counts, which include today). Daily-usage fields: `dailyUsageTotalKwh`, `dailyUsageDayCount`, `dailyUsageLargestKind` (with 0.01 kWh tolerance-band tie-break by chronological order), `dailyUsageLargestKindTotalKwh`, plus `dailyUsageAvgKwh` / `dailyUsageLargestKindAvgKwh` accessors.
- **DailyUsageEntry:** Per-day struct with `blocks` sorted into chronological order, clamped `totalKwh` ≥ 0 per block, `stackedTotalKwh`, `isToday`. `accessibilitySummary` formats `{date}: {kwh}, {largestKind} largest` for VoiceOver.
- Upsert-based caching: `cacheHistoricalDays()` updates existing `CachedDayEnergy` records, including the four derived fields (`dailyUsage`, `socLow`, `socLowTime`, `peakPeriods`). `warnIfClearing(cached:day:)` fires the injected `warn` callback once per (date, fieldName) pair when a non-nil cached value is overwritten with nil — observability for unexpected backend nil-emit (Decision 6).
- Falls back to SwiftData cache on network failure via `loadCachedDays()`.
- `selectDefaultDayIfNeeded()` preserves selection across reloads.
- Concurrent load guard prevents duplicate requests.

## DayDetailViewModel (DayDetail/DayDetailViewModel.swift)

- **Dependencies:** `apiClient`, `nowProvider`.
- **State:** `date` (private(set)), `readings`, `summary`, `isLoading`, `error`, `hasPowerData`, `note`.
- Uses centralised `DateFormatting.parseDayDate` and `dayDateString` (cached formatters, not created per call).
- `navigatePrevious()`/`navigateNext()` methods with `navigateNext` blocking advancement past today.
- Load guard prevents duplicate requests.
- `isFallbackData()` checks if readings are synthetic (backend returns synthetic data for days without real readings).
- `saveNote(_:)` applies client-side `NoteText.normalised + graphemeCount` cap (throws `FluxAPIError.badRequest` if over 200) before calling `apiClient.saveNote`. On success: sets `note = response.text` (or nil if empty — server confirms delete by returning empty text).

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
