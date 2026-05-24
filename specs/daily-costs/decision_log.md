# Decision Log: Daily Costs

## Decision 1: Sizing — full spec workflow

**Date**: 2026-05-23
**Status**: accepted

### Context

T-1326 introduces a new costs/income table on Day Detail (and History totals), backed by user-configured per-kWh rates stored in the cloud. The scope-assessment phase had to pick between the lightweight smolspec workflow and the full requirements/design/tasks workflow.

### Decision

Use the full spec workflow (requirements → design → tasks → branch).

### Rationale

The change touches multiple subsystems: a new DynamoDB table, four new CRUD endpoints on the Lambda, FluxCore data models, iOS Settings UI, macOS Settings scene, Day Detail UI, and History Period Overview UI. Estimated implementation is ~800–1500 LOC across ~15–25 files. Multiple ambiguities in the brief (surface, tenancy, currency, off-peak math, gaps, overlap, retroactivity) needed structured clarification.

### Alternatives Considered

- **Smolspec**: Lightweight path used for ≤80 LOC / ≤3 files. Rejected — scope is well above that threshold and the brief is too ambiguous.

### Consequences

**Positive:**
- Each subsystem (backend, FluxCore, iOS, macOS) gets explicit acceptance criteria before code is touched.
- The 7 open questions get resolved on paper, not in PR review.

**Negative:**
- More upfront work before any code lands.

---

## Decision 2: Costs surface — Day Detail card + History period totals

**Date**: 2026-05-23
**Status**: accepted

### Context

The brief said "detail/history view" without specifying whether History meant per-day rows or aggregate totals. Three placements were viable: Day Detail only, Day Detail + History period totals, or Day Detail + per-day mini-row on History.

### Decision

Render the four-value table on Day Detail and a four-tile aggregate on the History Period Overview. No per-day cost figures on History rows.

### Rationale

Period totals match how the user already consumes History (8 tile overview added in T-896). Per-day cost figures on each row would crowd a card that already renders five-block stacked bars. Day Detail is the obvious place for the per-day breakdown.

### Alternatives Considered

- **Day Detail only**: Less information, but consistent with the most conservative reading of the brief — rejected because period costs are a natural extension once the data exists.
- **Per-day mini-row on History**: Maximum information but a heavier UI change with diminishing returns — rejected.

### Consequences

**Positive:**
- Period totals fall naturally out of the existing Period Overview KPI card layout.
- Day Detail card is self-contained.

**Negative:**
- Two render paths to keep in sync rather than one.

---

## Decision 3: Tenancy — single shared pricing set

**Date**: 2026-05-23
**Status**: accepted

### Context

The brief said "stored in the cloud so we don't need to set it up for each user", implying tenant-wide. Flux has no user identity in the API today — auth is a single shared bearer token.

### Decision

Pricing periods are tenant-wide. There is no per-user dimension on the data or the endpoints.

### Rationale

Matches the brief verbatim. Matches existing Day Notes (single shared note per date) and the existing auth model. Avoids adding user identity for a two-user system.

### Alternatives Considered

- **Per-user / per-device**: Would require introducing user identity to the API. Rejected — overkill for a two-user setup and the brief explicitly argues against it.

### Consequences

**Positive:**
- No auth model changes.
- Simple data model.

**Negative:**
- If the second user has a different electricity contract, that is not modellable. Acceptable for a household system.

---

## Decision 4: Currency — fixed AUD

**Date**: 2026-05-23
**Status**: accepted

### Context

Flux is a personal Melbourne system. The brief didn't name a currency.

### Decision

All rates are entered and displayed in AUD with the `$` symbol and two-decimal formatting (e.g. "$3.42").

### Rationale

The system is single-tenant single-location. No locale routing or multi-currency benefit at this scale.

### Alternatives Considered

- **Locale-driven**: Use the device locale's currency. Rejected — adds formatter / parser surface for zero benefit.

### Consequences

**Positive:**
- No locale plumbing.

**Negative:**
- Anyone deploying Flux outside Australia would have to patch the formatter. Acceptable.

---

## Decision 5: Off-peak savings formula

**Date**: 2026-05-23
**Status**: accepted

