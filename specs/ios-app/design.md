# Design: iOS App

## Overview

The Flux iOS app is a SwiftUI application that displays battery monitoring data from the Flux Lambda API. It uses MVVM architecture with `@MainActor @Observable` view models, NavigationSplitView for adaptive layout, SwiftUI Charts for all visualisations, SwiftData for history caching, and Keychain for credential storage.

**Timezone:** The backend uses `Australia/Sydney` for all date operations. The app uses a shared `TimeZone` constant (`DateFormatting.sydneyTimeZone`) for all date comparisons, "today" determination, and off-peak window checks — never the device's local timezone.

The app has four view-level components: Dashboard, History, Day Detail, and Settings. These are coordinated by a root `AppNavigationView` that manages NavigationSplitView state. All data flows through a single `FluxAPIClient` protocol, making the networking layer testable and swappable.

iOS 26 Liquid Glass styling is applied through native SwiftUI container adoption — navigation bars, toolbars, and grouped lists pick up Liquid Glass material automatically. No manual glass effect modifiers are needed for standard controls.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  FluxApp (App entry point)                                  │
│  ├── ModelContainer (SwiftData)                             │
│  └── AppNavigationView                                      │
│      └── NavigationSplitView                                │
│          ├── Sidebar (screen picker, iPad only)             │
│          └── Detail                                         │
│              ├── DashboardView ← DashboardViewModel         │
│              ├── HistoryView ← HistoryViewModel             │
│              │   └── DayDetailView ← DayDetailViewModel     │
│              └── SettingsView ← SettingsViewModel           │
└─────────────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│  Services Layer                                             │
│  ┌─────────────────┐  ┌──────────────────┐  ┌───────────┐  │
│  │  FluxAPIClient   │  │  KeychainService │  │ SwiftData │  │
│  │  (protocol)      │  │  (App Group)     │  │ ModelCtx  │  │
│  └────────┬─────────┘  └──────────────────┘  └───────────┘  │
│           │                                                  │
│  ┌────────▼─────────┐                                       │
│  │  URLSessionAPI   │  Uses URLSession + async/await        │
│  │  Client          │                                       │
│  └──────────────────┘                                       │
└─────────────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│  Models                                                     │
│  ┌─────────────────┐  ┌──────────────────┐                  │
│  │  API Models      │  │  Cache Models    │                  │
│  │  (Codable)       │  │  (SwiftData)     │                  │
│  └─────────────────┘  └──────────────────┘                  │
└─────────────────────────────────────────────────────────────┘
```

### Data Flow

1. View model calls `FluxAPIClient` method (e.g. `fetchStatus()`)
2. `URLSessionAPIClient` builds the request with bearer token from `KeychainService`
3. URLSession performs the request, decodes JSON into API models
4. View model receives the decoded model, updates `@Observable` published properties
5. SwiftUI views react to property changes and re-render
6. For history data, the view model writes to SwiftData after a successful fetch

### Navigation Flow

`AppNavigationView` uses NavigationSplitView with `preferredCompactColumn: .detail` so the Dashboard shows immediately on iPhone (no sidebar visible). On iPad, the sidebar lists the three screens (Dashboard, History, Settings) with the detail area showing the selected screen.

```
iPhone (compact):
  Dashboard → push History → push Day Detail
            → push Settings (from toolbar)

iPad (regular):
  ┌──────────┬──────────────────────────┐
  │ Sidebar  │  Detail                  │
  │ ○ Dash   │  (selected screen)       │
  │ ○ History│                          │
  │ ○ Settings│                         │
  └──────────┴──────────────────────────┘
```

On iPhone, History and Day Detail are reached via `NavigationLink` / `navigationDestination` within a `NavigationStack` embedded in the detail column. Settings is accessed via a toolbar button on the Dashboard.

---

## Components and Interfaces

### `FluxApp` — App Entry Point

```swift
@main
struct FluxApp: App {
    var body: some Scene {
        WindowGroup {
            AppNavigationView()
        }
        .modelContainer(for: CachedDayEnergy.self)
    }
}
```

Configures the SwiftData `ModelContainer` and provides the root view.

### Dependency Wiring

View models are created in the views that own them, receiving their dependencies via initializer injection. The `FluxAPIClient` is constructed once in `AppNavigationView` and passed down:

```swift
struct AppNavigationView: View {
    @State private var apiClient: (any FluxAPIClient)?
    
    // Recreate API client when URL changes (e.g. after Settings save)
    private func makeAPIClient() -> (any FluxAPIClient)? {
        guard let urlString = UserDefaults.standard.apiURL,
              let url = URL(string: urlString) else { return nil }
        return URLSessionAPIClient(baseURL: url, keychainService: KeychainService())
    }
}
```

Each view creates its own view model with the shared `apiClient`. `HistoryViewModel` additionally receives the `ModelContext` from the SwiftUI environment. This keeps the dependency graph simple: one API client, one Keychain service, view models scoped to their views.

### `AppNavigationView` — Root Navigation

```swift
struct AppNavigationView: View {
    @State private var selectedScreen: Screen? = .dashboard
    @State private var navigationPath = NavigationPath()
    
