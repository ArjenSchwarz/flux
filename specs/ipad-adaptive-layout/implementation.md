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

To be filled in during Task 21.
