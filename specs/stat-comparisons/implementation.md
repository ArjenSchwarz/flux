# Implementation: Stat Comparisons (T-1161)

## 1. Beginner

The Day Detail screen shows one day's energy numbers (solar, house usage, grid in/out, plus a per-block breakdown). Comparing to yesterday or last week meant flipping back and forth.

This feature adds a **Compare** toggle near the top of Day Detail, between the day-navigation header and the panels. Off by default. Flip it on and:

- A period picker appears next to the toggle ("Yesterday" or "7 days ago").
- Each row in the **Power** card and each value in **The day in five blocks** grows a grey sub-line beneath its number, showing the difference: `▲ 1.2 kWh`, `▼ 0.4 kWh`, or `— kWh`.
- If the comparison day has no data, every sub-line stays as a blank placeholder of the same height, and a grey caption appears under the picker: *"No comparison data available for 7 days ago"*.
- Toggling off restores the original layout exactly.

The toggle and chosen period are remembered between launches, per device. Battery and Peak Usage cards, the charts, and other screens are intentionally untouched.

## 2. Intermediate

### Data flow

`DayDetailView` owns two `@AppStorage` properties — `compareEnabled: Bool` and `comparePeriodRaw: String` — keyed against `UserDefaults.fluxAppGroup` (`DayDetailView.swift:12-23`). Three `.onChange` handlers (`DayDetailView.swift:104-112`) call `viewModel.updateCompare(enabled:period:)` whenever the toggle, period, or `viewModel.date` changes.

`updateCompare` (`DayDetailViewModel.swift:100-128`) cancels any in-flight comparison task, computes the resolved compare date through `resolveCompareDate` (Sydney calendar, ±1 or ±7 days), then either short-circuits to `.unavailable` or spawns a `Task` that calls `apiClient.fetchDay(date:)`. The result becomes a `ComparisonState` published on the view model.

The view body forwards `viewModel.comparisonState` as a `compare:` parameter to both `DayInFiveBlocksPanel` and the Day Detail `SummaryBlock` (`DayDetailView.swift:67-85`). Both panels dispatch each row's slot through `ComparisonState.subline(current:comparison:)` (`ComparisonState.swift:22-31`) — the single source of truth for `.off → .hidden`, `.loading|.unavailable → .reserved`, `.ready → DeltaFormatter`.

### Key types

- `ComparePeriod` (`ComparePeriod.swift`) — two-case enum with `dayOffset` and `displayName`. `parseOrDefault` falls back to `.yesterday` for unknown raw values.
- `ComparisonSnapshot` (`ComparisonSnapshot.swift`) — value type holding the energy fields. Derives `houseUsed` via `HouseholdLoad.kwh` and `peakGridImport` as `max(0, gridImport - offpeakGridImport)`. `from(date:response:)` returns nil only when both `summary` and `dailyUsage` are absent.
- `ComparisonState` — `.off | .loading(date:) | .ready(snapshot, period:) | .unavailable(period:)`. `isUnavailable` drives the failure caption.
- `SublineContent` — `.hidden | .reserved | .text(String)` (Decision 18 — type-safe layout-stability contract).
- `DeltaFormatter` — pure helpers producing the visible string and VoiceOver clauses.
- `ValueSubline` — single-switch view mapping `SublineContent` to `EmptyView` or styled `Text`.
- `CompareControl` — toggle + period chip + caption host.

### State lifecycle

The view model holds a `comparisonTask: Task<Void, Never>?` (`DayDetailViewModel.swift:39`). Every `updateCompare` cancels the previous task before spawning a new one (`:101`). On view teardown, `deinit` cancels the running task (`:51-57`).

### Layout stability

Every row reserves the sub-line slot's height while `compare != .off`. `ValueSubline` renders one of three things: `.hidden → EmptyView()` (zero frame), `.reserved → Text("\u{00A0}")`, `.text("…") → the formatted string`. `.reserved` and `.text` are styled identically (`touTime`/`tertiaryText`), so they measure to the same height. Wrapping each value column in `VStack(alignment: .trailing, spacing: 2)` (`FluxV5Components.swift:65-83`, `DayInFiveBlocksPanel.swift:69-87`) puts the sub-line directly under its value.

### VoiceOver

