# Design: Lambda API

## Overview

The Lambda API is a Go binary that runs on AWS Lambda behind a Function URL. It handles HTTP requests from the Flux iOS app, reads from five DynamoDB tables populated by the Fargate poller, computes derived statistics (rolling averages, cutoff estimates, sustained grid detection), and returns JSON responses.

The design follows the existing project conventions: dependency injection via interfaces, structured logging with `slog`, and configuration via environment variables and SSM Parameter Store. The Lambda reuses the existing `internal/dynamo` package for models and table names, and adds a new `Reader` interface for DynamoDB read operations.

---

## Architecture

```
┌──────────────────────────────────────────────────────┐
│  cmd/api/main.go                                     │
│  ┌────────────────────────────────────────────────┐  │
│  │  Cold Start (init)                             │  │
│  │  • Load AWS config                             │  │
│  │  • Create DynamoDB client                      │  │
│  │  • Fetch SSM params (api-token, serial)        │  │
│  │  • Load env vars (tables, offpeak, TZ)         │  │
│  │  • Create Handler with Reader dependency       │  │
│  └────────────────────────────────────────────────┘  │
│  lambda.Start(handler.Handle)                        │
└──────────────────────────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────┐
│  internal/api/handler.go                             │
│  ┌────────────────────────────────────────────────┐  │
│  │  Handler.Handle(ctx, LambdaFunctionURLRequest) │  │
│  │  1. Log request                                │  │
│  │  2. Check method (GET only)                    │  │
│  │  3. Validate bearer token                      │  │
│  │  4. Route: /status | /history | /day | 404     │  │
│  │  5. Log response + duration                    │  │
│  └────────────────────────────────────────────────┘  │
│                                                      │
│  status.go   → handleStatus()                        │
│  history.go  → handleHistory()                       │
│  day.go      → handleDay()                           │
│  compute.go  → cutoff, rolling avg, sustained, etc.  │
│  response.go → JSON struct definitions               │
└──────────────────────────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────┐
│  internal/dynamo/                                    │
│  ┌───────────────────┐  ┌─────────────────────────┐  │
│  │  Reader interface │  │  Store interface         │  │
│  │  (new, for API)   │  │  (existing, for poller)  │  │
│  └────────┬──────────┘  └─────────────────────────┘  │
│           │                                          │
│  ┌────────▼──────────┐                               │
│  │  DynamoReader     │  Uses DynamoDB Query/GetItem  │
│  └───────────────────┘                               │
│                                                      │
│  models.go     → ReadingItem, DailyEnergyItem, etc.  │
│  store.go      → TableNames (shared)                 │
└──────────────────────────────────────────────────────┘
```

### Request Flow

1. Lambda Function URL receives HTTP request → passes `LambdaFunctionURLRequest` event to handler
2. `Handler.Handle` checks method (405 if not GET), validates bearer token (401 if invalid), routes to endpoint handler (404 if unknown)
3. Endpoint handler queries DynamoDB via `Reader`, computes derived values, returns JSON response
4. `Handler.Handle` logs request metadata (method, path, status, duration)

---

## Components and Interfaces

### `cmd/api/main.go` — Entry Point

Responsibilities:
- Load AWS SDK config
- Create DynamoDB client and SSM client
- Fetch SSM parameters (`api-token`, `serial`) and cache values
- Load environment variables (table names, offpeak window, TZ)
- Validate all configuration is present
- Create `Handler` with all dependencies injected
- Call `lambda.Start(handler.Handle)`

Cold start initialisation runs once per Lambda instance. SSM values are fetched here and passed to the Handler as plain strings — the Handler never touches SSM directly.

The entry point imports `_ "time/tzdata"` to embed timezone data, matching the poller pattern (`cmd/poller/main.go:12`). This ensures the `TZ` environment variable works correctly on the `provided.al2023` runtime.

```go
func main() {
    ctx := context.Background()
    cfg, err := loadConfig(ctx)
    if err != nil {
        slog.Error("init failed", "error", err)
        os.Exit(1)
    }

    handler := api.NewHandler(cfg.reader, cfg.serial, cfg.apiToken, cfg.offpeakStart, cfg.offpeakEnd)
    lambda.Start(handler.Handle)
}
```

