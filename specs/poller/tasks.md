---
references:
    - specs/poller/requirements.md
    - specs/poller/design.md
    - specs/poller/decision_log.md
---
# Flux Poller

## Project Setup

- [x] 1. Initialize Go module and project structure <!-- id:vqdz1ig -->
  - Create go.mod at repo root with module path and Go 1.26
  - Create directory structure: cmd/poller/, internal/alphaess/, internal/dynamo/, internal/poller/, internal/config/
  - Create Makefile with targets: build, test, fmt, vet, lint, modernize, check, docker-build, deps-tidy, deps-update
  - Create .dockerignore excluding .git/, specs/, docs/, infrastructure/, .github/
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6)
  - References: go.mod, Makefile, .dockerignore

## Configuration

- [x] 2. Write tests for config loading and validation <!-- id:vqdz1ih -->
  - Test valid config with all env vars set
  - Test missing required vars (each individually) returns error naming the variable
  - Test malformed OFFPEAK_START/END (invalid HH:MM, start >= end)
  - Test invalid TZ value
  - Test DRY_RUN=true relaxes AWS/DynamoDB var requirements
  - Test default timezone (Australia/Sydney) when TZ unset
  - Table-driven tests following strata conventions
  - Blocked-by: vqdz1ig (Initialize Go module and project structure)
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8), [5.9](requirements.md#5.9), [5.10](requirements.md#5.10), [5.11](requirements.md#5.11), [12.4](requirements.md#12.4)
  - References: internal/config/config_test.go

- [x] 3. Implement config loading and validation <!-- id:vqdz1ii -->
  - Config struct with all fields per design
  - Load from env vars, parse HH:MM to time.Duration
  - time.LoadLocation for timezone with Australia/Sydney default
  - Collect all validation errors and report together
  - DRY_RUN flag skips AWS/DynamoDB var requirements
  - Blocked-by: vqdz1ih (Write tests for config loading and validation)
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8), [5.9](requirements.md#5.9), [5.10](requirements.md#5.10), [5.11](requirements.md#5.11), [12.1](requirements.md#12.1), [12.4](requirements.md#12.4)
  - References: internal/config/config.go

## AlphaESS API Client

- [x] 4. Create AlphaESS API response models <!-- id:vqdz1ij -->
  - apiResponse envelope struct with Code, Msg, Data (json.RawMessage)
  - PowerData, EnergyData, PowerSnapshot, SystemInfo structs with json tags
  - All fields per design section 4
  - Blocked-by: vqdz1ig (Initialize Go module and project structure)
  - Stream: 2
  - Requirements: [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7)
  - References: internal/alphaess/models.go

- [x] 5. Write tests for AlphaESS client auth and HTTP handling <!-- id:vqdz1ik -->
  - Test SHA-512 signing produces correct digest for known inputs
  - Test auth headers (appId, timeStamp, sign) are set on requests
  - Test successful response parsing for each endpoint
  - Test non-200 HTTP status returns error with endpoint name
  - Test API envelope code != 0 returns error
  - Test malformed JSON response returns error
  - Test HTTP timeout handling
  - Test GetEssList filters to configured serial number
  - Use httptest.NewServer to mock AlphaESS API
  - Blocked-by: vqdz1ij (Create AlphaESS API response models)
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.8](requirements.md#2.8), [2.9](requirements.md#2.9)
  - References: internal/alphaess/client_test.go

- [x] 6. Implement AlphaESS client <!-- id:vqdz1il -->
  - Client struct with baseURL, appID, appSecret, httpClient
  - sign() method: SHA-512 of appID+appSecret+timestamp
  - Private doRequest helper: build request, set auth headers, POST, check HTTP status, unmarshal envelope, check code field
  - GetLastPowerData, GetOneDayPower, GetOneDateEnergy, GetEssList methods
  - GetEssList filters list to matching serial, returns error if not found
  - 10-second HTTP client timeout
  - All errors wrapped with fmt.Errorf and endpoint name
  - Blocked-by: vqdz1ik (Write tests for AlphaESS client auth and HTTP handling)
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7), [2.8](requirements.md#2.8), [2.9](requirements.md#2.9)
  - References: internal/alphaess/client.go

## DynamoDB Layer

- [x] 7. Create DynamoDB models, Store interface, and transformation functions <!-- id:vqdz1im -->
  - ReadingItem, DailyEnergyItem, DailyPowerItem, SystemItem, OffpeakItem structs with dynamodbav tags
  - OffpeakItem includes Status field (pending/complete)
  - Store interface with 7 methods: WriteReading, WriteDailyEnergy, WriteDailyPower, WriteSystem, WriteOffpeak, DeleteOffpeak, GetOffpeak
  - TableNames struct
  - NewReadingItem, NewDailyEnergyItem, NewDailyPowerItems, NewSystemItem transformation functions
  - Blocked-by: vqdz1ig (Initialize Go module and project structure), vqdz1ij (Create AlphaESS API response models)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6), [4.7](requirements.md#4.7), [4.10](requirements.md#4.10)
  - References: internal/dynamo/models.go, internal/dynamo/store.go

- [x] 8. Write tests for DynamoDB model transformations <!-- id:vqdz1in -->
  - Test NewReadingItem maps API fields correctly and computes 30-day TTL
  - Test NewDailyEnergyItem maps fields and sets date as sort key
  - Test NewDailyPowerItems maps all snapshots and computes 30-day TTL
  - Test NewSystemItem maps fields and sets lastUpdated
  - Table-driven with edge cases: zero values, negative power values
  - Blocked-by: vqdz1im (Create DynamoDB models, Store interface, and transformation functions)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6)
  - References: internal/dynamo/models_test.go

