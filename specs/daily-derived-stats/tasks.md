---
references:
    - specs/daily-derived-stats/requirements.md
    - specs/daily-derived-stats/design.md
    - specs/daily-derived-stats/decision_log.md
---
# Daily Derived Stats — Tasks

- [x] 1. Extract internal/derivedstats package with own Reading type <!-- id:qwdi4hx -->
  - Move findDailyUsage, findMinSOC, findPeakPeriods, integratePload, melbourneSunriseSunset, parseOffpeakWindow, related constants (preSunriseBlipBuffer, recentSolarThreshold), and the pendingBlock sentinel struct from internal/api/compute.go to a new internal/derivedstats package
  - Define derivedstats.Reading mirroring the dynamo.ReadingItem fields the helpers consume (Timestamp, Ppv, Pload, Soc, Pbat, Pgrid)
  - Move and adapt internal/api/compute_test.go content to internal/derivedstats/*_test.go (split by file boundary: blocks_test.go, peakperiods_test.go, socmin_test.go, integrate_test.go, melbourne_test.go)
  - Update internal/api/day.go call sites with a local toDerivedReadings([]dynamo.ReadingItem) []derivedstats.Reading helper
  - Export ParseOffpeakWindow so the poller can pre-gate
  - Confirm zero Flux-internal imports in derivedstats per Decision 9
  - go build and go test ./... pass
  - Stream: 1
  - Requirements: [1.9](requirements.md#1.9)
  - References: internal/api/compute.go, internal/api/compute_test.go, internal/api/day.go, internal/api/melbourne_sun_table.go

- [x] 2. Write tests for dynamo *Attr conversion functions <!-- id:qwdi4hy -->
  - Test DailyUsageFromAttr/ToAttr, SocLowFromAttr/ToAttr, PeakPeriodsFromAttr/ToAttr round-trip equivalence
  - Cover empty PeakPeriods slice case
  - Cover nil DailyUsage case
  - Use rapid for property-based round-trip per design's PBT plan
  - Tests fail (red) — conversion functions don't exist yet
  - Blocked-by: qwdi4hx (Extract internal/derivedstats package with own Reading type)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3)
  - References: internal/dynamo/derived_conv_test.go

- [x] 3. Implement dynamo *Attr types + conversion functions + DerivedStats bundle <!-- id:qwdi4hz -->
  - Add DailyUsageAttr, DailyUsageBlockAttr, SocLowAttr (Soc float64, Timestamp RFC3339 string), PeakPeriodAttr to internal/dynamo/models.go
  - Extend DailyEnergyItem with the four optional fields: DailyUsage, SocLow, PeakPeriods, DerivedStatsComputedAt (sentinel per Decision 8)
  - Define dynamo.DerivedStats bundle struct (per Decision 9 — lives in dynamo, not poller)
  - Create internal/dynamo/derived_conv.go with the six conversion functions
  - All Task 2 tests pass (green)
  - Blocked-by: qwdi4hy (Write tests for dynamo *Attr conversion functions)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4)
  - References: internal/dynamo/models.go, internal/dynamo/derived_conv.go

- [x] 4. Write tests for new dynamo store methods (UpdateItem migration + UpdateDailyEnergyDerived + GetDailyEnergy + struct-tag coverage) <!-- id:qwdi4i0 -->
  - Test WriteDailyEnergy migrated to UpdateItem: SET expression covers exactly the six energy fields, no derivedStats clobber
  - Test UpdateDailyEnergyDerived: SET expression covers all four derived attributes (dailyUsage, socLow, peakPeriods, derivedStatsComputedAt) atomically
  - Test GetDailyEnergy returns nil for missing rows, full item for present rows
  - Add struct-tag-coverage regression test using reflect: walk DailyEnergyItem, exclude derivedStats and key fields, assert each remaining tag appears in WriteDailyEnergy's SET expression (per design's future-proofing note)
  - LogStore (dry-run) stubs for the new methods
  - Tests fail (red)
  - Blocked-by: qwdi4hz (Implement dynamo *Attr types + conversion functions + DerivedStats bundle)
  - Stream: 1
  - Requirements: [1.4](requirements.md#1.4), [2.5](requirements.md#2.5), [6.3](requirements.md#6.3)
  - References: internal/dynamo/dynamostore_test.go, internal/dynamo/logstore_test.go

- [x] 5. Implement dynamo store: WriteDailyEnergy → UpdateItem; add UpdateDailyEnergyDerived + GetDailyEnergy <!-- id:qwdi4i1 -->
  - Migrate WriteDailyEnergy from PutItem to UpdateItem with SET on the six energy fields (Decision 3)
  - Implement UpdateDailyEnergyDerived: SET dailyUsage = :du, socLow = :sl, peakPeriods = :pp, derivedStatsComputedAt = :ts
  - Implement GetDailyEnergy via the existing getItem helper pattern
  - LogStore implements all three as no-ops that log the would-be payload for dry-run
  - Add DynamoAPI interface method UpdateItem
  - All Task 4 tests pass (green)
  - Blocked-by: qwdi4i0 (Write tests for new dynamo store methods (UpdateItem migration + UpdateDailyEnergyDerived + GetDailyEnergy + struct-tag coverage)), methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods, methods
  - Stream: 1
  - Requirements: [1.4](requirements.md#1.4), [2.1](requirements.md#2.1)
  - References: internal/dynamo/dynamostore.go, internal/dynamo/logstore.go

- [x] 6. Write tests for poller CloudWatch Metrics shim <!-- id:qwdi4i2 -->
  - Test RecordSummarisationPass emits a PutMetricData call with namespace Flux/Poller, metric name SummarisationPassResult, and Result dimension matching the input value (success / skipped-no-readings / skipped-no-row / skipped-ssm-unresolved / skipped-already-populated / error)
  - Test that a PutMetricData failure is logged but does not propagate to the caller
  - Test that the dry-run no-op variant makes no AWS calls
  - Tests fail (red)
  - Stream: 1
  - Requirements: [1.11](requirements.md#1.11)
  - References: internal/poller/metrics_test.go

- [x] 7. Implement poller Metrics struct + CloudWatchAPI interface <!-- id:qwdi4i3 -->
  - Define CloudWatchAPI interface (subset: PutMetricData)
  - Define Metrics struct with client + namespace fields
  - Implement RecordSummarisationPass: build PutMetricDataInput with one MetricDatum, dimension Result=<value>, log warn on failure, never return error
  - Dry-run no-op variant
  - All Task 6 tests pass (green)
  - Blocked-by: qwdi4i2 (Write tests for poller CloudWatch Metrics shim)
  - Stream: 1
  - Requirements: [1.11](requirements.md#1.11)
  - References: internal/poller/metrics.go

- [x] 8. Write tests for the poller summarisation pass (all AC 6.1 / 6.2 scenarios) <!-- id:qwdi4i4 -->
  - Cover all 8 scenarios from AC 6.1: success path; no-readings (no UpdateItem call); no-row (skipped per AC 1.4); ssm-unresolved (skipped per AC 1.6); readings-error (logged, no panic, no write); update-error (logged, no panic); already-populated via DerivedStatsComputedAt sentinel (skipped per AC 1.10, no QueryReadings); date passed as today to derivedstats.Blocks so the today-gate doesn't fire
  - Cover AC 6.2: two consecutive passes against same readings produce field-equivalent UpdateItem payloads, second pass short-circuits via precheck
  - Use a fake DynamoStore + fake Metrics + fake derivedstats inputs
  - Tests fail (red)
  - Blocked-by: qwdi4hz (Implement dynamo *Attr types + conversion functions + DerivedStats bundle), qwdi4i3 (Implement poller Metrics struct + CloudWatchAPI interface)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.10](requirements.md#1.10), [1.13](requirements.md#1.13), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)
  - References: internal/poller/dailysummary_test.go

- [x] 9. Implement poller summarisation pass + wire into Run <!-- id:qwdi4i5 -->
  - Add internal/poller/dailysummary.go with summariseYesterday + runSummarisationPass + toDerivedReadings (per design's code sketch)
  - Add dailySummaryInterval constant alongside the existing constant block in poller.go
  - Add pollDailySummary(loopCtx, drainCtx, wg) using pollLoop
  - Wire into Run: bump wg.Add(5)→wg.Add(6) and add `go p.pollDailySummary(ctx, drainCtx, &wg)`
  - Construct Metrics in Poller.Run (or NewPoller) using the AWS SDK CloudWatch client; pass dry-run no-op when cfg.DryRun
  - All Task 8 tests pass (green)
  - Blocked-by: qwdi4i4 (Write tests for the poller summarisation pass (all AC 6.1 / 6.2 scenarios))
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.10](requirements.md#1.10), [1.11](requirements.md#1.11), [1.12](requirements.md#1.12), [1.13](requirements.md#1.13), [1.14](requirements.md#1.14)
  - References: internal/poller/poller.go, internal/poller/dailysummary.go

- [x] 10. Write tests for Lambda /day past-date branch <!-- id:qwdi4i6 -->
  - AC 6.4 scenarios: completed-date with all three derived fields present (served from storage); completed-date with one field absent (only that section omitted); completed-date with all three absent (all sections omitted); completed-date with no readings AND no derivedStats but flux-daily-power available (chart and socLow from daily-power continue rendering — regression for AC 3.5)
  - Today path unchanged from pre-feature behaviour
  - Regression assertion: handleDay for a completed date issues no QueryReadings call (verify via fake reader)
  - Tests fail (red)
  - Blocked-by: qwdi4hz (Implement dynamo *Attr types + conversion functions + DerivedStats bundle), qwdi4i1 (Implement dynamo store: WriteDailyEnergy → UpdateItem; add UpdateDailyEnergyDerived + GetDailyEnergy)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7), [6.4](requirements.md#6.4)
  - References: internal/api/day_test.go

- [x] 11. Implement Lambda /day past-date storage read + skip-readings-query <!-- id:qwdi4i7 -->
  - Branch on date == today at request entry (single clock read per AC 3.7)
  - For past dates: skip the QueryReadings goroutine; read derivedStats from deItem (DailyUsage, SocLow, PeakPeriods) using the *Attr → derivedstats.* converters; preserve the existing flux-daily-power fallback (QueryDailyPower + mapDailyPowerToPoints + findMinSOCFromPower) for past dates with no readings (AC 3.5)
  - For today: existing live-compute path unchanged; convert allReadings via toDerivedReadings before calling derivedstats helpers
  - Wire socLow/socLowTime into summary section (existing DaySummary shape)
  - All Task 10 tests pass (green)
  - Blocked-by: qwdi4i6 (Write tests for Lambda /day past-date branch)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)
  - References: internal/api/day.go

- [x] 12. Write tests for Lambda /history derived-stats surfacing + today live-compute + AC 4.9 failure mode <!-- id:qwdi4i8 -->
  - AC 6.5 scenarios: 7-day window every day has all three; oldest day lacks derived fields; window straddling today (today live-computed, past rows from storage)
  - AC 4.9: today-readings query failure → today row served with energy only, derivedStats omitted, rest of range unaffected, response is not an error
  - Tests fail (red)
  - Blocked-by: qwdi4hz (Implement dynamo *Attr types + conversion functions + DerivedStats bundle), qwdi4i1 (Implement dynamo store: WriteDailyEnergy → UpdateItem; add UpdateDailyEnergyDerived + GetDailyEnergy)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6), [4.9](requirements.md#4.9), [6.5](requirements.md#6.5)
  - References: internal/api/history_test.go

- [x] 13. Implement Lambda /history per-row derived-stats + today live-compute + today error tolerance <!-- id:qwdi4i9 -->
  - For each DayEnergy row: when isItemToday is false and the item carries derived attrs, populate DayEnergy.DailyUsage / SocLow / SocLowTime / PeakPeriods via the converters
  - For today's row: when allReadings is non-empty (no error from the existing today-readings query), live-compute via toDerivedReadings(allReadings) and the three derivedstats helpers; when allReadings is nil due to a query error, omit the today derivedStats and proceed (do not fail the whole /history request — AC 4.9)
  - Single clock-of-record per AC 4.6 (use existing now value)
  - All Task 12 tests pass (green)
  - Blocked-by: qwdi4i8 (Write tests for Lambda /history derived-stats surfacing + today live-compute + AC 4.9 failure mode)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.6](requirements.md#4.6), [4.9](requirements.md#4.9)
  - References: internal/api/history.go

- [x] 14. Write cross-handler equivalence test (AC 6.6) <!-- id:qwdi4ia -->
  - For a fixed clock and a stored derivedStats payload, assert /day for date X returns derivedStats field-equivalent to the row's contents under summary.socLow/summary.socLowTime + dailyUsage + peakPeriods
  - Assert /history for a window containing X returns the same field set on that day's row (flat socLow/socLowTime per the wire-shape note)
  - Assert the values match field-by-field (not byte-by-byte)
  - Test fails (red), then passes once Tasks 11 and 13 are complete
  - Blocked-by: qwdi4i7 (Implement Lambda /day past-date storage read + skip-readings-query), qwdi4i9 (Implement Lambda /history per-row derived-stats + today live-compute + today error tolerance)
  - Stream: 1
  - Requirements: [3.6](requirements.md#3.6), [4.10](requirements.md#4.10), [6.6](requirements.md#6.6)
  - References: internal/api/cross_handler_test.go

- [x] 15. Write end-to-end DynamoDB Local round-trip integration test (AC 6.7) <!-- id:qwdi4ib -->
  - Gate on INTEGRATION env var: if unset, t.Skip
  - Use Testcontainers-managed DynamoDB Local image
  - Create flux-readings, flux-daily-energy, flux-daily-power, flux-system, flux-offpeak tables matching the production schema
  - Stage a day's worth of synthetic readings + an existing energy row
  - Invoke poller summarisation pass against that date
  - Invoke /day and /history against the same date and assert responses carry the expected derivedStats shape (verify the storage attribute-shape contract end-to-end)
  - Blocked-by: qwdi4i5 (Implement poller summarisation pass + wire into Run), qwdi4i7 (Implement Lambda /day past-date storage read + skip-readings-query), qwdi4i9 (Implement Lambda /history per-row derived-stats + today live-compute + today error tolerance)
  - Stream: 1
  - Requirements: [6.7](requirements.md#6.7)
  - References: internal/integration/derivedstats_e2e_test.go

- [x] 16. Property-based tests for derivedstats determinism + dynamo conversion round-trip <!-- id:qwdi4ic -->
  - Use pgregory.net/rapid (per Go testing rules)
  - Property 1 (determinism): for any (readings, offpeakStart, offpeakEnd, date, today, now) tuple, two consecutive runSummarisationPass invocations produce field-equivalent UpdateItem payloads
  - Property 2 (precheck round-trip): for any derivedstats output written via UpdateDailyEnergyDerived and read back via GetDailyEnergy, the precheck sentinel is present
  - Property 3 (conversion round-trip): for any derivedstats.DailyUsage, DailyUsageFromAttr(DailyUsageToAttr(d)) is field-equivalent to d; same for SocLow and PeakPeriods
  - Generators: genReadings (monotonic timestamps within one Sydney day, length 0–1500), genOffpeak (HH:MM strings + small fraction unparseable), genDerivedStats (block counts 0–5, peak period counts 0–3)
  - Blocked-by: qwdi4hz (Implement dynamo *Attr types + conversion functions + DerivedStats bundle), qwdi4i5 (Implement poller summarisation pass + wire into Run)
  - Stream: 1
  - Requirements: [1.8](requirements.md#1.8), [2.1](requirements.md#2.1)
  - References: internal/derivedstats/property_test.go, internal/dynamo/derived_conv_property_test.go, internal/poller/dailysummary_property_test.go

- [x] 17. Write iOS DayEnergy + CachedDayEnergy decoding tests <!-- id:qwdi4id -->
  - AC 6.8 scenarios: DayEnergy JSON with all three new sections (decodes with non-nil properties); DayEnergy JSON with none of them (decodes with nil properties, no error); CachedDayEnergy round-trip for both cases
  - Empirical pre-feature cache load test: load a SwiftData store file written by the pre-feature build (committed as a fixture) and verify it opens without ModelContainer error and rows have nil derivedStats fields (AC 5.5)
  - Tests fail (red) — new properties don't exist yet
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [6.8](requirements.md#6.8)
  - References: Flux/Packages/FluxCore/Tests/FluxCoreTests/DayEnergyDecodingTests.swift, Flux/FluxTests/CachedDayEnergyTests.swift

- [x] 18. Implement iOS DayEnergy + CachedDayEnergy extensions; add lightweight migration if needed <!-- id:qwdi4ie -->
  - Add four optional properties to DayEnergy (dailyUsage: DailyUsage?, socLow: Double?, socLowTime: String?, peakPeriods: [PeakPeriod]?) with nil defaults so existing callsites compile
  - Add same four optional Codable properties to CachedDayEnergy @Model; update init(from day:) and asDayEnergy round-trip
  - Pre-flight checks (per design): confirm CachedDayEnergy has no @Relationship; confirm DailyUsage / DailyUsageBlock / PeakPeriod in APIModels.swift are pure Codable structs
  - Run the AC 5.5 empirical migration check on the simulator. If SwiftData rejects the schema, add a VersionedSchema with a lightweight migration mapping pre-feature to post-feature (per AC 5.5 fail criterion)
  - All Task 17 tests pass (green)
  - Blocked-by: qwdi4id (Write iOS DayEnergy + CachedDayEnergy decoding tests)
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift, Flux/Flux/Models/CachedDayEnergy.swift

- [x] 19. Update CloudFormation: add poller IAM grants for UpdateItem + PutMetricData <!-- id:qwdi4if -->
  - Add to PollerTaskRole policies a new DerivedStatsWritePolicy with two statements: dynamodb:UpdateItem on !GetAtt DailyEnergyTable.Arn (per Decision 3); cloudwatch:PutMetricData with Resource: '*' and Condition StringEquals cloudwatch:namespace = 'Flux/Poller' (per AC 1.11 / 7.3)
  - Single CFN update lands atomically with the ECS task definition swap (Section 7)
  - Stream: 3
  - Requirements: [7.1](requirements.md#7.1), [7.3](requirements.md#7.3)
  - References: infrastructure/template.yaml

- [x] 20. Verify Lambda IAM stanza in CloudFormation already covers GetItem + Query on flux-daily-energy <!-- id:qwdi4ig -->
  - Locate the Lambda role policies in infrastructure/template.yaml
  - Confirm dynamodb:GetItem (used by handleDay) and dynamodb:Query (used by handleHistory) are granted on flux-daily-energy
  - If the existing grant is narrower than expected (attribute-condition scoped, GSI-restricted), update it to allow reading the new derivedStats attributes
  - Cite the specific stanza reference in the implementation notes
  - Stream: 3
  - Requirements: [7.2](requirements.md#7.2)
  - References: infrastructure/template.yaml

- [x] 21. Add `make integration` Makefile target <!-- id:qwdi4ih -->
  - Add target that runs INTEGRATION=1 go test ./...
  - If the e2e test (Task 15) needs DynamoDB Local started by the same target, add the container start/stop steps (e.g. via docker run + trap)
  - Update CI workflow to invoke the new target
  - The e2e test still works locally via the env var alone if the target is unavailable
  - Stream: 3
  - Requirements: [6.7](requirements.md#6.7)
  - References: Makefile

- [x] 22. Capture performance baselines + post-feature measurements (AC 2.5, 4.7, 4.8) <!-- id:qwdi4ii -->
  - Add a Go benchmark test in internal/api that exercises handleHistory against a 30-day fixture and reports p95 latency (use Go testing.B with B.Loop pattern per Go testing rules)
  - Add a sizing test in internal/dynamo that marshals a representative DailyEnergyItem with all three derivedStats sections via attributevalue.MarshalMap, computes the serialized item size, and asserts < 4 KB (per AC 2.5)
  - Capture pre-feature baseline numbers (run benchmarks against the pre-feature commit; record in the implementation PR description)
  - Capture post-feature numbers and assert they meet AC 4.7 (+50 ms p95 budget) and AC 4.8 (≤3× payload size)
  - If any budget is exceeded, surface to the implementation review for design revisit per the AC's escape clause
  - Blocked-by: qwdi4i5 (Implement poller summarisation pass + wire into Run), qwdi4i7 (Implement Lambda /day past-date storage read + skip-readings-query), qwdi4i9 (Implement Lambda /history per-row derived-stats + today live-compute + today error tolerance)
  - Stream: 1
  - Requirements: [2.5](requirements.md#2.5), [4.7](requirements.md#4.7), [4.8](requirements.md#4.8)
  - References: internal/api/history_bench_test.go, internal/dynamo/sizing_test.go