The `loadConfig` function handles all AWS setup, SSM fetching, and env var validation. If any required configuration is missing or SSM calls fail, it returns an error and the Lambda fails to start.

### `internal/api/handler.go` — Request Handler

```go
type Handler struct {
    reader       dynamo.Reader
    serial       string
    apiToken     string
    offpeakStart string  // "11:00" — pass-through display value only
    offpeakEnd   string  // "14:00" — pass-through display value only
}
```

The `Handle` method is the Lambda entry point. It receives a `events.LambdaFunctionURLRequest` and returns a `events.LambdaFunctionURLResponse`.

Processing order:
1. Record start time for duration logging
2. Check HTTP method — return 405 if not GET
3. Extract and validate bearer token — return 401 if missing/invalid
4. Route based on `rawPath`:
   - `/status` → `handleStatus`
   - `/history` → `handleHistory`
   - `/day` → `handleDay`
   - anything else → 404
5. Log request completion (method, path, status, duration)

Authentication uses `crypto/subtle.ConstantTimeCompare` and runs before routing so that unauthenticated requests to unknown paths receive 401, not 404.

### `internal/api/status.go` — Status Endpoint

`handleStatus` aggregates data from multiple DynamoDB queries into the `/status` response. It captures `now := time.Now()` once at the start and uses this value for all time window calculations (24h, 15min, 60s) and cutoff estimates, ensuring consistency within a single request.

It performs these operations:

1. **Latest reading** — `Reader.GetLatestReading(serial)` → `live` object
2. **Recent readings (60s)** — `Reader.QueryReadings(serial, now-60s, now)` → `pgridSustained` calculation
3. **Recent readings (15min)** — `Reader.QueryReadings(serial, now-15min, now)` → rolling averages + cutoff estimate
4. **24h readings (for low)** — `Reader.QueryReadings(serial, now-24h, now)` → `low24h` (min SOC)
5. **System info** — `Reader.GetSystem(serial)` → `capacityKwh`
6. **Off-peak** — `Reader.GetOffpeak(serial, today)` → off-peak deltas
7. **Today's energy** — `Reader.GetDailyEnergy(serial, today)` → `todayEnergy`

**Query optimisation:** Steps 2, 3, and 4 all query `flux-readings` with different time ranges. The 24h query is the largest (~8,640 rows). The Lambda performs a single 24h query and derives the 60-second and 15-minute subsets by filtering the results in memory. This reduces DynamoDB read operations from 3 queries to 1. The latest reading (step 1) is extracted from the 24h query results (last element) rather than a separate `GetLatestReading` call.

**Concurrency model:**

```
Phase 1 (parallel via errgroup):
  ├── QueryReadings(serial, now-24h, now)     → 24h readings
  ├── GetSystem(serial)                        → battery capacity
  ├── GetOffpeak(serial, today)                → off-peak deltas
  └── GetDailyEnergy(serial, today)            → today's energy totals

Phase 2 (sequential, in-memory, after phase 1 completes):
  ├── Extract latest reading from 24h results  → live object
  ├── Filter to last 60s → computePgridSustained
  ├── Filter to last 15min → computeRollingAverages + cutoff estimate
  ├── findMinSOC on full 24h results           → low24h
  └── computeCutoffTime with live reading      → battery cutoff estimate
```

Phase 1 uses `errgroup.WithContext(ctx)` so that if any DynamoDB call fails, the context is cancelled and remaining calls exit early. All Phase 1 operations are required — any failure returns HTTP 500. Phase 2 is pure computation on the results — no I/O.

### `internal/api/history.go` — History Endpoint

`handleHistory` parses the `days` query parameter (default 7, valid: 7/14/30), computes the date range from today in the configured timezone, and queries `flux-daily-energy`.

1. Parse and validate `days` parameter
2. Compute start date: `today.AddDate(0, 0, -(days-1))`
3. `Reader.QueryDailyEnergy(serial, startDate, today)` → sorted array
4. Return response with `days` array

### `internal/api/day.go` — Day Detail Endpoint

`handleDay` returns downsampled time-series readings and a summary for a specific date.

