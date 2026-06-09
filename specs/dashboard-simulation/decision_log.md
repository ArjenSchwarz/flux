# Decision Log: Dashboard Simulation

## Decision 1: Use the full spec workflow

**Date**: 2026-06-08
**Status**: accepted

### Context

T-1495 asks for a Dashboard toggle that applies a predetermined load as a what-if simulation, configured via user-level presets. The scope assessment estimated >80 LOC across ~7-9 files, a cross-cutting data-consistency concern (the client must recompute a metric currently produced server-side), and several open design questions.

### Decision

Run the full requirements → design → tasks workflow rather than smolspec.

### Rationale

The feature exceeds every smolspec threshold and carries a real consistency risk that benefits from explicit design.

### Alternatives Considered

- **Smolspec**: Lightweight single-doc spec - Rejected because the feature is larger and more cross-cutting than smolspec targets, risking under-specification of the consistency handling.

### Consequences

**Positive:**
- Design phase can resolve the client/server calculation parity explicitly.

**Negative:**
- More up-front effort than a smolspec.

---

## Decision 2: Store presets server-side, synced

**Date**: 2026-06-08
**Status**: accepted

### Context

Presets (label → watts) are described as managed "at the user level". Options were local UserDefaults, iCloud key-value sync, or server-side storage with Lambda CRUD (matching the existing SoC Alerts and Pricing features).

### Decision

Persist presets server-side with CRUD endpoints, mirroring the SoC Alerts pattern, so they survive reinstall and sync across devices.

### Rationale

The app already has an established server-side config pattern (SoC Alerts), giving sync and durability for free conceptually and keeping configuration consistent with how other user-level settings work.

### Alternatives Considered

- **Local per-device (UserDefaults)**: Simplest - Rejected because presets would not sync and would be lost on reinstall.
- **iCloud key-value sync**: No backend work - Rejected in favour of consistency with the existing server-side config pattern.

### Consequences

**Positive:**
- Presets sync across devices and survive reinstall; consistent with SoC Alerts.

**Negative:**
- Requires Go backend and DynamoDB changes (new table + endpoints).

---

## Decision 3: One simulation active at a time

**Date**: 2026-06-08
**Status**: accepted

### Context

Multiple presets could either stack (watts sum) or be mutually exclusive.

### Decision

At most one preset is active at a time; activating a preset replaces any currently active one.

### Rationale

Matches the motivating example (a single "charge car" scenario), and keeps both the toggle UI and the power-flow model simple.

### Alternatives Considered

- **Stack multiple presets**: Sum watts of all active presets - Rejected as unnecessary for the use case and more complex to present and reason about.

### Consequences

**Positive:**
- Simple selection model and indicator.

**Negative:**
- Cannot model two simultaneous appliances without a combined preset.

---

## Decision 4: Simulation adjusts load, battery flow, and battery life

**Date**: 2026-06-08
**Status**: accepted

### Context

The simulation could change only the house-load figure and the "empty by" estimate, or also adjust the battery/grid tiles so the live trio panel stays internally balanced.

### Decision

Added load W increases displayed house load by W, increases displayed battery discharge by W (battery modelled as absorbing the extra draw) while the discharge stays within the battery's sustained-discharge ceiling, leaves grid unchanged below that ceiling, and recomputes the "empty by" from the simulated battery power. Decision 14 refines this: discharge is capped at the ceiling and the excess is routed to grid import.

### Rationale

Keeping the trio panel balanced avoids showing load rising while the battery tile stays put, which would look broken. Attributing the extra draw to the battery is the simplest balanced model and directly drives the battery-life change the user wants to see.

### Alternatives Considered

- **Load + battery-life only**: Leave grid and battery tiles at real values - Rejected because the trio panel would not balance (load up, battery unchanged).

### Consequences

**Positive:**
- Internally consistent panel; one clear attribution rule.

**Negative:**
- The plain battery-absorbs rule is inaccurate once simulated discharge exceeds the inverter's discharge ceiling; Decision 14 corrects this by capping at the ceiling and routing the excess to the grid.

---

## Decision 5: Active simulation is transient session state

