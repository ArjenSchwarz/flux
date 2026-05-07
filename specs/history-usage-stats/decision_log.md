# Decision Log: History Usage Stats

## Decision 1: Single overview card with eight tiles

**Date**: 2026-05-06
**Status**: accepted

### Context

The ticket lists eight headline stats to surface above the existing History charts. Layout choices were a single overview card with internal tiles, two cards (totals + day-records), or an inline strip above the range picker.

### Decision

Render one overview card with eight tiles, placed above the four existing chart cards.

### Rationale

A single card is consistent with the rest of the History screen, which already groups related numbers behind one `HistoryCardChrome`. Eight stats fit cleanly in a 2-column (iPhone) or 4-column (Mac/iPad) grid, and a single card keeps the chrome-to-content ratio reasonable. Splitting into two cards doubles the header chrome and adds visual fragmentation for no clear gain. An inline strip cannot fit eight stats on iPhone width.

### Alternatives Considered

- **Two cards (totals + day-records)**: Clearer grouping but doubles header chrome and produces a redundant title pair (`Period totals` + `Day records`).
- **Inline strip above the range picker**: Tighter on phone real estate but cannot fit eight labelled values without truncation.

### Consequences

**Positive:**
- One container to style, layout, and test.
- Visually consistent with existing History cards.

**Negative:**
- An eight-tile grid is the largest single card on the History screen; minor risk of feeling dense on the smallest iPhone.

---

## Decision 2: "Total used solar" means total solar collected (sum of epv)

**Date**: 2026-05-06
**Status**: accepted

### Context

The ticket lists "Total used solar". Three plausible interpretations exist: (a) sum of `epv` (gross solar generated), (b) `sum(epv) − sum(eOutput)` (solar that did not get exported), or (c) self-consumption with battery cycle credited back. The user clarified directly: "Basically just all of the solar that was collected."

### Decision

The "Total used solar" tile renders `sum(epv)` across complete days in the range, labelled "Total solar" so the value name matches what the number represents.

### Rationale

The user's intent is the gross solar number. Labelling the tile "Total solar" rather than "Total used solar" avoids misleading a reader who interprets "used" as self-consumption. The two share an identical computation, so the existing `solarTotalKwh` field in `PeriodSummary` is the source.

### Alternatives Considered

- **`sum(epv) − sum(eOutput)` ("self-consumed solar")**: Closer to the literal reading of "used", but contradicts the user's explicit clarification.
- **Self-consumption with battery cycle**: Requires charge-source attribution we do not currently have; out of scope.

### Consequences

**Positive:**
- Reuses the existing `solarTotalKwh` aggregate; no new computation.
- Aligns with how the existing Solar card already reports this number.

**Negative:**
- Tile label diverges slightly from the ticket wording ("Total solar" vs "Total used solar"). Documented here so the linkage is traceable.

---

## Decision 3: "Total usage" sums daily-usage block totals

**Date**: 2026-05-06
**Status**: accepted

### Context

"Total usage" could mean (a) sum of `dailyUsage.stackedTotalKwh` across complete days-with-blocks, (b) an energy-balance derivation `eInput + epv − eOutput − eCharge + eDischarge`, or (c) sum of `eInput` (grid imports only).

### Decision

Total usage is the sum of `dailyUsage.stackedTotalKwh` across complete days-with-blocks.

### Rationale

Reusing the existing `dailyUsageTotalKwh` aggregate guarantees that the overview card's "Total usage" reconciles with the Daily Usage card's per-day breakdown. The energy-balance formula would produce a different number that doesn't match what the chart sums to, which would confuse a reader cross-checking the two cards.

### Alternatives Considered

- **Energy-balance formula**: Works on every day even when `dailyUsage` is missing, but the resulting total would not equal the visible Daily Usage chart total. Rejected.
- **`sum(eInput)` (grid imports only)**: Underestimates badly on solar-heavy days; not what "total usage" means colloquially.

### Consequences

**Positive:**
- Reconciles 1:1 with the Daily Usage card.
- Reuses the existing `dailyUsageTotalKwh` field.

**Negative:**
- A complete day with no `dailyUsage` payload (a backend-shape regression) is excluded from the total. This matches the Daily Usage card's behaviour, so cross-card reconciliation still holds.

---

## Decision 4: "Average night" uses days-with-night-block as the denominator

