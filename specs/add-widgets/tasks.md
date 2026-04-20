---
references:
    - specs/add-widgets/requirements.md
    - specs/add-widgets/design.md
    - specs/add-widgets/decision_log.md
    - specs/add-widgets/prerequisites.md
---
# Tasks: Add Widgets (T-843)

## Phase 1 — FluxCore package and file migration

- [x] 1. Create FluxCore Swift Package scaffolding <!-- id:behjcad -->
  - Create Flux/Packages/FluxCore directory.
  - Write Flux/Packages/FluxCore/Package.swift with swift-tools 6.0, platforms [.iOS(.v26)], FluxCore library product, FluxCoreTests test target (per design.md § 'Package.swift').
  - Create Sources/FluxCore/{Models,Networking,Security,Helpers,Widget} and Tests/FluxCoreTests/ directories.
  - Add a placeholder _placeholder.swift in both Sources/FluxCore and Tests/FluxCoreTests so the empty package compiles. These are deleted in task 2 when real sources migrate in.
  - Verify 'swift build' inside Flux/Packages/FluxCore compiles empty with no errors.
  - After this task completes, STOP and execute prerequisites.md (one Xcode sitting) before task 2 runs.
  - Stream: 1
  - Requirements: [9.1](requirements.md#9.1)

- [x] 2. Migrate models (APIModels, FluxAPIError) to FluxCore <!-- id:behjcae -->
  - Move Flux/Flux/Models/APIModels.swift to Sources/FluxCore/Models/.
  - Make public: StatusResponse, LiveData, BatteryInfo, Low24h, RollingAvg, OffpeakData, TodayEnergy, HistoryResponse, DayEnergy, DayDetailResponse, TimeSeriesPoint, DaySummary, PeakPeriod, APIErrorResponse (all types + stored properties + inits).
  - Move Flux/Flux/Models/FluxAPIError.swift to Sources/FluxCore/Models/ and make the enum + extensions (.from, .message, .suggestsSettings) public.
  - Do not delete originals yet — that happens in task 7.
  - Blocked-by: behjcad (Create FluxCore Swift Package scaffolding)
  - Stream: 1
  - Requirements: [9.2](requirements.md#9.2), [9.3](requirements.md#9.3)

- [x] 3. Migrate networking (FluxAPIClient + URLSessionAPIClient) to FluxCore <!-- id:behjcaf -->
  - Move FluxAPIClient protocol to Sources/FluxCore/Networking/ and make protocol + all methods public.
  - Move URLSessionAPIClient to Sources/FluxCore/Networking/ and make class + both initialisers public.
  - Preserve Sendable conformance and the existing noCacheSession static.
  - Do not change the existing session-injection init signature — the widget will use it with its own session in Phase 4.
  - Blocked-by: behjcae (Migrate models (APIModels, FluxAPIError) to FluxCore)
  - Stream: 1
  - Requirements: [9.2](requirements.md#9.2), [9.3](requirements.md#9.3)

- [x] 4. Migrate security (KeychainService) to FluxCore <!-- id:behjcag -->
  - Move KeychainService to Sources/FluxCore/Security/.
  - Make class + init + saveToken + loadToken + deleteToken public.
  - Leave the accessibility class parameter at its current value — the KeychainAccessibility enum and explicit accessibility parameter are added in task 19.
  - Blocked-by: behjcae (Migrate models (APIModels, FluxAPIError) to FluxCore)
  - Stream: 1
  - Requirements: [9.2](requirements.md#9.2), [9.3](requirements.md#9.3), [11.1](requirements.md#11.1)

- [x] 5. Migrate helpers (DateFormatting, PowerFormatting, colour helpers) to FluxCore <!-- id:behjcah -->
  - Move DateFormatting, PowerFormatting, BatteryColor, GridColor, CutoffTimeColor to Sources/FluxCore/Helpers/.
  - Extract ColorTier into its own Sources/FluxCore/Helpers/ColorTier.swift.
  - Keep ColorTier public + Sendable + Equatable but REMOVE the SwiftUI Color accessor — the package must compile without importing SwiftUI.
  - Make all formatter statics and colour-helper statics public.
  - Blocked-by: behjcae (Migrate models (APIModels, FluxAPIError) to FluxCore)
  - Stream: 1
  - Requirements: [9.2](requirements.md#9.2), [9.3](requirements.md#9.3)

- [x] 6. Migrate existing unit tests to FluxCoreTests <!-- id:behjcai -->
  - Move APIModelsTests, DateFormattingTests, ColoringTests, KeychainServiceTests, URLSessionAPIClientTests from FluxTests/ to Packages/FluxCore/Tests/FluxCoreTests/.
  - Replace @testable import Flux with import FluxCore (or @testable import FluxCore where internals are needed).
  - ColoringTests references ColorTier.color — add a private SwiftUI Color extension at the top of the test file so the existing assertions keep working.
  - Blocked-by: behjcaf (Migrate networking (FluxAPIClient + URLSessionAPIClient) to FluxCore), behjcag (Migrate security (KeychainService) to FluxCore), behjcah (Migrate helpers (DateFormatting, PowerFormatting, colour helpers) to FluxCore)
  - Stream: 1
  - Requirements: [9.5](requirements.md#9.5)

- [x] 7. Update main-app imports; add SwiftUI Color extension; delete migrated originals; verify build <!-- id:behjcaj -->
  - Delete the originals from Flux/Flux/Models/, Flux/Flux/Services/, Flux/Flux/Helpers/ (everything migrated in tasks 2–5).
  - Add import FluxCore to every remaining main-app file that references migrated types.
  - Create Flux/Flux/Helpers/ColorTier+Color.swift containing the SwiftUI Color extension on ColorTier.
  - Run xcodebuild build -scheme Flux -destination 'platform=iOS Simulator,name=iPhone 17 Pro' — must compile clean.
  - Run xcodebuild test — all pre-existing main-app tests plus migrated FluxCoreTests must pass.
  - Blocked-by: behjcai (Migrate existing unit tests to FluxCoreTests)
  - Stream: 1
  - Requirements: [9.3](requirements.md#9.3), [9.4](requirements.md#9.4), [16.3](requirements.md#16.3)

## Phase 2 — New FluxCore types (TDD)

- [ ] 8. Write tests for StatusSnapshotEnvelope <!-- id:behjcak -->
  - Create StatusSnapshotEnvelopeTests.swift in FluxCoreTests.
  - Round-trip: encode+decode returns an equal value (all fields preserved).
  - Payload JSON has top-level keys schemaVersion, fetchedAt, status.
  - fetchedAt serialises as ISO8601 string (UTC).
  - Decoder fails cleanly on payloads missing schemaVersion.
  - Blocked-by: behjcaj (Update main-app imports; add SwiftUI Color extension; delete migrated originals; verify build)
  - Stream: 1
  - Requirements: [4.7](requirements.md#4.7), [10.3](requirements.md#10.3)

- [ ] 9. Implement StatusSnapshotEnvelope <!-- id:behjcal -->
  - Create Sources/FluxCore/Widget/StatusSnapshotEnvelope.swift.
  - public struct with public static let currentSchemaVersion: Int = 1, public let schemaVersion: Int, public let fetchedAt: Date, public let status: StatusResponse.
  - Codable + Sendable.
  - Public init with schemaVersion defaulting to currentSchemaVersion.
  - Blocked-by: behjcak (Write tests for StatusSnapshotEnvelope)
  - Stream: 1
  - Requirements: [4.7](requirements.md#4.7), [10.3](requirements.md#10.3)

- [ ] 10. Write tests for WidgetSnapshotCache <!-- id:behjcam -->
  - Create WidgetSnapshotCacheTests.swift in FluxCoreTests.
  - Each test uses a unique suite name (test.widget.<UUID>) and clears it on teardown.
  - writeIfNewer returns true on empty cache; read returns the same envelope.
  - writeIfNewer returns false when stored envelope has strictly greater fetchedAt.
  - writeIfNewer returns true when stored envelope has equal fetchedAt (strict > semantics per Decision 16).
  - writeIfNewer returns true when stored envelope is older.
  - read returns nil for unknown schemaVersion.
  - read returns nil for garbage bytes.
  - clear removes the key; subsequent read returns nil.
  - Blocked-by: behjcal (Implement StatusSnapshotEnvelope)
  - Stream: 1
  - Requirements: [4.6](requirements.md#4.6), [4.7](requirements.md#4.7), [4.8](requirements.md#4.8), [10.2](requirements.md#10.2), [10.4](requirements.md#10.4)

- [ ] 11. Implement WidgetSnapshotCache <!-- id:behjcan -->
  - Create Sources/FluxCore/Widget/WidgetSnapshotCache.swift.
  - Storage key 'widgetSnapshotV1' in UserDefaults(suiteName:).
  - Use JSONEncoder/Decoder with .iso8601 date strategy.
  - writeIfNewer uses strict > comparison (equal timestamps write through).
  - read validates schemaVersion == currentSchemaVersion; otherwise returns nil.
  - public API: init(suiteName:nowProvider:), read, writeIfNewer (@discardableResult -> Bool), clear.
  - Final class Sendable.
  - Blocked-by: behjcam (Write tests for WidgetSnapshotCache)
  - Stream: 1
  - Requirements: [4.6](requirements.md#4.6), [4.7](requirements.md#4.7), [4.8](requirements.md#4.8), [10.2](requirements.md#10.2), [10.4](requirements.md#10.4)

- [ ] 12. Write tests for StalenessClassifier <!-- id:behjcao -->
  - Create StalenessClassifierTests.swift in FluxCoreTests.
  - Table-driven: age 0 → .fresh, 44 min → .fresh, 45 min → .stale, 179 min → .stale, 180 min → .offline, 1000 min → .offline.
  - Boundary tests exactly at freshThreshold and offlineThreshold (>= → next bucket).
  - nextTransition returns fresh boundary when now < fresh boundary.
  - nextTransition returns offline boundary when between fresh and offline boundaries.
  - nextTransition returns nil when now >= offline boundary.
  - Blocked-by: behjcaj (Update main-app imports; add SwiftUI Color extension; delete migrated originals; verify build)
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [15.2](requirements.md#15.2)

- [ ] 13. Implement StalenessClassifier <!-- id:behjcap -->
  - Create Sources/FluxCore/Widget/StalenessClassifier.swift.
  - public enum Staleness { case fresh, stale, offline } (Sendable, Equatable).
  - public enum StalenessClassifier with static freshThreshold = 45*60, offlineThreshold = 3*3600.
  - static func classify(fetchedAt: Date, now: Date) -> Staleness.
  - static func nextTransition(fetchedAt: Date, now: Date) -> Date?.
  - Pure functions — no side effects, no date-now access.
  - Blocked-by: behjcao (Write tests for StalenessClassifier)
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [15.2](requirements.md#15.2)

- [ ] 14. Write tests for WidgetDeepLink <!-- id:behjcaq -->
  - Create WidgetDeepLinkTests.swift in FluxCoreTests.
  - parse('flux://dashboard') returns .dashboard.
  - parse('flux://dashboard/extra') returns .dashboard (extra path ignored).
  - parse('flux://unknown') returns nil.
  - parse('other://dashboard') returns nil.
  - dashboardURL equals URL(string: 'flux://dashboard')!.
  - Blocked-by: behjcaj (Update main-app imports; add SwiftUI Color extension; delete migrated originals; verify build)
  - Stream: 1
  - Requirements: [8.1](requirements.md#8.1), [8.3](requirements.md#8.3)

- [ ] 15. Implement WidgetDeepLink <!-- id:behjcar -->
  - Create Sources/FluxCore/Widget/WidgetDeepLink.swift.
  - public enum WidgetDeepLink with static scheme = 'flux' and static dashboardURL.
  - public enum Destination { case dashboard } (Equatable).
  - static func parse(_ url: URL) -> Destination? — matches scheme + host, returns nil for unknown hosts.
  - Blocked-by: behjcaq (Write tests for WidgetDeepLink)
  - Stream: 1
  - Requirements: [8.1](requirements.md#8.1), [8.3](requirements.md#8.3)

- [ ] 16. Write tests for SettingsSuiteMigrator <!-- id:behjcas -->
  - Create SettingsSuiteMigratorTests.swift in FluxCoreTests.
  - Each test uses unique suite names for both 'standard' and 'fluxAppGroup' (inject both via init).
  - Standard has apiURL + threshold, suite empty → both copied; version written = 1; returns true.
  - Running twice: second call returns false, values unchanged.
  - Fresh install (standard empty, suite empty) → version written = 1; returns false; no copy.
  - Standard has apiURL but suite already has one → suite not overwritten.
  - Blocked-by: behjcaj (Update main-app imports; add SwiftUI Color extension; delete migrated originals; verify build)
  - Stream: 1
  - Requirements: [10.5](requirements.md#10.5), [10.6](requirements.md#10.6), [10.7](requirements.md#10.7)

- [ ] 17. Implement SettingsSuiteMigrator <!-- id:behjcat -->
  - Create Sources/FluxCore/Widget/SettingsSuiteMigrator.swift.
  - public enum SettingsSuiteMigrator with static currentVersion = 1.
  - static func run(standard: UserDefaults = .standard, suite: UserDefaults = UserDefaults(suiteName: 'group.me.nore.ig.flux')!) -> Bool.
  - Guard on suite.integer(forKey: 'settingsMigrationVersion') >= currentVersion.
  - Copy apiURL (String) if standard has it and suite does not.
  - Copy loadAlertThreshold (Double > 0) if standard has it and suite does not.
  - Always set settingsMigrationVersion = currentVersion before returning.
  - Idempotent on subsequent runs.
  - Blocked-by: behjcas (Write tests for SettingsSuiteMigrator)
  - Stream: 1
  - Requirements: [10.5](requirements.md#10.5), [10.6](requirements.md#10.6), [10.7](requirements.md#10.7)

- [ ] 18. Write tests for KeychainAccessibility + readAccessibility + updateAccessibility <!-- id:behjcau -->
  - Extend KeychainServiceTests in FluxCoreTests.
  - KeychainAccessibility Equatable: .afterFirstUnlockThisDeviceOnly == itself, != .other('foo').
  - readAccessibility returns nil when no item exists.
  - After saveToken with afterFirstUnlockThisDeviceOnly, readAccessibility == .afterFirstUnlockThisDeviceOnly.
  - updateAccessibility(.afterFirstUnlockThisDeviceOnly) on a saved item with a different class returns true; readAccessibility afterwards reflects the new class.
  - updateAccessibility when no item exists returns false.
  - Use per-test unique service+account UUIDs to isolate keychain state.
  - Blocked-by: behjcaj (Update main-app imports; add SwiftUI Color extension; delete migrated originals; verify build)
  - Stream: 1
  - Requirements: [11.5](requirements.md#11.5)

- [ ] 19. Implement KeychainAccessibility + readAccessibility + updateAccessibility <!-- id:behjcav -->
  - Add public enum KeychainAccessibility { case afterFirstUnlockThisDeviceOnly, other(String), missing } — Sendable, Equatable.
  - Add internal var cfString: CFString on the enum for mapping back.
  - Change KeychainService.init to accept accessibility: KeychainAccessibility = .afterFirstUnlockThisDeviceOnly (defaulted so existing callers compile).
  - saveToken writes kSecAttrAccessible = accessibility.cfString.
  - Add readAccessibility() -> KeychainAccessibility? — uses SecItemCopyMatching with kSecReturnAttributes; maps via 'as String' bridging; returns .missing when the attribute is absent from the returned dict; returns nil when no item exists (errSecItemNotFound).
  - Add updateAccessibility(_:) throws -> Bool — uses SecItemUpdate, does NOT delete-then-add.
  - Blocked-by: behjcau (Write tests for KeychainAccessibility + readAccessibility + updateAccessibility)
  - Stream: 1
  - Requirements: [11.5](requirements.md#11.5)

## Phase 3 — Main-app integration

- [ ] 20. Write tests for KeychainAccessibilityMigrator <!-- id:behjcaw -->
  - Create KeychainAccessibilityMigratorTests.swift in FluxTests (NOT in FluxCoreTests — the migrator lives in the app target).
  - No token stored → returns false, nothing mutated.
  - Token stored with correct class → returns false, nothing mutated.
  - Token stored with other class → returns true; after run, readAccessibility == .afterFirstUnlockThisDeviceOnly; token bytes preserved.
  - Simulate updateAccessibility failure via a wrapper → fallback path attempts saveToken; token preserved when saveToken succeeds; returns true.
  - Blocked-by: behjcav (Implement KeychainAccessibility + readAccessibility + updateAccessibility)
  - Stream: 2
  - Requirements: [11.5](requirements.md#11.5)

- [ ] 21. Implement KeychainAccessibilityMigrator <!-- id:behjcax -->
  - Create Flux/Flux/WidgetBridge/KeychainAccessibilityMigrator.swift.
  - enum KeychainAccessibilityMigrator with static required = KeychainAccessibility.afterFirstUnlockThisDeviceOnly.
  - @discardableResult static func run(keychain: KeychainService = KeychainService()) -> Bool.
  - Read current class; bail if nil (no token) or == required.
  - Try updateAccessibility(required); if that throws or returns false, fall back to loadToken + saveToken.
  - Never delete the token in the happy path.
  - Blocked-by: behjcaw (Write tests for KeychainAccessibilityMigrator)
  - Stream: 2
  - Requirements: [11.5](requirements.md#11.5)

- [ ] 22. Write tests for UserDefaults+Settings on App Group suite <!-- id:behjcay -->
  - Create UserDefaultsFluxAppGroupTests.swift in FluxTests.
  - apiURL getter/setter reads/writes the App Group suite (inject via a test helper that exposes a nonstandard suite).
  - loadAlertThreshold default is 3000 when the suite has no value.
  - loadAlertThreshold returns stored value when set.
  - The existing contract (apiURL optional String; threshold Double) is preserved — existing tests covering these behaviours on UserDefaults.standard must keep working after the refactor.
  - Blocked-by: behjcaj (Update main-app imports; add SwiftUI Color extension; delete migrated originals; verify build)
  - Stream: 2
  - Requirements: [10.5](requirements.md#10.5)

- [ ] 23. Migrate UserDefaults+Settings to App Group suite; update call sites <!-- id:behjcaz -->
  - Update Flux/Flux/Settings/UserDefaults+Settings.swift to expose a static fluxAppGroup: UserDefaults accessor (suite 'group.me.nore.ig.flux'); fatalError if the suite cannot be created.
  - Change existing extension getters/setters to read/write this suite by default. Keep the extension parameterisable (UserDefaults instance) so tests can inject.
  - Fallback behaviour: if a getter reads an empty value from the suite AND .standard still has a non-empty value, transparently return the .standard value. This covers the transient state between this task landing and FluxApp.init running SettingsSuiteMigrator (task 24).
  - Update every call site that previously used UserDefaults.standard for apiURL or loadAlertThreshold: Settings/SettingsViewModel.swift (read + write), Dashboard/PowerTrioView.swift (read), AppNavigationView.makeAPIClient (read), plus any remaining grep hits.
  - Tests from task 22 must pass.
  - Blocked-by: behjcay (Write tests for UserDefaults+Settings on App Group suite)
  - Stream: 2
  - Requirements: [10.5](requirements.md#10.5)

- [ ] 24. Wire FluxApp.init to run both migrators <!-- id:behjcb0 -->
  - Add init() to FluxApp struct calling SettingsSuiteMigrator.run() then KeychainAccessibilityMigrator.run().
  - Order matters (Decision 9 + 15 + rollout diagram in design.md) — settings first, keychain second.
  - Both migrators are idempotent; safe on every launch.
  - Blocked-by: behjcat (Implement SettingsSuiteMigrator), behjcax (Implement KeychainAccessibilityMigrator), behjcaz (Migrate UserDefaults+Settings to App Group suite; update call sites)
  - Stream: 2
  - Requirements: [10.6](requirements.md#10.6), [10.7](requirements.md#10.7), [11.5](requirements.md#11.5)

- [ ] 25. Register flux:// URL scheme in Info.plist <!-- id:behjcb1 -->
  - Edit Flux/Flux/Info.plist (or the Xcode-managed URL-types section).
  - Add CFBundleURLTypes array with one entry: CFBundleURLName = me.nore.ig.flux.deeplink, CFBundleURLSchemes = ['flux'].
  - Verify in Xcode target settings → Info → URL Types that the scheme shows up.
  - Stream: 2
  - Requirements: [8.3](requirements.md#8.3)

- [ ] 26. Write tests for AppNavigationView deep-link handling <!-- id:behjcb2 -->
  - Create AppNavigationViewDeepLinkTests.swift (or extend existing navigation tests).
  - Testable wrapper: expose the URL-handling logic as a pure function taking the URL and the current (selectedScreen, navigationPath) and returning updated values.
  - flux://dashboard → selectedScreen = .dashboard, navigationPath empty.
  - flux://unknown → unchanged.
  - other://dashboard → unchanged.
  - Blocked-by: behjcar (Implement WidgetDeepLink)
  - Stream: 2
  - Requirements: [8.3](requirements.md#8.3)

- [ ] 27. Implement AppNavigationView.onOpenURL handler <!-- id:behjcb3 -->
  - Add .onOpenURL modifier on AppNavigationView's root.
  - Use WidgetDeepLink.parse; switch on the returned destination.
  - For .dashboard: set selectedScreen = .dashboard, navigationPath = NavigationPath().
  - Unknown URLs: no-op (design §Deep link plumbing).
  - Blocked-by: behjcb2 (Write tests for AppNavigationView deep-link handling)
  - Stream: 2
  - Requirements: [8.3](requirements.md#8.3)

- [ ] 28. Write tests for DashboardViewModel cache write + debounced reload <!-- id:behjcb4 -->
  - Extend FluxTests/DashboardViewModelTests.swift.
  - Successful refresh writes a StatusSnapshotEnvelope (verify via a spy WidgetSnapshotCache pointing at a test suite).
  - Failed refresh does NOT write to the cache.
  - Reload closure is called exactly once after a successful refresh that advances fetchedAt.
  - Reload closure is NOT called when writeIfNewer returns false (same-timestamp write).
  - Second successful refresh within 5 minutes does NOT call reload (debounce).
  - Second successful refresh after debounce window DOES call reload.
  - Blocked-by: behjcan (Implement WidgetSnapshotCache)
  - Stream: 2
  - Requirements: [4.5](requirements.md#4.5), [5.3](requirements.md#5.3)

- [ ] 29. Implement DashboardViewModel cache write + debounced reload <!-- id:behjcb5 -->
  - Add initialiser-injected widgetCache: WidgetSnapshotCache = WidgetSnapshotCache(); widgetReloadTrigger closure; widgetReloadDebounce: TimeInterval = 5*60.
  - Default widgetReloadTrigger calls WidgetCenter.shared.reloadTimelines(ofKind: 'me.nore.ig.flux.widget.battery') then same for 'me.nore.ig.flux.widget.accessory' (Decision 13, updated Req 5.3).
  - Add private var lastWidgetReload: Date?.
  - On successful fetch: build envelope, call writeIfNewer. If returned true AND (lastWidgetReload is nil OR now - lastWidgetReload >= debounce) call trigger and record lastWidgetReload = now.
  - Do NOT write the cache on failure path.
  - Update Preview / existing callers to pass the defaults so main-app code compiles unchanged.
  - Blocked-by: behjcb4 (Write tests for DashboardViewModel cache write + debounced reload)
  - Stream: 2
  - Requirements: [4.5](requirements.md#4.5), [5.3](requirements.md#5.3)

## Phase 4 — Widget extension

- [ ] 30. Create FluxWidgets Info.plist <!-- id:behjcb6 -->
  - Already exists from prerequisites.md Xcode sitting: Flux/FluxWidgets/Info.plist with NSExtension = {NSExtensionPointIdentifier = com.apple.widgetkit-extension}.
  - Task verifies the file exists and contains the required keys; updates CFBundleDisplayName if needed.
  - (The Xcode-generated Info.plist from target creation is probably correct already; this task is a sanity check.)
  - Stream: 3
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.4](requirements.md#1.4)

- [ ] 31. Create FluxWidgets.entitlements <!-- id:behjcb7 -->
  - Already exists from prerequisites.md Xcode sitting: Flux/FluxWidgets/FluxWidgets.entitlements with com.apple.security.application-groups = [group.me.nore.ig.flux].
  - Note: keychain-access-groups is NOT added — KeychainService uses the App Group identifier directly as the access group, per Decision 19 / prerequisites Step 4.
  - Task verifies the file exists and contains the App Group; no-op if Xcode already wrote it.
  - Stream: 3
  - Requirements: [10.1](requirements.md#10.1), [11.1](requirements.md#11.1), [11.5](requirements.md#11.5)

- [ ] 32. Add SwiftUI Color extension on ColorTier in FluxWidgets target <!-- id:behjcb8 -->
  - Create Flux/FluxWidgets/ColorTier+Color.swift mirroring the app-side extension from task 7.
  - Keeps FluxCore free of SwiftUI; each consuming target owns its own Color mapping.
  - Blocked-by: behjcaj (Update main-app imports; add SwiftUI Color extension; delete migrated originals; verify build)
  - Stream: 3
  - Requirements: [12.1](requirements.md#12.1)

- [ ] 33. Write tests for RelevanceScoring (in FluxCoreTests) <!-- id:behjcb9 -->
  - Create Packages/FluxCore/Tests/FluxCoreTests/RelevanceScoringTests.swift (Decision 19 — widget-testable logic lives in FluxCore).
  - fresh + discharging with soc <= cutoffPercent + 5 → 0.9.
  - fresh + discharging with soc <= cutoffPercent + 20 → 0.7.
  - fresh otherwise → 0.5.
  - stale → 0.3.
  - offline → 0.1.
  - placeholder (no envelope) → 0.1.
  - Blocked-by: behjcap (Implement StalenessClassifier)
  - Stream: 3
  - Requirements: [5.2](requirements.md#5.2)

- [ ] 34. Implement RelevanceScoring in FluxCore <!-- id:behjcba -->
  - Create Packages/FluxCore/Sources/FluxCore/Widget/RelevanceScoring.swift.
  - public enum RelevanceScoring with static func score(staleness: Staleness, live: LiveData?, battery: BatteryInfo?) -> TimelineEntryRelevance.
  - FluxCore imports WidgetKit (iOS-only; FluxCore already targets iOS only).
  - Inputs are FluxCore types so the function is unit-testable via FluxCoreTests.
  - Blocked-by: behjcb9 (Write tests for RelevanceScoring (in FluxCoreTests))
  - Stream: 3
  - Requirements: [5.2](requirements.md#5.2)

- [ ] 35. Write tests for WidgetAccessibility (in FluxCoreTests) <!-- id:behjcbb -->
  - Create Packages/FluxCore/Tests/FluxCoreTests/WidgetAccessibilityTests.swift (Decision 19).
  - systemMedium normal: label contains SOC%, discharging verb, power trio values.
  - accessoryInline: label is one sentence with SOC + status word.
  - Offline state: label begins with 'Offline.' regardless of family (Req 13.4).
  - Placeholder entry (no envelope): label is a generic 'Flux battery data unavailable' string, NOT the SOC.
  - Every label begins with SOC percent when envelope present (Req 13.1).
  - No label relies on colour — verb/noun always present (Req 13.3).
  - Blocked-by: behjcap (Implement StalenessClassifier)
  - Stream: 3
  - Requirements: [13.1](requirements.md#13.1), [13.2](requirements.md#13.2), [13.4](requirements.md#13.4)

- [ ] 36. Implement WidgetAccessibility in FluxCore <!-- id:behjcbc -->
  - Create Packages/FluxCore/Sources/FluxCore/Widget/WidgetAccessibility.swift (Decision 19).
  - public enum WidgetAccessibility with static func label(for entry: StatusEntry, family: WidgetFamily) -> String.
  - FluxCore imports WidgetKit for WidgetFamily.
  - Branch per family for length-appropriate phrasing.
  - Always prepend 'Offline. ' when staleness == .offline.
  - Always include battery percentage, status verb, and where relevant power trio numbers in plain English.
  - Blocked-by: behjcbb (Write tests for WidgetAccessibility (in FluxCoreTests))
  - Stream: 3
  - Requirements: [13.1](requirements.md#13.1), [13.2](requirements.md#13.2), [13.3](requirements.md#13.3), [13.4](requirements.md#13.4)

- [ ] 37. Write tests for StatusTimelineLogic (in FluxCoreTests) <!-- id:behjcbd -->
  - Create Packages/FluxCore/Tests/FluxCoreTests/StatusTimelineLogicTests.swift.
  - Widget-logic is consolidated into FluxCore per Decision 19; the widget-extension StatusTimelineProvider is a thin WidgetKit shim over this testable logic type.
  - placeholder-for-context returns an entry with source == .placeholder and plausible fixture data.
  - timeline with empty cache + no token → single 'No data yet' entry; policy = .after(now + 30 min).
  - timeline with token + empty cache + successful fetch → cache is written; timeline contains entries at now, fresh→stale boundary, stale→offline boundary.
  - timeline with token + empty cache + fetch throws → single placeholder entry.
  - timeline with cache present + fetch throws → entry sourced from cache with correct staleness.
  - timeline with cache present + fetch succeeds + new fetchedAt > cache → cache overwritten.
  - timeline with cache present + fetch succeeds + new fetchedAt == cache → cache written (strict >).
  - timeline with Keychain errSecInteractionNotAllowed → cache-only path; no fetch attempted.
  - Cache-fallback on fetch timeout: inject a delayed FluxAPIClient mock that sleeps past the configured timeout; assert the entry comes from the cache.
  - Session-config guarantee lives in the thin StatusTimelineProvider in the extension target (no unit test — assert via a FluxCore-side helper that takes a URLSessionConfiguration and confirms request/resource timeouts are 5s).
  - Bucket-transition entries reuse the same envelope but advance displayAge/staleness classification.
  - Each timeline entry carries a non-default TimelineEntryRelevance (source: RelevanceScoring).
  - timeline calls SettingsSuiteMigrator.run() at the top (verify via a spy).
  - Blocked-by: behjcan (Implement WidgetSnapshotCache), behjcap (Implement StalenessClassifier), behjcat (Implement SettingsSuiteMigrator), behjcav (Implement KeychainAccessibility + readAccessibility + updateAccessibility)
  - Stream: 3
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [11.4](requirements.md#11.4), [11.6](requirements.md#11.6), [14.1](requirements.md#14.1), [14.3](requirements.md#14.3), [14.4](requirements.md#14.4), [15.1](requirements.md#15.1)

- [ ] 38. Implement StatusTimelineLogic + thin StatusTimelineProvider shim <!-- id:behjcbe -->
  - Create Packages/FluxCore/Sources/FluxCore/Widget/StatusTimelineLogic.swift — all orchestration (cache read, keychain, fetch-with-timeout, entry-stack, policy).
  - Create Packages/FluxCore/Sources/FluxCore/Widget/StatusEntry.swift — TimelineEntry-conforming value type with date, envelope (optional), staleness, source, relevance.
  - Create Flux/FluxWidgets/StatusTimelineProvider.swift in the widget extension — TimelineProvider conformance that constructs a StatusTimelineLogic with the widget-local URLSession and delegates getTimeline/getSnapshot/placeholder to it.
  - Widget-local URLSession: static let in the extension with timeoutIntervalForRequest = 5, timeoutIntervalForResource = 5, waitsForConnectivity = false.
  - Default apiClient constructor reads apiURL from App Group suite; if missing or invalid URL, apiClient is nil → skip fetch.
  - Policy = .after(now + 30*60).
  - Attach TimelineEntryRelevance via RelevanceScoring.
  - Blocked-by: behjcbd (Write tests for StatusTimelineLogic (in FluxCoreTests))
  - Stream: 3
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [11.4](requirements.md#11.4), [11.6](requirements.md#11.6), [14.1](requirements.md#14.1), [14.3](requirements.md#14.3), [14.4](requirements.md#14.4), [15.1](requirements.md#15.1)

- [ ] 39. Implement shared widget view components <!-- id:behjcbf -->
  - Create Flux/FluxWidgets/Views/Shared/SOCHeroLabel.swift — SOC percentage with BatteryColor.forSOC tint; size parameter for per-family scaling.
  - Create Flux/FluxWidgets/Views/Shared/StatusLineLabel.swift — renders BatteryHeroView.statusLine-equivalent text with .full / .short / .word styles.
  - Create Flux/FluxWidgets/Views/Shared/LoadRow.swift — Load column used by systemSmall; reads loadAlertThreshold from App Group suite (via UserDefaults+Settings accessor added in task 23); red when over threshold.
  - Create Flux/FluxWidgets/Views/Shared/StalenessFootnote.swift — secondary-coloured relative timestamp; renders only when staleness != .fresh.
  - All files are #Preview-wrapped (#if DEBUG) with fixture entries.
  - Blocked-by: behjcbe (Implement StatusTimelineLogic + thin StatusTimelineProvider shim), behjcb8 (Add SwiftUI Color extension on ColorTier in FluxWidgets target), behjcaz (Migrate UserDefaults+Settings to App Group suite; update call sites)
  - Stream: 3
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.6](requirements.md#2.6), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [12.1](requirements.md#12.1), [12.2](requirements.md#12.2)

- [ ] 40. Implement home-screen views (SystemSmall/Medium/Large) <!-- id:behjcbg -->
  - Create Flux/FluxWidgets/Views/SystemSmallView.swift — SOC hero + status line + Load row + staleness footnote when stale/offline.
  - Create Flux/FluxWidgets/Views/SystemMediumView.swift — two-column HStack with SOC hero + status line on left, power trio on right + staleness footnote.
  - Create Flux/FluxWidgets/Views/SystemLargeView.swift — medium layout plus a stats row (24h low + off-peak summary) modelled on SecondaryStatsView.
  - Each applies .containerBackground(for: .widget) { Color.clear } and .widgetURL(WidgetDeepLink.dashboardURL).
  - Each attaches WidgetAccessibility.label via .accessibilityLabel.
  - Placeholder-entry rendering (source == .placeholder): make values visually obvious as illustrative — apply .redacted(reason: .placeholder) to the numeric Text children while leaving layout scaffolding/typography intact (Req 14.2).
  - Each ships #if DEBUG #Preview blocks covering fresh/stale/offline AND placeholder states.
  - Blocked-by: behjcbf (Implement shared widget view components)
  - Stream: 3
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [8.1](requirements.md#8.1), [12.1](requirements.md#12.1), [12.2](requirements.md#12.2), [13.1](requirements.md#13.1), [14.2](requirements.md#14.2)

- [ ] 41. Implement lock-screen views (AccessoryCircular/Rectangular/Inline) <!-- id:behjcbh -->
  - Create AccessoryCircularView using Gauge + .accessoryCircularCapacity; tint = BatteryColor.forSOC in .fullColor rendering mode; tint = .primary in .accented/.vibrant; .opacity(0.5) when offline.
  - Create AccessoryRectangularView showing SOC + short status line + staleness only when != fresh; use .widgetAccentable() on leading icon glyph.
  - Create AccessoryInlineView producing one-line Text: 'Flux: NN% · <word>' or 'Flux: offline' when offline.
  - Placeholder-entry rendering: apply .redacted(reason: .placeholder) to numeric children so the widget gallery preview clearly shows illustrative values (Req 14.2).
  - All three apply .containerBackground(for: .widget) { Color.clear } and .widgetURL(WidgetDeepLink.dashboardURL).
  - Respect Dynamic Type up to .accessibility3 (Req 12.4).
  - Blocked-by: behjcbf (Implement shared widget view components)
  - Stream: 3
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [8.2](requirements.md#8.2), [12.1](requirements.md#12.1), [12.2](requirements.md#12.2), [12.4](requirements.md#12.4), [13.2](requirements.md#13.2), [13.4](requirements.md#13.4)

- [ ] 42. Wire FluxBatteryWidget + FluxAccessoryWidget + FluxWidgetsBundle <!-- id:behjcbi -->
  - Create Flux/FluxWidgets/FluxBatteryWidget.swift — StaticConfiguration with kind 'me.nore.ig.flux.widget.battery', StatusTimelineProvider, supportedFamilies = [.systemSmall, .systemMedium, .systemLarge]; configurationDisplayName 'Flux Battery'.
  - Create Flux/FluxWidgets/FluxAccessoryWidget.swift — StaticConfiguration with kind 'me.nore.ig.flux.widget.accessory', supportedFamilies = [.accessoryCircular, .accessoryRectangular, .accessoryInline]; configurationDisplayName 'Flux Accessory'.
  - Create Flux/FluxWidgets/FluxWidgetsBundle.swift — @main struct FluxWidgetsBundle: WidgetBundle with both widgets.
  - Verify xcodebuild builds both targets.
  - Blocked-by: behjcbe (Implement StatusTimelineLogic + thin StatusTimelineProvider shim), behjcbg (Implement home-screen views (SystemSmall/Medium/Large)), behjcbh (Implement lock-screen views (AccessoryCircular/Rectangular/Inline))
  - Stream: 3
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2)

## Phase 5 — Documentation

- [ ] 43. Add CHANGELOG entry for widget introduction <!-- id:behjcbj -->
  - Edit CHANGELOG.md.
  - Under the '## [Unreleased]' section add an 'Added' subsection (or extend if present) listing: widget extension with home-screen (small/medium/large) and lock-screen (circular/rectangular/inline) families showing battery SOC and power data; App Group cache + shared Keychain migration; flux:// deep link to Dashboard.
  - Also note the shared FluxCore Swift Package migration under 'Changed' (no observable app behaviour change — pure refactor).
  - Blocked-by: behjcbi (Wire FluxBatteryWidget + FluxAccessoryWidget + FluxWidgetsBundle)
  - Stream: 3
  - Requirements: [16.4](requirements.md#16.4)
