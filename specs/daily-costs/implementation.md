# Implementation: Daily Costs

Branch `T-1326/daily-costs`, branched from v1.3 (`9958320`). Spec sources:
[requirements](requirements.md), [design](design.md),
[decision log](decision_log.md), [tasks](tasks.md).

This explanation is structured at three expertise levels so a reader can
enter at the depth they need, and so the act of writing it surfaces gaps
in the implementation. The Completeness Assessment at the end pins which
acceptance criteria are fully satisfied, which are partial, and where the
implementation has silently diverged from the spec.

---

## Beginner Level

### What this does

Flux now shows what your day cost you. Open Day Detail and a new card
sits underneath the existing five-block panel with four lines:

- **Peak imports** — what you paid for grid electricity outside the
  off-peak window.
- **Solar feed-in** — what the grid paid you for the solar you exported.
- **Net** — peak imports minus feed-in. A positive number means you
  spent more than you earned; a leading "−" means the grid owed you.
- **Off-peak savings** — what your cheaper off-peak window grid
  electricity is worth (a separate "saved" amount, not subtracted from
  the net).

On the History screen, the same four numbers appear as totals for the
7/14/30-day range you're viewing. If only some of the days in the range
had pricing set up, a small caption underneath reads
"4 of 7 days priced" so the totals can't be misread as the whole range.

To make this work the app needs to know your electricity rates, so
Settings has a new "Pricing" entry. You add one or more *pricing
periods*: a start date, an optional end date, and three rates (peak,
feed-in, off-peak savings) per kWh in Australian dollars. You can change
or remove rates at any time, and the cost numbers update everywhere
they're shown.

### Why it matters

Until now, Flux showed only energy figures (kWh in / out / stored). To
reconcile against an electricity bill you had to do the multiplication
by hand. Now the cost is rendered directly in the app, and any rate
change rewrites every historical day's cost figure automatically.

### Key concepts

- **kWh**: the unit your battery, solar, and grid all measure. One kWh is
  the energy used by a 1000-watt appliance running for an hour.
- **Off-peak window**: a daily time-of-day band (configured to 11:00–14:00
  here) where the grid charges a cheaper rate for the electricity you
  import. Flux already records how many kWh you pulled from the grid
  inside that window each day.
- **Pricing period**: a date range plus the three rates that apply
  during it. A period with no end date is "open-ended" — it covers
  every day from its start onwards until you cap it.
- **DynamoDB**: the cloud database the backend uses. Pricing periods get
  a new table called `flux-pricing`.
- **Lambda**: the small serverless function that the iOS / macOS app
  talks to. It now has five new endpoints for managing pricing.

---

## Intermediate Level

### Changes overview

48 files changed, +7357 / −17 lines. The work splits cleanly into a
backend stream and a client stream, each landing as its own merge
commit on the feature branch.

**Go backend** (one new DynamoDB table, five new HTTP routes):

- `internal/dynamo/pricing.go` + `pricing_transactional.go` — CRUD
  storage with a sentinel-row pattern that uses `TransactWriteItems` to
  guarantee at most one open-ended period exists at any time.
- `internal/api/pricing.go` + `pricing_handler.go` — handlers for
  `GET/POST/PUT/DELETE /pricing` and `POST /pricing/replace-open-ended`,
  plus a validation chain that enforces the AC 1.10 ordering
  (`inverted_dates → overlap → rate_precision → rate_out_of_range →
  second_open_ended`).
- `infrastructure/template.yaml` — new `PricingTable` resource and IAM
  permissions; `cmd/api/main.go` adds `TABLE_PRICING` to its required
  env vars and a `pricingStoreAdapter` to bridge the two read/write
  interfaces.

**Swift FluxCore** (pure cost computation + service layer):

- `FluxCore/Pricing/PricingPeriod.swift` + `PricingPeriodDraft.swift` —
  the value types. Dates are stored as `String` (`YYYY-MM-DD`) so
  membership tests use lexicographic compare and avoid `Date` /
  timezone round-trips (Decision 22).
