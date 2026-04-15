# Requirements: Lambda API

## Introduction

The Lambda API is the server-side component that serves the Flux iOS app. It runs as a Go binary on AWS Lambda behind a Function URL, reads from five DynamoDB tables populated by the Fargate poller, and returns JSON responses for three endpoints: `/status`, `/history`, and `/day`.

The Lambda handles all derived computations (rolling averages, cutoff estimates, sustained grid detection) so the app receives ready-to-display data. Authentication uses a shared bearer token stored in SSM Parameter Store. The system is single-user (one AlphaESS system, two app users).

**Sign convention:** In the AlphaESS data model, positive `pbat` means the battery is discharging (supplying power to the house), and negative `pbat` means the battery is charging. Positive `pgrid` means importing from the grid, negative `pgrid` means exporting to the grid. The API preserves these conventions. The app may negate values for chart rendering.

**Timezone:** All date-based operations (determining "today", date range queries) use `Australia/Sydney` time, matching the poller. All timestamps in API responses use ISO 8601 UTC format.

**Deferred to V2+:** Estimated time to full charge ("Charging · full ~2:45 PM"), `eGridCharge` in API responses, CORS headers (not needed for native iOS).

---

### 1. Implementation Constraints

**User Story:** As a developer, I want the Lambda to follow established project conventions, so that the codebase remains consistent and maintainable.

**Acceptance Criteria:**

1. <a name="1.1"></a>The Lambda SHALL be written in Go, consistent with the existing poller codebase  
2. <a name="1.2"></a>The Lambda SHALL compile to a standalone binary named `bootstrap` for the `provided.al2023` Lambda runtime  
3. <a name="1.3"></a>The Lambda SHALL target `linux/arm64` (Graviton) architecture  
4. <a name="1.4"></a>The Lambda SHALL use the `aws-lambda-go` library for the Lambda handler entry point  
5. <a name="1.5"></a>The Lambda SHALL reuse the existing `internal/dynamo` package for DynamoDB models and table name configuration  
6. <a name="1.6"></a>The Lambda SHALL use the AWS SDK for Go v2 (`aws-sdk-go-v2`) for DynamoDB and SSM access, consistent with the poller  
7. <a name="1.7"></a>The Lambda entry point SHALL be in `cmd/api/`  
8. <a name="1.8"></a>The Lambda SHALL be buildable via the project's `Makefile`  

---

### 2. Authentication and Authorisation

**User Story:** As the app, I want to authenticate via a bearer token, so that only authorised clients can access battery data.

**Acceptance Criteria:**

1. <a name="2.1"></a>The Lambda SHALL validate an `Authorization: Bearer {token}` header on every request  
2. <a name="2.2"></a>The Lambda SHALL load the expected token from SSM Parameter Store (`/flux/api-token`) during cold start and cache it for the lifetime of the warm instance  
3. <a name="2.3"></a>WHEN the `Authorization` header is missing or the token does not match, the Lambda SHALL return HTTP 401 with `{"error": "unauthorized"}`  
4. <a name="2.4"></a>The Lambda SHALL use constant-time comparison (`crypto/subtle.ConstantTimeCompare`) for token validation  
5. <a name="2.5"></a>The Lambda SHALL evaluate authentication before routing — all unauthenticated requests receive HTTP 401 regardless of path  
6. <a name="2.6"></a>The Lambda SHALL load the system serial number from SSM Parameter Store (`/flux/serial`) during cold start and cache it for the lifetime of the warm instance  
7. <a name="2.7"></a>The Lambda SHALL assume a single system — no serial number parameter is accepted from the client  

---

### 3. Status Endpoint — Live Data

**User Story:** As the app, I want to fetch the current system status in a single request, so that the Dashboard screen can display all live data without multiple round trips.

**Acceptance Criteria:**

