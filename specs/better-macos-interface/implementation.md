# Implementation: Better macOS Interface (T-1342)

A three-level walkthrough of the changes that ship in branch `T-1342/better-macos-interface` (6 commits ahead of `origin/main`). Read the level that matches your context; each section is self-contained.

---

## Beginner Level

### What Changed

The Mac version of Flux used to look like a stretched-out iPhone — a single tall column of panels — even when the window was wide. iPad already had a smarter layout that splits Dashboard and History panels into two or three columns based on the window width (shipped in T-1150). This change turns that same iPad-style layout on for the Mac.

It also tidies two awkward bits of chrome:

- **Dashboard** used to show its own "Battery" title and a "Now · HH:MM · MMM d" line inside the content, even though the window already had a "Dashboard" title at the top. That duplicate is gone on Mac.
- **Day Detail** used to draw a date with arrows on either side (the team calls this the "mustache") inside the page. On Mac that now lives where it belongs: the date is the window title, and the arrows are buttons in the window toolbar.

Finally, the Mac window now refuses to shrink below 960 × 600 pixels — small enough to feel comfortable, big enough that the layout never falls back to one column.

### Why It Matters

Mac apps that look like blown-up phone apps feel cheap. By reusing the iPad layout and moving date controls into native Mac chrome, Flux now feels like a real Mac app — wider windows show more information, and the toolbar carries navigation the way `Mail` or `Calendar` does.

### Key Concepts

- **Layout gate**: a single function (`IPadLayoutGate.isActive`) that views ask "should I show the wide multi-column body?". The whole feature mostly comes down to making that function answer "yes" on Mac.
- **Adaptive columns**: a helper (`AdaptiveColumnsLayout`) that picks 1, 2, or 3 columns depending on width. Below 700pt is 1 column, 700–999pt is 2, and 1000pt+ is 3.
- **Toolbar**: the row of buttons at the top of a Mac window. SwiftUI lets you put buttons there with a `.toolbar { ... }` modifier instead of drawing them inside your content.
- **Window resizability**: a Mac-specific rule that pins the window's minimum size to the content's minimum size, so the user can't drag the window down to the point where the layout breaks.

---

## Intermediate Level

### Changes Overview

Seven files changed across two folders:

- `Flux/Flux/Helpers/IPadLayoutGate.swift` — the gate now returns `true` on `os(macOS)`.
- `Flux/Flux/Dashboard/DashboardView.swift` — adds `.navigationTitle("Dashboard")` inside `#if os(macOS)`.
- `Flux/Flux/DayDetail/DayDetailView.swift` — wraps `DayNavigationHeader` in `#if !os(macOS)`, adds `macPageTitle` computed property, adds `.navigationTitle(macPageTitle)` + `.toolbar { DayDetailMacToolbar(viewModel: viewModel) }` inside `#if os(macOS)`.
- `Flux/Flux/DayDetail/DayDetailViewSupport.swift` — new `DayDetailMacToolbar: ToolbarContent` struct.
- `Flux/Flux/FluxApp.swift` — `.frame(minWidth: 960, minHeight: 600)` on the content, `.defaultSize(1200, 800)` and `.windowResizability(.contentSize)` on the scene.
- `Flux/FluxTests/IPadLayoutGateTests.swift`, `DayDetailMacToolbarTests.swift`, `DayDetailNavTitleFormatterTests.swift` — three new compile-gated `#if os(macOS)` test files.

### Implementation Approach

The architectural lever is the "gate-flip propagation" pattern. `DashboardView`, `DayDetailView`, and `HistoryView` each already had two body branches — a compact one (`dashboardContent` / `dayDetailContent` / `historyContent`) and a regular one (`*ContentRegular`). They all selected between them via `usesRegularLayout`, which calls `IPadLayoutGate.isActive`. The design document audited every call site of the gate (5 in total — 3 view body sites + `AppNavigationView.usesPadShell` and `IPadFormWidthCap.shouldApply`) and confirmed only the three view sites actually compile on macOS; the other two are wrapped in `#if !os(macOS)` and don't observe the change. So a single one-line edit to `IPadLayoutGate` ripples through three views without any per-view conditional logic.

