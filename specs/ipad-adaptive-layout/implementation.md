# Implementation Notes: iPad Adaptive Layout

This document records implementation-time decisions and measurements that
inform the final shipped behaviour.

## Detail-column width measurements (Task 16)

`AdaptiveColumnsLayout` breakpoints currently use the design's hypothesised
values:

| Width tier | Column count |
|---|---|
| `< 700` | 1 |
| `700 ≤ w < 1000` | 2 |
| `≥ 1000` | 3 |

Measurement of actual detail-column widths in `FluxiPadRoot` is **deferred
to the verification phase** (Task 21). The probe recipe below should be
applied if any of the simulator runs in Task 21 produce a layout that
disagrees with these tiers.

### Probe recipe (for use during Task 21 if needed)

Temporarily add inside `FluxiPadRoot`'s `detailContent` body:

```swift
.overlay(alignment: .topLeading) {
    GeometryReader { proxy in
        Color.clear.onAppear {
            print("iPad detail column width: \(proxy.size.width)")
        }
    }
}
```

Run on the simulator matrix in Task 21 and record measured widths in this
file before the probe is removed. Adjust constants in
`Flux/Flux/Helpers/AdaptiveColumnsLayout.swift` if measured widths fall on
the wrong side of 700 / 1000, and re-run `AdaptiveColumnsLayoutTests` with
the updated boundary values.

### Hypothesised widths (from design)

| Device / orientation | Approx detail width | Expected tier |
|---|---|---|
| iPad mini portrait full-screen | ~430–500pt | 1-col |
| iPad mini landscape full-screen | ~770–820pt | 2-col |
| iPad Pro 13" portrait full-screen | ~760–1024pt | 2 or 3-col |
| iPad Pro 13" landscape full-screen | ~1080pt | 3-col |
| iPad ½ Split View (any model) | reports `.regular` but narrower | falls into 1-col tier below 700 |
| iPad Slide Over | reports `.compact` | iPhone shell fallback |

## Explanation at three levels

### Beginner level

#### What changed
Before this branch, opening Flux on iPad showed the iPhone layout stretched
across the whole screen — a tall, narrow column of content with a tab bar at
the bottom, the same on a 13-inch iPad Pro as on an iPhone. iPad users had a
lot of empty whitespace and could only see one card at a time.

After this branch, on iPad with enough screen width Flux now shows a sidebar
on the left (Dashboard / Today / History) and the selected screen on the
right, and the screens themselves rearrange their cards into two or three
columns so the wider screen actually gets used. The iPhone layout is
completely unchanged. When the iPad is split-screen with another app and
gets very narrow, Flux falls back to the iPhone layout automatically.

#### Why it matters
The app was already built for iPad — you could install it from the App
Store — but it didn't *feel* like an iPad app. Users with a battery system
and an iPad would naturally want to glance at the dashboard or compare
yesterday and today on a bigger screen, and the cramped iPhone-on-iPad
layout discouraged that. The change brings Flux in line with how Apple's
own apps (Mail, Notes, Settings) lay themselves out on iPad.

#### Key concepts
- **Size class**: Apple's name for "is the window roughly phone-shaped or
  tablet-shaped right now". Doesn't mean "is this device a phone or a
  tablet" — a Slide Over window on iPad is "phone-shaped" even though the
  device is a tablet, and a iPhone Plus in landscape is "tablet-shaped"
  even though the device is a phone. Flux uses the size class together
  with the actual device type to decide which layout to show.
- **Sidebar shell**: Apple's standard pattern for tablet apps — a list of
  destinations on the left, the chosen destination's content on the right.
- **Adaptive layout**: The same screen shows different layouts depending
  on how much space is available, instead of one fixed design.
- **Today entry**: A sidebar shortcut to today's day detail. When midnight
  passes while the iPad is sitting on this entry, the date updates
  automatically — you don't have to tap away and tap back.

### Intermediate level

