---
references:
    - requirements.md
    - design.md
    - decision_log.md
---
# Tasks: Stat Comparisons (T-1161)

## Compare types & formatting

- [x] 1. Write tests for Compare value types <!-- id:khmt97j -->
  - File: Flux/FluxTests/CompareValueTypesTests.swift (new)
  - ComparePeriod: rawValue, dayOffset (-1 / -7), displayName ("Yesterday" / "7 days ago"), parseOrDefault for nil / "" / unknown rawValue
  - ComparisonState.isUnavailable for each case
  - ComparisonSnapshot.from(response:) cases: present-summary-and-dailyUsage, no-summary-and-no-dailyUsage→nil, summary-with-all-nil-SR-fields, partial-availability (summary present, dailyUsage nil)
  - ComparisonSnapshot.houseUsed reuses HouseholdLoad.kwh; covers nil-input combinations
  - ComparisonSnapshot.peakGridImport: returns nil when either gridImport or offpeakGridImport is nil; otherwise max(0, gridImport - offpeakGridImport)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2)

- [x] 2. Implement Compare value types <!-- id:khmt97k -->
  - New files in Flux/Flux/DayDetail/Compare/: ComparePeriod.swift, SublineContent.swift, ComparisonState.swift, ComparisonSnapshot.swift
  - Match the type sketches in design.md "Components and Interfaces"
  - parseOrDefault falls back to .yesterday for unknown / nil rawValue
  - Blocked-by: khmt97j (Write tests for Compare value types)
  - Stream: 1
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2)