1. Parse and validate `date` parameter
2. Query `flux-readings` for the full day → raw readings
3. If no readings found, fall back to `Reader.QueryDailyPower(serial, date)` → map `cbat` to `soc`, power fields to 0. Fallback data is already at ~5-minute intervals and is **not** passed through the `downsample` function — it is used directly as the readings array
4. Compute `socLow` and `socLowTime` from raw readings (before downsampling). If raw readings exist, use them. If only fallback data exists, use the fallback `cbat` values
5. Downsample `flux-readings` data into 5-minute buckets (average). Skip this step for fallback data
6. Get `flux-daily-energy` for the summary
7. Assemble response: if both readings and daily energy exist, build full summary. If only readings exist, summary has `socLow`/`socLowTime` with null energy fields. If neither exists, summary is `null` (nil pointer, not a zero struct)

### `internal/api/compute.go` — Business Logic

Pure functions with no DynamoDB dependency. All take data as input and return computed values.

**`computeCutoffTime(soc, pbat, capacityKwh float64, cutoffPercent float64, now time.Time) *time.Time`**

Returns nil when:
- `pbat <= 0` (charging or idle)
- `soc <= cutoffPercent`

Otherwise:
```
remainingKwh = (soc - cutoffPercent) / 100 * capacityKwh
hoursRemaining = remainingKwh / (pbat / 1000)
return now.Add(hoursRemaining * time.Hour)
```

**`computeRollingAverages(readings []dynamo.ReadingItem) (avgLoad, avgPbat float64)`**

Returns the mean of `pload` and `pbat` values. Returns 0, 0 if the slice is empty.

**`computePgridSustained(readings []dynamo.ReadingItem) bool`**

Determines whether grid import is currently sustained (not just a transient spike). Iterates readings from the most recent backwards. Counts consecutive readings where `pgrid > 500` with each pair no more than 30 seconds apart. Returns true if 3+ consecutive qualifying readings are found. Stops at the first reading that does not qualify — this means it only evaluates the *current* run of high grid import, not any historical burst within the 60-second window. The function receives a pre-filtered 60-second subset of readings in ascending order and iterates from the end.

Note: the function accepts readings in ascending order (as returned by `QueryReadings`) and iterates backwards using `len(readings)-1` to `0`.

**`downsample(readings []dynamo.ReadingItem, date string) []TimeSeriesPoint`**

Divides a day into 288 five-minute buckets (00:00, 00:05, ..., 23:55). For each bucket, averages all readings that fall within it. Omits empty buckets. Returns sorted points.

**`findMinSOC(readings []dynamo.ReadingItem) (soc float64, timestamp int64, found bool)`**

Scans readings for the minimum SOC value, returning the value and its timestamp.

**`roundEnergy(v float64) float64`** — rounds to 2 decimal places (kWh values)

**`roundPower(v float64) float64`** — rounds to 1 decimal place (watts, SOC)

### `internal/api/response.go` — JSON Response Types

Response structs with `json` tags matching the V1 plan's API contract. Uses `*float64` and `*string` pointer types for nullable fields so they serialise as `null` in JSON rather than zero values.