**Date**: 2026-05-06
**Status**: accepted

### Context

The night-average can be divided by (a) the count of complete days that contributed a `night` block (presence-based denominator), (b) `dailyUsageDayCount` — every complete day-with-blocks regardless of whether a night block was present (the existing `dailyUsageAvgKwh` divisor), (c) the picker range size (7/14/30), or (d) total night-block hours (kW units instead of kWh).

The first review pass surfaced a contradiction between an earlier version of this decision (which said "same shape as `dailyUsageAvgKwh` divisor", i.e. option (b)) and the requirements text (which used option (a)). We resolved it by keeping option (a) and aligning the rationale.

### Decision

Average night = sum of `night`-block kWh / count of complete days that contributed a `night` block. The denominator is presence-based, **not** the existing `dailyUsageDayCount`.

### Rationale

Tile labels promise an average over nights, not an average over "all complete days with any block." Option (b) would deflate the displayed value during periods where some complete days have an off-peak-only or evening-only block shape (the partial-blocks shape `daily-derived-stats` already emits when off-peak boundaries can't be resolved). The `night` cohort matches what the label says.

### Alternatives Considered

- **Divide by `dailyUsageDayCount` (every complete day-with-blocks)**: Reuses an existing field, but mathematically dilutes the displayed kWh on sparse-blocks periods. Rejected as misleading.
- **Divide by 7/14/30**: Sparse-data weeks would further understate.
- **kWh per hour of night blocks (kW)**: Different unit from every other tile; mental-model break.

### Consequences

**Positive:**
- Matches what the tile label promises.
- Robust to sparse-block periods.

**Negative:**
- Adds a small new aggregate alongside the existing `dailyUsageAvgKwh` rather than reusing it.

---

## Decision 5: Day cohort matches existing card behaviour, with explicit asymmetry for Peak imports / Exported / Lowest SoC

**Date**: 2026-05-06
**Status**: accepted

### Context

Each tile needs an inclusion rule for today (a partial day) and for days missing required fields (no `offpeakGridImportKwh`, no `dailyUsage`, no `socLow`). Options were to mirror the existing `PeriodSummary` rules, to always exclude today, or to always include it.

### Decision

Mirror the existing `PeriodSummary` rules:
- Total solar, Total usage, Avg night, Most usage, Most solar — **complete days only** (today excluded). Same denominator philosophy as the existing Solar and Daily Usage cards.
- Peak imports, Exported — **days-with-offpeak in the range, today included when today has an off-peak record.** This matches the existing `peakImportTotalKwh` / `exportTotalKwh` / `gridDayCount` semantics in `Totals.addGrid`, which the Grid card already uses for its KPI. The asymmetry was confirmed during requirements review after a peer-review pass surfaced that the existing aggregates do not gate on `!isToday`.
- Lowest SoC — **all days-with-low in the range, today included.** `socLow` is already a final-or-running floor, so today's value is meaningful even mid-day.

### Rationale

Consistency with the existing Solar / Grid / Battery / Daily Usage cards means the overview tile values will reconcile 1:1 with each chart card's existing KPI. Always-exclude makes today look invisible on the Grid card cohort and contradicts existing semantics; always-include skews complete-day averages. The asymmetry is documented explicitly in the Definitions section of `requirements.md` so a reader doesn't have to chase it through ACs.

### Alternatives Considered

- **Always exclude today**: Forces a different filter than the chart cards; reconciliation breaks.
- **Always include today**: Today's eight-hour partial would tank averages and skew totals.

### Consequences

**Positive:**
- Numbers reconcile across cards on the same screen.
- No new `isToday` branching beyond the one `DateFormatting.isToday(_:now:)` already gates.

**Negative:**
- "Lowest SoC" pointing at today is informationally fine but visually introduces a "today" date stamp that could be mistaken for a peek into live data. The existing `socLow` semantics already handle this in `BatteryHeroView`.

---

## Decision 6: Day-record tiles select the day in the existing charts

**Date**: 2026-05-06
**Status**: accepted

### Context

The three "day-of" records (`Most usage`, `Most solar`, `Lowest SoC`) point at a specific date. We could leave them display-only or wire them through `HistoryViewModel.selectDay(_:)` so tapping them highlights the day across the four chart cards and shows its summary card.

### Decision

Day-record tiles SHALL be tap targets that call the same `onSelect: (String) -> Void` plumbing the chart cards already use.

### Rationale

The selection plumbing is already there and shared by every chart card. Adding it to the tiles costs almost nothing and makes the records discoverable as navigation, not just numbers.

### Alternatives Considered

- **Display-only**: Simpler but loses the obvious next step ("show me that day").

### Consequences

**Positive:**
- Reuses an existing affordance; no new selection model.
- Records become a pointer into the rest of the screen rather than dead text.

**Negative:**
- Tapping `Lowest SoC` selects a day in the History stack that the per-day summary card may then surface even if the day is today, which may be redundant with the Dashboard. Acceptable; the user explicitly opted in.

---

## Decision 7: Em-dash placeholder for missing values

**Date**: 2026-05-06
**Status**: accepted

### Context

When a tile's cohort is empty (e.g. no day-with-offpeak in the range), the tile needs a fallback. Options were em-dash, hide-the-tile, or "0 kWh".

### Decision

Render the tile with an em-dash (`"—"`) value. Tile remains visible and laid out.

### Rationale

A stable layout across ranges matters more than reclaiming space in sparse cases. An em-dash visibly distinguishes "no data" from "zero", which a literal `0 kWh` cannot. Hiding tiles would shift the grid layout as data fills in.

### Alternatives Considered

- **Hide the tile**: Layout shifts; harder to scan ranges side-by-side.
- **Show "0 kWh"**: Misleading — a real zero (a complete day with no peak imports) is meaningfully different from "we have no data yet".

### Consequences

**Positive:**
- Stable grid; comparable layout across 7/14/30.
- Clear semantic distinction from a real zero.

**Negative:**
- An entire empty card on a brand-new install will show eight em-dashes, which is correct but visually sparse. Mitigated by [1.4](requirements.md#1.4) which hides the card entirely on the screen-level empty state.

---

## Decision 8: Day-record tie-break is uniformly "most recent"

**Date**: 2026-05-06
**Status**: accepted

### Context

Three tiles point at a specific date: `Most usage`, `Most solar`, `Lowest SoC`. When two days carry the same record value, the tile must pick one. For maxima, "most recent" is the obvious answer (it points at the latest day that hit the high-water mark). For a minimum (`Lowest SoC`), there's a defensible argument that "earliest" — the day the floor was first hit — is more meaningful than the latest matching day. The alternative is per-tile rules.

### Decision

All three day-record tiles tie-break by selecting the **most recent** date.

### Rationale

A uniform rule is simpler to explain, simpler to test, and matches the user's likely scrubbing pattern (scanning the chart from oldest on the left to newest on the right; the rightmost matching bar is the one the eye lands on). The ambiguity for a minimum is real but small in practice — `socLow` floats precision means exact ties at the same percent are rare, and `Lowest SoC` rounds to whole percents for display anyway.

### Alternatives Considered

- **Earliest for `Lowest SoC`, most recent for the two maxima**: Per-tile rule justified by "first hit" semantics for minima. Rejected as cognitively heavier than the rule it would replace.
- **Earliest for all three**: Symmetric but loses the "most recent record" framing that makes the maxima feel current.

### Consequences

**Positive:**
- One rule to test, one rule to remember.

**Negative:**
- A `Lowest SoC` tie that points at a recent day rather than the historical first-hit date is acceptable noise.

---

## Decision 9: Total solar tile reuses `solarTotalKwh` field directly (extends Decision 2)

**Date**: 2026-05-06
**Status**: accepted

### Context

Decision 2 already established that the ticket's "Total used solar" tile renders `sum(epv)`. During requirements review the relationship to the Solar card's existing KPI was made explicit: the Solar card's `solarTotalKwh` is the same `sum(epv) across complete days` value, and the Solar card's "X kWh/day average" subtitle uses `solarDayCount` which equals `completeDayCount`. The overview tile reads the same field.

### Decision

The Total solar tile reuses `PeriodSummary.solarTotalKwh` directly. No parallel re-summation. The tile's KPI value will equal the Solar card's KPI value to the last digit.

### Rationale

Avoids two slightly-different-looking numbers on the same screen. Reusing the existing aggregate also keeps the cohort rule (complete days) in one place.

### Consequences

**Positive:**
- Visible reconciliation between the two cards' Solar numbers.

**Negative:**
- None.

---

## Decision 10: SoC display rounded half-up to nearest whole percent

**Date**: 2026-05-06
**Status**: accepted

### Context

`socLow` is a `Double` (e.g. `11.7`). The Lowest SoC tile displays an integer percent. The rounding rule was unspecified; Swift's `Int(_:)` truncates, which means `11.9` would render as `11%`. That is potentially misleading for a floor where users care about whether they hit a critical threshold.

### Decision

Round `socLow` half-up to the nearest whole percent before rendering. `11.5 → 12%`, `11.4 → 11%`. Tie-break for the Lowest SoC record (per [Decision 8](#decision-8-day-record-tie-break-is-uniformly-most-recent)) operates on the raw `Double` value, not on the rounded display integer, so display rounding cannot perturb which day the tile points at.

### Rationale

Matches the rounding convention most users expect from displayed percentages and avoids under-reporting a near-threshold floor.

### Implementation note

`NumberFormatter.roundingMode` defaults to **`.halfEven`** (banker's rounding) on Apple platforms, not half-up. For half-up behaviour without `NumberFormatter`, use `Int(socLow.rounded(.toNearestOrAwayFromZero))` for non-negative values; or set `formatter.roundingMode = .halfUp` explicitly. The previous version of this decision incorrectly stated `NumberFormatter`'s default was half-up.

### Consequences

**Positive:**
- Predictable, conventional rounding.
- Tie-break on raw `Double` keeps record selection stable across rounding boundaries.

**Negative:**
- The displayed integer can disagree with the `socLowTime` precision implied by the underlying float (e.g. a true `socLow` of `11.5` showing as `12%`). The trailing `at HH:mm` line keeps the precise moment visible regardless.

---

## Decision 11: PeriodSummary exposes nested record structs, not tuples

**Date**: 2026-05-07
**Status**: accepted

### Context

The four new aggregates (Avg night, Most usage, Most solar, Lowest SoC) need a Swift representation. Options: anonymous tuples (`(dayID: String, kwh: Double)?`), named tuples per field, or two small nested struct types. The first review pass deliberately deferred this to design.

### Decision

Two nested record structs: `DayKwhRecord` (used by `mostUsageDay`, `mostSolarDay`) and `LowestSocRecord` (carries `socLowTime` in addition to `soc`). Avg night uses the same scalar-pair pattern (`nightTotalKwh`, `nightBlockDayCount`) the existing aggregates already use, with a computed `nightAvgKwh: Double?`.

### Rationale

Anonymous tuples produce hard-to-read test assertions (`summary.mostUsageDay?.0`) and `Equatable` conformance is awkward for tests that diff fixtures. Named structs are self-documenting and round-trip cleanly through SwiftUI bindings if a future card ever wants the record exposed directly. Two structs (not one) avoids a meaningless optional `time: String?` on the kWh records that would be nil 2/3 of the time.

### Alternatives Considered

- **Tuples**: Compact but less readable; `Equatable` conformance through synthesis works, but call-site readability suffers.
- **Single record struct with `time: String?`**: Fewer types, but the time slot is meaningless for the kWh records and obscures intent.

### Consequences

**Positive:**
- Test assertions read naturally (`summary.mostUsageDay?.kwh`).
- New types stay confined to `HistoryDerivedState.swift`.

**Negative:**
- Two new public-ish types, slightly more surface area than tuples.

---

## Decision 12: KPI slot shows the inclusive date range covered

**Date**: 2026-05-07
**Status**: accepted

### Context

User requested that the card chrome's KPI slot not duplicate the range picker (which shows "7d / 14d / 30d" already). Options: leave the slot empty, show a different value, hide the slot entirely.

### Decision

Show the inclusive date range covered by the response (e.g. `"Apr 30 – May 6"`), formatted Sydney time. When the response is empty the slot is hidden (`HistoryCardChrome.kpi` becomes optional and renders only when non-nil).

### Rationale

The range picker shows "30 days" but not which 30 days — the date range adds information the user actually has to work to derive otherwise. It's also cheap: the Solar series already has every day's parsed date, so the formatter reads `entries.first?.date` and `entries.last?.date`.

### Alternatives Considered

- **Empty / no KPI**: Wastes the slot; the chrome still draws header padding.
- **Date count summary** ("7 days, 5 with data"): Useful but breaks parity with the picker label and is harder to keep readable.
- **Headline number from one of the eight tiles**: Promotes one tile to KPI status arbitrarily; inconsistent with the "overview" framing.

### Consequences

**Positive:**
- KPI carries information not duplicated elsewhere on the screen.
- `HistoryCardChrome.kpi` becoming optional is a backward-compatible change (Swift defaulted parameters).

**Negative:**
- Minor: requires the card to receive `entries` in addition to `summary`. Not a problem because the existing chart cards already receive both.

---

## Decision 13: Layout — `LazyVGrid`, two columns on iOS compact, four columns elsewhere

**Date**: 2026-05-07
**Status**: accepted

### Context

The requirements specified "two columns on compact width, four columns on regular/wide" but lifted breakpoint pixels and grid mechanics to design. Options: `Grid` (force row alignment), `LazyVGrid` (flow into columns), `GeometryReader` width-reading, or `ViewThatFits` between two layouts.

### Decision

`LazyVGrid` with `count: columnCount` columns. Column count derived from `horizontalSizeClass` on iOS (compact → 2, regular → 4) and fixed at 4 on macOS.

### Rationale

`LazyVGrid` matches the eight-tiles-flowing-in-rows shape exactly. The reading order (2.1 → 2.8) maps to row-major order in either 2-column or 4-column form, so no per-row markup is needed. `Grid` would require explicit `GridRow` blocks and lose the flow flexibility. `GeometryReader` adds a layout pass and is unnecessary because `horizontalSizeClass` already discriminates iPhone portrait from iPad regular and from a wide Mac window. On macOS a fixed 4 is acceptable: the default Mac window is wide, and a user shrinking to 2-column-equivalent width is rare enough that a slightly squeezed 4-column layout is not worth a `GeometryReader`.

### Alternatives Considered

- **`Grid` with `GridRow`**: Forces explicit per-row markup; doesn't add value over `LazyVGrid` here.
- **`GeometryReader` + width breakpoint**: Adds layout pass; only buys correctness in a narrow window-resize case.
- **Fixed 2 columns everywhere**: Wastes Mac / iPad width.
- **Fixed 4 columns everywhere**: iPhone portrait would render four tiles at ~85 pt each — unreadable.

### Consequences

**Positive:**
- Simple `Array(repeating: GridItem(.flexible()), count: ...)` — no custom layout.
- Reading order is row-major in both layouts.

**Negative:**
- A user resizing a Mac window very narrow gets four cramped columns instead of a 2-column reflow. Acceptable; not a primary use case.
- iPad in 1/3 split view (`horizontalSizeClass == .regular` at ~320 pt detail-column width) renders four columns at ~68 pt each — labels truncate. Documented as a known limitation; the `ViewThatFits` fallback is deferred unless real users hit this.

---

## Decision 14: `HistoryCardChrome.kpi` becomes optional

**Date**: 2026-05-07
**Status**: accepted

### Context

Decision 12 specified the new card's KPI slot would show the inclusive date range, hidden when the response is empty. Implementing that requires `HistoryCardChrome.kpi` to be optional — the chrome currently takes `kpi: String` (always rendered). The chrome is used by the four existing chart cards, which all pass non-nil strings.

### Decision

`HistoryCardChrome.kpi` parameter becomes `String?` with a default of `nil`. When nil, the chrome's header row renders only the title (the trailing `Text` for the KPI is conditional). The four existing call sites continue to pass non-nil strings — Swift's implicit `String → String?` wrapping covers the existing call shape, and behaviour is unchanged for them.

### Rationale

The alternative was keeping `kpi: String` and passing an empty string, which still renders an invisible-but-allocated `Text` and sits awkwardly with downstream changes (e.g. monospaced-digit modifier on an empty `Text`). Optional-with-default is the cleaner contract and is source-compatible.

### Alternatives Considered

- **Pass empty string**: works but leaves empty-text logic implicit at every call site.
- **Add a separate `HistoryCardChromeNoKPI` variant**: too much surface area for one feature.
- **Give the new card its own bespoke chrome**: drift from the existing four cards' visual treatment.

### Consequences

**Positive:**
- Source-compatible across all four existing call sites.
- Future cards that don't have a natural KPI can omit the parameter cleanly.

**Negative:**
- One existing struct grew a new optional parameter; trivial drift in the public API of the chrome.

---
