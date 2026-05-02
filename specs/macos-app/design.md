# Design: Mac App

## Overview

Native macOS 26+ build of Flux that reuses FluxCore, the existing widget extension, and ~all of the iOS view code, with platform-specific shells for the parts that diverge: app entry point (`Settings` scene + commands + `NSApplicationDelegate`), navigation chrome (sidebar filter, Liquid Glass modifiers), Dashboard refresh tier signal (Mac-only), credential plane (iCloud Keychain + `NSUbiquitousKeyValueStore` mirror — no migrator), and a new Control Center widget.

## Architecture

### Shared vs platform-specific

| Bucket | Examples |
|---|---|
| Shared (no `#if`) | All view models, all detail views, all chart code, FluxCore extensions, `KeychainService` (with extended attributes), `URLSessionAPIClient`, `iCloudURLMirror` |
| Platform-conditional (`#if os(macOS)` at modifier or section granularity) | `FluxApp` (Settings scene, commands, app delegate adaptor), `AppNavigationView` (sidebar filter, scroll background, `@SceneStorage`), `SidebarView` (filter Settings), History/Day Detail scroll views (scrollContentBackground hidden), `DashboardView` (tier monitor) |
| Platform-specific files | `FluxAppDelegate.swift` (Mac-only), `FluxControlWidget.swift` (Mac-only), `FluxKeyboardCommands.swift` (Mac-only) |

Style guide §8: prefer `#if` at the smallest unit. Two-shell split (§5) is not warranted — divergence is well under 30% per view.

### Build configuration changes

| Surface | Change |
|---|---|
| `Flux.xcodeproj` Flux app target | Add "My Mac" run destination. Set `SUPPORTS_MAC_DESIGNED_FOR_IPHONE_IPAD = NO`. Per-platform entitlements via separate file references on the macOS destination. |
| `Flux.xcodeproj` FluxWidgetsExtension target | Same Mac destination + `SUPPORTS_MAC_DESIGNED_FOR_IPHONE_IPAD = NO`. Per-platform entitlements. |
| `Flux/Packages/FluxCore/Package.swift` | Add `.macOS(.v26)` to `platforms`. |
| `Makefile` | Add `macos-build`, `macos-test`, `macos-lint`, `macos-archive`, `macos-upload`. Mirror the iOS targets with `-destination 'platform=macOS,arch=arm64'`. |
| `Flux/Flux/Info.plist` (macOS variant) | Strip `aps-environment` and `UIBackgroundModes` from the macOS Info.plist (iOS-only keys; `aps-environment` triggers a sandbox warning on Mac if left). |
| Bundle identifier | Single `me.nore.ig.Flux` for both platforms (Decision 16). |

Hand-editing `pbxproj` is unavoidable for adding the Mac destination and per-platform entitlements/Info.plist. Style guide §1 says don't hand-edit it — these changes are made via Xcode UI, then the resulting diff is reviewed.

### Per-platform entitlements

iOS (`Flux.entitlements` — unchanged):

```xml
aps-environment = development
com.apple.security.application-groups = [group.me.nore.ig.flux]
keychain-access-groups = [$(AppIdentifierPrefix)group.me.nore.ig.flux]
```

macOS (`Flux-macOS.entitlements` — new):

```xml
com.apple.security.app-sandbox = true
com.apple.security.network.client = true
com.apple.security.application-groups = [group.me.nore.ig.flux]
keychain-access-groups = [$(AppIdentifierPrefix)group.me.nore.ig.flux]
com.apple.developer.ubiquity-kvstore-identifier = $(TeamIdentifierPrefix)$(CFBundleIdentifier)
```

The `ubiquity-kvstore-identifier` line is required for `NSUbiquitousKeyValueStore` writes to actually persist under App Sandbox (peer-review consensus is mixed; over-entitling is harmless, under-entitling silently breaks Decision 12). Enabled in Xcode via "Signing & Capabilities → + Capability → iCloud → Key-value storage".

Widget extension iOS (`FluxWidgetsExtension.entitlements` — extend with `network.client`):

