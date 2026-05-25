---
references:
    - specs/offpeak-from-readings/requirements.md
    - specs/offpeak-from-readings/design.md
    - specs/offpeak-from-readings/decision_log.md
---
# Tasks: Off-Peak From Readings (T-1341)

## Foundation

- [ ] 1. Test IntegrateOffpeakDeltas including property-based tests <!-- id:tyhiuft -->
  - Unit tests for: happy path, single sample (returns false), bracketing sample at window edge with 60s gap rule, 90s gap inside window (SkippedPairs == 1), signed pgrid producing per-sample import/export split, reading exactly at endUnix excluded as interior but used as right-edge bracket, gridUsage+peak==eInput invariant (AC 2.3)
  - Property tests with pgregory.net/rapid: closure under window (Grid+Grid <= integral abs pgrid), monotonicity over window growth (AC 4.4), per-sample clamping symmetry, round-trip idempotence, len<2 always false
  - Place in internal/derivedstats/integrate_offpeak_test.go
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [2.3](requirements.md#2.3)

- [ ] 2. Implement IntegrateOffpeakDeltas and shared integrate helper <!-- id:tyhiufu -->
  - Create internal/derivedstats/integrate_offpeak.go
  - Export OffpeakDeltas struct with five kWh deltas + SampleCount + SkippedPairs
  - Export IntegrateOffpeakDeltas(readings, startUnix, endUnix) (OffpeakDeltas, bool)
  - Internal integrate(readings, startUnix, endUnix, selector) float64 with same control flow as integratePpv; sample-count tallying removed (callers handle once); len(pts)>=2 gate post-construction
  - Five selectors: max(pgrid,0), max(-pgrid,0), max(-pbat,0), max(pbat,0), max(ppv,0)
  - SampleCount + SkippedPairs computed once in a single pass over in-window readings before the five integrate calls
  - Blocked-by: tyhiuft (Test IntegrateOffpeakDeltas including property-based tests)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6)

- [ ] 3. Add TODO breadcrumbs to existing integrate siblings <!-- id:tyhiufv -->
  - Add // TODO(offpeak-from-readings): fold into integrate() in integrate_offpeak.go at the top of integratePload and integratePpv in internal/derivedstats/
  - Decision 9 breadcrumb so the consolidation work is discoverable
  - Wiring/documentation change only; no test
  - Blocked-by: tyhiufu (Implement IntegrateOffpeakDeltas and shared integrate helper)
  - Stream: 1

