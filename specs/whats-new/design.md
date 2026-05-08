# Design: What's New

## Overview

Adds a new `WhatsNew` module to `FluxCore` containing the catalogue, version parsing, decision logic, and SwiftUI sheet. The two app targets attach a single sheet to `AppNavigationView` for auto-presentation and add a row to `SettingsView` for manual access.

## Architecture

### Module placement

New `Flux/Packages/FluxCore/Sources/FluxCore/WhatsNew/`, sibling to `Settings/` and `Widget/`. Files:

- `WhatsNewRelease.swift` — value types (`WhatsNewRelease`, `Highlight`, `Highlight.Category`).
- `WhatsNewVersion.swift` — `Comparable` parser/comparator.
- `WhatsNewCatalogue.swift` — hand-authored `releases: [WhatsNewRelease]` constant.
- `WhatsNewCoordinator.swift` — pure decision struct (no UI, no I/O).
- `WhatsNewSheet.swift` — SwiftUI sheet (public, used by both app targets).

Persistence helpers extend the existing `Settings/UserDefaults+Settings.swift` rather than introducing a new file.

### Integration points

| Site | File | Change |
|---|---|---|
| Auto-trigger (both platforms) | `Flux/Flux/Navigation/AppNavigationView.swift` | `.sheet(item: $pendingAuto)` where `pendingAuto: PendingAutoPresentation?` (a small `Identifiable` wrapper around `[WhatsNewRelease]`) is set in `.task`. Parallel to the existing iOS Settings sheet pattern in `FluxiOSRoot.swift:74`. |
| iOS Settings row | `Flux/Flux/Settings/SettingsView.swift` (iOS branch) | New `Section("About")` containing a `Button("What's New")` that flips a local `@State showingManualWhatsNew = false`. The sheet is hosted on the Settings view itself; iOS tolerates sheet-on-sheet. |
| macOS Settings row | same file (macOS branch) | New `LiquidGlassSection(title: "About")` with a row using the existing `FormRow` pattern (SettingsView.swift:215). Same local `@State` and `.sheet` host as iOS. The manual sheet appears within the Settings scene; this is a deliberate convention choice (see Decision 11). |
| Persistence | `Flux/Packages/FluxCore/Sources/FluxCore/Settings/UserDefaults+Settings.swift` | Add `lastSeenWhatsNewVersion: String?` and `hasAnyFluxPreferenceWritten: Bool` to the existing `Keys` enum and `UserDefaults` extension. |

The auto and manual presentation sites are independent — `WhatsNewSheet` is a plain SwiftUI view hosted at each call site. No coordinator singleton, no cross-scene state.

### Trigger lifecycle

- Auto-presentation runs once per `AppNavigationView` `.task`. AC 2.9 ("at most once per cold launch") is enforced by a single `@State Bool didEvaluateAutoPresentation` guard inside `AppNavigationView`.
- On macOS, `.task` can re-fire if the main window is closed and reopened within the same process. The guard is per-view-instance, so it resets on view reconstruction. This is benign: re-evaluation is idempotent — the coordinator returns `.skip` once `lastSeenWhatsNewVersion` is at the installed version, and `pendingAuto` only mutates when the result is `.present`.
- The `.task` glue reads `Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String`, parses with `WhatsNewVersion(_:)`, and `guard let installed = WhatsNewVersion(...) else { return }` to satisfy the unparseable-version `skip` clause in Error Handling. Only after that guard is the coordinator constructed.
- The `.task` glue applies each decision branch as follows:
  - `.skip`: nothing.
  - `.silentSet(version:)`: `UserDefaults.fluxAppGroup.lastSeenWhatsNewVersion = version` (no sheet).
  - `.present(releases:)`: `pendingAuto = PendingAutoPresentation(releases: releases)`, which triggers the sheet via `.sheet(item: $pendingAuto, onDismiss: ...)`.
- The `lastSeenWhatsNewVersion` write for the `.present` branch happens in the `.sheet(item:onDismiss:)` callback — not inside the sheet's Done button — so swipe-down on iOS, Esc on macOS, and the Done button all converge on the same write site.
- Manual dismissal does not write `lastSeenWhatsNewVersion`. The manual sheet uses a plain `.sheet(isPresented:)` on `SettingsView` with no `onDismiss` callback writing to that key.

### Manual access semantics

`SettingsView` calls `WhatsNewCoordinator.manualLatest()` independently of `lastSeenWhatsNewVersion`. This means a fresh-install user who taps "What's New" sees the most recent release entry for their installed version, even though the auto-presentation logic suppressed the sheet on first launch. If `manualLatest()` returns `nil` (empty catalogue), the Settings row is hidden — see Decision 14.

## Components and Interfaces

