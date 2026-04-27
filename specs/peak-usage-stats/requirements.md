# Requirements: Peak Usage Stats

## Introduction

Today's Day Detail screen shows totals for the no-solar evening and night windows but offers no comparable view of how the rest of the day's load is distributed. This feature replaces the existing Evening / Night card with a single "Daily Usage" card that breaks each calendar date into up to five chronological no-overlap blocks — Night, Morning Peak, Off-Peak, Afternoon Peak, Evening — each carrying total kWh, average kWh/h, and a share of the day's load. Computation lives in the existing `/day` Lambda and reuses the established load-integration helpers; the iOS card supersedes `EveningNightCard` rather than sitting alongside it.

## Non-Goals

- Per-source breakdowns inside any block (solar self-consumption, grid-import share, battery-discharge share).
- Historical aggregation across multiple days (the History screen owns multi-day rollups).
- Predicting future usage during the upcoming day.
- Surfacing the new blocks on the Dashboard, History, or widget surfaces — Day Detail only.
- Astronomical accuracy beyond the existing ±2-minute Melbourne sunrise/sunset table.
- Operator-visible signal when off-peak SSM is misconfigured ([1.13](#1.13) is silent fallback by design).

## Definitions

- **firstSolar**: the timestamp of the earliest reading on the requested date with `Ppv > 0` whose timestamp is at or after `computedSunrise − 30 minutes`. When no qualifying reading exists, `firstSolar` falls back to the computed sunrise from the Melbourne sunrise/sunset table. (Carries forward `preSunriseBlipBuffer` from `findEveningNight`.)
- **lastSolar**: the timestamp of the latest reading on the requested date with `Ppv > 0` whose timestamp is in the closed interval `[computedSunrise − 30 minutes, computedSunset + 30 minutes]`. When no qualifying reading exists, `lastSolar` falls back to the computed sunset.
- **offpeakStart / offpeakEnd**: clock-time boundaries resolved from the SSM parameters used by the rest of the system (`/flux/offpeak-start`, `/flux/offpeak-end`).
- **requestTime**: the clock at which `/day` is processing the request, in UTC.
- **today**: the calendar date `requestTime` falls on, evaluated in `Australia/Sydney`.
- **recentSolarThreshold**: 5 minutes. A reading is *recent solar* when its timestamp is within `[requestTime − recentSolarThreshold, requestTime]` AND its `Ppv > 0`.
- **Solar-window invariant**: `firstSolar < offpeakStart < offpeakEnd < lastSolar`. The five-block layout is well-defined only when this holds.

## 1. Backend: Block Detection and Computation

**User Story:** As an iOS app user, I want the backend to identify and compute the day's load distribution across five chronological blocks, so that the app can render them without duplicating logic.

**Acceptance Criteria:**

1. <a name="1.1"></a>The `/day` response SHALL include a `dailyUsage` object containing a `blocks` array when at least one block survives the pipeline in [1.8](#1.8), and SHALL omit the object entirely otherwise.  
2. <a name="1.2"></a>The system SHALL define block intervals as the following half-open ranges against `Australia/Sydney`-anchored times for the requested date:  
   - **night**: `[midnight, firstSolar)`  
   - **morningPeak**: `[firstSolar, offpeakStart)`  
   - **offPeak**: `[offpeakStart, offpeakEnd)`  
   - **afternoonPeak**: `[offpeakEnd, lastSolar)`  
   - **evening**: `[lastSolar, nextMidnight)`  
3. <a name="1.3"></a>Each emitted block SHALL include `kind` (one of `"night" | "morningPeak" | "offPeak" | "afternoonPeak" | "evening"`), `start` and `end` as RFC 3339 UTC timestamps, `totalKwh` as a number rounded to 2 decimals, `averageKwhPerHour` as an optional number rounded to 2 decimals (omitted per [1.6](#1.6)), `percentOfDay` as an integer 0–100, `status` as either `"complete"` or `"in-progress"`, and `boundarySource` as either `"readings"` or `"estimated"`.  
4. <a name="1.4"></a>The system SHALL compute `totalKwh` for each block by invoking the existing `integratePload` helper once per block with that block's emitted `[start, end)` window. No re-implementation of trapezoidal integration, gap-skip rules, or boundary interpolation is permitted.  
5. <a name="1.5"></a>A block's `boundarySource` SHALL be `"estimated"` WHEN at least one of the block's emitted `start` or `end` timestamps was derived from the Melbourne sunrise/sunset fallback table; otherwise `"readings"`. Boundaries derived from real readings, SSM-configured off-peak times, calendar midnight, or `requestTime` (in-progress clamping) all count as `"readings"`.  
6. <a name="1.6"></a>The system SHALL compute `averageKwhPerHour` as `totalKwh` divided by the elapsed hours between the block's `start` and `end`, where elapsed hours = `(endUnix − startUnix) / 3600` (UTC-second arithmetic, naturally correct on DST-transition days). Result is rounded to 2 decimals. WHEN the elapsed duration is shorter than 60 seconds, `averageKwhPerHour` SHALL be omitted.  
7. <a name="1.7"></a>The system SHALL compute `percentOfDay` for each emitted block as `round(blockTotalKwh / sumOfEmittedBlocksTotalKwh × 100)`. Both numerator and denominator SHALL use unrounded `totalKwh` values (rounding is applied only at serialization, per the precedent set by `evening-night-stats` AC 1.2). The sum of `percentOfDay` across emitted blocks MAY differ from 100 by ±3 due to integer rounding. WHEN the denominator is 0, `percentOfDay` SHALL be 0 for every emitted block.  
8. <a name="1.8"></a>The system SHALL evaluate blocks through this pipeline, in order. Each step's output is the input to the next.  
   1. **Resolve nominal interval** per [1.2](#1.2), using the boundary definitions above.  
   2. **Solar-window guard**: IF the solar-window invariant (see Definitions) does not hold, THEN treat as off-peak unresolved per [1.13](#1.13) and emit only `night` and `evening` using their nominal intervals (then continue with steps 3–6 for those two blocks).  
   3. **Today gate** (fires when today AND `solarStillUp`, where `solarStillUp = recentSolar exists OR (no qualifying Ppv reading exists today AND requestTime ≤ computedSunset)`): override `evening` as omitted AND override `afternoonPeak`'s end to `requestTime` AND set `afternoonPeak.status = "in-progress"`. The today-gate does NOT fire on cloudy afternoons where solar genuinely stopped earlier in the day; the normal `lastSolar` boundary is used and `evening` runs from `lastSolar` to `requestTime` (in-progress).  
   4. **Future-block omit**: WHEN today AND a block's `start > requestTime`, omit that block.  
   5. **In-progress clamp**: WHEN today AND a block's `end > requestTime` AND `status` was not already set by step 3, set `end = requestTime` AND `status = "in-progress"`. Otherwise `status = "complete"`.  
   6. **Degenerate omit**: WHEN a block's `start ≥ end` after all clamping, omit that block.  
9. <a name="1.9"></a>The chronological order of the surviving blocks in the response SHALL match [1.2](#1.2): night, morningPeak, offPeak, afternoonPeak, evening.  
10. <a name="1.10"></a>WHEN no readings exist for the requested date, OR when only the daily-power fallback is available (no flux-readings), the system SHALL omit the `dailyUsage` object entirely. This precondition is checked before the pipeline in [1.8](#1.8) runs.  
11. <a name="1.11"></a>WHEN the off-peak window cannot be resolved (missing or unparseable SSM values, or `offpeakStart ≥ offpeakEnd`), the system SHALL emit only `night` and `evening` blocks following [1.2](#1.2)'s sunrise/sunset rules (including the `firstSolar`/`lastSolar` fallback) and SHALL omit `morningPeak`, `offPeak`, and `afternoonPeak`.  
12. <a name="1.12"></a>The system SHALL NOT introduce any new DynamoDB table, GSI, or query — the computation MUST reuse the readings slice already fetched by `handleDay`.  
13. <a name="1.13"></a>(Reserved alias for [1.11](#1.11).) The off-peak-unresolved degradation path is also referenced by [1.8](#1.8) step 2 when the solar-window invariant fails; both paths produce the same two-block (`night`+`evening`) output shape.

## 2. API Contract

**User Story:** As a developer of the iOS client, I want a single, replacement field with predictable shape, so that I can decode the new card without parsing two parallel structures.

**Acceptance Criteria:**

1. <a name="2.1"></a>The `dailyUsage` field SHALL be added to `DayDetailResponse` and SHALL be absent from the JSON payload when the conditions in [1.10](#1.10) apply.  
2. <a name="2.2"></a>The existing `eveningNight` field SHALL be removed from `DayDetailResponse`. iOS clients SHALL be updated in lockstep.  
3. <a name="2.3"></a>`dailyUsage.blocks` SHALL be an ordered array (chronological by `start`) with at most five entries; consumers SHALL NOT assume a fixed length or fixed positions and SHALL identify each block by its `kind` field.  
4. <a name="2.4"></a>No other existing fields in `DayDetailResponse` SHALL be removed, renamed, or have their types changed.

## 3. iOS: Display

**User Story:** As a Flux user, I want one card that shows where my day's load went — across night, morning, off-peak charging, afternoon, and evening — so that I can see at a glance which parts of the day cost the most.

**Acceptance Criteria:**

1. <a name="3.1"></a>The Day Detail screen SHALL render a "Daily Usage" card in the slot currently occupied by `EveningNightCard` (between the Peak Usage card and the Summary card), and the existing `EveningNightCard` SHALL be removed from the view hierarchy.  
2. <a name="3.2"></a>The card SHALL render one row per block in the order received, each row showing the block label per the mapping in [3.4](#3.4), the local start–end clock times in 24-hour format using `Australia/Sydney`, the total in kWh to one decimal, the average in kWh/h to two decimals, and the percentage of the day as an integer with `%` suffix.  
3. <a name="3.3"></a>WHEN a block's `status` is `"in-progress"`, the row SHALL display the same in-progress affordance previously used by `EveningNightCard` for in-progress blocks.  
4. <a name="3.4"></a>Block labels and estimated-boundary captions SHALL follow the mapping below. The caption SHALL render positionally adjacent to the start time when the estimated edge is the start, and adjacent to the end time when the estimated edge is the end (no caption otherwise). The caption SHALL render whenever `boundarySource = "estimated"`, regardless of `status`, because the estimated edge for the new blocks (morningPeak, afternoonPeak, evening) is always one that has actually happened.  

   | kind | label | estimated edge | caption | rendered next to |
   |------|-------|----------------|---------|------------------|
   | night | "Night" | end (sunrise) | ≈ sunrise | end time |
   | morningPeak | "Morning Peak" | start (sunrise) | ≈ sunrise | start time |
   | offPeak | "Off-Peak" | (none possible) | — | — |
   | afternoonPeak | "Afternoon Peak" | end (sunset) | ≈ sunset | end time |
   | evening | "Evening" | start (sunset) | ≈ sunset | start time |

   Special case: an in-progress `night` block has `boundarySource = "readings"` per [1.5](#1.5) (its emitted end is `requestTime`, not sunrise). The caption is therefore not shown for in-progress night blocks — this falls out naturally and requires no separate suppression rule.  
5. <a name="3.5"></a>The card's container SHALL match the existing `EveningNightCard` styling (background material, corner shape, padding, headline-font title); no new styling tokens SHALL be introduced.  
6. <a name="3.6"></a>The card SHALL NOT appear when the response omits `dailyUsage`, when `dailyUsage.blocks` is empty, or when `viewModel.hasPowerData` (defined in `DayDetailViewModel`) is false.  
7. <a name="3.7"></a>WHEN `averageKwhPerHour` is omitted on a block, the row SHALL display only the total kWh and percentage; the average column SHALL not render a placeholder.

## 4. Testing

**User Story:** As the project maintainer, I want enough coverage on the new computation to trust it across edge cases, so that future refactors don't silently break the daily totals.

**Acceptance Criteria:**

1. <a name="4.1"></a>Backend unit tests SHALL cover at minimum the following fixtures, each asserting the exact set of emitted block kinds and their `boundarySource` and `status` values:  
   - **Typical past day, all five blocks complete** — `boundarySource = "readings"` for all five.  
   - **Today before sunrise** — only `night` emitted, in-progress, `boundarySource = "readings"` (the emitted end is `requestTime`, not sunrise).  
   - **Today mid-morning-peak** (after `firstSolar`, before `offpeakStart`) — `night` complete, `morningPeak` in-progress; the rest omitted by future-omit.  
   - **Today during off-peak** — `night`, `morningPeak` complete; `offPeak` in-progress; `afternoonPeak`, `evening` omitted.  
   - **Today mid-afternoon-peak with sun still up** (recent Ppv reading exists) — `night`, `morningPeak`, `offPeak` complete; `afternoonPeak` in-progress with `end = requestTime` (today-gate fired); `evening` omitted.  
   - **Today late afternoon, cloudy, solar stopped 90 min ago** (no recent Ppv but `lastSolar < requestTime`) — today-gate does NOT fire; `afternoonPeak` complete with `end = lastSolar`; `evening` in-progress with `start = lastSolar`.  
   - **Today after sunset** — all five emitted; `evening` in-progress.  
   - **Overcast day, no qualifying Ppv** — all five emitted; `night.boundarySource = "estimated"` (end = sunrise fallback); `morningPeak.boundarySource = "estimated"` (start = sunrise fallback); `afternoonPeak.boundarySource = "estimated"` (end = sunset fallback); `evening.boundarySource = "estimated"` (start = sunset fallback); `offPeak.boundarySource = "readings"`.  
   - **Partial-data day, recorder died after off-peak (e.g. 15:30)** — solar-window invariant holds (`lastSolar = 15:25 > offpeakEnd = 14:00`); five-block path runs. `night`, `morningPeak`, `offPeak` emit normally; `afternoonPeak` emits with `end = lastSolar = 15:25`; `evening` emits with `start = 15:25`, `boundarySource = "readings"`, `totalKwh = 0`.  
   - **Partial-data day, recorder died during off-peak (e.g. 12:30)** — solar-window invariant fails (`lastSolar = 12:25 < offpeakEnd = 14:00`); two-block path per [1.11](#1.11). Only `night` (with reading-derived end at `firstSolar`) and `evening` (with `start = lastSolar = 12:25`, `boundarySource = "readings"`, `totalKwh = 0`) emit. The off-peak/morning/afternoon detail is intentionally suppressed because the day's data does not support a clean five-block layout.  
   - **Off-peak SSM misconfigured** ([1.11](#1.11)) — only `night` and `evening` emitted.  
   - **Daily-power fallback only** ([1.10](#1.10)) — `dailyUsage` omitted.  
   - **Solar-window invariant violated by constructed fixture** (e.g. computed sunrise > `offpeakStart`, or `firstSolar == lastSolar`) — only `night` and `evening` emitted.  
   - **DST spring-forward day** (Sydney 2025-10-05 or analogous) — five blocks emit; `averageKwhPerHour` math consumes 23 hours of UTC seconds; block boundaries land on the correct UTC instants.  
   - **DST fall-back day** (Sydney 2026-04-05 or analogous) — five blocks emit; 25-hour day handled correctly.  
   - **Pre-sunrise Ppv blip at 01:30** with `computedSunrise = 06:30` — blip is filtered; `firstSolar` falls back to sunrise (or to the next Ppv reading after `06:00`); `night` ends at the filtered boundary, not at 01:30.  
   - **Post-sunset Ppv blip at 22:00** with `computedSunset = 19:30` — blip is filtered (outside `[sunrise − 30 min, sunset + 30 min]`); `lastSolar` is the last pre-sunset Ppv reading or the sunset fallback; `afternoonPeak` does not absorb the post-sunset hours.  
   - **Future-dated request** — no readings exist; `dailyUsage` omitted per [1.10](#1.10).  
2. <a name="4.2"></a>Backend unit tests SHALL assert that `percentOfDay` values across emitted blocks sum to 100±3 on at least one fixture, and that the function returns `percentOfDay = 0` for every emitted block on a zero-load fixture.  
3. <a name="4.3"></a>iOS ViewModel tests SHALL cover at minimum: response with all five blocks; response with only `night` and `evening` (off-peak misconfigured); response with `dailyUsage` absent; response with `boundarySource = "estimated"` (caption rendered per the mapping in [3.4](#3.4) and adjacent to the correct timestamp); response in the fallback-data path (card hidden); response with one block in-progress (caption still rendered when boundarySource is estimated and the estimated edge has happened); and the error path (state cleared).
