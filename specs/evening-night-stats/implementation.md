# Implementation: Evening / Night Stats

Explains the shipped implementation at three levels of expertise. Use the
beginner pass for orientation, the intermediate pass to understand the
moving parts, and the expert pass when extending or auditing the code.

## Beginner — what does this feature do?

The Day Detail screen in the iOS app already shows charts and a list of
"peak usage" periods for any selected day. We added a new card to that
screen that surfaces how much electricity the household used while solar
was *not* generating: split into an **Evening** block (from the last solar
reading of the day until midnight) and a **Night** block (from midnight
until the first solar reading of the day).

For each block we display:

- the local clock-time range (e.g. `18:30 – 24:00`)
- total kWh consumed during that window
- average kWh per hour over the window
- a small caption when the block is still in progress (`(so far)`) or
  when the boundary was estimated from a sunrise/sunset table because
  no solar reading exists for that day (`≈ sunset` / `≈ sunrise`)

The numbers are computed by the Lambda backend and shipped as a new
`eveningNight` field on the existing `/day` JSON response. The iOS app
just decodes the field and renders the card — no new client-side maths.

## Intermediate — the architecture

### Backend

`internal/api/day.go:handleDay` already pulls a slice of `flux-readings`
rows for the requested date. After the existing `findPeakPeriods` call we
now also call `findEveningNight(readings, date, today, now)` and assign
the result to `resp.EveningNight`. When the function returns nil (no
readings, or only a daily-power fallback path) the field is omitted via
`omitempty` so older clients are unaffected.

`findEveningNight` (in `internal/api/compute.go`) does three things:

1. Walks the readings once to find the first and last reading where
   `Ppv > 0`. Those become the night-block end and evening-block start
   respectively.
2. Falls back per block to a Melbourne sunrise/sunset lookup when no
   `Ppv > 0` reading exists for that side of the day. The fallback table
   lives in `internal/api/melbourne_sun_table.go` (366 entries, generated
   once, DST-immune via `time.ParseInLocation`).
3. For each block, calls `integratePload` to compute `totalKwh` over the
   half-open `[start, end)` interval using trapezoidal integration of
   `pload`, with the same 60-second pair-gap rule used by the existing
   `computeTodayEnergy`. Period boundaries that fall between two readings
   are linearly interpolated (after clamping negative `pload` to zero).

Today's request gets two extra rules:

- The evening block is omitted entirely while `now <=` the computed
  sunset, so a midday request doesn't show a tiny 5-minute "evening"
  starting at the most recent reading.
- Both blocks clamp their `end` to `now` and report
  `status = "in-progress"` when their nominal end is in the future.

Status and boundary-source values are exposed as constants
(`EveningNightStatusComplete`, `EveningNightStatusInProgress`,
`EveningNightBoundaryReadings`, `EveningNightBoundaryEstimated`) in
`internal/api/response.go`, mirroring the existing `OffpeakStatus*`
convention.

### iOS

`Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift` adds two
public types:

- `EveningNight` with optional `evening` / `night` blocks and a
  `hasAnyBlock` helper.
- `EveningNightBlock` with `start`, `end`, `totalKwh`,
  `averageKwhPerHour?`, and typed `Status` / `BoundarySource` enums (the
  raw strings from the JSON map onto enum cases via `Codable`).

`DayDetailResponse` grew one optional field, `eveningNight`, so its
synthesised initialiser now requires that argument at every call site —
all such literal sites in the test target were updated to pass `nil`.

`Flux/Flux/DayDetail/DayDetailViewModel.swift` adds a
`private(set) var eveningNight: EveningNight?` property. `loadDay()` sets
it from the response; the error path resets it to nil.

`Flux/Flux/DayDetail/EveningNightCard.swift` is the new SwiftUI view. It
matches `PeakUsageCard`'s `.thinMaterial` background and corner radius,
and renders each block as a two-line row (label + time-range on line 1,
secondary caption + totals on line 2). Caption rules:

- in-progress → `(so far)` (boundary caption suppressed because the
  visible end is `now`, not the nominal sunrise/sunset)
- estimated boundary on a complete block → `≈ sunset` (evening row) or
  `≈ sunrise` (night row)
- otherwise → empty

`DayDetailView` adds a guarded render between the existing
`PeakUsageCard` and the `summaryCard`:

