# Explanation: Lambda API Design

## Beginner Level

### What This Does
The Flux app shows you the status of a home battery system — how much power your solar panels are generating, whether you're using grid electricity, and when the battery might run out. This design document describes the "backend brain" that the app talks to.

Think of it like a restaurant. The AlphaESS battery system is the kitchen, constantly producing data. A separate worker (the "poller") walks to the kitchen every 10 seconds, writes down the latest numbers, and stores them in a filing cabinet (DynamoDB). When your phone app wants to know what's happening, it asks the Lambda API — a clerk who reads from the filing cabinet, does some quick maths (like "at this rate, your battery will last until 4 AM"), and hands back a neat summary.

### Why It Matters
Without this component, the app has no data to display. The Lambda API is the only way the app gets information about the battery system.

### Key Concepts
- **Lambda**: A small program that runs on Amazon's servers only when someone calls it — no server to manage
- **Function URL**: A web address that triggers the Lambda when you visit it
- **DynamoDB**: Amazon's database service — stores the battery data as rows in tables
- **SSM Parameter Store**: A secure place to store secrets (like passwords) on Amazon's servers
- **Cold start**: The first time a Lambda runs after being idle, it has to set itself up — this takes a little longer
- **Bearer token**: A password sent with each request to prove you're allowed to access the data

---

## Intermediate Level

### Changes Overview
This design adds two new packages and modifies one:
- **`cmd/api/`** — Lambda entry point with cold-start initialisation (AWS config, SSM fetches, env var loading)
- **`internal/api/`** — Request handler, three endpoint handlers, business logic, JSON response types
- **`internal/dynamo/`** — New `Reader` interface and `DynamoReader` implementation for read operations, alongside the existing write-focused `Store`

### Implementation Approach
The architecture follows the existing project patterns:
- **Dependency injection** — `Handler` receives a `dynamo.Reader` interface, making it testable with mocks
- **Interface segregation** — A new `ReadAPI` client interface (Query + GetItem) avoids modifying the existing `DynamoAPI` (PutItem + DeleteItem + GetItem + BatchWriteItem)
- **Pure business logic** — `compute.go` contains functions with no I/O dependencies: cutoff estimation, rolling averages, sustained grid detection, downsampling
- **Single 24h query optimisation** — Instead of three separate DynamoDB queries for 60s/15min/24h readings, one 24h query is issued and subsets are filtered in memory

The handler uses `events.LambdaFunctionURLRequest/Response` from `aws-lambda-go`. Auth runs before routing (prevents path enumeration). Nullable JSON fields use pointer types.

### Trade-offs
- **Reader vs extending Store**: Chose a separate interface to avoid forcing the poller's `LogStore` and test mocks to implement read methods they'll never use. The cost is two interfaces in the same package.
- **Single 24h query**: Transfers ~8,640 items even when only the latest reading is needed. Acceptable given the 128MB Lambda memory and the latency savings from avoiding three round trips.
- **Concurrent GetItem calls**: Using `errgroup` for parallel system/offpeak/daily-energy GetItem calls adds complexity but reduces total latency since these are independent operations.

---

## Expert Level

### Technical Deep Dive
The most interesting implementation challenge is the `/status` endpoint, which aggregates data from 4 different tables in a single response. The query plan:

1. One `Query` on `flux-readings` for the last 24h (~8,640 items, sorted by timestamp ascending)
2. Three concurrent `GetItem` calls via `errgroup`: flux-system, flux-offpeak, flux-daily-energy
3. In-memory filtering of the 24h readings to extract 60s and 15min subsets
4. Pure function computations: `pgridSustained`, rolling averages, cutoff estimates, min SOC

The 24h query is the bottleneck. At ~8,640 items × ~100 bytes each ≈ 864KB, this consumes roughly 217 read capacity units (4KB per unit, eventually consistent). On-demand billing makes this predictable.

The `pgridSustained` algorithm iterates readings in reverse chronological order, tracking consecutive readings where `pgrid > 500W` with no gap exceeding 30 seconds. This handles poller restarts gracefully.

The downsampling algorithm divides a day into 288 fixed 5-minute buckets by computing `bucket = (timestamp - dayStart) / 300`. All readings in each bucket are averaged. `socLow` is computed from raw data before downsampling to preserve accuracy.

### Architecture Impact
- **DynamoDB read patterns**: The Lambda's `Query` operations benefit from the existing partition key (sysSn) + sort key (timestamp/date) design. No GSIs or table changes needed.
- **The `ReadAPI` interface** is deliberately minimal (Query + GetItem). The production DynamoDB client satisfies both `ReadAPI` and `DynamoAPI` without any wrapper.
- **CloudFormation change**: Adding `TZ: Australia/Melbourne` to the Lambda env vars is the only infrastructure change. No new resources.
- **Model reuse**: The `Reader` methods return existing model types, keeping the JSON contract decoupled from the storage schema.

### Potential Issues
1. **24h query size growth**: If polling frequency changes, the 24h query grows linearly. Monitor if the poller is modified.
2. **`QueryDailyPower` with `begins_with`**: Relies on the AlphaESS API's `uploadTime` format starting with the date string. Format changes would break this silently.
3. **Cold start latency**: Two SSM GetParameter calls + AWS config loading ≈ 300-500ms. For a two-user app with 10-second refresh, cold starts are frequent.
4. **Timezone handling**: The `TZ` env var affects `time.Now()` globally. Response timestamps must explicitly call `.UTC()` before formatting.
5. **`GetLatestReading` redundancy**: Could be extracted from the 24h query results instead of a separate call.

---

## Validation Findings

### Gaps Identified
1. **`GetLatestReading` is redundant with the 24h query** — the latest reading is the last element of the 24h results. Consider removing it from the Reader interface for `/status` use.
2. **Rounding application point** — the design states rounding happens during model-to-response mapping but doesn't show a systematic approach. Easy to miss for new fields.
3. **Concurrency model in `/status`** — the dependency graph (24h query must complete before in-memory filtering, but GetItems can run concurrently) should be more explicit.

### Logic Issues
None found. The data flow from DynamoDB → Reader → handler → compute → response is clean and unidirectional.

### Recommendations
1. Consider extracting the latest reading from the 24h query results to eliminate a separate DynamoDB call
2. Document the expected `uploadTime` format from AlphaESS API
3. Make the `/status` concurrency model (sequential vs parallel operations) more explicit in the design
