# Decision Log: History Month and Week to Date

## Decision 1: Use the full spec workflow

**Date**: 2026-06-03
**Status**: accepted

### Context

T-1361 adds "month to date" and "week to date" range options to the History screen. Scope assessment found the change spans two subsystems (the SwiftUI app and the Go backend), touches more than three files, and carries open design questions (how to express a variable-length range to the backend, and how to handle the locale-driven week start).

### Decision

Run the full spec workflow (requirements → design → tasks) rather than smolspec.

### Rationale

The feature crosses the app/backend boundary and the backend's fixed `validDays = {7, 14, 30}` allowlist must change to support variable-length ranges. Several smolspec exclusion criteria apply (multiple subsystems, >3 files, ambiguous approach), so the full workflow is appropriate.

### Alternatives Considered

- **Smolspec**: Lightweight single-document spec — Rejected because the cross-subsystem scope and unresolved backend approach exceed smolspec's criteria.

---

## Decision 2: Sydney calendar for boundaries, device locale for week start

**Date**: 2026-06-03
**Status**: accepted

### Context

The stored daily data is keyed by Sydney-local dates ("YYYY-MM-DD"), and the backend computes "today" in the Sydney timezone. The ticket states the week must start on "the system-defined week start day (per the app, not the backend)."

### Decision

Compute "today", the month start, and the week start against the Sydney-based calendar so the selected day set matches the stored data. Derive only the week's first weekday from the device locale (`Calendar.current.firstWeekday`).

### Rationale

Using a non-Sydney calendar for day boundaries would produce day sets that do not align with the Sydney-keyed data, causing off-by-one errors near midnight and at month/week edges. The single locale-dependent aspect the ticket calls out is which weekday a week begins on, which is independent of the timezone used for the boundary arithmetic.

### Alternatives Considered

- **Fully device-local calendar**: Use the device timezone for all boundary math — Rejected because day boundaries would diverge from the Sydney-keyed data.
- **Hardcode week start (e.g. Monday)**: Simpler — Rejected because the ticket explicitly requires the app/locale-defined start day.

---

## Decision 3: Five-segment range control with "Wk" / "Mo" labels

**Date**: 2026-06-03
**Status**: accepted

### Context

The existing range control is a 3-segment segmented `Picker` (7d / 14d / 30d). The two new ranges need a place in the UI without losing the existing fixed ranges.

### Decision

Keep all three fixed ranges and append two segments, giving a five-option control in the order 7d / 14d / 30d / Wk / Mo. The default selection remains 7d.

### Rationale

Appending preserves existing behaviour and muscle memory; no fixed range is removed. Short labels ("Wk", "Mo") keep the five segments legible on iPhone width and read more plainly than the "WTD/MTD" jargon alternative.

### Alternatives Considered

- **Switch to a Menu/dropdown**: Scales to more options and frees horizontal space — Rejected (for now) because it adds a tap and changes the established interaction.
- **Replace 14d & 30d with Week & Month**: Keeps three segments — Rejected because it removes existing fixed ranges users rely on.
- **"WTD" / "MTD" labels**: Width-matches "30d" — Rejected as jargon-heavy versus plain "Wk"/"Mo".

---

## Decision 4: Defer range-transport mechanism to the design phase

**Date**: 2026-06-04
**Status**: accepted

### Context

The requirements reviews (design-critic + external peer validation) converged on one central question that is fundamentally a design choice: how a variable-length to-date range is expressed to the backend. Today the range is a bare `?days=N` integer that the Lambda re-anchors with `startDate = today-(N-1)` in Sydney time, against a `validDays={7,14,30}` allowlist. Wk/Mo need 1–31 days, and the selection (currently a bare `Int`, also serialized as `ChartScope.historyRange(days:)`) must carry enough information to scale charts and the expanded-chart view.

### Decision

Keep the requirements behaviour-focused (retrieve 1–31 days without clipping; boundaries computed against the Sydney calendar; charts scale to the actual length) and defer the mechanism to the design phase. Design will choose between (a) the app computing N and widening the backend allowlist to 1–31, versus (b) the backend learning the range type and computing both boundaries itself, and will decide how the selection model changes from `Int` to a range representation.

### Rationale

