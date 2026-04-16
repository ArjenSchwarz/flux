# Implementation Explanation: Real-Time Today Energy (T-823)

## Beginner Level

### What Changed

The Flux dashboard shows how much energy your solar panels generated, how much you imported/exported from the grid, and how much your battery charged/discharged — all for today. Previously, these numbers came from the AlphaESS cloud API, which the system only checked every 6 hours. That meant the "today" card could show data that was hours old.

This change makes the system calculate those energy numbers itself, using the live power readings it already collects every 10 seconds. The result is energy totals that are at most 10 seconds old instead of 6 hours old.

### Why It Matters

When you're monitoring your home battery system, you want to see today's energy production and consumption in near-real-time. Stale data makes it hard to understand what's happening right now — for example, whether your solar is keeping up with your usage today, or how much you've exported to the grid so far.

### Key Concepts

- **Power vs Energy**: Power is how fast energy flows right now (measured in watts). Energy is the total amount that has flowed over time (measured in kilowatt-hours, kWh). Power is like the speed of water flowing through a pipe; energy is the total water that has passed through.
- **Trapezoidal integration**: A method to estimate total energy from a series of power snapshots. For each pair of consecutive readings, it averages the two power values and multiplies by the time between them. It's called "trapezoidal" because the shape between two points forms a trapezoid.
- **Reconciliation**: The system now has two sources for today's energy — its own calculation and the AlphaESS cloud data. It picks the higher value for each field, because energy totals only go up throughout the day, so a lower number just means that source hasn't caught up yet.

---

## Intermediate Level

### Changes Overview

Three files changed in production code:

| File | Change |
|------|--------|
| `internal/api/compute.go` | Added `computeTodayEnergy()` and `reconcileEnergy()` |
| `internal/api/status.go` | Replaced DailyEnergyItem-only block with compute + reconcile pipeline |
| `internal/poller/poller.go` | Changed `dailyEnergyInterval` from 6h to 1h |

Plus tests in `compute_test.go` (12 new test cases) and `status_test.go` (3 new integration tests).

### Implementation Approach

**`computeTodayEnergy(readings, midnightUnix)`** filters the existing 24-hour readings slice to only post-midnight entries, then runs trapezoidal integration over consecutive pairs. For directional fields (grid import/export, battery charge/discharge), values are clamped before integration — e.g., `max(pgrid, 0)` captures only grid import, `max(-pgrid, 0)` captures only export. Pairs with gaps over 60 seconds are skipped to avoid interpolating across outages. Returns nil if fewer than 2 readings exist after midnight.

**`reconcileEnergy(computed, stored)`** takes the per-field `max()` of both sources. This works because energy totals are monotonically increasing over a day — a lower value from either source means it hasn't caught up, not that it's more accurate.

**In `handleStatus`**, midnight is computed from the existing `now` (captured once per request via `nowFunc`), then `computeTodayEnergy` runs over `allReadings` (already fetched for rolling averages), and the result is reconciled with the `DailyEnergyItem` from DynamoDB.

No new DynamoDB queries are added — the readings are already in memory.

### Trade-offs

- **Clamped vs precise zero-crossing**: At 10-second intervals, the error from clamping (e.g., treating a reading that crosses from import to export as fully one direction) is negligible. Precise zero-crossing interpolation would add complexity for minimal accuracy gain.
- **60s gap threshold**: Conservative — skips gaps rather than interpolating across them. This slightly underestimates energy during outages but never adds phantom energy.
- **Per-field max reconciliation**: Could briefly show inconsistent values across fields (e.g., solar from computation, grid from AlphaESS) but self-corrects as both sources converge throughout the day.

---

## Expert Level

### Technical Deep Dive

The integration operates in Wh internally, converting to kWh only at the end. The formula per pair is:

```
energy_Wh += ((power_a + power_b) / 2) * dt_seconds / 3600
```

Five accumulators run in parallel (epv, eInput, eOutput, eCharge, eDischarge). The clamping strategy — `max(value, 0)` for one direction, `max(-value, 0)` for the other — loses the small triangle between zero and the actual crossing point within a 10-second interval. For a 5kW system crossing zero, this is at most ~0.007 Wh per crossing event, well below the 2-decimal-place rounding.

The `filtered` slice is built by linear scan. The readings are timestamp-sorted ascending (guaranteed by the DynamoDB query), so binary search would be possible, but the slice is at most ~8640 items (24h at 10s intervals) and the Lambda runs infrequently — the linear scan is measured in microseconds.

### Architecture Impact

This change has zero impact on the API contract — `TodayEnergy` already existed in the response schema. The only observable difference is that values update more frequently and may occasionally be slightly higher than the pure AlphaESS values (due to the per-field max reconciliation).

The poller's `dailyEnergyInterval` change from 6h to 1h is a supporting change — it makes the AlphaESS ground-truth data fresher for reconciliation. The midnight finalizer (00:05) is unchanged and still captures the previous day's final totals.

### Potential Issues

- **DST transitions**: Midnight is computed via `time.Date(..., sydneyTZ)`, which correctly handles DST. On the spring-forward night, the day is 23 hours; on fall-back, 25 hours. The integration handles both correctly because it works with Unix timestamps, not wall-clock durations.
- **Readings gap after midnight**: If the poller restarts around midnight, there may be a gap where `computeTodayEnergy` returns nil (< 2 readings). The fallback to `DailyEnergyItem` handles this gracefully.
- **Accumulation drift**: Over a full day (~8640 pairs), small floating-point errors could accumulate. The `roundEnergy` call at the end (2 decimal places) masks any drift below 0.005 kWh. For a 10kW system running all day, the worst-case accumulation error is well below this threshold.
- **`DailyEnergyItem` intra-day behaviour**: The AlphaESS `GetOneDateEnergy` API may return partial totals during the day (not just end-of-day finalised values). The per-field max reconciliation handles this correctly — whichever source is further ahead wins.

---

## Completeness Assessment

### Fully Implemented
- All 14 requirements (1.1-1.8, 2.1-2.4, 3.1-3.2) are satisfied
- All 7 tasks from the task list are marked complete
- Unit tests cover all specified scenarios (7 for compute, 5 for reconcile)
- Integration tests cover computed-only, reconciled, and fallback paths
- Decision log aligns with implementation

### Not in Scope (by design)
- `updatedAt` timestamp: Mentioned in the requirements introduction narrative but not formalised as an acceptance criterion. The iOS app does not currently use this field. A follow-up ticket would be needed if this becomes a requirement.
