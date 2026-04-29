# Requirements: History Daily Usage

## Introduction

The History screen currently visualises solar, grid (peak vs off-peak imports + exports), and battery totals across the selected range, but offers no view of how each day's load was distributed across the same five chronological blocks (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening) that the Day Detail screen shows for a single day. This feature adds a new History card that renders one stacked bar per day with the per-block load totals, sitting alongside the existing Solar / Grid / Battery cards and reusing the established block-detection and rendering vocabulary from Day Detail.

## Non-Goals

- Surfacing per-source breakdowns inside any block (solar share, grid-import share, battery-discharge share).
- Showing the per-block view on the Dashboard, Day Detail, or widget surfaces — Day Detail already owns the single-day breakdown and the Dashboard owns the live summary.
- Computing block totals for ranges greater than 30 days (history is already capped at 7 / 14 / 30 days).
- Predicting future-day blocks or displaying placeholder bars for days that have not yet started.
- Introducing a new range selector — the existing 7 / 14 / 30 picker MUST drive the new card.
- Adding a new "average load (kWh/h)" mini-graph in History; the per-block kWh/h figure stays on Day Detail.
- Replacing or restyling the existing Solar / Grid / Battery cards.

## Definitions

- **block**: one of the five chronological no-overlap intervals defined by `findDailyUsage` for a single calendar date — `night`, `morningPeak`, `offPeak`, `afternoonPeak`, `evening` — together with its `totalKwh`, `status`, and `boundarySource`. Block kinds, intervals, and computation rules carry forward unchanged from the `peak-usage-stats` spec.
- **day-with-blocks**: a calendar date in the requested range for which the backend can return at least one block (i.e. the same gating rules as Day Detail's `dailyUsage` apply: readings exist, not the daily-power fallback).
- **stacked total**: the sum of `totalKwh` across every emitted block for one day.

## 1. Backend: Per-Day Block Totals in `/history`

**User Story:** As an iOS app user, I want each day in the history range to carry its own per-block totals, so that the app can render the multi-day breakdown without making one `/day` call per date.

**Acceptance Criteria:**

1. <a name="1.1"></a>The `/history` response SHALL include, for each day-with-blocks in the requested range, a `dailyUsage` object with the same shape and field semantics as the existing `/day` `dailyUsage` (per the `peak-usage-stats` spec, sections 1 and 2).  
2. <a name="1.2"></a>For days that do not satisfy the day-with-blocks gate (no readings, or daily-power fallback only), the `dailyUsage` field on that day SHALL be omitted from the response — consistent with Day Detail's `dailyUsage` omission rule.  
3. <a name="1.3"></a>The block detection rules (block intervals, solar-window invariant, today-gate, future-omit, in-progress clamp, degenerate omit, off-peak-unresolved fallback) SHALL match those used by `/day` for an equivalent single-date request, with no behavioural drift between the two endpoints.  
4. <a name="1.4"></a>For the request's "today" entry, the in-progress and today-gate rules SHALL fire identically to a `/day` request issued at the same instant for the same date.  
5. <a name="1.5"></a>For dates older than the readings-retention window where the per-day blocks cannot be reconstructed, the day's response entry SHALL omit `dailyUsage` (per [1.2](#1.2)) and the rest of the day's totals SHALL still be returned. The system SHALL NOT fail or degrade the entire response when only some days lack block data.  
6. <a name="1.6"></a>No existing field on `DayEnergy` or `HistoryResponse` SHALL be removed, renamed, or have its type changed by this feature.  
7. <a name="1.7"></a>The handler SHALL produce a successful `/history` response within 1500 ms p95 for a 30-day request on a warm Lambda — the new computation MUST NOT regress past the existing budget by more than 200 ms p95.  
8. <a name="1.8"></a>For days where the off-peak window is unresolved (per `peak-usage-stats` AC 1.11), the day SHALL emit only the `night` and `evening` blocks following the same fallback rule.  

## 2. iOS: Daily Usage Card

**User Story:** As a Flux user, I want a History card that shows where each day's load went across the same blocks I see on Day Detail, so that I can spot patterns in my evening / night / peak / off-peak consumption over the past week or month.

**Acceptance Criteria:**

1. <a name="2.1"></a>The History screen SHALL render a "Daily usage" card after the existing Battery card and before the per-day Summary card.  
2. <a name="2.2"></a>The card SHALL render one stacked bar per day in the response, in chronological order (oldest day at the leading edge), using a `Chart` with one `BarMark` per block stacked on the same x-position.  
3. <a name="2.3"></a>The bar segments SHALL be ordered bottom-to-top in the chronological block sequence: Night, Morning Peak, Off-Peak, Afternoon Peak, Evening.  
4. <a name="2.4"></a>Each block kind SHALL render with a stable, distinguishable colour from the existing app palette. Off-Peak SHALL reuse the teal already used by the Grid card's off-peak segment, and the four peak / out-of-solar blocks SHALL be visually distinct from each other and from the Off-Peak teal.  
5. <a name="2.5"></a>The card SHALL include a chart legend identifying each block by name (matching the labels used on Day Detail: "Night", "Morning Peak", "Off-Peak", "Afternoon Peak", "Evening").  
6. <a name="2.6"></a>Today's bar SHALL render at 50% opacity to match the existing in-progress affordance used by the Solar / Grid / Battery cards.  
7. <a name="2.7"></a>WHEN a day in the response omits `dailyUsage`, the card SHALL render no bar for that day (an x-axis gap), and SHALL NOT substitute zero-value bars or placeholder labels.  
8. <a name="2.8"></a>The card's KPI line SHALL show the average daily total kWh across complete (non-today) days-with-blocks in the range. The subtitle SHALL summarise the largest contributing block over the same window using the format "{blockLabel} largest at {kwh} kWh/day average".  
9. <a name="2.9"></a>WHEN no day in the response has a `dailyUsage` payload, the card SHALL render the same placeholder treatment used by `HistoryGridUsageCard` when the off-peak split is unavailable, with text "No load breakdown available for this range."  
10. <a name="2.10"></a>Tapping a bar SHALL select that day across all History cards using the same `onSelect(dayID:)` flow already used by the Solar / Grid / Battery cards. The currently selected day SHALL be indicated with the same `RuleMark` highlight overlay used by the other three cards.  
11. <a name="2.11"></a>The card chrome (background material, corner radius, padding, headline-font title) SHALL use the existing `HistoryCardChrome` container — no new styling tokens SHALL be introduced.  
12. <a name="2.12"></a>Block kWh values rendered to the chart SHALL be rounded to one decimal in any tooltip / accessibility label, matching the rounding the Day Detail Daily Usage card uses.  

## 3. iOS: Caching and Offline Fallback

**User Story:** As a Flux user with an intermittent connection, I want the History view to keep showing the per-block breakdown for already-cached days when the API is unreachable, so that I do not lose context I had a moment ago.

**Acceptance Criteria:**

1. <a name="3.1"></a>The on-device cache backing the History view (currently `CachedDayEnergy`) SHALL persist the per-day `dailyUsage.blocks` payload for past days alongside the existing energy totals.  
2. <a name="3.2"></a>WHEN the API call fails and the view falls back to cached days, days for which a cached `dailyUsage` exists SHALL render their bars; days whose cache predates this feature SHALL render no bar for that day (per [2.7](#2.7)).  
3. <a name="3.3"></a>Today's `dailyUsage` SHALL NOT be persisted (it is in-progress and would mislead the offline fallback view), matching the existing rule that today is excluded from the historical cache.  
4. <a name="3.4"></a>A schema migration SHALL NOT be required to read pre-existing cached rows — they SHALL load with `dailyUsage` treated as absent, and SHALL be backfilled the next time `/history` succeeds for that day.  

## 4. Testing

**User Story:** As the project maintainer, I want enough coverage on the new computation and rendering to trust the card across edge cases, so that future changes to block detection don't silently break the multi-day view.

**Acceptance Criteria:**

1. <a name="4.1"></a>Backend unit tests SHALL cover `/history` for: a 7-day window where every day has all five blocks; a window straddling the readings-retention boundary so some days omit `dailyUsage`; a window whose "today" is in each of the today-gate states already covered by `peak-usage-stats` AC 4.1 (pre-sunrise, mid-morning-peak, off-peak, mid-afternoon-peak with sun up, late-afternoon cloudy, after-sunset); and a window containing one off-peak-unresolved day so that day emits only `night` and `evening` while neighbours emit five.  
2. <a name="4.2"></a>Backend tests SHALL assert that the `dailyUsage` payload returned by `/history` for a given date is byte-equivalent to the `dailyUsage` payload returned by `/day` for the same date and the same `now`.  
3. <a name="4.3"></a>iOS ViewModel tests SHALL cover: response with all days carrying `dailyUsage`; response with mixed presence (some days omit `dailyUsage`); response with all days omitting `dailyUsage` (placeholder rendered per [2.9](#2.9)); response with today's bar in-progress; cache fallback path with mixed `dailyUsage` presence in the cache.  
4. <a name="4.4"></a>iOS view tests (snapshot or rendered-tree assertions, per project convention) SHALL cover the legend ordering, the bar opacity for today, and the placeholder treatment.  
