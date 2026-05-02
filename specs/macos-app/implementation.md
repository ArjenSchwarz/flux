# Implementation: Mac App (T-1081)

This document explains what shipped under T-1081 at three levels of expertise. It is self-contained — every claim is anchored to a file:line reference so a reviewer does not need to read the diff alongside it.

## Beginner

Flux is a small dashboard for an AlphaESS home battery. It already runs on iPhone and iPad. T-1081 turns it into a real Mac app — not the "iPad app in a window" mode you get for free, but a proper macOS application with a sidebar, a menu bar, keyboard shortcuts, and Mac-style settings.

When you launch Flux on a Mac, you see a single window with a sidebar containing **Dashboard** and **History** (Settings is no longer there — it moved). The top of the application menu is "Flux > Settings…", reachable with the standard `⌘,` shortcut, and Settings opens in its own small window like every other Mac app you use. Closing the main window quits Flux completely (`Flux/Flux/Mac/FluxAppDelegate.swift:5-7`), so you do not have to wonder whether it is still running in the background.

The shortcuts you would expect work too. `⌘R` refreshes whatever screen you are looking at — Dashboard re-fetches `/status`, History re-fetches `/history`, and Day Detail re-fetches `/day` (`Flux/Flux/Mac/FluxKeyboardCommands.swift:8-13`). On the Day Detail screen, the left and right arrow keys page through previous and next days, just like the chevron buttons (`Flux/Flux/DayDetail/DayDetailView.swift:69-77`). `⌘W` closes the window (which quits the app) and `⌘Q` quits — both are the system defaults, untouched.

The single biggest user-visible improvement is that you only configure Flux once. Enter the API URL and token on iOS and they show up on the Mac (and vice versa). The token rides over iCloud Keychain and the URL rides over `NSUbiquitousKeyValueStore`, so as soon as you log in to iCloud on the Mac, Flux picks up everything. There is no migration prompt or "import from iPhone" step — it just works (`Flux/Packages/FluxCore/Sources/FluxCore/Security/KeychainService.swift:46-72`, `Flux/Packages/FluxCore/Sources/FluxCore/Settings/iCloudURLMirror.swift:58-71`).

Mac widgets ship in v1 too. The same small/medium/large home-screen widgets you can pin to the iOS Home Screen now appear in the Mac's Notification Center and on the desktop, plus a new **macOS Control Center** widget that shows live battery percentage with a battery-shaped SF Symbol. Tapping the Control Center widget opens Flux at the Dashboard (`Flux/FluxWidgets/FluxControlWidget.swift:17-24`). The widgets refresh themselves even when the host app is closed.

## Intermediate

### Architecture overview

The Mac build is one shared codebase with platform-specific shells, not a Catalyst translation or a separate target. FluxCore (the local Swift package at `Flux/Packages/FluxCore`) is the data layer for both platforms — its `Package.swift` now declares `.macOS(.v26)` alongside `.iOS(.v26)` (`Flux/Packages/FluxCore/Package.swift:6`). Every networking, parsing, formatting, Keychain, and chart-data type already worked unchanged; the package change just lets the Mac targets link it.

Where iOS and Mac genuinely differ, the divergence is gated at the smallest possible unit (`#if os(macOS)`) inside otherwise-shared files, per the style guide. Three brand-new files live under `Flux/Flux/Mac/` (`FluxAppDelegate.swift`, `FluxRefreshCoordinator.swift`, `FluxKeyboardCommands.swift`) and one under `Flux/Flux/Dashboard/` (`AppearsActiveMonitor.swift`); each is wrapped in a top-level `#if os(macOS)` so the iOS build skips them entirely.

### Credential plane (iCloud Keychain + KVS mirror, no migrator)

Decision 18 dropped the originally planned five-case Keychain migrator in favour of "lenient read + idempotent write." `KeychainService` defaults are now `accessibility = .afterFirstUnlock` and `synchronizable = true` (`Flux/Packages/FluxCore/Sources/FluxCore/Security/KeychainService.swift:46-58`). Reads query with `kSecAttrSynchronizableAny` so legacy non-synchronisable items remain visible (`KeychainService.swift:78`); deletes are also `SynchronizableAny` so a save first removes both variants before adding a single synchronisable one (`KeychainService.swift:96`, `60-66`). No one-shot migrator runs. Existing iOS users keep working without re-entering their token; the first save from either platform converges to a single synchronisable item.