- [ ] 4. Add provenance fields to OffpeakItem <!-- id:tyhiufw -->
  - Add IntegrationSampleCount int; IntegrationSkippedPairs int; IntegratedAt string fields to dynamo.OffpeakItem in internal/dynamo/models.go
  - All three with dynamodbav:omitempty so old rows survive marshalling round-trip
  - Types-only change; no test
  - Stream: 1
  - Requirements: [5.4](requirements.md#5.4)

- [ ] 5. Test conditional-write methods and consistent-read query <!-- id:tyhiufx -->
  - Test WriteOffpeakIfPendingOrAbsent: succeeds when row absent; succeeds when row has status=pending; fails with ErrOffpeakConditionFailed when row has status=complete
  - Test WriteOffpeakIfComplete: succeeds when row has status=complete; fails with ErrOffpeakConditionFailed when row absent or pending
  - Test consistent-read variant of QueryReadings propagates ConsistentRead=true to the DynamoDB query input
  - Place in internal/dynamo/dynamostore_test.go and reader_test.go
  - Blocked-by: tyhiufw (Add provenance fields to OffpeakItem)
  - Stream: 1
  - Requirements: [3.5](requirements.md#3.5), [7.8](requirements.md#7.8)

- [ ] 6. Implement conditional-write methods and consistent-read query option <!-- id:tyhiufy -->
  - Add WriteOffpeakIfPendingOrAbsent and WriteOffpeakIfComplete to Store interface in internal/dynamo/store.go
  - Implement on DynamoStore with conditional expressions: attribute_not_exists(#status) OR #status=:pending; and #status=:complete
  - Add ErrOffpeakConditionFailed sentinel error
  - Add ConsistentRead option to the poller's QueryReadings path (extend Store.QueryReadings signature with opts struct OR add sibling QueryReadingsConsistent — implementation choice)
  - Existing API Lambda Reader.QueryReadings stays on eventually-consistent reads
  - Blocked-by: tyhiufx (Test conditional-write methods and consistent-read query)
  - Stream: 1
  - Requirements: [3.5](requirements.md#3.5), [7.8](requirements.md#7.8)

## Poller Integration

- [ ] 7. Test waitForReadingAtOrAfter helper <!-- id:tyhiufz -->
  - Test cases: reading at-or-after target exists immediately (returns true fast); reading arrives after 5s of polling (returns true); no reading after 30s budget expires (returns false); store error returned upstream; context cancellation aborts
  - Place in internal/poller/offpeak_test.go
  - Blocked-by: tyhiufy (Implement conditional-write methods and consistent-read query option)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1)

- [ ] 8. Implement waitForReadingAtOrAfter on OffpeakScheduler <!-- id:tyhiug0 -->
  - Add waitForReadingAtOrAfter(ctx; target time.Time; budget; pollInterval time.Duration) (found bool; err error) method on OffpeakScheduler
  - Poll the readings table every pollInterval until a reading with timestamp >= target.Unix() exists or the budget expires
  - Use the consistent-read query from task 6
  - Default budget 30s; default pollInterval 2s — overridable for tests
  - Blocked-by: tyhiufz (Test waitForReadingAtOrAfter helper)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1)

- [ ] 9. Test handleEnd readings-integration path <!-- id:tyhiug1 -->
  - Mock store + readings fixture for a heavy-charge day (modelled on 2026-05-18)
  - Test: happy path calls WriteOffpeakIfPendingOrAbsent with integration-sourced deltas; provenance fields populated; status=complete
  - Test: handleEnd preserves the existing handleStart snapshot capture (AC 5.1) — handleStart still runs at window-open and persists StartE* fields on the pending row
  - Test: timeout case (waitForReadingAtOrAfter returns false after 30s) — integration runs with whatever readings exist; row written
  - Test: WriteOffpeakIfPendingOrAbsent returns ErrOffpeakConditionFailed — logged at warn; no error returned
  - Test: QueryReadings returns empty — row written with zero deltas and SampleCount==0
  - Blocked-by: tyhiufu (Implement IntegrateOffpeakDeltas and shared integrate helper), tyhiufy (Implement conditional-write methods and consistent-read query option), tyhiug0 (Implement waitForReadingAtOrAfter on OffpeakScheduler)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [5.1](requirements.md#5.1)

- [ ] 10. Refactor poller handleEnd to integrate from readings <!-- id:tyhiug2 -->
  - Remove computeOffpeakDeltas from internal/poller/offpeak.go (or repurpose for snapshot capture only)
  - New handleEnd flow: capture end snapshot via AlphaESS (kept per Decision 2 for diagnostics); call waitForReadingAtOrAfter; QueryReadings with ConsistentRead; IntegrateOffpeakDeltas; write via WriteOffpeakIfPendingOrAbsent
  - Persisted OffpeakItem has integration-sourced deltas; snapshot startE*/endE* retained as diagnostic; provenance fields populated
  - Blocked-by: tyhiug1 (Test handleEnd readings-integration path)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3)

- [ ] 11. Test recoverMidWindow simplification and positionAfter recovery <!-- id:tyhiug3 -->
  - Test: poller restart in positionDuring with pending row — recoverMidWindow confirms row exists (no in-memory state); handleEnd runs at window-end as normal
  - Test: poller restart in positionAfter with pending row — new recovery path runs handleEnd-from-readings immediately without waitForReading; writes complete
  - Test: poller restart in positionAfter with no row — existing log+skip preserved
  - Test: poller restart in positionDuring with no row — existing log+skip preserved
  - Blocked-by: tyhiug2 (Refactor poller handleEnd to integrate from readings)
  - Stream: 1
  - Requirements: [3.3](requirements.md#3.3), [3.4](requirements.md#3.4)

- [ ] 12. Simplify recoverMidWindow and add positionAfter recovery <!-- id:tyhiug4 -->
  - Strip in-memory state rebuild from recoverMidWindow; method now confirms pending row exists so duplicate write isn't issued
  - Add recovery branch to positionAfter in Run(): GetOffpeak(today); if status=pending; run handleEnd's integration path immediately (skip waitForReading)
  - Blocked-by: tyhiug3 (Test recoverMidWindow simplification and positionAfter recovery)
  - Stream: 1
  - Requirements: [3.3](requirements.md#3.3), [3.4](requirements.md#3.4)

- [ ] 13. Test drift logging output <!-- id:tyhiug5 -->
  - Test LogOffpeakDrift emits a single INFO log line with date; snapshotGrid; integratedGrid; driftGrid; plus the same triple for solar/charge/discharge/export
  - Test format is CloudWatch Insights-friendly key=value
  - Test called from handleEnd before the write
  - Blocked-by: tyhiug2 (Refactor poller handleEnd to integrate from readings)
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)

- [ ] 14. Implement LogOffpeakDrift and wire into handleEnd <!-- id:tyhiug6 -->
  - Create LogOffpeakDrift(date string; item dynamo.OffpeakItem) in internal/poller/ (or internal/derivedstats/)
  - Compute snapshot-diff per delta from endE*-startE*; integration value from item's existing fields; drift = abs(int - snap)
  - Emit slog.Info structured log entry
  - Wire into handleEnd just before the conditional write
  - Wire into backfill CLI as well (used in task 20)
  - Blocked-by: tyhiug5 (Test drift logging output)
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)

## API Live Path

- [ ] 15. Test liveOffpeakDeltas function <!-- id:tyhiug7 -->
  - Test: now < offpeak-start returns (_; false)
  - Test: now == offpeak-start + 30min integrates the first 30 minutes
  - Test: now >= offpeak-end integrates the full window
  - Test: determinism — same readings/now/window produces same output (basis for AC 4.4 PBT)
  - Place in internal/api/compute_test.go
  - Blocked-by: tyhiufu (Implement IntegrateOffpeakDeltas and shared integrate helper)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.4](requirements.md#4.4)

- [ ] 16. Implement liveOffpeakDeltas in api/compute.go <!-- id:tyhiug8 -->
  - Add liveOffpeakDeltas(readings []dynamo.ReadingItem; now time.Time; windowStart; windowEnd time.Duration) (offpeakDeltaValues; bool)
  - Integrates over [offpeakStart; min(now; offpeakEnd))
  - Reuses IntegrateOffpeakDeltas from task 2; converts to existing offpeakDeltaValues shape
  - Pure function — no state; no clock except via the now parameter
  - Blocked-by: tyhiug7 (Test liveOffpeakDeltas function)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.4](requirements.md#4.4)

- [ ] 17. Test buildOffpeak and offpeakSplit dispatch <!-- id:tyhiug9 -->
  - Test buildOffpeak with pending row + live readings: calls liveOffpeakDeltas; does not read op.StartE*
  - Test buildOffpeak with complete row: returns op.GridUsageKwh etc. directly
  - Test offpeakSplit (history.go) with pending today row: same live dispatch
  - Test offpeakSplit with complete past row: existing pass-through
  - Test now < offpeak-start: returns (_; false) — pre-window behaviour preserved (AC 4.3)
  - Blocked-by: tyhiug8 (Implement liveOffpeakDeltas in api/compute.go)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [5.3](requirements.md#5.3)

- [ ] 18. Update api compute.go, status.go, history.go to dispatch live vs stored <!-- id:tyhiuga -->
  - Modify offpeakDeltas to handle only the complete branch — pending returns (_; false)
  - Remove all references to op.StartE* and op.EndE* from compute.go to enforce AC 5.3 at compile time
  - Modify buildOffpeak in status.go to dispatch: if pending; call liveOffpeakDeltas with today's readings slice; if complete; read row
  - Modify offpeakSplit in history.go to do the same dispatch
  - Reuse the today readings slice already in memory from the existing live-compute path
  - Blocked-by: tyhiug9 (Test buildOffpeak and offpeakSplit dispatch)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [5.3](requirements.md#5.3)

## Backfill CLI and Regression Tests

- [ ] 19. Test cmd/backfill-offpeak <!-- id:tyhiugb -->
  - Test dry-run mode: no UpdateItem calls; summary printed
  - Test idempotence: re-running with same readings produces identical values for the five deltas + two count fields; integratedAt MAY differ (AC 7.3 + Decision 10)
  - Test skips today (AC 7.2): --to defaults to yesterday; today's row never processed even with explicit --to=today
  - Test sparse-readings skip (AC 7.4): day with <2 readings in window is reported SKIPPED and row left unchanged
  - Test conditional-write failure: WriteOffpeakIfComplete returns ErrOffpeakConditionFailed → row reported; CLI exits 0
  - Test rounding consistency (AC 7.7): two runs against same readings produce byte-equal delta fields after roundEnergy
  - Test drift log emitted per row (AC 6.1)
  - Blocked-by: tyhiufu (Implement IntegrateOffpeakDeltas and shared integrate helper), tyhiufy (Implement conditional-write methods and consistent-read query option), tyhiug6 (Implement LogOffpeakDrift and wire into handleEnd)
  - Stream: 1
  - Requirements: [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3), [7.4](requirements.md#7.4), [7.5](requirements.md#7.5), [7.6](requirements.md#7.6), [7.7](requirements.md#7.7), [7.8](requirements.md#7.8)

- [ ] 20. Implement cmd/backfill-offpeak <!-- id:tyhiugc -->
  - Create cmd/backfill-offpeak/main.go modelled on cmd/backfill-solar/main.go
  - Flags: --serial; --table-offpeak; --table-readings; --from; --to (default yesterday); --offpeak-start; --offpeak-end; --dry-run
  - Per day: QueryOffpeak + QueryReadings; IntegrateOffpeakDeltas; log drift; WriteOffpeakIfComplete
  - Per-day summary line: date; prev peak/off; new peak/off; delta-peak; samples; skipped
  - Sparse-readings days emit SKIPPED line; no write
  - Blocked-by: tyhiugb (Test cmd/backfill-offpeak)
  - Stream: 1
  - Requirements: [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3), [7.4](requirements.md#7.4), [7.5](requirements.md#7.5), [7.6](requirements.md#7.6), [7.7](requirements.md#7.7), [7.8](requirements.md#7.8)

- [ ] 21. 2026-05-18 regression fixture test <!-- id:tyhiugd -->
  - Create a fixture in internal/derivedstats/testdata/ with the actual 2026-05-18 readings (13:50-14:10 captured plus synthesised rest of window)
  - Test asserts AC 2.1: after IntegrateOffpeakDeltas; computed gridUsageKwh leaves peak = eInput - gridUsageKwh <= 0.25 kWh
  - Test asserts AC 2.2 structural bound: gridUsageKwh <= eInput holds
  - Place as a top-level test in internal/derivedstats/integrate_offpeak_test.go or sibling regression_test.go
  - Blocked-by: tyhiufu (Implement IntegrateOffpeakDeltas and shared integrate helper), tyhiug2 (Refactor poller handleEnd to integrate from readings)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2)

- [ ] 22. Performance benchmark and Lambda live-path test <!-- id:tyhiuge -->
  - Add Go benchmark BenchmarkIntegrateOffpeakDeltas on 1080 synthetic readings — assert <2s wall clock (AC 8.1)
  - Add API integration test that exercises liveOffpeakDeltas through a /status or /day request handler with a fixture readings slice — record p95 in test output; assert <500ms warm (AC 8.2)
  - Blocked-by: tyhiufu (Implement IntegrateOffpeakDeltas and shared integrate helper), tyhiug8 (Implement liveOffpeakDeltas in api/compute.go)
  - Stream: 1
  - Requirements: [8.1](requirements.md#8.1), [8.2](requirements.md#8.2)
