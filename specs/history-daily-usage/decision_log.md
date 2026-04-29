# Decision Log: History Daily Usage

## Decision 1: Feature name `history-daily-usage`

**Date**: 2026-04-29
**Status**: accepted

### Context

The feature adds a multi-day rollup of the same five-block daily-usage breakdown that the Day Detail "Daily Usage" card already shows for one day. Three candidate names were proposed: `history-daily-usage`, `history-block-usage`, and `history-load-distribution`.

### Decision

Use `history-daily-usage` for the spec folder and feature identifier.

### Rationale

Mirrors the existing Day Detail card name ("Daily Usage"), keeping the conceptual mapping obvious for anyone reading the History card alongside the Day Detail one. `history-block-usage` reads as jargon, and `history-load-distribution` overstates the scope (no per-source distribution is shown).

### Alternatives Considered

- **history-block-usage**: Names the underlying concept rather than the user-facing card — less discoverable for someone navigating from Day Detail.
- **history-load-distribution**: Implies a richer source/destination breakdown than is in scope.

### Consequences

**Positive:**
- Naming continuity with the existing `peak-usage-stats` spec and the Day Detail card.

**Negative:**
- The `history-` prefix collides visually with `history-multi-card`; readers may need both folder names in scope when thinking about History work.

---

## Decision 2: Five-block granularity (match Day Detail)

**Date**: 2026-04-29
**Status**: accepted

### Context

The ticket text says "evening/night/peak/offpeak", which literally enumerates four labels. The Day Detail Daily Usage card uses five blocks (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening). A user-facing graph could collapse Morning Peak + Afternoon Peak into a single "Peak" series, or even fold Evening into Night, to keep the chart simpler.

### Decision

Use the same five blocks as the Day Detail Daily Usage card.

### Rationale

Backend computation already produces all five blocks; collapsing them client-side would add a transformation layer with no compute saving. Keeping the same vocabulary across Day Detail and History means a user who taps from a History bar into Day Detail sees the same labels in the same order — a strong consistency win. The chart is a stacked bar with five segments, which is well within the readability budget of SwiftUI Charts at the 7 / 14 / 30-day densities the screen supports.

### Alternatives Considered

- **Four blocks (collapse peaks)**: Would make morning vs afternoon peak indistinguishable in History, defeating the user's likely question of "when am I drawing during peak?".
- **Three blocks (fold evening into night)**: Loses the evening signal entirely; evening is the most actionable block for the dashboard owner.

### Consequences

**Positive:**
- Direct vocabulary parity with Day Detail.
- No backend transformation needed beyond surfacing the existing payload.

**Negative:**
- A 30-day chart has 150 stacked segments; legibility relies on consistent palette ordering and decent bar widths.

---

## Decision 3: Stacked-bar layout in a single new card

**Date**: 2026-04-29
**Status**: accepted

### Context

Three layouts were considered: a single stacked-bar card, a single grouped-bar card, and one small card per block (mirroring Solar / Grid / Battery).

### Decision

Render a single stacked-bar card per the History layout, with one stack per day and segments per block.

### Rationale

Stacked bars communicate two things at once: total daily load and the share of each block. Grouped bars make per-block comparison easier but lose the daily-total reading at a glance and become noisy at 30 days. One card per block would multiply the number of History cards by four or five, blowing the screen scroll budget without telling the user anything they couldn't read from a single stack.

### Alternatives Considered

- **Grouped bars**: Better per-block comparison, worse daily-total readability; chart becomes noisy at 30 days.
- **One card per block**: Symmetrical with existing Solar / Grid / Battery cards but multiplies card count beyond the screen's scroll budget.

### Consequences

**Positive:**
- One additional card on the History screen.
- Daily total visible as the bar height; per-block share visible as segment height.

**Negative:**
- Stacked bars make precise per-block comparison across days harder than grouped bars would.

---

## Decision 4: Add card alongside existing — no replacement

**Date**: 2026-04-29
**Status**: accepted

### Context

The existing Grid Usage card already shows peak vs off-peak grid imports. Adding a Daily Usage card creates partial overlap: the Off-Peak block in the new card and the off-peak grid import in the Grid card both refer to the off-peak window, but the new card measures load (kWh consumed) while the Grid card measures imports (kWh from the grid). They are different metrics over the same window.

### Decision

Add the new card alongside Solar / Grid / Battery; do not modify or retire the Grid card.

### Rationale

The two metrics answer different questions ("how much did I consume during off-peak?" vs "how much did I import during off-peak?"). Conflating them would lose information. The Grid card's peak/off-peak split is also load-bearing for the V1 UX and is referenced by other specs.

### Alternatives Considered

- **Retire Grid card's off-peak split**: Would make the new card the canonical off-peak surface but lose the grid-import angle that the user originally cared about (when off-peak charging is happening).
- **Replace Grid card entirely**: Loses the export visualisation in History.

### Consequences

**Positive:**
- No regression to existing History cards.
- Additive change — easier to ship and roll back.

**Negative:**
- History screen now has four primary cards plus the per-day summary; vertical scroll grows.

---