```xml
com.apple.security.application-groups = [group.me.nore.ig.flux]
keychain-access-groups = [$(AppIdentifierPrefix)group.me.nore.ig.flux]
com.apple.security.network.client = true     # NEW — req 8.6 / Decision 13
```

Widget extension macOS (`FluxWidgetsExtension-macOS.entitlements` — new):

```xml
com.apple.security.app-sandbox = true
com.apple.security.network.client = true
com.apple.security.application-groups = [group.me.nore.ig.flux]
keychain-access-groups = [$(AppIdentifierPrefix)group.me.nore.ig.flux]
```

The widget extension does not need iCloud KVS — it reads the URL from App Group `UserDefaults` (mirrored by the host).

### Integration map

```
FluxApp.swift  ────────  FluxAppDelegate (NEW, macOS-only)
       │                       └─ applicationShouldTerminateAfterLastWindowClosed → true
       ├── WindowGroup { AppNavigationView() }
       ├── #if os(macOS) Settings { SettingsView() } #endif        (req 4.1)
       └── #if os(macOS) .commands { FluxKeyboardCommands() }      (req 5.x)

AppNavigationView        ────────  @SceneStorage("flux.sidebar")   (req 3.5)
       │                                    └─ replaces @State for selectedScreen on macOS
       ├── SidebarView(items: visibleScreens)
       │     └─ #if os(macOS) filters out .settings                (req 3.2)
       ├── List(...)
       │     └─ #if os(macOS) .backgroundExtensionEffect()         (req 9.2)
       ├── detail
       │     └─ ScrollView
       │          └─ #if os(macOS) .scrollContentBackground(.hidden)  (req 9.1)
       ├── .onOpenURL { DeepLinkHandler.handle(...) }              (existing — reused for OpenFluxIntent)
       └── .onReceive(.fluxCredentialsChanged) { reloadDependencies() }  (req 4.4)

DashboardView (macOS)    ────────  AppearsActiveMonitor (NEW, macOS-only)
       │                            └─ updates DashboardViewModel.activityState
       ├── @FocusedValue(\.fluxRefreshAction)                      (req 5.1, ⌘R)
       └── ...

DashboardViewModel       ────────  refresh tier provider (macOS uses tiers; iOS unchanged)
       └── existing refresh loop reads tier interval per tick

KeychainService          ────────  defaults change + SynchronizableAny on read/delete (req 7.1-7.3)
       │                            └─ no migrator (Decision 18)

UserDefaults+Settings    ────────  iCloudURLMirror (NEW)
       │                            ├─ NSUbiquitousKeyValueStore is source of truth
       │                            └─ mirrors to App Group UserDefaults on change

FluxWidgetsBundle        ────────  unchanged for systemSmall/Medium/Large
       │                       │
       │                       ├── #if os(iOS) FluxAccessoryWidget()       (req 8.3)
       │                       └── #if os(macOS) FluxControlWidget()       (req 8.2)
```

## Components and Interfaces

### `FluxAppDelegate` (new, macOS-only)

```swift
#if os(macOS)
import AppKit

final class FluxAppDelegate: NSObject, NSApplicationDelegate {
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true   // requirement 3.4
    }
}
#endif
```

(Default-actor isolation in Swift 6.2 places this on `MainActor` automatically; explicit `@MainActor` is redundant and can collide with `NSApplicationDelegate` protocol nonisolated requirements.)

Wired via `@NSApplicationDelegateAdaptor(FluxAppDelegate.self)` inside `#if os(macOS)`.

**Open behaviour to verify**: AppKit's "last window" check — does the SwiftUI `Settings { … }` scene window count? Apple docs are not explicit. The expected behaviour is that closing the main window terminates the app and any open Settings window goes with it. **Fallback if it doesn't**: observe `NSWindow.willCloseNotification` filtered to the main window's title/identifier and call `NSApp.terminate(nil)` from the observer. Verify on first Mac build before adding the fallback.

### `FluxApp.swift` (modified)

