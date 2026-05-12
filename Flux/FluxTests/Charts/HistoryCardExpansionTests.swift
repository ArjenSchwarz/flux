import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor
@Suite
struct HistoryCardExpansionTests {
    @Test("Each History card declares a distinct ChartKind matching its identity")
    func historyCardsDeclareDistinctChartKinds() {
        #expect(HistorySolarCard.chartKind == .historySolar)
        #expect(HistoryGridUsageCard.chartKind == .historyGridUsage)
        #expect(HistoryDailyUsageCard.chartKind == .historyDailyUsage)

        let kinds: Set<ChartKind> = [
            HistorySolarCard.chartKind,
            HistoryGridUsageCard.chartKind,
            HistoryDailyUsageCard.chartKind
        ]
        #expect(kinds.count == 3)
    }

    @Test("HistorySolarCard.expansionScope tracks the current rangeDays")
    func solarExpansionScopeTracksRange() {
        for days in [7, 14, 30] {
            let card = makeSolarCard(rangeDays: days)
            #expect(card.expansionScope == .historyRange(days: days))
        }
    }

    @Test("HistoryGridUsageCard.expansionScope tracks the current rangeDays")
    func gridExpansionScopeTracksRange() {
        for days in [7, 14, 30] {
            let card = makeGridCard(rangeDays: days)
            #expect(card.expansionScope == .historyRange(days: days))
        }
    }

    @Test("HistoryDailyUsageCard.expansionScope tracks the current rangeDays")
    func dailyUsageExpansionScopeTracksRange() {
        for days in [7, 14, 30] {
            let card = makeDailyUsageCard(rangeDays: days)
            #expect(card.expansionScope == .historyRange(days: days))
        }
    }

    private func makeSolarCard(rangeDays: Int) -> HistorySolarCard {
        HistorySolarCard(
            entries: [],
            summary: .empty,
            selectedDate: nil,
            rangeDays: rangeDays,
            onSelect: { _ in }
        )
    }

    private func makeGridCard(rangeDays: Int) -> HistoryGridUsageCard {
        HistoryGridUsageCard(
            entries: [],
            summary: .empty,
            selectedDate: nil,
            rangeDays: rangeDays,
            onSelect: { _ in }
        )
    }

    private func makeDailyUsageCard(rangeDays: Int) -> HistoryDailyUsageCard {
        HistoryDailyUsageCard(
            entries: [],
            summary: .empty,
            selectedDate: nil,
            rangeDays: rangeDays,
            onSelect: { _ in }
        )
    }
}
