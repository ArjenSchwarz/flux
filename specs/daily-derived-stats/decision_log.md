# Decision Log: Daily Derived Stats

## Decision 1: Feature name `daily-derived-stats`

**Date**: 2026-04-29
**Status**: accepted

### Context

The spec adds three reading-derived per-day stats to `flux-daily-energy` so that multi-day rollup features (starting with T-1022's history-daily-usage card) do not have to re-fetch readings on every Lambda call. Three candidate names were proposed: `daily-derived-stats`, `precomputed-daily-stats`, and `poller-daily-summary`.

### Decision

Use `daily-derived-stats` for the spec folder and feature identifier.

### Rationale

Names the data being added rather than the location (`poller-`) or the technique (`precomputed-`). "Derived" is the load-bearing word — these are stats derived from raw readings, in contrast to the AlphaESS-sourced energy totals already on the row. Future readers asking "what's on this row?" find a clear semantic group.

### Alternatives Considered

- **precomputed-daily-stats**: Names the technique (pre-compute), which is one implementation detail. Future replacement of the technique (e.g. moving to a stream processor) would leave the name confusingly behind.
- **poller-daily-summary**: Names the producer, but the consuming surface (Lambda `/day` and `/history`) carries equal architectural weight. Producer-only naming undersells the cross-cutting nature.

### Consequences

**Positive:**
- Clear conceptual grouping for any future per-day derived stat (the row already has a "derived stats" home).
- Implementation-agnostic.

**Negative:**
- Slightly less googlable than the more literal alternatives.

---

## Decision 2: Hourly summarisation for yesterday only — today stays live

**Date**: 2026-04-29
**Status**: accepted

### Context

Three triggering models were considered: hourly summarisation of today + yesterday in the existing `pollDailyEnergy` loop; hourly summarisation of yesterday only with today computed live by the Lambda; or once-per-day at e.g. 00:30 next day.

### Decision

Run the summarisation pass hourly against yesterday only. Today's derivedStats are computed live by `/day` and `/history` on demand, the same way `/day` already does for today.

### Rationale

Persisting today's derivedStats creates a moving-target row whose freshness is bounded by the poller cadence. The Lambda already has live-compute paths for today's reconciled energy (the `reconcileEnergy` call in `/history` and `/day`), and that path naturally carries the cost of one readings query for today's range — adding one `findDailyUsage` / `findMinSOC` / `findPeakPeriods` invocation against the same readings is essentially free. Persisting today would also break the "past dates immutable" invariant that simplifies cache reasoning on the iOS side.

The once-per-day cron alternative needs new scheduling infrastructure and risks being skipped on a container restart inside the firing window. Running hourly against yesterday is well-tolerated by `findDailyUsage` (idempotent, deterministic) and by the existing poller loop (it already polls `pollDailyEnergy` on a similar cadence).

### Alternatives Considered

- **Hourly for today + yesterday**: Persists today, freshens up to once an hour. Adds the moving-target problem and breaks the past-dates-immutable invariant for marginal benefit (today is already live elsewhere).
- **Once at end-of-day cron**: Cleanest write story but needs new scheduling and is fragile to container restarts.

### Consequences

**Positive:**
- Past dates are immutable on disk once summarised — simplifies caching, simplifies reasoning about drift between `/day` and `/history`.
- Today is always fresh (live compute), matching the dashboard's existing freshness contract.
- Yesterday is summarised within at most one hour after midnight — well inside the 30-day TTL window.

**Negative:**
- `/history` for the today bar pays for one readings query (already issued today for energy reconciliation, so the marginal cost is the integration pass itself).
- The first hour after midnight, yesterday's summarised row may be missing if the container restarted across the boundary. Acceptable: the next hourly tick fills it in.

---

## Decision 3: Switch `flux-daily-energy` writes to UpdateItem with field-level SET

**Date**: 2026-04-29
**Status**: accepted

### Context

The existing hourly AlphaESS poll writes the full `flux-daily-energy` row via `PutItem`, so adding a second writer (the summarisation pass) would clobber whichever set of fields the second writer doesn't carry. Three remediation strategies were considered: switch both writers to `UpdateItem` with field-level `SET` expressions; have each writer do a read-merge-Put; or have each writer carry all fields it knows about (zeroing the rest).

### Decision

Switch both writers to `UpdateItem` with field-level `SET` expressions. The energy poll updates only the energy fields; the summarisation pass updates only the three derived fields. The two writes never touch the same attributes.

### Rationale

`UpdateItem` is the standard DynamoDB primitive for partial-row writes and is exactly the operation we need. It eliminates lost-update races between the two writers without requiring read-before-write coordination. The IAM cost is one additional `dynamodb:UpdateItem` permission on the table, which is well within the existing scope of the poller's IAM role.

Read-merge-Put was rejected because it costs an extra read per write and introduces a race window even for a single writer (between the read and the write). Two-PutItems-with-zeros was rejected because it requires each writer to know the schema of every other writer — the AlphaESS poll would need to opt into not clobbering the derived fields, and any future field would need to be added to every writer.

### Alternatives Considered

- **Read-merge-Put**: Doubles the per-write I/O and races on concurrent writers (acceptable in the current single-writer-per-system model, but fragile).
- **Two PutItems carrying all known fields**: Each writer must know the schema of every other writer; future fields require touching every writer.

### Consequences

**Positive:**
- Each writer concerns itself with only its own fields; future fields are additive without touching other writers.
- No race between the two writers.

**Negative:**
- Adds `dynamodb:UpdateItem` to the poller's IAM role.
- Requires touching the existing `WriteDailyEnergy` call path (one DynamoDB store method change, plus tests).

---

## Decision 4: Extend the existing `flux-daily-energy` row with native Map / List attributes

**Date**: 2026-04-29
**Status**: accepted

### Context

A separate `flux-daily-blocks` (or `flux-daily-stats`) table was considered as a place for the derived stats. The existing `flux-daily-energy` row already keys on `(sysSn, date)`, the same key a derived-stats row would use, and the row is read by every `/day` and `/history` call.

### Decision

Extend the existing `flux-daily-energy` row with three new optional attributes carrying the derived stats payloads, stored as native DynamoDB Map / List attributes (via `attributevalue.MarshalMap`) rather than as JSON-encoded strings.

### Rationale

A separate table buys nothing: same key, 1:1 cardinality with the existing row, and would force every `/day` and `/history` call to do an additional Query (or BatchGet) just to assemble the response that one row could carry. Schema extension is a non-event in DynamoDB — new attributes are additive and pre-existing rows deserialise with the new fields nil.

Native Map / List storage keeps the data inspectable in the AWS console (a maintenance lifeline for a single-developer project) and avoids baking a serialisation choice into the storage layer. The `attributevalue` marshaller already in use for the existing fields handles the new ones identically. JSON-string storage was considered and rejected: it makes the data opaque to the console, prevents future per-attribute filter expressions, and adds a serialisation round-trip the row doesn't need.

### Alternatives Considered

- **`flux-daily-blocks` as a separate table**: Doubles the read I/O on every Lambda call. Doubles the IAM surface. Adds a new table to operate. No upside given 1:1 cardinality with the existing row.
- **JSON-encoded string attributes**: Smaller wire size by ~10% and slightly simpler marshalling, but opaque in the AWS console, blocks future filter expressions, and bakes a JSON-specific serialisation into the storage layer.

### Consequences

**Positive:**
- Single row per (sysSn, date) carries the full daily picture.
- No extra table to provision, monitor, or back up.
- One DynamoDB read per day in `/history`; no change to the read shape.

**Negative:**
- Row is wider; payload per item grows by ~2 KB. Still well below the 400 KB DynamoDB item limit and below the 4 KB read-unit boundary on a typical day.

---

## Decision 5: Field-equivalent (not byte-equivalent) cross-handler contract

**Date**: 2026-04-29
**Status**: accepted

### Context

The first draft of the T-1022 requirements asserted byte-equivalence between `/day` and `/history` payloads for the same date. The peer review (Gemini + Kiro) flagged that float serialisation, JSON field ordering, and whitespace can legitimately differ between two correct payloads, and that a single shared computation function is a better drift-prevention mechanism than a serialisation invariant.

### Decision

The contract between `/day` and `/history` is field-equivalence on the derivedStats payload (same set of fields, same values), enforced by both handlers reading from the same shared computation entry point (`findDailyUsage`, `findMinSOC`, `findPeakPeriods`). No byte-level invariant is asserted.

### Rationale

Field-equivalence is what the iOS client actually needs. Byte-equivalence over-specifies the contract and would lock the project out of `/history`-only payload-size optimisations (e.g. omitting `boundarySource` on past dates if it ever becomes worth doing). The shared compute function provides the structural guarantee that drift cannot be introduced silently.

### Alternatives Considered

- **Byte-equivalence**: Brittle; over-specified; locks out future per-handler optimisations.
- **Shared compute function only, no equivalence test**: Loses the structural assertion that the contract holds; relies on reviewers to catch drift.

### Consequences

**Positive:**
- Contract is what consumers actually need.
- Per-handler payload optimisations remain possible.
- Single shared compute function is asserted by AC and verified by a cross-handler equivalence test.

**Negative:**
- Slightly more nuanced test than a byte-diff (must walk the field tree).

---

## Decision 6: Extract shared compute helpers into a `derivedStats` package

**Date**: 2026-04-29
**Status**: accepted

### Context

`findDailyUsage`, `findMinSOC`, and `findPeakPeriods` currently live in `internal/api/compute.go`. AC [1.9](#1.9) requires a single shared computation entry point used by both the Lambda's live path and the poller's summarisation pass. Letting `internal/poller` import `internal/api` would drag the `aws-lambda-go` runtime, the request/response types, and the rest of the Lambda handler surface into the ECS poller binary, inverting the natural dependency direction.

### Decision

Extract the three helpers (and any shared types they need — `DailyUsage`, `DailyUsageBlock`, `PeakPeriod`, the `melbourneSunriseSunset` table) into a new `internal/derivedstats` package. Both `internal/poller` and `internal/api` import it; neither depends on the other for the helpers.

### Rationale

A shared package is the standard Go layering for code that two top-level packages need. The helpers are already pure functions of readings + SSM strings + clock — there is no Lambda runtime coupling to unwind. The extraction is a small refactor (move the functions, update imports, run `go build`) that pays for itself the first time a third caller wants the same helpers.

The alternative — letting the poller import `internal/api` — adds Lambda dependencies to an ECS binary that has no reason to know about HTTP request/response types, and makes the poller's build time and binary size larger than they need to be. The reverse alternative — letting `internal/api` import `internal/poller` — is a layering violation (the API should not know about the poller) and would force the helpers' tests to pull in poller dependencies.

### Alternatives Considered

- **Poller imports `internal/api`**: Drags `aws-lambda-go` and the Lambda handler types into the ECS binary. Inverts the natural dependency direction.
- **Duplicate the helpers**: Most direct violation of [1.9](#1.9). Drift between the two copies is the exact risk this spec exists to prevent.
- **Move helpers to `internal/api/compute` as a sub-package the poller imports**: Less disruptive but still couples the ECS binary to the API package's namespace, and the namespace is misleading (these helpers compute whether or not an API is in scope).

### Consequences

**Positive:**
- Clean layering: `internal/api` and `internal/poller` are siblings, neither depends on the other.
- Future reusers of the helpers (a CLI summary tool, a backfill script, a different surface) import `internal/derivedstats` directly without coupling to either existing package.
- Makes [1.9](#1.9)'s single-implementation invariant trivially provable by file location.

**Negative:**
- One-time refactor cost (move three functions plus the sunrise/sunset table and any private helpers they depend on).
- Adds a new package to the dependency graph; one more place to look for these helpers (mitigated by an unambiguous name).

---

## Decision 7: SSM-unresolved → skip the write entirely (preserve immutability)

**Date**: 2026-04-29
**Status**: accepted

### Context

The first draft of AC 1.6 said the summarisation pass would write a two-block `dailyUsage` payload (per the `peak-usage-stats` AC 1.11 fallback) when SSM off-peak parameters were unresolved. The design-critic review surfaced a conflict: the poller's SSM cache could be stale on an early hourly tick, persisting a two-block payload to disk; a later tick after SSM resolution would then overwrite the row with a five-block payload. This breaks Decision 2's "past dates immutable" invariant, which the iOS cache layer and the cross-handler equivalence contract both rely on.

### Decision

When the off-peak SSM parameters are unresolved at pass time, skip the write entirely (no `UpdateItem` call) and log at warn level. The next hourly tick retries; the [1.10](#1.10) precheck means a successful retry costs one `GetItem` and one `UpdateItem`.

### Rationale

The SSM resolution problem is a transient configuration issue, not a permanent data condition. Writing a degraded payload that a later tick has to correct breaks the immutability invariant for marginal benefit (the row eventually gets the correct payload anyway). Skipping the write keeps the row's state monotonic: once derivedStats are present, they stay present and stay correct.

The cost is bounded by how long SSM stays unresolved. In practice this is "until the next configuration push" or "until the SSM cache TTL refreshes" — not days. The iOS app handling for missing derivedStats already exists (per AC [3.3](#3.3) and [4.4](#4.4)), so the user-visible impact during the transient window is the same as a pass that hasn't run yet.

### Alternatives Considered

- **Write the two-block fallback**: Breaks the immutability invariant; later ticks would mutate the row. The iOS cache and cross-handler contract both leak through this seam.
- **Write the two-block fallback with a flag noting "ssm-degraded"**: Adds a state machine to the row (degraded → upgraded → final) that nothing in the read path has any reason to handle. Optimises for a transient case at the cost of permanent complexity.
- **Block the pass on SSM resolution (sleep + retry inside the tick)**: Conflates the pass loop with SSM client behaviour. The hourly retry loop already handles this naturally without introducing per-tick blocking.

### Consequences

**Positive:**
- Past-dates-immutable invariant holds in all paths.
- Read-side handlers need no awareness of "degraded" derivedStats; they either exist correctly or are absent.
- Operator alarm path is uniform: a row missing derivedStats N hours after its date completed indicates a real problem (SSM down, readings missing, code bug) regardless of which.

**Negative:**
- A user opening the app during a long SSM outage will see no derivedStats for the affected dates (matching the AC [3.3](#3.3) / [4.4](#4.4) absent-section behaviour).
- The `SummarisationPassResult` metric needs a `skipped-ssm-unresolved` dimension so the operator can distinguish this from other skip reasons.
- In the current poller architecture, `cfg.OffpeakStart` / `cfg.OffpeakEnd` are validated by `Load()` at startup and cannot become invalid at runtime, so the `skipped-ssm-unresolved` path is defensive and should never fire in production. The metric dimension is kept as a "should never happen, alarm if it does" signal rather than an expected state. (If a future change introduces per-tick SSM resolution, the path becomes load-bearing without further design changes.)

---

## Decision 8: `derivedStatsComputedAt` sentinel attribute for the precheck

**Date**: 2026-04-29
**Status**: accepted

### Context

The first design draft used a "are all three derived attributes present and non-empty?" precheck for AC [1.10](#1.10). The design-critic and explain-like reviews surfaced two cases this breaks:

1. `findDailyUsage` legitimately returns `nil` when no blocks survive the pipeline (rare but real: a date with completely empty / pathological readings). The "non-empty" check would reject `nil`, causing the pass to re-compute the same `nil` every hour forever.
2. `findPeakPeriods` legitimately returns an empty slice on cloudy days with no high-consumption excursions. DynamoDB's `attributevalue` marshalling means a present-but-empty list and an absent attribute cannot be distinguished by attribute-shape inspection in all cases, leaving the precheck unable to tell "computed and empty" from "never written".

A precheck that occasionally re-computes the same result is harmless from a correctness standpoint, but it defeats the purpose of [1.10](#1.10) (which is to make the hourly tick cost O(1) `GetItem` in steady state).

### Decision

Add a `derivedStatsComputedAt` attribute (RFC3339 UTC timestamp string) to the `flux-daily-energy` row. The summarisation pass writes it inside the same `UpdateItem` SET expression as the three derived attributes; the precheck reads only this sentinel — its presence means "summarised, do not re-run", regardless of the shape of the three derived attributes.

### Rationale

A single sentinel decouples "have we computed?" from "what did we compute?" — the natural separation of concerns. Cost is one extra small attribute (~30 bytes for an RFC3339 string) on each row. The timestamp itself is incidentally useful for operator forensics ("when was this row's derivedStats last computed?") at zero additional cost.

### Alternatives Considered

- **AC 1.10 carve-out for empty `peakPeriods` only**: Would fix one of the two cases (cloudy day) but not the other (no surviving blocks). Half a fix.
- **Add a "had-readings" boolean to `dailyUsage`**: Pollutes the compute type for storage concerns; bleeds storage semantics into `derivedstats`.
- **Re-compute every hour and accept the work**: Defeats the precheck's purpose; the [1.8](#1.8) determinism invariant exists specifically to support [1.10](#1.10).

### Consequences

**Positive:**
- Precheck is unambiguous and works for all `findDailyUsage` / `findPeakPeriods` outputs.
- Sentinel doubles as a "last computed at" forensic field for operators.
- Storage cost: one extra ~30 byte attribute per row.

**Negative:**
- One more attribute on the row; one more line in the SET expression.

---

## Decision 9: `derivedstats` defines its own `Reading` type — no upward import to `dynamo`

**Date**: 2026-04-29
**Status**: accepted

### Context

The first design draft had `internal/derivedstats` importing `dynamo.ReadingItem` ("the one acceptable upward dependency") and `internal/dynamo` importing `derivedstats` for the `*Attr ↔ derivedstats.*` conversion functions. The design-critic flagged this as a Go import cycle that the compiler would reject.

### Decision

`internal/derivedstats` defines its own `Reading` struct mirroring the fields its helpers consume (`Timestamp`, `Ppv`, `Pload`, `Soc`, `Pbat`, `Pgrid` — the subset of `dynamo.ReadingItem` actually read). Call sites in `internal/poller` and `internal/api` perform a one-line slice conversion from `[]dynamo.ReadingItem` to `[]derivedstats.Reading` before invoking the helpers. The conversion functions `*Attr ↔ derivedstats.*` live in `internal/dynamo` (the side that owns the storage shape).

### Rationale

Breaks the cycle in the simplest possible way. The conversion is mechanical, ~10 lines per call site, three call sites total (`day.go`, `history.go`, `dailysummary.go`). `derivedstats` becomes a true leaf package with zero internal dependencies on other Flux packages — the cleanest possible layering for the spec's "single shared compute package" goal.

The bundle struct passed to `dynamo.UpdateDailyEnergyDerived` (originally proposed as `poller.DerivedStats`) similarly moves to `internal/dynamo` as `dynamo.DerivedStats`, since it is a storage-write argument, not a poller-only concept.

### Alternatives Considered

- **Lift `Reading` to a third neutral package** (e.g. `internal/readings`): Adds a package for one type. Pure overhead.
- **Keep `derivedstats → dynamo` and put the conversion functions in a third package** (e.g. `internal/derivedstore`): Adds a fourth package whose only job is glue between two adjacent ones.
- **Move the `*Attr` types into `derivedstats`**: Makes the storage shape part of the compute package, conflating two concerns and contaminating `derivedstats` with DynamoDB struct tags.

### Consequences

**Positive:**
- Layering is provable by `go list -deps`: `derivedstats` has no Flux dependencies; `dynamo` depends on `derivedstats`; `poller` and `api` depend on both.
- Mechanical conversion at the three call sites is local and easy to audit.

**Negative:**
- Three near-identical 10-line conversions (one per call site) add ~30 lines of glue.
- `derivedstats.Reading` is a "shadow" of `dynamo.ReadingItem`; if a future helper needs another field, both types must be kept in sync. Mitigated by a comment on each that names the other.

---


