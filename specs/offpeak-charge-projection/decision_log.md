# Decision Log: Off-peak Charge Projection

## Decision 1: Use the full spec workflow

**Date**: 2026-06-13
**Status**: accepted

### Context

T-1533 asks for a projected battery SoC at the off-peak window end, shown on the Dashboard. The scope assessment estimated ~250 LOC across 6-8 files spanning the Go backend and the Swift app, with a non-linear charge model and several edge cases.

### Decision

Use the full spec workflow (requirements → design → tasks) rather than smolspec.

### Rationale

The change exceeds every smolspec threshold: more than three files, well over 80 LOC, two subsystems (backend + app), and a cross-cutting data-consistency concern. Several requirement questions also needed clarification before design.

### Alternatives Considered

- **Smolspec**: Lightweight single-document spec - Rejected because the feature fails the smolspec size and single-component criteria and carries data-consistency implications.

### Consequences

**Positive:**
- Edge cases and the charge model are pinned down before implementation.
- Data-consistency requirement is captured explicitly.

**Negative:**
- More upfront process than a trivial change would need.

---

## Decision 2: Show the projection only during the off-peak window

**Date**: 2026-06-13
**Status**: accepted

### Context

The projection could be shown only while the window is open, or also previewed before it starts ("if charging begins at 11:00 you'll reach X by 14:00").

### Decision

Show the projected SoC only while the current time is within the off-peak window [start, end).

### Rationale

The ticket explicitly frames the figure as something to see "during the off-peak window". A pre-window preview adds edge cases (assumed start time, no live charging yet) for a use case the ticket does not ask for.

### Alternatives Considered

- **Also preview before the window**: Show a projection ahead of window start - Rejected as out of scope and higher-complexity for no stated need.

### Consequences

**Positive:**
- Fewer edge cases; the projection always reflects a window that is actually open.

**Negative:**
- No advance warning before the window opens.

---

## Decision 3: Hardcode the charge-curve parameters as constants

**Date**: 2026-06-13
**Status**: accepted

### Context

The charge model uses 4.5 kW up to 95% SoC, then 500 W to 100%. These could be Go constants or configurable via SSM/env.

### Decision

Define the charge rates and the 95% threshold as hardcoded Go constants, mirroring the existing `maxDischargeKW = 5.0` constant.

### Rationale

Flux monitors a single, fixed battery system; these values change only if the hardware changes. Constants avoid added config surface, validation, and deploy steps. Mirror to FluxCore only if the app needs the values for display.

### Alternatives Considered

- **Configurable via SSM/env**: Add `OFFPEAK_CHARGE_RATE_KW` etc. - Rejected as unnecessary flexibility for values that rarely change.

### Consequences

**Positive:**
- No new configuration or validation paths.

**Negative:**
- Changing the values requires a code change and redeploy.

---

## Decision 4: Idealised max-charge projection, independent of live Pbat and losses

**Date**: 2026-06-13
**Status**: accepted

### Context

The projection could use a fixed idealised charge curve, or blend in the live measured charge power and efficiency losses.

### Decision

Compute the projection purely from current SoC, time remaining, and the fixed charge curve — independent of the battery's current Pbat and ignoring charging/round-trip losses.

### Rationale

The ticket states the projection "assumes charging continues to the max of the ability" — a best-case figure. Blending observed rate would contradict that intent and make the number noisy and harder to reason about.

### Alternatives Considered

- **Blend with observed charge rate / efficiency**: Factor live charge power and losses - Rejected as contrary to the "max ability" intent and noisier.

### Consequences

**Positive:**
- Deterministic, easy-to-explain number; stable across live-reading jitter.

**Negative:**
- Optimistic versus real-world charging when conditions limit the charge rate.

---

## Decision 5: Share the capacity value with the cutoff estimate

**Date**: 2026-06-13
**Status**: accepted

### Context

Both the existing cutoff estimate (`EstimatedCutoff`) and the new projection derive a result from the battery capacity (`SystemItem.Cobat`, fallback 13.34 kWh). If they read capacity from different sources the Dashboard could show two capacity-derived numbers that disagree.

### Decision

The projection uses the same capacity value and fallback path as `EstimatedCutoff`. A non-positive capacity disables the projection.

### Rationale

