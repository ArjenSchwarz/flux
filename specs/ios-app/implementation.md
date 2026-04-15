# iOS App Implementation Explanation

## Beginner Level

### What Changed / What This Does

This branch adds a complete iOS app called Flux that monitors a home battery system. The app talks to a backend server (a Lambda function on AWS) that collects data from an AlphaESS battery. The app never talks to the battery directly — it only reads pre-computed data from the server.

The app has four screens:

- **Dashboard** — shows live battery percentage, solar/load/grid power readings, and today's energy totals. It refreshes automatically every 10 seconds.
- **History** — shows a bar chart of daily energy totals (solar, grid in/out, battery charge/discharge) for the last 7, 14, or 30 days. Tapping a day shows a summary card.
- **Day Detail** — shows three time-series charts for a specific day: battery charge level (SOC), power flows (solar/load/grid), and battery power (charge vs discharge). You can navigate between days with arrow buttons.
- **Settings** — lets you enter the backend URL and API token. The app validates the connection before saving.

### Why It Matters

This replaces the official AlphaESS app for day-to-day monitoring. The official app is slow and cluttered — Flux shows exactly the data a home battery owner cares about, with colour-coded warnings (battery low, grid importing, cutoff time approaching).

### Key Concepts

- **MVVM** — each screen has a "view model" that handles data fetching and state management, keeping the visual code (SwiftUI views) simple.
- **SwiftData** — Apple's database framework. The app caches historical energy data so the History screen loads instantly for previously viewed days.
- **Keychain** — Apple's secure storage. The API token is stored here (encrypted), not in regular settings.
- **SwiftUI Charts** — Apple's charting framework. All graphs are built with this — no third-party libraries.
- **Liquid Glass** — iOS 26's new visual style. The app uses native controls that automatically adopt the translucent glass appearance.

---

## Intermediate Level

### Changes Overview

The implementation spans 11 commits building the app bottom-up:

**Foundation layer** (`Models/`, `Services/`, `Helpers/`):
- `APIModels.swift` — Codable structs mirroring the three API endpoints (`/status`, `/history`, `/day`)
- `FluxAPIError.swift` — typed error enum with String payloads for Sendable compliance
- `FluxAPIClient.swift` — protocol with three async methods; `URLSessionAPIClient` is the production implementation
- `KeychainService.swift` — Security framework wrapper with App Group access for future widget sharing
- `DateFormatting.swift` — centralised Sydney-timezone date parsing and formatting (the backend uses `Australia/Sydney` for all date boundaries)
- `BatteryColor.swift`, `GridColor.swift`, `CutoffTimeColor.swift` — colour logic extracted into testable helpers

**View models** (`Dashboard/`, `History/`, `DayDetail/`, `Settings/`):
- All use `@MainActor @Observable` pattern
- `DashboardViewModel` manages a 10-second auto-refresh loop with `Task` cancellation on background/disappear
- `HistoryViewModel` fetches from API, caches historical days in SwiftData, falls back to cache on network failure
- `DayDetailViewModel` handles day navigation with synchronous date mutation; the view uses `.task(id: date)` for automatic fetch cancellation
- `SettingsViewModel` validates credentials against the backend before saving, using a `tokenProvider` closure pattern to avoid the chicken-and-egg problem

**Views**:
- `AppNavigationView` — root `NavigationSplitView` with `preferredCompactColumn: .detail` (Dashboard on iPhone, sidebar on iPad)
- Dashboard composed of `BatteryHeroView`, `PowerTrioView`, `SecondaryStatsView`, `TodayEnergyView`
- History uses `BarMark` with `.position(by:)` for grouped bars, `chartOverlay` with `DragGesture` for day selection
- Day Detail uses three chart views (`SOCChartView`, `PowerChartView`, `BatteryPowerChartView`) receiving pre-parsed timestamps from the view model
- Settings uses a `Form` with validation-driven dismiss flow

**Tests** (10 test suites, 56 tests):
- `URLSessionAPIClientTests` — uses `URLProtocol` mock to verify request construction, error mapping, and token handling
- `APIModelsTests` — JSON decoding tests for all models including null/partial field scenarios
- View model tests for all four screens using mock API clients
- `DateFormattingTests`, `ColoringTests` — boundary tests for helper logic
- `KeychainServiceTests` — round-trip and deletion tests

### Implementation Approach

The architecture follows a strict layered approach: Models and Services have no SwiftUI imports, making them sharable with future widget/macOS targets. View models own all business logic; views are thin rendering layers.

Key patterns:
- **Token provider closure** — `URLSessionAPIClient` takes a `@Sendable () -> String?` closure. Production injects Keychain lookup; Settings validation injects the entered token directly.
- **Pre-parsed timestamps** — `DayDetailViewModel` parses all `TimeSeriesPoint` timestamps once after fetching, producing `[ParsedReading]`. The three chart views receive these pre-parsed values, avoiding redundant `DateFormatter` calls on every SwiftUI body evaluation.
- **Fallback data detection** — when `/day` returns data from `flux-daily-power` (SOC-only), all power fields are zero. The view model detects this heuristic and hides the power charts.
- **SwiftData caching** — only `CachedDayEnergy` (daily energy summaries) is persisted. Dashboard status is always fresh. Cache queries are scoped with predicates and fetch limits.

### Trade-offs