Requirements describe observable behaviour, not implementation. Both transport options can satisfy the same acceptance criteria; choosing between them depends on the double-computation / device-clock-skew trade-off (option a) versus a richer backend contract (option b), which is a design concern. Recording the deferral keeps the open question visible for the design phase.

### Alternatives Considered

- **Decide the mechanism now in requirements**: Rejected — it would prescribe implementation in a requirements document and pre-empt the design analysis.

### Consequences

**Positive:**
- Requirements stay testable and implementation-agnostic.
- The key design trade-off is recorded rather than lost.

**Negative:**
- The design phase must resolve the transport and selection-model question before tasks can be written.

---

## Decision 5: App resolves the range to a day-count; backend widens its allowlist

**Date**: 2026-06-04
**Status**: accepted

### Context

Decision 4 deferred how a variable-length to-date range reaches the backend. The existing contract is `GET /history?days=N`, with the Lambda re-deriving `startDate = today-(N-1)` in Sydney time against a `validDays={7,14,30}` allowlist. The selection is currently a bare `Int` that also feeds `ChartScope.historyRange(days:)`.

### Decision

The app resolves a new `HistoryRange` enum (`.days(7|14|30)`, `.weekToDate`, `.monthToDate`) to an inclusive day-count `N` (1–31) at load time — computed against the Sydney calendar, with the week's first weekday from `Calendar.current.firstWeekday` — and sends the existing `?days=N`. The backend's only change is replacing the allowlist with a `1 ≤ days ≤ 31` bounds check and matching error message.

### Rationale

This is the smallest change that satisfies the requirements: the wire contract, `ChartScope`, the chart axes, and `DerivedState`/`PeriodSummary` are all unchanged because they already adapt to the actual data and the resolved count. Both app and Lambda compute the window from the Sydney "today", so they agree except in the same seconds-wide midnight race the fixed ranges already tolerate. Because the boundary count is computed with the Sydney calendar (not the device calendar), the device timezone cannot shift the window; only the locale's first weekday is consulted.

### Alternatives Considered

- **App sends the start date (`?from=YYYY-MM-DD`)**: Single source of truth for the boundary, eliminating the double-computation race — Rejected as a larger backend contract change (new param, span/future-date validation, divergent code path) for a race that is already accepted for fixed ranges.

### Consequences

**Positive:**
- Minimal backend change; downstream app code largely untouched.
- Locale affects only the week-start weekday, not the date arithmetic.

**Negative:**
- The window boundary is computed on both sides; a request crossing Sydney midnight can be off by one until the next load (same as existing fixed ranges).

---

## Decision 6: Date-bound the offline cache fallback for all ranges

**Date**: 2026-06-04
**Status**: accepted

### Context

The offline fallback `loadCachedDays(limit: requestedDays)` returns the newest N cached days by count. For a to-date range this can surface days from before the week/month start when the cache has gaps ([4.3](requirements.md)).

### Decision

Change the fallback to fetch cached days with `date >= startDate` (where `startDate = today-(N-1)`), returned in the same ascending order as the online response. Apply this uniformly to fixed and to-date ranges rather than maintaining two paths.

### Rationale

Date-bounding is the only way to guarantee no pre-boundary day appears, and applying it uniformly keeps a single code path. For fixed ranges it is a strict correctness improvement: newest-N-by-count could previously cross the intended window on a gappy cache, whereas date-bounding matches what the online path returns.

### Alternatives Considered

- **Keep count-based for fixed ranges, add date-bound only for to-date**: Smaller diff — Rejected because two divergent fallback behaviours are harder to reason about and the unified path is also more correct for fixed ranges.

### Consequences

**Positive:**
- No day before the period start is ever shown offline; online/offline shapes match.

**Negative:**
- Fixed-range offline behaviour changes on gappy caches (returns the windowed days, not the most recent N) and the offline auto-selected day flips from oldest to newest — a latent-bug fix, since the current descending sort disagrees with the ascending online path.
- Two existing tests (`loadHistoryFallsBackToCacheWhenNetworkFails`, `cacheFallbackPathRendersNotes`) hardcode an out-of-window cache date against the real clock and WILL break; they must be rewritten to inject `nowProvider` with dates relative to the injected now. This is required task work, not optional.

---
