# Decision Log: Stat Comparisons

## Decision 1: Use the full spec workflow

**Date**: 2026-05-10
**Status**: accepted

### Context

T-1161 ("Stat comparisons") asks for a Day Detail comparison of the selected day to the day before. Initial scope assessment identified ~200–400 LOC across 6–10 files (backend `/day` or `/history` consumer, FluxCore models, DayDetailViewModel, SummaryBlock, DayInFiveBlocksPanel, FluxStatRow, and tests) and several UX questions that would benefit from documented decisions.

### Decision

Run T-1161 through the full spec workflow (requirements → design → tasks) rather than smolspec.

### Rationale

The cross-stack reach, the multiple UX choices (which stats compare, today vs partial-day, direction colour semantics, visual treatment, missing-data fallback, delta vs %), and the user's eventual desire for additional comparison periods (last week now, last month / last year later) make smolspec too lightweight. A full spec lets the design phase consider whether to extend `/day`, reuse `/history`, or fan out client-side requests, and lets the tasks phase plan tests across both back-end and the iOS / macOS UI.

### Alternatives Considered

- **Smolspec**: Single-document lightweight workflow — Rejected because the visual treatment alone needs more than a paragraph and the open UX questions are not pre-decided.
- **Defer entirely**: Skip until more periods (month/year) are wanted — Rejected because Yesterday + 7 days ago is enough value on its own and unblocks the future periods incrementally.

### Consequences

**Positive:**
- All UX decisions captured in writing before coding starts.
- Design phase can compare backend approaches with full context.
- v1 stays narrow even though the architecture supports later periods.

**Negative:**
- More upfront process for what could be a one-day implementation if it stayed at "vs yesterday only".

---

## Decision 2: Feature name `stat-comparisons`

**Date**: 2026-05-10
**Status**: accepted

### Context

The Transit ticket title is "Stat comparisons". The repo uses kebab-case directory names under `specs/`.

### Decision

Use `stat-comparisons` as the feature name and `specs/stat-comparisons/` as the spec directory.

### Rationale

Matches the ticket title and is generic enough to absorb future periods (month / year) without renaming.

### Alternatives Considered

- **`day-detail-comparisons`**: More specific to the screen — Rejected because the feature is conceptually about comparing stats; the screen happens to be Day Detail today but the catalogue could move.
- **`vs-yesterday`**: Short — Rejected because v1 already exceeds yesterday-only; the name would lie.

### Consequences

**Positive:**
- Stable name across future expansions.

**Negative:**
- Slightly less specific than tying to the screen.

---

## Decision 3: v1 stat coverage — SummaryBlock and Five-Block panel only

**Date**: 2026-05-10
**Status**: accepted

### Context

Day Detail renders five comparison-eligible cards: SummaryBlock (Power), BatteryBlock, DayInFiveBlocksPanel, PeakUsageCard, and the per-day Note. We need to decide which carry deltas in v1.

### Decision

v1 adds inline deltas to the Day Detail SummaryBlock (Power) rows and to the DayInFiveBlocksPanel rows (per-block total kWh and per-block solar kWh on daylight blocks). BatteryBlock and PeakUsageCard are out of scope.

### Rationale

The Power and Five-Block panels carry the day's most-asked-about numbers (solar, house usage, grid, per-block usage) and both already have equivalent fields available on `DayEnergy` from `/history` for past dates, which keeps the data path simple. BatteryBlock's lowest-SoC timestamp is awkward to compare in absolute terms; PeakUsageCard's "top peak" rows aren't single scalars and would need bespoke comparison rules. They can be revisited once v1 is shipped.

### Alternatives Considered

- **Cover all four cards in v1**: Maximum value upfront — Rejected because it widens scope and forces design choices for asymmetric data (peak periods, lowest-SoC time).
- **SummaryBlock only**: Smallest possible v1 — Rejected because Five-Block usage is the second-most-glanced-at panel and the delta logic is essentially the same.

