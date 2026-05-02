# Requirements: Mac App

## Introduction

Flux today is an iOS/iPadOS app. This feature ships a native macOS 26+ build of the same product so the user can monitor their AlphaESS battery from a desktop without resorting to "Designed for iPad" emulation. The Mac build follows native macOS conventions (dedicated Settings window, menu bar commands, Liquid Glass via system containers) and shares its data layer, networking stack, and credentials with the iOS app via FluxCore and iCloud Keychain.

Tracking ticket: T-1081.

## Non-Goals / Out of Scope

- Mac Catalyst or "Designed for iPad" execution modes
- Lock screen accessory widgets (N/A on macOS)
- Multiple simultaneous Day Detail windows
- Push notifications on macOS (not exercised by either platform today)
- Direct/Developer-ID distribution (Mac will ship through TestFlight Mac / Mac App Store only)
- Backend, poller, Lambda, or DynamoDB changes
- New product features (cards, charts, screens) — Mac v1 is parity, not expansion
- ⌘1/⌘2/⌘3 sidebar-jump shortcuts (deferred)
- Mac UI tests via `xcuitest` (deferred to v1.1; v1 ships unit tests only on Mac)
- Cross-device sync of ephemeral UI state (sidebar selection, Day Detail date, History range)
- iCloud-syncing of any user data other than the API token and base URL

## Requirements

### 1. Native macOS Build

**User Story:** As the user, I want a native Mac build of Flux, so that it behaves like a real Mac app rather than an iPad app in a window.

**Acceptance Criteria:**

