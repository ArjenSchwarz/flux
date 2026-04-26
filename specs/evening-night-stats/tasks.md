---
references:
    - specs/evening-night-stats/requirements.md
    - specs/evening-night-stats/design.md
    - specs/evening-night-stats/decision_log.md
---
# Tasks: Evening / Night Stats

- [x] 1. Add EveningNight and EveningNightBlock Go response types <!-- id:qxucsl2 -->
  - Add EveningNight struct with Evening *EveningNightBlock and Night *EveningNightBlock fields (both omitempty) to internal/api/response.go
  - Add EveningNightBlock with Start, End (string RFC 3339 UTC), TotalKwh (float64), AverageKwhPerHour (*float64), Status (string), BoundarySource (string)
  - Add EveningNight *EveningNight `json:"eveningNight,omitempty"` field to DayDetailResponse
  - Type-only; no test pairing required
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2)
  - References: internal/api/response.go, specs/evening-night-stats/design.md

- [x] 2. Generate and commit Melbourne sun lookup table <!-- id:qxucsl3 -->
  - Create internal/api/melbourne_sun_table.go containing var melbourneSunUTC = map[string]struct{ riseUTC, setUTC string }{ ... } with 366 entries (MM-DD keys 01-01 through 12-31; Feb 29 omitted intentionally so lookup falls through to Feb 28 in code)
  - Values are Sydney-local-clock-style HH:MM strings (wall-clock times that time.ParseInLocation will interpret in sydneyTZ to produce the correct UTC instant for any given calendar date — DST-immune by construction)
  - Top-of-file comment records the generation date and the astronomical reference used
  - Data file; no test pairing required (the table is the source of truth; sanity is asserted in the next task)
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5), [1.12](requirements.md#1.12)
  - References: specs/evening-night-stats/design.md, specs/evening-night-stats/decision_log.md

- [x] 3. Write sanity tests for melbourneSunriseSunset <!-- id:qxucsl4 -->
  - Add TestMelbourneSunriseSunset to internal/api/compute_test.go
  - Cases: 2026-06-21 (winter solstice) — sunset between 16:30 and 17:30 AEST; 2026-12-22 (summer solstice) — sunset between 20:00 and 21:00 AEDT; 2027-02-29 leap-year fallback to Feb 28 values; 2026-04-05 near AEDT-end DST transition resolves to a UTC instant on the correct local date
  - Tests must initially fail (function not yet implemented)
  - Blocked-by: qxucsl3 (Generate and commit Melbourne sun lookup table)
  - Stream: 1
  - Requirements: [4.2](requirements.md#4.2)
  - References: internal/api/compute_test.go

- [x] 4. Implement melbourneSunriseSunset <!-- id:qxucsl5 -->
  - Add melbourneSunriseSunset(date string, isSunrise bool) time.Time to internal/api/compute.go
  - Implementation: lookup MM-DD in melbourneSunUTC; on miss, fall back to "02-28"; combine the looked-up HH:MM with sydneyTZ-local midnight via time.ParseInLocation, return UTC truncated to the second
  - Make TestMelbourneSunriseSunset pass
  - Blocked-by: qxucsl4 (Write sanity tests for melbourneSunriseSunset)
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5), [1.12](requirements.md#1.12)
  - References: internal/api/compute.go

- [x] 5. Write tests for integratePload including the design's worked example <!-- id:qxucsl6 -->
  - Add TestIntegratePload (map-based table) to internal/api/compute_test.go
  - Cases: the worked example from design.md (readings t=0,10,20,30 with plouds 200,400,-100,600; period [15,25); expected ≈0.000347 kWh); half-open boundary on exact-timestamp readings (start and end); negative-pload clamp before interpolation; 60s pair-gap skip at brackets; left/right edge missing (start before all readings / end after all readings); single interior reading; zero usable points returns 0
  - Tests must initially fail
  - Stream: 1
  - Requirements: [1.6](requirements.md#1.6)
  - References: internal/api/compute_test.go

- [x] 6. Implement integratePload <!-- id:qxucsl7 -->
  - Add integratePload(readings []dynamo.ReadingItem, startUnix, endUnix int64) float64 to internal/api/compute.go
  - Half-open [startUnix, endUnix); clamp pload to max(p,0) before interpolation; left/right edge synthesis with 60s bracket-gap skip; trapezoidal sum across adjacent pairs in pts with the per-pair 60s skip retained; return kWh (watt-seconds / 3,600,000)
  - Make TestIntegratePload pass
  - Blocked-by: qxucsl6 (Write tests for integratePload including the design's worked example)
  - Stream: 1
  - Requirements: [1.6](requirements.md#1.6)
  - References: internal/api/compute.go

- [x] 7. Write tests for findEveningNight covering all req 4.1 scenarios <!-- id:qxucsl8 -->
  - Add TestFindEveningNight (map-based table) to internal/api/compute_test.go
  - Cases per req 4.1: typical past day complete; today before sunrise (only night, in-progress, end clamped to now); today after sunset (both blocks, evening in-progress); today midday (only night — evening omitted by today gate); fully overcast day no Ppv>0 (per-block fallback to estimated); morning solar but no afternoon (night=readings, evening=readings using lastPpvPositive=12:55 per spec); zero readings inside period (block emitted, totalKwh=0); 60s gap inside period; in-progress evening elapsed<60s (averageKwhPerHour=nil); future-date / no-readings caller-skip path
  - Verify the today-gate distinguishes "midday with sun up" from "evening in progress"
  - Tests must initially fail
  - Blocked-by: qxucsl2 (Add EveningNight and EveningNightBlock Go response types)
  - Stream: 1
  - Requirements: [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9), [4.1](requirements.md#4.1)
  - References: internal/api/compute_test.go, specs/evening-night-stats/design.md

- [x] 8. Implement findEveningNight and buildEveningNightBlock helper <!-- id:qxucsl9 -->
  - Add findEveningNight(readings []dynamo.ReadingItem, date, today string, now time.Time) *EveningNight to internal/api/compute.go
  - Add buildEveningNightBlock helper (start, end time.Time, boundarySource, status string, readings) returning *EveningNightBlock with totalKwh from integratePload, AverageKwhPerHour nil when elapsed<60s
  - Implement step 4 (night) and step 5 (evening) per design including today-gate for evening (omit when now <= melbourneSunriseSunset(date, false)), today-clamp for both (end = min(nominalEnd, now)), and start>=end final guards
  - Make TestFindEveningNight pass
  - Blocked-by: qxucsl5 (Implement melbourneSunriseSunset), qxucsl7 (Implement integratePload), qxucsl8 (Write tests for findEveningNight covering all req 4.1 scenarios)
  - Stream: 1
  - Requirements: [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9)
  - References: internal/api/compute.go

- [x] 9. Wire findEveningNight into handleDay and extend day_test.go integration cases <!-- id:qxucsla -->
  - In internal/api/day.go: after the existing findPeakPeriods call, when len(readings)>0, call findEveningNight(readings, date, today, now) and assign to resp.EveningNight (nil pointer means JSON omits via omitempty)
  - day.go already computes today and now locals; reuse them
  - Extend TestHandleDayNormalCase to assert eveningNight present and non-nil for a typical day with readings
  - Add fixture exercising per-block fallback on a partial-data day
  - Extend the daily-power-fallback test to assert eveningNight is absent
  - Blocked-by: qxucsl9 (Implement findEveningNight and buildEveningNightBlock helper)
  - Stream: 1
  - Requirements: [1.10](requirements.md#1.10), [1.11](requirements.md#1.11), [1.13](requirements.md#1.13), [2.3](requirements.md#2.3)
  - References: internal/api/day.go, internal/api/day_test.go

- [x] 10. Add BenchmarkFindEveningNight <!-- id:qxucslb -->
  - Mirror BenchmarkFindPeakPeriods: 8640 reading-fixture, run findEveningNight in B.Loop pattern, include -benchmem
  - Blocked-by: qxucsl9 (Implement findEveningNight and buildEveningNightBlock helper)
  - Stream: 1
  - References: internal/api/compute_test.go

- [x] 11. Add Swift EveningNight types and update DayDetailResponse with decode tests and literal call-site fixes <!-- id:qxucslc -->
  - In Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift: add public struct EveningNight (evening, night optional) with hasAnyBlock computed property; add public struct EveningNightBlock with Status enum (complete, inProgress = "in-progress") and BoundarySource enum (readings, estimated)
  - Add public let eveningNight: EveningNight? to DayDetailResponse and extend its synthesised init parameter list
  - Update every existing DayDetailResponse(...) literal to pass eveningNight: nil. Known sites: Packages/FluxCore/Tests/FluxCoreTests/StatusTimelineLogicTests.swift (~lines 374, 397) and Flux/FluxTests/DayDetailViewModelTests.swift (~lines 19, 65, 79, 93, 107). Re-grep before merging in case more have been added.
  - Extend Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift with: decode with both blocks present; decode with eveningNight key absent; decode with only one block (other-key absent); decode with averageKwhPerHour: null; decode with boundarySource "estimated"
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [2.1](requirements.md#2.1), [2.3](requirements.md#2.3)
  - References: Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift, Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift, Packages/FluxCore/Tests/FluxCoreTests/StatusTimelineLogicTests.swift, Flux/FluxTests/DayDetailViewModelTests.swift

- [x] 12. Write DayDetailViewModel tests for eveningNight handling <!-- id:qxucsld -->
  - In Flux/FluxTests/DayDetailViewModelTests.swift: cases per req 4.3 — both blocks populated, only one block, eveningNight absent → viewModel.eveningNight == nil, response with boundarySource "estimated", fallback-data path → eveningNight nil, error path → eveningNight reset to nil
  - Tests must initially fail (no eveningNight property yet)
  - Blocked-by: qxucslc (Add Swift EveningNight types and update DayDetailResponse with decode tests and literal call-site fixes)
  - Stream: 2
  - Requirements: [4.3](requirements.md#4.3)
  - References: Flux/FluxTests/DayDetailViewModelTests.swift

- [x] 13. Add eveningNight property and loadDay wiring to DayDetailViewModel <!-- id:qxucsle -->
  - In Flux/Flux/DayDetail/DayDetailViewModel.swift: add private(set) var eveningNight: EveningNight?; in loadDay() success path set eveningNight = response.eveningNight; in error path reset to nil
  - Make the new tests pass
  - Blocked-by: qxucsld (Write DayDetailViewModel tests for eveningNight handling)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)
  - References: Flux/Flux/DayDetail/DayDetailViewModel.swift

- [x] 14. Implement EveningNightCard SwiftUI view and update MockFluxAPIClient preview fixture <!-- id:qxucslf -->
  - Create Flux/Flux/DayDetail/EveningNightCard.swift implementing the two-line row layout from design.md (line 1: label leading, time-range trailing; line 2: secondary caption leading, totals trailing). Caption rules: status .inProgress → "(so far)" suppressing the boundary caption; status .complete && boundarySource .estimated → "≈ sunset" (evening row) or "≈ sunrise" (night row); else empty caption. Totals format "%.1f kWh · %.2f kWh/h" with average omitted when nil. Time range via DateFormatting.clockTime24h with sydneyTZ.
  - Style: .thinMaterial background, RoundedRectangle(cornerRadius: 16, style: .continuous), .headline title, .subheadline body, .caption .secondary for line-2 caption
  - Title "Evening / Night" regardless of which blocks are present; row order Night then Evening
  - Add #Preview using MockFluxAPIClient sample data; extend MockFluxAPIClient preview DayDetailResponse fixture so EveningNightCard renders in DayDetailView preview
  - Blocked-by: qxucslc (Add Swift EveningNight types and update DayDetailResponse with decode tests and literal call-site fixes)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5)
  - References: Flux/Flux/DayDetail/EveningNightCard.swift, Flux/Flux/DayDetail/PeakUsageCard.swift, Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift

- [x] 15. Wire EveningNightCard into DayDetailView <!-- id:qxucslg -->
  - In Flux/Flux/DayDetail/DayDetailView.swift: after the PeakUsageCard guard and before the summaryCard, add the conditional render: if viewModel.hasPowerData, let en = viewModel.eveningNight, en.hasAnyBlock { EveningNightCard(eveningNight: en) }
  - Verify the placement renders correctly in the SwiftUI preview
  - Blocked-by: qxucsle (Add eveningNight property and loadDay wiring to DayDetailViewModel), qxucslf (Implement EveningNightCard SwiftUI view and update MockFluxAPIClient preview fixture)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7)
  - References: Flux/Flux/DayDetail/DayDetailView.swift