### Consequences

**Positive:**
- Smaller surface area for v1; simpler design and tests.
- Both panels share the same data source path.

**Negative:**
- Battery and peak-period comparisons land in a follow-up.

---

## Decision 4: Today's partial-day handling — full vs full

**Date**: 2026-05-10
**Status**: accepted

### Context

When the user views Today (an in-progress day), comparing partial totals against yesterday's full totals will look skewed early in the day (e.g. "Solar −80%" at 10am). Three options were considered: hide deltas entirely, truncate the comparison day to the same wall-clock cutoff, or compare full vs full.

### Decision

Compare today's in-progress totals against the comparison day's full totals as-is. No cutoff, no hiding.

### Rationale

The user explicitly chose this. Same-time-of-day cutoff requires per-row backend logic and a new comparison API; hiding deltas on Today removes the feature exactly when it's most useful (forecasting whether today is on track). The in-progress nature is implied by the Today label and the live-updating values.

### Alternatives Considered

- **Hide deltas on Today**: Avoids early-day confusion — Rejected for the reason above.
- **Same-time-of-day cutoff**: Most "fair" comparison — Rejected for backend complexity and because the user accepted the tradeoff.

### Consequences

**Positive:**
- No backend cutoff logic needed.
- Same data path for today and past days.

**Negative:**
- Deltas on Today look strange before mid-afternoon. Acceptable per user input.

---

## Decision 5: Delta format — signed absolute, no percentages

**Date**: 2026-05-10
**Status**: accepted

### Context

Three formatting options were considered: absolute only (e.g. `+1.2 kWh`), percent only (e.g. `+12%`), or both.

### Decision

Render deltas as a signed absolute difference in the row's native unit (kWh), formatted to one decimal place to match existing energy formatting.

### Rationale

Absolute values are unambiguous and consistent across rows. Percent deltas are misleading when the comparison value is near zero (e.g. solar at midnight). Showing both crowds the row width — small phones already crop the rightmost column.

### Alternatives Considered

- **Percent only**: Compact — Rejected because of near-zero ambiguity.
- **Absolute + percent**: Most info — Rejected for row crowding on small phones.

### Consequences

**Positive:**
- Stable formatting; no near-zero edge cases.
- Reuses existing `EnergyFormatting`.

**Negative:**
- Users wanting "% better than last week" don't get it in v1.

---

## Decision 6: Interaction model — Concept A (toggle + inline deltas)

**Date**: 2026-05-10
**Status**: accepted

### Context

The user asked for a toggleable, multi-period feature. Four interaction models were sketched: A (toggle + inline deltas, period chip in header), B (overlay sheet), C (header strip with top-N highlights), D (per-row long-press popover).

### Decision

Implement Concept A: a single Compare toggle near the top of Day Detail, a period chip beside it when on, and inline deltas trailing each supported row's primary value.

### Rationale

Concept A keeps the existing layout intact, is the smallest change to FluxStatRow / SummaryBlock, and matches the user's reading of "an overlay that can be toggled" (an opt-in layer over existing rows). It also degrades gracefully when the toggle is off — zero visual change. Concepts B/C/D either require richer per-card surgery or hurt discoverability.

### Alternatives Considered

- **Concept B (overlay sheet)**: Cleaner default UI but extra tap to access — Rejected for v1 because deltas should be visible at a glance.
- **Concept C (header strip with top-N)**: Always-visible summary but adds "top-N" ranking complexity — Rejected as overkill for v1.
- **Concept D (per-row long-press)**: Zero default chrome but discoverability is poor — Rejected.

### Consequences

**Positive:**
- Smallest UI surgery.
- Direct mapping from each FluxStatRow to its delta.

**Negative:**
- Inline deltas widen each row; small-phone width is the bottleneck for layout.

---

## Decision 7: v1 periods — Yesterday and 7 days ago

**Date**: 2026-05-10
**Status**: accepted

### Context

