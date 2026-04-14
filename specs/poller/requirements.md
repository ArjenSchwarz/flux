# Requirements: Flux Poller

## Introduction

The Flux Poller is a long-running Go application that runs as an ECS Fargate task. It polls the AlphaESS API on multiple schedules, transforms the responses, and writes data to five DynamoDB tables. It also computes off-peak energy deltas by snapshotting energy data at the start and end of a configurable off-peak window. This spec covers the Go application, Dockerfile, and GitHub Actions CI pipeline. It does not cover the Lambda API or iOS app.

The poller is the sole writer to all DynamoDB tables. The Lambda API and future clients are read-only consumers.

---

### 1. Go Application

**User Story:** As a developer, I want the poller to use current Go tooling and idiomatic dependencies, so that the codebase is maintainable and benefits from the latest language features and security patches.

**Acceptance Criteria:**

1. <a name="1.1"></a>The application SHALL use Go 1.26 or later (current stable: 1.26.x) with Go modules for dependency management  
2. <a name="1.2"></a>The application SHALL use `github.com/aws/aws-sdk-go-v2` (not the deprecated v1 SDK) for all AWS service interactions (DynamoDB, SSM)  
3. <a name="1.3"></a>The application SHALL use `log/slog` (standard library) for structured logging — no third-party logging libraries  
4. <a name="1.4"></a>The application SHALL use only the Go standard library for HTTP client, JSON parsing, SHA-512 hashing, and signal handling — no third-party libraries for these concerns  
5. <a name="1.5"></a>The application SHALL be structured as a Go module rooted at the repository root, with the poller entrypoint at `cmd/poller/main.go`  
6. <a name="1.6"></a>The application SHALL include a `Makefile` with targets for: `build`, `test`, `fmt`, `vet`, `lint` (golangci-lint), `modernize` (go mod tidy -compat && go fix ./...), `check` (fmt + vet + lint + test), `docker-build`, `deps-tidy`, and `deps-update`  

---

### 2. AlphaESS API Client

**User Story:** As the poller, I want to authenticate with and call the AlphaESS API, so that I can retrieve real-time and historical battery system data.

**Acceptance Criteria:**

1. <a name="2.1"></a>The system SHALL authenticate every AlphaESS API request by sending three custom headers: `appId` (the App ID), `timeStamp` (current Unix timestamp as a string), and `sign` (SHA-512 hex digest of `appId + appSecret + timestamp`)  
2. <a name="2.2"></a>The system SHALL generate the `timeStamp` header value at request time to stay within the AlphaESS server's 300-second drift tolerance  
3. <a name="2.3"></a>The system SHALL implement client methods for four endpoints: `getLastPowerData`, `getOneDayPowerBySn`, `getOneDateEnergyBySn`, and `getEssList`  
4. <a name="2.4"></a>The `getLastPowerData` method SHALL accept a serial number parameter and return real-time power fields (ppv, pbat, pgrid, pload, soc, and per-phase detail)  
5. <a name="2.5"></a>The `getOneDayPowerBySn` method SHALL accept a serial number and query date, and return 5-minute battery state snapshots (cbat values)  
6. <a name="2.6"></a>The `getOneDateEnergyBySn` method SHALL accept a serial number and query date, and return daily energy totals (epv, eInput, eOutput, eCharge, eDischarge, eGridCharge)  
7. <a name="2.7"></a>The `getEssList` method SHALL return system information including serial number (sysSn), battery capacity (cobat), and inverter details  
8. <a name="2.8"></a>The API client SHALL use an HTTP client with a 10-second request timeout  
9. <a name="2.9"></a>WHEN the AlphaESS API returns an error or non-200 status, the system SHALL log the error with the endpoint name and response details, and skip to the next scheduled poll  

---

### 3. Multi-Schedule Polling

**User Story:** As the system operator, I want the poller to call different AlphaESS endpoints on independent schedules, so that real-time data is fresh while infrequent data doesn't waste API calls.

**Acceptance Criteria:**

