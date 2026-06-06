# Design: History Month and Week to Date

## Overview

Add "week to date" (Wk) and "month to date" (Mo) ranges to the History screen alongside the fixed 7/14/30-day ranges. The app resolves a selected range to an inclusive day-count ending today, computed against the Sydney calendar with the week's first weekday taken from the device locale, and sends it over the existing `?days=N` contract. The backend's only change is widening its accepted `days` range.

## Architecture

The range stops being a bare `Int` and becomes a `HistoryRange` enum that resolves to a day-count at load time. Everything downstream of the resolved count is unchanged because it already adapts to the actual data: chart axes derive their domain from the entries, `PeriodSummary`/`DerivedState` average over actual day counts, and `ChartScope.historyRange(days:)` carries only the resolved count `N`. The expanded chart re-fetches `?days=N` (and polls every 60s via `ChartSceneObserver`), so on the same Sydney day it reconstructs the identical window — a fixed 7d and a 7-day week both resolving to `days:7` is harmless precisely because their windows are identical, not because rendering is cached.

**Resolution path (per load trigger, satisfying [3.3](requirements.md)):**

```
HistoryRange ──resolvedDays(now, firstWeekday)──▶ N (1…31)
   │                                                │
   │                              ┌─────────────────┴───────────────┐
   ▼                              ▼                                  ▼
GET /history?days=N        rangeDays: N → cards            startDate = today-(N-1)
(backend re-derives          → ChartScope.historyRange      (offline cache bound)
 startDate = today-(N-1))      (days: N)
```

Both app and Lambda compute the window from the Sydney "today", so they agree except in the seconds-wide window where Sydney midnight passes mid-request — the same race the fixed ranges already tolerate. The expanded chart's observer carries only `N` (not the `HistoryRange`), so a chart left expanded across Sydney midnight keeps fetching `N` days ending on the new today rather than re-resolving the to-date window; this matches the fixed-range midnight tolerance and self-corrects when the chart is re-opened from the (re-resolved) inline card. Live re-resolution of the expanded view is out of scope.

### Pattern extension audit

Every site that touches the range, and whether it changes:

| Site | Today | Change? | Reason |
|---|---|---|---|
| `HistoryView` Picker (`:59-64`) | Int tags 7/14/30 | **Yes** | add Wk/Mo, tag with `HistoryRange` |
| `HistoryView.selectedRange` (`:9`) | `Int = 7` | **Yes** | → `HistoryRange = .days(7)` |
| `HistoryView` load calls — `.task`/`.onChange`/`.refreshable`/`macRefreshAction` (`:101-120`) | `loadHistory(days:)` | **Yes** | → `loadHistory(range:)` |
| `HistoryView` card `rangeDays:` args (`:196,207,218`) | `selectedRange` Int | **Yes** | → `viewModel.resolvedRangeDays` |
| `HistoryViewModel.loadHistory(days:)` (`:47`) | Int param | **Yes** | → `loadHistory(range:)`, resolve N inside |
| `HistoryViewModel.lastRequestedDays` (`:12`) | Int | **Yes** | → `lastRequestedRange: HistoryRange` |
| `HistoryViewModel.reload()` (`:78`) | replays Int | **Yes** | re-resolve from `lastRequestedRange` ([3.3](requirements.md)) |
| `HistoryViewModel.loadCachedDays(limit:)` (`:139`) | newest-N by count | **Yes** | → date-bounded by start ([4.3](requirements.md)) |
| `HistoryViewModel.resolvedRangeDays` | — | **Add** | last resolved count for cards/ChartScope |
| `FluxAPIClient.fetchHistory(days:)` | Int | No | still receives a resolved Int |
| `ChartScope.historyRange(days:)` (`ChartKind.swift:47`) | Int | No | resolved Int flows through |
| Cards `expansionScope`/`rangeDays` (Solar/Grid/DailyUsage `:13`) | Int | No | receive resolved Int |
| Chart x-axes (e.g. `HistoryDailyUsageCard:90`) | stride from `entries.count` | No | already adaptive |
| `DerivedState`/`PeriodSummary` (`HistoryDerivedState.swift`) | from `days` array | No | already adaptive |
| `ExpandedChartView.defaultHistoryRangeDays` (`:91`) | fallback 7 | No | unchanged; ChartScope still carries Int |
| `internal/api/history.go` `validDays` (`:16,27`) | `{7,14,30}` | **Yes** | accept 1–31, update message |