- `FluxCore/Pricing/DayCosts.swift` + `PeriodCosts.swift` — pure
  functions over `(DayEnergy, [PricingPeriod])`. Off-peak nil is
  treated as zero kWh (so the card still renders on days with a stale
  off-peak split) and peak imports fall back to all of `eInput` when
  the split is unknown (Decisions 16, 23).
- `FluxCore/Pricing/PricingService.swift` — `@Observable @MainActor`
  cache with optimistic in-place folds and a cancellable
  fire-and-forget refetch after every mutation.
- `URLSessionAPIClient.swift` — five `async throws` methods mirroring
  the new routes; 400 responses are decoded into typed
  `FluxAPIError.pricingValidation(.reason)` cases.

**Swift app** (Settings, Day Detail, History wiring):

- `Flux/Flux/Settings/Pricing/` — list view (mirrors SoC Alerts) and an
  editor sheet with inline validation, the one-tap
  close-and-create remediation, and a confirmation dialog for delete.
- `Flux/Flux/DayDetail/CostsCard.swift` + viewmodel wiring — renders
  the four-row card directly below the existing five-block panel.
- `Flux/Flux/History/HistoryPeriodCostsCard.swift` + viewmodel wiring —
  renders the four-tile card below `HistoryStatsOverviewCard`, with
  the partial-coverage caption beneath the grid.

### Implementation approach

**Cost math lives in the client.** Decision 14 chose client-side
computation over server-side enrichment. `/day` and `/history` response
shapes are unchanged. The client fetches the pricing list per AC 2.7
and multiplies it into existing kWh values; retroactive recomputation
is automatic because nothing is persisted in derived form.

**The sentinel row makes "one open-ended period" race-safe.** A
singleton row at `pricingId = "__open_ended"` carries one attribute,
`openEndedId`. Every write that introduces, retires, or replaces an
open-ended period puts an `Update` on the sentinel into the same
`TransactWriteItems` call with a `ConditionExpression` of
`attribute_not_exists(pricingId) OR openEndedId = :prev`. Two
concurrent writers cannot both observe and update the sentinel from the
same previous value — the loser gets `ConditionalCheckFailed` mapped to
HTTP 409 `concurrent_open_ended_write`.

**Atomic close-and-create.** The editor's overlap-remediation flow
calls `POST /pricing/replace-open-ended` instead of running two
sequential mutations. The server derives the closing row's new
`endDate` as `newPeriod.startDate − 1 day` (no `closeAt` on the wire —
AC 3.6 pins the offset), runs the full validation chain against the
projected two-row state, then commits the three-item transaction
(sentinel + close + insert). If anything fails, nothing persists.

**The PricingService folds responses optimistically and refetches.**
Every mutating call returns the affected row(s), which the service
inserts/replaces in `periods` immediately. It then kicks off a
fire-and-forget `refresh()` so AC 2.7's "re-fetch immediately after
any mutation" is satisfied while the editor sees instant feedback. As
part of the pre-push review the refetch now stores its `Task` handle
and cancels any prior in-flight refetch — without that, two back-to-
back saves could land out of order and the older response could
clobber the newer one.

### Trade-offs

- **`Float64` rates over `Decimal`** (Decision 20). The codebase
  already uses `float64` for everything, and at four decimal places
  with realistic kWh totals the drift is well below `$0.01`. Adopting
  `shopspring/decimal` (Go) or `Foundation.Decimal` (Swift) for three
  fields would touch every cost site for no observable benefit.
- **String date storage on the client** (Decision 22). `YYYY-MM-DD`
  strings sort identically under lexicographic and chronological
  compare, and `DayEnergy.date` is already `String`. Decoding to `Date`
  would add timezone-handling code for a comparison that needs none.
- **Sentinel pattern over per-row condition checks** (Decision 21). The
  alternative — asserting `attribute_exists(endDate)` on every other
  pricing row inside each transaction — scales O(n) with row count.
  The sentinel is O(1) and keeps the transaction at most three items.
- **Costs on a separate History card** (Decision 19). Folding the four
  cost tiles into the existing 8-tile overview was rejected because
  unconditional rendering would leave four em-dashed tiles permanently
  visible when no pricing is configured. A separate card disappears
  cleanly and gives the partial-coverage caption an obvious home.
