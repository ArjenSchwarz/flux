# Decision Log: Off-Peak From Readings

## Decision 1: Backfill scope — recompute all retained days

**Date**: 2026-05-24
**Status**: accepted

### Context

The bug fix changes how `flux-offpeak` rows are computed going forward. Existing rows carry lagged values from the snapshot-diff method. The `flux-readings` table has a 30-day TTL, so any backfill window is bounded.

### Decision

A one-time CLI run will recompute every `flux-offpeak` row whose date is still covered by `flux-readings`. Forward-only fixes are insufficient because the History 7/14/30-day stats and the new Daily Costs feature would carry the old lagged values for ~30 days post-deploy.

### Rationale

The discrepancy is large enough to be visible in the iOS app (4× overstatement of peak imports). Leaving it visible for a month after the deploy degrades user trust in the figures. The readings table is the ground truth and is still available, so the backfill is cheap and high-value.

### Alternatives Considered

- **Forward-only, no backfill**: Simpler but the History stats stay wrong for ~30 days post-deploy. Rejected.
- **On-demand only (ship CLI, don't run it)**: Operator overhead for no benefit. Rejected.

### Consequences

**Positive:**
- History and costs surfaces reflect corrected values within minutes of the deploy.
- Single one-time operator action with a clear endpoint.

**Negative:**
- Days older than 30 days remain on the lagged values forever (no remedy is possible — readings have aged out).
- A race exists between the backfill and the poller for "today" — handled by [[design]] (CLI skips today by default).

---

## Decision 2: Keep snapshot fields on OffpeakItem as diagnostic-only

**Date**: 2026-05-24
**Status**: accepted

### Context

`OffpeakItem` carries `startEpv`, `startEInput`, `startEOutput`, `startECharge`, `startEDischarge`, `startEGridCharge`, plus the matching `endE*` fields. With the readings-integration change these are no longer the source of the five deltas. They could be removed, retained, or made conditional.

### Decision

Keep the `startE*` / `endE*` fields on every new `flux-offpeak` row. The poller continues capturing both snapshots at the window boundaries. The five delta fields (`gridUsageKwh` etc.) are sourced from readings integration, not from the snapshot diff. No API consumer reads the snapshot fields.

### Rationale

Retaining the snapshot values gives a free forensic comparison every day: any future drift between the snapshot-diff number and the readings-integrated number is immediately visible in the row, without needing extra instrumentation. The cost is six `float64` fields per row on a table with <1000 rows lifetime — negligible.

### Alternatives Considered

- **Remove the snapshot fields entirely**: Cleaner schema but loses diagnostic visibility. Rejected — the storage cost is trivial.
- **Stop capturing snapshots (skip the AlphaESS API calls)**: Saves two API calls per day but removes the diagnostic comparison and would need a schema migration. Rejected.

### Consequences

**Positive:**
- Forensic visibility into snapshot-vs-readings drift on every row.
- No schema migration; existing dynamo marshalling unchanged.
- Today's pre-window-start `/status` response still has a `startEInput` to display for diagnostics if needed.

**Negative:**
- Two AlphaESS API calls per day continue (no cost saving on that axis).
- Future readers of the schema must understand that `startE*` / `endE*` are diagnostic and `endE* − startE* ≠ gridUsageKwh` by design.

---

## Decision 3: Live split for today integrates readings up to `now`

**Date**: 2026-05-24
**Status**: accepted

### Context

With snapshot-diff, the existing `offpeakDeltas` function projected pending records by `current.EInput − op.StartEInput`. With readings-integration, that projection logic no longer applies. Today's split during the window needs a fresh approach.

### Decision

While `now` is inside the off-peak window, integrate `flux-readings` from `offpeak-start` up to `now`. After the window closes, serve the deltas from the day's `flux-offpeak` row (the finalised value written by the poller per Requirement 3). Before the window opens, omit the split (existing behaviour).

### Rationale

The Dashboard and Day Detail off-peak tile is meaningful during the window — it shows grid-charging progress in real time, which several existing tiles depend on (e.g. the off-peak savings on the costs card for today). Suppressing it for the entire 11:00–14:00 window would regress on existing behaviour. Integrating live keeps the same UX with corrected values.

### Alternatives Considered

- **Suppress until window closes**: Off-peak tile blank from midnight to 14:00 (~14h/day). Regresses existing UX. Rejected.
- **Keep snapshot-diff projection for today, readings-integration for past days**: Hybrid keeps the lag bug for the active window. Rejected.

### Consequences

**Positive:**
- Today's tile updates as the window progresses, like the existing implementation but accurate.
- Single computation primitive shared by the poller's window-end writer, the Lambda's live projection, and the backfill CLI.

**Negative:**
- Each Lambda request inside the window pulls ≤1080 readings and integrates. Bounded by AC 8.2 (≤500 ms p95).
- Today's `/status` and `/day` no longer return the same off-peak value across two requests 10 seconds apart (it grows). Documented behaviour, matches the existing dashboard auto-refresh model.

---

## Decision 4: Handle sparse readings with best-effort integration

**Date**: 2026-05-24
**Status**: accepted

### Context

Readings can have gaps — poller outage, AlphaESS API failure, ECS deployment, container restart. The integration needs a defined behaviour for gaps.

### Decision

Skip any consecutive reading pair more than 60 seconds apart (matching the existing `computeTodayEnergy` pattern). The integration uses whatever pairs remain. If zero readings exist in the window for the day, omit the off-peak split entirely (matching the existing missing-record behaviour). The backfill CLI additionally reports days that hit the sparse-reading skip path so the operator can decide whether to investigate.

### Rationale

Aligns with the existing `computeTodayEnergy` policy — the same threshold that's been in production for live-compute since the original poller spec. Operators already understand "60-second gap = skip pair". Falling back to snapshot-diff on gappy days would reintroduce the lag for exactly the days where the lag matters least (the poller was offline, so there are fewer kWh to misclassify), at the cost of making the codepath nondeterministic between two methods.

### Alternatives Considered

- **Treat any gap as "missing"**: Drops too many days when the readings are mostly intact. Rejected.
- **Fall back to snapshot-diff when gaps exist**: Hybrid reintroduces the lag bug. Rejected.

### Consequences

**Positive:**
- Single integration policy; same behaviour in the poller, Lambda, and backfill CLI.
- Days with brief poller outages still get an accurate split for the parts the poller saw.

**Negative:**
- A day with a 5-minute outage in the middle of the window slightly under-counts the affected delta during the outage. Acceptable — the result is a small undercount, not a misclassification between peak and off-peak.

---

## Decision 5: Correctness validation is the bug-incident + structural bound

**Date**: 2026-05-25
**Status**: accepted

### Context

A first draft of the requirements expressed the correctness target as "the off-peak grid-import delta SHALL agree with the meter to ~1%, as on 2026-05-18". Both the design-critic and peer-review-validator flagged this as anecdotal — n=1 isn't a tolerance, and the 1% on a 20 kWh day day reads very differently from 1% on a 0.5 kWh day.

### Decision

Replace the single tolerance AC with two complementary acceptance criteria: (a) the specific bug-incident on 2026-05-18 must drop to a recomputed peak ≤ 0.25 kWh (the meter's 0.17 + an 0.08 calibration headroom); (b) for every day with a row, `gridUsageKwh ≤ eInput` as a structural sanity bound.

### Rationale

The first AC ties the success criterion to the actual bug report — if a future regression breaks this case, the test fails. The second is a structural invariant that cannot be wrong by design; together they cover the specific incident plus an "obviously broken" detector for unknown future days. A multi-day validation set was considered but rejected — it blocks on the user manually collecting meter splits for 5+ days, which is friction with marginal benefit beyond the structural bound.

### Alternatives Considered

- **Multi-day validation set (5+ days)**: Stronger statistical guarantee. Rejected — blocks on data the user has to collect manually.
- **Keep the 1% tolerance**: Anecdotal and not robustly testable. Rejected.

### Consequences

**Positive:**
- AC tied directly to the bug report — explicit traceability from incident → fix → test.
- Structural bound is cheap to assert at write-time as a defence-in-depth check.

**Negative:**
- Doesn't catch a regression that overstates peak by a small amount on a day other than 2026-05-18. Mitigated by [[design]] including a drift-logging hook (Decision 6) and by the existing iOS UI showing the kWh figures the user will compare to their bill.

---

## Decision 6: Drift observability is logging-only, no alerting

**Date**: 2026-05-25
**Status**: accepted

### Context

The bug survived in production for a release because no metric or log compared the snapshot-diff number against an integrated baseline. With [[decision-2-keep-snapshot-fields-as-diagnostic]] retaining both `endE*−startE*` and the readings integration on every row, a comparison is essentially free.

### Decision

On every off-peak row write (poller window-end and backfill CLI), emit a structured log entry containing the snapshot-diff value, the readings-integrated value, and the absolute difference per delta. No automatic alerting in scope; the log is queryable in CloudWatch for any operator investigation.

### Rationale

Logging is cheap, immediately useful for ad-hoc diagnosis (`fields date, gridUsageKwh, snapshotGridUsage` in CloudWatch Insights), and lets a future spec add alerting on top without re-instrumenting. Alerting was considered but the threshold ("when is drift large enough to act?") is unclear — better to ship visibility first and decide on thresholds after seeing real distributions.

### Alternatives Considered

- **Log + CloudWatch alarm above N kWh drift**: More valuable end-to-end but the threshold is unjustified up front. Deferred.
- **No observability**: The bug just survived a release for this reason. Rejected.

### Consequences

**Positive:**
- Future drift between AlphaESS's intraday and final eInput becomes visible without a user reporting a bill mismatch.
- Sets up a follow-up observability ticket with concrete data to set thresholds against.

**Negative:**
- Logs alone require operator pull (no automatic notification). Accepted given the two-user system.

---

## Decision 7: Window-end finalisation defers until a reading ≥ offpeak-end

**Date**: 2026-05-25
**Status**: accepted

### Context

A first draft had the poller compute the window-end deltas at exactly `offpeak-end`. The peer-review-validator pointed out this recreates the same class of boundary bug at the readings layer: if the poller fires at 14:00:00 sharp, the last sample may be 13:59:55 and a 14:00:01 charge tail is missed by the integration.

### Decision

Defer the window-end computation until the poller observes at least one reading with timestamp ≥ `offpeak-end`, bounded by a maximum wait of 30 seconds. After the 30 s budget expires, finalise with whatever readings exist (continuing to satisfy the 5-minute persistence deadline of AC 3.2).

### Rationale

The 10-second poller cadence means a reading at-or-after the boundary normally arrives within 5–15 s. Waiting up to 30 s is a small budget that buys the assurance that the last sample inside the window is bounded by a sample at or past the boundary, so trapezoidal integration on the last pair is well-defined (no extrapolation beyond the closing edge). Boundary interpolation was the alternative but it adds complexity to the integration code and produces the same result given dense sampling.

### Alternatives Considered

- **Interpolate from bracketing samples at offpeak-end**: Mathematically cleaner; same numerical result with 10 s sampling. Rejected for code complexity.
- **Fire at offpeak-end + safety buffer (e.g. +30 s) without waiting on a reading**: Simplest. Drops the last ~5 s of charging on ~50% of days. Boundary loss of ~0.01 kWh/day is negligible in isolation but matches the *category* of bug we're fixing, so rejected on principle.

### Consequences

**Positive:**
- No boundary-loss class of bug at the new computation layer.
- Maximum 30 s of additional latency post-window vs the snapshot-diff path's immediate write.

**Negative:**
- The poller now has a wait-on-condition state machine for window-end. Slightly more complex than "tick at 14:00:00".

---

## Decision 8: Sign convention and per-sample clamping policy

**Date**: 2026-05-25
**Status**: accepted

### Context

`flux-readings` stores `pgrid`, `pbat`, `ppv` as signed instantaneous power. Deriving five non-negative kWh deltas from three signed channels requires an unambiguous policy: per-sample clamp before integrating, or integrate the signed value and clamp the total. The two produce different numbers when the channel changes sign within the window.

### Decision

Split the signed power into positive / negative components on each sample before integration: grid import = `max(pgrid, 0)`, grid export = `max(-pgrid, 0)`, battery discharge = `max(pbat, 0)`, battery charge = `max(-pbat, 0)`, solar = `max(ppv, 0)`. Integrate each non-negative-by-sample series independently.

### Rationale

Per-sample clamping matches what a meter or independent energy counter would see — physical energy flow in one direction at one moment doesn't "cancel" flow in the other direction at another moment. Integrating signed values then clamping would zero out a day where 5 kWh imported in the morning and 5 kWh exported in the afternoon, which is wrong. The per-sample policy is also what `computeTodayEnergy` already uses for the daily totals, so the off-peak deltas stay consistent with the daily figures by construction.

### Alternatives Considered

- **Integrate signed power, clamp the total**: Cancels import vs export over the window. Wrong. Rejected.
- **Integrate signed power, take absolute value at the end**: Slightly less wrong but still loses directional information. Rejected.

### Consequences

**Positive:**
- The five deltas behave like five independent meter counters, matching real-world energy accounting.
- Consistency with `computeTodayEnergy`'s existing convention.

**Negative:**
- Negligible additional CPU per sample (one branch and one negation). Not material at the 1080-readings scale.

---

## Decision 9: Single integration helper with selectors, existing siblings retained

**Date**: 2026-05-25
**Status**: accepted

### Context

`derivedstats` already carries two near-identical integration siblings — `integratePload` and `integratePpv` — kept separate by Decision 6 of `specs/solar-by-block/decision_log.md` because folding two functions behind a closure-based generic wasn't worth the indirection cost. This feature needs five power channels integrated identically: pgrid (positive), pgrid (negative), pbat (positive), pbat (negative), ppv (positive).

### Decision

Introduce one new helper `integrate(readings, startUnix, endUnix, selector func(Reading) float64) float64` in `internal/derivedstats/integrate_offpeak.go`. `IntegrateOffpeakDeltas` calls it five times with the five selectors. The two existing siblings (`integratePload`, `integratePpv`) stay as-is — refactoring them is out of scope for the bug fix.

### Rationale

Decision 6 of solar-by-block was made when N=2. At N=5 (or N=7 if the existing two were folded in), the cost-benefit shifts: the algorithmic body is ~100 lines and any future change has to be mirrored five places. A single helper with five selectors mirrors `database/sql` and other selector-pattern stdlib precedent — readable, testable, and removes the "mirror this change five times" tax. The existing two siblings stay because (a) they're used by an unrelated feature and refactoring them now expands the blast radius, (b) the new helper's API is the right shape, so a follow-up that folds the older two in is mechanical.

### Alternatives Considered

- **Five new siblings, consistent with solar-by-block Decision 6**: Matches existing precedent but quintuples the mirroring tax. Rejected.
- **Refactor everything to the generic now**: Touches solar-by-block's tests + integration test suite. Out of scope for a bug fix. Deferred.

### Consequences

**Positive:**
- Single source of truth for the integration algorithm across the new feature's five deltas.
- The new helper's signature is what a future "fold all integrations into one place" refactor would land on.

**Negative:**
- Two integration code paths in `derivedstats` (the new generic + the two existing siblings) until a future refactor consolidates. Documented.

### Impact

Add a one-line `// TODO(offpeak-from-readings): fold into integrate() in integrate_offpeak.go` breadcrumb at the top of `integratePload` and `integratePpv` so the consolidation work is discoverable from the existing sites, not just from this decision log.

---

## Decision 10: Idempotence scoped to delta + count fields, not `integratedAt`

**Date**: 2026-05-25
**Status**: accepted

### Context

A first draft of AC 7.3 asserted the backfill CLI is "idempotent at the row level: re-running produces a row equal byte-for-byte to the prior run." Both the design-critic and the peer-review-validator flagged this as a defect: the `integratedAt` provenance field (Decision 2 + AC 5.4) is by definition the timestamp of the integration run and changes per run.

### Decision

AC 7.3 idempotence applies to the five delta fields (`gridUsageKwh`, `solarKwh`, `batteryChargeKwh`, `batteryDischargeKwh`, `gridExportKwh`) and the two integration-count fields (`integrationSampleCount`, `integrationSkippedPairs`). `integratedAt` is explicitly excluded.

### Rationale

The point of idempotence is "running the CLI twice doesn't corrupt the row, and two runs converge on the same numbers." That holds for the seven fields that describe what the integration computed. `integratedAt` describes WHEN it was computed — a different question, and a more useful piece of diagnostic information if it actually reflects the latest run time. Forcing it to a deterministic value (e.g. max-reading timestamp) sacrifices that diagnostic value for a property nothing relies on.

### Alternatives Considered

- **Force `integratedAt` to a deterministic value** (e.g., max reading timestamp): Preserves the byte-equal claim but loses the "when did the integration last run" diagnostic. Rejected.
- **Drop `integratedAt` entirely**: Saves the field but loses diagnostic value. The new fields are `omitempty` so cost is one string per row. Rejected.

### Consequences

**Positive:**
- The CLI's `--dry-run` summary can compare current vs prior numbers field-by-field with a clear scope of what should match.
- `integratedAt` retains its diagnostic value.

**Negative:**
- A naïve "diff the row" check after a re-run reports a difference on `integratedAt`. Mitigated by the test asserting field-scoped idempotence, not whole-row equality.

---