### Context

The brief listed three rates including "Off-peak savings per kWh" and asked for an "Off-peak savings" line in the table. The third rate name already encodes "savings per kWh", so the formula was ambiguous.

### Decision

Off-peak savings = off-peak window grid-import kWh × off-peak savings rate.

### Rationale

User confirmation: "its a calculation based on how much we've saved by using the offpeak window. usage * value." The "off-peak savings per kWh" rate is the saving figure per kWh consumed during the off-peak window — not a separate cheaper energy rate.

### Alternatives Considered

- **Savings vs peak rate**: offPeakKwh × (peakRate − offPeakRate). Rejected — doesn't match the user's stated intent and doesn't match how the third rate is labelled.
- **Both shown side-by-side**: More information, but rejected — user wanted a single savings figure.

### Consequences

**Positive:**
- Formula is trivial and matches the rate's literal name.

**Negative:**
- The figure expresses the user's stated savings model, which is not the same as an econometric "vs counterfactual" calculation. Acceptable since it's the user's call.

---

## Decision 6: Gap handling — omit cost figures entirely

**Date**: 2026-05-23
**Status**: accepted

### Context

Days that fall outside every configured pricing period need a defined behaviour: show em-dashes, fall back to the most recent rate, or hide the card.

### Decision

Days with no covering pricing period render no costs card at all (Day Detail), and are excluded from History period totals. History totals show "N of M days priced" caption when coverage is partial.

### Rationale

Matches the brief's "if no pricing is set, nothing should be shown" — extended uniformly to per-day coverage. Avoids quietly synthesising fake numbers via fallback rates, which would mask configuration mistakes.

### Alternatives Considered

- **Em-dashes**: Card stays visible with em-dashed cells. Rejected — same screen real estate cost, less consistent with the brief.
- **Most-recent-rate fallback**: Hide the gap from the user. Rejected — confidently wrong numbers are worse than absent numbers.

### Consequences

**Positive:**
- Empty-state behaviour is consistent across "no pricing at all", "this specific day uncovered", and "History range partially uncovered".

**Negative:**
- Partial-coverage period totals are easy to misread as "this is the cost across the whole range". The caption mitigates but doesn't eliminate the risk.

---

## Decision 7: Overlap rules — reject on save

**Date**: 2026-05-23
**Status**: accepted

### Context

Two adjacent pricing periods might overlap by a day or more if the user enters them inattentively. Three options were considered: reject, auto-truncate, or allow and resolve at compute time.

### Decision

Reject any new or updated period whose date range overlaps an existing period. The validation runs server-side, returns HTTP 400, and surfaces in the editor sheet.

### Rationale

Keeps the data model simple — every day in the database falls into at most one pricing period. Auto-truncation would silently mutate older data, and runtime-resolution would require an arbitrary tiebreaker.

### Alternatives Considered

- **Newer-period-wins truncation**: Auto-update the older period's end date. Rejected — silent mutation of historic config is surprising.
- **Latest-created wins at compute time**: Allow overlap, pick the most recently created. Rejected — invisible behaviour.

### Consequences

**Positive:**
- Single source of truth for every day's pricing.
- Editor errors are explicit.

**Negative:**
- Users must close the previous period (set its end date) before adding a new one. The editor offers a one-tap remediation (AC 3.6) so the workflow stays single-step in the common case.

---

## Decision 8: Backfill — retroactive recomputation

**Date**: 2026-05-23
**Status**: accepted

### Context

When pricing is added or changed after data has accrued, two policies are possible: rewrite historical figures with the new pricing, or freeze each day's cost figures at first-compute time.

### Decision

Cost figures are derived on read from current pricing. No per-day cost snapshot is persisted.

### Rationale

Matches the brief's "set for a specific period (starting date and optional end date)" — the user is configuring rates per date range, not stamping immutable invoices. Editing a rate to fix a mistake should fix every affected day. Storage stays minimal.

### Alternatives Considered

- **Snapshot at compute time**: Persist each day's cost figures. Rejected — costs extra storage and produces a surprising "I changed the rate but the old number sticks" experience.

### Consequences

**Positive:**
- No backfill job needed when rates change.
- Cost figures always reflect the current truth.

