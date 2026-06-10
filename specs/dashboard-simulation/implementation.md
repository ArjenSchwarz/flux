# Implementation: Dashboard Simulation (T-1495)

A three-level explanation of the implemented feature, written as a pre-push review aid, followed by a completeness assessment mapped to the five requirement groups.

---

## Beginner Level

### What This Does

The Dashboard normally shows your battery's real numbers: how much power the house is using, how fast the battery is draining, and roughly when it will be "empty". Dashboard Simulation lets you ask a what-if question — "what would happen if I started charging the car right now?" — and see the answer on the same screen, without actually turning anything on.

You set up named scenarios called **presets** in Settings. A preset is just a label plus a wattage, like "Charge car" at 1700 watts. On the Dashboard there is a new **Simulate** pill beside the big battery percentage. Tap it, pick a preset, and the figures change to show the picture *as if* that extra load were running. A coloured banner appears across the top saying you are looking at a what-if, and the affected numbers change colour so you can never mistake them for real readings. Tap **Stop** (or just relaunch the app) and everything snaps back to reality.

### Why It Matters

It answers a practical question — "can my battery handle this, or will I end up buying expensive grid power?" — before you commit. On a sunny day it shows the extra load eating into the solar you would otherwise sell back to the grid; in the evening it shows the battery draining faster and an earlier "empty by" time.

### Key Concepts

- **Preset**: a saved scenario (a name and a watt value). Stored on the server so it appears on all your devices and survives reinstalling the app.
- **Simulated vs real**: nothing is sent to the battery — the feature is read-only. It only changes what you *see*.
- **The banner and the colour**: a deliberate signal that the numbers are hypothetical. Turning simulation off removes both.
- **Server-computed**: the what-if numbers are worked out by the same backend that produces the real numbers, so the two can never disagree on how a value is calculated.

---

## Intermediate Level

### Changes Overview

The change spans three areas:

1. **Go backend** — a new `simulateLoadWatts` query parameter on `GET /status` returns a what-if status, plus a new system-wide presets CRUD resource (`/simulation-presets`) backed by a new DynamoDB table `flux-simulation-presets`.
2. **FluxCore (shared Swift)** — a `SimulationPreset` model + `SimulationPresetDraft`, five new `FluxAPIClient` methods (with default no-op fallbacks), a `URLSessionAPIClient+Simulation` extension, and a `SimulationPresetsService` (`@Observable`, server-confirmed-then-apply CRUD).
3. **iOS/macOS UI** — Settings gets a Simulation section (`SimulationPresetsView`/`Editor`/`ViewModel`); the Dashboard gets a `SimulationBanner`, a `DashboardSimulateMenu`, and tinting/accessibility changes in `DashboardViewModel`, `DashboardView`, `LiveTrioPanel`, and `DashboardHeroPanel`.

### Implementation Approach

The defining decision is **compute the simulated status server-side** (Decision 6). The "empty by" estimate is not a pure formula — it depends on handler policy (a 90s freshness gate, off-peak-window suppression, a 15-minute rolling average, and a separate "won't empty before off-peak" indicator). Re-deriving any of that in Swift would create a second source of truth that must track the Go handler forever — exactly the cross-screen divergence the project's data-consistency rule forbids. So the client sends an added-load parameter and renders whatever the server returns.

On the backend, `status.go` reads and validates `simulateLoadWatts`, then `status_simulate.go` allocates the added load `W` via a **priority waterfall**: reduce grid export first, then increase battery discharge up to the inverter ceiling, then import the remainder from the grid. The same allocation feeds the live trio, the live cutoff, and the rolling cutoff, so the whole response stays energy-balanced.

The presets resource deliberately reuses two different precedents (Decision 10): the **backend mirrors `/pricing`** (single system-wide table keyed by id only, list-all via Scan, no device/user partition), while the **client service/UI mirror SoC Alerts** (the closer UI shape, with server-confirmed-then-apply error handling). The simulated-status call is a *separate* method `fetchStatus(simulateLoadWatts:)`, not a change to `fetchStatus()`, so the widget-timeline and settings-validation call sites can never accidentally simulate.

