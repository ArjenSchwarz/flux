import FluxCore
import Foundation
import Testing
@testable import Flux

/// Lightweight view-state tests for PricingPeriodsView. The project has no
/// snapshot-test framework wired up (per the task brief), so these tests
/// assert on the static helpers that drive the view's text content rather
/// than rendering view trees.
@MainActor @Suite
struct PricingPeriodsViewTests {
    @Test
    func closedPeriodFormatsAsStartEnd() {
        let period = makePeriod(start: "2026-01-01", end: "2026-06-30")
        #expect(PricingPeriodsView.dateRangeText(for: period) == "2026-01-01 – 2026-06-30")
    }

    @Test
    func openEndedPeriodFormatsAsFromStart() {
        let period = makePeriod(start: "2026-07-01", end: nil)
        #expect(PricingPeriodsView.dateRangeText(for: period) == "from 2026-07-01")
    }

    @Test
    func rateSummaryUsesFourDecimalPlaces() {
        let period = makePeriod(start: "2026-01-01", end: nil, peak: 0.2873, feedIn: 0.05, offPeak: 0.1234)
        let summary = PricingPeriodsView.rateSummary(for: period)
        #expect(summary.contains("$0.2873"))
        #expect(summary.contains("$0.0500"))
        #expect(summary.contains("$0.1234"))
    }

    @Test
    func formatRateAlwaysShowsFourDecimalPlaces() {
        #expect(PricingPeriodsView.formatRate(0.05) == "$0.0500")
        #expect(PricingPeriodsView.formatRate(0.123456) == "$0.1235",
                "rounds to 4 decimal places")
        #expect(PricingPeriodsView.formatRate(0) == "$0.0000")
    }

    @Test
    func emptyStateCopyNamesUnlockedFeatures() {
        // AC 3.8 — empty state names what the feature unlocks.
        let detail = PricingPeriodsView.emptyStateDetail
        #expect(detail.contains("Day Detail"))
        #expect(detail.contains("History"))
    }

    // MARK: - sorted state via ViewModel

    @Test
    func viewModelExposesPeriodsSortedAscending() async {
        let apiClient = TestPricingAPIClient()
        apiClient.periodsToReturn = [
            makePeriod(id: "newer", start: "2026-07-01", end: nil),
            makePeriod(id: "older", start: "2026-01-01", end: "2026-06-30")
        ]
        let service = PricingService()
        service.bind(apiClient: apiClient)
        let viewModel = PricingViewModel(service: service)
        await viewModel.refresh()
        #expect(viewModel.periods.map(\.id) == ["older", "newer"])
    }

    // MARK: - helpers

    private func makePeriod(
        id: String = "p",
        start: String,
        end: String?,
        peak: Double = 0.30,
        feedIn: Double = 0.05,
        offPeak: Double = 0.12
    ) -> PricingPeriod {
        PricingPeriod(
            id: id,
            startDate: start,
            endDate: end,
            peakRate: peak,
            feedInRate: feedIn,
            offPeakSavingsRate: offPeak,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
        )
    }
}