The user wants multiple comparison periods, with eventual support for last month and last year. v1 needs to pick a starting set.

### Decision

v1 supports two periods: "Yesterday" and "7 days ago". Last month and last year are deferred.

### Rationale

Both Yesterday and 7-days-ago are reachable from a single `/history?days=8` call relative to the selected day, or from already-cached data when navigating from History. Last month / last year would need either a wider history window (the `/history` 30-day TTL caps it for past years) or new endpoints — out of scope for v1.

### Alternatives Considered

- **Yesterday only**: Smallest v1 — Rejected because the user explicitly asked for a multi-period design.
- **Yesterday + 7 + 30 days ago**: Full spread within the 30-day window — Rejected because the 30-day-ago row is often missing for older selected days, which makes the period chip feel unreliable.

### Consequences

**Positive:**
- Both periods serve from existing data sources.
- Multi-period UI scaffolding lands in v1, ready to extend.

**Negative:**
- Doesn't scratch the eventual "last month / last year" itch.

---

## Decision 8: Direction colour — neutral chevron only

**Date**: 2026-05-10
**Status**: accepted

### Context

Two options for chevron / colour: per-stat "good vs bad" colouring (Solar ↑ green, Grid import ↑ red, etc.) versus a single neutral colour with chevron-only direction.

### Decision

Use a single neutral text colour for both the chevron and delta value. The chevron (▲ / ▼ / —) conveys direction only.

### Rationale

"Good vs bad" colouring encodes a value judgement that doesn't always hold (more house usage isn't inherently bad; off-peak grid import is essentially free; battery cycling is neither). Neutral colouring keeps the rule consistent across rows and avoids implying that the user did "well" or "badly".

### Alternatives Considered

- **Per-stat semantic colouring**: Conveys more meaning at a glance — Rejected for the reason above.

### Consequences

**Positive:**
- One colour token, one shared component path.
- No per-row colour rules to maintain.

**Negative:**
- Slightly less visual punch.

---

## Decision 9: Single global toggle, off by default, period persists

**Date**: 2026-05-10
**Status**: accepted

### Context

Decisions needed for: where the toggle lives (global vs per-card), the default state (on/off), and whether toggle and period persist.

### Decision

A single global Compare toggle near the top of Day Detail, off by default. The period selector value persists across launches even when the toggle is off; the toggle's on/off state also persists across launches.

### Rationale

A global toggle removes per-card state drift and simplifies the "everything on or everything off" mental model. Off-by-default keeps the screen uncluttered for users who don't care; persistent period means returning users don't re-pick. Per-device persistence (no iCloud sync) avoids cross-device surprises and matches how other screen-level prefs work.

### Alternatives Considered

- **Per-panel toggles**: More flexibility — Rejected for state-management cost and visual clutter.
- **On by default**: Faster discovery — Rejected because deltas widen rows; users should opt in.
- **No persistence (per-launch state)**: Simplest — Rejected because users likely keep the same period preference.

### Consequences

**Positive:**
- Single source of truth for the comparison state.
- Predictable across navigation and relaunches.

**Negative:**
- Users on multiple devices need to set the preference per device.

---

## Decision 10: Grid In rows assume the off-peak split is always present

**Date**: 2026-05-10
**Status**: accepted

### Context

SummaryBlock currently renders either a peak / off-peak split or a single combined Grid In row, depending on whether `offpeakGridImportKwh` is non-nil. The first review draft worried about cross-day shape mismatches (one day split, the other combined).

### Decision

Treat the Grid In peak / off-peak split as always present on both the selected day and the comparison day. The "combined" fallback row is not part of `SR` for v1.

### Rationale

Per the user, every day in production has the off-peak split because the site's off-peak window is fixed and the poller computes off-peak deltas every day. Codifying the assumption keeps `SR` a fixed list of five rows and removes a class of edge cases that would never fire in practice.

### Alternatives Considered