**Negative:**
- Recomputation on every render. Trivial work for 30 days × 3 multiplications, so not a concern.

---

## Decision 9: Peak rate covers all non-off-peak imports

**Date**: 2026-05-23
**Status**: accepted

### Context

Design-critic flagged that the brief's "costs of the peak-imports" line is ambiguous: AU electricity contracts commonly carry three tariff bands (peak / shoulder / off-peak), but Flux currently only models two (the SSM `/flux/offpeak/*` window and "everything else"). Without a decision, AC 4.2's math is unspecified.

### Decision

The peak grid rate applies to every grid-import kWh that falls outside the SSM off-peak window — i.e. the existing "peak imports" figure already used on History Usage Stats. There is no separate shoulder band.

### Rationale

Matches the existing two-band data model. Avoids a backend schema change. The user can always close one period and start another on the same date if their real-world contract introduces a more granular tariff later. The off-peak savings line still captures the "off-peak window is cheaper" effect via the off-peak savings rate.

### Alternatives Considered

- **Three-band tariff (peak / shoulder / off-peak)**: Closer to real AU contracts. Rejected — adds a tariff-time-of-day schema before there's a stated need.
- **User-defined "peak window" per pricing period**: Most flexible. Rejected — large UI surface for a one-user system.

### Consequences

**Positive:**
- AC 4.2 math is unambiguous and uses an already-computed figure.

**Negative:**
- Households with a shoulder tariff will see slightly overstated peak-imports cost. Acceptable since the user controls the rate.

---

## Decision 10: Rate precision — store 4 dp, display rates at 4 dp, totals at 2 dp

**Date**: 2026-05-23
**Status**: accepted

### Context

Design-critic noted that AU electricity rates routinely run to 4 decimal places (e.g. `$0.2873/kWh`); storing or displaying rates at two decimals would lose meaningful precision when reconciling against a bill. Daily / period totals don't need the same precision — they're cents-level numbers.

### Decision

Rates are stored as decimals with up to 4 places, displayed at 4 places in Settings (e.g. "$0.2873 / kWh"), and used at full precision when computing monetary totals. Monetary totals are displayed rounded to 2 decimal places (e.g. "$3.42").

### Rationale

Two-decimal *display* of a total is what users see on their bill; four-decimal *display* of a rate is what they enter from their bill. Storage matches display granularity for rates so round-tripping through Settings is lossless.

### Alternatives Considered

- **Two-decimal storage for rates**: Simpler. Rejected — loses precision against real-world tariffs.
- **Integer cents storage**: Avoids floating-point pitfalls. Rejected — rates aren't integer cents; the four-decimal precision is the dominant constraint.

### Consequences

**Positive:**
- Reconcilable against an AU electricity bill.

**Negative:**
- Computation needs to use a precise decimal type or accept Float64 rounding. Float64 rounding errors at four-decimal precision over ~30 days are well below `$0.01` — acceptable.

---

## Decision 11: Idempotent delete — always 404 on unknown id

**Date**: 2026-05-23
**Status**: accepted

### Context

The initial draft said delete "SHALL succeed idempotently for already-deleted ids only on the immediate retry" — design-critic correctly noted that's not implementable without request deduplication infrastructure that doesn't exist.

### Decision

`DELETE /pricing/{id}` returns HTTP 404 when the id is unknown. There is no idempotency claim beyond what HTTP already provides.

### Rationale

Matches the existing SoC Alerts delete behaviour. Simpler to reason about; clients that need at-least-once delivery can retry on transport errors but treat 404 as terminal success.

### Alternatives Considered

- **Always 204 (no content) regardless of existence**: Idempotent in the strict sense. Rejected — masks "is this the id I think it is?" mistakes.

### Consequences

**Positive:**
- Behaviour is well-defined and matches SoC Alerts.

**Negative:**
- Concurrent deletes from two devices: the second device sees 404. Acceptable; the UI refreshes the list anyway.

---

## Decision 12: Rate sanity-cap at $10/kWh

**Date**: 2026-05-23
**Status**: accepted

### Context

