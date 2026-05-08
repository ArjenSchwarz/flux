---
references:
    - requirements.md
    - design.md
    - decision_log.md
---
# Tasks: Solar by Block (T-1162)

## Backend — derivedstats core

- [ ] 1. Write tests for integratePpv returning (kwh, sampleCount) <!-- id:967mk7j -->
  - File: internal/derivedstats/integrate_ppv_test.go (new)
  - Cases: empty readings; single reading inside window; >=2 readings inside window; gap >60s skipped; left-edge synthesis; right-edge synthesis; negative ppv clamped to zero
  - Each case asserts both kwh and sampleCount returns
  - Stream: 1
  - Requirements: [1.3](requirements.md#1.3)

- [ ] 2. Implement integratePpv <!-- id:967mk7k -->
  - File: internal/derivedstats/integrate.go (or sibling integrate_ppv.go)
  - Signature: func integratePpv(readings []Reading, startUnix, endUnix int64) (kwh float64, sampleCount int)
  - Algorithm mirrors integratePload exactly — 60s pair-gap rule, edge synthesis, half-open [start, end), max(ppv, 0) clamping
  - sampleCount counts readings whose Timestamp is in [startUnix, endUnix); synthesised edge points do not count
  - Blocked-by: 967mk7j (Write tests for integratePpv returning (kwh, sampleCount))
  - Stream: 1
  - Requirements: [1.3](requirements.md#1.3)

- [ ] 3. Add property-based test for integratePpv split-additivity <!-- id:967mk7l -->
  - File: internal/derivedstats/integrate_ppv_property_test.go (new)
  - pgregory.net/rapid generator: sorted readings with bounded inter-sample gap and a random split point b
  - Property: when no >60s gap straddles b and the integration domain is gap-free, integratePpv([a,c)) ~= integratePpv([a,b)) + integratePpv([b,c)) within float epsilon
  - Blocked-by: 967mk7k (Implement integratePpv)
  - Stream: 1
  - Requirements: [1.3](requirements.md#1.3)

- [ ] 4. Write tests for Blocks() SolarKwh emission per AC 4.1 cases <!-- id:967mk7m -->
  - File: internal/derivedstats/blocks_test.go (extend)
  - Cases: sunny day full readings; winter low-solar; reading gap inside daylight block; morning peak collapsed (no entry produced); in-progress today daylight block straddling now (clamped to elapsed portion); daylight block with no readings (nil); daylight block all-zero ppv readings >=1 sample (=> &0.0); daylight block single reading (sampleCount=1 + integration=0 => &0.0); night and evening always nil
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5)

- [ ] 5. Extend pendingBlock and Blocks() to emit SolarKwh <!-- id:967mk7n -->
  - Files: internal/derivedstats/types.go, internal/derivedstats/blocks.go
  - Add SolarKwh *float64 with json tag solarKwh,omitempty to DailyUsageBlock
  - Add unroundedSolarKwh float64, solarSampled bool to pendingBlock
  - In Blocks(): for morningPeak/offPeak/afternoonPeak only, call integratePpv over [p.start.Unix(), p.end.Unix()) and store result + sampleCount>0 flag
  - buildDailyUsageBlock emits SolarKwh as rounded *float64 when solarSampled, else nil
  - Blocked-by: 967mk7k (Implement integratePpv), 967mk7m (Write tests for Blocks() SolarKwh emission per AC 4.1 cases)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5)

## Backend — DynamoDB persistence and converters

- [ ] 6. Write tests for DailyUsageToAttr/FromAttr round-tripping SolarKwh <!-- id:967mk7o -->
  - Files: internal/dynamo/derived_conv_test.go (extend), internal/dynamo/derived_conv_property_test.go (extend)
  - Cases: nil SolarKwh round-trips to nil; non-nil round-trips to same value
  - Property generator emits SolarKwh as Maybe[float64]
  - Blocked-by: 967mk7n (Extend pendingBlock and Blocks() to emit SolarKwh)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.6](requirements.md#2.6)

- [ ] 7. Add SolarKwh field to DailyUsageBlockAttr and copy in both converters <!-- id:967mk7p -->
  - Files: internal/dynamo/models.go, internal/dynamo/derived_conv.go
  - Tag: dynamodbav:"solarKwh,omitempty"
  - DailyUsageFromAttr and DailyUsageToAttr each gain an explicit SolarKwh: b.SolarKwh, line in the loop body
  - Blocked-by: 967mk7o (Write tests for DailyUsageToAttr/FromAttr round-tripping SolarKwh)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.6](requirements.md#2.6)

- [ ] 8. Update sizing test fixture to include SolarKwh on daylight blocks <!-- id:967mk7q -->
  - File: internal/dynamo/sizing_test.go
  - Populate SolarKwh on the three daylight blocks of the post-feature fixture
  - Re-assert size stays <4 KB
  - Blocked-by: 967mk7p (Add SolarKwh field to DailyUsageBlockAttr and copy in both converters)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1)

## Backend — API and poller test coverage