- **`{"pricing": [...]}` envelope on multi-row responses** (Decision
  24). Added post-implementation when a client/server envelope
  mismatch surfaced. Single-row endpoints return bare objects; only
  multi-row responses use the envelope. Mildly asymmetric, but
  matches the SoC Alerts precedent.

---

## Expert Level

### Technical deep dive

**Validation chain ordering matters.** AC 1.10 fixes the chain order
because the server returns the *first* error encountered, and
different orders surface different problems first when a payload
violates multiple rules. The implementation in
`runPricingValidationChain` walks them in `inverted_dates → overlap →
rate_precision → rate_out_of_range → second_open_ended` order. One
quirk: `second_open_ended` is dead code on the create path because
`validateOverlap` always fires first when two open-ended candidates
collide (both carry `endDate = "9999-12-31"` in the half-open interval
math). The handler will surface the situation as `overlap` instead.
The Swift editor doesn't notice — it routes both codes to a banner —
but a future client that wanted distinct UX for the two cases would
need either a reorder or a documented note.

**`9999-12-31` is an implementation sentinel inside `validateOverlap`,
not part of the wire contract.** A client that posted
`endDate = "9999-12-31"` on a closed period would be treated as
closed by the open-ended check (because `EndDate != nil`) but its
collision diagnostics would lose the "open-ended row id" hint. The
date is implausible enough that this is theoretical, but it's the
kind of implicit coupling a reviewer should know about.

