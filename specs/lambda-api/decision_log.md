# Decision Log: Lambda API

## Decision 1: SSM Token Caching Strategy

**Date**: 2026-04-14
**Status**: accepted

### Context

The Lambda validates app requests via a bearer token stored in SSM Parameter Store. SSM calls add latency (~50ms) and count against API rate limits. The Lambda may handle bursts of requests from the app's 10-second auto-refresh.

### Decision

Cache the SSM token value on cold start and reuse it for the lifetime of the warm Lambda instance.

### Rationale

For a two-user personal app, token rotation is infrequent. Caching on cold start eliminates per-request SSM latency and API calls. A token change takes effect on the next cold start or redeployment, which is acceptable.

### Alternatives Considered

- **Fetch every request**: Always-fresh validation — Rejected because it adds ~50ms latency per request and unnecessary SSM API calls for a rarely-changing token
- **Cache with TTL**: Balance between freshness and performance — Rejected because the added complexity is not justified for a two-user app where token rotation is rare

### Consequences

**Positive:**
- Zero SSM latency after cold start
- No SSM rate limit concerns

**Negative:**
- Token changes require cold start or redeployment to take effect

---

## Decision 2: Computation Location for Rolling Averages

**Date**: 2026-04-14
**Status**: accepted

### Context

The `/status` endpoint returns rolling 15-minute averages (load, battery) and cutoff estimates derived from recent readings. These could be computed by the poller (pre-stored) or by the Lambda (on-the-fly).

### Decision

The Lambda computes rolling averages and cutoff estimates by querying the last 15 minutes of `flux-readings` at request time.

### Rationale

Querying ~90 rows (15 minutes at 10-second intervals) is fast and keeps the poller focused on data collection only. No additional DynamoDB table or poller complexity needed.

### Alternatives Considered

- **Poller pre-computes to a new table**: Lambda just reads pre-computed values — Rejected because it adds poller complexity, a new table, and tighter coupling between poller and API concerns

### Consequences

**Positive:**
- Poller remains a pure data collector
- No additional DynamoDB table
- Averages are always based on the freshest data

**Negative:**
- Lambda does more work per `/status` request (one additional Query)
- Marginally higher Lambda execution time

---

## Decision 3: Sustained Grid Import Threshold

**Date**: 2026-04-14
**Status**: accepted

### Context

The `pgridSustained` flag indicates whether grid import is a real sustained draw vs. a brief blip from battery settling. The threshold needs to distinguish between transient spikes and genuine grid dependency.

### Decision

Flag `pgridSustained` as `true` when grid import exceeds 500W for 3 or more consecutive readings (~30 seconds at 10-second intervals).

### Rationale

Three consecutive readings (~30 seconds) is long enough to filter single-reading blips from battery state transitions, while still being responsive enough to flag real grid import within a minute.

### Alternatives Considered

- **6 consecutive readings (~60 seconds)**: More conservative filtering — Rejected as too slow to react, the user would see stale grid state for a full minute
- **Configurable via SSM**: Tunable threshold — Rejected as unnecessary complexity for V1; the threshold can be changed via code if needed

### Consequences

**Positive:**
- Filters transient spikes from battery transitions
- Fast enough to flag real imports within 30 seconds

**Negative:**
- Fixed threshold — requires code change to adjust

---

## Decision 4: Day Detail Data Resolution

**Date**: 2026-04-14
**Status**: accepted

### Context

The `/day` endpoint returns time-series readings for chart rendering. Raw `flux-readings` data is at 10-second intervals, producing ~8,640 rows per day (~900KB JSON). This is excessive for mobile chart rendering.

### Decision

Downsample to approximately 5-minute intervals, returning ~288 data points per day.

### Rationale

288 points provides sufficient chart fidelity for a phone screen while keeping the payload around 30KB. SwiftUI Charts does not benefit from 10-second resolution at day-scale zoom.

### Alternatives Considered