- **Compare on the lowest common row**: Drop peak/off-peak deltas when either side lacks the split — Rejected as unnecessary because the split is always present.
- **Synthesise the missing split**: Treat a missing comparison split as all-peak — Rejected because it would silently encode a wrong assumption.

### Consequences

**Positive:**
- `SR` is a fixed list; no per-row branching at the comparison layer.

**Negative:**
- If the off-peak window or schedule ever changes such that some days lack a split, this assumption needs revisiting.

---

## Decision 11: No Today affordance / caption

**Date**: 2026-05-10
**Status**: accepted

### Context

Decision 4 already accepted "full vs full" semantics for Today. The first review draft suggested adding a subdued caption ("Today so far vs full {period}") to soften the early-day weirdness.

### Decision

Do not add a caption. The Compare feature behaves identically on Today and past days; users learn the convention by use.

### Rationale

Adds no extra surface, no extra strings, no extra a11y rules. The deltas being computed against in-progress totals is implicit in the "Today" label and the live-updating values.

### Alternatives Considered

- **Subdued caption near the period chip**: Acknowledges the asymmetry — Rejected for added complexity with marginal value.
- **Hide deltas on Today**: Cleanest but reverses Decision 4 — Rejected.

### Consequences

**Positive:**
- One fewer string and one fewer rendering path.

**Negative:**
- Early-day deltas on Today look skewed; users have to learn this themselves.

---

## Decision 12: Charts are an explicit non-goal in v1

**Date**: 2026-05-10
**Status**: accepted

### Context

PowerChartView, BatteryPowerChartView, and SOCChartView dominate the Day Detail screen. The review noted users may reasonably expect "Compare" to also overlay yesterday's curves. Adding chart overlays would roughly double the design and test surface.

### Decision

v1 does not touch the three chart panels. Comparisons apply only to the SummaryBlock and DayInFiveBlocksPanel rows. A non-goal entry records this explicitly.

### Rationale

Chart overlays carry their own design problems (axis alignment, colour distinction, legend, missing-data gaps). Shipping the row-level deltas first lets us learn whether users want chart overlays before building them.

### Alternatives Considered

- **Overlay comparison day's curves on each chart**: Visually richer — Rejected for scope and complexity in v1.

### Consequences

**Positive:**
- Smaller, ship-able v1.

**Negative:**
- Compare toggle changes numbers but not charts; users may notice the inconsistency.

---

## Decision 13: One shared caption for all comparison-data failure modes

**Date**: 2026-05-10
**Status**: accepted

### Context

Three distinct failure modes can leave the screen without comparison data: the resolved date has no record, the comparison fetch fails with a network or backend error, or the resolved date precedes the earliest day available to the client. The first review draft only addressed the first.

### Decision

All three failure modes collapse to the same subdued caption near the period chip: "No comparison data available for {period}". The period chip itself stays interactive in all three cases.

### Rationale

Distinct strings ("offline", "beyond history") add localization, copy, and test surface for marginal user value. A single caption is easier to read, write, and maintain. Keeping the chip interactive lets the user switch periods without first dismissing the caption.

### Alternatives Considered

- **Separate captions per failure mode**: More accurate — Rejected for added complexity.
- **Single caption + disable chip option for known boundaries**: Combines option 1 with chip greying — Rejected because the user does not currently have an "earliest day" hint to drive that disable, and the caption already communicates the result.

### Consequences

**Positive:**
- One caption string; one rendering path.

**Negative:**
- Loses the (minor) information value of distinguishing offline from missing.

---

## Decision 14: Layout / accessibility / localization explicit ACs deferred

**Date**: 2026-05-10
**Status**: accepted

### Context

The reviews flagged Dynamic Type stacking, RTL chevron mirroring, iPad / macOS wide-screen layouts, and English-only copy as items the requirements left vague.

### Decision

Do not add explicit ACs for any of these in v1. Ship with the existing FluxStatRow inline layout, English-only copy, and best-effort behaviour at large Dynamic Type sizes and in RTL. Revisit in a follow-up if real users complain.

