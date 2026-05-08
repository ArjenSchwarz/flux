# Battery Changes — Implementation Notes

Two small changes shipped together on the `battery-changes` branch:

1. The AlphaESS battery's hardware-side minimum discharge setting was lowered from 10% to 5%. Backend, charts, and product spec now use 5%.
2. **T-1163** — A new "Energy left" row on the Dashboard's `BatteryBlock`, showing usable kWh remaining at the current SOC.

No formal spec was written ahead of time (per discussion with the user — both are small enough). This document is the after-the-fact summary.

## Files touched

| File | Change |
| --- | --- |
| `internal/api/status.go` | `cutoffPercent` const 10 → 5 |
| `internal/api/status_test.go` | Assertion updated |
| `Flux/Packages/FluxCore/Sources/FluxCore/Helpers/BatteryEnergy.swift` | **New.** `BatteryEnergy.cutoffPercent` constant + `usableKwh(soc:capacityKwh:cutoffPercent:)` helper |
| `Flux/Flux/DayDetail/SOCChartView.swift` | Dashed cutoff `RuleMark` reads `BatteryEnergy.cutoffPercent` (was hardcoded `10`) |
| `Flux/Flux/DayDetail/BatteryCombinedChartView.swift` | Same |
| `Flux/Flux/Helpers/BatteryBlock.swift` | New optional `energyLeftKwh: Double?` input. When non-nil, renders an "Energy left" stat row at the top of the panel |
| `Flux/Flux/Dashboard/DashboardView.swift` | Computes `energyLeftKwh` via `BatteryEnergy.usableKwh(...)` from the live SOC, capacity, and `cutoffPercent` returned by `/status`; passes it to `BatteryBlock` |
| `docs/flux-v1.md` | Example response, prose, and chart description updated to "5%" |
| `CHANGELOG.md` | Unreleased entry under Changed (cutoff) and Added (Energy left) |

## Explained at three levels

### Beginner — what does the user see, and why

Open the Flux app. On the Dashboard, the panel that already showed today's battery cycle and the lowest SOC reached now has one extra row at the top:

```
Energy left      6.33 kWh
Battery cycle    4.20 / 3.80 kWh
Lowest           42% · SOC at 03:14
Charged during off-peak  +28%
```

That number tells you how much energy your battery can actually deliver right now before the AlphaESS controller stops discharging. It's a more practical answer than the SOC percentage on its own — "I have 47% of my battery left" doesn't tell you how much electricity that is in kWh, and at 5% SOC the battery effectively has 0 kWh left even though it isn't literally empty.

Separately, on the Day Detail screen, the dashed red line across the SOC chart sits a little lower than before — at 5% instead of 10%. That's the level at which the battery stops discharging. The hardware setting was changed; the chart now matches.

### Intermediate — what's the design

The dashboard's `/status` Lambda returns `battery.capacityKwh` (the AlphaESS-reported pack capacity, falling back to 13.34 kWh when the system record is missing) and `battery.cutoffPercent` (the SOC floor — now 5). Live SOC comes from the most recent reading.

Usable energy is a simple linear extrapolation of those three values, clamped at zero so a fresh post-cutoff dip never reads negative:

```swift
max(0, (soc - cutoffPercent) / 100 * capacityKwh)
```

This is the same arithmetic the Go backend uses inside `computeCutoffTime` to estimate when the battery will run out — picking the same formula keeps the iOS "Energy left" figure consistent with the existing "empty by HH:MM" subline on the hero panel. They reach zero / the named clock time at the same moment.

The formula and the cutoff value live in `FluxCore.BatteryEnergy` so:

- `DashboardView` computes the kWh once per render and hands it to `BatteryBlock` as a single optional `Double?`.
- The two Day Detail charts (`SOCChartView`, `BatteryCombinedChartView`) read `BatteryEnergy.cutoffPercent` for their dashed reference line instead of hardcoding the integer.

`BatteryBlock` itself stays presentational. When `energyLeftKwh` is nil (Day Detail and History — neither has live SOC), the row simply isn't rendered, so existing day-summary uses are untouched.

### Expert — what's worth knowing and what isn't

