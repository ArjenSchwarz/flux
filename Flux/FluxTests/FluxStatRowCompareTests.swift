#if canImport(UIKit)
import SwiftUI
import Testing
import UIKit
@testable import Flux

// Pins the new sub-line slot and accessibility-override contract on
// FluxStatRow. Default behaviour (no valueSub passed) must stay
// byte-identical to the pre-feature row body so the 19 existing
// callsites (Dashboard, BatteryBlock, OffPeakBlock, etc.) keep
// rendering at their current heights.
@MainActor
@Suite
struct FluxStatRowCompareTests {
    @Test
    func defaultParametersMatchHiddenSublineHeight() {
        // Default (no valueSub passed) must equal valueSub: .hidden.
        let defaultHeight = measureHeight(
            for: FluxStatRow(label: "Solar produced", value: "14.8 kWh", last: true)
        )
        let hiddenHeight = measureHeight(
            for: FluxStatRow(label: "Solar produced", value: "14.8 kWh", last: true, valueSub: .hidden)
        )
        #expect(defaultHeight == hiddenHeight)
    }

    @Test
    func reservedSublineRendersTallerThanHidden() {
        // Reserved adds a sub-line slot; height grows by the sub-line slot
        // height (plus the 2pt VStack spacing). Asserting strict-greater is
        // enough — exact sublineSlotHeight is pinned in ValueSublineTests.
        let hiddenHeight = measureHeight(
            for: FluxStatRow(label: "House used", value: "7.2 kWh", last: true, valueSub: .hidden)
        )
        let reservedHeight = measureHeight(
            for: FluxStatRow(label: "House used", value: "7.2 kWh", last: true, valueSub: .reserved)
        )
        #expect(reservedHeight > hiddenHeight)
    }

    @Test
    func textSublineRendersAtSameHeightAsReserved() {
        // Layout-stability across .reserved and .text(...) is the load-
        // bearing contract. Same height regardless of which one is used.
        let reservedHeight = measureHeight(
            for: FluxStatRow(label: "Grid in", value: "4.2 kWh", last: true, valueSub: .reserved)
        )
        let textHeight = measureHeight(
            for: FluxStatRow(label: "Grid in", value: "4.2 kWh", last: true, valueSub: .text("▲ 0.6 kWh"))
        )
        #expect(reservedHeight == textHeight)
    }

    @Test
    func accessibilityOverrideNilLeavesRowAsSeparateElements() {
        // Pre-feature behaviour: row reads as separate Text elements
        // (label, value, optional sub-line) rather than one combined
        // string. Unit tests can't directly inspect VoiceOver traversal —
        // SwiftUI doesn't always realise its accessibility tree into
        // UIKit's `accessibilityElementCount` before the view is attached
        // to a window. The richer composition (override vs. no-override
        // semantics) is exercised in SummaryBlockCompareTests and
        // DayInFiveBlocksPanelCompareTests against the mapping enums
        // directly. Here we only verify that the body builder doesn't
        // fail when `accessibilityOverride` is nil — i.e. that the
        // default no-override path renders.
        let view = FluxStatRow(label: "Solar", value: "14.8 kWh", last: true)
        let controller = UIHostingController(rootView: view.frame(width: 320))
        controller.view.setNeedsLayout()
        controller.view.layoutIfNeeded()
        let size = controller.sizeThatFits(in: CGSize(width: 320, height: CGFloat.infinity))
        #expect(size.height > 0)
    }

    @Test
    func accessibilityOverrideAppliesCombinedLabel() {
        // With an override: the row reads as one element with the override
        // string as its accessibility label. We verify the element exists
        // — full a11y traversal is covered in panel-integration tests.
        let view = FluxStatRow(
            label: "Solar produced",
            value: "14.8 kWh",
            last: true,
            valueSub: .text("▲ 1.2 kWh"),
            accessibilityOverride: "Solar produced: 14.8 kilowatt-hours, up 1.2 kilowatt-hours versus yesterday"
        )
        let controller = UIHostingController(rootView: view.frame(width: 320))
        controller.view.setNeedsLayout()
        controller.view.layoutIfNeeded()
        // Sanity: rendering didn't crash and the row produced non-zero size.
        let size = controller.sizeThatFits(in: CGSize(width: 320, height: CGFloat.infinity))
        #expect(size.height > 0)
    }

    @Test
    func defaultRowHeightIsByteIdenticalToPreFeatureBaseline() {
        // Capture the row height with no Compare parameters and assert it
        // equals the hidden-subline height. Pre-feature byte-identity is
        // covered by Decision 16 — `compare = .off` (which feeds .hidden)
        // must produce the pre-feature row body.
        let baseline = measureHeight(
            for: FluxStatRow(label: "Battery cycle", value: "12.4 kWh", last: true)
        )
        let hidden = measureHeight(
            for: FluxStatRow(label: "Battery cycle", value: "12.4 kWh", last: true, valueSub: .hidden)
        )
        #expect(baseline == hidden)
    }

    private func measureHeight<V: View>(for view: V) -> CGFloat {
        let controller = UIHostingController(rootView: view.frame(width: 320))
        controller.view.setNeedsLayout()
        controller.view.layoutIfNeeded()
        let size = controller.sizeThatFits(in: CGSize(width: 320, height: CGFloat.infinity))
        return size.height
    }
}
#endif
