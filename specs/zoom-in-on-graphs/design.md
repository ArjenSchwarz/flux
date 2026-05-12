# Design: Zoom in on Graphs

## Overview

Introduce a single expand-and-present pipeline shared by all five chart cards. iOS uses a `fullScreenCover` with a UIKit bridge that requests landscape on appear and reverses on dismiss. macOS uses a `WindowGroup(for: ChartKind)` with launch restoration suppressed; the new scene runs its own data observation rather than reaching into per-screen view models.

## Architecture

### Module layout

```
Flux/Flux/Charts/Expansion/
  ChartKind.swift                   — Hashable+Codable enum, window/sheet identity
  ChartExpansionAction.swift        — Environment value, platform-specific trigger
  ExpandableChartContainer.swift    — view modifier, overlays the expand button
  ExpandedChartView.swift           — root view shown in sheet/window
  ExpandedHistoryHost.swift         — enlarged History chart hosts (3 kinds)
  ExpandedDayHost.swift             — enlarged Day Detail chart hosts (2 kinds)
  ChartSelectionGate.swift          — refresh-deferral helper, per-chart-kind strategy
  iOS/OrientationBridge.swift       — UIKit bridge (compiled iOS-only)
  macOS/ChartScene.swift            — chart-detail WindowGroup (compiled macOS-only)
```

### Flow

```
Chart card
  └ ExpandableChartContainer(kind:) — button overlay, top-trailing 8pt inset
      └ Environment(\.chartExpansion).expand(kind)
            │
            ├─ iOS:  RootView holds  @SceneStorage("expandedChart") expanded: ChartKind?
            │       .fullScreenCover(item: $expanded) { ExpandedChartView(kind:) }
            │
            └─ macOS: openWindow(id: "chart-detail", value: kind)
                    WindowGroup(...) { $kind in ExpandedChartView(kind:) }
                    .defaultLaunchBehavior(.suppressed)
                    .windowManagerRole(.associated)
```

The button is overlaid by `ExpandableChartContainer` directly on each chart's `Chart { ... }` drawing area (not on `HistoryCardChrome` / status header), so the placement is identical across all five cards.

### Identity vs data — clean split

