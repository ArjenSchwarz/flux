# Requirements: History Period Navigation

**Ticket:** T-1497

## Introduction

The History screen's Wk and Mo ranges currently show only the current (to-date) week or month. This feature adds navigation to past periods: stepping back and forward one calendar week or month at a time, jumping to any past period via a date picker, and returning to the current period in one action. The backend must serve stored daily data for explicitly requested past date ranges, which it currently cannot (requests are always anchored to today).

## Definitions

- **Calendar week / calendar month** boundaries follow the rules established in `specs/history-month-week-to-date/` (Sydney calendar, locale-derived first weekday).
- **Today** in all checks (next-period disabling, picker maximum, server-side validation) is the current Sydney calendar date, not the device-timezone date.
- **Current period** checks are re-evaluated on each load trigger (range selection, navigation, refresh, screen appearance), per the prior spec's recompute rule.
- Day counts and date ranges are **inclusive on both ends**: a range covering one 31-day month spans 31 days.

## Out of Scope

- Period navigation for the fixed 7d/14d/30d ranges — they remain anchored to today.
- Persisting the viewed period across app restarts or range switches.
- Period-over-period comparison views (e.g. this week overlaid on last week).
- Swipe-gesture navigation; controls are chevrons, a return-to-current action, and the date picker.
- Changes to Dashboard, Day Detail, or widgets.
- Backfilling data for periods before collection started; such periods show as empty. Derived stats for old days can never be recomputed (source readings expire after 30 days).
- Cache eviction for accumulated past-period data.

## Requirements

### 1. Previous/Next Period Navigation

**User Story:** As a Flux user, I want to step back and forward through past weeks and months on the History screen, so that I can review earlier energy data.

**Acceptance Criteria:**

1. <a name="1.1"></a>WHERE the selected range is Wk or Mo, the History screen SHALL provide previous/next-period controls matching the existing Day Detail previous/next-day affordance.
2. <a name="1.2"></a>WHEN the previous-period control is activated, the screen SHALL display the calendar week (Wk) or calendar month (Mo) immediately before the period currently shown.
3. <a name="1.3"></a>WHEN the next-period control is activated, the screen SHALL display the period immediately after the one shown; WHERE the displayed period contains today, the next-period control SHALL be visible but disabled.
4. <a name="1.4"></a>WHERE the displayed period is the current week/month, the displayed data, chart domain, and statistics SHALL match the existing to-date behaviour; WHERE the displayed period is in the past, the screen SHALL show the full calendar period.
5. <a name="1.5"></a>WHERE the selected range is 7d, 14d, or 30d, period-navigation controls SHALL NOT be shown and existing behaviour SHALL be unchanged.
6. <a name="1.6"></a>Backward navigation SHALL NOT be bounded: WHEN a successfully fetched period contains no data, the screen SHALL render the full-period chart axis with an explicit no-data-for-this-period state, visually distinct from the fetch-error state.
7. <a name="1.7"></a>On macOS, the previous/next-period controls SHALL be operable via the same keyboard affordance Day Detail uses for previous/next day.
8. <a name="1.8"></a>WHEN the user refreshes (pull-to-refresh or the macOS refresh action) while a past period is displayed, the screen SHALL re-fetch the displayed period, not reset to the current one.

### 2. Returning to the Current Period

**User Story:** As a Flux user, I want a one-tap way back to the current period, so that I do not have to step forward repeatedly after browsing far into history.

**Acceptance Criteria:**

1. <a name="2.1"></a>WHERE a past period is displayed, the screen SHALL offer a single-action control that returns to the current to-date week/month.
2. <a name="2.2"></a>WHERE the current period is displayed, that control SHALL be hidden.
3. <a name="2.3"></a>WHEN the user changes the range selection (Wk↔Mo or to any fixed range), the screen SHALL reset to the current period for the newly selected range.

### 3. Calendar Date Picker

**User Story:** As a Flux user, I want to pick a date from a calendar, so that I can jump directly to the week or month containing it without repeated stepping.

**Acceptance Criteria:**

1. <a name="3.1"></a>WHERE the selected range is Wk or Mo, the screen SHALL offer a date picker; WHEN a date is picked, the screen SHALL display the week (Wk) or month (Mo) containing that date.
2. <a name="3.2"></a>The date picker SHALL NOT allow selection of dates after the current Sydney calendar date.
3. <a name="3.3"></a>WHEN the picked date falls within the current week/month, the screen SHALL show the current to-date view.

### 4. Period Identification

**User Story:** As a Flux user, I want to see which period I am viewing, so that I do not mistake past data for current data.

**Acceptance Criteria:**

1. <a name="4.1"></a>WHERE Wk or Mo is selected, the screen SHALL display a label identifying the displayed period (week date range, or month and year).

### 5. Historical Data Service

**User Story:** As a Flux user, I want past weeks and months to show the same stored energy data as the rest of the app, so that history beyond the current period is accurate and consistent.

**Acceptance Criteria:**

1. <a name="5.1"></a>The system SHALL accept history requests for an explicit inclusive start/end date range, alongside the existing day-count request form; WHEN both forms are supplied in one request, the system SHALL reject it as invalid.
2. <a name="5.2"></a>For a requested range, the system SHALL serve stored daily energy data, off-peak splits, derived stats, and notes where stored; fields that were never stored for a day SHALL be served as absent, not as an error.
3. <a name="5.3"></a>Date-range responses SHALL contain only stored values and no live-computed today entry (the range form is past-only; see 5.6).
4. <a name="5.4"></a>Existing day-count-based history requests SHALL continue to work unchanged.
5. <a name="5.5"></a>WHEN a requested range predates stored data, the system SHALL return whichever days exist (possibly none) without error.
6. <a name="5.6"></a>The system SHALL reject a range request with a client error WHEN end is before start, end is not before the current Sydney date (the range form is past-only; the current period uses the day-count form), or the inclusive span exceeds 31 days.
7. <a name="5.7"></a>For fields displayed by both screens, values shown for a day in a past period SHALL equal the values Day Detail shows for that same day.

### 6. Statistics over Past Periods

**User Story:** As a Flux user, I want totals and averages for a past period computed over its recorded days, so that the stats are meaningful for completed periods.

**Acceptance Criteria:**

1. <a name="6.1"></a>WHERE a past period is displayed, per-day averages SHALL divide by the number of days in the period that have a stored daily-energy record, with no partial-day exclusion; totals SHALL sum those same days.
2. <a name="6.2"></a>WHERE a past period has fewer recorded days than calendar days, the period overview SHALL indicate this (e.g. "11 of 30 days").
3. <a name="6.3"></a>WHERE the current period is displayed, statistics behaviour SHALL be unchanged.

### 7. Offline and Cache Behaviour

**User Story:** As a Flux user, I want previously viewed past periods to be available offline, so that connectivity loss does not blank the History screen.

**Acceptance Criteria:**

1. <a name="7.1"></a>Fetched past-period days SHALL be cached the same way as current-history days.
2. <a name="7.2"></a>WHEN a past-period fetch fails and cached data exists for dates in that period, the screen SHALL show only the cached days bounded by both the period start and end, using the existing offline-fallback presentation.
3. <a name="7.3"></a>WHEN a past-period fetch fails and no cached data exists for that period, the screen SHALL show the fetch-error state, not the no-data-for-this-period state.
