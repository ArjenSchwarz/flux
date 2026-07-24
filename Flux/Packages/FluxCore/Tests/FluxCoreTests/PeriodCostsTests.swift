import Foundation
import Testing
@testable import FluxCore

@Suite
struct PeriodCostsTests {
    // MARK: - Empty / nil / coverage cases

    @Test
    func emptyDaysReturnsNil() throws {
        let pricing = [plan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        #expect(PeriodCosts.compute(days: [], pricing: pricing) == nil)
    }

    @Test
    func zeroPricedDaysReturnsNil() throws {
        // Days exist but none is covered by any plan (AC 2.7).
        let pricing = [plan(start: "2030-01-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let days = [
            day(date: "2026-04-15", eInput: 10, eOutput: 4, offpeakKwh: 3),
            day(date: "2026-04-16", eInput: 12, eOutput: 5, offpeakKwh: 2)
        ]
        #expect(PeriodCosts.compute(days: days, pricing: pricing) == nil)
    }

    @Test
    func fullCoverageNoCaption() throws {
        let pricing = [plan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
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
        let pricing = [plan(start: "2026-04-01", end: "2026-05-01", rate: 0.30, feedIn: 0.05, savings: 0.10)]
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
        let pricing = [plan(start: "2026-04-01", end: "2026-05-01", rate: 0.30, feedIn: 0.05, savings: 0.10)]
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

    @Test
    func daysSpanningASwitchDateArePricedByTheirOwnPlan() throws {
        let pricing = [
            plan(id: "old", start: "2026-01-01", end: "2026-08-01", rate: 0.20, feedIn: 0.05, savings: 0.10),
            plan(id: "new", start: "2026-08-01", end: nil, rate: 0.40, feedIn: 0.05, savings: 0.10)
        ]
        let days = [
            day(date: "2026-07-31", eInput: 10, eOutput: 0, offpeakKwh: 0),
            day(date: "2026-08-01", eInput: 10, eOutput: 0, offpeakKwh: 0)
        ]
        let totals = try #require(PeriodCosts.compute(days: days, pricing: pricing))
        #expect(totals.pricedDayCount == 2)
        #expect(approximately(totals.peakImportsCost, 10 * 0.20 + 10 * 0.40))
    }

    // MARK: - Net invariant

    @Test
    func netEqualsSumOfPerDayNets() throws {
        let pricing = [plan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let days = [
            day(date: "2026-04-15", eInput: 10, eOutput: 4, offpeakKwh: 3),
            day(date: "2026-04-16", eInput: 12, eOutput: 5, offpeakKwh: 2),
            day(date: "2026-04-17", eInput: 8, eOutput: 3, offpeakKwh: 1)
        ]
        let totals = try #require(PeriodCosts.compute(days: days, pricing: pricing))

        let perDayNets = days.compactMap { $0.costs(in: pricing)?.net }
        #expect(approximately(totals.net, perDayNets.reduce(0, +)))
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
        let pricing = [plan(start: "2026-04-01", end: nil, rate: rate, feedIn: rate, savings: rate)]
        let summary = summary(eInput: kwh, eOutput: kwh, offpeakKwh: 0)
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(approximately(costs.peakImportsCost, rate * kwh))
        #expect(approximately(costs.solarFeedInIncome, rate * kwh))
    }

    @Test(arguments: [0.0, 1.0, 10.0, 100.0, 1000.0])
    func zeroRateProducesZeroCost(kwh: Double) throws {
        let pricing = [plan(start: "2026-04-01", end: nil, rate: 0, feedIn: 0, savings: 0)]
        let costs = try #require(summary(eInput: kwh, eOutput: kwh, offpeakKwh: 0)
            .costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.peakImportsCost == 0)
        #expect(costs.solarFeedInIncome == 0)
        #expect(costs.offPeakSavings == 0)
        #expect(costs.net == 0)
    }

    @Test(arguments: [0.0, 0.05, 0.30, 1.0, 9.99])
    func zeroKwhProducesZeroCost(rate: Double) throws {
        let pricing = [plan(start: "2026-04-01", end: nil, rate: rate, feedIn: rate, savings: rate)]
        let costs = try #require(summary(eInput: 0, eOutput: 0, offpeakKwh: 0)
            .costs(forDate: "2026-04-15", in: pricing))
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
        let base = [plan(start: "2026-04-01", end: nil, rate: rate, feedIn: 0, savings: 0)]
        let scaledPricing = [plan(start: "2026-04-01", end: nil, rate: rate * scale, feedIn: 0, savings: 0)]
        let summary = summary(eInput: kwh, eOutput: 0, offpeakKwh: 0)
        let baseCosts = try #require(summary.costs(forDate: "2026-04-15", in: base))
        let scaled = try #require(summary.costs(forDate: "2026-04-15", in: scaledPricing))
        #expect(approximately(scaled.peakImportsCost, baseCosts.peakImportsCost * scale))
    }

    // MARK: - Overlap symmetry (half-open, Decision 5)

    @Test(arguments: [
        ("2026-01-01", "2026-07-01", "2026-04-01", "2027-01-01"),
        ("2026-01-01", "2026-07-01", "2026-07-01", "2027-01-01"),
        ("2026-01-01", "2027-01-01", "2026-04-01", "2026-05-01"),
        ("2026-01-01", "2026-07-01", "2026-06-30", "2027-01-01")
    ])
    func overlapsIsSymmetric(aStart: String, aEnd: String, bStart: String, bEnd: String) throws {
        let alpha = plan(id: "a", start: aStart, end: aEnd, rate: 0, feedIn: 0, savings: 0)
        let beta = plan(id: "b", start: bStart, end: bEnd, rate: 0, feedIn: 0, savings: 0)
        #expect(rangesOverlap(alpha, beta) == rangesOverlap(beta, alpha))
    }

    @Test
    func abuttingRangesDoNotOverlapUnderExclusiveEnds() throws {
        // The whole point of Decision 5: "old ends 2026-08-01, new starts
        // 2026-08-01" is a clean succession, not an overlap.
        let alpha = plan(id: "a", start: "2026-01-01", end: "2026-08-01", rate: 0, feedIn: 0, savings: 0)
        let beta = plan(id: "b", start: "2026-08-01", end: nil, rate: 0, feedIn: 0, savings: 0)
        #expect(!rangesOverlap(alpha, beta))
        #expect(!rangesOverlap(beta, alpha))
    }

    @Test(arguments: [
        ("2026-01-01", "2026-07-01", "2026-04-01"),
        ("2026-01-01", nil, "2026-04-01"),
        ("2026-01-01", "2026-07-01", "2026-07-01")
    ])
    func overlapsWithOpenEnded(aStart: String, aEnd: String?, bStart: String) throws {
        let alpha = plan(id: "a", start: aStart, end: aEnd, rate: 0, feedIn: 0, savings: 0)
        let beta = plan(id: "b", start: bStart, end: nil, rate: 0, feedIn: 0, savings: 0)
        #expect(rangesOverlap(alpha, beta) == rangesOverlap(beta, alpha))
    }

    // MARK: - Helpers

    private func plan(
        id: String = "p",
        start: String,
        end: String?,
        rate: Double,
        feedIn: Double,
        savings: Double
    ) -> PricingPlan {
        PricingPlan(
            id: id,
            startDate: start,
            endDate: end,
            defaultRate: rate,
            windows: [PlanWindow(start: "11:00", end: "14:00", free: true, rate: nil)],
            feedInRate: feedIn,
            savingsReferenceRate: savings,
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

    private func summary(eInput: Double, eOutput: Double, offpeakKwh: Double?) -> DaySummary {
        DaySummary(
            epv: nil, eInput: eInput, eOutput: eOutput,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: offpeakKwh, offpeakGridExportKwh: nil
        )
    }

    private func approximately(_ lhs: Double, _ rhs: Double, tolerance: Double = 1e-6) -> Bool {
        abs(lhs - rhs) < tolerance
    }

    /// Half-open interval intersection — the relation the server's overlap
    /// check now uses (Decision 5).
    private func rangesOverlap(_ left: PricingPlan, _ right: PricingPlan) -> Bool {
        let leftEnd = left.endDate ?? "9999-12-31"
        let rightEnd = right.endDate ?? "9999-12-31"
        return left.startDate < rightEnd && right.startDate < leftEnd
    }
}
