# API Layer — Agent Notes

## Package Structure

`internal/api/` contains:
- `response.go` — JSON response structs for all three endpoints (`StatusResponse`, `HistoryResponse`, `DayDetailResponse` and their nested types). Uses pointer types (`*float64`, `*string`) for nullable fields.
- `compute.go` — Pure business logic functions with no DynamoDB dependency.
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