**Date**: 2026-06-08
**Status**: accepted

### Context

When the app restarts, the active simulation could either be restored or reset.

### Decision

Presets persist (server-side), but the *active* simulation resets to off on a cold launch. It persists across auto-refresh and tab navigation within a session.

### Rationale

A what-if is a momentary exploration; silently restoring it on launch risks the user mistaking simulated values for real ones after they have forgotten it was on.

### Alternatives Considered

- **Restore active simulation on launch**: Persist the toggle - Rejected because of the risk of confusing simulated values for real state.

### Consequences

**Positive:**
- No risk of a forgotten simulation persisting into a fresh session.

**Negative:**
- The user must re-enable a simulation after restarting the app.

---

## Decision 6: Compute the simulated status server-side

**Date**: 2026-06-09
**Status**: accepted

### Context

The battery "empty by" estimate is not a pure formula: the value the Dashboard shows is the result of server handler policy — a freshness gate, off-peak-window suppression, use of the 15-minute rolling-average battery power, and a separate pbat-independent "won't empty before off-peak" indicator that hides the "empty by" line when set. The client today does not recompute the cutoff; it renders the server-formatted value. Supporting simulation requires the estimate to reflect added load. The options were to reimplement this policy in Swift (verified by a Go↔Swift parity test) or to compute the simulated status server-side via a load parameter on `/status`. A design-critic review and external validation (Codex and Gemini, independently) both favoured the server-side approach.

### Decision

The backend computes the full simulated status — adjusted live values plus the recomputed "empty by" and off-peak indicator — for a given added-load parameter on `/status`. The client renders the returned simulated status and does not re-derive any shared metric.

### Rationale

The project mandates that a metric shown on more than one screen be computed once, server-side where possible. Reimplementing the handler policy in Swift would create a second source of truth that must be kept in lockstep with the Go handler forever, which is the exact cross-screen divergence the mandate forbids. A load parameter reuses the one existing implementation, and the app already round-trips `/status` every 10s, so the cost is one parameterised call.

### Alternatives Considered

- **Client-side recompute in Swift, verified by a Go↔Swift parity test**: Works offline - Rejected because it duplicates non-trivial handler policy (freshness gate, off-peak suppression, rolling-average series, off-peak indicator) and carries ongoing drift risk; the parity test verifies a copy rather than removing the second source of truth.

### Consequences

**Positive:**
- One source of truth; simulated and real values cannot diverge.
- Cleanly resolves the off-peak-indicator contradiction and the stale-data case (simulation is simply unavailable when data is stale).

**Negative:**
- Simulation requires a live connection; it is unavailable offline or when live data is stale.
- Adds a parameter and simulation path to the Go `/status` handler.

---

## Decision 7: Available on both iOS and macOS

**Date**: 2026-06-09
**Status**: accepted

### Context

The app ships on iOS and macOS sharing the Dashboard hero panel, FluxCore, and a Settings scene. The initial requirements named only "the app" without stating platform scope; review flagged the omission.

### Decision

The feature — preset management, the Dashboard activation control, and the simulation indicator — is available on both iOS and macOS.

### Rationale

The affected surfaces are shared code; scoping to one platform would either leave the other broken or require platform-gating that the shared architecture does not otherwise need.

### Alternatives Considered

- **iOS only**: Smaller initial scope - Rejected because the shared hero panel and Settings scene would otherwise behave inconsistently across platforms.

### Consequences

**Positive:**
- Consistent behaviour across platforms from the shared implementation.

**Negative:**
- macOS layout and interaction need explicit verification, not just iOS.

---

## Decision 8: Keep the battery-absorbs model; bound the watt value instead

**Date**: 2026-06-09
**Status**: superseded by Decision 14

> **Superseded:** the 20 kW watt-value bound (Req 1.3) stays, but the choice *not* to model the inverter ceiling is reversed by Decision 14 — car charging (the primary use case) is a hard requirement and the unclamped model produced optimistic "empty by" times once simulated discharge exceeded the ceiling.

### Context

