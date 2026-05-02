# Mac App — Native Shell

The macOS build (T-1081) is a native SwiftUI app — no Mac Catalyst, no "Designed for iPad". It reuses FluxCore, the widget extension, and ~all of the iOS view code; divergences are gated by `#if os(macOS)` at modifier or section granularity.

## Lifecycle and scenes

- `FluxApp.swift` — On macOS only: an `@NSApplicationDelegateAdaptor(FluxAppDelegate.self)` and a `@State refreshCoordinator = FluxRefreshCoordinator()`. The `body` declares two scenes on macOS — the main `WindowGroup { AppNavigationView().environment(refreshCoordinator) }` plus `Settings { SettingsView().frame(minWidth: 480, minHeight: 360) }` — and `.commands { FluxKeyboardCommands(coordinator: refreshCoordinator) }`. iOS keeps a single `WindowGroup`. `iCloudURLMirror.shared.start()` runs in `init()` on both platforms.
- `Flux/Flux/Mac/FluxAppDelegate.swift` (`#if os(macOS)`) — `NSApplicationDelegate` returning `true` from `applicationShouldTerminateAfterLastWindowClosed(_:)`. Closing the main window quits the app and dismisses any open Settings window (Decision 4).

## ⌘R refresh dispatch

- `Flux/Flux/Mac/FluxRefreshCoordinator.swift` — `@MainActor @Observable` class with a single optional `var refresh: (@MainActor () -> Void)?`. `@Observable` is required for `.environment(_:)` injection; nothing actually observes the closure.
- `Flux/Flux/Mac/FluxKeyboardCommands.swift` (`#if os(macOS)`) — `Commands` struct binding ⌘R to `coordinator.refresh?()` via `CommandGroup(after: .toolbar)`.
- `Flux/Flux/Mac/MacRefreshActionModifier.swift` (`#if os(macOS)`) — Internal `View.macRefreshAction { … }` modifier that reads `@Environment(FluxRefreshCoordinator.self)` and on `.onAppear` sets `coordinator.refresh = { Task { await action() } }`, then clears it on `.onDisappear`. Each top-level macOS screen (`DashboardView`, `HistoryView`, `DayDetailView`) calls `.macRefreshAction { await viewModel.<refreshMethod>() }` so ⌘R hits the topmost view's refresh path. Last-write-wins is acceptable because only one screen is visible at a time.

## Refresh tier (Mac only)

- `Flux/Flux/Dashboard/AppearsActiveMonitor.swift` (`#if os(macOS)`) — `ViewModifier` reading `@Environment(\.appearsActive)`; on change calls `viewModel.updateActivityTier(.active or .inactive)` with `initial: true`. Applied via `.modifier(AppearsActiveMonitor(viewModel: viewModel))` in `DashboardView` body inside `#if os(macOS)`.
- `DashboardViewModel` — `ActivityTier` enum (`active` → 10s, `inactive` → 60s), `private(set) activityTier`, `updateActivityTier(_:)` (a `.inactive → .active` transition fires one `Task { await refreshIfIdle() }` per Decision 5/req 6.3, blocked by `isLoading` per req 6.4), and a per-tick `currentInterval` read by the auto-refresh loop. iOS retains the `scenePhase`-paused behaviour unchanged via `#if !os(macOS)` on the existing `.onChange(of: scenePhase)` block in `DashboardView` (req 6.5).
- Constructor adds `controlReloadTrigger` and `controlReloadDebounce: TimeInterval = 60`; the default fires `ControlCenter.shared.reloadControls(ofKind: WidgetKinds.controlBattery)` on macOS (req 8.5).

## Credential plane

