# Requirements: History Month and Week to Date

## Introduction

The History screen currently offers three fixed-length ranges (7, 14, and 30 days). This feature adds two calendar-anchored ranges — "this week" and "this month" — so a user can see only the days since their current week or month began. Week and month boundaries are computed against the same Sydney-based calendar that keys the stored data, while the week's start day follows the device locale rather than a hardcoded value.

## Non-Goals

- Persisting the selected range across app launches (the range is not persisted today and this feature does not change that).
- Custom or arbitrary user-chosen date ranges (e.g. a date-range picker).
- "Last week" / "last month" (completed prior periods) — only the current week/month to date.
- Quarter-, year-, or all-time-to-date ranges.
- Changing how any History card aggregates or renders data; only the set of days it operates on changes.
- Backend retention changes to make older days available beyond what is already stored.

## Requirements

### 1. Week-to-date range

**User Story:** As a Flux user, I want a "this week" range on History, so that I can see only the days since my current week began.

**Acceptance Criteria:**

1. <a name="1.1"></a>WHEN the user selects the Week range, the History screen SHALL display data from the start of the current week through today, inclusive.  
2. <a name="1.2"></a>The current week SHALL start on the most recent day, on or before today, whose weekday matches the locale-derived first weekday (precise rule defined in [3.2](#3.2)).  
3. <a name="1.3"></a>IF today is the week's start day, THEN the Week range SHALL display only today.  

### 2. Month-to-date range

**User Story:** As a Flux user, I want a "this month" range on History, so that I can see only the days since the month began.

**Acceptance Criteria:**

1. <a name="2.1"></a>WHEN the user selects the Month range, the History screen SHALL display data from the 1st of the current calendar month through today, inclusive.  
2. <a name="2.2"></a>IF today is the 1st of the month, THEN the Month range SHALL display only today.  

### 3. Boundary computation

**User Story:** As a Flux user, I want week and month boundaries to line up with the data and respect my locale, so that the days shown are correct and the week starts on the right day for me.

**Acceptance Criteria:**

1. <a name="3.1"></a>The system SHALL compute "today", the month start, and the week start using the same Sydney-based calendar that keys the stored daily data.  
2. <a name="3.2"></a>The week's first weekday SHALL be derived from the device locale (not hardcoded), and the week start SHALL be the most recent Sydney calendar date — on or before the Sydney "today" — whose weekday equals that first weekday; the weekday SHALL be evaluated against the Sydney date, not the device-local date.  
3. <a name="3.3"></a>The week and month boundaries SHALL be recomputed against the current Sydney "today" on each load trigger (range selection, pull-to-refresh, or screen appearance); live recomputation while the screen sits idle is not required.  

### 4. Variable-length data retrieval

**User Story:** As a Flux user, I want the to-date ranges to load reliably regardless of how far into the week or month it is, so that the range works on any day.

**Acceptance Criteria:**

1. <a name="4.1"></a>The system SHALL retrieve History data for any range length from 1 through 31 days without error, including a full month-to-date range requested on the 31st of a 31-day month (the result SHALL NOT be clipped to 30 days).  
2. <a name="4.2"></a>IF data is unavailable for some days within the selected range, THEN the screen SHALL display the available days using the same gap handling as the existing fixed-length ranges.  
3. <a name="4.3"></a>WHEN a Week or Month range is served from the offline cache, the displayed days SHALL be bounded by the computed week/month start, so no day earlier than the period start is shown even when the cache has gaps.  
4. <a name="4.4"></a>IF a selected range fails to load, THEN the screen SHALL present the same error or empty state used by the fixed-length ranges, without crashing or hanging.  

### 5. Downstream consistency

**User Story:** As a Flux user, I want every card on the History screen to reflect the to-date range the same way the fixed ranges do, so that the numbers stay trustworthy and consistent.

**Acceptance Criteria:**

1. <a name="5.1"></a>All History cards driven by the selected range (period overview, charts, and daily usage list) SHALL compute their values from the Week/Month day set using the same logic applied to the fixed-length ranges.  
2. <a name="5.2"></a>Given an identical set of day records, every History card SHALL produce identical values regardless of which range selection produced that set.  
3. <a name="5.3"></a>Charts and the expanded-chart view SHALL scale their axes to the actual number of days in the selected range, including range lengths that are not 7, 14, or 30.  

### 6. Range control presentation

**User Story:** As a Flux user, I want the two new ranges in the existing range control, so that switching to them works like switching between the day ranges I already use.

**Acceptance Criteria:**

1. <a name="6.1"></a>The History range control SHALL present five options in this order: 7d, 14d, 30d, Wk, Mo.  
2. <a name="6.2"></a>The new options SHALL be labelled "Wk" (week to date) and "Mo" (month to date).  
3. <a name="6.3"></a>The range control SHALL retain the existing segmented-control interaction and styling.  
4. <a name="6.4"></a>The default selected range SHALL remain 7d.  
