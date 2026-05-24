import Foundation
import Testing
@testable import FluxCore

@Suite
struct PeriodCostsTests {
    // MARK: - Empty / nil / coverage cases

    @Test
    func emptyDaysReturnsNil() throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        #expect(PeriodCosts.compute(days: [], pricing: pricing) == nil)
    }

    @Test
    func zeroPricedDaysReturnsNil() throws {
        // Days exist but none is covered by any pricing period (AC 5.4).
        let pricing = [period(start: "2030-01-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let days = [
            day(date: "2026-04-15", eInput: 10, eOutput: 4, offpeakKwh: 3),
            day(date: "2026-04-16", eInput: 12, eOutput: 5, offpeakKwh: 2)
        ]
        #expect(PeriodCosts.compute(days: days, pricing: pricing) == nil)
    }

    @Test
    func fullCoverageNoCaption() throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let days = [
            day(date: "2026-04-15", eInput: 10, eOutput: 4, offpeakKwh: 3),
            day(date: "2026-04-16", eInput: 12, eOutput: 5, offpeakKwh: 2)
        ]
        let totals = try #require(PeriodCosts.compute(days: days, pricing: pricing))
        #expect(totals.pricedDayCount == 2)
        #expect(totals.totalDayCount == 2)
        #expect(!totals.hasPartialCoverage)
    }

    @Test
    func partialCoverageReportsCount() throws {
        let pricing = [period(start: "2026-04-01", end: "2026-04-30", peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let days = [
            day(date: "2026-04-15", eInput: 10, eOutput: 4, offpeakKwh: 3),
            day(date: "2026-05-01", eInput: 12, eOutput: 5, offpeakKwh: 2),
            day(date: "2026-05-02", eInput: 8, eOutput: 3, offpeakKwh: 1)
        ]
        let totals = try #require(PeriodCosts.compute(days: days, pricing: pricing))
        #expect(totals.pricedDayCount == 1)
        #expect(totals.totalDayCount == 3)
        #expect(totals.hasPartialCoverage)
    }

    @Test
    func totalsExcludeUnpricedDays() throws {
        let pricing = [period(start: "2026-04-01", end: "2026-04-30", peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let days = [
            day(date: "2026-04-15", eInput: 10, eOutput: 4, offpeakKwh: 3),
            day(date: "2026-05-01", eInput: 999, eOutput: 999, offpeakKwh: 999)
        ]
        let totals = try #require(PeriodCosts.compute(days: days, pricing: pricing))
        // Only the priced day contributes — peak kWh = 10 - 3 = 7.
        #expect(totals.peakImportsCost == 7 * 0.30)
        #expect(totals.solarFeedInIncome == 4 * 0.05)
        #expect(totals.offPeakSavings == 3 * 0.10)
        #expect(totals.net == 7 * 0.30 - 4 * 0.05)
    }

    // MARK: - Net invariant

    @Test
    func netEqualsSumOfPerDayNets() throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0.30, feedIn: 0.05, offPeak: 0.10)]
        let days = [
            day(date: "2026-04-15", eInput: 10, eOutput: 4, offpeakKwh: 3),
            day(date: "2026-04-16", eInput: 12, eOutput: 5, offpeakKwh: 2),
            day(date: "2026-04-17", eInput: 8, eOutput: 3, offpeakKwh: 1)
        ]
        let totals = try #require(PeriodCosts.compute(days: days, pricing: pricing))

        let perDayNets = days.compactMap { $0.costs(in: pricing)?.net }
        let summed = perDayNets.reduce(0, +)
        #expect(approximately(totals.net, summed))
    }

    // MARK: - Linearity (property-based)

    @Test(arguments: [
        (0.10, 1.0),
        (0.20, 5.0),
        (0.30, 12.5),
        (0.50, 0.0),
        (0.0001, 100.0),
        (1.0, 1.0)
    ])
    func costLinearityPerLine(rate: Double, kwh: Double) throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: rate, feedIn: rate, offPeak: rate)]
        let summary = DaySummary(
            epv: nil, eInput: kwh, eOutput: kwh,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: 0, offpeakGridExportKwh: nil
        )
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(approximately(costs.peakImportsCost, rate * kwh))
        #expect(approximately(costs.solarFeedInIncome, rate * kwh))
    }

    @Test(arguments: [0.0, 1.0, 10.0, 100.0, 1000.0])
    func zeroRateProducesZeroCost(kwh: Double) throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: 0, feedIn: 0, offPeak: 0)]
        let summary = DaySummary(
            epv: nil, eInput: kwh, eOutput: kwh,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: 0, offpeakGridExportKwh: nil
        )
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.peakImportsCost == 0)
        #expect(costs.solarFeedInIncome == 0)
        #expect(costs.offPeakSavings == 0)
        #expect(costs.net == 0)
    }

    @Test(arguments: [0.0, 0.05, 0.30, 1.0, 9.99])
    func zeroKwhProducesZeroCost(rate: Double) throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: rate, feedIn: rate, offPeak: rate)]
        let summary = DaySummary(
            epv: nil, eInput: 0, eOutput: 0,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: 0, offpeakGridExportKwh: nil
        )
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.peakImportsCost == 0)
        #expect(costs.solarFeedInIncome == 0)
        #expect(costs.offPeakSavings == 0)
    }

    @Test(arguments: [
        (0.10, 1.0, 2.0),
        (0.30, 5.0, 7.5),
        (0.0001, 1000.0, 0.5)
    ])
    func scalingRateScalesCostProportionally(rate: Double, kwh: Double, scale: Double) throws {
        let pricing = [period(start: "2026-04-01", end: nil, peak: rate, feedIn: 0, offPeak: 0)]
        let scaledPricing = [period(start: "2026-04-01", end: nil, peak: rate * scale, feedIn: 0, offPeak: 0)]
        let summary = DaySummary(
            epv: nil, eInput: kwh, eOutput: 0,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: 0, offpeakGridExportKwh: nil
        )
        let base = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        let scaled = try #require(summary.costs(forDate: "2026-04-15", in: scaledPricing))
        #expect(approximately(scaled.peakImportsCost, base.peakImportsCost * scale))
    }

    // MARK: - Overlap symmetry (property-based)

    @Test(arguments: [
        ("2026-01-01", "2026-06-30", "2026-04-01", "2026-12-31"),
        ("2026-01-01", "2026-06-30", "2026-07-01", "2026-12-31"),
        ("2026-01-01", "2026-12-31", "2026-04-01", "2026-04-30"),
        ("2026-01-01", "2026-06-30", "2026-06-30", "2026-12-31") // single-day overlap
    ])
    func overlapsIsSymmetric(aStart: String, aEnd: String, bStart: String, bEnd: String) throws {
        let alpha = period(id: "a", start: aStart, end: aEnd, peak: 0, feedIn: 0, offPeak: 0)
        let beta = period(id: "b", start: bStart, end: bEnd, peak: 0, feedIn: 0, offPeak: 0)
        #expect(rangesOverlap(alpha, beta) == rangesOverlap(beta, alpha))
    }

    @Test(arguments: [
        ("2026-01-01", "2026-06-30", "2026-04-01"),
        ("2026-01-01", nil, "2026-04-01"),
        ("2026-01-01", "2026-06-30", "2026-06-30")
    ])
    func overlapsWithOpenEnded(aStart: String, aEnd: String?, bStart: String) throws {
        // The right-hand range is open-ended; if its start is on or before
        // the left's end, the two overlap (and the relation is symmetric).
        let alpha = period(id: "a", start: aStart, end: aEnd, peak: 0, feedIn: 0, offPeak: 0)
        let beta = period(id: "b", start: bStart, end: nil, peak: 0, feedIn: 0, offPeak: 0)
        #expect(rangesOverlap(alpha, beta) == rangesOverlap(beta, alpha))
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

    private func day(date: String, eInput: Double, eOutput: Double, offpeakKwh: Double?) -> DayEnergy {
        DayEnergy(
            date: date,
            epv: 0, eInput: eInput, eOutput: eOutput,
            eCharge: 0, eDischarge: 0,
            offpeakGridImportKwh: offpeakKwh,
            offpeakGridExportKwh: nil,
            note: nil
        )
    }

    private func approximately(_ lhs: Double, _ rhs: Double, tolerance: Double = 1e-6) -> Bool {
        abs(lhs - rhs) < tolerance
    }

    /// Free function so we can test the relation directly.
    private func rangesOverlap(_ left: PricingPeriod, _ right: PricingPeriod) -> Bool {
        let leftEnd = left.endDate ?? "9999-12-31"
        let rightEnd = right.endDate ?? "9999-12-31"
        return left.startDate <= rightEnd && right.startDate <= leftEnd
    }
}
