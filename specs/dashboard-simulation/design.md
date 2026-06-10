# Design: Dashboard Simulation

## Overview

A `simulateLoadWatts` query parameter on `GET /status` makes the Go handler return a what-if status (added load, increased discharge, recomputed "empty by") so the client renders one server-authored picture and never re-derives a shared metric. Named presets (label + watts) are a new server-side CRUD resource modelled on the existing system-wide `/pricing` resource, so they sync across the user's devices, surfaced in Settings and selectable from the Dashboard.

## Architecture

Two independent server additions plus client wiring:

1. **Simulated status** — a parameter on the existing `/status` path. No new endpoint, no new storage.
2. **Simulation presets** — a new system-wide CRUD resource. The backend mirrors `/pricing` (single table keyed by row id only, no device/user partition, list-all); the client service and editor mirror SoC Alerts (closer UI shape).

### Simulated status — injection sites

The added load `W` is applied at every point where battery power feeds a value the Dashboard renders. `internal/api/status.go` reads `simulateLoadWatts` from `req.QueryStringParameters` (same access pattern as `history.go`/`day.go`), validates it, and threads it through. The load is allocated by a **priority waterfall** matching self-consumption-mode hardware: first reduce any grid export, then draw from the battery up to its sustained-discharge ceiling `maxDischargeW = maxDischargeKW * 1000` (the existing 5 kW constant in `compute.go`, reused so the ceiling has one definition), then import the rest from the grid. Sign convention: `pbat > 0` discharge, `pgrid > 0` import. The derived quantities (computed from the live snapshot):

- `exportReduction = min(W, max(0, -latest.Pgrid))` — added load served by cutting current export first
- `wBattery = W - exportReduction` — the remainder that reaches the battery
- `headroom(p) = max(0, maxDischargeW - p)` — discharge the battery can still take on (0 if `p` is already at/above the ceiling)
- `batteryAbsorbed(p) = min(wBattery, headroom(p))`; `simDischarge(p) = p + batteryAbsorbed(p)` — caps only the *added* discharge, so `simDischarge(p) ≥ p` always (it never reduces a real reading)
- `overflow = wBattery - batteryAbsorbed(latest.Pbat)` — what the battery can't take on, met by grid import

