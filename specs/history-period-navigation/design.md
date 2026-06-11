# Design: History Period Navigation

**Ticket:** T-1497 · **Requirements:** [requirements.md](requirements.md)

## Overview

Adds previous/next stepping, a date-picker jump, and a return-to-current action for the Wk/Mo History ranges, backed by a new explicit date-range form of the `/history` request. Three layers change: the Go handler accepts `start`/`end`, FluxCore grows a query type plus period date-math, and the History screen gains an anchor state and a navigation header.

## Architecture

### Request flow

```
HistoryView (range picker, period header)
  → HistoryViewModel (range + periodAnchor → HistoryQuery)
    → FluxAPIClient.fetchHistory(query:)        [FluxCore]
      → GET /history?days=N                      (current period — unchanged)
      → GET /history?start=YYYY-MM-DD&end=YYYY-MM-DD   (past period — new)
        → handleHistory: range form → no readings query, no live compute (past-only)
```

The response shape (`HistoryResponse`) is unchanged; past periods simply never contain a live today row (req 5.3).

### New shared types

**`HistoryQuery`** (FluxCore, `Networking/HistoryQuery.swift`) — the one type that travels from view model to API client to chart-expansion scope, so every consumer fetches the same window:

```swift
public enum HistoryQuery: Hashable, Sendable, Codable {
    case days(Int)                              // existing anchored-to-today form
    case dateRange(start: String, end: String)  // inclusive YYYY-MM-DD bounds
}
```

`Codable` is required, not optional: `ChartScope` (which will carry this type) is `Hashable, Codable` (`ChartKind.swift:46`) and is encoded for scene/expansion restoration — both cases synthesize trivially.

**`HistoryPeriod`** (app, `Flux/Flux/History/HistoryPeriod.swift`) — a resolved calendar period with all date math in one place, built on the existing `DateFormatting` helpers (Sydney calendar, locale first weekday per the T-1361 rules):

```swift
struct HistoryPeriod: Equatable {
    let start: Date          // Sydney midnight
    let endExclusive: Date   // Sydney midnight after the last day

    static func week(containing: Date, firstWeekday: Int) -> HistoryPeriod
    static func month(containing: Date) -> HistoryPeriod
    func previous(range: HistoryRange, firstWeekday: Int) -> HistoryPeriod
    func next(range: HistoryRange, firstWeekday: Int) -> HistoryPeriod
    // previous/next assert on `.days` ranges — navigation is undefined there
    // and the controls are never shown (req 1.5); a silent self-return would
    // hide a wiring bug.
    func contains(_ date: Date) -> Bool
    var dayCount: Int        // M in "N of M days"
    var startDateString: String  // YYYY-MM-DD
    var endDateString: String    // inclusive
}
```

`previous`/`next` shift by `date(byAdding: .day, value: ∓7)` for weeks and `.month, ∓1` for months on `start` — calendar arithmetic, never interval division, so DST days don't drift (same rule as `HistoryChartDomain.build`).

### View-model state

`HistoryViewModel` gains one piece of state: `periodAnchor: Date?` — `nil` means current (to-date) period; non-nil is the Sydney-midnight start of a past period. The picker's `HistoryRange` enum is untouched, so picker tags and equality keep working.

The anchor is mutated ONLY by intent methods — the load path itself is anchor-agnostic, so refresh can never reset the user's place (req 1.8). `reload()` keeps its current body (`loadHistory(range: lastRequestedRange)`) and stays correct precisely because `loadHistory` no longer touches the anchor:

| Intent method | Anchor effect | Caller |
|---|---|---|
| `selectRange(_ range:)` — sets anchor `nil`, then loads | reset (req 2.3) | picker `onChange`, view `.task` |
| `navigatePrevious()` / `navigateNext()` | shift via `HistoryPeriod`; `next` landing on the period containing Sydney-today → `nil` | header chevrons, macOS arrow keys |
| `jumpTo(date: Date)` | period containing date; contains today → `nil` (req 3.3) | date picker |
| `returnToCurrent()` | `nil` | "Current" button |
| `loadHistory(range:)` / `reload()` | none — anchor untouched | refresh paths |