**Architectural constraint that drove the shape:** the iOS `BatteryInfo` model carries `cutoffPercent` (Int) on `/status`, but `/day` (Day Detail) returns no `BatteryInfo`. So a chart like `SOCChartView` cannot get the cutoff from its own response payload. We had a hardcoded `10` in two views before this change; the agreed fix was to keep the chart-side reference value as a Swift constant, since the value is genuinely fixed for the foreseeable future (it tracks an inverter setting on a single piece of hardware). `BatteryEnergy.cutoffPercent` is that constant. The Dashboard, which does have live `BatteryInfo`, uses the API value rather than the constant — that way an out-of-band server-side change (unlikely but possible) flows through to the live row without an iOS rebuild.

**Why the Go const isn't promoted to SSM.** Same reasoning. It mirrors a hardware setting, and the AlphaESS API doesn't expose it, so an SSM parameter would be a pseudo-config that just adds a place to forget to update. A grep'able constant with a comment is more honest.

**Distributed-constant smell.** `internal/api/status.go` (Go) and `BatteryEnergy.swift` (Swift) both hold `5`. They cannot share a literal. The two-line comment on the Swift constant points at the Go file as the canonical site; the convention is "if you change one, change the other." This is acceptable because (a) the value changes rarely and only for hardware reasons, (b) the assertion in `internal/api/status_test.go` is keyed off the Go const, and (c) the iOS test fixtures using the old `10` (`StatusTimelineLogic.placeholderEnvelope`, `WidgetFixtures`, etc.) are widget skeletons / test inputs and don't need to track the production value.

**Parameter sprawl, considered and rejected.** The first cut of `BatteryBlock` took three optional inputs (`currentSOC`, `capacityKwh`, `cutoffPercent`) and computed `energyLeftKwh` inside the view. A pre-push review pointed out that these three are all-or-nothing — a classic "introduce parameter object" smell. Two fixes were on the table: wrap in a `LiveBatteryState` struct, or precompute the kWh in the caller and pass a single `Double?`. The second is what shipped. It moves the (trivial) domain math to where the view-model lives, keeps `BatteryBlock` presentational, and centralises the formula in `BatteryEnergy.usableKwh`, which any future caller can reach for.

**Performance.** Negligible. The dashboard re-renders on a 10-second cadence (60 s when inactive). `usableKwh` is three operations on `Double`s; `BatteryEnergy.cutoffPercent` is a static let read. The chart's `RuleMark` runs once per chart refresh, identical to before.

**What this didn't change.** The `BatteryColor.forSOC` thresholds (15 / 30) are colour-tier breakpoints, not the cutoff — left alone. `RelevanceScoringTests.swift` and the widget timeline placeholder pass `cutoffPercent: 10` as test input to verify scoring relative to that value; they keep working with their stated input and were not migrated to the new value.

## Completeness assessment

- **Fully implemented**
  - Backend cutoff value — single change, asserted in `TestHandleStatusAllDataPresent`.
  - "Energy left" row on Dashboard — visible whenever `viewModel.status?.live?.soc` and `viewModel.status?.battery` are both non-nil; hidden otherwise.
  - Cutoff reference line on Day Detail SOC and combined battery charts — pulls from `BatteryEnergy.cutoffPercent`.
  - V1 product spec updated to reflect the new threshold.
  - CHANGELOG entries (Changed for cutoff, Added for energy-left).
- **Partially / not implemented (and intentionally so)**
  - No new unit tests added for `BatteryEnergy.usableKwh`. The formula is a one-liner, the inputs are typed, and the clamp is the only branch — adding a test was judged disproportionate for the size of the change.
  - The `/day` response still does not include `BatteryInfo`. The Day Detail charts therefore can't show a per-day cutoff (which is fine — the cutoff doesn't change per-day). Documented above so a future change considering a per-day cutoff knows where to start.
  - Mock and widget-placeholder fixtures still reference `cutoffPercent: 10`. They're inputs to scoring or rendering tests, not assertions about production behaviour — left as-is.
- **Missing**
  - None for the agreed scope.

## Decisions

These are recorded narratively above (under Expert) rather than in a separate decision log, since this branch wasn't started from a formal spec. If it grows into one, fold the "why constant not SSM" and "parameter object rejected" notes into a `decision_log.md`.
