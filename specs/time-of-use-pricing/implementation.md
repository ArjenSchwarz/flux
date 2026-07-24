# Implementation: Time-of-Use Pricing

Explanation of the T-1890/T-1891 implementation at three levels, plus a
completeness assessment. Covers commits `cd78e35`–`51b616d` and the fixes
applied during pre-push review.

---

## Beginner Level

### What Changed

Flux tracks a home battery and shows what the electricity costs. Until now the
app assumed one price for power, all day, with one free window in the middle
(11am–2pm) when the battery charges for nothing.

A new electricity plan is arriving that doesn't work that way. It has three
different prices depending on the time of day: free from 10am to 3pm, cheap
from 1am to 6am, and normal the rest of the time. The old model couldn't
describe that at all.

So a plan is now a set of **time bands**. You enter a default price plus the
exceptions — "free 10:00–15:00", "$0.28 from 01:00–06:00" — and everything
else costs the default. The system works out the full 24-hour picture from
that.

Two more things changed alongside it:

- **Plans can hand over to each other on a date.** You can enter the new plan
  today with a start date next month; on that date it takes over automatically.
  The old plan's end date and the new plan's start date are the same day, and
  that day belongs to the new plan.
- **The free window now comes from the plan.** It used to be a separate setting
  stored in AWS that somebody had to remember to change. Now it's part of the
  plan, so when the plan switches, the window switches with it.

### Why It Matters

Without this, the day the new plan starts someone would have to hand-edit an
AWS setting at exactly the right moment, and every cost the app showed would be
wrong — it would price the cheap 1am–6am power at the full rate.

There's also a promise being kept: every cost the app has ever shown for a past
day must still show the same number afterwards. A migration tool proves that by
calculating every historical day's cost both the old way and the new way and
refusing to change anything if a single day disagrees.

### Key Concepts

- **Band** (or segment) — a slice of the day with one price. Bands sit
  end-to-end and cover all 24 hours with no gaps and no overlaps.
- **Free window** — the band that costs nothing. The battery deliberately
  charges during it. At most one per plan.
- **kWh** — a unit of energy. A 1000-watt heater running for an hour uses 1 kWh.
- **Integration** — the system takes a power reading every 10 seconds and adds
  them up over a time range to work out the energy used in that range. That's
  how it knows how much power was drawn during each band.
- **Exclusive end date** — a plan that ends on 1 August does *not* price
  1 August; its successor does. Storing it this way means both rows carry the
  same date and nothing has to add or subtract a day.
- **Migration** — a one-off program that rewrites stored data into a new shape.

---

## Intermediate Level

### Changes Overview

Five commits across a Go backend and a Swift app, in dependency order:

1. **`internal/plan`** — a new leaf package holding the plan domain: the band
   model, validation, the derived day segmentation, per-date plan selection,
   free-window resolution, and DST-correct segment bounds. Plus the Go side of
   three-tier cost resolution.
2. **Lambda API** — `/pricing` CRUD speaks the band shape; `/status`, `/day`,
   `/history` resolve the window from plans instead of environment variables;
   `/day` and `/history` gain a `bandImports` split.
3. **Poller and operator tools** — `PlanSource` (read-through with a last-good
   cache), a midnight-anchored off-peak scheduler, per-band capture at day
   close, updated backfill CLIs, and `cmd/migrate-pricing`.
4. **App** — `PricingPlan`/`PricingPlanDraft` replace
   `PricingPeriod`/`PricingPeriodDraft`, the editor gains a Windows section,
   and `DayCosts` implements the same three tiers as Go.
5. **Spec** — requirements, design, decision log (6 ADRs, 39 quick decisions),
   39 tasks.

### Implementation Approach

**Storage is "what the user entered", not what's derived.** A plan stores
`defaultRate` + `windows`. The contiguous full-day band list comes from
`plan.Segments` on demand. This makes gaps and partial coverage
*unrepresentable* — uncovered time simply carries the default rate — so two of
the four validation rules the requirements ask for are satisfied by
construction, and the editor round-trips exactly what was typed.

**Costs resolve in three tiers** (Decision 6):

