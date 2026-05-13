# Decision Log: Zoom in on Graphs

## Decision 1: Trigger via Dedicated Expand Button, Not a Gesture

**Date**: 2026-05-12
**Status**: accepted

### Context

Five charts already carry interactive gestures: the three History cards share a synchronised drag-to-select-day overlay (`historySelectionOverlay`), and the two Day Detail charts use `chartXSelection` for point inspection. Adding a "tap to enlarge" gesture risks competing with those existing behaviours and producing ambiguous taps.

### Decision

Trigger the enlarged view from an explicit expand button overlaid on each active chart card, styled to match the existing inline card affordances. No new gestures are introduced on the chart surface itself.

### Rationale

A discrete button decouples expand from the chart's interactive area entirely, so neither gesture has to win a race or be modified. It is also more discoverable than long-press and easier to label for accessibility.

### Alternatives Considered

- **Long-press the chart**: Discoverability is poor and it would compete with the History card drag gesture.
- **Double-tap**: Less discoverable than a visible control and would race with single-tap point selection on Day Detail charts.
- **Tap the whole card**: Simplest, but breaks both existing selection modes.

### Consequences

**Positive:**
- Zero risk of breaking existing selection behaviour.
- Accessible by default via standard control semantics.
- Visible affordance signals the feature exists.

**Negative:**
- Adds visual chrome to every chart card.
- One more thing to keep visually consistent across charts.

---

## Decision 2: iOS Uses a Full-Screen Modal That Supports Landscape

**Date**: 2026-05-12
**Status**: accepted

### Context

The point of the feature is more horizontal pixels for the chart. The rest of the iOS app is portrait-only.

### Decision

On iOS, the enlarged view is a full-screen modal that supports both portrait and landscape, overriding the app-wide orientation lock for the duration of the presentation.

### Rationale

Landscape provides materially more horizontal axis for time-series charts. The Stocks, Health, and Photos apps all use this pattern, so users have a strong mental model for "rotate to inspect."

### Alternatives Considered

- **Portrait-only full-screen sheet**: Simpler to build but defeats much of the value — solar and grid charts get only a modest height increase.
- **In-place card expansion**: Animated growth in the current scroll position. Harder to make work with the surrounding layout and gives less real estate than a full-screen presentation.

### Consequences

**Positive:**
- Maximises chart legibility, especially for time-series data.
- Familiar pattern from system apps.

**Negative:**
- Requires a per-scene orientation override on iOS.
- Two layouts to test for each chart instead of one.

---

## Decision 3: Preserve Existing Interactivity, Add No Zoom or Pan

**Date**: 2026-05-12
**Status**: accepted

### Context

The user request is "bigger version," not "more interactive version." SwiftUI Charts has no built-in pinch-zoom or pan, and rolling it by hand is substantial work.

### Decision

The enlarged presentation reuses each chart's existing inline interactivity: `chartXSelection` on Day Detail charts and the day-selection drag overlay on History charts. No pinch-to-zoom or time-axis panning is added.

### Rationale

Keeps scope contained, avoids hand-rolling gesture math, and matches user expectations — the chart is the same chart, only bigger.

### Alternatives Considered

- **Pinch-to-zoom on the time axis**: Significant scope increase for a feature the user did not ask for.
- **Read-only enlarged view**: Removes the very inspection capability the user is likely zooming in to use.

### Consequences

**Positive:**
- Minimal new gesture code.
- Behaviour predictable from the inline experience.

**Negative:**
- Users who want to dive into a sub-range will still need to rely on the existing date range controls.

---

## Decision 4: macOS Uses a Separate Window per Chart, Replace if Reopened

**Date**: 2026-05-12
**Status**: accepted

### Context

On macOS the natural unit of "more space" is a window, not a modal layer. Users may want the enlarged chart visible while continuing to navigate the main window.

### Decision

On macOS, each expand action opens a separate window. Reopening the same chart while its window is still open brings that existing window to the front instead of creating a duplicate. Different charts open additional windows. Open windows are not persisted across app launches.

### Rationale

A real window is the most native and most useful presentation on macOS — it can be moved, resized, and kept visible alongside the main view. Replacing-rather-than-duplicating avoids window clutter without preventing the user from having different charts open simultaneously.

### Alternatives Considered

- **Single shared "chart inspector" window**: Less Mac-like; forces the user to dismiss one chart to look at another.
- **Always open a new window**: Easy for the user to accidentally pile up identical windows.
- **iOS-style sheet on macOS**: Loses the ability to view the chart alongside other content.