Capping the added portion via `headroom` (rather than `min(pbat + wBattery, maxDischargeW)`) matters when a real reading is already at/above the ceiling: the naïve `min` would *lower* that reading and, at `W = 0`, return `maxDischargeW ≠ pbat`, breaking zero-load equivalence ([4.2](requirements.md#4.2)). The headroom form is identical whenever `pbat ≤ maxDischargeW` and is a true no-op at `W = 0`.

| Site (`status.go`) | Source value | Under simulation | Renders as |
|---|---|---|---|
| `Live.Pload` | `latest.Pload` | `+ W` | Trio "House" |
| `Live.Pbat` | `latest.Pbat` | `simDischarge(latest.Pbat)` | Hero subline "Discharging · X kW", hero mode |
| `Live.Pgrid` | `latest.Pgrid` | `+ exportReduction + overflow` (both 0 when not exporting and below the ceiling → unchanged) | Trio "Grid" |
| `battery.EstimatedCutoff` | `computeCutoffTime(latest.Soc, latest.Pbat, …)` | `simDischarge(latest.Pbat)` | not read by client; same inline math keeps the whole response coherent for any API/debug consumer |
| `rolling15m.EstimatedCutoff` | `computeCutoffTime(latest.Soc, avgPbat, …)` | `simDischarge(avgPbat)` | **Hero "empty by HH:MM"** |
| `rolling15m.AvgLoad` / `AvgPbat` | `computeRollingAverages(…)` | `AvgLoad + W`; `AvgPbat → simDischarge(avgPbat)` | not rendered today; carry the same adjustment so the returned averages stay coherent |
| `battery.CantEmptyBeforeOffpeak` | `computeCantEmptyBeforeOffpeak(…)` | forced `nil` when `W > 0` | suppressed (see below) |
| `Live.Ppv` | unchanged | unchanged | Trio "Solar" |

`wBattery` (not `W`) is the amount that reaches the battery, so `simDischarge` and the cutoffs use it; `headroom` is evaluated per-series (against `latest.Pbat` for the live tile, `avgPbat` for the rolling cutoff), so each series caps independently. `exportReduction` is computed from the live grid and threaded into both cutoff series — intentionally: it only bites while you're exporting (sunny, battery charging), where the cutoff is `nil` anyway, so it never makes the "empty by" wrong; in the importing/zero-grid evening case `exportReduction = 0` and `wBattery = W`, exactly the simple form. One state is deliberately not modelled: a battery *charging while importing* (e.g. a scheduled off-peak grid charge) — added load there is shown shifting the battery toward discharge with grid unchanged, which is acceptable for a what-if since the feature targets solar/evening states, not forced grid-charging.

The waterfall handles every starting state with one rule (`pbat > 0` discharge, `pgrid < 0` export):

- **Importing / zero grid (evening peak):** `exportReduction = 0`, so `wBattery = W` shifts the battery toward discharge; above the 5 kW ceiling the remainder becomes grid import — the load-bearing fix for the car-on-top-of-evening-draw case ([3.4](requirements.md#3.4)), where the battery is shown at the ceiling and the "empty by" reflects the real (capped) drain rather than an impossibly fast one.
- **Charging, zero grid (mild sun):** `wBattery = W` against a negative `pbat` crosses charge→discharge naturally (charging 1.0 kW + 1.7 kW → 0.7 kW discharge).
- **Charging *and* exporting (full sun):** `exportReduction` consumes the export first, so the battery keeps charging and only the export figure drops (charging 2.0 kW / exporting 2.5 kW + 1.7 kW → still charging 2.0 kW, exporting 0.8 kW). This is what self-consumption-mode hardware does, and it's the case the plain "battery absorbs everything" model got wrong.

In all cases load increase `W` equals battery increase + grid increase, so the trio stays energy-balanced. `computeCutoffTime` already returns `nil` when the (allocated, capped) `pbat <= 0` or `soc <= cutoffPercent`, so [4.4](requirements.md#4.4) is inherited unchanged. The off-peak boundary suppression (`ct.Before(nextOpWindowStart)`) and the 90s `liveFresh` gate are likewise unchanged and apply identically to the simulated cutoff — satisfying [4.1](requirements.md#4.1) and [4.5](requirements.md#4.5). When `W = 0` every step is a no-op, so the simulated response equals the real one ([4.2](requirements.md#4.2)).

**Off-peak indicator (resolves [4.3](requirements.md#4.3)).** `computeCantEmptyBeforeOffpeak` is pbat-independent — it asks "even at the 5 kW inverter ceiling, can the battery reach cutoff before off-peak?" That worst-case reassurance is meaningless under an added-load what-if, and the hero renders it *instead of* the "empty by" line (`DashboardHeroPanel.subline`, mutually exclusive). So while `W > 0` the handler returns `CantEmptyBeforeOffpeak = nil`; the hero then always takes the `statusLine` path and shows the simulated "empty by" (or just the discharge rate when the simulated cutoff lands after off-peak). The real indicator and a simulated "empty by" can never appear together.

### Simulation presets — CRUD, system-wide (mirrors `/pricing`)

The `/pricing` resource is already a system-wide, single-table CRUD with no device or user partition: `flux-pricing` is keyed by `pricingId` (HASH only) and `ListPricing(ctx)` scans the whole table. That is exactly the shape presets need — one monitored system, list-all, synced across every device that talks to the backend ([1.4](requirements.md#1.4)) — with **no** device path segment and **no** partition-scoping decision to make. Presets therefore mirror `/pricing`'s handler/store shape, **minus** its singleton-sentinel and transactional machinery (those exist only for pricing's open-ended-period coupling; presets are independent rows).

Endpoints (bearer-token auth, same global middleware — identical to `/pricing`):

```
GET    /simulation-presets
POST   /simulation-presets
PUT    /simulation-presets/{id}
DELETE /simulation-presets/{id}
```

A defensive cap of 20 presets is enforced on create (returns 409) — chosen for selection-menu legibility on the Dashboard rather than any storage limit.

### Client data flow

```
SimulationPresetsService (FluxCore, @Observable)  ──CRUD──▶  /simulation-presets
        │ presets: [SimulationPreset]
        ▼
SettingsView ▸ "Simulation" section ▸ SimulationPresetsView (list + editor sheet)

DashboardViewModel
  • activeSimulationPresetID: String?   (in-memory; nil on cold launch → [2.5])
  • resolves watts from presets list each refresh → fetchStatus(simulateLoadWatts:)
  • if active ID absent from list → clears it ([2.4]); watts re-read each cycle ([2.7])
        ▼
DashboardView passes isSimulating + active preset name to panels
  • SimulationBanner (new, distinct treatment)
  • Trio "House" / hero subline values tinted as simulated
```

The active simulation is view-model state, not persisted, so it resets on cold launch ([2.5]) and survives auto-refresh/tab changes within a session.

**Immediacy.** Activating a preset, switching presets, or turning simulation off does not wait for the next auto-refresh tick — each triggers an immediate `refresh()` so the on-screen figures and tint change at once. Turning off clears `activeSimulationPresetID` and immediately re-fetches without the parameter, so the real values and the removal of all simulated markings ([5.5](requirements.md#5.5)) happen in the same cycle rather than lingering until the next tick.

**Banner/figure agreement ([2.7]).** The banner's preset name and signed delta are sourced from the watt value that produced the *currently displayed* status (the view model records the watts it last sent), not from the live presets list. So even across a cross-device watts edit, the banner and the figures always describe the same watts; the next refresh re-resolves the watts from the updated list and both move together.

**Stale while simulating ([4.5], Req 5).** Whether the banner shows is driven by the toggle state (simulation active), independent of data availability. If live data goes stale or the fetch fails mid-simulation, the banner stays up but the affected values fall back to the existing "Awaiting live data" / error treatment — no simulated figures are fabricated.

### Pattern-extension audit — every reader of the injected values

| Consumer | File:symbol | Needs simulation handling? |
|---|---|---|
| Trio House | `LiveTrioPanel` `live?.pload` | No code change — reads simulated `live` from response; add "simulated" tint via `isSimulating` |
| Trio Solar/Grid | `LiveTrioPanel` `live?.ppv/pgrid` | No — values unchanged by sim |
| Hero discharge/charge line | `DashboardHeroPanel.mode` (`live.pbat`) | No code change — reads simulated `pbat`; tint via `isSimulating` |
| Hero "empty by" | `DashboardHeroPanel` `rolling15min?.estimatedCutoffTime` | No code change — server returns simulated value |
| Hero off-peak indicator | `DashboardHeroPanel` `battery.cantEmptyBeforeOffpeak` | Server returns `nil` under sim; no client change |
| Widget cache | `DashboardViewModel.refresh` `widgetCache.writeIfNewer` | **Yes — must NOT cache simulated status** (widgets show real data; Non-Goal). Skip the cache write while simulating |
| Widget timeline fetch | `Widget/StatusTimelineLogic.swift:90` `client.fetchStatus()` | **Yes — must keep the unsimulated call.** It uses the no-arg `fetchStatus()`; the simulation parameter is a *separate* method only the Dashboard calls (see API section) so this path can never simulate |
| Settings validation fetch | `SettingsViewModel.swift:75` `validationClient.fetchStatus()` | Keeps the no-arg `fetchStatus()`; never simulates |
| `battery.estimatedCutoffTime` | unused by client | Kept consistent server-side; no client impact |

The widget-cache row is the one non-obvious consumer: `refresh()` currently writes every fetched status to the shared widget cache. A simulated status must not leak into widgets, so the cache write (and widget reload trigger) is skipped while `activeSimulationPresetID != nil`.

## Components and Interfaces

### Backend (Go)

```go
// internal/dynamo/simulationpresets.go  (mirrors PricingItem — id-only key, no partition)
type SimulationPresetItem struct {
    PresetID  string `dynamodbav:"presetId"  json:"id"`
    Label     string `dynamodbav:"label"     json:"label"`
    Watts     int    `dynamodbav:"watts"     json:"watts"`
    CreatedAt string `dynamodbav:"createdAt" json:"createdAt"`
    UpdatedAt string `dynamodbav:"updatedAt" json:"updatedAt"`
}
// Store.ListPresets(ctx) (Scan) / PutPreset(ctx, item) / DeletePreset(ctx, presetId) — same shape as DynamoPricingStore

// internal/api/simulationpresets.go
type simulationPresetPayload struct { Label string `json:"label"`; Watts int `json:"watts"` }
func (p simulationPresetPayload) validate() error // label 1..40 chars; watts 1..20000

// internal/api/compute.go — no new function needed; callers pass pbat+W inline.
// handleStatus parses/validates the param:
func parseSimulateLoad(q map[string]string) (float64, error) // "", or 0<v<=20000, else error → 400
```

Handler behaviour: an out-of-range or unparseable `simulateLoadWatts` returns 400 with `{"error": …}` ([4.6](requirements.md#4.6)); presets are validated identically on write, so a valid preset can never produce a rejected status request.

### Client (Swift / FluxCore)

```swift
// FluxCore/Models/SimulationPreset.swift
public struct SimulationPreset: Identifiable, Codable, Sendable, Equatable {
    public let id: String
    public var label: String
    public var watts: Int
    public let createdAt: Date
    public var updatedAt: Date
}
public struct SimulationPresetDraft: Sendable, Equatable {
    public var label: String = ""
    public var watts: Int = 0      // starts invalid (validation requires >0) → forces a deliberate entry, like the empty-label rule
    public func validate() -> ValidationError?   // emptyLabel, labelTooLong(40), wattsOutOfRange(1...20000)
}

// FluxCore/Networking/FluxAPIClient.swift — additions (existing fetchStatus() is UNCHANGED)
func fetchStatus(simulateLoadWatts: Int) async throws -> StatusResponse   // NEW, separate method
func fetchPresets() async throws -> [SimulationPreset]
func createPreset(_ draft: SimulationPresetDraft) async throws -> SimulationPreset
func updatePreset(_ preset: SimulationPreset) async throws -> SimulationPreset
func deletePreset(id: String) async throws

// Default so existing conformers (~30 test mocks, widget, settings) need no change:
extension FluxAPIClient {
    func fetchStatus(simulateLoadWatts: Int) async throws -> StatusResponse {
        try await fetchStatus()        // non-simulating fallback
    }
}
// URLSessionAPIClient overrides it to actually send the parameter.

// FluxCore/Simulation/SimulationPresetsService.swift  (@MainActor @Observable)
//   presets: [SimulationPreset]; lastError: Error?
//   refresh()/create()/update()/delete() — server-confirmed-then-apply, mirroring SoCAlertsService
```

The simulation parameter is a **separate** method, not a change to `fetchStatus()`, so the widget timeline and settings-validation call sites are untouched and cannot accidentally simulate (audit rows above). Only `DashboardViewModel` calls the new method, and only while a simulation is active; `URLSessionAPIClient` adds one `URLQueryItem` via the existing `performRequest(path:queryItems:)`. Exactly one status request per refresh cycle ([2.6](requirements.md#2.6)).

### UI

| Element | File (new unless noted) | Baseline to match |
|---|---|---|
| Presets list + error banner | `Settings/Simulation/SimulationPresetsView.swift` | `SoCAlertsView` |
| Editor sheet (label, watts stepper/field, validation) | `Settings/Simulation/SimulationPresetEditor.swift` | `SoCAlertEditor` |
| List view model | `Settings/Simulation/SimulationPresetsViewModel.swift` | `SoCAlertsViewModel` |
| Settings entry | `Settings/SettingsView.swift` (edit) | add `NavigationLink` next to the "Alerts" section, iOS + macOS |
| Dashboard control | `DashboardSimulateMenu` (`Dashboard/SimulationBanner.swift`), injected into `DashboardHeroPanel` | `Menu` listing presets + "Off"; empty → "Add in Settings" deep link ([2.3]) |
| Simulation indicator | `Dashboard/SimulationBanner.swift` | **Deliberately distinct** from the hero `cantEmptyBeforeOffpeak` line |

**Where the control lives ([2.1], [2.3]).** The control sits in the Dashboard's *scroll content*, not the navigation toolbar: on compact iPhone the nav bar is hidden (`DashboardView` line 51, `.toolbar(... .hidden, for: .navigationBar)`), so a toolbar item would be invisible there. It is injected into the **hero panel** rather than the header row (see Decision 15) so the touch target is large and it renders on every layout. Placement:

- **Trigger (inactive):** a `Menu` (`DashboardSimulateMenu`) injected into `DashboardHeroPanel` as a top-trailing accessory beside the SoC numeral, rendered as a labelled "Simulate" pill (SF Symbol + text) styled like the tab-bar pills. Because it lives in the hero panel — which **both** the compact (`dashboardContent`) and regular (`dashboardContentRegular`, iPad regular + macOS) layouts render — it is visible on iPhone, iPad, and macOS. (The earlier design placed it in `headerSection`, which only the compact layout renders, so the regular layout had no trigger — see Decision 15.) The menu lists each preset (label + watts) and an "Off" entry; with no presets it shows a single "Add a preset…" item that opens Settings ▸ Simulation ([2.3]).
- **Active state:** the `SimulationBanner` renders at the top of `dashboardContent`, in the same slot and following the placement of the existing `stalenessBanner` (line 247) — a full-width banner above the panels — but with the distinct simulation treatment below. It shows the active preset + delta and a **Stop** control; the "Simulate" menu remains available to switch presets.

`stalenessBanner` is the *placement/structure* baseline (a Dashboard-level banner at the top of content); the simulation banner's visual treatment is deliberately distinct (next paragraph).

**Indicator treatment ([5.1]–[5.4]).** The existing hero off-peak indicator is calm secondary-text inside the hero subline. The simulation indicator must read as *not real data*, so it is a separate persistent banner above the panels with a distinct accent (a new `FluxTheme.Palette.simulation` accent, not amber/secondary), an SF Symbol, the active preset name and signed delta ("Simulation · Charge car · +1.7 kW"), and a Stop action. The values the simulation changes (Trio "House", hero discharge/empty-by) render in the same simulation accent while active, so the figures alone reveal the simulation ([5.3]). Banner and tinted values carry `.accessibilityLabel`s announcing "simulated", matching how the hero indicator already labels itself ([5.4]). Note the banner's `+1.7 kW` is the preset's *added load* (always positive), which is intentionally a different number from the hero's *net* battery figure (e.g. 1.0 kW charging + a 1.7 kW preset shows as 0.7 kW discharging) — [2.7]'s "agreement" is about the watt value sent matching the displayed status, not the banner delta equalling the hero's net figure.

## Data Models

New DynamoDB table `flux-simulation-presets` (mirrors `flux-pricing`): `BillingMode: PAY_PER_REQUEST`, `DeletionPolicy: Retain`, PITR enabled, key id-only (no partition by device/user):

| | Partition (HASH) | Sort (RANGE) |
|---|---|---|
| `flux-pricing` | `pricingId` | — |
| `flux-simulation-presets` | `presetId` | — |

`presetId` is a server-assigned 128-bit hex id; `createdAt`/`updatedAt` are RFC3339 UTC, `updatedAt` bumped on every PUT. Lambda IAM gains `Scan/PutItem/DeleteItem` on the new table (matching `flux-pricing`'s grant); `TABLE_SIMULATION_PRESETS` env var added. Unlike `flux-pricing`, there is **no** sentinel row — presets are independent.

## Error Handling

| Condition | Behaviour |
|---|---|
| Invalid/over-range `simulateLoadWatts` | 400 `{"error":…}`; client treats as simulation-unavailable, no fabricated values ([4.6]) |
| Live data stale (>90s) or fetch fails while simulating | server omits `live` (existing `liveFresh` gate) / client keeps existing error path → "Awaiting live data"; no simulated values fabricated ([4.5]) |
| Preset CRUD failure / offline | server-confirmed-then-apply: list unchanged, `lastError` surfaced in a banner; editor sheet stays open ([1.6]) — mirrors `SoCAlertsService` |
| Preset cap (20) exceeded | 409; create disabled in editor when at cap |
| Active preset deleted (locally or via sync) | absent from refreshed list → `DashboardViewModel` clears `activeSimulationPresetID`, simulation turns off ([2.4]) |
| Active preset's watts edited on another device | watts re-resolved from the refreshed list each cycle, next request uses the new value ([2.7]) |

## Testing Strategy

**Backend (Go, table-driven, mock reader + fake store — existing style):**
- `compute`/`status`: `W > 0` shifts the live `pbat`/`pload` and the rolling cutoff earlier by the expected amount; off-peak suppression and `liveFresh` still gate the simulated cutoff ([4.1], [4.5]); `CantEmptyBeforeOffpeak` is `nil` whenever `W > 0` ([4.3]); a request with `?simulateLoadWatts=0` (or unparseable / >20000) → 400 ([4.6]).
- Load-allocation waterfall ([3.2]/[3.4]) — assert `Δload == Δbattery + Δgrid` (energy balance) in every case: (a) importing/zero grid below ceiling → battery takes all of `W`, grid unchanged; (b) the 1.7 kW-over-evening-draw case → battery caps at `maxDischargeW`, `pgrid += overflow`, "empty by" reflects the capped rate; (c) charging + exporting (full-sun) → export reduced first, battery charging unchanged, no "empty by"; (d) export partially covers `W` with the remainder hitting the battery.
- Presets handler (fake store, fixed clock + deterministic id): create assigns id+timestamps, list sorted by `createdAt`, PUT bumps `updatedAt`, DELETE idempotent, validation parity (label 1..40, watts 1..20000), cap at 20 → 409. (No partition-isolation test — the table is system-wide like `flux-pricing`, so "list returns all stored presets" is the relevant assertion.)

**Property-based ([4.2], the load-bearing invariant).** Zero-load equivalence is an *internal compute-path* invariant: `status` computed with `W = 0` equals `status` computed with no simulation, field-by-field, for the same readings and clock. It is exercised at the compute layer with `W = 0` — **not** via `?simulateLoadWatts=0`, which is correctly a 400 ([4.6]'s "greater than zero" rule). Use `pgregory.net/rapid` (the repo's standard PBT framework) to generate `soc`/`pbat`/`avgPbat`/`pgrid`/`timestamp` across the boundary conditions the static cases miss — the charging→discharging crossover (`pbat == -W`), `soc == cutoffPercent`, the off-peak equality case (`ct == nextOpWindowStart`), the discharge-ceiling crossover (`pbat + W == maxDischargeW`), and **`pbat`/`avgPbat` already at or above `maxDischargeW`** (the case the headroom form must leave untouched at `W = 0`). Generation ranges must include `pbat > maxDischargeW` so the equivalence isn't accidentally only tested below the ceiling. Two further `rapid` properties: monotonicity, `W↑ ⇒ cutoff no later` (plateauing once the battery saturates, since extra `W` then moves grid, not the battery); and `simDischarge ≥ actual pbat` for all `W ≥ 0` ([3.4] — adding load never lowers the shown discharge).

**Client (Swift):**
- `SimulationPresetsService`: refresh/create/update/delete against a stubbed `FluxAPIClient`; failure sets `lastError` and leaves the list unchanged.
- `DashboardViewModel`: active id resolves to the current preset's watts; deleted active id clears simulation; edited watts flow to the next simulated fetch; widget cache write is skipped while simulating; an immediate fetch fires on activate/switch/stop.
- Widget non-regression: `StatusTimelineLogic` calls `fetchStatus()` (the unsimulated form) — assert the simulation parameter is never sent on the widget path.
- `SimulationPresetDraft.validate()`: boundary cases (empty label, 41 chars, 0 W, 20001 W).

## Decisions Logged
The pricing-precedent choice (Decision 10), the `CantEmptyBeforeOffpeak` suppression (11), the 20-preset cap (12), skipping the widget-cache write while simulating (13), and the inverter-ceiling clamp with grid overflow (14, superseding the no-clamp part of 8) are recorded in `decision_log.md`.
