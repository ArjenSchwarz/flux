---
references:
    - specs/macos-app/requirements.md
    - specs/macos-app/design.md
    - specs/macos-app/decision_log.md
    - specs/macos-app/prerequisites.md
---
# Tasks: Mac App

## FluxCore foundation

- [x] 1. Add macOS platform to FluxCore Package.swift <!-- id:ntwcpwe -->
  - Modify Flux/Packages/FluxCore/Package.swift to add `.macOS(.v26)` to the platforms array. No test pair (config-only change).
  - Requirements: [1.2](requirements.md#1.2)

- [x] 2. Write KeychainService tests for SynchronizableAny + new defaults (red) <!-- id:ntwcpvx -->
  - Add tests in Flux/Packages/FluxCore/Tests/FluxCoreTests/KeychainServiceTests.swift covering: loadToken finds a legacy non-synchronisable item; saveToken removes both legacy and synchronisable variants then writes a single synchronisable item; deleteToken removes both variants. Tests should fail against current implementation.
  - Requirements: [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3)

- [x] 3. Implement KeychainService SynchronizableAny semantics + new defaults <!-- id:ntwcpvy -->
  - Modify Flux/Packages/FluxCore/Sources/FluxCore/Security/KeychainService.swift: change accessibility default to `.afterFirstUnlock`, add `synchronizable: Bool = true` parameter, set `kSecAttrSynchronizable` on writes via `kCFBooleanTrue/False`, query with `kSecAttrSynchronizableAny` on loadToken and deleteToken. Add `.afterFirstUnlock` case to KeychainAccessibility enum.
  - Blocked-by: ntwcpvx (Write KeychainService tests for SynchronizableAny + new defaults (red))
  - Requirements: [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3)

- [x] 4. Write iCloudURLMirror tests (red) <!-- id:ntwcpvz -->
  - Add tests covering: write(url) updates both NSUbiquitousKeyValueStore and App Group UserDefaults; external KVS change notification updates UserDefaults mirror within MainActor; start() seeds UserDefaults from KVS on first call. Inject test doubles for both stores.
  - Requirements: [7.4](requirements.md#7.4)

- [x] 5. Implement iCloudURLMirror <!-- id:ntwcpw0 -->
  - New file Flux/Packages/FluxCore/Sources/FluxCore/Settings/iCloudURLMirror.swift. Singleton @MainActor @Observable; `start()` registers async-sequence observer on `NSUbiquitousKeyValueStore.didChangeExternallyNotification`; `write(url)` updates both stores.
  - Blocked-by: ntwcpvz (Write iCloudURLMirror tests (red))
  - Requirements: [7.4](requirements.md#7.4)

## macOS host app shell

- [x] 6. Write DashboardViewModel activity-tier tests (red) <!-- id:ntwcpw1 -->
  - Add tests in Flux/FluxTests/DashboardViewModelTests.swift covering: updateActivityTier(.inactive) switches the next sleep interval to 60s; updateActivityTier(.active) from .inactive triggers an immediate refresh; in-flight request blocks parallel refresh on tier transition. Inject `sleep` closure to drive deterministic tests.
  - Blocked-by: ntwcpvy (Implement KeychainService SynchronizableAny semantics + new defaults)
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4)

- [x] 7. Implement DashboardViewModel activity-tier and Control Center reload debounce <!-- id:ntwcpw2 -->
  - Modify Flux/Flux/Dashboard/DashboardViewModel.swift: add ActivityTier enum, currentInterval property, updateActivityTier(_:), refreshIfIdle(); change refresh loop to read currentInterval per tick. Add ControlCenter reload debounce (60s) on macOS after successful Dashboard fetch via `#if os(macOS)`.
  - Blocked-by: ntwcpw1 (Write DashboardViewModel activity-tier tests (red))
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [8.5](requirements.md#8.5)

- [x] 8. Add FluxAppDelegate (Mac-only) with unit test <!-- id:ntwcpw3 -->
  - New file Flux/Flux/Mac/FluxAppDelegate.swift gated by `#if os(macOS)`. Unit test in Flux/FluxTests verifying `applicationShouldTerminateAfterLastWindowClosed(_:)` returns true. Drop explicit @MainActor (Swift 6.2 default-isolation handles it).
  - Requirements: [3.4](requirements.md#3.4)

- [x] 9. Add FluxRefreshCoordinator and FluxKeyboardCommands <!-- id:ntwcpw4 -->
  - Two new files in Flux/Flux/Mac/: FluxRefreshCoordinator.swift (@MainActor @Observable, single optional closure property `refresh`) and FluxKeyboardCommands.swift (Commands struct with ⌘R bound to coordinator.refresh?()). FluxKeyboardCommands gated by `#if os(macOS)`. No test pair: pure types/wiring.
  - Requirements: [5.1](requirements.md#5.1), [5.3](requirements.md#5.3)

- [x] 10. Wire FluxApp for macOS scenes and lifecycle <!-- id:ntwcpw5 -->
  - Modify Flux/Flux/FluxApp.swift: add `@NSApplicationDelegateAdaptor(FluxAppDelegate.self)` inside `#if os(macOS)`; add @State refreshCoordinator; add Settings { SettingsView() } scene with min frame size; add .commands { FluxKeyboardCommands(coordinator:) }; pass coordinator into AppNavigationView via .environment; call `iCloudURLMirror.shared.start()` in init().
  - Blocked-by: ntwcpw0 (Implement iCloudURLMirror), ntwcpw3 (Add FluxAppDelegate (Mac-only) with unit test), ntwcpw4 (Add FluxRefreshCoordinator and FluxKeyboardCommands)
  - Requirements: [4.1](requirements.md#4.1), [3.4](requirements.md#3.4), [5.1](requirements.md#5.1)

- [x] 11. Update AppNavigationView and SidebarView for macOS
  - Modify Flux/Flux/Navigation/AppNavigationView.swift: add `@SceneStorage` selection binding on macOS; filter `.settings` from sidebar via a new `Screen.visible(for:)` helper in Flux/Flux/Navigation/Screen.swift; apply `.scrollContentBackground(.hidden)` on macOS detail; apply `.backgroundExtensionEffect()` on macOS sidebar List; replace iOS-style empty-state inline SettingsView with a macOS CTA that calls `openWindow(id:)` to open the Settings scene; add `.onReceive` listener for `.fluxCredentialsChanged` notification calling `reloadDependencies()`. Modify Flux/Flux/Navigation/SidebarView.swift to accept the filtered list.
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.5](requirements.md#3.5), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [9.1](requirements.md#9.1), [9.2](requirements.md#9.2)

- [x] 12. Wire refresh coordinator install/uninstall in DashboardView, HistoryView, DayDetailView <!-- id:ntwcpw6 -->
  - Each top-level view reads `@Environment(FluxRefreshCoordinator.self)` and on `.onAppear` (macOS-only) sets `coordinator.refresh = { Task { await viewModel.<refresh-method>() } }`; on `.onDisappear` clears it. Use the appropriate refresh method per view (DashboardViewModel.refresh, HistoryViewModel.reload, DayDetailViewModel.loadDay).
  - Blocked-by: ntwcpw4 (Add FluxRefreshCoordinator and FluxKeyboardCommands)
  - Requirements: [5.1](requirements.md#5.1)

- [x] 13. Add AppearsActiveMonitor and apply to DashboardView <!-- id:ntwcpw7 -->
  - New file Flux/Flux/Dashboard/AppearsActiveMonitor.swift gated by `#if os(macOS)`. ViewModifier reads `@Environment(\.appearsActive)` and on change calls `viewModel.updateActivityTier(.active or .inactive)`. Apply via `.modifier(AppearsActiveMonitor(viewModel: viewModel))` in DashboardView body inside `#if os(macOS)`.
  - Blocked-by: ntwcpw2 (Implement DashboardViewModel activity-tier and Control Center reload debounce)
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.5](requirements.md#6.5)

- [x] 14. Add Day Detail keyboard navigation (Mac-only)
  - Modify Flux/Flux/DayDetail/DayDetailView.swift to add `.onKeyPress(.leftArrow)` and `.onKeyPress(.rightArrow)` modifiers on the root, gated by `#if os(macOS)`, calling existing `viewModel.navigatePrevious()` / `viewModel.navigateNext()`. Boundary behaviour follows the chevron buttons.
  - Requirements: [5.2](requirements.md#5.2)

- [x] 15. Write SettingsViewModel save tests for credential mirror + notification (red) <!-- id:ntwcpw8 -->
  - Add tests in Flux/FluxTests/SettingsViewModelTests.swift verifying that on successful save: `iCloudURLMirror.write(url)` is called (inject mirror as a test double), `.fluxCredentialsChanged` notification is posted, and existing token-save behaviour still occurs. Add `Notification.Name.fluxCredentialsChanged` constant in the mirror or settings namespace.
  - Blocked-by: ntwcpw0 (Implement iCloudURLMirror)
  - Requirements: [4.4](requirements.md#4.4), [7.4](requirements.md#7.4)

- [x] 16. Update SettingsViewModel to write through iCloudURLMirror and post credentials notification <!-- id:ntwcpw9 -->
  - Modify Flux/Flux/Settings/SettingsViewModel.swift: replace direct write to `UserDefaults.fluxAppGroup.apiURL` with `iCloudURLMirror.shared.write(url)`; post `Notification.Name.fluxCredentialsChanged` after both URL and token are saved successfully.
  - Blocked-by: ntwcpw8 (Write SettingsViewModel save tests for credential mirror + notification (red))
  - Requirements: [4.4](requirements.md#4.4), [7.4](requirements.md#7.4)

- [x] 17. Apply Liquid Glass scroll polish on macOS detail views
  - Touch HistoryView and DayDetailView root scroll views to add `.scrollContentBackground(.hidden)` on macOS (mirror the same pattern used in AppNavigationView). For any bidirectional ScrollView (DailyUsage stack), pin content top-leading via `.frame(alignment: .topLeading)` per style guide §5. Audit pass: confirm no inline content uses `.glassEffect` or `.buttonStyle(.glass)` (req 9.4).
  - Requirements: [9.1](requirements.md#9.1), [9.3](requirements.md#9.3), [9.4](requirements.md#9.4)

## Widget extension Mac support

- [ ] 18. Add WidgetKinds.controlBattery constant <!-- id:ntwcpwa -->
  - Modify Flux/Packages/FluxCore/Sources/FluxCore/Widget/WidgetKinds.swift to add `public static let controlBattery = "FluxControlBattery"`.
  - Requirements: [8.2](requirements.md#8.2)

- [ ] 19. Write ControlSOCProvider tests (red) <!-- id:ntwcpwb -->
  - Add tests covering: cache hit (cache.read() returns recent envelope) returns SOCValue with stale=false; cache stale or missing path falls back to live snapshot. Inject WidgetSnapshotCache + StatusTimelineLogic test doubles.
  - Blocked-by: ntwcpwa (Add WidgetKinds.controlBattery constant)
  - Requirements: [8.2](requirements.md#8.2), [8.6](requirements.md#8.6)

- [ ] 20. Implement FluxControlWidget and ControlSOCProvider <!-- id:ntwcpwc -->
  - New file Flux/FluxWidgets/FluxControlWidget.swift gated by `#if os(macOS)`. Defines FluxControlWidget: ControlWidget with StaticControlConfiguration + ControlValueProvider (ControlSOCProvider). Action uses Apple OpenURLIntent(URL(string: "flux://dashboard")!) directly — no custom intent struct. Label shows `Int(value.percent)%` with SOCFormatting.symbol icon.
  - Blocked-by: ntwcpwb (Write ControlSOCProvider tests (red))
  - Requirements: [8.2](requirements.md#8.2), [8.6](requirements.md#8.6)

- [ ] 21. Update FluxWidgetsBundle for platform-conditional widget registration <!-- id:ntwcpwd -->
  - Modify Flux/FluxWidgets/FluxWidgetsBundle.swift to gate `FluxAccessoryWidget()` with `#if os(iOS)` and add `FluxControlWidget()` under `#if os(macOS)`. FluxBatteryWidget remains on both.
  - Blocked-by: ntwcpwc (Implement FluxControlWidget and ControlSOCProvider)
  - Requirements: [8.1](requirements.md#8.1), [8.2](requirements.md#8.2), [8.3](requirements.md#8.3)

## Build verification

- [ ] 22. Add macos-build, macos-test, macos-lint targets to the Makefile <!-- id:ntwcpwf -->
  - Modify the Makefile to add `macos-build`, `macos-test`, and `macos-lint` targets mirroring the iOS equivalents but with `-destination platform=macOS,arch=arm64`. UI test target deferred (Decision 15).
  - Blocked-by: ntwcpwe (Add macOS platform to FluxCore Package.swift)
  - Requirements: [1.2](requirements.md#1.2)