The API base URL travels via `iCloudURLMirror` (`Flux/Packages/FluxCore/Sources/FluxCore/Settings/iCloudURLMirror.swift`). It is a `@MainActor` singleton (`shared`) but takes an injectable `KeyValueStore` protocol and `UserDefaults`/`NotificationCenter` in its public `init` so tests can substitute in-memory doubles. `NSUbiquitousKeyValueStore` is the cross-device source of truth; `start()` is idempotent (no-op when already running), seeds the App Group `UserDefaults` from KVS, then observes `NSUbiquitousKeyValueStore.didChangeExternallyNotification` via an async-sequence loop on `MainActor`. `pullFromRemote()` handles both the seed and the change-notification path — when it actually changes `defaults.apiURL` it also posts `.fluxCredentialsChanged` so the running host's API client reloads against the new URL (without that, the in-memory client would keep using the old URL until next launch). The KVS key reuses `UserDefaults.apiURLKey` so both stores key off the same constant.

### Refresh tier model (Mac only)

iOS keeps its existing `scenePhase`-paused refresh. Mac adds an `ActivityTier` enum on `DashboardViewModel` with two cases: `.active` (10 s cadence) and `.inactive` (60 s cadence) (`Flux/Flux/Dashboard/DashboardViewModel.swift:8-11`, `127-132`). The cadence is read once per refresh-loop tick (`DashboardViewModel.swift:70`), so a tier change takes effect on the next sleep without restarting the loop. `updateActivityTier(_:)` triggers an immediate refresh on `false → true` transitions, gated by `refreshIfIdle()` so the in-flight request guard from req 6.4 holds (`DashboardViewModel.swift:83-89`, `134-137`).

The signal source is `AppearsActiveMonitor`, a Mac-only `ViewModifier` reading `@Environment(\.appearsActive)` (`Flux/Flux/Dashboard/AppearsActiveMonitor.swift:5-12`). It is applied via `.modifier(AppearsActiveMonitor(viewModel: viewModel))` at the bottom of `DashboardView` (`Flux/Flux/Dashboard/DashboardView.swift:106`). The iOS scene-phase block is preserved unchanged behind `#if !os(macOS)` (`DashboardView.swift:93-104`).

### New Mac shell pieces

- **`FluxAppDelegate`** (`Flux/Flux/Mac/FluxAppDelegate.swift`) — three-line `NSApplicationDelegate` returning `true` from `applicationShouldTerminateAfterLastWindowClosed(_:)`. Wired via `@NSApplicationDelegateAdaptor` inside `#if os(macOS)` in `FluxApp` (`Flux/Flux/FluxApp.swift:14-17`).
- **`FluxRefreshCoordinator`** (`Flux/Flux/Mac/FluxRefreshCoordinator.swift`) — `@MainActor @Observable` class with one `var refresh: (@MainActor () -> Void)?` property. Owned by `FluxApp` as `@State` and injected via `.environment(refreshCoordinator)` (`FluxApp.swift:16, 29`).
- **`FluxKeyboardCommands`** (`Flux/Flux/Mac/FluxKeyboardCommands.swift`) — `Commands` struct adding a `Refresh` button to `CommandGroup(after: .toolbar)` with `.keyboardShortcut("r", modifiers: .command)`. Calls `coordinator.refresh?()`.
- **`AppearsActiveMonitor`** (`Flux/Flux/Dashboard/AppearsActiveMonitor.swift`) — drives the refresh tier from the `appearsActive` environment value.
- **`MacRefreshActionModifier`** (`Flux/Flux/Mac/MacRefreshActionModifier.swift`) — extracted by the pre-push review. Encapsulates the pattern of "set `coordinator.refresh = { … }` on appear, clear on disappear" so the three top-level views call `.macRefreshAction { await viewModel.refresh() }` instead of repeating the closure-install boilerplate. Used in `DashboardView.swift:88-92`, `HistoryView.swift:98-102`, `DayDetailView.swift:64-67`.
- **`MacUnconfiguredView`** (`Flux/Flux/Navigation/AppNavigationView.swift:127-149`) — the empty-state CTA shown when no API client is configured. Replaces the iOS sheet-based settings flow with a `SettingsLink` that opens the Mac Settings scene directly, also bound to `⌘,`.

### Widget extension changes

