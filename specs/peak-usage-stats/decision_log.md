# Decision Log: Peak Usage Stats

## Decision 1: Spec Directory Name

**Date**: 2026-04-27
**Status**: accepted

### Context

The Transit ticket title is "Add peak usage statistics" but the existing `peak-usage-periods` spec already owns the word "peak" for cluster detection on a different code path. Naming options were `peak-usage-stats`, `day-usage-stats`, and `daytime-stats`.

### Decision

Use `peak-usage-stats` as the spec directory name.

### Rationale

The user preferred matching the ticket title verbatim despite the partial overlap with `peak-usage-periods`. The two specs touch the same handler (`handleDay`) but produce distinct fields (`peakPeriods` cluster array vs. `dailyUsage` block array), so the naming overlap is contained to the spec directory rather than the runtime contract.

### Alternatives Considered

- **day-usage-stats**: Recommended by the assistant; parallel to `evening-night-stats` — Rejected to keep the spec aligned with the ticket title.
- **daytime-stats**: Most concise and chronologically accurate — Rejected for the same reason.

### Consequences

**Positive:**
- Direct mapping from ticket title to spec directory.

**Negative:**
- Slight ambiguity with the existing `peak-usage-periods` spec; readers may need to consult both directories to understand which feature owns which `/day` field.

---

## Decision 2: Block Layout (Five Chronological Blocks)

**Date**: 2026-04-27
**Status**: accepted

### Context

Three layout options were considered: a single sunrise→sunset block, a peak/off-peak split, and a five-block chronological breakdown (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening) covering the entire calendar day.

### Decision

Use the five-block chronological layout, contiguous from midnight to next midnight, replacing the existing `EveningNightCard` with a single unified card.

### Rationale

The user wants visibility into where load happens within the day and how the off-peak charging window contributes. A single block hides that information; a peak/off-peak two-block split flattens the morning vs afternoon distinction. Five blocks give a complete day timeline that subsumes the existing Evening / Night card.

### Alternatives Considered

- **Single sunrise→sunset block**: Simplest; mirrors the existing pattern most closely — Rejected because it does not surface the off-peak vs peak distinction the user wants.
- **Two sub-blocks (peak vs off-peak)**: Cleaner card layout — Rejected because morning vs afternoon load patterns are different and the user wants both visible.
- **Side-by-side coexistence with existing EveningNightCard**: Avoids breaking changes — Rejected because two stacked cards covering overlapping windows is visually noisy and the existing card's information is already represented in the new five-block layout.

### Consequences

**Positive:**
- Complete chronological view of the day's load on a single card.
- Off-peak load becomes visible without leaving the Day Detail screen.
- Eliminates the existing `EveningNightCard`, simplifying the view hierarchy.

**Negative:**
- Five rows make the card visually heavier than the existing two-row version.
- Removing `eveningNight` from the API is a breaking change requiring iOS updates in lockstep.

### Impact

- Backend: new `findDailyUsage` function in `internal/api/compute.go`; new `DailyUsage` and `DailyUsageBlock` structs in `internal/api/response.go`; `eveningNight` field removed.
- iOS: new `DailyUsageCard` SwiftUI view replaces `EveningNightCard.swift`; `DayDetailViewModel` swaps the field; `APIModels.swift` swaps the struct.
- Tests: existing evening/night tests on both sides are rewritten for the new shape; literal `DayDetailResponse(...)` call sites need to swap `eveningNight: nil` for `dailyUsage: nil`.

---

## Decision 3: Off-Peak Boundary Source

**Date**: 2026-04-27
**Status**: accepted

### Context

The morning/off-peak/afternoon split needs concrete clock-time boundaries. Options were the SSM-configured off-peak window (`/flux/offpeak-start`, `/flux/offpeak-end`, currently 11:00–14:00) or hardcoded values.

### Decision

Use the SSM-configured off-peak window as the morning-peak/off-peak/afternoon-peak boundary source.

### Rationale

The rest of the system (poller off-peak delta computation, `findPeakPeriods` exclusion, History card splits) already consumes the SSM values. Hardcoding would produce drift if the user changes their tariff window.

### Alternatives Considered

- **Hardcoded 11:00–14:00**: Simpler, no SSM round-trip — Rejected to keep the feature consistent with the rest of the system.

### Consequences

**Positive:**
- Single source of truth for off-peak boundaries.
- Reconfiguration in SSM automatically updates the new card.

**Negative:**
- Adds a (small) SSM dependency to the new computation. Already handled by the existing handler context — no new fetch required.

---

## Decision 4: Per-Block Percentage of Day

**Date**: 2026-04-27
**Status**: accepted

