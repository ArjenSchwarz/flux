# Implementation: History Usage Stats

## Beginner

Open the History tab in Flux and pick a range (7 / 14 / 30 days). Above the four chart cards there's a new card called **Period overview**. It shows eight numbers in a grid: total usage, total solar collected, exported energy, peak imports, average night usage, the day with the most usage, the day with the most solar, and the lowest battery percent in the range.

If a number isn't available — say, the period had no off-peak data — that tile shows an em-dash (`—`) instead. Three of the tiles (Most usage, Most solar, Lowest SoC) point at a specific date; tap one and the charts below highlight that day so you can see what happened.

The card hides itself when there's no data at all, just like the chart cards already do.

## Intermediate (5–10 yrs)

The card lives at `Flux/Flux/History/HistoryStatsOverviewCard.swift`. Eight tiles are rendered in a `LazyVGrid` whose column count flips between 2 (compact iOS) and 4 (regular iOS / macOS) via `horizontalSizeClass`. Tile order is fixed by the `TileKey` enum (`totalUsage` → `lowestSoc`), driven through `ForEach(TileKey.allCases, id: \.self)`.

The card body never aggregates. Eight tile values come from `HistoryViewModel.PeriodSummary`, which already exposed the four totals (`solarTotalKwh`, `exportTotalKwh`, `peakImportTotalKwh`, `dailyUsageTotalKwh`) and now adds:

- `nightTotalKwh` / `nightBlockDayCount` (with computed `nightAvgKwh: Double?`)
- `mostUsageDay: DayKwhRecord?`
- `mostSolarDay: DayKwhRecord?`
- `lowestSocDay: LowestSocRecord?`

These four are computed in the same single pass `DerivedState.init(days:now:)` was already running. The `Totals` accumulator gained a generic `consider<T: DayRecordValue>(_:candidate:prefersLarger:)` comparator that's reused for the two max-records and the SoC min-record. Tie-break is uniform: equal value + later date wins (Decision 8). The SoC tie-break runs on the raw `Double` so display rounding doesn't perturb selection.

Day-cohort rules (Decision 5):
- **Complete days only** for Total solar, Total usage, Avg night, Most usage, Most solar
- **Days-with-offpeak in range, today included if it has an off-peak record** for Peak imports / Exported (mirrors existing `peakImportTotalKwh` / `exportTotalKwh` semantics)
- **All days-with-low, today included** for Lowest SoC

The tile rendering is split into two private SwiftUI views (`StatTile`, `DayRecordTile`) plus a set of static helpers on `HistoryStatsOverviewCard` (`label(for:)`, `valueText(for:summary:)`, `dateLineText(for:summary:)`, `isTappable(tile:summary:)`, `tapAction(for:summary:onSelect:)`, `accessibilityLabel(tile:summary:)`). The helpers are pure functions — tests assert against them directly without driving a SwiftUI layout pass (per AC 7.2 / 7.3).

Tappable tiles wrap their content in `Button(action:) { content }.buttonStyle(.plain)`; em-dash tiles render the same content without a button wrapper, so VoiceOver doesn't expose the `.isButton` trait (AC 3.2 / 6.3). Tapping reuses the `onSelect: (String) -> Void` plumbing the four chart cards already accept; on the overview card it routes through the same `selectDay` closure in `HistoryView`.

Formatters live in `HistoryStatsFormatters`:
- `socPercent`: half-up rounding via `Int(soc.rounded(.toNearestOrAwayFromZero))`, em-dash for non-finite (Decision 10).
- `shortDate` / `dateWithTime` / `dateRange`: Sydney time. `shortDate` delegates to `DateFormatting.shortMonthDay(from:)` so the new file doesn't add a fourth `MMM d` formatter to the codebase. `dateRange` uses `min`/`max` over entry dates so a defensively-reversed response still produces correct extremes.
- `accessibleKwh` / `accessibleSocPercent`: VoiceOver-friendly unit expansion ("kilowatt hours", "percent").

