# Decision Log: Peak From Readings

## Decision 1: Compute peak via direct integration instead of as a residual

**Date**: 2026-05-30
**Status**: accepted

### Context

The off-peak-from-readings feature (T-1341) introduced trapezoidal integration of 10-second `Pgrid` samples to compute `flux-offpeak.gridUsageKwh`. Empirical comparison against an OVO Energy meter CSV (May 2026, 24 days) showed the integration loses ~1.5% of energy relative to OVO's continuous meter — a sampling artifact: AlphaESS's internal counter integrates at sub-second resolution while the 10-second samples we poll are instantaneous power. AlphaESS's daily cumulative counter (`eInput`) is only exposed at the daily level.

For past days the iOS app reads stored `eInput` (high-accuracy, ~7 Wh/day vs meter) and stored `offpeakGridImportKwh` (low-accuracy, ~1.5% under), then displays peak as `eInput − offpeakGridImportKwh`. With peak values averaging ~0.45 kWh/day, the absolute 0.18 kWh sampling-loss error becomes a ~43% relative error on the displayed peak number.

### Decision

Compute `peakGridImportKwh` server-side as a direct trapezoidal integration of `Pgrid` over the two windows bracketing the off-peak window: `[day-start, offpeak-start)` and `[offpeak-end, next-day-start)`. Expose it as a new field so the iOS app stops subtracting.

### Rationale

Both peak and off-peak now use the same integration method over complementary windows; the sampling error is proportional to each window's own kWh throughput rather than concentrated on the residual. The peak window typically carries far less grid import than off-peak (no grid charging happens outside the cheap-tariff window), so 1.5% of peak is ~7-30 Wh/day — within sensor noise.

### Alternatives Considered

- **Accept the current pattern.** Absolute dollar impact is ~$1/month at typical AU feed-in rates. Rejected: the chart display of peak as a fraction of grid usage is visibly wrong relative to the bill, and the user-visible inconsistency erodes trust in the rest of the dashboard.
- **Snapshot-anchored scaling.** Scale integration by `eInput / fullDayIntegratedImport` to anchor to the authoritative daily counter. Rejected: muddies provenance (the stored value would no longer be a clean integration of what was measured), and "calibration multiplier" is an indirection that's hard to reason about and hard to test.
- **Calibration multiplier (×1.015).** Trivial code change. Rejected: the bias is unlikely to be uniform across days or across the five integrated channels, and lying about what was measured is the wrong way to fix a small systematic error.
- **Compute peak from `getOneDayPowerBySn` 5-minute snapshots.** AlphaESS exposes 5-min power snapshots via this endpoint. Rejected: 5-min sampling is coarser than the 10-second readings we already have — it would worsen the artifact we're mitigating.

### Consequences

**Positive:**
- Peak and off-peak treated symmetrically; the iOS display is no longer the noisy residual of two larger numbers.
- Reuses existing integration code with no new numerical methods.
- No schema migration, no new table.

**Negative:**
- One more derived field to maintain on `DailyEnergyItem`.
- `peakGridImportKwh + offpeakGridImportKwh ≠ eInput` exactly; they differ by ~1.5% of `eInput` due to the sampling artifact. No UI surface currently displays all three side-by-side, so this is an operator-visible anomaly only. Documented in the requirements as the 3% tolerance bound.

---

## Decision 2: Store on `flux-daily-energy`, not `flux-offpeak`

**Date**: 2026-05-30
**Status**: accepted

### Context

The new `peakGridImportKwh` value needs a home in DynamoDB. Candidates: extend `flux-offpeak`, extend `flux-daily-energy`, or create a new `flux-peak` table.

### Decision

Add `peakGridImportKwh` as an optional attribute on `dynamo.DailyEnergyItem` (the `flux-daily-energy` row), computed and written by the existing hourly daily-summarisation pass alongside `DailyUsage` / `SocLow` / `PeakPeriods` (internal/poller/dailysummary.go:83).

### Rationale

`flux-daily-energy` is the per-day summary row, and peak grid import is a per-day summary quantity. The summarisation pass already runs hourly, already loads the full day's readings, and already writes a multi-attribute `UpdateItem`. Adding one more attribute is the lowest-friction integration point.

### Alternatives Considered