#### Changes overview
- New `FluxiPadRoot.swift`: a `NavigationSplitView(.balanced)` with the
  sidebar bound to `selectedScreen: Screen?` and the detail column carrying
  a `NavigationStack(path: $navigationPath)`. Settings is opened from a
  `.primaryAction` toolbar gear, not from the sidebar.
- `AppNavigationView.iOSRoot` gates between `FluxiPadRoot` and the existing
  `FluxiOSRoot` via `IPadLayoutGate.isActive(hSizeClass:)`. That helper
  combines `UIDevice.current.userInterfaceIdiom == .pad` with
  `hSizeClass == .regular` — the idiom check is load-bearing because iPhone
  Plus/Max landscape reports `.regular` horizontal but must keep the
  tab-bar shell (AC 7.1).
- `AppNavigationView` now owns the `DashboardViewModel`, `HistoryViewModel`,
  and a `todayDayDetailViewModel` as `@State` instead of letting the inner
  shells build them. This means cached fetch state and refresh timers
  survive a size-class flip from iPad sidebar shell to iPhone-fallback
  shell and back (AC 6.4).
- A new `DayDetailViewModel.setDate(_:)` swaps the date in place and clears
  per-day fields rather than rebuilding the view via `.id(today)` — chart
  highlight, scroll position, and note-editor draft are preserved across
  midnight rollover. A 60-second `.task` loop and a `scenePhase → .active`
  recompute both call this when the local date changes.
- Per-screen adaptive layout via `AdaptiveColumnsLayout`. It uses a
  `LazyVGrid` whose column count comes from `columnCount(width:typeSize:)`:
  1 column below 700pt, 2 below 1000pt, 3 above; one column collapses at
  Dynamic Type `>= .accessibility4` per AC 8.2. Width is measured via
  `onGeometryChange`; the initial seed is the 2-column tier so iPad screens
  don't briefly render as a single column on first appearance.
- Two pure helpers (`mappedIosTab(for:currentTab:)` and
  `mappedSelectedScreen(for:currentSelection:)`) drive the bidirectional
  sidebar ↔ tab sync via two `onChange` handlers in `AppNavigationView`.
  `.settings` selection is preserved in the tab → screen direction so a
  tab change doesn't knock the user out of Settings.

#### Implementation approach
- **View-model hoisting**: The state-ownership pattern moves ownership up
  one level so the VM's `@State` identity outlives the swap between
  `FluxiPadRoot` and `FluxiOSRoot`. Both shells accept VMs via init.
- **Credential fingerprinting**: `reloadDependencies()` rebuilds the three
  hoisted VMs only when the URL+token fingerprint actually changes, not
  on every foreground. Without this, `makeAPIClient()` returns a fresh
  `URLSessionAPIClient` every call and reference-identity comparison
  would always rebuild — defeating the hoisting work.
- **Two-handler sidebar↔tab sync**: Each `onChange` handler runs a pure
  helper that maps from the trigger side to the other side, with a
  before-write comparison to break feedback loops. The pure helpers are
  unit-tested in isolation; the production handlers are thin wrappers
  around them.
- **`AdaptiveColumnsLayout` as the single reflow primitive**: Used in
  three places (Dashboard hero+trio row, Dashboard battery row, History
  card grid). Day Detail uses a hand-built two-column `Grid` because its
  left/right columns are heterogeneous (summary panels vs charts).

#### Trade-offs
- **Sidebar over keeping the tab bar**: Matches macOS, gives a cleaner
  look on iPad Pro, but means two navigation shells must stay in sync
  during size-class transitions. Decision 2.
- **`.balanced` over `.prominentDetail`**: Sidebar can coexist with detail
  on iPad Pro and auto-hides on iPad mini portrait — one style covers the
  whole iPad lineup without per-device branches. Decision 5.
- **`setDate(_:)` over `.id(today)` rebuild**: Preserves transient UI
  state (chart highlight, scroll, note draft) at the cost of an extra
  mutating method on the VM. Decision 5.