### Context

The user requested an additional per-block field beyond `totalKwh` and `averageKwhPerHour`: the share of the day's load each block represents. Computation can be against the sum of emitted blocks or against an independent day-total.

### Decision

Compute `percentOfDay = round(blockKwh / sumOfEmittedBlocksKwh × 100)`, expressed as integer 0–100. Allow ±3% rounding drift across the sum of percentages (relaxed from the original ±2 after round-2 review showed naive integer rounding on five blocks can produce ±2.5 drift; ±3 is safely satisfiable).

### Rationale

Sum-of-emitted-blocks denominator keeps the percentages internally consistent with the rendered card (always sums to ~100% of what's shown). Using a separate day-total would produce confusing percentages on partial days where some blocks are omitted.

### Alternatives Considered

- **Percentage of an independent day-total kWh**: Lets percentages reflect data outside the blocks — Rejected because partial days would not sum to 100% and confuse readers.
- **Decimal fraction (0.0–1.0)**: Closer to the underlying ratio — Rejected because integer percent is what the card actually displays.

### Consequences

**Positive:**
- Percentages always sum to ~100% of the visible card content.
- Easy to display ("17%") with no client-side conversion.

**Negative:**
- Rounding to integer can produce 99% or 101% totals on some fixtures; tests assert ±3 tolerance.

---

## Decision 5: Replace `eveningNight` (Breaking API Change)

**Date**: 2026-04-27
**Status**: accepted

### Context

The new card supersedes the existing `EveningNightCard`. The API can either replace the existing `eveningNight` field with `dailyUsage` (breaking change) or ship both fields side-by-side.

### Decision

Remove `eveningNight` from the `/day` response and replace it with `dailyUsage`. iOS clients are updated in lockstep with the backend.

### Rationale

Two-user app, no external API consumers, no version pinning. The new field carries strictly more information than the old one (Night and Evening blocks are still present, plus three new blocks). Keeping both would waste payload and bifurcate the iOS code path with no benefit.

### Alternatives Considered

- **Add `dailyUsage` and keep `eveningNight`**: Avoids any breaking change — Rejected because there is no caller benefit and it duplicates payload.

### Consequences

**Positive:**
- Cleaner contract; one source of truth for per-period load on Day Detail.
- Smaller payload than dual-shipping.

**Negative:**
- Lockstep deploy required: backend and iOS must ship together.
- Existing `EveningNightCard.swift` and its tests are deleted rather than evolved.
- Install-skew window: the iOS app distributes via TestFlight, so a backend deploy can land while a user still has the previous build installed. The Day Detail card silently disappears for that user until they update. Mitigation: ship the iOS update first to TestFlight and wait for both devices to have it before deploying the backend. This is acceptable for the two-user audience and would not be acceptable in a wider-distribution app.

---

## Decision 6: Pipeline Order for Block Emission

**Date**: 2026-04-27
**Status**: accepted

### Context

The first revision of requirements left the order of clamp / today-gate / future-omit / degenerate-omit operations implicit. Peer review surfaced collisions: e.g. on today mid-morning-peak, `afternoonPeak`'s start is in the future (future-omit fires) and the today-gate also fires (clamps `afternoonPeak`). Two implementations could match the spec and produce different output.

### Decision

Encode an explicit five-step pipeline in [requirements.md AC 1.8](requirements.md#1.8): resolve nominal interval → solar-window guard → today-gate → future-omit → in-progress clamp → degenerate-omit. Every block flows through every step in this order.

### Rationale

The order matters: the today-gate must fire before future-omit so that `afternoonPeak`'s end is rewritten to `min(computedSunset, requestTime)` before any future-omit check runs. Without the explicit order, the gap between `lastSolar` and `requestTime` on a cloudy mid-afternoon could be silently uncounted.

### Alternatives Considered

- **Leave order implicit and trust implementers**: Less prescriptive, more flexibility — Rejected because peer review flagged a real collision that produced two valid interpretations.

### Consequences

**Positive:**
- Removes ambiguity; tests can assert exact behaviour at each pipeline step.
- Makes the today-gate's intent (cover the [lastSolar, requestTime) interval that would otherwise fall through the gap) explicit.

**Negative:**
- Requirements document becomes more procedural than purely declarative; this is a deliberate trade-off.

---

## Decision 7: Solar-Window Invariant and Fallback

**Date**: 2026-04-27
**Status**: accepted

### Context

Peer review identified that `firstSolar >= offpeakStart`, `lastSolar <= offpeakEnd`, or `firstSolar == lastSolar` produce overlapping or absurd block intervals when the off-peak window does not sit cleanly inside the solar window. AC 1.11 (degenerate-omit) only filters zero/negative-duration blocks; it does not prevent overlap between non-degenerate ones.

### Decision

Define a **solar-window invariant** (`firstSolar < offpeakStart < offpeakEnd < lastSolar` AND `firstSolar < lastSolar`) and treat any violation the same as the off-peak misconfiguration path: emit only `night` and `evening` using their nominal intervals.

### Rationale

The five-block layout assumes the off-peak window sits inside the solar window. When that assumption breaks (Melbourne winter sunrise after 11:00 — implausible in practice; cloudy day with one stray reading; future tariff change), the cleanest degradation is the same one the off-peak misconfig path uses. Implementing per-block clip-and-merge would add complexity for cases that should not occur in production.

### Alternatives Considered

- **Clip overlapping blocks (e.g. shorten `night` to end at `min(firstSolar, offpeakStart)`)**: Preserves more granularity — Rejected because the resulting block layout becomes inconsistent with the documented one (a `night` block ending at off-peak-start is no longer "no-solar"). Confusing in the UI.
- **Reject the request with an error**: Surfaces the problem — Rejected because the request is for valid historical data; the system should still return the night/evening view even on pathological days.

### Consequences

**Positive:**
- The five-block layout has a single, well-documented degradation mode used by both off-peak misconfig and solar-window invariant violation.
- Test suite covers both the typical path and the degraded path with the same fixtures.

**Negative:**
- A day where the solar-window invariant breaks loses information: morning-peak/off-peak/afternoon-peak details are not rendered. Acceptable because the underlying day is not normal.

---

## Decision 8: Pre-Sunrise Blip Filter Carried Forward

**Date**: 2026-04-27
**Status**: accepted

### Context

The existing `findEveningNight` applies `preSunriseBlipBuffer` (30 minutes before computed sunrise) when picking `firstPpv` and `lastPpv` to ignore stray sensor noise — a single `Ppv > 0` reading at 01:30 would otherwise corrupt both boundaries. The first revision of the requirements omitted this filter. Both peer reviewers (Kiro and Gemini) flagged it as a silent regression of T-1033.

### Decision

The Definitions section of the requirements states that `firstSolar` and `lastSolar` only consider readings at or after `computedSunrise − 30 minutes`. The new `findDailyUsage` SHALL reuse the same buffer.

### Rationale

The blip filter is a load-bearing invariant of the existing code. Dropping it would silently regress an already-shipped fix. Reusing it keeps the new feature consistent with the established sensor-noise model.

### Alternatives Considered

- **Drop the filter**: Simpler — Rejected; reintroduces the T-1033 bug.
- **Tighten the buffer (e.g. 60 min)**: More conservative — Rejected without evidence; 30 min has been observed to work for the existing card.

### Consequences

**Positive:**
- No regression of T-1033.
- Same boundary semantics as `findEveningNight`, so the two functions agree on the night/evening edges they share.

**Negative:**
- New code carries an implicit dependency on the same `preSunriseBlipBuffer` constant. The design phase must decide whether to share the constant or duplicate it.

---

## Decision 9: Today-Gate Predicate Uses Recent-Solar Heuristic

**Date**: 2026-04-27
**Status**: accepted

### Context

The first revision's today-gate predicate was `requestTime ≤ computedSunset` (inherited verbatim from `findEveningNight`). Round-2 critic walk-through showed this is wrong on cloudy days where solar genuinely stopped at e.g. 16:30 and the user requests at 18:00: the gate would omit `evening` and override `afternoonPeak.end` to `requestTime`, hiding the 90 minutes of real evening load and overwriting a valid readings-derived `lastSolar` with a contrived sunset-derived end. The precedent gets away with the simpler predicate only because it has no `afternoonPeak` block competing for the same minutes.

### Decision

The today-gate fires when `solarStillUp = recentSolar exists OR (no qualifying Ppv reading exists today AND requestTime ≤ computedSunset)`. `recentSolar` is any reading in `[requestTime − 5 minutes, requestTime]` with `Ppv > 0`.

### Rationale

The gate's job is to suppress flickering of the `lastSolar`-derived boundary while solar is actively producing. "Solar is still being produced now (or very recently)" is the precise condition that matters; "the sun has not astronomically set" is a coarse proxy that misclassifies cloudy late afternoons. The 5-minute window is a heuristic that admits brief Ppv-zero gaps during normal daytime but rejects "solar genuinely stopped hours ago".

### Alternatives Considered

- **Keep precedent's `requestTime ≤ computedSunset`**: Simpler — Rejected because round-2 review proved it produces a discontinuity on cloudy days.
- **Use a longer recent-solar window (15 min, 30 min)**: More tolerance for transients — Rejected without evidence; 5 minutes covers the typical 10s polling cadence with 30 readings of buffer.

### Consequences

**Positive:**
- Cloudy late afternoons render the real evening block from `lastSolar` to `requestTime`.
- Daylight hours still suppress evening cleanly.
- Step-3 status assignment moved out of step 5's `end > requestTime` test, fixing the round-2 pipeline bug where `afternoonPeak` was wrongly marked `complete` after step 3 clamped its end.

**Negative:**
- Introduces a new constant (`recentSolarThreshold = 5 minutes`) the design phase must place.
- Behaviour change vs. `findEveningNight` is real but contained to today's view.

---

## Decision 10: Post-Sunset Blip Filter Added Symmetrically

**Date**: 2026-04-27
**Status**: accepted

### Context

The precedent's `findEveningNight` filters pre-sunrise Ppv blips (anything before `sunrise − 30 min`) but applies no upper bound — its evening block runs to midnight regardless. The new `afternoonPeak` block ends at `lastSolar`, so a stray post-sunset Ppv blip at e.g. 22:00 would push `lastSolar = 22:00`, making `afternoonPeak.end = 22:00` and swallowing the entire evening into "afternoon". Round-2 critic flagged this as a silent regression introduced by the new model.

### Decision

`lastSolar` only considers readings in the closed interval `[computedSunrise − 30 min, computedSunset + 30 min]`. Same buffer as the pre-sunrise filter, applied symmetrically.

### Rationale

The blip filter is asymmetric in the precedent because the precedent doesn't have an afternoon block sensitive to a post-sunset boundary. The new model does. Mirror the buffer.

### Alternatives Considered

- **No upper bound (matches precedent)**: Smaller diff to existing helper — Rejected because the new `afternoonPeak` block makes this a real correctness bug.
- **Asymmetric buffer (e.g. 60 min post-sunset)**: More tolerance for true late solar — Rejected without evidence; production data shows Ppv hits zero well before sunset, so 30 min is generous.

### Consequences

**Positive:**
- Post-sunset blips no longer corrupt `afternoonPeak`.
- Symmetric filter is easier to reason about and test.

**Negative:**
- A genuine late solar reading just inside the buffer would be excluded. Acceptable; the production pattern doesn't show this.

---

## Decision 11: Partial-Data Days Honour the Strict Solar-Window Invariant

**Date**: 2026-04-27
**Status**: accepted

### Context

Round-1 design review surfaced an internal contradiction: AC 1.8 step 2 requires the strict invariant `firstSolar < offpeakStart < offpeakEnd < lastSolar` to fall through to the two-block path on failure, but the original AC 4.1 "partial-data day, recorder died at 12:30" fixture expected a five-block-with-omissions output that violates the invariant (`lastSolar = 12:25 < offpeakEnd = 14:00` would produce overlap between `offPeak` and `evening`).

### Decision

Honour the strict invariant. Partial-data days where the recorder dies during or before off-peak collapse to the two-block path (`night` + `evening` only). Partial-data days where the recorder dies after off-peak end keep the five-block path, with `afternoonPeak` shortened by `lastSolar` and `evening` running from `lastSolar` with zero kWh. AC 4.1 was split into two fixtures to cover both sub-cases explicitly.

### Rationale

Allowing block overlap (e.g. `offPeak [11:00, 14:00)` and `evening [12:25, midnight)` overlapping in `[12:25, 14:00)`) violates the introduction's "no-overlap" promise and produces visually confusing rows on the card. The strict invariant guarantees clean tiling at the cost of suppressing the off-peak detail on rare broken-recorder days. Two-user Flux audience can tolerate the lost detail on those days; the alternative produces a card that looks broken every time.

### Alternatives Considered

- **Relax the invariant to just `offpeakStart < offpeakEnd`**: Allows the partial-data fixture's five-block-with-omissions output — Rejected because it permits overlap between `offPeak` and `evening` on broken-recorder days, contradicting the "no-overlap" introduction promise and producing confusing card layouts.
- **Split the invariant** (front-half check for sunrise; back-half handled by degenerate-omit): Surgical but introduces an asymmetric guard — Rejected because the degenerate-omit alone does not prevent overlap between non-degenerate blocks (e.g. `offPeak` and `evening` in the partial-data case).

### Consequences

**Positive:**
- No overlapping blocks in any output, period.
- Single, simple invariant predicate matches the existing one-line guard in `findEveningNight`.

**Negative:**
- Days where the recorder dies during off-peak lose the off-peak detail and morning split. Considered acceptable for the rare broken-recorder case.

---