The "battery absorbs the added load, grid unchanged" model (Decision 4) is physically inaccurate near the inverter discharge ceiling, near cutoff, or while solar is charging. A review suggested clamping simulated discharge at the inverter ceiling and routing overflow to the grid.

### Decision

Keep the documented first-order approximation and instead reject preset watt values above 20 kW, rather than modelling the inverter ceiling and grid overflow.

### Rationale

A what-if estimate does not need a faithful power-electronics model; building one is disproportionate to the feature. The 20 kW cap is only an input-sanity guard against typos and absurd values — it deliberately does **not** keep simulated discharge within the inverter's ~5 kW sustained ceiling. Simulated battery discharge (`real pbat + W`) can and often will exceed 5 kW in the feature's primary use case (high-watt car charging on top of existing load); under the battery-absorbs model the resulting "empty by" is then optimistic relative to what the real system, falling back to the grid at the ceiling, would do. This is the accepted limit of the approximation, recorded as a non-goal rather than corrected with a power-flow model.

### Alternatives Considered

- **Clamp discharge at the inverter ceiling with grid overflow**: More physically faithful - Rejected as over-engineering for a what-if; adds a power-flow model the feature does not need.
- **A 5 kW cap to match the inverter ceiling**: Appears safer - Rejected because added load stacks on existing load, so even a small `W` can push simulated discharge past 5 kW; a low cap would not make the model physically exact and would block legitimately large appliance presets.

### Consequences

**Positive:**
- Simple, predictable model; inputs bounded against nonsense values.

**Negative:**
- When simulated discharge exceeds the inverter ceiling (likely for car charging), the "empty by" is optimistic and carries no on-screen caveat. If this proves misleading in use, a follow-up could add a "beyond battery output" hint without changing the model.

---

## Decision 9: Mirror SoC Alerts for preset CRUD failure handling

**Date**: 2026-06-09
**Status**: accepted

### Context

Preset CRUD is server-side; the draft requirements specified only validation, not transport-failure or offline behaviour. The existing SoC Alerts feature already has an established error model.

### Decision

Preset CRUD follows the SoC Alerts pattern: apply a change locally only after the server confirms it, leave the list unchanged on failure, and surface a visible error.

### Rationale

Reusing the established pattern is near-zero marginal cost and consistent with the feature it is modelled on; inventing a lighter bespoke error path would diverge from precedent for no benefit.

### Alternatives Considered

- **Optimistic update with rollback**: Snappier UI - Rejected because it does not match the existing server-confirmed-then-apply pattern and adds rollback complexity.
- **No explicit error surface (silent/toast only)**: Less UI work - Rejected as inconsistent with SoC Alerts and likely to hide real failures.

### Consequences

**Positive:**
- Consistent, predictable failure behaviour matching the rest of the app.

**Negative:**
- A change is not reflected until the server confirms it (a brief delay on slow connections).

---

## Decision 10: Model presets on the `/pricing` precedent (system-wide, id-only key)

**Date**: 2026-06-09
**Status**: accepted

### Context

Presets must sync across the user's devices ([requirement 1.4](requirements.md#1.4)). The first draft mirrored the SoC Alert rules feature, which is partitioned by `deviceId` (because alert notifications target a registered device) — and so needed a deliberate deviation to a serial partition to achieve sync. Review pointed out a better existing precedent: `/pricing` is already a system-wide CRUD resource. `flux-pricing` is keyed by `pricingId` (HASH only, no device/user/serial partition) and `ListPricing(ctx)` scans the whole table. There is no per-user identity in the backend and the API token is a single shared secret, so a single-system, id-only table is the natural fit.

### Decision

Model the presets CRUD on `/pricing`: a `flux-simulation-presets` table keyed by `presetId` only (no partition), endpoints `/simulation-presets` with no `{deviceId}` segment, list-all via Scan. Omit pricing's singleton-sentinel and transactional machinery, which exist only for its open-ended-period coupling. Mirror SoC Alerts for the *client* service and editor UI, which is the closer UI shape.

### Rationale

Following an existing system-wide precedent removes the need for any partition-scoping decision: every device that talks to the backend sees the same list, satisfying sync, with no deviceId siloing and no serial partition that would hold exactly one value forever (the backend is single-system; `/status` itself is hard-wired to one serial, so a serial key enables nothing today). This is strictly simpler than the serial-partition draft.