- **Manual smoke check deferred**: Task 21's iPad simulator matrix is
  recorded as `_to verify_` rather than executed. The automated build,
  lint, and test passes cover the code correctness; the device-coverage
  verification is the final gate.

### Expert level

#### Technical deep dive

The branch lands a sidebar-shell pattern on iPad while keeping the iPhone
V5 tab-bar shell intact and macOS unchanged. The interesting work is in
the seams.

The gate at the top of `AppNavigationView.iOSRoot` selects between
`FluxiPadRoot` and `FluxiOSRoot` based on
`IPadLayoutGate.isActive(hSizeClass:)`. This check fires on every
horizontal-size-class flip, so the view tree literally tears down one
shell and stands up the other — a `@State`-owned VM inside the unmounted
shell would be lost. The fix is to lift the three "always-live" VMs
(Dashboard, History, Today-DayDetail) into `AppNavigationView`, which
stays mounted across the flip. `FluxiPadRoot` and `FluxiOSRoot` both
accept these VMs via `init`, so they get the same instances regardless of
which shell is currently rendering.

The Today entry has a midnight-rollover problem unique to a sidebar
shell: on iPhone the Today tab rebuilds on every selection, so the date
is recomputed naturally; on iPad the user may sit on Today across
midnight. `DayDetailViewModel.setDate(_:)` swaps the date in place, clears
the per-day fields (`readings`, `parsedReadings`, `summary`, `peakPeriods`,
`dailyUsage`, `note`, `offpeakStats`, `comparisonState`), cancels any
in-flight `comparisonTask`, and reloads. The VM's transient view-level
state (note editor sheet, chart highlight, scroll position) is preserved
because the VM identity survives the swap. The rollover is driven by a
60-second `Task.sleep` loop plus a `scenePhase → .active` recompute — the
latter catches the case where the user backgrounds the app overnight and
returns the next morning.

`reloadDependencies()` had a subtle bug pre-review. The original guard was
`(apiClient as AnyObject?) !== (client as AnyObject?)`, which is always
true after the first call because `makeAPIClient()` constructs a fresh
`URLSessionAPIClient` every invocation — the comparison is between two
distinct class instances. This meant every `scenePhase → .active` rebuilt
all three hoisted VMs, discarding cached fetch state and resetting
in-flight refresh timers on every foreground, in direct violation of
AC 6.4. The fix is to fingerprint the credentials (`apiURL|token` joined
into a single string) and only rebuild when that string changes. This is
robust to value-vs-reference differences in the client implementation —
two clients with the same URL and token are treated as equivalent
regardless of identity.

The bidirectional sidebar ↔ tab sync is hard to model with a single pure
reducer because the canonical pair depends on which side just changed.
The branch ships two trigger-aware helpers — `mappedIosTab(for:currentTab:)`
and `mappedSelectedScreen(for:currentSelection:)` — and the production
`onChange` handlers each call one of them. Both helpers preserve
`.settings` (which has no `FluxTab` counterpart), and both handlers
guard against feedback loops by comparing the computed value to the
current value before writing.

`AdaptiveColumnsLayout` is the only multi-column primitive on the branch.
It uses `LazyVGrid` rather than the Swift `Layout` protocol because
`LazyVGrid` interoperates cleanly with the existing `ScrollView`-based
screen layouts; a `Layout`-based implementation would do single-pass
measurement+placement but would require translating every callsite's
sizing expectations. The trade-off is the one-frame mismatch on first
appearance: `measuredWidth` starts at the seeded value (700pt = 2-column
tier) and reflows when `onGeometryChange` delivers the real width. With
the original `0` seed, every iPad screen flashed as a 1-column layout
for one frame before snapping to 2 or 3 columns. The 700 seed means the
worst case is a brief 2-col → 3-col reflow on iPad Pro 13" landscape,
which is much less obtrusive.