## Components and Interfaces

### FluxCore — `DateFormatting` additions

Pure, Sydney-calendar boundary helpers (testable, locale only via the passed `firstWeekday`):

```swift
/// Sydney-calendar 00:00 on the 1st of the month containing `now`.
static func startOfMonth(now: Date) -> Date

/// Sydney-calendar 00:00 on the week start containing `now`.
/// `firstWeekday` follows Calendar's convention (1 = Sunday … 7 = Saturday);
/// only the weekday is locale-driven — the date arithmetic is Sydney.
static func startOfWeek(now: Date, firstWeekday: Int) -> Date

/// Inclusive count of Sydney calendar days from `start` through `end`.
/// Computed by calendar-day difference (both normalised to Sydney midnight),
/// never by dividing an elapsed interval, so 23/25-hour DST days don't shift it.
static func inclusiveDayCount(from start: Date, through end: Date) -> Int
```

`startOfWeek` mutates a **copy** of `sydneyCalendar` (never the shared `static let`, which would corrupt every other date computation) with `firstWeekday` set, then `dateInterval(of: .weekOfYear, for: now)?.start`; `startOfMonth` uses `dateInterval(of: .month, for: now)?.start`.

### App — `HistoryRange` (new, in `Flux/History/`)

```swift
enum HistoryRange: Hashable {
    case days(Int)        // 7, 14, 30
    case weekToDate
    case monthToDate

    var pickerLabel: String   // "7d"/"14d"/"30d"/"Wk"/"Mo"

    /// Inclusive day-count ending on `now` (1…31). Fixed cases return their N.
    func resolvedDays(now: Date, firstWeekday: Int) -> Int
}
```

`pickerLabel` is the only UI string; the boundary math delegates to the FluxCore helpers. Selection order in the Picker: `.days(7), .days(14), .days(30), .weekToDate, .monthToDate` ([6.1](requirements.md)).

### App — `HistoryViewModel` changes

- `loadHistory(range: HistoryRange)` replaces `loadHistory(days:)`. It sets `lastRequestedRange = range` **before** the `isLoading` guard so a selection made during an in-flight load is not dropped, then resolves `N` from `nowProvider()` + a new injected `firstWeekdayProvider()`, sets `resolvedRangeDays = N`, and calls `fetchHistory(days: N)`. After the fetch it re-checks `lastRequestedRange`; if a newer range arrived during the load it loops and loads that one (coalescing — the latest selection always wins, so the picker segment and the rendered data never disagree).
- New init param `firstWeekdayProvider: @Sendable () -> Int = { Calendar.current.firstWeekday }`, mirroring the existing `nowProvider` injection for test determinism. Read at resolution time ([3.3](requirements.md)).
- `reload()` re-resolves from `lastRequestedRange` against the current `now`, so an app left open across Sydney midnight reloads the correct window.
- Offline fallback becomes date-bounded: `loadCachedDays(onOrAfter: startDate)` fetches `CachedDayEnergy` where `date >= startDate` (lexicographic compare on zero-padded `YYYY-MM-DD` equals chronological; `startDate` captured as a `let` for the `#Predicate`), **sorted ascending** — flipping the current `.reverse` descriptor so the offline shape matches the online response. `startDate = today-(N-1)` (Sydney), so a gappy cache never surfaces a day before the period start ([4.3](requirements.md)). Applying this uniformly to fixed ranges is a strict correctness improvement (newest-N-by-count could previously cross the window on a gap) and also corrects a latent bug: the current descending sort makes `selectDefaultDayIfNeeded` auto-select the *oldest* cached day offline, whereas ascending makes `days.last` the newest, matching online.
- `resolvedRangeDays: Int` (default 7) is set inside `loadHistory` and read by `HistoryView` for the cards' `rangeDays:`. On the first frame and briefly mid-load the cards read the default/previous count; harmless because charts and summaries are data-adaptive and only the expansion scope's `N` is affected.