    var body: some View {
        NavigationSplitView(preferredCompactColumn: .detail) {
            SidebarView(selection: $selectedScreen)
        } detail: {
            NavigationStack(path: $navigationPath) {
                // Show selected screen based on selectedScreen
            }
            .navigationDestination(for: DayDetailRoute.self) { route in
                DayDetailView(date: route.date)
            }
        }
        .onChange(of: selectedScreen) {
            // Reset navigation stack when switching screens (iPad sidebar).
            // Prevents stale Day Detail routes when switching from History to Dashboard.
            navigationPath = NavigationPath()
        }
    }
}

enum Screen: String, CaseIterable, Identifiable {
    case dashboard, history, settings
    var id: String { rawValue }
}
```

Uses `preferredCompactColumn: .detail` so iPhone shows the Dashboard directly, not the sidebar. The `NavigationStack` in the detail column handles push navigation for History → Day Detail flow. The `onChange(of: selectedScreen)` resets the navigation path when the user switches screens via the sidebar, preventing stale navigation state.

### `FluxAPIClient` — Networking Protocol

```swift
protocol FluxAPIClient: Sendable {
    func fetchStatus() async throws -> StatusResponse
    func fetchHistory(days: Int) async throws -> HistoryResponse
    func fetchDay(date: String) async throws -> DayDetailResponse
}
```

The protocol is `Sendable` for safe use across actor boundaries. Each method throws `FluxAPIError` (see Error Handling section).

### `URLSessionAPIClient` — Production Implementation

```swift
final class URLSessionAPIClient: FluxAPIClient, Sendable {
    private let session: URLSession
    private let baseURL: URL
    private let tokenProvider: @Sendable () -> String?
    private let decoder: JSONDecoder
    
    /// Production initializer — reads token from Keychain on each request.
    init(baseURL: URL, keychainService: KeychainService, session: URLSession = .shared) {
        self.session = session
        self.baseURL = baseURL
        self.tokenProvider = { keychainService.loadToken() }
        self.decoder = JSONDecoder()
    }
    
    /// Validation initializer — uses an explicit token (for Settings validation
    /// before the token has been saved to Keychain).
    init(baseURL: URL, token: String, session: URLSession = .shared) {
        self.session = session
        self.baseURL = baseURL
        self.tokenProvider = { token }
        self.decoder = JSONDecoder()
    }
}
```

The `tokenProvider` closure abstracts where the token comes from. The production initializer reads from Keychain; the validation initializer uses the token the user just entered. This solves the bootstrap problem where Settings needs to validate a token before storing it.

Responsibilities:
- Build URL requests with `Authorization: Bearer {token}` header (token from `tokenProvider`)
- Perform requests via `URLSession.data(for:)`
- Decode successful responses (HTTP 200) into Codable models
- Map non-200 responses to typed errors by decoding the `{"error": "message"}` body
- Catch and wrap all errors internally — `URLSession` failures become `FluxAPIError.networkError`, JSON decoding failures become `FluxAPIError.decodingError` — so callers never see raw `URLError` or `DecodingError`

The `decoder` uses default settings — the backend's JSON keys are already camelCase, matching Swift's default Codable behaviour.

### `KeychainService` — Credential Storage

```swift
final class KeychainService: Sendable {
    private let accessGroup: String?  // App Group identifier for widget sharing
    
    func saveToken(_ token: String) throws
    func loadToken() -> String?
    func deleteToken() throws
}
```

Wraps the Security framework's `SecItemAdd`/`SecItemCopyMatching`/`SecItemDelete` for the API token. Uses `kSecAttrAccessGroup` with the App Group identifier so a future widget extension can read the token.

The API URL is stored in `UserDefaults` (not Keychain) since it's not sensitive:

```swift
extension UserDefaults {
    var apiURL: String? {
        get { string(forKey: "apiURL") }
        set { set(newValue, forKey: "apiURL") }
    }
    
    var loadAlertThreshold: Double {
        get { double(forKey: "loadAlertThreshold").nonZero ?? 3000 }
        set { set(newValue, forKey: "loadAlertThreshold") }
    }
}
```

### `DashboardViewModel`

```swift
@MainActor @Observable
final class DashboardViewModel {
    private(set) var status: StatusResponse?
    private(set) var lastSuccessfulFetch: Date?
    private(set) var error: FluxAPIError?
    private(set) var isLoading = false
    
    private let apiClient: FluxAPIClient
    private var refreshTask: Task<Void, Never>?
    
    func startAutoRefresh()
    func stopAutoRefresh()
    func refresh() async
}
```

`@MainActor` ensures all property mutations happen on the main thread, which is required for `@Observable` to safely drive SwiftUI view updates.

Manages the 10-second auto-refresh cycle. `startAutoRefresh()` cancels any existing `refreshTask` before launching a new `Task` that loops with `try await Task.sleep(for: .seconds(10))`. This prevents duplicate timer tasks from rapid foreground/background transitions. `refresh()` guards against concurrent calls by checking `isLoading` and returning early if a fetch is already in-flight. The view calls `startAutoRefresh()` on appear and `stopAutoRefresh()` on disappear. Scene phase changes (background/foreground) are observed in the view and forwarded to the view model.

The `DashboardView` uses `.refreshable { await viewModel.refresh() }` on its `ScrollView` for pull-to-refresh support (requirement 8.2). Pull-to-refresh shares the same `refresh()` method as auto-refresh, and the `isLoading` guard prevents overlap.

When a fetch fails, the view model keeps the previous `status` value and sets `error` — this gives the view both stale data and the error state. The staleness indicator is shown when `error != nil && status != nil` (stale data exists and the current fetch failed). The indicator displays `lastSuccessfulFetch` as relative time (e.g. "Last updated 2 minutes ago") so the user knows how old the displayed data is.

### `HistoryViewModel`

```swift
@MainActor @Observable
final class HistoryViewModel {
    private(set) var days: [DayEnergy] = []
    private(set) var selectedDay: DayEnergy?
    private(set) var selectedDayRange: Int = 7
    private(set) var isLoading = false
    private(set) var error: FluxAPIError?
    
