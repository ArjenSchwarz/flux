---
references:
    - specs/history-month-week-to-date/requirements.md
    - specs/history-month-week-to-date/design.md
    - specs/history-month-week-to-date/decision_log.md
---
# History Month and Week to Date

- [ ] 1. Write Go table test for /history days validation (1-31) <!-- id:i6257bv -->
  - internal/api/history_test.go: days 1/7/31 accepted (window length = days), 0/32/-1/x -> 400 "between 1 and 31", absent -> 7.
  - Red: fails against the current {7,14,30} allowlist and old message.
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1)
  - References: internal/api/history.go

- [ ] 2. Widen /history days bounds in history.go <!-- id:i6257bw -->
  - history.go: replace validDays with `if err != nil || parsed < 1 || parsed > 31`, message "invalid days parameter, must be between 1 and 31".
  - Keep default 7 inside the d != empty block; startDate math unchanged.
  - Blocked-by: i6257bv (Write Go table test for /history days validation (1-31)), history
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1)
  - References: internal/api/history.go

- [ ] 3. Write unit + property tests for FluxCore date helpers <!-- id:i6257bx -->
  - Edges: month lengths 28/29/30/31; firstWeekday 1(Sun)/2(Mon)/7(Sat); single-day (1st of month, week-start day -> count 1); Sydney DST days (early Apr/Oct) same counts.
  - Property tests over seeded dates (no Date.now): wk count 1-7, mo count 1-31, startOfWeek weekday == firstWeekday, start <= now.
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Helpers/DateFormatting.swift

- [ ] 4. Implement DateFormatting month/week/day-count helpers <!-- id:i6257by -->
  - DateFormatting.swift: startOfMonth via dateInterval(.month); startOfWeek mutates a COPY of sydneyCalendar (never the shared static let) then dateInterval(.weekOfYear).
  - inclusiveDayCount via dateComponents([.day]) on Sydney-midnight-normalised dates +1 (no interval division).
  - Blocked-by: i6257bx (Write unit + property tests for FluxCore date helpers)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [2.1](requirements.md#2.1), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2)
  - References: Flux/Packages/FluxCore/Sources/FluxCore/Helpers/DateFormatting.swift

- [ ] 5. Write tests for HistoryRange resolution and labels <!-- id:i6257bz -->
  - Flux/History tests: .days(n) passthrough; .weekToDate/.monthToDate resolve via FluxCore helpers incl single-day edges; pickerLabel == 7d/14d/30d/Wk/Mo.
  - Stream: 3
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [6.2](requirements.md#6.2)
  - References: Flux/Flux/History/HistoryView.swift

- [ ] 6. Implement HistoryRange enum <!-- id:i6257c0 -->
  - Flux/Flux/History/HistoryRange.swift: Hashable enum .days(Int)/.weekToDate/.monthToDate; pickerLabel; resolvedDays(now:firstWeekday:) delegating to FluxCore helpers.
  - Blocked-by: i6257bz (Write tests for HistoryRange resolution and labels), i6257by (Implement DateFormatting month/week/day-count helpers)
  - Stream: 3
  - Requirements: [1.1](requirements.md#1.1), [2.1](requirements.md#2.1), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)
  - References: Flux/Flux/History/HistoryView.swift

- [ ] 7. Write/update HistoryViewModel tests for range loading <!-- id:i6257c1 -->
  - Cover: Wk/Mo resolution (injected now+firstWeekday); reload() re-resolves after now advances past midnight; offline fallback excludes pre-startDate days, ascending, auto-selects newest; mid-load range switch coalesces to latest; failed fetch + empty cache -> error/empty.
  - Fix loadHistoryFallsBackToCacheWhenNetworkFails + cacheFallbackPathRendersNotes to inject nowProvider with relative dates (they hardcode an out-of-window date).
  - Blocked-by: i6257c0 (Implement HistoryRange enum)
  - Stream: 3
  - Requirements: [3.3](requirements.md#3.3), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4)
  - References: Flux/Flux/History/HistoryViewModel.swift

- [ ] 8. Implement HistoryViewModel range loading changes <!-- id:i6257c2 -->
  - HistoryViewModel: loadHistory(range:) replaces loadHistory(days:) - set lastRequestedRange before the isLoading guard, resolve N (nowProvider + injected firstWeekdayProvider), set resolvedRangeDays, fetchHistory(days:N), coalesce to latest lastRequestedRange; reload() re-resolves.
  - loadCachedDays(onOrAfter:startDate): let-captured #Predicate date>=startDate, sorted ascending (flip the current .reverse descriptor).
  - Blocked-by: i6257c1 (Write/update HistoryViewModel tests for range loading)
  - Stream: 3
  - Requirements: [3.3](requirements.md#3.3), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4)
  - References: Flux/Flux/History/HistoryViewModel.swift

- [ ] 9. Wire HistoryView range control to HistoryRange <!-- id:i6257c3 -->
  - HistoryView: selectedRange:HistoryRange=.days(7); 5-segment Picker tags .days(7)/.days(14)/.days(30)/.weekToDate/.monthToDate showing pickerLabel, keep .pickerStyle(.segmented).
  - .task/.onChange/.refreshable/macRefreshAction call loadHistory(range:); cards receive rangeDays: viewModel.resolvedRangeDays.
  - Blocked-by: i6257c2 (Implement HistoryViewModel range loading changes), i6257c0 (Implement HistoryRange enum)
  - Stream: 3
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [3.3](requirements.md#3.3)
  - References: Flux/Flux/History/HistoryView.swift

- [ ] 10. Add downstream consistency test for range day sets <!-- id:i6257c4 -->
  - Identical [DayEnergy] array -> identical DerivedState/PeriodSummary regardless of producing range; to-date selection feeds resolved N to the card expansionScope (ChartScope.historyRange(days:)).
  - Blocked-by: i6257c2 (Implement HistoryViewModel range loading changes)
  - Stream: 3
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3)
  - References: Flux/Flux/History/HistoryDerivedState.swift
