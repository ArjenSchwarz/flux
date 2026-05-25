# Decision Log: Better macOS Interface

## Decision 1: Use Full Spec Workflow

**Date**: 2026-05-24
**Status**: accepted

### Context

T-1342 asks to port the iPad adaptive multi-column layout to macOS and remove the "mustache" date view from Dashboard and Day Detail. Scope assessment identified ~6–10 files affected, ~400–600 LOC, cross-cutting UX decisions (date navigation replacement, breakpoints, shell choice), and three top-level screens touched.

### Decision

Use the full spec workflow (requirements → design → tasks) rather than smolspec.

### Rationale

Multiple non-trivial UX decisions exist (macOS date nav replacement, whether to add a 4th column tier, whether to replace `AppNavigationView` with `FluxiPadRoot`). LOC and file count exceed smolspec thresholds. Smolspec would skip the explicit decision capture these choices warrant.

### Alternatives Considered

- **Smolspec**: Single lightweight doc, then implement. Rejected because UX decisions need explicit framing and review.
- **Direct implementation**: Rejected because cross-cutting macOS chrome changes risk regressions on iOS without a deliberate scope boundary.

### Consequences

**Positive:**
- Decisions captured and reviewable.
- Tasks can be sized predictably.

**Negative:**
- Slower start than smolspec.

---

## Decision 2: Keep AppNavigationView Shell

**Date**: 2026-05-24
**Status**: accepted

### Context

iPad uses `FluxiPadRoot` as its NavigationSplitView shell. macOS currently uses `AppNavigationView`, which also presents a NavigationSplitView sidebar but routes detail content differently. The question was whether to promote `FluxiPadRoot` to be the shared iPad+macOS shell or keep `AppNavigationView` on macOS.

### Decision

Keep `AppNavigationView` as the macOS shell. Enable the existing adaptive multi-column bodies on macOS by changing the layout gate, not by swapping the shell.

### Rationale

`AppNavigationView` already has macOS-specific behaviors wired (keyboard commands, refresh action, scene phase handling). Replacing it would force re-validating all of that. The user's intent is the multi-column body, not the shell. Smaller blast radius keeps the change auditable.

### Alternatives Considered

- **Promote FluxiPadRoot to shared iPad+macOS shell**: Reduces shell duplication but couples macOS chrome to iPad evolution and requires re-validating macOS-specific behaviors. Rejected.

### Consequences

**Positive:**
- Minimal churn to existing macOS chrome.
- No risk of regressing macOS keyboard commands / refresh.

**Negative:**
- Two shells continue to exist (one for iPad, one for macOS) — accepted as the cost of platform-specific chrome.

---

## Decision 3: Reuse iPad Breakpoints on macOS

**Date**: 2026-05-24
**Status**: accepted

### Context

macOS windows can be much wider than an iPad. The breakpoint question was: reuse iPad's 1/2/3-column thresholds (700pt / 1000pt), add a 4th column tier for wide displays, or keep 3 columns but let cards grow wider.

### Decision

Reuse the iPad breakpoints unchanged on macOS: 1 column below 700pt, 2 columns 700–1000pt, 3 columns at or above 1000pt.

### Rationale

Predictable behavior across platforms. No per-platform tuning of `AdaptiveColumnsLayout`. The 3-column cap is acceptable for personal-use density. Adding a 4th tier would require new tests and design validation for card sizing at very wide widths without a clear product need today.

### Alternatives Considered

- **Add a 4th column tier ≥ 1400pt**: More density on wide displays. Rejected — no current product need, more layout/test surface.
- **Cap at 3 columns but allow cards to grow wider**: Simpler but reduces information density gains on wide windows. Rejected — current behavior already grows cards to fill columns.

### Consequences

**Positive:**
- Zero change to `AdaptiveColumnsLayout`.
- Same visual behavior across iPad and macOS.

**Negative:**
- Wide macOS windows leave horizontal whitespace beyond the 3-column tier.

---

## Decision 4: Day Detail Matches iPad Two-Column Grid

**Date**: 2026-05-24
**Status**: accepted

### Context

iPad Day Detail uses a fixed two-column `Grid` (summary cards left, power + battery charts stacked right) rather than `AdaptiveColumnsLayout`. macOS could match this, use the adaptive reflow, or introduce a macOS-specific 3-pane layout.

### Decision

macOS Day Detail uses the same fixed two-column `Grid` as iPad.

### Rationale

The iPad Grid was designed to pair charts with summary cards visually. Reusing it on macOS preserves the design intent and avoids reimplementing the layout. A 3-pane macOS-specific layout would be a bigger effort and depart from the iPad pattern without a clear reason.

### Alternatives Considered

