# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- DynamoDB read layer (`internal/dynamo/reader.go`) with `Reader` interface (6 methods: `QueryReadings`, `GetSystem`, `GetOffpeak`, `GetDailyEnergy`, `QueryDailyEnergy`, `QueryDailyPower`) and `DynamoReader` implementation for the Lambda API
- `ReadAPI` client interface (Query, GetItem) separate from the existing write-focused `DynamoAPI` to maintain interface segregation between poller and API concerns
- Generic `getItem[T]` and `queryAll[T]` helpers for DynamoDB GetItem/Query operations with pagination, shared between `DynamoStore` and `DynamoReader`
- Unit tests for all `DynamoReader` methods covering success, not-found/empty, pagination, and error wrapping

### Changed

- `DynamoStore.GetOffpeak` refactored to use shared `getItem[T]` helper, eliminating implementation divergence with `DynamoReader.GetOffpeak`

- Lambda API spec: requirements document with 12 requirement groups and 74 acceptance criteria covering implementation constraints, authentication, status/history/day endpoints, response format, runtime configuration, and observability
- Lambda API spec: design document with architecture diagram, component interfaces (Handler, Reader, DynamoReader), response types, pure compute functions (cutoff estimation, rolling averages, sustained grid detection, downsampling), DynamoDB query patterns with pagination, concurrency model (errgroup Phase 1/Phase 2), and testing strategy
- Lambda API spec: decision log with 14 decisions (SSM caching, computation location, sustained grid threshold, day data resolution, cutoff estimation method, error format, single system, timezone, downsampling algorithm, low24h data source, float precision, time-to-full deferral, read layer design, query optimisation)
- Lambda API spec: task list with 17 implementation tasks across 5 phases and 2 parallel streams, TDD-ordered with dependency tracking and requirement traceability
- Lambda API spec: three-level explanation (beginner/intermediate/expert) with validation findings

