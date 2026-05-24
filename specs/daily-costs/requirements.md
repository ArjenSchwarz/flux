# Requirements: Daily Costs

## Introduction

Show monetary costs and income alongside the existing energy figures on Day Detail and the History Period Overview, driven by user-configured per-kWh rates stored centrally so a single tenant-wide pricing setup serves both users. Three rates per pricing period — peak grid usage, solar feed-in, and off-peak savings — are entered in AUD and apply to every day the period covers. When no pricing period covers a day, that day shows no cost figures at all.

## Non-Goals

- Per-user or per-device pricing (single shared tenant-wide configuration only).
- Localised currency or multi-currency support (AUD only).
- A snapshot/frozen cost history — cost figures are always recomputed from the current pricing config.
- Cost figures rendered per-day on individual History rows (only aggregated period totals are shown on History).
- Cost figures on Dashboard, home-screen widgets, Control Center widget, or What's New.
- Cost lines on Day Detail other than the four specified (peak imports, solar income, net, off-peak savings).
- Pricing tier transitions within a single day (a day uses exactly one pricing period; mid-day rate changes are not modelled).
- Tax / GST line items, fixed daily supply charges, or other non-per-kWh fees.
- A third "shoulder" or "peak window" tariff bucket — every grid import outside the SSM off-peak window is treated as peak-rate.
- Time-of-use bands beyond the existing off-peak window already configured via SSM (`/flux/offpeak/*`).
- Export / share of cost figures (CSV, PDF, sheet, share extension).

## Requirements

### 1. Pricing Period Storage

**User Story:** As a Flux user, I want pricing periods stored centrally, so that both users see the same configured rates without setting them up separately.

**Acceptance Criteria:**

1. <a name="1.1"></a>The system SHALL persist pricing periods centrally such that any authenticated client reads the same list.
2. <a name="1.2"></a>Each pricing period SHALL store a start date (YYYY-MM-DD, required), an optional end date (YYYY-MM-DD), a peak grid rate (AUD per kWh), a solar feed-in rate (AUD per kWh), and an off-peak savings rate (AUD per kWh).
3. <a name="1.3"></a>Pricing period start and end dates SHALL be interpreted in the same timezone as the off-peak SSM window (`/flux/offpeak/*`); the system SHALL persist a documented assumption of `Australia/Melbourne`.
4. <a name="1.4"></a>Each rate field SHALL be stored to exactly four decimal places of precision; a submitted rate with more than four decimal places SHALL be rejected with the `rate_precision` error code.
5. <a name="1.5"></a>A pricing period SHALL cover every calendar day from its start date to its end date inclusive (treating an absent end date as open-ended through the indefinite future).
6. <a name="1.6"></a>The system SHALL reject a pricing period whose end date is before its start date with the `inverted_dates` error code.
7. <a name="1.7"></a>The system SHALL reject a pricing period whose calendar-day set intersects any existing period's calendar-day set with the `overlap` error code; on update of an existing period, the period being updated SHALL be excluded from the overlap check.
8. <a name="1.8"></a>The system SHALL reject a pricing period with a rate less than 0 or greater than 10.0 AUD per kWh on any of the three rate fields with the `rate_out_of_range` error code.
9. <a name="1.9"></a>The system SHALL allow at most one pricing period to have an absent (open-ended) end date at any given time; an attempt to create or update a period that would produce a second open-ended period SHALL be rejected with the `second_open_ended` error code.
10. <a name="1.10"></a>WHEN a payload violates more than one validation rule above, the system SHALL return the first error encountered in the order ACs 1.6 → 1.7 → 1.4 → 1.8 → 1.9.

### 2. Pricing Management API

**User Story:** As the iOS / macOS app, I want endpoints to list, create, update, and delete pricing periods, so that the Settings UI can manage them.

**Acceptance Criteria:**

1. <a name="2.1"></a>The Lambda API SHALL expose endpoints to list all pricing periods, create a new period, update an existing period, and delete an existing period.
2. <a name="2.2"></a>Pricing endpoints SHALL require the same bearer-token authentication used by every other Flux Lambda endpoint and SHALL return HTTP 401 when the token is missing or wrong.
3. <a name="2.3"></a>Create and update SHALL return HTTP 400 with a single machine-parseable error code drawn from the set {`overlap`, `inverted_dates`, `rate_out_of_range`, `rate_precision`, `second_open_ended`} when a validation rule from Requirement 1 fails.
4. <a name="2.4"></a>Delete SHALL return HTTP 404 when the period id is unknown.
5. <a name="2.5"></a>The list endpoint SHALL return periods sorted by start date ascending.
6. <a name="2.6"></a>The API SHALL expose a single transactional endpoint that, in one request, closes the existing open-ended period at a caller-supplied date AND creates a new pricing period; if either operation fails for any reason, neither SHALL be persisted.
7. <a name="2.7"></a>The client SHALL re-fetch the pricing list every time the user opens Settings → Pricing, every time Day Detail loads, every time the History range changes (7/14/30), and immediately after any mutating call; no longer-lived cache SHALL be used for cost computation.

### 3. Settings UI — Pricing Periods

**User Story:** As a Flux user, I want to add, edit, and delete pricing periods from Settings on both iOS and macOS, so that I can keep the rates current as my electricity contract changes.

**Acceptance Criteria:**