- `KeychainService` defaults change to `.afterFirstUnlock` accessibility and `synchronizable: Bool = true` (Decision 11). Reads/deletes use `kSecAttrSynchronizableAny` so legacy non-synchronisable items remain readable (Decision 18); a write does delete-before-add via `SynchronizableAny` so any save converges to a single sync item without a one-shot migrator. The pre-existing `KeychainAccessibilityMigrator.run()` in `FluxApp.init()` is left in place — it's an idempotent iOS one-shot from T-843 and is orthogonal to the new defaults.
- `iCloudURLMirror` — `@MainActor` singleton with `start()` / `write(_:)` / `stop()`. `start()` synchronises the KVS, seeds defaults, and registers an async-sequence observer on `NSUbiquitousKeyValueStore.didChangeExternallyNotification` that updates the App Group `UserDefaults` mirror on `MainActor`. `write(_:)` updates both stores. The widget extension reads the URL from `UserDefaults.fluxAppGroup.apiURL` exactly as before; the host is the only writer of the KVS source-of-truth (Decision 12). Public `KeyValueStore` protocol with a default `NSUbiquitousKeyValueStore` conformance lets unit tests inject an in-memory double.
- `SettingsViewModel.save()` writes the URL via an injected `writeURL` closure (default `{ iCloudURLMirror.shared.write($0) }`) and posts `Notification.Name.fluxCredentialsChanged` after both URL and token are saved. `AppNavigationView` listens for that notification and calls `reloadDependencies()` — the `MacUnconfiguredView` empty-state CTA flips to the live Dashboard within one auto-refresh tick (req 4.4).

## Navigation chrome

- `AppNavigationView` — `@SceneStorage("flux.sidebar.selectedScreen")` mirror on macOS persists sidebar selection per-window. `.scrollContentBackground(.hidden)` on the macOS detail column. `.onReceive(.fluxCredentialsChanged)` calls `reloadDependencies()`. Empty-state CTA on macOS is a `MacUnconfiguredView` carrying a `SettingsLink` button (req 4.3) instead of the iOS sheet flow.
- `SidebarView` accepts an `items: [Screen]` parameter defaulting to a new `Screen.sidebarVisible` static helper that filters out `.settings` on macOS (req 3.2). `.backgroundExtensionEffect()` is applied to the List on macOS (req 9.2).
- `DayDetailView` — `.onKeyPress(.leftArrow)` / `.onKeyPress(.rightArrow)` on macOS (req 5.2) call `viewModel.navigatePrevious()` / `navigateNext()`; `→` returns `.ignored` when `viewModel.isToday`. `HistoryViewModel` exposes `private(set) lastRequestedDays` and a `reload()` method so the refresh-coordinator closure has a stable target.
- The settings sheet and `topBarTrailing` toolbar items in `DashboardView`, `HistoryView`, and `DayDetailView` are gated `#if !os(macOS)` since macOS uses the Settings scene + ⌘, instead. "Settings" buttons in error/staleness banners use `SettingsLink` on macOS.
- `Flux/Flux/Settings/SettingsView.swift` — iOS-only modifiers (`textInputAutocapitalization(_:)`, `keyboardType(_:)`) are gated `#if !os(macOS)` so the same Form renders on both platforms.

## Widget extension