```go
type StatusResponse struct {
    Live        *LiveData        `json:"live"`
    Battery     *BatteryInfo     `json:"battery"`
    Rolling15m  *RollingAvg      `json:"rolling15min"`
    Offpeak     *OffpeakData     `json:"offpeak"`
    TodayEnergy *TodayEnergy     `json:"todayEnergy"`
}

type LiveData struct {
    Ppv            float64 `json:"ppv"`
    Pload          float64 `json:"pload"`
    Pbat           float64 `json:"pbat"`
    Pgrid          float64 `json:"pgrid"`
    PgridSustained bool    `json:"pgridSustained"`
    Soc            float64 `json:"soc"`
    Timestamp      string  `json:"timestamp"`
}

type BatteryInfo struct {
    CapacityKwh      float64   `json:"capacityKwh"`
    CutoffPercent    int       `json:"cutoffPercent"`
    EstimatedCutoff  *string   `json:"estimatedCutoffTime"`
    Low24h           *Low24h   `json:"low24h"`
}

type Low24h struct {
    Soc       float64 `json:"soc"`
    Timestamp string  `json:"timestamp"`
}

type RollingAvg struct {
    AvgLoad         float64 `json:"avgLoad"`
    AvgPbat         float64 `json:"avgPbat"`
    EstimatedCutoff *string `json:"estimatedCutoffTime"`
}

type OffpeakData struct {
    WindowStart         string   `json:"windowStart"`
    WindowEnd           string   `json:"windowEnd"`
    GridUsageKwh        *float64 `json:"gridUsageKwh"`
    SolarKwh            *float64 `json:"solarKwh"`
    BatteryChargeKwh    *float64 `json:"batteryChargeKwh"`
    BatteryDischargeKwh *float64 `json:"batteryDischargeKwh"`
    GridExportKwh       *float64 `json:"gridExportKwh"`
    BatteryDeltaPercent *float64 `json:"batteryDeltaPercent"`
}

type TodayEnergy struct {
    Epv        float64 `json:"epv"`
    EInput     float64 `json:"eInput"`
    EOutput    float64 `json:"eOutput"`
    ECharge    float64 `json:"eCharge"`
    EDischarge float64 `json:"eDischarge"`
}

type HistoryResponse struct {
    Days []DayEnergy `json:"days"`
}

type DayEnergy struct {
    Date       string  `json:"date"`
    Epv        float64 `json:"epv"`
    EInput     float64 `json:"eInput"`
    EOutput    float64 `json:"eOutput"`
    ECharge    float64 `json:"eCharge"`
    EDischarge float64 `json:"eDischarge"`
}

type DayDetailResponse struct {
    Date     string            `json:"date"`
    Readings []TimeSeriesPoint `json:"readings"`
    Summary  *DaySummary       `json:"summary"`
}

type TimeSeriesPoint struct {
    Timestamp string  `json:"timestamp"`
    Ppv       float64 `json:"ppv"`
    Pload     float64 `json:"pload"`
    Pbat      float64 `json:"pbat"`
    Pgrid     float64 `json:"pgrid"`
    Soc       float64 `json:"soc"`
}

type DaySummary struct {
    Epv        *float64 `json:"epv"`
    EInput     *float64 `json:"eInput"`
    EOutput    *float64 `json:"eOutput"`
    ECharge    *float64 `json:"eCharge"`
    EDischarge *float64 `json:"eDischarge"`
    SocLow     float64  `json:"socLow"`
    SocLowTime string   `json:"socLowTime"`
}
```

### `internal/dynamo/reader.go` — DynamoDB Read Layer

A new `Reader` interface in the existing `internal/dynamo` package, alongside the write-focused `Store`:

```go
// Reader defines read operations for the API Lambda.
type Reader interface {
    QueryReadings(ctx context.Context, serial string, from, to int64) ([]ReadingItem, error)
    GetSystem(ctx context.Context, serial string) (*SystemItem, error)
    GetOffpeak(ctx context.Context, serial, date string) (*OffpeakItem, error)
    GetDailyEnergy(ctx context.Context, serial, date string) (*DailyEnergyItem, error)
    QueryDailyEnergy(ctx context.Context, serial, startDate, endDate string) ([]DailyEnergyItem, error)
    QueryDailyPower(ctx context.Context, serial, date string) ([]DailyPowerItem, error)
}
```

Note: `GetLatestReading` is intentionally absent. The `/status` endpoint extracts the latest reading from the 24h query results (last element), eliminating a separate DynamoDB call. The `/day` endpoint also uses `QueryReadings` for a full day range.

**Not-found convention:** All `Get*` methods return `(nil, nil)` when the item does not exist, matching the existing `DynamoStore.GetOffpeak` pattern. Query methods return `([]T{}, nil)` (empty slice, no error) when no items match.

**Pagination:** All `Query*` methods must handle DynamoDB pagination by looping on `LastEvaluatedKey`. The 24h readings query produces ~864KB of logical data, but DynamoDB wire format inflates this to ~1.3–1.7MB, exceeding the 1MB page limit. Without pagination, results are silently truncated and the "latest reading = last element" optimisation breaks.

