import FluxCore
import Foundation
import Testing
@testable import Flux

@Suite
struct HistoryRangeTests {
    // MARK: - pickerLabel

    @Test
    func pickerLabelsMatchSpec() {
        #expect(HistoryRange.days(7).pickerLabel == "7d")
        #expect(HistoryRange.days(14).pickerLabel == "14d")
        #expect(HistoryRange.days(30).pickerLabel == "30d")
        #expect(HistoryRange.weekToDate.pickerLabel == "Wk")
        #expect(HistoryRange.monthToDate.pickerLabel == "Mo")
    }

    // MARK: - .days passthrough

    @Test
    func fixedDaysResolvePassthrough() {
        // now/firstWeekday must not influence a fixed range.
        let now = makeSydneyMidday(year: 2026, month: 4, day: 15)
        #expect(HistoryRange.days(7).resolvedDays(now: now, firstWeekday: 1) == 7)
        #expect(HistoryRange.days(14).resolvedDays(now: now, firstWeekday: 2) == 14)
        #expect(HistoryRange.days(30).resolvedDays(now: now, firstWeekday: 7) == 30)
    }

    // MARK: - monthToDate

    @Test
    func monthToDateOnFirstOfMonthResolvesToOne() {
        let now = makeSydneyMidday(year: 2026, month: 4, day: 1)
        #expect(HistoryRange.monthToDate.resolvedDays(now: now, firstWeekday: 2) == 1)
    }

    @Test
    func monthToDateMidMonthResolvesToInclusiveCount() {
        // 15th of the month → days 1…15 inclusive = 15.
        let now = makeSydneyMidday(year: 2026, month: 4, day: 15)
        #expect(HistoryRange.monthToDate.resolvedDays(now: now, firstWeekday: 2) == 15)
    }

    @Test
    func monthToDateOn31stResolvesTo31() {
        // March has 31 days → full month-to-date is 31, never clipped to 30.
        let now = makeSydneyMidday(year: 2026, month: 3, day: 31)
        #expect(HistoryRange.monthToDate.resolvedDays(now: now, firstWeekday: 2) == 31)
    }

    // MARK: - weekToDate

    @Test
    func weekToDateOnWeekStartDayResolvesToOne() {
        // 2026-04-13 is a Monday. With firstWeekday = 2 (Monday) the week
        // starts today, so the inclusive count is 1.
        let now = makeSydneyMidday(year: 2026, month: 4, day: 13)
        #expect(HistoryRange.weekToDate.resolvedDays(now: now, firstWeekday: 2) == 1)
    }

    @Test
    func weekToDateMidWeekMondayStart() {
        // 2026-04-15 is a Wednesday. Monday-start week → Mon, Tue, Wed = 3.
        let now = makeSydneyMidday(year: 2026, month: 4, day: 15)
        #expect(HistoryRange.weekToDate.resolvedDays(now: now, firstWeekday: 2) == 3)
    }

    @Test
    func weekToDateMidWeekSundayStart() {
        // 2026-04-15 is a Wednesday. Sunday-start week begins 2026-04-12 →
        // Sun, Mon, Tue, Wed = 4.
        let now = makeSydneyMidday(year: 2026, month: 4, day: 15)
        #expect(HistoryRange.weekToDate.resolvedDays(now: now, firstWeekday: 1) == 4)
    }

    @Test
    func weekToDateOnSundayWithSundayStartResolvesToOne() {
        // 2026-04-12 is a Sunday. Sunday-start week starts today → 1.
        let now = makeSydneyMidday(year: 2026, month: 4, day: 12)
        #expect(HistoryRange.weekToDate.resolvedDays(now: now, firstWeekday: 1) == 1)
    }

    // MARK: - Helpers

    /// A `Date` at 12:00 Sydney time on the given calendar date, so it sits
    /// unambiguously inside the intended Sydney day regardless of UTC offset.
    private func makeSydneyMidday(year: Int, month: Int, day: Int) -> Date {
        DateFormatting.sydneyCalendar.date(from: DateComponents(
            timeZone: DateFormatting.sydneyTimeZone,
            year: year,
            month: month,
            day: day,
            hour: 12,
            minute: 0
        ))!
    }
}
