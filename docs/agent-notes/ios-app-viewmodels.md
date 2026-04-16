# iOS App — ViewModels

All ViewModels follow `@MainActor @Observable final class` pattern with `private(set)` on published state.

## DashboardViewModel (Dashboard/DashboardViewModel.swift)

- **Dependencies:** `apiClient: any FluxAPIClient`, injectable `nowProvider`, `sleep`, `refreshInterval` (all for testable auto-refresh timing).
- **State:** `status: StatusResponse?`, `lastSuccessfulFetch: Date?`, `error: FluxAPIError?`, `isLoading: Bool`.
- `refresh()` guards on `isLoading` to prevent concurrent fetches. Preserves previous `status` on failure (stale data is better than no data).
- `startAutoRefresh()` cancels existing task before creating new 10s loop. Idempotent — safe to call multiple times.
- Uses `weak self` in task closure to avoid retain cycles in long-lived refresh tasks.

## HistoryViewModel (History/HistoryViewModel.swift)

- **Dependencies:** `apiClient`, `modelContext` (SwiftData), `nowProvider`.
- **State:** `days`, `selectedDay`, `selectedDayRange` (7/14/30), `chartDays`, `chartEntries`, `isLoading`, `error`.
- Upsert-based caching: `cacheHistoricalDays()` updates existing `CachedDayEnergy` records instead of relying on unique constraint violations.
- Falls back to SwiftData cache on network failure via `loadCachedDays()`.
- `rebuildChartData()` transforms raw `DayEnergy` arrays into typed `HistoryChartDay`/`HistoryChartEntry`/`HistoryChartMetric` structs. Triggered automatically via `didSet` on `days`. This keeps chart computation out of view bodies.
- `selectDefaultDayIfNeeded()` preserves selection across reloads.
- Concurrent load guard prevents duplicate requests.

## DayDetailViewModel (DayDetail/DayDetailViewModel.swift)

- **Dependencies:** `apiClient`, `nowProvider`.
- **State:** `date` (private(set)), `readings`, `summary`, `isLoading`, `error`, `hasPowerData`.
- Uses centralised `DateFormatting.parseDayDate` and `dayDateString` (cached formatters, not created per call).
- `navigatePrevious()`/`navigateNext()` methods with `navigateNext` blocking advancement past today.
- Load guard prevents duplicate requests.
- `isFallbackData()` checks if readings are synthetic (backend returns synthetic data for days without real readings).

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
