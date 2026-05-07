import FluxCore
import Foundation
import Testing
@testable import Flux

// swiftlint:disable type_body_length

@MainActor @Suite
struct HistoryViewModelOverviewTests {
    // MARK: - Cohort fixtures

    @Test("(a) empty days array → every new field nil/zero")
    func emptyDaysProducesEmptySummary() {
        let derived = HistoryViewModel.DerivedState(days: [], now: now(month: 4, day: 16))
        let summary = derived.summary

        #expect(summary.nightTotalKwh == 0)
        #expect(summary.nightBlockDayCount == 0)
        #expect(summary.nightAvgKwh == nil)
        #expect(summary.mostUsageDay == nil)
        #expect(summary.mostSolarDay == nil)
        #expect(summary.lowestSocDay == nil)
    }

    @Test("(b) only-today: complete-day fields empty; Lowest SoC populated when today has socLow")
    func onlyTodayPopulatesOnlyLowestSoc() {
        let today = day(
            "2026-04-16", epv: 5.0, eInput: 1.0, eOutput: 0.5, eCharge: 1.0, eDischarge: 1.0,
            socLow: 22.7, socLowTime: "14:30:00",
            dailyUsage: usage(blocks: [(.night, 1.0), (.morningPeak, 1.5)])
        )
        let derived = HistoryViewModel.DerivedState(days: [today], now: now(month: 4, day: 16))
        let summary = derived.summary

        #expect(summary.nightTotalKwh == 0)
        #expect(summary.nightBlockDayCount == 0)
        #expect(summary.nightAvgKwh == nil)
        #expect(summary.mostUsageDay == nil)
        #expect(summary.mostSolarDay == nil)

        let record = try? #require(summary.lowestSocDay)
        #expect(record?.dayID == "2026-04-16")
        #expect(record?.soc == 22.7)
        #expect(record?.socLowTime == "14:30:00")
    }

    @Test("(b′) today + complete day with off-peak — peak imports and exports include today")
    func todayContributesToOffpeakAggregates() {
        let yesterday = day(
            "2026-04-15", epv: 8.0, eInput: 4.0, eOutput: 1.0, eCharge: 3.0, eDischarge: 2.0,
            offpeakGridImportKwh: 2.0
        )
        let today = day(
            "2026-04-16", epv: 6.0, eInput: 3.0, eOutput: 0.5, eCharge: 2.0, eDischarge: 1.5,
            offpeakGridImportKwh: 1.5
        )
        let derived = HistoryViewModel.DerivedState(days: [yesterday, today], now: now(month: 4, day: 16))
        let summary = derived.summary

        #expect(abs(summary.peakImportTotalKwh - ((4 - 2) + (3 - 1.5))) < 0.001)
        #expect(abs(summary.exportTotalKwh - (1.0 + 0.5)) < 0.001)
        #expect(summary.gridDayCount == 2)
    }