A few sites need a bit more than the gate flip, because the existing modifiers were already wrapped in `#if os(iOS)`:

- `DashboardView`'s existing `.navigationTitle(usesRegularLayout ? "Dashboard" : "")` is iOS-only, so on macOS the title was empty. A new `#if os(macOS) .navigationTitle("Dashboard") #endif` block sits beside it.
- `DayDetailView`'s existing `.navigationTitle(usesRegularLayout ? pageTitle : "")` is also iOS-only, so the macOS block is additive (one title per compile target — no last-one-wins ambiguity).

The Day Detail toolbar is the most interesting piece. Rather than draw chevrons inline, the code adds a `ToolbarContent` struct (`DayDetailMacToolbar`) with a `ToolbarItemGroup(placement: .primaryAction)` containing two `Button`s — `chevron.left` calling `viewModel.navigatePrevious()` and `chevron.right` calling `viewModel.navigateNext()` with `.disabled(viewModel.isToday)`. The struct lives in `DayDetailViewSupport.swift`; the view body just says `.toolbar { DayDetailMacToolbar(viewModel: viewModel) }`. (More on why it isn't inline: see Decision 11.)

Window sizing has a Mac-specific gotcha. `.frame(minWidth:minHeight:)` constrains the SwiftUI content view but does *not* clamp the `NSWindow` itself — the user can drag the window smaller, and the content gets clipped. `.windowResizability(.contentSize)` is the modifier that ties the window's resize limits to the content's frame. Both are required; one without the other fails AC 2.2.

### Trade-offs

- **Shell**: `AppNavigationView` (Mac's existing `NavigationSplitView` shell) is kept instead of promoting iPad's `FluxiPadRoot` to be shared (Decision 2). Smaller blast radius, no need to re-validate Mac keyboard commands or scene-phase handling.
- **Breakpoints**: 700pt and 1000pt are reused unchanged (Decision 3). No 4th column tier for ultra-wide Macs — no current product need.
- **Day Detail layout**: keeps iPad's fixed 2-column `Grid`, not the adaptive reflow (Decision 4). Wide Mac windows grow the cards, not the column count.
- **Window minimum**: 960pt was chosen because the sidebar (~240pt) + divider/padding (~20pt) + the 700pt 2-column threshold = ~960pt (Decision 9). A direct consequence is that the 1-column tier in `AdaptiveColumnsLayout` is unreachable at runtime on macOS — the unit test exercises it, but the user can never resize down to it. This was a deliberate trade vs. building a Mac-specific 1-column fallback.
- **Toolbar extraction**: tasks.md said the toolbar would be inline; it ended up as a separate `ToolbarContent` struct because the inline form pushed `DayDetailView.swift` past SwiftLint's 400-line cap (Decision 11, added during implementation).
- **No calendar popover**: prev/next chevrons only this iteration; jumping to an arbitrary date is deferred (Decision 5).
- **No replacement freshness indicator**: removing the "Now · HH:MM · MMM d" eyebrow doesn't leave a gap — the menu bar clock and the live-updating panels themselves convey freshness (Decision 8).
- **Two formatters**: macOS Day Detail title uses `DayDetailEyebrow.full` ("Friday, May 24, 2026"), iPad still uses `DayDetailEyebrow.formatter` ("Fri · May 24 · 2026"). Cross-platform consistency was traded for matching the deleted mustache's verbose-date character on Mac (Decision 10).

---

## Expert Level

### Technical Deep Dive

**SwiftUI reactivity inside `ToolbarContent`.** `DayDetailMacToolbar` is a value type conforming to `ToolbarContent` (not `View`), holding a `let viewModel: DayDetailViewModel`. The view model is an `Observable` class. Reading `viewModel.isToday` inside `body` registers a dependency in SwiftUI's Observation tracking system. When `setDate(_:)` mutates `date` (and therefore the computed `isToday`), the parent `DayDetailView` re-evaluates `body`, which in turn re-creates the toolbar content. The disabled state is therefore a function of the latest `isToday` value on every body pass, with no manual `@Bindable`/`@ObservedObject` wiring. The same logic applies to `macPageTitle` — it's a plain computed property on `DayDetailView`, but because it reads `viewModel.date` and `viewModel.isToday`, every mutation flows through. The midnight rollover handled by `DayDetailViewModel.setDate(_:)` (AC 4.3) rebinds the title automatically.

**`.windowResizability(.contentSize)` semantics.** This scene modifier (macOS 13+) flips the `NSWindow` resize policy from "free-form" to "match content view min/ideal/max". Under the hood, AppKit consults the content view's `intrinsicContentSize` and Auto Layout constraints; SwiftUI's `.frame(minWidth:minHeight:)` translates to a content min, so the window inherits a 960×600 floor. Without `.contentSize`, the window can be dragged below the content's minimum and SwiftUI clips — the content view doesn't grow the window the way `NSWindow.contentMinSize` would on AppKit. Two alternatives were considered and rejected (Decision 9): `.windowResizability(.contentMinSize)` (mac-13 only, less flexible on resize-larger), and setting `NSApp.windows.first?.contentMinSize` from `applicationDidFinishLaunching` (imperative AppKit reach-around in an otherwise pure SwiftUI scene).

**Per-route minimums considered, rejected.** A "relax to 800pt outside Day Detail, clamp to 960pt only on Day Detail" scheme was discussed (Decision 9 alternatives). The visible window jump on `NavigationStack` push/pop, plus fragile interaction with `@SceneStorage` for `NavigationSplitViewVisibility`, killed it. A single scene-wide minimum is one line and matches Mail / Notes / Calendar.

**Deliberate 1-column tier unreachable at runtime.** `AdaptiveColumnsLayout` still has a `< 700pt` branch on every platform. On macOS that branch is dead at runtime — the scene minimum guarantees the detail column (window width minus the ~240pt sidebar and ~20pt chrome) is ≥ ~700pt. The unit test (`AdaptiveColumnsLayoutTests.widthBelow700ReturnsSingleColumn`, asserting at 699pt) is the only path that touches it on macOS. This is documented as an explicit consequence in the gate's doc comment, AC 1.3, AC 2.2, and Decision 9. The reasoning: cross-platform unit-test coverage of the same code is acceptable because the same source compiles on both targets; the alternative (a macOS-specific 1-column Day Detail fallback) doubles the layout surface for an edge case that requires the user to override the scene minimum.

**Toolbar `ToolbarContent` extraction.** The decision to make `DayDetailMacToolbar` its own type instead of an inline closure (Decision 11) is semantics-preserving — SwiftUI's view-graph identity for `ToolbarItemGroup(placement: .primaryAction)` is stable regardless of where the items are declared, and dependency tracking through `viewModel.isToday` works transparently inside any `ToolbarContent.body`. The extraction was driven by the SwiftLint `file_length` cap (400 lines); inline would have pushed `DayDetailView.swift` to ~440 lines. The file still carries a `// swiftlint:disable file_length` directive (paired with `// swiftlint:enable file_length` at EOF per project convention) because it's at 416 lines after the extraction — but the toolbar's chevron code itself isn't in `DayDetailView.swift`. A subtle benefit is the dedicated doc comment on `DayDetailMacToolbar` explaining the AC 4.6 disabled-state mirror, which would be invisible in the inline form.

**`#if os(iOS)` vs `#if !os(macOS)` choice.** The wrap around `DayNavigationHeader` in `dayDetailContentRegular` is `#if !os(macOS)`, not `#if os(iOS)`. This matters because the codebase also targets iPadOS as part of `os(iOS)`, and the wrap should retain iPhone + iPad and exclude only Mac. The choice is defensive: if Apple ever ships another platform target, the default is "keep the in-content header" rather than "drop it everywhere unfamiliar".

**No `.keyboardShortcut` on toolbar buttons.** The existing `.focusable()` + `.onKeyPress(.leftArrow)` / `.onKeyPress(.rightArrow)` handlers on the `ScrollView` already cover ←/→ navigation. Adding `.keyboardShortcut(.leftArrow)` to the toolbar buttons would double-fire on each key press (AppKit dispatches keyEquivalent before SwiftUI consumes it via `onKeyPress`). The toolbar buttons therefore carry only `.accessibilityLabel` and `.help` (the macOS hover tooltip per HIG). When the next-day button is `.disabled(viewModel.isToday)`, AppKit may still focus the disabled button on Tab; the `.onKeyPress` handler on the parent `ScrollView` continues to receive the event because focus on a disabled button is a no-op rather than a focus-grab.

**Acknowledged test gap.** SwiftUI's `ToolbarContent` is not introspectable from a unit test — there's no public API to materialize a toolbar's button hierarchy and assert on it. `DayDetailMacToolbarTests` covers the predicate (`viewModel.isToday`) and the navigation methods the buttons call, but a regression that wires `chevron.left` to `navigateNext()` would not fail the suite. The design document calls this out explicitly under Testing Strategy / AC 7.2; manual click-through is the backstop. This is the right trade given the alternative is no automated coverage at all.

### Architecture Impact

- The "gate-flip propagation" pattern is now load-bearing. Future macOS UI work that wants to *opt out* of the iPad regular layout cannot rely on the gate — it needs a separate platform check at the call site. The gate's name (`IPadLayoutGate`) is now mildly misleading on macOS; a non-blocking follow-up rename to `AdaptiveLayoutGate` is captured in non-goals.
- Two formatters in active use for Day Detail nav titles (`pageTitle` iPad vs `macPageTitle` macOS). A future "unify all nav titles" request will need to pick one (Decision 10).
- `AppNavigationView` and `FluxiPadRoot` continue to coexist (Decision 2). The cost is two shells; the benefit is no re-validation of macOS-specific behavior (keyboard commands, refresh action, scene phase). If iPad-specific chrome evolves, macOS may need explicit catch-up changes.
- `DayDetailMacToolbar` joins `DayDetailErrorPanel` / `DayDetailMessagePanel` / `DayNavigationHeader` in `DayDetailViewSupport.swift`. The file's convention is "Day Detail subviews/toolbars not in the main view file"; future macOS-specific toolbar additions belong here.
- The scene-wide `minWidth: 960` is a hard floor for the whole app. Any future macOS screen (e.g., a Mac-only Settings pane) inherits the floor whether it needs it or not. Settings currently has its own `.frame(minWidth: 480, minHeight: 360)` in a separate scene — fine because it's a separate `Settings` scene.

### Decision Rationale (sourced)

| # | Decision | Source |
|---|---|---|
| 1 | Use full spec workflow | decision_log.md Decision 1 |
| 2 | Keep `AppNavigationView` instead of promoting `FluxiPadRoot` | decision_log.md Decision 2 |
| 3 | Reuse 700pt/1000pt iPad breakpoints, no 4th tier | decision_log.md Decision 3 |
| 4 | Day Detail uses iPad's 2-column `Grid` | decision_log.md Decision 4 |
| 5 | Replace mustache with toolbar chevrons + nav title | decision_log.md Decision 5 |
| 6 | Drop `legacyHeader` on macOS Dashboard | decision_log.md Decision 6 (user-confirmed after design-critic flagged interpretation) |
| 7 | Date in nav title, chevrons in trailing toolbar group | decision_log.md Decision 7 |
| 8 | No replacement freshness indicator | decision_log.md Decision 8 |
| 9 | Scene `minWidth: 960` derived from sidebar + 700pt threshold | decision_log.md Decision 9 |
| 10 | `DayDetailEyebrow.full` for macOS title, not `formatter` | decision_log.md Decision 10 (user choice for verbose-date character) |
| 11 | Extract `DayDetailMacToolbar` to support file | decision_log.md Decision 11 + commit c853330 (added during implementation when file hit the 400-line cap) |
| - | `#if !os(macOS)` instead of `#if os(iOS)` wrap | inferred — defensive against future platforms; not stated in spec or commits |
| - | `internal` visibility on `macPageTitle` | code comment in `DayDetailView.swift:367` ("so unit tests can read it directly") |
| - | No `.keyboardShortcut` on toolbar buttons | design.md Components / Risks; tasks.md task 4 |
| - | Comment fix in `DayDetailView.header` else branch | commit c725682 ("Fix stale macOS comment") |

---

## Important Changes

Five hunks a reviewer should look at:

1. **The gate flip** — `Flux/Flux/Helpers/IPadLayoutGate.swift:15-21`. The whole multi-column ripple flows from these three added lines (`#elseif os(macOS) return true`). Why it matters: changes the runtime behavior of three views (`DashboardView`, `DayDetailView`, `HistoryView`) without touching their bodies. Takeaway: verify the per-call-site audit in `design.md` against the codebase (`rg "IPadLayoutGate.isActive" Flux/`) — should still show exactly 5 sites. Rationale: design.md per-call-site table.

2. **Day Detail macOS toolbar wiring** — `Flux/Flux/DayDetail/DayDetailView.swift:81-87`. Adds `.navigationTitle(macPageTitle)` + `.toolbar { DayDetailMacToolbar(viewModel: viewModel) }` inside `#if os(macOS)`. Why it matters: this is the chrome replacement. Confirm one `.navigationTitle` per compile target (no last-one-wins). Takeaway: the existing `.navigationTitle(usesRegularLayout ? pageTitle : "")` is iOS-only, so this block is additive. Rationale: design.md Components / `DayDetailView`.

3. **`DayNavigationHeader` wrap** — `Flux/Flux/DayDetail/DayDetailView.swift:165-170`. One-line behavioral change inside `dayDetailContentRegular`: `#if !os(macOS) DayNavigationHeader(viewModel: viewModel) #endif`. Why it matters: this is what actually removes the mustache on Mac while preserving it on iPad. Takeaway: the wrap is `!os(macOS)`, not `os(iOS)` — defensive choice. Rationale: inferred (not explicitly stated in spec, but consistent with design's audit).

4. **`DayDetailMacToolbar` struct** — `Flux/Flux/DayDetail/DayDetailViewSupport.swift:189-218`. New `ToolbarContent`-conforming struct with two `Button`s. Why it matters: this is where the chevron buttons actually live, with the `.disabled(viewModel.isToday)` predicate that's the AC 4.6 contract. Takeaway: read `viewModel.isToday` directly, no `@Bindable` needed — Observation tracking works transparently inside `ToolbarContent.body`. Rationale: decision_log.md Decision 11 (extraction driven by file_length cap, not in original tasks.md).

5. **Scene sizing block** — `Flux/Flux/FluxApp.swift:48-61`. `.frame(minWidth: 960, minHeight: 600)` on content + `.defaultSize(1200, 800)` + `.windowResizability(.contentSize)` on the scene. Why it matters: without the `.windowResizability` modifier, the `.frame` alone doesn't clamp `NSWindow`, and AC 2.2 silently breaks. Takeaway: the inline comment captures the gotcha — keep it; future readers will hit the same trap. Rationale: design.md Components / `FluxApp` + tasks.md task 5 "Critical:" note.

6. **`macPageTitle` computed property** — `Flux/Flux/DayDetail/DayDetailView.swift:367-377`. Three-branch formatter: `"Today"` if `isToday`, else `DayDetailEyebrow.full.string(from: parsedDate)`, else raw `viewModel.date` as fallback. Why it matters: AC 4.3's reactive read guarantee depends on this being a computed property (not stored). `internal` visibility (not `private`) is intentional so the unit test can read it. Takeaway: the fallback mirrors `DayNavigationHeader.formattedDate` — keep them in sync if either changes. Rationale: code comment + decision_log.md Decision 10.

---

## Learnings

Patterns worth carrying to future work:

- **Gate-flip propagation**: when you have N views all reading the same boolean gate, you can change N runtime behaviors with one line — *if* the per-call-site behavior is already correct in both branches. The design's per-call-site audit table is what makes that safe; without it the change is risky. See `design.md` Per-Call-Site Gate Audit and `IPadLayoutGate.swift:15-21`.

- **`#if !os(X)` over `#if os(Y)`**: defensive platform wraps default to "keep behavior", not "drop it". Pattern at `DayDetailView.swift:165-170` (wrap is `#if !os(macOS)`, not `#if os(iOS)`).

- **`.frame(minWidth:)` + `.windowResizability(.contentSize)` is the macOS hard-floor pattern**. One without the other lets the user clip the content. `FluxApp.swift:48-61` documents it inline so it stays found-by-grep.

- **Computed property + Observation tracking = automatic toolbar reactivity**. No `@Bindable` ceremony needed for `ToolbarContent` — reading the view model directly inside `body` registers the dependency. `DayDetailMacToolbar` at `DayDetailViewSupport.swift:189-218` and `macPageTitle` at `DayDetailView.swift:367-377`.

- **`internal` is fine for test-only access to a SwiftUI computed property** — there's no protected scope in Swift, and `private` would require either `@testable` workarounds or duplicating the formatter in the test. Code comment at `DayDetailView.swift:367` makes the trade explicit so it's not mistaken for accidental over-exposure.

- **Document deliberate divergence from tasks.md in the decision log**, not just the commit. Decision 11 captures the `DayDetailMacToolbar` extraction so a reader of the spec doesn't have to spelunk commits to find out why the toolbar isn't inline as tasks.md said.

---

## Completeness Assessment

### Fully Implemented

| AC | Verified by |
|---|---|
| [1.1](requirements.md#1.1) Dashboard adaptive on macOS | Gate flip → `dashboardContentRegular` selection (no legacyHeader); `DashboardView.swift` |
| [1.2](requirements.md#1.2) History adaptive on macOS | Gate flip → `historyContentRegular`; no other changes needed |
| [1.3](requirements.md#1.3) Breakpoints reused, 699/700/999/1000 boundaries | Existing `AdaptiveColumnsLayoutTests` covers boundaries cross-platform |
| [1.5](requirements.md#1.5) Dynamic Type tier drop | Behavior inherited unchanged from iPad's `AdaptiveColumnsLayout` |
| [2.1](requirements.md#2.1) Day Detail 2-column Grid on macOS | Gate flip → `dayDetailContentRegular` |
| [2.2](requirements.md#2.2) Scene `minWidth: 960`, `minHeight: 600` | `FluxApp.swift:48-61` `.frame` + `.windowResizability(.contentSize)` |
| [3.1](requirements.md#3.1) macOS Dashboard skips `legacyHeader` | Gate flip routes through regular branch which excludes it |
| [3.2](requirements.md#3.2) macOS Dashboard title "Dashboard" | `DashboardView.swift:54-58` `#if os(macOS) .navigationTitle("Dashboard")` |
| [3.3](requirements.md#3.3) iPhone/iPad Dashboard unaffected | Mac-only modifier; iOS branch unchanged |
| [4.1](requirements.md#4.1) macOS Day Detail no `DayNavigationHeader` | `DayDetailView.swift:165-170` `#if !os(macOS)` wrap |
| [4.2](requirements.md#4.2) Nav title `"Today"` or `DayDetailEyebrow.full` | `macPageTitle` at `DayDetailView.swift:367-377` |
| [4.3](requirements.md#4.3) Reactive read of `isToday`/`date` | Computed property reads observed properties; SwiftUI re-evaluates on mutation |
| [4.4](requirements.md#4.4) Trailing prev/next buttons | `DayDetailMacToolbar` `ToolbarItemGroup(placement: .primaryAction)` |
| [4.5](requirements.md#4.5) Accessibility labels | `.accessibilityLabel("Previous day")` / `.accessibilityLabel("Next day")` in `DayDetailMacToolbar` |
| [4.6](requirements.md#4.6) Next disabled when `isToday`, prev always enabled | `.disabled(viewModel.isToday)` on next button only |
| [4.8](requirements.md#4.8) Toolbar items gated `#if os(macOS)` | Whole `DayDetailMacToolbar` struct and its callsite are `#if os(macOS)` |
| [4.9](requirements.md#4.9) iPhone/iPad Day Detail unaffected | Mac-only wrap; iOS branches unchanged |
| [5.1](requirements.md#5.1) Gate selects adaptive on macOS | `IPadLayoutGateTests.macOSAlwaysReturnsTrue` passes |
| [5.2](requirements.md#5.2) iOS behavior preserved | iOS branch of the gate unchanged |
| [5.3](requirements.md#5.3) Per-call-site table in design | `design.md` Architecture / Per-Call-Site Gate Audit (5 sites listed) |
| [6.3](requirements.md#6.3) No iOS rendering change | All new modifiers are `#if os(macOS)`-gated |
| [7.1](requirements.md#7.1) Gate unit test | `IPadLayoutGateTests.swift` |
| [7.2](requirements.md#7.2) Toolbar enabled/disabled and navigate calls | `DayDetailMacToolbarTests.swift` (with acknowledged toolbar-introspection gap, documented) |
| [7.3](requirements.md#7.3) Boundary tests | Existing `AdaptiveColumnsLayoutTests` |
| [7.4](requirements.md#7.4) Nav title formatter test | `DayDetailNavTitleFormatterTests.swift` |
| [7.5](requirements.md#7.5) Existing test suites unchanged | None of the existing test files were modified |

### Verification Pending (manual ACs, by design)

| AC | Status |
|---|---|
| [1.4](requirements.md#1.4) Live window resize 1000pt boundary | Manual; design.md Testing Strategy notes flicker is inherited from iPad behavior, no mitigation in this PR |
| [2.3](requirements.md#2.3) `.accessibility5` at min width | Manual visual check on macOS 26 |
| [4.7](requirements.md#4.7) ←/→ keyboard nav unchanged | Manual; the existing `.onKeyPress` handlers are untouched |
| [6.1](requirements.md#6.1) No Liquid Glass artifacts | Manual visual check, light + dark |
| [6.2](requirements.md#6.2) Toolbar contrast | Manual visual check |

These are flagged in `design.md` Testing Strategy as manual ACs by design; they're not gaps, but a reviewer should ensure the PR comment captures the visual verification.

### Deliberate Divergence from `design.md` / `tasks.md`

- **Toolbar location**: `design.md` (Components / `DayDetailView`) and `tasks.md` task 4 specify the toolbar block inline in `DayDetailView.body`. The implementation extracted it to `DayDetailMacToolbar: ToolbarContent` in `DayDetailViewSupport.swift`. Rationale captured in decision_log.md Decision 11 (added 2026-05-25 after the inline form pushed the file past the 400-line SwiftLint cap) and commit c853330. The `.toolbar` modifier in `DayDetailView` is now a one-liner: `.toolbar { DayDetailMacToolbar(viewModel: viewModel) }`. Semantics preserved.

No other divergences from the design were found.

### Missing / Partial

Nothing missing relative to the requirements document. Every AC is either implemented (with an automated test, where applicable) or flagged as a manual AC in design.md's Testing Strategy.

---

## Open Questions for the Author

None. All non-trivial decisions are sourced from `decision_log.md` (Decisions 1–11) or from inline code comments. The one implementation-time divergence (toolbar extraction) is captured in Decision 11 and commit c853330. The `#if !os(macOS)` vs `#if os(iOS)` wrap choice is marked `inferred` above — confirming it's the deliberate defensive choice would close that out, but it isn't load-bearing for the feature.
