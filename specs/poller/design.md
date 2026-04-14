# Design: Flux Poller

## Overview

The Flux Poller is a long-running Go service that bridges the AlphaESS API to DynamoDB. It runs as a single ECS Fargate task, polls four AlphaESS endpoints on independent schedules, and writes to five DynamoDB tables. It also computes off-peak energy deltas by capturing snapshots at configurable window boundaries.

The design prioritises simplicity: no frameworks, minimal dependencies, struct-driven architecture with explicit dependency injection. The only external dependencies beyond the standard library are `aws-sdk-go-v2` for DynamoDB and `time/tzdata` for embedded timezone data.

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  cmd/poller/main.go                                     │
│  - Config loading & validation                          │
│  - Healthcheck subcommand dispatch                      │
│  - Store selection (DynamoDB vs dry-run logger)          │
│  - Signal handling (SIGTERM/SIGINT)                      │
│  - Starts Poller.Run(ctx)                               │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│  internal/poller/poller.go                              │
│  - Orchestrates 4 polling goroutines + off-peak sched   │
│  - Each goroutine: ticker → API call → store write      │
│  - Midnight energy finalization goroutine                │
│  - Context cancellation stops all goroutines             │
└──────┬──────────────────────────────┬───────────────────┘
       │                              │
       ▼                              ▼
┌──────────────────┐    ┌─────────────────────────────────┐
│ internal/        │    │ internal/dynamo/                 │
│ alphaess/        │    │ - Store interface                │
│ - Client struct  │    │ - DynamoStore (real writes)      │
│ - Auth (SHA-512) │    │ - LogStore (dry-run logging)     │
│ - 4 endpoints    │    │ - Models (dynamodbav tags)       │
│ - HTTP + JSON    │    └─────────────────────────────────┘
└──────────────────┘
```

### Project Layout

```
flux/
├── cmd/
│   └── poller/
│       └── main.go              # Entrypoint, config, signal handling
├── internal/
│   ├── alphaess/
│   │   ├── client.go            # HTTP client, auth signing, API calls
│   │   └── models.go            # API response structs
│   ├── dynamo/
│   │   ├── store.go             # Store interface + DynamoStore
│   │   ├── logstore.go          # LogStore (dry-run mode)
│   │   └── models.go            # DynamoDB item structs (dynamodbav tags)
│   ├── poller/
│   │   ├── poller.go            # Polling orchestrator
│   │   └── offpeak.go           # Off-peak window scheduler
│   └── config/
│       └── config.go            # Config struct, env loading, validation
├── Dockerfile
├── Makefile
├── go.mod
└── .github/
    └── workflows/
        └── poller.yml           # Build + push container image
```

---

## Components and Interfaces

### 1. Entrypoint — `cmd/poller/main.go`

Responsibilities:
- Dispatch `healthcheck` subcommand via `os.Args`
- Load and validate configuration from environment variables
- Create the AlphaESS client
- Create the appropriate store implementation (DynamoDB or log-based dry-run)
- Set up signal handling (SIGTERM/SIGINT → context cancellation)
- Start the poller and block until shutdown completes

```go
func main() {
    // Healthcheck subcommand — fast path, no full startup
    if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
        os.Exit(runHealthCheck())
    }

    // Load config
    cfg, err := config.Load()
    if err != nil {
        slog.Error("configuration error", "error", err)
        os.Exit(1)
    }

    // Create dependencies
    client := alphaess.NewClient(cfg.AppID, cfg.AppSecret, cfg.HTTPTimeout)
    store := createStore(cfg)

    // Signal handling
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
    defer cancel()

    // Run poller (blocks until ctx is cancelled)
    p := poller.New(client, store, cfg)
    if err := p.Run(ctx); err != nil {
        slog.Error("poller stopped with error", "error", err)
        os.Exit(1)
    }
}
```

The `runHealthCheck()` function first checks the `DRY_RUN` env var directly — if set to `true`, it exits 0 immediately without creating any AWS clients. In normal mode, it reads `AWS_REGION`, `TABLE_READINGS`, and `SYSTEM_SERIAL` from environment variables, creates a minimal DynamoDB client, and queries `flux-readings` for the most recent item. If the item's `timestamp` is less than 60 seconds before `time.Now().Unix()`, it exits 0. Otherwise (stale or missing), it exits 1.

Traces to: [1.5], [5.9], [7.1]-[7.5], [8.1]-[8.5], [12.1], [12.5], [12.6]

### 2. Configuration — `internal/config/config.go`

A single `Config` struct loaded entirely from environment variables. No config files, no flags (except `--dry-run`).

```go
type Config struct {
    // AlphaESS credentials
    AppID     string
    AppSecret string
    Serial    string

    // Off-peak window
    OffpeakStart time.Duration // hours+minutes from midnight
    OffpeakEnd   time.Duration
    Location     *time.Location

    // DynamoDB table names (empty in dry-run mode)
    TableReadings    string
    TableDailyEnergy string
    TableDailyPower  string
    TableSystem      string
    TableOffpeak     string

    // Runtime
    AWSRegion   string
    DryRun      bool
    HTTPTimeout time.Duration // 10s default
}
```

**Loading logic:**

1. Check `DRY_RUN` env var or `--dry-run` flag
2. Read all required env vars — in dry-run mode, skip AWS/DynamoDB vars
3. Parse `OFFPEAK_START` and `OFFPEAK_END` as HH:MM → `time.Duration` from midnight
4. Load timezone from `TZ` (default `Australia/Sydney`) via `time.LoadLocation`
5. Validate: offpeak start < end, timezone loads, all required vars present
6. Return `Config` or error

**Validation errors** are collected and reported together (not one-at-a-time), so a misconfigured deployment gets a single log entry listing all problems.

Traces to: [5.1]-[5.11], [12.1], [12.4]

### 3. AlphaESS Client — `internal/alphaess/client.go`

A stateless HTTP client. No connection pooling beyond Go's default `http.Transport`.

```go
type Client struct {
    baseURL    string
    appID      string
    appSecret  string
    httpClient *http.Client
}

