# Decision Log: Time-of-Use Pricing

## Quick Decisions

| ID | Date | Decision | Rationale |
|----|------|----------|-----------|
| Q1 | 2026-07-23 | Combine T-1890 and T-1891 into one spec | Plan succession only matters because the new plan's rate structure is arriving; the designs are tightly coupled |
| Q2 | 2026-07-23 | Spec name: `time-of-use-pricing` | Leads with the dominant change; user's pick |
| Q3 | 2026-07-23 | New plan shape: free 10:00–15:00, cheaper rate 01:00–06:00, standard flat rate otherwise | Actual incoming plan, per user |
| Q4 | 2026-07-23 | Successor owns the switch day: plan ending on D and successor starting on D means D is priced by the successor | Matches T-1891's "starts from that same day" phrasing; every day priced by exactly one plan, switch at midnight |
| Q5 | 2026-07-23 | Keep the savings display, valued via an explicit per-plan savings reference rate | User preference; continuity with today's offPeakSavingsRate behaviour with explicit control |
| Q6 | 2026-07-23 | Feed-in stays a single flat rate per plan | Matches the actual plans; no banded exports needed |
| Q7 | 2026-07-23 | Daily supply charge out of scope | Not in either ticket; candidate for a later ticket |
| Q8 | 2026-07-23 | A plan has zero or one free band | All known plans have at most one; keeps window-dependent off-peak features single-window |
| Q9 | 2026-07-23 | Fallback when a day's band split is unavailable: price all import at the plan's highest band rate, no savings | Conservative overestimate mirroring today's all-at-peak fallback; must be identical on every screen per data-consistency rule |
| Q10 | 2026-07-23 | Bands never span midnight; a plan segments 00:00–24:00 | The new plan needs no midnight-spanning band (15:00–24:00 and 00:00–01:00 are separate segments); avoids relaxing the existing midnight-window guard |
| Q11 | 2026-07-23 | Next-window derivations (charge projection, cutoff suppression) use the plan pricing the day the window falls on | On switch eve the successor's window is the one the battery will actually charge in; anchoring to "when the code runs" gives wrong predictions (review finding) |
| Q12 | 2026-07-23 | Paid bands are costing-only; battery features stay tied to the free band | The battery charges in the free window; the cheaper 01:00–06:00 band needs no projection/suppression/masking behaviour. Revisit in a later ticket if wanted |
| Q13 | 2026-07-23 | Per-band splits must outlive the 30-day readings TTL, captured at day close; mechanism decided in design | Otherwise days older than 30 days silently degrade to the fallback forever (review finding); only the cheap band needs new capture — free-band and total imports are already durable |
| Q14 | 2026-07-23 | Poller treats plan-data read failures as transient (last-good schedule / retry), never as "no plan"; Lambda read failures fail the request | An infra blip at a window boundary must not permanently lose a day's split or fabricate an unpriced day |
| Q15 | 2026-07-23 | Fallback days show a $0.00 savings line | Continuity with the existing fallback presentation (daily-costs Decision 16); avoids a display change on a path 5.2 pins as identical |
| Q16 | 2026-07-23 | Band boundaries stay editable after a plan has priced days; stored splits are not recomputed | Rejected immutability-once-priced: a day-one typo in a window must be fixable; AC 4.5 already pins that stored values stand |
| Q17 | 2026-07-23 | All-free plans are rejected (at least one rated band required) | The fallback ("highest band rate") and cost math are undefined on a plan with no rated band |
| Q18 | 2026-07-23 | Coordinated migration cutover, no legacy compatibility layer; verified against recorded pre-migration cost values | Two-user app: record golden values, migrate, verify, update both clients; legacy builds fail safely in the interim |
| Q19 | 2026-07-23 | Sydney timezone named explicitly for plan dates and band boundaries; DST days use wall-clock band membership | Code uses Australia/Sydney throughout; daily-costs' "same TZ as the SSM window" definition dissolves when SSM stops being the source of truth |
| Q20 | 2026-07-23 | Editor captures plans as default rate + exception windows | User pick; matches how retailers describe plans, 3 fields + 2 windows to enter the new plan |
| Q21 | 2026-07-23 | Cost cards keep the current 4-row/4-tile layout | User pick; band detail is not worth the busier card |
| Q22 | 2026-07-23 | Migration via `cmd/migrate-pricing` CLI, dry-run default, aborts unless golden cost check passes | Matches the existing `cmd/backfill-*` operator-tool pattern; verification is built into the same run (AC 5.3) |
| Q23 | 2026-07-23 | Each stored `bandImports` entry snapshots its own geometry (`{start,end,kwh}`) | Self-describing after later plan window edits (Q16); join-by-geometry makes staleness detectable |
| Q24 | 2026-07-23 | Backfill CLIs resolve the window per day from the pricing table instead of `--offpeak-start/end` flags | A backfill spanning the switch date needs per-day windows; static flags would silently misattribute |
| Q25 | 2026-07-23 | Lambda reads the pricing table per request (added to existing errgroups), no caching | Table holds a handful of rows; a Scan is negligible next to the existing four queries. Revisit only if latency data says otherwise |
| Q26 | 2026-07-23 | `Segments` does not merge abutting same-rate segments | Stable geometry keeps the stored-split join deterministic |
| Q27 | 2026-07-23 | Poller's daily cycle re-anchored to midnight: refresh plans, resolve the day's window, then sleep to its start | Plans only change behaviour at midnight (AC 2.2), so one refresh per day is sufficient and the switch-day window is always current |
| Q28 | 2026-07-23 | Dynamo read path converts legacy rows via the migration tool's transform until migration runs; deleted afterwards | Decouples deploy order from the migration run without a permanent dual code path |
| Q29 | 2026-07-23 | `/status` does not carry `bandImports` | Dashboard shows no costs; Day Detail and History get the split from `/day` and `/history` |
| Q30 | 2026-07-23 | Tier 2 of cost resolution is the legacy `DayCosts` formula verbatim (server-peak preference, zero clamp, nil-offpeak path), not the residual | Stored `peakGridImportKwh` differs ~1.5% from `eInput − offpeak`; the residual would change essentially every historical day's cost and abort the migration golden check (review finding) |
| Q31 | 2026-07-23 | flux-offpeak row exclusively owns free-window import; `bandImports` stores rated segments only; offpeak row snapshots its window geometry | One writer per physical quantity (Data Consistency, Q13); avoids a second capture of the same kWh that backfill repairs could desynchronise (review finding) |
| Q32 | 2026-07-23 | `replace-open-ended` rejects (`legacyShape`) when the closing row is still legacy-shape, rather than rewriting it in-transaction | The closing write is a partial UpdateItem; a rewrite needs predecessor state the call doesn't carry and risks clobbering concurrent edits; cutover order already runs migration first (review finding, validator-adjudicated) |
| Q33 | 2026-07-23 | Summarisation pass gains typed per-outcome gating (read failure → retry, no sentinels; no free band → window-independent blocks still run; no plan → terminal until backfill) | The old single early-return would starve socLow/dailyUsage/peak/bands forever on no-window days (review finding) |
| Q34 | 2026-07-23 | Band boundaries parsed by a new parser accepting 24:00; `ParseOffpeakWindow` not reused | `ParseOffpeakWindow` rejects `h > 23` and would reject every plan (review finding) |
| Q35 | 2026-07-23 | `/status.offpeak` becomes nullable; nil means "no window" and clients never substitute the default-window constants | A no-free-band day has no window strings; widget defaults would falsely render the legacy window (review finding) |
| Q36 | 2026-07-23 | `handleEnd`/recovery relaxed to permit readings-only finalisation without a pending row or start snapshot | A plan-read failure at window start must not permanently lose the day (AC 4.6); the snapshot is diagnostics-only since T-1341 |

