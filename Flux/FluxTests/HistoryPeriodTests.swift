import FluxCore
import Foundation
import Testing
@testable import Flux

/// Property-based and unit tests for `HistoryPeriod`. All inputs are seeded
/// (never `Date.now`) so results are deterministic regardless of when or where
/// the suite runs — the T-1361 `DateBoundaryTests` style. Seeds span both
/// Sydney DST transitions, leap/non-leap February, and month-length edges,
/// plus deterministic pseudo-random dates from a fixed-seed generator.
@Suite struct HistoryPeriodTests {
    // MARK: - Seeded dates

    /// Hand-picked edge dates: DST transitions, month lengths, year edges.
    static let edgeSeeds: [Date] = {
        let seeds: [(Int, Int, Int, Int, Int)] = [
            (2026, 1, 1, 0, 0),     // Jan 1 (year edge)
            (2026, 2, 28, 23, 59),  // Feb 28 non-leap last day
            (2024, 2, 29, 12, 0),   // Feb 29 leap day
            (2026, 3, 31, 6, 30),   // Mar 31 (31-day month)
            (2026, 4, 5, 12, 0),    // Apr 5 DST-end day (25-hour day)
            (2026, 4, 15, 13, 37),  // mid-April
            (2026, 6, 1, 0, 1),     // Jun 1 (month start)
            (2026, 6, 30, 22, 0),   // Jun 30 (30-day month last day)
            (2026, 10, 4, 12, 0),   // Oct 4 DST-start day (23-hour day)
            (2026, 10, 31, 18, 0),  // Oct 31 (31-day month last day)
            (2026, 12, 31, 23, 0)   // Dec 31 (year edge)
        ]
        return seeds.map { year, month, day, hour, minute in
            makeSydneyDate(year: year, month: month, day: day, hour: hour, minute: minute)
        }
    }()

    /// Deterministic pseudo-random dates across 2024–2027 from a fixed-seed
    /// SplitMix64, so the property tests sweep arbitrary days without ever
    /// becoming flaky.
    static let randomSeeds: [Date] = {
        var generator = SplitMix64(seed: 0x1497)
        let epoch = makeSydneyDate(year: 2024, month: 1, day: 1, hour: 0, minute: 0)
        let spanSeconds = 4 * 365 * 24 * 3600
        return (0 ..< 40).map { _ in
            epoch.addingTimeInterval(TimeInterval(Int(generator.next() % UInt64(spanSeconds))))
        }
    }()

    static let allSeeds: [Date] = edgeSeeds + randomSeeds

    // MARK: - Round-trip invariants

    @Test(arguments: allSeeds, [1, 2, 7])
    func weekNextPreviousRoundTrips(date: Date, firstWeekday: Int) {
        let period = HistoryPeriod.week(containing: date, firstWeekday: firstWeekday)
        let forward = period.next(range: .weekToDate, firstWeekday: firstWeekday)
            .previous(range: .weekToDate, firstWeekday: firstWeekday)
        let backward = period.previous(range: .weekToDate, firstWeekday: firstWeekday)
            .next(range: .weekToDate, firstWeekday: firstWeekday)

        #expect(forward == period)
        #expect(backward == period)
    }

    @Test(arguments: allSeeds)
    func monthNextPreviousRoundTrips(date: Date) {
        let period = HistoryPeriod.month(containing: date)
        let forward = period.next(range: .monthToDate, firstWeekday: 2)
            .previous(range: .monthToDate, firstWeekday: 2)
        let backward = period.previous(range: .monthToDate, firstWeekday: 2)
            .next(range: .monthToDate, firstWeekday: 2)

        #expect(forward == period)
        #expect(backward == period)
    }

    // MARK: - Containment invariants

    @Test(arguments: allSeeds, [1, 2, 7])
    func weekContainsStartButNotEndExclusive(date: Date, firstWeekday: Int) {
        let period = HistoryPeriod.week(containing: date, firstWeekday: firstWeekday)
        #expect(period.contains(period.start))
        #expect(!period.contains(period.endExclusive))
        #expect(period.contains(date))
    }

    @Test(arguments: allSeeds)
    func monthContainsStartButNotEndExclusive(date: Date) {
        let period = HistoryPeriod.month(containing: date)
        #expect(period.contains(period.start))
        #expect(!period.contains(period.endExclusive))
        #expect(period.contains(date))
    }

    // MARK: - Period lengths

    @Test(arguments: allSeeds, [1, 2, 7])
    func weekPeriodsAlwaysSpanSevenDays(date: Date, firstWeekday: Int) {
        let period = HistoryPeriod.week(containing: date, firstWeekday: firstWeekday)
        #expect(period.dayCount == 7)
        #expect(period.previous(range: .weekToDate, firstWeekday: firstWeekday).dayCount == 7)
        #expect(period.next(range: .weekToDate, firstWeekday: firstWeekday).dayCount == 7)
    }

    @Test(arguments: allSeeds)
    func monthPeriodsSpanTwentyEightToThirtyOneDays(date: Date) {
        let period = HistoryPeriod.month(containing: date)
        #expect(period.dayCount >= 28)
        #expect(period.dayCount <= 31)
        let previous = period.previous(range: .monthToDate, firstWeekday: 2)
        #expect(previous.dayCount >= 28)
        #expect(previous.dayCount <= 31)
    }

    // MARK: - Same-period stability