The project's data-consistency rule requires a shared metric to be computed from one source. Reusing the cutoff estimate's capacity guarantees the two figures never contradict each other.

### Alternatives Considered

- **Independent capacity lookup**: Read capacity separately for the projection - Rejected as a data-consistency risk for no benefit.

### Consequences

**Positive:**
- Cutoff and projection always agree on capacity.

**Negative:**
- None of note.

---

## Decision 6: Load simulation does not affect the projection

**Date**: 2026-06-13
**Status**: accepted

### Context

The `/status` handler branches heavily on `simulateLoadWatts`: it recomputes `EstimatedCutoff` with simulated discharge and suppresses `CantEmptyBeforeOffpeak` entirely. The projection is a charge figure; simulation models added load (discharge).

### Decision

An active load simulation (`simulateLoadWatts` > 0) leaves the projected SoC unchanged.

### Rationale

Decision 4 makes the projection independent of Pbat, and simulation only adds load. Following the cutoff/suppress pattern here would silently break that locked premise. Stating "unchanged" prevents an implementer copying the wrong pattern.

### Alternatives Considered

- **Suppress under simulation** (like `CantEmptyBeforeOffpeak`): Hide the projection while simulating - Rejected because the projection does not model load, so there is nothing to suppress.
- **Recompute under simulation** (like `EstimatedCutoff`): Adjust for simulated load - Rejected as contrary to Decision 4.

### Consequences

**Positive:**
- Projection stays consistent with its idealised-charge definition during simulation.

**Negative:**
- The projection and a simulated discharge estimate describe different scenarios on screen at once; acceptable since they answer different questions.

---

## Decision 7: Resolve charge-math ambiguities in requirements; defer the worked example to design

**Date**: 2026-06-13
**Status**: accepted

### Context

The two-rate charge curve has three ambiguities that affect the output: the tie-break at exactly 95%, how a remaining-time interval that crosses 95% is split, and the kW→SoC% unit conversion. A design-critic review initially argued two implementations would disagree; that risk is bounded because the value is computed once server-side (Decision in requirement 3).

### Decision

State the tie-break (SoC ≥ 95% charges at 500 W; SoC < 95% charges at 4.5 kW up to 95% then 500 W), the segment-crossing behaviour, and the conversion basis in the requirements. Put the closed-form/worked numeric example in the design document as a test fixture.

### Rationale

The remaining risk is untestable, drift-prone math, not divergent implementations. Behavioural acceptance criteria plus a design-level worked example make the math verifiable without putting an algorithm into the requirements.

### Alternatives Considered

- **Write the full formula into requirements**: Embed closed-form math in ACs - Rejected as implementation detail that belongs in design.
- **Leave the tie-break unspecified**: Rely on the implementer - Rejected as a determinism gap.

### Consequences

**Positive:**
- Requirements stay behavioural; design carries the test fixture.

**Negative:**
- The exact numbers live in design, so requirements alone do not fully pin a test value.

---

## Decision 8: Place the projected SoC on OffpeakData, computed after buildOffpeak

**Date**: 2026-06-13
**Status**: accepted

### Context

The projected value could live on `BatteryInfo` (next to `EstimatedCutoff`, where SoC and capacity are already in scope) or on `OffpeakData` (next to `windowStart`/`windowEnd`). The compute site needs the latest SoC, the shared capacity, and the fresh-live gate.

### Decision

Add `ProjectedEndSoc *float64` to `OffpeakData`. Compute it with a standalone `projectOffpeakEndSoc` function called in the status handler immediately after `buildOffpeak`, reusing the `capacity` variable already used by `computeCutoffTime`.

### Rationale

The value is meaningful only during the window and the Dashboard labels it with `windowEnd`; co-locating value and label in one sub-object makes the label/value identity (AC 4.2) a same-object read. Keeping the computation out of `buildOffpeak` avoids widening that function (which only deals with energy deltas) with SoC/capacity/freshness inputs. Reusing the existing `capacity` variable satisfies the shared-capacity requirement (AC 1.4) by construction.

### Alternatives Considered

- **Field on `BatteryInfo`**: Compute in-place in the battery block - Rejected; splits the projection from the `windowEnd` it targets, weakening the label/value consistency guarantee.
- **Compute inside `buildOffpeak`**: Pass SoC/capacity/liveFresh in - Rejected; mixes charge projection with energy-delta logic and widens the signature.

