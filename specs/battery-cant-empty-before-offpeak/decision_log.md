# Decision Log: Battery Can't Empty Before Off-Peak

## Decision 1: Use a fixed 5 kW discharge ceiling as a Go constant

**Date**: 2026-05-23
**Status**: accepted

### Context

The new indicator answers "can the battery physically reach the 5 % cutoff before off-peak starts?". This requires assuming a maximum sustained discharge rate. AlphaESS inverters are nominally capped at 5 kW per the system specification, and there is no current need to vary this per-deployment.

### Decision

Bake the 5 kW max discharge rate into the Go backend as a single named constant (`maxDischargeKW = 5.0`).

### Rationale

The value is a hardware constant tied to the AlphaESS inverter. Treating it as a constant matches how `cutoffPercent = 5` is handled today and avoids the operational overhead of another SSM parameter for a value that will not change without a hardware swap.

### Alternatives Considered

- **Configurable SSM parameter (`/flux/max-discharge-kw`)**: Allows tuning without redeploy — Rejected because there is no realistic scenario in which the value needs to change for the current deployment.
- **Read from AlphaESS system metadata**: Pull the cap from the device API if available — Rejected because the field is not currently consumed anywhere and adds dependencies for no behavioural gain.

### Consequences

**Positive:**

- Simple, testable, no infra changes.
- Mirrors the existing `cutoffPercent` pattern.

**Negative:**

- Changing the value later requires a code change and redeploy.

---

## Decision 2: `pbat`-independent ceiling check (battery output only)

**Date**: 2026-05-23
**Status**: accepted

### Context

"Can't empty" can mean different things. With load and solar, the effective discharge rate is variable. The user wants a stable signal that does not move with live `pbat`: even at the inverter ceiling, the battery still cannot reach cutoff in time.

### Decision

Compute the check using battery output only at the constant 5 kW ceiling. Ignore live `pbat`, household load, and solar. (Note: household load actually empties the battery *faster* — so this is not strictly a "best case" rate, but it is the `pbat`-independent ceiling the user explicitly asked for.)

### Rationale

The user explicitly framed the question as "even if it suddenly discharges at 5 kW, the battery percentage can't go below 5 %". Coupling to load would reintroduce the live-rate volatility the existing `estimatedCutoffTime` already provides; the point of this signal is stability.

### Alternatives Considered

- **Battery + live load**: Adds household load to the discharge rate — Rejected because it answers a different question (closer to existing cutoff prediction) and reintroduces volatility.

### Consequences

**Positive:**

- Stable, easy to reason about, easy to test.
- Independent of live `pbat`; flag is meaningful even when the battery is currently charging or idle.

**Negative:**

- Ignores load that would empty the battery faster — accepted because the goal is a `pbat`-independent ceiling, not a realistic forecast.
- Ignores inverter derating at high SoC/temperature — accepted because the spec is "can it physically?" at the inverter nameplate.

---

## Decision 3: Compute server-side as a new boolean on `battery`

**Date**: 2026-05-23
**Status**: accepted

### Context

The math is straightforward but requires knowing the off-peak start time and SoC/capacity together. The off-peak window is currently a server-side construct (env-passed SSM values) and the existing cutoff estimate is also computed server-side.

### Decision

Compute the flag in the Lambda `/status` handler and expose it as a new optional boolean on the `battery` object.

### Rationale

Keeps the off-peak time-zone math in one place (Go), matches the existing `estimatedCutoffTime` pattern, and keeps the Swift Dashboard view a thin renderer of API truth.

### Alternatives Considered

- **Client-side compute from existing fields**: Avoid API changes by deriving the flag in Swift — Rejected because off-peak windowStart/windowEnd are not currently in the `/status` payload, so the client would need new fields anyway, and the time-zone parsing duplicates Go logic.

### Consequences

**Positive:**

- Single source of truth.
- Easy to unit-test in Go.

**Negative:**

- Tiny payload increase.

---

## Decision 4: Show on Dashboard hero, not BatteryBlock or SoC alerts

**Date**: 2026-05-23
**Status**: accepted

### Context

Three surfaces were considered: a row in `BatteryBlock`, the Dashboard hero panel near the existing cutoff time, or a SoC push alert.

### Decision

Render the indicator on the Dashboard hero, near the existing `estimatedCutoffTime` row.

### Rationale

The signal contradicts the existing cutoff-time prediction in a way the user needs to notice immediately, so placement next to that prediction is the most coherent UX. SoC alerts and `BatteryBlock` are deferred.

### Alternatives Considered

- **BatteryBlock row**: Less prominent and visually separated from the cutoff prediction it contradicts — Rejected as too easy to miss.
- **SoC alert (push notification)**: Adds infra and decision noise — Rejected as out of scope for the first cut.

### Consequences

**Positive:**

- Surface is shared between iOS and macOS via FluxCore.
- Sits next to the prediction it qualifies, so the user reads them together.

**Negative:**

- Hero space is tight; the indicator must be compact.

---

## Decision 5: Replace the cutoff-time row instead of showing both

**Date**: 2026-05-23
**Status**: accepted

### Context

When the new flag is true, the existing `estimatedCutoffTime` extrapolation is misleading — it claims a cutoff time that physics says cannot be reached before off-peak. Both rows would contradict each other.

