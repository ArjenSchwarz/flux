import FluxCore
import Foundation
import Testing
@testable import Flux

// swiftlint:disable type_body_length
@MainActor @Suite(.serialized)
struct DayDetailViewModelTests {
    @Test
    func loadDayPopulatesReadingsAndSummary() async {
        let apiClient = MockDayDetailAPIClient()
        let readings = [TimeSeriesPoint(
            timestamp: "2026-04-15T00:00:00Z",
            ppv: 1200, pload: 500, pbat: -300, pgrid: -400, soc: 72
        )]
        let summary = DaySummary(
            epv: 8.2, eInput: 1.3, eOutput: 0.7, eCharge: 2.4, eDischarge: 3.6,
            socLow: 21, socLowTime: "2026-04-15T20:00:00Z"
        )
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: readings, summary: summary, peakPeriods: nil, dailyUsage: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.readings.count == 1)
        #expect(viewModel.summary?.epv == 8.2)
        #expect(viewModel.hasPowerData)
    }

    @Test
    func navigatePreviousAndNextUpdateDateString() {
        let apiClient = MockDayDetailAPIClient()
        let fixedNow = Self.makeUTCDate(year: 2026, month: 4, day: 13, hour: 0, minute: 0)
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
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: fallbackReadings, summary: nil, peakPeriods: nil, dailyUsage: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.hasPowerData == false)
    }

    @Test
    func loadDayPopulatesPeakPeriodsFromResponse() async {
        let apiClient = MockDayDetailAPIClient()
        let peaks = [PeakPeriod(start: "17:00", end: "18:00", avgLoadW: 3200, energyWh: 3200)]
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: peaks, dailyUsage: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.peakPeriods.count == 1)
        #expect(viewModel.peakPeriods.first?.start == "17:00")
    }

    @Test
    func loadDayWithNilPeakPeriodsLeavesArrayEmpty() async {
        let apiClient = MockDayDetailAPIClient()
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.peakPeriods.isEmpty)
    }

    @Test
    func loadDayErrorClearsPeakPeriods() async {
        let apiClient = MockDayDetailAPIClient()
        let peaks = [PeakPeriod(start: "17:00", end: "18:00", avgLoadW: 3200, energyWh: 3200)]
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: peaks, dailyUsage: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()
        #expect(viewModel.peakPeriods.count == 1)

        apiClient.dayResult = .failure(FluxAPIError.notConfigured)
        await viewModel.loadDay()

        #expect(viewModel.peakPeriods.isEmpty)
    }

    @Test
    // swiftlint:disable:next function_body_length
    func loadDayPopulatesDailyUsageFromResponse() async {
        let apiClient = MockDayDetailAPIClient()
        let dailyUsage = DailyUsage(blocks: [
            DailyUsageBlock(
                kind: .night,
                start: "2026-04-14T14:00:00Z",
                end: "2026-04-14T20:30:00Z",
                totalKwh: 3.1,
                averageKwhPerHour: 0.48,
                percentOfDay: 18,
                status: .complete,
                boundarySource: .readings
            ),
            DailyUsageBlock(
                kind: .morningPeak,
                start: "2026-04-14T20:30:00Z",
                end: "2026-04-15T01:00:00Z",
                totalKwh: 2.1,
                averageKwhPerHour: 0.47,
                percentOfDay: 12,
                status: .complete,
                boundarySource: .readings
            ),
            DailyUsageBlock(
                kind: .offPeak,
                start: "2026-04-15T01:00:00Z",
                end: "2026-04-15T04:00:00Z",
                totalKwh: 5.0,
                averageKwhPerHour: 1.67,
                percentOfDay: 30,
                status: .complete,
                boundarySource: .readings
            ),
            DailyUsageBlock(
                kind: .afternoonPeak,
                start: "2026-04-15T04:00:00Z",
                end: "2026-04-15T08:42:00Z",
                totalKwh: 4.5,
                averageKwhPerHour: 0.96,
                percentOfDay: 27,
                status: .complete,
                boundarySource: .readings
            ),
            DailyUsageBlock(
                kind: .evening,
                start: "2026-04-15T08:42:00Z",
                end: "2026-04-15T14:00:00Z",
                totalKwh: 2.2,
                averageKwhPerHour: 0.41,
                percentOfDay: 13,
                status: .complete,
                boundarySource: .readings
            )
        ])
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, dailyUsage: dailyUsage
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        let loaded = viewModel.dailyUsage
        #expect(loaded?.blocks.count == 5)
        #expect(loaded?.blocks[0].kind == .night)
        #expect(loaded?.blocks[2].kind == .offPeak)
        #expect(loaded?.blocks[4].kind == .evening)
    }

    @Test
    func loadDayPropagatesDailyUsageWithTwoBlocks() async {
        let apiClient = MockDayDetailAPIClient()
        let dailyUsage = DailyUsage(blocks: [
            DailyUsageBlock(
                kind: .night,
                start: "2026-04-14T14:00:00Z",
                end: "2026-04-14T20:30:00Z",
                totalKwh: 3.1,
                averageKwhPerHour: 0.48,
                percentOfDay: 42,
                status: .complete,
                boundarySource: .estimated
            ),
            DailyUsageBlock(
                kind: .evening,
                start: "2026-04-15T08:42:00Z",
                end: "2026-04-15T14:00:00Z",
                totalKwh: 4.3,
                averageKwhPerHour: 0.81,
                percentOfDay: 58,
                status: .complete,
                boundarySource: .estimated
            )
        ])
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, dailyUsage: dailyUsage
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.dailyUsage?.blocks.count == 2)
        #expect(viewModel.dailyUsage?.blocks[0].kind == .night)
        #expect(viewModel.dailyUsage?.blocks[1].kind == .evening)
        #expect(viewModel.dailyUsage?.blocks[0].boundarySource == .estimated)
    }

    @Test
    func loadDayWithNilDailyUsageLeavesPropertyNil() async {
        let apiClient = MockDayDetailAPIClient()
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.dailyUsage == nil)
    }

    @Test
    func loadDayFallbackDataPathLeavesDailyUsageAsBackendSent() async {
        // Backend invariant (req 1.10): on fallback path, response omits dailyUsage.
        // Fixture mirrors that contract — viewModel must propagate the nil through.
        let apiClient = MockDayDetailAPIClient()
        let fallbackReadings = [
            TimeSeriesPoint(timestamp: "2026-04-15T00:00:00Z", ppv: 0, pload: 0, pbat: 0, pgrid: 0, soc: 45),
            TimeSeriesPoint(timestamp: "2026-04-15T00:05:00Z", ppv: 0, pload: 0, pbat: 0, pgrid: 0, soc: 44)
        ]
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: fallbackReadings, summary: nil, peakPeriods: nil, dailyUsage: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.hasPowerData == false)
        #expect(viewModel.dailyUsage == nil)
    }

    @Test
    func loadDayErrorResetsDailyUsageToNil() async {
        let apiClient = MockDayDetailAPIClient()
        let dailyUsage = DailyUsage(blocks: [
            DailyUsageBlock(
                kind: .evening,
                start: "2026-04-15T08:30:00Z",
                end: "2026-04-15T14:00:00Z",
                totalKwh: 4.2,
                averageKwhPerHour: 0.85,
                percentOfDay: 50,
                status: .complete,
                boundarySource: .readings
            )
        ])
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, dailyUsage: dailyUsage
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()
        #expect(viewModel.dailyUsage != nil)

        apiClient.dayResult = .failure(FluxAPIError.notConfigured)
        await viewModel.loadDay()

        #expect(viewModel.dailyUsage == nil)
    }

    @Test
    func responseWithoutPeakPeriodsKeyDecodesToNil() throws {
        let json = """
        {"date":"2026-04-15","readings":[],"summary":null}
        """
        let data = Data(json.utf8)
        let response = try JSONDecoder().decode(DayDetailResponse.self, from: data)
        #expect(response.peakPeriods == nil)
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

// swiftlint:enable type_body_length

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