func NewClient(appID, appSecret string, timeout time.Duration) *Client {
    return &Client{
        baseURL:   "https://openapi.alphaess.com/api",
        appID:     appID,
        appSecret: appSecret,
        httpClient: &http.Client{Timeout: timeout},
    }
}
```

**Authentication** — applied to every request:

```go
func (c *Client) sign() (timestamp string, signature string) {
    ts := strconv.FormatInt(time.Now().Unix(), 10)
    h := sha512.New()
    h.Write([]byte(c.appID + c.appSecret + ts))
    return ts, hex.EncodeToString(h.Sum(nil))
}
```

Headers set on each request: `appId`, `timeStamp`, `sign`, `Content-Type: application/json`.

**Endpoint methods:**

| Method | AlphaESS Endpoint | Params | Returns |
|--------|------------------|--------|---------|
| `GetLastPowerData(ctx, serial)` | `getLastPowerData` | `sysSn` | `*PowerData, error` |
| `GetOneDayPower(ctx, serial, date)` | `getOneDayPowerBySn` | `sysSn`, `queryDate` | `[]PowerSnapshot, error` |
| `GetOneDateEnergy(ctx, serial, date)` | `getOneDateEnergyBySn` | `sysSn`, `queryDate` | `*EnergyData, error` |
| `GetEssList(ctx)` | `getEssList` | — | `[]SystemInfo, error` |

Each method: constructs the request body as JSON → POST to the endpoint → reads response → unmarshals the API envelope → checks `code` field → unmarshals `data` field → returns typed result or error.

**Error handling:** The client treats these as errors: non-200 HTTP status, JSON parse failures, and API envelope responses where `code != 0` (e.g., `code: 6004` for rate limiting). All return a wrapped error. The caller (poller) logs and skips. No retries in the client itself.

**`GetEssList` filtering:** The API returns a list of systems. The client filters to the system matching the configured serial number. If no match is found, it returns an error.

Traces to: [2.1]-[2.9]

### 4. AlphaESS Models — `internal/alphaess/models.go`

API response types. The AlphaESS API wraps all responses in an envelope:

```go
// API envelope — all responses have this shape
type apiResponse struct {
    Code int             `json:"code"`
    Msg  string          `json:"msg"`
    Data json.RawMessage `json:"data"`
}

// getLastPowerData response
type PowerData struct {
    Ppv   float64 `json:"ppv"`
    Pload float64 `json:"pload"`
    Pbat  float64 `json:"pbat"`
    Pgrid float64 `json:"pgrid"`
    Soc   float64 `json:"soc"`
    // Per-phase fields included as received
}

// getOneDateEnergyBySn response
type EnergyData struct {
    Epv         float64 `json:"epv"`
    EInput      float64 `json:"eInput"`
    EOutput     float64 `json:"eOutput"`
    ECharge     float64 `json:"eCharge"`
    EDischarge  float64 `json:"eDischarge"`
    EGridCharge float64 `json:"eGridCharge"`
}

// getOneDayPowerBySn response — array of snapshots
type PowerSnapshot struct {
    Cbat       float64 `json:"cbat"`
    Ppv        float64 `json:"ppv"`
    Load       float64 `json:"load"`
    FeedIn     float64 `json:"feedIn"`
    GridCharge float64 `json:"gridCharge"`
    UploadTime string  `json:"uploadTime"`
}