    private let apiClient: FluxAPIClient
    private let modelContext: ModelContext
    
    func loadHistory(days: Int) async
    func selectDay(_ day: DayEnergy)
}
```

`@MainActor` is required because `ModelContext` is not `Sendable` and must be used on the actor it was created on. Since all SwiftUI view updates also happen on the main actor, this is consistent.

On `loadHistory`:
1. Fetch the full range from the `/history` endpoint
2. On success: update `days` array, write historical days (before today) to SwiftData cache using `modelContext`
3. On network failure: fall back to SwiftData cache for previously fetched days (may be incomplete for the requested range)
4. On first launch with no cache and no network: set `error`, show inline error with retry button

"Today" is determined using `DateFormatting.sydneyTimeZone` — the same timezone the backend uses — not the device's local timezone. This ensures the "today" boundary matches the backend's date-keyed records.

When no days array exists for the selected range but the user switches to a previously empty range and the chart would be empty, the view shows a "No data available" placeholder.

### `DayDetailViewModel`

```swift
@MainActor @Observable
final class DayDetailViewModel {
    private(set) var date: String  // YYYY-MM-DD
    private(set) var readings: [TimeSeriesPoint] = []
    private(set) var summary: DaySummary?
    private(set) var isLoading = false
    private(set) var error: FluxAPIError?
    private(set) var hasPowerData = true  // false when showing fallback data
    
    private let apiClient: FluxAPIClient
    
    init(date: String, apiClient: FluxAPIClient)
    
    func loadDay() async
    func navigatePrevious()
    func navigateNext()
}
```

`navigatePrevious()` and `navigateNext()` update the `date` property synchronously. The view uses `.task(id: viewModel.date)` to trigger an async `loadDay()` whenever the date changes. This pattern automatically cancels the previous fetch when navigating quickly through days:

```swift
DayDetailContent(viewModel: viewModel)
    .task(id: viewModel.date) {
        await viewModel.loadDay()
    }
```

The forward arrow is disabled when the current date is today (using `DateFormatting.sydneyTimeZone` for "today" determination). There is no lower bound on backward navigation — days with no data show an empty state gracefully (empty readings, null summary per requirement 10.8).

**Fallback data detection:** When all readings in the response have `ppv == 0 && pload == 0 && pbat == 0 && pgrid == 0` and `soc` varies, the data is from `flux-daily-power` fallback (5-minute SOC-only data). The view model sets `hasPowerData = false`, and the view renders only the SOC chart, leaving the power charts empty (requirement 10.8).

### `SettingsViewModel`

```swift
@MainActor @Observable
final class SettingsViewModel {
    var apiURL: String = ""
    var apiToken: String = ""
    var loadAlertThreshold: Double = 3000
    private(set) var isValidating = false
    private(set) var validationError: String?
    private(set) var isConfigured: Bool
    private(set) var shouldDismiss = false  // Observed by view for navigation
    
    private let keychainService: KeychainService
    
    func save() async
    func loadExisting()
}
```

On `save()`:
1. Capture current values of `apiURL` and `apiToken` at the start (prevents mutation during validation)
2. Build a temporary `URLSessionAPIClient` using the `init(baseURL:token:)` initializer — this passes the entered token directly, bypassing Keychain (which doesn't have the new token yet)
3. Call `fetchStatus()` to validate connectivity
4. If successful: store token in Keychain, URL in UserDefaults, set `shouldDismiss = true`
5. If failed: set `validationError`, leave credentials unchanged

The view observes `shouldDismiss` to trigger navigation away from Settings. Using an observable property instead of a return value avoids the need for `Task { }` wrappers in button actions to handle the result.

---

## Data Models

### API Models (Codable)

These mirror the backend JSON exactly. All field names match the backend's camelCase JSON tags, so no custom `CodingKeys` are needed.

```swift
// MARK: - /status response

struct StatusResponse: Codable, Sendable {
    let live: LiveData?
    let battery: BatteryInfo?
    let rolling15min: RollingAvg?
    let offpeak: OffpeakData?
    let todayEnergy: TodayEnergy?
}

struct LiveData: Codable, Sendable {
    let ppv: Double
    let pload: Double
    let pbat: Double
    let pgrid: Double
    let pgridSustained: Bool
    let soc: Double
    let timestamp: String
}

struct BatteryInfo: Codable, Sendable {
    let capacityKwh: Double
    let cutoffPercent: Int
    let estimatedCutoffTime: String?
    let low24h: Low24h?
}

struct Low24h: Codable, Sendable {
    let soc: Double
    let timestamp: String
}

