---
references:
    - specs/lambda-api/requirements.md
    - specs/lambda-api/design.md
    - specs/lambda-api/decision_log.md
---
# Lambda API

## DynamoDB Read Layer

- [x] 1. Define Reader interface and ReadAPI interface <!-- id:e3gfl56 -->
  - Create internal/dynamo/reader.go with Reader interface (6 methods: QueryReadings, GetSystem, GetOffpeak, GetDailyEnergy, QueryDailyEnergy, QueryDailyPower)
  - Define ReadAPI client interface (Query + GetItem)
  - Define DynamoReader struct and NewDynamoReader constructor
  - Extract shared getOffpeakItem helper for DynamoStore and DynamoReader
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5)
  - References: internal/dynamo/store.go, internal/dynamo/dynamostore.go

- [x] 2. Write tests for DynamoReader <!-- id:e3gfl57 -->
  - Test each Reader method: successful query, empty result (nil/nil for Get, empty slice for Query), DynamoDB error
  - Verify QueryReadings sets ScanIndexForward: true and correct key condition expression
  - Test QueryReadings pagination: mock returns non-nil LastEvaluatedKey on first call, verify all pages collected
  - Verify QueryDailyEnergy date range query
  - Verify QueryDailyPower uses begins_with condition
  - Use mockReadAPI with function fields matching existing mockDynamoAPI pattern
  - Blocked-by: e3gfl56 (Define Reader interface and ReadAPI interface)
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5)
  - References: internal/dynamo/dynamostore_test.go

