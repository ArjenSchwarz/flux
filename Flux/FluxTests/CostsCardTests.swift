import FluxCore
import Foundation
import Testing
@testable import Flux

/// Lightweight view-state tests for CostsCard. Asserts on the static
/// formatters that drive the card's rendered text — in lieu of the snapshot
/// framework, which is not wired up in this project.
@MainActor @Suite
struct CostsCardTests {
    @Test
    func labelsMatchSpec() {
        #expect(CostsCard.label(for: .peakImportsCost) == "Peak imports cost")
        #expect(CostsCard.label(for: .solarFeedInIncome) == "Solar feed-in income")
        #expect(CostsCard.label(for: .net) == "Net")
        #expect(CostsCard.label(for: .offPeakSavings) == "Off-peak savings")
    }

    @Test
    func formatAUDUsesTwoDecimalPlaces() {
        #expect(CostsCard.formatAUD(3.42) == "$3.42")
        #expect(CostsCard.formatAUD(0) == "$0.00")
        #expect(CostsCard.formatAUD(0.5) == "$0.50")
    }

    @Test
    func formatAUDUsesLeadingMinusForNegativeValues() {
        // Note: U+2212 "MINUS SIGN" — not a hyphen — per AC 4.7.
        #expect(CostsCard.formatAUD(-3.42) == "−$3.42")
    }

    @Test
    func formatAUDRoundsToCents() {
        #expect(CostsCard.formatAUD(3.4249) == "$3.42")
        #expect(CostsCard.formatAUD(3.425) == "$3.43")
    }

    @Test
    func formatAUDTreatsTinyNegativesAsZero() {
        // -0.00 must not render as "−$0.00" — rounding noise on near-zero
        // values shouldn't flip the leading minus on.
        #expect(CostsCard.formatAUD(-0.001) == "$0.00")
    }

    // MARK: - Row value text

    @Test
    func positiveNetRowsRenderAsAUD() {
        let costs = DayCosts(
            peakImportsCost: 5.20,
            solarFeedInIncome: 1.50,
            net: 3.70,
            offPeakSavings: 0.80
        )
        #expect(CostsCard.valueText(for: .peakImportsCost, costs: costs) == "$5.20")
        #expect(CostsCard.valueText(for: .solarFeedInIncome, costs: costs) == "$1.50")
        #expect(CostsCard.valueText(for: .net, costs: costs) == "$3.70")
        #expect(CostsCard.valueText(for: .offPeakSavings, costs: costs) == "$0.80")
    }

    @Test
    func negativeNetRowRendersWithLeadingMinus() {
        let costs = DayCosts(
            peakImportsCost: 1.20,
            solarFeedInIncome: 3.50,
            net: -2.30,
            offPeakSavings: 0
        )
        #expect(CostsCard.valueText(for: .net, costs: costs) == "−$2.30")
    }

    @Test
    func zeroOffPeakSavingsRendersAsZero() {
        let costs = DayCosts(peakImportsCost: 0, solarFeedInIncome: 0, net: 0, offPeakSavings: 0)
        #expect(CostsCard.valueText(for: .offPeakSavings, costs: costs) == "$0.00")
    }
}