- **Extend `flux-offpeak`.** Co-locates peak with off-peak (computed from same readings). Rejected: `flux-offpeak` is semantically "what happened in the off-peak window" — putting peak data there is naming-confusing, and the row is written at off-peak-end (14:00:00) which is before the evening peak window is complete.
- **New `flux-peak` table.** Cleanest separation. Rejected: storage shape would be identical to `flux-daily-energy` (one row per `(sysSn, date)`), so a separate table adds infrastructure for no gain.

### Consequences

**Positive:**
- Reuses an existing hourly pass and an existing write path.

**Negative:**
- `DailyEnergyItem` continues to accumulate optional fields; readers must handle each one being absent.

---

## Decision 3: Use a dedicated `peakComputedAt` sentinel for backfill

**Date**: 2026-05-30
**Status**: accepted

### Context

The summarisation pass uses `derivedStatsComputedAt` as an idempotency sentinel — rows where it is set are skipped entirely (dailysummary.go:53-56). After deploying the new field, every existing row's sentinel is already set, so the pass would not populate `peakGridImportKwh` on any historical row unless something else triggers a recompute.

### Decision

Add a second sentinel field `PeakComputedAt` to `DailyEnergyItem`. The pass's skip-result check becomes "both `DerivedStatsComputedAt != ""` AND `PeakComputedAt != ""`". The existing derived-stats block stays gated on the existing sentinel; a new peak-compute block is gated on the new sentinel.

The hourly pass runs for "yesterday" only (Decision 4), so the second sentinel makes the pass **forward-fill** peak onto each day's row as it becomes yesterday, independently of the derived-stats block. It does **not** reach rows that are already older than yesterday at deploy time — those are covered by a one-shot backfill CLI (Decision 7). The sentinel also keeps both writers (hourly pass and CLI) idempotent and lets either write the peak group without clobbering the derived-stats group.

### Rationale

The two sentinels are orthogonal: peak can be written on a row that already has derived stats (and vice versa) without re-doing or clobbering the other group, so the same write path (`UpdateDailyEnergyDerived`) serves both the hourly forward-fill and the historical CLI. Future "derived field X with independent lifecycle" additions follow the same pattern.

The original framing of this decision claimed the hourly pass would backfill the whole 30-day window automatically. That was wrong: the pass only ever processes yesterday (Decision 4), so historical rows older than yesterday at deploy would never be visited. Historical coverage is therefore delegated to the CLI of Decision 7; the sentinel's role here is forward-fill plus cross-writer idempotency, not historical backfill.

### Alternatives Considered

- **Make the hourly pass scan the whole TTL window each tick** and fill peak wherever `PeakComputedAt` is empty. Rejected: adds a per-tick range scan to a pass that is otherwise a single-row operation, and diverges from how every other readings-derived field in this repo is backfilled (one-shot CLI — see `cmd/backfill-offpeak`, `cmd/backfill-solar`).
- **Operator runbook: one-off `UpdateItem` to clear `DerivedStatsComputedAt` on each row.** Rejected: clearing the derived-stats sentinel forces the other stats (DailyUsage, SocLow, PeakPeriods) to be recomputed unnecessarily, and there is no safety against re-clearing and re-running. (`flux-daily-energy` is one row per day, ~30 rows in the TTL window — the operational concern is correctness, not row count.)
- **Repurpose `derivedStatsComputedAt` to mean "all derived stats including peak".** Rejected: would require either a coordinated deploy (clear all sentinels on rollout) or a versioning field. The two-sentinel design avoids both.

### Consequences

**Positive:**
- Peak is written independently of derived stats by both the hourly pass and the backfill CLI, with no clobbering and idempotent re-runs.
- Forward-fill of each new day is automatic.
- Pattern generalises to future per-field derived stats.

**Negative:**
- One more sentinel field on `DailyEnergyItem`.
- The pass's skip-result logic is slightly more complex (two conditions instead of one).
- Historical rows (older than yesterday at deploy) are not filled by the pass; they require the one-shot CLI of Decision 7. Until it is run — or those rows age out of the TTL — those days use the iOS residual fallback (Decision 4).

---

## Decision 4: Today and pre-30-day rows use the iOS fallback

**Date**: 2026-05-30
**Status**: accepted

### Context

