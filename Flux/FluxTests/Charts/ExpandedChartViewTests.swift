import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor
@Suite
struct ExpandedChartViewTests {
    @Test("History kinds route to the history host")
    func historyKindsRouteToHistory() {
        #expect(ChartKind.historySolar.hostKind == .history)
        #expect(ChartKind.historyGridUsage.hostKind == .history)
        #expect(ChartKind.historyDailyUsage.hostKind == .history)
    }

    @Test("Day kinds route to the day host")
    func dayKindsRouteToDay() {
        #expect(ChartKind.dayPower.hostKind == .day)
        #expect(ChartKind.dayBatteryCombined.hostKind == .day)
    }

    @Test("Every ChartKind maps to exactly one host")
    func everyChartKindHasAHost() {
        for kind in ChartKind.allCases {
            let host = kind.hostKind
            #expect(host == .history || host == .day)
        }
    }

    @Test("Router falls back to today's date for daySpecific scope when registry is empty")
    func dayScopeFallbackUsesToday() {
        let registry = ChartScopeRegistry()
        let resolved = ExpandedChartView.resolvedScope(
            for: .dayPower,
            in: registry,
            today: { Date(timeIntervalSince1970: 1_700_000_000) }
        )
        #expect(resolved == .daySpecific(date: Date(timeIntervalSince1970: 1_700_000_000)))
    }

    @Test("Router falls back to historyRange(.days(7)) for history scope when registry is empty")
    func historyScopeFallbackUsesDefault() {
        let registry = ChartScopeRegistry()
        let resolved = ExpandedChartView.resolvedScope(
            for: .historySolar,
            in: registry,
            today: { Date(timeIntervalSince1970: 1_700_000_000) }
        )
        #expect(resolved == .historyRange(.days(7)))
    }

    @Test("Router reads the registered scope when present")
    func registeredScopeIsUsed() {
        let registry = ChartScopeRegistry()
        let specific = Date(timeIntervalSince1970: 1_710_000_000)
        registry.current[.dayBatteryCombined] = .daySpecific(date: specific)

        let resolved = ExpandedChartView.resolvedScope(
            for: .dayBatteryCombined,
            in: registry,
            today: { Date(timeIntervalSince1970: 1_700_000_000) }
        )

        #expect(resolved == .daySpecific(date: specific))
    }
}