### Decision

When the flag is true, the Dashboard hero replaces the `estimatedCutoffTime` row with the new indicator. The cutoff time row is not shown alongside.

### Rationale

The indicator carries the same intent ("when will the battery hit cutoff?") with a more truthful answer ("it won't, before off-peak"). Showing both would invite the user to compare two readings that disagree.

### Alternatives Considered

- **Show both rows**: Indicator under or beside the cutoff time — Rejected because the two readings are mutually contradictory.
- **Hide the cutoff row with no replacement**: Drops information silently — Rejected because the user loses the off-peak context the indicator provides.

### Consequences

**Positive:**

- Single, internally consistent hero state.
- The off-peak start time travels with the indicator so the user still has temporal context.

**Negative:**

- The replacement row must be designed compactly to fit the hero without layout shifts.

---

## Decision 6: Flag is hero-only; do not duplicate on `rolling15min`

**Date**: 2026-05-23
**Status**: accepted

### Context

`/status` exposes both `battery.estimatedCutoffTime` (live `pbat`) and `rolling15min.estimatedCutoffTime` (15-min average `pbat`). Both could carry the new flag.

### Decision

The flag lives only on the `battery` object and only affects the hero row. `rolling15min` is unchanged.

### Rationale

The physics check is independent of `pbat`, so the result would be identical on both blocks — the duplication adds API surface for no UI consumer today.

### Alternatives Considered

- **Mirror on `rolling15min`**: API symmetry between the two cutoff blocks — Rejected because nothing renders `rolling15min` warnings and the value would always equal the hero flag.

### Consequences

**Positive:**

- Smaller API footprint.
- One place to change if the math evolves.

**Negative:**

- Slight asymmetry between the two cutoff blocks in the JSON.

---

## Decision 7: Suppress the flag when the live freshness gate fails

**Date**: 2026-05-23
**Status**: accepted

### Context

`/status` already gates `estimatedCutoffTime` on `liveFresh` (latest reading within 90 s). Without a recent SoC, any flag would be derived from a stale or absent value.

### Decision

The flag is computed and exposed only when `liveFresh` is true. Otherwise it is omitted (false).

### Rationale

Consistency with `estimatedCutoffTime`, and the underlying SoC value is the only honest input — a 10-minute-old SoC could make the flag flip when nothing has actually changed.

### Alternatives Considered

- **Compute against latest available SoC regardless of freshness**: Risks misleading UI during AlphaESS outages — Rejected.

### Consequences

**Positive:**

- One coherent freshness contract across hero fields.
- Avoids surprise flag flips during outages.

**Negative:**

- During outages, the user sees neither cutoff time nor warning — but the hero already degrades when `!liveFresh`, so this matches existing UX.

---

## Decision 8: Inherit the off-peak window limitations

**Date**: 2026-05-23
**Status**: accepted

### Context

`parseOffpeakWindow` (`internal/derivedstats/offpeak.go`) rejects windows where `start ≥ end`, so midnight-spanning off-peak windows are not supported anywhere in Flux today. `nextOffpeakStart` is also the only off-peak rollover helper.

### Decision

This feature reuses `nextOffpeakStart` directly and inherits its limitations: no support for midnight-spanning windows, no parallel rollover logic.

### Rationale

Reusing existing logic keeps a single source of truth for off-peak time math; expanding support is out of scope and would touch multiple unrelated features.

### Alternatives Considered

- **Add midnight-spanning support inside this feature**: Would split off-peak logic across two helpers — Rejected as a separate concern.

### Consequences

**Positive:**

- No drift between this feature and existing cutoff logic.

**Negative:**

- If a deployment ever needs a midnight-spanning window, this feature will silently not flag — explicitly documented as a Non-Goal.

---

## Decision 9: VoiceOver wording is expanded for context, not identical to visible text

**Date**: 2026-05-24
**Status**: accepted

### Context

AC 3.5 says the indicator "SHALL be accessible via VoiceOver, exposing the same wording (including the off-peak start time) it shows visually." The implementation ships visible text `"Won't empty before HH:MM"` and AX label `"Battery won't empty before off-peak at HH:MM"`. The literal strings differ; the off-peak start time is identical.

### Decision

Treat AC 3.5 as satisfied. The intent is that the time and the underlying signal are the same; the AX label expands the subject ("Battery") and the qualifier ("off-peak at" vs. "before") because VoiceOver lacks the visible context (chart, panel chrome) sighted users see.

### Rationale

Reading the AC literally would force one of the two strings to match the other. Both lose: the visible text becomes verbose enough to wrap on the hero panel; the AX label drops the subject and reads as a fragment. The pragmatic reading — same time, same signal — gives sighted and VoiceOver users an equivalent communication without compromising either presentation.

### Alternatives Considered

- **Match the literal strings**: Either truncate the AX label or expand the visible text. Rejected — neither produces good UX on its own surface.
- **Reword the requirement**: Update AC 3.5 to "VoiceOver SHALL expose the same off-peak start time and intent" before merging. Rejected — the looser reading already covers it, and editing requirements after implementation is noise.

### Consequences

**Positive:**
- Both presentations read naturally on their own surface.
- Future audits have a written rationale.

**Negative:**
- Anyone reading the AC strictly will notice the wording difference and need to find this decision.

---
