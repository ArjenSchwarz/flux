# API Layer — Agent Notes

## Package Structure

`internal/api/` contains:
- `handler.go` — Handler struct with routing, auth, and request logging. Routes to dedicated endpoint files.
- `status.go` — `/status` endpoint: concurrent DynamoDB queries via errgroup, in-memory computation for live data, battery info, rolling averages, off-peak, and today's energy.
- `history.go` — `/history` endpoint: parses days param (7/14/30), queries daily energy, returns sorted array with rounding.
- `day.go` — `/day` endpoint: queries readings, falls back to daily power, downsamples, computes socLow from raw data.
- `response.go` — JSON response structs for all three endpoints (`StatusResponse`, `HistoryResponse`, `DayDetailResponse` and their nested types). Uses pointer types (`*float64`, `*string`) for nullable fields.
- `compute.go` — Pure business logic functions with no DynamoDB dependency.
- `handler_test.go` — Tests for method validation, auth, auth-before-routing, routing, and error response format. Also defines `mockReader` and shared test helpers.
- `status_test.go` — Tests for all /status scenarios: all data present, no readings, offpeak pending/complete, no today energy, system missing/zero cobat fallback, DynamoDB errors, single now capture.
- `history_test.go` — Tests for default/explicit days, invalid days, no data, ascending order, rounding, DynamoDB errors.
- `day_test.go` — Tests for normal case, fallback to daily power, no data, readings without energy, date validation, socLow from raw data, DynamoDB errors.
- `compute_test.go` — Table-driven tests using `map[string]struct` pattern.

## Handler

- `Handler` struct holds: `reader` (dynamo.Reader), `serial`, `apiToken`, `offpeakStart`, `offpeakEnd`, `nowFunc`.
- `nowFunc` defaults to `time.Now`, overridable in tests for deterministic time.
- `Handle` is the Lambda entry point — logs method, path, status, duration via slog. Never logs the token.
- Processing order: method check → auth → routing. Auth runs before routing so invalid tokens get 401 regardless of path.
- `validToken` uses `strings.CutPrefix` for "Bearer " extraction and `subtle.ConstantTimeCompare` for comparison.
- `errorResponse(status, message)` builds `{"error":"message"}` with Content-Type header.
- `jsonResponse(v)` marshals any value to 200 JSON response.

## Status Endpoint

- Captures `now` once via `h.nowFunc()` for time consistency within a request.
- Phase 1: errgroup with 4 concurrent DynamoDB queries (readings 24h, system, offpeak, daily energy). Any failure → 500.
- Phase 2: in-memory computation — extract latest reading, filter to 60s/15min subsets, compute pgridSustained, rolling averages, cutoff estimates, findMinSOC for low24h.
- `filterReadings(readings, from, to)` — returns subset by timestamp range.
- `buildOffpeak(item, windowStart, windowEnd)` — always includes window times, delta fields only when status is "complete".
- `floatPtr(v)` — helper for nullable float64 fields.
- Battery capacity: fallback 13.34 when system missing or cobat == 0.
- Rolling 15min: requires >= 2 readings in window, otherwise null.

## History Endpoint

- `validDays` map for O(1) validation of 7/14/30.
- Date range: `today.AddDate(0, 0, -(days-1))` to today.
- Results come pre-sorted from DynamoDB (ScanIndexForward: true).
- All energy values rounded via `roundEnergy`.

## Day Endpoint

- Date validation: regex + `time.Parse` to catch invalid dates like 2026-13-45.
- Day range: `dayStart.Unix()` to `dayEnd.Unix()-1` (exclusive end).
- Fallback: when no flux-readings, queries flux-daily-power. Maps `cbat` → `soc`, power fields → 0. Not downsampled.
- `socLow` computed from raw data (or fallback cbat) before downsampling.
- `findMinSOCFromPower` — separate helper for daily power items since they use `Cbat` and `UploadTime` instead of `Soc` and `Timestamp`.
- `mapDailyPowerToPoints` — parses `UploadTime` in Sydney timezone.
- Summary is null when neither readings nor daily energy exist.

## Compute Functions

- `computeCutoffTime(soc, pbat, capacityKwh, cutoffPercent, now)` — Linear extrapolation. Returns nil for charging/idle/SOC≤cutoff.
- `computeRollingAverages(readings)` — Mean of pload and pbat. Returns (0,0) for empty.
- `computePgridSustained(readings)` — Iterates backwards from end, counts consecutive pgrid>500 within 30s gaps. Needs 3+ consecutive. Expects ascending order input.
- `downsample(readings, date)` — 288 five-minute buckets, averages per bucket, omits empty. Date parsed in Australia/Sydney timezone.
- `findMinSOC(readings)` — Returns (soc, timestamp, found).
- `roundEnergy(v)` — 2 decimal places (kWh).
- `roundPower(v)` — 1 decimal place (watts/SOC).

## Dependencies

- `golang.org/x/sync/errgroup` — used by status endpoint for concurrent DynamoDB queries.

## Mock Reader

`handler_test.go` defines `mockReader` with function fields for all 6 Reader methods. Default behavior returns empty results (no error). Shared test helpers: `newTestHandler()`, `makeRequest(method, path, authHeader)`.

## Testing Notes

- Tests use `map[string]struct` table-driven pattern with `tc` variable.
- `fixedNow()` returns 2026-04-15 10:00:00 AEST for deterministic tests.
- Status tests inject `nowFunc` to control time capture.
- `TestHandleStatusSingleNowCapture` verifies nowFunc is called exactly once.
- golangci-lint has a version mismatch (built with Go 1.25, project targets 1.26) — not related to API code.
