# Implementation: Add Widgets (T-843)

This document explains the T-843 widget introduction at three expertise levels and closes with a completeness check against every requirement group in `requirements.md`.

---

## Beginner Level

### What Changed / What This Does

Flux is an iPhone app that shows the current state of a home battery: how charged it is, whether it's charging or discharging, and how much power the house is using versus making from solar panels. Until now you had to open the app to see any of this. This change adds iOS widgets — the small panels you can put on your Home Screen or Lock Screen — so the same numbers are visible at a glance without unlocking the phone.

Along the way the project also rearranged some of its internal parts. The data models, the code that calls the server, and the colour rules were moved into a new reusable bundle called `FluxCore`. The main app and the widgets both use this bundle, so there is only one copy of each rule to maintain.

### Why It Matters

- You can see battery state without opening the app.
- When the data gets old the widget clearly marks it as *stale* or *offline* so you don't act on bad numbers.
- If the widget can't reach the internet it falls back to the last value the app saved — you're never stuck looking at nothing useful.

### Key Concepts

- **Widget extension**: a separate mini-app that iOS runs on your behalf to draw the widget. It has a tight memory budget (~30 MB) and runs only when iOS asks it to.
- **App Group**: a shared storage area that both the main app and the widget can read and write. Flux uses one to pass the latest battery reading to the widget.
- **Timeline**: a list of entries the widget provider hands to iOS, each saying "show this content from time T". iOS picks the right one based on the current clock.
- **Staleness**: a classification (`fresh`/`stale`/`offline`) based on how long ago the data was fetched.
- **Keychain**: secure storage used for the API token. Both the app and the widget read from the same Keychain item.
- **Deep link**: tapping a widget opens the app at a specific URL (`flux://dashboard`) so you land on the right screen.

---

## Intermediate Level

### Changes Overview

Work split into phases by the branch commits:

1. **FluxCore package** (`Flux/Packages/FluxCore`): new local SPM package exposing `public` versions of the API client, models, Keychain service, formatters, colour helpers, app-group settings, and the new widget types. The main app target and the new widget target both depend on it; no source files are duplicated across targets.
2. **Widget extension target** (`Flux/FluxWidgets`): contains `FluxBatteryWidget` (home-screen families small/medium/large) and `FluxAccessoryWidget` (lock-screen families circular/rectangular/inline), plus their views and shared helpers.
3. **Main-app integration** (`Flux/Flux/Dashboard/DashboardViewModel.swift`): every successful `/status` fetch writes a `StatusSnapshotEnvelope` to the shared cache and, when the payload is fresher and at least 5 minutes have passed since the last reload, asks `WidgetCenter` to refresh timelines.
4. **Deep linking** (`Flux/Flux/Navigation/DeepLinkHandler.swift`, `Flux/Flux/Navigation/AppNavigationView.swift`): parses `flux://dashboard` URLs and routes to the Dashboard screen.
5. **CHANGELOG entry**: user-facing description of the widget introduction.

### Implementation Approach

**FluxCore package migration.** Each migrated file kept its semantics; only access levels changed to `public`. Tests moved alongside their sources. This preserves the rule that refactors must not change behaviour (req 9.3) and lets the widget depend on the same networking and formatting logic the app uses.

**Timeline / snapshot flow.** `StatusTimelineLogic` in `FluxCore/Widget` is the testable core:

- On every `timeline()` call it runs the `SettingsSuiteMigrator` (idempotent, exits early once `settingsMigrationVersion` matches), reads the cache, then tries a live fetch if a token is available.
- `fetchWithTimeout` uses a `ThrowingTaskGroup` with a 5-second sleep task to enforce the 5 s budget from req 5.5. The thin `StatusTimelineProvider` in the widget target only wires a `URLSession` whose `timeoutIntervalForRequest/ForResource` are both 5 s and delegates all logic to `StatusTimelineLogic`.
- The result is a `Timeline<StatusEntry>` with `.after(now + 30 min)` (req 5.1) and up to three entries at `now`, the `fresh → stale` boundary, and the `stale → offline` boundary — no interpolation, just the same snapshot re-classified at future dates (req 5.2).

**Staleness classification.** `StalenessClassifier` is a pure function mapping `(fetchedAt, now)` → `{fresh (<45 min), stale (45 min – 3 h), offline (>3 h)}`. Every view passes its `StatusEntry.staleness` into colour and label decisions. `WidgetAccessibility` prepends "Offline" when offline so VoiceOver announces staleness first (req 13.4).

