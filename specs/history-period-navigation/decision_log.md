# Decision Log: History Period Navigation

## Decision 1: Limit period navigation to the Wk and Mo ranges

**Date**: 2026-06-11
**Status**: accepted

### Context

The History screen offers five ranges: fixed 7d/14d/30d windows and calendar-anchored Wk/Mo views. T-1497 asks for the ability to go back a week or month at a time. The fixed ranges could in principle also shift backward by their own length.

### Decision

Previous/next-period navigation and the date picker apply only to the Wk and Mo ranges. The 7d/14d/30d ranges remain anchored to today, unchanged.

### Rationale

The ticket is framed around weeks and months, which have natural calendar boundaries to step across. Shifted fixed windows ("the 14 days before the last 14 days") have unclear semantics and no obvious labelling, for little user value.

### Alternatives Considered

- **Navigation on all ranges**: shift 7d/14d/30d windows by their own length - Rejected: ambiguous semantics, more code, not requested.

### Consequences

**Positive:**
- Smaller scope; clear period labels (week range, month name).

**Negative:**
- Fixed ranges cannot view past windows; users must use Wk/Mo for history browsing.

---

## Decision 2: Include a calendar date picker in scope

**Date**: 2026-06-11
**Status**: accepted

### Context

The ticket mentions "a calendar option might work as well". T-1361 previously excluded a calendar-popover date picker from the Wk/Mo feature. Chevron-only navigation makes reaching distant periods tedious.

### Decision

Include a date picker alongside the previous/next chevrons. Picking a date jumps to the week or month containing it.

### Rationale

The user explicitly chose to include it during requirements gathering. Unbounded backward navigation makes a direct-jump mechanism genuinely useful rather than a nice-to-have.

### Alternatives Considered

- **Chevrons only**: simpler UI - Rejected by user: stepping far back one period at a time is tedious.
- **Type-specific pickers** (month+year picker for Mo): more tailored - Rejected: two different controls for one job; a single date picker that snaps to the containing period is consistent across both ranges.

### Consequences

**Positive:**
- Direct access to any past period.

**Negative:**
- Extra UI surface on the History screen; picker must be capped at today.

---

## Decision 3: Unbounded backward navigation with empty periods

**Date**: 2026-06-11
**Status**: accepted

### Context

`flux-daily-energy` retains data indefinitely but only since the poller started collecting. The app does not currently know the earliest stored date. Navigation could either stop at the earliest data or continue into empty periods.

### Decision

Backward navigation is unbounded. Periods before data collection render with the full-period chart axis and empty values.

### Rationale

Stopping at the earliest date requires the app to learn that date (new API surface or inference), for marginal benefit in a two-user app. Empty periods are honest and cheap.

### Alternatives Considered

- **Disable previous at earliest stored day**: cleaner UX at the boundary - Rejected: needs an earliest-date API capability or response inference; not worth the surface area.

### Consequences

**Positive:**
- No new API capability needed for boundaries; simple navigation logic.

**Negative:**
- Users can scroll into stretches of empty periods with no signal that older data will never exist.

---

## Decision 4: Switching range resets to the current period

**Date**: 2026-06-11
**Status**: accepted

### Context

When viewing a past week and switching the range picker to Mo (or to a fixed range), the view could either preserve the anchor date (show the month containing that week) or reset to the current period.

### Decision

Any range change resets the view to the current period for the newly selected range.

### Rationale

The user chose the simpler mental model: the picker always lands on "now", and past periods are reached deliberately via chevrons or the date picker.

### Alternatives Considered

- **Preserve anchor date across range switches**: keeps the user's place in history - Rejected by user: more complex mental model and state handling.

### Consequences

**Positive:**
- Simple, predictable picker behaviour; no anchor-translation edge cases (e.g. week spanning two months).

**Negative:**
- Users comparing a past week against its containing month must re-navigate after switching.

---

## Decision 5: Single date picker that snaps to the containing period

**Date**: 2026-06-11
**Status**: accepted