1. <a name="3.1"></a>The Lambda SHALL expose a `GET /status` endpoint  
2. <a name="3.2"></a>The response SHALL include a `live` object containing: `ppv`, `pload`, `pbat`, `pgrid`, `soc`, and `timestamp` fields from the most recent `flux-readings` record  
3. <a name="3.3"></a>The `timestamp` field SHALL be formatted as an ISO 8601 UTC string (e.g., `2026-04-11T21:47:00Z`)  
4. <a name="3.4"></a>IF `flux-readings` contains no records, the `live` object SHALL be `null`  
5. <a name="3.5"></a>The response SHALL include a `pgridSustained` boolean in the `live` object  
6. <a name="3.6"></a>The Lambda SHALL determine `pgridSustained` by inspecting the most recent readings within a 60-second window. Readings more than 30 seconds apart SHALL NOT be considered consecutive  
7. <a name="3.7"></a>WHEN 3 or more consecutive readings (each within 30 seconds of the previous) show `pgrid > 500`, `pgridSustained` SHALL be `true`  
8. <a name="3.8"></a>WHEN the consecutive threshold is not met, `pgridSustained` SHALL be `false`  

---

### 4. Status Endpoint — Battery Information

**User Story:** As the app, I want battery capacity and cutoff estimates included in the status response, so that the Dashboard can show meaningful battery state beyond raw percentage.

**Acceptance Criteria:**

1. <a name="4.1"></a>The response SHALL include a `battery` object containing `capacityKwh` from the `flux-system` table (`cobat` field)  
2. <a name="4.2"></a>IF the `flux-system` record is missing or `cobat` is 0, `capacityKwh` SHALL use the fallback value of `13.34`  
3. <a name="4.3"></a>The `battery` object SHALL include a `cutoffPercent` field with a fixed value of `10`  
4. <a name="4.4"></a>WHEN the battery is discharging (`pbat > 0`) and SOC is above `cutoffPercent`, the `battery` object SHALL include an `estimatedCutoffTime` calculated as:  
   `remainingKwh = (soc - cutoffPercent) / 100 × capacityKwh`  
   `hoursRemaining = remainingKwh / (pbat / 1000)`  
   `estimatedCutoffTime = now + hoursRemaining`  
5. <a name="4.5"></a>WHEN the battery is charging or idle (`pbat <= 0`), `estimatedCutoffTime` SHALL be `null`  
6. <a name="4.6"></a>WHEN SOC is at or below `cutoffPercent`, `estimatedCutoffTime` SHALL be `null`  
7. <a name="4.7"></a>The `estimatedCutoffTime` SHALL be formatted as an ISO 8601 UTC string  
8. <a name="4.8"></a>The `battery` object SHALL include a `low24h` object containing `soc` and `timestamp` for the lowest SOC reading in the last 24 hours from `flux-readings`  
9. <a name="4.9"></a>The `low24h.timestamp` SHALL be formatted as an ISO 8601 UTC string  
10. <a name="4.10"></a>IF no readings exist in the last 24 hours, `low24h` SHALL be `null`  

---

### 5. Status Endpoint — Rolling Averages

**User Story:** As the app, I want rolling 15-minute averages and a separate cutoff estimate, so that the Dashboard shows smoothed data that filters out momentary spikes.

**Acceptance Criteria:**