struct RollingAvg: Codable, Sendable {
    let avgLoad: Double
    let avgPbat: Double
    let estimatedCutoffTime: String?
}

struct OffpeakData: Codable, Sendable {
    let windowStart: String
    let windowEnd: String
    let gridUsageKwh: Double?
    let solarKwh: Double?
    let batteryChargeKwh: Double?
    let batteryDischargeKwh: Double?
    let gridExportKwh: Double?
    let batteryDeltaPercent: Double?
}

struct TodayEnergy: Codable, Sendable {
    let epv: Double
    let eInput: Double
    let eOutput: Double
    let eCharge: Double
    let eDischarge: Double
}

// MARK: - /history response

struct HistoryResponse: Codable, Sendable {
    let days: [DayEnergy]
}

struct DayEnergy: Codable, Sendable, Identifiable {
    let date: String
    let epv: Double
    let eInput: Double
    let eOutput: Double
    let eCharge: Double
    let eDischarge: Double
    
    var id: String { date }
}

// MARK: - /day response

struct DayDetailResponse: Codable, Sendable {
    let date: String
    let readings: [TimeSeriesPoint]
    let summary: DaySummary?
}

struct TimeSeriesPoint: Codable, Sendable, Identifiable {
    let timestamp: String
    let ppv: Double
    let pload: Double
    let pbat: Double
    let pgrid: Double
    let soc: Double
    
    var id: String { timestamp }
}

struct DaySummary: Codable, Sendable {
    let epv: Double?
    let eInput: Double?
    let eOutput: Double?
    let eCharge: Double?
    let eDischarge: Double?
    let socLow: Double?
    let socLowTime: String?
}

// MARK: - Error response

struct APIErrorResponse: Codable, Sendable {
    let error: String
}
```

### Timestamp Parsing

API timestamps are ISO 8601 UTC strings (e.g. `"2026-04-11T21:47:00Z"`). A shared `DateFormatting` utility provides parsing and formatting:

```swift
enum DateFormatting {
    /// The backend uses Australia/Sydney for all date operations.
    /// The app must use the same timezone for date comparisons, "today"
    /// determination, and off-peak window checks.
    static let sydneyTimeZone = TimeZone(identifier: "Australia/Sydney")!
    
    private static var sydneyCalendar: Calendar {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = sydneyTimeZone
        return cal
    }
    
    // Shared formatter — ISO8601DateFormatter is expensive to construct,
    // so a single static instance is reused for all timestamp parsing.
    private static let isoFormatter = ISO8601DateFormatter()
    
    static func parseTimestamp(_ string: String) -> Date? {
        isoFormatter.date(from: string)
    }
    
    static func clockTime(from date: Date) -> String {
        date.formatted(.dateTime.hour().minute().timeZone(sydneyTimeZone))
    }
    
    /// Today's date string in YYYY-MM-DD format, in Sydney timezone.
    static func todayDateString(now: Date = .now) -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.timeZone = sydneyTimeZone
        return formatter.string(from: now)
    }
    
    /// Parse off-peak window time strings ("11:00", "14:00") into
    /// today's Date in Sydney timezone for comparison.
    static func parseWindowTime(_ timeString: String, on date: Date = .now) -> Date? {
        let parts = timeString.split(separator: ":")
        guard parts.count == 2,
              let hour = Int(parts[0]),
              let minute = Int(parts[1]) else { return nil }
        return sydneyCalendar.date(bySettingHour: hour, minute: minute, second: 0, of: date)
    }
    
    /// Check if the current time falls within the off-peak window (Sydney time).
    static func isInOffpeakWindow(start: String, end: String, now: Date = .now) -> Bool {
        guard let startDate = parseWindowTime(start, on: now),
              let endDate = parseWindowTime(end, on: now) else { return false }
        return now >= startDate && now < endDate
    }
    
    /// Check if a date string (YYYY-MM-DD) represents today in Sydney timezone.
    static func isToday(_ dateString: String, now: Date = .now) -> Bool {
        dateString == todayDateString(now: now)
    }
}
```

Cutoff times and the 24h low timestamp are displayed as clock times using `DateFormatting.clockTime(from:)`. Off-peak window comparison uses `DateFormatting.isInOffpeakWindow(start:end:)` for grid conditional colouring.

### SwiftData Cache Model

```swift
@Model
final class CachedDayEnergy {
    @Attribute(.unique) var date: String
    var epv: Double
    var eInput: Double
    var eOutput: Double
    var eCharge: Double
    var eDischarge: Double
    
    init(from dayEnergy: DayEnergy) {
        self.date = dayEnergy.date
        self.epv = dayEnergy.epv
        self.eInput = dayEnergy.eInput
        self.eOutput = dayEnergy.eOutput
        self.eCharge = dayEnergy.eCharge
        self.eDischarge = dayEnergy.eDischarge
    }
    
