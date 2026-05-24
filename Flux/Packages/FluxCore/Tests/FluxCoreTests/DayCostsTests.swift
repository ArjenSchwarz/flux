import Foundation
import Testing
@testable import FluxCore

@Suite
struct DayCostsTests {
    // MARK: - DaySummary.costs(forDate:in:)

    @Test
    func costsHappyPathWithFullSplit() throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3)

        let costs = summary.costs(forDate: "2026-04-15", in: pricing)
        let unwrapped = try #require(costs)
        // peak imports kWh = 10 - 3 = 7
        #expect(unwrapped.peakImportsCost == 7 * 0.30)
        #expect(unwrapped.solarFeedInIncome == 4 * 0.05)
        #expect(unwrapped.offPeakSavings == 3 * 0.10)
        #expect(unwrapped.net == 7 * 0.30 - 4 * 0.05)
    }

    @Test
    func costsTreatsNilOffpeakSplitAsZeroAndBillsAllImportsAsPeak() throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: nil)

        let costs = summary.costs(forDate: "2026-04-15", in: pricing)
        let unwrapped = try #require(costs)
        // Decision 23: all 10 kWh peak when split is nil.
        #expect(unwrapped.peakImportsCost == 10 * 0.30)
        #expect(unwrapped.offPeakSavings == 0)
    }

    @Test
    func costsTreatsNilFieldsAsZero() throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let summary = DaySummary(
            epv: nil, eInput: nil, eOutput: nil,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: nil, offpeakGridExportKwh: nil
        )
        let costs = summary.costs(forDate: "2026-04-15", in: pricing)
        let unwrapped = try #require(costs)
        #expect(unwrapped.peakImportsCost == 0)
        #expect(unwrapped.solarFeedInIncome == 0)
        #expect(unwrapped.offPeakSavings == 0)
        #expect(unwrapped.net == 0)
    }

    @Test
    func costsZeroValuesProduceZeroLines() throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let summary = makeSummary(eInput: 0, eOutput: 0, offpeakKwh: 0)

        let costs = summary.costs(forDate: "2026-04-15", in: pricing)
        let unwrapped = try #require(costs)
        #expect(unwrapped.peakImportsCost == 0)
        #expect(unwrapped.solarFeedInIncome == 0)
        #expect(unwrapped.offPeakSavings == 0)
        #expect(unwrapped.net == 0)
    }

    @Test
    func costsReturnsNilWhenDateNotCoveredByAnyPeriod() throws {
        let pricing = [period(start: "2026-04-01", end: "2026-04-30", peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3)

        #expect(summary.costs(forDate: "2026-05-01", in: pricing) == nil)
        #expect(summary.costs(forDate: "2026-03-31", in: pricing) == nil)
    }

    @Test
    func costsPicksTheCoveringPeriodWhenMultiplePresent() throws {
        let pricing = [
            period(id: "old", start: "2026-01-01", end: "2026-03-31", peak: 0.20, feedIn: 0.04, offPeak: 0.08),
            period(id: "new", start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.06, offPeak: 0.12)
        ]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3)

        let aprilCosts = summary.costs(forDate: "2026-04-15", in: pricing)
        #expect(aprilCosts?.peakImportsCost == 7 * 0.30)

        let marchCosts = summary.costs(forDate: "2026-03-15", in: pricing)
        #expect(marchCosts?.peakImportsCost == 7 * 0.20)
    }

    @Test
    func netExcludesOffPeakSavings() throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 1.00)]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3)
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.net == 7 * 0.30 - 4 * 0.05)
        #expect(costs.offPeakSavings == 3 * 1.00)
    }

    @Test
    func peakImportsKwhClampedAtZeroWhenOffpeakExceedsEInput() throws {
        // Off-peak should never exceed eInput in real data, but the
        // computation must not produce a negative peak-imports value.
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let summary = makeSummary(eInput: 2, eOutput: 0, offpeakKwh: 5)
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.peakImportsCost == 0)
    }

    // MARK: - DayEnergy.costs(in:)

    @Test
    func dayEnergyForwardsToDaySummaryExtension() throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let day = DayEnergy(
            date: "2026-04-15",
            epv: 0, eInput: 10, eOutput: 4,
            eCharge: 0, eDischarge: 0,
            offpeakGridImportKwh: 3, offpeakGridExportKwh: nil,
            note: nil
        )
        let costs = try #require(day.costs(in: pricing))
        #expect(costs.peakImportsCost == 7 * 0.30)
        #expect(costs.solarFeedInIncome == 4 * 0.05)
        #expect(costs.offPeakSavings == 3 * 0.10)
    }

    @Test
    func dayEnergyReturnsNilWhenDateNotCovered() throws {
        let pricing = [period(start: "2026-04-01", end: "2026-04-30", peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let day = DayEnergy(
            date: "2026-05-15",
            epv: 0, eInput: 10, eOutput: 4,
            eCharge: 0, eDischarge: 0,
            offpeakGridImportKwh: 3,
            offpeakGridExportKwh: nil,
            note: nil
        )
        #expect(day.costs(in: pricing) == nil)
    }

    @Test
    func emptyPricingArrayReturnsNil() throws {
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3)
        #expect(summary.costs(forDate: "2026-04-15", in: []) == nil)
    }

    // MARK: - helpers

    private func period(
        id: String = "p",
        start: String,
        end: String?,
        peak: Double,
        feedIn: Double,
        offPeak: Double
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

    private func makeSummary(eInput: Double?, eOutput: Double?, offpeakKwh: Double?) -> DaySummary {
        DaySummary(
            epv: nil,
            eInput: eInput,
            eOutput: eOutput,
            eCharge: nil,
            eDischarge: nil,
            socLow: nil,
            socLowTime: nil,
            offpeakGridImportKwh: offpeakKwh,
            offpeakGridExportKwh: nil
        )
    }
}