**Consistency:** All reads use eventually consistent mode (DynamoDB default). For a monitoring dashboard with 10-second refresh, this is appropriate.

**Ordering:** All `Query*` methods set `ScanIndexForward: true` explicitly (ascending sort key order) rather than relying on the DynamoDB default. This makes the ordering contract visible and prevents subtle bugs.

The `DynamoReader` implementation uses the DynamoDB `Query` and `GetItem` operations:

```go
// ReadAPI is the subset of the DynamoDB client used by DynamoReader.
type ReadAPI interface {
    Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
    GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

type DynamoReader struct {
    client ReadAPI
    tables TableNames
}
```

This is separate from the existing `DynamoAPI` interface to avoid forcing changes on poller test mocks. The production DynamoDB client satisfies both `DynamoAPI` and `ReadAPI`.

**Key query patterns:**

| Method | Table | Key Condition | Sort |
|--------|-------|---------------|------|
| `QueryReadings` | flux-readings | `sysSn = :serial AND timestamp BETWEEN :from AND :to` | ascending |
| `GetSystem` | flux-system | `sysSn = :serial` (GetItem) | — |
| `GetOffpeak` | flux-offpeak | `sysSn = :serial AND date = :date` (GetItem) | — |
| `GetDailyEnergy` | flux-daily-energy | `sysSn = :serial AND date = :date` (GetItem) | — |
| `QueryDailyEnergy` | flux-daily-energy | `sysSn = :serial AND date BETWEEN :start AND :end` | ascending |
| `QueryDailyPower` | flux-daily-power | `sysSn = :serial AND begins_with(uploadTime, :date)` | ascending |

Note: `QueryDailyPower` uses `begins_with` because the `uploadTime` sort key is a timestamp string from the AlphaESS API in the format `"YYYY-MM-DD HH:MM:SS"` (e.g., `"2026-04-10 14:35:00"`). The `begins_with(uploadTime, "2026-04-10")` condition selects all records for a given date.

The existing `DynamoStore.GetOffpeak` performs the same DynamoDB operation as `DynamoReader.GetOffpeak`. To avoid implementation divergence, a shared unexported helper function (e.g., `getOffpeakItem`) can be used by both types. Both `DynamoAPI` and `ReadAPI` include `GetItem`, so the helper can accept the common `GetItem` method signature.

---

## Data Models

All DynamoDB item structs are already defined in `internal/dynamo/models.go`:

| Struct | Table | Used By |
|--------|-------|---------|
| `ReadingItem` | flux-readings | `/status` (live, rolling avg, 24h low, sustained), `/day` (time series) |
| `DailyEnergyItem` | flux-daily-energy | `/status` (today energy), `/history`, `/day` (summary) |
| `DailyPowerItem` | flux-daily-power | `/day` (fallback SOC data) |
| `SystemItem` | flux-system | `/status` (battery capacity) |
| `OffpeakItem` | flux-offpeak | `/status` (off-peak deltas) |

No new DynamoDB models are needed. The `Reader` interface uses the existing model types as return values.

The JSON response structs in `internal/api/response.go` are API-specific and distinct from the DynamoDB models. Each endpoint handler maps DynamoDB models to response structs via dedicated builder functions (e.g., `buildLiveData`, `buildBatteryInfo`, `buildDayEnergy`). Rounding is applied inside these builders — `roundPower` for watts/SOC fields, `roundEnergy` for kWh fields — so rounding is consistently applied in one place per response type rather than scattered across handler code.

---

## Error Handling

### Cold Start Failures

If any of the following fail during cold start, the Lambda logs the error and exits:
- AWS config loading
- SSM parameter fetching (api-token, serial)
- Missing required environment variables

This means the Lambda never starts the handler loop in a broken state. CloudWatch will show the error in the init logs.

### Request-Level Errors

| Condition | HTTP Status | Response Body |
|-----------|-------------|---------------|
| Non-GET method | 405 | `{"error": "method not allowed"}` |
| Missing/invalid bearer token | 401 | `{"error": "unauthorized"}` |
| Unknown path | 404 | `{"error": "not found"}` |
| Invalid `days` parameter | 400 | `{"error": "invalid days parameter, must be 7, 14, or 30"}` |
| Missing/invalid `date` parameter | 400 | `{"error": "invalid or missing date parameter"}` |
| DynamoDB error | 500 | `{"error": "internal error"}` |