The summarisation pass runs for "yesterday" only (dailysummary.go:25) so today's row is never reached by the pass during the same day. Independently, `flux-readings` has a 30-day TTL, so rows older than 30 days at deploy have no readings to integrate. Both cases result in `peakGridImportKwh` being absent from the API response.

The iOS app falls back to `max(0, day.eInput − offpeakImport)` when the new field is absent — i.e. the existing production calculation.

### Decision

Accept this. Do not add a real-time peak path in the API for today, and do not add cross-TTL persistence for pre-30-day rows.

### Rationale

For **today**, `day.eInput` in the API response is `max(stored, computed)` (compute.go:328-334, `reconcileEnergy`). The computed side is the same trapezoidal integration the new field uses, the stored side is the AlphaESS counter at the most recent hourly poll. Within the active hour, computed is fresher than stored; outside the active hour, stored wins. In either case the iOS fallback's error is bounded: it is at-worst the current production behaviour and at-best (when both sides are live integration) the errors cancel to ~1.5% on peak instead of ~43%. The user's complaint is about historical days; today's display is unchanged from today's behaviour.

For **rows older than 30 days at deploy**, the iOS fallback displays the same value the app currently shows (no regression). The discontinuity in the History chart between recent days (using the integrated field) and pre-30-day days (using the residual) rolls forward with the TTL window but never produces a value worse than today's production. Persisting peak in a way that survives the readings TTL would require either a separate peak-snapshot table (rejected — Decision 2 logic) or a backfill from the snapshot fields that's known to have its own correctness issues (T-1341 decision log).

### Alternatives Considered