- [x] 3. Implement DynamoReader methods <!-- id:e3gfl58 -->
  - Implement QueryReadings with BETWEEN key condition and pagination loop on LastEvaluatedKey
  - Implement GetSystem, GetOffpeak, GetDailyEnergy as GetItem calls with nil/nil for not-found
  - Implement QueryDailyEnergy with date BETWEEN range and pagination
  - Implement QueryDailyPower with begins_with(uploadTime, date) and pagination
  - All Query methods set ScanIndexForward: true explicitly
  - Blocked-by: e3gfl57 (Write tests for DynamoReader)
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5), [1.6](requirements.md#1.6)
  - References: internal/dynamo/dynamostore.go

## Response Types and Business Logic

- [x] 4. Define JSON response structs <!-- id:e3gfl59 -->
  - Create internal/api/response.go with StatusResponse, LiveData, BatteryInfo, Low24h, RollingAvg, OffpeakData, TodayEnergy, HistoryResponse, DayEnergy, DayDetailResponse, TimeSeriesPoint, DaySummary
  - Use pointer types (*float64, *string) for nullable fields
  - JSON tags must match V1 plan contract exactly
  - Stream: 2
  - Requirements: [3.2](requirements.md#3.2), [4.1](requirements.md#4.1), [5.1](requirements.md#5.1), [6.1](requirements.md#6.1), [7.1](requirements.md#7.1), [8.5](requirements.md#8.5), [9.5](requirements.md#9.5), [9.9](requirements.md#9.9)

- [x] 5. Write tests for compute functions <!-- id:e3gfl5a -->
  - TestComputeCutoffTime: discharging normal, charging (nil), SOC at cutoff (nil), SOC below cutoff (nil), zero pbat (nil), calculation verification
  - TestComputeRollingAverages: empty slice, single reading, multiple readings, arithmetic
  - TestComputePgridSustained: 3+ consecutive above threshold (true), 2 consecutive (false), gap >30s breaks chain (false), interspersed below-threshold (false), empty (false)
  - TestDownsample: full day, sparse readings, single reading, empty input, bucket boundary, average correctness
  - TestFindMinSOC: normal, single reading, empty input
  - TestRoundEnergy/TestRoundPower: rounding edge cases
  - Use table-driven tests with map[string]struct pattern
  - Blocked-by: e3gfl59 (Define JSON response structs)
  - Stream: 2
  - Requirements: [3.6](requirements.md#3.6), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6), [9.6](requirements.md#9.6), [9.10](requirements.md#9.10), [10.7](requirements.md#10.7)

- [x] 6. Implement compute functions <!-- id:e3gfl5b -->
  - Create internal/api/compute.go
  - computeCutoffTime: linear extrapolation with nil guards for charging, idle, SOC <= cutoff
  - computeRollingAverages: mean of pload and pbat
  - computePgridSustained: iterate backwards from most recent, track consecutive pgrid > 500 with gap <= 30s
  - downsample: 288 five-minute buckets, average all readings per bucket, omit empty buckets
  - findMinSOC: scan for minimum SOC, return (soc, timestamp, found)
  - roundEnergy (2dp) and roundPower (1dp)
  - Blocked-by: e3gfl5a (Write tests for compute functions)
  - Stream: 2
  - Requirements: [3.6](requirements.md#3.6), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6), [9.6](requirements.md#9.6), [9.10](requirements.md#9.10), [10.7](requirements.md#10.7)

## Handler and Routing

- [x] 7. Write tests for Handler routing and auth <!-- id:e3gfl5c -->
  - TestHandleMethod: GET passes, POST/PUT/DELETE return 405 with correct JSON body
  - TestHandleAuth: valid token 200, missing header 401, wrong token 401, malformed Bearer header 401
  - TestHandleAuthBeforeRouting: invalid token + unknown path returns 401 not 404
  - TestHandleRouting: /status 200, /history 200, /day 200 (with valid date param), unknown path 404
  - Verify all error responses have Content-Type: application/json
  - Use mockReader that returns minimal valid data for routing tests
  - Blocked-by: e3gfl59 (Define JSON response structs)
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [10.1](requirements.md#10.1), [10.2](requirements.md#10.2), [10.5](requirements.md#10.5), [10.6](requirements.md#10.6)
  - References: internal/dynamo/dynamostore_test.go

- [x] 8. Implement Handler struct with routing and auth <!-- id:e3gfl5d -->
  - Create internal/api/handler.go with Handler struct, NewHandler constructor
  - Handle method: accept LambdaFunctionURLRequest, return LambdaFunctionURLResponse
  - Method check (GET only, 405 otherwise), auth with crypto/subtle.ConstantTimeCompare, routing by rawPath
  - Auth runs before routing — unauthenticated requests get 401 regardless of path
  - Error response helper: errorResponse(status, message) returning LambdaFunctionURLResponse
  - Request logging with slog: method, path, status code, duration
  - Never log the bearer token
  - Blocked-by: e3gfl5c (Write tests for Handler routing and auth)
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [10.1](requirements.md#10.1), [10.2](requirements.md#10.2), [10.4](requirements.md#10.4), [10.5](requirements.md#10.5), [10.6](requirements.md#10.6), [12.1](requirements.md#12.1), [12.2](requirements.md#12.2), [12.4](requirements.md#12.4)

## Endpoint Handlers

- [x] 9. Write tests for /status endpoint <!-- id:e3gfl5e -->
  - Test normal case: all data present, verify all response fields with correct rounding
  - Test no readings: live null, rolling15min null, low24h null
  - Test off-peak pending: delta fields null, windowStart/windowEnd still present
  - Test off-peak complete: all delta fields populated
  - Test no today energy: todayEnergy null
  - Test system info missing: fallback capacity 13.34
  - Test DynamoDB error in any Phase 1 operation: returns 500
  - Verify single now capture: mock clock to verify time consistency
  - Blocked-by: e3gfl5b (Implement compute functions), e3gfl5d (Implement Handler struct with routing and auth)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8), [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.7](requirements.md#4.7), [4.8](requirements.md#4.8), [4.9](requirements.md#4.9), [4.10](requirements.md#4.10), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.7](requirements.md#5.7), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [6.5](requirements.md#6.5), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3), [10.3](requirements.md#10.3)

- [x] 10. Implement /status endpoint handler <!-- id:e3gfl5f -->
  - Create internal/api/status.go with handleStatus method
  - Phase 1: errgroup.WithContext for concurrent QueryReadings(24h), GetSystem, GetOffpeak, GetDailyEnergy
  - Phase 2: extract latest reading (last element of 24h), filter to 60s/15min subsets in memory
  - Build live object from latest reading + computePgridSustained from 60s subset
  - Build battery object: capacity from system (fallback 13.34), computeCutoffTime, findMinSOC for low24h
  - Build rolling15min: computeRollingAverages, cutoff from avg discharge
  - Build offpeak: check status == complete, pass-through windowStart/windowEnd
  - Build todayEnergy from daily energy
  - Apply rounding via builder functions
  - Blocked-by: e3gfl5e (Write tests for /status endpoint)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8), [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6), [4.7](requirements.md#4.7), [4.8](requirements.md#4.8), [4.9](requirements.md#4.9), [4.10](requirements.md#4.10), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [6.5](requirements.md#6.5), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3)

- [x] 11. Write tests for /history endpoint <!-- id:e3gfl5g -->
  - Test default days (7), explicit 14, explicit 30
  - Test invalid days parameter: returns 400 with correct message
  - Test no data for range: empty days array
  - Test result ordering: ascending date
  - Test energy values rounded to 2 decimal places
  - Blocked-by: e3gfl5b (Implement compute functions), e3gfl5d (Implement Handler struct with routing and auth)
  - Stream: 2
  - Requirements: [8.1](requirements.md#8.1), [8.2](requirements.md#8.2), [8.3](requirements.md#8.3), [8.4](requirements.md#8.4), [8.5](requirements.md#8.5), [8.6](requirements.md#8.6), [8.7](requirements.md#8.7), [8.8](requirements.md#8.8), [10.7](requirements.md#10.7)

- [x] 12. Implement /history endpoint handler <!-- id:e3gfl5h -->
  - Create internal/api/history.go with handleHistory method
  - Parse days query parameter with default 7, validate 7/14/30
  - Compute date range from today in configured timezone
  - Call Reader.QueryDailyEnergy(serial, startDate, today)
  - Map to DayEnergy response structs with rounding
  - Return HistoryResponse with sorted days array
  - Blocked-by: e3gfl5g (Write tests for /history endpoint)
  - Stream: 2
  - Requirements: [8.1](requirements.md#8.1), [8.2](requirements.md#8.2), [8.3](requirements.md#8.3), [8.4](requirements.md#8.4), [8.5](requirements.md#8.5), [8.6](requirements.md#8.6), [8.7](requirements.md#8.7), [8.8](requirements.md#8.8)

- [x] 13. Write tests for /day endpoint <!-- id:e3gfl5i -->
  - Test normal case: readings + daily energy, verify downsampled output and full summary
  - Test fallback: no flux-readings, flux-daily-power data returned directly (cbat -> soc, power fields 0)
  - Test no data from either source: empty readings, null summary
  - Test readings exist but no daily energy: summary has socLow/socLowTime, null energy fields
  - Test date validation: missing date 400, invalid format 400
  - Test socLow computed from raw data, not downsampled data
  - Blocked-by: e3gfl5b (Implement compute functions), e3gfl5d (Implement Handler struct with routing and auth)
  - Stream: 2
  - Requirements: [9.1](requirements.md#9.1), [9.2](requirements.md#9.2), [9.3](requirements.md#9.3), [9.4](requirements.md#9.4), [9.5](requirements.md#9.5), [9.6](requirements.md#9.6), [9.7](requirements.md#9.7), [9.8](requirements.md#9.8), [9.9](requirements.md#9.9), [9.10](requirements.md#9.10), [9.11](requirements.md#9.11), [9.12](requirements.md#9.12), [9.13](requirements.md#9.13), [9.14](requirements.md#9.14)

- [x] 14. Implement /day endpoint handler <!-- id:e3gfl5j -->
  - Create internal/api/day.go with handleDay method
  - Parse and validate date parameter (YYYY-MM-DD)
  - Query flux-readings for full day, fall back to QueryDailyPower if empty
  - Fallback data used directly (no downsample), map cbat to soc, power fields to 0
  - Compute socLow/socLowTime from raw readings before downsampling
  - Downsample flux-readings via downsample function (skip for fallback)
  - Get daily energy for summary assembly
  - Null summary when neither readings nor daily energy exist
  - Blocked-by: e3gfl5i (Write tests for /day endpoint)
  - Stream: 2
  - Requirements: [9.1](requirements.md#9.1), [9.2](requirements.md#9.2), [9.3](requirements.md#9.3), [9.4](requirements.md#9.4), [9.5](requirements.md#9.5), [9.6](requirements.md#9.6), [9.7](requirements.md#9.7), [9.8](requirements.md#9.8), [9.9](requirements.md#9.9), [9.10](requirements.md#9.10), [9.11](requirements.md#9.11), [9.12](requirements.md#9.12), [9.13](requirements.md#9.13), [9.14](requirements.md#9.14)

## Entry Point and Infrastructure

- [x] 15. Create Lambda entry point and config loading <!-- id:e3gfl5k -->
  - Create cmd/api/main.go with main function
  - Import time/tzdata for timezone embedding
  - loadConfig: load AWS SDK config, create DynamoDB client, create SSM client
  - Fetch SSM params (api-token, serial) and cache as strings
  - Load env vars: table names, offpeak window, TZ
  - Validate all required config present, log error and os.Exit(1) if missing
  - Create DynamoReader and Handler, call lambda.Start(handler.Handle)
  - Blocked-by: e3gfl5f (Implement /status endpoint handler), e3gfl5h (Implement /history endpoint handler), e3gfl5j (Implement /day endpoint handler)
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.7](requirements.md#1.7), [2.2](requirements.md#2.2), [2.6](requirements.md#2.6), [11.1](requirements.md#11.1), [11.2](requirements.md#11.2), [11.3](requirements.md#11.3), [11.4](requirements.md#11.4), [11.5](requirements.md#11.5), [11.6](requirements.md#11.6), [11.7](requirements.md#11.7), [12.1](requirements.md#12.1)

- [x] 16. Update Makefile and CloudFormation template <!-- id:e3gfl5l -->
  - Add build-api target: CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o lambda/bootstrap ./cmd/api
  - Add TZ: Australia/Sydney to Lambda environment variables in infrastructure/template.yaml
  - Update Lambda MemorySize from 128 to 256 in template.yaml
  - Blocked-by: e3gfl5k (Create Lambda entry point and config loading)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.8](requirements.md#1.8), [11.5](requirements.md#11.5)
  - References: Makefile, infrastructure/template.yaml

- [x] 17. Add new Go dependencies and verify build <!-- id:e3gfl5m -->
  - Run go get github.com/aws/aws-lambda-go
  - Run go get github.com/aws/aws-sdk-go-v2/service/ssm
  - Run go mod tidy
  - Run make check to verify all tests pass and linting is clean
  - Run make build-api to verify the Lambda binary compiles
  - Blocked-by: e3gfl5l (Update Makefile and CloudFormation template)
  - Stream: 2
  - Requirements: [1.4](requirements.md#1.4), [1.6](requirements.md#1.6), [1.8](requirements.md#1.8)