All errors are logged with context (table, operation, error message) but only generic messages are returned to the client. The bearer token is never logged.

### Null Handling

Response objects use pointer types for nullable fields. The mapping logic explicitly returns `nil` when:
- `flux-readings` is empty → `live` is `null`
- No readings in 24h → `low24h` is `null`
- Fewer than 2 readings in 15min → `rolling15min` is `null`
- Off-peak record is pending/missing → delta fields are `null`
- No daily energy for today → `todayEnergy` is `null`
- Battery not discharging or SOC ≤ cutoff → `estimatedCutoffTime` is `null`
- `flux-system` missing or `cobat` = 0 → use fallback capacity 13.34 kWh

---

## Testing Strategy

### Unit Tests

**`internal/api/compute_test.go`** — Pure function tests with no mocking needed:

- `TestComputeCutoffTime` — table-driven: discharging (normal case), charging (nil), SOC at cutoff (nil), SOC below cutoff (nil), zero pbat (nil), specific calculation verification
- `TestComputeRollingAverages` — empty slice, single reading, multiple readings, verification of arithmetic
- `TestComputePgridSustained` — 3+ consecutive above threshold (true), 2 consecutive (false), gap > 30s breaks consecutiveness (false), readings below threshold interspersed (false), empty readings (false)
- `TestDownsample` — full day of readings, sparse readings (some empty buckets), single reading, empty input, bucket boundary behaviour, verification that averages are computed correctly
- `TestFindMinSOC` — normal case, single reading, empty input
- `TestRoundEnergy` / `TestRoundPower` — rounding edge cases

**`internal/api/handler_test.go`** — Handler routing and auth tests using a mock `Reader`:

- Method validation (GET passes, POST/PUT/DELETE return 405)
- Auth validation (valid token, missing header, wrong token, malformed header)
- Auth before routing (invalid token + unknown path → 401, not 404)
- Routing (each path returns 200, unknown path returns 404)
- Error responses include correct content-type and JSON body

**`internal/api/status_test.go`** — Status endpoint integration with mock Reader:

- Normal case with all data present
- No readings → null live, null rolling, null low24h
- Off-peak pending → null delta fields
- Off-peak complete → populated delta fields
- No today energy → null todayEnergy
- System info missing → fallback capacity
- DynamoDB error → 500

**`internal/api/history_test.go`** — History endpoint tests:

- Default days (7), explicit 14 and 30
- Invalid days parameter → 400
- No data for range → empty array
- Result ordering (ascending date)

**`internal/api/day_test.go`** — Day detail endpoint tests:

- Normal case with readings and daily energy
- No flux-readings, fallback to flux-daily-power (cbat → soc, power fields → 0)
- No data from either source → empty readings, null summary
- Readings exist but no daily energy → summary with socLow/socLowTime only
- Date parameter validation (missing, invalid format)
- socLow computed from raw data, not downsampled

**`internal/dynamo/reader_test.go`** — DynamoReader tests with mock ReadAPI:

- Each Reader method: successful query, empty result, DynamoDB error
- `QueryReadings`: verify key condition expression, attribute values, and `ScanIndexForward: true`
- `QueryReadings` with pagination: mock returns non-nil `LastEvaluatedKey` on first call, verify all pages are collected
- `QueryDailyEnergy`: verify date range query
- `QueryDailyPower`: verify `begins_with` condition

### Mock Reader

```go
type mockReader struct {
    queryReadingsFn    func(ctx context.Context, serial string, from, to int64) ([]dynamo.ReadingItem, error)
    getSystemFn        func(ctx context.Context, serial string) (*dynamo.SystemItem, error)
    getOffpeakFn       func(ctx context.Context, serial, date string) (*dynamo.OffpeakItem, error)
    getDailyEnergyFn   func(ctx context.Context, serial, date string) (*dynamo.DailyEnergyItem, error)
    queryDailyEnergyFn func(ctx context.Context, serial, start, end string) ([]dynamo.DailyEnergyItem, error)
    queryDailyPowerFn  func(ctx context.Context, serial, date string) ([]dynamo.DailyPowerItem, error)
}
```

