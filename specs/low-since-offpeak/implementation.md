# Implementation: Low SoC Since Last Off-Peak End (T-1084)

Branch: `chore/low-since-offpeak-T-1084` (single commit `e2ad373`).

Linked: [smolspec.md](smolspec.md), [decision_log.md](decision_log.md), [tasks.md](tasks.md).

## Beginner Level

### What Changed

The Dashboard used to show a stat called "24h low" — the lowest battery
percentage the system had seen in the past 24 hours. The number was
computed by the cloud backend and shown by both the iPhone/iPad/Mac app and
the home-screen widget.

After this change, that same number is computed differently: it's the
lowest battery percentage seen since the most recent off-peak charging
window finished (in Sydney). The label on the screen now just says
"Lowest" instead of "24h low".

### Why It Matters

The home battery charges from cheap grid power during a fixed window each
afternoon (currently 11:00–14:00 in Sydney). After that window closes, the
battery is full. As the day rolls on it discharges, sometimes deeply.

The old "24h low" number was a rolling 24-hour memory. That meant the
morning after a low-battery night, the dashboard kept showing "your low
was 15%" even after the lunchtime window had topped the battery up to 95%.
The number stayed stale until 24 hours had passed.

The new behaviour resets the number after each charging window closes, so
"Lowest" always means "lowest since you were last full".

### Key Concepts

- **Off-peak window**: a fixed time-of-day range (today: 11:00–14:00
  Sydney) when grid power is cheap and the battery is allowed to charge
  from the grid.
- **SoC**: state of charge — the battery's percentage full.
- **Stale value**: a number that's correct historically but no longer
  represents what the user cares about right now.

---

## Intermediate Level

### Changes Overview

Backend (Go), 7 files (5 logic/test + 2 doc):

- `internal/api/compute.go` — adds `lastOffpeakEnd(now, offpeakStart, offpeakEnd) (time.Time, bool)`. Mirrors the existing `nextOffpeakStart` directly above it. Reuses `derivedstats.ParseOffpeakWindow` for the `"HH:MM"` parsing and `sydneyTZ` for the timezone.
- `internal/api/status.go` — `handleStatus` now resolves the lower bound, filters `allReadings` to that bound, and only then calls `MinSOC`. The output struct (`Low24h`) and JSON tag (`low24h`) are unchanged.
- `internal/api/response.go` — doc comments updated to describe the new semantics; type definitions unchanged.
- `internal/api/compute_test.go` — new `TestLastOffpeakEnd` covering before/inside/at/after window plus invalid config (5 cases).
- `internal/api/status_test.go` — `TestHandleStatusAllDataPresent` fixture grows by one reading: a pre-window SoC=15 entry that the new filter must exclude, leaving the in-window SoC=20 entry as the minimum. New `TestHandleStatusLow24hUnparseableOffpeak` asserts the field is nil when the off-peak config can't be parsed even though readings exist.
- `docs/agent-notes/api-layer.md`, `docs/agent-notes/ios-app-views.md`, `CHANGELOG.md` — narrative updates.

Frontend (Swift), 2 files:

- `Flux/Flux/Dashboard/SecondaryStatsView.swift:24` — `statRow` title `"24h low"` → `"Lowest"`.
- `Flux/FluxWidgets/Views/SystemLargeView.swift:39` — same.

No Swift model, mock, fixture, or test file is touched. The wire format is byte-identical so client decoding is unchanged.

### Implementation Approach

`lastOffpeakEnd` is structurally a mirror of `nextOffpeakStart`:

```go
func lastOffpeakEnd(now time.Time, offpeakStart, offpeakEnd string) (time.Time, bool) {
    _, endMin, ok := derivedstats.ParseOffpeakWindow(offpeakStart, offpeakEnd)
    if !ok { return time.Time{}, false }
    local := now.In(sydneyTZ)
    dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, sydneyTZ)
    todayEnd := dayStart.Add(time.Duration(endMin) * time.Minute)
    if local.Before(todayEnd) {
        return todayEnd.AddDate(0, 0, -1), true
    }
    return todayEnd, true
}
```

Boundary semantic: `local.Before(todayEnd)` is `<` (strict), so when `now`
exactly equals `todayEnd` the function returns today's end (the "fresh"
side). When `now < todayEnd` — including when `now` is inside today's
window — yesterday's end is returned. This is the documented intent
(Decision 1): inside the window the value should still track the dip
since yesterday's close, then reset cleanly when today's window ends.