```swift
// WhatsNewRelease.swift
public struct WhatsNewRelease: Identifiable, Hashable, Sendable {
    public let version: String     // canonical form, e.g. "1.1" or "1.1.0"
    public let date: Date
    public let highlights: [Highlight]
    public var id: String { version }
}

public struct Highlight: Hashable, Sendable {
    public enum Category: String, CaseIterable, Sendable {
        case new, improved, fixed
        public var label: String {  // user-visible word: "New" / "Improved" / "Fixed"
            switch self { case .new: "New"; case .improved: "Improved"; case .fixed: "Fixed" }
        }
    }
    public let category: Category
    public let title: String
    public let detail: String?
    public let symbol: String?     // SF Symbol name; nil → derived from category
}
```

```swift
// WhatsNewVersion.swift
public struct WhatsNewVersion: Comparable, Hashable, Sendable {
    public let components: [Int]   // never empty; trailing zeros are NOT trimmed at parse,
                                   // but comparison treats missing trailing components as 0.
    public init?(_ string: String) // returns nil if any component fails Int parsing
    public static func < (lhs: Self, rhs: Self) -> Bool
}
```

Contract: `WhatsNewVersion("1.2") == WhatsNewVersion("1.2.0")`. Comparison is component-wise integer; the shorter array is right-padded with zeros for the comparison only. Returns `nil` for `"1.x"`, `"1-beta"`, empty string. Catalogue entries with unparseable versions are filtered out at load (via `compactMap`) — there is no runtime crash path.

```swift
// WhatsNewCoordinator.swift
public struct WhatsNewCoordinator: Sendable {
    public init(
        catalogue: [WhatsNewRelease],
        installed: WhatsNewVersion,
        lastSeen: String?,                  // raw stored string, may be nil
        hasAnyFluxPref: Bool
    )

    public enum AutoDecision: Equatable, Sendable {
        case present(releases: [WhatsNewRelease])  // newest-first; non-empty
        case silentSet(version: String)            // write to lastSeenWhatsNewVersion, no sheet
        case skip                                   // do nothing
    }

    public func autoDecision() -> AutoDecision
    public func manualLatest() -> WhatsNewRelease?
}
```

`autoDecision` resolution table (maps directly to AC 2.4–2.8):

| `lastSeen` | `hasAnyFluxPref` | installed vs effective last-seen | entries with highlights in (effective, installed] | result |
|---|---|---|---|---|
| nil | false | — | — | `silentSet(installed)` (AC 2.4) |
| nil | true | installed > "1.0" | ≥1 | `present` |
| nil | true | installed > "1.0" | 0 | `silentSet(installed)` (AC 2.7) |
| nil | true | installed ≤ "1.0" | — | `skip` (AC 2.6) |
| set | — | installed ≤ lastSeen | — | `skip`, no write (AC 2.6) |
| set | — | installed > lastSeen | ≥1 | `present` |
| set | — | installed > lastSeen | 0 | `silentSet(installed)` (AC 2.7) |

The "effective last-seen" for the `nil + hasAnyFluxPref` case is the seed `"1.0"` from Decision 3.

```swift
// WhatsNewCatalogue.swift
public enum WhatsNewCatalogue {
    public static let releases: [WhatsNewRelease] = [ /* hand-authored */ ]
}
```

```swift
// WhatsNewSheet.swift
public struct WhatsNewSheet: View {
    public init(releases: [WhatsNewRelease])  // precondition: non-empty;
                                              // call sites guarantee this via .sheet(item:)
                                              // or by hiding the Settings row when nil
    public var body: some View
}

/// Identifiable wrapper used as the `item:` in `.sheet(item:)` for auto-presentation.
struct PendingAutoPresentation: Identifiable {
    let id = UUID()
    let releases: [WhatsNewRelease]
}
```

Layout: `NavigationStack { ScrollView { releases ForEach { ReleaseSection(release:) } } }` with a `Done` `ToolbarItem(placement: .confirmationAction)`. Each `ReleaseSection` renders the version + `release.date` formatted as `Date.FormatStyle().month(.wide).year()` (e.g. "May 2026"), then highlights grouped by category in fixed order New → Improved → Fixed. Category symbols (when `Highlight.symbol == nil`): `sparkles` (new), `wand.and.stars` (improved), `checkmark.circle` (fixed) — defined in a private `extension Highlight.Category`. Each highlight row applies `.accessibilityElement(children: .ignore)` and an `accessibilityLabel` composed as `"\(category.label). \(title)\(detail.map { ". \($0)" } ?? "")"` (e.g. "New. Battery cutoff predictions. Shows when the battery will run out."), satisfying AC 6.1. iOS 26 / macOS 26 sheets are Liquid Glass by default; no extra material modifiers are needed.