// getEssList response
type SystemInfo struct {
    SysSn    string  `json:"sysSn"`
    Cobat    float64 `json:"cobat"`
    Mbat     string  `json:"mbat"`
    Minv     string  `json:"minv"`
    Popv     float64 `json:"popv"`
    Poinv    float64 `json:"poinv"`
    EmsStatus string `json:"emsStatus"`
}
```

Traces to: [2.4]-[2.7]

### 5. Store Interface — `internal/dynamo/store.go`

The store interface is the key abstraction for dry-run mode. Both implementations satisfy the same contract.

```go
type Store interface {
    WriteReading(ctx context.Context, item ReadingItem) error
    WriteDailyEnergy(ctx context.Context, item DailyEnergyItem) error
    WriteDailyPower(ctx context.Context, items []DailyPowerItem) error
    WriteSystem(ctx context.Context, item SystemItem) error
    WriteOffpeak(ctx context.Context, item OffpeakItem) error
    DeleteOffpeak(ctx context.Context, serial, date string) error
    GetOffpeak(ctx context.Context, serial, date string) (*OffpeakItem, error)
}
```

`DeleteOffpeak` removes a pending off-peak record when the end snapshot fails (satisfying [6.13]). `GetOffpeak` retrieves an existing off-peak record for mid-window startup recovery.

The health check does **not** use the `Store` interface. Instead, `runHealthCheck()` creates a standalone DynamoDB client and queries `flux-readings` directly with `ScanIndexForward: false` and `Limit: 1`. This avoids polluting the Store interface with a read method only used by a separate process invocation.

**DynamoStore** — production implementation:

```go
type DynamoStore struct {
    client       *dynamodb.Client
    tableNames   TableNames
}

type TableNames struct {
    Readings    string
    DailyEnergy string
    DailyPower  string
    System      string
    Offpeak     string
}
```

- `WriteReading`: `PutItem` with marshaled `ReadingItem`
- `WriteDailyEnergy`: `PutItem` with marshaled `DailyEnergyItem`
- `WriteDailyPower`: `BatchWriteItem` in chunks of 25 (DynamoDB limit). One retry for unprocessed items.
- `WriteSystem`: `PutItem` with marshaled `SystemItem`
- `WriteOffpeak`: `PutItem` with marshaled `OffpeakItem`

**LogStore** — dry-run implementation:

```go
type LogStore struct {
    logger *slog.Logger
}
```

Each write method logs the table name (or a placeholder label) and the item as a JSON-serialized slog attribute. `DeleteOffpeak` logs what would be deleted. `GetOffpeak` returns nil (no existing record), so the off-peak scheduler always starts fresh in dry-run mode.

Traces to: [4.1]-[4.11], [7.2]-[7.4], [12.2]-[12.5]

### 6. DynamoDB Models — `internal/dynamo/models.go`

DynamoDB item structs with `dynamodbav` struct tags. These are distinct from the AlphaESS API models — the poller transforms API responses into these storage types.

```go
// flux-readings table
type ReadingItem struct {
    SysSn     string  `dynamodbav:"sysSn"`
    Timestamp int64   `dynamodbav:"timestamp"`
    Ppv       float64 `dynamodbav:"ppv"`
    Pload     float64 `dynamodbav:"pload"`
    Pbat      float64 `dynamodbav:"pbat"`
    Pgrid     float64 `dynamodbav:"pgrid"`
    Soc       float64 `dynamodbav:"soc"`
    TTL       int64   `dynamodbav:"ttl"`
}

// flux-daily-energy table
type DailyEnergyItem struct {
    SysSn       string  `dynamodbav:"sysSn"`
    Date        string  `dynamodbav:"date"`
    Epv         float64 `dynamodbav:"epv"`
    EInput      float64 `dynamodbav:"eInput"`
    EOutput     float64 `dynamodbav:"eOutput"`
    ECharge     float64 `dynamodbav:"eCharge"`
    EDischarge  float64 `dynamodbav:"eDischarge"`
    EGridCharge float64 `dynamodbav:"eGridCharge"`
}

// flux-daily-power table
type DailyPowerItem struct {
    SysSn      string  `dynamodbav:"sysSn"`
    UploadTime string  `dynamodbav:"uploadTime"`
    Cbat       float64 `dynamodbav:"cbat"`
    Ppv        float64 `dynamodbav:"ppv"`
    Load       float64 `dynamodbav:"load"`
    FeedIn     float64 `dynamodbav:"feedIn"`
    GridCharge float64 `dynamodbav:"gridCharge"`
    TTL        int64   `dynamodbav:"ttl"`
}