- **Return all readings**: Maximum fidelity — Rejected because ~900KB payloads are excessive for mobile, and charts can't render meaningful distinction between 10-second points at day scale
- **Configurable resolution parameter**: Client-specified resolution — Rejected as premature flexibility; V1 has one client with one use case

### Consequences

**Positive:**
- ~30x smaller payloads (~30KB vs ~900KB)
- Faster response times and lower mobile data usage
- Sufficient fidelity for day-scale charts

**Negative:**
- Short spikes (<5 minutes) may be smoothed out in chart data

---

## Decision 5: Cutoff Time Estimation Method

**Date**: 2026-04-14
**Status**: accepted

### Context

The app displays an estimated time when the battery will hit the 10% cutoff threshold. The estimate needs to be derived from current battery state and discharge rate.

### Decision

Use linear extrapolation: `timeRemaining = (soc - cutoff) * capacityKwh / dischargeRateKw`, then add to current time.

### Rationale

Linear extrapolation is simple, predictable, and sufficient for a monitoring display. Household load is relatively stable over short periods, making linear projection reasonable. Two estimates are provided: one from instantaneous discharge and one from the 15-minute rolling average.

### Alternatives Considered

- **Weighted recent trend**: Weight recent readings higher for more responsive estimates — Rejected because added complexity provides marginal improvement for a display value that updates every 10 seconds anyway

### Consequences

**Positive:**
- Simple, predictable calculation
- Easy to understand and debug
- Two estimates (instant and rolling) give the user both reactive and smoothed projections

**Negative:**
- Assumes constant discharge rate, which diverges when load changes significantly

---

## Decision 6: Error Response Format

**Date**: 2026-04-14
**Status**: accepted

### Context

The app needs to handle error responses from the Lambda. A consistent format simplifies client-side error handling.

### Decision

Return `{"error": "message"}` with appropriate HTTP status codes (400, 401, 404, 500).

### Rationale

A simple string error field is sufficient for a two-user app. The app only needs to know whether to retry, re-authenticate, or show an error message.

### Alternatives Considered

- **Structured error codes**: `{"error": {"code": "INVALID_DATE", "message": "..."}}` — Rejected because machine-parseable error codes add complexity without value when there's a single client under our control

### Consequences

**Positive:**
- Simple to implement and parse
- Consistent across all error cases

**Negative:**
- No machine-parseable error codes for programmatic error handling (acceptable for V1)

---

## Decision 7: Single System Assumption

**Date**: 2026-04-14
**Status**: accepted

### Context

The V1 plan confirms a single AlphaESS system. The Lambda could be designed to accept a serial number parameter for future multi-system support.

### Decision

Hard-code single system. Serial number loaded from SSM, no client parameter accepted.

### Rationale

The V1 plan explicitly states single system. Adding a serial parameter creates unnecessary API surface and validation logic. If multi-system is needed in V2+, it's a straightforward addition.

### Alternatives Considered

- **Optional serial parameter with SSM default**: Accept optional serial, default to SSM — Rejected because it adds API surface, validation, and authorization concerns (which serial can this user access?) for a feature that may never be needed

### Consequences

**Positive:**
- Simpler API with fewer parameters
- No authorization concerns about which system a client can access
- Smaller attack surface

**Negative:**
- Requires API changes if multi-system support is added later

---

## Decision 8: Timezone for Date-Based Operations

**Date**: 2026-04-14
**Status**: accepted

### Context

Multiple API operations reference "today's date" (off-peak lookup, daily energy, history range). The poller writes date-keyed records using Australian time. The Lambda defaults to UTC. A mismatch means the Lambda looks up the wrong date for roughly half of each day.

### Decision

Use `Australia/Sydney` timezone for all date-based operations. Add `TZ=Australia/Sydney` to the Lambda's environment variables in CloudFormation, matching the poller.

### Rationale

