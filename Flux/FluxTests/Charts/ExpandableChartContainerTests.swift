import FluxCore
import Foundation
import SwiftUI
import Testing
@testable import Flux

@MainActor
@Suite
struct ExpandableChartContainerTests {
    @Test("Visual contract matches AC 1.2")
    func visualContract() {
        #expect(ExpandableChartContainer<EmptyView>.buttonSymbolName == "arrow.up.left.and.arrow.down.right")
        #expect(ExpandableChartContainer<EmptyView>.buttonInset == 8)
        #expect(ExpandableChartContainer<EmptyView>.accessibilityLabel == "Expand chart")
    }

    @Test("invoke calls the expansion action with the container's kind and current scope")
    func invokeForwardsKindAndScope() {
        var captured: (ChartKind, ChartScope)?
        let action = ChartExpansionAction { kind, scope in
            captured = (kind, scope)
        }

        let date = Date(timeIntervalSince1970: 1_700_000_000)
        let container = ExpandableChartContainer(
            kind: .dayPower,
            scopeProvider: { .daySpecific(date: date) },
            content: { EmptyView() }
        )

        container.invoke(action: action)

        #expect(captured?.0 == .dayPower)
        #expect(captured?.1 == .daySpecific(date: date))
    }

    @Test("invoke evaluates the scope provider at call time, not construction time")
    func scopeProviderIsLazy() {
        var currentDays = 7
        let container = ExpandableChartContainer(
            kind: .historySolar,
            scopeProvider: { .historyRange(.days(currentDays)) },
            content: { EmptyView() }
        )

        var captured: ChartScope?
        let action = ChartExpansionAction { _, scope in captured = scope }

        currentDays = 14
        container.invoke(action: action)

        #expect(captured == .historyRange(.days(14)))
    }

    @Test("Each ChartKind passed in is forwarded unchanged to the action")
    func everyChartKindIsForwarded() {
        for kind in ChartKind.allCases {
            var captured: ChartKind?
            let action = ChartExpansionAction { capturedKind, _ in captured = capturedKind }

            let container = ExpandableChartContainer(
                kind: kind,
                scopeProvider: { .historyRange(.days(1)) },
                content: { EmptyView() }
            )

            container.invoke(action: action)

            #expect(captured == kind)
        }
    }
}
