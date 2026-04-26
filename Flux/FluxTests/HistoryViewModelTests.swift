import FluxCore
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

    @Test
    func gridSeriesOnlyIncludesDaysWithOffpeakData() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockHistoryAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            // No off-peak data — should be excluded from grid series.
            DayEnergy(date: "2026-04-13", epv: 5.0, eInput: 4.0, eOutput: 1.0, eCharge: 2.0, eDischarge: 1.5),
            // Has off-peak split — included.
            DayEnergy(
                date: "2026-04-14", epv: 6.0, eInput: 5.0, eOutput: 1.5, eCharge: 2.5, eDischarge: 2.0,
                offpeakGridImportKwh: 3.0, offpeakGridExportKwh: 0.4
            )
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        #expect(viewModel.solarSeries.count == 2, "solar shows every day")
        #expect(viewModel.batterySeries.count == 2, "battery shows every day")
        #expect(viewModel.gridSeries.count == 1, "grid only shows days with off-peak data")
        let gridEntry = try #require(viewModel.gridSeries.first)
        #expect(gridEntry.dayID == "2026-04-14")
        #expect(abs(gridEntry.peakImportKwh - 2.0) < 0.001, "peak = total import - off-peak import")
        #expect(abs(gridEntry.offpeakImportKwh - 3.0) < 0.001)
    }

    @Test
    func summaryExcludesTodayFromAveragesButCountsOffpeakAlways() async throws {
        let modelContext = try makeModelContext()
        let apiClient = MockHistoryAPIClient()
        // nowProvider below is 14:30 UTC on 2026-04-15 = 00:30 AEST on
        // 2026-04-16, so 2026-04-16 is the Sydney "today" used by the VM.
        apiClient.historyResult = .success(HistoryResponse(days: [
            DayEnergy(
                date: "2026-04-15", epv: 10.0, eInput: 4.0, eOutput: 1.0, eCharge: 5.0, eDischarge: 4.0,
                offpeakGridImportKwh: 2.5, offpeakGridExportKwh: 0.3
            ),
            DayEnergy(
                date: "2026-04-16", epv: 8.0, eInput: 3.0, eOutput: 0.5, eCharge: 4.0, eDischarge: 3.0,
                offpeakGridImportKwh: 1.5, offpeakGridExportKwh: 0.2
            )
        ]))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })
        await viewModel.loadHistory(days: 7)

        let summary = viewModel.summary
        #expect(summary.solarDayCount == 1, "today is excluded from completed-day count")
        #expect(abs(summary.solarTotalKwh - 10.0) < 0.001, "today's solar excluded from total")
        #expect(summary.gridDayCount == 2, "today's grid is still counted in the off-peak split")
        #expect(abs(summary.peakImportTotalKwh - (1.5 + 1.5)) < 0.001, "peak = (4-2.5) + (3-1.5)")
        #expect(abs(summary.offpeakImportTotalKwh - 4.0) < 0.001)
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