### Rationale

Each of these adds testing and design surface. The Day Detail screen has not previously needed RTL or wide-column layout work, and Dynamic Type has not been a complaint area. Codifying ACs ahead of evidence over-fits the spec to hypothetical concerns.

### Alternatives Considered

- **Add explicit ACs for Dynamic Type, RTL, iPad column variants, and localization**: More defensive — Rejected for scope.

### Consequences

**Positive:**
- Smaller v1 surface; faster ship.

**Negative:**
- A11y / RTL / iPad regressions, if any, surface only after release.

---

## Decision 15: Render deltas as a sub-line under the primary value, not inline

**Date**: 2026-05-10
**Status**: accepted, supersedes part of Decision 6

### Context

Decision 6 chose Concept A (toggle + inline deltas trailing each primary value). After drafting the requirements the user reconsidered: an inline trailing delta crowds the row width, and a sub-line under the value reads as a more useful overview at a glance. The five-block panel already uses the same lighter/smaller treatment for its time-range captions, which is a known-good visual reference.

### Decision

Render each delta as a right-aligned sub-line directly beneath the row's primary value, using the same lighter, smaller typography and `tertiaryText` colour already used for time-range captions on `DayInFiveBlocksPanel`. The toggle-and-period-chip header from Decision 6 is unchanged; only the per-row visual treatment moves.

### Rationale

The sub-line layout removes horizontal pressure on the value column (no need to fit chevron + signed kWh inline alongside the value), reuses an existing in-app typographic pattern the user has already approved aesthetically, and naturally accommodates two stacked deltas on the daylight five-block rows (one under solar, one under total).

### Alternatives Considered

- **Inline trailing delta (original Concept A)**: Tightest visual coupling — Rejected for row crowding and small-phone readability.
- **Delta rendered inside the row's existing label-side `sub` slot**: Reuses an existing slot — Rejected because that slot is already used for "paid" / "free" qualifiers on the Grid In rows.

### Consequences

**Positive:**
- More usable overview at a glance; no row-width crush.
- Reuses an established in-app typographic pattern.
- Daylight five-block rows can stack two deltas (under solar and under total) without inventing new layout.

**Negative:**
- Rows that show a delta become taller than rows that don't; the layout-stability rule has to address this asymmetry explicitly (which the requirements now do via Decision 16).

---

## Decision 16: Reserve sub-line height on every supported row when Compare is on

**Date**: 2026-05-10
**Status**: accepted

### Context