Each `Text` inside a `ValueSubline` is `.accessibilityHidden(true)` — the chevron glyph never reaches VoiceOver. The row instead applies `.accessibilityElement(children: .ignore)` plus a composed label via `RowAccessibilityModifier` (`FluxV5Components.swift:99-111`) when an override is provided. `DeltaFormatter.voiceOverLabel` produces *"Solar produced: 14.8 kilowatt-hours, up 1.2 kilowatt-hours versus yesterday"* (`DeltaFormatter.swift:22-32`); the fallback variant omits the comparison clause. With `compare == .off` the override is nil and the row keeps its pre-feature per-`Text` behaviour.

## 3. Expert

**Cancellation in `updateCompare`.** Cancellation in Swift is cooperative — an awaited request whose bytes are already on the wire resumes even after `cancel()`. The spawned closure has three `Task.isCancelled` guards: after `try await fetchDay` (`:115`), inside the `catch` (`:122`), and immediately before the final state mutation (`:125`). Without these guards, a slow Task A whose cancellation completes mid-flight could overwrite Task B's `.ready` after B published. The guards ensure the *late* task wins regardless of arrival order.

**Off-state byte-identity.** When `compare == .off`, `valueSub` defaults to `.hidden` and `accessibilityOverride` to nil at every `FluxStatRow` callsite (`FluxV5Components.swift:60-61`). `ValueSubline(.hidden)` returns `EmptyView()` (`ValueSubline.swift:11-12`), contributing no frame to the surrounding `VStack(spacing: 2)`. `RowAccessibilityModifier(label: nil)` is pure passthrough (`FluxV5Components.swift:107-109`). The Decision 16 byte-identity claim therefore applies to `SummaryBlock` and `DayInFiveBlocksPanel` *contents only* — `CompareControl` is hosted unconditionally at `DayDetailView.swift:61-65`.

**The NBSP trick for height reservation.** `ValueSubline(.reserved)` renders `Text("\u{00A0}")` (`ValueSubline.swift:16`) — a U+00A0 non-breaking space styled identically to the rendered text. SwiftUI measures the line at the font's natural cap height regardless of glyph content. An empty `Text("")` would collapse vertically; a regular space might be elided. NBSP is the smallest invisible character the text engine treats as a real glyph, so `.reserved` and `.text` slots match height exactly.

**Per-row fallback semantics.** `DeltaFormatter.sublineContent` returns `.reserved` whenever either side is nil (`DeltaFormatter.swift:11-15`). `ComparisonState.subline` (`ComparisonState.swift:22-31`) is the single dispatch — `SummaryBlockCompareMapping.valueSub` and the panel's `solarValueSub`/`totalValueSub` (`SummaryBlock.swift:192-194`, `DayInFiveBlocksPanel.swift:160-166`) are one-line forwarders. The card never decides on fallback; the row does, consistently.

**Partial-availability.** `ComparisonSnapshot.from` returns non-nil whenever the response has *either* a `summary` or a `dailyUsage` (`ComparisonSnapshot.swift:43-46`). A snapshot with `dailyUsage == nil` produces a `.ready` state. Inside `DayInFiveBlocksPanel`, `comparisonBlock(for:compare:)` returns `nil` when `snapshot.dailyUsage == nil` (`DayInFiveBlocksPanel.swift:168-171`), so each five-block sub-line independently resolves to `.reserved`. The SummaryBlock continues to render its deltas because it reads top-level snapshot fields. So a "summary present, dailyUsage nil" response yields visible deltas on the Power card and reserved-but-empty slots on every Five-Block row simultaneously.

**`compareEnabled .onChange(initial: true)` re-fire.** Without `initial: true` (`DayDetailView.swift:104`), launching with `compareEnabled` already true (persisted from a previous run) would render with `comparisonState == .off` and never fetch — `compareEnabled` doesn't *change*, so the handler doesn't fire. `initial: true` calls the handler once on first appearance, kicking off `updateCompare(enabled: true, …)` and resolving to `.loading` → `.ready`/`.unavailable` exactly as if the user had just toggled it on. The other two `.onChange` handlers (period, date) need no `initial` flag.

## Completeness Assessment

### Fully implemented

