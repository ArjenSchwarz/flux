# Peak Usage Periods — Implementation Explanation

## Beginner

### What does this feature do?

Flux monitors a home battery system and shows how much power the house uses throughout the day. The "Peak Usage Periods" feature automatically finds the top 3 times during the day when the house used the most electricity, and shows them on the iPhone app.

### How does it work at a high level?

The battery system records a power reading every 10 seconds. When you open a day's detail screen on the iPhone, the app asks the server for that day's data. The server already fetches all those 10-second readings for charts — now it also scans them to find "peak" periods.

A "peak" is any stretch of time where the household load (how much power the house is drawing) is above the day's average. Think of it like finding the hills on a graph — the server finds the tallest hills and tells you about the top 3.

The server skips the "off-peak" charging window (11:00–14:00 Sydney time) since that's when the battery charges from the grid and the readings aren't representative of normal household usage.

### What changed in the code?

Three areas were modified:

1. **Server (Go)** — A new function `findPeakPeriods` in `compute.go` does the math. It calculates the average load, finds readings above that average, groups them into periods, and picks the top 3 by energy consumed. The result is added to the existing `/day` API response as a `peakPeriods` array.

2. **Data models (Go + Swift)** — A new `PeakPeriod` type was added on both sides. Each period has a start time, end time, average power in watts, and total energy in watt-hours. The existing API response gained one new field — nothing else changed.

3. **iPhone app (Swift/SwiftUI)** — A new `PeakUsageCard` view shows the periods in a card that matches the existing summary card's look. It appears between the charts and the summary, and hides itself when there are no peak periods to show.

### What does a peak period look like on screen?

Each row in the card shows:
- The time range (e.g., "07:15 – 07:45")
- The average power draw (e.g., "4.2 kW")
- The total energy consumed (e.g., "2,100 Wh")

---

## Intermediate

### Algorithm overview

The `findPeakPeriods` function in `compute.go` implements a 5-step pipeline operating on the raw `[]dynamo.ReadingItem` slice (10-second polling data):

1. **Parse off-peak window** — Converts `"HH:MM"` strings to minute-of-day integers. If parsing fails or start ≥ end, the off-peak filter is disabled (all readings included).

2. **Compute threshold** — Single pass over readings. Skips off-peak readings (where Sydney local `minuteOfDay >= offpeakStart && minuteOfDay < offpeakEnd`). Computes the arithmetic mean of Pload from the remaining readings. Returns empty if no non-off-peak readings exist.

3. **Build initial clusters** — Second pass over the *original* readings slice (not a filtered subset — this is a deliberate design decision to preserve temporal adjacency). Both off-peak readings and below-threshold readings act as cluster-breakers. Each cluster tracks `startIdx`/`endIdx` into the readings slice, plus a running sum and count of above-threshold Pload values.

4. **Merge and filter** — Accumulator-based merge-intervals: clusters within 300 seconds (5 minutes) of each other are merged transitively. After merging, periods shorter than 120 seconds (2 minutes) are discarded.

5. **Compute energy and rank** — For each surviving period, trapezoidal integration over `readings[startIdx:endIdx+1]` computes energy in watt-hours, skipping reading pairs with gaps > 60 seconds. Average load comes from the cluster accumulators (above-threshold readings only, not diluted by gap readings). Periods with zero energy are discarded. Results are sorted by unrounded energy descending, and the top 3 are returned.

### Key design decisions reflected in code

- **Index-based tracking** (Decision 9): Clusters store `startIdx`/`endIdx` into the readings slice rather than timestamps. This gives O(1) access to period boundaries and structurally prevents off-peak readings from leaking into energy calculations.

- **Iterate original slice** (Decision 11): Step 3 walks the full readings array. If off-peak readings were removed first, readings at 10:59 and 14:01 would appear adjacent and form a false multi-hour cluster. Keeping off-peak readings as cluster-breakers prevents this.

