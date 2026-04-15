import Foundation
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct DayDetailViewModelTests {
    @Test
    func loadDayPopulatesReadingsAndSummary() async {
        let apiClient = MockDayDetailAPIClient()
        let readings = [TimeSeriesPoint(timestamp: "2026-04-15T00:00:00Z", ppv: 1200, pload: 500, pbat: -300, pgrid: -400, soc: 72)]
        let summary = DaySummary(epv: 8.2, eInput: 1.3, eOutput: 0.7, eCharge: 2.4, eDischarge: 3.6, socLow: 21, socLowTime: "2026-04-15T20:00:00Z")
        apiClient.dayResult = .success(DayDetailResponse(date: "2026-04-15", readings: readings, summary: summary))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.readings.count == 1)
        #expect(viewModel.summary?.epv == 8.2)
        #expect(viewModel.hasPowerData)
    }

    @Test
    func navigatePreviousAndNextUpdateDateString() {
        let apiClient = MockDayDetailAPIClient()
        let fixedNow = Self.makeUTCDate(year: 2026, month: 4, day: 14, hour: 0, minute: 0)
        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient, nowProvider: {
            fixedNow
        })

        viewModel.navigatePrevious()
        #expect(viewModel.date == "2026-04-14")

        viewModel.navigateNext()
        #expect(viewModel.date == "2026-04-15")
    }

    @Test
    func navigateNextIsDisabledWhenDateIsSydneyToday() {
        let apiClient = MockDayDetailAPIClient()
        let now = Self.makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = DayDetailViewModel(date: "2026-04-16", apiClient: apiClient, nowProvider: { now })

        viewModel.navigateNext()

        #expect(viewModel.date == "2026-04-16")
        #expect(viewModel.isToday)
    }

    @Test
    func loadDayMarksFallbackDataAsNoPowerData() async {
        let apiClient = MockDayDetailAPIClient()
        let fallbackReadings = [
            TimeSeriesPoint(timestamp: "2026-04-15T00:00:00Z", ppv: 0, pload: 0, pbat: 0, pgrid: 0, soc: 45),
            TimeSeriesPoint(timestamp: "2026-04-15T00:05:00Z", ppv: 0, pload: 0, pbat: 0, pgrid: 0, soc: 44)
        ]
        apiClient.dayResult = .success(DayDetailResponse(date: "2026-04-15", readings: fallbackReadings, summary: nil))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.hasPowerData == false)
    }

    private static func makeUTCDate(year: Int, month: Int, day: Int, hour: Int, minute: Int) -> Date {
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

private final class MockDayDetailAPIClient: FluxAPIClient, @unchecked Sendable {
    var dayResult: Result<DayDetailResponse, Error> = .failure(FluxAPIError.notConfigured)

    func fetchStatus() async throws -> StatusResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchDay(date _: String) async throws -> DayDetailResponse {
        try dayResult.get()
    }
}