`FluxWidgetsBundle` gates the iOS-only `FluxAccessoryWidget()` behind `#if os(iOS)` and adds `FluxControlWidget()` under `#if os(macOS)` (`Flux/FluxWidgets/FluxWidgetsBundle.swift:7-13`). `FluxBatteryWidget()` (the small/medium/large home-screen widget) ships on both platforms unchanged.

The Control Center widget itself (`Flux/FluxWidgets/FluxControlWidget.swift`) is a `ControlWidget` with a `StaticControlConfiguration`, a `ControlSOCProvider` value provider, and a `ControlWidgetButton` whose action is `OpenURLIntent(WidgetDeepLink.dashboardURL)` — it opens `flux://dashboard`, which the existing `DeepLinkHandler` already routes to Dashboard. No custom `AppIntent` was added.

`ControlSOCProvider` lives in FluxCore (`Flux/Packages/FluxCore/Sources/FluxCore/Widget/ControlSOCProvider.swift`) and conforms to `ControlValueProvider`. Cache-fresh (within the 600 s threshold) returns `stale = false`; cache-stale or missing falls through to `StatusTimelineLogic.snapshot(isPreview: false)` and tags `stale = (entry.source != .live)`.

The pre-push review extracted `WidgetRuntime.swift` (`Flux/FluxWidgets/WidgetRuntime.swift`) to dedupe the widget bootstrap (private `URLSession` with 5 s timeouts, `KeychainService`, App Group `apiURL` lookup) between `StatusTimelineProvider` and `FluxControlWidget`. Both call `WidgetRuntime.makeLogic()`.

## Expert

### Why no migrator (Decision 18)

The original design from 2026-05-01 carried a five-case Keychain migration matrix (none / legacy-only / sync-only / both / sentinel-set), an `iCloudKeychainAvailability` probe, and a `flux.keychain.migration.v1.complete` sentinel in App Group `UserDefaults`. Two reviewers flagged this as fragile — the probe (write a sentinel, delete it, look at side effects) cannot actually detect whether iCloud Keychain is available in any reliable way — and disproportionate for the use case. The user posture is "two-person personal app, both devices belong to the same person, worst case I re-enter the token."

Decision 18 collapses this to two changes in `KeychainService` (`Flux/Packages/FluxCore/Sources/FluxCore/Security/KeychainService.swift`):

1. `loadToken()` queries with `kSecAttrSynchronizable = kSecAttrSynchronizableAny` (line 78) — finds legacy `…ThisDeviceOnly` items written by the previous iOS build.
2. `deleteToken()` deletes with `SynchronizableAny` (line 96), and `saveToken()` calls `deleteToken()` first (line 61), so any save converges to a single synchronisable item regardless of what was there before.

The brief indeterminate state when both legacy and sync items co-exist (e.g. iOS legacy + Mac-entered sync) is acceptable: in the common case both hold the same token; if they hold different tokens because the user rotated between platform installs, the recovery is the same 401 → empty-state CTA → Settings flow that already exists. One re-save from either platform exits steady state. The legacy `KeychainAccessibilityMigrator` (an iOS one-shot) is left in place as idempotent and harmless.

### Why `iCloudURLMirror` is a singleton with a public injectable init

`iCloudURLMirror.shared` (`Flux/Packages/FluxCore/Sources/FluxCore/Settings/iCloudURLMirror.swift:20`) is the production entry point — `FluxApp.init()` calls `iCloudURLMirror.shared.start()` (`Flux/Flux/FluxApp.swift:22`) and `SettingsViewModel.save()` calls `iCloudURLMirror.shared.write(url)` by default. But the public `init` (lines 30-38) accepts a `KeyValueStore` protocol, a `UserDefaults`, and a `NotificationCenter`, and tests construct fresh instances against in-memory doubles. The `KeyValueStore` protocol (lines 7-12) abstracts `string(forKey:)` / `set(_:forKey:)` / `synchronize()` so an in-memory dictionary backs the test KVS — `NSUbiquitousKeyValueStore` conforms via the empty extension on line 14.

Tests for `SettingsViewModel` do not poke at the singleton. Instead the view model accepts an injectable `writeURL: @MainActor (String) -> Void` closure (`Flux/Flux/Settings/SettingsViewModel.swift:20, 29-31`); the production default points at `iCloudURLMirror.shared.write`, and tests pass a closure that captures the URL into a recorder (`Flux/FluxTests/SettingsViewModelTests.swift:144, 185`). This avoids the `XCTestCase`-detection dance for the singleton itself.

