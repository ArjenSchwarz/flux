# Implementation: History Month and Week to Date

Three-level explanation of the implemented feature, used both as documentation and
as a completeness check against the requirements and design.

## Beginner Level

### What This Does

The History screen used to offer three fixed ranges — 7, 14, and 30 days. This
change adds two more: **Wk** ("this week so far") and **Mo** ("this month so
far"). Picking Wk shows every day from the start of the current week through
today; picking Mo shows the 1st of the month through today.

The week and month boundaries are worked out using the same Sydney-based
calendar the stored data is keyed by, and the day a week "starts on" follows the
phone's regional setting (some locales start the week on Sunday, others on
Monday).

### Why It Matters

A user no longer has to mentally map "this month" onto a fixed 30-day window.
Late in a 31-day month, "30 days" and "this month" are genuinely different sets
of days; now the app can show exactly the period the user means.

### Key Concepts

- **To-date range**: a range that ends today and starts at the beginning of the
  current week or month, so its length changes day by day (1 to 31 days).
- **Sydney calendar**: the app's data is stored per Sydney calendar day, so all
  the "which days are in this range" maths is done in that timezone to stay
  aligned with the data.
- **First weekday**: which day a week begins on. Taken from the device locale
  (`Calendar.current.firstWeekday`), not hardcoded.

---

## Intermediate Level

### Changes Overview

- **Backend** (`internal/api/history.go`): the `/history?days=N` validation
  changed from an allowlist `{7, 14, 30}` to a numeric bounds check `1 ≤ N ≤ 31`,
  with the message updated to match. Non-numeric values still 400. The
  `startDate = now.AddDate(0,0,-(days-1))` arithmetic is unchanged.
- **FluxCore** (`DateFormatting.swift`): four pure, Sydney-calendar helpers —
  `startOfMonth(now:)`, `startOfWeek(now:firstWeekday:)`,
  `inclusiveDayCount(from:through:)`, and `windowStartDateString(inclusiveDays:now:)`.
- **App**:
  - `HistoryRange` enum (`.days(Int)`, `.weekToDate`, `.monthToDate`) with a
    `pickerLabel` and a `resolvedDays(now:firstWeekday:)` that delegates the
    boundary maths to the FluxCore helpers.
  - `HistoryViewModel.loadHistory(range:)` replaces `loadHistory(days:)`; it
    resolves the range to a day-count `N` at load time, stores `resolvedRangeDays`
    for the cards, and falls back to a date-bounded offline cache read.
  - `HistoryView` presents a five-segment segmented picker (7d/14d/30d/Wk/Mo).

### Implementation Approach

The selection model went from a bare `Int` to a `HistoryRange` enum that
**resolves to an `Int` day-count at load time**. Everything downstream of that
count — the chart axes, `DerivedState`/`PeriodSummary`, `ChartScope.historyRange(days:)` —
was already data-adaptive and needed no change. The app sends the existing
`?days=N` contract; only the backend's accepted range widened.

Range resolution reads two injected providers: `nowProvider` (already present)
and a new `firstWeekdayProvider` (defaulting to `Calendar.current.firstWeekday`).
Both are read on every load trigger (`.task`, `.onChange`, `.refreshable`, the
macOS refresh action, and Retry), so the window is recomputed against the current
Sydney "today" each time.

### Trade-offs

- **App resolves N vs. backend learns the range type** (Decision 5): the app
  computes the day-count and reuses `?days=N`, rather than teaching the backend a
  new range vocabulary. Smallest change; the only cost is that both sides compute
  the window, so a request crossing Sydney midnight can be off by one until the
  next load — the same race the fixed ranges already tolerate.
- **Date-bounded offline fallback for all ranges** (Decision 6): the offline
  cache read switched from "newest N by count" to "date ≥ window start, ascending."
  Applied uniformly to fixed and to-date ranges so there is one code path; this
  also fixed a latent bug where the offline auto-selected day was the oldest
  instead of the newest.

---

## Expert Level

### Technical Deep Dive

- **`startOfWeek` calendar isolation**: it mutates a **copy** of the shared
  `sydneyCalendar` to set `firstWeekday`, never the `static let`. Mutating the
  shared instance would corrupt every other date computation in the app. The
  locale supplies only the `firstWeekday` value; the weekday is evaluated against
  the Sydney date (AC 3.2).
