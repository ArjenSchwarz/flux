import FluxCore
import Foundation
import SwiftData
import Testing
@testable import Flux

/// The view models are the join point between the wire payloads and the cost
/// helper: they must hand over the band split, the off-peak row's geometry and
/// provenance, and the plan pricing the day. Anything dropped here silently
/// degrades a banded day to the fallback tier, so these tests assert on the
/// resolved tier, not just the numbers.
@MainActor @Suite(.serialized)
struct PricingCostWiringTests {
    // MARK: - Day Detail

    @Test
    func dayDetailPricesABandedDayFromTheStoredSplit() async throws {
        let apiClient = MockCostWiringAPIClient()
        apiClient.dayResponse = DayDetailResponse(
            date: "2026-08-15",
            readings: [],
            summary: bandedSummary(),
            peakPeriods: nil,
            dailyUsage: nil
        )
        let (viewModel, service) = try await makeDayDetail(date: "2026-08-15", apiClient: apiClient)
        await viewModel.loadDay()
        _ = service

        let costs = try #require(viewModel.costs)
        #expect(costs.tier == .banded, "the split, the off-peak row and the plan must all reach the helper")
        #expect(abs(costs.peakImportsCost - (1 * 0.35 + 4 * 0.28 + 2 * 0.35 + 8 * 0.35)) < 1e-9)
        #expect(abs(costs.offPeakSavings - 3 * 0.35) < 1e-9)
    }

    @Test
    func dayDetailFallsBackWhenTheSplitWasCapturedUnderAnotherWindow() async throws {
        let apiClient = MockCostWiringAPIClient()
        var summary = bandedSummary()
        // The off-peak row still carries the previous free window, so its
        // import cannot price this plan's free band (Q16).
        summary = DaySummary(
            epv: nil, eInput: 23, eOutput: 15,
            eCharge: nil, eDischarge: nil, socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: 3,
            bandImports: summary.bandImports,
            offpeakWindowStart: "11:00",
            offpeakWindowEnd: "14:00",
            offpeakIntegratedAt: "2026-08-16T05:00:00Z",
            offpeakSampleCount: 1500
        )
        apiClient.dayResponse = DayDetailResponse(
            date: "2026-08-15", readings: [], summary: summary, peakPeriods: nil, dailyUsage: nil
        )
        let (viewModel, _) = try await makeDayDetail(date: "2026-08-15", apiClient: apiClient)
        await viewModel.loadDay()

        let costs = try #require(viewModel.costs)
        #expect(costs.tier == .fallback)
    }

    @Test
    func dayDetailHasNoCostsForAnUnpricedDay() async throws {
        let apiClient = MockCostWiringAPIClient()
        apiClient.dayResponse = DayDetailResponse(
            date: "2020-01-01", readings: [], summary: bandedSummary(), peakPeriods: nil, dailyUsage: nil
        )
        let (viewModel, _) = try await makeDayDetail(date: "2020-01-01", apiClient: apiClient)
        await viewModel.loadDay()
        #expect(viewModel.costs == nil, "days no plan covers show no cost data (AC 2.7)")
    }

    @Test
    func dayDetailTakesItsFreeWindowFromThePlanPricingThatDay() async throws {
        let apiClient = MockCostWiringAPIClient()
        let (switchDay, _) = try await makeDayDetail(date: "2026-08-01", apiClient: apiClient)
        #expect(switchDay.offpeakWindow?.start == "10:00")
        #expect(switchDay.offpeakWindow?.end == "15:00")

        let (switchEve, _) = try await makeDayDetail(date: "2026-07-31", apiClient: apiClient)
        #expect(switchEve.offpeakWindow?.start == "11:00")
        #expect(switchEve.offpeakWindow?.end == "14:00")
    }

    @Test
    func anUnpricedDayGetsNoWindowRatherThanADefault() async throws {
        let apiClient = MockCostWiringAPIClient()
        let (viewModel, _) = try await makeDayDetail(date: "2020-01-01", apiClient: apiClient)
        #expect(viewModel.offpeakWindow == nil, "no plan means no window, never a substituted default")
    }

    // MARK: - History