- `FluxWidgetsBundle` gates `FluxAccessoryWidget()` with `#if os(iOS)` (lock-screen accessory family is iOS-only) and registers `FluxControlWidget()` under `#if os(macOS)`. `FluxBatteryWidget()` remains on both.
- `Flux/FluxWidgets/WidgetRuntime.swift` — Internal `enum WidgetRuntime` shared between `StatusTimelineProvider` and `FluxControlWidget`. Holds a single `URLSession` (5s timeouts, no cache, no `waitsForConnectivity`) and `makeLogic()` / `makeAPIClient(keychain:)` factories. Both providers fetch independently of the host (Decision 13) — the widget extension carries `com.apple.security.network.client` on both platforms.
- `Flux/FluxWidgets/FluxControlWidget.swift` (`#if os(macOS)`) — `ControlWidget` using `StaticControlConfiguration` with a `ControlSOCProvider`. Tap action is `OpenURLIntent(WidgetDeepLink.dashboardURL)` directly — no custom intent struct (Decision 17). The `flux://dashboard` URL is handled by the existing `AppNavigationView.onOpenURL → DeepLinkHandler.handle(...)` path.
- `Flux/Packages/FluxCore/Sources/FluxCore/Widget/ControlSOCProvider.swift` (`#if os(macOS)`) — `SOCValue { percent, stale }` and `ControlSOCProvider: ControlValueProvider`. `currentValue()` returns the cache value with `stale=false` when the envelope is within `cacheStalenessThreshold` (default 600s); otherwise it falls through to `StatusTimelineLogic.snapshot(isPreview: false)` and tags `stale = (entry.source != .live)`.
- `WidgetKinds.controlBattery = "FluxControlBattery"` — host triggers `ControlCenter.shared.reloadControls(ofKind:)` on a 60s debounce in addition to the existing 5-min `WidgetCenter.reloadTimelines` debounce.
- `SOCFormatting.symbol(for:)` (FluxCore) returns the appropriate `battery.{0,25,50,75,100}percent` SF Symbol for a given SOC. `SystemMediumView` calls it instead of an inline switch.

## Build / test

- `Makefile` adds `macos-build`, `macos-test`, `macos-lint` mirroring the iOS targets but with `-destination 'platform=macOS,arch=arm64'`. `macos-test` passes `-skip-testing:FluxUITests` since UI tests on macOS are deferred to v1.1 (Decision 15). FluxCore tests run via `swift test` from `Flux/Packages/FluxCore/` and stay platform-agnostic.
- `Flux/Packages/FluxCore/Package.swift` declares `.macOS(.v26)` alongside iOS.
- Per-platform entitlements, Info.plist trim, and the Mac destination on both targets are user-managed manual prerequisites — see `specs/macos-app/prerequisites.md`. The widget-extension's macOS embed validation will fail until those are completed.

## Cross-platform widget gating

- `WidgetAccessibility.swift` and `WidgetAccessibilityTests.swift` gate the iOS-only accessory families (`.accessoryInline`, `.accessoryCircular`, `.accessoryRectangular`) with `#if !os(macOS)` / `#if os(iOS)`. macOS doesn't ship lock-screen accessory families, so referencing them would fail to compile.
- `FluxAccessoryWidget()` is removed from the bundle on macOS (`#if os(iOS)` in `FluxWidgetsBundle`); `FluxControlWidget()` is added under `#if os(macOS)`. `FluxBatteryWidget()` (system small/medium/large) ships on both.
- `SystemMediumView` calls `SOCFormatting.symbol(for:)` (FluxCore) instead of an inline switch so the Control widget reuses the same SOC → battery SF Symbol mapping.

## Testing notes

- `KeychainServiceTests` runs `swift test` against the host process. On a bare `swift test` invocation the test bundle has no Keychain entitlements and `SecItemAdd` returns `errSecMissingEntitlement` (-34018). The suite probes for this once at setup and skips the affected tests rather than failing — Xcode-driven runs (with the proper entitlements) execute them normally. `makeUniqueIds` / `baseQuery` / `writeRawItem` / `countItemsAcrossVariants` / `cleanupAllVariants` helpers dedupe the per-test boilerplate around seeding both legacy and synchronisable items.
- `iCloudURLMirrorTests` injects a private `NotificationCenter` and posts the real `NSUbiquitousKeyValueStore.didChangeExternallyNotification` rather than calling an internal sync method directly. The mirror's async-sequence observer runs on the same `MainActor` and the test asserts the resulting mirror state. Keeps the production API (`start`/`write`/`stop`) as the only public surface.
- `SettingsViewModelTests` injects a `writeURL` closure into `SettingsViewModel` instead of mocking the singleton. Production default is `iCloudURLMirror.shared.write`; tests capture the URL into a recorder.
- `DashboardViewModelActivityTierTests` (split out into its own file to stay under SwiftLint's per-file line cap) covers the in-flight refresh guard, immediate refresh on `inactive → active`, and the per-tick `currentInterval` read.
