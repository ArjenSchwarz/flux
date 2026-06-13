# Implementation Explanation: History Period Navigation

Branch `T-1497/history-period-navigation`, 11 commits vs `origin/main`, 45 files, +3333/−231.

## Beginner Level

### What Changed

The History screen in the Flux app shows charts of your home's energy use — solar generated, power used, battery activity — over a chosen range like "this week" or "this month". Until now it could only show the *current* week or month. This change adds time travel: arrows to step back to last week (or last month), a calendar to jump to any past date, and a "Current" button to come back to today.

To make that possible, the server gained a new way of asking for data. Before, the app could only say "give me the last N days". Now it can also say "give me exactly March 1st through March 31st". The server treats those past requests in a simpler, safer way: it only returns numbers it already saved, and never tries to compute anything live.

### Why It Matters

You can now answer questions like "how much solar did we generate in March?" or "what did the week of the storm look like?" without exporting data or guessing. And because past numbers come straight from what the server recorded, the same date shows identical numbers everywhere in the app — a rule this project takes seriously.

### Key Concepts

- **Period**: one calendar week or month, defined by Sydney time (where the battery lives), so a "day" always matches what the hardware recorded — even if your phone is in another timezone.
- **Anchor**: the app remembers *which* period you're viewing as a single date ("the week containing June 3rd"). No anchor means "the current period".
- **Live vs stored data**: today's numbers change every 10 seconds (live); past days are fixed records. The new past-period requests deal only in fixed records.

---

## Intermediate Level

### Changes Overview

**Backend (Go, `internal/api/history.go`)**: `GET /history` gains a second, mutually-exclusive request form: `?start=YYYY-MM-DD&end=YYYY-MM-DD` alongside the existing `?days=N`. The range form is past-only — `end` must be strictly before Sydney-today (Decision 15) — capped at 31 inclusive days, and skips the readings query and all live compute (the today-readings channel is pre-filled empty). Mixed forms 400. The `days` path is byte-for-byte unchanged.

**FluxCore (`Networking/HistoryQuery.swift`)**: a `HistoryQuery` enum (`.days(Int)` / `.dateRange(start:end:)`, Hashable + Sendable + Codable) plus `fetchHistory(query:)` on `FluxAPIClient`. A protocol-extension default delegates `.days` to the existing `fetchHistory(days:)` and throws for `.dateRange`, so ~30 existing mocks compile unchanged — the same evolution pattern used for `fetchStatus(simulateLoadWatts:)`.

**Period model (`Flux/Flux/History/HistoryPeriod.swift`)**: value type holding a Sydney-midnight `start` and `endExclusive`, with `week(containing:firstWeekday:)` / `month(containing:)` factories, `previous`/`next` via calendar arithmetic (`.day ±7`, `.month ±1` — never interval division), `contains`, `dayCount`, and date-string accessors. It is the single owner of period boundary math; `HistoryChartDomain.make` delegates to it.

**View model (`HistoryViewModel`)**: a `periodAnchor: Date?` (nil = current period) mutated only by five intent methods — `selectRange`, `navigatePrevious`, `navigateNext`, `jumpTo`, `returnToCurrent` — while the load path stays anchor-agnostic (`reload()` never moves you, req 1.8). In-flight coalescing is keyed on `RequestedPeriod` (range + anchor) so a navigation issued mid-load wins. A `resolvedQuery` snapshot is adopted atomically with the data it describes and drives the header label, chart domain, day counts, and the next-chevron disabled state — the UI always describes *rendered* data, not in-flight requests. `navigateNext()` clamps at the current period in the view model (Decision 17) because the chevron's disabled state lags during loads. Offline fallback (`loadCachedDays(from:through:)`) is bounded on both ends.

**UI**: `HistoryPeriodHeader` (chevrons + tappable label + Current button), styled after Day Detail's navigation header, shown only for the Wk/Mo ranges. The label opens a graphical `DatePicker` capped at Sydney-today and rendered with explicit Sydney calendar/timezone environment values — device-calendar rendering would be off by a day on non-Sydney devices. macOS gets ←/→ key navigation. An empty past period keeps the cards rendered (axis intact) with a compact "no data" notice — distinct from the error state. Chart expansion carries the actual `HistoryQuery` through `ChartScope`, so an enlarged chart fetches exactly what its card showed.

### Trade-offs

