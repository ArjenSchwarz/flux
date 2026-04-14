# Decision Log: Flux Poller

## Decision 1: Error Handling Strategy

**Date**: 2026-04-13
**Status**: accepted

### Context

The poller makes frequent calls to the AlphaESS API (every 10 seconds for live data). Failures can occur due to rate limits, server errors, network issues, or API timeouts. We needed to decide how aggressively the poller should retry failed requests.

### Decision

Use a log-and-skip strategy: when an API call fails, log the error and wait for the next scheduled poll. No retries, no circuit breaker.

### Rationale

The 10-second polling interval for the primary endpoint means a missed reading is quickly superseded by the next attempt. The cost of a missed data point is low (a 10-second gap in readings), while retry logic adds complexity and risks compounding failures (e.g., retrying into a rate limit). The less frequent endpoints (hourly, 6-hourly, 24-hourly) have longer gaps between attempts but their data is also less time-sensitive.

### Alternatives Considered

- **Retry with exponential backoff**: Would recover from transient failures faster — rejected because the natural polling interval already provides implicit retries for the most frequent endpoint, and adds complexity for marginal benefit.
- **Retry + circuit breaker**: Full resilience pattern — rejected as over-engineering for a single-user system with frequent polling.

### Consequences

**Positive:**
- Simple implementation with no retry state management
- No risk of cascading retries amplifying API pressure
- The 10-second poll cycle naturally retries the most critical endpoint

**Negative:**
- A sustained outage will result in gaps in data until the API recovers
- Hourly/daily endpoints may miss their window and not retry for another full interval

---

## Decision 2: Observability Approach

**Date**: 2026-04-13
**Status**: accepted

### Context

The poller runs as a single Fargate task and needs some form of observability for debugging and operational awareness. Options range from plain logs to custom CloudWatch metrics to a Prometheus metrics endpoint.

### Decision

Use structured JSON logs to stdout only. The ECS health check uses a `healthcheck` subcommand that queries DynamoDB for recent data. No custom CloudWatch metrics or Prometheus endpoint.

### Rationale

CloudWatch Logs Insights can query JSON-structured logs for operational needs. Custom metrics add cost and complexity (CloudWatch custom metrics are $0.30/metric/month). A Prometheus endpoint would require opening an inbound port on the security group, which contradicts the zero-inbound-surface design. For a two-user personal app, structured logs provide sufficient observability.

### Alternatives Considered

- **Structured logs + CloudWatch custom metrics**: Better dashboarding — rejected because the per-metric cost isn't justified for a personal app, and Logs Insights covers most queries.
- **Structured logs + Prometheus /metrics endpoint**: Standard observability — rejected because it requires an inbound port, a Prometheus server, and is overkill for this use case.

### Consequences

**Positive:**
- No additional infrastructure or cost
- Zero inbound attack surface maintained
- CloudWatch Logs Insights provides ad-hoc querying

**Negative:**
- No pre-built dashboards or alerting without writing Logs Insights queries
- No real-time metrics (must query logs)

---

## Decision 3: Off-Peak SOC Tracking

**Date**: 2026-04-13
**Status**: accepted

### Context

The off-peak record needs a `batteryDeltaPercent` field showing how much battery percentage was gained during the off-peak window. This could be computed from energy fields (eCharge/eDischarge relative to battery capacity) or by capturing actual SOC readings at the window boundaries.

### Decision

Capture SOC snapshots at the off-peak window start and end by calling `getLastPowerData` alongside `getOneDateEnergyBySn`. Store `socStart` and `socEnd` in the off-peak record. Compute `batteryDeltaPercent` as the direct difference.

### Rationale

Direct SOC measurement is more accurate than deriving from energy fields, because energy-to-SOC conversion depends on battery efficiency, temperature, and degradation. An extra `getLastPowerData` call at window boundaries is negligible cost (2 extra calls per day).

### Alternatives Considered