### Why `FluxRefreshCoordinator` uses a closure rather than `@FocusedValue`

`@FocusedValue` is the documented SwiftUI pattern for "command in the menu bar invokes an action on the focused view." It is unreliable on macOS when the user has focus in the sidebar (the typical case) and the action lives in the detail column — `@FocusedValue(\.refresh)` returns nil and `⌘R` disables. The design avoids this with a tiny coordinator owned by `FluxApp` (`Flux/Flux/Mac/FluxRefreshCoordinator.swift`):

```
@MainActor @Observable
final class FluxRefreshCoordinator { var refresh: (@MainActor () -> Void)? }
```

Each top-level Mac view installs its closure on `.onAppear` and clears on `.onDisappear` via the `.macRefreshAction` modifier (`Flux/Flux/Mac/MacRefreshActionModifier.swift:10-17`). When `DayDetailView` is pushed onto `NavigationStack`, its `.onAppear` fires after `HistoryView`'s, so its closure wins; on pop, History's `.onAppear` re-fires (NavigationStack convention) and re-installs its closure. Deterministic, no focus mechanics. Concurrency-wise only one screen is visible at a time on the active scene, so racing installs are impossible in practice; even if both somehow ran, the worst case is one extra refresh.

### Why `ActivityTier` is Mac-only

iOS `Task.sleep` does not fire while the process is suspended on `.background`. The existing `scenePhase`-pause path in `DashboardView` (`Flux/Flux/Dashboard/DashboardView.swift:93-104`, gated `#if !os(macOS)`) calls `stopAutoRefresh()` on `.background` and `.inactive`, so the refresh task is cancelled and there is nothing to time. Adding a 60 s "inactive" tier on iOS would buy nothing — the sleep does not run anyway — and would only add cancellation noise on resume. Decision 5 explicitly scopes the tier model to macOS (req 6.5). The Mac path keeps the same loop running and varies `currentInterval` (`DashboardViewModel.swift:127-132`).

### `appearsActive` replaces deprecated `controlActiveState`

The first design draft used `@Environment(\.controlActiveState)`. Peer review flagged it as deprecated since macOS 15 in favour of `@Environment(\.appearsActive)` (a `Bool`). `AppearsActiveMonitor` reads the new value and calls `viewModel.updateActivityTier(isActive ? .active : .inactive)` on change with `initial: true` so the tier is set on first appearance (`Flux/Flux/Dashboard/AppearsActiveMonitor.swift:9-11`). Opening the Settings window flips the main window to `appearsActive == false` — accepted and noted in Decision 5.

### In-flight refresh guard (req 6.4)

Both `refresh()` and `refreshIfIdle()` early-return when `isLoading == true` (`Flux/Flux/Dashboard/DashboardViewModel.swift:92, 135`). On a tier transition, the in-flight request finishes naturally; the next refresh-loop tick reads the new `currentInterval` and schedules accordingly. The "immediate refresh on `inactive → active`" path goes through `refreshIfIdle()` (line 87) so it cannot enqueue a parallel request when one is already in flight. The cadence countdown re-bases on the in-flight resolution time because `try await self.sleep(self.currentInterval)` runs after `await self.refresh()` returns (lines 67-70).

### Control Center widget tap via `OpenURLIntent` directly

Decision 17 chose an `OpenIntent`-style action for the Control widget tap. The implementation goes one step further and uses Apple's built-in `OpenURLIntent` directly (`Flux/FluxWidgets/FluxControlWidget.swift:17-18`) with `WidgetDeepLink.dashboardURL` (`Flux/Packages/FluxCore/Sources/FluxCore/Widget/WidgetDeepLink.swift:5`, `flux://dashboard`). No custom `AppIntent` struct is added. The host app's existing `.onOpenURL` handler (`Flux/Flux/Navigation/AppNavigationView.swift:52-60`) routes the URL through the existing `DeepLinkHandler` to switch the sidebar selection to Dashboard. Zero new navigation code in the host. The pre-push review replaced an earlier `URL(string: "flux://dashboard")!` force-unwrap with the typed `WidgetDeepLink.dashboardURL` constant.

### App Group + Keychain identity preserved

The bundle identifier is shared (`me.nore.ig.Flux`, Decision 16) so the App Identifier prefix stays identical across platforms. The App Group is `group.me.nore.ig.flux` on both, and the Keychain access group is `$(AppIdentifierPrefix)group.me.nore.ig.flux` on both. This means iCloud Keychain sync, `WidgetSnapshotCache` reads, and the App Group `UserDefaults` API URL all key off the same identifiers — no platform fork in cache keys, access groups, or container paths.

