# Decision Log: Mac App

## Decision 1: Native macOS, not Mac Catalyst or Designed-for-iPad

**Date**: 2026-05-01
**Status**: accepted

### Context

Flux is currently an iOS/iPadOS app. The Xcode project already has `SUPPORTED_PLATFORMS` containing `macosx` and a `MACOSX_DEPLOYMENT_TARGET` set, but `TARGETED_DEVICE_FAMILY = "1,2,7"` excludes Mac and the app has never actually shipped or been tested as a Mac build. T-1081 calls for a Mac version that "conforms to the proper UI", and the user explicitly confirmed they want a native build.

### Decision

The Mac version SHALL ship as a native macOS SwiftUI app. Mac Catalyst, "Designed for iPad", and "Designed for Mac" execution modes are out of scope.

### Rationale

The app is SwiftUI from end to end. Per Arjen's iOS style guide §8 ("Don't add Mac Catalyst to a SwiftUI app"), native macOS gives better window management, real menu bars, real keyboard shortcuts, and access to macOS-only SwiftUI APIs (`Settings` scene, `CommandGroup`, `openWindow`). Catalyst is the migration path for UIKit apps, not a target for greenfield SwiftUI. "Designed for iPad" was rejected by the ticket title itself — it does not conform to "proper UI".

### Alternatives Considered

- **Mac Catalyst**: Reuse the iOS UI binary on Mac with platform translation. Rejected — gives a translated UIKit feel rather than native macOS, and the SwiftUI-first nature of this codebase removes Catalyst's main benefit.
- **Designed for iPad on Mac**: Zero code changes; iPad app runs in a Mac window. Rejected — the ticket explicitly asks for "proper UI", which this is not.

### Consequences

**Positive:**
- Real macOS menu bar with `CommandGroup`s and shortcuts
- Native `Settings` scene, `openWindow`, and other macOS-only SwiftUI APIs available
- Cleaner Liquid Glass story (no Catalyst-style translation layer)

**Negative:**
- Requires platform-specific code (`#if os(macOS)`) in views and entry points
- Widget extension and entitlements need separate Mac configuration

---

## Decision 2: Settings via Dedicated `Settings` Scene + ⌘,

**Date**: 2026-05-01
**Status**: accepted

### Context

iOS exposes Settings as a sidebar entry that pushes a `SettingsView` into the detail column. Mac apps typically expose settings as a separate window opened via `⌘,` or "App > Settings…". Style guide §13 ("Sheets, Popovers, and Windows") prescribes this pattern for macOS.

### Decision

On macOS, the app SHALL present Settings as a dedicated `Settings { … }` SwiftUI scene opened via `⌘,` or the "Flux > Settings…" menu item. The sidebar SHALL NOT include a Settings entry on macOS.

### Rationale

Native convention. Every Mac user expects `⌘,` to open settings in its own window and to find it under the application menu. Mixing it into the sidebar would feel iPad-translated.

### Alternatives Considered

- **Keep sidebar Settings entry on macOS**: Mirror iOS exactly. Rejected — non-idiomatic on Mac and contradicts the "proper UI" goal.
- **Sheet instead of window**: Match iOS's empty-state sheet flow. Rejected — sheets feel out of place for persistent settings on macOS.

### Consequences

**Positive:**
- Idiomatic macOS UX
- Settings stay open while the user navigates the main window

