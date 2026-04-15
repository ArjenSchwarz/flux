# Implementation: Lambda API

This document describes the Lambda API implementation at three levels of depth, followed by a validation assessment and completeness review against the requirements.

---

## Beginner Level

### What This Does

The Lambda API is a small web server that runs in AWS Lambda. When the Flux iOS app wants to know about the battery system — current power readings, historical energy data, or a detailed view of a past day — it sends an HTTP request to this Lambda, and the Lambda reads the relevant data from DynamoDB and sends back a JSON response.

There are three things you can ask for:

- **`/status`** — What is happening right now? Returns live power readings, battery state, and today's energy totals.
- **`/history`** — What happened over the past 7, 14, or 30 days? Returns daily energy summaries.
- **`/day`** — What happened on a specific day? Returns a detailed time-series of readings for that date.

Before responding to any request, the Lambda checks that the caller has provided the correct bearer token. If the token is missing or wrong, the request is rejected immediately.

### Why It Matters

The iOS app never talks to the AlphaESS battery system directly. All data collection is done by a separate "poller" service running on AWS, which writes readings to DynamoDB tables. The Lambda acts as the bridge: it reads that stored data and presents it in a form the app can use directly. This keeps the app simple — it just makes HTTP requests and renders what it receives.

### Key Concepts

- **Lambda Function URL** — Instead of going through an API Gateway, the Lambda has its own URL. This is simpler and cheaper for a small app with a handful of users.
- **DynamoDB** — A NoSQL database that stores the battery readings. The Lambda only reads from it; writing is handled by the poller.
- **Bearer Token** — A shared secret. The app sends it in an `Authorization` header with every request, and the Lambda checks it against a value stored in AWS SSM (a secure configuration store).
- **Cold Start** — When Lambda runs for the first time (or after being idle), it loads configuration from SSM and environment variables. Subsequent requests reuse that loaded state.

---

## Intermediate Level

### Changes Overview

This branch adds a complete Lambda API implementation across five commits:

1. `Response types and compute functions` — Adds `internal/api/response.go` (all JSON structs) and `internal/api/compute.go` (pure business logic: cutoff time, rolling averages, sustained grid detection, downsampling, min SOC).
2. `Handler with routing and bearer token authentication` — Adds `internal/api/handler.go` with the `Handler` struct, auth validation using `crypto/subtle.ConstantTimeCompare`, method checking, and path-based routing.
3. `Endpoint handlers for /status, /history, and /day` — Adds the three endpoint handlers. Both `/status` and `/day` use `errgroup` to run DynamoDB queries concurrently, then derive all values in-memory.
4. `DynamoDB Reader with generic helpers` — Adds `internal/dynamo/reader.go` with the `Reader` interface and `DynamoReader` implementation. Generic `queryAll[T]` and `getItem[T]` helpers eliminate pagination boilerplate.
5. `Lambda entry point, build target, and CloudFormation updates` — Adds `cmd/api/main.go` (cold-start initialisation), the `build-api` Makefile target, and the Lambda resource definition in the CloudFormation template.

### Implementation Approach

**Single 24-hour query, filtered in-memory for `/status`**

Rather than making three separate DynamoDB queries for the 60-second, 15-minute, and 24-hour reading windows, `handleStatus` makes one query covering the full 24-hour range. It then filters the result in-memory to derive the shorter windows via `filterReadings`. This reduces DynamoDB round-trips and is efficient because all filtering is a single pass over the result slice.

**Concurrent DynamoDB fetches with `errgroup`**

Both `/status` and `/day` use `errgroup.WithContext` for parallel I/O. The `/status` handler runs 4 concurrent queries (readings, system, offpeak, daily energy). The `/day` handler runs 2 concurrent queries (readings and daily energy), with a sequential fallback to `flux-daily-power` if readings are empty. If any query fails, the context is cancelled and the handler returns HTTP 500.

**Generic DynamoDB helpers**

`queryAll[T]` implements a single pagination loop that works for all DynamoDB Query operations. It accepts a type parameter T, key condition, expression names/values, and handles `LastEvaluatedKey` iteration, unmarshalling, and empty-slice initialisation in one place. `getItem[T]` does the same for GetItem calls. These eliminate ~100 lines of duplicated pagination loops that would otherwise exist for each query method.

**Downsampling in `/day`**

Raw readings from `flux-readings` arrive approximately every 10 seconds (up to ~8,640 per day). The `/day` endpoint buckets these into 288 five-minute intervals and averages each bucket. This gives the app a manageable number of chart points. The `socLow` value is computed from the raw pre-downsampled data to preserve accuracy.

**Fallback from `flux-readings` to `flux-daily-power`**

