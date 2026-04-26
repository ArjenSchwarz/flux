# Design: Evening / Night Stats

## Overview

Adds an `eveningNight` field to the `/day` API response with two optional blocks (`evening`, `night`), each carrying total kWh, average kWh/h, period boundaries, and provenance flags. iOS renders the blocks as a new `EveningNightCard` between the Peak Usage card and the Summary card on `DayDetailView`. Mirrors the existing `peakPeriods` pattern: same file layout, same testing approach, no new DynamoDB queries.

## Architecture

### Backend call flow

```
handleDay()  (day.go, existing)
  ├─ QueryReadings + GetDailyEnergy   (existing)
  ├─ findMinSOC                        (existing)
  ├─ downsample                        (existing)
  ├─ findPeakPeriods                   (existing)
  ├─ findEveningNight                  ← NEW
  └─ build DayDetailResponse
       ├─ peakPeriods                  (existing)
       └─ eveningNight                 ← NEW field
```

`findEveningNight` consumes the same `[]dynamo.ReadingItem` slice already in scope. No new DynamoDB query, no new struct on the handler. ([req 1.13](#))

### Pattern parity audit (vs `peakPeriods`)

Adding `eveningNight` to `DayDetailResponse` is a breaking change for every Swift literal call site of the `DayDetailResponse(...)` initializer (the synthesized memberwise init grows one parameter). All literal sites must be updated. The audit below was produced by `grep -rn "DayDetailResponse(" Flux/` and `grep -rn "DayDetailResponse{" internal/`.

| Existing peakPeriods touch point | eveningNight equivalent | Needed? |
|---|---|---|
| `internal/api/compute.go: findPeakPeriods` | `findEveningNight`, `integratePload`, `melbourneSunriseSunset` | Yes |
| (new file) | `internal/api/melbourne_sun_table.go` — generated lookup table (366 entries) | Yes |
| `internal/api/response.go: PeakPeriod` struct | `EveningNight` + `EveningNightBlock` structs | Yes |
| `internal/api/response.go: DayDetailResponse.PeakPeriods` | `DayDetailResponse.EveningNight *EveningNight` (pointer for `omitempty`) | Yes |
| `internal/api/day.go: peakPeriods wiring (line ~63, ~87)` | parallel wiring after `findPeakPeriods` call; pass existing `today` and `now` locals | Yes |
| `internal/api/compute_test.go` table tests | `TestFindEveningNight`, `TestIntegratePload` (with the worked-example fixture), `TestComputeSunriseSunset` | Yes |
| `internal/api/day_test.go` integration cases | extend `TestHandleDayNormalCase` to assert `eveningNight` non-nil; add per-block fallback fixture; extend fallback-only-power-data test to assert `eveningNight == nil` | Yes |
| `Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift` | `EveningNight`, `EveningNightBlock` (with `Status` and `BoundarySource` enums); `DayDetailResponse.eveningNight: EveningNight?`; `DayDetailResponse.init` adds parameter | Yes |
| `Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift` | extend decode tests with present / absent / one-block-only / null-average cases; **update existing literal `DayDetailResponse(...)` calls to pass `eveningNight: nil`** | Yes |
| `Packages/FluxCore/Tests/FluxCoreTests/StatusTimelineLogicTests.swift` | **two literal `DayDetailResponse(...)` call sites (~lines 374, 397) need `eveningNight: nil`** added | Yes — flagged by audit |
| `Flux/FluxTests/DayDetailViewModelTests.swift` | **five literal `DayDetailResponse(...)` call sites (~lines 19, 65, 79, 93, 107) need `eveningNight: nil`**; add new tests per [req 4.3](#) | Yes — flagged by audit |
| `Flux/Flux/DayDetail/DayDetailViewModel.swift: peakPeriods` | `eveningNight: EveningNight?` property; reset to `nil` on error path | Yes |
| `Flux/Flux/DayDetail/PeakUsageCard.swift` | new `EveningNightCard.swift` | Yes |
| `Flux/Flux/DayDetail/DayDetailView.swift` placement | parallel guard between PeakUsageCard and summaryCard | Yes |
| `MockFluxAPIClient` preview fixture | extend preview `DayDetailResponse` with realistic `eveningNight` payload so `#Preview` for `EveningNightCard` and `DayDetailView` render | Yes |
| Widget consumers (`Flux/FluxWidgets/`) | widgets use `/status`, not `/day` — no impact | No |
| History screen | uses `/history`, not `/day` — no impact | No |
| Other `DayDetailResponse(` literals | grep results above are exhaustive for the working tree at design time; reviewer should re-run the grep before merging in case new call sites have appeared. | — |

### File placement (new code)

| New code | Lives in |
|---|---|
| `findEveningNight`, helpers (`integratePload`, `findFirstLastPpvAbove`) | `internal/api/compute.go` (alongside `findPeakPeriods`) |
| `melbourneSunriseSunset` and Melbourne lat/long constants | `internal/api/compute.go`, in a clearly separated section near the bottom |
| `EveningNight`, `EveningNightBlock` Go structs | `internal/api/response.go` |
| Swift `EveningNight`, `EveningNightBlock`, `BoundarySource` enum | `Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift` |
| `EveningNightCard` SwiftUI view | `Flux/Flux/DayDetail/EveningNightCard.swift` |

## Components and Interfaces

### Backend types (`response.go`)

```go
type EveningNight struct {
    Evening *EveningNightBlock `json:"evening,omitempty"`
    Night   *EveningNightBlock `json:"night,omitempty"`
}

type EveningNightBlock struct {
    Start             string   `json:"start"`              // RFC 3339 UTC
    End               string   `json:"end"`                // RFC 3339 UTC
    TotalKwh          float64  `json:"totalKwh"`
    AverageKwhPerHour *float64 `json:"averageKwhPerHour"`  // nil when elapsed < 60s
    Status            string   `json:"status"`             // "complete" | "in-progress"
    BoundarySource    string   `json:"boundarySource"`     // "readings" | "estimated"
}
```

`DayDetailResponse` adds `EveningNight *EveningNight \`json:"eveningNight,omitempty"\``. Pointer is required because Go's `json` package does not honour `omitempty` on a non-pointer struct.

### Backend function: `findEveningNight`

```go
func findEveningNight(
    readings []dynamo.ReadingItem,
    date string,        // YYYY-MM-DD, the requested calendar date
    today string,       // today's YYYY-MM-DD in sydneyTZ, supplied by the caller
    now time.Time,      // request-scoped clock, supplied by the caller
) *EveningNight
```

**Caller passes `today` and `now`** (`day.go` already computes both at line 28–29). The function does not call `time.Now` or any helper that does — this avoids inventing a `todayString` helper and keeps the function deterministic for tests.

**Invariants:**
- `readings` is sorted ascending by `Timestamp`. Caller guarantees this (DynamoDB `ScanIndexForward: true`).
- All time boundaries are truncated to whole seconds before any subtraction. `now` from the handler is already at second precision; `dayStart`/`dayEnd` come from `time.ParseInLocation` and are second-precision; `melbourneSunriseSunset` returns second-precision (see below).

**Algorithm:**

1. Resolve `dayStart` and `dayEnd` as the Sydney-local midnight boundaries for `date` (use `time.ParseInLocation("2006-01-02", date, sydneyTZ)`; same pattern as `day.go:32`).
2. Walk `readings` once, tracking `firstPpvPositive` and `lastPpvPositive` (first/last reading with `Ppv > 0`).
3. `isToday := (date == today)`.
4. **Build night block** (chronologically first; period is `[dayStart, end)`):
   - `nominalEnd`:
     - If `firstPpvPositive` exists: `nominalEnd = firstPpvPositive.Timestamp`, `boundarySource = "readings"`.
     - Else: `nominalEnd = melbourneSunriseSunset(date, isSunrise=true)`, `boundarySource = "estimated"`.
   - If `isToday && nominalEnd.After(now)`: `end = now`, `status = "in-progress"`. Else `end = nominalEnd`, `status = "complete"`.
   - Final guard: if `dayStart >= end` → omit night block (degenerate / future date).
   - Otherwise build block with `start = dayStart`, `end`, `boundarySource`, `status`.
5. **Build evening block** (chronologically last; period is `[start, dayEnd)`):
   - **Today gate:** if `isToday && now <= melbourneSunriseSunset(date, isSunrise=false)` → omit evening block entirely. The sun hasn't astronomically set yet, so any positive `lastPpvPositive` reading represents ongoing daytime, not an actual sunset transition. This is the rule that distinguishes "evening in progress" from "still daytime".
   - `nominalStart`:
     - If `lastPpvPositive` exists: `nominalStart = lastPpvPositive.Timestamp`, `boundarySource = "readings"`.
     - Else: `nominalStart = melbourneSunriseSunset(date, isSunrise=false)`, `boundarySource = "estimated"`.
   - If `isToday && dayEnd.After(now)`: `end = now`, `status = "in-progress"`. Else `end = dayEnd`, `status = "complete"`.
   - Final guard: if `nominalStart >= end` → omit evening block.
   - Otherwise build block with `start = nominalStart`, `end`, `boundarySource`, `status`.
6. If both blocks are nil: return nil. Else return `&EveningNight{evening, night}`.

**Per-block-fallback semantics for past days with morning-only readings.** A past day where the recorder died at noon has `lastPpvPositive ≈ 12:55` and req 1.5 says "WHEN the evening block has no `ppv > 0` reading on the requested date, set its `start` to the computed sunset". Since 12:55 is a `ppv > 0` reading, fallback does NOT trigger and `nominalStart = 12:55`. Result: `evening` runs 12:55 → 24:00 with `boundarySource = "readings"` and `totalKwh` reflecting load over that 11-hour window. This is per spec; surprising for the "afternoon outage" interpretation, but consistent with the documented decision (Decision 2: noise risk accepted, sample-based boundary preferred).

**Today, pre-sunrise, overcast** (e.g. `now = 04:30`, no `Ppv > 0` yet today): nominal night `end = melbourneSunriseSunset(today, sunrise=true) ≈ 06:30`, which is `After(now)`, so `end = 04:30`, `status = "in-progress"`, `boundarySource = "estimated"`. The `boundarySource` reflects how the *nominal* end was computed; the iOS card uses `status` to decide whether to render the boundary caption (see iOS section), so `"≈ sunrise"` is suppressed in this clamped state.

**Block builder helper** (`buildEveningNightBlock`):

```go
func buildEveningNightBlock(
    readings []dynamo.ReadingItem,
    start, end time.Time,
    boundarySource, status string,
) *EveningNightBlock
```

- Computes `elapsedSeconds = end.Unix() - start.Unix()` (Unix() truncates sub-second already).
- Calls `integratePload(readings, start.Unix(), end.Unix())` for `totalKwh`.
- `AverageKwhPerHour`: if `elapsedSeconds < 60` → `nil` ([req 1.7](#)). Else `floatPtr(roundEnergy(totalKwh / (float64(elapsedSeconds) / 3600.0)))`.
- Rounds `totalKwh` via existing `roundEnergy`.
- Formats `start`/`end` as `time.RFC3339` UTC.

### Backend function: `integratePload`

```go
func integratePload(readings []dynamo.ReadingItem, startUnix, endUnix int64) float64
```

Trapezoidal integration of `max(pload, 0)` over the half-open period `[startUnix, endUnix)` in watt-seconds, returned as kWh.

**Algorithm (precise specification):**

1. Build the working point sequence `pts` (`{ts int64, pload float64}`):
   - Find the largest reading index `iL` with `readings[iL].Timestamp < startUnix` (call this the *left bracket*; `iL = -1` if none).
   - Find the smallest reading index `iR` with `readings[iR].Timestamp >= endUnix` (call this the *right bracket*; `iR = len(readings)` if none).
   - **Left edge** (when `iL >= 0` and `iL+1 < len(readings)` and `readings[iL+1].Timestamp >= startUnix`):
     - Pair gap: `g = readings[iL+1].Timestamp - readings[iL].Timestamp`.
     - If `g <= 60`: interpolate `pload` linearly between `max(readings[iL].Pload, 0)` and `max(readings[iL+1].Pload, 0)` at `startUnix`, append `{startUnix, interpolated}` to `pts`.
     - If `g > 60`: skip the left edge. The bracketing pair is being skipped per the 60s rule and we don't synthesize an edge that would inherit phantom values.
   - **Interior readings**: append every reading whose `Timestamp` is in `[startUnix, endUnix)` (i.e. `Timestamp >= startUnix && Timestamp < endUnix`) as `{Timestamp, max(Pload, 0)}`. A reading exactly at `startUnix` is included (half-open includes start); a reading exactly at `endUnix` is excluded.
   - **Right edge** (when `iR > 0` and `iR < len(readings)` and `readings[iR-1].Timestamp < endUnix`):
     - Pair gap: `g = readings[iR].Timestamp - readings[iR-1].Timestamp`.
     - If `g <= 60`: interpolate between `max(readings[iR-1].Pload, 0)` and `max(readings[iR].Pload, 0)` at `endUnix`, append `{endUnix, interpolated}` to `pts`.
     - If `g > 60`: skip the right edge.
2. **Negative-pload clamp ordering: clamp before interpolation.** `max(pload, 0)` is applied to each bracket reading's `pload` before the linear interpolation. This is consistent with `computeTodayEnergy` clamping each pair-value with `max(prev.Pload, 0)` before averaging.
3. If `len(pts) < 2`: return 0.
4. Sum trapezoidal areas across **adjacent point pairs** in `pts`:
   - For each adjacent pair `(a, b)`:
     - `dt = b.ts - a.ts` (always > 0; `pts` is monotonic ascending by construction).
     - **No 60s skip is applied between pairs in `pts`.** The 60s rule was already applied at the bracket level. Within the period, the readings are at native ~10s cadence, so any internal gap > 60s is a real outage and the corresponding trapezoid contributes proportionally — same behaviour as `computeTodayEnergy` once it's past the per-pair skip (which we keep at the brackets only).
     - Wait — `computeTodayEnergy` *does* apply `dt > 60` skip between every adjacent pair. To stay consistent, `integratePload` SHALL apply the same `dt > 60` skip on every adjacent pair in `pts`, including pairs synthesized at the brackets. **This is the canonical rule.** The bracket-level rule above is the *additional* guard that prevents synthesizing an edge from a pair that would itself be skipped (which would silently produce a single-sided trapezoid against the next interior reading and over-count).
     - Energy contribution: `((a.pload + b.pload) / 2) * dt` watt-seconds.
   - Total watt-seconds / 3,600,000 = kWh.
5. Return kWh (no rounding inside this function — caller rounds at serialization).

**Worked example.** Readings (in seconds since dayStart): `(t=0, pload=200), (t=10, pload=400), (t=20, pload=-100), (t=30, pload=600)`. Period `[15, 25)`.
- Left bracket: `iL = 1` (`t=10 < 15`). Pair gap `20 - 10 = 10 <= 60`. Clamp: `max(400,0)=400`, `max(-100,0)=0`. Interpolate at `t=15`: `400 + (0-400) * (15-10)/(20-10) = 400 + (-400)*0.5 = 200`. Append `{15, 200}`.
- Interior: reading at `t=20` is in `[15, 25)` → append `{20, 0}` (clamped).
- Right bracket: `iR = 3` (`t=30 >= 25`). Pair gap `30 - 20 = 10 <= 60`. Clamp: `max(0)=0`, `max(600,0)=600`. Interpolate at `t=25`: `0 + (600-0)*(25-20)/(30-20) = 300`. Append `{25, 300}`.
- `pts = [{15,200}, {20,0}, {25,300}]`.
- Trapezoids: `((200+0)/2)*5 = 500 W·s`, `((0+300)/2)*5 = 750 W·s`. Total `1250 W·s = 0.000347 kWh`.

This worked example MUST appear verbatim as a test case in `compute_test.go`.

**Edge cases nailed down:**

| Case | Behaviour |
|---|---|
| `start` precedes every reading (e.g. midnight when first reading is at 00:00:05) | No left bracket; no left-edge synthesis; integration starts at the first interior reading. Under-counts by at most one interval (~5s). Acceptable. |
| `end` follows every reading (e.g. recorder died, period extends past last reading) | No right bracket; no right-edge synthesis; integration ends at the last interior reading. |
| `start == reading.Timestamp` for some reading | That reading is interior (half-open includes start). Left edge is skipped because `readings[iL+1].Timestamp == startUnix`, so interpolating at `startUnix` would just reproduce the interior reading — we use the interior reading directly. The check `readings[iL+1].Timestamp >= startUnix` is `>=`; when it equals `startUnix`, skip the synthesis (otherwise we'd duplicate that point). Implementation: if `readings[iL+1].Timestamp == startUnix`, skip left edge synthesis. |
| `end == reading.Timestamp` for some reading | That reading is excluded (half-open). Right bracket is at `iR = that-index`. Right-edge synthesis uses `readings[iR-1] → readings[iR]` interpolated at `endUnix` (which equals `readings[iR].Timestamp`), so the interpolated `pload = readings[iR].Pload`. Append `{endUnix, that-pload}`. Trapezoid against the previous interior reading contributes normally. |
| Zero readings inside `[start, end)` | Possible to have valid left and right edges but no interior. `pts` then has 2 points (the synthesized edges); a single trapezoid is integrated. |
| Single interior reading, no usable brackets | `len(pts) == 1`, return 0. Block still emitted with `totalKwh = 0`. |

### Backend function: `melbourneSunriseSunset`

```go
func melbourneSunriseSunset(date string, isSunrise bool) time.Time
```

Returns Melbourne sunrise (`isSunrise=true`) or sunset (`false`) for `date` (YYYY-MM-DD), as a `time.Time` in UTC truncated to the second. Backed by an embedded **static lookup table of UTC times keyed by `MM-DD`**, not a runtime calculation.

**Storage format:**

```go
// internal/api/melbourne_sun_table.go (generated once, see "Table generation" below).
var melbourneSunUTC = map[string]struct {
    riseUTC string // "HH:MM" UTC
    setUTC  string // "HH:MM" UTC
}{
    "01-01": {"18:54", "09:42"},  // 05:54 AEDT / 20:42 AEDT
    "01-02": {"18:55", "09:42"},
    // ... 366 entries (Feb-29 reuses Feb-28's values; difference is in seconds)
    "12-31": {"18:54", "09:41"},
}
```

**Lookup logic:**

```go
func melbourneSunriseSunset(date string, isSunrise bool) time.Time {
    // date is "YYYY-MM-DD"; key is "MM-DD".
    key := date[5:]
    entry, ok := melbourneSunUTC[key]
    if !ok {
        // Feb-29 fallback (lookup will only miss on Feb-29 if the table omitted it).
        entry = melbourneSunUTC["02-28"]
    }
    hhmm := entry.setUTC
    if isSunrise {
        hhmm = entry.riseUTC
    }
    // Parse "HH:MM" and combine with the calendar date in UTC.
    // The astronomical event happens at the same UTC date for sunrise (UTC morning ≈ Melbourne afternoon-prior, depending on hour),
    // BUT this is not a problem because: sunrise UTC for Melbourne is in the previous UTC day for AEDT mornings.
    // To keep the implementation straightforward, the table stores BOTH events as UTC times relative to the LOCAL date —
    // i.e. the UTC instant of "sunrise on local Wednesday Jan 1" goes under key "01-01" even though that instant is "Tue Dec 31 18:54 UTC".
    // The function returns that UTC instant by interpreting the HH:MM as the offset to apply within a 24-hour reference window
    // anchored to the local-date midnight UTC. See "Reference anchor" below.
    return computeUTCInstant(date, hhmm)
}
```

**Reference anchor.** Storing both rise and set as raw UTC `HH:MM` strings is ambiguous because Melbourne sunrise (~20:54 UTC = 05:54 AEDT next-local-day) crosses the UTC date boundary. To remove the ambiguity, the table stores values as **minutes-since-Sydney-local-midnight on the requested date**, written in `HH:MM` form (so `05:54` means "5h54m after midnight Sydney-local on that date"). The function converts:

```go
func computeUTCInstant(date, hhmmLocal string) time.Time {
    h, m := parseHHMM(hhmmLocal)
    // dayStart is Sydney-local midnight for `date`, identical to the boundary handleDay computes (day.go:32).
    dayStart, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)
    return dayStart.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute).UTC()
}
```

This is **DST-immune** because `sydneyTZ` resolves the local-midnight wall-clock to the correct UTC instant for the year (`time.ParseInLocation` reads the IANA database and applies AEDT or AEST as appropriate for the given calendar date). The table stores wall-clock-style values that look like local time (`05:54` for sunrise, `20:42` for sunset), and Go's standard library handles the AEDT/AEST conversion at lookup time — same year-after-year, no table refresh needed across DST rule changes.

**Why "Sydney-local clock-style" and not raw UTC?** It makes the table human-readable and matches what an astronomical calculator outputs ("Melbourne sunrise on January 1, 2026 is 05:54 AEDT"). The values vary < 1 minute year-over-year on the same MM-DD, well inside the ±2-minute tolerance ([req 1.12](#)), so the table does not need to be regenerated even across DST rule changes (which would only affect the displayed clock time, not the stored values, because `time.ParseInLocation` always uses the *current* Australian DST rules).

**Edge cases:**

| Case | Behaviour |
|---|---|
| Feb 29 lookup | Falls back to Feb 28 values (mechanical map miss → fallback in code). |
| Date format invalid (e.g. `"2026-13-45"`) | Caller (`day.go`) already validates date format before calling. Lookup would fail; defensive fallback returns `dayStart` itself, which is benign because `findEveningNight` only uses this on the fallback path that won't trigger for invalid dates. |
| Future Australian DST rule change | Would shift the wall-clock display but not the stored values. Existing IANA database updates (shipped with Go releases) handle the transition automatically. No table change needed. |

**Table generation** (one-time scripting task, not part of runtime code):

The table is generated by querying any astronomical calculator (e.g. Geoscience Australia online almanac, `timeanddate.com`, or a one-off Python script using `astral` / `pyephem`) for Melbourne's 366 dates of sunrise/sunset times in `Australia/Melbourne` local time, then writing them into `melbourne_sun_table.go` as the literal map shown above. The generation script is not committed; the generated `.go` file is the source of truth. A code comment at the top of the file records the generation date and source for traceability.

**Why no test against an external reference?** The table *is* the reference. Year-over-year drift is below the ±2-minute tolerance and below the table's own precision (HH:MM, no seconds). A "sanity test" that asserts a couple of dates land in plausible ranges (e.g. summer-solstice sunset is between 19:30 and 20:30 AEDT) is sufficient to catch a generation script error or an accidental table corruption.

### iOS types (`APIModels.swift`)

```swift
public struct EveningNight: Codable, Sendable {
    public let evening: EveningNightBlock?
    public let night: EveningNightBlock?

    public var hasAnyBlock: Bool { evening != nil || night != nil }

    public init(evening: EveningNightBlock?, night: EveningNightBlock?) { ... }
}

public struct EveningNightBlock: Codable, Sendable, Identifiable {
    public enum Status: String, Codable, Sendable { case complete, inProgress = "in-progress" }
    public enum BoundarySource: String, Codable, Sendable { case readings, estimated }

    public let start: String
    public let end: String
    public let totalKwh: Double
    public let averageKwhPerHour: Double?
    public let status: Status
    public let boundarySource: BoundarySource

    public var id: String { start }   // unique per response (one evening, one night)
    public init(...) { ... }
}
```

`DayDetailResponse` gains `public let eveningNight: EveningNight?`. The synthesised memberwise init grows one parameter; mocks/tests pass `eveningNight: nil` to opt out.

### iOS view: `EveningNightCard`

Mirrors `PeakUsageCard` styling: `.thinMaterial` background, `RoundedRectangle(cornerRadius: 16)`, `.headline` title, `.subheadline` rows.

**Layout.** Each row is a two-line layout (a `VStack(alignment: .leading, spacing: 2)` containing two `HStack`s):

```
┌──────────────────────────────────────────┐
│ Night                       18:30 – 24:00│   line 1 (label leading, time trailing)
│ ≈ sunrise                4.2 kWh · 0.85/h│   line 2 (caption leading, totals trailing)
└──────────────────────────────────────────┘
```

This avoids the four-piece-on-one-row layout problem on a 320pt-wide screen. Both lines use `.subheadline` for the primary content and `.caption.foregroundStyle(.secondary)` for the leading caption on line 2.

**Caption rules (line 2 leading):**
- If `status == .inProgress`: show `(so far)`. The boundary `≈ sunrise`/`≈ sunset` caption is *suppressed* in this state, because for an in-progress block the visible `end` is `now`, not the nominal sunrise/sunset — showing `≈ sunrise` would mislabel the displayed time. The `boundarySource` field on the wire still reflects how the nominal end was computed, but the iOS card prioritises accurate UI labelling.
- Else if `boundarySource == .estimated`: show `≈ sunset` for the evening row (because the Evening block's `start` is the computed boundary) or `≈ sunrise` for the night row (because the Night block's `end` is the computed boundary).
- Else (status complete, boundarySource readings): line 2's leading caption is empty (the `HStack` still renders for layout consistency, with a leading `Spacer` of zero width).

**Totals format (line 2 trailing):** `String(format: "%.1f kWh · %.2f kWh/h", totalKwh, avg)`. When `averageKwhPerHour == nil` (elapsed < 60s): show only `String(format: "%.1f kWh", totalKwh)`.

**Time range (line 1 trailing):** `start` and `end` parsed via `DateFormatting.parseTimestamp` and rendered with `clockTime24h` in `sydneyTZ`. Format: `"00:00 – 06:14"`.

**Row order: Night first, Evening second.** Chronological for the calendar date being viewed (00:00→sunrise, then sunset→24:00). The card title stays `"Evening / Night"` regardless of which blocks are present.

**Code skeleton:**

```swift
struct EveningNightCard: View {
    let eveningNight: EveningNight

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Evening / Night").font(.headline)
            if let night = eveningNight.night   { row(label: "Night",   block: night) }
            if let evening = eveningNight.evening { row(label: "Evening", block: evening) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func row(label: String, block: EveningNightBlock) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(label).font(.subheadline)
                Spacer()
                Text(timeRange(block)).font(.subheadline)
            }
            HStack {
                Text(secondaryCaption(block)).font(.caption).foregroundStyle(.secondary)
                Spacer()
                Text(totals(block)).font(.subheadline).foregroundStyle(.secondary)
            }
        }
    }
    // helpers: secondaryCaption(_:), timeRange(_:), totals(_:)
}
```

**Visibility guard (in `DayDetailView`):** `if viewModel.hasPowerData, let en = viewModel.eveningNight, en.hasAnyBlock { EveningNightCard(eveningNight: en) }`. The card is hidden when `viewModel.eveningNight == nil`, when neither block is present (`hasAnyBlock == false`), or when `!viewModel.hasPowerData`.

**Past-day-zero-evening case.** A past day where the recorder died at 12:55 produces `evening` with `start = 12:55, end = 24:00, totalKwh ≈ load over 11h`. This is correct per spec; the card simply renders the long span with the actual usage. No special treatment in the card.

**Localisation and accessibility.** The card is shipped English-only. VoiceOver inherits the default behaviour from the `Text` views (the row reads "Night, eighteen thirty to midnight, four point two kWh, point eight five per hour"). No explicit `accessibilityLabel` is added — matching `PeakUsageCard`, which also relies on default behaviour.

### iOS view-model wiring

`DayDetailViewModel` adds:

```swift
private(set) var eveningNight: EveningNight?
```

In `loadDay()` success path: `eveningNight = response.eveningNight`. In error path: `eveningNight = nil`.

`DayDetailView` placement, after the existing PeakUsageCard guard, before `summaryCard`:

```swift
if viewModel.hasPowerData,
   let eveningNight = viewModel.eveningNight,
   eveningNight.hasAnyBlock {
    EveningNightCard(eveningNight: eveningNight)
}
```

If PeakUsageCard is hidden (no peaks), the EveningNightCard floats up to that slot — same behaviour as the existing PeakUsageCard sliding into Summary's slot when absent.

## Data Models

Covered above under Components and Interfaces. No DynamoDB schema changes ([req 1.13](#)).

## Error Handling

| Failure mode | Behaviour |
|---|---|
| `len(readings) == 0` | Caller skips `findEveningNight` (parallel to `findPeakPeriods` gating); `eveningNight` field omitted via `omitempty`. |
| Daily-power fallback path | `findEveningNight` not invoked ([req 1.11](#)); field omitted. |
| Today, evening period not yet begun (sun still up) | Evening block omitted by step 5's today gate; only night block is returned. |
| Today, evening built but elapsed < 60s | Block emitted with `totalKwh = 0` (likely) and `averageKwhPerHour = nil`. |
| Night block on a future-dated request | `dayStart > now` is possible only for a future date. Caller already gates on `len(readings) > 0`; future dates have no readings, so `findEveningNight` is not called. |
| `melbourneSunriseSunset` lookup miss | Only possible on Feb 29 (table omits it intentionally). Code falls back to Feb 28's values. |
| `melbourneSunriseSunset` called with malformed `date` | Caller (`day.go`) validates date format before invoking `findEveningNight`. Defensive: returns `dayStart` itself if `time.ParseInLocation` fails. Block helper handles `start == end` via the `start >= end` final guard. |
| `integratePload` returns 0 (no qualifying reading pairs / sparse data) | Block still emitted with `totalKwh = 0`. The user sees the boundaries; absence of usage is meaningful information. |
| `start >= end` after all clamping | Block omitted (final guards in steps 4–5). Prevents pathological zero-duration / inverted blocks from rendering. |

## Testing Strategy

### Backend unit tests (`compute_test.go`)

Map-based table-driven tests, one map per function.

`TestFindEveningNight` covers every case from [req 4.1](#):

| Test name | Input | Expected |
|---|---|---|
| typical day, both periods complete | full day, sunrise 06:30, sunset 18:00 | both blocks; `boundarySource = readings`; `status = complete` |
| today, before sunrise | now=04:30, no `Ppv>0` yet | only night; `end = now`; `status = in-progress` |
| today, after sunset | now=22:00, sunrise 06:30, sunset 18:30 | both; evening `status = in-progress`; night `complete` |
| today, midday | now=13:00, sunrise 06:30, no sunset yet | only night; evening omitted (its start is in the future) |
| overcast day, no `Ppv>0` | no positive ppv | both blocks; both `boundarySource = estimated`; both `status = complete` |
| morning solar but no afternoon (recorder died) | `Ppv>0` only before noon | night = readings, evening = estimated |
| zero readings inside the period | sparse data | block emitted with `totalKwh = 0`; `averageKwhPerHour` non-nil |
| 60s gap rule | reading pair gap > 60s | that pair skipped, totals exclude it |
| in-progress evening, elapsed < 60s | now is one second past sunset | block emitted; `averageKwhPerHour = nil` |
| boundary clamp clamps `end` to `now` | today, future end | `end == now` |

`TestIntegratePload` covers `[start, end)` half-open behaviour, edge interpolation, and the 60s rule. The worked example from the design (period `[15, 25)` over readings at `t=0,10,20,30`) is one of the table cases.

`TestMelbourneSunriseSunset` is a sanity test, not an external-reference comparison (the table *is* the reference):
- Lookup of `2026-06-21` (winter solstice) returns sunset between 16:30 and 17:30 AEST.
- Lookup of `2026-12-22` (summer solstice) returns sunset between 20:00 and 21:00 AEDT.
- Lookup of `2027-02-29` (leap year) falls back to Feb 28's values.
- Lookup of `2026-04-05` (the day AEDT typically ends) returns a UTC instant correctly resolved by `time.ParseInLocation` regardless of which side of the DST transition the time falls.

### Backend integration tests (`day_test.go`)

Extend `TestHandleDayNormalCase` to assert `eveningNight` is present and non-nil, and add:
- `eveningNight` absent on the daily-power fallback path.
- `eveningNight` absent when no readings exist.
- Per-block fallback exercised via a fixture with morning-only readings.

### Property-based testing

Skipped. The clustering-style invariants of `findPeakPeriods` (non-overlapping, descending energy, count ≤ N) suited PBT; here the function is essentially "scan once, integrate twice" with no algebraic invariants worth a generator. Existing example-based tables cover the boundary-condition surface.

### iOS tests

`APIModelsTests.swift`:
- Decode response with `eveningNight` present (both blocks) — fields populated, enums parse.
- Decode response with `eveningNight` field absent — `eveningNight == nil`.
- Decode response with only one block present (`evening` key absent) — `evening == nil`, `night != nil`.
- Decode response with `averageKwhPerHour == null` — Swift `Double?` is nil.

`DayDetailViewModel` tests ([req 4.3](#)):
- Response with both blocks → `viewModel.eveningNight` populated.
- Response with only one block → other side nil.
- Response with `eveningNight` absent → `viewModel.eveningNight == nil`.
- Response with `boundarySource: "estimated"` → string passes through.
- Fallback-data path → existing test extended to assert `eveningNight == nil` (per backend invariant).
- Error path → `eveningNight` reset to nil.

`MockFluxAPIClient.previewData` extended with a realistic `eveningNight` payload so SwiftUI previews render the new card.

### Benchmark

A `BenchmarkFindEveningNight` over 8640 readings, mirroring `BenchmarkFindPeakPeriods`. Acceptance bar: same order of magnitude as `findPeakPeriods` (the algorithm is structurally simpler — single pass + two integrations).
