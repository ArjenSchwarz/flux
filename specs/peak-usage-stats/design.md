# Design: Peak Usage Stats

## Overview

Adds a `dailyUsage` field to the `/day` API response carrying up to five chronological no-overlap blocks (`night`, `morningPeak`, `offPeak`, `afternoonPeak`, `evening`) with `totalKwh`, `averageKwhPerHour`, `percentOfDay`, `status`, and `boundarySource`. iOS replaces `EveningNightCard` with a new `DailyUsageCard` that renders the blocks. The existing `eveningNight` field, types, and card are deleted in lockstep ([decision 5](decision_log.md)).

## Architecture

### Backend call flow

```
handleDay()  (day.go)
  ├─ QueryReadings + GetDailyEnergy   (existing)
  ├─ findMinSOC + downsample           (existing)
  ├─ findPeakPeriods                   (existing)
  ├─ findDailyUsage                    ← NEW (replaces findEveningNight)
  └─ build DayDetailResponse
       ├─ peakPeriods                  (existing)
       └─ dailyUsage                   ← NEW field (replaces eveningNight)
```

The current `eveningNight = findEveningNight(...)` line in `day.go:71` and the `EveningNight: eveningNight` line in `day.go:90` are deleted; `dailyUsage = findDailyUsage(readings, h.offpeakStart, h.offpeakEnd, date, today, now)` and `DailyUsage: dailyUsage` take their place. `findDailyUsage` reuses `integratePload` and `melbourneSunriseSunset` and consumes the same `[]dynamo.ReadingItem` already in scope ([req 1.12, 1.14](requirements.md)).

### Pattern parity audit (vs existing `eveningNight`)

The audit was produced via `grep -rn "EveningNight\|eveningNight\|DayDetailResponse(" Flux/ Packages/ internal/`. Every site that touches the field needs a parallel update.