1. The stored per-band split, when its geometry matches the plan's rated
   segments *and* the free band's import is resolvable.
2. The pre-band single-rate formula, verbatim — applicable whenever the plan's
   rated segments share one rate, which every migrated legacy plan does.
3. All import at the plan's highest rate with no savings.

Tier 2 is the load-bearing one: it's why historical costs are unchanged with no
data backfill. Readings older than 30 days are gone, so a backfill *couldn't*
reach most history; tier 2 prices those days exactly from data that already
exists.

**One physical quantity, one writer.** The `flux-offpeak` row exclusively owns
free-window import; `bandImports` stores rated segments only. Peak grid import
and the band split come from a single integration
(`dynamo.IntegrateRatedBands`) so they cannot disagree.

**The two languages are pinned to each other by shared vectors.** Segmentation
and cost resolution exist in both Go and Swift. `internal/api/testdata/
pricing_segments.json` and `pricing_costs.json` are consumed by tests on both
sides, so a divergence fails a test rather than showing two different numbers
on two screens.

### Trade-offs

- **Exclusive end dates** were chosen over keeping inclusive ends and
  translating in the UI. Every consumer would otherwise carry ±1 arithmetic;
  now exactly one place does (the migration).
- **One-time migration, no compatibility layer.** For a two-user app with a
  handful of pricing rows, permanent dual code paths in validation, cost math,
  and UI buy nothing. Legacy app builds fail safely (no costs shown) in the
  interim.
- **Lambda reads the pricing table per request**, no caching. A Scan of a
  handful of rows inside the existing errgroup is negligible next to the four
  queries already there.
- **Segmentation duplicated across Go and Swift** — accepted, mitigated by the
  shared vectors, because the alternative (server-only costing) would mean the
  app couldn't price a cached day offline.

---

## Expert Level

### Technical Deep Dive

**DST is the sharp edge.** Band boundaries must resolve on the day's wall
clock, not as elapsed minutes from midnight — on Sydney's two transition days
those differ by an hour. `plan.SegmentBounds` uses `time.Date` in the location
so adjacent segments share a boundary instant and per-segment integrals over a
23- or 25-hour day still sum to the whole-day integral. A property test
asserts exactly that invariant across 23/24/25-hour days.

This is not hypothetical: pre-review, `internal/api/compute.go`'s
`offpeakWindow.bounds` still used `dayStart.Add(elapsed)` while
`liveBandImports` in the same file used `SegmentBounds`. On 2026-10-04 the
free-window edge and the band edges beside it would have sat an hour apart,
inside a single response — so today's `peakGridImportKwh` would not have
equalled the sum of `bandImports`. Fixed during review, with
`dst_window_test.go` pinning both the wall-clock hour and the
bounds-equal-segment-bounds invariant.

**Failure semantics are decomposed per outcome, not collapsed.** The
summarisation pass replaced a single early return with typed gating:

| Outcome | Blocks run | Sentinels | Result |
|---|---|---|---|
| Plan read failed | none | none | retried next tick |
| Plan with free band | all | set | normal |
| Plan without free band | window-independent + whole-day-rated | set | rated bands cover the day |
| No plan | window-independent only | band sentinel unset | repairable by backfill |

The distinction that matters: a *semantic* absence never returns before
window-independent work runs, and a *read failure* never sets sentinels. The
old single return would have starved socLow/dailyUsage/peak/bands forever on
any no-window day. A fifth outcome was added during review — once the
window-independent stats exist on a date no plan prices, the pass skips before
querying readings, rather than re-reading ~8,640 rows hourly to compute
nothing.

**Geometry is snapshotted, not assumed.** Each stored `bandImports` entry and
each off-peak row carries the window it was captured under. Plan windows stay
editable after a plan has priced days (Q16), so a later edit must be
*detectable* rather than silently repricing history — the join compares
geometry and degrades to a lower tier on mismatch. A sparse-complete off-peak
row (`integratedAt` set, zero samples) is a zero-delta artifact, not a measured
zero, and counts as unusable for costing.

