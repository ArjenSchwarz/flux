# Requirements: iOS App

## Introduction

The Flux iOS app is a native Swift/SwiftUI application targeting iOS 26+ that provides a focused view of AlphaESS battery system status. It replaces the official AlphaESS app for day-to-day monitoring by displaying data from the Flux Lambda API backend.

The app has three screens: Dashboard (default), History (with day detail drill-down), and Settings. It never communicates with the AlphaESS API directly — all data comes from the Lambda Function URL, which serves pre-computed stats from DynamoDB.

The app uses iOS 26 Liquid Glass styling throughout. All UI elements use native Liquid Glass material and follow iOS 26 conventions.

**Sign convention:** Positive `pbat` means discharging, negative means charging. Positive `pgrid` means importing from grid, negative means exporting. The app displays human-readable labels (e.g. "Exporting", "Importing") derived from these values.

**Deferred to V2+:** Widgets, push notifications, Shortcuts integration, data export.

---

### 1. Platform and Architecture

**User Story:** As a developer, I want the app to follow modern SwiftUI conventions and support future multi-platform expansion, so that the codebase remains maintainable and extensible.

**Acceptance Criteria:**

1. <a name="1.1"></a>The app SHALL target iOS 26 as the minimum deployment target
2. <a name="1.2"></a>The app SHALL be built entirely with SwiftUI — no UIKit view controllers
3. <a name="1.3"></a>The app SHALL use SwiftUI Charts for all chart rendering — no third-party charting libraries
4. <a name="1.4"></a>The app SHALL use NavigationSplitView as the root navigation container to support adaptive layout on iPad and macOS without a rewrite
5. <a name="1.5"></a>The app SHALL separate API client, data models, and business logic into a shared layer with no UIKit or SwiftUI view dependencies
6. <a name="1.6"></a>The app SHALL use Liquid Glass styling throughout — navigation bars, toolbars, tab bars, segmented controls, and grouped lists SHALL use native Liquid Glass material
7. <a name="1.7"></a>The app SHALL store credentials in the Keychain with App Group access so a future widget extension can authenticate independently
8. <a name="1.8"></a>The app SHALL use SwiftData for all cached data

---

### 2. API Client

**User Story:** As the app, I want a networking layer that communicates with the Lambda Function URL, so that all screens can fetch data from the backend.

**Acceptance Criteria:**

1. <a name="2.1"></a>The API client SHALL support three endpoints: `GET /status`, `GET /history?days=N`, and `GET /day?date=YYYY-MM-DD`
2. <a name="2.2"></a>The API client SHALL send an `Authorization: Bearer {token}` header on every request, where the token is read from Keychain
3. <a name="2.3"></a>The API client SHALL use `async/await` and Swift's structured concurrency model
4. <a name="2.4"></a>The API client SHALL decode JSON responses into strongly-typed Swift models using `Codable`
5. <a name="2.5"></a>WHEN the backend returns HTTP 401, the API client SHALL surface an authentication error distinct from other failures
6. <a name="2.6"></a>WHEN the backend is unreachable or returns a non-2xx status, the API client SHALL return a typed error that the UI can use to display appropriate error states
7. <a name="2.7"></a>The API client SHALL use `URLSession` — no third-party HTTP libraries

---

### 3. Authentication and Settings

**User Story:** As a user, I want to configure the backend URL and API token once, so that the app can communicate with my Flux backend.

**Acceptance Criteria:**

1. <a name="3.1"></a>The Settings screen SHALL provide fields for API URL (the Lambda Function URL) and API Token (shared secret)
2. <a name="3.2"></a>The API Token SHALL be stored in the Keychain — not in UserDefaults or SwiftData
3. <a name="3.3"></a>The API URL SHALL be stored in UserDefaults
4. <a name="3.4"></a>WHEN the user taps save, the app SHALL call `/status` to verify the backend is reachable and returning data
5. <a name="3.5"></a>WHEN validation succeeds, the app SHALL save the configuration and navigate to the Dashboard
6. <a name="3.6"></a>WHEN validation fails, the app SHALL display the error and keep the user on the Settings screen
7. <a name="3.7"></a>WHEN no API Token is configured on app launch, the app SHALL redirect to the Settings screen
8. <a name="3.8"></a>The Settings screen SHALL include a configurable load alert threshold (watts) stored in UserDefaults, with a default of 3000W

---

### 4. Dashboard — Battery Hero

**User Story:** As a user, I want to see my battery's current state at a glance, so that I can quickly assess whether the system needs attention.

**Acceptance Criteria:**

