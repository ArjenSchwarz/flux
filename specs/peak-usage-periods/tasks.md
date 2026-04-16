---
references:
    - specs/peak-usage-periods/requirements.md
    - specs/peak-usage-periods/design.md
    - specs/peak-usage-periods/decision_log.md
---
# Peak Usage Periods

## Backend Types & Constants

- [x] 1. Add PeakPeriod type and named constants to backend <!-- id:1x60rrg -->
  - Add PeakPeriod struct to response.go with json tags (start, end, avgLoadW, energyWh)
  - Add PeakPeriods []PeakPeriod field to DayDetailResponse (json tag: peakPeriods)
  - Add named constants to compute.go: mergeGapSeconds=300, minPeriodSeconds=120, maxPairGapSeconds=60, maxPeakPeriods=3
  - Run existing tests to confirm no regressions
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4)
  - References: internal/api/response.go, internal/api/compute.go

## Backend Algorithm — TDD

- [x] 2. Write unit tests for findPeakPeriods <!-- id:1x60rrh -->
  - Create map-based table-driven tests in compute_test.go following existing TestDownsample pattern
  - Test cases: empty readings, all readings in off-peak, uniform load, single peak above mean, two clusters within 5min merge, two clusters >5min separate, period under 2min discarded, more than 3 returns top 3, gap >60s skips energy pair, off-peak boundary, off-peak boundary clustering (10:59 and 14:01 must not cluster), transitive merge (A+B+C), zero-energy sparse period discarded, invalid off-peak parse failure, negative Pload clamped, two periods with same rounded energy ranked by unrounded
  - Use helper function to create readings at specific Sydney times
  - Tests will initially fail since findPeakPeriods does not exist yet
  - Blocked-by: 1x60rrg (Add PeakPeriod type and named constants to backend)
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9), [1.10](requirements.md#1.10), [1.11](requirements.md#1.11), [1.12](requirements.md#1.12)
  - References: internal/api/compute_test.go

- [x] 3. Implement findPeakPeriods to pass unit tests <!-- id:1x60rri -->
  - Implement the 5-step algorithm in compute.go as specified in design.md
  - Step 1: Parse off-peak HH:MM strings to minuteOfDay ints, validate start < end
  - Step 2: Single pass to compute mean Pload threshold from non-off-peak readings
  - Step 3: Second pass over ORIGINAL readings slice — off-peak and below-threshold both break clusters. Track startIdx/endIdx, sum, count
  - Step 4: Accumulator-based merge-intervals for transitive merges. Discard periods < minPeriodSeconds
  - Step 5: Trapezoidal integration with max(Pload,0) clamping. AvgLoadW from cluster accumulators only. Discard zero-energy. Sort by unrounded energy descending. Return top maxPeakPeriods
  - All unit tests from task 2 must pass
  - Blocked-by: 1x60rrh (Write unit tests for findPeakPeriods)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9), [1.10](requirements.md#1.10), [1.11](requirements.md#1.11), [1.12](requirements.md#1.12)
  - References: internal/api/compute.go, specs/peak-usage-periods/design.md

- [x] 4. Write property-based tests for findPeakPeriods <!-- id:1x60rrj -->
  - Add PBT tests using pgregory.net/rapid in compute_test.go
  - Properties: result count <= 3, all periods outside off-peak, non-overlapping, energy positive, descending energy order, duration >= 2 minutes
  - Generator: random ReadingItem slices spanning a day at ~10s intervals, random Pload 0-10000W, random off-peak windows with start < end
  - Blocked-by: 1x60rri (Implement findPeakPeriods to pass unit tests)
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.6](requirements.md#1.6), [1.8](requirements.md#1.8), [1.9](requirements.md#1.9)
  - References: internal/api/compute_test.go

- [x] 5. Add benchmark for findPeakPeriods <!-- id:1x60rrk -->
  - Add BenchmarkFindPeakPeriods in compute_test.go following existing BenchmarkDownsample pattern
  - Generate 8640 readings (full day at 10s intervals) with varied Pload values
  - Use b.Loop() pattern per Go testing rules
  - Blocked-by: 1x60rri (Implement findPeakPeriods to pass unit tests)
  - Stream: 1
  - Requirements: [1.13](requirements.md#1.13)
  - References: internal/api/compute_test.go

## Backend Integration

- [x] 6. Write integration tests for /day endpoint with peakPeriods <!-- id:1x60rrl -->
  - Update parseDayResponse in day_test.go to handle new PeakPeriods field
  - Update TestHandleDayNormalCase to verify peakPeriods is present and non-null
  - Update TestHandleDayFallbackToDailyPower to verify peakPeriods is empty array
  - Update TestHandleDayNoDataFromEitherSource to verify peakPeriods behaviour
  - Add test for a day with known readings producing predictable peak periods
  - Blocked-by: 1x60rrg (Add PeakPeriod type and named constants to backend)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [2.5](requirements.md#2.5)
  - References: internal/api/day_test.go

- [x] 7. Wire findPeakPeriods into handleDay <!-- id:1x60rrm -->
  - In day.go handleDay(), after findMinSOC and downsample, call findPeakPeriods(readings, h.offpeakStart, h.offpeakEnd)
  - Initialize peakPeriods to []PeakPeriod{} when nil (JSON encodes as [] not null)
  - Only call findPeakPeriods when len(readings) > 0
  - Assign to resp.PeakPeriods
  - All integration tests from task 6 must pass
  - Blocked-by: 1x60rri (Implement findPeakPeriods to pass unit tests), 1x60rrl (Write integration tests for /day endpoint with peakPeriods)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.13](requirements.md#1.13), [2.3](requirements.md#2.3), [2.5](requirements.md#2.5)
  - References: internal/api/day.go

## iOS Models

- [x] 8. Add PeakPeriod model and update DayDetailResponse in iOS <!-- id:1x60rrn -->
  - Add PeakPeriod struct to APIModels.swift: Codable, Sendable, Identifiable (id = start)
  - Fields: start (String), end (String), avgLoadW (Double), energyWh (Double)
  - Add optional peakPeriods: [PeakPeriod]? to DayDetailResponse
  - Update MockFluxAPIClient.dayDetailResponse(for:) to include sample peakPeriods
  - Update MockDayDetailAPIClient in tests to include peakPeriods in responses
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2)
  - References: Flux/Flux/Models/APIModels.swift, Flux/Flux/Services/MockFluxAPIClient.swift, Flux/FluxTests/DayDetailViewModelTests.swift

## iOS ViewModel — TDD

- [x] 9. Write unit tests for peakPeriods in DayDetailViewModel <!-- id:1x60rro -->
  - Add tests in DayDetailViewModelTests.swift following existing patterns
  - Test loadDay populates peakPeriods from response (nil-coalesces to [])
  - Test loadDay with nil peakPeriods leaves array empty
  - Test loadDay error clears peakPeriods
  - Test response without peakPeriods key decodes to nil (backward compat)
  - Blocked-by: 1x60rrn (Add PeakPeriod model and update DayDetailResponse in iOS)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2)
  - References: Flux/FluxTests/DayDetailViewModelTests.swift

- [x] 10. Add peakPeriods property to DayDetailViewModel <!-- id:1x60rrp -->
  - Add private(set) var peakPeriods: [PeakPeriod] = [] to DayDetailViewModel
  - In loadDay() success path: peakPeriods = response.peakPeriods ?? []
  - In loadDay() error path: peakPeriods = []
  - All ViewModel tests from task 9 must pass
  - Blocked-by: 1x60rro (Write unit tests for peakPeriods in DayDetailViewModel)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2)
  - References: Flux/Flux/DayDetail/DayDetailViewModel.swift