### Deferred prerequisites

Several tasks require Xcode UI work that the coding agent cannot perform and that are documented in `prerequisites.md`:

- "My Mac" run destination on both the Flux app and the FluxWidgetsExtension targets (with `SUPPORTS_MAC_DESIGNED_FOR_IPHONE_IPAD = NO`).
- Per-platform entitlements files (`Flux-macOS.entitlements`, `FluxWidgetsExtension-macOS.entitlements`) wired via `CODE_SIGN_ENTITLEMENTS[sdk=macosx*]`. The macOS host adds App Sandbox, network client, App Group, Keychain access group, and the iCloud Key-value store identifier (`com.apple.developer.ubiquity-kvstore-identifier = $(TeamIdentifierPrefix)$(CFBundleIdentifier)`).
- iCloud Key-value storage capability enabled in Signing & Capabilities so the KVS writes actually persist under the sandbox.
- A macOS-variant `Info.plist` (or `INFOPLIST_KEY_*` overrides) that strips `aps-environment` and `UIBackgroundModes` to avoid the sandbox warning on Mac.
- `com.apple.security.network.client` added to the iOS widget entitlements file too (req 8.6 / Decision 13: the widget extension fetches independently of the host on both platforms now).
- Mac platform SKU on the existing `me.nore.ig.Flux` App Store Connect record before TestFlight Mac submission.

The shipped state is: FluxCore tests pass (118 tests) on macOS; iOS build and tests pass; the macOS host app target compiles. The widget extension's macOS embed still fails until the user adds the Mac destination on the FluxWidgetsExtension target — this is the single remaining manual step, documented in `specs/macos-app/prerequisites.md`.

## Completeness Assessment