- **AdaptiveColumnsLayout reflow on macOS Day Detail**: Changes the iPad pattern's intent (charts paired with summary). Rejected.
- **Custom macOS 3-pane layout (nav | summary | charts)**: Bigger design effort, departs from iPad. Rejected.

### Consequences

**Positive:**
- Reuses the same view code path as iPad.
- Visually consistent across iPad and macOS.

**Negative:**
- Wide macOS windows show wider cards rather than more columns.

---

## Decision 5: Replace Day Detail Mustache with Window Toolbar Chrome

**Date**: 2026-05-24
**Status**: accepted

### Context

`DayNavigationHeader` (the chevron-date-chevron "mustache") is awkward inside a macOS window where the title bar and toolbar already exist. The replacement options were: toolbar chevrons + date label, toolbar date-picker popover, NavigationStack title only (keyboard-only), or keep the in-content header.

### Decision

Replace the in-content `DayNavigationHeader` on macOS with: (1) the date displayed in the navigation title / window toolbar, (2) prev/next chevron toolbar buttons that drive `viewModel.navigatePrevious()` / `viewModel.navigateNext()`, and (3) keep the existing ←/→ keyboard navigation.

### Rationale

Uses native macOS chrome. Discoverable (chevrons visible in toolbar) while keeping keyboard parity. Date in title matches the macOS convention for "what am I looking at right now". No calendar popover this iteration — keeps scope tight.

### Alternatives Considered

- **Toolbar date picker (calendar popover)**: Allows jumping to any date. Deferred — scope-limit this iteration.
- **NavigationStack title only (keyboard-only)**: Hides prev/next from non-keyboard users. Rejected.
- **Keep in-content header on macOS**: Defeats the feature's intent. Rejected.

### Consequences

**Positive:**
- Native macOS chrome, no duplicated header inside content.
- Keyboard, mouse, and touch (if any) all supported.

**Negative:**
- No direct-jump date picker yet — users must step day-by-day or use keyboard arrows.

---

## Decision 6: Dashboard Drops legacyHeader on macOS

**Date**: 2026-05-24
**Status**: accepted (confirmed by user 2026-05-25 after design-critic flagged the interpretation)

### Context

Dashboard renders an in-content `legacyHeader` (uppercase eyebrow + "Battery" title) on macOS via its legacy header branch, even though `AppNavigationView` already supplies a navigation title. iPad's `dashboardContentRegular` explicitly skips this block. The user's "Dashboard loses the silly mustache date view" was interpreted to include this duplicated header on macOS (Dashboard does not actually have a mustache-shaped date header today). The design-critic review flagged that this was an interpretation needing user confirmation; the user confirmed during the requirements review cycle.

### Decision

macOS Dashboard skips `legacyHeader` and shows "Dashboard" as the navigation title.

### Rationale

Removes the duplicated header. Aligns macOS Dashboard chrome with the iPad approach. The eyebrow ("Now · HH:MM · MMM d") is informational but redundant on macOS where the menu bar clock provides time and the date is rarely the user's primary question on a live-data screen.

### Alternatives Considered

- **Keep eyebrow as a subtitle**: Adds chrome complexity. Rejected by user.
- **Move eyebrow to toolbar**: Would clutter the toolbar with non-actionable text. Rejected.
- **Keep legacyHeader as-is** (only remove the Day Detail mustache): Considered after the design-critic review and explicitly rejected by the user.

### Consequences

**Positive:**
- Clean macOS Dashboard chrome matching iPad.
- One less code path to maintain (`legacyHeader` is no longer used in any adaptive context).

**Negative:**
- Users lose the inline "Now · HH:MM · MMM d" eyebrow on macOS. The menu bar clock covers time and the live-updating panels themselves convey freshness — accepted as sufficient.

---

## Decision 7: macOS Day Detail Toolbar — Date in Nav Title, Chevrons Trailing

**Date**: 2026-05-25
**Status**: accepted

### Context

When replacing the in-content `DayNavigationHeader` on macOS, the date label and the prev/next chevrons can be arranged several ways: date in the navigation title with chevrons grouped in the toolbar; date as a principal toolbar item flanked by chevrons (mirrors the deleted mustache); date in the title with chevrons split leading/trailing.

### Decision

The date is shown as the navigation title. The prev/next chevrons live together in a trailing toolbar group.

### Rationale

Date-as-nav-title is the most native macOS pattern for "what am I looking at right now" and integrates with the existing `AppNavigationView` shell without introducing custom principal toolbar layout. Grouping the chevrons trailing keeps them visually associated as a single control pair and aligns with the common macOS pattern for paired step controls.

### Alternatives Considered

