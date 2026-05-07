---
references:
    - specs/history-usage-stats/requirements.md
    - specs/history-usage-stats/design.md
    - specs/history-usage-stats/decision_log.md
---
# Tasks: History Usage Stats

- [ ] 1. Make HistoryCardChrome.kpi optional <!-- id:gy1z6r6 -->
  - Change `kpi: String` → `kpi: String? = nil` in Flux/Flux/History/HistoryCardChrome.swift; render the trailing KPI Text only when non-nil.
  - Existing call sites (HistorySolarCard, HistoryGridUsageCard, HistoryBatteryCard, HistoryDailyUsageCard) compile unchanged via Swift's implicit String → String? wrapping.
  - Decision 14.
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2)

- [ ] 2. Write tests for new PeriodSummary aggregates and Totals comparators <!-- id:gy1z6r7 -->
  - Add to Flux/FluxTests/HistoryViewModelTests.swift (or a new HistoryViewModelOverviewTests.swift).
  - Cover fixtures (a)–(o) per design §Testing Strategy: empty days; only-today; today+offpeak asymmetry (b′) — assert peakImportTotalKwh and exportTotalKwh include today's contribution; full data with Equatable record assertion (c) using ==; dailyUsage-only (d); ties on usage / solar / socLow (e/f/g, later date wins); night-block-missing (h); night-block zero kWh counts toward nightBlockDayCount (h′); negative night clamp (i); negative peakGridImportKwh clamp (j); no offpeak in range (k); day-with-offpeak with all peak imports zero (l) — locks AC 2.4 zero-vs-em-dash; mixed cohort dailyUsage-without-offpeak (m); NaN socLow → lowestSocDay nil (o).
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7), [2.8](requirements.md#2.8), [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [7.1](requirements.md#7.1)

- [ ] 3. Implement new PeriodSummary fields, record types, generic comparator, and Totals extensions <!-- id:gy1z6r8 -->
  - Edit Flux/Flux/History/HistoryDerivedState.swift.
  - Add nested DayKwhRecord and LowestSocRecord types and a private DayRecordValue protocol exposing comparableValue: Double / date: Date.
  - Extend PeriodSummary with nightTotalKwh, nightBlockDayCount, mostUsageDay, mostSolarDay, lowestSocDay, plus computed nightAvgKwh; update PeriodSummary.empty defaults.
  - Add a generic consider(_:candidate:prefersLarger:) helper to Totals; rewrite addCompleteDay(_:parsedDate:dailyUsageEntry:) to reuse the existing entry's pre-clamped stackedTotalKwh for mostUsage and read the night block from entry.blocks for nightTotal/nightBlockDayCount (no re-summation, no second clamp).
  - Add considerSocLow(day:parsedDate:) with `guard soc.isFinite` and call it unconditionally in DerivedState.init's main loop.
  - Update Totals.snapshot to populate the new fields. Decisions 5, 8, 11.
  - Blocked-by: gy1z6r7 (Write tests for new PeriodSummary aggregates and Totals comparators)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4)

