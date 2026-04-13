# Implementation Explanation: Flux Poller

## Beginner Level

### What This Does

The Flux Poller is a background program that continuously collects data from a home battery system (AlphaESS) and saves it to a database (DynamoDB). Think of it like a weather station that checks the temperature every few seconds and writes it down — except it's checking solar panels, battery charge, and electricity usage.

It runs inside a container on AWS (like a small virtual computer) and never stops. It checks different things at different speeds:
- Every 10 seconds: "How much power is flowing right now?"
- Every hour: "What did the power flow look like today so far?"
- Every 6 hours: "How much total energy was produced/consumed today?"
- Once a day: "What's the battery system's hardware info?"

There's also a special "off-peak" feature. During certain hours (like 11 AM to 2 PM), electricity is cheaper. The poller takes a snapshot at the start and end of this window, then calculates how much energy was used during that cheap period.

### Why It Matters

Without this poller, there's no data for the iOS app to display. It's the sole data collector — the app never talks to AlphaESS directly. The poller writes, the app reads.

### Key Concepts

- **Polling**: Repeatedly asking "what's the current state?" at regular intervals
- **DynamoDB**: Amazon's database service, used here to store readings
- **ECS Fargate**: Amazon's service for running containers without managing servers
- **Off-peak window**: A time period when electricity rates are lower
- **Dry-run mode**: A testing mode where the poller talks to AlphaESS but doesn't write to the database — it just logs what it would write

---

## Intermediate Level

### Architecture

The codebase follows standard Go project layout with `cmd/poller/` for the entrypoint and `internal/` packages for business logic:

```
cmd/poller/main.go          — Config, signal handling, dependency wiring
internal/alphaess/           — HTTP client with SHA-512 auth signing
internal/config/             — Env var loading with collected validation errors
internal/dynamo/             — Store interface with DynamoStore + LogStore
internal/poller/             — Orchestrator + off-peak scheduler
```

### Implementation Approach

**Dependency injection via interfaces**: The `APIClient` interface (defined in the poller package) and `Store` interface (defined in the dynamo package) allow the poller to work with either real or mock implementations. This enables both dry-run mode (LogStore) and unit testing (mock structs).

**Two-context graceful shutdown**: When SIGTERM arrives, a `loopCtx` is cancelled to stop scheduling new work, but a separate `drainCtx` keeps in-flight API calls and DynamoDB writes alive for up to 25 seconds. This prevents data loss during ECS deployments.

**Off-peak state machine**: The `OffpeakScheduler` determines its position relative to the off-peak window on startup (before/during/after) and acts accordingly. It persists a "pending" record to DynamoDB at window start so it can recover if the container restarts mid-window. At window end, it overwrites with a "complete" record containing computed deltas.

**Poll loop extraction**: All four polling schedules use a shared `pollLoop` helper that handles the ticker lifecycle, immediate first poll, and context cancellation — eliminating copy-paste across goroutines.

### Trade-offs

- **Log-and-skip error handling** (no retries for regular polls): The 10-second live data interval provides natural retries. Off-peak snapshots get explicit 3-attempt retries because they're high-value, low-frequency operations.
- **Independent goroutines** (no request serialization): API collisions between schedules are rare and handled gracefully by log-and-skip. A priority queue would add complexity for negligible benefit.
- **Subprocess health check**: The `healthcheck` subcommand runs as a separate process invocation, querying DynamoDB directly. This avoids opening an HTTP port but means each health check creates a new AWS SDK client.

---

## Expert Level

### Technical Deep Dive

**Auth signing**: Every AlphaESS request includes `SHA-512(appId + appSecret + timestamp)` in the `sign` header. The timestamp is generated at request time to stay within the 300-second drift tolerance. The client is stateless — no token refresh or session management.

**DynamoDB write patterns**: All writes use PutItem (last-write-wins). `WriteDailyPower` uses `BatchWriteItem` in chunks of 25 with one retry for unprocessed items. The daily power poll re-writes all of today's snapshots each hour (write amplification ~12x over a day), which is acceptable at this volume but worth noting.

**Off-peak crash recovery**: At window start, a `pending` OffpeakItem is written to DynamoDB. If the container restarts mid-window, the scheduler queries for this record and recovers the start snapshot. If the end snapshot fails after 3 retries, the pending record is deleted (no partial records). The Lambda API filters on `status == "complete"` as defence in depth.

**DST safety**: All wall-clock scheduling uses `time.Date()` rather than duration arithmetic from midnight. This ensures correct behaviour across DST transitions — `time.Date(2026, 4, 5, 1, 0, 0, 0, sydney)` resolves correctly regardless of whether AEDT or AEST is active.

**Timezone data**: The binary embeds `time/tzdata` via blank import, and the distroless base image also includes IANA timezone data. Belt and suspenders.

### Architecture Impact

The poller is the sole writer to all five DynamoDB tables. The Lambda API and future clients are read-only consumers. This eliminates write contention and simplifies the consistency model — the poller owns the data lifecycle.

The `Store` interface (7 methods) is the key abstraction boundary. Adding a new table requires: a new model struct, a new Store method, a new DynamoStore implementation, a new LogStore implementation, and a new transformation function. The interface is intentionally narrow — no generic CRUD, just the specific operations needed.

### Potential Issues

- **No test for `Poller.Run()` goroutine lifecycle**: The two-context shutdown, WaitGroup coordination, and 25s drain timeout are untested. Individual fetch helpers are tested, but the orchestration is not.
- **No test for `OffpeakScheduler.Run()` daily loop**: The `goto nextDay` pattern and state machine transitions are tested indirectly via component methods but not as an integrated flow.
- **Daily power write amplification**: Re-writing all snapshots hourly is correct but wasteful. Could track last-written uploadTime to only write new snapshots.
- **`io.ReadAll` without size limit**: The AlphaESS client reads entire response bodies with no cap. A `LimitReader` would be more defensive.

---

## Completeness Assessment

### Fully Implemented
- All 4 polling schedules with correct intervals [3.1-3.6]
- AlphaESS client with SHA-512 auth and 4 endpoints [2.1-2.9]
- DynamoDB storage with all 5 tables [4.1-4.11]
- Off-peak scheduler with retry, crash recovery, and delta computation [6.1-6.14]
- Configuration from env vars with collected validation [5.1-5.11]
- Health check subcommand [7.1-7.5]
- Graceful shutdown with two-context pattern [8.1-8.5]
- Structured JSON logging [9.1-9.2, 9.5-9.6]
- Dry-run mode with LogStore [12.1-12.6]
- Dockerfile with distroless base [10.1-10.6]
- GitHub Actions CI [11.1-11.5]
- CloudFormation template update [13.1-13.3]
- Midnight energy finalizer [3.7]

### Divergences from Spec (Documented)
- Requirement [9.3] (API call success logging with endpoint name and status) — not implemented. Only error paths are logged. This is a deliberate verbosity trade-off for a 10-second poll cycle but diverges from the spec.
- Requirement [9.4] (DynamoDB write success logging with table name) — not implemented in DynamoStore. LogStore logs table names. Same trade-off as above.
- Requirement [2.4] mentions "per-phase detail" in PowerData — not included in the model. The poller doesn't write per-phase data to DynamoDB, so the fields were omitted.
- Requirement [12.1] mentions `--dry-run` flag — only `DRY_RUN=true` env var is implemented. The spec uses "or" so this is technically compliant.