The poller uses `Australia/Sydney` in production (CloudFormation template line 472). The Lambda must use the same timezone for date boundaries to align. Using the standard `TZ` environment variable is the simplest approach — Go's `time` package respects it automatically.

### Alternatives Considered

- **UTC everywhere**: Align both poller and Lambda on UTC — Rejected because the poller already writes in Australian time, and off-peak windows are defined in local time. Changing the poller would require data migration.

### Consequences

**Positive:**
- Date boundaries match between poller and Lambda
- Off-peak window times are in the expected local timezone
- No data migration needed

**Negative:**
- CloudFormation template needs updating to add `TZ` env var

---

## Decision 9: Downsampling Algorithm for Day Detail

**Date**: 2026-04-14
**Status**: accepted

### Context

The `/day` endpoint returns time-series data for chart rendering. Raw data is at 10-second intervals (~8,640 rows/day). Need a concrete algorithm to reduce to ~288 points.

### Decision

Divide the day into 5-minute buckets and average all readings within each bucket. Omit empty buckets. Compute `socLow`/`socLowTime` from raw data before downsampling.

### Rationale

Averaging preserves the general trend and smooths noise, which is appropriate for chart rendering on a phone screen. Computing socLow from raw data ensures the true minimum is reported even if it falls within a bucket that gets averaged away.

### Alternatives Considered

- **Last reading per bucket**: Simpler, but loses information about within-bucket variation and could miss peaks/troughs
- **Nearest to bucket boundary**: Simulates regular sampling, but arbitrary choice of anchor point affects results

### Consequences

**Positive:**
- Smooth chart data suitable for phone display
- Deterministic — same input always produces same output
- socLow accuracy preserved from raw data

**Negative:**
- Short spikes (<5 minutes) are smoothed out in chart data

---

## Decision 10: low24h Data Source

**Date**: 2026-04-14
**Status**: accepted

### Context

The 24-hour battery low needs a data source. `flux-readings` has 10-second granularity with 30-day TTL. `flux-daily-power` has 5-minute granularity with shorter retention. The V1 plan originally mentioned `flux-daily-power` for 24h battery low.

### Decision

Use `flux-readings` as the source for `low24h`.

### Rationale

`flux-readings` has 10-second granularity (more accurate low detection) and 30-day TTL. The Lambda already queries this table for rolling averages and pgridSustained, so no additional table access pattern is needed. The query for 24h of readings (~8,640 rows) is manageable.

### Alternatives Considered

- **flux-daily-power**: Lower granularity (5-minute), matches V1 plan's original text — Rejected because flux-readings is more accurate and already being queried
- **Both with fallback**: Use readings when available, fall back to daily-power for gaps — Rejected as unnecessary complexity when readings have 30-day retention

### Consequences

**Positive:**
- More accurate low detection (10-second vs 5-minute granularity)
- No additional table access pattern
- Consistent data source for all recent-readings queries

**Negative:**
- Larger query (~8,640 rows vs ~288 rows), though still fast for DynamoDB

---

## Decision 11: Float Precision Rules

**Date**: 2026-04-14
**Status**: accepted

### Context

The V1 plan's JSON examples show kWh values with 2 decimal places (e.g., `0.25`, `5.94`) but watts/SOC with 1 decimal place (e.g., `207.0`, `41.2`). A blanket rounding rule would conflict with one or the other.

### Decision

Energy values (kWh) use 2 decimal places. Power values (watts) and SOC (percentage) use 1 decimal place.

### Rationale

Matches the V1 plan's JSON examples exactly. Two decimal places for kWh provides meaningful precision for energy accounting. One decimal place for watts and SOC is sufficient for display.

### Alternatives Considered

- **All 1 decimal place**: Simpler but loses precision on small kWh values (e.g., 0.25 → 0.3)
- **No rounding**: Return raw float values — Rejected because inconsistent precision makes the API harder to consume and test

### Consequences