- **Compute from energy fields**: Calculate from eCharge/eDischarge delta relative to cobat capacity — rejected because conversion losses and non-linear battery charging curves make the result approximate.

### Consequences

**Positive:**
- Accurate battery percentage delta based on direct measurement
- Simple calculation (subtraction of two SOC values)

**Negative:**
- Two extra API calls per day (at off-peak start and end) — negligible

---

## Decision 4: pgridSustained Computation

**Date**: 2026-04-13
**Status**: accepted

### Context

The dashboard shows a red grid indicator when grid import is >500W and sustained over multiple consecutive readings. Someone needs to compute the `pgridSustained` flag.

### Decision

The Lambda API computes `pgridSustained` by querying recent readings from `flux-readings`. The poller writes raw readings only.

### Rationale

Keeping the poller as a simple write-through pipeline reduces its complexity. The Lambda already reads from `flux-readings` for the `/status` endpoint, so checking the last N readings for sustained grid import is a natural addition there. This also means the sustained threshold can be adjusted without redeploying the container.

### Alternatives Considered

- **Poller computes and writes the flag**: Would save the Lambda a query — rejected because it adds stateful logic to the poller and couples it to a display concern.

### Consequences

**Positive:**
- Poller remains stateless (no tracking of consecutive readings)
- Threshold logic can be tuned in the Lambda without container redeployment

**Negative:**
- Lambda must query multiple recent readings to compute the flag

---

## Decision 5: Spec Scope — Go Code, Dockerfile, and CI

**Date**: 2026-04-13
**Status**: accepted

### Context

The poller needs Go application code, a Dockerfile for containerisation, and a CI pipeline to build and push images. These could be separate specs or combined.

### Decision

Include the Go application, Dockerfile, and GitHub Actions CI workflow in a single spec. Also include the minor CloudFormation template update to pass DynamoDB table names and timezone to the container.

### Rationale

These components are tightly coupled — the Dockerfile builds the Go binary, CI builds and pushes the Docker image, and the template update is a prerequisite for the Go code to find its tables. Splitting them across specs would create artificial boundaries.

### Alternatives Considered

- **Go code only, separate Dockerfile/CI spec**: Cleaner separation — rejected because the Dockerfile and CI are small and directly dependent on the Go code structure.

### Consequences

**Positive:**
- Single spec covers the complete path from code to running container
- No cross-spec dependencies to manage

**Negative:**
- Slightly larger spec scope than pure application code

---

## Decision 6: Timezone Handling

**Date**: 2026-04-13
**Status**: accepted

### Context

The off-peak window (11:00 AM – 2:00 PM) needs to be evaluated against a clock that matches the user's local time, including DST changes. Options were to use a fixed timezone or UTC.

### Decision

Evaluate the off-peak window in a fixed configured timezone (default: Australia/Sydney). Go's `time` package handles DST transitions automatically when using `time.LoadLocation`.

### Rationale

The off-peak window corresponds to the electricity tariff schedule, which follows local time. Using UTC would cause the window to shift by an hour during DST transitions, capturing the wrong time period. Go's timezone handling is robust and well-tested.

### Alternatives Considered

- **UTC only**: Simpler configuration — rejected because the off-peak window would drift relative to the actual tariff window during DST changes, defeating its purpose.

### Consequences

**Positive:**
- Off-peak window always aligns with local electricity tariff times
- DST transitions handled automatically by Go's time package

**Negative:**
- Requires timezone data in the container image (included in distroless images, or via tzdata)

---

## Decision 7: Off-Peak Snapshot Failure Recovery

**Date**: 2026-04-13
**Status**: accepted

### Context

The general error handling strategy (Decision 1) uses log-and-skip, which works well for the 10-second live data poll. However, off-peak snapshots get exactly 2 chances per day (window start and end). A failed snapshot means the entire day's off-peak data is lost.

### Decision