- [x] 3. Write tests for DeltaFormatter <!-- id:khmt97l -->
  - File: Flux/FluxTests/DeltaFormatterTests.swift (new)
  - sublineContent: returns .reserved when current is nil OR comparison is nil
  - sublineContent rounding: current=10.04 vs comparison=10.0 → .text("— kWh"); current=10.05 vs 10.0 → .text("▲ 0.1 kWh"); current=10.0 vs 10.05 → .text("▼ 0.1 kWh"); zero post-rounding → "—" branch
  - Sign formatting cases: positive → ▲, negative → ▼
  - voiceOverLabel composition with labelSub=nil, labelSub="paid", labelSub="free"; format matches AC 7.1 example
  - voiceOverFallbackLabel composition (no comparison clause)
  - Blocked-by: khmt97k (Implement Compare value types)
  - Stream: 1
  - Requirements: [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [4.3](requirements.md#4.3), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2)

- [x] 4. Implement DeltaFormatter <!-- id:khmt97m -->
  - File: Flux/Flux/DayDetail/Compare/DeltaFormatter.swift (new)
  - Use String(format: "%.1f", ...) for rounding; behaviour pinned by tests
  - Indicator selection on the rounded one-decimal display, not the raw difference
  - voiceOverLabel and voiceOverFallbackLabel produce strings matching AC 7.1
  - Blocked-by: khmt97l (Write tests for DeltaFormatter)
  - Stream: 1
  - Requirements: [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5), [4.3](requirements.md#4.3), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2)

- [x] 5. Write tests for ValueSubline <!-- id:khmt97n -->
  - File: Flux/FluxTests/ValueSublineTests.swift (new)
  - .hidden → body is EmptyView (zero frame)
  - .reserved → renders Text("\u{00A0}") in touTime + tertiaryText + .accessibilityHidden(true)
  - .text(s) → renders Text(s) with same modifiers
  - Measured-height equality: .reserved and .text("▲ 1.2 kWh") render at the same height at default Dynamic Type
  - Blocked-by: khmt97k (Implement Compare value types)
  - Stream: 1
  - Requirements: [3.7](requirements.md#3.7), [4.5](requirements.md#4.5)

- [x] 6. Implement ValueSubline <!-- id:khmt97o -->
  - File: Flux/Flux/DayDetail/Compare/ValueSubline.swift (new)
  - Single switch on SublineContent producing EmptyView / NBSP-Text / value-Text
  - All non-EmptyView branches apply .accessibilityHidden(true)
  - Blocked-by: khmt97n (Write tests for ValueSubline)
  - Stream: 1
  - Requirements: [3.7](requirements.md#3.7), [4.5](requirements.md#4.5)

## Row primitive & view model

- [x] 7. Write tests for FluxStatRow valueSub and accessibilityOverride <!-- id:khmt97p -->
  - File: Flux/FluxTests/FluxStatRowCompareTests.swift (new)
  - Default (no valueSub passed) preserves pre-feature row body — height assertion against a fixture
  - valueSub=.hidden equivalent to default
  - valueSub=.reserved adds slot — height greater than default by ValueSubline reserved height
  - valueSub=.text("▲ 0.6 kWh") renders the string at the right-aligned trailing edge under the value column
  - accessibilityOverride=nil: row reads as separate Text elements (default behaviour)
  - accessibilityOverride="..." applies .accessibilityElement(children: .ignore) and the override label
  - Blocked-by: khmt97o (Implement ValueSubline)
  - Stream: 1
  - Requirements: [1.4](requirements.md#1.4), [3.1](requirements.md#3.1), [3.3](requirements.md#3.3), [7.1](requirements.md#7.1)

- [x] 8. Modify FluxStatRow to support sub-line slot and accessibility override <!-- id:khmt97q -->
  - File: Flux/Flux/Helpers/FluxV5Components.swift (modify FluxStatRow)
  - Add valueSub: SublineContent = .hidden and accessibilityOverride: String? = nil
  - Wrap value HStack and ValueSubline in VStack(alignment: .trailing, spacing: 2)
  - Apply RowAccessibilityModifier(label: accessibilityOverride) — uses .accessibilityElement(children: .ignore) + .accessibilityLabel when non-nil; passthrough when nil
  - All 19 existing callsites stay source-compatible (defaults preserve pre-feature behaviour)
  - Blocked-by: khmt97p (Write tests for FluxStatRow valueSub and accessibilityOverride)
  - Stream: 1
  - Requirements: [1.4](requirements.md#1.4), [3.1](requirements.md#3.1), [3.3](requirements.md#3.3), [7.1](requirements.md#7.1)

- [x] 9. Write tests for DayDetailViewModel.updateCompare lifecycle <!-- id:khmt97r -->
  - File: Flux/FluxTests/DayDetailViewModelCompareTests.swift (new)
  - Toggle on triggers fetch and resolves to .ready on success
  - fetchDay throwing → .unavailable
  - Empty response (snapshot from returns nil) → .unavailable
  - Period change cancels in-flight task; the late task's outcome wins (use a slow-resolving mock)
  - Day-nav reissues fetch with new resolved date
  - Toggle off cancels and resets to .off
  - Date-resolution failure (corrupt viewModel.date) → synchronous .unavailable, no fetch issued
  - Stale fetch race: a slow Task A whose cancellation completes before a fast Task B starts must not overwrite Task B's .ready — verified by post-await Task.isCancelled guards
  - Blocked-by: khmt97k (Implement Compare value types)
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5), [2.4](requirements.md#2.4), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6)

- [x] 10. Implement updateCompare on DayDetailViewModel <!-- id:khmt97s -->
  - File: Flux/Flux/DayDetail/DayDetailViewModel.swift (modify)
  - Add comparisonState: ComparisonState property (private(set))
  - Add private comparisonTask: Task<Void, Never>?
  - updateCompare(enabled:period:) cancels existing task; sets .off when disabled; computes resolved compare date via Calendar.date(byAdding: .day, value: period.dayOffset, to: parsedDate) on sydneyCalendar; sets .loading; spawns Task that awaits fetchDay with Task.isCancelled guard before each state mutation; maps to .ready / .unavailable
  - Date-resolution failure short-circuits to synchronous .unavailable
  - Blocked-by: khmt97r (Write tests for DayDetailViewModel.updateCompare lifecycle)
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5), [2.4](requirements.md#2.4), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6)

- [x] 11. Write tests for CompareControl visibility logic <!-- id:khmt97t -->
  - File: Flux/FluxTests/CompareControlTests.swift (new)
  - enabled=false: period chip not in the body; failure caption not shown
  - enabled=true, unavailable=false: period chip shown with selected period; no caption
  - enabled=true, unavailable=true: period chip shown, caption "No comparison data available for {period.displayName}" rendered directly below the chip with leading alignment
  - Chip selection flips the period binding
  - Blocked-by: khmt97k (Implement Compare value types)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.4](requirements.md#1.4), [2.1](requirements.md#2.1), [2.5](requirements.md#2.5), [5.5](requirements.md#5.5)

- [x] 12. Implement CompareControl <!-- id:khmt97u -->
  - File: Flux/Flux/DayDetail/Compare/CompareControl.swift (new)
  - VStack(alignment: .leading, spacing: 4) with Toggle("Compare", isOn:) plus a period chip View when enabled, and a subdued caption Text below when enabled && unavailable
  - Caption uses FluxTheme.Typography.touTime and FluxTheme.Palette.tertiaryText, leading-aligned to the chip
  - Period chip uses the platform-standard Picker / Menu pattern; accessible with the period's displayName
  - Blocked-by: khmt97t (Write tests for CompareControl visibility logic)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.4](requirements.md#1.4), [2.1](requirements.md#2.1), [2.5](requirements.md#2.5), [5.5](requirements.md#5.5), [7.3](requirements.md#7.3)

## Panel integration

- [ ] 13. Write tests for SummaryBlock compare integration <!-- id:khmt97v -->
  - File: Flux/FluxTests/SummaryBlockCompareTests.swift (new)
  - compare=.off: each row's body matches pre-feature fixture (off-state row identity)
  - compare=.loading: every SR row passes valueSub=.reserved to its FluxStatRow
  - compare=.ready(snapshot): per-row valueSub computed via DeltaFormatter; per-row fallback when snapshot field is nil for that row; accessibilityOverride composed via DeltaFormatter.voiceOverLabel including labelSub="paid"/"free" on Grid in (peak)/Grid in (off-peak)
  - compare=.unavailable: every SR row passes valueSub=.reserved
  - "House used" comparison value uses HouseholdLoad.kwh on snapshot fields (matches selected day's formula)
  - Blocked-by: khmt97m (Implement DeltaFormatter), khmt97q (Modify FluxStatRow to support sub-line slot and accessibility override)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2)

- [ ] 14. Modify SummaryBlock to render compare deltas <!-- id:khmt97w -->
  - File: Flux/Flux/Helpers/SummaryBlock.swift (modify)
  - Add var compare: ComparisonState = .off parameter (default .off keeps Dashboard / other callers source-compatible)
  - Add private helpers valueSub(current:comparison:) and accessibilityOverride(rowLabel:labelSub:primary:current:comparison:) per the design's per-row mapping sketch
  - Pass valueSub: and accessibilityOverride: to each FluxStatRow in the SR list (Solar produced, House used, Grid in (peak), Grid in (off-peak), Grid out)
  - 15m avg load and Battery cycle rows do not participate (Day Detail does not render them; reaffirmed by SR scope)
  - Blocked-by: khmt97v (Write tests for SummaryBlock compare integration)
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2)

- [ ] 15. Write tests for DayInFiveBlocksPanel compare integration <!-- id:khmt97x -->
  - File: Flux/FluxTests/DayInFiveBlocksPanelCompareTests.swift (new)
  - compare=.off: rendered-height equality vs pre-feature fixture (off-state byte-identity scope from Decision 16)
  - compare=.loading: every row's total column receives ValueSubline(.reserved); daylight rows' solar column also receives ValueSubline(.reserved)
  - compare=.ready(snapshot) full-blocks: per-block per-column delta via DeltaFormatter; daylight rows show two independent sub-lines (solar and total can be in different states)
  - compare=.ready(snapshot) missing block of same kind: that row's deltas fall back to .reserved
  - compare=.ready(snapshot) daylight row with nil solarKwh on either side: solar sub-line falls back to .reserved
  - compare=.unavailable: every column reserved
  - Row a11y: when compare != .off, row applies .accessibilityElement(children: .ignore) with composed label via DeltaFormatter.voiceOverLabel / voiceOverFallbackLabel
  - Blocked-by: khmt97m (Implement DeltaFormatter), khmt97o (Implement ValueSubline)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2)

- [ ] 16. Modify DayInFiveBlocksPanel to render compare deltas <!-- id:khmt97y -->
  - File: Flux/Flux/DayDetail/DayInFiveBlocksPanel.swift (modify)
  - Add var compare: ComparisonState = .off parameter
  - Wrap each value column (solar on daylight rows, total on all rows) in a VStack(alignment: .trailing, spacing: 2) containing the existing Text and a new ValueSubline beneath, frame-aligned to the existing 76pt column width
  - Implement solarValueSub(for:) and totalValueSub(for:) per design
  - Apply .accessibilityElement(children: .ignore) + composed label on the row's outermost HStack when compare != .off
  - Blocked-by: khmt97x (Write tests for DayInFiveBlocksPanel compare integration)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2)

## Wiring

- [ ] 17. Wire DayDetailView: persistence, control hosting, and onChange reactions <!-- id:khmt97z -->
  - Files: Flux/Flux/DayDetail/DayDetailView.swift (modify), Flux/Flux/Helpers/UserDefaults+Flux.swift or equivalent (add static keys compareEnabledKey, comparePeriodKey)
  - Declare @AppStorage(UserDefaults.compareEnabledKey, store: UserDefaults.fluxAppGroup) private var compareEnabled: Bool = false
  - Declare @AppStorage(UserDefaults.comparePeriodKey, store: UserDefaults.fluxAppGroup) private var comparePeriodRaw: String = ComparePeriod.yesterday.rawValue
  - Add a Binding<ComparePeriod> bridging to comparePeriodRaw via ComparePeriod.parseOrDefault
  - Host CompareControl(enabled:period:unavailable:) directly above the existing `if let dailyUsage = viewModel.dailyUsage` block
  - Pass compare: viewModel.comparisonState into both DayInFiveBlocksPanel and the Day Detail SummaryBlock(summary:offpeakGridImport:showsBatteryCycle:) call
  - Three .onChange reactions on the screen: compareEnabled (initial: true), comparePeriodRaw, viewModel.date — each calls viewModel.updateCompare(enabled: compareEnabled, period: comparePeriod.wrappedValue)
  - Wiring/configuration only; no test pair required
  - Blocked-by: khmt97s (Implement updateCompare on DayDetailViewModel), khmt97u (Implement CompareControl), khmt97w (Modify SummaryBlock to render compare deltas), khmt97y (Modify DayInFiveBlocksPanel to render compare deltas)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.6](requirements.md#5.6), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2)
