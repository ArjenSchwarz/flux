import Foundation
import Testing
@testable import FluxCore

@Suite
struct DayCostsTests {
    // MARK: - Tier 2: the pre-band formula, verbatim (Q30)

    @Test
    func singleRatePlanUsesTheLegacyFormulaWithTheOffpeakResidual() throws {
        let pricing = [singleRatePlan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3)

        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.tier == .singleRate)
        // peak imports kWh = 10 - 3 = 7
        #expect(costs.peakImportsCost == 7 * 0.30)
        #expect(costs.solarFeedInIncome == 4 * 0.05)
        #expect(costs.offPeakSavings == 3 * 0.10)
        #expect(costs.net == 7 * 0.30 - 4 * 0.05)
    }

    @Test
    func nilOffpeakSplitBillsAllImportsAtThePlanRateWithNoSavings() throws {
        let pricing = [singleRatePlan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: nil)

        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.peakImportsCost == 10 * 0.30)
        #expect(costs.offPeakSavings == 0)
    }

    @Test
    func nilEnergyFieldsAreTreatedAsZeroAndStillPriceTheDay() throws {
        let pricing = [singleRatePlan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let summary = DaySummary(
            epv: nil, eInput: nil, eOutput: nil,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil
        )
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.peakImportsCost == 0)
        #expect(costs.solarFeedInIncome == 0)
        #expect(costs.offPeakSavings == 0)
        #expect(costs.net == 0)
    }

    @Test
    func serverPeakIsPreferredOverTheResidual() throws {
        // Server peak (6.5) deliberately differs from the residual
        // eInput − offpeak (10 − 3 = 7), so this proves the measured value wins.
        let pricing = [singleRatePlan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3, peakKwh: 6.5)

        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.peakImportsCost == 6.5 * 0.30)
        // Savings still price the measured off-peak kWh unchanged.
        #expect(costs.offPeakSavings == 3 * 0.10)
        #expect(costs.net == 6.5 * 0.30 - 4 * 0.05)
    }

    @Test
    func peakKwhIsClampedAtZeroWhenOffpeakExceedsEInput() throws {
        let pricing = [singleRatePlan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let summary = makeSummary(eInput: 2, eOutput: 0, offpeakKwh: 5)
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.peakImportsCost == 0)
    }

    @Test
    func aSingleRatePlanNeverReachesTheFallbackTier() throws {
        // No split, no server peak, no off-peak row — tier 2 still resolves.
        let pricing = [singleRatePlan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let summary = makeSummary(eInput: 10, eOutput: 0, offpeakKwh: nil)
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.tier == .singleRate)
    }

    // MARK: - Tier 1: the stored band split

    @Test
    func bandedPathPricesEachRatedBandAtItsOwnRate() throws {
        let pricing = [touPlan(start: "2026-08-01", end: nil)]
        let summary = makeSummary(
            eInput: 23, eOutput: 15, offpeakKwh: 3,
            offpeakWindowStart: "10:00", offpeakWindowEnd: "15:00",
            offpeakIntegratedAt: "2026-08-02T05:00:00Z", offpeakSampleCount: 1500,
            bandImports: [
                BandImport(start: "00:00", end: "01:00", kwh: 1),
                BandImport(start: "01:00", end: "06:00", kwh: 4),
                BandImport(start: "06:00", end: "10:00", kwh: 2),
                BandImport(start: "15:00", end: "24:00", kwh: 8)
            ]
        )
        let costs = try #require(summary.costs(forDate: "2026-08-15", in: pricing))
        #expect(costs.tier == .banded)
        #expect(abs(costs.peakImportsCost - (1 * 0.35 + 4 * 0.28 + 2 * 0.35 + 8 * 0.35)) < 1e-9)
        #expect(abs(costs.offPeakSavings - 3 * 0.35) < 1e-9)
    }

    @Test
    func aSparseCompleteOffpeakRowCannotPriceTheFreeBand() throws {
        // integratedAt set with no samples is a zero-delta artifact, not a
        // measured zero, so the free import is unresolvable.
        let pricing = [touPlan(start: "2026-08-01", end: nil)]
        let summary = makeSummary(
            eInput: 23, eOutput: 15, offpeakKwh: 0,
            offpeakWindowStart: "10:00", offpeakWindowEnd: "15:00",
            offpeakIntegratedAt: "2026-08-02T05:00:00Z", offpeakSampleCount: 0,
            bandImports: [
                BandImport(start: "00:00", end: "01:00", kwh: 1),
                BandImport(start: "01:00", end: "06:00", kwh: 4),
                BandImport(start: "06:00", end: "10:00", kwh: 2),
                BandImport(start: "15:00", end: "24:00", kwh: 8)
            ]
        )
        let costs = try #require(summary.costs(forDate: "2026-08-15", in: pricing))
        #expect(costs.tier == .fallback)
    }

    @Test
    func aPartiallyKnownSplitIsUnavailableNotPartiallyUsed() throws {
        let pricing = [touPlan(start: "2026-08-01", end: nil)]
        let summary = makeSummary(
            eInput: 23, eOutput: 15, offpeakKwh: 3,
            offpeakWindowStart: "10:00", offpeakWindowEnd: "15:00",
            offpeakIntegratedAt: "2026-08-02T05:00:00Z", offpeakSampleCount: 1500,
            bandImports: [
                BandImport(start: "00:00", end: "01:00", kwh: 1),
                BandImport(start: "01:00", end: "06:00", kwh: 4)
            ]
        )
        let costs = try #require(summary.costs(forDate: "2026-08-15", in: pricing))
        #expect(costs.tier == .fallback)
        #expect(abs(costs.peakImportsCost - 23 * 0.35) < 1e-9)
        #expect(costs.offPeakSavings == 0)
    }

    @Test
    func anOffpeakRowWithoutGeometryIsTreatedAsTheLegacyWindow() throws {
        // Pre-feature rows carry no snapshot; 11:00–14:00 is the only window
        // they can have been computed under, so they match a migrated plan.
        let pricing = [migratedPlan(start: "2026-04-01", end: nil)]
        let summary = makeSummary(
            eInput: 20, eOutput: 15, offpeakKwh: 6,
            bandImports: [
                BandImport(start: "00:00", end: "11:00", kwh: 5),
                BandImport(start: "14:00", end: "24:00", kwh: 9)
            ]
        )
        let costs = try #require(summary.costs(forDate: "2026-04-15", in: pricing))
        #expect(costs.tier == .banded)
        #expect(abs(costs.peakImportsCost - 14 * 0.35) < 1e-9)
    }

    @Test
    func aPlanWithoutAFreeBandNeedsNoOffpeakRow() throws {
        let plan = PricingPlan(
            id: "p", startDate: "2026-08-01", endDate: nil,
            defaultRate: 0.35,
            windows: [PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28)],
            feedInRate: 0.05, savingsReferenceRate: nil,
            createdAt: Date(timeIntervalSince1970: 0), updatedAt: Date(timeIntervalSince1970: 0)
        )
        let summary = makeSummary(
            eInput: 15, eOutput: 15, offpeakKwh: nil,
            bandImports: [
                BandImport(start: "00:00", end: "01:00", kwh: 1),
                BandImport(start: "01:00", end: "06:00", kwh: 4),
                BandImport(start: "06:00", end: "24:00", kwh: 10)
            ]
        )
        let costs = try #require(summary.costs(forDate: "2026-08-15", in: [plan]))
        #expect(costs.tier == .banded)
        #expect(costs.offPeakSavings == 0)
    }

    // MARK: - Tier 3: the fallback (AC 3.5 / 3.6)

    @Test
    func aMultiRatePlanWithNoSplitPricesEverythingAtTheHighestRate() throws {
        let pricing = [touPlan(start: "2026-08-01", end: nil)]
        let summary = makeSummary(eInput: 23, eOutput: 15, offpeakKwh: 3)
        let costs = try #require(summary.costs(forDate: "2026-08-15", in: pricing))
        #expect(costs.tier == .fallback)
        #expect(abs(costs.peakImportsCost - 23 * 0.35) < 1e-9)
        #expect(costs.offPeakSavings == 0)
    }

    @Test
    func aSplitCapturedUnderTheOldWindowDegradesToTheFallback() throws {
        let pricing = [touPlan(start: "2026-08-01", end: nil)]
        let summary = makeSummary(
            eInput: 23, eOutput: 15, offpeakKwh: 3,
            offpeakWindowStart: "10:00", offpeakWindowEnd: "15:00",
            offpeakIntegratedAt: "2026-08-02T05:00:00Z", offpeakSampleCount: 1500,
            bandImports: [
                BandImport(start: "00:00", end: "01:00", kwh: 1),
                BandImport(start: "01:00", end: "06:00", kwh: 4),
                BandImport(start: "06:00", end: "11:00", kwh: 2.5),
                BandImport(start: "14:00", end: "24:00", kwh: 9)
            ]
        )
        let costs = try #require(summary.costs(forDate: "2026-08-15", in: pricing))
        #expect(costs.tier == .fallback)
    }

    // MARK: - Plan coverage

    @Test
    func returnsNilWhenNoPlanCoversTheDate() throws {
        let pricing = [singleRatePlan(start: "2026-04-01", end: "2026-05-01", rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3)

        #expect(summary.costs(forDate: "2026-05-01", in: pricing) == nil)
        #expect(summary.costs(forDate: "2026-03-31", in: pricing) == nil)
        #expect(summary.costs(forDate: "2026-04-30", in: pricing) != nil)
    }

    @Test
    func switchDayIsPricedByTheSuccessor() throws {
        let pricing = [
            singleRatePlan(id: "old", start: "2026-01-01", end: "2026-08-01", rate: 0.20, feedIn: 0.04, savings: 0.08),
            singleRatePlan(id: "new", start: "2026-08-01", end: nil, rate: 0.30, feedIn: 0.06, savings: 0.12)
        ]
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3)

        #expect(summary.costs(forDate: "2026-07-31", in: pricing)?.peakImportsCost == 7 * 0.20)
        #expect(summary.costs(forDate: "2026-08-01", in: pricing)?.peakImportsCost == 7 * 0.30)
    }

    @Test
    func emptyPlanListReturnsNil() throws {
        let summary = makeSummary(eInput: 10, eOutput: 4, offpeakKwh: 3)
        #expect(summary.costs(forDate: "2026-04-15", in: []) == nil)
    }

    // MARK: - DayEnergy.costs(in:)

    @Test
    func dayEnergyForwardsEveryCostInputIntoTheHelper() throws {
        let pricing = [singleRatePlan(start: "2026-04-01", end: nil, rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let day = DayEnergy(
            date: "2026-04-15",
            epv: 0, eInput: 10, eOutput: 4,
            eCharge: 0, eDischarge: 0,
            offpeakGridImportKwh: 3, offpeakGridExportKwh: nil,
            peakGridImportKwh: 6.5,
            note: nil
        )
        let costs = try #require(day.costs(in: pricing))
        #expect(costs.peakImportsCost == 6.5 * 0.30)
        #expect(costs.solarFeedInIncome == 4 * 0.05)
        #expect(costs.offPeakSavings == 3 * 0.10)
    }

    @Test
    func dayEnergyForwardsTheBandSplit() throws {
        let pricing = [touPlan(start: "2026-08-01", end: nil)]
        let day = DayEnergy(
            date: "2026-08-15",
            epv: 0, eInput: 23, eOutput: 15,
            eCharge: 0, eDischarge: 0,
            offpeakGridImportKwh: 3, offpeakGridExportKwh: nil,
            peakGridImportKwh: nil,
            bandImports: [
                BandImport(start: "00:00", end: "01:00", kwh: 1),
                BandImport(start: "01:00", end: "06:00", kwh: 4),
                BandImport(start: "06:00", end: "10:00", kwh: 2),
                BandImport(start: "15:00", end: "24:00", kwh: 8)
            ],
            offpeakWindowStart: "10:00",
            offpeakWindowEnd: "15:00",
            offpeakIntegratedAt: "2026-08-16T05:00:00Z",
            offpeakSampleCount: 1500,
            note: nil
        )
        let costs = try #require(day.costs(in: pricing))
        #expect(costs.tier == .banded)
    }

    @Test
    func dayEnergyReturnsNilWhenDateNotCovered() throws {
        let pricing = [singleRatePlan(start: "2026-04-01", end: "2026-05-01", rate: 0.30, feedIn: 0.05, savings: 0.10)]
        let day = DayEnergy(
            date: "2026-05-15",
            epv: 0, eInput: 10, eOutput: 4,
            eCharge: 0, eDischarge: 0,
            offpeakGridImportKwh: 3, offpeakGridExportKwh: nil,
            note: nil
        )
        #expect(day.costs(in: pricing) == nil)
    }

    // MARK: - Helpers

    private func singleRatePlan(
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

    /// The shape every migrated legacy period takes: free 11:00–14:00 plus one
    /// flat rate for the rest of the day (AC 5.1).
    private func migratedPlan(start: String, end: String?) -> PricingPlan {
        singleRatePlan(start: start, end: end, rate: 0.35, feedIn: 0.05, savings: 0.35)
    }

    /// The incoming time-of-use plan: free 10:00–15:00, cheaper 01:00–06:00.
    private func touPlan(start: String, end: String?) -> PricingPlan {
        PricingPlan(
            id: "tou",
            startDate: start,
            endDate: end,
            defaultRate: 0.35,
            windows: [
                PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
                PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28)
            ],
            feedInRate: 0.05,
            savingsReferenceRate: 0.35,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
        )
    }

    private func makeSummary(
        eInput: Double?,
        eOutput: Double?,
        offpeakKwh: Double?,
        peakKwh: Double? = nil,
        offpeakWindowStart: String? = nil,
        offpeakWindowEnd: String? = nil,
        offpeakIntegratedAt: String? = nil,
        offpeakSampleCount: Int? = nil,
        bandImports: [BandImport]? = nil
    ) -> DaySummary {
        DaySummary(
            epv: nil,
            eInput: eInput,
            eOutput: eOutput,
            eCharge: nil,
            eDischarge: nil,
            socLow: nil,
            socLowTime: nil,
            offpeakGridImportKwh: offpeakKwh,
            offpeakGridExportKwh: nil,
            peakGridImportKwh: peakKwh,
            bandImports: bandImports,
            offpeakWindowStart: offpeakWindowStart,
            offpeakWindowEnd: offpeakWindowEnd,
            offpeakIntegratedAt: offpeakIntegratedAt,
            offpeakSampleCount: offpeakSampleCount
        )
    }
}
