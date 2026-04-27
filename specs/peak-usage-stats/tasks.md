---
references:
    - specs/peak-usage-stats/requirements.md
    - specs/peak-usage-stats/design.md
    - specs/peak-usage-stats/decision_log.md
---
# Tasks: Peak Usage Stats

- [ ] 1. Add DailyUsage and DailyUsageBlock Go response types <!-- id:31p9ruh -->
  - Add to internal/api/response.go: DailyUsage struct with Blocks []DailyUsageBlock; DailyUsageBlock struct with Kind, Start, End (string), TotalKwh (float64), AverageKwhPerHour (*float64 with omitempty), PercentOfDay (int), Status, BoundarySource (all string)
  - Add constants DailyUsageStatusComplete/InProgress, DailyUsageBoundaryReadings/Estimated, DailyUsageKindNight/MorningPeak/OffPeak/AfternoonPeak/Evening
  - Replace EveningNight field on DayDetailResponse with DailyUsage *DailyUsage `json:"dailyUsage,omitempty"`; delete the old EveningNight field, EveningNightStatus*/Boundary* constants, and EveningNight + EveningNightBlock structs (response.go lines 107-142)
  - Type-only; no test pairing required
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4)

- [ ] 2. Write tests for findDailyUsage covering all req 4.1 fixtures <!-- id:31p9rui -->
  - Add TestFindDailyUsage (map-based table) to internal/api/compute_test.go
  - Cover all AC 4.1 fixtures: typical past day; today before sunrise; today mid-morning-peak; today during off-peak; today mid-afternoon-peak with sun up; today cloudy late afternoon (solar stopped 90 min ago, today-gate does NOT fire); today after sunset; today + overcast morning mid-morning request (in-progress morningPeak with boundarySource=estimated); overcast complete day; partial-data after-offpeak (5-block); partial-data during-offpeak (2-block per Decision 11); off-peak SSM misconfigured; daily-power-fallback path (dailyUsage nil); solar-window invariant violated (sunrise > offpeakStart); single-solar (firstSolar==lastSolar); DST spring-forward; DST fall-back; pre-sunrise blip filtered; post-sunset blip filtered; today + off-peak misconfigured (today-gate still applies on 2-block path); future-dated request
  - Each case asserts the exact set of emitted block kinds and the boundarySource and status for each
  - Reuse the fixture-builder helper near compute_test.go:1630 (rename for the new function family)
  - Tests must initially fail (function not yet implemented)
  - Blocked-by: 31p9ruh (Add DailyUsage and DailyUsageBlock Go response types)
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9), [1.10](requirements.md#1.10), [1.11](requirements.md#1.11), [4.1](requirements.md#4.1)

- [ ] 3. Implement findDailyUsage and buildDailyUsageBlock helper <!-- id:31p9ruj -->
  - Add `recentSolarThreshold = 5 * time.Minute` constant to internal/api/compute.go near preSunriseBlipBuffer
  - Add findDailyUsage(readings, offpeakStart, offpeakEnd, date, today string, now time.Time) *DailyUsage following the design 11-step algorithm: single pass tracking firstSolar/lastSolar with closed [sunrise-30, sunset+30] window and recentSolar/hasQualifyingPpv flags; resolve fallbacks; solar-window guard with two-block degradation; build nominal intervals; today-gate with statusOverride sentinel; future-omit; in-progress clamp; degenerate-omit; resolve boundarySource from per-edge (startEstimated, endEstimated) bools per the design table; two-pass integration (per-block integratePload, then sum, then per-block percentOfDay)
  - Add buildDailyUsageBlock(p pendingBlock, unroundedSum float64) DailyUsageBlock as pure formatter
  - Use a local pendingBlock struct to carry kind, start, end, startEstimated, endEstimated, status, unroundedKwh between passes
  - Delete findEveningNight and buildEveningNightBlock (compute.go lines 640-781) once findDailyUsage compiles
  - Update the stale doc comments at compute.go:488 and :504 inside melbourneSunriseSunset to reference buildDailyUsageBlock (or generalise the comment)
  - Make TestFindDailyUsage pass
  - Blocked-by: 31p9rui (Write tests for findDailyUsage covering all req 4.1 fixtures)
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9), [1.10](requirements.md#1.10), [1.11](requirements.md#1.11), [1.12](requirements.md#1.12), [1.13](requirements.md#1.13), [1.14](requirements.md#1.14)

- [ ] 4. Wire findDailyUsage into handleDay and update day_test.go integration cases <!-- id:31p9ruk -->
  - In internal/api/day.go: replace `var eveningNight *EveningNight` (line 64), `eveningNight = findEveningNight(readings, date, today, now)` (line 71), and `EveningNight: eveningNight` (line 90) with the dailyUsage equivalents that pass h.offpeakStart and h.offpeakEnd as additional arguments. day.go already computes today and now locals; reuse them
  - Update TestHandleDayNormalCase in day_test.go to assert resp.DailyUsage is non-nil with len(blocks) > 0 (delete the existing eveningNight assertion)
  - Rename TestHandleDayEveningNightPerBlockFallback (day_test.go line ~92) to TestHandleDayDailyUsageOvercast and rewrite it to assert the AC 4.1 overcast fixture expected output
  - Update the fallback-path test (assert.Nil reference around line 183-185) to assert dailyUsage is nil
  - Blocked-by: 31p9ruj (Implement findDailyUsage and buildDailyUsageBlock helper)
  - Stream: 1
  - Requirements: [1.10](requirements.md#1.10), [2.1](requirements.md#2.1), [2.4](requirements.md#2.4)

- [ ] 5. Add BenchmarkFindDailyUsage <!-- id:31p9rul -->
  - Mirror BenchmarkFindEveningNight: 8640-reading fixture, B.Loop pattern, -benchmem
  - Acceptance bar: same order of magnitude as findEveningNight (single pass + five integrations vs two)
  - Blocked-by: 31p9ruj (Implement findDailyUsage and buildDailyUsageBlock helper)
  - Stream: 1

- [ ] 6. Add Swift DailyUsage types and update DayDetailResponse <!-- id:31p9rum -->
  - In Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift: add public struct DailyUsage with `let blocks: [DailyUsageBlock]`; add public struct DailyUsageBlock with Kind enum (night, morningPeak, offPeak, afternoonPeak, evening — case names with explicit JSON raw values), Status enum (complete, inProgress = "in-progress"), BoundarySource enum (readings, estimated); fields kind, start, end, totalKwh, averageKwhPerHour (Double?), percentOfDay (Int), status, boundarySource; Identifiable with `id: String { kind.rawValue }`
  - Update DayDetailResponse: replace `eveningNight: EveningNight?` with `dailyUsage: DailyUsage?`; update the memberwise init signature; delete the old EveningNight and EveningNightBlock structs (APIModels.swift lines 255-304)
  - Type-only; no test pairing required
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3)

- [ ] 7. Update FluxCore decode tests for DailyUsage <!-- id:31p9run -->
  - Delete decodeDayDetailResponseWithEveningNightBothBlocks and any other decodeDayDetailResponseWithEveningNight* tests in Flux/Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift
  - Add: decode response with dailyUsage containing all five blocks (assert kind, percentOfDay, boundarySource, status on each); decode with dailyUsage absent (nil); decode with two-block-only response (off-peak misconfigured shape, blocks.count == 2); decode with averageKwhPerHour: null (Swift nil)
  - Update the two literal `DayDetailResponse(... eveningNight: nil)` call sites at StatusTimelineLogicTests.swift:374 and :397 to pass `dailyUsage: nil` instead
  - Tests must initially fail until task 6 completes
  - Blocked-by: 31p9rum (Add Swift DailyUsage types and update DayDetailResponse)
  - Stream: 2
  - Requirements: [4.3](requirements.md#4.3), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2)

- [ ] 8. Update DayDetailViewModel and its tests for dailyUsage <!-- id:31p9ruo -->
  - Rewrite DayDetailViewModelTests.swift: rewrite the five test functions loadDayPopulatesEveningNightFromResponse, loadDayPropagatesEveningNightWithOnlyOneBlock, loadDayWithNilEveningNightLeavesPropertyNil, loadDayFallbackDataPathLeavesEveningNightAsBackendSent, loadDayErrorResetsEveningNightToNil into their dailyUsage equivalents (loadDayPopulatesDailyUsageFromResponse with a five-block fixture; loadDayPropagatesDailyUsageWithTwoBlocks for the off-peak-misconfigured shape; loadDayWithNilDailyUsageLeavesPropertyNil; loadDayFallbackDataPathLeavesDailyUsageAsBackendSent; loadDayErrorResetsDailyUsageToNil)
  - Update the 10 literal `DayDetailResponse(... eveningNight: nil)` call sites at DayDetailViewModelTests.swift lines 19, 65, 79, 93, 107, 142, 169, 183, 202, 227 to pass `dailyUsage: nil` (or a fixture for the rewritten tests)
  - Replace the EveningNight(...) and EveningNightBlock(...) constructor literals at lines 124, 125, 133, 158, 160, 216, 217 with DailyUsage(blocks: [DailyUsageBlock(...)]) calls
  - In Flux/Flux/DayDetail/DayDetailViewModel.swift: replace `eveningNight: EveningNight?` (line 28), `eveningNight = response.eveningNight` (line 60), and `eveningNight = nil` (line 68) with the dailyUsage equivalents
  - Tests must compile and pass after this task
  - Blocked-by: 31p9rum (Add Swift DailyUsage types and update DayDetailResponse), 31p9run (Update FluxCore decode tests for DailyUsage)
  - Stream: 2
  - Requirements: [4.3](requirements.md#4.3), [1.1](requirements.md#1.1)

- [ ] 9. Implement DailyUsageCard SwiftUI view and wire into DayDetailView <!-- id:31p9rup -->
  - Create Flux/Flux/DayDetail/DailyUsageCard.swift implementing the design view layout: title "Daily Usage", one row per block in received order; each row has label per AC 3.4 mapping, time range with caption rendered positionally adjacent to the estimated edge using @ViewBuilder + HStack(spacing: 4) (do NOT use Text+Text concatenation), totals (kWh + kWh/h), percentage, and (so far) indicator when in-progress
  - Caption is rendered only when boundarySource == .estimated (the in-progress night case naturally falls out as boundarySource = readings)
  - Use the existing EveningNightCard styling (.thinMaterial, RoundedRectangle(cornerRadius: 16, style: .continuous), subheadline rows, caption secondary text)
  - Include a #Preview block populated with a five-block fixture for Xcode previews
  - Delete Flux/Flux/DayDetail/EveningNightCard.swift (entire file)
  - In Flux/Flux/DayDetail/DayDetailView.swift lines 35-39: replace the existing EveningNightCard guard with `if viewModel.hasPowerData, let du = viewModel.dailyUsage, !du.blocks.isEmpty { DailyUsageCard(dailyUsage: du) }`
  - View-layer code; AC 4.3 caption-rendering check is at view-model construction time only (acknowledged untested at view layer, matches precedent)
  - Blocked-by: 31p9rum (Add Swift DailyUsage types and update DayDetailResponse), 31p9ruo (Update DayDetailViewModel and its tests for dailyUsage)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)

- [ ] 10. Update MockFluxAPIClient preview fixture for dailyUsage <!-- id:31p9ruq -->
  - In Flux/Flux/Services/MockFluxAPIClient.swift: replace the `dayEveningNight` helper with `dayDailyUsage`; build a realistic five-block fixture (night, morningPeak, offPeak, afternoonPeak, evening with reasonable kWh/percentOfDay/timestamps) so the SwiftUI preview at DayDetailView.swift:213 renders the new card with all five rows
  - Update the literal DayDetailResponse(...) call site at MockFluxAPIClient.swift:86 to pass `dailyUsage: ...` instead of `eveningNight: nil`
  - Blocked-by: 31p9rum (Add Swift DailyUsage types and update DayDetailResponse)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2)