**Negative:**
- Sidebar `Screen` enum needs platform-conditional cases or the `.settings` case needs to be hidden in the macOS sidebar build
- Empty-state CTA (when API client is unconfigured) needs to open the Settings window rather than reuse the iOS sheet
- Settings-window lifecycle is coupled to main-window close (see Decision 4 update / requirement [3.5](requirements.md#3.5))

---

## Decision 3: Credentials Sync via iCloud Keychain

**Date**: 2026-05-01 (updated 2026-05-02: migration ceremony removed in favour of lenient read + idempotent write)
**Status**: accepted

### Context

The user runs Flux on iOS and will run it on Mac. Today the iOS app stores the API token in the App Group Keychain (`kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`) and the API URL in App Group `UserDefaults`. Without sync, the user would have to enter the URL and token twice.

### Decision

The Mac and iOS apps SHALL share the API token via iCloud Keychain (`kSecAttrSynchronizable = true`) and the API base URL via `NSUbiquitousKeyValueStore` (see Decision 12). The accessibility class SHALL change from `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` to `kSecAttrAccessibleAfterFirstUnlock` (Decision 11). No one-shot migration is performed; instead the Keychain wrapper queries with `SynchronizableAny` on read and on delete-before-write, so legacy non-synchronisable items remain readable until the next save converges to a single synchronisable item.

### Rationale

Removes a real annoyance. Style guide §10 already recommends a thin Keychain wrapper; switching the accessibility attribute and adding `kSecAttrSynchronizable` is a small change for a notable UX win. The API URL needs the same sync because the token is meaningless without the matching backend URL.

The user explicitly chose to skip a migration ceremony: the app is used by two people, the user has access to all devices, and "worst case re-enter token" is an accepted outcome. Lenient read + idempotent write covers the common case without code complexity.

### Alternatives Considered

- **Separate per platform**: User enters credentials on each device. Rejected — pure friction; the user has both devices set up to the same backend.
- **One-shot Keychain migrator with sentinel and re-enable detection**: Five-case migration matrix (none, legacy-only, sync-only, both, sentinel-set), with re-enable detection via an `iCloudKeychainAvailability` probe. Rejected — overengineered for a two-person app where re-entering a token is acceptable; the probe was also non-deterministic.
- **Defer to a follow-up**: Ship Mac v1 without sync, add later. Rejected — adding sync attributes after ship is harder than doing it once now.

### Consequences

**Positive:**
- Single setup; token rotation propagates automatically
- Backend URL stays in lockstep across devices
- No migrator code to maintain; KeychainService changes are ~5 lines
- Existing iOS users keep working with no re-entry required (lenient read finds legacy item)

**Negative:**
- Brief indeterminate state if both legacy and synchronisable items exist (e.g. user installs Mac, enters token on Mac, iCloud syncs back to iOS) — `loadToken` may return either, but both should hold the same token until the user changes it; any subsequent `saveToken` from either platform converges to a single sync item
- If the user has rotated the token between platform installs, they may need to re-save once on the platform whose stale legacy item wins the indeterminate read — accepted as a worst case

---

## Decision 4: Single Main Window; Close Quits

**Date**: 2026-05-01
**Status**: accepted

### Context

Mac apps vary in window behaviour. Some quit on close (Calculator), some stay running in the Dock (Mail), and some allow multiple windows of the same content (Notes). For a personal monitoring tool, the simpler choices are preferable. macOS does NOT terminate a `WindowGroup`-backed app on last window close by default — this requires an explicit `applicationShouldTerminateAfterLastWindowClosed` returning `true` from an `NSApplicationDelegateAdaptor`.

### Decision

The Mac app SHALL open as a single main window. Closing the main window SHALL quit the app via an `NSApplicationDelegateAdaptor` returning `true` from `applicationShouldTerminateAfterLastWindowClosed(_:)`. Day Detail SHALL be a navigation push within the detail column, the same as iOS — no separate Day Detail windows. Closing the main window SHALL also dismiss any open Settings window.

### Rationale

Flux is a utility-style monitoring app, not a document-based editor. Multi-window adds state-management complexity (multiple view models, widget cache contention, multiple refresh timers) for a use case the user did not ask for. Quitting on close avoids the "where did the app go?" pattern of always-running utilities for a tool that mostly answers a single question per glance. Independent of host lifecycle, the Control Center widget continues to update via the widget extension's own fetch path (Decision 13).

### Alternatives Considered

- **Stay running after close (Dock-only)**: Keep menu bar commands available with no window open. Rejected — adds the "is the app running?" ambiguity for a tool that has no background work.
- **Day Detail in its own window via openWindow(id:)**: Allow side-by-side day comparisons. Rejected — not requested; can be added later with an "Open in New Window" command if desired.

### Consequences

**Positive:**
- One refresh timer, one view model tree, one widget cache writer
- Predictable lifecycle
- Settings window cannot be orphaned

**Negative:**
- "Open Day Detail in new window" comparison flows would need a follow-up spec
- ⌘W behaves as quit-equivalent rather than hide-window
- Control Center widget cannot rely on the host app to refresh its cache — it must fetch independently (Decision 13)

---

## Decision 5: Refresh Tiers — 10s Focused, 60s Unfocused (macOS only)

**Date**: 2026-05-01 (updated 2026-05-02: bound to `appearsActive`, scoped to macOS only)
**Status**: accepted

### Context

iOS refreshes Dashboard every 10 seconds while the app is foreground and pauses on `.background`. macOS does not have an equivalent "background" scene phase for a windowed app — the app is "running" the moment its window exists. Continuing to poll every 10 seconds while the user is in another app would burn battery and network on laptops. (`@Environment(\.controlActiveState)` was the obvious signal but is deprecated since macOS 15 in favour of `@Environment(\.appearsActive)`.)

### Decision

On macOS the Dashboard SHALL refresh every 10 seconds while the main window's `appearsActive` is `true` and every 60 seconds while it is `false`. Returning to `appearsActive == true` SHALL trigger an immediate refresh. In-flight requests during a tier transition SHALL be allowed to complete; no parallel duplicate request is enqueued. iOS SHALL retain its existing scenePhase-based pause behaviour unchanged.

### Rationale

Matches iOS perceptually when the user is looking, backs off when they are not. 60 seconds is an explicit, modest tier — the user could come back to a window mid-cycle and at most see ~60-second-old data, which the immediate-on-key refresh erases within one network round-trip. iOS suspends the process on `.background`, so a unified tier model would gain nothing on iOS (the `Task.sleep(60s)` would not fire while suspended) and would only add cancellation-noise on resume — keeping iOS unchanged is strictly better.

### Alternatives Considered

- **Match iOS exactly (10s focused, pause when not key on Mac)**: Pause entirely when window loses focus. Rejected — gives noticeably stale data on return to focus, even with an immediate refresh on key.
- **Always 10s**: Don't back off. Rejected — burns battery for a tool that's often left open in a background window.
- **Unified tier model on both platforms**: Apply the new tier on iOS too. Rejected — iOS suspends the process; the 60s sleep does not actually fire and the change buys nothing.
- **Bind to deprecated `controlActiveState`**: Initial design used this; rejected after peer review confirmed deprecation since macOS 15.

### Consequences

**Positive:**
- Matches the user's actual attention pattern on macOS
- iOS behaviour unchanged (no regression risk for the existing app)
- Bound to a current SwiftUI-observable signal (`appearsActive`)

**Negative:**
- Two refresh tiers add a small amount of state (active state + in-flight guard) to the Dashboard view model on macOS
- Opening the Settings window flips the main window to `appearsActive == false` and drops to 60s — acceptable (user is in Settings, not watching the dashboard), but worth noting

---

## Decision 6: Keyboard Shortcuts — ⌘R, ←/→ in Day Detail; Defer ⌘1/⌘2/⌘3

**Date**: 2026-05-01
**Status**: accepted

### Context

Mac apps are expected to be keyboard-reachable. The user was offered a menu of common shortcuts and selected ⌘R (refresh), ←/→ (Day Detail navigation), and the system-provided ⌘W/⌘Q. They explicitly skipped ⌘1/⌘2/⌘3 sidebar-jump shortcuts as unlikely to be used.

### Decision

The Mac app SHALL expose `⌘R` for refresh, `←` / `→` for previous/next day in Day Detail, and SHALL accept the system-provided `⌘W` / `⌘Q` defaults. Sidebar-jump shortcuts (`⌘1` / `⌘2` / `⌘3`) SHALL NOT be added in v1. `⌘R` SHALL act on the topmost view in the navigation stack (Day Detail when pushed; History at History root; Dashboard otherwise) — see requirement [5.1](requirements.md#5.1).

### Rationale

Stick to what the user has said they will use. Adding shortcuts that nobody uses adds maintenance and a longer "Keyboard Shortcuts" surface in System Settings without benefit.

### Alternatives Considered

- **Include ⌘1/⌘2/⌘3 sidebar-jump shortcuts**: Quick screen switching from anywhere. Rejected by the user — unlikely to be used.
- **Custom shortcut for "Open today's Day Detail"**: Deep-link shortcut. Rejected — not requested; can be added later.

### Consequences

**Positive:**
- Smaller, more deliberate shortcut set
- Fewer collisions with custom user shortcuts

**Negative:**
- Sidebar navigation requires a click; no keyboard equivalent in v1
- ⌘R semantics depend on the current navigation depth — implementation needs to plumb the "active refresh action" through the focused view

---

## Decision 7: Widget Scope — Desktop / Notification Center + Control Center, Skip Lock Screen

**Date**: 2026-05-01
**Status**: accepted, expanded by Decision 14

### Context

The current widget extension is iOS-only and ships small/medium/large home-screen widgets plus accessory (lock screen) widgets. macOS supports desktop widgets (in Notification Center and on the desktop) at the small/medium/large families and a Control Center widget surface (introduced in macOS 26). Lock screen accessory families do not apply to macOS.

### Decision

The widget extension SHALL build for macOS and ship `systemSmall`, `systemMedium`, `systemLarge` widget families plus a macOS Control Center widget conforming to the `ControlWidget` protocol (Decision 14). Lock screen accessory families SHALL remain iOS-only (gated by `#if os(iOS)` in the `WidgetBundle`).

### Rationale

Desktop widgets are the natural Mac equivalent of iOS home-screen widgets and reuse the existing timeline provider, snapshot cache, and rendering code. The Control Center widget is a small additional surface with high glanceability for "what's the SOC right now". Lock screen accessory widgets do not exist on Mac.

### Alternatives Considered

- **Defer widgets to v1.1**: Ship the app first, widgets later. Rejected by the user — widgets are part of v1.
- **Menu bar `NSStatusItem` instead of widgets**: A different surface entirely. Rejected — diverges from the iOS widget code path; reconsider as a separate spec if a menu-bar surface becomes desirable.

### Consequences

**Positive:**
- Reuses ~all of the existing widget rendering and timeline code
- Same App Group snapshot cache for both platforms

**Negative:**
- Widget extension entitlements need a Mac variant (App Sandbox + App Group + Keychain Access Group + network client per Decision 13)
- `#if os(iOS)` needed around accessory widget registration
- Control Center widget is a new code path using a different protocol family (Decision 14)

---

## Decision 8: iOS Impact — Refactors OK if They Improve Both, Bounded by Mac PR Scope

**Date**: 2026-05-01
**Status**: accepted

### Context

Adding a Mac target inevitably involves shared-code refactors (extracting helpers from the iOS app target into FluxCore, splitting platform-specific view code, switching Keychain attributes that iOS also uses). The user was asked how aggressive these refactors can be on the iOS side and approved larger refactors when they improve both platforms. Without a guardrail, the Mac PR can balloon to encompass arbitrary iOS cleanup.

### Decision

Refactors that touch the iOS app SHALL be acceptable provided they preserve iOS user-visible behaviour and pass the existing iOS test suite. The Mac PR SHALL include only refactors that are **required** by the Mac build (e.g. extracting code into FluxCore because both targets need it, switching Keychain attributes because the credential sync requires it). Refactors that merely benefit both platforms SHALL ship as separate PRs ahead of the Mac PR so the Mac PR stays reviewable.

### Rationale

Splitting required-vs-nice-to-have refactors keeps PRs reviewable while still allowing the work to happen. Required refactors are scoped by the Mac requirements; nice-to-have refactors are scoped by judgment and benefit from independent review.

### Alternatives Considered

- **No iOS regressions, no internal refactors**: Mac work must be purely additive. Rejected — leads to duplicated view code and a worse codebase for both platforms.
- **Acceptable to refactor iOS internals if Mac requires it**: Internals can change but no Mac-driven improvements unless necessary. Rejected by the user as too restrictive.
- **Anything goes in the Mac PR**: Bundle every cleanup that occurs to us. Rejected — review burden and risk.

### Consequences

**Positive:**
- Single shared implementation per concern
- No "Mac TODO" left behind in iOS code
- Mac PR remains reviewable

**Negative:**
- Some judgment calls about "required vs nice-to-have"; expect to re-litigate on a few PRs
- Pre-Mac PRs need their own review and merge cadence

---

## Decision 9: No Explicit Window Minimum Size

**Date**: 2026-05-01
**Status**: accepted

### Context

Mac apps typically declare a minimum window size to prevent the user from shrinking the window into an unusable state. The user was asked and explicitly chose to defer to the system default.

### Decision

The main window SHALL accept the SwiftUI default minimum size with no explicit constraint.

### Rationale

The user prefers to defer the call until they have used the app at small sizes. If the layout breaks under shrinking, a follow-up will add a constraint based on observed pain.

### Alternatives Considered

- **900×600 minimum**: Sidebar visible, charts comfortable. Rejected — speculative without a measured layout problem.
- **720×480 minimum**: Compact, sidebar collapsible. Rejected — same reason.

### Consequences

**Positive:**
- No premature constraint
- Defers a UI-polish call to after the app is usable

**Negative:**
- Layout may break at very small sizes; follow-up may be needed once usage data exists

---

## Decision 10: Distribution via TestFlight Mac / Mac App Store

**Date**: 2026-05-01
**Status**: accepted

### Context

Mac apps can be distributed via Developer ID + notarisation (no MAS), TestFlight Mac / Mac App Store (sandboxed), or local-only. The user explicitly chose TestFlight Mac / Mac App Store.

### Decision

The Mac app SHALL ship through TestFlight Mac / Mac App Store. The Mac target SHALL adopt the App Sandbox with entitlements for outbound HTTPS network client, App Group sharing, Keychain access group (including the iCloud-syncing variant), and (for the widget extension) the matching App Group + Keychain entitlements + network client (Decision 13).

### Rationale

TestFlight Mac handles installation and updates without any custom infrastructure. Sandboxing is required for MAS submission and is a security improvement regardless. Matches how the iOS app is already distributed.

### Alternatives Considered

- **Direct (Developer ID + notarised .dmg)**: Simpler entitlements, no sandbox required. Rejected by the user — TestFlight handles update plumbing and the user is willing to take on sandbox work.
- **Local-only (run from Xcode)**: No distribution at all in v1. Rejected by the user.

### Consequences

**Positive:**
- TestFlight Mac handles installation and update for the user
- Sandbox forces a clean entitlement story up front
- No notarisation pipeline to maintain

**Negative:**
- Sandbox imposes constraints (network client must be declared; no arbitrary disk access)
- App Store Connect setup required for the Mac product
- Switching to Developer-ID distribution later would be a one-way door for the App Group identifier (TeamID prefix changes between MAS-sandboxed and Developer-ID-signed apps), so the choice is effectively committed

---

## Decision 11: Switch Keychain Accessibility Class to `kSecAttrAccessibleAfterFirstUnlock`

**Date**: 2026-05-01
**Status**: accepted

### Context

The iOS app today uses `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` for the Keychain item holding the API token (per the Keychain wrapper convention in Arjen's iOS style guide §10). Decision 3 calls for iCloud Keychain sync of that item, which requires `kSecAttrSynchronizable = true`. Synchronisable Keychain items cannot use `…ThisDeviceOnly` accessibility classes — Apple's Security framework rejects them.

### Decision

New Keychain writes SHALL use `kSecAttrAccessibleAfterFirstUnlock` (without `ThisDeviceOnly`). Existing iOS items written under the legacy `…ThisDeviceOnly` class remain readable until the next save naturally rewrites them (Decision 3 — no proactive migration).

### Rationale

`AfterFirstUnlock` (without `ThisDeviceOnly`) keeps the property the style guide cares about — the item is accessible to background fetches after the device's first unlock — without the `ThisDeviceOnly` attribute that is incompatible with iCloud Keychain. The style guide's specific warning is against `kSecAttrAccessibleWhenUnlocked`, which is a stricter class that breaks background fetches; `AfterFirstUnlock` does not.

### Alternatives Considered

- **Use `kSecAttrAccessibleWhenUnlocked` for the synchronisable item**: Allowed by Security framework. Rejected — it breaks background fetches per style guide §10, which would silently degrade the iOS widget when the device is locked.
- **Skip iCloud Keychain entirely**: Avoid the accessibility-class change. Rejected by Decision 3.

### Consequences

**Positive:**
- iCloud Keychain sync works
- Single accessibility class for all new writes

**Negative:**
- Item is now accessible from any device the user logs into iCloud on (which is the point of sync, but worth noting in the security model). The token is a backend bearer for a personal-use service, so the threat model is consistent with this access policy.

---

## Decision 12: API Base URL — `NSUbiquitousKeyValueStore` as Source of Truth, App Group `UserDefaults` as Mirror

**Date**: 2026-05-01
**Status**: accepted

### Context

The API token is moving to iCloud Keychain (Decision 3). The API base URL is meaningless without the matching token, so it must travel with the token across devices. Today the URL lives in App Group `UserDefaults` (`UserDefaults.fluxAppGroup`), which is a per-device store with no sync. App Group `UserDefaults` is also where the widget extension reads the URL from, and sandboxed extensions cannot reliably observe `NSUbiquitousKeyValueStore`.

### Decision

The API base URL SHALL be persisted to `NSUbiquitousKeyValueStore` as the source of truth across devices. The host app SHALL mirror every URL change into App Group `UserDefaults` so the widget extension reads the URL from `UserDefaults` exactly as it does today. WHEN `NSUbiquitousKeyValueStore` notifies of an externally-changed URL, the host SHALL update the mirror within the same `@MainActor` step.

### Rationale

`NSUbiquitousKeyValueStore` is the iCloud key-value store and is the right surface for tiny shared settings like a base URL. Its 1MB / 1024-key limits are far beyond what we need. The widget extension cannot reliably observe it from a sandboxed extension, so the host app mirrors the value into the App Group `UserDefaults` the widget already reads — keeping the widget code unchanged.

### Alternatives Considered

- **Store URL only in App Group `UserDefaults`**: No sync. Rejected — token would propagate via iCloud Keychain but URL would not, so the user would still configure twice on a fresh device.
- **Store URL only in `NSUbiquitousKeyValueStore`, refactor widget to read from it**: Removes the mirror. Rejected — sandboxed extension reading `NSUbiquitousKeyValueStore` is finicky and not worth the change; mirroring is simpler.
- **Store URL in iCloud Keychain alongside the token**: Force them through the same store. Rejected — Keychain is for secrets; URLs are not secrets and Keychain entries are heavier than needed.

### Consequences

**Positive:**
- Widget extension code stays unchanged
- URL syncs across devices
- Single source-of-truth (host app's `NSUbiquitousKeyValueStore`) with a deterministic mirror

**Negative:**
- Two stores hold the URL; the mirror logic must run on every URL change in the host
- Edge case: if the host app is not running when iCloud propagates a new URL, the App Group mirror lags until the next host launch (the widget continues to use the previous URL until then — acceptable for this use case)

---

## Decision 13: Widget Extension Fetches API Independently of the Host

**Date**: 2026-05-01
**Status**: accepted

### Context

Decision 4 closes the host app when the main window closes. Decision 7 includes a Mac Control Center widget. If the widget were to rely on the host app to refresh the App Group snapshot cache, the Control Center widget would silently freeze the moment the user closed the main window — exactly the surprise the user would later report as a bug.

### Decision

The widget extension SHALL fetch live data directly from the API (`/status`) when its timeline provider runs and the App Group snapshot cache is older than the configured staleness threshold, regardless of whether the host app is currently running. The widget extension SHALL hold the `com.apple.security.network.client` entitlement (Mac and iOS).

### Rationale

Widgets that depend on a long-running host app are a smell. WidgetKit's timeline provider model exists precisely so widgets can refresh independently. The App Group snapshot cache becomes a freshness optimisation rather than the only path to data; if the cache is fresh, the widget uses it (no network); if it's stale, the widget fetches.

### Alternatives Considered

- **Widget reads only from snapshot cache; only host writes**: Smaller widget extension, but Control Center widget freezes when the host quits. Rejected.
- **Keep host running on close (Mail-style) so cache stays fresh**: Conflicts with Decision 4 and adds the "is the app running?" ambiguity. Rejected.
- **Mac Control Center widget reads through the host via XPC**: Possible but adds an XPC service and a separate refresh path. Rejected as over-engineered.

### Consequences

**Positive:**
- Widget always shows fresh data within its timeline cadence regardless of host state
- Snapshot cache becomes a clean freshness optimisation

**Negative:**
- Widget extension needs network client entitlement on both platforms (an iOS change too — minor, additive)
- Widget extension contains its own minimal API client path (likely shared via FluxCore — in line with Decision 8)
- Increased widget extension memory pressure during fetch (still well under WidgetKit's ~30MB limit)

---

## Decision 14: Control Center Widget via `ControlWidget` Protocol, Not `IntentTimelineProvider`

**Date**: 2026-05-01
**Status**: accepted

### Context

iOS lock screen accessory widgets use `accessoryCircular` / `accessoryRectangular` / `accessoryInline` families with a regular `TimelineProvider`. macOS Control Center is a different surface introduced in iOS 18 / macOS 26, with its own `ControlWidget` protocol, `ControlWidgetButton` / `ControlWidgetToggle` / `ControlWidgetView` building blocks, and a `ControlValueProvider` (rather than `TimelineProvider`) for refresh. `WidgetCenter.reloadTimelines(ofKind:)` does NOT drive `ControlWidget` updates — they refresh via `ControlCenter.shared.reloadControls(ofKind:)` and/or their own value-provider refresh path.

### Decision

The macOS Control Center surface SHALL be a `ControlWidget` (not an `accessoryCircular` widget reused from iOS). Refresh SHALL be triggered by `ControlCenter.shared.reloadControls(ofKind:)` from the host app on snapshot change, and by the `ControlValueProvider`'s own internal refresh from the widget extension.

### Rationale

Using the right API for the surface. `accessoryCircular` is the iOS lock screen family; reusing it on Mac via Control Center is not supported by WidgetKit. Using `ControlWidget` is the documented path.

### Alternatives Considered

- **Reuse `accessoryCircular` widget on Mac**: Rejected — wrong WidgetKit family; will not appear in macOS Control Center.
- **Skip Control Center widget for v1**: Rejected — explicitly in scope per user input on Decision 7.

### Consequences

**Positive:**
- Correct API usage
- Control Center widget appears in the right place in macOS System Settings

**Negative:**
- New protocol family in the widget bundle
- Refresh trigger is different from the home-screen widgets (two refresh paths to maintain in the host)
- Limited rendering primitives compared to a full `Widget` (Control Center widgets are mostly buttons/toggles plus glyph + value text)

---

## Decision 15: Mac UI Tests Deferred to v1.1

**Date**: 2026-05-01
**Status**: accepted

### Context

The iOS app has both unit tests (`FluxTests`) and UI tests (`FluxUITests`). Style guide §1 prescribes `make build-macos`, `make test`, `make test-ui` targets per platform. XCUITest on macOS is more flake-prone than on iOS, especially around sidebar interactions and window focus.

### Decision

The Mac v1 SHALL ship with unit-test coverage on macOS via `make test-macos` (running the existing FluxCore + Flux unit-test bundles on macOS). Mac UI tests SHALL be deferred to v1.1.

### Rationale

Unit tests are cheap to run on macOS — most are FluxCore tests that were already platform-agnostic. UI tests on macOS would require a parallel `FluxMacUITests` target with macOS-specific accessibility identifiers and gesture replacements, which is a separable effort with low marginal benefit for v1.

### Alternatives Considered

- **Ship Mac UI tests in v1**: Maximum coverage. Rejected — non-trivial effort and Mac XCUITest flakiness adds CI noise.
- **No Mac unit tests in v1**: Rely on iOS unit tests as a proxy. Rejected — FluxCore tests pass on macOS today and there's no reason not to run them on the Mac CI step.

### Consequences

**Positive:**
- Smaller v1 scope; CI stays green
- FluxCore + Flux unit-test bundles run on both platforms, catching platform-specific compile/test breakage early

**Negative:**
- Platform-specific UI behaviour (sidebar, Settings window, ⌘R, ←/→, key/inactive transitions) is not covered by automated tests in v1
- v1.1 needs to plan for Mac UI test scaffolding

---

## Decision 16: Single Bundle Identifier Across Both Platforms

**Date**: 2026-05-02
**Status**: accepted

### Context

Apps shipped to both the iOS App Store and Mac App Store can use either separate bundle identifiers (one per platform) or a single shared identifier with multiple platform SKUs in App Store Connect. The widget extension's App Group identifier (`group.me.nore.ig.flux`), the Keychain Access Group (`$(AppIdentifierPrefix)group.me.nore.ig.flux`), and the iCloud Keychain sync all key off the team identifier prefix — keeping them aligned across platforms reduces the migration surface.

### Decision

The Mac app SHALL use the same bundle identifier as the iOS app (`me.nore.ig.Flux`). App Store Connect SHALL host a single app record with two platform SKUs (iOS and macOS).

### Rationale

Single record, single product page, simpler App Store Connect operations. App Group and Keychain access group identifiers stay identical, so credential sync requires no special-casing for either platform.

### Alternatives Considered

- **Separate bundle ID (e.g. `me.nore.ig.Flux.macos`)**: Two App Store Connect records or one with two SKUs and divergent identifiers. Rejected — divergent identifiers complicate the App Group and Keychain access group story without a corresponding benefit.

### Consequences

**Positive:**
- App Group and Keychain access group are identical across platforms
- Single App Store Connect record to maintain

**Negative:**
- Cannot ship platform-only metadata (e.g. different screenshots / descriptions) under separate records
- Bundle ID is locked across platforms; future product split would require a rename

---

## Decision 18: No Keychain One-Shot Migrator — Lenient Read + Idempotent Write

**Date**: 2026-05-02
**Status**: accepted (supersedes the earlier migration plan implied by Decision 3 v1)

### Context

The first design draft included a five-case Keychain migration matrix (none, legacy-only, sync-only, both, sentinel-set), a `flux.keychain.migration.v1.complete` sentinel in App Group `UserDefaults`, and an `iCloudKeychainAvailability` probe to detect re-enabled iCloud Keychain. Both reviewers flagged this as fragile (probe write/delete cannot actually detect iCloud Keychain availability) and out of proportion for the use case (two-person personal app, both devices owned by the same person).

### Decision

No one-shot Keychain migrator. The Keychain wrapper SHALL handle both legacy non-synchronisable items and new synchronisable items via `kSecAttrSynchronizable = kSecAttrSynchronizableAny` on read and on delete-before-write. Existing iOS users keep working without any migration step. The "iCloud Keychain disabled" state is not separately diagnosed; if no item is found, the empty-state CTA routes the user to Settings exactly as it does today.

### Rationale

The user's posture is: two people use this app, both devices belong to the same person, "worst case I re-enter the URL and password again." Under that posture:

- Lenient read (`SynchronizableAny` on `loadToken`) means existing iOS items are picked up automatically — no migration step needed for the common case.
- Idempotent write (`SynchronizableAny` on `deleteToken` before `saveToken`) means any save converges to a single synchronisable item, regardless of what was there before.
- "iCloud Keychain disabled" gracefully degrades to a local-only item (writes still succeed locally; reads still find them); no separate fallback variant or Settings diagnostic is needed.

The trade-off is a brief indeterminate state if both items exist (e.g. iOS legacy + Mac-entered sync) — but both items hold the same token in the common case, and the moment the user touches Settings on either platform, the state resolves to a single item.

### Alternatives Considered

- **Five-case migration matrix with sentinel and re-enable check**: The original design. Rejected — overengineered; the re-enable probe was non-deterministic and the sentinel adds state that has to be reasoned about on every change.
- **Tiny one-shot delete-legacy migrator**: Just delete the legacy `…ThisDeviceOnly` item once on first launch with new code. Rejected — adds a sentinel and a deletion call for a case that resolves itself the first time the user saves anything; the indeterminate-token risk is small enough to accept.

### Consequences

**Positive:**
- Zero migrator code; KeychainService changes shrink to ~5 lines
- No sentinel state to reason about
- Existing iOS users see no difference until they install Mac

**Negative:**
- Brief possible indeterminate state when both legacy and sync items exist (acceptable per user posture)
- If the user has manually rotated the token between platform installs, they may need to re-save once on the platform whose stale item wins the indeterminate read

---

## Decision 17: Control Center Widget Tap Opens the Host App at Dashboard

**Date**: 2026-05-02
**Status**: accepted

### Context

macOS Control Center widgets built on `ControlWidgetButton` need an action — typically an `AppIntent`. The choice is between an `OpenIntent` (launches/foregrounds the host) and a custom `AppIntent` that performs work in the background (e.g. trigger a refresh, toggle a setting).

### Decision

Tapping the macOS Control Center widget SHALL open the host app at the Dashboard via an `OpenIntent`-conforming `AppIntent` (`OpenFluxIntent`). It SHALL NOT perform a background refresh, toggle, or other no-launch action.

### Rationale

The widget already shows the live SOC. Tapping it for more detail is the natural follow-through — exactly what the iOS widget does on tap. A background-refresh intent would add an `App Intent` infrastructure that is not used elsewhere in the codebase, for a feature the user did not ask for.

### Alternatives Considered

- **Toggle visibility (no app launch)**: Tap shows/hides the value glyph. Rejected — useless; Control Center widgets are always visible when added.
- **Background refresh-now intent**: Tap fires a `RefreshIntent` that re-fetches `/status`. Rejected — the value already refreshes via `ControlValueProvider`; user-initiated force refresh is an edge case best served by the host app.

### Consequences

**Positive:**
- Symmetric behaviour with iOS widget tap
- No new App Intent infrastructure required beyond the trivial `OpenIntent`

**Negative:**
- No way to force a refresh from the widget without opening the app (acceptable — the cadence is short)

---
