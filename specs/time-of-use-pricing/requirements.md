# Requirements: Time-of-Use Pricing

Transit tickets: T-1890 (Multiple prices support), T-1891 (New plan support)

## Introduction

Flux currently models an electricity plan as three flat rates (peak, feed-in, off-peak savings), with the free window held in separate SSM configuration. A new plan is arriving with time-of-use pricing — free 10:00–15:00, a cheaper rate 01:00–06:00, and a standard flat rate otherwise — starting on a known future date. This spec reworks pricing plans into daily time bands, adds plan succession so the new plan can be entered ahead of its start date, and makes the plan the single source of truth for the free window.

## Out of Scope

- Daily supply charge on plans (candidate for a later ticket)
- Time-banded feed-in rates — feed-in stays one flat rate per plan
- Bands spanning midnight — a plan segments the 24-hour day; a rate active before and after midnight is expressed as two bands
- Mid-day plan switches — plans change over at midnight local time only
- Retaining the legacy three-rate plan shape after migration
- A compatibility layer for legacy clients — after migration, not-yet-updated apps fail safely (no costs shown) until updated
- Battery features for paid bands — charge projection, cutoff suppression, and peak masking stay tied to the free band; the cheaper 01:00–06:00 band affects costing only
- Automatic tariff import from a retailer
- More than one open-ended plan (existing single-open-ended rule is kept)

## Requirements

### 1. Time-Band Plan Model

**User Story:** As the app owner, I want a plan defined as daily time bands each carrying a rate or marked free, so that the new plan's time-of-use pricing can be represented alongside the existing flat-rate plans.

**Acceptance Criteria:**

1. <a name="1.1"></a>A plan SHALL consist of an ordered set of contiguous, non-overlapping time bands that exactly cover the 24-hour day (00:00–24:00), each band carrying an AUD/kWh rate or marked free  
2. <a name="1.2"></a>Band boundaries SHALL be expressed at minute granularity (HH:MM) and interpreted, like plan dates, in Australia/Sydney local time  
3. <a name="1.3"></a>A plan SHALL contain zero or one free band, and SHALL contain at least one rated (non-free) band  
4. <a name="1.4"></a>A plan SHALL carry a single flat feed-in rate (AUD/kWh)  
5. <a name="1.5"></a>IF a plan has a free band, THEN it SHALL carry a savings reference rate (AUD/kWh) used to value free-window energy  
6. <a name="1.6"></a>All rates SHALL satisfy the existing pricing bounds and precision rules (0 ≤ rate ≤ 10.00, at most 4 decimal places)  
7. <a name="1.7"></a>The system SHALL reject a plan whose bands leave a gap, overlap, or do not cover the full day, identifying the violated rule  
8. <a name="1.8"></a>The model SHALL be able to represent both the current plan (free 11:00–14:00, one flat rate) and the new plan (free 10:00–15:00, cheaper rate 01:00–06:00, standard rate otherwise)  

### 2. Plan Succession

**User Story:** As the app owner, I want to end the current plan and add a successor that starts the same day, so that the switch to the new plan happens automatically on the right date.

**Acceptance Criteria:**