1. <a name="4.1"></a>The Dashboard SHALL display `live.soc` as a large centred percentage
2. <a name="4.2"></a>The battery percentage SHALL be colour-coded: green when >60%, default (system label colour) for 30–60%, amber for 15–30%, red for <15%
3. <a name="4.3"></a>The Dashboard SHALL display a progress bar below the percentage that matches the battery colour
4. <a name="4.4"></a>WHEN the battery is discharging (`pbat > 0`) and `battery.estimatedCutoffTime` is present, the status line SHALL read "Discharging · cutoff ~{time}" where time is formatted as a clock time (e.g. "4:03 AM")
5. <a name="4.5"></a>WHEN the battery is charging (`pbat < 0`), the status line SHALL read "Charging"
6. <a name="4.6"></a>WHEN the battery is idle (`pbat == 0`), the status line SHALL read "Idle"
7. <a name="4.7"></a>WHEN SOC is 100 and the battery is charging, the status line SHALL read "Full" instead of showing a cutoff or charge estimate

---

### 5. Dashboard — Power Readings

**User Story:** As a user, I want to see current solar, load, and grid power in a three-column layout, so that I can understand power flow at a glance.

**Acceptance Criteria:**

1. <a name="5.1"></a>The Dashboard SHALL display solar (`live.ppv`), household load (`live.pload`), and grid (`live.pgrid`) in a three-column row below the battery hero
2. <a name="5.2"></a>Power values SHALL be displayed in watts
3. <a name="5.3"></a>Solar SHALL be green when generating (`ppv > 0`), and muted/tertiary colour when 0
4. <a name="5.4"></a>Load SHALL be red when above the configurable threshold (default 3000W), default colour otherwise
5. <a name="5.5"></a>Grid SHALL display a direction label: "Exporting" when `pgrid < 0`, "Importing" when `pgrid > 0`
6. <a name="5.6"></a>Grid SHALL be green when exporting (`pgrid < 0`)
7. <a name="5.7"></a>Grid SHALL be red ONLY when `pgrid > 500` AND `live.pgridSustained` is true AND the current time is outside the off-peak window. Default colour otherwise, including during off-peak import
8. <a name="5.8"></a>The off-peak window times SHALL be read from the `/status` response (`offpeak.windowStart` and `offpeak.windowEnd`)

---

### 6. Dashboard — Secondary Stats

**User Story:** As a user, I want to see battery low point, off-peak data, and rolling averages, so that I can understand trends beyond the current moment.

**Acceptance Criteria:**

1. <a name="6.1"></a>The Dashboard SHALL display a vertical list of secondary stats below the power readings
2. <a name="6.2"></a>The list SHALL include 24h battery low: `battery.low24h.soc` with its timestamp
3. <a name="6.3"></a>The list SHALL include off-peak grid usage: `offpeak.gridUsageKwh`
4. <a name="6.4"></a>The list SHALL include off-peak battery delta: `offpeak.batteryDeltaPercent`
5. <a name="6.5"></a>The list SHALL include 15-minute average load: `rolling15min.avgLoad` with its own cutoff estimate from `rolling15min.estimatedCutoffTime`
6. <a name="6.6"></a>Estimated cutoff times SHALL be displayed as clock times (e.g. "~4:03 AM"), not countdowns
7. <a name="6.7"></a>The cutoff time SHALL be red when less than 2 hours from now
8. <a name="6.8"></a>The cutoff time SHALL be amber when the estimated cutoff is before `offpeak.windowStart` (battery will hit 10% before off-peak charging begins)

---

### 7. Dashboard — Today's Energy

**User Story:** As a user, I want to see today's cumulative energy totals, so that I can track daily generation and consumption.

**Acceptance Criteria:**

1. <a name="7.1"></a>The Dashboard SHALL display a "Today" section at the bottom with a vertical list of energy totals
2. <a name="7.2"></a>The section SHALL include: solar generated (`todayEnergy.epv`), grid imported (`todayEnergy.eInput`), grid exported (`todayEnergy.eOutput`), battery charged (`todayEnergy.eCharge`), battery discharged (`todayEnergy.eDischarge`) — all in kWh
3. <a name="7.3"></a>The today section SHALL use default text colour only — no conditional colouring on energy values
4. <a name="7.4"></a>A "View history" link at the bottom of the Dashboard SHALL navigate to the History screen

---

### 8. Dashboard — Refresh Behaviour

**User Story:** As a user, I want the Dashboard to refresh automatically, so that I always see current data without manual interaction.

**Acceptance Criteria:**

1. <a name="8.1"></a>The Dashboard SHALL fetch `/status` on app launch and when returning to foreground
2. <a name="8.2"></a>The Dashboard SHALL support pull-to-refresh
3. <a name="8.3"></a>The Dashboard SHALL auto-refresh every 10 seconds while foregrounded
4. <a name="8.4"></a>The auto-refresh timer SHALL stop when the app moves to the background
5. <a name="8.5"></a>The auto-refresh timer SHALL restart when the app returns to the foreground

