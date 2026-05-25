# Design: Better macOS Interface (T-1342)

## Overview

Activate the iPad adaptive multi-column body on macOS by flipping `IPadLayoutGate.isActive()` to return `true` on macOS, drop two in-content header blocks (Dashboard `legacyHeader`, Day Detail `DayNavigationHeader`) on macOS only, add a window toolbar (date as nav title + prev/next chevrons) to Day Detail on macOS, and clamp the macOS scene to `minWidth: 960, minHeight: 600`.

## Architecture

### Per-Call-Site Gate Audit (required by [AC 5.3](requirements.md#5.3))

| Call site | File:line | Compile platform | Current behavior | Desired macOS behavior | Implementation |
|---|---|---|---|---|---|
| `DashboardView.usesRegularLayout` | `Flux/Flux/Dashboard/DashboardView.swift:107` | iOS + macOS | macOS: `false` → renders `dashboardContent` (incl. `legacyHeader`) | `true` → renders `dashboardContentRegular` (no `legacyHeader`) | Use shared gate; behavior follows from gate flip. |
| `DayDetailView.usesRegularLayout` | `Flux/Flux/DayDetail/DayDetailView.swift:126` | iOS + macOS | macOS: `false` → renders `dayDetailContent` (incl. legacy eyebrow `header`) | `true` → renders `dayDetailContentRegular` (no eyebrow `header`; `DayNavigationHeader` skipped via `#if !os(macOS)`) | Use shared gate; combine with `#if !os(macOS)` around the `DayNavigationHeader` call inside `dayDetailContentRegular`. |
| `HistoryView.usesRegularLayout` | `Flux/Flux/History/HistoryView.swift:137` | iOS + macOS | macOS: `false` → renders `historyContent` | `true` → renders `historyContentRegular` | Use shared gate; behavior follows from gate flip. |
| `AppNavigationView.usesPadShell` | `Flux/Flux/Navigation/AppNavigationView.swift:201` (inside `#if !os(macOS)` block opening at line 173) | iOS only | n/a on macOS — never compiles | n/a — macOS uses `macOSRoot` directly | No change needed. Gate change does not reach this site on macOS. |
| `IPadFormWidthCap.shouldApply` | `Flux/Flux/Settings/SettingsView.swift:349` (struct at line 336, inside `#if !os(macOS)` block opening at line 331) | iOS only | n/a on macOS — never compiles | n/a — macOS Settings is a separate scene with its own `.frame(minWidth: 480, minHeight: 360)` | No change needed. Gate change does not reach this site on macOS. |

Verification: `rg -n "IPadLayoutGate.isActive" Flux/` returns exactly these 5 call sites plus the gate's own definition. `AdaptiveColumnsLayout.swift:15` is a doc-comment reference, not a call. `ChartDetailScene` and other macOS-only scenes do not reference the gate.

Outcome: flipping the gate's `#if os(iOS)` / `#else` branch to `true` for `os(macOS)` is safe — only the three view sites observe the change at runtime, and each one already does the right thing in its `dashboardContentRegular` / `dayDetailContentRegular` / `historyContentRegular` branch.

### Flow on macOS (post-change)

```
FluxApp (WindowGroup, minWidth=960, minHeight=600, defaultSize=1200x800)
  └── AppNavigationView.macOSRoot
       └── NavigationSplitView
            ├── SidebarView
            └── NavigationStack(path:)
                 └── currentScreenView  (.scrollContentBackground(.hidden) already applied)
                      ├── DashboardView           → dashboardContentRegular
                      ├── DayDetailView (today)   → dayDetailContentRegular  (+ macOS toolbar)
                      ├── HistoryView             → historyContentRegular
                      └── (HistoryView pushes DayDetailView for non-today dates)
```

## Components and Interfaces

### `IPadLayoutGate` (modify)

```swift
enum IPadLayoutGate {
    static func isActive(hSizeClass: UserInterfaceSizeClass?) -> Bool {
        #if os(iOS)
        return UIDevice.current.userInterfaceIdiom == .pad && hSizeClass == .regular
        #elseif os(macOS)
        return true                              // NEW: macOS always takes adaptive bodies
        #else
        return false
        #endif
    }
}
```