Negative rates were already rejected (Decision draft AC 1.5). Design-critic noted a typo could enter `$28.73` or `$287.3` instead of `$0.2873`, producing wildly wrong cost figures. AU electricity rates max out around $0.50–0.60/kWh in practice.

### Decision

Each of the three rate fields is rejected if > 10.0 AUD per kWh.

### Rationale

10× the highest plausible AU retail tariff. Catches typos by an order of magnitude without constraining legitimate use.

### Alternatives Considered

- **No cap**: Smaller surface area. Rejected — the failure mode is asymmetric (small mistakes are caught by the user, huge ones produce alarming card numbers).
- **Tighter cap (e.g. $2)**: Closer to plausible reality. Rejected — risks rejecting future legitimate prices.

### Consequences

**Positive:**
- Typos that change rate by an order of magnitude are caught at save.

**Negative:**
- A future hyperinflation scenario or a different deployment would need to raise the cap. Acceptable.

---

## Decision 13: Pricing dates use the SSM off-peak window's timezone

**Date**: 2026-05-23
**Status**: accepted

### Context

Pricing period dates need a defined interpretation: when does "the day 2026-04-12" start and end? Lambda runs in UTC; the off-peak window is configured in local time (Melbourne).

### Decision

Pricing period start and end dates are interpreted in the same timezone as the off-peak SSM window (`Australia/Melbourne`). The codebase documents this assumption alongside the off-peak handling.

### Rationale

Avoids two-timezone reasoning. Matches how the off-peak window already partitions a day. The "off-peak window grid import kWh" figure powering AC 4.4 is computed in local time, so pricing-period boundaries must use the same time basis.

### Alternatives Considered

- **UTC**: Simpler at the storage layer. Rejected — would misattribute days near midnight Melbourne time.
- **User-configurable timezone**: Maximum flexibility. Rejected — over-engineered for a Melbourne-only system.

### Consequences

**Positive:**
- Timezone reasoning is centralised in the off-peak handling code that already exists.

**Negative:**
- A deployment outside Melbourne would need to change one configured timezone in the off-peak handling. Acceptable.

---

## Decision 14: Client-side cost computation in FluxCore

**Date**: 2026-05-23
**Status**: accepted

### Context

Cost figures could be computed server-side (enriching `/day` and `/history` responses) or client-side (FluxCore math over the pricing list plus the existing daily-energy figures). Both satisfy the ACs.

### Decision

Cost computation lives in FluxCore as pure functions over `(DayEnergy, [PricingPeriod])`. The backend exposes pricing CRUD only; `/day` and `/history` response shapes are unchanged.

### Rationale

The math is six multiplications per day. FluxCore can unit-test it without HTTP. Retroactive recomputation (Requirement 6) is automatic on every render because the pricing list is re-fetched per AC 2.7. Cross-device consistency falls out of the same: both devices fetch the same pricing list and compute the same numbers. The Lambda stays unaffected by a feature whose data layer is entirely tangential to its read endpoints.

### Alternatives Considered

- **Server-side enrichment**: Single source of truth on the wire; clients render dumb fields. Rejected — adds a Lambda dependency on the pricing table for every `/day` / `/history` call, complicates caching, and the math isn't worth the round-trip when the client already has the pricing list to drive Settings.

### Consequences

**Positive:**
- Backend stays small (CRUD-only on a new table).
- Cost math is unit-testable as pure functions.
- Retroactivity is free.

**Negative:**
- The math contract lives in FluxCore; if a non-Apple client were ever added it would have to re-implement. Acceptable — there are no other clients.

---

## Decision 15: Atomic close-and-create endpoint for open-ended-period replacement

**Date**: 2026-05-23
**Status**: accepted

### Context

Peer-review-validator flagged that the editor's overlap-remediation flow (close the existing open-ended period, then create the new one) is two sequential mutations. If close succeeds and create fails, the user is left with a silently-closed open-ended period and no replacement.

### Decision

The Lambda API exposes a single transactional endpoint that closes the existing open-ended period at a caller-supplied date AND creates a new pricing period in one request. If either step fails for any reason, neither is persisted. The editor's one-tap remediation invokes this endpoint exclusively.

### Rationale