**Deep link.** `WidgetDeepLink` exposes `dashboardURL = URL(string: "flux://dashboard")!` and a `parse(_:)` returning `.dashboard` for known hosts, `nil` for unknown. Each home-screen view applies `.widgetURL(WidgetDeepLink.dashboardURL)`. `Info.plist` registers the `flux` scheme; `AppNavigationView.onOpenURL` feeds the URL to `DeepLinkHandler` which switches the selected screen. Unknown links are dropped silently (req 8.3).

**App Group cache.** `WidgetSnapshotCache` persists a `StatusSnapshotEnvelope` (SchemaVersion + ISO-8601 timestamp + `StatusResponse`) as JSON under the shared suite `group.me.nore.ig.flux`. `writeIfNewer` reads the existing payload first and writes only if the new timestamp is strictly greater — this prevents a late widget refresh from clobbering a fresher app write (req 4.8). The central `UserDefaults.fluxAppGroup` accessor in FluxCore is the single entry point; widget views read `loadAlertThreshold` and `apiURL` through it so there is one source of truth.

**Keychain accessibility migrator.** Lock-screen widgets run while the device is locked, which means the token must be stored with `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`. `KeychainAccessibilityMigrator` (in `Flux/Flux/Settings/`) runs at app launch and rewrites any pre-existing item whose accessibility is stricter than that (req 11.5). The migrator is idempotent and only runs when a token exists.

**Settings suite migrator.** `SettingsSuiteMigrator` copies `apiURL` and `loadAlertThreshold` from `UserDefaults.standard` to the App Group suite exactly once, gated on a `settingsMigrationVersion` integer. Fresh installs write the version immediately (req 10.7). The widget re-runs the migrator defensively on every timeline refresh — the early-exit check is a single integer read, effectively free.

### Trade-offs

- **StaticConfiguration over AppIntentConfiguration**: v1 ships without user-configurable parameters (req 7.x). Simpler widget shell, no intent schema to maintain; future configuration can add `AppIntentConfiguration` without changing the data path.
- **Pre-computed three-entry timeline**: gives VoiceOver and the Home Screen a clear staleness transition without any future extrapolation. Anything beyond the `fetchedAt + 3 h` offline boundary waits for the next timeline refresh.
- **5-minute reload debounce**: the Dashboard polls `/status` every 10 s; reloading widgets at that cadence would blow the daily widget-reload budget. The 5-minute floor covers the foreground polling session while still forwarding fresh data promptly to the widget.
- **Strict `>` in `writeIfNewer`**: simpler than a tombstone and matches the design decision that equal timestamps mean duplicate writes we can safely ignore.

---

## Expert Level

### Technical Deep Dive

**Concurrency boundaries.** `StatusTimelineLogic` is a `Sendable` struct whose dependencies (`FluxAPIClient` existential, `WidgetSnapshotCache`, `tokenProvider`, `nowProvider`, `migrator`) are all `@Sendable` or `Sendable` final classes. The timeline entry point is `async`. `StatusTimelineProvider` bridges WidgetKit's completion-handler APIs with `Task { ... }`. The migrator closure defaults to `{ _ = SettingsSuiteMigrator.run() }`, which is fine because `SettingsSuiteMigrator` synchronously reads/writes `UserDefaults` — no actor hops. If that ever becomes async the signature is already ready.

**Timeout semantics.** The 5 s budget is enforced in two places defensively: (1) the widget's `URLSessionConfiguration` sets both `timeoutIntervalForRequest` and `timeoutIntervalForResource` to 5 s (see Decision 14), and (2) `fetchWithTimeout` races the fetch against a `Task.sleep(for: fetchTimeout)`. On timeout both tasks are cancelled via `group.cancelAll()`. The `StatusTimelineLogic.validateWidgetSessionTimeouts` helper is exposed specifically so tests can assert the session config is correct.

**Cache consistency.** `writeIfNewer` is not strictly atomic — it's a read-compare-encode-write sequence against `UserDefaults`. In practice this is acceptable: (a) the 5-minute dashboard debounce makes concurrent writes rare, (b) a torn payload is detected on next read via `schemaVersion` validation (req 4.7) which falls back to placeholder, and (c) `UserDefaults.set(_:forKey:)` itself is documented as atomic per key. Considered a proper file lock but rejected as over-engineered for this volume.

**Keychain under Secure Enclave constraints.** When the device boots and the user has not yet unlocked it, Keychain returns `errSecInteractionNotAllowed` for items whose accessibility is `AfterFirstUnlockThisDeviceOnly`. Requirement 11.6 says the widget must not attempt a live fetch in that state. The implementation: `tokenProvider` returns nil when the read fails for any reason, and `StatusTimelineLogic.timeline()` only attempts a fetch when the token is present. Result: locked-boot path is cache-only, naturally transitioning to `stale` / `offline` until the user unlocks.