Card chrome change: `HistoryCardChrome.kpi` is now `String?` (Decision 14, source-compatible — Swift implicitly wraps the existing four call sites' non-nil strings). When nil the trailing KPI text is omitted. The overview card sets the KPI to the inclusive Sydney date range (`HistoryStatsFormatters.dateRange`) so it doesn't duplicate the picker (Decision 12).

The card sits inside the existing `viewModel.days.isEmpty` else-branch in `HistoryView`, so the screen-level empty/error state already gates it (AC 1.4).

## Expert

The interesting design tensions and how they resolve:

**1. Today inclusion is non-uniform across tiles.** Five tiles (Total usage, Total solar, Avg night, Most usage, Most solar) gate on `!isToday` via `DateFormatting.isToday(_:now:)`. Two (Peak imports, Exported) include today when `offpeakGridImportKwh` is set, because the existing `peakImportTotalKwh` / `exportTotalKwh` aggregates the Grid card already uses don't gate on today — and the overview card's contract (Decision 9) is "tile equals card KPI to the last digit". Lowest SoC includes today via the `socLow` semantics in `BatteryHeroView`. Decision 5 documents the asymmetry; fixture (b′) in `HistoryViewModelOverviewTests.swift:48` locks it so a future regression that adds `!isToday` to `Totals.addGrid` fails the test rather than silently reverting.

**2. Single-pass aggregation.** `DerivedState.init(days:now:)` already iterated `days` once to build solar / grid / battery / dailyUsage entries. The new aggregates piggyback on that pass. `addCompleteDay(_:parsedDate:dailyUsageEntry:)` takes the already-built `DailyUsageEntry?` and reuses its pre-clamped `stackedTotalKwh` for Most usage and its already-clamped night block for `nightTotal` — no re-summation, no re-clamp. `considerSocLow` is the only aggregate called unconditionally (i.e. for today too).

**3. Generic comparator with two record types.** `DayKwhRecord` and `LowestSocRecord` both adopt a private `DayRecordValue` protocol exposing `comparableValue: Double` and `date: Date`. `Totals.consider(_:candidate:prefersLarger:)` is generic over `T: DayRecordValue` and runs the same comparison for max-on-kWh and min-on-SoC. The `prefersLarger: Bool` flag flips strict-beats from `>` to `<`; the equal-then-later-date branch is symmetric. Two record types instead of one because `LowestSocRecord` carries `socLowTime` (Decision 11) — keeping it separate avoids a meaningless optional `time: String?` on the kWh records.

**4. SoC finiteness.** `Totals.considerSocLow` filters non-finite `socLow` payloads at the aggregate boundary, so `LowestSocRecord.soc` is always finite by construction. `HistoryStatsFormatters.socPercent` re-checks `isFinite` defensively so the formatter can be unit-tested against a hand-constructed pathological input without the test driving a `loadHistory` cycle. The redundancy is intentional — fixture (n′) in `HistoryStatsFormattersTests.swift:18` exercises this.

**5. Tile-key switch exhaustiveness.** All switches over `TileKey` enumerate every case explicitly (no `default:`). Adding a ninth tile requires updating each switch — the compiler rejects fall-through. Two helpers carry `// swiftlint:disable:next cyclomatic_complexity` because the eight-case switches genuinely have eight branches.

**6. Tap-target affordance.** `DayRecordTile` wraps content in `Button` only when `tapAction != nil && dayID != nil`. Em-dash tiles render `content` plain, so VoiceOver does not expose the `.isButton` trait — AC 3.2 / 6.3 fall out structurally rather than via a separate `accessibilityRemoveTraits`.

**7. Accessibility label composition.** Per AC 6.1 each tile is one accessibility element with `children: .ignore`. The label combines tile name + spelled-out value + (record tiles) long-month date / time. The visible date uses `MMM d` ("Apr 28") while the accessibility label uses `MMMM d` ("April 28") — VoiceOver reads dates better when the month is fully spelled.

**8. Formatter sharing.** `HistoryStatsFormatters.shortDate` delegates to `DateFormatting.shortMonthDay(from:)` so the new file doesn't add the fourth duplicate `MMM d` Sydney formatter to the codebase. The other three pre-existing duplicates in `HistoryView`, `DashboardView`, and `DayDetailViewSupport` are out of this branch's scope but would benefit from the same migration.

**9. Layout reflow.** `LazyVGrid` with `count: columnCount` columns. `columnCount` is derived from `horizontalSizeClass` on iOS (compact → 2, regular → 4) and fixed at 4 on macOS. `.animation(.default, value: columnCount)` smooths the reflow when iPad rotates between portrait and landscape. Known limitation (Decision 13): iPad in 1/3 split view reports `regular` at ~320 pt detail-column width — labels truncate. `ViewThatFits` fallback deferred to a follow-up unless real users hit it.

**10. Test split.** Three new test files. `HistoryViewModelOverviewTests` covers fixtures (a)–(o) for the aggregate semantics. `HistoryStatsFormattersTests` covers (n) / (n′) for SoC rounding without driving a `loadHistory` cycle. `HistoryStatsOverviewCardTests` covers labels, em-dash placement, date-line format, tap-target invariant, accessibility labels, and one end-to-end test that drives `selectDay` for the today-tap branch (AC 7.3).

## Completeness Assessment

**Fully implemented:**
- All 24 acceptance criteria (1.1–7.4) have implementing code and at least one test asserting against it. Mapping table in the spec & docs review covers the 1:1 trace.
- Fixtures (a) through (o) per design §Testing Strategy, distributed between the three new test files per the design's split.
- All 14 decisions in `decision_log.md` are reflected in code.
- Documentation updated: `CHANGELOG.md`, `specs/OVERVIEW.md`, `docs/agent-notes/ios-app-views.md`, `docs/agent-notes/ios-app-viewmodels.md`.
- Mock fixtures populate every tile so previews are not all em-dash.

**Partially implemented:**
- The Mac preview demonstrates a mixed em-dash / populated state (AC 7.4 calls for "at least one" mixing); the iPhone preview is fully populated. Acceptable per the AC wording.

**Not implemented (deferred):**
- iPad 1/3 split-view `ViewThatFits` fallback (Decision 13, called out as a known limitation).
- Migration of the three pre-existing `MMM d` Sydney formatters in `HistoryView` / `DashboardView` / `DayDetailViewSupport` to the new `DateFormatting.shortMonthDay`. Out of this branch's scope.

**Divergences from design:** None.