Retry off-peak snapshot API calls up to 3 times with 10-second intervals. If all retries fail, skip the off-peak record for that day. If the start snapshot succeeds but the end snapshot fails, do not write a partial record.

### Rationale

Off-peak snapshots are high-value, low-frequency operations. Three retries over 30 seconds is a modest investment for data that cannot be recaptured. Partial records (start without end) would produce meaningless deltas, so skipping is cleaner than writing incomplete data.

### Alternatives Considered

- **Log and skip (same as regular polls)**: Simplest — rejected because the cost of a missed off-peak record is much higher than a missed live reading.
- **Write partial records**: Would preserve available data — rejected because off-peak deltas require both snapshots to be meaningful.

### Consequences

**Positive:**
- Recovers from transient API failures during the off-peak window
- No incomplete off-peak records in DynamoDB

**Negative:**
- A sustained API outage during the off-peak window still loses the day's data
- Slightly more complex than log-and-skip for these two operations

---

## Decision 8: Independent Polling Schedules (No Request Serialization)

**Date**: 2026-04-13
**Status**: accepted

### Context

The AlphaESS API recommends a minimum 10-second interval between polls. An earlier proposal serialized all API requests through a single priority queue. On review, the non-live endpoints (hourly, 6-hourly, 24-hourly) fire so infrequently that collisions are rare and harmless.

### Decision

Run each polling schedule independently on its own goroutine and timer. No request queue or global serialization. The 10-second live poll naturally respects the minimum interval, and the infrequent endpoints are unlikely to collide with it in practice.

### Rationale

The serialization approach added complexity (priority queue, stagger logic) for a problem that barely exists. The hourly poll fires once per 360 live polls. The chance of two non-live polls coinciding is negligible. If a collision does occur and triggers a rate limit, the log-and-skip strategy handles it gracefully.

### Alternatives Considered

- **Global request queue with priority**: Guaranteed rate limit compliance — rejected as over-engineering for the actual collision frequency.

### Consequences

**Positive:**
- Simpler implementation with no coordination between goroutines
- Each schedule runs at its natural pace without delay from other schedules

**Negative:**
- Rare collisions could trigger a rate-limit response, but log-and-skip handles this

---

## Decision 9: Previous Day Energy Finalization

**Date**: 2026-04-13
**Status**: accepted

### Context

The 6-hourly energy poll queries "today's" date. The last poll before midnight might occur around 18:00 or 23:00, missing the final evening energy values. The daily energy record for that day would be incomplete.

### Decision

Call `getOneDateEnergyBySn` for the previous day's date once after local midnight (within the first hour) to capture final daily energy totals.

### Rationale

Energy values are cumulative throughout the day. The final values at midnight represent the true daily totals. Without this, the last snapshot (potentially hours before midnight) would be treated as the daily total, understating evening consumption and generation.

### Alternatives Considered

- **No previous-day query**: Simpler — rejected because the last 6-hourly poll could miss up to 6 hours of energy data.

### Consequences

**Positive:**
- Daily energy records reflect true end-of-day totals
- One extra API call per day (negligible)

**Negative:**
- Adds a time-triggered event (shortly after midnight) to the polling logic

---

## Decision 10: Daily Power TTL Extended to 30 Days

**Date**: 2026-04-13
**Status**: accepted

### Context

The V1 spec originally specified a 7-day TTL on `flux-daily-power` items, reasoning that this data is "only needed for recent 24h low and off-peak calculations." However, `flux-daily-power` also serves as a lower-resolution fallback for the day detail SOC chart. With a 7-day TTL, any day older than a week would lose its SOC chart data entirely, even though `flux-readings` keeps 10-second data for 30 days.

### Decision

Extend the `flux-daily-power` TTL to 30 days, matching the `flux-readings` TTL.

### Rationale

