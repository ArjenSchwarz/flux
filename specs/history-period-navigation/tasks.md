---
references:
    - specs/history-period-navigation/requirements.md
    - specs/history-period-navigation/design.md
    - specs/history-period-navigation/decision_log.md
---
# History Period Navigation

- [x] 1. Write failing /history handler tests for the param matrix and past-only range form <!-- id:lftd8w4 -->
  - Table-driven in internal/api/history_test.go
  - Matrix: no params (days=7 default unchanged); days only unchanged; days+start/end mixed form 400; lone start or end 400; unparseable date 400; end<start 400; end==today 400 (range form is past-only, Decision 15); end>today 400; 31-day inclusive span OK; 32-day 400; span crossing the April DST fallback
  - Past range issues NO readings query and no live compute (recording reader stub)
  - Range predating stored data returns the existing subset (possibly empty) without error
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6)
  - References: internal/api/history.go, internal/api/history_test.go, specs/history-period-navigation/design.md

- [x] 2. Implement start/end range form in handleHistory <!-- id:lftd8w5 -->
  - Distinguish days-present from days-defaulted for the mixed-form 400
  - Parse with time.ParseInLocation("2006-01-02", v, sydneyTZ)
  - Validation per Decisions 10/15: end strictly before Sydney today; span via end.After(start.AddDate(0,0,30))
  - Range path skips the readings goroutine and all live compute (todayComputed/todayReadings stay nil)
  - QueryDailyEnergy/QueryOffpeak/fetchNotesAsync take (startDate, endDate) instead of (startDate, today)
  - The days path behaviour stays byte-for-byte unchanged
  - Blocked-by: lftd8w4 (Write failing /history handler tests for the param matrix and past-only range form)
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6)
  - References: internal/api/history.go