For historical days where `flux-readings` data has expired (30-day TTL), the `/day` endpoint falls back to the lower-resolution `flux-daily-power` table. Fallback data maps `cbat` to `soc` and sets power fields to zero. It is not passed through the downsampling function since it is already at roughly 5-minute intervals.

**Timezone handling**

The Lambda embeds timezone data via `_ "time/tzdata"` because the `provided.al2023` runtime does not ship `/usr/share/zoneinfo`. A package-level `sydneyTZ` variable is loaded once via an IIFE that panics on error (fail-fast), replacing repeated `time.LoadLocation` calls. The `TZ` environment variable (set to `Australia/Sydney` in CloudFormation) is used for all date-keyed operations.

**DynamoDB pagination**

All `Query*` methods use `queryAll[T]` which loops on `LastEvaluatedKey`. The 24-hour readings query produces roughly 1.3–1.7 MB of wire data, exceeding DynamoDB's 1 MB per-page limit. Without pagination, results would be silently truncated.

### Trade-offs

| Decision | Benefit | Cost |
|---|---|---|
| Single 24h readings query | One DynamoDB call instead of three | Loads more data into memory than each narrower query would |
| Generic `queryAll[T]`/`getItem[T]` | DRY pagination, ~100 fewer lines | Slightly more abstract than inline loops |
| No API Gateway | Lower cost, simpler infrastructure | No built-in request throttling, no usage plans |
| Fallback capacity of 13.34 kWh | Graceful degradation if `flux-system` is absent | Hardcoded constant that becomes wrong if the battery is replaced |
| `sydneyTZ` as package-level IIFE | Single allocation, fail-fast, no repeated lookups | Panics at import time if timezone data is unavailable |

---

## Expert Level

### Technical Deep Dive

**`ReadAPI` interface isolation**

`DynamoReader` depends on `ReadAPI` (only `Query` and `GetItem`), not on the full `DynamoAPI` used by the poller's `DynamoStore`. This is intentional: the poller test mocks implement `DynamoAPI`; introducing a broader dependency would require updating those mocks. The production `*dynamodb.Client` satisfies both interfaces without modification.

**`getItem[T]` shared helper**

Both `DynamoStore` (poller) and `DynamoReader` (API) need to fetch offpeak records by the same key pattern. The implementation extracts this into a generic `getItem[T]` function that accepts any client implementing `GetItem`. An `offpeakKey` helper constructs the composite key. This prevents implementation divergence between the two access paths.

**`computePgridSustained` iteration order**

The function receives readings in ascending timestamp order and iterates backwards from `len(readings)-1`. It counts consecutive readings where `pgrid > 500` with each pair no more than 30 seconds apart. Starting from the end means it evaluates only the current run — a historical burst followed by a drop returns `false`. The loop starts with `consecutive = 1` (the last reading) and increments for each qualifying predecessor.

**Cutoff time formula and NaN/Inf guard**

`computeCutoffTime` converts `pbat` from watts to kilowatts by dividing by 1000, then divides `remainingKwh` by `(pbat/1000)`. At very low discharge rates or with unexpected sensor data, the result could be NaN or Inf. The function guards against this with explicit `math.IsNaN` and `math.IsInf` checks, as well as a `capacityKwh <= 0` guard, returning nil for any invalid computation.

**Bucket timestamp assignment in `downsample`**

The bucket index is computed as `(hour*60 + minute) / 5` using the reading's time in the Sydney timezone. The bucket timestamp is set to `dayStart + i*5min`, representing the start of each bucket interval. Buckets are iterated 0..287, producing chronologically-ordered output without a sort step.

**`DaySummary` zero-value fields when no readings exist**

When `handleDay` produces a `DaySummary` with `hasReadings = false` and only daily energy data, `SocLow` and `SocLowTime` are their zero values (`0` and `""` respectively). These are non-pointer types, so they cannot represent null. The app must handle `socLowTime = ""` defensively (it is not a valid RFC3339 string).

### Architecture Impact

The Lambda integrates cleanly with the existing codebase:

- It extends `internal/dynamo` with a read-side interface without modifying existing write-side code.
- It adds `internal/api` as a new package, keeping API logic isolated.
- `cmd/api/main.go` follows the same pattern as `cmd/poller/main.go` (structured logging, SSM fetching, dependency injection).
- The Makefile gains a `build-api` target parallel to the existing `build` target.
- CloudFormation adds the Lambda and Function URL resources to the existing stack.

### Potential Issues

**`DaySummary` with zero `SocLow` and empty `SocLowTime`**

