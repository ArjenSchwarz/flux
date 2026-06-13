import FluxCore
import Foundation
import Testing
@testable import Flux

@Suite
struct HistoryChartDomainTests {
    private let calendar = DateFormatting.sydneyCalendar

    /// Sydney-local wall-clock date for an unambiguous assertion baseline.
    private func sydneyDate(_ year: Int, _ month: Int, _ day: Int, hour: Int = 12) -> Date {
        calendar.date(from: DateComponents(
            timeZone: DateFormatting.sydneyTimeZone,
            year: year, month: month, day: day, hour: hour
        ))!
    }

    @Test("Fixed .days ranges reserve nothing")
    func fixedRangesReturnNil() {
        let now = sydneyDate(2026, 6, 10)
        #expect(HistoryChartDomain.make(range: .days(7), referenceDate: now, firstWeekday: 2) == nil)
        #expect(HistoryChartDomain.make(range: .days(14), referenceDate: now, firstWeekday: 1) == nil)
        #expect(HistoryChartDomain.make(range: .days(30), referenceDate: now, firstWeekday: 2) == nil)
    }

    @Test("Week-to-date reserves a full 7-day week from the week start")
    func weekToDateReservesFullWeek() throws {
        // 2026-06-10 is a Wednesday; Monday-start week begins 2026-06-08.
        let now = sydneyDate(2026, 6, 10)
        let domain = try #require(HistoryChartDomain.make(range: .weekToDate, referenceDate: now, firstWeekday: 2))

        let weekStart = DateFormatting.startOfWeek(now: now, firstWeekday: 2)
        #expect(domain.slotDates.count == 7)
        #expect(domain.slotDates.first == weekStart)
        #expect(domain.span.lowerBound == weekStart)
        // Upper bound is exclusive: Sydney 00:00 of the next week's first day.
        #expect(domain.span.upperBound == calendar.date(byAdding: .day, value: 7, to: weekStart))
        // Every slot is a distinct Sydney midnight one day apart.
        #expect(domain.slotDates.last == calendar.date(byAdding: .day, value: 6, to: weekStart))
        #expect(domain.slotDates.allSatisfy { calendar.startOfDay(for: $0) == $0 })
    }

    @Test("Week start honours the locale first weekday")
    func weekStartFollowsFirstWeekday() throws {
        let now = sydneyDate(2026, 6, 10) // Wednesday
        let mondayStart = try #require(HistoryChartDomain.make(range: .weekToDate, referenceDate: now, firstWeekday: 2))
        let sundayStart = try #require(HistoryChartDomain.make(range: .weekToDate, referenceDate: now, firstWeekday: 1))
        // Monday-start begins 2026-06-08; Sunday-start begins 2026-06-07.
        #expect(mondayStart.slotDates.first == sydneyDate(2026, 6, 8, hour: 0))
        #expect(sundayStart.slotDates.first == sydneyDate(2026, 6, 7, hour: 0))
        #expect(mondayStart.slotDates.count == 7)
        #expect(sundayStart.slotDates.count == 7)
    }

    @Test("Month-to-date reserves every day of the calendar month")
    func monthToDateReservesFullMonth() throws {
        // June has 30 days.
        let now = sydneyDate(2026, 6, 9)
        let domain = try #require(HistoryChartDomain.make(range: .monthToDate, referenceDate: now, firstWeekday: 2))

        let monthStart = DateFormatting.startOfMonth(now: now)
        #expect(domain.slotDates.count == 30)
        #expect(domain.slotDates.first == monthStart)
        #expect(domain.span.lowerBound == monthStart)
        #expect(domain.span.upperBound == sydneyDate(2026, 7, 1, hour: 0), "exclusive bound is the 1st of next month")
    }

    @Test("Month-to-date across the DST boundary stays day-aligned")
    func monthToDateHandlesDST() throws {
        // Sydney DST ends in early April 2026; April still has 30 calendar days,
        // and calendar-day arithmetic keeps every slot at Sydney midnight.
        let now = sydneyDate(2026, 4, 15)
        let domain = try #require(HistoryChartDomain.make(range: .monthToDate, referenceDate: now, firstWeekday: 2))
        #expect(domain.slotDates.count == 30)
        #expect(domain.slotDates.allSatisfy { calendar.startOfDay(for: $0) == $0 })
        #expect(domain.span.upperBound == sydneyDate(2026, 5, 1, hour: 0))
    }
}
