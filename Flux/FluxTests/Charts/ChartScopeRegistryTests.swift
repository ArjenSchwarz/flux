import Foundation
import Testing
@testable import Flux

@MainActor
@Suite
struct ChartScopeRegistryTests {
    @Test("Writing a scope is readable by the same kind")
    func writeThenReadByKind() {
        let registry = ChartScopeRegistry()
        let scope = ChartScope.historyRange(days: 7)

        registry.current[.historySolar] = scope

        #expect(registry.current[.historySolar] == scope)
    }

    @Test("Writing the same kind twice overwrites the previous value")
    func overwriteSameKind() {
        let registry = ChartScopeRegistry()
        registry.current[.dayPower] = .daySpecific(date: Date(timeIntervalSince1970: 1_700_000_000))

        let newScope = ChartScope.daySpecific(date: Date(timeIntervalSince1970: 1_800_000_000))
        registry.current[.dayPower] = newScope

        #expect(registry.current[.dayPower] == newScope)
        #expect(registry.current.count == 1)
    }

    @Test("Different kinds are stored independently")
    func differentKindsAreIndependent() {
        let registry = ChartScopeRegistry()
        let historyScope = ChartScope.historyRange(days: 14)
        let dayScope = ChartScope.daySpecific(date: Date(timeIntervalSince1970: 1_700_000_000))

        registry.current[.historyGridUsage] = historyScope
        registry.current[.dayBatteryCombined] = dayScope

        #expect(registry.current[.historyGridUsage] == historyScope)
        #expect(registry.current[.dayBatteryCombined] == dayScope)
        #expect(registry.current[.historySolar] == nil)
    }

    @Test("Registry starts empty")
    func startsEmpty() {
        let registry = ChartScopeRegistry()
        #expect(registry.current.isEmpty)
    }
}