    /// `week(containing: d)` must be identical for every `d` in the same week
    /// (likewise month) — the picker snaps any chosen day to one period.
    @Test(arguments: allSeeds, [1, 2, 7])
    func everyDayInAWeekResolvesTheSamePeriod(date: Date, firstWeekday: Int) {
        let period = HistoryPeriod.week(containing: date, firstWeekday: firstWeekday)
        let calendar = DateFormatting.sydneyCalendar
        for offset in 0 ..< 7 {
            guard let day = calendar.date(byAdding: .day, value: offset, to: period.start) else {
                Issue.record("date arithmetic failed for offset \(offset)")
                return
            }
            #expect(HistoryPeriod.week(containing: day, firstWeekday: firstWeekday) == period)
        }
    }

    @Test(arguments: allSeeds)
    func everyDayInAMonthResolvesTheSamePeriod(date: Date) {
        let period = HistoryPeriod.month(containing: date)
        let calendar = DateFormatting.sydneyCalendar
        for offset in 0 ..< period.dayCount {
            guard let day = calendar.date(byAdding: .day, value: offset, to: period.start) else {
                Issue.record("date arithmetic failed for offset \(offset)")
                return
            }
            #expect(HistoryPeriod.month(containing: day) == period)
        }
    }

    // MARK: - Boundaries at Sydney midnight

    @Test(arguments: allSeeds, [1, 2, 7])
    func weekBoundariesAreSydneyMidnights(date: Date, firstWeekday: Int) {
        let period = HistoryPeriod.week(containing: date, firstWeekday: firstWeekday)
        let calendar = DateFormatting.sydneyCalendar
        #expect(calendar.startOfDay(for: period.start) == period.start)
        #expect(calendar.startOfDay(for: period.endExclusive) == period.endExclusive)
        #expect(calendar.component(.weekday, from: period.start) == firstWeekday)
    }

    @Test(arguments: allSeeds)
    func monthStartsOnTheFirstAtSydneyMidnight(date: Date) {
        let period = HistoryPeriod.month(containing: date)
        let calendar = DateFormatting.sydneyCalendar
        #expect(calendar.startOfDay(for: period.start) == period.start)
        #expect(calendar.component(.day, from: period.start) == 1)
        #expect(calendar.component(.day, from: period.endExclusive) == 1)
    }

    // MARK: - Date strings and day counts (unit checks)

    @Test
    func weekDateStringsAndDayCountAreCorrect() {
        // 2026-04-15 is a Wednesday; Monday-start week is Apr 13–19.
        let date = Self.makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)
        let period = HistoryPeriod.week(containing: date, firstWeekday: 2)

        #expect(period.startDateString == "2026-04-13")
        #expect(period.endDateString == "2026-04-19")
        #expect(period.dayCount == 7)
    }

    @Test
    func monthDateStringsAndDayCountAreCorrect() {
        let date = Self.makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)
        let period = HistoryPeriod.month(containing: date)

        #expect(period.startDateString == "2026-04-01")
        #expect(period.endDateString == "2026-04-30")
        #expect(period.dayCount == 30)
    }

    @Test
    func februaryDayCountsRespectLeapYears() {
        let nonLeap = HistoryPeriod.month(
            containing: Self.makeSydneyDate(year: 2026, month: 2, day: 10, hour: 9, minute: 0)
        )
        let leap = HistoryPeriod.month(
            containing: Self.makeSydneyDate(year: 2024, month: 2, day: 10, hour: 9, minute: 0)
        )

        #expect(nonLeap.dayCount == 28)
        #expect(nonLeap.endDateString == "2026-02-28")
        #expect(leap.dayCount == 29)
        #expect(leap.endDateString == "2024-02-29")
    }

    @Test
    func previousWeekCrossesAprilDstFallbackWithoutDrift() {
        // 2026 Sydney DST ends Sun 2026-04-05. The Monday-start week containing
        // Wed 2026-04-08 is Apr 6–12; the previous week (Mar 30 – Apr 5)
        // contains the 25-hour day and must still start at Sydney midnight Monday.
        let date = Self.makeSydneyDate(year: 2026, month: 4, day: 8, hour: 12, minute: 0)
        let period = HistoryPeriod.week(containing: date, firstWeekday: 2)
        let previous = period.previous(range: .weekToDate, firstWeekday: 2)

        #expect(previous.startDateString == "2026-03-30")
        #expect(previous.endDateString == "2026-04-05")
        #expect(previous.dayCount == 7)
        #expect(previous.next(range: .weekToDate, firstWeekday: 2) == period)
    }

    @Test
    func previousMonthFromMarchIsFebruary() {
        let date = Self.makeSydneyDate(year: 2026, month: 3, day: 31, hour: 12, minute: 0)
        let period = HistoryPeriod.month(containing: date)
        let previous = period.previous(range: .monthToDate, firstWeekday: 2)

        #expect(previous.startDateString == "2026-02-01")
        #expect(previous.endDateString == "2026-02-28")
        #expect(previous.dayCount == 28)
    }

    // MARK: - Helpers

    private static func makeSydneyDate(year: Int, month: Int, day: Int, hour: Int, minute: Int) -> Date {
        DateFormatting.sydneyCalendar.date(from: DateComponents(
            timeZone: DateFormatting.sydneyTimeZone,
            year: year,
            month: month,
            day: day,
            hour: hour,
            minute: minute
        ))!
    }
}

/// Tiny deterministic PRNG so the "random" property-test dates are stable
/// across runs and machines.
private struct SplitMix64: RandomNumberGenerator {
    private var state: UInt64

    init(seed: UInt64) {
        state = seed
    }

    mutating func next() -> UInt64 {
        state &+= 0x9E3779B97F4A7C15
        var result = state
        result = (result ^ (result >> 30)) &* 0xBF58476D1CE4E5B9
        result = (result ^ (result >> 27)) &* 0x94D049BB133111EB
        return result ^ (result >> 31)
    }
}
