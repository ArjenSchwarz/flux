#if canImport(UIKit) && !os(macOS)
import Foundation
import Testing
@testable import Flux

@MainActor
@Suite
struct ExpandedChartTopHandleTests {
    @Test("Drag-indicator band is 32 pt tall and dismiss threshold is 60 pt")
    func contractConstants() {
        #expect(ExpandedChartTopHandle.bandHeight == 32)
        #expect(ExpandedChartTopHandle.dismissThreshold == 60)
    }

    @Test("A downward drag past 60 pt resolves to dismiss")
    func downwardDragPastThresholdDismisses() {
        let resolution = ExpandedChartTopHandle.resolve(translation: CGSize(width: 0, height: 70))
        #expect(resolution == .dismiss)
    }

    @Test("A downward drag at exactly 60 pt resolves to dismiss")
    func downwardDragAtThresholdDismisses() {
        let resolution = ExpandedChartTopHandle.resolve(translation: CGSize(width: 0, height: 60))
        #expect(resolution == .dismiss)
    }

    @Test("A downward drag below the threshold does not dismiss")
    func downwardDragBelowThresholdIsInert() {
        let resolution = ExpandedChartTopHandle.resolve(translation: CGSize(width: 0, height: 59))
        #expect(resolution == .none)
    }

    @Test("An upward drag does not dismiss")
    func upwardDragIsInert() {
        let resolution = ExpandedChartTopHandle.resolve(translation: CGSize(width: 0, height: -120))
        #expect(resolution == .none)
    }

    @Test("A purely horizontal drag is inert")
    func horizontalDragIsInert() {
        let resolution = ExpandedChartTopHandle.resolve(translation: CGSize(width: 200, height: 0))
        #expect(resolution == .none)
    }

    @Test("A mostly horizontal drag with mild downward component is inert")
    func mostlyHorizontalDragIsInert() {
        let resolution = ExpandedChartTopHandle.resolve(translation: CGSize(width: 120, height: 80))
        #expect(resolution == .none)
    }

    @Test("A diagonal drag that is more vertical than horizontal and past threshold dismisses")
    func mostlyVerticalDiagonalPastThresholdDismisses() {
        let resolution = ExpandedChartTopHandle.resolve(translation: CGSize(width: 30, height: 80))
        #expect(resolution == .dismiss)
    }
}
#endif
