import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct DayDetailViewModelSetDateTests {
    @Test
    func setDateWithSameDateIsNoOp() async {
        let apiClient = SetDateMockAPIClient()
        apiClient.dayResult = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil
        ))
        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()
        let initialCount = apiClient.fetchDayCount

        await viewModel.setDate("2026-04-15")

        #expect(apiClient.fetchDayCount == initialCount, "setDate with current date should not call the API")
        #expect(viewModel.date == "2026-04-15")
    }

    @Test
    func setDateCallsLoadDayExactlyOnce() async {
        let apiClient = SetDateMockAPIClient()
        apiClient.dayResultsByDate["2026-04-15"] = .success(DayDetailResponse(
            date: "2026-04-15", readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil
        ))
        apiClient.dayResultsByDate["2026-04-16"] = .success(DayDetailResponse(
            date: "2026-04-16", readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil
        ))
        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()
        let countAfterFirstLoad = apiClient.fetchDayCount

        await viewModel.setDate("2026-04-16")

        #expect(apiClient.fetchDayCount == countAfterFirstLoad + 1)
        #expect(apiClient.fetchDayDates.last == "2026-04-16")
        #expect(viewModel.date == "2026-04-16")
    }

    @Test
    func setDateClearsPerDayFieldsBeforeReload() async {
        let apiClient = SetDateMockAPIClient()
        let readings = [TimeSeriesPoint(
            timestamp: "2026-04-15T00:00:00Z",
            ppv: 1200, pload: 500, pbat: -300, pgrid: -400, soc: 72
        )]
        let summary = DaySummary(
            epv: 8.2, eInput: 1.3, eOutput: 0.7, eCharge: 2.4, eDischarge: 3.6,
            socLow: 21, socLowTime: "2026-04-15T20:00:00Z"
        )
        let peaks = [PeakPeriod(start: "17:00", end: "18:00", avgLoadW: 3200, energyWh: 3200)]
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
        apiClient.dayResultsByDate["2026-04-15"] = .success(DayDetailResponse(
            date: "2026-04-15",
            readings: readings,
            summary: summary,
            peakPeriods: peaks,
            dailyUsage: dailyUsage,
            note: "Day note"
        ))
        apiClient.dayResultsByDate["2026-04-16"] = .success(DayDetailResponse(
            date: "2026-04-16", readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil
        ))

        let viewModel = DayDetailViewModel(date: "2026-04-15", apiClient: apiClient)
        await viewModel.loadDay()
        #expect(!viewModel.readings.isEmpty)
        #expect(viewModel.summary != nil)
        #expect(!viewModel.peakPeriods.isEmpty)
        #expect(viewModel.dailyUsage != nil)
        #expect(viewModel.note == "Day note")

        await viewModel.setDate("2026-04-16")

        #expect(viewModel.readings.isEmpty)
        #expect(viewModel.parsedReadings.isEmpty)
        #expect(viewModel.summary == nil)
        #expect(viewModel.peakPeriods.isEmpty)
        #expect(viewModel.dailyUsage == nil)
        #expect(viewModel.note == nil)
        #expect(viewModel.offpeakStats == .empty)
        #expect(viewModel.comparisonState == .off)
    }
}

private final class SetDateMockAPIClient: FluxAPIClient, @unchecked Sendable {
    var dayResult: Result<DayDetailResponse, Error> = .failure(FluxAPIError.notConfigured)
    var dayResultsByDate: [String: Result<DayDetailResponse, Error>] = [:]
    private(set) var fetchDayCount = 0
    private(set) var fetchDayDates: [String] = []

    func fetchStatus() async throws -> StatusResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchDay(date: String) async throws -> DayDetailResponse {
        fetchDayCount += 1
        fetchDayDates.append(date)
        if let result = dayResultsByDate[date] {
            return try result.get()
        }
        return try dayResult.get()
    }

    func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}