## Decision 4: Store Plans as Default Rate + Exception Windows

**Date**: 2026-07-23
**Status**: accepted

### Context

The band model (Decision 1) needs a storage and wire representation. The requirements demand contiguous full-day coverage (AC 1.1) with validation codes for gaps/overlap/coverage (AC 7.2), and the user chose an editor that captures a default rate plus exception windows.

### Decision

Plans are stored and transmitted as entered: `defaultRate` + `windows` (each free or rated). The full-day segmentation is derived on demand by a shared `plan.Segments` helper (Go) and its FluxCore mirror, pinned to each other by shared test vectors.

### Rationale

Storing the source form round-trips the editor exactly (no lossy reconstruction of "which segment was the default"), and makes gap/coverage violations unrepresentable — uncovered time simply carries the default rate. Validation reduces to window overlap, bounds, precision, and free-band count.

### Alternatives Considered

- **Store derived segment list**: Canonical bands on the wire - Rejected because the editor would need to reconstruct default-vs-exception on open (ambiguous), and gap/coverage validation reappears
- **Fixed columns for the known plan shapes**: Minimal schema - Rejected in Decision 1 already; same reasons apply to the wire shape

### Consequences

**Positive:**
- Editor state == stored state; no derivation on save
- Invalid-by-construction states (gaps, partial coverage) cannot reach the store