- **NavigationSplitView from day one** — adds slight complexity on iPhone but avoids a navigation rewrite for iPad/macOS. The sidebar collapses to single-column push navigation on iPhone.
- **No third-party dependencies** — the app uses only Apple frameworks. URLSession with async/await is sufficient for three GET endpoints. SwiftUI Charts covers all needed chart types.
- **Sydney timezone hardcoded** — all date logic uses `Australia/Sydney` to match the backend. Correct for a two-user app in Australia but would need parameterisation for wider use.
- **`AnyView` in DashboardView's `historyFactory`** — used to break a circular dependency where DashboardView needs to construct HistoryView with a ModelContext not available at init time. Accepted as a pragmatic choice for V1.

---

## Expert Level

### Technical Deep Dive

**Concurrency model**: All view models are `@MainActor @Observable`, which satisfies two constraints: `ModelContext` (SwiftData) must stay on its creation actor, and `@Observable` property mutations must happen on the main thread for SwiftUI. The `FluxAPIClient` protocol is `Sendable` for safe cross-actor boundary passing. Error payloads use `String` descriptions (not raw `Error`) for `Sendable` compliance under Swift 6 strict concurrency.

**Auto-refresh lifecycle**: `DashboardViewModel.startAutoRefresh()` cancels any existing `refreshTask` before creating a new one, preventing duplicate timers from rapid foreground/background transitions. The refresh loop uses `Task.sleep(for:)` with cancellation checking. The view calls `startAutoRefresh()` on both `onAppear` and `scenePhase == .active`, with `stopAutoRefresh()` on `onDisappear` and background/inactive transitions. The `refresh()` method guards against concurrent fetches via `isLoading`.

**SwiftData cache scoping**: `cacheHistoricalDays` filters dates before querying, using a `#Predicate` on the incoming date set. `loadCachedDays` uses `fetchLimit` on the descriptor. Both avoid loading the entire table, which matters as the cache grows over months of use.

**Chart rendering**: Day Detail chart views receive `[ParsedReading]` — timestamps are parsed once in the view model, not per-chart per-render. Each chart still computes its own `xDomain` via `DayChartDomain.domain(for:)` (a lightweight `DateFormatter` + calendar addition). The SOC chart's low-point annotation parses the summary timestamp separately since it's a single value, not part of the readings array.

**Battery power sign convention**: The API uses positive `pbat` = discharging, negative = charging. `BatteryPowerChartView` negates this (`-reading.point.pbat`) so charging appears above zero and discharging below, matching user expectations. This negation is applied only in the chart view — all other code uses API convention.

**Settings validation bootstrap**: `SettingsViewModel.save()` captures field values at the start, constructs a temporary `URLSessionAPIClient` with `init(baseURL:token:)` (explicit token, not from Keychain), calls `fetchStatus()` to validate, and only writes to Keychain on success. This avoids poisoning the Keychain with an invalid token if the app crashes during validation.

### Architecture Impact

The separation into `FluxAPIClient` protocol, typed errors, and helper utilities means adding iPad/macOS targets requires only new view code — the service layer, models, and helpers are platform-agnostic. The `NavigationSplitView` root already supports adaptive layout. Widget extensions can share the Keychain token via the App Group and construct their own `URLSessionAPIClient`.

The `ParsedReading` indirection between view model and chart views creates a clean boundary: chart views never parse strings, and the view model doesn't know about chart-specific data structures.

### Potential Issues

- **Off-peak window defaults** — when `offpeak` is nil in the status response, the app falls back to `OffpeakData.defaultWindowStart` / `.defaultWindowEnd` ("11:00"/"14:00"). If the backend changes these defaults, the app's fallback values would diverge. A future version could make the backend always return the window times.
- **Fallback data heuristic** — detecting SOC-only data by checking if all power fields are zero is safe in practice (a running household never has zero load across all readings) but theoretically fragile. A backend `dataSource` flag would be more robust.
- **`ISO8601DateFormatter` fallback** — `DateFormatting.parseTimestamp` tries fractional seconds first, then falls back to no-fractional-seconds format. If the backend changes timestamp format, both formatters would fail silently (returning nil), causing readings to be dropped from charts.
- **No automatic cache pruning** — `CachedDayEnergy` rows accumulate indefinitely. For a personal app this is negligible (365 rows/year at ~100 bytes each), but a cache size limit or age-based pruning would be needed at scale.

---

## Completeness Assessment

### Fully Implemented

All 13 requirement sections (1.1 through 13.5) are implemented:

- Platform: iOS 26+, SwiftUI only, SwiftUI Charts, NavigationSplitView, shared service layer, Liquid Glass, Keychain with App Group, SwiftData
- API Client: three endpoints, Bearer auth, async/await, Codable, typed errors, URLSession only
- Settings: URL + token fields, Keychain storage, UserDefaults for URL, validation before save, redirect on missing config, load alert threshold
- Dashboard: battery hero with colour-coded SOC, power trio with conditional colouring, secondary stats with cutoff time colouring, today's energy, auto-refresh, pull-to-refresh, staleness indicator
- History: grouped bar chart, 7/14/30 range picker, day selection with highlight, summary card, day detail navigation
- Day Detail: three stacked charts (SOC area, power multi-line, battery power), day navigation, fallback data handling, summary card
- Caching: SwiftData for history, today excluded, dashboard never cached
- Error states: staleness indicator, first-launch error, auth error with Settings navigation
- Navigation: NavigationSplitView, single-column on iPhone, sidebar on iPad, Dashboard default

### Partially Implemented

- Some minor chart rendering patterns are repeated across views (x-axis configuration, card background styling) — consolidation would reduce code but isn't functionally impactful.
- `ContentView.swift` and `Item.swift` (Xcode template files) have been removed in the review cleanup.

### Missing

No critical or important spec requirements are missing. The implementation matches all 13 requirement sections and all 9 decision log entries.
