# Requirements: Evening / Night Stats

## Introduction

The day detail screen currently shows charts, peak usage periods, and an energy summary, but does not surface how much electricity is consumed when no solar is being generated. This feature adds two stat blocks — **Evening** (last solar of the day → midnight) and **Night** (midnight → first solar of the day) — each showing total household usage in kWh and the average per hour, computed by the backend `/day` endpoint and displayed in a new iOS card on `DayDetailView`. Periods are anchored to the requested calendar date in Australian eastern time and not to a physical overnight; an actual overnight is split between two `/day` responses (its first half in day N's Evening, its second half in day N+1's Night). The goal is to make the cost of "no-solar" usage visible at a glance, including when those periods are still in progress for today.

## Non-Goals

- Adjusting the iOS Dashboard or History screens — this feature lives only on Day Detail.
- Generating new DynamoDB tables, indexes, or polling routines — the data must come from existing readings.
- Predicting future usage during the upcoming evening or night.
- Splitting consumption by source (solar self-consumption is zero in these windows by definition).
- Astronomical accuracy beyond a coarse civil sunset/sunrise fallback (no atmospheric refraction, no DST edge handling beyond what the standard library gives).
- A site-coordinate configuration UI — coordinates are hardcoded.

## 1. Backend: Period Detection and Computation

**User Story:** As an iOS app user, I want the backend to identify the evening and night no-solar periods of a day and compute their usage, so that the app can render them without duplicating logic.

**Acceptance Criteria:**

