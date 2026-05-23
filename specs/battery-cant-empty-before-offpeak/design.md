# Design: Battery Can't Empty Before Off-Peak

**Transit ticket:** T-1327
**Related docs:** [requirements.md](./requirements.md), [decision_log.md](./decision_log.md)

## Overview

Add a `pbat`-independent server-computed boolean to `/status` that says "even at 5 kW sustained discharge from `now`, the battery cannot reach the 5 % cutoff before the next off-peak window starts." On the Dashboard hero, an indicator subview composes alongside the existing status line whenever the flag is true; the existing line is hidden when the indicator shows (only one is visible at a time).

## Architecture

### Backend integration points (Go)

| File | Change |
|---|---|
| `internal/api/status.go` | Add `maxDischargeKW = 5.0` to the constants block (alongside `cutoffPercent`, `fallbackCapacityKwh`). Inside the existing `liveFresh` branch — right after `EstimatedCutoff` is set — call the new helper and assign `battery.CantEmptyBeforeOffpeak`. |
| `internal/api/compute.go` | Add the helper described in §Helper contract. Add a small `withinOffpeakWindow` predicate that delegates parsing to the existing exported `derivedstats.ParseOffpeakWindow` (no new HH:MM parser). |
| `internal/api/response.go` | Add `CantEmptyBeforeOffpeak *bool \`json:"cantEmptyBeforeOffpeak"\`` to `BatteryInfo`. Pointer, no `omitempty` — JSON emits `null` when nil, matching the existing `estimatedCutoffTime` encoding (`response.go:32`). |

### Helper contract

To keep the call site readable and the test cases obvious, the helper takes an input struct:

```go
type cantEmptyInput struct {
    Soc, CapacityKwh   float64
    Now, NextOpStart   time.Time
    HasBoundary        bool
    WithinOffpeakWindow bool
}

// computeCantEmptyBeforeOffpeak returns &true when the battery cannot reach
// cutoffPercent before NextOpStart at maxDischargeKW sustained. Returns nil
// when the condition does not hold or inputs make the question meaningless.
// Reads cutoffPercent and maxDischargeKW as package-level constants.
func computeCantEmptyBeforeOffpeak(in cantEmptyInput) *bool
```

Branches (all return `nil`): `!in.HasBoundary` · `in.WithinOffpeakWindow` · `in.Soc <= cutoffPercent` · `in.CapacityKwh <= 0`.

Math (only reached after the above guards):

- `remainingKwh = (Soc - cutoffPercent) / 100 * CapacityKwh`
- `requiredHours = remainingKwh / maxDischargeKW`
- Return `&true` iff `Now.Add(requiredHours * time.Hour).After(NextOpStart)`. `After` is strict, so equality returns `nil` — matches AC [2.4] "now == nextOffpeakStart".

`maxDischargeKW` is a package constant, not a parameter — there is one max, set in code (Decision 1).

### Active-window check

`internal/derivedstats/offpeak.go:9` already exports `ParseOffpeakWindow`. The `isOffpeak` helper in that file is unexported but doing the active-window check inside `internal/api/compute.go` against `now` (already in Sydney local in `handleStatus`) avoids both re-parsing and a cross-package export:

```go
func withinOffpeakWindow(now time.Time, offpeakStart, offpeakEnd string) bool {
    startMin, endMin, ok := derivedstats.ParseOffpeakWindow(offpeakStart, offpeakEnd)
    if !ok {
        return false
    }
    minuteOfDay := now.Hour()*60 + now.Minute()
    return minuteOfDay >= startMin && minuteOfDay < endMin
}
```

Same single-source-of-truth invariant as Decision 8 (midnight-spanning windows still rejected via `ParseOffpeakWindow`).

### Wire path (existing → new)

```
handleStatus
  ├─ liveFresh ──┐
  │              ├─ existing: computeCutoffTime → battery.EstimatedCutoff
  │              └─ NEW:      computeCantEmptyBeforeOffpeak → battery.CantEmptyBeforeOffpeak
  └─ !liveFresh: neither field is set (both stay nil — null in JSON)
```

Decision 6 stands — flag does not appear on `rolling15min`.

### Swift model integration points

