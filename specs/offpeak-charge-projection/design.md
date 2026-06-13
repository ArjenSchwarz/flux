# Design: Off-peak Charge Projection

## Overview

Add a server-computed projected SoC at the off-peak window end to `/status`, and render it as a single contextual row on the Dashboard's battery panel. The projection uses an idealised two-rate charge curve and is gated identically to the existing cutoff estimate.

## Architecture

**Compute site.** A new pure function `projectOffpeakEndSoc` in `internal/api/compute.go`, alongside `computeCutoffTime`. It is called once in `internal/api/status.go` immediately after `resp.Offpeak = buildOffpeak(...)` (status.go:245), on the fresh-live branch, reusing the `capacity` variable already computed for `computeCutoffTime` (status.go:125) — this is what makes [1.4](requirements.md#1.4) hold by construction.

```go
if liveFresh {
    latest := allReadings[len(allReadings)-1]
    // Reuse `capacity` (status.go:125) for AC 1.4 parity with EstimatedCutoff —
    // do not introduce a second capacity lookup here.
    if p := projectOffpeakEndSoc(latest.Soc, capacity, now, h.offpeakStart, h.offpeakEnd); p != nil {
        resp.Offpeak.ProjectedEndSoc = p
    }
}
```

`resp.Offpeak` is always non-nil (`buildOffpeak` always returns window times), so the assignment is safe. The function never reads `simLoadW` or `Pbat`, so [1.9](requirements.md#1.9) and [2.4](requirements.md#2.4) hold by construction — no suppression branch needed (contrast `CantEmptyBeforeOffpeak`, which is suppressed under simulation).

**Field placement.** On `OffpeakData`, not `BatteryInfo`. The value is meaningful only during the window and the Dashboard labels it with `windowEnd`; co-locating value and label in one sub-object makes [4.2](requirements.md#4.2) a same-object read rather than a cross-field correlation. Kept out of `buildOffpeak` (which only knows energy deltas, not SoC/capacity/freshness) to avoid widening that function's inputs.

**Gating.** `projectOffpeakEndSoc` returns nil when: window unparseable, `now` outside `[start, end)` (`withinOffpeakWindow`), or `capacity <= 0`. The caller's `liveFresh` guard covers stale/no-reading ([2.2](requirements.md#2.2)). A nil pointer with no `omitempty` serialises as JSON `null` ([3.2](requirements.md#3.2)), matching `EstimatedCutoff`.

### Pattern-extension parity audit (BatteryBlock call sites)

| Call site | Init form | Needs projected params? |
|---|---|---|
| `DashboardView.batteryPanel` | memberwise | **Yes** — pass `projectedOffpeakEndSoc` + `offpeakWindowEnd` |
| `DayDetailView.batteryBlock` | memberwise | No — projection is live-only; params default `nil`, row hidden |
| `#Preview` | memberwise | No (optional) |

`BatteryBlock.init(day:)` is a convenience init with no live callers (History renders battery data via a Chart in `HistoryBatteryCard.swift`, not `BatteryBlock`); it needs no change. Locate sites by symbol, not line number. Day Detail calls the memberwise init directly; the two new params defaulting to `nil` keep its off-peak row hidden as intended. `OffpeakData(...)` constructor sites (MockFluxAPIClient, OffPeakBlock, WidgetFixtures) need no change: the new initializer parameter defaults to `nil`.

## Components and Interfaces

### Go — constants and computation (`internal/api/compute.go`)

```go
const (
    offpeakChargeRateKW  = 4.5  // grid charge rate while SoC < fastChargeMaxSoc
    offpeakTrickleRateKW = 0.5  // 500 W from fastChargeMaxSoc to 100%
    fastChargeMaxSoc     = 95.0 // percent; tie-break is >= -> trickle rate
)

// projectOffpeakEndSoc projects SoC (percent) at the off-peak window end using
// the idealised two-rate curve. Returns nil when outside the window, capacity
// is non-positive, or the window is unparseable. Result clamped to [soc, 100],
// rounded to 1 dp. Independent of Pbat and simLoadW by construction.
func projectOffpeakEndSoc(soc, capacityKwh float64, now time.Time, offpeakStart, offpeakEnd string) *float64
```

Closed-form curve. Let `r(kW) = kW / capacityKwh * 100` (SoC %/hour) and `h` = hours from `now` to today's window end:

- `soc >= 100` → `100`
- `soc >= 95` → `soc + r(0.5)·h`
- `soc < 95`:
  - `hoursTo95 = (95 − soc) / r(4.5)`
  - `hoursTo95 >= h` → `soc + r(4.5)·h` (stays below 95)
  - else → `95 + r(0.5)·(h − hoursTo95)`

then clamp to `[soc, 100]` and round (`roundPower`). The window-end instant is today's date in `sydneyTZ` plus `endMin` (same construction as `nextOffpeakStart`, compute.go:439-441). `withinOffpeakWindow` gates on minute-of-day while `h` is computed from seconds-precise `now`; near the boundary (e.g. 13:59:30) the gate is still true and `h` is a sub-minute positive value, absorbed by the `[soc, 100]` clamp (result ≈ current SoC). The two never disagree: at 14:00:30 the minute gate is already false.

### Swift — wire model (`FluxCore/Models/APIModels.swift`)

Add to `OffpeakData`:
```swift
public let projectedEndSoc: Double?   // percent; nil when not in window / stale
```
and a trailing `projectedEndSoc: Double? = nil` initializer parameter (default preserves existing call sites).

### Swift — display (`BatteryBlock.swift`)

`BatteryBlock` and `FluxStatRow` are app-target views (`Flux/Flux/Helpers/`) compiled into both the iOS and macOS builds via target membership — they are not in FluxCore. Only the `OffpeakData` model lives in FluxCore. (Requirements AC 4.4 says "shared FluxCore views"; the sharing mechanism is target membership, and the wording there is imprecise.)

Add two parameters:
```swift
var projectedOffpeakEndSoc: Double?
var offpeakWindowEnd: String?
```

The projection row **takes precedence** over the "Charged during off-peak" delta row — they are mutually exclusive in the layout, so the panel never shows two off-peak rows:

```swift
if let projected = projectedOffpeakEndSoc {
    FluxStatRow(label: projectedLabel, value: SOCFormatting.format(projected), last: true)
} else if rendersOffpeakDelta {
    FluxStatRow(label: "Charged during off-peak", value: offpeakDeltaText, last: true)
}
```
`projectedLabel` = `"Projected at \(offpeakWindowEnd ?? "off-peak end")"`. The "Lowest" row's `last:` becomes `!(projectedOffpeakEndSoc != nil || rendersOffpeakDelta)`. `SOCFormatting.format` already renders `>= 99.95` as `"100%"` and otherwise `"%.1f%%"`, matching the server's 1-dp rounding.

Row behaviour by phase: during window → "Projected at 14:00 / 97.5%"; after window complete → "Charged during off-peak / +42%"; before window → "Charged during off-peak / —" (unchanged from today).

### Dashboard wiring (`DashboardView.swift:256`)

Add to the `batteryPanel` `BatteryBlock(...)`:
```swift
projectedOffpeakEndSoc: viewModel.status?.offpeak?.projectedEndSoc,
offpeakWindowEnd: viewModel.status?.offpeak?.windowEnd,
```

## Data Models

`OffpeakData` gains one nullable field, `projectedEndSoc` (Go `ProjectedEndSoc *float64 json:"projectedEndSoc"`). No DynamoDB or stored-model changes — the value is derived live.

## Error Handling

No new failure modes. Every degenerate input (no live data, outside window, unparseable window, non-positive capacity, zero remaining time) resolves to an absent projection (`nil` → JSON `null`), which the client renders as "no row". This mirrors the existing absent-estimate convention.

## Testing Strategy

**Go unit (`compute_test.go`)** — table-driven `TestProjectOffpeakEndSoc`, capacity 13.34 kWh, window end 14:00 (fixtures double as the deferred worked example from decision_log Decision 7):

| now | soc | expected | exercises |
|---|---|---|---|
| 12:00 | 50 | 97.5 | crosses 95 boundary |
| 13:30 | 40 | 56.9 | fast-rate only, never reaches 95 |
| 13:00 | 97 | 100.0 | already in trickle band, clamps to 100 |
| 13:00 | 90 | 98.2 | crosses 95 mid-window |
| 12:00 | 100 | 100.0 | already full |
| 10:00 | 50 | nil | before window |
| 14:30 | 50 | nil | after window |
| 12:00 | 50 (cap 0) | nil | non-positive capacity |

**Go integration (`status_test.go`)**: inside window → `offpeak.projectedEndSoc` present; outside window → nil; stale live (no fresh reading) → nil; `simulateLoadWatts > 0` returns the **same** `projectedEndSoc` as the unsimulated call ([2.4](requirements.md#2.4)); response omits no field — absent value serialises as `null`.

**Property-based (Go `testing/quick`)** — invariants over random `(soc∈[0,100], capacity>0, hours>0)` against the closed form: result ∈ `[soc, 100]`; monotonic non-decreasing in `hours`; monotonic non-decreasing in `soc`. The monotonicity properties must hold the other inputs fixed and vary one dimension per generated pair — `testing/quick` generates inputs independently, so construct each paired comparison explicitly (one generated base input, plus a second input differing in only the dimension under test). These cover the clamp and curve guarantees more thoroughly than the table.

**Swift (FluxCore)**: decode test that `OffpeakData` round-trips `projectedEndSoc` for present, `null`, and absent. `BatteryBlock` logic test: with `projectedOffpeakEndSoc` set, the off-peak row label/value reflect the projection and the delta row is suppressed; with it nil, behaviour is unchanged.
