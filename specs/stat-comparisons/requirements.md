# Requirements: Stat Comparisons

## Introduction

The Day Detail screen currently shows a day's energy stats in isolation. This feature adds an opt-in comparison mode that, when enabled, displays an absolute delta as a sub-line beneath each supported stat showing how the selected day compares to a chosen reference period (Yesterday or 7 days ago). The comparison is purely additive UI — when the toggle is off, Day Detail renders exactly as it does today.

## Non-Goals

- Comparison on Dashboard, History, or Settings screens.
- Comparison periods beyond Yesterday and 7-days-ago in v1; last month / last year are deferred.
- Percentage deltas, dual absolute+percent deltas, or trend arrows over multiple periods.
- Per-stat "good vs bad" colouring; chevrons indicate direction only.
- Same-time-of-day cutoff for Today; Today's in-progress totals are compared against the comparison day's full totals as-is, with no extra caption or affordance beyond the standard failure caption defined in §5.5.
- Comparisons on the Battery (charge/discharge + Lowest SOC) and Peak Usage cards in v1.
- Comparisons on the Power, Battery Power, and SOC charts; only the SummaryBlock and Five-Block panel rows are touched in v1.
- iCloud sync of toggle / period preferences across devices.
- Localization of period chip and caption strings; copy stays English in v1.
- Explicit Dynamic Type stacking rules, RTL chevron mirroring rules, and wide-screen (iPad / macOS) column variants. These are left as best-effort for v1; concrete ACs land in a follow-up if needed.

## Supported surfaces (used throughout)

These three lists pin down the scope of every AC below; they are referenced by id rather than restated.

- **Supported cards (`SC`)**: the Day Detail SummaryBlock (Power) card and the DayInFiveBlocksPanel.
- **Supported SummaryBlock rows (`SR`)**: Solar produced, House used, Grid in (peak), Grid in (off-peak), Grid out. The Day Detail SummaryBlock is constructed via `SummaryBlock(summary:offpeakGridImport:showsBatteryCycle:)`, which renders with `showsBatteryCycle: false` and never sets `avgLoadWatts`. As a consequence, the "15m avg load" and "Battery cycle" rows are not rendered on Day Detail and are not in `SR`.
- **Supported five-block values (`FB`)**: each block's `totalKwh` and, on daylight blocks (`morningPeak`, `offPeak`, `afternoonPeak`), each block's `solarKwh`.

## Delta formatting (used throughout)

These rules apply to every delta rendered in `SR` and `FB`. Requirements §3 and §4 reference them rather than restating.

- **Placement**: each delta SHALL render as a sub-line directly beneath the row's primary value, right-aligned to the same trailing edge as the value it annotates. It SHALL NOT render inline alongside the value. On daylight rows of `DayInFiveBlocksPanel` that show both a solar value and a total value, each value SHALL receive its own independent sub-line beneath its own column.
- **Typography and colour**: the sub-line SHALL render in `FluxTheme.Typography.touTime` and `FluxTheme.Palette.tertiaryText`, matching the time-range caption already used on `DayInFiveBlocksPanel` rows. The colour is deliberately neutral; it does not encode "good" or "bad".
- **Value**: signed absolute difference (selected day − comparison day), formatted to one decimal place, in the row's native unit (kWh).
- **Direction indicator**: prefixed to the value:
  - `▲` when the rounded one-decimal display is greater than `0.0`.
  - `▼` when the rounded one-decimal display is less than `0.0`.
  - `—` (em-dash) when the rounded one-decimal display equals `0.0`. This replaces the chevron entirely; no `+0.0` is rendered.
- **Per-row fallback**: when the comparison day's value for a given row is unavailable — including, for daylight five-block rows, a present block whose `solarKwh` field is nil — the sub-line SHALL render no delta value or chevron for that row. Per the layout-stability rule below, the sub-line slot itself remains visually present.
- **Layout stability when Compare is on**: every row in `SR` and every value column in `FB` SHALL reserve sub-line height while the Compare toggle is on, regardless of whether that specific row has a delta to render. Rows that fall back render an empty sub-line slot at the same height as rows that show a delta. This guarantees a uniform card height and no jitter as deltas appear, change period, or load asynchronously after day-navigation.
- **Layout when Compare is off**: every row in `SR` and `FB` SHALL render at its pre-feature height with no reserved sub-line slot. Toggling Compare off SHALL return every row to its pre-feature layout exactly.
- **Implementation surface (deferred to design)**: whether the sub-line is added via a new optional parameter on `FluxStatRow`, a wrapper view, or a sibling component is a design-phase decision; the requirements above describe behaviour, not API.

## Requirements

### 1. Compare Toggle

**User Story:** As a Flux user, I want a single toggle on the Day Detail screen that turns comparisons on, so that the default view stays uncluttered.

**Acceptance Criteria:**

1. <a name="1.1"></a>The Day Detail screen SHALL display a single Compare toggle directly above the SummaryBlock card, beneath the existing day-navigation header and note section.  
2. <a name="1.2"></a>The toggle SHALL default to off on first install.  
3. <a name="1.3"></a>WHEN the toggle is off, the Day Detail screen SHALL render exactly as it does today, with no period chip, no failure caption, and no sub-line slots reserved on any card in `SC`.  
4. <a name="1.4"></a>WHEN the toggle is on, the Day Detail screen SHALL render a period chip beside the toggle, reserve a sub-line slot on every row in `SR` and value column in `FB`, and render a delta into each slot subject to the per-row fallback in Delta Formatting.  
5. <a name="1.5"></a>The toggle's on/off state SHALL persist across launches per device.

### 2. Period Selection

**User Story:** As a Flux user, I want to choose what comparison period the deltas are computed against, so that I can compare to yesterday or to the same day-of-week last week.

