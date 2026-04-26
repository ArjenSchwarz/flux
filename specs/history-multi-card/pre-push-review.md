# Pre-Push Review: history-multi-card

**Branch:** `history-multi-card`
**Commits reviewed:** `072a1ea` — `[feat]: history multi-card layout with peak vs off-peak grid split`
**Diff size:** 23 files changed, +890 / −228

## Scope

Single commit that:
- Adds per-day off-peak grid data to the Lambda `/history` response and surfaces in-progress off-peak deltas on `/status`.
- Replaces the iOS History screen's grouped 5-bar chart with three focused cards (solar / grid usage / battery) and a per-day summary card with a peak/off-peak grid split.

No spec existed for the change; this report doubles as the design record.

## Review findings and disposition

Three review agents (reuse, quality, efficiency) ran in parallel against the full diff. Findings are listed with their disposition.

### Fixed

| # | Finding | Where | Action |
|---|---|---|---|
| 1 | `buildOffpeak` (status.go) and `offpeakSplit` (history.go) duplicated the Status switch + delta projection. | `internal/api/status.go`, `internal/api/history.go` | Extracted shared helper `offpeakDeltas` in `internal/api/compute.go`. Both call sites now resolve a typed `offpeakDeltaValues` and apply their own rounding/null rules on top. `buildOffpeak` accepts `*TodayEnergy` instead of `*dynamo.DailyEnergyItem`; status.go does the conversion at the call site. |
| 2 | `OffpeakData.statusComplete`/`statusPending` were loose `static let String`. | `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift` | Replaced with a nested `enum Status: String, Codable, Sendable { case pending, complete }`. `status: Status?` on the response decodes directly. `isInProgress` now uses an enum comparison. |
| 3 | `HistoryViewModel.selectedDayRange` was written but never read after init. | `Flux/Flux/History/HistoryViewModel.swift`, `Flux/Flux/History/HistoryView.swift` | Removed. `HistoryView` owns the picker `@State`, defaults to 7. |
| 4 | `solarSeries`/`gridSeries`/`batterySeries`/`summary` were stored state with `didSet` cache invalidation. | `Flux/Flux/History/HistoryViewModel.swift` | Converted to computed properties backed by a single `DerivedState` value type built on demand. With `n ≤ 30` the recompute is trivial, and the `@Observable` macro tracks reads correctly. Removed `rebuildDerivedState` and the `didSet`. |
| 5 | The three card views each did `entries.first(where: { $0.dayID == selectedDayID })` on every `body` re-evaluation. | `Flux/Flux/History/HistorySolarCard.swift`, `HistoryGridUsageCard.swift`, `HistoryBatteryCard.swift`, `HistoryView.swift` | Cards now take `selectedDate: Date?` directly. `HistoryView` resolves it once via `DateFormatting.parseDayDate` and passes it to all three. |
| 6 | `Totals` was file-scope `private` but referenced `HistoryViewModel.GridEntry` / `PeriodSummary`. | `Flux/Flux/History/HistoryViewModel.swift` | Moved inside `HistoryViewModel` as a nested `private struct`. References are now unqualified. |

### Skipped (out of scope or not worth it)

- **Card chrome `.fluxCard()` extension across the app.** Pre-existing duplication in 13 locations across Dashboard / DayDetail / etc. Real but outside this commit's scope; would touch many unrelated files.
- **Unify `HistoryFormatters.kwh` with `EnergySummaryFormatter.formatKwh` and the four inline `"%.2f kWh"` strings.** Same pre-existing-fragmentation argument; deferred.
- **Replace custom `historySelectionOverlay` drag gesture with iOS 17 `chartXSelection`.** Reviewer flagged as a follow-up only; the current factoring is correct and the nearest-entry mapping would still be needed.
- **Card view structural duplication.** Each card differs in real ways (BarMark count, color scale, optional zero RuleMark); extracting further would push differences into config flags.
- **`buildOffpeak` parameter count growing from 3 to 4.** Still readable; an options struct would be premature.
- **`entries.map { ($0.dayID, $0.date) }` allocation per render and O(n) `selectDay` lookup.** Micro-optimisations at `n ≤ 30`.

## Verification

| Check | Result |
|---|---|
| `make check` (Go fmt / vet / golangci-lint / test) | pass — 0 issues |
| `xcodebuild build` | `BUILD SUCCEEDED` |
| `xcodebuild test` (unit suites) | 47 / 47 passed |
| SwiftLint (`--strict`) on touched files | clean (the 4 remaining errors are pre-existing in files outside this diff) |

## Verdict

**Ready to push.**

Six findings fixed; six skipped with reason. Backend changes apply only to the Lambda (`cmd/api`). The poller (`cmd/poller`) is unchanged — it already writes the start-snapshot fields the new in-progress projection reads.
