# Bugfix Report: Today overview values differ between Dashboard and History/Detail

**Date:** 2026-04-17
**Status:** Investigating
**Transit:** T-828

## Description of the Issue

Today's energy totals shown on the iOS Dashboard disagree with the values shown on the History summary card and the Day Detail summary card for the same day. The mismatch is most visible in the paired fields:

- Grid import (`eInput`) and grid export (`eOutput`)
- Battery charge / discharge (`eCharge` / `eDischarge`)

Solar (`epv`) is typically closer because it is unsigned. The paired fields drift because they come from different computations on different data freshness windows.

**Reproduction steps:**
1. Open the iOS app and view the Dashboard mid-day.
2. Note the Today card's Grid (+/-) and Battery (+/-) values.
3. Navigate to History and tap today's row (or open Day Detail for today).
4. Compare the Summary's Grid and Battery values — they are lower than the Dashboard's.

**Impact:** User-facing correctness. Two screens showing different numbers for the same concept ("today") is confusing and undermines trust in the data, even though both values are technically self-consistent.

## Investigation Summary

Compared the three Lambda endpoints that serve "today" fields:

- `internal/api/status.go:130` — `/status`: computes energy from 24h of readings via `computeTodayEnergy`, then calls `reconcileEnergy(computed, stored)` which takes the **per-field max** of the integration result and the stored `DailyEnergyItem`.
- `internal/api/day.go:102` — `/day`: populates the summary straight from `DailyEnergyItem` (stored only). No readings-based reconciliation.
- `internal/api/history.go:38` — `/history`: maps `DailyEnergyItem`s to `DayEnergy` rows directly (stored only). No reconciliation for today's row either.

The stored `DailyEnergyItem` is refreshed hourly by the poller (`dailyEnergyInterval = 1 * time.Hour`), polling AlphaESS's `getOneDateEnergyBySn`. Live readings are stored every 10 seconds. Therefore, for today, stored totals are up to 1 hour stale (plus AlphaESS's own aggregation lag), while `/status` presents a reconciled figure that tracks within ~10s.

- **Symptoms examined:** User reports of differing Grid (+/-) and Battery (+/-) values between Dashboard and History/Day Detail for today.
- **Code inspected:** `internal/api/status.go`, `internal/api/day.go`, `internal/api/history.go`, `internal/api/compute.go`, `internal/poller/poller.go`.
- **Hypotheses tested:**
  - Different sign conventions between endpoints — ruled out, all endpoints use the same field definitions from `DailyEnergyItem`.
  - Timezone mismatch — ruled out, all endpoints use `sydneyTZ` via the compute package.
  - Rounding differences — ruled out, all endpoints use `roundEnergy` identically.

## Discovered Root Cause

`/day` and `/history` do not reconcile today's stored `DailyEnergyItem` with a live integration of today's power readings, while `/status` does. That asymmetry is visible on the iOS app as inconsistent "today" values.

**Defect type:** Logic inconsistency across endpoints; missing reconciliation in `/day` and `/history` for the current day.

**Why it occurred:** When the `/day` and `/history` endpoints were added, they treated `DailyEnergyItem` as authoritative for all dates. For finalized (past) days this is correct — the midnight finalizer writes the final totals and readings within the 30-day TTL are supplementary. For today, however, the stored value is a lagging indicator and is not an authoritative "current" total.

**Contributing factors:**
- The reconciliation helper (`reconcileEnergy`) was used only by `/status`, not treated as a shared primitive across all endpoints that surface today's totals.
- No cross-endpoint consistency test caught the drift.

## Resolution for the Issue

_(filled in after implementation)_

## Regression Test

**Test file:** `internal/api/day_test.go`, `internal/api/history_test.go`

**Test names:**
- `TestHandleDayTodayReconcilesEnergy` — verifies `/day` reconciles for today.
- `TestHandleDayPastDateDoesNotReconcile` — locks in the scope: past dates keep the stored values.
- `TestHandleHistoryReconcilesTodaysRow` — verifies `/history` reconciles today's row only.

**What they verify:** When the stored `DailyEnergyItem` for today lags behind values integrated from recent readings, the API response reflects the per-field max (same contract as `/status`). Past days continue to pass stored values through unchanged.

**Run command:** `go test ./internal/api/ -run 'TestHandleDayTodayReconcilesEnergy|TestHandleDayPastDateDoesNotReconcile|TestHandleHistoryReconcilesTodaysRow' -v`

## Affected Files

_(filled in after implementation)_

## Verification

**Automated:**
- [ ] Regression tests pass
- [ ] Full test suite passes
- [ ] `make lint` / golangci-lint passes

**Manual verification:**
- Compare Dashboard vs Day Detail vs History summary on the iOS app after deploy.

## Prevention

- When adding an endpoint that surfaces a concept also served by another endpoint, consider whether they should share a single primitive (here: `reconcileEnergy`).
- Add a cross-endpoint consistency assertion (e.g., a test that feeds identical fixtures to `/status`, `/day`, and `/history` for today and checks the shared fields agree).

## Related

- Transit: T-828
- Branch: `T-828/bugfix-today-overview-discrepancy`