### Consequences

**Positive:**
- Idiomatic on macOS.
- Multi-chart inspection works naturally.

**Negative:**
- More scaffolding than reusing the iOS sheet path.
- Window management code is platform-specific.

---

## Decision 5: Scope Limited to the Five Currently Rendered Charts

**Date**: 2026-05-12
**Status**: accepted

### Context

Three chart views exist in code but are not currently rendered on any screen: HistoryBatteryCard, SOCChartView, BatteryPowerChartView.

### Decision

The feature applies only to the five charts actually shown to users today: HistorySolarCard, HistoryGridUsageCard, HistoryDailyUsageCard, PowerChartView, BatteryCombinedChartView.

### Rationale

Adding the affordance to unrendered views would do nothing for users and would require deciding where those charts ought to live first. That is a separate question.

### Alternatives Considered

- **Cover all eight defined chart views**: Wastes effort on UI that ships dead.

### Consequences

**Positive:**
- Smaller surface area for implementation and tests.

**Negative:**
- When/if the three dormant charts get rendered later, the expand affordance will need to be added at that point. That cost is trivial given the shared presentation infrastructure this feature will introduce.

---

## Decision 6: Window/Sheet Identity Carries No Data Payload

**Date**: 2026-05-12
**Status**: accepted

### Context

`WindowGroup(for:)` on macOS keys windows by a `Hashable & Codable` value. The naive temptation is to bundle the data the enlarged chart needs (selected date, date range) into that value so the window receives it directly. Doing so would couple window identity to state and force a new window whenever state changed.

### Decision

The window/sheet identity value is the unpopulated `ChartKind` enum. The enlarged view obtains the data it needs from app-root environment stores — the same stores the inline cards observe.

### Rationale

