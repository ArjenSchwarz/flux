---
references:
    - specs/ipad-adaptive-layout/requirements.md
    - specs/ipad-adaptive-layout/design.md
    - specs/ipad-adaptive-layout/decision_log.md
---
# iPad Adaptive Layout Tasks (T-1150)

## Foundations

- [x] 1. Write unit tests for AdaptiveColumnsLayout.columnCount <!-- id:sgahfr1 -->
  - New test file Flux/FluxTests/AdaptiveColumnsLayoutTests.swift
  - Use Swift Testing (@Test, #expect)
  - Boundary widths: 699 → 1, 700 → 2, 999 → 2, 1000 → 3
  - Dynamic Type collapse: .accessibility3 keeps base columns; .accessibility4 drops one; result never below 1
  - Tests fail because the type does not exist yet
  - Stream: 1
  - Requirements: [8.2](requirements.md#8.2)

- [x] 2. Implement AdaptiveColumnsLayout helper <!-- id:sgahfr2 -->
  - New file Flux/Flux/Helpers/AdaptiveColumnsLayout.swift
  - Generic over content; takes minCardWidth: CGFloat = 320, spacing: CGFloat = FluxTheme.Metrics.panelGap
  - Internal columnCount(width:) reads @Environment(\.dynamicTypeSize)
  - Lay children into the computed column count via Grid or LazyVGrid
  - Collapse trigger is typeSize >= .accessibility4 (NOT isAccessibilitySize)
  - Blocked-by: sgahfr1 (Write unit tests for AdaptiveColumnsLayout.columnCount)
  - Stream: 1
  - Requirements: [2.2](requirements.md#2.2), [3.1](requirements.md#3.1), [4.1](requirements.md#4.1), [8.2](requirements.md#8.2)

- [x] 3. Write unit tests for Screen tab init and widened sidebarVisible <!-- id:sgahfr3 -->
  - Edit Flux/FluxTests/ScreenTests.swift (create if missing)
  - Every FluxTab maps to a Screen whose .tab returns the original
  - Screen.sidebarVisible returns [.dashboard, .today, .history] on iOS (no .settings)
  - macOS continues to exclude .settings as before
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3), [1.7](requirements.md#1.7), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)

- [x] 4. Implement Screen tab init and widen Screen.sidebarVisible <!-- id:sgahfr4 -->
  - Edit Flux/Flux/Navigation/Screen.swift
  - Add init(tab: FluxTab) initialiser
  - Drop the iOS-side filter that excludes .today from sidebarVisible (lines 36-38 today)
  - macOS path stays as-is
  - Blocked-by: sgahfr3 (Write unit tests for Screen tab init and widened sidebarVisible)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3), [1.7](requirements.md#1.7), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)

- [x] 5. Write unit tests for DayDetailViewModel setDate <!-- id:sgahfr5 -->
  - Edit Flux/FluxTests/DayDetailViewModelTests.swift (create if missing)
  - Use a mock FluxAPIClient that counts loadDay invocations
  - Verify per-day fields reset: readings, parsedReadings, summary, peakPeriods, dailyUsage, note, offpeakStats, comparisonState
  - loadDay() called exactly once on date change
  - setDate(date) with the current date is a no-op (no client call, no field reset)
  - Stream: 1
  - Requirements: [1.7](requirements.md#1.7)

- [x] 6. Implement DayDetailViewModel setDate method <!-- id:sgahfr6 -->
  - Edit Flux/Flux/DayDetail/DayDetailViewModel.swift
  - Method signature: func setDate(_ newDate: String) async
  - Cancel comparisonTask; mutate date; clear listed per-day fields; await loadDay()
  - Do not touch apiClient or nowProvider
  - Blocked-by: sgahfr5 (Write unit tests for DayDetailViewModel setDate)
  - Stream: 1
  - Requirements: [1.7](requirements.md#1.7)

- [x] 7. Write unit tests for sidebar↔tab syncedState reducer <!-- id:sgahfr7 -->
  - New test file Flux/FluxTests/SidebarTabSyncTests.swift
  - Reducer signature: func syncedState(selected: Screen?, tab: FluxTab) -> (Screen?, FluxTab)
  - Identity inputs are fixed points
  - Mismatched inputs converge to a single canonical pair in one step
  - Every FluxTab ↔ Screen pair round-trips
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)

- [x] 8. Implement syncedState pure function <!-- id:sgahfr8 -->
  - New file Flux/Flux/Navigation/SidebarTabSync.swift
  - Pure free function — no SwiftUI imports beyond what Screen / FluxTab need
  - Single source of truth for the Screen ↔ FluxTab mapping
  - Blocked-by: sgahfr7 (Write unit tests for sidebar↔tab syncedState reducer)
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)

## AppNavigationView wiring

- [x] 9. Add today @State + 60s recompute task + scenePhase recompute to AppNavigationView <!-- id:sgahfr9 -->
  - Edit Flux/Flux/Navigation/AppNavigationView.swift
  - Add @State private var today: String = DateFormatting.todayDateString()
  - Add .task that loops with Task.sleep(for: .seconds(60)) and updates today when DateFormatting.todayDateString() differs
  - Add onChange(of: scenePhase) handler that recomputes today when phase becomes .active
  - State is not yet consumed — only the recompute mechanism lands
  - Blocked-by: sgahfr6 (Implement DayDetailViewModel setDate method)
  - Stream: 1
  - Requirements: [1.7](requirements.md#1.7)

- [x] 10. Hoist Dashboard / History / Today DayDetail view-models into AppNavigationView <!-- id:sgahfra -->
  - Edit Flux/Flux/Navigation/AppNavigationView.swift and Flux/Flux/Navigation/FluxiOSRoot.swift
  - Move @State dashboardViewModel out of FluxiOSRoot; AppNavigationView owns DashboardViewModel, HistoryViewModel, and a Today DayDetailViewModel
  - Thread VMs into FluxiOSRoot via init
  - Update HistoryView and DayDetailView call sites inside FluxiOSRoot to use the injected VMs (both views already support init(viewModel:))
  - Verify iPhone simulator: tab switches preserve cached data on all three tabs
  - Blocked-by: sgahfr4 (Implement Screen tab init and widen Screen.sidebarVisible), sgahfr6 (Implement DayDetailViewModel setDate method), sgahfr9 (Add today @State + 60s recompute task + scenePhase recompute to AppNavigationView)
  - Stream: 1
  - Requirements: [6.4](requirements.md#6.4), [6.5](requirements.md#6.5), [7.1](requirements.md#7.1)

- [x] 11. Wire today rollover task to call setDate on the Today view-model <!-- id:sgahfrb -->
  - Edit Flux/Flux/Navigation/AppNavigationView.swift
  - Inside the 60s rollover task and the scenePhase onChange, after updating today, call await todayDayDetailViewModel.setDate(today)
  - Dashboard 10s auto-refresh and SoCAlertsService.foregroundHook calls already inside the scenePhase block continue to run alongside the rollover handler
  - Blocked-by: sgahfr9 (Add today @State + 60s recompute task + scenePhase recompute to AppNavigationView), sgahfra (Hoist Dashboard / History / Today DayDetail view-models into AppNavigationView)
  - Stream: 1
  - Requirements: [1.7](requirements.md#1.7), [6.4](requirements.md#6.4)

- [x] 12. Rebuild hoisted VMs in reloadDependencies() when API client identity changes <!-- id:sgahfrc -->
  - Edit Flux/Flux/Navigation/AppNavigationView.swift, function reloadDependencies()
  - Compute clientChanged = (apiClient as AnyObject?) !== (client as AnyObject?)
  - When clientChanged && client != nil, reconstruct dashboardViewModel, historyViewModel, and todayDayDetailViewModel with the new client (and current today for DayDetail)
  - SoCAlertsService.shared.bind(apiClient:) continues to fire as today
  - Blocked-by: sgahfra (Hoist Dashboard / History / Today DayDetail view-models into AppNavigationView)
  - Stream: 1
  - Requirements: [6.4](requirements.md#6.4), [1.6](requirements.md#1.6)

- [x] 13. Apply syncedState reducer via two onChange handlers in AppNavigationView <!-- id:sgahfrd -->
  - Edit Flux/Flux/Navigation/AppNavigationView.swift
  - Add .onChange(of: selectedScreen) and .onChange(of: iosTab) handlers
  - Each handler computes syncedState(selected:tab:) and writes the other side only when it differs from the current value
  - Existing #if os(macOS) storedSelection write stays in the selectedScreen handler
  - Blocked-by: sgahfr8 (Implement syncedState pure function), sgahfra (Hoist Dashboard / History / Today DayDetail view-models into AppNavigationView)
  - Stream: 1
  - Requirements: [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)

## iPad shell

- [x] 14. Add FluxiPadRoot — sidebar + detail NavigationStack + toolbar gear <!-- id:sgahfre -->
  - New file Flux/Flux/Navigation/FluxiPadRoot.swift
  - Accepts injected apiClient and view-models from AppNavigationView
  - NavigationSplitView(preferredCompactColumn:) with SidebarView in the sidebar and NavigationStack(path:) in the detail
  - Apply .navigationSplitViewStyle(.balanced)
  - Detail switch on selectedScreen: .dashboard → DashboardView(viewModel:), .today → DayDetailView(viewModel:) (hoisted Today VM), .history → HistoryView(viewModel:...), .settings → small unconfigured fallback
  - Add ToolbarItem(.primaryAction) gear opening the existing Settings sheet
  - Screens render their existing single-column body in this task — adaptive layouts come in tasks 17-19
  - Blocked-by: sgahfra (Hoist Dashboard / History / Today DayDetail view-models into AppNavigationView)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.7](requirements.md#1.7), [1.8](requirements.md#1.8), [8.3](requirements.md#8.3)

- [x] 15. Add usesPadShell gate in AppNavigationView.iOSRoot <!-- id:sgahfrf -->
  - Edit Flux/Flux/Navigation/AppNavigationView.swift
  - Add @Environment(\.horizontalSizeClass) private var hSizeClass
  - Add private var usesPadShell: Bool { UIDevice.current.userInterfaceIdiom == .pad && hSizeClass == .regular }
  - In iOSRoot, branch: usesPadShell → FluxiPadRoot, else → FluxiOSRoot
  - Smoke-check matrix: iPhone simulators (incl. 16 Pro Max landscape) keep FluxiOSRoot; iPad mini/Air/Pro 13" full-screen show FluxiPadRoot; Slide Over and ⅓ Split View on iPad fall back to FluxiOSRoot
  - Blocked-by: sgahfre (Add FluxiPadRoot — sidebar + detail NavigationStack + toolbar gear)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [7.1](requirements.md#7.1)

## Adaptive layouts

- [x] 16. Measure detail-column widths on iPad simulators; adjust thresholds if needed <!-- id:sgahfrg -->
  - Add a temporary GeometryReader probe to FluxiPadRoot's detail column that logs the width on appear
  - Run on iPad mini portrait/landscape, iPad Air portrait/landscape, iPad Pro 13" portrait/landscape, half Split View on iPad Pro 13"
  - Record values in new file specs/ipad-adaptive-layout/implementation.md
  - If measured widths fall on the wrong side of 700/1000, adjust constants in AdaptiveColumnsLayout and re-run task 1's tests
  - Remove the probe before moving to task 17
  - Blocked-by: sgahfre (Add FluxiPadRoot — sidebar + detail NavigationStack + toolbar gear), sgahfrf (Add usesPadShell gate in AppNavigationView.iOSRoot)
  - Stream: 1
  - Requirements: [9.1](requirements.md#9.1), [9.3](requirements.md#9.3)

- [x] 17. Add Dashboard regular-size-class layout (DashboardView) <!-- id:sgahfrh -->
  - Edit Flux/Flux/Dashboard/DashboardView.swift
  - Add dashboardContentRegular @ViewBuilder branch using AdaptiveColumnsLayout for hero+trio side-by-side at w ≥ 700
  - SummaryBlock full width; BatteryBlock (+ future blocks) flowing through AdaptiveColumnsLayout
  - Branch selection via @Environment(\.horizontalSizeClass)
  - iPad shell passes tab: nil so legacyHeader renders
  - Add #Preview pinned to .frame(width: 770)
  - Compact width path stays at the existing single-column dashboardContent
  - Blocked-by: sgahfr2 (Implement AdaptiveColumnsLayout helper), sgahfrg (Measure detail-column widths on iPad simulators; adjust thresholds if needed)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [8.1](requirements.md#8.1)

- [x] 18. Add History regular-size-class card grid (HistoryView) <!-- id:sgahfri -->
  - Edit Flux/Flux/History/HistoryView.swift
  - Add historyContentRegular @ViewBuilder branch that lays the four cards (HistorySolarCard, HistoryGridUsageCard, HistoryDailyUsageCard, conditional summaryCard) through AdaptiveColumnsLayout
  - HistoryStatsOverviewCard stays full width above the grid; its existing 4-col/2-col internal branch is untouched (AC 3.3)
  - Add #Preview pinned to .frame(width: 770)
  - Blocked-by: sgahfr2 (Implement AdaptiveColumnsLayout helper), sgahfrg (Measure detail-column widths on iPad simulators; adjust thresholds if needed)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [8.1](requirements.md#8.1)

- [x] 19. Add Day Detail two-column regular-size-class layout (DayDetailView) <!-- id:sgahfrj -->
  - Edit Flux/Flux/DayDetail/DayDetailView.swift
  - Add dayDetailContentRegular @ViewBuilder branch
  - Header row (DayNavigationHeader, DayDetailNoteSection, CompareControl) full width
  - Below: two-column Grid with summary panels (DayInFiveBlocks, Summary, Battery, DailyUsage, PeakUsage) on the left; three charts (PowerChartView, BatteryPowerChartView, SOCChartView) stacked on the right
  - Branch selection via @Environment(\.horizontalSizeClass)
  - Add #Preview variants pinned to width 770 and 1080
  - Blocked-by: sgahfr2 (Implement AdaptiveColumnsLayout helper), sgahfrg (Measure detail-column widths on iPad simulators; adjust thresholds if needed)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [8.1](requirements.md#8.1)

- [x] 20. Verify Settings sheet width on iPad; add 640pt cap if needed <!-- id:sgahfrk -->
  - Run app on iPad simulator at regular width; present Settings
  - If default sheet width already constrains the form to a comfortable reading width (likely .formSheet behaviour), record observation in implementation.md and close the task with no code change
  - If the form stretches edge-to-edge, edit Flux/Flux/Settings/SettingsView.swift to wrap form contents in .frame(maxWidth: 640) + .frame(maxWidth: .infinity), gated by horizontalSizeClass == .regular
  - Do not apply the cap at compact size class
  - Blocked-by: sgahfre (Add FluxiPadRoot — sidebar + detail NavigationStack + toolbar gear)
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2)

## Verification

- [ ] 21. Run full smoke check + iPad device coverage; record results in implementation.md <!-- id:sgahfrl -->
  - Record per row in specs/ipad-adaptive-layout/implementation.md
  - iPhone: FluxTabBar visible all three tabs; Settings sheet from each screen; History → Day Detail push
  - macOS: sidebar with Dashboard/Today/History (no Settings row); ⌘, opens Settings scene; ⌘R refresh; ←/→ on Day Detail
  - iPad simulators (mini/Air/Pro 13") in both orientations + half Split View + Slide Over: sidebar at regular, FluxTabBar at compact
  - Confirm Dashboard 10s refresh, History → Day Detail push in detail column, Today midnight rollover (advance simulator clock past midnight and verify Today's date updates and reload fires)
  - Run make ios-test and make macos-test; both must pass
  - Blocked-by: sgahfrb (Wire today rollover task to call setDate on the Today view-model), sgahfrc (Rebuild hoisted VMs in reloadDependencies() when API client identity changes), sgahfrd (Apply syncedState reducer via two onChange handlers in AppNavigationView), sgahfrf (Add usesPadShell gate in AppNavigationView.iOSRoot), sgahfrh (Add Dashboard regular-size-class layout (DashboardView)), sgahfri (Add History regular-size-class card grid (HistoryView)), sgahfrj (Add Day Detail two-column regular-size-class layout (DayDetailView)), sgahfrk (Verify Settings sheet width on iPad; add 640pt cap if needed)
  - Stream: 1
  - Requirements: [7.1](requirements.md#7.1), [7.2](requirements.md#7.2), [7.3](requirements.md#7.3), [7.4](requirements.md#7.4), [9.1](requirements.md#9.1), [9.2](requirements.md#9.2), [9.3](requirements.md#9.3)
