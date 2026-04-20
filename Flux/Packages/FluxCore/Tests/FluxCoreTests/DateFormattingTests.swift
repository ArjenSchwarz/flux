import Foundation
import Testing
@testable import FluxCore

@Suite struct DateFormattingTests {
    @Test
    func parseTimestampReturnsDateForValidISO8601() {
        let parsed = DateFormatting.parseTimestamp("2026-04-11T21:47:00Z")
        #expect(parsed != nil)
    }

    @Test
    func parseTimestampReturnsNilForInvalidISO8601() {
        #expect(DateFormatting.parseTimestamp("not-a-date") == nil)
    }

    @Test
    func todayDateStringUsesSydneyTimezone() {
        let utcCalendar = Calendar(identifier: .gregorian)
        let now = utcCalendar.date(from: DateComponents(
            timeZone: TimeZone(secondsFromGMT: 0),
            year: 2026,
            month: 4,
            day: 15,
            hour: 16,
            minute: 30
        ))!

        #expect(DateFormatting.todayDateString(now: now) == "2026-04-16")
    }

    @Test
    func isTodayMatchesAndMismatchesInSydneyTimezone() {
        let utcCalendar = Calendar(identifier: .gregorian)
        let now = utcCalendar.date(from: DateComponents(
            timeZone: TimeZone(secondsFromGMT: 0),
            year: 2026,
            month: 4,
            day: 15,
            hour: 16,
            minute: 30
        ))!

        #expect(DateFormatting.isToday("2026-04-16", now: now))
        #expect(DateFormatting.isToday("2026-04-15", now: now) == false)
    }

    @Test
    func parseWindowTimeUsesSydneyTimezone() throws {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 9, minute: 5)
        let windowDate = DateFormatting.parseWindowTime("11:00", on: now)
        let parsed = try #require(windowDate)
        let components = sydneyCalendar.dateComponents([.year, .month, .day, .hour, .minute], from: parsed)

        #expect(components.year == 2026)
        #expect(components.month == 4)
        #expect(components.day == 15)
        #expect(components.hour == 11)
        #expect(components.minute == 0)
    }

    @Test
    func parseWindowTimeRejectsInvalidFormats() {
        #expect(DateFormatting.parseWindowTime("invalid") == nil)
        #expect(DateFormatting.parseWindowTime("24:00") == nil)
        #expect(DateFormatting.parseWindowTime("09:60") == nil)
    }

    @Test
    func isInOffpeakWindowHandlesBoundaries() {
        let startBoundary = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 11, minute: 0)
        let insideWindow = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 30)
        let beforeWindow = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 10, minute: 59)
        let endBoundary = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 14, minute: 0)

        #expect(DateFormatting.isInOffpeakWindow(start: "11:00", end: "14:00", now: startBoundary))
        #expect(DateFormatting.isInOffpeakWindow(start: "11:00", end: "14:00", now: insideWindow))
        #expect(DateFormatting.isInOffpeakWindow(start: "11:00", end: "14:00", now: beforeWindow) == false)
        #expect(DateFormatting.isInOffpeakWindow(start: "11:00", end: "14:00", now: endBoundary) == false)
    }

    private var sydneyCalendar: Calendar {
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
