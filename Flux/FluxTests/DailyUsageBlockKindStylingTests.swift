import FluxCore
import SwiftUI
import Testing
@testable import Flux

@Suite
struct DailyUsageBlockKindStylingTests {
    @Test
    func chronologicalOrderIsNightToEvening() {
        let order = DailyUsageBlock.Kind.chronologicalOrder
        #expect(order == [.night, .morningPeak, .offPeak, .afternoonPeak, .evening])
    }

    /// Guards against a future `Kind` case being added to `FluxCore` without
    /// being added to `chronologicalOrder` — `chronologicalIndex` would crash
    /// at render time. This test fails at build/test time instead.
    @Test
    func chronologicalOrderCoversAllKinds() {
        #expect(DailyUsageBlock.Kind.chronologicalOrder.count == DailyUsageBlock.Kind.allCases.count)
        for kind in DailyUsageBlock.Kind.allCases {
            #expect(DailyUsageBlock.Kind.chronologicalOrder.contains(kind), "missing \(kind)")
        }
    }

    @Test
    func chronologicalIndexMatchesOrder() {
        #expect(DailyUsageBlock.Kind.night.chronologicalIndex == 0)
        #expect(DailyUsageBlock.Kind.morningPeak.chronologicalIndex == 1)
        #expect(DailyUsageBlock.Kind.offPeak.chronologicalIndex == 2)
        #expect(DailyUsageBlock.Kind.afternoonPeak.chronologicalIndex == 3)
        #expect(DailyUsageBlock.Kind.evening.chronologicalIndex == 4)
    }

    @Test
    func chartColorMapsPerDecisionFive() {
        #expect(DailyUsageBlock.Kind.night.chartColor == Color.indigo)
        #expect(DailyUsageBlock.Kind.morningPeak.chartColor == Color.orange)
        #expect(DailyUsageBlock.Kind.offPeak.chartColor == Color.teal)
        #expect(DailyUsageBlock.Kind.afternoonPeak.chartColor == Color.red)
        #expect(DailyUsageBlock.Kind.evening.chartColor == Color.purple)
    }

    @Test
    func displayLabelMatchesDayDetailStrings() {
        #expect(DailyUsageBlock.Kind.night.displayLabel == "Night")
        #expect(DailyUsageBlock.Kind.morningPeak.displayLabel == "Morning Peak")
        #expect(DailyUsageBlock.Kind.offPeak.displayLabel == "Off-Peak")
        #expect(DailyUsageBlock.Kind.afternoonPeak.displayLabel == "Afternoon Peak")
        #expect(DailyUsageBlock.Kind.evening.displayLabel == "Evening")
    }
}
