#if canImport(UIKit)
import SwiftUI
import Testing
import UIKit
@testable import Flux

// Layout-stability is the load-bearing contract: when Compare is on,
// every row reserves sub-line height regardless of whether a delta is
// rendered. ValueSubline drives that — `.reserved` and `.text(...)`
// must produce equal heights, and `.hidden` must collapse to zero.
@MainActor
@Suite
struct ValueSublineTests {
    @Test
    func hiddenContentProducesZeroHeight() {
        let height = measureHeight(for: ValueSubline(content: .hidden))
        #expect(height == 0)
    }

    @Test
    func reservedContentProducesNonZeroHeight() {
        let height = measureHeight(for: ValueSubline(content: .reserved))
        #expect(height > 0)
    }

    @Test
    func textContentProducesNonZeroHeight() {
        let height = measureHeight(for: ValueSubline(content: .text("▲ 1.2 kWh")))
        #expect(height > 0)
    }

    @Test
    func reservedAndTextRenderAtSameHeight() {
        // The whole point of `.reserved` is to hold the slot at the same
        // height as `.text(...)` so a card with mixed-availability rows
        // doesn't jitter when deltas resolve.
        let reservedHeight = measureHeight(for: ValueSubline(content: .reserved))
        let textHeight = measureHeight(for: ValueSubline(content: .text("▲ 1.2 kWh")))
        #expect(reservedHeight == textHeight)
    }

    @Test
    func longerTextRendersAtSameHeightAsReserved() {
        // Width-bounded measurement: as long as the text fits in one line,
        // its height must equal `.reserved`. `frame(width:200)` in the
        // measurement helper matches the value column's worst-case width.
        let reservedHeight = measureHeight(for: ValueSubline(content: .reserved))
        let textHeight = measureHeight(for: ValueSubline(content: .text("▼ 12.3 kWh")))
        #expect(reservedHeight == textHeight)
    }

    @Test
    func hiddenContentProducesShorterHeightThanReserved() {
        // The "Compare off" off-state must collapse the sub-line entirely
        // (Decision 16). This pins that contract regression-style.
        let hiddenHeight = measureHeight(for: ValueSubline(content: .hidden))
        let reservedHeight = measureHeight(for: ValueSubline(content: .reserved))
        #expect(hiddenHeight < reservedHeight)
    }

    private func measureHeight<V: View>(for view: V) -> CGFloat {
        // Constrain to a realistic value-column width so single-line text
        // doesn't wrap. Height is what we're asserting on.
        let controller = UIHostingController(rootView: view.frame(width: 200))
        controller.view.setNeedsLayout()
        controller.view.layoutIfNeeded()
        let size = controller.sizeThatFits(in: CGSize(width: 200, height: CGFloat.infinity))
        return size.height
    }
}
#endif
