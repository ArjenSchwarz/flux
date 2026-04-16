# Design: Peak Usage Periods

## Overview

This feature adds peak usage period detection to the `/day` API endpoint and displays the results on the iOS day detail screen. The backend scans raw 10-second readings (already fetched for downsampling), identifies periods where household load (Pload) exceeds the day's mean outside the off-peak window, and returns the top 3 periods ranked by energy consumed. The iOS app renders these as a card between the charts and the summary card.

The design adds one new function to `compute.go`, one new type to `response.go`, wiring in `day.go`, and a new SwiftUI view with corresponding model updates.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│  handleDay()  (day.go)                              │
│                                                     │
│  1. Fetch raw readings + daily energy (existing)    │
│  2. findMinSOC(readings)              (existing)    │
│  3. downsample(readings, date)        (existing)    │
│  4. findPeakPeriods(readings, ...)    ← NEW         │
│  5. Build DayDetailResponse                         │
│     └─ peakPeriods: []PeakPeriod      ← NEW field   │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  iOS: DayDetailView                                 │
│                                                     │
│  PowerChartView           (existing)                │
│  BatteryPowerChartView    (existing)                │
│  SOCChartView             (existing)                │
│  PeakUsageCard            ← NEW                     │
│  summaryCard              (existing)                │
└─────────────────────────────────────────────────────┘
```

No new DynamoDB queries are required. The computation uses the same `[]dynamo.ReadingItem` already fetched by `handleDay`.

## Components and Interfaces

### Backend

#### New type: `PeakPeriod` (response.go)

```go
// PeakPeriod represents a contiguous period of high household load.
type PeakPeriod struct {
    Start    string  `json:"start"`    // RFC 3339
    End      string  `json:"end"`      // RFC 3339
    AvgLoadW float64 `json:"avgLoadW"` // average Pload, rounded to 1 decimal
    EnergyWh float64 `json:"energyWh"` // total energy, rounded to whole number
}
```

Req trace: [2.2](#2.2), [1.11](#1.11)

#### Modified type: `DayDetailResponse` (response.go)

Add a `PeakPeriods` field:

```go
type DayDetailResponse struct {
    Date        string            `json:"date"`
    Readings    []TimeSeriesPoint `json:"readings"`
    Summary     *DaySummary       `json:"summary"`
    PeakPeriods []PeakPeriod      `json:"peakPeriods"` // NEW — never null
}
```

Req trace: [2.1](#2.1), [2.3](#2.3), [2.4](#2.4)

#### New function: `findPeakPeriods` (compute.go)

```go
func findPeakPeriods(
    readings []dynamo.ReadingItem,
    offpeakStart, offpeakEnd string,
) []PeakPeriod
```

**Parameters:**
- `readings` — raw readings for the day, time-sorted ascending (same slice used by `downsample` and `findMinSOC`)
- `offpeakStart`, `offpeakEnd` — "HH:MM" strings from `h.offpeakStart` / `h.offpeakEnd`

**Named constants:**

```go
const (
    mergeGapSeconds    = 300 // max gap between clusters to merge (5 minutes)
    minPeriodSeconds   = 120 // minimum period duration (2 minutes)
    maxPairGapSeconds  = 60  // max gap between reading pairs for energy integration
    maxPeakPeriods     = 3   // maximum number of peak periods to return
)
```

**Algorithm (5 steps):**

1. **Parse off-peak window** — Convert "HH:MM" strings to `offpeakStartMinute` and `offpeakEndMinute` (as `hour*60 + minute`). If parsing fails or `startMinute >= endMinute`, treat as no off-peak window (include all readings). Overnight windows (start > end) are not supported. Req trace: [1.2](#1.2)

2. **Compute threshold** — Single pass over the original `readings` slice:
   - For each reading, convert its Unix timestamp to Sydney local time and extract `minuteOfDay = hour*60 + minute`.
   - Skip readings where `minuteOfDay >= offpeakStartMinute AND minuteOfDay < offpeakEndMinute`.
   - Accumulate sum and count of Pload from non-off-peak readings.
   - Compute `threshold = sum / count`. If count is 0, return empty slice.
   - Req trace: [1.2](#1.2), [1.3](#1.3)

3. **Build initial clusters** — Second pass over the **original `readings` slice** (not a pre-filtered subset — this is critical to preserve temporal adjacency):
   - Walk in timestamp order. For each reading, check three conditions: is it off-peak? Is `Pload <= threshold`? Either condition closes the current cluster.
   - Only readings that are both non-off-peak AND above-threshold extend or start a cluster.
   - Each cluster tracks: `startIdx`, `endIdx` (indices into the `readings` slice), sum of Pload values, count of readings.
   - This prevents false clusters across the off-peak gap: a reading at 10:59 and one at 14:01 will never be grouped, because the off-peak readings between them close the cluster.
   - Req trace: [1.4](#1.4)

4. **Merge clusters** — Use the accumulator-based merge-intervals pattern:
   ```
   merged = [clusters[0]]
   for each cluster c in clusters[1:]:
       last = &merged[len(merged)-1]
       gap = readings[c.startIdx].Timestamp - readings[last.endIdx].Timestamp
       if gap <= mergeGapSeconds:
           last.endIdx = c.endIdx
           last.sum += c.sum
           last.count += c.count
       else:
           append c to merged
   ```
   After merging, the comparison continues against the **extended** merged cluster, enabling transitive merges (A+B merge, then AB+C merges if within 5 min). After all merges, discard any period where `readings[endIdx].Timestamp - readings[startIdx].Timestamp < minPeriodSeconds`. Req trace: [1.5](#1.5), [1.6](#1.6)

5. **Compute energy and rank** — For each surviving period:
   - **Energy:** Trapezoidal integration over `readings[startIdx..endIdx]` (all readings within the period's index range, including below-threshold readings bridged during merge). Skip reading pairs where `curr.Timestamp - prev.Timestamp > maxPairGapSeconds`. This uses `max(Pload, 0)` to clamp any unexpected negative values.
   - **Average Pload:** Computed from the cluster accumulators (`sum / count`), which only include above-threshold readings. This avoids dilution from low-usage gap readings that were bridged during merge.
   - Discard periods where integrated energy is zero (can happen when all reading pairs have gaps > 60s).
   - Sort descending by **unrounded** energy. Return the top `maxPeakPeriods`.
   - Req trace: [1.7](#1.7), [1.8](#1.8), [1.9](#1.9), [1.10](#1.10)

**Index-based period tracking:** By storing `startIdx`/`endIdx` into the `readings` slice, step 5 iterates `readings[startIdx:endIdx+1]` directly. This is simpler than timestamp-based re-scanning and structurally prevents off-peak readings from leaking in — the indices bound an exact contiguous sub-slice.

**Off-peak overlap guarantee:** No cluster can start or end on an off-peak reading, because step 3 treats off-peak readings as cluster-breakers. Since merge only extends `endIdx` to another cluster's boundary (which is also non-off-peak), no period's index range can include off-peak readings. The only readings included within a merged period that weren't in the original clusters are below-threshold non-off-peak readings from the merge gap.

**Return value:** Each `PeakPeriod` has:
- `Start` / `End` — RFC 3339 UTC strings (converted from Unix timestamps via `time.Unix(ts, 0).UTC().Format(time.RFC3339)`, matching existing patterns in `downsample` and `findMinSOC`)
- `AvgLoadW` — `roundPower(sum / count)` from cluster accumulators (1 decimal place, above-threshold readings only)
- `EnergyWh` — `math.Round(energyWh)` (whole number, computed from all readings in index range)

Sorting uses unrounded energy to avoid arbitrary ranking of periods that round to the same value.

Req trace: [1.11](#1.11), [1.12](#1.12)

#### Modified handler: `handleDay` (day.go)

After the existing `findMinSOC` and `downsample` calls, add:

```go
var peakPeriods []PeakPeriod
if len(readings) > 0 {
    peakPeriods = findPeakPeriods(readings, h.offpeakStart, h.offpeakEnd)
}
if peakPeriods == nil {
    peakPeriods = []PeakPeriod{}
}
```

Assign to `resp.PeakPeriods`. Req trace: [1.1](#1.1), [1.13](#1.13), [2.3](#2.3), [2.5](#2.5)

### iOS

#### New model: `PeakPeriod` (APIModels.swift)

```swift
struct PeakPeriod: Codable, Sendable, Identifiable {
    let start: String
    let end: String
    let avgLoadW: Double
    let energyWh: Double

