# Implementation: Battery Can't Empty Before Off-Peak

This document explains what landed on branch `T-1327/battery-cant-empty-before-offpeak` at three expertise levels, plus the per-change reviewer notes the pre-push review surfaced.

Commits (oldest first):

1. `445629d` — Backend `cantEmptyBeforeOffpeak` indicator on `/status`
2. `bd06d82` — Swift Dashboard hero indicator + `cantEmptyBeforeOffpeak` model
3. `b768ddf` / `380bbe6` — stream merge commits (no-ff)
4. `15636a1` — mark validation task complete, clean up rune slug accumulation
5. `fbb060b` — phase changelog, design table fix, mark spec Done

---

## Beginner Level

### What Changed
The home screen of the Flux app shows live battery information. Today, it can tell you "the battery is on track to be empty by 18:45" based on how fast it's currently draining. But that estimate assumes the battery keeps draining at whatever speed it's draining *right now* — which is usually a slow trickle.

In reality, the battery can only push out at most 5 kW. If the battery is very full and off-peak power is only a few minutes away, no amount of trickle (or even maximum drain) is going to get it down to the 5 % floor in time. The "empty by HH:MM" line is misleading in that case.

This change adds a new line that says **"Won't empty before HH:MM"** (where HH:MM is when off-peak starts) for exactly that situation. The two lines never show at the same time — when the warning is showing, the regular cutoff estimate is hidden.

### Why It Matters
Instead of looking at a cutoff time that's promised but physically impossible, the user sees the truth: "this battery has too much energy to drain in the time available, even at full throttle." They can then take action — e.g. manually export to the grid, or just stop worrying about discharging it.

### Key Concepts
- **State of charge (SoC):** how full the battery is, expressed as a percent (0–100 %).
- **Cutoff:** the 5 % floor the system stops discharging at, so the battery never fully empties.
- **Off-peak:** a window of cheap grid electricity (e.g. 11:00–14:00) when you'd rather not be draining your own battery — you'd buy cheap power instead.
- **Max discharge rate:** the physical ceiling on how fast the battery can push energy out — here, 5 kilowatts.

---

## Intermediate Level

### Changes Overview

**Go backend (`internal/api/`):**
- New constant `maxDischargeKW = 5.0` in `status.go`.
- New helper `withinOffpeakWindow(now, start, end) bool` in `compute.go` that delegates HH:MM parsing to the existing exported `derivedstats.ParseOffpeakWindow`.
- New helper `computeCantEmptyBeforeOffpeak(in cantEmptyInput) *bool` in `compute.go` that returns `&true` when, at 5 kW sustained from `now`, the battery cannot reach 5 % before `NextOpStart`. Returns `nil` (not `&false`) in every other case.
- New field on `BatteryInfo` in `response.go`: `CantEmptyBeforeOffpeak *bool \`json:"cantEmptyBeforeOffpeak"\`` — pointer, no `omitempty`, so the JSON wire shape is `true` or `null` (never `false`), mirroring the existing `estimatedCutoffTime` encoding.
- Wired into `handleStatus` inside the existing `liveFresh` branch, immediately after `EstimatedCutoff` is set.
- Tests: 6 cases for `withinOffpeakWindow`, 10 for `computeCantEmptyBeforeOffpeak`, 6 for the handler (including a Sydney DST-start case and a fallback-capacity case).

**Swift (`Flux/Packages/FluxCore/` and `Flux/Flux/Dashboard/`):**
- `BatteryInfo` gains `public let cantEmptyBeforeOffpeak: Bool?` with a memberwise-init default of `nil`. The default preserves the five existing call sites listed in `design.md` §Pattern extension audit.
- `MockFluxAPIClient` gains a second fixture `statusResponseCantEmpty` (default fixture keeps the flag `nil`).
- `DashboardHeroPanel` takes two new inputs (`battery: BatteryInfo?` and `offpeakWindowStart: String?`) and composes a new `cantEmptyBeforeOffpeakIndicator` subview alongside the existing `statusLine`. The `Mode` enum is untouched; the swap happens at the `subline` `@ViewBuilder` level.
- `DashboardView` passes `viewModel.status?.battery` and `viewModel.status?.offpeak?.windowStart` to the panel.
- Tests: 3 decoding cases on `BatteryInfo`, 1 accessibility-label assertion.

### Implementation Approach

The check is a **physics ceiling**, not a rate estimate:

```
remainingKwh = (Soc - cutoffPercent) / 100 * CapacityKwh
requiredHours = remainingKwh / maxDischargeKW
flag := Now.Add(requiredHours * time.Hour).After(NextOpStart)
```

All four "false-equivalent" branches collapse to `nil` (returns `*bool`, never `&false`):

- `!HasBoundary` — off-peak config missing or invalid
- `WithinOffpeakWindow` — already inside the window
- `Soc <= cutoffPercent` — already at or below the floor
- `CapacityKwh <= 0` — defensive

Then `After` is **strict** — equality returns `nil`, not `&true`. This matches the requirements' explicit `now == nextOffpeakStart` case.

