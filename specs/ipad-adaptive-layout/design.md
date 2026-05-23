# Design: iPad Adaptive Layout

## Overview

Add a regular-size-class shell for iPad (`FluxiPadRoot`) that hosts the same `Dashboard`, `DayDetailView`, and `HistoryView` screens inside a `NavigationSplitView` with sidebar, and adaptive multi-column content in each screen. The existing `FluxiOSRoot` shell remains the compact-size-class path; the macOS shell is unchanged. View-model ownership is hoisted into `AppNavigationView` so cached state survives shell swaps.

## Architecture

### Size-class branch

The top-level platform switch in `AppNavigationView.rootView` currently picks between `macOSRoot` (`#if os(macOS)`) and `iOSRoot` (`#if !os(macOS)`). The iOS branch becomes size-class-and-idiom-aware in-place — no new wrapper type:

```swift
#if !os(macOS)
@Environment(\.horizontalSizeClass) private var hSizeClass

@ViewBuilder
private var iOSRoot: some View {
    if let apiClient {
        if usesPadShell {
            FluxiPadRoot(
                apiClient: apiClient,
                selectedScreen: $selectedScreen,
                navigationPath: $navigationPath,
                today: $today,
                dashboardViewModel: dashboardViewModel,
                historyViewModel: historyViewModel,
                todayDayDetailViewModel: todayDayDetailViewModel
            )
        } else {
            FluxiOSRoot(
                apiClient: apiClient,
                tab: $iosTab,
                dashboardViewModel: dashboardViewModel,
                historyViewModel: historyViewModel
            )
        }
    } else {
        SettingsView(onSaved: handleSettingsSaved)
    }
}

private var usesPadShell: Bool {
    // Gate on idiom + size class. iPhone Plus/Max in landscape reports
    // .regular horizontal but must keep FluxiOSRoot (AC 7.1).
    UIDevice.current.userInterfaceIdiom == .pad && hSizeClass == .regular
}
#endif
```

All iPad sizes in full-screen and ≥ ½ Split View report `.regular`; Slide Over and narrow Split View widths report `.compact` and fall through to `FluxiOSRoot`. iPhone always uses `FluxiOSRoot`. The idiom check is a regression guard for iPhone Plus/Max landscape, which reports `.regular` horizontal on some models — without the guard, those iPhones would render the sidebar shell.

> Per Apple's [`horizontalSizeClass`](https://developer.apple.com/documentation/swiftui/environmentvalues/horizontalsizeclass) docs: SwiftUI reports `.regular` for iPad in full-screen and ½ Split View, `.compact` for Slide Over and ⅓ Split View. The idiom gate is added only because iPhone Plus/Max landscape also reports `.regular`, which would silently regress AC 7.1.

### State ownership

Three view-models must outlive the shell branch (AC 6.4):

| View-model | Today | Tomorrow |
|---|---|---|
| `DashboardViewModel` | Owned in `FluxiOSRoot` | **Hoist to `AppNavigationView`** |
| `HistoryViewModel` | Owned in `HistoryView` | **Hoist to `AppNavigationView`** |
| `DayDetailViewModel` ("Today" entry) | Owned in `DayDetailView` | **Hoist to `AppNavigationView`**; date mutated via `setDate(_:)` on rollover |
| `DayDetailViewModel` (pushed from History) | Owned in pushed view | Unchanged — pushed views recreate on push anyway |

Hoisting means `AppNavigationView` owns the three "always-live" view-models as `@State` and passes them as initializer arguments into both `FluxiPadRoot` and `FluxiOSRoot`. The existing iPhone behaviour (Dashboard VM owned at root, History/Today VMs reconstructed cheaply on tab show) is preserved when iPhone never crosses the shell boundary; the new constraint only applies on iPad when size class flips.

`@State` ownership in `AppNavigationView` makes the state per-scene automatically (AC 6.5).

#### API-client replacement

`AppNavigationView.reloadDependencies()` creates a fresh `URLSessionAPIClient` when credentials change (e.g. after Settings save). Hoisted VMs hold their constructor-time `apiClient` reference, so a credentials change must rebuild them — otherwise the VMs would continue using the stale (unauthorized) client. `reloadDependencies()` therefore rebuilds the three hoisted VMs whenever the client identity changes:

