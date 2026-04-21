# Design: Add Widgets (T-843)

## Overview

This feature adds WidgetKit-based home-screen and lock-screen widgets to the Flux iOS app. Widgets mirror the top of the Dashboard — battery state (SOC, charge/discharge status, cutoff time) and, where space allows, the solar/load/grid power trio. Data comes from a shared App Group `UserDefaults` cache written by the app; the widget also attempts a direct `/status` fetch against the Lambda on every timeline refresh, falling back to the cache on failure.

The design is shaped by three hard constraints from WidgetKit on iOS 26:

1. **No SwiftData / no sharing of the main app's SwiftData store.** The widget extension runs in a separate sandbox with a different container; we therefore move shared types into a local Swift Package (`FluxCore`) and use App Group `UserDefaults` as the cache transport.
2. **Refresh budget ~40–70 reloads/day across all widgets.** The timeline uses a 30-minute `.after` cadence, paired with app-triggered reloads via `WidgetCenter.shared.reloadAllTimelines()` and TimelineEntryRelevance hints.
3. **Lock-screen widgets run while the device is locked.** The existing Keychain item's accessibility class is migrated to `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` so the widget can read the bearer token when locked.

The rest of the app's architecture is unchanged. The existing Dashboard, History, Day Detail, and Settings screens, their view models, networking, SwiftData caching, and auto-refresh behavior are preserved intact — the only app-side behavioral change is a post-refresh cache write and a `WidgetCenter.reloadAllTimelines()` call.

---

## Architecture

### High-level component diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                                                                     │
│  ┌───────────────────────────┐       ┌──────────────────────────┐   │
│  │  Flux (main app target)   │       │  FluxWidgets (extension) │   │
│  │                           │       │                          │   │
│  │  ┌─────────────────────┐  │       │  ┌────────────────────┐  │   │
│  │  │ FluxApp + Views     │  │       │  │ WidgetBundle       │  │   │
│  │  │   DashboardVM       │  │       │  │   FluxBatteryW     │  │   │
│  │  │   HistoryVM         │  │       │  │   FluxAccessoryW   │  │   │
│  │  │   DayDetailVM       │  │       │  └─────────┬──────────┘  │   │
│  │  │   SettingsVM        │  │       │            │             │   │
│  │  └──────────┬──────────┘  │       │  ┌─────────▼──────────┐  │   │
│  │             │             │       │  │ StatusTimelineProv │  │   │
│  │  ┌──────────▼──────────┐  │       │  └─────────┬──────────┘  │   │
│  │  │ KeychainMigrator    │  │       │            │             │   │
│  │  │ SettingsMigrator    │  │       │            │             │   │
│  │  └─────────────────────┘  │       │            │             │   │
│  └──────────────┬────────────┘       └────────────┼─────────────┘   │
│                 │                                 │                 │
│                 │         depends on              │                 │
│                 ▼                                 ▼                 │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  FluxCore (local Swift Package, iOS 26.4+)                  │    │
│  │                                                             │    │
│  │   Models:   APIModels, FluxAPIError                         │    │
│  │   Networking: FluxAPIClient protocol, URLSessionAPIClient   │    │
│  │   Security: KeychainService                                 │    │
│  │   Helpers:  DateFormatting, PowerFormatting,                │    │
│  │             BatteryColor, GridColor, CutoffTimeColor,       │    │
│  │             ColorTier                                       │    │
│  │   Widget:   WidgetSnapshotCache, StalenessClassifier,       │    │
│  │             StatusSnapshotEnvelope                          │    │
│  └──────────┬──────────────────────────────────────┬───────────┘    │
│             │                                      │                │
│             │ reads/writes                         │ reads          │
│             ▼                                      ▼                │
│  ┌──────────────────────────┐       ┌──────────────────────────┐    │
│  │  App Group UserDefaults  │       │    Shared Keychain       │    │
│  │  group.me.nore.ig.flux   │       │   (AfterFirstUnlock...)  │    │
│  │                          │       │                          │    │
│  │  • widgetSnapshotV1      │       │  • api-token             │    │
│  │  • settingsMigrationVer  │       │                          │    │
│  │  • loadAlertThreshold    │       │                          │    │
│  │  • apiURL                │       │                          │    │
│  └──────────────────────────┘       └──────────────────────────┘    │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Data flow — widget timeline refresh

```
 iOS WidgetKit                                StatusTimelineProvider
  │
  │ getTimeline(in:)
  ├────────────────────────────────────────────►│
  │                                              │
  │                                              │ ① Read cache (envelope or nil)
  │                                              │ ② Token = KeychainService.loadToken()
  │                                              │
  │                                              │ ③ if token present:
  │                                              │      async-with-timeout(5s):
  │                                              │          try URLSessionAPIClient.fetchStatus()
  │                                              │      on success:
  │                                              │          envelope = Envelope(fetchedAt: now, status:...)
  │                                              │          cache.writeIfNewer(envelope)
  │                                              │
  │                                              │ ④ final = live ?? cache ?? placeholder
  │                                              │
  │                                              │ ⑤ Build entries:
  │                                              │      entryNow    @ final.fetchedAt (or now)
  │                                              │      entryStale  @ final.fetchedAt + 45 min
  │                                              │      entryOffline@ final.fetchedAt + 3  h
  │                                              │
  │                                              │ ⑥ policy = .after(now + 30 min)
  │                                              │
  │◄─────────────────────────────────────────────┤ Timeline(entries, policy)
  │
  │ render entry for current wall-clock time
  │
  ▼
 Widget view
```

### Data flow — app triggers widget reload

```
 DashboardViewModel.refresh()  ──► APIClient.fetchStatus()
                                    │
                                    ▼ success
                                  status, fetchedAt
                                    │
                                    ▼
                         WidgetSnapshotCache.writeIfNewer(envelope)
                                    │
                                    ▼
                      WidgetCenter.shared.reloadAllTimelines()
```

---

## Project Structure and File Migration

### New Xcode layout

