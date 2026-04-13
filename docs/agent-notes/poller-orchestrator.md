# Poller Orchestrator — Agent Notes

## Package Structure

`internal/poller/` contains:
- `poller.go` — Poller struct, Run(), 4 polling goroutines, midnight finalizer, fetchAndStore* helpers
- `offpeak.go` — OffpeakScheduler, snapshot capture with retry, delta computation, mid-window recovery
- `poller_test.go` — Tests for fetchAndStore* helpers, nextLocalMidnight, dry-run logging
- `offpeak_test.go` — Tests for delta computation, time position, snapshot retry, recovery

## Key Design Choices

- `APIClient` interface defined in poller package (not alphaess) to decouple from concrete client. Methods: GetLastPowerData, GetOneDayPower, GetOneDateEnergy, GetEssList.
- Two-context pattern: `loopCtx` (cancelled by SIGTERM) stops ticker loops, `drainCtx` (25s timeout) allows in-flight ops to complete. Each goroutine receives both.
- `fetchAndStoreDailyEnergy` accepts an optional date parameter — empty string means "today". Used by both the 6h ticker (today) and midnight finalizer (yesterday).
- `nextLocalMidnight` uses `time.Date(year, month, day+1, 0, 0, 0, 0, loc)` for DST safety.
- Off-peak scheduler uses `retryDelay` field (overridable for tests, defaults to 10s).
- `timePosition` computes elapsed time from midnight to determine before/during/after window.
- `wallClockTime` constructs a specific wall-clock time using `time.Date` for DST safety.
- Mid-window recovery: queries store for pending record, recovers start snapshot from it. Store errors are logged and skipped (not propagated).
- Failed end snapshot: deletes pending record via `store.DeleteOffpeak`.

## Testing Patterns

- Tests use `mockClient` and `mockStore` with function fields and call counters.
- `captureLog()` helper redirects slog to a buffer for log assertion tests.
- `retryMockClient` wraps mockClient to override individual methods for retry testing.
- `testPoller()` helper creates a Poller with sensible defaults and optional config overrides via functional options.
- Off-peak tests use short `retryDelay` (1ms) to avoid slow tests.

## Infrastructure Changes

- Added `dynamodb:DeleteItem` to TaskRole policy (needed for off-peak pending record cleanup).
- Added TABLE_READINGS, TABLE_DAILY_ENERGY, TABLE_DAILY_POWER, TABLE_SYSTEM, TABLE_OFFPEAK env vars to container definition.
- Added TZ=Australia/Sydney env var.