```swift
private func reloadDependencies() {
    let client = makeAPIClient()
    let clientChanged = (apiClient as AnyObject?) !== (client as AnyObject?)
    apiClient = client
    if clientChanged, let client {
        dashboardViewModel = DashboardViewModel(apiClient: client)
        historyViewModel = HistoryViewModel(apiClient: client, modelContext: modelContext)
        todayDayDetailViewModel = DayDetailViewModel(date: today, apiClient: client)
    }
    if let client { SoCAlertsService.shared.bind(apiClient: client) }
    selectedScreen = apiClient == nil ? .settings : (selectedScreen ?? .dashboard)
}
```

Identity check is `AnyObject` reference equality — `URLSessionAPIClient` is a reference type, and the existing init only returns a new instance when the URL or token actually changes. Side effect: changing credentials kills in-flight refreshes and starts fresh fetches, which matches user expectation when they reconfigure the API.

### Sidebar ↔ tab binding

`AppNavigationView` already owns `selectedScreen: Screen?` (used by macOS) and `iosTab: FluxTab` (used by `FluxiOSRoot`). On iPad, both shells can be visible across a single user session via size-class transitions, so they must stay in sync.

```swift
// AppNavigationView body
.onChange(of: selectedScreen) { _, new in
    if let new, let tab = new.tab { iosTab = tab }
}
.onChange(of: iosTab) { _, new in
    if selectedScreen.map(\.tab) != new {
        selectedScreen = Screen(tab: new)
    }
}
```

`Screen.tab` already exists (`AppNavigationView.swift` line 193). Add the inverse:

```swift
extension Screen {
    init(tab: FluxTab) {
        switch tab {
        case .dashboard: self = .dashboard
        case .today: self = .today
        case .history: self = .history
        }
    }
}
```

The sync runs in both directions but is idempotent: `selectedScreen` → `iosTab` only writes if they differ; `iosTab` → `selectedScreen` does the same. The macOS `storedSelection: @SceneStorage` write inside the existing `onChange(of: selectedScreen)` continues to apply only on macOS (`#if os(macOS)`).

### NavigationPath in the iPad shell

Single `NavigationPath` in the detail column, owned by `AppNavigationView` (already exists — `navigationPath` on line 11). Sidebar selection clears it via the existing `onChange(of: selectedScreen)` handler.

When the size class flips from regular → compact while the user is viewing a pushed Day Detail in the iPad detail column:
- The iPad shell unmounts, the iPhone shell mounts.
- The shared `navigationPath` is replaced by the per-tab paths inside `FluxiOSRoot`.
- AC 6.3 floor: if the user was on `HistoryRoute.dayDetail(date)` in the iPad shell, the iPhone shell lands on the History tab's root (`historyPath = NavigationPath()`). Day Detail is not preserved across this transition. This matches the AC's "OR" clause: return to the History list. The `historyPath` is empty, so no invalid state.

Going the other way (compact → regular while viewing a pushed Day Detail in the History tab): drop the push, sidebar shows History at its root. Same floor.

A future refinement could thread the in-progress Day Detail date across, but AC 6.3 explicitly permits the "return to root" option.

### Today entry: midnight rollover

The hoisted Today `DayDetailViewModel` is reused across midnight rather than rebuilt. The view-model gains a `setDate(_:)` method that updates its internal date and reloads:

```swift
// DayDetailViewModel
func setDate(_ newDate: String) async {
    guard newDate != date else { return }
    comparisonTask?.cancel()
    date = newDate
    readings = []
    parsedReadings = []
    summary = nil
    peakPeriods = []
    dailyUsage = nil
    note = nil
    offpeakStats = .empty
    comparisonState = .off
    await loadDay()
}
```

`AppNavigationView` owns the date string and recompute timer:

```swift
@State private var today: String = DateFormatting.todayDateString()

.task {
    while !Task.isCancelled {
        try? await Task.sleep(for: .seconds(60))
        let now = DateFormatting.todayDateString()
        guard now != today else { continue }
        today = now
        await todayDayDetailViewModel.setDate(now)
    }
}
.onChange(of: scenePhase) { _, phase in
    guard phase == .active else { return }
    let now = DateFormatting.todayDateString()
    guard now != today else { return }
    Task {
        today = now
        await todayDayDetailViewModel.setDate(now)
    }
}
```