```swift
// UserDefaults+Settings.swift additions

// Public — written from the app, read by the WhatsNew coordinator.
public extension UserDefaults {
    static let lastSeenWhatsNewVersionKey = "lastSeenWhatsNewVersion"

    var lastSeenWhatsNewVersion: String? {
        get { string(forKey: Self.lastSeenWhatsNewVersionKey) }
        set { set(newValue, forKey: Self.lastSeenWhatsNewVersionKey) }
    }
}

// Internal — only the WhatsNew coordinator uses this. Not part of the public FluxCore API.
extension UserDefaults {
    /// True if any pre-existing Flux preference key has ever been written on this device.
    /// Used to distinguish a fresh install from an upgrade-from-pre-feature (Decision 3).
    var hasAnyFluxPreferenceWritten: Bool {
        let known: [String] = [
            Self.apiURLKey,
            Self.themeIdentifierKey,
            Self.appFontFamilyKey,
            Self.loadAlertThresholdKey,   // declared next to apiURLKey etc. in the same file
            Self.widgetUsesSymbolsKey,
        ]
        return known.contains { object(forKey: $0) != nil }
    }
}
```

The two keys `loadAlertThresholdKey` and `widgetUsesSymbolsKey` currently exist only as raw strings inside the file's private `Keys` enum (UserDefaults+Settings.swift); promote them to `public static let` constants in the same extension as the existing `apiURLKey` / `themeIdentifierKey` / `appFontFamilyKey`. This avoids stringly-typed list maintenance and means a typo causes a compile error rather than silently misclassifying a pre-feature upgrade as a fresh install.

## Data Models

Already covered by `WhatsNewRelease` / `Highlight` above. No persistent storage beyond the single string preference; no SwiftData involvement.

## Error Handling

The only runtime failure modes that touch users:

- **Catalogue contains an unparseable version**: filtered out at load via `WhatsNewVersion(_:)` returning `nil`. Logged via `os_log` at `.error` so it surfaces during development; ships as a no-op for users.
- **`Bundle.main.infoDictionary?["CFBundleShortVersionString"]` missing or unparseable**: treated as `skip`. This is impossible in practice (Xcode enforces the key) but the coordinator must not crash if a synthetic bundle is ever constructed.
- **App-group `UserDefaults` unavailable**: `UserDefaults.fluxAppGroup` already falls back to `.standard` (UserDefaults+Settings.swift:5). The What's New code reads/writes via the same accessor and inherits that fallback.

## Testing Strategy

Tests live alongside FluxCore in the existing `FluxCoreTests` target, using Swift Testing.

**`WhatsNewVersionTests`** — example-based covers the surface adequately:
- `1.10 > 1.9` (AC 1.3)
- `1.2 == 1.2.0` and `1.2.0 == 1.2` (AC 1.3)
- `2.0 > 1.99`
- `WhatsNewVersion("1.x")` and `WhatsNewVersion("")` return `nil`
- Sort fixture `["1.0", "1.10", "1.2", "2.0", "1.9.1"]` descending equals `["2.0", "1.10", "1.9.1", "1.2", "1.0"]` (transitivity + numeric ordering pin)

PBT was considered for the comparator but rejected: the input space is small, the invariants are obvious from the table-driven cases, and Swift Testing's parameterised tests already cover the table cleanly. PBT would add Swift Testing harness boilerplate without raising real coverage.

**`WhatsNewCoordinatorTests`** — one test per row of the decision table above plus:
- `present` ordering is newest-first
- `present` filters out releases > installed (AC 2.8)
- `present` does not write `lastSeenWhatsNewVersion` (regression guard — write happens only on dismissal)
- `manualLatest()` returns the newest release ≤ installed with at least one highlight (AC 3.2)
- `manualLatest()` returns `nil` when the catalogue is empty
- `manualLatest()` is independent of `lastSeenWhatsNewVersion` (manual-on-fresh-install path)
- The seed `"1.0"` is honoured: pre-feature upgrade with installed `"1.1"` and a `1.1` entry → `present([1.1])` (Decision 3)
- Pre-feature upgrade with installed `"1.1"` and only a `2.0` entry in the catalogue → `silentSet("1.1")` (Decision 3 + Decision 9 in one path; the future entry is filtered, leaving zero in-range entries)
- Downgrade explicitly returns `.skip` (not `.silentSet`) and does not modify last-seen (AC 2.6, Decision 10)

**View test** — one Swift Testing snapshot or smoke test of `WhatsNewSheet(releases: fixture)` rendering without crashing across `[fixtureMultiRelease, fixtureSingleRelease, fixtureCategoryAbsent]`. No pixel-level assertions.

**Manual verification on device** — required because the version-bump path can't be exercised by unit tests:
1. Run the app at `MARKETING_VERSION = 1.0` once to populate other Flux prefs.
2. Bump `MARKETING_VERSION` to `1.1` and add a v1.1 entry to the catalogue.
3. Launch — sheet should auto-present with the v1.1 entry (covers Decision 3 seed path).
4. Dismiss, relaunch — sheet should not reappear.
5. Open Settings → What's New — sheet should reappear; relaunching after dismiss should still not auto-present.
6. Bump to `1.3` (skipping 1.2) with both 1.2 and 1.3 entries — both should appear stacked, newest first.
7. Roll back to `1.2` — sheet should not appear, last-seen should remain at `1.3`.