1. <a name="3.1"></a>The system SHALL poll `getLastPowerData` every 10 seconds  
2. <a name="3.2"></a>The system SHALL poll `getOneDayPowerBySn` with a target interval of 1 hour, passing the current date in the configured timezone as the query date  
3. <a name="3.3"></a>The system SHALL poll `getOneDateEnergyBySn` with a target interval of 6 hours, passing the current date in the configured timezone as the query date, plus at the off-peak window start and end times (see requirement [6](#6-off-peak-energy-calculation))  
4. <a name="3.4"></a>The system SHALL poll `getEssList` with a target interval of 24 hours  
5. <a name="3.5"></a>The system SHALL run all polling schedules concurrently using separate goroutines, each operating on its own timer independently  
6. <a name="3.6"></a>The system SHALL execute the first poll for each endpoint immediately on startup, rather than waiting for the first interval to elapse  
7. <a name="3.7"></a>The system SHALL call `getOneDateEnergyBySn` for the previous day's date once after local midnight (within the first hour) to capture final daily energy totals  

---

### 4. DynamoDB Storage

**User Story:** As the Lambda API, I want the poller to write structured data to DynamoDB tables, so that I can serve pre-computed stats to the iOS app.

**Acceptance Criteria:**

1. <a name="4.1"></a>The system SHALL write `getLastPowerData` responses to the `flux-readings` table with partition key `sysSn` (string) and sort key `timestamp` (number, Unix epoch)  
2. <a name="4.2"></a>The system SHALL set a `ttl` attribute on `flux-readings` items to 30 days from the reading timestamp  
3. <a name="4.3"></a>The system SHALL write `getOneDateEnergyBySn` responses to the `flux-daily-energy` table with partition key `sysSn` (string) and sort key `date` (string, YYYY-MM-DD)  
4. <a name="4.4"></a>The system SHALL write `getOneDayPowerBySn` responses to the `flux-daily-power` table with partition key `sysSn` (string) and sort key `uploadTime` (string, as returned by the API), using `BatchWriteItem` for efficiency when writing multiple snapshots  
5. <a name="4.5"></a>The system SHALL set a `ttl` attribute on `flux-daily-power` items to 30 days from the record timestamp (matching `flux-readings` TTL, as this data serves as a lower-resolution fallback for the day detail SOC chart)  
6. <a name="4.6"></a>The system SHALL write `getEssList` responses to the `flux-system` table with partition key `sysSn` (string), storing cobat, mbat, minv, popv, poinv, emsStatus, and a `lastUpdated` timestamp  
7. <a name="4.7"></a>The system SHALL write off-peak snapshot and delta records to the `flux-offpeak` table as specified in requirement [6](#6-off-peak-energy-calculation)  
8. <a name="4.8"></a>The system SHALL use the DynamoDB table names provided via environment variables (`TABLE_READINGS`, `TABLE_DAILY_ENERGY`, `TABLE_DAILY_POWER`, `TABLE_SYSTEM`, `TABLE_OFFPEAK`)  
9. <a name="4.9"></a>WHEN a DynamoDB write fails, the system SHALL log the error with the table name and item key, and continue polling  
10. <a name="4.10"></a>All DynamoDB writes SHALL use PutItem semantics (last-write-wins). Writing to the same key overwrites the previous item. This is the intended behavior for updating daily energy totals and off-peak records  
11. <a name="4.11"></a>WHEN writing `BatchWriteItem` requests, IF the response contains unprocessed items, THEN the system SHALL retry the unprocessed items once before logging the failure and continuing  

---

### 5. Configuration

**User Story:** As the system operator, I want the poller to read all configuration from environment variables injected by ECS, so that credentials and settings are managed through SSM Parameter Store without code changes.

**Acceptance Criteria:**

1. <a name="5.1"></a>The system SHALL read the AlphaESS App ID from the `ALPHA_APP_ID` environment variable  
2. <a name="5.2"></a>The system SHALL read the AlphaESS App Secret from the `ALPHA_APP_SECRET` environment variable  
3. <a name="5.3"></a>The system SHALL read the system serial number from the `SYSTEM_SERIAL` environment variable  
4. <a name="5.4"></a>The system SHALL read the off-peak window start time from the `OFFPEAK_START` environment variable (HH:MM format)  
5. <a name="5.5"></a>The system SHALL read the off-peak window end time from the `OFFPEAK_END` environment variable (HH:MM format)  
6. <a name="5.6"></a>The system SHALL read the AWS region from the `AWS_REGION` environment variable  
7. <a name="5.7"></a>The system SHALL read DynamoDB table names from environment variables: `TABLE_READINGS`, `TABLE_DAILY_ENERGY`, `TABLE_DAILY_POWER`, `TABLE_SYSTEM`, `TABLE_OFFPEAK`  
8. <a name="5.8"></a>The system SHALL read the timezone from the `TZ` environment variable, defaulting to `Australia/Sydney` if not set  
9. <a name="5.9"></a>IF any required environment variable is missing or empty at startup, THEN the system SHALL log the missing variable name and exit with a non-zero status code  
10. <a name="5.10"></a>IF `OFFPEAK_START` or `OFFPEAK_END` cannot be parsed as HH:MM, or if `OFFPEAK_START` is equal to or after `OFFPEAK_END`, THEN the system SHALL log the validation error and exit with a non-zero status code  
11. <a name="5.11"></a>IF the `TZ` value cannot be loaded as a valid IANA timezone, THEN the system SHALL log the error and exit with a non-zero status code  

---

### 6. Off-Peak Energy Calculation

**User Story:** As the iOS app user, I want to see exactly how much energy was imported, exported, charged, and discharged during the off-peak window, so that I can understand my off-peak electricity usage.

**Acceptance Criteria:**

1. <a name="6.1"></a>The system SHALL evaluate the off-peak window times in the configured timezone (default: Australia/Sydney), handling DST transitions automatically  
2. <a name="6.2"></a>WHEN the off-peak window start time arrives, the system SHALL call `getOneDateEnergyBySn` and store the result as the start snapshot  
3. <a name="6.3"></a>WHEN the off-peak window start time arrives, the system SHALL also call `getLastPowerData` and store the `soc` value as `socStart` in the off-peak record  
4. <a name="6.4"></a>WHEN the off-peak window end time arrives, the system SHALL call `getOneDateEnergyBySn` and store the result as the end snapshot  
5. <a name="6.5"></a>WHEN the off-peak window end time arrives, the system SHALL also call `getLastPowerData` and store the `soc` value as `socEnd` in the off-peak record  
6. <a name="6.6"></a>The system SHALL compute and store the following deltas: `gridUsageKwh` (eInput end - start), `solarKwh` (epv end - start), `batteryChargeKwh` (eCharge end - start), `batteryDischargeKwh` (eDischarge end - start), `gridExportKwh` (eOutput end - start)  
7. <a name="6.7"></a>The system SHALL compute and store `batteryDeltaPercent` as `socEnd - socStart`  
8. <a name="6.8"></a>The system SHALL write the off-peak record to the `flux-offpeak` table with partition key `sysSn` and sort key `date` (YYYY-MM-DD)  
9. <a name="6.9"></a>The off-peak record SHALL include both the start and end energy snapshots (all fields) alongside the computed deltas  
10. <a name="6.10"></a>IF the poller starts after the off-peak window start time but before the end time, THEN the system SHALL still capture the end snapshot and compute deltas against whatever start snapshot exists for that day (or skip if none)  
11. <a name="6.11"></a>IF the poller starts after the off-peak window has ended for the day, THEN the system SHALL NOT attempt to capture snapshots for the current day's off-peak window  
12. <a name="6.12"></a>IF an off-peak snapshot API call fails, the system SHALL retry up to 3 times with 10-second intervals between attempts. IF all retries fail, the system SHALL log a warning and skip the off-peak record for that day  
13. <a name="6.13"></a>IF the start snapshot succeeds but the end snapshot fails (after retries), THEN the system SHALL NOT write an off-peak record for that day  
14. <a name="6.14"></a>The off-peak end write SHALL include the start snapshot data (since PutItem replaces the entire item)  

---

### 7. Health Check

**User Story:** As ECS, I want to verify the poller is functioning, so that unhealthy tasks are replaced automatically.

**Acceptance Criteria:**

1. <a name="7.1"></a>The system SHALL support a `healthcheck` subcommand (invoked as `/poller healthcheck`) that exits 0 for healthy and non-zero for unhealthy  
2. <a name="7.2"></a>The `healthcheck` subcommand SHALL query the `flux-readings` table for the most recent item for the configured serial number  
3. <a name="7.3"></a>IF the most recent reading is less than 60 seconds old, THEN the health check SHALL exit 0  
4. <a name="7.4"></a>IF the most recent reading is more than 60 seconds old or no reading exists, THEN the health check SHALL exit 1  
5. <a name="7.5"></a>The health check SHALL complete within 10 seconds (the ECS health check timeout)  

---

### 8. Process Lifecycle

**User Story:** As the system operator, I want the poller to start up reliably and shut down gracefully, so that no data is lost during deployments or task replacements.

**Acceptance Criteria:**

1. <a name="8.1"></a>The system SHALL validate all configuration and establish a DynamoDB client connection before starting any polling loops  
2. <a name="8.2"></a>WHEN the process receives SIGTERM or SIGINT, the system SHALL stop scheduling new polls and wait for any in-flight API calls and DynamoDB writes to complete before exiting  
3. <a name="8.3"></a>The graceful shutdown SHALL complete within 30 seconds (the ECS stop timeout)  
4. <a name="8.4"></a>The system SHALL log a startup message including the configured serial number, off-peak window, and timezone  
5. <a name="8.5"></a>The system SHALL log a shutdown message when the process is stopping  

---

### 9. Logging

**User Story:** As the system operator, I want structured JSON logs, so that I can query and filter poller activity in CloudWatch Logs Insights.

**Acceptance Criteria:**

1. <a name="9.1"></a>The system SHALL output all log messages as JSON-structured lines to stdout  
2. <a name="9.2"></a>Each log entry SHALL include at minimum: `timestamp` (ISO 8601), `level` (info, warn, error), and `msg` fields  
3. <a name="9.3"></a>Log entries for API calls SHALL include the endpoint name and response status  
4. <a name="9.4"></a>Log entries for DynamoDB writes SHALL include the table name  
5. <a name="9.5"></a>Log entries for errors SHALL include an `error` field with the error message  
6. <a name="9.6"></a>The system SHALL NOT log credentials, secrets, or the full App Secret in any log entry  

---

### 10. Dockerfile

**User Story:** As the CI pipeline, I want a multi-stage Dockerfile that produces a minimal container image, so that the image is small, fast to pull, and has a minimal attack surface.

**Acceptance Criteria:**

1. <a name="10.1"></a>The Dockerfile SHALL use a multi-stage build: a Go builder stage and a minimal runtime stage  
2. <a name="10.2"></a>The builder stage SHALL compile the Go binary for `linux/arm64` (matching the Fargate ARM64/Graviton runtime)  
3. <a name="10.3"></a>The runtime stage SHALL use a distroless or scratch-based image with no shell, package manager, or unnecessary binaries  
4. <a name="10.4"></a>The final binary SHALL be placed at `/poller` in the container image (matching the ECS health check command)  
5. <a name="10.5"></a>The Dockerfile SHALL include CA certificates in the runtime image for HTTPS connections to the AlphaESS API and AWS endpoints  
6. <a name="10.6"></a>The runtime image SHALL include IANA timezone data, either via the base image (e.g., `gcr.io/distroless/static`) or by embedding Go's `time/tzdata` package in the binary  

---

### 11. GitHub Actions CI

**User Story:** As a developer, I want the container image to be built and pushed to GHCR automatically on merge to main, so that deployments use the latest code without manual steps.

**Acceptance Criteria:**

1. <a name="11.1"></a>The GitHub Actions workflow SHALL trigger on push to the `main` branch when files in the poller's source directories or Dockerfile change  
2. <a name="11.2"></a>The workflow SHALL build the container image for the `linux/arm64` platform  
3. <a name="11.3"></a>The workflow SHALL push the image to GHCR with the tags `latest` and the short commit SHA  
4. <a name="11.4"></a>The workflow SHALL authenticate to GHCR using the `GITHUB_TOKEN` secret  
5. <a name="11.5"></a>The workflow SHALL run `go vet` and `go test ./...` before building the container image, and SHALL fail the pipeline if either step fails  

---

### 12. Dry-Run Mode

**User Story:** As a developer, I want to run the poller locally without DynamoDB, so that I can test AlphaESS API connectivity and verify data parsing without deploying to AWS.

**Acceptance Criteria:**

1. <a name="12.1"></a>The system SHALL support a `--dry-run` flag (or `DRY_RUN=true` environment variable) that disables all DynamoDB writes  
2. <a name="12.2"></a>WHEN running in dry-run mode, the system SHALL log each API response payload at info level  
3. <a name="12.3"></a>WHEN running in dry-run mode, the system SHALL log the DynamoDB item that would have been written for each write operation, including the target table name and all item attributes  
4. <a name="12.4"></a>WHEN running in dry-run mode, the DynamoDB table name environment variables (`TABLE_READINGS`, `TABLE_DAILY_ENERGY`, `TABLE_DAILY_POWER`, `TABLE_SYSTEM`, `TABLE_OFFPEAK`) and `AWS_REGION` SHALL NOT be required  
5. <a name="12.5"></a>WHEN running in dry-run mode, the health check subcommand SHALL always exit 0  
6. <a name="12.6"></a>The system SHALL log a startup message indicating dry-run mode is active  

---

### 13. Infrastructure Update

**User Story:** As the poller application, I want DynamoDB table names passed as environment variables, so that table names are not hardcoded and remain consistent with the CloudFormation-managed resources.

**Acceptance Criteria:**

1. <a name="13.1"></a>The CloudFormation template SHALL pass DynamoDB table names to the poller container as environment variables: `TABLE_READINGS`, `TABLE_DAILY_ENERGY`, `TABLE_DAILY_POWER`, `TABLE_SYSTEM`, `TABLE_OFFPEAK`  
2. <a name="13.2"></a>The environment variable values SHALL reference the CloudFormation table resources (e.g., `!Ref ReadingsTable`) to stay in sync with the actual table names  
3. <a name="13.3"></a>The CloudFormation template SHALL pass the `TZ` environment variable to the poller container, defaulting to `Australia/Sydney`  