**The migration verifies itself against an independent implementation.**
`cmd/migrate-pricing/golden.go` re-implements the legacy three-rate formula
rather than calling the shared helper — a check that reuses the code under test
proves nothing. Any day whose cost differs aborts before a single write.
During review its row-decoding was tightened: an undecodable row previously
warned and continued, which would have left an untransformed row, dropped every
day it priced out of the golden check, and exited 0 with a half-migrated table.

### Architecture Impact

- **The poller gains a dependency on `flux-pricing`**, which it never touched
  before. Read-only IAM (`Scan`/`GetItem`/`Query`); the Lambda keeps sole write
  access. `PlanSource` treats read failures as transient and serves last-good —
  never "no plan", because that would silently strip a day of its free window
  and its band split.
- **`internal/plan` is a genuine leaf** (imports only `fmt`, `math`, `sort`,
  `time`), consumed by `api`, `dynamo`, `poller`, and three CLIs. `dynamo` owns
  the `PricingItem ⇄ plan.Plan` conversion.
- **`/status.offpeak` became nullable.** A no-free-band day has no window
  strings to send, and the widget default constants were deleted so no client
  can substitute the legacy window.
- **Two end-date semantics exist in the wild until the migration runs.**
  Bounded by the cutover ordering, detectable via the `peakRate` attribute, and
  `replace-open-ended` refuses to run against a legacy closing row rather than
  producing a double-shifted date.

### Potential Issues

- **Cutover is ordered and manual** (`prerequisites.md`): deploy → migrate →
  enter the new plan → switch date. The migration must complete before the new
  plan is entered. Task 39 (deleting the transitional read transform) is gated
  on it and is intentionally still open.
- **Window edits degrade multi-rate days to the fallback tier.** Energy is
  frozen at capture; rates apply at display. A rate edit reprices history
  cleanly, but a *window* edit invalidates the geometry join for affected
  multi-rate days until a backfill re-captures them (only possible within the
  30-day readings TTL). Visible and consistent, but worth knowing.
- **A day no plan prices is terminal for `dailyUsage`/`peakPeriods`.** The band
  sentinel stays unset so a backfill can repair the split, but the derived
  sentinel is set with those two fields absent, and no tool recomputes the
  five-block panel afterwards. Intended per the design table; worth confirming.
- **Today's rated region is integrated twice per request** —
  `livePeakGridImport` and `liveBandImports` cover the same span, each
  computing five energy channels to use one. The poller deliberately fused
  these; the live path did not. Not a correctness bug now that the boundaries
  agree, but it is wasted work and the two carry different usability gates.

---

## Completeness Assessment

### Fully implemented

- **Requirement 1** (band model), **2** (succession), **3** (cost
  computation), **4** (plan-derived free window), **6** (management UI), **7**
  (pricing API). Every consumer in the design's audit table was verified
  changed: `DayChartDomain.offpeakRange`, the widget default constants, both
  backfill CLIs, `infrastructure/template.yaml` IAM and env, and the
  `OFFPEAK_START`/`OFFPEAK_END` plumbing in `cmd/api` and `internal/config`.
- Cross-language vectors are consumed by both Go (`pricing_vectors_test.go`)
  and Swift (`PlanSegmentsVectorTests`, `DayCostsVectorTests`).

### Partially implemented — by design

- **Requirement 5** (migration). The tool, its golden check, and its tests are
  complete; the *production run* is a prerequisite, not a code task. Task 39
  (removing the transitional legacy read transform) is correctly blocked on it.
  The write-path `legacyShape` rejection is permanent in two places and stays.

### Gaps worth noting

- **AC 6.4** (client validation mirrors server validation) is the one
  cross-language contract with no cross-language pin.
  `PricingPlanDraft.validate` mirrors `plan.Validate` by hand; segmentation and
  costs have shared vectors, validation does not. The two can drift silently.
  A `pricing_validation.json` vector set would close it.
- **`derivedstats.Blocks`** still derives its block boundaries with
  `dayStart.Add(elapsed)`. Pre-existing and outside this feature's scope (that
  package was explicitly unchanged), but it is the same latent DST bug class
  the band capture deliberately retired.