On the client, the active simulation lives only in `DashboardViewModel` as `activeSimulationPresetID` — in-memory, `nil` on cold launch (Decision 5). Each refresh re-resolves the preset's *current* watts from the presets list; if the active id has vanished (deleted locally or via sync) simulation turns itself off.

### Trade-offs

- **Server-side compute** means simulation is unavailable offline or when live data is stale — accepted, because stale what-ifs would be misleading anyway, and it cleanly resolves the off-peak/stale edge cases.
- **One preset at a time** (Decision 3) keeps the menu and the power model simple; no stacking.
- **Suppressing the off-peak indicator while simulating** (Decision 11) avoids a real reassurance appearing beside a simulated estimate, at the cost of temporarily hiding that indicator.
- **Not caching a simulated status for widgets** (Decision 13) keeps widgets showing real data, at the cost of pausing widget-cache updates while a simulation runs.

---

## Expert Level

### Technical Deep Dive

**The waterfall (`internal/api/status_simulate.go`).** Sign convention: `pbat > 0` discharge, `pgrid > 0` import (`< 0` export). Given added load `W`:

- `exportReductionFor(pgrid, w) = min(w, max(0, -pgrid))` — added load served by cutting current export first.
- `wBattery = W − exportReduction` — the remainder reaching the battery.
- `headroom(p) = max(0, maxDischargeW − p)` — discharge the battery can still take on; `maxDischargeW = maxDischargeKW * 1000` reuses the existing 5 kW constant so the ceiling has one definition.
- `batteryAbsorbed(p, wBattery) = min(wBattery, headroom(p))`; `simDischarge(p, wBattery) = p + batteryAbsorbed(p, wBattery)`.
- `overflow = wBattery − batteryAbsorbed` → grid import.

The load-bearing subtlety is that the cap is applied to the *added* portion via `headroom`, not as `min(p + wBattery, maxDischargeW)`. The naïve `min` would *lower* a real reading that already sits at/above the ceiling and, at `W = 0`, return `maxDischargeW ≠ pbat` — breaking zero-load equivalence ([4.2]). The headroom form guarantees `simDischarge(p, 0) == p` for *every* `p`, including `p ≥ ceiling`, and `simDischarge(p, wBattery) ≥ p` for all `wBattery ≥ 0` ([3.4] — added load never lowers the shown discharge).

`headroom` is evaluated **per series**: the live tile caps against `latest.Pbat`, the rolling "empty by" against `avgPbat`. `exportReduction` is threaded into both cutoff series — intentional, because it only bites while exporting (where the cutoff is `nil` anyway), so it never corrupts a real "empty by"; in the importing/zero-grid evening case it is 0 and `wBattery == W`.

**Off-peak suppression ([4.3], Decision 11).** `computeCantEmptyBeforeOffpeak` is pbat-independent — it answers "even at the 5 kW ceiling, can the battery reach cutoff before off-peak?", a worst-case reassurance meaningless under an added-load what-if. The hero renders it *instead of* the "empty by" line (mutually exclusive). So `status.go` returns `CantEmptyBeforeOffpeak = nil` whenever `W > 0`, forcing the hero onto the simulated `statusLine` path. The existing `ct.Before(nextOpWindowStart)` boundary suppression and the `liveFresh` gate are untouched and apply identically to the simulated cutoff ([4.1]/[4.5]).