// flux-system table
type SystemItem struct {
    SysSn       string  `dynamodbav:"sysSn"`
    Cobat       float64 `dynamodbav:"cobat"`
    Mbat        string  `dynamodbav:"mbat"`
    Minv        string  `dynamodbav:"minv"`
    Popv        float64 `dynamodbav:"popv"`
    Poinv       float64 `dynamodbav:"poinv"`
    EmsStatus   string  `dynamodbav:"emsStatus"`
    LastUpdated string  `dynamodbav:"lastUpdated"`
}

// flux-offpeak table
type OffpeakItem struct {
    SysSn    string  `dynamodbav:"sysSn"`
    Date     string  `dynamodbav:"date"`
    Status   string  `dynamodbav:"status"` // "pending" or "complete"

    // Start snapshot
    StartEpv         float64 `dynamodbav:"startEpv"`
    StartEInput      float64 `dynamodbav:"startEInput"`
    StartEOutput     float64 `dynamodbav:"startEOutput"`
    StartECharge     float64 `dynamodbav:"startECharge"`
    StartEDischarge  float64 `dynamodbav:"startEDischarge"`
    StartEGridCharge float64 `dynamodbav:"startEGridCharge"`
    SocStart         float64 `dynamodbav:"socStart"`

    // End snapshot
    EndEpv         float64 `dynamodbav:"endEpv"`
    EndEInput      float64 `dynamodbav:"endEInput"`
    EndEOutput     float64 `dynamodbav:"endEOutput"`
    EndECharge     float64 `dynamodbav:"endECharge"`
    EndEDischarge  float64 `dynamodbav:"endEDischarge"`
    EndEGridCharge float64 `dynamodbav:"endEGridCharge"`
    SocEnd         float64 `dynamodbav:"socEnd"`

    // Computed deltas
    GridUsageKwh         float64 `dynamodbav:"gridUsageKwh"`
    SolarKwh             float64 `dynamodbav:"solarKwh"`
    BatteryChargeKwh     float64 `dynamodbav:"batteryChargeKwh"`
    BatteryDischargeKwh  float64 `dynamodbav:"batteryDischargeKwh"`
    GridExportKwh        float64 `dynamodbav:"gridExportKwh"`
    BatteryDeltaPercent  float64 `dynamodbav:"batteryDeltaPercent"`
}
```

**Transformation functions** live alongside the models:

```go
func NewReadingItem(serial string, data *alphaess.PowerData, now time.Time) ReadingItem
func NewDailyEnergyItem(serial string, date string, data *alphaess.EnergyData) DailyEnergyItem
func NewDailyPowerItems(serial string, snapshots []alphaess.PowerSnapshot, now time.Time) []DailyPowerItem
func NewSystemItem(info *alphaess.SystemInfo, now time.Time) SystemItem
```

These functions handle TTL calculation (adding 30 days for readings/daily-power) and field mapping.

Traces to: [4.1]-[4.6]

### 7. Poller — `internal/poller/poller.go`

The orchestrator. Creates goroutines for each polling schedule and manages their lifecycle via context cancellation.

```go
type Poller struct {
    client  *alphaess.Client
    store   dynamo.Store
    cfg     *config.Config
    offpeak *OffpeakScheduler
}

func New(client *alphaess.Client, store dynamo.Store, cfg *config.Config) *Poller