```swift
@main
struct FluxApp: App {
    #if os(macOS)
    @NSApplicationDelegateAdaptor(FluxAppDelegate.self) private var appDelegate
    #endif

    init() {
        SettingsSuiteMigrator.run()                    // existing
        KeychainAccessibilityMigrator.run()            // existing iOS one-shot — left as-is
        iCloudURLMirror.shared.start()                 // NEW — req 7.4
    }

    var body: some Scene {
        WindowGroup {
            AppNavigationView()
        }
        .modelContainer(for: CachedDayEnergy.self)
        #if os(macOS)
        .commands {                                    // req 5.1
            FluxKeyboardCommands()
        }

        Settings {                                     // req 4.1
            SettingsView()
                .frame(minWidth: 480, minHeight: 360)
        }
        #endif
    }
}
```

The existing `KeychainAccessibilityMigrator` is left in place (iOS-only, idempotent, harmless). It targets the legacy accessibility class; new code paths use the updated `KeychainService` defaults. No new migrator is introduced (Decision 18).

### `FluxKeyboardCommands` and refresh coordinator (⌘R)

`@FocusedValue` depends on keyboard focus mechanics that are unreliable across `NavigationStack` push/pop on macOS — when the user has focus in the sidebar (typical), `@FocusedValue(\.refresh)` returns nil and ⌘R disables. To avoid that, refresh is dispatched through a tiny coordinator owned by `FluxApp`:

```swift
@MainActor @Observable
final class FluxRefreshCoordinator {
    var refresh: (@MainActor () -> Void)?
}

#if os(macOS)
struct FluxKeyboardCommands: Commands {
    let coordinator: FluxRefreshCoordinator

    var body: some Commands {
        CommandGroup(after: .toolbar) {
            Button("Refresh") {
                coordinator.refresh?()
            }
            .keyboardShortcut("r", modifiers: .command)
        }
        // ⌘W and ⌘Q are system defaults. ⌘1/⌘2/⌘3 deferred (Decision 6).
    }
}
#endif
```

In `FluxApp`:

```swift
@State private var refreshCoordinator = FluxRefreshCoordinator()

WindowGroup {
    AppNavigationView()
        .environment(refreshCoordinator)
}
#if os(macOS)
.commands {
    FluxKeyboardCommands(coordinator: refreshCoordinator)
}
#endif
```

Each top-level screen sets `coordinator.refresh = { … }` in `.onAppear` (or `.task`) and clears in `.onDisappear`:

| Screen | Closure body |
|---|---|
| `DashboardView` | `{ Task { await viewModel.refresh() } }` |
| `HistoryView` | `{ Task { await viewModel.reload() } }` |
| `DayDetailView` | `{ Task { await viewModel.loadDay() } }` |

When `DayDetailView` is pushed, its `.onAppear` fires after History's, so its closure wins; on pop, History's `.onAppear` re-fires (NavigationStack convention) and re-installs its closure. Deterministic, no focus magic. Concurrency safety: only one screen is visible at a time on the active scene; if both order-edge cases somehow run together (e.g. push and pop almost simultaneously), the worst case is one extra refresh, which is harmless.

`←` / `→` in Day Detail use `.onKeyPress(.leftArrow)` and `.onKeyPress(.rightArrow)` modifiers on the Day Detail root, calling the existing `viewModel.navigatePrevious()` / `navigateNext()` methods. Boundary behaviour follows the existing chevron buttons exactly: `→` no-op when `viewModel.isToday` (mirrors `.disabled(viewModel.isToday)`); `←` follows whatever `navigatePrevious()` does today.

### `KeychainService` changes (Decision 18)

Three small changes — no migrator, no inventory helper, no fallback enum.

1. **Default attributes change**: `accessibility` defaults to `.afterFirstUnlock`; new `synchronizable: Bool = true` parameter.
2. **`loadToken` queries with `SynchronizableAny`**: existing legacy items remain visible.
3. **`saveToken` and `deleteToken` delete via `SynchronizableAny` first**: idempotent convergence to a single synchronisable item.