**Delete uses `GetPricing` for the 404 short-circuit, not a conditional
`DeleteItem`.** Decision 11 advertises 404 on unknown id, but two
concurrent deletes can both pass the `GetPricing` check and both
return 204. The behavioural surface differs from the documented one
under racing deletes. Acceptable in practice (the UI refreshes the
list anyway, per Decision 11's negative consequence note), but a
`ConditionExpression: attribute_exists(pricingId)` on the closed-period
`DeleteItem` would tighten the contract.

**`pluckReplacedPair` was reading from an eventually-consistent
`Scan`.** The original `handleReplaceOpenEnded` re-listed the table
after the transaction to echo back both affected rows. DynamoDB
`Scan` is eventually consistent, so a stale read could return a
zero-value `PricingItem` for the just-written row, and the Swift
client would have folded an empty period into its local list. The
pre-push review rewrote this to synthesise the response from the
in-memory `closing` row (with its derived `endDate` and `updatedAt`)
plus the freshly-constructed `newItem`. Same wire shape, no race.

**The sentinel ConditionExpression covers two failure modes in one
expression.** `attribute_not_exists(pricingId) OR openEndedId = :prev`:
the first clause lets the very first write create the sentinel row
(no `Put` needed for provisioning); the second clause catches every
concurrent writer thereafter. The "first cold-start two-Lambda race"
case is documented in the design (case 10 in `pricing_atomicity_test.go`)
— both racers attempt the create, the loser fails with
`ConditionalCheckFailed`, and there's no partial state to clean up
because the sentinel is its own transaction item.

**Off-peak savings is a savings amount, not a counterfactual.**
Decision 5 settled the formula at `offPeakKwh × offPeakSavingsRate`
rather than `offPeakKwh × (peakRate − offPeakRate)`. The third rate is
named "off-peak savings per kWh" specifically because the user
encodes the savings figure directly. A future feature wanting to
report "what would this day have cost on the peak rate alone" would
have to add a separate computation; the current rate field can't be
reused for that.

**The History `periodCosts` and Day Detail `costs` accessors are
computed on every body evaluation.** SwiftUI `@Observable` re-runs
body when any tracked property changes, and these computed vars take
dependencies on `pricing` (a small array) and the day(s) being shown.
At realistic N (≤50 pricing rows, 30 history days) the work is
trivial — but the same view models cache other O(n) derived state
(`DayDetailViewModel.offpeakStats`) with an explicit "do this in
`loadDay`" comment. Future regression risk if these N's grow.

**`PricingService` was leaking detached tasks before the review.**
The original `scheduleRefetch()` fired `Task { try? await refresh() }`
with no stored handle. Two back-to-back mutations could land
out-of-order (older response overwrites newer) and the Task wasn't
cancellable on teardown. The fix stores `refetchTask: Task<Void, Never>?`
on the service, cancels the previous handle on every schedule, and
checks `Task.isCancelled` after the network round-trip in
`refresh()`. `DayDetailViewModel.comparisonTask` already used this
pattern; `PricingService` now matches.

### Architecture impact

- The `flux-pricing` table is read-only for the poller and CRUD-only
  for the Lambda. The poller's hot path is untouched. Cold-start cost
  for the Lambda gains one `dynamo.NewDynamoPricingStore` constructor
  (a struct alloc) and reuses the existing `ddbClient`.
- No change to `/status`, `/day`, or `/history` response shapes. A
  client that doesn't fetch `/pricing` sees no behavioural change —
  this is also why the feature flag is implicit ("did the user
  configure pricing?") rather than a server-side toggle.
- `PricingService` is registered at the composition root and threaded
  into `DayDetailViewModel`, `HistoryViewModel`, and the Settings
  flow. A `PricingService.shared` singleton fallback exists for
  preview / fast-path construction; the pre-push review noted this as
  a testability hazard worth addressing in a follow-up.
- The `pricing` JSON envelope is asymmetric (multi-row responses
  wrap; single-row responses don't). Decision 24 documents the choice
  and the SoC Alerts precedent that justified it. Any new pricing
  endpoint that returns ≥2 rows should use the envelope.

### Potential issues

- **Concurrent deletes return 204 instead of the documented 404** (see
  above). Practical impact is nil for a two-user system but the
  contract drifts from Decision 11.
- **Dead `second_open_ended` code path on create** (see above).
- **No DynamoDB Local integration tests.** The design's testing
  strategy explicitly called out that mocked atomicity tests cannot
  prove DynamoDB-side `ConditionExpression` behaviour for cases 1, 5,
  and 8. The mock-based `pricing_atomicity_test.go` proves the
  `Reasons[]` mapping but not the engine-level invariant.
- **`PricingService.shared` singleton fallback in three view-model
  default constructors.** Tests that don't inject a service will see
  state leaked from prior runs or previews. Removing the fallback and
  forcing explicit injection at the composition root is the right
  long-term fix.
- **Costs recompute on every SwiftUI body re-evaluation.** Auto-
  refresh (every 10s on Dashboard, which scrolls through DayDetail
  via the date carousel) and chart cursor scrubbing trigger frequent
  body evals. Negligible CPU today; worth caching in `loadDay()` /
  `loadHistory()` if the cost model ever grows beyond six
  multiplications per day.
- **`PricingPeriodsView` and `SoCAlertsView` share ~200 lines of
  near-identical structure** (Form wrappers, error banner, empty
  state, swipe-to-delete, sheet plumbing). The duplication is
  intentional in the spec ("mirror SoC Alerts") but invites drift on
  the next typography or layout change.

---

## Completeness Assessment

### Fully implemented (verified against the diff)

- **Requirement 1** (storage and validation): all 10 ACs have
  corresponding code. Validation chain ordering is correct.
- **Requirement 2** (HTTP API): all 7 ACs implemented; bearer-token
  middleware reused; list sorted; transactional replace-open-ended
  in place; client refresh policy correct.
- **Requirement 3** (Settings UI): all 9 ACs implemented across iOS
  and macOS; remediation flow wires through `replaceOpenEnded`;
  empty-state names the unlocked feature; layouts mirror SoC Alerts.
- **Requirement 4** (Day Detail card): all 8 ACs implemented; four
  rows in the required order; AUD formatting with leading `−` on
  negative net; off-peak nil → `$0.00`; card hidden when no covering
  period.
- **Requirement 5** (History totals): all 5 ACs implemented; the
  partial-coverage caption now renders beneath the four tiles (this
  was a defect fixed in the pre-push review).
- **Requirement 6** (retroactive recomputation): both ACs implemented;
  no per-day cost snapshot anywhere.

### Decisions reflected in code

All 24 decisions are observably honoured. Notable confirmations:

- **D10 / D20**: rates rounded to 4 dp on write
  (`roundTo4DP`), monetary totals displayed at 2 dp
  (`CostsCard.formatAUD`). `Float64` arithmetic throughout.
- **D16 / D23**: off-peak nil → 0 fallback in
  `DayCosts.costs(forDate:in:)`; peak imports fall back to all of
  `eInput`.
- **D21**: sentinel row at `pricingId = "__open_ended"`; every
  open-ended-touching write maintains the sentinel inside the same
  transaction with the documented `ConditionExpression`.
- **D22**: dates carried as `String` end-to-end on the Swift client;
  `PricingPeriod.covers(date:)` uses lexicographic compare.
- **D24**: `{"pricing": [...]}` envelope on `GET /pricing` and `POST
  /pricing/replace-open-ended`; `ReplaceOpenEndedResult` decoded from
  the envelope, now matched by id rather than array position after
  the pre-push review.

### Partial / extended (documented divergences worth noting)

- **AC 2.4 vs Decision 11**: concurrent deletes can both return 204.
  Practical impact is nil; the documented behaviour ("second device
  sees 404") is not what the code actually does. Either tighten with
  a `ConditionExpression: attribute_exists(pricingId)` on
  `DeleteItem`, or update Decision 11.
- **AC 3.5**: validation errors surface as a banner block beneath the
  rate inputs rather than inline against the offending field. The
  spec's "where possible" language tolerates this; a tighter
  implementation would attach date-shaped errors to the Dates section
  and rate-shaped errors to the Rates section.
- **AC 3.7**: the editor adds an extra `confirmationDialog` for
  delete that the SoC Alerts pattern doesn't have. Defensible (a
  whole pricing period is more destructive than a single alert rule)
  but technically diverges from "matching the existing SoC Alerts
  delete confirmation pattern."
- **CostsCard `±0.005 → $0.00` rendering** is a no-negative-zero UX
  tweak not captured in the decision log. Worth either documenting or
  reverting for strict spec conformance.
- **Editor's open-ended toggle** initialises `endDate = startDate` on
  toggle-off, producing a one-day period unless the user changes the
  end date. Defaulting to `startDate + 30 days` (or hinting the
  one-day duration) would be a friendlier default.

### Testing gaps

- **No DynamoDB Local integration tests** for atomicity cases 1, 5,
  and 8. The design called these out explicitly because mock-based
  tests cannot prove DynamoDB-side `ConditionExpression` behaviour.
- **No test asserts the `[closing, new]` ordering of the
  `replace-open-ended` response under odd start-date orderings.** The
  client now matches by id (post-review fix), so the regression risk
  is much smaller, but a handler-level test would lock the contract.

### Documentation

- **CHANGELOG** has a thorough Unreleased entry. The caption phrasing
  was corrected during the pre-push review to match the code
  ("N of M days priced", not "N of M days have pricing").
- **Decision log** is comprehensive (24 entries), including the
  post-implementation Decision 24 for the JSON envelope fix.
- **Tasks** marks all 31 tasks `[x]`.

---

## Validation findings

- **AC 5.3 caption placement** was wrong on first cut — the caption
  was passed as the `kpi:` argument of `HistoryCardChrome` and
  rendered as a headline in the top-right of the card header. The
  pre-push review moved it beneath the four-tile grid per the spec
  and the design's "single line in the card's tertiaryText
  treatment" note.
- **`pluckReplacedPair` could return zero-value rows** from an
  eventually-consistent post-write `Scan`. Replaced with synthesis
  from the in-memory transaction inputs; same wire shape, no race
  window.
- **`scheduleRefetch` leaked Tasks and allowed out-of-order
  responses.** Now stores a single task handle, cancels the prior
  one on every schedule, and `refresh()` checks
  `Task.isCancelled` before mutating `periods`.
- **`URLSessionAPIClient.replaceOpenEndedPricing` assumed positional
  response order.** Now matches by `closingId`, hardening the client
  against a future server-side reorder.
- **Implementation extensions not in the decision log** (CostsCard
  `±0.005 → $0.00`, editor confirmation dialog, open-ended toggle's
  endDate default) are worth either capturing as decisions or
  trimming for strict spec conformance. Calling them out so the
  author can pick.
