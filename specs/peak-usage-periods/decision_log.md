# Decision Log: Peak Usage Periods

## Decision 1: Always Include Peak Periods in Response

**Date**: 2026-04-16
**Status**: accepted

### Context

The `/day` endpoint returns a `DayDetailResponse` object. Adding a `peakPeriods` field changes the API response shape. We needed to decide whether to always include it or gate it behind a query parameter.

### Decision

Always include `peakPeriods` in every `/day` response.

### Rationale

Adding a new JSON field is additive — existing clients that don't parse the field will ignore it (standard JSON decoding behaviour in both Go and Swift). Gating behind a query parameter adds unnecessary complexity for a two-user app with a single client.

### Alternatives Considered

- **Query parameter opt-in (`?peakPeriods=true`)**: Only return when explicitly requested — Rejected because it adds complexity without benefit; the iOS app is the only client and will always want this data.

### Consequences

**Positive:**
- Simpler API surface — no conditional logic
- iOS app always has the data available without extra parameters

**Negative:**
- Marginal increase in response payload size (~100-200 bytes) for every `/day` request, even if the client doesn't use it

---

## Decision 2: Variable Result Count (0 to 3)

**Date**: 2026-04-16
**Status**: accepted

### Context

Some days may have fewer than 3 qualifying peak periods — for example, a low-usage day or one where most consumption happens during the off-peak window.

### Decision

Return however many periods qualify, from 0 to 3. Use an empty array when none qualify.

### Rationale

Padding to a fixed count of 3 would force the algorithm to lower its threshold, producing misleading results (e.g., labelling normal baseline usage as a "peak period"). An empty or short array is honest and easy for the UI to handle.

### Alternatives Considered

- **Always pad to 3**: Lower the threshold until 3 periods are found — Rejected because it produces meaningless results on low-usage days
- **Omit field entirely when empty**: Don't include `peakPeriods` at all when no periods qualify — Rejected because nullable fields are harder to work with in Swift; a consistent empty array is cleaner

### Consequences

**Positive:**
- Results are always meaningful — only genuinely high-usage periods appear
- iOS can simply hide the card when the array is empty

**Negative:**
- Users may occasionally see no peak usage card on quiet days (this is correct behaviour, not a problem)

---

## Decision 3: Mean Pload as Clustering Threshold

**Date**: 2026-04-16
**Status**: accepted

### Context

The algorithm needs a threshold to decide which readings count as "high usage." This threshold determines how many and which periods qualify.

### Decision

Use the mean (average) Pload of all non-off-peak readings for the day as the threshold.

### Rationale

The mean adapts to the day's overall usage pattern. A sunny winter day with low baseline usage will have a lower threshold (so moderate spikes still show), while a high-usage summer day will have a higher bar. This is more useful than a fixed watt value which would need seasonal tuning.

### Alternatives Considered

- **75th percentile**: Only the top quartile qualifies — Rejected because it may be too selective, missing moderate but sustained periods that are interesting to the user
- **Fixed watt threshold (e.g., 2000W)**: Consistent across days — Rejected because it doesn't adapt to seasonal patterns or household baseline changes

### Consequences

**Positive:**
- Self-adapting — works across seasons and usage patterns without configuration
- Simple to compute

**Negative:**
- On days with uniformly high usage, the mean will be high and fewer periods may qualify (which is arguably correct — nothing stands out)

---

## Decision 4: 5-Minute Merge Gap for Clustering

**Date**: 2026-04-16
**Status**: accepted

### Context

When usage dips briefly below the threshold and then spikes again, we need to decide whether to treat the dip as a boundary between two periods or merge them into one.

### Decision

Merge above-threshold clusters that are separated by a gap of 5 minutes or less into a single period.

### Rationale

A brief dip (e.g., an appliance cycling off for a minute) shouldn't split what is essentially one usage session. The 5-minute gap aligns with the downsampling bucket size, providing a natural resolution boundary. Longer gaps suggest genuinely separate activities.

### Alternatives Considered

- **No merging**: Strictly split at every below-threshold reading — Rejected because it produces many fragmented short periods from appliance cycling
- **10-minute merge gap**: More aggressive smoothing — Rejected because it may merge genuinely separate activities (e.g., morning cooking and a separate kettle boil 8 minutes later)