    var asDayEnergy: DayEnergy {
        DayEnergy(date: date, epv: epv, eInput: eInput,
                  eOutput: eOutput, eCharge: eCharge, eDischarge: eDischarge)
    }
}
```

The `@Attribute(.unique)` constraint on `date` prevents duplicate entries. The `date` field uses `YYYY-MM-DD` format as a natural key.

Only `CachedDayEnergy` is persisted in SwiftData. Dashboard status, day detail readings, and off-peak data are transient — fetched fresh each time and held in view model memory.

---

## View Design

### DashboardView

```
┌─────────────────────────────┐
│        Battery Hero          │
│    ┌───────────────────┐    │
│    │      62.4%         │    │
│    │ Discharging · ~4AM │    │
│    │ ████████████░░░░░░ │    │
│    └───────────────────┘    │
│                              │
│  ┌────────┬────────┬──────┐ │
│  │ Solar  │  Load  │ Grid │ │
│  │ 2.4kW  │  207W  │  -9W │ │
│  │(green) │(default)│(grn) │ │
│  └────────┴────────┴──────┘ │
│                              │
│  Secondary Stats             │
│  24h Low: 38.2% at 6:45 PM │
│  Off-peak grid: 6.1 kWh    │
│  Off-peak Δ battery: +42.3%│
│  15m avg load: 243W (~3 AM)│
│                              │
│  Today                       │
│  Solar: 14.3 kWh            │
│  Grid in: 0.25 kWh          │
│  Grid out: 5.94 kWh         │
│  Charged: 5.7 kWh           │
│  Discharged: 6.8 kWh        │
│                              │
│  View history →              │
└─────────────────────────────┘
```

The view is a `ScrollView` with a `VStack`. The battery hero and power trio are custom views. The secondary stats and today sections use `GroupBox` or `Section` within a `List`-style layout for Liquid Glass grouping.

**Conditional colouring logic** is encapsulated in a `BatteryColor` helper:

```swift
enum BatteryColor {
    static func forSOC(_ soc: Double) -> Color {
        if soc > 60 { return .green }       // Requirement 4.2: green when >60%
        if soc >= 30 { return .primary }     // 30-60%: default
        if soc >= 15 { return .orange }      // 15-30%: amber
        return .red                           // <15%: red
    }
}
```

Grid colouring depends on three conditions: `pgrid > 500`, `pgridSustained == true`, and the current time being outside the off-peak window. The off-peak window times come from the `/status` response (`offpeak.windowStart` / `offpeak.windowEnd`).

**Cutoff time colouring:**
- Red: cutoff is less than 2 hours from now
- Amber: cutoff is before `offpeak.windowStart` today (battery won't last until free charging)
- Default: otherwise

### HistoryView

Uses SwiftUI Charts `Chart` with `BarMark` and `.position(by:)` for grouped bars:

```swift
Chart(chartData) { item in
    BarMark(
        x: .value("Date", item.dayID),
        y: .value("kWh", item.value)
    )
    .foregroundStyle(by: .value("Metric", item.metric))
    .position(by: .value("Metric", item.metric))
    .opacity(item.isToday ? 0.5 : 1.0)
}
```

The x-axis binds to `item.dayID` — the YYYY-MM-DD String — rather than the parsed `Date`. `.position(by:)` subdivides the x-slot for each category into one sub-bar per metric, which requires a discrete axis; binding a continuous `Date` collapses every bar to a near-zero width and hides non-today data (T-841).

The `chartData` is a flattened array where each `DayEnergy` becomes 5 rows (one per metric: solar, grid imported, grid exported, battery charged, battery discharged). The `.position(by:)` modifier creates side-by-side grouped bars within each date. This transformation is computed once in the view model (as a computed property from `days`) and cached — not recomputed on every view body evaluation.

A `Picker` with `.pickerStyle(.segmented)` provides the 7/14/30 day range selector. This gets Liquid Glass styling automatically on iOS 26.

Day selection uses `.chartOverlay` with a `DragGesture` to detect taps. The x-position is resolved to a `dayID` string via `proxy.value(atX:, as: String.self)`, then matched back to the corresponding `HistoryChartDay`. The selected day gets a subtle background highlight via a `RectangleMark` rendered behind the bar group:

```swift
if let selected = viewModel.selectedDay {
    RectangleMark(x: .value("Date", selected.date))
        .foregroundStyle(.gray.opacity(0.1))
}
```

Below the chart, a summary card shows exact kWh values for the selected day. A "View day detail" link navigates to `DayDetailView`.

### DayDetailView

Three stacked charts in a `ScrollView`:

**Chart 1 — Battery SOC (filled area):**

```swift
Chart {
    ForEach(readings) { point in
        AreaMark(
            x: .value("Time", point.parsedTimestamp),
            y: .value("SOC", point.soc)
        )
        .foregroundStyle(.blue.opacity(0.3))
    }
    
    // 10% cutoff threshold
    RuleMark(y: .value("Cutoff", 10))
        .lineStyle(StrokeStyle(dash: [5, 3]))
        .foregroundStyle(.red.opacity(0.5))
    
    // Low point annotation
    if let low = summary?.socLow, let lowTime = summary?.socLowTime?.asDate {
        PointMark(
            x: .value("Time", lowTime),
            y: .value("SOC", low)
        )
        .annotation(position: .top) {
            Text("\(low, specifier: "%.1f")%")
                .font(.caption2)
        }
    }
}
.chartYScale(domain: 0...100)
```

**Chart 2 — Power flows (multi-line + area):**

```swift
Chart {
    ForEach(readings) { point in
        // Solar as filled area
        AreaMark(
            x: .value("Time", point.parsedTimestamp),
            yStart: .value("Power", 0),
            yEnd: .value("Power", point.ppv)
        )
        .foregroundStyle(.green.opacity(0.3))
        
        // Load as dark line
        LineMark(
            x: .value("Time", point.parsedTimestamp),
            y: .value("Power", point.pload)
        )
        .foregroundStyle(.primary)
        .series(key: "Load")
    }
    // Grid import and export as separate series with red/blue lines
}
```

**Chart 3 — Battery power (charge/discharge):**

```swift
Chart {
    ForEach(readings) { point in
        // Positive = charging, negative = discharging
        // Note: API uses opposite convention (positive = discharge)
        // so negate pbat for this chart
        LineMark(
            x: .value("Time", point.parsedTimestamp),
            y: .value("Power", -point.pbat)
        )
    }
    
    // Zero line
    RuleMark(y: .value("Zero", 0))
        .foregroundStyle(.secondary)
}
```

**`pbat` sign convention for charts:** The API uses positive `pbat` = discharging, negative = charging. For Chart 3, values are negated (`-point.pbat`) so that charging (battery receiving energy) appears above zero and discharging appears below, matching user expectation. This negation is applied only in `BatteryPowerChartView` — the API models and view models always use the API convention.

**Day navigation** uses left/right buttons in a toolbar or header:

```swift
HStack {
    Button(action: viewModel.navigatePrevious) {
        Image(systemName: "chevron.left")
    }
    Text(viewModel.formattedDate)  // "Tuesday, 10 Apr"
    Button(action: viewModel.navigateNext) {
        Image(systemName: "chevron.right")
    }
    .disabled(viewModel.isToday)
}
```

All three charts use `.chartXAxis` with hourly tick marks at 3-hour intervals (00:00, 03:00, 06:00, 09:00, 12:00, 15:00, 18:00, 21:00, 00:00) formatted with `.hour()` date style:

```swift
.chartXAxis {
    AxisMarks(values: .stride(by: .hour, count: 3)) { _ in
        AxisGridLine()
        AxisValueLabel(format: .dateTime.hour())
    }
}
```

### SettingsView

A `Form` with grouped sections:

```swift
Form {
    Section("Backend") {
        TextField("API URL", text: $viewModel.apiURL)
            .textContentType(.URL)
            .autocapitalization(.none)
        SecureField("API Token", text: $viewModel.apiToken)
    }
    
    Section("Display") {
        HStack {
            Text("Load alert threshold")
            TextField("Watts", value: $viewModel.loadAlertThreshold, format: .number)
                .keyboardType(.numberPad)
                .multilineTextAlignment(.trailing)
        }
    }
    
    Section {
        Button("Save") { Task { await viewModel.save() } }
            .disabled(viewModel.isValidating || viewModel.apiURL.isEmpty || viewModel.apiToken.isEmpty)
    }
    
    if let error = viewModel.validationError {
        Section { Text(error).foregroundStyle(.red) }
    }
}
.onChange(of: viewModel.shouldDismiss) {
    if viewModel.shouldDismiss { dismiss() }
}
```

The `Form` receives Liquid Glass styling automatically on iOS 26.

---

## Error Handling

### Error Types

```swift
enum FluxAPIError: Error, Sendable {
    case notConfigured              // No API URL or token
    case unauthorized               // HTTP 401
    case badRequest(String)         // HTTP 400 with error message
    case serverError                // HTTP 500
    case networkError(String)       // URLSession failure (description, not raw Error — Sendable)
    case decodingError(String)      // JSON decoding failure (description)
    case unexpectedStatus(Int)      // Other non-200 status
}
```

Error payloads use `String` descriptions instead of raw `Error` values because `Error` is not `Sendable`. This is required for strict concurrency checking under iOS 26.

### API Client Error Mapping

```swift
func performRequest<T: Decodable>(_ request: URLRequest) async throws(FluxAPIError) -> T {
    guard let token = tokenProvider() else {
        throw .notConfigured
    }
    
    var authedRequest = request
    authedRequest.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    
    let data: Data
    let response: URLResponse
    do {
        (data, response) = try await session.data(for: authedRequest)
    } catch {
        throw .networkError(error.localizedDescription)
    }
    
    guard let httpResponse = response as? HTTPURLResponse else {
        throw .unexpectedStatus(0)
    }
    
    switch httpResponse.statusCode {
    case 200:
        do {
            return try decoder.decode(T.self, from: data)
        } catch {
            throw .decodingError(error.localizedDescription)
        }
    case 401:
        throw .unauthorized
    case 400:
        let errorBody = try? decoder.decode(APIErrorResponse.self, from: data)
        throw .badRequest(errorBody?.error ?? "Bad request")
    case 500:
        throw .serverError
    default:
        throw .unexpectedStatus(httpResponse.statusCode)
    }
}
```

All error wrapping is centralized in `performRequest`. Callers never see raw `URLError` or `DecodingError` — they always receive `FluxAPIError`. Uses typed throws (`throws(FluxAPIError)`) so the compiler enforces exhaustive error handling at call sites.

### View-Level Error Handling

Each view model holds an optional `error` property. Views display errors contextually:

- **Dashboard**: shows stale data with a banner indicating when data was last refreshed. Tapping the banner offers "Retry" and "Settings" options.
- **History/Day Detail**: shows an inline error message with a retry button.
- **Settings validation**: shows the error message below the save button.

`FluxAPIError.unauthorized` triggers a prompt to navigate to Settings on any screen.

`FluxAPIError.notConfigured` is checked on app launch — if no token exists, the app redirects to Settings before showing the Dashboard.

---

## Testing Strategy

### Unit Tests

**API Models — Decoding Tests (`APIModelsTests.swift`):**

- Decode a full `/status` JSON response with all fields populated
- Decode `/status` with null optional fields (`live`, `rolling15min`, `todayEnergy`, `low24h`, off-peak delta fields)
- Decode `/history` with empty `days` array
- Decode `/day` with null `summary`
- Decode `/day` with partial summary (energy fields null, socLow present)
- Decode error response `{"error": "message"}`
- Verify all JSON keys match backend expectations (no encoding mismatches)

**URLSessionAPIClient Tests (`APIClientTests.swift`):**

Use `URLProtocol` subclass to mock HTTP responses.

- `fetchStatus` with 200 response returns decoded `StatusResponse`
- `fetchHistory(days: 7)` builds correct URL with query parameter
- `fetchDay(date:)` builds correct URL with date query parameter
- Bearer token is included in Authorization header
- HTTP 401 throws `FluxAPIError.unauthorized`
- HTTP 400 throws `FluxAPIError.badRequest` with error message from body
- HTTP 500 throws `FluxAPIError.serverError`
- Network failure throws `FluxAPIError.networkError`
- Invalid JSON throws `FluxAPIError.decodingError`
- Missing token throws `FluxAPIError.notConfigured`

**DashboardViewModel Tests (`DashboardViewModelTests.swift`):**

Use a mock `FluxAPIClient` (protocol conformance with closures).

- `refresh()` updates `status` on success
- `refresh()` preserves previous `status` and sets `error` on failure
- `refresh()` updates `lastSuccessfulFetch` on success, doesn't update on failure
- `refresh()` skips fetch when `isLoading` is true (no concurrent fetches)
- `startAutoRefresh()` triggers periodic fetches (verify with mock)
- `startAutoRefresh()` called twice does not create duplicate timer tasks
- `stopAutoRefresh()` cancels the refresh task

**HistoryViewModel Tests (`HistoryViewModelTests.swift`):**

- `loadHistory(days: 7)` fetches from API and populates `days`
- `selectDay` updates `selectedDay`
- Historical days are written to SwiftData cache on successful fetch
- Network failure with cached data: falls back to SwiftData cache
- Network failure with no cache: sets `error`
- `isToday` determination uses Sydney timezone, not device timezone

**DayDetailViewModel Tests (`DayDetailViewModelTests.swift`):**

- `loadDay` fetches from API and populates `readings` and `summary`
- `navigatePrevious` decrements date and reloads
- `navigateNext` increments date and reloads
- `navigateNext` is disabled when date is today

**SettingsViewModel Tests (`SettingsViewModelTests.swift`):**

- `save()` with valid URL and token calls API using the entered token (not Keychain) and stores credentials
- `save()` with unreachable backend sets `validationError`, does not modify Keychain
- `save()` captures URL and token at start — mutation during validation doesn't affect stored values
- `save()` sets `shouldDismiss` to true on success
- `loadExisting()` populates fields from Keychain and UserDefaults

**KeychainService Tests (`KeychainServiceTests.swift`):**

- `saveToken` + `loadToken` round-trip
- `deleteToken` removes the stored token
- `loadToken` returns nil when no token exists

**DateFormatting Tests (`DateFormattingTests.swift`):**

- `parseTimestamp`: valid ISO 8601 string returns correct `Date`
- `parseTimestamp`: invalid string returns nil
- `todayDateString`: returns correct date in Sydney timezone (test with a date that's different in Sydney vs UTC)
- `isToday`: matches for same date, returns false for yesterday
- `parseWindowTime`: "11:00" returns correct Date in Sydney timezone
- `parseWindowTime`: invalid format returns nil
- `isInOffpeakWindow`: inside window returns true, outside returns false, at exact boundary (start inclusive, end exclusive)
- All date operations use Sydney timezone, not device timezone

**Conditional Colouring Tests (`ColoringTests.swift`):**

- `BatteryColor.forSOC`: verify boundaries (0, 14.9, 15, 29.9, 30, 60, 60.1, 100)
- Grid colouring: verify all combinations of pgrid threshold, sustained flag, and off-peak window
- Cutoff time colouring: verify red (<2h), amber (before off-peak), default

### Mock API Client

```swift
final class MockFluxAPIClient: FluxAPIClient {
    var fetchStatusResult: Result<StatusResponse, Error> = .failure(FluxAPIError.notConfigured)
    var fetchHistoryResult: Result<HistoryResponse, Error> = .failure(FluxAPIError.notConfigured)
    var fetchDayResult: Result<DayDetailResponse, Error> = .failure(FluxAPIError.notConfigured)
    