- [ ] 9. Extend day_derivedstats_test for solarKwh on daylight blocks <!-- id:967mk7r -->
  - File: internal/api/day_derivedstats_test.go
  - Today path and past-day path both assert daylight blocks carry solarKwh
  - Night and evening blocks assert solarKwh is absent
  - Blocked-by: 967mk7p (Add SolarKwh field to DailyUsageBlockAttr and copy in both converters)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [2.6](requirements.md#2.6)

- [ ] 10. Extend history test fixtures with SolarKwh <!-- id:967mk7s -->
  - Files: internal/api/history_bench_test.go, internal/api/history_derivedstats_test.go
  - Populate SolarKwh on daylight blocks in fixtures so coverage stays current
  - Blocked-by: 967mk7p (Add SolarKwh field to DailyUsageBlockAttr and copy in both converters)
  - Stream: 1
  - Requirements: [2.6](requirements.md#2.6)

- [ ] 11. Extend poller summarisation test for SolarKwh persistence <!-- id:967mk7t -->
  - File: internal/poller/dailysummary_test.go
  - With synthetic ppv readings, assert daylight blocks in the resulting DailyUsageAttr carry SolarKwh values written via the existing UpdateDailyEnergyDerived path
  - Blocked-by: 967mk7p (Add SolarKwh field to DailyUsageBlockAttr and copy in both converters)
  - Stream: 1
  - Requirements: [2.2](requirements.md#2.2)

- [ ] 12. Extend e2e integration test to assert SolarKwh round-trips through DynamoDB Local <!-- id:967mk7u -->
  - File: internal/integration/derivedstats_e2e_test.go
  - Synthetic readings produce SolarKwh; after MarshalMap into DynamoDB Local and a real read, the value is preserved
  - Blocked-by: 967mk7p (Add SolarKwh field to DailyUsageBlockAttr and copy in both converters)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.6](requirements.md#2.6)

## Backfill CLI

- [ ] 13. Write tests for backfill CLI (cmd/backfill-solar) <!-- id:967mk7v -->
  - File: cmd/backfill-solar/main_test.go (new)
  - Dry-run prints intended writes without invoking UpdateItem
  - Live run writes once per row needing update; rows already populated are skipped (idempotency)
  - In-place patch preservation: existing block totalKwh, start, end, boundarySource, percentOfDay survive byte-for-byte even when recomputed Blocks() yields different values
  - Blocked-by: 967mk7p (Add SolarKwh field to DailyUsageBlockAttr and copy in both converters)
  - Stream: 2
  - Requirements: [2.4](requirements.md#2.4), [2.5](requirements.md#2.5)

- [ ] 14. Implement backfill CLI <!-- id:967mk7w -->
  - File: cmd/backfill-solar/main.go (new)
  - Standalone main package; flags: --dry-run, --from, --to, --serial (or env)
  - Date parsing via time.ParseInLocation in sydneyTZ
  - Algorithm: query flux-daily-energy rows; for each row with dailyUsage and missing SolarKwh on at least one daylight block, query flux-readings, run derivedstats.Blocks with today=date and now=time.Now().In(sydneyTZ), patch SolarKwh by Kind onto the existing stored blocks, write back via SET dailyUsage = :du
  - Serial writes; no concurrency
  - Blocked-by: 967mk7v (Write tests for backfill CLI (cmd/backfill-solar))
  - Stream: 2
  - Requirements: [2.4](requirements.md#2.4), [2.5](requirements.md#2.5)

## iOS

- [ ] 15. Add solarKwh: Double? to iOS DailyUsageBlock with default nil in init <!-- id:967mk7x -->
  - File: Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift
  - Property declared after totalKwh, before averageKwhPerHour
  - Init signature adds solarKwh: Double? = nil parameter at the matching position so existing call sites compile unchanged
  - Blocked-by: 967mk7p (Add SolarKwh field to DailyUsageBlockAttr and copy in both converters)
  - Stream: 3
  - Requirements: [1.1](requirements.md#1.1), [2.6](requirements.md#2.6)

- [ ] 16. Write decoder tests for solarKwh covering present, zero, null, absent <!-- id:967mk7y -->
  - File: Flux/Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift (extend)
  - Fixtures: solarKwh: 1.23 decodes to 1.23; solarKwh: 0.0 decodes to 0.0; solarKwh: null decodes to nil; key absent decodes to nil
  - Blocked-by: 967mk7x (Add solarKwh: Double? to iOS DailyUsageBlock with default nil in init)
  - Stream: 3
  - Requirements: [2.6](requirements.md#2.6), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5)

- [ ] 17. Update DayInFiveBlocksPanel to render solar inline on daylight rows <!-- id:967mk7z -->
  - File: Flux/Flux/DayDetail/DayInFiveBlocksPanel.swift
  - In row(), on .morningPeak/.offPeak/.afternoonPeak only, append a sun icon (SF symbol sun.max.fill) tinted FluxTheme.Palette.amber + EnergyFormatting.format(block.solarKwh!) when solarKwh is non-nil
  - When solarKwh is nil, render no icon and no value (per 3.4)
  - Night and evening rows unchanged
  - Update the file #Preview to populate sample solarKwh values on the three daylight blocks
  - Blocked-by: 967mk7x (Add solarKwh: Double? to iOS DailyUsageBlock with default nil in init), 967mk7y (Write decoder tests for solarKwh covering present, zero, null, absent)
  - Stream: 3
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6)

## Cross-cutting

- [ ] 18. Document UpdateDailyEnergyDerived as the sole writer for dailyUsage outside the backfill CLI <!-- id:967mk80 -->
  - File: internal/dynamo/dynamostore.go
  - One-line invariant comment near UpdateDailyEnergyDerived noting it is the only write path for the dailyUsage attribute outside the cmd/backfill-solar CLI
  - Blocked-by: 967mk7p (Add SolarKwh field to DailyUsageBlockAttr and copy in both converters)
  - Stream: 1
  - Requirements: [2.2](requirements.md#2.2), [2.3](requirements.md#2.3)