With sub-line deltas (Decision 15) and per-row fallback (when the comparison day's value for a given row is unavailable), the same card can contain rows with sub-lines and rows without. That makes the card height non-uniform and causes visible jitter as deltas appear during async comparison fetches or when the user changes period.

### Decision

When the Compare toggle is on, every row in `SR` and every value column in `FB` reserves the sub-line slot height regardless of whether that specific row currently renders a delta. Rows in fallback state render an empty sub-line slot at the same height as rows that show a delta. When the toggle is off, the sub-line slot disappears entirely and every row returns to its pre-feature height.

### Rationale

Reserving slot height makes the card's visual rhythm uniform within a Compare session, eliminates jitter during the async comparison fetch on day-navigation, and keeps the off-state visually identical to the pre-feature layout. The cost — a small empty band beneath fallback rows — is acceptable given the alternative was rows visibly resizing as deltas resolved.

### Alternatives Considered

- **Let row heights vary by per-row data availability**: More compact when many rows fall back — Rejected for the jitter and uneven rhythm during day-navigation.

### Consequences

**Positive:**
- Stable card height while Compare is on; no jitter during day-navigation or period change.
- Row/card layout is byte-identical to pre-feature when Compare is off (the screen as a whole gains the always-visible toggle row per AC 1.1).

**Negative:**
- Slightly more vertical real estate when Compare is on, even on rows with no delta to show.

---

## Decision 17: Reuse `apiClient.fetchDay` for the comparison day; do not extend the backend in v1

**Date**: 2026-05-10
**Status**: accepted

### Context

v1 supports two comparison periods reachable via a calendar offset from the selected day (Yesterday, 7 days ago). The comparison-day data we need (epv, eInput, eOutput, eCharge, eDischarge, offpeakGridImportKwh, dailyUsage) is exactly what `/day` already returns. `/history` is anchored to `now` (no end-date parameter — `internal/api/history.go:18-32`), so it is not directly usable for arbitrary "selected day − N" lookups.

### Decision

The iOS / macOS client issues a second `apiClient.fetchDay(date:)` request for the resolved comparison date when the Compare toggle is on. No backend changes; no new endpoint; no `/day?compare=` parameter; no `/history?endDate=` parameter. Cancellation and per-row fallback handle missing / failed responses.

### Rationale

Extending the backend for a UI feature whose data already exists in the response we'd otherwise duplicate is a poor trade. Past-date `/day` reads from `flux-daily-energy` without a readings query (`internal/api/day.go:115-146`), so the cost per request is minimal. Cancel-and-restart on rapid period / day-nav events keeps the wire traffic bounded.

### Alternatives Considered

- **Extend `/history` with an `endDate` parameter**: One round-trip covers both periods — Rejected because v1 only ever needs one comparison day at a time and the savings are nominal at site volume.
- **Add `/day?compare=yesterday|7d` server-side aggregation**: Cleaner client-side surface — Rejected for the same reason; also locks the "compare to" surface to dates rather than future aggregates.
- **Cache comparison responses across navigation**: Already-fetched dates could be re-served — Rejected for v1 to keep state simple; the existing per-day URL cache (if any) and SwiftData layer cover the wins anyway.

### Consequences

**Positive:**
- No backend changes; no new tests on the Go side.
- Comparison fetch failures are isolated from the primary day's render.

**Negative:**
- Two HTTP requests on every "Compare on" event instead of one richer response.
- Future periods that don't map to a single calendar day (e.g. "compare to 7-day average") would force a backend endpoint at that point. This is the inflection point — when (or if) v2 ships such a period, revisit this decision.

---

## Decision 18: Sub-line slot is a `SublineContent` enum, not an overloaded `String?`

**Date**: 2026-05-10
**Status**: accepted

### Context

The reserved-vs-rendered-vs-absent contract on the sub-line slot was initially modelled as `valueSub: String?` with the convention "nil = no slot, '' = reserved, non-empty = rendered". Reviewers flagged the triple meaning of `String?` as a footgun.

### Decision

Introduce `SublineContent` with three cases — `.hidden`, `.reserved`, `.text(String)` — and use it as the type of `FluxStatRow.valueSub` and the parameter consumed by `ValueSubline`. Default is `.hidden`, so existing 19 `FluxStatRow` callsites stay source-compatible without any change.

### Rationale

The three-state semantics are load-bearing for layout stability and accessibility correctness. Encoding them in a tagged union makes invalid states (e.g. `valueSub = ""` accidentally produced by string concatenation) unrepresentable at the type level, eliminating an entire class of bug from the per-row computation in `SummaryBlock` and `DayInFiveBlocksPanel`.

### Alternatives Considered

- **Keep `String?` with a documented convention**: Smallest API surface — Rejected because the convention can't be enforced by the compiler.
- **Two booleans (`reserved: Bool`, plus `text: String?`)**: Equivalent in expressiveness — Rejected for being more verbose at every callsite.

### Consequences

**Positive:**
- The "reserved slot during loading" state cannot be confused with the "delta resolved" state at the type level.
- `DeltaFormatter.sublineContent` returns `SublineContent` directly, so the row-builder code is a one-liner.

**Negative:**
- A small new public-ish type in the Compare module that other panels would have to import if they later opt into the same affordance.
