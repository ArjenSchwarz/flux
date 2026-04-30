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

## Decision 5: Pin block-kind colour palette in requirements

**Date**: 2026-04-30
**Status**: accepted

### Context

Reviewers flagged that AC 1.4's "visually distinct" requirement was unfalsifiable without a concrete palette — the legend / segment colour test cannot meaningfully assert anything against an unspecified set. Off-Peak teal was already locked by Decision 4's parity-with-Grid-card rationale, leaving four kinds (Night, Morning Peak, Afternoon Peak, Evening) open.

### Decision

Bind block kinds to fixed SwiftUI semantic colours: Night = `Color.indigo`, Morning Peak = `Color.orange`, Off-Peak = `Color.teal`, Afternoon Peak = `Color.red`, Evening = `Color.purple`. The mapping is exposed as a single source of truth that the legend, segments, and tests all read from.

### Rationale

Semantic SwiftUI colours adapt to dark / light mode automatically and have known contrast on system surfaces. The five chosen hues are well-spaced around the colour wheel (~70° apart), giving room for the chart's stacked density without two adjacent kinds bleeding into each other. Off-Peak teal is locked; the remaining four are intentionally chosen so morning/afternoon peaks (`orange` / `red`) read as related-but-distinct, and night/evening (`indigo` / `purple`) feel like the bookends of the day. Pinning the values in requirements rather than design lets the test for AC 1.4 / 5.4 be a fixture-table assertion rather than a subjective review.

### Alternatives Considered

- **Defer to design / asset catalog**: Would have allowed a custom palette but defers a falsifiable test target and risks two reviewers disagreeing on what "distinct" means.
- **Use `Color(red:green:blue:)` literals**: More precise but loses dark-mode adaptation and complicates dynamic-colour preview verification.
- **Reuse the chart-coloured palette from `daily-derived-stats`**: That spec exposes data, not visual tokens — there is nothing to reuse.

### Consequences

**Positive:**
- AC 1.4 / 5.4 are testable against a fixed table.
- Dark / light mode adaptation comes free with semantic colours.
- Easy to tweak post-launch by changing a single mapping table.

**Negative:**
- Five system colours leave little room for further visual differentiation if a sixth chart series is added later.
- `Color.red` for Afternoon Peak overlaps semantically with the Grid card's "Peak import" red — a user might assume the two reds refer to the same thing. Mitigated by the cards being separate and labelled, but worth watching during UX review.

---

## Decision 6: Log a warning when cache upsert overwrites a derived field with nil

**Date**: 2026-04-30
**Status**: accepted

### Context

The existing `HistoryViewModel.cacheHistoricalDays` upsert path was missing updates for the four derived fields (`dailyUsage`, `socLow`, `socLowTime`, `peakPeriods`) — a side-effect bug from `daily-derived-stats` that surfaced while scoping T-1022. The fix is to extend the upsert. The open question was whether to (a) overwrite unconditionally — including clearing previously-cached non-nil values when the API now returns nil — or (b) only ever set fields, never clear them.

### Decision

Overwrite unconditionally (option a) but log a warning when the upsert clears a previously-cached non-nil value, identifying the date and field cleared.

### Rationale

The backend is the source of truth for derived stats — `daily-derived-stats` AC 4.4 explicitly says missing-derived-attributes is a real, valid steady state for very old or transiently-failed dates, so the cache must be allowed to re-mirror that. But "we silently wiped breakdown data the user was looking at yesterday" is a worst-case offline regression, and a backend deploy that mistakenly emits nil for past dates would propagate to every device's cache without trace. The warning is cheap to emit and gives an operator something to notice if a regression hits — the rare expected case (date ages out of the readings TTL with no derived fields ever written) will be one log line per date per affected user, well within the noise floor.

### Alternatives Considered

- **Option (b): Never clear a non-nil cached value**: Avoids the worst-case wipe but lets stale cache data linger forever after a legitimate backend recompute decides a date no longer has valid blocks (e.g. a fix to `findDailyUsage` that newly recognises a date as unsummarisable). Since past-date recompute is rare but real, this would create a different long-tail bug.
- **Option (c): Add a separate "soft-delete with timestamp" column**: Overkill for a personal-use app.