```
flux/
├── Flux/                              (Xcode project root — unchanged)
│   ├── Flux.xcodeproj/
│   ├── Flux/                          (main-app target — existing sources stay here minus migrated files)
│   │   ├── FluxApp.swift
│   │   ├── Dashboard/
│   │   ├── History/
│   │   ├── DayDetail/
│   │   ├── Settings/
│   │   ├── Navigation/
│   │   ├── WidgetBridge/              (NEW — app-side widget plumbing)
│   │   │   ├── WidgetCacheWriter.swift
│   │   │   ├── KeychainAccessibilityMigrator.swift
│   │   │   └── SettingsSuiteMigrator.swift
│   │   ├── Flux.entitlements
│   │   └── Info.plist                 (registers flux:// URL scheme)
│   ├── FluxWidgets/                   (NEW — widget extension target; Xcode target name is `FluxWidgetsExtension`)
│   │   ├── FluxWidgetsBundle.swift
│   │   ├── FluxBatteryWidget.swift
│   │   ├── FluxAccessoryWidget.swift
│   │   ├── StatusTimelineProvider.swift   (thin WidgetKit shim over StatusTimelineLogic in FluxCore)
│   │   ├── ColorTier+Color.swift          (SwiftUI extension on FluxCore's ColorTier)
│   │   ├── Views/
│   │   │   ├── SystemSmallView.swift
│   │   │   ├── SystemMediumView.swift
│   │   │   ├── SystemLargeView.swift
│   │   │   ├── AccessoryCircularView.swift
│   │   │   ├── AccessoryRectangularView.swift
│   │   │   ├── AccessoryInlineView.swift
│   │   │   ├── Shared/SOCHeroLabel.swift
│   │   │   ├── Shared/StatusLineLabel.swift
│   │   │   ├── Shared/LoadRow.swift
│   │   │   └── Shared/StalenessFootnote.swift
│   │   └── Info.plist
│   ├── FluxWidgetsExtension.entitlements  (App Group + Keychain Sharing; Xcode auto-placed at project root)
│   ├── FluxTests/                     (unchanged)
│   ├── FluxUITests/                   (unchanged)
│   └── Packages/
│       └── FluxCore/                  (NEW — local Swift Package)
│           ├── Package.swift
│           ├── Sources/FluxCore/
│           │   ├── Models/APIModels.swift
│           │   ├── Models/FluxAPIError.swift
│           │   ├── Networking/FluxAPIClient.swift
│           │   ├── Networking/URLSessionAPIClient.swift
│           │   ├── Security/KeychainService.swift
│           │   ├── Helpers/DateFormatting.swift
│           │   ├── Helpers/PowerFormatting.swift
│           │   ├── Helpers/BatteryColor.swift
│           │   ├── Helpers/GridColor.swift
│           │   ├── Helpers/CutoffTimeColor.swift
│           │   ├── Helpers/ColorTier.swift
│           │   ├── Widget/WidgetSnapshotCache.swift
│           │   ├── Widget/StatusSnapshotEnvelope.swift
│           │   ├── Widget/StalenessClassifier.swift
│           │   ├── Widget/WidgetDeepLink.swift
│           │   ├── Widget/SettingsSuiteMigrator.swift
│           │   ├── Widget/StatusEntry.swift            (TimelineEntry-conforming value type)
│           │   ├── Widget/StatusTimelineLogic.swift    (testable orchestration; provider delegates to this)
│           │   ├── Widget/RelevanceScoring.swift       (moved from widget target per Decision 19)
│           │   └── Widget/WidgetAccessibility.swift    (moved from widget target per Decision 19)
│           └── Tests/FluxCoreTests/
│               ├── APIModelsTests.swift           (migrated)
│               ├── DateFormattingTests.swift      (migrated)
│               ├── ColoringTests.swift            (migrated)
│               ├── KeychainServiceTests.swift     (migrated)
│               ├── URLSessionAPIClientTests.swift (migrated)
│               ├── WidgetSnapshotCacheTests.swift (NEW)
│               ├── StalenessClassifierTests.swift (NEW)
│               ├── WidgetDeepLinkTests.swift      (NEW)
│               ├── SettingsSuiteMigratorTests.swift  (NEW)
│               ├── StatusTimelineLogicTests.swift    (NEW — all widget-logic scenarios)
│               ├── RelevanceScoringTests.swift       (NEW)
│               └── WidgetAccessibilityTests.swift    (NEW)
├── Makefile                           (add widget build targets)
├── CHANGELOG.md                       (add entry under Unreleased)
└── ...
```

### File migration plan (app → package)

| Source (app)                                     | Destination (`FluxCore`)                     | Access change            |
|--------------------------------------------------|----------------------------------------------|--------------------------|
| `Flux/Flux/Models/APIModels.swift`               | `Sources/FluxCore/Models/APIModels.swift`    | `public` on all types    |
| `Flux/Flux/Models/FluxAPIError.swift`            | `Sources/FluxCore/Models/FluxAPIError.swift` | `public` on enum/methods |
| `Flux/Flux/Services/FluxAPIClient.swift`         | `Sources/FluxCore/Networking/…`              | `public` on protocol     |
| `Flux/Flux/Services/URLSessionAPIClient.swift`   | `Sources/FluxCore/Networking/…`              | `public` on type+init    |
| `Flux/Flux/Services/KeychainService.swift`       | `Sources/FluxCore/Security/…`                | `public` on type+methods |
| `Flux/Flux/Helpers/DateFormatting.swift`         | `Sources/FluxCore/Helpers/…`                 | `public` on statics      |
| `Flux/Flux/Helpers/PowerFormatting.swift`        | `Sources/FluxCore/Helpers/…`                 | `public` on statics      |
| `Flux/Flux/Helpers/BatteryColor.swift`           | `Sources/FluxCore/Helpers/…`                 | `public`; split `ColorTier` into its own file for import clarity |
| `Flux/Flux/Helpers/GridColor.swift`              | `Sources/FluxCore/Helpers/…`                 | `public`                 |
| `Flux/Flux/Helpers/CutoffTimeColor.swift`        | `Sources/FluxCore/Helpers/…`                 | `public`                 |

`MockFluxAPIClient.swift` stays in the app target because it is `#if DEBUG` preview scaffolding with different concerns in the widget (the widget has its own `WidgetFixtures`).

`CachedDayEnergy.swift` (SwiftData model) stays in the app target because the widget does not use SwiftData.

`EnergySummaryFormatter.swift` stays in the app target because the widget does not format energy summaries.

### Test migration

The following test files currently under `FluxTests/` test pure-logic types that are moving into `FluxCore`. They move into `Packages/FluxCore/Tests/FluxCoreTests/`:

- `APIModelsTests.swift`
- `DateFormattingTests.swift`
- `ColoringTests.swift`
- `KeychainServiceTests.swift`
- `URLSessionAPIClientTests.swift`

The following stay in `FluxTests/` because they test types that remain in the app target (view models):

- `DashboardViewModelTests.swift`
- `HistoryViewModelTests.swift`
- `DayDetailViewModelTests.swift`
- `SettingsViewModelTests.swift`
- `EnergySummaryFormatterTests.swift`
- `FluxTests.swift`

---

## FluxCore Package API Surface

### `Package.swift`

```swift
// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "FluxCore",
    platforms: [.iOS(.v26)],
    products: [
        .library(name: "FluxCore", targets: ["FluxCore"])
    ],
    targets: [
        .target(name: "FluxCore"),
        .testTarget(name: "FluxCoreTests", dependencies: ["FluxCore"])
    ]
)
```

### Public surface

Only what widget and app both need becomes `public`. Everything else stays `internal`.

