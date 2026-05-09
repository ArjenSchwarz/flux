# Decision Log: Solar by Block

## Decision 1: Block coverage limited to daylight blocks

**Date**: 2026-05-09
**Status**: accepted

### Context

The existing five-block model (night, morning peak, off-peak, afternoon peak, evening) covers the whole day. Solar power is only produced between sunrise and sunset, which in practice falls inside the morning peak, off-peak, and afternoon peak blocks — these are bounded by first/last solar timestamps and the SSM-configured off-peak window. Night and evening blocks lie entirely outside the solar window.

### Decision

Show and persist solar kWh only for the three daylight blocks: morning peak, off-peak, afternoon peak.

### Rationale

Solar values on night and evening blocks would always be zero or near-zero (only sensor noise) and would crowd the row layout without adding insight. Limiting to daylight blocks also keeps the schema change minimal.

### Alternatives Considered

- **All five blocks**: Show solar on every row for consistency - Rejected because night/evening would render `0.0 kWh` permanently and add noise.
- **Auto-hide zero rows**: Show solar on all blocks but hide when ≤0.05 kWh - Rejected because it makes layout inconsistent across days.

### Consequences

**Positive:**
- Smaller schema change (three values per day, not five).
- Cleaner UI: night/evening rows untouched.

**Negative:**
- A future change to morning peak boundary semantics could leak solar into evening; the implementation must enforce the kind check.

---

## Decision 2: Persist solar in DailyUsageBlock + standalone CLI backfill

**Date**: 2026-05-09
**Status**: accepted

### Context

Two compute paths exist: today's data is computed fresh from `flux-readings`, and historical days read pre-computed blocks from `flux-daily-energy`. Raw readings have a 30-day TTL, so any approach that recomputes from readings on read would degrade for older days. We also have ~12 months of historical days whose readings have already expired.

### Decision

Extend the `DailyUsageBlock` DynamoDB record with a solar kWh value per daylight block. When the API computes blocks for a day whose readings are still available, persist the new values back. Provide a standalone Go CLI (under `cmd/`) that backfills the values for all days in `flux-daily-energy` whose readings are still present, with dry-run support and idempotency.

### Rationale

Persisting matches how `totalKwh` is already stored, so the read path is unchanged. A standalone CLI is the simplest backfill — manual, auditable, no infra changes, no risk of recurring background work. Days whose readings have already expired return null for the new field; the iOS UI handles null by hiding the row's solar value, so older days degrade gracefully.

### Alternatives Considered

- **Always recompute from readings**: No persistence - Rejected because data older than 30 days would never have solar values and the recompute on every read wastes Lambda time.
- **Lambda-based backfill**: One-shot Lambda triggered manually - Rejected because it adds infra to the stack just to be deleted later.
- **Lazy backfill on first read**: API writes back when reading a stale block - Rejected because read-path writes complicate concurrency and the savings are marginal.

### Consequences

**Positive:**
- Read path stays simple; no recompute overhead.
- Older history (≤30 days at deploy time) gets values via backfill.
- Days predating the readings TTL show null gracefully.

**Negative:**
- Cannot fully backfill days whose readings have already expired — the user accepts those will display without solar.
- Adds one more DynamoDB attribute per block, slightly larger items.

---

## Decision 4: Persistence skipped when no `ppv` samples in a block

**Date**: 2026-05-09
**Status**: accepted

### Context

The original draft specified that the backfill CLI must be idempotent and "SHALL NOT overwrite existing solar values with zero when readings are missing." Both the design-critic and external reviewers (Gemini, Kiro) flagged that this rule cannot be implemented without distinguishing a stored real-zero from a stored null/missing value, which would require an extra `solarComputed` flag or convoluted conditional writes. It also conflicts with the live `/day` path that legitimately overwrites partial values throughout a day.

### Decision

Persistence is gated on whether any `ppv` samples fall inside the block. WHEN samples are present, write the computed value (overwriting any prior). WHEN there are no samples, skip the write entirely (preserve whatever was previously stored, including absence).

### Rationale

This rule is implementable with a single `if len(samplesInBlock) > 0` check, no schema flags, no conditional writes. It naturally covers all the awkward cases: live partial-day refreshes, backfill re-runs after readings expired, and missing-data days. A genuinely zero-yield block (rare but possible — heavy fog from sunrise to sunset on a winter day) does have samples reading near zero, so the integral is honestly stored as ~0.

### Alternatives Considered