| File | Change |
|---|---|
| `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift` | Add `public let cantEmptyBeforeOffpeak: Bool?` to `BatteryInfo`. Memberwise initialiser gains `cantEmptyBeforeOffpeak: Bool? = nil` **with a default** so the five existing constructors (see §Pattern extension audit) keep compiling without edits. |
| `Flux/Flux/Services/MockFluxAPIClient.swift` | Add a second status fixture (e.g. `statusResponseCantEmpty`) that sets the flag to `true`, for preview/test coverage of the indicator state. The default fixture keeps the field `nil` to verify forward compatibility. |
| `Flux/FluxWidgets/WidgetFixtures.swift` | No edit needed thanks to the default; if a fixture explicitly wants the flag set, update locally. |

### Dashboard hero rendering

`Flux/Flux/Dashboard/DashboardHeroPanel.swift` today composes a single `statusLine` view branched by a `Mode` enum that reads `rolling15min.estimatedCutoffTime`. Changes:

1. `DashboardHeroPanel` gains two new inputs: `let battery: BatteryInfo?` and `let offpeakWindowStart: String?` (passed from `DashboardView.swift:113-116`).
2. Add a private subview `private var cantEmptyBeforeOffpeakIndicator: some View` that renders `"Won't empty before HH:MM"` using `FluxTheme.Typography.heroSubline` and `FluxTheme.Palette.secondaryText` (default subline styling — no `Palette.amber` reuse). HH:MM is read directly from the already-`HH:MM`-formatted `offpeakWindowStart` (no re-parsing needed).
3. `body` swaps the existing `statusLine` for the indicator when `battery?.cantEmptyBeforeOffpeak == true && offpeakWindowStart != nil`. The existing `Mode` enum and its branches are **unchanged**; the `.idle` "battery holding" copy is preserved.
4. `.accessibilityElement(children: .combine)` on the panel; the indicator subview sets `.accessibilityLabel("Battery won't empty before off-peak at HH:MM")` with HH:MM expanded (e.g. "23:00" → "23:00"). VoiceOver string is asserted by a Swift test (see §Testing).
5. When the flag is true but `offpeakWindowStart` is nil (defensive — server should always emit one when the flag is set), fall back to rendering the existing `statusLine`.

### Pattern extension audit

`BatteryInfo` is constructed via its public memberwise init in these places. Because the new property has `= nil` default, none of them require source changes:

| Call site | Constructor? | Action |
|---|---|---|
| `Flux/Packages/FluxCore/Tests/FluxCoreTests/WidgetAccessibilityTests.swift:19` | yes | none (default) |
| `Flux/Packages/FluxCore/Tests/FluxCoreTests/StatusSnapshotEnvelopeTests.swift:18` | yes | none (default) |
| `Flux/Packages/FluxCore/Tests/FluxCoreTests/StatusTimelineLogicTests.swift:25` | yes | none (default) |
| `Flux/Packages/FluxCore/Tests/FluxCoreTests/RelevanceScoringTests.swift:21` | yes | none (default) |
| `Flux/Packages/FluxCore/Sources/FluxCore/Widget/StatusTimelineLogic.swift:167` | yes | none (default) |
| `Flux/Flux/Services/MockFluxAPIClient.swift` | yes | add second fixture with flag `true` for previews |
| `Flux/FluxWidgets/WidgetFixtures.swift` | yes | none (default) |
| `Flux/FluxWidgets/Views/Shared/StatusEntry+WidgetColors.swift:48` | no (reads `rolling15min`) | none — widget is out of scope |
| `internal/api/status.go` | n/a (writer) | new write site (see Backend section) |

The default-value approach preserves source compatibility across the whole codebase. Without it, all five Swift constructors would need parallel edits.

## Data Models

Only one schema change: `BatteryInfo` gains a nullable boolean.

```jsonc
{
  "battery": {
    "capacityKwh": 13.34,
    "cutoffPercent": 5,
    "estimatedCutoffTime": null,      // existing, may be null
    "cantEmptyBeforeOffpeak": true,   // new, true | null
    "low24h": { /* ... */ }
  }
}
```

Wire contract (clarifies requirements AC [2.2]): the field is `null` when the condition is false, `true` when it holds. `false` is never emitted. Mirrors `estimatedCutoffTime`.

## Error Handling

No new failure modes. Every "false-equivalent" case (no boundary, window active, `!liveFresh`, soc at/below cutoff, capacity ≤ 0) yields `nil` and surfaces as JSON `null`. Swift decoding handles `null` → `nil` via `Bool?`.

## Testing Strategy

### Go: `internal/api/compute_test.go`

Map-based table tests for `computeCantEmptyBeforeOffpeak` and `withinOffpeakWindow`.