The daily-power records are small (5-minute cbat snapshots, ~288 items per day). The cost of keeping 30 days vs 7 is negligible on DynamoDB on-demand pricing. Aligning TTLs simplifies the mental model: both time-series tables retain 30 days, daily-energy retains forever.

### Alternatives Considered

- **Keep 7-day TTL**: Lower storage — rejected because it creates a confusing gap where day detail charts show SOC from readings (30 days) but lose the daily-power fallback after 7 days.
- **No TTL**: Keep forever — rejected because the data grows without bound and is superseded by daily energy summaries for historical purposes.

### Consequences

**Positive:**
- Day detail SOC chart has fallback data for the full 30-day window
- Consistent TTL across both time-series tables

**Negative:**
- ~4x more daily-power items stored (negligible cost)

---

## Decision 11: Package Structure — internal/ Packages

**Date**: 2026-04-13
**Status**: accepted

### Context

The flux repository will contain two binaries: `cmd/poller` (this spec) and `cmd/api` (Lambda, future spec). Both need access to shared DynamoDB models and potentially the store layer. The code needs to be organized so both binaries can share code.

### Decision

Use `internal/` packages: `internal/alphaess/`, `internal/dynamo/`, `internal/poller/`, `internal/config/`. This is the standard Go convention for multi-binary repositories.

### Rationale

`internal/` prevents external consumers from importing the packages (enforced by the Go compiler), which is appropriate for a service that isn't a library. Both `cmd/poller` and `cmd/api` can import from `internal/` freely. This matches the Go project layout convention for applications.

### Alternatives Considered