    @Test("(c) all complete days with full data — every tile populated; mostUsageDay equality")
    func allCompleteDaysWithFullData() {
        let april12 = day(
            "2026-04-12", epv: 9.0, eInput: 3.5, eOutput: 1.0, eCharge: 2.5, eDischarge: 2.0,
            offpeakGridImportKwh: 1.5,
            socLow: 35.0, socLowTime: "06:00:00",
            dailyUsage: usage(blocks: [(.night, 2.0), (.morningPeak, 1.0), (.evening, 1.5)])
        )
        let april13 = day(
            "2026-04-13", epv: 12.0, eInput: 5.0, eOutput: 2.0, eCharge: 3.0, eDischarge: 2.5,
            offpeakGridImportKwh: 2.0,
            socLow: 18.5, socLowTime: "07:30:00",
            dailyUsage: usage(blocks: [(.night, 3.0), (.morningPeak, 2.0), (.evening, 2.5)])
        )
        let april14 = day(
            "2026-04-14", epv: 10.0, eInput: 4.0, eOutput: 1.5, eCharge: 2.0, eDischarge: 1.5,
            offpeakGridImportKwh: 1.0,
            socLow: 22.0, socLowTime: "06:30:00",
            dailyUsage: usage(blocks: [(.night, 2.5), (.morningPeak, 1.5), (.evening, 2.0)])
        )

        let derived = HistoryViewModel.DerivedState(
            days: [april12, april13, april14], now: now(month: 4, day: 16)
        )
        let summary = derived.summary

        #expect(abs(summary.solarTotalKwh - 31.0) < 0.001)
        #expect(abs(summary.exportTotalKwh - 4.5) < 0.001)
        #expect(abs(summary.peakImportTotalKwh - ((3.5 - 1.5) + (5 - 2) + (4 - 1))) < 0.001)
        #expect(abs(summary.dailyUsageTotalKwh - (4.5 + 7.5 + 6.0)) < 0.001)

        // Avg night = (2.0 + 3.0 + 2.5) / 3 = 2.5
        #expect(summary.nightBlockDayCount == 3)
        #expect(abs(summary.nightTotalKwh - 7.5) < 0.001)
        #expect(abs((summary.nightAvgKwh ?? -1) - 2.5) < 0.001)

        // Most usage: april13 wins at 7.5 kWh
        let mostUsage = try? #require(summary.mostUsageDay)
        #expect(mostUsage?.dayID == "2026-04-13")
        #expect(abs((mostUsage?.kwh ?? 0) - 7.5) < 0.001)

        let expectedDate = parsedDay("2026-04-13")
        #expect(summary.mostUsageDay == HistoryViewModel.DayKwhRecord(
            dayID: "2026-04-13", date: expectedDate, kwh: 7.5
        ))

        // Most solar: april13 at 12.0
        #expect(summary.mostSolarDay?.dayID == "2026-04-13")
        #expect(abs((summary.mostSolarDay?.kwh ?? 0) - 12.0) < 0.001)

        // Lowest SoC: april13 at 18.5
        let lowest = try? #require(summary.lowestSocDay)
        #expect(lowest?.dayID == "2026-04-13")
        #expect(lowest?.soc == 18.5)
        #expect(lowest?.socLowTime == "07:30:00")
    }

    @Test("(d) only dailyUsage payload → Total usage / Most usage / Avg night non-nil")
    func onlyDailyUsagePopulatesUsageFields() {
        let only = day(
            "2026-04-14", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            dailyUsage: usage(blocks: [(.night, 1.5), (.evening, 2.0)])
        )
        let derived = HistoryViewModel.DerivedState(days: [only], now: now(month: 4, day: 16))
        let summary = derived.summary

        #expect(summary.dailyUsageDayCount == 1)
        #expect(abs(summary.dailyUsageTotalKwh - 3.5) < 0.001)
        #expect(summary.mostUsageDay?.dayID == "2026-04-14")
        #expect(abs((summary.mostUsageDay?.kwh ?? 0) - 3.5) < 0.001)
        #expect(summary.nightBlockDayCount == 1)
        #expect(abs((summary.nightAvgKwh ?? -1) - 1.5) < 0.001)
        #expect(summary.gridDayCount == 0)
        #expect(summary.lowestSocDay == nil)
    }

    // MARK: - Tie-break fixtures

    @Test("(e) ties for mostUsageDay → later date wins")
    func mostUsageTieBreaksToLaterDate() {
        let earlier = day(
            "2026-04-12", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            dailyUsage: usage(blocks: [(.night, 4.0)])
        )
        let later = day(
            "2026-04-14", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            dailyUsage: usage(blocks: [(.night, 4.0)])
        )
        let derived = HistoryViewModel.DerivedState(days: [earlier, later], now: now(month: 4, day: 16))
        #expect(derived.summary.mostUsageDay?.dayID == "2026-04-14")
    }

    @Test("(f) ties for mostSolarDay → later date wins")
    func mostSolarTieBreaksToLaterDate() {
        let earlier = day("2026-04-12", epv: 9.5, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0)
        let later = day("2026-04-14", epv: 9.5, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0)
        let derived = HistoryViewModel.DerivedState(days: [earlier, later], now: now(month: 4, day: 16))
        #expect(derived.summary.mostSolarDay?.dayID == "2026-04-14")
    }

    @Test("(g) ties for lowestSocDay (raw Double) → later date wins")
    func lowestSocTieBreaksToLaterDate() {
        let earlier = day(
            "2026-04-12", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            socLow: 11.7
        )
        let later = day(
            "2026-04-14", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            socLow: 11.7
        )
        let derived = HistoryViewModel.DerivedState(days: [earlier, later], now: now(month: 4, day: 16))
        #expect(derived.summary.lowestSocDay?.dayID == "2026-04-14")
    }

    // MARK: - Night-block fixtures

    @Test("(h) day-with-blocks lacks night → excluded from nightBlockDayCount")
    func dayWithoutNightBlockExcludedFromCount() {
        let withNight = day(
            "2026-04-13", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            dailyUsage: usage(blocks: [(.night, 2.0), (.evening, 1.0)])
        )
        let withoutNight = day(
            "2026-04-14", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            dailyUsage: usage(blocks: [(.morningPeak, 1.0), (.evening, 2.0)])
        )
        let derived = HistoryViewModel.DerivedState(
            days: [withNight, withoutNight], now: now(month: 4, day: 16)
        )
        let summary = derived.summary
        #expect(summary.nightBlockDayCount == 1)
        #expect(abs(summary.nightTotalKwh - 2.0) < 0.001)
        #expect(abs((summary.nightAvgKwh ?? -1) - 2.0) < 0.001)
    }

    @Test("(h′) night.totalKwh == 0 → still counts toward nightBlockDayCount")
    func zeroNightBlockStillCounts() {
        let zeroNight = day(
            "2026-04-13", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            dailyUsage: usage(blocks: [(.night, 0.0), (.evening, 1.0)])
        )
        let derived = HistoryViewModel.DerivedState(days: [zeroNight], now: now(month: 4, day: 16))
        let summary = derived.summary
        #expect(summary.nightBlockDayCount == 1)
        #expect(summary.nightTotalKwh == 0)
        #expect(summary.nightAvgKwh == 0)
    }

    @Test("(i) negative night.totalKwh → clamped to zero before summing")
    func negativeNightClampedToZero() {
        let negativeNight = day(
            "2026-04-13", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            dailyUsage: usage(blocks: [(.night, -1.5), (.evening, 1.0)])
        )
        let positiveNight = day(
            "2026-04-14", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            dailyUsage: usage(blocks: [(.night, 2.0), (.evening, 1.0)])
        )
        let derived = HistoryViewModel.DerivedState(
            days: [negativeNight, positiveNight], now: now(month: 4, day: 16)
        )
        // Clamped night summed: 0 + 2.0 = 2.0; both days are with-night-block (zero counts).
        #expect(derived.summary.nightBlockDayCount == 2)
        #expect(abs(derived.summary.nightTotalKwh - 2.0) < 0.001)
    }

    // MARK: - Peak / Exported fixtures

    @Test("(j) negative peakGridImportKwh payload → clamped to zero")
    func negativePeakClampedAtEntryConstruction() {
        // eInput < offpeakGridImport produces a negative peak before clamp.
        let negativePeak = day(
            "2026-04-14", epv: 0, eInput: 1.0, eOutput: 0, eCharge: 0, eDischarge: 0,
            offpeakGridImportKwh: 3.0
        )
        let derived = HistoryViewModel.DerivedState(days: [negativePeak], now: now(month: 4, day: 16))
        #expect(derived.summary.peakImportTotalKwh == 0)
        #expect(derived.summary.gridDayCount == 1)
    }

    @Test("(k) every day lacks offpeak → gridDayCount == 0 (peak/exported em-dash)")
    func noOffpeakProducesEmptyGridCohort() {
        let dayA = day("2026-04-13", epv: 5.0, eInput: 1.0, eOutput: 0.2, eCharge: 1.0, eDischarge: 0.5)
        let dayB = day("2026-04-14", epv: 6.0, eInput: 1.5, eOutput: 0.3, eCharge: 1.2, eDischarge: 0.6)
        let derived = HistoryViewModel.DerivedState(days: [dayA, dayB], now: now(month: 4, day: 16))
        #expect(derived.summary.gridDayCount == 0)
        #expect(derived.summary.peakImportTotalKwh == 0)
        #expect(derived.summary.exportTotalKwh == 0)
    }

    @Test("(l) day-with-offpeak with all peak imports zero → renders zero, not em-dash")
    func zeroPeakWithOffpeakStillCountsTowardCohort() {
        let dayA = day(
            "2026-04-14", epv: 5.0, eInput: 2.0, eOutput: 0.5, eCharge: 1.0, eDischarge: 0.5,
            offpeakGridImportKwh: 2.0
        )
        let derived = HistoryViewModel.DerivedState(days: [dayA], now: now(month: 4, day: 16))
        #expect(derived.summary.peakImportTotalKwh == 0)
        #expect(derived.summary.gridDayCount == 1)
    }

    @Test("(m) dailyUsage present but no offpeak → contributes to usage cohort, not grid")
    func dailyUsageWithoutOffpeakMixedCohort() {
        let mixed = day(
            "2026-04-14", epv: 4.0, eInput: 1.0, eOutput: 0.2, eCharge: 0.5, eDischarge: 0.3,
            dailyUsage: usage(blocks: [(.night, 1.5), (.evening, 2.0)])
        )
        let derived = HistoryViewModel.DerivedState(days: [mixed], now: now(month: 4, day: 16))
        let summary = derived.summary

        #expect(summary.dailyUsageDayCount == 1)
        #expect(abs(summary.dailyUsageTotalKwh - 3.5) < 0.001)
        #expect(summary.nightBlockDayCount == 1)
        #expect(summary.gridDayCount == 0)
        #expect(summary.peakImportTotalKwh == 0)
        #expect(summary.exportTotalKwh == 0)
    }

    // MARK: - SoC fixtures

    // Cohort (n) — `socPercent` half-up rounding boundaries — lives in
    // `HistoryStatsFormattersTests` since it's a formatter concern, not a Totals
    // accumulator concern. The (a)…(o) sequence intentionally skips (n) here.

    @Test("(o) socLow = NaN → lowestSocDay nil (isFinite guard)")
    func nanSocLowSkipped() {
        let nan = day(
            "2026-04-14", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            socLow: .nan
        )
        let valid = day(
            "2026-04-13", epv: 0, eInput: 0, eOutput: 0, eCharge: 0, eDischarge: 0,
            socLow: 25.0
        )
        let derived = HistoryViewModel.DerivedState(days: [valid, nan], now: now(month: 4, day: 16))
        let lowest = try? #require(derived.summary.lowestSocDay)
        #expect(lowest?.dayID == "2026-04-13")
    }

    // MARK: - Helpers

    private func now(month: Int, day: Int, hour: Int = 14, minute: Int = 30) -> Date {
        let calendar = Calendar(identifier: .gregorian)
        return calendar.date(from: DateComponents(
            timeZone: TimeZone(secondsFromGMT: 0),
            year: 2026, month: month, day: day - 1, hour: hour, minute: minute
        ))!
    }

    private func parsedDay(_ dayID: String) -> Date {
        DateFormatting.parseDayDate(dayID)!
    }

    // swiftlint:disable:next function_parameter_count
    private func day(
        _ dayID: String,
        epv: Double,
        eInput: Double,
        eOutput: Double,
        eCharge: Double,
        eDischarge: Double,
        offpeakGridImportKwh: Double? = nil,
        socLow: Double? = nil,
        socLowTime: String? = nil,
        dailyUsage: DailyUsage? = nil
    ) -> DayEnergy {
        DayEnergy(
            date: dayID,
            epv: epv,
            eInput: eInput,
            eOutput: eOutput,
            eCharge: eCharge,
            eDischarge: eDischarge,
            offpeakGridImportKwh: offpeakGridImportKwh,
            offpeakGridExportKwh: nil,
            note: nil,
            dailyUsage: dailyUsage,
            socLow: socLow,
            socLowTime: socLowTime,
            peakPeriods: nil
        )
    }

    private func usage(blocks: [(DailyUsageBlock.Kind, Double)]) -> DailyUsage {
        DailyUsage(blocks: blocks.map { kind, kwh in
            DailyUsageBlock(
                kind: kind,
                start: "2026-01-01T00:00:00Z",
                end: "2026-01-01T01:00:00Z",
                totalKwh: kwh,
                averageKwhPerHour: nil,
                percentOfDay: 0,
                status: .complete,
                boundarySource: .readings
            )
        })
    }
}

// swiftlint:enable type_body_length