Status handler integration:

```go
if lastOpEnd, ok := lastOffpeakEnd(now, h.offpeakStart, h.offpeakEnd); ok {
    sinceReadings := filterReadings(allReadings, lastOpEnd.Unix(), nowUnix)
    if soc, ts, found := derivedstats.MinSOC(toDerivedReadings(sinceReadings)); found {
        battery.Low24h = &Low24h{ Soc: roundPower(soc), Timestamp: ... }
    }
}
```

Two distinct nil paths fall out of this naturally:

- The off-peak config is unparseable → outer `ok` is false → field stays nil.
- The config is fine but no readings sit at or after `lastOpEnd` → `MinSOC` returns `found=false` → field stays nil.

Both are deliberate per smolspec.md.

### Trade-offs

- **Field rename declined.** The smolspec considered renaming `low24h` to
  `lowSinceOffpeak` across JSON, Go, and Swift (~14 files of mechanical
  churn). The user preferred the smaller diff and accepted the legacy
  name. The Go struct doc comment now states the actual semantics so
  reviewers aren't misled by the name alone.
- **Two helpers for one window.** `lastOffpeakEnd` and `nextOffpeakStart`
  share parsing and timezone arithmetic. A combined helper (e.g.
  `windowEndpoint(now, minute)`) was considered but would still need
  per-caller branching for "before/after" and "+1d/-1d", so the
  combined version doesn't actually reduce conceptual weight. Left
  duplicated; flagged for revisit if off-peak config grows beyond a
  single daily window.
- **DST not addressed.** `time.Date + AddDate` against `sydneyTZ` has the
  same DST-day quirks as the existing `nextOffpeakStart`. Decision 3
  records that matching the existing helper is preferred over fixing
  both in this change.

---

## Expert Level

### Technical Deep Dive

**Boundary algebra.** Given off-peak window `[startMin, endMin)` and `now` in Sydney local:

```
todayEnd       = midnight(now) + endMin minutes
lastOffpeakEnd = todayEnd                  if now >= todayEnd
                 todayEnd - 1 day          otherwise
```

`midnight(now)` is `time.Date(...0,0,0,0, sydneyTZ)`, which on a DST-spring-forward day yields the nominal local midnight even though the wall clock skips an hour. The downstream `Add(endMin minutes)` is wall-clock arithmetic — so on a spring-forward day where 14:00 still exists, the value is correct; on the (notional) day a transition lands inside the window, the result is the first concrete instant matching the wall-clock time, which is acceptable for this stat. Australia/Sydney transitions never land near 14:00 (they happen at 02:00/03:00), so this is a theoretical concern only.

**Allocation profile.** The hot path was:

```
toDerivedReadings(allReadings)   -- 1 alloc, ~8640 entries at 10s polling, 7 fields each
```

It is now:

```
filterReadings(allReadings, ...) -- 1 alloc, subset (0..8640), 10 fields each (ReadingItem)
toDerivedReadings(subset)        -- 1 alloc, same subset, 7 fields each
MinSOC(subset)                   -- O(n), no alloc
```

Two allocations replace one, but each is over a strictly smaller slice (the new bound is at most 24h ago, often much less). Net memory traffic is roughly equal in the worst case (just past midnight, before today's window) and lower otherwise. `lastOffpeakEnd` itself is allocation-free: `ParseOffpeakWindow` does fixed-length byte arithmetic on two 5-character strings, no `time.Parse`, no regex.

**Test fixture as oracle.** The pre-window SoC=15 reading in `TestHandleStatusAllDataPresent` is the regression guard: if the filter is removed or the boundary is computed incorrectly to extend further back, the assertion `Low24h.Soc == 20` fails because `MinSOC` would return 15. The fixture fails closed.

**Failure-mode separation in `TestHandleStatusLow24hUnparseableOffpeak`.** It exists specifically because the "no readings in window" path and the "no parseable window" path both produce a nil result via different code paths. A single test couldn't distinguish between them; an implementation that always returned nil would still pass the all-data test as long as the assertion changed.

### Architecture Impact

- **Cutoff suppression unchanged.** The status handler still uses `nextOffpeakStart` for cutoff-time suppression at `status.go:107` and the rolling-15min cutoff at `:138`. The new helper sits beside it; the two read off the same `derivedstats.ParseOffpeakWindow`, so any future change to off-peak config representation needs to update the parser and both call sites.
- **Wire compatibility.** Old `low24h` values from any cached client response (e.g. widgets persisting `StatusSnapshotEnvelope` to App Group UserDefaults) decode identically. The semantic change is invisible at the protocol level; only the displayed *value* will differ across the deploy boundary. The Lambda is deployed independently of the iOS/macOS app; an updated lambda paired with an old app version simply shows a number with the new meaning under the old "24h low" label, until the app is updated.
- **Day Detail unaffected.** `DayDetailView.swift:172-185`'s "24h low" row reads `viewModel.summary?.socLow`, which is the per-day stat populated by the daily-derived-stats poller (a different pipeline at `internal/derivedstats/socmin.go` invoked by the poller's hourly summary). That label and that value are deliberately untouched — they describe a single calendar day, not the rolling stat on the dashboard.