DynamoDB's `TransactWriteItems` supports atomic multi-item writes. Using a single endpoint keeps the partial-state failure mode out of the system. Failure handling on the client collapses to "retry the same one-tap action."

### Alternatives Considered

- **Two sequential mutations + visible partial state**: Client surfaces "previous period closed, new period failed; please retry" when create fails after close succeeds. Rejected — non-atomic by construction; users can quit the app between the two calls.
- **No remediation flow, just a documented two-step path**: Editor copy explains to close the period first. Rejected — the brief's "set for a specific period" usage pattern would frequently produce overlaps for naive users.

### Consequences

**Positive:**
- Editor flow is single-tap and atomic.
- No "ghost open-ended period closed but unreplaced" state.

**Negative:**
- One extra Lambda route to maintain.

---

## Decision 16: Off-peak split absence is treated as zero, not as "input missing"

**Date**: 2026-05-23
**Status**: accepted

### Context

Peer-review-validator noted that `OffpeakItem` records can be absent or stale on past dates (per `docs/agent-notes/api-layer.md`). The initial AC 4.5 said "if any of the three input kWh figures is unavailable, the card is not rendered" — but absent off-peak split is a common state, not a data-integrity failure, and the card should still render.

### Decision

A "priced day" requires only the peak-import kWh figure and the solar export kWh figure to be present. The off-peak window grid-import kWh figure is treated as `0` when unavailable or when the off-peak SSM window is unset / zero-width. The card still renders; off-peak savings displays "$0.00".

### Rationale

The two figures that materially drive the costs card (peak imports cost, solar feed-in income, net) are the existing daily-aggregate fields that survive the 30-day reading TTL. The off-peak split is a value-add overlay; treating its absence as render-blocking would erase the entire card for any day with a stale off-peak record.

### Alternatives Considered

- **Treat off-peak absence as render-blocking**: Conservative but hides the whole card on stale-but-recoverable state. Rejected.
- **Treat all three as required**: Same drawback as above. Rejected.

### Consequences

**Positive:**
- Card renders on every day with the two core figures, regardless of off-peak data hygiene.
- Off-peak savings $0.00 is a defensible value when the split isn't known.

**Negative:**
- Users may misread "$0.00 off-peak savings" as "I had no off-peak imports" rather than "the data isn't available." Acceptable — the partial-coverage caption on History does not flag this case, but the day-detail view's off-peak split tile would be missing anyway in this state.

---

## Decision 17: Update-overlap check excludes the period under update

**Date**: 2026-05-23
**Status**: accepted

### Context

The naive overlap check would cause every update to fail because the period being updated overlaps itself.

### Decision

On update, the period being updated is excluded from the overlap check (AC 1.7). On create, the check considers every existing period.

### Rationale

Standard CRUD semantics; obvious in hindsight, but worth pinning down so an implementer doesn't write the naive query.

### Alternatives Considered

None — this is a correctness fix, not a design choice.

### Consequences

**Positive:**
- Updates work.

**Negative:**
- One extra "is this an update? exclude the id" branch in the validation path. Trivial.

---

## Decision 18: Zero is a valid kWh value; the card hides only on a missing aggregate row

**Date**: 2026-05-23
**Status**: accepted

### Context

The initial "priced day" definition required peak-import kWh and solar export kWh to be "available," which is ambiguous: does an overcast day with `0` solar export count as available? Does a day where the user fully self-consumed (so `0` peak imports) count? The brief reviewer (user) flagged that 0 is itself a legitimate measured value and asked whether the card hides on those days.

### Decision

`0` is a legitimate measured value on all three input kWh fields and contributes to the cost computation unchanged. The card hides only when the entire daily-energy aggregate row is missing for the day. An individual input kWh field absent from an otherwise-present row is treated as `0` for the corresponding cost line.

### Rationale

The data model stores aggregate rows with optional fields; an unset numeric field on an otherwise-populated row is most often "no activity" rather than "data unavailable." Hiding the card on a fully-self-consumed day (`0` peak imports) would mislead the user — that's a meaningful "$0.00 spent on grid imports today" outcome that deserves to be shown. Same for an overcast `0` solar export day. The only state in which the card couldn't sensibly render is when there is no daily aggregate at all, and that state already suppresses every other Day Detail card by virtue of having nothing to render.

