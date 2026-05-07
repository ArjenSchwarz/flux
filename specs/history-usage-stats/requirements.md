# Requirements: History Usage Stats

## Introduction

The History screen renders four chart cards (Solar, Grid, Battery, Daily Usage) for the selected 7 / 14 / 30-day range but never surfaces the period-wide headline numbers — totals, averages, and which day stood out — in one place. This feature adds a single overview card above those charts that shows eight read-at-a-glance period stats, computed from the `/history` response already on hand. No backend or wire-format change is required.

## Non-Goals

- Any backend, FluxCore model, Lambda, poller, or DynamoDB change — every stat is derivable from the existing `DayEnergy` fields.
- Adding new range options. The feature consumes whatever range the existing 7 / 14 / 30 picker selects.
- Surfacing period stats on Dashboard, Day Detail, widgets, or watchOS.
- Replacing or restyling the existing Solar / Grid / Battery / Daily Usage cards. Subtitles on those cards may continue to repeat a number that also appears on the overview card.
- Drill-in on tiles other than the three "day-of" records (totals and averages are read-only).
- Period-over-period deltas ("vs previous 7 days"), forecasts, or trend arrows.
- Energy-cost / dollar conversions.
- Charts or sparklines inside tiles — tiles are number + label only.
- Copy-to-clipboard, share-sheet, or export of the overview values.
- Per-tile help tooltips or info popovers.

## Definitions

- **complete day**: a day in the range whose date is strictly before today, resolved through `DateFormatting.isToday(_:now:)` (the same boundary every other History aggregate uses).
- **day-with-blocks**: a day whose `DayEnergy.dailyUsage` is present and has at least one block (per `history-daily-usage` spec).
- **day-with-offpeak**: a day whose `DayEnergy.offpeakGridImportKwh` is non-nil. Days without an off-peak record are excluded from the Peak imports and Exported tiles because the off-peak / peak split cannot be derived without it.
- **day-with-night-block**: a day-with-blocks whose blocks include a `night` block.
- **day-with-low**: a day whose `DayEnergy.socLow` is non-nil.
- **stat tile**: one labelled value rendered inside the overview card.

Today inclusion across the eight tiles:
- **Lowest SoC** includes today via the `socLow` semantics already in `DayEnergy`.
- **Peak imports** and **Exported** include today when today has an off-peak record, because they reuse the existing `peakImportTotalKwh` / `exportTotalKwh` aggregates which do not gate on `!isToday`.
- All other tiles (Total usage, Total solar, Avg night, Most usage, Most solar) exclude today via the same `DateFormatting.isToday(_:now:)` boundary the rest of `PeriodSummary` already applies.

## 1. iOS / macOS: Overview Card Placement and Chrome

**User Story:** As a Flux user, I want a single overview card at the top of the History screen that summarises the selected range, so that I can read period-wide totals and stand-out days without scrolling through the charts.

**Acceptance Criteria:**