---

### 9. History Screen

**User Story:** As a user, I want to see daily energy totals over time, so that I can identify trends and compare days.

**Acceptance Criteria:**

1. <a name="9.1"></a>The History screen SHALL display a grouped vertical bar chart showing daily energy totals
2. <a name="9.2"></a>The chart SHALL default to 7 days, with a segmented control to switch to 14 or 30 days
3. <a name="9.3"></a>Each day SHALL show 5 side-by-side bars: solar (green), grid imported (red), grid exported (blue), battery charged (amber), battery discharged (purple)
4. <a name="9.4"></a>Today's bars SHALL be slightly faded to indicate partial data
5. <a name="9.5"></a>Tapping a day SHALL update a summary card below the chart with that day's exact kWh numbers
6. <a name="9.6"></a>The selected day SHALL have a subtle background highlight behind its bar group
7. <a name="9.7"></a>The summary card SHALL include a "View day detail" link that navigates to the Day Detail screen for the selected day
8. <a name="9.8"></a>The History screen SHALL fetch data from the `/history?days=N` endpoint

---

### 10. Day Detail Screen

**User Story:** As a user, I want to see detailed time-series charts for a specific day, so that I can understand power flow patterns throughout the day.

**Acceptance Criteria:**

1. <a name="10.1"></a>The Day Detail screen SHALL display three stacked time-series charts using data from the `/day?date=YYYY-MM-DD` endpoint
2. <a name="10.2"></a>The screen SHALL include left/right arrows for day-to-day navigation, with the current day name and date displayed between them
3. <a name="10.3"></a>**Chart 1 — Battery SOC:** filled area chart showing state of charge (%) with Y-axis 0–100%, a dashed line at the 10% cutoff threshold, and the 24h low point annotated with a dot and label showing percentage and time
4. <a name="10.4"></a>**Chart 2 — Power:** multi-line chart showing solar (green filled area), load (dark line), grid import (red line), and grid export (blue line) with Y-axis in watts/kilowatts
5. <a name="10.5"></a>**Chart 3 — Battery power:** single line chart showing charge (positive, above zero line) and discharge (negative, below zero line) with a clearly marked zero line
6. <a name="10.6"></a>All time axes SHALL show 00:00 through 00:00 with intermediate labels
7. <a name="10.7"></a>A day summary card at the bottom SHALL show the same kWh totals as the history view, plus the SOC low point
8. <a name="10.8"></a>WHEN the selected day has only fallback data (from `flux-daily-power`), the SOC chart SHALL render with 5-minute interval data, and the power charts SHALL be empty
9. <a name="10.9"></a>WHEN navigating to a new day, the screen SHALL fetch data from `/day` for that date

---

### 11. Caching

**User Story:** As a user, I want previously loaded history data to be available immediately, so that navigating back to the History screen doesn't require waiting for a network request.

**Acceptance Criteria:**

1. <a name="11.1"></a>The app SHALL cache history responses in SwiftData
2. <a name="11.2"></a>Historical days (before today) SHALL be cached indefinitely — their data is immutable
3. <a name="11.3"></a>Today's history entry SHALL be refreshed on each visit to the History screen
4. <a name="11.4"></a>The Dashboard status response SHALL NOT be cached — it is always fetched fresh

---

### 12. Error States

**User Story:** As a user, I want clear feedback when the app can't reach the backend, so that I know whether I'm looking at stale data.

**Acceptance Criteria:**

1. <a name="12.1"></a>WHEN the backend is unreachable, the app SHALL show the last known data with a staleness indicator and the timestamp of the last successful fetch
2. <a name="12.2"></a>WHEN no data has ever been loaded (first launch with no cache), the app SHALL show a loading state followed by an error message if the fetch fails
3. <a name="12.3"></a>WHEN the backend returns HTTP 401, the app SHALL display an authentication error and offer to navigate to Settings
4. <a name="12.4"></a>Network errors SHALL NOT crash the app or leave it in an unrecoverable state

---

### 13. Navigation

**User Story:** As a user, I want intuitive navigation between screens, so that I can move between Dashboard, History, and Settings without friction.

**Acceptance Criteria:**

1. <a name="13.1"></a>The app SHALL use NavigationSplitView as the root container
2. <a name="13.2"></a>On iPhone, the app SHALL present a single-column layout with push navigation between screens
3. <a name="13.3"></a>On iPad, the app SHALL present a sidebar + detail layout where Dashboard and History can sit side-by-side
4. <a name="13.4"></a>The Dashboard SHALL be the default screen on app launch (after Settings configuration)
5. <a name="13.5"></a>Settings SHALL be accessible from the Dashboard via a navigation bar button
