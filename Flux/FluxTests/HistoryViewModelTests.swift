import Foundation
import SwiftData
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct HistoryViewModelTests {
    @Test
    func loadHistoryFetchesFromAPIAndPopulatesDays() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockHistoryAPIClient()
        let expectedDay = DayEnergy(
            date: "2026-04-15", epv: 8.4, eInput: 1.2, eOutput: 0.4, eCharge: 2.1, eDischarge: 3.3
        )
        apiClient.historyResult = .success(HistoryResponse(days: [expectedDay]))

        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)

        await viewModel.loadHistory(days: 7)

        #expect(viewModel.days.count == 1)
        #expect(viewModel.days.first?.date == expectedDay.date)
    }

    @Test
    func loadHistoryCachesOnlyHistoricalDaysUsingSydneyTimezone() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockHistoryAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            DayEnergy(date: "2026-04-15", epv: 6.1, eInput: 1.0, eOutput: 0.2, eCharge: 3.0, eDischarge: 2.5),
            DayEnergy(date: "2026-04-16", epv: 4.0, eInput: 0.8, eOutput: 0.1, eCharge: 2.0, eDischarge: 1.9)
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })

        await viewModel.loadHistory(days: 7)

        let cached = try modelContext.fetch(
            FetchDescriptor<CachedDayEnergy>(sortBy: [SortDescriptor(\CachedDayEnergy.date)])
        )
        #expect(cached.map(\.date) == ["2026-04-15"])
    }

    @Test
    func loadHistoryFallsBackToCacheWhenNetworkFails() async throws {
        let modelContext = try makeModelContext()
        modelContext.insert(CachedDayEnergy(from: DayEnergy(
            date: "2026-04-14", epv: 5.2, eInput: 0.9, eOutput: 0.3, eCharge: 1.8, eDischarge: 2.7
        )))
        try modelContext.save()

        let apiClient = MockHistoryAPIClient()
        apiClient.historyResult = .failure(FluxAPIError.networkError("offline"))

        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)

        await viewModel.loadHistory(days: 7)

        #expect(viewModel.days.count == 1)
        #expect(viewModel.days.first?.date == "2026-04-14")
        #expect(viewModel.error == nil)
    }

    @Test
    func loadHistorySetsErrorWhenNetworkFailsWithoutCache() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockHistoryAPIClient()
        apiClient.historyResult = .failure(FluxAPIError.serverError)

        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)

        await viewModel.loadHistory(days: 7)

        #expect(viewModel.days.isEmpty)
        #expect(viewModel.error == .serverError)
    }

    // T-841 regression guards: the history chart must produce one chart entry
    // per metric per day, across the full requested range — not only today.
    // The rendering bug (invisible bars for non-today dates) was caused by
    // pairing a continuous Date x-axis with .position(by:); the fix switches
    // to the stable dayID string so each day gets a discrete axis slot. These
    // tests lock in the structural invariant that every day in the response
    // is represented in chartDays / chartEntries with a distinct dayID.
    @Test
    func rebuildChartDataProducesOneEntryPerMetricPerDay() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockHistoryAPIClient()
        let days = [
            DayEnergy(date: "2026-04-13", epv: 5.0, eInput: 1.0, eOutput: 0.2, eCharge: 2.0, eDischarge: 3.0),
            DayEnergy(date: "2026-04-14", epv: 6.0, eInput: 1.1, eOutput: 0.3, eCharge: 2.1, eDischarge: 3.1),
            DayEnergy(date: "2026-04-15", epv: 7.0, eInput: 1.2, eOutput: 0.4, eCharge: 2.2, eDischarge: 3.2)
        ]
        apiClient.historyResult = .success(HistoryResponse(days: days))

        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)
        await viewModel.loadHistory(days: 7)

        let metricCount = HistoryViewModel.HistoryChartMetric.allCases.count
        #expect(viewModel.chartDays.count == days.count)
        #expect(viewModel.chartEntries.count == days.count * metricCount)

        let uniqueDayIDs = Set(viewModel.chartEntries.map(\.dayID))
        #expect(uniqueDayIDs == Set(days.map(\.date)))
    }

    @Test
    func chartEntriesUseDiscreteDateKeyForEachDay() async throws {
        // With a discrete (String) x-axis, each day maps to a unique axis
        // category — this is what gives every day a visible slot on the chart.
        // Using the underlying Date on a continuous axis caused bars for
        // non-today dates to collapse to invisible widths.
        let modelContext = try makeModelContext()
        let apiClient = MockHistoryAPIClient()
        let days = (0 ..< 7).map { offset in
            DayEnergy(
                date: String(format: "2026-04-%02d", 9 + offset),
                epv: Double(offset),
                eInput: Double(offset),
                eOutput: Double(offset),
                eCharge: Double(offset),
                eDischarge: Double(offset)
            )
        }
        apiClient.historyResult = .success(HistoryResponse(days: days))

        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)
        await viewModel.loadHistory(days: 7)

        // Every day must contribute entries for every metric — proving the
        // chart has data for every day, not only today.
        for day in days {
            let entriesForDay = viewModel.chartEntries.filter { $0.dayID == day.date }
            #expect(entriesForDay.count == HistoryViewModel.HistoryChartMetric.allCases.count,
                    "expected one entry per metric for \(day.date)")
        }
    }

    @Test
    func selectDayUpdatesSelectedDay() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockHistoryAPIClient()
        let firstDay = DayEnergy(date: "2026-04-15", epv: 4.0, eInput: 1.0, eOutput: 0.4, eCharge: 1.5, eDischarge: 2.1)
        let secondDay = DayEnergy(
            date: "2026-04-14", epv: 5.0, eInput: 1.2, eOutput: 0.3, eCharge: 1.7, eDischarge: 2.3
        )
        apiClient.historyResult = .success(HistoryResponse(days: [firstDay, secondDay]))

        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)
        await viewModel.loadHistory(days: 7)
        viewModel.selectDay(secondDay)

        #expect(viewModel.selectedDay?.date == secondDay.date)
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
            year: year,
            month: month,
            day: day,
            hour: hour,
            minute: minute
        ))!
    }
}

private final class MockHistoryAPIClient: FluxAPIClient, @unchecked Sendable {
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
}
