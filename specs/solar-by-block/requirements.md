# Requirements: Solar by Block

## Introduction

The Day Detail page already breaks the day into five usage blocks (night, morning peak, off-peak, afternoon peak, evening) and shows kWh consumed per block. This feature adds solar production (kWh) for the three daylight blocks — morning peak, off-peak, and afternoon peak — so the user can reason about how much of each block's load was self-generated versus drawn from grid or battery, especially in winter when solar is limited.

"Solar" in this document means production measured at the panels (the integral of `ppv`), not the share self-consumed. A block's solar value can therefore exceed the block's load when generation is exported to grid or charging battery.

## Non-Goals

- Showing solar-by-block on Dashboard, History views, or any surface other than Day Detail.
- Showing solar values on the night or evening blocks.
- Adding new charts (the existing power chart already shows ppv).
- Storing per-block solar in any new DynamoDB table; the existing `flux-daily-energy` block schema is extended.
- Real-time per-block solar updates faster than the existing Day Detail refresh cadence.

## Requirements

### 1. Backend computes solar per daylight block

**User Story:** As an iOS app consumer of the `/day` API, I want each daylight block to include solar energy produced during that window, so that the UI can render solar alongside usage without doing its own integration.

**Acceptance Criteria:**

1. <a name="1.1"></a>The `/day` response SHALL include a solar energy value (kWh) on each block whose `kind` is `morningPeak`, `offPeak`, or `afternoonPeak`.  
2. <a name="1.2"></a>The `/day` response SHALL omit (or set to null) the solar field on blocks whose `kind` is `night` or `evening`.  
3. <a name="1.3"></a>The solar value on a block SHALL equal the trapezoidal integral of `ppv` (kW) over the block's `[start, end)` window, expressed in kWh.  
4. <a name="1.4"></a>WHEN the day has no `ppv` samples in a given daylight block (e.g., poller gap or block has not started), the API SHALL report the solar value for that block as null.  
5. <a name="1.5"></a>WHEN the day is in progress (today), blocks whose end time has not yet passed SHALL still report the integral of solar produced so far in that block's elapsed portion.  

### 2. Persistence and backfill

**User Story:** As the operator, I want solar-per-block values to be stored alongside the existing block totals so that historical Day Detail views show solar without recomputing from raw readings, which expire after 30 days.

**Acceptance Criteria:**

1. <a name="2.1"></a>The DynamoDB record that stores `DailyUsageBlock` data (in `flux-daily-energy`) SHALL be extended to persist the solar kWh value per daylight block.  
2. <a name="2.2"></a>WHEN the daily summarisation pass computes blocks for a daylight block that contains at least one `ppv` sample, it SHALL persist the computed solar value to DynamoDB for that block.  
3. <a name="2.3"></a>WHEN the daily summarisation pass computes blocks for a daylight block that contains no `ppv` samples, it SHALL NOT write to that block's solar attribute (any previously persisted value is preserved).  
4. <a name="2.4"></a>A standalone Go CLI (under `cmd/`) SHALL be provided that backfills solar-per-block values for all days in `flux-daily-energy` whose corresponding `flux-readings` are still available, applying the same persistence rules as 2.2 and 2.3.  
5. <a name="2.5"></a>The backfill CLI SHALL be idempotent: running it twice with no new readings SHALL produce no net change in DynamoDB state.  
6. <a name="2.6"></a>WHEN a stored block has no solar attribute (because raw readings were expired before the feature deployed, or because no `ppv` samples were ever observed in that block), the API SHALL return null for that block's solar field.  
7. <a name="2.7"></a>The API contract SHALL remain backwards-compatible: existing clients that ignore the new field SHALL continue to function without error.  

### 3. iOS Day Detail display

**User Story:** As the user opening the Day Detail page, I want to see how much solar was produced during each daylight block alongside the existing usage value, so that I can quickly compare load to generation per window.

**Acceptance Criteria:**

1. <a name="3.1"></a>The five-block panel on the Day Detail page SHALL display the solar kWh value inline next to the existing usage kWh value on the morning peak, off-peak, and afternoon peak rows.  
2. <a name="3.2"></a>The solar value SHALL be visually distinguished from the usage value by a sun icon prefix (matching the amber "Solar" colour already used in the power chart legend).  
3. <a name="3.3"></a>The night and evening rows SHALL render unchanged from the current layout.  
4. <a name="3.4"></a>WHEN the API returns null for a block's solar value, the row SHALL hide the solar value (no icon, no placeholder text) so days predating the feature deploy don't show false zeros.  
5. <a name="3.5"></a>WHEN the API returns 0.0 kWh for a block's solar value, the row SHALL render `0.0 kWh` (consistent with how the existing usage value renders zero).  
6. <a name="3.6"></a>The solar value SHALL be formatted with the same precision and unit suffix used for the existing usage value on the same row.  

### 4. Quality and verification

**User Story:** As a maintainer, I want the new solar integration to be covered by tests so regressions in block boundaries or integration logic are caught before deploy.

**Acceptance Criteria:**

1. <a name="4.1"></a>Unit tests SHALL cover the per-block solar integration with at least: a sunny day with full readings, a winter day with low solar, a day with a reading gap inside a daylight block, and a day with the morning peak collapsed to zero duration (sunrise after off-peak start).  
2. <a name="4.2"></a>The decoding tests for `DailyUsageBlock` on iOS SHALL include a fixture that exercises both the present-and-zero and the absent (null) cases for the new solar field.  
3. <a name="4.3"></a>The backfill CLI SHALL have a dry-run mode that prints intended writes without modifying DynamoDB.  
