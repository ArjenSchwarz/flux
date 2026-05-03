# Low SoC Since Last Off-Peak End

Transit ticket: **T-1084**

## Overview

The Lambda `/status` endpoint currently exposes the lowest battery SoC over the
past 24 hours as `battery.low24h`. The 24-hour rolling window can surface stale
lows from before the most recent off-peak charge, which is misleading once the
battery has been topped up. This change keeps the JSON field name `low24h` but
redefines its window to "lowest SoC since the most recent off-peak window end",
so the value resets after each off-peak charge.

## Requirements

- The system MUST compute `battery.low24h` from readings whose timestamp is at
  or after the most recent off-peak window end (in Sydney local time).
- The system MUST resolve "most recent off-peak window end" as today's window
  end when `now` is at or after it, otherwise yesterday's window end at the
  same time-of-day. When `now` falls *inside* today's window, yesterday's end
  is the correct boundary (the value tracks the dip during the current
  charge period, then resets once today's window closes).
- The system MUST return `battery.low24h` as `null` when no readings exist in
  the resolved window, including the case where readings exist but all
  predate the boundary. This is a deliberate semantic change from today's
  "any reading in the past 24h" behavior.
- The system MUST return `battery.low24h` as `null` when the off-peak window
  configuration is unparseable.
- The system MUST NOT change the JSON wire field name (`low24h`), the Go type
  name (`Low24h`), or the Swift type name (`Low24h`); only the underlying
  computation changes.
- The system SHOULD update the dashboard and large widget UI labels from
  "24h low" to "Lowest" so the displayed text no longer pins the value to
  a 24-hour window.

## Implementation Approach

**Backend (Go):**

- Add a `lastOffpeakEnd(now, offpeakStart, offpeakEnd) (time.Time, bool)`
  helper in `internal/api/compute.go`, mirroring the structure of the existing
  `nextOffpeakStart` helper at `internal/api/compute.go:255`. Reuses
  `derivedstats.ParseOffpeakWindow` for HH:MM parsing. DST handling matches
  `nextOffpeakStart` (relies on `time.Date` + `AddDate` against `sydneyTZ`,
  same accepted behavior as the existing helper — no new DST handling).
  Acknowledged duplication: `nextOffpeakStart` and `lastOffpeakEnd` share
  parsing and tz logic; if off-peak config grows beyond a single daily
  window, fold both into a shared helper.
- In `internal/api/status.go:119-124`, replace the unconditional
  `derivedstats.MinSOC(toDerivedReadings(allReadings))` call with one that
  first filters via `filterReadings(allReadings, lastEnd.Unix(), nowUnix)`.
  When `lastOffpeakEnd` returns `false`, leave `battery.Low24h` nil.
- The Go struct `Low24h` and JSON tag `low24h` in `internal/api/response.go:32-39`
  stay unchanged. Update the doc comment to describe the new semantics.

**Tests (Go):**

- Add a table-driven test for `lastOffpeakEnd` in
  `internal/api/compute_test.go` next to the existing `nextOffpeakStart`
  test (covers: now before today's end → yesterday; now at/after today's end
  → today; invalid config → false).
- Update `TestHandleStatusAllDataPresent` in
  `internal/api/status_test.go:35-116`: the current test asserts
  `Low24h.Soc == 20` from a reading 24h before "now" (which is 06:00 AEST,
  before today's 14:00 off-peak end → last end is yesterday 14:00, and the
  20-SoC reading at ~04:53 yesterday lies before that boundary, so it's
  excluded). The assertion must change to the lowest SoC inside the new
  window. Either retime the old reading to lie inside the window or assert
  against the lowest of the remaining readings.
- Update or add a test confirming `low24h` is omitted when off-peak parsing
  fails.

**UI labels (Swift, no wire changes):**

- `Flux/Flux/Dashboard/SecondaryStatsView.swift:24` — change the label
  string `"24h low"` to `"Lowest"`.
- `Flux/FluxWidgets/Views/SystemLargeView.swift:39` — same label change.

**Out of Scope:**

- Renaming the JSON field, Go struct, or Swift struct (`Low24h`).
- Changing the Day Detail "24h low" row at `DayDetailView.swift:172-185`,
  which reads from a separate per-day `socLow` field, not `battery.low24h`.
- Backend-side fixtures, mocks, and tests for clients (e.g., `MockFluxAPIClient`,
  `WidgetFixtures`, `APIModelsTests`) — they reference the unchanged field
  name and stay valid.

## Risks and Assumptions

- Risk: the existing `TestHandleStatusAllDataPresent` asserts a SoC value
  from a reading that lies before the new boundary, so it breaks under the
  new semantics. Mitigation: the implementation task updates the fixture or
  assertion together with the code change.
- Assumption: the off-peak window is configured and parseable in production
  (currently `11:00`–`14:00` per CLAUDE.md). When it isn't, returning `null`
  is consistent with how the field is omitted today (no readings).
- Assumption: the Lambda's reading query window of 24 hours
  (`internal/api/status.go:38`) is sufficient — the most recent off-peak end
  is at most ~24h ago for any valid daily window, so filtering inside that
  window will always include readings if any exist.