### Alternatives Considered

- **Device-scoped (mirror SoC rules exactly)**: Maximum consistency with the notifications feature - Rejected because presets would silo per device, violating 1.4.
- **Serial-partitioned table**: Aligns the key with `/status` - Rejected as needless complexity: the serial is a single configured value with no multi-system path, so the partition stores one value forever; `/pricing` shows the keyless single-system model is the established pattern.

### Consequences

**Positive:**
- Presets sync across devices with no extra machinery and no deviation to document.
- Reuses an existing single-system CRUD shape end to end (handler, store, table, IAM).

**Negative:**
- Two precedents in play (pricing for the backend, SoC Alerts for the client UI); the design states which applies where.

---

## Decision 11: Suppress the off-peak indicator while simulating

**Date**: 2026-06-09
**Status**: accepted

### Context

The hero panel shows either the real-data "won't empty before off-peak" indicator or the "empty by" line, never both (`DashboardHeroPanel.subline`). That indicator is computed pbat-independently against the 5 kW inverter ceiling — a worst-case reassurance. Under an added-load simulation it would still reflect real data and could hide the simulated "empty by", producing the contradiction [requirement 4.3](requirements.md#4.3) forbids.

### Decision

While a non-zero load is simulated, the backend returns `cantEmptyBeforeOffpeak = nil`, so the hero always shows the simulated "empty by" line (or just the simulated discharge rate when the cutoff lands after off-peak under the existing suppression).

### Rationale

The worst-case "won't empty" guarantee is not meaningful for a what-if that deliberately raises load; the informative value under simulation is the recomputed "empty by", whose presence already encodes whether the battery empties before off-peak. Suppressing the real indicator removes the only path by which a real and a simulated statement could appear together.

### Alternatives Considered

- **Recompute the indicator from the simulated discharge**: Keeps the affordance - Rejected because it mixes the simulated load with the indicator's fixed 5 kW-ceiling model, muddying its meaning; the "empty by" line already conveys the same information.
- **Leave the real indicator as-is**: No backend change - Rejected because it can hide the simulated "empty by" and contradict it (4.3).

### Consequences

**Positive:**
- No real/simulated contradiction; the hero's behaviour under simulation is simple to reason about.

**Negative:**
- The off-peak reassurance disappears while simulating; it returns the instant simulation is turned off.

---

## Decision 12: Cap presets at 20

**Date**: 2026-06-09
**Status**: accepted

### Context

The requirements do not bound the number of presets. The SoC rules feature caps rules at 10 to keep the list and the backend bounded.

### Decision

Enforce a defensive cap of 20 presets on create (HTTP 409 when exceeded), and disable the add affordance in the editor at the cap.

### Rationale

A cap keeps the Dashboard selection control usable and the stored list bounded, consistent with the precedent feature. Twenty is generous for personal use while still bounding the menu.

### Alternatives Considered

- **No cap**: Less code - Rejected as inconsistent with the SoC rules precedent and leaves the Dashboard menu unbounded.

### Consequences

**Positive:**
- Bounded list and selection menu; matches existing precedent.

**Negative:**
- A user wanting more than 20 presets cannot, which is implausible for this use case.

---

## Decision 13: Do not cache a simulated status for widgets

**Date**: 2026-06-09
**Status**: accepted

### Context

`DashboardViewModel.refresh()` writes every fetched status into the shared widget cache and may trigger a widget reload. Widgets and the Control Center widget must always show real data (a Non-Goal of this feature).

### Decision

While a simulation is active, skip the widget-cache write and the widget-reload trigger in `refresh()`.

### Rationale

A simulated status written to the shared cache would leak what-if values into widgets, which are out of scope and must stay real. Skipping the write is the minimal correct behaviour.

### Alternatives Considered

- **Tag cached entries as simulated and have widgets ignore them**: More flexible - Rejected as unnecessary work; widgets have no simulation feature, so simply not writing is sufficient.

### Consequences

**Positive:**
- Widgets never display simulated values.

**Negative:**
- The widget cache is not refreshed while a simulation runs; it resumes updating as soon as simulation is off (and the cache only ever held real data).

---

## Decision 14: Allocate added load by a priority waterfall — reduce export → battery (capped) → grid import

**Date**: 2026-06-09
**Status**: accepted (supersedes the no-clamp part of Decision 8)

### Context

Charging the car (the 1.7 kW example) is the primary use case and a hard requirement to model correctly across the states it's actually used in — both the evening peak and the sunny "soak up my solar" midday. The plain battery-absorbs model (Decisions 4/8) attributes the entire added load to the battery and leaves grid unchanged. Two physical realities break that:
1. The inverter has a sustained-discharge ceiling (`maxDischargeKW = 5.0 kW`, already a constant in `compute.go`); beyond it the battery cannot supply more and the surplus comes from the grid. Adding 1.7 kW on top of a ~4 kW evening household draw exceeds 5 kW, so the unclamped model showed the battery draining faster than possible and an "empty by" that was too early.
2. When the battery is charging *and* exporting surplus solar (full sun), self-consumption-mode hardware serves new load by cutting export first, keeping the battery charging — the plain model instead reduced battery charging and left export untouched, mis-attributing where the energy comes from.

### Decision

Allocate the added W by a priority waterfall (sign convention `pbat > 0` discharge, `pgrid < 0` export): (1) `exportReduction = min(W, max(0, -pgrid))` cuts current export toward zero; (2) the remainder `wBattery = W − exportReduction` is taken on by the battery up to its remaining headroom — `batteryAbsorbed = min(wBattery, max(0, maxDischargeW − pbat))`, giving `simDischarge = pbat + batteryAbsorbed` (never below the real `pbat`); (3) the leftover `overflow = wBattery − batteryAbsorbed` becomes grid import, `simPgrid = pgrid + exportReduction + overflow`. Headroom is evaluated per series (live `pbat`, rolling `avgPbat`). Capping only the *added* portion — not `min(pbat + wBattery, maxDischargeW)` — keeps `W = 0` a true no-op even when a real reading already sits at/above the ceiling.

### Rationale

One rule covers every starting state correctly with ~6 lines server-side, reusing the existing ceiling constant so the inverter limit has a single definition, and keeps the trio energy-balanced (`Δload = Δbattery + Δgrid`) throughout. It matches AlphaESS's default self-consumption priority (PV → load → battery → grid). As a bonus the Grid tile now conveys the real answer to the decision the feature exists for — charging the car reduces your export (you self-consume) or, once the battery saturates, pulls peak import — which the "grid unchanged" model could not show. The export step only changes outcomes while exporting (battery charging), where no "empty by" is shown, so it never affects the battery-life estimate.

### Alternatives Considered

- **Keep the unclamped battery-absorbs model (Decision 8)**: Simplest - Rejected because it gives optimistic "empty by" times in the evening case and mis-attributes the sunny case, both of which the user requires to be correct.
- **Cap discharge but skip the export-reduction step**: Fewer lines - Rejected because in the charging+exporting state it shows battery charging dropping while export stays flat, contradicting how the hardware actually responds.
- **Cap the discharge for the "empty by" only, leave the grid tile unchanged**: Slightly less code - Rejected because it re-breaks the panel's energy balance — the exact inconsistency Decision 4 exists to avoid.
- **Show an on-screen "beyond battery output" caveat instead of computing it correctly**: No model change - Rejected because computing the correct value is cheap and strictly better than flagging a known-wrong one.

### Consequences

**Positive:**
- The simulated power split and "empty by" are accurate in every state — evening peak, mild sun, and full-sun export — and the trio always balances.
- The Grid tile reveals whether charging would cut export or import peak power.

**Negative:**
- The grid value is no longer always unchanged under simulation (it moves when exporting or when the battery saturates) — intended, but a reader of the older non-goal must re-learn it.
- Assumes self-consumption priority (reduce export before reducing battery charge); a feed-in-priority configuration would behave differently. The 5 kW ceiling is a fixed constant — if the real inverter limit differs it updates in one place (`maxDischargeKW`).

---