Req 1.8 has two existing violations to fix in `HistoryView`: `.refreshable` (`HistoryView.swift:121`) and the error-state Retry button (`:295`) both call `loadHistory(range: selectedRange)`; under the new split that is harmless to the anchor, but both still switch to `reload()` so a stale `selectedRange` capture can never diverge from `lastRequestedRange`. The picker `onChange` and the `.task` switch to `selectRange(_:)`.

Load resolution: anchor `nil` → existing `resolvedDays` path → `.days(N)`. Anchor set → `.dateRange(start:end:)` from `HistoryPeriod`. The in-flight coalescing loop currently keyed on `lastRequestedRange` is re-keyed on a `RequestedPeriod` pair (range + anchor) so a navigation during a load is honoured the same way a range change is.

### Resolved snapshot — requested vs rendered

The existing code deliberately separates requested from resolved state: `chartDomain` reads `resolvedRange`, not `lastRequestedRange`, "so the charts' x-axis reservation matches the rendered data, not an in-flight selection" (`HistoryViewModel.swift:17-20`). The anchor follows the same rule via one atomic snapshot set inside `load()` alongside `resolvedRange`:

- `resolvedQuery: HistoryQuery` — the query whose data is in `days`.

Everything user-visible derives from the resolved snapshot, never from the live `periodAnchor`: `chartDomain` (reference date = the resolved period's start, falling back to `now` for `.days`), `resolvedRangeDays` (`.days(n)` → n; `.dateRange` → `HistoryPeriod.dayCount`), the period label, the cards' expansion scope, and the next-chevron disabled state (`resolvedQuery` is `.days` — i.e. the rendered period contains today). Keying the chevron off the resolved query rather than `periodAnchor == nil` also keeps it correct across a midnight period-rollover until the next load re-resolves (requirements Definitions). `HistoryChartDomain.make`'s `now:` parameter becomes `referenceDate:` (req 1.4: past periods always reserve the full week/month containing it).

### "Today" and current-period checks

All checks (anchor-collapse-to-nil in `navigateNext`/`jumpTo`, picker max) use `DateFormatting`'s Sydney calendar against `nowProvider()`, re-evaluated on each load trigger (requirements Definitions).

### Pattern parity audit — `fetchHistory(days:)` and `ChartScope.historyRange`

| Site | Needs equivalent | Change |
|---|---|---|
| `HistoryViewModel.load` (`HistoryViewModel.swift:88`) | yes | call `fetchHistory(query:)` |
| `ChartKind.swift:47` `case historyRange(days: Int)` | yes | → `case historyRange(HistoryQuery)` |
| `HistorySolarCard/GridUsage/DailyUsage.expansionScope` | yes | build from a new `periodQuery: HistoryQuery` param passed by `HistoryView` (replaces deriving from `rangeDays`) |
| `ChartSceneObserver.refresh` (`ChartSceneObserver.swift:83`) | yes | fetch via the scope's query; 60 s polling of an immutable past period is harmless and matches refresh semantics |
| `ChartExpansionContent.historyRangeDays(from:)` | yes | day count: `.days(n)` → n; `.dateRange` → `inclusiveDayCount` |
| `ExpandedChartView` default scope (`:91`) | yes | `.historyRange(.days(defaultHistoryRangeDays))` |
| `MockFluxAPIClient` (`Flux/Flux/Services/MockFluxAPIClient.swift:258`) | yes | implement `fetchHistory(query:)` — it backs every preview; under the protocol default a `.dateRange` would throw and previews navigating to a past period would show the error state |
| `URLSessionAPIClient+Simulation` | no | has no history path |
| Widget targets | no | no history usage |

Expanded windows do not currently use `HistoryChartDomain` (no full-period reservation); that pre-existing gap is unchanged by this feature — the expanded chart shows the same days, auto-fit.

## Components and Interfaces

### Backend — `internal/api/history.go`

Parameter parsing replaces the current `days` block. The mixed-form check distinguishes a *present* `days` parameter from the defaulted `days = 7` (`history.go:22`):

- No parameters → `days = 7`, existing behaviour (the easiest regression to introduce in this rewrite — it gets its own test row).
- `days` alone → existing behaviour, untouched (req 5.4).
- `start` and `end` alone → explicit range. Parse with `time.ParseInLocation("2006-01-02", v, sydneyTZ)`; 400 on parse failure.
- `days` together with `start`/`end`, or only one of `start`/`end` → 400 (req 5.1).
- Validation (req 5.6): `end < start` → 400; `end >= today` → 400 (string compare on zero-padded dates is safe); span: `end.After(start.AddDate(0, 0, 30))` → 400. `AddDate` is calendar-aware, so a DST transition inside the window cannot produce an off-by-one.

The range form is **past-only** (`end` strictly before Sydney today). The app never requests a range ending today — the current period always goes through `days=N` — so permitting it would create an untested live-compute surface for no consumer. Consequence: the date-range path never touches live compute at all — the readings goroutine is not started, `todayComputed`/`todayReadings` stay nil, and the per-day loop's `isItemToday` is false for every row (req 5.3 — also skips two DynamoDB queries per past-period request). The `days` path keeps its `includesToday` behaviour unchanged. `QueryDailyEnergy`, `QueryOffpeak`, and `fetchNotesAsync` take `(startDate, endDate)` instead of `(startDate, today)`. Days absent from storage are simply absent from the result — already today's behaviour (req 5.5); never-stored fields are already omitted per row (req 5.2).

**Day Detail parity (req 5.7):** for any non-today date, `/day` and `/history` both serve every shared summary field (energy totals, peak grid import, off-peak split, socLow, dailyUsage, peakPeriods) from the same non-TTL `flux-daily-energy` row, gated identically by `isToday`/`isItemToday` (`day.go:175-216` vs `history.go:130-198`) — neither recomputes from the TTL-expired `flux-readings`, so parity holds structurally even for dates older than 30 days, which this feature makes reachable in Day Detail for the first time. The existing `cross_handler_test.go` parity test asserts only derivedStats; it is extended to also assert energy totals, peak grid import, and off-peak split for an old date.

### FluxCore

- `FluxAPIClient` gains `func fetchHistory(query: HistoryQuery) async throws -> HistoryResponse`, with a protocol-extension default that delegates `.days(n)` to the existing required `fetchHistory(days:)` and throws `FluxAPIError.notConfigured` for `.dateRange` — the established evolution pattern (`fetchStatus(simulateLoadWatts:)`), so the ~30 existing test mocks keep compiling.
- `URLSessionAPIClient` implements it: `.days` → `?days=N`; `.dateRange` → `?start=…&end=…`.
- `DateFormatting`: no new functions — `HistoryPeriod` composes the existing helpers (`startOfWeek`, `startOfMonth`, `sydneyCalendar`, `inclusiveDayCount`, `dayDateString`); the month interval comes from `sydneyCalendar.dateInterval(of: .month, for:)` as in `HistoryChartDomain`. The period-label formatters ("MMM d", day-only "d", and month-year "MMM yyyy" — none of which exist today) live file-private in `HistoryPeriodHeader`, Sydney-zoned, mirroring the `HistorySummaryDateFormatter` precedent (`HistoryView.swift:326`).

### History screen

**`HistoryPeriodHeader`** (new, `Flux/Flux/History/HistoryPeriodHeader.swift`) — shown between the range picker and the note row, only when `selectedRange` is `.weekToDate`/`.monthToDate` (req 1.5). Layout and button styling match `DayNavigationHeader` (`DayDetailViewSupport.swift:50`): `chevron.left` / centred label / `chevron.right`, same `navButton` treatment and disabled style.

- Label (req 4.1): week → `"Jun 2 – 8"` (cross-month `"May 29 – Jun 4"`) via `shortMonthDay`; month → `"May 2026"`. The label is a button that presents the date picker (popover; compact iOS presents as sheet) — `DatePicker(.graphical)` with `in: ...sydneyTodayEnd` (req 3.2). The picker view gets `.environment(\.calendar, …)` / `.environment(\.timeZone, …)` set to Sydney — `DatePicker` otherwise renders and caps in the device calendar, so on a non-Sydney device "today" and the snapped period could be off by a day.
- A compact "Current" button appears trailing the header only when `periodAnchor != nil` (req 2.1/2.2, Decision 8).
- Deviation from Day Detail: macOS gets the same in-content header rather than window-toolbar chevrons — the picker-trigger label must be in content anyway, and req 1.7 only requires the keyboard affordance, added via `.focusable()` followed by `.onKeyPress(.leftArrow/.rightArrow)` on `HistoryView` (guarded to Wk/Mo), mirroring `DayDetailView.swift:104-111` — without `.focusable()` the key handler never receives events.

**`HistoryView`** — passes `viewModel.periodQuery` to the three cards for their expansion scope. Empty handling splits on `periodAnchor`: for a past period with no error and zero days, the cards stay rendered (the `HistoryChartDomain` scaffold reserves the full-period axis, satisfying req 1.6's "full-period chart axis") with a compact "No data for this period" notice in place of the note row — NOT the existing replace-everything `emptyState`, which would remove the charts and the axis with them. The current-period empty state and `errorState` are unchanged (req 7.3 distinguishes them).

**`HistoryStatsOverviewCard`** — `HistoryCardChrome`'s unused `subtitle` slot shows `"N of M days"` when viewing a past period with `summary.dayCount < periodDays` (req 6.2). N is `summary.dayCount` (days with a stored record), M is `resolvedRangeDays`. No changes to `DerivedState`/`PeriodSummary` math: for past periods no row is today, so every existing "complete days" average naturally divides by days-with-data (req 6.1) and current-period behaviour is untouched (req 6.3). Accepted wording overlap: the costs card's caption counts *recorded* days ("N of N days priced", `PeriodCosts`), so a partial past month can show "11 of 30 days" and "11 of 11 days priced" together — different questions (period coverage vs pricing coverage of recorded days); the costs card is unchanged.

### Cache

`loadCachedDays(onOrAfter:)` becomes `loadCachedDays(from:through:)` with both bounds in the predicate (`date >= lower && date <= upper`); the current-period call passes `(windowStart, todayString)` — same result set as before since nothing later than today is ever cached — and the past-period call passes the period bounds (req 7.2). `cacheHistoricalDays` works unchanged for past days (it upserts by date and only skips today's row) (req 7.1).

## Error Handling

| Condition | Behaviour |
|---|---|
| Mixed/incomplete/invalid params on `/history` | 400 with message naming the rule violated (mirrors existing `days` 400) |
| Past-period fetch fails, cache has period days | bounded cached days shown, existing offline presentation (req 7.2) |
| Past-period fetch fails, no cached days | existing `errorState` with Retry (req 7.3) |
| Past-period fetch succeeds, zero days | "No data for this period" state, full-period chart axis (req 1.6) |

## Testing Strategy

**Go (`internal/api/history_test.go` additions)** — table-driven: param matrix (no params → days=7 default unchanged, days only, range only, both → 400, lone start/end → 400, bad date → 400, `end < start` → 400, `end == today` → 400, `end > today` → 400, 31-day span OK, 32-day span → 400, span crossing the April DST fallback); past-range response has no live compute and no readings query (record calls on the reader stub); range predating data returns the existing subset without error. **`cross_handler_test.go`** — extend the `/day`-vs-`/history` parity assertion beyond derivedStats to energy totals, peak grid import, and off-peak split, for a date old enough that its readings would be TTL-expired (req 5.7).

**FluxCore (Swift Testing)** — `URLSessionAPIClient` encodes both query forms; protocol default for `.dateRange` throws.

**App — `HistoryPeriod` property-based tests** (the PBT candidate: round-trip and containment invariants over seeded random dates spanning DST transitions and month-length edges, in the style of the T-1361 suite):
- `p.next(...).previous(...) == p` and vice versa
- `p.contains(p.start)` and `!p.contains(p.endExclusive)`
- week periods are always 7 slot days; month periods 28–31
- `week(containing: d)` is identical for every `d` in the same week (likewise month)

**App — `HistoryViewModel`** — navigation sets the expected `HistoryQuery` on a recording mock; anchor resets on `selectRange` but not on `reload()`/`loadHistory`; next from "previous week" lands on current (anchor nil, `.days` query); jumpTo a date in the current month → anchor nil; coalescing honours a navigation issued mid-load; `resolvedQuery`/`chartDomain` reflect rendered data, not an in-flight navigation; cache fallback is bounded both ends; empty-vs-error state selection.

**App — snapshot-free view checks** — header hidden for `.days` ranges; "Current" button visibility; stats card subtitle for partial periods; an empty past period keeps the chart cards rendered (scaffold axis present) with the no-data notice, rather than falling into the replace-everything empty state.