1. <a name="3.1"></a>Settings SHALL include a "Pricing" entry that opens a list of all configured pricing periods, sorted by start date ascending.
2. <a name="3.2"></a>Each row SHALL show the period's date range (with the open-ended period rendered as "from <start>"), the three configured rates formatted to four decimal places (e.g. "$0.2873 / kWh"), and a tap target to open the editor.
3. <a name="3.3"></a>The list SHALL include an explicit add affordance matching the existing SoC Alerts add-rule pattern.
4. <a name="3.4"></a>The editor SHALL provide inputs for start date, end date (optional), peak grid rate, solar feed-in rate, and off-peak savings rate, with rate inputs accepting up to four decimal places.
5. <a name="3.5"></a>The editor SHALL surface backend validation errors (`overlap`, `inverted_dates`, `rate_out_of_range`, `rate_precision`, `second_open_ended`) inline against the offending field where possible, and otherwise as a banner on the editor sheet.
6. <a name="3.6"></a>WHEN a create attempt fails with `overlap` because the new period starts inside an existing open-ended period, the editor SHALL offer a one-tap remediation that invokes the transactional close-and-create endpoint of AC 2.6 with the existing open-ended period's new end date set to the new period's start date minus one day; the remediation SHALL leave no partial state behind on failure.
7. <a name="3.7"></a>The editor SHALL provide a destructive delete action for existing periods, behind a confirmation matching the existing SoC Alerts delete confirmation pattern.
8. <a name="3.8"></a>When no pricing periods are configured, the list SHALL show an empty-state message naming the feature it unlocks (e.g. costs appearing on Day Detail and History).
9. <a name="3.9"></a>Settings SHALL match the iOS and macOS pricing list and editor layouts to the existing SoC Alerts Settings pattern (Form/List on iOS, native Settings scene on macOS).

### 4. Day Detail Costs Card

**User Story:** As a Flux user, I want to see the monetary breakdown for a single day alongside its energy figures, so that I can quantify what the day cost or earned.

**Acceptance Criteria:**

A day is a *priced day* when (a) it is covered by exactly one pricing period, AND (b) the day's daily-energy aggregate row exists in the data store. A value of `0` on any of the three input kWh fields (peak imports, solar export, off-peak imports) is a legitimate measured figure — an overcast day really does have `0` solar export and a full-self-consumption day really does have `0` peak imports — and SHALL be carried through the cost computation unchanged. An individual input kWh figure that is absent from an otherwise-present aggregate row SHALL be treated as `0` for the corresponding cost line. The off-peak window grid-import kWh figure SHALL additionally be treated as `0` when the off-peak SSM window is unset or zero-width. The card hides only when the entire daily-energy aggregate row is missing — the same condition under which Day Detail has no energy figures to show anyway.

1. <a name="4.1"></a>WHEN a viewed day is a priced day, Day Detail SHALL render a costs card containing four rows: peak imports cost, solar feed-in income, net, and off-peak savings.
2. <a name="4.2"></a>Peak imports cost SHALL equal the day's peak-import kWh × the covering period's peak grid rate.
3. <a name="4.3"></a>Solar feed-in income SHALL equal the day's solar export kWh × the covering period's solar feed-in rate.
4. <a name="4.4"></a>Off-peak savings SHALL equal the day's off-peak window grid-import kWh × the covering period's off-peak savings rate, with the off-peak kWh resolved per the priced-day definition above; the resulting savings value SHALL be displayed as "$0.00" when the resolved off-peak kWh is `0`.
5. <a name="4.5"></a>The net row value SHALL equal peak imports cost − solar feed-in income; off-peak savings does NOT contribute to the net.
6. <a name="4.6"></a>WHEN the viewed day is not a priced day, Day Detail SHALL NOT render the costs card.
7. <a name="4.7"></a>Each monetary value on the costs card SHALL be formatted as AUD with two decimal places (e.g. "$3.42"), using a leading "−" for negative results.
8. <a name="4.8"></a>The costs card SHALL appear directly below the existing five-block panel on Day Detail.

### 5. History Period Costs Totals

**User Story:** As a Flux user, I want aggregated cost totals for the active History range, so that I can compare cost across 7/14/30-day periods.

**Acceptance Criteria:**

1. <a name="5.1"></a>WHEN at least one day in the active History range is a priced day (per the definition in Requirement 4), the History Period Overview card SHALL include four total tiles: total peak imports cost, total solar feed-in income, net total, and total off-peak savings.
2. <a name="5.2"></a>Each total SHALL be the sum of the per-day figures computed per Requirement 4 across only priced days within the active range.
3. <a name="5.3"></a>WHEN fewer than 100% of days in the active range are priced days, the cost tile block SHALL display a caption beneath the four tiles in the form "N of M days priced" so the net figure is read with explicit coverage context.
4. <a name="5.4"></a>WHEN no day in the active History range is a priced day, History SHALL NOT render any of the cost tiles or the partial-coverage caption.
5. <a name="5.5"></a>Cost totals SHALL recompute on the active view's next render after the active range (7/14/30) changes or after pricing is mutated on the same device; propagation across devices follows the cache rules of AC 2.7.

### 6. Retroactive Recomputation

**User Story:** As a Flux user, I want pricing changes to affect historical days immediately, so that correcting a rate fixes every day it should fix.

**Acceptance Criteria:**

1. <a name="6.1"></a>Cost figures SHALL be derived on read from the stored kWh values and the current pricing configuration; no per-day cost figure SHALL be persisted as a frozen snapshot.
2. <a name="6.2"></a>WHEN a pricing period is created, updated, or deleted, every subsequent Day Detail or History view of a day in the affected range SHALL reflect the new pricing on its next load, without an app restart or cache invalidation step beyond the client refresh already triggered by the mutation (AC 2.7).
