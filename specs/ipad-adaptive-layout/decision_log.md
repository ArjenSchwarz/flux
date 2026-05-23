# Decision Log: iPad Adaptive Layout

## Decision 1: Use full spec workflow rather than smolspec

**Date**: 2026-05-22
**Status**: accepted

### Context

T-1150 calls for adapting the existing iOS app to iPad-appropriate layouts. The scope assessment found four screens to adapt (Dashboard ~650 LOC, History ~1,780 LOC, Day Detail ~2,224 LOC, Settings ~1,090 LOC), an architectural decision about navigation chrome at regular size class, and a must-not-regress constraint for the iPhone V5 shell and the macOS `NavigationSplitView` shell. The codebase currently has no `userInterfaceIdiom` checks and only one `horizontalSizeClass` branch (`HistoryStatsOverviewCard.swift` line 22-28).

### Decision

Run the full spec workflow (`/starwave:creating-spec`) — requirements → design → tasks — rather than the lightweight smolspec path.

### Rationale

Every smolspec gate fails: estimated impact exceeds 80 LOC and three files; the work crosses multiple subsystems (navigation shell, four screens, shared `Screen` enum); it requires an architectural decision (sidebar vs tab-bar chrome) that should be recorded; and it must explicitly avoid regressing two existing shells. A formal design phase makes the chrome decision explicit and gives a place to document size-class transition behaviour.

### Alternatives Considered

- **smolspec (lightweight spec)**: Use `/starwave:smolspec` to write a brief spec and proceed directly to implementation - Rejected because the work exceeds smolspec sizing thresholds and includes an architectural decision worth documenting.
- **Implement directly from the Transit ticket**: Skip spec creation, branch + implement - Rejected because there is no recorded decision for the chrome choice and no place to capture size-class transition expectations before code goes in.

### Consequences

**Positive:**
- Architectural decision (sidebar vs tab bar) is captured in `decision_log.md` rather than implicit in code.
- Per-screen layout requirements are testable acceptance criteria, not informal notes.
- Future readers can find the iPad-vs-macOS / iPad-vs-iPhone rationale in one place.

**Negative:**
- Spec phase adds upfront effort before implementation begins.

---

## Decision 2: Sidebar shell at regular size class, iPhone shell at compact

**Date**: 2026-05-22
**Status**: accepted

### Context

T-1150 explicitly flags the navigation-chrome decision: reuse the macOS `NavigationSplitView` sidebar pattern on iPad at regular size class, or keep `FluxTabBar` everywhere and pair it with a multi-column content layout. iPad must also handle compact widths (Slide Over, narrow Split View) where a sidebar would be cramped.

### Decision

At regular horizontal size class, iPad renders a two-column `NavigationSplitView` with a sidebar (Dashboard / Today / History) and a detail column hosting the selected screen. At compact horizontal size class, iPad falls back to the existing `FluxiOSRoot` + `FluxTabBar` shell.

### Rationale

The sidebar matches the macOS shell already in `AppNavigationView`, which keeps cross-platform behaviour consistent and lets Liquid Glass apply its sidebar styling by default. Falling back to the iPhone shell at compact width avoids reinventing a tab bar for cramped split-view widths and gives the user the chrome they already know. Settings stays as a sheet (reachable via the existing `onSettingsTap` affordance on each screen) so the existing per-screen settings button continues to work without restructuring the sidebar.

### Alternatives Considered

- **Keep `FluxTabBar` at all widths with multi-column content**: Preserves V5 visual identity at regular width - Rejected because it diverges from the macOS shell and leaves the user with two competing patterns across platforms.
- **Sidebar visible alongside `FluxTabBar`**: Both chrome elements present at regular width - Rejected as visually redundant.
- **Three-column shell (sidebar + History list + Day Detail)**: Classic mail-style iPad layout - Rejected as a bigger restructure than warranted; the macOS shell uses two columns and matching it keeps the cross-platform model simple.

