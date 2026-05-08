# Implementation: What's New (T-1112)

## Beginner Level

### What This Does

Flux now has a small "What's New" sheet that pops up the first time you open the app after an update, summarising what changed in plain language. There's also a "What's New" button in Settings to bring the same sheet back later.

### Why It Matters

The existing `CHANGELOG.md` is written for developers — full of ticket IDs and engineering jargon. Most users won't read it, and they won't go looking for release notes. By auto-presenting a friendlier summary on a version bump, users actually discover new features instead of stumbling onto them by accident weeks later.

### Key Concepts

- **Marketing version**: The user-visible version number (e.g. `1.1`) that lives in the app's `Info.plist`. It bumps each time you release a new build.
- **App-group UserDefaults**: A shared preferences store that the iOS app, the macOS app, and the widgets can all read. Used here to remember the last version the user dismissed.
- **Sheet**: A modal panel that slides up from the bottom on iOS or appears centred on macOS, dismissible by swiping down or pressing Escape.

---

## Intermediate Level

### Changes Overview

A new `WhatsNew/` module under `FluxCore` containing five files:

- `WhatsNewRelease.swift` — value types: `WhatsNewRelease`, `Highlight`, `Highlight.Category` (`new` / `improved` / `fixed`).
- `WhatsNewVersion.swift` — `Comparable` value type wrapping `[Int]`. `1.2 == 1.2.0` and `1.10 > 1.9`.
- `WhatsNewCatalogue.swift` — hand-authored `releases: [WhatsNewRelease]`. Currently a single `1.1` entry.
- `WhatsNewCoordinator.swift` — pure decision struct; takes `(catalogue, installed, lastSeen, hasAnyFluxPref)` and returns `AutoDecision = .present | .silentSet | .skip`. Also exposes `manualLatest()` and a `forCurrentInstall()` factory.
- `WhatsNewSheet.swift` — SwiftUI sheet plus `PendingAutoPresentation` identifiable wrapper.

Persistence and call-site wiring:

- `Settings/UserDefaults+Settings.swift` — adds `lastSeenWhatsNewVersion: String?` and an internal `hasAnyFluxPreferenceWritten: Bool` that checks the five known keys (`apiURL`, `themeIdentifier`, `appFontFamily`, `loadAlertThreshold`, `widgetUsesSymbols`).
- `Flux/Navigation/AppNavigationView.swift` — `.task` runs `evaluateWhatsNewAutoPresentation()` once per cold launch. The `.sheet(item:onDismiss:)` is the single converged write site for swipe-down, Escape, and the Done button.
- `Flux/Settings/SettingsView.swift` — caches the `manualLatest()` result in `@State` on `.onAppear`, hides the "About → What's New" row when nil (Decision 14).

### Implementation Approach

- **Pure-value coordinator**. The decision logic lives in `WhatsNewCoordinator`, a `Sendable` struct with no I/O. The view layer reads `Bundle` and `UserDefaults`, then asks the coordinator what to do. This keeps every row of the AC decision table testable without mocking. (Decision 12.)
- **Single converged write site**. `lastSeenWhatsNewVersion` is written in `.sheet(onDismiss:)`, not in the Done button — so swipe-down, Escape, and Done all go through the same code path.
- **Fresh install vs pre-feature upgrade**. Both look like "no `lastSeenWhatsNewVersion`," so the coordinator inspects `hasAnyFluxPreferenceWritten` to disambiguate. Pre-feature upgrades seed an effective `1.0` (Decision 3). Fresh installs silently advance the counter without showing anything.
- **Future-version filtering**. Catalogue entries with version > installed are silently dropped (Decision 9), so notes can land in source ahead of a `MARKETING_VERSION` bump.
- **Hand-authored catalogue**. No CHANGELOG.md parsing (Decision 1) — voice and audience differ from the engineering changelog.

### Trade-offs

- The catalogue is a typed Swift array, not JSON or markdown. Updating notes requires a code change and rebuild — acceptable since notes ship with the release anyway.
- The "is this an upgrade?" signal is the presence of any other Flux preference. Soft coupling: if all five keys were ever cleared, an existing user would be misclassified as fresh and silently miss the next entry. Documented in Decision 3.
- Last-seen is per-device (Decision 6). Dismissing on iPhone doesn't suppress the sheet on iPad. Worst case: minor re-read, no data loss — chosen over iCloud KVS to avoid sync conflicts for a low-stakes preference.
- Manual access shows only the latest release (Decision 7), not a historical archive.

---

## Expert Level

### Technical Deep Dive