**Relevance scoring.** `RelevanceScoring.score(staleness:live:battery:)` returns a `TimelineEntryRelevance` whose score boosts high-signal moments (low SOC, high load, offline) so iOS prioritises redraws during them. This is best-effort — iOS reserves the right to ignore it — but costs nothing to emit.

**Deep link hardening.** The scheme is a simple `flux://dashboard`. Parsing tolerates unknown hosts (returns nil → no-op) so future widgets can emit `flux://day/2026-04-21` without crashing older app builds. The force-unwrap on `URL(string: "flux://dashboard")!` is bounded by a static literal; this is the standard Swift pattern for known-valid URLs.

**Accessibility label composition.** `WidgetAccessibility.label(for:family:)` is a switch over widget family that concatenates SOC, status verb, cutoff (when discharging), and — for home-screen families — the power trio. When staleness is `offline` the label is prefixed with "Offline. " unconditionally. Colour is never the sole signal (req 13.3); every colour decision is paired with a text verb or the `offline` marker.

### Architecture Impact

- **FluxCore as the shared-code boundary.** The package has zero SwiftUI dependencies in its core types (`Models`, `Networking`, `Security`, `Widget/StatusTimelineLogic`). SwiftUI-specific files live per target (`FluxWidgets/ColorTier+Color.swift` and `Flux/Flux/Helpers/ColorTier+Color.swift` — identical by design, see Decision 10 variant). This keeps the package usable if Flux ever picks up a non-SwiftUI consumer.
- **No hand-rolled stringly-typed keys in widget code.** Suite name, keys, and widget kinds live in FluxCore (`UserDefaults.fluxAppGroupSuiteName`, `WidgetKinds.battery/.accessory`). `DashboardViewModel`, `LoadRow`, `PowerTrioColumns`, and `StatusTimelineProvider` all read through those constants.
- **Dashboard view model as cache writer.** Every successful refresh calls `widgetCache.writeIfNewer(envelope)` and, if written and outside the 5-min debounce, `widgetReloadTrigger()`. Cheap when nothing changes (a timestamp comparison and an encode) but a measurable steady-state write at 10 s intervals — a future optimisation could diff the status payload before encoding.

### Potential Issues

- **Snapshot freshness vs. foreground polling.** Dashboard polls every 10 s; each successful poll currently re-encodes the envelope even when `StatusResponse` fields are unchanged. Over a 30-minute foreground session that's ~180 encodes. Not urgent because `writeIfNewer` short-circuits on equal timestamps via strict `>`, and the debounce prevents reload churn, but a payload-diff optimisation is noted.
- **Migrator runs on every widget timeline refresh.** Cost is one `UserDefaults.integer(forKey:)` call after the first successful migration; acceptable but not free. Could be gated behind a one-time check in the provider if it ever shows up in traces.
- **Cache double-decode in `writeIfNewer`.** `read()` decodes JSON to compare timestamps; if the new envelope is newer the code then encodes. A cheaper path would keep only the `fetchedAt` in a companion key and decode the full payload lazily, but the current approach is simple and safe.
- **`fatalError` in `UserDefaults.fluxAppGroup`.** Fires if the App Group is not provisioned. Acceptable because both entitlements files already declare it; if provisioning ever breaks we'd rather crash loudly in development than silently read the wrong defaults.

---

## Completeness Assessment

Requirement status verified against committed code and tests.

