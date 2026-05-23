import FluxCore
import Foundation
import Testing
@testable import Flux

/// Lightweight view-state tests for HistoryPeriodCostsCard. Asserts on the
/// static formatters that drive the tiles and caption, in lieu of the
/// snapshot framework (not wired up).
@MainActor @Suite
struct HistoryPeriodCostsCardTests {
    @Test
    func labelsMatchSpec() {
        #expect(HistoryPeriodCostsCard.label(for: .peakImportsCost) == "Peak imports")
        #expect(HistoryPeriodCostsCard.label(for: .solarFeedInIncome) == "Solar feed-in")
        #expect(HistoryPeriodCostsCard.label(for: .net) == "Net")
        #expect(HistoryPeriodCostsCard.label(for: .offPeakSavings) == "Off-peak savings")
    }

    @Test
    func tileValuesUseAUDFormatting() {
        let costs = PeriodCosts(
            peakImportsCost: 50.20,
            solarFeedInIncome: 12.50,
            net: 37.70,
            offPeakSavings: 8.40,
            pricedDayCount: 7,
            totalDayCount: 7
        )
        #expect(HistoryPeriodCostsCard.valueText(for: .peakImportsCost, costs: costs) == "$50.20")
        #expect(HistoryPeriodCostsCard.valueText(for: .solarFeedInIncome, costs: costs) == "$12.50")
        #expect(HistoryPeriodCostsCard.valueText(for: .net, costs: costs) == "$37.70")
        #expect(HistoryPeriodCostsCard.valueText(for: .offPeakSavings, costs: costs) == "$8.40")
    }

    @Test
    func fullCoverageHasNoCaption() {
        let costs = PeriodCosts(
            peakImportsCost: 1,
            solarFeedInIncome: 1,
            net: 0,
            offPeakSavings: 0,
            pricedDayCount: 7,
            totalDayCount: 7
        )
        #expect(HistoryPeriodCostsCard.captionText(costs: costs) == nil)
    }

    @Test
    func partialCoverageRendersNOfM() {
        let costs = PeriodCosts(
            peakImportsCost: 1,
            solarFeedInIncome: 1,
            net: 0,
            offPeakSavings: 0,
            pricedDayCount: 4,
            totalDayCount: 7
        )
        #expect(HistoryPeriodCostsCard.captionText(costs: costs) == "4 of 7 days priced")
    }

    @Test
    func partialCoverageBoundaryReportsCorrectCount() {
        // Single priced day in 30-day range — extreme partial coverage.
        let costs = PeriodCosts(
            peakImportsCost: 1,
            solarFeedInIncome: 0,
            net: 1,
            offPeakSavings: 0,
            pricedDayCount: 1,
            totalDayCount: 30
        )
        #expect(HistoryPeriodCostsCard.captionText(costs: costs) == "1 of 30 days priced")
    }

    @Test
    func negativeNetTileHasLeadingMinus() {
        let costs = PeriodCosts(
            peakImportsCost: 10,
            solarFeedInIncome: 15,
            net: -5,
            offPeakSavings: 0,
            pricedDayCount: 7,
            totalDayCount: 7
        )
        #expect(HistoryPeriodCostsCard.valueText(for: .net, costs: costs) == "−$5.00")
    }
}