The Dashboard / History compact paths use small per-panel `@ViewBuilder`
properties so both compact and regular content branches reuse the same
data path. Day Detail does the same but its regular layout also renders
two cards (`DailyUsageCard`, `PeakUsageCard`) that the compact layout
doesn't — the data was already in the view-model (`viewModel.dailyUsage`,
`viewModel.peakPeriods`) but never surfaced in a dedicated card. The
regular layout splits the previously-combined battery+SOC chart into two
panels because the chart column has enough vertical room to render them
separately.

#### Architecture impact

The branch establishes a pattern that the next platform (Vision Pro,
larger Stage Manager configurations, future iPad form factors) can adopt
without restructuring: a single gate (`IPadLayoutGate.isActive`) chooses
between shells, and per-screen content branches off `horizontalSizeClass`.
Adding a new size-class layout means adding one branch in each of three
files (Dashboard, History, Day Detail), not rewriting any of the
view-models or shells.

VM hoisting into `AppNavigationView` makes the root view the single
owner of "global session state" — credentials, today's date,
view-models. This is heavier than the previous model where shells owned
their own view-models, but it's the only way to make state survive a
shell teardown. Future expansion (e.g. an iPad-only fourth screen) would
add a new VM hoist alongside the existing three.

The `syncedState` refactor into two trigger-aware helpers narrows what
production code is doing to exactly what the unit tests cover —
previously the reducer was tested but production reimplemented the logic
inline. Test coverage and production behaviour are now the same.

#### Potential issues to monitor

- **Slide Over / narrow Split View round-trips**: The iPad shell mounts
  `FluxiPadRoot`, narrow widths mount `FluxiOSRoot`. A user
  alternating between configurations rapidly will see the shell flip
  repeatedly. AC 6.4 says VM identity must survive, AC 6.3 says
  in-flight Day Detail pushed from History may be dropped. Watch for
  navigation-stack inconsistencies that aren't reproducible from a fresh
  launch.
- **Multi-window scene state**: AC 6.5 says sidebar selection is
  scene-local (per window). This falls out of `@State` correctness — no
  global selection — but a future change that hoists selection to the
  app level would silently regress this AC.
- **Dynamic Type at AX5**: `AdaptiveColumnsLayout` collapses one column
  at `>= .accessibility4`, so AX4 and AX5 see at most 1-col at narrow
  widths and 2-col at wide widths. The cards themselves must remain
  non-clipping at AX5 (AC 8.1) — visual verification in the simulator
  at `.dynamicTypeSize(.accessibility5)` is part of Task 21 and is
  currently `_to verify_`.
- **iPad keychain protection class**: `keychainService.loadToken()` may
  return nil on a freshly-booted device that hasn't been unlocked. The
  `credentialFingerprint` will be nil, `reloadDependencies()` will set
  `apiClient = nil` and route to `SettingsView`. The user unlocks the
  device, `scenePhase` becomes `.active`, `reloadDependencies()` runs
  again, fingerprint is now non-nil, VMs are constructed. Worth a smoke
  test on a fresh iPad boot before merging.
- **Detail-column width measurements**: The 700/1000 breakpoints are
  hypothesised. Task 21's verification matrix has placeholders for actual
  measurements on iPad mini / Air / Pro 13" — any breakpoint disagreement
  will surface as a layout that's 1-col where 2-col was expected, or
  vice versa, and only manual verification will catch it.

## iPad smoke-test fixes (2026-05-23)

Issues found while driving the iPad simulator after the pre-push review
landed:

- **Sidebar could not be reopened after closing** — and **the toolbar
  settings gear was missing on Dashboard and Day Detail**. Root cause:
  `DashboardView` and `DayDetailView` called
  `.toolbar(.hidden, for: .navigationBar)` unconditionally, which hid
  both the system-provided sidebar toggle and `FluxiPadRoot`'s
  `.primaryAction` gear. `HistoryView` happened to have the right
  conditional (`tabBinding == nil ? .visible : .hidden`) which is why
  History was the only screen showing the gear. Fixed: gate the
  toolbar-hide on `usesRegularLayout` in all three views, and set a
  `navigationTitle` so the navbar has something to render.