func (p *Poller) Run(ctx context.Context) error
```

**`Run(ctx)` starts these goroutines:**

| Goroutine | Schedule | Action |
|-----------|----------|--------|
| `pollLiveData` | 10s ticker | `GetLastPowerData` → `WriteReading` |
| `pollDailyPower` | 1h ticker | `GetOneDayPower(today)` → `WriteDailyPower` |
| `pollDailyEnergy` | 6h ticker | `GetOneDateEnergy(today)` → `WriteDailyEnergy` |
| `pollSystemInfo` | 24h ticker | `GetEssList` → `WriteSystem` |
| `offpeak.Run` | Time-of-day triggers | See off-peak scheduler below |
| `midnightFinalizer` | Daily after midnight | `GetOneDateEnergy(yesterday)` → `WriteDailyEnergy` |

The 6-hour energy ticker ensures today's energy totals are visible on the dashboard between off-peak window times and midnight. The off-peak scheduler captures energy at window boundaries, and the midnight finalizer captures final totals — but without the 6-hour ticker, the dashboard would show stale energy data for up to 6 hours during the day.

**Dry-run API response logging:** In dry-run mode, each `fetchAndStore*` method logs the raw API response payload at info level before passing it to the store. This happens in the poller layer (not the store) because it logs the API response, not the transformed DynamoDB item. Both logs are emitted: the API response (poller layer) and the would-be DynamoDB item (LogStore).

Each goroutine follows the same pattern:

```go
func (p *Poller) pollLiveData(ctx context.Context, wg *sync.WaitGroup) {
    defer wg.Done()
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    // Immediate first poll
    p.fetchAndStoreLiveData(ctx)

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            p.fetchAndStoreLiveData(ctx)
        }
    }
}
```

**Graceful shutdown** uses a two-context pattern:

- `loopCtx` — cancelled by SIGTERM/SIGINT, stops goroutine ticker loops
- `drainCtx` — 25-second deadline starting when `loopCtx` is cancelled, used for in-flight API calls and DynamoDB writes so they can complete

Each goroutine's ticker loop selects on `loopCtx.Done()`, but passes `drainCtx` to the actual API/DynamoDB calls. When SIGTERM arrives, the loop stops scheduling new work, but any in-flight call finishes with its own timeout.

```go
func (p *Poller) Run(ctx context.Context) error {
    // drainCtx allows in-flight operations to complete after loop stops
    drainCtx, drainCancel := context.WithCancel(context.Background())
    defer drainCancel()

    var wg sync.WaitGroup
    wg.Add(6)
    go p.pollLiveData(ctx, drainCtx, &wg)
    go p.pollDailyPower(ctx, drainCtx, &wg)
    go p.pollDailyEnergy(ctx, drainCtx, &wg)
    go p.pollSystemInfo(ctx, drainCtx, &wg)
    go p.offpeak.Run(ctx, drainCtx, &wg)
    go p.midnightFinalizer(ctx, drainCtx, &wg)

    // Block until the loop context is cancelled (SIGTERM/SIGINT)
    <-ctx.Done()
    slog.Info("poller stopping")

    // Give goroutines 25 seconds to drain, then force exit
    done := make(chan struct{})
    go func() { wg.Wait(); close(done) }()

    select {
    case <-done:
        return nil
    case <-time.After(25 * time.Second):
        drainCancel() // cancel any still-running operations
        return fmt.Errorf("shutdown timed out after 25 seconds")
    }
}
```

Each goroutine selects on the loop context for scheduling, but passes the drain context to API/store calls:

```go
func (p *Poller) pollLiveData(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
    defer wg.Done()
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    p.fetchAndStoreLiveData(drainCtx) // immediate first poll

    for {
        select {
        case <-loopCtx.Done():
            return
        case <-ticker.C:
            p.fetchAndStoreLiveData(drainCtx)
        }
    }
}
```

**Midnight finalizer** — waits until the first midnight in the configured timezone, then runs once and repeats daily:

```go
func (p *Poller) midnightFinalizer(ctx context.Context, wg *sync.WaitGroup) {
    defer wg.Done()
    for {
        nextMidnight := nextLocalMidnight(p.cfg.Location)
        delay := time.Until(nextMidnight) + 5*time.Minute // 00:05 local time

        select {
        case <-ctx.Done():
            return
        case <-time.After(delay):
            yesterday := time.Now().In(p.cfg.Location).AddDate(0, 0, -1).Format("2006-01-02")
            p.fetchAndStoreEnergy(ctx, yesterday)
        }
    }
}
```

Traces to: [3.1]-[3.7], [8.1]-[8.5]

### 8. Off-Peak Scheduler — `internal/poller/offpeak.go`

Manages the off-peak window state machine. Responsible for scheduling API calls at window boundaries and computing deltas.

```go
type OffpeakScheduler struct {
    client *alphaess.Client
    store  dynamo.Store
    cfg    *config.Config

    // In-memory state for the current day's off-peak calculation
    startSnapshot *alphaess.EnergyData
    socStart      float64
    hasStart      bool
}
```

**State machine:**

```
                    ┌──────────────┐
         startup    │  Determine   │
        ────────────│  current     │
                    │  position    │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
              ▼            ▼            ▼
        Before start   During window   After end
        (wait for      (wait for       (skip today,
         start time)    end time)       wait tomorrow)
              │            │
              ▼            ▼
        ┌──────────┐ ┌──────────┐
        │ Capture  │ │ Capture  │
        │ start    │ │ end      │
        │ snapshot │ │ snapshot │
        └────┬─────┘ └────┬─────┘
             │             │
             ▼             ▼
        Wait for end  Compute deltas
                      Write offpeak record
                      Wait for tomorrow