Follows the same pattern as the existing `mockDynamoAPI` in `dynamostore_test.go`.

### Test Data

Tests use deterministic timestamps and fixed values. No reliance on wall clock time — functions that need "now" accept it as a parameter.

---

## Infrastructure Changes

The CloudFormation template needs one addition to the Lambda environment variables:

```yaml
Environment:
  Variables:
    TZ: Australia/Sydney  # NEW — matches the poller's timezone for date consistency
    # ... existing vars unchanged
```

Note: `Australia/Sydney` matches the poller's existing `TZ` setting. Both the poller and Lambda must use the same timezone so date-keyed records (`flux-daily-energy`, `flux-offpeak`) align.

The Lambda memory should be increased from 128MB to 256MB. The 24h query loads ~8,640 items into memory, and 256MB provides adequate headroom. The cost difference at this usage level is negligible.

The Lambda timeout is already configured at 10 seconds in the template, which is sufficient for the concurrent DynamoDB operations.

The Lambda runs outside of any VPC. It only accesses DynamoDB and SSM via public AWS endpoints. Placing it in a VPC would add cold start latency and VPC endpoint costs for zero benefit.

The Makefile needs a new build target:

```makefile
build-api:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o lambda/bootstrap ./cmd/api
```

New dependencies needed in `go.mod`:

```
github.com/aws/aws-lambda-go
github.com/aws/aws-sdk-go-v2/service/ssm
```

The entry point must import `_ "time/tzdata"` to embed timezone data, since the `provided.al2023` Lambda runtime may not include `/usr/share/zoneinfo`. Without this, the `TZ` environment variable would silently fall back to UTC.

---

## File Layout

```
cmd/api/
├── main.go           # Lambda entry point, cold start init

internal/api/
├── handler.go        # Handler struct, routing, auth
├── status.go         # /status endpoint
├── history.go        # /history endpoint
├── day.go            # /day endpoint
├── compute.go        # Pure business logic functions
├── response.go       # JSON response structs
├── handler_test.go   # Routing and auth tests
├── status_test.go    # Status endpoint tests
├── history_test.go   # History endpoint tests
├── day_test.go       # Day endpoint tests
└── compute_test.go   # Business logic tests

internal/dynamo/
├── reader.go         # Reader interface + DynamoReader (NEW)
├── reader_test.go    # Reader tests (NEW)
├── (existing files unchanged)
```

---

## Requirement Traceability

| Requirement | Design Element |
|-------------|---------------|
| 1.1–1.8 (Implementation) | `cmd/api/main.go`, Makefile `build-api` target, `go.mod` dependencies |
| 2.1–2.7 (Auth) | `handler.go`: token validation with `subtle.ConstantTimeCompare`, auth-before-routing |
| 3.1–3.8 (Live data) | `status.go`: latest reading from 24h query + `computePgridSustained` from 60s subset |
| 4.1–4.10 (Battery) | `status.go`: `GetSystem` → capacity, `computeCutoffTime`, `findMinSOC` for low24h |
| 5.1–5.7 (Rolling avg) | `status.go`: `QueryReadings(15min)` → `computeRollingAverages`, `computeCutoffTime` |
| 6.1–6.5 (Off-peak) | `status.go`: `GetOffpeak(today)` → check status, return deltas or nulls |
| 7.1–7.3 (Today energy) | `status.go`: `GetDailyEnergy(today)` → `TodayEnergy` or null |
| 8.1–8.8 (History) | `history.go`: parse days, `QueryDailyEnergy(range)` → sorted array |
| 9.1–9.14 (Day detail) | `day.go`: `QueryReadings(day)` → `downsample`, `findMinSOC`, fallback `QueryDailyPower` |
| 10.1–10.7 (Errors) | `handler.go`: error response helpers, content-type, status codes |
| 11.1–11.7 (Config) | `cmd/api/main.go`: `loadConfig` with env vars + SSM |
| 12.1–12.4 (Observability) | `handler.go`: slog request logging, error context, no token logging |