    func fetchStatus() async throws -> StatusResponse {
        try fetchStatusResult.get()
    }
    
    func fetchHistory(days: Int) async throws -> HistoryResponse {
        try fetchHistoryResult.get()
    }
    
    func fetchDay(date: String) async throws -> DayDetailResponse {
        try fetchDayResult.get()
    }
}
```

### UI Tests

UI tests are deferred to V2. The testing strategy focuses on unit-testable logic (view models, API client, conditional colouring) since the views are thin SwiftUI layers that delegate all logic to view models.

---

## File Layout

The Xcode project lives at `Flux/` in the repo root. App source files are under `Flux/Flux/` (the app target directory). Tests are under `Flux/FluxTests/`.

```
Flux/
├── Flux/                                # App target
│   ├── FluxApp.swift                    # App entry point, ModelContainer setup
│   ├── Navigation/
│   │   ├── AppNavigationView.swift      # NavigationSplitView root
│   │   ├── SidebarView.swift            # Screen list (iPad)
│   │   └── Screen.swift                 # Screen enum
│   ├── Dashboard/
│   │   ├── DashboardView.swift          # Main dashboard layout
│   │   ├── DashboardViewModel.swift     # Status fetching, auto-refresh
│   │   ├── BatteryHeroView.swift        # SOC, status line, progress bar
│   │   ├── PowerTrioView.swift          # Solar / Load / Grid columns
│   │   ├── SecondaryStatsView.swift     # 24h low, off-peak, rolling avg
│   │   └── TodayEnergyView.swift        # Today's kWh totals
│   ├── History/
│   │   ├── HistoryView.swift            # Bar chart + summary card
│   │   ├── HistoryViewModel.swift       # Fetch, cache, selection
│   │   └── HistoryChartView.swift       # Chart rendering
│   ├── DayDetail/
│   │   ├── DayDetailView.swift          # Three stacked charts + summary
│   │   ├── DayDetailViewModel.swift     # Fetch, day navigation
│   │   ├── SOCChartView.swift           # Battery SOC area chart
│   │   ├── PowerChartView.swift         # Multi-line power chart
│   │   └── BatteryPowerChartView.swift  # Charge/discharge chart
│   ├── Settings/
│   │   ├── SettingsView.swift           # Form with URL, token, threshold
│   │   └── SettingsViewModel.swift      # Validation, save
│   ├── Services/
│   │   ├── FluxAPIClient.swift          # Protocol
│   │   ├── URLSessionAPIClient.swift    # Production implementation
│   │   └── KeychainService.swift        # Keychain wrapper
│   ├── Models/
│   │   ├── APIModels.swift              # Codable structs (all endpoints)
│   │   ├── CachedDayEnergy.swift        # SwiftData model (replaces sample Item.swift)
│   │   └── FluxAPIError.swift           # Error enum
│   ├── Helpers/
│   │   ├── BatteryColor.swift           # SOC colour helper
│   │   ├── DateFormatting.swift         # Timestamp parsing, clock time
│   │   └── GridColor.swift              # Grid conditional colouring
│   ├── Assets.xcassets/
│   ├── Flux.entitlements
│   └── Info.plist
├── FluxTests/                           # Unit test target
│   ├── KeychainServiceTests.swift
│   ├── URLSessionAPIClientTests.swift
│   ├── DateFormattingTests.swift
│   ├── ColoringTests.swift
│   ├── DashboardViewModelTests.swift
│   ├── HistoryViewModelTests.swift
│   ├── DayDetailViewModelTests.swift
│   ├── SettingsViewModelTests.swift
│   └── APIModelsTests.swift
├── FluxUITests/                         # UI test target (deferred to V2)
└── Flux.xcodeproj/
```

The sample files from Xcode project creation (`ContentView.swift`, `Item.swift`) will be replaced during implementation. `FluxApp.swift` will be modified in-place.

---

## Requirement Traceability

| Requirement | Design Element |
|-------------|---------------|
| 1.1–1.3 (Platform) | FluxApp, SwiftUI-only views, SwiftUI Charts |
| 1.4 (NavigationSplitView) | AppNavigationView with preferredCompactColumn |
| 1.5 (Shared layer) | Services/, Models/ — no SwiftUI imports |
| 1.6 (Liquid Glass) | Form, List, Picker, NavigationSplitView — native iOS 26 adoption |
| 1.7 (Keychain App Group) | KeychainService with kSecAttrAccessGroup |
| 1.8 (SwiftData) | CachedDayEnergy model, ModelContainer in FluxApp |
| 2.1–2.7 (API Client) | FluxAPIClient protocol, URLSessionAPIClient |
| 3.1–3.8 (Settings) | SettingsView, SettingsViewModel, KeychainService |
| 4.1–4.7 (Battery Hero) | BatteryHeroView, BatteryColor helper |
| 5.1–5.8 (Power Readings) | PowerTrioView, GridColor helper |
| 6.1–6.8 (Secondary Stats) | SecondaryStatsView, cutoff time colouring |
| 7.1–7.4 (Today's Energy) | TodayEnergyView |
| 8.1–8.5 (Refresh) | DashboardViewModel auto-refresh, scene phase observation |
| 9.1–9.8 (History) | HistoryView, HistoryChartView, HistoryViewModel |
| 10.1–10.9 (Day Detail) | DayDetailView, three chart views, DayDetailViewModel |
| 11.1–11.4 (Caching) | CachedDayEnergy, HistoryViewModel cache logic |
| 12.1–12.4 (Error States) | FluxAPIError, view model error handling, staleness indicator |
| 13.1–13.5 (Navigation) | AppNavigationView, Screen enum, SidebarView |