### Alternatives Considered

- **Hide card whenever any input field is `0`**: Conservative but misleading — would erase legitimate cost figures on common days.
- **Hide card whenever any input field is unset (treating unset as different from `0`)**: Keeps fidelity to data hygiene at the cost of intermittent disappearance of the card during partial backfills. Rejected — failure mode is opaque to the user.

### Consequences

**Positive:**
- The card renders on every covered day that has a daily-energy row, including all-self-consumption and zero-solar days.
- Card visibility tracks "is there data to show at all" rather than the more fragile "is every field populated."

**Negative:**
- A partial backfill that leaves a row with one zero field will show a zero cost line that might not reflect reality; mitigated because the same partial state would corrupt the energy figures on the same card anyway, so the issue is not unique to costs.

---

## Decision 19: History costs land on a new "Period costs" card, not in the existing 8-tile overview

**Date**: 2026-05-23
**Status**: accepted

### Context

The History "Period overview" card already renders eight stat tiles. The four new cost figures (total peak imports cost, total solar income, net, total off-peak savings) could either extend that card to twelve tiles or live in a new card directly below.

### Decision

Render the four cost tiles on a new `HistoryPeriodCostsCard` placed directly below `HistoryStatsOverviewCard`. The new card is conditionally rendered (only when at least one priced day exists in the active range) and carries the partial-coverage caption inside the card body.

### Rationale

The existing 8-tile card is unconditional. Extending it would force "no pricing configured" to leave four em-dashed tiles permanently visible — at best noise, at worst confusing. A separate card disappears cleanly when there is nothing to show, and the partial-coverage caption ("N of M days priced") naturally belongs to the costs card rather than hanging off the side of a card that doesn't otherwise have one.

### Alternatives Considered

- **Extend the existing 8-tile overview to 12 tiles**: Tighter visual grouping. Rejected — couples cost rendering to overview rendering, and the empty-state behaviour is awkward.
- **Place the cost tiles inline with each per-day row**: Out of scope per Non-Goals; not reconsidered.

### Consequences

**Positive:**
- Clean conditional show/hide.
- Partial-coverage caption has an obvious home.

**Negative:**
- One more card to scroll past on the History screen. Acceptable given the screen already paginates by card.

---

## Decision 20: Storage encoding for rates — Float64 with 4-dp normalisation on write

**Date**: 2026-05-23
**Status**: accepted

### Context

The codebase stores every numeric value (kWh, percentages, SoC, watts) as `float64` / `Double`. No money / decimal type exists. Decision 10 fixed rate precision at exactly four decimal places. The encoding choice was between `float64` (codebase precedent), `int64` tenth-of-cent units, or introducing `shopspring/decimal`.

### Decision

Rates are stored and computed as `float64` (Go) / `Double` (Swift). The Lambda validator rejects rate payloads with more than four decimal places (`rate_precision` error code) and rounds accepted rates to exactly four decimals before persistence. Cost computation uses raw `float64` multiplication; display rounds to two decimals.

### Rationale

Float64 has 15+ significant decimal digits, enough to round-trip four-decimal rates without drift. Worst-case accumulated drift across 30 days × 3 multiplications × $1/kWh sits well below `$0.01`, which is the display granularity anyway. Introducing a decimal type would touch every cost site for no observable benefit and break the codebase's "every number is `float64`" convention. Integer microcents would impose conversion at every boundary for the same lack of benefit.

### Alternatives Considered

- **Integer tenth-of-cent units (`int64`)**: Avoids float arithmetic entirely. Rejected — adds conversion code at every API edge for zero observable improvement at our display precision.
- **`shopspring/decimal` (Go) / `Foundation.Decimal` (Swift)**: Exact arithmetic. Rejected — codebase precedent is `float64`; a new type for three fields is disproportionate.

### Consequences

**Positive:**
- Matches existing codebase convention.
- No new dependencies, no new conversion code.

**Negative:**
- Drift exists in principle; bounded sub-cent in practice. Future audit-ready accounting would need to change this.

---