**Acceptance Criteria:**

1. <a name="2.1"></a>WHEN the Compare toggle is on, the period chip SHALL offer exactly two periods: "Yesterday" and "7 days ago".  
2. <a name="2.2"></a>The default selected period on first install SHALL be "Yesterday".  
3. <a name="2.3"></a>The selected period SHALL persist across launches per device, independent of the toggle's on/off state.  
4. <a name="2.4"></a>WHEN the user changes the selected period, every visible delta on the screen SHALL update to the new period without requiring a screen reload or pull-to-refresh.  
5. <a name="2.5"></a>The period chip SHALL NOT be visible when the Compare toggle is off.

### 3. SummaryBlock (Power) Comparisons

**User Story:** As a Flux user, I want to see how solar, household, and grid totals changed versus the comparison day, so that I can spot day-over-day shifts at a glance.

**Acceptance Criteria:**

1. <a name="3.1"></a>WHEN comparisons are on, the Day Detail SummaryBlock SHALL render a sub-line beneath the primary value of every row in `SR`.  
2. <a name="3.2"></a>The "House used" comparison value SHALL be computed from the comparison day's solar, grid import, grid export, battery charge, and battery discharge using the same formula `HouseholdLoad.kwh` already uses for the selected day's value.  
3. <a name="3.3"></a>Delta value, indicator, colour, per-row fallback, and layout-stability rules SHALL match the shared Delta Formatting block.

### 4. Five-Block Usage Comparisons

**User Story:** As a Flux user, I want the per-block usage panel to show how each block's totals compare, so that I can see whether morning peak, off-peak, or evening usage shifted versus the comparison day.

**Acceptance Criteria:**

1. <a name="4.1"></a>WHEN comparisons are on, the DayInFiveBlocksPanel SHALL render a sub-line beneath each value in `FB`. On daylight rows, the solar sub-line SHALL appear directly beneath the solar value column and the total sub-line directly beneath the total value column; the two sub-lines are independent and may show different states (one rendering a delta, the other in fallback).  
2. <a name="4.2"></a>WHEN the comparison day has no entry of the same `kind` in its `dailyUsage.blocks`, both the total and (where applicable) solar deltas for that block SHALL fall back per Delta Formatting; the block's primary values continue to render.  
3. <a name="4.3"></a>Delta value, indicator, colour, per-row fallback, and layout-stability rules SHALL match the shared Delta Formatting block.

### 5. Comparison Day Resolution and Failure Handling

**User Story:** As a Flux user, I want comparison values to be drawn from the correct calendar day relative to the day I'm viewing, so that "Yesterday" and "7 days ago" mean the same thing regardless of which day I'm on.

**Acceptance Criteria:**

1. <a name="5.1"></a>The "Yesterday" period SHALL resolve to the calendar day immediately before the selected day, in the site-local timezone (currently Sydney).  
2. <a name="5.2"></a>The "7 days ago" period SHALL resolve to the calendar day exactly seven days before the selected day, in the site-local timezone (currently Sydney).  
3. <a name="5.3"></a>WHEN the user navigates to the previous or next day via the Day Detail navigation, the deltas SHALL recompute against the same period offset (i.e. "Yesterday" still means "the day before the new selected day"), without requiring the user to toggle Compare off and on.  
4. <a name="5.4"></a>WHEN the selected day is today, the comparison SHALL use the comparison day's full-day values without truncating to today's elapsed time.  
5. <a name="5.5"></a>WHEN the resolved comparison day has no data, the comparison fetch fails (network or backend error), or the comparison date precedes the earliest day available to the client, every row in `SR` and value in `FB` SHALL fall back per Delta Formatting (sub-line slot reserved, no value rendered) and the screen SHALL display a single subdued caption directly below the period chip, leading-aligned to the chip, reading "No comparison data available for {period}". This caption applies on Today and on past days alike.  
6. <a name="5.6"></a>WHEN comparisons are on and a comparison fetch is in flight after a day-navigation, every sub-line slot SHALL render in its fallback state (slot reserved, no value) until the new comparison data resolves; row primary values continue to render unaffected during this interval, and no row height changes while the fetch is in flight.

### 6. Cross-Platform Parity

**User Story:** As a Flux user on macOS, I want the comparison feature to behave the same as on iOS, so that I get a consistent experience.

**Acceptance Criteria:**

1. <a name="6.1"></a>The toggle, period chip, failure caption, and sub-line deltas SHALL render on both iOS and macOS Day Detail screens.  
2. <a name="6.2"></a>The toggle and period preferences SHALL be device-local; macOS and iOS instances SHALL each persist their own preference and SHALL NOT sync via iCloud KVS or iCloud Keychain.

### 7. Accessibility

**User Story:** As a VoiceOver user, I want spoken descriptions for the delta values, so that I can understand the comparisons without relying on the chevron icon.

**Acceptance Criteria:**

1. <a name="7.1"></a>Each row that renders both a primary value and a delta sub-line SHALL be exposed to VoiceOver as a single combined accessibility element, with a label of the form "{row label}: {primary value}, {up|down|unchanged} {absolute delta} {unit} versus {period}", e.g. "Solar produced: 14.8 kilowatt-hours, up 1.2 kilowatt-hours versus yesterday".  
2. <a name="7.2"></a>WHEN a row is in the Delta Formatting fallback state, its accessibility label SHALL omit the comparison clause and read only "{row label}: {primary value}".  
3. <a name="7.3"></a>The Compare toggle and period chip SHALL be reachable and operable via the platform's standard accessibility traversal, with labels that name the feature and the currently selected period.  
4. <a name="7.4"></a>The failure caption SHALL be exposed to VoiceOver as plain spoken text matching the visible string (e.g. "No comparison data available for 7 days ago").