```swift
// Models
public struct StatusResponse: Codable, Sendable { /* ...existing... */ }
public struct LiveData: Codable, Sendable { /* ... */ }
public struct BatteryInfo: Codable, Sendable { /* ... */ }
public struct Low24h: Codable, Sendable { /* ... */ }
public struct RollingAvg: Codable, Sendable { /* ... */ }
public struct OffpeakData: Codable, Sendable {
    public static let defaultWindowStart = "11:00"
    public static let defaultWindowEnd = "14:00"
    /* fields... */
}
public struct TodayEnergy: Codable, Sendable { /* ... */ }
public struct HistoryResponse: Codable, Sendable { /* ... */ }
public struct DayEnergy: Codable, Sendable, Identifiable { /* ... */ }
public struct DayDetailResponse: Codable, Sendable { /* ... */ }
public struct TimeSeriesPoint: Codable, Sendable, Identifiable { /* ... */ }
public struct DaySummary: Codable, Sendable { /* ... */ }
public struct PeakPeriod: Codable, Sendable, Identifiable { /* ... */ }
public struct APIErrorResponse: Codable, Sendable { /* ... */ }

// Errors
public enum FluxAPIError: Error, Sendable, Equatable { /* ... */ }
public extension FluxAPIError { /* .from, .message, .suggestsSettings */ }

// Networking
public protocol FluxAPIClient: Sendable {
    func fetchStatus() async throws -> StatusResponse
    func fetchHistory(days: Int) async throws -> HistoryResponse
    func fetchDay(date: String) async throws -> DayDetailResponse
}
public final class URLSessionAPIClient: FluxAPIClient, Sendable {
    public init(baseURL: URL, keychainService: KeychainService, session: URLSession? = nil)
    public init(baseURL: URL, token: String, session: URLSession? = nil)
    // existing method impls
}

// Security
public enum KeychainAccessibility: Sendable, Equatable {
    case afterFirstUnlockThisDeviceOnly
    case other(String) // raw kSecAttrAccessible string for anything else
    case missing       // attribute not returned by SecItemCopyMatching
}

public final class KeychainService: Sendable {
    public init(
        service: String = "me.nore.ig.flux",
        account: String = "api-token",
        accessGroup: String? = "group.me.nore.ig.flux",
        accessibility: KeychainAccessibility = .afterFirstUnlockThisDeviceOnly
    )
    public func saveToken(_ token: String) throws
    public func loadToken() -> String?
    public func deleteToken() throws
    /// Reads the token's current accessibility class.
    /// Returns `.missing` when the item exists but the attribute is not returned.
    /// Returns `nil` only when no item exists.
    public func readAccessibility() -> KeychainAccessibility?
    /// Updates just the accessibility class via `SecItemUpdate` — does NOT delete-then-add.
    /// Returns true on success, false if the item did not exist.
    public func updateAccessibility(_ class: KeychainAccessibility) throws -> Bool
}

// Formatting & colour helpers
public enum DateFormatting { /* existing statics, now public */ }
public enum PowerFormatting { /* existing statics, now public */ }
public enum BatteryColor { public static func forSOC(_: Double) -> ColorTier }
public enum GridColor { public static func forGrid(...) -> ColorTier }
public enum CutoffTimeColor { public static func forCutoff(...) -> ColorTier }
public enum ColorTier: Sendable, Equatable { case green, red, orange, amber, normal }
// NOTE: ColorTier lives in FluxCore without a SwiftUI.Color accessor.
// An extension in the main app and widget target adds .color on top.

// Widget support
public struct StatusSnapshotEnvelope: Codable, Sendable {
    public static let currentSchemaVersion: Int = 1
    public let schemaVersion: Int
    public let fetchedAt: Date
    public let status: StatusResponse
    public init(schemaVersion: Int = currentSchemaVersion, fetchedAt: Date, status: StatusResponse)
}
public final class WidgetSnapshotCache: Sendable {
    public init(suiteName: String = "group.me.nore.ig.flux",
                nowProvider: @escaping @Sendable () -> Date = { .now })
    public func read() -> StatusSnapshotEnvelope?
    /// Writes `envelope` only if no existing envelope exists OR existing envelope's
    /// `fetchedAt` is older than `envelope.fetchedAt`. Returns whether the write happened.
    @discardableResult
    public func writeIfNewer(_ envelope: StatusSnapshotEnvelope) -> Bool
    public func clear()
}
public enum Staleness: Sendable, Equatable { case fresh, stale, offline }
public enum StalenessClassifier {
    public static let freshThreshold: TimeInterval = 45 * 60
    public static let offlineThreshold: TimeInterval = 3 * 3600
    public static func classify(fetchedAt: Date, now: Date) -> Staleness
    public static func nextTransition(fetchedAt: Date, now: Date) -> Date?
}
public enum WidgetDeepLink {
    public static let scheme = "flux"
    public static let dashboardURL = URL(string: "flux://dashboard")!
    public enum Destination: Equatable { case dashboard }
    public static func parse(_ url: URL) -> Destination?
}
```

### SwiftUI colour extension (in each consuming target)

`ColorTier` in `FluxCore` is pure-logic (no SwiftUI import). Each consuming target adds its own colour accessor:

```swift
// In both Flux (app) and FluxWidgets (extension)
import SwiftUI
import FluxCore

extension ColorTier {
    var color: Color {
        switch self {
        case .green: .green
        case .red: .red
        case .orange: .orange
        case .amber: .yellow
        case .normal: .primary
        }
    }
}
```

This keeps `FluxCore` free of SwiftUI (helpful for any future CLI/server reuse and keeps package build fast).

---

## Components and Interfaces

### WidgetSnapshotCache

**Storage key:** `widgetSnapshotV1` in `UserDefaults(suiteName: "group.me.nore.ig.flux")`.

**Payload:** JSON-encoded `StatusSnapshotEnvelope`. `JSONEncoder` with `.iso8601` date strategy so `fetchedAt` is a readable string.

**Atomicity:** `UserDefaults.set(_:forKey:)` is atomic per key — a concurrent reader either sees the old bytes or the new bytes, never a torn value. This is exactly what we need; we do not need `NSFileCoordinator`, which is not supported in widget extensions anyway.

**Newer-wins write:**

```swift
public func writeIfNewer(_ envelope: StatusSnapshotEnvelope) -> Bool {
    guard let defaults = UserDefaults(suiteName: suiteName) else { return false }
    if let existing = readEnvelope(from: defaults),
       existing.fetchedAt > envelope.fetchedAt {
        return false
    }
    guard let data = try? encoder.encode(envelope) else { return false }
    defaults.set(data, forKey: Self.storageKey)
    return true
}
```