- **Two request forms, permanently** (Decision 16): `days=N` anchors "today" server-side, so a current-period request built just before midnight stays consistent; migrating everything to `start/end` would reintroduce the live-compute hybrid path Decision 15 removed.
- **Anchor + resolved snapshot instead of a single "current period" property**: requested state (what the user asked for) and resolved state (what's on screen) genuinely differ during loads; conflating them caused the class of bug Decision 17 fixes.
- **Full calendar periods for past, to-date for current**: a past "week" is always 7 slots; the current week renders only elapsed days. Same range control, two rendering rules — handled by `HistoryChartDomain` taking a reference date rather than always "now".

---

## Expert Level

### Technical Deep Dive

- **Validation order in the Go handler**: days-present vs days-defaulted is distinguished *before* parsing so `?days=7&start=…` 400s even though 7 is the default. The past-only check compares the raw `end` param string against today's `2006-01-02` string — safe because Go's fixed-width layout rejects non-zero-padded input, so string order equals date order. The 31-day cap uses `end.After(start.AddDate(0,0,30))`, which is calendar-aware across the April DST fallback (both 31-OK and 32-reject spans crossing it are in the test matrix).
- **No-goroutine skip**: the range path pre-fills the buffered readings channel with an empty result instead of branching the errgroup structure — downstream merge code is identical for both forms, which is what keeps the days path byte-for-byte unchanged.
- **Coalescing key**: keying in-flight dedup on `RequestedPeriod(range, anchor)` rather than range alone means navigate-while-loading is a *different* request and is honoured, while a redundant reload of the same period still coalesces. `lastRequestedPeriod` is derived (computed property), not stored — all mutations are MainActor-synchronous before the loop re-reads it.
- **`HistoryQuery.dayCount`**: `.dateRange` day count parses both dates in Sydney and uses the inclusive-day helper; this is the single derivation used by `resolvedRangeDays` and previously duplicated in the chart-expansion path (removed in review — the expansion pipeline now passes the real query end-to-end instead of collapsing to an `Int` and fabricating a nominal `.days` query).
- **Cross-handler parity**: `/day` and `/history` (range form) are pinned equal for a >30-day-old date — readings TTL-expired, so both must serve the persisted row — across energy totals, peak grid import, and the off-peak split. This is the project's data-consistency rule made executable.
- **Property-based period tests**: seeded SplitMix64 dates plus hand-picked edges (both 2026 Sydney DST transitions, leap Feb, month/year boundaries) assert `next(previous(p)) == p`, `contains(start) && !contains(endExclusive)`, weeks are exactly 7 slot days, and `week(containing:)` is stable across every day of the same week.

### Architecture Impact

`HistoryPeriod` is app-layer (not FluxCore) because it encodes presentation policy (what a "period" means on this screen); `HistoryQuery` is FluxCore because it's wire format. The `ChartScope` Codable shape changed (`historyRange(days:)` → `historyRange(HistoryQuery)`) — verified safe because no scope is ever persisted; the registry is in-memory. The protocol-default evolution pattern now has two precedents, making it the de-facto convention for extending `FluxAPIClient`.

### Potential Issues

- The header label reflects *resolved* state while the chevrons step from *requested* state; on a slow connection, rapid prev-taps step periods the label hasn't shown yet. Accepted as a consequence of the resolved-snapshot rule; a header loading affordance is the obvious future fix if it bothers users.
- `HistoryPeriodHeader.navButton` duplicates `DayNavigationHeader.navButton` styling verbatim. A shared `CircularNavButton` would also fix Day Detail's missing accessibility labels — deliberate deferral, noted in review.
- Server clock vs app clock: an app whose Sydney-today differs from the server's (clock skew at midnight) could send `end == server-today` and get a 400; the app surfaces it as the error state with Retry, which self-heals once either clock ticks over.

---

## Completeness Assessment

**Fully implemented**: all 28 numbered requirements (1.1–7.3) have corresponding implementation; 26 have direct tests. The review's requirements-coverage table found no unimplemented requirement. All 12 tasks complete; all divergences documented (17-decision log).

**Partially covered (tests only)**: req 1.7 (macOS arrow keys) — wiring untested, consistent with Day Detail's precedent; req 7.1 (past-day caching) — exercised only indirectly through the unchanged `cacheHistoricalDays` path. Both accepted.

**Missing**: nothing identified. Post-review additions closed the two flagged gaps (req 3.2 Sydney picker bound, req 6.1 average-divisor test).