- **Add a real-time peak computation path in `internal/api/day.go` for today.** Mirrors the existing `offpeakSplit` pattern (day.go:200-204). Rejected: ~30 LOC of API-layer complexity, and the worst-case today behaviour is no-worse than current production — see rationale. Worth revisiting if user feedback shows today's peak being notably wrong.
- **Persist peak with a longer TTL than `flux-readings` (e.g. recompute and store at the end of each day, keep forever).** Rejected: would eliminate the rolling-30-day discontinuity but adds a new write path with its own correctness questions, and the discontinuity is invisible to the user during normal use (they don't generally re-open History from 60 days ago to compare with last week).

### Consequences

**Positive:**
- Implementation stays small and additive.
- No new API-layer compute path, no new TTL-resistant storage.

**Negative:**
- Today's peak in History uses a different calculation method than yesterday's onward. Documented but not surfaced to the user.
- Historical chart has a slowly-rolling discontinuity at deploy-time minus 30 days. Documented but not surfaced.

---

## Decision 5: Only `peakGridImportKwh`; not peak versions of solar / export / battery channels

**Date**: 2026-05-30
**Status**: accepted

### Context

`IntegrateOffpeakDeltas` returns five channels (grid import, grid export, solar, battery charge, battery discharge). Adding peak versions of all five would cost no additional compute (same readings, same integrator) and only marginal storage.

### Decision

Only compute and store `peakGridImportKwh`. The other four channels are not added.

### Rationale

YAGNI. The user-visible problem this spec addresses is the peak grid import display on the History card. No current iOS view displays "peak solar generation" or "peak battery discharge". Adding fields with no consumer adds API surface, iOS model fields, and tests for code paths that no human will look at.

### Alternatives Considered

- **Add all five channels at the storage layer, expose only `peakGridImportKwh` in the API.** Rejected: the storage write path needs the same test coverage as the API path; the marginal cost of "free" channels is not actually zero once tests and read-side code are included.
- **Add all five channels everywhere (storage and API).** Rejected: speculative API surface. If a future spec needs peak-solar or peak-discharge, it can extend this in the same hourly pass without breaking anything.

### Consequences

**Positive:**
- Smaller change, smaller test surface.
- Future addition of other peak channels remains trivial and can be motivated by an actual UI requirement.

**Negative:**
- Slight asymmetry with `OffpeakItem`, which carries all five channels.

---

## Decision 6: Accept storage naming inconsistency between `GridUsageKwh` and `PeakGridImportKwh`

**Date**: 2026-05-30
**Status**: accepted

### Context

The off-peak feature stores grid import as `OffpeakItem.GridUsageKwh` (storage layer) but exposes it as `offpeakGridImportKwh` (API layer). The peak feature stores grid import as `DailyEnergyItem.PeakGridImportKwh` (storage layer) and exposes it as `peakGridImportKwh` (API layer). So at storage, off-peak uses "Usage" and peak uses "Import"; at API, both use "Import".

### Decision

Accept the inconsistency. Keep `OffpeakItem.GridUsageKwh` as-is; introduce `DailyEnergyItem.PeakGridImportKwh`. Document the divergence in implementation notes for future readers.

### Rationale

Renaming `OffpeakItem.GridUsageKwh` is out of scope: it touches the table, the off-peak feature's integration tests, the backfill CLI, the design doc, and the production-deployed JSON marshalling. The naming is internally consistent (off-peak storage is purely "what happened during the off-peak window", hence "usage" rather than "import"). At the API boundary both fields use "import" because the API is the user-facing surface and "import" is the term the iOS app and the OVO bill use.

### Alternatives Considered

- **Rename `OffpeakItem.GridUsageKwh` → `OffpeakItem.GridImportKwh` for consistency.** Rejected: deferred rename, low marginal value, large blast radius (off-peak tests, backfill CLI, design docs). Could be its own future spec if storage-layer naming becomes important.
- **Name the new field `DailyEnergyItem.PeakGridUsageKwh` for parity with off-peak storage.** Rejected: even less consistent at the API layer ("offpeakGridImportKwh" vs "peakGridUsageKwh" is worse than the current divergence at the storage layer only).

### Consequences

**Positive:**
- Smaller blast radius for this spec.

**Negative:**
- Storage layer has two synonyms for "grid import" depending on which feature wrote them. Implementation notes will document.

---

## Decision 7: Backfill the historical window by extending and renaming the off-peak CLI to `cmd/backfill-grid`

**Date**: 2026-05-30
**Status**: accepted

### Context

The hourly summarisation pass only ever processes "yesterday" (Decision 4), so the `PeakComputedAt` sentinel forward-fills peak from deploy day onward but never reaches rows that are already older than yesterday at deploy time. Those are the bulk of the History view — and historical days are exactly the user-visible problem this spec set out to fix. Decision 3 originally assumed the hourly pass would backfill them; it does not.

The repo already has the right pattern for "populate a new readings-derived field on existing rows": `cmd/backfill-offpeak` (modelled on `cmd/backfill-solar`) walks the daily rows over a `today-30d → yesterday` range in a single operator-run invocation, recomputing from `flux-readings` and skipping today. Off-peak deltas and peak grid import are both readings-derived quantities over the same set of days, computed from the same `Pgrid` series.

### Decision

Rename `cmd/backfill-offpeak` → `cmd/backfill-grid` and extend it so that, for each date in the range, in addition to recomputing the off-peak `flux-offpeak` row (unchanged behaviour), it computes `peakGridImportKwh` over the two windows bracketing off-peak and writes it (plus `peakComputedAt`) to the corresponding `flux-daily-energy` row via the same `UpdateDailyEnergyDerived` peak-group write the hourly pass uses.

To avoid creating a phantom `flux-daily-energy` row, the CLI MUST confirm the daily-energy row exists (GET) before writing peak; if it is absent the date is skipped for peak. The off-peak side keeps its existing `WriteOffpeakIfComplete` conditional write. Today is skipped on both sides.

### Rationale

One operator command for both readings-derived grid quantities over the same days is less surface than a second near-identical CLI, and the name `backfill-grid` describes what it now does (both grid-import channels). It matches the established T-1341 backfill pattern, so operators already know the flag shape (`--from`/`--to`/`--dry-run`/table names). The peak write reuses the independent-sentinel write path from Decision 3, so there is no new write semantics.

### Alternatives Considered

- **A separate `cmd/backfill-peak`.** Rejected: duplicates the date-range walk, readings query, Sydney-TZ handling, and flag parsing already in the off-peak CLI for two fields computed from the same readings over the same days. The user directed consolidation.
- **No CLI; accept that only yesterday-onward gets peak** and amend the requirement to drop pre-deploy historical coverage. Rejected: leaves the History view — the feature's whole motivation — on the noisy residual for ~30 days after deploy.
- **Scan the TTL window inside the hourly pass.** Rejected for the reasons in Decision 3's alternatives (per-tick scan, diverges from repo norm).

### Consequences

**Positive:**
- The pre-deploy historical window gets accurate peak in one operator run, matching the off-peak rollout flow.
- No new CLI to learn or maintain; one tool covers both grid-import channels.
- Reuses the Decision 3 write path and the existing readings/date-range machinery.

**Negative:**
- The CLI name no longer mentions off-peak; the rename touches the package, its usage docs, and its tests. (No Makefile/infra target references it — it is a `go run ./cmd/...` tool.)
- The CLI now reads two tables and writes two tables, so its result accounting and summary output grow to cover peak alongside the five off-peak deltas.
- A `flux-daily-energy` row missing at backfill time is skipped for peak (no phantom-row creation); that day stays on the iOS fallback until re-run.

### Impact

`cmd/backfill-offpeak` → `cmd/backfill-grid` (package, docs, tests). Reuses `derivedstats.IntegratePeakGridImportKwh`, `dynamo.UpdateDailyEnergyDerived`, and `dynamo.GetDailyEnergy`. The CLI gains a `--table-daily-energy` flag.

---

## Decision 8: Extend iOS consumption to Day Detail (past days); defer the Dashboard

**Date**: 2026-05-30
**Status**: accepted

### Context

The original iOS scope was History only. Day Detail (the "Grid in (peak)" summary row, its Compare overlay, and the peak-imports cost line) and the Dashboard still compute peak as the `eInput − offpeak` residual — the exact method this spec replaced for History — so they carry the same inaccuracy.

The `/day` response *already* carries `peakGridImportKwh` (the Go `DaySummary` emits it; the Swift `DaySummary` simply did not decode it), so Day Detail for past days needs no backend change — only client wiring.

The Dashboard is different: it always shows today via `/status`, and Decision 4 deliberately excluded a real-time peak path for today. A verification pass on today's `/status` confirmed the residual there is *conditionally* inaccurate: `eInput` is `max(stored, computed)` (`reconcileEnergy`) while off-peak is always a trapezoidal integration, so when the stored AlphaESS counter wins the reconcile the off-peak sampling loss lands on the peak residual (the same pathology as history); when the live-integrated `eInput` wins, both sides use the same method and the error largely cancels. Which side wins varies through the day, so today's Dashboard peak is not reliably accurate, but making it so requires reversing Decision 4 and adding `/status` surface.

### Decision

Extend this spec's iOS consumption to Day Detail for past days: decode `peakGridImportKwh` on the Swift `DaySummary`, and have `SummaryBlock`, `ComparisonSnapshot`, and `DayCosts` prefer the server value with the existing residual fallback. Leave the Dashboard unchanged here; treat real-time today peak as a separate future spec.

### Rationale

Day Detail is a one-field, client-only change reusing data already on the wire and the same `?? residual` pattern already shipped for History — low risk, high consistency. The Dashboard needs a backend design change (real-time integration, a new `/status` field, partial-evening-window semantics) that is out of proportion to a smolspec and reverses an accepted decision, so it belongs in its own spec where those trade-offs can be weighed against the bounded, self-cancelling-in-part error.

### Alternatives Considered

- **Do Day Detail and the Dashboard together now.** Rejected: the Dashboard requires reversing Decision 4 and new API surface — a full design change, not a wiring change; bundling them would blow smolspec scope and muddy a reviewed branch.
- **Day Detail as its own follow-up spec.** Rejected (per user direction): it is the same field and pattern as History, so it sits naturally in this spec; a separate spec would be ceremony for a ~one-field change.
- **Leave Day Detail on the residual.** Rejected: the user explicitly wants Day Detail accurate, and the data is already available.

### Consequences

**Positive:**
- Day Detail's peak row, Compare overlay, and peak cost become accurate for past days with no backend change.
- DayCosts peak pricing stops inheriting the residual error.

**Negative:**
- Peak and off-peak no longer sum exactly to `eInput` on the Day Detail rows (they differ by ~1.5%, the shared sampling artifact); the two rows are each individually more accurate but no longer reconcile to the total. Operator/observer-visible only.
- The Dashboard remains on the residual, so today's peak can still be off until a future spec addresses it — an intentional, documented gap.

### Impact

FluxCore `DaySummary` (`APIModels.swift`), `DayCosts.swift`; app-side `SummaryBlock.swift`, `ComparisonSnapshot.swift`. No backend or `/status` change. `DayEnergy.costs` forwarder must pass the new field into the transient `DaySummary` it builds.

---