- **Version equivalence and hashing**. `WhatsNewVersion` parses via `String.split(".")` + `Int.init`, returns `nil` on any failure. `<`, `==`, and `hash(into:)` all canonicalise by trimming trailing zeros (`trim(_:)` helper), so `1.2`, `1.2.0`, and `1.2.0.0` form a single equivalence class. The Hashable invariant `a == b ⇒ hash(a) == hash(b)` holds.
- **Coordinator state**. `parsedCatalogue` is computed once at init via `compactMap` over `WhatsNewVersion.init(_:)`, dropping any entry with an unparseable version string. `installedString` resolves to the catalogue's spelling for the installed version when present, otherwise falls back to `canonicalString(for:)` (joins `components` with dots). Exposed as `canonicalInstalledVersion` so call sites persist a single canonical form.
- **Auto-presentation lifecycle**. `.task` fires once per `AppNavigationView` instance; a `@State Bool didEvaluateAutoPresentation` guard makes re-fires (e.g. on macOS window-close-and-reopen) idempotent. The coordinator returns `.skip` once `lastSeen` is at the installed version, and `pendingAuto` only mutates on `.present`.
- **Manual access semantics**. `SettingsView.onAppear` calls `WhatsNewCoordinator.forCurrentInstall()?.manualLatest()` once and stores the result in `@State`. This avoids re-parsing the catalogue on every `body` re-evaluation (which fires on every `TextField` keystroke). The Settings row is hidden when the cache is `nil`.
- **Test layout**. Coordinator tests cover every row of the design's decision table by direct construction with `(catalogue, installed, lastSeen, hasAnyFluxPref)` quadruples. Version tests pin the numeric ordering with a sort fixture. Sheet tests use `UIHostingController` / `NSHostingController` smoke tests across three fixtures (multi-release, single-release, no-detail). UserDefaults tests use a fresh `UserDefaults(suiteName:)` per test with `removePersistentDomain` cleanup.

### Architecture Impact

- New decision-only module fits the existing `FluxCore` shape: `Settings/`, `Widget/`, `WhatsNew/` are siblings. No new dependency on iOS or AppKit specifics — the sheet is `import SwiftUI` only.
- `forCurrentInstall(bundle:defaults:catalogue:)` is the single bridge between FluxCore and the running process. Both `AppNavigationView` and `SettingsView` use it; if a future widget ever needs to know "what's new," the same factory works.
- The hand-rolled `WhatsNewVersion` does not depend on `OperatingSystemVersion` or `URLComponents`. Drop-in replaceable; nothing else parses dot-versioned strings in this codebase (verified by grep).
- The `hasAnyFluxPreferenceWritten` heuristic is internal-access. Adding a new preference key in the future requires extending the known-keys list — small but real maintenance cost.

### Potential Issues

- **Catalogue authoring discipline.** Decision 4 makes per-release entries optional. A version bump without a matching entry silently advances the last-seen counter — relies entirely on author judgement. No PR-time enforcement.
- **`hasAnyFluxPreferenceWritten` false negative.** If a user wipes app data (e.g. `defaults delete`) but the build is post-feature, the next launch looks like a fresh install. They'd miss any unseen release notes. Acceptable: extremely rare, no data loss.
- **Catalogue date helper.** `WhatsNewCatalogue.dateOf(year:month:)` now uses `DateFormatting.sydneyCalendar` and `fatalError`s on impossible inputs — surfaces author bugs at boot rather than shipping a 1970 epoch fallback.
- **Sheet typography.** `WhatsNewSheet` uses raw `.font(.title3)`/`.body`/`.subheadline` instead of the app's `.appFont(...)` system because `AppFont` lives in the Flux target and FluxCore can't depend on it. Known-and-accepted gap.
- **macOS `.task` re-fire.** Closing and reopening the main window on macOS reconstructs the view; the per-instance `didEvaluateAutoPresentation` guard resets but the coordinator returns `.skip` once `lastSeen` is up-to-date, so the only observable cost is one extra Bundle/UserDefaults read pair.

---

## Completeness Assessment

### Fully Implemented

- Requirement 1 (Release Content Catalogue): `WhatsNewRelease`, `Highlight`, category enum, optional fields, version comparison rules. AC 1.1–1.4 all reflected in code and tests.
- Requirement 2 (Auto-Presentation on Version Bump): `evaluateWhatsNewAutoPresentation` reads `CFBundleShortVersionString`, builds the coordinator, and applies the decision. The `.sheet(onDismiss:)` writes back. Fresh-install / pre-feature-upgrade / downgrade / future-version branches all match the design table. AC 2.1–2.9 covered.
- Requirement 3 (Manual Access from Settings): "About → What's New" row in both iOS and macOS branches; uses `.sheet(isPresented:)` with no dismiss writeback (AC 3.3); hidden when no eligible release exists (Decision 14).
- Requirement 4 (Sheet Presentation): `NavigationStack` + `ScrollView` + `Done` `confirmationAction`; categories rendered in fixed `new → improved → fixed` order; SF Symbol mapping with optional override.
- Requirement 5 (Cross-Platform): identical `.task` glue runs on both iOS and macOS; per-device `lastSeenWhatsNewVersion` lives in `UserDefaults.fluxAppGroup`.
- Requirement 6 (Accessibility): per-row `.accessibilityElement(children: .ignore)` with composed label; Done button labelled "Dismiss What's New".
- Initial 1.1 catalogue entry with six highlights covering the macOS launch, theme picker, font picker, dashboard redesign, history overview, and day blocks.

### Partially Implemented / Soft Spots

- **Manual sheet write-site test.** Decision 12 explicitly chose to keep the coordinator I/O-free, so the "manual sheet does not write `lastSeenWhatsNewVersion`" assertion has no unit-test home. Acknowledged in design.md and not regressed by this change.
- **Catalogue version-uniqueness.** No build-time check prevents a catalogue from containing both `1.1` and `1.1.0`. With one spelling per version (current state), the canonical-string resolution is deterministic.

### Not Implemented (Intentional Per Spec)

- Localization (explicit non-goal).
- Historical archive of all releases (Decision 7 — only latest manual).
- v1.0 backfill entry (Decision 3 — pre-feature upgrade is detected at runtime instead).
- App Store Connect release-notes automation, push messaging, remote-fetched catalogue (all explicit non-goals).