- **`legacyHeader` eyebrow + page title still rendered on iPad**
  (&ldquo;NOW · 13:45 · MAY 23 / Battery&rdquo; on Dashboard,
  &ldquo;EEE · MAY 23 / Today&rdquo; on Day Detail). Now that the
  system navigation bar is visible on iPad regular and carries the
  screen title, the inline eyebrow block is redundant. Fixed: the
  iPad content branches (`dashboardContentRegular`,
  `dayDetailContentRegular`) no longer include the `headerSection` /
  `header` block. Compact path retains the legacy header for iPhone.
- **Battery Power and SOC chart panels were missing the
  tap-to-enlarge affordance** in the Day Detail regular layout. The
  zoom infrastructure (T-1215) is tied to `ChartKind` cases, which
  only model `.dayPower` and `.dayBatteryCombined` — there are no
  separate cases for `BatteryPowerChartView` or `SOCChartView`. Adding
  new `ChartKind` cases would mean threading them through
  `ExpandedDayHost`, `ExpandableChartContainer`, and the macOS scene
  router. Fixed by reverting to the same `DayDetailPanels.power` +
  `DayDetailPanels.battery` panels the compact layout uses; the iPad
  regular layout now has two stacked chart panels (Power +
  Battery+SOC combined) rather than three. Diverges from the design
  doc's three-panel diagram (design.md lines 309-322) but matches the
  compact UX and preserves the chart-zoom feature.
- **Enlarged-chart close button was covered by iPad Stage Manager
  window controls.** The `ExpandedChartTopBar` close button used
  `.padding(.leading, 8)`, which puts it directly under the window
  controls iPadOS overlays in the top-leading corner under Stage
  Manager. Fixed: bumped the leading padding to ~56pt on iPad
  (`UIDevice.current.userInterfaceIdiom == .pad`). iPhone keeps the
  tighter 8pt.

These fixes apply only to the iPad path; the iPhone V5 shell, the
macOS shell, and the existing compact behaviour are bit-identical to
the post-pre-push-review state.

## Pre-push review fixes (2026-05-22)

Applied after the four-agent pre-push review:

- Extracted the duplicated `userInterfaceIdiom == .pad && hSizeClass == .regular`
  predicate into a single `IPadLayoutGate.isActive(hSizeClass:)` helper in
  `Flux/Flux/Helpers/IPadLayoutGate.swift`. `AppNavigationView`,
  `DashboardView`, `HistoryView`, `DayDetailView`, and `SettingsView` now
  delegate to it — drift between the five copies could otherwise silently
  regress AC 7.1 (the iPhone Plus/Max landscape guard).
- Replaced the unused symmetric `syncedState(selected:tab:)` reducer with two
  trigger-aware helpers (`mappedIosTab(for:currentTab:)`,
  `mappedSelectedScreen(for:currentSelection:)`) that the live `onChange`
  handlers in `AppNavigationView` actually invoke. The previous code shipped
  a reducer that the unit tests covered but production bypassed; the new
  helpers are the load-bearing path and the test suite is now exercising
  what runs.
- Fixed an AC 6.4 regression in `reloadDependencies()`. The previous identity
  comparison (`(apiClient as AnyObject?) !== (client as AnyObject?)`) was
  always true because `makeAPIClient()` returns a fresh `URLSessionAPIClient`
  every call; the three hoisted view-models were therefore rebuilt on every
  `scenePhase → .active` foreground transition, discarding cached state and
  in-flight refresh timers. Replaced with a `credentialFingerprint`
  (`apiURL|token`) compare so a rebuild only happens when the URL or token
  actually changed.
- `AdaptiveColumnsLayout` now seeds `measuredWidth` at the 2-column tier
  instead of `0`, so the first frame doesn't briefly render every iPad screen
  as a single column before `onGeometryChange` delivers the real width. The
  unused `minCardWidth` parameter (never consumed by `columnCount(width:typeSize:)`)
  was removed.
- Dropped the unused `today: String` parameter from `FluxiPadRoot.init` —
  the hoisted Today `DayDetailViewModel` already carries the date.