```

**Snapshot capture with retry** (per Decision 7):

```go
func (o *OffpeakScheduler) captureSnapshot(ctx context.Context, date string) (*alphaess.EnergyData, float64, error) {
    var lastErr error
    for attempt := 0; attempt < 3; attempt++ {
        if attempt > 0 {
            select {
            case <-ctx.Done():
                return nil, 0, ctx.Err()
            case <-time.After(10 * time.Second):
            }
        }

        energy, err := o.client.GetOneDateEnergy(ctx, o.cfg.Serial, date)
        if err != nil {
            lastErr = err
            continue
        }

        power, err := o.client.GetLastPowerData(ctx, o.cfg.Serial)
        if err != nil {
            lastErr = err
            continue
        }

        return energy, power.Soc, nil
    }
    return nil, 0, fmt.Errorf("off-peak snapshot failed after 3 attempts: %w", lastErr)
}
```

**Delta computation:**

```go
func computeOffpeakDeltas(serial, date string, start, end *alphaess.EnergyData, socStart, socEnd float64) dynamo.OffpeakItem {
    return dynamo.OffpeakItem{
        SysSn:               serial,
        Date:                date,
        // Start snapshot fields...
        // End snapshot fields...
        GridUsageKwh:        end.EInput - start.EInput,
        SolarKwh:            end.Epv - start.Epv,
        BatteryChargeKwh:    end.ECharge - start.ECharge,
        BatteryDischargeKwh: end.EDischarge - start.EDischarge,
        GridExportKwh:       end.EOutput - start.EOutput,
        BatteryDeltaPercent: socEnd - socStart,
    }
}
```

**Start snapshot persistence:**

The `OffpeakItem` includes a `Status` field (`dynamodbav:"status"`) with two values: `"pending"` and `"complete"`.

At start time, the scheduler writes a partial `OffpeakItem` with `Status: "pending"`, the start energy fields, and `socStart`. At end time, the full record (start + end + deltas, `Status: "complete"`) overwrites it.

If the end snapshot fails after retries, the scheduler deletes the pending record via `DeleteOffpeak(ctx, serial, date)` to satisfy requirement [6.13] (no partial records). The Lambda API filters on `Status == "complete"` as a safety net.

On startup, the off-peak scheduler queries DynamoDB for today's off-peak record. If a `"pending"` record exists, it loads the start snapshot from it and waits for end only.

**Edge cases:**
- Poller starts mid-window: Query DynamoDB for today's off-peak record. If a pending record exists, recover start snapshot and wait for end. If no record exists, skip today. If a complete record exists, skip today (already done).
- Poller starts after window: Skip today, schedule for tomorrow's start.
- DynamoDB query fails on startup recovery: Log warning, skip today's off-peak window (treat as "no record found").

Traces to: [6.1]-[6.14]

### 9. Logging

All logging uses `log/slog` with JSON output, configured once in `main.go`. A `ReplaceAttr` function renames `time` → `timestamp` and lowercases level values to match the requirements:

```go
handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
    ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
        if a.Key == slog.TimeKey {
            a.Key = "timestamp"
        }
        if a.Key == slog.LevelKey {
            a.Value = slog.StringValue(strings.ToLower(a.Value.String()))
        }
        return a
    },
})
slog.SetDefault(slog.New(handler))
```

**Log patterns by component:**

| Context | Fields | Example |
|---------|--------|---------|
| API call success | `endpoint`, `status` | `{"level":"INFO","msg":"api call","endpoint":"getLastPowerData","status":200}` |
| API call failure | `endpoint`, `error` | `{"level":"ERROR","msg":"api call failed","endpoint":"getLastPowerData","error":"timeout"}` |
| DynamoDB write | `table`, `pk`, `sk` | `{"level":"INFO","msg":"wrote item","table":"flux-readings","pk":"AB12...","sk":"1713052020"}` |
| DynamoDB error | `table`, `pk`, `sk`, `error` | `{"level":"ERROR","msg":"write failed","table":"flux-readings","pk":"AB12...","sk":"1713052020","error":"..."}` |
| Startup | `serial`, `offpeak`, `tz`, `dry_run` | `{"level":"INFO","msg":"poller starting","serial":"AB...","offpeak":"11:00-14:00","tz":"Australia/Sydney"}` |
| Shutdown | — | `{"level":"INFO","msg":"poller stopping"}` |
| Off-peak snapshot | `type`, `date` | `{"level":"INFO","msg":"off-peak snapshot","type":"start","date":"2026-04-13"}` |
| Dry-run write | `table`, `item` | `{"level":"INFO","msg":"dry-run write","table":"flux-readings","item":{...}}` |

**Secret safety:** The `Config` struct does not implement `fmt.Stringer` or `slog.LogValuer` and is never passed directly to a logger. Only individual non-secret fields are logged.

Traces to: [9.1]-[9.6], [12.2], [12.3]

---

## Data Models

### API → DynamoDB Transformation

```
AlphaESS API                    DynamoDB Table
─────────────                   ──────────────
getLastPowerData    ──────────► flux-readings
  PowerData                       ReadingItem
  + timestamp (now)               + TTL (now + 30d)