```swift
public enum KeychainAccessibility: Sendable, Equatable {
    case afterFirstUnlockThisDeviceOnly       // legacy — read-only after default change
    case afterFirstUnlock                     // NEW default (Decision 11)
    case other(String)
    case missing
}

public final class KeychainService: Sendable {
    // ...
    public init(
        service: String = "me.nore.ig.flux",
        account: String = "api-token",
        accessGroup: String? = "group.me.nore.ig.flux",
        accessibility: KeychainAccessibility = .afterFirstUnlock,   // CHANGED
        synchronizable: Bool = true                                  // NEW
    ) { … }

    public func loadToken() -> String? {
        var query = keychainQuery()
        query[kSecReturnData] = kCFBooleanTrue
        query[kSecMatchLimit] = kSecMatchLimitOne
        query[kSecAttrSynchronizable] = kSecAttrSynchronizableAny    // req 7.2
        // ... existing return-Data parsing
    }

    public func saveToken(_ token: String) throws {
        try deleteToken()                                            // req 7.3 — both variants
        var query = keychainQuery()
        query[kSecValueData] = Data(token.utf8)
        query[kSecAttrAccessible] = accessibility.cfString
        query[kSecAttrSynchronizable] = synchronizable
            ? kCFBooleanTrue
            : kCFBooleanFalse
        let status = SecItemAdd(query as CFDictionary, nil)
        // ... existing error handling
    }

    public func deleteToken() throws {
        var query = keychainQuery()
        query[kSecAttrSynchronizable] = kSecAttrSynchronizableAny    // req 7.3
        let status = SecItemDelete(query as CFDictionary)
        // existing handler treats errSecItemNotFound as success
    }
}
```

The existing `keychainQuery()` helper does not set `kSecMatchLimit`, so `SecItemDelete` removes all matches (both legacy and synchronisable). The widget extension instantiates `KeychainService()` exactly as today; the new defaults give it iCloud-aware behaviour for free.

### `iCloudURLMirror` (new)

Owned by the host app target (widget reads from App Group `UserDefaults`).

```swift
@MainActor
public final class iCloudURLMirror {
    public static let shared = iCloudURLMirror()

    private let kvs = NSUbiquitousKeyValueStore.default
    private let defaults: UserDefaults
    private static let key = "apiURL"
    private var observerTask: Task<Void, Never>?

    private init(defaults: UserDefaults = .fluxAppGroup) {
        self.defaults = defaults
    }

    public func start() {
        kvs.synchronize()
        if let remote = kvs.string(forKey: Self.key), !remote.isEmpty {
            defaults.apiURL = remote
        }
        observerTask = Task { [self] in            // singleton — strong capture is fine
            let stream = NotificationCenter.default.notifications(
                named: NSUbiquitousKeyValueStore.didChangeExternallyNotification,
                object: self.kvs
            )
            for await _ in stream {
                self.syncFromRemote()
            }
        }
    }

    private func syncFromRemote() {
        if let remote = kvs.string(forKey: Self.key), remote != defaults.apiURL {
            defaults.apiURL = remote                  // req 7.4 — MainActor-bound
        }
    }

    public func write(_ url: String) {
        defaults.apiURL = url
        kvs.set(url, forKey: Self.key)
        kvs.synchronize()
    }
}
```

Async-sequence pattern (rather than `@objc` + selector) keeps Swift 6 strict concurrency happy: the `for await` loop runs on `MainActor`, so the body's `defaults.apiURL = remote` is automatically isolated. No thread-hopping.

`SettingsViewModel.save(...)` is updated to call `iCloudURLMirror.shared.write(url)` instead of writing directly to `UserDefaults.fluxAppGroup.apiURL` (see Pattern Audit below).

### Settings save → main window auto-recover (req 4.4)

`SettingsViewModel.save(...)` posts `Notification.Name.fluxCredentialsChanged` after writing the new URL/token. `AppNavigationView` listens via `.onReceive(NotificationCenter.default.publisher(for: .fluxCredentialsChanged))` and calls the existing `reloadDependencies()`. Within one auto-refresh tick (≤10s on Mac), the Dashboard reads the new credentials and renders live.

Single notification, single sender, single listener — no race concerns.