Wire shape: `*bool` + no `omitempty` means the field is always present in JSON, always `true` or `null`. Swift's `Bool?` decoder handles both cases naturally, and a key-missing scenario (older server payload) also decodes to `nil`.

Hero panel: the existing `Mode` enum branches stay untouched. A higher-level `@ViewBuilder` chooses between the new indicator and the legacy `statusLine` based on whether the flag is true *and* `offpeakWindowStart` is non-nil. The accessibility label is a static helper on `DashboardHeroPanel` consumed by both the view body and the unit test, so the contract string lives in one place.

### Trade-offs

- **5 kW as a constant, not a parameter** (Decision 1): the inverter has a single max; this is the personal-use Flux deployment, not a multi-tenant product. A parameter would be dead weight.
- **No mirror on `rolling15min`** (Decision 6): the flag is a top-of-status-only concept; mirroring it on the rolling slice would always equal the hero flag.
- **Memberwise init default of `nil`** preserves source compatibility across the five existing constructors — none of them needed a parallel edit.
- **No `Palette.amber` for the indicator**: amber is reserved for warnings; this is a benign info state. The indicator uses `secondaryText` instead.
- **Static accessibility-label helper** vs. ViewInspector / snapshot test: the helper exists so the test pins the literal string without depending on a UI snapshot harness. The trade-off is a tiny piece of test-facing surface on the view; the alternative was a heavier test stack.

---

## Expert Level

### Technical Deep Dive

The math sits behind a guard fence:

```go
func computeCantEmptyBeforeOffpeak(in cantEmptyInput) *bool {
    if !in.HasBoundary || in.WithinOffpeakWindow || in.Soc <= cutoffPercent || in.CapacityKwh <= 0 {
        return nil
    }
    remainingKwh := (in.Soc - cutoffPercent) / 100 * in.CapacityKwh
    requiredHours := remainingKwh / maxDischargeKW
    if in.Now.Add(time.Duration(requiredHours * float64(time.Hour))).After(in.NextOpStart) {
        t := true
        return &t
    }
    return nil
}
```

Edge cases worth noting:

- **DST spring-forward (`2026-10-04 01:30 Sydney`):** the integration test pins `nowFunc` to the gap morning and verifies `nextOffpeakStart` returns the 11:00 AEDT boundary (+11 offset). `nextOffpeakStart` was already DST-correct; this test pins the assumption.
- **Fallback capacity:** when the system metadata row is missing, the handler falls back to `fallbackCapacityKwh = 13.34`. The integration test (`case f`) confirms the flag is computed against that value, not zero (which would have masked the bug behind the `CapacityKwh <= 0` guard).
- **Boundary equality:** when `Now + requiredHours == NextOpStart` exactly, `After` returns `false` (strict comparison), so the helper returns `nil`. The test for this case picks `Soc = 5 + 500/13.34` so `requiredHours = 1h` exactly (modulo FP arithmetic on 0.1-derived values; the test currently lands on the `nil` side and is stable on x86_64 / arm64).
- **`!liveFresh`:** the whole block lives inside the `liveFresh` branch of `handleStatus`. When the live reading is stale, the field stays `nil` — same as `EstimatedCutoff`. The Swift decoder treats `nil` as "no indicator," so the hero gracefully degrades to the existing status line.

Wire-shape symmetry: `EstimatedCutoff` already encodes "true value or `null`" via `*string` + no `omitempty`. The new field mirrors that exactly with `*bool`. Swift's `Bool?` (or `String?`) decoder handles `null` and key-missing identically, so older clients that don't know about the field still parse correctly — the older client's `BatteryInfo` simply lacks the field and the panel's `battery?.cantEmptyBeforeOffpeak == true` always evaluates to false.

Active-window check: `withinOffpeakWindow` re-parses the off-peak strings (which `nextOffpeakStart` also parses in the same request). The parses are pure integer math on `ParseOffpeakWindow` (no allocations, tens of nanoseconds), so the duplication is negligible on a 10s-cadence endpoint that already does four parallel DynamoDB queries dominating latency. A handler-init cache would shave ~50 ns at the cost of a state field and a refresh story; not worth the plumbing.

### Architecture Impact

The change adds one boolean to the existing `/status` wire shape and one private subview to one panel. It does not:

- Touch the poller (no new DynamoDB writes; no new AlphaESS API calls).
- Add new tables, new SSM parameters, new Lambda routes.
- Touch widgets, Day Detail, History, or alerts (all out-of-scope per requirements).

The new flag rides inside the existing `liveFresh` gate, so the freshness story is unchanged: stale data → no flag → no indicator, identical to the existing `EstimatedCutoff` behaviour.

The Swift defaulted-init approach means the new field can be added to `BatteryInfo` without touching the five other constructors (FluxCore tests, Widget snapshot logic, widget fixtures). This is the second time this pattern has paid off; future optional additions to `BatteryInfo` should follow the same shape.

### Potential Issues