1. <a name="1.1"></a>The History screen SHALL render an overview card above the Solar card, below the 7 / 14 / 30 range picker, and below any active note row.  
2. <a name="1.2"></a>The card SHALL use the existing `HistoryCardChrome` container (matching the other History cards) with a fixed title `"Period overview"`. The card SHALL NOT duplicate the active range (e.g. "7 days" / "14 days" / "30 days") in its KPI or subtitle slots, since the range picker immediately above the card already shows it. Whether the chrome's KPI slot is empty, repurposed for a different value (such as the inclusive date range covered, e.g. `"Apr 30 – May 6"`), or hidden entirely is a design-phase decision; the only requirement is no duplication of the picker.  
3. <a name="1.3"></a>The card SHALL contain eight stat tiles, in the layout and order defined in [§2](#2-ios--macos-stat-tile-content). Tile order SHALL NOT change when individual tiles render an em-dash placeholder.  
4. <a name="1.4"></a>WHEN the History screen is in its empty or error state (no days returned and no cached fallback), the overview card SHALL NOT render. The existing `emptyState` / `errorState` block already replaces the chart cards in this case; the overview card SHALL share that gating.  

## 2. iOS / macOS: Stat Tile Content

**User Story:** As a Flux user, I want each tile to show a clearly-labelled, well-defined number, so that I can compare ranges without guessing what is being counted.

**Acceptance Criteria:**

The eight tiles SHALL be defined as follows. Each tile renders a label and a value. Day-record tiles additionally render the date the value belongs to.

| # | Tile label | Value | Cohort |
|---|---|---|---|
| 2.1 | Total usage | `sum(dailyUsage.stackedTotalKwh)` | complete days-with-blocks |
| 2.2 | Total solar | `sum(epv)` | complete days |
| 2.3 | Exported | `sum(eOutput)` | days-with-offpeak in range (today included when it has an off-peak record) |
| 2.4 | Peak imports | `sum(eInput − offpeakGridImportKwh)`, each summand clamped ≥ 0 | days-with-offpeak in range (today included when it has an off-peak record) |
| 2.5 | Avg night | `sum(night-block kWh) / count(days-with-night-block)` | complete days-with-night-block |
| 2.6 | Most usage | `max(dailyUsage.stackedTotalKwh)` | complete days-with-blocks |
| 2.7 | Most solar | `max(epv)` | complete days |
| 2.8 | Lowest SoC | `min(socLow)` | days-with-low (today included if it has a `socLow`) |

1. <a name="2.1"></a>**Total usage** — Label `"Total usage"`. Value formatted via `HistoryFormatters.kwh`. Tile renders an em-dash `"—"` for the value WHEN the cohort is empty.  
2. <a name="2.2"></a>**Total solar** — Label `"Total solar"`. Value formatted via `HistoryFormatters.kwh`. The cohort and value match the existing Solar card's KPI exactly. Per Decision 2, this tile fulfils the ticket's "Total used solar" item; see decision log for the label change rationale. Tile renders `"—"` WHEN the cohort is empty.  
3. <a name="2.3"></a>**Exported** — Label `"Exported"`. Value formatted via `HistoryFormatters.kwh`. The cohort matches the Grid card's bar set. Tile renders `"—"` WHEN no day-with-offpeak exists in the range.  
4. <a name="2.4"></a>**Peak imports** — Label `"Peak imports"`. Value formatted via `HistoryFormatters.kwh`. The cohort matches the Grid card's bar set; reuses the existing `peakImportTotalKwh` aggregate (whose per-day `max(0, eInput − offpeakImport)` clamp is already applied at entry construction in `gridEntry`, so summation does not re-clamp). WHEN every day-with-offpeak in the range has zero peak imports, the tile SHALL render the formatted zero value (whatever `HistoryFormatters.kwh(0)` returns) — not em-dash, since a real zero is meaningfully distinct from "no data". WHEN no day-with-offpeak exists in the range, the tile SHALL render `"—"`.  
5. <a name="2.5"></a>**Avg night** — Label `"Avg night"`. Value formatted via `HistoryFormatters.kwh`. The denominator counts only days-with-night-block, not every complete day-with-blocks. Negative payload `totalKwh` values are clamped to zero before summing, matching the rule used by the Daily Usage card. Tile renders `"—"` WHEN no day-with-night-block exists.  
6. <a name="2.6"></a>**Most usage** — Label `"Most usage"`. Value formatted via `HistoryFormatters.kwh`, additionally rendering the date the value belongs to using `"MMM d"` Sydney-time formatting. Tie-break: the most recent date wins. Tile renders `"—"` WHEN the cohort is empty.  
7. <a name="2.7"></a>**Most solar** — Label `"Most solar"`. Value formatted via `HistoryFormatters.kwh`, additionally rendering the date per [2.6](#2.6). Tie-break: the most recent date wins. Tile renders `"—"` WHEN the cohort is empty.  
8. <a name="2.8"></a>**Lowest SoC** — Label `"Lowest SoC"`. Value rendered as a whole percent (`"12%"`), rounded half-up from the floating-point `socLow`. Tile additionally renders the date per [2.6](#2.6); IF `socLowTime` is non-nil for that day, the tile SHALL append `" at HH:mm"` Sydney time after the date (e.g. `"Apr 26 at 06:14"`). Tie-break: the most recent date wins (uniform with [2.6](#2.6) and [2.7](#2.7)). The tie-break SHALL operate on the raw `Double socLow` value, not on the post-rounding integer percent, so that two days within 0.5 of each other do not produce an unstable choice driven by display rounding. Tile renders `"—"` WHEN no day-with-low exists.  

## 3. iOS / macOS: Tap-to-Select Behaviour

**User Story:** As a Flux user, I want tapping a day-record tile to highlight that day in the existing charts and reveal its summary, so that I can drill into the day a record points at without scrolling and hunting for it.

**Acceptance Criteria:**

1. <a name="3.1"></a>The Most usage, Most solar, and Lowest SoC tiles SHALL be tap targets when their value is non-em-dash. Tapping SHALL invoke the same `onSelect: (String) -> Void` plumbing the chart cards already use, passing the underlying day's `dayID` so `HistoryViewModel.selectDay(_:)` runs and the existing chart-card highlight rectangles update. Tapping the Lowest SoC tile when its record day is today SHALL still call `selectDay(_:)` with today's `dayID`; the selection routes through `HistoryViewModel` and the existing per-day summary card renders the today-row. Highlight rectangles render only on chart cards whose entry list contains today (Solar and Battery always include today; Grid and Daily Usage include today only when their per-card data conditions are satisfied) — partial chart-card highlight is acceptable behaviour, not a defect.  
2. <a name="3.2"></a>Tiles whose value is em-dash SHALL NOT be tap targets and SHALL NOT show a highlight affordance.  
3. <a name="3.3"></a>The Total usage, Total solar, Exported, Peak imports, and Avg night tiles SHALL NOT be tap targets — these values are not bound to a single day.  
4. <a name="3.4"></a>Tappable tiles SHALL expose the same press-state visual treatment the other tappable affordances on the History screen use (e.g. the existing `View day detail` row in the per-day summary card). No bespoke highlight affordance.  

## 4. iOS / macOS: ViewModel Derivation

**User Story:** As the History view, I want `HistoryViewModel.derived` to expose the overview-card values, so that the card body does no aggregation in its `body` and tests can drive the values directly.

**Acceptance Criteria:**

1. <a name="4.1"></a>`HistoryViewModel.PeriodSummary` SHALL expose, for each of the eight tiles in [§2](#2-ios--macos-stat-tile-content), enough information for the card to render the value, format, em-dash placeholder, and (for day-record tiles) date / time without re-walking the `DayEnergy` array. The specific accessor shape for each tile is a design-phase decision; the only contract is that test fixtures (a)–(n) in [§7.1](#7.1) can assert against the exposed surface directly (i.e. there must be a value-or-absent representation per tile that tests can read).  
2. <a name="4.2"></a>WHEN the cohort for a tile is empty per the rules in [§2](#2-ios--macos-stat-tile-content), the corresponding `PeriodSummary` representation SHALL allow the card to detect that condition without inspecting raw counts (e.g. via an optional accessor or a sentinel that maps to em-dash one-to-one with the [§2](#2-ios--macos-stat-tile-content) rules).  
3. <a name="4.3"></a>The Total solar, Exported, Peak imports, and Total usage tiles SHALL be sourced from the existing `solarTotalKwh`, `exportTotalKwh`, `peakImportTotalKwh`, and `dailyUsageTotalKwh` aggregates respectively (no parallel re-summation of the same fields). The per-day clamp for `peakImportTotalKwh` is applied at entry construction in `gridEntry`, so reuse already includes the clamp.  
4. <a name="4.4"></a>The aggregates introduced for tiles [2.5](#2.5) – [2.8](#2.8) (Avg night, Most usage, Most solar, Lowest SoC) SHALL be (re)computed once per `days` update, in the same single pass that already builds the solar / grid / battery / daily-usage series. AC [4.3](#4.3) already covers the four reused aggregates.  

## 5. iOS / macOS: Layout

**User Story:** As a Flux user on the smallest supported iPhone and on a wide Mac window, I want the eight tiles to fit cleanly without overflow or excessive whitespace, so that the overview card stays readable across both surfaces.

**Acceptance Criteria:**

1. <a name="5.1"></a>The card body SHALL render the eight tiles in two columns on compact-width surfaces (iPhone portrait, narrow Mac windows) and in four columns on regular-width surfaces (iPad regular width, wide Mac windows). The reading order SHALL be left-to-right, top-to-bottom in the order defined by [§2](#2-ios--macos-stat-tile-content) (2.1 first, 2.8 last).  
2. <a name="5.2"></a>Tile heights within a row SHALL be uniform, so a record tile with a trailing date / time line does not make the adjacent total-tile shorter than its neighbours.  
3. <a name="5.3"></a>The label SHALL be visually de-emphasised relative to the value (smaller / secondary colour); the trailing date / time line on day-record tiles SHALL be visually de-emphasised further. Specific font tokens, padding, and the breakpoint between two- and four-column layouts are design-phase decisions.  

## 6. iOS / macOS: Accessibility

**User Story:** As a VoiceOver user, I want each tile to be announced as a single combined element with its label, value, and (where applicable) date, so that I do not have to scrub through label-and-value as separate stops.

**Acceptance Criteria:**

1. <a name="6.1"></a>Each non-em-dash tile SHALL expose itself as one accessibility element whose label combines the tile's name, value, and (for day-record tiles) the date / time line. Energy units SHALL be spelled out (`"kilowatt hours"`) rather than rendered as the symbol; SoC SHALL be spelled as `"percent"`.  
2. <a name="6.2"></a>Em-dash tiles SHALL expose a label of the form `"{tile name}, no data"`.  
3. <a name="6.3"></a>Tappable tiles ([3.1](#3.1)) SHALL expose `accessibilityTrait .isButton` and an `accessibilityHint` of `"Selects this day in the charts below."`.  

## 7. Testing

**User Story:** As the project maintainer, I want the new ViewModel aggregates and the tile rendering helpers covered by unit tests, so that the eight definitions stay aligned with how the existing cards interpret the same data.

**Acceptance Criteria:**

1. <a name="7.1"></a>`HistoryViewModel` tests SHALL cover, as separate fixtures: (a) empty `days` array (every tile cohort empty); (b) only-today in the range (every complete-day cohort empty; Lowest SoC populated when today has a `socLow`, asserted explicitly); (c) all complete days with full data (every tile populated, hand-computed expected values asserted); (d) one complete day-with-blocks where `dailyUsage` is the only payload field present (Total usage, Most usage, Avg night non-nil; complete-day-only fields zero-or-nil per [§2](#2-ios--macos-stat-tile-content)); (e) two days tie for `mostUsageDay` (later date wins); (f) two days tie for `mostSolarDay` (later date wins); (g) two days tie for lowest `socLow` (later date wins); (h) at least one complete day-with-blocks lacks a `night` block (excluded from the [2.5](#2.5) denominator); (i) one day's payload `night.totalKwh` is negative (clamp ≥ 0 before summing per [2.5](#2.5)); (j) one day's `peakGridImportKwh` payload is negative (clamp ≥ 0 before summing per [2.4](#2.4)); (k) every day in the range lacks `offpeakGridImportKwh` (Peak imports and Exported tiles' cohort empty → em-dash); (l) at least one day-with-offpeak exists but every day's `peakGridImportKwh` is exactly zero (Peak imports tile renders `"0.0 kWh"`, not em-dash, per [2.4](#2.4)); (m) one day where `dailyUsage` is present but `offpeakGridImportKwh` is nil (mixed cohort: contributes to Total usage / Avg night / Most usage but not Peak imports / Exported); (n) `socLow` boundary at `11.5` (rounds to `12%`) and `11.4` (rounds to `11%`) per [2.8](#2.8).  
2. <a name="7.2"></a>Tile rendering tests SHALL assert the literal label strings, em-dash placeholder treatment, and the format of the trailing date / time line for known fixtures. Tests SHALL also assert the tap-target invariant from [3.2](#3.2) (em-dash tiles are non-tappable) without relying on running a SwiftUI layout pass.  
3. <a name="7.3"></a>A `HistoryViewModel` test SHALL assert that `selectDay(_:)` fires when a day-record tile's tap action is invoked (driven by directly calling the action closure passed into the tile rather than synthesising a SwiftUI gesture), and that the resulting `selectedDay` matches the record's `dayID`. The test set SHALL include a fixture where the Lowest SoC record is today, asserting the today-tap behaviour described in [3.1](#3.1).  
4. <a name="7.4"></a>The card SHALL be exercised in `#Preview` blocks for iPhone and Mac, with at least one fixture that mixes em-dash and populated tiles. (Visual confirmation only; no snapshot test required.)  