**Positive:**
- Matches V1 plan examples exactly
- Appropriate precision per value type

**Negative:**
- Implementation must distinguish between energy and power fields for rounding

---

## Decision 12: Time to Full Estimate

**Date**: 2026-04-14
**Status**: accepted

### Context

The V1 plan mentions "Charging · full ~2:45 PM" as a dashboard status line. This requires computing an estimated time to reach 100% SOC when charging. The Lambda could compute this using the same linear extrapolation approach as the cutoff estimate.

### Decision

Defer time-to-full estimation to V2+. The app shows "Charging" without a time estimate in V1.

### Rationale

The cutoff estimate (time to 10%) is more critical for user decision-making (will the battery last until off-peak?). Time-to-full is a nice-to-have that can be added without API changes (new field in the battery object).

### Alternatives Considered

- **Add it now**: Same formula as cutoff but projecting to 100% — Rejected as lower priority than other V1 work; the charge rate varies more than discharge rate (solar fluctuations), making the estimate less reliable

### Consequences

**Positive:**
- Reduced V1 scope
- Can be added later as a new field without breaking changes

**Negative:**
- Dashboard charging status line is less informative in V1

---

## Decision 13: DynamoDB Read Layer Design

**Date**: 2026-04-15
**Status**: accepted

### Context

The existing `internal/dynamo` package has a write-focused `Store` interface used by the poller, with a `DynamoAPI` client interface that includes `PutItem`, `DeleteItem`, `GetItem`, and `BatchWriteItem`. The Lambda API needs read operations (`Query`, `GetItem`) to fetch data from all five tables.

### Decision

Add a new `Reader` interface to `internal/dynamo` alongside the existing `Store`. Create a separate `ReadAPI` client interface with only `Query` and `GetItem`. Implement `DynamoReader` using `ReadAPI`.

### Rationale

A separate `Reader` interface keeps the separation between write concerns (poller) and read concerns (API) clean. A separate `ReadAPI` client interface avoids adding `Query` to the existing `DynamoAPI`, which would force all poller test mocks to implement a method they never use. The production DynamoDB client satisfies both interfaces.

### Alternatives Considered

- **Extend Store interface**: Add read methods to the existing Store — Rejected because it forces LogStore and all poller mocks to implement no-op read methods, violating interface segregation
- **API-owned interface**: Define the read interface in internal/api — Rejected because it splits DynamoDB concerns across packages and the models are already in internal/dynamo

### Consequences

**Positive:**
- Clean separation between read and write concerns
- No changes to existing poller code or tests
- DynamoDB operations stay centralised in one package

**Negative:**
- Two interfaces in the same package (Reader and Store) that serve different consumers
- Two client interfaces (ReadAPI and DynamoAPI) that the same DynamoDB client satisfies

---

## Decision 14: Query Optimisation for /status

**Date**: 2026-04-15
**Status**: accepted

### Context

The `/status` endpoint needs readings from three different time windows: last 60 seconds (for pgridSustained), last 15 minutes (for rolling averages), and last 24 hours (for low24h). Each could be a separate DynamoDB Query.

### Decision

Issue a single 24-hour Query to `flux-readings` and derive the 60-second and 15-minute subsets by filtering in memory.

### Rationale

The 24h query returns ~8,640 items. Filtering to 60s or 15min subsets in Go is trivial compared to the latency of additional DynamoDB round trips. One query instead of three reduces DynamoDB read units and request latency.

### Alternatives Considered

- **Three separate queries**: Minimal data transfer per query — Rejected because the latency of three sequential DynamoDB queries is higher than one query plus in-memory filtering, and the 24h dataset is small enough to hold in memory

### Consequences

**Positive:**
- Single DynamoDB round trip instead of three
- Lower read unit consumption (one query vs three)
- Simpler error handling (one failure point)

**Negative:**
- Transfers ~8,640 items even when only the latest reading is needed (acceptable for a Lambda with 128MB memory)

---