getOneDateEnergyBySn ─────────► flux-daily-energy
  EnergyData                      DailyEnergyItem
  + date param                    + date as sort key

getOneDayPowerBySn  ──────────► flux-daily-power
  []PowerSnapshot                 []DailyPowerItem (batch)
  + uploadTime from API           + TTL (now + 30d)

getEssList          ──────────► flux-system
  SystemInfo                      SystemItem
  + lastUpdated (now)

off-peak computation ─────────► flux-offpeak
  start + end EnergyData          OffpeakItem
  + SOC at boundaries             + computed deltas
```

### TTL Calculation

```go
const (
    readingsTTL    = 30 * 24 * time.Hour
    dailyPowerTTL  = 30 * 24 * time.Hour
)

func ttlFromNow(d time.Duration) int64 {
    return time.Now().Add(d).Unix()
}
```

---

## Error Handling

### Strategy by Component

| Component | Failure | Behaviour | Rationale |
|-----------|---------|-----------|-----------|
| Config loading | Missing/invalid env var | Log error, exit(1) | Fail fast — can't operate without config |
| AlphaESS API call | HTTP error, timeout, non-200 | Log error, skip to next poll | Decision 1: natural retry via next tick |
| AlphaESS API call | JSON parse error | Log error, skip to next poll | Malformed response treated same as error |
| DynamoDB PutItem | Write error | Log error, continue polling | Data loss for one item; next poll overwrites |
| DynamoDB BatchWriteItem | Partial failure (unprocessed) | Retry unprocessed once, then log | Decision: one retry for batch partial failures |
| Off-peak snapshot | API error (3 retries exhausted) | Log warning, skip day's off-peak record | Decision 7: retry 3x, then give up |
| Off-peak end | API error after start succeeded | Log warning, do not write partial record | Decision 7: incomplete data is worse than none |
| Health check | DynamoDB query error | Exit 1 (unhealthy) | Conservative: if we can't verify, report unhealthy |
| Signal received | SIGTERM/SIGINT | Cancel context, wait 30s, exit | Decision: graceful drain of in-flight work |

### Error Wrapping

All errors are wrapped with context using `fmt.Errorf("...: %w", err)`. The poller layer adds the endpoint or table name. Errors never bubble up past the goroutine boundary — each goroutine handles its own errors by logging them.

---

## Testing Strategy

### Unit Tests

**`internal/alphaess/`**

- **Auth signing** — verify SHA-512 digest matches a known input/output pair. Verify headers are set correctly on a request. ([2.1], [2.2])
- **Response parsing** — table-driven tests with sample JSON responses for each endpoint. Include malformed JSON and error envelopes. ([2.4]-[2.7], [2.9])
- **HTTP client** — use `httptest.NewServer` to simulate AlphaESS API responses. Test timeout handling, non-200 status codes. ([2.8], [2.9])

**`internal/config/`**

- **Valid config** — set all env vars, verify `Config` fields. ([5.1]-[5.8])
- **Missing vars** — unset each required var, verify error message names the variable. ([5.9])
- **Invalid values** — malformed HH:MM, invalid timezone, start >= end. Table-driven. ([5.10], [5.11])
- **Dry-run relaxation** — verify DynamoDB vars are not required when `DRY_RUN=true`. ([12.4])

**`internal/dynamo/`**

- **Model transformation** — verify `NewReadingItem` etc. correctly map API fields to DynamoDB fields, compute TTLs. Table-driven with edge cases (zero values, negative power). ([4.1]-[4.6])
- **LogStore** — verify dry-run store logs the expected table name and item attributes. ([12.2], [12.3])
- **BatchWriteItem chunking** — verify items are split into groups of 25. ([4.4])

**`internal/poller/`**

- **Off-peak delta computation** — table-driven: given start and end snapshots, verify all delta calculations. ([6.6], [6.7])
- **Off-peak time determination** — verify the scheduler correctly identifies whether the current time is before, during, or after the off-peak window. Test DST transition dates. ([6.1], [6.10], [6.11])
- **Midnight finalizer timing** — verify `nextLocalMidnight` returns the correct time across DST boundaries. ([3.7])

### Integration Tests

- **DynamoDB round-trip** — test against DynamoDB Local (optional, not blocking CI). PutItem + GetItem for each table to verify schema compatibility.
- **Full poller cycle** — start the poller with a mock HTTP server (replacing AlphaESS) and DynamoDB Local, run for 15 seconds, verify items appear in all tables.

Integration tests are gated behind `INTEGRATION=1` environment variable (following strata's pattern).

### What's Not Tested

- The actual AlphaESS API (no test credentials available — dry-run mode serves as the manual verification path)
- ECS health check integration (verified during deployment)
- CloudFormation template changes (validated by `aws cloudformation validate-template`)

---

## Dockerfile

Multi-stage build targeting `linux/arm64`:

```dockerfile
# Builder stage
FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
    -trimpath -ldflags="-s -w" \
    -o /poller ./cmd/poller

