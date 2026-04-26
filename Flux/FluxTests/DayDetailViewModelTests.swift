import FluxCore
import Foundation
import Testing
@testable import Flux

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
            date: "2026-04-15", readings: readings, summary: summary, peakPeriods: nil, eveningNight: nil
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
            date: "2026-04-15", readings: fallbackReadings, summary: nil, peakPeriods: nil, eveningNight: nil
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
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: peaks, eveningNight: nil
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
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, eveningNight: nil
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
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: peaks, eveningNight: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()
        #expect(viewModel.peakPeriods.count == 1)

        apiClient.dayResult = .failure(FluxAPIError.notConfigured)
        await viewModel.loadDay()

        #expect(viewModel.peakPeriods.isEmpty)
    }

    @Test
    func loadDayPopulatesEveningNightFromResponse() async {
        let apiClient = MockDayDetailAPIClient()
        let eveningNight = EveningNight(
            evening: EveningNightBlock(
                start: "2026-04-15T08:30:00Z",
                end: "2026-04-15T14:00:00Z",
                totalKwh: 4.2,
                averageKwhPerHour: 0.85,
                status: .complete,
                boundarySource: .readings
            ),
            night: EveningNightBlock(
                start: "2026-04-14T14:00:00Z",
                end: "2026-04-14T20:30:00Z",
                totalKwh: 3.1,
                averageKwhPerHour: 0.48,
                status: .complete,
                boundarySource: .readings
            )
        )
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, eveningNight: eveningNight
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        let loaded = viewModel.eveningNight
        #expect(loaded?.evening?.totalKwh == 4.2)
        #expect(loaded?.night?.totalKwh == 3.1)
        #expect(loaded?.hasAnyBlock == true)
    }

    @Test
    func loadDayPropagatesEveningNightWithOnlyOneBlock() async {
        let apiClient = MockDayDetailAPIClient()
        let eveningNight = EveningNight(
            evening: nil,
            night: EveningNightBlock(
                start: "2026-04-14T14:00:00Z",
                end: "2026-04-14T20:30:00Z",
                totalKwh: 3.1,
                averageKwhPerHour: 0.48,
                status: .complete,
                boundarySource: .estimated
            )
        )
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, eveningNight: eveningNight
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.eveningNight?.evening == nil)
        #expect(viewModel.eveningNight?.night?.boundarySource == .estimated)
    }

    @Test
    func loadDayWithNilEveningNightLeavesPropertyNil() async {
        let apiClient = MockDayDetailAPIClient()
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, eveningNight: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.eveningNight == nil)
    }

    @Test
    func loadDayFallbackDataPathLeavesEveningNightAsBackendSent() async {
        // Backend invariant (req 1.11): on fallback path, response omits eveningNight.
        // Fixture mirrors that contract — viewModel must propagate the nil through.
        let apiClient = MockDayDetailAPIClient()
        let fallbackReadings = [
            TimeSeriesPoint(timestamp: "2026-04-15T00:00:00Z", ppv: 0, pload: 0, pbat: 0, pgrid: 0, soc: 45),
            TimeSeriesPoint(timestamp: "2026-04-15T00:05:00Z", ppv: 0, pload: 0, pbat: 0, pgrid: 0, soc: 44)
        ]
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: fallbackReadings, summary: nil, peakPeriods: nil, eveningNight: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()

        #expect(viewModel.hasPowerData == false)
        #expect(viewModel.eveningNight == nil)
    }

    @Test
    func loadDayErrorResetsEveningNightToNil() async {
        let apiClient = MockDayDetailAPIClient()
        let eveningNight = EveningNight(
            evening: EveningNightBlock(
                start: "2026-04-15T08:30:00Z",
                end: "2026-04-15T14:00:00Z",
                totalKwh: 4.2,
                averageKwhPerHour: 0.85,
                status: .complete,
                boundarySource: .readings
            ),
            night: nil
        )
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, eveningNight: eveningNight
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()
        #expect(viewModel.eveningNight != nil)

        apiClient.dayResult = .failure(FluxAPIError.notConfigured)
        await viewModel.loadDay()

        #expect(viewModel.eveningNight == nil)
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
