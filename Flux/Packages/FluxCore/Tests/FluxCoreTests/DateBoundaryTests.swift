import Foundation
import Testing
@testable import FluxCore

/// Tests for the Sydney-calendar boundary helpers: `startOfMonth`, `startOfWeek`,
/// and `inclusiveDayCount`. All inputs are seeded (never `Date.now`) so results are
/// deterministic regardless of when or where the suite runs.
@Suite struct DateBoundaryTests {
    // MARK: - startOfMonth

    @Test
    func startOfMonthReturnsFirstOfMonthAtSydneyMidnight() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 13, minute: 37)
        let start = DateFormatting.startOfMonth(now: now)
        let components = sydneyCalendar.dateComponents(
            [.year, .month, .day, .hour, .minute, .second], from: start
        )

        #expect(components.year == 2026)
        #expect(components.month == 4)
        #expect(components.day == 1)
        #expect(components.hour == 0)
        #expect(components.minute == 0)
        #expect(components.second == 0)
    }

    @Test
    func startOfMonthOnTheFirstReturnsTheSameDay() {
        let now = makeSydneyDate(year: 2026, month: 7, day: 1, hour: 9, minute: 0)
        let start = DateFormatting.startOfMonth(now: now)
        let components = sydneyCalendar.dateComponents([.year, .month, .day], from: start)

        #expect(components.year == 2026)
        #expect(components.month == 7)
        #expect(components.day == 1)
    }

    // MARK: - inclusiveDayCount: month lengths

    /// 28-day month (Feb non-leap), 29-day (Feb leap), 30-day (Apr), 31-day (Jul):
    /// month-to-date on the last day of each month yields the month's length.
    @Test(arguments: [
        (2026, 2, 28, 28),  // Feb 2026 non-leap
        (2024, 2, 29, 29),  // Feb 2024 leap
        (2026, 4, 30, 30),  // Apr 30 days
        (2026, 7, 31, 31)   // Jul 31 days
    ])
    func monthToDateCountOnLastDayEqualsMonthLength(
        year: Int, month: Int, lastDay: Int, expectedCount: Int
    ) {
        let now = makeSydneyDate(year: year, month: month, day: lastDay, hour: 23, minute: 30)
        let start = DateFormatting.startOfMonth(now: now)
        let count = DateFormatting.inclusiveDayCount(from: start, through: now)

        #expect(count == expectedCount)
    }

    @Test
    func monthToDateCountOnTheFirstIsOne() {
        let now = makeSydneyDate(year: 2026, month: 6, day: 1, hour: 0, minute: 1)
        let start = DateFormatting.startOfMonth(now: now)
        let count = DateFormatting.inclusiveDayCount(from: start, through: now)

        #expect(count == 1)
    }

    @Test
    func monthToDateCountMidMonth() {
        let now = makeSydneyDate(year: 2026, month: 6, day: 15, hour: 8, minute: 0)
        let start = DateFormatting.startOfMonth(now: now)
        let count = DateFormatting.inclusiveDayCount(from: start, through: now)

        #expect(count == 15)
    }

    // MARK: - startOfWeek

    /// `firstWeekday` 1 (Sunday), 2 (Monday), 7 (Saturday): the resolved start's
    /// weekday must equal the requested first weekday.
    @Test(arguments: [1, 2, 7])
    func startOfWeekWeekdayMatchesFirstWeekday(firstWeekday: Int) {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)
        let start = DateFormatting.startOfWeek(now: now, firstWeekday: firstWeekday)
        let weekday = sydneyCalendar.component(.weekday, from: start)

        #expect(weekday == firstWeekday)
    }

    /// 2026-04-15 is a Wednesday (weekday 4). With Monday start (firstWeekday 2)
    /// the week begins on Monday 2026-04-13; the count through Wed is 3.
    @Test
    func weekToDateCountMondayStartMidWeek() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)
        let start = DateFormatting.startOfWeek(now: now, firstWeekday: 2)
        let components = sydneyCalendar.dateComponents([.year, .month, .day], from: start)

        #expect(components.year == 2026)
        #expect(components.month == 4)
        #expect(components.day == 13)
        #expect(DateFormatting.inclusiveDayCount(from: start, through: now) == 3)
    }

    /// With Sunday start (firstWeekday 1) the week containing Wed 2026-04-15
    /// begins on Sunday 2026-04-12; the count through Wed is 4.
    @Test
    func weekToDateCountSundayStartMidWeek() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)
        let start = DateFormatting.startOfWeek(now: now, firstWeekday: 1)
        let components = sydneyCalendar.dateComponents([.year, .month, .day], from: start)

        #expect(components.day == 12)
        #expect(DateFormatting.inclusiveDayCount(from: start, through: now) == 4)
    }

    /// On the week-start day itself the count is 1. 2026-04-13 is a Monday.
    @Test
    func weekToDateCountOnWeekStartDayIsOne() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 13, hour: 6, minute: 0)
        let start = DateFormatting.startOfWeek(now: now, firstWeekday: 2)
        let count = DateFormatting.inclusiveDayCount(from: start, through: now)

        #expect(count == 1)
    }

    @Test
    func startOfWeekReturnsSydneyMidnight() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)
        let start = DateFormatting.startOfWeek(now: now, firstWeekday: 2)
        let components = sydneyCalendar.dateComponents([.hour, .minute, .second], from: start)

        #expect(components.hour == 0)
        #expect(components.minute == 0)
        #expect(components.second == 0)
    }

    /// Mutating a copy of the calendar for the week start must NOT corrupt the
    /// shared `sydneyCalendar.firstWeekday` used by every other computation.
    @Test
    func startOfWeekDoesNotMutateSharedCalendar() {
        let originalFirstWeekday = DateFormatting.sydneyCalendar.firstWeekday
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)
        _ = DateFormatting.startOfWeek(now: now, firstWeekday: 7)

        #expect(DateFormatting.sydneyCalendar.firstWeekday == originalFirstWeekday)
    }

    // MARK: - inclusiveDayCount: independent of time-of-day

    /// Same calendar day, different times of day, still counts as 1.
    @Test
    func inclusiveDayCountSameDayIsOne() {
        let start = makeSydneyDate(year: 2026, month: 6, day: 10, hour: 0, minute: 5)
        let end = makeSydneyDate(year: 2026, month: 6, day: 10, hour: 23, minute: 55)

        #expect(DateFormatting.inclusiveDayCount(from: start, through: end) == 1)
    }

    /// Count is driven by calendar day, not by the elapsed interval: an end time
    /// earlier in the day than the start time still spans 2 calendar days.
    @Test
    func inclusiveDayCountIgnoresTimeOfDay() {
        let start = makeSydneyDate(year: 2026, month: 6, day: 10, hour: 23, minute: 0)
        let end = makeSydneyDate(year: 2026, month: 6, day: 11, hour: 1, minute: 0)

        #expect(DateFormatting.inclusiveDayCount(from: start, through: end) == 2)
    }

    // MARK: - DST: 23/25-hour days must not shift the count

    /// Sydney DST ends in early April (clocks back, 25-hour day). A month-to-date
    /// window spanning the transition must yield the same per-day count as any
    /// other day — guards against dividing an elapsed interval.
    @Test
    func aprilDstTransitionDoesNotShiftMonthCount() {
        // 2026: DST ends Sun 2026-04-05 (clocks back, 25-hour day).
        let now = makeSydneyDate(year: 2026, month: 4, day: 6, hour: 12, minute: 0)
        let start = DateFormatting.startOfMonth(now: now)
        // Apr 1 through Apr 6 inclusive = 6 days, despite the 25-hour Apr 5.
        #expect(DateFormatting.inclusiveDayCount(from: start, through: now) == 6)
    }

    /// Sydney DST starts in early October (clocks forward, 23-hour day).
    @Test
    func octoberDstTransitionDoesNotShiftMonthCount() {
        // 2026: DST starts Sun 2026-10-04 (clocks forward, 23-hour day).
        let now = makeSydneyDate(year: 2026, month: 10, day: 7, hour: 12, minute: 0)
        let start = DateFormatting.startOfMonth(now: now)
        // Oct 1 through Oct 7 inclusive = 7 days, despite the 23-hour Oct 4.
        #expect(DateFormatting.inclusiveDayCount(from: start, through: now) == 7)
    }

    /// A week window crossing the April DST end yields the same count as a normal
    /// week. Sun 2026-04-05 is the DST-end day; with Sunday start the week begins
    /// on 2026-04-05 and through Sat 2026-04-11 is 7 days.
    @Test
    func dstWeekCountMatchesNormalWeek() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 11, hour: 12, minute: 0)
        let start = DateFormatting.startOfWeek(now: now, firstWeekday: 1)
        #expect(DateFormatting.inclusiveDayCount(from: start, through: now) == 7)
    }

    // MARK: - Property tests

    /// Seeded dates spanning leap/non-leap, month edges, and both DST transitions.
    /// Never `Date.now` — the suite must be deterministic.
    static let seedDates: [Date] = {
        let calendar = makeSydneyCalendar()
        let seeds: [(Int, Int, Int, Int, Int)] = [
            (2026, 1, 1, 0, 0),     // Jan 1 (year edge)
            (2026, 2, 28, 23, 59),  // Feb 28 non-leap last day
            (2024, 2, 29, 12, 0),   // Feb 29 leap day
            (2026, 3, 31, 6, 30),   // Mar 31 (31-day month)
            (2026, 4, 5, 12, 0),    // Apr 5 DST-end day
            (2026, 4, 15, 13, 37),  // mid-April
            (2026, 6, 1, 0, 1),     // Jun 1 (month start)
            (2026, 6, 30, 22, 0),   // Jun 30 (30-day month last day)
            (2026, 10, 4, 12, 0),   // Oct 4 DST-start day
            (2026, 10, 31, 18, 0),  // Oct 31 (31-day month last day)
            (2026, 12, 31, 23, 0),  // Dec 31 (year edge)
            (2027, 7, 14, 9, 0)     // arbitrary future day
        ]
        return seeds.map { year, month, day, hour, minute in
            calendar.date(from: DateComponents(
                timeZone: DateFormatting.sydneyTimeZone,
                year: year, month: month, day: day, hour: hour, minute: minute
            ))!
        }
    }()

    @Test(arguments: seedDates, 1 ... 7)
    func weekToDateInvariantsHold(now: Date, firstWeekday: Int) {
        let start = DateFormatting.startOfWeek(now: now, firstWeekday: firstWeekday)
        let count = DateFormatting.inclusiveDayCount(from: start, through: now)
        let weekday = sydneyCalendar.component(.weekday, from: start)

        #expect(weekday == firstWeekday)
        #expect(start <= now)
        #expect(count >= 1)
        #expect(count <= 7)
    }

    @Test(arguments: seedDates)
    func monthToDateInvariantsHold(now: Date) {
        let start = DateFormatting.startOfMonth(now: now)
        let count = DateFormatting.inclusiveDayCount(from: start, through: now)
        let day = sydneyCalendar.component(.day, from: start)

        #expect(day == 1)
        #expect(start <= now)
        #expect(count >= 1)
        #expect(count <= 31)
    }

    // MARK: - Helpers

    private var sydneyCalendar: Calendar { Self.makeSydneyCalendar() }

    private static func makeSydneyCalendar() -> Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = DateFormatting.sydneyTimeZone
        return calendar
    }

    private func makeSydneyDate(year: Int, month: Int, day: Int, hour: Int, minute: Int) -> Date {
        sydneyCalendar.date(from: DateComponents(
            timeZone: DateFormatting.sydneyTimeZone,
            year: year,
            month: month,
            day: day,
            hour: hour,
            minute: minute
        ))!
    }
}