### Consequences

**Positive:**
- Matches macOS shell, reducing the number of distinct navigation models in the app.
- Compact-width fallback reuses tested iPhone shell rather than introducing a new cramped layout.
- Settings stays accessible via the existing per-screen affordance with no sidebar restructure.

**Negative:**
- Two navigation shells coexist on iPad and must stay in sync (sidebar selection ↔ tab binding) across size-class transitions.
- Sidebar omits Settings, which is conventional but means iPad users discover Settings via the same per-screen button as iPhone rather than a sidebar row.

### Impact

`Flux/Flux/Navigation/AppNavigationView.swift`, `Flux/Flux/Navigation/FluxiOSRoot.swift`, `Flux/Flux/Navigation/SidebarView.swift`, `Flux/Flux/RootView.swift`.

---

## Decision 3: Today sidebar entry maps to existing `Screen.today` with auto-rollover

**Date**: 2026-05-22
**Status**: accepted

### Context

The macOS sidebar already includes a "Today" entry that renders `DayDetailView(date: DateFormatting.todayDateString())`. Peer review of the requirements flagged that the iPad sidebar inherits the same entry but the requirements did not pin its identity, root view, or behaviour when the local date crosses midnight while the entry is selected. The iPhone shell has no equivalent — Day Detail is reached via the "Today" tab in `FluxiOSRoot`, which uses the same date formatter.

### Decision

The Today sidebar entry on iPad uses the existing `Screen.today` enum case (no new screen). Its root view is `DayDetailView(date: DateFormatting.todayDateString())`. When the local date rolls over while Today is selected, the detail column updates to the new date on the next view appearance or refresh tick, without requiring the user to re-select Today.

### Rationale

Reusing `Screen.today` keeps the sidebar mapping identical across macOS and iPad and avoids adding a new navigation concept (which Non-Goals explicitly forbids). The date formatter is already shared. Midnight rollover handling matches what the existing macOS Today entry does implicitly — the date is recomputed on view recreation; pinning that behaviour in requirements prevents a regression where the entry sticks to yesterday's date until the user manually navigates away and back.

### Alternatives Considered

- **Introduce a new `Screen.dateBrowser` case**: Add explicit support for "the day you're currently viewing", with date as state - Rejected as out of scope; Non-Goals forbids new screens.
- **Recompute date only on app foreground**: Skip in-foreground rollover handling - Rejected because Flux is often left running on a charger / desk; the user would routinely see yesterday's date after midnight without it.

### Consequences

**Positive:**
- Sidebar mapping is identical macOS ↔ iPad; no new enum case.
- Rollover behaviour is explicit and testable.

**Negative:**
- Implementation must verify the date recomputation actually fires on view appearance / refresh tick (not just on construction); requires a deliberate test.

---

## Decision 4: Scene-local navigation state, no scene restoration

**Date**: 2026-05-22
**Status**: accepted

### Context

iPad supports multiple windows via `UIScene`. Two reviewers raised what should happen when the user opens a second window: should sidebar selection sync across windows, persist across termination, or be entirely independent? Round-1 review pushed for full multi-window persistence (`NSUserActivity`, state restoration); round 2 pushed back as over-scoped for a personal AlphaESS monitor with two users.

### Decision

Sidebar selection and the detail column's `NavigationPath` are scene-local: each window has its own independent state. The spec does NOT require persistence of selection across scene termination, scene restoration via `NSUserActivity`, or cross-window selection sync.

### Rationale

Scene-local state is the SwiftUI default when `@State` is used inside the root scene view; meeting the requirement is essentially "don't accidentally hoist selection into a global". Adding full scene restoration would expand the work substantially for negligible user benefit at this app's scale (2 users, primarily single-window).

### Alternatives Considered