- Implementation explanation (`specs/poller/implementation.md`) at beginner, intermediate, and expert levels with completeness assessment
- Poller orchestrator (`internal/poller/poller.go`) with 4 independent polling goroutines (10s live data, 1h daily power, 6h daily energy, 24h system info), immediate first poll on startup, two-context graceful shutdown pattern (25s drain timeout), and dry-run API response logging
- Off-peak scheduler (`internal/poller/offpeak.go`) with snapshot capture at window boundaries, 3-attempt retry with 10s intervals, delta computation for 6 energy fields + battery SOC, pending record persistence for crash recovery, mid-window startup recovery via DynamoDB query, and daily scheduling loop with DST-safe wall-clock times
- Midnight energy finalizer goroutine that captures previous day's final energy totals at 00:05 local time using DST-safe `time.Date` scheduling
- `APIClient` interface in poller package for testability of AlphaESS client dependency
- GitHub Actions CI workflow (`.github/workflows/poller.yml`) triggered on push to main, running `go vet` and `go test`, then building and pushing ARM64 container image to GHCR with `latest` and short SHA tags
- DynamoDB table name environment variables (`TABLE_READINGS`, `TABLE_DAILY_ENERGY`, `TABLE_DAILY_POWER`, `TABLE_SYSTEM`, `TABLE_OFFPEAK`) and `TZ=Australia/Sydney` to ECS container definition in CloudFormation template
- `dynamodb:DeleteItem` permission to TaskRole IAM policy for off-peak pending record cleanup
- DynamoDB storage layer (`internal/dynamo`) with `Store` interface, `DynamoStore` (production), and `LogStore` (dry-run) implementations
- DynamoDB item models: `ReadingItem`, `DailyEnergyItem`, `DailyPowerItem`, `SystemItem`, `OffpeakItem` with `dynamodbav` struct tags and `Status` field for off-peak record lifecycle
- Transformation functions (`NewReadingItem`, `NewDailyEnergyItem`, `NewDailyPowerItems`, `NewSystemItem`) mapping AlphaESS API types to DynamoDB items with 30-day TTL computation
- `DynamoStore` with `BatchWriteItem` chunking (max 25) and one retry for unprocessed items, contextual error wrapping on all operations
- `DynamoAPI` interface for DynamoDB client to enable unit testing without AWS
- `LogStore` dry-run implementation that logs table name and item JSON for each write operation
- AWS SDK v2 dependencies (`dynamodb`, `attributevalue`, `config`)
- AlphaESS API client (`internal/alphaess`) with SHA-512 request signing, 4 endpoint methods (`GetLastPowerData`, `GetOneDayPower`, `GetOneDateEnergy`, `GetEssList`), API envelope parsing, serial number filtering for `GetEssList`, and contextual error wrapping with endpoint names
- AlphaESS API response models (`internal/alphaess/models.go`): `PowerData`, `EnergyData`, `PowerSnapshot`, `SystemInfo` structs with JSON tags, and `apiResponse` envelope with `json.RawMessage` data field
- Configuration package (`internal/config`) with `Load()` function that reads all settings from environment variables, validates offpeak HH:MM times and timezone, collects all errors before reporting, and relaxes AWS/DynamoDB requirements in dry-run mode
- `testify` test dependency for assertions
- Go module (`github.com/ArjenSchwarz/flux`) with Go 1.26 and project directory structure (`cmd/poller/`, `internal/alphaess/`, `internal/config/`, `internal/dynamo/`, `internal/poller/`)
- Poller entrypoint (`cmd/poller/main.go`) with `os.Args` dispatch for `healthcheck` subcommand, slog JSON handler with `ReplaceAttr` (time→timestamp, lowercase levels), config loading, store creation (DynamoStore or LogStore), signal handling (SIGTERM/SIGINT), and poller startup/shutdown logging
- Health check (`cmd/poller/healthcheck.go`) querying `flux-readings` for the most recent item, returning exit 0 if reading is ≤60s old, exit 1 otherwise; dry-run mode always returns healthy; `healthQueryAPI` interface for testability
- Multi-stage Dockerfile: `golang:1.26-alpine` builder with ARM64 cross-compilation (`-trimpath -ldflags="-s -w"`), `gcr.io/distroless/static:nonroot` runtime with binary at `/poller`
- Embedded timezone data via `time/tzdata` blank import for distroless container compatibility
- Makefile with targets: build, test, fmt, vet, lint, modernize, check, docker-build, docker-dry-run, deps-tidy, deps-update
- `.dockerignore` excluding `.git/`, `specs/`, `docs/`, `infrastructure/`, `.github/`
- Poller spec: requirements document with 13 sections and 91 acceptance criteria covering Go application setup, AlphaESS API client, multi-schedule polling, DynamoDB storage, configuration, off-peak energy calculation, health check, process lifecycle, logging, Dockerfile, GitHub Actions CI, dry-run mode, and infrastructure update
- Poller spec: design document with architecture diagram, 9 component designs (entrypoint, config, AlphaESS client, Store interface with DynamoStore/LogStore, DynamoDB models, poller orchestrator, off-peak scheduler), two-context graceful shutdown pattern, error handling strategy, and testing strategy
- Poller spec: decision log with 16 decisions (log-and-skip error handling, structured logs only, off-peak SOC tracking, pgridSustained in Lambda, internal/ packages, os.Args over cobra, Store interface for dry-run, distroless base image, two-context shutdown, off-peak status field)
- Poller spec: task list with 23 tasks across 8 phases and 2 parallel streams, TDD-ordered with dependency tracking and requirement traceability
- CLAUDE.md project instructions for the flux repository
- VPC (`10.0.0.0/24`) with DNS support and two public subnets across availability zones (`10.0.0.0/25`, `10.0.0.128/25`)
- Internet Gateway with VPC attachment
- Route table with default route to IGW, associated with both subnets
- DynamoDB and S3 Gateway VPC endpoints attached to route table
- Security group allowing all egress and no ingress for Fargate tasks
- CloudWatch log groups for poller (`/flux/poller`) and API (`/flux/api`) with 14-day retention and DeletionPolicy Delete
- IAM roles: `TaskExecutionRole` (SSM read, CloudWatch Logs write), `TaskRole` (DynamoDB read/write on all 5 tables), `LambdaExecutionRole` (DynamoDB read, SSM read, CloudWatch Logs write) — all least-privilege, ARN-scoped
- 5 DynamoDB tables: `flux-readings` (TTL), `flux-daily-energy`, `flux-daily-power` (TTL), `flux-system`, `flux-offpeak` — all PAY_PER_REQUEST with DeletionPolicy Retain
- SSM parameters for app-id, serial, offpeak-start, and offpeak-end (String type, stack-managed via `SSMPathPrefix`)
- ECS cluster, Fargate task definition (ARM64, 256 CPU, 512 MB) with SSM secrets injection, health check (`/poller healthcheck`), and awslogs log driver
- ECS service (`flux-poller`) with Fargate launch type, both subnets, public IP enabled
- Lambda function (`flux-api`) with `provided.al2023` runtime, ARM64, 128 MB, 10s timeout, environment variables for SSM paths and DynamoDB table names
- Lambda Function URL (auth type NONE) with public invoke permission
- `.gitignore` with entries for `lambda/bootstrap` and `infrastructure/packaged.yaml`
- CloudFormation template skeleton (`infrastructure/template.yaml`) with 6 parameters (ContainerImageUri, AlphaESSAppId, SystemSerialNumber, OffPeakWindowStart, OffPeakWindowEnd, SSMPathPrefix) and 3 outputs (FunctionUrl, EcsClusterName, EcsServiceName)
- Infrastructure spec: requirements document with 8 requirement groups and 42 acceptance criteria covering VPC, ECS Fargate, DynamoDB, Lambda, SSM, IAM, CloudFormation deployment, and CloudWatch Logs
- Infrastructure spec: design document with full architecture diagram, CloudFormation resource definitions, IAM policies, health check design, deployment procedure, and testing strategy
- Infrastructure spec: decision log with 11 documented decisions (single template, ARM64, bearer token auth, on-demand DynamoDB, manual SecureString creation, cfn package deploy, DynamoDB health check, etc.)
- Infrastructure spec: task list with 12 implementation tasks across 6 phases, dependency-ordered with requirement traceability
- Infrastructure spec: prerequisites document listing manual setup steps required before deployment
- Deployment README (`infrastructure/README.md`) with prerequisites, SecureString setup commands, build/package/deploy workflow, and update procedures for Lambda code, container image, configuration, and infrastructure changes
- Infrastructure spec: implementation explanation at beginner, intermediate, and expert levels with completeness assessment

