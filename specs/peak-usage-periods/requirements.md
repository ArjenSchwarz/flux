# Peak Usage Periods

## Introduction

The Peak Usage Periods feature adds a section to the day detail page that highlights the top periods of highest household power consumption (Pload) during a given day, excluding the off-peak charging window. The goal is to make it easy to see at a glance when the house drew the most power, helping identify patterns like cooking, heating, or appliance usage.

The backend computes peak periods from raw 10-second readings before downsampling, using threshold-based clustering. Periods where Pload strictly exceeds the day's mean (outside off-peak) are grouped, ranked by total energy consumed, and the top 3 are returned as part of the existing `/day` API response. The iOS app displays these in a card on the day detail screen.

---

## Definitions

- **Off-peak window**: The time range defined by `offpeakStart` and `offpeakEnd` (HH:MM format), interpreted in Australia/Sydney timezone. A reading falls within the off-peak window when its local time is >= offpeakStart AND < offpeakEnd.
- **Non-off-peak readings**: All readings for the requested date whose local time falls outside the off-peak window.
- **Threshold**: The arithmetic mean of Pload across all non-off-peak readings for the day.
- **Above-threshold reading**: A reading where Pload is strictly greater than the threshold.
- **Period start time**: The timestamp of the first reading in the period.
- **Period end time**: The timestamp of the last reading in the period.

---

## Requirements

### 1. Backend Peak Period Computation

**User Story:** As a user viewing a day's data, I want the system to identify the highest-consumption periods automatically, so that I can see when the house used the most power without manually scanning the charts.

**Acceptance Criteria:**

1. <a name="1.1"></a>The system SHALL compute peak usage periods from raw readings (before downsampling) for the requested date, including partial days (today)  
2. <a name="1.2"></a>The system SHALL exclude all readings that fall within the off-peak window from peak period computation (see Definitions for boundary rules)  
3. <a name="1.3"></a>The system SHALL compute the threshold as the arithmetic mean of Pload across all non-off-peak readings for the day  
4. <a name="1.4"></a>The system SHALL identify above-threshold readings (Pload strictly greater than the threshold) and group strictly adjacent above-threshold readings (no below-threshold reading between them in the time-sorted sequence) into initial clusters  
5. <a name="1.5"></a>The system SHALL merge two initial clusters into a single period WHEN the clock-time gap between the last reading of one cluster and the first reading of the next cluster is 5 minutes or less, regardless of any below-threshold readings in between  
6. <a name="1.6"></a>The system SHALL discard any period with a total duration (end time minus start time) of less than 2 minutes  
7. <a name="1.7"></a>The system SHALL compute total energy for each period using trapezoidal integration of Pload over time, skipping reading pairs with gaps longer than 60 seconds (consistent with existing `computeTodayEnergy` gap handling)  
8. <a name="1.8"></a>The system SHALL rank periods by total energy consumed in descending order  
9. <a name="1.9"></a>The system SHALL return at most 3 peak periods  
10. <a name="1.10"></a>IF fewer than 3 periods qualify, THEN the system SHALL return however many exist (0, 1, or 2)  
11. <a name="1.11"></a>Each peak period SHALL include: start time (RFC 3339), end time (RFC 3339), average Pload in watts (rounded to 1 decimal place), and total energy consumed in watt-hours (rounded to whole number)  
12. <a name="1.12"></a>The system SHALL return an empty array when no readings exist, when all readings fall within the off-peak window, or when no readings exceed the threshold  
13. <a name="1.13"></a>The system SHALL use the already-fetched raw readings from the `/day` handler; no additional DynamoDB queries SHALL be required  

### 2. API Response Contract

**User Story:** As the iOS app, I want peak periods included in the existing `/day` response, so that no additional API call is needed and the data arrives with the rest of the day detail.

**Acceptance Criteria:**

1. <a name="2.1"></a>The `/day` endpoint response SHALL include a `peakPeriods` field on the `DayDetailResponse` object (top-level, alongside `date`, `readings`, and `summary`)  
2. <a name="2.2"></a>The `peakPeriods` field SHALL be an array of objects, each containing `start` (string), `end` (string), `avgLoadW` (number), and `energyWh` (number)  
3. <a name="2.3"></a>The `peakPeriods` field SHALL always be present (never null), using an empty array when no periods qualify  
4. <a name="2.4"></a>No existing fields in the `/day` response SHALL be removed, renamed, or have their types changed  
5. <a name="2.5"></a>WHEN the day has only fallback data (no raw readings, only daily power items), THEN `peakPeriods` SHALL be an empty array  

### 3. iOS Day Detail Display

**User Story:** As a user viewing a day's detail on my iPhone, I want to see the peak usage periods in a clear list, so that I can quickly identify when the house consumed the most power.

**Acceptance Criteria:**

1. <a name="3.1"></a>The day detail screen SHALL display a "Peak Usage" card when `peakPeriods` contains one or more entries and `hasPowerData` is true  
2. <a name="3.2"></a>WHEN `peakPeriods` is empty OR `hasPowerData` is false, THEN the card SHALL be hidden entirely (no empty state shown)  
3. <a name="3.3"></a>Each period row SHALL display: the time range in HH:MM format (e.g., "07:15 - 07:45"), the average load in kilowatts with one decimal place (e.g., "4.2 kW"), and the total energy in watt-hours as a whole number (e.g., "2,100 Wh")  
4. <a name="3.4"></a>The card SHALL use the same styling as the existing summary card (`.thinMaterial` background, rounded rectangle, consistent padding and font choices)  
5. <a name="3.5"></a>The card SHALL appear between the charts and the existing summary card  
6. <a name="3.6"></a>Times SHALL be displayed in the Sydney timezone, consistent with all other times on the day detail page  