## Decision 21: Sentinel-row pattern for AC 1.9 atomicity

**Date**: 2026-05-23
**Status**: accepted

### Context

Design-critic flagged that the initial atomicity design only used ConditionExpressions on the closing row and the new row inside the `TransactWriteItems` transaction. That guard catches the "two writers close the same row simultaneously" race but not the broader "two writers create a second open-ended row simultaneously" race that AC 1.9 prohibits. With the validator scan and the transaction running sequentially, two concurrent creators could both pass the validator and both succeed.

### Decision

The `flux-pricing` table holds a singleton sentinel row with `pricingId = "__open_ended"` and one functional attribute `openEndedId: string | null`. Every write that introduces, retires, or replaces the open-ended period maintains this row inside the same `TransactWriteItems` request, with a `ConditionExpression` on its previous value. Two concurrent writers cannot both observe and update the sentinel from the same previous value — the second one fails with `ConditionalCheckFailed`, mapped to HTTP 409 `concurrent_open_ended_write`. The editor refetches and retries on 409.

### Rationale

DynamoDB supports up to 100 items per `TransactWriteItems` and we're using at most 3 (sentinel + closing row + new row). Optimistic concurrency on the sentinel is the standard pattern for single-flight invariants and avoids the alternative of scanning every existing row inside the transaction's `ConditionCheck` items, which would scale linearly with pricing-row count.

### Alternatives Considered

- **Best-effort enforcement**: rely on the closing-row ConditionExpression alone and accept that AC 1.9 can be violated under rare concurrency. Document the violation as self-healing on next list-fetch. Rejected — easy to fix correctly with the sentinel; the design shouldn't ship a known correctness gap.
- **ConditionCheck against every existing pricing row**: each transaction asserts `attribute_exists(endDate)` on every other row. Scales O(n). Rejected — overkill for the invariant we need.
- **Scan inside the transaction**: not supported by `TransactWriteItems`. Rejected.

### Consequences

**Positive:**
- AC 1.9 enforced atomically under arbitrary concurrency.
- Single source of truth for "which row is open-ended."
- 3-item transactions stay well under DynamoDB's 100-item limit.

**Negative:**
- Every write that touches an open-ended period must read the sentinel first to capture `prevOpenEndedID`. One extra `GetItem` per such write.
- Sentinel must be filtered out of `ListPricing` results. Trivial guard in the reader.
- Initial provisioning has to create the sentinel row with `openEndedId = null`. CloudFormation custom resource or first-write-creates pattern.

---

## Decision 22: Pricing dates carried as `String` end-to-end on the client

**Date**: 2026-05-23
**Status**: accepted

### Context

Design-critic noted that `DayEnergy.date` is `String` ("YYYY-MM-DD"), not `Date`. The initial design's "closed-interval test" implied a `Date` comparison strategy that doesn't exist in the codebase.

### Decision

`PricingPeriod.startDate` and `endDate` are `String` ("YYYY-MM-DD") on the Swift side, matching `DayEnergy.date`. The priced-day predicate compares strings lexicographically — correct for ISO-formatted dates. No `Date` conversion is needed for the lookup. `createdAt` / `updatedAt` decode as `Date` (RFC3339) since they're not used for day arithmetic.

### Rationale

ISO `YYYY-MM-DD` strings sort identically under lexicographic and chronological comparison. Avoiding the `Date` round-trip eliminates a class of timezone bugs at the cost of nothing — the pricing-period lookup never needs `Date` semantics like calendar math or time-zone conversion.

### Alternatives Considered

- **Decode to `Date` in Australia/Melbourne**: Conceptually cleaner. Rejected — adds timezone-handling code for a comparison that works trivially on strings, and `DayEnergy.date` is already `String` so a round-trip would be needed at every comparison site.

### Consequences

**Positive:**
- No timezone bugs at the day-membership boundary.
- Symmetry with the existing `DayEnergy.date` type.

**Negative:**
- The editor's date picker has to format to `YYYY-MM-DD` on save. Trivial; `DateFormatter.iso8601(only: .date)` exists.

---

## Decision 23: Peak-imports fallback when off-peak split is nil

**Date**: 2026-05-23
**Status**: accepted