- **Top-level packages** (like strata's `lib/`): Simpler import paths — rejected because the code is not intended to be imported externally, and `internal/` makes that intent explicit.

### Consequences

**Positive:**
- Clear separation between importable and non-importable code
- Both binaries share models and store layer
- Standard Go convention, immediately understood by any Go developer

**Negative:**
- Slightly deeper import paths (`internal/dynamo/` vs `dynamo/`)

---

## Decision 12: Minimal CLI — os.Args over Cobra

**Date**: 2026-04-13
**Status**: accepted

### Context

The poller binary needs exactly two modes: the main polling loop (default) and a `healthcheck` subcommand. Strata uses Cobra for CLI handling, but that project has multiple commands and flags.

### Decision

Use a simple `os.Args` check for the `healthcheck` subcommand. No CLI framework.

### Rationale

Cobra is valuable for complex CLIs with many commands, flags, and help text. The poller has one subcommand and one flag (`--dry-run`, which can also be an env var). Adding Cobra would introduce a dependency, an `init()` function, and command registration boilerplate for a problem solved by three lines of code.

### Alternatives Considered

- **Cobra**: Consistent with strata — rejected because the complexity isn't justified for a two-mode binary.

### Consequences

**Positive:**
- Zero CLI dependencies
- Fast healthcheck path (no framework initialization)

**Negative:**
- No auto-generated help text (not needed for a container binary)

---

## Decision 13: Store Interface for Dry-Run Mode

**Date**: 2026-04-13
**Status**: accepted

### Context

Dry-run mode needs to log what would be written to DynamoDB instead of writing. This could be implemented with conditional checks in each write path, or with an interface.

### Decision

Define a `Store` interface with two implementations: `DynamoStore` (production) and `LogStore` (dry-run). The store is selected at startup based on the `DRY_RUN` configuration.

### Rationale

An interface keeps the polling logic clean — it calls `store.WriteReading()` without caring about the mode. The conditional logic exists only once (in `main.go` where the store is created). This also makes unit testing easier: tests can use `LogStore` or a mock without needing DynamoDB.

### Alternatives Considered

- **Conditional checks in each write**: Simpler — rejected because it scatters dry-run logic across every write call site and makes the code harder to follow.

### Consequences

**Positive:**
- Clean separation between write logic and write destination
- Polling code is identical in production and dry-run mode
- Easy to add other store implementations (e.g., DynamoDB Local for testing)

**Negative:**
- One more interface to maintain (6 methods)

---

## Decision 14: Distroless Base Image

**Date**: 2026-04-13
**Status**: accepted

### Context

The Dockerfile needs a minimal runtime stage. Options are `scratch` (empty), `gcr.io/distroless/static` (Google's minimal image), or Alpine.

### Decision

Use `gcr.io/distroless/static:nonroot` as the runtime base image.

### Rationale

Distroless static provides CA certificates and IANA timezone data out of the box, both of which the poller needs (HTTPS to AlphaESS API and `time.LoadLocation`). The `:nonroot` tag defaults to UID 65534, following the principle of least privilege. At ~2MB it's only marginally larger than scratch but eliminates manual file copying. Alpine would work but includes a shell and package manager that aren't needed.

### Alternatives Considered

- **scratch**: Smallest possible — rejected because it requires manually copying CA certs and timezone data into the image.
- **Alpine**: Battle-tested — rejected because it includes a shell and package manager that increase attack surface without benefit.

### Consequences

**Positive:**
- CA certificates and timezone data included automatically
- Non-root by default
- No shell or package manager (minimal attack surface)

**Negative:**
- Depends on Google's distroless image registry (well-maintained, widely used)

---

## Decision 15: Two-Context Graceful Shutdown Pattern

**Date**: 2026-04-13
**Status**: accepted

### Context

The poller needs to stop scheduling new polls on SIGTERM but allow in-flight API calls and DynamoDB writes to complete. Using a single context for both loop control and in-flight operations would cancel in-flight requests immediately when SIGTERM arrives.

### Decision

Use two contexts: a `loopCtx` (cancelled by SIGTERM) for goroutine ticker loops, and a `drainCtx` (cancelled after a 25-second timeout) for in-flight API/DynamoDB calls. After `loopCtx` is cancelled, goroutines stop scheduling but any in-flight call runs to completion under `drainCtx`.

### Rationale

A single context means SIGTERM immediately cancels in-flight DynamoDB writes, which may have already reached the server. The two-context pattern ensures in-flight work completes cleanly while still enforcing a hard shutdown deadline.

### Alternatives Considered

- **Single context**: Simpler — rejected because it cancels in-flight writes immediately, violating requirement [8.2].
- **Separate shutdown signal channel**: Custom signalling — rejected because Go's context pattern is standard and well-understood.

### Consequences

**Positive:**
- In-flight writes complete cleanly
- Hard 25-second deadline prevents runaway shutdown

**Negative:**
- Two context parameters on goroutine functions (minor complexity)

---

## Decision 16: Off-Peak Record Status Field

**Date**: 2026-04-13
**Status**: accepted

### Context

The off-peak start snapshot is written to DynamoDB as a partial record so it survives container restarts. But requirement [6.13] says no off-peak record should exist if the end snapshot fails. A partial record with zero-valued end fields is indistinguishable from a complete record with genuine zero deltas.

### Decision

Add a `status` field to `OffpeakItem` with values `"pending"` (start captured, waiting for end) and `"complete"` (both snapshots captured, deltas computed). If the end snapshot fails, the pending record is deleted. The Lambda API filters on `status == "complete"`.

### Rationale

A status field is the simplest way to distinguish partial from complete records. Deleting the pending record on end failure satisfies [6.13]. The Lambda filtering provides defence in depth.

### Alternatives Considered

- **Separate recovery key**: Use a different sort key pattern (e.g., `recovery#2026-04-13`) for the start checkpoint — rejected because it adds key management complexity without clear benefit.
- **In-memory only**: Don't persist start snapshot — rejected because a container restart mid-window loses the data.

### Consequences

**Positive:**
- Unambiguous record state (pending vs complete)
- Satisfies [6.13] — no partial records visible to consumers
- Crash recovery works via DynamoDB query for pending records

**Negative:**
- One extra DynamoDB field and a delete call on failure path

---