**Negative:**
- Segmentation logic exists in both Go and Swift and must be kept identical (mitigated by shared test vectors, the established `note_lengths.json` pattern)

---

## Decision 5: Exclusive End Dates in the New Plan Shape

**Date**: 2026-07-23
**Status**: accepted

### Context

Legacy periods use inclusive end dates (`covers` is `date <= endDate`), and `replace-open-ended` closes the predecessor at `startDate − 1`. Q4 settled that the successor owns the switch day, and T-1891 phrases the flow as "ends that day / starts that same day".

### Decision

New-shape `endDate` is the exclusive switch date: `covers(d) = startDate ≤ d < endDate`. Succession stores the same literal date on both rows. Migration maps legacy inclusive ends to `legacyEnd + 1 day`.

### Rationale

The stored form matches both the user's mental model and the switch-day semantics with no ±1 arithmetic anywhere in validation, succession, or display. Overlap checking becomes standard half-open interval intersection.

### Alternatives Considered

- **Keep inclusive ends, translate in the UI**: No storage semantics change - Rejected because every consumer (overlap check, succession, Swift `covers`, remediation copy) would carry the ±1 translation instead of exactly one place (the migration tool)

### Consequences

**Positive:**
- Succession writes the same date to both rows — directly expresses T-1891
- Half-open intervals compose cleanly with lexicographic date comparison

**Negative:**
- Two end-date semantics exist in the wild until migration runs (bounded by the cutover ordering in AC 5.4)
- Anyone reading raw table data must know which shape a row is (detectable via the `peakRate` attribute)

---

## Decision 6: Three-Tier Cost Resolution

**Date**: 2026-07-23
**Status**: accepted

### Context

Banded costing needs a per-day import split, which only exists for days closed after this feature ships. Historical days have the two-way off-peak split (`offpeakGridImportKwh` + durable totals) but no `bandImports`, and AC 5.2 requires their costs to be unchanged.

### Decision

FluxCore resolves a day's cost in order: (1) stored `bandImports` whose geometry exactly matches the day's plan segmentation; (2) when the plan's rated segments share a single rate — every migrated legacy plan — the existing two-way off-peak split; (3) the AC 3.5/3.6 fallback (all import at the highest rate, $0.00 savings).

### Rationale

Tier 2 is what makes migration lossless without backfilling history: a free + single-rate plan's band costs are fully determined by the two-way split, so pre-feature days price identically to today. Tier 1 is the only tier that can price a multi-rate plan exactly; tier 3 is the consistent, conservative floor required by the requirements.

### Alternatives Considered

- **Backfill `bandImports` for all history**: One uniform path - Rejected: readings older than 30 days are gone, so a backfill cannot reach most history; tier 2 covers those days exactly with data that already exists
- **Fallback whenever `bandImports` is absent**: Simplest client - Rejected because it silently changes every historical day's cost, violating AC 5.2

### Consequences

**Positive:**
- AC 5.2 holds with zero data migration of daily-energy rows
- The resolution order is a pure function of one day's data — testable via the shared cross-language vectors

**Negative:**
- Three code paths in the cost helper (bounded: tier 2 reuses the existing legacy formula, tier 3 the existing fallback)

---

## Decision 1: Model Plans as Daily Time Bands

**Date**: 2026-07-23
**Status**: accepted

### Context

The current pricing model gives each period exactly three flat rates (peak, feed-in, off-peak savings), with the free window held in SSM configuration outside the plan. The incoming plan has a free window plus two different import rates at different times of day, which the three-rate model cannot express. The `daily-costs` spec explicitly deferred time-of-use bands as out of scope, anticipating this change might come.

### Decision

A plan is an ordered set of contiguous, non-overlapping time bands exactly covering the 24-hour day, each carrying an AUD/kWh rate or marked free, plus a single flat feed-in rate and (when a free band exists) a savings reference rate.

