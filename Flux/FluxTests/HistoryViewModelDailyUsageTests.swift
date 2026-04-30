import FluxCore
import Foundation
import SwiftData
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct HistoryViewModelDailyUsageTests {
    @Test
    func dailyUsageSeriesPreservesChronologicalOrderWhenPayloadAlreadySorted() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            makeDay(date: "2026-04-14", blocks: makeChronologicalBlocks(
                night: 2.0, morning: 1.5, offPeak: 4.0, afternoon: 3.0, evening: 2.5
            ))
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        let entries = viewModel.derived.dailyUsage
        #expect(entries.count == 1)
        let entry = try #require(entries.first)
        #expect(entry.blocks.map(\.kind) == DailyUsageBlock.Kind.chronologicalOrder)
        #expect(abs(entry.stackedTotalKwh - 13.0) < 0.001)
        #expect(entry.isToday == false)
    }

    @Test
    func dailyUsageSeriesSortsBlocksWhenPayloadOrderIsMixed() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            makeDay(date: "2026-04-14", blocks: [
                makeBlock(kind: .evening, totalKwh: 2.5),
                makeBlock(kind: .night, totalKwh: 2.0),
                makeBlock(kind: .afternoonPeak, totalKwh: 3.0),
                makeBlock(kind: .morningPeak, totalKwh: 1.5),
                makeBlock(kind: .offPeak, totalKwh: 4.0)
            ])
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        let entry = try #require(viewModel.derived.dailyUsage.first)
        #expect(entry.blocks.map(\.kind) == DailyUsageBlock.Kind.chronologicalOrder)
    }

    @Test
    func dailyUsageSeriesIncludesOffpeakUnresolvedDay() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            makeDay(date: "2026-04-14", blocks: [
                makeBlock(kind: .night, totalKwh: 2.5),
                makeBlock(kind: .evening, totalKwh: 3.5)
            ])
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        let entry = try #require(viewModel.derived.dailyUsage.first)
        #expect(entry.blocks.map(\.kind) == [.night, .evening])
        #expect(abs(entry.stackedTotalKwh - 6.0) < 0.001)
    }

    @Test
    func dailyUsageSeriesOmitsDaysWithoutDailyUsage() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            makeDay(date: "2026-04-13", blocks: makeChronologicalBlocks(
                night: 1.0, morning: 1.0, offPeak: 2.0, afternoon: 2.0, evening: 1.0
            )),
            DayEnergy(
                date: "2026-04-14", epv: 5.0, eInput: 1.0, eOutput: 0.4, eCharge: 1.5, eDischarge: 2.1
            )
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        let entries = viewModel.derived.dailyUsage
        #expect(entries.count == 1)
        #expect(entries.first?.dayID == "2026-04-13")
    }

    @Test
    func dailyUsageSeriesIsEmptyWhenAllDaysNil() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            DayEnergy(date: "2026-04-13", epv: 4.0, eInput: 1.0, eOutput: 0.4, eCharge: 1.5, eDischarge: 2.1),
            DayEnergy(date: "2026-04-14", epv: 4.5, eInput: 1.1, eOutput: 0.4, eCharge: 1.5, eDischarge: 2.2)
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        let derived = viewModel.derived
        #expect(derived.dailyUsage.isEmpty)
        #expect(derived.summary.dailyUsageDayCount == 0)
        #expect(derived.summary.dailyUsageLargestKind == nil)
        #expect(derived.summary.dailyUsageAvgKwh == nil)
    }

    @Test
    func dailyUsageSummaryHidesAverageWhenOnlyTodayHasBlocks() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        // Sydney "today" at 14:30 UTC on 2026-04-15 = 2026-04-16.
        apiClient.historyResult = .success(HistoryResponse(days: [
            makeDay(date: "2026-04-16", blocks: makeChronologicalBlocks(
                night: 1.0, morning: 1.0, offPeak: 1.0, afternoon: 1.0, evening: 1.0
            ))
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        let derived = viewModel.derived
        #expect(derived.dailyUsage.count == 1)
        #expect(derived.dailyUsage.first?.isToday == true)
        #expect(derived.summary.dailyUsageDayCount == 0)
        #expect(derived.summary.dailyUsageLargestKind == nil)
        #expect(derived.summary.dailyUsageAvgKwh == nil)
    }

    @Test
    func dailyUsageSeriesTreatsEmptyBlocksArrayAsMissing() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            makeDay(date: "2026-04-13", blocks: makeChronologicalBlocks(
                night: 1.0, morning: 1.0, offPeak: 2.0, afternoon: 2.0, evening: 1.0
            )),
            makeDay(date: "2026-04-14", blocks: [])
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        #expect(viewModel.derived.dailyUsage.map(\.dayID) == ["2026-04-13"])
    }

    @Test
    func dailyUsageSummaryExcludesTodayMidWindow() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        // Sydney "today" is 2026-04-16; yesterday is 2026-04-15.
        apiClient.historyResult = .success(HistoryResponse(days: [
            makeDay(date: "2026-04-15", blocks: makeChronologicalBlocks(
                night: 1.0, morning: 1.0, offPeak: 1.0, afternoon: 1.0, evening: 1.0
            )),
            makeDay(date: "2026-04-16", blocks: [
                makeBlock(kind: .night, totalKwh: 2.0, status: .complete),
                makeBlock(kind: .morningPeak, totalKwh: 0.4, status: .inProgress)
            ])
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        let derived = viewModel.derived
        #expect(derived.dailyUsage.count == 2)
        let today = try #require(derived.dailyUsage.first(where: { $0.dayID == "2026-04-16" }))
        #expect(today.isToday)
        #expect(derived.summary.dailyUsageDayCount == 1)
        #expect(abs(derived.summary.dailyUsageTotalKwh - 5.0) < 0.001, "today excluded from total")
        #expect(abs((derived.summary.dailyUsageAvgKwh ?? -1) - 5.0) < 0.001)
    }

    @Test
    func dailyUsageClampsZeroAndNegativeBlocks() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            makeDay(date: "2026-04-13", blocks: [
                makeBlock(kind: .night, totalKwh: 0.0),
                makeBlock(kind: .morningPeak, totalKwh: -1.5),
                makeBlock(kind: .offPeak, totalKwh: 2.0),
                makeBlock(kind: .afternoonPeak, totalKwh: 1.0),
                makeBlock(kind: .evening, totalKwh: 1.0)
            ])
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        let entry = try #require(viewModel.derived.dailyUsage.first)
        let night = try #require(entry.blocks.first(where: { $0.kind == .night }))
        let morning = try #require(entry.blocks.first(where: { $0.kind == .morningPeak }))
        #expect(night.totalKwh == 0.0)
        #expect(morning.totalKwh == 0.0, "negative block clamps to zero")
        #expect(abs(entry.stackedTotalKwh - 4.0) < 0.001, "stacked total uses clamped values")
    }

    @Test
    func dailyUsageLargestKindBreaksTiesByChronologicalOrder() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockDailyUsageAPIClient()
        // Night and Evening tie at 1.5 kWh each across the range.
        // Integer-half values are exact in IEEE 754 so the tie is deterministic.
        apiClient.historyResult = .success(HistoryResponse(days: [
            makeDay(date: "2026-04-13", blocks: [
                makeBlock(kind: .night, totalKwh: 1.0),
                makeBlock(kind: .morningPeak, totalKwh: 0.5),
                makeBlock(kind: .offPeak, totalKwh: 0.5),
                makeBlock(kind: .afternoonPeak, totalKwh: 0.5),
                makeBlock(kind: .evening, totalKwh: 0.5)
            ]),
            makeDay(date: "2026-04-14", blocks: [
                makeBlock(kind: .night, totalKwh: 0.5),
                makeBlock(kind: .morningPeak, totalKwh: 0.5),
                makeBlock(kind: .offPeak, totalKwh: 0.5),
                makeBlock(kind: .afternoonPeak, totalKwh: 0.5),
                makeBlock(kind: .evening, totalKwh: 1.0)
            ])
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        #expect(viewModel.derived.summary.dailyUsageLargestKind == .night)
    }

    private func makeModelContext() throws -> ModelContext {
        let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
        let container = try ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
        return ModelContext(container)
    }

    private func makeUTCDate(year: Int, month: Int, day: Int, hour: Int, minute: Int) -> Date {
        let calendar = Calendar(identifier: .gregorian)
        return calendar.date(from: DateComponents(
            timeZone: TimeZone(secondsFromGMT: 0),
            year: year, month: month, day: day, hour: hour, minute: minute
        ))!
    }

    private func makeBlock(
        kind: DailyUsageBlock.Kind,
        totalKwh: Double,
        status: DailyUsageBlock.Status = .complete
    ) -> DailyUsageBlock {
        DailyUsageBlock(
            kind: kind,
            start: "2026-04-14T00:00:00Z",
            end: "2026-04-14T01:00:00Z",
            totalKwh: totalKwh,
            averageKwhPerHour: nil,
            percentOfDay: 0,
            status: status,
            boundarySource: .readings
        )
    }

    private func makeChronologicalBlocks(
        night: Double, morning: Double, offPeak: Double, afternoon: Double, evening: Double
    ) -> [DailyUsageBlock] {
        [
            makeBlock(kind: .night, totalKwh: night),
            makeBlock(kind: .morningPeak, totalKwh: morning),
            makeBlock(kind: .offPeak, totalKwh: offPeak),
            makeBlock(kind: .afternoonPeak, totalKwh: afternoon),
            makeBlock(kind: .evening, totalKwh: evening)
        ]
    }

    private func makeDay(date: String, blocks: [DailyUsageBlock]) -> DayEnergy {
        DayEnergy(
            date: date,
            epv: 5.0, eInput: 1.0, eOutput: 0.4, eCharge: 1.5, eDischarge: 2.1,
            dailyUsage: DailyUsage(blocks: blocks)
        )
    }
}

private final class MockDailyUsageAPIClient: FluxAPIClient, @unchecked Sendable {
    var historyResult: Result<HistoryResponse, Error> = .failure(FluxAPIError.notConfigured)

    func fetchStatus() async throws -> StatusResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        try historyResult.get()
    }

    func fetchDay(date _: String) async throws -> DayDetailResponse {
        throw FluxAPIError.notConfigured
    }

    func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}
