# Implementation: Zoom in on Graphs

This document explains the Zoom in on Graphs feature at three expertise levels and assesses how the shipped code on branch `T-1215/zoom-in-on-graphs` compares to the requirements. It is written from the current state of the code, not from the original design intent — divergences are called out explicitly and cross-referenced to decision-log entries.

---

## Beginner Level

### What This Does

Flux is a battery-monitoring app. Each screen has small charts that are nice for an at-a-glance look but too small for detailed reading. This feature adds a small "expand" button (a diagonal-arrow icon) to the top-right corner of every active chart. Tapping it shows the chart in a much larger view:

- On **iPhone**, the chart fills the whole screen and the phone is allowed to rotate to landscape so the chart gets even more horizontal space.
- On **Mac**, the chart opens in its own window that you can move and resize alongside the main app.

The same chart interactions you already have inline still work in the enlarged view — tapping/dragging to pick a time of day or a specific day.

### Why It Matters

Five-minute solar samples over a week, or per-second battery readings over a day, contain detail that simply doesn't fit on a card-sized chart. Without leaving the inline charts cluttered, this gives users a "click for full-screen" option.

### Key Concepts

- **Chart kind**: a label for which of the five charts is in play (e.g. "history solar", "day power"). Used everywhere as the identity of the enlarged presentation.
- **Scope**: the data slice the chart is showing (e.g. "last 7 days" or "Tuesday 12 May"). Captured when the user taps Expand.
- **Sheet / window identity**: iOS uses one full-screen modal at a time; macOS opens one window per chart kind. Re-opening the same kind brings the existing window forward instead of stacking duplicates.
- **Selection deferral**: while the user is dragging on the chart, an auto-refresh that arrives mid-drag is held until the user lets go, so the highlighted data point doesn't jump out from under them.

---

## Intermediate Level

### Module Layout

New SwiftUI module at `Flux/Flux/Charts/Expansion/`:

| File | Role |
|---|---|
| `ChartKind.swift` | `String`-backed `Hashable & Codable & CaseIterable & Identifiable` enum (5 cases). Drives window/sheet identity. Also `ChartScope` enum (`historyRange(days:)` / `daySpecific(date:)`) and `HostKind` mapping. |
| `ChartExpansionAction.swift` | `EnvironmentValues.chartExpansion` entry — a closure `(ChartKind, ChartScope) -> Void` that platform code installs to handle taps. |
| `ChartScopeRegistry.swift` | `@MainActor @Observable` `[ChartKind: ChartScope]`. App-root singleton, written on each expand tap, read by the enlarged presentation. |
| `ChartExpansionFocusCoordinator.swift` | Accessibility focus return. Tokenised so re-expanding the same kind produces a fresh edge SwiftUI's `.onChange` will fire on. |
| `ExpandableChartContainer.swift` | View modifier that overlays the expand button (SF Symbol `arrow.up.left.and.arrow.down.right`, `.title3.medium`, 8 pt inset, `secondaryText` foreground, `.plain` style, "Expand chart" a11y label). Reads `\.chartExpansion` and calls it on tap. |
| `ExpandedChartView.swift` | Router. Switches on `ChartKind.hostKind` and mounts either `ExpandedHistoryHost` or `ExpandedDayHost`. Provides `resolvedScope(for:in:)` registry lookup with fallbacks. Cross-fade gated on `\.accessibilityReduceMotion`. |
| `ExpandedHistoryHost.swift` | Wraps the existing `HistorySolarCard` / `HistoryGridUsageCard` / `HistoryDailyUsageCard` bodies. Owns an `ExpandedHistoryHostController` (`@Observable`) carrying a `HistoryDragGate` — `simultaneousGesture(DragGesture)` drives `beginDrag/endDrag`. |
| `ExpandedDayHost.swift` | Mounts `PowerChartView` or `BatteryCombinedChartView`. Owns an `ExpandedDayHostController` carrying an `XSelectionQuiescenceGate` with a 400 ms quiet window. `noteSelectionChange(to:)` schedules a one-shot `Task.sleep(0.4)` flush per selection edit (cancelled and rescheduled on each change). |
| `ChartSelectionGate.swift` | `protocol ChartSelectionGate<Snapshot>` plus two concrete implementations: `HistoryDragGate` (real gesture lifecycle) and `XSelectionQuiescenceGate` (timestamp-bumped quiet window, no polling). |
| `ChartExpansionContent.swift` | **Cross-platform mount point.** Owns the `ChartSceneObserver`, threads its host controllers into `ExpandedChartView`, polls `tick()` every 60 s. Used on both iOS (inside the cover) and macOS (inside the window). |
| `ExpandedChartObserverFactory.swift` | Shared `makeAPIClient(keychainService:)` factory — same wiring as `AppNavigationView`. |
| `iOS/OrientationLock.swift` | `@MainActor` singleton with ref-counted `enter/exit`. `FluxiOSAppDelegate.application(_:supportedInterfaceOrientationsFor:)` returns the current mask. |
| `iOS/OrientationLandscapeScope.swift` | `UIViewControllerRepresentable`. `viewWillAppear` sets the lock to `.allButUpsideDown`, calls `setNeedsUpdateOfSupportedInterfaceOrientations`, then `windowScene.requestGeometryUpdate(.iOS(.landscape))`. `viewWillDisappear` reverses. SwiftUI `.onDisappear` provides belt-and-braces reset for tab-switch tear-downs. Geometry denials log at `.info` and the cover renders portrait. |
| `iOS/ExpandedChartTopHandle.swift` | Visible 32 pt drag-pill above the title; downward `DragGesture(minimumDistance: 8)` past 60 pt dismisses. Used inside `RootView`'s cover. |
| `macOS/ChartScene.swift` | `ChartDetailScene: Scene` — `WindowGroup("Chart", id: "chart-detail", for: ChartKind.self)` with `.defaultSize(900, 600)`, `.windowResizability(.contentMinSize)`, min 720 × 480, `.defaultLaunchBehavior(.suppressed)` (no auto-restore on launch), `.windowManagerRole(.associated)`. `macOSChartExpansion(registry:focus:)` modifier installs the platform `ChartExpansionAction` that writes the scope and calls `openWindow(id:value:)`. |
| `macOS/ChartSceneObserver.swift` | Now cross-platform despite the path. `@MainActor @Observable` per-presentation observer. Polls `FluxAPIClient` at the 60 s tier when `appearsActive`, immediate refetch on reactivation or scope change, leaves previous snapshot intact on failure. |

