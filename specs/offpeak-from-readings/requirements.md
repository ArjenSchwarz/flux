# Requirements: Off-Peak From Readings

## Introduction

The off-peak window's five energy deltas (grid import / grid export / solar / battery charge / battery discharge) are currently derived from two `getOneDateEnergyBySn` snapshots taken at the SSM window boundaries. AlphaESS's intraday `eInput` value lags physical reality by ~15–30 minutes, so the snapshot at the window-end boundary misses the tail of grid-charging — those kWh later appear in the day's final `eInput` total and are misclassified as peak. This feature replaces the snapshot-diff computation with integration over the 10-second `flux-readings` table, eliminating the lag and making the five deltas match the physical meter to within sensor calibration.

## Non-Goals

- Changing the SSM off-peak window (`/flux/offpeak-start`, `/flux/offpeak-end`) — the window definition is unchanged.
- Adding a third tariff band or per-pricing-period window override — separate concern, deferred.
- Changing any client-facing JSON shape on `/status`, `/day`, `/history` — only the values shift; the schema is identical.
- Recovering off-peak deltas for days whose readings have already aged out of the 30-day TTL.
- Replacing AlphaESS as the source of the daily `eInput` total — only the *window split* is recomputed.
- Adjusting the costs computation logic in `FluxCore.DayCosts` — it consumes the corrected values without change.
- Retroactive re-firing of any past alerts (Battery-Can't-Empty, SoC alerts). Downstream surfaces pick up corrected data on their next read; no alert replay.
- Schema or retention change to `flux-readings`. Backfill scope is bounded by the existing 30-day TTL.

## Sign Conventions

The following conventions apply throughout this document, matching `flux-readings`:

- `pgrid > 0` = importing from grid; `pgrid < 0` = exporting to grid (watts)
- `pbat > 0` = battery discharging; `pbat < 0` = battery charging (watts)
- `ppv ≥ 0` = solar production (watts; ppv < 0 readings exist due to night-time inverter draw and SHALL be clamped to 0 per-sample)

## Requirements

### 1. Off-Peak Delta Computation From Readings

**User Story:** As a Flux user, I want the off-peak window's energy figures to match what physically happened on the wire, so that costs and peak/off-peak stats agree with my meter.

**Acceptance Criteria:**

1. <a name="1.1"></a>The system SHALL compute the five off-peak energy deltas — grid import, grid export, solar, battery charge, battery discharge — by integrating instantaneous power from `flux-readings` over the SSM off-peak window, replacing the existing `endE* − startE*` snapshot-diff computation as the source of these fields.  
2. <a name="1.2"></a>The integration SHALL produce a kWh value equal to the time-integral of the corresponding power channel across consecutive reading pairs within the window.  
3. <a name="1.3"></a>Any consecutive reading pair separated by more than 60 seconds SHALL contribute zero energy to the integration, to prevent phantom accumulation across polling outages.  
4. <a name="1.4"></a>For each reading pair, the contribution to each of the five deltas SHALL be derived from the positive (or negative, per the relevant convention) component of the instantaneous power, split per-sample before integration: grid import = `max(pgrid, 0)`, grid export = `max(-pgrid, 0)`, battery discharge = `max(pbat, 0)`, battery charge = `max(-pbat, 0)`, solar = `max(ppv, 0)`. The final integrated value for each delta is non-negative by construction; no additional total-level clamp is required.  
5. <a name="1.5"></a>The window SHALL be `[offpeak-start, offpeak-end)` in `Australia/Sydney` local time. When a bracketing reading exists outside the window within 60 s of the boundary, the integration SHALL include the partial pair clipped to the boundary. When no usable bracketing reading exists, the integration begins at the first in-window reading and ends at the last in-window reading without boundary extrapolation.  
6. <a name="1.6"></a>WHEN the readings table contains fewer than two usable samples inside or bracketing the window for a given day, the system SHALL omit the off-peak split for that day (downstream surfaces fall back to their existing "missing record" behaviour).  
7. <a name="1.7"></a>SSM window configuration SHALL be sampled once per computation pass: the poller's window-end pass uses the values in effect at window-close, and each Lambda live-projection request (AC 4.1) uses the values in effect at request time. If `/flux/offpeak-start` or `/flux/offpeak-end` changes mid-day, the persisted row and a subsequent live request may reflect different windows; the persisted row is authoritative once written.  

### 2. Correctness Validation

**User Story:** As a Flux user reviewing the fix, I want concrete evidence that the new computation matches reality, so that I can trust the corrected values.

**Acceptance Criteria:**

1. <a name="2.1"></a>For the reference incident on 2026-05-18, after recomputation the day's peak figure (`eInput − gridUsageKwh`) SHALL be ≤ 0.25 kWh (the meter's recorded peak was 0.17 kWh; the bound allows ~0.08 kWh for sensor calibration).  
2. <a name="2.2"></a>For every day with a `flux-offpeak` row, the recomputed `gridUsageKwh` SHALL be ≤ that day's `eInput` value in `flux-daily-energy` (a structural sanity bound: off-peak grid import is a subset of total grid import).  
3. <a name="2.3"></a>For every day, the recomputed `gridUsageKwh + (eInput − gridUsageKwh)` SHALL equal `eInput` exactly (trivially true by construction, but documented so a future refactor doesn't accidentally introduce a third bucket).  

### 3. Window-End Finalisation

**User Story:** As an operator, I want each day's off-peak row finalised promptly and accurately after the window closes, so that the History and Day Detail surfaces reflect the corrected value as soon as the window ends.

**Acceptance Criteria:**

1. <a name="3.1"></a>The poller SHALL defer the window-end computation for the day until it has observed at least one reading with timestamp ≥ `offpeak-end` (avoiding the boundary-loss bug at exactly `offpeak-end`), bounded by a maximum wait of 30 seconds after `offpeak-end`.  
2. <a name="3.2"></a>The poller SHALL persist the day's `flux-offpeak` row with `status = complete` no later than 5 minutes after `offpeak-end`, even if the bounded wait of AC 3.1 expires without an at-or-after-boundary reading (in which case the integration uses whatever readings exist).  
3. <a name="3.3"></a>The poller SHALL set `status = complete` only after the five readings-integrated deltas have been written; partial or interim values SHALL NOT be persisted under `complete`.  
4. <a name="3.4"></a>WHEN the poller restarts at any point between `offpeak-start` and 24:00 of the same day, it SHALL be able to compute the day's deltas from the readings table without depending on in-memory state from before the restart.  
5. <a name="3.5"></a>Window-end writes SHALL use a DynamoDB conditional update guarding against overwriting a row whose `status = complete` was written more recently by another writer (the backfill CLI), so a concurrent backfill in flight cannot lose the poller's authoritative write.  

### 4. Today's Live Split

**User Story:** As a Flux user, I want today's off-peak tile to reflect what's happened so far during the window, so that I can see grid-charging progress as it accumulates.

**Acceptance Criteria:**

1. <a name="4.1"></a>WHEN `now` is at or after `offpeak-start` and the day's `flux-offpeak` row does NOT yet have `status = complete`, the `/day?date=today` and `/status` responses SHALL compute the five deltas by integrating readings from `offpeak-start` up to `min(now, offpeak-end)`.  
2. <a name="4.2"></a>WHEN the day's `flux-offpeak` row has `status = complete`, the responses SHALL serve the deltas from that row (the value written per Requirement 3).  
3. <a name="4.3"></a>WHEN `now` is before `offpeak-start` on the same day, the responses SHALL omit the off-peak split (matching the existing pre-window behaviour).  
4. <a name="4.4"></a>WHEN the SSM off-peak window is unchanged during the day, the live-computed deltas of AC 4.1 SHALL grow monotonically (or stay equal) across successive requests within the same window. (A mid-day SSM change MAY cause a smaller delta on the next request; this is acceptable behaviour given AC 1.7.)  

### 5. Persisted Diagnostic Fields

**User Story:** As an operator debugging future off-peak issues, I want the original snapshot values and integration provenance retained on the row, so that I can detect drift between snapshot-diff and readings-integration without re-querying upstream.

**Acceptance Criteria:**

1. <a name="5.1"></a>The poller SHALL continue capturing snapshots of the AlphaESS daily energy totals at `offpeak-start` and `offpeak-end`, and SHALL persist the resulting `startE*` and `endE*` fields on the `flux-offpeak` row.  
2. <a name="5.2"></a>The five delta fields (`gridUsageKwh`, `solarKwh`, `batteryChargeKwh`, `batteryDischargeKwh`, `gridExportKwh`) SHALL be sourced from the readings integration, NOT from `endE* − startE*`.  
3. <a name="5.3"></a>No API consumer (the iOS clients, the History stats card, the Day Detail costs card) SHALL read the `startE*` / `endE*` fields; they exist only for operator diagnostics.  
4. <a name="5.4"></a>Each persisted row SHALL additionally record: the count of readings used in the integration, the count of consecutive pairs skipped due to the 60-second gap rule, and the timestamp of the integration pass. These provenance fields are diagnostic; no API consumer reads them.  

### 6. Drift Observability

**User Story:** As an operator, I want any future drift between snapshot-diff and readings-integration surfaced in logs, so that a regression in either upstream source is visible without a user reporting a bill mismatch.

**Acceptance Criteria:**

1. <a name="6.1"></a>On every `flux-offpeak` row write (poller window-end and backfill CLI), the writer SHALL emit a structured log entry containing: the date, the snapshot-diff value of each of the five deltas (`endE* − startE*`), the readings-integrated value, and the absolute difference per delta.  
2. <a name="6.2"></a>Log entries SHALL be at `INFO` level; no automatic alerting is in scope for this feature (deferred to future observability work).  

### 7. One-Time Backfill

**User Story:** As a Flux user, I want my historical 7/14/30 day stats corrected after the fix lands, so that the in-app numbers don't disagree with my bill for the next month.

**Acceptance Criteria:**

1. <a name="7.1"></a>The system SHALL provide a CLI that recomputes the five deltas for every `flux-offpeak` row whose date is strictly before today and whose readings still exist (within the 30-day TTL window).  
2. <a name="7.2"></a>The CLI SHALL NOT process today's row; the poller is the single authoritative writer for today.  
3. <a name="7.3"></a>The CLI SHALL be idempotent on the five delta fields and the two integration-count provenance fields: re-running it against the same unchanged readings SHALL produce identical values for these seven fields (subject to the precision policy of AC 7.7). The `integratedAt` provenance field is excluded from this guarantee since it captures the time of integration.  
4. <a name="7.4"></a>The CLI SHALL use the same integration policy as the poller (Requirement 1) including the sparse-readings handling of AC 1.6. Days with fewer than two usable samples in the window SHALL be reported in the run summary and the row SHALL be left unchanged.  
5. <a name="7.5"></a>The CLI SHALL print a per-day summary showing the prior delta values, the new delta values, the absolute difference per delta, and any sparse-readings skip — sized so the operator can sanity-check the magnitude of the correction before it propagates to the clients.  
6. <a name="7.6"></a>The CLI SHALL support a `--dry-run` flag that performs the recomputation and prints the summary without writing to DynamoDB.  
7. <a name="7.7"></a>Persisted delta values SHALL be rounded to two decimal places (matching the existing `roundEnergy` policy) before storage, so both the poller and the CLI produce identical persisted values for the same readings.  
8. <a name="7.8"></a>The CLI SHALL use DynamoDB conditional writes that fail if the row's `status` is anything other than `complete`, so a row mid-poll (`pending`, no status) is never overwritten.  

### 8. Performance

**User Story:** As an operator, I want the new computation to fit inside the existing poller and Lambda runtime budgets, so that the fix doesn't introduce timeouts or cost regressions.

**Acceptance Criteria:**

1. <a name="8.1"></a>The window-end computation SHALL complete within 2 seconds on a typical day (~1080 readings × five derived deltas integrated from three power channels).  
2. <a name="8.2"></a>The `/status` and `/day` Lambda responses on a day with live-integrated off-peak (AC 4.1) SHALL complete within 500 ms p95 over a full off-peak window's readings, measured warm at the production memory configuration.  
