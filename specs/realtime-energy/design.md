# Design: Real-Time Today Energy (T-823)

## Overview

Add a `computeTodayEnergy()` function to the Lambda that integrates today's power readings into energy totals, replacing the 6-hour-stale AlphaESS-sourced values as the primary data source. The AlphaESS `DailyEnergyItem` becomes a reconciliation source, with per-field max() used to pick the best value.

No new DynamoDB queries are needed — the readings are already fetched by `handleStatus`.

## Energy Computation

### Function Signature

```go
func computeTodayEnergy(readings []dynamo.ReadingItem, midnightUnix int64) *TodayEnergy
```

- `readings`: the full 24h readings slice already in memory
- `midnightUnix`: Unix timestamp of midnight Sydney time today (derived from `now`)
- Returns nil if fewer than 2 readings exist after midnight

### Integration Method

Trapezoidal integration over consecutive readings:

```
For each consecutive pair (a, b) where both are >= midnightUnix:
    dt = (b.Timestamp - a.Timestamp) seconds
    energy_Wh += ((power_a + power_b) / 2) * dt / 3600
```

Convert to kWh by dividing by 1000. Round with `roundEnergy()`.

### Field Mapping

| Reading field | Condition | Energy field |
|--------------|-----------|-------------|
| `ppv` | always (>= 0 by nature) | `epv` |
| `pgrid` | when > 0 (importing) | `eInput` |
| `pgrid` | when < 0 (exporting), use abs | `eOutput` |
| `pbat` | when < 0 (charging), use abs | `eCharge` |
| `pbat` | when > 0 (discharging) | `eDischarge` |

For the trapezoidal integration of directional fields (grid, battery), each pair's contribution is computed from the pair's values independently. If both readings in a pair have the same sign, the full trapezoid contributes to that field. If they cross zero, the pair's contribution is split proportionally by time at the zero crossing. 

**Simplification:** Given 10-second sample intervals, zero crossings are brief. The simpler approach — clamp each reading's value to the relevant direction before integrating — produces negligible error and matches the existing code style. Use this approach.

```go
// For eInput: clamp pgrid to positive values
pgridImportA := max(a.Pgrid, 0)
pgridImportB := max(b.Pgrid, 0)
eInputWh += ((pgridImportA + pgridImportB) / 2) * dt / 3600
```

### Edge Cases

- **< 2 readings today**: return nil, fall through to DailyEnergyItem
- **Gap in readings** (> 60s between consecutive readings): skip that pair — don't interpolate across gaps where the system may have been offline
- **First reading after midnight**: only pairs where both readings are >= midnight contribute

## Reconciliation Strategy

In `handleStatus`, after computing from readings and fetching `DailyEnergyItem`:

```go
func reconcileEnergy(computed *TodayEnergy, stored *TodayEnergy) *TodayEnergy
```

- If both are available: return per-field `max()` of each value
- If only computed: return computed
- If only stored: return stored
- If neither: return nil

Per-field max is correct because energy totals are cumulative and monotonically increasing over a day. A lower value means that source hasn't caught up, not that it's more accurate.

## Status Handler Changes

In `handleStatus` (status.go), after Phase 1 queries complete:

```go
// Compute today's energy from readings
midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydneyTZ).Unix()
computedEnergy := computeTodayEnergy(allReadings, midnight)

// Reconcile with DailyEnergyItem
var storedEnergy *TodayEnergy
if deItem != nil {
    storedEnergy = &TodayEnergy{
        Epv:        roundEnergy(deItem.Epv),
        EInput:     roundEnergy(deItem.EInput),
        EOutput:    roundEnergy(deItem.EOutput),
        ECharge:    roundEnergy(deItem.ECharge),
        EDischarge: roundEnergy(deItem.EDischarge),
    }
}
resp.TodayEnergy = reconcileEnergy(computedEnergy, storedEnergy)
```

This replaces the existing block (lines 130-138) that only uses `deItem`.

## Poller Change

Single constant change in `internal/poller/poller.go`:

```go
dailyEnergyInterval = 1 * time.Hour  // was 6 * time.Hour
```

## File Changes Summary

| File | Change |
|------|--------|
| `internal/api/compute.go` | Add `computeTodayEnergy()`, `reconcileEnergy()` |
| `internal/api/compute_test.go` | Tests for both new functions |
| `internal/api/status.go` | Replace `deItem`-only block with compute + reconcile |
| `internal/api/status_test.go` | Update/add tests for new TodayEnergy behaviour |
| `internal/poller/poller.go` | Change `dailyEnergyInterval` to 1 hour |

## Testing Strategy

### Unit Tests (compute_test.go)

`computeTodayEnergy`:
- Empty readings → nil
- Single reading → nil  
- Two readings spanning midnight → only post-midnight pair counted
- Normal day with multiple readings → correct integration
- Gap > 60s between readings → gap skipped
- Mixed sign pgrid/pbat → correct field separation
- Rounding applied correctly

`reconcileEnergy`:
- Both nil → nil
- Only computed → computed returned
- Only stored → stored returned
- Both present → per-field max

### Integration Tests (status_test.go)

- Status with readings but no DailyEnergyItem → computed energy returned
- Status with both → reconciled (max) values returned
- Status with no readings and no DailyEnergyItem → nil TodayEnergy