When only `flux-daily-energy` data exists for a date (no readings from either source), `hasReadings` is `false` and `dailyEnergy != nil`, so a `DaySummary` is created with `summary.SocLow = 0` and `summary.SocLowTime = ""`. The app would receive `"socLow": 0, "socLowTime": ""` alongside real energy values. Requirement 9.13 does not explicitly address `socLow` when readings are absent, but `socLowTime = ""` is not a valid RFC3339 string. A pointer type (`*float64`, `*string`) for these fields would allow true null serialisation.

**Timezone reliance on `TZ` environment variable**

The package-level `sydneyTZ` is loaded explicitly via `time.LoadLocation("Australia/Sydney")`, which works regardless of the `TZ` env var. However, `time.Now()` (used via `nowFunc`) returns time in UTC unless `TZ` is set. The CloudFormation template sets `TZ: Australia/Sydney`, but if this is removed, `time.Now().Format("2006-01-02")` would produce UTC dates rather than Sydney dates, causing incorrect day boundaries.

**`findMinSOCFromPower` timestamp parsing**

`findMinSOCFromPower` parses `UploadTime` strings using `time.ParseInLocation` with the format `"2006-01-02 15:04:05"`. If the AlphaESS API changes this format, parsing silently returns the zero time, producing incorrect timestamps. The function returns `found = true` even if all timestamps fail to parse, which would yield a `socLowTime` of `1970-01-01T00:00:00Z`.

---

## Validation Findings

### Gaps Identified

**`DaySummary` when no readings but daily energy exists**

When `hasReadings = false` and `dailyEnergy != nil`, the code creates a `DaySummary` and populates energy fields. However, `SocLow` and `SocLowTime` are non-pointer types (`float64` and `string`), so they serialise as `0` and `""` instead of being absent. A pointer type would allow true null serialisation.

### Recommendations

1. Change `SocLow` and `SocLowTime` in `DaySummary` to pointer types (`*float64` and `*string`) to allow true null serialisation when no readings exist.
2. Add a test for the `/day` endpoint when only `flux-daily-energy` exists but no readings from either source (the `hasReadings=false`, `dailyEnergy != nil` path), verifying the zero-value behaviour.
3. Add validation for the `UploadTime` format in `findMinSOCFromPower` to detect format changes.

---

## Completeness Assessment

### Fully Implemented

**Section 1: Implementation Constraints (1.1–1.8)** — All eight criteria met. Go binary at `cmd/api/main.go`, compiles to `bootstrap` via `build-api`, `linux/arm64`, uses `aws-lambda-go`, reuses `internal/dynamo`, AWS SDK v2.

**Section 2: Authentication and Authorisation (2.1–2.7)** — All seven criteria met. Bearer token validation with `crypto/subtle.ConstantTimeCompare`, auth before routing, token loaded from SSM during cold start, serial from SSM.

**Section 3: Status Endpoint — Live Data (3.1–3.8)** — All eight criteria met. `pgridSustained` checks 3+ consecutive readings with `pgrid > 500` within 30s gaps via backwards iteration.

**Section 4: Status Endpoint — Battery Information (4.1–4.10)** — All ten criteria met. Cutoff estimation with NaN/Inf guards, fallback capacity of 13.34 kWh, 24h low SOC.

**Section 5: Status Endpoint — Rolling Averages (5.1–5.7)** — All seven criteria met. Requires >= 2 readings in 15-minute window.

**Section 6: Status Endpoint — Off-Peak Data (6.1–6.5)** — All five criteria met. Delta fields only populated when status is "complete".

**Section 7: Status Endpoint — Today's Energy (7.1–7.3)** — All three criteria met.

**Section 8: History Endpoint (8.1–8.8)** — All eight criteria met. `validDays` map for O(1) lookup, ascending date order via `ScanIndexForward: true`.

**Section 9: Day Detail Endpoint (9.1–9.14)** — Thirteen of fourteen criteria fully met. See Partially Implemented for 9.13.

**Section 10: Response Format and Error Handling (10.1–10.7)** — All seven criteria met. `errorResponse` uses `json.Marshal` for safe JSON escaping.

**Section 11: Runtime Configuration (11.1–11.7)** — All seven criteria met. Timezone loaded explicitly via `time.LoadLocation` in the `sydneyTZ` IIFE.

**Section 12: Observability (12.1–12.4)** — All four criteria met. Structured JSON logging via `slog`, request logging with method/path/status/duration, bearer token never logged.

### Partially Implemented

**Requirement 9.13 — DaySummary when only daily energy exists**

When `hasReadings = false` and `dailyEnergy != nil`, the implementation creates a `DaySummary` and populates energy fields. `SocLow` and `SocLowTime` are non-pointer types, so they serialise as `0` and `""` instead of null. The app must handle this defensively.

### Missing

No requirements are entirely missing. All 12 sections have implementations covering their acceptance criteria.