1. <a name="5.1"></a>The response SHALL include a `rolling15min` object  
2. <a name="5.2"></a>The Lambda SHALL query the last 15 minutes of `flux-readings` to compute the rolling averages  
3. <a name="5.3"></a>The `rolling15min` object SHALL include `avgLoad` — the mean of `pload` values over the last 15 minutes  
4. <a name="5.4"></a>The `rolling15min` object SHALL include `avgPbat` — the mean of `pbat` values over the last 15 minutes  
5. <a name="5.5"></a>WHEN the rolling average battery discharge rate is positive (`avgPbat > 0`) and SOC is above `cutoffPercent`, the `rolling15min` object SHALL include an `estimatedCutoffTime` using the same formula as [4.4](#4.4) but substituting `avgPbat` for `pbat`  
6. <a name="5.6"></a>WHEN the rolling average indicates charging or idle (`avgPbat <= 0`), or SOC is at or below `cutoffPercent`, `estimatedCutoffTime` SHALL be `null`  
7. <a name="5.7"></a>IF fewer than 2 readings exist in the 15-minute window, the `rolling15min` object SHALL be `null`  

---

### 6. Status Endpoint — Off-Peak Data

**User Story:** As the app, I want off-peak energy data in the status response, so that the Dashboard can show how much energy was consumed and charged during the off-peak window.

**Acceptance Criteria:**

1. <a name="6.1"></a>The response SHALL include an `offpeak` object  
2. <a name="6.2"></a>The `offpeak` object SHALL include `windowStart` and `windowEnd` fields (e.g., `"11:00"`, `"14:00"`) read from the Lambda's environment variables (`OFFPEAK_START`, `OFFPEAK_END`)  
3. <a name="6.3"></a>The `offpeak` object SHALL include `gridUsageKwh`, `solarKwh`, `batteryChargeKwh`, `batteryDischargeKwh`, `gridExportKwh`, and `batteryDeltaPercent` from the `flux-offpeak` table for today's date (in `Australia/Sydney` timezone)  
4. <a name="6.4"></a>WHEN today's off-peak record has status `"complete"`, the Lambda SHALL return the computed delta values  
5. <a name="6.5"></a>WHEN today's off-peak record has status `"pending"` or does not exist, the off-peak delta fields SHALL be `null`  

---

### 7. Status Endpoint — Today's Energy

**User Story:** As the app, I want today's energy totals in the status response, so that the Dashboard can show cumulative generation, import, export, charge, and discharge for the current day.

**Acceptance Criteria:**

1. <a name="7.1"></a>The response SHALL include a `todayEnergy` object  
2. <a name="7.2"></a>The `todayEnergy` object SHALL include `epv`, `eInput`, `eOutput`, `eCharge`, and `eDischarge` from the `flux-daily-energy` table for today's date (in `Australia/Sydney` timezone)  
3. <a name="7.3"></a>IF no daily energy record exists for today, the `todayEnergy` object SHALL be `null`  

---

### 8. History Endpoint

**User Story:** As the app, I want to fetch daily energy summaries for a date range, so that the History screen can display a grouped bar chart of energy data.

**Acceptance Criteria:**

1. <a name="8.1"></a>The Lambda SHALL expose a `GET /history` endpoint  
2. <a name="8.2"></a>The endpoint SHALL accept an optional `days` query parameter with a default value of `7`  
3. <a name="8.3"></a>The `days` parameter SHALL accept values of `7`, `14`, or `30`  
4. <a name="8.4"></a>WHEN `days` has an invalid value, the Lambda SHALL return HTTP 400 with `{"error": "invalid days parameter, must be 7, 14, or 30"}`  
5. <a name="8.5"></a>The response SHALL include a `days` array of daily energy objects, each containing: `date`, `epv`, `eInput`, `eOutput`, `eCharge`, and `eDischarge`  
6. <a name="8.6"></a>The `days` array SHALL be sorted in ascending date order (oldest first)  
7. <a name="8.7"></a>The Lambda SHALL query `flux-daily-energy` for the requested date range, calculated from today's date in `Australia/Sydney` timezone  
8. <a name="8.8"></a>WHEN no records exist for the requested range, the `days` array SHALL be empty  

---

### 9. Day Detail Endpoint

**User Story:** As the app, I want time-series readings and a summary for a specific day, so that the Day Detail screen can render SOC, power, and battery charts.

**Acceptance Criteria:**

1. <a name="9.1"></a>The Lambda SHALL expose a `GET /day` endpoint  
2. <a name="9.2"></a>The endpoint SHALL require a `date` query parameter in `YYYY-MM-DD` format  
3. <a name="9.3"></a>WHEN the `date` parameter is missing or not in valid `YYYY-MM-DD` format, the Lambda SHALL return HTTP 400 with `{"error": "invalid or missing date parameter"}`  
4. <a name="9.4"></a>The response SHALL include a `date` field echoing the requested date  
5. <a name="9.5"></a>The response SHALL include a `readings` array of time-series objects, each containing: `timestamp`, `ppv`, `pload`, `pbat`, `pgrid`, and `soc`  
6. <a name="9.6"></a>The Lambda SHALL downsample `flux-readings` data by dividing the day into 5-minute buckets (00:00–00:05, 00:05–00:10, ..., 23:55–00:00) and averaging all readings within each bucket, producing approximately 288 data points per day. Buckets with no readings SHALL be omitted  
7. <a name="9.7"></a>WHEN `flux-readings` has no data for the requested date, the Lambda SHALL fall back to `flux-daily-power` for SOC data, mapping the `cbat` field to `soc` in the response, with power fields (`ppv`, `pload`, `pbat`, `pgrid`) set to `0`  
8. <a name="9.8"></a>The `readings` array SHALL be sorted in ascending timestamp order  
9. <a name="9.9"></a>The response SHALL include a `summary` object containing: `epv`, `eInput`, `eOutput`, `eCharge`, `eDischarge` from the `flux-daily-energy` table, plus `socLow` and `socLowTime` computed from the raw (pre-downsampled) readings  
10. <a name="9.10"></a>`socLow` SHALL be the minimum SOC value from the day's raw readings (not the downsampled data)  
11. <a name="9.11"></a>`socLowTime` SHALL be the timestamp of the minimum SOC reading, formatted as ISO 8601 UTC  
12. <a name="9.12"></a>IF no readings exist for the requested date (from either `flux-readings` or `flux-daily-power`), the `readings` array SHALL be empty  
13. <a name="9.13"></a>IF no `flux-daily-energy` record exists for the requested date, the `summary` object SHALL contain only `socLow` and `socLowTime` (derived from readings), with energy fields set to `null`  
14. <a name="9.14"></a>IF neither readings nor daily energy data exist, the Lambda SHALL return HTTP 200 with empty `readings` array and `null` summary  

---

### 10. Response Format and Error Handling

**User Story:** As the app, I want consistent JSON response formats and clear error messages, so that I can reliably parse responses and display appropriate error states.

**Acceptance Criteria:**

1. <a name="10.1"></a>All successful responses SHALL return HTTP 200 with `Content-Type: application/json`  
2. <a name="10.2"></a>All error responses SHALL return the appropriate HTTP status code (400, 401, 404, 405, 500) with `Content-Type: application/json` and a body of `{"error": "message"}`  
3. <a name="10.3"></a>WHEN the Lambda encounters a DynamoDB error, it SHALL return HTTP 500 with `{"error": "internal error"}`  
4. <a name="10.4"></a>The Lambda SHALL NOT expose internal error details (table names, stack traces) in error responses  
5. <a name="10.5"></a>WHEN the request path does not match any endpoint, the Lambda SHALL return HTTP 404 with `{"error": "not found"}`  
6. <a name="10.6"></a>WHEN the request method is not GET, the Lambda SHALL return HTTP 405 with `{"error": "method not allowed"}`  
7. <a name="10.7"></a>Energy values (kWh) in responses SHALL be rounded to two decimal places. Power values (watts) and SOC (percentage) SHALL be rounded to one decimal place  

---

### 11. Runtime Configuration

**User Story:** As an operator, I want the Lambda to load configuration from environment variables and SSM, so that I can deploy and reconfigure without code changes.

**Acceptance Criteria:**

1. <a name="11.1"></a>The Lambda SHALL read DynamoDB table names from environment variables: `TABLE_READINGS`, `TABLE_DAILY_ENERGY`, `TABLE_DAILY_POWER`, `TABLE_SYSTEM`, `TABLE_OFFPEAK`  
2. <a name="11.2"></a>The Lambda SHALL read the off-peak window from environment variables: `OFFPEAK_START`, `OFFPEAK_END`  
3. <a name="11.3"></a>The Lambda SHALL read SSM parameter paths from environment variables: `API_TOKEN_PARAM`, `SYSTEM_SERIAL_PARAM`  
4. <a name="11.4"></a>The Lambda SHALL resolve SSM parameters to their values during cold start initialisation  
5. <a name="11.5"></a>The Lambda SHALL read the timezone from the `TZ` environment variable (e.g., `Australia/Sydney`) for all date-based operations  
6. <a name="11.6"></a>WHEN an SSM parameter cannot be loaded during cold start, the Lambda SHALL log the error and fail to initialise (returning 500 on all requests)  
7. <a name="11.7"></a>WHEN a required environment variable is missing, the Lambda SHALL fail to initialise with a clear error log message identifying the missing variable  

---

### 12. Observability

**User Story:** As an operator, I want the Lambda to produce structured logs, so that I can diagnose issues via CloudWatch.

**Acceptance Criteria:**

1. <a name="12.1"></a>The Lambda SHALL use structured JSON logging (via `slog`)  
2. <a name="12.2"></a>The Lambda SHALL log each request with: method, path, status code, and duration  
3. <a name="12.3"></a>The Lambda SHALL log errors with sufficient context to diagnose the failure (table name, operation, error message)  
4. <a name="12.4"></a>The Lambda SHALL NOT log the bearer token or SSM parameter values  
