# Requirements: Real-Time Today Energy (T-823)

## Introduction

The `/status` endpoint currently returns today's energy totals (`TodayEnergy`) from the `flux-daily-energy` DynamoDB table, which the poller populates by calling the AlphaESS `GetOneDateEnergy` API every 6 hours. This means the dashboard's "Today" card can be up to 6 hours stale.

The Lambda already fetches all readings from the last 24 hours for rolling averages, cutoff estimation, and sustained grid detection. These readings contain instantaneous power values (`ppv`, `pload`, `pbat`, `pgrid`) sampled every 10 seconds. By integrating power over time, the Lambda can compute near-real-time energy totals with no additional DynamoDB queries.

This feature makes the computed energy the primary data source for `TodayEnergy`, with the AlphaESS-sourced `DailyEnergyItem` serving as a periodic reconciliation/ground-truth source. An `updatedAt` timestamp is added so the iOS app can display when the energy data was last computed.

**Sign convention:** Positive `pbat` = battery discharging, negative `pbat` = battery charging. Positive `pgrid` = grid import, negative `pgrid` = grid export. These match the existing AlphaESS conventions used throughout the codebase.

**Timezone:** "Today" is defined as midnight-to-now in `Australia/Sydney`, consistent with all other date-based operations.

---

### 1. Energy Computation from Readings

**User Story:** As a dashboard user, I want today's energy totals to reflect the latest data, so that I can see meaningful numbers instead of 6-hour-stale values.

**Acceptance Criteria:**

1. <a name="1.1"></a>The Lambda SHALL compute today's energy totals by integrating power readings from midnight (Sydney time) to the current time
2. <a name="1.2"></a>The Lambda SHALL use trapezoidal integration: for each consecutive pair of readings, `energy_Wh += ((power_a + power_b) / 2) * (time_b - time_a) / 3600`, then convert to kWh by dividing by 1000
3. <a name="1.3"></a>The Lambda SHALL compute the following energy fields from readings:
   - `epv` from `ppv` (solar generation, always >= 0)
   - `eInput` from positive `pgrid` values (grid import)
   - `eOutput` from absolute value of negative `pgrid` values (grid export)
   - `eCharge` from absolute value of negative `pbat` values (battery charging)
   - `eDischarge` from positive `pbat` values (battery discharging)
4. <a name="1.4"></a>The Lambda SHALL filter readings to only those with timestamps >= midnight Sydney time on the current day
5. <a name="1.5"></a>The Lambda SHALL use the existing `allReadings` slice already fetched by `handleStatus` — no additional DynamoDB queries
6. <a name="1.6"></a>The Lambda SHALL round all computed energy values using the existing `roundEnergy()` function (2 decimal places)
7. <a name="1.7"></a>WHEN fewer than 2 readings exist for today, the Lambda SHALL fall back to the `DailyEnergyItem` from DynamoDB (existing behaviour)
8. <a name="1.8"></a>The computation SHALL use the `now` time captured once per request (existing `nowFunc` pattern), not call `time.Now()` independently

---

### 2. Reconciliation with AlphaESS Data

**User Story:** As a system operator, I want the computed energy values to be corrected by the authoritative AlphaESS totals periodically, so that small integration errors don't accumulate over the day.

**Acceptance Criteria:**

1. <a name="2.1"></a>WHEN a `DailyEnergyItem` exists for today and the computed energy totals are available, the Lambda SHALL use the higher value for each field between the computed and AlphaESS-sourced values
2. <a name="2.2"></a>The reconciliation strategy SHALL prefer the higher value because energy totals are cumulative and monotonically increasing — a lower value indicates the source hasn't caught up yet, not that it's more accurate
3. <a name="2.3"></a>WHEN no `DailyEnergyItem` exists for today, the Lambda SHALL use the computed values alone
4. <a name="2.4"></a>WHEN fewer than 2 readings exist for today and a `DailyEnergyItem` exists, the Lambda SHALL use the `DailyEnergyItem` values alone

---

### 3. Poller Schedule Change

**User Story:** As a system operator, I want the AlphaESS energy endpoint polled more frequently, so that reconciliation happens sooner and the ground-truth values stay reasonably current.

**Acceptance Criteria:**

1. <a name="3.1"></a>The poller SHALL change `dailyEnergyInterval` from 6 hours to 1 hour
2. <a name="3.2"></a>The midnight finalizer SHALL remain unchanged (runs at 00:05 to capture the previous day's final totals)
