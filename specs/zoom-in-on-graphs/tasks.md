---
references:
    - specs/zoom-in-on-graphs/requirements.md
    - specs/zoom-in-on-graphs/design.md
    - specs/zoom-in-on-graphs/decision_log.md
---
# Zoom in on Graphs

## Foundations

- [x] 1. Write Codable + Hashable tests for ChartKind and ChartScope <!-- id:1iiifug -->
  - Requirements: [3.2](requirements.md#3.2)
  - References: Flux/FluxTests/Charts/ChartKindTests.swift

- [x] 2. Implement ChartKind and ChartScope value types <!-- id:1iiifuh -->
  - Blocked-by: 1iiifug (Write Codable + Hashable tests for ChartKind and ChartScope)
  - Requirements: [3.2](requirements.md#3.2)
  - References: Flux/Flux/Charts/Expansion/ChartKind.swift

- [x] 3. Write tests for ChartScopeRegistry: write/read by kind, overwrite, independence <!-- id:1iiifui -->
  - Requirements: [3.2](requirements.md#3.2), [5.4](requirements.md#5.4)
  - References: Flux/FluxTests/Charts/ChartScopeRegistryTests.swift

- [x] 4. Implement ChartScopeRegistry as @Observable, injected at app root <!-- id:1iiifuj -->
  - Blocked-by: 1iiifui (Write tests for ChartScopeRegistry: write/read by kind, overwrite, independence)
  - Requirements: [3.2](requirements.md#3.2), [4.5](requirements.md#4.5), [5.4](requirements.md#5.4)
  - References: Flux/Flux/Charts/Expansion/ChartScopeRegistry.swift

- [x] 5. Declare ChartExpansionAction and EnvironmentValues.chartExpansion entry <!-- id:1iiifuk -->
  - Blocked-by: 1iiifuh (Implement ChartKind and ChartScope value types)
  - Requirements: [1.3](requirements.md#1.3)
  - References: Flux/Flux/Charts/Expansion/ChartExpansionAction.swift

## Selection gates

- [x] 6. Write tests for HistoryDragGate (drag lifecycle, pending-flush, no leak across drags) <!-- id:1iiiful -->
  - Requirements: [4.6](requirements.md#4.6)
  - References: Flux/FluxTests/Charts/HistoryDragGateTests.swift

- [x] 7. Implement HistoryDragGate and ChartSelectionGate protocol <!-- id:1iiifum -->
  - Blocked-by: 1iiiful (Write tests for HistoryDragGate (drag lifecycle, pending-flush, no leak across drags))
  - Requirements: [4.6](requirements.md#4.6)
  - References: Flux/Flux/Charts/Expansion/ChartSelectionGate.swift

- [x] 8. Write tests for XSelectionQuiescenceGate (400 ms quiet window, immediate after clear) <!-- id:1iiifun -->
  - Requirements: [4.6](requirements.md#4.6)
  - References: Flux/FluxTests/Charts/XSelectionQuiescenceGateTests.swift

- [x] 9. Implement XSelectionQuiescenceGate <!-- id:1iiifuo -->
  - Blocked-by: 1iiifun (Write tests for XSelectionQuiescenceGate (400 ms quiet window, immediate after clear)), 1iiifum (Implement HistoryDragGate and ChartSelectionGate protocol)
  - Requirements: [4.6](requirements.md#4.6)
  - References: Flux/Flux/Charts/Expansion/ChartSelectionGate.swift

## Container and button

- [x] 10. Write tests for ExpandableChartContainer (button overlay, tap invokes expansion with kind+scope) <!-- id:1iiifup -->
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.5](requirements.md#1.5)
  - References: Flux/FluxTests/Charts/ExpandableChartContainerTests.swift

- [x] 11. Implement ExpandableChartContainer (top-trailing 8pt-inset button, label 'Expand chart') <!-- id:1iiifuq -->
  - Blocked-by: 1iiifup (Write tests for ExpandableChartContainer (button overlay, tap invokes expansion with kind+scope)), overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, overlay, invokes, 1iiifuk (Declare ChartExpansionAction and EnvironmentValues.chartExpansion entry)
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5)
  - References: Flux/Flux/Charts/Expansion/ExpandableChartContainer.swift

## Day Detail selection lift

- [x] 12. Refactor PowerChartView and BatteryCombinedChartView to accept @Binding<Date?>; lift state into DayDetailView; update previews to .constant(nil) <!-- id:1iiifuv -->
  - Requirements: [4.1](requirements.md#4.1), [4.4](requirements.md#4.4)
  - References: Flux/Flux/DayDetail/PowerChartView.swift, Flux/Flux/DayDetail/BatteryCombinedChartView.swift, Flux/Flux/DayDetail/DayDetailViewSupport.swift, Flux/Flux/DayDetail/DayDetailView.swift

## Enlarged views

- [x] 13. Write tests for ExpandedChartView router (each ChartKind mounts correct host type) <!-- id:1iiifur -->
  - Requirements: [1.3](requirements.md#1.3), [2.4](requirements.md#2.4), [4.1](requirements.md#4.1), [4.2](requirements.md#4.2)
  - References: Flux/FluxTests/Charts/ExpandedChartViewTests.swift

- [x] 14. Implement ExpandedChartView router (kind -> ExpandedHistoryHost / ExpandedDayHost) <!-- id:1iiifus -->
  - Blocked-by: 1iiifur (Write tests for ExpandedChartView router (each ChartKind mounts correct host type)), correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, correct, 1iiifuj (Implement ChartScopeRegistry as @Observable, injected at app root)
  - Requirements: [1.3](requirements.md#1.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [4.1](requirements.md#4.1), [4.2](requirements.md#4.2)
  - References: Flux/Flux/Charts/Expansion/ExpandedChartView.swift

- [x] 15. Implement ExpandedHistoryHost for solar/grid-usage/daily-usage (reuses inline body, historySelectionOverlay + HistoryDragGate) <!-- id:1iiifut -->
  - Blocked-by: 1iiifus (Implement ExpandedChartView router (kind -> ExpandedHistoryHost / ExpandedDayHost)), 1iiifum (Implement HistoryDragGate and ChartSelectionGate protocol)
  - Requirements: [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [4.2](requirements.md#4.2), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6)
  - References: Flux/Flux/Charts/Expansion/ExpandedHistoryHost.swift

- [x] 16. Write integration tests for ExpandedHistoryHost (refresh deferral during drag; clear unblocks) <!-- id:1iiifuu -->
  - Blocked-by: 1iiifut (Implement ExpandedHistoryHost for solar/grid-usage/daily-usage (reuses inline body, historySelectionOverlay + HistoryDragGate))
  - Requirements: [4.5](requirements.md#4.5), [4.6](requirements.md#4.6)
  - References: Flux/FluxTests/Charts/ExpandedHistoryHostTests.swift

- [x] 17. Implement ExpandedDayHost for power/battery-combined (iOS binds to DayDetailView; macOS owns @State; uses XSelectionQuiescenceGate) <!-- id:1iiifuw -->
  - Blocked-by: 1iiifus (Implement ExpandedChartView router (kind -> ExpandedHistoryHost / ExpandedDayHost)), 1iiifuo (Implement XSelectionQuiescenceGate), 1iiifuv (Refactor PowerChartView and BatteryCombinedChartView to accept @Binding<Date?>; lift state into DayDetailView; update previews to .constant(nil))
  - Requirements: [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [4.1](requirements.md#4.1), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6)
  - References: Flux/Flux/Charts/Expansion/ExpandedDayHost.swift

- [x] 18. Write integration tests for ExpandedDayHost (selection mirrors back; refresh deferred in quiet window) <!-- id:1iiifux -->
  - Blocked-by: 1iiifuw (Implement ExpandedDayHost for power/battery-combined (iOS binds to DayDetailView; macOS owns @State; uses XSelectionQuiescenceGate))
  - Requirements: [4.1](requirements.md#4.1), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6)
  - References: Flux/FluxTests/Charts/ExpandedDayHostTests.swift

## iOS presentation

- [x] 19. Write tests for OrientationLock singleton (refcount enter/exit, balanced restoration) <!-- id:1iiifuy -->
  - Requirements: [2.2](requirements.md#2.2), [2.6](requirements.md#2.6)
  - References: Flux/FluxTests/Charts/OrientationLockTests.swift

- [x] 20. Implement OrientationLock singleton + wire application(_:supportedInterfaceOrientationsFor:) in FluxAppDelegate <!-- id:1iiifuz -->
  - Blocked-by: 1iiifuy (Write tests for OrientationLock singleton (refcount enter/exit, balanced restoration))
  - Requirements: [2.2](requirements.md#2.2), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7)
  - References: Flux/Flux/Charts/Expansion/iOS/OrientationLock.swift, Flux/Flux/FluxApp.swift

- [x] 21. Implement OrientationLandscapeScope (UIViewControllerRepresentable) with viewWillAppear/Disappear sequencing and SwiftUI .onDisappear belt-and-braces <!-- id:1iiifv0 -->
  - Blocked-by: 1iiifuz (Implement OrientationLock singleton + wire application(_:supportedInterfaceOrientationsFor:) in FluxAppDelegate)
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7)
  - References: Flux/Flux/Charts/Expansion/iOS/OrientationLandscapeScope.swift

- [x] 22. Write tests for ExpandedChartTopHandle (60pt drag threshold; horizontal drag inert; frame above chart drawing area) <!-- id:1iiifv1 -->
  - Requirements: [2.3](requirements.md#2.3)
  - References: Flux/FluxTests/Charts/ExpandedChartTopHandleTests.swift

- [x] 23. Implement ExpandedChartTopHandle (visible 32pt drag-indicator band above title) <!-- id:1iiifv2 -->
  - Blocked-by: 1iiifv1 (Write tests for ExpandedChartTopHandle (60pt drag threshold; horizontal drag inert; frame above chart drawing area)), drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing, drawing
  - Requirements: [2.3](requirements.md#2.3)
  - References: Flux/Flux/Charts/Expansion/iOS/ExpandedChartTopHandle.swift

- [x] 24. Wire RootView (iOS): @SceneStorage('expandedChart'), .fullScreenCover, iOS ChartExpansionAction writing registry then storage <!-- id:1iiifv3 -->
  - Blocked-by: 1iiifv0 (Implement OrientationLandscapeScope (UIViewControllerRepresentable) with viewWillAppear/Disappear sequencing and SwiftUI .onDisappear belt-and-braces), 1iiifv2 (Implement ExpandedChartTopHandle (visible 32pt drag-indicator band above title)), visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, visible, 1iiifus (Implement ExpandedChartView router (kind -> ExpandedHistoryHost / ExpandedDayHost)), 1iiifuk (Declare ChartExpansionAction and EnvironmentValues.chartExpansion entry), 1iiifuj (Implement ChartScopeRegistry as @Observable, injected at app root)
  - Requirements: [1.3](requirements.md#1.3), [2.1](requirements.md#2.1), [5.1](requirements.md#5.1), [5.3](requirements.md#5.3), [5.5](requirements.md#5.5), [6.4](requirements.md#6.4)
  - References: Flux/Flux/RootView.swift

- [x] 25. Write iOS integration tests: tap expand on each card, verify cover/host/dismiss/orientation reset (including tab-switch teardown) <!-- id:1iiifv4 -->
  - Blocked-by: 1iiifv3 (Wire RootView (iOS): @SceneStorage('expandedChart'), .fullScreenCover, iOS ChartExpansionAction writing registry then storage), writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing
  - Requirements: [2.1](requirements.md#2.1), [2.6](requirements.md#2.6), [5.1](requirements.md#5.1), [5.3](requirements.md#5.3)
  - References: Flux/FluxTests/Charts/iOSExpandIntegrationTests.swift

## macOS presentation

- [x] 26. Write tests for macOS ChartSceneObserver (60s polling, pause when inactive, switches on scope change) <!-- id:1iiifv5 -->
  - Requirements: [4.5](requirements.md#4.5), [5.4](requirements.md#5.4)
  - References: Flux/FluxTests/Charts/MacOSScopedObserverTests.swift

- [x] 27. Implement ChartSceneObserver wrapping FluxAPIClient with appearsActive-tiered 60s polling <!-- id:1iiifv6 -->
  - Blocked-by: 1iiifv5 (Write tests for macOS ChartSceneObserver (60s polling, pause when inactive, switches on scope change)), polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling, polling
  - Requirements: [4.5](requirements.md#4.5), [5.4](requirements.md#5.4)
  - References: Flux/Flux/Charts/Expansion/macOS/ChartSceneObserver.swift

- [x] 28. Implement macOS chart-detail WindowGroup with .defaultLaunchBehavior(.suppressed), .windowManagerRole(.associated), defaultSize, contentMinSize, min 720x480 <!-- id:1iiifv7 -->
  - Blocked-by: 1iiifv6 (Implement ChartSceneObserver wrapping FluxAPIClient with appearsActive-tiered 60s polling), 1iiifus (Implement ExpandedChartView router (kind -> ExpandedHistoryHost / ExpandedDayHost)), 1iiifuj (Implement ChartScopeRegistry as @Observable, injected at app root)
  - Requirements: [3.1](requirements.md#3.1), [3.6](requirements.md#3.6), [3.7](requirements.md#3.7), [3.8](requirements.md#3.8)
  - References: Flux/Flux/Charts/Expansion/macOS/ChartScene.swift, Flux/Flux/FluxApp.swift

- [x] 29. Implement macOS ChartExpansionAction using openWindow(id:value:) after writing scope to registry <!-- id:1iiifv8 -->
  - Blocked-by: 1iiifv7 (Implement macOS chart-detail WindowGroup with .defaultLaunchBehavior(.suppressed), .windowManagerRole(.associated), defaultSize, contentMinSize, min 720x480), 1iiifuk (Declare ChartExpansionAction and EnvironmentValues.chartExpansion entry)
  - Requirements: [1.3](requirements.md#1.3), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5)
  - References: Flux/Flux/FluxApp.swift

- [x] 30. Write macOS integration tests: same kind twice -> 1 window; different kinds -> 2; relaunch -> no restore <!-- id:1iiifv9 -->
  - Blocked-by: 1iiifv8 (Implement macOS ChartExpansionAction using openWindow(id:value:) after writing scope to registry), writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing, writing
  - Requirements: [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.7](requirements.md#3.7)
  - References: Flux/Flux/Charts/Expansion/macOS/ChartSceneIntegrationTests.swift

## Apply to charts

- [x] 31. Write expansion tests for the three History cards (correct kind + historyRange scope from current range) <!-- id:1iiifva -->
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3)
  - References: Flux/FluxTests/Charts/HistoryCardExpansionTests.swift

- [x] 32. Wrap HistorySolarCard / HistoryGridUsageCard / HistoryDailyUsageCard with ExpandableChartContainer (historyRange scope) <!-- id:1iiifvb -->
  - Blocked-by: 1iiifva (Write expansion tests for the three History cards (correct kind + historyRange scope from current range)), correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, correct, current, 1iiifuq (Implement ExpandableChartContainer (top-trailing 8pt-inset button, label 'Expand chart'))
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4)
  - References: Flux/Flux/History/HistorySolarCard.swift, Flux/Flux/History/HistoryGridUsageCard.swift, Flux/Flux/History/HistoryDailyUsageCard.swift

- [x] 33. Write expansion tests for the two Day Detail charts (dayPower/dayBatteryCombined + daySpecific(date:) scope) <!-- id:1iiifvc -->
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3)
  - References: Flux/FluxTests/Charts/DayChartExpansionTests.swift

- [x] 34. Wrap PowerChartView and BatteryCombinedChartView with ExpandableChartContainer (daySpecific scope from DayDetailView) <!-- id:1iiifvd -->
  - Blocked-by: 1iiifvc (Write expansion tests for the two Day Detail charts (dayPower/dayBatteryCombined + daySpecific(date:) scope)), 1iiifuq (Implement ExpandableChartContainer (top-trailing 8pt-inset button, label 'Expand chart')), 1iiifuv (Refactor PowerChartView and BatteryCombinedChartView to accept @Binding<Date?>; lift state into DayDetailView; update previews to .constant(nil))
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4)
  - References: Flux/Flux/DayDetail/PowerChartView.swift, Flux/Flux/DayDetail/BatteryCombinedChartView.swift

## Polish

- [ ] 35. Write tests for VoiceOver/keyboard focus return to the expand button after dismissal <!-- id:1iiifve -->
  - Requirements: [6.2](requirements.md#6.2)
  - References: Flux/FluxTests/Charts/ExpansionAccessibilityTests.swift

- [ ] 36. Implement AccessibilityFocusState capture/restore around expansion in ExpandableChartContainer <!-- id:1iiifvf -->
  - Blocked-by: 1iiifve (Write tests for VoiceOver/keyboard focus return to the expand button after dismissal), 1iiifuq (Implement ExpandableChartContainer (top-trailing 8pt-inset button, label 'Expand chart'))
  - Requirements: [6.2](requirements.md#6.2)
  - References: Flux/Flux/Charts/Expansion/ExpandableChartContainer.swift

- [ ] 37. Implement Reduce Motion cross-fade transition and verify Dynamic Type scaling for title/header/axis/callouts <!-- id:1iiifvg -->
  - Blocked-by: 1iiifus (Implement ExpandedChartView router (kind -> ExpandedHistoryHost / ExpandedDayHost))
  - Requirements: [6.3](requirements.md#6.3), [6.4](requirements.md#6.4)
  - References: Flux/Flux/Charts/Expansion/ExpandedChartView.swift

- [ ] 38. Verify expand-button glyph WCAG-AA contrast on lightest solar fill (light mode); switch to primaryText if fails <!-- id:1iiifvh -->
  - Blocked-by: 1iiifuq (Implement ExpandableChartContainer (top-trailing 8pt-inset button, label 'Expand chart'))
  - Requirements: [1.2](requirements.md#1.2)
  - References: Flux/Flux/Charts/Expansion/ExpandableChartContainer.swift