**Parameter validation.** `parseSimulateLoad` accepts an empty/absent value (→ `0`, no simulation), an integer in `(0, 20000]`, else `errInvalidSimLoad` → HTTP 400 before any I/O ([4.6]). Crucially `0` is *rejected on the wire* (Req's "greater than zero" rule), so zero-load equivalence ([4.2]) is exercised only at the compute layer, never via `?simulateLoadWatts=0` — the property test (`status_simulate_property_test.go`, `pgregory.net/rapid`) asserts this and monotonicity (`W↑ ⇒ cutoff no later`, plateauing at saturation) plus `simDischarge ≥ pbat`.

**Presets resource.** `flux-simulation-presets` is keyed by `presetId` (HASH only), id-only, list-all via Scan with a 50-row page limit; the `SimulationPresetItem.PresetID` serialises as `json:"id"` so Swift decodes it through `Identifiable`. Handlers assign id/timestamps server-side, preserve `createdAt` on PUT, 404 on unknown id, idempotent DELETE (204), 409 at the 20-preset cap, 400 on validation failure (`label` 1..40 runes, `watts` 1..20000). No sentinel/transactional machinery (unlike pricing). IAM grants `Scan/PutItem/DeleteItem`; `TABLE_SIMULATION_PRESETS` env var added; table is `DeletionPolicy: Retain` with PITR.

**Client state machine.** `DashboardViewModel.refresh()` resolves watts from `simulationService.presets.first(where: id)` each cycle; absent id → clears `activeSimulationPresetID` ([2.4]); records the resolved watts/name into `activeSimulationDeltaWatts`/`activeSimulationName` *before* the fetch so the banner shows immediately and survives a failed fetch ([4.5], [5.1]). On a simulated success it `return`s early, skipping `widgetCache.writeIfNewer` and the reload trigger (Decision 13). `activate`/`stop` both call `refresh()` immediately so figures and tinting change in one cycle.

### Architecture Impact

- **One source of truth preserved.** The client never re-derives a shared metric; the simulated and real numbers come down the same `/status` pipeline. This is the explicit reason for the server-side approach and keeps the project's data-consistency invariant intact.
- **Pattern reuse.** Backend = pricing precedent; client = SoC Alerts precedent. Almost no novel infrastructure — new table, new handler file, new service, all shaped like existing ones.
- **Additive API surface.** The five new `FluxAPIClient` methods ship with default extensions (no-op `fetchStatus(simulateLoadWatts:)` delegates to `fetchStatus()`; CRUD throws `.notConfigured`), so ~30 existing conformers, the widget, and the settings client compile unchanged. Only `URLSessionAPIClient` overrides them.
- **Banner ≠ figure agreement is intentional.** The banner's `+1.7 kW` is the preset's *added* load; the hero's net battery figure is different (e.g. 1.0 kW charging + 1.7 kW preset → 0.7 kW discharging). [2.7]'s "agreement" is that the watts *sent* match the displayed status, not that the banner equals the hero number.

### Potential Issues

- **`maxDischargeKW` is a fixed 5 kW constant.** If the real inverter ceiling differs, the cap (and the off-peak indicator's model) is wrong; it updates in one place but is not configurable per system.
- **Self-consumption priority assumed** (reduce export before reducing battery charge). A feed-in-priority inverter configuration would allocate differently; the design records this as a known limitation.
- **Forced grid-charging not modelled.** A battery charging *while importing* (scheduled off-peak grid charge) is shown shifting toward discharge with grid unchanged — accepted, since the feature targets solar/evening states.
- **`label.count` vs trimmed length.** Validation trims for the empty check but measures `label.count` (untrimmed) for the 40-char cap; server counts runes on the raw label. Trailing whitespace counts toward the cap on both sides, which is consistent but worth noting.

---

## Completeness Assessment

Every task in `tasks.md` 1–17 is checked; task 18 (run full Go + iOS + macOS test/lint) is the only unchecked item and is a verification gate rather than implementation. All five requirement groups can be explained cleanly against the diff.

### Group 1 — Manage Simulation Presets — Fully implemented
- 1.1/1.2 create/edit/delete: `SimulationPresetsView` + `Editor` + `ViewModel`, service CRUD, backend handlers.
- 1.3 validation (empty label, watts ≤ 0, watts > 20000): both `SimulationPresetDraft.validate()` and server `simulationPresetPayload.validate()`, with a user-visible reason; backed by tests on both sides including 1/40-char and 1/20000-watt boundaries.
- 1.4 server-side, synced: `flux-simulation-presets` table, id-only key, system-wide (Decision 10).
- 1.5 in Settings on iOS + macOS: `SettingsFeatureSections` (iOS) and `macOSNavSection` (macOS).
- 1.6 server-confirmed-then-apply with visible error: `SimulationPresetsService` mirrors SoC Alerts; editor keeps the sheet open on failure.

### Group 2 — Activate From the Dashboard — Fully implemented
- 2.1 control on both platforms: `DashboardSimulateMenu` is injected into `DashboardHeroPanel` as a top-trailing pill, so it renders on every layout — compact iPhone (`dashboardContent`) and regular iPad/macOS (`dashboardContentRegular`) alike (Decision 15). Presets are loaded by a `DashboardView` `.task` on appear, so the menu is populated without first visiting Settings ▸ Simulation.
- 2.2 replace-on-switch: `activateSimulation` overwrites the single `activeSimulationPresetID`.
- 2.3 empty state offers a path to create: "Add a preset…" item (SettingsLink on macOS, callback on iOS).
- 2.4 deleted/synced-away active preset turns simulation off: `refresh()` clears the id when absent from the list (tested).
- 2.5 transient session state: in-memory id, nil on cold launch, survives refresh/tab nav (Decision 5).
- 2.6 exactly one request per cycle carrying the active watts: single `fetchStatus(simulateLoadWatts:)` per `refresh()`.
- 2.7 watts re-resolved each cycle; banner sources from the watts that produced the displayed status: `activeSimulationDeltaWatts` recorded pre-fetch (tested via cross-device edit).

### Group 3 — Simulated Power Flow — Fully implemented
- 3.1 house load + W; 3.2 waterfall (reduce export → battery → balanced); 3.3 unchanged when off (true no-op at W=0); 3.4 cap at the ceiling with grid-import overflow, discharge never below real `pbat`. All in `allocateSimLoad`, with energy-balance and ceiling tests plus the `rapid` properties.

### Group 4 — Simulated Battery Life — Fully implemented
- 4.1 "empty by" derived from the allocated/capped battery, same handler policy; live discharge and cutoff share one figure.
- 4.2 zero-load equivalence — exercised at the compute layer (the headroom form is the mechanism), property-tested across boundary conditions including `pbat ≥ ceiling`.
- 4.3 off-peak indicator vs simulated "empty by" never coexist — server forces `CantEmptyBeforeOffpeak = nil` while W>0.
- 4.4 no "empty by" when not a net discharge or already at cutoff — inherited from `computeCutoffTime`'s existing `nil` behaviour.
- 4.5 stale/failed → no fabricated values, banner stays up, figures fall back to the error path (`isSimulating` independent of data availability; tested).
- 4.6 invalid/over-range parameter → 400, treated as simulation-unavailable.

### Group 5 — Clear Simulation Labelling — Fully implemented
- 5.1 persistent banner naming the preset; 5.2 distinct `FluxTheme.Palette.simulation` accent + SF Symbol, separate from the calm hero off-peak line; 5.3 changed values tinted (Trio House, hero discharge/empty-by); 5.4 accessibility — banner and tinted values announced as simulated (`SimulatedSublineAccessibility`, trio `columnAccessibilityLabel`); 5.5 stop removes banner + markings and returns to real values in one cycle.

### Partial / Verification-pending
- **Task 18 (full test/lint on Go + iOS + macOS) is unchecked.** Unit/property/handler tests exist across all layers, but the consolidated cross-platform build+lint run is the remaining gate before push.

### Missing
- None against the requirements. The one explicitly-deferred behaviour — an on-screen "beyond battery output" caveat — was rejected in favour of computing the value correctly (Decision 14), so it is a non-goal, not a gap.

### Validation note
No requirement resisted a clean explanation. The only subtleties worth a reviewer's eye are documented above: the headroom-vs-naïve-min cap (the mechanism behind 4.2), the intentional banner-delta vs hero-net-figure difference (not a contradiction of 2.7), and the untrimmed `label.count` for the length cap (consistent client/server, but trailing whitespace counts toward 40).