- **AvgLoadW from accumulators only** (Decision 10): The average load uses only above-threshold readings from the cluster sum/count. Energy integration uses all readings in the index range (including below-threshold gap readings from merges). This prevents the average from being diluted while keeping energy accurate.

- **Mean threshold** (Decision 3): Self-adapting to the day's usage pattern. A quiet winter day gets a lower bar; a high-usage summer day gets a higher one.

### API contract

The `/day` response gained one additive field:

```json
{
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

`peakPeriods` is always present (never null) — initialized to `[]PeakPeriod{}` in `day.go` when nil. The iOS model declares it as `[PeakPeriod]?` and nil-coalesces to `[]` in the ViewModel, providing backwards compatibility if the backend predates the feature.

### iOS architecture

- `PeakPeriod` model in `APIModels.swift` — `Codable`, `Sendable`, `Identifiable` (id = start time).
- `DayDetailViewModel` — New `peakPeriods: [PeakPeriod]` published property, populated from `response.peakPeriods ?? []`, cleared on error.
- `PeakUsageCard` — Standalone SwiftUI view. Uses a dedicated `HH:mm` formatter (`DateFormatting.clockTime24h`) instead of the locale-dependent `clockTime` formatter to guarantee 24-hour display. Energy formatted with `NumberFormatter` using grouping separators.
- `DayDetailView` — Card inserted after `SOCChartView`, guarded by `viewModel.hasPowerData && !viewModel.peakPeriods.isEmpty`.

### Test coverage

**Backend unit tests** (16 table-driven cases in `compute_test.go`):
- Edge cases: empty readings, all off-peak, uniform load, negative Pload, invalid off-peak strings
- Clustering: single peak, merge within 5min, no merge >5min, transitive merge, off-peak boundary clustering (10:59/14:01 separation)
- Filtering: period under 2min discarded, zero-energy sparse period discarded
- Ranking: >3 periods returns top 3, same-rounded-energy ranked by unrounded, descending order
- Energy: gap >60s skips pair

**Property-based tests** (6 properties via `pgregory.net/rapid`):
- Result count ≤ 3, all periods outside off-peak, non-overlapping, positive energy, descending energy, duration ≥ 2 minutes

**Integration tests** (`day_test.go`):
- `TestHandleDayNormalCase` — verifies `peakPeriods` is present and non-null
- `TestHandleDayFallbackToDailyPower` — verifies empty array on fallback data
- `TestHandleDayNoDataFromEitherSource` — verifies empty array with no data
- `TestHandleDayPeakPeriods` — verifies a known high-load period produces correct peak period output

**Benchmark**: `BenchmarkFindPeakPeriods` with 8640 readings (full day).

---

## Expert

### Algorithm complexity and implementation details

`findPeakPeriods` is O(n) in the number of readings with two sequential passes and a merge pass over clusters (which is bounded by the number of clusters, itself ≤ n). The energy integration pass is also O(n) total across all periods since index ranges are non-overlapping. Memory allocation is minimal — clusters are stack-allocated structs, and the only heap allocation is the output slice.

#### Step 3: Clustering invariants

The clustering loop maintains a single `*cluster` pointer. Three conditions close a cluster: (a) the reading is off-peak, (b) Pload ≤ threshold, or (c) end of slice. The critical invariant is that `startIdx` and `endIdx` always point to readings that are both non-off-peak AND above-threshold. This is preserved through merging because merge only extends `endIdx` to another cluster's `endIdx`, which satisfies the same invariant.

```go
for i, r := range readings {
    offpk := hasOffpeak && isOffpeak(r.Timestamp, offpeakStartMin, offpeakEndMin)
    if offpk || r.Pload <= threshold {
        if cur != nil {
            clusters = append(clusters, *cur)
            cur = nil
        }
        continue
    }
    // extend or start cluster
}
```

This means the index range `[startIdx, endIdx]` of a merged period may contain below-threshold non-off-peak readings (from the merge gap), but never off-peak readings. The off-peak exclusion is structural, not conditional.

#### Step 4: Transitive merge correctness

The merge uses the standard accumulator pattern where the comparison is always against the *current* merged cluster's `endIdx`:

```go
merged := []cluster{clusters[0]}
for _, c := range clusters[1:] {
    last := &merged[len(merged)-1]
    gap := readings[c.startIdx].Timestamp - readings[last.endIdx].Timestamp
    if gap <= mergeGapSeconds {
        last.endIdx = c.endIdx
        last.sum += c.sum
        last.count += c.count
    } else {
        merged = append(merged, c)
    }
}
```

When A merges with B, `last.endIdx` becomes B's `endIdx`. If C is within 300s of B's end, the gap check `readings[C.startIdx].Timestamp - readings[last.endIdx].Timestamp` correctly evaluates against B's end (now part of the AB cluster), enabling the A+B+C transitive merge. This is verified by the "transitive merge" test case.

#### Step 5: Dual-source metrics

Energy and average load intentionally use different reading sets:

- **Energy** (`energyWh`): Trapezoidal integration over `readings[startIdx:endIdx+1]` — all readings in the index range, including below-threshold gap readings. Uses `max(Pload, 0)` clamping. Skips pairs with dt > 60s (consistent with `computeTodayEnergy`).

- **Average load** (`AvgLoadW`): `roundPower(c.sum / float64(c.count))` from cluster accumulators, which only accumulated above-threshold readings. This prevents dilution from low-usage readings bridged during merge.

Sorting uses the unrounded `energyWh` float to avoid arbitrary ordering when two periods round to the same whole number.

#### Off-peak boundary semantics

The off-peak check uses `>=` for start and `<` for end: a reading at exactly 11:00 is off-peak, a reading at exactly 14:00 is not. This is implemented in `isOffpeak()`:

```go
func isOffpeak(ts int64, offpeakStartMin, offpeakEndMin int) bool {
    t := time.Unix(ts, 0).In(sydneyTZ)
    minuteOfDay := t.Hour()*60 + t.Minute()
    return minuteOfDay >= offpeakStartMin && minuteOfDay < offpeakEndMin
}
```

The `parseOffpeakWindow` function rejects overnight windows (start ≥ end) and malformed strings, falling back to no off-peak filtering. The parser is hand-rolled (not `time.Parse`) for performance — it validates length, colon position, and digit ranges directly.

#### Nil-safety in the handler

`day.go` ensures `peakPeriods` is never JSON-null:

```go
peakPeriods = findPeakPeriods(readings, h.offpeakStart, h.offpeakEnd)
// ...
if peakPeriods == nil {
    peakPeriods = []PeakPeriod{}
}
resp.PeakPeriods = peakPeriods
```

`findPeakPeriods` returns `nil` (not `[]PeakPeriod{}`) on early exits for consistency with Go idioms. The handler normalizes this to an empty slice so `encoding/json` produces `[]` rather than `null`.

#### iOS decoding strategy

`DayDetailResponse.peakPeriods` is declared as `[PeakPeriod]?` (optional) rather than `[PeakPeriod]` with a custom `init(from:)`. This preserves the synthesized memberwise initializer needed by mocks and tests. The ViewModel nil-coalesces in one place:

```swift
peakPeriods = response.peakPeriods ?? []
```

#### 24-hour time formatting

`PeakUsageCard` uses `DateFormatting.clockTime24h(from:)` with a `DateFormatter` configured as `dateFormat = "HH:mm"` and `timeZone = sydneyTimeZone`. This avoids the existing `clockTime` formatter which uses `timeStyle = .short` and is locale-dependent (may produce 12-hour format on devices with that locale setting).

#### Property-based test generator

The PBT generator produces inputs with:
- Off-peak windows where `startH < endH` (or equal hours with `endM > startM`), ensuring the `start < end` invariant
- 0–500 readings with timestamps at 8–15 second intervals from midnight Sydney time
- Pload values uniformly distributed in [0, 10000]

Six properties are checked: count ≤ 3, periods outside off-peak, non-overlapping, positive energy, descending energy order, duration ≥ 2 minutes. These properties are exhaustive for the output contract — any algorithm bug that violates the requirements will violate at least one property.

---

## Completeness Assessment

### Fully Implemented

| Requirement | Evidence |
|---|---|
| 1.1 — Compute from raw readings, including partial days | `findPeakPeriods` called on raw `readings` in `day.go`; no date filtering within the function |
| 1.2 — Exclude off-peak window readings | `isOffpeak()` check in steps 2 and 3; boundary test confirms 11:00 excluded, 14:00 included |
| 1.3 — Threshold = mean of non-off-peak Pload | Step 2 computes `sum / float64(count)` from non-off-peak readings |
| 1.4 — Group strictly adjacent above-threshold readings | Step 3 clusters with off-peak and below-threshold as breakers |
| 1.5 — Merge clusters ≤ 5 min gap | Step 4 merge with `mergeGapSeconds = 300`; transitive merge verified |
| 1.6 — Discard periods < 2 min | Duration filter after merge with `minPeriodSeconds = 120` |
| 1.7 — Trapezoidal integration, skip >60s gaps | Step 5 integration with `maxPairGapSeconds = 60` and `max(Pload, 0)` clamping |
| 1.8 — Rank by energy descending | `sort.Slice` by unrounded `energyWh` descending |
| 1.9 — Return at most 3 | `n := min(len(results), maxPeakPeriods)` where `maxPeakPeriods = 3` |
| 1.10 — Return fewer than 3 if fewer qualify | Natural consequence of the min/slice logic |
| 1.11 — Fields: start (RFC 3339), end (RFC 3339), avgLoadW (1dp), energyWh (whole) | `PeakPeriod` struct with `time.RFC3339` formatting, `roundPower`, `math.Round` |
| 1.12 — Empty array for no readings / all off-peak / none above threshold | Early returns of `nil` normalized to `[]PeakPeriod{}` in handler |
| 1.13 — No additional DynamoDB queries | `findPeakPeriods` receives the already-fetched `readings` slice |
| 2.1 — `peakPeriods` field on `DayDetailResponse` | Present in both Go `response.go` and Swift `APIModels.swift` |
| 2.2 — Array of objects with start, end, avgLoadW, energyWh | `PeakPeriod` struct with matching JSON tags |
| 2.3 — Always present, never null | `if peakPeriods == nil { peakPeriods = []PeakPeriod{} }` in `day.go` |
| 2.4 — No existing fields changed | Additive change only; existing tests pass unchanged |
| 2.5 — Empty array for fallback data | `findPeakPeriods` only called when `len(readings) > 0`; fallback path skips it |
| 3.1 — Display card when peakPeriods non-empty and hasPowerData | Guard: `if viewModel.hasPowerData && !viewModel.peakPeriods.isEmpty` |
| 3.2 — Hide card when empty or no power data | Same guard; no empty state rendered |
| 3.3 — Time range HH:MM, avg load kW (1dp), energy Wh (whole, grouped) | `clockTime24h`, `String(format: "%.1f", ... / 1000)`, `NumberFormatter` with grouping |
| 3.4 — Match summary card styling | `.thinMaterial`, `RoundedRectangle(cornerRadius: 16, style: .continuous)`, `.headline`/`.subheadline` |
| 3.5 — Between charts and summary card | Inserted after `SOCChartView`, before `summaryCard` in `DayDetailView` |
| 3.6 — Sydney timezone | `clockTime24h` uses `sydneyTimeZone`; `parseTimestamp` handles UTC→Sydney conversion |

All 11 design decisions are reflected in the implementation.

### Partially Implemented

None.

### Missing

None. All 24 acceptance criteria across the 3 requirement groups are fully implemented and tested. All 13 tasks from the task list are complete.