### Context

The picker could be one control for both ranges, or tailored per range (date picker for Wk, month+year picker for Mo).

### Decision

One date picker for both ranges. The chosen date resolves to the week (Wk) or month (Mo) containing it.

### Rationale

One consistent control, one code path. Snapping to the containing period matches how the Wk/Mo ranges already define their boundaries (Sydney calendar).

### Alternatives Considered

- **Month+year picker for Mo**: avoids implying day-level precision - Rejected: introduces a second control style for marginal clarity.

### Consequences

**Positive:**
- One implementation; consistent interaction in both ranges.

**Negative:**
- In Mo, picking a specific day is slightly misleading since only the month matters.

---

## Decision 6: Quick-reset affordance in addition to the forward chevron

**Date**: 2026-06-11
**Status**: accepted

### Context

After navigating far back, returning to the current period via the forward chevron alone requires many taps.

### Decision

Provide a single-action control that returns to the current to-date period, shown or enabled only when viewing a past period.

### Rationale

Unbounded backward navigation plus a date picker means users can land far in the past; a one-tap return keeps the common case (checking current period) fast.

### Alternatives Considered

- **Forward chevron only**: minimal UI - Rejected by user: tedious after deep navigation.

### Consequences

**Positive:**
- Fast recovery to the default view.

**Negative:**
- One more control to fit into the History header/toolbar.

---

## Decision 7: Past-period averages divide by recorded days, with an "N of M days" indicator

**Date**: 2026-06-11
**Status**: accepted

### Context

Data collection started at a point in time, so the oldest periods have energy records for only some of their days. A per-day average could divide by calendar days in the period or by days that have data, and an incomplete period could silently look complete. External review split on the right divisor, making this an explicit product call.

### Decision

Per-day averages divide by the number of days in the period that have a stored daily-energy record. When recorded days are fewer than calendar days, the period overview shows an indicator such as "11 of 30 days".

### Rationale

Dividing by recorded days reflects actual daily usage rather than understating it with empty days. The indicator prevents an incomplete period from masquerading as a complete one, resolving the main objection to that divisor.

### Alternatives Considered

- **Divide by calendar days in the period**: honest about period length - Rejected: understates real per-day usage when data is missing.
- **Recorded-days divisor without an indicator**: less UI - Rejected: incomplete months silently look complete.

### Consequences

**Positive:**
- Averages stay meaningful for partially recorded periods; incompleteness is visible.

**Negative:**
- One more element in the period overview; "has data" needed a definition (a stored daily-energy row exists).

---

## Decision 8: Next-chevron disabled at current period; return-to-current control hidden

**Date**: 2026-06-11
**Status**: accepted

### Context

When the current period is displayed, the next-chevron and the return-to-current control are inapplicable. "Hidden or disabled" left untestable latitude and risked layout shift or dead chrome.

### Decision

