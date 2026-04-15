# API Layer — Agent Notes

## Package Structure

`internal/api/` contains:
- `handler.go` — Handler struct with routing, auth, and request logging. Endpoint stubs for `/status`, `/history`, `/day` return minimal valid JSON.
- `response.go` — JSON response structs for all three endpoints (`StatusResponse`, `HistoryResponse`, `DayDetailResponse` and their nested types). Uses pointer types (`*float64`, `*string`) for nullable fields.
- `compute.go` — Pure business logic functions with no DynamoDB dependency.
- `handler_test.go` — Tests for method validation, auth, auth-before-routing, routing, and error response format.
- `compute_test.go` — Table-driven tests using `map[string]struct` pattern.

## Compute Functions

- `computeCutoffTime(soc, pbat, capacityKwh, cutoffPercent, now)` — Linear extrapolation. Returns nil for charging/idle/SOC≤cutoff.
- `computeRollingAverages(readings)` — Mean of pload and pbat. Returns (0,0) for empty.
- `computePgridSustained(readings)` — Iterates backwards from end, counts consecutive pgrid>500 within 30s gaps. Needs 3+ consecutive. Expects ascending order input.
- `downsample(readings, date)` — 288 five-minute buckets, averages per bucket, omits empty. Date parsed in Australia/Sydney timezone.
- `findMinSOC(readings)` — Returns (soc, timestamp, found).
- `roundEnergy(v)` — 2 decimal places (kWh).
- `roundPower(v)` — 1 decimal place (watts/SOC).

## Key Design Choices

- All compute functions are pure — they take data in and return results, no I/O.
- `downsample` uses Australia/Sydney timezone for bucket boundaries to match the poller's date conventions.
- `computePgridSustained` only evaluates the *current* run from the end of the readings, not historical bursts.
- Threshold is strictly `> 500`, not `>= 500`.

## Testing Notes

- Tests use `map[string]struct` table-driven pattern with `tc` variable.
- `TestDownsample` creates readings using a helper that builds timestamps in Australia/Sydney timezone.
- `TestComputeCutoffTime` uses `assert.WithinDuration` with 1ms tolerance for float→duration conversion.
- golangci-lint has a version mismatch (built with Go 1.25, project targets 1.26) — not related to API code.

## Handler

- `Handler` struct holds: `reader` (dynamo.Reader), `serial`, `apiToken`, `offpeakStart`, `offpeakEnd`.
- `Handle` is the Lambda entry point — logs method, path, status, duration via slog. Never logs the token.
- Processing order: method check → auth → routing. Auth runs before routing so invalid tokens get 401 regardless of path.
- `validToken` uses `strings.CutPrefix` for "Bearer " extraction and `subtle.ConstantTimeCompare` for comparison.
- `errorResponse(status, message)` builds `{"error":"message"}` with Content-Type header.
- `jsonResponse(v)` marshals any value to 200 JSON response.
- Endpoint handlers (`handleStatus`, `handleHistory`, `handleDay`) are stubs returning minimal valid JSON — to be implemented in later tasks.

## Mock Reader

`handler_test.go` defines `mockReader` with function fields for all 6 Reader methods. Default behavior returns empty results (no error). Shared test helpers: `newTestHandler()`, `makeRequest(method, path, authHeader)`.