```swift
if viewModel.hasPowerData,
   let eveningNight = viewModel.eveningNight,
   eveningNight.hasAnyBlock {
    EveningNightCard(eveningNight: eveningNight)
}
```

## Expert — algorithm specifics, gotchas, decisions

### Per-block fallback semantics

Requirement 1.5 demands that fallback be applied *per block*, not as a
whole-day flag. A past day with morning-only readings (recorder dies at
noon) still has a `lastPpvPositive` at ~12:55, so the evening block uses
that as `nominalStart` rather than falling back to the sunset table.
This is the spec'd behaviour (Decision 2 in `decision_log.md`) — accept
the noise risk in exchange for sample-derived boundaries when any
positive reading exists. The card simply renders the long span (`12:55
→ 24:00`) with whatever load was integrated.

### Today-gate ordering

The today-gate (`now <= computedSunset → omit evening`) deliberately
fires *before* the readings-vs-fallback selection, so a midday
`lastPpvPositive` cannot leak into the response as an evening block.
Past dates are not gated; they always emit the evening block when
`nominalStart < dayEnd`.

### `integratePload` edge synthesis

The function turns a sorted readings slice plus a half-open interval
`[startUnix, endUnix)` into an unrounded kWh value. Three subtleties:

1. **Bracket-pair gap rule.** A bracketing pair is the reading just
   before the boundary plus the reading at-or-after the boundary. If
   that pair's gap is greater than `maxPairGapSeconds` (60s), the
   synthetic boundary point is *not* emitted; the integration starts at
   the first interior reading instead. This matches what
   `computeTodayEnergy` does at the per-pair level and prevents phantom
   accumulation across long polling outages.
2. **Clamp before interpolation.** `max(pload, 0)` is applied to each
   bracket reading's `pload` *before* the linear interpolation at the
   boundary. Doing this in the other order (interpolate then clamp)
   would bias the value upward when one side of the bracket pair is a
   large negative spike.
3. **Half-open semantics.** Readings exactly at `startUnix` are interior
   (included); readings exactly at `endUnix` are excluded. The left-edge
   synthesis is suppressed when `readings[iL+1].Timestamp == startUnix`
   because that reading is already an interior point and synthesising at
   `startUnix` would duplicate it.

The worked example from `design.md` (`(t=0,200), (t=10,400),
(t=20,-100), (t=30,600)` over `[15, 25)`, total ≈ 0.000347 kWh) is one
of the table cases in `compute_test.go:TestIntegratePload`.

### Sunrise/sunset table

The table stores **wall-clock `HH:MM` strings interpreted in the
`Australia/Sydney` zone**, not raw UTC offsets. `time.ParseInLocation`
applies AEST or AEDT for the requested calendar date, so the table is
DST-immune by construction — future Australian DST rule changes affect
only the IANA database that ships with Go. Feb 29 is intentionally
absent; the lookup falls back to Feb 28's values, which is well inside
the documented ±2-minute tolerance.

### Sunset is resolved at most once per request

`findEveningNight` may need the computed sunset both for the today-gate
(before deciding whether to emit the evening block) and for the evening
block's fallback path (when no `lastPpvPositive` exists). A small closure
caches the result so we don't pay the table lookup + `time.ParseInLocation`
cost twice on the today-path.

## Completeness Assessment

**Fully implemented:**
- All requirements 1.1–1.13 (backend computation and API contract)
- 2.1–2.3 (additive JSON field, omitempty semantics)
- 3.1–3.7 (iOS card rendering, visibility guards)
- 4.1 (nine backend scenarios)
- 4.2 (Melbourne sunrise/sunset sanity tests — solstices, leap-year,
  DST-transition)
- 4.3 (five iOS view-model scenarios) plus extended decode coverage on
  `APIModelsTests`

**Intentional non-goals (per requirements):**
- No Dashboard or History screen changes.
- No new DynamoDB tables / GSIs / queries.
- No predictive/forecasting logic.
- No site-coordinate configuration UI.
- No civil-twilight or atmospheric-refraction modelling.

**Notes on req 3.3 (in-progress styling).** The spec asks the in-progress
indicator to mirror "the existing offpeak pending indicator on the
Dashboard." The Dashboard does not currently render a visible `pending`
badge — it just surfaces the deltas — so the implementation chose a
secondary-text `(so far)` caption that matches the design.md caption
rules. If a Dashboard-level pending visual is added later, the card
should be updated to follow it.