### Sidebar selection persistence (req 3.5)

`AppNavigationView` backs `selectedScreen` with `@SceneStorage` on macOS via a derived `Binding` (computed properties cannot wrap a property wrapper's projected binding cleanly):

```swift
#if os(macOS)
@SceneStorage("flux.sidebar.selectedScreen") private var storedSelection: String = Screen.dashboard.rawValue

private var selectionBinding: Binding<Screen?> {
    Binding(
        get: { Screen(rawValue: storedSelection) ?? .dashboard },
        set: { storedSelection = ($0 ?? .dashboard).rawValue }
    )
}
// SidebarView(selection: selectionBinding)
#else
@State private var selectedScreen: Screen? = .dashboard
// SidebarView(selection: $selectedScreen)
#endif
```

`@SceneStorage` is per-window, so multiple Mac windows would maintain independent sidebar state — moot for v1 (Decision 4: single window).

### `DashboardViewModel` refresh tier (Mac only)

iOS keeps the existing `scenePhase`-paused behaviour. macOS adds a tier signal.

```swift
@MainActor @Observable
final class DashboardViewModel {
    // ...
    public enum ActivityTier: Sendable, Equatable {
        case active           // 10s
        case inactive         // 60s — Mac only; iOS uses scenePhase pause instead
    }
    private(set) var activityTier: ActivityTier = .active

    private var currentInterval: Duration {
        switch activityTier {
        case .active: .seconds(10)
        case .inactive: .seconds(60)
        }
    }

    public func updateActivityTier(_ new: ActivityTier) {
        let wasActive = (activityTier == .active)
        activityTier = new
        if !wasActive && new == .active {
            Task { await refreshIfIdle() }       // req 6.3 immediate refresh on key
        }
    }

    private func refreshIfIdle() async {
        guard !isLoading else { return }         // req 6.4 — no parallel
        await refresh()
    }
}
```

The auto-refresh loop changes one line: `try await self.sleep(self.currentInterval)`. The constructor's `refreshInterval` parameter is dropped (test churn is small — existing tests pass `refreshInterval: .seconds(0.01)` for fast sleeps; they switch to passing a `sleep` closure that returns immediately).

The Mac-only monitor:

```swift
#if os(macOS)
struct AppearsActiveMonitor: ViewModifier {
    @Environment(\.appearsActive) private var appearsActive
    let viewModel: DashboardViewModel

    func body(content: Content) -> some View {
        content.onChange(of: appearsActive, initial: true) { _, isActive in
            viewModel.updateActivityTier(isActive ? .active : .inactive)
        }
    }
}
#endif
```

Applied in `DashboardView` body inside `#if os(macOS)`. iOS continues to use the existing `.onAppear { startAutoRefresh() }` / `scenePhase` pattern unchanged (req 6.5).

### Widget extension changes

`FluxWidgetsBundle.swift`:

```swift
@main
struct FluxWidgetsBundle: WidgetBundle {
    var body: some Widget {
        FluxBatteryWidget()
        #if os(iOS)
        FluxAccessoryWidget()
        #endif
        #if os(macOS)
        FluxControlWidget()
        #endif
    }
}
```

`FluxControlWidget.swift` (new, macOS-only):

```swift
#if os(macOS)
import AppIntents
import FluxCore
import SwiftUI
import WidgetKit

struct FluxControlWidget: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(
            kind: WidgetKinds.controlBattery,
            provider: ControlSOCProvider()
        ) { value in
            ControlWidgetButton(
                action: OpenURLIntent(URL(string: "flux://dashboard")!)
            ) {
                Label("\(Int(value.percent))%",
                      systemImage: SOCFormatting.symbol(for: value.percent))
            }
        }
        .displayName("Flux Battery")
        .description("Live battery state")
    }
}

struct SOCValue: Sendable {
    let percent: Double
    let stale: Bool
}

struct ControlSOCProvider: ControlValueProvider {
    let placeholder = SOCValue(percent: 0, stale: true)

    func currentValue() async throws -> SOCValue {
        let cache = WidgetSnapshotCache()
        if let cached = cache.read(),
           Date().timeIntervalSince(cached.fetchedAt) < 600 {
            return SOCValue(percent: cached.status.soc, stale: false)
        }
        let entry = await StatusTimelineLogic.makeDefault().snapshot(isPreview: false)
        return SOCValue(percent: entry.soc ?? 0, stale: entry.source != .live)
    }
}
#endif
```

Notes:
- Tap navigation uses Apple's built-in `OpenURLIntent` directly — no custom intent struct. The URL `flux://dashboard` is handled by the existing `AppNavigationView.onOpenURL` → `DeepLinkHandler.handle(...)` path, which already routes to Dashboard. Zero new navigation code needed in the host.
- `StatusTimelineLogic.snapshot(isPreview:)` is read-only against the cache (verify during implementation; if it does write, `ControlSOCProvider` reads the cache directly via `WidgetSnapshotCache.read()` only and returns a placeholder when stale).

`WidgetKinds.controlBattery` is a new kind constant. Refresh trigger from the host: `ControlCenter.shared.reloadControls(ofKind: WidgetKinds.controlBattery)` after every successful Dashboard fetch, **debounced to 60s** (mirrors the home-screen widget debounce; doesn't rely on undocumented Apple dedup):

```swift
// In DashboardViewModel after a successful fetch + cache write:
if shouldTriggerControlReload(at: fetchedAt) {
    ControlCenter.shared.reloadControls(ofKind: WidgetKinds.controlBattery)
    lastControlReload = fetchedAt
}
```

### iOS `KeychainAccessibilityMigrator` interaction

Left in place. It's an idempotent iOS one-shot that has already done its job for existing users. The new defaults take over for any subsequent writes; no interaction required.

### Pattern Extension Audit — `KeychainService` and `apiURL`

Call sites that need verification:

| Call site | File | Needs change | Notes |
|---|---|---|---|
| iOS app initial wiring | `AppNavigationView.makeAPIClient()` | No | Constructed with default args; new defaults apply automatically. |
| Settings validation probe | `SettingsViewModel.validateToken()` | No | Same — uses defaults. |
| Widget timeline provider | `StatusTimelineProvider.makeLogic()` | No | Same — uses defaults. |
| Tests | `Flux/FluxTests/KeychainAccessibilityMigratorTests.swift`, `Flux/Packages/FluxCore/Tests/FluxCoreTests/KeychainServiceTests.swift` | **Yes** | Updated assertions for new default accessibility (`afterFirstUnlock`); tests for SynchronizableAny read/delete behaviour. |
| **Settings save (URL write)** | `SettingsViewModel.save(...)` | **Yes** | Currently writes directly to `UserDefaults.fluxAppGroup.apiURL`; switch to `iCloudURLMirror.shared.write(url)` so the iCloud KVS source-of-truth stays in sync. |
| Widget URL read | `StatusTimelineProvider.makeAPIClient()` | No | Reads from `UserDefaults.fluxAppGroup.apiURL` — correct (the mirror writes there). |
| Host URL read on launch | `AppNavigationView.makeAPIClient()` | No | Reads from `UserDefaults.fluxAppGroup.apiURL` — correct, after `iCloudURLMirror.start()` runs in `FluxApp.init()`. |
| `apiURL` public setter | `UserDefaults+Settings.swift:28` | No (convention only) | The setter remains public for the mirror to use; convention is "host writes through `iCloudURLMirror.write`". Not constrained in code — two-person app; documented here. |

## Data Models

No new persistent or API models. New value types: `ActivityTier` (enum), `SOCValue` (struct, widget-internal).

## Error Handling

- **iCloud KVS unavailable / signed out**: `NSUbiquitousKeyValueStore` writes silently no-op; the local `UserDefaults` mirror still works. App continues with the most recent URL it has.
- **iCloud Keychain disabled**: `SecItemAdd` with `synchronizable=true` succeeds locally; `loadToken` finds it via `SynchronizableAny`. No separate fallback or Settings diagnostic.
- **No token found**: existing empty-state CTA routes the user to Settings (req 7.6). Same as today.
- **401 on the wire**: existing `FluxAPIError.unauthorized` path; the next request re-reads the token from Keychain via the existing `tokenProvider` closure. Token rotation via iCloud Keychain is picked up automatically (req 7.5).
- **Indeterminate Keychain read (both legacy and sync items briefly coexist)**: `loadToken` may return either; if the user has rotated the token between platform installs, the stale variant could win and produce a 401. The existing 401 → empty-state-CTA → Settings flow is the recovery path. Worst case: one re-save, after which `saveToken`'s SynchronizableAny delete leaves a single sync item and steady state.
- **Widget network failure** (req 8.6 fetch path): `StatusTimelineLogic` already returns a cache-fallback or placeholder entry; rendering unchanged.
- **App termination edge case**: if `applicationShouldTerminateAfterLastWindowClosed` does not actually terminate when the Settings window is open, fall back to observing `NSWindow.willCloseNotification` on the main window and calling `NSApp.terminate(nil)`. Verify before adding.

## Testing Strategy

| Coverage area | Requirements covered | Test target | Approach |
|---|---|---|---|
| `KeychainService` — SynchronizableAny read finds legacy items | [7.1](requirements.md#7.1), [7.2](requirements.md#7.2) | FluxCoreTests | Integration-style test: write a non-synchronisable item via the legacy code path (or directly via `SecItemAdd` with `synchronizable=false`), then `loadToken()` returns the value. |
| `KeychainService` — SynchronizableAny delete removes both variants | [7.3](requirements.md#7.3) | FluxCoreTests | Write both variants, call `deleteToken()`, verify both gone via direct `SecItemCopyMatching`. |
| `KeychainService` — `saveToken` converges to single sync item | [7.3](requirements.md#7.3) | FluxCoreTests | Write a non-sync item, then `saveToken("new")`, then assert exactly one item with `synchronizable=true` and value `"new"`. |
| `iCloudURLMirror` | [7.4](requirements.md#7.4) | FluxCoreTests (or FluxTests) | Inject test doubles for `NSUbiquitousKeyValueStore` and `UserDefaults`; verify external-change notification mirrors to defaults; verify `write(...)` updates both stores. |
| `DashboardViewModel` tier transitions (macOS) | [6.1](requirements.md#6.1)–[6.4](requirements.md#6.4) | FluxTests | Inject `sleep` closure; call `updateActivityTier(.inactive)` then `.active`; assert `refresh()` is called once on transition; assert no parallel refresh when one is in flight. |
| Day Detail keyboard nav boundary | [5.2](requirements.md#5.2) | FluxTests (view model level) | Existing `navigatePrevious()` / `navigateNext()` tests; add `→` no-op-when-today assertion if missing. |
| Sidebar Settings filter on macOS | [3.2](requirements.md#3.2) | FluxTests | Pure helper test on `Screen.visible(for:)` (a small computed accessor used by `SidebarView`). |
| `FluxAppDelegate.applicationShouldTerminateAfterLastWindowClosed` | [3.4](requirements.md#3.4) | FluxTests (macOS only) | Trivial unit test instantiating the delegate and calling the method. |
| `OpenFluxIntent` opens Dashboard via deep link | [8.2](requirements.md#8.2), Decision 17 | FluxTests | Verify `perform()` returns an `OpenURLIntent` with URL `flux://dashboard`. The deep-link handler is already covered by existing `WidgetDeepLinkTests`. |
| Widget independence — cache stale → live fetch | [8.6](requirements.md#8.6) | FluxCoreTests | Existing `StatusTimelineLogic` cache-stale tests cover the path; manual verification on Mac TestFlight for the entitlement. |
| Visual checks (Liquid Glass, scroll backgrounds, widget rendering) | [9.1](requirements.md#9.1)–[9.4](requirements.md#9.4), [8.1](requirements.md#8.1)–[8.2](requirements.md#8.2) | Manual (macOS) | Visual inspection during dev; no automated coverage in v1 (Decision 15). |

PBT is not warranted — the new logic is stateful and case-driven, not invariant-based.
