# API Layer — Agent Notes

## Package Structure

`cmd/api/` contains:
- `main.go` — Lambda entry point. Validates env vars, loads AWS config, fetches SSM params (api-token, serial), creates DynamoReader and Handler, calls `lambda.Start`. Uses JSON slog handler. Imports `time/tzdata` for timezone embedding. Defines `ssmAPI` interface for testability.

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
- `errorResponse(status, message)` builds `{"error":"message"}` via `json.Marshal` with Content-Type header.
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
- Concurrent queries via errgroup: daily energy rows + last 24h of readings. The readings are used to compute today's integrated energy via `computeTodayEnergy`; only the row whose `Date == today` is then reconciled with `reconcileEnergy`. Past rows pass through untouched (the midnight finalizer has already written their authoritative totals).
- This keeps today's row consistent with `/status` and `/day` for today — see T-828.

## Day Endpoint

- Date validation: regex + `time.Parse` to catch invalid dates like 2026-13-45.
- Day range: `dayStart.Unix()` to `dayEnd.Unix()-1` (exclusive end).
- Concurrent queries: readings and daily energy fetched in parallel via errgroup (same pattern as status endpoint).
- Fallback: when no flux-readings, queries flux-daily-power. Maps `cbat` → `soc`, power fields → 0. Not downsampled.
- `socLow` computed from raw data (or fallback cbat) before downsampling.
- `findMinSOCFromPower` — separate helper for daily power items since they use `Cbat` and `UploadTime` instead of `Soc` and `Timestamp`.
- `mapDailyPowerToPoints` — parses `UploadTime` using package-level `sydneyTZ`.
- Summary is null when neither readings nor daily energy exist.
- `nowFunc` is captured once at the top of the handler. When the requested `date` equals today (Sydney local), the summary's energy fields are `reconcileEnergy(computed, stored)` so they match `/status`. For past dates the stored `DailyEnergyItem` passes through untouched. See T-828.

## Compute Functions

- Package-level `sydneyTZ` var loaded once via init function — avoids repeated `time.LoadLocation` calls and silently discarded errors. Panics on load failure (fail-fast).
- `computeCutoffTime(soc, pbat, capacityKwh, cutoffPercent, now)` — Linear extrapolation. Returns nil for charging/idle/SOC≤cutoff.
- `nextOffpeakStart(now, offpeakStart, offpeakEnd)` — Absolute Sydney-local time of the next off-peak window start (today's start if `now < todayEnd`, tomorrow's start otherwise). Returns `(_, false)` for invalid off-peak config. Used by `/status` to suppress cutoff predictions that land at or after the next scheduled charging window (see T-827).
- `computeRollingAverages(readings)` — Mean of pload and pbat. Returns (0,0) for empty.
- `computePgridSustained(readings)` — Iterates backwards from end, counts consecutive pgrid>500 within 30s gaps. Needs 3+ consecutive. Expects ascending order input.
- `downsample(readings, date)` — 288 five-minute buckets, averages per bucket, omits empty. Uses `sydneyTZ`. Output is already in chronological order (buckets iterated 0..287).
- `findMinSOC(readings)` — Returns (soc, timestamp, found).
- `computeTodayEnergy(readings, midnightUnix)` — Integrates power readings into energy totals using trapezoidal integration. Filters to post-midnight readings, skips pairs with gap >60s. Clamps directional values: `max(pgrid,0)` for eInput, `max(-pgrid,0)` for eOutput, `max(-pbat,0)` for eCharge, `max(pbat,0)` for eDischarge. Converts Wh→kWh, rounds with `roundEnergy`. Returns nil if <2 post-midnight readings.
- `reconcileEnergy(computed, stored)` — Takes per-field max of computed (from integration) and stored (from DynamoDB daily energy). Returns nil if both nil, whichever is non-nil if only one exists.
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
- `cmd/api/main_test.go` tests `loadConfig` missing-env-var validation path.
- `compute_test.go` includes benchmarks: `BenchmarkDownsample` (8640 readings) and `BenchmarkComputePgridSustained` (360 readings).
- golangci-lint has a version mismatch (built with Go 1.25, project targets 1.26) — not related to API code.

## Known Issues

- `DaySummary.SocLow` and `SocLowTime` are non-pointer types — they serialise as `0` and `""` when no readings exist instead of null. App must handle defensively. See `specs/lambda-api/implementation.md` validation findings.
- `findMinSOCFromPower` does not validate `UploadTime` format — parsing failures silently produce zero time.
- `computeCutoffTime` has NaN/Inf guards (added during consolidation) to prevent unreasonable cutoff times from very small pbat values.
- `/status` filters computed cutoffs through `nextOffpeakStart`: if the cutoff time is at or after the next off-peak start, both `battery.estimatedCutoffTime` and `rolling15min.estimatedCutoffTime` are returned as null because the battery will be charged during that window (T-827).
