# Lowest SoC Since Midnight (Today)

Transit ticket: **T-1084** (initial off-peak-end implementation, shipped).
Follow-up redirect ticket: TBD.

> Note: the spec folder is still named `low-since-offpeak` for history. The
> behaviour has been redirected — see Decision 4. Folder rename is out of
> scope for this change.

## Overview

The Lambda `/status` endpoint exposes the lowest battery SoC under the JSON
field `battery.low24h`. The value was originally a 24-hour rolling minimum,
then redefined (T-1084, shipped) as the lowest since the most recent
off-peak window end. This change redefines it again as **the lowest SoC
since 00:00 Sydney local time on `now`'s date** — i.e., "lowest today".
The JSON field name `low24h` and Go/Swift type name `Low24h` are unchanged;
only the underlying computation shifts.

## Requirements

- The system MUST compute `battery.low24h` from readings whose timestamp is
  at or after midnight (00:00) Sydney local time on the current day.
- The system MUST resolve "midnight today" using `Australia/Sydney`, not
  UTC, so the metric resets at the user's local midnight regardless of
  where the Lambda runs.
- The system MUST return `battery.low24h` as `null` when no readings exist
  in the resolved window (only expected briefly between midnight and the
  first reading of the new day).
- The system MUST NOT depend on off-peak configuration. Off-peak SSM
  parameters can be missing, malformed, or changed without affecting this
  field.
- The system MUST NOT change the JSON wire field name (`low24h`), the Go
  type name (`Low24h`), or the Swift type name (`Low24h`); only the
  underlying computation changes (Decision 2 still applies).
- The dashboard and large-widget UI label "Lowest" stays as-is — it remains
  accurate under the new semantics (no specific timeframe asserted in the
  label).

## Implementation Approach

**Backend (Go):**

- Replace the `lastOffpeakEnd` helper in `internal/api/compute.go:270` with
  `startOfDaySydney(now time.Time) time.Time`. One-line body:
  `local := now.In(sydneyTZ); return time.Date(local.Year(), local.Month(),
  local.Day(), 0, 0, 0, 0, sydneyTZ)`. No off-peak parsing, no second
  return value (cannot fail).
- In `internal/api/status.go:125-130`, drop the `lastOffpeakEnd` call and
  the off-peak field reads. Compute `start := startOfDaySydney(now).Unix()`,
  then `filterReadings(allReadings, start, nowUnix)`, then `MinSOC`.
  `battery.Low24h` stays nil only when `MinSOC` returns `found=false`.
- Update the `Low24h` doc comment at `internal/api/response.go:36` to
  describe the new "since midnight Sydney today" semantics.

**Tests (Go):**

- Replace `TestLastOffpeakEnd` in `internal/api/compute_test.go` with
  `TestStartOfDaySydney` covering: a Sydney 06:00 weekday → midnight same
  date; just past midnight Sydney → midnight same date; near 23:59
  Sydney → midnight same date; a UTC instant that lands on the next
  Sydney day → returns Sydney's tomorrow midnight (proves timezone
  conversion is right).
- Update `TestHandleStatusAllDataPresent` so its asserted `Low24h.Soc`
  reflects the lowest SoC at or after Sydney midnight on the test's `now`.
  The current pre-window SoC=15 fixture remains the regression guard:
  retime it (or another fixture point) to sit *before* the new midnight
  boundary so the filter must exclude it.
- Remove `TestHandleStatusLow24hUnparseableOffpeak` — there is no longer
  an off-peak failure mode for this field.
- Add `TestHandleStatusLow24hNoReadingsToday` confirming the field is nil
  when readings exist but all predate Sydney midnight.

**UI labels (Swift):**

- No changes. `"Lowest"` already reads correctly for "lowest today" at
  `Flux/Flux/Helpers/OffPeakBlock.swift:24` and
  `Flux/FluxWidgets/Views/SystemLargeView.swift:39`.

**Out of Scope:**

- Renaming the JSON field, Go struct, or Swift struct (`Low24h`). The name
  is now doubly stale (neither 24h nor since-off-peak). Per Decision 2 the
  legacy wire name is acceptable.
- Renaming the spec folder. Acknowledged misnomer; not worth the churn.
- Changing the Day Detail "24h low" row at `DayDetailView.swift` — it
  reads from the per-day `socLow` field on a separate pipeline and is
  independent of this change.
- Touching `nextOffpeakStart` (still used for cutoff suppression at
  `internal/api/status.go:107` and `:138`). Only `lastOffpeakEnd` and its
  test go.

## Risks and Assumptions

- Risk: `low24h` is `null` for the brief interval each day between Sydney
  midnight and the first reading after it. With 10s polling this is at
  most ~10s; the dashboard already renders `—` for nil. No mitigation
  required.
- Risk: a reader sees the field name `low24h` and assumes 24-hour rolling.
  Mitigation: the Go struct doc comment is updated to state the new
  semantics; per Decision 2 the wire rename remains out of scope.
- Assumption: the user's mental model of "today" is the Sydney calendar
  day. Matches the off-peak window's timezone and the Day Detail date
  selector.
- Assumption: DST transitions (02:00/03:00 Sydney) don't materially affect
  the metric — they're far from midnight, and `time.Date` against
  `sydneyTZ` produces a well-defined instant on either side.
- Assumption: the Lambda's reading query window (24h) trivially covers
  the new bound — at any moment Sydney midnight is at most ~24h behind
  `now`, regardless of the Lambda's own timezone.