- **Principal toolbar item with chevrons flanking the date**: Closer to the deleted mustache visually but introduces a custom toolbar layout and an unusual macOS pattern. Rejected.
- **Chevrons split leading/trailing with centered title**: Closer to mustache spirit but separates the paired control. Rejected.

### Consequences

**Positive:**
- Native chrome with no custom layout work.
- Toolbar group can be augmented later (e.g., a calendar popover) without re-arranging the layout.

**Negative:**
- Visual association between date and chevrons is looser than the deleted mustache — accepted because the chevrons remain a clearly paired group.

---

## Decision 8: No Replacement Freshness Indicator on macOS Dashboard

**Date**: 2026-05-25
**Status**: accepted

### Context

The design-critic review flagged that removing the "Now · HH:MM · MMM d" eyebrow on macOS Dashboard (Decision 6) takes away the only inline cue that the live data is fresh. Options were: add nothing; add an "Updated HH:MM" toolbar label; retain the eyebrow as a subtitle.

### Decision

Nothing replaces the eyebrow. The macOS menu bar clock and the live-updating Dashboard panels themselves convey freshness.

### Rationale

Adding an "Updated HH:MM" toolbar label introduces a small but always-changing toolbar element that competes visually with the nav title and the action buttons; it provides marginal value over the menu bar clock and the panels' own ticking values. Retaining the eyebrow reverses Decision 6 entirely. The simplest answer is best for a personal-use app.

### Alternatives Considered

- **Add "Updated HH:MM" toolbar label**: Marginal value, adds chrome. Rejected.
- **Keep eyebrow as a smaller subtitle**: Reverses Decision 6. Rejected.

### Consequences

**Positive:**
- Cleanest macOS Dashboard chrome.
- No new always-rebuilding view to manage.

**Negative:**
- Users relying on the eyebrow as the freshness cue will lose it. Mitigated by the menu bar clock and the live numbers visibly updating.

---

## Decision 9: macOS Scene Window Minimum Width 960pt

**Date**: 2026-05-25
**Status**: accepted (revised after round-2 review surfaced sidebar-vs-detail-column ambiguity)

### Context

The round-1 peer review noted that AC 1.5's "collapse to 1 column below 700pt" cannot apply to Day Detail because Decision 4 keeps it on a fixed 2-column `Grid` rather than `AdaptiveColumnsLayout`. An initial clamp of ≥ 720pt "content width" was added — but the round-2 review pointed out that (a) "content width" was ambiguous (window vs detail column), (b) `NavigationSplitView` sidebar (~240pt) plus chrome eats into any window-level budget, and (c) clamping the window scene-wide while leaving Dashboard/History expected to reach the 1-column tier (AC 1.3 boundary at 699pt) is internally contradictory.

### Decision