| Req group | Title | Status | Key implementation |
|---|---|---|---|
| 1 | Widget Families Supported | ✅ Implemented | `FluxBatteryWidget` declares `.systemSmall/.systemMedium/.systemLarge`; `FluxAccessoryWidget` declares `.accessoryCircular/.accessoryRectangular/.accessoryInline`; each family has its own distinct view under `Flux/FluxWidgets/Views/`. |
| 2 | Battery State Content | ✅ Implemented | `SOCHeroLabel`, `StatusLineLabel`, `AccessoryCircularView`, `AccessoryInlineView` per-family layouts; SOC formatted with one decimal; `BatteryColor.forSOC()` applied. |
| 3 | Power Trio Content | ✅ Implemented | `PowerTrioColumns` for medium/large; `LoadRow` for small; `GridColor.forGrid` applied; lock-screen families exclude power. `loadAlertThreshold` now read from `UserDefaults.fluxAppGroup`. |
| 4 | Data Source and Freshness | ✅ Implemented | `StatusTimelineLogic.timeline()` fetches + writes cache + falls back; 5 s timeout via URLSessionConfiguration and TaskGroup race; `schemaVersion` guards `WidgetSnapshotCache.read()`; `writeIfNewer` uses strict `>`. |
| 5 | Timeline Refresh Policy | ✅ Implemented | `.after(now + 30 min)` policy; 1–3 entries at staleness boundaries; `DashboardViewModel.shouldTriggerWidgetReload` enforces 5-min debounce; single fetch per refresh. |
| 6 | Staleness Presentation | ✅ Implemented | `StalenessClassifier` (45 min / 3 h thresholds); `StalenessFootnote` renders relative age on `stale`; dim logic in each family view; `AccessoryInlineView` drops SOC when offline. |
| 7 | Configuration (not required v1) | ✅ Implemented | Both widgets use `StaticConfiguration`; no `WidgetConfigurationIntent`. |
| 8 | Tap Behaviour and Deep Linking | ✅ Implemented | `WidgetDeepLink.dashboardURL` + `.widgetURL(...)` on home-screen views; `Info.plist` registers `flux` scheme; `DeepLinkHandler` routes to Dashboard; unknown hosts no-op. |
| 9 | Shared Code Architecture | ✅ Implemented | `FluxCore` package at `Flux/Packages/FluxCore` targeting iOS 26; `APIModels`, `FluxAPIError`, `FluxAPIClient`, `URLSessionAPIClient`, `KeychainService`, `DateFormatting`, `PowerFormatting`, `BatteryColor`, `GridColor`, `CutoffTimeColor` migrated with public access; `UserDefaults+Settings` also migrated for widget reuse. Tests migrated to `FluxCoreTests`. |
| 10 | Shared App Group Cache | ✅ Implemented | `group.me.nore.ig.flux` enabled on both targets; `WidgetSnapshotCache` persists JSON `StatusSnapshotEnvelope`; `UserDefaults.fluxAppGroup` single accessor; `SettingsSuiteMigrator` handles v1 migration and records `settingsMigrationVersion`. |
| 11 | Security and Credentials | ✅ Implemented | Widget reads via `KeychainService.loadToken()`; no write/delete calls from widget code; no token logging; `KeychainAccessibilityMigrator` upgrades existing items to `.afterFirstUnlockThisDeviceOnly`; locked-boot path returns cache-only. |
| 12 | Liquid Glass Styling | ✅ Implemented | Every view applies `.containerBackground(for: .widget) { Color.clear }`; no solid fills or custom materials; SF fonts only; Dynamic Type respected. |
| 13 | Accessibility | ✅ Implemented | `WidgetAccessibility.label(for:family:)` returns a single combined label per family; "Offline" prefix on offline; status verb always present; colour is never the only channel. |
| 14 | Preview and Placeholder | ✅ Implemented | `WidgetFixtures` supplies 68% SOC representative entry; `StatusTimelineLogic.snapshot(isPreview:)` returns placeholder on preview, live+fallback otherwise; first-install path attempts live fetch when a token exists. |
| 15 | Testability | ✅ Implemented | `StatusTimelineLogic.init(...)` injects `apiClient`, `cache`, `tokenProvider`, `nowProvider`, `fetchTimeout`, `migrator`; `StalenessClassifier` tested as a pure function; `WidgetSnapshotCacheTests` cover encode/decode/missing/invalid-schema; every widget view has a `#if DEBUG` `#Preview`. |
| 16 | Shipping and Backwards Compatibility | ✅ Implemented | Widget target deployment iOS 26.4; App Group identifier unchanged; migrations non-destructive; `CHANGELOG.md` has an entry describing the introduction. |

### Items Flagged

No requirement failed the "can I explain it clearly?" check. All 16 groups map cleanly to committed code paths.

One minor observation — requirement 4.4 specifies placeholder text "No data yet — open Flux". The placeholder copy lives inside the per-family views (e.g. `SystemSmallView`, `AccessoryInlineView`); a unit test could assert the literal string if future copy changes become a concern.

### Divergences from Design

No undocumented divergences. Decisions 13 (targeted reload), 14 (session-level timeout), 16 (strict `>`), and 19 (FluxCore widget consolidation) are all reflected in `decision_log.md`.

### Pre-push Review Notes

This document was written after a pre-push review that consolidated duplicated `UserDefaults` access in widget views (`LoadRow`, `PowerTrioColumns`, `StatusTimelineProvider`) behind `UserDefaults.fluxAppGroup` and moved widget kind constants into `WidgetKinds` in FluxCore. Behaviour unchanged; build and tests pass.