### Consequences

**Positive:**
- Produces natural, activity-aligned periods
- Consistent with the 5-minute downsampling resolution

**Negative:**
- Two genuinely separate activities within 5 minutes of each other will appear as one period

---

## Decision 5: Place peakPeriods on DayDetailResponse (Top-Level)

**Date**: 2026-04-16
**Status**: accepted

### Context

The `peakPeriods` field needs a home in the `/day` API response. The two candidates are the existing `DaySummary` object or the top-level `DayDetailResponse`.

### Decision

Place `peakPeriods` as a top-level field on `DayDetailResponse`, alongside `date`, `readings`, and `summary`.

### Rationale

`DaySummary` is currently a flat struct of scalar pointer fields (`*float64`, `*string`) representing energy totals and SOC low. Adding a nested array of structured objects is a structural mismatch. Placing it on `DayDetailResponse` also decouples peak period availability from whether a summary exists — peak periods are computed from readings, not from daily energy data.

### Alternatives Considered

- **Nest inside DaySummary**: Keeps all "computed stats" together — Rejected because it changes `DaySummary` from a flat scalar type to a mixed type, and creates a dependency where peak periods require a non-nil summary to exist

### Consequences

**Positive:**
- `DaySummary` remains a clean flat type
- Peak periods are independent of daily energy data availability
- Simpler nil-handling in both Go and Swift

**Negative:**
- `DayDetailResponse` gains a fourth field, but this is a natural fit alongside `readings`

---

## Decision 6: Compute Peak Periods for Partial Days (Today)

**Date**: 2026-04-16
**Status**: accepted

### Context

The `/day` endpoint accepts any date, including today. When viewing today, only a partial day of readings exists. The mean threshold and resulting periods will shift as more readings arrive.

### Decision

Compute peak periods for partial days using whatever readings are available. Results may change between refreshes as more readings arrive.

### Rationale

This is consistent with how the entire `/day` endpoint already works for today — downsampled readings, SOC low, and charts all reflect the partial day. Suppressing peak periods for today would be inconsistent and would prevent users from seeing useful information about their current day's usage.

### Alternatives Considered

- **Suppress for today**: Only compute for completed days (date < today) — Rejected because it's inconsistent with the rest of the endpoint and removes useful functionality

### Consequences

**Positive:**
- Consistent behaviour with the rest of the `/day` endpoint
- Users can see peak periods for today

**Negative:**
- Results may shift as the day progresses (the mean changes, new periods may appear or be displaced)

---

## Decision 7: 2-Minute Minimum Period Duration

**Date**: 2026-04-16
**Status**: accepted

### Context

A single above-threshold reading (~10 seconds) technically qualifies as a "period." Very short spikes produce entries with minimal energy that clutter the display.

### Decision

Discard periods with a total duration (end time minus start time) of less than 2 minutes.

### Rationale

A 2-minute minimum filters out momentary spikes (kettle starting, compressor kicking in) while keeping meaningful short bursts. This is more aggressive than 1 minute, ensuring only sustained usage qualifies.

### Alternatives Considered

- **No minimum**: Any above-threshold reading counts — Rejected because trivial spikes would appear as named periods with near-zero energy
- **1-minute minimum**: Less aggressive filter — Rejected by user preference in favour of 2 minutes for cleaner results

### Consequences

**Positive:**
- Cleaner results with only meaningful periods
- Avoids displaying "07:15 - 07:15" with negligible energy

**Negative:**
- Genuine short high-consumption events (90 seconds) would be excluded

---

## Decision 8: Always Display Load in Kilowatts

**Date**: 2026-04-16
**Status**: accepted

### Context

The iOS app needs to display the average load for each peak period. The value could be shown in watts or kilowatts, with or without conditional formatting.

### Decision

Always display the average load in kilowatts with one decimal place (e.g., "0.8 kW", "4.2 kW").

### Rationale

Using a single unit consistently avoids cognitive load from switching between W and kW. The kW convention aligns with how the rest of the app displays energy (kWh). One decimal place provides sufficient precision.