# Runtime stage
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /poller /poller
ENTRYPOINT ["/poller"]
```

**Build flags:** `-trimpath` prevents build paths leaking in stack traces. `-ldflags="-s -w"` strips debug symbols, reducing binary size by ~30%.

**`.dockerignore`:** The repository includes a `.dockerignore` excluding `.git/`, `specs/`, `docs/`, `infrastructure/`, and other non-build files to keep the Docker context small.

**Base image choice:** `gcr.io/distroless/static` provides CA certificates and IANA timezone data without a shell. The `:nonroot` tag runs as UID 65534 by default. The binary also embeds `time/tzdata` as a fallback (via an import in `main.go`).

**Why not scratch:** `scratch` has no CA certs and no timezone data. While both can be copied manually, `distroless/static` provides them without Dockerfile complexity and is still only ~2MB.

Traces to: [10.1]-[10.6]

---

## GitHub Actions CI

```yaml
name: Poller CI
on:
  push:
    branches: [main]
    paths:
      - 'cmd/poller/**'
      - 'internal/**'
      - 'go.mod'
      - 'go.sum'
      - 'Dockerfile'

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - run: go vet ./...
      - run: go test ./...

      - name: Short SHA
        id: short-sha
        run: echo "sha=${GITHUB_SHA::7}" >> "$GITHUB_OUTPUT"

      - uses: docker/setup-buildx-action@v3

      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - uses: docker/build-push-action@v6
        with:
          push: true
          platforms: linux/arm64
          tags: |
            ghcr.io/${{ github.repository_owner }}/flux-poller:latest
            ghcr.io/${{ github.repository_owner }}/flux-poller:sha-${{ steps.short-sha.outputs.sha }}
```

Uses `docker/build-push-action` with buildx for cross-platform ARM64 builds on the default AMD64 runner (via QEMU emulation).

Traces to: [11.1]-[11.5]

---

## Infrastructure Update

Add to the `ContainerDefinitions[0].Environment` section in `infrastructure/template.yaml`:

```yaml
- Name: TABLE_READINGS
  Value: !Ref ReadingsTable
- Name: TABLE_DAILY_ENERGY
  Value: !Ref DailyEnergyTable
- Name: TABLE_DAILY_POWER
  Value: !Ref DailyPowerTable
- Name: TABLE_SYSTEM
  Value: !Ref SystemTable
- Name: TABLE_OFFPEAK
  Value: !Ref OffpeakTable
- Name: TZ
  Value: Australia/Sydney
```

This is the only change to the CloudFormation template. No new resources or IAM changes needed — the existing `TaskRole` already grants DynamoDB access to all five tables.

Traces to: [13.1]-[13.3]

---

## Makefile

Following the conventions from the strata project, with targets adapted for this service:

| Target | Command | Purpose |
|--------|---------|---------|
| `build` | `CGO_ENABLED=0 go build -o bin/poller ./cmd/poller` | Build the poller binary locally |
| `test` | `go test ./...` | Run all unit tests |
| `fmt` | `go fmt ./...` | Format Go code |
| `vet` | `go vet ./...` | Static analysis |
| `lint` | `golangci-lint run` | Run linter |
| `modernize` | `go mod tidy -compat && go fix ./...` | Tidy deps and apply Go fixes |
| `check` | `fmt vet lint test` (composite) | Full validation suite |
| `docker-build` | `docker buildx build --platform linux/arm64 -t flux-poller .` | Build ARM64 container image locally |
| `deps-tidy` | `go mod tidy` | Clean up go.mod and go.sum |
| `deps-update` | `go get -u ./... && go mod tidy` | Update all dependencies |

Traces to: [1.6]

---

## Dependencies

```
github.com/aws/aws-sdk-go-v2                  # Core SDK
github.com/aws/aws-sdk-go-v2/config            # SDK config loading
github.com/aws/aws-sdk-go-v2/service/dynamodb   # DynamoDB client
github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue  # Struct marshaling
```

No other external dependencies. All other functionality (HTTP, JSON, SHA-512, signals, logging, time) uses the Go standard library.

Dev dependencies:
```
github.com/stretchr/testify                    # Test assertions (following strata convention)
```