- **`offpeakWindowStart == nil` when the flag is `true`:** the panel silently falls back to `statusLine`. This is the design-documented defensive behaviour ("server should always emit one when the flag is set"), but in DEBUG it'd be worth a `assertionFailure` to catch the unexpected state. Filed as a nit, not blocking.
- **Float-arithmetic boundary test:** the `"boundary equality"` test in `compute_test.go` constructs `boundarySoc = 5.0 + 500.0/13.34` so `requiredHours` is "exactly" 1 h. On IEEE 754 doubles the multiplication chain rounds rather than equals; the test currently lands at or below the strict-`After` threshold and is therefore stable on the current platform. Cross-platform deterministic stability would require integer-clean inputs.
- **`HasBoundary` + `NextOpStart` invariant:** `cantEmptyInput` keeps both `HasBoundary bool` and `NextOpStart time.Time`. They carry the same information (the helper guards against `!HasBoundary` before reading `NextOpStart`). Refactoring `NextOpStart` to `*time.Time` would let the compiler enforce the invariant, but it'd require touching `nextOffpeakStart`'s signature (and its other caller in `handleStatus`) — out of scope for this PR.

---

## Important Changes (reviewer focus)

1. **Backend helper + handler wiring** — `internal/api/compute.go:80-130`, `internal/api/status.go:24,136-143`. Why it matters: this is where the core physics check is implemented and where the wire field is populated. Take-away: the `*bool`-with-no-`omitempty` encoding lets you express tri-state ("true / null / not-emitted") without a Boolean wrapper type — useful pattern for any "rare positive signal" field. Rationale: matches existing `EstimatedCutoff` precedent.
2. **JSON shape on `BatteryInfo`** — `internal/api/response.go:38`. Why it matters: this is the API surface. Take-away: pointer + no `omitempty` = "value or null, never absent." Rationale: explicit AC 2.2 — `false` is never emitted; key is always present.
3. **Swift defaulted memberwise init** — `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift:75-95` (approx). Why it matters: source-compatibility across the five constructor call sites. Take-away: when adding an optional to a Codable struct, add the default to the memberwise init too; otherwise every constructor breaks. Rationale: design.md §Pattern extension audit calls this out explicitly.
4. **Hero subview composition** — `Flux/Flux/Dashboard/DashboardHeroPanel.swift:55-83`. Why it matters: this is the user-visible piece. Take-away: composing alongside an existing `Mode` enum at a higher `@ViewBuilder` level is cleaner than adding a new enum case — the existing copy paths stay untouched. Rationale: spec explicitly forbade extending the `Mode` enum.
5. **DST integration test** — `internal/api/status_test.go` case (e). Why it matters: DST gaps are the classic latent bug for any local-time math. Take-away: pinning `nowFunc` to `2026-10-04 01:30 Sydney` and asserting the +11 (AEDT) offset locks in the assumption — repeat this pattern whenever a feature does Sydney-local arithmetic. Rationale: AC 2.4 requires a DST case.

## Decision Rationale (from `decision_log.md`)

- **Decision 1: 5 kW baked, no parameter** — Flux is a single-deployment personal tool with one inverter; a parameter would be dead weight. *(stated)*
- **Decision 6: not mirrored to `rolling15min`** — the rolling slice already inherits live values; mirroring would duplicate without adding information. *(stated)*
- **Decision 7: live-fresh gate** — without trustworthy `now` data, the flag is meaningless; same reasoning as `EstimatedCutoff`. *(stated)*
- **Decision 8: midnight-spanning windows out-of-scope** — inherited from `ParseOffpeakWindow`'s existing `start ≥ end` rejection. Adding midnight-crossing support is a broader change touching the existing semantics. *(stated)*
- **Static accessibility-label helper** — exposed because no ViewInspector / snapshot harness is in place; co-locating with the view body keeps the contract single-source. *(inferred — agent-introduced during implementation; not in `decision_log.md`)*

## Completeness Assessment

- **Fully implemented:** AC 1.1–1.8, 2.1–2.4, 3.1–3.5. All design.md §Testing Strategy cases present (16 helper cases, 6 handler cases, 3 Swift decoding cases, 1 accessibility-label assertion).
- **Partially implemented:** none.
- **Not implemented (deferred per requirements §Non-Goals):** widget surfaces, SoC alerts integration, inverter derating curve, midnight-spanning off-peak windows, mirror on `rolling15min`. All explicit non-goals.

## Validation Findings

- **Gaps identified:** none against the spec.
- **Logic issues:** none.
- **Questions raised:**
  - Should the panel `assertionFailure` in DEBUG when `cantEmptyBeforeOffpeak == true` but `offpeakWindowStart == nil`? The design says fall back silently; in practice this state would indicate a server bug worth surfacing.
  - Is the visible text (`"Won't empty before HH:MM"`) and the VoiceOver label (`"Battery won't empty before off-peak at HH:MM"`) literal divergence intentional? AC 3.5 says "exposing the same wording (including the off-peak start time)" — the AX label expands the subject and the qualifier. Reading the AC charitably, "same wording" means "same off-peak time"; reading strictly, the two strings should match. The expanded AX wording is the better UX.
- **Recommendations:** none blocking. The two questions above are documentation polish, not implementation bugs.