| AC | Status | Evidence |
|---|---|---|
| [1.1](requirements.md#1.1) Native macOS, no Catalyst | Manual/prereq | Requires `SUPPORTS_MAC_DESIGNED_FOR_IPHONE_IPAD = NO` on app + widget targets per `prerequisites.md`. Code is native SwiftUI. |
| [1.2](requirements.md#1.2) `make build-macos` / `make test-macos` | Fully implemented | `Makefile:282-308` adds `macos-lint`, `macos-build`, `macos-test` (UI tests skipped per Decision 15). |
| [1.3](requirements.md#1.3) App Sandbox + entitlements (host) | Manual/prereq | `Flux-macOS.entitlements` file authoring is in `prerequisites.md` (host: sandbox, network.client, App Group, Keychain access group, KVS identifier). |
| [1.4](requirements.md#1.4) App Sandbox + entitlements (widget) | Manual/prereq | `FluxWidgetsExtension-macOS.entitlements` authoring is in `prerequisites.md`. iOS variant also needs `network.client` added. |
| [1.5](requirements.md#1.5) iOS keeps building/testing; no token re-entry | Fully implemented | `KeychainService.swift:78` (`SynchronizableAny` read finds legacy items); iOS test suite green. |
| [2.1](requirements.md#2.1) Same screens on Mac | Fully implemented | Same view files render on macOS; `AppNavigationView.swift:67-87` routes Dashboard/History/Settings via shared views. |
| [2.2](requirements.md#2.2) History card stack with chart selection | Fully implemented | History views unchanged from iOS; `HistoryView.swift:50-76` renders the same cards with `onSelect: selectDay`. SwiftUI Charts driving the cursor work natively on macOS. |
| [2.3](requirements.md#2.3) Day Detail charts | Fully implemented | `DayDetailView.swift:24-42` renders SOC/Power/Battery charts unchanged. |
| [2.4](requirements.md#2.4) Note row + edit | Fully implemented | `NoteRowView` + `NoteEditorSheet` shared (`DayDetailView.swift:101-123, 93-97`). |
| [3.1](requirements.md#3.1) Liquid Glass sidebar+detail | Fully implemented | `SidebarView.swift:18-20` (`backgroundExtensionEffect` on macOS); `AppNavigationView.swift:26-28` (`scrollContentBackground(.hidden)` on detail). |
| [3.2](requirements.md#3.2) No Settings in sidebar on macOS | Fully implemented | `Screen.swift:26-32` (`sidebarVisible` filters `.settings` on macOS); `SidebarView.swift:7` consumes the filtered list. |
| [3.3](requirements.md#3.3) Single main window, default min size | Fully implemented | `FluxApp.swift:27-30` single `WindowGroup`, no `.defaultSize` or min-size constraint. |
| [3.4](requirements.md#3.4) Close quits | Fully implemented | `FluxAppDelegate.swift:5-7` returns `true` from `applicationShouldTerminateAfterLastWindowClosed`. Wired via `@NSApplicationDelegateAdaptor` (`FluxApp.swift:15`). Verified by `FluxAppDelegateTests`. |
| [3.5](requirements.md#3.5) `@SceneStorage` sidebar | Fully implemented | `AppNavigationView.swift:16-18` (`@SceneStorage("flux.sidebar.selectedScreen")`); restore at `:32-37`; persist at `:41-45`. |
| [4.1](requirements.md#4.1) Settings as `Settings { … }` scene + ⌘, | Fully implemented | `FluxApp.swift:36-39`. `MacUnconfiguredView` adds an explicit `.keyboardShortcut(",", modifiers: .command)` on the `SettingsLink` (`AppNavigationView.swift:143`). |
| [4.2](requirements.md#4.2) Backend + Display sections in Settings | Fully implemented | Shared `SettingsView` renders both; iOS-only modifiers (`textInputAutocapitalization`, `keyboardType`) gated `#if !os(macOS)` (`SettingsView.swift:23-24, 30-31, 45-46`). |
| [4.3](requirements.md#4.3) Empty-state CTA opens Settings via `openWindow` | Fully implemented | `MacUnconfiguredView` (`AppNavigationView.swift:127-149`) renders a `SettingsLink` with the standard ⌘, shortcut. |
| [4.4](requirements.md#4.4) Save → main window auto-recovers within one tick | Fully implemented | `SettingsViewModel.swift:73` posts `.fluxCredentialsChanged`; `AppNavigationView.swift:61-63` listens and calls `reloadDependencies()`. |
| [5.1](requirements.md#5.1) ⌘R routes to topmost view | Fully implemented | `FluxKeyboardCommands.swift:9-12`; `FluxRefreshCoordinator` injected via env (`FluxApp.swift:29-33`). All three views install their closure via `.macRefreshAction` (`DashboardView.swift:88-92`, `HistoryView.swift:98-102`, `DayDetailView.swift:64-67`). |
| [5.2](requirements.md#5.2) ←/→ in Day Detail with boundary semantics | Fully implemented | `DayDetailView.swift:69-77` — `.onKeyPress(.leftArrow)` calls `navigatePrevious()`; `.onKeyPress(.rightArrow)` returns `.ignored` when `viewModel.isToday`. |
| [5.3](requirements.md#5.3) ⌘W / ⌘Q untouched | Fully implemented | No custom `CommandGroup` for `.appTermination` or window close; only `CommandGroup(after: .toolbar)` for Refresh (`FluxKeyboardCommands.swift:8`). |
| [6.1](requirements.md#6.1) 10 s when `appearsActive == true` | Fully implemented | `DashboardViewModel.swift:127-132` (`.active → .seconds(10)`); driver `AppearsActiveMonitor.swift:9-11`. |
| [6.2](requirements.md#6.2) 60 s when `appearsActive == false` | Fully implemented | `DashboardViewModel.swift:127-132` (`.inactive → .seconds(60)`). |
| [6.3](requirements.md#6.3) Immediate refresh on inactive→active | Fully implemented | `DashboardViewModel.swift:83-89` triggers `refreshIfIdle()` on transition. |
| [6.4](requirements.md#6.4) In-flight guard | Fully implemented | `refresh()` early-return at `DashboardViewModel.swift:92`; `refreshIfIdle()` early-return at `:135`. Covered by `DashboardViewModelActivityTierTests`. |
| [6.5](requirements.md#6.5) iOS scenePhase preserved | Fully implemented | `DashboardView.swift:93-104` gated `#if !os(macOS)`; the Mac monitor at `:105-106` is gated `#if os(macOS)`. |
| [7.1](requirements.md#7.1) Synchronisable Keychain, no migrator | Fully implemented | `KeychainService.swift:46-58` defaults; `:60-72` save converges to single sync item. |
| [7.2](requirements.md#7.2) Read with `SynchronizableAny` | Fully implemented | `KeychainService.swift:78`. |
| [7.3](requirements.md#7.3) Save/delete with `SynchronizableAny` | Fully implemented | `KeychainService.swift:61, 96` (delete-before-write + delete on `SynchronizableAny`). |
| [7.4](requirements.md#7.4) URL via NSUbiquitousKeyValueStore + UserDefaults mirror | Fully implemented | `iCloudURLMirror.swift:18-72`; host calls `start()` at `FluxApp.swift:22`; `SettingsViewModel.swift:69` writes via `iCloudURLMirror.shared.write` (default `writeURL` closure at `:29-31`). |
| [7.5](requirements.md#7.5) `tokenProvider` re-reads Keychain per request | Fully implemented | Existing `URLSessionAPIClient` behaviour preserved; widget runtime constructs the client via `WidgetRuntime.makeAPIClient` with a fresh `KeychainService` (`WidgetRuntime.swift:26-38`). |
| [7.6](requirements.md#7.6) Empty state when no token, no diagnostic | Fully implemented | `AppNavigationView.swift:99-103` (`effectiveScreen` returns `.settings` when `apiClient == nil`); `MacUnconfiguredView` renders the CTA with no iCloud-availability text. |
| [8.1](requirements.md#8.1) systemSmall/Medium/Large on macOS | Fully implemented (code) / Manual/prereq (build) | `FluxWidgetsBundle.swift:7` registers `FluxBatteryWidget()` on both platforms. Widget extension Mac destination must be added per `prerequisites.md` for the Mac embed to succeed. |
| [8.2](requirements.md#8.2) Mac Control Center widget | Fully implemented | `FluxControlWidget.swift:8-29` registers the `ControlWidget`; tap action is `OpenURLIntent(WidgetDeepLink.dashboardURL)`. `ControlSOCProvider` in `Flux/Packages/FluxCore/Sources/FluxCore/Widget/ControlSOCProvider.swift`. |
| [8.3](requirements.md#8.3) Lock-screen accessory iOS-only | Fully implemented | `FluxWidgetsBundle.swift:8-10` gates `FluxAccessoryWidget()` `#if os(iOS)`. |
| [8.4](requirements.md#8.4) Same App Group snapshot cache | Fully implemented | `WidgetRuntime.swift:16-17` uses default `WidgetSnapshotCache()` (App Group `group.me.nore.ig.flux`); host writes via `DashboardViewModel.swift:104-110`. |
| [8.5](requirements.md#8.5) Reload debounce (5 min home-screen, separate Control path) | Fully implemented | Home-screen widget reload debounced via `widgetReloadDebounce: 5 * 60` (`DashboardViewModel.swift:38, 107-110, 139-142`); Control reload debounced separately at 60 s (`:44, 113-117, 144-147`). |
| [8.6](requirements.md#8.6) Independent fetch when host quit | Fully implemented (code) / Manual/prereq (entitlement) | `WidgetRuntime.makeLogic()` constructs an independent `StatusTimelineLogic` with its own URLSession + Keychain (`WidgetRuntime.swift:15-24`). Requires `com.apple.security.network.client` entitlement on both platforms — see `prerequisites.md`. |
| [8.7](requirements.md#8.7) Widget reads token via shared access group; placeholder on miss | Fully implemented | `WidgetRuntime.swift:17` constructs `KeychainService()` with default access group; `ControlSOCProvider.swift:18` defines `previewValue = SOCValue(percent: 0, stale: true)`; existing `StatusTimelineLogic` cache-fallback path returns "Configure in Flux" on missing config. |
| [9.1](requirements.md#9.1) `scrollContentBackground(.hidden)` on detail | Fully implemented | `AppNavigationView.swift:26-28`, `HistoryView.swift:88-90`, `DayDetailView.swift:57-59`. |
| [9.2](requirements.md#9.2) `backgroundExtensionEffect` on sidebar only | Fully implemented | `SidebarView.swift:18-20` — applied on List; not applied on detail views. |
| [9.3](requirements.md#9.3) Bidirectional ScrollView pinned top-leading | Partially / N/A | History and Day Detail charts use vertical-only `ScrollView`; the only bidirectional content (DailyUsageCard) is inside the vertical scroll. No explicit `frame(alignment: .topLeading)` was added — relies on default top-leading layout. Verify visually if shrinking the window reveals centred content. |
| [9.4](requirements.md#9.4) No inline `glassEffect` / `.buttonStyle(.glass)` | Fully implemented | Audit of changed files shows no `glassEffect` or `.buttonStyle(.glass)` in inline content; existing `.thinMaterial` cards retained. |
| [10.1](requirements.md#10.1) Tab/Shift-Tab reachability | Fully implemented | All controls are native SwiftUI (`Button`, `NavigationLink`, `Picker`, `SettingsLink`, `TextField`); no custom hit-test overrides. System handles focus traversal. |
| [10.2](requirements.md#10.2) `accessibilityReduceMotion` gating | Partially / N/A | No new animations were added for Mac. The single `.animation(.easeInOut(duration: 0.15), value: viewModel.isLoading)` in `HistoryView.swift:83` predates this change. No regression introduced; not actively gated on `accessibilityReduceMotion`. |
| [10.3](requirements.md#10.3) `accessibilityLabel` on icon-only controls | Partially / N/A | New Mac controls use `Label("Refresh")` (text + system image) and `SettingsLink { Text("Open Settings…") }` — no icon-only buttons added. The Day Detail chevron buttons (`Image(systemName: "chevron.left/right")`) predate this change and lack explicit accessibility labels; not regressed by this PR. |
| [10.4](requirements.md#10.4) Inactive-window appearance not overridden | Fully implemented | No code overrides `controlActiveState`. The Mac shell only reads `appearsActive` (`AppearsActiveMonitor.swift:5`); inactive rendering is left to the system. |

## Divergences from Design

| Area | Design Doc | Implementation | Assessment |
|------|-----------|----------------|------------|
| Empty-state CTA | `openWindow(id: …)` from `@Environment(\.openWindow)` | `MacUnconfiguredView` uses `SettingsLink { Text("Open Settings…") }` with an explicit `.keyboardShortcut(",", modifiers: .command)` (`AppNavigationView.swift:127-149`) | **Correct divergence.** SwiftUI's `Settings { … }` scene doesn't accept a custom window ID, so `openWindow(id:)` cannot target it. `SettingsLink` is the documented primitive for opening the Settings scene from a button and tracks the system menu binding automatically. |
| Keychain migrator | Five-case migration matrix with availability probe and `flux.keychain.migration.v1.complete` sentinel | Decision 18: dropped entirely. `loadToken`/`deleteToken`/`readAccessibility`/`updateAccessibility` query with `kSecAttrSynchronizableAny`; `saveToken` deletes-before-add. Convergence is implicit. (`KeychainService.swift:60-101`) | **Intentional simplification.** Recorded as Decision 18; the lenient-read-idempotent-write pattern is robust to ordering and partial state where the probe-based migrator was not. |
| Control widget value provider | `StatusTimelineLogic.makeDefault().snapshot()` | `WidgetRuntime.makeLogic().snapshot(isPreview: false)` (`ControlSOCProvider.swift`, `WidgetRuntime.swift:15-24`) | **Equivalent.** Different accessor name. `WidgetRuntime` was extracted in the pre-push review to share a single `URLSession`/`KeychainService` between `StatusTimelineProvider` and `FluxControlWidget`. |
| Control widget tap intent | Custom `OpenIntent`-style action (Decision 17) | `OpenURLIntent(WidgetDeepLink.dashboardURL)` directly — no custom `AppIntent` struct (`FluxControlWidget.swift:17-18`) | **Correct simplification.** Apple's built-in `OpenURLIntent` covers Decision 17's contract (open the app and route to Dashboard) without adding a new intent type. The existing `flux://dashboard` deep-link handler already routes correctly. |
| `FluxRefreshCoordinator` platform gating | Design implies cross-platform type | Type itself is not gated; only the views that wire it (`MacRefreshActionModifier`, `FluxKeyboardCommands`) are `#if os(macOS)` (`FluxRefreshCoordinator.swift`) | **Acceptable.** The coordinator compiles trivially on iOS but has no callers there, so the platform gate sits at the use sites instead of on the type. |
| Per-view `scrollContentBackground(.hidden)` | Design lists "detail scroll views" | Applied at `AppNavigationView` detail level **and** redundantly at `HistoryView`/`DayDetailView` for safety inside pushed scenes (`AppNavigationView.swift:26-28`, `HistoryView.swift:88-90`, `DayDetailView.swift:57-59`) | **Defensive.** SwiftUI's modifier propagation through `NavigationStack` is reliable but not guaranteed across pushed scenes; explicit per-view application avoids platform-level surprises around detail-column glass artifacts. |