- **DST-safe counting**: `inclusiveDayCount` normalises both ends to Sydney
  midnight and uses `dateComponents([.day])`, never elapsed-interval division, so
  23-hour and 25-hour DST transition days don't shift the count.
- **Window-start single-sourcing**: `windowStartDateString(inclusiveDays:now:)`
  computes `today-(N-1)` in Sydney and is the exact inverse of `inclusiveDayCount`
  (a property test asserts the round-trip for N ∈ {1,2,7,14,28,30,31}). It mirrors
  the backend's `startDate` formula so the offline cache bound equals the period
  start. For a to-date range, `today-(N-1)` is provably equal to the week/month
  start, so no day before the period start can surface offline (AC 4.3).
- **Mid-load coalescing**: `loadHistory(range:)` sets `lastRequestedRange` before
  the `isLoading` guard, then loops — after each `await load(...)` it re-checks
  `lastRequestedRange` and reloads if a newer selection arrived. On `@MainActor`,
  `lastRequestedRange` only changes at suspension points, so the loop cannot spin
  (each iteration does a real fetch) and the latest selection always wins, keeping
  the picker segment and rendered data in agreement.

### Architecture Impact

The wire contract, `ChartScope`, chart axes, and the derived-stat path are
untouched — the change is contained to the selection model and the offline
fallback. `resolvedRangeDays` is computed once at fetch time and read by every
card, satisfying the project's data-consistency rule (one metric, one source).
The expanded chart carries only the resolved `N`, so a chart left expanded across
Sydney midnight keeps fetching `N` days ending on the new today rather than
re-resolving the to-date window — an explicitly out-of-scope live-re-resolution
case that self-corrects when the chart is reopened.

### Potential Issues

- **Five-segment picker at large Dynamic Type** on the narrowest iPhones can
  truncate; the short "Wk"/"Mo" labels mitigate it and the segmented style is
  retained per AC 6.3. Accepted limitation, not verified at accessibility sizes.
- **First-frame / mid-load `resolvedRangeDays`**: briefly the cards read the
  previous/default count. Harmless because charts and summaries are data-adaptive;
  only the expansion scope's `N` is affected for that window.

---

## Completeness Assessment

### Fully implemented

- Week-to-date and month-to-date ranges (Req 1, 2), including single-day edges
  (on the 1st / on the week-start day → count 1).
- Sydney-calendar boundaries with locale first weekday, recomputed on every load
  trigger (Req 3).
- 1–31 day retrieval with no clipping, including 31-day month-to-date on the 31st
  (Req 4.1); date-bounded offline fallback (Req 4.3); existing error/empty states
  reused (Req 4.4).
- Downstream consistency: identical day set → identical `DerivedState`/
  `PeriodSummary`; resolved `N` flows to the card expansion scope (Req 5.1, 5.2).
- Five-segment control in order 7d/14d/30d/Wk/Mo, "Wk"/"Mo" labels, segmented
  style retained, default 7d (Req 6). Picker order and default are now locked by
  tests.

### Partially verified (correct in code, lighter test coverage)

- **Req 5.3 — expanded-chart axis scaling for non-{7,14,30} lengths**: the
  resolved `N` passthrough into `ChartScope.historyRange(days:)` is tested, and
  the axes derive their stride from `entries.count` (structurally adaptive), but
  no test renders the expanded chart for an odd-length range. This relies on the
  pre-existing adaptive rendering the design scoped as unchanged.

### Not in scope (per requirements Non-Goals / design)

- Persisting the selected range across launches; "last week"/"last month";
  quarter/year/all-time ranges; live re-resolution of an already-expanded chart
  across Sydney midnight; backend retention changes.

### Divergences from design

- `windowStartDateString` (the `today-(N-1)` window-start helper) was added to
  FluxCore during the pre-push review, replacing an inline computation that had
  lived in `HistoryViewModel`. The design's component list named only the other
  three helpers; this is an additive refactor that single-sources the window-start
  formula the app and backend must agree on. No behavioural change; covered in
  spirit by Decision 5's dual-computation rationale.