## iOS View

- [x] 11. Add 24-hour time formatter to DateFormatting <!-- id:1x60rrq -->
  - Add static 24h formatter to DateFormatting: dateFormat=HH:mm, timeZone=sydneyTimeZone
  - Add static func clockTime24h(from:) -> String using this formatter
  - Avoids locale-dependent 12h/24h from existing clockTime formatter
  - Blocked-by: 1x60rrn (Add PeakPeriod model and update DayDetailResponse in iOS)
  - Stream: 2
  - Requirements: [3.3](requirements.md#3.3), [3.6](requirements.md#3.6)
  - References: Flux/Flux/Helpers/DateFormatting.swift

- [x] 12. Create PeakUsageCard SwiftUI view <!-- id:1x60rrr -->
  - Create new file Flux/Flux/DayDetail/PeakUsageCard.swift
  - Match summaryCard styling: .thinMaterial, RoundedRectangle(cornerRadius: 16, style: .continuous), .headline title, .subheadline rows
  - Title: Peak Usage
  - Each row: time range (HH:mm via clockTime24h), avg load (%.1f kW), energy (whole number with grouping + Wh)
  - Use ForEach with Identifiable conformance
  - Blocked-by: 1x60rrn (Add PeakPeriod model and update DayDetailResponse in iOS), 1x60rrq (Add 24-hour time formatter to DateFormatting)
  - Stream: 2
  - Requirements: [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.6](requirements.md#3.6)
  - References: Flux/Flux/DayDetail/PeakUsageCard.swift, Flux/Flux/DayDetail/DayDetailView.swift

- [x] 13. Wire PeakUsageCard into DayDetailView <!-- id:1x60rrs -->
  - Insert PeakUsageCard between SOCChartView and summaryCard in DayDetailView
  - Guard: if viewModel.hasPowerData && !viewModel.peakPeriods.isEmpty
  - Card hidden when peakPeriods is empty or hasPowerData is false
  - Verify SwiftUI preview renders correctly with mock data
  - Blocked-by: 1x60rrp (Add peakPeriods property to DayDetailViewModel), 1x60rrr (Create PeakUsageCard SwiftUI view)
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.5](requirements.md#3.5)
  - References: Flux/Flux/DayDetail/DayDetailView.swift