### Consequences

**Positive:**
- Cache stays consistent with backend truth.
- Test code has a defined capture point for the warning (per AC 4.2's pinned subsystem/category).
- No schema change.

**Negative:**
- One log line per nil-overwrite. Volume is bounded but not zero on first launch after a backend recompute.
- The warning is only useful to a developer with a debugger or Console.app attached. This feature does not include log shipping, in-app surfacing, or any always-on telemetry — if the user is running a release build with no logs being read, a silent cache wipe still happens silently for them. The trade-off is acceptable for a two-user personal app where the operator is also one of the users; it would not be acceptable at larger scale.

---

## Decision 7: Sort blocks client-side rather than rely on payload order

**Date**: 2026-04-30
**Status**: accepted

### Context

AC 1.3 requires segments to render bottom-to-top in chronological block order. The backend `derivedstats.Blocks` helper currently emits blocks in chronological order, but neither the `daily-derived-stats` ACs nor the `DailyUsage` JSON shape mandate that ordering — it is an implementation invariant, not a contract.

### Decision

The iOS series-derivation step in `HistoryViewModel.DerivedState` SHALL sort each day's blocks into the chronological sequence (Night, Morning Peak, Off-Peak, Afternoon Peak, Evening) before handing them to the card.

### Rationale

The sort is three lines of Swift over at most five entries — there is no measurable cost. Removing the implicit dependency on backend emission order means a future backend refactor (e.g. emitting blocks in size-descending order for a different consumer) cannot silently break the History card. It also lets the iOS test suite cover a "non-chronological payload" fixture (AC 5.1.b) that the backend would otherwise refuse to construct.

### Alternatives Considered

- **Cite the backend invariant in `daily-derived-stats` and rely on it**: Couples this card to an unstated implementation detail. The first reviewer to read the JSON contract for the daily-usage card would have to chase the invariant through the Go source.
- **Push the ordering into the wire format as a contract AC on `daily-derived-stats`**: Would make `daily-derived-stats` carry an extra AC purely to serve this consumer.

### Consequences

**Positive:**
- Decoupled from backend emission order.
- Test fixture (AC 5.1.b) can exercise a malformed-but-recoverable payload.

**Negative:**
- Three lines of redundant work per day. Negligible.

---

## Decision 8: Defer migration to `chartXSelection(value:)`

**Date**: 2026-04-30
**Status**: accepted

### Context

iOS 26's SwiftUI Charts ships `chartXSelection(value:)` as a binding-driven selection API — cleaner than the existing project pattern (`chartOverlay` + `DragGesture` + `proxy.value(atX:)` in `ChartHighlightOverlay.swift`). The new Daily Usage card could either use the modern API or reuse the legacy overlay the three other History cards already use.

### Decision

Reuse `historySelectionOverlay` for the new card. Defer migration to `chartXSelection(value:)` until all four History cards can be migrated together as a separate change.

### Rationale

AC 1.10 requires "the same gesture and selection-highlight affordance the Solar / Grid / Battery cards already use" — splitting the four cards across two selection mechanisms would produce visibly different tap behaviour between cards on the same screen. Reuse is the parity-preserving choice. The future migration is also not a syntactic swap: `chartXSelection(value:)` is `Binding<T?>`-driven and would change the selection state architecture from the current callback-driven `onSelect(dayID:)` to a shared selection binding lifted into `HistoryView`. That's a non-trivial refactor across all four cards plus their tests, and it belongs in its own ticket.

### Alternatives Considered

- **Migrate only the new card to `chartXSelection`**: Would break parity per AC 1.10 and make a future four-card migration harder (one less consistent baseline to refactor from).
- **Migrate all four cards as part of T-1022**: Triples the diff size and pulls in three pre-existing cards' tests for a behavioural-no-op change. The right move when there is appetite for a focused selection-architecture refactor, not bundled into a feature spec.

### Consequences

**Positive:**
- All four cards share one selection mechanism.
- Smaller diff, faster review.

**Negative:**
- Carries the legacy overlay forward another release. The `chartOverlay` + `proxy.value(atX:)` pattern is not deprecated as of iOS 26 but is no longer the canonical path for chart selection.

---