### Rationale

The band model expresses both the current plan (free window + one flat rate) and the new plan (free window + two rates) in one schema, so there is a single validation, cost-computation, and UI path. A future plan with different windows or an extra rate needs no schema change. This supersedes the `daily-costs` decision that rejected time-of-use bands — the situation it anticipated ("close one period and start another when the real-world contract changes") has now arrived, but the contract change also changed the rate structure, which that decision did not cover.

### Alternatives Considered

- **Fixed shape (free window + two named rates)**: Models exactly the new plan - Rejected because the next plan shape change would force another schema migration, and the old plan would still need a degenerate mapping
- **Flat rate + optional second rate window**: Minimal delta on the current model - Rejected because it special-cases every consumer (validation, cost math, UI) for one plan generation and still cannot express a third band

### Consequences

**Positive:**
- One schema and one code path for all past and future plans
- Cost computation is a uniform sum over bands
- The free window becomes a property of the plan, enabling Decision 2

**Negative:**
- Larger one-time change: model, validation, API payloads, editor UI, and cost math all change shape
- Requires migrating existing rows (Decision 3)

---

## Decision 2: The Active Plan Is the Source of Truth for the Free Window

**Date**: 2026-07-23
**Status**: accepted

### Context

The free/off-peak window (11:00–14:00) lives in SSM parameters consumed by the poller (window boundary processing, off-peak integration) and the Lambda API (off-peak splits, charge projection, cutoff suppression, peak masking). With band-based plans, the free window is a property of the plan — and the new plan moves it to 10:00–15:00 on the switch date. Keeping SSM authoritative would mean two definitions of the window that must be manually kept in sync exactly when they diverge.

### Decision

All window-dependent features derive the free window from the free band of the plan that prices the day in question. Separately maintained window configuration ceases to be the source of truth.

### Rationale

One definition of the window, switching automatically at the succession boundary, with per-day correctness for historical dates. This matches the project's data-consistency rule: a value used on multiple screens must come from one source. The manual alternative would have required an SSM update at the exact switch date and would still misattribute the window for historical days after the change.

### Alternatives Considered

- **Keep SSM authoritative for the poller, plan bands for pricing only**: Smaller scope - Rejected because the window would be defined twice, and off-peak stats would silently disagree with pricing after a plan switch until someone edits SSM
- **Defer to a follow-up ticket**: Ship pricing first - Rejected by the user; the switch date is known and near, so the window change must be handled in the same feature

### Consequences

**Positive:**
- The plan switch date changes rates and window together, atomically, per day
- Removes a manually synchronised configuration value

**Negative:**
- The poller gains a dependency on the pricing data (it currently never touches `flux-pricing`)
- Window boundary scheduling must follow plan data rather than static configuration, including across a switch date

### Impact

Touches the poller's off-peak jobs, the API's off-peak env-var configuration, and every window consumer listed in requirement 4.1.

---

## Decision 3: Migrate Existing Periods to the Band Model Once

**Date**: 2026-07-23
**Status**: accepted

### Context

The `flux-pricing` table holds existing three-rate periods that price historical days. The band model changes the row shape. Either the old shape remains supported alongside the new one, or existing rows are converted once.

### Decision

Existing periods are migrated once into the band model (free band 11:00–14:00 matching the window their historical data was computed under, flat-rate band for the remainder, feed-in unchanged, savings reference rate = former off-peak savings rate). The legacy shape is not accepted or served afterwards.

### Rationale

The migrated representation is semantically identical for costing (requirement 5.2 pins historical cost equality), so keeping the legacy shape would buy nothing except permanent dual code paths in validation, cost math, and UI for a two-user app with a handful of pricing rows.

### Alternatives Considered

- **Support both shapes indefinitely**: No migration risk - Rejected because every consumer would carry two code paths forever, for data that converts losslessly
- **Versioned rows with on-read conversion**: Convert lazily - Rejected as needless machinery for a table with a handful of rows; a one-time conversion is simpler and verifiable

### Consequences

**Positive:**
- One schema everywhere after cutover
- Historical costs provably unchanged (5.2 is testable)

**Negative:**
- Requires a one-time, verified migration step during rollout
- Older app builds that only speak the three-rate shape stop working against the migrated API (acceptable: two users, same household)

---