Requirement [3.2](requirements.md#3.2) mandates view-type-only identity. Threading data through the identity value would break [4.5](requirements.md#4.5) (live updates) on macOS, because changing the data would change the identity and SwiftUI would open a fresh window. Sharing the store via environment is the standard pattern for cross-scene data on SwiftUI and reuses what already works for the main window.

### Alternatives Considered

- **Carry a `(ChartKind, ContextPayload)` value**: Breaks identity semantics; explicitly disallowed by requirement 3.2.
- **Refetch from the network in the enlarged view**: Duplicate work; risks divergence between inline and enlarged renders during the gap between fetches.

### Consequences

**Positive:**
- Window identity and rendered state are decoupled; updates flow naturally.
- iOS sheet and macOS window use the same `ChartKind` value.

**Negative:**
- Requires that the chart-feeding stores are reachable from the app-root environment, not held in screen-local state. Some lift may be needed in `HistoryView` and `DayDetailView`. The audit table in design.md names the touched stores.

---

## Decision 7: iOS Landscape via App-Delegate Singleton + UIKit Bridge

**Date**: 2026-05-12
**Status**: accepted

### Context

SwiftUI has no per-presentation orientation override API on iOS 26. The two viable approaches are (a) a global app-delegate hook (`application(_:supportedInterfaceOrientationsFor:)`) gated by a singleton, or (b) a `UIViewControllerRepresentable` that overrides `supportedInterfaceOrientations` on its hosted controller. Both require the app delegate hook to *permit* landscape — the controller alone is not sufficient.

### Decision

Use both, layered: an `OrientationLock` singleton gates the app-delegate hook (default `.portrait`, flipped to `.allButUpsideDown` for the duration of the enlarged presentation). A `UIViewControllerRepresentable` wrapping the enlarged content drives the singleton from its lifecycle and calls `UIWindowScene.requestGeometryUpdate(.iOS(...))` on appear.

### Rationale

The singleton is required regardless because the app delegate is the only place iOS asks about supported orientations. The bridge scopes the side effect to exactly the lifetime of the presentation, so other paths cannot accidentally trigger landscape. The split is consistent with the TN3192 migration patterns Apple documents.

### Alternatives Considered

- **App-delegate only, flipped by SwiftUI `.onAppear`**: Couples the side effect to view appearance ordering and leaks easily if `.onDisappear` does not fire (e.g. interrupted dismissal).
- **Controller only**: Will not work — the app delegate gate still has the final say.
- **Pure SwiftUI**: No such API exists on iOS 26.

### Consequences

**Positive:**
- Side effect is bracketed by view controller lifetime, including unexpected dismissals.
- Falls back cleanly to portrait if `requestGeometryUpdate` is denied (per [2.7](requirements.md#2.7)).

**Negative:**
- Requires a small UIKit bridge and a global singleton. This is the smallest footprint that actually works on iOS 26.

---

## Decision 8: AC 3.5 (Cursor-Display Placement) Accepted as Soft Guarantee

**Date**: 2026-05-12
**Status**: superseded by Decision 13

### Context

Requirement [3.5](requirements.md#3.5) asks new macOS windows to open on the display containing the cursor. SwiftUI's `openWindow(value:)` exposes no API for this; the system default places the new window on the key window's screen.

### Decision

Treat AC 3.5 as a soft guarantee delivered by SwiftUI's default placement (key window's screen). When the user expands a chart while the main window is on a non-cursor display, the enlarged window may land on the main window's display rather than the cursor's. We do not introduce post-creation `NSWindow.setFrame` plumbing to chase the cursor.

### Rationale

The expand button is itself on the main window's display (the user just clicked it), so the cursor is on the main window's display in the overwhelmingly common case. The corner-case discrepancy is not worth a per-window `NSWindow` introspection layer for a personal-use app. If this turns out to matter in practice, an `NSWindowDelegate`-based placement fix is a localised follow-up.

### Alternatives Considered

- **Introspect the new `NSWindow` and call `setFrame(_:display:animate:)` to recenter on the cursor's screen**: Adds AppKit interop just for this one guarantee. Disproportionate.
- **Tighten the requirement to "key window's display"**: More honest, but cosmetic — defer until/unless the trade-off matters in real use.

### Consequences

**Positive:**
- No AppKit bridging needed for window placement.

**Negative:**
- AC 3.5 is satisfied only by coincidence in the multi-display edge case. Documented for transparency.

---

## Decision 9: Expand Button Visual Contract Defined Here, Not Inherited

**Date**: 2026-05-12
**Status**: accepted

### Context

The original AC 1.2 claimed to "match" an existing card affordance. Codebase exploration showed there is no existing top-trailing icon-button affordance on any chart card. `HistoryCardChrome` has no buttons; the closest existing affordance (the "add note" control on `DayDetailNoteSection`) is a full-width labeled button living *outside* the chart area, not a corner icon.

### Decision

Define the expand button's visual contract directly in AC 1.2: SF Symbol `arrow.up.left.and.arrow.down.right`, `.title3` size at `.medium` weight, `FluxTheme.Palette.secondaryText`, `.plain` button style, overlaid in the top-trailing corner of the chart's drawing area with an 8-point inset. All five charts use the identical control in the identical position.

### Rationale

Pretending to match a nonexistent pattern would have produced an unverifiable requirement. The honest move is to define the new control concretely and keep it strictly identical across all five charts, so the *consistency* anchor for future affordances is "the expand button itself."

### Alternatives Considered

- **Add the button to `HistoryCardChrome`'s header instead of the chart area**: Would require restructuring the header HStack and would not produce an identical placement on Day Detail charts (which use no chrome). Rejected for inconsistency.
- **Different placement per screen**: Smaller refactor risk but loses the predictability target.

### Consequences

**Positive:**
- Single, testable visual contract.
- Identical control across all charts.

**Negative:**
- Introduces a new precedent rather than reusing an existing one — but no existing one fit.

---

## Decision 10: Replace Single `ChartGestureGate` with Two Per-Strategy Gates

**Date**: 2026-05-12
**Status**: accepted

### Context

The first design draft proposed a single `ChartGestureGate` keyed on a `gestureActive` boolean toggled by a "non-nil → nil transition" of `chartXSelection`. Three independent reviewers (internal critic plus Kiro and Gemini) flagged this as unimplementable: `chartXSelection` is a value binding, not a gesture with `onBegan`/`onEnded`. A tap that leaves the selection non-nil leaves the gate permanently "active", silently violating [4.5](requirements.md#4.5).

### Decision

Replace the single gate with two strategy types behind a `ChartSelectionGate` protocol:

- `HistoryDragGate` — driven by the existing `DragGesture` in `historySelectionOverlay`. Real begin/end events. Pending snapshot is flushed on drag end.
- `XSelectionQuiescenceGate` — used by `PowerChartView` and `BatteryCombinedChartView`. Bumps a `lastSelectionChange` timestamp whenever the `chartXSelection` binding's value changes; defers data updates while the elapsed time is under a 400 ms quiet window.

### Rationale

`chartXSelection` provides no lifecycle. The only honest substitutes are (a) snapshot at present and never refresh while selected — wrong, breaks [4.5](requirements.md#4.5), or (b) treat "no change for X ms" as "user paused". 400 ms is comfortably above the gap between drag samples (~16 ms at 60 Hz) and well below the 10 s poll cadence, so refreshes still flow in normal use without nudging a live selection.

### Alternatives Considered

- **Single gate with "non-nil = active"**: Permanent lock after first tap; rejected by all reviewers.
- **Drop [4.6](requirements.md#4.6) for `chartXSelection` charts**: Mid-poll selection nudges are subtle, but the 5-minute reading spacing means a poll can move the highlighted point by a noticeable amount in low-light periods (different non-zero readings). Worth defending against.

### Consequences

**Positive:**
- The mechanism actually corresponds to the user's interaction model on both chart families.
- History case stays event-driven (no timer).

**Negative:**
- Two implementations instead of one. The shared `ChartSelectionGate` protocol keeps the call sites symmetric.

---

## Decision 11: macOS Uses Self-Contained Re-observation, Not Store Lift

**Date**: 2026-05-12
**Status**: accepted

### Context

The first design draft proposed lifting `HistoryViewModel` and `DayDetailViewModel` to the app-root environment so a separate macOS scene could read them. Code inspection showed those view models are per-screen, parameterised by the visible date/range, and tied to `refreshCoordinator`'s tier system. Lifting them creates either perpetual polling for data nobody is viewing, or requires a keyed-cache layer that does not exist today. Two reviewers (Kiro, Gemini) flagged this; design-critic flagged it as a "sneaky refactor."

### Decision

The macOS chart-detail scene does not reach into the main window's view models. Instead it owns a small scoped observer that wraps `FluxAPIClient` (already injected at app root) and polls at the inactive 60-second tier scoped to its own `appearsActive`. The scope (date or date range) is captured into a small app-root `ChartScopeRegistry` at the moment the expand action fires and read back by the scene when the window opens or its `ChartKind` value is reapplied.

### Rationale

The original lift conflated identity, lifetime, and observation. Splitting them out gives:

- Identity = `ChartKind` (per [3.2](requirements.md#3.2)).
- Lifetime = window scene's own lifetime.
- Observation = scoped to the captured `ChartScope`, isolated from the main window's polling.

Independence from main-window navigation ([5.4](requirements.md#5.4)) emerges naturally: passive nav changes update no shared state the scene observes; only an explicit re-expand updates the registry entry for the scene's kind.

### Alternatives Considered

- **Lift `DayDetailViewModel` wholesale**: Bad cost-to-value trade; introduces unbounded retention and breaks scope semantics.
- **Snapshot the data at open and never refresh**: Violates [4.5](requirements.md#4.5).
- **Make the enlarged window observe the main view models via a key**: Requires building a keyed cache the codebase does not have, and still entangles main-nav with window state.

### Consequences

**Positive:**
- No lift of existing per-screen view models.
- Window observation lifetime tracks window lifetime exactly.
- Refresh tier is appropriate to a passive viewer (60 s, not 10 s).

**Negative:**
- Two pollers exist for the same data on macOS when the main window and an enlarged window show overlapping ranges. Acceptable in a two-user app; can be deduplicated later if it ever shows up in cost or device load.

---

## Decision 12: Use a Visible Top Drag Handle, Not a Hidden Full-Area Swipe Layer

**Date**: 2026-05-12
**Status**: accepted

### Context

The original [2.3](requirements.md#2.3) required dismissal "on a downward swipe from anywhere in the presentation." `fullScreenCover` provides no system swipe-to-dismiss; the first design hand-rolled a 44-point hidden gesture strip at the top edge, which satisfied neither "from anywhere" nor coexistence with `chartXSelection`/History drag selection if the chart extended into that region. Two reviewers proposed switching to `.sheet(.large)`, but that leaves a strip of the parent visible on iPhone and fails [2.1](requirements.md#2.1)'s "covers the underlying screen."

### Decision

Keep `fullScreenCover`. Replace the hidden gesture strip with a **visible 32 pt drag-indicator pill** above the chart title. A downward `DragGesture` confined to this pill (and its surrounding tap band) dismisses past 60 pt. The chart's drawing area is wholly below the band, so there is no gesture conflict. Update [2.3](requirements.md#2.3) to require the visible indicator rather than a swipe-from-anywhere gesture.

### Rationale

`fullScreenCover` is the only iOS presentation that actually covers the underlying screen, so we keep it. The previous AC's "swipe from anywhere" was aspirational — implementing it for real requires either a `simultaneousGesture` that arbitrates against chart selection (fragile) or a `.sheet`-based presentation that doesn't fully cover. A visible drag handle is the same pattern Apple's own non-sheet modals use (e.g. picker over a map) and is unambiguous to users.

### Alternatives Considered

- **`.sheet(.presentationDetents([.large]))`**: Cleaner gesture handling, but leaves parent edge visible — fails [2.1](requirements.md#2.1).
- **Full-area `simultaneousGesture` for swipe**: Constant fight with `chartXSelection` and History drag-select; bug factory.
- **Close button only, no swipe affordance**: Less ergonomic on a one-handed phone, and inconsistent with user expectations for full-screen presentations.

### Consequences

**Positive:**
- Predictable: the gesture target is visible.
- No gesture conflict with chart interactions.

**Negative:**
- Less "fluid" than a free-swipe sheet, but the swipe-anywhere version was illusory anyway.

---

## Decision 13: AC 3.5 Renegotiated to SwiftUI Default Placement

**Date**: 2026-05-12
**Status**: accepted; supersedes Decision 8

### Context

The original [3.5](requirements.md#3.5) mandated that new macOS chart windows open on the display containing the cursor. SwiftUI has no public API for this; the system default opens on the key window's display. Decision 8 captured the gap as a "soft guarantee" but left the AC reading as `SHALL`.

### Decision

Renegotiate [3.5](requirements.md#3.5) to specify SwiftUI's default placement (key-window display) and note that a future iteration may tighten this if it proves to matter. Drop Decision 8.

### Rationale

A `SHALL` we cannot deliver without AppKit interop is not a requirement, it is wishful. Pinning the AC to what SwiftUI actually does removes the gap and keeps the door open for a follow-up.

### Alternatives Considered

- **Keep AC 3.5 strict and add `NSWindow.setFrame` plumbing**: Disproportionate for a personal-use app where the cursor is virtually always on the same display as the expand button at activation time.
- **Drop AC 3.5 entirely**: Loses the documented intent.

### Consequences

**Positive:**
- AC is now verifiable by inspection of SwiftUI's documented behaviour.

**Negative:**
- Multi-display edge case is unimproved.

---

## Decision 14: `WindowGroup` Auto-Restore Disabled via `.defaultLaunchBehavior(.suppressed)`

**Date**: 2026-05-12
**Status**: accepted

### Context

`WindowGroup(for:)` persists window values for SwiftUI state restoration and rehydrates them on launch. The first design assumed nothing would mount at launch "because nothing is mounted at launch unless the user reopens" — which is incorrect for `WindowGroup(for:)`. Both Kiro and Gemini flagged this; it would silently violate [3.7](requirements.md#3.7) (no persistence across launches).

### Decision

Apply `.defaultLaunchBehavior(.suppressed)` to the chart-detail `WindowGroup`. The window only appears in response to an explicit `openWindow` call.

### Rationale

`.defaultLaunchBehavior(.suppressed)` is the documented way to keep a `WindowGroup` out of the auto-restored set, without disabling Codable conformance on the value type or removing state restoration globally.

### Alternatives Considered

- **Drop `Codable` from `ChartKind`**: Doesn't work — `WindowGroup(for:)` requires it.
- **Manually clear the persisted state on app launch**: Brittle and inconsistent.

### Consequences

**Positive:**
- AC [3.7](requirements.md#3.7) is delivered by a single line in the scene declaration.

**Negative:**
- None observed.

---

## Decision 15: iOS Sheet State Preserved via `@SceneStorage`

**Date**: 2026-05-12
**Status**: accepted

### Context

[5.5](requirements.md#5.5) requires the enlarged presentation to survive an app backgrounding. With `@State` alone, iOS may discard the view hierarchy if memory pressure tears down the host scene; the cover would not return on foregrounding.

### Decision

Store the `expanded: ChartKind?` state in `@SceneStorage("expandedChart")` on `RootView`. SwiftUI serialises the value into scene state and restores it on scene reconnect.

### Rationale

`@SceneStorage` is the standard SwiftUI mechanism for surviving scene-level state churn. `ChartKind` is `Codable`, so it slots in cleanly.

### Alternatives Considered

- **Default `@State`**: Loses state under memory pressure.
- **Custom UserDefaults persistence**: Reinventing `@SceneStorage` poorly.

### Consequences

**Positive:**
- One-line delivery of [5.5](requirements.md#5.5).

**Negative:**
- The enlarged presentation also re-mounts after a full app cold-launch if the scene is restored — but this is iOS's normal scene restoration, which we accept and is consistent with how the rest of the app behaves.

---

## Decision 16: `.windowManagerRole(.associated)` Replaces `CommandGroup` Hack

**Date**: 2026-05-12
**Status**: accepted

### Context

The first design suggested `CommandGroup(replacing: .windowList) { }` to suppress "auto window-menu duplication." That command actually hides the entire Window menu list, including the main window. Peer review flagged it.

### Decision

Use `.windowManagerRole(.associated)` on the chart-detail `WindowGroup`. Chart windows appear in the Window menu (so users can find open enlarged windows) without elevating any of them to the app's main role.

### Rationale

The intent was always "don't accidentally promote chart windows over the main one." `.windowManagerRole(.associated)` says exactly that. The `CommandGroup` hack was a misdiagnosis of the symptom.

### Alternatives Considered

- **Keep `CommandGroup(replacing: .windowList) { }`**: Hides the entire Window menu list — user-hostile.
- **Do nothing**: Default role works but is less precise about intent; explicit role declaration documents the choice in code.

### Consequences

**Positive:**
- Window menu remains useful.
- Intent is explicit at the scene declaration.

**Negative:**
- None.

---

## Decision 17: Keep `secondaryText` for the Expand Button Glyph — WCAG-AA Verified

**Date**: 2026-05-12
**Status**: accepted

### Context

[Decision 9](#decision-9-expand-button-visual-contract-defined-here-not-inherited) sets the button glyph foreground to `FluxTheme.Palette.secondaryText` and notes that a WCAG-AA contrast check against the lightest plot fill in light mode is a verification task — and to fall back to `FluxTheme.Palette.primaryText` if the check fails. This decision records the result of that check.

In light mode, `secondaryText` resolves to `Color.black.opacity(0.6)`. The plot fills the glyph could overlap with — across all five active charts — are:

- `Color.green.opacity(0.25)` over the near-white card background (`PowerChartView` solar area — the lightest fill in scope)
- System green at full opacity (`HistorySolarCard` daily bars)
- `FluxTheme.Palette.amber` rgb(255, 179, 71) at full opacity (used elsewhere in the app, not in these chart fills today, but reachable if the solar visual is unified later)

### Decision

Keep `FluxTheme.Palette.secondaryText` as the expand button glyph foreground. No fallback to `primaryText` is needed.

### Rationale

WCAG-AA requires a 3:1 contrast ratio for non-text UI elements (including icon buttons). Computed contrast ratios for the 60 %-opaque-black glyph composited over each candidate fill (light mode, card background `~rgb(244, 244, 244)`):

- `green.opacity(0.25)` over card: **5.25 : 1** ✓
- System green at full opacity: **4.31 : 1** ✓
- `Palette.amber` at full opacity: **4.71 : 1** ✓

All exceed the 3:1 floor for graphical objects, and the worst case (4.31 : 1) also meets the 4.5 : 1 normal-text bar. The glyph stays legible everywhere it can land.

### Alternatives Considered

- **Switch to `primaryText` (full black in light mode)**: Would push contrast to ~7 : 1 but visually competes with the chart's primary axis labels and selection callouts, both rendered in `primaryText`. Rejected because the glyph is intentionally secondary chrome, not a primary action.
- **Add an opaque background plate behind the glyph**: Would solve any future contrast risk, but adds visual chrome that [Decision 9](#decision-9-expand-button-visual-contract-defined-here-not-inherited) deliberately avoided. Rejected as unwarranted.

### Consequences

**Positive:**
- Visual contract from Decision 9 stands unchanged; no rework required.
- Legibility is documented and reproducible from the colour values, not from a screenshot.

**Negative:**
- If a future chart introduces a fill brighter than the values audited here (e.g. white or unscaled amber over the card background), the check will need to be redone for that fill.

---

## Decision 18: iOS Cover Also Uses Self-Contained Observation, Not Parent Bindings

**Date**: 2026-05-13
**Status**: accepted; extends Decision 11

### Context

Design.md described the iOS data path as "parent bindings — the sheet lives inside the parent screen and inherits the same state already passed to the inline card." That assumed the `fullScreenCover` would be attached to `HistoryView` and `DayDetailView`, with each parent threading its existing entries/readings/summary state into the enlarged view.

The implementation that landed instead places the cover on `RootView` (above `AppNavigationView`). Pre-push review found that the cover content was `ExpandedChartView(kind: kind)` with no host controllers, falling through to the `ExpandedChartMissingDataView` placeholder — the iOS path of the feature rendered "Chart data unavailable" instead of the chart. The root cause is the position of the cover: at `RootView` it cannot reach `HistoryView` / `DayDetailView` state, but moving the cover into the parents would require giving each parent its own `@SceneStorage("expanded\(kind)")` and lose the simple single-key restoration model from Decision 15.

### Decision

iOS uses the same self-contained scope-and-observer pattern as macOS (Decision 11). `RootView` keeps the cover and the `@SceneStorage("expandedChart")` binding; the cover mounts a cross-platform `ChartExpansionContent` view that owns a `ChartSceneObserver` and renders `ExpandedChartView` with the resolved host controllers. The original "parent bindings" sketch in design.md is superseded.

### Rationale

Self-observation is what macOS already does; matching iOS to that pattern keeps the data plumbing symmetrical and removes the cross-platform asymmetry that made the iOS path break silently. The alternative — lifting the cover into each parent screen — would have duplicated the scene-storage key per screen and forced threading bindings through multiple intermediate views (HistorySolarCard / HistoryGridUsageCard / HistoryDailyUsageCard, plus the two Day Detail charts) just so the cover could subscribe to the same data the inline card already has. The scoped observer pays one extra HTTP round-trip per open cover; in a two-user app that is dramatically cheaper than the structural complexity.

### Alternatives Considered

- **Move the cover into HistoryView/DayDetailView and pass bindings**: True to the original design sketch, but requires per-screen `@SceneStorage` keys, threads bindings through several intermediate views, and produces a structurally different iOS path from macOS. Rejected as disproportionate.
- **Inject the active view-model into a shared environment value the cover can read**: Same retention/lifetime problems Decision 11 already rejected for macOS.
- **Leave the iOS path rendering the missing-data placeholder**: Not a real alternative; the cover would be useless.

### Consequences

**Positive:**
- iOS and macOS data paths are identical, so the cross-platform `ChartExpansionContent` view contains the entire mount logic in one place.
- The cover survives parent unmount (tab switch) because it is anchored to the root, matching Decision 15's scene-storage assumption.
- Adding the affordance to a future chart only requires registering the kind; no per-screen plumbing.

**Negative:**
- Two pollers exist for the same data while the inline card and the enlarged cover are both visible (one for `HistoryViewModel`/`DayDetailViewModel`, one for the observer). Acceptable on iOS for the same reason it is on macOS (Decision 11).
- This decision means [AC 4.4](requirements.md#4.4) cannot be satisfied automatically through shared bindings; see Decision 19 for how that AC is resolved.

---

## Decision 19: AC 4.4 (Selection Mirror-Back) Not Implemented Either Platform

**Date**: 2026-05-13
**Status**: accepted; resolves an open spec divergence

### Context

[AC 4.4](requirements.md#4.4) requires that when the user changes the selected point or selected day inside the enlarged presentation, the system reflects that selection in the underlying inline card on close. Both platforms now use self-contained observation (Decision 11 for macOS; Decision 18 for iOS), which means the enlarged presentation's selection state is local to its own scope and is not threaded back into the inline view models.

Implementing the mirror would require either lifting the inline cards' selection state into a shared bidirectional registry, or wiring a write-on-dismiss callback chain from the enlarged presentation into the inline view model — both of which contradict the lifetime independence Decisions 11 and 18 chose.

### Decision

Treat the enlarged presentation's selection as ephemeral: it is local to the cover/window's lifetime, not persisted to the inline card on close. [AC 4.4](requirements.md#4.4) is documented as not delivered; closing the enlarged presentation returns the inline card to its pre-existing selection (which may be `nil`).

### Rationale

The original AC was written before the data plumbing was settled. Decisions 11 and 18 deliberately decoupled the enlarged presentation from inline view-model state to keep window/cover lifetime independent of main-window navigation. Reintroducing a write-back path would require maintaining a per-kind "last selection" cell that both the inline view and the enlarged view observe — feature-level complexity disproportionate to the user benefit (a personal-use app where the user can simply tap the same point again in the inline view).

If usage shows the mirror-back materially matters, a follow-up can add a `lastSelection: [ChartKind: Date?]` field to `ChartScopeRegistry` and have the inline cards adopt it on appear. That change is localised and reversible.

### Alternatives Considered

- **Add a bidirectional selection registry**: Same cost as Decision 11's rejected store-lift. Premature for personal-app scale.
- **Pass dismissal callback that pushes the last selection into the inline view model**: Cross-cuts otherwise-clean lifetime boundaries; complicates testing.
- **Leave AC 4.4 silently unimplemented**: Misleading; the spec implies it works.

### Consequences

**Positive:**
- The decoupling from Decisions 11 + 18 is preserved.
- No new shared state.

**Negative:**
- Users who change the selection in the enlarged view and then dismiss will see the inline card at its previous selection. Documented as a known gap.
- AC 4.4 is honest about not being delivered.

---

## Decision 20: Quiescence Gate Flushes via Scheduled Task, Not 100 ms Poll

**Date**: 2026-05-13
**Status**: accepted

### Context

The first implementation of `ExpandedDayHost` ran a `while !Task.isCancelled` loop that slept 100 ms and called `controller.tick()` for the lifetime of the enlarged view. That wakes the main actor ten times per second for the entire window's lifetime even when no selection is pending and no work needs doing. The pre-push efficiency review flagged this.

### Decision

`ExpandedDayHostController.noteSelectionChange(to:)` schedules a single `Task.sleep(quietWindow)` flush whenever a non-nil selection arrives, cancelling and rescheduling on each subsequent change. Clearing the selection cancels the task and flushes pending immediately via `gate.noteSelectionCleared()`. The `tick()` method remains exposed so existing unit tests can drive the gate deterministically with their mock clock.

### Rationale

The original 100 ms poll was a workaround for the fact that `chartXSelection` has no end-of-gesture event — but it solved that problem by polling regardless of whether the gate had any pending work. Event-driven scheduling exploits the fact that the gate's quiescence window is itself event-driven (it only matters in the 400 ms after the most recent `noteSelectionChange`).

### Alternatives Considered

- **Keep the 100 ms poll**: Wakeful and wasteful; flagged by efficiency review.
- **Drive the flush from the chart binding's `didSet`**: SwiftUI's `@Binding` has no `didSet`; would require routing through an intermediary state-object.
- **Use a `Timer` instead of `Task.sleep`**: More platform-coupled; no test affordance.

### Consequences

**Positive:**
- Idle enlarged Day Detail views perform zero main-thread work between user interactions.
- Existing controller-level unit tests (which call `tick()` directly with a mock clock) are unaffected.

**Negative:**
- One extra `Task` allocation per selection change, cancelled on the next change. Negligible.

---

## Decision 21: Bar-Tap Day Navigation Not Wired in Enlarged History View

**Date**: 2026-05-13
**Status**: accepted

### Context

Inside the inline history cards, tapping a bar invokes `onSelect(dayID)`, which `HistoryView.selectDay` routes into the navigation stack to push Day Detail. The enlarged History view reuses the same card bodies (`HistorySolarCard` / `HistoryGridUsageCard` / `HistoryDailyUsageCard`) via `ExpandedHistoryHost`, so the affordance is visible to the user — but the enlarged view currently passes `onSelectHistoryDay: nil` from `ChartExpansionContent` into `ExpandedChartView`, making bar-tap a no-op.

The PR review (iteration 2) flagged this: bar-tap navigation is a different concern from AC 4.4 selection-mirror-back (Decision 19), and the silent `nil` is potentially confusing for future readers.

### Decision

Document the deferral with an inline code comment at the `nil` site in `ChartExpansionContent` and treat enlarged-view bar-tap navigation as out of scope for T-1215.

### Rationale

Wiring bar-tap navigation would require routing a "dismiss the enlarged presentation, then push Day Detail in the main navigation stack" callback through the enlarged view. On iOS that means lifting `dismiss()` + the `NavigationStack(path:)` binding up to `ChartExpansionContent`; on macOS it means orchestrating a window-close plus a separate main-window navigation push. Both reintroduce exactly the lifetime coupling Decisions 11 (macOS scope-based observer) and 18 (iOS self-contained presentation) chose to avoid.

The user impact is small: dismissing the enlarged view returns the user to the inline card with its bar still tappable. The personal-use scope makes this a reasonable trade-off versus the architectural cost.

### Alternatives Considered

- **Wire dismissal + navigation through the enlarged view**: Reintroduces the cross-layer coupling rejected in Decisions 11 / 18; significant additional surface.
- **Disable the bar-tap affordance inside the enlarged view**: Would require either a separate "non-navigable" card variant or a no-op closure with a visual cue. The latter is what we ship today (the user can't see the difference until they tap); the former adds two new card permutations.
- **Leave the `nil` undocumented**: What iteration 1 of this PR did; the reviewer correctly flagged the ambiguity.

### Consequences

**Positive:**
- Decisions 11 + 18 lifetime independence is preserved.
- No new shared state and no new cross-layer wiring.

**Negative:**
- Bar-tap inside the enlarged History view does nothing. Documented as a known gap; future work can revisit if usage data shows it matters.

---