`computeCantEmptyBeforeOffpeak` cases:

| Name | soc | capacity | now → opStart | hasBoundary | windowActive | want |
|---|---|---|---|---|---|---|
| `soc just above cutoff, short window` | 6 | 13.34 | +5min | true | false | `&true` |
| `soc just above cutoff, long window` | 6 | 13.34 | +24h | true | false | `nil` |
| `soc well above cutoff, short window` | 90 | 13.34 | +30min | true | false | `&true` |
| `soc well above cutoff, long window` | 90 | 13.34 | +48h | true | false | `nil` |
| `soc exactly at cutoff` | 5 | 13.34 | +1h | true | false | `nil` |
| `soc below cutoff` | 3 | 13.34 | +1h | true | false | `nil` |
| `window currently active` | 80 | 13.34 | (any) | true | true | `nil` |
| `no boundary` | 80 | 13.34 | (any) | false | false | `nil` |
| `zero capacity` | 80 | 0 | +1h | true | false | `nil` |
| `boundary equality` | derived* | 13.34 | exact | true | false | `nil` |

\* For boundary equality: pick `Soc = cutoffPercent + (maxDischargeKW * 1h / 13.34) * 100` so `requiredHours == 1h` exactly, set `opStart = now + 1h`, expect `nil` because `After` is strict.

`withinOffpeakWindow` cases:

| Name | now (Sydney) | window | want |
|---|---|---|---|
| `before window` | 10:59 | 11:00-14:00 | false |
| `at start` | 11:00 | 11:00-14:00 | true |
| `mid-window` | 12:30 | 11:00-14:00 | true |
| `at end (exclusive)` | 14:00 | 11:00-14:00 | false |
| `after window` | 14:30 | 11:00-14:00 | false |
| `unparseable strings` | 12:00 | "x" / "y" | false |

### Go: `internal/api/status_test.go` (handler integration)

Add cases asserting the marshalled response includes/excludes the new field correctly:

1. `liveFresh && condition true` → `"cantEmptyBeforeOffpeak": true` in JSON.
2. `liveFresh && condition false` → `"cantEmptyBeforeOffpeak": null` in JSON.
3. `!liveFresh` → `"cantEmptyBeforeOffpeak": null` regardless of inputs (mirrors `estimatedCutoffTime` behaviour).
4. Off-peak config empty (`OFFPEAK_START=""`, `OFFPEAK_END=""`) → `null`.
5. **Sydney DST start (concrete date):** `nowFunc` returns 2026-10-04 01:30 Sydney local (the morning the clocks jump 02:00 → 03:00). Off-peak 11:00-14:00, Soc 60, capacity 13.34. Assert flag set/unset consistently with `nextOffpeakStart` advancing through the DST gap. (The existing `nextOffpeakStart` does the local-time math; this test pins the integration.)
6. Fallback capacity path: system record missing → flag is computed against `fallbackCapacityKwh = 13.34`.

### Swift: `Flux/Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift`

Decoding tests:

1. JSON with `"cantEmptyBeforeOffpeak": true` → decoded `Bool? == true`.
2. JSON with `"cantEmptyBeforeOffpeak": null` → decoded `nil`.
3. JSON without the key (older server payload) → decoded `nil` (forward compatibility).

### Swift: `DashboardHeroPanel` accessibility + preview tests

1. Add an XCTest (or Swift Testing) case in `FluxTests/` that constructs the panel with `battery.cantEmptyBeforeOffpeak == true`, `offpeakWindowStart == "23:00"`, and asserts the accessibility label of the indicator is exactly `"Battery won't empty before off-peak at 23:00"`. Covers [3.5].
2. Add the `#Preview` block in `DashboardHeroPanel.swift` to render both states (flag nil vs flag true) side by side, so the shared iOS+macOS Dashboard hero ([3.4]) can be visually verified in both schemes during code review. No additional macOS-specific runtime divergence exists because the panel uses platform-agnostic SwiftUI primitives.

### Property-based testing

Not applicable. The math is a single closed-form expression with fully enumerated branches.

## Out-of-scope follow-ups (noted, not implemented)

- Widget surfaces (medium/large) — Non-Goal.
- SoC push alerts integrating the flag — Non-Goal.
- Inverter derating curve — Non-Goal (Decision 1 consequence).
- Midnight-spanning off-peak windows — Non-Goal (Decision 8, inherited from `ParseOffpeakWindow`).