    var id: String { start }
}
```

#### Modified model: `DayDetailResponse` (APIModels.swift)

Add `peakPeriods` field as optional (decoded as `nil` when the key is absent, for backwards compatibility):

```swift
struct DayDetailResponse: Codable, Sendable {
    let date: String
    let readings: [TimeSeriesPoint]
    let summary: DaySummary?
    let peakPeriods: [PeakPeriod]?  // NEW — nil when backend predates this feature
}
```

The ViewModel nil-coalesces to `[]` on assignment. This preserves the synthesised memberwise init for mocks and tests.

Req trace: [2.1](#2.1)

#### Modified ViewModel: `DayDetailViewModel` (DayDetailViewModel.swift)

Add a published property:

```swift
private(set) var peakPeriods: [PeakPeriod] = []
```

In `loadDay()`, after setting `summary`, add:

```swift
peakPeriods = response.peakPeriods ?? []
```

In the error path, reset:

```swift
peakPeriods = []
```

#### New view: `PeakUsageCard` (DayDetail/PeakUsageCard.swift)

A new file containing a SwiftUI view that matches the existing `summaryCard` styling.

```swift
struct PeakUsageCard: View {
    let periods: [PeakPeriod]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Peak Usage")
                .font(.headline)

            ForEach(periods) { period in
                periodRow(period)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }
}
```

Each row displays:
- **Time range** — Parse `start` and `end` RFC 3339 strings to `Date` using `DateFormatting.parseTimestamp`. Format with a dedicated 24-hour formatter (`DateFormatter` with `dateFormat = "HH:mm"` and `timeZone = sydneyTimeZone`) rather than the locale-dependent `clockTime` formatter, which uses `timeStyle = .short` and may produce 12-hour format depending on device locale. Display as "07:15 – 07:45". Req trace: [3.3](#3.3), [3.6](#3.6)
- **Average load** — `String(format: "%.1f kW", period.avgLoadW / 1000)`. Always kW. Req trace: [3.3](#3.3), Decision 8
- **Energy** — Format `energyWh` as whole number with grouping separator (e.g., "2,100 Wh"). Req trace: [3.3](#3.3)

Styling matches the existing summary card: `.thinMaterial` background, `RoundedRectangle(cornerRadius: 16, style: .continuous)`, `.headline` for the title, `.subheadline` for rows. Req trace: [3.4](#3.4)

#### Modified view: `DayDetailView` (DayDetailView.swift)

Insert `PeakUsageCard` between the SOC chart and the summary card:

```swift
// After SOCChartView, before summaryCard:
if viewModel.hasPowerData && !viewModel.peakPeriods.isEmpty {
    PeakUsageCard(periods: viewModel.peakPeriods)
}
```

Req trace: [3.1](#3.1), [3.2](#3.2), [3.5](#3.5)

## Data Models

### API contract change

**Before:**
```json
{
  "date": "2026-04-15",
  "readings": [...],
  "summary": {...}
}
```

**After:**
```json
{
  "date": "2026-04-15",
  "readings": [...],
  "summary": {...},
  "peakPeriods": [
    {
      "start": "2026-04-15T07:15:00Z",
      "end": "2026-04-15T07:45:00Z",
      "avgLoadW": 4200.3,
      "energyWh": 2100
    }
  ]
}
```

The only change is the addition of `peakPeriods`. All existing fields retain their types and semantics. Req trace: [2.4](#2.4)

### DynamoDB — No changes

No new tables, indexes, or attributes. The feature reads from `flux-readings` (already queried by `handleDay`). Req trace: [1.13](#1.13)

## Error Handling

| Scenario | Behaviour | Req |
|---|---|---|
| Off-peak window strings unparseable | Treat as no off-peak window (include all readings in computation) | Defensive |
| Zero non-off-peak readings | Return empty `peakPeriods` array | [1.12](#1.12) |
| All readings below threshold | Return empty `peakPeriods` array | [1.12](#1.12) |
| Fallback data (no raw readings) | `findPeakPeriods` is not called; `peakPeriods` is `[]PeakPeriod{}` | [2.5](#2.5) |
| Readings query fails | Existing 500 error handler; `peakPeriods` never computed | Existing |
| iOS receives response without `peakPeriods` key | Swift `Codable` decoding: field must exist. For older backend versions before this feature is deployed, add a `CodingKeys` default or use `decodeIfPresent` with a fallback to `[]` | [2.4](#2.4) |

For the iOS backwards-compatibility concern (last row): since the iOS app and backend are both deployed by the same person, they'll be updated together. However, for safety, declare `peakPeriods` as optional with a default and expose a non-optional computed property. This avoids a custom `init(from:)` (which would suppress the memberwise init needed by mocks and tests):

```swift
struct DayDetailResponse: Codable, Sendable {
    let date: String
    let readings: [TimeSeriesPoint]
    let summary: DaySummary?
    let peakPeriods: [PeakPeriod]?
}
```

The ViewModel assigns via `response.peakPeriods ?? []`, keeping the nil-coalescing in one place. Mocks and tests can construct `DayDetailResponse` directly using the synthesised memberwise init, passing `peakPeriods: [...]` or `peakPeriods: nil`.

## Testing Strategy

### Backend (Go)

**Unit tests in `compute_test.go`** — map-based table-driven tests following the existing pattern:

| Test case | What it verifies | Req |
|---|---|---|
| Empty readings | Returns empty slice | [1.12](#1.12) |
| All readings in off-peak window | Returns empty slice | [1.12](#1.12) |
| Uniform load (all equal to mean) | Returns empty slice (strict >) | [1.3](#1.3), [1.4](#1.4) |
| Single peak period above mean | Returns 1 period with correct energy/avg | [1.4](#1.4), [1.7](#1.7), [1.11](#1.11) |
| Two clusters within 5 min merged | Returns 1 period spanning both | [1.5](#1.5) |
| Two clusters >5 min apart remain separate | Returns 2 periods | [1.5](#1.5) |
| Period under 2 minutes discarded | Returns empty or fewer periods | [1.6](#1.6) |
| More than 3 qualifying periods | Returns only top 3 by energy | [1.8](#1.8), [1.9](#1.9) |
| Gap >60s within period skips that pair in energy calc | Energy excludes phantom accumulation | [1.7](#1.7) |
| Off-peak boundary: reading at exactly offpeakStart excluded, reading at offpeakEnd included | Correct boundary behaviour | [1.2](#1.2) |
| Energy ranking is descending | First period has highest energy | [1.8](#1.8) |
| Off-peak boundary clustering: above-threshold readings at 10:59 and 14:01 | Must NOT form a single cluster (off-peak gap breaks them) | [1.2](#1.2), [1.4](#1.4) |
| Transitive merge: 3 clusters where A+B proximity creates AB+C proximity | All three merge into one period | [1.5](#1.5) |
| Zero-energy sparse period: all reading pairs have >60s gaps | Period discarded despite passing duration filter | [1.7](#1.7) |
| Off-peak parse failure (invalid HH:MM) | Treats as no off-peak window, includes all readings | Defensive |
| Negative Pload values in readings | Clamped to 0, don't corrupt threshold or energy | Defensive |
| Two periods with same rounded energy | Stable ranking by unrounded energy | [1.8](#1.8) |

**Property-based tests** — The clustering algorithm has properties well-suited to PBT using `pgregory.net/rapid`:

| Property | Description |
|---|---|
| Result count <= 3 | For any input, output length is 0-3 |
| All periods outside off-peak | No returned period's start or end falls within the off-peak window |
| Non-overlapping | When periods are sorted by start time, no time ranges overlap |
| Energy is positive | All energyWh values > 0 (zero-energy periods are discarded) |
| Descending energy order | periods[i].energyWh >= periods[i+1].energyWh (unrounded) |
| Duration >= 2 minutes | All returned periods have end - start >= 120 seconds |

Generator: produce random `[]dynamo.ReadingItem` slices with timestamps spanning a day at ~10s intervals, random Pload values (0-10000W), and random off-peak windows (with start < end).

**Integration test in `day_test.go`** — Add a test case to `TestHandleDayNormalCase` verifying:
- `peakPeriods` field is present and non-null in the JSON response
- Peak periods are empty when expected (fallback data test)

**Benchmark** — `BenchmarkFindPeakPeriods` with 8640 readings (full day), following the existing `BenchmarkDownsample` pattern.

### iOS (Swift)

**Unit tests in `DayDetailViewModelTests.swift`**:

| Test case | What it verifies | Req |
|---|---|---|
| `loadDay` populates `peakPeriods` from response | ViewModel stores periods correctly | [3.1](#3.1) |
| `loadDay` with empty `peakPeriods` leaves array empty | ViewModel handles zero periods | [3.2](#3.2) |
| `loadDay` error clears `peakPeriods` | Reset on failure | Defensive |
| Response without `peakPeriods` key decodes to empty array | Backwards compat | [2.4](#2.4) |

**Mock updates:**
- `MockFluxAPIClient.dayDetailResponse(for:)` — Add sample `peakPeriods` to the preview data
- `MockDayDetailAPIClient` in tests — Include `peakPeriods` in test responses

### Requirement Traceability Matrix

| Req | Design element | Test coverage |
|---|---|---|
| [1.1](#1.1) | `handleDay` calls `findPeakPeriods` on raw readings | Integration test |
| [1.2](#1.2) | Step 2 off-peak filter | Unit: all-offpeak, boundary |
| [1.3](#1.3) | Step 2 mean computation | Unit: uniform load |
| [1.4](#1.4) | Step 3 initial clustering | Unit: single peak, boundary |
| [1.5](#1.5) | Step 4 merge | Unit: merge within 5min, no merge >5min |
| [1.6](#1.6) | Step 4 duration filter | Unit: under 2min discarded |
| [1.7](#1.7) | Step 5 trapezoidal integration | Unit: gap >60s skip |
| [1.8](#1.8) | Step 5 sort descending | Unit + PBT |
| [1.9](#1.9) | Step 5 top-3 slice | Unit: >3 periods |
| [1.10](#1.10) | Step 5 top-3 slice | Unit: <3 periods |
| [1.11](#1.11) | `PeakPeriod` struct fields + rounding | Unit: field values |
| [1.12](#1.12) | Empty slice returns | Unit: empty/all-offpeak/uniform |
| [1.13](#1.13) | No new DynamoDB queries | Code review (no new reader calls) |
| [2.1](#2.1) | `DayDetailResponse.PeakPeriods` field | Integration test |
| [2.2](#2.2) | `PeakPeriod` struct JSON tags | Unit: JSON round-trip |
| [2.3](#2.3) | `peakPeriods` initialized to `[]PeakPeriod{}` | Integration test |
| [2.4](#2.4) | No existing fields changed | Code review + existing tests pass |
| [2.5](#2.5) | Fallback path doesn't call `findPeakPeriods` | Integration: fallback test |
| [3.1](#3.1) | `PeakUsageCard` + visibility condition | ViewModel test |
| [3.2](#3.2) | `if !viewModel.peakPeriods.isEmpty` guard | ViewModel test |
| [3.3](#3.3) | `periodRow` formatting | Visual verification |
| [3.4](#3.4) | `.thinMaterial` + `RoundedRectangle` | Visual verification (matches summaryCard) |
| [3.5](#3.5) | Placement in `DayDetailView` body | Visual verification |
| [3.6](#3.6) | `DateFormatting.clockTime` uses Sydney TZ | Existing `DateFormatting` tests |