At the current period, the next-chevron is visible but disabled (matching Day Detail's next-day button), and the return-to-current control is hidden entirely.

### Rationale

The chevron pair mirrors Day Detail's established pattern, so it follows that pattern's disabled state. A permanently visible but usually-disabled reset button would be dead chrome; it only appears when it can do something.

### Alternatives Considered

- **Both disabled**: no layout shift anywhere - Rejected: permanently visible reset control that is disabled most of the time.
- **Both hidden**: cleanest chrome - Rejected: chevron row changes shape and diverges from the Day Detail pattern.

### Consequences

**Positive:**
- Consistent with Day Detail; reset control self-explains by appearing only when useful.

**Negative:**
- Reset control appearing/disappearing causes minor layout change on navigation.

---

## Decision 9: Refresh re-fetches the displayed past period

**Date**: 2026-06-11
**Status**: accepted

### Context

Pull-to-refresh (iOS) and ⌘R (macOS) on the History screen need defined semantics while a past period is displayed: re-fetch the viewed period or reset to the current one.

### Decision

Refresh re-fetches the displayed period and keeps the user's place.

### Rationale

Refresh meaning "reload what I'm looking at" is predictable. Past data is mostly immutable, but notes and late-computed derived stats can still change, so re-fetching is not wasted.

### Alternatives Considered

- **Reset to current period on refresh**: treats refresh as "show me now" - Rejected: silently loses the user's place; the dedicated return-to-current control already covers that intent.

### Consequences

**Positive:**
- Predictable refresh semantics; picks up late-arriving notes and stats for past days.

**Negative:**
- Refreshing immutable old periods re-queries the backend for data that rarely changes.

---

## Decision 10: Explicit inclusive start/end range parameters; reject mixed request forms

**Date**: 2026-06-11
**Status**: accepted (end-bound validation refined by Decision 15: end strictly before today)

### Context

The /history endpoint only accepts days=N anchored to today and cannot express a past period. Review flagged that the new request contract was assumed but unstated, that the span limit ("one full month") was untestable, and that an inclusive/exclusive fencepost mismatch between client and server could spuriously reject 31-day months.

### Decision

History requests accept an explicit start/end date range, inclusive on both ends, validated as: end ≥ start, end ≤ current Sydney date, inclusive span ≤ 31 days. The days=N form is retained unchanged; a request supplying both forms is rejected.

### Rationale

An explicit range is the only way to express an arbitrary past period without over-fetching and client-side filtering. Inclusive-both-ends matches how the table is keyed (YYYY-MM-DD dates) and makes a 31-day month exactly 31 days, eliminating the fencepost ambiguity. Rejecting mixed requests is clearer than a precedence rule that would mask client bugs. The 31-day cap covers any calendar week or month while bounding query cost, mirroring the existing days cap.

### Alternatives Considered

- **Over-fetch days=N and filter client-side**: no API change - Rejected: brittle, re-introduces the cache-bounding leak, cannot reach periods older than 31 days.
- **Precedence rule when both forms supplied** (range wins): lenient - Rejected: silently masks malformed client requests.

### Consequences

**Positive:**
- Any past week/month is addressable; testable validation; backward compatible.

**Negative:**
- Two request forms to maintain on the endpoint.

---

## Decision 11: HistoryQuery enum with protocol-extension default

**Date**: 2026-06-11
**Status**: accepted

### Context

The `FluxAPIClient` protocol has ~30 test-mock conformers. Adding a required method breaks them all; the codebase already evolved the protocol once via a defaulted method (`fetchStatus(simulateLoadWatts:)`). The new date-range request also needs to travel through the chart-expansion scope, so the request form wants to be a value type, not two separate method signatures at every layer.

### Decision

Introduce `HistoryQuery` (`.days(Int)` / `.dateRange(start:end:)`) in FluxCore and add `fetchHistory(query:)` to the protocol with a default implementation: `.days` delegates to the existing required `fetchHistory(days:)`; `.dateRange` throws `notConfigured`.

### Rationale

One Hashable value type carries the request through view model, API client, and `ChartScope`, guaranteeing every consumer fetches the same window (the data-consistency rule). The default keeps all existing mocks compiling; only mocks that exercise navigation implement the new method.

### Alternatives Considered

- **Second required method `fetchHistory(start:end:)`**: explicit - Rejected: breaks every existing mock and leaves the expansion scope without a single value to carry.
- **Widen `fetchHistory(days:)` to take optional dates**: one method - Rejected: optional-parameter soup; invalid combinations become representable.

### Consequences

**Positive:**
- No churn in existing mocks; expansion scope reuses the same type.

**Negative:**
- A mock that forgets to implement the new method fails at runtime (notConfigured) rather than compile time.

---

## Decision 12: Period anchor as separate view-model state, not a HistoryRange case

**Date**: 2026-06-11
**Status**: accepted

### Context

The displayed past period needs representing. `HistoryRange` is the segmented picker's tag type (Hashable, five fixed cases); embedding an anchor date in its cases would break picker equality and force every range consumer to handle anchored variants.

### Decision

`HistoryViewModel` holds `periodAnchor: Date?` — nil means the current to-date period; non-nil is the Sydney-midnight start of a past period. `HistoryRange` is unchanged. Period math lives in a new `HistoryPeriod` struct.

### Rationale

The picker and the navigation are orthogonal axes (which period type × which period instance); modelling them separately keeps the picker untouched and confines anchor handling to the view model and header. nil-as-current makes "is viewing past" and the range-switch reset (Decision 4) trivial.

### Alternatives Considered

- **`HistoryRange.week(anchor: Date?)` cases**: single source - Rejected: breaks segmented-picker tags and spreads anchor handling across every range consumer.
- **Separate `displayedPeriod` enum (.current/.past(HistoryPeriod))**: more explicit - Rejected: equivalent information to `Date?` with more ceremony; the period is derivable from (range, anchor, now).

### Consequences

**Positive:**
- Picker, chart-domain, and cards keep their existing range plumbing.

**Negative:**
- The (range, anchor) pair must be threaded through the load-coalescing loop together.

---

## Decision 13: Chart-expansion scope carries the HistoryQuery

**Date**: 2026-06-11
**Status**: accepted

### Context

Enlarging a History card spawns a window/cover that fetches its own data via `ChartScope.historyRange(days: Int)` anchored to today. While viewing a past period, the expanded chart would show the current period — violating the CLAUDE.md data-consistency rule.

### Decision

`ChartScope.historyRange` carries a `HistoryQuery` instead of a day count; cards build their expansion scope from the view model's current query. The expanded window therefore fetches and renders the same period, including its 60-second polling.

### Rationale

The consistency rule leaves no room for the expanded chart to disagree with the card it came from. Carrying the query is a mechanical change across six files and reuses the type introduced in Decision 11.

### Alternatives Considered

- **Disable expansion while viewing a past period**: smaller change - Rejected by user: a feature silently disappearing on navigation is worse than the plumbing.

### Consequences

**Positive:**
- Expanded charts always match the on-screen period.

**Negative:**
- Six expansion-related files change; polling re-fetches immutable past data (harmless, matches refresh semantics).

---

## Decision 14: Shared in-content navigation header on both platforms; period label triggers the picker

**Date**: 2026-06-11
**Status**: accepted

### Context

Day Detail splits its navigation by platform: in-content chevron header on iOS, window-toolbar chevrons on macOS. The History header must also host the period label (req 4.1), the picker trigger, and the conditional "Current" button.

### Decision

One `HistoryPeriodHeader` rendered in content on both platforms, styled after `DayNavigationHeader`: chevrons flanking a centred period label; tapping the label opens a graphical `DatePicker` (popover, sheet on compact iOS) capped at Sydney today; a "Current" button appears only when viewing a past period. macOS adds `←`/`→` via `onKeyPress`, matching Day Detail's keyboard affordance.

### Rationale

The picker-trigger label has to live in content regardless, so toolbar chevrons on macOS would duplicate controls across two locations for no requirement — req 1.7 only obliges the keyboard affordance. One component keeps the two platforms identical and the change surface small. Chosen by user (header below picker; label as picker trigger).

### Alternatives Considered

- **macOS window-toolbar chevrons (full Day Detail parity)**: closest pattern match - Rejected: duplicates navigation controls; the label/picker must be in content anyway.
- **Controls inside the Period overview card**: saves vertical space - Rejected by user: navigation hides when scrolled and mixes controls into card content.
- **Separate calendar icon button**: more discoverable - Rejected by user: one more control in the row; the label affords tapping.

### Consequences

**Positive:**
- One shared component; navigation always visible at the top of the screen.

**Negative:**
- Minor divergence from Day Detail's macOS toolbar placement.

---

## Decision 15: The date-range request form is past-only (end strictly before today)

**Date**: 2026-06-11
**Status**: accepted (refines Decision 10; requirement 5.6 amended accordingly)

### Context

Decision 10 validated range requests with `end ≤ today`. A range ending today would trigger the handler's live-today compute path — a surface the app never exercises, since the current period is always requested via `days=N`. Design review flagged this as untested, unrequired surface where consistency bugs hide.

### Decision

Range requests must have `end` strictly before the current Sydney date; `end == today` is rejected with a 400. The date-range path therefore never performs live compute or the readings query.

### Rationale

Every permitted input should have a consumer and a test. Making the range form past-only gives the handler two cleanly separated paths — `days=N` (may include live today, unchanged) and `start/end` (stored values only) — instead of a hybrid that req 5.3 would otherwise have to qualify.

### Alternatives Considered

- **Permit `end == today` and test the live path**: more general API - Rejected: tests a code path with no consumer; the two-user app gains nothing.

### Consequences

**Positive:**
- Simpler handler (range path skips two DynamoDB queries unconditionally); req 5.3 becomes unconditional.

**Negative:**
- A hypothetical future consumer wanting "this week including today" as a range must use `days=N` or relax the validation.

---

## Decision 16: Keep days=N and start/end as permanently distinct request forms

**Date**: 2026-06-11
**Status**: accepted

### Context

With the range form added, the question arose whether the app should migrate to start/end for all requests so days=N could eventually be removed from the backend, leaving one request form.

### Decision

Both forms stay, with distinct semantics: `days=N` means "window ending on the server's today" (mutable, may include live data); `start/end` means "this exact immutable past window". No migration or removal is planned from this spec; a separate ticket exists to revisit the idea later.

### Rationale

`days=N` anchors "today" server-side, so a request built just before midnight and processed just after stays internally consistent — with client-computed dates the same request would either silently render a current view missing today (window now ends "yesterday", a valid past range) or 400 on `end == today`. Migrating the current period to the range form would also force `end == today` to be permitted again, reintroducing the hybrid live-compute path Decision 15 removed. The eventual saving (~10 lines of param parsing) does not cover the migration's change surface.

### Alternatives Considered

- **Migrate app to start/end now, remove days=N later**: one request form eventually - Rejected: reverses Decision 15, reintroduces live compute on the range path, and creates a midnight clock-skew failure mode the current form cannot have.

### Consequences

**Positive:**
- Current-period requests remain self-correcting at the midnight boundary; the range path stays stored-values-only.

**Negative:**
- Two request forms to maintain and document on one endpoint.

---

## Decision 17: navigateNext clamps in the view model, not only via the disabled chevron

**Date**: 2026-06-12
**Status**: accepted

### Context

The design keyed the next-chevron's disabled state off the resolved snapshot (`isViewingCurrentPeriod`), which by design lags rendered data while a load is in flight. Post-implementation review showed that a second `navigateNext` arriving mid-load — a rapid double-tap, or macOS arrow-key repeat, which fires faster than a network round trip — could step past the current period and issue a future-dated range request. The server correctly 400s it, but the user lands in an error state whose Retry repeats the same bad request.

### Decision

`navigateNext()` guards `periodAnchor != nil` and is a no-op at the current period, mirroring `DayDetailViewModel`'s `isToday` clamp. The chevron's resolved-snapshot disabled state is presentation only.

### Rationale

UI disabled states are advisory under async load; the view model is the only place that can enforce the invariant against event timing. The sibling Day Detail pattern already clamps in the view model for the same reason.

### Alternatives Considered

- **Key the disabled state off requested state instead of resolved**: closes the race - Rejected: breaks the resolved-snapshot rule that the header reflects rendered data, and still leaves programmatic callers unguarded.
- **Have load() reject future ranges client-side**: catches the symptom - Rejected: the navigation intent is the wrong layer to let produce an invalid period in the first place.

### Consequences

**Positive:**
- Future-dated range requests are unreachable from the UI regardless of event timing; covered by no-op and mid-load double-tap tests.

**Negative:**
- The clamp duplicates, at the intent layer, a constraint the disabled chevron also expresses.

---