1. <a name="1.1"></a>The `/day` response SHALL include an `eveningNight` object containing an `evening` block and a `night` block when at least one of the two periods can be determined.  
2. <a name="1.2"></a>Each block SHALL include `start` and `end` as RFC 3339 timestamps in UTC, `totalKwh` as a number rounded to 2 decimals, `averageKwhPerHour` as an optional number rounded to 2 decimals (omitted per [1.7](#1.7)), `status` as either `"complete"` or `"in-progress"`, and `boundarySource` as either `"readings"` or `"estimated"`. Rounding SHALL be applied at serialization only; intermediate computations use full float precision.  
3. <a name="1.3"></a>The system SHALL define the evening period as starting at the timestamp of the latest reading on the requested date with `ppv > 0` and ending at the next midnight (`Australia/Sydney`, which is the IANA zone the codebase already uses and shares Melbourne's UTC offset year-round) for that date.  
4. <a name="1.4"></a>The system SHALL define the night period as starting at the requested date's midnight (`Australia/Sydney`) and ending at the timestamp of the earliest reading on the requested date with `ppv > 0`.  
5. <a name="1.5"></a>The system SHALL apply the sunrise/sunset fallback **per block**: WHEN the evening block has no `ppv > 0` reading on the requested date, set its `start` to the computed sunset (see [1.12](#1.12)) and set `boundarySource = "estimated"`; WHEN the night block has no `ppv > 0` reading on the requested date, set its `end` to the computed sunrise and set `boundarySource = "estimated"`. Otherwise `boundarySource = "readings"`.  
6. <a name="1.6"></a>The system SHALL compute `totalKwh` for each period by trapezoidal integration of `pload` (in watts, converted to kWh by dividing the integrated watt-seconds by 3,600,000) over the readings whose timestamps fall in the half-open interval `[start, end)`, applying the same 60-second pair-gap skip used by the existing today-energy integration (`computeTodayEnergy`). Negative `pload` values SHALL be clamped to 0. Reading pairs that straddle the period boundary SHALL be linearly interpolated to `start` or `end` so the integration is anchored to the period edges rather than the nearest interior reading.  
7. <a name="1.7"></a>The system SHALL compute `averageKwhPerHour` as `totalKwh` divided by the wall-clock elapsed hours between the period's `start` and `end` (rounded to 2 decimals). WHEN the elapsed wall-clock duration is less than 60 seconds, the system SHALL omit `averageKwhPerHour` entirely from that block.  
8. <a name="1.8"></a>WHEN the requested date is today (`Australia/Sydney`) and a period's nominal end is in the future, the system SHALL set `end = min(nominalEnd, requestTime)` and `status = "in-progress"`, where `nominalEnd` is the value resolved by [1.3](#1.3)/[1.4](#1.4)/[1.5](#1.5). Totals and averages are computed from `start` to this clamped `end`.  
9. <a name="1.9"></a>WHEN the night period's `start` (midnight) is in the future relative to the request time on today's date, the system SHALL omit the `night` block entirely (it has not begun yet).  
10. <a name="1.10"></a>WHEN no readings exist for the requested date and no daily-power fallback exists, the system SHALL omit the `eveningNight` object entirely.  
11. <a name="1.11"></a>WHEN only the daily-power fallback is available (no flux-readings), the system SHALL omit the `eveningNight` object — fallback data lacks the `pload` resolution needed for accurate integration.  
12. <a name="1.12"></a>The sunrise/sunset fallback SHALL be sourced from an embedded static lookup table of Melbourne sunrise/sunset wall-clock times keyed by `MM-DD`, with `time.ParseInLocation` resolving the appropriate UTC instant via `Australia/Sydney` (DST-immune by construction). Values SHALL be within ±2 minutes of an astronomical reference; a single year's data is acceptable for use across multiple years given that ±2-minute tolerance.  
13. <a name="1.13"></a>The system SHALL NOT introduce any new DynamoDB table, GSI, or query — the computation MUST reuse the readings slice already fetched by `handleDay`.

## 2. API Contract

**User Story:** As a developer integrating the iOS app, I want the new field to be additive and predictable, so that older clients continue to function and newer clients can decode without custom logic.

**Acceptance Criteria:**

1. <a name="2.1"></a>The `eveningNight` field SHALL be added to `DayDetailResponse` without changing the type, name, or semantics of any existing field.  
2. <a name="2.2"></a>The field SHALL be omitted from the JSON payload (using `omitempty` semantics) when the conditions in [1.10](#1.10) or [1.11](#1.11) apply.  
3. <a name="2.3"></a>WHEN only one of the two periods is available (e.g. today before sunrise, or today between midnight and the first reading), the response SHALL include the `eveningNight` object with the unavailable block omitted entirely rather than zero-filled.

## 3. iOS: Display

**User Story:** As a Flux user, I want to see at a glance how much I used during the no-solar parts of a day, so that I can spot expensive evenings and overnight loads.

**Acceptance Criteria:**

1. <a name="3.1"></a>The Day Detail screen SHALL render an "Evening / Night" card between the Peak Usage card and the Summary card whenever the response contains an `eveningNight` object with at least one block.  
2. <a name="3.2"></a>The card SHALL display each present block as a row showing the period label ("Evening" or "Night"), the local start–end clock times in 24-hour format using Melbourne/Sydney time zone, the total in kWh to one decimal, and the average in kWh/h to two decimals.  
3. <a name="3.3"></a>WHEN a block's `status` is `"in-progress"`, the row SHALL show an "in-progress" indicator styled the same way as the existing offpeak pending indicator on the Dashboard.  
4. <a name="3.4"></a>WHEN a block's `boundarySource` is `"estimated"`, the row SHALL append a small caption (e.g. "≈ sunset" / "≈ sunrise") next to the time range so the user knows the boundary is estimated rather than measured.  
5. <a name="3.5"></a>The card's container SHALL use the same `.thinMaterial` background and `RoundedRectangle(cornerRadius: 16, style: .continuous)` styling as the existing Peak Usage and Summary cards.  
6. <a name="3.6"></a>The card SHALL NOT appear on days where the response omits `eveningNight`, matching how `PeakUsageCard` is suppressed on empty data.  
7. <a name="3.7"></a>The card SHALL NOT appear when `viewModel.hasPowerData` is false (fallback data path), even if a payload were somehow present.

## 4. Testing

**User Story:** As the project maintainer, I want enough test coverage to trust the new computation across edge cases, so that future refactors don't silently break the evening/night totals.

**Acceptance Criteria:**

1. <a name="4.1"></a>Backend unit tests SHALL cover at minimum: a typical day with both periods complete, today before sunrise, today after sunset (in-progress evening), today after sunrise but before sunset (only night present), a fully overcast day with no `ppv > 0` readings, a partial-data day with morning solar but no afternoon readings (per-block fallback to computed sunset for evening only), a day with readings but no daily-energy row, a day with daily-power fallback only, and a date in the future or with no data.  
2. <a name="4.2"></a>The Melbourne sunrise/sunset lookup SHALL have a sanity test that asserts plausible-range values at the two solstices, leap-year (Feb 29) fallback, and a date near the AEDT-end DST transition.  
3. <a name="4.3"></a>iOS ViewModel tests SHALL cover at minimum: response with both blocks, response with only one block, response with `eveningNight` absent, response with `boundarySource = "estimated"` (caption rendered), and response in the fallback-data path.