    @Test
    func historyPricesEachDayUnderItsOwnPlanAcrossASwitchDate() async throws {
        let apiClient = MockCostWiringAPIClient()
        apiClient.historyResponse = HistoryResponse(days: [
            day(date: "2026-07-31", eInput: 10),
            day(date: "2026-08-01", eInput: 10)
        ])
        let (viewModel, _) = try await makeHistory(apiClient: apiClient)
        await viewModel.loadHistory(range: .days(7))

        let costs = try #require(viewModel.periodCosts)
        #expect(costs.pricedDayCount == 2)
        // Predecessor rate 0.2873 on the 31st, successor's highest rate 0.35
        // (fallback: multi-rate plan with no split) on the switch day.
        #expect(abs(costs.peakImportsCost - (10 * 0.2873 + 10 * 0.35)) < 1e-9)
    }

    @Test
    func historyRetainsThePartialCoverageCount() async throws {
        let apiClient = MockCostWiringAPIClient()
        apiClient.historyResponse = HistoryResponse(days: [
            day(date: "2026-08-01", eInput: 10),
            day(date: "2020-01-01", eInput: 10),
            day(date: "2020-01-02", eInput: 10)
        ])
        let (viewModel, _) = try await makeHistory(apiClient: apiClient)
        await viewModel.loadHistory(range: .days(7))

        let costs = try #require(viewModel.periodCosts)
        #expect(costs.pricedDayCount == 1)
        #expect(costs.totalDayCount == 3)
        #expect(costs.hasPartialCoverage, "AC 3.7's N of M days priced caption must survive")
    }

    @Test
    func historyPricesABandedDayFromTheStoredSplit() async throws {
        let apiClient = MockCostWiringAPIClient()
        apiClient.historyResponse = HistoryResponse(days: [bandedDay()])
        let (viewModel, _) = try await makeHistory(apiClient: apiClient)
        await viewModel.loadHistory(range: .days(7))

        let costs = try #require(viewModel.periodCosts)
        #expect(abs(costs.peakImportsCost - (1 * 0.35 + 4 * 0.28 + 2 * 0.35 + 8 * 0.35)) < 1e-9)
        #expect(abs(costs.offPeakSavings - 3 * 0.35) < 1e-9)
    }

    // MARK: - Data consistency

    @Test
    func dayDetailAndHistoryReportTheSameCostForTheSameDay() async throws {
        let dayClient = MockCostWiringAPIClient()
        dayClient.dayResponse = DayDetailResponse(
            date: "2026-08-15", readings: [], summary: bandedSummary(), peakPeriods: nil, dailyUsage: nil
        )
        let (dayViewModel, _) = try await makeDayDetail(date: "2026-08-15", apiClient: dayClient)
        await dayViewModel.loadDay()

        let historyClient = MockCostWiringAPIClient()
        historyClient.historyResponse = HistoryResponse(days: [bandedDay()])
        let (historyViewModel, _) = try await makeHistory(apiClient: historyClient)
        await historyViewModel.loadHistory(range: .days(7))

        let dayCosts = try #require(dayViewModel.costs)
        let periodCosts = try #require(historyViewModel.periodCosts)
        #expect(abs(dayCosts.peakImportsCost - periodCosts.peakImportsCost) < 1e-9)
        #expect(abs(dayCosts.solarFeedInIncome - periodCosts.solarFeedInIncome) < 1e-9)
        #expect(abs(dayCosts.net - periodCosts.net) < 1e-9)
        #expect(abs(dayCosts.offPeakSavings - periodCosts.offPeakSavings) < 1e-9)
    }

    @Test
    func aCachedDayKeepsItsBandSplitSoOfflineCostsMatchTheLiveOnes() throws {
        // History falls back to the SwiftData cache when a fetch fails. If the
        // cache drops the split or the off-peak row's geometry, the same day
        // silently reprices at the fallback tier the moment the network blips.
        let live = bandedDay()
        let restored = CachedDayEnergy(from: live).asDayEnergy

        #expect(restored.bandImports == live.bandImports)
        #expect(restored.offpeakWindowStart == live.offpeakWindowStart)
        #expect(restored.offpeakWindowEnd == live.offpeakWindowEnd)
        #expect(restored.offpeakIntegratedAt == live.offpeakIntegratedAt)
        #expect(restored.offpeakSampleCount == live.offpeakSampleCount)

        let liveCosts = try #require(live.costs(in: Self.plans))
        let offlineCosts = try #require(restored.costs(in: Self.plans))
        #expect(liveCosts == offlineCosts)
        #expect(offlineCosts.tier == .banded)
    }

    // MARK: - Fixtures