Plus the iOS-only `RootView.swift` (replaces the bare `AppNavigationView()` in `FluxApp`'s iOS branch) and small refactors to the inline charts.

### Platform Flow

**iOS**: `RootView` holds `@SceneStorage("expandedChart")` as a raw String, wraps `AppNavigationView()` with `.environment(\.chartExpansion, ...)`, and presents a `.fullScreenCover(item:)` containing `ChartExpansionCover` — itself an `OrientationLandscapeScope { VStack { ExpandedChartTopBar; ChartExpansionContent } }`. The top bar carries the visible drag pill and a leading-aligned Close button (xmark). Tapping Close, dragging the pill past 60 pt, or losing the presenter all set `expanded.wrappedValue = nil`, which dismisses the cover. The cover's `.transaction { disablesAnimations }` is gated on `\.accessibilityReduceMotion` for cross-fade behaviour.

**macOS**: `ChartDetailContent` (the macOS scene's body) and the iOS cover both mount `ChartExpansionContent(kind:)`. That view reads scope from `ChartScopeRegistry`, builds a `FluxAPIClient` via `ExpandedChartObserverFactory.makeAPIClient`, constructs a `ChartSceneObserver`, and renders `ExpandedChartView` with the observer's `historyController` / `dayController`. The observer is driven by a `.task(id: kind)` that sleeps 60 s between `tick()` calls; an `.onChange(of: appearsActive)` flips the observer's active flag and triggers an immediate fetch on reactivation. macOS gets the standard `⌘W` / red-traffic-light close from `WindowGroup`; iOS gets Close + drag-pill from the top bar.

### Implementation Approach

- **Identity is `ChartKind` only.** Scope is in a separate registry. This means `WindowGroup(for: ChartKind.self)` brings the same-kind window forward instead of opening a new one (AC 3.3); changing the scope updates the registry entry and the observer adopts it without a new window (Decision 6).
- **Self-contained observation on both platforms.** Originally the iOS path was supposed to use parent bindings (cover inside `HistoryView` / `DayDetailView`); the cover landed at `RootView` and pre-push review found it was rendering the missing-data placeholder. Decision 18 captures the resolution: iOS now matches macOS — the cover owns its own observer at 60 s. This loses automatic selection mirror-back (Decision 19), which is documented as deferred.
- **Two refresh-deferral strategies behind one protocol.** `HistoryDragGate` is event-driven (real `DragGesture` lifecycle, no timer); `XSelectionQuiescenceGate` uses a 400 ms quiet window because `chartXSelection` has no begin/end (Decision 10). The Day Detail controller schedules a single `Task.sleep` per selection edit to flush after the quiet window, instead of the original 100 ms tight poll (Decision 20).
- **Accessibility focus return.** `ChartExpansionFocusCoordinator` tokenises requests so a fresh `(kind, token)` is published every time, satisfying `.onChange` and tolerating stale `consume` callbacks. iOS triggers from the cover binding clearing; macOS triggers from window `.onDisappear`.
- **No persistence of enlarged windows across launches.** macOS uses `.defaultLaunchBehavior(.suppressed)` to opt out of `WindowGroup(for:)`'s default restoration (Decision 14). iOS uses `@SceneStorage` so the cover survives memory pressure but is restored only as part of normal scene restoration (Decision 15).

### Trade-offs

- **Two pollers exist while inline + enlarged are both visible.** The enlarged presentation runs its own 60 s observer instead of subscribing to the inline view model. The cost is one extra HTTP fetch per minute per open enlarged view; the win is full lifetime independence (Decisions 11 and 18).
- **AC 4.4 selection mirror-back is deferred.** With self-contained observation, the enlarged view's selection isn't pushed back to the inline view on close. Documented in Decision 19 as a known gap with an easy follow-up path if it matters in practice.
- **AC 3.5 cursor-display placement is honoured by coincidence.** SwiftUI's `openWindow` places the new window on the key-window's display (Decision 13).

---

## Expert Level

### Architecture Impact

The feature deliberately splits **identity** (window/sheet) from **lifetime** (scene) from **data observation** (scope-driven observer). On macOS this is enforced by SwiftUI itself: `WindowGroup(for: ChartKind.self)` keys windows by `ChartKind` and SwiftUI guarantees the bring-forward semantics for value reapply. The novel piece is `ChartScopeRegistry`: a tiny `[ChartKind: ChartScope]` `@Observable` that lives at the app root. The expand action writes scope before invoking `openWindow(id:value:)` so the new window or already-open window reads the latest scope by kind. iOS uses the same registry with a different platform trigger (`@SceneStorage("expandedChart")` ChartKind sheet binding).

This split is what made Decision 11 viable: the enlarged presentation does not lift any per-screen view model, does not subscribe to the main `refreshCoordinator`, and does not key off the inline card's `selectedDate`. It is its own scope-bounded subscriber, retained by SwiftUI for as long as the window/cover is alive.

Pre-push review found that the iOS path of the original implementation never made it to that subscriber: `RootView`'s cover constructed `ExpandedChartView(kind: kind)` with no host controllers, which routed through to `ExpandedChartMissingDataView` ("Chart data unavailable"). The fix on this branch extracts the macOS observer-mount logic into a cross-platform `ChartExpansionContent` and reuses it inside the iOS cover. This also retires the per-platform duplication of `makeAPIClient` (now `ExpandedChartObserverFactory`) and `parseReadings` (now `ParsedReading.parse`).

### Concurrency Model

- All public surfaces are `@MainActor`. The observer's `tick()` and `refresh()` are async on the main actor; the network call is awaited but data adoption happens back on main when control returns.
- The `ChartSceneObserver`'s `appearsActive` flips drive `pendingFetch = true` via `didSet`; reactivation produces an immediate fetch on the next `tick()`.
- `ExpandedDayHostController.scheduleFlush` creates a `Task { @MainActor }` that sleeps for the quiet window and then calls `gate.tick()`. The Task captures `[weak self]` so a deinit while the flush is pending cancels safely. Cancellation on every new `noteSelectionChange` means at most one flush task is alive per controller; the test suite drives `tick()` directly with the gate's injected clock, so the real-time `Task.sleep` doesn't gate the unit tests.
- `HistoryDragGate` is timer-free: the user's `DragGesture` provides real `.onChanged` / `.onEnded` callbacks, and `endDrag` flushes any single pending snapshot.

### Edge Cases & Failure Modes

- **Scene restoration on cold launch (iOS).** `@SceneStorage("expandedChart")` may rehydrate `expandedRaw` to a `ChartKind` value the user never explicitly re-opened. Accepted as standard SwiftUI scene-restoration behaviour (Decision 15). The cover mounts, `ChartExpansionContent` builds a fresh observer, and the scope falls back via `resolvedScope` because the registry is empty on cold launch.
- **macOS launch-time auto-restore.** `WindowGroup(for: ChartKind.self)` would normally rehydrate the last-open windows; `.defaultLaunchBehavior(.suppressed)` disables that, satisfying AC 3.7 in a single line (Decision 14).
- **Tab-switch tear-down without `viewWillDisappear` (iOS).** Observed historically — SwiftUI may unmount a UIKit-bridged controller without firing UIKit lifecycle. `OrientationLandscapeScope` adds a SwiftUI `.onDisappear` belt-and-braces path that resets `OrientationLock` and re-requests portrait geometry. `OrientationLock` is ref-counted, so a redundant `exit()` is a no-op.
- **Geometry denial (iOS).** If `requestGeometryUpdate(.iOS(.landscape))` fails, the error handler logs at `.info` and the cover renders portrait (AC 2.7). The cover content itself doesn't depend on landscape — the chart just gets less horizontal space.
- **API client unavailable.** If the user opens the cover before configuring URL/token, `ExpandedChartObserverFactory.makeAPIClient` returns `nil` and `ChartExpansionContent`'s observer stays `nil`, which causes `ExpandedChartView` to render `ExpandedChartMissingDataView`. Acceptable: an unconfigured app already shows the settings screen at the main level.
- **Refresh arriving mid-gesture.** Both gates hold the new snapshot until the gesture/quiet window ends. The `XSelectionQuiescenceGate`'s `noteSelectionCleared` flushes pending immediately so the chart never goes stale once selection is dismissed.

### Things to Monitor

- **Idle main-actor wake-ups.** The cross-platform observer's `task(id: kind)` sleeps 60 s between ticks; the day-controller flush task is event-driven. Watch the per-frame profile if a future change reintroduces a tighter loop. The original 100 ms quiescence poll was caught here.
- **Logger output.** `OrientationLandscapeScope` uses subsystem `eu.arjen.flux` now (aligned with the rest of the app); `Logger.expansion` is the per-file category. A previous typo (`me.nore.ig.Flux` — the bundle ID) was a copy-paste artefact.
- **Scope drift on re-expand.** `ChartDetailContent` (macOS) and `ChartExpansionContent` both observe `registry.current[kind]` via `.onChange`, so re-expanding from a different main-window context updates the existing window/cover's scope and the observer re-fetches. If you ever route through `ChartScopeRegistry` from anywhere else, that observation will fire.

---

## Completeness Assessment

Verified against requirements §§1.1–6.4 and the code on this branch (`T-1215/zoom-in-on-graphs`).

### Fully implemented

- **1.1, 1.2, 1.3, 1.4, 1.5** — All five active charts wrap their `Chart` body with `ExpandableChartContainer`. Visual contract (symbol, size, weight, colour, inset, button style, a11y label) is enforced in `ExpandableChartContainer.swift`. WCAG-AA contrast verified in Decision 17.
- **2.1** — iOS uses `fullScreenCover(item:)` on `RootView`. Cover renders `ChartExpansionContent` with controllers; was previously broken, now fixed on this branch (Decision 18).
- **2.2, 2.6, 2.7** — `OrientationLock` + `OrientationLandscapeScope` + `FluxiOSAppDelegate` deliver the landscape mask while presented and reset on dismiss (including the tab-switch belt-and-braces). Geometry denial logs and renders portrait.
- **2.3** — Visible 32 pt drag-pill (`ExpandedChartTopHandle`) wired into the cover top bar with a 60 pt dismissal threshold. The pill sits above the chart drawing area so there is no gesture conflict.
- **2.4, 2.5** — The cover (and macOS window) mount `ExpandedChartView`, which dispatches to the existing inline cards. The cards already render their title and contextual header, and SwiftUI's container chrome lets the chart fill the rest.
- **3.1, 3.2, 3.3, 3.4, 3.6, 3.7, 3.8** — `ChartDetailScene` delivers all of these. Identity = `ChartKind`. `defaultLaunchBehavior(.suppressed)` prevents launch restoration. Min size 720 × 480. `windowManagerRole(.associated)` keeps chart windows out of the main role.
- **3.5** — SwiftUI default placement (Decision 13).
- **4.1, 4.2, 4.3** — Selection (point on Day Detail, drag-to-pick-day on History) is preserved by reusing the inline card bodies inside the enlarged hosts. No zoom/pan added.
- **4.5** — Live updates flow because each enlarged presentation has its own observer polling the right scope on a 60 s cadence (immediate fetch on reactivation).
- **4.6** — Refresh deferral mid-drag (History) and mid-selection (Day Detail) handled by `HistoryDragGate` and `XSelectionQuiescenceGate`. Flush task is event-driven on the day side now (Decision 20).
- **5.1** — iOS cover dismisses via Close button (xmark) and the visible drag pill past 60 pt.
- **5.2** — macOS dismisses via standard window close and `⌘W`.
- **5.4** — macOS window is independent of main-window navigation; only an explicit re-expand updates its scope.
- **5.5** — `@SceneStorage("expandedChart")` preserves the cover across app backgrounding.
- **6.2** — `ChartExpansionFocusCoordinator` + `@AccessibilityFocusState` returns VoiceOver / keyboard focus to the originating expand button on dismiss.
- **6.3** — Dynamic Type carried through because the enlarged hosts reuse the inline card bodies, which already use `appFont(.textStyle:)`.
- **6.4** — Reduce Motion respected: `ExpandedChartView` cross-fades when reduce-motion is on; `RootView.fullScreenCover`'s transaction disables the system slide so the cross-fade isn't compounded.

### Partial / divergent (documented)

- **5.3 — Tab-switch teardown on iOS.** `OrientationLock` resets correctly on tab-switch (covered by `iOSExpandIntegrationTests.tabSwitchTeardownResetsLockWithoutViewWillDisappear`). The cover state in `@SceneStorage` is *not* explicitly cleared on tab switch — if the user switches tabs while the cover is up, returning to the tab will re-present the cover at the same kind. The spec says the presentation "SHALL dismiss"; the current behaviour is "the cover survives because the storage survives." A follow-up could observe the tab binding and clear `expandedRaw` on tab change.
- **6.1 — Performance signpost.** No `os_signpost` instrumentation was added. The implementation avoids synchronous fetch and main-thread blocking work between activation and animation start, but the verification path the AC names is absent.

### Intentionally deferred

- **4.4 — Selection mirrors back to the inline card on close.** Decision 19. With self-contained observation on both platforms (Decisions 11 + 18), the enlarged presentation's selection is not threaded back into the inline view model. Adding a `lastSelection: [ChartKind: Date?]` shared cell would suffice as a follow-up; in the current implementation the inline card returns to whatever selection it held before expand.

### Not implemented

- None outside the partial / deferred items above.

### Test Coverage Map

| Requirement | Test file |
|---|---|
| 1.1, 1.2, 1.3, 1.4, 1.5 | `ExpandableChartContainerTests`, `HistoryCardExpansionTests`, `DayChartExpansionTests` |
| 2.2, 2.6 | `OrientationLockTests`, `iOSExpandIntegrationTests` |
| 2.3 | `ExpandedChartTopHandleTests` (unit-level; not asserted as mounted inside the cover) |
| 3.2, 3.3, 3.4, 3.7 | `ChartSceneIntegrationTests`, `MacOSScopedObserverTests` |
| 4.5, 4.6 | `HistoryDragGateTests`, `XSelectionQuiescenceGateTests`, `ExpandedHistoryHostTests`, `ExpandedDayHostTests`, `MacOSScopedObserverTests` |
| 5.3 | partial — `iOSExpandIntegrationTests.tabSwitchTeardownResetsLockWithoutViewWillDisappear` |
| 6.2 | `ExpansionAccessibilityTests` |

Gaps: integration-level iOS test that the cover actually mounts the chart (now possible with `ChartExpansionContent`); end-to-end macOS test that re-expand updates the existing window's observer scope (covered at the registry layer by `ChartSceneIntegrationTests`).

---

## Validation Findings

### Gaps identified

- AC 6.1 calls for `os_signpost` verification of activation-to-animation latency. Not instrumented. Low-cost follow-up.
- AC 5.3 (iOS tab-switch dismissal of the cover content) is not enforced beyond orientation-lock reset; the cover's `@SceneStorage`-backed kind is not cleared on tab change.
- No integration test asserts that the iOS cover renders an actual chart (the pre-push miss). The fix is verifiable manually; a hosting-controller smoke test would lock it in.

### Logic notes

- Decisions 18 and 19 collectively replace the "parent bindings" data sketch from `design.md` with self-contained observation on both platforms. The design document still describes the original parent-bindings approach; readers should treat decisions 11 / 18 / 19 as authoritative.

### Recommendations

1. Add a hosting-controller integration test that mounts the iOS cover for each `ChartKind` and asserts that `ExpandedChartMissingDataView` is *not* present.
2. Add `os_signpost` intervals around activation and dismissal so AC 6.1 has a verifiable artefact.
3. If selection mirror-back becomes a real ergonomic complaint, implement Decision 19's deferred follow-up by extending `ChartScopeRegistry` with a `lastSelection: [ChartKind: Date?]` cell and reading it in the inline cards on appear.