The macOS app scene sets `minWidth: 960pt` and `minHeight: 600pt`. This guarantees the detail column always exceeds 700pt (well above the 2-column `AdaptiveColumnsLayout` threshold and the Day Detail Grid's natural minimum). The macOS adaptive bodies therefore start at the 2-column tier at runtime; the 1-column tier is reachable only via the `AdaptiveColumnsLayout` unit test, not via window resize.

### Rationale

A single scene-wide minimum is one line of code and matches what most native macOS apps do for data-rich layouts (Mail, Notes, Calendar). Sizing it from the *detail-column* requirement (700pt for the iPad regular threshold) plus the sidebar (~240pt) plus divider/padding (~20pt) produces ~960pt, which comfortably covers both Day Detail's Grid and the adaptive bodies' 2-column tier. The 1-column tier on macOS is a deliberate non-runtime path; unit-test-only coverage is acceptable because the same `AdaptiveColumnsLayout` code runs cross-platform.

### Alternatives Considered

- **Clamp ≥ 720pt content width (no derivation)**: Ambiguous (window vs detail column) and too narrow once the sidebar is subtracted. Rejected by round-2 review.
- **Add a 1-col Day Detail fallback on macOS**: Doubles the Day Detail macOS layout surface for an edge case. Rejected.
- **Let users resize below the Grid's natural minimum and accept horizontal clipping**: Bad UX. Rejected.
- **Per-route window minimums (relax to 800pt outside Day Detail, clamp to 960pt only while Day Detail is the active route)**: Fragile interaction with `NavigationSplitView` and `@SceneStorage`; the visible window jump on navigation would be jarring. Rejected.

### Consequences

**Positive:**
- One concrete, testable scene `minWidth` value.
- Day Detail and adaptive-body code stay identical to iPad.
- No new layout fallback paths to maintain.

**Negative:**
- Users cannot shrink the macOS window below 960×600.
- macOS Dashboard and History never reach the 1-column tier at runtime — accepted as a deliberate consequence of matching the iPad layout.

---

## Decision 10: macOS Day Detail Nav Title Uses `DayDetailEyebrow.full`

**Date**: 2026-05-25
**Status**: accepted

### Context

[AC 4.2](requirements.md#4.2) specifies the macOS Day Detail nav title uses `DayDetailEyebrow.full` (system `.full` date style — "Friday, May 24, 2026"). The existing `pageTitle` computed property in `DayDetailView` uses `DayDetailEyebrow.formatter` ("EEE · MMM d · yyyy" — "Fri · May 24 · 2026") and is reused by the iPad nav title. The choice surfaced during design: reuse `pageTitle` for cross-platform consistency, or honor AC 4.2 verbatim and use `.full`.

### Decision

Add a macOS-only `macPageTitle` computed property that uses `DayDetailEyebrow.full`. The iPad nav title continues to use `pageTitle` / `DayDetailEyebrow.formatter`.

### Rationale

The user explicitly chose this in the design-phase review to preserve the deleted mustache's verbose-date character on macOS. Cross-platform consistency in nav title format was considered but rejected — the platforms can carry slightly different chrome conventions.

### Alternatives Considered

- **Reuse `pageTitle` (DayDetailEyebrow.formatter)**: Cross-platform consistency, one fewer computed property. Rejected by user in favor of the mustache-matching format.

### Consequences

**Positive:**
- macOS Day Detail nav title reads more like a sentence ("Friday, May 24, 2026") — natural in a macOS window title.
- The mustache's verbose-date character is preserved on the platform that's losing the mustache.

**Negative:**
- Two formatters in active use for Day Detail titles (`pageTitle` on iPad, `macPageTitle` on macOS).
- A future request to "make all nav titles consistent" would need to pick one.

---

## Decision 11: Extract macOS Toolbar to `DayDetailMacToolbar` Struct

**Date**: 2026-05-25
**Status**: accepted

### Context

[Task 4](tasks.md) and `design.md` specify the macOS prev/next chevron toolbar as an inline `.toolbar { ToolbarItemGroup(placement: .primaryAction) { ... } }` block inside `DayDetailView.body`. During implementation, adding the toolbar inline pushed `DayDetailView.swift` past SwiftLint's 400-line `file_length` cap. A `// swiftlint:disable file_length` would suppress the warning but the existing project convention pairs the disable with an `enable` at end-of-file (see `APIModels.swift`, `APIModelsTests.swift`), and the file was already carrying a `type_body_length` disable for similar reasons.

### Decision

Extract the toolbar into a new `DayDetailMacToolbar: ToolbarContent` struct in `Flux/Flux/DayDetail/DayDetailViewSupport.swift`, gated `#if os(macOS)`. `DayDetailView.body` keeps a one-line `.toolbar { DayDetailMacToolbar(viewModel: viewModel) }` call so the wiring point is still obvious. The `file_length` disable still wraps the file (paired with `enable` at EOF) because the file is 416 lines after the extraction, but the toolbar code itself lives in the support file.

### Rationale

The extraction is a semantics-preserving refactor — the toolbar reads `viewModel.isToday` directly and SwiftUI's Observation tracking works transparently inside `ToolbarContent.body`, so the disabled-state reactivity is identical to the inline form. Splitting the toolbar into its own struct gives it a doc comment in isolation (which the inline form would not have) and matches the existing `DayDetailViewSupport.swift` convention of housing Day Detail subviews (`DayDetailErrorPanel`, `DayDetailMessagePanel`, `DayNavigationHeader`).

### Alternatives Considered

- **Keep the toolbar inline and only add `// swiftlint:disable file_length`**: Matches `design.md` literally but adds another suppression to a file that's already over-budget. The extraction is the cleaner answer.
- **Inline the toolbar and split a different chunk of `DayDetailView` out**: Would touch unrelated, working code (e.g. `dayDetailContent`, `header`, `summaryColumn`) for no benefit beyond satisfying the task description verbatim. Rejected.

### Consequences

**Positive:**
- `DayDetailMacToolbar` has its own dedicated doc comment explaining the AC 4.6 disabled-state mirror.
- `DayDetailView.body` stays readable — one line for the toolbar wiring instead of an inline `ToolbarItemGroup` with two `Button { } label: { }` blocks.
- Follows the existing `DayDetailViewSupport.swift` convention for Day Detail subviews.

**Negative:**
- Reader has to jump to `DayDetailViewSupport.swift` to see the chevron button definitions.
- One more cross-file dependency for the macOS chrome.

### Impact

`Flux/Flux/DayDetail/DayDetailView.swift` (one-line `.toolbar { DayDetailMacToolbar(viewModel: viewModel) }` call), `Flux/Flux/DayDetail/DayDetailViewSupport.swift` (new struct, ~30 lines, `#if os(macOS)` gated).

---