### Potential Issues

- **Brief nil after the window closes on a fresh start.** Right after the lambda's reading query window first contains a "today's off-peak end" boundary (e.g., immediately after redeploy with stale DynamoDB), there can be a momentary case where readings exist but none are after the new boundary. The field will be nil. The dashboard renders `—`. Self-correcting on the next reading.
- **DST oddities** twice a year, low-impact (Sydney DST transitions are at 02:00/03:00, far from the 14:00 boundary).
- **Future scope drift.** If off-peak config grows to multiple windows or per-day variation, the duplication between `lastOffpeakEnd` and `nextOffpeakStart` becomes a real maintenance hazard. Decision 3 notes the intent to refactor into a shared helper at that point.

---

## Validation Findings

Cross-checked the code against the smolspec requirements, the decision log, and the test fixtures.

### Gaps Identified

None blocking. The Go struct comment in `response.go` could mention the
"lowest since last off-peak end" phrasing more verbosely, but the current
two-line doc captures the essentials.

### Logic Issues

None. Boundary algebra is consistent across `compute.go`, the helper test,
and the in-handler call site. The two nil paths are intentionally distinct
and individually exercised.

### Questions Raised

- The intermediate-level explanation surfaced a hidden coupling: any
  change to the off-peak config representation now needs to update the
  parser and both call sites (`nextOffpeakStart`, `lastOffpeakEnd`).
  Decision 3 acknowledges this; no action this round.

### Recommendations

- If a follow-up adds multi-window off-peak support, fold both helpers
  into one — the cost of two near-duplicates is acceptable today only
  because the config is a single daily window.

---

## Completeness Assessment

| Spec Requirement | Status | Evidence |
| --- | --- | --- |
| Compute `low24h` from readings since last off-peak end | Fully implemented | `internal/api/status.go` filter call wraps `MinSOC`. |
| Boundary resolution: today's end if `now ≥ todayEnd`, else yesterday's, including inside-window | Fully implemented | `internal/api/compute.go` `lastOffpeakEnd`; `TestLastOffpeakEnd` covers all 4 boundary positions. |
| `null` when no readings exist in the resolved window | Fully implemented | `MinSOC` returns `found=false`; `TestHandleStatusAllDataPresent` proves filter excludes pre-boundary SoC=15. |
| `null` when off-peak window config is unparseable | Fully implemented | Outer `ok` from `lastOffpeakEnd`; `TestHandleStatusLow24hUnparseableOffpeak` asserts. |
| Preserve JSON wire field, Go type, Swift type names | Fully implemented | `internal/api/response.go`, Swift `APIModels.swift`, all client mocks/fixtures untouched. |
| UI labels reflect new semantics ("Lowest") | Fully implemented | `SecondaryStatsView.swift:24`, `SystemLargeView.swift:39`. |
| Out-of-scope items not touched | Fully implemented | No rename of JSON tag; `DayDetailView.swift:172` "24h low" row left alone (separate `socLow` field); client mocks/fixtures (`MockFluxAPIClient`, `WidgetFixtures`, `APIModelsTests`) unchanged. |
| Documentation updated to reflect new semantics | Fully implemented | `docs/agent-notes/api-layer.md`, `docs/agent-notes/ios-app-views.md`, `CHANGELOG.md` Unreleased > Changed entry. |

No partial or missing items.
