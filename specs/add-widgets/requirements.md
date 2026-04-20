# Requirements: Add Widgets (T-843)

## Introduction

Add WidgetKit-based home-screen and lock-screen widgets to the Flux iOS app that surface the same live data currently shown at the top of the Dashboard: battery state of charge, battery discharge/charge status with estimated cutoff time, and the solar/load/grid power trio. Widgets must work within WidgetKit's constraints (sandboxed extension, ~30 MB memory, limited timeline refresh budget, no SwiftData access) while matching the Dashboard's data accuracy and Liquid Glass visual language.

The feature introduces a new Xcode widget extension target and a local Swift Package that the main app and widget both depend on. The main app writes a snapshot of `/status` to an App Group cache on every successful refresh; the widget reads that cache first, then refreshes by calling the Lambda directly using the shared Keychain bearer token.

---

## Requirements

### 1. Widget Families Supported

**User Story:** As a Flux user, I want widgets across both the home screen and lock screen, so that I can glance at my battery state without unlocking my phone or opening the app.

**Acceptance Criteria:**

1. <a name="1.1"></a>The widget extension SHALL support the home-screen families `systemSmall`, `systemMedium`, and `systemLarge`.
2. <a name="1.2"></a>The widget extension SHALL support the lock-screen families `accessoryCircular`, `accessoryRectangular`, and `accessoryInline`.
3. <a name="1.3"></a>Each supported family SHALL have a distinct layout tuned to its size — the `systemSmall` and `accessoryRectangular` layouts SHALL NOT simply scale the `systemMedium` layout.
4. <a name="1.4"></a>The widget gallery SHALL display one Flux widget entry per family with a description derived from the app's existing naming.
5. <a name="1.5"></a>iPad SHALL be supported as a widget host for all home-screen families (no explicit iPad-specific layout beyond what WidgetKit provides). Apple Watch SHALL NOT be a target of this feature.

---

### 2. Battery State Content

**User Story:** As a Flux user, I want to see my battery state of charge and whether it is charging, discharging, or idle, so that I know at a glance whether I am on battery power or the grid.

**Acceptance Criteria:**

1. <a name="2.1"></a>Every widget family SHALL display the current state of charge as a percentage with one decimal place (matching `BatteryHeroView` formatting).
2. <a name="2.2"></a>The `systemSmall`, `systemMedium`, `systemLarge`, and `accessoryRectangular` families SHALL display a status line indicating one of: `Charging at <rate>`, `Discharging at <rate>`, `Idle`, `Full`, or `No live data`.
3. <a name="2.3"></a>WHEN the battery is discharging AND a `rolling15min.estimatedCutoffTime` is present, the `systemMedium` and `systemLarge` and `accessoryRectangular` families SHALL include the cutoff clock time in the status line (matching `BatteryHeroView.statusLine`).
4. <a name="2.4"></a>The SOC text and progress indicator colour SHALL use the `BatteryColor.forSOC()` tier mapping already defined in the app, so widget and Dashboard colouring stay in lock-step.
5. <a name="2.5"></a>The `accessoryCircular` family SHALL render SOC as a progress ring with the numeric percentage centred and SHALL NOT display any additional text.
6. <a name="2.6"></a>The `accessoryInline` family SHALL render one line of text in the form `Flux: <SOC>% · <short status>` where `<short status>` is one of `charging`, `discharging`, `idle`, `full`, or `offline`.

---

### 3. Power Trio Content

**User Story:** As a Flux user, I want to see solar, load, and grid power readings in the widget, so that I can understand where my home's energy is coming from right now.

**Acceptance Criteria:**