Doc comment updated to drop the "macOS never qualifies" sentence and add a one-liner about macOS always returning `true` (the scene's `minWidth=960pt` makes 1-column unreachable at runtime). Name retained per Decision 1 in scope; rename is a non-blocking follow-up.

### `DayDetailView` (modify)

Two surgical changes:

1. Inside `dayDetailContentRegular` (`DayDetailView.swift:158`), wrap the `DayNavigationHeader` call:
   ```swift
   #if !os(macOS)
   DayNavigationHeader(viewModel: viewModel)
   #endif
   ```
   No structural rewrite; iOS iPad continues to render it.

2. Add a macOS-only navigation title + toolbar block on the `ScrollView` (or its container). The existing `.navigationTitle(usesRegularLayout ? pageTitle : "")` at line 78 is inside `#if os(iOS)`, so on macOS the title is currently empty — the new macOS block is additive, not a replacement, and there is exactly one `.navigationTitle` active per compile target (no last-one-wins ambiguity). Added after the existing `#if os(iOS)` block at line 69-79:
   ```swift
   #if os(macOS)
   .navigationTitle(macPageTitle)
   .toolbar {
       ToolbarItemGroup(placement: .primaryAction) {
           Button {
               viewModel.navigatePrevious()
           } label: {
               Image(systemName: "chevron.left")
           }
           .accessibilityLabel("Previous day")
           .help("Previous day")          // macOS hover tooltip per HIG

           Button {
               viewModel.navigateNext()
           } label: {
               Image(systemName: "chevron.right")
           }
           .accessibilityLabel("Next day")
           .help("Next day")
           .disabled(viewModel.isToday)
       }
   }
   #endif
   ```
   `macPageTitle` is a new `#if os(macOS)`-gated computed property — `internal` (not `private`) so the unit test for [AC 7.4](requirements.md#7.4) can read it directly via `@testable import Flux`:
   ```swift
   #if os(macOS)
   var macPageTitle: String {
       if viewModel.isToday { return "Today" }
       guard let parsedDate = DateFormatting.parseDayDate(viewModel.date) else {
           return viewModel.date
       }
       return DayDetailEyebrow.full.string(from: parsedDate)
   }
   #endif
   ```
   SwiftUI dependency tracking: `.navigationTitle(macPageTitle)` is evaluated inside `body`; `macPageTitle` reads `viewModel.date` and `viewModel.isToday`; `viewModel` is `@State`, so any mutation to those properties triggers a body re-evaluation and the title rebinds. This satisfies [AC 4.3](requirements.md#4.3)'s reactive-read guarantee on both first render and on every subsequent date / isToday change. The dedicated computed property (vs reusing the existing `pageTitle`) is required because `pageTitle` uses `DayDetailEyebrow.formatter` while [AC 4.2](requirements.md#4.2) specifies `DayDetailEyebrow.full` (Decision 10). The fallback to raw `viewModel.date` matches `DayNavigationHeader.formattedDate` behavior.

The existing `.focusable()` (line 96) and `.onKeyPress(.leftArrow)` / `.onKeyPress(.rightArrow)` (lines 97-105) handlers stay as-is. They already function on macOS today (verifiable via the existing macOS keyboard-nav path). No `keyboardShortcut` is added to the toolbar buttons (would double-fire with `.onKeyPress`). One implementation note: when the next-day toolbar button is `.disabled(viewModel.isToday)`, AppKit may still grant focus to the disabled button on tab; the `.onKeyPress` handler on the `ScrollView` continues to receive the event because focus management on the disabled button is a no-op rather than a focus-grab.

### `DashboardView` (modify)

Body branch selection auto-resolves once the gate returns `true` on macOS (picks `dashboardContentRegular`; `legacyHeader` is unreachable from the regular branch — [AC 3.1](requirements.md#3.1)). The existing `.navigationTitle(usesRegularLayout ? "Dashboard" : "")` at line 52 is inside `#if os(iOS)`, so on macOS the title is currently empty. Add an explicit macOS title block to satisfy [AC 3.2](requirements.md#3.2):

```swift
#if os(macOS)
.navigationTitle("Dashboard")
#endif
```

### `HistoryView` (no changes)

Picks `historyContentRegular` automatically once the gate returns `true` on macOS. The existing `.navigationTitle("History")` at line 89 is unconditional, so [AC 3.2](requirements.md#3.2) parity for History is already in place.

### `FluxApp` (modify)

Add scene sizing + resizability constraint to the macOS `WindowGroup`:

```swift
WindowGroup {
    AppNavigationView()
        .environment(refreshCoordinator)
        .environment(chartScopeRegistry)
        .macOSChartExpansion(registry: chartScopeRegistry, focus: chartExpansionFocus)
        .preferredColorScheme(preferredScheme)
        .frame(minWidth: 960, minHeight: 600)             // NEW: content-view minimum
}
.defaultSize(width: 1200, height: 800)                     // NEW: opening size
.windowResizability(.contentSize)                          // NEW: clamps NSWindow to content min
.modelContainer(for: CachedDayEnergy.self)
.commands {
    FluxKeyboardCommands(coordinator: refreshCoordinator)
}
```

**Why `.windowResizability(.contentSize)` is required:** `.frame(minWidth:minHeight:)` alone constrains the SwiftUI content view but does *not* clamp the macOS `NSWindow` resize behavior — the user can drag the window smaller and the content gets clipped. `.windowResizability(.contentSize)` ties the window's resize limits to the content view's `frame` constraints, which is the documented way to set a hard window minimum on macOS 14+ ([SwiftUI Scene.windowResizability(_:)](https://developer.apple.com/documentation/swiftui/scene/windowresizability(_:))). Without it, [AC 2.2](requirements.md#2.2) is not actually enforced.

**Defending `defaultSize` (not in requirements):** Without it, macOS opens new windows at the *minimum* size (960×600) — which is functional but cramped. `defaultSize(1200, 800)` opens with one extra column of breathing room (still hits the 2-column tier in `AdaptiveColumnsLayout`). This is a one-line UX call; documented here so the implementer doesn't omit it.

**Other scenes unaffected:** The Settings scene already has `.frame(minWidth: 480, minHeight: 360)`. The `ChartDetailScene` (separate `WindowGroup`-style Scene at `Flux/Flux/Charts/Expansion/macOS/ChartScene.swift`) does not read `IPadLayoutGate` and is not affected; smoke-verify it opens after the change.

### Toolbar layout sketch (macOS Day Detail)

```
┌────────────────────────────────────────────────────────────────────────┐
│ ◀ │ ▶ │ 🗓 sidebar │ Fri · May 24 · 2026          │       │ ◁ │ ▷ │ ⟳ │
└────────────────────────────────────────────────────────────────────────┘
  ↑   ↑       ↑                  ↑                                 ↑   ↑   ↑
  system    sidebar         nav title (pageTitle)              prev/next   ⌘R
  back     toggle                                              (this PR)   (macRefresh)
  buttons
```

Chevrons render as a single Liquid Glass bubble (paired in one `ToolbarItemGroup`); ⌘R refresh sits in its own bubble (already added via `.macRefreshAction`). No `ToolbarSpacer` needed between them — different toolbar items in different placements already separate visually.

## Testing Strategy

| AC | Test |
|---|---|
| [7.1](requirements.md#7.1) | New `IPadLayoutGateTests` (or extend existing layout test file): `#if os(macOS)` test asserts `IPadLayoutGate.isActive(hSizeClass: nil) == true`. iOS-side coverage already implicit in existing tests / preview behavior. |
| [7.2](requirements.md#7.2) | New `DayDetailMacToolbarTests` (compile-gated `#if os(macOS)`): construct a `DayDetailViewModel` with a stub API client, assert (a) `viewModel.isToday == true` → next-day disabled-state predicate matches, (b) `viewModel.isToday == false` → enabled, (c) invoking `viewModel.navigatePrevious()` / `viewModel.navigateNext()` updates `viewModel.date` as expected. **Acknowledged test gap:** SwiftUI toolbar buttons are not directly inspectable in unit tests; a regression that wires the toolbar to the *wrong* view-model method would not fail these tests. Manual click-through is the backstop. Documented here so the test author doesn't claim coverage they don't have. |
| [7.3](requirements.md#7.3) | Already satisfied by `AdaptiveColumnsLayoutTests.widthBelow700ReturnsSingleColumn` (699), `widthAt700ReturnsTwoColumns` (700), `widthBelow1000ReturnsTwoColumns` (999), `widthAt1000ReturnsThreeColumns` (1000). No new test needed; AC 7.3 is verification of existing coverage. |
| [7.4](requirements.md#7.4) | New `DayDetailNavTitleFormatterTests` (compile-gated `#if os(macOS)`): assert `macPageTitle` resolves to `"Today"` for the Sydney-local today date and to the `DayDetailEyebrow.full` formatted string for a non-today date. |
| [7.5](requirements.md#7.5) | Existing test suites run unchanged: `AdaptiveColumnsLayoutTests`, `DayDetailViewModelTests`, `DayDetailViewModelCompareTests`, `DayDetailViewModelSetDateTests`, `SidebarTabSyncTests`, `ScreenTests`. |
| [4.7](requirements.md#4.7) | Manual: with Day Detail focused on macOS, press ←/→ and confirm the date changes; press → when on today and confirm no-op. |
| [6.1](requirements.md#6.1), [6.2](requirements.md#6.2) | Manual visual verification on macOS 26 build (light + dark) at min window (960pt), mid (1200pt), and wide (1800pt). Surfaces to inspect: (a) `ScrollView` content under the toolbar on each of Dashboard / Day Detail / History; (b) the Day Detail toolbar Liquid Glass bubble containing the prev/next chevrons; (c) the disabled-state contrast of the next-day chevron on the last (today) day. Log results in a PR comment. |
| [1.4](requirements.md#1.4) | Manual: drag-resize the window through the 1000pt detail-column boundary on Dashboard and History and confirm clean 2↔3 column re-tier. `AdaptiveColumnsLayout` uses `onGeometryChange` (not `GeometryReader`), which delivers a single settled width per layout pass — flicker at the boundary is expected to be no worse than the iPad behavior shipped in T-1150. No mitigation in this PR. |
| [2.3](requirements.md#2.3) | Manual: set the largest dynamic type (`.accessibility5`) at min window width on Day Detail; confirm no horizontal clip in the 2-col `Grid`. |

PBT is not appropriate for this feature — the requirements are concrete behavioral checks on a small surface, not invariants over a domain.

## Deferred (intentionally out of scope)

Surfaced during design review; captured here to make the scope cut explicit rather than implicit.

- **Sidebar visibility persistence** (`NavigationSplitViewVisibility` via `@SceneStorage`). Mac users expect sidebar state to survive relaunch. The existing `AppNavigationView` doesn't store this. Out of scope — a separate ticket.
- **CommandMenu prev/next-day shortcuts.** `FluxKeyboardCommands` already exists (`Flux/Flux/Mac/FluxKeyboardCommands.swift`); adding ⌘← / ⌘→ menu entries would give discoverability and HIG parity. Out of scope — Decision 5 explicitly scopes the keyboard story to the existing `.onKeyPress` handlers, no menu surface.
- **Toolbar customization** (`toolbarRole`, item IDs for "Customize Toolbar…"). Out of scope — single fixed toolbar item group, no user-driven customization.
- **Multi-window state coordination.** `WindowGroup` already gives each macOS window its own view-state instance. No new coordination logic; users opening a second Flux window get an independent Day Detail date. Accepted.
- **Gate rename** (`IPadLayoutGate` → `AdaptiveLayoutGate`). Non-blocking follow-up per the requirements' non-goals.

## Risks and mitigations

- **Risk:** macOS users in the wild have set custom window sizes < 960pt via prior versions; the new minimum will snap them larger on launch. **Mitigation:** acceptable; the snap is once-per-window and the alternative is broken layouts. Document in release notes.
- **Risk:** `keyboardShortcut(.leftArrow)` on the toolbar button (if added) could conflict with existing `onKeyPress(.leftArrow)` handler. **Mitigation:** do not add `.keyboardShortcut` to the toolbar buttons; rely on the existing `.onKeyPress` handler ([AC 4.7](requirements.md#4.7)).
- **Risk:** macOS Day Detail nav title format (`"Friday, May 24, 2026"`) differs from iPad's (`"Fri · May 24 · 2026"`). **Mitigation:** intentional, captured in Decision 10 below; matches the deleted mustache's verbose-date character on macOS.