- **Conditional DynamoDB writes** (`attribute_not_exists` plus a `solarComputed` flag): Works, but adds a flag to the schema and adds branching to the writer. - Rejected because the sample-count rule achieves the same result with less ceremony.
- **Always overwrite**: Live path and backfill always write, even when no samples. - Rejected because it would clobber legitimate stored values whenever the readings table loses data.

### Consequences

**Positive:**
- Implementation is one line plus a guard.
- No schema flag needed.
- Handles backfill, live refresh, and TTL-expired readings consistently.

**Negative:**
- A block where `ppv` is genuinely absent for a whole day (sensor failure) will read as null even if the day completed — but that's the correct semantics anyway.

---

## Decision 5: Drop AC requiring solar-block sum to match `epv`

**Date**: 2026-05-09
**Status**: accepted

### Context

The original draft required the sum of daylight-block solar values to be within 0.1 kWh of `DaySummary.epv`. Reviewers pointed out that `epv` is an AlphaESS meter snapshot from `getOneDateEnergyBySn`, while the per-block values are integrals of instantaneous `ppv`. These are different physical quantities — the meter integrates with its own sampling at the inverter, possibly subject to clipping and rounding — and they can legitimately differ by more than 0.1 kWh, especially on low-yield winter days.

### Decision

Drop the cross-check from requirements. Add an internal consistency test in the tasks/tests phase that compares the sum of three blocks to a separate full-day `ppv` integral over the same time window — this verifies the implementation, not the AlphaESS meter alignment.

### Rationale

A requirement that depends on third-party meter behaviour is not testable in our control. Internal consistency (block-sum vs full-window integral) is testable and catches actual implementation bugs.

### Alternatives Considered

- **Loosen the tolerance to 1 kWh or 5%**: Keep the AC, accept noise. - Rejected because the underlying mismatch is structural, not a tolerance problem.
- **Replace with a non-functional accuracy target**: e.g., "within 1% on >5 kWh days". - Rejected as still mixing meter and integration semantics.

### Consequences

**Positive:**
- Acceptance criteria stay testable end-to-end.
- Internal consistency test still catches integration regressions.

**Negative:**
- No automated cross-check against the AlphaESS meter; if AlphaESS reports a wildly different `epv` than our integration, that goes unnoticed unless someone looks at the chart.

---

## Decision 6: `integratePpv` as a sibling function, not a generalised helper

**Date**: 2026-05-09
**Status**: accepted

### Context

The new solar integration mirrors `integratePload` almost line-for-line: same trapezoidal algorithm, same 60s pair-gap rule, same edge synthesis, same half-open interval. A field-selector closure could collapse them to one helper, saving ~50 lines.

### Decision

Implement `integratePpv` as a sibling function in `internal/derivedstats/integrate.go` (or a new file) that follows the same shape as `integratePload`.

### Rationale

`integratePload` is heavily commented and references a specific design doc; turning it into a closure-based generic obscures the algorithmic clarity for marginal LOC savings. The two consumers (load and ppv) are stable and the duplication is bounded.

### Alternatives Considered

- **Generic `integrate(readings, fieldFn)`**: One function, two thin wrappers. - Rejected because the closure indirection slows readability at the integration loop's hot path and the saved lines aren't worth the refactor blast radius.
- **Inline solar integration into `Blocks()`**: Skip the helper entirely. - Rejected because the algorithm's edge synthesis logic is too intricate to inline cleanly.

### Consequences

**Positive:**
- Solar integration reads at the same level of abstraction as load.
- No closure plumbing; tests stay direct.

**Negative:**
- Two functions to keep in sync if the algorithm ever changes (e.g., a different gap rule).

---

## Decision 7: Backfill patches `solarKwh` in place; preserves all other fields

**Date**: 2026-05-09
**Status**: accepted (revised after review)

### Context

The backfill CLI needs to set `solarKwh` on selected blocks within an existing `dailyUsage` map. An earlier draft of this decision had the CLI rewrite the whole `dailyUsage` attribute via `DailyUsageToAttr(Blocks(...))`. Both the design-critic and external peer review (Gemini, Kiro) flagged a real risk: `Blocks()` derives morning/afternoon peak boundaries from the first/last `ppv > 0` readings. Readings have a 30-day TTL. If readings have been partially pruned since the original summarisation, recomputing against the surviving subset can shift block boundaries and change `totalKwh` away from the value originally written. A whole-attribute rewrite would silently overwrite historically correct values.

### Decision