The comparison is strict `>`, not `>=` — an equal-timestamp write proceeds. This aligns with requirement [4.8](requirements.md#4.8) ("SHALL NOT overwrite whose fetch timestamp is newer") and avoids silently dropping a write when two processes compute `fetchedAt` in the same second (the second write may carry a richer or corrected payload).

A read-compare-write race is possible (app and widget both pass the guard before either writes). The worst outcome is that the slightly-older write lands last and the next widget read picks up the older snapshot. Classification is based on `fetchedAt`, not write-time, so the staleness marker remains correct even in the racing case. Within 30 minutes the next timeline refresh overwrites with fresher data. Across-process `cfprefsd` synchronisation is best-effort: a reader mid-write transition may briefly see `nil`; the provider treats this as "no cache" and may emit one placeholder render — acceptable jitter.

**Schema-version handling:** `read()` decodes the envelope, verifies `schemaVersion == StatusSnapshotEnvelope.currentSchemaVersion`, and returns `nil` if it does not match. A future bump (e.g. after a breaking `StatusResponse` change) simply increases `currentSchemaVersion`; the widget transparently behaves as if the cache were empty until the app next runs and rewrites it.

### StalenessClassifier

```swift
public enum StalenessClassifier {
    public static let freshThreshold: TimeInterval = 45 * 60          // 45 min
    public static let offlineThreshold: TimeInterval = 3 * 3600       // 3 h

    public static func classify(fetchedAt: Date, now: Date) -> Staleness {
        let age = now.timeIntervalSince(fetchedAt)
        if age < freshThreshold { return .fresh }
        if age < offlineThreshold { return .stale }
        return .offline
    }

    /// The next wall-clock moment at which classification transitions.
    /// Used by the timeline provider to emit bucket-transition entries.
    public static func nextTransition(fetchedAt: Date, now: Date) -> Date? {
        let freshBoundary = fetchedAt.addingTimeInterval(freshThreshold)
        let offlineBoundary = fetchedAt.addingTimeInterval(offlineThreshold)
        if now < freshBoundary { return freshBoundary }
        if now < offlineBoundary { return offlineBoundary }
        return nil
    }
}
```

Pure, deterministic, covered by Swift Testing tests.

### StatusTimelineProvider (in widget extension)

```swift
struct StatusEntry: TimelineEntry {
    let date: Date                        // the wall-clock moment this entry renders for
    let envelope: StatusSnapshotEnvelope? // nil only for the "never seen data" placeholder
    let staleness: Staleness
    let source: Source

    enum Source { case live, cache, placeholder }
}

struct StatusTimelineProvider: TimelineProvider {
    private let apiClient: any FluxAPIClient
    private let cache: WidgetSnapshotCache
    private let keychain: KeychainService
    private let nowProvider: @Sendable () -> Date
    private let fetchTimeout: Duration

    init(apiClient: any FluxAPIClient? = nil,
         cache: WidgetSnapshotCache = WidgetSnapshotCache(),
         keychain: KeychainService = KeychainService(),
         nowProvider: @escaping @Sendable () -> Date = { .now },
         fetchTimeout: Duration = .seconds(5))
    { ... }

    func placeholder(in context: Context) -> StatusEntry
    func getSnapshot(in context: Context, completion: @escaping (StatusEntry) -> Void)
    func getTimeline(in context: Context, completion: @escaping (Timeline<StatusEntry>) -> Void)
}
```

Provider lifecycle:

1. **`placeholder(in:)`** — returns a `StatusEntry` built from `WidgetFixtures.placeholderEnvelope` (plausible mid-discharge). Source = `.placeholder`. Renders instantly, needs no I/O.
2. **`getSnapshot(in:)`** —
   - If `context.isPreview`, call `completion(placeholder(in:))` and return.
   - Otherwise, run the same pipeline as `getTimeline` and return the current entry only.
3. **`getTimeline(in:)`** —
   1. Read the cache once.
   2. If `keychain.loadToken()` is non-nil, attempt a live fetch with a 5-second timeout. On success, build a new envelope and `cache.writeIfNewer(…)`.
   3. Choose the best envelope: `live ?? cached`.
   4. If no envelope exists at all, emit a single "No data yet" placeholder-source entry with `.after(now + 30 min)` policy.
   5. Otherwise emit a stack of up to three entries:
      - Current moment (`date = now`) with classification for `now`.
      - Next staleness transition (if any) with classification one bucket worse.
      - The offline boundary (if the fresh boundary is next, the offline boundary is the one after that).
   6. Return `Timeline(entries, .after(now + 30 min))`.

Pseudo-code for entry stack:

```swift
private func makeEntries(envelope: StatusSnapshotEnvelope?, source: StatusEntry.Source, now: Date) -> [StatusEntry] {
    guard let env = envelope else {
        return [StatusEntry(date: now, envelope: nil, staleness: .offline, source: .placeholder)]
    }
    let freshBoundary = env.fetchedAt.addingTimeInterval(StalenessClassifier.freshThreshold)
    let offlineBoundary = env.fetchedAt.addingTimeInterval(StalenessClassifier.offlineThreshold)

    var dates: [Date] = [now]
    if now < freshBoundary { dates.append(freshBoundary) }
    if now < offlineBoundary { dates.append(offlineBoundary) }

    return dates.map { d in
        StatusEntry(
            date: d,
            envelope: env,
            staleness: StalenessClassifier.classify(fetchedAt: env.fetchedAt, now: d),
            source: source
        )
    }
}
```

**Timeout — enforced at the URLSession layer, not the Swift task layer.**

Cancelling a `URLSession.data(for:)` task via `withTaskGroup(...).cancelAll()` does not reliably interrupt the underlying socket on iOS 26 — the TCP/TLS request continues until `URLSession`'s own timeouts fire. To keep the widget firmly within its ~15-second WidgetKit budget, the widget's `URLSessionAPIClient` is constructed with a 5-second request timeout at the session level:

```swift
// In widget extension
static let widgetSession: URLSession = {
    let config = URLSessionConfiguration.default
    config.requestCachePolicy = .reloadIgnoringLocalCacheData
    config.urlCache = nil
    config.timeoutIntervalForRequest = 5   // time between packets
    config.timeoutIntervalForResource = 5  // total wall-clock for the request
    config.waitsForConnectivity = false    // don't wait for a slow network
    return URLSession(configuration: config)
}()

let client = URLSessionAPIClient(
    baseURL: apiURL,
    keychainService: keychain,
    session: widgetSession
)
```

Constructing the session once as a `static let` reuses the connection pool across timeline refreshes within a single extension invocation. `URLSessionAPIClient` already accepts an optional `URLSession` via its initializer (existing code path) so no package-level change is needed.

The widget resolves `apiURL` from `UserDefaults(suiteName: "group.me.nore.ig.flux")`. If `apiURL` is missing or not a parseable URL, the widget does not attempt a fetch and uses the cache. If `keychain.loadToken()` returns `nil` or fails with `errSecInteractionNotAllowed`, same behaviour.

**TimelineEntryRelevance — in scope:**

Timeliness near cutoff *is* the widget's primary value. Each `StatusEntry` carries a relevance score:

| State                                                             | Score |
|-------------------------------------------------------------------|-------|
| `.fresh` and discharging with `soc <= cutoffPercent + 5`          | 0.9   |
| `.fresh` and discharging with `soc <= cutoffPercent + 20`         | 0.7   |
| `.fresh` otherwise                                                | 0.5   |
| `.stale`                                                          | 0.3   |
| `.offline` or placeholder                                         | 0.1   |

```swift
struct StatusEntry: TimelineEntry {
    let date: Date
    let envelope: StatusSnapshotEnvelope?
    let staleness: Staleness
    let source: Source
    let relevance: TimelineEntryRelevance?

    enum Source { case live, cache, placeholder }
}
```

The scoring function lives in `FluxCore` (Decision 19 — widget-testable logic consolidates into the package). It uses `TimelineEntryRelevance` from `WidgetKit`, which `FluxCore` can import since the package targets iOS. The function takes only `Staleness` + `BatteryInfo?` + `LiveData?` as inputs so it is unit-testable without WidgetKit-runtime dependencies beyond the return type.

### Widget declarations

```swift
// FluxWidgetsBundle.swift
import WidgetKit
import SwiftUI

@main
struct FluxWidgetsBundle: WidgetBundle {
    var body: some Widget {
        FluxBatteryWidget()
        FluxAccessoryWidget()
    }
}

// FluxBatteryWidget.swift — home-screen families
struct FluxBatteryWidget: Widget {
    let kind = "me.nore.ig.flux.widget.battery"
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: kind, provider: StatusTimelineProvider()) { entry in
            FluxHomeScreenView(entry: entry)
        }
        .configurationDisplayName("Flux Battery")
        .description("Battery state and household power at a glance.")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge])
    }
}

// FluxAccessoryWidget.swift — lock-screen families
struct FluxAccessoryWidget: Widget {
    let kind = "me.nore.ig.flux.widget.accessory"
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: kind, provider: StatusTimelineProvider()) { entry in
            FluxAccessoryView(entry: entry)
        }
        .configurationDisplayName("Flux Accessory")
        .description("Battery state for the lock screen.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}
```

Two widgets keep the widget-gallery entries grouped correctly (home screen vs lock screen). `FluxWidgetsBundle` is the extension's `@main` entry; iOS picks the correct widget type based on the family the user selects in the gallery. Targeted reloads use the widget-`kind` constants (`me.nore.ig.flux.widget.battery`, `me.nore.ig.flux.widget.accessory`).

---

## Widget Views

### Family layout matrix

| Family              | SOC    | Status text | Cutoff | Power trio | Secondary metric | Timestamp |
|---------------------|--------|-------------|--------|------------|------------------|-----------|
| `systemSmall`       | hero   | ✓           | ✓ (when discharging) | ✗    | load (pload)    | stale/offline only |
| `systemMedium`      | hero   | ✓           | ✓                    | ✓    | ✗                | stale/offline always |
| `systemLarge`       | hero   | ✓           | ✓                    | ✓    | 24h-low + off-peak summary (from `battery`/`offpeak`) | always |
| `accessoryCircular` | ring + %centred | ✗ | ✗         | ✗    | ✗                | ✗ (ring dims when offline) |
| `accessoryRect.`    | %      | short       | ✓ (short clock) | ✗ | ✗                | offline only |
| `accessoryInline`   | SOC%   | one word    | ✗      | ✗          | ✗                | replaces status when offline |

### Shared utilities

```swift
// In FluxWidgets target — the UI-side extension on ColorTier lives here too.
extension StatusEntry {
    var soc: Double { envelope?.status.live?.soc ?? 0 }
    var pbat: Double { envelope?.status.live?.pbat ?? 0 }
    var pload: Double { envelope?.status.live?.pload ?? 0 }
    var ppv: Double { envelope?.status.live?.ppv ?? 0 }
    var pgrid: Double { envelope?.status.live?.pgrid ?? 0 }

    /// Mirrors BatteryHeroView.statusLine on the Dashboard.
    func statusLine(style: StatusLineStyle) -> String { ... }

    enum StatusLineStyle { case full, short, word }
}
```

`statusLine(style:)`:

- `.full` — "Discharging at 2.1 kW · cutoff ~5:12 pm" (medium/large/small/rectangular).
- `.short` — "Discharging · 5:12 pm" (rectangular if space tight).
- `.word` — "discharging" / "charging" / "idle" / "full" / "offline" (inline).

### `SystemSmallView`

```
┌──────────────────────────────┐
│                              │
│        73.4%                 │   ← SOC hero, BatteryColor.forSOC tint
│                              │
│   Discharging at 2.1 kW      │   ← status line (.full if it fits, .short otherwise)
│                              │
│   ────────────────────       │   ← faint divider
│                              │
│   Load  412 W                │   ← pload, red if > loadAlertThreshold
│                              │
│   (stale 47 min ago)         │   ← only when staleness != .fresh
└──────────────────────────────┘
```

Component tree:

```swift
struct SystemSmallView: View {
    let entry: StatusEntry
    @Environment(\.widgetFamily) private var family

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            SOCHeroLabel(entry: entry, size: .small)
            StatusLineLabel(entry: entry, style: .short, allowClockTime: true)
            Divider().opacity(0.3)
            LoadRow(entry: entry)
            if entry.staleness != .fresh {
                StalenessFootnote(entry: entry)
            }
        }
        .padding(12)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(WidgetAccessibility.label(for: entry, family: family))
        .widgetURL(WidgetDeepLink.dashboardURL)
        .containerBackground(for: .widget) { Color.clear }
    }
}
```

### `SystemMediumView`

```
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│  73.4%   │  Solar  1.8 kW                                      │
│          │  Load    412 W                                      │
│  Discharging at 2.1 kW                                         │
│  cutoff ~5:12 pm    │  Grid (import)  210 W                    │
│                                                                │
│                                         (stale 47 min ago)     │
└────────────────────────────────────────────────────────────────┘
```

Two-column `HStack`: left column SOC hero + status line, right column power trio. This mirrors the Dashboard's `BatteryHeroView` + `PowerTrioView` hierarchy but horizontally stacked to suit the 16:9 widget aspect.

### `SystemLargeView`

Adds a third row below the medium layout containing the 24h low and the off-peak summary (grid usage + battery delta) from `battery.low24h`, `offpeak.gridUsageKwh`, and `offpeak.batteryDeltaPercent`. Mirrors `SecondaryStatsView` minus the 15-min rolling load (already encoded in the status line's cutoff time).

### `AccessoryCircularView`

```swift
struct AccessoryCircularView: View {
    let entry: StatusEntry
    @Environment(\.widgetRenderingMode) private var renderingMode

    var body: some View {
        Gauge(value: entry.soc, in: 0...100) {
            EmptyView()
        } currentValueLabel: {
            Text(Int(entry.soc.rounded()), format: .number)
                .font(.headline)
                .minimumScaleFactor(0.5)
        }
        .gaugeStyle(.accessoryCircularCapacity)
        .tint(tintForRenderingMode())
        .opacity(entry.staleness == .offline ? 0.5 : 1)
        .containerBackground(for: .widget) { Color.clear }
        .widgetURL(WidgetDeepLink.dashboardURL)
        .accessibilityLabel(WidgetAccessibility.label(for: entry, family: .accessoryCircular))
    }

    private func tintForRenderingMode() -> Color {
        switch renderingMode {
        case .fullColor: return BatteryColor.forSOC(entry.soc).color
        case .accented, .vibrant: return .primary
        @unknown default: return .primary
        }
    }
}
```

**Rendering-mode handling** — in `.accented` (tinted lock screen) and `.vibrant` (watch-face-style) modes, WidgetKit applies its own tint overlay, and per-element colours are ignored or muddied. We defer to the system tint by returning `.primary`, which iOS will map correctly. In `.fullColor` we retain `BatteryColor.forSOC`.

### `AccessoryRectangularView`

```
┌─────────────────────────────────┐
│ 🔋  73.4%                       │
│     Discharging · 5:12 pm       │
│     (stale 47 min ago)          │ ← only when != fresh
└─────────────────────────────────┘
```

Uses `.widgetAccentable()` on the icon so it stays visible when the lock screen tints the widget.

### `AccessoryInlineView`

One-line text, follows iOS's single-line constraint:

```swift
Text("Flux: 73% · discharging")
```

When offline: `Text("Flux: offline")` — no SOC because the cached number may be hours out of date.

### Accessibility

```swift
enum WidgetAccessibility {
    static func label(for entry: StatusEntry, family: WidgetFamily) -> String {
        if entry.staleness == .offline {
            return "Offline. " + baseLabel(for: entry, family: family)
        }
        return baseLabel(for: entry, family: family)
    }

    private static func baseLabel(for entry: StatusEntry, family: WidgetFamily) -> String {
        // e.g. "Battery 73 percent, discharging at 2.1 kilowatts, cutoff around 5:12 pm.
        //       Solar 1.8 kilowatts, load 412 watts, grid importing 210 watts."
    }
}
```

Every label begins with battery percentage so VoiceOver users get SOC first. Offline always prepends "Offline.".

---

## Deep Link Plumbing

### Entitlements

Both targets share the `group.me.nore.ig.flux` App Group and the same Keychain access group. The widget extension gets its own `.entitlements` file alongside the existing app's:

```xml
<!-- FluxWidgets/FluxWidgets.entitlements -->
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>com.apple.security.application-groups</key>
    <array>
        <string>group.me.nore.ig.flux</string>
    </array>
    <key>keychain-access-groups</key>
    <array>
        <string>$(AppIdentifierPrefix)group.me.nore.ig.flux</string>
    </array>
</dict>
</plist>
```

The app's existing `Flux.entitlements` gains the matching `keychain-access-groups` entry if not already present. Without the Keychain access group, the widget cannot read the shared token even with App Group membership — these are independent entitlements.

### URL scheme registration

`Flux/Flux/Info.plist` gains:

```xml
<key>CFBundleURLTypes</key>
<array>
  <dict>
    <key>CFBundleURLName</key>
    <string>me.nore.ig.flux.deeplink</string>
    <key>CFBundleURLSchemes</key>
    <array>
      <string>flux</string>
    </array>
  </dict>
</array>
```

### Parser in `FluxCore`

```swift
public enum WidgetDeepLink {
    public static let scheme = "flux"
    public static let dashboardURL = URL(string: "flux://dashboard")!

    public enum Destination: Equatable { case dashboard }

    public static func parse(_ url: URL) -> Destination? {
        guard url.scheme == scheme else { return nil }
        switch url.host {
        case "dashboard": return .dashboard
        default: return nil // Unknown hosts return nil so future links don't crash older builds.
        }
    }
}
```

### App-side handling

`AppNavigationView` already uses `selectedScreen` state; on `.onOpenURL`, set it to `.dashboard`:

```swift
.onOpenURL { url in
    switch WidgetDeepLink.parse(url) {
    case .dashboard?:
        selectedScreen = .dashboard
        navigationPath = NavigationPath()
    default:
        break
    }
}
```

The existing `scenePhase` .active handler reloads dependencies; no further change needed.

---

## Main App Changes

### DashboardViewModel

The Dashboard refreshes every 10 seconds while active. Naively calling `WidgetCenter.shared.reloadAllTimelines()` on every refresh would exhaust the ~40–70 daily reload budget in a single foreground session — per iOS 26 guidance that budget is shared across the bundle, and two widget kinds placed multiple times accelerates exhaustion.

The refresh path applies two guards to the widget reload:

1. **Gate on `writeIfNewer` returning `true`.** If the cached snapshot already matches (same `fetchedAt` — no behaviour change), no reload is triggered. In practice the Dashboard advances `fetchedAt` each refresh, so the guard primarily suppresses reloads for redundant or no-op writes.
2. **Debounce to at most one reload per 5 minutes.** The view model records the last reload-trigger time and suppresses subsequent triggers within that window.

```swift
// existing:
let response = try await apiClient.fetchStatus()
status = response
lastSuccessfulFetch = nowProvider()
error = nil

// new:
let envelope = StatusSnapshotEnvelope(fetchedAt: lastSuccessfulFetch!, status: response)
let cacheUpdated = widgetCache.writeIfNewer(envelope)
let dueForReload = lastWidgetReload.map { nowProvider().timeIntervalSince($0) >= widgetReloadDebounce } ?? true
if cacheUpdated && dueForReload {
    lastWidgetReload = nowProvider()
    widgetReloadTrigger()
}
```

`widgetReloadTrigger` targets both widget kinds explicitly rather than `reloadAllTimelines()`:

```swift
init(
    apiClient: any FluxAPIClient,
    widgetCache: WidgetSnapshotCache = WidgetSnapshotCache(),
    widgetReloadTrigger: @Sendable @escaping () -> Void = {
        WidgetCenter.shared.reloadTimelines(ofKind: "me.nore.ig.flux.widget.battery")
        WidgetCenter.shared.reloadTimelines(ofKind: "me.nore.ig.flux.widget.accessory")
    },
    widgetReloadDebounce: TimeInterval = 5 * 60, // 5 minutes
    // ...
)
```

Net effect: one widget reload every 5 minutes at most while the Dashboard is active, reliably fresh when the user opens the app after a long absence, and no reload at all if the refresh produced an identical snapshot. This keeps widget-reload spend inside the daily budget while still satisfying requirement [5.3](requirements.md#5.3) (widget sees fresh data after app opens).

Both closures and the debounce interval are initialiser-injected so tests can substitute deterministic values. `lastWidgetReload` is private state on the view model (`Date?`).

### UserDefaults+Settings migration

Current implementation uses `UserDefaults.standard`. Move to `UserDefaults(suiteName: "group.me.nore.ig.flux")`:

```swift
extension UserDefaults {
    static let fluxAppGroup: UserDefaults = {
        guard let defaults = UserDefaults(suiteName: "group.me.nore.ig.flux") else {
            fatalError("App Group 'group.me.nore.ig.flux' not configured.")
        }
        return defaults
    }()
}

extension UserDefaults {
    private enum Keys {
        static let apiURL = "apiURL"
        static let loadAlertThreshold = "loadAlertThreshold"
        static let settingsMigrationVersion = "settingsMigrationVersion"
    }

    var apiURL: String? {
        get { string(forKey: Keys.apiURL) }
        set { set(newValue, forKey: Keys.apiURL) }
    }
    var loadAlertThreshold: Double {
        get {
            let stored = double(forKey: Keys.loadAlertThreshold)
            return stored == 0 ? 3000 : stored
        }
        set { set(newValue, forKey: Keys.loadAlertThreshold) }
    }
}
```

Callers (e.g. `PowerTrioView`'s `loadAlertThreshold` default, `SettingsViewModel.loadExisting`) switch from `UserDefaults.standard` to `UserDefaults.fluxAppGroup`.

### `SettingsSuiteMigrator`

Runs once at app launch via `FluxApp.init()` or the existing `AppNavigationView.reloadDependencies` path:

```swift
enum SettingsSuiteMigrator {
    static let currentVersion = 1

    @discardableResult
    static func run() -> Bool {
        let suite = UserDefaults.fluxAppGroup
        let runAt = suite.integer(forKey: "settingsMigrationVersion")
        guard runAt < currentVersion else { return false }

        let standard = UserDefaults.standard
        if let url = standard.string(forKey: "apiURL") {
            if suite.string(forKey: "apiURL") == nil {
                suite.set(url, forKey: "apiURL")
            }
        }
        let standardThreshold = standard.double(forKey: "loadAlertThreshold")
        if standardThreshold > 0, suite.double(forKey: "loadAlertThreshold") == 0 {
            suite.set(standardThreshold, forKey: "loadAlertThreshold")
        }

        suite.set(currentVersion, forKey: "settingsMigrationVersion")
        return true
    }
}
```

Fresh installs hit the `standardThreshold == 0` branch (no-op) and still set `settingsMigrationVersion = 1`, so future cold starts early-exit.

### `KeychainAccessibilityMigrator`

Existing Keychain items may have a stricter accessibility class. Migrator runs once at app launch and is **crash-safe** — it never deletes the token as part of the happy path. Delete-then-add would risk losing the token if the process is killed between the two operations.

```swift
enum KeychainAccessibilityMigrator {
    static let required: KeychainAccessibility = .afterFirstUnlockThisDeviceOnly

    @discardableResult
    static func run(keychain: KeychainService = KeychainService()) -> Bool {
        guard let currentClass = keychain.readAccessibility() else {
            return false // no token stored; nothing to migrate
        }
        if currentClass == required {
            return false // already correct
        }
        do {
            // SecItemUpdate changes the attribute in place, preserving the token value.
            let updated = try keychain.updateAccessibility(required)
            if updated { return true }
            // Rare: the item exists but cannot be updated. Fall back to read+save, which
            // loses the token if the process is killed between delete and add.
            if let token = keychain.loadToken() {
                try keychain.saveToken(token)
                return true
            }
            return false
        } catch {
            // Log once (without the token). Non-fatal — widget will fall back to cache
            // until the user next saves credentials in Settings.
            return false
        }
    }
}
```

**`SecItemUpdate` vs delete-then-add.** `SecItemUpdate` applies a single atomic change to the existing item's attributes — the underlying token blob is preserved. This is crash-safe: a process kill during the update either leaves the old item untouched (best case) or produces a transient error on the next launch (handled by the `false` return). Delete-then-add is only used as a fallback for the rare case where `SecItemUpdate` cannot apply the change (e.g. keychain corruption), and the migrator logs an error when it has to fall back.

**Implementation notes for `KeychainService`:**

- `readAccessibility()` issues `SecItemCopyMatching` with `kSecReturnAttributes = true` and reads the `kSecAttrAccessible` field. Comparison uses `as String` bridging rather than raw `CFString ==`, which can silently fail: `let raw = attrs[kSecAttrAccessible as String] as? String` then match against `(kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly as String)`. If the attribute is missing from the returned dictionary (iOS sometimes omits it), the accessor returns `.missing` and the migrator treats that as "needs migration".
- `updateAccessibility(_:)` issues `SecItemUpdate` with the same service/account/accessGroup query and a single-attribute update dictionary `[kSecAttrAccessible: required.cfString]`. `required.cfString` is a private computed property on `KeychainAccessibility` that maps the enum back to the Security-framework CFString.

**Why run both migrators?** The Keychain migrator only runs if a token exists (fresh installs skip it harmlessly); the settings migrator is idempotent via its version flag. Both live in `WidgetBridge/` for locality and run in `FluxApp.init`.

**Widget must also run `SettingsSuiteMigrator`.** If the user installs the app, sets up credentials, but adds the widget before the app has ever fully launched (unlikely but possible on a restore-from-backup flow), the App Group suite may be empty. `StatusTimelineProvider.getTimeline` calls `SettingsSuiteMigrator.run()` as its very first step. The migrator is idempotent (version-flag guarded) so the repeated call is free on normal runs. `SettingsSuiteMigrator` lives in `FluxCore` (reachable from both targets); it was shown as app-only above but belongs in the package — the package-side definition is the canonical one, and the app's `WidgetBridge/` only contains the `KeychainAccessibilityMigrator`.

### FluxApp

```swift
@main
struct FluxApp: App {
    init() {
        SettingsSuiteMigrator.run()
        KeychainAccessibilityMigrator.run()
    }

    var body: some Scene {
        WindowGroup {
            AppNavigationView()
        }
        .modelContainer(for: CachedDayEnergy.self)
    }
}
```

Migrators are synchronous, cheap, and safe to run on every launch.

---

## Data Models

Cache envelope (the only new persisted shape):

```swift
public struct StatusSnapshotEnvelope: Codable, Sendable {
    public static let currentSchemaVersion: Int = 1
    public let schemaVersion: Int
    public let fetchedAt: Date
    public let status: StatusResponse
}
```

Encoded with `JSONEncoder` (`.iso8601` date strategy):

```json
{
  "schemaVersion": 1,
  "fetchedAt": "2026-04-20T08:12:33Z",
  "status": { /* full StatusResponse */ }
}
```

`StatusResponse` is unchanged (it already exists in the app and will be public in the package).

### UserDefaults (App Group) keys

| Key                          | Type        | Owner        | Notes |
|------------------------------|-------------|--------------|-------|
| `widgetSnapshotV1`           | Data (JSON) | app + widget | envelope |
| `apiURL`                     | String      | app          | Lambda URL |
| `loadAlertThreshold`         | Double      | app (read by widget) | default 3000 |
| `settingsMigrationVersion`   | Int         | app          | prevents migration replay |

The `V1` suffix on `widgetSnapshotV1` is belt-and-braces alongside `StatusSnapshotEnvelope.schemaVersion`. A future truly-incompatible change (e.g. different encoder) can use `widgetSnapshotV2` and leave the old key for cleanup.

### Keychain

Single item, unchanged location; migrated accessibility class:

```
service:    "me.nore.ig.flux"
account:    "api-token"
accessGroup:"group.me.nore.ig.flux"
accessible: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
```

---

## Error Handling

### Widget timeline provider states

| Situation | Token | Cache | Fetch | Entry returned |
|-----------|-------|-------|-------|----------------|
| Fresh install, token + cache absent | nil | empty | n/a | "No data yet — open Flux" placeholder source |
| Fresh install, token present (via Settings) | present | empty | attempted | live entry if fetch succeeds within 5 s; else same placeholder |
| Normal operation | present | fresh | succeeds | live entry, cache updated |
| App open recently, no widget fetch needed | present | fresh | attempted but slow | cache entry (unchanged), next timeline attempts again |
| Network failure | present | present | fails | cache entry with correct staleness |
| Network failure | present | absent | fails | "No data yet" placeholder |
| Device locked since boot | n/a | present | n/a | cache entry (no fetch attempted; `errSecInteractionNotAllowed` treated as "no token") |
| Cache decode failure | any | corrupted | attempted | live if succeeds; otherwise "No data yet" placeholder |
| Cache schema mismatch | any | unknown schemaVersion | attempted | same as cache absent |

### Error never surfaced to the UI

- Raw `URLError` descriptions.
- `FluxAPIError.serverError` / `.decodingError` / `.networkError` contents.
- Keychain `OSStatus` values.
- JSON encode/decode errors.

Staleness markers and the `Offline` label are the only failure-mode UI.

### App-side error handling

Unchanged — the Dashboard already has two error states (initial-load card and staleness banner). The widget cache write is guarded by `if error == nil`, so a failed refresh never writes a partially-populated envelope.

---

## Testing Strategy

### Package tests (Swift Testing) — `FluxCoreTests`

**Migrated tests stay green:** `APIModelsTests`, `DateFormattingTests`, `ColoringTests`, `KeychainServiceTests`, `URLSessionAPIClientTests`.

**New tests:**

`WidgetSnapshotCacheTests`:

- Round-trip: `writeIfNewer` then `read` returns the same envelope (encode/decode preserves all fields).
- `writeIfNewer` returns `true` on empty cache.
- `writeIfNewer` returns `false` when the stored envelope is newer.
- `writeIfNewer` returns `true` when the stored envelope is older.
- `writeIfNewer` returns `true` on ties: implementation compares `>=`, so equal timestamps NOT overwritten (prevents churn). Requirement [4.8](requirements.md#4.8) says "newer than" — equal timestamps explicitly do not overwrite.
- `read` returns `nil` when bytes are present but `schemaVersion` ≠ `currentSchemaVersion`.
- `read` returns `nil` when bytes are garbage.
- `clear()` removes the key.

Each test uses a unique suite name (`"test.widget.\(UUID())"`) so tests are isolated.

`StalenessClassifierTests` (table-driven):

| age (min) | expected |
|-----------|----------|
| 0         | fresh    |
| 44.9      | fresh    |
| 45.0      | stale    |
| 179.9     | stale    |
| 180.0     | offline  |
| 10000     | offline  |

Plus `nextTransition` tests for each bucket boundary.

**Property-based tests (PBT) evaluation:**

- `WidgetSnapshotCache.writeIfNewer` has a clear invariant: "after any interleaving of `writeIfNewer` calls, `read()` returns the envelope with the latest `fetchedAt` among all successful writes". This is a candidate. Swift Testing does not have built-in PBT; the [`SwiftCheck`](https://github.com/typelift/SwiftCheck) package is the common third-party choice but adds a dependency for relatively limited surface. **Decision: skip PBT for v1**; the example-based set above covers the boundary cases cleanly. If flakiness emerges, revisit with a lightweight custom generator.
- `StalenessClassifier.classify` is a pure function with a total ordering on age → bucket. PBT candidate ("for any `age`, the returned bucket is monotonic as age increases"). **Decision: skip PBT**; the table-driven test is exhaustive for boundaries and the implementation has no branch where PBT would find a bug example-based would miss.

`WidgetDeepLinkTests`:

- `parse("flux://dashboard")` returns `.dashboard`.
- `parse("flux://unknown")` returns `nil`.
- `parse("other://dashboard")` returns `nil`.
- `parse("flux://dashboard/extra/path")` returns `.dashboard` (extra path components ignored).

### Widget-logic tests — `FluxCoreTests` (per Decision 19)

The testable widget orchestration lives in `FluxCore.StatusTimelineLogic`; the widget-extension's `StatusTimelineProvider` is a thin `TimelineProvider` conformance that calls into it. All of the following scenarios are covered at the logic layer, not via WidgetKit-hosted tests:

```swift
@Suite struct StatusTimelineLogicTests {
    @Test func emptyCacheNoToken_returnsPlaceholder() async { ... }
    @Test func emptyCacheTokenPresent_fetchSuccess_returnsLive() async { ... }
    @Test func emptyCacheTokenPresent_fetchFails_returnsPlaceholder() async { ... }
    @Test func cachePresent_fetchSucceeds_writesCacheAndReturnsLive() async { ... }
    @Test func cachePresent_fetchFails_returnsCacheEntry() async { ... }
    @Test func cachePresent_fetchTimesOutWithin5s_returnsCacheEntry() async { ... }
    @Test func cachePresent_emitsBucketTransitionEntries() async { ... }
    @Test func offlineCachedData_classifiedAsOffline() async { ... }
    @Test func fetchWriteIsNewerWins_notClobberedByStaleFetch() async { ... }
    @Test func keychainErrSecInteractionNotAllowed_cachesOnly() async { ... }
}
```

`StatusTimelineLogic` takes all dependencies via initializer so tests inject:

- `MockFluxAPIClient` with configurable response/delay/error.
- `WidgetSnapshotCache` pointing at a unique test suite.
- Deterministic `nowProvider` closure.

`RelevanceScoringTests` and `WidgetAccessibilityTests` similarly live in `FluxCoreTests` (both types moved into `FluxCore` per Decision 19) — the scoring table and accessibility-label scenarios are straight table-driven pure-function tests.

### App-side tests — `FluxTests`

`SettingsSuiteMigratorTests`:

- Running with `.standard` values present copies to the suite and sets version = 1.
- Running with version already = 1 is a no-op.
- Running with no `.standard` values still sets version = 1 (fresh-install behaviour).

`KeychainAccessibilityMigratorTests`:

- Run with no token → returns false, nothing mutated.
- Run with token already at correct class → returns false.
- Run with token at different class → returns true, class is migrated, token bytes preserved.

`DashboardViewModelTests` gets two new cases:

- Successful refresh invokes the widgetCache writeIfNewer closure once.
- Successful refresh invokes the widgetReloadTrigger closure once.
- Failed refresh does NOT invoke either closure.

### UI-level checks

- Widget preview blocks in `#if DEBUG` using `WidgetPreviewContext(family:)` for every supported family × every staleness bucket (fresh/stale/offline). These are visual smoke tests; no automated snapshot assertions.
- Manual test matrix documented in tasks phase: install on physical device, verify each family in gallery + after placement, verify lock-screen monochrome/tinted modes render sensibly.

### Coverage tracing against requirements

| Req                            | Covered by                                       |
|--------------------------------|--------------------------------------------------|
| 1.1/1.2 families supported     | Widget declarations + manual matrix              |
| 2.x battery state              | Timeline provider tests + preview blocks         |
| 3.x power trio                 | Preview blocks + manual matrix                   |
| 3.6 systemSmall load metric    | Preview blocks + manual device                   |
| 4.x data source                | Timeline provider tests                          |
| 4.7 schema version             | `WidgetSnapshotCacheTests`                       |
| 4.8 newer-wins                 | `WidgetSnapshotCacheTests`                       |
| 5.x timeline cadence           | Provider tests (policy + entry dates)            |
| 6.x staleness                  | `StalenessClassifierTests` + provider tests      |
| 8.x deep link                  | `WidgetDeepLinkTests` + app onOpenURL test       |
| 9.x FluxCore migration         | Building app + tests after migration             |
| 10.x cache                     | `WidgetSnapshotCacheTests`                       |
| 10.6/10.7 settings migration   | `SettingsSuiteMigratorTests`                     |
| 11.x security                  | `KeychainServiceTests` + `KeychainAccessibilityMigratorTests` |
| 12.x Liquid Glass              | Preview blocks (visual)                          |
| 13.x accessibility             | Preview blocks + Accessibility Inspector (manual)|
| 14.x placeholders              | Provider tests for placeholder path              |
| 15.x testability               | All tests (provider DI + pure classifier)        |
| 16.x shipping                  | Build + migration tests                          |

---

## Rollout / Migration Order

Order matters because the Keychain and Settings migrations both need to run before the widget first fires:

```
┌─────────────────────────────────────────────────────────────┐
│ User installs new app version                               │
└─────────────┬───────────────────────────────────────────────┘
              │ cold start
              ▼
┌─────────────────────────────────────────────────────────────┐
│ FluxApp.init()                                              │
│   1. SettingsSuiteMigrator.run()                            │
│      - copies apiURL / loadAlertThreshold to App Group      │
│      - sets settingsMigrationVersion = 1                    │
│   2. KeychainAccessibilityMigrator.run()                    │
│      - rewrites token with AfterFirstUnlockThisDeviceOnly   │
└─────────────┬───────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────┐
│ AppNavigationView appears → DashboardView loads             │
│   - DashboardViewModel.refresh() fires                      │
│   - on success: writeIfNewer → App Group cache populated    │
│              WidgetCenter.reloadAllTimelines()              │
└─────────────┬───────────────────────────────────────────────┘
              │
              ▼ user adds widget from gallery (or auto-installed one)
┌─────────────────────────────────────────────────────────────┐
│ Widget timeline provider fires                              │
│   - reads cache (populated)                                 │
│   - Keychain token readable (class is already migrated)     │
│   - fetches live                                            │
│   - renders                                                 │
└─────────────────────────────────────────────────────────────┘
```

### Edge cases in the migration order

- **User never opens the app after upgrade, only adds the widget.** Keychain migration has not run. The widget fetches the Keychain — which still has the old accessibility class — and gets the token (home-screen widgets run while the device is unlocked, so no class problem). On first device auto-lock, the lock-screen widget may fail Keychain reads and fall back to its placeholder. This is acceptable: the first app launch is imminent; any user who adds a widget is likely to open the app to verify it.
- **User downgrades the app.** The old app version doesn't know about the App Group suite and goes back to `.standard`. Settings the user changed in the new version may be invisible in the old version. This is a known limitation; "do not downgrade" is stated in the release notes.
- **User deletes the app and reinstalls.** Keychain persists past app deletion (by default). App Group UserDefaults do too. On reinstall, the migration version is already `1` (suite persisted) so the migrator early-exits. The Keychain token is still present with the migrated class. Nothing to do.

### Ship sequence (PR-level)

The spec recommends a single PR for the whole feature. Splitting into stages (package-only, then widget, then migrations) would need temporary double-placement of files and produce churn. One PR, reviewed holistically, lands the feature cleanly.

---

## Pattern Extension Audit

Widgets are additive — they don't extend an existing patterns table (e.g., they're not a new row type in an existing renderer). The only "pattern extension" is moving internal types to `public` in a new package. Audit:

| Call site of migrated types | Update required? | Rationale |
|-----------------------------|------------------|-----------|
| `Flux/Flux/Dashboard/*`     | `import FluxCore` at top; no other change | Types keep the same names |
| `Flux/Flux/History/*`       | `import FluxCore` | same |
| `Flux/Flux/DayDetail/*`     | `import FluxCore` | same |
| `Flux/Flux/Settings/*`      | `import FluxCore` | same |
| `Flux/Flux/Navigation/*`    | `import FluxCore` | same |
| `Flux/Flux/Helpers/EnergySummaryFormatter.swift` | `import FluxCore` (uses `DayEnergy`, `PowerFormatting`) | same |
| `FluxTests/DashboardViewModelTests.swift` | `import FluxCore` + update mock access | same |
| `FluxTests/HistoryViewModelTests.swift` | `import FluxCore` | same |
| `FluxTests/DayDetailViewModelTests.swift` | `import FluxCore` | same |
| `FluxTests/SettingsViewModelTests.swift` | `import FluxCore` | same |
| `FluxTests/EnergySummaryFormatterTests.swift` | `import FluxCore` | same |
| `FluxTests/FluxTests.swift` | `import FluxCore` | same |

Migrated tests (`APIModelsTests`, `DateFormattingTests`, `ColoringTests`, `KeychainServiceTests`, `URLSessionAPIClientTests`) move under `Packages/FluxCore/Tests/FluxCoreTests/`.

---

## UI Consistency References

Each widget view references an existing Dashboard component as its visual baseline:

| Widget view                   | Dashboard baseline                              | Deviations |
|-------------------------------|-------------------------------------------------|------------|
| SOC hero (all families)       | `BatteryHeroView`                               | Text size scales per family; no ProgressView in small/medium (vertical space taken by power trio). |
| Status line                   | `BatteryHeroView.statusLine`                    | `.short` variant omits "Discharging at X · cutoff" for `accessoryRectangular`. |
| Power trio columns            | `PowerTrioView.metricColumn`                    | Compact typography; secondary/primary foregroundStyle unchanged; colour rules verbatim. |
| systemSmall load row          | `PowerTrioView`'s Load column in isolation      | Same formatter and same red-above-threshold rule. |
| systemLarge stats row         | `SecondaryStatsView`'s 24h-low + off-peak rows  | Same formatters; no 15-min rolling load (cutoff already in status line). |
| Offline banner                | `DashboardView.stalenessBanner`                 | Widget shows compact icon/word, not the full banner with retry+settings. |

Colouring rules come from `BatteryColor.forSOC`, `GridColor.forGrid`, and `CutoffTimeColor.forCutoff` verbatim — the widget never redefines a threshold.

---

## Open questions deferred to implementation

These are intentionally left for tasks-phase to decide when the code is being written; they do not affect the design contract:

1. Exact font weights per family — pick during layout work; match iOS widget style guidelines.
2. Whether `SystemSmallView` omits the divider line under Dynamic Type `.xLarge`+ to save vertical space. Small refinement.
3. Whether `TimelineEntryRelevance` scores are assigned or left default. Nice-to-have; skip if it adds complexity.
4. Icon choice for the `accessoryRectangular` prefix glyph (currently `battery.75` placeholder).

These are layout polish questions with no cross-cutting effect.
