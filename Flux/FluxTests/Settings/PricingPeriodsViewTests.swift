import FluxCore
import Foundation
import Testing
@testable import Flux

/// Lightweight view-state tests for PricingPeriodsView. The project has no
/// snapshot-test framework wired up, so these tests assert on the static
/// helpers that drive the view's text content rather than rendering view trees.
@MainActor @Suite
struct PricingPeriodsViewTests {
    @Test
    func closedPlanFormatsAsStartUntilEnd() {
        // The end date is exclusive (Decision 5), so the range reads as
        // "until" rather than an inclusive dash range that would look like
        // the plan prices the switch day.
        let plan = makePlan(start: "2026-01-01", end: "2026-08-01")
        #expect(PricingPeriodsView.dateRangeText(for: plan) == "2026-01-01 until 2026-08-01")
    }

    @Test
    func openEndedPlanFormatsAsFromStart() {
        let plan = makePlan(start: "2026-07-01", end: nil)
        #expect(PricingPeriodsView.dateRangeText(for: plan) == "from 2026-07-01")
    }

    // MARK: - Band summary (AC 6.1)

    @Test
    func bandSummaryListsTheFreeWindowThenExceptionsThenTheDefault() {
        let plan = makePlan(
            start: "2026-08-01",
            end: nil,
            defaultRate: 0.35,
            windows: [
                PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
                PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28)
            ],
            savings: 0.35
        )
        let summary = PricingPeriodsView.bandSummary(for: plan)
        #expect(summary == "Free 10:00–15:00 · $0.2800 01:00–06:00 · $0.3500 default")
    }

    @Test
    func bandSummaryOfAFlatPlanIsJustTheDefaultRate() {
        let plan = makePlan(start: "2026-01-01", end: nil, defaultRate: 0.35, windows: [], savings: nil)
        #expect(PricingPeriodsView.bandSummary(for: plan) == "$0.3500 default")
    }

    @Test
    func bandSummaryOfAMigratedPlanNamesItsFreeWindow() {
        let plan = makePlan(
            start: "2026-01-01",
            end: nil,
            defaultRate: 0.2873,
            windows: [PlanWindow(start: "11:00", end: "14:00", free: true, rate: nil)],
            savings: 0.2873
        )
        #expect(PricingPeriodsView.bandSummary(for: plan) == "Free 11:00–14:00 · $0.2873 default")
    }

    @Test
    func formatRateAlwaysShowsFourDecimalPlaces() {
        #expect(PricingPeriodsView.formatRate(0.05) == "$0.0500")
        #expect(PricingPeriodsView.formatRate(0.123456) == "$0.1235", "rounds to 4 decimal places")
        #expect(PricingPeriodsView.formatRate(0) == "$0.0000")
    }

    @Test
    func feedInSummaryIsShownSeparatelyFromTheImportBands() {
        let plan = makePlan(start: "2026-01-01", end: nil, defaultRate: 0.35, windows: [], savings: nil)
        #expect(PricingPeriodsView.feedInSummary(for: plan).contains("$0.0500"))
    }

    @Test
    func emptyStateCopyNamesUnlockedFeatures() {
        let detail = PricingPeriodsView.emptyStateDetail
        #expect(detail.contains("Day Detail"))
        #expect(detail.contains("History"))
    }

    // MARK: - sorted state via ViewModel

    @Test
    func viewModelExposesPlansSortedAscending() async {
        let apiClient = TestPricingAPIClient()
        apiClient.plansToReturn = [
            makePlan(id: "newer", start: "2026-07-01", end: nil),
            makePlan(id: "older", start: "2026-01-01", end: "2026-07-01")
        ]
        let service = PricingService()
        service.bind(apiClient: apiClient)
        let viewModel = PricingViewModel(service: service)
        await viewModel.refresh()
        #expect(viewModel.plans.map(\.id) == ["older", "newer"])
    }

    // MARK: - helpers

    private func makePlan(
        id: String = "p",
        start: String,
        end: String?,
        defaultRate: Double = 0.30,
        windows: [PlanWindow] = [PlanWindow(start: "11:00", end: "14:00", free: true, rate: nil)],
        savings: Double? = 0.12
    ) -> PricingPlan {
        PricingPlan(
            id: id,
            startDate: start,
            endDate: end,
            defaultRate: defaultRate,
            windows: windows,
            feedInRate: 0.05,
            savingsReferenceRate: savings,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
        )
    }
}