- **AC 1.1** — `CompareControl` is hosted directly above the `if let dailyUsage` block (`DayDetailView.swift:61-67`), beneath header, navigation, and note section.
- **AC 1.2** — `compareEnabled` defaults to `false` (`DayDetailView.swift:13`).
- **AC 1.3** — `compare == .off` produces `.hidden` slots and nil overrides; chip/caption gated on `enabled` (`CompareControl.swift:22-32`).
- **AC 1.4** — `compare != .off` reserves slots (`ComparisonState.swift:26-27`); chip rendered when `enabled` (`CompareControl.swift:22-24`).
- **AC 1.5** — `@AppStorage` persists toggle (`DayDetailView.swift:12-13`).
- **AC 2.1, 2.2** — `ComparePeriod.allCases` is `[.yesterday, .sevenDaysAgo]`; `parseOrDefault` defaults to `.yesterday` (`ComparePeriod.swift`).
- **AC 2.3** — `comparePeriodRaw` persists independently of `compareEnabled` (`DayDetailView.swift:15-16`).
- **AC 2.4** — `.onChange(of: comparePeriodRaw)` calls `updateCompare` (`DayDetailView.swift:107-109`).
- **AC 2.5** — Period chip is inside `if enabled` (`CompareControl.swift:22-24`).
- **AC 3.1, 3.3, 4.1, 4.3** — Every `SR` row passes `valueSub` through `compareRow` (`SummaryBlock.swift:41-108`); every `FB` value column wraps in a `VStack` containing a `ValueSubline` (`DayInFiveBlocksPanel.swift:69-87`).
- **AC 3.2** — `ComparisonSnapshot.houseUsed` reuses `HouseholdLoad.kwh` (`ComparisonSnapshot.swift:18-26`).
- **AC 4.2** — `comparisonBlock(for:)` returns `nil` for missing kinds (`DayInFiveBlocksPanel.swift:168-171`); both columns fall back.
- **AC 5.1, 5.2, 5.3** — `resolveCompareDate` uses `sydneyCalendar.date(byAdding: .day, value: period.dayOffset)` (`DayDetailViewModel.swift:130-138`); `.onChange(of: viewModel.date)` re-fires after navigation (`DayDetailView.swift:110-112`).
- **AC 5.4** — No same-time-of-day cutoff exists; today is fetched and compared full-vs-full.
- **AC 5.5** — Failure caption renders inside `CompareControl` directly under the chip with `tertiaryText`/`touTime` (`CompareControl.swift:27-32`).
- **AC 5.6** — `.loading` maps to `.reserved` (`ComparisonState.swift:26-27`).
- **AC 6.1** — No `#if os(iOS)` guards on the Compare path; hosted in shared `DayDetailView`.
- **AC 6.2** — `@AppStorage` writes to `UserDefaults.fluxAppGroup`; no iCloud KVS or Keychain wiring.
- **AC 7.1, 7.2** — `DeltaFormatter.voiceOverLabel`/`voiceOverFallbackLabel` (`DeltaFormatter.swift:22-44`); applied via `RowAccessibilityModifier`. Primary value is spoken via `EnergyFormatting.formatSpoken(_:)` ("14.80 kilowatt-hours") so VoiceOver doesn't read "kWh" as the letters k-W-h.
- **AC 7.3** — Native `Toggle` and `Picker` with `.accessibilityLabel("Compare period, …")` (`CompareControl.swift:42-43`).
- **AC 7.4** — Caption has explicit `.accessibilityLabel` matching its visible string (`CompareControl.swift:31`).

### Partially implemented

- **AC 5.6 height stability** — Structurally guaranteed because `.loading`, `.reserved`, and `.text` all render through `ValueSubline` with identical font/colour, and `.reserved` uses NBSP to pin the line height. There's no card-level test pinning `loading.height == ready.height` byte-for-byte; `ValueSublineTests` measures the primitive but not the composed assertion.

### Missing or deferred

- **Dynamic Type stacking, RTL chevron mirroring, iPad/macOS column variants** — Deferred per Decision 14 and §Non-Goals. No tests, no explicit handling.
- **Localization** — Strings ("Compare", "Yesterday", "7 days ago", caption) hard-coded English per §Non-Goals.
- **iCloud sync of preferences** — Excluded (AC 6.2); writes go to `UserDefaults.fluxAppGroup` only.
- **Comparison on Battery, Peak Usage, charts, Dashboard, History, Settings** — Out of scope per Decisions 3, 12 and §Non-Goals.
- **Today same-time-of-day cutoff / partial-day caption** — Decisions 4 and 11.
- **Comparison response caching across navigation** — Decision 17 alternative; not implemented. Each day-nav reissues a fresh `fetchDay`.
