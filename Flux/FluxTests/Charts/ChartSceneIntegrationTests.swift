#if os(macOS)
import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor
@Suite
struct ChartSceneIntegrationTests {
    @Test("Same kind twice keeps a single registry entry (one window guarantee)")
    func sameKindTwiceKeepsOneEntry() {
        let registry = ChartScopeRegistry()
        var openCalls: [ChartKind] = []
        let action = ChartExpansionAction { kind, scope in
            registry.current[kind] = scope
            openCalls.append(kind)
        }

        action(.dayPower, scope: .daySpecific(date: Date(timeIntervalSince1970: 1_714_521_600)))
        action(.dayPower, scope: .daySpecific(date: Date(timeIntervalSince1970: 1_714_608_000)))

        #expect(registry.current.count == 1)
        #expect(registry.current[.dayPower] == .daySpecific(date: Date(timeIntervalSince1970: 1_714_608_000)))
        #expect(openCalls == [.dayPower, .dayPower])
    }

    @Test("Different kinds open separate entries")
    func differentKindsOpenSeparateEntries() {
        let registry = ChartScopeRegistry()
        let action = ChartExpansionAction { kind, scope in
            registry.current[kind] = scope
        }

        action(.dayPower, scope: .daySpecific(date: Date(timeIntervalSince1970: 1_714_521_600)))
        action(.historySolar, scope: .historyRange(.days(7)))

        #expect(registry.current.count == 2)
        #expect(registry.current[.dayPower] == .daySpecific(date: Date(timeIntervalSince1970: 1_714_521_600)))
        #expect(registry.current[.historySolar] == .historyRange(.days(7)))
    }

    @Test("Relaunch: a fresh registry is empty (no persistence)")
    func relaunchYieldsEmptyRegistry() {
        let firstLaunch = ChartScopeRegistry()
        firstLaunch.current[.dayPower] = .daySpecific(date: Date(timeIntervalSince1970: 1_714_521_600))
        #expect(firstLaunch.current.count == 1)

        let secondLaunch = ChartScopeRegistry()
        #expect(secondLaunch.current.isEmpty)
    }

    @Test("Resolved scope falls back when the registry has no entry for the kind")
    func resolvedScopeFallsBackWithoutEntry() {
        let registry = ChartScopeRegistry()
        let today = Date(timeIntervalSince1970: 1_700_000_000)

        let solar = ExpandedChartView.resolvedScope(for: .historySolar, in: registry, today: { today })
        let dayPower = ExpandedChartView.resolvedScope(for: .dayPower, in: registry, today: { today })

        if case let .historyRange(query) = solar {
            #expect(query == .days(ExpandedChartView.defaultHistoryRangeDays))
        } else {
            Issue.record("Expected historyRange fallback for historySolar")
        }
        #expect(dayPower == .daySpecific(date: today))
    }

    @Test("Resolved scope returns the registered value when present")
    func resolvedScopeUsesRegistryEntry() {
        let registry = ChartScopeRegistry()
        let scope = ChartScope.daySpecific(date: Date(timeIntervalSince1970: 1_714_608_000))
        registry.current[.dayPower] = scope

        let resolved = ExpandedChartView.resolvedScope(for: .dayPower, in: registry)

        #expect(resolved == scope)
    }

    @Test("ChartDetailScene exposes a stable window identifier and the documented min size")
    func chartDetailSceneConstants() {
        #expect(ChartDetailScene.id == "chart-detail")
        #expect(ChartDetailScene.minWidth == 720)
        #expect(ChartDetailScene.minHeight == 480)
    }

    @Test("Re-expand updates the scope so the observer adopts the new context")
    func reExpandUpdatesObserverScope() {
        let registry = ChartScopeRegistry()
        let action = ChartExpansionAction { kind, scope in
            registry.current[kind] = scope
        }

        let firstScope = ChartScope.daySpecific(date: Date(timeIntervalSince1970: 1_714_521_600))
        action(.dayPower, scope: firstScope)
        #expect(registry.current[.dayPower] == firstScope)

        let secondScope = ChartScope.daySpecific(date: Date(timeIntervalSince1970: 1_714_608_000))
        action(.dayPower, scope: secondScope)
        #expect(registry.current[.dayPower] == secondScope)
        #expect(registry.current.count == 1)
    }
}
#endif