- [x] 3. Extend cross-handler parity test to energy totals, peak grid import, and off-peak split <!-- id:lftd8w6 -->
  - cross_handler_test.go currently asserts derivedStats parity only
  - Add /day vs /history (range form) equality for a date more than 30 days old (readings TTL-expired): energy totals, peak grid import, off-peak split
  - Fixture must set PeakGridImportKwh and an offpeak row, which the current fixture leaves nil/absent
  - Blocked-by: lftd8w5 (Implement start/end range form in handleHistory)
  - Stream: 1
  - Requirements: [5.7](requirements.md#5.7)
  - References: internal/api/cross_handler_test.go, internal/api/day.go

- [ ] 4. Write failing FluxCore tests for HistoryQuery and fetchHistory(query:) <!-- id:lftd8w7 -->
  - URLSessionAPIClient encodes .days(n) as ?days=N and .dateRange as ?start=...&end=...
  - Protocol-extension default delegates .days to the required fetchHistory(days:) and throws FluxAPIError.notConfigured for .dateRange
  - HistoryQuery Hashable + Codable round-trip (Codable required: ChartScope is Codable)
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Networking/FluxAPIClient.swift, Flux/Packages/FluxCore/Sources/FluxCore/Networking/URLSessionAPIClient.swift

- [ ] 5. Implement HistoryQuery and the fetchHistory(query:) client path <!-- id:lftd8w8 -->
  - New Networking/HistoryQuery.swift: enum HistoryQuery: Hashable, Sendable, Codable { case days(Int); case dateRange(start: String, end: String) }
  - Protocol method + default per the fetchStatus(simulateLoadWatts:) evolution pattern so ~30 existing mocks keep compiling
  - URLSessionAPIClient implements the real encoding
  - Blocked-by: lftd8w7 (Write failing FluxCore tests for HistoryQuery and fetchHistory(query:))
  - Stream: 2
  - Requirements: [5.1](requirements.md#5.1)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Networking/HistoryQuery.swift, Flux/Packages/FluxCore/Sources/FluxCore/Networking/FluxAPIClient.swift, Flux/Packages/FluxCore/Sources/FluxCore/Networking/URLSessionAPIClient.swift

- [ ] 6. Write HistoryPeriod property-based and unit tests <!-- id:lftd8w9 -->
  - Seeded random dates across DST transitions and month-length edges, T-1361 test style
  - Invariants: next(previous(p)) == p and inverse; contains(start) true and contains(endExclusive) false; week periods always 7 slot days, months 28-31; week(containing: d) identical for every d in the same week (likewise month)
  - dayCount and start/end date strings correct
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [3.1](requirements.md#3.1)
  - References: Flux/Flux/History/HistoryPeriod.swift, Flux/Packages/FluxCore/Sources/FluxCore/Helpers/DateFormatting.swift

- [ ] 7. Implement HistoryPeriod <!-- id:lftd8wa -->
  - New Flux/Flux/History/HistoryPeriod.swift: Sydney-midnight start + endExclusive; week/month factories composing existing DateFormatting helpers
  - previous/next via byAdding .day -7/+7 and .month -1/+1 on start; assert on .days ranges (controls never shown there, silent self-return would hide a wiring bug)
  - contains, dayCount, startDateString/endDateString
  - Blocked-by: lftd8w9 (Write HistoryPeriod property-based and unit tests)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [3.1](requirements.md#3.1)
  - References: Flux/Flux/History/HistoryPeriod.swift, specs/history-period-navigation/design.md

- [ ] 8. Write failing HistoryViewModel tests for navigation, resolved snapshot, and cache bounds <!-- id:lftd8wb -->
  - Recording mock asserts the expected HistoryQuery per intent
  - selectRange resets the anchor; reload()/loadHistory never touch it (req 1.8)
  - navigateNext from previous week collapses to anchor nil + .days query; jumpTo a date in the current month gives anchor nil
  - Coalescing honours a navigation issued mid-load (RequestedPeriod key)
  - resolvedQuery/chartDomain reflect rendered data, not an in-flight request
  - loadCachedDays bounded both ends
  - Fetch fail + no cache shows error state; fetch success + zero days + anchor set shows the no-data state (distinct flags)
  - Blocked-by: lftd8w8 (Implement HistoryQuery and the fetchHistory(query:) client path), lftd8wa (Implement HistoryPeriod)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.8](requirements.md#1.8), [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [3.3](requirements.md#3.3), [6.1](requirements.md#6.1), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3)
  - References: Flux/Flux/History/HistoryViewModel.swift

- [ ] 9. Implement HistoryViewModel intent methods, resolvedQuery, and bounded cache <!-- id:lftd8wc -->
  - periodAnchor: Date? (nil = current); intent methods selectRange/navigatePrevious/navigateNext/jumpTo/returnToCurrent; the load path is anchor-agnostic
  - Coalescing re-keyed on RequestedPeriod (range + anchor)
  - resolvedQuery set inside load() drives chartDomain, resolvedRangeDays, the periodQuery accessor, and the next-chevron disabled state
  - HistoryChartDomain.make parameter now: renamed referenceDate:
  - loadCachedDays(from:through:) with both-ends predicate; current path passes windowStart...today
  - Blocked-by: lftd8wb (Write failing HistoryViewModel tests for navigation, resolved snapshot, and cache bounds)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.6](requirements.md#1.6), [1.8](requirements.md#1.8), [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [3.3](requirements.md#3.3), [6.1](requirements.md#6.1), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3)
  - References: Flux/Flux/History/HistoryViewModel.swift, Flux/Flux/History/HistoryChartDomain.swift

- [ ] 10. Implement HistoryPeriodHeader <!-- id:lftd8wd -->
  - New Flux/Flux/History/HistoryPeriodHeader.swift styled after DayNavigationHeader (DayDetailViewSupport.swift:50): chevrons + centred tappable label
  - File-private Sydney formatters (MMM d, day-only d, MMM yyyy - none exist today)
  - Label opens graphical DatePicker (popover; sheet on compact iOS) with in: ...sydneyTodayEnd and .environment calendar/timeZone set to Sydney (device-calendar rendering would be off by a day on non-Sydney devices)
  - Current button only when viewing past; next chevron visible-but-disabled at the current period
  - Blocked-by: lftd8wc (Implement HistoryViewModel intent methods, resolvedQuery, and bounded cache)
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3), [1.5](requirements.md#1.5), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [4.1](requirements.md#4.1)
  - References: Flux/Flux/History/HistoryPeriodHeader.swift, Flux/Flux/DayDetail/DayDetailViewSupport.swift

- [ ] 11. Wire HistoryView: header, key handling, reload switches, empty-period notice, stats subtitle <!-- id:lftd8we -->
  - Header between range picker and note row, Wk/Mo only
  - Picker onChange and .task call selectRange; .refreshable and error-state Retry switch to reload()
  - macOS: .focusable() then .onKeyPress(left/right), guarded to Wk/Mo
  - Past period + zero days + no error keeps cards rendered (scaffold reserves the axis) with a compact No-data-for-this-period notice replacing the note row - NOT the replace-everything emptyState
  - HistoryStatsOverviewCard subtitle "N of M days" when summary.dayCount < resolvedRangeDays on a past period (costs-card caption intentionally unchanged)
  - View checks: header hidden for .days ranges; Current button visibility; subtitle; empty-past-period card rendering
  - Blocked-by: lftd8wc (Implement HistoryViewModel intent methods, resolvedQuery, and bounded cache), lftd8wd (Implement HistoryPeriodHeader)
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3)
  - References: Flux/Flux/History/HistoryView.swift, Flux/Flux/History/HistoryStatsOverviewCard.swift

- [ ] 12. Carry HistoryQuery through the chart-expansion scope and MockFluxAPIClient <!-- id:lftd8wf -->
  - ChartScope.historyRange(days:) becomes historyRange(HistoryQuery) (Codable preserved)
  - ChartSceneObserver fetches via the scope's query; ChartExpansionContent.historyRangeDays derives the day count (.dateRange via inclusiveDayCount)
  - ExpandedChartView default scope .historyRange(.days(defaultHistoryRangeDays))
  - The three History cards take periodQuery from HistoryView instead of deriving the scope from rangeDays
  - MockFluxAPIClient implements fetchHistory(query:) so previews handle .dateRange
  - Update existing expansion tests
  - Blocked-by: lftd8w8 (Implement HistoryQuery and the fetchHistory(query:) client path), lftd8wc (Implement HistoryViewModel intent methods, resolvedQuery, and bounded cache)
  - Stream: 2
  - Requirements: [5.7](requirements.md#5.7)
  - References: Flux/Flux/Charts/Expansion/ChartKind.swift, Flux/Flux/Charts/Expansion/macOS/ChartSceneObserver.swift, Flux/Flux/Charts/Expansion/ChartExpansionContent.swift, Flux/Flux/Charts/Expansion/ExpandedChartView.swift, Flux/Flux/Services/MockFluxAPIClient.swift
