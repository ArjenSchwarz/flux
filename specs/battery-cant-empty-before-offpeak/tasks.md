---
references:
    - specs/battery-cant-empty-before-offpeak/requirements.md
    - specs/battery-cant-empty-before-offpeak/design.md
    - specs/battery-cant-empty-before-offpeak/decision_log.md
metadata:
    transit: T-1327
---
# Tasks: Battery Can't Empty Before Off-Peak

## Backend

- [x] 1. Backend (Go) — write helper tests (red) <!-- id:p1ve881 -->
  - Add `internal/api/compute_test.go` table tests (map-based, `t.Run`) per design.md §Testing Strategy.
  - `withinOffpeakWindow`: 5 cases — before window, at start, mid-window, at end (exclusive), after window, plus unparseable strings.
  - `computeCantEmptyBeforeOffpeak`: 10 cases — short window × low soc, long window × low soc, short window × high soc, long window × high soc, soc at cutoff, soc below cutoff, window currently active, no boundary, zero capacity, boundary equality (now + requiredHours == nextOpStart).
  - Tests fail initially because both functions are undefined.
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.8](requirements.md#1.8)

- [x] 2. Backend (Go) — implement helpers (green) <!-- id:p1ve882 -->
  - `internal/api/status.go`: add `maxDischargeKW = 5.0` to the constants block next to `cutoffPercent` and `fallbackCapacityKwh`.
  - `internal/api/compute.go`: add `withinOffpeakWindow(now time.Time, offpeakStart, offpeakEnd string) bool` that delegates HH:MM parsing to `derivedstats.ParseOffpeakWindow` (no new parser).
  - `internal/api/compute.go`: add `cantEmptyInput` struct and `computeCantEmptyBeforeOffpeak(in cantEmptyInput) *bool`. Returns nil when `!HasBoundary`, `WithinOffpeakWindow`, `Soc <= cutoffPercent`, or `CapacityKwh <= 0`.
  - Otherwise computes `requiredHours = (Soc - cutoffPercent)/100 * CapacityKwh / maxDischargeKW` and returns `&true` iff `Now.Add(requiredHours * time.Hour).After(NextOpStart)` (strict comparison — equality returns nil).
  - Blocked-by: p1ve881 (Backend (Go) — write helper tests (red))
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.6](requirements.md#1.6), [1.8](requirements.md#1.8)

- [x] 3. Backend (Go) — handler integration tests (red) <!-- id:p1ve883 -->
  - Extend `internal/api/status_test.go` with cases asserting JSON output of `/status`.
  - (a) `liveFresh` + condition true → `cantEmptyBeforeOffpeak: true`.
  - (b) `liveFresh` + condition false → JSON `null`.
  - (c) `!liveFresh` → `null` regardless.
  - (d) `OFFPEAK_START`/`OFFPEAK_END` empty → `null`.
  - (e) `nowFunc` pinned to `2026-10-04 01:30 Australia/Sydney` (DST start), window 11:00-14:00, Soc 60, capacity 13.34, assert flag tracks `nextOffpeakStart` advancing through the DST gap.
  - (f) System record missing → flag uses `fallbackCapacityKwh = 13.34`.
  - Tests fail because the field is not yet on `BatteryInfo`.
  - Blocked-by: p1ve882 (Backend (Go) — implement helpers (green)), helpers, helpers, helpers, helpers
  - Stream: 1
  - Requirements: [1.3](requirements.md#1.3), [1.7](requirements.md#1.7), [2.4](requirements.md#2.4)

- [x] 4. Backend (Go) — add field + wire handler (green) <!-- id:p1ve884 -->
  - `internal/api/response.go`: add `CantEmptyBeforeOffpeak *bool` JSON-tagged `cantEmptyBeforeOffpeak` to `BatteryInfo` (pointer, no `omitempty`, mirrors `EstimatedCutoff` encoding).
  - `handleStatus` in `internal/api/status.go`: inside the existing `liveFresh` branch, immediately after `battery.EstimatedCutoff` is set, call `withinOffpeakWindow(now, h.offpeakStart, h.offpeakEnd)`, build `cantEmptyInput`, call `computeCantEmptyBeforeOffpeak`, and assign the result to `battery.CantEmptyBeforeOffpeak`.
  - No changes to the `!liveFresh` branch — the field stays nil.
  - Blocked-by: p1ve882 (Backend (Go) — implement helpers (green)), helpers, helpers, helpers, helpers, p1ve883 (Backend (Go) — handler integration tests (red)), handler, handler, handler, handler
  - Stream: 1
  - Requirements: [1.7](requirements.md#1.7), [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3)

## Swift

- [ ] 5. Swift — APIModels decoding tests (red) <!-- id:p1ve885 -->
  - Extend `Flux/Packages/FluxCore/Tests/FluxCoreTests/APIModelsTests.swift` with three decoding cases.
  - JSON containing `"cantEmptyBeforeOffpeak": true` decodes to `.cantEmptyBeforeOffpeak == true`.
  - JSON containing `"cantEmptyBeforeOffpeak": null` decodes to `nil`.
  - JSON omitting the key entirely decodes to `nil` (forward compatibility with older server payloads).
  - Tests fail because the field does not exist on `BatteryInfo` yet.
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2)

- [ ] 6. Swift — add field to BatteryInfo + fixture variants (green) <!-- id:p1ve886 -->
  - `Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift`: add `public let cantEmptyBeforeOffpeak: Bool?` to `BatteryInfo`.
  - Update the memberwise initialiser with `cantEmptyBeforeOffpeak: Bool? = nil` as the last parameter — the default keeps the five existing call sites in design.md §Pattern extension audit compiling unchanged.
  - `Flux/Flux/Services/MockFluxAPIClient.swift`: add a second fixture `statusResponseCantEmpty` with `battery.cantEmptyBeforeOffpeak == true`. The default `statusResponse` keeps it `nil`.
  - Blocked-by: p1ve885 (Swift — APIModels decoding tests (red))
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1)

- [ ] 7. Swift — Dashboard hero accessibility test (red) <!-- id:p1ve887 -->
  - Add a Swift Testing case under `Flux/FluxTests/` that constructs `DashboardHeroPanel(live:, rolling15min:, battery:, offpeakWindowStart:)` with `battery.cantEmptyBeforeOffpeak == true` and `offpeakWindowStart == "23:00"`.
  - Assert the rendered indicator's `accessibilityLabel` is exactly `"Battery won't empty before off-peak at 23:00"`.
  - Test fails because the panel does not yet accept the new inputs.
  - Blocked-by: p1ve886 (Swift — add field to BatteryInfo + fixture variants (green)), fixture, fixture, fixture, fixture
  - Stream: 2
  - Requirements: [3.5](requirements.md#3.5)

- [ ] 8. Swift — implement hero indicator + wire DashboardView (green) <!-- id:p1ve888 -->
  - `Flux/Flux/Dashboard/DashboardHeroPanel.swift`: add inputs `let battery: BatteryInfo?` and `let offpeakWindowStart: String?`.
  - Add a private `cantEmptyBeforeOffpeakIndicator` subview using `FluxTheme.Typography.heroSubline` and `FluxTheme.Palette.secondaryText`. Do NOT reuse `Palette.amber`; do NOT change the `Mode` enum.
  - In `body`, render the indicator when `battery?.cantEmptyBeforeOffpeak == true && offpeakWindowStart != nil`; otherwise render the existing `statusLine` exactly as today.
  - Set the indicator's `accessibilityLabel` to `"Battery won't empty before off-peak at <HH:MM>"` where `<HH:MM>` is the value of `offpeakWindowStart`.
  - `Flux/Flux/Dashboard/DashboardView.swift:113-116`: pass `battery: viewModel.status?.battery` and `offpeakWindowStart: viewModel.status?.offpeak?.windowStart`.
  - Update the `#Preview` block to render both states using the new `MockFluxAPIClient.statusResponseCantEmpty` fixture alongside the default.
  - Blocked-by: p1ve886 (Swift — add field to BatteryInfo + fixture variants (green)), fixture, fixture, fixture, fixture, p1ve887 (Swift — Dashboard hero accessibility test (red))
  - Stream: 2
  - Requirements: [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [3.4](requirements.md#3.4), [3.5](requirements.md#3.5)

## Validation

- [ ] 9. Validate — run lint and full test suites <!-- id:p1ve889 -->
  - Run `make lint`, `go test ./...`, and the Swift test schemes (`make macos-test` and/or `make ios-test` depending on what's defined in the Makefile).
  - Fix any lint/test failures introduced by this branch. No new feature code — only test fixes, lint fixes, and follow-on adjustments.
  - Blocked-by: p1ve884 (Backend (Go) — add field + wire handler (green)), handler, handler, handler, handler, p1ve888 (Swift — implement hero indicator + wire DashboardView (green))
  - Stream: 1