### Alternatives Considered

- **Conditional W/kW at 1000W boundary**: Show "850 W" below 1000 and "4.2 kW" above — Rejected because the inconsistency adds complexity without meaningful benefit

### Consequences

**Positive:**
- Consistent formatting, no conditional logic
- Aligns with kWh convention used elsewhere in the app

**Negative:**
- Small values like "0.3 kW" are slightly less intuitive than "300 W", but acceptable

---

## Decision 9: Index-Based Period Tracking Over Timestamp Re-scanning

**Date**: 2026-04-16
**Status**: accepted

### Context

Step 5 of the algorithm needs to iterate readings within each peak period for energy integration. The original design proposed re-scanning the full readings slice by timestamp range. Review identified this as error-prone (off-peak readings could leak in) and inefficient (requires searching for start/end positions).

### Decision

Track `startIdx`/`endIdx` into the `readings` slice for each cluster. Step 5 iterates `readings[startIdx:endIdx+1]` directly.

### Rationale

Index-based tracking is simpler, faster (direct slice access), and structurally prevents off-peak readings from leaking into the energy calculation. The indices are set during clustering (step 3) and extended during merging (step 4), always pointing to non-off-peak above-threshold cluster boundaries.

### Alternatives Considered

- **Timestamp-based re-scanning**: Iterate full readings slice, filter by `[period.start, period.end]` range — Rejected because it requires additional off-peak filtering during the re-scan, and is slower (linear scan per period)

### Consequences

**Positive:**
- Eliminates an entire class of off-peak leakage bugs
- O(1) access to period boundaries, no re-scanning
- Simpler implementation

**Negative:**
- Clusters must store two extra int fields (negligible memory)

---

## Decision 10: AvgLoadW from Cluster Accumulators Only

**Date**: 2026-04-16
**Status**: accepted

### Context

After merging clusters, below-threshold readings from the merge gap fall within the period's index range. Review identified that computing average Pload from all readings in the range would dilute the "peak" average with low-usage gap readings, producing misleadingly low values.

### Decision

Compute `avgLoadW` from the cluster accumulators (sum and count of above-threshold readings only). Energy integration still uses all readings in the index range.

### Rationale

The average load represents "how intense was this peak period" — including low-usage gap readings undermines that signal. Energy represents "how much power was consumed during this span" — including all readings gives an accurate total. These two metrics serve different purposes and should use different reading sets.

### Alternatives Considered

- **Average from all readings in range**: Simpler, single data source — Rejected because it dilutes the peak signal with gap readings
- **Time-weighted average**: More accurate for irregular intervals — Rejected as unnecessary complexity given the consistent ~10s polling interval

### Consequences

**Positive:**
- Reported average reflects actual peak behaviour
- Energy and average serve distinct, clear purposes

**Negative:**
- Average and energy come from different reading sets, which could confuse if someone audits the numbers closely (mitigated by documenting the distinction)

---

## Decision 11: Iterate Original Readings Slice in Clustering (Step 3)

**Date**: 2026-04-16
**Status**: accepted

### Context

The original design proposed step 3 iterate a "filtered (non-off-peak) readings" subset. Review identified this as a critical bug: removing off-peak readings from the list makes readings flanking the off-peak window appear adjacent (e.g., 10:59 and 14:01 become neighbours), creating false multi-hour clusters.

### Decision

Step 3 iterates the original `readings` slice. Both off-peak readings and below-threshold readings act as cluster-breakers.

### Rationale

Preserving temporal adjacency is essential for correct clustering. The off-peak window creates a natural gap of several hours — treating off-peak readings as cluster-breakers prevents any cluster from spanning across it.

### Alternatives Considered

- **Pre-filtered subset with gap detection**: Remove off-peak readings but check timestamp gaps between consecutive elements — Rejected because it adds complexity to solve a problem that doesn't exist when iterating the original slice

### Consequences

**Positive:**
- Correct clustering — no false clusters across the off-peak window
- Simpler mental model: one pass over one slice, three conditions (off-peak, below-threshold, or valid)

**Negative:**
- Step 3 must check the off-peak condition per-reading (trivial cost, same comparison as step 2)

---