The backfill reads the row's existing `DailyUsageAttr` from DynamoDB. It runs `Blocks()` against the current readings to extract `SolarKwh` per daylight block, then patches that field onto the **existing** block whose `Kind` matches. Every other field (`TotalKwh`, `Start`, `End`, `BoundarySource`, `PercentOfDay`, `Status`) is copied through verbatim from the stored row. The patched attribute is written back via `SET dailyUsage = :du`.

### Rationale

In-place patching guarantees the historically-stored derived stats are preserved byte-for-byte, even if readings have been pruned or the algorithm subtly shifts. It still uses one `UpdateItem` per row; the only added cost is a per-block deep copy in memory.

### Alternatives Considered

- **Whole-attribute rewrite via `DailyUsageToAttr(Blocks(...))`**: One line of code shorter, but as noted above, vulnerable to TTL-pruning drift. - Rejected.
- **Per-block nested update via `SET dailyUsage.blocks[i].solarKwh = :v`**: List-index addressing in DynamoDB is awkward and adds expression-name complexity. - Rejected.
- **Conditional write with `attribute_not_exists`**: Idempotent re-runs make it unnecessary. - Rejected.

### Consequences

**Positive:**
- Historical `totalKwh` and boundary metadata are preserved exactly.
- One `UpdateItem` per row.
- Idempotent re-runs are still cheap (skip rows already populated).

**Negative:**
- Slightly more code than the whole-attribute rewrite (one extra match-by-`Kind` loop).

---

## Decision 8: `integratePpv` returns `(kwh, sampleCount)`; presence drives nil vs 0.0

**Date**: 2026-05-09
**Status**: accepted (revised after review)

### Context

A daylight block with no readings inside its window must emit `SolarKwh = nil`. A daylight block fully covered by near-zero readings (heavy fog) must emit `SolarKwh = &0.0`. An earlier draft had a separate `hasPpvSample` helper plus a `solarSampled` flag tracked inline in `Blocks()`. Reviewers (explain-like + design-critic) flagged the duplication.

### Decision

`integratePpv` returns `(kwh float64, sampleCount int)`. `Blocks()` sets `pendingBlock.unroundedSolarKwh = kwh` and `pendingBlock.solarSampled = sampleCount > 0`. `buildDailyUsageBlock` emits `nil` when `solarSampled == false` (or kind is night/evening), and a rounded `*float64` otherwise.

### Rationale

Folding the count into the integration function avoids a second pass over the same readings slice and keeps the contract explicit at the call site. The two values flow through one return.

### Alternatives Considered

- **Separate `hasPpvSample` helper**: Two scans per block, two functions to keep aligned. - Rejected (duplication without benefit).
- **Sentinel value (e.g., `-1`)**: Breaks `*float64` semantics and Codable round-trip. - Rejected.
- **Single-pass extension of the existing first-pass loop in `Blocks()`** (lines 60–73): That scan runs before block boundaries are known, so per-block counters can't be assigned there. - Rejected.

### Consequences

**Positive:**
- One scan per block; one source of truth for presence.
- Clear contract; nil and zero are not confused.
- iOS UI can rely on null/non-null distinction (per AC 3.4 vs 3.5).

**Negative:**
- `integratePpv` signature differs from `integratePload` (which returns just `float64`). Acceptable — they serve different consumers.

---

## Decision 3: Surface limited to Day Detail; inline display with sun icon

**Date**: 2026-05-09
**Status**: accepted

### Context

The ticket explicitly mentions the details page. Solar splits could also be useful on Dashboard or History but extend scope. The five-block panel already shows `time range / kWh` per row; adding solar alongside is the obvious place to present it.

### Decision

Show solar only on the Day Detail page's five-block panel. Render the solar value inline next to the existing usage kWh, prefixed with a sun icon coloured to match the amber "Solar" series in the power chart.

### Rationale

Keeps scope tight to the ticket. Inline rendering reuses existing row layout and avoids doubling vertical space. The sun icon visually disambiguates solar from load.

### Alternatives Considered

- **Two-line stack per row**: Usage line on top, solar line below - Rejected because it adds vertical clutter.
- **Solar replaces usage on daylight rows**: Show solar instead of load - Rejected because it loses the existing usage information.
- **Surface on History/Dashboard too**: Wider rollout - Rejected as out-of-scope; can be added later.

### Consequences

**Positive:**
- Minimal UI churn; easy to revert or extend later.
- Keeps row height consistent with night/evening rows.

**Negative:**
- Inline layout may pinch on smaller iPhone screens; design must verify on smallest target.