`FluxiPadRoot`'s Today branch renders `DayDetailView(viewModel: todayDayDetailViewModel)` — the existing init that accepts a view-model directly, so the view never recreates the VM. The `.id(today)` rebuild approach was rejected: it would discard transient UI state (chart highlight, scroll position, in-progress note draft, `comparisonState`) every midnight. `setDate(_:)` resets only the per-day fields and leaves view-level state untouched.

The 60s tick is fine: AlphaESS data has minute-scale granularity and the Dashboard already auto-refreshes on a 10s timer, so any user actively watching the app will see the date change within a minute of midnight.

The iPhone "Today" tab and macOS Today entry continue to construct a fresh `DayDetailViewModel(date: todayDateString())` on appearance — they don't need the rollover task because the VM is recreated on every tab/sidebar select.

### Deep link handling

`DeepLinkHandler.handle` returns `Action.navigate(Screen)`. The existing `onOpenURL` handler in `AppNavigationView.body` (lines 58–67) writes both `selectedScreen` and `iosTab` already:

```swift
selectedScreen = screen
navigationPath = NavigationPath()
iosTab = screen.tab ?? .dashboard
```

On iPad regular, the sidebar is bound to `selectedScreen` → the deep-linked screen renders. On iPad compact, `iosTab` drives `FluxiOSRoot`. No new code is required for AC 1.8.

### Per-screen adaptive layouts

#### Breakpoints

| Container width | Tier |
|---|---|
| `< 700` | Single column (matches iPhone layout) |
| `700 ≤ w < 1000` | 2 columns |
| `≥ 1000` | 3 columns |

Width is measured at the screen's outermost `ScrollView` content via `GeometryReader` (one per screen, not nested). The reasoning below is **provisional** — actual detail-column widths depend on the `NavigationSplitView` style (see next section) and must be measured on simulators before locking thresholds. Locked values are recorded in `implementation.md` (AC 9.3).

Hypothesised widths (to be confirmed):

- iPad mini portrait full-screen ≈ 744pt total; with system sidebar the detail may be ~430–500pt — single column.
- iPad mini landscape detail ≈ 770–820pt — 2-col.
- iPad Pro 13" portrait detail ≈ 760–1024pt depending on whether the sidebar auto-hides — 2 or 3 col.
- iPad Pro 13" landscape detail ≈ 1080pt — 3-col.