| Existing `eveningNight` site | `dailyUsage` equivalent | Action |
|---|---|---|
| `internal/api/compute.go: findEveningNight` (lines 640–755) | `findDailyUsage` | Delete `findEveningNight` and `buildEveningNightBlock`; add `findDailyUsage` and `buildDailyUsageBlock`. Reuse `integratePload`, `melbourneSunriseSunset`, `parseOffpeakWindow`, and `preSunriseBlipBuffer`. |
| `internal/api/compute.go:488,504` doc-comment references to `buildEveningNightBlock` (inside `melbourneSunriseSunset`) | reword to reference `buildDailyUsageBlock` (or generalise) | Stale comment fix. |
| `internal/api/response.go:107` `EveningNight *EveningNight` | `DailyUsage *DailyUsage` | Field rename + struct rename. |
| `internal/api/response.go:113-118` `EveningNightStatus*` / `EveningNightBoundary*` constants | `DailyUsageStatus*` / `DailyUsageBoundary*` | Rename. Same string values. |
| `internal/api/response.go:120-142` `EveningNight` and `EveningNightBlock` structs | `DailyUsage` and `DailyUsageBlock` | Replace; new shape carries `Blocks []DailyUsageBlock` and `kind`/`percentOfDay` per block. |
| `internal/api/day.go:64,71,90` wiring | parallel wiring | Swap variable name and call site. |
| `internal/api/compute_test.go` `TestFindEveningNight*`, `TestBuildEveningNightBlock*`, plus the shared fixture-builder helper near line 1630 (`makeReadingsWithPpv` or equivalent) | `TestFindDailyUsage*`, `TestBuildDailyUsageBlock*` | Delete old test functions; rename and reuse the fixture builder (it stays useful for the new test matrix). Add new tests with the AC 4.1 fixture matrix. |
| `internal/api/day_test.go` integration assertions on `eveningNight` (including `TestHandleDayEveningNightPerBlockFallback` and the `assert.Nil(t, dr.EveningNight, …)` in `TestHandleDayFallbackPath`) | parallel assertions on `dailyUsage` | Rename `TestHandleDayEveningNightPerBlockFallback` → `TestHandleDayDailyUsageOvercast` (it maps onto the AC 4.1 overcast fixture). Update the fallback-path assertion to `assert.Nil(t, dr.DailyUsage, …)`. |
| `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift:233-304` `DayDetailResponse`, `EveningNight`, `EveningNightBlock` | `DayDetailResponse` (init grows new param), `DailyUsage`, `DailyUsageBlock` | Replace. Memberwise init becomes `init(date:readings:summary:peakPeriods:dailyUsage:)`. |
| `Flux/Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift:308-onwards` `decodeDayDetailResponseWithEveningNightBothBlocks` and friends | `decodeDayDetailResponseWithDailyUsage*` | Delete old, add new. JSON fixtures need rewriting (new field, new shape). |
| `Flux/Packages/FluxCore/Tests/FluxCoreTests/StatusTimelineLogicTests.swift:374,397` literal `DayDetailResponse(... eveningNight: nil)` | `DayDetailResponse(... dailyUsage: nil)` | Two literal call sites need parameter rename. |
| `Flux/FluxTests/DayDetailViewModelTests.swift` 10 literal `DayDetailResponse(...)` call sites + `loadDayPopulatesEveningNightFromResponse` + `loadDayPropagatesEveningNightWithOnlyOneBlock` + `loadDayWithNilEveningNightLeavesPropertyNil` + `loadDayFallbackDataPathLeavesEveningNightAsBackendSent` + `loadDayErrorResetsEveningNightToNil` (lines 122–238) | rename literal call-site params; rewrite all five test funcs as `loadDayPopulatesDailyUsage*` / `loadDayWithNilDailyUsage*` etc. | Test rewrite. |
| `Flux/Flux/Services/MockFluxAPIClient.swift:86` preview `DayDetailResponse(...)` literal + `dayEveningNight` helper | swap to `dailyUsage:` parameter; `dayEveningNight` → `dayDailyUsage` (build five-block sample fixture for the preview) | Update preview to render the new card. |
| `Flux/Flux/DayDetail/DayDetailViewModel.swift:28,60,68` `eveningNight: EveningNight?` and reset paths | `dailyUsage: DailyUsage?` and reset paths | Rename. |
| `Flux/Flux/DayDetail/DayDetailView.swift:35-39` `EveningNightCard` slot | `DailyUsageCard` slot in same position | Replace. Guard becomes `if viewModel.hasPowerData, let du = viewModel.dailyUsage, !du.blocks.isEmpty { DailyUsageCard(dailyUsage: du) }` per [req 3.6](requirements.md#3.6). |
| `Flux/Flux/DayDetail/EveningNightCard.swift` (entire file, including its `#Preview` block at lines 84-106) | `Flux/Flux/DayDetail/DailyUsageCard.swift` | Delete old file; create new file. The new file SHOULD include a `#Preview` block populated with a five-block fixture so the card renders standalone in Xcode previews. |
| Widgets (`Flux/FluxWidgets/`) | n/a | Widgets consume `/status`, not `/day` — no impact. |
| History screen | n/a | `/history`, not `/day` — no impact. |

### File placement (new code)

| New code | Lives in |
|---|---|
| `findDailyUsage`, `buildDailyUsageBlock`, `recentSolarThreshold` constant | `internal/api/compute.go` (alongside the deleted `findEveningNight` site) |
| `DailyUsage`, `DailyUsageBlock` Go structs and status/boundary/kind constants | `internal/api/response.go` |
| Swift `DailyUsage`, `DailyUsageBlock`, `DailyUsageBlockKind` enum | `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift` |
| `DailyUsageCard` SwiftUI view | `Flux/Flux/DayDetail/DailyUsageCard.swift` |

## Components and Interfaces

### Backend types (`response.go`)

```go
type DailyUsage struct {
    Blocks []DailyUsageBlock `json:"blocks"`
}

type DailyUsageBlock struct {
    Kind              string   `json:"kind"`               // "night" | "morningPeak" | "offPeak" | "afternoonPeak" | "evening"
    Start             string   `json:"start"`              // RFC 3339 UTC
    End               string   `json:"end"`                // RFC 3339 UTC
    TotalKwh          float64  `json:"totalKwh"`
    AverageKwhPerHour *float64 `json:"averageKwhPerHour,omitempty"`
    PercentOfDay      int      `json:"percentOfDay"`
    Status            string   `json:"status"`             // "complete" | "in-progress"
    BoundarySource    string   `json:"boundarySource"`     // "readings" | "estimated"
}
```

`DayDetailResponse` adds `DailyUsage *DailyUsage \`json:"dailyUsage,omitempty"\`` and removes `EveningNight`. `Blocks` is a non-pointer slice — when no blocks survive, the parent `DailyUsage` pointer is nil and `omitempty` drops the field per [req 1.1, 2.1](requirements.md#1.1).

Constants:

```go
const (
    DailyUsageStatusComplete    = "complete"
    DailyUsageStatusInProgress  = "in-progress"
    DailyUsageBoundaryReadings  = "readings"
    DailyUsageBoundaryEstimated = "estimated"

    DailyUsageKindNight         = "night"
    DailyUsageKindMorningPeak   = "morningPeak"
    DailyUsageKindOffPeak       = "offPeak"
    DailyUsageKindAfternoonPeak = "afternoonPeak"
    DailyUsageKindEvening       = "evening"
)
```

### Backend function: `findDailyUsage`

```go
func findDailyUsage(
    readings []dynamo.ReadingItem,
    offpeakStart, offpeakEnd string,
    date, today string,
    now time.Time,
) *DailyUsage
```

**Caller passes `today` and `now`** — same convention as `findEveningNight`. The function does not call `time.Now`. `now` from the handler is at second precision.

**Caller contract:**
- `today` MUST be the calendar date `now` falls on, formatted as `YYYY-MM-DD` in `Australia/Sydney`. The handler at `day.go:29` already satisfies this; future refactors must preserve it. Passing UTC-formatted `today` would silently miss the today-gate at the midnight transition.
- `readings` MUST be sorted ascending by `Timestamp` (DynamoDB `ScanIndexForward: true` guarantees this in production).

**Invariants:**
- All time boundaries are second-precision. `dayStart`/`dayEnd` come from `time.ParseInLocation`; `melbourneSunriseSunset` returns second-precision (compute.go:510); `now` is second-precision.

**Algorithm** (mirrors [requirements AC 1.8](requirements.md#1.8) pipeline):

1. **Resolve constants and per-day anchors.**
   - `dayStart, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)`; `dayEnd := dayStart.AddDate(0, 0, 1)`.
   - `isToday := date == today`.
   - Resolve `computedSunrise` and `computedSunset` once via `melbourneSunriseSunset` (cached in closures, mirroring the existing `findEveningNight` pattern at compute.go:660–677 to avoid re-parsing the sun table).

2. **Single pass over readings.** All filters consult the closed window `[computedSunrise.Unix() - preSunriseBlipBuffer, computedSunset.Unix() + preSunriseBlipBuffer]` ([req Definitions](requirements.md#definitions); applies symmetrically to both `firstSolar` and `lastSolar` per decisions [8](decision_log.md) and [10](decision_log.md)). Track:
   - `firstSolar` — earliest reading with `Ppv > 0` AND timestamp inside the closed window.
   - `lastSolar` — latest reading with `Ppv > 0` AND timestamp inside the closed window.
   - `recentSolar` — set only when `isToday`. True if any reading in `[now.Unix() - int64(recentSolarThreshold.Seconds()), now.Unix()]` has `Ppv > 0`.
   - `hasQualifyingPpv` — true if `firstSolar` is non-nil (i.e. at least one reading passed the qualifying filter). Distinct from "any reading exists today"; the today-gate's second disjunct keys off this flag, NOT the post-fallback `lastSolar` value (which always exists).

3. **Resolve `firstSolarTS` and `lastSolarTS` (Unix seconds).** When the corresponding tracker is nil, fall back to `computedSunrise.Unix()` / `computedSunset.Unix()` respectively. Record per-edge fallback flags (`firstSolarFromFallback`, `lastSolarFromFallback`) for use in step 9.

4. **Solar-window guard ([req 1.8 step 2](requirements.md#1.8)).** Parse the off-peak window first via `parseOffpeakWindow`. If parsing fails, fall through immediately to the two-block path. Otherwise resolve `offpeakStartTS`/`offpeakEndTS` against `dayStart` and check the strict invariant `firstSolarTS < offpeakStartTS < offpeakEndTS < lastSolarTS`. (Transitively this implies `firstSolarTS < lastSolarTS`, covering the single-solar-reading case from [decision 7](decision_log.md).) On invariant failure, fall through to the **two-block path**: build only `night` and `evening` using their nominal intervals, then continue at step 6 for those two blocks. Per [decision 11](decision_log.md), this includes partial-data days where the recorder died during or before off-peak (`lastSolar < offpeakEnd`).

5. **Build nominal intervals** (regular five-block path):
   - `night`: `[dayStart, time.Unix(firstSolarTS, 0))`
   - `morningPeak`: `[time.Unix(firstSolarTS, 0), offpeakStartTime)`
   - `offPeak`: `[offpeakStartTime, offpeakEndTime)`
   - `afternoonPeak`: `[offpeakEndTime, time.Unix(lastSolarTS, 0))`
   - `evening`: `[time.Unix(lastSolarTS, 0), dayEnd)`

6. **Apply today-gate ([req 1.8 step 3](requirements.md#1.8)).** Only when `isToday`. Compute `solarStillUp = recentSolar || (!hasQualifyingPpv && !now.After(computedSunset))`. When true:
   - Mark `evening` for omission.
   - Override `afternoonPeak.end = now` AND set `afternoonPeak.statusOverride = "in-progress"` (a sentinel that step 7 honours).

   **Note:** the gate fires "early" pre-sunrise on overcast days (e.g. `now = 04:30`, no readings, gate fires because `!hasQualifyingPpv && !now.After(computedSunset)`). This is intentional — the side-effects on `afternoonPeak` and `evening` are then mopped up by step 7's future-omit (`afternoonPeak.start = offpeakEnd > now`) and the explicit `evening` omission. The gate is correct-by-construction even when its targets don't survive; do not "fix" the predicate.

7. **Apply future-omit and in-progress clamp ([req 1.8 steps 4–5](requirements.md#1.8)).** For each surviving block in chronological order:
   - If `isToday && block.start.After(now)`: omit.
   - If `isToday && block.end.After(now) && statusOverride == ""`: set `block.end = now`, status = in-progress.
   - Else if `statusOverride != ""`: status = `statusOverride`.
   - Else: status = complete.

8. **Apply degenerate-omit ([req 1.8 step 6](requirements.md#1.8)).** Drop any block with `!block.start.Before(block.end)`.

9. **Resolve `boundarySource` from emitted edges.** For each surviving block, the question is only "was either of this block's emitted boundaries the sunrise/sunset fallback?" Two booleans per block suffice: `(startEstimated, endEstimated)`. Wire them per the table below; per [req 1.5](requirements.md#1.5) the block's `boundarySource = "estimated"` iff `startEstimated || endEstimated`, otherwise `"readings"`.

   | Block | `startEstimated` source | `endEstimated` source |
   |---|---|---|
   | night | always false (start = midnight) | `firstSolarFromFallback` AND end was not overridden by step 7's clamp (i.e. status = complete) |
   | morningPeak | `firstSolarFromFallback` (start = `firstSolarTS`) | always false (end = `offpeakStart` or `requestTime` clamp) |
   | offPeak | always false | always false |
   | afternoonPeak | always false | `lastSolarFromFallback` AND end was not overridden (today-gate clamp or step-7 clamp both produce `requestTimeClamp` ⇒ `endEstimated = false`) |
   | evening | `lastSolarFromFallback` (start = `lastSolarTS`) | always false (end = midnight or `requestTime` clamp) |

   Per the rules above, an in-progress `night` (end = `requestTime`) has `endEstimated = false` ⇒ `boundarySource = "readings"`. AC 3.4's "no caption on in-progress night" expectation falls out automatically.

10. **Compute totalKwh, percentOfDay, averageKwhPerHour ([req 1.4, 1.6, 1.7](requirements.md#1.4)).** Two-pass over surviving blocks:
    - **Pass 1**: for each block, call `integratePload(readings, block.startUnix, block.endUnix)` and store the unrounded result in `pendingBlock.unroundedKwh`.
    - **Sum**: compute `unroundedSum = Σ pendingBlock.unroundedKwh`.
    - **Pass 2**: for each `pendingBlock`, invoke `buildDailyUsageBlock(p, unroundedSum)`. Inside the helper:
      - `totalKwh = roundEnergy(p.unroundedKwh)` (req 1.3 rounding to 2 decimals).
      - `averageKwhPerHour = roundEnergy(p.unroundedKwh / (elapsedSeconds / 3600.0))` if `elapsedSeconds >= 60`, otherwise omitted (req 1.6). Computed from unrounded numerator to avoid double-rounding (req 1.7's "rounding at serialization only" precedent).
      - `percentOfDay = int(math.Round(p.unroundedKwh / unroundedSum * 100))` if `unroundedSum > 0`, else 0 (req 1.7).

11. **If no blocks survived, return nil.** Otherwise return `&DailyUsage{Blocks: blocks}`.

**Why `statusOverride` instead of pre-setting status in step 6?** To keep step 7's clamp logic simple and preserve a single point where status is finalised. The override flag is a local-only sentinel; it never leaves `findDailyUsage`.

**Constants placement.**

```go
// In compute.go, near preSunriseBlipBuffer.
const recentSolarThreshold = 5 * time.Minute  // req decision 9
```

`preSunriseBlipBuffer` (compute.go:638) is reused; the post-sunset filter applies the same value to the upper bound.

### Backend helper: `buildDailyUsageBlock`

The two-pass nature of `percentOfDay` (need the cross-block sum before per-block percentages) is captured by a small intermediate struct:

```go
type pendingBlock struct {
    kind                          string
    start, end                    time.Time
    startEstimated, endEstimated  bool
    status                        string
    unroundedKwh                  float64  // populated by step 10's first pass
}

func buildDailyUsageBlock(p pendingBlock, unroundedSum float64) DailyUsageBlock
```

`buildDailyUsageBlock` is a pure formatter: no integration, no slice access. It computes `boundarySource = "estimated"` iff `p.startEstimated || p.endEstimated`, formats `start`/`end` as `time.RFC3339` UTC, computes `averageKwhPerHour` and `percentOfDay` from `p.unroundedKwh` and `unroundedSum`, and assigns `kind`/`status`. Returns by value (`Blocks []DailyUsageBlock` is a non-pointer slice; per-block `omitempty` is not needed because omission happens by not appending).

The two-pass loop in step 10 builds the slice of `pendingBlock`s first (calling `integratePload` per block), sums `unroundedKwh` across them, then maps each through `buildDailyUsageBlock`. This keeps the helper signature at two parameters and isolates the cross-block dependency to one place.

### iOS types (`APIModels.swift`)

```swift
public struct DailyUsage: Codable, Sendable {
    public let blocks: [DailyUsageBlock]

    public init(blocks: [DailyUsageBlock]) { ... }
}

public struct DailyUsageBlock: Codable, Sendable, Identifiable {
    public enum Kind: String, Codable, Sendable {
        case night
        case morningPeak
        case offPeak
        case afternoonPeak
        case evening
    }

    public enum Status: String, Codable, Sendable {
        case complete
        case inProgress = "in-progress"
    }

    public enum BoundarySource: String, Codable, Sendable {
        case readings
        case estimated
    }

    public let kind: Kind
    public let start: String
    public let end: String
    public let totalKwh: Double
    public let averageKwhPerHour: Double?
    public let percentOfDay: Int
    public let status: Status
    public let boundarySource: BoundarySource

    public var id: String { kind.rawValue }   // unique per response (one block per kind)

    public init(...) { ... }
}
```

`DayDetailResponse` swaps `eveningNight: EveningNight?` for `dailyUsage: DailyUsage?`. Existing `EveningNight` and `EveningNightBlock` structs are deleted entirely. The synthesised memberwise init becomes `init(date:readings:summary:peakPeriods:dailyUsage:)`.

### iOS view: `DailyUsageCard`

Mirrors the existing `EveningNightCard` styling: `.thinMaterial` background, `RoundedRectangle(cornerRadius: 16, style: .continuous)`, `.headline` title.

**Card title:** `"Daily Usage"` (per requirements [3.1, 3.2](requirements.md#3.1)).

**Row layout.** Each row uses a two-line `VStack(alignment: .leading, spacing: 2)` containing two `HStack`s. The caption position depends on which edge is estimated, per the AC 3.4 mapping:

```
┌──────────────────────────────────────────────────┐
│ Morning Peak               ≈ sunrise 06:32–11:00 │   line 1: label, time range (caption inline at start)
│                            2.1 kWh · 0.47 kWh/h  │   line 2: totals
│                                              17% │   line 3: percentage
└──────────────────────────────────────────────────┘
```

For end-edge captions (`night`, `afternoonPeak`):

```
│ Night                              00:00–06:30 ≈ sunrise │
```

In code:

```swift
struct DailyUsageCard: View {
    let dailyUsage: DailyUsage

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Daily Usage").font(.headline)
            ForEach(dailyUsage.blocks) { block in row(block) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func row(_ block: DailyUsageBlock) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(label(block.kind)).font(.subheadline)
                Spacer()
                timeRangeView(block)   // caption-inline rendering
            }
            HStack {
                Spacer()
                Text(totals(block)).font(.subheadline).foregroundStyle(.secondary)
            }
            HStack {
                if block.status == .inProgress {
                    Text("(so far)").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                Text("\(block.percentOfDay)%").font(.caption).foregroundStyle(.secondary)
            }
        }
    }
    // helpers: label(_:), timeRangeView(_:) (renders caption inline based on kind),
    //         totals(_:), captionFor(kind:)
}
```

**`timeRangeView`** renders the caption positionally adjacent to the relevant timestamp. Use a `@ViewBuilder` returning `HStack(spacing: 4)` rather than `Text + Text` concatenation — the row's outer `HStack` includes a `Spacer()` and other view types, which `Text + Text` cannot accommodate (concatenation requires both operands to be `Text`).

```swift
@ViewBuilder
private func timeRangeView(_ block: DailyUsageBlock) -> some View {
    let times = formatTimes(block.start, block.end)  // "06:30–11:00"
    let cap = caption(block.kind)                    // "≈ sunrise" / "≈ sunset" / ""
    let showCaption = block.boundarySource == .estimated && !cap.isEmpty
    HStack(spacing: 4) {
        if showCaption && captionLeads(block.kind) {
            Text(cap).font(.caption).foregroundStyle(.secondary)
        }
        Text(times).font(.subheadline)
        if showCaption && !captionLeads(block.kind) {
            Text(cap).font(.caption).foregroundStyle(.secondary)
        }
    }
}
```

| Kind | Caption position | Result |
|---|---|---|
| `night` | trailing | `"00:00–06:30 ≈ sunrise"` |
| `morningPeak` | leading | `"≈ sunrise 06:30–11:00"` |
| `offPeak` | (no caption ever) | `"11:00–14:00"` |
| `afternoonPeak` | trailing | `"14:00–18:42 ≈ sunset"` |
| `evening` | leading | `"≈ sunset 18:42–24:00"` |

Caption rendered iff `block.boundarySource == .estimated`. The in-progress `(so far)` indicator is shown beneath, mirroring the existing `EveningNightCard.swift:55-57` affordance. Per [decision 9](decision_log.md) and the round-3 critic note, an in-progress `night` block has `boundarySource = "readings"` (its emitted end is `requestTime`, not sunrise) so no caption shows for it — this falls out of the boundarySource rule, no special-case needed.

**Card density caveat.** Five blocks × three visual lines per row + title = ~16 lines of secondary text. On a 320pt-wide iPhone (SE-class) the card will be tall. The implementer should manually verify the layout on the smallest supported screen and either accept the height or compress to two lines per row (label + time on line 1; totals + percentage on line 2, caption inline with the time as above).

**Visibility guard (in `DayDetailView`):**
```swift
if viewModel.hasPowerData, let du = viewModel.dailyUsage, !du.blocks.isEmpty {
    DailyUsageCard(dailyUsage: du)
}
```
Replaces the existing block at `DayDetailView.swift:35-39`. Same slot — between `PeakUsageCard` and `summaryCard`.

**Past-day partial-data case.** Per [req 4.1](requirements.md#4.1) "partial-data day" fixture: a recorder that died at 12:30 produces `evening` with `start = 12:30, end = 24:00, totalKwh = 0`. No special card treatment — the row renders the long span with the actual usage and `0%` of day.

**Localisation and accessibility.** English-only. Default VoiceOver behaviour from `Text` views (matches the existing `EveningNightCard`, which also relies on default behaviour). No explicit `accessibilityLabel`.

### iOS view-model wiring

`DayDetailViewModel.swift:28` swaps `private(set) var eveningNight: EveningNight?` for `private(set) var dailyUsage: DailyUsage?`.

`loadDay()` success path (`DayDetailViewModel.swift:60`): `dailyUsage = response.dailyUsage`. Error path (`DayDetailViewModel.swift:68`): `dailyUsage = nil`.

## Data Models

Covered above under Components and Interfaces. No DynamoDB schema changes ([req 1.14](requirements.md#1.14)).

## Error Handling

| Failure mode | Behaviour |
|---|---|
| `len(readings) == 0` | Caller (`handleDay`) skips `findDailyUsage` (parallel to existing `findEveningNight` gate); `dailyUsage` field omitted via `omitempty`. ([req 1.10](requirements.md#1.10)) |
| Daily-power fallback path | `findDailyUsage` not invoked; field omitted. ([req 1.10](requirements.md#1.10)) |
| Off-peak SSM unparseable / `start >= end` | Two-block path triggers per step 4; only `night` and `evening` emitted. ([req 1.11](requirements.md#1.11), [decision 7](decision_log.md)) |
| Solar-window invariant violated (`firstSolar >= offpeakStart`, `lastSolar <= offpeakEnd`, or `firstSolar == lastSolar`) | Same two-block path. ([decision 7](decision_log.md)) |
| Today, sun still up (recent Ppv reading) | Today-gate fires; `evening` omitted; `afternoonPeak.end = now`, in-progress. ([decision 9](decision_log.md)) |
| Today, cloudy late afternoon (no recent Ppv, `lastSolar < now`) | Today-gate does NOT fire; `afternoonPeak` complete with reading-derived `end`; `evening` in-progress from `lastSolar` to `now`. ([decision 9](decision_log.md)) |
| Today, mid-block elapsed < 60s | Block emits with `totalKwh = 0` and `averageKwhPerHour` omitted. |
| `melbourneSunriseSunset` lookup miss (Feb 29) | Falls back to Feb 28 values via the existing helper at compute.go:495. |
| `integratePload` returns 0 (no qualifying reading pairs / sparse data) | Block still emitted with `totalKwh = 0` and `percentOfDay = 0` (when sum across all blocks is also 0). |
| Pre-sunrise Ppv blip at 01:30 | Filtered by `Timestamp >= computedSunrise - 30 min` ([decision 8](decision_log.md)). |
| Post-sunset Ppv blip at 22:00 | Filtered by `Timestamp <= computedSunset + 30 min` ([decision 10](decision_log.md)). |
| Future date | `len(readings) == 0` is the upstream gate; `dailyUsage` omitted. |

## Testing Strategy

### Backend unit tests (`compute_test.go`)

Map-based table-driven tests, one map per function. The fixture matrix from [req 4.1](requirements.md#4.1) becomes the `TestFindDailyUsage` cases. Each case asserts the exact set of emitted block kinds and the `boundarySource`/`status` for each. Where the requirement specifies a numeric expectation (e.g. partial-data day's `totalKwh = 0`), assert it directly.

| Test name | Coverage |
|---|---|
| `TestFindDailyUsage` (table) | All AC 4.1 fixtures (now 17 entries after the partial-data split per [decision 11](decision_log.md)): typical, today×6 plus the overcast-morning-mid-morning case folded in (today, no qualifying Ppv yet, mid-morning request, confirms `morningPeak` in-progress with `boundarySource = "estimated"`), overcast-complete-day, partial-data-after-offpeak, partial-data-during-offpeak (two-block), off-peak misconfigured, fallback-only, invariant violation by constructed sunrise>offpeakStart, single-solar (firstSolar == lastSolar), DST spring-forward, DST fall-back, pre-sunrise blip, post-sunset blip, future-date, plus a today + off-peak misconfigured case asserting the today-gate and in-progress clamp still apply on the two-block path. |
| `TestFindDailyUsage_PercentOfDay` | Asserts sum of `percentOfDay` across emitted blocks = 100±3 on a typical-day fixture; asserts `percentOfDay = 0` for every emitted block on a zero-load fixture. ([req 4.2](requirements.md#4.2)) |
| `TestBuildDailyUsageBlock` (table) | Covers `boundarySource` resolution for each `(startEstimated, endEstimated)` combination; covers `averageKwhPerHour` omission on elapsed < 60s. |

`TestIntegratePload` and `TestMelbourneSunriseSunset` (existing) are unaffected and continue to pass — no changes to those helpers.

### Backend integration tests (`day_test.go`)

- Extend `TestHandleDayNormalCase` to assert `dailyUsage` is non-nil and `len(blocks) > 0`.
- Update `TestHandleDayFallbackPath` to assert `dailyUsage == nil`.
- Add `TestHandleDayNoReadings` (or extend the existing equivalent) to assert `dailyUsage == nil`.
- Delete the existing `TestHandleDay*EveningNight*` cases.

### iOS tests

`APIModelsTests.swift`:
- Decode a response with `dailyUsage` containing all five blocks; assert each field including `kind`, `percentOfDay`, `boundarySource`, `status`.
- Decode a response with `dailyUsage` absent → `dailyUsage == nil`.
- Decode a response with only `night` and `evening` (off-peak misconfigured shape) → `blocks.count == 2` and kinds match.
- Decode a response with `averageKwhPerHour: null` → Swift `Double?` is nil.
- Delete the existing `decodeDayDetailResponseWithEveningNight*` tests.
- The two literal `DayDetailResponse(...)` call sites at `StatusTimelineLogicTests.swift:374,397` swap `eveningNight: nil` for `dailyUsage: nil`.

`DayDetailViewModelTests.swift`:
- Rewrite `loadDayPopulatesEveningNightFromResponse` → `loadDayPopulatesDailyUsageFromResponse` with a five-block fixture; assert `viewModel.dailyUsage?.blocks.count == 5` and selected fields.
- Rewrite `loadDayPropagatesEveningNightWithOnlyOneBlock` → `loadDayPropagatesDailyUsageWithTwoBlocks` (two-block off-peak-misconfig path).
- Rewrite `loadDayWithNilEveningNightLeavesPropertyNil` → `loadDayWithNilDailyUsageLeavesPropertyNil`.
- Rewrite `loadDayFallbackDataPathLeavesEveningNightAsBackendSent` → `loadDayFallbackDataPathLeavesDailyUsageAsBackendSent`.
- Rewrite `loadDayErrorResetsEveningNightToNil` → `loadDayErrorResetsDailyUsageToNil`.
- Update the 10 literal `DayDetailResponse(... eveningNight: nil)` call sites at lines 19, 65, 79, 93, 107, 142, 169, 183, 202, 227 to `dailyUsage: nil`.
- Replace the `EveningNight(...)` and `EveningNightBlock(...)` constructor literals inside the rewritten test bodies (lines 124, 125, 133, 158, 160, 216, 217 in the current file) with `DailyUsage(blocks: [DailyUsageBlock(...)])` calls.

**Acknowledged AC 4.3 caveat.** The "caption rendered when boundarySource = estimated" expectation is exercised at view-model construction time only — there is no view-layer assertion that the caption actually renders adjacent to the correct timestamp. Matches precedent (`EveningNightCard` is similarly untested at the view layer). If view-layer testing is added later (ViewInspector or snapshot tests), this is the first place to wire it.

`MockFluxAPIClient.swift`:
- Replace the `dayEveningNight` helper with `dayDailyUsage`. Build a realistic five-block fixture so the SwiftUI preview at `DayDetailView.swift:213` renders the new card with all five rows.

### Property-based testing

Skipped, same reasoning as `evening-night-stats`: the function is a sequence of "scan, classify, integrate" with no algebraic invariants worth a generator. Existing example-based table tests cover the boundary surface.

### Benchmark

`BenchmarkFindDailyUsage` over 8640 readings, mirroring `BenchmarkFindPeakPeriods` and `BenchmarkFindEveningNight`. Acceptance bar: same order of magnitude as `findEveningNight` (the algorithm is a single pass + five integrations versus two; expected ~2.5× the predecessor's runtime, still well under 1 ms).