| Concern | Type | Notes |
|---|---|---|
| Window identity (macOS) | `ChartKind` (enum, no payload) | View type only, per [3.2](requirements.md#3.2). Reopening same kind brings existing window forward; SwiftUI guarantees this for `WindowGroup(for:)`. |
| Sheet identity (iOS) | `ChartKind?` state on the host | One optional binding for `fullScreenCover(item:)`. Persisted via `@SceneStorage` to satisfy [5.5](requirements.md#5.5). |
| Data on iOS | Parent bindings | The sheet lives inside the parent screen and inherits the same state already passed to the inline card. No store lift. |
| Data on macOS | Self-contained re-observation | The enlarged window scene observes the FluxCore API client (already injected at `App` root) and runs its own polling for the captured context. |

### macOS data: self-observation, not store lift

The earlier draft proposed lifting `HistoryViewModel` / `DayDetailViewModel` to the app root. That misrepresented the cost: those view models are per-screen, parameterised by the screen's current date/range, and tied to `refreshCoordinator`'s tier. Lifting them either creates app-lifetime polling for data nobody is looking at, or requires a keyed-cache layer that does not exist today.

This design takes the opposite path: the enlarged macOS scene owns a small, scoped observer. On open it captures `ChartScope` (a sibling of `ChartKind`) recorded by the expand action immediately before `openWindow` is called.

```swift
enum ChartScope: Hashable, Codable {
    case historyRange(DateRange)
    case daySpecific(date: Date)
}

@Observable final class ChartScopeRegistry {
    var current: [ChartKind: ChartScope] = [:]
}
```

`ChartScopeRegistry` is a single `@Observable` injected at the app root and reachable from any scene. When the user activates the expand button on a chart, `ChartExpansionAction.expand(kind)` writes the appropriate scope into the registry *before* invoking the platform trigger. The new macOS window reads its scope from the registry by `ChartKind` and instantiates its own scoped data observer (a thin wrapper that wraps `FluxAPIClient` and polls at the inactive 60-second tier — see [Refresh cadence](#refresh-cadence) below).

The registry is small (≤5 entries), in-memory only, and does not affect window identity. Re-expanding the same chart kind from a different main-window context updates the registry entry and brings the window forward; the window's observer adopts the new scope. This satisfies [3.2](requirements.md#3.2), [4.5](requirements.md#4.5), and [5.4](requirements.md#5.4) together:

- **3.2** Window identity is `ChartKind` alone.
- **4.5** Live updates flow because the scoped observer is polling.
- **5.4** The enlarged window is independent of *passive* main-window navigation. Only an explicit re-expand changes the window's scope.

#### Refresh cadence

The enlarged window does not subscribe to the main `refreshCoordinator`'s 10-second active tier; it polls its scoped endpoint at 60 seconds. Rationale: the enlarged window has its own visibility lifecycle (it can be on a different display, the user may be focused on the main window). Reusing the existing `RefreshTier` machinery scoped to the window's `appearsActive` state is the implementation hook.

### Selection state for Day Detail

Today `PowerChartView` and `BatteryCombinedChartView` each own `@State private var selectedDate: Date?`. The inline-and-enlarged-mirror requirement ([4.4](requirements.md#4.4)) needs that state shared with the enlarged view. The change:

1. Add `@Binding var selectedDate: Date?` to both views' init.
2. `DayDetailView` owns the state as `@State private var powerSelected: Date?` and `@State private var batterySelected: Date?` and passes bindings down.
3. Previews and any test that constructs the chart views directly pass a `.constant(nil)` binding.

Call-site fan-out (verified):
- `DayDetailPanels.power(...)` and `DayDetailPanels.battery(...)` factories — accept the binding and forward.
- `DayDetailView.body` — owns the state, forwards into the panel factories.
- Their `#Preview` declarations — pass `.constant(nil)`.
- No other production call sites; no existing test files construct them directly.

This is a real but contained refactor. It is listed as a separate task.

### Pattern Extension Audit

| Touchpoint | Used by feature | Action |
|---|---|---|
| `HistoryCardChrome` | Wraps 3 History cards | No change; `ExpandableChartContainer` lives inside the chrome's content closure, not in the chrome itself |
| `historySelectionOverlay` | History cards' day-select drag | Reused unchanged in `ExpandedHistoryHost` |
| `chartXSelection` (PowerChartView, BatteryCombinedChartView) | Point selection | Selection state lifted to `DayDetailView` (see above); behaviour otherwise unchanged |
| `DayChartDomain` | Off-peak shaded band, 24h domain | Reused unchanged in `ExpandedDayHost` |
| `refreshCoordinator` | App-wide 10s/60s polling tiers | Not connected to the enlarged window; the scoped observer is independent |
| `FluxAPIClient` | History range + day endpoints | Injected once at app root; used directly by the scoped observer |
| `FluxTheme.Palette.secondaryText` | The expand button's foreground | Used elsewhere at caption sizes; the button is `.title3` `.medium` in this colour. A WCAG-AA contrast check against the lightest plot fill (solar yellow in light mode) is a verification task — if insufficient, drop to `FluxTheme.Palette.primaryText` |
| `RootView`'s `@SceneStorage` | The persisted `expanded` state | One new key; no collision check needed because no other scene storage exists today |
| `HistoryStatsOverviewCard` and other non-chart History cards | Not in scope | No expand affordance per [non-goals]; one-line note in code review |

## Components and Interfaces

```swift
// ChartKind.swift — cross-platform
enum ChartKind: String, Hashable, Codable, CaseIterable {
    case historySolar, historyGridUsage, historyDailyUsage
    case dayPower, dayBatteryCombined
}

// ChartExpansionAction.swift — cross-platform
struct ChartExpansionAction {
    let expand: (ChartKind, ChartScope) -> Void
}
extension EnvironmentValues {
    @Entry var chartExpansion: ChartExpansionAction = .init { _, _ in }
}

// ExpandableChartContainer.swift — cross-platform
struct ExpandableChartContainer<Content: View>: View {
    let kind: ChartKind
    let scopeProvider: () -> ChartScope   // captures current parent state at tap time
    @ViewBuilder var content: () -> Content
}

// ExpandedChartView.swift — cross-platform
struct ExpandedChartView: View {
    let kind: ChartKind
    @Environment(\.chartScopeRegistry) private var registry
    // Resolves to ExpandedHistoryHost or ExpandedDayHost based on kind.
    // Reads scope from registry; on macOS, constructs a scoped observer.
}
```

### `ChartSelectionGate` — per-strategy, not one-size

The earlier draft proposed a single gate keyed on a fabricated "gestureActive" boolean. That is not implementable for `chartXSelection`, which is a value binding with no begin/end. Replaced with two concrete strategies:

```swift
protocol ChartSelectionGate: AnyObject {
    func snapshotIsStale() -> Bool
    func adopt(_ snapshot: ChartSnapshot)
}

// History charts — driven by a real DragGesture
final class HistoryDragGate: ChartSelectionGate {
    private(set) var dragging = false       // set by DragGesture.onChanged/onEnded
    private var pending: ChartSnapshot?
    func snapshotIsStale() -> Bool { dragging }
    func adopt(_ s: ChartSnapshot) {
        if dragging { pending = s } else { /* apply immediately */ }
    }
    // when drag ends → flush pending
}

// Day Detail charts — chartXSelection has no lifecycle, so use quiescence
final class XSelectionQuiescenceGate: ChartSelectionGate {
    private var lastSelectionChange: Date = .distantPast
    private let quietWindow: TimeInterval = 0.4
    private var pending: ChartSnapshot?
    // selection binding's didSet bumps lastSelectionChange
    // a timer (or .task with sleep) flushes pending after `quietWindow` of silence
    func snapshotIsStale() -> Bool {
        Date().timeIntervalSince(lastSelectionChange) < quietWindow
    }
}
```

The History gate is fully event-driven (no timer); the Day Detail gate uses a 400 ms quiescence window after the most recent `chartXSelection` change. A tap-and-release that leaves selection non-nil therefore unblocks updates 400 ms after the user stops interacting — not "indefinitely". This satisfies [4.6](requirements.md#4.6) without misusing `chartXSelection`'s binding semantics.

### iOS presentation

```swift
struct RootView: View {
    @SceneStorage("expandedChart") private var expanded: ChartKind?
    var body: some View {
        AppNavigationView()
            .environment(\.chartExpansion, .init { kind, scope in
                ChartScopeRegistry.shared.current[kind] = scope
                expanded = kind
            })
            .fullScreenCover(item: $expanded) { kind in
                OrientationLandscapeScope { ExpandedChartView(kind: kind) }
            }
    }
}
```

#### `OrientationLandscapeScope`

A `UIViewControllerRepresentable` wrapping a small `UIViewController`. The hosting flow:

1. `viewWillAppear(_:)` — **first** sets `OrientationLock.shared.mask = .allButUpsideDown`. **Then** calls `parent?.setNeedsUpdateOfSupportedInterfaceOrientations()` to make the host scene re-ask. **Then** calls `view.window?.windowScene?.requestGeometryUpdate(.iOS(interfaceOrientations: .landscape)) { error in os_log(.info, "geometry denied: \(error)") }`.
2. `viewWillDisappear(_:)` — sets `OrientationLock.shared.mask = .portrait`, then `parent?.setNeedsUpdateOfSupportedInterfaceOrientations()`. No geometry request is needed on the way back; the system requests an orientation that complies with the updated supported set.
3. The host scene's `application(_:supportedInterfaceOrientationsFor:)` returns `OrientationLock.shared.mask`. Default `.portrait`.

Belt-and-braces: the SwiftUI subtree also wires `.onDisappear` on `ExpandedChartView` to reset the lock, in case SwiftUI tears down the controller without `viewWillDisappear` (this has been observed with parent unmounts during tab switches). `OrientationLock` uses a reference-counted enter/exit to make double-reset safe.

If `requestGeometryUpdate`'s error handler fires, the chart renders in portrait. Per [2.7](requirements.md#2.7).

#### Top drag handle for dismissal

Per the updated [2.3](requirements.md#2.3), the enlarged presentation displays a visible drag-indicator pill above the chart title. The pill is in a 32 pt tall safe-area band; a downward `DragGesture(minimumDistance: 8)` on that band calls `dismiss()` past a 60 pt translation. The chart's drawing area starts below this band, so the gesture cannot conflict with `chartXSelection` or History drag-select.

This replaces the earlier proposal of a hidden full-width swipe layer.

### macOS presentation

```swift
// FluxApp.body
WindowGroup("Chart", id: "chart-detail", for: ChartKind.self) { $kind in
    if let kind {
        ExpandedChartView(kind: kind)
            .frame(minWidth: 720, minHeight: 480)
    }
}
.defaultSize(width: 900, height: 600)
.windowResizability(.contentMinSize)
.defaultLaunchBehavior(.suppressed)   // satisfies AC 3.7
.windowManagerRole(.associated)       // chart windows in Window menu, but don't claim main role
```

`.defaultLaunchBehavior(.suppressed)` prevents SwiftUI from auto-restoring chart-detail windows on launch, which is the default behaviour of `WindowGroup(for:)` and would silently violate [3.7](requirements.md#3.7). Verified in WWDC 2024 *What's new in SwiftUI* and Apple's `Scene` reference.

`.windowManagerRole(.associated)` keeps the chart windows visible in the Window menu (so users can find open enlarged windows) without elevating them to main-window status. The earlier `CommandGroup(replacing: .windowList) { }` would have hidden the entire app's Window menu list and is removed.

On macOS the `ChartExpansionAction` resolves to:

```swift
.environment(\.chartExpansion, .init { kind, scope in
    ChartScopeRegistry.shared.current[kind] = scope
    openWindow(id: "chart-detail", value: kind)
})
```

SwiftUI's `openWindow(value:)` semantics guarantee that an equal value brings the existing window forward. Different kinds open additional windows. No `NSApp.activate` is needed.

Display placement is left to SwiftUI's default. Per [3.5](requirements.md#3.5) (renegotiated) this is acceptable; no `NSWindow.setFrame` plumbing is added.

## Data Models

No new persisted models. `ChartKind` and `ChartScope` are value types used only for in-memory routing.

## Error Handling

Two new failure modes:

1. `requestGeometryUpdate` denied (iOS). The error handler logs at `os_log` `.info`; the chart renders portrait. Covered by [2.7](requirements.md#2.7).
2. Scope registry missing an entry when `ExpandedChartView` loads (macOS edge case if the user reopens a restored window — but `.defaultLaunchBehavior(.suppressed)` makes this impossible). Defensive: the view falls back to a reasonable scope (today's date for `dayPower`/`dayBatteryCombined`; the History view's current default range for the three History kinds). Logged at `.info`.

## Testing Strategy

### Unit
- `ChartKind` Codable/Hashable round-trip.
- `HistoryDragGate`: drag-start → update arrives → snapshot pending → drag-end → snapshot applied once.
- `XSelectionQuiescenceGate`: selection change at t=0 → update at t=100 ms → snapshot pending → no further change → snapshot applied at ~t=400 ms.
- `XSelectionQuiescenceGate`: selection clears → next update applies immediately.
- `ChartScopeRegistry`: write-then-read by kind; overwrite same kind; different kinds independent.

### Integration / view-level
- Test harness: plain XCTest with `_UIHostingController` (iOS) and `NSHostingController` (macOS). No third-party view inspection dependency.
- For each of the five cards, mount the inline card with a stub `ChartExpansionAction`, simulate the button tap (via `Button.action()` lookup on the hosted view), verify the action was invoked with the matching `ChartKind`.
- iOS: open the cover, verify `OrientationLock.shared.mask == .allButUpsideDown` while presented; dismiss, verify reset to `.portrait`.
- iOS: simulate parent unmount (cover host view's `onDisappear`), verify orientation lock resets even without a viewWillDisappear path.
- macOS: open the same `ChartKind` twice in a row, assert the `WindowGroup` value list contains exactly one entry.
- macOS: open two different kinds, assert two entries.

Snapshot testing is intentionally **not** part of this plan. The repo has no snapshot harness today and adding `swift-snapshot-testing` is outside this feature's scope.

### Manual / accessibility
- VoiceOver: expand button on each card has label "Expand chart"; activation opens the enlarged view; on dismiss, focus returns to the same control.
- Keyboard (macOS): Tab reaches the expand button; Space activates; ⌘W dismisses the resulting window.
- Reduce Motion: enabled — presentation cross-fades.
- Dynamic Type at largest accessibility size: title, axis labels, callouts all scale.
- Contrast spot-check: enlarge the History solar chart in light mode; verify the expand-button glyph remains legible over the lightest data point. If not, switch the foreground colour to `primaryText`.

### Manual scenarios
- Refresh-mid-drag (History): begin drag-select on enlarged History solar; wait through 10s poll; release; verify chart adopts refreshed data and highlighted day did not jump mid-drag.
- Refresh-mid-selection (Day Detail): tap a point on enlarged PowerChartView; wait through 60s poll; verify selected point does not jump within 400 ms of the tap; if untouched, chart adopts refreshed data after the quiescence window.
- iOS background/foreground: open enlarged chart, background app for 5 minutes, foreground — same chart still presented at the same selection (`@SceneStorage` preservation).
- macOS launch: open enlarged chart, quit app, relaunch — no enlarged window restored.
- macOS re-expand: with enlarged DayPower open showing day X, change main window to day Y, click expand on the main window's DayPower — the existing window's scope updates to Y and the window is brought forward (one window per kind).
