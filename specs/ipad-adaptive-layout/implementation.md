# Implementation Notes: iPad Adaptive Layout

This document records implementation-time decisions and measurements that
inform the final shipped behaviour.

## Detail-column width measurements (Task 16)

`AdaptiveColumnsLayout` breakpoints currently use the design's hypothesised
values:

| Width tier | Column count |
|---|---|
| `< 700` | 1 |
| `700 ≤ w < 1000` | 2 |
| `≥ 1000` | 3 |

Measurement of actual detail-column widths in `FluxiPadRoot` is **deferred
to the verification phase** (Task 21). The probe recipe below should be
applied if any of the simulator runs in Task 21 produce a layout that
disagrees with these tiers.

### Probe recipe (for use during Task 21 if needed)

Temporarily add inside `FluxiPadRoot`'s `detailContent` body:

```swift
.overlay(alignment: .topLeading) {
    GeometryReader { proxy in
        Color.clear.onAppear {
            print("iPad detail column width: \(proxy.size.width)")
        }
    }
}
```

Run on the simulator matrix in Task 21 and record measured widths in this
file before the probe is removed. Adjust constants in
`Flux/Flux/Helpers/AdaptiveColumnsLayout.swift` if measured widths fall on
the wrong side of 700 / 1000, and re-run `AdaptiveColumnsLayoutTests` with
the updated boundary values.

### Hypothesised widths (from design)

| Device / orientation | Approx detail width | Expected tier |
|---|---|---|
| iPad mini portrait full-screen | ~430–500pt | 1-col |
| iPad mini landscape full-screen | ~770–820pt | 2-col |
| iPad Pro 13" portrait full-screen | ~760–1024pt | 2 or 3-col |
| iPad Pro 13" landscape full-screen | ~1080pt | 3-col |
| iPad ½ Split View (any model) | reports `.regular` but narrower | falls into 1-col tier below 700 |
| iPad Slide Over | reports `.compact` | iPhone shell fallback |

## Task 21 verification log

### Automated tests (2026-05-22)

- `make ios-build` — succeeds.
- `make macos-build` — succeeds.
- `make ios-lint` — no violations in files touched by this spec; pre-existing
  violations remain in unrelated FluxCore and SoCAlerts files.
- `make ios-test` — passes for the four new test suites added by this spec
  (`AdaptiveColumnsLayoutTests`, `ScreenTests`, `SidebarTabSyncTests`,
  `DayDetailViewModelSetDateTests`). Two pre-existing tests are flaky when
  run in parallel mode but pass in isolation:
  - `DashboardViewModelTests.refreshSkipsWhenAlreadyLoading` — timing-based
    concurrency test, fails under load.
  - `CompareControlTests.*` — UIHostingController text-inspection tests,
    fail when the test host has stale state from prior tests in the same
    process. Confirmed passing in isolation (`-only-testing` flag).
- `make macos-test` — same observation: only `refreshSkipsWhenAlreadyLoading`
  is flaky, passes in isolation.

### Manual smoke check (deferred to human verification)

The interactive verification matrix below requires a developer to drive the
simulators (rotate, advance the clock, switch to Slide Over / Split View).
Build and code-level verification has been completed; recording these rows
is the final acceptance step before merging.

| Target | Expected | Result |
|---|---|---|
| iPhone 17 Pro | FluxTabBar visible all three tabs; Settings sheet reachable from each; History → Day Detail push | _to verify_ |
| macOS | Sidebar with Dashboard / Today / History; ⌘, opens Settings scene; ⌘R refresh; ← / → on Day Detail | _to verify_ |
| iPad mini portrait | FluxiPadRoot, sidebar visible, detail column shows Dashboard at single-col (width < 700) | _to verify_ |
| iPad mini landscape | FluxiPadRoot, sidebar visible, detail column shows 2-col Dashboard | _to verify_ |
| iPad Air landscape | FluxiPadRoot, 2-col Dashboard / History card grid / Day Detail two-column | _to verify_ |
| iPad Pro 13" portrait | FluxiPadRoot, 2 or 3-col Dashboard (depending on sidebar state) | _to verify_ |
| iPad Pro 13" landscape | FluxiPadRoot, 3-col Dashboard at detail width ≥ 1000pt | _to verify_ |
| iPad Pro 13" ½ Split View | FluxiPadRoot, detail column narrow → single-col fallback | _to verify_ |
| iPad Slide Over | FluxiOSRoot (tab-bar shell) — `usesPadShell` returns false at compact size class | _to verify_ |
| Today midnight rollover | Advance simulator clock past midnight on Today sidebar entry; date updates and reload fires within 60s | _to verify_ |
| Settings sheet on iPad | Form content capped to 640pt-wide column centred in sheet | _to verify_ |
| Dashboard 10s refresh on iPad | Live values update every 10s while Dashboard is selected | _to verify_ |
| History → Day Detail push on iPad regular | Day Detail pushes onto detail-column NavigationStack | _to verify_ |
