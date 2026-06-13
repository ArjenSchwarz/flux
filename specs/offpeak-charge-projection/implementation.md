# Implementation: Off-peak Charge Projection (T-1533)

Three-level explanation of the implemented feature, plus a completeness assessment
used to validate the implementation against the spec.

## Beginner Level

### What This Does

During the cheap "off-peak" charging window, the battery is being filled from the
grid. Before this change, the app showed you the current charge level but gave no
hint of how full the battery would be by the time the cheap window closed. This
feature adds a small line on the Dashboard's battery panel that reads, for example,
**"Projected at 14:00 — 97.5%"**: an estimate of where the battery will end up if
charging keeps going at full speed until the window ends.

The estimate is worked out on the server (the part of Flux that talks to the
battery), not in the app, so every device that shows it shows the same number.

### Why It Matters

It answers a practical question — "will my battery be full enough by the time cheap
power ends?" — without you having to watch the charge level climb. It only appears
while it's useful (during the window) and disappears otherwise, so you never see a
stale or meaningless figure.

### Key Concepts

- **State of Charge (SoC)**: how full the battery is, as a percentage (0–100%).
- **Off-peak window**: a fixed daily time range (here 11:00–14:00) when grid power is
  cheap and the battery charges from the grid.
- **Projection**: a forecast. Here it's a *best-case* forecast — it assumes charging
  continues at the fastest the hardware allows and ignores real-world losses, so the
  real result is usually a little lower.
- **Two-rate charging**: batteries charge fast until nearly full, then slow down to
  protect the cells. This model charges at 4.5 kW up to 95%, then at 0.5 kW (500 W)
  the rest of the way to 100% — like filling a glass quickly then easing off near the
  brim to avoid spilling.

---

## Intermediate Level

### Changes Overview

Backend (Go, `internal/api/`):
- `compute.go` — new pure function `projectOffpeakEndSoc(soc, capacityKwh, now,
  offpeakStart, offpeakEnd) *float64` and three charge-curve constants
  (`offpeakChargeRateKW = 4.5`, `offpeakTrickleRateKW = 0.5`, `fastChargeMaxSoc = 95`).
- `response.go` — new field `ProjectedEndSoc *float64 \`json:"projectedEndSoc"\`` on
  `OffpeakData` (pointer, no `omitempty`).
- `status.go` — wiring: after `resp.Offpeak = buildOffpeak(...)`, on the fresh-live
  branch, call the projection and assign it.

App (Swift):
- `FluxCore/Models/APIModels.swift` — `OffpeakData` gains `projectedEndSoc: Double?`
  with a trailing defaulted init parameter.
- `Flux/Helpers/BatteryBlock.swift` — two new view inputs (`projectedOffpeakEndSoc`,
  `offpeakWindowEnd`) and an `offpeakRow` computed property that selects a single
  off-peak row.
- `Dashboard/DashboardView.swift` — passes the projection and window-end label from
  `viewModel.status?.offpeak?`.

### Implementation Approach

The projection is a **closed-form two-rate curve**, not a simulation loop. Let
`r(kW) = kW / capacityKwh × 100` be the SoC gained per hour at a given power, and let
`h` be the hours from now to the window end:

- `soc ≥ 100` → `100`
- `soc ≥ 95` → `soc + r(0.5)·h` (already in the trickle band)
- `soc < 95` → charge at 4.5 kW until 95%, then 0.5 kW for whatever time is left:
  `hoursTo95 = (95 − soc) / r(4.5)`; if that exceeds `h`, stay on the fast rate the
  whole time; otherwise switch to trickle for the remaining `h − hoursTo95`.

The result is clamped to `[soc, 100]` and rounded to 1 dp via the existing `roundPower`.

The function is **gated** to return `nil` (→ JSON `null`) when the window is
unparseable, `now` is outside `[start, end)` (reusing `withinOffpeakWindow`), or
capacity is non-positive. The caller adds the **fresh-live** gate — the same
`liveFresh` flag that guards `EstimatedCutoff` — so a stale reading never produces a
projection.

On the app side, `BatteryBlock` consolidates the off-peak row into one `offpeakRow`
computed property: a projection row takes precedence over the "Charged during
off-peak" delta row (they're mutually exclusive), and `nil` means no row at all. The
projection's value is rendered with `SOCFormatting.format`, which matches the server's
1-dp rounding.

### Trade-offs

- **Server-side, single source (AC 3.1/3.3)**: computed once in the Lambda so all
  clients agree, rather than each client re-deriving it.
- **Field on `OffpeakData`, not `BatteryInfo`** (Decision 8): co-locates the value
  with the `windowEnd` that labels it, making the label/value pairing a single-object
  read.
- **Idealised, Pbat-independent** (Decisions 4/6): the function literally cannot see
  the live battery power or the simulated load, so "unchanged under simulation"
  (AC 2.4) holds structurally rather than via a suppression branch. Cost: it's
  optimistic versus real charging.
- **Closed form over iteration**: O(1), deterministic, and easy to property-test;
  avoids stepping through time.
- **Hardcoded constants** (Decision 3): the rates change only if the hardware does, so
  no SSM/env config surface was added.

---

## Expert Level

### Technical Deep Dive

- **Capacity parity (AC 1.4)**: `status.go` passes the *same* `capacity` local already
  resolved for `computeCutoffTime` (`fallbackCapacityKwh`, overridden by
  `sysItem.Cobat` when positive). There is no second lookup, so the projection and the
  cutoff estimate cannot disagree about capacity — a structural guarantee, not a
  convention.