- [x] 9. Implement DynamoStore <!-- id:vqdz1io -->
  - DynamoStore struct with dynamodb.Client and TableNames
  - WriteReading: PutItem with MarshalMap
  - WriteDailyEnergy: PutItem with MarshalMap
  - WriteDailyPower: BatchWriteItem in chunks of 25, one retry for unprocessed items
  - WriteSystem: PutItem with MarshalMap
  - WriteOffpeak: PutItem with MarshalMap
  - DeleteOffpeak: DeleteItem by sysSn+date
  - GetOffpeak: GetItem by sysSn+date, return nil if not found
  - All errors wrapped with table name and item key context
  - Blocked-by: vqdz1im (Create DynamoDB models, Store interface, and transformation functions), vqdz1ii (Implement config loading and validation)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.6](requirements.md#4.6), [4.7](requirements.md#4.7), [4.8](requirements.md#4.8), [4.9](requirements.md#4.9), [4.10](requirements.md#4.10), [4.11](requirements.md#4.11)
  - References: internal/dynamo/store.go

- [x] 10. Write tests for LogStore (dry-run) <!-- id:vqdz1ip -->
  - Test each write method logs table name and item attributes
  - Test DeleteOffpeak logs what would be deleted
  - Test GetOffpeak returns nil
  - Verify log output contains expected JSON fields
  - Blocked-by: vqdz1im (Create DynamoDB models, Store interface, and transformation functions)
  - Stream: 1
  - Requirements: [12.2](requirements.md#12.2), [12.3](requirements.md#12.3)
  - References: internal/dynamo/logstore_test.go

- [x] 11. Implement LogStore <!-- id:vqdz1iq -->
  - LogStore struct with slog.Logger
  - Each write method logs table name and JSON-serialized item
  - DeleteOffpeak logs table and key that would be deleted
  - GetOffpeak returns nil, nil (no record in dry-run)
  - Blocked-by: vqdz1ip (Write tests for LogStore (dry-run))
  - Stream: 1
  - Requirements: [12.2](requirements.md#12.2), [12.3](requirements.md#12.3), [12.5](requirements.md#12.5)
  - References: internal/dynamo/logstore.go

## Poller Orchestrator

- [x] 12. Write tests for poller polling goroutines <!-- id:vqdz1ir -->
  - Test pollLiveData calls GetLastPowerData and WriteReading
  - Test pollDailyPower calls GetOneDayPower with todays date and WriteDailyPower
  - Test pollDailyEnergy calls GetOneDateEnergy with todays date and WriteDailyEnergy
  - Test pollSystemInfo calls GetEssList and WriteSystem
  - Test immediate first poll on startup (no waiting for first tick)
  - Test API error is logged and polling continues
  - Test DynamoDB error is logged and polling continues
  - Test dry-run mode logs API response payloads
  - Use httptest.NewServer for AlphaESS mock and LogStore for DynamoDB
  - Blocked-by: vqdz1il (Implement AlphaESS client), vqdz1io (Implement DynamoStore), vqdz1iq (Implement LogStore)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [2.9](requirements.md#2.9), [4.9](requirements.md#4.9), [12.2](requirements.md#12.2)
  - References: internal/poller/poller_test.go

- [x] 13. Implement poller orchestrator <!-- id:vqdz1is -->
  - Poller struct with Client, Store, Config, OffpeakScheduler
  - Run(ctx) with two-context pattern: loopCtx for tickers, drainCtx for in-flight ops
  - pollLiveData: 10s ticker goroutine
  - pollDailyPower: 1h ticker goroutine, passes todays date in configured timezone
  - pollDailyEnergy: 6h ticker goroutine, passes todays date in configured timezone
  - pollSystemInfo: 24h ticker goroutine
  - fetchAndStore* helper methods that call API, transform, write to store
  - In dry-run mode, log raw API response payloads before transformation
  - Graceful shutdown: wait for ctx.Done(), then 25s drain timeout with WaitGroup
  - Blocked-by: vqdz1ir (Write tests for poller polling goroutines)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [8.2](requirements.md#8.2), [8.3](requirements.md#8.3), [9.3](requirements.md#9.3), [9.4](requirements.md#9.4), [12.2](requirements.md#12.2)
  - References: internal/poller/poller.go

- [x] 14. Write tests for off-peak scheduler <!-- id:vqdz1it -->
  - Test off-peak delta computation: given start and end snapshots, verify all 6 deltas
  - Test batteryDeltaPercent = socEnd - socStart
  - Test time position detection: before window, during window, after window
  - Test DST transition dates for correct window boundaries
  - Test snapshot retry: fail twice then succeed on third attempt
  - Test all 3 retries fail: no off-peak record written
  - Test start succeeds but end fails: pending record deleted
  - Test mid-window startup recovery: finds pending record in store
  - Test mid-window startup with no existing record: skips today
  - Test post-window startup: skips today
  - Blocked-by: vqdz1im (Create DynamoDB models, Store interface, and transformation functions), vqdz1il (Implement AlphaESS client)
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [6.5](requirements.md#6.5), [6.6](requirements.md#6.6), [6.7](requirements.md#6.7), [6.10](requirements.md#6.10), [6.11](requirements.md#6.11), [6.12](requirements.md#6.12), [6.13](requirements.md#6.13), [6.14](requirements.md#6.14)
  - References: internal/poller/offpeak_test.go

- [x] 15. Implement off-peak scheduler <!-- id:vqdz1iu -->
  - OffpeakScheduler struct with Client, Store, Config, in-memory start state
  - Run method: determine position (before/during/after), schedule accordingly
  - captureSnapshot: call GetOneDateEnergy + GetLastPowerData with 3-attempt retry
  - At start time: capture snapshot, write pending OffpeakItem to store, set in-memory state
  - At end time: capture snapshot, compute deltas, write complete OffpeakItem
  - If end fails: delete pending record, log warning, skip day
  - computeOffpeakDeltas: compute all 6 energy deltas + batteryDeltaPercent
  - On startup mid-window: GetOffpeak to check for existing pending record
  - Use time.Date for wall-clock scheduling (DST-safe)
  - Blocked-by: vqdz1it (Write tests for off-peak scheduler)
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [6.5](requirements.md#6.5), [6.6](requirements.md#6.6), [6.7](requirements.md#6.7), [6.8](requirements.md#6.8), [6.9](requirements.md#6.9), [6.10](requirements.md#6.10), [6.11](requirements.md#6.11), [6.12](requirements.md#6.12), [6.13](requirements.md#6.13), [6.14](requirements.md#6.14)
  - References: internal/poller/offpeak.go

- [x] 16. Write tests for midnight energy finalizer <!-- id:vqdz1iv -->
  - Test nextLocalMidnight returns correct time from various times of day
  - Test nextLocalMidnight across DST transition boundaries
  - Test finalizer calls GetOneDateEnergy with yesterdays date after midnight
  - Blocked-by: vqdz1im (Create DynamoDB models, Store interface, and transformation functions), vqdz1il (Implement AlphaESS client)
  - Stream: 1
  - Requirements: [3.7](requirements.md#3.7)
  - References: internal/poller/poller_test.go

- [x] 17. Implement midnight energy finalizer <!-- id:vqdz1iw -->
  - midnightFinalizer goroutine in poller
  - nextLocalMidnight helper using time.Date for DST safety
  - Sleep until midnight + 5 minutes, call GetOneDateEnergy(yesterday), write to store
  - Loop daily, respecting context cancellation
  - Blocked-by: vqdz1iv (Write tests for midnight energy finalizer), vqdz1is (Implement poller orchestrator)
  - Stream: 1
  - Requirements: [3.7](requirements.md#3.7)
  - References: internal/poller/poller.go

## Entrypoint and Health Check

- [ ] 18. Write tests for health check <!-- id:vqdz1ix -->
  - Test health check returns 0 when recent reading exists (<60s old)
  - Test health check returns 1 when reading is stale (>60s)
  - Test health check returns 1 when no reading exists
  - Test health check returns 0 in dry-run mode without DynamoDB
  - Blocked-by: vqdz1io (Implement DynamoStore)
  - Stream: 1
  - Requirements: [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3), [7.4](requirements.md#7.4), [7.5](requirements.md#7.5), [12.5](requirements.md#12.5)
  - References: cmd/poller/healthcheck_test.go

- [ ] 19. Implement entrypoint and health check <!-- id:vqdz1iy -->
  - main.go: os.Args dispatch for healthcheck subcommand
  - main.go: slog JSON handler with ReplaceAttr (time->timestamp, lowercase levels)
  - main.go: config.Load, create AlphaESS client, create store (DynamoStore or LogStore)
  - main.go: signal.NotifyContext for SIGTERM/SIGINT
  - main.go: poller.New + Run, log startup/shutdown messages
  - runHealthCheck: check DRY_RUN env var first, then query DynamoDB for latest reading
  - Embed time/tzdata via blank import
  - Blocked-by: vqdz1ix (Write tests for health check), vqdz1is (Implement poller orchestrator), vqdz1iu (Implement off-peak scheduler), vqdz1iw (Implement midnight energy finalizer), vqdz1ii (Implement config loading and validation)
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3), [7.4](requirements.md#7.4), [7.5](requirements.md#7.5), [8.1](requirements.md#8.1), [8.4](requirements.md#8.4), [8.5](requirements.md#8.5), [9.1](requirements.md#9.1), [9.2](requirements.md#9.2), [9.6](requirements.md#9.6), [12.1](requirements.md#12.1), [12.5](requirements.md#12.5), [12.6](requirements.md#12.6)
  - References: cmd/poller/main.go

## Logging Setup

- [ ] 20. Configure slog JSON handler with field name customization <!-- id:vqdz1iz -->
  - This is wired into main.go but the slog setup can be extracted to a helper
  - ReplaceAttr: rename time -> timestamp, lowercase level values
  - Verify secret safety: Config struct never logged directly
  - Blocked-by: vqdz1ig (Initialize Go module and project structure)
  - Stream: 1
  - Requirements: [9.1](requirements.md#9.1), [9.2](requirements.md#9.2), [9.5](requirements.md#9.5), [9.6](requirements.md#9.6)
  - References: cmd/poller/main.go

## Docker and CI

- [ ] 21. Create Dockerfile <!-- id:vqdz1j0 -->
  - Multi-stage build: golang:1.26-alpine builder, gcr.io/distroless/static:nonroot runtime
  - Build with CGO_ENABLED=0 GOOS=linux GOARCH=arm64 -trimpath -ldflags=-s -w
  - Binary placed at /poller
  - Distroless provides CA certs and timezone data
  - Blocked-by: vqdz1ig (Initialize Go module and project structure)
  - Stream: 1
  - Requirements: [10.1](requirements.md#10.1), [10.2](requirements.md#10.2), [10.3](requirements.md#10.3), [10.4](requirements.md#10.4), [10.5](requirements.md#10.5), [10.6](requirements.md#10.6)
  - References: Dockerfile

- [x] 22. Create GitHub Actions CI workflow <!-- id:vqdz1j1 -->
  - Trigger on push to main when poller source, Dockerfile, or go.mod/sum change
  - Setup Go 1.26, run go vet and go test
  - Short SHA extraction step with id: short-sha
  - docker/setup-buildx-action + docker/login-action for GHCR
  - docker/build-push-action for linux/arm64 with latest + sha-{short} tags
  - Blocked-by: vqdz1ig (Initialize Go module and project structure)
  - Stream: 2
  - Requirements: [11.1](requirements.md#11.1), [11.2](requirements.md#11.2), [11.3](requirements.md#11.3), [11.4](requirements.md#11.4), [11.5](requirements.md#11.5)
  - References: .github/workflows/poller.yml

## Infrastructure Update

- [x] 23. Update CloudFormation template with poller env vars <!-- id:vqdz1j2 -->
  - Add TABLE_READINGS, TABLE_DAILY_ENERGY, TABLE_DAILY_POWER, TABLE_SYSTEM, TABLE_OFFPEAK environment variables to container definition
  - Values reference CloudFormation table resources via !Ref
  - Add TZ=Australia/Sydney environment variable
  - Blocked-by: vqdz1ig (Initialize Go module and project structure)
  - Stream: 2
  - Requirements: [13.1](requirements.md#13.1), [13.2](requirements.md#13.2), [13.3](requirements.md#13.3)
  - References: infrastructure/template.yaml
