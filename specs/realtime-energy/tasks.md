---
references:
    - specs/realtime-energy/requirements.md
    - specs/realtime-energy/design.md
    - specs/realtime-energy/decision_log.md
---
# Real-Time Today Energy (T-823)

## Core Compute Functions

- [ ] 1. Write tests for computeTodayEnergy() <!-- id:ao4cq06 -->
  - Write map-based table-driven tests in internal/api/compute_test.go
  - Test cases: empty readings returns nil, single reading returns nil, two readings after midnight computes correct energy, readings spanning midnight only counts post-midnight pairs, gap >60s between readings skips that pair, mixed sign pgrid/pbat maps to correct fields (eInput vs eOutput, eCharge vs eDischarge), rounding matches roundEnergy() output
  - Use assert.InDelta() for floating point comparisons
  - Follow existing test patterns: map[string]struct with t.Run()
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7)

- [ ] 2. Implement computeTodayEnergy() <!-- id:ao4cq07 -->
  - Add function to internal/api/compute.go
  - Signature: func computeTodayEnergy(readings []dynamo.ReadingItem, midnightUnix int64) *TodayEnergy
  - Filter readings to those >= midnightUnix, return nil if <2 remain
  - Iterate consecutive pairs, skip pairs with gap >60s (Decision 4)
  - Use trapezoidal integration with clamped directional values (Decision 3)
  - max(pgrid,0) for eInput, max(-pgrid,0) for eOutput, max(-pbat,0) for eCharge, max(pbat,0) for eDischarge
  - Convert Wh to kWh, round with roundEnergy()
  - Blocked-by: ao4cq06 (Write tests for computeTodayEnergy())
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8)

- [ ] 3. Write tests for reconcileEnergy() <!-- id:ao4cq08 -->
  - Write map-based table-driven tests in internal/api/compute_test.go
  - Test cases: both nil returns nil, only computed returns computed, only stored returns stored, both present returns per-field max, mixed values where some fields higher in computed and some in stored
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4)

- [ ] 4. Implement reconcileEnergy() <!-- id:ao4cq09 -->
  - Add function to internal/api/compute.go
  - Signature: func reconcileEnergy(computed *TodayEnergy, stored *TodayEnergy) *TodayEnergy
  - Return nil if both nil
  - Return whichever is non-nil if only one exists
  - Return per-field max() when both exist (Decision 2)
  - Blocked-by: ao4cq08 (Write tests for reconcileEnergy())
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4)

## Integration

- [ ] 5. Write tests for computed energy in handleStatus <!-- id:ao4cq0a -->
  - Add/update tests in internal/api/status_test.go
  - Test: readings present with no DailyEnergyItem returns computed energy
  - Test: both readings and DailyEnergyItem returns reconciled max values
  - Test: no readings and no DailyEnergyItem returns nil TodayEnergy
  - Test: single reading with DailyEnergyItem uses DailyEnergyItem values only
  - Blocked-by: ao4cq07 (Implement computeTodayEnergy()), ao4cq09 (Implement reconcileEnergy())
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5), [1.7](requirements.md#1.7), [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4)

- [ ] 6. Wire computeTodayEnergy and reconcileEnergy into handleStatus <!-- id:ao4cq0b -->
  - Modify internal/api/status.go handleStatus function
  - Compute midnight Unix from now using sydneyTZ
  - Call computeTodayEnergy(allReadings, midnight)
  - Build stored TodayEnergy from deItem if present
  - Call reconcileEnergy(computed, stored) and assign to resp.TodayEnergy
  - Replaces existing deItem-only block (lines 130-138)
  - Blocked-by: ao4cq0a (Write tests for computed energy in handleStatus)
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4)

## Poller

- [ ] 7. Reduce daily energy poll interval to 1 hour <!-- id:ao4cq0c -->
  - Change dailyEnergyInterval from 6 * time.Hour to 1 * time.Hour in internal/poller/poller.go
  - No other changes needed - midnight finalizer remains at 00:05
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2)