If the system reports `.regular` for a detail column narrower than 700pt (e.g. iPad Pro 13" ½ Split View at ~507pt window where the system keeps regular size class), the layout falls back to single column via the `< 700` tier — no separate fallback path needed.

#### `NavigationSplitView` style

`FluxiPadRoot` uses `.navigationSplitViewStyle(.balanced)`. On iPad Pro 13" portrait the balanced style lets the sidebar coexist with the detail; on iPad mini portrait it can auto-hide the sidebar (showing a sidebar toggle), giving more room to the detail. macOS keeps its current default style. The choice is recorded in `decision_log.md` (added below as Decision 5).

#### Helper: `AdaptiveColumnsLayout`

A reusable view that takes a list of card builders and lays them out in 1/2/3 columns based on container width and Dynamic Type:

```swift
struct AdaptiveColumnsLayout<Content: View>: View {
    let minCardWidth: CGFloat   // 320 default
    let spacing: CGFloat        // FluxTheme.Metrics.panelGap
    @ViewBuilder let content: () -> Content

    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        GeometryReader { proxy in
            let cols = columnCount(width: proxy.size.width)
            // Lay out children in `cols` columns
        }
    }

    func columnCount(width: CGFloat) -> Int {
        let base: Int
        if width >= 1000 { base = 3 }
        else if width >= 700 { base = 2 }
        else { base = 1 }
        // AC 8.2: drop one column at AX4+ (battery-percent numerals and
        // multi-segment chart legends are the worst-case widths; both
        // tolerate AX1–AX3 in 2-col).
        return typeSize >= .accessibility4 ? max(1, base - 1) : base
    }
}
```

`AdaptiveColumnsLayout` is added in `Flux/Flux/Helpers/AdaptiveColumnsLayout.swift`. It replaces ad-hoc `LazyVGrid` configurations on each screen so the 1→2→3 collapse rule (AC 8.2) is implemented once.

The collapse trigger is `>= .accessibility4`, not `isAccessibilitySize` (which is AX1+). AX1–AX3 readability is required by AC 8.1; collapsing those sizes prematurely would empty the wider iPad layouts of their reason to exist.

#### Dashboard

```
┌──────────────────────────────────────────────────────────┐
│ FluxScreenHeader (hidden on iPad regular — sidebar plays │
│   the role; settings reachable via toolbar gear button)  │
├───────────────────────────┬──────────────────────────────┤
│ DashboardHeroPanel        │ LiveTrioPanel                │
│                           │                              │
├───────────────────────────┴──────────────────────────────┤
│ SummaryBlock (full width)                                │
├───────────────────────────┬──────────────────────────────┤
│ BatteryBlock              │ (3rd block when added)       │
└───────────────────────────┴──────────────────────────────┘
```

- At `w ≥ 700`: `DashboardHeroPanel` and `LiveTrioPanel` are in a 2-column row at the top. `SummaryBlock` spans full width below. `BatteryBlock` and any future blocks flow through `AdaptiveColumnsLayout` (2-col at 700+, 3-col at 1000+).
- At `w < 700`: existing single-column `VStack` layout.

The `FluxScreenHeader` tab bar is suppressed on iPad regular (the sidebar replaces it). The settings gear moves to a `.toolbar` `ToolbarItem(placement: .primaryAction)` on the detail's `NavigationStack`. On iPad compact, `FluxScreenHeader` continues to render the tab bar inline.

`DashboardView.contentContainer` adds a size-class branch that selects either `dashboardContentRegular` (the new 2-col arrangement) or `dashboardContent` (existing single column). Only the layout differs; the data path is identical.

#### History

```
┌──────────────────────────────────────────────────────────┐
│ Range picker (full width)                                │
│ NoteRowView (full width)                                 │
├──────────────────────────────────────────────────────────┤
│ HistoryStatsOverviewCard                                 │
│   (its own 4-col at regular / 2-col at compact branch    │
│    is preserved — AC 3.3)                                │
├───────────────────────────┬──────────────────────────────┤
│ HistorySolarCard          │ HistoryGridUsageCard         │
├───────────────────────────┼──────────────────────────────┤
│ HistoryDailyUsageCard     │ summaryCard (when selected)  │
└───────────────────────────┴──────────────────────────────┘
```

- At `w ≥ 700`: the four cards (Solar, Grid, Daily Usage, plus the conditionally rendered selected-day summary) flow through `AdaptiveColumnsLayout`.
- At `w < 700`: existing single-column stack.
- `HistoryStatsOverviewCard`'s internal 4-col/2-col branch is unchanged.

The "View day detail" `NavigationLink(value: HistoryRoute.dayDetail)` and `.navigationDestination(for: HistoryRoute.self)` continue to work — they sit inside whichever `NavigationStack` wraps `HistoryView` (the detail column's NavigationStack on iPad regular, the History tab's NavigationStack on iPad compact / iPhone).

#### Day Detail

```
┌──────────────────────────────────────────────────────────┐
│ DayNavigationHeader (prev / next day)                    │
│ DayDetailNoteSection                                     │
│ CompareControl                                           │
├───────────────────────────┬──────────────────────────────┤
│ DayInFiveBlocksPanel      │ PowerChartView               │
│ SummaryBlock              ├──────────────────────────────┤
│ BatteryBlock              │ BatteryPowerChartView        │
│ DailyUsageCard            ├──────────────────────────────┤
│ PeakUsageCard             │ SOCChartView                 │
└───────────────────────────┴──────────────────────────────┘
```

- At `w ≥ 700`: left column holds summary panels (DayInFiveBlocks, Summary, Battery, DailyUsage, PeakUsage). Right column stacks the three charts. The header row (DayNavigationHeader, NoteSection, CompareControl) spans full width. At this width each column is at least 320pt — the chart minimum-legible width — assuming a 20pt gutter and the standard horizontal padding.
- At `w < 700`: existing single-column layout.

If the implementation measurement on iPad mini landscape shows the chart column is unusably narrow at the lower end of this band, the Day Detail breakpoint is raised in `implementation.md` rather than baked into the design now.

`DayDetailView.body` adds a size-class branch that picks `dayDetailContentRegular` (two-column `Grid` with full-width header) or the existing `VStack`.

#### Settings sheet width cap

`SettingsView`'s root `Form` is wrapped in a width-capped container at regular size class:

```swift
Form { ... }
    .frame(maxWidth: 640)
    .frame(maxWidth: .infinity)  // center horizontally
```

The double-frame trick centers a 640pt content column inside the full sheet width. At compact size class, no cap applies (existing behaviour).

### Sidebar items source

`Screen.sidebarVisible` currently filters out `.today` on iOS (line 36–38 of `Screen.swift`). The macOS and iPad sidebars need an identical list (Dashboard / Today / History — no Settings entry per Decision 2). The simplest fix is to widen the existing `sidebarVisible` to expose Today on iOS as well:

```swift
static var sidebarVisible: [Screen] {
    // macOS uses the Settings scene (⌘,); iPad regular uses the per-screen
    // settings affordance. Neither shell wants a Settings sidebar row.
    Screen.allCases.filter { $0 != .settings }
}
```

This is a one-line change and removes the iOS-specific exclusion. `FluxiOSRoot` does not consume `Screen.sidebarVisible` (it uses its own `FluxTab` enum), so widening it has no effect on the iPhone shell. `SidebarView` reads `Screen.sidebarVisible` and is rendered by `macOSRoot` and `FluxiPadRoot` only.

### Settings affordance on iPad regular

Settings stays as a sheet (Decision 2). The per-screen `onSettingsTap` callback that opens it is currently wired through `FluxScreenHeader`'s gear pill (inside `FluxiOSRoot`). On iPad regular, `FluxScreenHeader` is hidden — so the gear pill must move.

Two equally good options, picked: a `.toolbar` `ToolbarItem(placement: .primaryAction)` on the detail column's `NavigationStack` showing the gear, opening the same Settings sheet. This places the gear in the standard iPad toolbar slot rather than inside the screen's content area. The sheet implementation (`NavigationStack { SettingsView() }`) is identical to the existing iPhone sheet.

## Components and Interfaces

### New types

| Type | File | Purpose |
|---|---|---|
| `FluxiPadRoot` | `Flux/Flux/Navigation/FluxiPadRoot.swift` | iPad regular-size-class shell: `NavigationSplitView` + sidebar + detail. |
| `AdaptiveColumnsLayout` | `Flux/Flux/Helpers/AdaptiveColumnsLayout.swift` | Reusable 1/2/3-column reflow with Dynamic Type collapse. |

### Modified types

| Type | File | Change |
|---|---|---|
| `AppNavigationView` | `Flux/Flux/Navigation/AppNavigationView.swift` | Hoist Dashboard / History / Today Day Detail view-models. Add `today: String` state + 60s recompute task. Add `usesPadShell` size-class+idiom check inside `iOSRoot`. Add `selectedScreen ↔ iosTab` two-way sync. Rebuild hoisted VMs in `reloadDependencies()` when the API client identity changes. |
| `Screen` | `Flux/Flux/Navigation/Screen.swift` | Add `init(tab: FluxTab)`. Widen `sidebarVisible` to include `.today` on iOS. |
| `DayDetailViewModel` | `Flux/Flux/DayDetail/DayDetailViewModel.swift` | Add `setDate(_:) async` that swaps the date, clears per-day fields, and reloads — used only by the Today rollover path. |
| `DashboardView` | `Flux/Flux/Dashboard/DashboardView.swift` | Add `dashboardContentRegular` body branch using `AdaptiveColumnsLayout`. Accept an injected view-model (already supports it). |
| `HistoryView` | `Flux/Flux/History/HistoryView.swift` | Add regular-size-class card grid via `AdaptiveColumnsLayout`. Accept an injected view-model (already supports it). |
| `DayDetailView` | `Flux/Flux/DayDetail/DayDetailView.swift` | Add `dayDetailContentRegular` body branch. |
| `SettingsView` | `Flux/Flux/Settings/SettingsView.swift` | Wrap form content in a 640pt width cap at regular size class (only if the iPad default `.formSheet` width does not already constrain it — measured during implementation). |
| `FluxiOSRoot` | `Flux/Flux/Navigation/FluxiOSRoot.swift` | Accept injected Dashboard / History view-models (rather than constructing internally) so iPad-shell-owned VMs survive the swap. |

### Behavioural contracts not obvious from signatures

- `AppNavigationView.usesPadShell` SHALL gate the iPad shell on `userInterfaceIdiom == .pad && hSizeClass == .regular` so iPhone Plus/Max landscape (which reports regular) keeps the iPhone shell.
- `AppNavigationView.today` SHALL be recomputed only via the 60s task and `scenePhase → .active` transition; SHALL NOT be recomputed on every render.
- `DayDetailViewModel.setDate(_:)` SHALL cancel any in-flight `comparisonTask`, clear per-day fields (`readings`, `parsedReadings`, `summary`, `peakPeriods`, `dailyUsage`, `note`, `offpeakStats`, `comparisonState`), and then call `loadDay()`. It SHALL NOT touch the apiClient or nowProvider.
- The Today `DayDetailView` SHALL be constructed with the hoisted view-model via the existing `init(viewModel:)` so the VM identity survives midnight rollover.
- Sidebar ↔ tab sync `onChange` handlers SHALL guard against feedback loops by comparing values before writing.
- `reloadDependencies()` SHALL rebuild the three hoisted VMs only when the API client identity changes (`AnyObject` reference inequality); otherwise leave them in place.
- `AdaptiveColumnsLayout.columnCount(width:)` SHALL drop one column when `dynamicTypeSize >= .accessibility4`; SHALL NOT collapse at AX1–AX3 (those sizes are required to read in the multi-column layout per AC 8.1).

## Data Models

No changes.

## Error Handling

No new failure modes. The unconfigured-app branch (`apiClient == nil`) continues to short-circuit to `SettingsView` before any shell selection.

## Testing Strategy

### Unit tests (Swift Testing)

`Flux/FluxTests/iPadAdaptiveLayoutTests.swift`:

- `Screen(tab:)` round-trips: every `FluxTab` produces a `Screen` whose `.tab` returns the original case.
- `Screen.sidebarVisible` includes Dashboard / Today / History but not Settings on both iOS and macOS (compile-time `#if` covers the macOS-only difference, if any remains).
- `AdaptiveColumnsLayout.columnCount(width:typeSize:)` produces:
  - 1 at width 699, 2 at 700, 2 at 999, 3 at 1000 (boundary checks).
  - One fewer column when `typeSize >= .accessibility4` (boundary check at `.accessibility3` vs `.accessibility4`).
  - Never less than 1.
- `DayDetailViewModel.setDate(_:)`:
  - Clears `readings`, `parsedReadings`, `summary`, `peakPeriods`, `dailyUsage`, `note`, `offpeakStats`, `comparisonState`.
  - Calls `loadDay()` exactly once (via a mock API client that counts calls).
  - Is a no-op when called with the current date (no client call, no field reset).
- Sidebar ↔ tab sync: extract the bidirectional mapping into a pure function `func syncedState(selected: Screen?, tab: FluxTab) -> (Screen?, FluxTab)` and test that:
  - Identity inputs return identity outputs (no flip-flop).
  - Mismatched input converges in one step.
  - All `FluxTab ↔ Screen` pairs round-trip.

### Snapshot / preview verification

- SwiftUI `#Preview` for `FluxiPadRoot` at three widths (430, 770, 1080) with a `MockFluxAPIClient.preview` API client. These previews are checked into the spec implementation as the human verification artifact (no snapshot test framework added).
- `DashboardView`, `HistoryView`, `DayDetailView` get a `#Preview` variant that pins width via `.frame(width:)` to validate the regular-size-class branches.

### Manual smoke check (AC 7.4)

Recorded in `specs/ipad-adaptive-layout/implementation.md` after the work lands:

- iPhone simulator: `FluxTabBar` visible; Dashboard → History → Today switches via tab bar; Settings sheet from each screen; History → tap day → Day Detail push.
- macOS: sidebar shows Dashboard / Today / History (no Settings); ⌘, opens Settings scene; ⌘R refresh; ← / → on Day Detail.
- iPad simulator coverage per AC 9.1.

### Property-based testing

Not appropriate for this feature. Layout decisions are stepwise (column thresholds) and the column-count function has trivially enumerable input space — example-based tests cover it.

## Risks

| Risk | Mitigation |
|---|---|
| **VM reconstruction on shell swap** silently breaks AC 6.4 | Hoist VMs into `AppNavigationView` and pass instances down. Verified by code inspection during review: both `FluxiOSRoot` and `FluxiPadRoot` accept VMs via init (no internal `@State viewModel = ...`). |
| **`navigationDestination(for: HistoryRoute.self)` placement** | `HistoryView` declares the destination inside its body. SwiftUI's lookup walks up to the nearest enclosing `NavigationStack`, which is the detail-column stack on iPad regular and the per-tab stack on iPad compact / iPhone. Verified manually on iPad simulator during implementation; if iPad regular fails to push, lift the destination declaration to `FluxiPadRoot`'s detail-column `NavigationStack`. |
| **Sidebar selection ↔ tab feedback loop** | Guarded by value comparison in `onChange`. Tested via a pure reducer extracted from the sync (input: pair of values; output: writes — no `onChange` counting). |
| **API-client replacement leaves hoisted VMs with stale client** | `reloadDependencies()` rebuilds the three hoisted VMs when client identity changes (see [State ownership → API-client replacement](#api-client-replacement)). |
| **iPhone Plus/Max landscape (`.regular` horizontal)** silently routes to `FluxiPadRoot` | `usesPadShell` requires `userInterfaceIdiom == .pad`. |
| **`@State` view-models lost on shell swap** | The shell branch is a single `@ViewBuilder` body in `iOSRoot` that swaps siblings — SwiftUI may destroy the unmounted subtree's `@State`. Since the VMs we care about live in `AppNavigationView` (their owner stays mounted), this is fine. Pushed Day Detail VMs remain transient and are expected to be discarded on shell swap (AC 6.3 floor). |
| **iPad Dynamic Type AX4+ in 2-col Dashboard** clips the battery hero | `AdaptiveColumnsLayout` drops one column at AX4+; verified visually in `#Preview` at `.dynamicTypeSize(.accessibility4)`. |
| **Slide Over width** drops sidebar but keeps stale `selectedScreen` | Size-class flips to compact → `iOSRoot` renders `FluxiOSRoot`, which reads `iosTab`. The bidirectional sync ensures `iosTab` matches the last-active `selectedScreen` (and runs on initial value, not only on change). |
| **Width breakpoints (700 / 1000) not measured** | Marked provisional in the Breakpoints section. The implementation step opens each screen in iPad mini / Air / Pro 13" simulators, records detail-column widths, and adjusts thresholds if measured values fall on the wrong side of the boundary. Recorded in `implementation.md`. |

## Migration plan

1. Add `AdaptiveColumnsLayout` helper + unit tests (column count, AX4 collapse).
2. Add `Screen(tab:)`; widen `Screen.sidebarVisible` to include `.today` on iOS. Update `SidebarView` callers if needed (it reads `Screen.sidebarVisible` already).
3. Add `DayDetailViewModel.setDate(_:)` + unit tests covering per-day field reset and `loadDay()` invocation.
4. Add `today: @State` to `AppNavigationView` with the 60s recompute task and scene-phase recompute. No consumers yet; verify the task starts and stops with the view lifecycle via a probe `#Preview` or instrumentation.
5. Hoist Dashboard / History / Today-`DayDetail` view-models into `AppNavigationView`. Wire the rollover task to call `todayDayDetailViewModel.setDate(today)` on change. Update `FluxiOSRoot` to accept injected VMs. Verify iPhone still works (smoke check item: tab switches preserve state).
6. Add API-client-identity rebuild path in `reloadDependencies()`. Test by changing the API URL in Settings and confirming a refresh on each screen.
7. Add `FluxiPadRoot` with `NavigationSplitView(.balanced)` + sidebar + detail column. Wire toolbar gear → existing settings sheet. Render existing single-column screens initially (no adaptive layout yet — just prove the shell works).
8. Add `usesPadShell` size-class+idiom check in `AppNavigationView.iOSRoot`. iPhone simulators (all sizes) must keep `FluxiOSRoot`; iPad simulators at regular must show `FluxiPadRoot`.
9. Add `selectedScreen ↔ iosTab` sync in `AppNavigationView`. Tested via the pure-reducer unit test from the Risks table.
10. Measure detail-column widths on iPad mini / Air / Pro 13" simulators in portrait + landscape and at ½ / ⅓ Split View. Record values in `implementation.md`; adjust thresholds in `AdaptiveColumnsLayout` if measurements disagree with the 700 / 1000 hypothesis.
11. Add Dashboard regular-size-class layout.
12. Add History regular-size-class layout.
13. Add Day Detail regular-size-class layout.
14. Verify whether the iPad default sheet width already constrains `SettingsView`; if not, add the 640pt width cap.
15. Run the full smoke check (AC 7.4) + iPad device coverage (AC 9.1–9.3). Record results in `implementation.md`.

Each step compiles and ships an intermediate but coherent state; steps 1–6 land without any visible change.