- [ ] 4. Write tests for HistoryStatsFormatters <!-- id:gy1z6r9 -->
  - New file Flux/FluxTests/HistoryStatsFormattersTests.swift.
  - socPercent half-up boundaries (n: 11.5→12, 11.4→11, 0.5→1, 99.5→100); socPercent NaN/Inf returns "—" (n′); dateRange chronologically-reversed input produces same string as forward; dateRange single-day range returns one date; shortDate returns "MMM d" Sydney; dateWithTime non-nil returns "MMM d at HH:mm" Sydney; accessibleKwh and accessibleSocPercent spell out units.
  - Stream: 1
  - Requirements: [2.6](requirements.md#2.6), [2.7](requirements.md#2.7), [2.8](requirements.md#2.8), [6.1](requirements.md#6.1)

- [ ] 5. Implement HistoryStatsFormatters <!-- id:gy1z6ra -->
  - New file Flux/Flux/History/HistoryStatsFormatters.swift.
  - Seven formatters per design §Formatters: socPercent (Int(soc.rounded(.toNearestOrAwayFromZero)) with isFinite guard returning "—"), shortDate, dateWithTime, dateRange (uses entries.map(\.date).min()/.max() so reversed input still works), accessibleKwh, accessibleSocPercent.
  - Decision 10.
  - Blocked-by: gy1z6r9 (Write tests for HistoryStatsFormatters)
  - Stream: 1
  - Requirements: [2.6](requirements.md#2.6), [2.7](requirements.md#2.7), [2.8](requirements.md#2.8), [6.1](requirements.md#6.1)

- [ ] 6. Extend MockFluxAPIClient.historyDays fixtures so previews populate every tile <!-- id:gy1z6rb -->
  - Flux/Flux/Services/MockFluxAPIClient.swift — populate offpeakGridImportKwh, offpeakGridExportKwh, socLow, socLowTime on each historyDays fixture row so previews render every tile populated (no all-em-dash preview).
  - Required for the #Preview blocks in task 8 to demonstrate populated and tappable tiles.
  - Stream: 1
  - Requirements: [7.4](requirements.md#7.4)

- [ ] 7. Write tests for HistoryStatsOverviewCard static helpers and tap-action behaviour <!-- id:gy1z6rc -->
  - New file Flux/FluxTests/HistoryStatsOverviewCardTests.swift.
  - Static helpers: HistoryStatsOverviewCard.valueText(for:summary:), dateLineText, isTappable, accessibilityLabel — all keyed by a TileKey enum.
  - Cover literal label strings per tile, em-dash placeholder per tile, date-line format for record tiles, tap-target invariant (em-dash tiles non-tappable), accessibility label format (label/value/date joined by ", "; "no data" for em-dash; .isButton trait + hint for tappable).
  - Tap-action test: invoke the action closure passed to a DayRecordTile directly (no synthesised gesture); assert HistoryViewModel.selectedDay matches the record's dayID. Include a fixture where Lowest SoC's record is today, asserting today-tap behaviour per AC 3.1.
  - Blocked-by: gy1z6r8 (Implement new PeriodSummary fields, record types, generic comparator, and Totals extensions), gy1z6ra (Implement HistoryStatsFormatters)
  - Stream: 1
  - Requirements: [1.3](requirements.md#1.3), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7), [2.8](requirements.md#2.8), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3)

- [ ] 8. Implement HistoryStatsOverviewCard <!-- id:gy1z6rd -->
  - New file Flux/Flux/History/HistoryStatsOverviewCard.swift.
  - TileKey enum, static helpers per task 7, private StatTile and DayRecordTile views, LazyVGrid body with columnCount derived from horizontalSizeClass on iOS (compact→2, regular→4) and fixed 4 on macOS.
  - HistoryCardChrome wiring with title "Period overview" and the dateRange formatter as KPI; .animation(.default, value: columnCount) on the grid for column reflow.
  - Two #Preview blocks (iPhone 375pt, Mac 900pt) gated under #if DEBUG, mixing populated and em-dash tiles. Decisions 12, 13, 14.
  - Blocked-by: gy1z6r6 (Make HistoryCardChrome.kpi optional), gy1z6rc (Write tests for HistoryStatsOverviewCard static helpers and tap-action behaviour)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7), [2.8](requirements.md#2.8), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [7.4](requirements.md#7.4)

- [ ] 9. Wire HistoryStatsOverviewCard into HistoryView <!-- id:gy1z6re -->
  - Flux/Flux/History/HistoryView.swift — insert HistoryStatsOverviewCard between NoteRowView and HistorySolarCard, inside the existing viewModel.days.isEmpty else-branch so the screen-level empty/error state continues to gate it.
  - Pass summary: derived.summary, entries: derived.solar, onSelect: selectDay.
  - Blocked-by: gy1z6rd (Implement HistoryStatsOverviewCard)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.4](requirements.md#1.4), [3.1](requirements.md#3.1)

- [ ] 10. Refresh agent notes for views and viewmodels <!-- id:gy1z6rf -->
  - Update docs/agent-notes/ios-app-views.md with a paragraph describing HistoryStatsOverviewCard (placement, eight tiles, tap-to-select on day-record tiles).
  - Update docs/agent-notes/ios-app-viewmodels.md to list the four new PeriodSummary fields (nightTotalKwh, nightBlockDayCount, mostUsageDay, mostSolarDay, lowestSocDay) and the two new record types (DayKwhRecord, LowestSocRecord).
  - Blocked-by: gy1z6r8 (Implement new PeriodSummary fields, record types, generic comparator, and Totals extensions), gy1z6rd (Implement HistoryStatsOverviewCard), gy1z6re (Wire HistoryStatsOverviewCard into HistoryView)
  - Stream: 1