    /// The plans either side of the switch date: the migrated single-rate plan
    /// until 2026-08-01, the time-of-use plan from it.
    private static let plans: [PricingPlan] = [
        PricingPlan(
            id: "old", startDate: "2026-01-01", endDate: "2026-08-01",
            defaultRate: 0.2873,
            windows: [PlanWindow(start: "11:00", end: "14:00", free: true, rate: nil)],
            feedInRate: 0.05, savingsReferenceRate: 0.2873,
            createdAt: Date(timeIntervalSince1970: 1), updatedAt: Date(timeIntervalSince1970: 1)
        ),
        PricingPlan(
            id: "new", startDate: "2026-08-01", endDate: nil,
            defaultRate: 0.35,
            windows: [
                PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
                PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28)
            ],
            feedInRate: 0.05, savingsReferenceRate: 0.35,
            createdAt: Date(timeIntervalSince1970: 1), updatedAt: Date(timeIntervalSince1970: 1)
        )
    ]

    private static let split = [
        BandImport(start: "00:00", end: "01:00", kwh: 1),
        BandImport(start: "01:00", end: "06:00", kwh: 4),
        BandImport(start: "06:00", end: "10:00", kwh: 2),
        BandImport(start: "15:00", end: "24:00", kwh: 8)
    ]

    private func bandedSummary() -> DaySummary {
        DaySummary(
            epv: nil, eInput: 23, eOutput: 15,
            eCharge: nil, eDischarge: nil, socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: 3,
            bandImports: Self.split,
            offpeakWindowStart: "10:00",
            offpeakWindowEnd: "15:00",
            offpeakIntegratedAt: "2026-08-16T05:00:00Z",
            offpeakSampleCount: 1500
        )
    }

    private func bandedDay() -> DayEnergy {
        DayEnergy(
            date: "2026-08-15",
            epv: 0, eInput: 23, eOutput: 15, eCharge: 0, eDischarge: 0,
            offpeakGridImportKwh: 3,
            bandImports: Self.split,
            offpeakWindowStart: "10:00",
            offpeakWindowEnd: "15:00",
            offpeakIntegratedAt: "2026-08-16T05:00:00Z",
            offpeakSampleCount: 1500,
            note: nil
        )
    }

    private func day(date: String, eInput: Double) -> DayEnergy {
        DayEnergy(
            date: date,
            epv: 0, eInput: eInput, eOutput: 0, eCharge: 0, eDischarge: 0,
            note: nil
        )
    }

    private func makePricingService(apiClient: MockCostWiringAPIClient) async -> PricingService {
        let service = PricingService()
        service.bind(apiClient: apiClient)
        try? await service.refresh()
        return service
    }

    private func makeDayDetail(
        date: String,
        apiClient: MockCostWiringAPIClient
    ) async throws -> (DayDetailViewModel, PricingService) {
        apiClient.plansToReturn = Self.plans
        let service = await makePricingService(apiClient: apiClient)
        return (DayDetailViewModel(date: date, apiClient: apiClient, pricingService: service), service)
    }

    private func makeHistory(
        apiClient: MockCostWiringAPIClient
    ) async throws -> (HistoryViewModel, PricingService) {
        apiClient.plansToReturn = Self.plans
        let service = await makePricingService(apiClient: apiClient)
        let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
        let container = try ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
        let context = ModelContext(container)
        let viewModel = HistoryViewModel(
            apiClient: apiClient,
            modelContext: context,
            pricingService: service
        )
        return (viewModel, service)
    }
}

// MARK: - test double

private final class MockCostWiringAPIClient: FluxAPIClient, @unchecked Sendable {
    var plansToReturn: [PricingPlan] = []
    var dayResponse: DayDetailResponse?
    var historyResponse: HistoryResponse?

    func fetchStatus() async throws -> StatusResponse {
        StatusResponse(live: nil, battery: nil, rolling15min: nil, offpeak: nil, todayEnergy: nil, note: nil)
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        guard let historyResponse else { throw FluxAPIError.notConfigured }
        return historyResponse
    }

    func fetchHistory(query _: HistoryQuery) async throws -> HistoryResponse {
        guard let historyResponse else { throw FluxAPIError.notConfigured }
        return historyResponse
    }

    func fetchDay(date: String) async throws -> DayDetailResponse {
        guard let dayResponse else {
            return DayDetailResponse(
                date: date, readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil
            )
        }
        return dayResponse
    }

    func saveNote(date: String, text _: String) async throws -> NoteResponse {
        NoteResponse(date: date, text: "", updatedAt: nil)
    }

    func fetchPricing() async throws -> [PricingPlan] { plansToReturn }
}
