---
references:
    - specs/ios-app/requirements.md
    - specs/ios-app/design.md
    - specs/ios-app/decision_log.md
---
# iOS App

## Foundation — Models & Types

- [ ] 1. Define API response models (Codable structs) <!-- id:oxpta5l -->
  - Create Models/APIModels.swift with StatusResponse, LiveData, BatteryInfo, Low24h, RollingAvg, OffpeakData, TodayEnergy, HistoryResponse, DayEnergy, DayDetailResponse, TimeSeriesPoint, DaySummary, APIErrorResponse
  - All structs Codable + Sendable, field names match backend camelCase JSON tags
  - DayEnergy and TimeSeriesPoint conform to Identifiable with computed id
  - Stream: 1
  - Requirements: [2.4](requirements.md#2.4), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [4.1](requirements.md#4.1), [5.1](requirements.md#5.1), [6.1](requirements.md#6.1), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [8.5](requirements.md#8.5), [9.5](requirements.md#9.5), [9.9](requirements.md#9.9), [10.1](requirements.md#10.1)
  - References: specs/ios-app/design.md

- [ ] 2. Define FluxAPIError enum <!-- id:oxpta5m -->
  - Create Models/FluxAPIError.swift with cases: notConfigured, unauthorized, badRequest(String), serverError, networkError(String), decodingError(String), unexpectedStatus(Int)
  - Conforms to Error + Sendable — uses String payloads for Sendable compliance (Decision 10)
  - Stream: 1
  - Requirements: [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [12.3](requirements.md#12.3)

- [ ] 3. Define CachedDayEnergy SwiftData model <!-- id:oxpta5n -->
  - Create Models/CachedDayEnergy.swift with @Model class
  - @Attribute(.unique) on date field (YYYY-MM-DD format)
  - init(from: DayEnergy) and asDayEnergy computed property for conversion
  - Blocked-by: oxpta5l (Define API response models (Codable structs))
  - Stream: 1
  - Requirements: [1.8](requirements.md#1.8), [11.1](requirements.md#11.1), [11.2](requirements.md#11.2)
  - References: specs/ios-app/design.md

## Foundation — Services

- [ ] 4. Write tests for KeychainService <!-- id:oxpta5o -->
  - Create KeychainServiceTests.swift
  - Test saveToken + loadToken round-trip
  - Test deleteToken removes stored token
  - Test loadToken returns nil when no token exists
  - Blocked-by: oxpta5m (Define FluxAPIError enum)
  - Stream: 1
  - Requirements: [1.7](requirements.md#1.7), [3.2](requirements.md#3.2)

- [ ] 5. Implement KeychainService <!-- id:oxpta5p -->
  - Create Services/KeychainService.swift
  - Wrap SecItemAdd/SecItemCopyMatching/SecItemDelete
  - Use kSecAttrAccessGroup with App Group identifier
  - Sendable final class
  - Blocked-by: oxpta5o (Write tests for KeychainService)
  - Stream: 1
  - Requirements: [1.7](requirements.md#1.7), [3.2](requirements.md#3.2)

- [ ] 6. Define FluxAPIClient protocol <!-- id:oxpta5q -->
  - Create Services/FluxAPIClient.swift
  - Protocol with Sendable conformance
  - Three methods: fetchStatus(), fetchHistory(days:), fetchDay(date:)
  - All async throws
  - Blocked-by: oxpta5l (Define API response models (Codable structs)), oxpta5m (Define FluxAPIError enum)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.3](requirements.md#2.3)

- [ ] 7. Write tests for URLSessionAPIClient <!-- id:oxpta5r -->
  - Create URLSessionAPIClientTests.swift
  - Use URLProtocol subclass to mock HTTP responses
  - Test fetchStatus/fetchHistory/fetchDay with 200 returns decoded response
  - Test Bearer token in Authorization header
  - Test HTTP 401/400/500 throw correct FluxAPIError cases
  - Test network failure throws networkError, invalid JSON throws decodingError
  - Test missing token throws notConfigured
  - Test validation initializer uses explicit token (not Keychain)
  - Blocked-by: oxpta5p (Implement KeychainService), oxpta5q (Define FluxAPIClient protocol)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7)

- [ ] 8. Implement URLSessionAPIClient <!-- id:oxpta5s -->
  - Create Services/URLSessionAPIClient.swift
  - Two initializers: production (KeychainService tokenProvider) and validation (explicit token) per Decision 8
  - performRequest centralizes error wrapping with typed throws
  - Builds Authorization: Bearer header from tokenProvider
  - Blocked-by: oxpta5r (Write tests for URLSessionAPIClient)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7)

## Foundation — Helpers

- [ ] 9. Write tests for DateFormatting <!-- id:oxpta5t -->
  - Create DateFormattingTests.swift
  - Test parseTimestamp with valid/invalid ISO 8601 strings
  - Test todayDateString returns correct date in Sydney timezone
  - Test isToday matches/mismatches with Sydney timezone
  - Test parseWindowTime and isInOffpeakWindow with boundary conditions
  - All tests verify Sydney timezone usage, not device timezone
  - Stream: 2
  - Requirements: [5.8](requirements.md#5.8), [6.6](requirements.md#6.6)

- [ ] 10. Implement DateFormatting <!-- id:oxpta5u -->
  - Create Helpers/DateFormatting.swift
  - sydneyTimeZone constant (Decision 7)
  - Static shared ISO8601DateFormatter instance
  - parseTimestamp, clockTime, todayDateString, parseWindowTime, isInOffpeakWindow, isToday
  - All date operations use sydneyCalendar, never Calendar.current
  - Blocked-by: oxpta5t (Write tests for DateFormatting)
  - Stream: 2
  - Requirements: [5.8](requirements.md#5.8), [6.6](requirements.md#6.6)

- [ ] 11. Write tests for BatteryColor and GridColor <!-- id:oxpta5v -->
  - Create ColoringTests.swift
  - BatteryColor.forSOC: verify boundaries — 0, 14.9, 15, 29.9, 30, 60, 60.1, 100
  - GridColor: all combinations of pgrid threshold, sustained flag, off-peak window
  - CutoffTimeColor: red <2h, amber before windowStart, default otherwise
  - Blocked-by: oxpta5u (Implement DateFormatting)
  - Stream: 2
  - Requirements: [4.2](requirements.md#4.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [6.7](requirements.md#6.7), [6.8](requirements.md#6.8)

- [ ] 12. Implement BatteryColor and GridColor <!-- id:oxpta5w -->
  - Create Helpers/BatteryColor.swift — forSOC uses > 60 (not >= 60) per requirement 4.2
  - Create Helpers/GridColor.swift — pgrid > 500 AND pgridSustained AND outside off-peak for red
  - Create Helpers/CutoffTimeColor.swift — red <2h, amber before windowStart
  - Blocked-by: oxpta5v (Write tests for BatteryColor and GridColor)
  - Stream: 2
  - Requirements: [4.2](requirements.md#4.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [6.7](requirements.md#6.7), [6.8](requirements.md#6.8)

## View Models

- [ ] 13. Write tests for DashboardViewModel <!-- id:oxpta5x -->
  - Create DashboardViewModelTests.swift with MockFluxAPIClient
  - Test refresh() updates status on success, preserves on failure
  - Test refresh() skips when isLoading is true
  - Test startAutoRefresh idempotency (no duplicate tasks)
  - Test stopAutoRefresh cancels the refresh task
  - Blocked-by: oxpta5s (Implement URLSessionAPIClient)
  - Stream: 1
  - Requirements: [8.1](requirements.md#8.1), [8.2](requirements.md#8.2), [8.3](requirements.md#8.3), [8.4](requirements.md#8.4), [8.5](requirements.md#8.5), [12.1](requirements.md#12.1)

- [ ] 14. Implement DashboardViewModel <!-- id:oxpta5y -->
  - Create Dashboard/DashboardViewModel.swift
  - @MainActor @Observable, holds FluxAPIClient
  - startAutoRefresh cancels existing refreshTask before creating new loop
  - refresh() guards against concurrent calls via isLoading
  - On failure: keeps previous status, sets error for staleness indicator
  - Blocked-by: oxpta5x (Write tests for DashboardViewModel)
  - Stream: 1
  - Requirements: [8.1](requirements.md#8.1), [8.2](requirements.md#8.2), [8.3](requirements.md#8.3), [8.4](requirements.md#8.4), [8.5](requirements.md#8.5), [11.4](requirements.md#11.4), [12.1](requirements.md#12.1), [12.4](requirements.md#12.4)

- [ ] 15. Write tests for HistoryViewModel <!-- id:oxpta5z -->
  - Create HistoryViewModelTests.swift
  - Test loadHistory fetches from API and populates days
  - Test historical days written to SwiftData cache on success
  - Test network failure with/without cache
  - Test isToday uses Sydney timezone
  - Blocked-by: oxpta5s (Implement URLSessionAPIClient), oxpta5u (Implement DateFormatting), oxpta5n (Define CachedDayEnergy SwiftData model)
  - Stream: 2
  - Requirements: [9.1](requirements.md#9.1), [9.2](requirements.md#9.2), [9.8](requirements.md#9.8), [11.1](requirements.md#11.1), [11.2](requirements.md#11.2), [11.3](requirements.md#11.3)

- [ ] 16. Implement HistoryViewModel <!-- id:oxpta60 -->
  - Create History/HistoryViewModel.swift
  - @MainActor @Observable with FluxAPIClient and ModelContext
  - loadHistory: fetch, cache historical days, fall back on failure
  - Uses DateFormatting.isToday for Sydney timezone
  - selectDay updates selectedDay
  - Blocked-by: oxpta5z (Write tests for HistoryViewModel)
  - Stream: 2
  - Requirements: [9.1](requirements.md#9.1), [9.2](requirements.md#9.2), [9.8](requirements.md#9.8), [11.1](requirements.md#11.1), [11.2](requirements.md#11.2), [11.3](requirements.md#11.3)

- [ ] 17. Write tests for DayDetailViewModel <!-- id:oxpta61 -->
  - Create DayDetailViewModelTests.swift
  - Test loadDay populates readings and summary
  - Test navigatePrevious/Next date string changes
  - Test forward arrow disabled when today (Sydney timezone)
  - Test hasPowerData false for fallback data (Decision 9)
  - Blocked-by: oxpta5s (Implement URLSessionAPIClient), oxpta5u (Implement DateFormatting)
  - Stream: 2
  - Requirements: [10.1](requirements.md#10.1), [10.2](requirements.md#10.2), [10.8](requirements.md#10.8), [10.9](requirements.md#10.9)

- [ ] 18. Implement DayDetailViewModel <!-- id:oxpta62 -->
  - Create DayDetail/DayDetailViewModel.swift
  - @MainActor @Observable with init(date:apiClient:)
  - navigatePrevious/Next update date synchronously — view uses .task(id:)
  - Fallback detection: all power fields == 0 sets hasPowerData = false
  - Uses DateFormatting.isToday for forward arrow disable
  - Blocked-by: oxpta61 (Write tests for DayDetailViewModel)
  - Stream: 2
  - Requirements: [10.1](requirements.md#10.1), [10.2](requirements.md#10.2), [10.8](requirements.md#10.8), [10.9](requirements.md#10.9)

- [ ] 19. Write tests for SettingsViewModel <!-- id:oxpta63 -->
  - Create SettingsViewModelTests.swift
  - Test save() validates with entered token (not Keychain), stores on success
  - Test save() sets validationError on failure, does not modify Keychain
  - Test save() captures values at start, sets shouldDismiss on success
  - Test loadExisting populates from Keychain and UserDefaults
  - Blocked-by: oxpta5s (Implement URLSessionAPIClient)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8)

- [ ] 20. Implement SettingsViewModel <!-- id:oxpta64 -->
  - Create Settings/SettingsViewModel.swift
  - @MainActor @Observable with KeychainService dependency
  - save() creates URLSessionAPIClient with init(baseURL:token:) per Decision 8
  - On success: store token, set shouldDismiss = true
  - loadAlertThreshold in UserDefaults with 3000W default
  - Blocked-by: oxpta63 (Write tests for SettingsViewModel)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8)

## Views — Dashboard

- [ ] 21. Create BatteryHeroView <!-- id:oxpta65 -->
  - Create Dashboard/BatteryHeroView.swift
  - Large centred SOC with BatteryColor.forSOC colouring
  - Progress bar matching battery colour
  - Status line: Discharging/Charging/Idle/Full
  - Uses DateFormatting.clockTime for cutoff time
  - Blocked-by: oxpta5w (Implement BatteryColor and GridColor), oxpta5y (Implement DashboardViewModel)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6), [4.7](requirements.md#4.7)

- [ ] 22. Create PowerTrioView <!-- id:oxpta66 -->
  - Create Dashboard/PowerTrioView.swift
  - Three-column HStack: Solar, Load, Grid
  - Solar green when generating, muted when 0
  - Load red above threshold, Grid uses GridColor
  - Reads load threshold from UserDefaults
  - Blocked-by: oxpta5w (Implement BatteryColor and GridColor), oxpta5y (Implement DashboardViewModel)
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8)

- [ ] 23. Create SecondaryStatsView and TodayEnergyView <!-- id:oxpta67 -->
  - Create Dashboard/SecondaryStatsView.swift — 24h low, off-peak grid, off-peak battery delta, 15m avg load
  - Create Dashboard/TodayEnergyView.swift — kWh totals, default text colour only
  - Cutoff time colouring via CutoffTimeColor
  - Clock time formatting via DateFormatting.clockTime
  - Blocked-by: oxpta5w (Implement BatteryColor and GridColor), oxpta5y (Implement DashboardViewModel)
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [6.5](requirements.md#6.5), [6.6](requirements.md#6.6), [6.7](requirements.md#6.7), [6.8](requirements.md#6.8), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3)

- [ ] 24. Create DashboardView with pull-to-refresh and auto-refresh <!-- id:oxpta68 -->
  - Create Dashboard/DashboardView.swift
  - ScrollView + VStack assembling sub-views
  - .refreshable for pull-to-refresh
  - .onAppear/.onDisappear for auto-refresh lifecycle
  - Scene phase observation for background/foreground
  - Staleness banner when error != nil && status != nil
  - View history link + Settings toolbar button
  - Blocked-by: oxpta65 (Create BatteryHeroView), oxpta66 (Create PowerTrioView), oxpta67 (Create SecondaryStatsView and TodayEnergyView)
  - Stream: 1
  - Requirements: [8.1](requirements.md#8.1), [8.2](requirements.md#8.2), [8.3](requirements.md#8.3), [8.4](requirements.md#8.4), [8.5](requirements.md#8.5), [7.4](requirements.md#7.4), [12.1](requirements.md#12.1), [12.2](requirements.md#12.2), [12.3](requirements.md#12.3), [13.5](requirements.md#13.5)

## Views — History & Day Detail

- [ ] 25. Create HistoryChartView with grouped bars and selection <!-- id:oxpta69 -->
  - Create History/HistoryChartView.swift
  - BarMark with .foregroundStyle(by:) and .position(by:) for grouped bars
  - 5 metrics: solar (green), grid imported (red), grid exported (blue), charged (amber), discharged (purple)
  - Today bars at 0.5 opacity
  - .chartOverlay with DragGesture for selection
  - RectangleMark for selected day highlight
  - Segmented Picker for 7/14/30 range
  - Blocked-by: oxpta60 (Implement HistoryViewModel)
  - Stream: 2
  - Requirements: [9.1](requirements.md#9.1), [9.2](requirements.md#9.2), [9.3](requirements.md#9.3), [9.4](requirements.md#9.4), [9.5](requirements.md#9.5), [9.6](requirements.md#9.6)

- [ ] 26. Create HistoryView with summary card and navigation <!-- id:oxpta6a -->
  - Create History/HistoryView.swift
  - Assembles HistoryChartView + summary card
  - View day detail link to DayDetailView
  - No data available placeholder when empty
  - Fetches history on appear
  - Blocked-by: oxpta69 (Create HistoryChartView with grouped bars and selection)
  - Stream: 2
  - Requirements: [9.5](requirements.md#9.5), [9.7](requirements.md#9.7), [9.8](requirements.md#9.8)

- [ ] 27. Create SOCChartView, PowerChartView, and BatteryPowerChartView <!-- id:oxpta6b -->
  - Create DayDetail/SOCChartView.swift — AreaMark + RuleMark at 10% + PointMark for low
  - Create DayDetail/PowerChartView.swift — AreaMark solar, LineMark load/grid
  - Create DayDetail/BatteryPowerChartView.swift — LineMark with -pbat, RuleMark at zero
  - All charts: .chartXAxis 3-hour stride, time axis 00:00-00:00
  - Blocked-by: oxpta62 (Implement DayDetailViewModel)
  - Stream: 2
  - Requirements: [1.3](requirements.md#1.3), [10.3](requirements.md#10.3), [10.4](requirements.md#10.4), [10.5](requirements.md#10.5), [10.6](requirements.md#10.6)

- [ ] 28. Create DayDetailView with day navigation and summary <!-- id:oxpta6c -->
  - Create DayDetail/DayDetailView.swift
  - ScrollView with three stacked charts + summary card
  - Day navigation with chevron buttons, .task(id: date) for loading
  - When hasPowerData false: SOC chart only, power charts empty
  - Summary card with kWh totals and SOC low point
  - Blocked-by: oxpta6b (Create SOCChartView, PowerChartView, and BatteryPowerChartView)
  - Stream: 2
  - Requirements: [10.1](requirements.md#10.1), [10.2](requirements.md#10.2), [10.7](requirements.md#10.7), [10.8](requirements.md#10.8), [10.9](requirements.md#10.9)

## Views — Settings & Navigation

- [ ] 29. Create SettingsView <!-- id:oxpta6d -->
  - Create Settings/SettingsView.swift
  - Form with Backend + Display sections
  - Save button disabled during validation / when empty
  - Validation error display, .onChange(of: shouldDismiss) for dismiss
  - Liquid Glass styling automatic on iOS 26
  - Blocked-by: oxpta64 (Implement SettingsViewModel)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8)

- [ ] 30. Create AppNavigationView, SidebarView, and FluxApp entry point <!-- id:oxpta6e -->
  - Create Navigation/AppNavigationView.swift — NavigationSplitView preferredCompactColumn: .detail
  - Create Navigation/SidebarView.swift and Screen.swift enum
  - .onChange(of: selectedScreen) resets navigationPath
  - Create FluxApp.swift — @main with ModelContainer for CachedDayEnergy
  - Dependency wiring: URLSessionAPIClient from UserDefaults URL + KeychainService
  - Redirect to Settings when no token configured
  - Blocked-by: oxpta68 (Create DashboardView with pull-to-refresh and auto-refresh), oxpta6d (Create SettingsView)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.4](requirements.md#1.4), [1.6](requirements.md#1.6), [1.8](requirements.md#1.8), [3.7](requirements.md#3.7), [13.1](requirements.md#13.1), [13.2](requirements.md#13.2), [13.3](requirements.md#13.3), [13.4](requirements.md#13.4), [13.5](requirements.md#13.5)

## Integration

- [ ] 31. Create MockFluxAPIClient for SwiftUI previews <!-- id:oxpta6f -->
  - Create MockFluxAPIClient with static sample data for all endpoints
  - Add SwiftUI preview providers to all view files
  - Verify all previews render without crashes
  - Blocked-by: oxpta6e (Create AppNavigationView, SidebarView, and FluxApp entry point)
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5)
