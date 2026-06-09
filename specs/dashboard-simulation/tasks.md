---
references:
    - specs/dashboard-simulation/requirements.md
    - specs/dashboard-simulation/design.md
    - specs/dashboard-simulation/decision_log.md
---
# Dashboard Simulation

## Backend

- [x] 1. Write Go unit tests for simulated /status (RED) <!-- id:0plbrzn -->
  - internal/api/status_test.go + compute_test.go; existing table-driven + mockReader style, h.nowFunc clock.
  - Load-allocation waterfall (reduce export -> battery capped at 5 kW -> grid import): assert delta load = delta battery + delta grid (energy balance) in every case, and that simDischarge is never below the real pbat.
  - Cases: (a) importing/zero grid below ceiling -> battery takes all W, grid unchanged; (b) 1.7 kW over a ~4 kW evening draw -> battery caps at maxDischargeW, grid import = overflow, empty-by from the capped rate; (c) charging + exporting (full sun) -> export cut first, battery charging unchanged, no empty-by; (d) export partially covers W -> remainder hits the battery; (e) real pbat already at/above the ceiling -> battery left at the real value, all of wBattery to grid.
  - Off-peak suppression (ct.Before(nextOpWindowStart)) and the 90s liveFresh gate still gate the simulated cutoff; CantEmptyBeforeOffpeak is nil whenever W>0.
  - Request with simulateLoadWatts=0, unparseable, or >20000 returns 400.
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.4](requirements.md#3.4), [4.1](requirements.md#4.1), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.6](requirements.md#4.6)

- [x] 2. Write rapid property tests for the simulated compute path (RED) <!-- id:0plbrzo -->
  - Use pgregory.net/rapid.
  - Property A: zero-load equivalence at the compute layer (W=0 equals no-simulation, field-by-field, same readings+clock); exercised internally, NOT via simulateLoadWatts=0.
  - Property B: monotonicity (larger W gives a cutoff no later), plateauing once discharge saturates at the ceiling. Property C: simDischarge >= actual pbat for all W>=0 (adding load never lowers shown discharge).
  - Generate soc/pbat/avgPbat/pgrid/timestamp across boundaries: pbat == -W crossover, soc == cutoffPercent, ct == nextOpWindowStart, pbat+W == maxDischargeW (ceiling crossover), and pbat/avgPbat already >= maxDischargeW (the headroom form must leave these untouched at W=0).
  - Stream: 1
  - Requirements: [4.2](requirements.md#4.2)

- [x] 3. Implement simulateLoadWatts on handleStatus (GREEN) <!-- id:0plbrzp -->
  - Parse+validate in internal/api/status.go (parseSimulateLoad: empty allowed; else 0<v<=20000 or 400).
  - Allocate W by waterfall (sign: pbat>0 discharge, pgrid<0 export): exportReduction = min(W, max(0,-pgrid)); wBattery = W - exportReduction; headroom(p)=max(0, maxDischargeW-p); batteryAbsorbed(p)=min(wBattery, headroom(p)); simDischarge(p)=p+batteryAbsorbed(p) (never below p); overflow = wBattery - batteryAbsorbed(latest.Pbat).
  - Apply: Pload+W; live Pbat = simDischarge(latest.Pbat); both computeCutoffTime inputs and the returned rolling AvgPbat use simDischarge with per-series headroom; AvgLoad+W; Pgrid = pgrid + exportReduction + overflow. Reuse the existing maxDischargeKW constant.
  - Force battery.CantEmptyBeforeOffpeak=nil when W>0; W=0 path is a true no-op (headroom form does not clamp a real reading already at/above the ceiling); a small allocation helper, no new cutoff function.
  - Blocked-by: 0plbrzn (Write Go unit tests for simulated /status (RED)), 0plbrzo (Write rapid property tests for the simulated compute path (RED)), compute, compute, compute, compute, compute, compute, compute, compute, compute
  - Stream: 1
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [4.1](requirements.md#4.1), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4), [4.6](requirements.md#4.6)

- [x] 4. Write Go tests for simulation-presets store + handler (RED) <!-- id:0plbrzq -->
  - New internal/api/simulationpresets_test.go with a fake store (mirror pricing_test.go / socrules_test.go style, fixed clock + deterministic id).
  - Create assigns id+createdAt/updatedAt; list returns all stored presets; PUT bumps updatedAt; DELETE idempotent.
  - Validation (label 1..40 chars, watts 1..20000) returns 400 with a reason; cap 20 returns 409.
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.6](requirements.md#1.6)

- [x] 5. Implement simulation-presets CRUD (GREEN) <!-- id:0plbrzr -->
  - internal/dynamo/simulationpresets.go: SimulationPresetItem keyed by presetId only (no partition); Store with ListPresets(ctx) Scan / PutPreset / DeletePreset, mirroring DynamoPricingStore minus sentinel/transactional bits.
  - internal/api/simulationpresets.go + _handler.go: payload+validate, handlers, error-JSON bodies, 200/201/204/400/409.
  - Register GET/POST/PUT/DELETE /simulation-presets in internal/api/handler.go; wire the store in cmd/api/main.go (TABLE_SIMULATION_PRESETS).
  - Blocked-by: 0plbrzq (Write Go tests for simulation-presets store + handler (RED)), presets, handler, presets, handler, presets, handler, presets, handler, presets, handler, presets, handler, presets, handler, presets, handler, presets, handler
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.6](requirements.md#1.6)

- [x] 6. Add infrastructure for the presets table (config) <!-- id:0plbrzs -->
  - infrastructure/template.yaml: add SimulationPresetsTable (flux-simulation-presets, key presetId HASH only, PAY_PER_REQUEST, DeletionPolicy/UpdateReplacePolicy Retain, PITR on), copied from PricingTable.
  - Add Scan/PutItem/DeleteItem IAM on the Lambda role for the new table; add TABLE_SIMULATION_PRESETS env var to ApiFunction.
  - Blocked-by: 0plbrzr (Implement simulation-presets CRUD (GREEN)), presets, presets, presets, presets, presets, presets, presets, presets, presets
  - Stream: 1
  - Requirements: [1.4](requirements.md#1.4)

## Client

- [ ] 7. Write tests for SimulationPresetDraft.validate() (RED) <!-- id:0plbrzt -->
  - FluxCore tests; boundary cases: empty label, 41-char label, 0 W, 20001 W, and a valid case.
  - Stream: 2
  - Requirements: [1.3](requirements.md#1.3)

- [ ] 8. Implement SimulationPreset model + draft (GREEN) <!-- id:0plbrzu -->
  - FluxCore/Models/SimulationPreset.swift: Identifiable/Codable/Sendable/Equatable (id,label,watts,createdAt,updatedAt).
  - SimulationPresetDraft: label empty default, watts=0 so it starts invalid; validate() -> ValidationError emptyLabel/labelTooLong(40)/wattsOutOfRange(1...20000).
  - Blocked-by: 0plbrzt (Write tests for SimulationPresetDraft.validate() (RED))
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.3](requirements.md#1.3)

- [ ] 9. Write tests for API client preset CRUD + simulated status request (RED) <!-- id:0plbrzv -->
  - FluxCore URLSessionAPIClient tests.
  - Assert path/method/body for fetchPresets/createPreset/updatePreset/deletePreset on /simulation-presets, and that fetchStatus(simulateLoadWatts:) adds the query item while the existing fetchStatus() stays param-free.
  - Blocked-by: 0plbrzu (Implement SimulationPreset model + draft (GREEN))
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [3.1](requirements.md#3.1)

- [ ] 10. Implement FluxAPIClient additions (GREEN) <!-- id:0plbrzw -->
  - Add protocol methods fetchPresets/createPreset/updatePreset/deletePreset and fetchStatus(simulateLoadWatts: Int).
  - Add a default extension fetchStatus(simulateLoadWatts:) delegating to fetchStatus() so the widget, settings, and ~30 test mocks compile unchanged.
  - URLSessionAPIClient overrides it (one URLQueryItem via performRequest) and implements the preset CRUD.
  - Blocked-by: 0plbrzv (Write tests for API client preset CRUD + simulated status request (RED)), request, request, request, request, request, request, request, request, request
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [3.1](requirements.md#3.1), [3.3](requirements.md#3.3)

- [ ] 11. Write tests for SimulationPresetsService (RED) <!-- id:0plbrzx -->
  - FluxCore; refresh/create/update/delete against a stubbed FluxAPIClient.
  - Server-confirmed-then-apply; on failure lastError is set and the list is unchanged (mirror SoCAlertsServiceTests).
  - Blocked-by: 0plbrzu (Implement SimulationPreset model + draft (GREEN))
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.6](requirements.md#1.6)

- [ ] 12. Implement SimulationPresetsService (GREEN) <!-- id:0plbrzy -->
  - FluxCore/Simulation/SimulationPresetsService.swift: @MainActor @Observable; presets, lastError; bind(apiClient:); refresh/create/update/delete; mirroring SoCAlertsService.
  - Blocked-by: 0plbrzw (Implement FluxAPIClient additions (GREEN)), 0plbrzx (Write tests for SimulationPresetsService (RED))
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.6](requirements.md#1.6)

- [ ] 13. Write tests for SimulationPresetsViewModel (RED) <!-- id:0plbrzz -->
  - canSave reflects draft.validate(); the add affordance is disabled at the 20 cap; save() handles create vs edit; service errors are surfaced.
  - Blocked-by: 0plbrzu (Implement SimulationPreset model + draft (GREEN))
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.6](requirements.md#1.6)

- [ ] 14. Implement Settings Simulation list + editor (GREEN) <!-- id:0plbs00 -->
  - Settings/Simulation/SimulationPresetsView.swift + SimulationPresetEditor.swift + SimulationPresetsViewModel.swift (mirror SoCAlerts views/editor/vm, error banner, cap 20).
  - Add a NavigationLink in Settings/SettingsView.swift near the Alerts section for iOS and macOS.
  - Blocked-by: 0plbrzy (Implement SimulationPresetsService (GREEN)), 0plbrzz (Write tests for SimulationPresetsViewModel (RED))
  - Stream: 2
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6)

- [ ] 15. Write tests for DashboardViewModel simulation logic (RED) <!-- id:0plbs01 -->
  - Flux/FluxTests: activeSimulationPresetID single (replace on switch); watts resolved from current presets each refresh; deleted/absent active id clears the simulation.
  - Edited watts flow to the next simulated fetch; one status request per cycle; immediate fetch on activate/switch/stop.
  - Widget-cache write skipped while simulating; widget non-regression (StatusTimelineLogic stays on fetchStatus()); banner presentation values (preset name, +delta).
  - Blocked-by: 0plbrzu (Implement SimulationPreset model + draft (GREEN))
  - Stream: 2
  - Requirements: [2.2](requirements.md#2.2), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7), [4.5](requirements.md#4.5), [5.5](requirements.md#5.5)

- [ ] 16. Implement DashboardViewModel simulation state + fetch wiring (GREEN) <!-- id:0plbs02 -->
  - Flux/Flux/Dashboard/DashboardViewModel.swift: add activeSimulationPresetID (in-memory, nil on cold launch), resolve watts from SimulationPresetsService each refresh.
  - Call fetchStatus(simulateLoadWatts:) when active else fetchStatus(); immediate refresh() on toggle change.
  - Skip widgetCache.writeIfNewer + reload trigger while simulating; expose isSimulating + active preset name/delta.
  - Blocked-by: 0plbrzw (Implement FluxAPIClient additions (GREEN)), 0plbrzy (Implement SimulationPresetsService (GREEN)), 0plbs01 (Write tests for DashboardViewModel simulation logic (RED))
  - Stream: 2
  - Requirements: [2.2](requirements.md#2.2), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [2.7](requirements.md#2.7), [4.5](requirements.md#4.5), [5.5](requirements.md#5.5)

- [ ] 17. Implement Dashboard simulation UI (view wiring) <!-- id:0plbs03 -->
  - Simulate menu in DashboardView headerSection (lists presets + Off; empty shows Add a preset deep-linking to Settings Simulation).
  - New Dashboard/SimulationBanner.swift at the top of dashboardContent (stalenessBanner placement baseline) with a distinct FluxTheme.Palette.simulation accent, preset+delta, and a Stop control.
  - Tint the simulated values (trio House, hero discharge/empty-by) in the accent while active; accessibility labels announce simulated; iOS + macOS.
  - Blocked-by: 0plbs00 (Implement Settings Simulation list + editor (GREEN)), 0plbs02 (Implement DashboardViewModel simulation state + fetch wiring (GREEN))
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.3](requirements.md#2.3), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5)

## Verification

- [ ] 18. Run full Go + iOS + macOS test/lint and fix issues <!-- id:0plbs04 -->
  - Run the Makefile targets for Go tests and ios/macos build+test+lint.
  - Fix any build/lint/platform-parity breakages; confirm the feature compiles and tests pass on both iOS and macOS.
  - Blocked-by: 0plbrzs (Add infrastructure for the presets table (config)), presets, presets, presets, presets, presets, presets, presets, presets, presets, 0plbs03 (Implement Dashboard simulation UI (view wiring))
  - Stream: 1
  - Requirements: [1.5](requirements.md#1.5), [2.1](requirements.md#2.1)