1. <a name="3.1"></a>The `systemMedium` and `systemLarge` families SHALL display solar (`ppv`), load (`pload`), and grid (`pgrid`) values formatted via the existing `PowerFormatting.format` helper.
2. <a name="3.2"></a>The grid column title SHALL switch between `Grid`, `Grid (import)`, and `Grid (export)` based on `pgrid` sign (matching `PowerTrioView.gridTitle`).
3. <a name="3.3"></a>Solar SHALL be coloured green when `ppv > 0` and secondary otherwise.
4. <a name="3.4"></a>Load SHALL be coloured red when `pload > loadAlertThreshold` and primary otherwise. The widget SHALL read `loadAlertThreshold` from the shared App Group `UserDefaults` suite.
5. <a name="3.5"></a>Grid SHALL use the `GridColor.forGrid()` tier mapping, passing through `pgridSustained`, the off-peak window, and the current time, so widget colouring stays in lock-step with the Dashboard.
6. <a name="3.6"></a>The `systemSmall` family SHALL display exactly one secondary metric below the SOC hero: the current household load (`pload`), formatted via `PowerFormatting.format` and coloured red when `pload > loadAlertThreshold` (same rule as [3.4](#3.4)). No other metric SHALL be shown in `systemSmall`.
7. <a name="3.7"></a>Lock-screen families SHALL NOT display the power trio (space and contrast constraints make it unreliable at that size).

---

### 4. Data Source and Freshness

**User Story:** As a Flux user, I want widget data to be recent, so that the readings I see are trustworthy.

**Acceptance Criteria:**

1. <a name="4.1"></a>The widget timeline provider SHALL attempt to fetch `/status` from the Lambda directly on every timeline refresh using the bearer token read from the shared Keychain.
2. <a name="4.2"></a>IF the timeline fetch succeeds THEN the widget SHALL render the new data AND write it to the shared App Group cache with a timestamp.
3. <a name="4.3"></a>IF the timeline fetch fails OR times out after 5 seconds THEN the widget SHALL render the most recent cached snapshot written by either the app or the widget itself.
4. <a name="4.4"></a>IF no cached snapshot exists AND the live fetch has failed THEN the widget SHALL render a placeholder entry with the text `No data yet — open Flux`.
5. <a name="4.5"></a>The main app's Dashboard view model SHALL write the latest `StatusResponse` plus its fetch timestamp to the shared App Group cache on every successful `/status` refresh.
6. <a name="4.6"></a>The shared App Group cache SHALL store at most one live snapshot (the most recent one), plus the fetch timestamp.
7. <a name="4.7"></a>The cache payload SHALL include an integer `schemaVersion` field. IF a cache reader finds a `schemaVersion` it does not recognise THEN it SHALL treat the cache as empty and fall back to the live fetch / placeholder path.
8. <a name="4.8"></a>A cache writer (app or widget) SHALL NOT overwrite an existing cached snapshot whose fetch timestamp is newer than the one about to be written. This prevents a late widget refresh from clobbering a fresher app write.

---

### 5. Timeline Refresh Policy

**User Story:** As a Flux user, I want widgets to refresh often enough that they do not feel abandoned, but without draining my battery or exhausting the system refresh budget.

**Acceptance Criteria:**

1. <a name="5.1"></a>The timeline provider SHALL return a timeline with a `.after` refresh time of `now + 30 minutes`. This is the nominal refresh cadence; iOS may reload earlier when the Home Screen has focus or when `TimelineEntryRelevance` signals a high-relevance moment, and the main app triggers an immediate reload via [5.3](#5.3) whenever the user opens the Dashboard.
2. <a name="5.2"></a>The timeline MAY contain multiple entries whose timestamps mark the expected `fresh → stale` and `stale → offline` transition boundaries, reusing the most recent snapshot's data with an advanced `displayAge`. This SHALL be the only form of pre-computation; the provider SHALL NEVER extrapolate or interpolate power or SOC values into the future.
3. <a name="5.3"></a>The main app SHALL request a widget reload after a successful Dashboard `/status` refresh (via `WidgetCenter.shared.reloadTimelines(ofKind:)` targeted at every Flux widget kind — see Decision 13). The reload request SHALL be gated on the underlying cache having actually changed AND rate-limited to at most once per 5 minutes so the daily widget-reload budget is preserved even during extended foreground sessions.
4. <a name="5.4"></a>The widget SHALL NOT make more than one network request per timeline refresh.
5. <a name="5.5"></a>The widget SHALL complete each timeline refresh within 5 seconds of wall-clock time or abandon the network fetch and return the cached entry (WidgetKit's hard kill window is ~15 seconds; the 5 s budget leaves headroom for cache read, rendering, and timeline assembly).

---

### 6. Staleness Presentation

**User Story:** As a Flux user, I want to know when widget data is stale, so that I do not act on outdated readings.

**Acceptance Criteria:**

1. <a name="6.1"></a>The widget SHALL classify every rendered snapshot into one of three states: `fresh` (fetch age < 45 minutes), `stale` (45 minutes – 3 hours), or `offline` (> 3 hours). The `fresh` threshold is set to 1.5× the nominal refresh interval from [5.1](#5.1) so a normal refresh cycle keeps most renders classified as `fresh`; the `offline` threshold corresponds to "this data cannot be trusted without opening the app".
2. <a name="6.2"></a>WHEN the snapshot is `stale` THEN the `systemMedium` and `systemLarge` families SHALL show a subtle relative timestamp (e.g. `12 min ago`) in a secondary colour.
3. <a name="6.3"></a>WHEN the snapshot is `offline` THEN all families SHALL dim SOC and power values to secondary colour and append an `Offline` marker (an icon for lock-screen families, text for home-screen families).
4. <a name="6.4"></a>The `accessoryInline` family SHALL replace the status word with `offline` when the snapshot is `offline` AND SHALL NOT render SOC in that case (SOC may be wildly out of date).
5. <a name="6.5"></a>The widget SHALL NEVER surface raw network error messages — staleness and the `Offline` marker are the only failure-mode UI.

---

### 7. Configuration (Not Required in v1)

**User Story:** As the sole maintainer of Flux, I want to ship the widget quickly without building a configuration UI I do not need, so that I can iterate on layout before committing to parameters.

**Acceptance Criteria:**

1. <a name="7.1"></a>The widget SHALL use `StaticConfiguration` — there SHALL be no `WidgetConfigurationIntent` in v1.
2. <a name="7.2"></a>The widget SHALL NOT offer any user-configurable parameters in v1. Future configuration (e.g. which metric to show in `systemSmall`) MAY be added in a later spec without changing the data path.
3. <a name="7.3"></a>IF a future revision adds configuration it SHALL do so behind an `AppIntentConfiguration`, leaving the shared-package contract unchanged.

---

### 8. Tap Behaviour and Deep Linking

**User Story:** As a Flux user, I want tapping the widget to open the app, so that I can see full detail with one gesture.

**Acceptance Criteria:**

1. <a name="8.1"></a>Home-screen widgets SHALL open the app to the Dashboard when tapped, via `widgetURL` pointing at a custom scheme (e.g. `flux://dashboard`).
2. <a name="8.2"></a>Lock-screen widgets SHALL rely on WidgetKit's default tap behaviour (open the app).
3. <a name="8.3"></a>The main app SHALL register the custom URL scheme AND SHALL navigate to the Dashboard when it receives a Flux widget deep link (ignoring any unknown path so future links do not crash older app builds).
4. <a name="8.4"></a>The widget SHALL NOT use `Button(intent:)` for in-widget interactivity in v1 (out of scope).

---

### 9. Shared Code Architecture

**User Story:** As the sole maintainer of Flux, I want widget and app to share the same models and networking code, so that there is exactly one place to fix a bug in the status parsing.

**Acceptance Criteria:**

1. <a name="9.1"></a>A local Swift Package named `FluxCore` SHALL be added at `Flux/Packages/FluxCore` with products targeting `iOS(.v26)`.
2. <a name="9.2"></a>The following existing source files SHALL be moved into the package with `public` access on their types: `APIModels.swift`, `FluxAPIError.swift`, `FluxAPIClient.swift` protocol, `URLSessionAPIClient.swift`, `KeychainService.swift`, `DateFormatting.swift`, `PowerFormatting.swift`, `BatteryColor.swift`, `GridColor.swift`, `CutoffTimeColor.swift`.
3. <a name="9.3"></a>The existing main-app code SHALL continue to compile after the migration — there SHALL be no behavioural change to the app beyond the import path.
4. <a name="9.4"></a>Both the main app target AND the widget extension target SHALL depend on `FluxCore` via local SPM, with NO files copied between the two targets.
5. <a name="9.5"></a>The existing main-app tests SHALL continue to pass after the migration AND package-level tests SHALL be added for any types whose access level is changed to `public`.

---

### 10. Shared App Group Cache

**User Story:** As a Flux user, I want the widget to show something useful immediately after I install it, so that the first render is not an empty placeholder.

**Acceptance Criteria:**

1. <a name="10.1"></a>The app group `group.me.nore.ig.flux` SHALL be enabled on the widget extension target (it is already enabled on the app).
2. <a name="10.2"></a>A `WidgetSnapshotCache` type SHALL be added to `FluxCore` that persists a `StatusResponse` plus fetch timestamp under the App Group `UserDefaults` suite.
3. <a name="10.3"></a>The cache payload SHALL be encoded as JSON (Codable) to isolate it from future Swift ABI changes.
4. <a name="10.4"></a>The cache SHALL be atomic — a partial write SHALL NOT leave the widget reading a torn payload.
5. <a name="10.5"></a>The `UserDefaults+Settings` extension SHALL migrate from `UserDefaults.standard` to `UserDefaults(suiteName: "group.me.nore.ig.flux")` so that the widget reads the same `loadAlertThreshold` the user configured in the app.
6. <a name="10.6"></a>On first app launch after upgrade, IF a `loadAlertThreshold` exists in `UserDefaults.standard` AND the App Group suite has not yet been migrated THEN the app SHALL copy the value to the App Group suite.
7. <a name="10.7"></a>The migration from [10.6](#10.6) SHALL record a `settingsMigrationVersion` integer in the App Group suite AND SHALL NOT re-run on subsequent launches. Fresh installs with no `.standard` value SHALL record the current migration version immediately (no-op migration) so that the flag's presence is canonical.

---

### 11. Security and Credentials

**User Story:** As a Flux user, I want my API bearer token to stay secure when the widget accesses it, so that installing widgets does not introduce new credential-exposure risk.

**Acceptance Criteria:**

1. <a name="11.1"></a>The widget SHALL read the bearer token from the existing Keychain item at `kSecClassGenericPassword` / service `me.nore.ig.flux` / account `api-token`, using the existing App Group access group.
2. <a name="11.2"></a>The widget SHALL NEVER write or delete the Keychain token.
3. <a name="11.3"></a>The widget SHALL NEVER log the bearer token nor any full request header.
4. <a name="11.4"></a>IF the Keychain read returns no token THEN the widget SHALL render the `No data yet — open Flux` placeholder (same as [4.4](#4.4)) AND SHALL NOT fall through to an unauthenticated request.
5. <a name="11.5"></a>The Keychain item SHALL be stored with `kSecAttrAccessible = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`. This is required for lock-screen widgets (which run while the device is locked) to read the token. The app SHALL migrate any existing Keychain item that uses a stricter accessibility class on first launch after upgrade.
6. <a name="11.6"></a>IF the Keychain read fails with `errSecInteractionNotAllowed` (device has not been unlocked since boot) THEN the widget SHALL render from the cached snapshot only AND SHALL NOT attempt a live fetch. Classification SHALL proceed as normal; staleness will naturally surface if the device stays locked long enough.

---

### 12. Liquid Glass Styling

**User Story:** As a Flux user, I want the widget to feel native on iOS 26, so that it visually belongs beside my other widgets.

**Acceptance Criteria:**

1. <a name="12.1"></a>Every widget view SHALL apply `.containerBackground(for: .widget) { Color.clear }` at its root so the system Liquid Glass material shows through.
2. <a name="12.2"></a>The widget SHALL NOT set solid background fills, custom materials, or `widgetBackground(...)` (deprecated).
3. <a name="12.3"></a>Typography SHALL use SF system fonts at the weights/sizes chosen by the family (no custom fonts).
4. <a name="12.4"></a>The widget views SHALL respect Dynamic Type for the lock-screen families up to `.accessibility3`. Home-screen families MAY cap at `.xxxLarge` because Springboard imposes tight size limits.

---

### 13. Accessibility

**User Story:** As a Flux user who relies on VoiceOver, I want the widget to announce meaningful content, so that I can use it without sighted interaction.

**Acceptance Criteria:**

1. <a name="13.1"></a>Each home-screen widget SHALL expose a single `accessibilityElement(children: .combine)` with a label that concatenates SOC, status, and (where present) the power trio in plain English.
2. <a name="13.2"></a>Each lock-screen widget SHALL expose an accessibility label suitable for the family size (e.g. "Flux battery 73.4%, discharging, cutoff 5:12 pm").
3. <a name="13.3"></a>Colour SHALL NEVER be the only channel communicating state — the status line always includes a text verb.
4. <a name="13.4"></a>WHEN the widget is `offline` THEN the accessibility label SHALL begin with the word "Offline" so screen-reader users learn of staleness first.

---

### 14. Preview and Placeholder

**User Story:** As a Flux user adding the widget for the first time, I want the widget gallery to show a realistic preview, so that I understand what it will look like once data arrives.

**Acceptance Criteria:**

1. <a name="14.1"></a>The timeline provider's `placeholder(in:)` SHALL return a representative entry with a plausible SOC (e.g. 68%), discharge status, and mid-range power trio.
2. <a name="14.2"></a>The placeholder view SHALL render the widget's layout scaffolding clearly while making it visually obvious that the numeric values are illustrative, not live (the design phase will choose the exact technique, e.g. redaction, reduced opacity, or fixture labels).
3. <a name="14.3"></a>`snapshot(in:)` SHALL return the same representative entry when `context.isPreview` is true AND SHALL attempt a live fetch (with a cached fallback) when `context.isPreview` is false.
4. <a name="14.4"></a>On first install IF the Keychain token is present AND no cached snapshot exists THEN the timeline provider SHALL attempt a live fetch for the first entry rather than immediately showing the "No data yet" placeholder.

---

### 15. Testability

**User Story:** As the sole maintainer of Flux, I want the widget's data-path logic to be unit-testable, so that I can refactor layouts without re-running manual widget sanity checks.

**Acceptance Criteria:**

1. <a name="15.1"></a>The timeline provider SHALL inject its `FluxAPIClient`, `WidgetSnapshotCache`, and `nowProvider` closure through the initialiser so tests can substitute mocks.
2. <a name="15.2"></a>A staleness classifier (fresh/stale/offline based on age) SHALL live in `FluxCore` as a pure function with Swift Testing coverage.
3. <a name="15.3"></a>The `WidgetSnapshotCache` SHALL have round-trip (encode/decode) and missing-entry tests in the `FluxCore` package test target.
4. <a name="15.4"></a>The widget view types SHALL be previewable via `#Preview` blocks guarded by `#if DEBUG`, using static fixture snapshots.

---

### 16. Shipping and Backwards Compatibility

**User Story:** As the sole maintainer of Flux, I want the widget introduction to ship without breaking existing app installs, so that users do not lose their current Dashboard.

**Acceptance Criteria:**

1. <a name="16.1"></a>The widget target's deployment target SHALL match the app (`iOS 26.4` or later). No legacy iOS build support is required.
2. <a name="16.2"></a>The App Group container identifier SHALL remain `group.me.nore.ig.flux` to preserve the existing Keychain sharing.
3. <a name="16.3"></a>The shared package migration ([9.2](#9.2), [9.3](#9.3)) SHALL NOT change observable app behaviour — view models, views, tests, and the Dashboard refresh cadence SHALL be indistinguishable before and after.
4. <a name="16.4"></a>The `CHANGELOG.md` SHALL receive an entry describing the widget introduction.
