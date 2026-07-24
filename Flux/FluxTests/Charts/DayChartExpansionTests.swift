import FluxCore
import Foundation
import SwiftUI
import Testing
@testable import Flux

@MainActor
@Suite
struct DayChartExpansionTests {
    @Test("Day Detail charts declare distinct ChartKinds matching their identity")
    func dayChartsDeclareDistinctChartKinds() {
        #expect(PowerChartView.chartKind == .dayPower)
        #expect(BatteryCombinedChartView.chartKind == .dayBatteryCombined)

        let kinds: Set<ChartKind> = [PowerChartView.chartKind, BatteryCombinedChartView.chartKind]
        #expect(kinds.count == 2)
    }

    @Test("PowerChartView.expansionScope is daySpecific built from the date string")
    func powerExpansionScopeParsesDate() {
        let dateString = "2026-05-12"
        guard let expected = DateFormatting.parseDayDate(dateString) else {
            Issue.record("Failed to parse fixture date string")
            return
        }
        let view = PowerChartView(date: dateString, readings: [], offpeakWindow: nil, selectedDate: .constant(nil))
        #expect(view.expansionScope == .daySpecific(date: expected))
    }

    @Test("BatteryCombinedChartView.expansionScope is daySpecific built from the date string")
    func batteryExpansionScopeParsesDate() {
        let dateString = "2026-05-12"
        guard let expected = DateFormatting.parseDayDate(dateString) else {
            Issue.record("Failed to parse fixture date string")
            return
        }
        let view = BatteryCombinedChartView(
            date: dateString,
            readings: [],
            summary: nil,
            offpeakWindow: nil,
            selectedDate: .constant(nil)
        )
        #expect(view.expansionScope == .daySpecific(date: expected))
    }

    @Test("Different date strings produce distinct expansion scopes")
    func differentDatesProduceDistinctScopes() {
        let first = PowerChartView(date: "2026-05-01", readings: [], offpeakWindow: nil, selectedDate: .constant(nil))
        let second = PowerChartView(date: "2026-05-02", readings: [], offpeakWindow: nil, selectedDate: .constant(nil))
        #expect(first.expansionScope != second.expansionScope)
    }
}