1. <a name="2.1"></a>Every calendar day (Australia/Sydney local time) SHALL be priced by at most one plan — the plan whose date range covers that day  
2. <a name="2.2"></a>WHEN a plan ends on date D and a successor starts on date D, all of day D SHALL be priced by the successor (the predecessor's last priced day is D−1)  
3. <a name="2.3"></a>The system SHALL accept a successor plan entered in advance of its start date  
4. <a name="2.4"></a>The system SHALL allow ending the open-ended plan by giving it an end date without requiring a successor; days after it SHALL be unpriced until a successor exists  
5. <a name="2.5"></a>The system SHALL reject plans whose date ranges would price the same day twice, identifying the conflicting plan  
6. <a name="2.6"></a>The succession operation (end current plan + create successor) SHALL never expose an intermediate state that violates [2.1](#2.1) or the single-open-ended rule, including under concurrent edits  
7. <a name="2.7"></a>Days not covered by any plan SHALL show no cost data, matching today's unpriced-day behaviour  

### 3. Time-of-Use Cost Computation

**User Story:** As a user, I want daily and period costs computed from the plan's time bands, so that displayed costs reflect what I actually pay under the new plan.

**Acceptance Criteria:**

1. <a name="3.1"></a>A day's grid import cost SHALL be the sum over the day's bands of (import kWh consumed during the band × the band's rate), with free bands contributing $0, using the bands of the plan that prices that day  
2. <a name="3.2"></a>Feed-in income SHALL be the day's total export kWh × the plan's feed-in rate, and net cost SHALL be import cost minus feed-in income  
3. <a name="3.3"></a>WHEN the day's plan has a free band, savings SHALL be the free band's import kWh × the plan's savings reference rate  
4. <a name="3.4"></a>The per-band energy split for a day SHALL come from a single source, so that every screen showing a cost or band value for that day shows the identical number  
5. <a name="3.5"></a>For every day priced by a banded plan, the per-band import split SHALL remain available for as long as the day's energy figures are retained (i.e. beyond the raw-readings retention window); the fallback of [3.6](#3.6) SHALL be the exception for days whose split was never captured, not the steady state  
6. <a name="3.6"></a>A day's split counts as available only when every band's import kWh is known; WHEN it is unavailable (including partially known), all of that day's import SHALL be priced at the plan's highest band rate and the savings line SHALL show $0.00 (matching the existing fallback presentation), identically on every screen  
7. <a name="3.7"></a>Period cost totals (History) SHALL equal the sum of the per-day costs over the priced days in the period, retaining the existing partial-coverage indication when some days are unpriced  
8. <a name="3.8"></a>On daylight-saving transition days, band membership SHALL follow local wall-clock time (energy in a repeated hour counts toward the band containing that wall-clock time) and the day's band energies SHALL sum to the day's total  

### 4. Free Window Driven by the Active Plan

**User Story:** As the app owner, I want the free window used by off-peak features to come from the plan active on the relevant day, so that it switches automatically when plans change.

**Acceptance Criteria:**

1. <a name="4.1"></a>WHEN computing new values, features that consume the off-peak/free window (off-peak energy split, charge-window stats, off-peak charge projection, cutoff suppression, peak-period masking) SHALL derive the window from the free band of the plan that prices the day in question, not from separately maintained window configuration  
2. <a name="4.2"></a>WHEN plans change on a switch date, window-dependent behaviour SHALL follow each day's own pricing plan without manual reconfiguration; derivations of the next window (charge projection, cutoff suppression) SHALL use the free band of the plan pricing the day that window falls on, even when that plan is not yet active  
3. <a name="4.3"></a>Off-peak window boundary processing (start/end of the free window) SHALL occur at the free-band times of the plan pricing that day  
4. <a name="4.4"></a>WHEN no plan prices a day being computed, or its pricing plan has no free band, off-peak features SHALL behave as they do today when no off-peak data exists (values absent, not zero); display of already-stored per-day values SHALL be independent of current plan coverage  
5. <a name="4.5"></a>Off-peak values already stored for past days SHALL remain valid and SHALL NOT be retroactively recomputed  
6. <a name="4.6"></a>A failure to read plan data SHALL NOT be treated as "no plan": window boundary processing and band-split capture SHALL tolerate transient plan-data unavailability without permanently losing a day's band split  

### 5. Migration of Existing Plans

**User Story:** As the app owner, I want existing flat-rate periods converted to the band model once, so that historical costs keep working without dual code paths.

**Acceptance Criteria:**

1. <a name="5.1"></a>Each existing pricing period SHALL be represented in the band model as a free band matching the window its historical data was computed under (11:00–14:00), rated band segments covering the remainder of the day each carrying the former flat rate, the unchanged feed-in rate, and a savings reference rate equal to its former off-peak savings rate  
2. <a name="5.2"></a>Day and period cost values for historical dates SHALL be identical before and after migration, including each closed period's last priced day (legacy inclusive end dates SHALL map to the switch-day semantics of [2.2](#2.2) without gaining or losing a day)  
3. <a name="5.3"></a>Migration SHALL be verified by comparing recorded pre-migration day and period cost values against post-migration output before the legacy shape is removed  
4. <a name="5.4"></a>After migration, the legacy three-rate plan shape SHALL no longer be accepted or served anywhere; migration SHALL be complete before the new plan's switch date, and legacy app builds encountering the band shape SHALL fail safely (no costs shown, no crash, no writes)  

### 6. Plan Management UI

**User Story:** As a user, I want to view and edit band-based plans in Settings, so that I can enter the new plan and set the switch date before it starts.

**Acceptance Criteria:**

1. <a name="6.1"></a>The pricing settings screen SHALL display each plan's date range and its bands (times and rates, free band identified), replacing the current three-rate summary  
2. <a name="6.2"></a>The plan editor SHALL allow defining a plan's bands (boundaries, per-band rate or free), start and end dates, feed-in rate, and savings reference rate, following the existing pricing editor's validation and error presentation patterns  
3. <a name="6.3"></a>The editor SHALL support the succession flow of [2.2](#2.2)–[2.3](#2.3): ending the current plan on date D and creating the successor starting D  
4. <a name="6.4"></a>Client-side validation SHALL mirror server-side validation rules so that a plan accepted locally is not rejected by the server for a rule the client could have checked  
5. <a name="6.5"></a>Date-range conflicts SHALL offer the existing overlap remediation affordance, adjusted to the switch-day semantics of [2.2](#2.2)  

### 7. Pricing API

**User Story:** As the app, I want the pricing API to serve and accept band-based plans, so that clients and the backend agree on one model.

**Acceptance Criteria:**

1. <a name="7.1"></a>The pricing API SHALL provide the same capabilities as today (list, create, update, delete, replace-open-ended succession) over the band-based plan shape  
2. <a name="7.2"></a>Validation failures SHALL identify the violated rule in the response, extending the existing pricing error-code pattern to band rules (gap, overlap, coverage, precision, range, free-band count)  
3. <a name="7.3"></a>Requests in the legacy three-rate shape SHALL be rejected with a validation error after migration  