### Consequences

**Positive:**
- Projection and its target time travel together; capacity is shared with the cutoff estimate automatically.

**Negative:**
- The value is assigned a few lines after `buildOffpeak` rather than within it.

---

## Decision 9: Projection row takes precedence over the off-peak delta row in BatteryBlock

**Date**: 2026-06-13
**Status**: superseded by Decision 10

### Context

The Dashboard's `BatteryBlock` is invoked with `showsOffpeakDelta: true`, so a "Charged during off-peak" row always renders (showing "—" before the window produces data). During the window the delta is unknown (pending) but the projection is available, so a naive add would show two off-peak rows, one of them "—".

### Decision

Render the projection row instead of the delta row whenever a projection is present; otherwise render the delta row as today. The two off-peak rows are mutually exclusive in the layout.

### Rationale

During the window the projection is the meaningful figure and the delta is genuinely unknown, so a single contextual row is clearer than two. In practice they never co-occur (projection is window-only; the final delta exists only once the window completes), so precedence is a clean expression of that.

### Alternatives Considered

- **Show both rows**: Projection plus a "—" delta row - Rejected as redundant and noisy during the window.
- **Reuse the delta row's value slot**: Show SoC % in the delta row - Rejected; conflates two different metrics (absolute SoC vs delta percent) under one label.

### Consequences

**Positive:**
- One off-peak row at all times; Day Detail/History (which pass neither value) are unaffected.

**Negative:**
- The "Charged during off-peak" row is hidden during the window; the realised delta only appears once the window closes.

---

## Decision 10: Show the projection in the hero subline, not a BatteryBlock row

**Date**: 2026-06-14
**Status**: accepted

### Context

Decision 9 placed the projection as a row in the Dashboard's `BatteryBlock` ("Projected at 14:00 / 97.5%"). In use the figure read as a minor stat buried among the daily battery totals, not as the live, glanceable number it is meant to be. The hero panel already owns the live battery story: the big SoC numeral plus a subline that, when discharging, shows `Discharging · 2.40 kW · empty by 18:30` (the cutoff time in amber). The off-peak projection is the charging-side counterpart of that "empty by" figure and belongs in the same place.

### Decision

Render the projection in the hero panel's charging subline rather than in `BatteryBlock`. While the battery is charging and `/status` carries a projection, the subline reads `Charging · 4.50 kW · ~99% by 14:00`, with the projected figure in the same amber accent the cutoff time uses. The `BatteryBlock` projection row is removed and its delta-row behaviour reverts to the pre-feature state.

### Rationale

The projection is a live, transient figure (off-peak window only), so it belongs with the other live hero figures, not among the day's accumulated battery stats. Mirroring the discharge "empty by" line gives charge and discharge a symmetric, already-familiar treatment, and keeps the projection in exactly one place — there is no second screen or panel to keep consistent. Rounding to a whole percent prefixed with "~" signals an idealised best-case estimate, which suits the densely packed subline better than a 1-dp figure.

### Alternatives Considered

- **Keep the BatteryBlock row (Decision 9)**: A labelled row in the battery panel - Rejected; reads as a static daily stat rather than a live figure, and is visually distant from the SoC numeral it projects forward.
- **A dedicated second subline under the rate**: `Charging · 4.50 kW` on one line, `Off-peak target ~99% by 14:00` below - Rejected; adds vertical weight to the hero and breaks the symmetry with the single-line discharge treatment.
- **Show it in both the hero and the BatteryBlock row**: Rejected; the same value in two places on one screen is redundant and risks the two drifting on formatting.

### Consequences

**Positive:**
- The projection sits beside the SoC numeral it extrapolates, with the same amber accent as the cutoff time — charge and discharge are symmetric.
- Exactly one place renders the projection; no cross-panel consistency to maintain.
- `BatteryBlock` returns to a single off-peak responsibility (the realised delta row), simplifying it.

**Negative:**
- The projection is shown only while charging; if the battery is idle or (under simulation) discharging during the window, it is not surfaced.
- Whole-percent rounding in the hero drops the 1-dp precision the server computes (acceptable for an idealised figure shown in one spot).

---