- **Window-end instant**: built as `startOfDaySydney(now).Add(endMin minutes)` (the
  pre-push review replaced an inlined `time.Date(...)` with the shared helper). The
  gate (`withinOffpeakWindow`) compares *minute-of-day*, while `h` is computed from a
  *seconds-precise* `now`. Near the boundary (e.g. 13:59:30) the minute gate is still
  true and `h` is a sub-minute positive value absorbed by the `[soc, 100]` clamp
  (result ≈ current SoC); at 14:00:30 the minute gate is already false → `nil`. The two
  never disagree.
- **DST**: the window (11:00–14:00) is far from Sydney's 02:00/03:00 DST transitions,
  and `startOfDaySydney` + minute-of-day arithmetic are both transition-agnostic within
  this window.
- **Serialization (AC 3.2)**: `*float64` with no `omitempty` → absence serialises as an
  explicit `null`, mirroring `EstimatedCutoff`, so clients distinguish "no projection"
  from a value. Swift `Double?` decodes present / `null` / absent identically to `nil`.
- **SwiftUI selection**: `offpeakRow` is the single source of truth for the off-peak
  row; the "Lowest" row's `last:` reads `offpeakRow == nil` (post-review), so the
  two-row precedence rule lives in exactly one place. `rendersOffpeakDelta` /
  `offpeakDeltaText` were widened from `private` to internal solely so the logic is
  testable without view hosting — an accepted trade-off documented in the code.

### Architecture Impact

- No stored-model or DynamoDB changes — the value is derived live, so there is no
  migration or backfill.
- The `/status` hot path parses the off-peak window a couple more times per request
  (`withinOffpeakWindow` + a direct `ParseOffpeakWindow` for `endMin`). `ParseOffpeakWindow`
  is allocation-free byte arithmetic on two 5-char strings; against four concurrent
  DynamoDB queries this is unmeasurable. Keeping each helper self-contained (parsing
  the window independently) is the established handler convention and was deliberately
  preserved over threading parsed minutes through every consumer.
- The Day Detail and History screens are unaffected: the two new `BatteryBlock`
  parameters default to `nil`, so their off-peak row behaviour is unchanged.

### Potential Issues / To Monitor

- **Optimism by design**: the figure is best-case (ignores charge-rate derating and
  round-trip losses), so it will typically read higher than the battery achieves. This
  is the accepted definition (Decisions 4/6), not a bug — but it is the most likely
  source of user "it didn't reach that" feedback.
- **Hardcoded rates**: if the battery/inverter hardware changes, the constants need a
  code change and redeploy (Decision 3).

---

## Completeness Assessment

Every spec requirement maps to code that can be explained and is covered by a test.

**Fully implemented and tested:**

| AC | Where | Test |
|---|---|---|
| 1.1 / 2.1 | window + fresh gate (`projectOffpeakEndSoc` + `liveFresh`) | `TestProjectOffpeakEndSoc` (before/after window → nil); `TestHandleStatusProjectedEndSoc` (outside → null) |
| 1.2 / 1.3 / 1.7 | two-rate curve + 95% crossing split | table fixtures 50→97.5, 40→56.9, 90→98.2, 96→trickle-only |
| 1.4 | reuse of `capacity` in `status.go` | covered by integration test using the shared capacity |
| 1.5 / 1.6 / 1.8 | `max(soc, min(100, projected))`; `soc ≥ 100 → 100` | fixtures 97→100.0, 100→100.0; property `result ∈ [soc,100]` |
| 1.9 / 2.4 | function can't see Pbat/simLoadW | `simulation does not change projection` (byte-equal) |
| 1.10 | `roundPower` (1 dp) | fixtures are all 1-dp |
| 2.2 | `liveFresh` gate | `stale live emits null even inside window` |
| 2.3 | unparseable window / capacity ≤ 0 → nil | `negative capacity`, `zero capacity` fixtures |
| 3.1 / 3.2 | server-side; `*float64` no `omitempty` | `absent projection serialises as JSON null`; Swift decode present/null/absent |
| 3.3 | Dashboard reads `offpeak?.projectedEndSoc` directly | n/a (no re-derivation to test) |
| 4.1 / 4.2 | `offpeakRow` projection row labelled with `offpeakWindowEnd` | `projectionRowLabelUsesWindowEnd`, fallback test |
| 4.3 | `offpeakRow == nil` → no row | `noOffpeakRowWhenProjectionNilAndDeltaHidden` |
| 4.4 | `BatteryBlock` shared across iOS + macOS via target membership | macOS build + test |

Decision 9 (projection row takes precedence over the delta row) is covered by
`projectionSuppressesDeltaRow` and `deltaRowRendersWhenProjectionNil`.

**Partially implemented:** none.

**Missing:** none.

**Notes / non-defects surfaced while explaining:**
- AC 4.4 says "shared FluxCore views" but `BatteryBlock` lives in the app target
  (`Flux/Flux/Helpers/`), shared via target membership, not FluxCore. The design
  explicitly flags this wording as imprecise; only the `OffpeakData` model is in
  FluxCore. Not a divergence.
- AC 3.3 has no automated test (it asserts the *absence* of client-side derivation);
  verified by reading the Dashboard wiring.