### Context

Design-critic flagged that `DayEnergy.peakGridImportKwh` is computed as `eInput - offpeakGridImportKwh`, which returns `nil` when `offpeakGridImportKwh == nil`. Decision 16 says off-peak absence must not hide the card; Decision 18 says zero is a valid kWh value. The behaviour on `nil` off-peak split was not pinned in the design.

### Decision

When `dayEnergy.offpeakGridImportKwh == nil`, the cost computation treats:
- Peak imports kWh = `dayEnergy.eInput ?? 0` (all grid imports are billed as peak).
- Off-peak savings kWh = `0` → off-peak savings displays "$0.00".

When `dayEnergy.offpeakGridImportKwh != nil`, the computation uses `dayEnergy.peakGridImportKwh ?? 0` and `dayEnergy.offpeakGridImportKwh ?? 0` directly.

### Rationale

Decision 9 frames peak as "everything outside the off-peak window." If the split is unknown, conservatively treating all imports as peak gives the user the larger of the two possible cost figures — the safer default for a billing-cost display. Off-peak savings degrades to zero gracefully, consistent with Decision 16's "off-peak unset → savings $0.00".

### Alternatives Considered

- **Hide the card when off-peak split is nil**: Contradicts Decision 16, which already settled this. Rejected.
- **Use `peakGridImportKwh ?? 0` regardless**: Would undercount peak imports on every stale-offpeak day (returning `0` instead of `eInput`). Rejected.
- **Split `eInput` 50/50 between peak and off-peak as a fallback**: Arbitrary. Rejected.

### Consequences

**Positive:**
- Cost card renders on every day with `eInput` present, even when the off-peak split is missing.
- Conservative default (over- rather than under-counts peak when uncertain).

**Negative:**
- A day with a missing off-peak split shows `$0.00` savings even if the user actually consumed off-peak energy. Mitigated because the missing-split state is rare on recent data and the Day Detail off-peak energy tile would already be missing in this state.

---

## Decision 24: Pricing list and replace-open-ended responses share a `pricing` envelope

**Date**: 2026-05-24
**Status**: accepted

### Context

During the post-merge design-critic review of the implementation, two production-breaking JSON envelope mismatches surfaced. The server's `GET /pricing` returns `{"pricing": [...]}` and `POST /pricing/replace-open-ended` returns `{"pricing": [closing, new]}`, but the original Swift client decoder expected `{"periods": [...]}` for the list endpoint and a bare `PricingPeriod` object from the replace endpoint. The design document specified the wire types for `PricingPayload` and the `pricingError` shape but was silent on the envelope key used by the multi-row endpoints.

### Decision

Standardise both multi-row pricing responses on the `{"pricing": [...]}` envelope. Single-row endpoints (`POST /pricing`, `PUT /pricing/{id}`) continue to return bare `PricingPeriod` objects. The Swift client decodes `replace-open-ended` into a new `ReplaceOpenEndedResult { closing, newPeriod }` value, and `PricingService.replaceOpenEnded` folds both rows into its local list instead of only the new row.

### Rationale

Both endpoints already returned the same shape on the server, so aligning the client to the server avoided a server change and any deployed-Lambda compatibility window. Keeping a structured `ReplaceOpenEndedResult` instead of returning `[PricingPeriod]` lets the editor surface "the open-ended period you just closed ended on X" without re-indexing the array, and pins the closing/new ordering at the type level.

### Alternatives Considered

- **Change the server to return `periods`**: Would require redeploying the Lambda before any client could be released and offered no advantage over aligning the client. Rejected.
- **Have `replaceOpenEndedPricing` return `[PricingPeriod]`**: Simpler but loses the closing/new distinction at the type level, forcing every caller to know which index is which. Rejected.

### Consequences

**Positive:**
- Server and client agree end-to-end on every pricing endpoint.
- The editor's optimistic fold updates both rows immediately, removing the dependency on the fire-and-forget refetch to surface the closing row's new `endDate`.

**Negative:**
- The protocol is mildly asymmetric — multi-row responses carry an envelope, single-row responses do not. This is consistent with how the SoC Alerts feature already shapes its responses.

---