- `DayDetailView.summaryColumn` now unwraps `viewModel.dailyUsage` once per
  render instead of twice (once for `DayInFiveBlocksPanel`, once for
  `DailyUsageCard`).
- `HistoryView`'s per-card `@ViewBuilder` properties became functions that
  accept `derived: HistoryViewModel.DerivedState` and `selectedDate: Date?`
  parameters. The values are now computed once at the top of
  `historyContent`/`historyContentRegular` instead of four times per render.
- Updated the `nonisolated(unsafe) comparisonTask` invariant comment in
  `DayDetailViewModel` to list `setDate` as an approved MainActor access
  point alongside `updateCompare`.
- Collapsed the four CHANGELOG entries (Added × 2, Changed × 2) for this
  feature into a single user-facing Added bullet — the "Changed" entries
  described internal scaffolding that ships together with the user-facing
  shell, and the sidebar-shell bullet had a leftover "adaptive layouts land
  in next phase" sentence that now contradicted reality.

## Task 21 verification log

### Automated tests (2026-05-22)

- `make ios-build` — succeeds.
- `make macos-build` — succeeds.
- `make ios-lint` — no violations in files touched by this spec; pre-existing
  violations remain in unrelated FluxCore and SoCAlerts files.
- `make ios-test` — passes for the four new test suites added by this spec
  (`AdaptiveColumnsLayoutTests`, `ScreenTests`, `SidebarTabSyncTests`,
  `DayDetailViewModelSetDateTests`). Two pre-existing tests are flaky when
  run in parallel mode but pass in isolation:
  - `DashboardViewModelTests.refreshSkipsWhenAlreadyLoading` — timing-based
    concurrency test, fails under load.
  - `CompareControlTests.*` — UIHostingController text-inspection tests,
    fail when the test host has stale state from prior tests in the same
    process. Confirmed passing in isolation (`-only-testing` flag).
- `make macos-test` — same observation: only `refreshSkipsWhenAlreadyLoading`
  is flaky, passes in isolation.

### Manual smoke check (deferred to human verification)

The interactive verification matrix below requires a developer to drive the
simulators (rotate, advance the clock, switch to Slide Over / Split View).
Build and code-level verification has been completed; recording these rows
is the final acceptance step before merging.

| Target | Expected | Result |
|---|---|---|
| iPhone 17 Pro | FluxTabBar visible all three tabs; Settings sheet reachable from each; History → Day Detail push | _to verify_ |
| macOS | Sidebar with Dashboard / Today / History; ⌘, opens Settings scene; ⌘R refresh; ← / → on Day Detail | _to verify_ |
| iPad mini portrait | FluxiPadRoot, sidebar visible, detail column shows Dashboard at single-col (width < 700) | _to verify_ |
| iPad mini landscape | FluxiPadRoot, sidebar visible, detail column shows 2-col Dashboard | _to verify_ |
| iPad Air landscape | FluxiPadRoot, 2-col Dashboard / History card grid / Day Detail two-column | _to verify_ |
| iPad Pro 13" portrait | FluxiPadRoot, 2 or 3-col Dashboard (depending on sidebar state) | _to verify_ |
| iPad Pro 13" landscape | FluxiPadRoot, 3-col Dashboard at detail width ≥ 1000pt | _to verify_ |
| iPad Pro 13" ½ Split View | FluxiPadRoot, detail column narrow → single-col fallback | _to verify_ |
| iPad Slide Over | FluxiOSRoot (tab-bar shell) — `usesPadShell` returns false at compact size class | _to verify_ |
| Today midnight rollover | Advance simulator clock past midnight on Today sidebar entry; date updates and reload fires within 60s | _to verify_ |
| Settings sheet on iPad | Form content capped to 640pt-wide column centred in sheet | _to verify_ |
| Dashboard 10s refresh on iPad | Live values update every 10s while Dashboard is selected | _to verify_ |
| History → Day Detail push on iPad regular | Day Detail pushes onto detail-column NavigationStack | _to verify_ |
