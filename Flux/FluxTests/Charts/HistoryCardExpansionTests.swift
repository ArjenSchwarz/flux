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

    @Test("HistorySolarCard.expansionScope carries the current periodQuery")
    func solarExpansionScopeTracksQuery() {
        for query in Self.sampleQueries {
            let card = makeSolarCard(periodQuery: query)
            #expect(card.expansionScope == .historyRange(query))
        }
    }

    @Test("HistoryGridUsageCard.expansionScope carries the current periodQuery")
    func gridExpansionScopeTracksQuery() {
        for query in Self.sampleQueries {
            let card = makeGridCard(periodQuery: query)
            #expect(card.expansionScope == .historyRange(query))
        }
    }

    @Test("HistoryDailyUsageCard.expansionScope carries the current periodQuery")
    func dailyUsageExpansionScopeTracksQuery() {
        for query in Self.sampleQueries {
            let card = makeDailyUsageCard(periodQuery: query)
            #expect(card.expansionScope == .historyRange(query))
        }
    }

    /// Both query forms: the fixed day-count windows and a navigated past
    /// period, which the expansion scope must carry unchanged (Decision 13).
    private static let sampleQueries: [HistoryQuery] = [
        .days(7),
        .days(14),
        .days(30),
        .dateRange(start: "2026-04-06", end: "2026-04-12")
    ]

    private func makeSolarCard(periodQuery: HistoryQuery) -> HistorySolarCard {
        HistorySolarCard(
            entries: [],
            summary: .empty,
            selectedDate: nil,
            periodQuery: periodQuery,
            onSelect: { _ in }
        )
    }

    private func makeGridCard(periodQuery: HistoryQuery) -> HistoryGridUsageCard {
        HistoryGridUsageCard(
            entries: [],
            summary: .empty,
            selectedDate: nil,
            periodQuery: periodQuery,
            onSelect: { _ in }
        )
    }

    private func makeDailyUsageCard(periodQuery: HistoryQuery) -> HistoryDailyUsageCard {
        HistoryDailyUsageCard(
            entries: [],
            summary: .empty,
            selectedDate: nil,
            periodQuery: periodQuery,
            onSelect: { _ in }
        )
    }
}