1. <a name="1.1"></a>The Flux app SHALL launch on macOS 26+ as a native macOS application without Mac Catalyst, "Designed for iPad", or Designed-for-Mac fallback modes.
2. <a name="1.2"></a>The Mac app SHALL build, run, and pass its unit tests via `make build-macos` and `make test-macos` Makefile targets. UI tests on macOS are out of scope for v1.
3. <a name="1.3"></a>The Mac app target SHALL adopt the App Sandbox with entitlements for outbound HTTPS network client (`com.apple.security.network.client`), App Group sharing, and the matching Keychain access group, sufficient to be submitted to the Mac App Store and TestFlight Mac.
4. <a name="1.4"></a>The Mac widget extension target SHALL adopt the App Sandbox with the same App Group, the same Keychain access group, and `com.apple.security.network.client` (the widget extension fetches from the API independently of the host — see [8.6](#8.6)).
5. <a name="1.5"></a>The existing iOS app SHALL continue to build, run, and pass its tests with no functional regression. The Keychain attribute change ([7.1](#7.1)) SHALL NOT require an existing iOS user to re-enter their token, except in the rare case where the user has deliberately changed the token on another device after the new code shipped — at which point worst case is one re-entry.

### 2. Screen Parity

**User Story:** As the user, I want the same Dashboard, History, Day Detail, and Settings on Mac as on iOS, so that I can do everything I currently do on iOS from my desktop.

**Acceptance Criteria:**

1. <a name="2.1"></a>The Mac app SHALL render Dashboard, History, Day Detail, and Settings with the same data, controls, and interactions available on iOS.
2. <a name="2.2"></a>The Mac app SHALL render the History card stack (Solar, Grid Usage, Battery, Daily Usage, per-day summary) with shared chart selection working identically to iOS, including a cursor-driven equivalent of the iOS drag-to-select gesture.
3. <a name="2.3"></a>The Mac app SHALL render Day Detail charts (SOC, Power, Battery Power) with the same domains, color coding, and annotations as iOS.
4. <a name="2.4"></a>The Mac app SHALL render the read-only note row on Dashboard and History and SHALL allow editing the day's note from Day Detail with the same 200-grapheme limit and validation as iOS.

### 3. Native Mac Navigation Shell

**User Story:** As the user, I want the Mac app to feel like a Mac app, so that the chrome and layout match what I expect from other native apps on macOS 26.

**Acceptance Criteria:**

1. <a name="3.1"></a>The Mac app SHALL present a sidebar + detail layout that follows Liquid Glass conventions (sidebar receives `backgroundExtensionEffect`; detail scroll views hide their content background; no glass-on-glass artifacts in detail content).
2. <a name="3.2"></a>The Mac app SHALL surface only Dashboard and History in the sidebar; Settings SHALL NOT appear in the sidebar on macOS.
3. <a name="3.3"></a>The Mac app SHALL open as a single main window. The main window SHALL accept whatever minimum size SwiftUI applies by default (no explicit minimum size constraint).
4. <a name="3.4"></a>WHEN the main window is closed, the application SHALL terminate, even if a Settings window is open. The user SHALL NOT be left with an orphan Settings window after the main window closes.
5. <a name="3.5"></a>WHEN the application launches, it SHALL restore the sidebar selection (Dashboard / History) from the previous launch via `@SceneStorage` scoped to the main window. Sidebar selection state SHALL NOT be persisted across devices via iCloud.

### 4. Settings as a Dedicated Scene

**User Story:** As the user, I want Settings to behave like every other Mac app's settings, so that I reach it through the standard menu and shortcut.

**Acceptance Criteria:**

1. <a name="4.1"></a>The Mac app SHALL present Settings as a dedicated `Settings { … }` SwiftUI scene window opened via `⌘,` or "Flux > Settings…".
2. <a name="4.2"></a>The Settings window SHALL expose the same Backend (URL, token) and Display (load alert threshold) sections as the iOS Settings screen, with the same validation and save semantics.
3. <a name="4.3"></a>WHEN no API client is configured (missing URL or token), the main window SHALL show an empty-state CTA that opens the Settings window via `openWindow(id: …)` rather than the iOS sheet-based settings flow.
4. <a name="4.4"></a>WHEN the user successfully saves settings in the Settings window, the main window SHALL automatically replace any "not configured" empty state with the live Dashboard within one auto-refresh tick (no explicit user refresh required).

### 5. Menu Bar Commands & Keyboard Shortcuts

**User Story:** As the user, I want menu commands and keyboard shortcuts for the things I do most, so that I don't have to point-and-click for routine actions.

**Acceptance Criteria:**

1. <a name="5.1"></a>The Mac app SHALL expose a "Refresh" menu command bound to `⌘R` that re-fetches the data for the topmost view in the navigation stack: Day Detail when pushed (re-fetches `/day` for the current date), History when at the History root (re-fetches `/history`), Dashboard otherwise (re-fetches `/status`).
2. <a name="5.2"></a>WHEN the Day Detail screen is the active view, the Mac app SHALL navigate to the previous day on `←` and the next day on `→`. `←` SHALL have no effect when the displayed day is at the earliest cached day (matching the chevron `previous` button's disabled state). `→` SHALL have no effect when the displayed day is today (matching the chevron `next` button's disabled state — no wrap, no future days).
3. <a name="5.3"></a>The Mac app SHALL accept the system-provided `⌘W` (close window) and `⌘Q` (quit) shortcuts without overriding them. `⌘W` on the main window SHALL trigger the close-quits behaviour in [3.4](#3.4).

### 6. Auto-Refresh Behaviour

**User Story:** As the user, I want the Dashboard to refresh frequently when I'm watching it but back off when I'm not, so that the app stays useful without burning battery on a laptop.

**Acceptance Criteria:**

1. <a name="6.1"></a>WHEN the main window's `appearsActive` value is `true`, the Dashboard SHALL refresh every 10 seconds. (Definition: derived from SwiftUI's `@Environment(\.appearsActive)` Bool, observed at the Dashboard root.)
2. <a name="6.2"></a>WHEN the main window's `appearsActive` value is `false` (other app focused, window minimised, app behind another window, or a Settings window is the key window), the Dashboard SHALL refresh every 60 seconds.
3. <a name="6.3"></a>WHEN `appearsActive` transitions from `false` to `true`, the Dashboard SHALL trigger an immediate refresh and resume the 10-second cadence.
4. <a name="6.4"></a>WHEN a refresh request is in flight at the moment of any tier transition, the in-flight request SHALL be allowed to complete, and a new refresh SHALL NOT be enqueued until the in-flight one resolves. The next scheduled refresh SHALL use the new tier's cadence relative to the in-flight resolution time.
5. <a name="6.5"></a>The iOS app's existing scenePhase-based refresh behaviour (full pause on `.background`) SHALL be preserved unchanged. The new tier model SHALL apply on macOS only.

### 7. Cross-Platform Credential Sync

**User Story:** As the user, I want to enter my API URL and token once and have them work on both my iOS and Mac apps, so that I don't have to maintain two configurations.

**Acceptance Criteria:**

1. <a name="7.1"></a>Both apps SHALL store the API token in the Keychain with `kSecAttrSynchronizable = true` and `kSecAttrAccessible = kSecAttrAccessibleAfterFirstUnlock`, under the same Keychain access group as today. Existing tokens written under the legacy `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` (non-synchronisable) attributes SHALL remain readable; no one-shot migrator is required.
2. <a name="7.2"></a>The Keychain read path (`loadToken`) SHALL query with `kSecAttrSynchronizable = kSecAttrSynchronizableAny`, so both legacy non-synchronisable items and new synchronisable items are visible.
3. <a name="7.3"></a>The Keychain write path (`saveToken`) and delete path (`deleteToken`) SHALL operate via `kSecAttrSynchronizable = kSecAttrSynchronizableAny`, removing both variants before writing the new item, so a re-save from either platform converges on a single synchronisable item.
4. <a name="7.4"></a>The API base URL SHALL be persisted to `NSUbiquitousKeyValueStore` as the source of truth across devices. The host app SHALL mirror every URL change into the existing App Group `UserDefaults` so the widget extension reads the URL from `UserDefaults` as it does today. WHEN `NSUbiquitousKeyValueStore` notifies of an externally-changed URL, the host SHALL update the App Group `UserDefaults` mirror on `MainActor` before any further request is issued.
5. <a name="7.5"></a>The `URLSessionAPIClient`'s `tokenProvider` SHALL re-read the Keychain on every request, so a token rotated by iCloud Keychain on another device is picked up by the next request without an app restart. (This is the existing behaviour and SHALL be preserved.)
6. <a name="7.6"></a>WHEN no token is present in the Keychain (either because nothing has ever been entered or because iCloud Keychain has not yet propagated the value to this device), the app SHALL render the existing empty-state CTA routing the user to Settings. No "iCloud sync unavailable" diagnostic SHALL be shown.

### 8. Mac Widgets

**User Story:** As the user, I want the same Flux widgets on my Mac desktop and in macOS Control Center as I have on iOS, so that I can glance at battery state without opening the app.

**Acceptance Criteria:**

1. <a name="8.1"></a>The widget extension SHALL build for macOS and provide `systemSmall`, `systemMedium`, and `systemLarge` widget families that render on the macOS desktop and in Notification Center with the same content and styling as iOS.
2. <a name="8.2"></a>The widget extension SHALL provide a macOS Control Center widget conforming to the `ControlWidget` protocol (introduced for iOS 18 / macOS 26), surfacing the live SOC. Tapping the Control Center widget SHALL open the host app to the Dashboard via `widgetURL` or an equivalent `OpenURLIntent`.
3. <a name="8.3"></a>The lock screen accessory widget families (currently iOS-only) SHALL remain iOS-only via `#if os(iOS)` around their `Widget` registration in the `WidgetBundle` and SHALL NOT cause a build error on the macOS widget target.
4. <a name="8.4"></a>The Mac widgets SHALL read from the same App Group `UserDefaults` snapshot cache the iOS widgets use today, with no separate cache key. The App Group identifier SHALL be the existing `group.me.nore.ig.flux`.
5. <a name="8.5"></a>WHEN the host app pushes a fresh snapshot, `WidgetCenter.shared.reloadTimelines(ofKind:)` calls for the `systemSmall`, `systemMedium`, and `systemLarge` families SHALL be debounced to no more than once every 5 minutes, matching the existing iOS rule. This debounce SHALL NOT apply to the Control Center widget, which refreshes via `ControlCenter.shared.reloadControls(ofKind:)` and/or its own `ControlValueProvider` refresh path.
6. <a name="8.6"></a>The widget extension SHALL fetch live data directly from the API (`/status`) when its timeline provider runs and the App Group snapshot cache is older than the configured staleness threshold, regardless of whether the host app is currently running. WHEN the host app is quit (per [3.4](#3.4)), the widget SHALL continue to update via this independent fetch path.
7. <a name="8.7"></a>The widget extension SHALL read the API token from the Keychain via the same access group as the host app, including the synchronisable variant when iCloud Keychain is available. The extension SHALL handle the missing-token case by rendering the existing "Configure in Flux" placeholder rather than crashing.

### 9. Visual Polish (Liquid Glass on macOS)

**User Story:** As the user, I want the Mac app to look right under macOS 26's Liquid Glass styling, so that I don't see reflection artifacts, glass-on-glass muddying, or centred-content layout bugs.

**Acceptance Criteria:**

1. <a name="9.1"></a>Detail-column scroll views in the Mac app SHALL hide their scroll content background (`scrollContentBackground(.hidden)` on macOS) to avoid the Liquid Glass reflection artifact under the navigation bar.
2. <a name="9.2"></a>The sidebar `List` SHALL apply `backgroundExtensionEffect` on macOS; the detail views SHALL NOT.
3. <a name="9.3"></a>Bidirectional `ScrollView` content (charts, daily-usage stack) SHALL pin top-leading on macOS rather than centre.
4. <a name="9.4"></a>Inline content (cards, list rows, body text) SHALL NOT use `glassEffect` or `.buttonStyle(.glass)`; glass styling SHALL be confined to navigation chrome. Existing iOS materials (e.g. `.thinMaterial` in `BatteryHeroView`) SHALL be verified to render correctly on macOS 26 without modification; any divergence SHALL be addressed by the iOS-equivalent material on macOS rather than by adding glass.

### 10. Accessibility & Keyboard Reachability

**User Story:** As the user, I want the Mac app to be reachable from the keyboard and to honour system accessibility preferences, so that it works without a trackpad and respects Reduce Motion.

**Acceptance Criteria:**

1. <a name="10.1"></a>Every interactive element in the sidebar and detail views SHALL be reachable via Tab/Shift-Tab keyboard navigation.
2. <a name="10.2"></a>The Mac app SHALL gate any animation (chart selection feedback, sheet/window transitions added for Mac) on `accessibilityReduceMotion`, omitting or shortening animation when enabled.
3. <a name="10.3"></a>Icon-only controls added for Mac (toolbar buttons, menu items with symbol-only labels) SHALL provide an `accessibilityLabel`.
4. <a name="10.4"></a>Sidebar items and toolbar controls SHALL render their inactive-window appearance correctly when `controlActiveState` is `.inactive` (system handles this for native containers; the requirement is to NOT override it).
