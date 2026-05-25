---
references:
    - specs/better-macos-interface/requirements.md
    - specs/better-macos-interface/design.md
    - specs/better-macos-interface/decision_log.md
---
# Tasks: Better macOS Interface (T-1342)

## Implementation

- [ ] 1. Add macOS layout gate test <!-- id:n55vfw7 -->
  - Create a Swift Testing test (compile-gated `#if os(macOS)`) that asserts `IPadLayoutGate.isActive(hSizeClass: nil) == true` on macOS.
  - The test must fail against the current implementation (which returns false on macOS). This is the red step.
  - Place either in a new `IPadLayoutGateTests.swift` or extend `AdaptiveColumnsLayoutTests` — pick one to match file conventions.
  - Stream: 1
  - Requirements: [7.1](requirements.md#7.1), [5.1](requirements.md#5.1)
  - References: Flux/FluxTests/IPadLayoutGateTests.swift (new file, or extend AdaptiveColumnsLayoutTests), Flux/Flux/Helpers/IPadLayoutGate.swift

- [ ] 2. Flip IPadLayoutGate to return true on macOS <!-- id:n55vfw8 -->
  - Change `#else return false #endif` to `#elseif os(macOS) return true #else return false #endif`.
  - Update the doc comment to drop the 'macOS never qualifies' sentence and add a one-liner about macOS always returning true (1-column tier unreachable at runtime due to FluxApp scene minWidth=960pt).
  - Verify task 1's test now passes. This also activates `dashboardContentRegular`, `dayDetailContentRegular`, and `historyContentRegular` on macOS (causes `legacyHeader` and DayDetail's legacy eyebrow `header` to drop on macOS via existing branch selection).
  - Per AC 5.3, the per-call-site audit is already documented in design.md — no code change for the audit itself.
  - Blocked-by: n55vfw7 (Add macOS layout gate test)
  - Stream: 1
  - Requirements: [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [2.1](requirements.md#2.1), [3.1](requirements.md#3.1)
  - References: Flux/Flux/Helpers/IPadLayoutGate.swift

- [ ] 3. Add DayDetail macOS toolbar and nav-title formatter tests <!-- id:n55vfw9 -->
  - Create `DayDetailMacToolbarTests` (compile-gated `#if os(macOS)`) with three assertions: (a) when `viewModel.isToday == true`, the disabled-state predicate the toolbar reads matches; (b) when `viewModel.isToday == false`, it is enabled; (c) invoking `viewModel.navigatePrevious()` / `viewModel.navigateNext()` updates `viewModel.date` to the previous / next Sydney-local day.
  - Create `DayDetailNavTitleFormatterTests` (compile-gated `#if os(macOS)`) asserting `macPageTitle` resolves to "Today" when `viewModel.isToday`, otherwise the `DayDetailEyebrow.full` formatted string (system `.full` date style for the parsed date).
  - Both tests must fail against the current code (the property does not exist yet). This is the red step.
  - Use the existing `DayDetailViewModel` stub-API-client pattern from `DayDetailViewModelTests` for setup.
  - Stream: 1
  - Requirements: [7.2](requirements.md#7.2), [7.4](requirements.md#7.4)
  - References: Flux/FluxTests/DayDetailMacToolbarTests.swift (new), Flux/FluxTests/DayDetailNavTitleFormatterTests.swift (new), Flux/Flux/DayDetail/DayDetailView.swift

- [ ] 4. Update DayDetailView for macOS chrome <!-- id:n55vfwa -->
  - Inside `dayDetailContentRegular` (around line 159), wrap the existing `DayNavigationHeader(viewModel: viewModel)` call in `#if !os(macOS)` / `#endif`.
  - Add an `internal var macPageTitle: String` computed property gated `#if os(macOS)` that returns "Today" when `viewModel.isToday` is true, otherwise `DayDetailEyebrow.full.string(from: parsedDate)` with a fallback to raw `viewModel.date` when parsing fails (mirror `DayNavigationHeader.formattedDate`).
  - After the existing `#if os(iOS)` toolbar/title block (line 69-79), add a `#if os(macOS)` block: `.navigationTitle(macPageTitle)` plus `.toolbar { ToolbarItemGroup(placement: .primaryAction) { ... } }` containing two buttons. Previous-day button: `Image(systemName: "chevron.left")` label, calls `viewModel.navigatePrevious()`, `.accessibilityLabel("Previous day")`, `.help("Previous day")`. Next-day button: `Image(systemName: "chevron.right")` label, calls `viewModel.navigateNext()`, `.accessibilityLabel("Next day")`, `.help("Next day")`, `.disabled(viewModel.isToday)`.
  - Do not add `.keyboardShortcut` to the toolbar buttons — would double-fire with the existing `.onKeyPress(.leftArrow)` / `.onKeyPress(.rightArrow)` handlers at line 97-105 (keep those handlers unchanged for AC 4.7).
  - Verify task 3's tests now pass.
  - Blocked-by: n55vfw8 (Flip IPadLayoutGate to return true on macOS), n55vfw9 (Add DayDetail macOS toolbar and nav-title formatter tests)
  - Stream: 1
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.5](requirements.md#4.5), [4.6](requirements.md#4.6), [4.7](requirements.md#4.7), [4.8](requirements.md#4.8), [4.9](requirements.md#4.9), [2.1](requirements.md#2.1)
  - References: Flux/Flux/DayDetail/DayDetailView.swift

- [ ] 5. Wire macOS chrome on Dashboard and FluxApp scene <!-- id:n55vfwb -->
  - DashboardView: add a `#if os(macOS) .navigationTitle("Dashboard") #endif` block in the body's modifier chain (the existing `.navigationTitle(...)` is inside `#if os(iOS)` so on macOS the title is empty without this change).
  - FluxApp macOS `WindowGroup` (lines 42-52): add `.frame(minWidth: 960, minHeight: 600)` to the `AppNavigationView` instance inside the WindowGroup closure; add `.defaultSize(width: 1200, height: 800)` and `.windowResizability(.contentSize)` as scene modifiers (alongside the existing `.modelContainer(...)` and `.commands { ... }` chain).
  - Critical: `.windowResizability(.contentSize)` is required — without it `.frame(minWidth:minHeight:)` alone does NOT clamp the macOS NSWindow.
  - Configuration-only change; no preceding test required per the skill's wiring exemption. Manual verification (AC 2.2) is that the window cannot be resized below 960×600.
  - Blocked-by: n55vfw8 (Flip IPadLayoutGate to return true on macOS)
  - Stream: 1
  - Requirements: [3.2](requirements.md#3.2), [2.2](requirements.md#2.2)
  - References: Flux/Flux/Dashboard/DashboardView.swift, Flux/Flux/FluxApp.swift