### Changed

- Go module version updated from 1.25 to 1.26 to match spec requirements
- Exported `config.FormatHHMM` and removed duplicate in `cmd/poller/main.go`
- Off-peak status values use `dynamo.OffpeakStatusPending` and `dynamo.OffpeakStatusComplete` constants instead of raw strings
- `timePosition` returns typed `windowPosition` instead of raw strings
- Extracted `pollLoop` helper to eliminate repeated poll goroutine pattern across 4 schedules
- Extracted `handleEndOrCleanup` helper to eliminate 3x repeated off-peak end-failure cleanup block
- Makefile `modernize` target now uses `go mod tidy -compat=1.26` per spec [1.6]
- CLAUDE.md corrected `flux-daily-power` TTL from 7d to 30d (per Decision 10)

### Fixed

- AlphaESS API client now uses GET requests with query parameters instead of POST with JSON body, matching the actual API specification (fixes HTTP 405 errors)
- AlphaESS API envelope success check now accepts both code 0 and code 200, matching the API's actual success response format
- Duplicate `dynamodb:DeleteItem` permission removed from TaskRole IAM policy in CloudFormation template
- CloudWatch Logs IAM policies now use `:*` suffix on log group ARNs for `TaskExecutionRole` and `LambdaExecutionRole`, required for `logs:CreateLogStream` and `logs:PutLogEvents` to match log stream resources