### App — `HistoryView` changes

`selectedRange: HistoryRange = .days(7)`; the segmented Picker tags each case and shows `pickerLabel` ([6.3](requirements.md) keeps `.pickerStyle(.segmented)`); load triggers call `loadHistory(range: selectedRange)`; cards receive `rangeDays: viewModel.resolvedRangeDays`. Known limitation: five segments at large Dynamic Type sizes can truncate on the narrowest iPhones; the short fixed-width abbreviations ("Wk"/"Mo") mitigate this and the segmented style is retained per [6.3](requirements.md).

### Backend — `internal/api/history.go`

Replace the `validDays` allowlist with a bounds check:

```go
if err != nil || parsed < 1 || parsed > 31 {
    return errorResponse(400, "invalid days parameter, must be between 1 and 31")
}
```

The `err != nil` check is retained so a non-numeric value (e.g. `?days=x`) still 400s. The default-7 path stays inside the existing `if d := req.QueryStringParameters["days"]; d != ""`. `startDate = now.AddDate(0,0,-(days-1))` and the default of 7 are unchanged. The 31 ceiling covers the longest month-to-date; historical rows come from `flux-daily-energy` (no TTL), and only the single today row uses the 24-hour readings window, so no retention limit is hit.

## Error Handling

- **Out-of-range `days`**: the app only ever sends 1–31, so a 400 is reachable only via clock skew / the midnight race. It surfaces through the existing `errorState` with Retry ([4.4](requirements.md)); Retry re-resolves against the current `now`, self-correcting after the boundary settles.
- **Offline / fetch failure**: date-bounded cache fallback as above; empty cache shows the existing error or empty state unchanged.
- **Backgrounded app / stale window**: an already-rendered range persists until the next load trigger, so a window resolved before Sydney midnight stays shown until the user refreshes, switches range, or reactivates the screen — the accepted trade-off from Decision 5.

## Testing Strategy

**FluxCore (`DateFormatting`)** — example-based across edge inputs:
- `startOfMonth`/`inclusiveDayCount`: 28/29/30/31-day months; on the 1st → count 1; on the 31st → count 31.
- `startOfWeek`: `firstWeekday` ∈ {1 Sunday, 2 Monday, 7 Saturday}; on the week-start day → count 1; mid-week → expected count.
- DST: Sydney transition days (early April, early October) return the same day counts as non-DST days (guards against interval-division regressions).

**Property-based candidate** — the boundary helpers express clean invariants. Using Swift Testing `@Test(arguments:)` over a generated set of seed dates (no `Date.now`) and all `firstWeekday` 1…7:
- `weekToDate` resolved count ∈ 1…7; `startOfWeek` weekday == `firstWeekday`; `start ≤ now`.
- `monthToDate` resolved count ∈ 1…31; `startOfMonth` is day 1; `start ≤ now`.

**`HistoryViewModel`** (injected `now` + `firstWeekday`):
- `loadHistory(range:)` resolves the expected `N` and `startDate` for Wk/Mo on representative dates.
- `reload()` after advancing `now` past midnight re-resolves to the new window ([3.3](requirements.md)).
- Offline fallback with a gappy cache excludes days before `startDate`, returns days ascending, and auto-selects the newest (today) day ([4.3](requirements.md)).
- A range switch during an in-flight load coalesces to the latest selection (final `days`/`resolvedRangeDays` match the last-selected range).
- Failed fetch with empty cache yields `error` / empty days ([4.4](requirements.md)).

**Consistency ([5.2](requirements.md))**: feeding an identical `[DayEnergy]` array through the card-building path yields identical `DerivedState`/`PeriodSummary` regardless of which range produced it (unit, no fetch).

**Backend (`history.go`)** — map-based table test: `days` ∈ {1, 7, 31} accepted with correct window length; {0, 32, -1, "x"} → 400 with the new message; absent param → 7.