- **Full scene restoration with `NSUserActivity`**: Persist sidebar selection per-scene across termination - Rejected as disproportionate for a 2-user personal app.
- **Cross-window selection sync (shared global state)**: Selection in one window updates all windows - Rejected as anti-pattern for iPad multi-window UX.

### Consequences

**Positive:**
- Minimal implementation burden — falls out of using `@State` correctly.
- Each window behaves the way iPad users expect.

**Negative:**
- A user who frequently uses multi-window may want their last-selected sidebar entry remembered per scene; deferred to a follow-up if it becomes a real ask.

### Impact

`Flux/Flux/Navigation/AppNavigationView.swift` (state ownership).

---

## Decision 5: `NavigationSplitView(.balanced)` on iPad; reuse hoisted view-model for Today rollover

**Date**: 2026-05-22
**Status**: accepted

### Context

Two design choices needed lockdown during the design phase. (a) `NavigationSplitView` accepts a style (`.automatic`, `.balanced`, `.prominentDetail`) that affects whether the sidebar auto-hides on smaller iPads — this changes detail-column widths and therefore the layout-breakpoint math. (b) The Today sidebar entry's date must update when local midnight passes; the initial design proposed `.id(today)` on `DayDetailView` to force a SwiftUI rebuild, but design review flagged that this discards transient UI state (chart highlight, scroll position, in-progress note editor draft, `comparisonState`).

### Decision

Use `.navigationSplitViewStyle(.balanced)` on iPad regular. Handle midnight rollover by calling a new `DayDetailViewModel.setDate(_:)` on the hoisted Today VM rather than rebuilding the view with `.id(today)`.

### Rationale

`.balanced` lets the sidebar coexist with the detail on iPad Pro and auto-hides it on iPad mini portrait via the system sidebar toggle, which gives the detail column meaningful width across the iPad lineup without per-device branching. `.prominentDetail` would always favour the detail and feel modal; `.automatic` would defer to SwiftUI's heuristics, which can change between iOS versions.

`setDate(_:)` reuses the same `DayDetailViewModel` instance, so:
- View-level `@State` (note editor sheet, scroll position, chart highlights) is preserved.
- `@AppStorage` settings (compareEnabled, comparePeriod) are unaffected either way.
- The VM's per-day fields are explicitly cleared so stale yesterday data does not bleed into today.
The `.id(today)` rebuild approach would have nuked all transient state at every midnight crossing — a poor trade-off for a once-a-day event.

### Alternatives Considered

- **`.prominentDetail` style**: Always-on detail with sidebar accessible via toggle - Rejected because the user benefit of a persistent sidebar (one-tap nav) outweighs the extra width.
- **`.automatic` style**: Let SwiftUI choose - Rejected because behaviour can change between iOS versions and we want deterministic breakpoint math.
- **`.id(today)` SwiftUI rebuild for rollover**: Trigger a full DayDetailView rebuild when today's date changes - Rejected because it discards transient UI state the user cares about more than they care about midnight precision.
- **Don't handle rollover; require manual reselect**: Skip the rollover task entirely - Rejected because Flux is often left running on a charger/desk overnight; users would see yesterday's date on the Today entry until they manually navigate away and back.

### Consequences

**Positive:**
- One style choice covers all iPad sizes without per-device branches.
- Midnight rollover preserves what the user was doing in the view.
- `setDate(_:)` is independently testable.

**Negative:**
- `.balanced` on iPad mini portrait gives a narrower detail column than `.prominentDetail` would; the `< 700` breakpoint absorbs this by falling back to single column.
- `DayDetailViewModel` gains a mutating method (`setDate`) that is only used by the Today rollover path; needs a comment explaining it is not for general date navigation (which always constructs a new VM).

### Impact

`Flux/Flux/Navigation/FluxiPadRoot.swift` (style application), `Flux/Flux/DayDetail/DayDetailViewModel.swift` (new method), `Flux/Flux/Navigation/AppNavigationView.swift` (rollover task).

---
